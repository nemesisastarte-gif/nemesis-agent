package local

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/teteekoue/NemesisCode/backend/pkg/taskflow"
)

type vmManager struct{ c *Client }

var _ taskflow.VirtualMachiner = (*vmManager)(nil)

// Create « crée une VM » : en mode local cela revient à créer le répertoire
// de travail de la tâche sur la machine hôte (et cloner le repo le cas échéant).
func (m *vmManager) Create(ctx context.Context, req *taskflow.CreateVirtualMachineReq) (*taskflow.VirtualMachine, error) {
	vmID := req.ID
	if vmID == "" {
		vmID = "agent_" + uuid.NewString()
	}
	ws := filepath.Join(m.c.root, vmID)
	if err := os.MkdirAll(ws, 0o755); err != nil {
		return nil, fmt.Errorf("create workspace %s: %w", ws, err)
	}

	if req.Git.URL != "" {
		if err := m.cloneRepo(ctx, ws, &req.Git); err != nil {
			m.c.logger.WarnContext(ctx, "local git clone failed",
				"error", err, "url", req.Git.URL)
		}
	}

	hostname, _ := os.Hostname()
	vm := &taskflow.VirtualMachine{
		ID:            vmID,
		HostID:        m.c.hostID,
		Hostname:      hostname,
		Arch:          runtime.GOARCH,
		OS:            runtime.GOOS,
		Name:          "local-" + vmID,
		Repository:    req.Git.URL,
		Status:        taskflow.VirtualMachineStatusOnline,
		StatusMessage: "local environment on host",
		Cores:         int32(runtime.NumCPU()),
		Memory:        req.Memory,
		TTL:           taskflow.TTL{Kind: taskflow.TTLForever},
		CreatedAt:     time.Now().Unix(),
		Version:       "nemesis-local-1.0",
	}

	rec := &VM{
		record:    vm,
		userID:    req.UserID,
		workspace: ws,
		repoURL:   req.Git.URL,
		live:      NewLiveStream(),
		shells:    make(map[string]*Shell),
		ports:     make(map[string]*taskflow.PortForwardInfo),
	}

	m.c.mu.Lock()
	m.c.vms[vmID] = rec
	m.c.mu.Unlock()

	m.c.logger.InfoContext(ctx, "local vm created", "vm_id", vmID, "workspace", ws)

	// Callback vm-ready : même chemin que taskflow en cloud — transitionne les
	// tâches pending → processing, ce qui déclenche le lancement du moteur
	// (pkg/lifecycle/taskhook.go handleProcessing). Async : ne bloque pas la
	// création de la tâche.
	go m.c.notifyVMReady(vm)
	return vm, nil
}

// notifyVMReady appelle POST /internal/vm-ready (route interne sans auth,
// même contrat que le callback taskflow) pour signaler que la VM locale est
// prête. Retry : le callback part en asynchrone pendant la création de la
// tâche, alors que la machine à états (Redis) n'a pas encore enregistré la
// transition "" → pending — la première tentative peut donc échouer. Les
// appels suivants sont idempotents (ignorés une fois la tâche processing).
//
// NB : le framework web renvoie HTTP 200 même en cas d'échec métier — on
// vérifie donc aussi le corps JSON {code: ≠0}.
func (c *Client) notifyVMReady(vm *taskflow.VirtualMachine) {
	if c.internalBaseURL == "" {
		return
	}
	b, err := json.Marshal(vm)
	if err != nil {
		c.logger.Warn("vm-ready marshal failed", "error", err)
		return
	}

	const attempts = 15
	const interval = 500 * time.Millisecond
	for i := 1; i <= attempts; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			c.internalBaseURL+"/internal/vm-ready", bytes.NewReader(b))
		if err != nil {
			cancel()
			c.logger.Warn("vm-ready request build failed", "error", err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		var body []byte
		if resp != nil {
			body, _ = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
		}
		cancel()

		ok := err == nil && resp != nil && resp.StatusCode < 300
		if ok {
			// Échec métier : le framework répond 200 avec {code:≠0}.
			var r struct {
				Code int `json:"code"`
			}
			if json.Unmarshal(body, &r) == nil && r.Code != 0 {
				ok = false
			}
		}
		if ok {
			c.logger.Info("vm-ready callback ok", "vm_id", vm.ID, "attempt", i)
			return
		}
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		c.logger.Warn("vm-ready callback failed", "vm_id", vm.ID,
			"status", status, "body", strings.TrimSpace(string(body)), "attempt", i)
		if i < attempts {
			time.Sleep(interval)
		}
	}
	c.logger.Error("vm-ready callback gave up", "vm_id", vm.ID, "attempts", attempts)
}

