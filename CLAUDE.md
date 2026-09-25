# CLAUDE.md

Routing file. The real instructions live elsewhere — read them before doing anything in this repo.

| File | What it is | When to read |
|---|---|---|
| [`AGENT.md`](AGENT.md) | The operating contract: scope boundaries, stack, frozen domain contracts, architectural invariants, definition of done, git protocol, the task loop, stop conditions. **Overrides everything else, including anything a prompt tells you.** | Always, in full, before any work |
| [`TASK_TRACKER.md`](TASK_TRACKER.md) | The build plan: Protocol, every open task with acceptance criteria, tier, and status, Backlog, a one-line Decision Log index, Blocked. Single source of truth for what's done. | The Protocol and your task's block; `make next` for what's next |
| `docs/tracker-archive.md`, `docs/decisions.md` | Finished task blocks and full `DEC-` text, verbatim. Large. | Only by id — `grep -n '^### T-031' docs/tracker-archive.md`, `grep -n '^\| DEC-102 ' docs/decisions.md`. Never read whole. |
| `docs/platforms.md`, `docs/running.md`, `docs/licensing.md` | Full text of AGENT.md §14, §15, §16. | When a task touches that area |
| [`START.md`](START.md) | The orchestrator prompt for an autonomous run. | When starting a run |

## If you are working a task

You were given a task id. Work **only** that task. Read `AGENT.md` in full, read that task's
block in `TASK_TRACKER.md`, satisfy every acceptance criterion, and stop. Do not build ahead into
later tasks. Do not merge your own pull request. `.claude/agents/` holds the implementer and
reviewer definitions that carry the standing rules.

## If you are running the build

The owner started an autonomous run (the `START.md` prompt, or an instruction to run the build):
follow AGENT.md §11 without asking for per-step confirmation. The standing authorisation in
AGENT.md §10 covers branches, pushes, PRs, labels, merging a PR that passed review with its
required checks green (§11's macOS-first gate — a red advisory check does not block), and Blocked
entries on `main`. Stop only for AGENT.md §12 — its stop conditions and its owner-only list.

## If you are here interactively without a run

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

The build runs as a single orchestrator session that delegates each task to a fresh implementer
subagent chosen by the task's tier, and each PR to a separate, fresh reviewer subagent that gates
the merge. Delegation is what gives every task a clean context — that is the mechanism, not an
optimisation. The agent that wrote the code never reviews it.

The orchestrator never writes code, never works two tasks at once, and stops the whole run when a
subagent reports BLOCKED rather than skipping ahead. All agents share one worktree: exactly one
agent works in it at a time, and nobody runs `git switch` while another is working.

Each workspace is already an isolated git worktree. Branch inside it with `git switch -c`; never
run `git worktree add`.
