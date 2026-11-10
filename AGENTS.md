# AGENTS.md

Instructions for AI coding agents (Claude Code, Codex, Cursor, Copilot, Gemini CLI, …) working in this repository. Humans should read [CONTRIBUTING.md](CONTRIBUTING.md); this file is the short, strict version for agents.

## Project

`lcp`: a keyboard-driven terminal panel for every project in one folder, with one-key actions through herdr and a hash-chained audit log. Go, Bubble Tea, macOS only. See [README.md](README.md). Status: early; expect breaking changes before 1.0.

## Boundaries

- `cmd/lcp` is the program; everything else is in `internal/` (`config`, `scan`, `actions`, `herdr`, `safety`, `audit`, `debuglog`, `ui`).
- `scan` only reads the system. Only `actions` change it.
- Only `internal/herdr` calls herdr. Only `internal/audit` writes the audit log.
- `scan` and `actions` contain no UI code.
- Every path an action touches must pass the `safety` checks.

## Safety rules (never weaken)

If a task seems to need changing one of these, stop and ask.

- Stop only by recorded PID, checking its start time before every signal.
- Clean only from the allowlist. Move to the Trash or rename; never delete permanently.
- Preview, then confirm. Declines are audited.
- The panel's git actions: only fetch, `pull --ff-only`, init, status and log. Never push, reset, checkout, stash, clean or force.
- Commands run as argument lists, not shell strings (except the run wrapper, with strict quoting).
- Never write secrets or environment variable values to any log.

## Git rules (strict)

- **Never run `git commit` without explicit permission from the user.**
- **Never run `git push` (including `--force`, `--force-with-lease`, or pushing tags) without explicit permission from the user.**
- Permission covers one action only. Approval for one commit or push does not cover later ones; ask again each time.
- Before asking to commit, show the changed files (`git status`), a short summary, and the proposed commit message.
- Before asking to push, state the branch and remote.
- Do not amend, rebase, reset, squash, cherry-pick, merge, delete branches, or rewrite history without explicit permission.
- Do not create, delete, or switch branches without telling the user first.
- Never modify git config (user, email, hooks, remotes). Never skip hooks (`--no-verify`) or bypass signing.
- `git add` only when preparing a commit the user has approved.
- Read-only git commands (`status`, `diff`, `log`, `show`, `branch --list`) are always fine.

## Commit messages

[Conventional Commits](https://www.conventionalcommits.org/): `type(scope): summary`, imperative, ≤ 72 characters. Types and scopes are in [CONTRIBUTING.md](CONTRIBUTING.md).

## Working rules

- Ask before deleting files, running destructive commands, or making large-scale changes. If something is unclear, ask instead of guessing.
- Keep changes focused and small. Follow the existing style of the package you're in, and look for an existing helper before writing a new one.
- **Don't add dependencies without asking.**
- **Don't weaken, skip or delete tests to make them pass.** If a test looks wrong, say so and ask.
- **Never test actions against the user's real projects.** Use a throwaway `HOME` and project root, and an isolated herdr session.
- Never commit or expose secrets, tokens, `.env` files, audit or run logs, or personal data (names, emails, home paths).
- Update docs and `CHANGELOG.md` (under **Unreleased**) for user-visible changes.

## Definition of done

1. The change does what was asked, and nothing else.
2. `gofmt -l .` prints nothing, and `go mod tidy -diff`, `go vet ./...`, `go build ./...` and `go test ./...` pass. Report results honestly, including failures.
3. Docs and changelog updated where relevant.
4. You've summarised what changed and what still needs the user's attention.
