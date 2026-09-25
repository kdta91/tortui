# TASK_TRACKER.md — tortui

Single source of truth for build state. Read `AGENT.md` first.

## Protocol

- Status values: `todo` · `in-progress` · `blocked` · `done`. Tiers: `H` · `M` · `L` (AGENT.md §11).
- **Status lives only in the task block.** Do not maintain a summary table; it will drift.
  `make next` derives the next eligible task and the done/todo/blocked counts from this file.
- **Next task:** the first `todo` block, in file order, whose `depends:` are all `done`. One at a time.
- **The PR is the status change.** Nothing is committed to `main` to start a task — the open
  branch and PR show what is in flight. The task's own PR, before review:
  1. flips its block to `status: done` and fills `**Notes:**` (≤ 10 lines; detail goes in the PR body),
  2. moves the finished block **verbatim** to the end of its phase in `docs/tracker-archive.md`
     and adds its id to that phase's **Done** line here,
  3. appends any `DEC-` entry in full to `docs/decisions.md` (≤ 8 lines) plus a one-line row in the
     index below,
  4. adds one line to `docs/session-log.md` (date, task, tier, minutes, outcome).
  Merging the PR is what makes the task done. An unmerged PR means it is not done.
- **Only a Blocked entry is committed straight to `main`.**
- Never delete a task. Superseded tasks become `done` with a note pointing at the replacement.
- New work discovered mid-task goes to **Backlog** as `T-9NN`, not into the current task.

---

## Process

**Done (archived in `docs/tracker-archive.md`):** `T-945` Fast task loop · `T-949` macOS-first, lower-latency task loop.

---

## Phase 0 — Foundation

**Done (archived in `docs/tracker-archive.md`):** `T-001` Repository bootstrap · `T-002` Configuration package · `T-003` File logging · `T-004` Secret hygiene · `T-005` Cross-platform tooling baseline · `T-006` Licensing · `T-007` Contribution policy and repo hygiene.

---

## Phase 1 — Domain contracts

**Done (archived in `docs/tracker-archive.md`):** `T-010` Indexer contracts · `T-011` Category taxonomy · `T-012` Registry and fan-out search.

---

## Phase 2 — Indexer adapters

**Done (archived in `docs/tracker-archive.md`):** `T-020` Shared HTTP client · `T-021` Torznab adapter · `T-941` Raise the minimum Go version to 1.25 · `T-022` Scraper adapter framework · `T-023` Definition loading and hot-reload · `T-024` Bundled lawful default sources · `T-025` Import an existing definition.

---

## Phase 3 — Torrent engine

**Done (archived in `docs/tracker-archive.md`):** `T-030` Engine contracts and fake · `T-942` Admit MPL-2.0 for the torrent engine · `T-943` Extend the MPL-2.0 exception to the engine's named module set · `T-031` anacrolix engine — add and list · `T-032` Engine lifecycle operations · `T-033` Update stream · `T-944` Engine review follow-ups from T-031/T-032 QA · `T-034` Download policy and path safety.

---

## Phase 4 — Persistence

**Done (archived in `docs/tracker-archive.md`):** `T-040` bbolt store · `T-042` Single-instance lock and data integrity · `T-041` Session resume.

---

## Phase 5 — TUI shell

**Done (archived in `docs/tracker-archive.md`):** `T-050` Theme · `T-051` Root model and routing · `T-052` Status bar · `T-053` Responsive table component · `T-054` Modals and confirm dialog · `T-055` Terminal capability detection and `doctor` · `T-056` Demo mode.

---

## Phase 6 — Search and results

**Done (archived in `docs/tracker-archive.md`):** `T-060` Search screen · `T-061` Results screen · `T-062` Trust badges and filtering · `T-063` Details screen.

---

## Phase 7 — Downloads


### T-070 · Add flow
```
status: todo
depends: T-063, T-031
tier: M
```
**Acceptance**
- Calls `Resolve` first when the result lacks a magnet; failures surface as a status-bar error,
  not a crash.
- Duplicate infohash already in the engine is detected and selects the existing row instead of
  adding twice.
- `Origin` populated with indexer ID and source URL, persisted via the store.
- Destination is chosen here via T-074 before the engine is handed the torrent; the resolved
  absolute path goes into `AddSource.SavePath`.

---

### T-071 · Downloads screen
```
status: todo
depends: T-070, T-033, T-053
tier: M
```
**Acceptance**
- Row layout per AGENT.md §7: name, block progress bar with percentage, transferred/total,
  down/up rate, peers, ETA, source, added-at, and the action hint line.
- Subscribes to `Updates()`; no polling of `List()`.
- Completed torrents move to a distinct section and keep their actions; the row states the
  seeding policy in effect so continued upload is never a surprise (T-034).
- Queued torrents show their position and why they are waiting (T-034).
- Each row shows its destination, since destinations are per-torrent (T-074).
- Errored torrents show the reason inline, truncated, expandable in details.
- `teatest` runs the whole screen against `engine/fake` through download → complete → error.

---

### T-072 · Download actions
```
status: todo
depends: T-071, T-032, T-054
tier: H
```
**Acceptance**
- `p` pause/resume, optimistic state update reconciled on the next snapshot.
- `x` opens the confirm dialog with three choices: remove keeping data, remove deleting data,
  cancel. Default is cancel.
- `u` opens the source page.
- Actions on a torrent that vanished between render and keypress fail gracefully.

---

### T-073 · Open file and folder
```
status: todo
depends: T-071
tier: H
```
**Files:** `internal/platform/`

**Acceptance**
- `o` opens the largest file in the torrent; `f` opens the containing folder.
- Per-OS via build tags in `internal/platform`, no `runtime.GOOS` switches (AGENT.md §14):
  macOS `open` / `open -R`; Linux `xdg-open`; Windows `explorer` / `explorer /select,`.
- Each implementation has a unit test asserting the exact argv it would exec, without
  actually launching anything. CI runs all three via `GOOS` cross-compilation of the tests.
- Path is resolved, symlink-checked, and asserted to be **inside the known destination roots**
  before launching (AGENT.md §6.12) — not against a single directory, since destinations are
  per-torrent. Outside → refuse and log.
- `exec.Command` with an argument slice, never a shell string. Test asserts a path containing
  shell metacharacters is passed through inertly.
- Incomplete torrent → status-bar message, no launch.

---

### T-074 · Download destination selection
```
status: todo
depends: T-070, T-034, T-054
tier: H
```
**Acceptance**
- The add flow shows the destination and lets the user change it **before** the torrent starts.
  Default is the configured download dir; the choice is per-torrent.
- Destination picker offers: the default, a list of saved destinations, the most recently used,
  and a free-text path field. Directory browsing is a plus, not a requirement — a validated
  path field plus saved entries covers the real use case.
- Path input expands `~`, expands environment variables, accepts Windows drive letters and UNC
  paths, and normalises separators. Relative paths resolve against the default download dir,
  never against the process working directory.
- Live validation shows: exists or will-be-created, writable, and free space against the
  torrent's size (T-034). A destination failing any check blocks the add with the reason
  visible, rather than failing after the user walks away.
- Directories are created on demand, with parents, only after the user confirms.
- The chosen path is recorded in the store so the downloads screen, open-file, open-folder, and
  remove-with-data all operate on the right root after a restart (T-041).
- Saved destinations are managed in Settings (T-082) and offered in most-recent-first order.
- **Every destination the user has used or saved joins the known-roots set** that path
  containment is checked against (AGENT.md §6.12). Adding a destination is the only way that
  set grows — it is never widened implicitly by a torrent's contents.
- `teatest`: accept default, pick a saved destination, type a new path, type an invalid path,
  type a path with no space, cancel out of the picker.

---

## Phase 8 — Settings


### T-080 · Indexer management
```
status: todo
depends: T-051, T-023, T-054
tier: M
```
**Acceptance**

