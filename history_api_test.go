package main

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// historyWorkbench is a workbench on a temp state dir, with the agents'
// history pointed at temp folders and no agent binary behind it. A tab it
// starts must find no agent on PATH and no settings of the user's, so
// nothing a test opens can run a real agent. It returns the workbench, the
// Claude config dir and the project repository.
func historyWorkbench(t *testing.T) (*workbench, string, string) {
	t.Helper()
	claude := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	t.Setenv("CODEX_HOME", t.TempDir())
	repo := cleanRepo(t)
	useSettings(t, map[string]any{})
	t.Setenv("SHELL", "/bin/sh")
	// Git and nothing else: a tab these tests open must find no agent to run.
	bin := t.TempDir()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not installed")
	}
	if err := os.Symlink(git, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	newTestStore(t)
	wb := testWorkbench(t, "/", repo, "", nil)
	wb.records.Chats().cli = noCLI{}
	return wb, claude, repo
}

type historyList struct {
	Sessions []struct {
		Agent, ID, Dir, Title, Workspace string
		Messages                         int
		Updated                          int64
		Open, Missing, Removed           bool
	}
}

func TestHistoryListScopesAndSearch(t *testing.T) {
	wb, claude, repo := historyWorkbench(t)
	outside := t.TempDir()
	p, err := wb.records.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	ws := p.Workspaces[0]
	writeChat(t, claude, "-repo", "11111111-0000-4000-8000-000000000001", claudeUser(repo, "11111111-0000-4000-8000-000000000001", "fix the login bug"))
	writeChat(t, claude, "-out", "22222222-0000-4000-8000-000000000002", claudeUser(outside, "22222222-0000-4000-8000-000000000002", "scratch experiment"))

	var all historyList
	getJSON(t, wb, "/api/history?scope=all", &all)
	if len(all.Sessions) != 2 {
		t.Fatalf("scope=all listed %+v", all.Sessions)
	}

	var mine historyList
	getJSON(t, wb, "/api/history?scope=workspace&ws="+ws.ID, &mine)
	if len(mine.Sessions) != 1 || mine.Sessions[0].ID != "11111111-0000-4000-8000-000000000001" {
		t.Fatalf("scope=workspace listed %+v", mine.Sessions)
	}
	if mine.Sessions[0].Workspace != ws.ID || mine.Sessions[0].Open || mine.Sessions[0].Missing {
		t.Fatalf("chat in a registered folder: %+v", mine.Sessions[0])
	}

	// The search matches the title, ignoring case.
	var found historyList
	getJSON(t, wb, "/api/history?scope=all&q=LOGIN", &found)
	if len(found.Sessions) != 1 || found.Sessions[0].ID != "11111111-0000-4000-8000-000000000001" {
		t.Fatalf("q=LOGIN listed %+v", found.Sessions)
	}

	// A chat outside punk0 is listed with no workspace, so the page can offer
	// to add its folder.
	var out historyList
	getJSON(t, wb, "/api/history?scope=all&q=scratch", &out)
	if len(out.Sessions) != 1 || out.Sessions[0].Workspace != "" {
		t.Fatalf("chat outside punk0: %+v", out.Sessions)
	}

	// A tab on a chat marks it open.
	if _, err := wb.records.AddTab(ws.ID, tabClaude, "11111111-0000-4000-8000-000000000001"); err != nil {
		t.Fatal(err)
	}
	var open historyList
	getJSON(t, wb, "/api/history?scope=workspace&ws="+ws.ID, &open)
	if !open.Sessions[0].Open {
		t.Fatal("a chat with a tab on it is not marked open")
	}

	// A folder that is gone is listed as missing, not dropped.
	gone := filepath.Join(outside, "gone")
	if err := os.MkdirAll(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	writeChat(t, claude, "-gone", "33333333-0000-4000-8000-000000000003", claudeUser(gone, "33333333-0000-4000-8000-000000000003", "vanished folder"))
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	var missing historyList
	getJSON(t, wb, "/api/history?scope=all&q=vanished", &missing)
	if len(missing.Sessions) != 1 || !missing.Sessions[0].Missing {
		t.Fatalf("chat whose folder is gone: %+v", missing.Sessions)
	}

	// An unknown workspace is a 404, not an empty list that looks like none.
	if w := reqJSON(t, wb, "GET", "/api/history?scope=workspace&ws=nope", ""); w.Code != http.StatusNotFound {
		t.Fatalf("unknown workspace: %d %s", w.Code, w.Body.String())
	}
}

// Removing a chat is punk0's own bookkeeping: the row goes, the transcript
// stays, Show removed brings the row back and Sync takes every removal back
// at once.
func TestHistoryRemovesAChatAndSyncRestoresIt(t *testing.T) {
	wb, claude, repo := historyWorkbench(t)
	const id = "44444444-0000-4000-8000-000000000004"
	path := writeChat(t, claude, "-repo", id, claudeUser(repo, id, "noisy chat"))

	if w := postJSON(t, wb, "/api/history/remove", `{"agent":"claude","id":"`+id+`"}`); w.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", w.Code, w.Body.String())
	}
	var list historyList
	getJSON(t, wb, "/api/history?scope=all", &list)
	if len(list.Sessions) != 0 {
		t.Fatalf("a removed chat is still listed: %+v", list.Sessions)
	}
	var shown historyList
	getJSON(t, wb, "/api/history?scope=all&removed=1", &shown)
	if len(shown.Sessions) != 1 || !shown.Sessions[0].Removed {
		t.Fatalf("removed=1 listed %+v", shown.Sessions)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("removing took the transcript off disk: %v", err)
	}

	// Restore one by hand, then remove it again and let Sync do it.
	if w := postJSON(t, wb, "/api/history/remove", `{"agent":"claude","id":"`+id+`","removed":false}`); w.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", w.Code, w.Body.String())
	}
	var after historyList
	getJSON(t, wb, "/api/history?scope=all", &after)
	if len(after.Sessions) != 1 || after.Sessions[0].Removed {
		t.Fatalf("after restoring: %+v", after.Sessions)
	}
	postJSON(t, wb, "/api/history/remove", `{"agent":"claude","id":"`+id+`"}`)
	w := postJSON(t, wb, "/api/history/sync", `{}`)
	if w.Code != http.StatusOK {
		t.Fatalf("sync: %d %s", w.Code, w.Body.String())
	}
	var synced struct{ Restored int }
	decode(t, w.Body, &synced)
	if synced.Restored != 1 {
		t.Fatalf("sync restored %d chats", synced.Restored)
	}
	var back historyList
	getJSON(t, wb, "/api/history?scope=all", &back)
	if len(back.Sessions) != 1 || back.Sessions[0].Removed {
		t.Fatalf("after Sync: %+v", back.Sessions)
	}
}

