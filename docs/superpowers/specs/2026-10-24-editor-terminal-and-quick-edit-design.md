# Specification: Editor Terminal Integration, Floating Selection Pill & Quick Edit

- **Date**: 2026-10-24
- **Status**: Approved (2026-10-24)
- **Target Components**: `web/index.html`, `web/src/selbar.js`, `web/src/inspector.js`, `web/src/term.js`, `server.go`, `agent.go`

---

## 1. Problem Statement & Motivation

In the current editor mode (`web/index.html`):
1. **Threads Sidebar Flaws**: The legacy Threads system (`#pane-right-threads`, `web/src/thread.js`) spawns headless background processes (`claude -p` / `codex exec`) with closed `stdin`. When coding agents request interactive permissions or prompt for confirmation (e.g. `[y/N]`), the headless process hangs or crashes on EOF.
2. **Awkward Selection Gutter Menu**: Highlighting code renders an intrusive line-gutter "Edit" button (`.agent-btn`) with a drop-down menu (`Start Agent Thread`, `Edit Inline`, etc.) that couples directly into the Threads drawer.
3. **Missing In-Place Quick Edit**: Developers lack a fast, localized inline transformation mechanism (like Cursor's `Cmd+K` or VS Code inline chat) that previews diffs directly inside the code before committing to disk.

punk0 already possesses a robust, interactive PTY terminal subsystem (`term.go`, `creack/pty`, `xterm.js`, `GET /api/tabs/{id}/term`) in the orchestrator workbench. This specification defines bringing that interactive terminal into Editor Mode, retiring Threads, modernizing code selection with a floating action pill (`Cmd+K`, `Cmd+L`, `Alt+C`), and implementing an in-place Quick Edit engine with virtual inline diffs.

---

## 2. High-Level Architecture

```
+------------------------------------+---------------------------------------+
|  Editor Viewport                   |  Right Inspector                      |
|                                    |  [ Terminal ] References Symbols Calls|
|  10 const handleIdle = () => {     +---------------------------------------+
|  11     idleTime += 1;             |  [ ● Claude Code ] [ + Shell ]        |
|                                    |  -----------------------------------  |
|  [✦ Quick Edit ⌘K] [💬 Chat ⌘L]   |  ❯ @src/player.ts:12-14               |
|  12     if (idleTime > 10) {       |  Refactor idle timeout handler        |
|  13         resetIdleAnimation();  |                                       |
|  14     }                          |  ● Checking callers in useTimeline... |
|                                    |  ● Modified src/player.ts             |
|                                    |  ❯ _                                  |
+------------------------------------+---------------------------------------+
```

---

## 3. Detailed Component Specifications

### 3.1 Right-Inspector Terminal (`#pane-right-term`)

1. **Tab Layout**:
   * Replace `<button data-itab="threads">` with `<button class="inspector-tab active" data-itab="term">Terminal</button>`.
   * Replace `<section id="pane-right-threads">` with `<section class="inspector-pane active" id="pane-right-term">`.
   * Tab sequence: `Terminal` (default active), `References`, `Symbols`, `Calls`, `Search`, `Tips`.
2. **Sub-Bar Header, Tab Ownership & State Refresh**:
   * The editor view is mounted at `/w/<workspace-id>/`. The parent orchestrator owns `/api/tabs/{id}/term`, `/api/tabs/{id}/send`, and `/api/workspaces/{id}/tabs`.
   * **Workspace Metadata Contract**: The parent passes `-workspace-id <id>` when spawning the child editor process (`workbench.go`). The child's `/api/meta` returns `workspaceId`.
   * **Tab Query & State Refresh**: The editor frontend queries the parent's `GET /api/workspaces/{workspace-id}/tabs` to discover active terminal tabs:
     ```json
     {
       "tabs": [
         {
           "id": "tab-1234",
           "kind": "claude",
           "name": "Claude Code",
           "running": true,
           "status": "idle"
         }
       ]
     }
     ```
     * Polling rule: Polled every 3 seconds while the Terminal inspector pane is visible, and immediately refreshed after tab creation (`POST /api/workspaces/{id}/tabs`) or tab termination.
   * Sub-bar displays tab chips: `[ ● Claude Code ]` (or detected agent) and `[ + Shell ]`.
   * If the workspace currently has zero terminal tabs, the editor sends `POST /api/workspaces/{workspace-id}/tabs {"kind": "claude"}` (or `"shell"` if no agent CLI is detected) upon first opening the Terminal pane.
   * Clicking `[+ Shell]` calls `POST /api/workspaces/{workspace-id}/tabs {"kind": "shell"}` and switches active terminal focus to the newly spawned tab.
   * Status indicators display live agent state (green dot = `idle`, yellow pulsing dot = `working` or `waiting`).
3. **PTY Connection**:
   * Dynamically loads `xterm.js` via `static/vendor/xterm.mjs`.
   * Establishes WebSocket connection to `/api/tabs/{id}/term`.
   * Handles terminal resizing via JSON text frames `{"cols": N, "rows": N}` and binary PTY streaming.
   * Maintains persistent session in Go backend: switching away to `Symbols` or hiding the inspector detaches the WebSocket; reopening reattaches and replays recent scrollback from the ring buffer.

### 3.2 Floating Selection Pill

1. **Trigger & Selection Context Gating**:
   * Listens to editor selection change events (`updateSelectionBar` in `selbar.js`).
   * **Context Restriction**: Quick Edit (`Cmd/Ctrl+K`) and Add to Chat (`Alt+L`) are strictly restricted to standard Source Code Editor views (where line numbers $l1 \dots l2$ map directly to raw source file lines on disk).
   * In Markdown preview (`mdview`), CSV table view, and read-only Diff views, Quick Edit and Add to Chat are disabled and hidden from the pill. In those views, only `Copy Ref` and `Copy Formatted Text` appear.
   * When a valid source range is highlighted ($l1 \le l2$), renders a floating pill anchored directly above the start line of the selection (flipping below if near the top window boundary).
   * Dismissed immediately upon selection collapse, clicking away, or pressing `Esc`.
2. **Buttons & Actions**:
   * **`✦ Quick Edit` (`Cmd/Ctrl+K`)**: Opens the inline floating prompt box directly over the selected lines.
   * **`💬 Add to Chat` (`Alt+L` / `Cmd+Shift+L`)**:
     * Note on shortcut: Standard browser address-bar reservation blocks `Cmd/Ctrl+L`. Therefore, **`Alt+L`** (with `Cmd/Ctrl+Shift+L` as alias) is assigned for reliable browser event capture.
     * Activates and focuses the `Terminal` pane in the right inspector.
     * If the active tab is currently busy (`working` or `waiting`), `POST /api/tabs/{id}/send` returns `409 Conflict`. In this case, the editor shows a toast: *"Agent is busy with an in-flight command"*, keeps the code selection preserved, and allows the user to wait or switch to an idle tab.
     * If the tab is idle, dispatches `POST /api/tabs/{id}/send` with formatted payload:
       ```text
       @<path>:<l1>-<l2>
       ```
       followed by a newline, the verbatim selected code enclosed in markdown fences, and a trailing space ready for user typing.
   * **`📋 Copy Ref` (`Alt+C`)**: Copies `<path>:<l1>-<l2>` to the clipboard with visual checkmark feedback.
3. **`Cmd/Ctrl+K` Precedence & Keybinding Resolution**:
   * Currently, `Cmd/Ctrl+K` is bound globally to Quick Open palette alongside `Cmd/Ctrl+P`.
   * **Precedence rule**:
     * If an active non-collapsed code selection exists in a source editor ($l1 \le l2$ with selected characters), `Cmd/Ctrl+K` triggers **Quick Edit**.
     * If there is no active text selection, `Cmd/Ctrl+K` falls through to open the Quick Open file palette (`openPalette()`), preserving existing muscle memory.
     * Keyboard tips, palette hints, and help dialogs are updated to reflect this dual-binding rule.
4. **Legacy Threads & Agentbox Deprecation Cutover**:
   * Remove gutter `.agent-btn` ("Edit" badge) and the old popup context menu.
   * Remove `<div id="agentbox">` and `<section id="pane-right-threads">` from `web/index.html`.
   * Remove `initThreads()` from `web/src/main.js` and delete `web/src/thread.js`.
   * Remove `/api/threads/*`, `/api/agent/edit`, and `/api/agent/batch` route registrations from `server.go` and `agent.go`.
   * In `newAgentManager()`, remove `threadManager` instantiation and remove thread ID coupling from job records.
   * Update `selbar.js`, `tips-util.js`, and `help.js` to remove deprecated thread and inline-comment actions.
   * Existing thread JSON files in `~/.px0/threads/` are retained passively on disk, but no new thread files are written.

### 3.3 Quick Edit (`Cmd/Ctrl+K`) Engine & In-Place Diff

1. **Line-Based Selection Expansion**:
   * Quick Edit operates strictly on full-line boundaries. If the user's cursor selection starts or ends mid-line, Quick Edit automatically expands the target range to complete lines ($l1$ column 1 through $l2$ line end).
   * This guarantees that the `original` comparison block always matches complete lines on disk without character-offset ambiguities.
2. **Floating Prompt Bar**:
   * Positioned immediately above the selected lines with autofocus on the input textarea.
   * Submits on `Enter` (or `Cmd+Enter`), dismisses on `Esc`.
3. **Stateless Headless Execution (`POST /api/quick-edit`)**:
   * Request payload:
     ```json
     {
       "path": "src/player.ts",
       "l1": 12,
       "l2": 14,
       "selection": "    if (idleTime > 10) {\n        resetIdleAnimation();\n    }",
       "instruction": "add timeout parameter with 5s fallback"
     }
     ```
   * **Sandbox & Zero Disk Modification Guarantee**:
     * Spawns harness CLI in pipe mode with a system prompt instructing the model:
       `"Given the code snippet below and instruction, output ONLY the replacement code enclosed in <replacement>...</replacement> tags. Do NOT modify any files on disk. Do NOT provide explanation or conversation."`
     * `cmd.Stdin` is set to `nil` (`/dev/null`) so the process cannot block on interactive input prompts.
     * Execution is bounded by a strict 30-second context deadline (`context.WithTimeout(ctx, 30*time.Second)`).
     * The backend performs a git/file check to confirm zero disk modifications occurred during execution.
   * **Parsing Protocol**:
     * The backend parses the stdout looking for `<replacement>(.*?)</replacement>`.
     * Fallback parser: If tags are absent, matches the first markdown fenced code block (```...```).
     * If neither is present, returns HTTP `422 Unprocessable Entity` with `{"ok": false, "error": "Model response could not be parsed into replacement code"}`.
   * Success Response:
     ```json
     {
       "ok": true,
       "replacement": "    const timeout = 5000;\n    if (idleTime > 10) {\n        resetIdleAnimation(timeout);\n    }"
     }
     ```
4. **In-Editor Inline Diff Rendering & Atomic Accept Contract**:
   * Editor renders a temporary overlay spanning $l1 \dots l2$:
     * Red struck-through background for removed lines (`-`).
     * Green highlighted background for added lines (`+`).
   * Renders floating review strip below the diff:
     * **`✓ Accept` (`Cmd+Enter` / `Enter`)**: Sends atomic replacement request:
       ```http
       POST /api/file/replace
       Content-Type: application/json

       {
         "path": "src/player.ts",
         "l1": 12,
         "l2": 14,
         "original": "    if (idleTime > 10) {\n        resetIdleAnimation();\n    }",
         "replacement": "    const timeout = 5000;\n    if (idleTime > 10) {\n        resetIdleAnimation(timeout);\n    }"
       }
       ```
       * **Stale-Source Precondition**: If the file on disk was modified by an external process or terminal agent while Quick Edit was running and lines $l1 \dots l2$ no longer match `original` verbatim, the backend returns `409 Conflict` with `{"ok": false, "error": "File was modified on disk; please review current lines"}`.
       * On `409 Conflict`, the editor displays an inline conflict warning on the review strip, preserves the replacement text, and prevents silent source overwrites.
       * On `200 OK`, updates file on disk, triggers live editor reload, and dismisses the overlay.
     * **`✕ Reject` (`Esc`)**: Reverts view to original lines with zero disk modification.

---

## 4. Backend Endpoints & API Contract

| Endpoint | Method | Payload | Description |
| :--- | :--- | :--- | :--- |
| `/api/tabs/{id}/term` | `GET (WS)` | Binary PTY frames, JSON resize | Reused from orchestrator for terminal sessions. |
| `/api/tabs/{id}/send` | `POST` | `{"text": "..."}` | Bracketed paste injection into active terminal tab. Returns 409 if tab is busy. |
| `/api/workspaces/{id}/tabs` | `GET` | N/A | Lists tabs for workspace `{id}` with id, kind, name, running, and status. |
| `/api/workspaces/{id}/tabs` | `POST` | `{"kind": "claude"\|"shell"}` | Spawns a new terminal tab in the workspace. |
| `/api/quick-edit` | `POST` | `{"path", "l1", "l2", "selection", "instruction"}` | Headless one-shot agent execution for inline diffs (30s timeout, sandboxed). |
| `/api/file/replace` | `POST` | `{"path", "l1", "l2", "original", "replacement"}` | Atomic line replacement with optimistic conflict detection. |
| `/api/threads/*` | `*` | N/A | **Deprecated**: Removed from active routing. |
| `/api/agent/edit`, `/batch` | `*` | N/A | **Deprecated**: Replaced by terminal agent and quick edit. |
---

## 5. Error Handling & Edge Cases

1. **Interactive Prompt in Terminal**:
   * When an agent CLI requires user approval (`[y/N]`), the user types directly into the right-sidebar `xterm.js` terminal. No EOF deadlocks occur.
2. **Add to Chat on Busy Tab**:
   * If the user triggers `Alt+L` while the terminal agent is in the middle of executing a previous command (`working` or `waiting`), the request is rejected with `409 Conflict`. A toast notification informs the user, and the selected code remains highlighted without data loss.
3. **Quick Edit Conflict / External File Change**:
   * If lines on disk change between the prompt submission and clicking Accept, `/api/file/replace` rejects the write with `409 Conflict`. The UI highlights the conflict without clobbering disk content.
4. **Editor Scroll & Virtualized Line Rendering**:
   * The floating selection pill and Quick Edit widgets anchor to line pixel coordinates calculated via the editor's virtualization layout, automatically repositioning on scroll.
5. **Security & Path Confinement**:
   * All mutating endpoints (`/api/quick-edit`, `/api/file/replace`, `/api/tabs/{id}/send`) require loopback callers (`localPost`).
   * All path parameters are validated via `filepath.Clean` and confined strictly within the project working tree; paths resolving outside the workspace root return `400 Bad Request`.

---

## 6. Verification & Testing Plan


1. **Go Backend Tests**:
   * `TestQuickEditStatelessExecution`: Verify `/api/quick-edit` processes request, parses replacement snippet, and does not alter thread storage.
   * `TestTabSendSafety`: Verify `/api/tabs/{id}/send` applies bracketed paste without unescaped control characters.
2. **Frontend Tests (Bun)**:
   * Test selection pill display, positioning, and shortcut triggers (`Cmd+K`, `Cmd+L`, `Alt+C`).
   * Test inline diff rendering, Accept (`Cmd+Enter`), and Reject (`Esc`).
3. **End-to-End Smoke Verification**:
   * Launch `punk0 <dir>` $\to$ verify right sidebar default tab is `Terminal`.
   * Verify terminal prompt is interactive and connected.
   * Select lines $\to$ verify floating selection pill appears.
   * Click **Add to Chat (`Alt+L`)** $\to$ verify `@path:line` is pasted into terminal prompt.
   * Highlight lines $\to$ press **`Cmd+K`** $\to$ enter prompt $\to$ verify inline green/red diff $\to$ verify Accept applies to file on disk and Reject leaves file intact.
