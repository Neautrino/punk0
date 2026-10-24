package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ledgerRepo registers a clean repository holding a.txt, q.txt and old.txt
// as a project, and gives its local workspace one claude tab.
func ledgerRepo(t *testing.T) (*workbench, string, *Workspace, *chatSim) {
	t.Helper()
	newTestStore(t)
	repo := cleanRepo(t)
	for _, name := range []string{"a.txt", "q.txt", "old.txt"} {
		writeFile(t, filepath.Join(repo, name), name+" one\n")
	}
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-q", "-m", "files")
	wb := testWorkbench(t, "/", t.TempDir(), "", nil)
	_, ws, err := wb.records.Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	return wb, repo, ws, newChat(t, wb, ws)
}

// chatSim plays a Claude Code session in a tab, sending the hook events it
// would, with the payloads it sends them with.
type chatSim struct {
	t          *testing.T
	wb         *workbench
	tab        string
	cwd        string
	transcript string
}

func newChat(t *testing.T, wb *workbench, ws *Workspace) *chatSim {
	t.Helper()
	tab, err := wb.records.AddTab(ws.ID, tabClaude)
	if err != nil {
		t.Fatal(err)
	}
	return &chatSim{t: t, wb: wb, tab: tab.ID, cwd: ws.Dir, transcript: filepath.Join(t.TempDir(), tab.Session+".jsonl")}
}

func (c *chatSim) send(event string, extra map[string]any) {
	c.t.Helper()
	ev := map[string]any{
		"session_id":      "sess-" + c.tab,
		"transcript_path": c.transcript,
		"cwd":             c.cwd,
		"permission_mode": "default",
		"hook_event_name": event,
	}
	maps.Copy(ev, extra)
	b, err := json.Marshal(ev)
	if err != nil {
		c.t.Fatal(err)
	}
	if w := postJSON(c.t, c.wb, "/api/tabs/"+c.tab+"/hook", string(b)); w.Code != http.StatusOK {
		c.t.Fatalf("%s hook: %d %s", event, w.Code, w.Body.String())
	}
	c.wb.ledgers.wait()
}

func (c *chatSim) prompt(text string) {
	c.t.Helper()
	c.log(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": text}})
	c.send("UserPromptSubmit", map[string]any{"prompt": text})
}

// edit writes a file the way the Edit tool does, and reports it.
func (c *chatSim) edit(rel, content string) {
	c.t.Helper()
	path := filepath.Join(c.cwd, rel)
	writeFile(c.t, path, content)
	c.send("PostToolUse", map[string]any{
		"tool_name":     "Edit",
		"tool_input":    map[string]any{"file_path": path, "old_string": "x", "new_string": "y"},
		"tool_response": map[string]any{"filePath": path, "success": true},
		"tool_use_id":   "toolu_01",
	})
}

func (c *chatSim) stop(reply string) {
	c.t.Helper()
	c.log(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{
		map[string]any{"type": "tool_use", "id": "toolu_01", "name": "Edit", "input": map[string]any{}},
	}}})
	c.log(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{
		map[string]any{"type": "text", "text": reply},
	}}})
	c.log(map[string]any{"type": "system", "subtype": "stop_hook_summary"})
	c.send("Stop", map[string]any{"stop_hook_active": false})
}

func (c *chatSim) log(line map[string]any) {
	c.t.Helper()
	b, _ := json.Marshal(line)
	f, err := os.OpenFile(c.transcript, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		c.t.Fatal(err)
	}
	defer f.Close()
	f.Write(append(b, '\n'))
}

func (c *chatSim) turnID(n int) string { return c.tab + "-" + string(rune('0'+n)) }

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func getLedger(t *testing.T, wb *workbench, dir string) ledgerView {
	t.Helper()
	var v ledgerView
	getJSON(t, wb, "/api/folders/"+wbItemID("dir", dir)+"/ledger", &v)
	return v
}

// claimed maps each listed turn to its files, as "path:status".
func claimed(v ledgerView) map[string][]string {
	out := map[string][]string{}
	for _, g := range v.Groups {
		for _, f := range g.Files {
			out[g.Turn.ID] = append(out[g.Turn.ID], f.Path+":"+f.Status)
		}
	}
	return out
}

func outside(v ledgerView) []string {
	var out []string
	for _, f := range v.Outside {
		out = append(out, f.Path+":"+f.Status)
	}
	return out
}

func groupOf(t *testing.T, v ledgerView, turnID string) groupView {
	t.Helper()
	for _, g := range v.Groups {
		if g.Turn.ID == turnID {
			return g
		}
	}
	t.Fatalf("no group for %s in %+v", turnID, claimed(v))
	return groupView{}
}

func equalLists(got, want []string) bool {
	return slices.Equal(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(want)))
}