func TestHistoryOpenResumesTheChatInATab(t *testing.T) {
	wb, claude, repo := historyWorkbench(t)
	p, err := wb.records.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	ws := p.Workspaces[0]
	const id = "55555555-0000-4000-8000-000000000005"
	writeChat(t, claude, "-repo", id, claudeUser(repo, id, "resume me"))

	var opened struct{ Tab, Workspace string }
	w := postJSON(t, wb, "/api/history/open", `{"agent":"claude","id":"`+id+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("open: %d %s", w.Code, w.Body.String())
	}
	decode(t, w.Body, &opened)
	if opened.Workspace != ws.ID {
		t.Fatalf("opened in workspace %q, want %q", opened.Workspace, ws.ID)
	}
	tab := wb.records.Tab(opened.Tab)
	if tab == nil || tab.Kind != tabClaude || tab.Session != id {
		t.Fatalf("tab %+v", tab)
	}
	if tab.newChat {
		t.Fatal("the tab would start a new session instead of resuming the chat")
	}

	// Opening it again reuses the tab rather than making a second one on one
	// conversation.
	w = postJSON(t, wb, "/api/history/open", `{"agent":"claude","id":"`+id+`"}`)
	var again struct{ Tab, Workspace string }
	decode(t, w.Body, &again)
	if again.Tab != opened.Tab {
		t.Fatalf("second open made tab %q, want %q", again.Tab, opened.Tab)
	}
	if n := len(wb.records.Workspace(ws.ID).Tabs); n != 1 {
		t.Fatalf("workspace has %d tabs", n)
	}
}

