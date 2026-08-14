package local

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/teteekoue/NemesisCode/backend/pkg/taskflow"
)

// ============================================================================
// Driver du VRAI moteur opencode (github.com/anomalyco/opencode, MIT).
//
// Chaque message utilisateur lance `opencode run` en mode non-interactif :
//
//	opencode run --format json --auto [--continue] --model <alias> "<texte>"
//
//   - --format json : événements NDJSON sur stdout — format validé contre le
//     binaire réel (v1.18.x) : step_start, tool_use, text, reasoning,
//     step_finish, error
//   - --auto        : auto-approuve les permissions non explicitement
//                     refusées (mode local « confiance », équivalent du
//                     permission_mode=yolo)
//   - --continue    : reprend la dernière session du répertoire de travail
//   - cwd           : workspace de la tâche
//
// La config LLM du modèle utilisateur est fournie par les fichiers générés
// par le usecase (biz/task/usecase) :
//   - ~/.config/opencode/opencode.json  (provider "nemesiscode-ai" → base_url)
//   - ~/.local/share/opencode/auth.json (api key du provider)
//   - <workspace>/opencode.json         (config projet, prioritaire — écrite
//     par le driver avec l'api_key inline pour ne dépendre d'aucun état
//     global de la machine)
// ============================================================================

// agentClient est le processus `opencode run` d'une tâche.
type agentClient struct {
	cmd     *exec.Cmd
	stdout  io.ReadCloser
	done    chan struct{}
	waitErr chan error
}

// startOpenCodeRun lance `bin args...` avec cwd = workspace. Le processus est
// placé dans son propre groupe (Setpgid) pour que stopAgent puisse tuer tout
// l'arbre (opencode lance des enfants : outils bash, LSP…).
// stderr → logPath (fichier, pas le flux de la tâche).
func startOpenCodeRun(bin string, args []string, cwd string, env []string, logPath string) (*agentClient, error) {
	cmd := exec.Command(bin, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), env...)
	// opencode détermine son répertoire de projet via la variable PWD : on la
	// force au workspace (sinon l'environnement hérité du serveur — lancé
	// depuis un autre répertoire — pointerait ailleurs que le cwd réel).
	cmd.Env = append(cmd.Env, "PWD="+cwd)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("opencode stdout pipe: %w", err)
	}
	if lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		cmd.Stderr = lf
	} else {
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		return nil, fmt.Errorf("start opencode: %w", err)
	}

	ac := &agentClient{
		cmd:     cmd,
		stdout:  stdout,
		done:    make(chan struct{}),
		waitErr: make(chan error, 1),
	}
	go func() {
		ac.waitErr <- cmd.Wait()
		close(ac.done)
	}()
	return ac, nil
}

// close tue le processus (SIGTERM au groupe, puis SIGKILL après grace).
func (ac *agentClient) close() {
	if ac.cmd != nil && ac.cmd.Process != nil {
		pgid := ac.cmd.Process.Pid
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
		go func() {
			time.Sleep(3 * time.Second)
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}()
	}
}

// ============================================================================
// Mapping opencode → TaskChunk (format ACP attendu par le frontend web).
//
// Le frontend (task-message-handler.ts) attend :
//   - {Event:"task-running", Kind:"acp_event", Data: b64({update:{sessionUpdate,
//     "agent_message_chunk", content:{type:"text", text}}})}
//   - {Event:"task-error", Data: b64({error:...})}
//   - {Event:"task-ended", Kind: status}
//
// NB : TaskChunk.Data contient le JSON BRUT ; le transport WebSocket l'encode
// en base64 et le frontend fait b64decode + JSON.parse (pas de double encodage).
// ============================================================================

func rawJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte{}
	}
	return b
}

func acpUpdate(update any) *taskflow.TaskChunk {
	return &taskflow.TaskChunk{
		Event: "task-running",
		Kind:  "acp_event",
		Data:  rawJSON(map[string]any{"update": update}),
	}
}

// openCodePart est le contenu de l'event "part" (selon le type d'event).
type openCodePart struct {
	Type   string          `json:"type"`
	Text   string          `json:"text"`
	Tool   string          `json:"tool"`
	CallID string          `json:"callID"`
	Reason string          `json:"reason"`
	State  openCodeToolState `json:"state"`
}

type openCodeToolState struct {
	Status string          `json:"status"`
	Input  json.RawMessage `json:"input"`
	Output json.RawMessage `json:"output"`
	Title  string          `json:"title"`
}

