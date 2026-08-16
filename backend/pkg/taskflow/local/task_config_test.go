package local

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/teteekoue/NemesisCode/backend/pkg/taskflow"
)

func TestExpandConfigHome(t *testing.T) {
	home := "/home/tester"
	cases := map[string]string{
		"~":                          home,
		"~/config/opencode.json":     "/home/tester/config/opencode.json",
		"${HOME}":                    home,
		"${HOME}/.codingmatrix/rule": "/home/tester/.codingmatrix/rule",
		"relative/file":              "relative/file",
	}
	for input, want := range cases {
		if got := expandConfigHome(input, home); got != want {
			t.Errorf("expandConfigHome(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestWriteProjectOpenCodeConfigUsesRuntimeLimits(t *testing.T) {
	workspace := t.TempDir()
	err := writeProjectOpenCodeConfig(workspace, taskflow.LLM{
		ApiKey:       "secret",
		BaseURL:      "https://api.fireworks.ai/inference/v1",
		Model:        "accounts/fireworks/models/test",
		ApiType:      "openai_chat",
		ContextLimit: 131072,
		OutputLimit:  8192,
	})
	if err != nil {
		t.Fatalf("writeProjectOpenCodeConfig() error = %v", err)
	}
	path := filepath.Join(workspace, "opencode.json")
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(payload, &config); err != nil {
		t.Fatal(err)
	}
	provider := config["provider"].(map[string]any)["nemesiscode-ai"].(map[string]any)
	model := provider["models"].(map[string]any)["accounts/fireworks/models/test"].(map[string]any)
	limit := model["limit"].(map[string]any)
	if limit["context"] != float64(131072) || limit["output"] != float64(8192) {
		t.Fatalf("project model limit = %+v", limit)
	}
	if mode := fileMode(t, path); mode != 0o600 {
		t.Fatalf("opencode config mode = %o, want 600", mode)
	}
}
