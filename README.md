# punk0

Code review and agent orchestration in your browser · Fork of [px0](https://github.com/px0-ai/px0) by Arpit Bhayani

---

punk0 keeps px0's fast viewer, search, git and pull request review, and adds what px0 leaves out: running your coding agents. Each project gets workspaces, each workspace gets terminal tabs, and the code they change sits right beside them.

- Single Go binary, no Electron
- Real terminals running your own shell and the unmodified agent CLIs
- Listens on `127.0.0.1` by default

> **Status:** early. This repository holds px0 v0.1.19 as the starting point of the fork. The orchestrator is being moved in from the original from-scratch punk0 on the [`scratch`](https://github.com/Neautrino/punk0/tree/scratch) branch.

## Install

Build from source (requires Go 1.25+ and Node.js or Bun):

```bash
git clone https://github.com/Neautrino/punk0.git
cd punk0
make web
go build -o punk0 .
install -d ~/.local/bin && install punk0 ~/.local/bin/
```

## Usage

```bash
# Open the agents page: projects, workspaces, terminal tabs
punk0

# Add a folder as a project and open its editor (hands it to a running punk0)
punk0 ~/workspace/project
punk0 main.go:42

# Review a pull request on its own (GitHub, GitLab, Gitea/Forgejo/Codeberg, Bitbucket Cloud)
punk0 https://github.com/owner/repo/pull/123

# Remote or headless server mode (terminal tabs are refused off loopback)
punk0 -host 0.0.0.0 -port 7777 ~/workspace
```

punk0 listens on <http://127.0.0.1:7777> by default. See [Agents Page](docs/features/workbench.md).

## What punk0 adds

In progress, moving over from the `scratch` branch.

**01 · Projects and workspaces** — A sidebar of projects; each workspace is the main checkout, a git worktree on its own branch, or a scratch folder.

**02 · Terminal tabs** — Shell and Claude Code tabs beside your files; Claude tabs resume their own chat when reopened.

**03 · Agents beside the code** — The editor's right side has the workspace's shell and agent tabs and, one icon over, this folder's earlier chats to resume (`Alt+T` focuses the terminal, `Cmd/Ctrl+J` hides or shows the side). Select code to get a pill: **Add to Chat** (`Alt+L`) sends it to the agent in the active tab, and **Quick Edit** (`Cmd/Ctrl+K`) has your coding harness rewrite the lines and shows a -/+ preview before anything is written. See [Editor Terminal & Quick Edit](docs/features/editor-terminal-and-quick-edit.md).

**04 · Shells that outlive the browser** — Closing or reloading the page keeps every tab running; reconnecting replays recent output.

## Everything from px0

**05 · Pull request review and git** — Review GitHub, GitLab, Gitea/Forgejo and Bitbucket Cloud PRs, comment inline, merge; stage, commit, push and pull; read unpushed commits before you push.

**06 · Search and navigation** — Fuzzy file search (`Cmd/Ctrl+P`), workspace regex search (`Cmd/Ctrl+Shift+F`), symbol outline (`Cmd/Ctrl+Shift+O`).

**07 · Code intelligence** — Go to definition, find references, call trails and hover docs through language servers.

**08 · Viewer** — ~280 languages via Chroma, 400,000-line files without lag, Markdown with Mermaid, CSV/TSV tables, images, 14 themes, Vim keybindings, in-browser editing (`Cmd/Ctrl+E`).

**09 · Remote mode** — Serve from a server or VM with `-host 0.0.0.0`. Anyone who can reach the address can browse and edit files; terminal tabs are served only while punk0 listens on loopback. Expose it only over trusted networks.

## What's different from px0

- **Removed:** Threads and Edit Inline (with its comment batching). Agent tabs in the editor's Terminal, Add to Chat (`Alt+L`) and Quick Edit (`Cmd/Ctrl+K` on a selection) replace them. PR Batch Apply stays.
- **Changed shortcuts:** `Alt+T` focuses the editor's Terminal tab; `Cmd/Ctrl+K` is Quick Edit when code is selected and Quick Open otherwise; `Cmd/Ctrl+Shift+K` opens the command palette. `Alt+E` and `Cmd/Ctrl+Shift+Enter` are gone.
- **Changed:** in a PR review's diff, line numbers carry a **Comment** button instead of the hover thread icon.

## Development

```bash
make test                          # run tests
make web && go build -o punk0 .    # build
```

punk0 tracks px0 as a second remote:

```bash
git remote add px0 https://github.com/px0-ai/px0.git
git config remote.px0.tagOpt --no-tags   # px0's version tags would clash with ours
git config remote.px0.pushurl no_push    # never push to px0
git fetch px0 && git merge px0/master
```

The guides in [docs/features](docs/features/README.md) come from px0 and still use its name.

## License and credits

[MIT License](LICENSE) © Arpit Bhayani, Subhendu Singh

punk0 is a fork of [px0](https://github.com/px0-ai/px0) by Arpit Bhayani.
