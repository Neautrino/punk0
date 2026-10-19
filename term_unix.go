//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"github.com/creack/pty"
)

// hangupOnCancel makes cancellation SIGHUP the shell's process group, like a
// terminal window closing; interactive shells pass the hangup on to their jobs.
// pty.Start makes the shell a session leader, so its pgid equals its pid.
func hangupOnCancel(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP)
	}
}

// startPTY starts cmd on a new PTY and returns a pollable master. creack/pty
// v1.1.24 ioctls through File.Fd, which leaves the master in blocking mode, so
// Close could not interrupt a Read while a stray background job keeps the PTY
// open. A dup'd non-blocking fd restores that.
func startPTY(cmd *exec.Cmd) (*os.File, error) {
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 80, Rows: 24})
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fd, err := syscall.Dup(int(f.Fd()))
	if err == nil {
		err = syscall.SetNonblock(fd, true)
	}
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return nil, err
	}
	return os.NewFile(uintptr(fd), f.Name()), nil
}

// setPTYSize is pty.Setsize without File.Fd (see startPTY).
func setPTYSize(f *os.File, cols, rows uint16) error {
	ws := pty.Winsize{Cols: cols, Rows: rows}
	return ptyIoctl(f, syscall.TIOCSWINSZ, unsafe.Pointer(&ws))
}

// ptyForeground returns the PTY's foreground process group.
func ptyForeground(f *os.File) (int, error) {
	var pgrp int32
	err := ptyIoctl(f, syscall.TIOCGPGRP, unsafe.Pointer(&pgrp))
	return int(pgrp), err
}

func ptyIoctl(f *os.File, req uintptr, arg unsafe.Pointer) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var errno syscall.Errno
	if err := rc.Control(func(fd uintptr) {
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg))
	}); err != nil {
		return err
	}
	if errno != 0 {
		return errno
	}
	return nil
}
