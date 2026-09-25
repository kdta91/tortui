<!--
Before you fill this in: pull requests adding an indexer definition, endpoint,
or default source for a specific site are closed without review (see
CONTRIBUTING.md's "Indexer sources are user-supplied only" section for why).
Also, no site names anywhere in this description, in commit messages, or in
review discussion (AGENT.md §2) -- describe indexer-adjacent behavior
generically instead.
-->

## Task

<!-- Task ID and tier from TASK_TRACKER.md, e.g. T-042 (tier M). -->

## What changed

<!-- Summary of the change and why. -->

## How it was verified

<!-- Commands you ran and what they showed -- not just "it works." -->

## Deferred / follow-up

<!-- Anything intentionally left out of scope, and where it's tracked (a TASK_TRACKER.md task ID or a new T-9NN backlog entry). -->

## Checklist

- [ ] Task ID named above, and this PR does only that task (no building ahead into a later one)
- [ ] `make check` is green
- [ ] Tests added or updated for the new behavior
- [ ] Docs updated if user-facing behavior changed (`README.md`, `config.example.toml`, `docs/`)
- [ ] Tracker update per the `TASK_TRACKER.md` Protocol: block `done` with notes (≤ 10 lines) and
      moved to `docs/tracker-archive.md`, any `DEC-` in `docs/decisions.md` + index row,
      one `docs/session-log.md` line
- [ ] No site names anywhere in this diff, this description, or the commit messages — indexer
      definitions/endpoints/default sources are user-supplied only (AGENT.md §2)
