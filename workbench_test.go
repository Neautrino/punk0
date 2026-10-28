package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *wbStore {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	return &wbStore{path: workbenchStorePath()}
}

// testWorkbench builds a workbench on the temporary state directory
// newTestStore set up, and stops whatever it started when the test ends.
func testWorkbench(t *testing.T, basePath, cwd, exe string, childArgs []string) *workbench {
	t.Helper()
	isolateCodeRoot(t)
	wb, err := newWorkbench(basePath, cwd, exe, childArgs)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(wb.Close)
	return wb
}

// TestStateFollowsLegacyInstall: an install that still has ~/.punk0/records.json
// keeps using it; once it is gone, the standard state folder wins.
func TestStateFollowsLegacyInstall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix layout")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "")
	legacy := filepath.Join(home, ".punk0")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "records.json"), []byte(`{"projects":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := workbenchStateDir(); got != legacy {
		t.Fatalf("workbenchStateDir = %s, want the legacy %s", got, legacy)
	}
	if err := os.RemoveAll(legacy); err != nil {
		t.Fatal(err)
	}
	if got, want := workbenchStateDir(), filepath.Join(home, ".local", "state", "punk0"); got != want {
		t.Fatalf("workbenchStateDir = %s, want %s", got, want)
	}
}

func TestWorkbenchItemIDsAreStable(t *testing.T) {
	a := wbDirItem("/work/repo/")
	b := wbDirItem("/work/repo")
	if a.ID != b.ID || len(a.ID) != 12 {
		t.Fatalf("dir ids differ or wrong length: %q %q", a.ID, b.ID)
	}
	p1 := wbPRItem(PRTarget{Provider: "github", Owner: "Px0-AI", Repo: "px0", Number: 7, URL: "https://github.com/Px0-AI/px0/pull/7"})
	p2 := wbPRItem(PRTarget{Provider: "github", Owner: "px0-ai", Repo: "PX0", Number: 7, URL: "https://github.com/px0-ai/px0/pull/7/files"})
	if p1.ID != p2.ID {
		t.Fatalf("same PR got different ids: %q %q", p1.ID, p2.ID)
	}
	if p1.ID == wbDirItem("px0-ai/px0#7").ID {
		t.Fatal("dir and PR ids must not collide")
	}
}

func TestWorkbenchStoreRecordKeepsTitle(t *testing.T) {
	st := newTestStore(t)
	it := wbPRItem(PRTarget{Provider: "github", Owner: "o", Repo: "r", Number: 1, URL: "u"})
	it.Title = "Fix things"
	if err := st.Record(it); err != nil {
		t.Fatal(err)
	}
	it.Title = "" // reopened before the title is known again
	if err := st.Record(it); err != nil {
		t.Fatal(err)
	}
	got, ok := st.Get(it.ID)
	if !ok || got.Title != "Fix things" || got.LastOpened == 0 {
		t.Fatalf("got %+v", got)
	}
	if n := len(st.Load().Items); n != 1 {
		t.Fatalf("want 1 item, got %d", n)
	}
}

func TestWorkbenchStoreTrimsOldest(t *testing.T) {
	st := newTestStore(t)
	err := st.Update(func(f *wbFile) {
		for i := range wbMaxRecent + 10 {
			it := wbPRItem(PRTarget{Provider: "github", Owner: "o", Repo: "r", Number: i, URL: "u"})
			it.LastOpened = int64(i)
			f.Items = append(f.Items, it)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	items := st.Load().Items
	if len(items) != wbMaxRecent {
		t.Fatalf("want %d items, got %d", wbMaxRecent, len(items))
	}
	if items[0].LastOpened != wbMaxRecent+9 {
		t.Fatalf("not most recent first: %d", items[0].LastOpened)
	}
	if items[len(items)-1].LastOpened != 10 {
		t.Fatalf("oldest kept: %d", items[len(items)-1].LastOpened)
	}
}

func TestWorkbenchStoreSetsAsideCorruptFile(t *testing.T) {
	st := newTestStore(t)
	os.MkdirAll(filepath.Dir(st.path), 0o700)
	os.WriteFile(st.path, []byte("{not json"), 0o600)
	if n := len(st.Load().Items); n != 0 {
		t.Fatalf("want empty, got %d", n)
	}
	if _, err := os.Stat(st.path + ".bad"); err != nil {
		t.Fatalf("corrupt file not kept: %v", err)
	}
}

func TestRemoteMatchesRepo(t *testing.T) {
	cases := []struct {
		origin string
		want   bool
	}{
		{"git@github.com:px0-ai/px0.git\n", true},
		{"https://github.com/PX0-AI/px0", true},
		{"ssh://git@github.com/px0-ai/px0.git", true},
		{"https://github.com/px0-ai/px0-docs.git", false},
		{"https://github.com/other/px0.git", false},
	}
	for _, c := range cases {
		if got := remoteMatchesRepo(c.origin, "px0-ai", "px0"); got != c.want {
			t.Errorf("%q: got %v", c.origin, got)
		}
	}
}

func TestWorkbenchBaseOf(t *testing.T) {
	cases := map[string]string{
		"/w/abc/":          "/",
		"/rev-1/w/abc/":    "/rev-1/",
		"/a/w/b/w/abc123/": "/a/w/b/",
		"/":                "/",
	}
	for in, want := range cases {
		if got := workbenchBaseOf(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}

func TestExpandPath(t *testing.T) {
	home, _ := os.UserHomeDir()
	cases := map[string]string{
		"":       "/base",
		"sub/":   "/base/sub",
		"/abs/x": "/abs/x",
		"~":      home,
		"~/code": filepath.Join(home, "code"),
	}
	for in, want := range cases {
		got, err := expandPath(in, "/base")
		if err != nil || got != want {
			t.Errorf("%q: got %q (%v), want %q", in, got, err, want)
		}
	}
}

func TestLineWriterSplitsLinesAndSpinnerFrames(t *testing.T) {
	var got []string
	lw := &lineWriter{fn: func(s string) { got = append(got, s) }}
	io.WriteString(lw, "one\r\ntw")
	io.WriteString(lw, "o\n⠋ Cloning\r⠙ Cloning\rpx0-ready 127.0.0.1:1\nlast")
	lw.Flush()
	want := []string{"one", "two", "px0-ready 127.0.0.1:1", "last"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
}

func TestWorkbenchResolveTarget(t *testing.T) {
	newTestStore(t)
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	wb := &workbench{cwd: dir}

	it, folder, err := wb.resolveTarget("sub")
	if err != nil || it.Kind != "" || filepath.Base(folder) != "sub" {
		t.Fatalf("relative dir: %+v %q %v", it, folder, err)
	}
	it, folder, err = wb.resolveTarget("https://github.com/px0-ai/px0/pull/12")
	if err != nil || it.Kind != "pr" || it.Repo != "px0-ai/px0" || it.Number != 12 || folder != "" {
		t.Fatalf("pr: %+v %q %v", it, folder, err)
	}
	for _, bad := range []string{"", "https://github.com/px0-ai/px0", filepath.Join(dir, "missing")} {
		if _, _, err := wb.resolveTarget(bad); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
}

func TestWorkbenchOpenRequiresLocalPost(t *testing.T) {
	newTestStore(t)
	wb := testWorkbench(t, "/", t.TempDir(), "", nil)
	r := httptest.NewRequest("POST", "/api/wb/open", strings.NewReader(`{"target":"."}`))
	r.Host = "127.0.0.1:7777"
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	wb.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("got %d", w.Code)
	}
	if n := len(wb.store.Load().Items); n != 0 {
		t.Fatalf("recorded %d items", n)
	}
}

func TestWorkbenchUnknownWorkspace(t *testing.T) {
	newTestStore(t)
	wb := testWorkbench(t, "/", t.TempDir(), "", nil)
	r := httptest.NewRequest("GET", "/w/000000000000/", nil)
	r.Host = "127.0.0.1:7777"
	w := httptest.NewRecorder()
	wb.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d", w.Code)
	}
	r = httptest.NewRequest("GET", "/w/000000000000/api/meta", nil)
	w = httptest.NewRecorder()
	wb.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("api on unknown workspace: got %d", w.Code)
	}
}

// End to end: a real px0 binary opened as a workspace, reached through the
// workbench's proxy, then stopped through its stdin.
func TestWorkbenchRunsChildThroughProxy(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the px0 binary")
	}
	newTestStore(t)
	exe := filepath.Join(t.TempDir(), "px0")
	if out, err := exec.Command("go", "build", "-o", exe, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	ws := t.TempDir()
	os.WriteFile(filepath.Join(ws, "hello.go"), []byte("package hello\n"), 0o644)

	wb := testWorkbench(t, "/base/", ws, exe, []string{"-no-lsp", "-no-agent", "-no-telemetry"})
	srv := httptest.NewServer(wb)
	defer srv.Close()
	defer wb.Close()

	post := func(path, body string) map[string]any {
		r, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(body))
		r.Header.Set("Origin", srv.URL)
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var m map[string]any
		json.NewDecoder(resp.Body).Decode(&m)
		return m
	}
	opened := post("/base/api/wb/open", `{"target":"`+ws+`"}`)
	href, _ := opened["href"].(string)
	if opened["project"] == nil || opened["workspace"] == nil || !strings.HasPrefix(href, "w/") {
		t.Fatalf("open: %v", opened)
	}
	id := strings.TrimSuffix(strings.TrimPrefix(href, "w/"), "/")
	if wb.childState(id) != "stopped" {
		t.Fatalf("open started a child before anything asked for the folder")
	}

	// An API request starts the child and waits for it, rather than failing.
	resp, err := http.Get(srv.URL + "/base/w/" + id + "/api/meta")
	if err != nil {
		t.Fatal(err)
	}
	var meta struct {
		Root, Workbench, BasePath string
	}
	json.NewDecoder(resp.Body).Decode(&meta)
	resp.Body.Close()
	if meta.Workbench != "/base/" || meta.BasePath != "/base/w/"+id+"/" {
		t.Fatalf("meta: %+v", meta)
	}
	if resolved, _ := filepath.EvalSymlinks(ws); meta.Root != resolved {
		t.Fatalf("root %q, want %q", meta.Root, resolved)
	}
	// The running child is the workspace's editor, measured as its own
	// process tree; with the parent's share it makes up the total.
	resp, err = http.Get(srv.URL + "/base/api/resources")
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		RSS        uint64 `json:"rss"`
		Self       struct{ RSS uint64 }
		Workspaces []struct {
			Editor *struct{ RSS uint64 }
		}
	}
	json.NewDecoder(resp.Body).Decode(&res)
	resp.Body.Close()
	if len(res.Workspaces) != 1 || res.Workspaces[0].Editor == nil || res.Workspaces[0].Editor.RSS == 0 ||
		res.RSS != res.Self.RSS+res.Workspaces[0].Editor.RSS {
		t.Fatalf("resources: %+v", res)
	}

	if m := post("/base/api/wb/stop", `{"id":"`+id+`"}`); m["ok"] != true {
		t.Fatalf("stop: %v", m)
	}
	wb.mu.Lock()
	state := wb.children[id].state
	wb.mu.Unlock()
	if state != "stopped" {
		t.Fatalf("after stop: %s", state)
	}
}

// A target that fails on its first open leaves the list; the holding page
// still explains why, without a Retry that could not work.
func TestWorkbenchForgetsNewItemThatFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the child")
	}
	newTestStore(t)
	dir := t.TempDir()
	exe := filepath.Join(dir, "fake-punk0")
	os.WriteFile(exe, []byte("#!/bin/sh\necho 'punk0: punk0: github: fetch PR #9: 404 Not Found' >&2\nexit 1\n"), 0o755)
	wb := testWorkbench(t, "/", dir, exe, nil)
	defer wb.Close()

	it := wbPRItem(PRTarget{Provider: "github", Owner: "o", Repo: "r", Number: 9, URL: "https://github.com/o/r/pull/9"})
	wb.store.Record(it)
	wb.rememberClone(it.Repo, gitRepoWithRemotes(t, map[string]string{"origin": "https://github.com/o/r.git"}))
	c := wb.start(it, true)
	select {
	case <-c.done:
	case <-time.After(10 * time.Second):
		t.Fatal("child never exited")
	}
	if c.state != "failed" || c.err != "github: fetch PR #9: 404 Not Found" {
		t.Fatalf("state %q err %q", c.state, c.err)
	}
	if _, ok := wb.store.Get(it.ID); ok {
		t.Fatal("failed new item still listed")
	}
	r := httptest.NewRequest("GET", "/w/"+it.ID+"/", nil)
	r.Host = "127.0.0.1:7777"
	w := httptest.NewRecorder()
	wb.ServeHTTP(w, r)
	body := w.Body.String()
	if !strings.Contains(body, "404 Not Found") || strings.Contains(body, "retry=1") {
		t.Fatalf("holding page:\n%s", body)
	}
}

// The holding page that starts a stopped workspace refreshes to the same
// file and line, so `punk0 <file>:<n>` against a cold workspace still lands
// there; only ?retry is dropped.
func TestWorkbenchHoldingPageKeepsFileAndLine(t *testing.T) {
	newTestStore(t)
	repo := cleanRepo(t)
	wb := testWorkbench(t, "/", t.TempDir(), "", nil)
	defer wb.Close()
	var opened map[string]any
	json.Unmarshal(reqJSON(t, wb, "POST", "/api/projects", `{"dir":"`+repo+`"}`).Body.Bytes(), &opened)
	href, _ := opened["href"].(string)
	r := httptest.NewRequest("GET", "/"+href+"?line=3&path=go.mod&retry=1", nil)
	r.Host = "127.0.0.1:7777"
	w := httptest.NewRecorder()
	wb.ServeHTTP(w, r)
	body := w.Body.String()
	if !strings.Contains(body, "line=3&amp;path=go.mod") || strings.Contains(body, "retry=1") {
		t.Fatalf("holding page refresh lost the query:\n%s", body)
	}
}

// gitRepoWithRemotes makes an empty git repository with the given remotes.
func gitRepoWithRemotes(t *testing.T, remotes map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	for name, u := range remotes {
		if out, err := exec.Command("git", "-C", dir, "remote", "add", name, u).CombinedOutput(); err != nil {
			t.Fatalf("git remote add: %v %s", err, out)
		}
	}
	resolved, _ := filepath.EvalSymlinks(dir)
	return resolved
}

func TestFindPRRemotePrefersOriginThenUpstream(t *testing.T) {
	fork := gitRepoWithRemotes(t, map[string]string{
		"origin":   "git@github.com:me/px0.git",
		"upstream": "https://github.com/px0-ai/px0.git",
	})
	if top, remote := findPRRemote(fork, "px0-ai", "px0"); top != fork || remote != "upstream" {
		t.Fatalf("fork: %q %q", top, remote)
	}
	both := gitRepoWithRemotes(t, map[string]string{
		"origin":   "https://github.com/px0-ai/px0",
		"upstream": "https://github.com/px0-ai/px0.git",
	})
	if _, remote := findPRRemote(both, "px0-ai", "px0"); remote != "origin" {
		t.Fatalf("both: %q", remote)
	}
	other := gitRepoWithRemotes(t, map[string]string{"origin": "https://github.com/px0-ai/px0-docs.git"})
	if top, _ := findPRRemote(other, "px0-ai", "px0"); top != "" {
		t.Fatalf("other repo matched: %q", top)
	}
}

func TestScanForCloneFindsNestedClone(t *testing.T) {
	root := t.TempDir()
	clone := filepath.Join(root, "work", "px0")
	os.MkdirAll(filepath.Join(root, "work", "node_modules", "px0"), 0o755)
	if out, err := exec.Command("git", "init", "-q", clone).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	exec.Command("git", "-C", clone, "remote", "add", "origin", "git@github.com:px0-ai/px0.git").Run()
	want, _ := filepath.EvalSymlinks(clone)
	if got := scanForClone(context.Background(), []string{root}, "px0-ai/px0"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := scanForClone(context.Background(), []string{root}, "px0-ai/other"); got != "" {
		t.Fatalf("found a clone of the wrong repo: %q", got)
	}
}

func postJSON(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return reqJSON(t, h, "POST", path, body)
}

// Opening a PR with no known clone asks for one and records nothing; giving
// a folder that is not a clone is refused; a real clone is remembered.
func TestWorkbenchOpenPRAsksForClone(t *testing.T) {
	newTestStore(t)
	t.Setenv("HOME", t.TempDir()) // keep the scan away from real code folders
	wb := testWorkbench(t, "/", t.TempDir(), "/nonexistent/px0", nil)
	defer wb.Close()
	pr := "https://github.com/acme/widget/pull/5"

	w := postJSON(t, wb, "/api/wb/open", `{"target":"`+pr+`"}`)
	var res map[string]any
	json.Unmarshal(w.Body.Bytes(), &res)
	if res["needClone"] != true || res["repo"] != "acme/widget" {
		t.Fatalf("got %s", w.Body.String())
	}
	if n := len(wb.store.Load().Items); n != 0 {
		t.Fatalf("recorded %d items before a clone was chosen", n)
	}

	notClone := t.TempDir()
	if w := postJSON(t, wb, "/api/wb/open", `{"target":"`+pr+`","clone":"`+notClone+`"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("non-clone accepted: %d %s", w.Code, w.Body.String())
	}

	clone := gitRepoWithRemotes(t, map[string]string{"origin": "git@github.com:acme/widget.git"})
	if w := postJSON(t, wb, "/api/wb/open", `{"target":"`+pr+`","clone":"`+clone+`"}`); w.Code != http.StatusOK {
		t.Fatalf("clone refused: %d %s", w.Code, w.Body.String())
	}
	if got := wb.store.Load().Clones["acme/widget"]; got != clone {
		t.Fatalf("remembered %q, want %q", got, clone)
	}
	// Next time it is found without asking.
	w = postJSON(t, wb, "/api/wb/open", `{"target":"https://github.com/acme/widget/pull/6"}`)
	if strings.Contains(w.Body.String(), "needClone") {
		t.Fatalf("asked again: %s", w.Body.String())
	}
}

