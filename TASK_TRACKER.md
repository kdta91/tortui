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
`<Options.StateDir>/tortui.log` when `File` is empty), and — critically — calls
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
embedded basic-auth credentials), `key=value`/`key: value` credential patterns, `Bearer <token>`
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
`Bearer <token>` header, and a basic-auth URL under a non-sensitive key — alongside a regression
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
- `T-911` Install a `pre-merge-commit` hook. QA on T-004 (PR #4) demonstrated that merge commits
  bypass `scripts/pre-commit` entirely: git invokes `pre-merge-commit` for merges, and that hook is
  not installed. Reproduced by committing a secret on a side branch with hooks disabled, then
  merging with `git merge --no-ff` while `pre-commit` was active — the merge succeeded and the
  secret entered history uncaught.
- `T-912` Close the binary-file gap in the secret scan. QA on T-004 (PR #4) confirmed that gitleaks
  skips binary content by design in both `gitleaks git --staged` (the hook) and `gitleaks dir`
  (`make scan` in CI), so a secret embedded in a binary-ish file is caught by neither the hook nor
  the CI backstop.

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
| DEC-020 | 2026-09-12 | **Superseded by DEC-021.** Branch protection on `main` requiring the CI check was left unconfigured, flagged as blocked rather than silently skipped | `kdta91/tortui` was a private repo on a free GitHub plan at the time; both the classic branch-protection API and the repository-rulesets API returned `403 Upgrade to GitHub Pro or make this repository public` for this repo. Unblocking needed either the repo made public or the account upgraded to GitHub Pro/Team — a decision for the human owner, not the agent. The repo owner made it public; see DEC-021 | T-001 |
| DEC-021 | 2026-09-12 | Repo owner made `kdta91/tortui` public; branch protection on `main` then configured via the classic API (`required_status_checks` on the three `make check` matrix contexts, strict, `required_pull_request_reviews` left unset) | Verified `gh repo view` reports `"visibility":"PUBLIC"` before retrying, which is what unblocked the classic protection endpoint (previously 403 per DEC-020). Deliberately did **not** enable "require a pull request before merging" — AGENT.md §10 requires tracker status flips to push straight to `main`, and that setting would stall the loop on the first task | T-001 |
| DEC-022 | 2026-09-12 | **Corrected 2026-09-12 (same day, on QA remediation of PR #2).** Per-OS config/state/download path resolution lives in `internal/platform` (`paths_darwin.go`, `paths_linux.go`, `paths_windows.go`, one function set per file, no shared `runtime.GOOS` switch). `github.com/adrg/xdg` **is** a dependency in `go.mod`, per AGENT.md §3, and is genuinely used: `paths_linux.go` calls `xdg.Reload()` then reads `xdg.ConfigHome` / `xdg.StateHome` / `xdg.UserDirs.Download`. `paths_darwin.go` and `paths_windows.go` continue to read `os.Getenv`/`os.UserHomeDir` directly rather than through `xdg`, for OS-specific reasons below | The original version of this row justified dropping `adrg/xdg` entirely with: "there is no supported way to make it re-resolve against an env this package sets mid-test." **That claim was false.** `adrg/xdg@v0.5.3` exports `xdg.Reload()`, documented as refreshing base and user directories by reading the environment — exactly the injectable-env mechanism the claim said didn't exist. Verified directly: read the library's source (`xdg.go`, `paths_unix.go`, `paths_darwin.go`, `paths_windows.go`) rather than trusting the earlier claim, then proved `t.Setenv` + `xdg.Reload()` re-resolves correctly with real tests, including cross-executing the compiled `internal/platform` Linux test binary on linux/amd64 under Docker (not just reasoning about it) — all pass. It's safe under `go test -race` too: `t.Setenv` already forbids a test from calling `t.Parallel`, so nothing exercises `xdg`'s mutable package-level vars concurrently. Given it works, why not use it on all three OSes? Because its own compiled-in defaults conflict with conventions this project already froze, on two of the three: on macOS, `xdg.ConfigHome` defaults to `~/Library/Application Support` when `XDG_CONFIG_HOME` is unset (confirmed by reading `paths_darwin.go` in the module and by a live `xdg.Reload()` call), the opposite of DEC-005's `~/.config`. On Windows, `xdg.ConfigHome` and `xdg.StateHome` both default to `%LocalAppData%` with no roaming-config field exposed at all, so the library cannot express the `%AppData%`-for-config / `%LocalAppData%`-for-state split AGENT.md §14 requires. Using `xdg` as the sole implementation on those two OSes would mean either silently reintroducing the exact paths DEC-005 and §14 already rejected, or bypassing its default resolution entirely — at which point it would not be meaningfully "used," just imported for appearances. Linux is the one OS where `adrg/xdg`'s own defaults are exactly the XDG Base Directory Specification this project targets there, so `paths_linux.go` uses it directly; `paths_darwin.go` and `paths_windows.go` keep their plain `os.Getenv`/`os.UserHomeDir` implementation, which was always just as `t.Setenv`-testable as Linux's — the original claim's blocker never actually applied to any of the three files, on any OS. `BurntSushi/toml` is unaffected and remains the TOML library | T-002, T-003 |
| DEC-023 | 2026-09-12 | On Linux, the default download directory prefers `$XDG_DOWNLOAD_DIR/tortui` over `~/Downloads/tortui` when `XDG_DOWNLOAD_DIR` is set (same precedence used for `XDG_CONFIG_HOME` vs `~/.config`) | AGENT.md §14's Linux line — "Default download dir `~/Downloads/tortui`, falling back to `$XDG_DOWNLOAD_DIR`" — reads ambiguously about which one is primary. Treating the explicit env var as the override and the literal path as the fallback matches ordinary XDG semantics and how every other XDG variable in this codebase behaves; the reverse reading (env var only used when `~/Downloads` is somehow unusable) has no clear trigger condition. **Addendum, DEC-022 remediation:** `DownloadDir()` on Linux now delegates to `github.com/adrg/xdg`'s `xdg.UserDirs.Download`, which actually has three levels, not two: `$XDG_DOWNLOAD_DIR` (if set to an absolute path), then an entry from `~/.config/user-dirs.dirs` (the desktop `xdg-user-dirs` mechanism) if that file exists and sets one, then the literal `~/Downloads`. This doesn't change the precedence conclusion above — the explicit env var still wins over every fallback — it just means the fallback is one step richer than this row originally described | T-002 |

| DEC-024 | 2026-09-12 | `internal/logging` (T-003) writes JSON log lines and calls `slog.SetDefault` on the logger it builds | JSON was chosen only because it's trivial to parse in tests and matches how the masking handler already walks nested `slog.Group` values; no acceptance criterion names a format. Calling `slog.SetDefault` is what makes "nothing is ever written to stdout or stderr" hold for code that uses the package-level `slog.X` functions rather than a logger obtained from `New` directly — otherwise it would silently fall back to slog's built-in stderr-writing handler | T-003 |
| DEC-025 | 2026-09-12 | T-003's "rotation at 10 MB with 3 files retained" is implemented as 3 rotated backups plus the still-active file (4 files on disk at steady state: `tortui.log`, `.log.1`, `.log.2`, `.log.3`) | The phrase reads two ways — 3 files total, or 3 backups on top of the active one. Read it the second way, matching the common `MaxSize`/`MaxBackups`-style rotation convention this phrasing echoes; the reverse reading would mean only 2 backups are ever kept, which is what "3 files retained" would describe less naturally | T-003 |
| DEC-026 | 2026-09-12 | **Addendum 2026-09-12 (PR #3 QA remediation, same day).** A sensitive attribute or URL is redacted wholesale (`[REDACTED]`), not partially (e.g. keeping a URL's scheme and host) | An indexer URL routinely carries the API key or session cookie itself as a query parameter (`?apikey=...`), so a partial mask keyed only on the attribute's own name (`cookie`, `api_key`) could still leave the same secret sitting in the part of a `url`-keyed value that was left visible. Masking is applied both by attribute key (recursing through `slog.Group` nesting and `Logger.With`-bound attrs, and — as of the addendum — through struct fields and map keys reached via a `slog.Any` value) and, independently, by scanning message text and string values for URL-shaped or `key=value`-shaped credentials, `Bearer <token>` headers, and a finite list of known bare secret-token shapes. **Addendum, correcting an overclaim:** the original wording here and in the PR description — "masked in every log line" — was not fully true: QA reproduced two live leaks, a struct logged via `slog.Any` (invisible to the masking handler entirely, since it only inspected `KindGroup`/`KindString`) and a bare secret value with no URL/`key=value` shape under a non-sensitive key. Both are now handled (see T-003's notes and `mask.go`'s package doc), but a **residual gap remains and is not closable in general**: an opaque secret with none of the recognized shapes, logged under a key name that isn't itself flagged sensitive, cannot be reliably distinguished from ordinary opaque data. That gap is procedural to close (name credential-carrying keys/fields using a recognized substring), not technical | T-003 |
| DEC-027 | 2026-09-12 | Rotation (`internal/logging/rotate.go`) is a from-scratch `io.WriteCloser`, not a third-party library such as `natefinch/lumberjack` | AGENT.md §3 requires a `DEC-` row with a license check for any new dependency, and the task explicitly invited weighing writing it directly against adding one. The rotate-and-prune-N-backups logic needed here is small (~140 lines) and self-contained, with nothing to audit in someone else's rotation/retry/error-handling behaviour, and it sits directly behind the masking handler with no intermediary. No third-party library's source was read as part of this choice, since none was added — this row is the "wrote it myself" side of that trade-off, not a claim about any specific library's behaviour | T-003 |
| DEC-028 | 2026-09-12 | **Corrected 2026-09-12 (same day, on QA remediation of PR #3) — the original row below contained a false statement and is rewritten rather than superseded, matching how DEC-022 was corrected in place.** `internal/logging`'s config/env/flag precedence (`ResolveLevel`, `ResolveFile`) is exposed as pure string-in/string-out functions, and `cmd/tortui/main.go` **does** declare `--log-level` and `--log-file` on its existing `flag.FlagSet` (alongside `--version`/`--config`, both since T-001), feeding their values through `ResolveLevel`/`ResolveFile` (config-side argument hardcoded to `""` for now — see below) and then `ParseLevel` for validation. `internal/config.Config` still gains no `log_level`/`log_file` fields in this task — that half of the original row was accurate and is unchanged. New environment variables: `TORTUI_LOG_LEVEL`, `TORTUI_LOG_FILE`, checked between the config value and the flag value in that priority order (flag > env > config > built-in default) | **What was false:** the original justification claimed "T-002 established the precedent of implementing a package's full behaviour while deferring its wiring into `main` to the composition root," and used that precedent to justify not declaring the flags at all. PR #3 QA (kdta91, 2026-09-12) checked this directly: T-002 deferred *consuming* `internal/config.Load`'s result into `main` — but the `--config` flag it would have consumed already existed, declared in T-001, before T-002 started. T-002 never had to *declare* a new flag on `main`'s `flag.FlagSet`; T-003 did, and `cmd/tortui/main.go` already has a working `FlagSet` with two flags declared on it, so adding two more needed no composition root. That made the precedent claim inapplicable to what T-003 actually needed to do, not merely a stretched reading of it. The config-side input to `ResolveLevel`/`ResolveFile` is still `""` in `main.go` because `internal/config.Config` carries no `log_level`/`log_file` fields yet (unaffected by this correction) — once the composition root loads config.toml, its resolved values replace that placeholder | T-003 |
| DEC-029 | 2026-09-12 | CI installs `gitleaks` v8.30.1 via `go install github.com/zricethezav/gitleaks/v8@v8.30.1` (not `github.com/gitleaks/gitleaks/v8`) on all three OSes; `make scan` (part of `make check`) runs `gitleaks dir` — a working-tree scan with no git-history walk — rather than a full-history `gitleaks git` scan | GitHub-hosted runners don't ship `gitleaks`, so it needs the same `go install` treatment already used for `gofumpt`/`goimports`/`golangci-lint`. `go install github.com/gitleaks/gitleaks/v8@v8.30.1` was tried first (that is the project's GitHub org/repo name) and fails with "version constraints conflict ... module declares its path as: github.com/zricethezav/gitleaks/v8" — confirmed directly by attempting the install locally before writing it into CI, not assumed. `dir` (not `git`) mode was chosen for `make check` because the question each CI run needs answered is "does the tree as checked out right now contain a secret," which is fast and constant-time per run; a full-history scan re-walks the same already-scanned commits on every push and answers a different question ("was a secret ever committed at any point"), which the pre-commit hook's `gitleaks git --staged` already prevents going forward | T-004 |
| DEC-030 | 2026-09-12 | `scripts/pre-commit` and `make scan` both fail loudly (block the commit / fail the build) with an install hint when the `gitleaks` binary is missing, rather than skipping the scan silently | A hook or check that can pass by omission isn't trustworthy — a missing binary and "no secrets found" must never look the same. CI is guaranteed to have `gitleaks` (DEC-029), so this path only bites a local dev without it installed, and the message tells them exactly what to run | T-004 |
| DEC-031 | 2026-09-12 | A committed `config.toml` (as opposed to `config.example.toml`) is blocked by two independent mechanisms: a filename check in `scripts/pre-commit` (`git diff --cached --name-only` matched against `(^\|/)config\.toml$`) and a path-only `tortui-config-toml` rule in `.gitleaks.toml` with no content regex, mirroring the pattern gitleaks' own built-in `pkcs12-file` rule uses for `.p12`/`.pfx` files | Neither mechanism alone covers both gates: the hook only runs if a contributor has activated it (`make hooks`) and isn't bypassed with `--no-verify`, while `make check`'s `gitleaks dir` scan only runs in CI/on demand and needs its own rule to catch a `config.toml` with no secret-shaped content yet (content-only scanning would miss a "boring" one). Verified live: a forced `git add -f config.toml` (bypassing `.gitignore`, which already blocks a plain `git add`) was still rejected by the hook | T-004 |
| DEC-032 | 2026-09-12 | `internal/logging/mask_test.go`'s deliberate secret-shaped fixtures (T-003) are excluded from scanning via a `[allowlist]` `paths` entry in `.gitleaks.toml` naming that exact file, not via a global rule/entropy change | Confirmed by direct test that the same fixture strings (an AWS-shaped key, a GitHub-PAT-shaped token) committed at a *different* path are still flagged — proving the allowlist is scoped to the one file that needs it rather than quietly weakening detection everywhere | T-004 |
| DEC-033 | 2026-09-12 | Added a custom `tortui-generic-cookie` rule (`(?i)\b(?:set-)?cookie\b\s*[:=]\s*\S{6,}`) to `.gitleaks.toml` | Read the full default ruleset (`config/gitleaks.toml` inside the `gitleaks` module) rather than assuming: it has no generic Cookie/Set-Cookie rule, only vendor-specific session-cookie formats such as `gitlab-session-cookie`, and "cookie" appears only as a `generic-api-key` allowlist *stopword* (a value gitleaks ignores if the entire match equals it), never as a keyword that would trigger that rule on an arbitrary cookie-header line. Without a dedicated rule, T-004's "blocks cookies" acceptance criterion had no default coverage at all | T-004 |

Append a row whenever you make a choice a future reader would question. Empty date means
inherited from the initial plan.

---

## Blocked

*(empty — append `T-0NN` blocks here with the exact input needed to unblock)*
