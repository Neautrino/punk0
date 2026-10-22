package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A fetch from a remote that never answers must end at its timeout, even
// though git leaves a helper (git-remote-http) holding the output pipe.
func TestRunCtxKillsWhatTheCommandStarted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no sh")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := runCtx(ctx, exec.Command("sh", "-c", "sleep 30; :")); err == nil {
		t.Fatal("a command killed at its timeout succeeded")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("runCtx returned after %s: the command's child kept it waiting", d)
	}
}

// repoWithOrigin is a clean repository whose origin is a bare clone of it,
// fetched, so origin/main and origin/HEAD exist like in a normal clone.
func repoWithOrigin(t *testing.T) (repo, origin string) {
	t.Helper()
	repo = cleanRepo(t)
	origin = filepath.Join(t.TempDir(), "origin.git")
	if out, err := exec.Command("git", "clone", "-q", "--bare", repo, origin).CombinedOutput(); err != nil {
		t.Fatalf("clone --bare: %v\n%s", err, out)
	}
	runGit(t, repo, "remote", "add", "origin", origin)
	runGit(t, repo, "fetch", "-q", "origin")
	runGit(t, repo, "remote", "set-head", "origin", "main")
	return repo, origin
}

func commit(t *testing.T, dir, file string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(file+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", file)
	runGit(t, dir, "commit", "-q", "-m", file)
	return runGit(t, dir, "rev-parse", "HEAD")
}

// A new worktree starts from the remote's default branch, freshly fetched,
// not from whatever the main checkout has checked out; its branch and its
// folder are named separately.
func TestNewWorktreeStartsFromFetchedDefaultBranch(t *testing.T) {
	repo, origin := repoWithOrigin(t)
	runGit(t, repo, "checkout", "-q", "-b", "wip")
	wip := commit(t, repo, "wip.txt")

	// Someone else pushes to main after the last fetch.
	other := filepath.Join(t.TempDir(), "other")
	if out, err := exec.Command("git", "clone", "-q", origin, other).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	pushed := commit(t, other, "pushed.txt")
	runGit(t, other, "push", "-q", "origin", "main")

	rec := openRecords(t, t.TempDir())
	p, err := rec.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	pl := rec.Plan(p.ID, WorkspaceOptions{Name: "Fix login", Worktree: true, Branch: "feature/login"})
	if pl.Error != "" || pl.Action != planNew || pl.Base != "origin/main" || pl.Fetch != "origin" {
		t.Fatalf("plan = %+v, want a new branch from origin/main, fetched from origin", pl)
	}
	w, warning, err := rec.AddWorkspace(p.ID, WorkspaceOptions{Name: "Fix login", Worktree: true, Branch: "feature/login"})
	if err != nil || warning != "" {
		t.Fatalf("AddWorkspace: %v, warning %q", err, warning)
	}
	if w.Name != "Fix login" || w.Branch != "feature/login" || filepath.Base(w.Dir) != "feature-login" {
		t.Fatalf("workspace %q on %q in %s, want \"Fix login\" on feature/login in feature-login", w.Name, w.Branch, w.Dir)
	}
	head := runGit(t, w.Dir, "rev-parse", "HEAD")
	if head == wip {
		t.Fatal("the worktree started from the main checkout's feature branch")
	}
	if head != pushed {
		t.Fatalf("worktree at %s, want the commit pushed to origin/main (%s): origin was not fetched", head, pushed)
	}
	if exec.Command("git", "-C", w.Dir, "rev-parse", "--abbrev-ref", "@{u}").Run() == nil {
		t.Fatal("a new branch tracks its start point; pushing it would go to main")
	}

	// A name alone becomes the branch, and a second branch of the same
	// folder name gets its own folder.
	w2, _, err := rec.AddWorkspace(p.ID, WorkspaceOptions{Name: "Feature Login", Worktree: true, Branch: "feature-login"})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(w2.Dir) != "feature-login-2" {
		t.Fatalf("second feature-login folder is %s", w2.Dir)
	}
	w3, _, err := rec.AddWorkspace(p.ID, WorkspaceOptions{Name: "Tidy README", Worktree: true})
	if err != nil {
		t.Fatal(err)
	}
	if w3.Branch != "tidy-readme" {
		t.Fatalf("branch from name %q = %q", w3.Name, w3.Branch)
	}
}

func TestPlanBranches(t *testing.T) {
	repo, origin := repoWithOrigin(t)
	other := filepath.Join(t.TempDir(), "other")
	if out, err := exec.Command("git", "clone", "-q", origin, other).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	runGit(t, other, "checkout", "-q", "-b", "remote-only")
	commit(t, other, "r.txt")
	runGit(t, other, "push", "-q", "origin", "remote-only")
	runGit(t, repo, "fetch", "-q", "origin")
	runGit(t, repo, "branch", "local-one")
	runGit(t, repo, "branch", "busy")
	busy := filepath.Join(t.TempDir(), "busy")
	runGit(t, repo, "worktree", "add", "-q", busy, "busy")

	rec := openRecords(t, t.TempDir())
	p, _ := rec.AddProject(repo)
	for _, tc := range []struct {
		name, action, branch, base, err string
	}{
		{"local-one", planExisting, "local-one", "", ""},
		{"origin/remote-only", planTrack, "remote-only", "origin/remote-only", ""},
		{"remote-only", planTrack, "remote-only", "origin/remote-only", ""},
		{"busy", "", "", "", "already checked out in"},
	} {
		pl := rec.Plan(p.ID, WorkspaceOptions{Name: tc.name, Worktree: true})
		if tc.err != "" {
			if !strings.Contains(pl.Error, tc.err) {
				t.Errorf("%s: error %q, want %q", tc.name, pl.Error, tc.err)
			}
			continue
		}
		if pl.Error != "" || pl.Action != tc.action || pl.Branch != tc.branch || pl.Base != tc.base {
			t.Errorf("%s: plan %+v, want %s %s from %q", tc.name, pl, tc.action, tc.branch, tc.base)
		}
	}

	w, _, err := rec.AddWorkspace(p.ID, WorkspaceOptions{Name: "origin/remote-only", Worktree: true})
	if err != nil {
		t.Fatal(err)
	}
	if w.Name != "remote-only" {
		t.Fatalf("workspace for origin/remote-only named %q", w.Name)
	}
	if u := runGit(t, w.Dir, "rev-parse", "--abbrev-ref", "@{u}"); u != "origin/remote-only" {
		t.Fatalf("tracks %q, want origin/remote-only", u)
	}
}

func TestNewWorkspaceTabAndLanguageServers(t *testing.T) {
	repo := cleanRepo(t)
	rec := openRecords(t, t.TempDir())
	p, _ := rec.AddProject(repo)
	w, _, err := rec.AddWorkspace(p.ID, WorkspaceOptions{Name: "with-shell", Worktree: true, Agent: tabShell, LSP: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Tabs) != 1 || w.Tabs[0].Kind != tabShell {
		t.Fatalf("tabs %+v, want one shell", w.Tabs)
	}
	if !rec.LSPOn(w.Dir) {
		t.Fatal("language servers asked for but off in the new folder")
	}
	none, err := addWorkspace(rec, p.ID, "no-agent", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(none.Tabs) != 0 || rec.LSPOn(none.Dir) {
		t.Fatal("no agent and no language servers asked for, yet got some")
	}
}

func TestBranchAndFolderNames(t *testing.T) {
	for in, want := range map[string]string{
		"Fix login bug": "fix-login-bug",
		"feature/Login": "feature/login",
		"  --x--  ":     "x",
		"a//b":          "a/b",
		"émoji 🎉 time!": "moji-time",
	} {
		if got := branchSlug(in); got != want {
			t.Errorf("branchSlug(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"feature/login": "feature-login",
		".hidden":       "hidden",
		"":              "worktree",
		"v1.2":          "v1.2",
	} {
		if got := folderName(in); got != want {
			t.Errorf("folderName(%q) = %q, want %q", in, got, want)
		}
	}
}

// #N is turned into the forge's own PR URL, which must parse back to the
// same pull request.
func TestPRWebURLParsesBack(t *testing.T) {
	for _, tc := range []struct{ forge, host, owner string }{
		{"github", "github.com", "octo"},
		{"gitlab", "gitlab.com", "group/sub"},
		{"bitbucket", "bitbucket.org", "team"},
		{"gitea", "codeberg.org", "ana"},
	} {
		u := prWebURL(tc.forge, tc.host, tc.owner, "repo", 42)
		p, target, ok := DetectPRURL(u)
		if !ok || p.Name() != tc.forge || target.Owner != tc.owner || target.Repo != "repo" || target.Number != 42 {
			t.Errorf("%s: %s parsed as %v %+v", tc.forge, u, ok, target)
		}
	}
}
