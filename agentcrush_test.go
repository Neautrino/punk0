//go:build !windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
