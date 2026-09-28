# Terminal compatibility matrix (T-094)

T-094's acceptance criteria require a **verified pass by a human at a real terminal** for each
required entry (AGENT.md §12: "manual steps needing a human at a real terminal (the T-094
matrix)" is on the owner-only list, never run autonomously). This document is that checklist,
plus the unattended evidence an agent *can* record without a human present — the two are kept in
separate sections so a reader never has to guess which rows were actually watched by a person.

**Do not flip a row to "verified" without a human confirming it at that terminal.** An agent
filling in a pass here from inference, a screenshot it can't see, or "should work" is exactly the
failure mode this document exists to prevent.

## How to run each pass

From `docs/running.md` / AGENT.md §15, after `make build`:

```sh
./bin/tortui --demo                     # Terminal.app  ← the floor
./bin/tortui --demo                     # iTerm2 or Ghostty
tmux new-session ./bin/tortui --demo    # inside tmux
NO_COLOR=1 ./bin/tortui --demo          # monochrome
TERM=xterm ./bin/tortui --demo          # 16-colour
./bin/tortui --ascii --demo             # glyph fallback
printf '' | ./bin/tortui                # non-TTY → clean refusal, exit 1
```

For each terminal, also:
- Resize the window to exactly 80×24, then to ~60 columns, while `--demo` is running. Columns
  must drop in the documented order (AGENT.md §7: Source → Age → Trust) with no garbling.
- Confirm shell-agnosticism once (not per terminal) with:
  ```sh
  zsh  -lc './bin/tortui --version'
  bash -lc './bin/tortui --version'
  sh   -c  './bin/tortui --version'
  pwsh -Command './bin/tortui.exe --version'   # or Windows PowerShell on the Windows machine
  ```

## Owner checklist — fill in after a real pass, one row per terminal

Mark each cell `PASS` / `FAIL` (with a one-line note) only after watching it happen. Leave a cell
blank until it has actually been run. Windows Terminal and legacy `conhost` are both required
because Windows is tier 1 (AGENT.md §14); `conhost` must show the clear upgrade message rather
than a garbled render if it can't support the TUI.

| Terminal | Column alignment | Colour degradation | Clean exit | Terminal restore |
|---|---|---|---|---|
| Terminal.app (macOS) — **pass/fail floor** | PASS (owner, macOS, 2026-09-28) | PASS (owner, macOS, 2026-09-28) | PASS (owner, macOS, 2026-09-28) | PASS (owner, macOS, 2026-09-28) |
| iTerm2 or Ghostty (macOS) | PASS (owner, macOS, 2026-09-28) | PASS (owner, macOS, 2026-09-28) | PASS (owner, macOS, 2026-09-28) | PASS (owner, macOS, 2026-09-28) |
| Windows Terminal | DEFERRED (DEC-132) — no device | DEFERRED (DEC-132) — no device | DEFERRED (DEC-132) — no device | DEFERRED (DEC-132) — no device |
| conhost (legacy, Windows) — upgrade message, not garbled render | DEFERRED (DEC-132) — no device | DEFERRED (DEC-132) — no device | DEFERRED (DEC-132) — no device | DEFERRED (DEC-132) — no device |
| Linux terminal emulator (e.g. GNOME Terminal, Konsole, Alacritty) | DEFERRED (DEC-132) — no device | DEFERRED (DEC-132) — no device | DEFERRED (DEC-132) — no device | DEFERRED (DEC-132) — no device |
| tmux (inside any of the above) | PASS (owner, macOS, 2026-09-28) | PASS (owner, macOS, 2026-09-28) | PASS (owner, macOS, 2026-09-28) | PASS (owner, macOS, 2026-09-28) |

Additional required conditions — same four columns, run in whichever terminal is convenient
(note which one):

| Condition | Column alignment | Colour degradation | Clean exit | Terminal restore |
|---|---|---|---|---|
| `NO_COLOR=1 ./bin/tortui --demo` | PASS (owner, macOS, 2026-09-28) | PASS (owner, macOS, 2026-09-28) | PASS (owner, macOS, 2026-09-28) | PASS (owner, macOS, 2026-09-28) |
| `TERM=xterm ./bin/tortui --demo` | PASS (owner, macOS, 2026-09-28) | PASS (owner, macOS, 2026-09-28) | PASS (owner, macOS, 2026-09-28) | PASS (owner, macOS, 2026-09-28) |
| `./bin/tortui --ascii --demo` | PASS (owner, macOS, 2026-09-28) | PASS (owner, macOS, 2026-09-28) | PASS (owner, macOS, 2026-09-28) | PASS (owner, macOS, 2026-09-28) |
| `printf '' \| ./bin/tortui` (non-TTY refusal) | PASS (owner, macOS, 2026-09-28) | PASS (owner, macOS, 2026-09-28) | PASS (owner, macOS, 2026-09-28) | PASS (owner, macOS, 2026-09-28) |

