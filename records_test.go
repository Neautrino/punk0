package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
	isolateCodeRoot(t)
	rec, err := OpenRecords(home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rec.Close)
	return rec
}

// isolateCodeRoot keeps a test off the real punk0: a Workspaces Folder in a
// temp dir and a HOME of its own, unless the test chose its own already.
func isolateCodeRoot(t *testing.T) {
	t.Helper()
	if !strings.HasPrefix(os.Getenv("XDG_CONFIG_HOME"), os.TempDir()) {
		useSettings(t, nil)
	}
	if home, _ := os.UserHomeDir(); !strings.HasPrefix(home, os.TempDir()) {
		dir := t.TempDir()
		t.Setenv("HOME", dir)
		t.Setenv("USERPROFILE", dir)
	}
}

// useCodeRoot points workspace.dir at a temp folder and returns it: where
// new worktrees and scratch chats land.
func useCodeRoot(t *testing.T) string {
	t.Helper()
	root := tempDir(t)
	useSettings(t, map[string]any{"workspace.dir": root})
	return root
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
	claude, err := rec.AddTab(local.ID, tabClaude, "")
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

	if _, err := rec.RemoveWorkspace(local.ID, RemoveOptions{}); err != nil {
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
	if _, err := rec.RemoveProject(p.ID, RemoveOptions{}); err != nil {
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
	root := useCodeRoot(t)
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
	if want := filepath.Join(root, "workspaces", p.ID, "feat-a"); w.Dir != want {
		t.Fatalf("worktree at %q, want %q", w.Dir, want)
	}
	if runGit(t, w.Dir, "rev-parse", "--abbrev-ref", "HEAD") != "feat-a" {
		t.Fatal("worktree not on branch feat-a")
	}
	if st := runGit(t, repo, "status", "--porcelain"); st != "" {
		t.Fatalf("worktree shows up in the project's status:\n%s", st)
	}

	os.WriteFile(filepath.Join(w.Dir, "wip.txt"), []byte("agent output"), 0o644)
	if _, err := rec.RemoveWorkspace(w.ID, RemoveOptions{Files: true}); !errors.Is(err, errDirty) {
		t.Fatalf("removing a dirty worktree: %v, want errDirty", err)
	}
	if _, err := os.Stat(filepath.Join(w.Dir, "wip.txt")); err != nil || rec.Workspace(w.ID) == nil {
		t.Fatal("refused removal still deleted the worktree or its record")
	}
	if _, err := rec.RemoveWorkspace(w.ID, RemoveOptions{Files: true, Force: true}); err != nil {
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
	// Owned travels with the record: the folder punk0 made is still punk0's
	// to delete once the project is added back.
	if err := rec.SetOwned(p.ID); err != nil {
		t.Fatal(err)
	}
	w, err := addWorkspace(rec, p.ID, "feat-b", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rec.RemoveProject(p.ID, RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(w.Dir); err != nil {
		t.Fatalf("removing the project deleted its worktree: %v", err)
	}

	p, _ = rec.AddProject(repo)
	if len(p.Workspaces) != 2 || p.Workspaces[1].Dir != w.Dir || p.Workspaces[1].Kind != kindWorktree || p.Workspaces[1].Branch != "feat-b" {
		t.Fatalf("worktree not found again: %+v", p.Workspaces)
	}
	if !p.Owned {
		t.Fatal("the project came back unowned, so its folder can no longer be deleted")
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
	// A missing folder is pruned only when files are asked for.
	if _, err := rec.RemoveWorkspace(w.ID, RemoveOptions{Files: true}); err != nil {
		t.Fatal(err)
	}
	if list := runGit(t, repo, "worktree", "list"); strings.Contains(list, "feat-c") {
		t.Fatalf("git still lists the deleted worktree:\n%s", list)
	}
}

// Without files, deleting a worktree workspace only forgets it: the folder,
// its branch and the repository's worktree registration all stay, so adding
// the project again finds it.
func TestRemoveWorkspaceForgetsOnly(t *testing.T) {
	repo := cleanRepo(t)
	rec := openRecords(t, t.TempDir())
	p, _ := rec.AddProject(repo)
	w, err := addWorkspace(rec, p.ID, "feat-keep", true)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := rec.RemoveWorkspace(w.ID, RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	if rec.Workspace(w.ID) != nil {
		t.Fatal("forgetting the workspace kept its record")
	}
	if !isDir(w.Dir) {
		t.Fatalf("forgetting the workspace deleted %s", w.Dir)
	}
	runGit(t, repo, "rev-parse", "--verify", "refs/heads/feat-keep")
	if list := runGit(t, repo, "worktree", "list"); !strings.Contains(list, w.Dir) {
		t.Fatalf("git no longer lists the worktree:\n%s", list)
	}
	if _, err := rec.RemoveProject(p.ID, RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	back, err := rec.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Workspaces) != 2 || back.Workspaces[1].Dir != w.Dir {
		t.Fatalf("the worktree was not found again: %+v", back.Workspaces)
	}
}

// git's worktree list is the only proof that a folder is punk0's to delete,
// so a record pointing anywhere else — the project's own checkout, a folder
// of the user's, a registration git calls prunable — is refused rather than
// removed, and with its tabs left running.
func TestRemoveWorkspaceRefusesForeignFolder(t *testing.T) {
	repo := cleanRepo(t)
	rec := openRecords(t, t.TempDir())
	p, _ := rec.AddProject(repo)
	outside := t.TempDir()
	// Records a page could never make, which is the point: the guard, not
	// the caller, is what keeps git away from these folders.
	main := newWorkspace("bogus", kindWorktree, repo, "main")
	stray := newWorkspace("stray", kindWorktree, outside, "")
	p.Workspaces = append(p.Workspaces, main, stray)
	// A worktree whose .git file the user deleted: git lists it prunable and
	// would refuse to remove it.
	stale, err := addWorkspace(rec, p.ID, "feat-stale", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(stale.Dir, ".git")); err != nil {
		t.Fatal(err)
	}
	// A locked worktree: git worktree remove refuses it even with --force.
	locked, err := addWorkspace(rec, p.ID, "feat-locked", true)
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "worktree", "lock", locked.Dir)

	// The reason matters: each refusal comes from punk0's own look at git's
	// worktree list, before a tab is stopped, not from git failing later.
	for _, tc := range []struct {
		w    *Workspace
		want string
	}{
		{main, "own checkout"},
		{stray, "not a worktree"},
		{stale, "prunable"},
		{locked, "locked"},
	} {
		_, err := rec.RemoveWorkspace(tc.w.ID, RemoveOptions{Files: true, Force: true})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("removing %s: %v, want %q", tc.w.Dir, err, tc.want)
		}
		if rec.Workspace(tc.w.ID) == nil {
			t.Fatalf("the refused removal still forgot %s", tc.w.Dir)
		}
	}
	if _, err := os.Stat(filepath.Join(repo, "README.md")); err != nil {
		t.Fatalf("the project's own checkout was touched: %v", err)
	}
	if !isDir(outside) || !isDir(stale.Dir) || !isDir(locked.Dir) {
		t.Fatal("a refused removal deleted a folder")
	}
}

// Removing a workspace or a project is punk0's own bookkeeping: the agents'
// transcripts are theirs, stay on disk and keep being listed (Chats mode
// deletes one at a time).
func TestRemovalNeverTouchesTranscripts(t *testing.T) {
	repo, home := cleanRepo(t), t.TempDir()
	cfg, _ := chatHomes(t)
	rec := openRecords(t, home)
	rec.Chats().cli = noCLI{} // tests never run a real agent binary
	p, _ := rec.AddProject(repo)
	w, err := addWorkspace(rec, p.ID, "feat-chat", true)
	if err != nil {
		t.Fatal(err)
	}
	inWorktree := writeChat(t, cfg, "wt", "11111111-1111-4111-8111-111111111111",
		claudeUser(w.Dir, "11111111-1111-4111-8111-111111111111", "fix the login bug"))
	inProject := writeChat(t, cfg, "proj", "22222222-2222-4222-8222-222222222222",
		claudeUser(repo, "22222222-2222-4222-8222-222222222222", "rename the branch"))

	if _, err := rec.RemoveWorkspace(w.ID, RemoveOptions{Files: true, Force: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.RemoveProject(p.ID, RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{inWorktree, inProject} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s is gone: %v", path, err)
		}
	}
	if trashed, _ := filepath.Glob(filepath.Join(home, "trash", "*")); len(trashed) != 0 {
		t.Fatalf("trash holds %v, want nothing", trashed)
	}
	// Both chats are still listed, and the one of the removed worktree now
	// has no workspace: All scope files it under Other folders.
	chats := chatsByID(rec.Chats())
	if len(chats) != 2 {
		t.Fatalf("listed %+v, want both chats", chats)
	}
	if got := workspaceIn(rec.WorkspaceByDir(), w.Dir); got != "" {
		t.Fatalf("the removed worktree's folder still answers workspace %q", got)
	}
}

func TestMoveToTrashKeepsWhatItMoves(t *testing.T) {
	state, src := t.TempDir(), t.TempDir()
	path := filepath.Join(src, "notes.txt")
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest, err := moveToTrash(state, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the file is still where it was: %v", err)
	}
	if b, err := os.ReadFile(dest); err != nil || string(b) != "x\n" {
		t.Fatalf("trashed file %q: %q %v", dest, b, err)
	}
	if filepath.Dir(dest) != filepath.Join(state, "trash") {
		t.Fatalf("moved to %q, want <state>/trash", dest)
	}
	// Two removals of the same name must not overwrite each other, however
	// close together they are.
	if err := os.WriteFile(path, []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	again, err := moveToTrash(state, path)
	if err != nil || again == dest {
		t.Fatalf("second move: %q %v", again, err)
	}
	if b, err := os.ReadFile(dest); err != nil || string(b) != "x\n" {
		t.Fatalf("the first trashed file is now %q %v", b, err)
	}
}

// Deleting a project's folder is the one removal that cannot be undone, so
// it is refused for a folder punk0 did not make, without the name typed
// back, while the repository still has worktrees checked out, and while
// another registered folder sits inside it.
func TestRemoveProjectRefusesToDeleteTheFolder(t *testing.T) {
	repo := cleanRepo(t)
	rec := openRecords(t, t.TempDir())
	p, _ := rec.AddProject(repo)
	w, err := addWorkspace(rec, p.ID, "feat-out", true)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := rec.RemoveProject(p.ID, RemoveOptions{Files: true, Confirm: p.Name}); err == nil {
		t.Fatal("deleted a folder punk0 did not make")
	}
	if err := rec.SetOwned(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.RemoveProject(p.ID, RemoveOptions{Files: true, Confirm: "not-the-name"}); err == nil {
		t.Fatal("deleted the folder without its name typed back")
	}
	_, err = rec.RemoveProject(p.ID, RemoveOptions{Files: true, Confirm: p.Name})
	if err == nil || !strings.Contains(err.Error(), filepath.Base(w.Dir)) {
		t.Fatalf("remove while a worktree is out: %v, want the worktree named", err)
	}
	if rec.ProjectAt(repo) != p.ID || !isDir(repo) || !isDir(w.Dir) {
		t.Fatal("a refused removal still changed something")
	}

	// A project of its own inside the folder: the move would take it along.
	if _, err := rec.RemoveWorkspace(w.ID, RemoveOptions{Files: true}); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(repo, "sub")
	runGit(t, nested, "init", "-q", "-b", "main")
	if _, err := rec.AddProject(nested); err != nil {
		t.Fatal(err)
	}
	_, err = rec.RemoveProject(p.ID, RemoveOptions{Files: true, Confirm: p.Name})
	if err == nil || !strings.Contains(err.Error(), "sub") {
		t.Fatalf("remove around a nested project: %v, want it named", err)
	}
	if !isDir(nested) {
		t.Fatal("the nested project's folder is gone")
	}
}

// With its worktrees gone, an owned project's folder is moved to the trash
// rather than deleted, the chats that ran in it are left alone, and it is not
// offered under Recently removed: it cannot be added back.
func TestRemoveProjectTrashesOwnedFolder(t *testing.T) {
	repo, home := cleanRepo(t), t.TempDir()
	cfg, _ := chatHomes(t)
	rec := openRecords(t, home)
	rec.Chats().cli = noCLI{}
	p, _ := rec.AddProject(repo)
	if err := rec.SetOwned(p.ID); err != nil {
		t.Fatal(err)
	}
	chat := writeChat(t, cfg, "proj", "33333333-3333-4333-8333-333333333333",
		claudeUser(repo, "33333333-3333-4333-8333-333333333333", "write the docs"))

	note, err := rec.RemoveProject(p.ID, RemoveOptions{Files: true, Confirm: p.Name})
	if err != nil || note != "" {
		t.Fatalf("remove: note %q, err %v", note, err)
	}
	if isDir(repo) {
		t.Fatalf("%s is still there", repo)
	}
	if _, err := os.Stat(chat); err != nil {
		t.Fatalf("the project's chat went with its folder: %v", err)
	}
	if trashed, _ := filepath.Glob(filepath.Join(home, "trash", "*")); len(trashed) != 1 {
		t.Fatalf("trash holds %v, want the folder alone", trashed)
	}
	if removed := openRecords(t, home).Removed(); len(removed) != 0 {
		t.Fatalf("a trashed project is still offered: %+v", removed)
	}
}

func TestAddWorkspaceRejects(t *testing.T) {
	repo := cleanRepo(t)
	rec := openRecords(t, t.TempDir())
	p, _ := rec.AddProject(repo)
	runGit(t, repo, "branch", "gone")
	gone := filepath.Join(t.TempDir(), "gone")
	runGit(t, repo, "worktree", "add", "-q", gone, "gone")
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}

	for _, o := range []WorkspaceOptions{
		{Name: "", Worktree: true},                       // no name
		{Name: "main"},                                   // duplicate of the first workspace
		{Name: "ok", Worktree: true, From: "-x"},         // option-like start point
		{Name: "ok", Worktree: true, Branch: "bad..ref"}, // invalid branch
		{Name: "ok", Worktree: true, Branch: "-x"},       // option-like branch
		{From: "gone", Worktree: true},                   // branch held by a deleted worktree
		{From: t.TempDir(), Worktree: true},              // a folder that is no worktree
		{Name: "ok", Worktree: true, From: "nowhere"},    // unknown start point
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
	root := useCodeRoot(t)
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
	scratchDir := filepath.Join(root, "scratch")
	stale := filepath.Join(scratchDir, "chat-"+time.Now().Format("0102-1504"))
	os.MkdirAll(stale, 0o700)
	os.WriteFile(filepath.Join(stale, "notes.md"), []byte("old"), 0o644)
	w, err := rec.NewChat(ChatOptions{Agent: tabClaude})
	if err != nil {
		t.Fatal(err)
	}
	if w.Kind != kindFolder || filepath.Dir(w.Dir) != scratchDir || w.Dir == stale || !isDir(w.Dir) || !w.Owned {
		t.Fatalf("chat %+v, want a new folder of its own under the code root's scratch", w)
	}
	if len(w.Tabs) != 1 || w.Tabs[0].Kind != tabClaude || w.Tabs[0].Session == "" || w.Active == 0 {
		t.Fatalf("chat should open on a claude tab, active now: %+v", w)
	}

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
		if _, err := rec.RemoveWorkspace(id, RemoveOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if isDir(w.Dir) || isDir(named.Dir) || !isDir(stale) || !isDir(grouped.Dir) || !isDir(mine) {
		t.Fatal("deleting a chat should remove its own folder and only that")
	}
	if _, err := rec.RemoveProject(scratch.ID, RemoveOptions{}); err == nil {
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

// Chats punk0 made go with the chat, wherever the scratch folder has moved
// to since; a chat in a folder of the user's never does.
func TestScratchChatOwnership(t *testing.T) {
	root := useCodeRoot(t)
	home := tempDir(t)
	oldScratch := filepath.Join(home, "scratch")
	mine := filepath.Join(tempDir(t), "photos")
	for _, d := range []string{filepath.Join(oldScratch, "chat-0101-0101"), mine} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	fixture := `{"projects":[{"id":"scratch","name":"Scratch","dir":"` + oldScratch + `","scratch":true,"workspaces":[
		{"id":"old","name":"chat-0101-0101","kind":"folder","dir":"` + filepath.Join(oldScratch, "chat-0101-0101") + `","tabs":[]},
		{"id":"ext","name":"photos","kind":"folder","dir":"` + mine + `","tabs":[]}]}]}`
	if err := os.WriteFile(filepath.Join(home, "records.json"), []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	rec := openRecords(t, home)
	old, ext := rec.Workspace("old"), rec.Workspace("ext")
	if old == nil || !old.Owned {
		t.Fatalf("a chat in the old scratch folder is not punk0's: %+v", old)
	}
	if ext == nil || ext.Owned {
		t.Fatalf("a chat in my folder is punk0's: %+v", ext)
	}

	fresh, err := rec.NewChat(ChatOptions{Agent: tabClaude})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "scratch"); filepath.Dir(fresh.Dir) != want {
		t.Fatalf("new chat in %q, want one under %q", fresh.Dir, want)
	}

	if _, err := rec.RemoveWorkspace("old", RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	if isDir(filepath.Join(oldScratch, "chat-0101-0101")) {
		t.Error("the folder punk0 made for the chat survived it")
	}
	if _, err := rec.RemoveWorkspace("ext", RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	if !isDir(mine) {
		t.Error("deleting a chat deleted my folder")
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
	useSettings(t, map[string]any{})
	claude := claudeCommand("claude", posixShell)
	cases := []struct {
		kind, session string
		first         bool
		want          string
	}{
		{tabShell, "", true, ""},
		{tabClaude, "S", true, claude + " --session-id S"},
		{tabClaude, "S", false, claude + " --resume S || " + claude + " --session-id S"},
		// No id known: a new tab starts fresh, a restart takes the folder's latest.
		{"codex", "", true, "codex"},
		{"codex", "", false, "codex resume --last || codex"},
		{"opencode", "", false, "opencode --continue || opencode"},
		{"cursor-agent", "", false, "cursor-agent"},
		// An id is known: resume exactly that chat, first start or not.
		{"codex", "S", true, "codex resume S || codex"},
		{"codex", "S", false, "codex resume S || codex"},
		{"opencode", "S", false, "opencode --session S || opencode"},
		{"cursor-agent", "S", true, "cursor-agent --resume S || cursor-agent"},
		// agy cannot resume by id: an id it was given changes nothing.
		{"agy", "S", false, "agy"},
	}
	for _, c := range cases {
		if got := agentCommand(c.kind, c.session, c.first, posixShell); got != c.want {
			t.Errorf("agentCommand(%s, %q, %v)\n got %s\nwant %s", c.kind, c.session, c.first, got, c.want)
		}
	}

	// Windows shells: PowerShell 5.1 has no ||, and each shell quotes the
	// hooks path its own way.
	hooks := claudeHooksPath()
	for _, c := range []struct {
		sx            shellSyntax
		kind, session string
		first         bool
		want          string
	}{
		{powerShell, "codex", "S", false, "codex resume S; if ($LASTEXITCODE) { codex }"},
		{powerShell, tabClaude, "S", false, "claude --settings '" + hooks + "' --resume S; if ($LASTEXITCODE) { claude --settings '" + hooks + "' --session-id S }"},
		{cmdShell, "codex", "S", false, "codex resume S || codex"},
		{cmdShell, tabClaude, "S", true, `claude --settings "` + hooks + `" --session-id S`},
	} {
		if got := agentCommand(c.kind, c.session, c.first, c.sx); got != c.want {
			t.Errorf("agentCommand(%s, %q, %v) in syntax %d\n got %s\nwant %s", c.kind, c.session, c.first, c.sx, got, c.want)
		}
	}
	if got, want := powerShell.quote(`C:\Users\O'Brien\x.json`), `'C:\Users\O''Brien\x.json'`; got != want {
		t.Errorf("PowerShell quote = %s, want %s", got, want)
	}
}

// A tab opened on an existing chat resumes it from its very first start, and
// still does after punk0 restarts.
func TestAddTabWithSession(t *testing.T) {
	useSettings(t, map[string]any{})
	repo, home := cleanRepo(t), t.TempDir()
	rec := openRecords(t, home)
	p, err := rec.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	ws := p.Workspaces[0]
	codex, err := rec.AddTab(ws.ID, "codex", "019a-abc_1")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := codex.command(posixShell), "codex resume 019a-abc_1 || codex"; got != want {
		t.Fatalf("first start of a tab opened on a chat\n got %s\nwant %s", got, want)
	}
	claude, err := rec.AddTab(ws.ID, tabClaude, "5f0c-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := claude.command(posixShell); !strings.Contains(got, " --resume 5f0c-1 || ") {
		t.Fatalf("claude tab opened on a chat starts a new session: %s", got)
	}
	fresh, err := rec.AddTab(ws.ID, "codex", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := fresh.command(posixShell); got != "codex" {
		t.Fatalf("a new codex tab must not pick up an older session: %s", got)
	}

	tabs := listed(openRecords(t, home), p.ID).Workspaces[0].Tabs
	if len(tabs) != 3 || tabs[0].tab.Session != "019a-abc_1" || tabs[0].tab.command(posixShell) != "codex resume 019a-abc_1 || codex" {
		t.Fatalf("reloaded tab lost its session: %+v", tabs)
	}

	for _, bad := range []struct{ kind, session string }{
		{"codex", "x; rm -rf ~"},
		{"codex", "a b"},
		{"codex", "--dangerously-bypass-approvals-and-sandbox"}, // a flag, not an id
		{tabShell, "019a"},
		{"agy", "019a"}, // cannot resume by id
	} {
		if _, err := rec.AddTab(ws.ID, bad.kind, bad.session); err == nil {
			t.Errorf("AddTab(%s, %q) accepted", bad.kind, bad.session)
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

// useSettings points the settings file at a temp folder holding m, with a
// Workspaces Folder of its own unless m names one: no test may create the
// real ~/punk0.
func useSettings(t *testing.T, m map[string]any) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	all := map[string]any{"workspace.dir": tempDir(t)}
	for k, v := range m {
		all[k] = v
	}
	data, err := json.Marshal(all)
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
	claude := claudeCommand("claude --model opus", posixShell)
	if !strings.HasPrefix(claude, "claude --model opus --settings ") {
		t.Fatalf("claude command %q", claude)
	}
	for _, c := range []struct {
		kind, session string
		first         bool
		want          string
	}{
		{tabClaude, "S", true, claude + " --session-id S"},
		{tabClaude, "S", false, claude + " --resume S || " + claude + " --session-id S"},
		{"codex", "", true, "codex -m o3"},
		{"codex", "", false, "codex -m o3 resume --last || codex -m o3"},
		{"opencode", "", false, "opencode --continue || opencode"}, // no args set
		{"codex", "S", false, "codex -m o3 resume S || codex -m o3"},
	} {
		if got := agentCommand(c.kind, c.session, c.first, posixShell); got != c.want {
			t.Errorf("agentCommand(%s, %q, %v)\n got %s\nwant %s", c.kind, c.session, c.first, got, c.want)
		}
	}
}

func TestTabProcessShellAndEnv(t *testing.T) {
	t.Setenv("SHELL", "/bin/fromenv")
	useSettings(t, map[string]any{})
	if sh := terminalShell(); sh != "/bin/fromenv" {
		t.Fatalf("default shell %q, want $SHELL", sh)
	}
	if args, _ := tabProcess("/bin/fromenv", "", nil); args != nil {
		t.Fatalf("a plain shell got args %v", args)
	}
	home, _ := os.UserHomeDir()
	useSettings(t, map[string]any{
		"terminal.shell": "/bin/zsh",
		"terminal.env":   "# work account\nCLAUDE_CONFIG_DIR=~/.claude-work\nPUNK0_HOOK_URL=mine\n",
	})
	sh := terminalShell()
	args, env := tabProcess(sh, "claude", []string{"PUNK0_HOOK_URL=punk0"})
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
	if _, env := tabProcess("/bin/zsh", "", nil); len(env) != len(os.Environ())+3 {
		t.Fatal("a terminal.env that does not parse still changed the environment")
	}

	// PowerShell stays open after the command with -NoExit; SHELL is left
	// as it was, since nothing execs it.
	args, env = tabProcess(`C:\Program Files\PowerShell\7\pwsh.exe`, "claude", nil)
	if strings.Join(args, " ") != "-NoLogo -NoExit -Command claude" || slices.Contains(env, `SHELL=C:\Program Files\PowerShell\7\pwsh.exe`) {
		t.Fatalf("pwsh args %v, SHELL set: %v", args, slices.Contains(env, `SHELL=C:\Program Files\PowerShell\7\pwsh.exe`))
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
	if note, err := rec.RemoveWorkspace(merged.ID, RemoveOptions{Files: true}); err != nil || note != "" {
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
	note, err := rec.RemoveWorkspace(open.ID, RemoveOptions{Files: true, Force: true})
	if err != nil || !strings.Contains(note, "open") {
		t.Fatalf("remove unmerged: note %q, err %v", note, err)
	}
	if rec.Workspace(open.ID) != nil {
		t.Fatal("workspace kept after its branch could not be deleted")
	}
	runGit(t, repo, "rev-parse", "--verify", "refs/heads/open")
}

// A worktree adopted from outside the Workspaces Folder tells the page where
// it is; one punk0 made in there does not, so no row carries a path it does
// not need.
func TestAdoptedForeignWorktreeIsOutside(t *testing.T) {
	wb, _, repo := historyWorkbench(t)
	p, err := wb.records.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(tempDir(t), "elsewhere")
	runGit(t, repo, "worktree", "add", "-q", "-b", "x", elsewhere)
	foreign, err := wb.records.AdoptWorktree(p.ID, elsewhere)
	if err != nil {
		t.Fatal(err)
	}
	if !foreign.Adopted {
		t.Fatalf("adopted %s: %+v", elsewhere, foreign)
	}
	ours, _, err := wb.records.AddWorkspace(p.ID, WorkspaceOptions{Name: "ours", Worktree: true})
	if err != nil {
		t.Fatal(err)
	}
	var projects []struct {
		ID         string `json:"id"`
		Workspaces []struct {
			ID      string `json:"id"`
			Kind    string `json:"kind"`
			Outside bool   `json:"outside"`
		} `json:"workspaces"`
	}
	getJSON(t, wb, "/api/projects", &projects)
	outside := map[string]bool{}
	for _, pv := range projects {
		for _, wv := range pv.Workspaces {
			outside[wv.ID] = wv.Outside
		}
	}
	if !outside[foreign.ID] {
		t.Errorf("the adopted worktree %s is not marked outside", elsewhere)
	}
	if outside[ours.ID] {
		t.Errorf("a worktree punk0 made in the Workspaces Folder is marked outside")
	}
	if outside[p.Workspaces[0].ID] {
		t.Errorf("the local workspace is marked outside")
	}
}

// Removing a worktree keeps its branch, so commits on a branch are never
// lost; commits only a detached HEAD reaches would be, and only Lose agrees
// to that -- Force, which the page sends for uncommitted changes, must not.
func TestRemoveWorkspaceKeepsDetachedCommits(t *testing.T) {
	repo := cleanRepo(t)
	rec := openRecords(t, t.TempDir())
	p, err := rec.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	detached := filepath.Join(tempDir(t), "detached")
	runGit(t, repo, "worktree", "add", "-q", "--detach", detached)
	commit(t, detached, "work.txt")
	w, err := rec.AdoptWorktree(p.ID, detached)
	if err != nil {
		t.Fatal(err)
	}
	var lost *lostCommitsError
	if _, err := rec.RemoveWorkspace(w.ID, RemoveOptions{Files: true}); !errors.As(err, &lost) || lost.n != 1 {
		t.Fatalf("remove: %v, want 1 lost commit", err)
	}
	if !isDir(detached) || rec.Workspace(w.ID) == nil {
		t.Fatal("a refused delete removed the folder or the row")
	}
	if _, err := rec.RemoveWorkspace(w.ID, RemoveOptions{Files: true, Force: true}); !errors.As(err, &lost) {
		t.Fatalf("remove with Force: %v, want the same refusal", err)
	}
	if _, err := rec.RemoveWorkspace(w.ID, RemoveOptions{Files: true, Force: true, Lose: true}); err != nil {
		t.Fatalf("remove with Lose: %v", err)
	}
	if isDir(detached) {
		t.Fatal("the worktree folder is still there")
	}

	// A branch keeps its commits, so neither flag is needed.
	onBranch, err := addWorkspace(rec, p.ID, "feature", true)
	if err != nil {
		t.Fatal(err)
	}
	commit(t, onBranch.Dir, "more.txt")
	if _, err := rec.RemoveWorkspace(onBranch.ID, RemoveOptions{Files: true}); err != nil {
		t.Fatalf("remove a worktree on a branch: %v", err)
	}
	runGit(t, repo, "rev-parse", "--verify", "refs/heads/"+onBranch.Branch)
}

// Moving a worktree into the Workspaces Folder keeps the row it is: same id,
// tabs, parent and branch, now punk0's own folder, with git's registration
// and the language-server flag following it.
func TestMoveWorktreeIntoCodeRoot(t *testing.T) {
	root := useCodeRoot(t)
	repo := cleanRepo(t)
	rec := openRecords(t, t.TempDir())
	p, err := rec.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	main := p.Workspaces[0]
	elsewhere := filepath.Join(tempDir(t), "elsewhere")
	runGit(t, repo, "worktree", "add", "-q", "-b", "x", elsewhere)
	w, _, err := rec.AddWorkspace(p.ID, WorkspaceOptions{From: elsewhere, Worktree: true, Parent: main.ID})
	if err != nil {
		t.Fatal(err)
	}
	tab, err := rec.AddTab(w.ID, tabShell, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.SetLSP(elsewhere, true); err != nil {
		t.Fatal(err)
	}

	old, err := rec.MoveWorktree(w.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "workspaces", p.ID, "elsewhere")
	got := rec.Workspace(w.ID)
	if old != elsewhere || got == nil || got.Dir != want {
		t.Fatalf("moved %s to %+v, want %s", old, got, want)
	}
	if got.Adopted || got.Parent != main.ID || got.Branch != "x" || len(got.Tabs) != 1 || got.Tabs[0].ID != tab.ID {
		t.Fatalf("the moved row: %+v", got)
	}
	if tab.dir != want {
		t.Fatalf("the tab still runs in %s", tab.dir)
	}
	if !rec.LSPOn(want) || rec.LSPOn(elsewhere) {
		t.Fatalf("language servers on %s: %v, on %s: %v", want, rec.LSPOn(want), elsewhere, rec.LSPOn(elsewhere))
	}
	if isDir(elsewhere) {
		t.Fatal("the old folder is still there")
	}
	if out := runGit(t, repo, "worktree", "list"); !strings.Contains(out, want) {
		t.Fatalf("git lists:\n%s\nwant %s", out, want)
	}
	// It is punk0's own folder now, so removing the workspace offers it.
	if _, err := rec.RemoveWorkspace(w.ID, RemoveOptions{Files: true}); err != nil {
		t.Fatalf("remove the moved worktree: %v", err)
	}
}

// Everything MoveWorktree refuses is refused before anything moves.
func TestMoveWorktreeRefusals(t *testing.T) {
	wb, _, repo := historyWorkbench(t)
	rec := wb.records
	p, err := rec.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	local := p.Workspaces[0]
	inside, err := addWorkspace(rec, p.ID, "inside", true)
	if err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(tempDir(t), "locked")
	runGit(t, repo, "worktree", "add", "-q", "-b", "locked", locked)
	runGit(t, repo, "worktree", "lock", locked)
	lockedWS, err := rec.AdoptWorktree(p.ID, locked)
	if err != nil {
		t.Fatal(err)
	}
	busy := filepath.Join(tempDir(t), "busy")
	runGit(t, repo, "worktree", "add", "-q", "-b", "busy", busy)
	busyWS, err := rec.AdoptWorktree(p.ID, busy)
	if err != nil {
		t.Fatal(err)
	}
	tab, err := rec.AddTab(busyWS.ID, tabShell, "")
	if err != nil {
		t.Fatal(err)
	}
	// A machine with no usable shell still checks every other refusal.
	busyRuns := false
	if _, _, err := tab.Shell(); err == nil {
		busyRuns = tab.running()
	}
	gone := filepath.Join(tempDir(t), "gone")
	runGit(t, repo, "worktree", "add", "-q", "-b", "gone", gone)
	goneWS, err := rec.AdoptWorktree(p.ID, gone)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}

	type refusal struct{ what, id, want string }
	cases := []refusal{
		{"a local workspace", local.ID, "worktree"},
		{"one already inside", inside.ID, "already"},
		{"a locked worktree", lockedWS.ID, "locked"},
		{"a worktree whose folder is gone", goneWS.ID, "gone"},
		{"an unknown workspace", "nope", "not found"},
	}
	if busyRuns {
		cases = append(cases, refusal{"a worktree with a running tab", busyWS.ID, "stop its tabs"})
	}
	for _, c := range cases {
		_, err := rec.MoveWorktree(c.id)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("move %s: %v, want an error naming %q", c.what, err, c.want)
		}
	}
	if got := rec.Workspace(lockedWS.ID).Dir; got != locked {
		t.Fatalf("the locked worktree moved to %s", got)
	}
}

// tempDir is t.TempDir with symlinks resolved, the way git reports worktree
// folders.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// Every worktree comes back with its branch or detached head and git's
// locked and prunable marks; a space in a folder name stays in the path.
func TestListWorktrees(t *testing.T) {
	repo := cleanRepo(t)
	base := tempDir(t)
	odd, det, gone := filepath.Join(base, "odd worktree dir"), filepath.Join(base, "det"), filepath.Join(base, "gone")
	runGit(t, repo, "branch", "odd")
	runGit(t, repo, "branch", "gone")
	runGit(t, repo, "worktree", "add", "-q", odd, "odd")
	runGit(t, repo, "worktree", "add", "-q", "--detach", det)
	runGit(t, repo, "worktree", "lock", det)
	runGit(t, repo, "worktree", "add", "-q", gone, "gone")
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}

	wts, err := listWorktrees(repo)
	if err != nil {
		t.Fatal(err)
	}
	head := runGit(t, repo, "rev-parse", "HEAD")
	want := []worktreeInfo{
		{Path: repo, Branch: "main", Head: head, Main: true},
		{Path: odd, Branch: "odd", Head: head},
		{Path: det, Head: head, Detached: true, Locked: true},
		{Path: gone, Branch: "gone", Head: head, Prunable: true},
	}
	if len(wts) != len(want) || !wts[0].Main {
		t.Fatalf("listed %+v, want the main checkout first and %d worktrees in all", wts, len(want))
	}
	for _, w := range want {
		if got := worktreeAt(wts, w.Path); got == nil || *got != w {
			t.Errorf("worktree %q = %+v, want %+v", w.Path, got, w)
		}
	}
}

// A worktree folder punk0 did not make is adopted where it is, named after
// its branch or, detached, its folder, and stays adopted across a restart;
// asking again returns the same workspace, also through a symlink to its
// folder, and anything that is not a worktree folder of the project — a
// plain folder, or a relative name, which would be read as a branch — is
// refused.
func TestAdoptWorktree(t *testing.T) {
	useSettings(t, map[string]any{})
	repo, home := cleanRepo(t), t.TempDir()
	base := tempDir(t)
	det := filepath.Join(base, "det")
	runGit(t, repo, "worktree", "add", "-q", "--detach", det)
	rec := openRecords(t, home)
	p, _ := rec.AddProject(repo)

	w, err := rec.AdoptWorktree(p.ID, det)
	if err != nil {
		t.Fatal(err)
	}
	if w.Kind != kindWorktree || w.Name != "det" || w.Dir != det || w.Branch != "" || !w.Adopted {
		t.Fatalf("adopted %+v, want worktree workspace det in %s, adopted", w, det)
	}
	link := filepath.Join(tempDir(t), "link")
	if err := os.Symlink(base, link); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{det, filepath.Join(link, "det")} {
		if again, err := rec.AdoptWorktree(p.ID, dir); err != nil || again.ID != w.ID {
			t.Fatalf("adopting %s again = %+v, %v; want workspace %s", dir, again, err, w.ID)
		}
	}
	for _, dir := range []string{filepath.Join(repo, "sub"), filepath.Join(base, "nowhere"), "det"} {
		if _, err := rec.AdoptWorktree(p.ID, dir); err == nil {
			t.Errorf("adopted %s, which is no worktree", dir)
		}
	}
	if pv := listed(openRecords(t, home), p.ID); len(pv.Workspaces) != 2 || !pv.Workspaces[1].Adopted {
		t.Fatalf("after a restart %+v, want det still adopted", pv.Workspaces)
	}
}

// Removed chats are punk0's own bookkeeping, kept under records.json's
// hiddenChats key (the name on disk from before the UI said "removed"), and
// Sync takes every removal back at once.
func TestRemovedChatsSurviveARestartAndSyncRestoresThem(t *testing.T) {
	home := t.TempDir()
	rec := openRecords(t, home)

	for _, c := range [][2]string{{"claude", "abc"}, {"codex", "def"}, {"opencode", "ses_1"}} {
		if err := rec.RemoveChat(c[0], c[1], true); err != nil {
			t.Fatal(err)
		}
	}
	if err := rec.RemoveChat("codex", "def", false); err != nil {
		t.Fatal(err)
	}
	back := openRecords(t, home)
	removed := back.RemovedChats()
	if !removed[chatKey("claude", "abc")] || removed[chatKey("codex", "def")] || len(removed) != 2 {
		t.Fatalf("removed chats after a restart: %v", removed)
	}
	if raw, err := os.ReadFile(filepath.Join(home, "records.json")); err != nil || !strings.Contains(string(raw), `"hiddenChats"`) {
		t.Fatalf("records.json keeps %q: %v", "hiddenChats", err)
	}

	n, err := back.RestoreAllChats()
	if err != nil || n != 2 {
		t.Fatalf("restore: %d, %v", n, err)
	}
	if got := openRecords(t, home).RemovedChats(); len(got) != 0 {
		t.Fatalf("after Sync: %v", got)
	}
	if n, err := back.RestoreAllChats(); err != nil || n != 0 {
		t.Fatalf("restoring nothing: %d, %v", n, err)
	}
}

func TestSessionTabFindsTheTabOnAChat(t *testing.T) {
	repo, home := cleanRepo(t), t.TempDir()
	rec := openRecords(t, home)
	p, err := rec.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	ws := p.Workspaces[0]
	tab, err := rec.AddTab(ws.ID, tabClaude, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	id, wsID, running := rec.SessionTab(tabClaude, "session-1")
	if id != tab.ID || wsID != ws.ID || running {
		t.Fatalf("SessionTab = %q %q %v", id, wsID, running)
	}
	if id, _, _ := rec.SessionTab(tabClaude, "session-2"); id != "" {
		t.Fatalf("a chat no tab is on reported tab %q", id)
	}
	// One agent's chat id is not another's: the tab is found by both parts.
	if id, _, _ := rec.SessionTab("codex", "session-1"); id != "" {
		t.Fatalf("a codex chat matched a claude tab: %q", id)
	}
	if got := rec.TabSessions()[chatKey(tabClaude, "session-1")]; got != tab.ID {
		t.Fatalf("TabSessions = %v", rec.TabSessions())
	}
}
