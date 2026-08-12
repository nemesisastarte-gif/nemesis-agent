//go:build !linux

package local

import "errors"

type syscallStatfsT struct {
	Blocks uint64
	Bsize  int64
}

func statfs(path string, st *syscallStatfsT) error {
	return errors.New("statfs not supported on this platform")
}
