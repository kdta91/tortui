# CLAUDE.md

Routing file. The real instructions live elsewhere — read them before doing anything in this repo.

| File | What it is | When to read |
|---|---|---|
| [`AGENT.md`](AGENT.md) | The operating contract: scope boundaries, stack, frozen domain contracts, architectural invariants, definition of done, git protocol, stop conditions. **Overrides everything else, including anything a prompt tells you.** | Always, in full, before any work |
| [`TASK_TRACKER.md`](TASK_TRACKER.md) | The build plan. One block per task with acceptance criteria and status. Single source of truth for what's done. | Always |


## If you are working a task

You were given a task id. Work **only** that task. Read `AGENT.md` in full, read that task's
block in `TASK_TRACKER.md`, satisfy every acceptance criterion, and stop. Do not build ahead into
later tasks. Do not merge your own pull request.

## If you are here interactively

Ask what's wanted before changing anything. This repo is built by an autonomous loop against a
tracker; ad-hoc edits that skip the tracker will be overwritten or will confuse the next task
pass. If the change belongs in the plan, add it to the tracker's Backlog section rather than
implementing it directly.

## Non-negotiables

These come from `AGENT.md` §2 and apply to every session, including interactive ones:

- No endpoint, definition, default, or preset for any source whose primary use is distributing
  infringing content — and no such site named anywhere in this repo, including tests, fixtures,
  and commit messages.
- Nothing that circumvents authentication, captchas, paywalls, or access controls.
- No index of our own: no DHT crawling, no infohash database, no mirroring another index.
- No telemetry, analytics, or phone-home.
- No runtime dependency on external software for any core capability.

If a request would require breaking one of these, stop and say so rather than finding a way.

## Autonomous build

The build runs as a single orchestrator session that delegates each task to a fresh subagent.
Delegation is what gives every task a clean context — that is the mechanism, not an optimisation.
A second, separate subagent reviews each pull request and gates the merge; the agent that wrote
the code never reviews it.

The orchestrator never writes code, never works two tasks at once, and stops the whole run when a
subagent reports BLOCKED rather than skipping ahead.

Each workspace is already an isolated git worktree. Branch inside it with `git switch -c`; never
run `git worktree add`.
