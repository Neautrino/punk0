//go:build !windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// crushRun writes punk0's startup files with the user's crush folder holding
// files (name -> body), and returns the folder crush runs with.
func crushRun(t *testing.T, files map[string]string) string {
	t.Helper()
	useSettings(t, map[string]any{})
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Cleanup(func() {
		reportOff.mu.Lock()
		reportOff.kind = nil
		reportOff.mu.Unlock()
	})
	user := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "crush")
	for name, body := range files {
		if err := os.MkdirAll(user, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(user, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeAgentShell(filepath.Join(t.TempDir(), "punk0")); err != nil {
		t.Fatal(err)
	}
	return reportDir("crush")
}

// Crush reads its global config from punk0's folder for a run, in place of
// the user's: what the user set there must still apply, beside punk0's hooks.
func TestCrushKeepsTheUsersOwnConfig(t *testing.T) {
	dir := crushRun(t, map[string]string{
		"crush.json": `{"options":{"debug":true},"hooks":{"TurnEnd":[{"command":"notify-send done"}]}}`,
		"crushrc":    `hook add TurnStart --command "echo mine" --name mine`,
	})

	b, err := os.ReadFile(filepath.Join(dir, "crush.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Options map[string]any `json:"options"`
		Hooks   map[string][]struct {
			Command string `json:"command"`
			Timeout int    `json:"timeout"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("crush.json: %v\n%s", err, b)
	}
	if cfg.Options["debug"] != true {
		t.Errorf("the user's options were lost: %s", b)
	}
	ends := cfg.Hooks["TurnEnd"]
	if len(ends) != 2 || ends[0].Command != "notify-send done" || !strings.Contains(ends[1].Command, " hook crush") || ends[1].Timeout != 10 {
		t.Errorf("TurnEnd hooks: want the user's then punk0's, got %+v", ends)
	}
	for _, ev := range []string{"SessionStart", "SessionEnd", "TurnStart", "StopFailure", "Interrupt", "PreToolUse", "PostToolUse", "PermissionRequest", "PermissionResult"} {
		if len(cfg.Hooks[ev]) != 1 {
			t.Errorf("%s: want punk0's hook alone, got %+v", ev, cfg.Hooks[ev])
		}
	}

	rc, err := os.ReadFile(filepath.Join(dir, "crushrc"))
	if err != nil {
		t.Fatal(err)
	}
	want := "source " + shellQuote(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "crush", "crushrc"))
	if !strings.Contains(string(rc), want) {
		t.Errorf("crushrc does not source the user's:\n%s", rc)
	}
}

// A user crush.json that crush itself would refuse leaves crush unhooked this
// run: a config punk0 could not fold the user's into would drop it silently.
// The user's rc is gone from a later run that has none.
func TestCrushRunsUnhookedOnTheUsersBadConfig(t *testing.T) {
	crushRun(t, map[string]string{"crush.json": `{"options":`})
	if agentReportOf("crush") != nil {
		t.Fatal("crush reports through a config that dropped the user's")
	}
	sh, err := os.ReadFile(filepath.Join(agentShellDir(), "agents.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(sh), "CRUSH_GLOBAL_CONFIG") {
		t.Fatalf("the crush function still points at punk0's folder:\n%s", sh)
	}

	dir := crushRun(t, map[string]string{"crushrc": "true"})
	os.Remove(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "crush", "crushrc"))
	if err := writeAgentShell(filepath.Join(t.TempDir(), "punk0")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "crushrc")); !os.IsNotExist(err) {
		t.Fatalf("crushrc sources a file the user removed: %v", err)
	}
}

// Crush's title comes from crush itself, asked in the chat's own folder once
// a turn ended. A crush that has not named the chat yet, a chat crush does
// not list there, a turn that only started, an id a hook could forge, and a
// crush that hangs all leave the tab as it was.
func TestCrushTitleComesFromCrush(t *testing.T) {
	bin, project, other, hung := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	const named, untitled, unlisted = "ec14c082-ecbb-474e-92bf-1d229b584cec", "f103b48e-afd2-4ae7-ac35-b18bf4f38433", "740e079b-104b-4f12-bdd3-5e9b64ea33fd"
	ran := filepath.Join(bin, "ran")
	os.WriteFile(filepath.Join(project, "marker"), nil, 0o644)
	os.WriteFile(filepath.Join(hung, "hang"), nil, 0o644)
	// Chats are per project: each folder lists its own (crush session list --json).
	writeExec(t, filepath.Join(bin, "crush"), `#!/bin/sh
echo "$*" >> `+ran+`
[ "$*" = "session list --json" ] || exit 2
[ -f hang ] && sleep 30
if [ -f marker ]; then
  echo '[{"id":"75bb0b811fb73599","uuid":"`+named+`","title":"Spike Hello Title","created":"2026-11-01T20:56:37Z","modified":"2026-11-01T20:57:11Z"},'
  echo ' {"id":"aa11","uuid":"`+untitled+`","title":"Untitled Session","created":"2026-11-01T20:58:00Z","modified":"2026-11-01T20:58:00Z"},'
  echo ' {"id":"bb22","uuid":"230426e0-1780-4772-b14c-54b081446f07","title":"Another chat","created":"2026-11-01T20:55:00Z","modified":"2026-11-01T20:55:30Z"}]'
else
  echo '[{"id":"cc33","uuid":"0b0b0b0b-0000-4000-8000-000000000000","title":"Someone else","created":"2026-11-01T20:00:00Z","modified":"2026-11-01T20:00:00Z"}]'
fi
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	stop := func(session, cwd string) hookEvent {
		return hookEvent{Name: "Stop", Agent: "crush", Session: session, Cwd: cwd}
	}
	cases := []struct {
		name string
		ev   hookEvent
		want string
	}{
		{"named", stop(named, project), "Spike Hello Title"},
		{"untitled", stop(untitled, project), ""},
		{"not listed", stop(unlisted, project), ""},
		{"another folder", stop(named, other), ""},
		{"turn start", hookEvent{Name: "UserPromptSubmit", Agent: "crush", Session: named, Cwd: project}, ""},
		{"relative folder", stop(named, "."), ""},
	}
	for _, c := range cases {
		if got := (crushTitle{}).title(nil, c.ev); got != c.want {
			t.Errorf("%s: title %q, want %q", c.name, got, c.want)
		}
	}

	os.Remove(ran)
	for _, forged := range []string{"--help", "a b", "../x", ""} {
		if got := (crushTitle{}).title(nil, stop(forged, project)); got != "" {
			t.Errorf("id %q: title %q", forged, got)
		}
	}
	if _, err := os.Stat(ran); !os.IsNotExist(err) {
		t.Fatal("crush ran for an id that is not a session's")
	}

	start := time.Now()
	if got := (crushTitle{}).title(nil, stop(named, hung)); got != "" || time.Since(start) > 5*time.Second {
		t.Fatalf("a hanging crush: title %q after %v", got, time.Since(start))
	}
}
