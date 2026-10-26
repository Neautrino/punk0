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
	"cmp"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
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
	Owned      bool         `json:"owned,omitempty"` // punk0 made this folder, so removing the project may delete it
	Scratch    bool         `json:"scratch,omitempty"`
	Groups     []ChatGroup  `json:"groups,omitempty"`
	Workspaces []*Workspace `json:"workspaces"`
}

// Workspace is one feature: the tabs running in Dir, and the files the editor
// view shows, which come from the punk0 child serving that folder. A
// workspace is also the group its chats belong to (Records.chatWorkspaces).
// Parent is the workspace a worktree is listed under: the one whose agent made
// it (attribution.go), or where the user moved it. Title, Group, Pinned,
// Archived, Active and Created are a scratch chat's only (scratch.go).
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
	Adopted  bool         `json:"adopted,omitempty"`  // worktree kind: a folder punk0 did not make, used where it is
	Parent   string       `json:"parent,omitempty"`   // worktree kind: the workspace it is listed under, "" at the top
	Tabs     []*Satellite `json:"tabs"`
}

// Satellite is one tab: a shell in its workspace's folder, started on first
// attach so a stopped tab costs no process. Kind is "shell" or the agent the
// shell runs first (tabagents.go).
type Satellite struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Session string `json:"session,omitempty"` // the agent's chat id the tab resumes (agentCommand); claude's is its --session-id

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
		sh, err := startShell(s.dir, s.command(), env)
		if err != nil {
			return nil, err
		}
		s.sh, s.newChat = sh, false
	}
	return s.sh, nil
}

// command is the line the tab's next start runs. Callers hold s.mu or own s.
func (s *Satellite) command() string {
	return agentCommand(s.Kind, s.Session, s.newChat)
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
	mu           sync.Mutex
	projects     []*Project
	removed      []RemovedProject // newest first, at most maxRemoved
	lsp          map[string]bool  // folder -> language servers wanted there
	hookBase     string           // this punk0's loopback URL, for PUNK0_HOOK_URL
	creating     map[string]bool  // folders and names of workspaces whose git is running
	removedChats []string         // chats Chats mode leaves out, agent+":"+id
	// chatWorkspaces files each chat (agent+":"+id) under the workspace it
	// belongs to, past its tab's life: local workspaces share a folder, so the
	// folder alone cannot tell them apart.
	chatWorkspaces map[string]string

	chats *chatHistory // the agents' own chat history (history.go), read-only after this
}

// RemovedProject is a project the user removed, offered again by the Add
// project panel: adding it back finds the worktrees punk0 made for it.
type RemovedProject struct {
	Name    string `json:"name"`
	Dir     string `json:"dir"`
	Owned   bool   `json:"owned,omitempty"` // Project.Owned, so adding it back can still delete its folder
	Removed int64  `json:"removed"`         // unix ms
}

const maxRemoved = 10

// recordsFile is what records.json holds.
type recordsFile struct {
	Projects []*Project       `json:"projects"`
	Removed  []RemovedProject `json:"removed,omitempty"`
	LSP      map[string]bool  `json:"lsp,omitempty"`
	// RemovedChats keeps the JSON key hiddenChats: records.json files already
	// on disk hold it, and `removed` is the removed projects' key.
	RemovedChats   []string          `json:"hiddenChats,omitempty"`
	ChatWorkspaces map[string]string `json:"chatWorkspaces,omitempty"`
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
	r := &Records{home: home, worktrees: wt, lsp: map[string]bool{}, creating: map[string]bool{}, chats: newChatHistory()}
	// OpenCode lists the folder its CLI runs in, so it needs punk0's folders.
	r.chats.dirs = r.ChatFolders
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
	r.projects, r.removed, r.removedChats = f.Projects, f.Removed, f.RemovedChats
	r.chatWorkspaces = f.ChatWorkspaces
	if r.chatWorkspaces == nil {
		r.chatWorkspaces = map[string]string{}
	}
	for k, v := range f.LSP {
		r.lsp[k] = v
	}
	for _, p := range r.projects {
		for _, w := range p.Workspaces {
			r.adoptLocked(w)
			// Chats opened before membership was kept belong where their tab is.
			for _, t := range w.Tabs {
				if k := chatKey(t.Kind, t.Session); t.Session != "" && r.chatWorkspaces[k] == "" {
					r.chatWorkspaces[k] = w.ID
				}
			}
		}
	}
	if !slices.ContainsFunc(r.projects, func(p *Project) bool { return p.Scratch }) {
		r.projects = append(r.projects, &Project{ID: r.projectIDLocked("scratch"), Name: "Scratch", Dir: filepath.Join(home, "scratch"), Scratch: true})
	}
	return r, nil
}

