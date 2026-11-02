package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

// A qwen turn with a permission prompt, as qwen 0.26.5 delivered it, shows
// working, needs-you, working, idle in its tab, and /clear moves the tab to
// the new chat. Only what punk0's defaults file registers reaches the tab:
// qwen also sends Notification idle_prompt just before a permission prompt,
// which would flash the tab idle while it waits for the user.
func TestQwenTurnShowsInItsTab(t *testing.T) {
	wb, _, repo := historyWorkbench(t)
	exe := "/x/punk0"
	_, env, err := agentReportOf("qwen").via.setup(exe, "qwen")
	if err != nil {
		t.Fatal(err)
	}
	path := lastEnv(env, "QWEN_CODE_SYSTEM_DEFAULTS_PATH")
	// qwen migrates a settings file that lacks its version by rewriting it.
	var file struct {
		Version int `json:"$version"`
	}
	if b, err := os.ReadFile(path); err != nil || json.Unmarshal(b, &file) != nil || file.Version != 4 {
		t.Fatalf("defaults file %s: version %d (%v)", path, file.Version, err)
	}

	statuses, sessions := agentTurn(t, wb, repo, "qwen", reportedEvents(t, path, exe, "qwen"), qwenTurn)
	if want := []string{statusIdle, statusWorking, statusWaiting, statusWorking, statusIdle, "", statusIdle}; !slices.Equal(statuses, want) {
		t.Errorf("statuses %q, want %q", statuses, want)
	}
	if want := []string{"2bc902cf-d0df-4eb6-8c53-34b8f9812772", "b73a38b8-eabf-4a34-8e28-b7e1b17408af"}; !slices.Equal(sessions, want) {
		t.Errorf("chats %q, want %q", sessions, want)
	}
}

