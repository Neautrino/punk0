package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// Tool events can carry whole files; what reaches punk0 is only the path.
func TestSlimHookEvent(t *testing.T) {
	big := strings.Repeat("x", 1<<20)
	in, _ := json.Marshal(map[string]any{
		"hook_event_name": "PostToolUse", "tool_name": "Write", "cwd": "/w",
		"tool_input":    map[string]any{"file_path": "/w/a.go", "content": big},
		"tool_response": map[string]any{"content": big},
	})
	out := slimHookEvent(in)
	if len(out) > 300 {
		t.Fatalf("slimmed event is %d bytes: %.300s", len(out), out)
	}
	var ev hookEvent
	var input struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(out, &ev); err != nil || ev.Name != "PostToolUse" ||
		json.Unmarshal(ev.ToolInput, &input) != nil || input.FilePath != "/w/a.go" {
		t.Fatalf("slimmed = %s (%v)", out, err)
	}
	if got := slimHookEvent([]byte("not json")); string(got) != "not json" {
		t.Fatalf("unparsed event changed: %q", got)
	}
	// The worktree events carry what the answer is made of; slimming must
	// keep every one of those fields.
	wt, _ := json.Marshal(map[string]any{
		"hook_event_name": "WorktreeCreate", "cwd": "/w", "name": "fix-login",
		"worktree_path": "/w/t", "agent_id": "a1",
	})
	var kept hookEvent
	if err := json.Unmarshal(slimHookEvent(wt), &kept); err != nil {
		t.Fatal(err)
	}
	if kept.Name != "WorktreeCreate" || kept.WorktreeName != "fix-login" || kept.WorktreePath != "/w/t" || kept.AgentID != "a1" || kept.Cwd != "/w" {
		t.Fatalf("slimmed worktree event = %+v", kept)
	}
}

// The settings file claude tabs start with registers every event punk0
// reads, the worktree ones included: without them Claude makes its own
// worktrees wherever it likes.
func TestClaudeHooksFile(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if err := writeClaudeHooks("/x/punk0"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(claudeHooksPath())
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type    string   `json:"type"`
				Command string   `json:"command"`
				Args    []string `json:"args"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("%s: %v\n%s", claudeHooksPath(), err, data)
	}
	for _, ev := range []string{"WorktreeCreate", "WorktreeRemove", "SubagentStop", "SessionStart"} {
		if !slices.Contains(claudeHookEvents, ev) {
			t.Errorf("%s is not registered", ev)
		}
		m := file.Hooks[ev]
		if len(m) != 1 || len(m[0].Hooks) != 1 {
			t.Fatalf("%s: %+v", ev, m)
		}
		h := m[0].Hooks[0]
		want, wantArgs := "/x/punk0 hook", []string(nil)
		if runtime.GOOS == "windows" {
			want, wantArgs = "/x/punk0", []string{"hook"}
		}
		if h.Type != "command" || h.Command != want || !slices.Equal(h.Args, wantArgs) {
			t.Errorf("%s runs %q %q, want %q %q", ev, h.Command, h.Args, want, wantArgs)
		}
	}
	if len(file.Hooks) != len(claudeHookEvents) {
		t.Errorf("%d events in the file, %d registered", len(file.Hooks), len(claudeHookEvents))
	}
	if claudeHooksPath() != filepath.Join(workbenchStateDir(), "claude-hooks.json") {
		t.Errorf("hooks file at %s", claudeHooksPath())
	}
}
