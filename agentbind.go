package main

// A tab is bound to the agent running in it, and only while it runs: the
// agent's shell function says when it starts and exits (agentshell.go), and
// its hooks say which chat it is in and what that chat is called. The tab's
// Kind is that agent, or "shell" when none runs; its Session and Title are
// the chat's. A tab whose shell dies with the agent still in it keeps them,
// so its next start resumes that chat (agentCommand).

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// The events the shell functions report (`punk0 hook start|exit <agent>`).
const (
	agentStartEvent = "punk0.start"
	agentExitEvent  = "punk0.exit"
)

// tabLabel is what a tab is called where the page cannot work it out itself
// (web/src/term-util.js getTabLabel does the same): the user's name for it,
// else its chat's title, else what runs in it.
func tabLabel(kind, name, title string) string {
	if a, ok := tabAgents[kind]; ok {
		return cmp.Or(name, title, a.Label)
	}
	return cmp.Or(name, title, kind)
}

// kind is what runs in the tab now, for callers that do not hold Records.mu.
func (s *Satellite) kind() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Kind
}

// AgentStarted binds the tab to agent. The same agent starting again keeps
// the chat it was given (a tab punk0 started on a session reports its start
// too); another one starts with none until its hooks say.
func (r *Records) AgentStarted(tabID, agent string) error {
	if _, ok := tabAgents[agent]; !ok {
		return fmt.Errorf("unknown agent %q", agent)
	}
	return r.bindLocked(tabID, func(t *Satellite) bool {
		if t.Kind == agent {
			return false
		}
		t.Kind, t.Session, t.Title, t.status = agent, "", "", ""
		return true
	})
}

// AgentExited unbinds the tab when the agent it runs quits: what is left at
// the prompt is a shell. A shell punk0 is closing keeps its agent, which is
// what its next start resumes.
func (r *Records) AgentExited(tabID, agent string) error {
	return r.bindLocked(tabID, func(t *Satellite) bool {
		if t.Kind != agent || t.sh == nil || t.sh.closed.Load() {
			return false
		}
		t.Kind, t.Session, t.Title, t.status, t.newChat = tabShell, "", "", "", false
		return true
	})
}

// AgentSession records the chat the tab's agent says it is in: a new chat
// after /clear, or the first one an agent started by hand names.
func (r *Records) AgentSession(tabID, agent, session string) error {
	if !validSession.MatchString(session) {
		return nil
	}
	return r.bindLocked(tabID, func(t *Satellite) bool {
		if t.Kind != agent || t.Session == session {
			return false
		}
		t.Session, t.Title, t.newChat = session, "", false
		return true
	})
}

// AgentTitle records the title of the tab's chat, if that is still its chat.
func (r *Records) AgentTitle(tabID, agent, session, title string) error {
	title = truncateLabel(strings.TrimSpace(title), 80)
	return r.bindLocked(tabID, func(t *Satellite) bool {
		if t.Kind != agent || t.Session != session || t.Title == title {
			return false
		}
		t.Title = title
		return true
	})
}

// bindLocked applies change to tab id under both locks (the tab's fields are
// read under either) and saves when it changed something.
func (r *Records) bindLocked(id string, change func(*Satellite) bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, i := r.tabLocked(id)
	if w == nil {
		return fmt.Errorf("tab %q: %w", id, errNotFound)
	}
	t := w.Tabs[i]
	t.mu.Lock()
	changed := change(t)
	t.mu.Unlock()
	if !changed {
		return nil
	}
	r.fileTabLocked(w, t)
	return r.saveLocked()
}

// titleSource is where the title of one agent's chat comes from, for an agent
// whose events do not carry it (refreshTitle). h caches the transcripts punk0
// parses; "" is no title yet, which leaves the tab's as it was.
type titleSource interface {
	title(h *chatHistory, ev hookEvent) string
}

// chatFileTitle reads the title from the chat file a hook named, through the
// cache the Chats panel reads with. Only a file under the agent's own folder
// is read, for a chat id that could be one (validSession): both come from a
// hook. titles, where the agent keeps one, is its own list of chat names
// under that folder, which wins over the file's (Codex records a renamed
// chat there).
type chatFileTitle struct {
	root   func() string
	read   func(string) (chatSession, bool)
	titles string
}

