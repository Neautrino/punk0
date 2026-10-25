package main

// Agents make git worktrees on their own: Claude Code's EnterWorktree and
// --worktree, its subagents with isolation: worktree, or plain git. A hook
// event says where the agent works (cwd, which follows it into a worktree,
// and the file an edit tool wrote), so a worktree of the tab's project that no
// workspace runs in becomes one, listed under the tab's workspace. When such a
// worktree's folder is gone, it goes: the workspace is forgotten, git prunes
// it, and its chats are hidden under the workspace above it, where Chats
// mode's Show removed finds them again.

import (
	"context"
	"encoding/json"
	"log"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

// worktreeAttribution is the workbench's memory of the folders hooks named
// and of the worktrees found gone, so a busy agent costs a few stats per
// event, and git only for a folder not seen before.
type worktreeAttribution struct {
	mu   sync.Mutex
	seen map[string]bool      // tab id + "\x00" + folder, already looked at
	busy map[string]bool      // worktree folders being adopted
	miss map[string]time.Time // folders that are no worktree of the tab's project
	gone map[string]bool      // workspaces whose folder was gone on the last pass
}

const (
	// attrMissFor is how long a folder found to be no worktree stays so:
	// git can make one there any time, but not on every hook event.
	attrMissFor = 30 * time.Second
	// reapWorktreesEvery is how often agent-made worktrees are checked for.
	reapWorktreesEvery = 5 * time.Second
)

// hookDirs are the folders a hook event says the agent works in.
func hookDirs(ev hookEvent) []string {
	var dirs []string
	if filepath.IsAbs(ev.Cwd) {
		dirs = append(dirs, ev.Cwd)
	}
	if len(ev.ToolInput) > 0 {
		var in struct {
			FilePath     string `json:"file_path"`
			NotebookPath string `json:"notebook_path"`
		}
		if json.Unmarshal(ev.ToolInput, &in) == nil {
			for _, p := range []string{in.FilePath, in.NotebookPath} {
				if p != "" && !filepath.IsAbs(p) && filepath.IsAbs(ev.Cwd) {
					p = filepath.Join(ev.Cwd, p)
				}
				if filepath.IsAbs(p) {
					dirs = append(dirs, filepath.Dir(p))
				}
			}
		}
	}
	return dirs
}

// noteAgentDirs looks at the folders of one hook event off the request: the
// agent is waiting for the hook's answer.
func (wb *workbench) noteAgentDirs(tabID string, ev hookEvent) {
	var fresh []string
	wb.attr.mu.Lock()
	if wb.attr.seen == nil {
		wb.attr.seen = map[string]bool{}
	}
	for _, d := range hookDirs(ev) {
		if k := tabID + "\x00" + d; !wb.attr.seen[k] {
			wb.attr.seen[k] = true
			fresh = append(fresh, d)
		}
	}
	wb.attr.mu.Unlock()
	if len(fresh) == 0 {
		return
	}
	go func() {
		for _, d := range fresh {
			wb.attributeDir(tabID, d)
		}
	}()
}

// attributeDir makes the worktree holding dir a workspace listed under the
// tab's workspace, when it is a linked worktree of the tab's project that no
// workspace runs in yet. A seen folder is looked at again only after a miss
// has expired, so a folder that becomes a worktree later is still caught.
func (wb *workbench) attributeDir(tabID, dir string) {
	place, ok := wb.records.Place(tabID)
	if !ok || place.kind == kindFolder {
		return // scratch chats belong to no project's repository
	}
	top := worktreeTop(dir)
	if top == "" || sameDir(top, place.dir) || workspaceIn(wb.records.WorkspaceByDir(), top) != "" {
		return
	}
	a := &wb.attr
	a.mu.Lock()
	if a.busy == nil {
		a.busy, a.miss = map[string]bool{}, map[string]time.Time{}
	}
	if a.busy[top] || time.Since(a.miss[top]) < attrMissFor {
		a.mu.Unlock()
		return
	}
	a.busy[top] = true
	a.mu.Unlock()
	missed := true
	defer func() {
		a.mu.Lock()
		delete(a.busy, top)
		if missed {
			a.miss[top] = time.Now()
			// Looked at again once the miss expires.
			delete(a.seen, tabID+"\x00"+dir)
		}
		a.mu.Unlock()
	}()

	projectID, _ := wb.records.workspaceProject(place.workspaceID)
	repo := wb.records.ProjectDir(projectID)
	if repo == "" {
		return
	}
	wts, err := listWorktrees(repo)
	if err != nil {
		return
	}
	wt := worktreeAt(wts, top)
	if wt == nil || wt.Main || wt.Prunable {
		return
	}
	ws, _, err := wb.records.AddWorkspace(projectID, WorkspaceOptions{Name: wt.Path, Worktree: true, Parent: place.workspaceID})
	if err != nil {
		log.Printf("worktree %s of tab %s: %v", tildePath(wt.Path), tabID, err)
		return
	}
	missed = false
	log.Printf("worktree %s listed under its agent's workspace as %q", tildePath(wt.Path), ws.Name)
}

// worktreeTop is the top of the linked worktree holding dir, or "": the
// nearest folder up from dir with a .git entry, when that entry is a file. A
// .git folder is a repository's own checkout. A submodule's .git is a file
// too; git's worktree list, checked next, tells the two apart.
func worktreeTop(dir string) string {
	for d := filepath.Clean(dir); ; {
		if fi, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			if fi.Mode().IsRegular() {
				return d
			}
			return ""
		}
		up := filepath.Dir(d)
		if up == d {
			return ""
		}
		d = up
	}
}

