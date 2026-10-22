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

## Layout

Three panes: projects on the left, the selected workspace's tabs in the middle, its files on the right. Drag either border to resize its side pane: the projects pane from 200 to 400 px, the files pane from 200 px up to whatever leaves the middle 360 px. The status bar's left section follows the projects pane. Double-click a border, or focus it and press Enter, to go back to 260 px; with a border focused, the arrow keys move it 16 px (64 px with Shift) and Home / End jump to its limits. Widths are remembered in this browser, and a narrower window shrinks the files pane first. While you drag, a terminal keeps its size and refits once the pointer rests, so an agent's screen is not redrawn at every pixel.

Either side pane can be hidden: the panel button at its inner edge (the right end of the projects pane's header, the left end of the files pane's) hides it, and a button at that end of the tab bar brings it back. **Ctrl+B** (⌘B on macOS) toggles the projects pane and **Ctrl+Alt+B** (⌘⌥B) the files pane, except while a terminal has focus, where Ctrl+B belongs to the shell. Which panes are hidden is remembered with their widths.

## Sidebar

The **Projects | Scratch** switch at the top picks what the pane lists. Under it, a search field narrows the list by project, workspace, branch, chat or group name; **Esc** clears it. Each view keeps its own search. Below the search, the list's header has **View options**, then two buttons: in Projects a new-workspace button and **+**, in Scratch a new-group button and **+** for a new chat.

**View options** (the sliders button) chooses how the view is listed; the choices are kept in this browser, per view:

| | Projects | Scratch |
| :--- | :--- | :--- |
| **Group by** | Project (default), Status, None | Group (default), Date, Agent, Status, None |
| **Sort** | | Last used (default), Created, Name |
| **Show** | Each project | Each agent your chats run |
| **Filters** | Hide sleeping (nothing running; a project's local workspace stays), Hide local workspaces, Hide missing folders, Hide reviews | Hide sleeping, Hide chats in my folders, Show archived |

**Status** groups by needs you, working, idle, running and stopped. The button shows how many filters are on, and **Clear filters** turns them all off, leaving grouping and sorting as they are. A search still finds rows the filters hide, and the selected workspace always shows.

- **Projects.** **+** opens the **Add project** panel beside the sidebar (below). A folder inside a git repository adds the repository. A new project starts with one local workspace and no tabs open. Click a project's name to fold it. ⟳ makes punk0 look at the folder again right away (after `git init` or other changes made outside punk0; it notices on its own within seconds) and restarts the folder's editor. ✕ removes the project from punk0 and stops its tabs; no files are deleted, and its worktrees come back if it is added again.
- **Workspaces.** The header's new-workspace button (in the selected workspace's project) and **+** on a project's row open the **New workspace** panel beside the sidebar. ✕ deletes a workspace: its tabs stop, a worktree folder is removed (punk0 asks again if it has uncommitted changes) and the branch is kept. The selected workspace's folder opens in the full editor from the button at the right end of the tab bar.
- **Reviews.** Listed under the projects: pull requests opened from **Add project** or from the editor's **Git: Open Pull Request…**. Click one to open its review; ■ stops it, ✕ removes it from the list.

### Add project

The panel opens on a short menu: a **Folder** field, **Browse folder**, **Clone from URL**, **Create new project** and **Recently removed**. Browse, Clone and Create open their own step with a folder browser; ← or **Esc** goes back to the menu.

