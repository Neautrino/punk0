package main

// The turn ledger records what each chat turn -- one prompt up to the agent's
// Stop -- did to the folder the chat runs in, so uncommitted work can be
// reviewed and committed by turn, and commits an agent made itself can be
// told apart from the user's.
//
// Hook events (hook.go) are queued per folder and handled in order by one
// goroutine at a time, so a hook never waits on git. A turn opens on
// UserPromptSubmit with a snapshot of the working tree and closes on Stop,
// StopFailure or SessionEnd with another. Snapshots are commits made through
// a copy of the index (gitSnapshotCommit) and kept by refs/punk0/turns/<id>.
// Which turn owns which uncommitted file is worked out each time the ledger
// is read (computeClaims), never stored, so commits made anywhere, by anyone,
// are always accounted for.

import (
	"bytes"
	"cmp"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"px0/internal/ui"
)

const (
	ledgerMaxTurns   = 500                 // turns a folder keeps, newest first
	ledgerKeep       = 14 * 24 * time.Hour // older turns go once they claim nothing
	ledgerMaxCommits = 30                  // agent commits the ledger lists
	ledgerScan       = 1000                // commits searched for them
	turnPromptMax    = 4000
	turnReplyMax     = 600
	draftSubjectMax  = 72
	draftBodyMax     = 400
	turnRefPrefix    = "refs/punk0/turns/"
	ledgerCountMax   = 8 << 20         // untracked files counted line by line up to this size
	ledgerSummaryTTL = 2 * time.Second // how long a folder's view serves the changes summary
)

// ledgerTurn is one turn as the ledger file keeps it. Ended is 0 while the
// turn runs; Before and After are its snapshot commits, "" when one failed.
type ledgerTurn struct {
	ID          string       `json:"id"` // "<tabId>-<n>"
	TabID       string       `json:"tabId"`
	WorkspaceID string       `json:"workspaceId"`
	Chat        string       `json:"chat"` // the tab's name when the turn started
	Session     string       `json:"session,omitempty"`
	N           int          `json:"n"`
	Prompt      string       `json:"prompt"`
	Reply       string       `json:"reply"`
	Started     int64        `json:"started"` // unix ms
	Ended       int64        `json:"ended"`
	Before      string       `json:"before"`
	After       string       `json:"after"`
	HeadBefore  string       `json:"headBefore"`
	HeadAfter   string       `json:"headAfter"`
	Transcript  string       `json:"transcript,omitempty"`
	Tools       []string     `json:"tools"`   // paths the agent's edit tools wrote
	Files       []turnFile   `json:"files"`   // what changed between the snapshots
	Commits     []turnCommit `json:"commits"` // commits HEAD gained during the turn
}

