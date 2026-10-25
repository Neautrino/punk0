package main

// Agent chat history: the conversations Claude Code, Codex and OpenCode keep
// on disk, listed read-only in the agents page's Chats mode so a chat started
// in any terminal can be found again and resumed in a punk0 tab. punk0 never
// writes an agent's transcript. Removing a chat is punk0's own bookkeeping
// (records.json), and Sync takes every removal back. Deleting one permanently
// goes through the agent's own command, except Claude Code's, which has none:
// punk0 then unlinks exactly its transcript and the folder of subagent
// transcripts beside it (history_claude.go). A row expands into a preview --
// the last exchange and how the chat started -- read on demand, never by the list.

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/px0-ai/harness"
)

// chatSession is one conversation of one agent. Updated is unix ms; File is
// the transcript it was read from, empty for OpenCode, which keeps a database
// only its CLI reads. File stays off the wire: it is an absolute path the
// page has no use for.
type chatSession struct {
	Agent    string `json:"agent"`
	ID       string `json:"id"`
	Dir      string `json:"dir"`
	Title    string `json:"title"`
	Updated  int64  `json:"updated"`
	Messages int    `json:"messages"`
	File     string `json:"-"`
}

// chatCLITimeout caps an agent command: one that hangs must not hold the
// request.
const chatCLITimeout = 20 * time.Second

// agentCLI runs the agent binaries. OpenCode's listing, its preview and the
// permanent deletes need one; tests replace it so no agent runs and no real
// chat is touched.
type agentCLI interface {
	look(bin string) bool
	// run works in dir, or in punk0's own folder when dir is "": OpenCode's
	// v2 CLI answers for the project of the folder it was started in.
	run(ctx context.Context, dir, bin string, args ...string) ([]byte, error)
}

type execCLI struct{}

func (execCLI) look(bin string) bool {
	_, ok := harness.LookPath(bin)
	return ok
}

