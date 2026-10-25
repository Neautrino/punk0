package main

// The workbench is the process a bare `punk0` leaves running: it serves the
// orchestrator page at / and owns the projects, workspaces, tabs and
// terminals on it (records.go, term.go). It holds no index and no language
// servers of its own. Every folder is served by a child punk0 process bound
// to 127.0.0.1 on a free port under the base path /w/<id>/, which the
// workbench reverse-proxies, so the whole thing is reachable on one port and
// workspaces in one folder share one child.
//
// Children are tied to the workbench through their stdin: when the pipe closes,
// because the workbench stopped them or died, they shut down the same way a
// Ctrl-C would, which removes PR checkouts and stops language servers. A child
// nobody has had open for workspace.idleStop minutes is stopped to free its
// memory; the next request for it starts a new one.
//
// Pull requests, which are not folders, are recorded in workbench.json (see
// workbenchStorePath) along with the clones they are checked out from. It
// holds what to reopen, never live state: process IDs and ports are not saved.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"px0/internal/settings"
	"px0/internal/ui"
)

const (
	wbReadyPrefix       = "px0-ready "    // a child prints this, then its address, once it is listening
	wbStartTimeout      = 3 * time.Minute // a PR checkout may clone a large repository
	wbAPIWait           = 30 * time.Second
	wbWorkspaceIdleStop = 5.0  // minutes, unless workspace.idleStop says otherwise
	wbTerminalIdleStop  = 30.0 // minutes, unless terminal.idleStop says otherwise
	wbStopGrace         = 5 * time.Second
	wbMaxRecent         = 50 // pull requests kept in workbench.json
	wbLogLines          = 20 // child output lines kept for error reports
	wbPingTimeout       = 400 * time.Millisecond
)

// wbItem is one thing a /w/<id>/ URL can serve: a folder, which records.go
// owns and which is never stored here, or a pull request, which is.
type wbItem struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"` // "dir" or "pr"
	Path       string `json:"path,omitempty"`
	URL        string `json:"url,omitempty"`
	Title      string `json:"title,omitempty"`
	Repo       string `json:"repo,omitempty"` // owner/repo, PRs only
	Number     int    `json:"number,omitempty"`
	LastOpened int64  `json:"lastOpened"` // unix milliseconds
}

type wbFile struct {
	Version int      `json:"version"`
	Items   []wbItem `json:"items"`
	// Clones maps a lowercased "owner/repo" to the local clone its pull
	// requests are checked out from (see workbench_clone.go).
	Clones map[string]string `json:"clones,omitempty"`
}

// workbenchStateDir is where workbench.json and the instance lock live:
// $XDG_STATE_HOME/punk0 when set, otherwise ~/.punk0, like the session files.
func workbenchStateDir() string {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "punk0")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".punk0")
	}
	return filepath.Join(os.TempDir(), "punk0")
}

func workbenchStorePath() string { return filepath.Join(workbenchStateDir(), "workbench.json") }
func workbenchLockPath() string  { return filepath.Join(workbenchStateDir(), "workbench.lock") }

// wbItemID is stable for a directory or PR, so its /w/<id>/ URL and the
// session file keyed on that base path (review drafts) survive restarts.
func wbItemID(kind, key string) string {
	h := sha256.Sum256([]byte(kind + "\x00" + key))
	return hex.EncodeToString(h[:6])
}

func wbDirItem(root string) wbItem {
	root = filepath.Clean(root)
	return wbItem{ID: wbItemID("dir", root), Kind: "dir", Path: root, Title: filepath.Base(root)}
}

func wbPRItem(pt PRTarget) wbItem {
	key := strings.ToLower(fmt.Sprintf("%s/%s/%s#%d", pt.Provider, pt.Owner, pt.Repo, pt.Number))
	return wbItem{
		ID:     wbItemID("pr", key),
		Kind:   "pr",
		URL:    pt.URL,
		Repo:   pt.Owner + "/" + pt.Repo,
		Number: pt.Number,
	}
}

// wbStore reads and writes workbench.json. It keeps no copy in memory: every
// change re-reads the file first, so a pull request recorded meanwhile by a
// plain `punk0 <pr-url>` in another terminal is not overwritten.
type wbStore struct {
	mu   sync.Mutex
	path string
}

func (st *wbStore) load() wbFile {
	f := wbFile{Version: 1}
	b, err := os.ReadFile(st.path)
	if err != nil {
		return f
	}
	if err := json.Unmarshal(b, &f); err != nil {
		// Keep the unreadable file for inspection rather than overwriting it.
		os.Rename(st.path, st.path+".bad")
		return wbFile{Version: 1}
	}
	f.Version = 1
	// Folders were recorded here before records.json owned them; a file
	// written by an older punk0 still lists them.
	kept := f.Items[:0]
	for _, it := range f.Items {
		if it.Kind == "pr" {
			kept = append(kept, it)
		}
	}
	f.Items = kept
	return f
}

func (st *wbStore) Load() wbFile {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.load()
}

func (st *wbStore) Update(fn func(*wbFile)) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	f := st.load()
	fn(&f)
	trimRecent(&f)
	return writeFileAtomic(st.path, f)
}

func (st *wbStore) Get(id string) (wbItem, bool) {
	for _, it := range st.Load().Items {
		if it.ID == id {
			return it, true
		}
	}
	return wbItem{}, false
}

// Record adds an item or refreshes an existing one, marking it opened now. A
// title is only replaced by a non-empty one.
func (st *wbStore) Record(it wbItem) error {
	return st.Update(func(f *wbFile) {
		it.LastOpened = time.Now().UnixMilli()
		for i := range f.Items {
			if f.Items[i].ID == it.ID {
				if it.Title == "" {
					it.Title = f.Items[i].Title
				}
				f.Items[i] = it
				return
			}
		}
		f.Items = append(f.Items, it)
	})
}

// trimRecent orders items most recent first and drops the oldest ones.
func trimRecent(f *wbFile) {
	sort.SliceStable(f.Items, func(i, j int) bool { return f.Items[i].LastOpened > f.Items[j].LastOpened })
	if len(f.Items) > wbMaxRecent {
		f.Items = f.Items[:wbMaxRecent]
	}
}

func writeFileAtomic(p string, v any) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".workbench-*.json")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// recordRecent notes a pull request opened outside the orchestrator
// (`punk0 <pr-url>`) so it shows up in its list later. Best effort.
func recordRecent(it wbItem) {
	st := &wbStore{path: workbenchStorePath()}
	_ = st.Record(it)
}

/* ---------- child processes ---------- */

type wbChild struct {
	id       string
	title    string // for the holding page, which may outlive the item (see forget)
	gone     bool   // failed, and the item was taken out of the list with it
	state    string // "starting", "running", "failed", "stopped"
	err      string
	progress string // what a starting child is waiting on, e.g. a clone
	noClone  bool   // failed because no local clone of the PR's repository is known
	addr     string
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	proxy    *httputil.ReverseProxy
	done     chan struct{}

	// cancel abandons a start still preparing (cloning), before there is a
	// process to stop.
	ctx    context.Context
	cancel context.CancelFunc

	active     int       // proxied requests in flight, open event streams included
	lastActive time.Time // when the last proxied request finished

	logMu sync.Mutex
	log   []string
}

func (c *wbChild) appendLog(line string) {
	c.logMu.Lock()
	defer c.logMu.Unlock()
	c.log = append(c.log, line)
	if len(c.log) > wbLogLines {
		c.log = c.log[len(c.log)-wbLogLines:]
	}
}

// lineWriter hands each complete line written to it to fn. A spinner redraws
// its line with \r, so only the text after the last \r counts.
type lineWriter struct {
	mu  sync.Mutex
	buf []byte
	fn  func(string)
}

func (lw *lineWriter) Write(p []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	lw.buf = append(lw.buf, p...)
	for {
		i := strings.IndexByte(string(lw.buf), '\n')
		if i < 0 {
			break
		}
		lw.emit(string(lw.buf[:i]))
		lw.buf = lw.buf[i+1:]
	}
	return len(p), nil
}

// Flush hands over a last line that did not end in a newline.
func (lw *lineWriter) Flush() {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	if len(lw.buf) > 0 {
		lw.emit(string(lw.buf))
		lw.buf = nil
	}
}

func (lw *lineWriter) emit(line string) {
	line = strings.TrimRight(line, "\r")
	if i := strings.LastIndexByte(line, '\r'); i >= 0 {
		line = line[i+1:]
	}
	lw.fn(line)
}

