# Agents Page (Orchestrator)

Running `punk0` with no arguments opens the **agents page** at `/`: your projects and their workspaces on the left, the selected workspace's terminal and file tabs in the middle, and its file tree on the right. Each folder's full editor (px0's single-folder view) opens in a browser tab of its own.

---

## Starting punk0

| Command | Effect |
| :--- | :--- |
| `punk0` | Starts punk0 and opens the agents page. |
| `punk0 <dir>`, `punk0 <file>[:line]` | Adds the folder (a repository's top level) as a project and opens its editor view at `/w/<id>/`. If punk0 is not running yet, it starts and opens both the agents page and the editor. |
| `punk0 <pr-url>` | Reviews a pull request on its own, outside the agents page (see [Pull Request Review](github-pr-review.md)). |

Only one punk0 runs per machine. A second `punk0` or `punk0 <dir>` hands its folder to the running one, opens the page there and exits; `-host` and `-port` only apply to the first.

## Sidebar

- **Projects.** **+** at the top takes a folder path (with folder completion: **Tab** completes, **↑/↓** choose, **Esc** dismisses) or a pull request URL. A folder inside a git repository adds the repository. A new project starts with one local workspace holding a shell tab. ✕ removes the project from punk0 and stops its tabs; no files are deleted, and its worktrees come back if it is added again.
- **Workspaces.** **+** on a project creates one. A local workspace works in the project folder itself. Tick the worktree box (git projects only) to give it its own `git worktree` on a new branch, optionally from a base you name. ✕ deletes a workspace: its tabs stop, a worktree folder is removed (punk0 asks again if it has uncommitted changes) and the branch is kept.
- **Scratch.** Chats that belong to no project, each in its own folder under the state directory.
- **Reviews.** Pull requests opened from the **+** box or from the editor's **Git: Open Pull Request…**. Click one to open its review; ■ stops it, ✕ removes it from the list.

Each workspace row shows its editor's state (stopped, starting, running, failed), a badge with the number of running tabs or the most urgent agent status, and its actions: **LSP** (see below), open in editor, delete.

## Tabs

The tab bar holds the selected workspace's terminals and open files. **+** offers a new Claude Code tab, a shell tab, or a file picker that fuzzy-finds a file of the workspace. Clicking a file in the right-hand tree also opens it. File tabs are read-only views; the editor view is for editing. Double-click a terminal tab to rename it.

- **Terminals are real.** Each terminal tab is your shell in a PTY, started when you first open the tab. A Claude tab runs `claude` in that shell.
- **They outlive the browser.** Closing or reloading the page leaves every terminal running; reattaching replays its recent output. Everything stops when punk0 stops (Ctrl-C). After a restart, a Claude tab resumes its own chat.
- **Agent status.** Claude tabs report what the agent is doing through Claude Code hooks. A dot on the tab shows **working**, **waiting** (needs you: a permission prompt or a question) or **idle** (done).
- **Idle stop.** A terminal that has carried nothing for `terminal.idleStop` minutes (default 30, `0` never) is stopped: a shell only while it sits at its prompt with nothing running in it (Unix only; elsewhere shells are never stopped), a Claude tab only when it is idle. Opening the tab again starts it again.

## Editor view

The editor icon on a workspace row opens the folder in px0's full editor at `/w/<id>/`, in a browser tab named for that folder, so opening it again reuses the tab. The address is stable, so a bookmarked or reloaded editor reopens. The ▦ button in the explorer header goes back to the agents page, in its own window.

Workspaces that share a folder share one editor process. It starts on the first request for the folder and is stopped after `workspace.idleStop` minutes (default 5, `0` never) with nothing open; the next request starts it again.

## Language servers

Language servers are **off** by default for every folder: they cost hundreds of megabytes to gigabytes each (gopls measured at about 260–316 MB per folder, rust-analyzer at 852 MB steady and 1.5 GB peak on a small Anchor project). The **LSP** button on a workspace row turns them on or off for its folder, which restarts that folder's editor. The choice is saved. Starting punk0 with `-no-lsp` keeps them off everywhere.

## Pull requests

A pull request always opens as a `git worktree` of a local clone of its repository: a separate checkout that shares the clone's history, so opening a PR costs a checkout and never a fresh clone. The PR is checked out again each time it is opened, so it is always current, and the worktree is removed when its editor stops. Your clone's own branch and working tree are not touched. Unsubmitted review drafts are kept across reopenings.

The first time you open a PR of a repository, punk0 finds the clone:

1. a clone you used for that repository before;
2. the folder punk0 was started in;
3. a quick scan (a few levels deep, at most 1.5 seconds) of the usual code folders in your home directory (`~/workspace`, `~/code`, `~/src`, `~/projects`, `~/dev`, `~/repos`, `~/go/src/github.com`, ...) and the folders around the one punk0 was started in.

A clone matches when any of its remotes (`origin`, `upstream` for a fork, or another) points at the PR's repository. The clone is remembered, so every later PR of that repository opens straight away.

If no clone is found, the sidebar asks. You can **use your clone**: type its path (with folder completion). Or you can **let punk0 clone it**: punk0 makes one partial clone under `<state>/repos/<owner>/<repo>`, without a checkout of its own, and reuses it for every later PR of that repository.

A plain `punk0 <pr-url>` run outside the repository uses the remembered clone too.

If a pull request cannot be opened (a typo in the URL, no access), the error is shown and the entry is left out of the list.

## Status bar and themes

The bottom bar shows the version, what is running, and the **CPU · RAM · LSP** chip. The chip adds up punk0 and every running editor; click it for a breakdown by process. The palette button opens the theme list. Arrow keys preview a theme, Enter or a click keeps it, and Esc restores the one you had. The choice is saved to settings, so the editors use it too.

## Security

Terminals are shells on your machine, so they are served only while punk0 listens on loopback (the default `-host 127.0.0.1`); otherwise opening one is refused. The page can also list and open **any directory the punk0 process can read**. With `-host 0.0.0.0`, anyone who can reach the address can browse the machine's folders and open any of them for reading and editing, and punk0 prints a warning saying so. Expose it only over networks you trust. The same DNS rebinding and same-origin checks as the rest of punk0 apply, including to the agents' hook reports.

## Where state lives

Everything is kept in the state directory, `$XDG_STATE_HOME/punk0`, falling back to `~/.punk0`:

| Path | Holds |
| :--- | :--- |
| `records.json` | Projects, workspaces, tabs, and the folders language servers are on in |
| `worktrees/<project>/<name>` | Worktree workspaces |
| `scratch/<name>` | Scratch chats |
| `workbench.json` | Pull requests in the Reviews list, and the clone each repository's PRs use |
| `repos/` | Clones punk0 makes for PRs |
| `claude-hooks.json` | The hook settings Claude tabs start with |
| `workbench.lock` | The running punk0's address |

Nothing is written inside your repositories, apart from worktree registrations (worktree workspaces, and temporary ones for PRs) and the `refs/px0/*` refs that PR review uses and removes on exit.

## Flags

`-no-lsp`, `-no-git`, `-no-agent`, `-no-telemetry` and `-agent` are passed on to every editor punk0 starts. `-host`, `-port`, `-base-path` and `-no-open` apply to punk0 itself.