// qwenTurn is qwen 0.26.5's every hook event for one turn with a permission
// prompt, then /clear, in order (hookspike-2026-11-01/out/qwen.jsonl,
// sessions 2bc902cf and b73a38b8), trimmed of fields punk0 does not read.
var qwenTurn = []string{
	`{"session_id":"2bc902cf-d0df-4eb6-8c53-34b8f9812772","transcript_path":"/home/u/.qwen/projects/-tmp-w/chats/2bc902cf-d0df-4eb6-8c53-34b8f9812772.jsonl","cwd":"/tmp/w","hook_event_name":"InstructionsLoaded","file_path":"/home/u/.qwen/output-language.md","memory_type":"extension","load_reason":"session_start"}`,
	`{"session_id":"2bc902cf-d0df-4eb6-8c53-34b8f9812772","transcript_path":"/home/u/.qwen/projects/-tmp-w/chats/2bc902cf-d0df-4eb6-8c53-34b8f9812772.jsonl","cwd":"/tmp/w","hook_event_name":"SessionStart","permission_mode":"default","source":"startup","model":"stub-model"}`,
	`{"session_id":"2bc902cf-d0df-4eb6-8c53-34b8f9812772","transcript_path":"/home/u/.qwen/projects/-tmp-w/chats/2bc902cf-d0df-4eb6-8c53-34b8f9812772.jsonl","cwd":"/tmp/w","hook_event_name":"UserPromptSubmit","prompt":"Run the shell command: echo hi > hello.txt  — then reply with just DONE."}`,
	`{"session_id":"2bc902cf-d0df-4eb6-8c53-34b8f9812772","transcript_path":"/home/u/.qwen/projects/-tmp-w/chats/2bc902cf-d0df-4eb6-8c53-34b8f9812772.jsonl","cwd":"/tmp/w","hook_event_name":"PreToolUse","tool_name":"run_shell_command","tool_input":{"command":"echo hi > hello.txt","description":"write hi to hello.txt","is_background":false},"tool_call_id":"call_fe3d782e"}`,
	`{"session_id":"2bc902cf-d0df-4eb6-8c53-34b8f9812772","transcript_path":"/home/u/.qwen/projects/-tmp-w/chats/2bc902cf-d0df-4eb6-8c53-34b8f9812772.jsonl","cwd":"/tmp/w","hook_event_name":"Notification","notification_type":"idle_prompt","message":"Qwen Code is waiting for your input"}`,
	`{"session_id":"2bc902cf-d0df-4eb6-8c53-34b8f9812772","transcript_path":"/home/u/.qwen/projects/-tmp-w/chats/2bc902cf-d0df-4eb6-8c53-34b8f9812772.jsonl","cwd":"/tmp/w","hook_event_name":"PermissionRequest","tool_name":"run_shell_command","tool_input":{"command":"echo hi > hello.txt","description":"write hi to hello.txt","is_background":false}}`,
	`{"session_id":"2bc902cf-d0df-4eb6-8c53-34b8f9812772","transcript_path":"/home/u/.qwen/projects/-tmp-w/chats/2bc902cf-d0df-4eb6-8c53-34b8f9812772.jsonl","cwd":"/tmp/w","hook_event_name":"Notification","notification_type":"permission_prompt","title":"Permission needed","message":"Qwen Code needs your permission to use run_shell_command"}`,
	`{"session_id":"2bc902cf-d0df-4eb6-8c53-34b8f9812772","transcript_path":"/home/u/.qwen/projects/-tmp-w/chats/2bc902cf-d0df-4eb6-8c53-34b8f9812772.jsonl","cwd":"/tmp/w","hook_event_name":"PostToolUse","tool_name":"run_shell_command","tool_input":{"command":"echo hi > hello.txt","description":"write hi to hello.txt","is_background":false},"tool_response":{"llmContent":"Command: echo hi > hello.txt\nExit Code: 0"}}`,
	`{"session_id":"2bc902cf-d0df-4eb6-8c53-34b8f9812772","transcript_path":"/home/u/.qwen/projects/-tmp-w/chats/2bc902cf-d0df-4eb6-8c53-34b8f9812772.jsonl","cwd":"/tmp/w","hook_event_name":"PostToolBatch","tool_calls":[{"tool_name":"run_shell_command"}]}`,
	`{"session_id":"2bc902cf-d0df-4eb6-8c53-34b8f9812772","transcript_path":"/home/u/.qwen/projects/-tmp-w/chats/2bc902cf-d0df-4eb6-8c53-34b8f9812772.jsonl","cwd":"/tmp/w","hook_event_name":"UserPromptSubmit","prompt":""}`,
	`{"session_id":"2bc902cf-d0df-4eb6-8c53-34b8f9812772","transcript_path":"/home/u/.qwen/projects/-tmp-w/chats/2bc902cf-d0df-4eb6-8c53-34b8f9812772.jsonl","cwd":"/tmp/w","hook_event_name":"Stop","stop_hook_active":false,"last_assistant_message":"DONE","background_tasks":[],"crons":[]}`,
	`{"session_id":"2bc902cf-d0df-4eb6-8c53-34b8f9812772","transcript_path":"/home/u/.qwen/projects/-tmp-w/chats/2bc902cf-d0df-4eb6-8c53-34b8f9812772.jsonl","cwd":"/tmp/w","hook_event_name":"Notification","notification_type":"idle_prompt","title":"Waiting for input"}`,
	`{"session_id":"2bc902cf-d0df-4eb6-8c53-34b8f9812772","transcript_path":"/home/u/.qwen/projects/-tmp-w/chats/2bc902cf-d0df-4eb6-8c53-34b8f9812772.jsonl","cwd":"/tmp/w","hook_event_name":"SessionEnd","reason":"clear"}`,
	`{"session_id":"b73a38b8-eabf-4a34-8e28-b7e1b17408af","transcript_path":"/home/u/.qwen/projects/-tmp-w/chats/b73a38b8-eabf-4a34-8e28-b7e1b17408af.jsonl","cwd":"/tmp/w","hook_event_name":"SessionStart","source":"clear","model":"stub-model"}`,
	`{"session_id":"b73a38b8-eabf-4a34-8e28-b7e1b17408af","transcript_path":"/home/u/.qwen/projects/-tmp-w/chats/b73a38b8-eabf-4a34-8e28-b7e1b17408af.jsonl","cwd":"/tmp/w","hook_event_name":"Notification","notification_type":"idle_prompt"}`,
}

