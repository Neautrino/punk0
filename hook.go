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
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	type hookMatcher struct {
		Hooks []hookCmd `json:"hooks"`
	}
	cmd := shellQuote(exe) + " hook"
	hooks := make(map[string][]hookMatcher, len(claudeHookEvents))
	for _, ev := range claudeHookEvents {
		hooks[ev] = []hookMatcher{{Hooks: []hookCmd{{Type: "command", Command: cmd}}}}
	}
	return writeFileAtomic(claudeHooksPath(), map[string]any{"hooks": hooks})
}

// runHook is `punk0 hook`: Claude Code runs it with the event on stdin. It
// posts the event to the tab it came from and says nothing either way -- a
// hook that writes to stderr or fails would show up in the agent's chat.
func runHook() {
	target := os.Getenv("PUNK0_HOOK_URL")
	if target == "" {
		return
	}
	body, err := io.ReadAll(io.LimitReader(os.Stdin, 64<<10))
	if err != nil || len(body) == 0 {
		return
	}
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

// hookEvent is the part of a Claude Code hook event a tab's status depends on.
type hookEvent struct {
	Name         string          `json:"hook_event_name"`
	Notification string          `json:"notification_type"`
	Background   json.RawMessage `json:"background_tasks"`
}

// hookStatus maps one hook event to the status its tab shows. The second
// result is false for an event that says nothing about the agent, so an
// unknown event leaves the status as it was.
func hookStatus(body []byte) (string, bool) {
	var ev hookEvent
	if json.Unmarshal(body, &ev) != nil {
		return "", false
	}
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
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	if status, ok := hookStatus(body); ok {
		tab.setStatus(status)
	}
	writeJSON(w, map[string]any{"ok": true})
}