func (r *Records) file() string { return filepath.Join(r.home, "records.json") }

// Chats is the agents' own chat history, read from their history folders
// (history.go). It is built once, so callers need no nil check.
func (r *Records) Chats() *chatHistory { return r.chats }

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
	return writeFileAtomic(r.file(), recordsFile{Projects: r.projects, Removed: r.removed, LSP: r.lsp, RemovedChats: r.removedChats, ChatWorkspaces: r.chatWorkspaces})
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
	// The workspace the tab is in, for a list that spans a folder's
	// workspaces (Records.TabsAt).
	Workspace     string `json:"workspace,omitempty"`
	WorkspaceName string `json:"workspaceName,omitempty"`

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
	// A folder punk0 made stays punk0's to delete across a removal and a
	// re-add: Recently removed offers it back as the project it was.
	if i := slices.IndexFunc(r.removed, func(rp RemovedProject) bool { return rp.Dir == dir }); i >= 0 {
		p.Owned = r.removed[i].Owned
	}
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

// SetOwned marks a project as one punk0 made itself, which is the only thing
// that lets removing it delete its folder. Create new project and a finished
// clone call it, for a folder they just made and nothing else.
func (r *Records) SetOwned(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p := r.projectLocked(id)
	if p == nil {
		return fmt.Errorf("project %q: %w", id, errNotFound)
	}
	p.Owned = true
	return r.saveLocked()
}

// RemoveProject stops the project's tabs and forgets it. Without o.Files its
// folder and its worktrees stay on disk and the Add project panel offers it
// again, since adding it back finds them. o.Files moves the folder to
// <state>/trash instead, which only a project punk0 made or cloned
// (Project.Owned) offers: it wants the project's name in o.Confirm, and is
// refused while the repository still has worktrees checked out or another
// registered folder sits inside, which the move would leave pointing at
// nothing. The agents' transcripts are never touched: a chat is deleted from
// Chats mode, one at a time (history.go).
func (r *Records) RemoveProject(id string, o RemoveOptions) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	i := slices.IndexFunc(r.projects, func(p *Project) bool { return p.ID == id })
	if i < 0 {
		return "", fmt.Errorf("project %q: %w", id, errNotFound)
	}
	p := r.projects[i]
	if p.Scratch {
		return "", errors.New("scratch cannot be removed")
	}
	// Refused before a single tab is stopped, so a project the user decides
	// against keeps its agents running. The move itself goes first too: a
	// folder that cannot move (another filesystem, permissions, a Windows
	// handle) leaves everything as it was. Shells running in it follow it to
	// the trash and are stopped right after.
	if o.Files {
		if err := r.trashableLocked(p, o.Confirm); err != nil {
			return "", err
		}
		if _, err := moveToTrash(r.home, p.Dir); err != nil {
			if errors.Is(err, syscall.EXDEV) {
				return "", fmt.Errorf("%s is on another filesystem than punk0's state folder, so it cannot be moved to the trash: delete it yourself", tildePath(p.Dir))
			}
			return "", err
		}
	}
	var tabs []*Satellite
	for _, w := range p.Workspaces {
		tabs = append(tabs, w.Tabs...)
	}
	closeAll(tabs)
	r.projects = slices.Delete(r.projects, i, i+1)
	for _, w := range p.Workspaces {
		r.refileLocked(w.ID, "")
	}
	r.forgetRemovedLocked(p.Dir)
	// A trashed project can never be added back, so it is not offered.
	if !o.Files {
		r.removed = slices.Insert(r.removed, 0, RemovedProject{Name: p.Name, Dir: p.Dir, Owned: p.Owned, Removed: time.Now().UnixMilli()})
		if len(r.removed) > maxRemoved {
			r.removed = r.removed[:maxRemoved]
		}
	}
	return "", r.saveLocked()
}

