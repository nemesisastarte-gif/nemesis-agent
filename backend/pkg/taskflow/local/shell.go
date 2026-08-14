package local

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/teteekoue/NemesisCode/backend/pkg/taskflow"
)

// terminalProcess isole les détails PTY propres au système. Sous Linux (cible
// du .deb), stdin/stdout pointent vers un vrai pseudo-terminal : le shell est
// interactif, affiche son prompt, accepte Ctrl-C et reçoit les resize xterm.
type terminalProcess struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	resize    func(cols, rows uint32) error
	terminate func(syscall.Signal) error
}

// Shell est l'implémentation locale de Sheller autour d'un PTY.
type Shell struct {
	ctx        context.Context
	terminalID string
	createdAt  int64
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     io.ReadCloser
	resize     func(cols, rows uint32) error
	terminate  func(syscall.Signal) error
	done       chan struct{}
	stopOnce   sync.Once
	doneOnce   sync.Once
	closeOnce  sync.Once
	writeMu    sync.Mutex
}

var _ taskflow.Sheller = (*Shell)(nil)

func newShell(
	ctx context.Context,
	shellBin, dir, terminalID, execCommand string,
	size taskflow.TerminalSize,
) (*Shell, error) {
	process, err := startTerminalProcess(shellBin, dir, execCommand, size)
	if err != nil {
		return nil, err
	}

	s := &Shell{
		ctx:        ctx,
		terminalID: terminalID,
		createdAt:  time.Now().Unix(),
		cmd:        process.cmd,
		stdin:      process.stdin,
		stdout:     process.stdout,
		resize:     process.resize,
		terminate:  process.terminate,
		done:       make(chan struct{}),
	}

	// Récolte toujours le processus pour éviter les zombies.
	go func() {
		_ = s.cmd.Wait()
		s.finish()
	}()
	// Une déconnexion WebSocket annule ctx. Fermer le PTY débloque
	// immédiatement BlockRead, même si le shell n'écrit rien.
	go func() {
		select {
		case <-ctx.Done():
			s.Stop()
		case <-s.done:
		}
	}()
	return s, nil
}

func (s *Shell) finish() {
	s.doneOnce.Do(func() {
		close(s.done)
	})
}

func (s *Shell) closeIO() {
	s.closeOnce.Do(func() {
		_ = s.stdin.Close()
		_ = s.stdout.Close()
	})
}

// Write écrit l'entrée du terminal ou applique sa nouvelle taille.
func (s *Shell) Write(data taskflow.TerminalData) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	select {
	case <-s.done:
		return io.ErrClosedPipe
	default:
	}
	if data.Resize != nil {
		if s.resize == nil {
			return nil
		}
		return s.resize(data.Resize.Col, data.Resize.Row)
	}
	if len(data.Data) == 0 {
		return nil
	}
	_, err := s.stdin.Write(data.Data)
	return err
}

// Stop termine tout le groupe du shell, puis force l'arrêt après deux secondes.
func (s *Shell) Stop() {
	s.stopOnce.Do(func() {
		if s.terminate != nil {
			_ = s.terminate(syscall.SIGHUP)
			_ = s.terminate(syscall.SIGTERM)
		}
		// Fermer le master PTY fait sortir les lectures et signale aussi un
		// hangup au shell.
		s.finish()
		s.closeIO()
		go func() {
			time.Sleep(2 * time.Second)
			if s.terminate != nil {
				_ = s.terminate(syscall.SIGKILL)
			} else if s.cmd.Process != nil {
				_ = s.cmd.Process.Kill()
			}
		}()
	})
}

// BlockRead transmet le flux PTY au navigateur. Le message Connected est
// envoyé avant le premier prompt afin que xterm puisse immédiatement envoyer
// sa taille et recevoir l'affichage interactif.
func (s *Shell) BlockRead(fn func(taskflow.TerminalData)) error {
	fn(taskflow.TerminalData{Connected: true})
	buf := make([]byte, 32*1024)
	for {
		n, err := s.stdout.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			fn(taskflow.TerminalData{Data: data})
		}
		if err != nil {
			s.closeIO()
			select {
			case <-s.done:
				return nil
			case <-s.ctx.Done():
				return nil
			default:
				// Linux renvoie EIO sur le master PTY quand le dernier fd du
				// slave se ferme. C'est l'équivalent normal d'un EOF.
				if err == io.EOF || errors.Is(err, syscall.EIO) {
					return nil
				}
				return fmt.Errorf("read local terminal: %w", err)
			}
		}
	}
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, item := range env {
		if len(item) >= len(prefix) && item[:len(prefix)] == prefix {
			continue
		}
		out = append(out, item)
	}
	return append(out, prefix+value)
}

func terminalEnv(dir string) []string {
	env := os.Environ()
	env = setEnv(env, "PWD", dir)
	env = setEnv(env, "TERM", "xterm-256color")
	env = setEnv(env, "COLORTERM", "truecolor")
	return env
}
