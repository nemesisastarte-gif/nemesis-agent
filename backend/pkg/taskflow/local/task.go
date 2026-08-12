package local

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/teteekoue/NemesisCode/backend/pkg/taskflow"
)

const taskConfigFile = "nemesis-task.json"

type taskManager struct{ c *Client }

var _ taskflow.TaskManager = (*taskManager)(nil)

// Create 持久化 la config de tâche dans le workspace puis lance le moteur
// agent sur la machine hôte (contrat décrit dans docs/local-mode-design.md) :
//
//	$NEMESIS_AGENT_BIN --task-config <workspace>/nemesis-task.json
//
// env : NEMESIS_TASK_ID / NEMESIS_VM_ID / NEMESIS_WORKSPACE, cwd = workspace.
func (m *taskManager) Create(ctx context.Context, req taskflow.CreateTaskReq) error {
	rec := m.c.getVM(req.VMID)
	if rec == nil {
		return fmt.Errorf("environment not found: %s", req.VMID)
	}

	rec.mu.Lock()
	rec.lastReq = &req
	rec.mu.Unlock()

	if err := m.persistTaskConfig(rec); err != nil {
		return err
	}
	return m.c.spawnAgent(ctx, rec)
}

// persistTaskConfig 把 lastReq 序列化到 workspace/nemesis-task.json.
func (m *taskManager) persistTaskConfig(rec *VM) error {
	rec.mu.Lock()
	req := rec.lastReq
	rec.mu.Unlock()
	if req == nil {
		return fmt.Errorf("no task config for vm %s", rec.record.ID)
	}
	b, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal task config: %w", err)
	}
	cfgPath := filepath.Join(rec.workspace, taskConfigFile)
	if err := os.WriteFile(cfgPath, b, 0o644); err != nil {
		return fmt.Errorf("write task config: %w", err)
	}
	return nil
}

// spawnAgent 启动（或重启）agent 进程并接上输出流。
func (c *Client) spawnAgent(ctx context.Context, rec *VM) error {
	c.stopAgent(rec)

	rec.mu.Lock()
	req := rec.lastReq
	rec.mu.Unlock()
	if req == nil {
		return fmt.Errorf("no task config for vm %s", rec.record.ID)
	}

	cfgPath := filepath.Join(rec.workspace, taskConfigFile)
	args := append([]string{}, c.cfg.AgentArgs...)
	if len(args) == 0 {
		args = []string{"--task-config", cfgPath}
	}

	// NB: on utilise exec.Command (pas CommandContext) : le ctx du hook de
	// lifecycle peut être annulé dès que handleProcessing rend la main, ce qui
	// tuerait l'agent. La fin de vie est gérée explicitement par stopAgent.
	cmd := exec.Command(c.cfg.AgentBin, args...)
	cmd.Dir = rec.workspace
	cmd.Env = append(os.Environ(),
		"NEMESIS_TASK_ID="+req.ID.String(),
		"NEMESIS_VM_ID="+rec.record.ID,
		"NEMESIS_WORKSPACE="+rec.workspace,
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("agent stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("agent stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start agent %s: %w", c.cfg.AgentBin, err)
	}

	rec.mu.Lock()
	rec.proc = cmd.Process
	rec.mu.Unlock()

	c.logger.InfoContext(ctx, "local agent started",
		"task_id", req.ID.String(), "vm_id", rec.record.ID,
		"bin", c.cfg.AgentBin, "workspace", rec.workspace)

	go c.pumpOutput(rec, stdout, "stdout")
	go c.pumpOutput(rec, stderr, "stderr")
	go c.waitAgent(rec, cmd)
	return nil
}

// pumpOutput 读进程输出：ligne JSON valide (event non vide) → chunk structuré
// tel quel ; sinon → chunk brut Event "output" / Kind stdout|stderr.
func (c *Client) pumpOutput(rec *VM, r io.Reader, kind string) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		var chunk taskflow.TaskChunk
		if json.Unmarshal(line, &chunk) == nil && chunk.Event != "" {
			rec.live.Publish(&chunk)
			continue
		}
		data := make([]byte, len(line)+1)
		copy(data, line)
		data[len(line)] = '\n'
		rec.live.Publish(&taskflow.TaskChunk{
			Event: "output",
			Kind:  kind,
			Data:  data,
		})
	}
}

// waitAgent 在进程退出时发布 task-ended 并清理 proc 引用。
func (c *Client) waitAgent(rec *VM, cmd *exec.Cmd) {
	waitErr := cmd.Wait()

	status := "success"
	if waitErr != nil {
		status = "failed"
	}

	rec.mu.Lock()
	if rec.proc == cmd.Process {
		rec.proc = nil
	}
	rec.mu.Unlock()

	rec.live.Publish(&taskflow.TaskChunk{Event: "task-ended", Kind: status})
}

