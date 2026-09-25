# START.md

Everything needed to run the tortui build autonomously. Two commands, one prompt.

---

## 1. Setup (once)

```sh
gh repo create kdta91/tortui --private --clone && cd tortui
# add AGENT.md, TASK_TRACKER.md, CLAUDE.md, README.md, START.md
git add . && git commit -m "docs: agent contract and task tracker" && git push -u origin main

gh label create qa::pending           --color FBCA04
gh label create qa::passed            --color 0E8A16
gh label create qa::changes-requested --color D93F0B
```

**Do not enable "Require a pull request before merging" on `main`.** The loop pushes Blocked
entries straight to `main`, and that rule would block them.

Then open a workspace on this repo in Orca and start a Claude Code session in it. Set the session
to Opus at medium effort (`/model opus`, `/effort medium`) — the orchestrator coordinates; the
subagents carry their own model and effort in `.claude/agents/`.

---

## 2. The prompt

Paste this whole block as your first message. Nothing else is needed.

```
/goal Every task in TASK_TRACKER.md is `done`, or every remaining task is `blocked` with a written reason pushed to main.

You are the ORCHESTRATOR for the tortui build. You coordinate. You never write code.
This is an autonomous run: AGENT.md §10's standing authorisation applies — do not ask me to
confirm branches, pushes, PRs, labels, or merges. Stop only for AGENT.md §12.

Read AGENT.md in full now — it overrides anything in this prompt. §11 is the loop; follow it:

1. `make next` and `gh pr list --state open`. An open task PR is in flight — finish it first.
   Otherwise take the task `make next` names. None eligible → stop and say why.
2. Delegate to a fresh subagent of the type the tier names (§11 table: implementer-h for H,
   implementer for M/L). Brief: "Task <ID>, tier <T>, branch task/<ID>-<slug>." Nothing more —
   the agent definition carries the rules.
3. As soon as it reports a PR, delegate review to a fresh reviewer-h (H) or reviewer (M/L):
   "Review PR #<N> for <ID>, tier <T>." Never the agent that wrote it.
4. PASS at the current head SHA and every check `pass` → `gh pr edit <N> --add-label qa::passed
   --remove-label qa::pending`, `gh pr merge <N> --squash --delete-branch`,
   `git switch main && git pull --ff-only`. Next task.
5. FAIL → SendMessage the numbered findings to the same implementer to fix on the branch; then a
   fresh reviewer in re-review mode ("Re-review PR #<N>: findings <list>, since <sha>"). Two
   FAILs on a tier M/L task → redo it with implementer-h.
6. BLOCKED → write the Blocked entry in TASK_TRACKER.md on main, push, print
   "BLOCKED: <task> <reason>", stop.
7. Print one line per task: id, tier, verdict, PR number, minutes. Continue.

RULES:
- Never implement anything yourself. Delegate. One task at a time. One agent in the worktree at a
  time; never `git switch` while an agent is working.
- Verify only PR state, CI state, and the reviewed SHA — do not re-run the agents' gates.
- Print subagent verdict first lines verbatim. Keep your own output short.
- Progress updates are informational; do not stop to report status.
```

---

## How it works

`/goal` keeps the session running across turns — after each turn a small evaluator model reads the
transcript and decides whether the condition holds. If not, Claude starts another turn instead of
handing control back to you.

Each task is delegated to a **fresh subagent** picked by the task's tier (AGENT.md §11), which is
what gives it a clean context. That's the mechanism, not an optimisation — it's why the
orchestrator is forbidden from writing code itself. Each gate is run by exactly one party
(implementer, CI, or reviewer), so nothing is verified three times over.

A **second, separate subagent** reviews each PR. The agent that wrote the code never reviews it,
because a reviewer that remembers writing the thing isn't reviewing it.

Because the evaluator can only judge what's in the transcript, both subagent briefs require
printing real command output rather than summarising. "Tests pass" is a claim nothing can check.

---

## Watch these two things

**Sit through T-001.** It's where a missing label, a `gh` auth problem, or a branch-protection
rule surfaces. Ten minutes there beats thirty broken branches later.

**Test the QA gate once it has merged something.** Push a deliberately broken branch — delete a
test, add a blocking call inside a bubbletea `Update()`, put a fake indexer hostname in a comment —
and confirm the reviewer rejects it. A reviewer that approves everything is worse than no reviewer,
because it manufactures confidence you'll act on.

Monitor the rest from the Orca mobile app.

---

## When it stops

It's built to stop rather than guess. Expect it at:

| Task | Why |
|---|---|
| T-083 | Aggregator import — same instruction |
| any | A dependency outside the license allowlist, or a change to §2/§3/§5 |
| T-091, T-094 | Need real network and real terminals; T-094 is a manual matrix by nature |
| T-092 | Publishing a release or creating the tap/bucket repos is owner-only (AGENT.md §12) |

To resume: resolve the blocker, set the task back to `status: todo` in `TASK_TRACKER.md`, push, and
paste the prompt again. `make next` picks up from there.

---

## The files

| File | What |
|---|---|
| `AGENT.md` | The operating contract. Overrides everything, including the prompt above. |
| `TASK_TRACKER.md` | Open tasks with acceptance criteria and tiers, Backlog, DEC index, Blocked. Single source of truth for progress. |
| `docs/tracker-archive.md`, `docs/decisions.md` | Finished task blocks and full `DEC-` text, verbatim. |
| `.claude/agents/` | Implementer and reviewer definitions (model, effort, standing rules). |
| `.claude/settings.json` | Project permission allowlist so the run never stalls on a prompt. |
| `CLAUDE.md` | Router — loads automatically so any session finds the contract. |
| `README.md` | User-facing docs. T-090 verifies it against shipped behaviour. |
| `START.md` | This file. |