// failure picks the line that best explains why a child exited: the last
// "punk0: ..." line fatal() wrote, otherwise the last thing it said.
func (c *wbChild) failure() string {
	c.logMu.Lock()
	defer c.logMu.Unlock()
	for i := len(c.log) - 1; i >= 0; i-- {
		if strings.HasPrefix(c.log[i], "punk0: ") {
			msg := c.log[i]
			for strings.HasPrefix(msg, "punk0: ") {
				msg = strings.TrimPrefix(msg, "punk0: ")
			}
			return msg
		}
	}
	for i := len(c.log) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(c.log[i]); s != "" {
			return s
		}
	}
	return "punk0 exited unexpectedly"
}

type workbench struct {
	store     *wbStore
	records   *Records // projects, workspaces and the terminals in them
	ledgers   *ledgers // what each chat turn changed, per folder (ledger.go)
	basePath  string
	cwd       string
	exe       string
	childArgs []string // flags handed on to every child (-no-lsp, -agent, ...)
	localOnly bool     // listening on loopback only: refuse DNS-rebound requests (hostguard.go)

	mu       sync.Mutex
	children map[string]*wbChild
	closing  bool
	// gitSeen is whether each project folder was a repository when last
	// listed, so the folder's editor restarts when it turns into one.
	gitSeen map[string]bool
	clones  map[string]*cloneJob // Add project's clones, by id (addproject.go)
	cloning sync.WaitGroup       // runClone goroutines, waited for by Close

	cpu cpuMeter // per-process CPU deltas for /api/resources

	mux *http.ServeMux
}

func newWorkbench(basePath, cwd, exe string, childArgs []string) (*workbench, error) {
	home := workbenchStateDir()
	rec, err := OpenRecords(home)
	if err != nil {
		return nil, err
	}
	wb := &workbench{
		store:     &wbStore{path: workbenchStorePath()},
		records:   rec,
		ledgers:   newLedgers(home),
		basePath:  cleanBasePath(basePath),
		cwd:       cwd,
		exe:       exe,
		childArgs: childArgs,
		children:  map[string]*wbChild{},
		mux:       http.NewServeMux(),
	}
	wb.routes()
	go wb.reapIdle()
	return wb, nil
}

// serveFrom tells punk0 how to reach itself: a tab's agent reports its hooks
// to this URL, and the hook settings a claude tab starts with name this
// executable. Both are known only once the server is listening.
func (wb *workbench) serveFrom(loopbackURL string) {
	wb.records.SetHookBase(loopbackURL)
	if err := writeClaudeHooks(wb.exe); err != nil {
		ui.Status("warn", "claude hooks", err.Error(), 0, os.Stderr)
	}
}

func (wb *workbench) route(sub string) string {
	return strings.TrimSuffix(wb.basePath, "/") + sub
}

func (wb *workbench) childBase(id string) string { return wb.route("/w/" + id + "/") }

// childHref is the same link relative to the page's <base href>.
func (wb *workbench) childHref(id string) string { return "w/" + id + "/" }

// childState is how the child serving a folder is doing, for the list.
func (wb *workbench) childState(id string) string {
	wb.mu.Lock()
	defer wb.mu.Unlock()
	if c := wb.children[id]; c != nil {
		return c.state
	}
	return "stopped"
}

// item resolves a /w/<id>/ id: a pull request from the store, or a folder
// registered in records, which keeps no item of its own.
func (wb *workbench) item(id string) (wbItem, bool) {
	if it, ok := wb.store.Get(id); ok {
		return it, true
	}
	if dir := wb.records.Folder(id); dir != "" {
		return wbDirItem(dir), true
	}
	return wbItem{}, false
}

func (wb *workbench) routes() {
	sub, _ := fs.Sub(assets, "web")
	staticPrefix := wb.route("/static/")
	wb.mux.Handle(staticPrefix, (&Server{}).handleStatic(sub, staticPrefix))
	wb.mux.HandleFunc(wb.route("/static/themes.css"), (&Server{}).handleThemes)
	wb.mux.HandleFunc(wb.route("/api/wb/ping"), wb.handlePing)
	wb.mux.HandleFunc(wb.route("/api/wb/state"), wb.handleState)
	wb.mux.HandleFunc(wb.route("/api/wb/complete"), wb.handleComplete)
	wb.mux.HandleFunc(wb.route("/api/resources"), wb.handleResources)
	wb.mux.HandleFunc(wb.route("/api/wb/open"), wb.handleOpen)
	wb.mux.HandleFunc(wb.route("/api/wb/stop"), wb.handleStop)
	wb.mux.HandleFunc(wb.route("/api/wb/remove"), wb.handleRemove)
	wb.mux.HandleFunc("GET "+wb.route("/api/projects"), wb.handleProjectList)
	wb.mux.HandleFunc("POST "+wb.route("/api/projects"), wb.handleProjectAdd)
	wb.mux.HandleFunc("GET "+wb.route("/api/projects/removed"), wb.handleRemovedProjects)
	wb.mux.HandleFunc("POST "+wb.route("/api/projects/new"), wb.handleProjectNew)
	wb.mux.HandleFunc("GET "+wb.route("/api/wb/folder"), wb.handleFolder)
	wb.mux.HandleFunc("POST "+wb.route("/api/clones"), wb.handleCloneStart)
	wb.mux.HandleFunc("GET "+wb.route("/api/clones/{id}"), wb.handleCloneStatus)
	wb.mux.HandleFunc("DELETE "+wb.route("/api/clones/{id}"), wb.handleCloneCancel)
	wb.mux.HandleFunc("POST "+wb.route("/api/projects/{id}/refresh"), wb.handleProjectRefresh)
	wb.mux.HandleFunc("DELETE "+wb.route("/api/projects/{id}"), wb.handleProjectRemove)
	wb.mux.HandleFunc("POST "+wb.route("/api/projects/{id}/workspaces"), wb.handleWorkspaceAdd)
	wb.mux.HandleFunc("GET "+wb.route("/api/projects/{id}/plan"), wb.handleWorkspacePlan)
	wb.mux.HandleFunc("GET "+wb.route("/api/projects/{id}/refs"), wb.handleRefs)
	wb.mux.HandleFunc("POST "+wb.route("/api/projects/{id}/prune"), wb.handleWorktreePrune)
	wb.mux.HandleFunc("GET "+wb.route("/api/projects/{id}/changes-summary"), wb.handleChangesSummary)
	wb.mux.HandleFunc("DELETE "+wb.route("/api/workspaces/{id}"), wb.handleWorkspaceRemove)
	wb.mux.HandleFunc("POST "+wb.route("/api/workspaces/{id}/lsp"), wb.handleLSP)
	wb.mux.HandleFunc("POST "+wb.route("/api/workspaces/{id}/tabs"), wb.handleTabAdd)
	wb.mux.HandleFunc("GET "+wb.route("/api/workspaces/{id}/tabs"), wb.handleWorkspaceTabs)
	wb.mux.HandleFunc("POST "+wb.route("/api/chats"), wb.handleChatAdd)
	wb.mux.HandleFunc("PATCH "+wb.route("/api/chats/{id}"), wb.handleChatUpdate)
	wb.mux.HandleFunc("POST "+wb.route("/api/chat-groups"), wb.handleChatGroupAdd)
	wb.mux.HandleFunc("PATCH "+wb.route("/api/chat-groups/{id}"), wb.handleChatGroupRename)
	wb.mux.HandleFunc("DELETE "+wb.route("/api/chat-groups/{id}"), wb.handleChatGroupRemove)
	wb.mux.HandleFunc("GET "+wb.route("/api/agents"), wb.handleAgents)
	wb.mux.HandleFunc("GET "+wb.route("/api/history"), wb.handleHistory)
	wb.mux.HandleFunc("GET "+wb.route("/api/history/{agent}/{id}/preview"), wb.handleHistoryPreview)
	wb.mux.HandleFunc("POST "+wb.route("/api/history/open"), wb.handleHistoryOpen)
	wb.mux.HandleFunc("POST "+wb.route("/api/history/remove"), wb.handleHistoryRemove)
	wb.mux.HandleFunc("POST "+wb.route("/api/history/sync"), wb.handleHistorySync)
	wb.mux.HandleFunc("DELETE "+wb.route("/api/history/{agent}/{id}"), wb.handleHistoryDelete)
	wb.mux.HandleFunc(wb.route("/api/settings"), wb.handleSettings)
	wb.mux.HandleFunc("PATCH "+wb.route("/api/tabs/{id}"), wb.handleTabRename)
	wb.mux.HandleFunc("DELETE "+wb.route("/api/tabs/{id}"), wb.handleTabRemove)
	wb.mux.HandleFunc("GET "+wb.route("/api/tabs/{id}/term"), wb.handleTerm)
	wb.mux.HandleFunc("POST "+wb.route("/api/tabs/{id}/hook"), wb.handleHook)
	wb.mux.HandleFunc("POST "+wb.route("/api/tabs/{id}/send"), wb.handleTabSend)
	wb.mux.HandleFunc("GET "+wb.route("/api/folders/{dirID}/ledger"), wb.handleLedger)
	wb.mux.HandleFunc("POST "+wb.route("/api/folders/{dirID}/reviews"), wb.handleReview)
	wb.mux.HandleFunc(wb.route("/w/"), wb.handleWorkspace)
	wb.mux.HandleFunc(wb.route("/"), wb.handleIndex)
	if wb.basePath != "/" {
		trimmed := strings.TrimSuffix(wb.basePath, "/")
		wb.mux.HandleFunc(trimmed, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, wb.basePath, http.StatusMovedPermanently)
		})
	}
}

