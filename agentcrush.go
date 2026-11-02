package main

// Crush (charm.land/crush) reports through hooks in its global config. A run
// reads that config from CRUSH_GLOBAL_CONFIG in place of the user's folder, so
// punk0 points one run at a folder of its own that holds the user's config
// with punk0's hooks added (spec: crush fold-in).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// crushHookEvents are the crush events punk0 registers: the chat's id rides
// on every one, and these say what the tab's status dot shows (crushEvents).
var crushHookEvents = []string{
	"SessionStart", "SessionEnd", "TurnStart", "TurnEnd", "StopFailure", "Interrupt",
	"PreToolUse", "PostToolUse", "PermissionRequest", "PermissionResult",
}

// crushEvents names crush's events the way punk0 reads Claude's (hookStatus).
// A permission's result is working whatever it was: a denial ends the turn
// a moment later with TurnEnd.
var crushEvents = map[string]string{
	"TurnStart":        "UserPromptSubmit",
	"TurnEnd":          "Stop",
	"Interrupt":        "Stop",
	"PermissionResult": "PostToolUse",
}

// crushUserDir is the global config folder crush reads when punk0 does not
// point it elsewhere.
func crushUserDir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "crush")
}

// crushConfig reports through the global config folder one crush run is
// pointed at. It is rebuilt at every punk0 start from the user's folder: their
// crush.json with punk0's hooks appended (unknown keys kept as they are), and
// a crushrc that sources theirs. A crush.json of theirs that does not parse
// fails setup, so crush runs with the user's own config and no reporting
// rather than with punk0's and none of theirs.
type crushConfig struct{}

func (v crushConfig) setup(exe, kind string) (argv, env []string, err error) {
	dir, user := reportDir(kind), crushUserDir()
	cfg := map[string]json.RawMessage{}
	if b, err := os.ReadFile(filepath.Join(user, "crush.json")); err == nil {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", filepath.Join(user, "crush.json"), err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	hooks := map[string][]json.RawMessage{}
	if raw, ok := cfg["hooks"]; ok {
		if err := json.Unmarshal(raw, &hooks); err != nil {
			return nil, nil, fmt.Errorf("%s: hooks: %w", filepath.Join(user, "crush.json"), err)
		}
	}
	entry, _ := json.Marshal(struct {
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
	}{hookLine(exe, kind), 10})
	for _, ev := range crushHookEvents {
		hooks[ev] = append(hooks[ev], entry)
	}
	if cfg["hooks"], err = json.Marshal(hooks); err != nil {
		return nil, nil, err
	}
	if err := writeFileAtomic(filepath.Join(dir, "crush.json"), cfg); err != nil {
		return nil, nil, err
	}
	rc, userRC := filepath.Join(dir, "crushrc"), filepath.Join(user, "crushrc")
	if _, err := os.Stat(userRC); err == nil {
		if err := writeBytesAtomic(rc, []byte("source "+shellQuote(userRC)+"\n")); err != nil {
			return nil, nil, err
		}
	} else if err := os.Remove(rc); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	argv, env = v.unhooked(kind)
	return argv, env, nil
}

func (crushConfig) unhooked(kind string) (argv, env []string) {
	return nil, []string{"CRUSH_GLOBAL_CONFIG=" + reportDir(kind)}
}

// crushTitle asks crush for a chat's title: crush keeps its chats per project,
// so it lists the chats of the folder the event names, and the chat is the one
// whose uuid is the event's session. Crush names a chat during its first turn,
// so only a turn's end asks; "Untitled Session" is crush's name for one it has
// not named yet. Crush makes <folder>/.crush where it runs: a real event names
// the folder crush already runs in, so only a forged cwd could put one
// elsewhere, and a relative one is refused. An id that is not a session's
// (validSession) never starts crush; a crush that hangs costs 2 s.
type crushTitle struct{}

func (crushTitle) title(_ *chatHistory, ev hookEvent) string {
	if ev.Name != "Stop" || !validSession.MatchString(ev.Session) || !filepath.IsAbs(ev.Cwd) {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "crush", "session", "list", "--json")
	cmd.Dir = ev.Cwd
	cmd.WaitDelay = 100 * time.Millisecond
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	var chats []struct {
		UUID  string `json:"uuid"`
		Title string `json:"title"`
	}
	if json.Unmarshal(out, &chats) != nil {
		return ""
	}
	for _, c := range chats {
		if c.UUID == ev.Session && c.Title != "Untitled Session" {
			return c.Title
		}
	}
	return ""
}
