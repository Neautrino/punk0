//go:build windows

package main

import (
	"os"
	"os/exec"
)

// restartSelf starts exe on this console and exits: Windows has no exec. The
// listener is closed by then, so the new punk0 can bind the same port. It
// returns only when the start failed.
func restartSelf(exe string, argv []string) error {
	cmd := exec.Command(exe, argv[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