func (execCLI) run(ctx context.Context, dir, bin string, args ...string) ([]byte, error) {
	path, ok := harness.LookPath(bin)
	if !ok {
		return nil, fmt.Errorf("%s is not installed", bin)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	out, err := runCtx(ctx, cmd)
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", bin, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// runDir is where an agent command for this chat runs: its own folder while
// that folder is there, else nowhere in particular. A command started in a
// folder that is gone would not start at all.
func runDir(dir string) string {
	if isDir(dir) {
		return dir
	}
	return ""
}

// chatCacheEntry is one transcript as it was last read, with what makes it
// stale.
type chatCacheEntry struct {
	mod  time.Time
	size int64
	chat chatSession
	ok   bool // the file held a chat punk0 can list
}

// chatHistory reads the agents' history folders. Claude's and Codex's are
// files: each is parsed once and cached by path, mtime and size, so a warm
// scan costs one Stat per transcript. OpenCode keeps a database, so its
// sessions come from its CLI, asked off the request's goroutine
// (history_opencode.go).
type chatHistory struct {
	cli agentCLI // replaced in tests so no agent binary runs
	// dirs is every folder punk0 knows, for OpenCode, whose CLI lists the
	// project of the folder it runs in. Set by OpenRecords.
	dirs func() []string

	// mu is held across a scan: the cold scan is the one that costs, and two
	// pages asking at once should wait for one scan rather than run two. A
	// preview is read outside it.
	mu       sync.Mutex
	files    map[string]chatCacheEntry
	index    codexIndex // titles from Codex's session_index.jsonl
	opencode []chatSession
	asked    time.Time // when OpenCode last answered
	asking   bool
	// askGen counts the Invalidates: a round that started before one must not
	// stamp h.asked, or Sync would wait out another opencodeEvery.
	askGen   int
	previews map[string]previewEntry // by chatKey
}

func newChatHistory() *chatHistory {
	return &chatHistory{cli: execCLI{}, files: map[string]chatCacheEntry{}, previews: map[string]previewEntry{}}
}

// Sessions lists every chat on disk, most recently updated first. Nothing
// here is an error: an agent that is not installed, a history folder that is
// not there and a transcript that does not parse all read as no chats.
func (h *chatHistory) Sessions(ctx context.Context) []chatSession {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []chatSession{}
	seen := map[string]bool{}
	h.scanClaude(ctx, &out, seen)
	h.scanCodex(ctx, &out, seen)
	if ctx.Err() != nil {
		// A walk that stopped early never reached the rest of the files, so
		// what seen holds says nothing about which are gone.
		return out
	}
	for path := range h.files {
		if !seen[path] {
			delete(h.files, path) // the transcript is gone
		}
	}
	out = append(out, h.opencodeSessions()...)
	slices.SortFunc(out, func(a, b chatSession) int { return cmp.Compare(b.Updated, a.Updated) })
	return out
}

// SessionsIn is Sessions narrowed to the chats that ran in one of dirs,
// compared with symlinks resolved, since the agent recorded the folder it saw
// and punk0 stores the one git reports.
func (h *chatHistory) SessionsIn(dirs ...string) []chatSession {
	want := make(map[string]bool, len(dirs))
	for _, d := range dirs {
		want[resolvePath(filepath.Clean(d))] = true
	}
	all := h.Sessions(context.Background())
	out := make([]chatSession, 0, len(all))
	for _, c := range all {
		if want[resolvePath(filepath.Clean(c.Dir))] {
			out = append(out, c)
		}
	}
	return out
}

// Invalidate throws every cached read away, so the next list is a cold scan
// and OpenCode is asked at once. Sync is its only caller: the agents write
// these folders from outside punk0, and a user who clicks Sync is saying the
// list is behind. The rows OpenCode last answered stay until it answers
// again, so the list never blinks empty.
func (h *chatHistory) Invalidate() {
	h.mu.Lock()
	defer h.mu.Unlock()
	clear(h.files)
	clear(h.previews)
	h.index = codexIndex{}
	h.asked = time.Time{}
	h.askGen++
}

// cached returns the chat path holds, reading it only when its mtime or size
// changed since the last scan. Updated comes from the file: a transcript is
// appended to as the conversation goes on. Called with h.mu held.
func (h *chatHistory) cached(path string, info fs.FileInfo, read func(string) (chatSession, bool)) (chatSession, bool) {
	if f, ok := h.files[path]; ok && f.mod.Equal(info.ModTime()) && f.size == info.Size() {
		return f.chat, f.ok
	}
	c, ok := read(path)
	c.Updated = info.ModTime().UnixMilli()
	h.files[path] = chatCacheEntry{mod: info.ModTime(), size: info.Size(), chat: c, ok: ok}
	return c, ok
}

// userText reports whether text is one the user typed: both Claude Code and
// Codex wrap slash commands, command output and injected context in
// angle-bracket tags, which no one wrote and which name no chat; a message
// with no text at all is an attachment or a tool result.
func userText(text string) bool {
	text = strings.TrimSpace(text)
	return text != "" && !strings.HasPrefix(text, "<")
}

// promptTitle names a chat after its first prompt; chatTitle (scratch.go)
// drops slash commands and cuts at a word.
func promptTitle(text string) string {
	if !userText(text) {
		return ""
	}
	return chatTitle(text)
}

// chatKey is how one chat is named across agents: removed chats are
// remembered under it, tabs are found by it, previews are cached under it,
// and the page addresses a chat by the same two parts.
func chatKey(agent, id string) string { return agent + ":" + id }

// Delete removes a chat for good, the way its agent would: Codex and OpenCode
// have a command for it, Claude Code has none and is unlinked by hand. An
// agent with neither is an error rather than a silent no-op: the page would
// show the chat again on the next scan. The id is checked first, since it
// goes on a command line and into a path.
func (h *chatHistory) Delete(c chatSession) error {
	if !validSession.MatchString(c.ID) {
		return errors.New("punk0 will not delete a chat with an id like that")
	}
	ctx, cancel := context.WithTimeout(context.Background(), chatCLITimeout)
	defer cancel()
	var err error
	switch c.Agent {
	case tabClaude:
		err = deleteClaudeChat(c)
	case "codex":
		_, err = h.cli.run(ctx, "", "codex", "delete", c.ID)
	case "opencode":
		_, err = h.cli.run(ctx, runDir(c.Dir), "opencode", "session", "delete", c.ID)
	default:
		err = fmt.Errorf("punk0 cannot delete %s chats", cmp.Or(c.Agent, "unknown"))
	}
	if err != nil {
		return err
	}
	h.forget(c)
	return nil
}

// forget drops a deleted chat from the caches, so the list loses it at once
// rather than at the next scan of a folder that changed. An OpenCode listing
// that was already running can put its row back until the next one answers,
// at most opencodeEvery later.
func (h *chatHistory) forget(c chatSession) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.files, c.File)
	delete(h.previews, chatKey(c.Agent, c.ID))
	h.opencode = slices.DeleteFunc(h.opencode, func(o chatSession) bool { return o.Agent == c.Agent && o.ID == c.ID })
}

