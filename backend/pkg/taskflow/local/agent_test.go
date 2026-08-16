package local

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// Tests du mapping des événements NDJSON du VRAI moteur opencode
// (`opencode run --format json`) vers les TaskChunk ACP attendus par le
// frontend web. Les formes de ligne sont celles observées avec le binaire
// réel v1.18.x.

func decodeACPData(t *testing.T, chunkData []byte) map[string]any {
	t.Helper()
	// TaskChunk.Data contient le JSON brut ; le transport WebSocket l'encode
	// en base64 côté fil, et le frontend fait b64decode + JSON.parse.
	var v map[string]any
	if err := json.Unmarshal(chunkData, &v); err != nil {
		t.Fatalf("chunk data is not JSON: %v", err)
	}
	return v
}

func TestOpenCodeLineText(t *testing.T) {
	line := `{"type":"text","timestamp":1,"sessionID":"s1","part":{"type":"text","text":"Bonjour depuis le moteur !"}}`
	chunks := openCodeLineToChunks([]byte(line))
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	c := chunks[0]
	if c.Event != "task-running" || c.Kind != "acp_event" {
		t.Fatalf("unexpected chunk header: %+v", c)
	}
	payload := decodeACPData(t, c.Data)
	update, ok := payload["update"].(map[string]any)
	if !ok {
		t.Fatalf("missing update: %+v", payload)
	}
	if update["sessionUpdate"] != "agent_message_chunk" {
		t.Fatalf("unexpected sessionUpdate: %v", update["sessionUpdate"])
	}
	content := update["content"].(map[string]any)
	if content["type"] != "text" || content["text"] != "Bonjour depuis le moteur !" {
		t.Fatalf("unexpected content: %+v", content)
	}
}

func TestOpenCodeLineReasoning(t *testing.T) {
	line := `{"type":"reasoning","timestamp":1,"sessionID":"s1","part":{"type":"reasoning","text":"réfléchir…"}}`
	chunks := openCodeLineToChunks([]byte(line))
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	payload := decodeACPData(t, chunks[0].Data)
	update := payload["update"].(map[string]any)
	if update["sessionUpdate"] != "agent_thought_chunk" {
		t.Fatalf("unexpected sessionUpdate: %v", update["sessionUpdate"])
	}
}

func TestOpenCodeLineToolUse(t *testing.T) {
	// Forme réelle observée avec le binaire opencode (v1.18.x).
	line := `{"type":"tool_use","timestamp":1,"sessionID":"s1","part":{` +
		`"type":"tool","tool":"bash","callID":"call_1",` +
		`"state":{"status":"completed","input":{"command":"ls"},"output":"(no output)",` +
		`"metadata":{"exit":0},"title":"ls"}}}`
	chunks := openCodeLineToChunks([]byte(line))
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks (tool_call + update), got %d", len(chunks))
	}
	first := decodeACPData(t, chunks[0].Data)
	upd := first["update"].(map[string]any)
	if upd["sessionUpdate"] != "tool_call" {
		t.Fatalf("unexpected first sessionUpdate: %v", upd["sessionUpdate"])
	}
	if upd["toolCallId"] != "call_1" || upd["title"] != "ls" || upd["status"] != "in_progress" {
		t.Fatalf("unexpected tool call: %+v", upd)
	}
	if upd["rawInput"] == nil {
		t.Fatalf("rawInput missing: %+v", upd)
	}

	second := decodeACPData(t, chunks[1].Data)
	upd2 := second["update"].(map[string]any)
	if upd2["sessionUpdate"] != "tool_call_update" || upd2["toolCallId"] != "call_1" ||
		upd2["status"] != "completed" {
		t.Fatalf("unexpected tool update: %+v", upd2)
	}
}

func TestPortableToolEventsAreStreamedSeparately(t *testing.T) {
	startLine := `{"type":"tool_use_start","part":{"type":"tool","tool":"bash","callID":"call_stream","state":{"status":"in_progress","input":{"command":"pwd"},"title":"bash"}}}`
	startChunks := openCodeLineToChunks([]byte(startLine))
	if len(startChunks) != 1 {
		t.Fatalf("start chunks = %d, want 1", len(startChunks))
	}
	start := decodeACPData(t, startChunks[0].Data)["update"].(map[string]any)
	if start["sessionUpdate"] != "tool_call" || start["status"] != "in_progress" {
		t.Fatalf("unexpected start event: %+v", start)
	}

	stopLine := `{"type":"tool_use_stop","part":{"type":"tool","tool":"bash","callID":"call_stream","state":{"status":"completed","input":{"command":"pwd"},"output":"/workspace","title":"bash"}}}`
	stopChunks := openCodeLineToChunks([]byte(stopLine))
	if len(stopChunks) != 1 {
		t.Fatalf("stop chunks = %d, want 1", len(stopChunks))
	}
	stop := decodeACPData(t, stopChunks[0].Data)["update"].(map[string]any)
	if stop["sessionUpdate"] != "tool_call_update" || stop["status"] != "completed" || stop["rawOutput"] != "/workspace" {
		t.Fatalf("unexpected stop event: %+v", stop)
	}
}

