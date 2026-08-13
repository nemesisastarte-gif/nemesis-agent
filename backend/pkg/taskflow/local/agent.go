package local

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/teteekoue/NemesisCode/backend/pkg/taskflow"
)

// ============================================================================
// Client JSON-RPC du VRAI moteur ohmyagent (protocole --stdio).
//
// Le moteur est un sous-processus lancé avec `ohmyagent --stdio` ; il parle
// JSON-RPC 2.0 ligne par ligne (newline-delimited JSON) sur stdin/stdout.
// Voir desktop/src/driver/ (transport.rs / normalize.rs) pour la référence
// du protocole — le shell desktop utilise exactement ce protocole.
//
//   - handshake : le moteur émet system/ready {protocolVersion:"1.0",
//     capabilities, version, shutdownGraceMs} au démarrage
//   - requêtes (client → moteur) : {"jsonrpc":"2.0","id":N,"method":...,"params":...}
//   - notifications (moteur → client) : {"jsonrpc":"2.0","method":...,"params":...}
//     → system/ready, event/stream, permission/*, question/*, turn/stopped
//   - réponses : {"jsonrpc":"2.0","id":N,"result":...} ou {"error":{...}}
// ============================================================================

const (
	engineProtocolVersion = "1.0"
	engineReadyTimeout    = 30 * time.Second
	engineRPCTimeout      = 60 * time.Second
)

// engineEvent est une notification normalisée reçue du moteur.
type engineEvent struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// agentClient pilote un processus moteur unique (une tâche = un moteur).
type agentClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Scanner

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan json.RawMessage

	// notifications (hors system/ready) livrées ici.
	notif chan engineEvent
	// ready reçoit nil (OK) ou l'erreur de handshake.
	ready chan error

	// État moteur (lu après ready).
	engineVersion string
	caps          map[string]struct{}

	done chan struct{}
}

// startAgentProcess lance `bin args...` en --stdio et attend system/ready.
// cwd = engineDir, env OHMYAGENT_CONFIG_DIR = engineDir.
func startAgentProcess(bin string, args []string, engineDir string, env []string) (*agentClient, error) {
	cmd := exec.Command(bin, args...)
	cmd.Dir = engineDir
	cmd.Env = append(os.Environ(), env...)
	if len(args) == 0 {
		cmd.Args = append(cmd.Args, "--stdio")
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("engine stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("engine stdout pipe: %w", err)
	}
	// stderr → log moteur (fichier, pas le flux de la tâche).
	logPath := engineDir + "/ohmyagent.log"
	if lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		cmd.Stderr = lf
	} else {
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("start ohmyagent: %w", err)
	}

	ac := &agentClient{
		cmd:     cmd,
		stdin:   stdin,
		reader:  bufio.NewScanner(stdout),
		pending: make(map[int64]chan json.RawMessage),
		notif:   make(chan engineEvent, 256),
		ready:   make(chan error, 1),
		caps:    make(map[string]struct{}),
		done:    make(chan struct{}),
	}
	ac.reader.Buffer(make([]byte, 64*1024), 4*1024*1024)

	go ac.readLoop()

	// Attente du handshake system/ready.
	select {
	case err := <-ac.ready:
		if err != nil {
			ac.close()
			return nil, err
		}
		return ac, nil
	case <-time.After(engineReadyTimeout):
		ac.close()
		return nil, fmt.Errorf("ohmyagent: system/ready timeout (engine not responding)")
	}
}