func (m *taskManager) taskVM(req taskflow.TaskReq) (*VM, error) {
	if req.VirtualMachine == nil {
		return nil, fmt.Errorf("task request missing virtual machine")
	}
	rec := m.c.getVM(req.VirtualMachine.ID)
	if rec == nil {
		return nil, fmt.Errorf("environment not found: %s", req.VirtualMachine.ID)
	}
	return rec, nil
}

// Stop 停掉 agent 进程（SIGINT → SIGKILL 兜底）。
func (m *taskManager) Stop(ctx context.Context, req taskflow.TaskReq) error {
	rec, err := m.taskVM(req)
	if err != nil {
		return err
	}
	m.c.stopAgent(rec)
	return nil
}

// Cancel 取消任务：SIGINT 优雅停止；若进程已不在，仅记录。
func (m *taskManager) Cancel(ctx context.Context, req taskflow.TaskReq) error {
	rec, err := m.taskVM(req)
	if err != nil {
		return err
	}
	if m.c.stopAgent(rec) {
		rec.live.Publish(&taskflow.TaskChunk{Event: "task-ended", Kind: "cancelled"})
	}
	return nil
}

// Continue 续跑：若 agent 进程已退出，用同样的 config 重新拉起。
// 若请求携带了新的任务文本（Task.Text），先把它写进 config 再重启。
func (m *taskManager) Continue(ctx context.Context, req taskflow.TaskReq) error {
	rec, err := m.taskVM(req)
	if err != nil {
		return err
	}
	rec.mu.Lock()
	running := rec.proc != nil
	if !running && req.Task != nil && req.Task.Text != "" && rec.lastReq != nil {
		rec.lastReq.Text = req.Task.Text
	}
	rec.mu.Unlock()
	if running {
		return nil
	}
	if err := m.persistTaskConfig(rec); err != nil {
		return err
	}
	return m.c.spawnAgent(ctx, rec)
}

// Restart 重启任务：停掉旧进程再拉起（req.ID est l'UUID de la tâche).
func (m *taskManager) Restart(ctx context.Context, req taskflow.RestartTaskReq) (*taskflow.RestartTaskResp, error) {
	rec := m.c.getVMByTask(req.ID.String())
	if rec == nil {
		return nil, fmt.Errorf("environment not found: %s", req.ID)
	}
	if err := m.c.spawnAgent(ctx, rec); err != nil {
		return &taskflow.RestartTaskResp{
			ID: req.ID, RequestId: req.RequestId, Success: false, Message: err.Error(),
		}, nil
	}
	return &taskflow.RestartTaskResp{
		ID: req.ID, RequestId: req.RequestId, Success: true, SessionID: req.RequestId,
	}, nil
}

// AutoApprove 记录批准状态（v1 : journalisation, pas encore transmis au moteur).
func (m *taskManager) AutoApprove(ctx context.Context, req taskflow.TaskApproveReq) error {
	m.c.logger.InfoContext(ctx, "local auto-approve (logged only)",
		"task_id", req.ID.String(), "auto_approve", req.AutoApprove)
	return nil
}

// AskUserQuestion 记录用户对 agent 提问的答复（v1 : journalisation).
func (m *taskManager) AskUserQuestion(ctx context.Context, req taskflow.AskUserQuestionResponse) error {
	m.c.logger.InfoContext(ctx, "local ask-user-question (logged only)",
		"task_id", req.TaskId, "request_id", req.RequestId, "cancelled", req.Cancelled)
	return nil
}

// ---- repo 操作（FS 本机 + git）----

// workspacePath 把请求里的相对路径解析到 workspace 内（防 traversée).
func (m *taskManager) workspacePath(rec *VM, p string) (string, error) {
	base, err := filepath.Abs(rec.workspace)
	if err != nil {
		return "", err
	}
	full, err := filepath.Abs(filepath.Join(base, p))
	if err != nil {
		return "", err
	}
	if full != base && !strings.HasPrefix(full, base+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace: %s", p)
	}
	return full, nil
}

func (m *taskManager) ListFiles(ctx context.Context, req taskflow.RepoListFilesReq) (*taskflow.RepoListFiles, error) {
	rec := m.c.getVMByTask(req.TaskId)
	if rec == nil {
		return &taskflow.RepoListFiles{TaskId: req.TaskId, RequestId: req.RequestId, Success: false, Error: strPtr("environment not found")}, nil
	}
	dir, err := m.workspacePath(rec, req.Path)
	if err != nil {
		return &taskflow.RepoListFiles{TaskId: req.TaskId, RequestId: req.RequestId, Success: false, Error: strPtr(err.Error())}, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return &taskflow.RepoListFiles{TaskId: req.TaskId, RequestId: req.RequestId, Success: false, Error: strPtr(err.Error())}, nil
	}
	files := make([]*taskflow.RepoFileInfo, 0, len(entries))
	for _, e := range entries {
		if !req.IncludeHidden && strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		mode := taskflow.RepoEntryModeFile
		if e.IsDir() {
			mode = taskflow.RepoEntryModeTree
		}
		files = append(files, &taskflow.RepoFileInfo{
			Name:       e.Name(),
			Path:       filepath.Join(req.Path, e.Name()),
			EntryMode:  mode,
			Size:       info.Size(),
			ModifiedAt: info.ModTime().Unix(),
		})
	}
	return &taskflow.RepoListFiles{TaskId: req.TaskId, RequestId: req.RequestId, Path: req.Path, Files: files, Success: true}, nil
}

