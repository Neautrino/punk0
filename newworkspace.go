package main

// New workspaces, from the New workspace panel (web/src/orch/newworkspace.js).
// A worktree workspace gets its own folder under <state>/worktrees/<project>
// and its own branch; a local one runs in the project folder. What the panel
// holds is planned first, exactly as creating it would go, so the panel can
// say what will happen -- a new branch from main, an existing branch
// checked out, a worktree made outside punk0 adopted where it is, or why it
// cannot be done -- before anything is made.
//
// git never runs under Records.mu: fetching and checking out a large
// repository takes seconds, and every request of the page waits on that lock.
// Creating reserves the name and folder under the lock, runs git without it,
// then records the workspace.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"px0/internal/settings"
)

// WorkspaceOptions is what the New workspace panel asks for.
type WorkspaceOptions struct {
	Name     string `json:"name"`     // the workspace's label; "" is the default (worktree only)
	From     string `json:"from"`     // branch, remote branch, tag, commit, pull request or worktree folder; "" is the project folder's HEAD
	Branch   string `json:"branch"`   // the new branch's name; "" derives it from From or Name
	Worktree bool   `json:"worktree"` // own folder and branch; else the project folder
	Base     string `json:"-"`        // a pull request's fetched head (workOn): the start point, over From
	Agent    string `json:"agent"`    // the first tab: a tabAgents key or "shell"; "" opens no tab
	LSP      bool   `json:"lsp"`      // language servers on in the new folder
	Parent   string `json:"-"`        // a worktree an agent made: its agent's workspace, which it is listed under

	fetched bool // Base was just fetched (a pull request's head)
}

// Plan actions: what creating the workspace does with git.
const (
	planLocal    = "local"    // runs in the project folder, no git
	planNew      = "new"      // a new branch from Base, not tracking it
	planTrack    = "track"    // a new branch tracking the remote branch Base
	planExisting = "existing" // a branch that exists, checked out as it is
	planPR       = "pr"       // a pull request: reviewed, or worked on in a worktree
	planAdopt    = "adopt"    // a worktree that exists, recorded where it is, no git
	planOpen     = "open"     // a workspace runs in that worktree already: the panel selects it
)

// WorkspacePlan is what creating a workspace from some options would do, or
// Error saying why it cannot.
type WorkspacePlan struct {
	Worktree  bool    `json:"worktree"`
	Name      string  `json:"name"`
	Branch    string  `json:"branch"`
	Action    string  `json:"action"`
	Base      string  `json:"base,omitempty"`
	Fetch     string  `json:"fetch,omitempty"` // the remote fetched before branching from Base
	Dir       string  `json:"dir"`
	Workspace string  `json:"workspace,omitempty"` // open: the workspace running in Dir
	Prune     bool    `json:"prune,omitempty"`     // Error is a deleted worktree that Prune forgets
	Error     string  `json:"error,omitempty"`
	PR        *prPlan `json:"pr,omitempty"`
	// The start point asked for (From, else HEAD) is not used: Branch exists
	// and is checked out as it is, or origin/Branch exists and is tracked.
	StartIgnored bool `json:"startIgnored,omitempty"`

	repo     string // the project folder git runs in
	fetchRef string // the remote's branch Fetch fetches
}

