package main

// Punk Records is what the orchestrator page lists: every project, workspace
// and tab, saved to records.json in the state directory on each change.
//
// A project is a folder the user registered; its workspaces are the ways to
// work in it. Local workspaces share the project folder, so they also share
// the one punk0 child that serves its files (see workbench.go); a worktree
// workspace owns exactly one git worktree under <state>/worktrees, so deleting
// it can never pull files from under another.
//
// Running shells are never saved: after a restart every tab is listed stopped
// and starts again on attach, a claude tab resuming its session.

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
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"px0/internal/settings"
)

// Workspace kinds. A folder workspace is a scratch chat in its own folder
// under <state>/scratch.
const (
	kindLocal    = "local"
	kindWorktree = "worktree"
	kindFolder   = "folder"
)

// Tab kinds.
const (
	tabShell  = "shell"
	tabClaude = "claude"
)

// dirName is a folder name punk0 makes under its state directory. The
// leading letter or digit rules out ".", ".." and option-like names.
var dirName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

var (
	errNotFound = errors.New("not found")
	errDirty    = errors.New("worktree has uncommitted changes")
)

// Project is a folder the user registered: a git repository's top level or a
// plain folder. The one Scratch project holds chats that belong to no
// project; it is always listed and cannot be removed. Groups is Scratch's
// only: the groups its chats can be filed under.
type Project struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	Dir        string       `json:"dir"`
	Scratch    bool         `json:"scratch,omitempty"`
	Groups     []ChatGroup  `json:"groups,omitempty"`
	Workspaces []*Workspace `json:"workspaces"`
}

// Workspace is one feature: the tabs running in Dir, and the files the editor
// view shows, which come from the punk0 child serving that folder. Title,
// Group, Pinned, Archived, Active and Created are a scratch chat's only
// (scratch.go).
type Workspace struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Kind     string       `json:"kind"`
	Dir      string       `json:"dir"`
	Branch   string       `json:"branch,omitempty"`   // worktree kind: shown when the folder is gone
	Title    string       `json:"title,omitempty"`    // the chat's label; Name stays its folder's name
	Group    string       `json:"group,omitempty"`    // ChatGroup.ID, "" when in none
	Pinned   bool         `json:"pinned,omitempty"`   // listed at the top, above the groups
	Archived bool         `json:"archived,omitempty"` // tabs stopped and hidden from the list; folder kept
	Active   int64        `json:"active,omitempty"`   // unix ms of the last prompt, else of creation
	Created  int64        `json:"created,omitempty"`  // unix ms the chat was made
	Tabs     []*Satellite `json:"tabs"`
}

// Satellite is one tab: a shell in its workspace's folder, started on first
// attach so a stopped tab costs no process. Kind is "shell" or the agent the
// shell runs first (tabagents.go).
type Satellite struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Session string `json:"session,omitempty"` // claude --session-id, so the chat can be resumed

	dir     string
	hook    string // PUNK0_HOOK_URL the agent's hooks report to
	newChat bool   // an agent tab made this run whose session does not exist yet
	mu      sync.Mutex
	sh      *Shell
	status  string // last hook status: "", "working", "waiting", "idle"
}

// Shell returns the tab's running shell, starting one if there is none or the
// last one exited. An agent tab's shell runs the agent first: a new session
// on the tab's first start, else a resume (agentCommand).
func (s *Satellite) Shell() (*Shell, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sh == nil || s.sh.Exited() {
		var env []string
		if s.hook != "" {
			env = append(env, "PUNK0_HOOK_URL="+s.hook)
		}
		sh, err := startShell(s.dir, agentCommand(s.Kind, s.Session, s.newChat), env)
		if err != nil {
			return nil, err
		}
		s.sh, s.newChat = sh, false
	}
	return s.sh, nil
}

func (s *Satellite) running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sh != nil && !s.sh.Exited()
}

// shell returns the running shell without starting one.
func (s *Satellite) shell() *Shell {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sh != nil && !s.sh.Exited() {
		return s.sh
	}
	return nil
}

// Status is what the agent's hooks last reported; a tab with no shell has
// nothing to say.
func (s *Satellite) Status() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sh == nil || s.sh.Exited() {
		return ""
	}
	return s.status
}

func (s *Satellite) setStatus(status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}

