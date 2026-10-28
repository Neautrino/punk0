# Changelog

All notable changes to punk0. Versions follow [Semantic Versioning](https://semver.org/).

## [0.1.1] - Unreleased

### Added

- **Worktrees go where punk0 keeps them.** Claude Code and OpenCode tabs make their worktrees under `<Workspaces Folder>/workspaces/<project>/<name>`, on a new `worktree-<name>` branch, and the workspace shows up right away. Claude's own worktree removal, and subagents that never remove theirs, clean up only a clean, merged worktree. Neither agent's configuration is written to.
- **Move into punk0…** on a worktree's ⋯ moves a worktree made elsewhere into the Workspaces Folder with `git worktree move`, keeping its name, tabs, branch and history.
- Worktree rows show the folder of a worktree that sits outside the Workspaces Folder. Worktree and scratch chat menus offer **Copy path** and **Open folder**.
- Deleting a worktree whose detached HEAD holds commits no branch keeps asks first and says how many.
- **Log file** at `<state>/logs/punk0.log` for punk0, its editors and agent hooks, rotated at 5 MiB. Home folder paths and credential-looking tokens are scrubbed; terminal output, prompts and file contents are never written. A Go crash goes to `crash.log`.
- The agents page help sheet ends with **Open logs folder** and **Copy diagnostics** (versions, paths, editor state and the last 200 log lines, redacted) for bug reports.
- **Copy and paste in both terminals.** On Windows and Linux, Ctrl+C with a selection copies, Ctrl+Insert copies, Ctrl+Shift+V and Shift+Insert paste; Ctrl+V stays the program's. Copies drop the indent all copied lines share. The editor's terminal follows the `terminal.*` settings.
- **Windows installer:** `install.ps1` picks the build for your architecture, checks it against `checksums.txt`, installs to `%LOCALAPPDATA%\punk0` and adds it to your PATH.

### Changed

- **Standard folders.** Settings live in the config folder, records and logs in the state folder, downloads in the cache folder (AppData on Windows). New worktrees and scratch chats go in the Workspaces Folder (`workspace.dir`, `~/punk0` by default). An existing `~/.punk0` keeps working; `punk0 migrate` moves it when you ask.
- "Workspace is not responding" now says why and where the log is.

## [0.1.0] - 2026-10-27

First release of punk0, a fork of [px0](https://github.com/px0-ai/px0) by Arpit Bhayani.

### Added

- **Agents page:** projects, workspaces and terminal tabs. A workspace is the project folder, a git worktree on its own branch, or a scratch folder.
- **Terminal tabs** running your own shell and the unmodified agent CLIs: Claude Code, Codex, OpenCode, Antigravity and Cursor Agent. Tabs keep running when the browser closes or reloads, and replay recent output on reconnect. Claude Code tabs resume their own conversation.
- **Agent status:** the status bar and page title count agents that are working or need you, with optional desktop notifications.
- **Chats:** lists the conversations Claude Code, Codex and OpenCode keep on disk, grouped by workspace, and resumes any of them in a tab.
- **Resource panel:** CPU and memory per workspace, editor, language server and terminal.
- **Agents beside the code:** the editor's right side holds the workspace's terminals. **Quick Edit** (`Cmd/Ctrl+K`), **Add to Chat** (`Alt+L`) and **Copy Ref** (`Alt+C`) work on a selection.
- Idle terminals stop after 30 minutes (never a working or waiting Claude tab); idle editors stop after 5 minutes and restart on the next request.
- From px0: pull request review for GitHub, GitLab, Gitea/Forgejo and Bitbucket Cloud, git staging and commits, fuzzy file, text and symbol search, language servers, syntax highlighting, Markdown with Mermaid, CSV/TSV tables and in-browser editing.
- Single Go binary listening on `127.0.0.1:7777`; `install.sh` for macOS, Linux and the BSDs; self-update with `punk0 -update`.

### Removed

- px0's Threads and Edit Inline; agent tabs, Add to Chat and Quick Edit replace them.

[0.1.1]: https://github.com/Neautrino/punk0/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/Neautrino/punk0/releases/tag/v0.1.0
