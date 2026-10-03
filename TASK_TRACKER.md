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

**Done (archived in `docs/tracker-archive.md`):** `T-070` Add flow · `T-071` Downloads screen · `T-072` Download actions · `T-073` Open file and folder · `T-074` Download destination selection.

---

## Phase 8 — Settings

**Done (archived in `docs/tracker-archive.md`):** `T-080` Indexer management · `T-081` Connection test · `T-082` Preferences · `T-083` Bulk import from a Torznab aggregator.

---

## Phase 9 — Release readiness

**Done (archived in `docs/tracker-archive.md`):** `T-090` Documentation and first run · `T-091` Integration suite · `T-092` Build and release · `T-093` Final hardening pass · `T-095` Composition root · `T-096` Settings wiring · `T-097` Windows engine storage handles · `T-094` Terminal compatibility matrix.

---

## Phase 10 — Release hardening follow-ups

**Done (archived in `docs/tracker-archive.md`):** `T-993` Engine destination-roots test · `T-994` Serialise session saves · `T-9008` Pin session-save wiring and shutdown window · `T-9010` Internet Archive results carry the .torrent URL · `T-9011` Open on Search, no startup Latest · `T-9012` Unknown seeders render as a dash · `T-9019` Shell-style path completion in the import field · `T-9024` Import-field tab follow-ups · `T-9021` Unknown seeders pass the minimum-seeders filter · `T-9025` Race-detector CI job · `T-9026` Hostname scanner ignores capitalised selector names · `T-9031` Scraper sources don't require the URL field · `T-9034` Clear recent searches; source-toggle hint · `T-9037` Scraper details-page resolve · `T-9041` Details-page address refuses userinfo · `T-9045` Userinfo hardening: base_url, redirects, torznab links · `T-9046` Same-origin userinfo redirects · `T-9049` Unparseable redirect Location never echoed · `T-9052` Scraper form: save runs a pending import · `T-9056` Add by one link; status-bar message stays visible · `T-9057` URL-added downloads never show their address · `T-9061` UI fixes from the README audit · `T-9069` Bundled sources are visible in Settings and `doctor` · `T-9073` README audit fixes · `T-9079` A .torrent link that redirects to a magnet adds by the magnet · `T-9082` v1.0.0 release records · `T-9090` CI runner and action versions · `T-9094` Every magnet drops its xs= and as= addresses · `T-9095` Credential hygiene: magnet redirects, userinfo schemes, unparseable names · `T-9099` SafeName hidden-prefix forms; credentialed requests keep the strict redirect rule · `T-9100` Styled-line truncation; tab bar and Settings list clip · `T-9101` Built-in source rows follow saves; Search empty state; one definition resolver for doctor · `T-9106` One Enter runs the Search; Trust sort is a total order · `T-9111` Settings and Search follow-ups from the T-9101 and T-9100 reviews · `T-9112` Downloads remembers its scroll offset; the block snap is tested · `T-9114` Doc and comment nits · `T-9118` httpx error tidy: one prefix, the right hop, fast 500 test · `T-9120` Test hardening: weak assertions, Downloads cases, retry-limit error text · `T-9121` Engine races, refused re-adds, duplicate resume records, storage lock, first-record magnet · `T-9127` Storage discard keeps others' data; dropped resume records and refused re-adds tell the user.

---

## v1.0 release criteria

Every one of these must hold before tagging `v1.0.0`. This is the finish line — the agent stops
when it reaches it and does not start backlog items on its own.

- [x] All tasks T-001 through T-097, plus T-993, T-994, T-9008, T-9010, T-9011, T-9012, T-9019, T-9021, T-9024, T-9025, T-9026, T-9031, T-9034, T-9037, T-9041, T-9045, T-9046, T-9049, T-9052, T-9056, T-9057, T-9061, T-9069, T-9073, T-9079, T-9082 and T-9090, are `done`.
- [x] `make check` and `go test -race ./...` green on Linux, macOS, and Windows CI. The race jobs
      (T-9025) are advisory per PR under DEC-107 but must all be green before the release tag.
- [x] Coverage thresholds from AGENT.md §9 met.
- [x] `govulncheck` clean; `NOTICE` current; no GPL/AGPL dependency.
- [x] Terminal matrix (T-094) passes on macOS for v1.0 (DEC-132 — owner has no Windows/Linux
      device; Windows Terminal/conhost/Linux emulator/PowerShell deferred to Backlog T-9004).
- [x] A real download completes, resumes across a restart, and removes cleanly on macOS for
      v1.0 (DEC-132 — Linux/Windows deferred to Backlog T-9004).
- [x] No infringement-oriented site is named anywhere in the repository (AGENT.md §2, §16).
      Verified by grep against the T-024 allowlist.
- [x] A fresh install searches and downloads successfully with no configuration, no account,
      and no other software installed — verified on macOS for v1.0 (T-091; DEC-132 —
      Linux/Windows deferred to Backlog T-9004).
- [x] A search works on first launch with zero config, against every bundled source.
- [x] Malicious-path `.torrent` fixtures are refused on all three OSes (T-034).
- [x] A second instance refuses to start; a crash mid-config-write loses nothing (T-042).
- [x] Quit and `SIGINT` both restore the terminal cleanly with downloads active (T-042).
- [x] Per-torrent destinations survive a restart and are honoured by open, reveal, and
      remove-with-data (T-074).
- [x] No documented path to a core capability (search, add, download, open, remove) requires
      installing anything besides tortui.
- [x] README accurate against shipped behaviour; `--demo` works on a clean install.

---

**v1.0.0 was tagged by the owner on 2026-10-02 at d18c06e; release run 36975766756 succeeded
(goreleaser, cosign, build attestation).** Verified: CI (macOS, Linux, Windows `make check` and
`-race` green on main), the local release check (`make cover` floors, `govulncheck` 0 reachable with
the 3 module-level findings being T-093's triaged set, `make licenses` with NOTICE unchanged, a
full-history hostname scan clean), and the owner's macOS checks (a real download completes, resumes
after `q` and after Ctrl-C with the terminal restored, and is removed with its data; a fresh
`TORTUI_HOME` searches and downloads with zero config; `--demo` matches the README). The DEC-132
deferrals stay deferred to Backlog T-9004.

---

## Backlog (not scheduled)

