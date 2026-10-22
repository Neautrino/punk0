# Settings & Configuration System

px0 includes a built-in visual Settings manager modeled after VS Code. Accessible via `Cmd/Ctrl+,` or the gear icon in the status bar, it enables complete customization of editor ergonomics, typography, themes, diff views, and coding agents with live real-time synchronization.

---

## Overview & Core Purpose

Developer environments are deeply personal: engineers have strong preferences regarding font sizes, line heights, font families, cursor blink styles, tab spacing, and keybindings. However, managing configuration files manually in text editors or dealing with fragmented config directories can be cumbersome. Furthermore, storing settings inside a workspace repository risks polluting Git commits with personal editor preferences.

px0 stores all configuration in a single per-user global file (`~/.px0/settings.json` or `$XDG_CONFIG_HOME/px0/settings.json`), completely isolated from your repository files. The visual Settings editor offers a dual-mode experience: an intuitive graphical form with interactive pill buttons for quick toggling, and a synchronized raw JSON editor with schema validation. Changes apply immediately in real time without refreshing the browser.

---

## Key Capabilities

- **Dual UI & Raw JSON Modes**: Switch instantly between the graphical form editor and raw JSON mode with a single click. Changes made in either view synchronize bidirectionally in real time.
- **Interactive Attribute Pills**: Every setting displays metadata badges (category, type, default value) along with interactive pill buttons for quick selection (e.g., `[line]`, `[block]`, `[underline]` for cursor styles; `[true]`, `[false]` for boolean toggles). Clicking any pill applies that value immediately.
- **Instant Live Preview Without Reload**: Adjusting font sizes, line heights, themes, cursor animations, or diff modes takes effect immediately across all open tabs without requiring a page reload.
- **One-Click Factory Reset**: Any setting that has been customized displays an amber `Modified` badge and an inline `Reset` button, allowing you to restore default settings individually.
- **Lists and Card Notes**: A list setting such as `agents.hidden` shows one checkbox per choice. Exec-gated cards note that they can only be changed when punk0 listens on 127.0.0.1. Turning `notifications.desktop` on asks the browser for permission; when the page is not on https or localhost, or the browser blocked notifications, the card says so and the value is saved anyway. A refused save shows the server's reason in the footer and the card keeps the stored value.
- **Clean Workspace Separation**: All configuration lives in your user directory. Your repository working trees, `.git` configs, and tracked files remain 100% pristine.

---

## Key Configurable Settings