// Snapshots go through a copy of the index: what the user staged stays
// staged as it was, and HEAD, the branches and the stash are left alone.
func TestLedgerSnapshotsLeaveRepositoryAlone(t *testing.T) {
	wb, repo, _, chat := ledgerRepo(t)
	writeFile(t, filepath.Join(repo, "staged.txt"), "staged\n")
	runGit(t, repo, "add", "staged.txt")
	writeFile(t, filepath.Join(repo, "a.txt"), "a.txt one\nunstaged\n")
	index := filepath.Join(repo, ".git", "index")
	idxBefore, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	state := func() string {
		return runGit(t, repo, "ls-files", "-s") + "\n" +
			runGit(t, repo, "diff", "--cached", "--name-status") + "\n" +
			runGit(t, repo, "rev-parse", "HEAD") + "\n" +
			runGit(t, repo, "for-each-ref", "refs/heads", "refs/stash") + "\n" +
			runGit(t, repo, "stash", "list")
	}
	before := state()

	chat.prompt("tidy up")
	chat.edit("q.txt", "q.txt two\n")
	writeFile(t, filepath.Join(repo, "untracked.txt"), "new\n")
	chat.stop("Tidied.")
	getLedger(t, wb, repo)

	if idxAfter, _ := os.ReadFile(index); !bytes.Equal(idxAfter, idxBefore) {
		t.Fatal("the real index file changed")
	}
	if after := state(); after != before {
		t.Fatalf("repository state changed:\n%s\n---\n%s", before, after)
	}
	snap := runGit(t, repo, "rev-parse", turnRefPrefix+chat.turnID(1))
	if got := runGit(t, repo, "show", snap+":untracked.txt"); got != "new" {
		t.Fatalf("after snapshot lacks the untracked file: %q", got)
	}
	if got := runGit(t, repo, "show", snap+":staged.txt"); got != "staged" {
		t.Fatalf("after snapshot lacks the staged file: %q", got)
	}
	if got := runGit(t, repo, "show", snap+"^:a.txt"); got != "a.txt one\nunstaged" {
		t.Fatalf("before snapshot (the after's parent) = %q, want the unstaged edit", got)
	}
	if got, head := runGit(t, repo, "rev-parse", snap+"^^"), runGit(t, repo, "rev-parse", "HEAD"); got != head {
		t.Fatalf("before snapshot's parent %s, want HEAD %s", got, head)
	}
}

// A turn's files are what changed between its snapshots, with line counts
// and the blob on each side.
func TestLedgerTurnFiles(t *testing.T) {
	wb, repo, ws, chat := ledgerRepo(t)
	chat.prompt("rework the files")
	chat.edit("a.txt", "a.txt one\nadded\nadded too\n")
	chat.edit("new.txt", "brand new\n")
	os.Remove(filepath.Join(repo, "old.txt"))
	chat.stop("Reworked.")

	turns := wb.ledgers.get(ws.Dir).turns()
	if len(turns) != 1 {
		t.Fatalf("got %d turns", len(turns))
	}
	tn := turns[0]
	if tn.ID != chat.turnID(1) || tn.N != 1 || tn.Prompt != "rework the files" || tn.Reply != "Reworked." || tn.Ended < tn.Started {
		t.Fatalf("turn = %+v", tn)
	}
	byPath := map[string]turnFile{}
	for _, f := range tn.Files {
		byPath[f.Path] = f
	}
	if len(byPath) != 3 {
		t.Fatalf("files = %+v", tn.Files)
	}
	head := func(p string) string { return runGit(t, repo, "rev-parse", "HEAD:"+p) }
	if f := byPath["a.txt"]; f.Status != "M" || f.Add != 2 || f.Del != 0 || f.Before != head("a.txt") || f.After == "" {
		t.Errorf("a.txt = %+v", f)
	}
	if f := byPath["new.txt"]; f.Status != "A" || f.Add != 1 || f.Before != "" || f.After != runGit(t, repo, "rev-parse", tn.After+":new.txt") {
		t.Errorf("new.txt = %+v", f)
	}
	if f := byPath["old.txt"]; f.Status != "D" || f.Del != 1 || f.Before != head("old.txt") || f.After != "" {
		t.Errorf("old.txt = %+v", f)
	}
	if !slices.Equal(tn.Tools, []string{"a.txt", "new.txt"}) {
		t.Errorf("tools = %v", tn.Tools)
	}
	v := getLedger(t, wb, repo)
	if got := claimed(v)[tn.ID]; !equalLists(got, []string{"a.txt:M", "new.txt:A", "old.txt:D"}) {
		t.Errorf("claimed = %v", got)
	}
}

// Turns that ran at the same time own only what their tools wrote; anything
// else they saw change is nobody's. A turn on its own owns all it changed.
func TestLedgerAttribution(t *testing.T) {
	wb, repo, ws, one := ledgerRepo(t)
	two := newChat(t, wb, ws)
	one.prompt("edit a")
	two.prompt("edit q")
	one.edit("a.txt", "a.txt two\n")
	two.edit("q.txt", "q.txt two\n")
	writeFile(t, filepath.Join(repo, "c.txt"), "from a shell command\n")
	one.stop("Edited a.")
	two.stop("Edited q.")

	one.prompt("format the code")
	writeFile(t, filepath.Join(repo, "d.txt"), "formatter output\n") // through Bash: no tool event
	one.stop("Formatted.")

	v := getLedger(t, wb, repo)
	want := map[string][]string{
		one.turnID(1): {"a.txt:M"},
		two.turnID(1): {"q.txt:M"},
		one.turnID(2): {"d.txt:A"},
	}
	got := claimed(v)
	if len(got) != len(want) {
		t.Fatalf("claimed = %v, want %v", got, want)
	}
	for id, files := range want {
		if !equalLists(got[id], files) {
			t.Errorf("%s claims %v, want %v", id, got[id], files)
		}
	}
	if !equalLists(outside(v), []string{"c.txt:A"}) {
		t.Errorf("outside = %v", outside(v))
	}
	// Both overlapping turns saw c.txt change all the same.
	for _, tn := range wb.ledgers.get(ws.Dir).turns()[:2] {
		if tn.file("c.txt") == nil {
			t.Errorf("%s files lack c.txt: %+v", tn.ID, tn.Files)
		}
	}
	// Newest first.
	if ids := []string{v.Groups[0].Turn.ID, v.Groups[1].Turn.ID, v.Groups[2].Turn.ID}; !slices.Equal(ids, []string{one.turnID(2), two.turnID(1), one.turnID(1)}) {
		t.Errorf("group order = %v", ids)
	}
}

