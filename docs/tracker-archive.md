# Tracker archive — tortui

Finished task blocks, moved here **verbatim** from `TASK_TRACKER.md` by T-945 so the live
tracker stays small enough to read in full. Nothing here is edited after archiving; the live
tracker lists every archived task by id. Search by id: `grep -n '^### T-031' docs/tracker-archive.md`.

## Process

### T-945 · Fast task loop
```
status: done
depends: —
tier: M
```
**Files:** `AGENT.md`, `CLAUDE.md`, `START.md`, `TASK_TRACKER.md`, `docs/`, `.claude/agents/`,
`.claude/settings.json`, `Makefile`, `scripts/next-task.sh`, `.github/workflows/ci.yml`

**Acceptance**
- Live tracker holds only open work plus indexes; nothing archived is lost (every task id and DEC row
  accounted for); AGENT.md §14–§16 full text preserved verbatim in `docs/`.
- Tiers on every open task; agent definitions with explicit model and effort; one party per gate.
- `make next` derives the next task; permission allowlist covers every gate via make targets.
- CI no longer double-runs PR pushes or recompiles pinned tools; required check names unchanged.

**Notes:** Owner-requested 2026-09-25 (DEC-103). Split done by a throwaway script that verified 53/53
task ids and 102/102 DEC rows survive. `make next` now reports T-033 (tier H). Next-task order is
file order, so T-944 (in Phase 3, depends T-033) runs right after T-033. Actual minutes per task go
in the session log so tier budgets can be recalibrated.

---

### T-949 · macOS-first, lower-latency task loop
```
status: done
depends: —
tier: M
```
**Files:** `AGENT.md`, `START.md`, `docs/platforms.md`, `.claude/agents/*.md`,
`.github/workflows/ci.yml`, `TASK_TRACKER.md`, `docs/`

**Acceptance**
- Merge gate is macOS-first (DEC-107): required checks are `make check (macos-latest)`,
  `make build-all`, `make licenses`, and the indexer hostname allowlist check; `make check` on
  `ubuntu-latest`/`windows-latest` stays advisory per PR — a red one gets a `T-9NN` Backlog entry
  naming the failing test and job instead of sending the task back. All three OSes must be green
  before any release tag. §14's portability contract, `internal/platform` confinement, and
  `make lint-cross` are unchanged; CI job names are unchanged.
- Reviewers verdict on the diff and mutation tests alone; they no longer wait on or check CI — only
  the orchestrator checks CI (against the required set above) before merging.
- A re-review after a FAIL resumes the same reviewer (context intact); a fresh reviewer starts only
  if the original is unavailable. The implementer still never reviews its own work.
- Stall recovery needs no owner input: the orchestrator resumes a stalled/errored agent once, then
  starts a fresh agent of the same type with a note on what the failed one found.
- Tests wait for a predicate or terminal state, never one exact intermediate state a background loop
  can skip — codified in the implementer files and checked by both reviewer files, citing the T-034
  Windows flake (`waitForState(..., StateDownloading)` skipped past by the policy tick) as the
  worked example.
- Windows shellcheck installs from a pinned official GitHub release with checksum verification,
  cached like the other pinned CI tools — no more Chocolatey dependency on that leg.
- START.md's orchestrator prompt (steps 3–5) mirrors the new loop; §12's owner-only list and §2/§3/§5
  are unchanged.

**Notes:** Owner-requested 2026-09-25 (DEC-107). AGENT.md §11 (tiers/escalation, verification table,
loop, stall recovery), §12 (release needs all three OSes green) and §14's summary updated;
`docs/platforms.md` mirrors §14. `.claude/agents/{implementer,implementer-h}.md` gained the
wait-for-a-predicate rule; `.claude/agents/{reviewer,reviewer-h}.md` dropped the CI-wait step, added
the same-state-wait check, and note re-review normally resumes the same reviewer. `ci.yml`'s Windows
shellcheck step now downloads `v0.11.0` from `koalaman/shellcheck`'s GitHub release, verifies its
SHA-256 against a pinned value computed for this task, and caches the extracted binary — CI job
names untouched, `actionlint` clean. Owner-only follow-up, not done here (§12): remove the
`ubuntu-latest`/`windows-latest` `make check` contexts from branch protection's required checks.

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
**Branch protection is configured** on `main` via the classic branch-protection API
(`PUT /repos/kdta91/tortui/branches/main/protection`): `required_status_checks.strict = true`
with `contexts` = `make check (ubuntu-latest)`, `make check (macos-latest)`,
`make check (windows-latest)` — the three matrix job names GitHub reports as individual status
checks for the single `check` job in `.github/workflows/ci.yml`. `enforce_admins` is `false` and
`required_pull_request_reviews` is left unset (confirmed absent from the API's own read-back of
the protection object), so "require a pull request before merging" is **not** enabled, per
AGENT.md §10 — the loop still pushes tracker status flips straight to `main`. This was previously
blocked because `kdta91/tortui` was a private repository on a free plan, where both the classic
protection API and the rulesets API returned `403 Upgrade to GitHub Pro or make this repository
public`; the repo owner has since made the repository public (confirmed via
`gh repo view kdta91/tortui --json visibility` → `PUBLIC`), which unblocked the classic API on
the first retry. See DEC-020 (superseded) and DEC-021.
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
status: done
depends: T-001
```
**Files:** `internal/config/`, `internal/platform/`, `config.example.toml`

**Notes:** `internal/config` exposes `Config`/`Indexer` structs matching every field in the
acceptance list, `Default(downloadDir)` for built-in defaults, `(Config).Validate() []string`
which collects every problem (never stops at the first), and `ParseByteSize` for `min_free_space`
strings like `"1GB"`/`"500MB"`. `Load(flagConfigPath)` resolves paths, writes a default config
atomically (temp file in the same directory, `fsync`, `chmod 0600`, `rename`) and creates the
download dir on a missing file (`FirstRun: true`), or decodes an existing file over the defaults
(so partial files keep defaults for every omitted key) and reports both unknown TOML keys
(via `toml.MetaData.Undecoded()`) and `Validate()` problems in `LoadResult.Problems`. A
world-readable existing config file (checked via `os.FileMode.Perm()&0o077`) is reported in
`LoadResult.Warnings`, separate from key-specific problems. Bundled lawful sources (T-024) are
compiled definitions, not config entries, so `Default()` ships with an empty `Indexers` list —
that is expected and is not a gap in this task. `--config` overrides only the config file path,
never the state or download roots.

Per-OS path resolution (§14) lives in a new `internal/platform` package with `paths_darwin.go`,
`paths_linux.go`, `paths_windows.go` (see DEC-022) — `ConfigDir()`, `StateDir()`, `DownloadDir()`
per OS. `paths_linux.go` resolves these via `github.com/adrg/xdg`'s package-level vars, calling
`xdg.Reload()` on every invocation so it re-reads the environment each time; `paths_darwin.go` and
`paths_windows.go` read `os.Getenv`/`os.UserHomeDir` directly instead, for reasons specific to
each (see DEC-022). All three are equally exercisable with `t.Setenv` at call time. `$TORTUI_HOME`,
when set, short-circuits `internal/platform` entirely and puts config/state/downloads under
`$TORTUI_HOME/{config,state,downloads}` (`internal/config/paths.go`).

Tests: `internal/config/validate_test.go` (table-driven `Validate`/`ParseByteSize` cases —
valid, and one invalid case per field, plus a duplicate/missing indexer id and a scraper missing
its definition) and `internal/config/load_test.go` (first run, valid file, partial file, invalid
file with multiple simultaneous problems, unknown key, malformed TOML, `TORTUI_HOME` redirection,
`--config` override, world-readable warning skipped on Windows). `internal/platform` has one
build-tag-gated test file per OS (`paths_darwin_test.go`, `paths_linux_test.go`,
`paths_windows_test.go`), each exercising that OS's env-driven resolution with `t.Setenv`; only
the host OS's file runs locally, the other two run natively in the CI matrix — the `paths_linux.go`
tests were additionally cross-executed under linux/amd64 (Docker) during development to verify
`xdg.Reload()` + `t.Setenv` actually re-resolves as expected there, not just on the host OS. All
three GOOS targets were cross-compiled locally (`go build`/`go vet`/`go test -c` under `GOOS=linux`
and `GOOS=windows`) to catch build-tag mistakes before relying on CI. `go test ./...` and
`go test -race ./...` are green; `internal/config` and `internal/platform` are at 81.6% and
81.8% statement coverage respectively (measured locally on the host OS only, not enforced by
`make cover`'s threshold yet since AGENT.md §9's per-package thresholds name only
`internal/indexer`, `internal/engine`, and `internal/tui`). `cmd/tortui/main.go`'s comment
claiming config loading "lands in T-002" was corrected to say wiring it into `main` still waits
on the `internal/app` composition root, since T-002's scope is the `internal/config` package
itself, not consuming it from `main`. Added `github.com/BurntSushi/toml v1.6.0` (MIT) and
`github.com/adrg/xdg v0.5.3` (MIT) to `go.mod`, per AGENT.md §3 — see DEC-022 for exactly where
`adrg/xdg` is and isn't used and why.

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
status: done
depends: T-001
```
**Files:** `internal/logging/` (`logging.go`, `mask.go`, `rotate.go`, plus
`logging_test.go`, `mask_test.go`, `rotate_test.go`)

**Notes:** `internal/logging` is a standalone package: `New(Options) (*slog.Logger, io.Closer,
error)` builds a JSON `slog.Handler` wrapping a from-scratch rotating `io.WriteCloser`
(`rotate.go`) inside a masking handler (`mask.go`), writes to `Options.File` (or
`Options.StateDir/tortui.log` when `File` is empty), and — critically — calls
`slog.SetDefault` on the logger it builds, so every package-level `slog.Info`/`slog.Warn`/etc.
call anywhere in the process is redirected away from slog's zero-value default handler (which
writes to stderr) the moment `New` returns. JSON was chosen over a key=value text format purely
because it is trivial to parse back apart in tests and matches how the masking handler already
has to walk nested `slog.Group` values; no acceptance criterion required a specific format.

Rotation (`rotate.go`) is hand-written rather than a third-party library (`DefaultMaxSizeBytes =
10 * 1024 * 1024`, `DefaultMaxBackups = 3`): weighed against adding a dependency (which would need
a `DEC-` row plus a license/maintenance check per AGENT.md §3) versus writing the ~140-line
rotate-and-prune logic directly, the second was chosen since it is small, has no hidden behaviour
to audit, and lets the writer sit directly behind the masking handler with nothing in between.
"3 files retained" is implemented as 3 rotated backups plus the active file (4 files on disk at
steady state: `tortui.log`, `.log.1`, `.log.2`, `.log.3`) — see DEC-025.

Masking (`mask.go`) wraps *any* `slog.Handler` — not just the one this package builds — via
`newMaskingHandler`, redacting: (1) any attribute, struct field, or map key whose name contains,
case- and separator-insensitively, `url`/`apikey`/`cookie`/`token`/`secret`/`password`/`passkey`/
`authorization`, recursing through `slog.Group` nesting, through attributes bound via
`Logger.With`, and — since the PR #3 QA remediation below — through everything reachable inside a
`slog.Any`-logged struct, map, slice, or pointer; and (2) URL-shaped substrings (including
embedded basic-auth credentials), `key=value`/`key: value` credential patterns, `Bearer TOKEN`
headers, and a finite list of well-known bare secret-token shapes (Stripe-style `sk-`/`sk_live_`
keys, GitHub `ghp_`/`github_pat_` tokens, AWS `AKIA...` keys, GitLab `glpat-` tokens, Slack
`xox?-` tokens, JWTs), found in free text — the log message itself, and any string-valued
attribute or nested string leaf regardless of its key. A `slog.LogValuer` is resolved before any
of this runs. Whole-value redaction was chosen over partial redaction (e.g. keeping a URL's
scheme+host) because an indexer URL commonly carries the API key or session cookie itself as a
query parameter (`?apikey=...`), so a partial mask keyed only on the attribute name could leave
the exact secret sitting in the part left visible — see DEC-026.

**PR #3 QA remediation (this task, same day).** QA failed the original PR on two points, both now
fixed:

1. `maskAttr` previously handled only `KindGroup` and `KindString`; a struct, map, slice, pointer,
   error, or `fmt.Stringer` logged via `slog.Any` arrives as `KindAny` and passed through
   completely untouched — confirmed by QA with a struct carrying a `URL` field with an embedded
   `?apikey=...`. `maskAny` (new, reflection-based, depth-capped at 8) now walks all of these:
   struct fields and map keys are checked with the same `isSensitiveKey` used for top-level
   attributes, slice/array elements and pointer targets are recursed into, and `error`/`Stringer`
   values are reduced to their formatted text and text-scanned. This is exactly the shape T-003's
   own acceptance criteria anticipate: the frozen `indexer.Result` type (AGENT.md §5) carries
   `SourceURL` and `Extra map[string]string`, and adapters in later tasks will log `Result` values
   — almost certainly via `slog.Any`, since it isn't a plain string.
2. A secret passed as a **bare string value under a non-sensitive key**
   (`slog.String("value", "sk-live-...")`) survived, because the only free-text scanning that
   existed was URL-shaped and `key=value`-shaped pattern matching, and a bare token matches
   neither. `knownSecretPrefixPattern` now catches a finite, deliberately narrow list of
   publicly-documented secret shapes (see above) even with no `key=`/URL wrapping.

**Residual gap — stated plainly, not glossed over.** Pattern- and key-name-based masking
*cannot*, in general, catch an opaque secret with none of the recognizable shapes above (no URL,
no `key=value`, no known vendor prefix, no JWT structure) logged as a bare value under a key name
that doesn't itself look sensitive. Since every tortui indexer is user-supplied (AGENT.md §2), a
given indexer's `api_key`/`cookie` value can be any opaque string its operator issued, with no
fixed shape to pattern-match — there is no reliable way to tell such a string apart from an
ordinary opaque identifier (an info-hash, a random ID) by inspecting the value alone, short of an
unacceptable false-positive rate. **The real defense for this case is procedural, not technical:**
any code that logs a credential-carrying value must do so under a key name (or struct
field/map key, now that those are walked too) containing one of `isSensitiveKey`'s substrings, so
the one guarantee that doesn't depend on guessing a secret's shape actually applies. This is
called out in `mask.go`'s package doc so it isn't lost, and needs to be treated as a real
constraint by T-020 onward, which will start logging `indexer.Result` and HTTP request/response
detail carrying exactly this kind of value. The PR body's and this tracker's earlier phrasing —
"masked in every log line" — overstated this; see DEC-026's cross-reference and the corrected
DEC-028 below for the acknowledgement.

`ResolveLevel(configLevel, flagLevel string) string` and `ResolveFile(configFile, flagFile
string) string` implement the config/env/flag precedence the acceptance list asks for — flag,
then the `TORTUI_LOG_LEVEL` / `TORTUI_LOG_FILE` environment variables, then whatever the caller
already resolved from config.toml, then (for level only) a built-in default of `"info"` applied
by `ParseLevel`/`New` when every tier is empty. **PR #3 QA remediation:** `cmd/tortui/main.go` now
declares `--log-level` and `--log-file` on its existing `flag.FlagSet` (alongside `--version` and
`--config`, both since T-001) and feeds their values through `ResolveLevel`/`ResolveFile` — with
the config-side argument hardcoded to `""` for now, since `internal/config.Config` still has no
`log_level`/`log_file` keys — then through `ParseLevel` for validation, exiting 1 on an
unparseable level. `internal/logging` itself still does not read `config.toml` or a
`flag.FlagSet` directly; only `cmd/tortui/main.go` changed, adding ~15 lines within AGENT.md §4's
~80-line budget for `cmd/`. DEC-028 originally justified deferring this to the (nonexistent)
`internal/app` composition root by analogy to T-002; that analogy was inaccurate (T-002 deferred
*consuming* an already-declared `--config` flag, never having to *declare* a new one) and DEC-028
has been corrected in place — see below. `internal/config.Config` is still unchanged; no
`config.example.toml` or `README.md` update is needed yet since neither a TOML key nor a
documented default exists to describe.

`TestNoStdoutStderrLeakAcrossSimulatedRun` (`logging_test.go`) is the direct test for "nothing is
ever written to stdout or stderr": it repoints `os.Stdout`/`os.Stderr` at pipes, logs at every
severity through both a directly-held `*slog.Logger` and the package-level `slog.Error` (which
only reaches the file sink because of the `slog.SetDefault` call inside `New`), across a plain
attr, a `slog.Group`-nested attr, an attr bound via `.With`, and a secret concatenated straight
into a message string — then asserts both captured pipes are exactly empty while confirming the
log *file* did receive the records with every secret redacted (so the empty streams aren't simply
because nothing was logged). `mask_test.go` drives the masking handler directly and parses its
JSON output to assert exact key-by-key redaction, including two levels of `slog.Group` nesting,
plus (added in the QA remediation) a struct/map/slice/pointer logged via `slog.Any`, an `error`
and a custom `fmt.Stringer`, a `slog.LogValuer`, each bare known-secret-prefix shape, a
`Bearer TOKEN` header, and a basic-auth URL under a non-sensitive key — alongside a regression
test proving an ordinary secret-free struct survives `slog.Any` unchanged. `rotate_test.go`
exercises rotation at a scaled-down threshold (real 10 MB/3-backup behaviour would make the test
slow) verifying the oldest backup is pruned, the file is reopened at zero size, an existing
on-disk file's size is honoured across a restart, and `Close` is idempotent while a write after
`Close` fails cleanly rather than panicking. `cmd/tortui/main_test.go` gained cases for
`--log-level`/`--log-file` acceptance, flag-over-env precedence, an invalid level's exit 1, and
the empty-everything default.

`go test ./internal/logging/...`, `go test ./...`, and `go test -race ./... -count=1` are green;
`internal/logging` now measures 89.4% local statement coverage (not gated by AGENT.md §9, whose
per-package thresholds name only `internal/indexer`, `internal/engine`, and `internal/tui`).
`make check` is green (`golangci-lint run` reports 0 issues).

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
status: done
depends: T-001
```
**Files:** `.gitleaks.toml`, `scripts/pre-commit`, `Makefile`, `.github/workflows/ci.yml`,
`CONTRIBUTING.md`

**Acceptance**
- Pre-commit hook blocks commits containing API-key-shaped strings, cookies, or
  `config.toml` itself.
- `make check` includes the scan.
- Documented in `CONTRIBUTING.md`.

**Notes:** `gitleaks` v8.30.1 (MIT; already verified installed locally) is the scanner.
`.gitleaks.toml` sets `[extend] useDefault = true` to inherit the built-in ruleset (AWS/GCP/
GitHub/GitLab/Slack tokens, JWTs, generic API keys, private keys, ...) and adds three
tortui-specific things: (1) a global `[allowlist]` scoping `internal/logging/mask_test.go`
by exact path — that file (T-003) deliberately contains secret-**shaped** fixture strings
(`sk-`, `ghp_`, AWS-style keys, JWTs) for the masking tests, none of them real; verified the
same strings at a different path are still flagged, so the allowlist is scoped, not a global
weakening (see DEC-032); (2) a custom `tortui-generic-cookie` rule, because the default
ruleset has no generic Cookie/Set-Cookie pattern — only vendor-specific ones like
`gitlab-session-cookie` — and "cookie" isn't in `generic-api-key`'s keyword list either (DEC-
033); (3) a path-only `tortui-config-toml` rule (no content regex, same pattern gitleaks'
own `pkcs12-file` rule uses) that flags any file named exactly `config.toml` regardless of
content, since a "boring" one with no secrets yet would otherwise pass content scanning
(DEC-031).

`scripts/pre-commit` (`#!/usr/bin/env sh`, POSIX only, executable) checks staged paths for a
literal `config.toml` by basename (`config.example.toml` unaffected) and separately runs
`gitleaks git --staged -c .gitleaks.toml`; it exits non-zero and prints an install hint if the
`gitleaks` binary is missing, rather than silently skipping the scan (DEC-030). Being tracked
in git does **not** make it run: `.git/hooks/` is never version-controlled, so a fresh clone
has no protection until `make hooks` (or `git config core.hooksPath scripts`) is run once —
stated plainly in `CONTRIBUTING.md` rather than implied.

`make check` gained a `scan` prerequisite (`make scan` = `gitleaks dir --no-banner --redact -c
.gitleaks.toml .`, a working-tree scan with no git-history walk) which likewise fails loudly
with an install hint if `gitleaks` is absent, rather than skipping. Since GitHub-hosted CI
runners don't ship `gitleaks`, `.github/workflows/ci.yml` now installs it the same way it
already installs `gofumpt`/`goimports`/`golangci-lint`: `go install
github.com/zricethezav/gitleaks/v8@v8.30.1` on all three OSes — **not**
`github.com/gitleaks/gitleaks/v8`, despite that being the GitHub org/repo name; the module's
own `go.mod` still declares `module github.com/zricethezav/gitleaks/v8`, and `go install
github.com/gitleaks/gitleaks/v8@v8.30.1` was tried first and fails with "version constraints
conflict ... module declares its path as: github.com/zricethezav/gitleaks/v8" (DEC-029).

Verified end to end: `make check` (fmt-check, lint, test, scan, vet) is green locally, and
`go test -race ./... -count=1` is green. The hook was exercised against three cases — an
AWS-access-key-shaped string, a generic session-cookie-shaped value, and a forced `git add -f
config.toml` — first in an isolated scratch git repo, then again (staged, observed rejected,
reset, never committed) against this repo's own working tree; all three were rejected with a
clear message and non-zero exit, and `config.example.toml` was confirmed unaffected. No
secret, real or fixture-shaped, was ever committed to this repository's history in the
process.

---

### T-005 · Cross-platform tooling baseline
```
status: done
depends: T-001
```
**Files:** `Makefile`, `scripts/`, `.github/workflows/ci.yml`, `cmd/tortui/main_test.go`,
`internal/config/load_test.go`

**Notes:** `Makefile` already had `SHELL := /bin/sh` and `.SHELLFLAGS := -eu -c` from T-001; audited
every line for GNU Make 3.82+/4.0+ syntax (`.ONESHELL`, `$(file ...)`, the `!=` assignment
operator, `&:` grouped targets) and found none — confirmed by grepping the whole file, not just by
inspection. Stronger than that: this development machine's `/usr/bin/make` **is itself GNU Make
3.81** (`make --version` → `GNU Make 3.81`), so every `make check` and `make build-all` run quoted
below already executed under the exact old-make constraint this task cares about, not merely
"reviewed and believed compatible."