Resize check (run in at least the Terminal.app pass and one other):

| Terminal | 80×24 holds, no garbling | ~60 columns drops Source→Age→Trust in order, no garbling |
|---|---|---|
| Terminal.app (macOS) | PASS (owner, macOS, 2026-09-28) | PASS (owner, macOS, 2026-09-28) |
| iTerm2 or Ghostty (macOS) | PASS (owner, macOS, 2026-09-28) | PASS (owner, macOS, 2026-09-28) |

Shell-agnosticism (confirmed once, note which terminal/OS each ran on):

| Shell | `./bin/tortui --version` runs cleanly |
|---|---|
| zsh | PASS (owner, macOS, 2026-09-28) |
| bash | PASS (owner, macOS, 2026-09-28) |
| sh | PASS (owner, macOS, 2026-09-28) |
| PowerShell (Windows) | DEFERRED (DEC-132) — no device |

**Any failure becomes a `T-9NN` Backlog item or blocks the release — record which, and why, in
this file next to the failing row, add the Backlog entry to `TASK_TRACKER.md` (or note the
release-blocking decision), and add the bug-vs-release-blocker call itself as a `DEC-` entry in
`docs/decisions.md` (not only as a note in this file — a note here is not a substitute for the
decision log). Terminal.app is the pass/fail floor (AGENT.md §7, §12, DEC-007): a defect seen
only in a modern emulator (iTerm2, Ghostty, Windows Terminal, a Linux emulator) is a bug, routed
to Backlog; a defect seen in Terminal.app blocks the release, full stop, regardless of how it
behaves elsewhere.**

Once every required row above is filled in with a real `PASS`, a documented non-blocking `FAIL`
routed to Backlog, or an owner-decided `DEFERRED` citing its `DEC-` entry, flip
`TASK_TRACKER.md`'s T-094 block to `status: done` per the Protocol, moving it into
`docs/tracker-archive.md`. As of 2026-09-28 every macOS row is `PASS` (owner pass) and the
Windows/Linux/PowerShell rows are `DEFERRED (DEC-132)` — v1.0 manual verification is macOS-only
because the owner has no Windows or Linux device; that work is Backlog `T-9004`.

---

## What is already verified unattended (T-094 implementer pass, 2026-09-28)

An agent cannot watch a real terminal, so it cannot fill in the table above. What it *can* prove
is the underlying behaviour each row exercises, through non-interactive commands and existing
`teatest`/golden coverage. This section records exactly that, with quoted output, so the owner's
manual pass is confirming rendering fidelity on real hardware, not re-discovering logic bugs.

### Non-TTY refusal — clean exit, no garbling

```
$ printf '' | ./bin/tortui; echo "exit=$?"
tortui: refusing to start — stdout is not an interactive terminal (TERM=dumb or output is redirected/piped)
exit=1
```

```
$ printf '' | TERM=dumb ./bin/tortui; echo "exit=$?"
tortui: refusing to start — stdout is not an interactive terminal (TERM=dumb or output is redirected/piped)
exit=1
```

Backed by `cmd/tortui`'s `TestRunDefaultRefusesNonInteractiveOutput` and
`TestRunDemoRefusesNonInteractiveOutput`, and `internal/tui/theme`'s
`TestDetectInteractiveFalseOnTermDumb` / `TestDetectInteractiveFalseOnNonTTYStdout`:

```
$ go test ./cmd/tortui/... -run 'TestRunDemoRefusesNonInteractiveOutput|TestRunDefaultRefusesNonInteractiveOutput|TestRunDoctorTermDumbExitsNonZero' -v
=== RUN   TestRunDemoRefusesNonInteractiveOutput
--- PASS: TestRunDemoRefusesNonInteractiveOutput (0.00s)
=== RUN   TestRunDoctorTermDumbExitsNonZero
--- PASS: TestRunDoctorTermDumbExitsNonZero (0.01s)
=== RUN   TestRunDefaultRefusesNonInteractiveOutput
--- PASS: TestRunDefaultRefusesNonInteractiveOutput (0.00s)
PASS
ok  	github.com/kdta91/tortui/cmd/tortui	0.533s
```

### `NO_COLOR=1` — colour degradation

