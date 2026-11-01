package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"px0/internal/paths"
	"px0/internal/settings"
)

// A -dev punk0's Claude tabs run `punk0 hook -dev=<dir>`: the hook process
// then logs, reads records and makes worktrees in the dev folder, never in
// the installed punk0's. The folder may hold a space.
func TestDevHookStaysInTheDevFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the hook's POSIX command line")
	}
	base := t.TempDir()
	dev := filepath.Join(base, "my checkout", devFolder)
	codeRoot := settings.Default[string]("workspace.dir")
	t.Cleanup(func() {
		paths.UseDev("")
		settings.SetDefault("workspace.dir", codeRoot)
	})
	useDevFolder(dev)

	// The hook command line, run by a shell as Claude does, with a stand-in
	// punk0 that prints the arguments it was given.
	exe := filepath.Join(base, "punk0")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeClaudeHooks(exe); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(claudeHooksPath(), filepath.Join(dev, "state")+string(filepath.Separator)) {
		t.Fatalf("hooks file at %s, outside the dev folder", claudeHooksPath())
	}
	data, err := os.ReadFile(claudeHooksPath())
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Hooks map[string][]struct {
			Hooks []struct{ Command string }
		}
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("sh", "-c", file.Hooks["Stop"][0].Hooks[0].Command).Output()
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(args) < 1 || args[0] != "hook" {
		t.Fatalf("hook command runs punk0 with %q", args)
	}

	// The hook process starts knowing nothing of dev mode.
	paths.UseDev("")
	settings.SetDefault("workspace.dir", codeRoot)
	hookDev(args[1:])
	if got, want := workbenchStateDir(), filepath.Join(dev, "state"); got != want {
		t.Errorf("hook's state folder = %s, want %s", got, want)
	}
	if got, err := codeRootPath(); err != nil || got != dev {
		t.Errorf("hook's worktree folder = %s (%v), want %s", got, err, dev)
	}
}

func TestDevFlagNeedsAFullPath(t *testing.T) {
	var d devFlag
	if err := d.Set("rel/dev"); err == nil {
		t.Fatal("-dev=rel/dev accepted")
	}
	if err := d.Set("true"); err != nil || !d.on || d.dir != "" {
		t.Fatalf("-dev alone: %+v %v", d, err)
	}
	abs := filepath.Join(t.TempDir(), "punk0-dev")
	if err := d.Set(abs + string(filepath.Separator)); err != nil || !d.on || d.dir != abs {
		t.Fatalf("-dev=%s/: %+v %v", abs, d, err)
	}
}