// view is one tab as the page sees it.
func (s *Satellite) view() tabView {
	return tabView{ID: s.ID, Kind: s.Kind, Name: s.Name, Running: s.running(), Status: s.Status(), tab: s}
}

func (s *Satellite) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sh != nil {
		s.sh.Close()
		s.sh = nil
	}
	s.status = ""
}

// Records holds the tree and the folders language servers are allowed in.
type Records struct {
	home      string // <state>: XDG_STATE_HOME/punk0 or ~/.punk0
	worktrees string // home/worktrees, symlinks resolved to match git's paths

	// mu guards projects, lsp, creating and the file. git never runs under
	// it (newworkspace.go).
	mu       sync.Mutex
	projects []*Project
	removed  []RemovedProject // newest first, at most maxRemoved
	lsp      map[string]bool  // folder -> language servers wanted there
	hookBase string           // this punk0's loopback URL, for PUNK0_HOOK_URL
	creating map[string]bool  // folders and names of workspaces whose git is running
}

// RemovedProject is a project the user removed, offered again by the Add
// project panel: adding it back finds the worktrees punk0 made for it.
type RemovedProject struct {
	Name    string `json:"name"`
	Dir     string `json:"dir"`
	Removed int64  `json:"removed"` // unix ms
}

const maxRemoved = 10

// recordsFile is what records.json holds.
type recordsFile struct {
	Projects []*Project       `json:"projects"`
	Removed  []RemovedProject `json:"removed,omitempty"`
	LSP      map[string]bool  `json:"lsp,omitempty"`
}

// OpenRecords loads <state>/records.json; a missing file is an empty list. A
// file that does not parse is an error rather than an empty list, so a later
// save cannot overwrite it.
func OpenRecords(home string) (*Records, error) {
	for _, dir := range []string{"worktrees", "scratch"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o700); err != nil {
			return nil, err
		}
	}
	wt, err := filepath.EvalSymlinks(filepath.Join(home, "worktrees"))
	if err != nil {
		return nil, err
	}
	r := &Records{home: home, worktrees: wt, lsp: map[string]bool{}, creating: map[string]bool{}}
	var f recordsFile
	data, err := os.ReadFile(r.file())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, fmt.Errorf("%s: %w", r.file(), err)
		}
	}
	r.projects, r.removed = f.Projects, f.Removed
	for k, v := range f.LSP {
		r.lsp[k] = v
	}
	for _, p := range r.projects {
		for _, w := range p.Workspaces {
			r.adoptLocked(w)
		}
	}
	if !slices.ContainsFunc(r.projects, func(p *Project) bool { return p.Scratch }) {
		r.projects = append(r.projects, &Project{ID: r.projectIDLocked("scratch"), Name: "Scratch", Dir: filepath.Join(home, "scratch"), Scratch: true})
	}
	return r, nil
}

func (r *Records) file() string { return filepath.Join(r.home, "records.json") }

// SetHookBase tells the tabs where to report agent hooks: the loopback URL
// this punk0 serves on, with its base path.
func (r *Records) SetHookBase(base string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hookBase = base
	for _, p := range r.projects {
		for _, w := range p.Workspaces {
			r.adoptLocked(w)
		}
	}
}

// adoptLocked fills in what a workspace's tabs need but records.json does not
// hold: where they run and where their hooks report to.
func (r *Records) adoptLocked(w *Workspace) {
	for _, t := range w.Tabs {
		t.dir = w.Dir
		if r.hookBase != "" {
			t.hook = r.hookBase + "api/tabs/" + t.ID + "/hook"
		}
	}
}

func (r *Records) saveLocked() error {
	return writeFileAtomic(r.file(), recordsFile{Projects: r.projects, Removed: r.removed, LSP: r.lsp})
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
	Href    string    `json:"href"`    // the editor view for this folder
	Backend string    `json:"backend"` // state of the child serving the folder
	LSP     bool      `json:"lsp"`
	Tabs    []tabView `json:"tabs"`
}

// tabView copies the tab's fields under the lock, since a rename may change
// Name while the response is being encoded.
type tabView struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Running bool   `json:"running"`
	Status  string `json:"status"`

	tab *Satellite
}

