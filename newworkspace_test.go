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

// A new worktree from a remote branch starts from it freshly fetched, not
// from whatever the main checkout has checked out; its branch and its folder
// are named separately. One from nothing starts from the project folder's
// HEAD.
func TestNewWorktreeStartsFromFetchedRemoteBranch(t *testing.T) {
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
	pl := rec.Plan(p.ID, WorkspaceOptions{Name: "Fix login", From: "origin/main", Worktree: true, Branch: "feature/login"})
	if pl.Error != "" || pl.Action != planNew || pl.Base != "origin/main" || pl.Fetch != "origin" || pl.StartIgnored {
		t.Fatalf("plan = %+v, want a new branch from origin/main, fetched from origin", pl)
	}
	w, warning, err := rec.AddWorkspace(p.ID, WorkspaceOptions{Name: "Fix login", From: "origin/main", Worktree: true, Branch: "feature/login"})
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
	if head := runGit(t, w3.Dir, "rev-parse", "HEAD"); head != wip {
		t.Fatalf("worktree from nothing at %s, want the project folder's HEAD %s", head, wip)
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
		o                    WorkspaceOptions
		name, action, branch string
		base                 string
	}{
		{WorkspaceOptions{From: "local-one"}, "local-one", planExisting, "local-one", ""},
		{WorkspaceOptions{From: "origin/remote-only"}, "remote-only", planTrack, "remote-only", "origin/remote-only"},
		{WorkspaceOptions{Branch: "remote-only"}, "remote-only", planTrack, "remote-only", "origin/remote-only"},
		{WorkspaceOptions{From: "busy"}, "busy", planAdopt, "busy", ""}, // checked out in a worktree punk0 did not make
	} {
		tc.o.Worktree = true
		pl := rec.Plan(p.ID, tc.o)
		if pl.Error != "" || pl.Name != tc.name || pl.Action != tc.action || pl.Branch != tc.branch || pl.Base != tc.base {
			t.Errorf("%+v: plan %+v, want %q: %s %s from %q", tc.o, pl, tc.name, tc.action, tc.branch, tc.base)
		}
	}

	w, _, err := rec.AddWorkspace(p.ID, WorkspaceOptions{From: "origin/remote-only", Worktree: true})
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
// the folder keeps the bare name, an existing prefixed branch is reused, and
// a typed branch is used as it is.
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
		{WorkspaceOptions{From: "local-one"}, planExisting, "local-one", "local-one"},
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
	pl := rec.Plan(p.ID, WorkspaceOptions{Name: "x", From: "origin/main", Branch: "x", Worktree: true})
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
	if pl := wb.records.Plan(proj.Project, WorkspaceOptions{From: "outside", Worktree: true}); pl.Action != planOpen || pl.Workspace != local {
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
	decode(t, reqJSON(t, wb, "GET", "/api/projects/"+proj.Project+"/plan?from=outside&worktree=1", "").Body, &pl)
	if pl.Action != planOpen || pl.Workspace != local {
		t.Fatalf("plan over HTTP %+v, want %s opened", pl, local)
	}
	decode(t, reqJSON(t, wb, "GET", "/api/projects/"+proj.Project+"/plan?from=gone&worktree=1", "").Body, &pl)
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

	pl := rec.Plan(p.ID, WorkspaceOptions{From: "outside", Worktree: true})
	if pl.Error != "" || pl.Action != planAdopt || pl.Dir != outside || pl.Branch != "outside" {
		t.Fatalf("plan %+v, want outside adopted in %s", pl, outside)
	}
	if pl = rec.Plan(p.ID, WorkspaceOptions{From: "gone", Worktree: true}); !pl.Prune || !strings.Contains(pl.Error, "prune it") {
		t.Fatalf("plan for a branch of a deleted worktree %+v, want a refusal offering Prune", pl)
	}
	if pl = rec.Plan(p.ID, WorkspaceOptions{Branch: "main", Worktree: true}); pl.Error != "" || pl.Action != planOpen || pl.Workspace != p.Workspaces[0].ID {
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
	if pl = rec.Plan(p.ID, WorkspaceOptions{From: "feat", Worktree: true}); pl.Error != "" || pl.Action != planOpen || pl.Workspace != feat.ID {
		t.Fatalf("plan for a missing worktree workspace's branch %+v, want workspace %s opened", pl, feat.ID)
	}

	w, _, err := rec.AddWorkspace(p.ID, WorkspaceOptions{From: "outside", Worktree: true, Agent: tabShell})
	if err != nil {
		t.Fatal(err)
	}
	if w.Kind != kindWorktree || w.Dir != outside || w.Branch != "outside" || !w.Adopted || len(w.Tabs) != 1 {
		t.Fatalf("adopted %+v, want an adopted worktree workspace in %s with one shell", w, outside)
	}
	if _, err := os.Stat(filepath.Join(outside, "README.md")); err != nil {
		t.Fatalf("adopting touched the folder: %v", err)
	}
	again, _, err := rec.AddWorkspace(p.ID, WorkspaceOptions{From: "outside", Worktree: true, Agent: tabShell})
	if err != nil || again.ID != w.ID || len(again.Tabs) != 1 {
		t.Fatalf("adding outside again = %+v, %v; want workspace %s opened as it was", again, err, w.ID)
	}
	if n := len(listed(rec, p.ID).Workspaces); n != 3 {
		t.Fatalf("%d workspaces, want local, feat and outside", n)
	}

	if err := rec.PruneWorktrees(p.ID); err != nil {
		t.Fatal(err)
	}
	if pl = rec.Plan(p.ID, WorkspaceOptions{From: "gone", Worktree: true}); pl.Error != "" || pl.Action != planExisting || pl.StartIgnored {
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

// The New workspace panel's Behaviour table, every row but pull requests,
// and each error it names, planned; then the actions that change git made.
func TestPlanBehaviour(t *testing.T) {
	useSettings(t, map[string]any{"workspace.branchPrefix": "me/"})
	repo, origin := repoWithOrigin(t)
	other := filepath.Join(t.TempDir(), "other")
	if out, err := exec.Command("git", "clone", "-q", origin, other).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	runGit(t, other, "checkout", "-q", "-b", "remote-only")
	commit(t, other, "r.txt")
	runGit(t, other, "push", "-q", "origin", "remote-only", "remote-only:me/pushed")
	runGit(t, repo, "branch", "free")
	runGit(t, repo, "push", "-q", "origin", "free")
	runGit(t, repo, "fetch", "-q", "origin")
	runGit(t, repo, "branch", "me/taken")
	runGit(t, repo, "tag", "v1")
	sha := runGit(t, repo, "rev-parse", "HEAD")[:12]
	base := tempDir(t)
	busy, gone := filepath.Join(base, "busy"), filepath.Join(base, "gone")
	runGit(t, repo, "branch", "busy")
	runGit(t, repo, "worktree", "add", "-q", busy, "busy")
	runGit(t, repo, "branch", "gone")
	runGit(t, repo, "worktree", "add", "-q", gone, "gone")
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	rec := openRecords(t, t.TempDir())
	p, _ := rec.AddProject(repo)
	made, _, err := rec.AddWorkspace(p.ID, WorkspaceOptions{Name: "made", Worktree: true})
	if err != nil || made.Branch != "me/made" {
		t.Fatalf("made %+v, %v; want it on me/made", made, err)
	}
	// A workspace named free makes the default name for branch free free-2.
	if _, _, err := rec.AddWorkspace(p.ID, WorkspaceOptions{Name: "free"}); err != nil {
		t.Fatal(err)
	}

	type want struct {
		name, action, branch, base string
		folder                     string // the last element of Dir, or all of it when absolute
		workspace                  string // open: the workspace opened; "" is any
		ignored                    bool
		err                        string // a part of the error
	}
	off := func(o WorkspaceOptions) WorkspaceOptions { return o }
	on := func(o WorkspaceOptions) WorkspaceOptions { o.Worktree = true; return o }
	for _, tc := range []struct {
		what string
		o    WorkspaceOptions
		want want
	}{
		// Branch current or empty, box off: the project folder.
		{"project folder", off(WorkspaceOptions{Name: "Here"}), want{name: "Here", action: planLocal, branch: "main", folder: repo}},
		{"project folder from current", off(WorkspaceOptions{Name: "Here", From: "main"}), want{name: "Here", action: planLocal, branch: "main", folder: repo}},
		// Current, empty, tag or commit, box on, no branch typed: named after Name.
		{"from HEAD", on(WorkspaceOptions{Name: "Fix login"}), want{name: "Fix login", action: planNew, branch: "me/fix-login", base: "HEAD", folder: "fix-login"}},
		{"from current", on(WorkspaceOptions{Name: "Fix login", From: "main"}), want{name: "Fix login", action: planNew, branch: "me/fix-login", base: "main", folder: "fix-login"}},
		{"from a tag", on(WorkspaceOptions{Name: "Fix login", From: "v1"}), want{name: "Fix login", action: planNew, branch: "me/fix-login", base: "v1", folder: "fix-login"}},
		{"from a commit", on(WorkspaceOptions{Name: "Fix login", From: sha}), want{name: "Fix login", action: planNew, branch: "me/fix-login", base: sha, folder: "fix-login"}},
		{"named branch exists", on(WorkspaceOptions{Name: "Taken"}), want{name: "Taken", action: planExisting, branch: "me/taken", folder: "taken", ignored: true}},
		{"named branch on origin", on(WorkspaceOptions{Name: "Pushed", From: "v1"}), want{name: "Pushed", action: planTrack, branch: "me/pushed", base: "origin/me/pushed", folder: "pushed", ignored: true}},
		// A branch typed.
		{"typed, new", on(WorkspaceOptions{Name: "Auth", From: "free", Branch: "alice/auth-timeout"}), want{name: "Auth", action: planNew, branch: "alice/auth-timeout", base: "free", folder: "alice-auth-timeout"}},
		{"typed, exists", on(WorkspaceOptions{From: "main", Branch: "free"}), want{name: "free-2", action: planExisting, branch: "free", folder: "free", ignored: true}},
		{"typed, from its remote", on(WorkspaceOptions{From: "origin/remote-only", Branch: "remote-only"}), want{name: "remote-only", action: planTrack, branch: "remote-only", base: "origin/remote-only", folder: "remote-only"}},
		{"typed, on origin", on(WorkspaceOptions{From: "main", Branch: "remote-only"}), want{name: "remote-only", action: planTrack, branch: "remote-only", base: "origin/remote-only", folder: "remote-only", ignored: true}},
		{"typed, in a worktree", on(WorkspaceOptions{Branch: "busy"}), want{name: "busy", action: planAdopt, branch: "busy", folder: busy}},
		{"typed, in a workspace", on(WorkspaceOptions{Branch: "me/made"}), want{action: planOpen, branch: "me/made", folder: made.Dir, workspace: made.ID}},
		{"typed, in the project folder", on(WorkspaceOptions{Branch: "main"}), want{action: planOpen, branch: "main", folder: repo}},
		// A free branch, nothing typed.
		{"free local branch", on(WorkspaceOptions{From: "free"}), want{name: "free-2", action: planExisting, branch: "free", folder: "free"}},
		{"free remote branch", on(WorkspaceOptions{From: "origin/remote-only"}), want{name: "remote-only", action: planTrack, branch: "remote-only", base: "origin/remote-only", folder: "remote-only"}},
		{"remote branch with a local one", on(WorkspaceOptions{From: "origin/free"}), want{name: "free-2", action: planExisting, branch: "free", folder: "free"}},
		// A worktree row.
		{"worktree, adopt", on(WorkspaceOptions{From: busy}), want{name: "busy", action: planAdopt, branch: "busy", folder: busy}},
		{"worktree, adopt named", on(WorkspaceOptions{Name: "Busy work", From: busy}), want{name: "Busy work", action: planAdopt, branch: "busy", folder: busy}},
		{"worktree, open", on(WorkspaceOptions{From: made.Dir}), want{action: planOpen, branch: "me/made", folder: made.Dir, workspace: made.ID}},
		{"worktree, deleted", on(WorkspaceOptions{From: gone}), want{err: "prune it"}},
		{"branch of a deleted worktree", on(WorkspaceOptions{From: "gone"}), want{err: "prune it"}},
		// Errors.
		{"box off, no name", off(WorkspaceOptions{}), want{err: "Type a name"}},
		{"box off, another branch", off(WorkspaceOptions{Name: "x", From: "free"}), want{err: `"free" is not checked out in the project folder`}},
		{"box on, no name", on(WorkspaceOptions{}), want{err: "Type a name or a branch name"}},
		{"box on, tag, no name", on(WorkspaceOptions{From: "v1"}), want{err: "Type a name or a branch name"}},
		{"invalid branch", on(WorkspaceOptions{Name: "x", Branch: "bad..ref"}), want{err: "not a valid branch name"}},
		{"unknown start point", on(WorkspaceOptions{Name: "x", From: "nowhere"}), want{err: `unknown branch or commit "nowhere"`}},
		{"option-like start point", on(WorkspaceOptions{Name: "x", From: "-x"}), want{err: "invalid start point"}},
		{"taken name", on(WorkspaceOptions{Name: "free", From: "v1"}), want{err: `a workspace named "free" already exists`}},
		{"folder of no worktree", on(WorkspaceOptions{From: base}), want{err: "is not a worktree of this project"}},
	} {
		pl := rec.Plan(p.ID, tc.o)
		w := tc.want
		if w.err != "" {
			if !strings.Contains(pl.Error, w.err) {
				t.Errorf("%s: error %q, want %q", tc.what, pl.Error, w.err)
			}
			if w.err == "prune it" && !pl.Prune {
				t.Errorf("%s: %+v does not offer Prune", tc.what, pl)
			}
			continue
		}
		folderOK := filepath.Base(pl.Dir) == w.folder
		if filepath.IsAbs(w.folder) {
			folderOK = sameDir(pl.Dir, w.folder)
		}
		if pl.Error != "" || (w.name != "" && pl.Name != w.name) || pl.Action != w.action || pl.Branch != w.branch || pl.Base != w.base ||
			!folderOK || pl.StartIgnored != w.ignored || (w.action == planOpen && pl.Workspace == "") || (w.workspace != "" && pl.Workspace != w.workspace) {
			t.Errorf("%s: plan %+v, want %+v", tc.what, pl, w)
		}
	}
	plain, _ := rec.AddProject(t.TempDir())
	if pl := rec.Plan(plain.ID, WorkspaceOptions{Name: "x", Worktree: true}); pl.Error != "worktrees need a git repository" {
		t.Errorf("worktree outside git: %q", pl.Error)
	}
	fresh := t.TempDir()
	runGit(t, fresh, "init", "-q")
	empty, _ := rec.AddProject(fresh)
	if pl := rec.Plan(empty.ID, WorkspaceOptions{Name: "x", Worktree: true}); pl.Error != "no commits yet: make a first commit to create worktrees" {
		t.Errorf("worktree with no commits: %q", pl.Error)
	}

	// The actions that run git.
	w, _, err := rec.AddWorkspace(p.ID, WorkspaceOptions{Name: "Auth", From: "free", Branch: "alice/auth-timeout", Worktree: true})
	if err != nil {
		t.Fatal(err)
	}
	if w.Name != "Auth" || w.Branch != "alice/auth-timeout" || runGit(t, w.Dir, "rev-parse", "HEAD") != runGit(t, repo, "rev-parse", "free") {
		t.Fatalf("new %+v, want Auth on alice/auth-timeout at free", w)
	}
	if exec.Command("git", "-C", w.Dir, "rev-parse", "--abbrev-ref", "@{u}").Run() == nil {
		t.Fatal("a new branch tracks its start point")
	}
	w, _, err = rec.AddWorkspace(p.ID, WorkspaceOptions{From: "main", Branch: "free", Worktree: true})
	if err != nil {
		t.Fatal(err)
	}
	if w.Name != "free-2" || runGit(t, w.Dir, "rev-parse", "--abbrev-ref", "HEAD") != "free" {
		t.Fatalf("existing %+v, want free-2 with free checked out", w)
	}
	w, _, err = rec.AddWorkspace(p.ID, WorkspaceOptions{From: "main", Branch: "remote-only", Worktree: true})
	if err != nil {
		t.Fatal(err)
	}
	if u := runGit(t, w.Dir, "rev-parse", "--abbrev-ref", "@{u}"); w.Name != "remote-only" || u != "origin/remote-only" {
		t.Fatalf("track %+v tracks %q, want remote-only tracking origin/remote-only", w, u)
	}

	// A detached project folder: empty is its HEAD, and no branch is current.
	runGit(t, repo, "checkout", "-q", "--detach")
	if pl := rec.Plan(p.ID, WorkspaceOptions{Name: "Detached"}); pl.Error != "" || pl.Action != planLocal {
		t.Errorf("detached project folder: %+v", pl)
	}
	if pl := rec.Plan(p.ID, WorkspaceOptions{Name: "Detached", From: "main"}); !strings.Contains(pl.Error, "not checked out in the project folder") {
		t.Errorf("detached project folder from main: %+v", pl)
	}
	if pl := rec.Plan(p.ID, WorkspaceOptions{Name: "Off detached", Worktree: true}); pl.Error != "" || pl.Action != planNew || pl.Branch != "me/off-detached" || pl.Base != "HEAD" {
		t.Errorf("worktree from a detached HEAD: %+v", pl)
	}
}

// Working on a pull request, without a forge: options as the handlers make
// them, then Base as workOn sets it after its fetch.
func TestPlanPullRequest(t *testing.T) {
	useSettings(t, map[string]any{})
	repo, _ := repoWithOrigin(t)
	runGit(t, repo, "update-ref", "refs/remotes/origin/feature", "HEAD")
	rec := openRecords(t, t.TempDir())
	p, _ := rec.AddProject(repo)
	pp := &projectPull{target: PRTarget{Number: 7}, meta: PRMeta{Title: "Add the feature", HeadRef: "feature"}}

	o := pp.options(WorkspaceOptions{From: "#7", Agent: tabShell})
	if o.From != "" || !o.Worktree || o.Name != "#7 Add the feature" || o.Branch != "feature" || o.Agent != tabShell {
		t.Fatalf("options %+v, want a worktree #7 Add the feature on feature, from nothing", o)
	}
	// Planned before anything is fetched: the handler shows it as pr.
	if pl := rec.Plan(p.ID, o); pl.Error != "" || pl.Branch != "feature" {
		t.Fatalf("plan before the fetch %+v", pl)
	}
	o.Base, o.fetched = "origin/feature", true
	if pl := rec.Plan(p.ID, o); pl.Error != "" || pl.Action != planTrack || pl.Branch != "feature" || pl.Base != "origin/feature" || pl.Fetch != "" || pl.Name != "#7 Add the feature" {
		t.Errorf("head branch: plan %+v, want feature tracking origin/feature, not fetched again", pl)
	}

	typed := pp.options(WorkspaceOptions{From: "#7", Branch: "mine"})
	typed.Base, typed.fetched = "origin/feature", true
	if pl := rec.Plan(p.ID, typed); pl.Error != "" || pl.Action != planNew || pl.Branch != "mine" || pl.Base != "origin/feature" {
		t.Errorf("typed branch: plan %+v, want mine from origin/feature", pl)
	}
	named := pp.options(WorkspaceOptions{From: "#7", Name: "Review it"})
	named.Base, named.fetched = "origin/feature", true
	if pl := rec.Plan(p.ID, named); pl.Error != "" || pl.Name != "Review it" || pl.Branch != "feature" {
		t.Errorf("typed name: plan %+v, want Review it on feature", pl)
	}

	fork := &projectPull{target: PRTarget{Number: 8}, meta: PRMeta{Title: "From a fork", HeadRef: "main", HeadIsFork: true}}
	fo := fork.options(WorkspaceOptions{From: "#8"})
	runGit(t, repo, "update-ref", "refs/punk0/pr/8", "HEAD")
	fo.Base, fo.fetched = "refs/punk0/pr/8", true
	if pl := rec.Plan(p.ID, fo); pl.Error != "" || pl.Action != planNew || pl.Branch != "pr-8" || pl.Base != "refs/punk0/pr/8" {
		t.Errorf("fork: plan %+v, want pr-8 from refs/punk0/pr/8", pl)
	}

	// Worked on again: the branch kept from last time is checked out as it is.
	runGit(t, repo, "branch", "--no-track", "feature", "origin/feature")
	if pl := rec.Plan(p.ID, o); pl.Error != "" || pl.Action != planExisting || pl.Branch != "feature" {
		t.Errorf("kept head branch: plan %+v, want feature checked out as it is", pl)
	}
}

// The panel sends from and branch on the wire: a renamed JSON tag or query
// parameter would plan and make something else.
func TestWorkspaceFromAndBranchOverHTTP(t *testing.T) {
	useSettings(t, map[string]any{})
	repo := cleanRepo(t)
	runGit(t, repo, "branch", "free")
	commit(t, repo, "later.txt")
	newTestStore(t)
	wb := testWorkbench(t, "/", t.TempDir(), "", nil)
	var proj struct{ Project string }
	decode(t, postJSON(t, wb, "/api/projects", `{"dir":"`+repo+`"}`).Body, &proj)

	var pl struct{ Name, Action, Branch, Base string }
	decode(t, reqJSON(t, wb, "GET", "/api/projects/"+proj.Project+"/plan?name=Fix&from=free&branch=alice/fix&worktree=1", "").Body, &pl)
	if pl.Name != "Fix" || pl.Action != planNew || pl.Branch != "alice/fix" || pl.Base != "free" {
		t.Fatalf("plan over HTTP %+v, want Fix: a new alice/fix from free", pl)
	}
	w := postJSON(t, wb, "/api/projects/"+proj.Project+"/workspaces", `{"name":"Fix","from":"free","branch":"alice/fix","worktree":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var made struct{ Name, Branch, Dir string }
	decode(t, w.Body, &made)
	if made.Name != "Fix" || made.Branch != "alice/fix" || runGit(t, made.Dir, "rev-parse", "HEAD") != runGit(t, repo, "rev-parse", "free") {
		t.Fatalf("made %+v, want Fix on alice/fix at free", made)
	}
}

// refs says what the project folder has checked out, and lists the main
// checkout when the project folder is a linked worktree.
func TestRefsCurrentHeadAndMainCheckout(t *testing.T) {
	useSettings(t, map[string]any{})
	repo := cleanRepo(t)
	full := runGit(t, repo, "rev-parse", "HEAD")
	runGit(t, repo, "branch", "outside")
	outside := filepath.Join(tempDir(t), "outside")
	runGit(t, repo, "worktree", "add", "-q", outside, "outside")
	newTestStore(t)
	wb := testWorkbench(t, "/", t.TempDir(), "", nil)
	project := func(dir string) string {
		var proj struct{ Project string }
		decode(t, postJSON(t, wb, "/api/projects", `{"dir":"`+dir+`"}`).Body, &proj)
		return proj.Project
	}
	type refsReply struct {
		Current, Head string
		Empty         bool
		Refs          []string
		Worktrees     []worktreeInfo
	}
	refs := func(id string) refsReply {
		var res refsReply
		decode(t, reqJSON(t, wb, "GET", "/api/projects/"+id+"/refs", "").Body, &res)
		return res
	}
	mainOf := func(res refsReply) *worktreeInfo {
		for i := range res.Worktrees {
			if res.Worktrees[i].Main {
				return &res.Worktrees[i]
			}
		}
		return nil
	}

	top := project(repo)
	res := refs(top)
	if res.Current != "main" || len(res.Head) < 4 || !strings.HasPrefix(full, res.Head) || res.Empty || mainOf(res) != nil {
		t.Fatalf("refs of the main checkout %+v, want current main, head %s, no main entry", res, full)
	}
	res = refs(project(outside))
	if m := mainOf(res); res.Current != "outside" || m == nil || m.Path != repo || m.Branch != "main" {
		t.Fatalf("refs of a linked worktree %+v, want current outside and the main checkout %s on main", res, repo)
	}
	for _, wt := range res.Worktrees {
		if sameDir(wt.Path, outside) {
			t.Fatalf("refs of %s lists its own folder: %+v", outside, res.Worktrees)
		}
	}

	runGit(t, repo, "checkout", "-q", "--detach")
	if res = refs(top); res.Current != "" || !strings.HasPrefix(full, res.Head) || res.Head == "" {
		t.Fatalf("refs of a detached project folder %+v, want no current branch and head %s", res, full)
	}

	var plain map[string]any
	decode(t, reqJSON(t, wb, "GET", "/api/projects/"+project(t.TempDir())+"/refs", "").Body, &plain)
	if plain["current"] != "" || plain["head"] != "" || plain["empty"] != false {
		t.Fatalf("refs of a folder outside git %+v, want current and head empty", plain)
	}
}

// New worktrees go in the Workspaces Folder, where punk0 keeps owning them:
// adding the project again finds them, and they are not adopted folders.
func TestNewWorktreeGoesToCodeRoot(t *testing.T) {
	root := useCodeRoot(t)
	repo := cleanRepo(t)
	rec := openRecords(t, t.TempDir())
	p, err := rec.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	w, _, err := rec.AddWorkspace(p.ID, WorkspaceOptions{Name: "login", Worktree: true})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "workspaces", p.ID, "login"); w.Dir != want {
		t.Fatalf("worktree at %q, want %q", w.Dir, want)
	}
	if w.Adopted {
		t.Error("a worktree punk0 made is adopted")
	}

	if _, err := rec.RemoveProject(p.ID, RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	back, err := rec.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !slicesContainsDir(back.Workspaces, w.Dir) {
		t.Fatalf("adding the project back lost its worktree: %+v", back.Workspaces)
	}
}

func slicesContainsDir(ws []*Workspace, dir string) bool {
	for _, w := range ws {
		if w.Dir == dir {
			return true
		}
	}
	return false
}

// ~/punk0 may be a clone of punk0 itself; worktrees must not be made inside
// a repository, and punk0 still starts when the setting points at one.
func TestCodeRootRefusesRepository(t *testing.T) {
	useSettings(t, map[string]any{"workspace.dir": cleanRepo(t)})
	home := t.TempDir()
	rec := openRecords(t, home)
	p, err := rec.AddProject(cleanRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	pl := rec.Plan(p.ID, WorkspaceOptions{Name: "login", Worktree: true})
	if !strings.Contains(pl.Error, "workspace.dir") {
		t.Fatalf("plan error %q, want one naming workspace.dir", pl.Error)
	}
	if _, _, err := rec.AddWorkspace(p.ID, WorkspaceOptions{Name: "login", Worktree: true}); err == nil {
		t.Fatal("made a worktree inside a git repository")
	}
	var scratch projectView
	for _, v := range rec.List() {
		if v.Scratch {
			scratch = v
		}
	}
	if want := filepath.Join(home, "scratch"); scratch.Dir != want {
		t.Fatalf("scratch folder %q, want the recorded %q while the code root is refused", scratch.Dir, want)
	}
}

// An install from before the Workspaces Folder keeps its worktrees under
// <state>/worktrees: punk0 made them, so it still owns and removes them.
func TestLegacyWorktreesStayOwned(t *testing.T) {
	useCodeRoot(t)
	repo := cleanRepo(t)
	home := tempDir(t)
	legacy := filepath.Join(home, "worktrees", "repo")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(legacy, "old")
	runGit(t, repo, "worktree", "add", "-q", dir, "-b", "old")

	rec := openRecords(t, home)
	p, err := rec.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	var w *Workspace
	for _, x := range p.Workspaces {
		if x.Dir == dir {
			w = x
		}
	}
	if w == nil {
		t.Fatalf("the legacy worktree is not listed: %+v", p.Workspaces)
	}
	if w.Adopted {
		t.Error("a worktree under <state>/worktrees is adopted")
	}
	if _, err := rec.RemoveWorkspace(w.ID, RemoveOptions{Files: true}); err != nil {
		t.Fatal(err)
	}
	if isDir(dir) {
		t.Error("the legacy worktree's folder survived its workspace")
	}
}

// A new install has no <state>/worktrees, which must not make every folder
// look like punk0's own worktree.
func TestOpenAcceptsAnyFolderWithoutLegacyWorktrees(t *testing.T) {
	useCodeRoot(t)
	repo := cleanRepo(t)
	home := t.TempDir()
	rec := openRecords(t, home)
	if rec.worktrees != "" {
		t.Fatalf("a new install has a legacy worktrees folder: %q", rec.worktrees)
	}
	p, w, err := rec.Open(repo)
	if err != nil {
		t.Fatalf("opening a plain repository: %v", err)
	}
	if p.Dir != repo || w.Kind != kindLocal || w.Dir != repo {
		t.Fatalf("Open(%s) = project %+v workspace %+v", repo, p, w)
	}
}
