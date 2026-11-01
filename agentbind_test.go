package main

import (
	"encoding/json"
	"testing"
)

// A tab runs an agent only while one runs in it: the agent's start binds it,
// its hooks name the chat and the chat's title, a hook of any other agent
// changes nothing, and its exit leaves a shell. The binding outlives a
// restart, and a shell punk0 stops keeps it, so the chat resumes.
func TestTabBindsTheAgentRunningInIt(t *testing.T) {
	wb, claude, repo := historyWorkbench(t)
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
	post := func(ev map[string]any) {
		t.Helper()
		body, _ := json.Marshal(ev)
		if w := reqJSON(t, wb, "POST", "/api/tabs/"+tab.ID+"/hook", string(body)); w.Code != 200 {
			t.Fatalf("%v: %d %s", ev, w.Code, w.Body.String())
		}
	}
	check := func(step, kind, session, title string) {
		t.Helper()
		if v := tab.view(); v.Kind != kind || tab.Session != session || v.Title != title {
			t.Fatalf("%s: kind %q session %q title %q, want %q %q %q", step, v.Kind, tab.Session, v.Title, kind, session, title)
		}
	}

	post(map[string]any{"hook_event_name": agentStartEvent, "punk0_agent": tabClaude})
	check("claude started", tabClaude, "", "")

	// Claude's Bash tool ran codex: codex's hooks are not the tab's.
	post(map[string]any{"hook_event_name": "UserPromptSubmit", "punk0_agent": "codex", "session_id": "019a-x"})
	if tab.Status() != "" {
		t.Fatalf("another agent's hook set the status to %q", tab.Status())
	}
	check("another agent's hook", tabClaude, "", "")

	id := "11111111-0000-4000-8000-000000000001"
	transcript := writeChat(t, claude, "-w", id, claudeUser(repo, id, "fix the login"), `{"type":"ai-title","aiTitle":"Fix login redirect"}`)
	post(map[string]any{"hook_event_name": "SessionStart", "punk0_agent": tabClaude, "session_id": id, "transcript_path": transcript})
	waitFor(t, func() bool { return tab.view().Title == "Fix login redirect" })
	check("session start", tabClaude, id, "Fix login redirect")

	// A subagent's event carries its own id; the tab's chat stays.
	post(map[string]any{"hook_event_name": "Stop", "punk0_agent": tabClaude, "session_id": "sub-1", "agent_id": "a1"})
	check("subagent stop", tabClaude, id, "Fix login redirect")

	// /clear starts a new chat in the same Claude.
	cleared := "22222222-0000-4000-8000-000000000002"
	post(map[string]any{"hook_event_name": "SessionStart", "punk0_agent": tabClaude, "session_id": cleared})
	check("cleared", tabClaude, cleared, "")

	reopened := openRecords(t, wb.records.home).Tab(tab.ID)
	if reopened == nil || reopened.Kind != tabClaude || reopened.Session != cleared {
		t.Fatalf("after a restart: %+v", reopened)
	}

	post(map[string]any{"hook_event_name": agentExitEvent, "punk0_agent": tabClaude})
	check("claude quit", tabShell, "", "")

	// A shell punk0 stops takes its agent down; the exit it may still
	// report must not cost the tab the chat it resumes.
	post(map[string]any{"hook_event_name": agentStartEvent, "punk0_agent": "codex"})
	post(map[string]any{"hook_event_name": "SessionStart", "punk0_agent": "codex", "session_id": "019a-y"})
	tab.close()
	post(map[string]any{"hook_event_name": agentExitEvent, "punk0_agent": "codex"})
	check("shell stopped by punk0", "codex", "019a-y", "")
}
