# Editing with Coding Agents

px0 is a reader first. Small direct edits are possible (see [File Editing](file-editing.md)), and longer agent sessions run in the [editor's Terminal tab](editor-terminal-and-quick-edit.md). For one-shot jobs, px0 runs the CLI coding harness already installed on your machine and uses its answer: a rewrite of selected lines, a commit message, a rebase, or a batch of review comments applied to the code.

---

## Overview & Core Purpose

px0 does not ship a model or an API key. It finds the coding harnesses you already have (Claude Code, Cursor Agent, Antigravity, OpenCode, Codex), lets you pick one, and runs it headless when a feature needs it. The same harness and model serve every feature below, so choosing them once is enough.

| Feature | What the harness does | Where it runs | Guide |
| :--- | :--- | :--- | :--- |
| **Quick Edit** | Rewrites the selected lines from your instruction; you review a -/+ preview before anything is written | An empty temporary folder, 30 s limit | [Editor Terminal & Quick Edit](editor-terminal-and-quick-edit.md) |
| **Commit message** | Writes a message for the staged diff (the git panel's **✦**) | The workspace | [Git Integration](git-integration.md) |
| **Commit plan** | With nothing staged, plans one or more commits for you to review and edit (**✦**) | The workspace | [Git Integration](git-integration.md) |
| **Rebase / Merge with AI** | Replays or merges diverged history, resolving conflicts | The workspace | [Git Integration](git-integration.md) |
| **PR merge message** | Rewrites the commit message for a squash merge | The workspace | [Pull Request Review](github-pr-review.md) |
| **PR Batch Apply** | Applies every located draft review comment to the code in one run | The workspace | [Pull Request Review](github-pr-review.md) |

---

## Supported Harnesses & Models

px0 detects these harnesses on `PATH` and in the directories they usually install into (`~/.local/bin`, npm's global prefix, and so on):

| Harness | Command run (headless) |
| :--- | :--- |
| **Claude Code** (`claude`) | `claude --permission-mode acceptEdits [--model M] -p {prompt}` |
| **Cursor Agent** (`cursor-agent`) | `cursor-agent --force [--model M] -p {prompt}` |
| **Antigravity** (`agy`) | `agy --dangerously-skip-permissions --mode accept-edits [--model M] -p {prompt}` |
| **OpenCode** (`opencode`) | `opencode run [-m M] {prompt}` |
| **OpenAI Codex** (`codex`) | `codex exec --skip-git-repo-check --sandbox workspace-write [-m M] {prompt}` |

The model list for each harness comes from the harness itself. With no model picked, the harness uses its own default. Any other tool works through a command template containing `{prompt}` (see Configuration below).

---

## Choosing a Harness

Detection alone never runs anything: nothing is sent to a harness until you pick one. The harness and model selects appear where an AI action is offered, for example in the git panel's notice card for Rebase / Merge with AI and in the PR merge dialog; Settings (`Cmd/Ctrl+,`) has the harness too, as **Coding Harness** (`agent.harness`). Changing it in one place changes it everywhere, and the choice is saved in `~/.px0/settings.json` (or `$XDG_CONFIG_HOME/px0/settings.json`), never in your repository.

Until a harness is picked, the selects read **pick a harness** and AI actions are refused with an error.

---

## How a Workspace Run Works

Commit messages and plans, Rebase / Merge with AI, the PR merge message and PR Batch Apply run the harness in the workspace:

1. px0 composes the prompt (the staged diff, the conflicting commits, or each review comment with the code it points at) and starts the harness with the workspace as its working directory and stdin closed.
2. Progress streams to the terminal where px0 was launched, prefixed per harness.
3. When the harness exits, px0 compares `git status` (with each modified file's size and modification time) before and after the run to find every file it touched, including ones it was never pointed at.
4. Changed files reload in place: open tabs keep their view (source stays source, diff stays diff), scroll position and cursor; git badges and gutters update.

Quick Edit is different: it runs outside the workspace and only returns text. px0 writes the replacement itself, and only when you accept it. See [Quick Edit](editor-terminal-and-quick-edit.md#quick-edit).

---

## Safety

- **Local only**: AI actions are accepted only when px0 is reached through `localhost` or an IP address. Requests through a hostname, such as a public tunnel or reverse proxy, are refused.
- **No overlapping batch runs**: a PR Batch Apply whose comments cover lines that a running batch is still changing is refused.
- **Errors are shown, not hidden**: a failed run reports the harness's own error (an expired login, a bad flag) with its output.
- **Time limit**: a workspace run is stopped after 10 minutes; Quick Edit after 30 seconds. Whatever a stopped workspace run already wrote stays.

---

## Configuration Options

- **`agent.harness`** (stored as `agent` in `settings.json`): the chosen harness, a preset name or a command template. `models` holds the model picked for each harness. The selects write both.
- **`git.commitMessageInstruction`**: extra instructions for commit messages (Settings, `Cmd/Ctrl+,`).
- **CLI flags**:
  - `px0 -agent <name>`: pin a harness for this run; the selects show it as fixed. A command template works too: `px0 -agent "claude -p --permission-mode acceptEdits {prompt}"`.
  - `px0 -no-agent`: turn off every harness-backed feature.

---

## Technical Architecture Deep Dive

For harness discovery, the invocation contract, change detection, the reload path and the HTTP endpoints, see [Harness Editing & Agent Dispatch Internals](../internals/agent-editing.md).