| Setting Key | Category | Default | Allowed Values / Range | Description |
| :--- | :--- | :--- | :--- | :--- |
| `editor.fontSize` | Text Editor | `13.5` | `9.0` – `32.0` (px) | Font size in the code viewer |
| `editor.fontFamily` | Text Editor | JetBrains Mono stack | CSS font stack string | Font family stack for viewer |
| `editor.lineHeight` | Text Editor | `21.0` | `14.0` – `48.0` (px) | Line height for viewer rows |
| `editor.tabSize` | Text Editor | `4` | `2`, `4`, `8` | Number of spaces per tab |
| `editor.wordWrap` | Text Editor | `"on"` | `"on"`, `"off"` | Soft line wrapping at editor boundary (also applies to the edit view) |
| `editor.lineNumbers` | Text Editor | `"on"` | `"on"`, `"off"` | Line numbers display in gutter |
| `editor.cursorStyle` | Text Editor | `"line"` | `"line"`, `"block"`, `"underline"` | Cursor rendering style |
| `editor.cursorBlinking` | Text Editor | `"smooth"` | `"blink"`, `"smooth"`, `"solid"` | Cursor blinking animation style |
| `editor.renderLineHighlight` | Text Editor | `"line"` | `"line"`, `"none"` | Highlight style for the active line |
| `editor.occurrencesHighlight` | Text Editor | `true` | `true`, `false` | Highlight matches of selected identifier |
| `editor.scrollBeyondLastLine` | Text Editor | `true` | `true`, `false` | Enable scrolling beyond document end |
| `editor.bracketPairColorization` | Text Editor | `true` | `true`, `false` | Rainbow bracket pairs & matching |
| `editor.vimMode` | Text Editor | `false` | `true`, `false` | Modal Vim navigation keybindings |
| `markdown.preview.open` | Workbench | `true` | `true`, `false` | Open Markdown files in rendered preview |
| `table.preview.open` | Workbench | `true` | `true`, `false` | Open CSV and TSV files as a table |
| `diffEditor.renderSideBySide` | Diff Editor | `true` | `true`, `false` | Split vs. unified diff view default |
| `diffEditor.ignoreTrimWhitespace` | Diff Editor | `true` | `true`, `false` | Ignore whitespace differences in diffs |
| `git.gutterIndicators` | Git | `true` | `true`, `false` | Visual change markers in gutter |
| `git.commitMessageInstruction` | Git & Diff | `""` | any string (multi-line textarea) | Extra instructions given to the coding harness when the git panel's **Commit with AI** writes a commit message |
| `explorer.compactFolders` | Explorer | `true` | `true`, `false` | Compact single-child directory chains |
| `explorer.autoReveal` | Explorer | `true` | `true`, `false` | Auto-scroll to active file in tree |
| `files.exclude` | Files | Default globs | Array of glob patterns | Exclude patterns from trees and searches |
| `search.smartCase` | Search | `true` | `true`, `false` | Case-insensitive unless query has uppercase |
| `search.maxResults` | Search | `1000` | `50` – `10,000` | Maximum search results returned |
| `lsp.enabled` | Language Servers | `true` | `true`, `false` | Master switch: off stops every running language server in every folder (within a few seconds) and starts none; each folder's own choice is kept |
| `lsp.idleStop` | Language Servers | `15` | minutes, `0`–`1440` | Stop a language server unused for this long, even while its editor is open; the next use starts it again. `0` never |
| `lsp.disabled` | Language Servers | `[]` | server names (`gopls`, `rust-analyzer`, `pyright`, `pylsp`, `ruff`, `tsc`, `typescript`, `clangd`, `zls`, `lua`, `solargraph`, `jdtls`, `omnisharp`, `texlab`) | Servers punk0 never starts; another installed server for the same language is used instead |
| `lsp.memoryWarning` | Language Servers | `2048` | MB, `0` or more | Agents page: the language server memory total turns red above this. `0` never |
| `lsp.hover.enabled` | Language Servers | `true` | `true`, `false` | Hover documentation cards |
| `workspace.lspDefault` | Language Servers | `false` | `true`, `false` | Default of the New workspace form's Language Servers checkbox for worktrees |
| `agent.harness` | Coding Agent | `""` | `claude`, `gemini`, `agy`, etc. | Preferred CLI coding harness |
| `agent.timeoutSeconds` | Coding Agent | `120` | `10` – `600` (seconds) | Max runtime for agent edits |
| `github.token` | GitHub | `""` | any string | Personal access token for GitHub pull request review; takes precedence over `GITHUB_TOKEN` and `gh auth token`. Masked in the Settings UI. |
| `gitlab.token` | GitLab | `""` | any string | Personal access token (`api` scope) for GitLab merge request review. Sent only to `gitlab.host`; takes precedence over `GITLAB_TOKEN`. Masked in the Settings UI. |
| `gitlab.host` | GitLab | `""` | host name | The instance `gitlab.token` belongs to. Falls back to `GITLAB_HOST`, then `gitlab.com`. Set automatically when you connect through the Connect dialog. |
| `gitea.token` | Gitea | `""` | any string | Access token for Gitea, Forgejo or Codeberg pull request review. Sent only to `gitea.host`; takes precedence over `GITEA_TOKEN`. Masked in the Settings UI. |
| `gitea.host` | Gitea | `""` | host name | The instance `gitea.token` belongs to. Falls back to `GITEA_HOST`, then `codeberg.org`. Set automatically when you connect through the Connect dialog. |
| `bitbucket.token` | Bitbucket | `""` | any string | Bitbucket Cloud access token, or an Atlassian API token together with `bitbucket.email`. Takes precedence over `BITBUCKET_TOKEN`. Masked in the Settings UI. |
| `bitbucket.email` | Bitbucket | `""` | email | The Atlassian account email of an API token; empty for an access token. Falls back to `BITBUCKET_EMAIL`. |
| `server.basePath` | Server | `"/"` | any path prefix (e.g. `"/rev-123/"`) | Base URL path prefix to serve endpoints and assets from. Overridden by the `-base-path` CLI flag. |

---

## Agents Page Settings

The categories listed first in the modal belong to punk0 itself and apply to the agents page, its tabs and workspaces:

| Setting Key | Category | Default | Allowed Values / Range | Description |
| :--- | :--- | :--- | :--- | :--- |
| `general.openBrowser` | General | `true` | `true`, `false` | Open the agents page when punk0 starts; `-no-open` overrides |
| `update.auto` | General | `true` | `true`, `false` | Check for and install releases in the background; `-no-update` overrides |
| `tips.enabled` | General | `true` | `true`, `false` | Show tips in the interface |
| `workbench.colorTheme` | Appearance | `"catppuccin-mocha"` | 14 built-in theme IDs | Color theme of both pages |
| `workbench.uiFontFamily` | Appearance | `""` | CSS font stack | Interface font; empty uses the theme's |
| `layout.fileView` | Appearance | `"beside"` | `beside`, `tabs` | Files in a panel beside the terminals, or among the terminal tabs (agents page) |
| `agents.default` | Agents | `"claude"` | agent id or `shell` | Agent New chat and New workspace start with |
| `agents.hidden` | Agents | `[]` | list of agent ids | Agents left out of menus and the welcome screen |
| `agents.<id>.args` ⚿ | Agents | `""` | shell words | Extra arguments after the program name, one key per agent (`claude`, `codex`, `opencode`, `agy`, `cursor-agent`) |
| `terminal.fontSize` | Terminal | `13` | `8` – `32` (px) | Terminal font size |
| `terminal.fontFamily` | Terminal | `""` | CSS font stack | Terminal font |
| `terminal.lineHeight` | Terminal | `1.0` | `1.0` – `2.0` | Terminal line height |
| `terminal.scrollback` | Terminal | `10000` | `1000` – `100000` lines | Scrollback kept per tab |
| `terminal.cursorStyle` | Terminal | `"block"` | `"block"`, `"bar"`, `"underline"` | Terminal cursor shape |
| `terminal.cursorBlink` | Terminal | `true` | `true`, `false` | Blink the terminal cursor |
| `terminal.copyOnSelect` | Terminal | `false` | `true`, `false` | Copy selected text automatically |
| `terminal.confirmCloseRunning` | Terminal | `true` | `true`, `false` | Ask before closing a tab that is still running |
| `terminal.shell` ⚿ | Terminal | `""` | program path | Program new tabs run; empty uses `$SHELL`, else `/bin/sh` |
| `terminal.env` ⚿ | Terminal | `""` | `KEY=VALUE` per line | Added to every tab's environment; `~/` expands to the home folder, `#` lines are comments |
| `terminal.idleStop` | Terminal | `30` | minutes, `0` never | Stop a tab that carries nothing for this long; a shell only at its prompt, an agent only when idle |
| `workspace.branchPrefix` | Workspaces | `""` | branch prefix | Prefix for branches New workspace creates (the folder stays unprefixed) |
| `workspace.fetchBase` | Workspaces | `true` | `true`, `false` | Fetch the base branch before creating a workspace |
| `workspace.deleteMergedBranch` | Workspaces | `false` | `true`, `false` | Delete a workspace's branch with it when it is merged; the delete response notes why it was kept otherwise |
| `workspace.idleStop` | Workspaces | `5` | minutes, `0` never | Stop a folder's editor process after this long without requests; the next request starts it again |
| `notifications.desktop` | Notifications | `"off"` | `"off"`, `"unfocused"`, `"always"` | Desktop notifications; asks for browser permission when turned on |
| `notifications.onWaiting` | Notifications | `true` | `true`, `false` | Notify when an agent waits for input |
| `notifications.onDone` | Notifications | `true` | `true`, `false` | Notify when an agent finishes |
| `notifications.sound` | Notifications | `false` | `true`, `false` | Play a sound with a notification |
| `notifications.titleBadge` | Notifications | `true` | `true`, `false` | Count waiting tabs in the page title |

⚿ marks an Exec-gated key, see below.

---

## Validation & Remote Mode

Every save is checked against the schema before it is written, from the form and from raw JSON alike: a value of the wrong type, a choice that is not one of the options, a list element that is not one of the options, or a number out of range is refused with an error naming the key, and a bad `terminal.env` line is refused naming the line. Keys punk0 does not know are stored as given.

Some values end up in a command line or a process environment: `agents.<id>.args`, `terminal.shell`, `terminal.env` and `agent.harness`. When punk0 listens on anything other than 127.0.0.1, anyone who reaches the port could otherwise plant a command that runs the next time you open a tab or start an agent edit. So in remote mode those keys can only be removed or reset to their default; changing them is refused with `<key> can only be changed when punk0 listens on 127.0.0.1`; picking a harness that is not built in (a command template) is refused the same way, built-in harnesses and models can still be picked. Saving the whole form with those keys untouched still works. Change them on the machine itself, or in the settings file.

---

## Accessing Settings

- **Keyboard Shortcut**: Press **`Cmd+,`** (macOS) or **`Ctrl+,`** (Linux/Windows).
- **Status Bar**: Click the **⚙️ Settings** icon in the bottom-right corner of the editor.
- **Agents Page**: Click the gear icon. It opens the same modal as the editor, even with no project yet; both pages read and write the one settings file. `Cmd/Ctrl+,` works on either page, except while focus is in a terminal.
- **Command Palette**: Press `Cmd/Ctrl+Shift+P` and choose:
  - `Preferences: Open Settings (UI)`
  - `Preferences: Open Settings (JSON)`

---

## Direct JSON File Configuration

For automated machine setup, dotfile repositories, or scripting, you can directly edit the JSON configuration file:

```json
{
  "editor.fontSize": 14,
  "editor.fontFamily": "\"JetBrains Mono\", monospace",
  "editor.lineHeight": 22,
  "workbench.colorTheme": "tokyo-night",
  "diffEditor.renderSideBySide": true,
  "agent.harness": "claude",
  "agent.timeoutSeconds": 180
}
```

The file is stored at:
- `~/.px0/settings.json`
- Or `$XDG_CONFIG_HOME/px0/settings.json` (if `XDG_CONFIG_HOME` is set).