`--demo` piped to a non-TTY still refuses to start (the refusal check runs before demo mode, by
design — a piped terminal can't render a TUI at all, coloured or not):

```
$ NO_COLOR=1 ./bin/tortui --demo < /dev/null
tortui: refusing to start — stdout is not an interactive terminal (TERM=dumb or output is redirected/piped)
```

The actual zero-escape-code guarantee is proved at the render layer, where it can run against a
non-TTY buffer directly: `internal/tui/theme`'s `TestDetectNoColorForcesColorNone`,
`TestDetectNoColorRendersZeroEscapeCodes`, and the golden `TestGoldenNoColorZeroEscapeCodes`
(fails if any semantic style emits an ANSI escape sequence under `NO_COLOR`):

```
$ go test ./internal/tui/theme/... -run 'TestDetectNoColorForcesColorNone|TestDetectNoColorRendersZeroEscapeCodes|TestGoldenNoColorZeroEscapeCodes' -v
=== RUN   TestDetectNoColorForcesColorNone
--- PASS: TestDetectNoColorForcesColorNone (0.00s)
=== RUN   TestDetectNoColorRendersZeroEscapeCodes
--- PASS: TestDetectNoColorRendersZeroEscapeCodes (0.00s)
=== RUN   TestGoldenNoColorZeroEscapeCodes
--- PASS: TestGoldenNoColorZeroEscapeCodes (0.00s)
PASS
ok  	github.com/kdta91/tortui/internal/tui/theme	0.786s
```

### `TERM=xterm` (16-colour) and `--ascii` (glyph fallback)

Both are colour/glyph *selection* logic, not rendering a live human eyeballs — the selection
itself is unit-tested (`TestDetectUnicodeCapability`'s `force ASCII overrides a UTF-8 locale`
case, plus the existing colour-profile detection tests), while what those choices actually look
like on a given terminal is exactly the part this doc defers to the owner's manual pass:

```
$ go test ./internal/tui/theme/... -run TestDetectUnicodeCapability -v
=== RUN   TestDetectUnicodeCapability
=== RUN   TestDetectUnicodeCapability/UTF-8_locale
=== RUN   TestDetectUnicodeCapability/non-UTF-8_locale
=== RUN   TestDetectUnicodeCapability/LC_ALL_wins_over_LANG
=== RUN   TestDetectUnicodeCapability/no_locale_vars_set_defaults_to_Unicode
=== RUN   TestDetectUnicodeCapability/force_ASCII_overrides_a_UTF-8_locale
--- PASS: TestDetectUnicodeCapability (0.01s)
    --- PASS: TestDetectUnicodeCapability/UTF-8_locale (0.00s)
    --- PASS: TestDetectUnicodeCapability/non-UTF-8_locale (0.00s)
    --- PASS: TestDetectUnicodeCapability/LC_ALL_wins_over_LANG (0.00s)
    --- PASS: TestDetectUnicodeCapability/no_locale_vars_set_defaults_to_Unicode (0.00s)
    --- PASS: TestDetectUnicodeCapability/force_ASCII_overrides_a_UTF-8_locale (0.00s)
PASS
ok  	github.com/kdta91/tortui/internal/tui/theme	0.01s
```

### Column alignment and the resize/drop-order check

`internal/tui/components`'s `TestTableGolden80x24`, `TestTableGolden120x40`, and
`TestTableGolden60x20` are the exact resize check from AGENT.md §7 and this task's acceptance
criteria (80×24 and ~60 columns), run against a test-side copy of the results column set
(`internal/tui/components/table_test.go`'s `resultColumns()`) that currently matches
`internal/tui/results.go`'s own column builder, asserting the documented drop order
(`Source → Age → Trust`) holds with every remaining column still at its declared width — the
golden diff *is* the garbling check:

```
$ go test ./internal/tui/components/... -run 'TestTableGolden80x24|TestTableGolden120x40|TestTableGolden60x20' -v
=== RUN   TestTableGolden80x24
--- PASS: TestTableGolden80x24 (0.00s)
=== RUN   TestTableGolden120x40
--- PASS: TestTableGolden120x40 (0.00s)
=== RUN   TestTableGolden60x20
--- PASS: TestTableGolden60x20 (0.00s)
PASS
ok  	github.com/kdta91/tortui/internal/tui/components	0.376s
```

### Shell-agnosticism (zsh / bash / sh) — on this machine only

`pwsh`/PowerShell isn't available on this (darwin) agent machine, so that row stays for the
owner. zsh, bash, and sh were run here:

```
$ zsh  -lc './bin/tortui --version'
tortui dev (commit 4fc7162, built 2026-09-28T12:20:46Z)
$ bash -lc './bin/tortui --version'
tortui dev (commit 4fc7162, built 2026-09-28T12:20:46Z)
$ sh   -c  './bin/tortui --version'
tortui dev (commit 4fc7162, built 2026-09-28T12:20:46Z)
```

### What this does *not* prove

None of the above is a substitute for the owner checklist. In particular, no unattended check can
confirm: what a real Terminal.app or Windows Terminal window actually paints (escape sequence
support varies by terminal, not just by the `TERM`/`NO_COLOR` values this machine can fake);
`conhost`'s actual behaviour (it isn't present on this OS at all); tmux's pass-through of any of
the above; or that a live resize event (as opposed to a fixed `tea.WindowSizeMsg` in a test)
reflows without garbling on a real PTY. Those are exactly the rows left blank above.
