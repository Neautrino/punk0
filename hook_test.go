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
	out := slimHookEvent(in, "")
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
	if got := slimHookEvent([]byte("not json"), "codex"); string(got) != "not json" {
		t.Fatalf("unparsed event changed: %q", got)
	}
	// The hook names the agent that sent it, so the tab can tell its own
	// agent's events from another's.
	var tagged hookEvent
	if err := json.Unmarshal(slimHookEvent(in, "codex"), &tagged); err != nil || tagged.Agent != "codex" {
		t.Fatalf("tagged event = %+v (%v)", tagged, err)
	}
	// The worktree events carry what the answer is made of; slimming must
	// keep every one of those fields.
	wt, _ := json.Marshal(map[string]any{
		"hook_event_name": "WorktreeCreate", "cwd": "/w", "name": "fix-login",
		"worktree_path": "/w/t", "agent_id": "a1",
	})
	var kept hookEvent
	if err := json.Unmarshal(slimHookEvent(wt, ""), &kept); err != nil {
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
		want, wantArgs := "/x/punk0 hook claude", []string(nil)
		if runtime.GOOS == "windows" {
			want, wantArgs = "/x/punk0", []string{"hook", "claude"}
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

// A crush turn shows in its tab like a Claude one: its events, renamed on the
// way as punk0's hook renames them, take the tab's dot working, waiting on a
// permission, working, then idle, and bind the chat they carry. A resumed
// chat sends no SessionStart, so its first turn binds it. The payloads are
// crush v0.99.0's, captured live (hookspike out/crush.jsonl), trimmed.
func TestCrushTurnShowsInItsTab(t *testing.T) {
	wb, _, repo := historyWorkbench(t)
	p, err := wb.records.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	tab, err := wb.records.AddTab(p.Workspaces[0].ID, tabShell, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := tab.Shell(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tab.close)
	post := func(body string) {
		t.Helper()
		ev := slimHookEvent(renameHookEvent([]byte(body), "crush"), "crush")
		if w := reqJSON(t, wb, "POST", "/api/tabs/"+tab.ID+"/hook", string(ev)); w.Code != 200 {
			t.Fatalf("%s: %d %s", body, w.Code, w.Body.String())
		}
	}
	// The shell function's start, as `punk0 hook start crush` sends it.
	if w := reqJSON(t, wb, "POST", "/api/tabs/"+tab.ID+"/hook", `{"hook_event_name":"`+agentStartEvent+`","punk0_agent":"crush"}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	const first, resumed = "ec14c082-ecbb-474e-92bf-1d229b584cec", "f103b48e-afd2-4ae7-ac35-b18bf4f38433"
	steps := []struct {
		body, status, session string
	}{
		{`{"event":"SessionStart","hook_event_name":"SessionStart","session_id":"` + first + `","cwd":"/w","source":"startup"}`, statusIdle, first},
		{`{"event":"TurnStart","hook_event_name":"TurnStart","session_id":"` + first + `","cwd":"/w"}`, statusWorking, first},
		{`{"event":"PreToolUse","hook_event_name":"PreToolUse","session_id":"` + first + `","cwd":"/w","tool_name":"bash","tool_input":{"command":"mkdir permdir","description":"spike command"},"tool_use_id":"call_e2cb9a9b"}`, statusWorking, first},
		{`{"event":"PermissionRequest","hook_event_name":"PermissionRequest","session_id":"` + first + `","cwd":"/w","tool_name":"bash","tool_input":{"description":"spike command","command":"mkdir permdir","working_dir":"","run_in_background":false,"auto_background_after":0},"tool_use_id":"call_e2cb9a9b"}`, statusWaiting, first},
		{`{"event":"PermissionResult","hook_event_name":"PermissionResult","session_id":"` + first + `","cwd":"/w","tool_name":"bash","tool_input":{"description":"spike command","command":"mkdir permdir","working_dir":"","run_in_background":false,"auto_background_after":0},"tool_use_id":"call_e2cb9a9b","decision":"granted"}`, statusWorking, first},
		{`{"event":"PostToolUse","hook_event_name":"PostToolUse","session_id":"` + first + `","cwd":"/w","tool_name":"bash","tool_input":{"command":"mkdir permdir","description":"spike command"},"tool_use_id":"call_e2cb9a9b","tool_response":"no output"}`, statusWorking, first},
		{`{"event":"TurnEnd","hook_event_name":"TurnEnd","session_id":"` + first + `","cwd":"/w","last_assistant_message":"DONE"}`, statusIdle, first},
		// A denial: the result shows working until the turn ends.
		{`{"event":"PermissionRequest","hook_event_name":"PermissionRequest","session_id":"` + first + `","cwd":"/w","tool_name":"bash","tool_input":{"command":"mkdir permdir2"},"tool_use_id":"call_2ee14087"}`, statusWaiting, first},
		{`{"event":"PermissionResult","hook_event_name":"PermissionResult","session_id":"` + first + `","cwd":"/w","tool_name":"bash","tool_input":{"command":"mkdir permdir2"},"tool_use_id":"call_2ee14087","decision":"denied"}`, statusWorking, first},
		{`{"event":"TurnEnd","hook_event_name":"TurnEnd","session_id":"` + first + `","cwd":"/w","last_assistant_message":""}`, statusIdle, first},
		// crush -s <id>: no SessionStart; the turn binds the chat. Esc on a
		// running tool ends it with Interrupt and no TurnEnd.
		{`{"event":"TurnStart","hook_event_name":"TurnStart","session_id":"` + resumed + `","cwd":"/w"}`, statusWorking, resumed},
		{`{"event":"PermissionRequest","hook_event_name":"PermissionRequest","session_id":"` + resumed + `","cwd":"/w","tool_name":"bash","tool_input":{"command":"sleep 20"},"tool_use_id":"call_b1fe908b"}`, statusWaiting, resumed},
		{`{"event":"PermissionResult","hook_event_name":"PermissionResult","session_id":"` + resumed + `","cwd":"/w","tool_name":"bash","tool_input":{"command":"sleep 20"},"tool_use_id":"call_b1fe908b","decision":"granted"}`, statusWorking, resumed},
		{`{"event":"Interrupt","hook_event_name":"Interrupt","session_id":"` + resumed + `","cwd":"/w"}`, statusIdle, resumed},
		{`{"event":"SessionEnd","hook_event_name":"SessionEnd","session_id":"` + resumed + `","cwd":"/w","reason":"exit"}`, "", resumed},
	}
	for i, s := range steps {
		post(s.body)
		if got := tab.Status(); got != s.status {
			t.Fatalf("step %d %s: status %q, want %q", i, s.body[:60], got, s.status)
		}
		if tab.Session != s.session {
			t.Fatalf("step %d: session %q, want %q", i, tab.Session, s.session)
		}
	}

	// Only crush's names change; a body that does not parse goes as it came.
	if got := string(renameHookEvent([]byte(`{"hook_event_name":"TurnEnd"}`), tabClaude)); got != `{"hook_event_name":"TurnEnd"}` {
		t.Errorf("claude's event renamed: %s", got)
	}
	if got := string(renameHookEvent([]byte(`{"hook_event_name":`), "crush")); got != `{"hook_event_name":` {
		t.Errorf("a broken body changed: %s", got)
	}
}