func (wb *workbench) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if refuseRebound(wb.localOnly, w, r) {
		return
	}
	if !strings.HasPrefix(r.URL.Path, wb.route("/w/")) {
		// Proxied responses carry the child's own headers.
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", wbCSP(r.Host))
	}
	if ui.Verbose {
		start := time.Now()
		defer func() {
			ui.Status("info", "http", fmt.Sprintf("%s %s  (%s)", r.Method, r.URL.RequestURI(), ui.FmtDuration(time.Since(start))), 0, os.Stdout)
		}()
	}
	wb.mux.ServeHTTP(w, r)
}

// wbCSP is the app's policy with a websocket to this same host allowed: the
// terminals connect back here, and older browsers do not read 'self' as
// covering ws:. A Host that is not a plain name, address or port is left out
// rather than copied into a header.
func wbCSP(host string) string {
	plain := func(r rune) bool {
		return r == '.' || r == '-' || r == ':' || r == '[' || r == ']' ||
			r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
	}
	if host == "" || strings.IndexFunc(host, func(r rune) bool { return !plain(r) }) >= 0 {
		return contentSecurityPolicy
	}
	return strings.Replace(contentSecurityPolicy, "connect-src 'self';",
		"connect-src 'self' ws://"+host+" wss://"+host+";", 1)
}

// start launches the child for it unless one is already starting or running.
// With forget set, a child that fails before it is ready also takes the item
// out of the list: a mistyped PR URL should not linger there.
func (wb *workbench) start(it wbItem, forget bool) *wbChild {
	wb.mu.Lock()
	defer wb.mu.Unlock()
	if c := wb.children[it.ID]; c != nil && (c.state == "starting" || c.state == "running") {
		return c
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &wbChild{id: it.ID, title: itemTitle(it), state: "starting", done: make(chan struct{}),
		ctx: ctx, cancel: cancel, lastActive: time.Now()}
	wb.children[it.ID] = c
	if wb.closing {
		c.state, c.err = "failed", "punk0 is shutting down"
		cancel()
		close(c.done)
		return c
	}
	go wb.run(c, it, forget)
	return c
}

// run prepares what the child needs (a PR's local clone), starts it, waits
// for it to report its address, then waits for it to exit.
func (wb *workbench) run(c *wbChild, it wbItem, forget bool) {
	defer close(c.done)
	defer c.cancel()
	failed := func(msg string) {
		wb.setState(c, "failed", msg)
		if forget {
			wb.mu.Lock()
			c.gone = true
			wb.mu.Unlock()
			_ = wb.store.Update(func(f *wbFile) { f.Items = withoutItem(f.Items, it.ID) })
		}
	}

	args := append([]string{}, wb.childArgs...)
	args = append(args, "-workbench-child", "-host", "127.0.0.1", "-port", "0",
		"-no-open", "-no-update", "-no-color", "-base-path", wb.childBase(it.ID),
		"-workspace-id", it.ID)
	dir := wb.cwd
	if it.Kind == "pr" {
		// checkoutPR makes a worktree of the clone in its working directory,
		// so start the child in the PR repository's local clone.
		clone, err := wb.prClone(c, it)
		if err != nil {
			if c.ctx.Err() != nil {
				wb.setState(c, "stopped", "")
				return
			}
			if errors.Is(err, errNoClone) {
				wb.mu.Lock()
				c.noClone = true
				wb.mu.Unlock()
			}
			failed(err.Error())
			return
		}
		dir = clone
		args = append(args, it.URL)
	} else {
		// Language servers are off unless the folder asked for them: an
		// orchestrator keeps many folders open and one server per folder
		// costs gigabytes. A parent started with -no-lsp already put the flag
		// in childArgs, which forces them off everywhere.
		if !wb.records.LSPOn(it.Path) && !slices.Contains(args, "-no-lsp") {
			args = append(args, "-no-lsp")
		}
		args = append(args, it.Path)
	}

	cmd := exec.Command(wb.exe, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	ready := make(chan string, 1)
	out := &lineWriter{fn: func(line string) {
		if i := strings.Index(line, wbReadyPrefix); i >= 0 {
			select {
			case ready <- strings.TrimSpace(line[i+len(wbReadyPrefix):]):
			default:
			}
			return
		}
		c.appendLog(line)
	}}
	cmd.Stdout = out
	cmd.Stderr = &lineWriter{fn: c.appendLog}
	// A language server or harness the child started could hold its output
	// open after it exits; do not let that keep Wait from returning.
	cmd.WaitDelay = wbStopGrace
	stdin, err := cmd.StdinPipe()
	if err == nil {
		wb.mu.Lock()
		if c.ctx.Err() != nil {
			err = context.Canceled
		} else if err = cmd.Start(); err == nil {
			c.cmd, c.stdin, c.progress = cmd, stdin, ""
		}
		wb.mu.Unlock()
	}
	if err == context.Canceled {
		wb.setState(c, "stopped", "")
		return
	}
	if err != nil {
		failed(err.Error())
		return
	}

	exited := make(chan error, 1)
	go func() {
		err := cmd.Wait()
		out.Flush()
		exited <- err
	}()

	timer := time.NewTimer(wbStartTimeout)
	defer timer.Stop()
	select {
	case addr := <-ready:
		target, err := url.Parse("http://" + addr)
		if err != nil {
			cmd.Process.Kill()
			<-exited
			failed("bad address from child: " + addr)
			return
		}
		proxy := &httputil.ReverseProxy{
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.SetURL(target)
				// The child checks Host and Origin on every POST; keep the
				// browser's so they still agree with each other.
				pr.Out.Host = pr.In.Host
			},
			// The editor page's terminal connects back here, so its policy
			// allows this host's websocket as the agents page's does.
			ModifyResponse: func(res *http.Response) error {
				if res.Header.Get("Content-Security-Policy") != "" {
					res.Header.Set("Content-Security-Policy", wbCSP(res.Request.Host))
				}
				return nil
			},
			FlushInterval: -1, // event streams must not sit in a buffer
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				fail(w, http.StatusBadGateway, "workspace is not responding: "+err.Error())
			},
		}
		wb.mu.Lock()
		c.addr, c.proxy, c.state = addr, proxy, "running"
		wb.mu.Unlock()
		go wb.learnTitle(c, it)
	case err := <-exited:
		msg := c.failure()
		if err == nil && msg == "" {
			msg = "punk0 exited before it was ready"
		}
		failed(msg)
		return
	case <-timer.C:
		cmd.Process.Kill()
		<-exited
		failed("timed out while starting: " + c.failure())
		return
	}

	err = <-exited
	if err != nil && !wb.isClosing() {
		wb.setState(c, "stopped", c.failure())
		return
	}
	wb.setState(c, "stopped", "")
}

func (wb *workbench) setState(c *wbChild, state, msg string) {
	wb.mu.Lock()
	c.state, c.err, c.proxy, c.progress = state, msg, nil, ""
	wb.mu.Unlock()
}

func (wb *workbench) setProgress(c *wbChild, msg string) {
	wb.mu.Lock()
	c.progress = msg
	wb.mu.Unlock()
}

func (wb *workbench) isClosing() bool {
	wb.mu.Lock()
	defer wb.mu.Unlock()
	return wb.closing
}

// learnTitle asks a freshly started child for its PR title so the list shows
// more than a number.
func (wb *workbench) learnTitle(c *wbChild, it wbItem) {
	if it.Kind != "pr" {
		return
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + c.addr + wb.childBase(it.ID) + "api/meta")
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var meta struct {
		PR *struct {
			Title string `json:"title"`
		} `json:"pr"`
	}
	if json.NewDecoder(resp.Body).Decode(&meta) != nil || meta.PR == nil || meta.PR.Title == "" {
		return
	}
	_ = wb.store.Update(func(f *wbFile) {
		for i := range f.Items {
			if f.Items[i].ID == it.ID {
				f.Items[i].Title = meta.PR.Title
			}
		}
	})
}