// prPlan is the pull request the panel's text named.
type prPlan struct {
	URL    string `json:"url"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	Head   string `json:"head"`
	Fork   bool   `json:"fork"`
}

// Plan says what AddWorkspace would do with o, without doing it.
func (r *Records) Plan(projectID string, o WorkspaceOptions) WorkspacePlan {
	pl, err := r.plan(projectID, o)
	if err != nil {
		pl.Error = err.Error()
	}
	return pl
}

// plan resolves o in this order: no worktree is the project folder; a folder
// in From is the worktree there; otherwise the start point is Base, else
// From, else HEAD, and the branch is Branch, else one named after Name (From
// empty, the current branch, a tag or a commit), else From itself (a local
// branch) or the local branch for it (a remote one). A branch checked out
// somewhere opens or adopts that worktree; one that exists is checked out as
// it is; else a new branch is made, tracking the remote one it names.
func (r *Records) plan(projectID string, o WorkspaceOptions) (WorkspacePlan, error) {
	pl := WorkspacePlan{Worktree: o.Worktree}
	name := strings.TrimSpace(o.Name)
	from := strings.TrimSpace(o.From)

	// What git needs is copied out, so git runs without the lock.
	r.mu.Lock()
	p := r.projectLocked(projectID)
	if p == nil {
		r.mu.Unlock()
		return pl, fmt.Errorf("project %q: %w", projectID, errNotFound)
	}
	if p.Scratch {
		r.mu.Unlock()
		return pl, errors.New("scratch chats are started with a new chat, not a workspace")
	}
	pl.repo = p.Dir
	names := map[string]bool{}
	taken := map[string]bool{}
	for _, w := range p.Workspaces {
		names[w.Name] = true
		taken[w.Dir] = true
	}
	for k := range r.creating {
		taken[k] = true
	}
	open := r.workspaceDirsLocked()
	pid := p.ID
	r.mu.Unlock()

	if o.Agent != "" && o.Agent != tabShell {
		if _, ok := tabAgents[o.Agent]; !ok {
			return pl, fmt.Errorf("unknown agent %q", o.Agent)
		}
	}
	isGit := gitAvailable(pl.repo)
	current := "" // the project folder's branch, "" when detached
	if isGit {
		if current = gitBranch(pl.repo); current == "HEAD" {
			current = ""
		}
	}
	// The name a workspace gets when none is typed: one its worktree gives,
	// numbered rather than refused, since the user did not choose it.
	defaultName := func(stem string) string {
		n := stem
		for i := 2; names[n]; i++ {
			n = stem + "-" + strconv.Itoa(i)
		}
		return n
	}
	worktreeName := func(wt worktreeInfo) string {
		if name != "" {
			return name
		}
		if wt.Branch != "" {
			return defaultName(wt.Branch)
		}
		return defaultName(filepath.Base(wt.Path))
	}

	// 1. The project folder, on whatever it has checked out.
	if !o.Worktree {
		pl.Action, pl.Dir = planLocal, pl.repo
		if isGit {
			pl.Branch = gitBranch(pl.repo)
		}
		if from != "" && from != current {
			return pl, fmt.Errorf("%q is not checked out in the project folder", from)
		}
		if name == "" {
			return pl, errors.New("Type a name")
		}
		var err error
		if pl.Name, err = label(name, 80); err != nil {
			return pl, err
		}
		if names[pl.Name] {
			return pl, fmt.Errorf("a workspace named %q already exists", pl.Name)
		}
		return pl, nil
	}

	if !isGit {
		return pl, errors.New("worktrees need a git repository")
	}
	if !hasCommit(pl.repo) {
		return pl, errors.New("no commits yet: make a first commit to create worktrees")
	}
	wts, err := listWorktrees(pl.repo)
	if err != nil {
		return pl, err
	}
	// 2. A folder names a worktree that exists, never a branch.
	if filepath.IsAbs(from) || strings.HasPrefix(from, "~/") {
		dir, err := expandPath(from, pl.repo)
		if err != nil {
			return pl, err
		}
		wt := worktreeAt(wts, dir)
		if wt == nil {
			return pl, fmt.Errorf("%s is not a worktree of this project", tildePath(dir))
		}
		pl.Name = worktreeName(*wt)
		return planWorktree(pl, *wt, open, names)
	}

	// 3. The start point.
	base := strings.TrimSpace(o.Base)
	for _, s := range []string{from, base} {
		if strings.HasPrefix(s, "-") {
			return pl, fmt.Errorf("invalid start point %q", s)
		}
	}
	start := base
	if start == "" {
		start = from
	}
	if start == "" {
		start = "HEAD"
	}
	if exec.Command("git", "-C", pl.repo, "rev-parse", "--verify", "--quiet", start+"^{commit}").Run() != nil {
		return pl, fmt.Errorf("unknown branch or commit %q", start)
	}

	// 4. The branch checked out.
	branch := strings.TrimSpace(o.Branch)
	track := ""    // the remote branch From names, which the new branch tracks
	folder := ""   // the worktree's folder, when not named after the branch
	chosen := true // the branch was typed or made from Name, not From itself
	switch {
	case branch != "": // typed: used as it is, with no branchPrefix
	case from == "" || from == current: // named after Name, below
	case refExists(pl.repo, "refs/heads/"+from):
		branch, chosen = from, false
	case remoteBranch(pl.repo, from) != "":
		// origin/feature names the remote branch; the local one is feature.
		track, branch, chosen = from, remoteBranch(pl.repo, from), false
	}
	if branch == "" {
		// A tag, a commit or the current HEAD: a branch named after Name.
		if name == "" {
			return pl, errors.New("Type a name or a branch name")
		}
		// workspace.branchPrefix marks only branches punk0 names itself;
		// the folder keeps the bare name.
		folder = folderName(branchSlug(name))
		branch = strings.TrimSpace(settings.SettingOr[string]("workspace.branchPrefix")) + branchSlug(name)
	}
	if strings.HasPrefix(branch, "-") || branch == "" {
		return pl, fmt.Errorf("invalid branch name %q", branch)
	}
	if out, err := exec.Command("git", "-C", pl.repo, "check-ref-format", "--branch", branch).CombinedOutput(); err != nil {
		return pl, gitError(out, err)
	}
	pl.Branch = branch

	// 5. git checks a branch out in one worktree only: that one is opened or
	// adopted instead of making another.
	if wt := checkedOutAt(wts, branch); wt != nil {
		pl.Name = worktreeName(*wt)
		return planWorktree(pl, *wt, open, names)
	}

	// 7. The name: the branch when none is typed. Checked before 6 so a
	// taken name is refused before git is asked more.
	if name == "" {
		pl.Name = defaultName(branch)
	} else {
		pl.Name = name
	}
	if pl.Name, err = label(pl.Name, 80); err != nil {
		return pl, err
	}
	if names[pl.Name] {
		return pl, fmt.Errorf("a workspace named %q already exists", pl.Name)
	}

	// 6. What git does.
	switch {
	case refExists(pl.repo, "refs/heads/"+branch):
		pl.Action = planExisting
		pl.StartIgnored = chosen && branch != from
	case track != "":
		pl.Action, pl.Base = planTrack, track
	case remoteBranch(pl.repo, start) == branch:
		pl.Action, pl.Base = planTrack, start
	case refExists(pl.repo, "refs/remotes/origin/"+branch):
		// The remote's branch of that name is not shadowed by a new one
		// from somewhere else.
		pl.Action, pl.Base, pl.StartIgnored = planTrack, "origin/"+branch, true
	default:
		pl.Action, pl.Base = planNew, start
	}
	if pl.Base != "" && !o.fetched && settings.SettingOr[bool]("workspace.fetchBase") {
		if remote := remoteOf(pl.repo, pl.Base); remote != "" {
			pl.Fetch, pl.fetchRef = remote, strings.TrimPrefix(strings.TrimPrefix(pl.Base, "refs/remotes/"), remote+"/")
		}
	}

	if folder == "" {
		folder = folderName(branch)
	}
	root, err := codeRoot()
	if err != nil {
		return pl, err
	}
	dirs := filepath.Join(root, "workspaces", pid)
	pl.Dir = filepath.Join(dirs, folder)
	for n := 2; taken[pl.Dir] || exists(pl.Dir); n++ {
		pl.Dir = filepath.Join(dirs, folder+"-"+strconv.Itoa(n))
	}
	return pl, nil
}

// planWorktree is the plan for a worktree that exists: open the workspace
// running in it, adopt it where it is, or say why neither. open maps every
// workspace folder to its workspace (workspaceDirsLocked); names are the
// project's workspace names, which an adopted worktree's pl.Name must not
// repeat. A workspace on a deleted folder is opened, not pruned: its ✕ does
// both.
func planWorktree(pl WorkspacePlan, wt worktreeInfo, open map[string]string, names map[string]bool) (WorkspacePlan, error) {
	pl.Dir, pl.Branch = wt.Path, wt.Branch
	what := "a detached commit"
	if wt.Branch != "" {
		what = "branch " + wt.Branch
	}
	if id := workspaceIn(open, wt.Path); id != "" {
		pl.Action, pl.Workspace = planOpen, id
		return pl, nil
	}
	switch {
	case wt.Prunable:
		pl.Prune = true
		return pl, fmt.Errorf("%s was deleted, but git still has %s checked out there: prune it first", tildePath(wt.Path), what)
	case sameDir(wt.Path, pl.repo):
		return pl, fmt.Errorf("%s is checked out in the project folder: choose it as Branch to work there", what)
	case wt.Main:
		return pl, fmt.Errorf("%s is checked out in the repository's main checkout %s", what, tildePath(wt.Path))
	}
	var err error
	if pl.Name, err = label(pl.Name, 80); err != nil {
		return pl, err
	}
	if names[pl.Name] {
		return pl, fmt.Errorf("a workspace named %q already exists", pl.Name)
	}
	pl.Action = planAdopt
	return pl, nil
}

// AddWorkspace adds a feature to a project, as planned for o. A remote branch
// it starts from is fetched first; when that fails the local copy is used and
// warning says so. A worktree that exists is adopted where it is, with no
// git; one a workspace runs in already returns that workspace, unchanged.
func (r *Records) AddWorkspace(projectID string, o WorkspaceOptions) (w *Workspace, warning string, err error) {
	if o.Parent != "" {
		if !o.Worktree {
			return nil, "", errors.New("only a worktree can be listed under another workspace")
		}
		if p, _ := r.workspaceProject(o.Parent); p != projectID {
			return nil, "", fmt.Errorf("workspace %q is not one of this project's", o.Parent)
		}
	}
	pl, err := r.plan(projectID, o)
	if err != nil {
		return nil, "", err
	}
	if pl.Action == planOpen {
		if w := r.Workspace(pl.Workspace); w != nil {
			return w, "", nil
		}
		return nil, "", fmt.Errorf("workspace %q: %w", pl.Workspace, errNotFound)
	}
	// Two requests planned at once may have picked the same name or folder.
	nameKey := "name\x00" + projectID + "\x00" + pl.Name
	r.mu.Lock()
	if r.creating[nameKey] || (pl.Worktree && r.creating[pl.Dir]) {
		r.mu.Unlock()
		return nil, "", fmt.Errorf("%q is being created", pl.Name)
	}
	r.creating[nameKey] = true
	if pl.Worktree {
		r.creating[pl.Dir] = true
	}
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.creating, nameKey)
		delete(r.creating, pl.Dir)
		r.mu.Unlock()
	}()

	if pl.Worktree && pl.Action != planAdopt {
		if pl.Fetch != "" {
			if err := fetchBranch(pl.repo, pl.Fetch, pl.fetchRef); err != nil {
				warning = fmt.Sprintf("Could not fetch %s from %s (%v), so it starts from the copy you already had.", pl.fetchRef, pl.Fetch, err)
			}
		}
		if err := addWorktree(pl); err != nil {
			return nil, "", err
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.projectLocked(projectID)
	if p == nil {
		// Removed meanwhile. Its worktree stays, and adding the project
		// again finds it.
		return nil, "", fmt.Errorf("project %q: %w", projectID, errNotFound)
	}
	if slices.ContainsFunc(p.Workspaces, func(x *Workspace) bool { return x.Name == pl.Name }) {
		return nil, "", fmt.Errorf("a workspace named %q already exists", pl.Name)
	}
	if pl.Action == planAdopt && workspaceIn(r.workspaceDirsLocked(), pl.Dir) != "" {
		return nil, "", fmt.Errorf("%s is a workspace already", tildePath(pl.Dir))
	}
	if pl.Worktree {
		root, _ := codeRootPath()
		w = newWorkspace(pl.Name, kindWorktree, pl.Dir, pl.Branch)
		// A folder punk0 made is punk0's even when adopted back.
		w.Adopted = pl.Action == planAdopt && !r.ownWorktree(pl.Dir, root)
		// Checked before git ran; a parent removed meanwhile leaves it on top.
		if slices.ContainsFunc(p.Workspaces, func(x *Workspace) bool { return x.ID == o.Parent }) {
			w.Parent = o.Parent
		}
	} else {
		w = newWorkspace(pl.Name, kindLocal, pl.Dir, "")
	}
	if o.Agent != "" {
		t := newTab(o.Agent, w.Dir, "")
		w.Tabs = append(w.Tabs, t)
		r.fileTabLocked(w, t)
	}
	if o.LSP {
		r.lsp[w.Dir] = true
	}
	r.adoptLocked(w)
	p.Workspaces = append(p.Workspaces, w)
	return w, warning, r.saveLocked()
}

// AdoptWorktree makes the worktree at dir, of the project's repository, a
// workspace where it is, as choosing its folder in the New workspace panel
// does; a folder some workspace runs in already returns that workspace. Only
// an absolute folder is one: a relative one would be read as a branch and
// make a worktree instead of adopting one.
func (r *Records) AdoptWorktree(projectID, dir string) (*Workspace, error) {
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("%q is not an absolute folder", dir)
	}
	w, _, err := r.AddWorkspace(projectID, WorkspaceOptions{From: dir, Worktree: true})
	return w, err
}

// Worktrees lists the linked worktrees of the repository at repo, each with
// the workspace running in it, for the New workspace panel. The project
// folder is left out, also when it is itself a linked worktree. A project
// folder that is gone or not a repository has none, which is no error worth
// logging: Chats mode asks this of every project on each Open.
func (r *Records) Worktrees(repo string) []worktreeInfo {
	out := []worktreeInfo{}
	if !isDir(repo) || !gitAvailable(repo) {
		return out
	}
	wts, err := listWorktrees(repo)
	if err != nil {
		log.Printf("worktrees of %s: %v", repo, err)
		return out
	}
	r.mu.Lock()
	dirs := r.workspaceDirsLocked()
	r.mu.Unlock()
	for _, wt := range wts {
		if !wt.Main && !sameDir(wt.Path, repo) {
			wt.Workspace = workspaceIn(dirs, wt.Path)
			out = append(out, wt)
		}
	}
	return out
}

// PruneWorktrees runs git worktree prune in the project, so the branches of
// worktrees whose folders were deleted can be checked out again. Workspaces
// on such folders stay, listed as missing.
func (r *Records) PruneWorktrees(projectID string) error {
	repo := r.ProjectDir(projectID)
	if repo == "" {
		return fmt.Errorf("project %q: %w", projectID, errNotFound)
	}
	if out, err := exec.Command("git", "-C", repo, "worktree", "prune").CombinedOutput(); err != nil {
		return gitError(out, err)
	}
	return nil
}

// ProjectDir is a registered project's folder, "" for none or Scratch.
func (r *Records) ProjectDir(id string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p := r.projectLocked(id); p != nil && !p.Scratch {
		return p.Dir
	}
	return ""
}

func addWorktree(pl WorkspacePlan) error {
	args := []string{"-C", pl.repo, "worktree", "add"}
	switch pl.Action {
	case planExisting:
		args = append(args, pl.Dir, pl.Branch)
	case planTrack:
		args = append(args, "--track", "-b", pl.Branch, pl.Dir, pl.Base)
	default:
		// Not tracking the base: the branch is pushed under its own name.
		args = append(args, "--no-track", "-b", pl.Branch, pl.Dir, pl.Base)
	}
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		return gitError(out, err)
	}
	return nil
}

// fetchBranch brings remote's branch up to date in its remote-tracking ref,
// bounded in time: a remote that does not answer must not hold up the panel.
func fetchBranch(repo, remote, branch string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := runCtx(ctx, gitAuthCmd(gitAuth{}, "-C", repo, "fetch", "--no-tags", remote, branch))
	if err != nil {
		return gitError(out, err)
	}
	return nil
}

// gitRefs lists the repository's branches, local and remote, most recently
// committed first, for the panel's start point list.
func gitRefs(repo string) []string {
	out, err := exec.Command("git", "-C", repo, "for-each-ref", "--sort=-committerdate", "--count=500",
		"--format=%(refname)", "refs/heads", "refs/remotes").Output()
	if err != nil {
		return []string{}
	}
	refs := []string{}
	for _, ref := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if ref == "" || strings.HasSuffix(ref, "/HEAD") {
			continue
		}
		refs = append(refs, strings.TrimPrefix(strings.TrimPrefix(ref, "refs/heads/"), "refs/remotes/"))
	}
	return refs
}

func refExists(repo, ref string) bool {
	return exec.Command("git", "-C", repo, "show-ref", "--verify", "--quiet", ref).Run() == nil
}

// hasCommit reports whether the repository's HEAD is a commit: a new
// repository has none, and a worktree has nothing to start from until it does.
func hasCommit(repo string) bool {
	return exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", "HEAD^{commit}").Run() == nil
}

// remoteOf is the remote whose remote-tracking branch ref is ("origin" for
// origin/main), or "" when ref is no such branch.
func remoteOf(repo, ref string) string {
	ref = strings.TrimPrefix(ref, "refs/remotes/")
	remote, rest, ok := strings.Cut(ref, "/")
	if !ok || rest == "" || !refExists(repo, "refs/remotes/"+ref) {
		return ""
	}
	out, err := exec.Command("git", "-C", repo, "remote").Output()
	if err != nil || !slices.Contains(strings.Fields(string(out)), remote) {
		return ""
	}
	return remote
}

// remoteBranch is the branch name in a remote-tracking ref ("feature" for
// origin/feature), or "".
func remoteBranch(repo, ref string) string {
	remote := remoteOf(repo, ref)
	if remote == "" {
		return ""
	}
	return strings.TrimPrefix(strings.TrimPrefix(ref, "refs/remotes/"), remote+"/")
}

// checkedOutAt is the worktree branch is checked out in, or nil: git refuses
// to check one branch out twice.
func checkedOutAt(wts []worktreeInfo, branch string) *worktreeInfo {
	if i := slices.IndexFunc(wts, func(wt worktreeInfo) bool { return wt.Branch == branch }); i >= 0 {
		return &wts[i]
	}
	return nil
}

var (
	slugBad   = regexp.MustCompile(`[^a-z0-9._/-]+`)
	slugDash  = regexp.MustCompile(`-{2,}`)
	folderBad = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
)

// branchSlug turns a typed name ("Fix login bug") into a branch name
// ("fix-login-bug"); slashes stay, so "feature/login" is kept as it is.
func branchSlug(name string) string {
	s := slugBad.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	s = slugDash.ReplaceAllString(s, "-")
	parts := strings.Split(s, "/")
	out := parts[:0]
	for _, part := range parts {
		if part = strings.Trim(part, "-."); part != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, "/")
}

// folderName is a worktree's folder for branch: one path segment, so
// feature/login is feature-login.
func folderName(branch string) string {
	f := strings.TrimLeft(folderBad.ReplaceAllString(strings.ReplaceAll(branch, "/", "-"), "-"), ".-_")
	if !dirName.MatchString(f) {
		return "worktree"
	}
	return f
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

/* ---------- pull requests ---------- */

var prNumber = regexp.MustCompile(`^#([0-9]+)$`)

// projectPull is a pull request of a project's repository.
type projectPull struct {
	provider GitProvider
	target   PRTarget
	meta     PRMeta
	token    string
	remote   string // the project's remote for the PR's repository
}

// findProjectPull resolves text to a pull request of the repository at dir:
// the URL of one, or #N on the forge the project's remote is on. ok is false
// when text names no pull request at all.
func findProjectPull(ctx context.Context, dir, text string) (pp *projectPull, ok bool, err error) {
	text = strings.TrimSpace(text)
	provider, target, isURL := DetectPRURL(text)
	m := prNumber.FindStringSubmatch(text)
	if !isURL && m == nil {
		return nil, false, nil
	}
	pp = &projectPull{}
	if isURL {
		if _, pp.remote = findPRRemote(dir, target.Owner, target.Repo); pp.remote == "" {
			return nil, true, fmt.Errorf("that pull request is in %s/%s, which is not this project's repository", target.Owner, target.Repo)
		}
	} else {
		n, _ := strconv.Atoi(m[1])
		if provider, target, err = remotePR(dir, n); err != nil {
			return nil, true, err
		}
		_, pp.remote = findPRRemote(dir, target.Owner, target.Repo)
	}
	pp.provider, pp.target = provider, target
	pp.token, _ = provider.ResolveToken(settings.Read(), target)
	if pp.meta, err = provider.FetchPR(ctx, target, pp.token); err != nil {
		return nil, true, fmt.Errorf("cannot read pull request #%d: %w", target.Number, err)
	}
	return pp, true, nil
}

// remotePR is pull request n of the repository the project's origin (or its
// only remote) points at.
func remotePR(dir string, n int) (GitProvider, PRTarget, error) {
	provider, repo, err := originRepo(dir)
	if err != nil {
		return nil, PRTarget{}, fmt.Errorf("#%d: %w", n, err)
	}
	target, err := provider.ParseURL(prWebURL(provider.Name(), repo.Host, repo.Owner, repo.Repo, n))
	return provider, target, err
}

// originRepo is the repository dir's origin (or its only remote) points at,
// as a target with no pull request number.
func originRepo(dir string) (GitProvider, PRTarget, error) {
	remote := "origin"
	if out, err := exec.Command("git", "-C", dir, "remote").Output(); err == nil {
		if rs := strings.Fields(string(out)); !slices.Contains(rs, remote) && len(rs) == 1 {
			remote = rs[0]
		}
	}
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", remote).Output()
	if err != nil {
		return nil, PRTarget{}, fmt.Errorf("this project has no remote named %s", remote)
	}
	rawURL := strings.TrimSpace(string(out))
	provider := ForgeForRemote(rawURL)
	host, path, ok := remoteHostPath(rawURL)
	if provider == nil || !ok {
		return nil, PRTarget{}, fmt.Errorf("%s is not on a forge punk0 knows (GitHub, GitLab, Bitbucket, Gitea)", rawURL)
	}
	i := strings.LastIndex(path, "/")
	if i <= 0 {
		return nil, PRTarget{}, fmt.Errorf("cannot tell the repository from %s", rawURL)
	}
	// Parsed from a pull request page, as everywhere else, so each forge
	// cleans up owner and repository its own way.
	target, err := provider.ParseURL(prWebURL(provider.Name(), host, path[:i], path[i+1:], 1))
	if err != nil {
		return nil, PRTarget{}, err
	}
	target.Number, target.URL = 0, ""
	return provider, target, nil
}

// prWebURL is the page of pull request n on a forge.
func prWebURL(forge, host, owner, repo string, n int) string {
	base := "https://" + host + "/" + owner + "/" + repo
	switch forge {
	case "gitlab":
		return fmt.Sprintf("%s/-/merge_requests/%d", base, n)
	case "bitbucket":
		return fmt.Sprintf("%s/pull-requests/%d", base, n)
	case "gitea":
		return fmt.Sprintf("%s/pulls/%d", base, n)
	default:
		return fmt.Sprintf("%s/pull/%d", base, n)
	}
}

func (pp *projectPull) view() *prPlan {
	url := pp.target.URL
	if url == "" {
		url = prWebURL(pp.provider.Name(), pp.target.Host, pp.target.Owner, pp.target.Repo, pp.target.Number)
	}
	return &prPlan{URL: url, Number: pp.target.Number, Title: pp.meta.Title, Head: pp.meta.HeadRef, Fork: pp.meta.HeadIsFork}
}

// options is o working on the pull request: a worktree named after it unless
// o names it, on the branch o names, else its head branch, or pr-N for a
// fork's, whose branch name means nothing here. From is dropped: it named the
// pull request, which is no start point.
func (pp *projectPull) options(o WorkspaceOptions) WorkspaceOptions {
	n := pp.target.Number
	o.Worktree = true
	o.From = ""
	if strings.TrimSpace(o.Name) == "" {
		o.Name = truncateLabel(fmt.Sprintf("#%d %s", n, pp.meta.Title), 80)
	}
	if strings.TrimSpace(o.Branch) == "" {
		o.Branch = pp.meta.HeadRef
		if pp.meta.HeadIsFork {
			o.Branch = fmt.Sprintf("pr-%d", n)
		}
	}
	return o
}

// workOn fetches the pull request's head and returns the options that make a
// worktree on it.
func (pp *projectPull) workOn(dir string, o WorkspaceOptions) (WorkspaceOptions, error) {
	o = pp.options(o)
	fg := newForgeGit(pp.provider, pp.target, pp.token)
	n := pp.target.Number
	if !pp.meta.HeadIsFork {
		if out, err := gitLocalThenToken(fg.Auth, "-C", dir, "fetch", "--no-tags", pp.remote, pp.meta.HeadRef); err != nil {
			return o, fmt.Errorf("git fetch %s: %s", pp.meta.HeadRef, fg.Auth.redact(strings.TrimSpace(string(out))))
		}
		o.Base = pp.remote + "/" + pp.meta.HeadRef
	} else {
		if fg.HeadRef == "" {
			return o, fmt.Errorf("#%d comes from a fork, and %s publishes no ref for it: review it instead", n, pp.provider.Info().Label)
		}
		ref := fmt.Sprintf("refs/punk0/pr/%d", n)
		if out, err := gitLocalThenToken(fg.Auth, "-C", dir, "fetch", "--no-tags", pp.remote, "+"+fg.HeadRef+":"+ref); err != nil {
			return o, fmt.Errorf("git fetch #%d: %s", n, fg.Auth.redact(strings.TrimSpace(string(out))))
		}
		o.Base = ref
	}
	o.fetched = true
	return o, nil
}

// truncateLabel cuts s to max characters at a word, for a label made from
// someone else's text.
func truncateLabel(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	cut := string(r[:max-1])
	if i := strings.LastIndex(cut, " "); i > max/2 {
		cut = cut[:i]
	}
	return cut + "…"
}
