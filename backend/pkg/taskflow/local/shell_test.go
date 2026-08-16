//go:build linux

package local

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/teteekoue/NemesisCode/backend/pkg/taskflow"
)

func TestLocalShellUsesPTYAndStreamsOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shell, err := newShell(ctx, "/bin/sh", t.TempDir(), "terminal-test",
		"if [ -t 0 ] && [ -t 1 ]; then printf 'PTY_OK\\n'; else printf 'NO_PTY\\n'; fi",
		taskflow.TerminalSize{Col: 100, Row: 30})
	if err != nil {
		t.Fatalf("newShell() error = %v", err)
	}
	defer shell.Stop()

	var output strings.Builder
	connected := false
	if err := shell.BlockRead(func(data taskflow.TerminalData) {
		connected = connected || data.Connected
		output.Write(data.Data)
	}); err != nil {
		t.Fatalf("BlockRead() error = %v", err)
	}
	if !connected {
		t.Fatal("terminal never emitted Connected")
	}
	if got := output.String(); !strings.Contains(got, "PTY_OK") || strings.Contains(got, "NO_PTY") {
		t.Fatalf("terminal output = %q, want PTY_OK", got)
	}
}

func TestLocalShellResizeAndInput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shell, err := newShell(ctx, "/bin/sh", t.TempDir(), "terminal-input", "",
		taskflow.TerminalSize{Col: 80, Row: 24})
	if err != nil {
		t.Fatalf("newShell() error = %v", err)
	}
	defer shell.Stop()

	if err := shell.Write(taskflow.TerminalData{Resize: &taskflow.TerminalSize{Col: 120, Row: 40}}); err != nil {
		t.Fatalf("resize error = %v", err)
	}
	if err := shell.Write(taskflow.TerminalData{Data: []byte("printf 'INPUT_OK\\n'\nexit\n")}); err != nil {
		t.Fatalf("write error = %v", err)
	}

	var output strings.Builder
	if err := shell.BlockRead(func(data taskflow.TerminalData) { output.Write(data.Data) }); err != nil {
		t.Fatalf("BlockRead() error = %v", err)
	}
	if !strings.Contains(output.String(), "INPUT_OK") {
		t.Fatalf("terminal output = %q, want INPUT_OK", output.String())
	}
}
