package main

import (
	"context"
	"net/http"
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
		name, action, branch, base string
	}{
		{"local-one", planExisting, "local-one", ""},
		{"origin/remote-only", planTrack, "remote-only", "origin/remote-only"},
		{"remote-only", planTrack, "remote-only", "origin/remote-only"},
		{"busy", planAdopt, "busy", ""}, // checked out in a worktree punk0 did not make
	} {
		pl := rec.Plan(p.ID, WorkspaceOptions{Name: tc.name, Worktree: true})
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

// workspace.branchPrefix names only branches punk0 derives from the name;
// the folder keeps the bare name, and an existing prefixed branch is reused.
func TestPlanBranchPrefix(t *testing.T) {
	useSettings(t, map[string]any{"workspace.branchPrefix": "me/"})
	repo := cleanRepo(t)
	runGit(t, repo, "branch", "local-one")
	runGit(t, repo, "branch", "me/again")
	rec := openRecords(t, t.TempDir())
	p, _ := rec.AddProject(repo)
	for _, tc := range []struct {
		o              WorkspaceOptions
		action, branch string
		folder         string
	}{
		{WorkspaceOptions{Name: "Fix login"}, planNew, "me/fix-login", "fix-login"},
		{WorkspaceOptions{Name: "Fix", Branch: "pr-branch"}, planNew, "pr-branch", "pr-branch"},
		{WorkspaceOptions{Name: "local-one"}, planExisting, "local-one", "local-one"},
		{WorkspaceOptions{Name: "again"}, planExisting, "me/again", "again"},
	} {
		tc.o.Worktree = true
		pl := rec.Plan(p.ID, tc.o)
		if pl.Error != "" || pl.Action != tc.action || pl.Branch != tc.branch || filepath.Base(pl.Dir) != tc.folder {
			t.Errorf("%+v: plan %+v, want %s %s in %s", tc.o, pl, tc.action, tc.branch, tc.folder)
		}
	}
}

func TestPlanFetchBaseOff(t *testing.T) {
	useSettings(t, map[string]any{"workspace.fetchBase": false})
	repo, _ := repoWithOrigin(t)
	rec := openRecords(t, t.TempDir())
	p, _ := rec.AddProject(repo)
	pl := rec.Plan(p.ID, WorkspaceOptions{Name: "x", Worktree: true})
	if pl.Error != "" || pl.Base != "origin/main" || pl.Fetch != "" {
		t.Fatalf("plan %+v, want origin/main with no fetch", pl)
	}
}

// The panel's suggestions: refs lists the worktrees other than the project
// folder, with the workspace running in each (in any project), and Prune
// drops deleted ones.
func TestRefsListWorktreesAndPrune(t *testing.T) {
	useSettings(t, map[string]any{})
	repo := cleanRepo(t)
	runGit(t, repo, "branch", "outside")
	runGit(t, repo, "branch", "gone")
	base := tempDir(t)
	outside, gone := filepath.Join(base, "outside"), filepath.Join(base, "gone")
	runGit(t, repo, "worktree", "add", "-q", outside, "outside")
	runGit(t, repo, "worktree", "add", "-q", gone, "gone")
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	newTestStore(t)
	wb := testWorkbench(t, "/", t.TempDir(), "", nil)
	var proj struct{ Project string }
	decode(t, postJSON(t, wb, "/api/projects", `{"dir":"`+repo+`"}`).Body, &proj)
	var made struct{ ID, Dir string }
	decode(t, postJSON(t, wb, "/api/projects/"+proj.Project+"/workspaces", `{"name":"made","worktree":true}`).Body, &made)

	refs := func(id string) map[string]worktreeInfo {
		var res struct{ Worktrees []worktreeInfo }
		decode(t, reqJSON(t, wb, "GET", "/api/projects/"+id+"/refs", "").Body, &res)
		m := map[string]worktreeInfo{}
		for _, wt := range res.Worktrees {
			m[wt.Path] = wt
		}
		return m
	}
	got := refs(proj.Project)
	if len(got) != 3 || got[made.Dir].Workspace != made.ID || got[outside].Workspace != "" || got[outside].Branch != "outside" || !got[gone].Prunable {
		t.Fatalf("refs worktrees %+v, want made (workspace %s), outside (none) and gone (prunable), not the project folder", got, made.ID)
	}

	// A worktree folder is a project of its own: its local workspace is the
	// one running in outside, and the first project opens it.
	var other struct{ Project string }
	decode(t, postJSON(t, wb, "/api/projects", `{"dir":"`+outside+`"}`).Body, &other)
	local := listed(wb.records, other.Project).Workspaces[0].ID
	if got = refs(proj.Project); got[outside].Workspace != local {
		t.Fatalf("outside's workspace %q, want the other project's %s", got[outside].Workspace, local)
	}
	if pl := wb.records.Plan(proj.Project, WorkspaceOptions{Name: "outside", Worktree: true}); pl.Action != planOpen || pl.Workspace != local {
		t.Fatalf("plan for outside %+v, want the other project's workspace opened", pl)
	}
	if _, ok := refs(other.Project)[outside]; ok {
		t.Fatal("a project that is a linked worktree lists its own folder")
	}

	// The panel reads these two off the wire: a renamed JSON tag would leave
	// the Open button without a workspace and hide the Prune one.
	var pl struct {
		Action    string
		Workspace string
		Prune     bool
	}
	decode(t, reqJSON(t, wb, "GET", "/api/projects/"+proj.Project+"/plan?name=outside&worktree=1", "").Body, &pl)
	if pl.Action != planOpen || pl.Workspace != local {
		t.Fatalf("plan over HTTP %+v, want %s opened", pl, local)
	}
	decode(t, reqJSON(t, wb, "GET", "/api/projects/"+proj.Project+"/plan?name=gone&worktree=1", "").Body, &pl)
	if !pl.Prune {
		t.Fatalf("plan for gone over HTTP %+v, want prune", pl)
	}

	if w := postJSON(t, wb, "/api/projects/"+proj.Project+"/prune", ""); w.Code != http.StatusOK {
		t.Fatalf("prune: %d %s", w.Code, w.Body.String())
	}
	if got = refs(proj.Project); len(got) != 2 || got[gone].Path != "" {
		t.Fatalf("refs worktrees after Prune %+v, want made and outside only", got)
	}
}

// A branch checked out in a worktree punk0 did not make adopts that worktree
// where it is; one a workspace runs in opens that workspace, even when its
// folder was deleted; one whose folder was deleted otherwise is refused until
// Prune.
func TestPlanExistingWorktrees(t *testing.T) {
	useSettings(t, map[string]any{})
	repo := cleanRepo(t)
	runGit(t, repo, "branch", "outside")
	runGit(t, repo, "branch", "gone")
	base := tempDir(t)
	outside, gone := filepath.Join(base, "outside"), filepath.Join(base, "gone")
	runGit(t, repo, "worktree", "add", "-q", outside, "outside")
	runGit(t, repo, "worktree", "add", "-q", gone, "gone")
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	rec := openRecords(t, t.TempDir())
	p, _ := rec.AddProject(repo)

	pl := rec.Plan(p.ID, WorkspaceOptions{Name: "outside", Worktree: true})
	if pl.Error != "" || pl.Action != planAdopt || pl.Dir != outside || pl.Branch != "outside" {
		t.Fatalf("plan %+v, want outside adopted in %s", pl, outside)
	}
	if pl = rec.Plan(p.ID, WorkspaceOptions{Name: "gone", Worktree: true}); !pl.Prune || !strings.Contains(pl.Error, "prune it") {
		t.Fatalf("plan for a branch of a deleted worktree %+v, want a refusal offering Prune", pl)
	}
	if pl = rec.Plan(p.ID, WorkspaceOptions{Name: "main", Worktree: true}); pl.Error != "" || pl.Action != planOpen || pl.Workspace != p.Workspaces[0].ID {
		t.Fatalf("plan for the project folder's branch %+v, want its local workspace opened", pl)
	}

	// A punk0 worktree deleted outside punk0 is listed as missing; its
	// branch opens that row, whose ✕ prunes, rather than pruning under it.
	feat, _, err := rec.AddWorkspace(p.ID, WorkspaceOptions{Name: "feat", Worktree: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(feat.Dir); err != nil {
		t.Fatal(err)
	}
	if pl = rec.Plan(p.ID, WorkspaceOptions{Name: "feat", Worktree: true}); pl.Error != "" || pl.Action != planOpen || pl.Workspace != feat.ID {
		t.Fatalf("plan for a missing worktree workspace's branch %+v, want workspace %s opened", pl, feat.ID)
	}

	w, _, err := rec.AddWorkspace(p.ID, WorkspaceOptions{Name: "outside", Worktree: true, Agent: tabShell})
	if err != nil {
		t.Fatal(err)
	}
	if w.Kind != kindWorktree || w.Dir != outside || w.Branch != "outside" || !w.Adopted || len(w.Tabs) != 1 {
		t.Fatalf("adopted %+v, want an adopted worktree workspace in %s with one shell", w, outside)
	}
	if _, err := os.Stat(filepath.Join(outside, "README.md")); err != nil {
		t.Fatalf("adopting touched the folder: %v", err)
	}
	again, _, err := rec.AddWorkspace(p.ID, WorkspaceOptions{Name: "outside", Worktree: true, Agent: tabShell})
	if err != nil || again.ID != w.ID || len(again.Tabs) != 1 {
		t.Fatalf("adding outside again = %+v, %v; want workspace %s opened as it was", again, err, w.ID)
	}
	if n := len(listed(rec, p.ID).Workspaces); n != 3 {
		t.Fatalf("%d workspaces, want local, feat and outside", n)
	}

	if err := rec.PruneWorktrees(p.ID); err != nil {
		t.Fatal(err)
	}
	if pl = rec.Plan(p.ID, WorkspaceOptions{Name: "gone", Worktree: true}); pl.Error != "" || pl.Action != planExisting {
		t.Fatalf("plan after Prune %+v, want gone checked out anew", pl)
	}

	// Deleting an adopted workspace forgets it; the folder is the user's.
	if _, err := rec.RemoveWorkspace(w.ID, RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("deleting the adopted workspace removed its folder: %v", err)
	}
	if !strings.Contains(runGit(t, repo, "worktree", "list"), outside) {
		t.Fatal("deleting the adopted workspace unregistered its worktree")
	}
}
