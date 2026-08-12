package local

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/teteekoue/NemesisCode/backend/pkg/taskflow"
)

type fileManager struct{ c *Client }

var _ taskflow.FileManager = (*fileManager)(nil)

// resolve 解析 (vmID, path) 到 workspace 内绝对路径，防 traversée.
func (m *fileManager) resolve(req taskflow.FileReq) (string, error) {
	rec := m.c.getVM(req.ID)
	if rec == nil {
		return "", fmt.Errorf("environment not found: %s", req.ID)
	}
	base, err := filepath.Abs(rec.workspace)
	if err != nil {
		return "", err
	}
	full, err := filepath.Abs(filepath.Join(base, req.Path))
	if err != nil {
		return "", err
	}
	if full != base && !strings.HasPrefix(full, base+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace: %s", req.Path)
	}
	return full, nil
}

func (m *fileManager) Operate(ctx context.Context, req taskflow.FileReq) ([]*taskflow.File, error) {
	switch req.Operate {
	case taskflow.FileOpSave:
		full, err := m.resolve(req)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(full, []byte(req.Content), 0o644); err != nil {
			return nil, err
		}
		return nil, nil

	case taskflow.FileOpDelete:
		full, err := m.resolve(req)
		if err != nil {
			return nil, err
		}
		return nil, os.RemoveAll(full)

	case taskflow.FileOpCopy, taskflow.FileOpMove:
		src, err := m.resolve(taskflow.FileReq{ID: req.ID, Path: req.Source})
		if err != nil {
			return nil, err
		}
		dst, err := m.resolve(taskflow.FileReq{ID: req.ID, Path: req.Target})
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return nil, err
		}
		if req.Operate == taskflow.FileOpCopy {
			return nil, copyPath(src, dst)
		}
		return nil, os.Rename(src, dst)

	case taskflow.FileOpMkdir:
		full, err := m.resolve(req)
		if err != nil {
			return nil, err
		}
		return nil, os.MkdirAll(full, 0o755)

	case taskflow.FileOpList:
		full, err := m.resolve(req)
		if err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(full)
		if err != nil {
			return nil, err
		}
		out := make([]*taskflow.File, 0, len(entries))
		for _, e := range entries {
			info, err := e.Info()
			if err != nil {
				continue
			}
			kind := taskflow.FileKindFile
			if e.IsDir() {
				kind = taskflow.FileKindDir
			}
			out = append(out, &taskflow.File{
				Name:      e.Name(),
				Size:      uint64(info.Size()),
				Kind:      kind,
				UnixMode:  uint32(info.Mode().Perm()),
				CreatedAt: info.ModTime().Unix(),
				UpdatedAt: info.ModTime().Unix(),
			})
		}
		return out, nil

	default:
		return nil, fmt.Errorf("unsupported file operate: %s", req.Operate)
	}
}

func (m *fileManager) Download(ctx context.Context, req taskflow.FileReq, fn func(uint64, []byte) error) error {
	full, err := m.resolve(req)
	if err != nil {
		return err
	}
	f, err := os.Open(full)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}
	if err := fn(uint64(info.Size()), nil); err != nil {
		return err
	}
	buf := make([]byte, 64*1024)
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			if err := fn(uint64(info.Size()), buf[:n]); err != nil {
				return err
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

func (m *fileManager) Upload(ctx context.Context, req taskflow.FileReq, data <-chan []byte) error {
	full, err := m.resolve(req)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	f, err := os.Create(full)
	if err != nil {
		return err
	}
	defer f.Close()
	for chunk := range data {
		if _, err := f.Write(chunk); err != nil {
			return err
		}
	}
	return nil
}

// copyPath 递归复制（file 或 dir).
func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFile(src, dst, info.Mode())
	}
	if err := os.MkdirAll(dst, info.Mode()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyPath(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
