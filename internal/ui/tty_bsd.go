//go:build darwin || freebsd || netbsd || openbsd

package ui

import "syscall"

const (
	ioctlGetTermios = syscall.TIOCGETA
	ioctlSetTermios = syscall.TIOCSETA
)