// reportedEvents are the events the hook settings file at path registers;
// every one runs this punk0's hook for kind and nothing else.
func reportedEvents(t *testing.T, path, exe, kind string) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Hooks map[string][]struct {
			Hooks []struct{ Type, Command string }
		}
	}
	if err := json.Unmarshal(b, &file); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	events := map[string]bool{}
	for ev, matchers := range file.Hooks {
		for _, m := range matchers {
			for _, h := range m.Hooks {
				if h.Type != "command" || h.Command != hookLine(exe, kind) {
					t.Fatalf("%s runs %+v, not punk0's hook for %s", ev, h, kind)
				}
			}
		}
		events[ev] = true
	}
	return events
}

// agentTurn plays an agent's hook events into a shell tab running kind, as
// that agent delivers them with the reporting punk0 wrote for it: an event
// the file does not register never runs punk0's hook, and one that does
// reaches the tab slimmed by runHook. It returns the statuses the tab
// showed and the chats it was bound to, each without repeats.
func agentTurn(t *testing.T, wb *workbench, repo, kind string, registered map[string]bool, events []string) (statuses, sessions []string) {
	t.Helper()
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
	post := func(body []byte) {
		t.Helper()
		if w := reqJSON(t, wb, "POST", "/api/tabs/"+tab.ID+"/hook", string(body)); w.Code != 200 {
			t.Fatalf("%s: %d %s", body, w.Code, w.Body.String())
		}
	}
	start, _ := json.Marshal(map[string]any{"hook_event_name": agentStartEvent, "punk0_agent": kind})
	post(start)
	for _, raw := range events {
		var ev hookEvent
		if err := json.Unmarshal([]byte(raw), &ev); err != nil {
			t.Fatalf("fixture %s: %v", raw, err)
		}
		if !registered[ev.Name] {
			continue
		}
		post(slimHookEvent([]byte(raw), kind))
		statuses = append(statuses, tab.Status())
		if tab.Session != "" {
			sessions = append(sessions, tab.Session)
		}
	}
	return slices.Compact(statuses), slices.Compact(sessions)
}

// A droid turn with a permission prompt shows working, needs-you, working,
// idle in its tab. droid reports through Claude's event names and values.
// Captured live: SessionStart, PreToolUse, PostToolUse, Stop and SessionEnd
// (droid exec 1.1.0, hookspike-2026-11-01/out/droid.jsonl, session
// 7f8dbc47). WRITTEN BY HAND, never seen from droid: UserPromptSubmit and
// both Notifications. The interactive app needs a Factory login the spike
// did not have; their shape is Claude's.
func TestDroidTurnShowsInItsTab(t *testing.T) {
	wb, _, repo := historyWorkbench(t)
	exe := "/x/punk0"
	argv, _, err := agentReportOf("droid").via.setup(exe, "droid")
	if err != nil {
		t.Fatal(err)
	}
	if len(argv) != 2 || argv[0] != "--settings" {
		t.Fatalf("droid is given %q, not a settings file", argv)
	}
	statuses, sessions := agentTurn(t, wb, repo, "droid", reportedEvents(t, argv[1], exe, "droid"), droidTurn)
	if want := []string{statusIdle, statusWorking, statusWaiting, statusWorking, statusIdle, ""}; !slices.Equal(statuses, want) {
		t.Errorf("statuses %q, want %q", statuses, want)
	}
	if want := []string{"7f8dbc47-cde1-4007-90a9-ea517575555b"}; !slices.Equal(sessions, want) {
		t.Errorf("chats %q, want %q", sessions, want)
	}
}

