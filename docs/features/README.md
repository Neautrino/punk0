# punk0 Features Guide

punk0 is an ultra-fast, zero-config code reader and navigator specifically optimized for fast reads, remote inspection, and AI-assisted workflows. It provides instant access to codebases of any size with minimal resource consumption (px0, which punk0 forks, measured ~20–30 MB server RAM, ~100–180 MB total including the browser tab, and < 1 ms startup; not re-measured for punk0).

This directory provides comprehensive documentation for all punk0 user-facing features, detailing how each capability works, its role in developer and AI agent pairing workflows, interactive controls, and configuration options.

---

## Feature Matrix

| Feature Area | Primary Shortcut | Core Capability | Detailed Guide |
| :--- | :--- | :--- | :--- |
| **Fuzzy File Search** | `Cmd/Ctrl+P`, `Cmd/Ctrl+K` | Instant fuzzy path finding across 100,000+ files with recency ranking | [Fuzzy File Search](fuzzy-file-search.md) |
| **Workspace Search** | `Cmd/Ctrl+Shift+F` | Full-repository literal and regex text search with match previews | [Workspace Search](workspace-search.md) |
| **Symbol Outline** | `Cmd/Ctrl+Shift+O` | In-file function, class, and struct hierarchy with regex fallback | [Symbol Outline](symbol-outline.md) |
| **In-File Find & Caret** | `Cmd/Ctrl+F`, `Cmd/Ctrl+G` | Active document search, minimap match markers, and line jumps | [In-File Search](in-file-search.md) |
| **Git Awareness, Diffs & Stage/Commit/Push/Pull** | `Cmd/Ctrl+D` | Real-time status stream, stat cache fast-path, split / unified diffs with intra-line highlights, single-hunk staging/discard, a VS Code-style source control panel with one contextual commit/push/pull button, and AI rebase/merge | [Git Integration](git-integration.md) |
| **Agents Page (Orchestrator)** | `punk0`, `punk0 <dir>` | Projects and their local, worktree and scratch workspaces; shell and Claude Code terminal tabs that outlive the browser and show agent status; each folder's editor in its own tab, language servers opt-in per folder; PRs as worktrees of your existing clone | [Agents Page](workbench.md) |
| **Pull Request Review** | `punk0 <pr-url>`, `Alt+R` | GitHub, GitLab, Gitea/Forgejo and Bitbucket Cloud: full-tree checkout, merge-base diffing, draft comments with Approve / Request Changes / Comment, AI conflict resolution, merging, and committing/pushing/pulling straight from the checkout | [Pull Request Review](github-pr-review.md) · [Getting Started](pr-review-getting-started.md) |
| **Tips & Tricks System** | `?` / `#btn-help` | Interactive floating tip widget, Tips tab in the help card, and offline fallbacks for feature discoverability | [Tips System](tips-system.md) |
| **Direct File Editing** | `Cmd/Ctrl+E`, `Cmd/Ctrl+S` | Edit and save a file in the browser, in-editor find (`Cmd/Ctrl+F`), create new files and folders, word wrap and unsaved-change protection | [File Editing](file-editing.md) |
| **Editor Terminal & Quick Edit** | `Alt+T`, `Cmd/Ctrl+K`, `Alt+L` | The workspace's shell and agent tabs on the editor's right side, a floating pill over code selections, and Quick Edit: one harness run on the selected lines with a -/+ preview before anything is written | [Editor Terminal & Quick Edit](editor-terminal-and-quick-edit.md) |
| **Coding Harnesses** | ⚡ Batch Apply | The installed harness (Claude Code, Codex, OpenCode, Antigravity, Cursor Agent) behind Quick Edit, AI rebase/merge and PR Batch Apply | [Agent Editing](agent-editing.md) |
| **Semantic Code Intelligence** | `F12`, `Shift+F12`, `Alt+Shift+H` | Go to Definition, Find References, Call Trails, inline diagnostics (`publishDiagnostics`), and Hover docs | [LSP & Intelligence](lsp-code-intelligence.md) |
| **Markdown Preview** | `Alt+M` | Full GFM preview, Mermaid diagrams, syntax-highlighted code fences, and scroll sync | [Markdown Preview](markdown-preview.md) |
| **CSV / TSV Table View** | `Alt+M` | Data files open as a table with a sticky header and source-line gutter | [Table View](table-view.md) |
| **Image Viewer & Assets** | Click image file / lightbox | Standalone image tabs, zoom/pan transforms, and markdown lightbox | [Image Viewer](image-viewer.md) |
| **Settings & Preferences** | `Cmd/Ctrl+,` | Graphical form editor, raw JSON sync, and instant live preview | [Settings & Configuration](settings-and-configuration.md) |
| **Syntax Highlighting** | Automatic | Viewport-windowed Chroma lexing for ~280 languages | [Syntax Highlighting](syntax-highlighting.md) |
| **Themes & Styling** | Settings / Palette | 14 built-in dark and light themes powered by CSS tokens | [Themes & Styling](themes-and-styling.md) |
| **Selection Actions** | `Cmd/Ctrl+K`, `Alt+L`, `Alt+C`, `Alt+A`, `Alt+U` | Quick Edit, add to the agent's chat, copy reference, copy with context, find usages | [Selection Actions](selection-actions.md) |
| **Virtualized Scroller** | Automatic | Virtual DOM rendering ~60 rows with idle memory scavenging | [Editor Virtualization](editor-virtualization.md) |
| **Remote Workspaces** | CLI flags | Zero-config remote browsing over Tailscale, SSH-free operation | [Remote Workspaces](remote-workspaces.md) |
| **Vim Keybindings** | Toggle in Settings | Modal normal, visual, and motion modes for keyboard navigation | [Vim Mode](vim-mode.md) |
| **File Explorer** | `Cmd/Ctrl+B` | High-density tree, expand/collapse-all controls, compact folder chains, and tab management | [File Explorer](file-explorer.md) |

