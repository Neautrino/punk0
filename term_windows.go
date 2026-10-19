//go:build windows

package main

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

func hangupOnCancel(cmd *exec.Cmd) {
	// On Windows, exec.CommandContext kills the process directly on cancel.
}

// startPTY fails on Windows until ConPTY support lands.
func startPTY(cmd *exec.Cmd) (*os.File, error) {
	return pty.Start(cmd)
}

func setPTYSize(f *os.File, cols, rows uint16) error {
	return pty.Setsize(f, &pty.Winsize{Cols: cols, Rows: rows})
}
