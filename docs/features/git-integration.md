# Git Awareness, Diff Viewer & Stage/Commit/Push/Pull

punk0 includes built-in Git awareness, an interactive visual diff viewer, and a sidebar panel for the rest of the everyday git loop. It highlights working-tree modifications across your file tree and editor gutters, lets you toggle between source code and an interactive side-by-side or unified diff against `HEAD` with `Cmd/Ctrl+D`, and stages, commits, pushes, and pulls without leaving the browser.

---

## Overview & Core Purpose

In modern software engineering, coding agents, background formatters, and compilers continuously generate or modify files on disk. Developers spend a significant portion of their time verifying what changed, ensuring unintended edits were not introduced, and auditing modifications prior to staging or committing.

punk0's git status and diffing are read-only and zero-latency by default: querying `git status` asynchronously in the background never stages files, mutates index locks, or slows down viewer startup. With visual badges, ancestor dirty propagation, gutter indicators, and full split/unified diffs, you can review changes with complete confidence without leaving the browser.

The sidebar's git panel is the one part of this feature that *does* write to the repository — but only in direct response to a click: staging, committing, pushing, and pulling all happen exactly when you ask for them, never automatically. Pull is deliberately conservative (fast-forward only) so it can never leave the working tree mid-conflict on its own; when branches have diverged, a rebase or merge happens only if you ask your coding harness to do it.

---

## Key Capabilities

- **Real-Time Live Status Synchronization**: punk0 establishes a lightweight Server-Sent Events (SSE) connection (`/api/stream`, aliased by `/api/git/stream`) to push working-tree status changes directly to the browser. You do not need to refresh the browser or click manual reindex buttons when files change on disk.
- **Sub-Millisecond CLI Change Awareness**: When you execute Git operations in your terminal (`git checkout`, `git reset`, `git add`, `git commit`, `git restore`, `git stash`), punk0 detects the operation in sub-milliseconds by checking metadata timestamps on Git control files (`.git/index`, `.git/HEAD`, `.git/packed-refs`), instantly updating your view without scanning files on disk.
- **File Tree Status Badges**: The file explorer decorates changed files with colored badges indicating their Git working-tree status:
  - `M` (Modified): Working tree file differs from `HEAD`.
  - `A` (Added / Staged): Newly added file staged in the index.
  - `D` (Deleted): File removed from the working tree.
  - `U` (Untracked): New file not yet tracked by Git.
  - `R` (Renamed): File renamed or moved.
- **Dirty Ancestor Folder Propagation**: When a nested file is modified (e.g., `src/core/auth/token.go`), all parent directories in the tree (`auth/`, `core/`, `src/`) display a subtle dirty indicator badge. This allows you to spot modifications even when folder branches are collapsed.
- **File Explorer vs. Git Changes Toggle**: A dedicated segmented toggle in the sidebar header allows you to switch between the full project directory tree and the Git changes view. In Git changes mode, punk0 collapses untouched folders and presents only files with uncommitted additions, modifications, or deletions.
- **Automatic Explorer Fallback**: If all uncommitted changes are discarded or committed while you are in Git changes mode, punk0 automatically switches back to standard file explorer mode so you are never left viewing an empty tree.
- **Auto-Closing Discarded Diff Tabs**: When you discard changes to a file from the terminal (`git checkout -- file` or `git reset`), any tab opened in diff view for that file automatically closes in reverse index order, keeping the active tab index stable and preventing stale diff errors.
- **Visual Gutter Diff Indicators**: The code viewer gutter places colored indicator bars alongside line numbers to mark edits in real time:
  - Green bar for added lines.
  - Blue bar for modified lines.
  - Red triangle or marker for deleted lines.
- **Interactive Diff Viewer (`Cmd/Ctrl+D`)**: Toggle between normal source view and full Git diff with a single keystroke.
- **Side-by-Side & Unified Diff Modes**:
  - **Side-by-Side (Split)**: View original `HEAD` code on the left and active working-tree code on the right with synchronized scrolling.
  - **Unified**: View changes inline with consecutive additions and deletions.