func TestWorkbenchCompleteFolders(t *testing.T) {
	newTestStore(t)
	dir := t.TempDir()
	for _, d := range []string{"Projects", "proto", "public", ".private", "other"} {
		os.Mkdir(filepath.Join(dir, d), 0o755)
	}
	os.WriteFile(filepath.Join(dir, "profile.txt"), nil, 0o644)
	os.MkdirAll(filepath.Join(dir, "proto", ".git"), 0o755)
	wb := testWorkbench(t, "/", dir, "", nil)

	complete := func(q string) []wbEntry {
		r := httptest.NewRequest("GET", "/api/wb/complete?q="+url.QueryEscape(q), nil)
		r.Host = "127.0.0.1:7777"
		w := httptest.NewRecorder()
		wb.ServeHTTP(w, r)
		var res struct{ Entries []wbEntry }
		json.Unmarshal(w.Body.Bytes(), &res)
		return res.Entries
	}
	names := func(es []wbEntry) string {
		var n []string
		for _, e := range es {
			n = append(n, e.Path)
		}
		return strings.Join(n, ",")
	}
	if got := names(complete("pr")); got != "proto/,Projects/" {
		t.Fatalf("relative prefix: %s", got)
	}
	if got := names(complete(dir + "/P")); got != dir+"/Projects/,"+dir+"/proto/,"+dir+"/public/" {
		t.Fatalf("absolute, case-insensitive: %s", got)
	}
	if got := names(complete(dir + "/.p")); got != dir+"/.private/" {
		t.Fatalf("hidden only when typed: %s", got)
	}
	if es := complete("proto"); len(es) != 1 || !es[0].Git {
		t.Fatalf("git flag: %+v", es)
	}
	if got := complete("https://github.com/x"); len(got) != 0 {
		t.Fatalf("URL completed: %v", got)
	}
	// It lists folders anywhere on disk: a DNS-rebound page must not read it.
	r := httptest.NewRequest("GET", "/api/wb/complete?q="+url.QueryEscape(dir+"/"), nil)
	r.Host = "evil.example:7777"
	w := httptest.NewRecorder()
	wb.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("rebound host: got %d", w.Code)
	}
}

