//go:build linux || darwin || freebsd || netbsd || openbsd

package ui

import (
	"os"
	"syscall"
	"unsafe"
)

func termios(fd uintptr, req uint, t *syscall.Termios) bool {
	_, _, err := syscall.Syscall6(syscall.SYS_IOCTL, fd, uintptr(req), uintptr(unsafe.Pointer(t)), 0, 0, 0)
	return err == 0
}

func checkTerminalFD(fd uintptr) bool {
	var t syscall.Termios
	return termios(fd, ioctlGetTermios, &t)
}

// NoEcho stops the terminal on f from echoing what is typed into it, so keys
// pressed while punk0 runs print nothing; restore puts the old mode back.
// Signals still work: only echo is turned off. Without a terminal it does
// nothing.
func NoEcho(f *os.File) (restore func()) {
	fd := f.Fd()
	var t syscall.Termios
	if !termios(fd, ioctlGetTermios, &t) {
		return func() {}
	}
	old := t
	t.Lflag &^= syscall.ECHO
	if !termios(fd, ioctlSetTermios, &t) {
		return func() {}
	}
	return func() { termios(fd, ioctlSetTermios, &old) }
}
