//go:build !linux

package local

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"

	"github.com/teteekoue/NemesisCode/backend/pkg/taskflow"
)

// Fallback sans PTY pour les builds non-Linux. Le paquet .deb utilise toujours
// terminal_linux.go ; ce chemin conserve la possibilité de lancer le backend
// pendant le développement sur une autre plateforme.
func startTerminalProcess(
	shellBin, dir, execCommand string,
	_ taskflow.TerminalSize,
) (*terminalProcess, error) {
	args := []string{}
	if strings.TrimSpace(execCommand) != "" {
		args = []string{"-lc", execCommand}
	}
	cmd := exec.Command(shellBin, args...)
	cmd.Dir = dir
	cmd.Env = terminalEnv(dir)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("shell stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("shell stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("start shell %s: %w", shellBin, err)
	}
	return &terminalProcess{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		resize: func(uint32, uint32) error { return nil },
		terminate: func(signal syscall.Signal) error {
			if cmd.Process == nil {
				return nil
			}
			return cmd.Process.Signal(signal)
		},
	}, nil
}