/* ---------- preview ---------- */

const (
	previewMax   = 600     // the last prompt, its reply and the opening prompt, in characters
	previewWhole = 8 << 20 // a transcript up to here is read whole
	previewHead  = 256 << 10
	previewTail  = 1 << 20
)

// chatPreview is what an expanded row shows: plain text only, since the page
// writes it with textContent. LastPrompt and LastReply are the last
// exchange; Started is how the chat opened, when that says something the
// title and the last prompt do not.
type chatPreview struct {
	LastPrompt string `json:"lastPrompt"`
	LastReply  string `json:"lastReply"`
	Started    string `json:"started,omitempty"`
	Messages   int    `json:"messages,omitempty"` // the list's count; OpenCode has none
	Dir        string `json:"dir"`
	Branch     string `json:"branch,omitempty"` // never HEAD: a detached head names no branch
}

// previewEntry is a preview and what makes it stale: the transcript's mtime
// and size, or, for OpenCode, the time its CLI last reported.
type previewEntry struct {
	stamp   string
	preview chatPreview
}

// Preview reads a chat's last exchange and how it started. The file is
// stat'd and read outside h.mu -- a transcript runs to megabytes and the list
// polls every ten seconds -- and the lock is taken only to look the cache
// entry up and to store it.
func (h *chatHistory) Preview(c chatSession) (chatPreview, error) {
	stamp := strconv.FormatInt(c.Updated, 10)
	if c.File != "" {
		info, err := os.Stat(c.File)
		if err != nil {
			return chatPreview{}, err
		}
		stamp = fmt.Sprintf("%d-%d", info.ModTime().UnixNano(), info.Size())
	}
	key := chatKey(c.Agent, c.ID)
	h.mu.Lock()
	e, ok := h.previews[key]
	h.mu.Unlock()
	if ok && e.stamp == stamp {
		return e.preview, nil
	}
	p := chatPreview{Messages: c.Messages, Dir: c.Dir}
	var x exchange
	var err error
	switch c.Agent {
	case tabClaude:
		err = readClaudePreview(c.File, &p, &x)
	case "codex":
		err = readCodexPreview(c.File, &p, &x)
	case "opencode":
		err = h.readOpenCodePreview(c, &p, &x)
	default:
		err = fmt.Errorf("punk0 cannot read %s chats", cmp.Or(c.Agent, "unknown"))
	}
	if err != nil {
		return chatPreview{}, err
	}
	x.fill(&p, c.Title)
	if p.Branch == "HEAD" {
		p.Branch = ""
	}
	h.mu.Lock()
	h.previews[key] = previewEntry{stamp: stamp, preview: p}
	h.mu.Unlock()
	return p, nil
}

// previewText is one message as the preview shows it: trimmed and cut to n
// characters, with an ellipsis saying there was more.
func previewText(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimRight(string(r[:n]), " \t\n") + "…"
}

// exchange follows a conversation as it is scanned, keeping only what the
// preview shows: the first prompt, the last one, and the reply after it --
// every text block the agent said since, which Claude spreads over several
// records with tool calls between them. The reply stops growing once it is
// past what the preview shows, so a transcript of any length costs three
// strings.
type exchange struct {
	first, prompt string
	seen          bool // a prompt was taken, so first is settled
	reply         []string
	replyLen      int
}

