# local-control-panel

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A keyboard-driven terminal panel for every project in one folder. One screen shows each project's
stack, git state, what is running, disk use and how recently it was touched. One keystroke opens it
in herdr, starts or stops its dev server, pulls it, cleans its build folders or archives it. Every
action goes into an append-only, hash-chained audit log that you can read inside the panel.

```bash
lcp          # run from any terminal; actions need herdr
```

> **Status: pre-release.** No code is published yet. macOS only. Expect rough edges and breaking changes before 1.0.

## Features

- **One screen for all projects:** stack, git state (dirty, ahead, behind, no git), listening ports,
  Docker containers, disk and build-folder size, and age.
- **One-key actions:** open in herdr, editor, Claude Code, run and stop dev servers, fetch all,
  pull, init, clean to the Trash, archive and restore.
- **Safe by design:** previews and confirmations, Trash instead of delete, fast-forward-only git,
  and stop only by recorded PID. The rules are fixed in code.
- **Audited:** every action is logged before and after in an append-only JSONL file with a sha256
  hash chain, with secrets masked.

## Install

Coming with the first release.

## Documentation

Full documentation (screens, keys, safety rules, audit log, configuration) is coming soon on the
project website.

## Contributing

Contributions are welcome. Read [CONTRIBUTING.md](CONTRIBUTING.md) first. If you use AI tools, also
read [AGENTS.md](AGENTS.md).

Please follow the [Code of Conduct](CODE_OF_CONDUCT.md). Report security issues privately as
described in [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE) © 10mTech