// cloneRepo clone le repo (avec éventuellement le token) dans le workspace.
// On clone dans le répertoire courant pour que le workspace soit la racine du repo.
func (m *vmManager) cloneRepo(ctx context.Context, ws string, git *taskflow.Git) error {
	url := git.URL
	if git.Token != "" {
		url = injectToken(url, git.Token)
	}
	args := []string{"clone", "--quiet"}
	if git.Branch != "" {
		args = append(args, "--branch", git.Branch)
	}
	args = append(args, url, ".")

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = ws
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone: %w: %s", err, truncateString(string(out), 500))
	}
	if git.Username != "" || git.Email != "" {
		cfg := exec.CommandContext(ctx, "git", "config", "user.name", git.Username)
		cfg.Dir = ws
		_ = cfg.Run()
		cfg = exec.CommandContext(ctx, "git", "config", "user.email", git.Email)
		cfg.Dir = ws
		_ = cfg.Run()
	}
	return nil
}

// injectToken insère le token dans l'URL https:// (ou http://) pour l'authentification.
func injectToken(raw, token string) string {
	idx := strings.Index(raw, "://")
	if idx < 0 {
		return raw
	}
	return raw[:idx+3] + token + "@" + raw[idx+3:]
}

// Delete supprime la VM locale : arrête l'agent, ferme les terminaux et
// supprime le workspace (sauf keep_workspace_on_delete).
func (m *vmManager) Delete(ctx context.Context, req *taskflow.DeleteVirtualMachineReq) error {
	m.c.mu.Lock()
	rec := m.c.vms[req.ID]
	if rec != nil {
		delete(m.c.vms, req.ID)
	}
	m.c.mu.Unlock()
	if rec == nil {
		return nil
	}

	m.c.stopAgent(rec)

	rec.mu.Lock()
	for id, s := range rec.shells {
		s.Stop()
		delete(rec.shells, id)
	}
	rec.mu.Unlock()

	if !m.c.cfg.KeepWorkspaceOnDelete {
		if err := os.RemoveAll(rec.workspace); err != nil {
			m.c.logger.WarnContext(ctx, "remove workspace failed", "error", err, "workspace", rec.workspace)
		}
	}
	m.c.logger.InfoContext(ctx, "local vm deleted", "vm_id", req.ID)
	return nil
}

func (m *vmManager) Hibernate(ctx context.Context, req *taskflow.HibernateVirtualMachineReq) error {
	rec := m.c.getVM(req.ID)
	if rec == nil {
		return fmt.Errorf("environment not found: %s", req.ID)
	}
	rec.mu.Lock()
	rec.record.Status = taskflow.VirtualMachineStatusHibernated
	rec.record.StatusMessage = "hibernated (local marker only)"
	rec.mu.Unlock()
	return nil
}

func (m *vmManager) Resume(ctx context.Context, req *taskflow.ResumeVirtualMachineReq) error {
	rec := m.c.getVM(req.ID)
	if rec == nil {
		return fmt.Errorf("environment not found: %s", req.ID)
	}
	rec.mu.Lock()
	rec.record.Status = taskflow.VirtualMachineStatusOnline
	rec.record.StatusMessage = "local environment on host"
	rec.mu.Unlock()
	return nil
}

