//go:build !windows

package main

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"github.com/creack/pty"
)

// defaultShell is $SHELL, else /bin/sh.
func defaultShell() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	return "/bin/sh"
}

// unixTTY is a shell on a PTY. Hanging up SIGHUPs the shell's process group,
// like a terminal window closing; interactive shells pass the hangup on to
// their jobs. pty.Start makes the shell a session leader, so its pgid equals
// its pid.
type unixTTY struct {
	cmd    *exec.Cmd
	f      *os.File
	hangup context.CancelFunc
}

func startTTY(argv []string, dir string, env []string) (tty, error) {
	ctx, hangup := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP)
	}
	cmd.WaitDelay = 2 * time.Second // then Wait SIGKILLs a shell that ignored the hangup
	f, err := startPTY(cmd)
	if err != nil {
		hangup()
		return nil, err
	}
	// Hanging up closes the PTY too, which ends a Read even if a stray
	// background job still holds the PTY open.
	context.AfterFunc(ctx, func() { f.Close() })
	return &unixTTY{cmd: cmd, f: f, hangup: hangup}, nil
}

func (t *unixTTY) Read(p []byte) (int, error)  { return t.f.Read(p) }
func (t *unixTTY) Write(p []byte) (int, error) { return t.f.Write(p) }
func (t *unixTTY) Pid() int                    { return t.cmd.Process.Pid }
func (t *unixTTY) Wait()                       { t.cmd.Wait() }
func (t *unixTTY) Hangup()                     { t.hangup() }

// Resize is pty.Setsize without File.Fd (see startPTY).
func (t *unixTTY) Resize(cols, rows uint16) error {
	ws := pty.Winsize{Cols: cols, Rows: rows}
	return ptyIoctl(t.f, syscall.TIOCSWINSZ, unsafe.Pointer(&ws))
}

// Foreground compares the process group the terminal sends keystrokes to
// with the shell's own: they differ while a job runs in front of it.
func (t *unixTTY) Foreground() bool {
	var pgrp int32
	if err := ptyIoctl(t.f, syscall.TIOCGPGRP, unsafe.Pointer(&pgrp)); err != nil {
		return false
	}
	return int(pgrp) == t.cmd.Process.Pid
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

// setProcessGroup starts cmd in its own process group and makes cancellation
// kill the whole group, so a git clone's helper processes go with it.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil && cmd.Process.Pid > 0 {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
}
