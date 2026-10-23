package main

// New workspaces, from the New workspace panel (web/src/orch/newworkspace.js).
// A worktree workspace gets its own folder under <state>/worktrees/<project>
// and its own branch; a local one runs in the project folder. What the panel
// holds is planned first, exactly as creating it would go, so the panel can
// say what will happen -- a new branch from origin/main, an existing branch
// checked out, or why it cannot be done -- before anything is made.
//
// git never runs under Records.mu: fetching and checking out a large
// repository takes seconds, and every request of the page waits on that lock.
// Creating reserves the name and folder under the lock, runs git without it,
// then records the workspace.

import (
	"context"
	"errors"
	"fmt"
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
	Name     string `json:"name"`     // the workspace's label, or the branch or pull request to start from
	Worktree bool   `json:"worktree"` // own folder and branch; else the project folder
	Branch   string `json:"-"`        // set for a pull request; "" derives it from Name
	Base     string `json:"base"`     // what a new branch starts from; "" is the default branch
	Agent    string `json:"agent"`    // the first tab: a tabAgents key or "shell"; "" opens no tab
	LSP      bool   `json:"lsp"`      // language servers on in the new folder

	fetched bool // Base was just fetched (a pull request's head)
}

// Plan actions: what creating the workspace does with git.
const (
	planLocal    = "local"    // runs in the project folder, no git
	planNew      = "new"      // a new branch from Base, not tracking it
	planTrack    = "track"    // a new branch tracking the remote branch Base
	planExisting = "existing" // a branch that exists, checked out as it is
	planPR       = "pr"       // a pull request: reviewed, or worked on in a worktree
)

