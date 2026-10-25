# Editor Terminal, Selection Pill & Quick Edit

Each folder's editor has the workspace's agents on its right side, a small pill of actions over any code selection, and Quick Edit, which rewrites the selected lines with your coding harness and shows the result before anything is written.

- **Terminal**: the workspace's shell and agent tabs, the same ones the [Agents Page](workbench.md) shows, inside the editor.
- **Chats**: the agents' earlier chats in this folder, to read and resume.
- **Selection pill**: Quick Edit, Add to Chat and Copy Ref, floating above a code selection.
- **Quick Edit**: one instruction, one harness run on the selected lines, a -/+ preview, then Accept or Reject.

---

## The Right Side

The right side has two modes, picked with the icons at the left of its top row: **Terminal** (`>_`) and **Chats** (the chat bubble), the same switch as the sidebar's and the agents page's. The top row's right end holds **Maximize** (the right side takes the whole editor width; click again, or `Cmd/Ctrl+B`, to restore), ⊞ (the punk0 agents page, only when the editor was opened from punk0) and ◫ (hide). `Cmd/Ctrl+J` hides or shows the right side; `Alt+T` shows the Terminal mode and focuses it. `Esc` does not close it. While the side is hidden, ⊞ and ◫ sit at the end of the editor tab bar.

## The Terminal

The Terminal mode's own row holds the tab chips and **+**.

The pane is an xterm.js terminal connected to the orchestrator's PTY for the editor's workspace folder. The tabs are the workspace's own terminal tabs, so a tab opened here also appears on the agents page and the other way round.

- **Chips**: one per tab, with a status dot that lights while an agent is working or waiting. Click a chip to show that tab. A stopped tab starts when you click it.
- **`+`**: opens a menu of agents (Claude Code, Codex, OpenCode, Antigravity, Cursor Agent), then Shell, and starts a tab running the one you pick.
- **`×`** on a chip closes that tab.

Shells and agents run in the orchestrator, not the page: closing the browser or the editor leaves them running, and reconnecting replays their scrollback. When the connection ends, the terminal says so; press any key to reconnect.

While focus is in the terminal, every key goes to the shell or agent. Editor shortcuts such as `Esc`, `Ctrl+W`, `Ctrl+K`, `Ctrl+P`, `Ctrl+D`, `Ctrl+E`, `Ctrl+F` and `Ctrl+B` do not fire there. Click the code to give the keyboard back to the editor. `Shift+Enter` sends a newline to the agent's prompt rather than submitting it.

The agents page and the editor can show the same tab at once. A PTY has one size, so the agent draws at the width of whichever view you last typed into or clicked.

## Chats

The Chats mode lists the chats Claude Code, Codex and OpenCode kept for this folder; the box above the list searches their titles. A filled dot and **· open** mark a chat a tab has open. Clicking a row expands it: how many messages, the last exchange, how the chat started, and two buttons. **Resume** (**Show** for an open chat) opens the chat in a terminal tab, on that exact session, and switches to the Terminal mode; **Copy id** copies the session id.