// Two chats working at once on the same file, both from what HEAD holds: each
// claims it and names the other.
func TestLedgerConcurrentTurnsShareAFile(t *testing.T) {
	wb, repo, ws, one := ledgerRepo(t)
	two := newChat(t, wb, ws)
	one.prompt("auth")
	two.prompt("ticker")
	one.edit("a.txt", "a.txt\nauth\n")
	two.edit("a.txt", "a.txt\nauth\nticker\n")
	one.stop("Auth done.")
	two.stop("Ticker done.")
	v := getLedger(t, wb, repo)
	for _, id := range []string{one.turnID(1), two.turnID(1)} {
		if got := claimed(v)[id]; !equalLists(got, []string{"a.txt:M"}) {
			t.Errorf("%s claims %v, want a.txt", id, got)
		}
	}
	for _, g := range v.Groups {
		if len(g.Files) != 1 || len(g.Files[0].Shared) != 1 {
			t.Errorf("%s files = %+v, want a.txt shared with the other chat", g.Turn.ID, g.Files)
		}
	}
}

// By chat: each chat lists every path its turns claim once, naming the other
// chats that claim it too, lines counted from HEAD (an untracked text file
// by its lines, a binary one 0). The project's changes summary adds it up.
func TestLedgerChatsAndSummary(t *testing.T) {
	wb, repo, ws, one := ledgerRepo(t)
	two := newChat(t, wb, ws)
	one.prompt("auth")
	two.prompt("ticker")
	one.edit("a.txt", "a.txt\nauth\n")
	one.edit("q.txt", "q\n")
	two.edit("a.txt", "a.txt\nauth\nticker\n")
	one.stop("Auth done.")
	two.stop("Ticker done.")
	writeFile(t, filepath.Join(repo, "logo.bin"), "\x89PNG\x00\x00\n")
	writeFile(t, filepath.Join(repo, "notes.txt"), "x\ny")
	one.prompt("more") // running, claiming nothing yet

	v := getLedger(t, wb, repo)
	if len(v.Chats) != 2 || v.Chats[0].TabID != one.tab || v.Chats[1].TabID != two.tab {
		t.Fatalf("chats = %+v, want the running one first", v.Chats)
	}
	c1, c2 := v.Chats[0], v.Chats[1]
	if c1.Chat != "main · claude 1" || c2.Chat != "main · claude 2" || !c1.Running || c2.Running {
		t.Errorf("chats = %q running %v, %q running %v", c1.Chat, c1.Running, c2.Chat, c2.Running)
	}
	if got := fmt.Sprint(c1.Files); got != "[{a.txt M 3 1 [main · claude 2]} {q.txt M 1 1 []}]" || c1.Add != 4 || c1.Del != 2 {
		t.Errorf("chat 1 files = %s, +%d -%d", got, c1.Add, c1.Del)
	}
	if got := fmt.Sprint(c2.Files); got != "[{a.txt M 3 1 [main · claude 1]}]" || c2.Add != 3 || c2.Del != 1 {
		t.Errorf("chat 2 files = %s, +%d -%d", got, c2.Add, c2.Del)
	}
	if lt := c1.LastTurn; lt.ID != one.turnID(1) || !slices.Equal(lt.Files, []string{"a.txt", "q.txt"}) || lt.Add != 4 || lt.Ended == 0 {
		t.Errorf("chat 1 last turn = %+v, want its ended turn 1", lt)
	}
	if c1.Draft.Subject != "Auth" || c2.Draft.Subject != "Ticker" {
		t.Errorf("drafts = %+v, %+v", c1.Draft, c2.Draft)
	}
	if v.LastTurn == nil || v.LastTurn.ID != two.turnID(1) || v.LastTurn.TabID != two.tab || v.LastTurn.Chat != c2.Chat {
		t.Errorf("folder last turn = %+v, want the turn that ended last", v.LastTurn)
	}
	if got := fmt.Sprint(v.Outside); got != "[{logo.bin A 0 0} {notes.txt A 2 0}]" {
		t.Errorf("outside = %s", got)
	}

	var sum struct {
		Workspaces map[string]workspaceChanges `json:"workspaces"`
	}
	getJSON(t, wb, "/api/projects/"+wb.records.ProjectAt(repo)+"/changes-summary", &sum)
	want := map[string]workspaceChanges{ws.ID: {Files: 4, Add: 6, Del: 2, Running: true}}
	if !maps.Equal(sum.Workspaces, want) {
		t.Errorf("summary = %+v, want %+v", sum.Workspaces, want)
	}
	if w := getStatus(t, wb, "/api/projects/nope/changes-summary"); w != http.StatusNotFound {
		t.Errorf("unknown project: %d", w)
	}
}

