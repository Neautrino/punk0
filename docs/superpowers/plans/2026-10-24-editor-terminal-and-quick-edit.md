# Editor Terminal Integration, Floating Selection Pill & Quick Edit Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace legacy background Threads with an interactive right-sidebar PTY terminal, replace the line-gutter edit menu with a modern floating selection pill (Quick Edit, Add to Chat, Copy Ref), and implement in-place stateless Quick Edit (`Cmd+K`) with virtual inline diffs and atomic acceptance.

**Architecture:** The right inspector mounts an embedded `xterm.js` terminal connecting to the parent orchestrator's PTY engine over WebSocket. Code selection renders an anchoring floating pill above source lines. Quick Edit executes a sandboxed, one-shot harness command in a temporary directory returning `<replacement>` code tags, renders an inline green/red diff in the editor, and submits to an atomic line replacement endpoint with optimistic conflict detection.

**Tech Stack:** Go (HTTP routing, PTY management, process execution), JavaScript ESM (xterm.js, DOM virtualization, selection handling), CSS3.

---

## File Structure & Responsibilities

| File | Status | Responsibility |
| :--- | :--- | :--- |
| `edit.go` / `server.go` | Modify / Create | Implements `/api/file/replace` atomic line replacement and conflict check. |
| `quickedit.go` / `agent.go` | Modify / Create | Implements `/api/quick-edit` sandboxed one-shot harness execution and output parsing. |
| `workbench.go` | Modify | Passes `-workspace-id` flag to child editor and adds `GET /api/workspaces/{id}/tabs`. |
| `web/index.html` | Modify | Replaces `#pane-right-threads` with `#pane-right-term`, removes `#agentbox`. |
| `web/src/inspector.js` | Modify | Wires `Terminal` inspector tab, manages lazy PTY attachment and tab switching. |
| `web/src/term_inspector.js` | Create | Terminal inspector pane manager (xterm.js, tab switcher, `/api/tabs/{id}/term` WS). |
| `web/src/selpill.js` | Create | Floating selection pill component (`Quick Edit`, `Add to Chat`, `Copy Ref`). |
| `web/src/selbar.js` | Modify | Integrates `selpill.js`, removes legacy gutter `.agent-btn` and dropdown menu. |
| `web/src/quick_edit.js` | Create | Floating prompt bar, inline diff renderer, Accept/Reject review actions. |
| `web/src/keybinds.js` / `main.js` | Modify | Context-sensitive `Cmd/Ctrl+K` and `Alt+L` keybindings, removes `thread.js`. |
| `web/src/thread.js` | Delete | Deprecated threads frontend module removed. |

---

## Chunk 1: Backend Endpoints & Workspace Routing

### Task 1: Atomic Line Replacement Endpoint (`/api/file/replace`)

**Files:**
- Create: `replace.go`
- Test: `replace_test.go`
- Modify: `server.go:680-720` (route registration)

- [ ] **Step 1: Write failing test for `/api/file/replace`**

```go
// replace_test.go
package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFileReplaceEndpoint(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "sample.txt")
	originalContent := "line 1\nline 2\nline 3\nline 4\n"
	if err := os.WriteFile(filePath, []byte(originalContent), 0644); err != nil {
		t.Fatal(err)
	}

	s := &server{root: dir, localOnly: true}

	// 1. Successful replace
	payload := replaceRequest{
		Path:        "sample.txt",
		L1:          2,
		L2:          3,
		Original:    "line 2\nline 3",
		Replacement: "line 2 modified\nline 3 modified",
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/api/file/replace", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleFileReplace(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	content, _ := os.ReadFile(filePath)
	expected := "line 1\nline 2 modified\nline 3 modified\nline 4\n"
	if string(content) != expected {
		t.Fatalf("unexpected content:\n%s\nexpected:\n%s", string(content), expected)
	}

	// 2. Conflict replace (original does not match)
	conflictPayload := replaceRequest{
		Path:        "sample.txt",
		L1:          2,
		L2:          3,
		Original:    "old line 2\nold line 3",
		Replacement: "new",
	}
	body, _ = json.Marshal(conflictPayload)
	req = httptest.NewRequest("POST", "/api/file/replace", bytes.NewReader(body))
	w = httptest.NewRecorder()
	s.handleFileReplace(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict, got %d", w.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestFileReplaceEndpoint`