// reapWorktrees forgets agent-made worktrees whose folders are gone, every
// reapWorktreesEvery.
func (wb *workbench) reapWorktrees() {
	tick := time.NewTicker(reapWorktreesEvery)
	defer tick.Stop()
	for range tick.C {
		wb.reapGoneWorktrees()
	}
}

// reapGoneWorktrees forgets each worktree workspace punk0 adopted under
// another (Parent and Adopted: an agent's, or one moved there) whose folder
// is gone and that git no longer lists as live, two passes in a row: a folder
// can be missing for a moment while it is recreated. A project folder git
// cannot read says nothing, so its worktrees wait. Worktrees punk0 made
// itself, and ones at the top, keep their missing row as before.
func (wb *workbench) reapGoneWorktrees() {
	lists := map[string][]worktreeInfo{}
	unreadable := map[string]bool{}
	gone := map[string]wsRef{}
	for _, x := range wb.records.WorkspaceRefs() {
		if x.Parent == "" || !x.Adopted || x.Kind != kindWorktree || isDir(x.Dir) {
			continue
		}
		repo := wb.records.ProjectDir(x.Project)
		if _, ok := lists[repo]; !ok && !unreadable[repo] {
			wts, err := listWorktrees(repo)
			if err != nil || !isDir(repo) {
				unreadable[repo] = true
				continue
			}
			lists[repo] = wts
		}
		if unreadable[repo] {
			continue
		}
		if wt := worktreeAt(lists[repo], x.Dir); wt != nil && !wt.Prunable {
			continue
		}
		gone[x.ID] = x
	}
	wb.attr.mu.Lock()
	var confirmed []wsRef
	for id, x := range gone {
		if wb.attr.gone[id] {
			confirmed = append(confirmed, x)
		}
	}
	wb.attr.gone = map[string]bool{}
	for id := range gone {
		wb.attr.gone[id] = true
	}
	wb.attr.mu.Unlock()
	for _, x := range confirmed {
		wb.reapWorktree(x)
	}
	if len(confirmed) > 0 {
		wb.stopOrphans()
	}
}

// reapWorktree forgets one gone worktree workspace and hides its chats: the
// ones filed under it, and every one that ran in its folder. They are filed
// under its parent, so the parent's Show removed lists them. The chat list is
// read outside the records' lock: OpenCode's comes from its CLI.
func (wb *workbench) reapWorktree(x wsRef) {
	// A chat both filed under it and run in its folder is one chat.
	keys := map[string]bool{}
	for k, id := range wb.records.ChatWorkspaces() {
		if id == x.ID {
			keys[k] = true
		}
	}
	for _, c := range wb.records.Chats().Sessions(context.Background()) {
		if sameDir(c.Dir, x.Dir) {
			keys[chatKey(c.Agent, c.ID)] = true
		}
	}
	// Files with the folder gone only prunes: git lets the branch be checked
	// out again.
	note, err := wb.records.RemoveWorkspace(x.ID, RemoveOptions{Files: true})
	if err != nil {
		log.Printf("forgetting gone worktree %s: %v", tildePath(x.Dir), err)
		return
	}
	if note != "" {
		log.Printf("forgot gone worktree %s: %s", tildePath(x.Dir), note)
	}
	if err := wb.records.HideChats(slices.Collect(maps.Keys(keys)), x.Parent); err != nil {
		log.Printf("hiding the chats of gone worktree %s: %v", tildePath(x.Dir), err)
	}
	log.Printf("worktree %s is gone: workspace %q forgotten, %d chats hidden", tildePath(x.Dir), x.Name, len(keys))
}

// handleWorkspaceUpdate lists a worktree workspace under another workspace of
// its project, or at the top ({"parent": ""}): Move under in the sidebar.
func (wb *workbench) handleWorkspaceUpdate(w http.ResponseWriter, r *http.Request) {
	if !localOrigin(w, r) {
		return
	}
	var body struct {
		Parent *string `json:"parent"`
	}
	if readJSONBody(r, &body) != nil || body.Parent == nil {
		fail(w, http.StatusBadRequest, "parent is required")
		return
	}
	id := r.PathValue("id")
	if err := wb.records.SetParent(id, *body.Parent); err != nil {
		failRecord(w, err)
		return
	}
	ws := wb.records.Workspace(id)
	if ws == nil {
		fail(w, http.StatusNotFound, "workspace not found")
		return
	}
	view := wb.records.View(ws)
	wb.fillWorkspace(&view)
	writeJSON(w, view)
}