---

## 1. Navigation & Search

Fast code discovery is essential when reviewing large codebases or inspecting agent changes. punk0 provides three complementary navigation layers:

- **[Fuzzy File Search & Quick Open](fuzzy-file-search.md)**: Jump directly to any file in the workspace using progressive fuzzy filtering (`Cmd/Ctrl+P`). It scores matches by boundary transitions, file extensions, and recent tab activity so target files appear with only 2–3 keystrokes.
- **[Full Workspace Search](workspace-search.md)**: Search across every file in the repository for text patterns or regular expressions (`Cmd/Ctrl+Shift+F`). Results are grouped by file with context snippets and match counts.
- **[In-File Search & Caret Navigation](in-file-search.md)**: Quickly find occurrences within the currently active document (`Cmd/Ctrl+F`), jump to line and column coordinates (`Cmd/Ctrl+G`), and traverse browsing history (`Alt+Left` / `Alt+Right`).

---

## 2. Code Intelligence & Structure

Understanding complex software systems requires more than raw text search. punk0 delivers structural and semantic code intelligence while maintaining an ultra-lightweight footprint:

- **[Symbol Outline Navigation](symbol-outline.md)**: Open a structured list of symbols (`Cmd/Ctrl+Shift+O`) to view all functions, methods, classes, types, and variables defined within the active document.
- **[Language Server Protocol (LSP) Integration](lsp-code-intelligence.md)**: Tap into semantic analysis for Go, Rust, TypeScript, Python, C/C++, and other languages. Jump directly to definitions (`F12`), explore all reference call sites (`Shift+F12`), inspect interactive Call Trails (`Alt+Shift+H`), and view documentation cards on hover.
- **[Syntax Highlighting & Language Support](syntax-highlighting.md)**: Beautiful native highlighting for ~280 programming languages and markup formats via Chroma, enhanced with bracket pair colorization and word occurrence highlighting.

---

## 3. Git Integration & Review

Modern developers spend substantial time verifying diffs and reviewing code generated by background agents:

- **[Git Awareness & Visual Diff Viewer](git-integration.md)**: Real-time Git status synchronization powered by Server-Sent Events and sub-millisecond control file stat checks. Status badges and parent directory dirty indicators update without full re-indexing. A segmented sidebar switch toggles between the file explorer and uncommitted Git changes, while `Cmd/Ctrl+D` reveals side-by-side or unified diffs with intra-line token-level Myers diff character highlighting, insertion carets, live gutter indicators, and automatic tab cleanup when changes are checked out or reset.
- **[Stage, Commit, Push, Pull & AI Rebase/Merge](git-integration.md)**: A resizable, VS Code-style git panel at the bottom of the sidebar stages files (per-file tick, hunk-level stage/unstage/discard, or Stage all), commits exactly what is staged, pushes, and pulls fast-forward-only, all from one contextual primary button with a ▾ menu (Amend and Undo last commit included). If history has diverged, punk0 displays a notice card offering **Rebase with AI** (to replay commits linearly) or **Merge with AI** (to join histories via merge commit) using your configured coding harness, with full abort/finish controls.
- **[Commit Graph](git-integration.md)**: A VS Code-style commit graph at the bottom of the git panel: lanes, branch and tag badges, and the commits still to push (Outgoing) or pull (Incoming) grouped apart. Expand any commit to inspect touched files, view diffs in commit-pinned tabs with ‹/› neighbor navigation, and copy SHAs.
- **[Getting Started with Pull Request Review](pr-review-getting-started.md)**: Your first review end to end: open a PR, connect your forge (token steps for GitHub, GitLab, Gitea/Forgejo and Bitbucket), review, fix, resolve conflicts, merge, plus troubleshooting.
- **[Pull Request Review](github-pr-review.md)**: `punk0 <pr-url>` checks out a GitHub, GitLab, Gitea/Forgejo or Bitbucket Cloud PR's full source tree into its own process, diffs it against the PR's merge-base instead of `HEAD`, lets you draft inline review comments, batch-apply them with coding agents, submit Approve / Request Changes / Comment, have your coding harness resolve conflicts with the base branch (**Rebase with AI**), rewrite commit messages with AI upon squash, and merge with any method the repository allows — and the same git panel lets you commit and push fixes back to the PR's own branch, or pull in commits pushed by someone else mid-review.

