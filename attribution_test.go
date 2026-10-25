package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An agent working in a worktree of its tab's project lists that worktree
// under the tab's workspace (found until then); one named like a workspace
// gets a number; a plain folder of the project is no worktree. Once the
// worktree's folder is gone for two passes it is forgotten, git prunes it,
// and its chats are hidden under the workspace it was listed under.
func TestAgentWorktreesNestAndGoWithTheirFolder(t *testing.T) {
	wb, claude, repo := historyWorkbench(t)
	p, err := wb.records.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	hmm, _, err := wb.records.AddWorkspace(p.ID, WorkspaceOptions{Name: "hmm"})
	if err != nil {
		t.Fatal(err)
	}
	tab, err := wb.records.AddTab(hmm.ID, tabClaude, "")
	if err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(repo, ".claude", "worktrees", "pay-api")
	runGit(t, repo, "worktree", "add", "-q", "-b", "worktree-pay-api", wt)
	clash := filepath.Join(t.TempDir(), "x")
	runGit(t, repo, "worktree", "add", "-q", "-b", "hmm", clash)
	if err := os.MkdirAll(filepath.Join(wt, "src"), 0o755); err != nil {
		t.Fatal(err)
	}

	found := func() []string {
		t.Helper()
		var out []string
		for _, pv := range wb.records.List() {
			if pv.ID == p.ID {
				for _, f := range pv.Found {
					out = append(out, f.Path)
				}
			}
		}
		return out
	}
	if f := found(); len(f) != 2 {
		t.Fatalf("found before any agent worked there: %v", f)
	}

	wb.attributeDir(tab.ID, filepath.Join(repo, "sub")) // the project's own checkout
	wb.attributeDir(tab.ID, filepath.Join(wt, "src"))   // cwd after EnterWorktree, cd src
	wb.attributeDir(tab.ID, clash)
	child := map[string]*Workspace{}
	for _, pv := range wb.records.List() {
		for _, w := range pv.Workspaces {
			if pv.ID == p.ID {
				child[w.Dir] = w.Workspace
			}
		}
	}
	pay, named := child[wt], child[clash]
	if pay == nil || pay.Parent != hmm.ID || !pay.Adopted || pay.Kind != kindWorktree || pay.Name != "worktree-pay-api" {
		t.Fatalf("the agent's worktree: %+v", pay)
	}
	if named == nil || named.Name != "hmm-2" || named.Parent != hmm.ID {
		t.Fatalf("a worktree on branch hmm: %+v", named)
	}
	if len(child) != 3 { // main and hmm share the project folder
		t.Fatalf("workspace folders after attribution: %v", child)
	}
	if f := found(); len(f) != 0 {
		t.Fatalf("still found once listed: %v", f)
	}

	const worker = "99999999-0000-4000-8000-000000000009"
	writeChat(t, claude, "-pay", worker, claudeUser(wt, worker, "write the payments API"))
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	wb.reapGoneWorktrees()
	if wb.records.Workspace(pay.ID) == nil {
		t.Fatal("forgotten on the first pass: a folder can be missing for a moment")
	}
	wb.reapGoneWorktrees()
	if wb.records.Workspace(pay.ID) != nil {
		t.Fatal("kept after its folder was gone for two passes")
	}
	if out := runGit(t, repo, "worktree", "list"); strings.Contains(out, "pay-api") {
		t.Fatalf("git still lists the worktree:\n%s", out)
	}
	if !wb.records.RemovedChats()[chatKey("claude", worker)] {
		t.Fatal("its worker's chat is still listed")
	}
	var l historyList
	getJSON(t, wb, "/api/history?scope=workspace&removed=1&ws="+wbItemID("dir", repo), &l)
	if l.workspaceOf(worker) != hmm.ID {
		t.Fatalf("its hidden chat lists under %q, want hmm %s", l.workspaceOf(worker), hmm.ID)
	}
}

// A worktree moves under any workspace of its project but never under itself
// or a worktree below it, and only worktrees move; removing one moves what
// was under it up a level.
func TestWorkspaceParents(t *testing.T) {
	wb, _, repo := historyWorkbench(t)
	p, err := wb.records.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	main := p.Workspaces[0]
	a, _, err := wb.records.AddWorkspace(p.ID, WorkspaceOptions{Name: "a", Worktree: true, Parent: main.ID})
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := wb.records.AddWorkspace(p.ID, WorkspaceOptions{Name: "b", Worktree: true, Parent: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	patch := func(id, parent string) int {
		t.Helper()
		return reqJSON(t, wb, "PATCH", "/api/workspaces/"+id, `{"parent":"`+parent+`"}`).Code
	}
	for _, c := range []struct{ id, parent string }{{a.ID, b.ID}, {a.ID, a.ID}, {main.ID, a.ID}, {b.ID, "nope"}} {
		if code := patch(c.id, c.parent); code != 400 {
			t.Fatalf("%s under %s: %d, want 400", c.id, c.parent, code)
		}
	}
	if _, _, err := wb.records.AddWorkspace(p.ID, WorkspaceOptions{Name: "c", Parent: main.ID}); err == nil {
		t.Fatal("a local workspace was listed under another")
	}
	if code := patch(b.ID, ""); code != 200 || wb.records.Workspace(b.ID).Parent != "" {
		t.Fatalf("to the top: %d", code)
	}
	if code := patch(b.ID, a.ID); code != 200 {
		t.Fatalf("back under a: %d", code)
	}
	if _, err := wb.records.RemoveWorkspace(a.ID, RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := wb.records.Workspace(b.ID).Parent; got != main.ID {
		t.Fatalf("with a removed, b is under %q, want main %s", got, main.ID)
	}
}
