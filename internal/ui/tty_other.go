//go:build !linux && !darwin && !freebsd && !netbsd && !openbsd && !windows

package ui

import "os"

func checkTerminalFD(fd uintptr) bool {
	return true
}

// NoEcho does nothing where punk0 cannot change the terminal's mode.
func NoEcho(f *os.File) (restore func()) { return func() {} }
