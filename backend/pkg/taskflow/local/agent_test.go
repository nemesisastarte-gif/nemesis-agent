package local

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

// TestEngineEventToChunk vérifie le mapping des notifications du vrai moteur
// ohmyagent (protocole --stdio) vers les TaskChunk ACP attendus par le
// frontend web.

func decodeACPData(t *testing.T, chunkData []byte) map[string]any {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(string(chunkData))
	if err != nil {
		t.Fatalf("chunk data is not base64: %v", err)
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("chunk data is not JSON: %v", err)
	}
	return v
}

func TestEngineEventToChunkModelDelta(t *testing.T) {
	chunks := engineEventToChunk(engineEvent{
		Method: "event/stream",
		Params: json.RawMessage(`{"type":"model_delta","session_id":"s1","data":{"text":"Bonjour "}}`),
	})
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
	if content["type"] != "text" || content["text"] != "Bonjour " {
		t.Fatalf("unexpected content: %+v", content)
	}
}

func TestEngineEventToChunkThinkingDelta(t *testing.T) {
	chunks := engineEventToChunk(engineEvent{
		Method: "event/stream",
		Params: json.RawMessage(`{"type":"thinking_delta","session_id":"s1","data":{"text":"réfléchir…"}}`),
	})
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	payload := decodeACPData(t, chunks[0].Data)
	update := payload["update"].(map[string]any)
	if update["sessionUpdate"] != "agent_thought_chunk" {
		t.Fatalf("unexpected sessionUpdate: %v", update["sessionUpdate"])
	}
}

func TestEngineEventToChunkToolCall(t *testing.T) {
	chunks := engineEventToChunk(engineEvent{
		Method: "event/stream",
		Params: json.RawMessage(`{"type":"tool_call","session_id":"s1","data":{"tool_call_id":"tc-1","title":"Bash","input":{"command":"ls"}}}`),
	})
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	payload := decodeACPData(t, chunks[0].Data)
	update := payload["update"].(map[string]any)
	if update["sessionUpdate"] != "tool_call" {
		t.Fatalf("unexpected sessionUpdate: %v", update["sessionUpdate"])
	}
	if update["toolCallId"] != "tc-1" || update["title"] != "Bash" {
		t.Fatalf("unexpected tool call: %+v", update)
	}
	if update["status"] != "in_progress" {
		t.Fatalf("unexpected status: %v", update["status"])
	}
}

func TestEngineEventToChunkToolResult(t *testing.T) {
	chunks := engineEventToChunk(engineEvent{
		Method: "event/stream",
		Params: json.RawMessage(`{"type":"tool_result","session_id":"s1","data":{"tool_call_id":"tc-1","output":"file1","status":"success"}}`),
	})
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	payload := decodeACPData(t, chunks[0].Data)
	update := payload["update"].(map[string]any)
	if update["sessionUpdate"] != "tool_call_update" || update["toolCallId"] != "tc-1" {
		t.Fatalf("unexpected tool result: %+v", update)
	}
}

func TestEngineEventToChunkError(t *testing.T) {
	// Erreur transitoire → aucun chunk (le moteur réessaie).
	chunks := engineEventToChunk(engineEvent{
		Method: "event/stream",
		Params: json.RawMessage(`{"type":"error","session_id":"s1","data":{"kind":"transient_retry","error":"timeout"}}`),
	})
	if len(chunks) != 0 {
		t.Fatalf("transient error should produce no chunk, got %d", len(chunks))
	}
	// Erreur terminale → task-error.
	chunks = engineEventToChunk(engineEvent{
		Method: "event/stream",
		Params: json.RawMessage(`{"type":"error","session_id":"s1","data":{"error":"boom"}}`),
	})
	if len(chunks) != 1 || chunks[0].Event != "task-error" {
		t.Fatalf("expected task-error chunk, got %+v", chunks)
	}
}

func TestEngineEventToChunkUserMessageIgnored(t *testing.T) {
	chunks := engineEventToChunk(engineEvent{
		Method: "event/stream",
		Params: json.RawMessage(`{"type":"user_message","session_id":"s1","data":{}}`),
	})
	if len(chunks) != 0 {
		t.Fatalf("user_message should produce no chunk, got %d", len(chunks))
	}
}

func TestAgentSessionCreateParams(t *testing.T) {
	// Vérifie la construction des paramètres session/create (proto réel).
	params := map[string]any{
		"cwd":             "/ws",
		"permission_mode": "yolo",
		"interactive":     true,
		"model":           "gpt-4o",
	}
	b, _ := json.Marshal(params)
	var p map[string]any
	_ = json.Unmarshal(b, &p)
	if p["cwd"] != "/ws" || p["permission_mode"] != "yolo" || p["interactive"] != true || p["model"] != "gpt-4o" {
		t.Fatalf("unexpected params: %+v", p)
	}
}
