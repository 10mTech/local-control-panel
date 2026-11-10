# Contributing

Thanks for helping. This guide covers what a good change looks like and how it gets reviewed.

## Before you start

- **Bugs:** open an issue using the bug form.
- **Features or larger changes:** open an issue first so we can agree on the approach before you write code.
- **Security problems:** don't open a public issue. See [SECURITY.md](SECURITY.md).

## Setup

You need macOS. herdr is only needed to try the actions.

```sh
git clone https://github.com/10mTech/local-control-panel.git
cd local-control-panel
```

Build instructions will be added with the Go code.

## Making a change

1. Fork the repo and create a branch from `main`, e.g. `fix/stop-pid-check`.
2. Keep the change small and focused: one feature or fix per pull request.
3. Follow the existing style in the package you're touching.
4. Add or update tests where you can. There is no test suite yet, so tests for the safety, audit and scan code are very welcome.
5. Update the docs and [CHANGELOG.md](CHANGELOG.md) (under **Unreleased**) for anything users would notice.
6. Run the checks: `gofmt -l .` (prints nothing), `go mod tidy -diff`, `go vet ./...`, `go build ./...`, `go test ./...`.

New dependencies need a reason: say in the PR why it's needed and what you considered instead.

**Never test actions against your real projects.** Use a throwaway `HOME` and project root, and an
isolated herdr session (`herdr --session lcp-test server`).

**The safety rules are not negotiable.** The panel moves files and stops processes on people's
machines, so rules like "stop only by recorded PID", "preview then confirm", "move to the Trash, never
delete" and "non-destructive git" are fixed in code. A change that weakens one needs an issue and a
maintainer's agreement first.

## Commit messages

We use [Conventional Commits](https://www.conventionalcommits.org/): `type(scope): short imperative summary`.

- Types: `feat`, `fix`, `docs`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, `revert`.
- Scopes: `config`, `scan`, `actions`, `herdr`, `safety`, `audit`, `ui`, `docs`, `repo`.
- Mark breaking changes with `!` (e.g. `feat(audit)!: …`) and a `BREAKING CHANGE:` footer.

## AI-assisted contributions

AI tools are welcome, with these rules:

1. **You are responsible for every line.** Read and understand the whole diff before you open a PR.
2. **Disclose it.** Fill in the **AI tools** line in the PR template.
3. **Be able to explain it.** If you can't answer review questions about your change, we may close the PR.
4. **Write issues, PR descriptions and review replies in your own words.** Don't paste model output.
5. **No unattended agent PRs.** Every PR needs a human who owns it.
6. **Don't weaken tests to make them pass.**
7. **Licensing:** don't submit code copied from sources whose license isn't compatible with MIT.

If you point an AI agent at this repo, it should follow [AGENTS.md](AGENTS.md).

## Pull requests

- Fill in the PR template.
- A maintainer from `@10mTech/maintainers` reviews every PR. Expect questions; that's normal.
- PRs are squash-merged, so the PR title should be a valid Conventional Commit message.
- Never include secrets, tokens, personal paths or email addresses in code, logs or screenshots.

By taking part you agree to follow our [Code of Conduct](CODE_OF_CONDUCT.md).