// Workspaces sharing a folder split it by their chats; what none of theirs
// claims counts for each.
func TestSummarizeLedger(t *testing.T) {
	v := ledgerView{
		Chats: []chatView{
			{WorkspaceID: "w1", Files: []chatFile{{Path: "a", Add: 1}}},
			{WorkspaceID: "w2", Files: []chatFile{{Path: "a", Add: 1}, {Path: "b", Add: 2, Del: 1}}},
			{WorkspaceID: "w9", Files: []chatFile{{Path: "c", Add: 5}}},
		},
		Outside:      []outsideFile{{Path: "d", Add: 1, Del: 1}},
		running:      map[string]bool{"w2": true},
		unreviewedBy: map[string]int{"w1": 1, "w9": 2},
	}
	got := summarizeLedger(v, []string{"w1", "w2"})
	want := map[string]workspaceChanges{
		"w1": {Files: 3, Add: 7, Del: 1, Unreviewed: 3},
		"w2": {Files: 4, Add: 9, Del: 2, Unreviewed: 2, Running: true},
	}
	if !maps.Equal(got, want) {
		t.Errorf("summary = %+v, want %+v", got, want)
	}
}

// What a turn claims follows the repository: later edits keep the claim, a
// commit ends it, and a turn working on another turn's change shares it.
func TestLedgerLiveClaims(t *testing.T) {
	wb, repo, _, chat := ledgerRepo(t)
	chat.prompt("change a and start notes")
	chat.edit("a.txt", "a.txt two\n")
	chat.edit("notes.txt", "notes\n")
	chat.stop("Changed.")
	t1 := chat.turnID(1)

	// The user goes on editing a.txt, and edits q.txt, which no turn did.
	writeFile(t, filepath.Join(repo, "a.txt"), "a.txt two\nby hand\n")
	writeFile(t, filepath.Join(repo, "q.txt"), "q.txt by hand\n")
	v := getLedger(t, wb, repo)
	if got := claimed(v)[t1]; !equalLists(got, []string{"a.txt:M", "notes.txt:A"}) {
		t.Fatalf("after later edits %s claims %v", t1, got)
	}
	if !equalLists(outside(v), []string{"q.txt:M"}) {
		t.Fatalf("outside = %v", outside(v))
	}

	// A second turn works on a.txt again: both claim it, and say so.
	chat.prompt("change a again")
	chat.edit("a.txt", "a.txt three\n")
	chat.stop("Changed again.")
	t2 := chat.turnID(2)
	v = getLedger(t, wb, repo)
	if got := claimed(v)[t2]; !equalLists(got, []string{"a.txt:M"}) {
		t.Fatalf("%s claims %v", t2, got)
	}
	if got := claimed(v)[t1]; !equalLists(got, []string{"a.txt:M", "notes.txt:A"}) {
		t.Fatalf("%s claims %v", t1, got)
	}
	for _, g := range v.Groups {
		for _, f := range g.Files {
			if f.Path != "a.txt" {
				if len(f.Shared) != 0 {
					t.Errorf("%s %s shared with %+v", g.Turn.ID, f.Path, f.Shared)
				}
				continue
			}
			other := t1
			if g.Turn.ID == t1 {
				other = t2
			}
			if len(f.Shared) != 1 || f.Shared[0].TurnID != other || f.Shared[0].Chat != "main" {
				t.Errorf("%s a.txt shared = %+v, want %s", g.Turn.ID, f.Shared, other)
			}
		}
	}

	// Committing a.txt ends both claims; the second turn, which held nothing
	// else, is no longer listed. Editing it again is nobody's work.
	runGit(t, repo, "add", "a.txt")
	runGit(t, repo, "commit", "-q", "-m", "a")
	v = getLedger(t, wb, repo)
	if got := claimed(v); len(got) != 1 || !equalLists(got[t1], []string{"notes.txt:A"}) {
		t.Fatalf("after commit claimed = %v", got)
	}
	writeFile(t, filepath.Join(repo, "a.txt"), "a.txt four\n")
	v = getLedger(t, wb, repo)
	if !equalLists(outside(v), []string{"a.txt:M", "q.txt:M"}) {
		t.Fatalf("after commit and edit outside = %v", outside(v))
	}
}

// A running turn already claims what its tools wrote.
func TestLedgerRunningTurnClaimsToolEdits(t *testing.T) {
	wb, repo, _, chat := ledgerRepo(t)
	chat.prompt("work in progress")
	chat.edit("a.txt", "a.txt two\n")
	writeFile(t, filepath.Join(repo, "q.txt"), "q.txt by hand\n")
	v := getLedger(t, wb, repo)
	g := groupOf(t, v, chat.turnID(1))
	if !g.Turn.Running || g.Turn.Ended != 0 || g.Turn.Snapshot != "" {
		t.Fatalf("running turn = %+v", g.Turn)
	}
	if got := claimed(v)[chat.turnID(1)]; !equalLists(got, []string{"a.txt:M"}) {
		t.Fatalf("claims %v", got)
	}
	if !equalLists(outside(v), []string{"q.txt:M"}) {
		t.Fatalf("outside = %v", outside(v))
	}
}