// List returns the tree with live state: current branches, worktree folders
// deleted outside punk0, and which tabs have a running shell. Backend and
// Href are filled in by the workbench, which owns the children.
func (r *Records) List() []projectView {
	r.mu.Lock()
	// Slices start non-nil so an empty project or workspace encodes as [].
	views := make([]projectView, len(r.projects))
	lsp := make(map[string]bool, len(r.lsp))
	for k, v := range r.lsp {
		lsp[k] = v
	}
	for i, p := range r.projects {
		// Copies, so a rename under the lock cannot race the encoder.
		pc := *p
		pc.Groups = slices.Clone(p.Groups)
		views[i].Project = &pc
		views[i].Workspaces = make([]workspaceView, len(p.Workspaces))
		for j, w := range p.Workspaces {
			wv := &views[i].Workspaces[j]
			wc := *w
			wv.Workspace, wv.Branch, wv.LSP = &wc, w.Branch, lsp[w.Dir]
			wv.Tabs = make([]tabView, len(w.Tabs))
			for k, t := range w.Tabs {
				wv.Tabs[k] = tabView{ID: t.ID, Kind: t.Kind, Name: t.Name, tab: t}
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
				wv.Tabs[k].Status = wv.Tabs[k].tab.Status()
			}
		}
	}
	return views
}

// View returns one workspace the way List does, for the answer to the request
// that made it. The workbench fills in Href and Backend.
func (r *Records) View(ws *Workspace) workspaceView {
	r.mu.Lock()
	wc := *ws
	v := workspaceView{Workspace: &wc, Branch: ws.Branch, LSP: r.lsp[ws.Dir]}
	tabs := slices.Clone(ws.Tabs)
	r.mu.Unlock()
	if !isDir(ws.Dir) {
		v.Missing = true
	} else if b := gitBranch(ws.Dir); b != "" {
		v.Branch = b
	}
	v.Tabs = make([]tabView, len(tabs))
	for i, t := range tabs {
		v.Tabs[i] = t.view()
	}
	return v
}

// Open is what `punk0 <dir>` and the Add project panel resolve to: the project
// and workspace a folder belongs to, registering the project when it is new.
// A folder punk0 made a worktree in is that worktree's workspace, even if the
// project was removed meanwhile.
func (r *Records) Open(dir string) (*Project, *Workspace, error) {
	dir, err := projectFolder(dir)
	if err != nil {
		return nil, nil, err
	}
	if p, w := r.byDir(dir); w != nil {
		return p, w, nil
	}
	if strings.HasPrefix(dir, r.worktrees+string(filepath.Separator)) {
		main := gitMainTree(dir)
		if main == "" {
			return nil, nil, fmt.Errorf("%s is not a punk0 worktree", dir)
		}
		if _, err := r.AddProject(main); err != nil {
			return nil, nil, err
		}
		if p, w := r.byDir(dir); w != nil {
			return p, w, nil
		}
		return nil, nil, fmt.Errorf("%s is not a punk0 worktree", dir)
	}
	p, err := r.AddProject(dir)
	if err != nil {
		return nil, nil, err
	}
	if _, w := r.byDir(dir); w != nil {
		return p, w, nil
	}
	return nil, nil, fmt.Errorf("project %q: %w", p.ID, errNotFound)
}

// byDir returns the first workspace that runs in dir.
func (r *Records) byDir(dir string) (*Project, *Workspace) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.projects {
		if i := slices.IndexFunc(p.Workspaces, func(w *Workspace) bool { return w.Dir == dir }); i >= 0 {
			return p, p.Workspaces[i]
		}
	}
	return nil, nil
}

// ProjectAt returns the id of the project registered for dir, or "".
func (r *Records) ProjectAt(dir string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i := slices.IndexFunc(r.projects, func(p *Project) bool { return p.Dir == dir }); i >= 0 {
		return r.projects[i].ID
	}
	return ""
}

