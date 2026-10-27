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