- `T-9004` Manual terminal matrix + real-download/fresh-install checks on Windows and Linux
  (deferred by DEC-132 — the owner has no Windows or Linux device for v1.0). Covers the
  Windows Terminal, conhost, Linux-emulator, and PowerShell rows in `docs/terminal-matrix.md`
  and the Windows/Linux halves of the v1.0 release criteria's manual download-resume-remove and
  fresh-install bullets. CI and cross-builds on all three OSes are unaffected.
- `T-991` `make cover` enforces AGENT.md §9's per-package floors (T-093) but no CI job runs it,
  so a regression is caught only when an agent runs its verification row. Add it to a CI job;
  which checks are required stays the owner's call (AGENT.md §12). From T-093.
- `T-990` Bump `golang.org/x/crypto` past v0.55.0 (GO-2026-6354/6355, `ssh`) once the module's
  `go` directive may rise to 1.26: v0.56.0 requires it. Not reachable today — no `x/crypto/ssh` or
  `openpgp` package is compiled on any OS (DEC-127) — so this is hygiene, not a fix. From T-093.
- `T-989` T-091's `TestZeroConfigStandaloneSearchAddDownload` (`internal/app`) picks the smallest
  live result under its 25 MiB cap dynamically rather than a pinned identifier, so it never
  invents knowledge of the bundled source's catalogue. If a specific, durable public-domain item
  can be identified as guaranteed to stay small and long-lived (verified against the source's own
  documentation, not guessed), pinning it would make the test's runtime and item identity fully
  deterministic instead of catalogue-dependent. Non-blocking finding from the T-091 review (PR #53).
- `T-988` README's preferences-restart Troubleshooting entry ends on a confusing sentence
  ("`ascii = true` is the one exception this mirrors…"). Reword for clarity. Non-blocking finding
  from the T-090 review (PR #52).
- `T-987` The `e` binding's Help string ("view source errors, if any (T-052; DEC-092)") leaks
  internal task/decision ids into the README's generated keymap table and the `?` overlay, which
  are user-facing surfaces. Strip the parenthetical from `GlobalBindings()`
  (`internal/tui/keymap.go:368`) and regenerate the README table. Non-blocking finding from the
  T-090 review (PR #52).
- `T-986` `theme.Truncate` (`internal/tui/theme/width.go:140`) is not escape-sequence-aware: it
  walks `s` grapheme cluster by grapheme cluster (`uniseg.NewGraphemes`) and cuts once `Width`'s
  budget is spent, with no notion that an ANSI SGR sequence (`\x1b[38;2;r;g;bm`) is one atomic,
  zero-width unit — a cut landing inside one leaves a dangling, unterminated escape code in the
  rendered output. Observed in T-090's `docs/assets/demo.cast`: the status bar's rate/peer text is
  colour-styled then truncated to the terminal width, and several captured frames show a broken
  `[38;2;155;155;15...` tail. Fix: either skip escape sequences whole during the walk (matching
  `\x1b\[[0-9;]*m` verbatim, contributing 0 width) or truncate on the plain text and re-wrap the
  active style afterwards. Needs a fixture string containing an unterminated multi-byte SGR
  sequence right at the cut boundary.
- `T-984` Aggregator import (T-083) for Jackett and NZBHydra2, split out of T-083 by the owner
  (DEC-123). **Blocked on API verification, same rule as T-083:** do not infer endpoints. Jackett
  lists configured indexers only through `/api/v2.0/indexers`, which needs a browser session rather
  than the API key (jackett/jackett#16324, closed unresolved). Logging in on the user's behalf is
  out per AGENT.md §2, so this waits on Jackett shipping an API-key endpoint. NZBHydra2's only
  documented endpoint, `/api/stats/indexers`, returns health stats, not re-addable entries; its
  real surface is its instance Swagger (`/swagger-ui/index`), which the owner must supply from a
  running instance. Once either is verified, add it behind T-083's aggregator seam.
- `T-985` T-083's aggregator import (`internal/indexer/prowlarr.ListIndexers`) silently leaves out
  any indexer the aggregator itself has disabled (`enable: false`), rather than importing it and
  marking it disabled — `config.Indexer` has no "disabled at the source" distinct from the user's
  own `Enabled` toggle, and inventing one wasn't worth it for a first pass (found in review,
  PR #51). Revisit if a user asks to bulk-import a currently-disabled indexer on purpose.
- `T-983` `handlePrefsDownloadDirCheck` (internal/tui/preferences.go, ~line 762) compares
  `path`/`margin` against the current form to drop stale results, but no test pins this: if an
  older probe result arrives after a newer one (e.g. the user edits again before the first probe
  returns), the guard as written can't tell old-but-matching from new, so a stale result can
  overwrite the fresher one and the row sticks on "checking…" until the next edit. Add a test
  that delivers the old `prefsDownloadDirCheckMsg` after the new one and asserts the newer result
  wins. Non-blocking finding from the T-082 review (PR #50).
- `T-982` Apply theme and ASCII-mode changes live from the preferences panel
  instead of requiring a restart. Both are TUI-owned rendering state (not a
  property of the frozen engine.Engine contract), so — unlike the other
  restart-required fields in T-082's prefsForm — there is no structural
  reason they could not take effect immediately on save, the same way
  download_dir/saved_destinations/min_free_space already do. Deferred as
  non-blocking QA feedback on PR #50 (T-082) to keep that fix scoped.
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
- `T-916` **Resolved by T-093 (DEC-127).** Enforce per-package coverage thresholds in `make cover`. `COVER_THRESHOLD := 0` at
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
  and `internal/indexer/scraper`, character for character in most cases; httpx's `validInfohash`
  is a third infohash validator beside them (found in review of T-9079, PR #85). AGENT.md §4 forbids one
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
- `T-939` (done in T-9037) A `detail` block for the scraper schema, so `Resolve` can fetch a details page. Today
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
- `T-947` The free-space checks (add-time and the periodic re-check) are per torrent and ignore the
  remaining need of other active downloads on the same destination/filesystem, so several
  downloads can together overcommit a disk each one fits alone. Sum the remaining need per
  destination (ideally per filesystem). Found in review of T-034 (PR #36).
- `T-950` *(resolved by T-095)* Wire `lifecycle.Session` into the composition root when one exists: `NewSession` after
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
- `T-954` *(resolved by T-097)* `make check (windows-latest)`, advisory: `internal/engine`
  `TestProbeListenPortFallsBackWhenTaken` and `TestProbeListenPortPrefersTheConfiguredPort` fail
  with "no random port was free on both TCP and UDP" (PR #38 run 36134477067). This is T-034's
  probe; it passed on `main`'s last run, so it is intermittent on Windows runners. Make the probe
  retry more than one random port before giving up. Found on T-041's PR.
- `T-955` *(resolved by T-097)* The anacrolix file storage keeps every data file memory-mapped after `Engine.Close`. The
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
- `T-964` `TestDetailsScreenEndToEndSelectAndAdd` (`internal/tui/details_test.go`) never checks
  that the rendered output actually shows the downloads screen after `enter` — it only waits for
  the "added ..." status-bar message, which appears regardless of which screen is current.
  Strengthen it to also assert on the downloads screen's own body. Found by QA on T-063 (PR #42).
- `T-965` `details.go`'s file list (`writeWrappedField`'s sibling rendering under "Files") has no
  scrolling or cap: a long `indexer.ExtraKeyFiles` list can overflow the 80×24 floor with no way
  to see the rest. Needs the same kind of viewport `components.Table` already has, or a hard cap
  with a "+N more" line. Found by QA on T-063 (PR #42).
- `T-966` `resolveCmd`/`addTorrentCmd` in `internal/tui/details.go` call `Indexer.Resolve`/
  `Engine.Add` with `context.Background()` and no deadline, which AGENT.md §6.2 requires ("every
  network call takes a `context.Context` with a deadline"). Neither call site currently bounds how
  long a hung source or engine call can block the goroutine the returned `tea.Cmd` runs on. Found
  in review of T-070 (PR #43).
- `T-967` *(resolved by T-095)* Extend T-950 (composition-root wiring) to also pass `tui.WithTorrentStore(store)` and
  `tui.WithDownloadDir(cfg.Paths.DownloadDir)` into the production `tui.New` call. Both options
  exist and are exercised by tests since T-070, but no production call site passes either yet, so
  the add flow's Origin persistence and configured download directory are inert outside tests
  until this wiring exists — the same gap T-950 already documents for `tui.WithHistory`. Found in
  review of T-070 (PR #43).
- `T-968` *(resolved by T-074: the picker only offers an absolute default and refuses relative
  paths without one)* `details.go`'s `resolveSavePath` never checks that `m.downloadDir` is an absolute path
  before cleaning and returning it — it trusts `WithDownloadDir`'s caller-side contract
  ("expected to already be an absolute path") rather than verifying it. T-074 (per-torrent
  destination picker) should enforce this when it replaces this default-only answer, per
  `engine.TorrentStatus.SavePath`'s own documented invariant ("always ... absolute"). Found in
  review of T-070 (PR #43).
- `T-969` TOCTOU window in `platform.OpenFile`/`RevealFile`: `resolveInsideRoots` symlink-resolves
  and containment-checks the target, then the launcher (`open`/`xdg-open`/`explorer`) is exec'd
  with the resolved path as a separate step. A process that can write inside a destination root
  could swap a path component for a symlink between the check and the launcher's own open, and
  the launcher would follow it. Residual risk is low (needs local write access to the root, and
  the launcher only opens/reveals, never writes), but it is not closed. Closing it would mean
  handing the launcher an already-open handle, which none of the three OS launchers accept.
  Found in review of T-073 (PR #46).
- `T-970` *(startup half resolved by T-095; runtime half resolved by T-096)* Production wiring for T-074: the composition root (T-950/T-967) must also pass
  `tui.WithDestinationStore(store)` and `tui.WithMinFreeSpace(<parsed min_free_space>)` into
  `tui.New`, and T-082 must call `engine.RootAdder.AddRoot` when a saved destination is added at
  runtime (config's saved destinations are engine roots only at construction). Found in T-074.
- `T-971` ctrl+c does nothing while the destination picker (T-074, `ContextDestination`) is open —
  add a quit binding to that context. Found in review of T-074 (PR #47). Also true of the settings
  screen's add/edit source form (T-080, `ContextSourceForm`), which claims every key itself the
  same way; found in review of T-080 (PR #48).
- `T-972` The destination picker's write probe (`probeWritable`) runs on every keystroke in the
  path field, not debounced — a fast typist fires a filesystem write-then-remove per character.
  Found in review of T-074 (PR #47).
- `T-973` `expandDestination` (destination.go, T-074) has two edge cases: an environment variable
  whose value itself starts with `~` is not home-expanded after substitution (expansion order is
  env-vars-then-`~`, so a value produced by a var never gets the second pass), and there is no way
  to type a literal `$NAME`/`%NAME%` into a folder name — no escape syntax. Found in review of
  T-074 (PR #47).
- `T-974` The TUI's add flow calls `engine.RootAdder.AddRoot` (destination.go's `prepareDestination`)
  before `engine.Add`, so a destination that gets admitted as a known root but then fails the
  actual `Add` call leaves that root admitted for the rest of the session without ever being
  recorded to `store.Destinations` — a harmless but unrecorded permanent widening of the known-root
  set for one process lifetime. Found in review of T-074 (PR #47).
- `T-975` *(resolved by T-096, DEC-128)* Production wiring for T-080: the composition root (T-950/T-967/T-970) needs a concrete
  `tui.SourceManager` — built over `internal/config` (load/`Save`), `internal/indexer.Registry`
  (add/remove/enable/disable at runtime), `internal/indexer/scraper.Loader`/`Importer`, and
  `internal/indexer/torznab.New`/`internal/indexer/scraper.New` to actually run a `TestSource`
  probe — passed via `tui.WithSourceManager` into `tui.New`. `cmd/tortui/main.go` itself has no
  composition root yet (T-950's own scope), so this stays a Backlog item rather than this task's
  work. Found in T-080. Its `ListAggregatorIndexers` (T-083) must fill each
  `tui.AggregatorIndexer.FeedURL` itself via `prowlarr.FeedURL(baseURL, idx.ID)`, since
  `prowlarr.Indexer` has no `FeedURL` field; an empty `FeedURL` would save a torznab source with no URL.
- `T-976` `config.Save` (T-002) round-trips through `toml.Encoder` over the whole `Config` struct,
  so it does not preserve a hand-edited file's comments or original key order — T-080's "preserving
  existing comments and key order where practical" acceptance is satisfied only in the sense that
  key order follows the (stable) struct field order and no separate writer was invented; genuine
  comment/order preservation would need a TOML AST editor. Found in T-080.
- `T-977` T-080/PR #48 review: the settings save-failure revert path
  (`m.sourcesSnapshot = msg.previous` in `handleSourcesSaveResult`) has no test — no existing test
  sets `fakeSourceManager.saveErr` for the toggle/remove paths, only for the add/edit form's own
  save. Found in review of T-080.
- `T-978` T-080/PR #48 review: concurrent `SaveSources` calls from toggle/remove are unordered —
  two `tea.Cmd`s racing the same optimistic-then-revert state have no ordering guarantee, so an
  earlier save can overwrite a later one on disk, and a failed earlier save's revert can wipe a
  later change that already landed. Refuse a second action while one is in flight, the same
  discipline DEC-115 already applies to pause/resume, or serialise the saves. Found in review of
  T-080.
- `T-979` T-080/PR #48 review: during an add's in-flight save, `sourcesSnapshot` already holds the
  new row (optimistic update), so the live duplicate-id warning (`sourceForm.liveIssues`) flashes
  for a second, unrelated add against the same id, and a second `ctrl+s` on that second add is
  rejected as a duplicate even though the first save has not actually landed yet. Found in review
  of T-080.
- `T-980` T-080/PR #48 review: `TestFormLiveValidationAppearsAndClearsAsYouType` never asserts that
  the live-issue hints actually clear once the field is fixed — it only proves they appear. Found
  in review of T-080.
- `T-981` T-081's `classifyProbeError` parse-failed bucket (`probeParseFailure`,
  `internal/tui/settings.go`) is exercisable only by a test double: the auth-failed side is now
  real (PR #49 review remediation added `httpx.StatusError.AuthFailed()` for 401/403 and
  `torznab.APIError.AuthFailed()` wrapping `IsAuth`), but no adapter error implements
  `ParseFailed()` yet — torznab's `ErrDocumentEmpty`/`ErrDocumentMalformed`/
  `ErrDocumentUnexpectedRoot` and scraper's `ErrDefinitionMalformed`/`ErrDocumentMalformed`/
  `ErrDocumentTooDeep`/`ErrRowsNotAList` are plain `errors.New` sentinel values, which cannot carry
  a method — implementing this needs a small wrapper error type in each adapter, not just a method
  addition. Also, no composition root calls `TestSource` with a real adapter at all yet (Backlog
  `T-975`), so this has no live caller either way until then. Found in T-081.

- `T-992` The composition root builds each `[[indexer]]` torznab source with `torznab.New`, whose
  caps are the fail-closed baseline (keyword search only), and never calls `torznab.Discover`, so a
  configured torznab source is greyed out for Latest until
  something probes its caps. Run the caps probe once per source off the UI goroutine after startup
  (bounded, AGENT.md §6.2/§6.13) and re-register with the discovered caps. Found in T-095.

- `T-993` (promoted to task, 2026-09-28) No test covers the engine's destination roots in
  `internal/app/app.go`: dropping `st.Destinations()` or `cfg.SavedDestinations` from
  `engCfg.SavedDestinations` passes every test, and that is the AGENT.md §6.12 boundary. Add one:
  record a destination in the store, add a torrent under it, restart the root, assert
  `Restored == 1` with nothing in `Failed`/`Errored`. Found in review of T-095 (PR #56).
- `T-994` (promoted to task, 2026-09-28) `tui.saveSessionCmd` (T-095) runs `Session.Save` on a Cmd
  goroutine, so it can race with shutdown (a Save landing after `Store`/`Engine` Close) and with
  the next add's `SetTorrent` (`Save`'s `GetTorrent`-then-`SetTorrent` is not atomic, so a fresh
  record's Origin can be overwritten). Serialise saves through one owner, or drain in-flight saves
  before `Shutdown`. Found in review of T-095 (PR #56).
- `T-995` Re-registering a source under the same id (an edit or disable/enable in Settings, T-096)
  gives it a fresh `indexer.Registry` source, so its per-source minimum refresh interval (§6.13)
  restarts from zero. Carry `lastFetch` across `Unregister`/`Register` of the same id if the 1s
  floor ever matters here. Related: `reserveFetch` looks its source up by id, so a fetch from a
  source replaced mid-search reserves the *replacement's* refresh floor; key it on the selected
  source like `storeResults` is. Found in T-096 and its review (PR #57).
- `T-996` `internal/app` tests that make two requests to one httptest host wait out httpx's real
  1s `DefaultMinHostInterval` (~2s total in T-096's settings tests): the composition root has no
  seam for `httpx.Config.Clock`/`MinHostInterval`. Add one alongside `Options.transport`. Found in
  T-096.
- `T-997` No test proves the production settings wiring: removing the `WithSourceManager` and
  `WithPreferencesManager` options from `internal/app/app.go` leaves every test green. Add a teatest
  check on the root's model that the settings screen never shows "no source manager configured"
  (nor the preferences equivalent). Found in review of T-096 (PR #57).
- `T-998` `settingsManager.SaveConfig` (T-096) returns an `AddRoot` error after `config.Save` and
  the in-memory update already succeeded, so the TUI keeps its old snapshot while disk and memory
  hold the new one. Only `ErrClosed` during shutdown can cause it in practice (roots are pre-checked);
  log it instead of returning it, or document the divergence. Found in review of T-096 (PR #57).
- `T-999` (promoted to T-9026, 2026-09-29) *(owner decision)* The indexer hostname check (T-007) reads Go field or function access
  on a url-style name (a struct field or package function whose name ends in URL, inside an
  indexer-context line) as a hostname, and it scans commit messages too. T-095 and T-096 both hit
  it; the workaround is binding the value to a local first. Record this as a known false-positive
  class and decide whether the scanner should skip Go selector expressions. Scanner not changed.
  Found in T-096 (PR #57).
- `T-9001` T-097 review: the every-OS remove-after-`Close` test deletes the data file with
  `os.Remove` directly rather than restarting the engine and calling `Remove(id, true)`, so it
  does not exercise the real remove-with-data path through a freshly reopened `fileStore`. Found
  in review of T-097 (PR #58), non-blocking.
- `T-9002` T-097 review: on Unix, a data file deleted outside tortui mid-download keeps receiving
  writes through `fileStore`'s cached handle until `Completion` re-checks it — the same behaviour
  the old mmap-based storage had. Found in review of T-097 (PR #58), non-blocking.
- `T-9005` *(owner decision)* `docs/platforms.md`'s tier table still says macOS and Windows are
  "verified by hand ... before release", but DEC-132 waives the manual Windows (and Linux) pass
  for v1.0. It needs an explicit note pointing at DEC-132 / Backlog `T-9004`; editing
  `docs/platforms.md` (AGENT.md §14's full text) is the owner's call. Found in review of T-094
  (PR #60), non-blocking.
- `T-9006` `docs/terminal-matrix.md`'s non-TTY refusal row (`printf '' | ./bin/tortui`) marks
  alignment and colour `PASS`, but a one-line refusal has no table to align and no colour to
  degrade; `N/A` fits those two columns better (exit and restore stay `PASS`). Found in review of
  T-094 (PR #60), non-blocking.
- `T-9007` `internal/app/roots_test.go` (T-993): if a `t.Fatalf` fires between `New` and the first
  `Close`, the first App is never closed (lock, engine and log stay open), which can add a Windows
  `TempDir` cleanup error on top of the real failure. Register a `t.Cleanup` that closes it once.
  Found in review of T-993 (PR #61), non-blocking.
- `T-9008` (promoted to task, 2026-09-28) No test proves `internal/app/app.go` passes the session (not the store) to
  `tui.WithTorrentStore`: reverting it to `a.store` leaves every test green and reopens T-994's
  origin race. Same gap class as `T-997`; one teatest add-flow check on the root would cover both.
  Also, `Shutdown` calling `Session.Save` instead of `Session.Close` is caught only by chance by
  `TestSessionSavesRacingShutdownNeverOutliveIt`. Found in T-994.
- `T-9013` The bundled Internet Archive definition maps no `source_url`, so `u` opens nothing for
  its results. The T-9010 field `template` can now build `/details/{{value}}` from `identifier`;
  verify that address against the Archive's docs and map it. Found in T-9010.
- `T-9014` The add flow (`internal/tui/destination.go`) puts both `Result.Magnet` and
  `Result.TorrentURL` into `engine.AddSource`, and the engine refuses two links with
  `ErrAmbiguousSource`. A torznab item with a magnet and an enclosure, or a scraper definition
  mapping both `magnet` and `torrent_url`, therefore cannot be added. Pick one (prefer the
  `.torrent`, which carries trackers and web seeds) in the TUI, with a test. Found in T-9010.
- `T-9015` On an httpx client built with `FollowSubdomainRedirects`, a per-request header set by
  the caller (a `Cookie` or `Authorization` in the Request's own headers, as opposed to injected
  credentials) would be forwarded by net/http to the subdomain a redirect lands on. Not reachable
  today: the engine's `.torrent` fetch passes no headers. Either strip or refuse sensitive
  per-request headers when the option is on, or document the limitation on the option. Found in
  review of T-9010 (PR #64).
- `T-9016` Pressing enter on an empty "Import from (path or URL)" field does nothing, silently
  (`internal/tui/settings.go`, the `fieldImport` enter branch returns `m, nil`). It should show a
  hint such as "type a file path or URL first". The owner expected a file picker. Found in an
  owner run of the add-source form.
- `T-9017` While the import field is the only thing being used, the add-source form still shows
  "! name is required", which suggests Name is needed for an import when it isn't. Found in an
  owner run of the add-source form.
- `T-9018` (done in T-9019) The import path was not `~`-expanded, so `~/x.yml` failed. Found in an
  owner run of the add-source form; T-9019's `~/` expansion covers it.
- `T-9020` `TestSearchResultAlwaysMovesToResults` (`internal/tui/startup_test.go`) only starts from
  `ScreenSearch`, so a stay-put guard coming back would pass it. It should also start from
  `ScreenDownloads`. Found in review of T-9011.
- `T-9021` (promoted to task, 2026-09-29) `Query.MinSeeders` filtering in the torznab and scraper
  adapters compares the raw `Seeders` int, so a result whose source reported no seeders (marked
  unknown, T-9012) is dropped by any minimum above zero. Found in T-9012.
- `T-9022` `registry.mergeResults` replaces a duplicate only when it has strictly more seeders. When
  one source reports a real 0 and another reports unknown, whichever arrived first wins, so the S/L
  cell can show `–` or `0` depending on order. Prefer a known count on a tie. Found in review of
  T-9012 (PR #66).
- `T-9023` The component goldens build the `–` cell by hand in `goldenRows()` instead of going
  through `resultRow`/`formatSwarm`, so no golden exercises the real results model. Found in review
  of T-9012 (PR #66).
- `T-9027` The `go test -race` job's "Confirm C toolchain" step prints `go env CGO_ENABLED` but does
  not assert it is `1`, so a runner where cgo silently ends up off would only fail later, less
  clearly, in `-race` itself. Found in review of T-9025 (PR #70), non-blocking.
- `T-9028` The Windows `go test -race` job installs make and mingw through Chocolatey, which DEC-107
  moved shellcheck off after the T-932 outage; a Chocolatey outage would turn that advisory check
  red for a cause unrelated to the code. Found in review of T-9025 (PR #70), non-blocking.
- `T-9029` In `https://host.org@Other` the hostname scanner reads `host.org` as userinfo and never
  checks it; it reports `other` instead, so the check still fails but names the wrong host.
  Pre-existing, unchanged by T-9026. Consider checking a hostname-shaped userinfo too. Found in
  review of T-9026 (PR #71).
- `T-9030` Under `LC_ALL=C` (T-9026) the key/value shape no longer matches a value that starts with
  a non-ASCII letter, such as `Host: "İİİİ.some-host.example.zzz"`; main matched it after
  lowercasing. Non-ASCII letters are also no longer lowercased in reported names. Minor, since
  such labels are not valid registrable hostnames. Found in review of T-9026 (PR #71).
- `T-9032` The archived T-9026 acceptance text in `docs/tracker-archive.md` still describes the
  broader rule; DEC-141 records the narrowing. Its Notes now carry a one-line pointer (done in
  T-9031), so nothing further is needed unless the acceptance text itself should be rewritten.
  Found in review of T-9026 (PR #71), non-blocking.
- `T-9033` `Host: probe.org_X` and `Host: probe.org9X` fall inside the narrowed gap of the hostname
  scanner (a bare two-part token with an uppercase letter after the dot); neither is a valid
  hostname. Found in review of T-9026 (PR #71), non-blocking.
- `T-9035` No config or form test covers a scraper with an empty URL AND an empty definition. The
  code rejects it, but no test pins that. Found in review of T-9031 (PR #72), non-blocking.
- `T-9036` `tortui doctor` skips a scraper saved without a URL ("no URL configured") and, for
  scraper entries, probes the config URL rather than the definition's `base_url`. Found in review
  of T-9031 (PR #72), non-blocking.
- `T-9038` A periodic history flush that snapshots before `ClearHistory` but commits after it can
  briefly write the old history back to disk. Only a crash inside one flush interval matters; the
  next flush fixes it. `Flush()` has the same window. Found in review of T-9034 (PR #73),
  non-blocking.
- `T-9039` `ClearHistory` can report failure during shutdown even though `Close`'s final flush
  already saved the clear. Found in review of T-9034 (PR #73), non-blocking.
- `T-9040` No test checks `ClearHistory`'s immediate flush separately from `Close`'s flush. Found
  in review of T-9034 (PR #73), non-blocking.
- `T-9042` A details page's torrent link may point to another host. Allowed on purpose: it matches
  the listing rule, and the engine's `.torrent` client sends no credentials. Revisit only if that
  client ever gains per-source credentials. Found in review of T-9037 (PR #74), non-blocking.
- `T-9043` The TUI's `resolveCmd` calls `Resolve` with `context.Background()`, so the user cannot
  cancel a slow details-page fetch; httpx's timeouts still bound it. Found in review of T-9037
  (PR #74), non-blocking.
- `T-9044` Opening Details on a link-less scraper result does not call `Resolve`, so no magnet shows
  there until the user adds it. UX follow-up from review of T-9037 (PR #74); a fetch on open must
  stay user-initiated and cached per AGENT.md §6.13.
- `T-9047` Torznab `torrentAddress`/`sourceAddress` return the first http(s) candidate (enclosure
  then link; comments then permalink guid) through `linkWithoutUserinfo`, which gives "" when it
  does not parse, so the result has no link even if a later candidate is usable. Fall back to the
  next candidate instead. Non-blocking note from review of T-9045 (PR #76).
- `T-9048` T-9045's "existing tests unchanged" criterion conflicted with its criterion 1:
  `TestListedLinksNeverCarryUserinfo` had to move to a clean base_url once a base_url with
  userinfo no longer reaches `New`. The change was justified. Future criteria: "existing tests
  pass; a test changes only where another criterion requires it". Note from review of T-9045 (PR #76).
- `T-9064` Host-only naming (DEC-148) still shows a secret embedded in a hostname. Note only; no
  change planned.
- `T-9065` Results rows with wide emoji (U+1F680, U+1F6F0 in the demo) may shift later columns.
  Verify in a real terminal (owner step).
- `T-9078` `make run` should set TORTUI_HOME so the dev run does not touch real state, lock and
  downloads. Noted in the README by T-9073.
- `T-9087` Note only: the magnet validator allows non-ASCII and UTF-8 C1 control bytes. Harmless,
  because the display name is replaced. Found in review of T-9079 (PR #85).
- `T-9091` Ubuntu 26.04: once the Linux CI jobs are green on it, move release.yml from ubuntu-24.04
  to ubuntu-26.04 so a tag run matches CI, then decide whether to go back to a floating label.
  GitHub's runner-images README now says `ubuntu-latest` moves to 26.04 in November 2026 (T-9089
  said 2026-10-19). Found in T-9090.
- `T-9092` sigstore/cosign-installer has a v4 line (v4.1.2, no floating v4 tag) that installs a
  newer cosign. v3 is a composite action with no Node runtime, so T-9090 left it. Check the
  .goreleaser.yaml signing step against the newer cosign before moving. Found in T-9090.
- `T-9093` Three CI tools are pinned below their latest because those need Go 1.26 and CI builds
  with Go 1.25: gofumpt v0.11.0 (v0.12.0), golangci-lint v2.12.2 (v2.13.0 and later), goimports
  from x/tools v0.49.0 (v0.50.0). Bump all three when the repo moves to Go 1.26. Found in T-9090.
- `T-9113` `doctor` checks only whether each enabled source is reachable. It never builds the adapter, so
  a source that fails registration (logged as "app: skipping source") can still show as reachable.
  Consider having `doctor` report build and Register failures. Non-blocking note from review of
  T-9111 (PR #94).
- `T-9115` Downloads builds its layout twice per message, in Update and View, at about 2.4 ms each
  at 1000 torrents. Fine today. If it ever shows up, sync only on messages that can change the
  offset. Non-blocking note from review of T-9112 (PR #95).
- `T-9122` `TestGraceFollowsTheControlsDelay` (`internal/engine/anacrolix`) builds its expected values
  from the constants it tests, so a change to a constant's value still passes. Assert literal values.
  Non-blocking note from review of T-9120 (PR #98).
- `T-9123` The doc comment on `TestPublishedMagnetSourcesAreDropped`
  (`internal/app/metainfosources_test.go`) still says the xs= and as= addresses "name a loopback host";
  they now name sources.example.org. Non-blocking note from review of T-9120 (PR #98).
- `T-9124` The top of the Backlog is out of numeric order (T-9004, then T-991 down to T-982). Sort it.
  Non-blocking note from review of T-9120 (PR #98).
- `T-9128` A restored torrent tracked as failed by `trackFailed` (`internal/engine/anacrolix/resume.go`), such as not
  enough free space at restore, keeps no data name and no infohash: remove-with-data deletes nothing even when its
  metainfo names data on disk, and a re-add is not matched to it. Found while doing T-9127.

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
| DEC-113 | 2026-09-26 | T-070: resolves the T-070/T-074 mutual dependency — SavePath defaults via WithDownloadDir only, T-074 adds the real picker; dedup matches only on Result.InfoHash; Resolve triggers on an empty Magnet via new Searcher.Get; Origin persists via new optional TorrentStore; duplicate selects the existing row via m.selection; results-screen enter now also adds |
| DEC-114 | 2026-09-26 | T-071: active/completed split treats StatePaused-at-Progress-1 as complete too (seed-policy-satisfied torrents have no distinct state); queue position and seed-policy text read new optional queueProvider/seedPolicyProvider assertions, fake implements neither; TorrentStore gained GetTorrent so Source/added-at fall back to the store record for a torrent added this session (live Origin is zero until a restart); enter toggles an errored row's reason in place rather than reusing ScreenDetails |
| DEC-115 | 2026-09-26 | T-072: optimistic pause/resume overlays snapshots only while the engine call is in flight, the next snapshot wins once it returns, failure reverts; a second p in flight is refused; remove dialog targets the ID captured at open and has no one-key delete |
| DEC-116 | 2026-09-26 | T-073: containment enforced inside platform.OpenFile/RevealFile (symlink-resolved, strictly inside a root, logged refusal); roots = download dir + saved destinations + every torrent SavePath; f reveals the largest file o opens, both refuse below 100%; Windows quotes via SysProcAttr.CmdLine and ignores explorer exit 1; Linux reveals the parent dir |
| DEC-117 | 2026-09-26 | T-074: destination picker as a modal step of the add flow; validation in a Cmd keyed by path; known roots grow only via new optional engine.RootAdder + store.TouchDestination, re-admitted by Session.Resume; / and \\ both separators, Windows drive/UNC paths accepted only where filepath gives them a volume; relative paths resolve against the default only |
| DEC-118 | 2026-09-26 | T-080: import runs on enter (ctrl+i is literally tab's keycode); the list's own keys are handled directly in root.go rather than as Bindings to keep the `?` overlay inside the 80×24 budget, with a one-line legend instead; SourceManager mirrors Searcher/TorrentStore, concrete wiring deferred to Backlog T-975 |
| DEC-119 | 2026-09-26 | T-080 review remediation: model-side sourcesSnapshot replaces every synchronous SourceManager.Sources() call from Update/View, updated optimistically and reverted on failure; refreshSearchSources re-reads Searcher.Enabled() after every successful save so Settings changes are visible on the Search screen live |
| DEC-120 | 2026-09-26 | T-081: classifyProbeError sorts a TestSource error via errors.Is(context.DeadlineExceeded) plus two duck-typed marker interfaces (no adapter import, AGENT.md §4); t refuses a second in-flight probe (DEC-115), esc cancels via its own CancelFunc; d opens a new ContextSourceTestDetail panel from lastProbe |
| DEC-121 | 2026-09-26 | T-081 review remediation: settings list esc/d now guarded against quit-confirm/error-detail (regression test added); classifyProbeError also matches net.Error's Timeout() bool; httpx.StatusError and torznab.APIError now implement the auth marker for real, narrowing Backlog T-981 to parse-failed only |
| DEC-122 | 2026-09-26 | T-082: preferences panel (p key) applies download_dir/saved_destinations/min_free_space live (TUI-owned state); rate limits/peers/port/seed policy/search timeout/theme/ascii have no live-reconfigure path against the frozen Engine interface, so they're persisted and named "restart to apply" instead; destination-removal warning reuses engine.ContainedIn — the known-roots set was never actually at risk since tracked torrents' own SavePaths already widen it regardless of SavedDestinations |
| DEC-123 | 2026-09-28 | T-083 narrowed by the owner to Prowlarr only; Jackett and NZBHydra2 aggregator import deferred to Backlog T-984 pending API verification |
| DEC-124 | 2026-09-28 | T-090: first-run overlay wording corrected against AGENT.md §2 (bundled sources do ship, T-024) instead of the stale acceptance text; README's Status notice now states precisely what's wired today (`--version`/`doctor`/`--demo`) vs. pending the composition root (Backlog T-950), and concrete false claims (`--ascii` flag, ten MPL modules not yet in `NOTICE`) were fixed |
| DEC-125 | 2026-09-28 | T-091: one combined suite (search + add + real download + offline restore) covers both the download/resume and zero-config-standalone acceptance items; reachability checked generically across every bundled source; seed_policy pinned to "off" and the wait accepts Seeding-or-Paused (T-034-class race), and the 25 MiB size cap is actually enforced (PR #53 remediation) |
| DEC-126 | 2026-09-28 | T-092: goreleaser config uses homebrew_casks, not the deprecated brews pipe; run()/runDoctor() parse argv through the same flag-set constructors completions walk; each publisher's skip_upload is templated on its own token (not `auto`, which only skips prereleases); cask gets a quarantine-clearing postflight hook |
| DEC-127 | 2026-09-28 | T-093: goleak on every package's TestMain with third-party-only ignores; otel bumped to 1.42.0 (adds MIT cespare/xxhash/v2); x/crypto ssh/openpgp findings triaged as not compiled in (bump needs go 1.26, T-990); make cover enforces per-package §9 floors (T-916) |
| DEC-128 | 2026-09-28 | Owner scheduled the missing composition root (T-095), its settings wiring (T-096) and the Windows engine storage fix (T-097) ahead of T-094; folds Backlog T-950/T-967/T-970/T-975/T-955/T-954 |
| DEC-129 | 2026-09-28 | T-095: "starts on Latest" read as one Latest fetch per launch against the selected sources (tui.WithStartupLatest), never a timer, not yanking a user who already left Search; session saves after add/remove are a TUI hook (tui.WithSessionSaver) so they follow the add flow's SetTorrent, not an engine wrapper |
| DEC-130 | 2026-09-28 | T-096: disabling a source unregisters it (as at startup), edits re-register; SaveConfig keeps the manager's own Indexers and admits download_dir plus every saved destination as engine roots only after the write succeeds |
| DEC-131 | 2026-09-28 | T-097: tortui owns its file storage (plain `*os.File` handles, closed on torrent and engine close) instead of the library's mmap file storage, whose classic I/O is reachable only through a process-wide env var |
| DEC-132 | 2026-09-28 | v1.0 manual verification is macOS-only (owner has no Windows/Linux device); Windows/Linux terminal matrix and manual download/fresh-install checks deferred to Backlog T-9004 — CI and cross-builds on all three OSes unchanged |
| DEC-133 | 2026-09-28 | T-993: Session.Resume re-admits recorded destinations, so a restart alone cannot pin app.go's st.Destinations() root source; test seam Options.beforeResume probes the freshly built engine before Resume; the duplicate admission is kept |
| DEC-134 | 2026-09-28 | T-994: lifecycle.Session owns the torrents bucket — the add flow's SetTorrent runs under Save's lock (a store-level atomic merge would still let a save prune a fresh record); Shutdown calls Session.Close, then seals it even on timeout |
| DEC-135 | 2026-09-28 | T-9008: unexported Session.afterUnlock seam (runs after every lock release) makes the Close/seal no-gap window deterministic to test; the app wiring is pinned by a closed-session probe through the real add flow |
| DEC-136 | 2026-09-29 | T-9010: the engine's credential-free .torrent client follows a redirect to a subdomain of the requested host (same port, never https to http) so a source's storage hand-off works; indexer clients keep DEC-062's same-host rule |
| DEC-137 | 2026-09-29 | T-9011: supersedes DEC-129's startup Latest; the program opens on an empty Search screen and queries nothing until the user acts (owner decision) |
| DEC-138 | 2026-09-29 | T-9012: an unreported swarm count travels as `Result.Extra` markers (`ExtraKeySeedersUnknown`/`ExtraKeyLeechersUnknown`), not a Result field; the TUI renders `–` |
| DEC-139 | 2026-09-29 | T-9024: tab on an empty import field always advances; completion (incl. the definitions-dir start) needs typed text; supersedes T-9019's third criterion |
| DEC-140 | 2026-09-29 | T-9021: a result with unknown seeders passes `MinSeeders`; a real count below the minimum is still dropped; one shared `indexer.MeetsMinSeeders` |
| DEC-141 | 2026-09-29 | T-9026: the hostname scanner skips only an unquoted bare two-part Go selector value (`srv.URL`); URLs and multi-label values are never skipped |
| DEC-142 | 2026-09-29 | T-9037: scraper `details:` block; Resolve fetches the item page once, at add time only, through the same client, refused unless on base_url's own host and port without https to http; one link filled (magnet, else torrent, else infohash) |
| DEC-143 | 2026-09-29 | T-9041: a scraper details address with any userinfo is refused before a request; every link the scraper resolves (listing and details) has its userinfo dropped, the link kept |
| DEC-144 | 2026-09-29 | T-9045: a scraper base_url with any userinfo fails validation; httpx refuses any redirect whose target carries userinfo; torznab links have their userinfo dropped, the link kept; magnets untouched |
| DEC-145 | 2026-09-30 | T-9046: refines DEC-144; httpx follows a redirect whose userinfo is byte-identical to the original request's (as written in the Location, or inherited by a relative one), only on the original host and effective port; scheme rule unchanged; every other userinfo redirect refused |
| DEC-146 | 2026-09-30 | T-9049: httpx recognises net/http's unparseable-Location error by its leading text and replaces it with `ErrRedirectLocationInvalid` at the request host; no Location text kept; every other cause unchanged |
| DEC-147 | 2026-10-01 | T-9056: an add sends the engine one link: a published magnet, else the TorrentURL, chosen once at `confirmDestination`; torznab Resolve builds a magnet from a hash only when there is no TorrentURL |
| DEC-148 | 2026-10-01 | T-9057: a download is named by the add flow's recorded title until metadata (TotalBytes 0), then by the engine's name; no name is ever a web address (host only, `engine.SafeName`); the record keeps the title until metainfo; the record's TorrentURL keeps the key |
| DEC-149 | 2026-10-01 | T-9061: on a short terminal the `?` overlay flows its bindings as wrapped runs and Downloads scrolls by whole blocks; legends wrap between hints, never truncate |
| DEC-150 | 2026-10-01 | T-9069: bundled sources are Settings rows tagged built-in and doctor lines; a `disabled_builtin` config list is their off switch (default empty, all on) |
| DEC-151 | 2026-10-02 | T-9079: a .torrent link that redirects to a magnet adds by the validated magnet in place (same id, destination, name); the magnet replaces the link in resume data and the record |
| DEC-152 | 2026-10-02 | T-9090: the Linux CI jobs run on ubuntu-26.04 (tried in the PR), the release job is pinned to ubuntu-24.04; matrix job names keep the os key, so no check name changes |
| DEC-153 | 2026-10-02 | T-9094: every magnet drops `xs=`/`as=` at `specFromMagnet`, the one magnet-to-spec call (add, queue, restore, redirect); the stripped magnet is what resume data and the record keep; Offline refuses the library's metainfo-sources client |
| DEC-154 | 2026-10-02 | T-9095: when url.Parse refuses a name, `engine.SafeName` treats a `scheme://` prefix (RFC 3986 scheme) as an address and names it "torrent file"; a name like `http://example.org with spaces` is no longer kept |
| DEC-155 | 2026-10-02 | T-9099: refines DEC-154; `engine.SafeName` judges a name with its invisible runes (control, Cf, default-ignorable) removed, and an http(s) scheme is an address with or without a host |
| DEC-156 | 2026-10-02 | T-9099: a lenient httpx redirect rule (subdomain, magnet) applies only when the chain's first request carries no userinfo and no header besides User-Agent; otherwise the strict rule |
| DEC-157 | 2026-10-02 | T-9100: styled lines are truncated with `charmbracelet/x/ansi` (already in the module graph, MIT), promoted to a direct dependency behind `theme.TruncateStyled`; no new module. The tab bar degrades in steps: full, no `[n]` hints, then an ellipsis clip. |
| DEC-158 | 2026-10-02 | T-9106: the Trust sort treats Unknown as the lowest level (Unknown < None < Verified < Trusted < VIP) and breaks ties by seeders, title, id, in one fixed direction. Supersedes T-062's "Unknown pinned last". |
| DEC-159 | 2026-10-02 | T-9111: `BuiltinSources` returns a manager-side version with the rows; the TUI drops older refreshes by it. |
| DEC-160 | 2026-10-02 | T-9112: the Downloads scroll offset is a block start kept by Update; a stale one snaps down, a downward scroll snaps up. |
| DEC-161 | 2026-10-03 | T-9121: the magnet strip helper is exported from the engine package; the add flow's first record write runs it too, beside the engine's choke point (DEC-153) |
| DEC-162 | 2026-10-03 | T-9121: a torrent refused after metadata, or whose queued start failed, is untracked when its infohash is added again, which starts over under a new ID |
| DEC-163 | 2026-10-03 | T-9127: refines DEC-162; an add to another destination is refused (ErrLeftData) while a refused entry for the infohash left data on disk; the user removes that entry, keeping or deleting the data |
| DEC-164 | 2026-10-03 | T-9127: a create that finds the storage closed keeps the file when a handle in the table is for it or it is no longer empty, and keeps any created directory holding a kept or in-use file |

## Blocked

None.

---


Resolved blockers are archived verbatim under **Blocked — Resolved** in `docs/tracker-archive.md`.