**Principle: a user must never have to open the config file.** Everything about a source is
addable, editable, testable, and removable from inside the TUI. Hand-editing TOML stays
supported for people who prefer it, but it is never the documented path.

- List view: one row per source with name, type, enabled toggle, and last-search outcome.
  Keys: `a` add, `e` edit, `t` test (T-081), `space` enable/disable, `x` remove (confirm),
  `r` reload definitions (T-023).
- **Add form** with typed fields, arrow/tab navigation, and inline validation as you type:
  name, type (torznab | scraper), URL, API key, cookie, definition file. Fields irrelevant to
  the selected type are hidden, not greyed out.
- **Paste handling.** Bracketed paste must work — API keys are long and nobody types them.
  Pasting a complete Torznab feed URL that already contains `?apikey=...` splits it
  automatically into the URL and key fields rather than storing the whole string as the URL.
- `id` is derived from the name, slugified, and uniqueness-checked. The user never types an id
  unless they want to override it.
- Credential fields masked by default with a reveal toggle; never written to the log, never
  shown in `doctor` output.
- `t` tests the source **before** saving, so a bad URL or key is caught in the form rather than
  discovered at the next search.
- Save writes back to the config file, preserving existing comments and key order where
  practical, and reloads the registry live — no restart.
- The add form offers "import a definition" (T-025) alongside manual entry, so a user with a
  definition file does not have to hand-place it and then wire it up separately.
- Cancel discards cleanly with no partial write. Confirm before discarding a dirty form.
- **Empty state.** If every source has been disabled or removed, the search screen shows an
  explicit prompt that jumps straight into the add form rather than returning zero results with
  no explanation. On a default install this state should be unreachable — bundled sources are
  active and the first screen shows Latest (T-024, T-060).
- `teatest` covers: add a torznab source end-to-end, add a scraper source, paste a URL with an
  embedded key, duplicate id rejection, edit, disable, remove, and cancel-with-dirty-form.

---

### T-081 · Connection test
```
status: todo
depends: T-080
tier: M
```
**Acceptance**
- `t` runs a probe search against the selected indexer with a short timeout.
- Reports reachable / auth failed / parse failed / timeout as distinct outcomes with the
  underlying error available in details.
- Runs as a `tea.Cmd`; the UI stays responsive and the probe is cancellable.

---

### T-082 · Preferences
```
status: todo
depends: T-080
tier: M
```
**Acceptance**
- Edit default download dir (with existence, writability, and free-space validation), manage
  the **saved destinations list** (add/rename/remove, most-recent-first), rate limits, max
  active downloads, max peers, listen port, seeding policy, ratio and duration, minimum free space,
  search timeout, theme, and ASCII mode.
- Removing a saved destination warns if any active torrent is downloading there, and never
  silently drops it from the known-roots set while in use (AGENT.md §6.12).
- Changes apply live where the engine supports it; where they need a restart, say so explicitly.
- Invalid values rejected inline with the reason, never silently clamped.

---

### T-083 · Bulk import from a Torznab aggregator
```
status: todo
depends: T-080, T-081
tier: M
```
**Acceptance**
- From the add form, an "import from aggregator" path takes a base URL and API key for a
  self-hosted Torznab aggregator (Prowlarr, Jackett, NZBHydra), lists the indexers that
  instance exposes, and lets the user multi-select which to add.
- Each import becomes an ordinary `[[indexer]]` entry — nothing special-cased afterwards.
- Names come from the aggregator; ids are slugified and de-duplicated against existing entries.
- A failed or unauthenticated probe reports the distinct reason, same taxonomy as T-081.
- **Verify the aggregator's API shape against its official documentation before implementing.**
  Do not infer endpoint paths or response fields. If the documentation cannot be reached,
  mark this task `blocked` rather than guessing — a wrong endpoint here silently produces
  broken sources.

This targets self-hosted aggregator software the user already runs. It is not a source list and
it does not put any indexer into the repo.

---

## Phase 9 — Release readiness


### T-090 · Documentation and first run
```
status: todo
depends: T-072, T-082
tier: L
```
**Acceptance**
- `README.md` already exists at the repo root. This task **verifies and completes** it against
  shipped behaviour — it does not rewrite it from scratch.
- Every command, flag, path, and keybind in the README is executed and confirmed correct.
  Anything that drifted during the build is corrected here.
- Replace the Status notice at the top once the app is functional.
- Keymap table regenerated from the actual keymap definition (T-051), not hand-edited.
- Troubleshooting section extended with anything hit during the build.
- First-run screen explains that no sources ship with the binary and points at the docs, and
  carries the same legal notice as the README (AGENT.md §2).
- `docs/indexer-definitions.md` complete and verified against the real loader.
- Screenshot or asciinema cast added, captured from `--demo` so it contains no real content.

---

### T-091 · Integration suite
```
status: todo
depends: T-041, T-072
tier: M
```
**Acceptance**
- `//go:build integration` tests covering a real end-to-end download of a small,
  freely-distributable test torrent, plus resume across a restart.
- **Zero-config standalone test**: on a clean machine with an empty `TORTUI_HOME` and no other
  software installed, run a scripted search against the bundled sources, add a result, and
  download it to completion. This is the executable form of the standalone contract
  (AGENT.md §1) — if it fails, the release is blocked regardless of what else passes.
- A reachability check against every bundled source, so a rotted default fails CI.
- Excluded from `make check`; separate `make test-integration` target and a manual CI job.
- Documented prerequisites and expected runtime.

---

### T-092 · Build and release
```
status: todo
depends: T-090
tier: M
```
**Acceptance**
- `goreleaser` config producing darwin/arm64, darwin/amd64, linux/amd64, linux/arm64,
  windows/amd64, windows/arm64 artifacts with checksums.
- Version, commit, and build date injected via ldflags and shown by `--version`.
- CI release job triggered on `v*` tags; artifacts published to GitHub Releases.
- Binary runs on a clean machine with no Go toolchain present, on all three OSes.
- Free distribution channels wired up, all automated from the same tag (AGENT.md §16):
  - Homebrew tap — `goreleaser` commits the formula to `kdta91/homebrew-tap`.
  - Scoop bucket — formula committed to `kdta91/scoop-bucket` for Windows.
  - WinGet manifest PR to `microsoft/winget-pkgs` (moderated; may lag the release).
  - `go install github.com/kdta91/tortui/cmd/tortui@latest` verified working.
- Provenance: Sigstore keyless signing and GitHub build attestations on every artifact. Both
  are free. Document the `gh attestation verify` command in the README.
- Artifacts are **unsigned** for OS trust purposes. README documents the resulting warnings and
  workarounds on macOS and Windows. Log backlog items for Apple notarisation and Windows code
  signing, both of which cost money and are out of scope for v1.
- Shell completions for zsh, bash, fish, and PowerShell generated from the flag set and shipped
  in every archive, with per-shell install instructions.

---

### T-093 · Final hardening pass
```
status: todo
depends: T-091, T-092
tier: H
```
**Acceptance**
- `go test -race ./...` clean.
- `goleak` clean on startup/shutdown.
- `govulncheck` clean; any finding triaged in the decision log.
- Every `TODO` in the tree has a tracker ID.
- Coverage thresholds from AGENT.md §9 met.

---

### T-094 · Terminal compatibility matrix
```
status: todo
depends: T-056, T-092, T-093
tier: M
```
**Acceptance**
- `docs/terminal-matrix.md` records a verified pass for each of: Terminal.app, iTerm2 or
  Ghostty, Windows Terminal, a common Linux emulator, tmux, `NO_COLOR=1`, `TERM=xterm`,
  `--ascii`, and non-TTY refusal — per the command list in AGENT.md §15.
- Windows is tier 1: Windows Terminal must pass, and legacy conhost must produce the clear
  upgrade message rather than a garbled render.
- Each entry confirms column alignment, colour degradation, clean exit, and correct terminal
  restore.