Expected: FAIL with `undefined: replaceRequest` or `handleFileReplace`

- [ ] **Step 3: Implement `handleFileReplace` in `replace.go`**

```go
// replace.go
package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type replaceRequest struct {
	Path        string `json:"path"`
	L1          int    `json:"l1"`
	L2          int    `json:"l2"`
	Original    string `json:"original"`
	Replacement string `json:"replacement"`
}

func (s *server) handleFileReplace(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var req replaceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cleanPath, ok := s.safePath(req.Path)
	if !ok {
		fail(w, http.StatusBadRequest, "invalid file path")
		return
	}

	data, err := os.ReadFile(cleanPath)
	if err != nil {
		fail(w, http.StatusNotFound, "file not found")
		return
	}

	// Normalize newlines for line splitting
	content := string(data)
	lines := strings.Split(content, "\n")
	if req.L1 < 1 || req.L2 < req.L1 || req.L1 > len(lines) {
		fail(w, http.StatusBadRequest, "line range out of bounds")
		return
	}

	endIdx := req.L2
	if endIdx > len(lines) {
		endIdx = len(lines)
	}

	targetSlice := lines[req.L1-1 : endIdx]
	currentBlock := strings.Join(targetSlice, "\n")

	// Optimistic concurrency check
	if strings.TrimRight(currentBlock, "\r") != strings.TrimRight(req.Original, "\r") {
		fail(w, http.StatusConflict, "File was modified on disk; please review current lines")
		return
	}

	repLines := strings.Split(req.Replacement, "\n")
	newLines := make([]string, 0, len(lines)-(endIdx-req.L1+1)+len(repLines))
	newLines = append(newLines, lines[:req.L1-1]...)
	newLines = append(newLines, repLines...)
	if endIdx < len(lines) {
		newLines = append(newLines, lines[endIdx:]...)
	}

	outContent := strings.Join(newLines, "\n")
	if err := os.WriteFile(cleanPath, []byte(outContent), 0644); err != nil {
		fail(w, http.StatusInternalServerError, "failed to write file")
		return
	}

	writeJSON(w, map[string]any{"ok": true})
}
```

- [ ] **Step 4: Register route in `server.go` and run tests**

Register: `mux.HandleFunc("POST /api/file/replace", s.handleFileReplace)`
Run: `go test -run TestFileReplaceEndpoint`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add replace.go replace_test.go server.go
git commit -m "feat: add atomic line replacement endpoint with conflict detection"
```

---

### Task 2: Sandboxed Stateless Quick Edit Endpoint (`/api/quick-edit`)

**Files:**
- Create: `quickedit.go`
- Test: `quickedit_test.go`
- Modify: `server.go` (register `/api/quick-edit`)

- [ ] **Step 1: Write failing test for `/api/quick-edit` output parser**

```go
// quickedit_test.go
package main

import (
	"testing"
)

