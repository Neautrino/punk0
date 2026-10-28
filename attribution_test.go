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

	// The worktrees no workspace runs in, which the New workspace panel offers
	// to adopt.
	found := func() []string {
		t.Helper()
		var out []string
		for _, wt := range wb.records.Worktrees(repo) {
			if wt.Workspace == "" {
				out = append(out, wt.Path)
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

// An agent's worktree inside the Workspaces Folder is punk0's own folder, so
// it is never Adopted: Auto is what marks it as the agent's. It goes the same
// way once its folder has been missing for two passes, chats and all, while a
// worktree the user made keeps its missing row.
func TestAutoWorktreeInTheCodeRootIsReaped(t *testing.T) {
	wb, claude, repo := historyWorkbench(t)
	p, err := wb.records.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	main := p.Workspaces[0]
	agent, _, err := wb.records.AddWorkspace(p.ID, WorkspaceOptions{Name: "fix-login", Worktree: true, Parent: main.ID, Auto: true})
	if err != nil {
		t.Fatal(err)
	}
	mine, _, err := wb.records.AddWorkspace(p.ID, WorkspaceOptions{Name: "mine", Worktree: true, Parent: main.ID})
	if err != nil {
		t.Fatal(err)
	}
	root, err := codeRootPath()
	if err != nil {
		t.Fatal(err)
	}
	if !agent.Auto || agent.Adopted || !wb.records.ownWorktree(agent.Dir, root) {
		t.Fatalf("the agent's worktree %+v, want Auto and not adopted under %s", agent, root)
	}

	const worker = "99999999-0000-4000-8000-00000000000a"
	writeChat(t, claude, "-fix", worker, claudeUser(agent.Dir, worker, "fix the login"))
	for _, dir := range []string{agent.Dir, mine.Dir} {
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
	}
	wb.reapGoneWorktrees()
	if wb.records.Workspace(agent.ID) == nil {
		t.Fatal("forgotten on the first pass: a folder can be missing for a moment")
	}
	wb.reapGoneWorktrees()
	if wb.records.Workspace(agent.ID) != nil {
		t.Fatal("kept after its folder was gone for two passes")
	}
	if wb.records.Workspace(mine.ID) == nil {
		t.Fatal("a worktree the user made was forgotten with its folder")
	}
	if out := runGit(t, repo, "worktree", "list"); strings.Contains(out, "fix-login") {
		t.Fatalf("git still lists the worktree:\n%s", out)
	}
	if !wb.records.RemovedChats()[chatKey("claude", worker)] {
		t.Fatal("its agent's chat is still listed")
	}
}

// A worktree is listed under a workspace of its project when it is made,
// only worktrees are, and removing one moves what was under it up a level.
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
	if _, _, err := wb.records.AddWorkspace(p.ID, WorkspaceOptions{Name: "c", Parent: main.ID}); err == nil {
		t.Fatal("a local workspace was listed under another")
	}
	if _, err := wb.records.RemoveWorkspace(a.ID, RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := wb.records.Workspace(b.ID).Parent; got != main.ID {
		t.Fatalf("with a removed, b is under %q, want main %s", got, main.ID)
	}
}

// Renaming a workspace changes only its label, which stays unique in the
// project and must be a valid name; its folder and branch stay.
func TestRenameWorkspace(t *testing.T) {
	wb, _, repo := historyWorkbench(t)
	p, err := wb.records.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	a, _, err := wb.records.AddWorkspace(p.ID, WorkspaceOptions{Name: "a", Worktree: true})
	if err != nil {
		t.Fatal(err)
	}
	rename := func(id, body string) int {
		t.Helper()
		return reqJSON(t, wb, "PATCH", "/api/workspaces/"+id, body).Code
	}
	if code := rename(a.ID, `{"name":"  Fix login  "}`); code != 200 {
		t.Fatalf("rename: %d", code)
	}
	got := wb.records.Workspace(a.ID)
	if got.Name != "Fix login" || got.Dir != a.Dir || got.Branch != a.Branch {
		t.Fatalf("renamed %+v, want name %q with folder %s and branch %s kept", got, "Fix login", a.Dir, a.Branch)
	}
	for _, body := range []string{`{"name":"` + p.Workspaces[0].Name + `"}`, `{"name":"  "}`, `{}`} {
		if code := rename(a.ID, body); code != 400 {
			t.Fatalf("rename %s: %d, want 400", body, code)
		}
	}
	if code := rename("nope", `{"name":"x"}`); code != 404 {
		t.Fatalf("rename unknown: %d, want 404", code)
	}
	// A chat's Name is its folder's: it is renamed through its title instead.
	chat, err := wb.records.NewChat(ChatOptions{Agent: tabShell})
	if err != nil {
		t.Fatal(err)
	}
	if code := rename(chat.ID, `{"name":"x"}`); code != 400 || wb.records.Workspace(chat.ID).Name != chat.Name {
		t.Fatalf("rename chat: %d, name %q; want 400 and %q kept", code, wb.records.Workspace(chat.ID).Name, chat.Name)
	}
}