// stop closes the child's stdin, which it takes as the signal to shut down,
// and kills it if it is still around after wbStopGrace. A child still
// preparing (cloning) is cancelled instead.
func (wb *workbench) stop(id string) {
	wb.mu.Lock()
	c := wb.children[id]
	var cmd *exec.Cmd
	if c != nil {
		cmd = c.cmd
		c.cancel()
	}
	wb.mu.Unlock()
	if c == nil {
		return
	}
	if cmd != nil {
		c.stdin.Close()
	}
	select {
	case <-c.done:
	case <-time.After(wbStopGrace):
		if cmd != nil {
			cmd.Process.Kill()
		}
		<-c.done
	}
}

// Close stops everything punk0 owns: the terminals first, so agents are hung
// up while the folders they run in are still served, then every child, in
// parallel.
func (wb *workbench) Close() {
	wb.mu.Lock()
	wb.closing = true
	ids := make([]string, 0, len(wb.children))
	for id := range wb.children {
		ids = append(ids, id)
	}
	wb.mu.Unlock()
	wb.cancelClones()
	wb.records.Close()
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func() { defer wg.Done(); wb.stop(id) }()
	}
	wg.Wait()
	// A turn snapshot still being taken finishes, and is saved, first.
	wb.ledgers.wait()
}

// reapIdle stops what nobody is using: a child nobody has had open for
// workspace.idleStop minutes, and a terminal that has carried nothing for
// terminal.idleStop minutes. Both are read every tick, so a changed setting
// takes effect without a restart; 0 means never.
func (wb *workbench) reapIdle() {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for range tick.C {
		cfg := settings.Read()
		wb.reapChildren(idleLimit(cfg.WorkspaceIdleStop, wbWorkspaceIdleStop))
		wb.reapTabs(idleLimit(cfg.TerminalIdleStop, wbTerminalIdleStop))
	}
}

// idleLimit turns a setting in minutes into a duration; an unset setting is
// the default, and 0 means never.
func idleLimit(set *float64, def float64) time.Duration {
	if set != nil {
		def = *set
	}
	if def <= 0 {
		return 0
	}
	return time.Duration(def * float64(time.Minute))
}

func (wb *workbench) reapChildren(limit time.Duration) {
	if limit == 0 {
		return
	}
	var idle []string
	wb.mu.Lock()
	for id, c := range wb.children {
		if c.state == "running" && c.active == 0 && time.Since(c.lastActive) > limit {
			idle = append(idle, id)
		}
	}
	wb.mu.Unlock()
	for _, id := range idle {
		ui.Status("info", "stopped idle workspace", id, 0, os.Stdout)
		wb.stop(id)
	}
}

func (wb *workbench) reapTabs(limit time.Duration) {
	if limit == 0 {
		return
	}
	for _, t := range wb.records.Tabs() {
		sh := t.shell()
		if sh == nil {
			continue
		}
		if stopIdleTab(t.Kind, t.Status(), sh.Foreground(), sh.IdleFor(), limit) {
			ui.Status("info", "stopped idle terminal", t.ID, 0, os.Stdout)
			t.close()
		}
	}
}

// stopIdleTab decides whether a terminal nothing has used for idleFor may be
// hung up. A shell only while it sits at its prompt, with no job in front of
// it; an agent only when its hooks say it is done, since one that is working,
// waiting for an answer, or reporting nothing at all may lose work.
func stopIdleTab(kind, status string, foregroundIsShell bool, idleFor, limit time.Duration) bool {
	if limit == 0 || idleFor < limit {
		return false
	}
	if kind == tabClaude {
		return status == statusIdle
	}
	return foregroundIsShell
}

/* ---------- HTTP ---------- */

// handleSettings serves the same settings API as the editor, so the agents
// page can open the one settings modal.
func (wb *workbench) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && !trustedGet(w, r) {
		return
	}
	serveSettings(w, r, wb.localOnly, nil)
}

// trustedGet refuses reads whose Host is a name other than localhost: the
// folder browser lists the whole disk, and a DNS-rebound page must not read it.
func trustedGet(w http.ResponseWriter, r *http.Request) bool {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host != "localhost" && net.ParseIP(host) == nil {
		fail(w, http.StatusForbidden, "open punk0 by IP address or localhost")
		return false
	}
	return true
}

func (wb *workbench) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != wb.basePath {
		http.NotFound(w, r)
		return
	}
	b, err := fs.ReadFile(assets, "web/workbench.html")
	if err != nil {
		fail(w, 500, err.Error())
		return
	}
	html := strings.Replace(string(b), `<base href="/">`, fmt.Sprintf(`<base href="%s">`, template.HTMLEscapeString(wb.basePath)), 1)
	html = strings.Replace(html, `data-theme="catppuccin-mocha"`, fmt.Sprintf(`data-theme="%s"`, template.HTMLEscapeString(wbTheme())), 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, html)
}

func wbTheme() string {
	if t := settings.Read().WorkbenchColorTheme; t != nil && *t != "" {
		return *t
	}
	return "catppuccin-mocha"
}

func (wb *workbench) handlePing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"workbench": true, "version": version})
}

type wbItemView struct {
	wbItem
	State    string `json:"state"`
	Error    string `json:"error,omitempty"`
	Progress string `json:"progress,omitempty"`
	Href     string `json:"href"`
}

func (wb *workbench) view(it wbItem) wbItemView {
	v := wbItemView{wbItem: it, State: "stopped", Href: wb.childHref(it.ID)}
	wb.mu.Lock()
	if c := wb.children[it.ID]; c != nil {
		v.State, v.Error, v.Progress = c.state, c.err, c.progress
	}
	wb.mu.Unlock()
	return v
}

func (wb *workbench) handleState(w http.ResponseWriter, r *http.Request) {
	if !trustedGet(w, r) {
		return
	}
	items := wb.store.Load().Items
	views := make([]wbItemView, 0, len(items))
	for _, it := range items {
		views = append(views, wb.view(it))
	}
	home, _ := os.UserHomeDir()
	writeJSON(w, map[string]any{
		"cwd":     wb.cwd,
		"home":    home,
		"sep":     string(filepath.Separator),
		"version": version,
		"items":   views,
	})
}

type wbEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Git  bool   `json:"git,omitempty"`
}

// hasGitMarker reports whether dir has a .git directory or file.
func hasGitMarker(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

// handleComplete completes the folder name being typed: q's last path
// element is a case-insensitive prefix of folders in its parent. A q ending
// in a separator lists the folder itself. Each value keeps the q's own
// spelling of the parent (~, relative), so the input does not jump. Typing
// gets the first 50; all=1, for the New chat folder browser, gets them all.
func (wb *workbench) handleComplete(w http.ResponseWriter, r *http.Request) {
	if !trustedGet(w, r) {
		return
	}
	q := r.URL.Query().Get("q")
	out := []wbEntry{}
	if strings.TrimSpace(q) == "" || strings.Contains(q, "://") {
		writeJSON(w, map[string]any{"entries": out})
		return
	}
	typedDir, prefix := q, ""
	if !strings.HasSuffix(q, "/") && !strings.HasSuffix(q, string(filepath.Separator)) {
		if i := strings.LastIndexAny(q, `/`+string(filepath.Separator)); i >= 0 {
			typedDir, prefix = q[:i+1], q[i+1:]
		} else if q == "~" {
			typedDir, prefix = "~/", ""
		} else {
			typedDir, prefix = "", q
		}
	}
	dir, err := expandPath(typedDir, wb.cwd)
	if err != nil {
		writeJSON(w, map[string]any{"entries": out})
		return
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		writeJSON(w, map[string]any{"entries": out})
		return
	}
	lp := strings.ToLower(prefix)
	for _, e := range ents {
		name := e.Name()
		if !strings.HasPrefix(strings.ToLower(name), lp) || (strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".")) {
			continue
		}
		full := filepath.Join(dir, name)
		isDir := e.IsDir()
		if e.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(full); err == nil {
				isDir = st.IsDir()
			}
		}
		if !isDir {
			continue
		}
		out = append(out, wbEntry{Name: name, Path: typedDir + name + "/", Git: hasGitMarker(full)})
	}
	sort.Slice(out, func(i, j int) bool {
		// Exact-case prefix matches first, then alphabetical.
		ai, aj := strings.HasPrefix(out[i].Name, prefix), strings.HasPrefix(out[j].Name, prefix)
		if ai != aj {
			return ai
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	if len(out) > 50 && r.URL.Query().Get("all") != "1" {
		out = out[:50]
	}
	writeJSON(w, map[string]any{"dir": dir, "entries": out})
}

// expandPath resolves ~ and relative paths (against base) to a clean absolute path.
func expandPath(p, base string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		p = base
	}
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, p[1:])
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	return filepath.Clean(p), nil
}

