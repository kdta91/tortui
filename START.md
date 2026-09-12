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

**Do not enable "Require a pull request before merging" on `main`.** The loop pushes task status
flips straight to `main`, and that rule would block the first one and stall the whole run.

Then open a workspace on this repo in Orca and start a Claude Code session in it.

---

## 2. The prompt

Paste this whole block as your first message. Nothing else is needed.

```
/goal Every task in TASK_TRACKER.md is `done`, or every remaining task is `blocked` with a written reason pushed to main.

You are the ORCHESTRATOR for the tortui build. You coordinate. You never write code.

Read AGENT.md in full now — it overrides anything in this prompt.

Then repeat until the goal is met:

1. Read TASK_TRACKER.md. Pick the lowest-numbered task with status `todo` whose every
   `depends:` entry is `done`. None eligible → stop and say why.
2. Set it to `in-progress`, commit that one change to main, push.
3. Delegate to a FRESH SUBAGENT: "Read AGENT.md in full — it overrides everything. Read the
   <TASK_ID> block in TASK_TRACKER.md. Implement exactly that task, nothing else, no building
   ahead. Tests first where behaviour is observable. `make check` until green. Branch with
   `git switch -c task/<TASK_ID>-<slug>` — do NOT run `git worktree add`, this workspace is
   already a worktree. Update the task block to `done` with notes, add a DEC- row if you made a
   judgement call. Commit per AGENT.md §10, push, `gh pr create --label qa::pending`. Do not
   merge. On an AGENT.md §12 stop condition, do not guess past it — report BLOCKED: <reason and
   what would unblock it>. Report back the real `make check` output and the PR URL."
4. Delegate review to a SECOND, SEPARATE SUBAGENT — never the one that wrote it: "You are
   reviewing code you did not write. Read AGENT.md §2, §6, §9, §16 and the <TASK_ID> block.
   Check out the PR branch. Run `make check` yourself; do not trust the author's claim. Check
   the diff against every acceptance criterion individually, pass/fail each — one you cannot
   verify is a fail. Look specifically for: an infringement-oriented site named anywhere
   including tests and commit messages; a bundled endpoint or default source outside the T-024
   allowlist; anything circumventing auth or captchas; blocking I/O in a bubbletea Update(); a
   remote-derived path used without containment validation; a runtime.GOOS switch outside
   internal/platform; telemetry; or work belonging to a later task. Pass → label qa::passed,
   remove qa::pending, `gh pr merge --squash --delete-branch`. Fail → request changes with
   specific findings, label qa::changes-requested, report FAILED. Never fix the code yourself.
   Report your per-criterion verdict."
5. QA passed → confirm `done` on main. QA failed → set back to `todo`, push, return to step 1.
6. Print one line: task id, verdict, PR number. Continue.

RULES:
- Never implement anything yourself. Always delegate. Delegation is what gives each task a clean
  context — that is the entire mechanism.
- One task at a time, never two.
- Keep your own output short.
- Subagent reports BLOCKED → write the reason into the Blocked section, commit to main, push,
  print "BLOCKED: <task> <reason>", stop the run. Do not skip ahead.
- Print subagent verdicts verbatim. An evaluator reads this transcript and cannot run commands,
  so a claim without evidence does not count.
```

---

## How it works

`/goal` keeps the session running across turns — after each turn a small evaluator model reads the
transcript and decides whether the condition holds. If not, Claude starts another turn instead of
handing control back to you.

Each task is delegated to a **fresh subagent**, which is what gives it a clean context. That's the
mechanism, not an optimisation — it's why the orchestrator is forbidden from writing code itself.

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
| T-024 | Bundled lawful sources — told to verify each source's search API against official docs and block rather than infer endpoints |
| T-083 | Aggregator import — same instruction |
| T-006 | Needs a human decision if a dependency turns out to be GPL |
| T-091, T-094 | Need real network and real terminals; T-094 is a manual matrix by nature |

To resume: resolve the blocker, set the task back to `status: todo` in `TASK_TRACKER.md`, push, and
paste the prompt again. It picks up from there.

---

## The files

| File | What |
|---|---|
| `AGENT.md` | The operating contract. Overrides everything, including the prompt above. |
| `TASK_TRACKER.md` | 49 tasks with acceptance criteria. Single source of truth for progress. |
| `CLAUDE.md` | Router — loads automatically so any session finds the contract. |
| `README.md` | User-facing docs. T-090 verifies it against shipped behaviour. |
| `START.md` | This file. |
