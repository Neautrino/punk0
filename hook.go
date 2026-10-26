package main

// Claude Code tells punk0 what the agent in a tab is doing through hooks: a
// settings file registers `punk0 hook` for every event punk0 cares about, and
// each tab's PTY carries the URL of the tab to report to. The orchestrator
// page then shows which agents are working, waiting for an answer, or done.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Tab statuses. An empty status means nothing is known: no shell, or the
// session ended.
const (
	statusWorking = "working"
	statusWaiting = "waiting"
	statusIdle    = "idle"
)

// claudeHookEvents are the events punk0 registers. Anything not listed here
// would only cost the agent a process launch per event.
var claudeHookEvents = []string{
	"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse",
	"PostToolUseFailure", "PermissionRequest", "Notification", "Stop",
	"StopFailure", "SessionEnd",
}

func claudeHooksPath() string { return filepath.Join(workbenchStateDir(), "claude-hooks.json") }

// writeClaudeHooks writes the --settings file claude tabs start with. exe is
// this punk0, so a hook keeps reporting to the version that started it.
func writeClaudeHooks(exe string) error {
	type hookCmd struct {
		Type    string   `json:"type"`
		Command string   `json:"command"`
		Args    []string `json:"args,omitempty"`
	}
	type hookMatcher struct {
		Hooks []hookCmd `json:"hooks"`
	}
	h := hookCmd{Type: "command", Command: shellQuote(exe) + " hook"}
	if runtime.GOOS == "windows" {
		// Claude runs a hook's command line through Git Bash there, or
		// PowerShell without it, and they quote differently. With args
		// (exec form, Claude Code 2.1.139+) it starts punk0 with no shell.
		// Unix keeps the command line: an older Claude would ignore args
		// and start a whole punk0.
		h = hookCmd{Type: "command", Command: exe, Args: []string{"hook"}}
	}
	hooks := make(map[string][]hookMatcher, len(claudeHookEvents))
	for _, ev := range claudeHookEvents {
		hooks[ev] = []hookMatcher{{Hooks: []hookCmd{h}}}
	}
	return writeFileAtomic(claudeHooksPath(), map[string]any{"hooks": hooks})
}

// hookBodyMax bounds one event as the handler reads it. runHook slims events
// first (slimHookEvent), so only a very long prompt comes near it.
const hookBodyMax = 1 << 20

// runHook is `punk0 hook`: Claude Code runs it with the event on stdin. It
// posts the event to the tab it came from and says nothing either way -- a
// hook that writes to stderr or fails would show up in the agent's chat.
func runHook() {
	target := os.Getenv("PUNK0_HOOK_URL")
	if target == "" {
		return
	}
	body, err := io.ReadAll(io.LimitReader(os.Stdin, 64<<20))
	if err != nil || len(body) == 0 {
		return
	}
	body = slimHookEvent(body)
	u, err := url.Parse(target)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	// The parent checks Origin on every write, like it does for its page.
	req.Header.Set("Origin", u.Scheme+"://"+u.Host)
	if resp, err := http.DefaultClient.Do(req); err == nil {
		resp.Body.Close()
	}
}

// slimHookEvent drops what punk0 never reads and a tool event can carry
// megabytes of: the tool's result, and its input but for the file it wrote.
// An event that does not parse goes as it came.
func slimHookEvent(body []byte) []byte {
	var ev map[string]json.RawMessage
	if json.Unmarshal(body, &ev) != nil {
		return body
	}
	delete(ev, "tool_response")
	if in, ok := ev["tool_input"]; ok {
		var paths struct {
			FilePath     string `json:"file_path,omitempty"`
			NotebookPath string `json:"notebook_path,omitempty"`
		}
		json.Unmarshal(in, &paths)
		ev["tool_input"], _ = json.Marshal(paths)
	}
	out, err := json.Marshal(ev)
	if err != nil {
		return body
	}
	return out
}

// hookEvent is the part of a Claude Code hook event punk0 uses: what the tab
// shows, the prompt a scratch chat is titled after, and the session and
// folders its agent works in.
type hookEvent struct {
	Name         string          `json:"hook_event_name"`
	Notification string          `json:"notification_type"`
	Background   json.RawMessage `json:"background_tasks"`
	Prompt       string          `json:"prompt"` // UserPromptSubmit
	Session      string          `json:"session_id"`
	Cwd          string          `json:"cwd"`
	ToolInput    json.RawMessage `json:"tool_input"` // file_path or notebook_path for the edit tools
}

// hookStatus maps one hook event to the status its tab shows. The second
// result is false for an event that says nothing about the agent, so an
// unknown event leaves the status as it was.
func hookStatus(ev hookEvent) (string, bool) {
	switch ev.Name {
	case "UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolUseFailure":
		return statusWorking, true
	case "PermissionRequest":
		return statusWaiting, true
	case "Stop", "StopFailure":
		// Claude stopped answering, but tasks it started in the background
		// are still going.
		if hasBackgroundTasks(ev.Background) {
			return statusWorking, true
		}
		return statusIdle, true
	case "SessionStart":
		return statusIdle, true
	case "SessionEnd":
		return "", true
	case "Notification":
		switch ev.Notification {
		case "permission_prompt", "elicitation_dialog", "elicitation_url_dialog":
			return statusWaiting, true
		case "idle_prompt":
			return statusIdle, true
		}
	}
	return "", false
}

// hasBackgroundTasks reports whether the event carries any; the field is
// absent, null or empty when it does not.
func hasBackgroundTasks(raw json.RawMessage) bool {
	s := string(bytes.TrimSpace(raw))
	return s != "" && s != "null" && s != "[]" && s != "{}"
}

// handleHook takes a hook event from a tab's agent. It is a write from this
// machine like any other, so it goes through the same Origin check.
func (wb *workbench) handleHook(w http.ResponseWriter, r *http.Request) {
	if !localOrigin(w, r) {
		return
	}
	tab := wb.records.Tab(r.PathValue("id"))
	if tab == nil {
		fail(w, http.StatusNotFound, "tab not found")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, hookBodyMax))
	if err != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	var ev hookEvent
	if json.Unmarshal(body, &ev) != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	if status, ok := hookStatus(ev); ok {
		tab.setStatus(status)
	}
	// The session it reports is filed under the tab's workspace, unless the
	// user filed it elsewhere; a worktree it works in is listed under it.
	if ev.Session != "" {
		if err := wb.records.NoteSession(tab.ID, ev.Session); err != nil {
			log.Printf("filing session of tab %s: %v", tab.ID, err)
		}
	}
	wb.noteAgentDirs(tab.ID, ev)
	if ev.Name == "UserPromptSubmit" {
		if err := wb.records.ChatPrompt(tab.ID, ev.Prompt); err != nil {
			fail(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, map[string]any{"ok": true})
}
