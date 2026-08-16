package local

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
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
	logPath string
	ignored atomic.Bool // remplacement/arrêt volontaire : ne pas publier de fin
}

// startOpenCodeRun lance `bin args...` avec cwd = workspace. Le processus est
// placé dans son propre groupe (Setpgid) pour que stopAgent puisse tuer tout
// l'arbre (opencode lance des enfants : outils bash, LSP…).
// stderr → logPath (fichier, pas le flux de la tâche).
func startOpenCodeRun(bin string, args []string, cwd string, env []string, logPath string) (*agentClient, error) {
	cmd := exec.Command(bin, args...)
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	for _, item := range env {
		if key, value, ok := strings.Cut(item, "="); ok && key != "" {
			cmd.Env = setEnv(cmd.Env, key, value)
		}
	}
	// opencode détermine son répertoire de projet via la variable PWD : on la
	// force au workspace sans conserver de doublon hérité du serveur.
	cmd.Env = setEnv(cmd.Env, "PWD", cwd)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("opencode stdout pipe: %w", err)
	}
	var logFile *os.File
	if lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600); err == nil {
		logFile = lf
		_ = os.Chmod(logPath, 0o600)
		cmd.Stderr = lf
	} else {
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		if logFile != nil {
			_ = logFile.Close()
		}
		return nil, fmt.Errorf("start opencode: %w", err)
	}
	// exec a dupliqué le descripteur pour l'enfant : le parent n'en a plus
	// besoin. Le fermer ici évite une fuite à chaque tour de conversation.
	if logFile != nil {
		_ = logFile.Close()
	}

	return &agentClient{
		cmd:     cmd,
		stdout:  stdout,
		logPath: logPath,
	}, nil
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
	Type   string            `json:"type"`
	Text   string            `json:"text"`
	Tool   string            `json:"tool"`
	CallID string            `json:"callID"`
	Reason string            `json:"reason"`
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

	case "tool_use", "tool_use_start", "tool_use_stop":
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
		start := acpUpdate(map[string]any{
			"sessionUpdate": "tool_call",
			"toolCallId":    toolCallID,
			"title":         title,
			"input":         input,
			"rawInput":      input,
			"status":        "in_progress",
		})
		status := p.State.Status
		if status == "" {
			status = "completed"
		}
		stop := acpUpdate(map[string]any{
			"sessionUpdate": "tool_call_update",
			"toolCallId":    toolCallID,
			"title":         title,
			"status":        status,
			"input":         input,
			"rawInput":      input,
			"output":        output,
			"rawOutput":     output,
		})
		switch ev.Type {
		case "tool_use_start":
			return []*taskflow.TaskChunk{start}
		case "tool_use_stop":
			return []*taskflow.TaskChunk{stop}
		default:
			return []*taskflow.TaskChunk{start, stop}
		}

	case "error":
		msg := openCodeErrorMessage(ev.Err)
		if msg == "" {
			// Certaines versions placent l'erreur dans `part` plutôt que
			// dans `error`. Accepter les deux formes rend le diagnostic
			// compatible avec les mises à jour du binaire embarqué.
			msg = openCodeErrorMessage(ev.Part)
		}
		if msg == "" {
			msg = "opencode error"
		}
		return []*taskflow.TaskChunk{taskErrorChunk(msg)}

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

// taskErrorChunk conserve l'ancien champ error.message mais fournit aussi le
// champ details que le frontend affiche. Sans ce champ, le badge était vide et
// ne proposait qu'un « reload » incapable d'expliquer une erreur fournisseur.
func taskErrorChunk(details string) *taskflow.TaskChunk {
	details = strings.TrimSpace(details)
	if details == "" {
		details = "opencode error"
	}
	if len(details) > 12_000 {
		details = details[len(details)-12_000:]
	}
	return &taskflow.TaskChunk{
		Event: "task-error",
		Data: rawJSON(map[string]any{
			"details": details,
			"message": details,
			"error":   map[string]any{"message": details},
		}),
	}
}

// openCodeErrorMessage extrait un message utile des différentes formes
// d'erreurs opencode/API observées (error.data.message, error.message,
// error.data.error.message, chaîne brute, etc.).
func openCodeErrorMessage(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return strings.TrimSpace(string(raw))
	}
	return nestedErrorMessage(value, 0)
}