func reviewed(t *testing.T, wb *workbench, dir, kind, id string, on bool) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"kind": kind, "id": id, "reviewed": on})
	if w := postJSON(t, wb, "/api/folders/"+wbItemID("dir", dir)+"/reviews", string(body)); w.Code != http.StatusOK {
		t.Fatalf("review %s %s: %d %s", kind, id, w.Code, w.Body.String())
	}
}

// Claude starts editing the moment the prompt's hook answers, so that answer
// waits for the before snapshot: a file written right after it is the turn's.
func TestLedgerPromptHookWaitsForSnapshot(t *testing.T) {
	wb, repo, _, chat := ledgerRepo(t)
	b, _ := json.Marshal(map[string]any{
		"session_id": "sess-" + chat.tab, "transcript_path": chat.transcript, "cwd": chat.cwd,
		"hook_event_name": "UserPromptSubmit", "prompt": "add a middleware",
	})
	if w := postJSON(t, wb, "/api/tabs/"+chat.tab+"/hook", string(b)); w.Code != http.StatusOK {
		t.Fatalf("prompt hook: %d %s", w.Code, w.Body.String())
	}
	// No ledgers.wait() here: the hook's answer alone must be enough.
	chat.edit("middleware.ts", "export const requireSession = 1\n")
	chat.stop("Added the middleware.")
	if got := claimed(getLedger(t, wb, repo))[chat.turnID(1)]; !equalLists(got, []string{"middleware.ts:A"}) {
		t.Fatalf("turn claims %v, want the file written right after the prompt", got)
	}
}

// A commit the agent made during a turn is listed as the agent's until it is
// reviewed; the review follows the change through an amend and a rebase.
func TestLedgerAgentCommits(t *testing.T) {
	wb, repo, _, chat := ledgerRepo(t)
	base := runGit(t, repo, "rev-parse", "HEAD")
	runGit(t, repo, "commit", "-q", "--allow-empty", "-m", "the user's own")

	chat.prompt("fix a and commit it")
	chat.edit("a.txt", "a.txt fixed\n")
	runGit(t, repo, "commit", "-q", "-am", "Fix a")
	chat.stop("Committed the fix.")

	one := func() commitView {
		t.Helper()
		v := getLedger(t, wb, repo)
		if len(v.Commits) != 1 {
			t.Fatalf("commits = %+v", v.Commits)
		}
		if c := v.Commits[0]; c.TurnID != chat.turnID(1) || c.TabID != chat.tab || c.N != 1 || c.Chat != "main" || c.Pushed {
			t.Fatalf("commit = %+v", c)
		}
		want := 1
		if v.Commits[0].Reviewed {
			want = 0
		}
		if v.Unreviewed != want {
			t.Fatalf("unreviewed = %d with %+v", v.Unreviewed, v.Commits[0])
		}
		return v.Commits[0]
	}
	c := one()
	if c.SHA != runGit(t, repo, "rev-parse", "HEAD") || c.Subject != "Fix a" || c.Short == "" || c.When == 0 || c.Reviewed {
		t.Fatalf("commit = %+v", c)
	}

	reviewed(t, wb, repo, "commit", c.SHA, true)
	if c = one(); !c.Reviewed {
		t.Fatal("review not recorded")
	}

	runGit(t, repo, "commit", "-q", "--amend", "-m", "Fix a, reworded")
	if c = one(); c.SHA != runGit(t, repo, "rev-parse", "HEAD") || !c.Reviewed {
		t.Fatalf("after amend: %+v", c)
	}

	runGit(t, repo, "branch", "up", base)
	runGit(t, repo, "checkout", "-q", "up")
	writeFile(t, filepath.Join(repo, "u.txt"), "upstream\n")
	runGit(t, repo, "add", "u.txt")
	runGit(t, repo, "commit", "-q", "-m", "upstream work")
	runGit(t, repo, "checkout", "-q", "main")
	runGit(t, repo, "rebase", "-q", "up")
	if c = one(); c.SHA != runGit(t, repo, "rev-parse", "HEAD") || !c.Reviewed {
		t.Fatalf("after rebase: %+v", c)
	}

	reviewed(t, wb, repo, "commit", c.SHA, false)
	if c = one(); c.Reviewed {
		t.Fatal("unreview not recorded")
	}

	// Pushed, it needs no review to be counted out.
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, repo, "init", "-q", "--bare", remote)
	runGit(t, repo, "remote", "add", "origin", remote)
	runGit(t, repo, "push", "-q", "-u", "origin", "main")
	v := getLedger(t, wb, repo)
	if len(v.Commits) != 1 || !v.Commits[0].Pushed || v.Unreviewed != 0 {
		t.Fatalf("after push: %+v unreviewed %d", v.Commits, v.Unreviewed)
	}
}

