# Agents Page (Orchestrator)

Running `punk0` with no arguments opens the **agents page** at `/`: your projects and their workspaces on the left, the selected workspace's terminal and file tabs in the middle, and its file tree on the right. Each folder's full editor (px0's single-folder view) opens in a browser tab of its own.

---

## Starting punk0

| Command | Effect |
| :--- | :--- |
| `punk0` | Starts punk0 and opens the agents page on the workspace you selected last. |
| `punk0 <dir>` | Adds the folder (a repository's top level) as a project and opens the agents page with it selected. The editor opens only when you ask for it (**Open in editor**). |
| `punk0 <file>[:line]` | The same, and also opens the folder's editor view at `/w/<id>/` on that file and line. |
| `punk0 <pr-url>` | Reviews a pull request on its own, outside the agents page (see [Pull Request Review](github-pr-review.md)). |

Only one punk0 runs per machine. A second `punk0 <dir>` hands its folder to the running one and exits: an agents page already open in a browser switches to the folder, and only when none is open does a new browser tab open on it. A bare second `punk0` opens the page. `-host` and `-port` only apply to the first.

### In the terminal

At start punk0 prints its version, the agents page URL, and for `punk0 <dir>` the project it opened (`project  portfolio  ~/Code/projects/portfolio`). The browser is opened at `?ws=<id>` so the page lands on that project; the printed URL leaves it out unless no browser opens (`-no-open`). A file adds an `editor` line. Then, one timestamped line per event, always naming the project and workspace:

```
21:40  + opened   punk0  from another terminal
21:41  ▶ claude   punk0 / ac
22:12  ■ claude   punk0 / ac  idle 30 min
22:15  ■ editor   portfolio  idle 5 min
22:20  ■ shell    portfolio  exited
```

`+` is a folder handed over by a second `punk0 <dir>`, `▶` a tab started, `■` a tab or an editor process stopped for being idle, or a tab whose shell exited on its own. Problems are marked `!`. Editor processes starting and stopping as you browse, and tabs you close yourself, are not printed; `-v` adds one line per HTTP request. Keys typed into that terminal are not echoed while punk0 runs; echo comes back when it stops.

## Layout

Three panes: projects on the left, the selected workspace's tabs in the middle, its files on the right. Drag either border to resize its side pane: the projects pane from 200 to 400 px, the files pane from 200 px up to whatever leaves the middle 360 px. The status bar's left and right sections stay 260 px wide, the panes' opening width, whatever the panes' widths; the editor's status bar and side panels use the same 260 px. Double-click a border, or focus it and press Enter, to go back to 260 px; with a border focused, the arrow keys move it 16 px (64 px with Shift) and Home / End jump to its limits. Widths are remembered in this browser, and a narrower window shrinks the files pane first. While you drag, a terminal keeps its size and refits once the pointer rests, so an agent's screen is not redrawn at every pixel.

Either side pane can be hidden by a button in its header: the panel button at the right end of the projects pane's header, and the filled ≡ button at the right end of the files tree's head (the second row), which hides the tree only. A hidden projects pane comes back from a button at the left end of the middle column's tab bar; a hidden tree from a ≡ button at the same place, the right end of the second row: the end of the file tabs while the file panel shows, else the right end of the terminals' top row. ◫ closes everything right of the terminals at once (the open files and the files pane, or the chats); when any of it is closed, it opens it all again. While the right side is closed, ◫ and **Open in editor** sit at the right end of the terminals' top row. **Ctrl+B** (⌘B on macOS) toggles the projects pane and **Ctrl+Alt+B** (⌘⌥B) does what ◫ does, except while a terminal has focus, where Ctrl+B belongs to the shell. Which panes are hidden is remembered with their widths.

### The right side: Files and Chats

Everything right of the terminals — the file panel and the files pane — is one side with its own top row: at its left the mode switch, **Files** (the files icon) and **Chats** (the chat bubble); at its right **Maximize** (Chats mode, or Files mode with the file panel open), **Open in editor** and ◫. With no file open the row sits over the files pane alone. The two modes never share the side: **Files** shows the open files and the tree; **Chats** hides both and takes the whole side for the list of your agents' chats. The mode is remembered in this browser; the arrow keys, Home and End move along the switch.

### Files beside the terminals

By default, open files sit in a panel of their own right of the terminals, as in VS Code's Agents window, so you can read code while an agent works. Each side has its own tab bar: terminals and agent chats on the left; on the right the side's top row, then the open files' tabs in a second row, ending with **+** (open a file), and the open file's path in the third. The file's line count is in the status bar. The panel appears with the first open file and goes when the last one closes.

In Files mode the files pane starts at the second row, level with the file tabs: its head is the changed / all files switch, refresh, collapse all folders and ≡. In the tabs layout (below) the side is the files pane alone, with the same top row.

- **Resize.** Drag the border between them. Each side keeps at least 320 px, and the split keeps its proportion when the window or a side pane changes width. Double-click the border, or focus it and press Enter, for half and half; the arrow keys move it 16 px (64 px with Shift), Home / End to its limits.
- **Collapse.** Drag the border most of the way into one side and that side collapses; drag back before letting go to keep it. A collapsed side comes back at the width it had before the drag.
- **Maximize** gives the files the whole middle column; the same button restores the terminals. Dragging the border collapses the files; ◫ shows them again. Opening a file (tree, picker, **+**) shows a hidden panel, and switching to a terminal tab (the needs-you button, a notification) shows maximized-away terminals. In Chats mode **Maximize** gives the chats the whole middle column (with or without open files) and the same button brings the terminals back; switching to Files keeps the files maximized if any are open. With no file open Files mode has no Maximize: the tree alone would only stretch.
- **Narrow windows.** While the terminals and the open files have less than 640 px between them (the files pane not counted), files go back among the terminal tabs, and return to their panel once there is room.

Settings → Appearance → **File View** (`layout.fileView`): `beside` (the default) or `tabs`, which always keeps files among the terminal tabs, one in front at a time. The split, what is collapsed and each workspace's last terminal and file tab are remembered in this browser.

### Changed files and the diff

In a git checkout the files pane lists only the files changed against `HEAD`, untracked ones included, with their folders opened. The switch in its head (folder | branch) shows all files instead; the choice is remembered in this browser, and a folder without git shows all files with the switch's second button off. The pane follows git as files change, while the page is in view and the pane shown; Refresh re-reads it at any time.

Clicking a file in the changed-files view opens it as its diff: one tab per file, as in the editor. In a git checkout the path row of a file tab has a switch between the file's text and its diff; a file opened from all files keeps what its tab showed, the text at first.

**± All changes N**, the first row of the changed-files view while anything changed (N is how many files), opens a **Changes** tab first in the file group: every changed file one after another, each under a header with its status, path and `+`/`−` lines; click a header to fold the file, or its ↗ button to open the file. Beside the terminals a diff is one column. **Maximize** shows it side by side, and a switch in the path row picks side by side or one column; the choice is remembered. A diff re-reads the changes each time it comes into view, and with the refresh button in the path row.

### Chats

Chats mode lists every chat your coding agents kept on disk, read live, so a conversation started in any terminal — not only in a punk0 tab — can be found again and picked up here. punk0 never writes these transcripts; it reads Claude Code's (`~/.claude`, or `CLAUDE_CONFIG_DIR`), Codex's (`~/.codex`, or `CODEX_HOME`) and asks OpenCode's own CLI (`opencode session list`). Chats whose folder punk0 cannot work out are left out, and `.jsonl.zst` rollouts are not read. OpenCode answers per folder, so **only OpenCode chats that ran in a folder punk0 knows — a project folder or one of its workspaces' — are listed**; the others are invisible to punk0 until you add their folder.

Its head reads **Chats**, then the scope tabs (**Workspace** | **Project** | **All**; the arrow keys, Home and End move along them), the agent dropdown, the search and a ⋯ menu. While the side is narrower than about 520 px the head stacks: **Chats** with an agent filter button (a funnel, badged **1** while one agent is picked; it opens a menu of All agents and each agent) and ⋯, then the tabs, then the search, each full width.

- **Scope.** **Workspace** is the selected workspace's folder, as one flat list; **Project** (the default) every folder of its project, grouped by workspace (a worktree's name followed by *(worktree)*; chats in the project's other folders under the project's name); **All** everything on the machine, grouped by project, with the chats of folders in no project last under **Other folders**. Groups come in order of their most recent chat. A group's header — chevron, name, count — folds it on a click, Enter or Space; groups start open except Other folders, and the ones you fold or unfold stay so in this browser. While a search is on, every group with matches shows open, without changing what is remembered. The scope is remembered in this browser.
- **Agent.** **All agents** or one agent's chats (Claude Code, Codex, OpenCode; only the agents with chats in the list are offered). The choice is remembered in this browser; while the list has no chats of the remembered agent it shows all.
- **Search.** Matches the chat's title, ignoring case; **Esc** clears it.
- **⋯** has **Sync**, which re-reads the agents' folders now and brings every removed chat back to the list, and **Show removed**, which lists the chats you removed where they are (their titles muted and in italics, with **Restore** instead of the trash).
- **A row.** One line: a dot (filled while a punk0 tab is on the chat, hollow otherwise), the title (the one you set in the agent, else the one it chose, else the first prompt), then the agent — followed by *· open* while a tab is on it — how long ago it was touched (`5m`, `3h`, `1d`) and a chevron. Hovering or focusing the row shows the **trash** over the agent and the age. A narrow pane drops the agent's name first.
- **The preview.** Click the row — or press Enter, Space or → on it — and it expands in place: how many messages it holds, its folder and its branch (none on a detached head), the **last exchange** — your last prompt (about three lines) and the agent's whole reply to it (about five, fading out; just the prompt if the agent has not answered yet) — then **Started with**, the first prompt that is more than a greeting, when it is neither the title nor that last prompt, then **Resume**, **Copy id** and **Remove…** (**Restore** instead, on a chat you removed). Clicking a row never starts an agent: resuming is a button, so a mis-click costs nothing. One row is expanded at a time; ← or **Esc** closes it, leaving the row focused, and it survives the ten-second reload. The text is read from the transcript when you open the row and is shown as plain text, cut where it gets long.
- **Resume.** The preview's **Resume** opens a tab on that exact conversation — `claude --resume <id>`, `codex resume <id>`, `opencode --session <id>` — in the workspace running in its folder, reusing the tab already on it rather than opening a second one. For a worktree punk0 does not know, it offers to add that worktree as a workspace, in place; for a folder outside every project, to add the folder as a project; then it opens the chat. A chat that ran somewhere punk0 cannot work in on its own — a subfolder of a project or of another repository, your home folder — says so and names the folder: resume it from a terminal there. A chat whose folder is gone does not resume.
- **Remove and delete.** The trash on the row, and **Remove…** in the preview, open one popup beside the row. Left as it is, **Remove** only takes the chat out of punk0's list: it stays on disk, its tabs keep running, **Show removed** finds it and **Sync** brings it back. Tick **Delete permanently** and the button turns into a red **Delete**: Codex and OpenCode delete the conversation with their own command (`codex delete`, `opencode session delete`), and Claude Code, which has none, has `<id>.jsonl` and its sibling `<id>/` folder deleted — that cannot be undone, and nothing goes to punk0's trash. Every tab on the chat closes first, stopping the agent in it, which the note under the box says as soon as you tick it.
- **Closing a tab.** The × on a chat's tab only stops it. The conversation stays on disk and in the list, its dot going hollow as the tab goes; **Resume** picks it up again under the same id.
- **Refreshing.** The list reloads every ten seconds while Chats mode is in front and the page is in view, and whenever you change the scope, the search or the selected workspace; **Sync** reloads it at once, from the files rather than from punk0's cache. OpenCode's sessions are fetched in the background, at most every ten seconds, since each folder costs a process; a chat you just deleted can come back for one of those rounds before it goes for good.

### Staging and the branch bar

Each changed file in the tree has a stage tick, as in the editor's tree: empty while unstaged, filled once staged. Click it to stage or unstage that one file.

Under the tree, in a git checkout, the branch bar: `⑂ branch  staged / changed  ↑a ↓b` — the branch, how many changed files are staged, and how far the branch is from its upstream (`↑` commits to push and `↓` commits to pull as of the last fetch, nothing when it is even, **unpublished** when it tracks no remote branch). Click it to open the workspace in the editor with its git panel unfolded (`/w/<id>/?panel=git`): committing by hand or with AI, push and pull, staging hunks and the full commit graph are there.

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

- **Projects.** **+** opens the **Add project** panel beside the sidebar (below). A folder inside a git repository adds the repository. A new project starts with one local workspace and no tabs open. Click a project's name to fold it. ⟳ makes punk0 look at the folder again right away (after `git init` or other changes made outside punk0; it notices on its own within seconds) and restarts the folder's editor. ✕ removes the project from punk0 and stops its tabs; a dialog asks what goes with it. **Also delete the project folder** is offered only for a project punk0 created or cloned itself, and only once its name is typed back: the folder is then moved to punk0's trash folder, never deleted outright, and punk0 refuses while the repository still has worktrees checked out or another project of yours sits inside it. Left alone, no files are deleted and the project's worktrees come back if it is added again; a project whose folder was trashed is not offered under **Recently removed**, since it cannot come back. The agents' own transcripts are never touched: they keep showing in Chats mode, where each is removed or deleted on its own.
- **Workspaces.** The header's new-workspace button (in the selected workspace's project) and **+** on a project's row open the **New workspace** panel beside the sidebar. A workspace row's ⋯ (or a right-click on it) offers **Rename…**, which changes only its name in the sidebar (unique in the project; folder and branch stay), and **Delete…**, which deletes the workspace after a dialog saying what goes with it: its tabs always stop. A worktree workspace also offers **Also delete the worktree folder** — ticked for worktrees punk0 made, clear for ones it adopted in place — which removes the folder with `git worktree remove` and keeps the branch (unless `workspace.deleteMergedBranch` is on); punk0 asks once more if it has uncommitted changes. Left unticked, punk0 only forgets the workspace: the folder, its branch and its git worktree registration stay, so adding the project again finds it. The chats that ran in its folder stay on disk and keep being listed in Chats mode, in **All** scope under **Other folders**. The selected workspace's folder opens in the full editor from the button before ◫ at the right end of the tab bars.
- **Worktrees an agent makes.** When an agent works in a git worktree of the project that no workspace runs in, it appears as a workspace under the workspace whose agent made it, nested to any depth; deleting a workspace moves the ones under it up a level. Other worktrees git lists that no workspace runs in are not shown in the sidebar: **New workspace** offers them in its **Branch** list, tagged `adopt`. When the folder of a worktree an agent made is deleted, its row goes within seconds and its chats are hidden under the workspace it was under: **Show removed** in the agents page's Chats mode, or **Sync**, brings them back.
- **Reviews.** Listed under the projects: pull requests opened from **Add project** or from the editor's **Git: Open Pull Request…**. Click one to open its review; ■ stops it, ✕ removes it from the list.