- **Folder.** Type a path (**Tab** completes, **↑/↓** choose, **Esc** dismisses), or choose **Browse folder** and open folders in the browser; the field follows the folder you open. The line under it says what **Add** will do: add a git repository (or the repository the folder is inside), select a folder that already is a project (**Open**), or add a plain folder ("Not a git repository yet": once you `git init` there, punk0 notices within seconds and restarts the folder's editor so it shows git). The browser starts in the folder your last project went in, the first time in the one most of your projects are in.
- **Several repositories.** Browsing a folder that holds git repositories, each has a checkbox; tick some and **Add N projects** adds each as a project of its own. Folders that already are projects are marked *added*.
- **Pull requests.** A pull request URL in the folder field opens it for review. When punk0 knows no local clone of its repository it asks for one, or clones it once under its state folder.
- **Recently removed.** The last ten projects you removed whose folders still exist, with how many of punk0's worktrees come back with them. Click one to add it again.
- **Clone from URL.** A git URL and the folder to clone into, picked in the browser (it starts where the last project went); the clone is named like the repository. It is a full checkout whose older file contents download when needed (`--filter=blob:none`). Progress shows while it runs; **Cancel clone**, **Esc** or ✕ stops it and deletes what was cloned. Clicking outside does not close the panel while a clone runs.
- **Create new project.** A name and the folder to make it in: punk0 makes the folder and runs `git init`, nothing more. Until its first commit it cannot have worktrees, and **New workspace** offers only **Project folder**.

### New workspace

- **Where.** **New worktree** gives the workspace its own folder (`<state>/worktrees/<project>/<branch>`, `/` in the branch becoming `-`) and branch. **Project folder** runs it in the project folder itself, on whatever branch that is on; a folder that is not a git repository only has this.
- **Name or start from.** A name ("Fix login bug") becomes a new branch (`fix-login-bug`). An existing local branch is checked out as it is; a remote branch (`origin/feature` or just `feature` when only the remote has it) gets a local branch tracking it. `#123` or a pull request URL of the project's repository looks the PR up on its forge: **Review** opens it in the editor's review view, **Work on it** checks out its branch in a worktree (`pr-123` for a fork's).
- **from.** What a new branch starts from: any branch, tag or commit, with the repository's branches offered. Left empty it is the remote's default branch (`origin/main`), not whatever the project folder has checked out. A remote branch is fetched first (30 s at most; if that fails the workspace starts from your copy and punk0 says so). New branches do not track their start point, so a push goes to their own name.
- **The line under it** says what Create will do (new branch from where, existing branch, tracking, the folder) or why it cannot, such as a branch already checked out in another worktree, before anything is made.
- **Agent.** The first tab: an installed agent, a shell, or none (the welcome view). **Language servers in this folder** turns them on for the new worktree (each costs 300 MB to 1.5 GB).
- **Ctrl+Enter** creates. Agent, Where, start point and language servers are remembered per project, in this browser.

Each workspace row shows its editor's state (stopped, starting, running, failed), a badge with the number of running tabs or the most urgent agent status, and, on hover, delete.

### Scratch chats

Scratch is for agent work that has no repository yet: research, one-off scripts, prototypes. Each chat is a coding agent in a terminal tab, in a folder of its own under the state directory or in a folder of yours, so it can write and run files without touching a project.

- **Starting.** **+** in the header, **New chat**, or **+** on a group's row opens the **New chat** flyout just right of the sidebar, pointing at the button you pressed. **Enter** starts the chat; **Esc**, ✕ or a click elsewhere closes it.
  - **Title.** Optional. Left empty, a Claude chat is named after the first prompt you send (not a `/command`, cut to about 60 characters), any other after its folder.
  - **Agent.** Every agent punk0 knows that is on your `PATH`: Claude Code, Codex, OpenCode, Antigravity, Cursor Agent, or a plain shell; the one you used last is picked. Claude Code is the agent punk0 follows closely: its tab shows working / needs you / idle, and it resumes its exact conversation after a restart. The others show no status; after a restart Codex picks up the folder's latest session and OpenCode its latest one, the rest start a new one. The flyout says which applies.
  - **Group.** **Ungrouped** or one of your groups; preselected when you start from a group's **+**.
  - **Folder.** Where the agent runs. It starts as a new folder in punk0's scratch folder named after the title (`photo-renamer`), or after the time while there is none (`chat-1021-1432`); such a folder must not exist yet and is deleted with the chat. Type another path (with folder completion) or press **Browse**: click a folder to open it, ← to go back to its parent, **Use this folder** to take the one shown, or **New folder here** to name one that is made when the chat starts. Any path outside the scratch folder is a folder of yours: made if it does not exist, never deleted by punk0, and shown by name next to the chat's title.
- **Pinning.** The pin on a chat's row, or **Pin to the top** in its ⋯ menu, lists it under **Pinned** above everything else, wherever it is filed; pin it again to put it back.
- **Names.** Double-click a chat, or use **Rename…** in its ⋯ menu, to rename it. Renaming changes the label only: the folder keeps its name, because agents find a chat's history by the folder it ran in.
- **Groups.** The new-group button adds a group at the top of the list with its name field ready: **Enter** or a click elsewhere makes it, **Esc** cancels, and a name already used is refused under the field. **New group…** in a chat's ⋯ menu does the same and moves the chat into it; the menu also moves a chat into a group or out of one. Double-click a group's name, or use its pencil, to rename it the same way; ✕ on its row deletes the group and keeps its chats, in no group. Click a group to fold it. Groups show while the list is grouped by Group; making one switches to it.
- **Archiving.** **Archive** in the ⋯ menu stops the chat's tabs and editor and hides it, keeping its folder and conversation. **Show archived** in View options lists archived chats in a folded **Archived** section at the bottom, as a search does; ↺ restores one. The trash button there deletes the chat, and with it the folder punk0 made for it; a folder of yours is kept. The agent's own copy of the conversation (Claude's under `~/.claude`) is not touched.
- **Order.** Pinned chats come first, then the rest grouped and sorted as View options says: by default the groups, then the chats in no group under **Today**, **Previous 7 days** and **Older**, most recently used first.

## Tabs

The tab bar holds the selected workspace's terminals and open files. **+** offers a new tab for every installed agent not hidden in settings (`agents.hidden`) and a shell, with `agents.default` first, then a file picker that fuzzy-finds a file of the workspace. Clicking a file in the right-hand tree also opens it. File tabs are read-only views; the editor view is for editing. Double-click a terminal tab to rename it. Closing a running terminal asks first unless `terminal.confirmCloseRunning` is off.

With no tab open, as in a new project or worktree workspace, the center shows the welcome view: punk0's name and version, the workspace and its folder, and the same choices as **+**, the default agent first, then a file. Nothing starts until you pick one. A scratch chat opens on the tab its agent runs in.

- **Terminals are real.** Each terminal tab is your shell in a PTY, started when you first open the tab. A Claude tab runs `claude` in that shell.
- **They outlive the browser.** Closing or reloading the page leaves every terminal running; reattaching replays its recent output. Everything stops when punk0 stops (Ctrl-C). After a restart, a Claude tab resumes its own chat.
- **Agent status.** Claude tabs report what the agent is doing through Claude Code hooks. A dot on the tab shows **working**, **waiting** (needs you: a permission prompt or a question) or **idle** (done).
- **Terminal settings.** Font, line height, scrollback, cursor and copy-on-select (`terminal.*`) apply to the open terminal as soon as they are saved; `terminal.shell` and `terminal.env` apply to tabs started afterwards.
- **Idle stop.** A terminal that has carried nothing for `terminal.idleStop` minutes (default 30, `0` never) is stopped: a shell only while it sits at its prompt with nothing running in it (Unix only; elsewhere shells are never stopped), a Claude tab only when it is idle. Opening the tab again starts it again.

## Notifications

While the agents page is open, even in a background browser tab, punk0 can tell you when a tab with status starts **waiting** for you or goes from **working** to **idle**: a browser notification (`notifications.desktop`: off, only when the page is not focused, or always), a short tone (`notifications.sound`), and the number of waiting tabs in the page title (`notifications.titleBadge`, on by default). `notifications.onWaiting` and `notifications.onDone` pick which changes count. Clicking a notification focuses the page and selects that tab. Browser notifications need https or localhost; turning them on in settings asks for permission. With no agents page open, nothing notifies.

## Editor view

The editor icon at the right end of the tab bar opens the selected workspace's folder in px0's full editor at `/w/<id>/`; a project workspace's row has the same icon. It opens in a browser tab named for that folder, so opening it again reuses the tab. The address is stable, so a bookmarked or reloaded editor reopens. The ▦ button in the explorer header goes back to the agents page, in its own window.

Workspaces that share a folder share one editor process. It starts on the first request for the folder and is stopped after `workspace.idleStop` minutes (default 5, `0` never) with nothing open; the next request starts it again.

## Language servers

Language servers are **off** by default for every folder: they cost hundreds of megabytes to gigabytes each (gopls measured at about 260–316 MB per folder, rust-analyzer at 852 MB steady and 1.5 GB peak on a small Anchor project). The **LSP** item in the status bar, at the right corner of the middle column, is the selected folder's, as in the editor's status bar: its dot is grey while they are off (or on but the folder's editor is not running), green while that editor runs with them. Click it for the menu: **Turn on** / **Turn off** saves the choice for the folder and restarts its editor; while on and the editor runs, its servers are listed with **Start** / **Stop** (installing one is done in the editor). Starting punk0 with `-no-lsp` keeps them off everywhere.

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

## Status bar and help

The bottom bar shows the logo and version, then the help, GitHub and settings buttons on the left. On the right, one button shows the running terminals (`>_`), running agent tabs (robot icon) and the total RAM of punk0 and everything it started. Click it for the resource panel: CPU and RAM for each workspace with a recent-CPU sparkline, then its editor (language servers included) and each terminal (the shell and everything running in it, such as an agent and the tools it starts), then pull request reviews, then punk0 itself. Click a workspace to fold it; Esc or a click elsewhere closes the panel. The **?** button opens the agents page's keyboard shortcuts; Esc or a click closes them. The gear, or Ctrl/Cmd+, outside a terminal, opens the settings on this page, the same settings the editor shows, also with no project added. Theme, interface font and terminal settings apply as soon as they are saved.

## Security

Terminals are shells on your machine, so they are served only while punk0 listens on loopback (the default `-host 127.0.0.1`); otherwise opening one is refused. The page can also list and open **any directory the punk0 process can read**. With `-host 0.0.0.0`, anyone who can reach the address can browse the machine's folders and open any of them for reading and editing, and punk0 prints a warning saying so. Expose it only over networks you trust. The same DNS rebinding and same-origin checks as the rest of punk0 apply, including to the agents' hook reports.

## Where state lives

Everything is kept in the state directory, `$XDG_STATE_HOME/punk0`, falling back to `~/.punk0`:

| Path | Holds |
| :--- | :--- |
| `records.json` | Projects, workspaces, tabs, and the folders language servers are on in |
| `worktrees/<project>/<name>` | Worktree workspaces |
| `scratch/<name>` | Scratch chats, one folder each, deleted only with the chat |
| `workbench.json` | Pull requests in the Reviews list, and the clone each repository's PRs use |
| `repos/` | Clones punk0 makes for PRs |
| `claude-hooks.json` | The hook settings Claude tabs start with |
| `workbench.lock` | The running punk0's address |

Nothing is written inside your repositories, apart from worktree registrations (worktree workspaces, and temporary ones for PRs) and the `refs/px0/*` refs that PR review uses and removes on exit.

## Flags

`-no-lsp`, `-no-git`, `-no-agent`, `-no-telemetry` and `-agent` are passed on to every editor punk0 starts. `-host`, `-port`, `-base-path` and `-no-open` apply to punk0 itself.