// AddProject registers dir, or returns the project already registered for it.
// A folder inside a git repository registers the repository's top level. A
// new project starts with one local workspace, plus any worktrees punk0 made
// for it before it was last removed.
func (r *Records) AddProject(dir string) (*Project, error) {
	dir, err := projectFolder(dir)
	if err != nil {
		return nil, err
	}
	// A whole home folder is not a project: the child would index everything
	// under it, and every workspace in it would share one backend.
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
	if gitAvailable(dir) {
		p.Workspaces = append(p.Workspaces, r.ownWorktrees(dir)...)
	}
	for _, w := range p.Workspaces {
		r.adoptLocked(w)
	}
	r.projects = append(r.projects, p)
	r.forgetRemovedLocked(dir)
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
	if r.projects[i].Scratch {
		return errors.New("scratch cannot be removed")
	}
	var tabs []*Satellite
	for _, w := range r.projects[i].Workspaces {
		tabs = append(tabs, w.Tabs...)
	}
	closeAll(tabs)
	gone := r.projects[i]
	r.projects = slices.Delete(r.projects, i, i+1)
	r.forgetRemovedLocked(gone.Dir)
	r.removed = slices.Insert(r.removed, 0, RemovedProject{Name: gone.Name, Dir: gone.Dir, Removed: time.Now().UnixMilli()})
	if len(r.removed) > maxRemoved {
		r.removed = r.removed[:maxRemoved]
	}
	return r.saveLocked()
}

func (r *Records) forgetRemovedLocked(dir string) {
	r.removed = slices.DeleteFunc(r.removed, func(rp RemovedProject) bool { return rp.Dir == dir })
}

// removedView is a removed project as the Add project panel lists it.
type removedView struct {
	RemovedProject
	Worktrees int `json:"worktrees"` // punk0's worktrees that come back with it
}

// Removed lists the removed projects whose folders still exist, newest
// first, with how many of punk0's worktrees adding each back would restore.
func (r *Records) Removed() []removedView {
	r.mu.Lock()
	removed := slices.Clone(r.removed)
	r.mu.Unlock()
	out := []removedView{}
	for _, rp := range removed {
		if !isDir(rp.Dir) {
			continue
		}
		v := removedView{RemovedProject: rp}
		if gitAvailable(rp.Dir) {
			v.Worktrees = len(r.ownWorktrees(rp.Dir))
		}
		out = append(out, v)
	}
	return out
}