func readJSONBody(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(v)
}

// handleOpen takes what the page's box holds: a pull request URL, which this
// records and starts, or a folder, which is registered as a project. The page
// then navigates to href.
func (wb *workbench) handleOpen(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var body struct {
		Target string `json:"target"`
		ID     string `json:"id"`
		// For a PR: the folder of a local clone of its repository, or
		// "managed" to let px0 clone it. Asked for when none is found.
		Clone string `json:"clone"`
	}
	if err := readJSONBody(r, &body); err != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	var it wbItem
	if body.ID != "" {
		known, ok := wb.store.Get(body.ID)
		if !ok {
			fail(w, http.StatusNotFound, "unknown workspace")
			return
		}
		it = known
	} else {
		var dir string
		var err error
		if it, dir, err = wb.resolveTarget(body.Target); err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		if dir != "" {
			// A folder is not an item: registering it as a project answers
			// with the workspace to open, like POST /api/projects.
			wb.openFolder(w, dir, false)
			return
		}
	}
	if it.Kind == "pr" {
		switch body.Clone {
		case "":
			if wb.findClone(r.Context(), it.Repo) == "" {
				// Let the page ask where the clone is, before anything is recorded.
				writeJSON(w, map[string]any{"needClone": true, "repo": it.Repo, "managed": tildePath(managedCloneDir(it.Repo))})
				return
			}
		case "managed":
			wb.rememberClone(it.Repo, managedCloneDir(it.Repo))
		default:
			dir, err := expandPath(body.Clone, wb.cwd)
			if err != nil {
				fail(w, http.StatusBadRequest, err.Error())
				return
			}
			top := cloneOf(dir, it.Repo)
			if top == "" {
				fail(w, http.StatusBadRequest, fmt.Sprintf("%s is not a clone of %s: none of its remotes points there", tildePath(dir), it.Repo))
				return
			}
			wb.rememberClone(it.Repo, top)
		}
	}
	_, known := wb.store.Get(it.ID)
	if err := wb.store.Record(it); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	wb.start(it, !known)
	writeJSON(w, map[string]any{"id": it.ID, "href": wb.childHref(it.ID)})
}

// resolveTarget classifies what was typed or passed on the command line: a
// pull request, which is an item of its own, or a folder, which is not --
// folders live in records, keyed on their path.
func (wb *workbench) resolveTarget(target string) (wbItem, string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return wbItem{}, "", errors.New("enter a directory path or a pull request URL")
	}
	if _, pt, ok := DetectPRURL(target); ok {
		if gitDisabled {
			return wbItem{}, "", errors.New("git is required for PR review; restart punk0 without -no-git")
		}
		return wbPRItem(pt), "", nil
	}
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		return wbItem{}, "", errors.New("not a pull or merge request URL (e.g. " + prURLExamples + ")")
	}
	p, err := expandPath(target, wb.cwd)
	if err != nil {
		return wbItem{}, "", err
	}
	root, _, _, err := resolveTarget(p)
	if err != nil {
		return wbItem{}, "", err
	}
	return wbItem{}, root, nil
}

func (wb *workbench) handleStop(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if readJSONBody(r, &body) != nil || body.ID == "" {
		fail(w, http.StatusBadRequest, "id is required")
		return
	}
	wb.stop(body.ID)
	writeJSON(w, map[string]any{"ok": true})
}

/* ---------- projects, workspaces and tabs ---------- */

// handleProjectList is what the orchestrator page polls: the whole tree, with
// the state of the child serving each folder.
func (wb *workbench) handleProjectList(w http.ResponseWriter, r *http.Request) {
	if !trustedGet(w, r) {
		return
	}
	views := wb.records.List()
	for i := range views {
		for j := range views[i].Workspaces {
			wb.fillWorkspace(&views[i].Workspaces[j])
		}
	}
	wb.restartNewRepos(views)
	writeJSON(w, views)
}

// restartNewRepos stops the editor of a project folder that has become a git
// repository since it was last listed (git init after adding it). An editor
// decides at start whether it shows git, so the next request starts one that
// does.
func (wb *workbench) restartNewRepos(views []projectView) {
	var stale []string
	wb.mu.Lock()
	if wb.gitSeen == nil {
		wb.gitSeen = map[string]bool{}
	}
	for _, pv := range views {
		if was, ok := wb.gitSeen[pv.Dir]; ok && !was && pv.Git {
			stale = append(stale, wbItemID("dir", pv.Dir))
		}
		wb.gitSeen[pv.Dir] = pv.Git
	}
	wb.mu.Unlock()
	for _, id := range stale {
		go wb.stop(id) // the list does not wait for a child to shut down
	}
}

// fillWorkspace adds what only the workbench knows: where the folder's editor
// view is, and whether its child is running.
func (wb *workbench) fillWorkspace(wv *workspaceView) {
	id := wbItemID("dir", wv.Dir)
	wv.Href, wv.Backend = wb.childHref(id), wb.childState(id)
}

func (wb *workbench) handleProjectAdd(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var body struct {
		Dir string `json:"dir"`
	}
	if readJSONBody(r, &body) != nil || strings.TrimSpace(body.Dir) == "" {
		fail(w, http.StatusBadRequest, "dir is required")
		return
	}
	dir, err := expandPath(body.Dir, wb.cwd)
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	wb.openFolder(w, dir, false)
}

// openFolder registers a folder and answers with the workspace that opens it:
// the Add project panel, `punk0 <dir>` in a terminal and a second punk0 handing
// its folder over all come through here, and all of them may name a folder
// that is already registered. owned is set only by the paths that made the
// folder themselves, since it is what lets removing the project delete it.
func (wb *workbench) openFolder(w http.ResponseWriter, dir string, owned bool) {
	p, ws, err := wb.records.Open(dir)
	if err != nil {
		failRecord(w, err)
		return
	}
	if owned {
		if err := wb.records.SetOwned(p.ID); err != nil {
			failRecord(w, err)
			return
		}
	}
	writeJSON(w, map[string]any{"project": p.ID, "workspace": ws.ID, "href": wb.childHref(wbItemID("dir", ws.Dir))})
}

// handleProjectRemove takes the Remove project dialog's answers:
// ?files=1&confirm=<name> moves the folder to the trash.
func (wb *workbench) handleProjectRemove(w http.ResponseWriter, r *http.Request) {
	if !localOrigin(w, r) {
		return
	}
	note, err := wb.records.RemoveProject(r.PathValue("id"), removeOptions(r))
	if err != nil {
		failRecord(w, err)
		return
	}
	wb.stopOrphans()
	writeNote(w, note)
}

// removeOptions reads a removal dialog's answers off the query string.
func removeOptions(r *http.Request) RemoveOptions {
	q := r.URL.Query()
	return RemoveOptions{
		Force:   q.Get("force") == "1",
		Files:   q.Get("files") == "1",
		Confirm: q.Get("confirm"),
	}
}

// writeNote answers a removal that worked: note says what it could not do
// along the way (a branch git kept), which is never a reason to fail it.
func writeNote(w http.ResponseWriter, note string) {
	res := map[string]any{"ok": true}
	if note != "" {
		res["note"] = note
	}
	writeJSON(w, res)
}

// handleWorkspaceAdd makes a workspace from the New workspace panel
// (newworkspace.go). A pull request typed there is worked on: its head is
// fetched and checked out in a worktree of its own — unless its head branch
// is checked out already, when the panel offered Open or Adopt and nothing
// is fetched.
func (wb *workbench) handleWorkspaceAdd(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var o WorkspaceOptions
	if readJSONBody(r, &o) != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	id := r.PathValue("id")
	if dir := wb.records.ProjectDir(id); dir != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		pp, ok, err := findProjectPull(ctx, dir, o.Name)
		cancel()
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		if ok {
			// The plan decides first: Open and Adopt check nothing out, so
			// fetching the head would only make the click slow or fail.
			po := pp.options(o)
			if pl := wb.records.Plan(id, po); pl.Action == planAdopt || pl.Action == planOpen {
				o = po
			} else if o, err = pp.workOn(dir, o); err != nil {
				fail(w, http.StatusBadRequest, err.Error())
				return
			}
		}
	}
	ws, warning, err := wb.records.AddWorkspace(id, o)
	if err != nil {
		failRecord(w, err)
		return
	}
	view := wb.records.View(ws)
	wb.fillWorkspace(&view)
	writeJSON(w, struct {
		workspaceView
		Warning string `json:"warning,omitempty"`
	}{view, warning})
}