// reqJSON sends a write the way punk0's own page does.
func reqJSON(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = "127.0.0.1:7777"
	r.Header.Set("Origin", "http://127.0.0.1:7777")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func getJSON(t *testing.T, h http.Handler, path string, v any) {
	t.Helper()
	r := httptest.NewRequest("GET", path, nil)
	r.Host = "127.0.0.1:7777"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("GET %s: %v\n%s", path, err, w.Body.String())
	}
}

type testProject struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Dir        string `json:"dir"`
	Git        bool   `json:"git"`
	Branch     string `json:"branch"`
	Scratch    bool   `json:"scratch"`
	Workspaces []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Kind    string `json:"kind"`
		Dir     string `json:"dir"`
		Href    string `json:"href"`
		Backend string `json:"backend"`
		LSP     bool   `json:"lsp"`
		Missing bool   `json:"missing"`
		Tabs    []struct {
			ID      string `json:"id"`
			Kind    string `json:"kind"`
			Name    string `json:"name"`
			Running bool   `json:"running"`
			Status  string `json:"status"`
		} `json:"tabs"`
	} `json:"workspaces"`
}

// Registering a folder is the same request however often it is made, and the
// workspaces of one folder share the child that serves it.
func TestWorkbenchProjectRoutes(t *testing.T) {
	newTestStore(t)
	repo := cleanRepo(t)
	wb := testWorkbench(t, "/", t.TempDir(), "", nil)

	var opened, again map[string]any
	w := reqJSON(t, wb, "POST", "/api/projects", `{"dir":"`+repo+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("add project: %d %s", w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &opened)
	json.Unmarshal(reqJSON(t, wb, "POST", "/api/projects", `{"dir":"`+filepath.Join(repo, "sub")+`"}`).Body.Bytes(), &again)
	if opened["href"] != wb.childHref(wbItemID("dir", repo)) {
		t.Fatalf("href %v", opened["href"])
	}
	if again["project"] != opened["project"] || again["workspace"] != opened["workspace"] {
		t.Fatalf("a folder inside the project made %v, want %v", again, opened)
	}

	var projects []testProject
	getJSON(t, wb, "/api/projects", &projects)
	if len(projects) != 2 {
		t.Fatalf("want the project and Scratch: %+v", projects)
	}
	p, scratch := projects[0], projects[1]
	if p.Scratch {
		p, scratch = projects[1], projects[0]
	}
	if p.ID != opened["project"] || !p.Git || p.Branch != "main" || len(p.Workspaces) != 1 {
		t.Fatalf("project view: %+v", p)
	}
	ws := p.Workspaces[0]
	if ws.Kind != kindLocal || ws.Backend != "stopped" || ws.LSP || ws.Href != opened["href"] || len(ws.Tabs) != 0 {
		t.Fatalf("workspace view: %+v", ws)
	}
	if !scratch.Scratch {
		t.Fatalf("scratch missing: %+v", scratch)
	}

	// A second local workspace runs in the same folder, so it is served by
	// the same child.
	var second struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
		Href string `json:"href"`
		Tabs []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"tabs"`
	}
	w = reqJSON(t, wb, "POST", "/api/projects/"+p.ID+"/workspaces", `{"name":"review"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("add workspace: %d %s", w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &second)
	// No tabs encodes as [], not null: the page reads w.tabs as an array.
	if second.ID == "" || second.Kind != kindLocal || second.Href != ws.Href || !strings.Contains(w.Body.String(), `"tabs":[]`) {
		t.Fatalf("second workspace: %s", w.Body.String())
	}

	var tab struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
		Name string `json:"name"`
	}
	w = reqJSON(t, wb, "POST", "/api/workspaces/"+second.ID+"/tabs", `{"kind":"claude"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("add tab: %d %s", w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &tab)
	if tab.ID == "" || tab.Kind != tabClaude {
		t.Fatalf("tab: %+v", tab)
	}
	if w := reqJSON(t, wb, "PATCH", "/api/tabs/"+tab.ID, `{"name":"review"}`); w.Code != http.StatusOK {
		t.Fatalf("rename tab: %d %s", w.Code, w.Body.String())
	}
	if w := reqJSON(t, wb, "POST", "/api/workspaces/"+second.ID+"/lsp", `{"on":true}`); w.Code != http.StatusOK {
		t.Fatalf("lsp on: %d %s", w.Code, w.Body.String())
	}
	if !wb.records.LSPOn(repo) {
		t.Fatal("language servers still off after the toggle")
	}
	if w := reqJSON(t, wb, "DELETE", "/api/tabs/"+tab.ID, ""); w.Code != http.StatusOK {
		t.Fatalf("remove tab: %d %s", w.Code, w.Body.String())
	}
	if w := reqJSON(t, wb, "DELETE", "/api/workspaces/"+second.ID, ""); w.Code != http.StatusOK {
		t.Fatalf("remove workspace: %d %s", w.Code, w.Body.String())
	}
	if w := reqJSON(t, wb, "DELETE", "/api/projects/"+p.ID, ""); w.Code != http.StatusOK {
		t.Fatalf("remove project: %d %s", w.Code, w.Body.String())
	}
	getJSON(t, wb, "/api/projects", &projects)
	if len(projects) != 1 || !projects[0].Scratch {
		t.Fatalf("after removal: %+v", projects)
	}
	if w := reqJSON(t, wb, "DELETE", "/api/projects/nope", ""); w.Code != http.StatusNotFound {
		t.Fatalf("unknown project: %d", w.Code)
	}
}

// A folder punk0 made a worktree in opens that worktree's workspace, not a
// project of its own.
func TestWorkbenchHandsOverWorktreeFolder(t *testing.T) {
	newTestStore(t)
	repo := cleanRepo(t)
	wb := testWorkbench(t, "/", t.TempDir(), "", nil)
	p, _, err := wb.records.Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := addWorkspace(wb.records, p.ID, "feat-handoff", true)
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]any
	w := reqJSON(t, wb, "POST", "/api/projects", `{"dir":"`+wt.Dir+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("hand over worktree: %d %s", w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if out["project"] != p.ID || out["workspace"] != wt.ID || out["href"] != wb.childHref(wbItemID("dir", wt.Dir)) {
		t.Fatalf("worktree folder opened %v", out)
	}
}

// A worktree with uncommitted changes is kept until the page asks again with
// force, so an agent's unsaved work is never thrown away by one click — and
// nothing on disk is touched at all unless the dialog asked for files.
func TestWorkbenchRefusesDirtyWorktreeDelete(t *testing.T) {
	newTestStore(t)
	repo := cleanRepo(t)
	wb := testWorkbench(t, "/", t.TempDir(), "", nil)
	p, _, err := wb.records.Open(repo)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := addWorkspace(wb.records, p.ID, "feat-dirty", true)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(wt.Dir, "wip.txt"), []byte("agent output"), 0o644)

	w := reqJSON(t, wb, "DELETE", "/api/workspaces/"+wt.ID+"?files=1", "")
	if w.Code != http.StatusConflict {
		t.Fatalf("dirty delete: %d %s", w.Code, w.Body.String())
	}
	var res map[string]any
	decode(t, w.Body, &res)
	if res["dirty"] != true || res["error"] == "" {
		t.Fatalf("409 body: %s", w.Body.String())
	}
	if wb.records.Workspace(wt.ID) == nil {
		t.Fatal("refused delete still forgot the workspace")
	}
	// Without files the same dirty worktree is only forgotten.
	if w := reqJSON(t, wb, "DELETE", "/api/workspaces/"+wt.ID, ""); w.Code != http.StatusOK {
		t.Fatalf("forget: %d %s", w.Code, w.Body.String())
	}
	if wb.records.Workspace(wt.ID) != nil || !isDir(wt.Dir) {
		t.Fatalf("forget kept the record or deleted %s", wt.Dir)
	}
}

// The project dialog's answers travel as query parameters: the folder goes
// only with files=1 and the name in confirm, and only for a project punk0
// made.
func TestWorkbenchRemovesProjectFolder(t *testing.T) {
	newTestStore(t)
	repo := cleanRepo(t)
	wb := testWorkbench(t, "/", t.TempDir(), "", nil)
	parent := t.TempDir()
	var made struct{ Project, Workspace string }
	decode(t, postJSON(t, wb, "/api/projects/new", `{"name":"fresh","parent":"`+parent+`"}`).Body, &made)
	p, _, err := wb.records.Open(repo)
	if err != nil {
		t.Fatal(err)
	}

	if w := reqJSON(t, wb, "DELETE", "/api/projects/"+p.ID+"?files=1&confirm="+url.QueryEscape(p.Name), ""); w.Code != http.StatusBadRequest {
		t.Fatalf("delete a folder punk0 did not make: %d %s", w.Code, w.Body.String())
	}
	if !isDir(repo) {
		t.Fatalf("%s was deleted anyway", repo)
	}
	if w := reqJSON(t, wb, "DELETE", "/api/projects/"+made.Project+"?files=1&confirm=wrong", ""); w.Code != http.StatusBadRequest {
		t.Fatalf("delete without the name: %d %s", w.Code, w.Body.String())
	}
	if w := reqJSON(t, wb, "DELETE", "/api/projects/"+made.Project+"?files=1&confirm=fresh", ""); w.Code != http.StatusOK {
		t.Fatalf("delete the folder: %d %s", w.Code, w.Body.String())
	}
	if isDir(filepath.Join(parent, "fresh")) {
		t.Fatal("the project folder is still there")
	}
}

// Terminals are shells on this machine: they are served only while punk0
// listens on loopback.
func TestWorkbenchTerminalsNeedLoopback(t *testing.T) {
	newTestStore(t)
	wb := testWorkbench(t, "/", t.TempDir(), "", nil)
	r := httptest.NewRequest("GET", "/api/tabs/whatever/term", nil)
	r.Host = "box.tailnet.ts.net:7777"
	w := httptest.NewRecorder()
	wb.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("remote terminal: %d %s", w.Code, w.Body.String())
	}
}