func TestOpenCodeLineError(t *testing.T) {
	// Forme réelle : {"type":"error","error":{"name":"APIError","data":{"message":"..."}}}.
	line := `{"type":"error","timestamp":1,"sessionID":"s1","error":{"name":"APIError","data":{"message":"Cannot connect to API"}}}`
	chunks := openCodeLineToChunks([]byte(line))
	if len(chunks) != 1 || chunks[0].Event != "task-error" {
		t.Fatalf("expected task-error chunk, got %+v", chunks)
	}
	payload := decodeACPData(t, chunks[0].Data)
	errObj := payload["error"].(map[string]any)
	if errObj["message"] != "Cannot connect to API" {
		t.Fatalf("unexpected error message: %+v", errObj)
	}
	if payload["details"] != "Cannot connect to API" {
		t.Fatalf("unexpected error details: %+v", payload)
	}
}

func TestOpenCodeLineNestedProviderError(t *testing.T) {
	line := `{"type":"error","error":{"name":"APIError","data":{"error":{"message":"Fireworks rejected max_tokens"}}}}`
	chunks := openCodeLineToChunks([]byte(line))
	if len(chunks) != 1 {
		t.Fatalf("expected one task-error, got %+v", chunks)
	}
	payload := decodeACPData(t, chunks[0].Data)
	if payload["details"] != "Fireworks rejected max_tokens" {
		t.Fatalf("unexpected nested error details: %+v", payload)
	}
}

func TestOpenCodeLineIgnored(t *testing.T) {
	// step_start / step_finish / lignes non JSON → aucun chunk UI.
	for _, line := range []string{
		`{"type":"step_start","timestamp":1,"sessionID":"s1","part":{"type":"step-start"}}`,
		`{"type":"step_finish","timestamp":1,"sessionID":"s1","part":{"type":"step-finish","reason":"stop"}}`,
		`pas du json`,
	} {
		if chunks := openCodeLineToChunks([]byte(line)); len(chunks) != 0 {
			t.Fatalf("line %q should produce no chunk, got %d", line, len(chunks))
		}
	}
}

func TestOpenCodeExitMessageNamesSIGILL(t *testing.T) {
	dir := t.TempDir()
	agent, err := startOpenCodeRun("/bin/sh", []string{"-c", "kill -ILL $$"}, dir, nil, filepath.Join(dir, "engine.log"))
	if err != nil {
		t.Fatal(err)
	}
	_, waitErr := consumeOpenCodeOutput(agent, func(string) {})
	if waitErr == nil {
		t.Fatal("expected process error")
	}
	if got := openCodeExitMessage(waitErr); !strings.Contains(got, "SIGILL") || !strings.Contains(got, "CPU") {
		t.Fatalf("message = %q", got)
	}
}

func TestOpenCodeOutputIsDrainedBeforeWait(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 100; i++ {
		agent, err := startOpenCodeRun("/bin/sh", []string{"-c", `printf '%s\n' '{"type":"text","part":{"type":"text","text":"ok"}}'`}, dir, nil, filepath.Join(dir, "engine.log"))
		if err != nil {
			t.Fatal(err)
		}
		var lines []string
		scanErr, waitErr := consumeOpenCodeOutput(agent, func(line string) {
			lines = append(lines, line)
		})
		if scanErr != nil {
			t.Fatalf("iteration %d: scan error = %v", i, scanErr)
		}
		if waitErr != nil {
			t.Fatalf("iteration %d: wait error = %v", i, waitErr)
		}
		if len(lines) != 1 {
			t.Fatalf("iteration %d: got %d lines, want 1", i, len(lines))
		}
	}
}

func TestOpenCodeArgs(t *testing.T) {
	// Vérifie la construction des arguments de `opencode run` (comme dans
	// spawnAgent : sortie JSON + permissions auto + journal de diagnostic).
	args := []string{"run", "--format", "json", "--auto", "--print-logs", "--log-level", "INFO"}
	args = append(args, "--model", "nemesiscode-ai/test-model")
	args = append(args, "Crée un fichier hello.txt")
	want := `["run","--format","json","--auto","--print-logs","--log-level","INFO","--model","nemesiscode-ai/test-model","Crée un fichier hello.txt"]`
	got, _ := json.Marshal(args)
	if string(got) != want {
		t.Fatalf("args mismatch:\n got %s\nwant %s", got, want)
	}
}