// droidTurn is one droid turn with a permission prompt; see
// TestDroidTurnShowsInItsTab for which lines are captured and which are not.
var droidTurn = []string{
	`{"session_id":"7f8dbc47-cde1-4007-90a9-ea517575555b","transcript_path":"/home/u/.factory/sessions/-tmp-w/7f8dbc47-cde1-4007-90a9-ea517575555b.jsonl","cwd":"/tmp/w","permission_mode":"auto-medium","hook_event_name":"SessionStart","source":"startup"}`,
	// by hand
	`{"session_id":"7f8dbc47-cde1-4007-90a9-ea517575555b","transcript_path":"/home/u/.factory/sessions/-tmp-w/7f8dbc47-cde1-4007-90a9-ea517575555b.jsonl","cwd":"/tmp/w","hook_event_name":"UserPromptSubmit","prompt":"Run the shell command: echo hi > hello.txt"}`,
	`{"session_id":"7f8dbc47-cde1-4007-90a9-ea517575555b","transcript_path":"/home/u/.factory/sessions/-tmp-w/7f8dbc47-cde1-4007-90a9-ea517575555b.jsonl","cwd":"/tmp/w","permission_mode":"auto-low","hook_event_name":"PreToolUse","tool_name":"Execute","tool_input":{"command":"echo hi > hello.txt","riskLevel":"low","riskLevelReason":"creates a file in the work folder"}}`,
	// by hand
	`{"session_id":"7f8dbc47-cde1-4007-90a9-ea517575555b","transcript_path":"/home/u/.factory/sessions/-tmp-w/7f8dbc47-cde1-4007-90a9-ea517575555b.jsonl","cwd":"/tmp/w","hook_event_name":"Notification","notification_type":"permission_prompt","message":"Droid needs your permission to use Execute"}`,
	`{"session_id":"7f8dbc47-cde1-4007-90a9-ea517575555b","transcript_path":"/home/u/.factory/sessions/-tmp-w/7f8dbc47-cde1-4007-90a9-ea517575555b.jsonl","cwd":"/tmp/w","permission_mode":"auto-low","hook_event_name":"PostToolUse","tool_name":"Execute","tool_input":{"command":"echo hi > hello.txt","riskLevel":"low","riskLevelReason":"creates a file in the work folder"},"tool_response":"Command completed successfully\n\n[Process exited with code 0]"}`,
	`{"session_id":"7f8dbc47-cde1-4007-90a9-ea517575555b","transcript_path":"/home/u/.factory/sessions/-tmp-w/7f8dbc47-cde1-4007-90a9-ea517575555b.jsonl","cwd":"/tmp/w","permission_mode":"auto-low","hook_event_name":"Stop","stop_hook_active":false,"tool_execution_count":0,"elapsed_time":108}`,
	// by hand
	`{"session_id":"7f8dbc47-cde1-4007-90a9-ea517575555b","transcript_path":"/home/u/.factory/sessions/-tmp-w/7f8dbc47-cde1-4007-90a9-ea517575555b.jsonl","cwd":"/tmp/w","hook_event_name":"Notification","notification_type":"idle_prompt","message":"Droid is waiting for your input"}`,
	`{"session_id":"7f8dbc47-cde1-4007-90a9-ea517575555b","transcript_path":"/home/u/.factory/sessions/-tmp-w/7f8dbc47-cde1-4007-90a9-ea517575555b.jsonl","cwd":"/tmp/w","permission_mode":"auto-low","hook_event_name":"SessionEnd","reason":"other","session_duration_ms":338.795654296875,"message_count":10}`,
}