// user takes a prompt the user typed, by the title's rules: no wrappers, no
// slash commands. It starts a new reply.
func (x *exchange) user(text string) {
	if promptTitle(text) == "" {
		return
	}
	text = strings.TrimSpace(text)
	if !x.seen {
		x.seen = true
		// A greeting says nothing about the chat: only a prompt of three
		// words or more is worth showing as its start.
		if len(strings.Fields(text)) >= 3 {
			x.first = previewText(text, previewMax)
		}
	}
	x.prompt = text
	x.reply, x.replyLen = x.reply[:0], 0
}

// agent adds a text block the agent said to the reply after the last prompt.
func (x *exchange) agent(text string) {
	text = strings.TrimSpace(text)
	if text == "" || !x.seen || x.replyLen > previewMax {
		return
	}
	x.reply = append(x.reply, text)
	x.replyLen += len([]rune(text)) + 2
}

// fill writes the exchange into p. Started is left out when it would only
// repeat the title or the last prompt.
func (x *exchange) fill(p *chatPreview, title string) {
	p.LastPrompt = previewText(x.prompt, previewMax)
	p.LastReply = previewText(strings.Join(x.reply, "\n\n"), previewMax)
	same := func(a, b string) bool { return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) }
	if x.first != "" && !same(x.first, title) && !same(x.first, p.LastPrompt) {
		p.Started = x.first
	}
}

// previewLines calls fn for each line of a JSONL transcript, oldest first. A
// file past previewWhole is read as its head and its tail only -- the first
// prompt is at the head, the last exchange at the tail -- and the tail starts
// after the first newline at or past its offset, so no half record is parsed.
// The head's last line is half a record and simply fails to parse.
func previewLines(path string, fn func(line []byte)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	scan := func(r io.Reader) {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64<<10), claudeLineMax)
		for sc.Scan() {
			fn(sc.Bytes())
		}
	}
	if info.Size() <= previewWhole {
		scan(f)
		return nil
	}
	scan(io.LimitReader(f, previewHead))
	if _, err := f.Seek(info.Size()-previewTail, io.SeekStart); err != nil {
		return err
	}
	tail := bufio.NewReader(f)
	if _, err := tail.ReadBytes('\n'); err != nil {
		return nil // nothing whole in the tail
	}
	scan(tail)
	return nil
}

/* ---------- HTTP ---------- */

// historyView is a chat as Chats mode lists it: what is on disk, plus
// what punk0 knows about the folder it ran in and the workspace it is in.
type historyView struct {
	chatSession
	Removed   bool   `json:"removed,omitempty"` // taken out of the list by the user
	Open      bool   `json:"open,omitempty"`    // a tab resumes this chat
	Missing   bool   `json:"missing,omitempty"` // its folder is gone
	Workspace string `json:"workspace,omitempty"`
}

// historyWorkspace is a workspace an editor's Chats mode cuts its list by:
// here when it runs in the editor's folder, else the workspace a listed chat
// is filed under, or a worktree listed under one of those; Href opens its
// folder's editor.
type historyWorkspace struct {
	wsRef
	Here bool   `json:"here,omitempty"`
	Href string `json:"href"`
}

