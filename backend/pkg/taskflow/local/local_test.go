package local

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/teteekoue/NemesisCode/backend/config"
	"github.com/teteekoue/NemesisCode/backend/pkg/taskflow"
)

func TestNewClientRestoresPersistedWorkspace(t *testing.T) {
	root := t.TempDir()
	vmID := "agent-persisted"
	workspace := filepath.Join(root, vmID)
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	taskID := uuid.New()
	req := taskflow.CreateTaskReq{ID: taskID, VMID: vmID, Text: "continue"}
	payload, _ := json.Marshal(req)
	configPath := filepath.Join(workspace, taskConfigFile)
	if err := os.WriteFile(configPath, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "opencode.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	client, err := NewClient(config.LocalTaskFlow{WorkspaceRoot: root},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	rec := client.getVM(vmID)
	if rec == nil || rec.lastReq == nil {
		t.Fatalf("persisted VM was not restored: %+v", rec)
	}
	if rec.lastReq.ID != taskID || client.getVMByTask(taskID.String()) != rec {
		t.Fatalf("restored task mapping is incorrect: %+v", rec.lastReq)
	}
	if mode := fileMode(t, configPath); mode != 0o600 {
		t.Fatalf("task config mode = %o, want 600", mode)
	}
	if mode := fileMode(t, filepath.Join(workspace, "opencode.json")); mode != 0o600 {
		t.Fatalf("opencode config mode = %o, want 600", mode)
	}
}

func TestEnsureVMRejectsTraversal(t *testing.T) {
	client, err := NewClient(config.LocalTaskFlow{WorkspaceRoot: t.TempDir()},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ensureVM("../outside"); err == nil {
		t.Fatal("ensureVM accepted path traversal")
	}
}

func TestNewClientMigratesLegacyOhMyAgentToOpenCode(t *testing.T) {
	binDir := t.TempDir()
	opencode := filepath.Join(binDir, "opencode")
	if err := os.WriteFile(opencode, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	client, err := NewClient(config.LocalTaskFlow{
		WorkspaceRoot: t.TempDir(),
		AgentBin:      "ohmyagent",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(client.cfg.AgentBin) != "opencode" {
		t.Fatalf("legacy agent resolved to %q, want opencode", client.cfg.AgentBin)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