// A qwen or droid tab is titled from the transcript its hook names: qwen's
// last title record (a /rename after the auto title wins), droid's first
// line, where "New Session" is no title yet. A transcript outside the
// agent's own folder, or an event whose chat id could not be one, gives none.
func TestQwenAndDroidChatTitles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("QWEN_HOME", "")
	write := func(path string, lines ...string) string {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	// Records from ~/.qwen/projects/-tmp-hookspike-work-qwen/chats/33333333-….jsonl (qwen 0.26.5), trimmed.
	qid := "33333333-4444-4555-8666-777777777777"
	qwen := []string{
		`{"uuid":"42bb48c2","sessionId":"` + qid + `","type":"user","cwd":"/tmp/w","message":{"role":"user","parts":[{"text":"say ok"}]}}`,
		`{"uuid":"ae778d94","sessionId":"` + qid + `","type":"system","cwd":"/tmp/w","subtype":"custom_title","systemPayload":{"customTitle":"Stub auto title for spike","titleSource":"auto"}}`,
		`{"uuid":"b2d54a17","sessionId":"` + qid + `","type":"system","cwd":"/tmp/w","subtype":"custom_title","systemPayload":{"customTitle":"Manual spike title 0265","titleSource":"manual"}}`,
		`{"uuid":"c1","sessionId":"` + qid + `","type":"system","subtype":"ui_telemetry","systemPayload":{"uiEvent":{"event.name":"qwen-code.api_response"}}}`,
	}
	qwenFile := write(filepath.Join(home, ".qwen", "projects", "-tmp-w", "chats", qid+".jsonl"), qwen...)
	qwenElsewhere := write(filepath.Join(home, "elsewhere", qid+".jsonl"), qwen...)

	// First lines from ~/.factory/sessions/-tmp-hookspike-work-droid/ (droid 1.1.0).
	did, newID := "2189db3c-b56c-45fe-a201-ee16e496fb97", "11111111-2222-3333-4444-555555555555"
	sessions := filepath.Join(home, ".factory", "sessions", "-tmp-w")
	droidFile := write(filepath.Join(sessions, did+".jsonl"),
		`{"type":"session_start","id":"`+did+`","title":"say ok","owner":"u","version":2,"cwd":"/tmp/w"}`,
		`{"type":"message","id":"m1"}`)
	droidNew := write(filepath.Join(sessions, newID+".jsonl"),
		`{"type":"session_start","id":"`+newID+`","title":"New Session","owner":"u","version":2,"cwd":"/tmp/w"}`)
	droidElsewhere := write(filepath.Join(home, ".factory", did+".jsonl"),
		`{"type":"session_start","id":"`+did+`","title":"say ok"}`)

	for _, c := range []struct {
		step, kind, session, transcript, want string
	}{
		{"qwen", "qwen", qid, qwenFile, "Manual spike title 0265"},
		{"qwen outside ~/.qwen", "qwen", qid, qwenElsewhere, ""},
		{"qwen climbing out of ~/.qwen", "qwen", qid, filepath.Join(home, ".qwen", "..", "elsewhere", qid+".jsonl"), ""},
		{"qwen id like a flag", "qwen", "-rf", qwenFile, ""},
		{"droid", "droid", did, droidFile, "say ok"},
		{"droid untitled", "droid", newID, droidNew, ""},
		{"droid outside its sessions", "droid", did, droidElsewhere, ""},
		{"droid id with a slash", "droid", "a/b", droidFile, ""},
	} {
		src := tabAgents[c.kind].report.title
		if src == nil {
			t.Fatalf("%s: %s names no title source", c.step, c.kind)
		}
		if got := src.title(newChatHistory(), hookEvent{Session: c.session, Transcript: c.transcript}); got != c.want {
			t.Errorf("%s: title %q, want %q", c.step, got, c.want)
		}
	}
}

