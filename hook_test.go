package main

import (
	"encoding/json"
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
}
