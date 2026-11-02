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
	"sync"

	"github.com/px0-ai/harness"

	"px0/internal/settings"
)

// tabAgent is how one agent starts in a tab.
type tabAgent struct {
	Label    string
	bin      string
	resume   []string     // args resuming the latest session; nil when the agent has none
	resumeID []string     // args before a session id resuming exactly it; nil when the agent cannot
	joinID   bool         // the id joins the last of resumeID ("--resume=" + id) rather than following it
	report   *agentReport // how it reports its chat and status (agentshell.go); nil: punk0 only sees it start and exit
}

// agentReport is what punk0 learns from one agent while it runs: a mechanism
// that gets punk0's reporting into a single run (agentshell.go), and where
// the chat's title comes from (agentbind.go), for an agent whose events do
// not carry it.
type agentReport struct {
	via   reportVia
	title titleSource
}

// reportOff names the agents punk0 could not set reporting up for this run
// (writeAgentShell): each runs with none until punk0 restarts. It is written
// once at startup and read by every tab that starts a shell.
var reportOff = struct {
	mu   sync.Mutex
	kind map[string]bool
}{}

// agentReportOf is how kind reports to its tab this run: nil for an agent
// that does not report, and for one whose setup failed.
func agentReportOf(kind string) *agentReport {
	a, ok := tabAgents[kind]
	if !ok || a.report == nil {
		return nil
	}
	reportOff.mu.Lock()
	defer reportOff.mu.Unlock()
	if reportOff.kind[kind] {
		return nil
	}
	return a.report
}

// reportLine is bin with what one run of kind needs on the command line to
// report to its tab, for a shell punk0 could not hook: in one it did, the
// agent's own function adds it instead (writeAgentShell). A flag goes bare
// and anything else is quoted for the shell, as Claude's settings path is.
func reportLine(kind, bin string, sx shellSyntax) string {
	r := agentReportOf(kind)
	if r == nil {
		return bin
	}
	argv, _ := r.via.unhooked(kind)
	for _, a := range argv {
		if strings.HasPrefix(a, "-") && shellQuote(a) == a {
			bin += " " + a
			continue
		}
		bin += " " + sx.quote(a)
	}
	return bin
}

// tabAgentOrder is the dialog's order; tabAgents has every agent in it.
var tabAgentOrder = []string{tabClaude, "codex", tabOpenCode, "copilot", "agy", "cursor-agent", "qwen", "droid"}

var tabAgents = map[string]tabAgent{
	tabClaude:      {Label: "Claude Code", bin: "claude", report: &agentReport{via: claudeSettings{}, title: chatFileTitle{root: claudeRoot, read: readClaudeChat}}},
	"codex":        {Label: "Codex", bin: "codex", resume: []string{"resume", "--last"}, resumeID: []string{"resume"}, report: &agentReport{via: codexOverrides{}, title: chatFileTitle{root: codexRoot, read: readCodexChat, titles: "session_index.jsonl"}}},
	tabOpenCode:    {Label: "OpenCode", bin: "opencode", resume: []string{"--continue"}, resumeID: []string{"--session"}, report: &agentReport{via: opencodeVia{}}},
	"copilot":      {Label: "GitHub Copilot", bin: "copilot", resume: []string{"--continue"}, resumeID: []string{"--resume="}, joinID: true, report: &agentReport{via: pluginDir{}, title: copilotTitle{}}},
	"agy":          {Label: "Antigravity", bin: "agy"},
	"cursor-agent": {Label: "Cursor Agent", bin: "cursor-agent", resumeID: []string{"--resume"}},
	"qwen":         {Label: "Qwen Code", bin: "qwen", resume: []string{"--continue"}, resumeID: []string{"--resume"}, report: &agentReport{via: qwenDefaults{}, title: chatFileTitle{root: qwenRoot, read: readQwenTitle}}},
	"droid":        {Label: "Factory Droid", bin: "droid", resume: []string{"--last"}, resumeID: []string{"--resume"}, report: &agentReport{via: droidSettings{}, title: chatFileTitle{root: droidRoot, read: readDroidTitle}}},
}

// resumeArgs are the arguments that resume session: resumeID, then the id, or
// the id joined to the last of them for a flag that takes its value that way
// (copilot's `--resume=<id>`). nil for an agent that cannot resume by id.
func (a tabAgent) resumeArgs(session string) []string {
	if a.resumeID == nil {
		return nil
	}
	args := append([]string(nil), a.resumeID...)
	if a.joinID {
		args[len(args)-1] += session
		return args
	}
	return append(args, session)
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
// text the user wrote. session must be shell-safe (validSession). In a hooked
// shell (hookFor) the agent's function adds punk0's reporting; elsewhere
// reportLine puts it on the line here.
func agentCommand(kind, session string, first, hooked bool, sx shellSyntax) string {
	a, ok := tabAgents[kind]
	if !ok {
		return ""
	}
	bin := a.bin
	if args := strings.TrimSpace(settings.SettingOr[string]("agents." + kind + ".args")); args != "" {
		bin += " " + args
	}
	if !hooked {
		bin = reportLine(kind, bin, sx)
	}
	if kind == tabClaude {
		start := bin + " --session-id " + session
		if first {
			return start
		}
		return sx.orElse(bin+" --resume "+session, start)
	}
	if session != "" && a.resumeID != nil {
		return sx.orElse(bin+" "+strings.Join(a.resumeArgs(session), " "), bin)
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
			ResumesByID: id == tabClaude || a.resumeID != nil, Status: a.report != nil})
	}
	writeJSON(w, out)
}