func (m *taskManager) ReadFile(ctx context.Context, req taskflow.RepoReadFileReq) (*taskflow.RepoReadFile, error) {
	rec := m.c.getVMByTask(req.TaskId)
	if rec == nil {
		return &taskflow.RepoReadFile{TaskId: req.TaskId, RequestId: req.RequestId, Success: false, Error: strPtr("environment not found")}, nil
	}
	full, err := m.workspacePath(rec, req.Path)
	if err != nil {
		return &taskflow.RepoReadFile{TaskId: req.TaskId, RequestId: req.RequestId, Success: false, Error: strPtr(err.Error())}, nil
	}
	info, err := os.Stat(full)
	if err != nil {
		return &taskflow.RepoReadFile{TaskId: req.TaskId, RequestId: req.RequestId, Success: false, Error: strPtr(err.Error())}, nil
	}
	var offset, length int64
	length = info.Size()
	if req.Offset != nil {
		offset = *req.Offset
	}
	if req.Length != nil {
		length = *req.Length
	}
	if offset > info.Size() {
		offset = info.Size()
	}
	if offset+length > info.Size() {
		length = info.Size() - offset
	}
	f, err := os.Open(full)
	if err != nil {
		return &taskflow.RepoReadFile{TaskId: req.TaskId, RequestId: req.RequestId, Success: false, Error: strPtr(err.Error())}, nil
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return &taskflow.RepoReadFile{TaskId: req.TaskId, RequestId: req.RequestId, Success: false, Error: strPtr(err.Error())}, nil
	}
	buf := make([]byte, length)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return &taskflow.RepoReadFile{TaskId: req.TaskId, RequestId: req.RequestId, Success: false, Error: strPtr(err.Error())}, nil
	}
	return &taskflow.RepoReadFile{
		TaskId: req.TaskId, RequestId: req.RequestId, Path: req.Path,
		Content: buf[:n], TotalSize: info.Size(), Offset: offset, Length: int64(n),
		IsTruncated: int64(n) < length, Success: true,
	}, nil
}

func (m *taskManager) FileDiff(ctx context.Context, req taskflow.RepoFileDiffReq) (*taskflow.RepoFileDiff, error) {
	rec := m.c.getVMByTask(req.TaskId)
	if rec == nil {
		return &taskflow.RepoFileDiff{TaskId: req.TaskId, RequestId: req.RequestId, Success: false, Error: strPtr("environment not found")}, nil
	}
	args := []string{"diff", "--no-color"}
	if req.Unified != nil && *req.Unified {
		args = append(args, "--unified=3")
	}
	args = append(args, "--", req.Path)
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = rec.workspace
	out, err := cmd.Output()
	if err != nil && !isGitNoChanges(err) {
		return &taskflow.RepoFileDiff{TaskId: req.TaskId, RequestId: req.RequestId, Success: false, Error: strPtr(err.Error())}, nil
	}
	return &taskflow.RepoFileDiff{TaskId: req.TaskId, RequestId: req.RequestId, Path: req.Path, Diff: string(out), Success: true}, nil
}

func (m *taskManager) FileChanges(ctx context.Context, req taskflow.RepoFileChangesReq) (*taskflow.RepoFileChanges, error) {
	rec := m.c.getVMByTask(req.TaskId)
	if rec == nil {
		return &taskflow.RepoFileChanges{TaskId: req.TaskId, RequestId: req.RequestId, Success: false, Error: strPtr("environment not found")}, nil
	}
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = rec.workspace
	out, err := cmd.Output()
	if err != nil {
		return &taskflow.RepoFileChanges{TaskId: req.TaskId, RequestId: req.RequestId, Success: false, Error: strPtr(err.Error())}, nil
	}
	changes := make([]*taskflow.RepoFileChangeInfo, 0)
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if len(line) < 4 {
			continue
		}
		status := strings.TrimSpace(line[:2])
		path := strings.TrimSpace(line[3:])
		changes = append(changes, &taskflow.RepoFileChangeInfo{
			Path:   path,
			Status: status,
		})
	}
	// branch + commit hash courants
	branch, _ := gitOutput(rec.workspace, "rev-parse", "--abbrev-ref", "HEAD")
	commit, _ := gitOutput(rec.workspace, "rev-parse", "HEAD")
	return &taskflow.RepoFileChanges{
		TaskId: req.TaskId, RequestId: req.RequestId,
		Changes: changes, Branch: strPtr(branch), CommitHash: strPtr(commit), Success: true,
	}, nil
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func isGitNoChanges(err error) bool {
	// git diff 无变更时 exit code 1
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode() == 1
	}
	return false
}

func strPtr(s string) *string { return &s }
