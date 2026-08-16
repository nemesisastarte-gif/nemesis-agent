//go:build linux

package local

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/teteekoue/NemesisCode/backend/pkg/taskflow"
)

// Constantes ioctl Linux de l'API Unix98 PTY.
const (
	tioCGPTN   = uintptr(0x80045430)
	tioCSPTLCK = uintptr(0x40045431)
	tioCSWINSZ = uintptr(0x5414)
)

type linuxWinsize struct {
	Row    uint16
	Col    uint16
	Xpixel uint16
	Ypixel uint16
}

func linuxIOCtl(fd uintptr, request uintptr, value unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, uintptr(value))
	if errno != 0 {
		return errno
	}
	return nil
}

func setLinuxPTYSize(master *os.File, cols, rows uint32) error {
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}
	if cols > 65535 {
		cols = 65535
	}
	if rows > 65535 {
		rows = 65535
	}
	ws := linuxWinsize{Row: uint16(rows), Col: uint16(cols)}
	return linuxIOCtl(master.Fd(), tioCSWINSZ, unsafe.Pointer(&ws))
}

func openLinuxPTY() (master, slave *os.File, err error) {
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open /dev/ptmx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = master.Close()
		}
	}()

	unlock := int32(0)
	if err = linuxIOCtl(master.Fd(), tioCSPTLCK, unsafe.Pointer(&unlock)); err != nil {
		return nil, nil, fmt.Errorf("unlock ptmx: %w", err)
	}
	var number uint32
	if err = linuxIOCtl(master.Fd(), tioCGPTN, unsafe.Pointer(&number)); err != nil {
		return nil, nil, fmt.Errorf("get pts number: %w", err)
	}
	slavePath := filepath.Join("/dev/pts", fmt.Sprintf("%d", number))
	slave, err = os.OpenFile(slavePath, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("open %s: %w", slavePath, err)
	}
	return master, slave, nil
}

func startTerminalProcess(
	shellBin, dir, execCommand string,
	size taskflow.TerminalSize,
) (*terminalProcess, error) {
	master, slave, err := openLinuxPTY()
	if err != nil {
		return nil, err
	}
	cleanup := func() {
		_ = master.Close()
		_ = slave.Close()
	}

	args := []string{}
	if strings.TrimSpace(execCommand) != "" {
		args = []string{"-lc", execCommand}
	}
	cmd := exec.Command(shellBin, args...)
	cmd.Dir = dir
	cmd.Env = terminalEnv(dir)
	cmd.Stdin = slave
	cmd.Stdout = slave
	cmd.Stderr = slave
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid:  true,
		Setctty: true,
		// stdin devient le fd 0 dans l'enfant.
		Ctty: 0,
	}
	if err := setLinuxPTYSize(master, size.Col, size.Row); err != nil {
		cleanup()
		return nil, fmt.Errorf("resize local pty: %w", err)
	}
	if err := cmd.Start(); err != nil {
		cleanup()
		return nil, fmt.Errorf("start shell %s: %w", shellBin, err)
	}
	_ = slave.Close()

	return &terminalProcess{
		cmd:    cmd,
		stdin:  master,
		stdout: master,
		resize: func(cols, rows uint32) error {
			return setLinuxPTYSize(master, cols, rows)
		},
		terminate: func(signal syscall.Signal) error {
			if cmd.Process == nil {
				return nil
			}
			// Setsid fait du PID le PGID : tuer le groupe arrête aussi les
			// commandes lancées depuis le terminal.
			return syscall.Kill(-cmd.Process.Pid, signal)
		},
	}, nil
}