---

## 4. Coding Agents in the Editor

punk0 intentionally omits a heavyweight text editor in favor of working alongside the coding agents you already use:

- **[Editor Terminal, Selection Pill & Quick Edit](editor-terminal-and-quick-edit.md)**: The editor's right side (`Alt+T`, `Cmd/Ctrl+J`) has two modes: **Terminal**, the workspace's shell and agent tabs, the same ones the agents page shows, with a **+** menu to start more; and **Chats**, this folder's earlier chats to preview and resume. ⛶ gives it the whole editor width. Selecting code shows a pill with **✦ Quick Edit** (`Cmd/Ctrl+K`), **Add to Chat** (`Alt+L`, sends the selection to the agent in the active terminal tab) and **Copy Ref** (`Alt+C`). Quick Edit runs your coding harness once on the selected lines and shows a -/+ preview; nothing is written until you accept it.
- **[Coding Harnesses](agent-editing.md)**: Which harnesses punk0 detects (Claude Code, Codex, OpenCode, Antigravity, Cursor Agent), how you pick one, and the features that run it: Quick Edit, AI rebase/merge, the PR merge message and PR Batch Apply. Changed files reload in place when a run finishes.
- **[Selection Toolbar & Context Actions](selection-actions.md)**: The pill, the footer selection bar and the right-click menu: Quick Edit, Add to Chat (`Alt+L`), copy `path:line` references (`Alt+C`), copy code with its path and lines for an LLM (`Alt+A`), and find usages (`Alt+U`).

---

## 5. Documentation & Media Viewing

Repositories contain documentation, architecture notes, and graphical assets alongside code:

- **[Rendered Markdown Preview](markdown-preview.md)**: Read project documentation, RFCs, and README files in rendered GitHub Flavored Markdown (GFM) mode (`Alt+M`), complete with tables, task lists, footnotes, and GitHub-style alert callouts (`[!NOTE]`, `[!WARNING]`). Features bi-directional scroll synchronization and one-click copy buttons on code blocks.
- **[CSV & TSV Table View](table-view.md)**: Open `.csv` and `.tsv` files as a table (`Alt+M` toggles the source). Quoted cells with commas and line breaks parse correctly, the header row stays pinned, and a gutter numbers each row by its source line so find and go-to-line still line up. Large files show the first 1,000 rows.
- **[Image Viewer & Asset Inspection](image-viewer.md)**: Open image files (PNG, SVG, JPG, WebP, GIF, etc.) as native interactive tabs. Smoothly zoom up to 3200%, pan freely, toggle alpha background modes (checkerboard, dark matte, light matte), switch between bilinear smoothing and pixelated rendering, and click inline Markdown images to inspect them in a modal lightbox.

---

## 6. Ergonomics & Customization

punk0 adapts to developer habits and viewing environments without requiring manual config file tinkering:

- **[Settings & Preferences System](settings-and-configuration.md)**: Comprehensive settings manager accessible via `Cmd/Ctrl+,`. Offers a VS Code-style graphical UI with interactive attribute pills alongside a raw JSON editor (`$XDG_CONFIG_HOME/punk0/settings.json`, else `~/.punk0/settings.json`) that updates themes, typography, diff layouts, and search behavior in real time without refreshing the browser.
- **[Themes & Styling](themes-and-styling.md)**: 14 built-in dark and light themes crafted for high readability and visual consistency across all panels, diffs, and markdown documents.
- **[Tips & Tricks System](tips-system.md)**: Interactive discoverability system featuring a gentle floating tip widget, Tips tab in the help card, `#btn-help` status bar trigger, and offline fallbacks.
- **[Vim Keybindings & Modal Navigation](vim-mode.md)**: Full modal navigation emulation supporting Normal, Visual, and Motion modes for developers accustomed to Vim, Neovim, or Helix.
- **[File Explorer & Workspace Management](file-explorer.md)**: Dense, clean file tree with single-child folder compacting, `.gitignore` dimming, expand/collapse-all buttons, tab shortcuts (`Alt+1..9`, `Ctrl+Tab`, `Alt+W`), and right-click tab close actions.

---

## 7. Performance & Remote Workspaces

Engineered from the ground up for instantaneous response times and zero-friction remote usage:

- **[Editor Virtualization & Memory Scavenging](editor-virtualization.md)**: Handles 500,000-line files effortlessly by rendering only ~60 visible rows in the browser DOM. Automatically releases memory back to the operating system after 15 seconds of inactivity.
- **[Remote Workspaces & Cloud Inspection](remote-workspaces.md)**: Run punk0 on remote servers, cloud VMs, Docker containers, or CI runners and view code in your local browser over Tailscale or private networks without SSH keys, port forwarding setups, or remote desktop daemons.
