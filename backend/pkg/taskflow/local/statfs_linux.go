//go:build linux

package local

import "syscall"

type syscallStatfsT = syscall.Statfs_t

func statfs(path string, st *syscallStatfsT) error {
	return syscall.Statfs(path, st)
}
