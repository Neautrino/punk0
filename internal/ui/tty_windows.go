//go:build windows

package ui

import (
	"os"

	"golang.org/x/sys/windows"
)

func checkTerminalFD(fd uintptr) bool {
	return true
}

// NoEcho stops the console on f from echoing what is typed into it, so keys
// pressed while punk0 runs print nothing; restore puts the old mode back.
// Without a console it does nothing.
func NoEcho(f *os.File) (restore func()) {
	h := windows.Handle(f.Fd())
	var mode uint32
	if windows.GetConsoleMode(h, &mode) != nil || windows.SetConsoleMode(h, mode&^windows.ENABLE_ECHO_INPUT) != nil {
		return func() {}
	}
	return func() { windows.SetConsoleMode(h, mode) }
}
