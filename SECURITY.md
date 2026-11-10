# Security Policy

## Supported versions

The project is pre-1.0. Only the latest release and the latest commit on `main` get security fixes.

## Reporting a vulnerability

**Don't open a public issue, discussion or pull request for a security problem.**

Report it privately through GitHub:

1. Go to the [Security tab](https://github.com/10mTech/local-control-panel/security) of this repository.
2. Click **Report a vulnerability**.
3. Describe the issue, how to reproduce it, and the impact you expect. Redact any real tokens or personal data.

Only the maintainers can see the report. We'll acknowledge it as soon as we can, keep you updated, and
once a fix is released publish an advisory and credit you, unless you prefer to stay anonymous.

## Scope

The panel runs on a developer's own Mac and moves files, runs git and stops processes. We especially
want to hear about:

- An action touching a path outside the projects root or archive folder (symlinks, `..`, crafted names).
- Clean, archive or move-to-Trash acting on something it should skip, or deleting permanently.
- Stop signalling a process the panel didn't start, or a reused PID.
- A project name, path or config value that ends up run as shell or as extra arguments.
- Anything that makes the panel push, reset, check out, stash, clean or force in git.
- Changing or removing audit lines without the hash chain reporting it, or secrets written unmasked
  to the audit, run or debug logs.

## Out of scope

- Attackers who can already run code as your user or edit your files.
- Deleting the whole audit log. The hash chain makes edits and removals visible, not impossible.
- Bugs in herdr, git, Docker or other tools the panel calls. Report those upstream.
