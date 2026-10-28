package main

// The coding agents a tab can run interactively, for scratch chats picked in
// the New chat dialog. These are the agents px0's harness package knows (it
// runs them headless for edits); here each starts in its own TUI instead.
// Claude Code is the one punk0 follows closely: its hooks report what it is
// doing. Every agent that can resume a chat by id does so when the tab knows
// it (Satellite.Session: always for Claude, for the others when the tab was
// opened on an existing chat); without an id, the others resume this folder's
// most recent session after a restart where the agent can.

import (
	"net/http"
	"strings"

	"github.com/px0-ai/harness"

	"px0/internal/settings"
)

// tabAgent is how one agent starts in a tab.
type tabAgent struct {
	Label    string
	bin      string
	resume   []string // args resuming the latest session; nil when the agent has none
	resumeID []string // args before a session id resuming exactly it; nil when the agent cannot
}

// tabAgentOrder is the dialog's order; tabAgents has every agent in it.
var tabAgentOrder = []string{tabClaude, "codex", tabOpenCode, "agy", "cursor-agent"}

var tabAgents = map[string]tabAgent{
	tabClaude:      {Label: "Claude Code", bin: "claude"},
	"codex":        {Label: "Codex", bin: "codex", resume: []string{"resume", "--last"}, resumeID: []string{"resume"}},
	tabOpenCode:    {Label: "OpenCode", bin: "opencode", resume: []string{"--continue"}, resumeID: []string{"--session"}},
	"agy":          {Label: "Antigravity", bin: "agy"},
	"cursor-agent": {Label: "Cursor Agent", bin: "cursor-agent", resumeID: []string{"--resume"}},
}

// agentCommand is the line a tab's shell runs first, in the shell's syntax;
// "" for a plain shell. first is a tab's first start this run, which opens a
// new session; later starts resume. Claude resumes its own session id, and
// exits 1 when there is nothing to resume (the tab never got a message), so
// the fallback then starts that session. Another agent given a session
// resumes exactly it on every start, falling back to a new chat when the id
// is gone; without one it resumes its latest session when it can, else starts
// a new one. A new tab without a session never resumes: in a folder of the
// user's, the latest session may belong to something else.
// agents.<kind>.args goes right after the program name, verbatim: it is shell
// text the user wrote. session must be shell-safe (validSession).
func agentCommand(kind, session string, first bool, sx shellSyntax) string {
	a, ok := tabAgents[kind]
	if !ok {
		return ""
	}
	bin := a.bin
	if args := strings.TrimSpace(settings.SettingOr[string]("agents." + kind + ".args")); args != "" {
		bin += " " + args
	}
	if kind == tabClaude {
		start := claudeCommand(bin, sx) + " --session-id " + session
		if first {
			return start
		}
		return sx.orElse(claudeCommand(bin, sx)+" --resume "+session, start)
	}
	if session != "" && a.resumeID != nil {
		return sx.orElse(bin+" "+strings.Join(a.resumeID, " ")+" "+session, bin)
	}
	if first || a.resume == nil {
		return bin
	}
	return sx.orElse(bin+" "+strings.Join(a.resume, " "), bin)
}

// agentView is one choice in the New chat dialog.
type agentView struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Installed   bool   `json:"installed"`
	Resumes     bool   `json:"resumes"`     // picks its conversation up again after a restart
	ResumesByID bool   `json:"resumesByID"` // can open one exact earlier chat (History's Open)
	Status      bool   `json:"status"`      // reports working / needs you / idle
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
		out = append(out, agentView{ID: id, Label: a.Label, Installed: installed, Resumes: id == tabClaude || a.resume != nil,
			ResumesByID: id == tabClaude || a.resumeID != nil, Status: id == tabClaude})
	}
	writeJSON(w, out)
}
