// Package local 提供 taskflow.Clienter 的「本机模式」实现。
//
// 当 backend 以 MCAI_TASKFLOW_MODE=local 启动时，机器 hôte 本身就是
// l'environnement de développement : 没有 taskflow 服务、没有 VM 容器、
// 没有 rustfs。每条任务对应一个工作区目录（默认
// ~/.nemesiscode/workspaces/<vm_id>），agent 引擎直接在本机以子进程
// 方式运行，终端、文件、diff 都作用于本机。
package local

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/teteekoue/NemesisCode/backend/config"
	"github.com/teteekoue/NemesisCode/backend/pkg/taskflow"
)

// VM 是「本机虚拟机」：一个工作区目录 + 可选的 agent 进程 + 终端/端口注册表。
type VM struct {
	mu        sync.Mutex
	record    *taskflow.VirtualMachine
	userID    string
	workspace string
	repoURL   string
	// agent : processus `opencode run` du vrai moteur opencode (ou nil).
	agent *agentClient
	// stopped : arrêt volontaire (Cancel) — la fin du process ne doit alors
	// pas publier task-ended (déjà publié par Cancel).
	stopped bool
	live    *LiveStream
	shells  map[string]*Shell
	ports   map[string]*taskflow.PortForwardInfo
	lastReq *taskflow.CreateTaskReq
}

// Client 实现 taskflow.Clienter。
type Client struct {
	cfg      config.LocalTaskFlow
	root     string
	hostID   string
	hostName string
	shell    string
	logger   *slog.Logger
	// internalBaseURL : base URL du serveur (pour les callbacks internes
	// comme /internal/vm-ready qui déclenchent le lancement des tâches).
	internalBaseURL string

	mu  sync.Mutex
	vms map[string]*VM
}

// WithInternalBaseURL configure l'URL de base du serveur backend local
// (ex. http://127.0.0.1:8888) pour les callbacks internes.
func WithInternalBaseURL(u string) func(*Client) {
	return func(c *Client) {
		c.internalBaseURL = u
	}
}

// NewClient 创建本机 taskflow 客户端。workspace root 缺省
// ~/.nemesiscode/workspaces，目录不存在则创建。
func NewClient(cfg config.LocalTaskFlow, logger *slog.Logger, opts ...func(*Client)) (*Client, error) {
	if logger == nil {
		logger = slog.Default()
	}
	root := cfg.WorkspaceRoot
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir: %w", err)
		}
		root = filepath.Join(home, ".nemesiscode", "workspaces")
	}
	root = expandHome(root)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create workspace root %s: %w", root, err)
	}

	hostname, _ := os.Hostname()
	hostID := cfg.HostID
	if hostID == "" {
		hostID = "local-" + hostname
	}
	hostName := cfg.HostName
	if hostName == "" {
		hostName = hostname
	}
	shell := cfg.Shell
	if shell == "" {
		shell = os.Getenv("SHELL")
	}
	if shell == "" {
		shell = "/bin/sh"
	}

	c := &Client{
		cfg:      cfg,
		root:     root,
		hostID:   hostID,
		hostName: hostName,
		shell:    shell,
		logger:   logger.With("taskflow", "local"),
		vms:      make(map[string]*VM),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

func expandHome(p string) string {
	if p == "~" {
		home, _ := os.UserHomeDir()
		return home
	}
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

// ---- taskflow.Clienter ----

func (c *Client) VirtualMachiner() taskflow.VirtualMachiner { return &vmManager{c: c} }
func (c *Client) Host() taskflow.Hoster                     { return &hostManager{c: c} }
func (c *Client) FileManager() taskflow.FileManager         { return &fileManager{c: c} }
func (c *Client) TaskManager() taskflow.TaskManager         { return &taskManager{c: c} }
func (c *Client) PortForwarder() taskflow.PortForwarder     { return &portForwarder{c: c} }

// Stats 统计本机当前状态。
func (c *Client) Stats(ctx context.Context) (*taskflow.Stats, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := &taskflow.Stats{OnlineHostCount: 1}
	for _, vm := range c.vms {
		vm.mu.Lock()
		if vm.record.Status == taskflow.VirtualMachineStatusOnline {
			st.OnlineVMCount++
		}
		if vm.agent != nil {
			st.OnlineTaskCount++
		}
		vm.mu.Unlock()
	}
	return st, nil
}

// TaskLive 重放（可选）历史 chunk 然后订阅实时 chunk，直到 ctx 取消。
// 语义与远端 taskflow 的 /internal/ws/task-live 一致。
func (c *Client) TaskLive(ctx context.Context, taskID string, flush bool, fn func(*taskflow.TaskChunk) error) error {
	rec := c.getVMByTask(taskID)
	if rec == nil {
		return fmt.Errorf("environment not found: %s", taskID)
	}
	if flush {
		if err := rec.live.Replay(fn); err != nil {
			return err
		}
	}
	ch := make(chan *taskflow.TaskChunk, 256)
	rec.live.Subscribe(ch)
	defer rec.live.Unsubscribe(ch)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case chunk := <-ch:
			if err := fn(chunk); err != nil {
				return err
			}
		}
	}
}

// ---- helpers ----

func (c *Client) getVM(id string) *VM {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.vms[id]
}

// getVMByTask 通过任务 UUID 找 VM（CreateTaskReq.ID）。
func (c *Client) getVMByTask(taskID string) *VM {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, vm := range c.vms {
		vm.mu.Lock()
		req := vm.lastReq
		vm.mu.Unlock()
		if req != nil && req.ID.String() == taskID {
			return vm
		}
	}
	return nil
}

// stopAgent arrête le processus opencode en cours (SIGTERM au groupe, 3 s
// de grâce puis SIGKILL). Retourne vrai s'il y avait bien un processus.
// Ne touche pas à rec.stopped (géré par Cancel).
func (c *Client) stopAgent(rec *VM) bool {
	rec.mu.Lock()
	ag := rec.agent
	rec.agent = nil
	rec.mu.Unlock()
	if ag == nil {
		return false
	}
	ag.close()
	return true
}

// HostInfo 返回本机宿主信息（供 pkg/localhost 注册到数据库）。
func (c *Client) HostInfo() *taskflow.Host {
	return c.hostInfo()
}

// hostInfo 构建本机宿主信息（磁盘容量尽力而为，失败返回 0）。
func (c *Client) hostInfo() *taskflow.Host {
	hostname, _ := os.Hostname()
	return &taskflow.Host{
		ID:       c.hostID,
		Hostname: hostname,
		Name:     c.hostName,
		Arch:     runtime.GOARCH,
		OS:       runtime.GOOS,
		Cores:    int32(runtime.NumCPU()),
		Memory:   readMemTotal(),
		Disk:     readDiskTotal(c.root),
		TTL:      taskflow.TTL{Kind: taskflow.TTLForever},
		Version:  "nemesis-local-1.0",
	}
}

func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
