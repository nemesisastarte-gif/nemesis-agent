package oss

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/teteekoue/NemesisCode/backend/config"
)

// LocalStore est un stockage objet sur disque (mode local sans rustfs/S3).
// Les objets sont écrits sous <dir>/<key> ; key est un chemin relatif.
type LocalStore struct {
	Dir string
}

// NewLocal crée un client OSS « local » : les fichiers sont stockés sur le
// disque de la machine hôte. Provider "local" dans la config object_storage.
func NewLocal(cfg config.ObjectStorageConfig) (*Client, error) {
	dir := strings.TrimSpace(cfg.LocalDir)
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir: %w", err)
		}
		dir = filepath.Join(home, ".nemesiscode", "uploads")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create local storage dir: %w", err)
	}
	return &Client{
		cfg:   cfg,
		local: &LocalStore{Dir: dir},
	}, nil
}

func (s *LocalStore) pathFor(key string) string {
	return filepath.Join(s.Dir, filepath.FromSlash(key))
}

func (s *LocalStore) Put(ctx context.Context, key string, r io.Reader) error {
	p := s.pathFor(key)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, r)
	return err
}

func (s *LocalStore) Head(ctx context.Context, key string) (bool, error) {
	_, err := os.Stat(s.pathFor(key))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func (s *LocalStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	f, err := os.Open(s.pathFor(key))
	if err != nil {
		return nil, fmt.Errorf("local: get %q: %w", key, err)
	}
	return f, nil
}