// handleHistory lists the agents' chats, most recently updated first.
// scope=workspace|project|all is how wide the list is around ws (a
// workspace id, or an editor's folder backend id: WorkspaceAt), q matches
// the title, and removed=1 lists the chats the user removed as well. A chat is
// in a scope when it ran in one of the scope's folders or is filed under one
// of the workspaces there (Records.chatWorkspaces); its workspace is the one
// it is filed under, else the first running in its folder. scope=workspace
// also names the workspaces its list is cut by, and the project.
func (wb *workbench) handleHistory(w http.ResponseWriter, r *http.Request) {
	if !trustedGet(w, r) {
		return
	}
	q := r.URL.Query()
	scope := cmp.Or(q.Get("scope"), "all")
	if scope != "all" && scope != "workspace" && scope != "project" {
		fail(w, http.StatusBadRequest, "unknown scope "+scope)
		return
	}
	refs := wb.records.WorkspaceRefs()
	known := make(map[string]wsRef, len(refs))
	for _, x := range refs {
		known[x.ID] = x
	}
	members := wb.records.ChatWorkspaces()
	sessions := wb.records.Chats().Sessions(r.Context())
	var ws *Workspace
	here := map[string]bool{} // the scope's workspaces
	if scope != "all" {
		if ws = wb.records.WorkspaceAt(q.Get("ws")); ws == nil {
			fail(w, http.StatusNotFound, "workspace not found")
			return
		}
		pid := known[ws.ID].Project
		folders := map[string]bool{}
		for _, x := range refs {
			if (scope == "project" && x.Project == pid) || (scope == "workspace" && sameDir(x.Dir, ws.Dir)) {
				here[x.ID] = true
				folders[resolvePath(filepath.Clean(x.Dir))] = true
			}
		}
		if scope == "project" {
			for _, d := range wb.records.ProjectDirsOf(ws.ID) {
				folders[resolvePath(filepath.Clean(d))] = true
			}
		}
		sessions = slices.DeleteFunc(sessions, func(c chatSession) bool {
			return !folders[resolvePath(filepath.Clean(c.Dir))] && !here[members[chatKey(c.Agent, c.ID)]]
		})
	}
	removed, byDir, tabs := wb.records.RemovedChats(), wb.records.WorkspaceByDir(), wb.records.TabSessions()
	match, showRemoved := strings.ToLower(strings.TrimSpace(q.Get("q"))), q.Get("removed") == "1"
	out := make([]historyView, 0, len(sessions))
	for _, c := range sessions {
		key := chatKey(c.Agent, c.ID)
		gone := removed[key]
		if gone && !showRemoved {
			continue
		}
		if match != "" && !strings.Contains(strings.ToLower(c.Title), match) {
			continue
		}
		in := members[key]
		if _, ok := known[in]; !ok {
			in = workspaceIn(byDir, c.Dir)
		}
		out = append(out, historyView{
			chatSession: c,
			Removed:     gone,
			Open:        tabs[key] != "",
			Missing:     !isDir(c.Dir),
			Workspace:   in,
		})
	}
	if scope != "workspace" {
		writeJSON(w, map[string]any{"sessions": out})
		return
	}
	writeJSON(w, map[string]any{
		"sessions":   out,
		"workspaces": wb.historyWorkspaces(refs, here, out),
		"project":    map[string]string{"id": known[ws.ID].Project, "name": known[ws.ID].ProjectName},
	})
}

// historyWorkspaces lists, in records order, the workspaces an editor's list
// is cut by: the folder's own (here), every worktree listed under them at any
// depth, and every workspace a listed chat is filed under.
func (wb *workbench) historyWorkspaces(refs []wsRef, here map[string]bool, rows []historyView) []historyWorkspace {
	want := maps.Clone(here)
	for _, row := range rows {
		if row.Workspace != "" {
			want[row.Workspace] = true
		}
	}
	for _, x := range refs {
		if x.Parent != "" && descendsFrom(refs, x.Parent, here) {
			want[x.ID] = true
		}
	}
	out := []historyWorkspace{}
	for _, x := range refs {
		if want[x.ID] {
			out = append(out, historyWorkspace{wsRef: x, Here: here[x.ID], Href: wb.childHref(wbItemID("dir", x.Dir))})
		}
	}
	return out
}

// descendsFrom reports whether workspace id is one of here or listed, at any
// depth, under one of them. The walk is bounded by len(refs), so a cycle the
// records could not hold anyway cannot loop.
func descendsFrom(refs []wsRef, id string, here map[string]bool) bool {
	for range refs {
		if here[id] {
			return true
		}
		i := slices.IndexFunc(refs, func(x wsRef) bool { return x.ID == id })
		if i < 0 || refs[i].Parent == "" {
			return false
		}
		id = refs[i].Parent
	}
	return false
}