// Copilot keeps a chat's name in workspace.yaml beside the chat, quoted when
// the name holds YAML's own characters. The id comes from a hook, so one
// that could leave copilot's folder is never followed.
func TestCopilotTitle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("COPILOT_HOME", home)
	chat := func(id, yaml string) {
		t.Helper()
		dir := filepath.Join(home, "session-state", id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "workspace.yaml"), []byte(yaml), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Captured from ~/.copilot/session-state (copilot 1.0.63), trimmed.
	chat("c344e21f-c7b9-490e-ae30-41ae7579aa13", "id: c344e21f-c7b9-490e-ae30-41ae7579aa13\ncwd: /tmp/hookspike/work/copilot\nbranch: master\nclient_name: github/cli\nname: Run Shell Command to Create File\nuser_named: false\nsummary_count: 0\n")
	chat("2bd7c780-8b3c-4c00-b2f9-c388049af354", "id: 2bd7c780-8b3c-4c00-b2f9-c388049af354\nname: 'Reply with exactly: AGENT_OK'\nuser_named: false\n")
	chat("11111111-2222-4333-8444-555555555555", "id: 11111111-2222-4333-8444-555555555555\nname: 'it''s #1'\n")
	chat("22222222-2222-4333-8444-555555555555", "id: 22222222-2222-4333-8444-555555555555\nname: \"say \\\"ok\\\": now\"\n")
	// Titled only after the first turn: no name line yet.
	chat("1bb5ade6-db6b-4423-8817-495b26a80e17", "id: 1bb5ade6-db6b-4423-8817-495b26a80e17\ncwd: /home/u\nuser_named: false\n")
	// Not a session id: it would name a path outside copilot's folder.
	chat("../escape", "name: Escaped\n")

	for id, want := range map[string]string{
		"c344e21f-c7b9-490e-ae30-41ae7579aa13": "Run Shell Command to Create File",
		"2bd7c780-8b3c-4c00-b2f9-c388049af354": "Reply with exactly: AGENT_OK",
		"11111111-2222-4333-8444-555555555555": "it's #1",
		"22222222-2222-4333-8444-555555555555": `say "ok": now`,
		"1bb5ade6-db6b-4423-8817-495b26a80e17": "",
		"33333333-2222-4333-8444-555555555555": "",
		"../escape":                            "",
	} {
		if got := (copilotTitle{}).title(nil, hookEvent{Session: id}); got != want {
			t.Errorf("title of %q = %q, want %q", id, got, want)
		}
	}
}

// A copilot turn with a permission prompt drives its tab as Claude's does,
// from copilot's own payloads: the first event binds the chat (punk0 hears
// no SessionStart from copilot), the prompt's Notification shows needs-you,
// the chat's name comes from copilot's workspace.yaml, and /clear's new chat
// takes the tab over. Captured from copilot 1.0.63 under --plugin-dir with
// punk0's keys (2026-11-02), trimmed; the report_intent PostToolUse copilot
// sent beside the prompt is left out, as punk0's matcher keeps it from
// running punk0's hook.
func TestCopilotTurnDrivesTheTab(t *testing.T) {
	wb, _, repo := historyWorkbench(t)
	home := t.TempDir()
	t.Setenv("COPILOT_HOME", home)
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
		if w := reqJSON(t, wb, "POST", "/api/tabs/"+tab.ID+"/hook", string(slimHookEvent([]byte(body), "copilot"))); w.Code != 200 {
			t.Fatalf("%s: %d %s", body, w.Code, w.Body.String())
		}
	}
	first, second := "82df0e65-deca-416d-9628-c09f0e507b46", "3d70ee6f-b12d-4982-ac0f-8c90aa8e5f64"
	dir := filepath.Join(home, "session-state", first)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "workspace.yaml"), []byte("id: "+first+"\nname: Create hello.txt\nuser_named: false\n"), 0o644)

	post(`{"hook_event_name":"` + agentStartEvent + `","punk0_agent":"copilot"}`)
	for _, step := range []struct{ body, status string }{
		{`{"hook_event_name":"UserPromptSubmit","session_id":"` + first + `","prompt":"Run the shell command: echo hi > hello.txt  - then reply with just DONE.","cwd":"/tmp/w"}`, statusWorking},
		{`{"sessionId":"` + first + `","notification_type":"permission_prompt","cwd":"/tmp/w","hook_event_name":"Notification","message":"Run command: echo hi > hello.txt","title":"Permission needed"}`, statusWaiting},
		{`{"hook_event_name":"PostToolUse","session_id":"` + first + `","tool_input":{"command":"echo hi > hello.txt","description":"Create hello.txt with the requested content","mode":"sync","initial_wait":10},"cwd":"/tmp/w","tool_name":"Bash"}`, statusWorking},
		{`{"hook_event_name":"Stop","session_id":"` + first + `","stop_reason":"end_turn","cwd":"/tmp/w","transcript_path":"` + filepath.Join(dir, "events.jsonl") + `"}`, statusIdle},
	} {
		post(step.body)
		if got := tab.Status(); got != step.status {
			t.Fatalf("after %.60s…: status %q, want %q", step.body, got, step.status)
		}
		if tab.Session != first {
			t.Fatalf("after %.60s…: chat %q, want %q", step.body, tab.Session, first)
		}
	}
	waitFor(t, func() bool { return tab.view().Title == "Create hello.txt" })

	// /clear: the old chat ends, the next prompt is a new one.
	post(`{"hook_event_name":"SessionEnd","session_id":"` + first + `","reason":"user_exit","cwd":"/tmp/w"}`)
	if got := tab.Status(); got != "" {
		t.Fatalf("after SessionEnd: status %q", got)
	}
	post(`{"hook_event_name":"UserPromptSubmit","session_id":"` + second + `","prompt":"say ok","cwd":"/tmp/w"}`)
	if v := tab.view(); tab.Session != second || v.Kind != "copilot" || tab.Status() != statusWorking {
		t.Fatalf("new chat: kind %q chat %q status %q, want copilot %q working", v.Kind, tab.Session, tab.Status(), second)
	}
}