- **Intra-Line Character Diff Highlighting**: Powered by a token-level Myers diff algorithm with sub-character refinement. Exact modified characters and words are highlighted with distinct contrast within changed lines across both split and unified layouts, while insertion carets pinpoint where text was inserted relative to counterpart lines.
- **Whitespace Diff Filtering**: Toggle whitespace trimming to hide trivial indentation and trailing space differences when reviewing significant logic changes.
- **Hand a Diff to Your Agent**: Select any modified or added lines in the diff view and press `Alt+L` to send them, with their working-tree path and line numbers, to the agent in the editor's [Terminal tab](editor-terminal-and-quick-edit.md).
- **Battery and Focus Awareness**: The live stream automatically suspends when the browser tab is hidden (`document.visibilityState === 'hidden'`), conserving CPU cycles and laptop battery. When you switch back to punk0, it instantly reconnects and queries `/api/git/refresh` to catch any changes made while the window was in the background.
- **Per-File Stage Tick**: Every changed row in the file tree carries a small tick button next to its status badge. Clicking it stages or unstages that file (`git add` / `git reset`) without opening a terminal; the tick updates live as the same status stream that drives the badges reconciles it.
- **Git Panel (Source Control)**: A resizable panel at the bottom of the sidebar, laid out like VS Code's Source Control. The head row shows the branch, `↑ahead ↓behind` (or *unpublished* for a branch with no upstream), the upstream it tracks, and a `staged / changed` count that switches the sidebar to the changed files when clicked; a key icon appears there when the remote's forge has no token yet. Below it sit the commit message box (**`Cmd/Ctrl+Enter`** inside it runs the primary action), warnings for staged files that look like secrets (`.env`, keys, credentials) or are over 1 MiB, and one primary button whose label says what comes next: **Commit N staged**, **Push ↑N** (**Publish branch**, **Push to PR ↑N**), **Pull ↓N**, **Sync ↑N ↓M**, **Force-push** after a PR rebase, or a disabled **Up to date**. Its **▾** menu holds Commit & Push, Stage all, Amend last commit, Undo last commit, Pull, and Push. Commit only ever commits what is staged; nothing is staged for you.
- **Safe Repository & Unborn HEAD Support**: Seamlessly inspects repositories operating under safe directory configurations (`safe.directory`) and cleanly handles freshly initialized repositories before their first commit (unborn `HEAD`), without error dialogs during branch detection, unstaging, or diffing.
- **Commit Graph**: The bottom of the git panel is a commit graph, like VS Code's: each commit on a lane drawn beside it (merges and forks curve between lanes, HEAD is a hollow ring), with its branch and tag badges, author and date. **Load more** pages further back; **Open on GitHub** (or your forge) opens the full history. Click a commit to expand it in place: every file it changed, each with an `A`/`M`/`D`/`R` status letter and `+`/`−` line counts. Click a file to read that commit's own diff: the tab is pinned to the commit and marked `@03beaf0`. A strip above the diff shows where the file sits in the commit (`3 / 17`); ‹ › step to the neighbouring file, swapping the tab in place rather than opening one per file. Hover a row and click its short hash to copy the SHA. Merge commits are shown against their first parent. A large commit shows 100 files at a time, with a **Show 100 more** row below.
- **Fast-Forward-Only Pull**: Pull always tries a clean fast-forward onto the remote (or, in a PR review session, the pull request's current head). If history has diverged, punk0 itself never starts a merge. Instead the git panel shows what diverged and offers **Rebase with AI** or **Merge with AI**, which hand the job, conflicts included, to your selected coding harness. Nothing runs unless you click one, and the harness is told never to push.
- **Git Errors Stay in the Git Panel**: Every git failure (stage, commit, push, pull, abort) shows in a card at the top of the git panel, next to the buttons that caused it, never as a global toast. The card stays until you dismiss it or the next git action succeeds.
- **Push to the Right Place**: In a plain workspace, Push pushes the current branch to its configured upstream (offering to set one up on a first push). In a PR review session, Push targets the pull request's actual head branch on its actual repository — a fork included — never wherever the checkout happens to be sitting.
- **Outgoing & Incoming Commits**: Commits on your branch that its upstream doesn't have yet sit under an **Outgoing · N to push** row in the graph, and commits on the upstream you haven't pulled under **Incoming · N**; the graph's header names the upstream (e.g. `origin/master`) so it's clear what they're measured against, and carries **Pull**, **Push** and **Refresh**. Committing no longer hides your work: the Git changes toggle stays available on a clean working tree, precisely because the outgoing commits are then the only thing left to review. With no upstream configured nothing is marked outgoing — punk0 never guesses a branch to measure against.
- **Commit Hover Card**: Hovering a commit in the graph shows its author, the full commit message, and its diffstat (insertions in green, deletions in red), with a **Copy SHA** button and — on a GitHub remote — a link on the author's name.

---

## Developer Workflows & Practical Value

### Continuous Auditing of AI Agent Edits
When an AI coding agent (Claude Code, Codex, Cursor Agent, Antigravity, OpenCode) edits your code in the background:
1. Switch to the **Git Changes** view in the sidebar to isolate touched files.
2. Status badges and gutter markers update in real time as the agent writes to disk.
3. Open any modified file and press **`Cmd/Ctrl+D`** to review side-by-side changes against `HEAD`.
4. If an edit needs refinement, select the relevant lines inside the diff view and press `Alt+L` to send them to the agent in the editor's terminal with a correction, or switch to the source view (`Cmd/Ctrl+D`) and use [Quick Edit](editor-terminal-and-quick-edit.md#quick-edit) (`Cmd/Ctrl+K`).

### Terminal Interaction Without Stale Views
When managing branches or staging files from your terminal:
1. Stage or reset files in the terminal (`git add file.go` or `git checkout -- file.go`).
2. punk0 immediately catches the `.git/index` modification and patches the file tree badges in place without resetting your scroll position or collapsing expanded folders.
3. Tabs displaying diffs for discarded files close automatically, keeping your workspace clean and focused.

### Pre-Commit Visual Review Station
Before committing code from your terminal, open punk0 to perform a visual walk-through of all pending changes. The Git changes view isolates your work, ensuring you do not commit debug logs, temporary comments, or unintended formatting tweaks.

### Stage, Write, and Commit Without Leaving the Browser
Once you've reviewed a change in the diff view, finish the commit right there:
1. Tick the files you want in this commit from the file tree (or **Stage all** from the git panel's ▾ menu).
2. Write a message in the git panel's message box, then press **Commit N staged** (or `Cmd/Ctrl+Enter`).
3. Press **Push ↑N** to send the branch to its remote (**Publish branch** on a branch's first push sets the upstream for you).

### Reviewing Local Commits Before You Push
When you've made several commits locally and want to read them over before sending them up:
1. Look at the **Graph** at the bottom of the git panel. The **Outgoing** group lists every commit your upstream doesn't have yet, newest first.
2. Hover a commit to see who wrote it, its full message, and how much it changed.
3. Click it to expand the files it touched, then click a file to read that commit's diff — just that commit's, unaffected by anything you've edited since.
4. Fix anything that needs fixing, commit again (the graph updates itself), and click **Push** in the graph's header. The Outgoing group empties as the commits land.

### Catching Up With a Moving Remote
When a teammate (or CI) has pushed new commits to the branch you're reviewing:
1. Click **Pull** in the git panel.
2. If your local branch can fast-forward cleanly onto the new commits, punk0 updates it and the diff view refreshes automatically.
3. If your branch has diverged (you have local commits the remote doesn't), the git panel shows a **Can't fast-forward** card with the local and incoming commit counts and a harness picker. Pick one:
   - **Rebase with AI** replays your commits on top of the remote for a straight history.
   - **Merge with AI** joins the two histories with a merge commit.
   - Or dismiss the card and resolve it in a terminal.
4. While the harness runs, the card shows its progress and live output; **Cancel** stops it. Pull and Push are disabled until it finishes.
5. punk0 then checks where the branch actually ended up:
   - **Rebased/Merged**: the branch is up to date, with a **Push** button to publish once you've reviewed the result.
   - **Not finished**: the harness stopped mid-rebase or mid-merge, usually on a conflict. **Finish with AI** asks it to resolve and continue; **Abort** restores the branch exactly as it was.
   - **Didn't complete**: the branch is unchanged (the harness aborted or was cancelled); its output says why.

A rebase or merge left half-done from a terminal is shown the same way when punk0 loads or Pull is clicked.

---

## Keyboard Shortcuts & Controls

| Shortcut / Control | Context | Action |
| :--- | :--- | :--- |
| `Cmd/Ctrl+D` | Editor | Toggle Git Diff View (Split / Unified vs. `HEAD`) |
| Toggle Segment (`Files` / `Changes`) | Sidebar Header | Switch between File Explorer and Changed Files Only |
| Toggle Icon | Diff Header | Switch between Side-by-Side and Unified Diff |
| Space Icon | Diff Header | Toggle Ignore Leading/Trailing Whitespace |
| `Mod+Shift+R` | Global | Force Workspace and Git Status Refresh |
| Stage Tick | File Tree Row | Stage / unstage that file |
| Hover a Commit | Graph | Show author, full message and diffstat, with Copy SHA |
| `staged / changed` count | Git Panel head | Show the changed files in the sidebar |
| Primary button / `Cmd/Ctrl+Enter` | Git Panel / Message Box | The next step: commit the staged files, push, pull, sync, or force-push a rebased PR |
| ▾ **Commit & Push** | Git Panel | Commit the staged files, then push |
| ▾ **Stage all** | Git Panel | Stage every changed file |
| ▾ **Amend last commit** | Git Panel | Fold the staged files into the last (unpushed) commit; the box's text replaces its message, an empty box keeps it |
| ▾ **Undo last commit** | Git Panel | Undo the last (unpushed) commit, keeping its changes staged and its message in the box |
| ▾ **Pull** | Git Panel | Fast-forward onto the remote (or, in PR review, the PR's current head); on divergence, offers Rebase/Merge with AI |
| ▾ **Push** | Git Panel | Push the current branch (or, in PR review, to the PR's head branch) |
| Click a commit | Graph | Expand / collapse its changed files |
| Click a changed file | Graph | Open that file's diff within the commit, in a tab pinned to it |
| **Show 100 more** | Graph | List the next 100 files of a large commit |
| Double-click a commit / `Enter` | Graph | Open the commit's first changed file |
| Hash chip (on hover) | Graph | Copy the full SHA |
| `↑` / `↓`, `→` / `←` | Graph | Move between commits and files; expand / collapse |
| **Load more** | Graph | Show 30 older commits (up to 200) |
| ‹ › | Diff strip | Previous / next changed file, in the file tree's order (in a pinned tab: the commit's files); binary files are passed over; the tooltip shows the file's position |
| `[c` / `]c`, ˄ ˅ | Diff View / diff strip | Jump to previous / next diff hunk |
| `[f` / `]f` | Diff View / Editor | Step to previous / next changed file |
| Expand all / Collapse all | Diff strip | Add up to 15 lines of file context around every hunk, or put them all back |
| **Expand / Collapse** | Diff Hunk Header (`@@ ... @@`) | Add up to 15 lines of surrounding file context to that hunk inline, or put it back (not on a new or deleted file: the hunk is already the whole file) |
| **Stage / Unstage / Discard** | Diff Hunk Header (`@@ ... @@`) | Stage, unstage, or discard a single diff hunk via `git apply --unidiff-zero` |
| **Stage / Unstage / Discard** | New or deleted file's hunk header | Whole-file actions, each shown only when it would change something: Stage (`git add` / the deletion), Unstage (a new file stays on disk, untracked; a deletion stays on disk), Discard (deletes a new file after a confirm, since git has no copy of it; restores a deleted one from `HEAD`) |

---

## Configuration & Preferences

Git behavior can be customized in Settings (`Cmd/Ctrl+,`):

- **Git: Gutter Indicators** (`git.gutterIndicators`): Enable or disable real-time change indicator bars in the editor gutter (defaults to `true`).
- **Diff Editor: Render Side-by-Side** (`diffEditor.renderSideBySide`): Default layout for the diff view (`true` for split, `false` for unified).
- **Diff Editor: Ignore Trim Whitespace** (`diffEditor.ignoreTrimWhitespace`): Ignore leading and trailing whitespace diffs (defaults to `true`).
- **Commit Message Instructions** (`git.commitMessageInstruction`): Free-text instructions appended to the prompt your coding harness gets when it writes a pull request's merge or squash message (e.g. *"Follow Conventional Commits"* or *"Reference the ticket number in the branch name"*). Defaults to empty. This setting renders as a multi-line textarea in the Settings UI.
- **CLI Flag `-no-git`**: Launch punk0 with Git features completely disabled (`punk0 -no-git`) for environments where Git is not installed or when viewing plain directory archives.
- **CLI Flag `-no-agent`**: Disables every coding-harness feature; staging, committing, pushing and pulling remain available since they never invoke a harness.

---

## Technical Architecture Deep Dive

For an explanation of how punk0 executes read-only `git status --porcelain=v2` and `git diff` commands concurrently with directory indexing, how `.git/index` stat cache fast-paths achieve sub-millisecond CLI detection, and how Server-Sent Events stream diffs to the DOM, see [Git Awareness & Diffing Internals](../internals/git-integration.md).