// openCodeLineToChunks convertit une ligne NDJSON de `opencode run --format
// json` en TaskChunks. Retourne nil si l'événement ne produit rien à
// l'écran (step_start, step_finish, …).
func openCodeLineToChunks(line []byte) []*taskflow.TaskChunk {
	var ev struct {
		Type string          `json:"type"`
		Part json.RawMessage `json:"part"`
		Err  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(line, &ev); err != nil || ev.Type == "" {
		return nil
	}

	switch ev.Type {
	case "text", "reasoning":
		var p openCodePart
		if err := json.Unmarshal(ev.Part, &p); err != nil || p.Text == "" {
			return nil
		}
		update := "agent_message_chunk"
		if ev.Type == "reasoning" {
			update = "agent_thought_chunk"
		}
		return []*taskflow.TaskChunk{acpUpdate(map[string]any{
			"sessionUpdate": update,
			"content":       map[string]any{"type": "text", "text": p.Text},
		})}

	case "tool_use":
		var p openCodePart
		if err := json.Unmarshal(ev.Part, &p); err != nil || p.Tool == "" {
			return nil
		}
		toolCallID := p.CallID
		if toolCallID == "" {
			toolCallID = p.Tool
		}
		title := p.State.Title
		if title == "" {
			title = p.Tool
		}
		input := decodeRaw(p.State.Input)
		output := decodeRaw(p.State.Output)
		status := p.State.Status
		if status == "" {
			status = "completed"
		}
		return []*taskflow.TaskChunk{
			acpUpdate(map[string]any{
				"sessionUpdate": "tool_call",
				"toolCallId":    toolCallID,
				"title":         title,
				"input":         input,
				"rawInput":      input,
				"status":        "in_progress",
			}),
			acpUpdate(map[string]any{
				"sessionUpdate": "tool_call_update",
				"toolCallId":    toolCallID,
				"status":        status,
				"output":        output,
				"rawOutput":     output,
			}),
		}

	case "error":
		var e struct {
			Name string `json:"name"`
			Data struct {
				Message string `json:"message"`
			} `json:"data"`
		}
		_ = json.Unmarshal(ev.Err, &e)
		msg := e.Data.Message
		if msg == "" {
			msg = e.Name
		}
		if msg == "" {
			msg = "opencode error"
		}
		return []*taskflow.TaskChunk{{
			Event: "task-error",
			Data:  rawJSON(map[string]any{"error": map[string]any{"message": msg}}),
		}}

	default:
		// step_start / step_finish / … : rien à afficher.
		return nil
	}
}

// decodeRaw décode un champ JSON arbitraire en valeur Go (string → string,
// objet → map[string]any, sinon brut).
func decodeRaw(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var v any
	if err := json.Unmarshal(raw, &v); err == nil {
		return v
	}
	return strings.TrimSpace(string(raw))
}

// streamOpenCode lit la sortie NDJSON du process et publie les chunks dans le
// LiveStream de la tâche ; à la fin du process, publie task-ended (success si
// exit 0, sinon task-error + failed) — sauf arrêt volontaire (Cancel/Stop).
func (c *Client) streamOpenCode(rec *VM, agent *agentClient) {
	scanner := bufio.NewScanner(agent.stdout)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		for _, chunk := range openCodeLineToChunks([]byte(line)) {
			rec.live.Publish(chunk)
		}
	}

	waitErr := <-agent.waitErr

	rec.mu.Lock()
	stopped := rec.stopped
	rec.agent = nil
	rec.mu.Unlock()

	if stopped {
		return // Cancel a déjà publié task-ended cancelled
	}

	if waitErr == nil {
		rec.live.Publish(&taskflow.TaskChunk{Event: "task-ended", Kind: "success"})
	} else {
		msg := "opencode failed"
		if ee, ok := waitErr.(*exec.ExitError); ok {
			msg = fmt.Sprintf("opencode exited with code %d", ee.ExitCode())
		} else {
			msg = fmt.Sprintf("opencode failed: %v", waitErr)
		}
		rec.live.Publish(&taskflow.TaskChunk{
			Event: "task-error",
			Data:  rawJSON(map[string]any{"error": map[string]any{"message": msg}}),
		})
		rec.live.Publish(&taskflow.TaskChunk{Event: "task-ended", Kind: "failed"})
	}
	c.logger.Info("local opencode run finished", "vm_id", rec.record.ID, "err", waitErr)
}