- Resize to 80×24 and ~60 columns while running; documented column-drop order holds with no
  garbling.
- Shell-agnosticism confirmed once under `zsh`, `bash`, `sh`, and PowerShell.
- **Terminal.app is the pass/fail floor.** A defect visible only in a modern emulator is a
  bug; a defect visible in Terminal.app is a release blocker.
- Any failure becomes a `T-9NN` backlog item or blocks the release — agent decides and records
  which in the decision log.

---

## v1.0 release criteria

Every one of these must hold before tagging `v1.0.0`. This is the finish line — the agent stops
when it reaches it and does not start backlog items on its own.

- [ ] All tasks T-001 through T-094 are `done`.
- [ ] `make check` and `go test -race ./...` green on Linux, macOS, and Windows CI.
- [ ] Coverage thresholds from AGENT.md §9 met.
- [ ] `govulncheck` clean; `NOTICE` current; no GPL/AGPL dependency.
- [ ] Terminal matrix (T-094) passes, Terminal.app and Windows Terminal included.
- [ ] A real download completes, resumes across a restart, and removes cleanly on all three OSes.
- [ ] No infringement-oriented site is named anywhere in the repository (AGENT.md §2, §16).
      Verified by grep against the T-024 allowlist.
- [ ] A fresh install searches and downloads successfully with no configuration, no account,
      and no other software installed — verified on all three OSes (T-091).
- [ ] Latest works on first launch with no keyword typed, against every bundled source.
- [ ] Malicious-path `.torrent` fixtures are refused on all three OSes (T-034).
- [ ] A second instance refuses to start; a crash mid-config-write loses nothing (T-042).
- [ ] Quit and `SIGINT` both restore the terminal cleanly with downloads active (T-042).
- [ ] Per-torrent destinations survive a restart and are honoured by open, reveal, and
      remove-with-data (T-074).
- [ ] No documented path to a core capability (search, add, download, open, remove) requires
      installing anything besides tortui.
- [ ] README accurate against shipped behaviour; `--demo` works on a clean install.

---

## Backlog (not scheduled)

