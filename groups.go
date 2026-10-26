package main

// A project's workspaces are its features, and each is the group its chats
// belong to. Local workspaces share the project folder, so where a chat ran
// cannot say which of them it is in: chatWorkspaces files every chat
// (agent:id) under its workspace when a tab starts or resumes it, when its
// agent reports a session the tab was not started on, and when the user moves
// it. A worktree an agent makes is listed under the workspace whose agent
// made it (Workspace.Parent, attribution.go).

import (
	"cmp"
	"fmt"
	"slices"
)

// fileTabLocked files the chat a new tab starts or resumes under the tab's
// workspace. A claude tab always has its session id; another agent's has one
// only when it resumes a chat. Opening a chat somewhere is the user saying it
// belongs there, so this overwrites. Callers hold r.mu and save.
func (r *Records) fileTabLocked(w *Workspace, t *Satellite) {
	if t.Session != "" && t.Kind != tabShell {
		r.chatWorkspaces[chatKey(t.Kind, t.Session)] = w.ID
	}
}

// refileLocked moves every chat filed under workspace from to workspace to,
// or out of every workspace with to "". Callers hold r.mu and save.
func (r *Records) refileLocked(from, to string) {
	for k, id := range r.chatWorkspaces {
		if id != from {
			continue
		}
		if to == "" {
			delete(r.chatWorkspaces, k)
		} else {
			r.chatWorkspaces[k] = to
		}
	}
}

// NoteSession files the chat an agent reports from a tab under the tab's
// workspace, unless it is filed already: /clear starts a new session in the
// same tab, and a chat the user moved elsewhere stays where it was moved.
func (r *Records) NoteSession(tabID, session string) error {
	if !validSession.MatchString(session) {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	w, i := r.tabLocked(tabID)
	if w == nil || w.Tabs[i].Kind == tabShell {
		return nil
	}
	k := chatKey(w.Tabs[i].Kind, session)
	if r.chatWorkspaces[k] != "" {
		return nil
	}
	r.chatWorkspaces[k] = w.ID
	return r.saveLocked()
}

// MoveChats files chats (agent:id keys) under workspace id.
func (r *Records) MoveChats(keys []string, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, _ := r.workspaceLocked(id); p == nil {
		return fmt.Errorf("workspace %q: %w", id, errNotFound)
	}
	for _, k := range keys {
		r.chatWorkspaces[k] = id
	}
	return r.saveLocked()
}

// ForgetChat drops a chat's workspace: it was deleted for good.
func (r *Records) ForgetChat(agent, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := chatKey(agent, id)
	if _, ok := r.chatWorkspaces[k]; !ok {
		return nil
	}
	delete(r.chatWorkspaces, k)
	return r.saveLocked()
}

// ChatWorkspaces is every filed chat's workspace, for one listing.
func (r *Records) ChatWorkspaces() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]string, len(r.chatWorkspaces))
	for k, v := range r.chatWorkspaces {
		out[k] = v
	}
	return out
}

// HideChats takes chats out of Chats mode, as Remove does, and files them
// under workspace id ("" leaves them where they are), in one save: the chats
// of a worktree that is gone go under the workspace it was listed under,
// where Show removed finds them.
func (r *Records) HideChats(keys []string, id string) error {
	if len(keys) == 0 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, _ := r.workspaceLocked(id); p == nil {
		id = ""
	}
	for _, k := range keys {
		if id != "" {
			r.chatWorkspaces[k] = id
		}
		if !slices.Contains(r.removedChats, k) {
			r.removedChats = append(r.removedChats, k)
		}
	}
	return r.saveLocked()
}

// RenameWorkspace gives workspace id a new label, unique among its
// project's workspaces. Only the label changes: its folder, branch and tabs
// stay as they are.
func (r *Records) RenameWorkspace(id, name string) error {
	name, err := label(name, 80)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	p, i := r.workspaceLocked(id)
	if p == nil {
		return fmt.Errorf("workspace %q: %w", id, errNotFound)
	}
	w := p.Workspaces[i]
	if w.Kind == kindFolder {
		// A chat's Name is its folder's, which agents key its history on: a
		// chat is renamed through its Title (UpdateChat).
		return fmt.Errorf("chat %q is renamed through its title", id)
	}
	if w.Name == name {
		return nil
	}
	if slices.ContainsFunc(p.Workspaces, func(x *Workspace) bool { return x.ID != id && x.Name == name }) {
		return fmt.Errorf("a workspace named %q already exists", name)
	}
	w.Name = name
	return r.saveLocked()
}

// workspaceProject is the project holding workspace id, and the workspace's
// folder; both "" for an unknown workspace.
func (r *Records) workspaceProject(id string) (projectID, dir string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, i := r.workspaceLocked(id); p != nil {
		return p.ID, p.Workspaces[i].Dir
	}
	return "", ""
}

// wsRef is a workspace as a listing names it, copied under the lock.
type wsRef struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Dir         string `json:"-"`
	Branch      string `json:"branch,omitempty"`
	Parent      string `json:"parent,omitempty"`
	Adopted     bool   `json:"-"`
	Project     string `json:"-"`
	ProjectName string `json:"-"`
	Scratch     bool   `json:"-"`
}

// WorkspaceRefs lists every workspace, in records order.
func (r *Records) WorkspaceRefs() []wsRef {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []wsRef
	for _, p := range r.projects {
		for _, w := range p.Workspaces {
			out = append(out, wsRef{ID: w.ID, Name: cmp.Or(w.Title, w.Name), Kind: w.Kind, Dir: w.Dir, Branch: w.Branch, Parent: w.Parent, Adopted: w.Adopted, Project: p.ID, ProjectName: p.Name, Scratch: p.Scratch})
		}
	}
	return out
}

// TabsAt lists the tabs of workspace id, or, for an editor's folder backend
// id (/w/<id>/), of every workspace running in that folder: the editor shows
// the folder, and the folder is every one of its workspaces. Each tab says
// which workspace it is in.
func (r *Records) TabsAt(id string) ([]tabView, bool) {
	r.mu.Lock()
	var tabs []*Satellite
	var where []*Workspace
	if p, i := r.workspaceLocked(id); p != nil {
		w := p.Workspaces[i]
		for _, t := range w.Tabs {
			tabs, where = append(tabs, t), append(where, w)
		}
	} else {
		found := false
		for _, p := range r.projects {
			for _, w := range p.Workspaces {
				if wbItemID("dir", w.Dir) != id {
					continue
				}
				found = true
				for _, t := range w.Tabs {
					tabs, where = append(tabs, t), append(where, w)
				}
			}
		}
		if !found {
			r.mu.Unlock()
			return nil, false
		}
	}
	names := make([]string, len(where))
	for i, w := range where {
		names[i] = cmp.Or(w.Title, w.Name)
	}
	r.mu.Unlock()
	// A tab's state takes its own lock, never r.mu.
	out := make([]tabView, len(tabs))
	for i, t := range tabs {
		out[i] = t.view()
		out[i].Workspace, out[i].WorkspaceName = where[i].ID, names[i]
	}
	return out, true
}
