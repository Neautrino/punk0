package main

// The coding agents a tab can run interactively, for scratch chats picked in
// the New chat dialog. These are the agents px0's harness package knows (it
// runs them headless for edits); here each starts in its own TUI instead.
// Claude Code is the one punk0 follows closely: its hooks report what it is
// doing and its session id resumes the exact conversation. The others resume
// this folder's most recent session after a restart where the agent can, and
// report no status.

import (
	"net/http"
	"strings"

	"github.com/px0-ai/harness"
)

// tabAgent is how one agent starts in a tab.
type tabAgent struct {
	Label  string
	bin    string
	resume []string // args resuming the latest session; nil when the agent has none
}

// tabAgentOrder is the dialog's order; tabAgents has every agent in it.
var tabAgentOrder = []string{tabClaude, "codex", "opencode", "agy", "cursor-agent"}

var tabAgents = map[string]tabAgent{
	tabClaude:      {Label: "Claude Code", bin: "claude"},
	"codex":        {Label: "Codex", bin: "codex", resume: []string{"resume", "--last"}},
	"opencode":     {Label: "OpenCode", bin: "opencode", resume: []string{"--continue"}},
	"agy":          {Label: "Antigravity", bin: "agy"},
	"cursor-agent": {Label: "Cursor Agent", bin: "cursor-agent"},
}

// agentCommand is the line a tab's shell runs first; "" for a plain shell.
// first is a tab's first start this run, which opens a new session; later
// starts resume. Claude resumes its own session id, and exits 1 when there is
// nothing to resume (the tab never got a message), so || then starts that
// session. Other agents resume their latest session when they can, else start
// a new one. A new tab never resumes: in a folder of the user's, the latest
// session may belong to something else. agents.<kind>.args goes right after
// the program name, verbatim: it is shell text the user wrote.
func agentCommand(kind, session string, first bool) string {
	a, ok := tabAgents[kind]
	if !ok {
		return ""
	}
	bin := a.bin
	if args := strings.TrimSpace(settingOr[string]("agents." + kind + ".args")); args != "" {
		bin += " " + args
	}
	if kind == tabClaude {
		start := claudeCommand(bin) + " --session-id " + session
		if first {
			return start
		}
		return claudeCommand(bin) + " --resume " + session + " || " + start
	}
	if first || a.resume == nil {
		return bin
	}
	return bin + " " + strings.Join(a.resume, " ") + " || " + bin
}

// agentView is one choice in the New chat dialog.
type agentView struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Installed bool   `json:"installed"`
	Resumes   bool   `json:"resumes"` // picks its conversation up again after a restart
	Status    bool   `json:"status"`  // reports working / needs you / idle
}

// handleAgents lists the agents a chat can start with, installed or not.
func (wb *workbench) handleAgents(w http.ResponseWriter, r *http.Request) {
	if !trustedGet(w, r) {
		return
	}
	out := make([]agentView, 0, len(tabAgentOrder))
	for _, id := range tabAgentOrder {
		a := tabAgents[id]
		_, installed := harness.LookPath(a.bin)
		out = append(out, agentView{ID: id, Label: a.Label, Installed: installed, Resumes: id == tabClaude || a.resume != nil, Status: id == tabClaude})
	}
	writeJSON(w, out)
}
