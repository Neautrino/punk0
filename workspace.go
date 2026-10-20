package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode"
)

// Workspace kinds. A local workspace runs in the project folder, which any
// number of local workspaces share; a worktree workspace owns exactly one
// git worktree, so deleting it can never pull files from under another.
const (
	kindLocal    = "local"
	kindWorktree = "worktree"
)

// Tab kinds.
const (
	tabShell  = "shell"
	tabClaude = "claude"
)

// dirName is a worktree workspace's directory and default branch name. The
// leading letter or digit rules out ".", ".." and option-like names.
var dirName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

var (
	errNotFound = errors.New("not found")
	errDirty    = errors.New("worktree has uncommitted changes")
)

// Project is a folder the user registered: a git repository's top level or
// a plain folder.
type Project struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	Dir        string       `json:"dir"`
	Workspaces []*Workspace `json:"workspaces"`
}

// Workspace is one feature: the tabs running in Dir and the file index the
// right pane shows, built on first view.
type Workspace struct {
	ID     string       `json:"id"`
	Name   string       `json:"name"`
	Kind   string       `json:"kind"`
	Dir    string       `json:"dir"`
	Branch string       `json:"branch,omitempty"` // worktree kind: shown when the folder is gone
	Tabs   []*Satellite `json:"tabs"`

	mu sync.Mutex
	ix *Index
}

// Satellite is one tab: a shell in its workspace's folder, started on first
// attach so a stopped tab costs no process.
type Satellite struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Session string `json:"session,omitempty"` // claude --session-id, so the chat can be resumed

	dir string
	mu  sync.Mutex
	sh  *Shell
}

// Shell returns the tab's running shell, starting one if there is none or
// the last one exited.
func (s *Satellite) Shell() (*Shell, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sh == nil || s.sh.Exited() {
		sh, err := startShell(s.dir)
		if err != nil {
			return nil, err
		}
		s.sh = sh
	}
	return s.sh, nil
}

func (s *Satellite) running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sh != nil && !s.sh.Exited()
}

func (s *Satellite) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sh != nil {
		s.sh.Close()
		s.sh = nil
	}
}

func (w *Workspace) Index() *Index {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ix == nil {
		w.ix = NewIndex(w.Dir)
		go w.ix.Build()
	}
	return w.ix
}

// Records is Punk Records: every project, workspace and tab, saved to
// records.json on each change. Running shells are never saved; after a
// restart tabs are listed stopped and start again on attach.
type Records struct {
	home      string // ~/.punk0
	worktrees string // home/worktrees, symlinks resolved to match git's paths

	// mu guards projects and the file, and serializes worktree add and
	// remove so two requests cannot race for one name.
	mu       sync.Mutex
	projects []*Project
}

// OpenRecords loads home/records.json; a missing file is an empty list. A
// file that does not parse is an error rather than an empty list, so a
// later save cannot overwrite it.
func OpenRecords(home string) (*Records, error) {
	if err := os.MkdirAll(filepath.Join(home, "worktrees"), 0o700); err != nil {
		return nil, err
	}
	wt, err := filepath.EvalSymlinks(filepath.Join(home, "worktrees"))
	if err != nil {
		return nil, err
	}
	r := &Records{home: home, worktrees: wt}
	data, err := os.ReadFile(r.file())
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &r.projects); err != nil {
		return nil, fmt.Errorf("%s: %w", r.file(), err)
	}
	for _, p := range r.projects {
		for _, w := range p.Workspaces {
			for _, t := range w.Tabs {
				t.dir = w.Dir
			}
		}
	}
	return r, nil
}

func (r *Records) file() string { return filepath.Join(r.home, "records.json") }

// saveLocked writes through a temporary file so a crash mid-write leaves the
// previous records intact.
func (r *Records) saveLocked() error {
	data, err := json.MarshalIndent(r.projects, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.file() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, r.file())
}

type projectView struct {
	*Project
	Git        bool            `json:"git"`
	Branch     string          `json:"branch"`
	Workspaces []workspaceView `json:"workspaces"`
}

type workspaceView struct {
	*Workspace
	Branch  string    `json:"branch"`
	Missing bool      `json:"missing"`
	Tabs    []tabView `json:"tabs"`
}