// RemoveWorkspace stops the workspace's tabs and forgets it; workspaces
// listed under it move to the top level. A local workspace leaves the project
// folder alone. A scratch chat's folder is deleted with it: the page offers
// that only for an archived chat. A worktree workspace also removes its
// worktree, but answers errDirty while it has uncommitted changes unless
// force is set. Its branch is kept unless workspace.deleteMergedBranch is on;
// then git deletes it if merged, and note says why when it did not.
func (r *Records) RemoveWorkspace(id string, force bool) (note string, err error) {
	var repo, branch string
	// Registered before the unlock, so it runs after it: git branch -d needs
	// no lock, and a failure only ever becomes the note.
	defer func() {
		if err == nil && branch != "" {
			note = deleteMergedBranch(repo, branch)
		}
	}()
	r.mu.Lock()
	defer r.mu.Unlock()
	p, i := r.workspaceLocked(id)
	if p == nil {
		return "", fmt.Errorf("workspace %q: %w", id, errNotFound)
	}
	w := p.Workspaces[i]
	exists := isDir(w.Dir)
	if w.Kind == kindWorktree {
		// Only folders punk0 created are ever removed.
		if !strings.HasPrefix(w.Dir, r.worktrees+string(filepath.Separator)) {
			return "", fmt.Errorf("%s is not a punk0 worktree", w.Dir)
		}
		// Checked before stopping anything, so a refused delete leaves the
		// agents running.
		if exists && !force {
			out, err := exec.Command("git", "-C", w.Dir, "status", "--porcelain").Output()
			if err != nil {
				return "", fmt.Errorf("git status: %w", err)
			}
			if len(out) > 0 {
				return "", errDirty
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
			return "", gitError(out, err)
		}
		if settings.SettingOr[bool]("workspace.deleteMergedBranch") {
			repo, branch = p.Dir, w.Branch
		}
	}
	// A chat's folder goes with it only when punk0 made it; a chat started
	// in a folder of yours leaves that folder alone.
	if w.Kind == kindFolder && strings.HasPrefix(w.Dir, p.Dir+string(filepath.Separator)) {
		if err := os.RemoveAll(w.Dir); err != nil {
			return "", err
		}
	}
	p.Workspaces = slices.Delete(p.Workspaces, i, i+1)
	return "", r.saveLocked()
}

// deleteMergedBranch runs git branch -d, which refuses an unmerged branch;
// any failure comes back as a note for the user, never an error.
func deleteMergedBranch(repo, branch string) string {
	if out, err := exec.Command("git", "-C", repo, "branch", "-d", branch).CombinedOutput(); err != nil {
		return fmt.Sprintf("Kept branch %s: %v", branch, gitError(out, err))
	}
	return ""
}

// AddTab opens a new tab in a workspace. A claude tab gets the session id its
// conversation will be saved under.
func (r *Records) AddTab(workspaceID, kind string) (*Satellite, error) {
	if _, agent := tabAgents[kind]; kind != tabShell && !agent {
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
	r.adoptLocked(w)
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

// tabPlace is where a tab runs, copied under the lock: what the turn ledger
// (ledger.go) files the tab's turns under, and the name its chat goes by.
type tabPlace struct {
	tabID, chat, workspaceID, kind, dir string
}

// Place returns where tab id runs; ok is false for an unknown tab. A chat goes
// by its workspace's name -- the feature it works on -- since agent tabs are
// named after their harness ("claude") until renamed. A renamed tab adds its
// name; one of several unrenamed agent tabs adds its harness and position.
func (r *Records) Place(id string) (place tabPlace, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, i := r.tabLocked(id)
	if w == nil {
		return tabPlace{}, false
	}
	t := w.Tabs[i]
	chat := w.Title
	if chat == "" {
		chat = w.Name
	}
	if t.Name != "" && t.Name != t.Kind {
		chat += " · " + t.Name
	} else {
		agents, at := 0, 0
		for _, o := range w.Tabs {
			if o.Kind == tabShell {
				continue
			}
			agents++
			if o == t {
				at = agents
			}
		}
		if agents > 1 {
			chat += " · " + t.Kind + " " + strconv.Itoa(at)
		}
	}
	return tabPlace{tabID: t.ID, chat: chat, workspaceID: w.ID, kind: w.Kind, dir: w.Dir}, true
}

// WorkspaceLabel is what workspace id goes by -- its title, else its name --
// or "" for an unknown one. The turn ledger groups a workspace's chats under
// it.
func (r *Records) WorkspaceLabel(id string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, i := r.workspaceLocked(id)
	if p == nil {
		return ""
	}
	if w := p.Workspaces[i]; w.Title != "" {
		return w.Title
	}
	return p.Workspaces[i].Name
}

// workspaceDir is a workspace's id and the folder it runs in.
type workspaceDir struct{ id, dir string }

// WorkspaceDirs lists project id's workspaces in order; ok is false for an
// unknown project.
func (r *Records) WorkspaceDirs(id string) (dirs []workspaceDir, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.projectLocked(id)
	if p == nil {
		return nil, false
	}
	for _, w := range p.Workspaces {
		dirs = append(dirs, workspaceDir{w.ID, w.Dir})
	}
	return dirs, true
}

// Tabs returns every tab, for the idle reaper and shutdown.
func (r *Records) Tabs() []*Satellite {
	r.mu.Lock()
	defer r.mu.Unlock()
	var tabs []*Satellite
	for _, p := range r.projects {
		for _, w := range p.Workspaces {
			tabs = append(tabs, w.Tabs...)
		}
	}
	return tabs
}

// runningWorkspace is a workspace and the shell pids of its running tabs,
// in tab order, for the resource view.
type runningWorkspace struct {
	id, dir string
	tabs    []runningTab
}

type runningTab struct {
	id  string
	pid int
}

// Running lists every workspace with the pids of its running tabs. Shells
// are asked outside r.mu, as everywhere else.
func (r *Records) Running() []runningWorkspace {
	r.mu.Lock()
	var all []*Workspace
	tabs := map[*Workspace][]*Satellite{}
	for _, p := range r.projects {
		for _, w := range p.Workspaces {
			all = append(all, w)
			tabs[w] = slices.Clone(w.Tabs)
		}
	}
	r.mu.Unlock()
	out := make([]runningWorkspace, 0, len(all))
	for _, w := range all {
		rw := runningWorkspace{id: w.ID, dir: w.Dir}
		for _, t := range tabs[w] {
			if sh := t.shell(); sh != nil {
				rw.tabs = append(rw.tabs, runningTab{t.ID, sh.Pid()})
			}
		}
		out = append(out, rw)
	}
	return out
}

// Folder returns the folder behind a /w/<id>/ workspace URL: the dir of any
// workspace whose backend id is id (local workspaces of one project share it).
func (r *Records) Folder(id string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.projects {
		if wbItemID("dir", p.Dir) == id {
			return p.Dir
		}
		for _, w := range p.Workspaces {
			if wbItemID("dir", w.Dir) == id {
				return w.Dir
			}
		}
	}
	return ""
}

// ProjectDirs lists the registered project folders, Scratch aside: the cheap
// first places to look for a pull request's clone.
func (r *Records) ProjectDirs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return projectDirs(r.projects)
}

// recordedProjectDirs reads the project folders from <state>/records.json
// without opening Records, for `punk0 <pr-url>`, which runs no orchestrator.
func recordedProjectDirs(home string) []string {
	var f recordsFile
	data, err := os.ReadFile(filepath.Join(home, "records.json"))
	if err != nil || json.Unmarshal(data, &f) != nil {
		return nil
	}
	return projectDirs(f.Projects)
}

func projectDirs(projects []*Project) []string {
	var dirs []string
	for _, p := range projects {
		if !p.Scratch {
			dirs = append(dirs, p.Dir)
		}
	}
	return dirs
}

// LSPOn reports whether language servers are wanted in dir. They are off by
// default: an orchestrator holds many folders open, and a language server per
// folder is gigabytes.
func (r *Records) LSPOn(dir string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lsp[dir]
}

// SetLSP records whether the child serving dir gets language servers. The
// caller stops the running child: the flag is read when one starts.
func (r *Records) SetLSP(dir string, on bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if on {
		r.lsp[dir] = true
	} else {
		delete(r.lsp, dir)
	}
	return r.saveLocked()
}

// Close stops every shell.
func (r *Records) Close() { closeAll(r.Tabs()) }

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
	for _, p := range r.projects {
		if i := slices.IndexFunc(p.Workspaces, func(w *Workspace) bool { return wbItemID("dir", w.Dir) == id }); i >= 0 {
			return p, i
		}
		if wbItemID("dir", p.Dir) == id && len(p.Workspaces) > 0 {
			return p, 0
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
// projects. It names the project's folder under <state>/worktrees.
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
// dir that lives under <state>/worktrees.
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

// newWorkspace opens empty, on the page's welcome view; a scratch chat gets
// its agent's tab from NewChat.
func newWorkspace(name, kind, dir, branch string) *Workspace {
	return &Workspace{ID: newID(), Name: name, Kind: kind, Dir: dir, Branch: branch, Tabs: []*Satellite{}}
}

// newTab makes a tab of kind. An agent tab starts a new session the first
// time it runs; a claude tab gets the session id its conversation will be
// saved under.
func newTab(kind, dir string) *Satellite {
	t := &Satellite{ID: newID(), Kind: kind, Name: kind, dir: dir}
	if a, ok := tabAgents[kind]; ok && kind != tabClaude {
		t.Name = a.Label
	}
	t.newChat = kind != tabShell
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
	var b [8]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// projectFolder turns a path into the folder a project is keyed on: absolute,
// symlinks resolved (git reports resolved paths), and the repository top
// level when it is inside one.
func projectFolder(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	if !isDir(abs) {
		return "", fmt.Errorf("%s is not a directory", abs)
	}
	if info := gitProbe(abs); info.ok && info.toplevel != "" {
		return info.toplevel, nil
	}
	return abs, nil
}

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// gitBranch is the branch checked out in dir ("HEAD" when detached), or "".
// A new repository's branch has no commit yet, which rev-parse cannot name.
func gitBranch(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		out, err = exec.Command("git", "-C", dir, "symbolic-ref", "--short", "-q", "HEAD").Output()
		if err != nil {
			return ""
		}
	}
	return strings.TrimSpace(string(out))
}

// gitMainTree returns the top level of the repository a worktree belongs to,
// which is how a folder under <state>/worktrees finds its project again.
func gitMainTree(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return ""
	}
	common := strings.TrimSpace(string(out))
	if common == "" {
		return ""
	}
	top := filepath.Dir(common)
	if !isDir(top) {
		return ""
	}
	return top
}

// gitError prefers git's own message, which says why (branch checked out
// elsewhere, invalid name), over the bare exit status.
func gitError(out []byte, err error) error {
	if msg := strings.TrimSpace(string(out)); msg != "" {
		return errors.New(strings.TrimPrefix(msg, "fatal: "))
	}
	return err
}
