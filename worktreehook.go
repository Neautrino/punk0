package main

// Claude Code does not make worktrees itself: WorktreeCreate asks a hook for
// the folder to use and WorktreeRemove tells it to take one away, so punk0
// puts them in the Workspaces Folder and lists them under the tab that asked.
// `punk0 hook` answers both -- through the running punk0, or, when there is
// none to reach, with git in the same layout, so `claude --worktree` keeps
// working while punk0 is stopped.

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"px0/internal/applog"
)

// The hook events punk0 answers rather than reports.
const (
	worktreeCreateEvent = "WorktreeCreate"
	worktreeRemoveEvent = "WorktreeRemove"
	subagentStopEvent   = "SubagentStop"
)

// worktreeBranch is the branch a worktree named name gets, Claude's own
// naming: the folder is <name>, the branch worktree-<name>.
func worktreeBranch(name string) string { return "worktree-" + name }

// agentWorktree makes the worktree a tab's Claude asked for: folder <name>
// and a new branch worktree-<name> from the tab folder's HEAD, in the
// Workspaces Folder, listed under the tab's workspace. `claude --worktree
// <name>` re-enters a worktree it made before, so one of that name under the
// same workspace comes back as it is.
func (wb *workbench) agentWorktree(tabID, name string) (string, error) {
	if name == "" {
		return "", errors.New("a worktree needs a name")
	}
	place, ok := wb.records.Place(tabID)
	if !ok {
		return "", fmt.Errorf("tab %q: %w", tabID, errNotFound)
	}
	if place.kind == kindFolder {
		return "", errors.New("a scratch chat has no repository to make a worktree of")
	}
	if dir, ok := wb.records.ChildWorktree(place.workspaceID, name); ok {
		return dir, nil
	}
	projectID, _ := wb.records.workspaceProject(place.workspaceID)
	ws, _, err := wb.records.AddWorkspace(projectID, WorkspaceOptions{
		Name: name, Folder: name, From: gitHeadCommit(place.dir), Branch: worktreeBranch(name),
		Worktree: true, Parent: place.workspaceID, Auto: true,
	})
	if err != nil {
		return "", err
	}
	slog.Info("agent worktree made", "dir", ws.Dir, "branch", ws.Branch, "tab", tabID)
	return ws.Dir, nil
}

// agentWorktreeGone takes away the worktree Claude asks to remove. A folder
// punk0 has no row for is not found, which lets the hook remove it with git
// itself.
func (wb *workbench) agentWorktreeGone(path string) error {
	if path == "" {
		return errors.New("WorktreeRemove without a worktree_path")
	}
	id := workspaceIn(wb.records.WorkspaceByDir(), path)
	if id == "" {
		return fmt.Errorf("%s is no workspace of punk0's: %w", tildePath(path), errNotFound)
	}
	return wb.removeAgentWorktree(id)
}

// removeAgentWorktree forgets one worktree row, removes its folder and, when
// nothing is left on it, deletes its branch: Claude leaves the branch of a
// worktree it made behind, and git branch -d refuses one with unmerged
// commits, so only empty ones go. Any other kind of workspace is refused: a
// stray path must never stop and forget a project or a chat. Neither Force
// nor Lose, so uncommitted changes or commits on no branch keep the folder.
func (wb *workbench) removeAgentWorktree(id string) error {
	x, ok := wb.records.WorkspaceRef(id)
	if !ok {
		return fmt.Errorf("workspace %q: %w", id, errNotFound)
	}
	if x.Kind != kindWorktree {
		return fmt.Errorf("%s is the workspace %q, not a worktree", tildePath(x.Dir), x.Name)
	}
	if _, err := wb.records.RemoveWorkspace(id, RemoveOptions{Files: true}); err != nil {
		return err
	}
	if repo := wb.records.ProjectDir(x.Project); repo != "" && x.Branch != "" {
		if note := deleteMergedBranch(repo, x.Branch); note != "" {
			slog.Info("agent worktree removed", "dir", x.Dir, "note", note)
		}
	}
	wb.stopOrphans()
	return nil
}