**Workspaces.** Chats are grouped by the workspaces of this folder (for a project with two local workspaces, say **main** and **hmm**), each workspace showing even before it has a chat; workspaces elsewhere that a chat here belongs to come next, and chats in no workspace follow under **Today**, **Yesterday** and **Older**. A chat belongs to the workspace it was started or resumed in. Click a workspace's header to fold it. Its **+** starts a new chat in that workspace (pick the agent or Shell), and under it a link row for each worktree made by an agent in that workspace opens that worktree's editor. Right-click a chat (or press the context-menu key or `Shift+F10` on it) for **Resume**, then **Move to workspace**: every workspace of this folder (the chat's own ticked; picking another moves it there) and **New workspace…**, whose name box opens right under the chat and which makes a local workspace and moves the chat into it. Then **Copy id**. Resuming a chat that belongs to a workspace in another folder opens that folder's editor. punk0 keeps which workspace each chat belongs to, so it survives closing the tab and a restart.

**Group and filter.** The funnel beside the search sets how the list is grouped — **Workspace** (the default), **Date**, **Agent** (each agent's chats under a header that folds) or **None** (one list, newest first) — and what it shows: one agent, only the chats open in a tab, or no untitled chats (ones no prompt was ever sent in). The funnel shows how many filters are on. Both choices are remembered in this browser.

The Terminal mode shows the tabs of every workspace in this folder; when they come from more than one, each tab's name is prefixed with its workspace's.

Removing or deleting chats, and the chats of other folders, are on the agents page's Chats mode.

---

## The Selection Pill

Selecting code in the source view shows a pill just above the selection:

| Button | Shortcut | Action |
| :--- | :--- | :--- |
| **✦ Quick Edit** | `Mod+K` | Opens the Quick Edit box on the selected lines. |
| **Add to Chat** | `Alt+L` | Sends the selection to the agent in the active terminal tab. |
| **Copy Ref** | `Alt+C` | Copies `path:line` or `path:start-end`. |

The pill is not shown in the diff view or the Markdown preview, and it hides when the view scrolls. The same actions, plus the copy actions and Find Usages, are in the right-click menu; see [Selection Toolbar & Context Actions](selection-actions.md).

### Add to Chat

Add to Chat sends the selection to the agent in the active terminal tab as `@path lines a-b` followed by the code in a fenced block, the same text **Copy with Context** (`Alt+A`) copies. It works in the source view and the diff view. With no terminal tab running, or while the tab's agent is working or waiting on a question, it is refused with a notice.

---

## Quick Edit

Quick Edit runs your coding harness once on the selected lines and shows its replacement as a diff. Nothing is written until you accept it.

### How to use it

1. Select the lines in the source view.
2. Press `Mod+K`, click **✦ Quick Edit** in the pill, or choose **Quick Edit** from the right-click menu. A box opens below the selected lines, or above them when there is no room below.
3. Type an instruction and press `Enter`. The button reads **Editing…** while the harness runs.
4. Review the preview: the selected lines as they are on disk (`-`) and the replacement (`+`).
5. **Accept** (`Mod+Enter`) writes the replacement; **Reject** (`Esc`) closes the box and writes nothing.

`Esc` also cancels a harness that is still running.

### What runs

- The harness and model are the ones selected for px0's other AI features (the git panel's commit message and plan, AI rebase/merge, PR Batch Apply). See [Editing with Coding Agents](agent-editing.md).
- The harness runs in an empty temporary folder with stdin closed, and is stopped after 30 seconds. It never runs in, or writes to, your workspace.
- The prompt carries the path, line range, selected lines and your instruction, and asks for the replacement inside `<replacement>` tags. px0 takes the last `<replacement>` block in the reply, or the last fenced code block when there is none. The lines' indentation is kept.
- A harness error, a timeout, or a reply with no replacement block is shown in the box. Your instruction stays, so you can change it and press `Enter` again.

### What Accept writes

- Accept writes only if the selected lines still read exactly as they did in the preview. If an agent, another tab or you changed them meanwhile, the box shows "File was modified on disk" and nothing is written.
- The file keeps its line endings: a CRLF file stays CRLF.
- An empty replacement deletes the selected lines.
- After a write, open tabs reload in place.

---

## Shortcuts

| Shortcut | Context | Action |
| :--- | :--- | :--- |
| `Alt+T` | Editor | Focus the Terminal tab |
| `Mod+K` | Code selected in the source view | Quick Edit |
| `Mod+K` | No selection | Quick Open |
| `Mod+Shift+K` | Anywhere | Command palette |
| `Alt+L` | Code selected | Add to Chat |
| `Alt+C` | Code selected | Copy Ref |
| `Enter` | Quick Edit box | Run the instruction |
| `Shift+Enter` | Quick Edit box | New line in the instruction |
| `Mod+Enter` | Quick Edit preview | Accept |
| `Esc` | Quick Edit box | Reject, or cancel a running harness |
| `Shift+Enter` | Terminal | Newline in the agent's prompt |

In [Vim mode](vim-mode.md), `e` or `c` on a Visual mode selection opens Quick Edit.

---

## Limits

- Quick Edit works in the source view only: not in the diff view, the Markdown preview, or while the file is open for manual editing (`Mod+E`). Save or leave edit mode first.
- One run per box, limited to 30 seconds. The harness starts in an empty folder, not your workspace, so it sees only what the prompt carries: put what it needs in the selection or the instruction.
- Quick Edit needs a coding harness to be selected. Without one, the box shows the error.
- The Terminal tab needs the orchestrator: it connects to the parent `punk0` process, which owns the PTYs, and terminals are served only while `punk0` listens on this machine (loopback).
- While the terminal has focus, editor shortcuts do not fire.

For the endpoints behind these features, see [Harness Editing & Agent Dispatch](../internals/agent-editing.md).