// workspaceDirs lists the folders a project's workspaces run in, each once.
func workspaceDirs(p *Project) []string {
	var dirs []string
	for _, w := range p.Workspaces {
		if !slices.Contains(dirs, w.Dir) {
			dirs = append(dirs, w.Dir)
		}
	}
	return dirs
}

// trashableLocked says why a project's folder must not be moved to the
// trash: punk0 did not make it, its name was not typed back, the repository
// still has worktrees, or another registered folder lives inside it — the
// last two would be moved along and left pointing at nothing. A prunable
// registration (its folder already gone) does not count, or a stale one
// would be impossible to get past.
func (r *Records) trashableLocked(p *Project, confirm string) error {
	if !p.Owned {
		return fmt.Errorf("punk0 did not create %s: remove the project and delete the folder yourself", tildePath(p.Dir))
	}
	if confirm != p.Name {
		return fmt.Errorf("type the project name %q to delete its folder", p.Name)
	}
	if !isDir(p.Dir) {
		return fmt.Errorf("%s is already gone: remove the project without deleting its folder", tildePath(p.Dir))
	}
	var inner []string
	for _, q := range r.projects {
		if q == p {
			continue
		}
		if inside(q.Dir, p.Dir) {
			inner = append(inner, tildePath(q.Dir))
			continue
		}
		for _, w := range q.Workspaces {
			if inside(w.Dir, p.Dir) {
				inner = append(inner, tildePath(w.Dir))
			}
		}
	}
	if len(inner) > 0 {
		return fmt.Errorf("%s holds other folders punk0 knows: %s. Remove those first", p.Name, strings.Join(inner, ", "))
	}
	if !gitAvailable(p.Dir) {
		return nil
	}
	wts, err := listWorktrees(p.Dir)
	if err != nil {
		return err
	}
	var out []string
	for _, wt := range wts {
		if !wt.Main && !wt.Prunable {
			out = append(out, tildePath(wt.Path))
		}
	}
	if len(out) > 0 {
		return fmt.Errorf("%s still has worktrees checked out: %s. Delete them first", p.Name, strings.Join(out, ", "))
	}
	return nil
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

// RemoveOptions is what a removal dialog answers with: Force deletes a
// worktree that has uncommitted changes, Files deletes folders as well as
// records, and Confirm is the project name typed back before its folder is
// moved to the trash.
type RemoveOptions struct {
	Force   bool
	Files   bool
	Confirm string
}

// RemoveWorkspace stops the workspace's tabs and forgets it; the worktrees
// listed under it and the chats filed under it move up to its parent (to the
// top, and to no workspace, when it has none). Without o.Files nothing on disk is
// touched: a worktree keeps its folder, its branch and its registration in
// the repository, so adding the project again finds it. With o.Files the
// worktree is removed with git, never with os.RemoveAll, and only once
// listWorktrees agrees the folder is a linked worktree of this project,
// which is what makes an adopted folder safe to remove and the project's own
// checkout impossible to; it answers errDirty while the worktree has
// uncommitted changes unless o.Force is set. The branch is kept unless
// workspace.deleteMergedBranch is on; then git deletes it if merged, and
// note says why when it did not. A local workspace never touches the project
// folder, and a scratch chat's folder goes with it exactly when punk0 made
// it, whatever o.Files says. The agents' transcripts are never touched: a
// removed workspace's chats stay on disk and keep being listed (history.go).
func (r *Records) RemoveWorkspace(id string, o RemoveOptions) (note string, err error) {
	var repo, branch string
	// Registered before the unlock, so it runs after it: git branch -d needs
	// no lock, and its failing only ever becomes the note.
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
	worktree, exists := w.Kind == kindWorktree && o.Files, isDir(w.Dir)
	// Everything that can refuse runs before a single tab is stopped, so a
	// refused delete leaves the agents running.
	if worktree && exists {
		if err := linkedWorktree(p.Dir, w.Dir); err != nil {
			return "", err
		}
		if !o.Force {
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
	if worktree {
		args := []string{"-C", p.Dir, "worktree", "prune"}
		if exists {
			args = []string{"-C", p.Dir, "worktree", "remove", w.Dir}
			if o.Force {
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
	// What was under it moves up a level: its worktrees, and its chats.
	for _, c := range p.Workspaces {
		if c.Parent == w.ID {
			c.Parent = w.Parent
		}
	}
	r.refileLocked(w.ID, w.Parent)
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

// linkedWorktree answers nil only while dir is one of repo's linked
// worktrees that git will remove. Adopted worktrees live anywhere, so where
// a folder sits says nothing about whose it is; git's own list does, and it
// is also what keeps a stale record from running worktree remove on the
// main checkout. Prunable and locked registrations are refused here rather
// than by git later (worktree remove refuses a locked one even with
// --force), so the tabs are not stopped for a removal that cannot happen.
func linkedWorktree(repo, dir string) error {
	wts, err := listWorktrees(repo)
	if err != nil {
		return err
	}
	wt := worktreeAt(wts, dir)
	switch {
	case wt == nil:
		return fmt.Errorf("%s is not a worktree of %s", dir, repo)
	case wt.Main:
		return fmt.Errorf("%s is the project's own checkout, not a worktree", dir)
	case wt.Prunable:
		return fmt.Errorf("%s is no longer a worktree of %s: git lists it as prunable", dir, repo)
	case wt.Locked:
		return fmt.Errorf("%s is locked (git worktree unlock to allow removing it)", dir)
	}
	return nil
}

// moveToTrash moves path into <state>/trash, stamped with the time and, if
// that name is taken, numbered, and returns where it went. A project folder
// punk0 deletes for the user is never removed outright: a rename can be
// undone by hand, RemoveAll cannot. A trash folder on another filesystem
// fails here rather than copying.
func moveToTrash(state, path string) (string, error) {
	trash := filepath.Join(state, "trash")
	if err := os.MkdirAll(trash, 0o700); err != nil {
		return "", err
	}
	base := fmt.Sprintf("%d-%s", time.Now().UnixMilli(), filepath.Base(path))
	dest := filepath.Join(trash, base)
	for i := 1; ; i++ {
		if _, err := os.Lstat(dest); errors.Is(err, fs.ErrNotExist) {
			break
		}
		dest = filepath.Join(trash, fmt.Sprintf("%s.%d", base, i))
	}
	if err := os.Rename(path, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// validSession is what an agent's chat id may hold: it goes into the tab's
// shell command line and into codex/opencode argument lists, and every
// agent's ids (UUIDs, ses_…) fit it. It cannot start with '-', or an id read
// from a transcript on disk could pass itself off as a flag.
var validSession = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// AddTab opens a new tab in a workspace. session "" starts a new chat (a
// claude tab gets the session id it will be saved under); else the tab
// resumes that chat of the agent's from its first start, so the agent must be
// able to resume by id.
func (r *Records) AddTab(workspaceID, kind, session string) (*Satellite, error) {
	a, agent := tabAgents[kind]
	if kind != tabShell && !agent {
		return nil, fmt.Errorf("invalid tab kind %q", kind)
	}
	if session != "" && (!agent || (kind != tabClaude && a.resumeID == nil) || !validSession.MatchString(session)) {
		return nil, fmt.Errorf("invalid session %q for a %s tab", session, kind)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p, i := r.workspaceLocked(workspaceID)
	if p == nil {
		return nil, fmt.Errorf("workspace %q: %w", workspaceID, errNotFound)
	}
	w := p.Workspaces[i]
	t := newTab(kind, w.Dir, session)
	w.Tabs = append(w.Tabs, t)
	r.adoptLocked(w)
	r.fileTabLocked(w, t)
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

// WorkspaceAt is Workspace for an editor's terminal and Chats mode, which
// know only their folder's backend id (/w/<id>/): that names the folder's
// first workspace. Every other route takes a workspace id only.
func (r *Records) WorkspaceAt(id string) *Workspace {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, i := r.workspaceLocked(id); p != nil {
		return p.Workspaces[i]
	}
	for _, p := range r.projects {
		if i := slices.IndexFunc(p.Workspaces, func(w *Workspace) bool { return wbItemID("dir", w.Dir) == id }); i >= 0 {
			return p.Workspaces[i]
		}
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

// RemoveChat takes one of the agents' chats out of Chats mode, or lists it
// again. Only punk0's list changes: the conversation is the agent's, stays on
// disk, and its tabs keep running.
func (r *Records) RemoveChat(agent, id string, removed bool) error {
	key := chatKey(agent, id)
	r.mu.Lock()
	defer r.mu.Unlock()
	has := slices.Contains(r.removedChats, key)
	switch {
	case removed && !has:
		r.removedChats = append(r.removedChats, key)
	case !removed && has:
		r.removedChats = slices.DeleteFunc(r.removedChats, func(k string) bool { return k == key })
	default:
		return nil
	}
	return r.saveLocked()
}

// RemovedChats is the removed set, for one listing.
func (r *Records) RemovedChats() map[string]bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]bool, len(r.removedChats))
	for _, k := range r.removedChats {
		out[k] = true
	}
	return out
}

// RestoreAllChats lists every removed chat again and says how many came
// back. Sync does it in one go: removing a chat is punk0's own bookkeeping,
// so one click undoes all of it.
func (r *Records) RestoreAllChats() (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(r.removedChats)
	if n == 0 {
		return 0, nil
	}
	r.removedChats = nil
	return n, r.saveLocked()
}

// CloseSessionTabs stops and forgets every tab resuming one chat, wherever it
// is: deleting a conversation an agent still holds would have it written back
// on the next stop. Close waits until the shell's process group is reaped, so
// the delete that follows cannot race it.
func (r *Records) CloseSessionTabs(agent, id string) (int, error) {
	if agent == "" || id == "" {
		return 0, nil
	}
	closed := 0
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.projects {
		for _, w := range p.Workspaces {
			w.Tabs = slices.DeleteFunc(w.Tabs, func(t *Satellite) bool {
				if t.Kind != agent || t.Session != id {
					return false
				}
				t.close()
				closed++
				return true
			})
		}
	}
	if closed == 0 {
		return 0, nil
	}
	return closed, r.saveLocked()
}

// ChatFolders lists every folder punk0 knows a chat could have run in -- each
// project's own folder and each of its workspaces' -- once. OpenCode's CLI
// answers for the folder it is started in, so this is how wide its listing
// can be.
func (r *Records) ChatFolders() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, p := range r.projects {
		for _, dir := range append([]string{p.Dir}, workspaceDirs(p)...) {
			if dir != "" && !slices.Contains(out, dir) {
				out = append(out, dir)
			}
		}
	}
	return out
}

// SessionTab finds the tab that resumes an agent's chat: Open reuses it
// instead of opening a second tab on one conversation, and a permanent
// delete checks none is left after CloseSessionTabs.
func (r *Records) SessionTab(agent, session string) (tabID, workspaceID string, running bool) {
	if session == "" {
		return "", "", false
	}
	r.mu.Lock()
	var t *Satellite
found:
	for _, p := range r.projects {
		for _, w := range p.Workspaces {
			i := slices.IndexFunc(w.Tabs, func(s *Satellite) bool { return s.Kind == agent && s.Session == session })
			if i >= 0 {
				t, workspaceID = w.Tabs[i], w.ID
				break found
			}
		}
	}
	r.mu.Unlock()
	if t == nil {
		return "", "", false
	}
	// running takes the tab's own lock, never r.mu.
	return t.ID, workspaceID, t.running()
}

// TabSessions maps every conversation a tab resumes, by chatKey, to that
// tab, so one listing of the chat history can mark them all.
func (r *Records) TabSessions() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]string{}
	for _, p := range r.projects {
		for _, w := range p.Workspaces {
			for _, t := range w.Tabs {
				if t.Session != "" {
					out[chatKey(t.Kind, t.Session)] = t.ID
				}
			}
		}
	}
	return out
}

// ProjectDirsOf lists the folders of the project that holds workspace id --
// the project folder and every workspace's -- for Chats mode's Project
// scope.
func (r *Records) ProjectDirsOf(workspaceID string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, _ := r.workspaceLocked(workspaceID)
	if p == nil {
		return nil
	}
	dirs := make([]string, 0, len(p.Workspaces)+1)
	dirs = append(dirs, p.Dir)
	for _, w := range p.Workspaces {
		dirs = append(dirs, w.Dir)
	}
	return dirs
}

// WorkspaceByDir maps every workspace folder to its workspace, for
// workspaceIn: a chat says where punk0 would open it. Local workspaces share
// a folder; the first one wins, as elsewhere.
func (r *Records) WorkspaceByDir() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.workspaceDirsLocked()
}

// WorkspaceAround is the first workspace whose folder holds dir, with the
// name it goes by: a chat that ran in a subfolder of a workspace is in
// punk0's tree without being a folder punk0 can open a tab in.
func (r *Records) WorkspaceAround(dir string) (id, label string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, p := range r.projects {
		for _, w := range p.Workspaces {
			if inside(dir, w.Dir) {
				return w.ID, cmp.Or(w.Title, w.Name)
			}
		}
	}
	return "", ""
}

// ProjectFolders lists the registered projects as id and folder, Scratch
// aside: what a folder outside punk0 is matched against.
func (r *Records) ProjectFolders() []workspaceDir {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]workspaceDir, 0, len(r.projects))
	for _, p := range r.projects {
		if !p.Scratch {
			out = append(out, workspaceDir{p.ID, p.Dir})
		}
	}
	return out
}

// tabPlace is where a tab runs, copied under the lock.
type tabPlace struct {
	workspaceID, kind, dir string
}

// Place returns where tab id runs; ok is false for an unknown tab.
func (r *Records) Place(id string) (place tabPlace, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, _ := r.tabLocked(id)
	if w == nil {
		return tabPlace{}, false
	}
	return tabPlace{workspaceID: w.ID, kind: w.Kind, dir: w.Dir}, true
}

// workspaceDir is a workspace's id and the folder it runs in.
type workspaceDir struct{ id, dir string }

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

// runningWorkspace is a workspace and its running tabs, in tab order, for
// the resource view. name is what the panel calls it: a scratch chat's label,
// else "project / workspace", or the workspace alone when the two match.
type runningWorkspace struct {
	id, dir, name string
	tabs          []runningTab
}

type runningTab struct {
	id, name, kind, status string
	pid                    int
}

// Running lists every workspace with the pids of its running tabs. Shells
// and statuses are asked outside r.mu, as everywhere else.
func (r *Records) Running() []runningWorkspace {
	type tabCopy struct {
		t          *Satellite
		name, kind string
	}
	type wsCopy struct {
		rw   runningWorkspace
		tabs []tabCopy
	}
	r.mu.Lock()
	var all []wsCopy
	for _, p := range r.projects {
		for _, w := range p.Workspaces {
			name := w.Name
			switch {
			case p.Scratch:
				if w.Title != "" {
					name = w.Title
				}
			case p.Name != w.Name:
				name = p.Name + " / " + w.Name
			}
			c := wsCopy{rw: runningWorkspace{id: w.ID, dir: w.Dir, name: name}}
			for _, t := range w.Tabs {
				c.tabs = append(c.tabs, tabCopy{t, t.Name, t.Kind})
			}
			all = append(all, c)
		}
	}
	r.mu.Unlock()
	out := make([]runningWorkspace, 0, len(all))
	for _, c := range all {
		rw := c.rw
		for _, tc := range c.tabs {
			if sh := tc.t.shell(); sh != nil {
				rw.tabs = append(rw.tabs, runningTab{id: tc.t.ID, name: tc.name, kind: tc.kind, status: tc.t.Status(), pid: sh.Pid()})
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

// worktreeInfo is one worktree of a repository, as git lists it: the main
// checkout first, then the linked ones. Workspace is the punk0 workspace
// running in Path, filled in by Records.Worktrees.
type worktreeInfo struct {
	Path      string `json:"path"`
	Branch    string `json:"branch"` // "" when detached
	Head      string `json:"head"`
	Detached  bool   `json:"detached"`
	Locked    bool   `json:"locked"`
	Prunable  bool   `json:"prunable"` // its folder is gone; git worktree prune forgets it
	Main      bool   `json:"main"`
	Workspace string `json:"workspace"`
}

// listWorktrees parses `git worktree list --porcelain`: one "key value" line
// per field, a blank line ending each worktree. -z would keep a newline in a
// folder name, but needs git 2.36.
func listWorktrees(repo string) ([]worktreeInfo, error) {
	out, err := exec.Command("git", "-C", repo, "worktree", "list", "--porcelain").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, gitError(ee.Stderr, err)
		}
		return nil, err
	}
	var wts []worktreeInfo
	cur := -1
	for _, line := range strings.Split(string(out), "\n") {
		key, val, _ := strings.Cut(line, " ")
		switch {
		case line == "":
			cur = -1
		case key == "worktree":
			wts = append(wts, worktreeInfo{Path: val, Main: len(wts) == 0})
			cur = len(wts) - 1
		case cur < 0:
		case key == "HEAD":
			wts[cur].Head = val
		case key == "branch":
			wts[cur].Branch = strings.TrimPrefix(val, "refs/heads/")
		case key == "detached":
			wts[cur].Detached = true
		case key == "locked":
			wts[cur].Locked = true
		case key == "prunable":
			wts[cur].Prunable = true
		}
	}
	return wts, nil
}

// sameDir reports whether a and b name one folder: equal once cleaned, or
// once symlinks are resolved (git records worktree folders resolved).
func sameDir(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA != nil {
		ra = a
	}
	if errB != nil {
		rb = b
	}
	return ra == rb
}

// worktreeAt is the worktree in folder dir, or nil.
func worktreeAt(wts []worktreeInfo, dir string) *worktreeInfo {
	for i := range wts {
		if sameDir(wts[i].Path, dir) {
			return &wts[i]
		}
	}
	return nil
}

// ownWorktrees returns a workspace for every worktree of the repository at
// dir that lives under <state>/worktrees.
func (r *Records) ownWorktrees(dir string) []*Workspace {
	wts, _ := listWorktrees(dir)
	prefix := r.worktrees + string(filepath.Separator)
	var ws []*Workspace
	for _, wt := range wts {
		if strings.HasPrefix(wt.Path, prefix) {
			ws = append(ws, newWorkspace(filepath.Base(wt.Path), kindWorktree, wt.Path, wt.Branch))
		}
	}
	return ws
}

// workspaceDirsLocked maps each folder a workspace runs in, in any project,
// to the first such workspace.
func (r *Records) workspaceDirsLocked() map[string]string {
	dirs := map[string]string{}
	for _, p := range r.projects {
		for _, w := range p.Workspaces {
			if _, ok := dirs[w.Dir]; !ok {
				dirs[w.Dir] = w.ID
			}
		}
	}
	return dirs
}

// workspaceIn is the workspace of dirs (workspaceDirsLocked) running in dir,
// or "". The folders are walked in order, so two workspaces spelling one
// folder differently always answer with the same one.
func workspaceIn(dirs map[string]string, dir string) string {
	if id := dirs[dir]; id != "" {
		return id
	}
	for _, d := range slices.Sorted(maps.Keys(dirs)) {
		if sameDir(d, dir) {
			return dirs[d]
		}
	}
	return ""
}

// newWorkspace opens empty, on the page's welcome view; a scratch chat gets
// its agent's tab from NewChat.
func newWorkspace(name, kind, dir, branch string) *Workspace {
	return &Workspace{ID: newID(), Name: name, Kind: kind, Dir: dir, Branch: branch, Tabs: []*Satellite{}}
}

// newTab makes a tab of kind. With session "" an agent tab starts a new
// session the first time it runs, and a claude tab gets the session id its
// conversation will be saved under; with a session it resumes that chat.
func newTab(kind, dir, session string) *Satellite {
	t := &Satellite{ID: newID(), Kind: kind, Name: kind, dir: dir, Session: session}
	if a, ok := tabAgents[kind]; ok && kind != tabClaude {
		t.Name = a.Label
	}
	if session == "" {
		t.newChat = kind != tabShell
		if kind == tabClaude {
			t.Session = newUUID()
		}
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
