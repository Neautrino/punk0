package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cleanRepo builds a repository with one commit and nothing uncommitted, the
// state a project is normally added in (git_test.go's gitRepo is dirty).
func cleanRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	runGit(t, dir, "init", "-q", "-b", "main")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func openRecords(t *testing.T, home string) *Records {
	t.Helper()
	rec, err := OpenRecords(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rec.Close)
	return rec
}

// addWorkspace adds a local or worktree workspace named name, the rest left
// as the New workspace panel defaults it.
func addWorkspace(rec *Records, projectID, name string, worktree bool) (*Workspace, error) {
	w, _, err := rec.AddWorkspace(projectID, WorkspaceOptions{Name: name, Worktree: worktree})
	return w, err
}

func TestProjectAddAndReload(t *testing.T) {
	repo, home := cleanRepo(t), t.TempDir()
	rec := openRecords(t, home)

	p, err := rec.AddProject(filepath.Join(repo, "sub"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Dir != repo {
		t.Fatalf("project dir %q, want the repository top level %q", p.Dir, repo)
	}
	if again, _ := rec.AddProject(repo); again != p {
		t.Fatal("adding the same repository twice made a second project")
	}
	if len(p.Workspaces) != 1 || p.Workspaces[0].Kind != kindLocal || p.Workspaces[0].Name != "main" || len(p.Workspaces[0].Tabs) != 0 {
		t.Fatalf("new project should hold one local workspace on main, with no tabs: %+v", p.Workspaces)
	}
	local, err := addWorkspace(rec, p.ID, "sidebar design", false)
	if err != nil {
		t.Fatal(err)
	}
	if local.Dir != repo {
		t.Fatalf("local workspace runs in %q, want the project folder", local.Dir)
	}
	claude, err := rec.AddTab(local.ID, tabClaude)
	if err != nil {
		t.Fatal(err)
	}

	// A restarted punk0 lists the same tree, with every shell stopped.
	reopened := openRecords(t, home)
	if views := reopened.List(); len(views) != 2 {
		t.Fatalf("reloaded tree, want the project and Scratch: %+v", views)
	}
	pv := listed(reopened, p.ID)
	if len(pv.Workspaces) != 2 {
		t.Fatalf("reloaded project: %+v", pv)
	}
	tabs := pv.Workspaces[1].Tabs
	if len(tabs) != 1 || tabs[0].ID != claude.ID || tabs[0].Running || tabs[0].Status != "" {
		t.Fatalf("reloaded tabs: %+v", tabs)
	}
	if tabs[0].tab.Session != claude.Session {
		t.Fatalf("reloaded claude tab lost its session: %q", tabs[0].tab.Session)
	}
	if tabs[0].tab.dir != repo {
		t.Fatalf("reloaded tab would start in %q", tabs[0].tab.dir)
	}

	if _, err := rec.RemoveWorkspace(local.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, "README.md")); err != nil {
		t.Fatalf("removing a local workspace touched the project folder: %v", err)
	}
}

func TestProjectRejectsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if _, err := openRecords(t, t.TempDir()).AddProject(home); err == nil {
		t.Fatal("registered the home folder as a project")
	}
}

// Local workspaces share the project folder, so they share the one child that
// serves its files; a worktree workspace has a folder, and a child, of its own.
func TestWorkspacesShareOneBackendPerFolder(t *testing.T) {
	repo := cleanRepo(t)
	rec := openRecords(t, t.TempDir())
	p, err := rec.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	second, err := addWorkspace(rec, p.ID, "review", false)
	if err != nil {
		t.Fatal(err)
	}
	id := wbItemID("dir", repo)
	if wbItemID("dir", second.Dir) != id {
		t.Fatal("two local workspaces of one project got different backends")
	}
	if rec.Folder(id) != repo {
		t.Fatalf("backend %s resolves to %q, want %q", id, rec.Folder(id), repo)
	}
	wt, err := addWorkspace(rec, p.ID, "feat-x", true)
	if err != nil {
		t.Fatal(err)
	}
	if wbItemID("dir", wt.Dir) == id {
		t.Fatal("a worktree workspace shares the project's backend")
	}
	if rec.Folder(wbItemID("dir", wt.Dir)) != wt.Dir {
		t.Fatalf("worktree backend not found: %q", wt.Dir)
	}
	if rec.Folder("000000000000") != "" {
		t.Fatal("unknown backend id resolved to a folder")
	}
}

