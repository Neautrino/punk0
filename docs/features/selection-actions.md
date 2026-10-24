# Selection Toolbar & Context Actions

When code is selected, px0 offers actions in three places: a floating pill just above the selection (Quick Edit, Add to Chat, Copy Ref), the footer selection bar (the copy actions), and a right-click menu with all of them plus Find Usages. Every action also has a keyboard shortcut.

---

## Overview & Core Purpose

Sharing code with a teammate or an AI agent usually means selecting lines, copying them, writing down the path and line numbers, and adding a code fence by hand. px0 turns a selection into those steps directly: copy a `path:line` reference, copy the code with its path and lines in a fenced block, send that block to the agent in the editor's terminal, or have your coding harness rewrite the lines with Quick Edit.

---

## Where the Actions Appear

- **Selection pill**: floats just above a selection in the source view, with **✦ Quick Edit** (`Mod+K`), **Add to Chat** (`Alt+L`) and **Copy Ref** (`Alt+C`). It is not shown in the diff view or the Markdown preview, and it hides when the view scrolls.
- **Footer selection bar**: while code is selected, the left of the status bar trades its navigation buttons for the copy actions, and the right shows the selection's line and character count. It never covers code.
- **Right-click menu**: a right click on a selection opens the actions at the pointer. On code it offers Quick Edit (source view only), Add to Chat, Copy Source, Copy Ref, Copy with Context and Find Usages. In the Markdown preview it keeps its own copy items: Copy Formatted Text, Copy Raw Markdown, Copy Ref and Copy with Context. A right click on plain code with no selection keeps the browser's menu.

---

## Key Actions & Capabilities

### 1. Quick Edit (`Mod+K`)
Opens a box below the selected lines. Type an instruction and your coding harness returns a replacement, shown as a -/+ preview; **Accept** (`Mod+Enter`) writes it, **Reject** (`Esc`) writes nothing. Source view only. With no selection, `Mod+K` opens Quick Open instead. See [Quick Edit](editor-terminal-and-quick-edit.md#quick-edit).

### 2. Add to Chat (`Alt+L`)
Sends the selection to the agent in the active tab of the editor's [Terminal](editor-terminal-and-quick-edit.md#the-terminal-tab) as `@path lines a-b` followed by the code in a fenced block, the same text Copy with Context copies. Works in the source and diff views.

### 3. Copy Source (`Mod+C`)
Copies the highlighted code as is:
- Excludes line numbers and gutter buttons.
- Preserves leading indentation, whitespace and newlines.
- Resolves lines outside the rendered window of the virtualized editor.

### 4. Copy Raw Markdown (`Alt+M`)
In the Markdown preview, copies the raw Markdown source of the selected lines.

### 5. Copy Formatted Text (`Mod+C` in Preview)
In the Markdown preview, copies both rich text (`text/html`) and plain text (`text/plain`), so formatting survives a paste into Google Docs, Slack, Notion or email.

### 6. Copy Reference (`Alt+C`)
Copies a pointer to the selected code: `path/to/file.go:42` for one line, `path/to/file.go:42-68` for a range. Paste it into a review comment, an issue, or an agent prompt.

### 7. Copy with Context (`Alt+A`)
Copies a block ready for an LLM prompt:

````
@path/to/file.go lines 42-68
```go
…selected code…
```
````

### 8. Find Usages (`Alt+U`)
Searches the workspace for the selected symbol and opens the results in the right inspector.

---

## Split Diff Selection

When selecting in the split (side-by-side) diff, on either the left (`HEAD`) or right (working tree) side, px0 normalizes the selection:
- Gutter line numbers and `+`/`-` signs are excluded.
- The selection is mapped to working-tree line numbers, so Copy Ref and Add to Chat point at the file on disk.

---

## Keyboard Shortcuts & Interaction Matrix

| Action | Shortcut | Pill | Footer Bar | Right-Click | Description |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **Quick Edit** | `Mod+K` | ✓ | | ✓ (source view) | Rewrite the selection with your coding harness, with a preview |
| **Add to Chat** | `Alt+L` | ✓ | | ✓ | Send the selection to the agent in the active terminal tab |
| **Copy Source** | `Mod+C` | | ✓ | ✓ | Copy the code as is, without line numbers |
| **Copy Text (Preview)** | `Mod+C` | | ✓ | ✓ | Copy plain text and formatted HTML |
| **Copy Raw Markdown** | `Alt+M` | | ✓ | ✓ | Copy the Markdown source of the selection |
| **Copy Ref** | `Alt+C` | ✓ | ✓ | ✓ | Copy `path:10-25` |
| **Copy with Context** | `Alt+A` | | ✓ | ✓ | Copy `@path lines a-b` and the fenced code |
| **Find Usages** | `Alt+U` | | | ✓ | Find references across the workspace |

In a pull request review session, the right-click menu also offers **Add Review Comment** (`Alt+R`). See [Pull Request Review](github-pr-review.md).

### Line Actions

In a pull request review's diff, a reviewable line number shows a **Comment** button. Clicking it opens the actions menu aimed at that line, including **Add Review Comment**.

---

## Non-Intrusive Ergonomics

- **Small pill, out of the code's way**: the pill sits above the selection and holds three buttons; it disappears as soon as the selection collapses or the view scrolls.
- **State restoration**: when the selection collapses, the status bar returns to the file's line and column, language and git details.