### Add project

The panel opens on a short menu: a **Folder** field, **Browse folder**, **Clone from URL**, **Create new project** and **Recently removed**. Browse, Clone and Create open their own step with a folder browser; ← or **Esc** goes back to the menu.

- **Folder.** Type a path (**Tab** completes, **↑/↓** choose, **Esc** dismisses), or choose **Browse folder** and open folders in the browser; the field follows the folder you open. The line under it says what **Add** will do: add a git repository (or the repository the folder is inside), select a folder that already is a project (**Open**), or add a plain folder ("Not a git repository yet": once you `git init` there, punk0 notices within seconds and restarts the folder's editor so it shows git). The browser starts in the folder your last project went in, the first time in the one most of your projects are in.
- **Several repositories.** Browsing a folder that holds git repositories, each has a checkbox; tick some and **Add N projects** adds each as a project of its own. Folders that already are projects are marked *added*.
- **Pull requests.** A pull request URL in the folder field opens it for review. When punk0 knows no local clone of its repository it asks for one, or clones it once under its state folder.
- **Recently removed.** The last ten projects you removed whose folders still exist, with how many of punk0's worktrees come back with them. Click one to add it again.
- **Clone from URL.** A git URL and the folder to clone into, picked in the browser (it starts where the last project went); the clone is named like the repository. It is a full checkout whose older file contents download when needed (`--filter=blob:none`). Progress shows while it runs; **Cancel clone**, **Esc** or ✕ stops it and deletes what was cloned. Clicking outside does not close the panel while a clone runs.
- **Create new project.** A name and the folder to make it in: punk0 makes the folder and runs `git init`, nothing more. Until its first commit it cannot have worktrees, and **New workspace** only makes workspaces in the project folder.

### New workspace

- **Name.** What the workspace is called in the sidebar. Required for a workspace in the project folder; with a new worktree it may be left empty and becomes the branch's name (a pull request's `#123 Title`), with `-2`, `-3`… when the project has that name already.
- **Branch.** What to work on. It opens on the project folder's current branch (tagged `current`; `detached at <commit>` when it has none). Clicking it, pressing **↓** or typing drops down a list over the form: the current branch, the project's worktrees (branch, or `detached` and its commit, tagged `open`, `adopt` or `deleted`, then the folder), then up to 50 of its other branches that match what you type. **↑/↓** choose, **Enter** or a click fills it in, **Esc** dismisses. A branch checked out in a worktree is only listed as that worktree. Any tag or commit can be typed, and so can `#123` or a pull request URL of the project's repository, which hides the list.
- **Create a new worktree.** Only the current branch can run in the project folder itself: unticked, the workspace runs there, beside the project's other workspaces in it. Ticked, it gets its own folder (`<state>/worktrees/<project>/<branch>`, `/` in the branch becoming `-`). Git checks a branch out in one folder at a time, so for any other branch, tag, commit or pull request the box is ticked and locked ("not checked out here"). For a worktree it is hidden: **Open** selects the workspace already running there (also when its folder was deleted; its ✕ cleans up), **Adopt** makes a worktree punk0 does not know (made with `git worktree add` or another tool) a workspace where it is, nothing moved or checked out, and a deleted one offers **Prune**, which has git forget the repository's deleted worktrees.
- **New branch name.** Under the ticked box. Typed, the worktree is on a new branch of that name, exactly as typed, starting from Branch. Left empty, it works on Branch itself when that is a branch (a remote one, `origin/feature`, gets a local `feature` tracking it), and otherwise (the current branch, a tag, a commit) on a new branch named after Name (`Fix login bug` → `fix-login-bug`, with `workspace.branchPrefix` in front). A name that exists as a branch already is checked out as it is, and one that exists only on `origin` tracks it; the line under the box then says Branch is not used. For a pull request, **Review** opens it in the editor's review view and **Work on it** checks it out in a worktree, on its head branch or on `pr-123` for a fork's, or on the new branch name you typed.
- **Start point.** A new branch starts from exactly what Branch holds, usually your local current branch. A remote branch is fetched first (30 s at most; if that fails the workspace starts from your copy and punk0 says so). New branches do not track their start point, so a push goes to their own name.
- **The line under it** says what Create will do (new branch from where, existing branch, tracking, adopting or opening an existing worktree, the folder) or why it cannot, before anything is made. The Create button reads **Adopt** or **Open** to match.
- **Agent.** The first tab: an installed agent, a shell, or none (the welcome view); not shown for **Open**, which makes nothing. **Language servers in this folder** turns them on for a new or adopted worktree (each costs 300 MB to 1.5 GB).
- **Ctrl+Enter** creates. Agent, the new-worktree box and language servers are remembered per project, in this browser.