// handleWorkspacePlan answers the panel as it is filled in: what creating
// the workspace would do, or why it cannot. A pull request is looked up on
// its forge, so the panel can offer to review it or work on it.
func (wb *workbench) handleWorkspacePlan(w http.ResponseWriter, r *http.Request) {
	if !trustedGet(w, r) {
		return
	}
	q := r.URL.Query()
	o := WorkspaceOptions{
		Name:     q.Get("name"),
		Worktree: q.Get("worktree") == "1",
		Base:     q.Get("base"),
	}
	id := r.PathValue("id")
	if dir := wb.records.ProjectDir(id); dir != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		pp, ok, err := findProjectPull(ctx, dir, o.Name)
		cancel()
		if err != nil {
			writeJSON(w, WorkspacePlan{Worktree: true, Action: planPR, Error: err.Error()})
			return
		}
		if ok {
			// The base is the PR's head, fetched only when the worktree is made;
			// a head branch checked out already is opened or adopted instead.
			pl := wb.records.Plan(id, pp.options(o))
			if pl.Action != planAdopt && pl.Action != planOpen {
				pl.Action, pl.Base, pl.Fetch = planPR, "", ""
			}
			pl.PR = pp.view()
			pl.Dir = tildePath(pl.Dir)
			writeJSON(w, pl)
			return
		}
	}
	pl := wb.records.Plan(id, o)
	pl.Dir = tildePath(pl.Dir)
	writeJSON(w, pl)
}

// handleRefs lists a project's branches for the panel's start point, with
// the one a new branch starts from by default, and its worktrees other than
// the project folder, each with the workspace running in it.
func (wb *workbench) handleRefs(w http.ResponseWriter, r *http.Request) {
	if !trustedGet(w, r) {
		return
	}
	dir := wb.records.ProjectDir(r.PathValue("id"))
	if dir == "" {
		fail(w, http.StatusNotFound, "project not found")
		return
	}
	if !gitAvailable(dir) {
		writeJSON(w, map[string]any{"default": "", "refs": []string{}, "worktrees": []worktreeInfo{}})
		return
	}
	writeJSON(w, map[string]any{"default": defaultBase(dir), "refs": gitRefs(dir), "empty": !hasCommit(dir), "worktrees": wb.records.Worktrees(dir)})
}

