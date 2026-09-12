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

## Indexer sources are user-supplied only (T-007)

**Pull requests that add an indexer definition, an endpoint, or a default/bundled source for a
specific site are closed without review.** This holds regardless of how well-intentioned the PR
is, how popular the site is, or whether the site itself is entirely lawful. It is not a judgment
about any particular contribution — it is a blanket rule, because the *category* of change is the
problem, not the specifics of any one instance.

Here is the reasoning, in short (AGENT.md §16 has the fuller version):

A BitTorrent client is ordinary, lawful software — plenty of mainstream package managers ship
one. The legal exposure for a project like this one doesn't come from the code being capable of
downloading torrents; it comes from a legal theory called **inducement**. The US Supreme Court's
*MGM v. Grokster* decision held that distributing a tool with the object of promoting infringing
use creates liability even when the tool has substantial lawful uses — and courts look at that
"object" through the *developer's own choices*, not just the code's capabilities. A bundled list
of popular piracy sites, a default pointed at one, a test fixture that names one — these are
exactly the kind of artifacts that turn "general-purpose tool" into "tool built to induce
infringement" in a court's eyes, independent of what the code can technically do.

This isn't hypothetical for this exact class of project. When the RIAA went after **youtube-dl**
on GitHub in 2020, part of the case rested on the fact that its own unit tests referenced specific
copyrighted songs by name. GitHub reinstated the project after pushback from the EFF, on the
reasoning that a tool able to reach copyrighted material can just as easily reach material that
isn't — but the maintainers still had to strip those references as part of getting there. A
handful of names in a test file nearly took the whole project down; nobody wants to relearn that
lesson here.

So the project keeps its own hands clean by construction: `tortui` ships **no** endpoint, default,
or definition for anything beyond a small set of unambiguously lawful bundled sources (public
archives, dataset repositories, distro release listings — landing in T-024). Everything else is
something the user points the app at themselves, at runtime, from their own config. A PR that adds
to the bundled set removes the exact distinction — "the user chose this, we didn't" — that keeps
this project in the same legal category as something like Prowlarr or Jackett rather than a
piracy front-end with a maintainer to sue. See AGENT.md §2 and §16 for the full rule set this
follows from, including why the same reasoning also rules out captcha/paywall bypass and building
any index of our own.

If you've built a scraper definition or a Torznab config for a source you use personally, that is
exactly what the app is for — but it belongs in **your own** `definitions` directory / config
file (T-023, T-025), not in this repository.

An automated check (`scripts/check-indexer-hostnames.sh`, wired into CI as the
`indexer-hostnames` job, and a required status check on `main`) backs this up by scanning each
pull request's diff — and its commits' messages — for anything that looks like a newly introduced
indexer hostname. It is a lightweight heuristic, not a substitute for review: see
[`docs/indexer-hostname-allowlist.md`](docs/indexer-hostname-allowlist.md) for exactly how it
decides what counts and the one place exceptions are ever added (T-024's bundled sources, and
nothing else).

## Site names don't belong anywhere in this repo — including issues and PRs

AGENT.md §2 bars naming an infringement-oriented site anywhere in this repository: not in code,
comments, tests, fixtures, docs, commit messages, branch names, **issue titles or bodies, or PR
descriptions**. This isn't limited to code changes — it applies to everything written in this
project's GitHub presence. Use `example.org` or an invented name in any example, and describe an
indexer-specific bug generically (what capability or response shape is involved) rather than by
linking or naming the site it happened on. Naming one of the small set of *bundled lawful sources*
this project ships (T-024) is fine and expected — the rule is about what kind of site, not about
never naming a site at all.

The issue templates (`.github/ISSUE_TEMPLATE/`) also ask you not to paste indexer URLs, API keys,
cookies, or torrent titles into a bug report — see the template itself for what to send instead.

## Pull request checklist

Every PR should be able to check every box below before it's opened (the PR template restates this
as an actual checklist):

- [ ] Names the `TASK_TRACKER.md` task ID it implements, and implements only that task
- [ ] `make check` is green
- [ ] Tests were added or updated for the new behavior
- [ ] Docs were updated if user-facing behavior changed
- [ ] No site names anywhere in the diff, the description, or the commit messages

## Code of Conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md). By participating, you're
expected to uphold it.