// tabView copies the tab's fields under the lock, since a rename may change
// Name while the response is being encoded.
type tabView struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Session string `json:"session,omitempty"`
	Running bool   `json:"running"`

	tab *Satellite
}

// List returns the tree with live state: current branches, worktree folders
// deleted outside punk0, and which tabs have a running shell.
func (r *Records) List() []projectView {
	r.mu.Lock()
	// Slices start non-nil so an empty project or workspace encodes as [].
	views := make([]projectView, len(r.projects))
	for i, p := range r.projects {
		views[i].Project = p
		views[i].Workspaces = make([]workspaceView, len(p.Workspaces))
		for j, w := range p.Workspaces {
			wv := &views[i].Workspaces[j]
			wv.Workspace, wv.Branch = w, w.Branch
			wv.Tabs = make([]tabView, len(w.Tabs))
			for k, t := range w.Tabs {
				wv.Tabs[k] = tabView{ID: t.ID, Kind: t.Kind, Name: t.Name, Session: t.Session, tab: t}
			}
		}
	}
	r.mu.Unlock()

	// git runs outside the lock: listing must not wait behind a worktree add.
	for i := range views {
		pv := &views[i]
		if pv.Git = gitAvailable(pv.Dir); pv.Git {
			pv.Branch = gitBranch(pv.Dir)
		}
		for j := range pv.Workspaces {
			wv := &pv.Workspaces[j]
			switch {
			case wv.Kind == kindLocal:
				wv.Branch = pv.Branch
			case !isDir(wv.Dir):
				wv.Missing = true
			default:
				wv.Branch = gitBranch(wv.Dir)
			}
			for k := range wv.Tabs {
				wv.Tabs[k].Running = wv.Tabs[k].tab.running()
			}
		}
	}
	return views
}