Each workspace row shows its editor's state (stopped, starting, running, failed), a badge with the number of running tabs or the most urgent agent status, and, on hover, delete. Under the name, a second line says where it runs and on what: `⌂ main · project folder` for a workspace in the project folder (the folder's branch as it is now, shared by every workspace there), `⑂ branch` for a worktree, with `· adopted` for one made outside punk0 and `· missing` when its folder was deleted. A worktree punk0 does not list yet shows the same way, with `· found`.

### Scratch chats

Scratch is for agent work that has no repository yet: research, one-off scripts, prototypes. Each chat is a coding agent in a terminal tab, in a folder of its own under the state directory or in a folder of yours, so it can write and run files without touching a project.

- **Starting.** **+** in the header, **New chat**, or **+** on a group's row opens the **New chat** flyout just right of the sidebar, pointing at the button you pressed. **Enter** starts the chat; **Esc**, ✕ or a click elsewhere closes it.
  - **Title.** Optional. Left empty, a Claude chat is named after the first prompt you send (not a `/command`, cut to about 60 characters), any other after its folder.
  - **Agent.** Every agent punk0 knows that is on your `PATH`: Claude Code, Codex, OpenCode, Antigravity, Cursor Agent, or a plain shell; the one you used last is picked. Claude Code is the agent punk0 follows closely: its tab shows working / needs you / idle, and it resumes its exact conversation after a restart. The others show no status; after a restart Codex picks up the folder's latest session and OpenCode its latest one, the rest start a new one. The flyout says which applies. A tab opened on an earlier chat from the Agents view resumes exactly that chat in Claude Code, Codex and OpenCode, every time it starts.
  - **Group.** **Ungrouped** or one of your groups; preselected when you start from a group's **+**.
  - **Folder.** Where the agent runs. It starts as a new folder in punk0's scratch folder named after the title (`photo-renamer`), or after the time while there is none (`chat-1021-1432`); such a folder must not exist yet and is deleted with the chat. Type another path (with folder completion) or press **Browse**: click a folder to open it, ← to go back to its parent, **Use this folder** to take the one shown, or **New folder here** to name one that is made when the chat starts. Any path outside the scratch folder is a folder of yours: made if it does not exist, never deleted by punk0, and shown by name next to the chat's title.