// subagentWorktreeGone removes what a subagent run with isolation "worktree"
// leaves: Claude asks for its folder through WorktreeCreate as
// agent-<agent_id> and never removes it, so every subagent would leave a row
// and a branch behind. Only a clean worktree of this tab's whose branch holds
// no commit of its own goes; anything the subagent made stays for the user.
func (wb *workbench) subagentWorktreeGone(tabID string, ev hookEvent) {
	if ev.AgentID == "" || ev.Cwd == "" {
		return
	}
	place, ok := wb.records.Place(tabID)
	if !ok {
		return
	}
	id := workspaceIn(wb.records.WorkspaceByDir(), ev.Cwd)
	if id == "" {
		return
	}
	x, ok := wb.records.WorkspaceRef(id)
	if !ok || !x.Auto || x.Kind != kindWorktree || x.Parent != place.workspaceID ||
		x.Name != "agent-"+ev.AgentID || x.Branch == "" {
		return
	}
	out, err := exec.Command("git", "-C", x.Dir, "status", "--porcelain").Output()
	if err != nil || len(out) > 0 {
		return
	}
	// What git branch -d asks: nothing on the branch the tab's folder does
	// not have already.
	if exec.Command("git", "-C", place.dir, "merge-base", "--is-ancestor", x.Branch, "HEAD").Run() != nil {
		return
	}
	if err := wb.removeAgentWorktree(id); err != nil {
		log.Printf("subagent worktree %s: %v", tildePath(x.Dir), err)
		return
	}
	log.Printf("subagent %s left %s unchanged: its worktree is removed", ev.AgentID, tildePath(x.Dir))
}

/* ---------- the hook process ---------- */

// worktreeHookTimeout bounds the wait on punk0: git worktree add on a large
// repository takes seconds, and Claude allows a command hook 600 s.
const worktreeHookTimeout = 60 * time.Second

// worktreeHook answers WorktreeCreate and WorktreeRemove. punk0 makes the
// worktree, so it is listed under the tab; when punk0 cannot be reached, git
// makes it here in the same layout. The folder goes to stdout, where Claude
// reads the last non-empty line; a refusal goes to stderr with status 1,
// which Claude shows and takes as "no worktree".
func worktreeHook(ev hookEvent, body []byte, target string, stdout, stderr io.Writer) int {
	create := ev.Name == worktreeCreateEvent
	if target != "" {
		reply, err := postWorktreeHook(target, body)
		switch {
		case err != nil:
			// punk0 is stopped or starting: git below.
			hookLog(slog.LevelWarn, ev.Name, "worktree hook could not reach punk0", "err", err)
		case reply.status/100 == 2:
			if !create {
				return 0
			}
			if reply.Path == "" {
				fmt.Fprintln(stderr, "punk0 answered without a folder: restart punk0 and try again")
				hookLog(slog.LevelWarn, ev.Name, "worktree hook got no folder from punk0")
				return 1
			}
			fmt.Fprintln(stdout, reply.Path)
			return 0
		case reply.status == http.StatusNotFound && !create:
			// punk0 has no row for it; git takes the folder away below.
		default:
			fmt.Fprintln(stderr, cmp.Or(reply.Error, fmt.Sprintf("punk0 refused: %d", reply.status)))
			return 1
		}
	}
	if create {
		return createWorktreeLocally(ev, stdout, stderr)
	}
	return removeWorktreeLocally(ev, stderr)
}

// worktreeReply is what punk0 answers a worktree event with.
type worktreeReply struct {
	Path   string `json:"path"`
	Error  string `json:"error"`
	status int
}