// WorkspacePlan is what creating a workspace from some options would do, or
// Error saying why it cannot.
type WorkspacePlan struct {
	Worktree bool    `json:"worktree"`
	Name     string  `json:"name"`
	Branch   string  `json:"branch"`
	Action   string  `json:"action"`
	Base     string  `json:"base,omitempty"`
	Fetch    string  `json:"fetch,omitempty"` // the remote fetched before branching from Base
	Dir      string  `json:"dir"`
	Error    string  `json:"error,omitempty"`
	PR       *prPlan `json:"pr,omitempty"`

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

func (r *Records) plan(projectID string, o WorkspaceOptions) (WorkspacePlan, error) {
	pl := WorkspacePlan{Worktree: o.Worktree}
	name := strings.TrimSpace(o.Name)

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
	pid := p.ID
	r.mu.Unlock()

	if o.Agent != "" && o.Agent != tabShell {
		if _, ok := tabAgents[o.Agent]; !ok {
			return pl, fmt.Errorf("unknown agent %q", o.Agent)
		}
	}
	if name == "" {
		return pl, errors.New("type a name or a branch")
	}

	if !o.Worktree {
		pl.Action, pl.Dir = planLocal, pl.repo
		if gitAvailable(pl.repo) {
			pl.Branch = gitBranch(pl.repo)
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

	if !gitAvailable(pl.repo) {
		return pl, errors.New("worktrees need a git repository: choose Project folder")
	}
	if !hasCommit(pl.repo) {
		return pl, errors.New("no commits yet: make a first commit to create worktrees, or choose Project folder")
	}
	base := strings.TrimSpace(o.Base)
	branch := strings.TrimSpace(o.Branch)
	if strings.HasPrefix(base, "-") {
		return pl, fmt.Errorf("invalid start point %q", base)
	}
	pl.Name = name
	track := "" // the remote branch the new branch tracks
	folder := ""
	if branch == "" {
		switch {
		case refExists(pl.repo, "refs/heads/"+name):
			branch = name
		case remoteBranch(pl.repo, name) != "":
			// origin/feature names the remote branch; the workspace and its
			// local branch are feature.
			track = name
			branch = remoteBranch(pl.repo, name)
			pl.Name = branch
		default:
			// workspace.branchPrefix marks only branches punk0 names itself;
			// the folder keeps the bare name.
			folder = folderName(branchSlug(name))
			branch = strings.TrimSpace(settings.SettingOr[string]("workspace.branchPrefix")) + branchSlug(name)
		}
	}
	if strings.HasPrefix(branch, "-") || branch == "" {
		return pl, fmt.Errorf("invalid branch name %q", branch)
	}
	if out, err := exec.Command("git", "-C", pl.repo, "check-ref-format", "--branch", branch).CombinedOutput(); err != nil {
		return pl, gitError(out, err)
	}
	pl.Branch = branch
	var err error
	if pl.Name, err = label(pl.Name, 80); err != nil {
		return pl, err
	}
	if names[pl.Name] {
		return pl, fmt.Errorf("a workspace named %q already exists", pl.Name)
	}

	switch {
	case refExists(pl.repo, "refs/heads/"+branch):
		if at := checkedOutAt(pl.repo, branch); at != "" {
			return pl, fmt.Errorf("branch %s is already checked out in %s", branch, tildePath(at))
		}
		pl.Action = planExisting
	case track != "":
		pl.Action, pl.Base = planTrack, track
	case base != "" && remoteBranch(pl.repo, base) == branch:
		pl.Action, pl.Base = planTrack, base
	case refExists(pl.repo, "refs/remotes/origin/"+branch):
		pl.Action, pl.Base = planTrack, "origin/"+branch
	default:
		pl.Action = planNew
		if pl.Base = base; pl.Base == "" {
			pl.Base = defaultBase(pl.repo)
		}
		if exec.Command("git", "-C", pl.repo, "rev-parse", "--verify", "--quiet", pl.Base+"^{commit}").Run() != nil {
			return pl, fmt.Errorf("unknown start point %q", pl.Base)
		}
	}
	if pl.Base != "" && !o.fetched && settings.SettingOr[bool]("workspace.fetchBase") {
		if remote := remoteOf(pl.repo, pl.Base); remote != "" {
			pl.Fetch, pl.fetchRef = remote, strings.TrimPrefix(strings.TrimPrefix(pl.Base, "refs/remotes/"), remote+"/")
		}
	}

	if folder == "" {
		folder = folderName(branch)
	}
	pl.Dir = filepath.Join(r.worktrees, pid, folder)
	for n := 2; taken[pl.Dir] || exists(pl.Dir); n++ {
		pl.Dir = filepath.Join(r.worktrees, pid, folder+"-"+strconv.Itoa(n))
	}
	return pl, nil
}

// AddWorkspace adds a feature to a project, as planned for o. A remote branch
// it starts from is fetched first; when that fails the local copy is used and
// warning says so.
func (r *Records) AddWorkspace(projectID string, o WorkspaceOptions) (w *Workspace, warning string, err error) {
	pl, err := r.plan(projectID, o)
	if err != nil {
		return nil, "", err
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

	if pl.Worktree {
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
	if pl.Worktree {
		w = newWorkspace(pl.Name, kindWorktree, pl.Dir, pl.Branch)
	} else {
		w = newWorkspace(pl.Name, kindLocal, pl.Dir, "")
	}
	if o.Agent != "" {
		w.Tabs = append(w.Tabs, newTab(o.Agent, w.Dir))
	}
	if o.LSP {
		r.lsp[w.Dir] = true
	}
	r.adoptLocked(w)
	p.Workspaces = append(p.Workspaces, w)
	return w, warning, r.saveLocked()
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

// defaultBase is what a new branch starts from when nothing else is said: the
// remote's default branch, since the main checkout may be on any feature
// branch, else the usual local names, else HEAD.
func defaultBase(repo string) string {
	if out, err := exec.Command("git", "-C", repo, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD").Output(); err == nil {
		if b := strings.TrimSpace(string(out)); b != "" {
			return b
		}
	}
	for _, ref := range []string{"refs/remotes/origin/main", "refs/remotes/origin/master", "refs/heads/main", "refs/heads/master"} {
		if refExists(repo, ref) {
			return strings.TrimPrefix(strings.TrimPrefix(ref, "refs/remotes/"), "refs/heads/")
		}
	}
	return "HEAD"
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

// checkedOutAt is the worktree branch is checked out in, or "": git refuses
// to check one branch out twice.
func checkedOutAt(repo, branch string) string {
	out, err := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return ""
	}
	at := ""
	for _, line := range strings.Split(string(out), "\n") {
		if path, ok := strings.CutPrefix(line, "worktree "); ok {
			at = path
		} else if line == "branch refs/heads/"+branch {
			return at
		}
	}
	return ""
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
	remote := "origin"
	if out, err := exec.Command("git", "-C", dir, "remote").Output(); err == nil {
		if rs := strings.Fields(string(out)); !slices.Contains(rs, remote) && len(rs) == 1 {
			remote = rs[0]
		}
	}
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", remote).Output()
	if err != nil {
		return nil, PRTarget{}, fmt.Errorf("#%d: this project has no remote named %s", n, remote)
	}
	rawURL := strings.TrimSpace(string(out))
	provider := ForgeForRemote(rawURL)
	host, path, ok := remoteHostPath(rawURL)
	if provider == nil || !ok {
		return nil, PRTarget{}, fmt.Errorf("#%d: %s is not on a forge punk0 knows (GitHub, GitLab, Bitbucket, Gitea)", n, rawURL)
	}
	i := strings.LastIndex(path, "/")
	if i <= 0 {
		return nil, PRTarget{}, fmt.Errorf("#%d: cannot tell the repository from %s", n, rawURL)
	}
	target, err := provider.ParseURL(prWebURL(provider.Name(), host, path[:i], path[i+1:], n))
	return provider, target, err
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

// options is o working on the pull request: a worktree named after it, on
// its head branch, or on pr-N for a fork's, whose branch name means nothing
// here.
func (pp *projectPull) options(o WorkspaceOptions) WorkspaceOptions {
	n := pp.target.Number
	o.Worktree = true
	o.Name = truncateLabel(fmt.Sprintf("#%d %s", n, pp.meta.Title), 80)
	if o.Branch == "" {
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