func TestHookStatus(t *testing.T) {
	cases := []struct {
		body   string
		want   string
		known  bool
		reason string
	}{
		{`{"hook_event_name":"SessionStart"}`, statusIdle, true, "a new session waits for a prompt"},
		{`{"hook_event_name":"UserPromptSubmit"}`, statusWorking, true, ""},
		{`{"hook_event_name":"PreToolUse"}`, statusWorking, true, ""},
		{`{"hook_event_name":"PostToolUse"}`, statusWorking, true, ""},
		{`{"hook_event_name":"PostToolUseFailure"}`, statusWorking, true, ""},
		{`{"hook_event_name":"PermissionRequest"}`, statusWaiting, true, ""},
		{`{"hook_event_name":"Notification","notification_type":"permission_prompt"}`, statusWaiting, true, ""},
		{`{"hook_event_name":"Notification","notification_type":"elicitation_dialog"}`, statusWaiting, true, ""},
		{`{"hook_event_name":"Notification","notification_type":"elicitation_url_dialog"}`, statusWaiting, true, ""},
		{`{"hook_event_name":"Notification","notification_type":"idle_prompt"}`, statusIdle, true, ""},
		{`{"hook_event_name":"Notification","notification_type":"something_new"}`, "", false, "an unknown notification says nothing"},
		{`{"hook_event_name":"Stop"}`, statusIdle, true, ""},
		{`{"hook_event_name":"StopFailure"}`, statusIdle, true, ""},
		{`{"hook_event_name":"Stop","background_tasks":[]}`, statusIdle, true, ""},
		{`{"hook_event_name":"Stop","background_tasks":[{"id":"1"}]}`, statusWorking, true, "a background task keeps running"},
		{`{"hook_event_name":"SessionEnd"}`, "", true, "the session is gone"},
		{`{"hook_event_name":"Whatever"}`, "", false, ""},
	}
	for _, c := range cases {
		var ev hookEvent
		json.Unmarshal([]byte(c.body), &ev)
		got, ok := hookStatus(ev)
		if got != c.want || ok != c.known {
			t.Errorf("%s: got %q %v, want %q %v %s", c.body, got, ok, c.want, c.known, c.reason)
		}
	}
}