// postWorktreeHook asks the running punk0. An error is a transport one only:
// a refusal comes back in the reply's status.
func postWorktreeHook(target string, body []byte) (worktreeReply, error) {
	var reply worktreeReply
	u, err := url.Parse(target)
	if err != nil {
		return reply, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), worktreeHookTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return reply, err
	}
	req.Header.Set("Content-Type", "application/json")
	// The parent checks Origin on every write, like it does for its page.
	req.Header.Set("Origin", u.Scheme+"://"+u.Host)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return reply, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, hookBodyMax))
	if err != nil {
		return reply, err
	}
	json.Unmarshal(data, &reply) // a reply that is not JSON leaves the status to speak
	reply.status = resp.StatusCode
	return reply, nil
}

// createWorktreeLocally makes the worktree git-side, in the layout punk0
// would have used, so a stopped punk0 never breaks `claude --worktree`. The
// tab's next hook event, once punk0 runs again, lists it under its agent
// (attribution.go); until then the New workspace panel offers it.
func createWorktreeLocally(ev hookEvent, stdout, stderr io.Writer) int {
	if ev.WorktreeName == "" {
		fmt.Fprintln(stderr, "a worktree needs a name")
		return 1
	}
	top := gitProbe(ev.Cwd).toplevel
	if top == "" {
		fmt.Fprintf(stderr, "%s is not a git repository\n", tildePath(ev.Cwd))
		return 1
	}
	root, err := codeRoot()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	dirs := filepath.Join(root, "workspaces", localProjectID(top))
	if err := os.MkdirAll(dirs, 0o700); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	folder := folderName(ev.WorktreeName)
	dir := filepath.Join(dirs, folder)
	for n := 2; exists(dir); n++ {
		dir = filepath.Join(dirs, folder+"-"+strconv.Itoa(n))
	}
	out, err := exec.Command("git", "-C", top, "worktree", "add", "-b", worktreeBranch(ev.WorktreeName), dir, "HEAD").CombinedOutput()
	if err != nil {
		fmt.Fprintln(stderr, gitError(out, err))
		hookLog(slog.LevelWarn, ev.Name, "worktree hook could not make the worktree", "dir", dir, "err", gitError(out, err))
		return 1
	}
	hookLog(slog.LevelInfo, ev.Name, "worktree made without punk0", "dir", dir)
	fmt.Fprintln(stdout, dir)
	return 0
}

// removeWorktreeLocally takes the folder away with git, keeping one with
// changes in it: without --force git refuses a dirty worktree, and Claude
// then keeps it.
func removeWorktreeLocally(ev hookEvent, stderr io.Writer) int {
	if ev.WorktreePath == "" {
		fmt.Fprintln(stderr, "WorktreeRemove without a worktree_path")
		return 1
	}
	out, err := exec.Command("git", "-C", ev.WorktreePath, "worktree", "remove", ev.WorktreePath).CombinedOutput()
	if err != nil {
		fmt.Fprintln(stderr, gitError(out, err))
		hookLog(slog.LevelWarn, ev.Name, "worktree hook could not remove the worktree", "dir", ev.WorktreePath, "err", gitError(out, err))
		return 1
	}
	hookLog(slog.LevelInfo, ev.Name, "worktree removed without punk0", "dir", ev.WorktreePath)
	return 0
}

// localProjectID is the project folder top's worktrees go under, read
// straight from records.json: the hook has its own process, beside a punk0
// that may be stopped, and takes no lock. A repository punk0 does not know
// goes under its folder's name.
func localProjectID(top string) string {
	if data, err := os.ReadFile(filepath.Join(workbenchStateDir(), "records.json")); err == nil {
		var f recordsFile
		if json.Unmarshal(data, &f) == nil {
			for _, p := range f.Projects {
				if !p.Scratch && sameDir(p.Dir, top) {
					return p.ID
				}
			}
		}
	}
	return folderName(filepath.Base(top))
}

// hookLog writes to punk0's log file, the only place a hook may say
// anything: its stderr would show up in the agent's chat.
func hookLog(level slog.Level, event, msg string, args ...any) {
	applog.Append(workbenchStateDir(), level, msg, append([]any{"event", event}, args...)...)
}
