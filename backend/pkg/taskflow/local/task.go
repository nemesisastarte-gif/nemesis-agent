package local

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/teteekoue/NemesisCode/backend/pkg/taskflow"
)

const taskConfigFile = "nemesis-task.json"

type taskManager struct{ c *Client }

var _ taskflow.TaskManager = (*taskManager)(nil)

// engineDirFor retourne le répertoire privé du moteur pour une tâche :
// <workspace>/.ohmyagent (config, sessions, logs du moteur).
func engineDirFor(ws string) string {
	return filepath.Join(ws, ".ohmyagent")
}

// Create prépare le workspace puis démarre le VRAI moteur ohmyagent :
//
//	$NEMESIS_AGENT_BIN --stdio            (JSON-RPC ligne par ligne)
//	  env  OHMYAGENT_CONFIG_DIR=<workspace>/.ohmyagent
//	  cwd  <workspace>/.ohmyagent
//
// puis session/create {cwd: workspace, permission_mode, interactive, model}
// et session/sendMessage {session_id, message: req.Text}.
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
	return m.c.spawnAgent(ctx, rec, "")
}

// persistTaskConfig sérialise la config de tâche dans le workspace
// (référence pour l'utilisateur / débogage).
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

// spawnAgent démarre le vrai moteur pour la tâche du workspace rec.
// resumeSession != "" → session/create {resume: resumeSession}.
func (c *Client) spawnAgent(ctx context.Context, rec *VM, resumeSession string) error {
	c.stopAgent(rec)

	rec.mu.Lock()
	req := rec.lastReq
	rec.mu.Unlock()
	if req == nil {
		return fmt.Errorf("no task config for vm %s", rec.record.ID)
	}

	engineDir := engineDirFor(rec.workspace)
	if err := os.MkdirAll(engineDir, 0o755); err != nil {
		return fmt.Errorf("create engine dir: %w", err)
	}

	// Config LLM du moteur : settings.json (résolution par alias du modèle).
	// La base_url + api_key sont celles du modèle configuré par l'utilisateur
	// (pas de LLMProxy en mode local — le flux task.go les préserve).
	if err := writeEngineSettings(engineDir, req.LLM, 200000, 32768); err != nil {
		return fmt.Errorf("engine settings: %w", err)
	}

	bin := c.cfg.AgentBin
	if bin == "" {
		bin = "ohmyagent"
	}
	args := append([]string{}, c.cfg.AgentArgs...)
	if len(args) == 0 {
		args = []string{"--stdio"}
	}

	// NB: exec.Command (pas CommandContext) — le ctx du hook lifecycle peut
	// être annulé dès que handleProcessing rend la main. La fin de vie est
	// gérée explicitement par stopAgent.
	agent, err := startAgentProcess(bin, args, engineDir, []string{
		"OHMYAGENT_CONFIG_DIR=" + engineDir,
		"NEMESIS_TASK_ID=" + req.ID.String(),
		"NEMESIS_VM_ID=" + rec.record.ID,
		"NEMESIS_WORKSPACE=" + rec.workspace,
	})
	if err != nil {
		return err
	}

	rec.mu.Lock()
	rec.agent = agent
	rec.mu.Unlock()

	// session/create
	params := map[string]any{
		"cwd":             rec.workspace,
		"permission_mode": c.cfg.PermissionMode,
		"interactive":     true,
	}
	if c.cfg.PermissionMode == "" {
		params["permission_mode"] = "yolo" // mode local : confiance
	}
	if resumeSession != "" {
		params["resume"] = resumeSession
	}
	if req.LLM.Model != "" {
		params["model"] = req.LLM.Model
	}
	resp, err := agent.call(ctx, "session/create", params)
	if err != nil {
		agent.close()
		rec.mu.Lock()
		rec.agent = nil
		rec.mu.Unlock()
		return fmt.Errorf("session/create: %w", err)
	}
	var created struct {
		SessionID string `json:"session_id"`
	}
	_ = json.Unmarshal(resp, &created)
	if created.SessionID == "" {
		agent.close()
		rec.mu.Lock()
		rec.agent = nil
		rec.mu.Unlock()
		return fmt.Errorf("session/create: engine returned no session_id")
	}
	rec.mu.Lock()
	rec.sessionID = created.SessionID
	rec.mu.Unlock()

	c.logger.InfoContext(ctx, "local agent session created",
		"task_id", req.ID.String(), "vm_id", rec.record.ID,
		"session_id", created.SessionID, "model", req.LLM.Model)

	// Démarrer la pompe notifications (publication LiveStream) puis envoyer
	// la demande initiale.
	go c.pumpAgentNotifications(rec, agent)
	if req.Text != "" {
		if err := c.sendAgentMessage(rec, req.Text); err != nil {
			return fmt.Errorf("session/sendMessage: %w", err)
		}
	}
	return nil
}