func TestStopIdleTab(t *testing.T) {
	const limit = 30 * time.Minute
	cases := []struct {
		name       string
		kind       string
		status     string
		foreground bool
		idleFor    time.Duration
		limit      time.Duration
		want       bool
	}{
		{"shell at its prompt", tabShell, "", true, time.Hour, limit, true},
		{"shell with a job running", tabShell, "", false, time.Hour, limit, false},
		{"shell used recently", tabShell, "", true, time.Minute, limit, false},
		{"shell with the rule off", tabShell, "", true, time.Hour, 0, false},
		{"idle agent", tabClaude, statusIdle, false, time.Hour, limit, true},
		{"working agent", tabClaude, statusWorking, true, time.Hour, limit, false},
		{"agent waiting for an answer", tabClaude, statusWaiting, true, time.Hour, limit, false},
		{"claude that reported nothing", tabClaude, "", false, time.Hour, limit, true},
		{"quiet agent without status", "codex", "", false, time.Hour, limit, true},
		{"agent without status used recently", "codex", "", false, time.Minute, limit, false},
	}
	for _, c := range cases {
		if got := stopIdleTab(c.kind, c.status, c.foreground, c.idleFor, c.limit); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestIdleLimit(t *testing.T) {
	five := 5.0
	zero := 0.0
	if got := idleLimit(nil, wbWorkspaceIdleStop); got != 5*time.Minute {
		t.Errorf("default: %v", got)
	}
	if got := idleLimit(&five, wbTerminalIdleStop); got != 5*time.Minute {
		t.Errorf("set: %v", got)
	}
	if got := idleLimit(&zero, wbTerminalIdleStop); got != 0 {
		t.Errorf("0 should mean never: %v", got)
	}
}

// Pinning went with the recent-items list; removing a recorded pull request
// from the workbench is all that is left of it.
func TestWorkbenchRemovesRecordedItem(t *testing.T) {
	st := newTestStore(t)
	wb := testWorkbench(t, "/", t.TempDir(), "", nil)
	wb.store = st

	it := wbDirItem("/work/test-repo")
	if err := st.Record(it); err != nil {
		t.Fatal(err)
	}

	w := postJSON(t, wb, "/api/wb/remove", `{"id":"`+it.ID+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("remove failed: %d %s", w.Code, w.Body.String())
	}
	if _, ok := st.Get(it.ID); ok {
		t.Fatal("item was not removed")
	}
}

func TestWorkbenchLockLifecycle(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	// No lock initially
	if url := runningWorkbench(); url != "" {
		t.Errorf("expected empty runningWorkbench, got %q", url)
	}

	// Write lock with a mock server that answers ping
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/wb/ping" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"workbench":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	writeWorkbenchLock(ts.URL)

	// Verify runningWorkbench finds it
	if u := runningWorkbench(); u != ts.URL {
		t.Errorf("runningWorkbench = %q, want %q", u, ts.URL)
	}

	// Remove lock
	removeWorkbenchLock()
	if u := runningWorkbench(); u != "" {
		t.Errorf("runningWorkbench after remove = %q, want empty", u)
	}
}

func TestWorkspaceTabsList(t *testing.T) {
	newTestStore(t)
	repo := cleanRepo(t)
	wb := testWorkbench(t, "/", t.TempDir(), "", nil)

	var opened map[string]any
	w := reqJSON(t, wb, "POST", "/api/projects", `{"dir":"`+repo+`"}`)
	json.Unmarshal(w.Body.Bytes(), &opened)
	wsID, _ := opened["workspace"].(string)

	// Add a tab to the workspace
	reqJSON(t, wb, "POST", "/api/workspaces/"+wsID+"/tabs", `{"kind":"shell"}`)

	// Get tabs
	var res struct {
		Tabs []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"tabs"`
	}
	getJSON(t, wb, "/api/workspaces/"+wsID+"/tabs", &res)
	if len(res.Tabs) != 1 || res.Tabs[0].Kind != "shell" {
		t.Fatalf("expected 1 shell tab, got %+v", res.Tabs)
	}

	// In editor mode, the client queries using wbItemID("dir", dir):
	dirID := wbItemID("dir", repo)
	var resDir struct {
		Tabs []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"tabs"`
	}
	getJSON(t, wb, "/api/workspaces/"+dirID+"/tabs", &resDir)
	if len(resDir.Tabs) != 1 || resDir.Tabs[0].Kind != "shell" {
		t.Fatalf("expected 1 shell tab via dirID, got %+v", resDir.Tabs)
	}

	// Add an agent tab via dirID
	wClaude := reqJSON(t, wb, "POST", "/api/workspaces/"+dirID+"/tabs", `{"kind":"claude"}`)
	if wClaude.Code != http.StatusOK {
		t.Fatalf("add claude tab via dirID: %d %s", wClaude.Code, wClaude.Body.String())
	}

	var claudeTab struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
	}
	json.Unmarshal(wClaude.Body.Bytes(), &claudeTab)

	// Add another agent tab (codex) via dirID
	wCodex := reqJSON(t, wb, "POST", "/api/workspaces/"+dirID+"/tabs", `{"kind":"codex"}`)
	if wCodex.Code != http.StatusOK {
		t.Fatalf("add codex tab via dirID: %d %s", wCodex.Code, wCodex.Body.String())
	}

	// Verify all 3 tabs are listed
	getJSON(t, wb, "/api/workspaces/"+dirID+"/tabs", &resDir)
	if len(resDir.Tabs) != 3 {
		t.Fatalf("expected 3 tabs, got %d", len(resDir.Tabs))
	}

	// Remove claude tab
	wDel := reqJSON(t, wb, "DELETE", "/api/tabs/"+claudeTab.ID, "")
	if wDel.Code != http.StatusOK {
		t.Fatalf("delete tab: %d %s", wDel.Code, wDel.Body.String())
	}

	// Verify remaining 2 tabs
	getJSON(t, wb, "/api/workspaces/"+dirID+"/tabs", &resDir)
	if len(resDir.Tabs) != 2 {
		t.Fatalf("expected 2 tabs after deletion, got %d", len(resDir.Tabs))
	}

	// Only the tab routes take a folder id: a DELETE by it must not remove
	// the folder's workspace.
	if w := reqJSON(t, wb, "DELETE", "/api/workspaces/"+dirID, ""); w.Code != http.StatusNotFound {
		t.Fatalf("DELETE by folder id = %d, want 404", w.Code)
	}
}

func TestTabAddWithSession(t *testing.T) {
	useSettings(t, map[string]any{})
	newTestStore(t)
	repo := cleanRepo(t)
	wb := testWorkbench(t, "/", t.TempDir(), "", nil)
	var opened map[string]any
	decode(t, reqJSON(t, wb, "POST", "/api/projects", `{"dir":"`+repo+`"}`).Body, &opened)
	wsID, _ := opened["workspace"].(string)

	w := reqJSON(t, wb, "POST", "/api/workspaces/"+wsID+"/tabs", `{"kind":"opencode","session":"ses_42"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("add tab with session: %d %s", w.Code, w.Body.String())
	}
	var tab struct{ ID string }
	decode(t, w.Body, &tab)
	ws := wb.records.WorkspaceAt(wsID)
	if got := wb.records.View(ws).Tabs[0].tab.command(posixShell); got != "opencode --session ses_42 || opencode" {
		t.Fatalf("tab %s starts with %q", tab.ID, got)
	}
	if w := reqJSON(t, wb, "POST", "/api/workspaces/"+wsID+"/tabs", `{"kind":"opencode","session":"$(id)"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("unsafe session: %d %s", w.Code, w.Body.String())
	}
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
