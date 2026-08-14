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

	"github.com/teteekoue/NemesisCode/backend/pkg/taskflow"
)

const taskConfigFile = "nemesis-task.json"

type taskManager struct{ c *Client }

var _ taskflow.TaskManager = (*taskManager)(nil)

// engineDirFor retourne le répertoire privé du moteur pour une tâche :
// <workspace>/.opencode (logs du moteur opencode).
func engineDirFor(ws string) string {
	return filepath.Join(ws, ".opencode")
}

// Create prépare le workspace puis démarre le VRAI moteur opencode :
//
//	opencode run --format json --auto --model nemesiscode-ai/<model> "<texte>"
//
// (cwd = workspace, config LLM via les fichiers générés par le usecase).
func (m *taskManager) Create(ctx context.Context, req taskflow.CreateTaskReq) error {
	rec := m.c.getVM(req.VMID)
	if rec == nil {
		return fmt.Errorf("environment not found: %s", req.VMID)
	}

	rec.mu.Lock()
	rec.lastReq = &req
	rec.mu.Unlock()

	if err := m.persistTaskConfig(rec); err != nil {
		m.publishStartFailure(ctx, rec, err)
		return nil
	}
	if err := m.c.spawnAgent(ctx, rec, false); err != nil {
		m.publishStartFailure(ctx, rec, err)
		return nil
	}
	return nil
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
	// Le fichier contient la clé API du provider : il ne doit jamais être
	// lisible par les autres utilisateurs de la machine.
	if err := writeFileMode(cfgPath, b, 0o600); err != nil {
		return fmt.Errorf("write task config: %w", err)
	}
	return nil
}

// publishStartFailure transforme les erreurs de préparation/lancement en un
// vrai tour de tâche échoué. Le lifecycle conserve la tâche en mode
// interactif, le navigateur peut donc rejouer le détail et l'utilisateur peut
// corriger son modèle puis réessayer, au lieu de rester bloqué sur « reload ».
func (m *taskManager) publishStartFailure(ctx context.Context, rec *VM, err error) {
	details := fmt.Sprintf("Impossible de démarrer le moteur opencode: %v", err)
	rec.live.Publish(taskErrorChunk(details))
	rec.live.Publish(&taskflow.TaskChunk{Event: "task-ended", Kind: "failed"})
	m.c.logger.ErrorContext(ctx, "local opencode start failed",
		"vm_id", rec.record.ID, "error", err)
}

// spawnAgent lance `opencode run` pour la tâche du workspace rec.
// resume=true → `--continue` (reprend la dernière session du workspace).
func (c *Client) spawnAgent(ctx context.Context, rec *VM, resume bool) error {
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

	// Configs générées par le usecase (auth.json + opencode.json globaux,
	// règles…) — chemins ~ expandés vers le HOME de la machine locale.
	if err := materializeConfigs(req.Configs); err != nil {
		return fmt.Errorf("write agent configs: %w", err)
	}
	// Config projet <workspace>/opencode.json (prioritaire pour opencode) :
	// api_key inline → aucun état global requis, fiable dès le premier run.
	if err := writeProjectOpenCodeConfig(rec.workspace, req.LLM); err != nil {
		return fmt.Errorf("write opencode config: %w", err)
	}

	bin := c.cfg.AgentBin
	if bin == "" {
		bin = "opencode"
	}
	// --print-logs envoie le diagnostic interne vers stderr, capturé dans le
	// journal privé du workspace. Sans lui, un crash n'affichait que « exit 1 ».
	args := []string{"run", "--format", "json", "--auto", "--print-logs", "--log-level", "INFO"}
	if resume {
		args = append(args, "--continue")
	}
	if req.LLM.Model != "" {
		args = append(args, "--model", "nemesiscode-ai/"+req.LLM.Model)
	}
	if req.Text != "" {
		args = append(args, req.Text)
	}

	// NB: exec.Command (pas CommandContext) — le ctx du hook lifecycle peut
	// être annulé dès que handleProcessing rend la main. La fin de vie est
	// gérée explicitement par stopAgent / la fin naturelle du process.
	agentEnv := []string{
		"OPENCODE_DISABLE_AUTOUPDATE=1",
		"OPENCODE_DISABLE_ERROR_REPORTING=1",
		"OPENCODE_DISABLE_USAGE=1",
		"OPENCODE_DISABLE_BANNER=1",
		"NEMESIS_TASK_ID=" + req.ID.String(),
		"NEMESIS_VM_ID=" + rec.record.ID,
		"NEMESIS_WORKSPACE=" + rec.workspace,
	}
	for key, value := range req.Env {
		if key != "" && !strings.Contains(key, "=") {
			agentEnv = append(agentEnv, key+"="+value)
		}
	}
	agent, err := startOpenCodeRun(bin, args, rec.workspace, agentEnv,
		filepath.Join(engineDir, "opencode.log"))
	if err != nil {
		return err
	}

	rec.mu.Lock()
	rec.agent = agent
	rec.stopped = false
	rec.mu.Unlock()

	c.logger.InfoContext(ctx, "local opencode run started",
		"task_id", req.ID.String(), "vm_id", rec.record.ID,
		"model", req.LLM.Model, "resume", resume, "cmd", strings.Join(args, " "))

	// Streaming async : le process vit jusqu'à la fin du tour de l'agent.
	go c.streamOpenCode(rec, agent)
	return nil
}