// Open is what a second `punk0 <dir>` hands over: the same answer every time,
// and a folder under <state>/worktrees opens its worktree workspace.
func TestRecordsOpenIsIdempotent(t *testing.T) {
	repo := cleanRepo(t)
	rec := openRecords(t, t.TempDir())
	p, ws, err := rec.Open(filepath.Join(repo, "sub"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Dir != repo || ws.Kind != kindLocal {
		t.Fatalf("opened %+v %+v", p, ws)
	}
	again, sameWS, err := rec.Open(repo)
	if err != nil || again.ID != p.ID || sameWS.ID != ws.ID {
		t.Fatalf("second open made %+v %+v (%v)", again, sameWS, err)
	}
	if n := len(rec.List()); n != 2 {
		t.Fatalf("%d projects after opening the same folder twice", n)
	}

	wt, err := addWorkspace(rec, p.ID, "feat-open", true)
	if err != nil {
		t.Fatal(err)
	}
	wtProject, wtWS, err := rec.Open(wt.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if wtProject.ID != p.ID || wtWS.ID != wt.ID {
		t.Fatalf("worktree folder opened %+v %+v, want the worktree workspace", wtProject, wtWS)
	}

	// The project was removed, but its worktree still points at the
	// repository, so opening the folder finds its way back.
	if err := rec.RemoveProject(p.ID); err != nil {
		t.Fatal(err)
	}
	_, found, err := rec.Open(wt.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if found.Kind != kindWorktree || found.Dir != wt.Dir {
		t.Fatalf("reopened worktree as %+v", found)
	}
}

func TestRecordsLSPIsOffByDefault(t *testing.T) {
	repo, home := cleanRepo(t), t.TempDir()
	rec := openRecords(t, home)
	if _, err := rec.AddProject(repo); err != nil {
		t.Fatal(err)
	}
	if rec.LSPOn(repo) {
		t.Fatal("language servers on for a new project")
	}
	if err := rec.SetLSP(repo, true); err != nil {
		t.Fatal(err)
	}
	if !openRecords(t, home).LSPOn(repo) {
		t.Fatal("the choice did not survive a restart")
	}
	if err := rec.SetLSP(repo, false); err != nil {
		t.Fatal(err)
	}
	if openRecords(t, home).LSPOn(repo) {
		t.Fatal("turning language servers off did not stick")
	}
}

func TestWorktreeWorkspace(t *testing.T) {
	repo, home := cleanRepo(t), t.TempDir()
	rec := openRecords(t, home)
	p, err := rec.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}

	w, err := addWorkspace(rec, p.ID, "feat-a", true)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(rec.worktrees, p.ID, "feat-a"); w.Dir != want {
		t.Fatalf("worktree at %q, want %q", w.Dir, want)
	}
	if runGit(t, w.Dir, "rev-parse", "--abbrev-ref", "HEAD") != "feat-a" {
		t.Fatal("worktree not on branch feat-a")
	}
	if st := runGit(t, repo, "status", "--porcelain"); st != "" {
		t.Fatalf("worktree shows up in the project's status:\n%s", st)
	}

	os.WriteFile(filepath.Join(w.Dir, "wip.txt"), []byte("agent output"), 0o644)
	if _, err := rec.RemoveWorkspace(w.ID, false); !errors.Is(err, errDirty) {
		t.Fatalf("removing a dirty worktree: %v, want errDirty", err)
	}
	if _, err := os.Stat(filepath.Join(w.Dir, "wip.txt")); err != nil || rec.Workspace(w.ID) == nil {
		t.Fatal("refused removal still deleted the worktree or its record")
	}
	if _, err := rec.RemoveWorkspace(w.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(w.Dir); !os.IsNotExist(err) {
		t.Fatalf("worktree still on disk: %v", err)
	}
	runGit(t, repo, "rev-parse", "--verify", "refs/heads/feat-a") // branch survives
}

func TestWorktreeFoundAfterReadd(t *testing.T) {
	repo := cleanRepo(t)
	rec := openRecords(t, t.TempDir())
	p, _ := rec.AddProject(repo)
	w, err := addWorkspace(rec, p.ID, "feat-b", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.RemoveProject(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(w.Dir); err != nil {
		t.Fatalf("removing the project deleted its worktree: %v", err)
	}

	p, _ = rec.AddProject(repo)
	if len(p.Workspaces) != 2 || p.Workspaces[1].Dir != w.Dir || p.Workspaces[1].Kind != kindWorktree || p.Workspaces[1].Branch != "feat-b" {
		t.Fatalf("worktree not found again: %+v", p.Workspaces)
	}
}

func TestWorktreeDeletedOutsidePunk0(t *testing.T) {
	repo := cleanRepo(t)
	rec := openRecords(t, t.TempDir())
	p, _ := rec.AddProject(repo)
	w, err := addWorkspace(rec, p.ID, "feat-c", true)
	if err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(w.Dir)

	wv := listed(rec, p.ID).Workspaces[1]
	if !wv.Missing || wv.Branch != "feat-c" {
		t.Fatalf("deleted worktree listed as %+v, want missing on feat-c", wv)
	}
	if _, err := rec.RemoveWorkspace(w.ID, false); err != nil {
		t.Fatal(err)
	}
	if list := runGit(t, repo, "worktree", "list"); strings.Contains(list, "feat-c") {
		t.Fatalf("git still lists the deleted worktree:\n%s", list)
	}
}

func TestAddWorkspaceRejects(t *testing.T) {
	repo := cleanRepo(t)
	rec := openRecords(t, t.TempDir())
	p, _ := rec.AddProject(repo)
	runGit(t, repo, "branch", "used")
	runGit(t, repo, "worktree", "add", "-q", filepath.Join(t.TempDir(), "elsewhere"), "used")

	for _, o := range []WorkspaceOptions{
		{Name: "", Worktree: true},                       // no name
		{Name: "main"},                                   // duplicate of the first workspace
		{Name: "main", Worktree: true},                   // branch checked out in the project folder
		{Name: "ok", Worktree: true, Base: "-x"},         // option-like base
		{Name: "ok", Worktree: true, Branch: "bad..ref"}, // invalid branch
		{Name: "ok", Worktree: true, Branch: "-x"},       // option-like branch
		{Name: "used", Worktree: true},                   // branch checked out in another worktree
		{Name: "ok", Worktree: true, Base: "nowhere"},    // unknown base
		{Name: "ok", Worktree: true, Agent: "nonesuch"},  // unknown agent
	} {
		if _, _, err := rec.AddWorkspace(p.ID, o); err == nil {
			t.Errorf("AddWorkspace(%+v) succeeded", o)
		}
	}
	if n := len(listed(rec, p.ID).Workspaces); n != 1 {
		t.Fatalf("got %d workspaces after rejected adds, want 1", n)
	}

	plain, err := rec.AddProject(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := addWorkspace(rec, plain.ID, "x", true); err == nil {
		t.Fatal("made a worktree in a folder that is not a git repository")
	}
}

func TestScratchChats(t *testing.T) {
	home := t.TempDir()
	rec := openRecords(t, home)
	views := rec.List()
	if len(views) != 1 || !views[0].Scratch {
		t.Fatalf("empty records should list only Scratch: %+v", views)
	}
	scratch := views[0]
	if _, err := addWorkspace(rec, scratch.ID, "research", false); err == nil {
		t.Fatal("added a named workspace to Scratch")
	}

	// A folder left by an older chat of the same minute is never reused.
	stale := filepath.Join(home, "scratch", "chat-"+time.Now().Format("0102-1504"))
	os.Mkdir(stale, 0o700)
	os.WriteFile(filepath.Join(stale, "notes.md"), []byte("old"), 0o644)
	w, err := rec.NewChat(ChatOptions{Agent: tabClaude})
	if err != nil {
		t.Fatal(err)
	}
	if w.Kind != kindFolder || filepath.Dir(w.Dir) != filepath.Join(home, "scratch") || w.Dir == stale || !isDir(w.Dir) {
		t.Fatalf("chat %+v, want a new folder of its own under <state>/scratch", w)
	}
	if len(w.Tabs) != 1 || w.Tabs[0].Kind != tabClaude || w.Tabs[0].Session == "" || w.Active == 0 {
		t.Fatalf("chat should open on a claude tab, active now: %+v", w)
	}
	scratchDir := filepath.Join(home, "scratch")
	for _, o := range []ChatOptions{
		{Agent: tabClaude, Group: "no-such-group"},
		{Agent: "vim"},
		{Agent: tabClaude, Title: "\x07"},
		{Agent: tabClaude, Dir: stale},                                  // another chat's folder, deleted with it
		{Agent: tabClaude, Dir: filepath.Join(scratchDir, "a", "b")},    // not directly in scratch
		{Agent: tabClaude, Dir: filepath.Join(scratchDir, "has space")}, // not a folder name punk0 makes
		{Agent: tabClaude, Dir: scratchDir},
	} {
		if _, err := rec.NewChat(o); err == nil {
			t.Errorf("NewChat(%+v) succeeded", o)
		}
	}

	// A title and a scratch folder named in the dialog.
	named, err := rec.NewChat(ChatOptions{Agent: tabShell, Title: " Auth research ", Dir: filepath.Join(scratchDir, "auth-research")})
	if err != nil {
		t.Fatal(err)
	}
	if named.Title != "Auth research" || named.Name != "auth-research" || !isDir(named.Dir) || named.Tabs[0].Kind != tabShell {
		t.Fatalf("named chat: %+v", named)
	}

	// A folder of mine, made when missing.
	mine := filepath.Join(t.TempDir(), "photos")
	ext, err := rec.NewChat(ChatOptions{Agent: "codex", Dir: mine})
	if err != nil {
		t.Fatal(err)
	}
	if ext.Dir != mine || !isDir(mine) || ext.Title != "" || ext.Tabs[0].Kind != "codex" || ext.Tabs[0].Name != "Codex" {
		t.Fatalf("chat in my folder: %+v %+v", ext, ext.Tabs[0])
	}

	// The first real prompt names the chat; later ones and commands do not.
	tab := w.Tabs[0].ID
	rec.ChatPrompt(tab, "/model opus")
	rec.ChatPrompt(tab, "compare zod and valibot\nfor form validation")
	rec.ChatPrompt(tab, "now benchmark them")
	if got := rec.Workspace(w.ID).Title; got != "compare zod and valibot" {
		t.Fatalf("title %q", got)
	}
	title := "  Validation libs "
	if err := rec.UpdateChat(w.ID, ChatUpdate{Title: &title}); err != nil || rec.Workspace(w.ID).Title != "Validation libs" {
		t.Fatalf("rename: %v %q", err, rec.Workspace(w.ID).Title)
	}
	blank := " "
	if err := rec.UpdateChat(w.ID, ChatUpdate{Title: &blank}); err == nil {
		t.Fatal("renamed to nothing")
	}

	g, err := rec.AddChatGroup("Research")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rec.AddChatGroup("research"); err == nil {
		t.Fatal("two groups named alike")
	}
	if err := rec.UpdateChat(w.ID, ChatUpdate{Group: &g.ID}); err != nil || rec.Workspace(w.ID).Group != g.ID {
		t.Fatalf("move to group: %v", err)
	}
	grouped, err := rec.NewChat(ChatOptions{Agent: tabShell, Group: g.ID})
	if err != nil || grouped.Group != g.ID {
		t.Fatalf("new chat in group: %v %+v", err, grouped)
	}
	if err := rec.RemoveChatGroup(g.ID); err != nil {
		t.Fatal(err)
	}
	if rec.Workspace(w.ID).Group != "" || rec.Workspace(grouped.ID).Group != "" {
		t.Fatal("removing a group left chats filed under it")
	}

	yes := true
	if err := rec.UpdateChat(w.ID, ChatUpdate{Archived: &yes}); err != nil {
		t.Fatal(err)
	}
	reopened := openRecords(t, home)
	if got := reopened.Workspace(w.ID); got == nil || !got.Archived || got.Title != "Validation libs" {
		t.Fatalf("archived chat after reload: %+v", got)
	}

	// Deleting a chat deletes the folder punk0 made for it, never another,
	// and never a folder of yours.
	os.WriteFile(filepath.Join(w.Dir, "notes.md"), []byte("findings"), 0o644)
	for _, id := range []string{w.ID, ext.ID, named.ID} {
		if _, err := rec.RemoveWorkspace(id, false); err != nil {
			t.Fatal(err)
		}
	}
	if isDir(w.Dir) || isDir(named.Dir) || !isDir(stale) || !isDir(grouped.Dir) || !isDir(mine) {
		t.Fatal("deleting a chat should remove its own folder and only that")
	}
	if err := rec.RemoveProject(scratch.ID); err == nil {
		t.Fatal("removed Scratch")
	}

	// Saved records hold Scratch; reopening must not add a second one.
	n := 0
	for _, p := range openRecords(t, home).List() {
		if p.Scratch {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("reopened records list %d Scratch projects, want 1", n)
	}
}

func TestChatTitle(t *testing.T) {
	long := strings.Repeat("word ", 30)
	cases := map[string]string{
		"  fix the flaky test  ":  "fix the flaky test",
		"first line\nsecond line": "first line",
		"tabs\tand   spaces":      "tabs and spaces",
		"/clear":                  "",
		long:                      strings.TrimSpace(strings.Repeat("word ", 12)) + "…",
		strings.Repeat("x", 70):   strings.Repeat("x", 60) + "…",
		"ünïcödé " + strings.Repeat("é", 70): "ünïcödé…",
	}
	for in, want := range cases {
		if got := chatTitle(in); got != want {
			t.Errorf("chatTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAgentCommand(t *testing.T) {
	claude := claudeCommand("claude")
	cases := []struct {
		kind  string
		first bool
		want  string
	}{
		{tabShell, true, ""},
		{tabClaude, true, claude + " --session-id S"},
		{tabClaude, false, claude + " --resume S || " + claude + " --session-id S"},
		{"codex", true, "codex"}, // a new tab never picks up an older session in the folder
		{"codex", false, "codex resume --last || codex"},
		{"opencode", false, "opencode --continue || opencode"},
		{"cursor-agent", false, "cursor-agent"},
	}
	for _, c := range cases {
		if got := agentCommand(c.kind, "S", c.first); got != c.want {
			t.Errorf("agentCommand(%s, %v)\n got %s\nwant %s", c.kind, c.first, got, c.want)
		}
	}
}

func listed(rec *Records, id string) projectView {
	for _, p := range rec.List() {
		if p.ID == id {
			return p
		}
	}
	return projectView{}
}

// useSettings points the settings file at a temp folder holding m.
func useSettings(t *testing.T, m map[string]any) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(dir, "punk0"), 0o755)
	if err := os.WriteFile(filepath.Join(dir, "punk0", "settings.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAgentCommandArgs(t *testing.T) {
	useSettings(t, map[string]any{"agents.claude.args": "--model opus", "agents.codex.args": " -m o3 "})
	claude := claudeCommand("claude --model opus")
	if !strings.HasPrefix(claude, "claude --model opus --settings ") {
		t.Fatalf("claude command %q", claude)
	}
	for _, c := range []struct {
		kind  string
		first bool
		want  string
	}{
		{tabClaude, true, claude + " --session-id S"},
		{tabClaude, false, claude + " --resume S || " + claude + " --session-id S"},
		{"codex", true, "codex -m o3"},
		{"codex", false, "codex -m o3 resume --last || codex -m o3"},
		{"opencode", false, "opencode --continue || opencode"}, // no args set
	} {
		if got := agentCommand(c.kind, "S", c.first); got != c.want {
			t.Errorf("agentCommand(%s, %v)\n got %s\nwant %s", c.kind, c.first, got, c.want)
		}
	}
}

func TestTabProcessShellAndEnv(t *testing.T) {
	t.Setenv("SHELL", "/bin/fromenv")
	useSettings(t, map[string]any{})
	if sh, args, _ := tabProcess("", nil); sh != "/bin/fromenv" || args != nil {
		t.Fatalf("default shell %q %v, want $SHELL and no args", sh, args)
	}
	home, _ := os.UserHomeDir()
	useSettings(t, map[string]any{
		"terminal.shell": "/bin/zsh",
		"terminal.env":   "# work account\nCLAUDE_CONFIG_DIR=~/.claude-work\nPUNK0_HOOK_URL=mine\n",
	})
	sh, args, env := tabProcess("claude", []string{"PUNK0_HOOK_URL=punk0"})
	if sh != "/bin/zsh" || len(args) != 3 || args[2] != `claude; exec "$SHELL"` {
		t.Fatalf("shell %q args %v", sh, args)
	}
	last := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		last[k] = v
	}
	if last["SHELL"] != "/bin/zsh" || last["CLAUDE_CONFIG_DIR"] != filepath.Join(home, ".claude-work") {
		t.Fatalf("env SHELL=%q CLAUDE_CONFIG_DIR=%q", last["SHELL"], last["CLAUDE_CONFIG_DIR"])
	}
	if last["PUNK0_HOOK_URL"] != "punk0" {
		t.Fatalf("PUNK0_HOOK_URL=%q: the user's terminal.env beat punk0's own", last["PUNK0_HOOK_URL"])
	}
	useSettings(t, map[string]any{"terminal.env": "not a pair"})
	if _, _, env := tabProcess("", nil); len(env) != len(os.Environ())+3 {
		t.Fatal("a terminal.env that does not parse still changed the environment")
	}
}

// deleteMergedBranch drops a merged branch with its worktree, and keeps an
// unmerged one with a note instead of failing the delete.
func TestRemoveWorkspaceDeletesMergedBranch(t *testing.T) {
	useSettings(t, map[string]any{"workspace.deleteMergedBranch": true})
	repo := cleanRepo(t)
	rec := openRecords(t, t.TempDir())
	p, _ := rec.AddProject(repo)
	merged, err := addWorkspace(rec, p.ID, "merged", true)
	if err != nil {
		t.Fatal(err)
	}
	if note, err := rec.RemoveWorkspace(merged.ID, false); err != nil || note != "" {
		t.Fatalf("remove merged: note %q, err %v", note, err)
	}
	if exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", "refs/heads/merged").Run() == nil {
		t.Fatal("merged branch kept")
	}
	open, err := addWorkspace(rec, p.ID, "open", true)
	if err != nil {
		t.Fatal(err)
	}
	commit(t, open.Dir, "work.txt")
	os.WriteFile(filepath.Join(open.Dir, "dirty.txt"), []byte("x"), 0o644)
	note, err := rec.RemoveWorkspace(open.ID, true)
	if err != nil || !strings.Contains(note, "open") {
		t.Fatalf("remove unmerged: note %q, err %v", note, err)
	}
	if rec.Workspace(open.ID) != nil {
		t.Fatal("workspace kept after its branch could not be deleted")
	}
	runGit(t, repo, "rev-parse", "--verify", "refs/heads/open")
}