func TestHistoryOpenOffersToAddTheFolder(t *testing.T) {
	wb, claude, _ := historyWorkbench(t)
	outside := t.TempDir()
	const id = "66666666-0000-4000-8000-000000000006"
	writeChat(t, claude, "-out", id, claudeUser(outside, id, "somewhere else"))

	w := postJSON(t, wb, "/api/history/open", `{"agent":"claude","id":"`+id+`"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("open outside punk0: %d %s", w.Code, w.Body.String())
	}
	var offer struct{ AddProject, Adopt, Project string }
	decode(t, w.Body, &offer)
	if offer.AddProject == "" || offer.Adopt != "" {
		t.Fatalf("offer %+v, want addProject", offer)
	}

	// With the folder added, the same request opens the chat.
	if w := postJSON(t, wb, "/api/projects", `{"dir":"`+offer.AddProject+`"}`); w.Code != http.StatusOK {
		t.Fatalf("add project: %d %s", w.Code, w.Body.String())
	}
	if w := postJSON(t, wb, "/api/history/open", `{"agent":"claude","id":"`+id+`"}`); w.Code != http.StatusOK {
		t.Fatalf("open after adding: %d %s", w.Code, w.Body.String())
	}
}

// TestHistoryOpenRefusesFoldersThatCannotBecomeProjects pins the folders the
// page must never be offered: adding them would register another folder and
// leave the chat exactly where it was, so the user would confirm in a loop.
func TestHistoryOpenRefusesFoldersThatCannotBecomeProjects(t *testing.T) {
	wb, claude, repo := historyWorkbench(t)
	if _, err := wb.records.AddProject(repo); err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	// A subfolder of the project, a folder in a repository punk0 does not
	// know, and the home folder.
	loose := cleanRepo(t)
	for _, c := range []struct{ id, dir, want string }{
		{"77777777-0000-4000-8000-00000000000a", filepath.Join(repo, "sub"), "inside the workspace"},
		{"77777777-0000-4000-8000-00000000000b", filepath.Join(loose, "sub"), "inside the repository"},
		{"77777777-0000-4000-8000-00000000000c", home, "home folder"},
	} {
		writeChat(t, claude, "-x"+c.id, c.id, claudeUser(c.dir, c.id, "ran in "+c.dir))
		w := postJSON(t, wb, "/api/history/open", `{"agent":"claude","id":"`+c.id+`"}`)
		if w.Code != http.StatusConflict {
			t.Fatalf("open in %s: %d %s", c.dir, w.Code, w.Body.String())
		}
		var res struct{ AddProject, Adopt, Error string }
		decode(t, w.Body, &res)
		if res.AddProject != "" || res.Adopt != "" {
			t.Fatalf("open in %s offered %+v; the page would loop", c.dir, res)
		}
		if !strings.Contains(res.Error, c.want) || !strings.Contains(res.Error, tildePath(c.dir)) {
			t.Fatalf("open in %s said %q, want it to name the folder and say %q", c.dir, res.Error, c.want)
		}
	}
}

// Deleting a chat permanently closes every tab on it first -- an agent still
// holding the session would write the transcript back -- then unlinks the
// transcript and the folder of subagent transcripts beside it.
func TestHistoryDeleteClosesTheTabAndRemovesTheTranscript(t *testing.T) {
	wb, claude, repo := historyWorkbench(t)
	p, err := wb.records.AddProject(repo)
	if err != nil {
		t.Fatal(err)
	}
	const id = "77777777-0000-4000-8000-000000000007"
	path := writeChat(t, claude, "-repo", id, claudeUser(repo, id, "delete me"))
	side := filepath.Join(claude, "projects", "-repo", id)
	if err := os.MkdirAll(side, 0o755); err != nil {
		t.Fatal(err)
	}

	tab, err := wb.records.AddTab(p.Workspaces[0].ID, tabClaude, id)
	if err != nil {
		t.Fatal(err)
	}
	// The shell finds no claude on PATH; what matters is that the tab runs.
	if _, err := tab.Shell(); err != nil {
		t.Skipf("no shell to run: %v", err)
	}
	if w := reqJSON(t, wb, "DELETE", "/api/history/claude/"+id, ""); w.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	if wb.records.Tab(tab.ID) != nil {
		t.Fatal("the tab on the deleted chat is still registered")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the transcript is still there: %v", err)
	}
	if _, err := os.Stat(side); !os.IsNotExist(err) {
		t.Fatalf("the subagents folder is still there: %v", err)
	}
	if trashed, _ := filepath.Glob(filepath.Join(wb.records.home, "trash", "*")); len(trashed) != 0 {
		t.Fatalf("punk0's trash holds %v, want nothing: a deleted chat is gone", trashed)
	}
	var list historyList
	getJSON(t, wb, "/api/history?scope=all&removed=1", &list)
	if len(list.Sessions) != 0 {
		t.Fatalf("the deleted chat is still listed: %+v", list.Sessions)
	}
	if w := reqJSON(t, wb, "DELETE", "/api/history/claude/"+id, ""); w.Code != http.StatusNotFound {
		t.Fatalf("deleting it twice: %d %s", w.Code, w.Body.String())
	}
}

// Closing a tab never deletes the chat: the conversation stays on disk, stays
// listed, and opening it again resumes that very session.
func TestHistoryClosedTabLeavesTheChatListed(t *testing.T) {
	wb, claude, repo := historyWorkbench(t)
	if _, err := wb.records.AddProject(repo); err != nil {
		t.Fatal(err)
	}
	const id = "88888888-0000-4000-8000-000000000008"
	path := writeChat(t, claude, "-repo", id, claudeUser(repo, id, "come back to me"))

	var opened struct{ Tab, Workspace string }
	decode(t, postJSON(t, wb, "/api/history/open", `{"agent":"claude","id":"`+id+`"}`).Body, &opened)
	if w := reqJSON(t, wb, "DELETE", "/api/tabs/"+opened.Tab, ""); w.Code != http.StatusOK {
		t.Fatalf("closing the tab: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("closing the tab took the transcript: %v", err)
	}
	var list historyList
	getJSON(t, wb, "/api/history?scope=all", &list)
	if len(list.Sessions) != 1 || list.Sessions[0].ID != id || list.Sessions[0].Open {
		t.Fatalf("after closing the tab: %+v", list.Sessions)
	}

	var again struct{ Tab, Workspace string }
	decode(t, postJSON(t, wb, "/api/history/open", `{"agent":"claude","id":"`+id+`"}`).Body, &again)
	if again.Tab == "" || again.Tab == opened.Tab {
		t.Fatalf("reopened as tab %q, want a new one", again.Tab)
	}
	if tab := wb.records.Tab(again.Tab); tab == nil || tab.Session != id {
		t.Fatalf("the new tab resumes %+v, want session %s", tab, id)
	}
}

// The preview is read on demand, for a removed chat as much as a listed one;
// a chat that is not on disk is 404.
func TestHistoryPreviewReadsTheConversation(t *testing.T) {
	wb, claude, repo := historyWorkbench(t)
	const id = "99999999-0000-4000-8000-000000000009"
	writeChat(t, claude, "-repo", id,
		claudeUser(repo, id, "<command-name>/model</command-name>"),
		claudeUser(repo, id, "port the parser"),
		claudeAssistantLine(repo, id))

	var p struct {
		LastPrompt, LastReply, Dir, Branch string
		Messages                           int
	}
	getJSON(t, wb, "/api/history/claude/"+id+"/preview", &p)
	if p.LastPrompt != "port the parser" || p.LastReply == "" || p.Dir != repo || p.Messages != 2 {
		t.Fatalf("preview %+v", p)
	}

	// Removed from the list, still previewable: it is still on disk.
	postJSON(t, wb, "/api/history/remove", `{"agent":"claude","id":"`+id+`"}`)
	if w := reqJSON(t, wb, "GET", "/api/history/claude/"+id+"/preview", ""); w.Code != http.StatusOK {
		t.Fatalf("preview of a removed chat: %d %s", w.Code, w.Body.String())
	}
	if w := reqJSON(t, wb, "GET", "/api/history/claude/nope/preview", ""); w.Code != http.StatusNotFound {
		t.Fatalf("preview of a chat that is not there: %d %s", w.Code, w.Body.String())
	}
}
