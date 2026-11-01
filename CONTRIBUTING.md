# Contributing to punk0

Thank you for your interest in improving punk0.

## AI-driven development

punk0 is built with AI coding agents. Using one for your contribution is welcome. [`AGENT.md`](AGENT.md) has the workflow and rules the agent should follow, and you are responsible for what it produces: read the diff, run the checks, and make sure you can explain every change.

## What we accept

Anyone can contribute. Pull requests are welcome for:

- **Bug fixes**: navigation, rendering, terminals, git, review, anything that misbehaves.
- **Performance work**: faster startup, search, indexing or rendering, lower memory use. Include before/after numbers (`make bench` or `make benchmark`).
- **Features** that fit punk0: a single Go binary, no runtime dependencies, zero config, small memory footprint.
- **Docs**: corrections and missing explanations.

We decline bloat: new dependencies, options nobody asked for, abstractions without a second user, or features that make punk0 heavier without a clear payoff. If you're unsure, open an issue first.

## Suggestions and bug reports

Not writing code? Ideas and reports are just as useful.

- Search [existing issues](https://github.com/Neautrino/punk0/issues) first.
- Bugs: use the [bug report template](.github/ISSUE_TEMPLATE/bug_report.md). Include OS, browser, workspace size, steps to reproduce, and expected vs. actual behavior.
- Features: use the [feature request template](.github/ISSUE_TEMPLATE/feature_request.md). Say what problem it solves and who it helps.

## Set up

You need Go 1.25+ and [Bun](https://bun.sh).

```sh
git clone https://github.com/Neautrino/punk0.git
cd punk0
make build      # bundles web assets and builds ./punk0
```

## Run your build

```sh
make dev                          # builds ./punk0 as a dev build
./punk0 ~/code/some-project       # runs beside your installed punk0
```

A dev build runs beside an installed punk0 without touching it: it keeps its settings, projects, chats, logs, worktrees and scratch chats in `.punk0-dev/` in your checkout (gitignored), listens on <http://127.0.0.1:7800>, says `dev` beside its version, and never updates itself. Delete `.punk0-dev/` to start over. `make build` (and every release) makes a normal binary again, which uses the installed punk0's folders. Any build runs as a dev punk0 with `-dev`, and `-dev=/full/path` keeps the folder somewhere else; `go run . -dev` needs a full path, since `go run` builds into a temporary folder. The agents' own conversations (`~/.claude` and the like) are shared with the installed punk0, so don't resume one chat in both at once. A scratch chat made in dev sits inside your punk0 checkout, so git (and an agent running git) there sees punk0's repository.

web/ is embedded in the binary: after changing HTML, CSS or JS, stop punk0 (Ctrl-C), run `make dev` and start it again.

## Before you open a pull request

```sh
make check      # web checks, asset bundle, fast Go tests
make test       # full Go test suite
```

- New Go packages need a `*_test.go` and at least one `Benchmark` function ([testing docs](docs/internals/testing-and-benchmarks.md)).
- User-facing features need a tip in `DEFAULT_TIPS` (`web/src/tips-util.js`) and an update to the matching page under `docs/features/`.
- Keep each pull request to one logical change. Describe what changed and how you checked it; add screenshots for anything visible.

## License

punk0 is licensed under the [MIT License](LICENSE). By opening a pull request you agree that your contribution is licensed under the same terms.