// handleHistoryRemove takes a chat out of Chats mode or lists it again.
// removed is optional: without it the chat is removed, so the page sends
// false only to bring one back. Nothing on disk is touched, and the chat's
// tabs keep running: the row simply stops being listed.
func (wb *workbench) handleHistoryRemove(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var body struct {
		Agent   string `json:"agent"`
		ID      string `json:"id"`
		Removed *bool  `json:"removed"`
	}
	if readJSONBody(r, &body) != nil || body.Agent == "" || body.ID == "" {
		fail(w, http.StatusBadRequest, "agent and id are required")
		return
	}
	removed := body.Removed == nil || *body.Removed
	if err := wb.records.RemoveChat(body.Agent, body.ID, removed); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"removed": removed})
}

// handleHistorySync re-reads the agents' folders now and takes every removal
// back: Sync is the one button that says "show me what is really there".
func (wb *workbench) handleHistorySync(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	wb.records.Chats().Invalidate()
	n, err := wb.records.RestoreAllChats()
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"restored": n})
}

// handleHistoryMove files chats under a workspace: Move to workspace in an
// editor's Chats mode. The chats themselves, and any tab on them, stay as
// they are; resuming one later opens it in that workspace.
func (wb *workbench) handleHistoryMove(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var body struct {
		Chats []struct {
			Agent string `json:"agent"`
			ID    string `json:"id"`
		} `json:"chats"`
		Workspace string `json:"workspace"`
	}
	if readJSONBody(r, &body) != nil || len(body.Chats) == 0 || body.Workspace == "" {
		fail(w, http.StatusBadRequest, "chats and workspace are required")
		return
	}
	keys := make([]string, 0, len(body.Chats))
	for _, c := range body.Chats {
		if c.Agent == "" || c.ID == "" {
			fail(w, http.StatusBadRequest, "each chat needs its agent and id")
			return
		}
		keys = append(keys, chatKey(c.Agent, c.ID))
	}
	if err := wb.records.MoveChats(keys, body.Workspace); err != nil {
		failRecord(w, err)
		return
	}
	writeJSON(w, map[string]any{"workspace": body.Workspace})
}

// handleHistoryPreview reads the chat's last exchange and how it started, on
// demand: the ten-second list never reads this much. A chat the user removed
// previews like any other -- it is still on disk -- so 404 here means the
// transcript is gone. An agent command that failed is 502 in its own words;
// a transcript punk0 could not read is this server's own 500.
func (wb *workbench) handleHistoryPreview(w http.ResponseWriter, r *http.Request) {
	if !trustedGet(w, r) {
		return
	}
	c, ok := wb.chat(r.Context(), r.PathValue("agent"), r.PathValue("id"))
	if !ok {
		fail(w, http.StatusNotFound, "chat not found")
		return
	}
	p, err := wb.records.Chats().Preview(c)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			fail(w, http.StatusNotFound, "chat not found")
			return
		}
		if c.Agent == "opencode" { // the only preview that runs a command
			fail(w, http.StatusBadGateway, err.Error())
			return
		}
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, p)
}

// handleHistoryOpen resumes a chat in a tab of the workspace running in its
// folder, reusing the tab already on that conversation. A folder punk0 does
// not work in has no tabs to open one in, so the answer says what would
// change that: adopting the worktree it is, or adding it as a project. The
// page asks before doing either.
func (wb *workbench) handleHistoryOpen(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var body struct {
		Agent string `json:"agent"`
		ID    string `json:"id"`
	}
	if readJSONBody(r, &body) != nil || body.Agent == "" || body.ID == "" {
		fail(w, http.StatusBadRequest, "agent and id are required")
		return
	}
	c, ok := wb.chat(r.Context(), body.Agent, body.ID)
	if !ok {
		fail(w, http.StatusNotFound, "chat not found")
		return
	}
	if tab, ws, _ := wb.records.SessionTab(c.Agent, c.ID); tab != "" {
		writeJSON(w, map[string]any{"tab": tab, "workspace": ws})
		return
	}
	if !isDir(c.Dir) {
		fail(w, http.StatusConflict, "the folder this chat ran in is gone: "+tildePath(c.Dir))
		return
	}
	dir := resolvePath(filepath.Clean(c.Dir))
	// The workspace it is filed under, when that runs in its folder: local
	// workspaces share one, and the first of them is only the fallback.
	ws := wb.records.Workspace(wb.records.ChatWorkspaces()[chatKey(c.Agent, c.ID)])
	if ws == nil || !sameDir(ws.Dir, dir) {
		ws = wb.records.Workspace(workspaceIn(wb.records.WorkspaceByDir(), dir))
	}
	if ws == nil {
		wb.offerFolder(w, dir)
		return
	}
	t, err := wb.records.AddTab(ws.ID, c.Agent, c.ID)
	if err != nil {
		failRecord(w, err)
		return
	}
	writeJSON(w, map[string]any{"tab": t.ID, "workspace": ws.ID})
}