// AddProject registers dir, or returns the project already registered for
// it. A folder inside a git repository registers the repository's top level.
// A new project starts with one local workspace holding one shell tab, plus
// any worktrees punk0 made for it before it was last removed.
func (r *Records) AddProject(dir string) (*Project, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if !isDir(dir) {
		return nil, fmt.Errorf("%s is not a directory", dir)
	}
	info := gitProbe(dir)
	if info.ok {
		dir = info.toplevel
	}
	// Without a .gitignore the index walks everything below: ~ took 648 MB.
	if home, _ := os.UserHomeDir(); dir == home || dir == filepath.Dir(dir) {
		return nil, fmt.Errorf("%s is too broad for a project: add a folder inside it", dir)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if i := slices.IndexFunc(r.projects, func(p *Project) bool { return p.Dir == dir }); i >= 0 {
		return r.projects[i], nil
	}
	p := &Project{ID: r.projectIDLocked(filepath.Base(dir)), Name: filepath.Base(dir), Dir: dir}
	name := p.Name
	if b := gitBranch(dir); b != "" && b != "HEAD" {
		name = b
	}
	p.Workspaces = append(p.Workspaces, newWorkspace(name, kindLocal, dir, ""))
	if info.ok {
		p.Workspaces = append(p.Workspaces, r.ownWorktrees(dir)...)
	}
	r.projects = append(r.projects, p)
	return p, r.saveLocked()
}

// RemoveProject stops the project's tabs and forgets it. Its folder and any
// worktrees stay on disk; adding the project again finds the worktrees.
func (r *Records) RemoveProject(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	i := slices.IndexFunc(r.projects, func(p *Project) bool { return p.ID == id })
	if i < 0 {
		return fmt.Errorf("project %q: %w", id, errNotFound)
	}
	var tabs []*Satellite
	for _, w := range r.projects[i].Workspaces {
		tabs = append(tabs, w.Tabs...)
	}
	closeAll(tabs)
	r.projects = slices.Delete(r.projects, i, i+1)
	return r.saveLocked()
}

// AddWorkspace adds a feature to a project with one shell tab. A local
// workspace runs in the project folder. A worktree workspace gets its own
// worktree under home/worktrees on branch name, which is created from base
// (HEAD when empty) unless it already exists.
func (r *Records) AddWorkspace(projectID, name string, worktree bool, base string) (*Workspace, error) {
	name = strings.TrimSpace(name)
	if worktree && !dirName.MatchString(name) {
		return nil, fmt.Errorf("invalid name %q: use letters, digits, '.', '_' and '-'", name)
	}
	if !worktree && (name == "" || len(name) > 80 || strings.IndexFunc(name, unicode.IsControl) >= 0) {
		return nil, fmt.Errorf("invalid name %q", name)
	}
	if strings.HasPrefix(base, "-") {
		return nil, fmt.Errorf("invalid base %q", base)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.projectLocked(projectID)
	if p == nil {
		return nil, fmt.Errorf("project %q: %w", projectID, errNotFound)
	}
	if slices.ContainsFunc(p.Workspaces, func(w *Workspace) bool { return w.Name == name }) {
		return nil, fmt.Errorf("workspace %q already exists", name)
	}
	var w *Workspace
	if worktree {
		if !gitAvailable(p.Dir) {
			return nil, errors.New("worktrees need a git repository")
		}
		dir := filepath.Join(r.worktrees, p.ID, name)
		if err := addWorktree(p.Dir, dir, name, base); err != nil {
			return nil, err
		}
		w = newWorkspace(name, kindWorktree, dir, name)
	} else {
		w = newWorkspace(name, kindLocal, p.Dir, "")
	}
	p.Workspaces = append(p.Workspaces, w)
	return w, r.saveLocked()
}

// RemoveWorkspace stops the workspace's tabs and forgets it. A local
// workspace leaves the project folder alone. A worktree workspace also
// removes its worktree, but answers errDirty while it has uncommitted
// changes unless force is set; its branch is always kept.
func (r *Records) RemoveWorkspace(id string, force bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, i := r.workspaceLocked(id)
	if p == nil {
		return fmt.Errorf("workspace %q: %w", id, errNotFound)
	}
	w := p.Workspaces[i]
	exists := isDir(w.Dir)
	if w.Kind == kindWorktree {
		// Only folders punk0 created are ever removed.
		if !strings.HasPrefix(w.Dir, r.worktrees+string(filepath.Separator)) {
			return fmt.Errorf("%s is not a punk0 worktree", w.Dir)
		}
		// Checked before stopping anything, so a refused delete leaves the
		// agents running.
		if exists && !force {
			out, err := exec.Command("git", "-C", w.Dir, "status", "--porcelain").Output()
			if err != nil {
				return fmt.Errorf("git status: %w", err)
			}
			if len(out) > 0 {
				return errDirty
			}
		}
	}
	closeAll(w.Tabs)
	if w.Kind == kindWorktree {
		args := []string{"-C", p.Dir, "worktree", "prune"}
		if exists {
			args = []string{"-C", p.Dir, "worktree", "remove", w.Dir}
			if force {
				args = append(args, "--force")
			}
		}
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			return gitError(out, err)
		}
	}
	p.Workspaces = slices.Delete(p.Workspaces, i, i+1)
	return r.saveLocked()
}

// AddTab opens a new tab in a workspace. A claude tab gets the session id its
// conversation will be saved under.
func (r *Records) AddTab(workspaceID, kind string) (*Satellite, error) {
	if kind != tabShell && kind != tabClaude {
		return nil, fmt.Errorf("invalid tab kind %q", kind)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p, i := r.workspaceLocked(workspaceID)
	if p == nil {
		return nil, fmt.Errorf("workspace %q: %w", workspaceID, errNotFound)
	}
	w := p.Workspaces[i]
	t := newTab(kind, w.Dir)
	w.Tabs = append(w.Tabs, t)
	return t, r.saveLocked()
}

// RenameTab names a tab after what it is for, e.g. "review".
func (r *Records) RenameTab(id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 40 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return fmt.Errorf("invalid name %q", name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	w, i := r.tabLocked(id)
	if w == nil {
		return fmt.Errorf("tab %q: %w", id, errNotFound)
	}
	w.Tabs[i].Name = name
	return r.saveLocked()
}

// RemoveTab stops the tab's shell and forgets it.
func (r *Records) RemoveTab(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, i := r.tabLocked(id)
	if w == nil {
		return fmt.Errorf("tab %q: %w", id, errNotFound)
	}
	w.Tabs[i].close()
	w.Tabs = slices.Delete(w.Tabs, i, i+1)
	return r.saveLocked()
}

func (r *Records) Workspace(id string) *Workspace {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, i := r.workspaceLocked(id); p != nil {
		return p.Workspaces[i]
	}
	return nil
}

func (r *Records) Tab(id string) *Satellite {
	r.mu.Lock()
	defer r.mu.Unlock()
	if w, i := r.tabLocked(id); w != nil {
		return w.Tabs[i]
	}
	return nil
}

// Close stops every shell.
func (r *Records) Close() {
	r.mu.Lock()
	var tabs []*Satellite
	for _, p := range r.projects {
		for _, w := range p.Workspaces {
			tabs = append(tabs, w.Tabs...)
		}
	}
	r.mu.Unlock()
	closeAll(tabs)
}

func (r *Records) projectLocked(id string) *Project {
	if i := slices.IndexFunc(r.projects, func(p *Project) bool { return p.ID == id }); i >= 0 {
		return r.projects[i]
	}
	return nil
}

func (r *Records) workspaceLocked(id string) (*Project, int) {
	for _, p := range r.projects {
		if i := slices.IndexFunc(p.Workspaces, func(w *Workspace) bool { return w.ID == id }); i >= 0 {
			return p, i
		}
	}
	return nil, -1
}

func (r *Records) tabLocked(id string) (*Workspace, int) {
	for _, p := range r.projects {
		for _, w := range p.Workspaces {
			if i := slices.IndexFunc(w.Tabs, func(t *Satellite) bool { return t.ID == id }); i >= 0 {
				return w, i
			}
		}
	}
	return nil, -1
}

// projectIDLocked turns name into a readable id, unique among registered
// projects. It names the project's folder under home/worktrees.
func (r *Records) projectIDLocked(name string) string {
	base := strings.Map(func(c rune) rune {
		if c < 128 && (unicode.IsLetter(c) || unicode.IsDigit(c) || c == '.' || c == '_' || c == '-') {
			return c
		}
		return '-'
	}, name)
	base = strings.TrimLeft(base, ".-_")
	if base == "" {
		base = "project"
	}
	id := base
	for n := 2; r.projectLocked(id) != nil; n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	return id
}

// ownWorktrees returns a workspace for every worktree of the repository at
// dir that lives under home/worktrees.
func (r *Records) ownWorktrees(dir string) []*Workspace {
	out, err := exec.Command("git", "-C", dir, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return nil
	}
	prefix := r.worktrees + string(filepath.Separator)
	var ws []*Workspace
	var cur *Workspace
	for _, line := range strings.Split(string(out), "\n") {
		if path, ok := strings.CutPrefix(line, "worktree "); ok {
			cur = nil
			if strings.HasPrefix(path, prefix) {
				cur = newWorkspace(filepath.Base(path), kindWorktree, path, "")
				ws = append(ws, cur)
			}
		} else if b, ok := strings.CutPrefix(line, "branch refs/heads/"); ok && cur != nil {
			cur.Branch = b
		}
	}
	return ws
}

func addWorktree(repo, dir, branch, base string) error {
	if out, err := exec.Command("git", "-C", repo, "check-ref-format", "--branch", branch).CombinedOutput(); err != nil {
		return gitError(out, err)
	}
	args := []string{"-C", repo, "worktree", "add"}
	if exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch).Run() == nil {
		args = append(args, dir, branch)
	} else {
		args = append(args, "-b", branch, dir)
		if base != "" {
			args = append(args, base)
		}
	}
	if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
		return gitError(out, err)
	}
	return nil
}

func newWorkspace(name, kind, dir, branch string) *Workspace {
	return &Workspace{ID: newID(), Name: name, Kind: kind, Dir: dir, Branch: branch, Tabs: []*Satellite{newTab(tabShell, dir)}}
}

func newTab(kind, dir string) *Satellite {
	t := &Satellite{ID: newID(), Kind: kind, Name: kind, dir: dir}
	if kind == tabClaude {
		t.Session = newUUID()
	}
	return t
}

// closeAll stops shells in parallel, so it waits for the slowest hangup
// instead of their sum.
func closeAll(tabs []*Satellite) {
	var wg sync.WaitGroup
	for _, t := range tabs {
		wg.Go(t.close)
	}
	wg.Wait()
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// newUUID returns a random (version 4) UUID, the form claude --session-id takes.
func newUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func gitBranch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitError prefers git's own message, which says why (branch checked out
// elsewhere, invalid name), over the bare exit status.
func gitError(out []byte, err error) error {
	if msg := strings.TrimSpace(string(out)); msg != "" {
		return errors.New(strings.TrimPrefix(msg, "fatal: "))
	}
	return err
}