- `T-901` RSS/watch-list auto-download
- `T-902` Sequential download / streaming-while-downloading
- `T-903` Per-file selection before adding
- `T-904` Bandwidth scheduling by time of day
- `T-905` Torznab `caps`-driven dynamic category UI
- `T-906` Mouse support
- `T-907` Remote-control mode (headless daemon + TUI client)
- `T-908` Apple notarisation (requires a paid Apple Developer account)
- `T-909` Windows code signing (requires a certificate or signing service)
- `T-910` Homebrew core submission (has notability requirements a tap does not)
- `T-911` Install a `pre-merge-commit` hook. QA on T-004 (PR #4) demonstrated that merge commits
  bypass `scripts/pre-commit` entirely: git invokes `pre-merge-commit` for merges, and that hook is
  not installed. Reproduced by committing a secret on a side branch with hooks disabled, then
  merging with `git merge --no-ff` while `pre-commit` was active — the merge succeeded and the
  secret entered history uncaught.
- `T-912` Close the binary-file gap in the secret scan. QA on T-004 (PR #4) confirmed that gitleaks
  skips binary content by design in both `gitleaks git --staged` (the hook) and `gitleaks dir`
  (`make scan` in CI), so a secret embedded in a binary-ish file is caught by neither the hook nor
  the CI backstop.
- `T-913` Close the backtick raw-string bypass in `scripts/check-indexer-hostnames.sh`. QA on T-007
  (PR #7) found that a Go raw string literal — a backtick-delimited value with no `http(s)://`
  scheme, in a gated path — evades the check entirely, because the value-shape regex strips only an
  optional `"` and never a backtick. This is outside the gaps (a)/(b)/(c) that T-007 documented.
- `T-914` `docs/indexer-hostname-allowlist.md`'s prose enumeration of auto-allowed private IPv4
  ranges omits `0.0.0.0/8`, which `is_private_ipv4()` in the script does allow and which DEC-040 and
  the script header both list. Cosmetic doc inconsistency found by QA on T-007.
- `T-915` Create `docs/session-log.md` and backfill T-001…T-010. AGENT.md §11 requires a running
  session log — one line per task with timestamp, task ID, and outcome — as the thing a human reads
  to catch up. It has never existed; `docs/` currently holds only `indexer-hostname-allowlist.md`.
  Found by QA on T-010 (PR #8), disclosed by the T-010 build agent rather than backfilled from one
  task's vantage point.
- `T-916` Enforce per-package coverage thresholds in `make cover`. `COVER_THRESHOLD := 0` at
  `Makefile:28`, so the gate currently enforces nothing, and it compares a single repo-wide total.
  AGENT.md §9 mandates per-package floors (`internal/indexer` and `internal/engine` >= 75%,
  `internal/tui` >= 50%) that one global number structurally cannot express. Live as of T-010:
  `internal/indexer` is at 100% with nothing in CI guarding it against regression. Found by QA on
  T-010 (PR #8).
- `T-917` `CategoryFromString` is first-token-wins, so an Other-class word leading the label
  discards a specific token later in it. Reproduced against merged `main` (261a381):
  `"Misc/Software"` -> other and `"Unknown/Audio"` -> other, while `"Software/Misc"` -> software.
  Common in the newznab block-0 dialect, so T-021/T-022 will meet it. Found by QA on T-011 (PR #9).
- `T-918` `CategoryFromString` applies simple case folding only, so fullwidth forms miss every
  bucket. Reproduced against merged `main` (261a381): `"AUDIO"` and `"audio"` in fullwidth both ->
  other. Cosmetic; recorded so T-022 is not surprised. Found by QA on T-011 (PR #9).
- `T-919` Unwrap `*ast.ParenExpr` in `isCategoryTypeExpr` in `internal/indexer/category_test.go`.
  The §2 bucket-set tripwire matches only a bare `*ast.Ident`, so a parenthesized type spelling
  evades it. QA round 3 on T-011 (PR #9) judged this non-blocking because `make fmt-check` rejects
  that spelling and gofumpt rewrites it to the form the tripwire catches — but the guard should not
  depend on the formatter to hold. Also covers the alias, untyped-conversion and bare-inline shapes
  that DEC-051 discloses as open by design.
- `T-920` Share an in-flight fetch between concurrent fan-outs (single-flight per cache key). As
  built in T-012 the per-source minimum refresh interval is claimed before the request, so two
  concurrent *distinct* queries reaching one source inside the interval get one fetch and one
  `ErrThrottled` skip. Pinned by `TestSearchAllConcurrentCallsShareTheCache` and documented in
  DEC-054; harmless while the TUI issues one search at a time, worth closing before anything
  issues two.
- `T-921` **Resolved by T-061 (DEC-110).** `SearchAll` did not tell the caller which sources
  answered from cache, and T-061's acceptance criteria required the status bar to show "when
  results came from cache rather than a fresh fetch" with the T-012 return shape
  (`[]Result`, `[]SourceError`, `error`) having nowhere to put it. Decided without changing that
  exported signature: `mergeResults` tags each merged `Result.Extra["tortui.cacheHit"]`
  (`indexer.ExtraKeyCacheHit`), the same registry-writes-`Extra` pattern `ExtraKeySources` already
  used; `internal/tui/results.go`'s `cacheSummary` aggregates it for the status bar.
- `T-922` No TOML keys for the registry's cache TTL and per-source minimum refresh interval.
  `internal/config` has `search_timeout` only; `indexer.Config`'s other two durations are
  code-level defaults. Nothing is wired either way yet — no composition root exists — so this is
  a note for whichever task builds one.
- `T-923` `SearchAll` waits out the slowest source's full per-indexer timeout even once every other
  source has answered. That is the documented meaning of a per-indexer timeout and not a bug, but
  T-060/T-061 may want results delivered incrementally rather than at the slowest source's pace.
  Found by QA on T-012 (PR #10).
- `T-924` `reserveFetch` returns `false` for an id that is not registered, which surfaces to the
  caller as an `ErrThrottled` skip rather than an unknown-source failure. Currently unreachable —
  there is no deregistration API — so this is a latent misleading-message bug only, worth closing
  before any API that can remove a source lands. Found by QA on T-012 (PR #10).
- `T-925` Document the dedup seeder-tie survivor. When two candidate results tie on seeders the
  first in selection order survives; verified deterministic by QA, but stated in neither DEC-056 nor
  the `mergeResults` godoc, so a future reader cannot rely on it. Found by QA on T-012 (PR #10).
- `T-926` `scripts/check-indexer-hostnames.sh`'s value-shape rule fires on ordinary Go inside
  `internal/indexer/**`, which is a fully gated path so every line is scanned. Its regex is
  `(url|host|endpoint|base_url)[:=]` followed by anything with a dot in it, so
  `URL: server.URL`, `ErrEmptyURL = errors.New("...")` and even the prose `a URL: AGENT.md §2`
  are all reported as new indexer hostnames (`server.url`, `errors.new`, `agent.md`). T-020
  worked around it by renaming the sentinels prefix-first (`ErrURLEmpty`) and assigning through
  a dotless local before a `URL:` struct key, which is a real cost on every future adapter in
  this tree. Requiring the matched value to look like a hostname — at least one dot-separated
  label followed by a plausible TLD, and not a known Go identifier shape — would keep the check
  meaningful without the false positives. Found while building T-020.
  T-041 hit it on `internal/lifecycle` field copies and worked around it in code (split lines).
- `T-927` `internal/logging`'s free-text masker misses `CookieHeader:` in a `%+v` struct dump. The
  regex requires the sensitive word immediately followed by `[:=]`, so `APIKey:` is caught but
  `CookieHeader:` is not — the `Header` sits between. Only bites when a caller formats a struct into
  a string itself rather than passing it as a log attribute (reflection-based masking handles the
  attribute path correctly). Found by QA on T-020 (PR #11); a defect in `internal/logging`, not in
  `httpx`.
- `T-928` `httpx`'s per-host limiter keys on the literal `url.URL.Host`, so `feed.example.org` and
  `feed.example.org:80` are separate buckets and `checkRedirect` refuses a redirect that only adds
  an explicit default port. Both fail closed — a doubled rate budget and a refused-but-safe
  redirect, never a followed unsafe one. Normalising the default port per scheme fixes both. Found
  by QA on T-020 (PR #11), disclosed in the `ErrCrossHostRedirect` godoc and DEC-064.
- `T-929` `slog.Any` on an `httpx.Config` renders `!ERROR:json: unsupported type: func(...)` because
  `Config.Jitter` is a function field. Fails safe — no credential is emitted — but the log line is
  useless. A `LogValue` on `Config` (as `Credentials` already has) would fix it. Found by QA on
  T-020 (PR #11).
- `T-930` `httpx.checkRedirect` compares each hop's scheme against `via[0]`, the original request,
  rather than the immediately preceding hop, so `http` -> `https` -> `http` is followed. No new
  exposure versus the configured scheme — the first hop was already cleartext by the user's own
  config, which is why DEC-064 follows an upgrade — but comparing against the previous hop would be
  tighter. Found by QA on T-020 (PR #11).
- `T-931` `httpx`'s `leakCases` table does not include the `ErrInsecureRedirect` path added in
  T-020's round-1 remediation. That path is covered by its own dedicated tests, so this is a gap in
  the systematic leak sweep rather than an uncovered behaviour. Found by QA on T-020 (PR #11).
- `T-932` Harden the Windows `make check` CI leg against a Chocolatey CDN outage. T-020 (PR #11) was
  blocked twice by `choco install shellcheck` failing with a 504 from
  `community.chocolatey.org` -> the pinned shellcheck v0.9.0 release asset; the identical job passed
  on re-run, so it is a transient upstream outage, not a code defect. Because a push and a
  pull_request event each produce a `make check (windows-latest)` context on the same head commit
  (DEC-042), BOTH runs must be re-run before the required context clears — re-running one leaves the
  PR blocked with no obvious signal. Pinning a vendored shellcheck binary, caching it, or retrying
  the install step would remove a recurring stall from every future PR.

- `T-933` `scripts/check-indexer-hostnames.sh` flags the value of an XML namespace declaration.
  A Torznab feed identifies its extension attributes with an `xmlns:torznab` declaration whose
  value is an http URL on the protocol's own domain, and an RSS document often carries an
  `xmlns:atom` one pointing at the W3C (neither is written out here, for the same reason the
  fixtures cannot write them). Both are namespace
  *names* — identifiers compared as strings, never dereferenced by any parser — but the script's
  scheme scan sees a hostname inside a `testdata/` file and fails the check. T-021's fixtures
  therefore omit the declarations and say so in their headers, which costs nothing functionally
  (Go's `encoding/xml` accepts an undeclared prefix and this adapter matches `attr` by local
  name regardless of namespace, both asserted by
  `TestAttrElementParsesUnderAnyNamespacePrefix`) but does make the fixtures slightly less
  faithful to the wire. Skipping the quoted value of an attribute literally named `xmlns` or
  `xmlns:PREFIX`, and nothing else on the line, would fix it. Deliberately **not** done inside
  T-021: it is a change to a §2 safety gate, and one belongs in its own reviewed task rather than
  as a side effect of an adapter. Every scraper fixture in T-022 will hit the same wall. Found
  while building T-021.

- `T-934` `indexer.Result` has no safe logging path. `Title`, `Magnet`, `Uploader` and `ID` — on
  the branches where it falls back to a non-URL guid, comments, link or the title — all carry
  source-controlled free text under key names `internal/logging` does not mask
  (`sensitiveKeySubstrings` is `url, apikey, cookie, token, secret, password, passkey,
  authorization`), and an opaque credential has no value shape `maskText` recognises. So logging
  a whole `Result` writes attacker-controlled text to the log file in plaintext, including an
  api_key a hostile or broken source echoed back into a `title` element, a magnet's `dn=`, an
  `attr name="uploader"` element or a guid. It is not a Torznab problem: every adapter from T-022 on
  produces the same frozen §5 type, and the leak is in the type's relationship to the logger
  rather than in any adapter. A real fix is a `LogValue()` on `Result` that renders the risky
  fields under masked names, or a registry-level rule that a result is only ever logged under a
  masked key; either touches the frozen §5 contract, so it needs its own `DEC-` and a note on
  every affected task (AGENT.md §5, §12). Found by QA on T-021 (PR #12), rounds 1 and 2;
  disclosed rather than fixed there, per DEC-066 and DEC-071.
- `T-935` `Adapter.Search` does not consult `Caps.Search`, so a source that declared
  `search available="no"` still receives a request from a direct caller. The registry already
  skips a source that lacks the capability a query needs (§6.3), so nothing reaches this path
  today, and `torznab.Discover` already refuses to run the Latest probe against such a source —
  but a backstop inside the adapter, mirroring the existing `ErrLatestUnsupported` path for
  `ModeLatest`, would make the two capabilities behave the same way and would stop a future
  caller sending a request the source said it cannot answer. Found by QA on T-021 (PR #12).
- `T-936` Decide whether `internal/logging`'s sensitive-key list should cover domain fields that
  carry attacker-controlled free text — `title`, `uploader`, `magnet`, `name` — rather than only
  credential-shaped key names. `T-934` fixes the `indexer.Result` case at the type level;
  `engine.TorrentStatus.Name` will have exactly the same shape once the engine lands and no
  adapter owns it, so whether the key list itself should grow is a cross-cutting policy question
  rather than a torznab one. It cuts both ways: masking `title` wholesale would redact the one
  field a user reads a log line to identify, so the answer may be a narrower rendering rather
  than a new substring. Found by QA on T-021 (PR #12), round 2.
- `T-937` Decide where the infohash helpers live. `normaliseInfoHash`, `isHex`, `isBase32`,
  `infoHashInMagnet`, `magnetFor` and `withoutQuery` now exist in both `internal/indexer/torznab`
  and `internal/indexer/scraper`, character for character in most cases. AGENT.md §4 forbids one
  adapter importing another, so the duplication is correct today; the alternative is exporting
  them from `internal/indexer`, which grows the package that holds the frozen §5 contracts.
  Deliberately deferred so a third adapter makes the call with three data points rather than
  two. Found while building T-022.
- `T-938` Category push-down for scraper sources. The T-022 schema has no way to say which of a
  site's own category values correspond to tortui's buckets, so `Caps.Categories` is honestly
  `false` and a category filter is neither sent to the source nor applied locally —
  `indexer.Query` permits exactly that, but it means the search screen's category filter does
  nothing for a scraped source. A `categories:` mapping block (bucket → the site's own parameter
  value) would fix it, mirroring what `torznab` does with the ids from a caps document
  (DEC-070). Out of scope for T-022, whose criteria enumerate the schema. Found while building
  T-022.
- `T-939` A `detail` block for the scraper schema, so `Resolve` can fetch a details page. Today
  `scraper.Resolve` makes no network call: it is a no-op when a magnet is present, derives a
  magnet from an infohash, and otherwise returns `ErrUnresolvable`. That leaves the case AGENT.md
  §5 wrote `Resolve` for — "indexers that only return a details page" — unserved for exactly the
  adapter most likely to meet it, so a definition must currently read the magnet, the torrent
  link or the infohash off the listing page itself. A `detail:` block reusing the same field
  engine is perhaps sixty lines. Deliberately not built in T-022: the criteria enumerate the
  schema and a `detail` block is not in the list. Found while building T-022.
- `T-940` A `{{page}}` placeholder for the scraper schema. T-022 substitutes `{{query}}`,
  `{{limit}}` and `{{offset}}`; page-numbered pagination (`?page=3`) is at least as common on a
  listing site as offset pagination, and expressing it needs a page size the definition declares
  so `Query.Offset` can be divided by it. Left out rather than guessed at. Found while building
  T-022.

- `T-942` Watch the definitions directory instead of only reloading on request. T-023 ships
  `(*Loader).Reload()`, which the user (via Settings) drives; nothing notices a file that changed
  underneath tortui. A watcher would need a dependency (`fsnotify` or equivalent, so a `DEC-` row
  and a licence check) or a poll loop, and the criteria asked for neither, so it was left out
  rather than guessed at. Note that §6.13's "never poll a source on a timer" is about *sources*,
  not about the local filesystem, so it does not settle this. Found while building T-023.

- `T-943` Decide how a config `[[indexer]]` entry names its definition. `internal/config`'s
  schema (T-002) gives a scraper indexer a `definition` field documented as a **path**
  (`definition = "example.yml"`), while T-023's loader indexes the set by the definition's own
  `id` and keeps a file name only for the files it skipped. Whoever wires config to the registry
  has to pick one — resolve by file name, resolve by id, or have the loader expose both — and
  T-023 deliberately did not pick, because the wiring task is the one that can see both sides.
  Found while building T-023.

- `T-944` Decide whether `*.yaml` should be read as well as `*.yml`. T-023's criterion says
  `*.yml` and the loader reads exactly that, so a user who names a file `archive.yaml` gets
  silence: it is not loaded and, because it is never opened, it is not reported as skipped
  either. Options are to read both extensions, or to keep reading only `.yml` and warn about a
  `.yaml` file sitting in the directory. Found while building T-023.
- `T-945` A field-concatenation or URL-template capability for the scraper schema, so a field
  like `source_url` or `torrent_url` could be built from more than one selector (e.g. a fixed
  prefix plus an `identifier` field) instead of needing the whole value in one selector. The
  bundled Internet Archive definition (T-024) cannot populate `SourceURL` for exactly this
  reason: the Advanced Search API returns a bare `identifier`, not the `details/{identifier}`
  page path, and building that path today would mean hand-formatting a string no response field
  actually carries, which AGENT.md §16 treats as inference rather than verification. Found while
  building T-024; see `docs/bundled-sources.md`.
- `T-946` `awaitInfo` reads `paused`/queued under `Engine.mu`, then calls
  `DisallowDataDownload`/`DisallowDataUpload` after unlocking, so a concurrent `promote` or `Resume`
  in that window can leave a torrent showing `StateDownloading` with its transfers disallowed.
  Apply the gate under the lock, or re-check after it. Found in review of T-034 (PR #36).
- `T-947` The free-space checks (add-time and the periodic re-check) are per torrent and ignore the
  remaining need of other active downloads on the same destination/filesystem, so several
  downloads can together overcommit a disk each one fits alone. Sum the remaining need per
  destination (ideally per filesystem). Found in review of T-034 (PR #36).
- `T-948` A magnet refused in `awaitInfo` (unsafe path or not enough space) stays tracked as
  `StateErrored`, so a re-`Add` of the same infohash returns that stale id with a nil error instead
  of refusing again; a queued spec whose promote-time `attach` fails stays tracked via `e.fail` the
  same way. Untrack (or re-evaluate) refused entries on re-`Add`, as `untrackFailedSpec` does for
  `addSpec`. Found in review of T-034 (PR #36).
- `T-950` Wire `lifecycle.Session` into the composition root when one exists: `NewSession` after
  `OpenStore` and the engine, `Resume` before the TUI starts (show `ResumeReport.Missing` on
  first render), `Save` after every add/remove, and `ShutdownOptions.Session`. The add flow
  (T-070) records `Origin` with `SetTorrent` before `Save`; `Save` keeps it. From T-041. Also pass
  `tui.WithHistory(store)` when wiring `tui.New` there — T-060 built the search screen against a
  `HistoryStore` interface `*store.Store` already satisfies, but no production call site (only
  `internal/app/demo.go`'s `WithSearcher`) passes one yet, so recent-query suggestions are inert
  outside tests until this wiring exists. From T-060 QA.
- `T-951` Downloads screen: a row whose `Err` wraps `engine.ErrDataMissing` offers removal (the
  `x` dialog, keep-data default) as its primary action, not just the truncated reason (T-071,
  T-072). From T-041.
- `T-952` A user-paused torrent comes back running after a restart. `Shutdown` pauses everything
  before `Session.Save`, so pause state cannot be read from `State` there; needs a
  user-pause flag in `engine.ResumeData` read before the shutdown pause. From T-041.
- `T-953` `Session.Resume` re-keys a record onto whatever ID `Restore` returns. Two store records
  sharing an infohash make `Restore` return the first's ID for the second, and the
  `DeleteTorrent`/`SetTorrent` pair then overwrites the first record's data (and the re-key can
  delete a record already re-keyed onto that ID; the next `Save` repairs it). Detect an ID already
  restored this pass and drop the duplicate record instead. Found in review of T-041 (PR #38).
- `T-954` `make check (windows-latest)`, advisory: `internal/engine`
  `TestProbeListenPortFallsBackWhenTaken` and `TestProbeListenPortPrefersTheConfiguredPort` fail
  with "no random port was free on both TCP and UDP" (PR #38 run 36134477067). This is T-034's
  probe; it passed on `main`'s last run, so it is intermittent on Windows runners. Make the probe
  retry more than one random port before giving up. Found on T-041's PR.
- `T-955` The anacrolix file storage keeps every data file memory-mapped after `Engine.Close`. The
  library's default mmap file IO never unmaps, and `fileTorrentImpl.Close` is a no-op. On Windows
  the file then cannot be rewritten or deleted ("user-mapped section open" / "being used by
  another process"), so remove-with-data breaks too. Before T-041 a completed file was unmapped by
  its `.part` rename; T-041 turned part files off, so completed files now stay mapped as well.
  Failing on `make check (windows-latest)` (advisory, PR #38 run 36134477067):
  `TestRestoreSurfacesMissingDataAsErroredNotDropped`,
  `TestRestoreResumesFromExistingDataWithoutDownloading`,
  `TestRestoreResumesAPartialDownloadFromItsVerifiedPieces`,
  `TestCompletedTorrentSeedsUnderTheRatioPolicy` and
  `TestCompletedTorrentStopsUploadingUnderTheOffPolicyAndResumeOverrides` (the last two regressed
  in T-041). Options: selecting the library's classic file IO is only possible through the
  `TORRENT_STORAGE_DEFAULT_FILE_IO` env var at process start, so use a wrapping `storage.ClientImpl`
  whose close releases handles, or a small tortui-owned file storage. Tier H.
- `T-956` The search screen's recent-query suggestions (T-060) are display-only: `Recent: ...` is
  rendered from `HistoryStore.ListHistory`, but nothing lets the user click/select one back into
  the query field — the acceptance text says "offered as suggestions," which this reads narrowly
  as "shown," not "recallable." Needs a keybind (the flat cursor would need a row for the
  suggestion list, or a dedicated key) that sets `search.query` to the chosen entry. Found by QA
  on T-060 (PR #39).
- `T-957` `internal/tui/results.go`'s `parseAgeSeconds` maps `formatAge`'s `"-"` (no `Published`
  date) to `0`, the same value as "just now" — so an undated result sorts as the *newest* item
  under the Latest default (ascending age), a false positive rather than the "sorts last" a
  missing date should get. Needs a distinct sentinel (e.g. a very large value, or a stable
  secondary key) so undated results sort to the end regardless of sort direction. Found by QA on
  T-061 (PR #40).
- `T-958` `internal/tui/results.go`'s `formatSize` can render 9 characters (`"1023.9 MB"`,
  `"1023.9 GB"`, etc. — one decimal digit plus a 4-character unit above 1000) while the Size
  column is only 8 wide, so `components.Table`'s `theme.Truncate` ellipsises it. Either widen the
  column, or round/format so the string never exceeds 8. Found by QA on T-061 (PR #40).
- `T-959` The results-screen `?` help overlay is 27 lines at 80×24, over the 24-line floor
  (DEC-109, AGENT.md §7). Bring it to 24 or fewer, for example by merging the s/S lines or the
  1–4 screen-jump lines. Found by QA on T-062 (PR #41).
- `T-960` No test checks that the trust column in `results.go` sets `Accent: true`. Found by QA
  on T-062 (PR #41).
- `T-961` The absence checks in the teatest trust-filter tests are weak: a 150ms sleep followed
  by `io.ReadAll` can see no new frame at all. Found by QA on T-062 (PR #41).
- `T-962` The first `s` onto the Trust column sorts ascending, so the most trusted rows need `S`
  to reach the top. Consider descending as the first direction. Found by QA on T-062 (PR #41).
- `T-963` `resultsModel.setResults`'s initial row selection anchors to the raw pre-sort result
  order — `components.Table.SetRows`'s `ensureSelection` runs before `applyModeDefault`'s sort,
  and the subsequent `SortBy` calls preserve selection by identity — rather than the row visually
  at the top after the default sort. A fresh result set's highlighted row is not necessarily the
  one shown at the top of the table. Found while building T-063, whose `d` key needed "the
  highlighted row" to mean something predictable.



---

## Decision Log

One line per decision. Full text — rationale, evidence, affected tasks — lives verbatim in
`docs/decisions.md`; read one entry with `grep -n '^| DEC-102 ' docs/decisions.md`.
New entries: append the full row to `docs/decisions.md` **and** a one-line row here.

| ID | Date | Summary |
|---|---|---|
| DEC-001 | — | Go + bubbletea + anacrolix/torrent |
| DEC-002 | — | Torznab adapter before scrapers |
| DEC-003 | — | Scraper sources are YAML definitions, not compiled selectors |
| DEC-004 | — | Trust is display metadata only |
| DEC-005 | — | macOS config at ~/.config/tortui, not ~/Library/Application Support |
| DEC-006 | — | All scripts POSIX sh, make targets GNU make 3.81-compatible |
| DEC-007 | — | Terminal.app is the compatibility floor |
| DEC-008 | — | --demo mode is a first-class feature, not a test fixture |
| DEC-009 | — | MIT license; dependencies restricted to MIT/Apache-2.0/BSD/ISC |
| DEC-010 | — | No *infringement-oriented* site is named anywhere in the repo, including tests and fixtures |
| DEC-011 | — | Distribution via GitHub Releases, a Homebrew tap, a Scoop bucket, WinGet, and go install |
| DEC-012 | — | v1 ships unsigned with documented OS warnings |
| DEC-013 | — | Ship definitions for a few unambiguously lawful sources, enabled by default |
| DEC-014 | — | No DHT crawling and no self-built index, ever |
| DEC-015 | — | tortui depends on no external software at runtime; bundled definitions are go:embed-ed |
| DEC-016 | — | Latest is an explicit Query.Mode, not an empty-string search |
| DEC-017 | — | Destination is per-torrent, chosen before the add completes |
| DEC-018 | — | Path containment checks resolve against a set of known destination roots |
| DEC-019 | 2026-09-12 | CI installs golangci-lint via go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2, and installs GNU Make via choco on the windo… |
| DEC-020 | 2026-09-12 | Superseded by DEC-021. Branch protection on main requiring the CI check was left unconfigured, flagged as blocked rather than silently skipped |
| DEC-021 | 2026-09-12 | Repo owner made kdta91/tortui public; branch protection on main then configured via the classic API (required_status_checks on the three make check m… |
| DEC-022 | 2026-09-12 | Corrected 2026-09-12 (same day, on QA remediation of PR #2). Per-OS config/state/download path resolution lives in internal/platform (paths_darwin.go… |
| DEC-023 | 2026-09-12 | On Linux, the default download directory prefers $XDG_DOWNLOAD_DIR/tortui over ~/Downloads/tortui when XDG_DOWNLOAD_DIR is set (same precedence used… |
| DEC-024 | 2026-09-12 | internal/logging (T-003) writes JSON log lines and calls slog.SetDefault on the logger it builds |
| DEC-025 | 2026-09-12 | T-003's "rotation at 10 MB with 3 files retained" is implemented as 3 rotated backups plus the still-active file (4 files on disk at steady state: to… |
| DEC-026 | 2026-09-12 | Addendum 2026-09-12 (PR #3 QA remediation, same day). A sensitive attribute or URL is redacted wholesale ([REDACTED]), not partially (e.g. keeping a… |
| DEC-027 | 2026-09-12 | Rotation (internal/logging/rotate.go) is a from-scratch io.WriteCloser, not a third-party library such as natefinch/lumberjack |
| DEC-028 | 2026-09-12 | Corrected 2026-09-12 (same day, on QA remediation of PR #3) — the original row below contained a false statement and is rewritten rather than superse… |
| DEC-029 | 2026-09-12 | CI installs gitleaks v8.30.1 via go install github.com/zricethezav/gitleaks/v8@v8.30.1 (not github.com/gitleaks/gitleaks/v8) on all three OSes; make… |
| DEC-030 | 2026-09-12 | scripts/pre-commit and make scan both fail loudly (block the commit / fail the build) with an install hint when the gitleaks binary is missing, rathe… |
| DEC-031 | 2026-09-12 | A committed config.toml (as opposed to config.example.toml) is blocked by two independent mechanisms: a filename check in scripts/pre-commit (git dif… |
| DEC-032 | 2026-09-12 | internal/logging/mask_test.go's deliberate secret-shaped fixtures (T-003) are excluded from scanning via a [allowlist] paths entry in .gitleaks.toml… |
| DEC-033 | 2026-09-12 | Added a custom tortui-generic-cookie rule ((?i)\b(?:set-)?cookie\b\s*[:=]\s*\S{6,}) to .gitleaks.toml |
| DEC-034 | 2026-09-12 | shellcheck -s sh $(wildcard scripts/*) is wired into make lint and fails loudly (build error + install hint) when shellcheck is missing, rather than… |
| DEC-035 | 2026-09-12 | make build-all sets CGO_ENABLED=0 for all six cross-compiles |
| DEC-036 | 2026-09-12 | The new CI build-all job runs on ubuntu-latest only, not the three-OS matrix check already uses |
| DEC-037 | 2026-09-12 | QA remediation of PR #5, same day. Every Makefile recipe line now starts its shell invocation with a literal set -eu;, in addition to (not instead of… |
| DEC-038 | 2026-09-12 | LICENSE's copyright holder is the GitHub handle kdta91, not a personal or legal name; NOTICE (generated by go-licenses/v2 v2.0.1) enumerates exactly… |
| DEC-039 | 2026-09-13 | QA remediation of PR #6, same day. make licenses's NOTICE-merge step now pins LC_ALL=C on both the sort -u that dedups/orders the merged per-GOOS rep… |
| DEC-040 | 2026-09-13 | Corrected 2026-09-13 (QA remediation of PR #7) — the closing sentence of the rationale column below was false and is rewritten in place, matching how… |
| DEC-041 | 2026-09-13 | check-indexer-hostnames.sh is wired as its own indexer-hostnames CI job (if: github.event_name == 'pull_request'), not as a new prerequisite of make… |
| DEC-042 | 2026-09-13 | QA remediation of PR #7, same day. Branch protection's required_status_checks.contexts on main now includes check indexer hostname allowlist (T-007)… |
| DEC-043 | 2026-09-13 | internal/indexer/indexer.go declares a bare type Category int with no constants and no methods, rather than T-010 creating category.go, inlining a pl… |
| DEC-044 | 2026-09-13 | Trust.String() returns lowercase tokens (unknown/none/verified/trusted/vip) while Trust.Badge() returns the display cells (VIP/TR/✓/""); Badge() retu… |
| DEC-045 | 2026-09-13 | Result.Validate() validates only that at least one of Magnet/TorrentURL is set, treats a whitespace-only value as unset, and reports failure by wrapp… |
| DEC-046 | 2026-09-13 | Corrected 2026-09-13 (same day, on QA remediation of PR #9) — the tripwire sentence in this column was false and is rewritten in place, matching how… |
| DEC-047 | 2026-09-13 | CategoryFromTorznab maps by 1000-block (id/1000) and the code contains the block NUMBERS only — never the block names the Torznab sources give them.… |
| DEC-048 | 2026-09-13 | CategoryFromString lowercases the label, splits it on every rune that is not a letter or digit, and returns the bucket for the first token found in a… |
| DEC-049 | 2026-09-13 | The bare type Category int declared by T-010 was relocated from internal/indexer/indexer.go into internal/indexer/category.go rather than left in pla… |
| DEC-050 | 2026-09-13 | Corrected 2026-09-13 (same day, on QA remediation round 2 of PR #9) — the scan-coverage sentence in the rationale column below was false and is rewri… |
| DEC-051 | 2026-09-13 | QA remediation round 2 of PR #9. categoryConstNames gains a second pass that sweeps every const and var declaration in every non-test file of the pac… |
| DEC-052 | 2026-09-13 | SourceError is one struct carrying both outcomes — IndexerID, Err, and a Skipped bool — rather than two return slices or a skip reported as an ordina… |
| DEC-053 | 2026-09-13 | SearchAll returns a non-nil error in exactly two cases: ErrAllSourcesFailed when at least one source failed and none succeeded, and ErrNoSources when… |
| DEC-054 | 2026-09-13 | The per-source minimum refresh interval (default 1s) is enforced by *skipping* the source with ErrThrottled, and the slot is claimed under the regist… |
| DEC-084 | 2026-09-16 | internal/engine.FileStatus is {Path, SizeBytes, DownloadedBytes, Progress} and internal/engine.Origin is {IndexerID, SourceURL}; internal/engine/fake… |
| DEC-055 | 2026-09-13 | Each source's Search runs on a goroutine of its own with the answer handed back over a buffered channel, and that goroutine recovers a panicking adap… |
| DEC-056 | 2026-09-13 | Dedup identity is the infohash (trimmed, lowercased) when present, else the normalised title (lowercased, every run of non-letter/non-digit collapsed… |
| DEC-057 | 2026-09-13 | SetEnabled(id, bool) was added alongside the four methods the criteria name, and naming ids explicitly in SearchAll queries those sources whether or… |
| DEC-058 | 2026-09-14 | Only 429 and 5xx are retried. Every other 4xx is permanent, 408 Request Timeout included, and a transport-level failure (dial refused, connection res… |
| DEC-059 | 2026-09-14 | Corrected 2026-09-14 (QA remediation of PR #11) — the "ends the attempt loop" claim in this column was false for one class of value and the row is re… |
| DEC-060 | 2026-09-14 | The per-host rate limiter is ~60 lines of sync.Mutex + map[string]time.Time in ratelimit.go. No dependency was added — in particular not golang.org/x… |
| DEC-061 | 2026-09-14 | Credential safety in httpx is structural, not incidental: credential fields are named so internal/logging's key-name rule fires on them (APIKey, Cook… |
| DEC-062 | 2026-09-14 | Corrected twice on 2026-09-14 (QA remediation of PR #11, rounds 1 and 2) — round 1: this row described the check as host-only and said same-host redi… |
| DEC-063 | 2026-09-14 | A response body over the cap is an error (ErrBodyTooLarge), never a truncated body, and the cap is enforced twice: a declared Content-Length above it… |
| DEC-064 | 2026-09-14 | The redirect scheme rule is asymmetric on purpose: a same-host https to http hop is refused, a same-host http to https hop is followed, and a change… |
| DEC-065 | 2026-09-14 | A Torznab item's peers attribute is the total swarm (seeders + leechers), so Result.Leechers is peers - seeders. An explicit leechers attribute, when… |
| DEC-066 | 2026-09-14 | Corrected twice on 2026-09-15 (QA remediation of PR #12, rounds 1 and 2) — round 1: the second clause of this column said no Result field that is not… |
| DEC-067 | 2026-09-14 | Caps.Latest is set by making a real keyword-less t=search request during Discover, and is true only when that request returned a well-formed feed con… |
| DEC-068 | 2026-09-14 | There is no uploader-trust attribute anywhere in the Torznab/Newznab protocol, so a Torznab source's results are TrustUnknown unless the server inven… |
| DEC-069 | 2026-09-14 | Corrected 2026-09-15 (QA remediation of PR #12) — the last clause of this column was false and is rewritten in place, matching how DEC-040, DEC-046,… |
| DEC-070 | 2026-09-14 | A category filter is translated into only the category ids the source published in its own caps document, sent as cat=, and the results are not filte… |
| DEC-071 | 2026-09-15 | Corrected 2026-09-15 (QA remediation of PR #12, round 2) — this row was written on the round-1 finding and named two pass-through fields; there are f… |
| DEC-072 | 2026-09-15 | The project's minimum Go version rises from 1.23 to 1.25 — AGENT.md §3's Language row, go.mod's directive (go 1.25.0), .github/workflows/ci.yml's GO_… |
| DEC-073 | 2026-09-15 | No error the scraper package produces ever contains a param value, a path, or the base_url out of a definition, and no text out of a response. What a… |
| DEC-074 | 2026-09-15 | A scraper definition cannot carry a credential. The schema has no placeholder for one, and validation refuses every {{…}} it does not define, so {{ap… |
| DEC-075 | 2026-09-15 | A definition is decoded strictly: an unknown key, a duplicate key and a value of the wrong type are all errors |
| DEC-076 | 2026-09-15 | The json mode's "gjson-style path" is hand-written against encoding/json — dot-separated keys, an all-digit segment as an array index, \. and \\ esca… |
| DEC-077 | 2026-09-15 | maxHTMLDepth refuses an HTML response nested deeper than 512 elements before html.Parse is called, using a linear, allocation-free pre-scan that excl… |
| DEC-078 | 2026-09-16 | The scraper's HTML stack is pinned as the set goquery itself pairs: golang.org/x/net v0.58.0, github.com/PuerkitoBio/goquery v1.13.0 and github.com/a… |
| DEC-079 | 2026-09-16 | The loader reads *.yml only, in the definitions directory itself, and compares the extension case-insensitively on every platform. A sub-directory is… |
| DEC-080 | 2026-09-16 | Four cases the criteria do not mention resolve as follows. A missing definitions directory is not an error — it yields an empty set and one Debug lin… |
| DEC-081 | 2026-09-16 | internal/indexer/scraper's package doc no longer says the package "writes no log lines at all". The definition Loader logs, and it is the only thing… |
| DEC-082 | 2026-09-16 | T-024 bundles exactly one lawful default source, the Internet Archive, rather than the "two or three" the task's own acceptance text targets. Academi… |
| DEC-083 | 2026-09-16 | T-025's import supports only the framework's own YAML schema; no third-party definition format is mapped, even though the acceptance text allows it (… |
| DEC-085 | 2026-09-16 | T-040 adds go.etcd.io/bbolt v1.4.3 to go.mod — the persistence choice AGENT.md §3 already locked, just not yet a real dependency. Verified MIT agains… |
| DEC-086 | 2026-09-16 | T-042's single-instance lock is implemented as a real OS-level advisory file lock (internal/platform.TryLockFile: flock(2) on macOS/Linux via _darwin… |
| DEC-087 | 2026-09-16 | internal/platform/lock_windows.go's TryLockFile now locks a fixed sentinel byte range (offset 1 << 30, 1 byte) instead of offset 0 |
| DEC-088 | 2026-09-16 | T-050 adds github.com/charmbracelet/lipgloss v1.1.0 and github.com/rivo/uniseg v0.4.7 as real dependencies (AGENT.md §3 already locked both), plus gi… |
| DEC-089 | 2026-09-16 | internal/tui/theme's TERM/COLORTERM colour-degradation test is split into capability_posix_test.go (//go:build !windows) and capability_windows_test.… |
| DEC-090 | 2026-09-16 | T-051 adds github.com/charmbracelet/bubbletea v1.3.10 as a real dependency (AGENT.md §3 already locked it) and github.com/charmbracelet/x/exp/teatest… |
| DEC-091 | 2026-09-16 | Pinned github.com/mattn/go-localereader (a bubbletea transitive dependency, Windows-only TTY input path) to commit 2491eb6 instead of its only tagged… |
| DEC-092 | 2026-09-16 | T-052's acceptance text — "source-error indicator (2/4 sources failed) expandable with tab" — is implemented with tab scoped to a new modal Context (… |
| DEC-093 | 2026-09-16 | Theme gained one new field, Border, a lipgloss.Style carrying Border(lipgloss.RoundedBorder()) (or lipgloss.ASCIIBorder() when Capability.Unicode is… |
| DEC-094 | 2026-09-16 | T-055's Files line names internal/tui/capability.go, but the terminal-capability detector already lives at internal/tui/theme/capability.go (T-050, m… |
| DEC-095 | 2026-09-16 | tortui doctor's per-indexer reachability probe (internal/doctor.checkIndexers) reuses internal/indexer/httpx.Client directly — the same HTTP client t… |
| DEC-096 | 2026-09-16 | QA on T-055's PR root-caused that platform.RaiseFDLimit's own Setrlimit call is dead code in every real invocation: since Go 1.19, src/syscall/rlimit… |
| DEC-097 | 2026-09-16 | T-056's acceptance text describes two things that, read literally, presuppose screens/actions that don't exist yet: a fixture indexer "wired into the… |
| DEC-098 | 2026-09-17 | MPL-2.0 is admitted as a single, named exception to the dependency-license allowlist, for github.com/anacrolix/torrent alone — AGENT.md §3's Language… |
| DEC-099 | 2026-09-17 | QA remediation of PR #28 (T-942/DEC-098): the "MPL-2.0 for anacrolix/torrent alone" scope is now enforced by make licenses, not only stated in AGENT.… |
| DEC-101 | 2026-09-23 | go.uber.org/goleak (MIT) is added as a direct test-only dependency of internal/engine/anacrolix, to verify T-031's Close() acceptance criterion ("ide… |
| DEC-100 | 2026-09-18 | The MPL-2.0 exception is widened from one named module to a named set of ten, enforced the same way DEC-099 built: Makefile's ALLOWED_MPL_MODULE beco… |
| DEC-102 | 2026-09-23 | T-032 (Pause/Resume/Remove/Files) judgement calls. (1) engine.FileStatus (AGENT.md §5, frozen) carries no Priority field — only Path, SizeBytes, Down… |
| DEC-103 | 2026-09-25 | Owner-authorised rework of the task loop for pace (T-945): slim tracker/AGENT.md, tiers, PR-carried status, single-party gates, faster CI, autonomy |
| DEC-104 | 2026-09-25 | T-033: Updates sends on the sample tick only when the snapshot changed; ETA from a 10-sample rolling average; test-only injectable ticker |
| DEC-105 | 2026-09-25 | T-944: findOrTrack (one critical section) fixes concurrent Add; a beforeAttach test hook and untrackFailedSpec fix two pre-merge review findings in the fix itself; check-goos-scope gates runtime.GOOS |
| DEC-106 | 2026-09-25 | T-034: queue via optional engine.Queuer; space shortfall pauses as StateErrored; seed-policy stop shows StatePaused, Resume overrides; listen port 6881 with random fallback; Windows path limit is MAX_PATH |
| DEC-107 | 2026-09-25 | T-949: merge gate is macOS-first (macOS make check + build-all + licenses + hostname check required, Linux/Windows make check advisory); reviewers stop checking CI; re-review resumes the same reviewer; stalled agents get one auto-resume then a fresh agent; tests wait on a predicate/terminal state, not one exact intermediate state; Windows shellcheck installs from a pinned, checksum-verified GitHub release |
| DEC-108 | 2026-09-25 | T-041: session resume via optional engine.Resumer; persistent per-destination piece completion; unresumable torrents tracked as StateErrored (ErrDataMissing); lifecycle.Session saves only after Resume |
| DEC-109 | 2026-09-25 | T-060: internal/tui defines its own Searcher/HistoryStore interfaces, never a concrete Registry/Store; New() gains a variadic Option instead of new required params; esc-cancel/space-toggle handled as raw key checks, not declarative Bindings, to stay inside the search screen's help overlay's 24-line budget at the 80×24 floor; empty query dispatches Latest; L jumps to Results once the fetch resolves |
| DEC-111 | 2026-09-25 | T-062: components.Table gains Row.SortKey, Column.SortMissingLast, and Column.Accent (all generic, no Trust-specific logic); Trust's sort key comes from the real Trust value since Badge() renders Unknown/None identically; Unknown pins last in both sort directions; the "t" filter toggle re-derives rows from resultsModel.allRows |
| DEC-112 | 2026-09-25 | T-063: indexer.ExtraKeyFiles is a new well-known, optional Extra convention for the details screen's file list; internal/platform.OpenURL (open/xdg-open/rundll32, http(s)-only) backs `u`; results-screen j/k now move the table's own cursor instead of the unused generic m.selection |

## Blocked

*(empty — append `T-0NN` blocks here with the exact input needed to unblock)*

Resolved blockers are archived verbatim under **Blocked — Resolved** in `docs/tracker-archive.md`.