// turnFile is one path that changed between a turn's snapshots. Before and
// After are its blob ids, "" where the path did not exist.
type turnFile struct {
	Path   string `json:"path"`
	Status string `json:"status"` // A, M or D
	Add    int    `json:"add"`
	Del    int    `json:"del"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// turnCommit is a commit an agent made. The patch id finds it again after an
// amend or a rebase that kept its diff; it is "" for a merge or an empty one.
type turnCommit struct {
	SHA     string `json:"sha"`
	PatchID string `json:"patchId"`
}

func (t *ledgerTurn) file(path string) *turnFile {
	for i := range t.Files {
		if t.Files[i].Path == path {
			return &t.Files[i]
		}
	}
	return nil
}

// ledgerFile is what <state>/turns/<dirID>.json holds.
type ledgerFile struct {
	Folder string         `json:"folder"`
	Next   map[string]int `json:"next"`  // tab id -> n of its last turn
	Turns  []*ledgerTurn  `json:"turns"` // in the order they started
}

// ledgers holds the ledger of every folder a hook or a page asked about, and
// the reviews, which are kept for every folder in one file.
type ledgers struct {
	home    string // <state>
	pending sync.WaitGroup

	mu    sync.Mutex
	byDir map[string]*ledger

	reviewMu sync.Mutex
	reviews  map[string]int64 // "commit:<patch-id>" or "turn:<id>" -> unix ms; nil until read

	viewMu sync.Mutex
	views  map[string]cachedView // folder -> its view, for the changes summary
}

type cachedView struct {
	at   time.Time
	view ledgerView
}

func newLedgers(home string) *ledgers {
	return &ledgers{home: home, byDir: map[string]*ledger{}, views: map[string]cachedView{}}
}

// ledger is one folder's: dir is the workspace folder, as /w/<dirID>/ names
// it, and git runs at the top level of the repository it is in.
type ledger struct {
	ls   *ledgers
	dir  string
	file string

	mu       sync.Mutex // guards everything below; git never runs under it
	loaded   bool
	data     ledgerFile
	queue    []ledgerJob
	busy     bool              // a goroutine is draining queue
	patchIDs map[string]string // sha -> patch id, "" for none: commits never change
}

// ledgerJob is a hook event for the ledger worker. at is when the hook
// arrived, which is when its turn started or ended.
type ledgerJob struct {
	event      string
	at         int64
	place      tabPlace
	prompt     string
	session    string
	transcript string
	path       string        // PostToolUse: the file written, absolute
	stale      []string      // "stale": turns a previous run left open
	done       chan struct{} // closed once handled; nil when nobody waits
}

// wait blocks until every queued hook event has been handled.
func (ls *ledgers) wait() { ls.pending.Wait() }

// get returns dir's ledger, read from disk the first time.
func (ls *ledgers) get(dir string) *ledger {
	ls.mu.Lock()
	l := ls.byDir[dir]
	if l == nil {
		l = &ledger{ls: ls, dir: dir, file: filepath.Join(ls.home, "turns", wbItemID("dir", dir)+".json")}
		ls.byDir[dir] = l
	}
	ls.mu.Unlock()
	l.load()
	return l
}

// hook queues what a hook event means for the ledger of the tab's folder.
// Scratch chats keep none. For a prompt it returns a channel closed once the
// turn's before snapshot is taken (nil otherwise): the hook waits on it, since
// Claude starts editing as soon as the hook answers, and an edit landing ahead
// of the snapshot would read as already there before the turn.
func (ls *ledgers) hook(place tabPlace, ev hookEvent) <-chan struct{} {
	if place.kind == kindFolder {
		return nil
	}
	j := ledgerJob{event: ev.Name, at: time.Now().UnixMilli(), place: place, transcript: ev.Transcript}
	switch ev.Name {
	case "UserPromptSubmit":
		j.prompt, j.session = ev.Prompt, ev.Session
		j.done = make(chan struct{})
	case "PostToolUse":
		path := editedPath(ev)
		if path == "" {
			return nil
		}
		if !filepath.IsAbs(path) {
			base := ev.Cwd
			if base == "" {
				base = place.dir
			}
			path = filepath.Join(base, path)
		}
		j.path = path
	case "Stop", "StopFailure", "SessionEnd":
	default:
		return nil
	}
	ls.get(place.dir).enqueue(j)
	return j.done
}

// editedPath is the file an edit tool wrote, or "" for any other tool.
func editedPath(ev hookEvent) string {
	switch ev.Tool {
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
	default:
		return ""
	}
	var in struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
	}
	if json.Unmarshal(ev.ToolInput, &in) != nil {
		return ""
	}
	if in.FilePath != "" {
		return in.FilePath
	}
	return in.NotebookPath
}

// load reads the ledger file once. Turns it left open belonged to agents a
// previous run stopped, so they are closed as of now.
func (l *ledger) load() {
	l.mu.Lock()
	if l.loaded {
		l.mu.Unlock()
		return
	}
	l.loaded = true
	l.data = ledgerFile{Folder: l.dir, Next: map[string]int{}}
	if b, err := os.ReadFile(l.file); err == nil {
		var f ledgerFile
		if json.Unmarshal(b, &f) != nil {
			// Keep the unreadable file for inspection rather than overwriting it.
			os.Rename(l.file, l.file+".bad")
		} else {
			l.data.Turns = slices.DeleteFunc(f.Turns, func(t *ledgerTurn) bool { return t == nil })
			if f.Next != nil {
				l.data.Next = f.Next
			}
		}
	}
	var stale []string
	for _, t := range l.data.Turns {
		if t.Ended == 0 {
			stale = append(stale, t.ID)
		}
	}
	l.mu.Unlock()
	if len(stale) > 0 {
		l.enqueue(ledgerJob{event: "stale", at: time.Now().UnixMilli(), stale: stale})
	}
}

func (l *ledger) enqueue(j ledgerJob) {
	l.ls.pending.Add(1)
	l.mu.Lock()
	l.queue = append(l.queue, j)
	start := !l.busy
	l.busy = true
	l.mu.Unlock()
	if start {
		go l.drain()
	}
}

func (l *ledger) drain() {
	for {
		l.mu.Lock()
		if len(l.queue) == 0 {
			l.busy = false
			l.mu.Unlock()
			return
		}
		j := l.queue[0]
		l.queue = l.queue[1:]
		l.mu.Unlock()
		l.run(j)
		if j.done != nil {
			close(j.done)
		}
		l.ls.pending.Done()
	}
}

func (l *ledger) run(j ledgerJob) {
	root := ledgerRoot(l.dir)
	if root == "" {
		return // not a repository: no ledger
	}
	switch j.event {
	case "UserPromptSubmit":
		// A turn still open lost its Stop; this prompt ends it.
		l.closeOpen(root, j)
		l.open(root, j)
	case "PostToolUse":
		l.addTool(root, j)
	case "stale":
		for _, id := range j.stale {
			if t := l.turn(id); t != nil {
				l.close(root, t, j)
			}
		}
	default:
		l.closeOpen(root, j)
	}
}

// ledgerRoot is the top level of the repository dir is in, or "".
func ledgerRoot(dir string) string {
	if info := gitProbe(dir); info.ok {
		return info.toplevel
	}
	return ""
}

func (l *ledger) open(root string, j ledgerJob) {
	l.mu.Lock()
	n := l.data.Next[j.place.tabID] + 1
	l.mu.Unlock()
	t := &ledgerTurn{
		ID:          j.place.tabID + "-" + strconv.Itoa(n),
		TabID:       j.place.tabID,
		WorkspaceID: j.place.workspaceID,
		Chat:        j.place.chat,
		Session:     j.session,
		N:           n,
		Prompt:      clipText(strings.TrimSpace(j.prompt), turnPromptMax),
		Started:     j.at,
		HeadBefore:  gitHeadSHA(root),
		Transcript:  j.transcript,
		Tools:       []string{},
		Files:       []turnFile{},
		Commits:     []turnCommit{},
	}
	if sha, err := gitSnapshotCommit(root, t.HeadBefore, "punk0 turn "+t.ID+" before"); err != nil {
		ui.Status("warn", "turn ledger", err.Error(), 0, os.Stderr)
	} else {
		t.Before = sha
		// The ref keeps the snapshot from git gc until the turn is pruned.
		setTurnRef(root, t.ID, sha)
	}
	l.mu.Lock()
	l.data.Next[t.TabID] = n
	l.data.Turns = append(l.data.Turns, t)
	l.mu.Unlock()
	l.save()
}

// openTurn is the tab's running turn, or nil.
func (l *ledger) openTurn(tabID string) *ledgerTurn {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i := len(l.data.Turns) - 1; i >= 0; i-- {
		if t := l.data.Turns[i]; t.TabID == tabID && t.Ended == 0 {
			return t
		}
	}
	return nil
}

func (l *ledger) turn(id string) *ledgerTurn {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, t := range l.data.Turns {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func (l *ledger) hasTurn(id string) bool { return l.turn(id) != nil }

func (l *ledger) addTool(root string, j ledgerJob) {
	t := l.openTurn(j.place.tabID)
	if t == nil {
		return
	}
	rel := relInRepo(root, j.path)
	if rel == "" {
		return
	}
	l.mu.Lock()
	added := !slices.Contains(t.Tools, rel)
	if added {
		t.Tools = append(t.Tools, rel)
	}
	l.mu.Unlock()
	if added {
		l.save()
	}
}

// relInRepo is path relative to root in git's spelling, or "" outside it.
// The agent may name the file through a symlinked folder; git resolves them.
func relInRepo(root, path string) string {
	under := func(p string) string {
		rel, err := filepath.Rel(root, filepath.Clean(p))
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return ""
		}
		return filepath.ToSlash(rel)
	}
	if rel := under(path); rel != "" {
		return rel
	}
	if dir, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		return under(filepath.Join(dir, filepath.Base(path)))
	}
	return ""
}

func (l *ledger) closeOpen(root string, j ledgerJob) {
	if t := l.openTurn(j.place.tabID); t != nil {
		l.close(root, t, j)
	}
}

// close ends a turn: the after snapshot, what changed since the before one,
// the commits HEAD gained, and the agent's last words.
func (l *ledger) close(root string, t *ledgerTurn, j ledgerJob) {
	l.mu.Lock()
	id, before, headBefore, started, transcript := t.ID, t.Before, t.HeadBefore, t.Started, t.Transcript
	l.mu.Unlock()
	if j.transcript != "" {
		transcript = j.transcript
	}
	headAfter := gitHeadSHA(root)
	after, files := "", []turnFile{}
	if before != "" {
		if sha, err := gitSnapshotCommit(root, before, "punk0 turn "+id+" after"); err != nil {
			ui.Status("warn", "turn ledger", err.Error(), 0, os.Stderr)
		} else {
			after, files = sha, turnFiles(root, before, sha)
			setTurnRef(root, id, sha)
		}
	}
	commits := l.agentCommits(root, headBefore, headAfter, started)
	reply := clipText(lastAssistantText(transcript), turnReplyMax)

	l.mu.Lock()
	t.Ended = max(j.at, started)
	t.After, t.Files, t.HeadAfter, t.Commits, t.Reply = after, files, headAfter, commits, reply
	l.mu.Unlock()
	l.save()
	l.prune(root)
}

func setTurnRef(root, id, sha string) {
	if out, err := exec.Command("git", "-C", root, "update-ref", turnRefPrefix+id, sha).CombinedOutput(); err != nil {
		ui.Status("warn", "turn ledger", gitError(out, err).Error(), 0, os.Stderr)
	}
}

// turnFiles lists what changed between two snapshots, with line counts and
// the blob ids on both sides.
func turnFiles(root, before, after string) []turnFile {
	files := []turnFile{}
	out, err := exec.Command("git", "-C", root, "diff-tree", "-r", "-z", "--no-renames", "--raw", "--numstat", before, after).Output()
	if err != nil {
		return files
	}
	at := map[string]int{}
	parts := strings.Split(string(out), "\x00")
	for i := 0; i < len(parts); i++ {
		part := parts[i]
		if strings.HasPrefix(part, ":") {
			// :<mode> <mode> <blob> <blob> <status>, then the path.
			f := strings.Fields(part)
			if len(f) < 5 || i+1 >= len(parts) {
				continue
			}
			i++
			status := "M"
			switch f[4] {
			case "A", "D":
				status = f[4]
			}
			at[parts[i]] = len(files)
			files = append(files, turnFile{Path: parts[i], Status: status, Before: blobID(f[2]), After: blobID(f[3])})
			continue
		}
		// <added>\t<deleted>\t<path>; "-" for a binary file.
		f := strings.SplitN(part, "\t", 3)
		if len(f) != 3 {
			continue
		}
		if k, ok := at[f[2]]; ok {
			files[k].Add, _ = strconv.Atoi(f[0])
			files[k].Del, _ = strconv.Atoi(f[1])
		}
	}
	return files
}

// blobID is "" for the all-zero id git gives a side that has no file.
func blobID(id string) string {
	if strings.Trim(id, "0") == "" {
		return ""
	}
	return id
}

// agentCommits lists the commits HEAD gained between from and to (from ""
// for an unborn branch). Commits older than the turn came from somewhere
// else, a pull or a branch switch, and are left out.
func (l *ledger) agentCommits(root, from, to string, started int64) []turnCommit {
	commits := []turnCommit{}
	if to == "" || to == from {
		return commits
	}
	args := []string{"-C", root, "rev-list", "--max-count=200", "--since=" + strconv.FormatInt(started/1000-5, 10), to}
	if from != "" {
		args = append(args, "^"+from)
	}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return commits
	}
	shas := strings.Fields(string(out))
	ids := l.patchIDsFor(root, shas)
	for _, sha := range shas {
		commits = append(commits, turnCommit{SHA: sha, PatchID: ids[sha]})
	}
	return commits
}

// patchIDsFor is gitPatchIDs, remembered: a commit's patch id never changes.
func (l *ledger) patchIDsFor(root string, shas []string) map[string]string {
	ids := make(map[string]string, len(shas))
	var missing []string
	l.mu.Lock()
	for _, sha := range shas {
		if id, ok := l.patchIDs[sha]; ok {
			ids[sha] = id
		} else {
			missing = append(missing, sha)
		}
	}
	l.mu.Unlock()
	if len(missing) == 0 {
		return ids
	}
	found := gitPatchIDs(root, missing)
	l.mu.Lock()
	if l.patchIDs == nil {
		l.patchIDs = map[string]string{}
	}
	for _, sha := range missing {
		ids[sha] = found[sha]
		l.patchIDs[sha] = found[sha]
	}
	l.mu.Unlock()
	return ids
}

// gitPatchIDs maps commits to `git patch-id --stable` of their diff. A merge
// or a commit with an empty diff has none.
func gitPatchIDs(root string, shas []string) map[string]string {
	ids := map[string]string{}
	if len(shas) == 0 {
		return ids
	}
	logCmd := exec.Command("git", "-C", root, "log", "--no-walk=unsorted", "--stdin", "-p",
		"--no-color", "--no-ext-diff", "--no-show-signature", "--format=commit %H")
	logCmd.Stdin = strings.NewReader(strings.Join(shas, "\n") + "\n")
	pipe, err := logCmd.StdoutPipe()
	if err != nil {
		return ids
	}
	pidCmd := exec.Command("git", "-C", root, "patch-id", "--stable")
	pidCmd.Stdin = pipe
	var out bytes.Buffer
	pidCmd.Stdout = &out
	if logCmd.Start() != nil {
		return ids
	}
	pidErr := pidCmd.Run()
	if logCmd.Wait() != nil || pidErr != nil {
		return ids
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if f := strings.Fields(line); len(f) == 2 {
			ids[f[1]] = f[0]
		}
	}
	return ids
}

// commitKey is what a review of commit sha is filed under: its patch id, so
// the review survives an amend or rebase that keeps the diff, else the sha.
func (l *ledger) commitKey(root, sha string) string {
	if id := l.patchIDsFor(root, []string{sha})[sha]; id != "" {
		return "commit:" + id
	}
	return "commit:" + sha
}

// lastAssistantText is the text of the last assistant message in a Claude
// Code transcript (JSON lines), read from the end since transcripts grow
// long. Subagents' messages are skipped.
func lastAssistantText(path string) string {
	if path == "" {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return ""
	}
	size := fi.Size()
	for chunk := int64(256 << 10); ; chunk *= 4 {
		start := max(size-chunk, 0)
		buf := make([]byte, size-start)
		if _, err := f.ReadAt(buf, start); err != nil && err != io.EOF {
			return ""
		}
		lines := bytes.Split(buf, []byte{'\n'})
		if start > 0 {
			lines = lines[1:] // starts mid-line
		}
		for i := len(lines) - 1; i >= 0; i-- {
			if text := assistantText(lines[i]); text != "" {
				return text
			}
		}
		if start == 0 || chunk >= 64<<20 {
			return ""
		}
	}
}

func assistantText(line []byte) string {
	if !bytes.Contains(line, []byte(`"assistant"`)) {
		return "" // tool results can be megabytes; not worth decoding
	}
	var e struct {
		Type      string `json:"type"`
		Sidechain bool   `json:"isSidechain"`
		Message   struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &e) != nil || e.Type != "assistant" || e.Sidechain {
		return ""
	}
	var s string
	if json.Unmarshal(e.Message.Content, &s) == nil {
		return strings.TrimSpace(s)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(e.Message.Content, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if t := strings.TrimSpace(b.Text); b.Type == "text" && t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "\n\n")
}

func (l *ledger) save() {
	l.mu.Lock()
	b, err := json.Marshal(l.data)
	l.mu.Unlock()
	if err == nil {
		err = writeFileAtomic(l.file, json.RawMessage(b))
	}
	if err != nil {
		ui.Status("warn", "turn ledger", err.Error(), 0, os.Stderr)
	}
}

// turns copies the turns for reading outside the lock.
func (l *ledger) turns() []ledgerTurn {
	l.mu.Lock()
	defer l.mu.Unlock()
	turns := make([]ledgerTurn, len(l.data.Turns))
	for i, t := range l.data.Turns {
		turns[i] = *t
		turns[i].Tools = slices.Clone(t.Tools)
	}
	return turns
}

// prune drops the turns beyond the newest ledgerMaxTurns, and those older
// than ledgerKeep that no longer claim anything, with their refs and reviews.
func (l *ledger) prune(root string) {
	now := time.Now()
	cutoff := now.Add(-ledgerKeep).UnixMilli()
	turns := l.turns()
	over := len(turns) - ledgerMaxTurns
	if over <= 0 && !slices.ContainsFunc(turns, func(t ledgerTurn) bool { return t.Ended != 0 && t.Ended < cutoff }) {
		return
	}
	claimed := map[int]bool{}
	for _, owners := range computeClaims(root, turns, now.UnixMilli()).owners {
		for _, i := range owners {
			claimed[i] = true
		}
	}
	drop := map[string]bool{}
	var refs strings.Builder
	var reviews []string
	for i, t := range turns {
		if t.Ended == 0 || (i >= over && (t.Ended >= cutoff || claimed[i])) {
			continue
		}
		drop[t.ID] = true
		refs.WriteString("delete " + turnRefPrefix + t.ID + "\n")
		reviews = append(reviews, "turn:"+t.ID)
	}
	if len(drop) == 0 {
		return
	}
	l.mu.Lock()
	l.data.Turns = slices.DeleteFunc(l.data.Turns, func(t *ledgerTurn) bool { return drop[t.ID] })
	l.mu.Unlock()
	l.save()
	cmd := exec.Command("git", "-C", root, "update-ref", "--stdin")
	cmd.Stdin = strings.NewReader(refs.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		ui.Status("warn", "turn ledger", gitError(out, err).Error(), 0, os.Stderr)
	}
	if err := l.ls.forgetReviews(reviews); err != nil {
		ui.Status("warn", "turn ledger", err.Error(), 0, os.Stderr)
	}
}

// claims is the folder's uncommitted work by turn.
type claims struct {
	status map[string]string // changed path -> A, M or D against HEAD
	paths  []string          // the changed paths, sorted
	owners map[string][]int  // path -> the turns claiming it (indexes), oldest first
}

// computeClaims works out which turns claim each changed path. A turn owns a
// path it changed if its edit tools wrote it, or if no other turn in the
// folder ran at the same time. Of the turns owning a path, the last one that
// found it as HEAD has it (both missing counts), along with any owner that ran
// alongside it from that same content, and every later owner claims it: what
// HEAD already holds of a turn's work is not the turn's any more. A running
// turn owns only what its tools wrote, as it has no after snapshot yet.
func computeClaims(root string, turns []ledgerTurn, now int64) claims {
	c := claims{status: ledgerStatus(root), owners: map[string][]int{}}
	for p := range c.status {
		c.paths = append(c.paths, p)
	}
	sort.Strings(c.paths)
	if len(c.paths) == 0 {
		return c
	}
	type owner struct {
		turn   int
		before string // blob id, "" for no file
		spec   string // else what to ask git for it: <snapshot>:<path>
	}
	cand := map[string][]owner{}
	overlap := overlapping(turns, now)
	for i, t := range turns {
		if t.Before == "" {
			continue
		}
		if t.Ended == 0 {
			for _, p := range t.Tools {
				if _, ok := c.status[p]; ok && !strings.Contains(p, "\n") {
					cand[p] = append(cand[p], owner{turn: i, spec: t.Before + ":" + p})
				}
			}
			continue
		}
		for _, f := range t.Files {
			if _, ok := c.status[f.Path]; !ok || strings.Contains(f.Path, "\n") {
				continue
			}
			if overlap[i] && !slices.Contains(t.Tools, f.Path) {
				continue
			}
			cand[f.Path] = append(cand[f.Path], owner{turn: i, before: f.Before})
		}
	}
	if len(cand) == 0 {
		return c
	}
	head := gitHeadSHA(root)
	var specs []string
	for p, owners := range cand {
		if head != "" {
			specs = append(specs, head+":"+p)
		}
		for _, o := range owners {
			if o.spec != "" {
				specs = append(specs, o.spec)
			}
		}
	}
	ids := gitObjectIDs(root, specs)
	endOf := func(t ledgerTurn) int64 {
		if t.Ended == 0 {
			return max(now, t.Started) + 1
		}
		return t.Ended
	}
	for p, owners := range cand {
		inHead := ""
		if head != "" {
			inHead = ids[head+":"+p]
		}
		before := func(o owner) string {
			if o.spec != "" {
				return ids[o.spec]
			}
			return o.before
		}
		for k := len(owners) - 1; k >= 0; k-- {
			if before(owners[k]) != inHead {
				continue
			}
			// Turns that ran alongside it from the same HEAD content -- two
			// chats editing one file at once -- claim it too.
			from := k
			for from > 0 {
				a, b := turns[owners[from-1].turn], turns[owners[k].turn]
				if before(owners[from-1]) != inHead || a.Started >= endOf(b) || b.Started >= endOf(a) {
					break
				}
				from--
			}
			for _, o := range owners[from:] {
				c.owners[p] = append(c.owners[p], o.turn)
			}
			break
		}
	}
	return c
}

// overlapping marks the turns that ran while another turn in the folder did.
// A running turn runs until now and beyond.
func overlapping(turns []ledgerTurn, now int64) []bool {
	end := func(t ledgerTurn) int64 {
		if t.Ended == 0 {
			return max(now, t.Started) + 1
		}
		return t.Ended
	}
	out := make([]bool, len(turns))
	for i := range turns {
		for j := i + 1; j < len(turns); j++ {
			if turns[j].Started < end(turns[i]) && turns[i].Started < end(turns[j]) {
				out[i], out[j] = true, true
			}
		}
	}
	return out
}

// ledgerStatus maps every path that differs between HEAD and the working
// tree, untracked files included, to A, M or D. It never writes the index.
func ledgerStatus(root string) map[string]string {
	cmd := exec.Command("git", "-C", root, "status", "--porcelain=v1", "-z", "-uall", "--no-renames")
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	type side struct{ inHead, onDisk bool }
	sides := map[string]*side{}
	for _, e := range strings.Split(string(out), "\x00") {
		if len(e) < 4 {
			continue
		}
		x, y, p := e[0], e[1], e[3:]
		s := sides[p]
		if s == nil {
			s = &side{}
			sides[p] = s
		}
		// "git rm --cached" lists a path twice: deleted, and untracked.
		if x != 'A' && x != '?' && y != 'A' {
			s.inHead = true
		}
		if y != 'D' && !(x == 'D' && y == ' ') {
			s.onDisk = true
		}
	}
	status := make(map[string]string, len(sides))
	for p, s := range sides {
		switch {
		case s.inHead && s.onDisk:
			status[p] = "M"
		case s.onDisk:
			status[p] = "A"
		case s.inHead:
			status[p] = "D"
		}
	}
	return status
}

// gitObjectIDs resolves <rev>:<path> specs to object ids; a spec naming no
// object is left out.
func gitObjectIDs(root string, specs []string) map[string]string {
	ids := map[string]string{}
	if len(specs) == 0 {
		return ids
	}
	cmd := exec.Command("git", "-C", root, "cat-file", "--batch-check=%(objectname)")
	cmd.Stdin = strings.NewReader(strings.Join(specs, "\n") + "\n")
	out, err := cmd.Output()
	if err != nil {
		return ids
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	for i, spec := range specs {
		// "<spec> missing" (or ambiguous) has a space; an id does not.
		if i < len(lines) && lines[i] != "" && !strings.Contains(lines[i], " ") {
			ids[spec] = lines[i]
		}
	}
	return ids
}

// The ledger as GET /api/folders/{dirID}/ledger answers it. running and
// unreviewedBy are per workspace, for the projects' changes summary.
type ledgerView struct {
	Folder     string          `json:"folder"`
	Branch     string          `json:"branch"`
	Groups     []groupView     `json:"groups"`
	Chats      []chatView      `json:"chats"`
	Outside    []outsideFile   `json:"outside"`
	LastTurn   *folderTurnView `json:"lastTurn"`
	Commits    []commitView    `json:"commits"`
	Unreviewed int             `json:"unreviewed"`

	running      map[string]bool // workspaces with a turn running
	unreviewedBy map[string]int  // workspace -> its share of Unreviewed
}

type turnView struct {
	ID          string `json:"id"`
	TabID       string `json:"tabId"`
	WorkspaceID string `json:"workspaceId"`
	Chat        string `json:"chat"`
	N           int    `json:"n"`
	Prompt      string `json:"prompt"`
	Reply       string `json:"reply"`
	Started     int64  `json:"started"`
	Ended       int64  `json:"ended"`
	Running     bool   `json:"running"`
	Snapshot    string `json:"snapshot"` // the after snapshot, "" while running
}

type groupView struct {
	Turn     turnView    `json:"turn"`
	Draft    draftView   `json:"draft"`
	Reviewed bool        `json:"reviewed"`
	Files    []groupFile `json:"files"`
}

type draftView struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

type groupFile struct {
	Path   string      `json:"path"`
	Status string      `json:"status"`
	Add    int         `json:"add"`
	Del    int         `json:"del"`
	Shared []sharedRef `json:"shared"` // the other turns claiming the path
}

type sharedRef struct {
	TurnID string `json:"turnId"`
	Chat   string `json:"chat"`
	N      int    `json:"n"`
}

type outsideFile struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Add    int    `json:"add"`
	Del    int    `json:"del"`
}

// chatView is one chat's uncommitted work: every path its turns claim, the
// lines counted from HEAD to the working tree, so a path two of its turns
// changed counts once. Group is the chat's workspace, the chats of one
// feature.
type chatView struct {
	TabID       string       `json:"tabId"`
	WorkspaceID string       `json:"workspaceId"`
	Chat        string       `json:"chat"`
	Group       string       `json:"group"`
	GroupName   string       `json:"groupName"`
	Running     bool         `json:"running"` // a turn of the chat is running
	Files       []chatFile   `json:"files"`
	Add         int          `json:"add"`
	Del         int          `json:"del"`
	LastTurn    lastTurnView `json:"lastTurn"` // its newest turn claiming anything
	Draft       draftView    `json:"draft"`    // that turn's
}

type chatFile struct {
	Path   string   `json:"path"`
	Status string   `json:"status"`
	Add    int      `json:"add"`
	Del    int      `json:"del"`
	Shared []string `json:"shared"` // the other chats claiming the path, by name
}

// lastTurnView is a turn and the paths it claims, lines counted as for its
// chat. Ended is 0 and Snapshot "" while it runs.
type lastTurnView struct {
	ID       string   `json:"id"`
	N        int      `json:"n"`
	Prompt   string   `json:"prompt"`
	Snapshot string   `json:"snapshot"`
	Files    []string `json:"files"`
	Add      int      `json:"add"`
	Del      int      `json:"del"`
	Ended    int64    `json:"ended"`
}

// folderTurnView is the folder's newest ended turn that claims anything.
type folderTurnView struct {
	lastTurnView
	Chat  string `json:"chat"`
	TabID string `json:"tabId"`
}

type commitView struct {
	SHA      string `json:"sha"`
	Short    string `json:"short"`
	Subject  string `json:"subject"`
	When     int64  `json:"when"` // committer date, unix ms
	Chat     string `json:"chat"`
	TabID    string `json:"tabId"`
	TurnID   string `json:"turnId"`
	N        int    `json:"n"`
	Reviewed bool   `json:"reviewed"`
	Pushed   bool   `json:"pushed"`
}

func emptyLedgerView(folder, branch string) ledgerView {
	return ledgerView{Folder: folder, Branch: branch, Groups: []groupView{}, Chats: []chatView{}, Outside: []outsideFile{}, Commits: []commitView{}}
}

// view is the ledger as the pages show it. chatOf names a tab's chat; a tab
// that is gone keeps the name it had when its turn started. groupOf names a
// workspace.
func (l *ledger) view(root string, chatOf func(tabID string) (string, bool), groupOf func(workspaceID string) string, reviews map[string]int64) ledgerView {
	v := emptyLedgerView(root, gitBranch(root))
	turns := l.turns()
	chat := func(t ledgerTurn) string {
		if name, ok := chatOf(t.TabID); ok {
			return name
		}
		return t.Chat
	}
	c := computeClaims(root, turns, time.Now().UnixMilli())
	lines := changedLines(root, c.status)
	claimed := map[int][]string{}
	for _, p := range c.paths {
		owners := c.owners[p]
		if len(owners) == 0 {
			n := lines[p]
			v.Outside = append(v.Outside, outsideFile{Path: p, Status: c.status[p], Add: n[0], Del: n[1]})
			continue
		}
		for _, i := range owners {
			claimed[i] = append(claimed[i], p)
		}
	}
	for i := len(turns) - 1; i >= 0; i-- {
		paths := claimed[i]
		if len(paths) == 0 {
			continue
		}
		t := &turns[i]
		g := groupView{
			Turn: turnView{
				ID: t.ID, TabID: t.TabID, WorkspaceID: t.WorkspaceID, Chat: chat(*t), N: t.N,
				Prompt: t.Prompt, Reply: t.Reply, Started: t.Started, Ended: t.Ended,
				Running: t.Ended == 0, Snapshot: t.After,
			},
			Draft: draftView{Subject: draftSubject(t.Prompt), Body: draftBody(t.Reply)},
			Files: make([]groupFile, 0, len(paths)),
		}
		_, g.Reviewed = reviews["turn:"+t.ID]
		for _, p := range paths {
			f := groupFile{Path: p, Status: c.status[p], Shared: []sharedRef{}}
			if tf := t.file(p); tf != nil {
				f.Add, f.Del = tf.Add, tf.Del
			}
			for _, j := range c.owners[p] {
				if j != i {
					f.Shared = append(f.Shared, sharedRef{TurnID: turns[j].ID, Chat: chat(turns[j]), N: turns[j].N})
				}
			}
			g.Files = append(g.Files, f)
		}
		v.Groups = append(v.Groups, g)
	}
	v.Chats, v.LastTurn = chatViews(turns, c, claimed, lines, chat, groupOf)
	v.running = map[string]bool{}
	for _, t := range turns {
		if t.Ended == 0 {
			v.running[t.WorkspaceID] = true
		}
	}
	v.Commits, v.Unreviewed, v.unreviewedBy = l.commitRows(root, turns, reviews, chat)
	return v
}

// chatViews gathers the claims by chat, newest activity first, and finds
// the folder's newest ended turn that still claims anything. claimed maps a
// turn to the paths it claims.
func chatViews(turns []ledgerTurn, c claims, claimed map[int][]string, lines map[string][2]int,
	chat func(ledgerTurn) string, groupOf func(string) string) ([]chatView, *folderTurnView) {
	lastTurn := func(i int) lastTurnView {
		t := turns[i]
		lt := lastTurnView{ID: t.ID, N: t.N, Prompt: t.Prompt, Snapshot: t.After, Files: claimed[i], Ended: t.Ended}
		for _, p := range claimed[i] {
			lt.Add += lines[p][0]
			lt.Del += lines[p][1]
		}
		return lt
	}
	type tab struct {
		newest   int   // its newest turn claiming anything
		activity int64 // when it last started or ended a turn
		running  bool
	}
	tabs := map[string]*tab{}
	var order []string
	var folderLast *folderTurnView
	for i, t := range turns {
		if len(claimed[i]) > 0 {
			if tabs[t.TabID] == nil {
				tabs[t.TabID] = &tab{}
				order = append(order, t.TabID)
			}
			tabs[t.TabID].newest = i
			if t.Ended != 0 && (folderLast == nil || t.Ended >= folderLast.Ended) {
				folderLast = &folderTurnView{lastTurnView: lastTurn(i), Chat: chat(t), TabID: t.TabID}
			}
		}
	}
	for _, t := range turns {
		if tb := tabs[t.TabID]; tb != nil {
			tb.activity = max(tb.activity, t.Started, t.Ended)
			tb.running = tb.running || t.Ended == 0
		}
	}
	slices.SortStableFunc(order, func(a, b string) int { return cmp.Compare(tabs[b].activity, tabs[a].activity) })
	// A path's chats, by tab, for naming the others that share it.
	chatsOf := map[string][]string{}
	for _, p := range c.paths {
		for _, i := range c.owners[p] {
			if id := turns[i].TabID; !slices.Contains(chatsOf[p], id) {
				chatsOf[p] = append(chatsOf[p], id)
			}
		}
	}
	names := map[string]string{}
	for _, id := range order {
		names[id] = chat(turns[tabs[id].newest])
	}
	views := make([]chatView, 0, len(order))
	for _, id := range order {
		tb := tabs[id]
		t := turns[tb.newest]
		cv := chatView{
			TabID: id, WorkspaceID: t.WorkspaceID, Chat: names[id], Group: t.WorkspaceID, GroupName: groupOf(t.WorkspaceID),
			Running: tb.running, Files: []chatFile{}, LastTurn: lastTurn(tb.newest),
			Draft: draftView{Subject: draftSubject(t.Prompt), Body: draftBody(t.Reply)},
		}
		for _, p := range c.paths {
			if !slices.Contains(chatsOf[p], id) {
				continue
			}
			n := lines[p]
			f := chatFile{Path: p, Status: c.status[p], Add: n[0], Del: n[1], Shared: []string{}}
			for _, other := range chatsOf[p] {
				if other != id {
					f.Shared = append(f.Shared, names[other])
				}
			}
			cv.Files = append(cv.Files, f)
			cv.Add += n[0]
			cv.Del += n[1]
		}
		views = append(views, cv)
	}
	return views, folderLast
}

// changedLines counts the lines each changed path adds and deletes from
// HEAD to the working tree: git's numstat for what git tracks, the lines on
// disk for the rest (all of it on an unborn branch), 0/0 for a binary file
// or one too big to count.
func changedLines(root string, status map[string]string) map[string][2]int {
	lines := make(map[string][2]int, len(status))
	if len(status) == 0 {
		return lines
	}
	cmd := exec.Command("git", "-C", root, "diff", "HEAD", "--numstat", "-z", "--no-renames", "--no-ext-diff", "--no-textconv")
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	if out, err := cmd.Output(); err == nil {
		for _, rec := range strings.Split(string(out), "\x00") {
			f := strings.SplitN(rec, "\t", 3)
			if len(f) < 3 {
				continue
			}
			add, _ := strconv.Atoi(f[0]) // "-" for a binary file: 0
			del, _ := strconv.Atoi(f[1])
			lines[f[2]] = [2]int{add, del}
		}
	}
	for p, st := range status {
		if _, ok := lines[p]; !ok && st != "D" {
			lines[p] = [2]int{countLines(filepath.Join(root, filepath.FromSlash(p))), 0}
		}
	}
	return lines
}

// countLines counts a text file's lines the way git does, a last line
// without a newline included; a binary file (a NUL in its first 8000
// bytes, git's test), a file over ledgerCountMax or anything not a regular
// file counts 0.
func countLines(path string) int {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > ledgerCountMax {
		return 0
	}
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 || bytes.IndexByte(b[:min(len(b), 8000)], 0) >= 0 {
		return 0
	}
	n := bytes.Count(b, []byte{'\n'})
	if b[len(b)-1] != '\n' {
		n++
	}
	return n
}

// commitRows lists the agents' commits reachable from HEAD, newest first,
// and counts those neither pushed nor reviewed, in all and by the workspace
// of the turn that made them. A commit is the agent's if its sha or its
// patch id is one a turn recorded, so an amend or rebase that kept the diff
// keeps it tagged, and reviewed.
func (l *ledger) commitRows(root string, turns []ledgerTurn, reviews map[string]int64, chat func(ledgerTurn) string) ([]commitView, int, map[string]int) {
	rows := []commitView{}
	byWorkspace := map[string]int{}
	bySHA, byPatch := map[string]int{}, map[string]int{}
	var since int64
	for i, t := range turns {
		for _, c := range t.Commits {
			bySHA[c.SHA] = i
			if c.PatchID != "" {
				byPatch[c.PatchID] = i
			}
			if since == 0 || t.Started < since {
				since = t.Started
			}
		}
	}
	if len(bySHA) == 0 {
		return rows, 0, byWorkspace
	}
	// A commit, amended or rebased or not, was committed after its turn began.
	sinceArg := "--since=" + strconv.FormatInt(since/1000-60, 10)
	out, err := exec.Command("git", "-C", root, "log", "-z", "--no-show-signature", sinceArg,
		"--max-count="+strconv.Itoa(ledgerScan), "--format=%H%x1f%h%x1f%ct%x1f%s", "HEAD").Output()
	if err != nil {
		return rows, 0, byWorkspace
	}
	type cand struct {
		sha, short, subject string
		when                int64
	}
	var cands []cand
	reachable := map[string]bool{}
	for _, rec := range strings.Split(string(out), "\x00") {
		f := strings.Split(strings.TrimLeft(rec, "\n"), "\x1f")
		if len(f) < 4 {
			continue
		}
		when, _ := strconv.ParseInt(f[2], 10, 64)
		cands = append(cands, cand{sha: f[0], short: f[1], subject: f[3], when: when * 1000})
		reachable[f[0]] = true
	}
	// Patch ids are needed for the reviews of the commits tagged by sha, and
	// for every commit when a tagged one is not there as it was.
	rewritten := false
	for sha := range bySHA {
		if !reachable[sha] {
			rewritten = true
			break
		}
	}
	var need []string
	for _, c := range cands {
		if _, ok := bySHA[c.sha]; ok || rewritten {
			need = append(need, c.sha)
		}
	}
	ids := l.patchIDsFor(root, need)
	unpushed, hasUpstream := map[string]bool{}, false
	if out, err := exec.Command("git", "-C", root, "rev-list", sinceArg, "--max-count="+strconv.Itoa(ledgerScan),
		"HEAD", "--not", "@{u}").Output(); err == nil {
		hasUpstream = true
		for _, sha := range strings.Fields(string(out)) {
			unpushed[sha] = true
		}
	}
	unreviewed := 0
	for _, c := range cands {
		i, ok := bySHA[c.sha]
		pid := ids[c.sha]
		if !ok && pid != "" {
			i, ok = byPatch[pid]
		}
		if !ok {
			continue
		}
		key := "commit:" + c.sha
		if pid != "" {
			key = "commit:" + pid
		}
		_, reviewed := reviews[key]
		pushed := hasUpstream && !unpushed[c.sha]
		if !pushed && !reviewed {
			unreviewed++
			byWorkspace[turns[i].WorkspaceID]++
		}
		if len(rows) < ledgerMaxCommits {
			t := turns[i]
			rows = append(rows, commitView{
				SHA: c.sha, Short: c.short, Subject: c.subject, When: c.when,
				Chat: chat(t), TabID: t.TabID, TurnID: t.ID, N: t.N,
				Reviewed: reviewed, Pushed: pushed,
			})
		}
	}
	return rows, unreviewed, byWorkspace
}

// draftSubject makes a commit subject of a prompt: its first line, trimmed,
// capitalized, without closing punctuation, cut at a word to fit 72.
func draftSubject(prompt string) string {
	line := ""
	for _, l := range strings.Split(prompt, "\n") {
		if l = strings.Join(strings.Fields(l), " "); l != "" {
			line = l
			break
		}
	}
	line = trimClosing(cutWords(trimClosing(line), draftSubjectMax))
	r, size := utf8.DecodeRuneInString(line)
	if r == utf8.RuneError {
		return line
	}
	return string(unicode.ToUpper(r)) + line[size:]
}

func trimClosing(s string) string {
	return strings.TrimRight(s, " \t.,;:!?…")
}

// draftBody is the reply's first paragraph, cut at a word to fit 400.
func draftBody(reply string) string {
	var para []string
	for _, l := range strings.Split(strings.TrimSpace(reply), "\n") {
		l = strings.TrimRight(l, " \t\r")
		if strings.TrimSpace(l) == "" {
			break
		}
		para = append(para, l)
	}
	return cutWords(strings.Join(para, "\n"), draftBodyMax)
}

// cutWords cuts s to at most n characters, at a word boundary when there is
// one.
func cutWords(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	cut := string(r[:n])
	if !unicode.IsSpace(r[n]) {
		if i := strings.LastIndexFunc(cut, unicode.IsSpace); i > 0 {
			cut = cut[:i]
		}
	}
	return strings.TrimRightFunc(cut, unicode.IsSpace)
}

// clipText is s cut at a word to at most n characters, "…" marking the cut.
func clipText(s string, n int) string {
	if len([]rune(s)) <= n {
		return s
	}
	return cutWords(s, n-1) + "…"
}

func (ls *ledgers) reviewsFile() string { return filepath.Join(ls.home, "reviews.json") }

func (ls *ledgers) loadReviewsLocked() {
	if ls.reviews != nil {
		return
	}
	ls.reviews = map[string]int64{}
	b, err := os.ReadFile(ls.reviewsFile())
	if err != nil {
		return
	}
	if json.Unmarshal(b, &ls.reviews) != nil {
		os.Rename(ls.reviewsFile(), ls.reviewsFile()+".bad")
		ls.reviews = map[string]int64{}
	}
}

// reviewed returns a copy of the reviews.
func (ls *ledgers) reviewed() map[string]int64 {
	ls.reviewMu.Lock()
	defer ls.reviewMu.Unlock()
	ls.loadReviewsLocked()
	return maps.Clone(ls.reviews)
}

func (ls *ledgers) setReviewed(key string, on bool) error {
	ls.reviewMu.Lock()
	defer ls.reviewMu.Unlock()
	ls.loadReviewsLocked()
	if on {
		ls.reviews[key] = time.Now().UnixMilli()
	} else {
		delete(ls.reviews, key)
	}
	return writeFileAtomic(ls.reviewsFile(), ls.reviews)
}

func (ls *ledgers) forgetReviews(keys []string) error {
	ls.reviewMu.Lock()
	defer ls.reviewMu.Unlock()
	ls.loadReviewsLocked()
	n := len(ls.reviews)
	for _, k := range keys {
		delete(ls.reviews, k)
	}
	if len(ls.reviews) == n {
		return nil
	}
	return writeFileAtomic(ls.reviewsFile(), ls.reviews)
}

// handleLedger serves GET /api/folders/{dirID}/ledger: the folder's
// uncommitted work by chat and by chat turn, the agents' commits, and how
// many of those are neither pushed nor reviewed. A folder outside git has an
// empty one.
func (wb *workbench) handleLedger(w http.ResponseWriter, r *http.Request) {
	if !trustedGet(w, r) {
		return
	}
	dir := wb.records.Folder(r.PathValue("dirID"))
	if dir == "" {
		fail(w, http.StatusNotFound, "unknown folder")
		return
	}
	writeJSON(w, wb.ledgerView(dir))
}

// ledgerView is dir's ledger as the pages show it, chats named as they go
// by now.
func (wb *workbench) ledgerView(dir string) ledgerView {
	root := ledgerRoot(dir)
	if root == "" {
		return emptyLedgerView(dir, "")
	}
	names := map[string]string{}
	chatOf := func(tabID string) (string, bool) {
		if name, ok := names[tabID]; ok {
			return name, true
		}
		p, ok := wb.records.Place(tabID)
		if ok {
			names[tabID] = p.chat
		}
		return p.chat, ok
	}
	groups := map[string]string{}
	groupOf := func(workspaceID string) string {
		name, ok := groups[workspaceID]
		if !ok {
			name = wb.records.WorkspaceLabel(workspaceID)
			groups[workspaceID] = name
		}
		return name
	}
	return wb.ledgers.get(dir).view(root, chatOf, groupOf, wb.ledgers.reviewed())
}

// summaryView is ledgerView, reused for ledgerSummaryTTL: the agents page
// asks for every workspace of a project every few seconds.
func (wb *workbench) summaryView(dir string) ledgerView {
	ls := wb.ledgers
	ls.viewMu.Lock()
	c, ok := ls.views[dir]
	ls.viewMu.Unlock()
	if ok && time.Since(c.at) < ledgerSummaryTTL {
		return c.view
	}
	v := wb.ledgerView(dir)
	ls.viewMu.Lock()
	ls.views[dir] = cachedView{at: time.Now(), view: v}
	ls.viewMu.Unlock()
	return v
}

// workspaceChanges is one workspace's line in the changes summary.
type workspaceChanges struct {
	Files      int  `json:"files"`
	Add        int  `json:"add"`
	Del        int  `json:"del"`
	Unreviewed int  `json:"unreviewed"`
	Running    bool `json:"running"`
}

// handleChangesSummary serves GET /api/projects/{id}/changes-summary: for
// each of the project's workspaces, how much is uncommitted, how many agent
// commits await review, and whether an agent turn is running. Workspaces
// sharing a folder (the local ones) split its ledger by the chats' workspace;
// what no chat of theirs claims -- the user's own changes, or a removed
// workspace's -- counts for each of them, as each shows the whole folder.
func (wb *workbench) handleChangesSummary(w http.ResponseWriter, r *http.Request) {
	if !trustedGet(w, r) {
		return
	}
	dirs, ok := wb.records.WorkspaceDirs(r.PathValue("id"))
	if !ok {
		fail(w, http.StatusNotFound, "unknown project")
		return
	}
	byDir := map[string][]string{}
	for _, d := range dirs {
		byDir[d.dir] = append(byDir[d.dir], d.id)
	}
	out := make(map[string]workspaceChanges, len(dirs))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for dir, ids := range byDir {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sum := summarizeLedger(wb.summaryView(dir), ids)
			mu.Lock()
			maps.Copy(out, sum)
			mu.Unlock()
		}()
	}
	wg.Wait()
	writeJSON(w, map[string]any{"workspaces": out})
}

// summarizeLedger splits a folder's ledger among the workspaces ids that
// run in it. A path counts once per workspace, however many of its chats
// claim it.
func summarizeLedger(v ledgerView, ids []string) map[string]workspaceChanges {
	type lines struct{ add, del int }
	paths := make(map[string]map[string]lines, len(ids))
	for _, id := range ids {
		paths[id] = map[string]lines{}
	}
	everyone := func(p string, n lines) {
		for _, id := range ids {
			paths[id][p] = n
		}
	}
	for _, f := range v.Outside {
		everyone(f.Path, lines{f.Add, f.Del})
	}
	for _, c := range v.Chats {
		for _, f := range c.Files {
			if mine := paths[c.WorkspaceID]; mine != nil {
				mine[f.Path] = lines{f.Add, f.Del}
			} else {
				everyone(f.Path, lines{f.Add, f.Del})
			}
		}
	}
	unclaimed := 0 // agent commits of workspaces no longer here
	for ws, n := range v.unreviewedBy {
		if !slices.Contains(ids, ws) {
			unclaimed += n
		}
	}
	out := make(map[string]workspaceChanges, len(ids))
	for _, id := range ids {
		s := workspaceChanges{Files: len(paths[id]), Unreviewed: v.unreviewedBy[id] + unclaimed, Running: v.running[id]}
		for _, n := range paths[id] {
			s.Add += n.add
			s.Del += n.del
		}
		out[id] = s
	}
	return out
}

// handleReview serves POST /api/folders/{dirID}/reviews, marking an agent's
// commit (by sha) or a turn (by id) reviewed or not.
func (wb *workbench) handleReview(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	dir := wb.records.Folder(r.PathValue("dirID"))
	if dir == "" {
		fail(w, http.StatusNotFound, "unknown folder")
		return
	}
	var body struct {
		Kind     string `json:"kind"`
		ID       string `json:"id"`
		Reviewed bool   `json:"reviewed"`
	}
	if readJSONBody(r, &body) != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	root := ledgerRoot(dir)
	if root == "" {
		fail(w, http.StatusNotFound, "not a git repository")
		return
	}
	l := wb.ledgers.get(dir)
	var key string
	switch body.Kind {
	case "commit":
		var sha string
		if validSHA(body.ID) {
			if out, err := exec.Command("git", "-C", root, "rev-parse", "--verify", "-q", body.ID+"^{commit}").Output(); err == nil {
				sha = strings.TrimSpace(string(out))
			}
		}
		if sha == "" {
			fail(w, http.StatusNotFound, "unknown commit")
			return
		}
		key = l.commitKey(root, sha)
	case "turn":
		if !l.hasTurn(body.ID) {
			fail(w, http.StatusNotFound, "unknown turn")
			return
		}
		key = "turn:" + body.ID
	default:
		fail(w, http.StatusBadRequest, `kind is "commit" or "turn"`)
		return
	}
	if err := wb.ledgers.setReviewed(key, body.Reviewed); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