// Pi and Oh My Pi report through punk0's extension, which posts these bodies
// itself (pi-extension.ts, omp-extension.ts; ids, paths and titles from live
// runs): the tab follows the chat's status, shows the title the agent gives
// the chat once a re-send carries it, and a new chat in the same agent
// rebinds the tab.
func TestExtensionAgentsReportToTheirTab(t *testing.T) {
	wb, _, repo := historyWorkbench(t)
	p, err := wb.records.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	type step struct {
		name, session, title, prompt string
		agentID                      string // a subagent's event (omp's approval prompts)
		status                       string
	}
	for _, c := range []struct {
		agent, transcripts string
		steps              []step
	}{
		{"pi", "/home/u/.pi/agent/sessions/--w--/", []step{
			{name: "SessionStart", session: "01a197bb-4ce2-7bcb-b773-c5abb0ad2f65", status: statusIdle},
			{name: "UserPromptSubmit", session: "01a197bb-4ce2-7bcb-b773-c5abb0ad2f65", prompt: "Run the shell command: echo hi > hello.txt", status: statusWorking},
			{name: "Stop", session: "01a197bb-4ce2-7bcb-b773-c5abb0ad2f65", status: statusIdle},
			// /name: the last status again, with the name.
			{name: "Stop", session: "01a197bb-4ce2-7bcb-b773-c5abb0ad2f65", title: "Spike chat A", status: statusIdle},
			// /new: the old chat ends, the new one binds with no title yet.
			{name: "SessionEnd", session: "01a197bb-4ce2-7bcb-b773-c5abb0ad2f65", title: "Spike chat A", status: ""},
			{name: "SessionStart", session: "01a197bb-acad-722f-8079-369fb8aeb5b8", status: statusIdle},
		}},
	} {
		t.Run(c.agent, func(t *testing.T) {
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
			post(map[string]any{"hook_event_name": agentStartEvent, "punk0_agent": c.agent})
			for i, s := range c.steps {
				ev := map[string]any{
					"hook_event_name": s.name,
					"punk0_agent":     c.agent,
					"session_id":      s.session,
					"transcript_path": c.transcripts + s.session + ".jsonl",
					"title":           s.title,
					"cwd":             repo,
				}
				if s.name == "UserPromptSubmit" {
					ev["prompt"] = s.prompt
				}
				if s.agentID != "" {
					ev["agent_id"] = s.agentID
				}
				post(ev)
				if tab.view().Kind != c.agent || tab.Session != s.session || tab.Status() != s.status {
					t.Fatalf("step %d %s: kind %q session %q status %q, want %q %q %q", i, s.name, tab.view().Kind, tab.Session, tab.Status(), c.agent, s.session, s.status)
				}
				waitFor(t, func() bool { return tab.view().Title == s.title })
			}
		})
	}
}
