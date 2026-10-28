package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// claudeTab is a workbench with the repository registered and a claude tab on
// its local workspace, the place a worktree hook comes from. It returns the
// workbench, the project, its local workspace, the tab and the code root.
func claudeTab(t *testing.T) (wb *workbench, p *Project, ws *Workspace, tab *Satellite, root string) {
	t.Helper()
	wb, _, repo := historyWorkbench(t)
	p, err := wb.records.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	ws = p.Workspaces[0]
	if tab, err = wb.records.AddTab(ws.ID, tabClaude, ""); err != nil {
		t.Fatal(err)
	}
	if root, err = codeRoot(); err != nil {
		t.Fatal(err)
	}
	return wb, p, ws, tab, root
}

// hookPost sends one hook event to a tab, the way `punk0 hook` does.
func hookPost(t *testing.T, wb *workbench, tabID string, ev map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	return reqJSON(t, wb, "POST", "/api/tabs/"+tabID+"/hook", string(body))
}

// hookPath is the folder a WorktreeCreate answer names.
func hookPath(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("hook: %d %s", w.Code, w.Body.String())
	}
	var reply struct{ Path string }
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil {
		t.Fatalf("hook reply %s: %v", w.Body.String(), err)
	}
	if reply.Path == "" {
		t.Fatalf("hook answered without a path: %s", w.Body.String())
	}
	return reply.Path
}

// projectWorkspaces counts the rows a project has.
func projectWorkspaces(wb *workbench, projectID string) int {
	n := 0
	for _, x := range wb.records.WorkspaceRefs() {
		if x.Project == projectID {
			n++
		}
	}
	return n
}

func branchOf(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if err != nil {
		t.Fatalf("git branch in %s: %v", dir, err)
	}
	return strings.TrimSpace(string(out))
}

func branchExists(repo, branch string) bool {
	return exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch).Run() == nil
}

// A tab's WorktreeCreate makes the worktree in the Workspaces Folder, under
// the project, listed under the tab's workspace; asking again re-enters it.
func TestWorktreeCreateHookMakesNestedWorkspace(t *testing.T) {
	wb, p, ws, tab, root := claudeTab(t)
	dir := hookPath(t, hookPost(t, wb, tab.ID, map[string]any{
		"hook_event_name": worktreeCreateEvent, "name": "fix-login", "cwd": p.Dir,
	}))
	want := filepath.Join(root, "workspaces", p.ID, "fix-login")
	if dir != want {
		t.Fatalf("worktree at %s, want %s", dir, want)
	}
	if got := branchOf(t, dir); got != "worktree-fix-login" {
		t.Errorf("worktree is on %q, want worktree-fix-login", got)
	}
	made, ok := wb.records.WorkspaceRef(workspaceIn(wb.records.WorkspaceByDir(), dir))
	if !ok {
		t.Fatalf("no workspace for %s", dir)
	}
	if made.Parent != ws.ID || !made.Auto || made.Adopted || made.Kind != kindWorktree || made.Name != "fix-login" {
		t.Errorf("row %+v: want parent %s, auto, not adopted", made, ws.ID)
	}

	// --worktree <name> re-enters the one it made before.
	again := hookPath(t, hookPost(t, wb, tab.ID, map[string]any{
		"hook_event_name": worktreeCreateEvent, "name": "fix-login", "cwd": dir,
	}))
	if again != dir {
		t.Errorf("second WorktreeCreate answered %s, want %s", again, dir)
	}
	if n := projectWorkspaces(wb, p.ID); n != 2 {
		t.Errorf("%d workspaces, want the local one and the worktree", n)
	}
}

// WorktreeRemove keeps a worktree with changes in it, takes a clean one with
// its branch, and never touches a row that is not a worktree.
func TestWorktreeRemoveHookKeepsChanges(t *testing.T) {
	wb, p, _, tab, _ := claudeTab(t)
	dir := hookPath(t, hookPost(t, wb, tab.ID, map[string]any{
		"hook_event_name": worktreeCreateEvent, "name": "fix-login", "cwd": p.Dir,
	}))
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	remove := map[string]any{"hook_event_name": worktreeRemoveEvent, "worktree_path": dir, "cwd": dir}
	if w := hookPost(t, wb, tab.ID, remove); w.Code != http.StatusConflict {
		t.Fatalf("removing a worktree with changes: %d %s", w.Code, w.Body.String())
	}
	if !isDir(dir) {
		t.Fatal("a worktree with changes in it was removed")
	}

	// The project folder is a workspace too, and must never be removed by a
	// stray path.
	w := hookPost(t, wb, tab.ID, map[string]any{"hook_event_name": worktreeRemoveEvent, "worktree_path": p.Dir, "cwd": dir})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("removing the project folder: %d %s", w.Code, w.Body.String())
	}
	if !isDir(p.Dir) || projectWorkspaces(wb, p.ID) != 2 {
		t.Fatal("the project folder's workspace was touched")
	}

	if err := os.Remove(filepath.Join(dir, "new.txt")); err != nil {
		t.Fatal(err)
	}
	if w := hookPost(t, wb, tab.ID, remove); w.Code != http.StatusOK {
		t.Fatalf("removing a clean worktree: %d %s", w.Code, w.Body.String())
	}
	if isDir(dir) {
		t.Error("the folder is still there")
	}
	if n := projectWorkspaces(wb, p.ID); n != 1 {
		t.Errorf("%d workspaces after the removal, want the local one", n)
	}
	// Claude leaves the branch of a hook-made worktree behind; punk0 takes
	// an empty one with the folder.
	if branchExists(p.Dir, "worktree-fix-login") {
		t.Error("the empty branch is still there")
	}
}