// Sending types the text into the tab's terminal as a bracketed paste and
// presses Enter, but never into an agent that is busy.
func TestTabSend(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell")
	}
	wb, _, ws, _ := ledgerRepo(t)
	wb.localOnly = true
	useSettings(t, map[string]any{})
	t.Setenv("SHELL", "/bin/sh")
	tab, err := wb.records.AddTab(ws.ID, tabClaude)
	if err != nil {
		t.Fatal(err)
	}
	send := func(id, text string) (int, string) {
		body, _ := json.Marshal(map[string]string{"text": text})
		w := postJSON(t, wb, "/api/tabs/"+id+"/send", string(body))
		return w.Code, w.Body.String()
	}
	if code, body := send("nope", "hi"); code != http.StatusNotFound {
		t.Fatalf("unknown tab: %d %s", code, body)
	}
	if code, body := send(tab.ID, "hi"); code != http.StatusConflict || !strings.Contains(body, "chat isn't running") {
		t.Fatalf("stopped tab: %d %s", code, body)
	}

	// The tab's program records exactly what reaches it, in raw mode so the
	// terminal turns no \r into \n. The escape a text carries is dropped, so
	// it cannot end the paste.
	dir := t.TempDir()
	const want = "\x1b[200~fix it\nplease[201~\x1b[201~\r"
	sh, err := startShell(dir, "stty raw -echo && touch ready && head -c "+strconv.Itoa(len(want))+" > got", nil)
	if err != nil {
		t.Fatal(err)
	}
	tab.mu.Lock()
	tab.sh = sh
	tab.mu.Unlock()
	waitFor(t, func() bool { _, err := os.Stat(filepath.Join(dir, "ready")); return err == nil })

	for _, status := range []string{statusWorking, statusWaiting} {
		tab.setStatus(status)
		if code, body := send(tab.ID, "too early"); code != http.StatusConflict || !strings.Contains(body, "chat is busy") {
			t.Fatalf("%s tab: %d %s", status, code, body)
		}
	}
	tab.setStatus(statusIdle)
	if code, body := send(tab.ID, "fix it\r\nplease\x1b[201~"); code != http.StatusOK {
		t.Fatalf("idle tab: %d %s", code, body)
	}
	waitFor(t, func() bool { b, _ := os.ReadFile(filepath.Join(dir, "got")); return len(b) >= len(want) })
	if got, _ := os.ReadFile(filepath.Join(dir, "got")); string(got) != want {
		t.Fatalf("typed %q, want %q", got, want)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !ok(); {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// keys lists a JSON object's keys, sorted.
func keys(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	return slices.Sorted(maps.Keys(m))
}

// The whole route, driven by the hook events a Claude Code session sends:
// two turns from one chat, a change of the user's, and a commit the agent
// made; then the shape the pages read.
func TestLedgerRouteThroughHooks(t *testing.T) {
	wb, repo, ws, chat := ledgerRepo(t)
	if err := wb.records.RenameTab(chat.tab, "visitor ticker"); err != nil {
		t.Fatal(err)
	}
	chat.prompt("fix the typo in the readme.\n\nIt says 'tset'.")
	chat.edit("README.md", "# test\n\nA readme.\n")
	chat.stop("Done. Fixed the typo in README.md.\n\nNothing else needed changing.")

	chat.prompt("add notes, and commit a changelog")
	chat.edit("notes.md", "# notes\n")
	writeFile(t, filepath.Join(repo, "CHANGELOG.md"), "# changes\n")
	runGit(t, repo, "add", "CHANGELOG.md")
	runGit(t, repo, "commit", "-q", "-m", "Add a changelog")
	chat.stop("Added notes.md and committed CHANGELOG.md.")

	writeFile(t, filepath.Join(repo, "vite.config.ts"), "export default {}\n")
	reviewed(t, wb, repo, "turn", chat.turnID(1), true)

	r := getRaw(t, wb, "/api/folders/"+wbItemID("dir", ws.Dir)+"/ledger")
	var pretty bytes.Buffer
	json.Indent(&pretty, r, "", "  ")
	t.Logf("ledger:\n%s", pretty.String())

	if got := keys(t, r); !slices.Equal(got, []string{"branch", "chats", "commits", "folder", "groups", "lastTurn", "outside", "unreviewed"}) {
		t.Fatalf("keys = %v", got)
	}
	var v ledgerView
	json.Unmarshal(r, &v)
	if v.Folder != repo || v.Branch != "main" || v.Unreviewed != 1 {
		t.Fatalf("folder %q branch %q unreviewed %d", v.Folder, v.Branch, v.Unreviewed)
	}
	if len(v.Groups) != 2 || len(v.Outside) != 1 || len(v.Commits) != 1 {
		t.Fatalf("groups %d outside %d commits %d", len(v.Groups), len(v.Outside), len(v.Commits))
	}
	var raw struct {
		Groups   []json.RawMessage `json:"groups"`
		Chats    []json.RawMessage `json:"chats"`
		Outside  []json.RawMessage `json:"outside"`
		LastTurn json.RawMessage   `json:"lastTurn"`
		Commits  []json.RawMessage `json:"commits"`
	}
	json.Unmarshal(r, &raw)
	var chatRaw struct {
		Files    []json.RawMessage `json:"files"`
		LastTurn json.RawMessage   `json:"lastTurn"`
	}
	json.Unmarshal(raw.Chats[0], &chatRaw)
	var group struct {
		Turn  json.RawMessage   `json:"turn"`
		Draft json.RawMessage   `json:"draft"`
		Files []json.RawMessage `json:"files"`
	}
	json.Unmarshal(raw.Groups[0], &group)
	for _, c := range []struct {
		name string
		raw  json.RawMessage
		want []string
	}{
		{"group", raw.Groups[0], []string{"draft", "files", "reviewed", "turn"}},
		{"turn", group.Turn, []string{"chat", "ended", "id", "n", "prompt", "reply", "running", "snapshot", "started", "tabId", "workspaceId"}},
		{"draft", group.Draft, []string{"body", "subject"}},
		{"file", group.Files[0], []string{"add", "del", "path", "shared", "status"}},
		{"outside", raw.Outside[0], []string{"add", "del", "path", "status"}},
		{"chat", raw.Chats[0], []string{"add", "chat", "del", "draft", "files", "group", "groupName", "lastTurn", "running", "tabId", "workspaceId"}},
		{"chat file", chatRaw.Files[0], []string{"add", "del", "path", "shared", "status"}},
		{"chat last turn", chatRaw.LastTurn, []string{"add", "del", "ended", "files", "id", "n", "prompt", "snapshot"}},
		{"last turn", raw.LastTurn, []string{"add", "chat", "del", "ended", "files", "id", "n", "prompt", "snapshot", "tabId"}},
		{"commit", raw.Commits[0], []string{"chat", "n", "pushed", "reviewed", "sha", "short", "subject", "tabId", "turnId", "when"}},
	} {
		if got := keys(t, c.raw); !slices.Equal(got, c.want) {
			t.Errorf("%s keys = %v, want %v", c.name, got, c.want)
		}
	}

	second, first := v.Groups[0], v.Groups[1]
	if second.Turn.ID != chat.turnID(2) || first.Turn.ID != chat.turnID(1) {
		t.Fatalf("groups = %s, %s", second.Turn.ID, first.Turn.ID)
	}
	tn := first.Turn
	if tn.TabID != chat.tab || tn.WorkspaceID != ws.ID || tn.Chat != "main · visitor ticker" || tn.N != 1 || tn.Running ||
		tn.Ended < tn.Started || tn.Snapshot != runGit(t, repo, "rev-parse", turnRefPrefix+tn.ID) {
		t.Errorf("turn 1 = %+v", tn)
	}
	if tn.Reply != "Done. Fixed the typo in README.md.\n\nNothing else needed changing." {
		t.Errorf("reply = %q", tn.Reply)
	}
	if first.Draft != (draftView{Subject: "Fix the typo in the readme", Body: "Done. Fixed the typo in README.md."}) {
		t.Errorf("draft = %+v", first.Draft)
	}
	if !first.Reviewed || second.Reviewed {
		t.Errorf("reviewed = %v, %v", first.Reviewed, second.Reviewed)
	}
	if f := first.Files; len(f) != 1 || f[0].Path != "README.md" || f[0].Status != "M" || f[0].Add != 2 || f[0].Del != 0 {
		t.Errorf("turn 1 files = %+v", f)
	}
	if f := second.Files; len(f) != 1 || f[0].Path != "notes.md" || f[0].Status != "A" {
		t.Errorf("turn 2 files = %+v", f)
	}
	if v.Outside[0] != (outsideFile{Path: "vite.config.ts", Status: "A", Add: 1}) {
		t.Errorf("outside = %+v", v.Outside)
	}
	// By chat: both turns' files once, lines counted from HEAD.
	if len(v.Chats) != 1 {
		t.Fatalf("chats = %+v", v.Chats)
	}
	c := v.Chats[0]
	if c.TabID != chat.tab || c.WorkspaceID != ws.ID || c.Chat != "main · visitor ticker" || c.Group != ws.ID || c.GroupName != "main" ||
		c.Running || c.Add != 3 || c.Del != 0 || c.Draft != second.Draft {
		t.Errorf("chat = %+v", c)
	}
	if got := fmt.Sprint(c.Files); got != "[{README.md M 2 0 []} {notes.md A 1 0 []}]" {
		t.Errorf("chat files = %s", got)
	}
	if lt := c.LastTurn; lt.ID != chat.turnID(2) || lt.N != 2 || !slices.Equal(lt.Files, []string{"notes.md"}) || lt.Add != 1 || lt.Del != 0 ||
		lt.Snapshot != second.Turn.Snapshot || lt.Ended != second.Turn.Ended || lt.Prompt != "add notes, and commit a changelog" {
		t.Errorf("chat last turn = %+v", lt)
	}
	if v.LastTurn == nil || v.LastTurn.lastTurnView.ID != c.LastTurn.ID || v.LastTurn.Chat != c.Chat || v.LastTurn.TabID != chat.tab {
		t.Errorf("folder last turn = %+v", v.LastTurn)
	}
	if c := v.Commits[0]; c.Subject != "Add a changelog" || c.TurnID != chat.turnID(2) || c.Chat != "main · visitor ticker" || c.N != 2 || c.Reviewed || c.Pushed {
		t.Errorf("commit = %+v", c)
	}

	// A folder outside git has an empty ledger; an unknown one has none.
	plain := t.TempDir()
	if _, _, err := wb.records.Open(plain); err != nil {
		t.Fatal(err)
	}
	pv := getLedger(t, wb, plain)
	if pv.Groups == nil || pv.Outside == nil || pv.Commits == nil || len(pv.Groups)+len(pv.Outside)+len(pv.Commits) != 0 {
		t.Errorf("plain folder ledger = %+v", pv)
	}
	if w := getStatus(t, wb, "/api/folders/000000000000/ledger"); w != http.StatusNotFound {
		t.Errorf("unknown folder: %d", w)
	}
}

func getRaw(t *testing.T, wb *workbench, path string) json.RawMessage {
	t.Helper()
	var raw json.RawMessage
	getJSON(t, wb, path, &raw)
	return raw
}

func getStatus(t *testing.T, wb *workbench, path string) int {
	t.Helper()
	w := reqJSON(t, wb, "GET", path, "")
	return w.Code
}

// Turns a stopped punk0 left open are closed when the ledger is read again;
// turns past two weeks go once they claim nothing, with their refs and
// reviews.
func TestLedgerPrunesAndReloads(t *testing.T) {
	wb, repo, ws, chat := ledgerRepo(t)
	head := runGit(t, repo, "rev-parse", "HEAD")
	old := time.Now().Add(-20 * 24 * time.Hour).UnixMilli()
	oldTurn := func(id, path string, at int64) *ledgerTurn {
		runGit(t, repo, "update-ref", turnRefPrefix+id, head)
		return &ledgerTurn{
			ID: id, TabID: "gone", N: 1, Chat: "old chat", Started: at, Ended: at + 1000, Before: head, After: head,
			Tools: []string{}, Commits: []turnCommit{},
			Files: []turnFile{{Path: path, Status: "M", Add: 1, Before: runGit(t, repo, "rev-parse", "HEAD:"+path), After: head}},
		}
	}
	keep, drop := oldTurn("gone-1", "a.txt", old), oldTurn("gone-2", "q.txt", old+2000)
	writeFile(t, filepath.Join(repo, "a.txt"), "a.txt changed long ago\n")
	// A turn the last run left open, from before which nothing changed.
	since, err := gitSnapshotCommit(repo, head, "test")
	if err != nil {
		t.Fatal(err)
	}
	open := &ledgerTurn{ID: chat.tab + "-1", TabID: chat.tab, N: 1, Started: time.Now().Add(-time.Hour).UnixMilli(),
		Before: since, HeadBefore: head, Tools: []string{}, Files: []turnFile{}, Commits: []turnCommit{}}
	file := filepath.Join(workbenchStateDir(), "turns", wbItemID("dir", ws.Dir)+".json")
	if err := writeFileAtomic(file, ledgerFile{Folder: ws.Dir, Next: map[string]int{chat.tab: 1}, Turns: []*ledgerTurn{keep, drop, open}}); err != nil {
		t.Fatal(err)
	}
	if err := wb.ledgers.setReviewed("turn:gone-2", true); err != nil {
		t.Fatal(err)
	}

	chat.prompt("a new turn")
	chat.stop("Nothing to do.")

	turns := wb.ledgers.get(ws.Dir).turns()
	var ids []string
	for _, tn := range turns {
		ids = append(ids, tn.ID)
	}
	if !slices.Equal(ids, []string{"gone-1", chat.tab + "-1", chat.tab + "-2"}) {
		t.Fatalf("turns = %v", ids)
	}
	if reopened := turns[1]; reopened.Ended == 0 || reopened.After == "" {
		t.Errorf("turn left open = %+v", reopened)
	}
	refs := runGit(t, repo, "for-each-ref", "--format=%(refname)", turnRefPrefix)
	if strings.Contains(refs, "gone-2") || !strings.Contains(refs, "gone-1") {
		t.Errorf("refs = %s", refs)
	}
	if _, ok := wb.ledgers.reviewed()["turn:gone-2"]; ok {
		t.Error("pruned turn's review kept")
	}
	var saved ledgerFile
	if err := json.Unmarshal(must(os.ReadFile(file)), &saved); err != nil || len(saved.Turns) != 3 || saved.Next[chat.tab] != 2 {
		t.Errorf("saved ledger = %+v (%v)", saved, err)
	}
	if got := claimed(getLedger(t, wb, repo))["gone-1"]; !equalLists(got, []string{"a.txt:M"}) {
		t.Errorf("old turn claims %v", got)
	}
}

func TestDrafts(t *testing.T) {
	subjects := map[string]string{
		"  fix the flaky test.  ":            "Fix the flaky test",
		"\n\nwhy does it crash?!\nmore":      "Why does it crash",
		"/review":                            "/review",
		"émigré names break the parser…":     "Émigré names break the parser",
		strings.Repeat("word ", 20) + "end.": strings.TrimSpace("Word " + strings.Repeat("word ", 13)),
		strings.Repeat("x", 80):              "X" + strings.Repeat("x", 71),
	}
	for prompt, want := range subjects {
		if got := draftSubject(prompt); got != want {
			t.Errorf("draftSubject(%q) = %q, want %q", prompt, got, want)
		}
	}
	if got := draftBody("First paragraph\nstill first.\n\nSecond."); got != "First paragraph\nstill first." {
		t.Errorf("draftBody = %q", got)
	}
	if got := draftBody(strings.Repeat("long ", 100)); len(got) > draftBodyMax || strings.HasSuffix(got, " ") {
		t.Errorf("draftBody long = %d %q", len(got), got)
	}
}

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
	if err := json.Unmarshal(out, &ev); err != nil || ev.Name != "PostToolUse" || ev.Tool != "Write" || editedPath(ev) != "/w/a.go" {
		t.Fatalf("slimmed = %s (%v)", out, err)
	}
	if got := slimHookEvent([]byte("not json")); string(got) != "not json" {
		t.Fatalf("unparsed event changed: %q", got)
	}
}