// readLoop route chaque ligne : réponse RPC (id numérique, sans method) ou
// notification (method).
func (ac *agentClient) readLoop() {
	defer close(ac.done)
	for ac.reader.Scan() {
		line := strings.TrimSpace(ac.reader.Text())
		if line == "" {
			continue
		}
		var msg struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *json.RawMessage `json:"error"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if msg.Method == "" && msg.ID != 0 {
			// Réponse à une requête.
			ac.mu.Lock()
			ch, ok := ac.pending[msg.ID]
			if ok {
				delete(ac.pending, msg.ID)
			}
			ac.mu.Unlock()
			if ok {
				if msg.Error != nil {
					ch <- json.RawMessage(`{"__error__":` + string(*msg.Error) + `}`)
				} else {
					ch <- msg.Result
				}
				close(ch)
			}
			continue
		}
		switch msg.Method {
		case "system/ready":
			ac.handleReady(msg.Params)
		default:
			select {
			case ac.notif <- engineEvent{Method: msg.Method, Params: msg.Params}:
			default: // notif buffer plein → on droppe (jamais bloquer le reader)
			}
		}
	}
	// EOF : le moteur est sorti. Débloque les RPC en attente.
	ac.mu.Lock()
	for id, ch := range ac.pending {
		close(ch)
		delete(ac.pending, id)
	}
	ac.mu.Unlock()
}

func (ac *agentClient) handleReady(params json.RawMessage) {
	var p struct {
		ProtocolVersion string   `json:"protocolVersion"`
		Version         string   `json:"version"`
		Capabilities    []string `json:"capabilities"`
	}
	_ = json.Unmarshal(params, &p)
	if p.ProtocolVersion != engineProtocolVersion {
		ac.ready <- fmt.Errorf("ohmyagent protocol version mismatch: engine %q, supported %q",
			p.ProtocolVersion, engineProtocolVersion)
		return
	}
	ac.engineVersion = p.Version
	for _, c := range p.Capabilities {
		ac.caps[c] = struct{}{}
	}
	ac.ready <- nil
}

// call envoie une requête JSON-RPC et attend la réponse.
func (ac *agentClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	ac.mu.Lock()
	ac.nextID++
	id := ac.nextID
	ch := make(chan json.RawMessage, 1)
	ac.pending[id] = ch
	ac.mu.Unlock()

	req := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if _, err := ac.stdin.Write(append(b, '\n')); err != nil {
		return nil, fmt.Errorf("engine write %s: %w", method, err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-ac.done:
		return nil, fmt.Errorf("ohmyagent process exited")
	case resp := <-ch:
		if resp == nil {
			return nil, fmt.Errorf("ohmyagent: no response to %s", method)
		}
		if strings.Contains(string(resp), `"__error__"`) {
			var e struct {
				Error struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"__error__"`
			}
			_ = json.Unmarshal(resp, &e)
			if e.Error.Message != "" {
				return nil, fmt.Errorf("ohmyagent %s: %s", method, e.Error.Message)
			}
			return nil, fmt.Errorf("ohmyagent %s: rpc error", method)
		}
		return resp, nil
	}
}

// close arrête le moteur (SIGINT, puis SIGKILL après grace).
func (ac *agentClient) close() {
	if ac.cmd != nil && ac.cmd.Process != nil {
		_ = ac.cmd.Process.Signal(os.Interrupt)
		go func() {
			time.Sleep(3 * time.Second)
			_ = ac.cmd.Process.Kill()
		}()
	}
	_ = ac.stdin.Close()
}

// wait attend la fin du processus.
func (ac *agentClient) wait() error { return ac.cmd.Wait() }

// ============================================================================
// settings.json — la config LLM que le moteur ohmyagent lit dans son
// config dir (OHMYAGENT_CONFIG_DIR). Même mécanisme que le client desktop
// (desktop/src/config.rs write_ohmyagent_config) : le moteur résout le
// modèle passé à session/create par son alias dans settings.models.
// ============================================================================

// wireTypeOf mappe l'interface_type du modèle (champs backend) vers le
// wire type attendu par le moteur ohmyagent.
func wireTypeOf(interfaceType string) string {
	switch interfaceType {
	case "openai_responses":
		return "openai-responses"
	case "anthropic":
		return "anthropic"
	default:
		return "openai-chat"
	}
}

// writeEngineSettings écrit <engineDir>/settings.json avec le modèle de la
// tâche (base_url + api_key + model) — le moteur s'en sert pour appeler le
// fournisseur. Retourne une erreur si l'écriture échoue.
func writeEngineSettings(engineDir string, llm taskflow.LLM, contextLimit, outputLimit int) error {
	alias := llm.Model
	if alias == "" {
		return fmt.Errorf("engine settings: empty model id")
	}
	if contextLimit <= 0 {
		contextLimit = 200000
	}
	if outputLimit <= 0 {
		outputLimit = 32768
	}
	entry := map[string]any{
		"type":            wireTypeOf(string(llm.ApiType)),
		"model":           llm.Model,
		"base_url":        llm.BaseURL,
		"api_key":         llm.ApiKey,
		"context_window":  contextLimit,
		"supports_images": false,
		"max_output":      outputLimit,
		"thinking":        map[string]any{"enabled": true, "effort": "low"},
	}
	settings := map[string]any{
		"default_model":   alias,
		"permission_mode": "yolo",
		"models":          map[string]any{alias: entry},
	}
	b, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("engine settings marshal: %w", err)
	}
	path := filepath.Join(engineDir, "settings.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("engine settings write: %w", err)
	}
	return nil
}

