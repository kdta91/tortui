# TASK_TRACKER.md — tortui

Single source of truth for build state. Read `AGENT.md` first.

## Protocol

- Status values: `todo` · `in-progress` · `blocked` · `done`
- **Status lives only in the task block.** Do not maintain a summary table; it will drift.
- Pick the lowest-numbered `todo` whose dependencies are all `done`. One task at a time.
- Flip to `in-progress` and commit the tracker to `main` *before* creating the branch.
- On completion: flip to `done`, fill `notes:` with anything a future reader needs, append to
  the Decision Log if you made a judgement call.
- Never delete a task. Superseded tasks become `done` with a note pointing at the replacement.
- New work discovered mid-task goes to **Backlog** as `T-9NN`, not into the current task.

---

## Phase 0 — Foundation

### T-001 · Repository bootstrap
```
status: done
depends: —
```
**Notes:** `go.mod` declares `github.com/kdta91/tortui`, `go 1.23`. `cmd/tortui/main.go` is
wiring-only (54 lines): parses `--version` and accepts (but does not yet consume) `--config`;
otherwise prints a stub line, since the composition root (`internal/app`) doesn't exist until
later phases. `main_test.go` covers `--version`, the default stub path, and an invalid-flag exit
code. `.golangci.yml` uses the v2 schema (`version: "2"`): `linters.enable` adds `revive`,
`bodyclose`, `contextcheck`, `errorlint` on top of the v2 defaults (`errcheck`, `govet`,
`staticcheck`, `ineffassign`, `unused`); `gofumpt`/`goimports` live under `formatters.enable`
since v2 moved formatters out of the linters list. `make check` = fmt-check + lint + test + vet,
all green locally. `make cover` computes total coverage and fails under a threshold (currently 0,
since the per-package thresholds in AGENT.md §9 apply to `internal/indexer`, `internal/engine`,
and `internal/tui`, none of which exist yet — raise the threshold as those packages land).
CI (`.github/workflows/ci.yml`) runs `make check` on `ubuntu-latest`, `macos-latest`, and
`windows-latest`, and is green on all three (see PR #1). golangci-lint is installed with
`go install .../v2/cmd/golangci-lint@v2.13.2` — the official `install.sh` was tried first but
its release-asset checksum verification failed against the currently published `v2.13.2` tarball
on every runner, so `go install` replaced it (DEC-019). GNU Make is installed via `choco` on the
Windows runner since it is not preinstalled there. A `.gitattributes` forcing `eol=lf` was needed
because Git for Windows' default `core.autocrlf=true` checkout turned the Go sources into CRLF,
which made `gofumpt` report them as unformatted on `windows-latest` only.
**Branch protection could not be configured**: `kdta91/tortui` is a private repository on a free
plan, and both the classic branch-protection API and the newer repository-rulesets API return
`403 Upgrade to GitHub Pro or make this repository public` (verified directly against both
endpoints). This needs either the repo made public or the account upgraded — flagged for the
orchestrator/user rather than silently skipped (DEC-020).
**Files:** `go.mod`, `Makefile`, `.gitignore`, `.golangci.yml`, `.github/workflows/ci.yml`, `cmd/tortui/main.go`

**Acceptance**
- `go.mod` declares module `github.com/kdta91/tortui` and Go 1.23+.
- All `make` targets from AGENT.md §8 exist and run.
- `golangci-lint` configured with at least: `errcheck`, `govet`, `staticcheck`, `revive`,
  `gofumpt`, `bodyclose`, `contextcheck`, `errorlint`.
- GitHub Actions runs `make check` on every push and PR, on a matrix of
  `ubuntu-latest`, `macos-latest`, and `windows-latest`; fails the run on any error.
- Branch protection on `main` requires the CI check to pass.
- `make build` produces `bin/tortui` which prints version and exits 0 on `--version`.

---

### T-002 · Configuration package
```
status: todo
depends: T-001
```
**Files:** `internal/config/`, `config.example.toml`

**Acceptance**
- Loads TOML from `$XDG_CONFIG_HOME/tortui/config.toml`, overridable with `--config`.
- Struct covers: `download_dir`, `saved_destinations` (list), `max_download_rate`,
  `max_upload_rate`, `max_active_downloads`, `max_peers`, `listen_port`, `seed_policy`,
  `seed_ratio`, `min_free_space`, `theme`, `ascii`, `search_timeout`, and an `[[indexer]]` array with `id`, `name`, `type`
  (`torznab` | `scraper`), `url`, `api_key`, `cookie`, `definition` (path), `enabled`.
- Validation returns a list of *all* problems, not the first one, each naming the offending key.
- Missing file → written defaults + a first-run flag surfaced to the app. **First run requires
  no input**: defaults are written, the download dir is created, bundled sources (T-024) are
  active, and search works before the user has typed anything. No blocking wizard.
- File written mode 0600 and **atomically** — temp file, fsync, rename (T-042). Warn if an
  existing config is world-readable.
- `config.example.toml` contains placeholder values only — no real hosts, no real keys.
- Path resolution per AGENT.md §14: `~/.config/tortui/` on macOS and Linux honouring
  `XDG_CONFIG_HOME`, `%AppData%\tortui\` on Windows (DEC-005).
- `TORTUI_HOME` env var, when set, overrides config, state, and download roots to live under
  that one directory. Required for sandboxed manual testing (AGENT.md §15).
- Default download dir resolved per-OS; created on first run if absent.
- Table-driven tests for valid, invalid, partial, and unknown-key configs, plus a
  `TORTUI_HOME` redirection test and per-OS path resolution tests using injected env.

---

### T-003 · File logging
```
status: todo
depends: T-001
```
**Files:** `internal/config/log.go` (or `internal/logging/`)

**Acceptance**
- `slog` handler writing to the per-OS state dir (`~/.local/state/tortui/` on Linux,
  `~/.config/tortui/` on macOS, `%LocalAppData%\tortui\` on Windows), level from config/env.
- Rotation at 10 MB, 3 files retained.
- **Nothing is ever written to stdout or stderr after TUI start.** A test asserts both are
  empty across a simulated run.
- `--log-level` and `--log-file` flags override config.
- Indexer URLs, API keys, and cookies are masked in every log line.

---

### T-004 · Secret hygiene
```
status: todo
depends: T-001
```
**Files:** `.gitleaks.toml`, `scripts/pre-commit`, `Makefile`

**Acceptance**
- Pre-commit hook blocks commits containing API-key-shaped strings, cookies, or
  `config.toml` itself.
- `make check` includes the scan.
- Documented in `CONTRIBUTING.md`.

---

### T-005 · Cross-platform tooling baseline
```
status: todo
depends: T-001
```
**Files:** `Makefile`, `scripts/`, `.github/workflows/ci.yml`

**Acceptance**
- `Makefile` sets `SHELL := /bin/sh` and `.SHELLFLAGS := -eu -c`; uses no GNU make 3.82+ or
  4.0+ features (macOS ships 3.81). Targets run unmodified on Windows under Git Bash.
- Every script and git hook is `#!/usr/bin/env sh`, POSIX only — **no bash 4 syntax anywhere**,
  because macOS ships bash 3.2 (AGENT.md §14).
- `scripts/` passes `shellcheck -s sh`; the check is wired into `make lint`.
- `make build-all` cross-compiles darwin/arm64, darwin/amd64, linux/amd64, linux/arm64,
  windows/amd64, windows/arm64 and fails the target if any combination does not compile.
- CI builds all six targets on every push, and runs the full test suite natively on
  `ubuntu-latest`, `macos-latest`, and `windows-latest` — Windows is tier 1, so a
  Windows-only test failure blocks the merge like any other.
- No test in the tree depends on case-sensitive filenames (APFS and NTFS are both
  case-insensitive by default) or on `/`-joined paths.

---

### T-006 · Licensing
```
status: todo
depends: T-001
```
**Files:** `LICENSE`, `NOTICE`, `.github/workflows/ci.yml`

**Acceptance**
- `LICENSE` at repo root — MIT, correct copyright holder and year.
- `NOTICE` enumerates every direct and transitive dependency with its license, generated by
  `go-licenses` rather than hand-written.
- `make licenses` regenerates `NOTICE`; CI fails if it is stale or if any dependency is
  GPL/AGPL/LGPL or unlicensed (AGENT.md §3).
- README license section matches the actual `LICENSE`.

---

### T-007 · Contribution policy and repo hygiene
```
status: todo
depends: T-006
```
**Files:** `CONTRIBUTING.md`, `.github/ISSUE_TEMPLATE/`, `.github/PULL_REQUEST_TEMPLATE.md`, `CODE_OF_CONDUCT.md`

**Acceptance**
- `CONTRIBUTING.md` states plainly that **pull requests adding indexer definitions, endpoints,
  or default sources for specific sites will be closed without review**, and explains why
  (AGENT.md §16). Users add their own definitions locally; the project distributes none.
- Bug report template asks for `tortui doctor` output and the terminal name, and instructs the
  reporter **not** to paste indexer URLs, keys, cookies, or torrent titles.
- Issue and PR templates note that site names do not belong in issue text.
- PR checklist: task ID, `make check` green, tests added, docs updated, no named sites.
- A CI check scans the diff for anything resembling a real indexer hostname and fails the PR,
  with an allowlist covering only the bundled lawful sources from T-024 (AGENT.md §2, §16).

---

## Phase 1 — Domain contracts

### T-010 · Indexer contracts
```
status: todo
depends: T-002
```
**Files:** `internal/indexer/indexer.go`

**Acceptance**
- `Indexer`, `Query`, `Result`, `Caps`, `Trust`, `Mode` exactly as specified in AGENT.md §5.
- `Query.Mode` distinguishes keyword search from a latest/recent-additions feed; `Caps.Latest`
  declares whether a source can serve one. Mode is explicit rather than inferred from an empty
  `Text`, so a source that cannot browse fails loudly at the registry instead of silently
  returning nothing.
- `Trust` implements `String()` and a `Badge()` returning `VIP` / `TR` / `✓` / `""`.
- `Result.Validate()` rejects a result with neither `Magnet` nor `TorrentURL`.
- Godoc on every exported identifier explaining what an adapter must guarantee.
- **After this task these types are frozen.** Changes require a `DEC-` entry.

---

### T-011 · Category taxonomy
```
status: todo
depends: T-010
```
**Files:** `internal/indexer/category.go`

**Acceptance**
- Small internal enum (`CategoryOther` as the zero value, plus a handful of broad buckets).
- Helpers to map from Torznab numeric IDs and from arbitrary adapter strings.
- Unknown input maps to `CategoryOther` and is never dropped or errored.
- No content-specific or scene-specific categories (AGENT.md §2).

---

### T-012 · Registry and fan-out search
```
status: todo
depends: T-010, T-011
```
**Files:** `internal/indexer/registry.go`

**Acceptance**
- `Register(Indexer)` / `Get(id)` / `List()` / `Enabled()`.
- `SearchAll(ctx, q, ids...)` queries selected sources concurrently, in either mode.
- For `ModeLatest`, sources without `Caps.Latest` are skipped and reported as skipped — not as
  errors, and never counted toward the all-sources-failed condition.
- Short-TTL result cache (default 60s) keyed on mode, query, categories, and source, plus a
  per-source minimum refresh interval. A user mashing `R` re-renders from cache rather than
  re-fetching (AGENT.md §6.11). Cache is in-memory only and never persisted.
- Per-indexer timeout from config (default 15s). A slow source is cancelled, not waited on.
- Returns `([]Result, []SourceError, error)`. The `error` is non-nil **only** when every
  source failed. Partial success returns results plus the per-source errors.
- Deduplication by `InfoHash` when present, else normalised title + size; the surviving copy
  keeps the highest seeder count and records all contributing `IndexerID`s in `Extra`.
- Default ordering depends on mode: seeders descending for `ModeSearch`, published date
  descending for `ModeLatest`.
- Tests use fake indexers covering: all succeed, one times out, one errors, all fail,
  duplicate infohashes across sources, zero results.

---

## Phase 2 — Indexer adapters

### T-020 · Shared HTTP client
```
status: todo
depends: T-012
```
**Files:** `internal/indexer/httpx/`

**Acceptance**
- Configurable user-agent, connect/read timeouts, and a per-host rate limiter.
- Retry with exponential backoff on 429/5xx only; never on 4xx. Honours `Retry-After`.
- Injects user-supplied `api_key` / `cookie` from config when present. **No credential
  discovery, no session harvesting, no captcha handling** (AGENT.md §2).
- Response body size cap (default 8 MB) to avoid unbounded reads.
- Tests against `httptest.Server` for each behaviour including the backoff path.

---

### T-021 · Torznab adapter
```
status: todo
depends: T-020
```
**Files:** `internal/indexer/torznab/`

**Acceptance**
- Implements `Indexer` against the Torznab/Newznab XML API.
- Parses `caps` endpoint into `Caps`; handles servers that omit it.
- `ModeLatest` uses the recent-additions feed (an empty-keyword search on most Torznab
  servers). Probe this during `caps` handling and set `Caps.Latest` from what the server
  actually supports rather than assuming — implementations vary.
- Maps `seeders`, `peers`, `size`, `pubDate`, `category`, `magneturl`, `infohash` attributes.
- Derives `Trust` from any uploader/verified attribute the server exposes; `TrustUnknown`
  when absent.
- `Resolve` is a no-op when a magnet is already present.
- Handles malformed XML, HTTP errors, and empty result sets without panicking.
- Fixture-driven tests from `testdata/torznab/*.xml` — no network.

**Why first:** a Torznab client makes the app compatible with any Prowlarr/Jackett instance the
user already runs, which delivers source-agnosticism immediately without writing scrapers.

---

### T-022 · Scraper adapter framework
```
status: todo
depends: T-020
```
**Files:** `internal/indexer/scraper/`, `docs/indexer-definitions.md`

**Acceptance**
- A source is defined entirely by a **user-supplied YAML file** — no selectors in Go code.
- Definition schema covers: `id`, `name`, `base_url`, `search.path`, `search.params`,
  `rows` selector, and per-field selectors with `attr`/`text`/`regex`/`transform` for each
  `Result` field, plus a `trust` mapping block (selector value → `Trust` enum).
- An optional `latest` block mirrors `search` with its own path and params, for sites whose
  recent-additions page differs from their results page. Field selectors are shared by default
  and overridable per block. A definition omitting `latest` reports `Caps.Latest = false`.
- Supports HTML (goquery) and JSON (gjson-style path) response modes.
- Definition validation produces actionable errors naming the failing field and selector.
- A missing optional selector yields a zero value, never an error.
- `docs/indexer-definitions.md` documents the schema with a complete worked example.
- Tests run against a checked-in fixture page served by `httptest` plus a sample definition.
- Definitions for bundled lawful sources land in T-024, not here. This task ships the framework
  and the test fixture definition only.

---

### T-023 · Definition loading and hot-reload
```
status: todo
depends: T-022
```
**Files:** `internal/indexer/scraper/loader.go`

**Acceptance**
- Loads all `*.yml` from `$XDG_CONFIG_HOME/tortui/definitions/`.
- Bad definitions are skipped with a logged error; one broken file never blocks startup.
- `Reload()` re-reads from disk and swaps definitions atomically, exposed in Settings.
- Test covers: valid dir, empty dir, one malformed file among valid ones.

---

### T-024 · Bundled lawful default sources
```
status: todo
depends: T-022, T-023
```
**Files:** `internal/indexer/scraper/builtin/`, `docs/bundled-sources.md`

**Acceptance**
- Ships definitions for a small set of sources that **officially distribute their own content
  over BitTorrent** — public archives, research dataset repositories, distro release listings.
  Candidates to evaluate: the Internet Archive and Academic Torrents. Two or three working
  sources is the target, not breadth.
- **Verify each source's search interface against its official documentation before writing the
  definition.** Do not infer endpoint paths, parameters, or response fields. If a candidate has
  no documented search interface or its terms disallow automated querying, drop it and note why
  in the decision log — do not substitute a guess.
- **Compiled into the binary with `go:embed`**, not shipped as loose files. Installing the
  binary is the whole install — there is no definitions directory to create, no archive to
  unpack, no companion files to keep next to the executable.
- Enabled on first run with no prompt, no wizard, and no configuration step, so **a fresh
  install can search and download immediately on a machine with nothing else installed**. This
  is the point of the task and the standalone contract in AGENT.md §1 depends on it.
- A user-supplied definition in the definitions directory with the same `id` **overrides** the
  embedded one. This is how a broken selector gets repaired without waiting for a release —
  document it in `docs/bundled-sources.md`.
- `t` (T-081) must pass against every bundled source in CI's integration job, so a silently
  rotted default is caught by the build rather than by a user's first search.
- Each is an ordinary scraper definition using the T-022 framework, with no special-casing
  anywhere in the codebase. The user can disable or remove any of them like any other source.
- Every bundled source supports **both** modes. A default that can only be keyword-searched
  leaves a fresh install with an empty first screen, which defeats T-024's purpose.
- `docs/bundled-sources.md` lists each one, what it covers, which modes it supports, and why it
  qualifies under AGENT.md §2.
- Fixture-driven tests; the live-endpoint test is `//go:build integration`.
- Bundling any source whose primary use is distributing infringing content fails this task
  regardless of popularity (AGENT.md §2, §16).

---

### T-025 · Import an existing definition
```
status: todo
depends: T-023
```
**Acceptance**
- Settings can install a definition from a local file path or a URL the user provides,
  validating it and reporting actionable errors before it is saved.
- Supports the framework's own schema. If a widely-used third-party definition format is
  straightforward to map, support importing it too — this is what lets a user add a source by
  obtaining a file rather than authoring one, which is the difference between a five-minute
  task and an afternoon.
- A definition that fails validation is rejected with the failing field and selector named, and
  nothing is written.
- tortui **fetches only what the user explicitly points it at**. No definition repository, no
  index of available definitions, no auto-discovery, no update feed.

---

## Phase 3 — Torrent engine

### T-030 · Engine contracts and fake
```
status: todo
depends: T-002
```
**Files:** `internal/engine/engine.go`, `internal/engine/fake/`

**Acceptance**
- `Engine`, `TorrentStatus`, `State`, `AddSource`, `FileStatus`, `Origin` per AGENT.md §5.
- `fake` drives scripted progress on a controllable clock: downloads, stalls, errors, completes.
- `fake` satisfies the full interface and is used by every TUI test thereafter.
- Frozen after this task.

---

### T-031 · anacrolix engine — add and list
```
status: todo
depends: T-030
```
**Files:** `internal/engine/anacrolix/`

**Acceptance**
- Accepts magnet URI, `.torrent` URL, and local `.torrent` path via `AddSource`.
- **anacrolix logging is redirected into the slog file sink before any torrent is added.**
  A test asserts stderr stays empty. (AGENT.md §13 — do this here, not later.)
- Download dir, max peers, and rate limits applied from config.
- `Add` returns as soon as the source is accepted; metadata fetch is async and the torrent sits
  in `StateChecking` until the info dict arrives.
- Metadata fetch timeout (default 60s) transitions to `StateErrored` with a readable message.
- `List()` maps engine internals onto `TorrentStatus` with correct progress and rates.
- `Close()` is idempotent and leaves no goroutines (verified with `goleak`).

---

### T-032 · Engine lifecycle operations
```
status: todo
depends: T-031
```
**Acceptance**
- `Pause`, `Resume`, `Remove(id, deleteData)` implemented.
- `Remove` with `deleteData=true` deletes only inside the **known destination roots** — the
  default dir plus every saved and in-use destination (AGENT.md §6.12). A path outside them is
  refused with an error and logged. Tests cover traversal, a symlink pointing outside a root,
  and a torrent whose recorded destination has since been removed from the set.
- Operating on an unknown ID returns a typed `ErrNotFound`, never a panic.
- `Files(id)` returns per-file name, size, progress, and priority.

---

### T-033 · Update stream
```
status: todo
depends: T-031
```
**Acceptance**
- `Updates()` emits a full `[]TorrentStatus` snapshot at ~2 Hz, coalesced — no per-torrent spam.
- Channel is buffered; a slow consumer drops stale snapshots rather than blocking the engine.
- ETA computed from a rolling rate average, `-1` when indeterminate.
- Closing the engine closes the channel exactly once.
- Test asserts cadence, coalescing, and drop-on-slow-consumer behaviour.

---

### T-034 · Download policy and path safety
```
status: todo
depends: T-031, T-032
```
**Files:** `internal/engine/anacrolix/`, `internal/engine/paths.go`

**Acceptance**
- **Path sanitization.** Every file path declared by a torrent is cleaned and verified to resolve
  inside its destination before any write. Reject `..` traversal, absolute paths, NUL bytes,
  Windows reserved names (`CON`, `NUL`, `AUX`, `COM1`…), trailing dots and spaces, and paths
  exceeding the platform limit. A torrent with an unsafe path is refused with a clear reason,
  not silently rewritten. **Tests must include a crafted malicious `.torrent` fixture for each
  category** (AGENT.md §6.11, §13).
- **Free-space precheck.** Refuse to add when the destination has less free space than the
  torrent needs plus a configurable margin, and say how much is short. Re-check periodically
  during download and pause with a clear message rather than filling the disk.
- **Queueing.** `max_active_downloads` (default 3). Torrents beyond it sit in `StateQueued` and
  start automatically as slots free. Queue order is user-visible and reorderable.
- **Seeding policy.** Configurable: seed until ratio, seed for a duration, or stop at
  completion. Default is a modest ratio with the policy stated in the UI, never silent
  indefinite upload (AGENT.md §13).
- **Listen port** configurable, with a sensible default and a random fallback if taken. `doctor`
  reports the bound port.
- Every knob here is editable in Settings (T-082), not config-file-only.

---

## Phase 4 — Persistence

### T-040 · bbolt store
```
status: todo
depends: T-002
```
**Files:** `internal/store/`

**Acceptance**
- Buckets: `torrents` (id → origin, added-at, save path, source URL), `history` (recent queries),
  `prefs` (sort column, last screen, selected sources).
- Writes debounced 5s on a dedicated goroutine (AGENT.md §13).
- Schema version key with a migration hook; unknown future version refuses to open rather than
  corrupting.
- Concurrent read/write test with `-race`.

---

### T-041 · Session resume
```
status: todo
depends: T-040, T-032
```
**Acceptance**
- On startup, torrents recorded in the store are re-added to the engine and resume from
  existing data without re-downloading completed pieces.
- A torrent whose data is missing on disk is surfaced as `StateErrored` with a clear message
  and an offer to remove the entry — it is not silently dropped.
- `Origin` (indexer ID + source URL) survives restart so the downloads screen can still show
  the source.

---

### T-042 · Single-instance lock and data integrity
```
status: todo
depends: T-040
```
**Acceptance**
- Lock file in the state dir taken at startup. A second instance exits immediately with a clear
  message naming the running process, rather than two engines fighting over the same store and
  download directory (AGENT.md §13).
- Stale locks from a crashed process are detected and cleared, not inherited.
- **Config writes are atomic**: write to a temp file in the same directory, fsync, rename. A
  crash mid-save must never leave a truncated or empty config.
- A corrupt or unreadable store is renamed aside with a timestamp and recreated empty, with the
  user told what happened and where the old file went. Never block startup on it.
- **Graceful shutdown sequence**, on quit and on `SIGINT`/`SIGTERM`: stop accepting input,
  pause torrents, flush the store, close the engine, restore the terminal — each step with a
  timeout so a hung engine cannot strand the user in raw mode. Terminal restore runs last and
  runs unconditionally, including on panic.
- Tests: concurrent start attempt, stale lock recovery, simulated crash during config write,
  corrupt store file, and shutdown-with-hung-engine.

---

## Phase 5 — TUI shell

### T-050 · Theme
```
status: todo
depends: T-001
```
**Files:** `internal/tui/theme/`

**Acceptance**
- One accent colour plus foreground/muted/dim/error/success. No second accent.
- Truecolor → 256 → 16 degradation via lipgloss profile detection.
- `NO_COLOR` produces a fully monochrome render; golden test asserts zero escape codes.
- `CLICOLOR_FORCE` honoured. `TERM=dumb` or non-TTY stdout → no TUI, one-line message, exit 1.
- Glyph sets: Unicode (`█░✓`) and ASCII fallback (`#-+`), selected by capability and forceable
  with `--ascii` / `ascii = true`.
- All string widths measured with `rivo/uniseg`. A golden test renders a results row containing
  CJK text and an emoji and asserts column alignment holds.
- At least two built-in themes selectable from config.

---

### T-051 · Root model and routing
```
status: todo
depends: T-050, T-012, T-030
```
**Files:** `internal/tui/root.go`, `internal/tui/keymap.go`

**Acceptance**
- Screen routing across the five screens; `tab`/`shift+tab` and `1`–`5` per AGENT.md §7.
- Global keymap in one place, rendered by a `?` help overlay generated from that keymap
  (no hand-written help text that can drift).
- **A test asserts no key is bound to two actions within the same screen**, including modal
  contexts. Conflicts are found by the build, not by a user pressing `x` and getting the wrong
  dialog.
- `tea.WindowSizeMsg` recomputes layout for every child; no cached widths survive a resize.
- Quit prompts for confirmation when any torrent is active.
- `teatest` covers navigation between all screens and the help overlay.

---

### T-052 · Status bar
```
status: todo
depends: T-051
```
**Acceptance**
- Single line: current screen, active download count, aggregate down/up rate, source-error
  indicator (`2/4 sources failed`) expandable with `tab`.
- Transient messages with a 4s timeout, queued rather than overwritten.
- Truncates gracefully at 80 columns.

---

### T-053 · Responsive table component
```
status: todo
depends: T-051
```
**Acceptance**
- Column set with flex/fixed widths and a documented drop order
  (Source → Age → Trust) as width shrinks.
- Sort by any column, ascending/descending, indicator in the header.
- Keyboard scrolling with viewport, selection preserved across re-sorts.
- Renders correctly at 80×24, 120×40, and 60×20; golden files for each.

---

### T-054 · Modals and confirm dialog
```
status: todo
depends: T-051
```
**Acceptance**
- Generic confirm dialog with configurable options and a default.
- Never more than one modal deep — opening a second is a programming error that returns a
  logged no-op, not a stack.
- `esc` always cancels; focus returns to the originating screen and selection.

---

### T-055 · Terminal capability detection and `doctor`
```
status: todo
depends: T-050, T-002
```
**Files:** `internal/tui/capability.go`, `cmd/tortui/doctor.go`

**Acceptance**
- Detects colour profile, Unicode/glyph support, terminal size, tmux/screen wrapping, and
  whether stdout is a TTY.
- `tortui doctor` prints OS/arch, `TERM`, `COLORTERM`, detected profile, Unicode verdict,
  terminal size, resolved config/state/download paths, file-descriptor soft and hard limits,
  and every configured indexer with a reachability verdict. Exits without starting the TUI.
- Output is plain text, safe to pipe and paste into a bug report. Credentials are masked.
- Non-zero exit when a hard problem is found (unwritable download dir, `TERM=dumb`).
- On darwin, startup raises the soft FD limit to the hard limit and `doctor` reports both
  the original and the raised value (AGENT.md §13).

---

### T-056 · Demo mode
```
status: todo
depends: T-051, T-030
```
**Files:** `internal/app/demo.go`, `internal/indexer/fake/`

**Acceptance**
- `tortui --demo` wires `engine/fake` plus a fixture-backed fake indexer into the real TUI.
- Canned search results span every `Trust` value, wide CJK and emoji titles, huge and tiny
  sizes, zero-seeder entries, and a source that deliberately fails.
- Simulated downloads exercise: normal progress to completion, a stall, a metadata timeout,
  and an error state — on a controllable clock so the whole cycle runs in under a minute.
- Zero network. Zero writes outside a temp dir. Nothing to clean up afterwards.
- Every screen, keybind, and dialog is reachable in demo mode, including open-file and
  open-folder against dummy files in the temp dir.
- A banner makes it unmistakable that this is demo data.

**Why this exists:** it is the primary way to verify rendering after a build, and the only way
the agent can self-check the UI without a live swarm (AGENT.md §15).

---

## Phase 6 — Search and results

### T-060 · Search screen
```
status: todo
depends: T-051, T-012
```
**Acceptance**
- Text input, mode selector (Search / Latest), multi-select of enabled sources, optional
  category and min-seeders filter.
- `enter` dispatches a `tea.Cmd` — `Update` never blocks (AGENT.md §6.1).
- In-flight query shows a spinner and is cancellable with `esc`.
- Recent queries from the store offered as suggestions.
- **`enter` on an empty query runs Latest** rather than doing nothing — an empty box is a
  request to see what's there, not a mistake to scold.
- `L` from anywhere runs Latest against the currently selected sources and jumps to results.
- Sources that cannot serve the selected mode are shown greyed in the multi-select with the
  reason, so the user understands why a source is missing from the results.

---

### T-061 · Results screen
```
status: todo
depends: T-060, T-053
```
**Acceptance**
- Columns exactly `Title · Size · S/L · Trust · Age · Source`. Default sort follows the mode:
  seeders desc for Search, age ascending (newest first) for Latest.
- Header states the current mode and query, so a Latest view is never mistaken for a stale
  search result.
- `R` refreshes, honouring the T-012 cache and per-source minimum interval; the status bar shows
  when results came from cache rather than a fresh fetch.
- Sizes human-readable; ages relative (`3h`, `2d`, `1y`).
- Per-source failures shown in the status bar without hiding successful results.
- Zero results shows an explicit empty state naming which sources were queried, and in Search
  mode offers Latest as a next step.
- `teatest` covers render, sort cycling, and the partial-failure case.

---

### T-062 · Trust badges and filtering
```
status: todo
depends: T-061
```
**Acceptance**
- `Trust` rendered as a compact badge (`VIP` / `TR` / `✓` / blank), accent-coloured, and
  legible with `NO_COLOR`.
- Sortable by trust; a filter toggle restricts to `TrustTrusted` and above.
- `TrustUnknown` sorts last and never displays as a false negative.
- Badge semantics documented in `README.md`: this is uploader reputation metadata reported by
  the source, it is not a quality guarantee and it does not affect download behaviour.

---

### T-063 · Details screen
```
status: todo
depends: T-061
```
**Acceptance**
- Full title, size, category, uploader, trust, published date, source URL, infohash.
- File list when the indexer provides one, otherwise an explicit "not available from this
  source" state.
- `u` opens the source page in the system browser via `internal/platform`.
- `enter` adds the torrent and switches to the downloads screen.

---

## Phase 7 — Downloads

### T-070 · Add flow
```
status: todo
depends: T-063, T-031
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
```
**Acceptance**
- Edit default download dir (with existence, writability, and free-space validation), manage
  the **saved destinations list** (add/rename/remove, most-recent-first), rate limits, max
  active downloads, max peers, listen port, seeding policy and ratio, minimum free space,
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

---

## Decision Log

| ID | Date | Decision | Rationale | Affects |
|---|---|---|---|---|
| DEC-001 | — | Go + bubbletea + anacrolix/torrent | Single binary, no external daemon, fast autonomous iteration loop | all |
| DEC-002 | — | Torznab adapter before scrapers | Instant compatibility with any Prowlarr/Jackett the user already runs | T-021 |
| DEC-003 | — | Scraper sources are YAML definitions, not compiled selectors | Selectors rot; a user can fix a text file without waiting for a release. Superseded in part by DEC-013 on which definitions ship in-tree | T-022, T-024 |
| DEC-004 | — | `Trust` is display metadata only | Uploader badges are reputation signals, never an entitlement or access path | T-062 |
| DEC-005 | — | macOS config at `~/.config/tortui`, not `~/Library/Application Support` | CLI-tool convention; users expect dotfile-adjacent config they can version-control | T-002 |
| DEC-006 | — | All scripts POSIX `sh`, make targets GNU make 3.81-compatible | macOS ships bash 3.2 and make 3.81; bash-4 syntax passes in the agent's container and fails on the user's Mac | T-005 |
| DEC-007 | — | Terminal.app is the compatibility floor | No truecolor, limited glyphs — anything that renders there renders everywhere | T-050, T-094 |
| DEC-008 | — | `--demo` mode is a first-class feature, not a test fixture | Only practical way to verify TUI rendering without a live swarm; also the best bug-report reproducer | T-056 |
| DEC-009 | — | MIT license; dependencies restricted to MIT/Apache-2.0/BSD/ISC | Keeps `NOTICE` accurate and the license clean; enforced by `go-licenses` in CI | T-006 |
| DEC-010 | — | No *infringement-oriented* site is named anywhere in the repo, including tests and fixtures | Named references in unit tests were a central hook in the 2020 youtube-dl takedown. Scoped to that category rather than all sites — see DEC-013 | T-007, all adapters |
| DEC-011 | — | Distribution via GitHub Releases, a Homebrew tap, a Scoop bucket, WinGet, and `go install` | All free and automated from one tag; Homebrew core has notability requirements a tap does not | T-092 |
| DEC-012 | — | v1 ships unsigned with documented OS warnings | Notarisation and Windows signing are the only parts of the pipeline that cost money; Sigstore and GitHub attestations give free provenance instead | T-092, T-908, T-909 |
| DEC-013 | — | Ship definitions for a few unambiguously lawful sources, enabled by default | The original blanket ban made a fresh install search nothing, forcing every user to run Prowlarr or author YAML. Bundling lawful sources costs nothing legally and is affirmative evidence of substantial non-infringing use | T-024 |
| DEC-014 | — | No DHT crawling and no self-built index, ever | Operating an index is a categorically different legal posture from querying sources the user chose; closing this off explicitly so it is never proposed as a search improvement | AGENT.md §2 |
| DEC-015 | — | tortui depends on no external software at runtime; bundled definitions are `go:embed`-ed | "Install and it works" is a product requirement, not a convenience. An indexer proxy as a prerequisite would contradict the single-binary premise | T-024, T-091 |
| DEC-016 | — | Latest is an explicit `Query.Mode`, not an empty-string search | Inferring browse from an empty keyword hides the capability difference between sources and fails silently on ones that can't do it | T-010, T-012 |
| DEC-017 | — | Destination is per-torrent, chosen before the add completes | Asking after the fact means moving data or re-downloading; the picker is cheap and the alternative is not | T-074 |
| DEC-018 | — | Path containment checks resolve against a set of known destination roots | Per-torrent destinations make "the download dir" the wrong boundary; a single-dir check would either block legitimate paths or be quietly widened until it checks nothing | T-032, T-073, T-074 |
| DEC-019 | 2026-09-12 | CI installs golangci-lint via `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2`, and installs GNU Make via `choco` on the `windows-latest` runner | `windows-latest` does not ship GNU Make by default (confirmed against the runner-images software inventory). golangci-lint's own `install.sh` was tried first but its checksum verification failed against the published `v2.13.2` release asset on all three runners; `go install` against the pinned module version sidesteps that second verification path entirely | T-001 |
| DEC-020 | 2026-09-12 | Branch protection on `main` requiring the CI check is left unconfigured, flagged as blocked rather than silently skipped | `kdta91/tortui` is a private repo on a free GitHub plan; both the classic branch-protection API and the repository-rulesets API return `403 Upgrade to GitHub Pro or make this repository public` for this repo. Unblocking needs either the repo made public or the account upgraded to GitHub Pro/Team — a decision for the human owner, not the agent | T-001 |

Append a row whenever you make a choice a future reader would question. Empty date means
inherited from the initial plan.

---

## Blocked

*(empty — append `T-0NN` blocks here with the exact input needed to unblock)*
