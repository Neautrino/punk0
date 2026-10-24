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
2. **Sub-Bar Header**:
   * Displays tab chips: `[ ● Claude Code ]` (or detected agent) and `[ + Shell ]`.
   * Shows active agent status dot (green = idle/ready, yellow = working/waiting).
3. **PTY Connection**:
   * Dynamically loads `xterm.js` via `static/vendor/xterm.mjs`.
   * Establishes WebSocket to `/api/tabs/{id}/term`.
   * Handles terminal resizing via JSON text frames `{"cols": N, "rows": N}` and binary PTY streaming.
   * Maintains persistent session in Go backend: switching away to `Symbols` or hiding the inspector detaches the WebSocket; reopening reattaches and replays recent scrollback from the ring buffer.

### 3.2 Floating Selection Pill

1. **Trigger & Positioning**:
   * Listens to editor selection change events (`updateSelectionBar` in `selbar.js`).
   * When a range is highlighted ($l1 \le l2$), renders a floating pill anchored directly above the start line of the selection (flipping below if near the top window boundary).
   * Dismissed immediately upon selection collapse, clicking away, or pressing `Esc`.
2. **Buttons & Actions**:
   * **`✦ Quick Edit` (`Cmd/Ctrl+K`)**: Opens the inline floating prompt box directly over the selected lines.
   * **`💬 Add to Chat` (`Cmd/Ctrl+L`)**:
     * Activates and focuses the `Terminal` pane in the right inspector.
     * Dispatches `POST /api/tabs/{id}/send` with payload:
       ```text
       @<path>:<l1>-<l2>
       ```
       followed by bracketed paste of the code snippet.
   * **`📋 Copy Ref` (`Alt+C`)**: Copies `<path>:<l1>-<l2>` to the clipboard with visual checkmark feedback.
3. **Legacy Clean-up**:
   * Remove gutter `.agent-btn` ("Edit" badge) and the old popup context menu.

### 3.3 Quick Edit (`Cmd/Ctrl+K`) Engine & In-Place Diff

1. **Floating Prompt Bar**:
   * Positioned immediately above the selected lines with autofocus on the input textarea.
   * Submits on `Enter` (or `Cmd+Enter`), dismisses on `Esc`.
2. **Stateless Headless Execution (`POST /api/quick-edit`)**:
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
   * Backend executes one-shot headless CLI command using the detected agent harness (e.g. `claude -p` or `codex exec`), passing prompt with instructions to return only replacement code.
   * Response:
     ```json
     {
       "ok": true,
       "replacement": "    const timeout = 5000;\n    if (idleTime > 10) {\n        resetIdleAnimation(timeout);\n    }"
     }
     ```
3. **In-Editor Inline Diff Rendering**:
   * Editor renders a temporary overlay spanning $l1 \dots l2$:
     * Red struck-through background for removed lines (`-`).
     * Green highlighted background for added lines (`+`).
   * Renders floating review strip below the diff:
     * **`✓ Accept` (`Cmd+Enter` / `Enter`)**: Writes changes to disk via `/api/file/edit`, re-indexes, refreshes editor lines, and dismisses overlay.
     * **`✕ Reject` (`Esc`)**: Reverts view to original lines with zero disk modification.

---

## 4. Backend Endpoints & API Contract

| Endpoint | Method | Payload | Description |
| :--- | :--- | :--- | :--- |
| `/api/tabs/{id}/term` | `GET (WS)` | Binary PTY frames, JSON resize | Reused from orchestrator for terminal sessions. |
| `/api/tabs/{id}/send` | `POST` | `{"text": "..."}` | Bracketed paste injection into active terminal tab. |
| `/api/quick-edit` | `POST` | `{"path", "l1", "l2", "selection", "instruction"}` | Headless one-shot agent execution for inline diffs. |
| `/api/threads/*` | `*` | N/A | **Deprecated**: Removed from active routing. |

---

## 5. Error Handling & Edge Cases

1. **Interactive Prompt in Terminal**:
   * When an agent CLI requires user approval (`[y/N]`), the user types directly into the right-sidebar `xterm.js` terminal. No EOF deadlocks occur.
2. **Quick Edit Failure / Invalid Output**:
   * If the one-shot agent fails (timeout, bad prompt), the prompt bar displays an inline error message with a **Retry** button; the editor code remains unmodified.
3. **Editor Scroll & Resize**:
   * The floating selection pill and Quick Edit widgets recalculate their relative top/left pixel positions on editor scroll and window resize.
4. **Non-Loopback Security Guard**:
   * Like all punk0 terminal endpoints, `/api/tabs/{id}/term` and `/api/tabs/{id}/send` reject non-loopback requests with `403 Forbidden` unless authorized.

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
   * Click **Add to Chat (`Cmd+L`)** $\to$ verify `@path:line` is pasted into terminal prompt.
   * Highlight lines $\to$ press **`Cmd+K`** $\to$ enter prompt $\to$ verify inline green/red diff $\to$ verify Accept applies to file on disk and Reject leaves file intact.