// A scratch chat belongs to no repository, so there is nothing to make a
// worktree of.
func TestWorktreeCreateHookRefusesScratchChat(t *testing.T) {
	wb, _, _ := historyWorkbench(t)
	chat, err := wb.records.NewChat(ChatOptions{Agent: tabClaude, Title: "notes"})
	if err != nil {
		t.Fatal(err)
	}
	w := hookPost(t, wb, chat.Tabs[0].ID, map[string]any{
		"hook_event_name": worktreeCreateEvent, "name": "fix-login", "cwd": chat.Dir,
	})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "scratch chat") {
		t.Fatalf("scratch chat: %d %s", w.Code, w.Body.String())
	}
}

// A subagent run in its own worktree leaves nothing behind when it changed
// nothing; work it did keeps the folder, its row and its branch.
func TestSubagentWorktreeCleanedWhenEmpty(t *testing.T) {
	wb, p, _, tab, _ := claudeTab(t)
	stop := func(agentID, dir string) *httptest.ResponseRecorder {
		return hookPost(t, wb, tab.ID, map[string]any{
			"hook_event_name": subagentStopEvent, "agent_id": agentID, "cwd": dir,
		})
	}

	clean := hookPath(t, hookPost(t, wb, tab.ID, map[string]any{
		"hook_event_name": worktreeCreateEvent, "name": "agent-aaa", "cwd": p.Dir,
	}))
	if w := stop("aaa", clean); w.Code != http.StatusOK {
		t.Fatalf("SubagentStop: %d %s", w.Code, w.Body.String())
	}
	if isDir(clean) || branchExists(p.Dir, "worktree-agent-aaa") {
		t.Error("a subagent worktree with nothing in it was kept")
	}

	kept := hookPath(t, hookPost(t, wb, tab.ID, map[string]any{
		"hook_event_name": worktreeCreateEvent, "name": "agent-bbb", "cwd": p.Dir,
	}))
	if err := os.WriteFile(filepath.Join(kept, "work.txt"), []byte("done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, kept, "add", "work.txt")
	runGit(t, kept, "commit", "-q", "-m", "subagent work")
	if w := stop("bbb", kept); w.Code != http.StatusOK {
		t.Fatalf("SubagentStop: %d %s", w.Code, w.Body.String())
	}
	if !isDir(kept) || !branchExists(p.Dir, "worktree-agent-bbb") {
		t.Error("a subagent's commits were thrown away")
	}
	if n := projectWorkspaces(wb, p.ID); n != 2 {
		t.Errorf("%d workspaces, want the local one and the subagent's", n)
	}
}

/* ---------- the hook process ---------- */

// hookServer answers one worktree event with status and body.
func hookServer(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func runWorktreeHook(t *testing.T, ev hookEvent, target string) (code int, stdout, stderr string) {
	t.Helper()
	body, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	var out, errs bytes.Buffer
	code = worktreeHook(ev, body, target, &out, &errs)
	return code, out.String(), errs.String()
}

// Claude reads the last non-empty line of stdout as the worktree's folder.
func TestWorktreeHookPrintsServerPath(t *testing.T) {
	isolateCodeRoot(t)
	target := hookServer(t, http.StatusOK, `{"ok":true,"path":"/p"}`)
	code, stdout, stderr := runWorktreeHook(t, hookEvent{Name: worktreeCreateEvent, WorktreeName: "x"}, target)
	if code != 0 || stdout != "/p\n" || stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

// An older punk0, still running after an update, answers without a folder:
// failing is the only honest answer, since Claude would take any stray line.
func TestWorktreeHookRejectsEmptyReply(t *testing.T) {
	isolateCodeRoot(t)
	target := hookServer(t, http.StatusOK, `{"ok":true}`)
	code, stdout, stderr := runWorktreeHook(t, hookEvent{Name: worktreeCreateEvent, WorktreeName: "x"}, target)
	if code != 1 || stdout != "" || stderr == "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

// What punk0 refuses, Claude sees: the reason goes to stderr and nothing is
// made behind punk0's back.
func TestWorktreeHookPassesRefusal(t *testing.T) {
	isolateCodeRoot(t)
	t.Setenv("PATH", t.TempDir()) // no git: the refusal must not fall back
	target := hookServer(t, http.StatusConflict, `{"error":"has changes"}`)
	code, stdout, stderr := runWorktreeHook(t, hookEvent{Name: worktreeCreateEvent, WorktreeName: "x"}, target)
	if code != 1 || stdout != "" || !strings.Contains(stderr, "has changes") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

// With punk0 stopped the hook makes the worktree itself, in the folder punk0
// would have used, so `claude --worktree` keeps working.
func TestWorktreeHookFallsBackToGit(t *testing.T) {
	repo := cleanRepo(t)
	isolateCodeRoot(t)
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	records, err := json.Marshal(recordsFile{Projects: []*Project{{ID: "p", Name: "repo", Dir: repo}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(state, "punk0"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "punk0", "records.json"), records, 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := codeRoot()
	if err != nil {
		t.Fatal(err)
	}

	// Nothing listens on port 1: a transport error is what falls back.
	code, stdout, stderr := runWorktreeHook(t, hookEvent{Name: worktreeCreateEvent, WorktreeName: "x", Cwd: repo}, "http://127.0.0.1:1/")
	want := filepath.Join(root, "workspaces", "p", "x")
	if code != 0 || strings.TrimSpace(stdout) != want {
		t.Fatalf("exit %d, stdout %q, stderr %q, want %s", code, stdout, stderr, want)
	}
	if !isDir(want) {
		t.Fatalf("%s was not made", want)
	}
	if got := branchOf(t, want); got != "worktree-x" {
		t.Errorf("worktree is on %q, want worktree-x", got)
	}

	// And it takes it away again.
	code, _, stderr = runWorktreeHook(t, hookEvent{Name: worktreeRemoveEvent, WorktreePath: want, Cwd: want}, "http://127.0.0.1:1/")
	if code != 0 || isDir(want) {
		t.Fatalf("exit %d, stderr %q, folder still there: %v", code, stderr, isDir(want))
	}
}

// A removal punk0 has no row for is still a removal: git takes the folder
// away, so Claude's worktree list does not grow stale entries.
func TestWorktreeHookFallbackRemove(t *testing.T) {
	repo := cleanRepo(t)
	isolateCodeRoot(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := filepath.Join(t.TempDir(), "stray")
	runGit(t, repo, "worktree", "add", "-q", "-b", "worktree-stray", dir, "HEAD")
	target := hookServer(t, http.StatusNotFound, `{"error":"not found"}`)
	code, _, stderr := runWorktreeHook(t, hookEvent{Name: worktreeRemoveEvent, WorktreePath: dir, Cwd: dir}, target)
	if code != 0 || isDir(dir) {
		t.Fatalf("exit %d, stderr %q, folder still there: %v", code, stderr, isDir(dir))
	}
}

// OpenCode reads its inline config before its config files, so a punk0 tab's
// worktrees land in the Workspaces Folder -- unless the user chose a folder
// of their own.
func TestOpenCodeTabGetsWorktreeRoot(t *testing.T) {
	for _, c := range []struct{ name, user, want string }{
		{"none", "", `{"worktree":{"directory":"/root/workspaces/p"}}`},
		{"kept keys", `{"model":"x"}`, `{"model":"x","worktree":{"directory":"/root/workspaces/p"}}`},
		{"other worktree keys", `{"worktree":{"branch":"b"}}`, `{"worktree":{"branch":"b","directory":"/root/workspaces/p"}}`},
		{"user's own directory wins", `{"worktree":{"directory":"/mine"}}`, `{"worktree":{"directory":"/mine"}}`},
		{"invalid json is left alone", `{oops`, `{oops`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := opencodeConfig(c.user, "/root/workspaces/p"); got != c.want {
				t.Errorf("opencodeConfig(%q) = %s, want %s", c.user, got, c.want)
			}
		})
	}

	// A tab knows its project, so its environment names the project's
	// folder; any other tab is left alone.
	wb, _, repo := historyWorkbench(t)
	p, err := wb.records.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	tab, err := wb.records.AddTab(p.Workspaces[0].ID, tabOpenCode, "")
	if err != nil {
		t.Fatal(err)
	}
	root, err := codeRoot()
	if err != nil {
		t.Fatal(err)
	}
	want := "OPENCODE_CONFIG_CONTENT=" + opencodeConfig("", filepath.Join(root, "workspaces", p.ID))
	if got := opencodeEnv(tab.Kind, tab.project); got != want {
		t.Errorf("opencode tab env = %q, want %q", got, want)
	}
	if got := opencodeEnv(tabClaude, tab.project); got != "" {
		t.Errorf("claude tab env = %q", got)
	}
	chat, err := wb.records.NewChat(ChatOptions{Agent: tabOpenCode, Title: "notes"})
	if err != nil {
		t.Fatal(err)
	}
	if got := opencodeEnv(chat.Tabs[0].Kind, chat.Tabs[0].project); got != "" {
		t.Errorf("scratch chat env = %q: it belongs to no project", got)
	}
}