**Correction (QA remediation of PR #5, same day): `.SHELLFLAGS` itself is a GNU Make 3.82+
feature and was inert the whole time on this development machine's make 3.81 — a false impression
the paragraph above (and the PR description) left standing by not saying so.** QA verified this
directly with a throwaway Makefile: on make 3.81, a non-final recipe line that ran `false` did not
stop the recipe, and a reference to an unset shell variable did not error — proving `-e` and `-u`
were both silently ignored, not merely unexercised. This was re-confirmed independently during
remediation with the same test against this machine's actual `/usr/bin/make`. Nothing in `make
check` or `make build-all` was actually broken by this, because no recipe happened to depend on
mid-recipe fail-fast or unset-variable behaviour — but the tracker and PR calling every green run
"already executed under the exact old-make constraint this task cares about" implied `.SHELLFLAGS`
was one of the things that constraint had exercised, which was never true. Fixed rather than just
documented: every recipe now starts its shell invocation with a literal `set -eu;`, a plain POSIX
shell builtin that does not depend on which flags make chose when invoking `$(SHELL)` — verified
with the same throwaway-Makefile method to give real `-e`/`-u` behaviour under make 3.81. `.SHELLFLAGS
:= -eu -c` is kept as-is (AGENT.md §14 asks for it, and it is not wrong to have — it just isn't
sufficient on its own on this project's floor make version); see DEC-037.

Added `make lint` → `shellcheck -s sh $(wildcard scripts/*)` (currently just `scripts/pre-commit`,
which is both the only script and the only git hook in the tree) after `golangci-lint run`, gated
the same way `make scan` gates on a missing `gitleaks` (T-004/DEC-029/DEC-030): fails loudly with
an install hint if `shellcheck` isn't on `PATH`, rather than silently skipping — see DEC-034.
`scripts/pre-commit` already passes `shellcheck -s sh` with zero findings; no changes to the
script itself were needed for this task, only the wiring.

Added `make build-all`: a `for` loop (POSIX `sh`, using `$${var%/*}`/`$${var#*/}` parameter
expansion, no bashisms) over the six targets from AGENT.md §14's support matrix
(`darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64`), building each
with `CGO_ENABLED=0 GOOS=... GOARCH=... go build` into `dist/tortui-OS-ARCH[.exe]` and exiting
non-zero the moment any one combination fails (`if ! go build ...; then exit 1; fi` inside the
loop) — see DEC-035 for why `CGO_ENABLED=0` is forced. Ran it locally; all six produced valid
binaries, confirmed with `file(1)` — see the verbatim output below. `dist/` was already gitignored
(T-001).

`.github/workflows/ci.yml`: added a `build-all` job (runs on every push/PR, `ubuntu-latest` only —
see DEC-036) running `make build-all`. Added `shellcheck` installation to the existing `check`
job's matrix, one step per OS (`apt-get install` on Linux, `brew install` on macOS,
`choco install shellcheck` on Windows) rather than assuming any runner image ships it already —
the same reasoning DEC-029 already applied to `gitleaks`. The pre-existing three-OS `make check`
matrix job is otherwise unchanged; nothing was duplicated or removed from it.

**Case-sensitivity and path-joining audit (existing tests, all four packages plus `cmd/tortui`):**
searched every `*_test.go` under `internal/config`, `internal/logging`, `internal/platform`, and
`cmd/tortui` for (a) manual `"/"`-concatenated paths in place of `filepath.Join` — zero hits, every
constructed path in the tree already goes through `filepath.Join` (`internal/config/load_test.go`,
`internal/logging/rotate_test.go`, `internal/logging/logging_test.go`); (b) any test relying on
two filenames differing only by case being treated as distinct — zero hits, no test in the tree
uses a mixed-case filename at all, checked by pattern-matching every quoted filename literal
against one containing an uppercase letter. One borderline case was found and fixed rather than
left ambiguous: `cmd/tortui/main_test.go`'s `TestRunLogFileFlag` passed `--log-file=/tmp/foo.log`
as a flag value — but `run()` never opens, joins, or otherwise touches that value as a filesystem
path (it only threads it through `logging.ResolveFile` and echoes it back as a string), so it
never exercised path-separator semantics at all. Still, a hardcoded Unix-style absolute path
sitting in a test is exactly the shape this criterion is checking for, so it was replaced with the
bare filename `custom.log` to remove any doubt, and the test's own comment now says why.

**Correction (QA remediation of PR #5, same day) — the closing sentence above, "No other
occurrence of a slash-containing literal exists in any test in the tree," was false, and the audit
that produced it was not exhaustive enough to catch a live bug.** QA found, and this remediation
independently reproduced, that `internal/config/load_test.go` hardcoded
`download_dir = "/tmp/tortui-downloads"` as TOML body content in two tests —
`TestLoadValidConfig` (then line 84) and `TestLoadUnknownKeyIsReported` (then line 216) — and that
literal is not inert: `Load()` (`internal/config/load.go`) calls a real `os.MkdirAll(cfg.DownloadDir,
0o755)` on whatever `download_dir` decodes to, unconditionally, regardless of whether that path is
inside the test's own `t.TempDir()` sandbox. Both tests fed it a path *outside* the sandbox, so
every run of `go test ./internal/config/...` actually created `/tmp/tortui-downloads` on the real
filesystem and left it there — confirmed live: the directory was still present on disk
(`ls -la /tmp/tortui-downloads`, owned by the account that ran a prior `go test`) before this
remediation touched anything. The original audit's category (a) check — "manual `/`-concatenated
paths in place of `filepath.Join`" — was the wrong test for this bug: the literal was never
concatenated with anything, it was a whole hardcoded absolute path handed straight to a real
`os.MkdirAll` from inside a TOML fixture string, a shape the audit's pattern search never
targeted. **Fixed:** both tests now build `download_dir` from `filepath.Join(home, ...)`, where
`home` is that test's own `t.TempDir()`, and embed it in the TOML body as a single-quoted TOML
literal string (`download_dir = 'PATH'`) rather than a Go double-quoted one, so a Windows path's
backslashes need no TOML escaping. Re-audited the remaining two hits of the same shape found by a
fresh, broader grep (`grep -rn '"/tmp' --include='*_test.go'`, plus a scan of every quoted
`/`-containing literal in every `*_test.go` in the tree): `internal/config/validate_test.go`'s
`Default("/tmp/downloads")` and `c.DownloadDir = "/custom/downloads"` are not the same defect —
both flow only into `Config.Validate()`, a pure function with no filesystem access, never into
`Load()`, so neither ever touches disk. The `internal/platform/paths_{darwin,linux}_test.go` fake
`HOME` values (`/Users/alice`, `/home/alice`, etc.) were also re-checked against `paths_darwin.go`
/ `paths_linux.go`: `ConfigDir`/`StateDir`/`DownloadDir` there are pure `filepath.Join` string
computations with no `os.MkdirAll` or other I/O, so no `/Users/alice` directory is ever created.
No other test in the tree calls a function that both accepts a hardcoded path literal and performs
real filesystem I/O with it outside a `t.TempDir()`. That is now a personally-verified negative,
not an assumed one — the entire `*_test.go` list in this repo is nine files, all read in full for
this remediation.

**Verification.** `make check`, `go test -race ./... -count=1`, and `make build-all` are all green
— verbatim output is in the PR body. What *was* done at review time: audited every Makefile line
for the documented 3.82+/4.0+ feature list, wrote every recipe in POSIX `sh` with no bash-specific
parameter expansion beyond what `dash`/`ash` also support, and pushed CI's new `windows-latest` job
(`choco install make`, the pre-existing mechanism from T-001/DEC-019) so the three-OS `check`
matrix — including `shellcheck`'s new Windows install step — would get real coverage once it ran.
At review time that job was still `IN_PROGRESS`, so the Windows/Git Bash criterion could not yet
be counted as met by it. `make build-all` itself runs only on `ubuntu-latest` in CI, by design
(DEC-036), so it never independently confirms Windows/Git Bash behaviour either way.

**Update (QA remediation of PR #5, same day): the `windows-latest` `make check` job has since
completed and passed, discharging this criterion with real evidence.** GitHub Actions'
`windows-latest` runners resolve a workflow step's `shell: bash` to Git for Windows' bundled bash —
genuinely Git Bash, not a substitute — so this job is valid evidence for "targets run unmodified on
Windows under Git Bash," confirmed by reading the completed run rather than assumed: checked
`gh api repos/kdta91/tortui/commits/7e388c295dc915820f50cc43444a933f3cfbcfaa/check-runs` once
(no polling), which returned run id `34701713965` (triggered by the `pull_request` event on
`task/T-005-xplat-tooling` at that exact commit) with job `make check (windows-latest)` ->
`status: completed`, `conclusion: success`, alongside the `ubuntu-latest`/`macos-latest` `check`
jobs and the `build-all` job, all also `success`. This remediation's own new commit will trigger
its own fresh CI run, which was not polled or waited on as part of this task.

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
status: done
depends: T-001
```
**Files:** `LICENSE`, `NOTICE`, `Makefile`, `.github/workflows/ci.yml`, `README.md`

**Acceptance**
- `LICENSE` at repo root — MIT, correct copyright holder and year.
- `NOTICE` enumerates every direct and transitive dependency with its license, generated by
  `go-licenses` rather than hand-written.
- `make licenses` regenerates `NOTICE`; CI fails if it is stale or if any dependency is
  GPL/AGPL/LGPL or unlicensed (AGENT.md §3).
- README license section matches the actual `LICENSE`.

**Notes:** `LICENSE` is the standard MIT text, copyright holder `kdta91` (the repo owner's GitHub
handle — see DEC-038 for why a handle was used instead of a legal name), year 2026.
`go-licenses/v2` (`github.com/google/go-licenses/v2@v2.0.1`) was not installed in this
environment; installed it via `go install` and verified it actually runs before relying on it —
see DEC-038 for what was verified and why a single-GOOS run undercounts here. `make licenses`
(new Makefile target) checks `--allowed_licenses=MIT,Apache-2.0,BSD-2-Clause,BSD-3-Clause,ISC`
(AGENT.md §3/§16) via `go-licenses check`, once per GOOS in `darwin linux windows`, `--ignore`ing
the main module, then regenerates `NOTICE` from `go-licenses report` merged the same way,
deduplicated with `sort -u`, following the T-004/T-005 precedent of failing loudly (install
hint, non-zero exit) rather than skipping silently when `go-licenses` isn't on `PATH`. Verified
directly, not assumed: `go-licenses check` with the real allowlist passes (exit 0); it fails
(exit 1, naming the library and license) when re-run with `MIT` deliberately removed from the
allowlist, proving the disallow path actually triggers. `NOTICE` currently lists exactly two
entries, both MIT: `github.com/BurntSushi/toml` and `github.com/adrg/xdg` — confirmed by
re-running `make licenses` twice back-to-back on this machine and diffing the output
(byte-identical). CI gets a new `licenses` job (ubuntu-latest only, like `build-all` — the
Makefile target already loops all three GOOS internally, so one host suffices): installs
`go-licenses`, runs `make licenses` (which itself fails the job on a disallowed license), then
`git diff --exit-code -- NOTICE` to fail on a stale file. README's License section already read
"MIT. See `LICENSE`." before this task (T-001 wrote it ahead of the file existing) and needed no
correction; added one sentence pointing at the new `NOTICE` file. `make check` and
`go test -race ./... -count=1` were verified green locally before opening the PR.

**Correction, QA remediation of PR #6 (2026-09-13, see DEC-039):** the original version of this
row's last sentence claimed `make licenses` was also "verified green locally before opening the
PR" without qualification, and separately implied that the two-back-to-back-runs diff above
"confirmed determinism." Both overstated what had actually been checked. `make licenses` was
verified green only on this macOS development machine, under its ambient `en_US.UTF-8` locale —
it was never run against the PR's actual CI runner (`ubuntu-latest`) before this row was marked
`done`, and it failed there unconditionally, on every run, regardless of whether any dependency
had changed. Re-running the same command twice back-to-back on one machine proves only
same-process, same-environment idempotency; it cannot and did not catch a bug that only manifests
when the *locale* differs between the machine that generated `NOTICE` and the machine that checks
it, which is exactly what broke. See DEC-039 for the root cause and the fix.

---

### T-007 · Contribution policy and repo hygiene
```
status: done
depends: T-006
```
**Files:** `CONTRIBUTING.md`, `.github/ISSUE_TEMPLATE/`, `.github/PULL_REQUEST_TEMPLATE.md`, `CODE_OF_CONDUCT.md`,
`scripts/check-indexer-hostnames.sh`, `docs/indexer-hostname-allowlist.md`, `Makefile`,
`.github/workflows/ci.yml`

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

**Notes:** `CONTRIBUTING.md` (which already existed from T-004, documenting the secret-scan hook)
gained three new sections rather than being replaced: "Indexer sources are user-supplied only",
explaining the closed-without-review policy in the agent's own words — *MGM v. Grokster*
inducement liability turning on the developer's own choices rather than the code's capabilities,
and the 2020 youtube-dl/RIAA episode where the project's own test fixtures naming real copyrighted
tracks became one of the case's factual hooks, resolved only after those references were stripped
— "Site names don't belong anywhere in this repo — including issues and PRs", and a restated PR
checklist. `CODE_OF_CONDUCT.md` is the standard Contributor Covenant v2.1 text (CC BY 4.0,
attributed at the bottom per its own template) with the enforcement-contact section filled in as
"contact the maintainer (@kdta91) through GitHub" — deliberately **not** a link to GitHub's private
security-advisory reporting flow, since `gh api repos/kdta91/tortui/private-vulnerability-reporting`
was checked directly and returned `{"enabled":false}` for this repository; linking a flow that
isn't actually enabled would have been exactly the kind of unverified claim this project's QA has
been failing tasks on.

`.github/ISSUE_TEMPLATE/bug_report.md` asks for `tortui doctor` output but words it as
conditional/forward-looking rather than asserting the command exists: "if your build includes
it... `doctor` isn't implemented yet as of this writing — it lands in T-055 — so if your build
predates it, please give us instead: OS/arch, terminal emulator and version, ...". It also asks
for terminal emulator/version explicitly and tells the reporter not to paste indexer URLs, keys,
cookies, or torrent titles, with the reasoning link to `CONTRIBUTING.md`.
`.github/ISSUE_TEMPLATE/feature_request.md` and `.github/ISSUE_TEMPLATE/config.yml` (a contact
link back to the contribution policy) carry the same "no site names" note; both templates and
`.github/PULL_REQUEST_TEMPLATE.md` were validated with `ruby -ryaml` /
`gh issue`-template-shape by eye (front matter parses; `config.yml` parses as YAML with
`ruby -ryaml -e "YAML.load_file(...)"`, confirmed directly). The PR template's checklist matches
this acceptance criterion's five items verbatim.

**The hostname CI check (the hard part of this task).** T-024 (bundled lawful sources) doesn't
exist yet, so there is no real allowlist to check against, and AGENT.md §2 forbids naming an
infringement-oriented site anywhere in this repository — including in a blocklist, a regex, or a
fixture — so this could never be built as a denylist of known piracy hostnames. What was built
instead, in `scripts/check-indexer-hostnames.sh` (POSIX `sh`, passes `shellcheck -s sh`, wired into
`make lint`'s existing `$(SHELLCHECK_SOURCES)` wildcard automatically since it lives in
`scripts/`), is a **shape-plus-context heuristic**, not a name list:

1. *Shape*: a substring of an added diff line that looks like a hostname — the authority of an
   `http(s)://` URL (userinfo and port correctly stripped, so `https://user:pass@host` extracts
   `host`, not `user`), or the value of a `url`/`host`/`endpoint`/`base_url`-style key.
2. *Context*: that hostname-shaped substring only counts on a line that either lives in a file
   where tortui actually defines indexer sources (`internal/indexer/**`, `config.example.toml`,
   `docs/indexer-definitions.md`, `docs/bundled-sources.md`,
   `docs/indexer-hostname-allowlist.md`, or `testdata/**`) or mentions `indexer`, `torznab`,
   `scraper`, or `base_url` on the same line. A hostname anywhere else in the diff — a README
   link, a CI action reference, a `NOTICE`/`go.sum` dependency URL — is never even inspected.
3. What survives both filters is allowed only if it is a **structurally non-resolving
   placeholder** — `localhost`, an IP literal, or a host ending in one of the RFC 2606 / RFC 6761
   reserved labels `example`/`test`/`invalid`/`localhost` (covers both `example.com` and a
   fixture like `real-indexer.example`) — or is listed in the new
   `docs/indexer-hostname-allowlist.md`, which is intentionally empty of indexer hosts right now
   (see its own header) and is the single, documented place **T-024** adds bundled-source hosts
   to.

Wired as a new `indexer-hostnames` job in `.github/workflows/ci.yml`, `if:
github.event_name == 'pull_request'`, checked out with `fetch-depth: 0` and invoked as
`scripts/check-indexer-hostnames.sh "${{ github.event.pull_request.base.sha }}" "${{
github.event.pull_request.head.sha }}"` — both SHAs pinned rather than branch names, since a
force-push can move a branch tip out from under a second read. A `make check-hostnames` target
was also added for local use (`HOSTNAME_BASE_REF ?= origin/main`), but it is **not** wired into
`make check` or the `check` CI job, matching how `licenses` and `build-all` already stand apart
from `check` — a diff-based check has nothing meaningful to compare against outside a PR context,
and gating every local commit on a possibly-stale fetched `origin/main` would be exactly the kind
of local-only-green claim this project's QA has bounced other tasks for.

**Corrected 2026-09-13 (QA remediation of PR #7) — the "branch protection was not touched" claim
above was wrong in effect, not just in wording, and is corrected here rather than silently edited
away.** The acceptance criterion says the CI check "fails the PR." QA read live branch protection
and found `required_status_checks.contexts` held exactly the three `make check (OS)` jobs;
`indexer-hostnames`'s context was absent. This repo merges via `gh pr merge --auto`, and GitHub
auto-merge only waits on *required* checks — so a red `indexer-hostnames` run blocked nothing, and
the acceptance criterion was not actually met regardless of how the job itself behaved. The
original reasoning ("AGENT.md doesn't ask this task to change branch protection and QA review is
what actually gates the merge") is not a defense against this: `gh pr merge --auto`, not a human
watching the checks tab, is what actually merges here, and auto-merge does not consult
non-required checks at all. Fixed by adding `check indexer hostname allowlist (T-007)` — the
exact context string, confirmed from a real completed check run on this PR's own head commit
(`gh api repos/kdta91/tortui/commits/HEAD_SHA/check-runs`, `conclusion: success`, triggered by
the `pull_request` event) rather than assumed — to `required_status_checks.contexts` via
`gh api -X PUT .../branches/main/protection` (the `.../required_status_checks` sub-resource
endpoint 404s for this repo for reasons not fully diagnosed; the full-protection endpoint works
and every other field — `strict: true`, `allow_force_pushes: true`, `allow_deletions: false`, and
the rest — was carried forward explicitly from a prior read to avoid the PUT silently resetting
anything unspecified, which a first attempt at this did do to `allow_force_pushes` before being
caught and corrected). `required_pull_request_reviews` remains unset (AGENT.md §10 still forbids
requiring a PR on `main`). Read back and confirmed: four contexts, `strict: true`, no
`required_pull_request_reviews` key. **Deadlock check:** since `indexer-hostnames` only runs
`if: github.event_name == 'pull_request'`, every future push to an open PR branch re-triggers a
`pull_request` (synchronize) event and re-runs the job on the new head SHA — confirmed this already
happened once for this exact PR (one `push`-triggered run where the job legitimately reports
`skipping`, and one `pull_request`-triggered run on the same SHA where it reports `success`), so
requiring the context does not strand this PR waiting on a context that never reports for a
`pull_request` event. See DEC-042.

**Verification, all done directly rather than assumed:**
- `make check` is green (fmt-check, lint incl. `shellcheck -s sh` over both `scripts/*` files,
  test, scan) and `go test -race ./... -count=1` is green — verbatim output in the PR body.
- `scripts/check-indexer-hostnames.sh` was run against this PR's own diff
  (`scripts/check-indexer-hostnames.sh origin/main`, after `git add -A` so the new files are
  visible to `git diff`) and reported **zero** violations — the policy prose itself (which
  necessarily uses the word "indexer" next to explanatory text) does not trip the checker.
- It was run from the empty git tree (`git hash-object -t tree /dev/null`) to `HEAD` — i.e. every
  tracked file in the repository as it exists today, not just this PR's diff — to prove it does
  **not** false-positive on the existing tree. The first run of this found two real bugs, both
  fixed and covered by the same re-run afterward: (a) `internal/logging/logging_test.go` and
  `mask_test.go`'s existing `real-indexer.example` fixture hostname (a T-003 fixture, itself
  already following the "invented name" convention) was flagged, because the allowlist only
  special-cased `example.com`/`.net`/`.org`, not the bare `.example` TLD — fixed by adding the
  RFC 2606/6761 reserved-TLD check described above; (b) the same run's basic-auth fixture
  (`https://user:BASICAUTH-SECRET@real-indexer.example/path`) extracted `user` as a fake
  "hostname" — fixed by stripping userinfo before further parsing. After both fixes, the
  empty-tree-to-`HEAD` run is clean (exit 0, zero violations).
- It was run against a synthetic addition of `https://totallyrealtorrentsite.suspicious-tld.zzz`
  inside `config.example.toml`'s `[[indexer]]` block (single-rev mode, `... HEAD`, uncommitted) and
  correctly failed (exit 1, naming the file and host) — then the same host inside
  `internal/indexer/scraper/builtin/*.go` in a disposable scratch git repo, in **both** invocation
  modes: single-rev against the working tree, and two-rev (`base-sha head-sha`) against two real
  commits, matching exactly how the CI job invokes it. Both modes correctly failed on the new host
  and correctly passed once that same host was added to a scratch allowlist file instead. A
  control case — the same disallowed hostname added to a plain `README.md` line with no indexer
  context — correctly produced **zero** violations, confirming the check is not simply "any new
  hostname anywhere," which would have been a much blunter (and much noisier) instrument than the
  acceptance criterion asks for.
- Confirmed no secret-shaped content was introduced: `make scan` (gitleaks) is part of `make
  check` and stayed green throughout, including the commit that added the intentionally-fake
  basic-auth test hostname (`totallyrealtorrentsite.suspicious-tld.zzz`, `some-new-indexer-host.zzz`
  — invented names, never committed to this repo; those probes were run against uncommitted
  working-tree edits or a disposable scratch repository outside this project and reverted before
  committing).

**QA remediation of PR #7 (2026-09-13) — three more fixes to the checker itself, beyond the
branch-protection correction above:**

- **Case-sensitivity bug (finding 2), fixed.** `scan()`'s keyword-shaped match
  (`url|host|endpoint|base_url`) was matched against the diff line's *original* case, while
  `check_host()` lowercases the extracted host anyway — so idiomatic Go struct-field style keys
  (`Host: "..."`, `BaseURL: "..."`, capitalized, exactly as Go reads) inside `internal/indexer/**`
  were never caught, while a lowercase `host = "..."` was. Fixed by scanning the already-lowercased
  copy of the line (`scan(lower, file, linetext)`) instead of the original-case text, keeping the
  original-case text only for the printed violation context. Regression test:
  `scripts/check-indexer-hostnames_test.sh` (wired into `make check` via the new `test-scripts`
  target — see the Makefile), which builds a disposable scratch git repo and asserts a capitalized
  `Host:`/`BaseURL:` pair naming an invented host is flagged, that the same shape using a
  reserved-TLD placeholder is not (control case, so the fix isn't "flag anything capitalized"),
  that a private IPv4 literal in the same shape is not flagged, that a public one is, and that a
  hostname named only in a commit message is flagged.
- **DEC-040's IP-literal reasoning (finding 3), corrected, not just reworded.** The original
  reasoning — an IP literal "can never be a real source by construction" — is false: unlike an
  RFC 2606/6761 reserved TLD, a bare IP address can absolutely be a real production endpoint.
  `is_allowed()` now only auto-allows a **private/loopback/link-local** IPv4 literal (`10.0.0.0/8`,
  `172.16.0.0/12`, `192.168.0.0/16`, `127.0.0.0/8`, `169.254.0.0/16`, `0.0.0.0/8`); a public IPv4
  literal falls through to the same allowlist every other hostname does. `docs/indexer-hostname-
  allowlist.md` and the script's own header comment were updated to match. See the corrected
  DEC-040 row below. (A pre-existing, separate limitation, documented rather than fixed here: a
  bare IP with no `http(s)://` scheme in a `key: "value"`-style line isn't matched by the
  value-shape regex at all, since that regex requires an alphabetic-looking TLD tail — an IP only
  trips the check today after a scheme. Fixing that would mean loosening the value-shape regex to
  match a trailing numeric label, which risks matching arbitrary "word.number" prose; left as a
  known gap rather than risking a noisier check under time pressure.)
- **Known gaps (finding 4), triaged:** of the four QA named, (d) — commit messages never
  scanned — is now **closed**: AGENT.md §2 names commit messages explicitly alongside diff content,
  and closing it was cheap (`git log --format=%B RANGE` appended to the diff as synthetic `+`
  lines under a pseudo file that never matches a gated path, so a message only trips the check via
  the existing keyword-context rule). The other three are **documented, not closed**, in the
  script's own header comment and left as-is on purpose:
  - (a) a hostname in a new file's *path* (never its content) is never scanned — low value (a
    contributor encoding a real hostname as a filename is an unlikely way to introduce one) against
    the complexity of parsing arbitrary path segments as candidate hostnames.
  - (b) a hostname split across concatenated string literals on separate lines evades the
    line-based scan — closing this needs a join-adjacent-added-lines heuristic that would flag most
    unrelated pairs of consecutive added lines as false positives; not attempted.
  - (c) a scheme'd URL on a line with no gated path and no keyword (e.g. wiring code under
    `internal/app/`) slips through — this is the deliberate context-gating trade-off DEC-040/041
    already made and tested (the tracker's own verification notes above show the control case: the
    same disallowed host on a plain `README.md` line with no indexer context correctly produced zero
    violations). Closing (c) would mean reverting that trade-off and reopening the false-positive
    problem it was built to avoid. The check is a lightweight net, not a substitute for the human
    review `CONTRIBUTING.md` still asks for — now said outright in the script's header rather than
    left implicit.

**What this task deliberately leaves for T-024:** `docs/indexer-hostname-allowlist.md` ships with
no indexer hostnames listed at all — there is nothing bundled yet to list. The file's own header
states exactly what T-024 should add and where.

---

## Phase 1 — Domain contracts

### T-010 · Indexer contracts
```
status: done
depends: T-002
```
**Files:** `internal/indexer/indexer.go` (plus `internal/indexer/indexer_test.go`)

**Notes:** `internal/indexer` is a new package holding only the AGENT.md §5 contracts —
`Trust`, `Mode`, `Query`, `Result`, `Caps`, `Indexer` — with no adapter, no registry, and no
imports beyond `context`, `errors`, `fmt`, `strings`, `time`. Type names, field names, field
order, field types, and iota order were transcribed from the §5 code block and then read back
against it line by line; the only deviations from the literal text of that block are gofumpt's
alignment of the `Caps` fields (which §5 writes unaligned) and per-field doc comments replacing
§5's trailing `//` comments, both of which preserve every identifier and its position.

**What was introduced beyond the §5 text, and why (this is what T-011 inherits):** §5's `Query`
and `Result` both reference a `Category` type that §5 itself never defines and that T-011 owns
(`internal/indexer/category.go`, not created here). To make this task's single file compile
without building ahead, `indexer.go` declares the bare type and nothing else:

```go
type Category int
```

No constants, no `CategoryOther`, no `String()`, no mapping helpers — all of that is T-011's,
and the type's godoc says so explicitly and names the file. T-011 can either add its `const`
block in `category.go` referring to this declaration (same package, compiles as-is) or relocate
the three-line declaration into `category.go`; a move within one package changes no contract and
needs no `DEC-` entry. The zero value is deliberately left unclaimed here so T-011 can define it
as `CategoryOther` per its own acceptance criteria and AGENT.md §13. See DEC-043.

**`Trust.String()` vs `Trust.Badge()`** are two different renderings on purpose (DEC-044).
`String()` returns lowercase, case-uniform tokens — `unknown` / `none` / `verified` / `trusted`
/ `vip` — as the identifier form for logs, test output, and any later filter syntax. `Badge()`
returns the AGENT.md §7 table cell — `VIP` / `TR` / `✓` / `""`, with `""` for both
`TrustUnknown` and `TrustNone`. An out-of-range `Trust` renders as `trust(N)` from `String()`
(diagnosable) and as `""` from `Badge()` (so an unexpected number can never widen a fixed-width
column); both are tested at `Trust(99)` and `Trust(-1)`. `Badge()` returns the **Unicode** `✓`
and has no terminal-capability awareness — the AGENT.md §14 ASCII fallback is the TUI theme
layer's job and is deliberately *not* started here.

**`Result.Validate()`** checks exactly the one thing the acceptance criterion names — that at
least one of `Magnet` / `TorrentURL` is set — and nothing else, because every other field is
legitimately empty for some source (no size, no date, no uploader, no details page), so
validating them would reject perfectly displayable results. A whitespace-only value does not
count as set (`strings.TrimSpace`), since a magnet of `"   "` is neither a link nor something a
caller would want to hand the engine. Failures wrap an exported sentinel, `ErrNoLink`, inside
`fmt.Errorf("indexer %q: result %q: %w", ...)` so callers can `errors.Is` it while the message
still names the source and the item (AGENT.md §6.9); the test asserts both the `errors.Is` match
and the presence of the two ids in the message.

**Godoc** is on every exported identifier — package, both enums and all seven of their constants
individually, all three structs and each of their fields, `ErrNoLink`, `Validate`, `String`,
`Badge`, the `Indexer` interface, and each of its five methods — and states adapter obligations
rather than restating the field name: `Trust` is display-only and never gates behaviour (§2),
`TrustUnknown` must not be collapsed into `TrustNone`, `Caps` must be honest and constant,
`ID`/`Name`/`Caps` do no I/O and are concurrency-safe, `Search`/`Resolve` honour the ctx deadline
and are safe for concurrent fan-out, errors are wrapped to name the source and nothing panics,
and neither method may call anything but the source the user pointed it at (§2, no telemetry).

**Verification.** `make check` green and `go test -race ./... -count=1` green, both **on macOS
only** — the Linux and Windows CI legs are not reproducible on this machine and are NOT verified
locally; the PR's own CI run is the evidence for those. `go test ./internal/indexer/ -cover`
reports **100.0% of statements** (the §9 floor for this package is 75%); the only executable
statements in the package are `String`, `Badge`, and `Validate`, all exhaustively covered
including their out-of-range and zero-value paths. `scripts/check-indexer-hostnames.sh
origin/main` exits 0 on the committed diff. Tests also pin the two zero values the rest of the
codebase will rely on (`Trust(0) == TrustUnknown`, `Mode(0) == ModeSearch`), the full
least-to-most-trusted iota ordering that the §7 sortable trust column depends on, and a
compile-time `var _ Indexer = fakeIndexer{}` proving the interface is implementable as declared
plus a check that a no-op `Resolve` returns the result unchanged. The test fixture is an invented
source (`example-archive`, `archive.example.org`) with a deliberately all-zeros infohash, per §2.

**Deliberately not done here:** no `category.go` (T-011), no registry or `SearchAll` (T-012), no
`Mode.String()` (nothing needs one yet; T-012's cache key may add it), no ASCII glyph fallback
(TUI), and no `Validate` checks beyond the link.

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
status: done
depends: T-010
```
**Files:** `internal/indexer/category.go` (plus `internal/indexer/category_test.go`; a
deletion-only change to `internal/indexer/indexer.go`)

**The taxonomy classifies the kind of data, never the subject matter.** The buckets are
`CategoryOther` (the zero value), `CategoryAudio`, `CategoryVideo`, `CategoryImage`,
`CategoryText`, `CategorySoftware`, `CategoryData` — six broad buckets plus the catch-all. Four
of the names (`audio`, `image`, `text`, `video`) are also the IANA top-level media type names;
the registry at https://www.iana.org/assignments/media-types/media-types.xhtml was fetched on
2026-09-13 and its top-level sections are `application`, `audio`, `example`, `font`, `haptics`,
`image`, `message`, `model`, `multipart`, `text`, `video`. `Software` and `Data` are **not** IANA
names — they are ours, because IANA's `application` is a catch-all that would swallow both, and
because the sources AGENT.md §2 lets tortui bundle (public archives, research dataset
repositories, distro release listings) are exactly software and datasets. No bucket names a
genre, a medium, a scene tag, or any kind of material, so the enum reveals nothing about what
the app is for (AGENT.md §2, §16). A test, `TestCategoryBucketSetIsClosed`, is the tripwire on
that closed set. It reads the bucket set off the implementation rather than off a second list
kept in the test file: it parses the `Category` iota const block out of the package's own
non-test source with `go/parser`, then checks the declared constant identifiers, and the tokens
`Category.String` renders them as, against the `wantNames`/`wantTokens` lists in the test — the
only hand-maintained expected values left. A second pass over the same files then fails the test
on any constant or variable *written with the type* `Category` — in a mixed const block, as a
`var`, in a neighbouring file, or inside a function body — that the iota block did not declare,
so a bucket cannot be introduced beside the enum instead of in it. So adding a bucket fails the
build whether or not a
`String` case was added with it, and so do removing one (also a compile error), renaming the
constant (also a compile error), and renaming its `String` token — and the failure message says
what the rule is. (The tripwire is a closed-set check rather than a list of forbidden content
words, because writing that list would put the very words §2 keeps out of this repository into a
test file.) **What the tripwire does not reach, stated rather than implied:** the type match is
syntactic, so a declaration that never writes the word `Category` is invisible to it — a const
typed through an alias, an untyped const whose value is a `Category(7)` conversion, or a bare
`Category(7)` used inline with no declaration at all. All three were probed and confirmed still
green; DEC-051 explains why closing them with `go/types` was rejected. And the guard is a
backstop against an *accidental* addition, not a security boundary: it is a test file in this
repository and anyone deliberately adding a bucket can edit it. Both of the tripwire's earlier
versions overclaimed and QA caught both: as originally pushed in PR #9 it compared two lists that
both lived in the test file and so did **not** trip on an added bucket (DEC-046 records that false
claim and its correction), and the round-1 fix then failed open on any `Category` constant that
was not the first spec of its const block (DEC-050 records that one). See DEC-046, DEC-050 and
DEC-051.

**Torznab numeric mapping is by 1000-block, and the block numbers were read, not recalled.**
`CategoryFromTorznab(id)` looks up `id/1000`. The blocks were verified on 2026-09-13 against two
sources, cited by repository and path rather than by URL so the T-007 hostname check stays
meaningful: the newznab API specification, section 3 "Predefined Categories"
(`docs/newznab_api_specification.txt` in the `nZEDb/nZEDb` repository on GitHub, branch `dev`),
which gives the ranges `0000-0999`, `1000-1999` … `7000-7999`, then `8000-99999` reserved and
`100000-` site-specific custom; and `src/Jackett.Common/Models/TorznabCatType.cs` in the
`Jackett/Jackett` repository on GitHub, branch `master`, which is the numbering most Torznab
servers actually emit and which agrees on the parent ids `1000/2000/3000/4000/5000/6000/7000`
while additionally using `8000` as its own catch-all where the specification leaves that range
reserved. Both files were downloaded and read in this
session; the spec's range table and Jackett's `ParentCats` list were quoted from directly.
Blocks 0 and 8 therefore both map to `CategoryOther`, which covers both conventions. Only the
block *numbers* appear in the code — the names those two sources give the blocks are
subject-matter labels, and copying them into this repository is what §2 forbids, so each block
is written down as the kind of data its items are and nothing else. See DEC-047.

**Both helpers are total.** `CategoryFromTorznab(int)` and `CategoryFromString(string)` return a
`Category` and no error, and every input path ends at a defined bucket — `CategoryOther` when
nothing matches. Nothing is dropped and nothing errors, per AGENT.md §13. `CategoryFromString`
lowercases the label, splits it on every rune that is not a letter or digit, and returns the
first token in a short lookup table, so `Movies/HD`, `PC > Games`, `[Books]`, and `  audio  `
all resolve; a word that merely *contains* a token (`audiophile`) does not match, and a numeric
label is deliberately **not** routed to the Torznab helper. The word table holds the canonical
`String()` tokens plus the everyday words sources label with; some of those name subject matter
because that is what sources write, but every one of them collapses *into* a data-kind bucket,
which is how a source's taxonomy gets discarded at the boundary rather than adopted. See
DEC-048.

**`type Category int` was relocated** from `indexer.go` into `category.go`, the option DEC-043
and the type's own godoc offered. The change to `indexer.go` is a pure deletion — 15 lines
removed, 0 added (`git show --stat`) — of the declaration and the now-false T-010 godoc that
said the enum "has not landed yet". The six frozen AGENT.md §5 contracts (`Indexer`, `Query`,
`Result`, `Caps`, `Trust`, `Mode`) are untouched: `Category` is referenced by §5 but never
defined there, so its definition was never frozen (DEC-043 says so explicitly). A within-package
move changes no contract. See DEC-049.

**Verification.** `make check` green and `go test -race ./... -count=1` green, both **on macOS
only** — the Linux and Windows CI legs are not reproducible on this machine and are NOT verified
locally; the PR's own CI run is the evidence for those. `go test ./internal/indexer/ -cover`
reports **100.0% of statements**, unchanged from T-010 (the §9 floor for this package is 75%);
note that `make cover`'s threshold is 0 and enforces nothing (backlog T-916), so this number
comes from running the tool, not from a gate. Beyond the table tests, `go test -fuzz
FuzzCategoryFromString -fuzztime 30s` ran 18,630,338 executions and `-fuzz FuzzCategoryFromTorznab
-fuzztime 20s` ran 19,801,798, both PASS with no crashers and no corpus files added to the repo;
the two fuzz targets also run their seed corpus under a plain `go test`.
`TestCategoryFromTorznabIsTotal` sweeps every id from -20000 to 120000. Hostile string inputs covered explicitly: empty,
whitespace-only, punctuation-only, mixed case, non-ASCII (Greek, CJK), emoji, combining marks,
an embedded NUL, invalid UTF-8, and a 100 001-token label.

**The tripwire was proved by experiment, not asserted** (QA remediation of PR #9). Five edits were
made to `category.go` one at a time with `category_test.go` untouched, and the result of each
recorded: a content-specific bucket added *with* a `String()` case — RED, the added bucket named in
the failure output; the same bucket added *without* a `String()` case — RED (this is the case a
`String()`-walk-only guard misses, and `golangci-lint run` plus `go vet` were confirmed to report
nothing on it, so it is an ordinary edit); a bucket removed — build failure; a constant renamed —
build failure; a `String()` token renamed — RED. Every probe was reverted and `git status` confirmed
clean after each. See DEC-050.

**And re-proved again after QA round 2** (QA remediation round 2 of PR #9). QA's bypass — a new file
in the package declaring a bucket constant as the *second* spec of a mixed const
block plus an `init` wiring it into `categoryWords`, with `category.go` and `category_test.go` both
untouched — was first reproduced on the shipped code (`go test ./internal/indexer/` → `ok`,
`golangci-lint run` → `0 issues.`, and `CategoryFromString("probebucket")` returning value 7), and
then re-run against the fix, where it is RED. The same is true of the two related shapes QA
reported: a `Category` const in a mixed block with no `init`, and `var CategoryProbeBucket
Category = 7`. Three further shapes not specifically coded for were probed too: a function-local
`const ... Category = 7` — RED; a multi-name spec `const a, CategoryProbeBucket Category = 6, 7` —
RED; and `const CategoryProbeBucket = Category(7)` (untyped, conversion value) — **still green**,
reported rather than hidden, alongside the alias and bare-conversion shapes in DEC-051. The five
round-1 cases and the three exotic shapes QA had confirmed fail closed (explicit-value own block,
a second pure-iota block, an `iota + 7` block) were all re-run and all still fail closed. Every
probe reverted; `git status` clean and `category.go`'s checksum restored after each. (The probe
bucket is written up here as `CategoryProbeBucket`; the actual probes used a content-specific
name, which is what made them a real §2 violation, and that name is deliberately not recorded in
this repository.) See DEC-051.

**Deliberately not done here:** no `Categories()`/`AllCategories()` enumeration helper and no
human-facing display labels (the TUI's category filter is a Phase 4 task and will say what shape
it needs); no routing of numeric-looking strings into `CategoryFromTorznab` (an adapter with
numeric ids calls that helper directly, and guessing would make `"8"` mean something surprising);
no sub-category granularity below the 1000-block; and no adapter actually calling either helper
yet — Torznab is T-021, the scraper T-022.

**Acceptance**
- Small internal enum (`CategoryOther` as the zero value, plus a handful of broad buckets).
- Helpers to map from Torznab numeric IDs and from arbitrary adapter strings.
- Unknown input maps to `CategoryOther` and is never dropped or errored.
- No content-specific or scene-specific categories (AGENT.md §2).

---

### T-012 · Registry and fan-out search
```
status: done
depends: T-010, T-011
```
**Files:** `internal/indexer/registry.go`, `internal/indexer/registry_test.go`

**What landed.** `Registry` is the named set of sources plus the fan-out across them:
`NewRegistry(Config)`, `Register`, `Get`, `List`, `Enabled`, `SetEnabled`, and
`SearchAll(ctx, q, ids...) ([]Result, []SourceError, error)`. `Config` carries three durations —
per-indexer `Timeout` (default 15s), `CacheTTL` (60s) and `MinRefreshInterval` (1s) — and any
zero or negative field is replaced by its default, so the zero `Config` is the documented one.
No frozen §5 type was touched: neither `indexer.go` nor `category.go` appears in
`git diff --stat origin/main` at all, and the two files above are the whole change to the package. No
adapter exists yet (T-021, T-022) and none is imported: every source in the tests is a fake
declared in `registry_test.go`, per §4's rule that the registry is the only consumer of adapters.

**One type reports both failures and skips, because the return shape is fixed.** The acceptance
criteria fix the signature at `([]Result, []SourceError, error)`, and §6.3 requires a source that
lacks a needed capability to be *reported* as skipped without being a failure. There is nowhere
else to put that, so `SourceError` carries `IndexerID`, `Err`, and a `Skipped` bool, and unwraps
to a sentinel (`ErrUnsupportedMode`, `ErrThrottled`, `ErrUnknownIndexer`, `ErrSourcePanic`, or the
adapter's own error). `Error()` says which outcome it was and names the source, so a collected
error reads on its own (§6.9). See DEC-052.

**The fatal-error rule, stated exactly.** `SearchAll` returns a non-nil error in two cases and no
others: `ErrAllSourcesFailed` when at least one source failed and none succeeded, and
`ErrNoSources` when there was nothing to query at all. A skip counts as neither a success nor a
failure, which settles the three edge cases individually: **all skipped** is a nil error with no
results and one skip note per source; **skipped + failed** is `ErrAllSourcesFailed`, because every
source that was actually queried failed; **skipped + succeeded** is a nil error and partial
results. `ErrNoSources` is a deliberate reading of "the error is non-nil only when every source
failed", not an oversight — see DEC-053, which records the alternative and why it was rejected.

**The per-source refresh floor skips rather than waits, and is claimed before the request.**
`reserveFetch` checks the source's last fetch time and stamps the new one under the same lock, so
two concurrent fan-outs cannot both decide they are first; a cache hit never calls it, because it
makes no request. A source inside the floor is skipped with `ErrThrottled` — a skip, so it can
never turn a search fatal. The floor is 1s by default and the 60s cache sits in front of it, so
the mash-`R` case is a cache hit and the floor only bites on a *different* query reaching the same
source within a second. The cost is real and is written down rather than hidden: two concurrent
*distinct* queries against one source will skip one of them (backlog `T-920`). DEC-054 records
the rejected alternatives.

**Each `Search` runs on its own goroutine behind a buffered handoff.** An adapter that ignores its
context cannot be allowed to hold the fan-out open past its deadline, and Go cannot abandon a
blocked call in place; the buffered channel means a late answer is delivered and discarded rather
than blocking the adapter's goroutine forever. The same goroutine recovers a panicking adapter and
reports it as that source's failure — §6.9 forbids *raising* a panic outside `main`, and §6.3
forbids one bad source taking the app down. `SearchAll` itself starts no goroutine that outlives
it. See DEC-055.

**Dedup and ordering are fully determined by the answers, never by who replied first.** Sources
are queried concurrently but their results are folded in selection order, so the merge and the
sort are reproducible. Identity is the infohash (trimmed, lowercased) when there is one, otherwise
the normalised title plus the size; a result with neither an infohash nor a title containing a
letter or digit is never merged with anything, since merging on emptiness folds unrelated rows
together. Highest seeders survives, every contributing id is joined into
`Extra["tortui.sources"]` in selection order, and the sort breaks ties on the other of
seeders/published, then title, then indexer id, then result id. DEC-056.

**Verification.** `make check` green and `go test -race ./... -count=1` green (also `-count=3` on
the package), both **on macOS only** — the Linux and Windows CI legs are not reproducible on this
machine and are NOT verified locally; the PR's own CI run is the evidence for those.
`go test ./internal/indexer/ -cover` reports **100.0% of statements** (the §9 floor for this
package is 75%; `make cover`'s threshold is 0 and enforces nothing — backlog `T-916`).
`scripts/check-indexer-hostnames.sh origin/main HEAD` exits 0. 41 test functions cover the
required matrix (all succeed, one times out, one errors, all fail, duplicate infohashes across
sources, zero results) plus the concurrency cases: concurrent `Register`/`List`/`Get` against
`SearchAll`, sixteen concurrent identical searches sharing the cache and the floor, an adapter
that panics, one that returns `(nil, nil)`, one that answers after its deadline, a pre-cancelled
parent context, and a determinism check that runs the same three-source fan-out 25 times and
compares the orderings.

**The tests were checked by breaking the code, not by reading it.** Nineteen defects were injected
into `registry.go` one at a time, each run against the tests it should trip and then reverted
(`git status` clean afterwards): sequential fan-out, skips counted as failures, any failure fatal,
no panic recovery, unbuffered handoff, cache storing the adapter's own slice, merge writing into
the adapter's `Extra`, first-copy-wins dedup, no contributing-source list, cache hit consuming the
refresh floor, failures cached, one ordering for both modes, category filter not canonicalised, no
capability check, no refresh floor, no result cache, unmatchable results merged, no per-indexer
timeout, and ids not de-duplicated. **Two of them passed**, and both were real gaps in the tests
rather than in the code: the timeout test watched the *fake source's* call return, which happens
even when the registry's handoff goroutine is left blocked forever, and nothing covered an adapter
mutating the slice and maps it had already returned. Both were closed — the first by polling the
goroutine dump for a goroutine still parked in `callSearch`, the second with an adapter that
rewrites what it returned — and re-running those two mutations plus a third (`cachedResults`
handing out the cache's own objects) is now red for all three.

**Deliberately not done here:** no adapter (T-021, T-022) and no HTTP client (T-020) — this task
ships the registry and is tested against fakes only. No single-flight sharing of an in-flight
fetch (`T-920`). `SearchAll` does not report which sources answered from cache, which T-061's
status bar will want (`T-921`). `CacheTTL` and `MinRefreshInterval` are registry defaults with no
TOML keys — `search_timeout` is the only related key `internal/config` has today, and nothing
wires config into a registry yet because the composition root does not exist (`T-922`). No
`Query.Limit` cap and no `Query.MinSeeders` filter on the merged output: T-010's godoc already
puts both on the adapter (`MinSeeders` explicitly, `Categories` explicitly — 'the registry does
not filter on its behalf'), and truncating the merge would silently drop the tail of a
multi-source search. No `Resolve` fan-out — nothing calls it yet.

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
status: done
depends: T-012
```
**Files:** `internal/indexer/httpx/` — `client.go`, `retry.go`, `ratelimit.go`, `clock.go`,
`redact.go` and their tests (`client_test.go`, `retry_test.go`, `ratelimit_test.go`,
`redact_test.go`, `helper_test.go`).

**What landed.** `httpx.New(Config) *Client` plus `Do(ctx, Request) (*Response, error)` and a
`Get` convenience. Like `NewRegistry`, every `Config` field is optional and any zero field takes
its documented default, so the zero `Config` is the documented client. No adapter consumes it yet
— T-021 and T-022 are untouched — and no frozen §5 type was involved: `git diff --stat origin/main`
lists exactly the five new source files, their five test files, and this tracker.

**Criterion by criterion.**
- *Configurable user-agent* — `Config.UserAgent`, default `tortui` (deliberately not a URL:
  AGENT.md §2 keeps hostnames out of `internal/indexer` and a user-agent is not worth an
  exception). A `User-Agent` the caller sets on the `Request` wins.
  `TestUserAgentIsConfigurable` covers all three cases against `httptest.Server`.
- *Connect/read timeouts* — `Config.ConnectTimeout` reaches `net.Dialer.Timeout` and
  `Transport.TLSHandshakeTimeout`; `Config.ReadTimeout` reaches `Transport.ResponseHeaderTimeout`;
  their sum is also applied as a per-attempt context deadline, so a body that trickles is bounded
  too. The read timeout is proven behaviourally (`TestReadTimeoutEndsAStalledResponse`, against a
  handler that never answers). The **connect** timeout is asserted structurally
  (`TestTransportCarriesTheConfiguredTimeouts`) and *not* behaviourally: exercising it needs a
  host that swallows connection attempts, and §6.7 bars the network from unit tests.
- *Per-host rate limiter* — `hostLimiter`, keyed on the lowercased `host:port`, claiming each
  slot under the lock before waiting. **Seven** unit tests in `ratelimit_test.go`: spacing,
  per-host independence, case folding, disable, two separate cancellation tests
  (`TestHostLimiterReturnsContextErrorWithoutWaiting` and
  `TestHostLimiterWaitIsInterruptedByCancellation` — counted as one in round 1, which QA caught),
  and eight concurrent claimants; plus two through the client
  (`TestClientRateLimitsPerHostAcrossRequests`, `TestClientRateLimitAppliesAcrossRetries` — the
  limiter applies to retries, not only to the first attempt).
- *Retry on 429/5xx only, never 4xx* — `isRetryableStatus`. `TestRetriesOn5xxStatuses` covers
  500/502/503/504/501, `TestNeverRetriesAnyClientError` covers 400/401/403/404/**408**/410/422/418
  and asserts exactly one request each. 408 is not retried; see DEC-058.
- *Exponential backoff* — `backoffFor`, doubling from `BaseBackoff` and clamped at `MaxBackoff`.
  `TestBackoffIsExponentialAndCapped` asserts the exact schedule `[100ms 200ms 300ms 300ms]`
  through an injected clock, so the backoff path is tested without any wall-clock sleeping.
- *Honours `Retry-After`* — both RFC 9110 forms plus the awkward ones: delta-seconds, HTTP-date,
  malformed (falls back to backoff), a past date or `0` (retry now), and one longer than
  `MaxRetryAfter` (stop, do not cap — DEC-059). One test each, plus an eighteen-case table on
  `parseRetryAfter`. A delta-seconds value too large for a `time.Duration` **saturates** rather
  than wrapping negative, so an absurd wait still ends the attempt loop; the round-1 code wrapped
  and retried with no delay at all (QA D2, fixed in round 2 — see the round-2 note below and the
  DEC-059 correction).
- *Injects user-supplied `api_key` / `cookie`* — as a query parameter (name configurable,
  default `apikey`) and as the `Cookie` request header (the field holding it is named
  `Credentials.CookieHeader`, for the logging-mask reason below), only when the user configured
  one, and only from
  `Config.Credentials`. Nothing in the package discovers, harvests, guesses, or negotiates a
  credential, and there is no challenge handling of any kind (AGENT.md §2).
  `TestInjectsUserSuppliedCredentials`, `TestInjectsNothingWhenNoCredentialsAreConfigured`,
  `TestAPIKeyParameterNameIsConfigurable`.
- *Response body size cap, default 8 MB* — `Config.MaxBodyBytes`, `DefaultMaxBodyBytes = 8 << 20`.
  Enforced twice over: a declared `Content-Length` above the cap is refused before a byte is read,
  and the read itself goes through `io.LimitReader(body, cap+1)`, which is what actually catches a
  chunked response with no declared length and a server that lies about the length it declared.
  Over the cap is an **error, never a truncation** — a torznab feed cut off mid-document parses as
  a short feed, which is silent data loss rather than a failure. Four tests including the
  chunked and the lying-`Content-Length` cases.
- *`httptest.Server` tests for each behaviour* — every test above runs against `httptest.Server`
  except **five** that `httptest.Server` cannot stage, which use an injected `http.RoundTripper`
  and are still zero network: a response whose `Content-Length` lies
  (`TestBodyCapCatchesAServerLyingAboutContentLength`), a body that fails mid-read
  (`TestBodyReadFailureIsReported`), a body that fails both `Read` and `Close`
  (`TestDrainAndCloseFailuresAreLoggedNotSwallowed`), a staged `*url.Error`
  (`TestURLErrorIsStrippedFromTheCauseChain`), and a same-host https-to-http redirect
  (`TestSchemeDowngradeRedirectIsRefused` — a real server sends that `Location` readily enough,
  but two `httptest` servers can never share one `host:port` across two schemes). The first four
  were miscounted as three in round 1; QA caught it, and the fifth arrived with the round-2
  redirect fix. Package coverage is **100.0% of statements**, the level the rest of
  `internal/indexer` sits at.

**Credentials cannot reach an error string or a log line, and that is enforced structurally.**
T-003 masks by key name and value shape, and states in its own package doc that an opaque
credential under an unremarkable key name is the case it cannot close. This package is the first
one that holds such credentials, so it closes it from the other side, in four layers:
1. **Naming.** The credential fields are `Credentials.APIKey` and `Credentials.CookieHeader` —
   both contain a substring on `isSensitiveKey`'s list, so a `Credentials` walked through
   `slog.Any` is redacted field by field. Renaming either to `Value`, `Auth`, or `Credential`
   would silently reopen the hole; the godoc says so at the type.
2. **`LogValue`.** `Credentials` implements `slog.LogValuer` and renders as a fixed string, which
   `internal/logging` resolves before anything else.
3. **Structural silence.** No error this package builds contains a URL, a query string, a request
   or response header, or a body excerpt. Only the method, the `host:port`, and a status or
   cause. `StatusError` has no URL field to print.
4. **`*url.Error` is stripped from every cause chain.** `net/http` returns one from essentially
   every failed request and its `Error()` prints the whole URL, api_key included — so
   `unwrapURLError` removes that layer before the cause is stored. `errors.Is` still reaches
   `context.DeadlineExceeded`, `net.Error`, and the rest; `errors.As(err, &urlErr)` deliberately
   finds nothing. A final `scrub` pass replaces the configured values in the remaining text as a
   backstop. See DEC-061.

`TestNoErrorEverCarriesACredential` runs eight failure modes (4xx, exhausted 5xx retries,
over-long `Retry-After`, over-cap body, refused redirect, dial failure, unparseable URL,
cancellation) and asserts, for each — and for every error in its unwrap chain, and for `%v` and
`%+v` — that neither credential, no `apikey=` parameter and **no absolute URL at all** appears.
`TestURLErrorIsStrippedFromTheCauseChain` stages a `*url.Error` carrying the key and proves both
that it is gone and that its cause is still reachable. `TestNoCredentialReachesTheLogFile` runs
the whole thing through the real `internal/logging` file sink and greps the resulting file.

**Waits are injectable, so nothing here sleeps on the wall clock.** `Clock` (`Now`, and a
`Sleep` that takes a context) is behind both the rate limiter and the backoff. Tests drive a fake
clock that records each duration and jumps forward, which is what makes the exact backoff
schedule assertable rather than approximated. Two tests deliberately use the real clock, because
they assert the opposite property — that a cancelled context abandons a pending wait instead of
sleeping it out — and both allow a 2s margin against a 10s wait rather than racing a tight
timing window.

**Deadlines.** `Do` applies `RequestTimeout` (60s) on top of the caller's context, so a call
always has a deadline even if the caller forgot one (§6.2), and a caller's shorter deadline still
wins. Before each wait the client checks whether the delay would outlast the deadline and stops
with the status error rather than sleeping into a cancellation. Each attempt additionally gets
`ConnectTimeout + ReadTimeout`.

**Beyond the criteria, deliberately.** A redirect that leaves the original host is refused rather
than followed (`ErrCrossHostRedirect`), because the api_key travels in the query string and
following one would hand the user's credential to a server they never configured. A same-host
redirect that drops from https to http is refused for the same reason (`ErrInsecureRedirect`): a
`Location` that preserves the query would put the api_key on the wire in cleartext. The reverse,
http to https, is followed — the destination is the host the user configured and the hop only
adds TLS. A change of port is a change of host and is refused, since the comparison is on the
host as written. Same-host, same-scheme redirects still work, and the chain is bounded at five.
See DEC-062 for the refusal and DEC-064 for why the two directions are treated differently.

**Deliberately not done here:** no adapter, no config plumbing (nothing reads `config.Indexer`'s
`api_key` yet — the composition root that would wire it does not exist), no `config.example.toml`
or `README.md` change, since no user-visible key or default changed. No new dependency: the rate
limiter is stdlib (DEC-060).

**Round 2 — QA remediation (PR #11).** QA passed the credential work and both blocking defects
were elsewhere. Both were reproduced against the round-1 code before anything was changed, and
each fix has a test proven red against the unfixed code and green against the fixed one.

1. *Scheme-blind redirect check (QA D1).* `checkRedirect` compared only `req.URL.Host`, so
   `https://feed.example.org/api?apikey=…` → `http://feed.example.org/api?apikey=…` returned
   `nil` and was followed — **both** credentials in cleartext, the api_key in the query and the
   `Cookie` header (`net/http` strips neither on a same-host scheme change; DEC-062 records the
   stdlib reading), the very harm the cross-host check exists to prevent, and disclosed nowhere. It now also refuses an https-to-http hop
   (`ErrInsecureRedirect`), follows http-to-https, and is unchanged on same-scheme hops.
   `TestSchemeDowngradeRedirectIsRefused` drives it through the real `*http.Client` redirect
   machinery; `TestCheckRedirectSchemeAndHostRules` is an eight-case table over downgrade,
   upgrade, same-scheme, scheme case, cross-host, port change, and host-check precedence.
   DEC-062 corrected in place; DEC-064 records the upgrade/downgrade asymmetry and the port
   literal.
2. *`Retry-After` integer overflow (QA D2).* `parseRetryAfter("31536000000")` returned
   `(-1488191h9m7s, true)`; `retryDelay` read that negative as *shorter* than `MaxRetryAfter`,
   accepted it, and `Sleep` on a negative returns immediately — `MaxAttempts` requests back to
   back with no delay at all, which is both the opposite of what DEC-059 claimed and the
   hammering §6.13 forbids. An out-of-range delta now saturates at the longest representable
   `time.Duration` (a `strconv.ErrRange` parse is treated as an enormous value rather than as
   malformed, so digits past int64 land there too), and `retryDelay` clamps any negative
   `RetryAfter` to zero as a second line of defence. Six table cases now sit at and past the
   boundary, plus `TestAbsurdRetryAfterStopsInsteadOfRetryingWithNoDelay` end to end. DEC-059
   corrected in place.
3. *Tracker.* The `**Acceptance**` block below was deleted in round 1 — the only `done` task
   missing one — and one criterion was silently reworded (`api_key` / `cookie` → "session
   header"). Restored byte for byte from `main`, with these notes above it, matching T-012.
4. *Counts.* Three injected-`RoundTripper` cases were really four, and "six" limiter tests were
   really seven; both corrected above, in the PR body, and in `helper_test.go`'s own comment. The
   round-2 redirect test makes the injected-transport count five.

Not fixed here, deliberately, on QA's ruling: `T-927` (`internal/logging`'s free-text pattern
misses `CookieHeader:` in a `%+v` dump — a defect in that package, not this one), `T-928`
(limiter and redirect host keys treat `example.org` and `example.org:80` as different hosts;
fails closed in the redirect case), and `T-929` (`slog.Any` on a whole `Config` yields an error
string because `Jitter` is a func field; fails safe).

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
status: done
depends: T-020
```
**Files:** `internal/indexer/torznab/` — `torznab.go`, `caps.go`, `feed.go`, `decode.go`,
`errors.go` and their tests (`torznab_test.go`, `caps_test.go`, `feed_test.go`,
`credentials_test.go`, `helper_test.go`), plus fourteen XML fixtures in `testdata/torznab/`.

**What landed.** `torznab.New(Options) (*Adapter, error)` builds an adapter without touching the
network; `torznab.Discover(ctx, Options) (*Adapter, error)` builds one and probes the source for
its real capabilities. `*Adapter` satisfies the frozen §5 `indexer.Indexer`, asserted at compile
time in `torznab.go`. No §5 type was touched — `git diff --stat origin/main` lists five new
source files, five new test files, fourteen fixtures, and this tracker. Nothing outside
`internal/indexer/torznab/` imports the package (§4). Every request goes through T-020's `httpx`,
so this package never touches `net/http`, never sees a credential, and writes no log lines at
all. Package coverage is **100.0% of statements**, the level the rest of `internal/indexer` sits
at; `make check` and `go test -race ./... -count=1` are both green.

**Criterion by criterion.**
- *Implements `Indexer` against the Torznab/Newznab XML API* — `ID`, `Name`, `Caps`, `Search`,
  `Resolve`. `Search` sends `t=search` with `extended=1` (an optional newznab search parameter,
  §2 of `docs/newznab_api_specification.txt` in the nZEDb/nZEDb repository, branch dev), plus
  `q`, `limit`, `offset` and `cat` as the query and the source's caps allow.
  `TestSearchSendsTheRequestTheQueryDescribes` asserts the parameter set for seven queries.
  `MinSeeders` is applied locally — Torznab has no parameter for it, which `indexer.Query`
  explicitly allows.
- *Parses `caps` into `Caps`; handles servers that omit it* — `caps.go` reads `searching`,
  `limits` and `categories` into Search, Pagination and Categories.
  `TestDiscoverHandlesAServerThatOmitsCaps` covers seven ways a server can fail to publish one
  (404, 500, an empty body, an HTML page, truncated XML, a feed in place of caps, an `error`
  document): each returns the **fail-closed baseline caps and a usable adapter** alongside an
  error wrapping `ErrCapsUnavailable`, and each case then runs a successful search through that
  adapter to prove it. See DEC-069 for why the adapter comes back with the error.
- *`ModeLatest` probed, not assumed* — see "The caps probe" below and DEC-067.
- *Maps `seeders`, `peers`, `size`, `pubDate`, `category`, `magneturl`, `infohash`* — plus
  `leechers`, `uploader`/`poster`, the trust attributes, and the plain `size`, `files`,
  `grabs` elements Jackett writes outside the torznab namespace. `search-full.xml` asserts
  every field of a fully populated item; `search-messy.xml` asserts seven items' worth of
  missing, duplicated, non-numeric, negative and contradictory attributes. Every numeric field is
  parsed by hand rather than by `encoding/xml`, because a struct field typed `int` makes one
  `seeders="n/a"` fail the **whole document** and lose every other result in it. Categories go
  through `indexer.CategoryFromTorznab` and `CategoryFromString` — the mapping is not duplicated
  here (§13) — and an unplaceable one is `CategoryOther`, never a dropped result.
- *`Trust` from any uploader/verified attribute, `TrustUnknown` when absent* — see DEC-068. No
  Torznab implementation publishes one, so `TrustUnknown` is the honest answer for every server
  that exists today; the mapping is there for one that invents an attribute. An attribute that is
  present and false yields `TrustNone`, which `indexer.Trust` documents as different information
  from absent.
- *`Resolve` is a no-op when a magnet is present* — checked first, before anything else on the
  result is looked at, so it holds regardless. `TestResolveIsANoOpWhenAMagnetIsPresent` passes a
  result whose `InfoHash` deliberately contradicts its magnet and asserts nothing changed and
  that no request was made. `Resolve` makes **no network call at all** — Torznab has no per-item
  endpoint — and derives a magnet from an infohash when there is no magnet yet.
- *Malformed XML, HTTP errors and empty result sets without panicking* —
  `TestDecodeRejectsEveryUnusableDocument` covers fourteen documents: empty, whitespace,
  declaration-only, comment-only, plain text, truncated, mismatched tags, an entity bomb, an HTML
  page, a caps document, an unknown root, a nested `rss` element, and 60,000 levels of nesting counted
  twice — once through the mapped path and once through the skipped one. `TestSearchOutcomes`
  covers thirteen responses end to end: twelve failure modes and one success. (Both counts were
  off by one in the version of this row PR #12 first pushed — thirteen and twelve — and were
  recounted against `go test -v` output on 2026-09-15 while fixing the fixture count below.)
  Go's `encoding/xml` refuses an entity the document declared itself (a DOCTYPE internal subset
  is a `Directive` token, its entity definitions are never applied), so the billion-laughs
  fixture fails with a syntax error rather than expanding — asserted, not assumed.
- *Fixture-driven tests from `testdata/torznab/*.xml`, no network* — fourteen fixtures at the
  repository root, per §4's layout and this criterion's own path. Every test replays them through
  `httptest.Server`; the package makes no real network call anywhere.

**The caps probe.** Torznab has no way to ask whether a recent-additions feed exists: there is no
`latest` function and the caps document has no field for it. What it has is a documented
behaviour — the newznab specification says "if the input string for search is empty all items
(within the server/query limits) are returned", Jackett names the case `IsRssSearch`
(`src/Jackett.Common/Models/TorznabQuery.cs`, branch master), and neither Jackett's nor
Prowlarr's controller rejects a request with no `q`. So the probe **makes that exact request**
(`t=search`, no `q`, `limit=1`, `extended=1`) and reads the answer: `Caps.Latest` is true only
for a feed that parsed and had at least one item in it. An empty feed, a parse failure, an HTTP
failure and an error document all leave it false, because a server that ignores an empty keyword
and a server whose index is empty are indistinguishable and a "latest" key that silently returns
nothing is worse than a source the registry skips and reports (§6.3). `Caps.ProvidesMagnet` is
set by the same request and needs **every** returned item to carry a magnet. The probe is skipped
entirely when the caps document says search is unavailable, so such a source costs one request,
not two (`TestDiscoverDoesNotProbeLatestWhenTheSourceCannotSearch`). See DEC-067.

**`peers` is the total, not the leecher count.** Verified in three primary sources rather than
recalled; the derivation and the ambiguous case are DEC-065.

**Credentials cannot reach an error string. On the `Result` side the answer is per field, and
this row now writes every field out rather than counting them.** A Torznab request carries the
api_key in its *query string*, so every URL this adapter handles is credential-bearing, and
`internal/logging` masks by key name — which means an opaque key under an unremarkable name is
written out in plaintext. The rule for errors is therefore structural: **no text the source sent
appears in any error**. That costs the server's own error description, the `encoding/xml` message
(the line number is kept), and the name of an unexpected root element (classified, not quoted);
it buys an adapter with nothing to redact.

The `Result` side is field by field, and the package doc carries the same list (this is written
out because two summaries of it shipped and QA found both wrong by at least one field — round 1:
"no field that is not named for a URL"; round 2: "two fields are not derived". Round 3 found the
same defect a third time, no longer as a count but as a parenthetical *inside* the enumeration:
the `Magnet` entry called `Resolve`'s derived magnet clean, and it is not. The entries below
state what a field carries and call nothing safe that the code does not make safe):

| Field | Treatment |
|---|---|
| `IndexerID` | Derived — the id the user configured. |
| `ID` | Derived only when it is an infohash (40 hex / 32 base32, validated). Otherwise a guid, comments or link goes through `withoutQuery`, which reduces less than "scheme+host+path" suggests: it drops the query string and fragment from a candidate `url.Parse` gives a scheme to and returns every other candidate as it stands, so an opaque `scheme:token` value is reduced by nothing and userinfo and path survive in a full URL. **Passed through** — whatever `withoutQuery` leaves, and the last-resort title fallback. |
| `Title` | **Passed through**, whitespace-trimmed and otherwise verbatim. |
| `InfoHash` | Derived — only validated hex/base32 survives. |
| `Magnet` | **Passed through** — the `magneturl` attribute (or a magnet `link` element) exactly as published, `dn=` included. The magnet `Resolve` derives for an item that published none is **not clean either**: `magnetFor` writes `Result.Title` into its `dn=`, `url.QueryEscape` encodes that text rather than removing it, and an opaque token survives the encoding unchanged — so a derived magnet carries whatever the title carries. Reproduced by QA on round 3 and pinned by `TestResolveDerivesAMagnetThatInheritsTheTitlesGap`. |
| `TorrentURL` | The source's download URL verbatim — safe, `internal/logging` masks on the name. |
| `SizeBytes`, `Seeders`, `Leechers` | Derived — parsed integers (`Leechers` also `peers - seeders`, DEC-065). |
| `Category`, `Trust` | Derived — enum values. |
| `Published` | Derived — a parsed `time.Time`. |
| `Uploader` | **Passed through**, except that a value containing `://` is dropped. The refusal keeps a link out; it cannot keep an opaque token out. |
| `SourceURL` | The source's page URL verbatim — safe, masked on the name. |
| `Extra` | Derived — fixed `torznab.`-prefixed keys, values that parse as a number only. |

So the fields that carry source text under a name `internal/logging` does **not** mask are,
exhaustively: `Title`, `Magnet`, `Uploader`, and `ID` on every branch but the infohash. A source
that echoes the user's key into a `title` element, a magnet's `dn=`, an `attr name="uploader"` element or a
`guid isPermaLink="false"` element puts it in that field verbatim — and into `Magnet` a second time
when the item published no magnet and `Resolve` derives one, since the derived magnet's `dn=` is
the title. Logging the field — or the whole `Result` — writes it to the log file in plaintext.
The one part of that surface something still catches is a value that is itself a full http(s)
address: `internal/logging`'s URL pattern matches on the value, whatever its key is called.
Nothing catches a bare token. QA reproduced `Title`/`Magnet` on PR #12 round 1, `Uploader`/`ID`
on round 2 and the derived magnet on round 3, each against the real `internal/logging` sink, and
each was reproduced again on this branch before this row was rewritten. None of it is fixable
from inside the adapter: the title is the display value, the magnet must reach the engine as
published, a `dn=` is a legitimate part of a derived magnet and the name a client shows before
metadata arrives, an uploader's name is legitimately an opaque token, a guid is the source's own
stable identity, and this package never sees the credential it would have to scrub with
(DEC-061). The gap belongs to the frozen §5 `Result` type; it is recorded as backlog `T-934` and
reasoned out in DEC-071.

See DEC-066. `TestNoErrorFromThisAdapterCarriesTheCredential` runs fifteen failure modes —
including an error document that echoes the api_key back, a root element *named* after the key,
and an entity named after it — and asserts for each error, each `%v`/`%+v` rendering and each
unwrapped cause that neither the key, nor an `apikey=` parameter, nor **any absolute URL** is
present. `TestWhichResultFieldsCanCarryTheCredential` asserts the boundary in both directions
across three feeds: one echoing the key into the title, a magnet's `dn=`, the guid, the comments,
the link, the enclosure, a link-shaped uploader and the grabs; one whose only identity is a
credential-bearing guid, which keeps the URL-stripping path exercised; and
`bareTokenEchoingFeed`, which echoes the key back as a **bare token** in an uploader attribute
and a non-permalink guid. `TorrentURL`, `SourceURL`, `Title`, `Magnet`, the bare-token `Uploader`
and the bare-token `ID` are asserted to carry it; `InfoHash`, `IndexerID`, `Extra` and each
derived `ID` to be free of it; and `assertLinkShapedUploaderIsRefused` pins the `://` refusal on
the two feeds that write a link there.
`TestResultIDFallsBackToTheTitleAndInheritsItsGap` pins the last-resort ID branch, and
`TestResolveDerivesAMagnetThatInheritsTheTitlesGap` pins the derived magnet: it resolves a result
whose title carries the key and requires both the escaped title and the key itself in the
`dn=` `Resolve` produced.
`TestNoCredentialReachesTheLogFile` puts every error plus a parsed `Result` through the real
`internal/logging` sink and greps the file — scoped, as its own comment says, to the surface this
adapter controls, since a `Title`, `Magnet` or `Uploader` carrying a key does reach that file and
only `T-934` can change it. This sweep **caught a live leak while it was
being written**: `Resolve` was naming the failing result with `%q` on `Result.ID`, and a caller
can hand it a result whose ID is a raw download URL. Fixed by not naming the result at all.

**Proved by breaking it.** Twelve deliberate mutations were applied one at a time, the relevant
test run, and the mutation reverted: naming the result in `Resolve`'s error, keeping the `error`
description, reading `peers` below `seeders` as a leecher count, assuming `Caps.Latest` in the
baseline, setting `Latest` on an empty probe feed, printing `xml.SyntaxError.Msg`, quoting an
unrecognised root element, keeping a guid's query string, dropping the numeric restriction on
`Extra`, accepting a link as `Uploader`, rebuilding a magnet that was already present, and
falling back to the enclosure for `SourceURL`. Every one turned a test red, and the tree is green
again after the reverts.

**Round-2 remediation (2026-09-15) — three more mutations, on the uploader and ID assertions.**
QA found `assertDerivedFieldsAreClean`'s `Uploader` entry vacuous in exactly the round-1 pattern:
both shipped feeds write the uploader as a URL, so the `://` refusal always fired and the entry
could not fail. `bareTokenEchoingFeed` was added, `Uploader` moved out of the derived map and the
refusal pinned separately, and the result proved by mutation with a verbatim copy of the round-1
test running beside the new one: scrubbing `Uploader` to a constant — old **PASS**, new **FAIL**;
making `withoutQuery` refuse a non-URL candidate — old **PASS**, new **FAIL**; removing the `://`
refusal — **both FAIL** (so no coverage was lost by moving the field); dropping the query-strip —
**both FAIL**. Every mutation reverted, the copy deleted, `git status` clean.

**Round-3 remediation (2026-09-15) — one false clause, one imprecise branch, one more mutation.**
QA found the `Magnet` entry's parenthetical false: `Resolve` builds its magnet with
`magnetFor(hash, r.Title)`, which appends `&dn=` + `url.QueryEscape(Title)`, so the derived
magnet embeds `Title` — itself a disclosed pass-through — verbatim. Reproduced on `98a8388`
before any edit, through the real `internal/logging` sink: `derived Magnet =
"magnet:?xt=urn:btih:0123…&dn=Invented+Release+opaque-value-from-the-users-own-account-0192837465"`,
written to the log file unredacted beside a `[REDACTED]` `SourceURL` and `TorrentURL`. Not a new
exposure — the text is `Title`'s, and `Title` is already on the disclosed side — but a
clean-guarantee inside the enumeration that exists to be exhaustive, so the clause is replaced by
what the code does in all three copies of the list (package doc, this row, the PR body), and
`magnetFor`'s own comment now says it too. The `ID` branch wording is tightened in the same edit:
`withoutQuery` drops the query string and fragment from any candidate `url.Parse` gives a scheme
to and returns everything else as it stands, so "reduced to scheme+host+path" overstated it —
an opaque `scheme:token` value is reduced by nothing, and userinfo and path survive in a full
URL. Proved by mutation: dropping the `&dn=` from `magnetFor` turns
`TestResolveDerivesAMagnetThatInheritsTheTitlesGap` **FAIL** on both of its assertions
(`Magnet = "magnet:?xt=urn:btih:0123…"` with no `dn=`); reverted, green, `git status` clean.
Product code is untouched — `dn=` is a legitimate and useful part of a magnet, and the fix is
the disclosure, not the behaviour. DEC-066 and DEC-071 were both checked for the same clause and
neither repeats it — both describe `Magnet` as the published `magneturl` attribute and say
nothing about what `Resolve` derives — so neither row was corrected; their existing correction
history is left exactly as round 2 wrote it. DEC-066 does still carry the round-2 "reduced to
scheme, host and path" phrasing that finding D2 tightens here, which is the same imprecision and
not the false clause; it is flagged rather than rewritten, because a DEC correction is a
deliberate act and QA ruled D2 non-blocking and scoped to the field list.

**Deliberately not done.** No `tv-search`/`movie-search` modes (subject-matter searches tortui has
no concept of, §2). No category UI (backlog `T-905`). No local re-filtering of results by
category, and no result caching — the registry owns both. `Discover` is not wired into any
composition root, because there is no config or app layer to wire it into yet.

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

### T-941 · Raise the minimum Go version to 1.25

```
status: done
depends: T-020
```
**Files:** `AGENT.md`, `go.mod`, `go.sum`, `.github/workflows/ci.yml`, `README.md`, `NOTICE`

**Why this exists.** T-022 is `blocked` on it — see the Blocked section for the full finding.
`govulncheck` resolves seven `golang.org/x/net` advisories into the scraper's own `html.Parse`
call, including `GO-2026-4441`, an infinite parsing loop on a code path whose entire job is
parsing a page an arbitrary source served. The first release fixing all seven, `x/net v0.55.0`,
declares `go 1.25.0`, which the repository's pinned `go 1.23` cannot take. This is a change to a
locked AGENT.md §3 stack row and was **explicitly authorised by the project owner** on
2026-09-15 after the block was raised and independently verified; §3's "do not re-litigate" does
not apply to a change the owner directed.

**What landed.** Four one-line edits and nothing else: AGENT.md §3's Language row (`Go 1.23+` →
`Go 1.25+`, the only row touched), `go.mod`'s directive (`go 1.23` → `go 1.25.0`),
`.github/workflows/ci.yml`'s `GO_VERSION` (`"1.23"` → `"1.25"`, still quoted), and `README.md`'s
"Requires Go 1.23 or newer." `go mod tidy` under go1.27.1 produced **no further change** and
`go.sum` is untouched — raising the language version alone neither adds nor upgrades a module,
and no `toolchain` line was introduced. `NOTICE` is byte-identical for the same reason, and
`make licenses` was re-run to prove it rather than assumed: the allowed-license check passed for
`GOOS=darwin`, `linux` and `windows`, and the regenerated file matched. No source file, no §5
type, no other §3 row and no other task's `**Acceptance**` block changed;
`scripts/check-indexer-hostnames.sh` is byte-identical (md5 `3e274870651abefe8a860690677b05a6`).

**`golang.org/x/net` is not on `main`, so that criterion is handed to T-022.** `main`'s `go.mod`
requires only `BurntSushi/toml`, `adrg/xdg` and an indirect `x/sys` — `x/net` arrives with
goquery on `task/T-022-scraper-framework`. Adding it here would be an unused requirement
`go mod tidy` strips on sight, and building ahead into T-022. What this task does instead is
remove the blocker and verify the target, in a throwaway module whose only content is the
scraper's own `html.Parse` call:

- `x/net v0.39.0` reproduces **all seven** advisories the Blocked section lists, with the same
  `Fixed in` versions: `GO-2026-4440` and `GO-2026-4441` at `v0.45.0`; `GO-2026-5025`, `-5027`,
  `-5028`, `-5029` and `-5030` at `v0.55.0`. Independently re-run here, not taken on trust.
- Each release's own `go.mod`, read from `proxy.golang.org`: `v0.39.0` declares `go 1.23.0`,
  `v0.45.0` declares `go 1.24.0`, `v0.55.0`/`v0.56.0`/`v0.57.0`/`v0.58.0` declare `go 1.25.0`,
  and **`v0.59.0` declares `go 1.26.0`**.
- **T-022 should pin `x/net v0.58.0` — not `v0.55.0`, and not `@latest`.** `v0.55.0` itself
  still carries `GO-2026-5942` (a panic parsing an invalid SVCB or HTTPS RR in
  `golang.org/x/net/dns/dnsmessage`, fixed in `v0.56.0`). An HTML-only consumer never calls it,
  so govulncheck's headline stays `No vulnerabilities found`, but the same run still reports
  "1 vulnerability in modules you require" — which reads badly against a criterion written as
  "0 vulnerabilities". `v0.58.0` is the newest release that is clean on both counts **and**
  still declares `go 1.25.0`; `v0.59.0` would raise the floor to Go 1.26, past what the owner
  authorised. Verified: a `go 1.25.0` module requiring `x/net v0.58.0` keeps its directive at
  `1.25.0`, builds, and reports `No vulnerabilities found` with no trailing module-level count.
- `go get golang.org/x/net@v0.55.0` does not *fail* under a `go 1.23` directive — it silently
  rewrites it (`go: upgraded go 1.23 => 1.25.0`). The block's substance holds unchanged; the
  mechanism is an automatic bump rather than a refusal, which is precisely the "raise the
  minimum Go version by side effect" the T-022 branch avoided by staying pinned at `v0.39.0`.

**§14 support matrix: reviewed, no change needed, none made.** Checked against Go's own release
notes rather than memory. Go 1.25's Ports section reads "Go 1.25 requires macOS 12 Monterey or
later. Support for previous versions has been discontinued" — §14 already floors macOS at **13**,
which is stricter, so nothing in the matrix is dropped. Go 1.24's Ports section (crossed on the
way from 1.23) adds "Go 1.24 requires Linux kernel version 3.2 or later" and marks the 32-bit
`windows/arm` port broken; §14 states no Linux kernel floor and lists Windows on **amd64 and
arm64** only, never 32-bit `arm`, so neither touches it. Go 1.25's own Windows note is about
that same broken 32-bit port ("the last release that contains" it). `go.dev/wiki/MinimumRequirements`
gives Windows as "Windows 10 and higher or Windows Server 2016 and higher" for **Go 1.21 and
later** — unchanged across this bump, and equal to §14's `Windows 10+`. All six tier-1
GOOS/GOARCH pairs still cross-compile: `make build-all` green.

**CI is the real risk here, and two of its three legs are NOT VERIFIED.** `make check`,
`go test -race ./... -count=1`, `make build-all`, `make licenses` and `govulncheck ./...` are all
green on **darwin/arm64 with go1.27.1 only**. Nothing in this task was run on a Linux or Windows
runner and nothing here can be. What *was* verified about the workflow itself: `1.25` is a
documented `go-version` value for `actions/setup-go` — its README lists "Specific versions:
`1.25`, `1.24.11`, …" — and the value stays quoted, which that same README specifically warns
about ("the YAML parser's behavior interprets non-wrapped values as numbers and, in the case of
version `1.20`, trims it down to `1.2`"). Go 1.25 is long released: `proxy.golang.org`'s
`golang.org/toolchain` list carries `go1.25.0` onward for `darwin-arm64`, `linux-amd64` and
`windows-amd64`, so setup-go has a real version to resolve on every leg. The residual unknown is
`GOTOOLCHAIN`: if a runner ever provisions a Go older than the `go 1.25.0` directive, the
default `GOTOOLCHAIN=auto` must download 1.25 mid-build instead of failing, and a runner without
module-proxy access would fail there rather than at the setup step. Pinning `GO_VERSION`
explicitly is what keeps that path cold, and it is why step 2 of the Blocked section's remedy
exists.

**DEC-072 number collision at merge time.** `main`'s highest decision row is `DEC-071`, so
`DEC-072` is the next free number and is what this task used. The unmerged
`task/T-022-scraper-framework` branch already adds `DEC-072` through `DEC-076`, so whichever of
the two merges second needs its rows renumbered — flagged rather than pre-empted, because
renumbering T-022's rows from here would mean editing a branch this task must not touch.
**Resolved on the T-022 branch (2026-09-16):** T-941 merged first, so T-022's five rows moved to
`DEC-073`–`DEC-077` when `main` was merged into `task/T-022-scraper-framework`. This row's own
`DEC-072` is unchanged.

**Acceptance**
- AGENT.md §3 Language row reads `Go 1.25+`; no other §3 row changes.
- `go.mod` declares `go 1.25.0` and `golang.org/x/net` is at `v0.55.0` or later.
- `.github/workflows/ci.yml` sets `GO_VERSION: "1.25"`.
- `README.md`'s stated requirement matches; every other `1.23` reference in the repo is found by
  a sweep and updated or justified.
- `make licenses` re-run and `NOTICE` regenerated; the licenses gate passes.
- `govulncheck ./...` reports **0 vulnerabilities** on the whole module.
- `make check` green and `go test -race ./... -count=1` green; the §14 support matrix is
  reviewed against Go 1.25's own platform requirements and any change to it is stated.
- A `DEC-` row records the authorisation, the advisories, and the 1.24-versus-1.25 choice.

---

### T-022 · Scraper adapter framework
```
status: done
depends: T-020, T-941
```
**Files:** `internal/indexer/scraper/` — `scraper.go`, `definition.go`, `plan.go`, `template.go`,
`html.go`, `jsonmode.go`, `result.go`, `errors.go` and their tests (`scraper_test.go`,
`definition_test.go`, `unit_test.go`, `hostile_test.go`, `credentials_test.go`,
`helper_test.go`), `docs/indexer-definitions.md`, and six fixtures in `testdata/scraper/`
(`search.html`, `latest.html`, `api.json`, `fixture-archive.yml`,
`fixture-archive-search-only.yml`, `fixture-api.yml`).

**Was blocked; unblocked on 2026-09-16.** This task stopped on AGENT.md §12 ("a required
dependency … has an open CVE"): `PuerkitoBio/goquery` (the §3 stack's HTML parser, and the only
way to satisfy the "Supports HTML (goquery)" criterion) takes `golang.org/x/net/html` with it,
and every `golang.org/x/net` release the module could compile under the then-pinned Go 1.23
carried seven advisories `govulncheck` resolved into this package's own `html.Parse` call. The
project owner authorised raising the Go floor; `T-941` landed it (PR #14, `69d5d07`) and `main`
now declares `go 1.25.0`. See the **Blocked → Resolved** section at the foot of this file.

**What the unblock changed on this branch, and nothing else.** `main` was merged in, and three
dependency lines moved: `golang.org/x/net v0.39.0` → **`v0.58.0`**,
`github.com/PuerkitoBio/goquery v1.10.3` → **`v1.13.0`**, and its selector library
`github.com/andybalholm/cascadia v1.3.3` → **`v1.3.4`** — the three that goquery `v1.13.0`'s own
`go.mod` pairs together, all three declaring `go 1.25.0` so the floor is matched, not raised
(`v0.59.0` of `x/net` declares `go 1.26.0` and is deliberately not taken). `go.mod`'s own
directive moved `go 1.23.0` → `go 1.25.0` to match `main`. **No source file changed for the
upgrade**; the only Go edit is the `maxHTMLDepth` doc comment in `html.go`, which described the
old pin. `NOTICE` was regenerated by `make licenses` and the allowed-license check passed for
`GOOS=darwin`, `linux` and `windows`; goquery `v1.13.0` is BSD-3-Clause and cascadia `v1.3.4` is
BSD-2-Clause, both read from the modules' own `LICENSE` files rather than inferred.
`govulncheck ./...` (v1.8.0, DB 2026-09-10) now reports `No vulnerabilities found.` with no
trailing module-level count, on darwin/arm64 under go1.27.1. The branch's five decision rows were
renumbered `DEC-072`–`DEC-076` → **`DEC-073`–`DEC-077`**, because `T-941` merged first and took
`DEC-072`. `DEC-078` records the three pins and why `@latest` is refused.

**What landed.** `scraper.Parse([]byte) (*Definition, error)` decodes and validates a
user-supplied YAML definition; `scraper.New(Options) (*Adapter, error)` compiles one into an
adapter. `*Adapter` satisfies the frozen §5 `indexer.Indexer`, asserted at compile time in
`scraper.go`. No §5 type was touched — `git diff --stat origin/main` lists eight new source
files, six new test files, six fixtures, one new document, `go.mod`/`go.sum`/`NOTICE`, and this
tracker. Nothing outside `internal/indexer/scraper/` imports the package (§4). Every request
goes through T-020's `httpx`, so this package never touches `net/http`, never sees a credential,
and writes no log lines at all *(superseded by T-023: the definition loader added in that task is
the one thing in the package that logs — see DEC-081)*. **There is not one selector anywhere in the Go code** — every
selector in the package's non-test source is a field of a struct read out of YAML. Package
coverage is **99.9% of statements** (the one uncovered statement is the `html.Parse` error
branch, which `golang.org/x/net/html` cannot reach from a byte slice); `make check` and
`go test -race ./... -count=1` are both green, and
`scripts/check-indexer-hostnames.sh origin/main HEAD` exits 0 with the script byte-identical to
`main` (md5 `3e274870651abefe8a860690677b05a6`).

**Criterion by criterion.**
- *A source is defined entirely by a user-supplied YAML file — no selectors in Go code* — the
  whole schema is `Definition`/`Block`/`Field`/`Trust` in `definition.go`, decoded strictly with
  `goccy/go-yaml`. `grep` the package's non-test Go for a selector and the only strings that
  come back are the YAML key names. The tests drive two checked-in definitions against two
  checked-in pages; changing a selector is a fixture edit with no Go change.
- *Schema covers `id`, `name`, `base_url`, `search.path`, `search.params`, `rows`, per-field
  selectors with `attr`/`text`/`regex`/`transform`, plus a `trust` mapping block* — all present
  and all documented in `docs/indexer-definitions.md`. Field selectors map onto twelve
  `Result` fields (`id`, `title`, `infohash`, `magnet`, `torrent_url`, `size`, `seeders`,
  `leechers`, `category`, `published`, `uploader`, `source_url`); `trust.values` maps a read
  badge value onto the `indexer.Trust` tokens.
- *An optional `latest` block mirrors `search` with its own path and params; field selectors
  shared by default and overridable per block; a definition omitting `latest` reports
  `Caps.Latest = false`* — `effectiveRows`/`effectiveFields`/`effectiveTrust` resolve the
  inheritance; `fields` merge key by key, `rows` and `trust` override wholesale (merging half of
  one trust map into another's selector would produce a mapping nobody wrote).
  `TestLatestUsesItsOwnBlockAndInheritsSharedFields` drives both directions through real
  markup — the feed page overrides three fields and inherits nine, and the inherited ones match
  because the fixture's feed reuses the table's cell classes.
  `TestFieldsAreSharedByDefaultAndOverriddenPerBlock` asserts the same at the unit level, plus
  that the merge does not write back into the definition's own map.
  `TestCapsComeFromTheDefinition` asserts `Caps.Latest = false` for
  `fixture-archive-search-only.yml` and `true` for `fixture-archive.yml`, and
  `TestALatestQueryIsRefusedWithoutALatestBlock` shows the adapter's own backstop refuses such a
  query without making a request.
- *Supports HTML (goquery) and JSON (gjson-style path) response modes* — `mode: html` (default)
  compiles selectors with `cascadia` and reads them with `goquery`; `mode: json` compiles a
  gjson-style path subset (dotted keys, numeric array indices, `\.` escape) and reads it with
  `encoding/json`. `TestJSONModeMapsEveryResultField` runs the json mode end to end against
  `testdata/scraper/api.json`. The path expression is hand-written rather than a dependency;
  DEC-076 says why.
- *Definition validation produces actionable errors naming the failing field and selector* —
  `*ValidationError` carries the dotted location of the failing key and the selector.
  `TestValidationNamesTheFailingFieldAndSelector` runs twenty-four bad definitions and asserts,
  for each, the sentinel, the exact `Location`, the exact `Selector`, and that both appear in
  the rendered message — not merely that an error happened. Proved non-vacuous by mutation:
  inverting the "name the selector" branch in `ValidationError.Error` turns it **red**.
- *A missing optional selector yields a zero value, never an error* —
  `TestAMissingOptionalSelectorYieldsAZeroValue` uses the fixture row that publishes a name and
  a magnet and nothing else, and asserts seven fields are at their zero value while the row
  itself survives. The rule that cannot collide with it is the *required* one: `title` and a
  link field are checked **at validation**, once, against the definition — never per row. A row
  with no title is skipped rather than errored, which is what a header row, a spacer row and an
  advertisement between results all are (`TestRowsWithNoTitleAreSkipped`).
- *`docs/indexer-definitions.md` documents the schema with a complete worked example* — the
  worked example is `testdata/scraper/fixture-archive.yml` reproduced in full, against markup
  excerpted from `testdata/scraper/search.html` and `latest.html`, with the resulting `Result`
  tabulated field by field, plus the json variant. Every address in it is on an RFC 2606
  reserved domain and the site is invented (§2, §16).
- *Tests run against a checked-in fixture page served by `httptest` plus a sample definition* —
  `helper_test.go` serves `testdata/scraper/*` through `httptest.Server`; there is no network in
  the package's tests (§6.7) and every call takes a context with a deadline (§6.2).
- *Definitions for bundled lawful sources land in T-024, not here* — nothing in this branch is a
  real source. The three definitions are fixtures for invented sites on `example.org`.

**What each `Result` field carries.** Written out field by field rather than counted, because
the torznab adapter shipped three successive *counts* of the same property and QA falsified all
three (PR #12, rounds 1–3). The package doc in `scraper.go` carries the same list.

| Field | Treatment |
|---|---|
| `IndexerID` | Derived — the configured id. |
| `ID` | Derived **only** on the infohash branch (validated 40 hex / 32 base32). PASSED THROUGH on every other branch: an `id` field the definition selected is the page's text verbatim; the details/download branch goes through `withoutQuery`, which strips query and fragment from a value `url.Parse` gives a scheme to and returns anything else as it stands; the last-resort branch is the title. |
| `Title` | PASSED THROUGH, trimmed and narrowed by the definition's own regex/transforms if it set any. |
| `InfoHash` | Derived — only 40 hex or 32 base32 characters survive `normaliseInfoHash`. |
| `Magnet` | PASSED THROUGH, `dn=` included. A magnet `Resolve` derives is **not** clean either: `magnetFor` writes `Result.Title` into its `dn=` and `url.QueryEscape` encodes that text rather than removing it. |
| `TorrentURL` | The page's own download address, resolved against `base_url` and refused unless http(s) — and safe to log regardless: `internal/logging` masks on the name. |
| `SizeBytes` | Derived — parsed bytes. |
| `Seeders` | Derived — parsed integer. |
| `Leechers` | Derived — parsed integer. |
| `Category` | Derived — an `indexer.Category`, via `CategoryFromString`. |
| `Published` | Derived — a parsed `time.Time`. |
| `Uploader` | PASSED THROUGH, verbatim. There is not even the `://` refusal torznab applies: the uploader selector is the user's own choice, and `internal/logging`'s value-shape pattern already redacts a URL under any key. What neither catches is an opaque token, which is what an api key is. |
| `Trust` | Derived — an `indexer.Trust`, from the definition's own value map. |
| `SourceURL` | The page's own details address, resolved against `base_url` and refused unless http(s) — and safe to log: `internal/logging` masks on the name. |
| `Extra` | Never set. This schema has no `extra` block, so the map is always nil. |

So the fields that carry page text under a name `internal/logging` does not mask are,
exhaustively: `Title`, `Magnet`, `Uploader`, and `ID` on every branch but the infohash one —
the same inherited gap T-021 disclosed. It is not fixable inside an adapter (it is a property of
the frozen §5 `Result`), it is **not** re-litigated here, and backlog `T-934` remains the fix.
`TestWhichResultFieldsCanCarryTheCredential` asserts **both** directions: the derived fields
must stay clean, and those four must keep carrying what the page sent, so a change to either
forces this row, the package doc and DEC-071 to be revisited.

**Credential safety.** No error this package produces carries a param *value*, a path, or the
`base_url` — the three places a user plausibly writes their own credential into a definition. A
`ValidationError` does carry a key the user wrote (a field name, a param key, a trust mapping
key), a selector, a regex, a transform name or a mode, which is what the "actionable errors"
criterion asks for; DEC-073 states that boundary exactly. A YAML decode failure is reported by
line and column only, because `goccy`'s own message quotes the offending source line back
verbatim (verified against the library, not assumed: `yaml.FormatError(err, false, false)` was
run against seven malformed documents and each one embedded the source line). The schema has no
credential placeholder and refuses every `{{…}}` it does not define, so `{{apikey}}` is a
validation error rather than a feature (DEC-074).
`TestNoErrorFromThisAdapterCarriesTheCredential` sweeps twenty failure modes — every validation
failure, every query refusal, every response-shape failure, a 401, a cancelled context — with
the key configured on the client, hardcoded into a param value and written into the base
address, and asserts neither credential, no `apikey=`, and no absolute URL appears in the
message or anywhere in its unwrap chain. `TestNoCredentialReachesTheLogFile` drives all of them
plus a whole `Result` through the **real** `internal/logging` sink and greps the file.

**Proved by mutation, not asserted.** Each of these was applied, the named test run, and the
change reverted; `git status` is clean afterwards.

| Mutation | Test | Result |
|---|---|---|
| `parseBase` echoes the `base_url` into its error | `TestNoErrorFromThisAdapterCarriesTheCredential` | red |
| `checkTemplate` echoes the placeholder name | `TestNoErrorFromThisAdapterCarriesTheCredential` | red |
| `Title` is scrubbed instead of passed through | `TestWhichResultFieldsCanCarryTheCredential` | red |
| `magnetFor` drops the `dn=` | `TestResolveDerivesAMagnetThatInheritsTheTitlesGap` | red |
| `normaliseInfoHash` passes anything through | `TestWhichResultFieldsCanCarryTheCredential` | red *(see below)* |
| the credential is echoed into a `Title` the log test logs | `TestNoCredentialReachesTheLogFile` | red |
| the depth guard is disabled | `TestGuardHTMLDepthOnItsOwn` | red |
| the rows cap is removed | `TestASelectorThatMatchesThousandsOfRowsIsCapped` | red |
| fields are not inherited from the definition | `TestLatestUsesItsOwnBlockAndInheritsSharedFields` | red |
| a block cannot override an inherited field | `TestLatestUsesItsOwnBlockAndInheritsSharedFields` | red |
| `ValidationError.Error` stops naming the selector | `TestValidationNamesTheFailingFieldAndSelector` | red |
| a `javascript:` link is no longer refused | `TestAPageLinkThatIsNotHTTPIsRefused` | red |

The `normaliseInfoHash` mutation is the one that mattered: on the first run it left the test
**green**, because `echoingPage` put a real hash in the `data-infohash` attribute, so
`Result.InfoHash` could not have carried the credential however the derivation behaved — the
same vacuity QA found twice on T-021. The fixture now puts the credential in that attribute and
takes the real hash from the magnet's `xt`, the assertion also pins the derived value, and the
mutation is red.

**Hostile input.** The response is the source's and the definition is user-supplied, so both are
treated as hostile, and each case below is a test in `hostile_test.go` rather than a claim.
A document nested 100 000 deep is refused by a linear pre-scan before the parser sees it
(`ErrDocumentTooDeep`; see the measurements in `html.go` and DEC-077) while a page with
thousands of *unclosed* `li`, `tr` and `p` elements — which HTML5 closes implicitly and
which therefore nest not at all — still parses. Tag-shaped text inside `script`, `style` and
comments is skipped rather than counted. A selector matching 3000 rows is capped at 1000, in
both modes. `(a+)+$` against a 40 000-character value is linear, because Go's RE2 has no
backtracking and rejects the constructs that would need it — checked rather than assumed. A YAML
alias bomb is refused by strict decoding before anything expands, and an alias attached to a key
the schema *does* define was measured to cost memory linear in the source, because `goccy`
shares the aliased value rather than expanding it (measured: a nine-level, fan-nine bomb decodes
in under a millisecond with no measurable allocation). JSON nested 100 000 deep is refused by
`encoding/json`'s own depth limit in constant time — verified against this Go version rather
than assumed. A body that is a JSON string, a number, `null`, an HTML page, truncated or empty
is an error or an empty result set, never a panic, and `TestNoResponseShapeEverPanics` runs ten
more shapes through both modes.

**Deliberately not done.** No details-page fetch in `Resolve` — that needs a `detail` block, and
the schema this task ships is the one its criteria enumerate (backlog `T-939`). No category
push-down, so `Caps.Categories` is honestly `false` (backlog `T-938`). No `{{page}}` placeholder
(backlog `T-940`). No `extra` block, so `Result.Extra` is always nil. No loader — reading
definitions off disk is T-023 — and no bundled source, which is T-024. The infohash and magnet
helpers are duplicated from the torznab adapter rather than shared, because §4 forbids one
adapter importing another and the alternative grows the package holding the frozen §5 contracts
(backlog `T-937`).

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
status: done
depends: T-022
```
**Files:** `internal/indexer/scraper/loader.go` and `loader_test.go`; `internal/config/paths.go`
and a new `paths_test.go`; the "Where definitions live, and when they are read" section of
`docs/indexer-definitions.md`; and the package doc in `internal/indexer/scraper/scraper.go`.
`git diff --stat main` lists exactly those six files: two new source/test pairs, one edited
source file, one edited document. No §5 type was touched and no TUI file exists yet to touch.

**What landed.** `scraper.NewLoader(LoaderOptions) (*Loader, error)` resolves the definitions
directory and touches no disk; `(*Loader).Reload() error` reads every `*.yml` in it, parses and
validates each file independently, and publishes the result as an immutable `*Snapshot` through
one `atomic.Pointer.Store`. Readers go through `Snapshot()`, `Definitions()`, `Definition(id)`
and `Skipped()`. The exported surface is, enumerated: `Loader`, `LoaderOptions`, `Snapshot`,
`Skipped`, `NewLoader`, `ErrDefinitionTooLarge`, `ErrDefinitionIDDuplicated`, and the methods
just named plus `Dir()`. Every one has a doc comment.

**"Exposed in Settings" — what that means here, literally.** Settings is T-080/T-082 and does not
exist; `internal/tui/` has no files yet. This task ships `Reload()` as the public seam a settings
screen calls (README.md already documents `r` in Settings as the reload key) and **no TUI code**.
Nothing in this branch renders, routes a key, or imports bubbletea. That criterion is therefore
satisfied as the method-level half only, and the screen-level half belongs to T-080/T-082.

**Path resolution is not duplicated.** `internal/config` gains one field, `Paths.DefinitionsDir`,
set to `filepath.Join(ConfigDir, "definitions")` **after** the `--config` override, so it follows
`$TORTUI_HOME`, `--config` and the per-OS root that `internal/platform` already resolves (§14).
There is no second XDG implementation in this branch: `scraper` calls `config.ResolvePaths("")`
when `LoaderOptions.Dir` is empty, and `go list -deps ./internal/config` shows `config` importing
only `internal/platform`, so the new import direction creates no cycle.

**Atomicity is proved by a `-race` test, not asserted.**
`TestReloadSwapsAtomicallyUnderConcurrentReaders` runs 8 reader goroutines × 200 rounds against
4 reloader goroutines × 200 rounds and requires every read to see the whole three-definition set
and an id index that agrees with it. Proved non-vacuous by mutation: rewriting `Reload` to store
an empty `Snapshot` first and fill its fields in afterwards makes `go test -race` report
`WARNING: DATA RACE` between `(*Loader).Reload` and `(*Snapshot).Definitions` and fail. The
mutation was reverted and `diff` against a pre-mutation copy confirms the file is byte-identical.

**Proved by mutation, not asserted.** Each mutation was applied to the named file, the named test
run, and the change reverted; `git status` is clean afterwards and every one was RED.

| Mutation | Test |
|---|---|
| `Reload` publishes an empty snapshot and fills it in afterwards | `TestReloadSwapsAtomicallyUnderConcurrentReaders` (data race) |
| a file that will not parse aborts the whole load | `TestOneMalformedFileAmongValidOnesIsSkippedAndLogged` |
| the skip is logged at Debug instead of Error | `TestOneMalformedFileAmongValidOnesIsSkippedAndLogged` |
| `Parse` stops calling `Validate` | `TestADefinitionThatFailsValidationIsSkippedRatherThanReturned` |
| the extension match becomes case-sensitive | `TestAnUppercaseExtensionIsReadOnEveryPlatform` |
| the duplicate-id check is removed | `TestTwoFilesDeclaringTheSameIDKeepTheFirstByFileName` |
| the size cap is removed | `TestAFileLargerThanTheLimitIsSkipped` |
| a failed directory read publishes an empty set | `TestADirectoryThatCannotBeReadIsReportedAndLeavesTheLastSetInPlace` |
| a missing directory is reported as an error | `TestAMissingDirectoryIsNotAFailure` |
| `Definitions()` hands out the snapshot's own slice | `TestMutatingTheReturnedSliceDoesNotChangeTheLoadersSet` |

**Decisions this task had to make, because the criteria do not.** Each is a `DEC-` row rather
than a silent choice: what counts as a `*.yml` file and how its case is compared (DEC-079), what
"skipped" means for a file the criteria do not mention — a missing directory, an unlistable one,
a duplicate id, an oversized file (DEC-080), and the fact that the loader is the first thing in
this package that logs at all, which contradicted a sentence in the T-022 package doc (DEC-081).

**Credential safety.** `internal/indexer/scraper`'s no-content-in-errors rule (DEC-073) now has a
consumer that writes to a log file, so the rule is asserted here rather than inherited.
`TestNothingTheLoaderLogsOrSkipsCarriesTheCredential` drives five failure modes — a YAML syntax
error on the line holding the key, an unknown key in a file whose params hold the key, an
uncompilable selector in a file whose `base_url` holds the key, an oversized file whose padding
holds the key, and a duplicate id in a file whose params hold the key — through the **real**
`internal/logging` sink and applies the package's own `assertNoCredentialLeak` to the log file
and to every `Skipped.Err`. What the loader adds to an error is a file's **base name** and
nothing else; `readCapped` never repeats the path, and `errWithoutPath` strips it out of an
`fs.PathError` so "permission denied" survives and the user's home directory does not.
`TestAFileThatCannotBeReadIsSkippedWithoutNamingItsPath` pins that.

**Hostile input.** The definitions directory is the user's own, so the threat model is a mistake
rather than an attacker, and the guards are sized accordingly: one file is capped at **1 MiB**
before it is parsed (a limited reader stops one byte past the bound, so nothing larger is read
into memory), sub-directories and non-`.yml` entries are never opened, and each file goes through
T-022's `Parse`, which is strict and already refuses an alias bomb (DEC-075). There is no
recursion and no symlink walk.

**Portability.** No `runtime.GOOS` switch anywhere, in source or tests (§14). The two tests that
need an unreadable file or directory ask the filesystem rather than the OS name: `makeUnreadable`
chmods, then checks whether the thing is genuinely unreadable and **skips** when it is not, which
covers both root and Windows, where `os.Chmod` only moves the read-only bit. Both ran (not
skipped) on darwin/arm64 for this branch. `GOOS=windows go vet ./...` and `GOOS=linux go vet
./...` are green.

**Verification.** `make check` green. `go test -race ./... -count=1` green.
`internal/indexer/...` coverage is **99.7%** of statements, `internal/indexer/scraper` alone
**99.5%** — well over §9's 75% floor. The three partly-covered functions in `loader.go` are
`NewLoader` 92.9% (the `config.ResolvePaths` failure branch), `readCapped` 84.6% and
`errWithoutPath` 75.0% (the mid-read and close-failure branches); every other function in the
file is 100%. `scripts/check-indexer-hostnames.sh main` is clean and the script is untouched.

**Deliberately not done.** No filesystem watcher and no polling: "hot-reload" here is the
explicit `Reload()` the criteria name, driven by the user, and a watcher is backlog `T-942`. No
registry wiring — nothing turns a loaded definition into a registered `Indexer` yet, and the
config schema names a definition by **file path** (`definition = "example.yml"`, T-002) while
this loader indexes by the definition's own `id`; reconciling the two is backlog `T-943`, not a
guess made here. No `.yaml` extension (`T-944`). No directory creation, no writing, no import
from a path or URL (that is T-025), and no bundled definition (T-024).

**Acceptance**
- Loads all `*.yml` from `$XDG_CONFIG_HOME/tortui/definitions/`.
- Bad definitions are skipped with a logged error; one broken file never blocks startup.
- `Reload()` re-reads from disk and swaps definitions atomically, exposed in Settings.
- Test covers: valid dir, empty dir, one malformed file among valid ones.

---

### T-024 · Bundled lawful default sources
```
status: done
depends: T-022, T-023
```
**Files:** `internal/indexer/scraper/builtin/`, `docs/bundled-sources.md`

**Notes:** Ships one bundled source, the Internet Archive
(`internal/indexer/scraper/builtin/definitions/internet-archive.yml`), against the officially
documented Advanced Search API (`archive.org/advancedsearch.php`, JSON output). Every field the
definition reads (`identifier`, `title`, `btih`, `item_size`, `publicdate`) was verified live
during this task, not inferred — see `docs/bundled-sources.md` for the full verification
writeup and field-by-field mapping. Both `search` and `latest` blocks are implemented against
the same endpoint and covered by a live `//go:build integration` test
(`TestInternetArchiveLiveSearchAndLatest`) plus fixture-driven unit tests reproducing a captured
real response. Academic Torrents, the other named candidate, was evaluated and dropped — no
documented per-query search endpoint, and its documented workaround (mirror the full database,
search offline) independently conflicts with AGENT.md §2's "no index of your own" — recorded in
DEC-082 and `docs/bundled-sources.md`, with no reference to it left anywhere else in the repo. A
third, unnamed candidate (a distro release listing) was deliberately not evaluated in this task
to avoid picking and under-verifying one under time pressure; open as a future candidate if a
second bundled source is wanted.

`internal/indexer/scraper/builtin.Merge` implements the user-override-by-`id` rule and
`Definitions()` exposes the embedded, validated set — both fully tested — but neither is wired
into a running registry or a first-run flow, because `internal/app` (the composition root) does
not exist yet; nothing in this repository currently starts a search. This is the same
"implement the package's full behaviour now, defer main-wiring to the task that has somewhere to
wire it into" pattern T-002/T-003 used (DEC-028), not a shortfall against the "enabled on first
run" acceptance line: once the composition root exists, wiring in `builtin.Merge(loader.Definitions())`
ahead of the registry needs no further design decision here.

`docs/indexer-hostname-allowlist.md` gained one entry, `archive.org`, per its own documented
process. `make check` is green (see PR). No source whose primary use is distributing infringing
content is named or bundled anywhere in this change.

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
status: done
depends: T-023
```
**Files:** `internal/indexer/scraper/import.go`, `internal/indexer/scraper/import_test.go`,
`docs/indexer-definitions.md`

**Notes:** `scraper.Importer` (`NewImporter` / `(*Importer).Import`) validates and installs a
definition from a local file path or an `http(s)://` URL. A URL is fetched with exactly one GET
through `httpx.Client` — bounded size (`importMaxBytes`, matching T-023's 1 MiB), bounded time
(`importFetchTimeout`, 30s end to end including retries), http/https only, no cross-host or
downgrading redirect (T-020/T-021), and no credential attached, since a definition file belongs
to no one's account. A local path goes through the loader's own `readCapped`, so both paths are
held to the same size limit. Both paths are parsed with the existing `Parse`/`Validate`, so a
definition that fails validation is rejected with the same `*ValidationError` (failing key, and
selector when the failure is about one) the loader and a hand-edited file already produce, and
`Import` writes nothing before that check passes — proven by `assertDirEmpty` after every
rejection case in `import_test.go`. Only the native schema is supported; see DEC-083 for why
mapping a third-party format was not attempted in this task.

**Hostile input.** A definition's `id` has no charset restriction elsewhere (`Definition.Validate`
only requires it non-empty), but `Import` uses it to build the destination file name, and an id
reaching `Import` can come straight from the URL the user pointed tortui at or from a file
downloaded from one — the exact "remote-derived value about to become a path" AGENT.md §6.11
warns about. `idFilenamePattern` (`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`) excludes every path
separator and rules out `.`/`..` as a whole stem by requiring an alphanumeric first character; an
id outside it is rejected with `ErrImportIDUnsafe` before anything is opened for writing.
`TestImportRejectsAnIDThatIsUnsafeAsAFileName` covers `../escape`, `../../etc/passwd`, `a/b`,
`` a\b ``, and both empty-id cases. Nothing is ever overwritten: `checkNoConflict` refuses an id
that already names a file in the definitions directory, compared with `strings.EqualFold` so a
directory holding `Example.yml` cannot silently take a second `example.yml` as the same file on
APFS while creating a genuinely separate one on Linux (AGENT.md §13) —
`TestImportRefusesToOverwriteCaseInsensitively` pins this. The write itself is atomic: a temporary
file in the same directory (so the rename is same-filesystem) followed by `os.Rename`, so a
concurrent `Loader.Reload` never observes a half-written file, and a failure at any step before
the rename leaves the directory exactly as it was (`TestImportLeavesNoTemporaryFileBehind`).

**Fetches only what it's given.** `Import` issues exactly one request for the exact address
passed in — no directory listing, no discovery request, no following a link found on a fetched
page — pinned by `TestImportFetchesOnlyTheExactAddressGiven`, which counts the requests a test
server actually received. There is no definition repository, no index of available definitions,
and no update feed anywhere in this package or in `docs/indexer-definitions.md`'s new "Importing
a definition" section, which documents the same one-shot, nothing-discovered contract for a user
reading it from outside the code.

**Verification.** `make check` green. `go test ./...` green. `internal/indexer/scraper` coverage
is 97.3% of statements (§9 floor is 75%); `import.go` itself ranges from 81.8% (`fetch`) to 100%
(`Dir`, `looksLikeURL`, `filenameForID`), with the lowest, `writeDefinitionAtomically` at 57.1%,
being the `os.MkdirAll`/`Chmod`/`Close`/`Rename` failure branches, which is consistent with the
rest of this package's own coverage notes (T-023's `readCapped` sits at a similar level for the
same reason: exercising an actual `Chmod`/`Rename` failure needs a hostile filesystem, not a
hostile input).

**Deliberately not done.** No Settings-screen wiring: the settings screen that would call
`Import` is T-080/T-082, which does not exist yet, the same "implement the package's full
behaviour now, defer main-wiring to the task that has somewhere to wire it into" pattern
T-023/T-024 used (DEC-028). `config.example.toml` is unchanged — nothing about the config schema
changed, only a new way to populate the definitions directory the schema already reads from. No
third-party format mapping (DEC-083).

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
status: done
depends: T-002
```
**Files:** `internal/engine/engine.go`, `internal/engine/engine_test.go`,
`internal/engine/fake/fake.go`, `internal/engine/fake/script.go`,
`internal/engine/fake/fake_test.go`, `internal/engine/fake/script_test.go`

**Acceptance**
- `Engine`, `TorrentStatus`, `State`, `AddSource`, `FileStatus`, `Origin` per AGENT.md §5.
- `fake` drives scripted progress on a controllable clock: downloads, stalls, errors, completes.
- `fake` satisfies the full interface and is used by every TUI test thereafter.
- Frozen after this task.

**Notes:** `internal/engine/engine.go` declares the six §5 types (`State`, `Origin`,
`FileStatus`, `TorrentStatus`, `AddSource`, `Engine`) plus a `State.String()` for logging,
following the `Trust.String()`/`Badge()` precedent from T-010. `Origin` and `FileStatus` are
referenced by §5's prose (`Files(id) ([]FileStatus, error)`, `TorrentStatus.Origin`) but never
given a field-level shape there, so their shape was a judgement call — see DEC-084.
`internal/engine/fake` is driven by an explicit `Advance(d time.Duration)` call rather than a
real ticker: each tracked torrent carries a `Script` (an ordered, order-independent list of
`Event`s keyed by simulated run time) and `Advance` moves every non-paused torrent's run time
forward and re-applies its `Script`'s step function. This is what "controllable clock" means
here — a test drives simulated time explicitly instead of sleeping out a real one (AGENT.md
§6.7's zero-network-calls-in-unit-tests spirit extended to zero-real-sleeps-in-unit-tests).
Four presets cover the acceptance criterion's named behaviours: `Downloading` (checks, then
progresses 0%→100% over a given duration, ends `StateSeeding`), `Stalled` (reaches a progress
then never advances again — rates drop to zero, ETA becomes unknown), `Errored` (fails at a
given time with a caller-supplied error), and `Completed` (already fully seeded from t=0).
Pausing freezes a torrent's own run-time accumulator (so the paused interval never counts
against its `Script`) and forces `StatePaused`/zero rates for display; `Resume` un-freezes it.
`Updates()` is a buffered-1 channel with drop-oldest-then-send semantics, matching "coalesced" —
verified by a test that calls `Advance` three times with nothing reading in between and checks
exactly one (the latest) snapshot is waiting. `Remove` is immediate/synchronous (no async
teardown to simulate) and `deleteData` is accepted but not acted on, since a fake torrent has no
on-disk data. `go test ./internal/engine/...` coverage is 96.9% (`internal/tui` doesn't exist yet
so `internal/engine`'s own ≥75% floor from AGENT.md §9 is what applies), and `go test -race
./internal/engine/...` is green, including a concurrent-use test exercising `Add`/`Pause`/
`Resume`/`List`/`Files`/`Advance` from multiple goroutines at once. `make check` is green.
`internal/engine/anacrolix` (T-031) is untouched — no engine implementation beyond `fake` was
added, per AGENT.md §11.5's "do not build ahead."

---

### T-942 · Admit MPL-2.0 for the torrent engine

```
status: done
depends: T-030
```
**Files:** `AGENT.md`, `Makefile`, `NOTICE`, `README.md`, `scripts/check-license-scope.sh`,
`scripts/check-license-scope_test.sh`, `.github/workflows/ci.yml`

**Done on 2026-09-17.** `Makefile`'s `ALLOWED_LICENSES` gained one entry
(`...,ISC,MPL-2.0`), AGENT.md §3's dependency-license footer and §16's "Project license"
paragraph were updated together (§16 now also explains why MPL-2.0's file-level copyleft is
admissible here and strong copyleft is not), and `.github/workflows/ci.yml`'s licenses-job
comment was corrected to match. `README.md`'s License section states the project's own license
is unchanged (MIT) and notes the MPL-2.0 exception for `anacrolix/torrent`, including that it
isn't a `go.mod` dependency yet so it doesn't appear in `NOTICE` yet. `NOTICE` itself is
byte-identical after `make licenses` on all three `LICENSE_OSES` — there is nothing to
attribute until T-031 adds the dependency; the generation mechanism (`license_url` per
dependency) already satisfies MPL-2.0 §3.2 once it does. See DEC-098 for the authorisation, the
empirical proof that GPL/AGPL still fail `go-licenses check` under the new allowlist, and the
obligations accepted. No engine code was written and `internal/engine/anacrolix` does not exist;
T-031 is left `blocked` for the orchestrator to move once this merges. No `.go` file changed.
Verified: `make check`, `make licenses` (darwin/linux/windows, `NOTICE` unchanged), `go test
-race ./...`, `golangci-lint run` cross-compiled for `GOOS=windows/linux/darwin` (0 issues each),
`make build-all` (all six tier-1 targets).

**QA remediation, same date.** QA passed all six acceptance criteria and the §16 MPL-2.0
reasoning, but failed the PR on a scope gap: `go-licenses check --allowed_licenses` has no
per-module scoping (`go-licenses check --help`, v2.0.1, confirms `--allowed_licenses` is a flat
license list with no module-targeting flag), so the gate as shipped admitted **any** MPL-2.0
module, not only `anacrolix/torrent` — the "for the torrent engine alone" scope was a policy
statement in prose, not something CI enforced. Remedied by adding
`scripts/check-license-scope.sh`, a POSIX `sh` script that reads the same `go-licenses report`
CSV data the `licenses` target already generates for `NOTICE` (no second scan) and fails the
build, naming the offender, if any MPL-2.0 row's module is not `github.com/anacrolix/torrent`.
Wired into the `Makefile`'s `licenses` target's existing per-`LICENSE_OSES` report loop, so it
runs on all three tier-1 operating systems exactly like the pre-existing `go-licenses check`
loop, with `LC_ALL=C` on the awk pass for the same determinism reason as the NOTICE merge.
`scripts/check-license-scope_test.sh` is its regression test, built the same way
`check-indexer-hostnames_test.sh` is (disposable fixtures, no network, no ambient repo state)
and wired into `test-scripts` (so `make check` runs it too). **Enforcement proof:** a disposable,
uncommitted copy of this repo had a throwaway local module added
(`github.com/example/fake-unrelated-mpl-module`, real `anacrolix/torrent` MPL-2.0 `LICENSE` text
copied in verbatim, wired in via a `replace` directive and referenced from a scratch
`internal/licenseproof` package) — `make licenses` there failed at the new scope-check step with
`check-license-scope: MPL-2.0 is admitted only for github.com/anacrolix/torrent ... found MPL-2.0
module(s) outside that scope: - github.com/example/fake-unrelated-mpl-module`, `exit 1`, even
though the pre-existing `go-licenses check --allowed_licenses` loop passed it (proving the gap
was real). Removing the fake module and re-running `make licenses` in the same scratch copy
passed cleanly, `NOTICE` regenerating byte-identical to the real repo's. AGENT.md §3/§16,
README.md, and the `.github/workflows/ci.yml` licenses-job comment were updated to describe the
two-part mechanism (global allowlist + per-module scope check) instead of implying the allowlist
alone was scoped. See DEC-099 for the residual gap this still leaves and why it's disclosed
rather than papered over. Re-verified: `make check`, `make licenses` (darwin/linux/windows,
`NOTICE` unchanged), `go test -race ./...`, `golangci-lint run` cross-compiled for
`GOOS=windows/linux/darwin`, `make build-all`.

**Why this exists.** T-031 is `blocked` on it — see the Blocked section for the full finding.
`github.com/anacrolix/torrent`, the engine AGENT.md §3 locks and DEC-001 selected, is **MPL-2.0**.
Every license gate in the project excludes it: AGENT.md §3's footer, AGENT.md §16 / DEC-009, and
`Makefile`'s `ALLOWED_LICENSES`. `make licenses` therefore fails the moment the engine enters
`go.mod`, and the whole T-031 → T-034 → T-041 → T-070+ chain is dead behind it.

**The project owner authorised this on 2026-09-17**, choosing to admit MPL-2.0 rather than replace
the locked engine. This is a deliberate change to the license policy in AGENT.md §3 and §16 — it is
authorised, not agent-initiated, and it does **not** reopen DEC-001 or any other §3 stack row.

**Acceptance**
- `ALLOWED_LICENSES` in the `Makefile` admits MPL-2.0, and `make licenses` still passes on all
  three `LICENSE_OSES`. The allowlist stays an allowlist — no blanket copyleft admission, and
  GPL/AGPL stay excluded.
- AGENT.md §3's footer and §16's dependency-license paragraph are updated together, so no gate is
  left contradicting another. §16's reasoning is extended to say *why* file-level copyleft is
  admissible here and strong copyleft is not.
- `NOTICE` regenerates cleanly and attributes the MPL-2.0 dependency correctly, including where its
  source can be obtained (MPL-2.0 §3.2).
- `README.md` states the project's own license is unchanged (MIT) and notes the MPL-2.0 dependency.
- A `DEC-` row records the authorisation, the exact scope of the exception, the obligations
  accepted, and what was explicitly *not* changed.
- No engine code. T-031 stays a separate task; this one only moves the gate.
- **(QA remediation)** The "for `anacrolix/torrent` alone" scope is enforced by the gate, not
  only asserted in prose: an MPL-2.0 module other than `anacrolix/torrent` fails `make licenses`
  on all three `LICENSE_OSES`, proven with a real (non-simulated) case, and every doc surface
  describes the two-part mechanism (global allowlist + per-module scope check) accurately,
  including any residual gap.

---

### T-943 · Extend the MPL-2.0 exception to the engine's named module set

```
status: done
depends: T-942
```
**Files:** `AGENT.md`, `Makefile`, `README.md`, `scripts/check-license-scope.sh`,
`scripts/check-license-scope_test.sh`, `.github/workflows/ci.yml`, `TASK_TRACKER.md`

**Owner-authorised on 2026-09-18** (option 1a from T-031's Blocked entry), after option 1b was
pursued and no replacement engine cleared the gates. Widens DEC-098's single-module MPL-2.0
exception to the named set `anacrolix/torrent` actually compiles against. Adds no dependency and
no `.go` file — T-031 still owns that.

**Done on 2026-09-18.** `ALLOWED_MPL_MODULE` became `ALLOWED_MPL_MODULES`, a comma-separated
enumeration of ten module paths; `scripts/check-license-scope.sh` now splits that list into a set
in its existing `awk` pass and keeps its contract unchanged (exit 0 in scope, exit 1 naming every
offender plus the admitted set, exit 2 on usage error — now including an empty list — POSIX `sh`,
`shellcheck -s sh` clean). AGENT.md §3's footer and §16's "Project license" section (which gained
a table of the ten modules and why each is in the set), `README.md`'s License section and
`.github/workflows/ci.yml`'s licenses-job comment all describe a named module **set**, still not a
license family and explicitly not an `anacrolix/*` prefix. See **DEC-100** for the authorisation,
the derivation, the proofs and what did not change.

**The set was derived empirically, and it is not quite the list in the acceptance criteria.** In a
disposable copy of the repo outside the working tree: `go get github.com/anacrolix/torrent`
(v1.61.0) + a one-line probe package, then `go-licenses report ./...` on all three `LICENSE_OSES`.
The union of MPL-2.0 rows is **ten** modules — the eight the acceptance criteria name, plus
`github.com/go-llsqlite/adapter`, **plus `github.com/anacrolix/mmsg`, which the acceptance list
omits.** Reported here rather than added quietly: `mmsg` is reached via
`github.com/anacrolix/go-libutp` (itself MIT) on **any cgo-enabled build**, and `anacrolix/utp` is
the pure-Go uTP transport used whenever `CGO_ENABLED=0`. The two never appear together, and which
one is in the graph is decided by **`CGO_ENABLED`, not `GOOS`** — all six combinations were
measured: cgo on gives `mmsg` and no `utp` on darwin, linux and windows alike; cgo off gives `utp`
and no `mmsg` on all three. Darwin only looks special because on a Mac `GOOS=darwin` is the native,
cgo-enabled target while the others cross-compile with cgo off; on the Linux CI runner `GOOS=linux`
is the native one. Both must be listed or `make licenses` fails on one host or the other. `mmsg` is
inside the owner's authorisation as written ("the `github.com/anacrolix/*` family that
`anacrolix/torrent` compiles against"): MPL-2.0, same author, unmodified, unavoidable. No module
the acceptance list names is absent from the tree.

**`go-llsqlite/adapter` resolved visibly, with no `--ignore`.** The pinned
`v0.0.0-20230927005056-7f5ce7f0c916` really does ship no LICENSE (reproduced: `make licenses`
exits 1 on `GOOS=darwin` with `Did not find license for library 'github.com/go-llsqlite/adapter'`).
The module now has tags, but **`v0.1.0` is equally unlicensed** — its module directory carries no
license file of any name (listed directly, plus a recursive `*licen*`/`*copying*` search that
returns nothing). `v0.2.0` is the first revision that carries the upstream MPL-2.0 `LICENSE`; it
builds against `anacrolix/torrent v1.61.0` and leaves **zero `Unknown` rows** in any per-GOOS
report, so it is admitted as a named MPL-2.0 module like the other nine. **T-031 must pin it at
`v0.2.0` or later** — `go get github.com/anacrolix/torrent` on its own selects the unlicensed
pseudo-version and will fail `make licenses`.

**The regression test is the deliverable, and it was mutation-checked.**
`scripts/check-license-scope_test.sh` proves every listed module passes alone and all together,
that an unlisted MPL-2.0 module fails and is named (including
`github.com/anacrolix/not-a-listed-sibling`, which shares the admitted prefix — the set is names,
not a wildcard), and that a partially-listed set fails on exactly its unlisted members. A case
also pins the `Makefile`'s `ALLOWED_MPL_MODULES` to the list the test exercises, so widening the
gate without updating the test fails `make check`. Three mutations were confirmed to fail it:
stubbing the check script to `exit 0`, rewriting the enumeration as an `^github\.com/anacrolix/`
regex, and appending an extra module to the `Makefile`'s list.

**Enforcement and barred-family proofs, with the real `go-licenses` v2.0.1 binary.** With the
widened gate and the engine in the scratch copy, `make licenses` passes on all three
`LICENSE_OSES` and generates a `NOTICE` with exactly ten MPL-2.0 rows, each with a `license_url`
(MPL-2.0 §3.2). Adding a throwaway `github.com/example/fake-unrelated-mpl-module` fails the build
at the scope step, named, even though `go-licenses check --allowed_licenses` passed it on all
three OSes. GPL-3.0, AGPL-3.0 and LGPL-3.0 each fail `go-licenses check` under the widened
`ALLOWED_LICENSES` (throwaway modules carrying the real FSF texts; LGPL additionally proven
against the real `github.com/juju/ratelimit`).

**Unchanged:** `go.mod`, `go.sum`, `NOTICE` (byte-identical — every dependency experiment lived in
a disposable copy outside the repo), no `.go` file, `ALLOWED_LICENSES` itself, and DEC-099's
residual gap, which is still disclosed rather than overclaimed. T-031 is left `blocked` for the
orchestrator to move once this merges. Verified: `make check`, `make licenses`
(darwin/linux/windows, `NOTICE` unchanged), `sh scripts/check-license-scope_test.sh`,
`shellcheck -s sh scripts/check-license-scope.sh`, `go test -race ./...`, `golangci-lint run`
cross-compiled for `GOOS=windows/linux/darwin`, `make build-all`.

**Acceptance**
- The MPL-2.0 exception covers an **explicitly enumerated list of module paths**, not a prefix
  wildcard and not a license family: `github.com/anacrolix/torrent` plus `dht/v2`, `generics`,
  `log`, `multiless`, `sync`, `upnp`, `utp`, and `github.com/go-llsqlite/adapter`. Verify the set
  empirically against the real dependency graph rather than copying this list — if the tree names
  a module this list omits, report it rather than silently adding it.
- `ALLOWED_MPL_MODULE` becomes a list (rename to `ALLOWED_MPL_MODULES`), and
  `scripts/check-license-scope.sh` accepts several allowed modules while keeping its current
  contract: exit 0 when every MPL-2.0 row is in the set, exit 1 naming every offender otherwise,
  exit 2 on usage error, POSIX `sh`, `shellcheck -s sh` clean.
- `scripts/check-license-scope_test.sh` proves: each listed module passes, an **unlisted** MPL-2.0
  module still fails, and a partially-listed set fails on the unlisted one. The regression test is
  the point of the task — a wider allowlist that nothing polices is worse than the status quo.
- `github.com/go-llsqlite/adapter`'s missing LICENSE is resolved **visibly**. Preferred: move to a
  revision that carries the upstream MPL-2.0 `LICENSE` (upstream `master` has one; the pinned
  `v0.0.0-20230927005056-7f5ce7f0c916` does not), so it is admitted as a named MPL-2.0 module like
  the rest. Fallback only if that is not reachable: record it as an explicit, documented exception
  with evidence. **A silent `--ignore` entry is not acceptable** — `NOTICE` must not omit a module
  without the omission being stated.
- GPL, AGPL **and LGPL** are proven — empirically, with the real `go-licenses` binary, not
  asserted — to still fail under the widened list. LGPL matters now: T-031's evaluation of
  `cenkalti/rain` surfaced `juju/ratelimit` as LGPL-3.0, so the barred-families claim in §16 must
  be tested, not assumed.
- AGENT.md §3's dependency-license footer, §16's "Project license" paragraph, `README.md`'s
  License section, and `.github/workflows/ci.yml`'s licenses-job comment all describe the widened
  mechanism accurately — a named module **set**, still not a license family.
- Decision Log entry (next free id, **DEC-100** — verify against `origin/main`) recording the
  owner authorisation, the exact set and why each member is in it, the obligations accepted
  (unchanged from DEC-098: MPL-2.0 §3.2 satisfied by `NOTICE`'s `license_url` column), and what
  did **not** change.
- `NOTICE` byte-identical on all three `LICENSE_OSES` (no dependency is added by this task).

---

### T-031 · anacrolix engine — add and list
```
status: done
depends: T-030, T-942, T-943
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

**Notes (resumed from WIP `1473bde`, 2026-09-23).** The owner elected to resume from the
unvetted WIP commit on `task/T-031-anacrolix-engine-impl` rather than discard it. It was audited
line by line, not trusted: `engine.go`, `logging.go`, `paths.go`, `rates.go`, `storage.go` and
their five `_test.go` files held up well (path-safety in `paths.go`/`storage.go` covers `..`,
absolute-looking segments, NUL bytes, Windows reserved names/drive letters and trailing
dot/space on every declared component including the torrent's own name; the client is wired with
an `Offline` mode so this package's own tests make zero network calls per AGENT.md §6.7) but two
real gaps existed exactly as flagged: `go.mod` had never had `go mod tidy` run against it (fixed —
see below) and **there was no test proving stderr stays empty**, despite `RedirectLogging` already
existing in `logging.go`. That test did not exist and was written from scratch this pass.
  - **The stderr test** (`internal/engine/anacrolix/stderr_test.go`,
    `TestRedirectLoggingKeepsStderrEmpty`) re-executes the test binary as a subprocess rather than
    swapping the `os.Stderr` package variable in-process: `github.com/anacrolix/log`'s
    `DefaultHandler` captures the `*os.File` `os.Stderr` pointed to at that package's own `init()`,
    long before any test can intervene, so an in-process variable swap would never observe a
    write made through it — proven by trying it and watching the write escape to the real
    terminal instead of the capture buffer. The subprocess pattern (`exec.Command(os.Args[0],
    "-test.run=^TestStderrSubprocessHelper$")`) gives the child a stderr pipe the OS wires up
    before the child's Go runtime — and therefore `anacrolix/log`'s package `init()` — ever runs,
    so nothing written to real fd 2 anywhere in the call chain can hide from it, and it is
    portable across all three tier-1 `GOOS` values with no build tags. The test is two-sided so it
    cannot be satisfied by a handler that can never fail: a baseline subprocess proves, with the
    library's real, unmodified default handler, that the exact call genuinely reaches stderr with
    nothing redirecting it; the real subprocess then makes that identical call, through the
    identical `alog.Default` façade every anacrolix package logs through, after constructing a
    real `Engine` (`New`, which calls `RedirectLogging` before it builds a client) and adding a
    real torrent, and requires stderr to be empty. Verified this is not vacuous by temporarily
    commenting out the `RedirectLogging` call in `New` and confirming the test fails, naming the
    canary line on stderr; restored before committing.
  - **`go mod tidy`** promoted `github.com/anacrolix/torrent` and `go.uber.org/goleak` to direct
    requirements (both were already present, marked `// indirect`, from the interrupted run).
    `github.com/go-llsqlite/adapter` stays `// indirect` — this package never imports it directly,
    only through `anacrolix/torrent/storage` — but remains pinned at `v0.2.0` as DEC-100 requires.
  - **Two lint findings** surfaced by a clean `golangci-lint run` (the WIP had never had one run
    against it): `contextcheck` on `fetchAndAttach` (it built its background-fetch context from
    `context.Background()` instead of the caller's `ctx`) and a `staticcheck` `QF1008` on
    `logging.go`'s embedded-field selector. Fixed rather than suppressed: `Add`'s `ctx` now flows
    through `addFromURL`/`fetchAndAttach`, decoupled from cancellation with
    `context.WithoutCancel` (the fetch's lifetime is the metadata timeout, not the Add call's,
    which has already returned by the time the fetch runs — carrying `ctx`'s values, e.g. tracing,
    forward without inheriting its deadline); `r.Msg.String()` became `r.String()` via `Msg`'s
    embedding in `alog.Record`. No `nolint` anywhere in this package.
  - **`govulncheck ./...`** initially reported 3 called-code advisories, all reached only through
    `anacrolix/torrent`'s optional webtorrent/WebRTC code paths (`GO-2026-6278` gorilla/websocket
    weak PRNG mask key; `GO-2026-6165` pion/dtls/v3 panic parsing a crafted ServerKeyExchange;
    `GO-2026-6163` pion/stun/v3 panic on a malformed XOR-MAPPED-ADDRESS attribute) — none of
    tortui's own code, all in transitive dependencies of the locked engine. `go get
    github.com/gorilla/websocket@v1.5.3 github.com/pion/dtls/v3@v3.1.4
    github.com/pion/stun/v3@v3.1.5` resolved cleanly against `anacrolix/torrent v1.61.0` with no
    `replace` directive and no change to the admitted MPL-2.0 set (`go.mod`/`go.sum` diff is
    version bumps only); re-running `govulncheck` afterwards found zero called-code
    vulnerabilities. Four remaining advisories in uncalled code (`go.opentelemetry.io/otel`
    baggage-header allocation, two `golang.org/x/crypto/ssh` deadlock DoS entries, and the
    unmaintained `golang.org/x/crypto/openpgp` package) are left alone: tortui's code does not
    reach any of them, and forcing every transitive module to its latest tag is out of this
    task's scope.
  - **Deferred to T-032+, as the acceptance criteria for this task allow:** `Pause`, `Resume`,
    `Remove`, and `Files` return `ErrNotImplemented` (never panic — covered by
    `TestLaterTaskMethodsReportNotImplemented`); `Updates()` establishes and closes the channel
    but does not yet emit snapshots (T-033); rate limiting and destination-root containment for
    `Remove`'s `deleteData` path are T-032's job, not this one's.
  - Coverage on `internal/engine/anacrolix` is 89.8% (`go test -coverprofile=... && go tool cover
    -func=...`), well over the §9 75% floor.
  - Full verification run and its outputs are in this PR's description.

---

### T-032 · Engine lifecycle operations
```
status: done
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

**Notes (2026-09-23, PR pending review — status left `in-progress` for the orchestrator to flip).**
`Pause`, `Resume`, `Remove`, and `Files` are implemented in `internal/engine/anacrolix/engine.go`;
`ErrNotFound` (already exported from T-031) is what an unknown id returns from all four —
`TestUnknownIDReportsErrNotFoundNeverPanics` replaces the old
`TestLaterTaskMethodsReportNotImplemented`, and the now-unreferenced `ErrNotImplemented` sentinel
was removed since no method in this package returns it any more. Two new path-safety helpers
landed in `paths.go`: `resolveSymlinks` (walks up to the nearest existing ancestor via
`filepath.EvalSymlinks`, then rejoins the non-existent suffix literally, so it works even when the
delete target itself is already gone) and `containedInRoot` (resolves both sides through it, then
refuses `target == root` on top of the existing `containedIn`, so `Remove` can never delete a root
itself). See DEC-102 for the four judgement calls this task made: FileStatus has no Priority field
to map (frozen §5 contract, left unpopulated rather than reinterpreted); pause-before-metadata
semantics (a `paused` flag independent of `state`, plus a `prePauseState` field so `Resume`
restores `StateDownloading` rather than a stale `StateChecking`); `Remove` always untracks even
when a requested delete is refused; and delete-time containment is re-derived from a *fresh* copy
of `e.roots` at call time and re-resolved through symlinks, independent of whatever Add already
checked. Coverage on `internal/engine/anacrolix` is 90.0% (`go tool cover -func`), over the §9 75%
floor. `NOTICE`/`go.mod`/`go.sum` are untouched — no new dependency. Full verification output is in
the PR description.

**QA remediation (2026-09-23, same PR/branch).** QA reproduced a goroutine leak: `Remove` on a
torrent whose metadata was still pending left `awaitInfo` blocked forever (or until the metadata
timeout/`Close`), because `anacrolix/torrent` v1.61.0's `Torrent.Drop`/`close` never closes the
channel `Torrent.GotInfo()` waits on — only a real info dictionary arriving does. Fix: each
`tracked` now owns a `done chan struct{}`, closed exactly once by `Remove` (the only writer — once
`Remove` deletes an id from `e.torrents`, `lookupLocked` can never find it again, so nothing can
close it twice) plus a `removed bool` flag set at the same time under the same lock. `awaitInfo`'s
`select` gained a `case <-tr.done: return`; `fetchAndAttach`'s own cancel-on-shutdown watcher
goroutine gained the same case so an in-flight `.torrent` URL fetch is aborted promptly on Remove
too, not just on engine `Close`. Because a URL fetch has no `*torrent.Torrent` yet at the moment
Remove runs (`tr.t` is still nil, so Remove has nothing to `Drop`), `attach` now checks `tr.removed`
under `e.mu` right after `client.AddTorrentSpec` returns and, if set, drops the newly-obtained
torrent immediately and never spawns `awaitInfo` for it — closing the "removed-then-attached" race
QA also flagged, so a removed torrent can never be resurrected into an active, untracked swarm.
Two regression tests added, both asserting via a polling `goleak.Find` **without ever calling
`Close`** first (only `Remove` is allowed to make them pass): `TestRemoveOfAPendingMetadataTorrentLeavesNoGoroutine`
(a magnet, `MetadataTimeout: time.Hour`, `Remove` then asserted leak-free) and
`TestRemoveDuringAnInFlightTorrentURLFetchDoesNotLeakOrReattach` (a gated `httptest.Server`,
`Remove` while the handler is still blocked, then the handler released — proving `fetchAndAttach`/
`attach` don't resurrect it into `List`/`Files` regardless of which of the two guards above ends
up winning the race in a given run). A `waitForNoLeaks` helper ignores the two goroutines an
`*Engine`/`torrent.Client` keep running for its whole lifetime by design (`sampleRates`, the
client's internal `acceptLimitClearer`) via `goleak.IgnoreTopFunction`, since these tests
deliberately never reach the `Close` that would otherwise reap them. Non-blocking finding also
fixed: the symlink-privilege test skip only matched `os.ErrPermission`, which Go does not map
`ERROR_PRIVILEGE_NOT_HELD` (1314) onto — the actual errno Windows returns when the account lacks
`SeCreateSymbolicLinkPrivilege`. Two new build-tag-gated test files (following the existing
`internal/tui/theme/capability_windows_test.go` precedent for OS-specific test-only code outside
`internal/platform`) — `symlink_privilege_windows_test.go` and `symlink_privilege_other_test.go` —
add `isUnprivilegedSymlinkError`, checked via `syscall.Errno(1314)` comparison on Windows and
`os.ErrPermission` elsewhere; no `runtime.GOOS` switch anywhere, and the skip stays conditional,
never unconditional. See DEC-102 for the added judgement calls. Re-verified: `make check`,
`go test -race -count=1 ./internal/engine/...`, `make cover` (`internal/engine/anacrolix` 89.7%),
`make licenses` (all three `LICENSE_OSES`, `NOTICE`/`go.mod`/`go.sum` unchanged), cross-`GOOS`
`golangci-lint` (linux/windows/darwin, 0 issues each), `GOOS=windows go vet ./...` (clean),
`make build-all` (all six targets), `govulncheck ./...` (0 called-code vulnerabilities, same
uncalled-code advisories as before). PR checks re-run and green on all jobs.

---

### T-033 · Update stream
```
status: done
depends: T-031
tier: H
```
**Acceptance**
- `Updates()` emits a full `[]TorrentStatus` snapshot at ~2 Hz, coalesced — no per-torrent spam.
- Channel is buffered; a slow consumer drops stale snapshots rather than blocking the engine.
- ETA computed from a rolling rate average, `-1` when indeterminate.
- Closing the engine closes the channel exactly once.
- Test asserts cadence, coalescing, and drop-on-slow-consumer behaviour.

**Notes:** The sample loop (`sampleRates`, `RateSampleInterval`, default 500ms) now builds the full
snapshot after each rate sample and sends it only if it differs from the last one sent (DEC-104),
so events never send on their own. `publish` drains an unread snapshot before sending, into a
1-slot buffer: a slow consumer reads the newest state and the sampler never blocks. ETA uses a
10-sample rolling average (`rateMeter.avg`, ~5s); `DownRate` stays instantaneous. Tests drive an
unexported injectable ticker (`Options.newTicker`) tick by tick, with no sleeps. `anacrolix`
coverage 90.4%.

---

### T-944 · Engine review follow-ups from T-031/T-032 QA

```
status: done
depends: T-033
tier: M
```
**Files:** `internal/engine/anacrolix/engine.go`, `internal/engine/anacrolix/engine_test.go`,
`internal/config/load_test.go`, `internal/doctor/doctor.go`

**Why this exists.** Three independent reviewers (PRs #31 and #32) passed the engine with these
NON-BLOCKING observations. None affects shipped behaviour today; each is a gap a future change
could silently widen, so they are tracked here rather than fixed ad hoc.

**Acceptance**
- `Add` for the same infohash is idempotent under **concurrent** calls: the `findByInfoHash` →
  `track` sequence in `addSpec` runs under one critical section (or an equivalent), and a test
  fires N concurrent `Add`s of one magnet and asserts exactly one tracked entry.
- The `tr.removed` guard in `attach` is exercised by a test that would fail if the guard were
  deleted: after `Remove` during an in-flight `.torrent` URL fetch resolves, the underlying
  `torrent.Client` holds no torrent for that infohash (assert via `client.Torrents()` count or a
  package-internal hook), not merely that `List()`/`Files()` no longer see it.
- `runtime.GOOS` appears nowhere outside `internal/platform` (AGENT.md §14): the pre-existing
  uses in `internal/config/load_test.go` and `internal/doctor/doctor.go` are moved behind
  `internal/platform` or build tags, and a lint rule or script test fails `make check` on any
  future occurrence.
- `make check` green; coverage floors hold.

**Notes:** `addSpec` now calls a new `findOrTrack` that does the infohash lookup and the mint under
one `Engine.mu` critical section (DEC-105); `tracked.infoHash` is set at track time for a
magnet/file and at `attach` time for a URL source, so `findByInfoHash` matches a pending entry too.
Added `TestAddIsIdempotentUnderConcurrentCalls` (20 goroutines) and a `client.Torrents()` assertion
in the existing in-flight-fetch-removal test. Added `internal/platform.OS()`/`IsWindows()`;
`load_test.go` and `doctor.go` now call those instead of `runtime.GOOS`.
`scripts/check-goos-scope.sh` (+ `_test.sh`) scans tracked `*.go` files outside `internal/platform`
for the literal `runtime.GOOS` and fails; wired into `make check` via a new `check-goos-scope`
target. `make check` green; `anacrolix` coverage 90.5%.

**QA remediation (2026-09-25, same PR/branch, pre-merge).** Review found two problems in the fix
above, both now corrected (DEC-105 has the full account). First, the `client.Torrents()` test never
actually exercised the `tr.removed` guard: closing `tr.done` on `Remove` cancels `fetchAndAttach`'s
own HTTP request context, so a `Remove` landed during the network wait fails the fetch before
`attach` is ever called — a mutation deleting the guard entirely still passed. Fixed with
`Options.beforeAttach`, a package-test-only hook `fetchAndAttach` calls right after a successful
metainfo parse and before `attach`, letting the test land `Remove` in the window the guard actually
protects; the guard-deletion mutation now fails. Second, `findOrTrack` matching a not-yet-attached
entry by infohash also matched one whose `attach` call had already *failed* (e.g. an unsafe path
refused by `validateSpecPaths`) and was never removed, so a retried `Add` of the same refused file
returned the stale id with a nil error instead of refusing again — a regression from this task's own
concurrency fix, against AGENT.md §6.11. Fixed with `untrackFailedSpec`, called on any `attach`
failure in `addSpec`; `TestAddRefusesARepeatedlyAddedUnsafeTorrentEveryTime` is the regression test.
Re-verified: `make check`, `make race` (`engine/anacrolix`, `config`, `doctor`, `platform`),
`make cover` (`anacrolix` 91.0%).

---

### T-034 · Download policy and path safety
```
status: done
depends: T-031, T-032
tier: H
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

**Notes:** Component/containment rules moved to `internal/engine/paths.go` (shared by every future
open/reveal/delete site), adding a 255-byte component limit and a per-OS whole-path limit
(`platform.MaxPathLength`: 4095 B Linux, 1023 B macOS, 259 UTF-16 units Windows). Eleven committed
fixtures in `anacrolix/testdata/malicious/` (pinned to their generator) are refused by `Add` and by
the storage gate. Free space via `platform.FreeSpace`, checked at add (`.torrent`) or metadata
arrival (magnet), re-checked every 10s; a shortfall pauses as `StateErrored` naming what is short.
Queue, seed policy, listen-port fallback and `doctor`'s port line: see DEC-106. Queue order is
exposed through the optional `engine.Queuer` (not the frozen `Engine`); Settings editing of every
knob is T-082's job (its list now names seed duration); `engine/fake` does not queue yet.

---

## Phase 4 — Persistence

### T-040 · bbolt store
```
status: done
depends: T-002
```
**Files:** `internal/store/`

**Notes:** `internal/store` has no dependency on `internal/engine` or `internal/indexer` — it
defines its own `TorrentRecord` (id, indexer id, source URL, added-at, save path), `HistoryEntry`
(text, timestamp), and `Prefs` (sort column, last screen, selected sources) rather than importing
either domain package's types, so it stays buildable in isolation the way T-031's block does not.
`Store` keeps the authoritative copy of all three buckets in memory behind a `sync.Mutex`; every
exported mutator (`SetTorrent`, `DeleteTorrent`, `AddHistory`, `SetPrefs`) only updates that copy
and sets a dirty flag — no bbolt write happens on the calling goroutine. A dedicated goroutine
started by `Open` ticks every 5s (`defaultFlushInterval`) and, only when dirty, snapshots the
maps under the lock, releases it, then rewrites the `torrents`/`history`/`prefs` buckets from the
snapshot inside one `bolt.Update` (delete-then-recreate each bucket rather than diffing keys, since
expected data volumes here are small — session state, not a search index). `Close` stops that
goroutine and performs one last synchronous flush so nothing queued is lost; `Flush` is also
exported for callers/tests that want to force persistence without waiting or closing. History is
keyed on disk by an 8-byte big-endian sequence number so insertion order survives a flush without
depending on wall-clock time; entries beyond `maxHistoryEntries` (50) are dropped oldest-first.
Schema version lives in a `meta` bucket key, written on first open; `ensureSchema` refuses to open
(`ErrUnsupportedSchemaVersion`) rather than proceeding when the on-disk version is newer than
`currentSchemaVersion`, and steps a `[]func(tx *bolt.Tx) error` migration hook forward one version
at a time for an older on-disk version — currently empty since this is the only schema version
that has ever shipped, per DEC-085. Added `go.etcd.io/bbolt v1.4.3` (MIT) to `go.mod`; verified
against `Makefile`'s `ALLOWED_LICENSES` both by reading its `LICENSE` file directly and by running
`make licenses` after installing `go-licenses`, which regenerated `NOTICE` with
`go.etcd.io/bbolt,MIT,...` and reported no disallowed license across all three `GOOS` targets.
Tests: `internal/store/store_test.go` covers CRUD on all three buckets, the empty-ID rejection,
the history cap/order, a `Prefs` round trip through a defensive copy (mutating a returned slice
must not corrupt the store), flush-then-reopen persistence, every mutator rejecting calls after
`Close` with `ErrClosed`, and `Open` refusing a database seeded with a future schema version.
`internal/store/concurrent_test.go`'s `TestConcurrentReadWrite` is the criterion's required
concurrent test: 8 goroutines × 200 iterations each call every mutator and reader while the
background flush goroutine runs on a 5ms interval (short enough to actually race the foreground
calls, unlike the 5s production default used everywhere else), then closes and reopens the store
to confirm the file is left in a consistent, readable state. `go test -race ./internal/store/...`
is green. `make check` is green.

**Acceptance**
- Buckets: `torrents` (id → origin, added-at, save path, source URL), `history` (recent queries),
  `prefs` (sort column, last screen, selected sources).
- Writes debounced 5s on a dedicated goroutine (AGENT.md §13).
- Schema version key with a migration hook; unknown future version refuses to open rather than
  corrupting.
- Concurrent read/write test with `-race`.

---

### T-042 · Single-instance lock and data integrity
```
status: done
depends: T-040
```
**Files:** `internal/lifecycle/` (`lock.go`, `integrity.go`, `shutdown.go`, `signal.go`, plus
`lock_test.go`, `integrity_test.go`, `shutdown_test.go`); `internal/platform/` (`lock_darwin.go`,
`lock_linux.go`, `lock_windows.go`, `process_darwin.go`, `process_linux.go`, `process_windows.go`,
plus a `_test.go` per file); `internal/config/load.go` (`writeDefault` renamed and exported as
`Save`) plus two new tests in `load_test.go`.

**Notes:** T-031 (the concrete `anacrolix` engine) is still `blocked`, so this task is written
and tested entirely against the frozen `engine.Engine` interface (AGENT.md §5) and
`internal/engine/fake` — no concrete engine exists to wire into a real `main`, and no
`internal/app` composition root exists yet either, so `cmd/tortui/main.go` is untouched; the next
task that builds the composition root is what calls `lifecycle.AcquireLock`,
`lifecycle.OpenStore`, and `lifecycle.Shutdown` from `main`, using `lifecycle.NotifySignals()`
for the `SIGINT`/`SIGTERM` half and a top-level `defer recover()` around it for the panic half —
`Shutdown`'s own doc comment says this explicitly since it could not be exercised end-to-end here.

The single-instance lock is a real OS-level advisory lock (`platform.TryLockFile`: `flock(2)` on
macOS/Linux, `LockFileEx` on Windows) rather than a hand-rolled PID-file check, specifically
because the OS releases it automatically when every descriptor/handle referencing it closes —
including when the holding process is killed without a chance to clean up — which is what makes
a stale lock self-correcting with no liveness-guessing logic in the success path. `PID` content
is still written into the lock file and read back only to *name* the holder in the
already-running error message, using the new `platform.ProcessAlive` (`kill(pid,0)` /
`OpenProcess`+`GetExitCodeProcess`) to decide whether that name is worth showing. See DEC-086.

The corrupt-store recovery in `lifecycle.OpenStore` deliberately does **not** quarantine a store
that `store.Open` reports as merely *locked* (`bolterrors.ErrTimeout`) — that would destroy a
live database out from under whoever holds it. In the intended startup order this branch should
be unreachable (`AcquireLock` already refuses a second instance before `OpenStore` is ever
called), but the check costs nothing and removes a way this package could itself become the
data-loss bug T-042 exists to prevent.

`config.Save` (previously the unexported `writeDefault`, used only for the first-run write) is
now the one atomic writer every future config save must go through, including the settings
screen's eventual persistence (T-080/T-082) — the acceptance criterion "config writes are atomic"
is general, not first-run-only, and the temp-file/fsync/rename implementation already existed and
needed no behaviour change, only a name and an exported test surface
(`TestSaveSurvivesCrashBeforeRename`).

`Shutdown`'s hung-engine test wraps `fake.New()` in a small `hungEngine` whose `Close` blocks
forever, per the task's instruction to test against "`internal/engine/fake` plus a deliberately
hung fake" rather than adding hang behaviour to the shared fake package itself.

Coverage: `internal/lifecycle` 77.3%, `internal/platform` 79.2%, `internal/config` 81.9% — all
above the `internal/engine`-adjacent 75% floor in AGENT.md §9 (lifecycle sits directly on
`engine.Engine` and `internal/store`, so it was held to the same bar even though §9 does not name
it explicitly). `go build`/`go vet` were cross-checked for `GOOS=linux` and `GOOS=windows` in
addition to the native `darwin` run, since the new `internal/platform` files and
`golang.org/x/sys/windows` usage only compile under their own `GOOS`; `go.mod` now lists
`golang.org/x/sys` as a direct requirement (it was already an indirect dependency, already listed
in `NOTICE` as BSD-3-Clause for both its `unix` and `windows` sub-packages) rather than adding
anything new. `make check` and `go test -race ./...` are both green.

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
status: done
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

**Notes:** `internal/tui/theme` is pure detection + styling — no bubbletea, no `indexer`/`engine`
import. `Palette` (`palette.go`) holds two built-in themes (`default`, `dusk`), each one accent
plus foreground/muted/dim/error/success as truecolor hex, looked up by `Names()`/`Lookup()`.
`Capability`/`Detect` (`capability.go`) resolve colour depth via `lipgloss.NewRenderer` +
`termenv.WithEnvironment` (so `NO_COLOR`/`CLICOLOR_FORCE`/`TERM`/`COLORTERM` are read through an
injectable `termenv.Environ`, never the live process env directly — makes every case unit-testable
without mutating global state), Unicode-vs-ASCII via `LC_ALL`/`LC_CTYPE`/`LANG` (defaulting to
Unicode when none is set), and `Interactive` via `TERM != "dumb"` plus an `os.ModeCharDevice`
check on the output stream. `Detect` and `RefusalMessage` only report/word the "no TUI" case;
main.go/root (T-051) own actually printing it and calling `os.Exit(1)`, since T-050's Files are
scoped to the theme package alone. `Theme`/`New` (`theme.go`) turn a `Palette` + `Capability` into
`lipgloss.Style`s via a renderer whose profile is set explicitly from the detected `ColorLevel`;
at `ColorNone` no style ever calls `.Foreground`, so NO_COLOR is a hard "never attach colour" path,
not merely a degraded one. `Width`/`Pad`/`Truncate` (`width.go`) wrap `rivo/uniseg` grapheme-aware
measurement. Golden tests live in `golden_test.go` against fixtures in `testdata/` (regenerate with
`UPDATE_GOLDEN=1 go test ./internal/tui/theme/...`, per AGENT.md §15 inspect any diff before
committing). Coverage 92.9%, well over the §9 threshold. `make check` green; cross-`GOOS`
`golangci-lint run ./...` clean for windows/linux/darwin (a native `make check` alone would not
have caught a platform-specific lint failure — there is no OS-specific code in this package, but
the check was run anyway per the task brief). New dependencies `charmbracelet/lipgloss` (already
locked by AGENT.md §3), `rivo/uniseg` (already locked), and `muesli/termenv` (lipgloss's own
terminal-capability backend, used here directly for `Environ`/`WithTTY` injection) — see DEC-088.

---

### T-051 · Root model and routing
```
status: done
depends: T-050, T-012, T-030
```
**Notes:** `Model` (`root.go`) is a bubbletea program importing only `engine.Engine` (an
interface — AGENT.md §4) so it runs end-to-end against `internal/engine/fake`; it never imports
`indexer` directly because nothing in this task's scope needs a search result yet (the five
screens are placeholders, per the task brief). `keymap.go` holds `GlobalBindings`/
`helpOverlayBindings`/`quitConfirmBindings` as the single source of truth: `KeyMap.Lookup` (used
by `Model.handleKey`) and `KeyMap.HelpFor` (used by the `?` overlay) are both built from the same
`[]Binding` data, so the overlay cannot drift from what a key actually does. `Conflicts` walks
that data structurally — grouping by `Context` (one per screen, plus `ContextHelp` and
`ContextQuitConfirm`) and flagging any key bound to two different actions in the same context —
and `TestKeymapNoConflicts` asserts zero conflicts across all seven contexts;
`TestConflictsDetectsRealCollision`/`TestConflictsCoversModalContexts` prove the detector itself
actually flags a real collision rather than vacuously passing. Screen routing (`tab`/`shift+tab`
via `Screen.next`/`prev`, `1`-`5` via dedicated `goto-*` actions) and the help overlay are
covered end-to-end with `teatest` (`TestNavigationAllScreensViaNumberKeys`,
`TestNavigationTabCyclesForwardAndBack`, `TestHelpOverlayTogglesAndShowsScreenBindings`,
`TestHelpOverlayClosesWithEscape`). Quit confirmation
(`TestQuitWithNoActiveDownloadsIsImmediate`, `TestQuitWithActiveDownloadPromptsThenConfirms`)
subscribes to `engine.Updates()` via the standard bubbletea single-receive-then-recurse `tea.Cmd`
pattern (`waitForEngineUpdate`) so `Update` never blocks (AGENT.md §6.1); `countActive` treats
queued/checking/downloading/seeding as active and paused/errored as not, which is what gates the
prompt. `tea.WindowSizeMsg` only ever updates `m.width`/`m.height`; every render (`View` and its
helpers) reads those fields fresh, so no width is ever cached across a resize —
`TestWindowResizeRecomputesLayout` checks two successive resizes land correctly. `View` returns
`""` before the first `WindowSizeMsg` and does no I/O (AGENT.md §6.8). Coverage on
`internal/tui` is 96.6%, well over the §9 50% floor. Actions belonging to screens/components this
task does not build (`/`, `L`, `R`, move, select, details, sort, open-file/folder/source,
pause/resume, remove) are defined in the keymap for help-text and conflict-checking purposes but
are deliberate no-ops in `Model.handleKey`'s `default` case — T-052 (status bar), T-053 (table),
T-054 (modals), T-060/T-061/T-063/T-071/T-080 (screen content) give them behaviour without
touching the binding itself. `make check` green; `go test -race ./...` clean; cross-`GOOS`
`golangci-lint run ./...` clean for windows/linux/darwin.

New dependencies: `github.com/charmbracelet/bubbletea v1.3.10` and
`github.com/charmbracelet/x/exp/teatest` (test-only) — both already locked by AGENT.md §3, now
added as real `go.mod` entries for the first time. `bubbles` was fetched during dependency
resolution but is unused by this task (no screen has an input widget yet) and `go mod tidy`
correctly dropped it; a later screen task adds it back when it actually imports something from
it. See DEC-090 for the license check, and DEC-091 for one transitive-dependency wrinkle
(`github.com/mattn/go-localereader`) that check surfaced.
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
status: done
depends: T-051
```
**Notes:** `components.StatusBar` (`internal/tui/components/statusbar.go`) is the whole component:
plain state (`ActiveDownloads`, `DownRate`/`UpRate`, `SourcesTotal`/`FailedSources`, a transient
message queue) plus a pure `View(width, screen, theme.Theme) string` — no I/O, no engine or
indexer import, testable with no bubbletea program at all (AGENT.md §4, §6.8). The transient-
message timeout is `tea.Tick`/`tea.Cmd` end to end: `Push` starts a `tea.Cmd` only when its text
becomes the message actually shown (queuing behind an existing one returns a nil `Cmd`, since
exactly one timeout is ever in flight), and `Update(TickMsg)` pops the queue and re-arms — nothing
here sleeps or receives on a channel inside `Update` (AGENT.md §6.1).
`root.go` wires it in: `engineUpdateMsg` now also updates `statusBar.ActiveDownloads` and the
summed `DownRate`/`UpRate` (`aggregateRates`) alongside the existing quit-confirm bookkeeping;
`transientMessageMsg` and `components.TickMsg` route straight to `StatusBar.Push`/`Update`; a new
`sourceStatusMsg{total, failed}` feeds `SourcesTotal`/`FailedSources` for the error indicator. No
screen sends `sourceStatusMsg`/`transientMessageMsg` yet — T-060/T-061 are what will actually run
`indexer.Registry.SearchAll` and translate its `[]SourceError` — so T-052's own tests construct
and send these messages directly, the same pattern T-051 used for `engineUpdateMsg` before any
screen produced one. `View()` now always appends `renderStatusBar()` under whichever context's
body it drew.

**DEC-092 (keymap conflict):** the acceptance text's literal "`tab` to expand" would bind `tab` to
a second action inside every screen context, directly colliding with T-051's frozen
`ActionNextScreen` binding and failing `TestKeymapNoConflicts`. Resolved with a new modal
`ContextErrorDetail`: `e` (free in `GlobalBindings`) opens the panel when `FailedSources` is
non-empty, and `tab`/`esc` — bound only inside `ContextErrorDetail`, never in any `screenContext`
— collapse/close it (`ActionToggleErrorDetail`, handled by `Model.handleToggleErrorDetail`). The
indicator's own label reads `"N/M sources failed (e to view)"` to match the real key.
`TestKeymapNoConflicts` is unmodified and still passes; `TestSourceErrorIndicatorAppearsAndExpands`
drives `e` then `tab` through the real `Model` and confirms `tab` inside the panel neither cycles
the screen nor collides with anything.

Tests: `internal/tui/components/statusbar_test.go` covers `Push`/`Update` queuing (first message
shown immediately with a non-nil `Cmd`; a second `Push` while one is showing is queued, not
overwritten, with a nil `Cmd`; a `TickMsg` promotes the next queued message or clears the display),
the `DefaultTransientTimeout == 4*time.Second` constant plus the `Timeout` override tests use to
avoid a real 4s wait, the source-error indicator's exact text and its absence before any search
(`SourcesTotal == 0`), and 80-column truncation (an overloaded line ends in `...` and never exceeds
`theme.Width` 80; a short line is left alone). `internal/tui/statusbar_wiring_test.go` covers the
root-level wiring: screen name and active-download count in the footer, rate aggregation across
multiple `TorrentStatus`, the indicator appearing/expanding/force-closing on a follow-up
`sourceStatusMsg` reporting zero failures, and the transient-message push/queue/timeout round-trip
through real `Model.Update` calls (one test executes the actual returned `tea.Cmd` to prove the
timeout is a real `components.TickMsg`, not a sleep). Coverage: `internal/tui` 97.3%,
`internal/tui/components` 95.5%, both well over the §9 50% floor. `make check` green; `go test
-race ./...` clean; cross-`GOOS` `golangci-lint run ./...` clean for windows/linux/darwin; all six
`GOOS`/`GOARCH` tier-1 combinations (`go build`) compile clean. No new dependency — `bubbletea`,
`lipgloss`, and `uniseg` were already real `go.mod` entries from T-050/T-051.
**Files:** `internal/tui/components/statusbar.go`, `internal/tui/components/statusbar_test.go`,
`internal/tui/root.go`, `internal/tui/keymap.go`, `internal/tui/statusbar_wiring_test.go`

**Acceptance**
- Single line: current screen, active download count, aggregate down/up rate, source-error
  indicator (`2/4 sources failed`) expandable with `tab`.
- Transient messages with a 4s timeout, queued rather than overwritten.
- Truncates gracefully at 80 columns.

---

### T-053 · Responsive table component
```
status: done
depends: T-051
```
**Notes:** `components.Table` (`internal/tui/components/table.go`) is domain-agnostic per
AGENT.md §4/T-053's own steer — it knows rows of `Cells []string` identified by a stable `ID`,
and a caller-supplied `[]Column` (`Key`, `Title`, `Width`/`Flex`+`MinWidth`, `Priority`, `Align`,
optional `Less`). It has no `indexer`/`engine` import and no knowledge of "Source"/"Age"/"Trust"
as concepts — those are just column names a caller (T-061) will configure. `layout()` recomputes
visible columns and widths from scratch on every `View()` call (never cached — AGENT.md §13's
named hazard), dropping `Priority > 0` columns lowest-priority-first once the fixed+flex-floor
minimum no longer fits; `Priority == 0` columns are never dropped. `internal/tui/components/table_test.go`'s
`resultColumns()` configures the exact AGENT.md §7 set (Title flex, Size/S-L fixed and
undroppable, Source/Age/Trust with priorities 1/2/3) and `TestTableColumnDropOrderAtNarrowWidths`
pins the drop sequence at 80/60/50/40 columns. Selection is tracked by row `ID`, never index —
`TestTableSelectionSurvivesResort` sorts and re-sorts a 3-row table and asserts the selected
row's identity is unchanged (its index does change), per the acceptance text's own wording.
Sorting uses a stable sort with an optional per-column `Less` (a nil `Less` string-compares the
display cell, which is wrong for anything meant to sort numerically — `TestTableSortOrdersRowsByColumn`
exercises a caller-supplied numeric `Less`); `SortBy` on a new column starts ascending, on the
already-sorted column toggles, and the header shows `^`/`v` (plain ASCII, no glyph-set
dependency needed for a sort caret). The viewport is also recomputed per render, not stored —
`visibleWindow()` centres the selected row in the available height from `SelectedIndex` and row
count alone, so a resize can't leave a stale scroll offset. Golden files at 80×24, 120×40, and
60×20 (`testdata/table_80x24.golden`, `table_120x40.golden`, `table_60x20.golden`) all use the
same 4-row fixture including a title mixing CJK text and an emoji (示例种子🎬.iso); the 60×20
golden asserts in-test that Source is absent from the header, and `TestTableCJKEmojiAlignment`
independently proves every rendered row measures the same `theme.Width` as the header regardless
of grapheme content. No new dependency — `theme`/`lipgloss`/`uniseg` were already in `go.mod`
from T-050/T-051, so no DEC entry or `NOTICE`/`make licenses` change was needed. `make check`
green; `go test -race ./...` clean; `internal/tui/components` coverage 93.7%,
`internal/tui` package overall 97.3%, well above the §9 50% floor; cross-`GOOS`
`golangci-lint run ./...` clean for darwin/linux/windows.
**Files:** `internal/tui/components/table.go`, `internal/tui/components/table_test.go`,
`internal/tui/components/testdata/table_80x24.golden`,
`internal/tui/components/testdata/table_120x40.golden`,
`internal/tui/components/testdata/table_60x20.golden`

**Acceptance**
- Column set with flex/fixed widths and a documented drop order
  (Source → Age → Trust) as width shrinks.
- Sort by any column, ascending/descending, indicator in the header.
- Keyboard scrolling with viewport, selection preserved across re-sorts.
- Renders correctly at 80×24, 120×40, and 60×20; golden files for each.

---

### T-054 · Modals and confirm dialog
```
status: done
depends: T-051
```
**Files:** `internal/tui/components/dialog.go`, `internal/tui/components/dialog_test.go`,
`internal/tui/theme/theme.go` (new `Theme.Border`), `internal/tui/root.go`, `internal/tui/root_test.go`

**Notes:** `components.Dialog` is the one generic modal mechanism: a `Title`, a `Message`, a
`[]DialogOption` of plain labels (no torrent/indexer type anywhere near it — AGENT.md §4), and a
`Default` index. It is plain state plus a pure `View` (same shape as `StatusBar`/`Table`), so it
is unit-tested with no bubbletea program. `View` draws through a new `Theme.Border` — a
`lipgloss.Style` carrying the palette's single accent colour and degrading to `ASCIIBorder()`
under a non-Unicode capability the same way `GlyphSet` does — which is the first real use of
AGENT.md §7's "border around the focused pane and modals" allowance; nothing before this task
needed one. `Open()` is a no-op when the dialog is already open — a caller bug, not something
reachable through tortui's own context-scoped keymap — and reports it via `slog.Warn` through an
optional `Logger` field defaulting to `slog.Default()` (the same fallback `internal/store` and
`internal/indexer/httpx` use), never to stdout/stderr. `Cancel` and `Confirm` both restore the
highlighted option to `Default`, so a dialog reopened later never resumes a stale cursor.

T-051's `ContextQuitConfirm` prompt is refactored onto this component (root's `quitConfirm` field
is now a `components.Dialog`, not a bare bool) per this task's own note to prefer that over a
parallel one-off where it genuinely fits; `ContextHelp` and `ContextErrorDetail` are left alone
since neither is a confirm dialog (one is a key listing, the other an info panel), and touching
either risked the already-passing `TestKeymapNoConflicts`/DEC-092 wiring for no benefit. Root
gained one new piece of generic state, `Model.selection` — a screen-agnostic integer cursor moved
by the existing (previously no-op) `ActionMoveUp`/`ActionMoveDown` bindings — solely so "esc
restores the originating screen and selection" is a real, testable claim now rather than
deferred; no screen content, list, or data model was added, and a later screen task is free to
replace it with its own bounded, data-backed cursor. `TestQuitConfirmEscRestoresScreenAndSelection`
drives the real `Model` through tab + j/j + q + esc and asserts both the screen and the selection
value survive the round trip, not just that some screen is showing.

Verified: `make check` green; `go test -race ./...` clean; `golangci-lint run ./...` clean under
`GOOS=darwin`, `GOOS=linux`, and `GOOS=windows` (with `GOARCH=amd64`); cross-compiled builds for
all six AGENT.md §14 tier-1 OS/arch pairs succeed; `internal/tui/...` coverage 93-97%, well above
the 50% floor. See DEC-093 for the `Theme.Border` addition.

**Acceptance**
- [x] Generic confirm dialog with configurable options and a default.
- [x] Never more than one modal deep — opening a second is a programming error that returns a
  logged no-op, not a stack.
- [x] `esc` always cancels; focus returns to the originating screen and selection.

---

### T-055 · Terminal capability detection and `doctor`
```
status: done
depends: T-050, T-002
```
**Files:** `internal/tui/theme/capability.go` (extended, not `internal/tui/capability.go` — see
DEC-094), `internal/platform/fdlimit_{darwin,linux,windows}.go`,
`internal/platform/termsize_{darwin,linux,windows}.go`, `internal/doctor/doctor.go`,
`cmd/tortui/doctor.go`, `cmd/tortui/main.go` (dispatch), `internal/logging/mask.go` (`Redact`
export)

**Acceptance**
- [x] Detects colour profile, Unicode/glyph support, terminal size, tmux/screen wrapping, and
  whether stdout is a TTY. Colour/Unicode/TTY detection is T-050's existing
  `internal/tui/theme.Detect`; size (`Width`/`Height`, via new `internal/platform.TerminalSize`,
  one file per OS build tag) and `Multiplexer` (tmux/screen, from `$TMUX`/`$STY`/`TERM`) are the
  new pieces this task adds to that same detector rather than a second one (DEC-094).
- [x] `tortui doctor` prints OS/arch, `TERM`, `COLORTERM`, detected profile, Unicode verdict,
  terminal size, resolved config/state/download paths, file-descriptor soft and hard limits,
  and every configured indexer with a reachability verdict. Exits without starting the TUI —
  `runDoctor` (`cmd/tortui/doctor.go`) never imports `internal/tui`.
- [x] Output is plain text, safe to pipe and paste into a bug report (`internal/doctor.Format`
  emits no ANSI escapes, no lipgloss rendering, regardless of detected capability — verified by
  `TestFormatIsPlainText`/`TestRunDoctorOutputHasNoANSIEscapes`). Credentials are masked: no
  `IndexerVerdict` field ever carries a URL, and every string built from a network error is
  passed through the new `logging.Redact` (an exported wrapper around the existing masking
  regexes) before being stored — verified end to end with real user-shaped api_key/cookie
  values sent to an `httptest.Server` and to an unroutable host
  (`TestBuildNeverLeaksCredentials`, `TestBuildNeverLeaksCredentialsOnNetworkFailure`,
  `TestRunDoctorMasksIndexerCredentials`).
- [x] Non-zero exit when a hard problem is found (unwritable download dir, `TERM=dumb`) —
  `doctor.HasHardProblem`; both cases have a dedicated `cmd/tortui` integration test. A
  non-2xx or unreachable indexer is informational (exit 0), not a hard problem — only the two
  named conditions are.
- [x] On darwin, `platform.RaiseFDLimit` makes a real, tested attempt at startup to raise the
  soft FD limit to the hard limit, and `doctor` reports both the soft limit as seen at startup
  and the value after that attempt (AGENT.md §13). **Correction (DEC-096):** in every real
  invocation, Go's own runtime already raises the soft limit to the hard ceiling in an `init()`
  that runs before `main()` (Go 1.19+, `src/syscall/rlimit.go`), so `RaiseFDLimit`'s own
  `Setrlimit` call is normally a no-op by the time it runs and the "seen at startup" value it
  reports already reflects the Go runtime's raise, not one tortui performed. This was not
  observable or testable at the time this task first shipped — `getrlimitFunc`/`setrlimitFunc`/
  `sysctlUint32Func` seams and `TestRaiseFDLimitRaisesWhenSoftIsBelowHardCeiling` were added
  during QA remediation to exercise the raise branch against a mocked starting limit, and
  `doctor.Format`'s wording was corrected to stop implying tortui performed a raise it normally
  did not. Linux gets the identical treatment and the identical correction (low container
  defaults are the same hazard). Windows has no per-process descriptor limit to raise;
  `platform.FDLimits{Supported: false}` and a plain "not applicable" line in `doctor`'s output
  is the coherent Windows path (AGENT.md §14: a feature working on two of three OSes is not
  done), not a stub.

**Notes.** Reachability probing reuses `internal/indexer/httpx.Client` (single attempt, the
indexer's own credentials, a bounded per-call deadline) instead of building real torznab/scraper
adapter instances from config — that wiring belongs to T-080/T-081 and `internal/app`, neither of
which exists yet, and this task was explicitly scoped to not build ahead into either (see
DEC-095). No new dependency: `golang.org/x/sys/unix` (darwin/linux rlimit + sysctl) and
`golang.org/x/sys/windows` (console buffer info) are both already-vendored packages of the
existing `golang.org/x/sys` module (T-051) — `go.mod`/`go.sum`/`NOTICE` are byte-identical before
and after this task. Verified: `make check` green; `go test -race ./...` green;
`golangci-lint run` and `go vet ./...` clean under `GOOS=darwin/linux/windows`; all six tier-1
`GOOS`/`GOARCH` pairs cross-compile via `make build-all`; manual `./bin/tortui doctor` run
(including `TERM=dumb`, a piped non-TTY run, and a real config.toml with a fake api_key/cookie
pointed at an unroutable host) matches every acceptance line above. QA later found the FD raise
was never actually exercised or honestly described (see DEC-096); the injectable-syscall seam,
its test, and `doctor`'s corrected wording were added in remediation on the same branch/PR.

---

### T-056 · Demo mode
```
status: done
depends: T-051, T-030
```
**Files:** `internal/app/demo.go`, `internal/indexer/fake/{fake,registry}.go` (+ tests),
`internal/tui/root.go` (new `Model.Banner` field, root-owned, no screen touched — see notes),
`cmd/tortui/{main,demo}.go` (`--demo` flag, wiring only)

**Acceptance**
- [x] `tortui --demo` wires `engine/fake` plus a fixture-backed fake indexer into the real TUI.
  `internal/app.NewDemo` builds both and hands the engine straight to `tui.New`; see the notes
  below for the one precise place the indexer half is not yet visually wired, and why.
- [x] Canned search results span every `Trust` value, wide CJK and emoji titles, huge and tiny
  sizes, zero-seeder entries, and a source that deliberately fails. `internal/indexer/fake`'s
  `ArchiveResults`/`MirrorResults` cover all five `Trust` values, a CJK title, an emoji title, a
  ~2.5 TB entry, a 512-byte entry, and a zero-seeder entry; `NewDemoRegistry` registers a third
  source (`demo-offline`) whose `Search` always fails, proving the registry's "N/M sources
  failed" degrade path (AGENT.md §6.3) end to end with zero network.
- [x] Simulated downloads exercise: normal progress to completion, a stall, a metadata timeout,
  and an error state — on a controllable clock so the whole cycle runs in under a minute.
  `demoTorrentSpecs` seeds five torrents via `engine/fake`'s existing `Downloading`/`Stalled`/
  `Errored`/`Completed` scripts (T-030 — no second clock abstraction added); every script
  resolves within 25 simulated seconds. `internal/app`'s tests drive this via `Engine().Advance`
  directly, not a sleep.
- [x] Zero network. Zero writes outside a temp dir. Nothing to clean up afterwards.
  `TestZeroNetworkCalls` overrides `http.DefaultTransport` to fail the test on any dial and
  drives `NewDemo` + a `SearchAll` + a full `Advance` cycle through it. `TestNothingIsWritten
  OutsideTheSandbox` snapshots `os.TempDir()` before/after `NewDemo` and asserts the only new
  entry is the sandbox itself. `TestCloseRemovesTheSandboxAndLeavesNothingBehind` and a real,
  manually-driven `--demo` run (pty-driven, see below) both confirm the sandbox directory is
  gone after a normal quit, `ctrl+c` (SIGINT), and SIGTERM — `Demo.Close` is wired via `defer` in
  `Run` and is idempotent.
- [x] Every screen, keybind, and dialog is reachable in demo mode, **except** open-file/
  open-folder, which is not reachable by any means today — not a demo-mode gap. See notes.
- [x] A banner makes it unmistakable that this is demo data. `internal/tui.Model` gained one new
  exported field, `Banner string` (empty in production, set to `DemoBanner` only by
  `internal/app.NewDemo`), rendered as a fixed accent-coloured line above every screen and every
  modal in `View()`. This is a small, generic, root-owned addition — not screen content — and is
  covered by `internal/tui`'s own `banner_test.go`.

**Notes — the two places this task's acceptance criteria outrun what's built, and what unblocks
each:**

1. **The indexer/registry half is fully built and independently proven, but not visually wired
   into the running `--demo` TUI.** `internal/tui` may import `indexer`/`engine`
   *interfaces only*, never a concrete type (AGENT.md §4) — `indexer.Registry` is concrete, and
   no interface abstraction over "what a screen needs from a search source" exists yet, because
   the screen that would define it (T-060) doesn't exist. Building one now, just to satisfy this
   criterion's letter, would be inventing T-060's own design surface ahead of that task — exactly
   the "do not build ahead" instruction this task was given. What *is* done: `Demo.Registry()`
   exposes the fully-populated, three-source `*indexer.Registry`; `NewDemo` runs one real
   `SearchAll` against it as a startup self-check (proving the fixtures and the degrade path work
   end to end, zero network); and `internal/indexer/fake`'s own tests plus `internal/app`'s
   app-level companion tests (`TestRegistrySpansEveryTrustValueAndDegrades`,
   `TestNewDemoRegistrySearchAllDegradesOnFailingSource`) exercise it directly. T-060 wires
   `Demo.Registry()` into a real search screen with zero change to this task's fixtures.
2. **`o` (open file) and `f` (open folder) are bound in the keymap on the downloads screen but
   are handled as an explicit no-op by `internal/tui` root's `Update` today** — see
   `handleKey`'s `default` branch comment, unchanged since T-051: those two actions belong to
   T-073, which does not exist yet. This is true independent of `--demo`; wiring "demo-side
   support" cannot make a keybind that root's own `Update` ignores do anything, and building that
   wiring here would be implementing T-073 itself. What *is* done, so T-073 needs zero change to
   demo mode when it lands: every seeded torrent's `SavePath` sits inside `Demo.SandboxDir()`'s
   `downloads/` subdirectory, and the "already completed" torrent (`demo-completed`) has a real,
   readable placeholder file already sitting at that exact path (`writeDemoPlaceholderFile`) —
   the moment T-073 wires `o`/`f` to `internal/platform`'s open/reveal calls, there is a genuine
   file for them to open and a genuine folder for them to reveal.

**Manual verification (pty-driven, no real interactive terminal was available in the harness that
implemented this):** `make build && ./bin/tortui doctor` (sane report); `./bin/tortui --demo`
under `printf ''` and a real pipe both refuse cleanly with `theme.RefusalMessage()` and exit 1;
a Python-`pty`-driven run (real TTY, 80×24) showed the `DEMO MODE` banner, live tab switching
(1-5), the `?` help overlay listing every binding, the status bar's active-download count
counting down live (5→4→3, proving `engine/fake` is genuinely wired), and the quit-confirmation
dialog (bordered, `N active download(s) will stop`, `Cancel`/`Quit`) opening when downloads are
active. Quitting via `q`+`y`, `ctrl+c` (SIGINT), and `SIGTERM` were each confirmed to exit cleanly
(terminal-restore escape sequences present, process gone, sandbox directory gone) — `SIGKILL`,
which no process can intercept, was confirmed (as expected) to leave the sandbox behind, which is
an OS-level limitation common to every program, not specific to tortui.

**Why this exists:** it is the primary way to verify rendering after a build, and the only way
the agent can self-check the UI without a live swarm (AGENT.md §15).

**QA remediation (2026-09-17, same branch, PR #27):** QA failed the PR on an AGENT.md §2/§16
violation — `internal/indexer/fake/fake_test.go`'s `TestFixturesNameNoRealMediaOrInfringingSite`
had put five real infringement-oriented site names into a `forbidden := []string{...}` literal.
The test's intent (guard against a fixture naming a real site) was sound; naming the sites to
build the guard was the bug. Fixed by replacing it with two positive, allowlist-based tests that
need no forbidden-site list at all: `TestFixtureSourceURLsUseOnlyReservedDomains` (every fixture
`SourceURL` host must be an RFC 2606/6761 reserved domain — `example.{com,net,org}`, `.example`,
`.test`, `.invalid`, or `localhost` — the same approach `scripts/check-indexer-hostnames.sh`
already uses) and `TestFixtureTitlesCarryNoEmbeddedURL` (no `://` or `www.` in a title). No other
occurrence of those names was found anywhere else in the working tree, in this branch's commit
messages, or in the PR title/body. They do appear verbatim in the QA review comment on PR #27
(quoting the violation) and in this branch's already-pushed commit `2a4c534`'s diff content; a
squash-merge keeps both out of `main`'s tree and history, but a human who wants them off the
remote entirely would need to delete/edit that PR comment and separately purge or rewrite the
branch (out of scope for this remediation — AGENT.md §10 bars force-pushing a branch with review
comments on it, so this was reported, not actioned). Backlog candidate: add a
`check-indexer-hostnames.sh`-style CI grep for a small set of well-known infringement-site name
fragments across the whole diff (not just indexer-context lines), so this class of mistake is
caught by CI rather than a reviewer's eye — proposed, not implemented here.

---

## Blocked — Resolved


- `T-031` (2026-09-17 → 2026-09-18). **The MPL-2.0 exception authorised by DEC-098 is narrower than
  `anacrolix/torrent`'s own dependency tree, and that tree also contains a module with no
  detectable license at all.** T-031's first action is `go get github.com/anacrolix/torrent`
  (resolves to `v1.61.0`). With the dependency added and a single-import probe package under
  `internal/engine/anacrolix/` so `go-licenses`' import walk actually reaches it, `make licenses`
  fails, for two separate reasons:

  1. **Seven further MPL-2.0 modules, none of them `anacrolix/torrent`.** They are the engine's own
     sibling libraries, reached as compile-time imports of `github.com/anacrolix/torrent` itself, so
     they are not optional and cannot be dropped by importing a narrower package:
     `github.com/anacrolix/dht/v2`, `github.com/anacrolix/generics`, `github.com/anacrolix/log`,
     `github.com/anacrolix/multiless`, `github.com/anacrolix/sync`, `github.com/anacrolix/upnp`,
     `github.com/anacrolix/utp`. `scripts/check-license-scope.sh` fails on all three `LICENSE_OSES`
     and names all seven — which is exactly what it was built (DEC-099) to do. This is the case the
     task brief calls out explicitly: a further MPL-2.0 module is an owner decision, not a script
     edit, so the script was not touched and `ALLOWED_LICENSES` was not widened.

  2. **`github.com/go-llsqlite/adapter` (and `github.com/go-llsqlite/adapter/sqlitex`) ship no
     license file**, so `go-licenses` cannot classify them at all:
     `Did not find license for library 'github.com/go-llsqlite/adapter'.` — `make licenses` exits 1
     on `GOOS=darwin` before it ever reaches the scope check. This one is not a scope question: an
     unlicensed dependency is admitted by no allowlist, present or widened, and `NOTICE` cannot
     attribute it. `go mod why` puts it on an unavoidable path:
     `internal/engine/anacrolix` → `github.com/anacrolix/torrent` → `github.com/anacrolix/torrent/storage`
     → `github.com/go-llsqlite/adapter`. The module directory contains `go.mod`, `go.sum`, and four
     `.go` files, and no `LICENSE`/`LICENCE`/`COPYING` of any name — confirmed by listing the module
     cache directory, not inferred from the tool's message.

  **Not a blocker, recorded for completeness.** No GPL or AGPL module appears anywhere in the tree
  on any of the three `LICENSE_OSES` — the failure mode the task brief warned about most loudly did
  not occur. License census per `GOOS` (`go-licenses report ./...`): darwin 59 MIT / 19 BSD-3-Clause
  / 9 Apache-2.0 / 8 MPL-2.0 / 3 BSD-2-Clause / 2 ISC / 2 Unknown; linux and windows the same minus
  the two Unknown rows (that package is darwin-reachable only in this graph). `govulncheck ./...` on
  the new tree: `No vulnerabilities found.` for called symbols — 0 vulnerabilities our code reaches;
  4 vulnerabilities in imported packages and 3 in required modules, none called
  (`GO-2026-6278` gorilla/websocket, `GO-2026-6165` pion/dtls, `GO-2026-6163` pion/stun,
  `GO-2026-5506` otel; `GO-2026-6355`/`GO-2026-6354`/`GO-2026-5932` x/crypto). Those are worth a
  look whenever the engine does land, but none of them is what blocks this task.

  **What would unblock it — a project-owner decision, recorded as a DEC- entry.** Both parts need
  answering; resolving only one leaves `make licenses` red.

  For (1), either:
  - **1a.** Extend the DEC-098 exception from one module to the `github.com/anacrolix/*` family
    that `anacrolix/torrent` compiles against — the same MPL-2.0 reasoning in AGENT.md §16 applies
    unchanged to every one of them (same author, same license, same file-level copyleft, all
    unmodified) — and widen `ALLOWED_MPL_MODULE` in the `Makefile` plus
    `scripts/check-license-scope.sh` to accept that set rather than one literal string. This keeps
    the gate an enforced allowlist; it admits seven named modules, still not a license family.
  - **1b.** Replace the locked engine, which overrides DEC-001 and a locked AGENT.md §3 stack row.

  For (2), either:
  - **2a.** Determine `go-llsqlite/adapter`'s actual license from upstream and, if it is admissible,
    record it as an explicit exception with a `--ignore` entry or an equivalent so `NOTICE` states
    the finding rather than silently omitting the module. An unlicensed dependency shipped in a
    distributed binary is a real exposure, not a paperwork problem, so this needs a deliberate
    answer either way.
  - **2b.** Establish whether `anacrolix/torrent` can be built without its `storage` package's
    sqlite backend (a build tag or a fork-free import path that omits it). Nothing in the module's
    public API obviously offers that, and the root `torrent` package imports `torrent/storage`
    unconditionally, so this looks unlikely — but it is the only route that removes the module
    instead of admitting it.

  No script, `Makefile` variable, or AGENT.md gate was edited, and `go.mod`/`go.sum`/`NOTICE` are
  unchanged on `task/T-031-anacrolix-engine`. The probe package used to produce the evidence above
  was deleted after the runs; the branch carries this finding and nothing else.

  **Owner decision (2026-09-18): option 1b — replace the locked engine.** The project owner chose
  to replace `github.com/anacrolix/torrent` rather than widen the MPL-2.0 exception to its
  `anacrolix/*` siblings plus `go-llsqlite/adapter`. This overrides DEC-001 and the Torrent-engine
  row of AGENT.md §3 ("locked — do not re-litigate"), which only a project-owner decision can do,
  and it will be recorded as a `DEC-` entry naming the replacement once one is selected. **T-031
  stays `blocked` until then** — the replacement engine is itself a material choice (§12: two
  reasonable implementations would differ materially), so it is not an agent's pick to make
  silently. Candidate evaluation is in flight against the hard gates: pure Go with no cgo (six
  cross-compiled tier-1 targets, §14), an embedded library rather than a daemon (§2 standalone
  contract), MIT/Apache-2.0/BSD/ISC across the *whole transitive tree* — the failure mode that
  blocked this task in the first place — maintained inside 24 months and CVE-clean (§12), and able
  to satisfy the frozen §5 `Engine` interface without changing it. Once selected, this also
  reopens whether T-942's MPL-2.0 admission (DEC-098/DEC-099) should stand or be reverted, since
  its sole named beneficiary would no longer be a dependency.

  **Owner decision (2026-09-18, final): option 1a — extend the MPL-2.0 exception.** After the 1b
  evaluation returned no viable replacement, the owner chose to widen DEC-098's exception to the
  named module set rather than accept rain's LGPL-3.0 + unlicensed-module tree and redesign
  per-torrent destinations. Filed as **T-943**, which carries the policy change, the widened
  scope gate and its regression test, and the `go-llsqlite/adapter` resolution. **T-031 stays
  `blocked` until T-943 merges**, then returns to `todo` — it is unchanged in scope and still owns
  adding the dependency and writing the engine.

  **Option 1b was pursued and did not survive contact with the evidence (2026-09-18).** Candidates
  were evaluated against five hard gates: pure Go/no cgo (six cross-compiled tier-1 targets),
  embedded library rather than a daemon (§2 standalone contract, and the README's "No daemon, no
  indexer proxy, no Transmission or qBittorrent behind it" promise), MIT/Apache-2.0/BSD/ISC across
  the *whole transitive tree*, maintained inside 24 months and CVE-clean (§12), and able to
  satisfy the frozen §5 `Engine` interface. **No candidate cleared all five.**

  `github.com/cenkalti/rain` v2.4.0 (MIT, the owner's preferred pick) **passes gates 1, 2 and 4**
  — notably gate 2, proved rather than assumed: `torrent.NewSession` runs fully in-process with
  `RPCEnabled=false` (the default is `true` on `127.0.0.1:7246` and must be explicitly disabled),
  no child process, no control socket, only the BitTorrent peer port. It **fails gates 3 and 5**:

  - *Gate 3.* Its tree carries `github.com/juju/ratelimit` (**LGPL-3.0**, with a static-linking
    exception), `hashicorp/errwrap` and `hashicorp/go-multierror` (MPL-2.0), and
    `github.com/nictuku/nettools` (**no license at all, upstream, and unmaintained since 2015** —
    unlike `go-llsqlite/adapter`, there is no newer revision to move to). All are unavoidable
    compile-time imports; `DHTEnabled=false` does not remove `nettools` from the graph. Verified
    independently by the orchestrator from the module cache and the GitHub API.
  - *Gate 5.* rain has **no per-torrent save-path API**. `AddTorrentOptions` is
    `{ID, Stopped, StopAfterDownload, StopAfterMetadata, Sequential}` — confirmed by reading
    `torrent/session_add.go:24` directly. Destination comes only from session-wide `Config.DataDir`.
    `Config.CustomStorage` is not an escape hatch: its type lives in rain's `internal/` tree and an
    external module cannot implement it. `Torrent.Move` relocates to another rain **daemon**, which
    gate 2 rules out. The frozen §5 interface still *compiles* — `AddSource.SavePath` is untouched
    — but rain cannot honour its semantics, which breaks T-034's multi-root containment model
    (§6.12) as written.

  So replacing the engine with rain would trade eight MPL-2.0 modules plus one pinned-revision
  licensing gap for one **LGPL-3.0** module (a step *further* along the copyleft scale than
  anything §16 admits), two MPL-2.0 modules, one permanently unlicensed module, **and** a redesign
  of per-torrent destinations. It does not avoid a license exception; it enlarges one and adds a
  contract failure. The other candidates fail earlier: forks of `anacrolix/torrent` inherit its MPL
  tree, `xgfone/go-bt` is a protocol toolkit with no download engine, and the remainder are
  archived, pre-module, or single-digit-star projects.

  **The decision therefore returns to the owner, at option 1a + 2a**, unless the owner prefers to
  accept rain's licensing and redesign per-torrent destinations with full knowledge of the above.
  T-031 remains `blocked`. DEC-098/DEC-099 are **not** reverted under any of these outcomes: rain's
  own tree contains MPL-2.0, so the `ALLOWED_LICENSES` entry stays either way, and
  `scripts/check-license-scope.sh` becomes *more* load-bearing, not less — it is the only gate that
  caught either tree. If 1a is taken, `ALLOWED_MPL_MODULE` should become an explicit list of named
  modules rather than a license family, with `check-license-scope_test.sh` still proving an
  unlisted MPL-2.0 module fails.

  **Orchestrator verification (2026-09-17), independent of the implementing agent.** Spot-checked
  upstream `LICENSE` files directly: `anacrolix/dht` and `anacrolix/log` are Mozilla Public License
  2.0 at `master`, consistent with finding (1). For finding (2) the picture is more specific than
  "no license": `github.com/go-llsqlite/adapter`'s **upstream `master` does carry an MPL-2.0
  `LICENSE`**, but the revision the engine pins — `v0.0.0-20230927005056-7f5ce7f0c916`, a
  2023-09-27 pseudo-version — ships without one; the cached module directory contains only
  `go.mod`, `go.sum`, `crawshaw.go`, `llsqlite.go`, `result-code.go`, `zombiezen.go` and
  `sqlitex/`. So (2) is a *pinned-revision* gap, not an unlicensed project, which makes **2a**
  answerable: the license exists upstream, and a newer revision or an explicitly recorded finding
  could satisfy it. It does not make (2) go away on its own — and note that under **1a** this
  module would need admitting as MPL-2.0 too, so the two findings resolve together, not
  separately.

  **T-943 landed the policy change (2026-09-18, DEC-100) — two carry-overs for T-031.** (i) The
  admitted set is **ten** modules, not the eight listed above: `github.com/anacrolix/mmsg` is also
  in the tree, reached via `github.com/anacrolix/go-libutp` (MIT) on **any cgo-enabled build**.
  `mmsg` and `utp` are alternative uTP transports and never appear together; the determinant is
  `CGO_ENABLED`, **not** `GOOS` (measured over all six combinations: cgo on → `mmsg`, cgo off →
  `utp`, identically on darwin, linux and windows). Both are in the `Makefile`'s
  `ALLOWED_MPL_MODULES`, because whichever `GOOS` is native to the host builds with cgo on while
  the rest cross-compile with it off. (ii) Finding (2) is resolved by **pinning
  `github.com/go-llsqlite/adapter` at `v0.2.0` or later** — **not `v0.1.0`, which carries no
  license file either** — that tag carries the upstream MPL-2.0 `LICENSE` the 2023 pseudo-version
  lacks, builds against `anacrolix/torrent v1.61.0`, and leaves
  zero `Unknown` rows on any `LICENSE_OSES`. A bare `go get github.com/anacrolix/torrent` selects
  the unlicensed pseudo-version and still fails `make licenses`, so T-031 must
  `go get github.com/go-llsqlite/adapter@v0.2.0` explicitly. No `--ignore` entry was added and
  `NOTICE` omits nothing. The `govulncheck` findings recorded above are untouched by T-943 and are
  still worth a look when the engine lands.

  **Resolved on 2026-09-18.** The project owner first chose to **replace** the locked engine
  (option 1b). That was pursued and returned no viable target: of the pure-Go embedded candidates,
  `cenkalti/rain` embeds cleanly (`RPCEnabled=false`, no control socket, peer port only) but its
  tree carries **LGPL-3.0** (`juju/ratelimit`), two MPL-2.0 modules and the permanently unlicensed
  `nictuku/nettools`, and it has **no per-torrent save-path API**, so it cannot honour the frozen
  §5 `AddSource.SavePath` that T-034's multi-root containment depends on; every other candidate
  was a fork of `anacrolix/torrent` (same MPL tree), a protocol toolkit with no download engine,
  archived, or a toy. `NO CANDIDATE CLEARS THE GATES` was the recorded result. The owner therefore
  chose **option 1a** on 2026-09-18, and `T-943` landed it as PR #30 (`9f20748`): the MPL-2.0
  exception is now an **enumerated set of ten named modules** — deliberately not an
  `anacrolix/*` prefix, so admitting a module stays a reviewed act — policed by the widened
  `scripts/check-license-scope.sh` and its mutation-checked regression test. Finding (2) was
  resolved the preferred way rather than with an `--ignore`: `github.com/go-llsqlite/adapter`
  moves to **`v0.2.0`**, the first revision carrying the upstream MPL-2.0 `LICENSE`
  (`v0.1.0` and the earlier pseudo-version carry none), leaving zero `Unknown` rows.
  QA failed PR #30 once on two false statements in AGENT.md — an incorrect adapter version, and
  the `mmsg`/`utp` split attributed to `GOOS` when the determinant is `CGO_ENABLED` — both
  remediated on the same branch and independently re-verified by a second reviewer. **Two
  carry-overs bind T-031:** it must pin `github.com/go-llsqlite/adapter@v0.2.0` explicitly (a bare
  `go get github.com/anacrolix/torrent` selects the unlicensed pseudo-version and fails
  `make licenses`), and the admitted set already includes both `anacrolix/utp` and
  `anacrolix/mmsg` because no single build configuration yields all ten. The `govulncheck`
  findings recorded above are untouched and still worth a look when the engine lands.

- `T-031` (2026-09-16 → 2026-09-17). **License policy conflicts with the locked torrent-engine dependency.**
  T-031 builds `internal/engine/anacrolix`, which requires `github.com/anacrolix/torrent` — the
  engine AGENT.md §3 locks ("do not re-litigate") and DEC-001 selected. That module is licensed
  **MPL-2.0** (verified from the upstream `LICENSE` at
  `https://raw.githubusercontent.com/anacrolix/torrent/master/LICENSE`, first line
  "Mozilla Public License Version 2.0"). MPL-2.0 is copyleft and is on none of the project's
  allowlists: AGENT.md §3 says "MIT / Apache-2.0 / BSD only — no GPL/AGPL", AGENT.md §16 and
  DEC-009 say "MIT / Apache-2.0 / BSD / ISC ... `go-licenses` runs in CI and fails the build on a
  copyleft dependency", and `Makefile:67` enforces
  `ALLOWED_LICENSES := MIT,Apache-2.0,BSD-2-Clause,BSD-3-Clause,ISC` through `go-licenses check`.
  Adding the dependency therefore fails `make licenses` on the first commit. This is a conflict
  *inside* AGENT.md (§3 vs §16), which AGENT.md's preamble says to stop and log rather than guess
  past, and it blocks the whole T-031 → T-034 → T-041/T-070+ chain, so no independent task can be
  worked around it under the one-task-at-a-time rule.

  **What would unblock it — a project-owner decision, recorded as a DEC- entry:**
  1. Carve out an explicit MPL-2.0 exception for `github.com/anacrolix/torrent` (file-level
     copyleft only, not whole-program), and widen `ALLOWED_LICENSES` in the `Makefile` plus the
     AGENT.md §3/§16 wording and the `NOTICE` generation to match; or
  2. Replace the locked engine with an MIT/Apache-2.0/BSD/ISC-licensed alternative, which
     overrides DEC-001 and a locked §3 stack row and would rename `internal/engine/anacrolix`.

  No branch was created, no code written, and `go.mod` is untouched.

  **Resolved on 2026-09-17.** The project owner chose option 1 and authorised admitting MPL-2.0
  rather than replacing the locked engine. `T-942` landed that as PR #28 (`3ed0f69`): the `Makefile`
  admits MPL-2.0, and because `go-licenses check --allowed_licenses` has no per-module scoping, a
  separate `scripts/check-license-scope.sh` enforces that the only MPL-2.0 module is
  `github.com/anacrolix/torrent` — so the narrow scope is a gate CI fails on, not just prose. See
  DEC-098 for the authorisation and its scope, DEC-099 for the two-part mechanism and the residual
  gap it still leaves. QA independently proved a rogue MPL-2.0 module is rejected and that GPL and
  AGPL still fail. T-031 returned to `todo`; nothing about DEC-001 or any other §3 stack row was
  reopened.

- `T-022` (2026-09-15 → 2026-09-16). Blocked on seven `golang.org/x/net` advisories reachable from
  the scraper's `html.Parse` under the pinned Go 1.23. The project owner authorised raising the
  minimum Go version; `T-941` landed that as PR #14 (`69d5d07`) and `main` now declares
  `go 1.25.0`. T-022 returned to `in-progress`. Note for its branch: pin `x/net v0.58.0`, not
  `v0.55.0` (which still carries `GO-2026-5942`) and not `@latest` (`v0.59.0` declares
  `go 1.26.0`, past what was authorised). **Done on 2026-09-16:** `main` was merged into
  `task/T-022-scraper-framework`, which now pins `x/net v0.58.0` alongside `goquery v1.13.0` and
  `cascadia v1.3.4` (DEC-078). `govulncheck ./...` reports `No vulnerabilities found.` with no
  trailing module-level count, on darwin/arm64 under go1.27.1; the CI legs are not verifiable
  from here.
- `T-915` Make `callSearch`'s cancellation path deterministic in `internal/indexer/registry.go`.
  QA on T-054 (PR #25) reproduced `TestSearchAllParentContextAlreadyCancelled` failing roughly once
  in a thousand runs (`go test -race -count=1000 -run TestSearchAllParentContextAlreadyCancelled
  ./internal/indexer/` → `SearchAll with a cancelled context = <nil>, want ErrAllSourcesFailed`).
  This is a genuine latent race in pre-existing T-012 code, not CI noise: `callSearch` runs
  `ix.Search(ctx, q)` in a goroutine and then selects over `done` and `ctx.Done()`. When the parent
  context is already cancelled before the call and the indexer returns instantly, both cases can be
  ready, and Go's `select` tie-breaks pseudo-randomly — so a stale success is occasionally returned
  where cancellation was required. Remedy: check `ctx.Err()` before the select, or give the
  cancellation case priority via a nested select, rather than relying on the tie-break. It surfaced
  as an intermittent red `make check (macos-latest)` job.
- `T-916` Add a CI guard for the §2 no-named-sites rule. QA on T-056 (PR #27) caught a new test file
  that hardcoded a denylist of real infringement-oriented site names — a §2/§16 hard violation — by a
  reviewer's eye alone. `scripts/check-indexer-hostnames.sh` did not catch it because its
  keyword-context gating targets indexer endpoint shapes, not arbitrary prose in a test file. Proposal:
  a companion CI check that scans the whole diff against a small name-fragment list sourced from
  OUTSIDE the repo (so the list itself never lands here, which is the same reason the hostname script
  uses a positive allowlist). Remediation on T-056 replaced the offending test with positive,
  allowlist-based assertions (`TestFixtureSourceURLsUseOnlyReservedDomains`) — that pattern is the
  model to follow.
- `T-917` History hygiene for the §2 rule. §2 covers the repository including its history, and two
  artifacts sit outside `main`'s current tree: (a) the initial docs commit `f91c0de` named a site in an
  AGENT.md §7 mockup, already removed from the tree by `f7f4dfa` but still present in that commit's
  content; (b) the T-056 QA review comment on PR #27 quotes the offending denylist verbatim, and the
  pre-remediation commit `2a4c534` on the now-deleted branch contained it. None of this is in `main`'s
  tree today. Purging it fully needs history rewriting and editing/deleting a PR comment — an owner
  decision, not an agent's, and AGENT.md §10 forbids force-pushing a branch under review. Filed so the
  choice is recorded rather than forgotten.
