# punk0

---

punk0 is a minimal agent orchestrator that runs in your browser. Each project gets its own workspaces: the main checkout, git worktrees, or scratch folders. Each workspace gets tabs running your shell or Claude Code, with a fast code viewer beside the terminal.

As coding agents do more of the typing, the work shifts to running several of them side by side, keeping them out of each other's files, and checking what they changed. punk0 gives every agent its own worktree and terminal, keeps those terminals alive when the browser closes, and lets you read the code without leaving the page.

- Single Go binary with the UI embedded (no Electron, no Node, no build step)
- ~15 MB server RAM with one shell; ~57 MB with four shells and four indexed workspaces
- Real PTYs running your own `$SHELL`, rendered with xterm.js
- Listens on `127.0.0.1` only

## Features

- Projects, workspaces and tabs: A left sidebar lists projects, and under each one its workspaces, each with its own tabs. Projects are any folder; workspaces are the project folder itself, a git worktree, or a scratch folder.
- Worktree workspaces: Each worktree gets its own branch under `~/.punk0/worktrees/<project>/<name>`, so parallel agents never touch each other's files. Deleting one with uncommitted changes asks a second time; its branch is always kept.
- Claude tabs: `+ Claude` starts Claude Code with its own session id and resumes that same chat whenever the tab is reopened, even after a restart. `Shift+Enter` inserts a newline.
- Shell tabs: `+ Shell` opens your `$SHELL` in the workspace folder; double-click a tab to rename it.
- Shells that outlive the browser: Closing or reloading the page keeps every tab running; reconnecting replays the recent output.
- Scratch chats: Claude chats outside any project, each in its own folder under `~/.punk0/scratch/<name>`.
- Code viewer: File tree that honours `.gitignore` and shows git status, syntax highlighting via Chroma, and virtual rendering that mounts only the visible rows of large files.

## Installation

Build from source (requires Go 1.27+, Linux or macOS; on Windows use WSL2):

```bash
git clone https://github.com/Neautrino/punk0.git
cd punk0
go build -o punk0 .
install -d ~/.local/bin && install punk0 ~/.local/bin/
```

Claude tabs need `claude` on the `PATH` your shell rc sets, since they start through `$SHELL -i -c`.

## Usage

```bash
# Add the current directory as a project and open punk0
punk0 .

# Add a specific directory as a project
punk0 ~/workspace/project

# Later runs: projects are remembered
punk0

# Pick another port (0 picks a free one)
punk0 -port 7777
```

Open <http://127.0.0.1:7070>. `Ctrl+C` stops the server and hangs up every tab's shell; Claude tabs resume their chat the next time they are opened.

Projects, workspaces and tabs are saved in `~/.punk0/records.json`, worktrees in
`~/.punk0/worktrees/<project>/<name>`, and scratch chats in `~/.punk0/scratch/<name>`.
Removing a project from punk0 deletes no files, and its worktrees come back if you add it again.

punk0 binds to loopback, rejects requests whose `Host` is not loopback (DNS rebinding),
rejects cross-origin requests that change state, and serves the UI under a strict
Content Security Policy. Anyone who can reach the port can run commands as you, so keep it on `127.0.0.1`.

## Development

```bash
git clone https://github.com/Neautrino/punk0.git
cd punk0

# Run tests
go test -race ./...

# Run from source
go run . ~/workspace/project

# Build binary
go build -o punk0 .
```

The UI is plain ES modules in `web/`, embedded into the binary with `go:embed`.

> **Memory accounting**: the figures above are the punk0 server's resident memory (RSS) on Linux: ~15 MB with one shell, ~57 MB with four shells running and sixteen files highlighted across four workspaces. Each shell process adds its own ~12 MB (zsh), and agents such as Claude Code add theirs; the browser tab is separate.

## License

[MIT License](LICENSE) © Subhendu Singh

punk0 includes code from [px0](https://github.com/px0-ai/px0) (MIT License © Arpit Bhayani); see [NOTICE](NOTICE).