// handleWorktreePrune is the panel's Prune: git forgets the project's
// worktrees whose folders were deleted.
func (wb *workbench) handleWorktreePrune(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	if err := wb.records.PruneWorktrees(r.PathValue("id")); err != nil {
		failRecord(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleWorkspaceRemove takes the Delete workspace dialog's answers:
// ?files=1 removes the worktree folder. It answers 409 while the worktree has
// uncommitted changes; ?force=1 deletes them. note says why a merged-branch
// delete did not happen.
func (wb *workbench) handleWorkspaceRemove(w http.ResponseWriter, r *http.Request) {
	if !localOrigin(w, r) {
		return
	}
	note, err := wb.records.RemoveWorkspace(r.PathValue("id"), removeOptions(r))
	if err != nil {
		failRecord(w, err)
		return
	}
	wb.stopOrphans()
	writeNote(w, note)
}

// handleLSP turns language servers on or off for a workspace's folder. The
// child is stopped rather than reconfigured: whether it has language servers
// is decided when it starts, and the next request starts one that matches.
func (wb *workbench) handleLSP(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var body struct {
		On bool `json:"on"`
	}
	if readJSONBody(r, &body) != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	ws := wb.records.Workspace(r.PathValue("id"))
	if ws == nil {
		fail(w, http.StatusNotFound, "workspace not found")
		return
	}
	if err := wb.records.SetLSP(ws.Dir, body.On); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	wb.stop(wbItemID("dir", ws.Dir))
	writeJSON(w, map[string]any{"ok": true})
}

/* ---------- scratch chats (scratch.go) ---------- */

// handleChatAdd starts a scratch chat from the New chat dialog. A relative
// or ~ path is resolved like the Add project panel's.
func (wb *workbench) handleChatAdd(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var o ChatOptions
	if readJSONBody(r, &o) != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	if strings.TrimSpace(o.Dir) != "" {
		dir, err := expandPath(o.Dir, wb.cwd)
		if err != nil {
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
		o.Dir = dir
	} else {
		o.Dir = ""
	}
	ws, err := wb.records.NewChat(o)
	if err != nil {
		failRecord(w, err)
		return
	}
	view := wb.records.View(ws)
	wb.fillWorkspace(&view)
	writeJSON(w, view)
}

// handleChatUpdate renames, regroups, archives or restores a chat. An
// archived chat's editor is stopped along with its tabs.
func (wb *workbench) handleChatUpdate(w http.ResponseWriter, r *http.Request) {
	if !localOrigin(w, r) {
		return
	}
	var u ChatUpdate
	if readJSONBody(r, &u) != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	id := r.PathValue("id")
	if err := wb.records.UpdateChat(id, u); err != nil {
		failRecord(w, err)
		return
	}
	if u.Archived != nil && *u.Archived {
		if ws := wb.records.Workspace(id); ws != nil {
			wb.stop(wbItemID("dir", ws.Dir))
		}
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (wb *workbench) handleChatGroupAdd(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if readJSONBody(r, &body) != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	g, err := wb.records.AddChatGroup(body.Name)
	if err != nil {
		failRecord(w, err)
		return
	}
	writeJSON(w, g)
}

func (wb *workbench) handleChatGroupRename(w http.ResponseWriter, r *http.Request) {
	if !localOrigin(w, r) {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if readJSONBody(r, &body) != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	if err := wb.records.RenameChatGroup(r.PathValue("id"), body.Name); err != nil {
		failRecord(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (wb *workbench) handleChatGroupRemove(w http.ResponseWriter, r *http.Request) {
	if !localOrigin(w, r) {
		return
	}
	if err := wb.records.RemoveChatGroup(r.PathValue("id")); err != nil {
		failRecord(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (wb *workbench) handleWorkspaceTabs(w http.ResponseWriter, r *http.Request) {
	if !trustedGet(w, r) {
		return
	}
	ws := wb.records.WorkspaceAt(r.PathValue("id"))
	if ws == nil {
		fail(w, http.StatusNotFound, "workspace not found")
		return
	}
	writeJSON(w, map[string]any{"tabs": wb.records.View(ws).Tabs})
}

func (wb *workbench) handleTabAdd(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var body struct {
		Kind    string `json:"kind"`
		Session string `json:"session"` // optional: the agent's chat to resume
	}
	if readJSONBody(r, &body) != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	ws := wb.records.WorkspaceAt(r.PathValue("id"))
	if ws == nil {
		fail(w, http.StatusNotFound, "workspace not found")
		return
	}
	t, err := wb.records.AddTab(ws.ID, body.Kind, body.Session)
	if err != nil {
		failRecord(w, err)
		return
	}
	writeJSON(w, t.view())
}

func (wb *workbench) handleTabRename(w http.ResponseWriter, r *http.Request) {
	if !localOrigin(w, r) {
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if readJSONBody(r, &body) != nil {
		fail(w, http.StatusBadRequest, "bad request")
		return
	}
	if err := wb.records.RenameTab(r.PathValue("id"), body.Name); err != nil {
		failRecord(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (wb *workbench) handleTabRemove(w http.ResponseWriter, r *http.Request) {
	if !localOrigin(w, r) {
		return
	}
	if err := wb.records.RemoveTab(r.PathValue("id")); err != nil {
		failRecord(w, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// failRecord answers a records error: 404 for an unknown id, 409 for a
// worktree with uncommitted changes, which the page may repeat with force,
// 400 for the rest.
func failRecord(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errNotFound):
		fail(w, http.StatusNotFound, err.Error())
	case errors.Is(err, errDirty):
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]any{"error": err.Error(), "dirty": true})
	default:
		fail(w, http.StatusBadRequest, err.Error())
	}
}

// stopOrphans stops the children of folders records no longer holds, after a
// project or workspace was removed.
func (wb *workbench) stopOrphans() {
	wb.mu.Lock()
	ids := make([]string, 0, len(wb.children))
	for id := range wb.children {
		ids = append(ids, id)
	}
	wb.mu.Unlock()
	for _, id := range ids {
		if _, ok := wb.item(id); ok {
			continue
		}
		wb.stop(id)
		wb.mu.Lock()
		delete(wb.children, id)
		wb.mu.Unlock()
	}
}

func (wb *workbench) handleRemove(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if readJSONBody(r, &body) != nil || body.ID == "" {
		fail(w, http.StatusBadRequest, "id is required")
		return
	}
	wb.stop(body.ID)
	wb.mu.Lock()
	delete(wb.children, body.ID)
	wb.mu.Unlock()
	err := wb.store.Update(func(f *wbFile) { f.Items = withoutItem(f.Items, body.ID) })
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleWorkspace serves /w/<id>/...: proxied to the child, which any request
// starts. The page gets a holding page that refreshes until the child is
// ready, so a bookmarked or reloaded workspace just reopens; every other
// request waits for it instead, since nothing behind it would retry.
func (wb *workbench) handleWorkspace(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, wb.route("/w/"))
	id, _, hasSlash := strings.Cut(rest, "/")
	if id == "" {
		http.Redirect(w, r, wb.basePath, http.StatusFound)
		return
	}
	if !hasSlash {
		http.Redirect(w, r, wb.childBase(id), http.StatusFound)
		return
	}
	if wb.serveProxy(w, r, id) {
		return
	}

	wb.mu.Lock()
	c := wb.children[id]
	var snap wbChild
	if c != nil {
		snap = wbChild{state: c.state, err: c.err, title: c.title, gone: c.gone, noClone: c.noClone}
	}
	wb.mu.Unlock()
	it, known := wb.item(id)

	if r.URL.Path != wb.childBase(id) || r.Method != http.MethodGet {
		if !known {
			fail(w, http.StatusNotFound, "unknown workspace")
			return
		}
		wb.start(it, false)
		if wb.waitRunning(id) && wb.serveProxy(w, r, id) {
			return
		}
		fail(w, http.StatusServiceUnavailable, "workspace is not responding")
		return
	}
	if !trustedGet(w, r) {
		return
	}
	if snap.state == "failed" && r.URL.Query().Get("retry") == "" {
		p := holdingPage{Title: snap.title, Error: snap.err}
		if known && snap.noClone {
			p.Choose = wb.basePath + "?open=" + url.QueryEscape(it.URL)
		} else if !snap.gone {
			p.Retry = wb.childBase(id) + "?retry=1"
		}
		wb.renderHolding(w, p)
		return
	}
	if !known {
		w.WriteHeader(http.StatusNotFound)
		wb.renderHolding(w, holdingPage{Title: "Not found", Error: "This workspace is not in punk0 any more."})
		return
	}
	// The refresh drops ?retry, so a second failure stops on the error page,
	// and keeps the rest of the query: the file and line `punk0 <file>:<n>`
	// asked for must survive a cold start.
	if snap.state != "starting" {
		if it.Kind == "pr" {
			_ = wb.store.Record(it)
		}
		c = wb.start(it, false)
	}
	wb.mu.Lock()
	progress := c.progress
	wb.mu.Unlock()
	self := wb.childBase(id)
	q := r.URL.Query()
	q.Del("retry")
	if len(q) > 0 {
		self += "?" + q.Encode()
	}
	wb.renderHolding(w, holdingPage{Title: itemTitle(it), Self: self, Progress: progress, Starting: true})
}

// serveProxy hands the request to the child when one is running, counting it
// so the idle reaper leaves a workspace in use alone.
func (wb *workbench) serveProxy(w http.ResponseWriter, r *http.Request, id string) bool {
	wb.mu.Lock()
	c := wb.children[id]
	var proxy *httputil.ReverseProxy
	if c != nil && c.state == "running" {
		proxy = c.proxy
		c.active++
	}
	wb.mu.Unlock()
	if proxy == nil {
		return false
	}
	defer func() {
		wb.mu.Lock()
		c.active--
		c.lastActive = time.Now()
		wb.mu.Unlock()
	}()
	proxy.ServeHTTP(w, r)
	return true
}

// waitRunning waits out a starting child, up to wbAPIWait: a cold folder
// takes a moment to index, a PR checkout longer than any request should wait.
func (wb *workbench) waitRunning(id string) bool {
	deadline := time.Now().Add(wbAPIWait)
	for {
		wb.mu.Lock()
		state := ""
		if c := wb.children[id]; c != nil {
			state = c.state
		}
		wb.mu.Unlock()
		if state == "running" {
			return true
		}
		if state != "starting" || time.Now().After(deadline) {
			return false
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func withoutItem(items []wbItem, id string) []wbItem {
	kept := items[:0]
	for _, it := range items {
		if it.ID != id {
			kept = append(kept, it)
		}
	}
	return kept
}

func itemTitle(it wbItem) string {
	if it.Kind == "pr" {
		t := fmt.Sprintf("%s#%d", it.Repo, it.Number)
		if it.Title != "" {
			t += " " + it.Title
		}
		return t
	}
	return it.Path
}

type holdingPage struct {
	Base, Theme, Title, Error, Retry, Self, Progress string
	Choose                                           string // workbench URL that asks where the PR's clone is
	Starting                                         bool
}

var holdingTmpl = template.Must(template.New("holding").Parse(`<!doctype html>
<html lang="en" data-theme="{{.Theme}}">
<head>
<meta charset="utf-8">
<base href="{{.Base}}">
<meta name="viewport" content="width=device-width,initial-scale=1">
{{if .Starting}}<meta http-equiv="refresh" content="1; url={{.Self}}">{{end}}
<title>{{if .Starting}}Opening…{{else}}px0{{end}}</title>
<link rel="icon" type="image/x-icon" href="https://px0.ai/favicon.ico">
<link rel="stylesheet" href="static/style.css">
<link rel="stylesheet" href="static/themes.css">
<link rel="stylesheet" href="static/workbench.css">
</head>
<body class="wb">
<main class="wb-holding">
  <div class="empty-logo">
    <img class="logo-dark" src="https://px0.ai/logo/px0-logo-dark.png" alt="px0" width="160" height="60">
    <img class="logo-light" src="https://px0.ai/logo/px0-logo-light.png" alt="px0" width="160" height="60">
  </div>
  {{if .Starting}}
  <p class="wb-holding-msg"><span class="wb-spin" aria-hidden="true"></span>Opening <b>{{.Title}}</b>…</p>
  <p class="wb-faint">{{if .Progress}}{{.Progress}}…{{else}}Pull requests are checked out first; large repositories take a while.{{end}}</p>
  {{else}}
  <p class="wb-holding-msg">Could not open <b>{{.Title}}</b></p>
  <pre class="wb-error">{{.Error}}</pre>
  {{end}}
  <p class="wb-holding-actions">{{if .Retry}}<a class="wb-btn wb-primary" href="{{.Retry}}">Retry</a>{{end}}{{if .Choose}}<a class="wb-btn wb-primary" href="{{.Choose}}">Choose a clone</a>{{end}}<a class="wb-btn" href="{{.Base}}" target="punk0-agents">Back to punk0 agents</a></p>
</main>
</body>
</html>
`))

func (wb *workbench) renderHolding(w http.ResponseWriter, p holdingPage) {
	p.Base, p.Theme = wb.basePath, wbTheme()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	holdingTmpl.Execute(w, p)
}

/* ---------- single instance ---------- */

type wbLock struct {
	PID int    `json:"pid"`
	URL string `json:"url"`
}

// runningWorkbench returns the URL of a workbench that is already serving,
// so a second bare `px0` opens it instead of starting another.
func runningWorkbench() string {
	b, err := os.ReadFile(workbenchLockPath())
	if err != nil {
		return ""
	}
	var l wbLock
	if json.Unmarshal(b, &l) != nil || l.URL == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), wbPingTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(l.URL, "/")+"/api/wb/ping", nil)
	if err != nil {
		return ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var ping struct {
		Workbench bool `json:"workbench"`
	}
	if json.NewDecoder(resp.Body).Decode(&ping) != nil || !ping.Workbench {
		return ""
	}
	return l.URL
}

func writeWorkbenchLock(u string) {
	_ = writeFileAtomic(workbenchLockPath(), wbLock{PID: os.Getpid(), URL: u})
}

// removeWorkbenchLock deletes the lock only if it is still this process's.
func removeWorkbenchLock() {
	b, err := os.ReadFile(workbenchLockPath())
	if err != nil {
		return
	}
	var l wbLock
	if json.Unmarshal(b, &l) == nil && l.PID == os.Getpid() {
		os.Remove(workbenchLockPath())
	}
}

/* ---------- PR local clones ---------- */

// Pull requests opened from the workbench are always checked out as a git
// worktree of a local clone of their repository, never as a fresh clone per
// PR: a worktree shares the clone's objects, so it costs a checkout and
// nothing else. The clone for each repository is found once and remembered
// in workbench.json:
//
//  1. the clone remembered for owner/repo, if it is still one;
//  2. a recorded directory (or the workbench's own directory) inside one;
//  3. a bounded scan of the usual places people keep code (~/workspace,
//     ~/code, ~/src, ...) and the folders around recorded directories.
//
// When none is found the page asks: point at a clone, or let px0 make one,
// once, under the state directory (repos/<owner>/<repo>), to be reused by
// every later PR of that repository.

const (
	wbScanDepth   = 3                       // folder levels below each scan root
	wbScanBudget  = 20000                   // folders looked at per scan, at most
	wbScanTimeout = 1500 * time.Millisecond // wall-clock cap on one scan
)

// wbScanSkip are folder names never worth descending into for a clone.
var wbScanSkip = map[string]bool{
	"node_modules": true, "vendor": true, "target": true, "build": true, "dist": true,
	"Library": true, "Applications": true, "AppData": true, "venv": true, "__pycache__": true,
}

// wbScanRoots are where people usually keep clones, relative to home.
var wbScanRoots = []string{
	"workspace", "workspaces", "code", "src", "projects", "dev", "repos", "git",
	"github", "work", "Developer", "Documents/GitHub", "go/src/github.com", "",
}

func repoKey(repo string) string { return strings.ToLower(repo) }

func splitRepo(repo string) (owner, name string) {
	owner, name, _ = strings.Cut(repo, "/")
	return owner, name
}

// managedCloneDir is where px0 makes a clone when the user asks it to.
func managedCloneDir(repo string) string {
	owner, name := splitRepo(repo)
	return filepath.Join(workbenchStateDir(), "repos", owner, name)
}

// cloneOf returns the top level of dir's repository when one of its remotes
// is repo ("owner/name"), or "".
func cloneOf(dir, repo string) string {
	owner, name := splitRepo(repo)
	top, _ := findPRRemote(dir, owner, name)
	return top
}

// knownClone looks only where it is cheap to: the remembered clone, then
// dirs (the registered project folders and the current directory).
func knownClone(f wbFile, repo string, dirs ...string) string {
	if p := f.Clones[repoKey(repo)]; p != "" {
		if top := cloneOf(p, repo); top != "" {
			return top
		}
	}
	seen := map[string]bool{}
	for _, d := range dirs {
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		if top := cloneOf(d, repo); top != "" {
			return top
		}
	}
	return ""
}

// scanForClone walks roots breadth-first, a few levels deep, for a folder
// named like the repository that is a clone of it. Bounded in folders and time.
func scanForClone(ctx context.Context, roots []string, repo string) string {
	_, name := splitRepo(repo)
	ctx, cancel := context.WithTimeout(ctx, wbScanTimeout)
	defer cancel()
	type node struct {
		path  string
		depth int
	}
	seen := map[string]bool{}
	var queue []node
	for _, r := range roots {
		if r != "" && !seen[r] {
			seen[r] = true
			queue = append(queue, node{r, 0})
		}
	}
	budget := wbScanBudget
	for len(queue) > 0 && budget > 0 && ctx.Err() == nil {
		n := queue[0]
		queue = queue[1:]
		ents, err := os.ReadDir(n.path)
		if err != nil {
			continue
		}
		for _, e := range ents {
			budget--
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || wbScanSkip[e.Name()] {
				continue
			}
			p := filepath.Join(n.path, e.Name())
			if seen[p] {
				continue
			}
			seen[p] = true
			if strings.EqualFold(e.Name(), name) {
				if hasGitMarker(p) {
					if top := cloneOf(p, repo); top != "" {
						return top
					}
				}
			}
			if n.depth+1 < wbScanDepth {
				queue = append(queue, node{p, n.depth + 1})
			}
		}
	}
	return ""
}

// scanRoots lists the usual code folders under home plus the parents of the
// project folders, which is where other clones tend to sit.
func scanRoots(dirs []string, cwd string) []string {
	var roots []string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		for _, r := range wbScanRoots {
			roots = append(roots, filepath.Join(home, r))
		}
	}
	for _, d := range dirs {
		roots = append(roots, filepath.Dir(d))
	}
	if cwd != "" {
		roots = append(roots, cwd, filepath.Dir(cwd))
	}
	return roots
}

// findClone finds and remembers the local clone of repo, or returns "".
func (wb *workbench) findClone(ctx context.Context, repo string) string {
	f := wb.store.Load()
	dirs := wb.records.ProjectDirs()
	top := knownClone(f, repo, append(dirs, wb.cwd)...)
	if top == "" {
		top = scanForClone(ctx, scanRoots(dirs, wb.cwd), repo)
	}
	if top != "" {
		wb.rememberClone(repo, top)
	}
	return top
}

func (wb *workbench) rememberClone(repo, dir string) {
	_ = wb.store.Update(func(f *wbFile) {
		if f.Clones == nil {
			f.Clones = map[string]string{}
		}
		f.Clones[repoKey(repo)] = dir
	})
}

var errNoClone = errors.New("no local clone")

// prClone returns the clone a PR's child should start in. The remembered
// clone wins; a remembered px0-made clone that is missing is made again.
func (wb *workbench) prClone(c *wbChild, it wbItem) (string, error) {
	f := wb.store.Load()
	remembered := f.Clones[repoKey(it.Repo)]
	if remembered != "" && remembered == managedCloneDir(it.Repo) {
		if top := cloneOf(remembered, it.Repo); top != "" {
			return top, nil
		}
		return wb.makeClone(c, it)
	}
	wb.setProgress(c, "Looking for a local clone of "+it.Repo)
	if top := wb.findClone(c.ctx, it.Repo); top != "" {
		return top, nil
	}
	return "", fmt.Errorf("%w of %s found; open the pull request again from the workbench to choose one", errNoClone, it.Repo)
}

// cloneLocks serializes makeClone per repository, so two PRs of one repository
// opened together do not both clone it.
var cloneLocks sync.Map

// makeClone clones the PR's repository into managedCloneDir, once. The clone
// is partial (blobs on demand) and has no checkout of its own: worktrees are
// all it is for.
func (wb *workbench) makeClone(c *wbChild, it wbItem) (string, error) {
	mu, _ := cloneLocks.LoadOrStore(repoKey(it.Repo), &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()

	dest := managedCloneDir(it.Repo)
	if top := cloneOf(dest, it.Repo); top != "" {
		return top, nil // made while this one waited
	}
	provider, pt, ok := DetectPRURL(it.URL)
	if !ok {
		return "", fmt.Errorf("not a pull request URL: %s", it.URL)
	}
	token, _ := provider.ResolveToken(settings.Read(), pt)
	wb.setProgress(c, fmt.Sprintf("Cloning %s into %s (once; later pull requests reuse it)", it.Repo, tildePath(dest)))

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	// Clone beside dest and rename, so an interrupted clone never looks done.
	tmp, err := os.MkdirTemp(filepath.Dir(dest), "."+filepath.Base(dest)+"-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	auth := provider.GitAuth(pt, token)
	cmd := gitAuthCmd(auth, "clone", "--filter=blob:none", "--no-checkout", provider.CloneURL(pt), tmp)
	if out, err := runCtx(c.ctx, cmd); err != nil {
		return "", fmt.Errorf("git clone %s: %w: %s", it.Repo, err, auth.redact(strings.TrimSpace(string(out))))
	}
	os.RemoveAll(dest) // a broken earlier attempt
	if err := os.Rename(tmp, dest); err != nil {
		return "", err
	}
	wb.rememberClone(it.Repo, dest)
	return dest, nil
}

// runCtx runs cmd and kills it if ctx ends first, with whatever it started:
// git fetch and clone run git-remote-http, which would otherwise keep running
// and hold the output pipe open, so Wait would not return.
func runCtx(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	var out strings.Builder
	err := runCtxTo(ctx, cmd, &out)
	return []byte(out.String()), err
}

// runCtxTo is runCtx with the command's output going to w as it is written.
func runCtxTo(ctx context.Context, cmd *exec.Cmd, w io.Writer) error {
	cmd.Stdout, cmd.Stderr = w, w
	setProcessGroup(cmd)
	// exec only runs Cancel for a CommandContext command: it is called here.
	kill := cmd.Cancel
	cmd.Cancel = nil
	cmd.WaitDelay = 2 * time.Second // where no group can be killed (Windows)
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		if kill != nil {
			kill()
		} else {
			cmd.Process.Kill()
		}
		<-done
		return ctx.Err()
	}
}

func tildePath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if p == home {
			return "~"
		}
		if strings.HasPrefix(p, home+string(filepath.Separator)) {
			return "~" + p[len(home):]
		}
	}
	return p
}