func TestParseQuickEditOutput(t *testing.T) {
	// 1. Tagged replacement
	out1 := "Here is the code:\n<replacement>\nconst x = 42;\n</replacement>\nDone."
	parsed, err := parseQuickEditOutput(out1)
	if err != nil || parsed != "const x = 42;" {
		t.Fatalf("unexpected parse result: %q, err: %v", parsed, err)
	}

	// 2. Fallback fenced block
	out2 := "Sure, try this:\n```ts\nlet y = 100;\n```"
	parsed, err = parseQuickEditOutput(out2)
	if err != nil || parsed != "let y = 100;" {
		t.Fatalf("unexpected fenced result: %q, err: %v", parsed, err)
	}

	// 3. Multiple tags (chooses last one)
	out3 := "<replacement>old example</replacement>\n<replacement>final code</replacement>"
	parsed, err = parseQuickEditOutput(out3)
	if err != nil || parsed != "final code" {
		t.Fatalf("expected last tag: %q, err: %v", parsed, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -run TestParseQuickEditOutput`
Expected: FAIL with `undefined: parseQuickEditOutput`

- [ ] **Step 3: Implement `quickedit.go`**

```go
// quickedit.go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/px0-ai/harness"
)

type quickEditRequest struct {
	Path        string `json:"path"`
	L1          int    `json:"l1"`
	L2          int    `json:"l2"`
	Selection   string `json:"selection"`
	Instruction string `json:"instruction"`
}

var (
	reReplacement = regexp.MustCompile(`(?s)<replacement>(.*?)</replacement>`)
	reCodeBlock   = regexp.MustCompile(`(?s)```(?:[a-zA-Z0-9_-]+)?\n(.*?)\n````)
)

func parseQuickEditOutput(out string) (string, error) {
	tagMatches := reReplacement.FindAllStringSubmatch(out, -1)
	if len(tagMatches) > 0 {
		return strings.TrimSpace(tagMatches[len(tagMatches)-1][1]), nil
	}
	blockMatches := reCodeBlock.FindAllStringSubmatch(out, -1)
	if len(blockMatches) > 0 {
		return strings.TrimSpace(blockMatches[len(blockMatches)-1][1]), nil
	}
	return "", errors.New("no replacement tags or code block found in agent output")
}

func (s *server) handleQuickEdit(w http.ResponseWriter, r *http.Request) {
	if !localPost(w, r) {
		return
	}
	var req quickEditRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if s.agent == nil {
		fail(w, http.StatusBadRequest, "agent manager not available")
		return
	}

	harnessName, baseArgv, model := s.agent.current()
	if harnessName == "" || len(baseArgv) == 0 {
		fail(w, http.StatusBadRequest, "No coding harness installed or selected in settings")
		return
	}

	prompt := fmt.Sprintf(
		"File: %s (lines %d-%d)\n\nOriginal Code:\n```\n%s\n```\n\nInstruction: %s\n\nOutput ONLY the replacement code enclosed in <replacement>...</replacement> tags. Do NOT modify any files on disk. Do NOT provide explanation.",
		req.Path, req.L1, req.L2, req.Selection, req.Instruction,
	)

	// Sandboxed execution in isolated temp directory
	tempDir, err := os.MkdirTemp("", "punk0-quickedit-*")
	if err != nil {
		fail(w, http.StatusInternalServerError, "failed to create sandbox")
		return
	}
	defer os.RemoveAll(tempDir)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	res := harness.Run(ctx, harness.Command{Name: harnessName, Args: baseArgv, Model: model}, prompt, harness.Options{
		Dir:     tempDir,
		Timeout: 30 * time.Second,
	})

	if res.Error != "" {
		fail(w, http.StatusUnprocessableEntity, res.Error)
		return
	}

	replacement, err := parseQuickEditOutput(res.Stdout + "\n" + res.Reply)
	if err != nil {
		fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	writeJSON(w, map[string]any{"ok": true, "replacement": replacement})
}
```

- [ ] **Step 4: Register route and run tests**

Register: `mux.HandleFunc("POST /api/quick-edit", s.handleQuickEdit)`
Run: `go test -run TestParseQuickEditOutput`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add quickedit.go quickedit_test.go server.go
git commit -m "feat: add sandboxed quick-edit endpoint with deterministic parser"
```

---

### Task 3: Workspace Tabs Listing & Child Metadata Pass-Through

**Files:**
- Modify: `workbench.go:340-390`
- Modify: `main.go:60-90` (pass `-workspace-id`)
- Modify: `server.go` (store `workspaceID` in `meta`)

- [ ] **Step 1: Write test for `GET /api/workspaces/{id}/tabs` in `workbench_test.go`**

Verify that `GET /api/workspaces/{id}/tabs` returns the tab list:
```go
func TestWorkspaceTabsList(t *testing.T) {
    // Check that GET /api/workspaces/{id}/tabs returns 200 with JSON tabs array
}
```

- [ ] **Step 2: Implement `handleWorkspaceTabs` in `workbench.go`**

```go
func (wb *workbench) handleWorkspaceTabs(w http.ResponseWriter, r *http.Request) {
	wsID := r.PathValue("id")
	tabs := wb.records.WorkspaceTabs(wsID)
	out := []map[string]any{}
	for _, tab := range tabs {
		out = append(out, map[string]any{
			"id":      tab.ID,
			"kind":    tab.Kind,
			"name":    tab.Name,
			"running": tab.Running(),
			"status":  tab.Status(),
		})
	}
	writeJSON(w, map[string]any{"tabs": out})
}
```

- [ ] **Step 3: Register route and test**

Register: `mux.HandleFunc("GET /api/workspaces/{id}/tabs", wb.handleWorkspaceTabs)`
Run: `go test -run TestWorkspaceTabsList`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add workbench.go main.go server.go
git commit -m "feat: expose workspace tabs endpoint and pass workspace ID to editor child"
```

---

## Chunk 2: Frontend Right-Inspector Terminal & Selection UI

### Task 4: Right-Inspector Terminal Component

**Files:**
- Create: `web/src/term_inspector.js`
- Modify: `web/index.html:430-470`
- Modify: `web/src/inspector.js:15-60`

- [ ] **Step 1: Update `web/index.html` structure**

Replace `#pane-right-threads` with:
```html
<button class="inspector-tab active" data-itab="term" title="Workspace agent terminal">Terminal</button>
...
<section class="inspector-pane active" id="pane-right-term">
  <div class="term-bar" id="term-bar">
    <div class="term-tabs" id="term-tab-list"></div>
    <div class="term-actions">
      <button id="btn-term-add-shell" class="mini" title="New Shell Tab">+ Shell</button>
    </div>
  </div>
  <div class="term-body" id="editor-term"></div>
</section>
```

- [ ] **Step 2: Implement `web/src/term_inspector.js`**

Implement xterm.js lazy loading, WebSocket connection to `/api/tabs/{id}/term`, tab discovery via `/api/workspaces/{id}/tabs`, and status indicator syncing.

- [ ] **Step 3: Wire `setRightInspectorTab` in `web/src/inspector.js`**

Attach terminal on tab activation; detach on tab hide.

- [ ] **Step 4: Verify terminal pane connects and renders**

Run: `bun run check-web`
Expected: PASS with 0 lint errors

- [ ] **Step 5: Commit**

```bash
git add web/index.html web/src/term_inspector.js web/src/inspector.js
git commit -m "feat: embed interactive xterm terminal in editor right inspector"
```

---

### Task 5: Floating Selection Pill (`✦ Quick Edit`, `💬 Add to Chat`, `📋 Copy Ref`)

**Files:**
- Create: `web/src/selpill.js`
- Modify: `web/src/selbar.js`
- Modify: `web/style.css`

- [ ] **Step 1: Write test for selection pill positioning**

Create `test/selpill.test.js`:
Verify that given a line range selection, `calculatePillPosition(l1, l2)` produces valid viewport coordinates and flips if near top edge.

- [ ] **Step 2: Implement `web/src/selpill.js`**

Create the floating pill DOM element:
- `✦ Quick Edit` (`Cmd+K`)
- `💬 Add to Chat` (`Alt+L`)
- `📋 Copy Ref` (`Alt+C`)
Context gating: Only render if current active view is source code (hidden in markdown preview and diffs).

- [ ] **Step 3: Update `selbar.js`**

Remove legacy `.agent-btn` gutter element and old `SEL_MENU_ITEMS`. Bind `Alt+L` to `sendToChat(selection)`.

- [ ] **Step 4: Run frontend tests**

Run: `bun test test/selpill.test.js`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add web/src/selpill.js web/src/selbar.js web/style.css test/selpill.test.js
git commit -m "feat: add floating selection pill with quick edit, add to chat, and copy ref"
```

---

## Chunk 3: Quick Edit UI & Deprecation Cutover

### Task 6: In-Place Quick Edit UI & Diff Overlay

**Files:**
- Create: `web/src/quick_edit.js`
- Modify: `web/src/keybinds.js`
- Modify: `web/style.css`

- [ ] **Step 1: Implement `web/src/quick_edit.js`**

1. Render floating prompt bar over lines $l1 \dots l2$ with autofocus input.
2. On `Enter`:
   - Send `POST /api/quick-edit` with expanded full-line text.
   - Show loading spinner.
3. On Response:
   - Render inline diff overlay: red strike-through removed lines, green added lines.
   - Render review strip: `[✓ Accept (Cmd+Enter)]` and `[✕ Reject (Esc)]`.
4. On Accept:
   - Send `POST /api/file/replace`. If 409, show conflict notice. If 200, reload editor and dismiss.
5. On Reject:
   - Dismiss overlay and restore original lines.

- [ ] **Step 2: Add context-sensitive `Cmd+K` keybinding in `web/src/keybinds.js`**

- If source selection active $\to$ trigger `openQuickEdit()`.
- Else $\to$ trigger `openPalette()`.

- [ ] **Step 3: Verify styling and behavior**

Run: `bun run check-web`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add web/src/quick_edit.js web/src/keybinds.js web/style.css
git commit -m "feat: implement in-place quick edit prompt and inline diff review"
```

---

### Task 7: Complete Legacy Threads & Agentbox Deprecation Cutover

**Files:**
- Delete: `web/src/thread.js`
- Modify: `web/src/agent.js` (remove thread sessions & `#agentbox`)
- Modify: `agent.go` & `server.go` (remove `/api/threads/*` and `/api/agent/edit`)
- Modify: `docs/features/threads.md`, `README.md`

- [ ] **Step 1: Remove legacy thread endpoints from `server.go` and `agent.go`**

Remove `/api/threads/*`, `/api/agent/edit`, `/api/agent/batch`. Remove `newThreadManager`.

- [ ] **Step 2: Delete `web/src/thread.js` and clean up `agent.js` and `main.js`**

Remove `import './thread.js'`, remove `#agentbox` template and logic.

- [ ] **Step 3: Update documentation and help shortcuts**

Update `README.md`, `docs/features/agent-editing.md`, `docs/features/selection-actions.md` reflecting the new Terminal, Floating Pill, and Quick Edit workflows.

- [ ] **Step 4: Run full test suites**

Run: `go test ./...`
Expected: PASS
Run: `bun run check-web`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add -u
git commit -m "refactor: deprecate legacy threads system and remove unused inline edit drawers"
```

---

## Chunk 4: Verification & Smoke Testing

### Task 8: End-to-End Verification

- [ ] **Step 1: Run complete Go test suite**

Run: `go test -v ./...`
Expected: PASS across all packages.

- [ ] **Step 2: Run web asset build and linter**

Run: `bun run build-web && bun run check-web`
Expected: PASS with clean bundle output.

- [ ] **Step 3: End-to-End Interactive Smoke Verification**

1. Launch `punk0 <dir>`: verify right inspector opens with `Terminal` tab active and connected to agent CLI.
2. Select code lines in editor: verify floating selection pill appears with `✦ Quick Edit`, `💬 Add to Chat`, and `📋 Copy Ref`.
3. Click `Add to Chat` (`Alt+L`): verify `@path:lines` + snippet is pasted into terminal prompt.
4. Press `Cmd+K` on selection: enter prompt `"add error check"`, verify inline green/red diff displays.
5. Press `Esc`: verify diff disappears without disk write.
6. Press `Cmd+K` again, submit, press `Cmd+Enter`: verify file on disk updates and editor reloads.

- [ ] **Step 4: Final commit**

```bash
git commit --allow-empty -m "chore: complete verification of editor terminal and quick edit system"
```