- **Pinning.** The pin on a chat's row, or **Pin to the top** in its ⋯ menu, lists it under **Pinned** above everything else, wherever it is filed; pin it again to put it back.
- **Names.** Double-click a chat, or use **Rename…** in its ⋯ menu, to rename it. Renaming changes the label only: the folder keeps its name, because agents find a chat's history by the folder it ran in.
- **Groups.** The new-group button adds a group at the top of the list with its name field ready: **Enter** or a click elsewhere makes it, **Esc** cancels, and a name already used is refused under the field. **New group…** in a chat's ⋯ menu does the same and moves the chat into it; the menu also moves a chat into a group or out of one. Double-click a group's name, or use its pencil, to rename it the same way; ✕ on its row deletes the group and keeps its chats, in no group. Click a group to fold it. Groups show while the list is grouped by Group; making one switches to it.
- **Archiving.** **Archive** in the ⋯ menu stops the chat's tabs and editor and hides it, keeping its folder and conversation. **Show archived** in View options lists archived chats in a folded **Archived** section at the bottom, as a search does; ↺ restores one. The trash button there deletes the chat, and with it the folder punk0 made for it; a folder of yours is kept. The agent's own copy of the conversation (Claude's under `~/.claude`) is kept whatever you do here; delete it in Chats mode.
- **Order.** Pinned chats come first, then the rest grouped and sorted as View options says: by default the groups, then the chats in no group under **Today**, **Previous 7 days** and **Older**, most recently used first.

## Tabs

The tab bar holds the selected workspace's terminals; open files have the panel beside it (see [Files beside the terminals](#files-beside-the-terminals)), or share this bar with **File View** set to `tabs`. **+** offers a new tab for every installed agent not hidden in settings (`agents.hidden`) and a shell, with `agents.default` first, then a file picker that fuzzy-finds a file of the workspace. Clicking a file in the right-hand tree also opens it. File tabs are read-only views; the editor view is for editing. Double-click a terminal tab to rename it. An agent tab with uncommitted files shows their count; click it to see them (see [Changes by chat](#changes-by-chat)). Closing a running terminal asks first unless `terminal.confirmCloseRunning` is off.

With no tab open, as in a new project or worktree workspace, the center shows the welcome view: punk0's name and version, the workspace and its folder, and the same choices as **+**, the default agent first, then a file. Nothing starts until you pick one. With files open but no terminal, the welcome view takes the terminals' side. A scratch chat opens on the tab its agent runs in.

- **Terminals are real.** Each terminal tab is your shell in a PTY, started when you first open the tab. A Claude tab runs `claude` in that shell.
- **Windows.** Terminals run on the Windows pseudo console (Windows 10 1809 or later). With `terminal.shell` empty a tab runs PowerShell 7 (`pwsh`), else Windows PowerShell, else `cmd`; agent tabs start in that shell and leave its prompt when the agent quits. Claude's status needs Claude Code 2.1.139 or later there.
- **They outlive the browser.** Closing or reloading the page leaves every terminal running; reattaching replays its recent output. Everything stops when punk0 stops (Ctrl-C). After a restart, a Claude tab resumes its own chat.
- **Agent status.** Claude tabs report what the agent is doing through Claude Code hooks. A dot on the tab shows **working**, **waiting** (needs you: a permission prompt or a question) or **idle** (done).
- **Terminal settings.** Font, line height, scrollback, cursor and copy-on-select (`terminal.*`) apply to the open terminal as soon as they are saved; `terminal.shell` and `terminal.env` apply to tabs started afterwards.
- **Idle stop.** A terminal that has carried nothing for `terminal.idleStop` minutes (default 30, `0` never) is stopped: a shell only while it sits at its prompt with nothing running in it (Unix only; elsewhere shells are never stopped), and every agent the same way, except a Claude tab that says it is working or waiting for an answer. An agent that reports no status (Codex, OpenCode, Antigravity, Cursor Agent) is judged by its output alone, so one running a long silent command can be stopped. Opening the tab again starts it again and picks its chat up where it was (Antigravity starts a new one).

## Notifications

While the agents page is open, even in a background browser tab, punk0 can tell you when a tab with status starts **waiting** for you or goes from **working** to **idle**: a browser notification (`notifications.desktop`: off, only when the page is not focused, or always), a short tone (`notifications.sound`), and the number of waiting tabs in the page title (`notifications.titleBadge`, on by default). `notifications.onWaiting` and `notifications.onDone` pick which changes count. Clicking a notification focuses the page and selects that tab. Browser notifications need https or localhost; turning them on in settings asks for permission. With no agents page open, nothing notifies.

## Editor view

The `</>` button before ◫ at the right end of the middle column's rightmost tab bar opens the selected workspace's folder in px0's full editor at `/w/<id>/`. It opens in a browser tab named for that folder, so opening it again reuses the tab. The address is stable, so a bookmarked or reloaded editor reopens. The ▦ button in the explorer header goes back to the agents page, in its own window.

Workspaces that share a folder share one editor process. It starts on the first request for the folder and is stopped after `workspace.idleStop` minutes (default 5, `0` never) with nothing open; the next request starts it again.

## Language servers

Language servers are **off** by default for every folder: they cost hundreds of megabytes to gigabytes each (gopls measured at about 260–316 MB per folder, rust-analyzer at 852 MB steady and 1.5 GB peak on a small Anchor project). Every folder runs its own: a project's main checkout and each of its worktrees are separate folders, each with its own editor and servers, while workspaces on the same folder share them. Turning them on is remembered for the folder: when its editor is stopped for being idle (`workspace.idleStop`), the next editor starts with them on again. A server starts when a file of its language is opened, in that folder's editor or in this page's viewer, and the status bar item's dot follows it from starting to running. A server stopped from the menu stays stopped until **Start**, even with its files open.

The language server item in the status bar, at the right corner of the middle column, is the selected folder's, as in the editor's status bar. With a text file in front it is named after the file's language (**Go**, **Markdown**, **Plain Text**); otherwise it reads **LSP**. Next to the name it shows how much memory the language servers of **every** folder use together (`LSP 1.3 GB`), measured from the process table each time the status bar refreshes (every 3 seconds), so it keeps no editor awake. With no file in front the item shows only while this folder's servers are on or any folder's servers hold memory. Its dot is grey while this folder's servers are off (or on but its editor is not running), green while that editor runs with them, and hollow when they are on but no server is installed for the file's language.

Click it for the menu: **Turn on** / **Turn off** saves the choice for the folder and restarts its editor; while on and the editor runs, its servers are listed as in the editor's own LSP menu, the same list. It has one row per language among the folder's files: the server that opening a file of that language starts (the first in punk0's list that is installed and usable, such as **tsc** for TypeScript 7 before typescript-language-server), or, with none installed, the best one punk0 can install here (pyright for Python). Other servers for the same language are left out, except one still running, which stays listed so it can be stopped. A running server shows its memory (the server and every process it started, such as tsserver under typescript-language-server) with **Stop**, an installed one **Start**, and a missing one **Install**, which runs punk0's own install command for it in the background and starts the server once it is in. Where punk0 has no command it may run here (it would need an administrator, or its installer is missing too), or an install failed, the row shows the commands to run by hand with **Copy**, and an **Install guide** link. Below, **All folders** lists every folder whose servers hold memory, biggest first, with the total and a **Stop** for each. Stopped servers stay off until **Start** in that folder's list or until its editor starts again. Starting punk0 with `-no-lsp` keeps them off everywhere.

Settings (category **Language Servers**) cap what they cost across all folders. **Language Servers** (`lsp.enabled`) is the master switch: off stops every running server in every folder within a few seconds, starts none, and hides the status bar item, while each folder's own choice is kept for when it is turned back on; turned back on, the file in front starts its server again. A page left open elsewhere picks up settings saved by another page or by editing settings.json when it is looked at again. **Idle Stop** (`lsp.idleStop`, 15 minutes by default) stops a server nobody has used for that long, even while its folder's editor stays open, so an editor left open in a window does not hold gigabytes forever; the agents page's dot turns off, and the next hover, definition or opened file starts it again. **Disabled Servers** (`lsp.disabled`) lists servers punk0 never starts: the next installed server for that language is used instead, and a running one is stopped. **Memory Warning** (`lsp.memoryWarning`, 2048 MB by default, `0` never) turns the status bar's memory figure and the **All folders** total red once all folders together use more.

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

The bottom bar shows the logo and version, then the help, GitHub and settings buttons on the left. On the right, one button shows the running terminals (`>_`), running agent tabs (robot icon) and the total RAM of punk0 and everything it started. Click it for the resource panel: CPU and RAM for each workspace with a recent-CPU sparkline, then its editor, its language servers (each on its own row, the servers shown while any run) and each terminal (the shell and everything running in it, such as an agent and the tools it starts), then pull request reviews, then punk0 itself. Every punk0 process (punk0 itself, each editor, each review) shows its goroutines, and the head adds them up beside the total CPU and RAM. RAM is each process's proportional share (PSS): memory several processes share, such as the agent program every Claude tab runs, is split between them instead of being counted in full by each, so the rows add up to what punk0 really uses. Hover a RAM figure for its RSS, the number `ps` and `top` show, and how much of it is shared. Click a workspace to fold it; Esc or a click elsewhere closes the panel. The editor's status bar carries the same button and panel. The **?** button opens the agents page's keyboard shortcuts; Esc or a click closes them. The gear, or Ctrl/Cmd+, outside a terminal, opens the settings on this page, the same settings the editor shows, also with no project added. Theme, interface font and terminal settings apply as soon as they are saved.

The middle column starts with the agents across every workspace: **N need you** (agents waiting on a permission prompt) and **N working**, each shown only while there are any. Clicking one opens the next such tab, so repeated clicks walk through all of them. The counts come from the same Claude Code hook reports as the tab dots, so they cost no extra requests. An editor's status bar shows the same two counts for its own workspace's tabs, right after Wrap and Go to, and a click there shows the next such tab in the editor's terminal.

While a file is shown (in its panel, or in front among the tabs), its details sit at the right of the middle column, before the language server item, which then shows the file's language (see [Language servers](#language-servers)): **Ln, Col** (a click in the code places the cursor; tabs count to the next tab stop), the file's line count, its size on disk, **Tab Size** (the `editor.tabSize` setting, which the viewer also renders tabs at), the encoding (**UTF-8**, **UTF-8 with BOM** or **Not UTF-8**) and the line endings (**LF**, **CRLF** or **Mixed**). They are read once, when the file is opened. Click **Ln, Col**, or press Ctrl/Cmd+G outside a terminal, to go to a line: type `120` or `120:8` and press Enter. A file tab switched to its diff keeps these details, without **Ln, Col** and Go to line; the Changes tab, being every changed file, shows none. With no file shown, none of this is. As the column narrows it drops tab size, line count, size and line endings first, then encoding.

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