func nestedErrorMessage(value any, depth int) string {
	if depth > 8 || value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		// Les champs les plus précis passent avant le nom générique de
		// l'exception (APIError, UnknownError…).
		for _, key := range []string{"details", "message", "error", "data", "cause", "body", "name"} {
			if child, ok := v[key]; ok {
				if msg := nestedErrorMessage(child, depth+1); msg != "" {
					return msg
				}
			}
		}
	case []any:
		for _, child := range v {
			if msg := nestedErrorMessage(child, depth+1); msg != "" {
				return msg
			}
		}
	}
	return ""
}

// tailLog retourne les dernières lignes du journal opencode produit par
// --print-logs. Il est joint aux erreurs de processus (crash, instruction
// illégale, configuration invalide) au lieu d'afficher seulement « exit 1 ».
func tailLog(path string, maxLines int) string {
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n")
}

func openCodeExitMessage(err error) string {
	if ee, ok := err.(*exec.ExitError); ok {
		if status, ok := ee.Sys().(syscall.WaitStatus); ok && status.Signaled() {
			signal := status.Signal()
			if signal == syscall.SIGILL {
				return "opencode a reçu SIGILL (instruction processeur illégale) : moteur incompatible avec ce CPU"
			}
			return fmt.Sprintf("opencode a été arrêté par le signal %s", signal)
		}
		return fmt.Sprintf("opencode s'est arrêté avec le code %d", ee.ExitCode())
	}
	return fmt.Sprintf("opencode a échoué: %v", err)
}

func consumeOpenCodeOutput(agent *agentClient, onLine func(string)) (scanErr, waitErr error) {
	scanner := bufio.NewScanner(agent.stdout)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			onLine(line)
		}
	}
	// Wait ferme les pipes créés par StdoutPipe. Il doit impérativement être
	// appelé après la fin de lecture ; l'ancien Wait concurrent provoquait la
	// fausse erreur « read |0: file already closed » à la sortie du moteur.
	return scanner.Err(), agent.cmd.Wait()
}

// streamOpenCode lit la sortie NDJSON du process et publie les chunks dans le
// LiveStream de la tâche ; à la fin du process, publie task-ended (success si
// exit 0, sinon task-error + failed) — sauf arrêt volontaire (Cancel/Stop).
func (c *Client) streamOpenCode(rec *VM, agent *agentClient) {
	sawEngineError := false
	scanErr, waitErr := consumeOpenCodeOutput(agent, func(line string) {
		for _, chunk := range openCodeLineToChunks([]byte(line)) {
			if chunk.Event == "task-error" {
				sawEngineError = true
			}
			rec.live.Publish(chunk)
		}
	})

	rec.mu.Lock()
	stopped := rec.stopped
	// Ne pas effacer le nouveau process si un Restart a remplacé celui-ci
	// pendant sa terminaison.
	if rec.agent == agent {
		rec.agent = nil
	}
	rec.mu.Unlock()

	if stopped || agent.ignored.Load() {
		return // Cancel/Stop/Restart gère déjà la fin ou remplace ce tour
	}

	if scanErr != nil {
		details := fmt.Sprintf("lecture de la sortie opencode impossible: %v", scanErr)
		if logTail := tailLog(agent.logPath, 30); logTail != "" {
			details += "\n\nJournal opencode:\n" + logTail
		}
		rec.live.Publish(taskErrorChunk(details))
		sawEngineError = true
	}

	if waitErr != nil && !sawEngineError {
		msg := openCodeExitMessage(waitErr)
		if logTail := tailLog(agent.logPath, 30); logTail != "" {
			msg += "\n\nJournal opencode:\n" + logTail
		}
		rec.live.Publish(taskErrorChunk(msg))
		sawEngineError = true
	}

	kind := "success"
	if sawEngineError {
		kind = "failed"
	}
	rec.live.Publish(&taskflow.TaskChunk{Event: "task-ended", Kind: kind})
	c.logger.Info("local opencode run finished", "vm_id", rec.record.ID,
		"err", waitErr, "stream_error", scanErr, "engine_error", sawEngineError)
}
