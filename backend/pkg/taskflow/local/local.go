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
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

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

	configuredAgentBin := cfg.AgentBin
	cfg.AgentBin = resolveLocalAgentBin(configuredAgentBin)

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
	c.logger.Info("local opencode engine resolved",
		"configured", configuredAgentBin, "resolved", c.cfg.AgentBin)
	// Le registre des VM est en mémoire, mais les workspaces persistent. Les
	// restaurer au démarrage garde le terminal et l'explorateur utilisables
	// après `nemesiscode restart`.
	if err := c.restoreWorkspaces(); err != nil {
		c.logger.Warn("restore local workspaces failed", "error", err)
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

// resolveLocalAgentBin rend le backend autonome par rapport au launcher. Une
// ancienne configuration 1.x peut encore fournir "ohmyagent" ; comme le
// driver local parle désormais exclusivement le CLI opencode, cette valeur est
// ignorée au profit du moteur baseline embarqué dans le paquet.
func resolveLocalAgentBin(configured string) string {
	configured = strings.TrimSpace(configured)

	// Une surcharge explicite par chemin garde la priorité si elle existe.
	if strings.Contains(configured, string(filepath.Separator)) {
		if candidate := executablePath(configured); candidate != "" {
			return candidate
		}
	}
	// Un nom de commande personnalisé reste supporté. Les deux anciens
	// defaults sont traités plus bas pour préférer le baseline du paquet.
	if configured != "" && configured != "ohmyagent" && configured != "opencode" {
		if candidate := executablePath(configured); candidate != "" {
			return candidate
		}
	}

	home, _ := os.UserHomeDir()
	candidates := []string{
		"/usr/share/nemesiscode/opencode",
		filepath.Join(home, ".nemesiscode", "opencode"),
		filepath.Join(home, ".local", "bin", "opencode"),
		"/usr/local/bin/opencode",
		"opencode",
	}
	for _, candidate := range candidates {
		if path := executablePath(candidate); path != "" {
			return path
		}
	}
	// L'erreur de démarrage mentionnera opencode, jamais l'ancien ohmyagent.
	return "opencode"
}

func executablePath(candidate string) string {
	candidate = expandHome(strings.TrimSpace(candidate))
	if candidate == "" {
		return ""
	}
	if strings.Contains(candidate, string(filepath.Separator)) {
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
			return ""
		}
		return candidate
	}
	path, err := exec.LookPath(candidate)
	if err != nil {
		return ""
	}
	return path
}

func (c *Client) restoreWorkspaces() error {
	entries, err := os.ReadDir(c.root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := c.ensureVM(entry.Name()); err != nil {
			c.logger.Warn("skip invalid local workspace", "workspace", entry.Name(), "error", err)
		}
	}
	return nil
}

// ensureVM reconstruit à la demande l'enregistrement mémoire d'un workspace
// persistant. C'est indispensable pour les terminaux après redémarrage du
// backend, car la VM locale est un répertoire et non un conteneur éphémère.
func (c *Client) ensureVM(id string) (*VM, error) {
	if id == "" || filepath.Base(id) != id || id == "." || id == ".." {
		return nil, fmt.Errorf("invalid environment id: %q", id)
	}

	c.mu.Lock()
	if rec := c.vms[id]; rec != nil {
		c.mu.Unlock()
		return rec, nil
	}
	c.mu.Unlock()

	ws := filepath.Join(c.root, id)
	info, err := os.Stat(ws)
	if err != nil {
		return nil, fmt.Errorf("environment not found: %s", id)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("environment workspace is not a directory: %s", id)
	}

	var lastReq *taskflow.CreateTaskReq
	cfgPath := filepath.Join(ws, taskConfigFile)
	if b, err := os.ReadFile(cfgPath); err == nil {
		var req taskflow.CreateTaskReq
		if err := json.Unmarshal(b, &req); err != nil {
			c.logger.Warn("invalid persisted task config", "vm_id", id, "error", err)
		} else {
			lastReq = &req
		}
		// Met à niveau les permissions des anciennes installations 1.1.
		_ = os.Chmod(cfgPath, 0o600)
	}
	_ = os.Chmod(filepath.Join(ws, "opencode.json"), 0o600)

	hostname, _ := os.Hostname()
	rec := &VM{
		record: &taskflow.VirtualMachine{
			ID:            id,
			HostID:        c.hostID,
			Hostname:      hostname,
			Arch:          runtime.GOARCH,
			OS:            runtime.GOOS,
			Name:          "local-" + id,
			Status:        taskflow.VirtualMachineStatusOnline,
			StatusMessage: "restored local environment on host",
			Cores:         int32(runtime.NumCPU()),
			TTL:           taskflow.TTL{Kind: taskflow.TTLForever},
			CreatedAt:     info.ModTime().Unix(),
			Version:       "nemesis-local-1.2.2",
		},
		workspace: ws,
		live:      NewLiveStream(),
		shells:    make(map[string]*Shell),
		ports:     make(map[string]*taskflow.PortForwardInfo),
		lastReq:   lastReq,
	}
	if rec.record.CreatedAt == 0 {
		rec.record.CreatedAt = time.Now().Unix()
	}

	c.mu.Lock()
	if existing := c.vms[id]; existing != nil {
		c.mu.Unlock()
		return existing, nil
	}
	c.vms[id] = rec
	c.mu.Unlock()
	c.logger.Info("local workspace restored", "vm_id", id, "workspace", ws,
		"has_task_config", lastReq != nil)
	return rec, nil
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
	rec := c.vms[id]
	c.mu.Unlock()
	if rec != nil {
		return rec
	}
	rec, _ = c.ensureVM(id)
	return rec
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
	ag.ignored.Store(true)
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
		Version:  "nemesis-local-1.2.2",
	}
}

func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
