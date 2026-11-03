//go:build !windows

package main

import (
	"os"
	"syscall"
)

// restartSelf replaces this process with exe: same PID and terminal, and the
// listener is gone already (Go opens sockets close-on-exec). It returns only
// when the exec failed.
func restartSelf(exe string, argv []string) error {
	return syscall.Exec(exe, argv, os.Environ())
}