// offerFolder answers a chat whose folder punk0 does not work in: a worktree
// of a registered project is adopted in place (newworkspace.go), and a folder
// that can become a project of its own is offered as one. Anything else --
// a folder inside a workspace or a repository, the home folder -- is a plain
// error naming the folder: adding it would register another folder, leaving
// the chat where it was, so the page must not offer it.
func (wb *workbench) offerFolder(w http.ResponseWriter, dir string) {
	for _, p := range wb.records.ProjectFolders() {
		// Worktrees runs git, so it is called outside every lock.
		if wt := worktreeAt(wb.records.Worktrees(p.dir), dir); wt != nil && wt.Workspace == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]any{"adopt": dir, "project": p.id, "error": "this chat ran in a worktree punk0 does not know yet"})
			return
		}
	}
	if _, label := wb.records.WorkspaceAround(dir); label != "" {
		fail(w, http.StatusConflict, "this chat ran in "+tildePath(dir)+", a folder inside the workspace "+label+": resume it from a terminal there")
		return
	}
	if why := notAProject(dir); why != "" {
		fail(w, http.StatusConflict, "punk0 cannot open a chat that ran in "+tildePath(dir)+": "+why)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	json.NewEncoder(w).Encode(map[string]any{"addProject": dir, "error": "this chat ran outside every punk0 project"})
}

// notAProject says why adding dir as a project would not give the chat a
// workspace in that very folder, or "" when it would: a folder inside a
// repository registers the repository instead, and the home folder and the
// filesystem root are refused (AddProject), so offering either would send the
// page round a loop that cannot end.
func notAProject(dir string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" && sameDir(dir, home) {
		return "a whole home folder cannot be a project"
	}
	if dir == filepath.Dir(dir) {
		return "the filesystem root cannot be a project"
	}
	top, err := projectFolder(dir)
	if err != nil {
		return err.Error()
	}
	if !sameDir(top, dir) {
		return "it is a folder inside the repository " + tildePath(top) + ", which is what punk0 would add"
	}
	return ""
}

// handleHistoryDelete deletes a chat for good (Delete). Every tab on that
// conversation is closed first and in one go (CloseSessionTabs): an agent
// still holding the session would write the transcript back. A tab that is
// somehow still registered afterwards refuses the delete rather than racing
// it.
func (wb *workbench) handleHistoryDelete(w http.ResponseWriter, r *http.Request) {
	if !localOrigin(w, r) {
		return
	}
	agent, id := r.PathValue("agent"), r.PathValue("id")
	c, ok := wb.chat(r.Context(), agent, id)
	if !ok {
		fail(w, http.StatusNotFound, "chat not found")
		return
	}
	if _, err := wb.records.CloseSessionTabs(agent, id); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tab, _, _ := wb.records.SessionTab(agent, id); tab != "" {
		fail(w, http.StatusConflict, "a tab is still on this chat; close it and try again")
		return
	}
	if err := wb.records.Chats().Delete(c); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Nothing left to remember: not its removal, not its workspace.
	if err := wb.records.RemoveChat(agent, id, false); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := wb.records.ForgetChat(agent, id); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// chat is the one chat on disk agent and id name, removed or not.
func (wb *workbench) chat(ctx context.Context, agent, id string) (chatSession, bool) {
	for _, c := range wb.records.Chats().Sessions(ctx) {
		if c.Agent == agent && c.ID == id {
			return c, true
		}
	}
	return chatSession{}, false
}