func (t chatFileTitle) title(h *chatHistory, ev hookEvent) string {
	root := t.root()
	if root == "" || ev.Transcript == "" || !within(root, ev.Transcript) || !validSession.MatchString(ev.Session) {
		return ""
	}
	info, err := os.Stat(ev.Transcript)
	if err != nil {
		return ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	c, ok := h.cached(ev.Transcript, info, t.read)
	if t.titles != "" {
		h.loadCodexIndex(filepath.Join(root, t.titles))
		if e, named := h.index.entries[ev.Session]; named && e.title != "" {
			return e.title
		}
	}
	if !ok || c.Title == "(untitled)" {
		return ""
	}
	return c.Title
}

// qwenRoot is where Qwen Code keeps its state: QWEN_HOME (a leading ~ is the
// home folder, as qwen reads it), else ~/.qwen. Its chats are
// projects/<folder>/chats/<id>.jsonl under it. A chat qwen keeps elsewhere
// (QWEN_RUNTIME_DIR) gets no title: punk0 does not follow that variable.
func qwenRoot() string {
	if d := os.Getenv("QWEN_HOME"); strings.TrimSpace(d) != "" {
		// A relative path stays relative, which within never matches.
		d, err := expandPath(d, "")
		if err != nil {
			return ""
		}
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".qwen")
}

var qwenTitleRecord = []byte(`"subtype":"custom_title"`)

// readQwenTitle reads a qwen chat's title: the last title record, which
// qwen appends for its own title and for each /rename.
func readQwenTitle(path string) (chatSession, bool) {
	f, err := os.Open(path)
	if err != nil {
		return chatSession{}, false
	}
	defer f.Close()
	var c chatSession
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), claudeLineMax)
	for sc.Scan() {
		if !bytes.Contains(sc.Bytes(), qwenTitleRecord) {
			continue
		}
		var r struct {
			Type    string `json:"type"`
			Subtype string `json:"subtype"`
			Payload struct {
				Title string `json:"customTitle"`
			} `json:"systemPayload"`
		}
		if json.Unmarshal(sc.Bytes(), &r) == nil && r.Type == "system" && r.Subtype == "custom_title" && r.Payload.Title != "" {
			c.Title = r.Payload.Title
		}
	}
	return c, true
}

// droidRoot is where droid keeps its chats: ~/.factory/sessions/<folder>/<id>.jsonl.
func droidRoot() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".factory", "sessions")
}

// readDroidTitle reads a droid chat's title from its first line, the
// session_start record; "New Session" is droid's name for a chat with none.
func readDroidTitle(path string) (chatSession, bool) {
	f, err := os.Open(path)
	if err != nil {
		return chatSession{}, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), claudeLineMax)
	var r struct {
		Type  string `json:"type"`
		Title string `json:"title"`
	}
	if !sc.Scan() || json.Unmarshal(sc.Bytes(), &r) != nil || r.Type != "session_start" || r.Title == "New Session" {
		return chatSession{}, false
	}
	return chatSession{Title: r.Title}, true
}

// copilotTitle reads a copilot chat's name from the workspace.yaml copilot
// keeps beside it, found by the event's id: only Stop names a transcript.
// The id comes from a hook, so it must be one (validSession) and stay in
// copilot's folder before it becomes a path. punk0 parses no YAML: the name
// is one top-level line.
type copilotTitle struct{}

func (copilotTitle) title(_ *chatHistory, ev hookEvent) string {
	root := copilotRoot()
	if root == "" || !validSession.MatchString(ev.Session) {
		return ""
	}
	p := filepath.Join(root, ev.Session, "workspace.yaml")
	if !within(root, p) {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	for line := range strings.Lines(string(b)) {
		if v, ok := strings.CutPrefix(strings.TrimRight(line, "\r\n"), "name: "); ok {
			return yamlScalar(v)
		}
	}
	return ""
}

// copilotRoot is where copilot keeps its chats: $COPILOT_HOME, else
// ~/.copilot, as copilot resolves it (app.js: configDir ?? COPILOT_HOME ??
// ~/.copilot). A --config-dir in agents.copilot.args moves the chats where
// punk0 does not look, so such a tab gets no title.
func copilotRoot() string {
	home := strings.TrimSpace(os.Getenv("COPILOT_HOME"))
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		home = filepath.Join(h, ".copilot")
	}
	return filepath.Join(home, "session-state")
}

// yamlScalar is the text of a one-line YAML scalar: copilot quotes a name
// holding ':' or '#', single quotes doubling a quote inside, double quotes
// escaping it.
func yamlScalar(v string) string {
	v = strings.TrimSpace(v)
	if len(v) < 2 {
		return v
	}
	switch q := v[0]; {
	case q == '\'' && v[len(v)-1] == q:
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	case q == '"' && v[len(v)-1] == q:
		return strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(v[1 : len(v)-1])
	}
	return v
}

// within reports whether path is inside dir, both cleaned and absolute.
func within(dir, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	return err == nil && filepath.IsAbs(path) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// refreshTitle looks the tab's chat title up after an event that may have
// changed it: OpenCode says it in the event, the others in a file of their
// own (agentReport.title, read straight from the table: a title comes from
// the agent's own folder, not from a file punk0 wrote).
func (wb *workbench) refreshTitle(tabID, agent string, ev hookEvent) {
	if ev.Session == "" {
		return
	}
	title := ev.Title
	if title == "" {
		if r := tabAgents[agent].report; r != nil && r.title != nil {
			title = r.title.title(wb.records.Chats(), ev)
		}
	}
	if title == "" {
		return
	}
	if err := wb.records.AgentTitle(tabID, agent, ev.Session, title); err != nil {
		slog.Warn("could not keep the chat title", "tab", tabID, "err", err)
	}
}
