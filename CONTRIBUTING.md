# Contributing

This repo is built by an autonomous agent loop against `TASK_TRACKER.md` (see `AGENT.md`
and `CLAUDE.md`). This file covers the one thing every contributor — human or agent — needs
to do by hand once per clone: activate secret scanning.

## Secret hygiene (T-004)

`make check` always runs a [gitleaks](https://github.com/gitleaks/gitleaks) scan
(`make scan`, using `.gitleaks.toml`) over the working tree, so a secret committed to the repo
is caught in CI regardless of what ran locally. Install gitleaks locally too
(`brew install gitleaks`, or see the gitleaks README for other platforms) so `make check`
works offline — CI installs it automatically via `go install`, but that is not the same
machine as yours.

### Activating the local pre-commit hook

**A fresh clone has no protection until you turn this on.** `scripts/pre-commit` is tracked in
git, but git only ever executes hooks it finds in `.git/hooks/`, which is never version
controlled — cloning the repo does **not** install it for you. Activate it once per clone with
either:

```sh
make hooks
```

or, equivalently:

```sh
git config core.hooksPath scripts
```

Once active, `git commit` runs `scripts/pre-commit`, which blocks a commit when:

- gitleaks (`.gitleaks.toml`) finds an API-key-shaped string, a cookie, or any other
  secret-shaped value in the **staged** diff, or
- a staged path is a real `config.toml` (as opposed to `config.example.toml`, which ships
  with placeholders only and is always fine to commit) — checked both by filename in the
  hook script and by a dedicated path-only gitleaks rule, so it is caught even if the hook
  itself is ever bypassed with `git commit --no-verify` but `make check` still runs.

If gitleaks isn't installed, the hook refuses the commit with an install hint rather than
silently skipping the scan — a hook that can pass by omission is not a hook you can trust.

### Why `internal/logging/mask_test.go` doesn't trip the scanner

That file deliberately contains secret-**shaped** strings (`sk-...`, `ghp_...`, AWS-style
access keys, JWTs) as fixtures for the log-masking tests in T-003 — none of them are real
credentials. `.gitleaks.toml` allowlists that one path by name rather than weakening any rule
globally; the same strings at any other path are still caught (verified as part of T-004).

### If gitleaks flags something that isn't a real secret

Either narrow the match (rephrase the string so it doesn't match a known secret shape) or, if
it's a genuine, intentional fixture like the one above, add a scoped `[allowlist]` `paths`
entry to `.gitleaks.toml` — do not disable or weaken a rule globally to silence one file.