// ============================================================================
// Mapping moteur → TaskChunk (format ACP attendu par le frontend web).
//
// Le frontend (task-message-handler.ts) attend :
//   - {Event:"task-running", Kind:"acp_event", Data: b64({update:{sessionUpdate,
//     "agent_message_chunk", content:{type:"text", text}}})}
//   - {Event:"task-error", Data: b64({error:...})}
//   - {Event:"task-ended", Kind: status}
// ============================================================================

// rawJSON sérialise v en JSON brut. TaskChunk.Data est un []byte : le
// transport WebSocket l'encode en base64, et le frontend (task-message-
// handler.ts) fait b64decode(chunk.data) puis JSON.parse — donc Data doit
// contenir le JSON brut, PAS du base64 (sinon double encodage).
func rawJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte{}
	}
	return b
}

// acpUpdate construit un chunk ACP.
func acpUpdate(update any) *taskflow.TaskChunk {
	return &taskflow.TaskChunk{
		Event: "task-running",
		Kind:  "acp_event",
		Data:  rawJSON(map[string]any{"update": update}),
	}
}

// engineEventToChunk convertit une notification event/stream en TaskChunk.
// Retourne nil si l'événement ne produit pas de chunk (ex. user_message).
func engineEventToChunk(ev engineEvent) []*taskflow.TaskChunk {
	if ev.Method != "event/stream" {
		return nil
	}
	var e struct {
		Type      string          `json:"type"`
		SessionID string          `json:"session_id"`
		Seq       uint64          `json:"seq"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(ev.Params, &e); err != nil || e.Type == "" {
		return nil
	}
	switch e.Type {
	case "model_delta":
		var d struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(e.Data, &d)
		if d.Text == "" {
			return nil
		}
		return []*taskflow.TaskChunk{acpUpdate(map[string]any{
			"sessionUpdate": "agent_message_chunk",
			"content":       map[string]any{"type": "text", "text": d.Text},
		})}
	case "thinking_delta":
		var d struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(e.Data, &d)
		if d.Text == "" {
			return nil
		}
		return []*taskflow.TaskChunk{acpUpdate(map[string]any{
			"sessionUpdate": "agent_thought_chunk",
			"content":       map[string]any{"type": "text", "text": d.Text},
		})}
	case "tool_call":
		var d struct {
			ToolCallID string `json:"tool_call_id"`
			Title      string `json:"title"`
			Input      any    `json:"input"`
		}
		_ = json.Unmarshal(e.Data, &d)
		if d.Title == "" {
			d.Title = "tool"
		}
		return []*taskflow.TaskChunk{acpUpdate(map[string]any{
			"sessionUpdate": "tool_call",
			"toolCallId":    d.ToolCallID,
			"title":         d.Title,
			"input":         d.Input,
			"status":        "in_progress",
		})}
	case "tool_result":
		var d struct {
			ToolCallID string `json:"tool_call_id"`
			Output     any    `json:"output"`
			Status     string `json:"status"`
		}
		_ = json.Unmarshal(e.Data, &d)
		return []*taskflow.TaskChunk{acpUpdate(map[string]any{
			"sessionUpdate": "tool_call_update",
			"toolCallId":    d.ToolCallID,
			"status":        d.Status,
			"output":        d.Output,
		})}
	case "error":
		var d struct {
			Error string `json:"error"`
			Kind  string `json:"kind"`
		}
		_ = json.Unmarshal(e.Data, &d)
		if d.Kind == "transient_retry" {
			return nil // erreur transitoire : le moteur réessaie
		}
		msg := d.Error
		if msg == "" {
			msg = "agent error"
		}
		return []*taskflow.TaskChunk{{
			Event: "task-error",
			Data:  rawJSON(map[string]any{"error": map[string]any{"message": msg}}),
		}}
	default:
		// complete / interrupted / max_turns / usage / todo_update /
		// compaction / session_summary / agent_result / model_start /
		// model_done / user_message → pas de chunk UI (turn/stopped clôture)
		return nil
	}
}