// materializeConfigs écrit les ConfigFile générés par le usecase sur la
// machine locale (chemins "~" expandés, modes respectés).
func materializeConfigs(configs []taskflow.ConfigFile) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home dir: %w", err)
	}
	for _, cf := range configs {
		if cf.Path == "" {
			continue
		}
		p := expandConfigHome(cf.Path, home)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return fmt.Errorf("create config dir %s: %w", filepath.Dir(p), err)
		}
		mode := os.FileMode(0o644)
		if cf.Mode != nil {
			mode = os.FileMode(*cf.Mode)
		}
		// opencode ne développe pas `${HOME}` dans les chemins de son JSON et
		// les traite comme relatifs au workspace. Matérialiser la valeur absolue
		// en mode local rend rules/skills réellement accessibles.
		content := strings.ReplaceAll(cf.Content, "${HOME}", home)
		if err := writeFileMode(p, []byte(content), mode); err != nil {
			return fmt.Errorf("write config %s: %w", p, err)
		}
	}
	return nil
}

func expandConfigHome(path, home string) string {
	if path == "~" || path == "${HOME}" {
		return home
	}
	for _, prefix := range []string{"~/", "${HOME}/"} {
		if strings.HasPrefix(path, prefix) {
			return filepath.Join(home, path[len(prefix):])
		}
	}
	return path
}

func writeFileMode(path string, content []byte, mode os.FileMode) error {
	if err := os.WriteFile(path, content, mode); err != nil {
		return err
	}
	// os.WriteFile conserve les permissions d'un fichier existant. Chmod est
	// donc nécessaire pour sécuriser aussi les installations mises à niveau.
	return os.Chmod(path, mode)
}

// openCodeNpmPackage mappe l'interface_type du modèle vers le paquet npm
// openai-sdk utilisé par opencode (même logique que le usecase backend).
func openCodeNpmPackage(interfaceType string) string {
	switch interfaceType {
	case "openai_responses":
		return "@ai-sdk/openai"
	case "anthropic":
		return "@ai-sdk/anthropic"
	default:
		return "@ai-sdk/openai-compatible"
	}
}

// writeProjectOpenCodeConfig écrit <workspace>/opencode.json — la config
// « projet » qu'opencode charge automatiquement depuis son répertoire de
// travail (prioritaire sur la config globale). L'api_key est inline dans les
// options du provider : la connexion ne dépend d'aucun auth.json global.
func writeProjectOpenCodeConfig(ws string, llm taskflow.LLM) error {
	if llm.Model == "" {
		return nil
	}
	contextLimit := llm.ContextLimit
	if contextLimit <= 0 {
		contextLimit = 200000
	}
	outputLimit := llm.OutputLimit
	if outputLimit <= 0 {
		outputLimit = 32000
	}
	cfg := map[string]any{
		"provider": map[string]any{
			"nemesiscode-ai": map[string]any{
				"npm":  openCodeNpmPackage(string(llm.ApiType)),
				"name": "nemesiscode-ai",
				"options": map[string]any{
					"baseURL": llm.BaseURL,
					"apiKey":  llm.ApiKey,
				},
				"models": map[string]any{
					llm.Model: map[string]any{
						"name":  llm.Model,
						"limit": map[string]any{"context": contextLimit, "output": outputLimit},
					},
				},
			},
		},
		"model":              "nemesiscode-ai/" + llm.Model,
		"disabled_providers": []string{"openai", "opencode"},
		"permission": map[string]any{
			"doom_loop":          "allow",
			"external_directory": map[string]any{"*": "allow"},
			"read":               map[string]any{"*.env": "allow", "*.env.*": "allow"},
		},
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	// apiKey est inline : permissions strictes obligatoires.
	return writeFileMode(filepath.Join(ws, "opencode.json"), b, 0o600)
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

// Stop arrête le moteur (kill du process opencode).
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
	rec.mu.Lock()
	rec.stopped = true
	rec.mu.Unlock()
	if m.c.stopAgent(rec) {
		rec.live.Publish(&taskflow.TaskChunk{Event: "task-ended", Kind: "cancelled"})
	}
	return nil
}

// Continue envoie un nouveau message : si l'agent travaille encore, on
// refuse (opencode run est non-interactif — pas d'injection dans un process
// vivant) ; sinon on relance avec --continue (reprise de la session).
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
	if text == "" {
		return nil
	}

	rec.mu.Lock()
	agent := rec.agent
	rec.mu.Unlock()
	if agent != nil {
		return fmt.Errorf("agent is still running, cannot continue yet")
	}

	rec.mu.Lock()
	if rec.lastReq != nil {
		rec.lastReq.Text = text
	}
	rec.mu.Unlock()
	_ = m.persistTaskConfig(rec)
	return m.c.spawnAgent(ctx, rec, true)
}

// Restart réinitialise le processus local. Contrairement au moteur distant,
// opencode run n'est pas un daemon à « recharger » : le message de réparation
// est envoyé juste après par le frontend et Continue le relancera avec
// --continue. Le lancer ici dupliquerait l'ancien prompt et provoquerait deux
// tours concurrents.
func (m *taskManager) Restart(ctx context.Context, req taskflow.RestartTaskReq) (*taskflow.RestartTaskResp, error) {
	rec := m.c.getVMByTask(req.ID.String())
	if rec == nil {
		return &taskflow.RestartTaskResp{
			ID: req.ID, RequestId: req.RequestId, Success: false,
			Message: fmt.Sprintf("environment not found: %s", req.ID),
		}, nil
	}
	m.c.stopAgent(rec)
	return &taskflow.RestartTaskResp{
		ID: req.ID, RequestId: req.RequestId, Success: true,
	}, nil
}

// AutoApprove : opencode est lancé avec --auto (mode local yolo) — no-op.
func (m *taskManager) AutoApprove(ctx context.Context, req taskflow.TaskApproveReq) error {
	return nil
}

// AskUserQuestion : opencode non-interactif (--auto) ne pose pas de
// questions — no-op.
func (m *taskManager) AskUserQuestion(ctx context.Context, req taskflow.AskUserQuestionResponse) error {
	return nil
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
