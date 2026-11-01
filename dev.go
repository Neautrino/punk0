package main

// A dev punk0 runs beside the installed one: its settings, records, instance
// lock, logs, Claude hooks, worktrees and scratch chats all go in one folder
// of its own (devLayout), it listens on devPort unless -port says otherwise,
// and it never updates itself. `make dev` builds one (devBuild), so
// `./punk0 <dir>` from the checkout is dev with no flag; -dev makes any
// build one for a run. The folders are switched inside punk0 rather than
// through XDG variables, which every tab's shell would inherit and pass on
// to git, gh and the rest.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"px0/internal/paths"
	"px0/internal/settings"
)

// devPort is a -dev punk0's port: another origin than the installed one's
// 7777, so the two pages keep apart in the browser too.
const devPort = 7800

// devBuild is set by `make dev` (-ldflags -X main.devBuild=1): this binary
// is a dev punk0 by default. Release builds leave it empty; -dev=false still
// turns it off for a run.
var devBuild string

// devFolder is the dev folder's name beside the executable.
const devFolder = ".punk0-dev"

// devFlag is -dev. Alone it puts the dev folder beside the executable, which
// is the checkout for a punk0 built there; -dev=<full path> puts it anywhere.
type devFlag struct {
	on  bool
	dir string
}

func (d *devFlag) String() string {
	if d == nil {
		return ""
	}
	return d.dir
}

func (d *devFlag) IsBoolFlag() bool { return true }

func (d *devFlag) Set(s string) error {
	switch s {
	case "true":
		d.on = true
	case "false":
		d.on, d.dir = false, ""
	default:
		if !filepath.IsAbs(s) {
			return errors.New("needs a full path, as in -dev=/home/me/punk0-dev")
		}
		d.on, d.dir = true, filepath.Clean(s)
	}
	return nil
}

// devDir is where -dev keeps everything: the folder given, else devFolder
// beside the executable. `go run` builds into a temporary folder that is
// gone by the next run, so there the folder has to be given.
func devDir(d *devFlag) (string, error) {
	if d.dir != "" {
		return d.dir, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	if strings.Contains(exe, string(filepath.Separator)+"go-build") {
		return "", errors.New("-dev: this punk0 runs from go run's temporary folder; build it (make build) or pass -dev=<full path>")
	}
	return filepath.Join(filepath.Dir(exe), devFolder), nil
}

// useDevFolder points punk0 into dir (devLayout): settings and state below
// it, and new worktrees and scratch chats in it while workspace.dir is
// unset. The children and `punk0 hook` are handed the same -dev=<dir>.
//
// devLayout:
//
//	<dir>/config/settings.json
//	<dir>/state/   records.json, workbench.lock, logs/, claude-hooks.json, ...
//	<dir>/workspaces/, <dir>/scratch/
func useDevFolder(dir string) {
	paths.UseDev(dir)
	settings.SetDefault("workspace.dir", dir)
}

// devArg is the -dev flag a punk0 started by this one needs, "" outside dev
// mode.
func devArg() string {
	if d := paths.Dev(); d != "" {
		return "-dev=" + d
	}
	return ""
}

// hookDev applies the -dev=<dir> writeClaudeHooks puts after `punk0 hook`.
// The hook takes no other flags and must stay quiet, so it is read by hand.
func hookDev(args []string) {
	for _, a := range args {
		if dir, ok := strings.CutPrefix(a, "-dev="); ok && filepath.IsAbs(dir) {
			useDevFolder(filepath.Clean(dir))
		}
	}
}
