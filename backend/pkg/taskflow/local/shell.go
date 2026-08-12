package local

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/teteekoue/NemesisCode/backend/pkg/taskflow"
)

// Shell 是 Sheller 的本机实现：一个 shell 子进程，stdin/stdout 用管道桥接。
// v1 没有 PTY : resize 被忽略（见 docs/local-mode-design.md，后续可用 x/sys 升级）。
type Shell struct {
	terminalID string
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     io.ReadCloser
	done       chan struct{}
	once       sync.Once
}

var _ taskflow.Sheller = (*Shell)(nil)

func newShell(shellBin, dir, terminalID string) (*Shell, error) {
	cmd := exec.Command(shellBin)
	cmd.Dir = dir
	cmd.Env = os.Environ()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("shell stdin pipe: %w", err)
	}
	// stdout + stderr fusionnés sur le même pipe (un seul flux pour le front).
	pr, pw, err := os.Pipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("shell stdout pipe: %w", err)
	}
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = pr.Close()
		_ = pw.Close()
		return nil, fmt.Errorf("start shell %s: %w", shellBin, err)
	}
	_ = pw.Close() // le writer vit dans le child ; fermer ici évite les fuites

	return &Shell{
		terminalID: terminalID,
		cmd:        cmd,
		stdin:      stdin,
		stdout:     pr,
		done:       make(chan struct{}),
	}, nil
}

// Write 写入 shell 输入。resize 在 v1 被忽略（无 PTY）。
func (s *Shell) Write(data taskflow.TerminalData) error {
	if data.Resize != nil {
		return nil
	}
	if len(data.Data) == 0 {
		return nil
	}
	_, err := s.stdin.Write(data.Data)
	return err
}

// Stop 停掉 shell 进程（SIGINT 优雅，2s 后 SIGKILL）。
func (s *Shell) Stop() {
	s.once.Do(func() {
		close(s.done)
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Signal(os.Interrupt)
			go func() {
				time.Sleep(2 * time.Second)
				_ = s.cmd.Process.Kill()
			}()
		}
		_ = s.stdin.Close()
	})
}

// BlockRead 逐块读取 shell 输出并回调。先发一个 Connected 事件（与远端
// taskflow 语义一致），然后持续发 Data 事件，直到 shell 退出。
func (s *Shell) BlockRead(fn func(taskflow.TerminalData)) error {
	fn(taskflow.TerminalData{Connected: true})
	buf := make([]byte, 32*1024)
	for {
		select {
		case <-s.done:
			return nil
		default:
		}
		n, err := s.stdout.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			fn(taskflow.TerminalData{Data: data})
		}
		if err != nil {
			return nil // EOF
		}
	}
}
