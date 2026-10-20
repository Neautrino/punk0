package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, dir, "add", "README.md")
	git(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
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

func TestProjectAddAndReload(t *testing.T) {
	repo, home := gitRepo(t), t.TempDir()
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
	if len(p.Workspaces) != 1 || p.Workspaces[0].Kind != kindLocal || p.Workspaces[0].Name != "main" || len(p.Workspaces[0].Tabs) != 1 {
		t.Fatalf("new project should hold one local workspace on main with one tab: %+v", p.Workspaces)
	}
	local, err := rec.AddWorkspace(p.ID, "sidebar design", false, "")
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
	if len(tabs) != 2 || tabs[1].ID != claude.ID || tabs[1].Session != claude.Session || tabs[1].Running {
		t.Fatalf("reloaded tabs: %+v", tabs)
	}
	if tabs[1].tab.dir != repo {
		t.Fatalf("reloaded tab would start in %q", tabs[1].tab.dir)
	}

	if err := rec.RemoveWorkspace(local.ID, false); err != nil {
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

func TestWorktreeWorkspace(t *testing.T) {
	repo, home := gitRepo(t), t.TempDir()
	rec := openRecords(t, home)
	p, err := rec.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}

	w, err := rec.AddWorkspace(p.ID, "feat-a", true, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(rec.worktrees, p.ID, "feat-a"); w.Dir != want {
		t.Fatalf("worktree at %q, want %q", w.Dir, want)
	}
	if git(t, w.Dir, "rev-parse", "--abbrev-ref", "HEAD") != "feat-a" {
		t.Fatal("worktree not on branch feat-a")
	}
	if st := git(t, repo, "status", "--porcelain"); st != "" {
		t.Fatalf("worktree shows up in the project's status:\n%s", st)
	}

	os.WriteFile(filepath.Join(w.Dir, "wip.txt"), []byte("agent output"), 0o644)
	if err := rec.RemoveWorkspace(w.ID, false); !errors.Is(err, errDirty) {
		t.Fatalf("removing a dirty worktree: %v, want errDirty", err)
	}
	if _, err := os.Stat(filepath.Join(w.Dir, "wip.txt")); err != nil || rec.Workspace(w.ID) == nil {
		t.Fatal("refused removal still deleted the worktree or its record")
	}
	if err := rec.RemoveWorkspace(w.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(w.Dir); !os.IsNotExist(err) {
		t.Fatalf("worktree still on disk: %v", err)
	}
	git(t, repo, "rev-parse", "--verify", "refs/heads/feat-a") // branch survives
}

func TestWorktreeFoundAfterReadd(t *testing.T) {
	repo := gitRepo(t)
	rec := openRecords(t, t.TempDir())
	p, _ := rec.AddProject(repo)
	w, err := rec.AddWorkspace(p.ID, "feat-b", true, "")
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
	repo := gitRepo(t)
	rec := openRecords(t, t.TempDir())
	p, _ := rec.AddProject(repo)
	w, err := rec.AddWorkspace(p.ID, "feat-c", true, "")
	if err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(w.Dir)

	wv := listed(rec, p.ID).Workspaces[1]
	if !wv.Missing || wv.Branch != "feat-c" {
		t.Fatalf("deleted worktree listed as %+v, want missing on feat-c", wv)
	}
	if err := rec.RemoveWorkspace(w.ID, false); err != nil {
		t.Fatal(err)
	}
	if list := git(t, repo, "worktree", "list"); strings.Contains(list, "feat-c") {
		t.Fatalf("git still lists the deleted worktree:\n%s", list)
	}
}

func TestAddWorkspaceRejects(t *testing.T) {
	repo := gitRepo(t)
	rec := openRecords(t, t.TempDir())
	p, _ := rec.AddProject(repo)
	git(t, repo, "branch", "used")
	git(t, repo, "worktree", "add", "-q", filepath.Join(t.TempDir(), "elsewhere"), "used")

	for _, tc := range []struct {
		name     string
		worktree bool
		base     string
	}{
		{"..", true, ""},        // escapes the worktrees folder
		{"a/b", true, ""},       // nested directory
		{"-x", true, ""},        // option-like
		{"", false, ""},         // no name
		{"main", false, ""},     // duplicate of the first workspace
		{"ok", true, "-x"},      // option-like base
		{"bad..ref", true, ""},  // invalid branch
		{"used", true, ""},      // branch checked out in another worktree
		{"ok", true, "nowhere"}, // unknown base
	} {
		if _, err := rec.AddWorkspace(p.ID, tc.name, tc.worktree, tc.base); err == nil {
			t.Errorf("AddWorkspace(%q, %v, %q) succeeded", tc.name, tc.worktree, tc.base)
		}
	}
	if n := len(listed(rec, p.ID).Workspaces); n != 1 {
		t.Fatalf("got %d workspaces after rejected adds, want 1", n)
	}

	plain, err := rec.AddProject(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rec.AddWorkspace(plain.ID, "x", true, ""); err == nil {
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

	w, err := rec.AddWorkspace(scratch.ID, "research", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if w.Kind != kindFolder || w.Dir != filepath.Join(home, "scratch", "research") || !isDir(w.Dir) {
		t.Fatalf("scratch chat %+v, want its own folder under home/scratch", w)
	}
	if len(w.Tabs) != 1 || w.Tabs[0].Kind != tabClaude || w.Tabs[0].Session == "" {
		t.Fatalf("scratch chat should open on a claude tab: %+v", w.Tabs)
	}
	if _, err := rec.AddWorkspace(scratch.ID, "../escape", false, ""); err == nil {
		t.Fatal("scratch chat name escaped its folder")
	}
	os.WriteFile(filepath.Join(w.Dir, "notes.md"), []byte("findings"), 0o644)
	if err := rec.RemoveWorkspace(w.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(w.Dir, "notes.md")); err != nil {
		t.Fatalf("deleting a scratch chat deleted its folder: %v", err)
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

func listed(rec *Records, id string) projectView {
	for _, p := range rec.List() {
		if p.ID == id {
			return p
		}
	}
	return projectView{}
}