func (m *vmManager) List(ctx context.Context, id string) ([]*taskflow.VirtualMachine, error) {
	m.c.mu.Lock()
	defer m.c.mu.Unlock()
	out := make([]*taskflow.VirtualMachine, 0, len(m.c.vms))
	for _, rec := range m.c.vms {
		rec.mu.Lock()
		out = append(out, cloneVM(rec.record))
		rec.mu.Unlock()
	}
	return out, nil
}

func (m *vmManager) Info(ctx context.Context, req taskflow.VirtualMachineInfoReq) (*taskflow.VirtualMachine, error) {
	rec := m.c.getVM(req.ID)
	if rec == nil {
		return &taskflow.VirtualMachine{}, fmt.Errorf("environment not found: %s", req.ID)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return cloneVM(rec.record), nil
}

func (m *vmManager) IsOnline(ctx context.Context, req *taskflow.IsOnlineReq[string]) (*taskflow.IsOnlineResp, error) {
	m.c.mu.Lock()
	defer m.c.mu.Unlock()
	online := make(map[string]bool, len(req.IDs))
	for _, id := range req.IDs {
		rec, ok := m.c.vms[id]
		if !ok {
			online[id] = false
			continue
		}
		rec.mu.Lock()
		online[id] = rec.record.Status == taskflow.VirtualMachineStatusOnline
		rec.mu.Unlock()
	}
	return &taskflow.IsOnlineResp{OnlineMap: online}, nil
}

// Terminal ouvre un shell local dans le workspace de la VM.
// Le ctx est conservé dans le Shell pour que BlockRead se débloque à la
// déconnexion (sinon le handler ws resterait bloqué et le process jamais
// stoppé — voir docs/local-mode-design.md).
func (m *vmManager) Terminal(ctx context.Context, req *taskflow.TerminalReq) (taskflow.Sheller, error) {
	rec := m.c.getVM(req.ID)
	if rec == nil {
		return nil, fmt.Errorf("environment not found: %s", req.ID)
	}
	shell, err := newShell(ctx, m.c.shell, rec.workspace, req.TerminalID)
	if err != nil {
		return nil, err
	}
	rec.mu.Lock()
	rec.shells[req.TerminalID] = shell
	rec.mu.Unlock()
	m.c.logger.InfoContext(ctx, "local terminal opened", "vm_id", req.ID, "terminal_id", req.TerminalID)
	return shell, nil
}

// Reports abonne aux rapports. En mode local : reporter vide (aucun rapport).
func (m *vmManager) Reports(ctx context.Context, req taskflow.ReportSubscribeReq) (taskflow.Reporter, error) {
	return newEmptyReporter(), nil
}

func (m *vmManager) TerminalList(ctx context.Context, id string) ([]*taskflow.Terminal, error) {
	rec := m.c.getVM(id)
	if rec == nil {
		return nil, fmt.Errorf("environment not found: %s", id)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	out := make([]*taskflow.Terminal, 0, len(rec.shells))
	for tid := range rec.shells {
		out = append(out, &taskflow.Terminal{ID: tid, Title: tid, CreatedAt: time.Now().Unix()})
	}
	return out, nil
}

func (m *vmManager) CloseTerminal(ctx context.Context, req *taskflow.CloseTerminalReq) error {
	rec := m.c.getVM(req.ID)
	if rec == nil {
		return nil
	}
	rec.mu.Lock()
	s := rec.shells[req.TerminalID]
	delete(rec.shells, req.TerminalID)
	rec.mu.Unlock()
	if s != nil {
		s.Stop()
	}
	return nil
}

func cloneVM(v *taskflow.VirtualMachine) *taskflow.VirtualMachine {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}

// ---- empty reporter ----

type emptyReporter struct {
	done chan struct{}
}

func newEmptyReporter() *emptyReporter {
	return &emptyReporter{done: make(chan struct{})}
}

func (r *emptyReporter) Stop() {
	select {
	case <-r.done:
	default:
		close(r.done)
	}
}

func (r *emptyReporter) BlockRead(fn func(taskflow.ReportEntry)) error {
	<-r.done
	return nil
}