// pumpAgentNotifications lit les notifications du moteur et les publie dans
// le LiveStream de la tâche (mapping ACP → TaskChunk).
func (c *Client) pumpAgentNotifications(rec *VM, agent *agentClient) {
	for {
		select {
		case <-agent.done:
			return
		case ev := <-agent.notif:
			switch ev.Method {
			case "event/stream":
				for _, chunk := range engineEventToChunk(ev) {
					rec.live.Publish(chunk)
				}
			case "permission/request":
				// Mode local : auto-approve (confiance) — le moteur poursuit.
				var p struct {
					RequestID string `json:"request_id"`
				}
				_ = json.Unmarshal(ev.Params, &p)
				if p.RequestID != "" {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					_, _ = agent.call(ctx, "permission/respond", map[string]any{
						"request_id": p.RequestID,
						"approved":   true,
					})
					cancel()
				}
			case "question/request":
				// Question posée à l'utilisateur : v1 → répondre "annulé"
				// (le frontend web question/answer n'est pas branché sur le
				// moteur local pour l'instant).
				var p struct {
					RequestID string `json:"request_id"`
				}
				_ = json.Unmarshal(ev.Params, &p)
				if p.RequestID != "" {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					_, _ = agent.call(ctx, "question/respond", map[string]any{
						"request_id": p.RequestID,
						"answers":    []any{},
						"cancelled":  true,
					})
					cancel()
				}
			case "turn/stopped":
				var p struct {
					SessionID  string `json:"session_id"`
					StopReason string `json:"stop_reason"`
					Error      string `json:"error"`
				}
				_ = json.Unmarshal(ev.Params, &p)
				kind := "success"
				if p.Error != "" || p.StopReason == "error" {
					kind = "failed"
				}
				rec.live.Publish(&taskflow.TaskChunk{Event: "task-ended", Kind: kind})
				c.logger.Info("local agent turn stopped",
					"session_id", p.SessionID, "stop_reason", p.StopReason)
			default:
				// permission/cancelled, question/cancelled, … : rien à publier
			}
		}
	}
}

// sendAgentMessage envoie un message utilisateur à la session active.
func (c *Client) sendAgentMessage(rec *VM, text string) error {
	rec.mu.Lock()
	agent := rec.agent
	sid := rec.sessionID
	rec.mu.Unlock()
	if agent == nil || sid == "" {
		return fmt.Errorf("no active agent session")
	}
	ctx, cancel := context.WithTimeout(context.Background(), engineRPCTimeout)
	defer cancel()
	_, err := agent.call(ctx, "session/sendMessage", map[string]any{
		"session_id": sid,
		"message":    text,
	})
	return err
}

// ---------------------------------------------------------------------------
// Contrôle des tâches
// ---------------------------------------------------------------------------

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

// Stop arrête le moteur (destroy session + SIGINT).
func (m *taskManager) Stop(ctx context.Context, req taskflow.TaskReq) error {
	rec, err := m.taskVM(req)
	if err != nil {
		return err
	}
	m.c.stopAgent(rec)
	return nil
}

// Cancel arrête le moteur et marque la fin (cancelled).
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

// Continue envoie un nouveau message à la session active ; si le moteur est
// sorti, relance la session (resume).
func (m *taskManager) Continue(ctx context.Context, req taskflow.TaskReq) error {
	rec, err := m.taskVM(req)
	if err != nil {
		return err
	}
	text := ""
	if req.Task != nil {
		// Task.Text est déjà le texte clair : le décodage du format frontend
		// (b64(JSON{content: b64(texte)})) est fait dans parseUserInputData
		// (biz/task/handler/v1/task.go) avant d'arriver ici.
		text = req.Task.Text
	}
	rec.mu.Lock()
	agent := rec.agent
	sid := rec.sessionID
	rec.mu.Unlock()

	if agent != nil && sid != "" {
		if text == "" {
			return nil
		}
		return m.c.sendAgentMessage(rec, text)
	}
	// Session morte → relancer avec resume.
	if text != "" && rec.lastReq != nil {
		rec.mu.Lock()
		rec.lastReq.Text = text
		rec.mu.Unlock()
		_ = m.persistTaskConfig(rec)
	}
	return m.c.spawnAgent(ctx, rec, sid)
}

// Restart relance le moteur (resume de la session si possible).
func (m *taskManager) Restart(ctx context.Context, req taskflow.RestartTaskReq) (*taskflow.RestartTaskResp, error) {
	rec := m.c.getVMByTask(req.ID.String())
	if rec == nil {
		return nil, fmt.Errorf("environment not found: %s", req.ID)
	}
	rec.mu.Lock()
	sid := rec.sessionID
	rec.mu.Unlock()
	if err := m.c.spawnAgent(ctx, rec, sid); err != nil {
		return &taskflow.RestartTaskResp{
			ID: req.ID, RequestId: req.RequestId, Success: false, Message: err.Error(),
		}, nil
	}
	return &taskflow.RestartTaskResp{
		ID: req.ID, RequestId: req.RequestId, Success: true, SessionID: sid,
	}, nil
}

// AutoApprove : le mode local auto-approuve (permission_mode=yolo) — no-op.
func (m *taskManager) AutoApprove(ctx context.Context, req taskflow.TaskApproveReq) error {
	return nil
}

// AskUserQuestion : réponse utilisateur aux questions du moteur.
// v1 : si la session est active, on relaie via question/respond.
func (m *taskManager) AskUserQuestion(ctx context.Context, req taskflow.AskUserQuestionResponse) error {
	rec := m.c.getVMByTask(req.TaskId)
	if rec == nil {
		return fmt.Errorf("environment not found: %s", req.TaskId)
	}
	rec.mu.Lock()
	agent := rec.agent
	rec.mu.Unlock()
	if agent == nil {
		return nil
	}
	ctx2, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := agent.call(ctx2, "question/respond", map[string]any{
		"request_id": req.RequestId,
		"answers":    parseAnswersJSON(req.AnswersJson),
		"cancelled":  req.Cancelled,
	})
	return err
}

func parseAnswersJSON(s string) []any {
	var out []any
	if s != "" {
		_ = json.Unmarshal([]byte(s), &out)
	}
	return out
}

// ---------------------------------------------------------------------------
// Opérations repo (FS local + git) — inchangées
// ---------------------------------------------------------------------------

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
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode() == 1
	}
	return false
}

func strPtr(s string) *string { return &s }
