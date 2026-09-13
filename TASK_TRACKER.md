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
with `CGO_ENABLED=0 GOOS=... GOARCH=... go build` into `dist/tortui-<os>-<arch>[.exe]` and exiting
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
literal string (`download_dir = '<path>'`) rather than a Go double-quoted one, so a Windows path's
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
and found `required_status_checks.contexts` held exactly the three `make check (<os>)` jobs;
`indexer-hostnames`'s context was absent. This repo merges via `gh pr merge --auto`, and GitHub
auto-merge only waits on *required* checks — so a red `indexer-hostnames` run blocked nothing, and
the acceptance criterion was not actually met regardless of how the job itself behaved. The
original reasoning ("AGENT.md doesn't ask this task to change branch protection and QA review is
what actually gates the merge") is not a defense against this: `gh pr merge --auto`, not a human
watching the checks tab, is what actually merges here, and auto-merge does not consult
non-required checks at all. Fixed by adding `check indexer hostname allowlist (T-007)` — the
exact context string, confirmed from a real completed check run on this PR's own head commit
(`gh api repos/kdta91/tortui/commits/<head-sha>/check-runs`, `conclusion: success`, triggered by
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
  and closing it was cheap (`git log --format=%B <range>` appended to the diff as synthetic `+`
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
the registry at <https://www.iana.org/assignments/media-types/media-types.xhtml> was fetched on
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
status: in-progress
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

| DEC-034 | 2026-09-12 | `shellcheck -s sh $(wildcard scripts/*)` is wired into `make lint` and fails loudly (build error + install hint) when `shellcheck` is missing, rather than skipping the check silently; CI installs it explicitly on all three OSes (`apt-get` / `brew` / `choco`) instead of assuming a runner image ships it | Mirrors T-004's `gitleaks` precedent (DEC-029/DEC-030) exactly: a check that can pass by omission ("shellcheck wasn't run" vs. "shellcheck ran and found nothing") isn't trustworthy, and this project has no verified claim about what any GitHub-hosted runner image ships by default, so it is installed rather than assumed | T-005 |
| DEC-035 | 2026-09-12 | `make build-all` sets `CGO_ENABLED=0` for all six cross-compiles | None of this project's current dependencies (`BurntSushi/toml`, `adrg/xdg`) need cgo, and cross-compiling a cgo-enabled build for a non-host GOOS/GOARCH (e.g. `windows/arm64` from a `darwin/arm64` host) needs a matching C cross-toolchain that isn't installed anywhere in this pipeline. Forcing pure-Go compilation sidesteps that entirely; if a future dependency genuinely needs cgo, that dependency choice needs its own `DEC-` row per AGENT.md §3 and would have to address this directly | T-005 |
| DEC-036 | 2026-09-12 | The new CI `build-all` job runs on `ubuntu-latest` only, not the three-OS matrix `check` already uses | `go build` with `GOOS`/`GOARCH` set produces the same output regardless of which OS or arch the compiler itself runs on — cross-compilation is the entire point of `make build-all` — so running it three times would triple the job's cost for identical output, not additional coverage. The three-OS matrix stays reserved for `make check`, which runs native tests that really do depend on the host OS | T-005 |
| DEC-037 | 2026-09-12 | **QA remediation of PR #5, same day.** Every `Makefile` recipe line now starts its shell invocation with a literal `set -eu;`, in addition to (not instead of) keeping `.SHELLFLAGS := -eu -c` | QA found, and this remediation independently reproduced with a throwaway Makefile against this machine's actual `/usr/bin/make`, that `.SHELLFLAGS` is itself a GNU Make 3.82+ feature: on this project's floor version, make 3.81, it is not recognized at all, so `-e`/`-u` were silently never in effect — a non-final `false` in a recipe didn't stop the recipe, and referencing an unset shell variable didn't error. AGENT.md §14 asks for both `.SHELLFLAGS := -eu -c` *and* zero 3.82+/4.0+ features, which is an internal tension once `.SHELLFLAGS` itself turns out to be one — resolved here by keeping the line (harmless, and it does take effect on newer make, e.g. this project's Linux CI runner) and separately achieving the same `-e`/`-u` behaviour on 3.81 the only way that's actually possible there: `set -eu;` is plain POSIX shell syntax passed as part of the command string itself, so it works identically regardless of what flags make chose when invoking `$(SHELL)`. Verified with the same throwaway-Makefile method: a `set -eu; false; echo ...` recipe line stopped before the echo, and `set -eu; echo $${UNSET_VAR}` failed on the unbound reference, both under make 3.81 | T-005 |
| DEC-038 | 2026-09-12 | `LICENSE`'s copyright holder is the GitHub handle `kdta91`, not a personal or legal name; `NOTICE` (generated by `go-licenses/v2 v2.0.1`) enumerates exactly two dependencies — `github.com/BurntSushi/toml` and `github.com/adrg/xdg`, both MIT — and `make licenses` runs `go-licenses` once per `GOOS` in `darwin linux windows` rather than once on the host OS | No legal name for the repo owner is available anywhere in this repo or its history to attribute copyright to, and inventing one would be a false attribution in a legal document; the task instructions explicitly required falling back to the GitHub handle rather than fabricating a name in that situation, so `kdta91` is used verbatim. Separately, `go-licenses report ./...` was verified (by direct experiment, not assumed) to inspect the *compiled* import graph, and this repo's own `internal/platform` is intentionally split into `_darwin.go`/`_linux.go`/`_windows.go` files (T-002/DEC-022): only `paths_linux.go` imports `github.com/adrg/xdg`, so a report run with the host's default `GOOS=darwin` misses `adrg/xdg` entirely (confirmed: it appears under `GOOS=linux` and is absent under `GOOS=darwin` and `GOOS=windows`). Since AGENT.md §14 treats darwin/linux/windows as equally tier-1, "every … transitive dependency" has to mean every dependency compiled into *any* of the three, not just whichever one happens to be the CI runner's or developer's own OS — so `make licenses` loops all three `GOOS` values and merges, the same "analyze every platform from one host" approach `build-all` already established for cross-compilation (DEC-036). This also settles what NOTICE should *not* list: `github.com/stretchr/testify`, `github.com/davecgh/go-spew`, `github.com/pmezard/go-difflib`, and `gopkg.in/yaml.v3` all appear in `go.sum` and `go mod graph`, but `go mod graph` confirms every one of them is required only by `github.com/adrg/xdg`'s own `go.mod` (its test dependencies), not by any package this repo imports; grepping this repo's `.go` files for all four turns up nothing, and none of the three per-GOOS `go-licenses report` runs lists them either. They are go.sum/module-graph entries needed to verify checksums, not anything ever compiled into a tortui binary, so listing them in `NOTICE` would misrepresent what tortui actually ships. The same "never actually compiled into a tortui binary" reasoning, for a different mechanical cause, is why `golang.org/x/sys` (present in `go.mod` as an `// indirect` requirement, and surfaced by `go mod why -m golang.org/x/sys` as required via `github.com/adrg/xdg`'s own `golang.org/x/sys/windows` import) is also correctly absent from `NOTICE`: that import lives in a Windows-only file inside `adrg/xdg` itself, and `adrg/xdg` is in turn only ever imported by this repo's `paths_linux.go` (never `paths_darwin.go` or `paths_windows.go` — T-002/DEC-022). So on every one of the three tier-1 `GOOS` values this repo actually builds for, either `adrg/xdg` isn't compiled into tortui at all (darwin, windows) or it is compiled in under `GOOS=linux`, where Go's own build constraints exclude `adrg/xdg`'s `golang.org/x/sys/windows`-importing file. Verified directly: none of the three per-GOOS `go-licenses report` runs — including the `GOOS=windows` one — lists `golang.org/x/sys` either. `go-licenses check`'s enforcement is an allowlist (`--allowed_licenses=MIT,Apache-2.0,BSD-2-Clause,BSD-3-Clause,ISC`, matching AGENT.md §3/§16) rather than a GPL/AGPL/LGPL denylist, verified directly: removing `MIT` from the allowlist and re-running made the same two dependencies fail with an explicit "not allowed" message, proving the mechanism actually rejects a disallowed license rather than passing by omission | T-006 |
| DEC-039 | 2026-09-13 | **QA remediation of PR #6, same day.** `make licenses`'s NOTICE-merge step now pins `LC_ALL=C` on both the `sort -u` that dedups/orders the merged per-GOOS reports and the final `awk` column reformat, instead of relying on whichever locale happens to be ambient | QA reproduced the PR's `licenses` CI job failing on *every* run, not only when a dependency actually changed. Root cause, confirmed directly rather than assumed: plain `sort -u` collates by the shell's ambient `LC_COLLATE`, and `github.com/BurntSushi/toml` sorts before `github.com/adrg/xdg` under the `C`/POSIX byte-order collation GitHub's `ubuntu-latest` runner uses, but *after* it under `en_US.UTF-8` — the locale of the macOS machine the original `NOTICE` was generated and committed on. Same two dependencies, same licenses, different byte order — so CI's `git diff --exit-code -- NOTICE` staleness check failed unconditionally on the runner. Reproduced by regenerating under `LC_ALL=C` then `LC_ALL=en_US.UTF-8` on the old Makefile (byte-different output, confirming the bug); regenerating under both locales with `LC_ALL=C` pinned on the fix produces byte-identical output, confirming the fix. Committed `NOTICE` is regenerated in the new, `C`-locale/CI-matching order (`BurntSushi/toml` now sorts first). Swept the rest of the `Makefile` for the same class of bug (ambient locale, sort order, hash/map-iteration order, timestamps): no other recipe's output feeds a committed file or a CI comparison the way `licenses` does — `build-all`'s trailing `ls -1 $(DIST)` and `lint`'s `shellcheck $(wildcard scripts/*)` both have an order that can vary by environment, but neither gates on that order (each build target / each shellcheck'd file passes or fails independently of the others' order), and `DATE`'s build timestamp is intentionally build-specific `ldflags` metadata that is never diffed against a committed value — none of those needed a change. **Also corrects an overstated claim in PR #6 and T-006's own tracker notes:** both said `make licenses` was verified green "locally... before opening the PR" and that two back-to-back local runs "confirm[ed] determinism"; in fact it was verified only on macOS under one locale, that check could not have caught a cross-locale bug by construction, and the PR's own CI (`ubuntu-latest`) was failing on both of its runs the whole time the T-006 row said `done`. That CI result was never checked before the row was marked complete. See the corrected T-006 notes above and the corrected PR #6 description | T-006 |
| DEC-040 | 2026-09-13 | **Corrected 2026-09-13 (QA remediation of PR #7) — the closing sentence of the rationale column below was false and is rewritten in place, matching how DEC-022/DEC-028 were corrected.** The T-007 "scans the diff for anything resembling a real indexer hostname" check is implemented as a shape-plus-context heuristic (`scripts/check-indexer-hostnames.sh`) with a currently-empty allowlist (`docs/indexer-hostname-allowlist.md`), rather than any form of denylist | AGENT.md §2 bars naming an infringement-oriented site anywhere in this repository, including in a blocklist, a regex, or a fixture — so a denylist of known piracy hostnames was never an option regardless of implementation difficulty; that is a hard `AGENT.md §2` constraint, not a design preference. T-024 (bundled lawful sources) hasn't landed yet, so there is also no real allowlist content to check against today. The heuristic scans only lines that (a) live in files where tortui actually defines indexer sources (`internal/indexer/**`, `config.example.toml`, `docs/indexer-definitions.md`, `docs/bundled-sources.md`, `docs/indexer-hostname-allowlist.md`, `testdata/**`) or (b) mention `indexer`/`torznab`/`scraper`/`base_url`, so a dependency URL in `NOTICE`, a README link, or a CI action reference is never inspected regardless of allowlist content — this is what keeps the check from false-positiving on the current tree (verified by diffing the empty git tree against `HEAD`, see the T-007 tracker notes) without needing to allowlist every incidental hostname already in the repo. A structurally non-resolving placeholder — `localhost`, or a host ending in the RFC 2606/6761 reserved `example`/`test`/`invalid`/`localhost` labels — is always allowed independent of the allowlist file, since those can never resolve to a real bundled source by construction. **What was false:** the original version of this row extended that same "can never be a real source by construction" reasoning to *any* IP literal. That does not hold — unlike a reserved TLD, a bare IP address can absolutely be a real production endpoint, and QA (PR #7) called this out directly. Corrected: only a **private/loopback/link-local** IPv4 literal (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `127.0.0.0/8`, `169.254.0.0/16`, `0.0.0.0/8`) is auto-allowed by construction now; a public IPv4 literal is treated like any other hostname and falls through to the same allowlist, flagged if it isn't on it. `is_allowed()`/`is_private_ipv4()` in `scripts/check-indexer-hostnames.sh`, `docs/indexer-hostname-allowlist.md`, and the T-007 tracker notes were all updated to match, with a regression test (`scripts/check-indexer-hostnames_test.sh`) covering both the private-allowed and public-flagged cases | T-007 |
| DEC-041 | 2026-09-13 | `check-indexer-hostnames.sh` is wired as its own `indexer-hostnames` CI job (`if: github.event_name == 'pull_request'`), not as a new prerequisite of `make check` or the `check` CI job — **this clause is unaffected and stands** — and branch protection's required-status-check list (T-001/DEC-021) was left unchanged — **this clause was reversed by DEC-042 below (QA remediation of PR #7); see there** | A diff-based check has nothing meaningful to compare against outside a pull-request's base/head — on `main` itself, or in a local `make check` run with no reliable base ref, it would either need a network-fetched `origin/main` (which may be stale or absent) or trivially no-op, neither of which is a real per-commit gate. This mirrors how `licenses` and `build-all` (T-005/T-006) already stand apart from `check` for their own, different reasons. A `make check-hostnames` convenience target was still added for local use (`HOSTNAME_BASE_REF ?= origin/main`, overridable) since a contributor may want to run it before pushing. Branch protection was not touched because this task's acceptance criteria don't ask for it and QA review (not required-status enforcement) is what actually gates every merge here per AGENT.md §10; `indexer-hostnames` reports as an ordinary, non-required PR check for now, same as `build-all`/`licenses` — **QA (PR #7) found this reasoning didn't hold: merges here happen via `gh pr merge --auto`, which only waits on *required* checks, so "QA review gates the merge" and "the check fails the PR" are not equivalent, and the acceptance criterion asks for the latter. See DEC-042** | T-007 |
| DEC-042 | 2026-09-13 | **QA remediation of PR #7, same day.** Branch protection's `required_status_checks.contexts` on `main` now includes `check indexer hostname allowlist (T-007)` alongside the three existing `make check (<os>)` contexts (`strict: true` preserved; `required_pull_request_reviews` still not set) | This is the fix for DEC-041's reversed clause above. QA verified live that a red `indexer-hostnames` run blocked nothing, since `gh pr merge --auto` only waits on required checks and this context wasn't one. Before adding it, the exact registered context string was confirmed from a real completed check run on this PR's own head commit (`gh api repos/kdta91/tortui/commits/<head-sha>/check-runs`: `"name":"check indexer hostname allowlist (T-007)"`, `"conclusion":"success"`, run `event: pull_request`) rather than assumed from the workflow YAML's job `name:` field, since GitHub's actual registered context can differ from that (e.g. under a matrix). Applied via `gh api -X PUT repos/kdta91/tortui/branches/main/protection` with the full protection object — the `.../required_status_checks` sub-resource PUT 404s for this repo for reasons not fully diagnosed in the time available — carrying forward every other field's existing value explicitly (`strict`, `allow_force_pushes`, `allow_deletions`, `required_linear_history`, `block_creations`, `required_conversation_resolution`, `lock_branch`, `allow_fork_syncing`) rather than omitting them, because a first attempt that omitted `allow_force_pushes` silently reset it to `false`; caught by reading protection back and diffing against the pre-change GET before treating the change as done. Deadlock check: `indexer-hostnames` only runs `if: github.event_name == 'pull_request'`, and every push to this PR's branch triggers both a `push` event (job reports `skipping`, harmless) and a `pull_request` synchronize event (job actually runs) — confirmed directly for this PR's current head commit, which already has one of each, with the `pull_request`-triggered run reporting `success` — so requiring this context does not strand the PR on a context that never reports for a `pull_request` event. Read-back proof is in the PR description/remediation report, not duplicated here | T-007 |
| DEC-043 | 2026-09-13 | `internal/indexer/indexer.go` declares a bare `type Category int` with no constants and no methods, rather than T-010 creating `category.go`, inlining a placeholder `CategoryOther`, or changing `Query`/`Result` to avoid the type | AGENT.md §5 freezes `Query.Categories []Category` and `Result.Category Category`, so the type must exist for T-010's file to compile, but the taxonomy itself — the enum values, the `CategoryOther` zero value, and the Torznab-numeric/adapter-string mapping helpers — is T-011's acceptance criteria and T-011's file (`internal/indexer/category.go`). Three options were weighed: (a) create `category.go` here with the full enum, which is building ahead into another task and would leave T-011 with nothing to do; (b) declare a placeholder `CategoryOther Category = iota` here, which pre-commits the zero value that T-011's own criteria are supposed to define and would leave a stray constant in the wrong file if T-011 chose differently; (c) declare only the type. (c) was chosen as the literal minimum: it adds three lines, claims no zero value, and leaves every T-011 decision open. The type's godoc names `category.go` and T-011 explicitly, and states that T-011 may either add its `const` block in `category.go` against this declaration (legal Go — same package) or relocate the declaration into `category.go`, and that such a move is a within-package relocation that changes no contract and needs no `DEC-` entry of its own. This row exists so T-011 does not mistake the bare type for a frozen §5 contract: §5 references `Category` but never defines it, so its definition is not frozen by this task | T-010, T-011 |
| DEC-044 | 2026-09-13 | `Trust.String()` returns lowercase tokens (`unknown`/`none`/`verified`/`trusted`/`vip`) while `Trust.Badge()` returns the display cells (`VIP`/`TR`/`✓`/`""`); `Badge()` returns the Unicode `✓` with no ASCII fallback, and both methods return a safe value for an out-of-range `Trust` rather than panicking | The two methods have different jobs and AGENT.md pins only one of them. §7 pins `Badge()` exactly: `VIP` / `TR` / `✓` / blank, "a compact badge, not a word", with blank covering both `TrustUnknown` and `TrustNone` since neither is worth a column cell. `String()` is unpinned, so it was made the *identifier* form instead of a second display form — case-uniform lowercase so it is stable to write in a log line, assert on in a test, and later parse in a filter expression, with no per-value casing exception for VIP that a parser would have to special-case. Out-of-range handling differs on purpose: `String()` returns `trust(N)` because a surprising value in a log should be diagnosable, `Badge()` returns `""` because a surprising value in a fixed-width table column must not widen it. On the ASCII fallback: AGENT.md §14 requires `✓` to have an ASCII substitute chosen by terminal-capability detection and forceable with `--ascii`, but that detection lives in the TUI theme layer, so `Badge()` deliberately stays capability-unaware and returns the Unicode form — putting the fallback here would mean this package reading terminal state, which would also make `Badge()` impure and untestable without an environment | T-010, T-050 |
| DEC-045 | 2026-09-13 | `Result.Validate()` validates only that at least one of `Magnet`/`TorrentURL` is set, treats a whitespace-only value as unset, and reports failure by wrapping an exported sentinel `ErrNoLink` inside `fmt.Errorf("indexer %q: result %q: %w", ...)` | The acceptance criterion names exactly one rejection and adding more was considered and rejected: every other `Result` field is legitimately empty for some source (a source may publish no size, no date, no uploader, no details page, and `InfoHash` is explicitly allowed to be empty until `Resolve` by §5's own comment), so a stricter `Validate` would discard results that are perfectly displayable, and it would do so inside a frozen contract that every future adapter has to satisfy. Whitespace-only is treated as unset because a `Magnet` of `"   "` is not a link by any reading of "has neither" and would otherwise pass a bare `!= ""` check straight through to the engine. The sentinel plus `%w` wrapping is what lets a caller branch on the condition with `errors.Is` rather than by string-matching the message, while still satisfying AGENT.md §6.9's requirement that errors name their context — the ids are `%q`-quoted so an empty `IndexerID` on a malformed result reads as `""` rather than producing a mangled message. Documented on the method: a result that `Search` returns unresolved will not pass `Validate`, so callers validate after `Resolve`, not before | T-010 |
| DEC-046 | 2026-09-13 | **Corrected 2026-09-13 (same day, on QA remediation of PR #9) — the tripwire sentence in this column was false and is rewritten in place, matching how DEC-022, DEC-028 and DEC-040 were corrected.** The `Category` enum is `CategoryOther` (zero value) plus six buckets named for the kind of DATA a torrent carries — `Audio`, `Video`, `Image`, `Text`, `Software`, `Data` — and no bucket names subject matter (no genre, medium, scene tag, or kind of material). `TestCategoryBucketSetIsClosed` is the tripwire on that closed set, and it reads the bucket set **off the implementation** rather than off a second list kept in the test file: it parses the `Category` iota const block out of the package's own non-test source with `go/parser`, then compares the declared constant identifiers, and the tokens `Category.String` renders those constants as, against the `wantNames`/`wantTokens` lists in the test — the only hand-maintained expected values left. A bucket **added** to `category.go` therefore fails the build whether or not a `String` case was added with it; so do a removal (also a compile error), a constant-identifier rename (also a compile error), and a `String` token rename | AGENT.md §2 bars content-specific categorisation outright and §16 explains that this is the project's legal posture, not a style preference: a bucket list that told a reader what kind of media the app is for would be the curated-front-end evidence §16 describes. Data kind is the classification that survives that rule while still being useful for a coarse search filter and a one-word table column. Four of the six names (`audio`, `image`, `text`, `video`) are also IANA top-level media type names — the registry at https://www.iana.org/assignments/media-types/media-types.xhtml was fetched and read on 2026-09-13 for this row, and its top-level sections are `application`, `audio`, `example`, `font`, `haptics`, `image`, `message`, `model`, `multipart`, `text`, `video`. `Software` and `Data` are deliberately NOT IANA names: IANA's `application` is a catch-all that would swallow both, and the sources §2 lets tortui bundle (public archives, research dataset repositories, distro release listings) are precisely software and datasets, so collapsing them into one bucket would make the filter useless for the only sources that ship enabled. Six was chosen as the smallest set that keeps those two distinct and still covers what any source publishes. **What was false:** the original version of this row said `TestCategoryBucketSetIsClosed` "pins the exact seven tokens so a future edit cannot slip one in unnoticed." It did not. As first written, the test compared `allCategories` — a hand-maintained slice in `category_test.go` — against a `want` list in the *same* file, so both sides of the comparison were test-local and agreed with each other no matter what `category.go` declared. PR #9 QA (kdta91, 2026-09-13) proved it by adding a content-specific bucket to `category.go` plus its `String()` case, touching nothing in the test file: `make check` stayed fully green. The guard did catch removal and both kinds of rename (all three are compile errors or token divergence), but **addition** — the one case the sentence promised and the only §2 violation this tripwire exists to stop — passed silently. Corrected by deriving the bucket set from the package source with `go/parser` as described in the decision column, and re-proved by re-running QA's exact bypass against the fixed test: it now fails, with the added bucket named in the failure output. The closed-set design itself was not the problem and is kept — a list of forbidden content words would put the very words §2 keeps out of this repository into a test file | T-011, T-021, T-022, T-040 |
| DEC-047 | 2026-09-13 | `CategoryFromTorznab` maps by 1000-block (`id/1000`) and the code contains the block NUMBERS only — never the block names the Torznab sources give them. Blocks 0 and 8 both map to `CategoryOther`; every unmapped block, including the reserved and site-specific custom ranges, negatives, and extreme ints, also returns `CategoryOther` | The block numbers were verified in this session on 2026-09-13, not recalled: the newznab API specification section 3 'Predefined Categories' (`docs/newznab_api_specification.txt` in the `nZEDb/nZEDb` repository on GitHub, branch `dev`) was downloaded and its range table read directly (0000-0999, 1000-1999 through 7000-7999, then 8000-99999 reserved and 100000- site-specific custom), and `src/Jackett.Common/Models/TorznabCatType.cs` in the `Jackett/Jackett` repository on GitHub, branch `master`, was downloaded and its parent-category list read directly (1000/2000/3000/4000/5000/6000/7000 identical to the spec, plus 8000 used as Jackett's own catch-all where the spec leaves that range reserved). Handling both 0 and 8 as `CategoryOther` is what makes the helper correct against both conventions. Block granularity rather than sub-id granularity because the block already determines the kind of data and the sub-id only refines the source's own subject-matter labelling, which tortui has no use for. Writing the block names into the table was considered and rejected: they are subject-matter labels, and importing them would put exactly the content-specific vocabulary AGENT.md §2 excludes into this repository, so each block is recorded as the data kind its items are and the two citations above carry the provenance | T-011, T-021 |
| DEC-048 | 2026-09-13 | `CategoryFromString` lowercases the label, splits it on every rune that is not a letter or digit, and returns the bucket for the first token found in a fixed word table; a token must match a whole word (`audiophile` does not match `audio`), and an all-digit label is NOT routed to `CategoryFromTorznab`. The table holds the canonical `String()` tokens plus a short list of the everyday words sources label with, several of which do name subject matter | Adapter labels are arbitrary source text (`Movies/HD`, `PC > Games`, `[Books]`, padded, mixed case, non-ASCII, invalid UTF-8), so tokenising and matching whole words is the only rule that stays predictable across them, and first-match-wins gives a deterministic answer for a multi-part label. Substring matching was rejected as it produces silent false positives. Auto-routing numeric strings was rejected because it would make a source whose category is literally named `8` mean something surprising, and an adapter holding numeric ids should call the numeric helper. Keeping subject-matter words as table KEYS is not a content-specific category (AGENT.md §2): the buckets they map to are all data kinds, so the table is precisely where a source's own taxonomy is discarded rather than adopted — the alternative, recognising only tortui's own seven tokens, would leave nearly every real label falling to `CategoryOther` and make the helper pointless. The list is kept short and generic on purpose and the godoc says it is not a place to accumulate genre words, format names, or scene tags | T-011, T-022 |
| DEC-049 | 2026-09-13 | The bare `type Category int` declared by T-010 was relocated from `internal/indexer/indexer.go` into `internal/indexer/category.go` rather than left in place with the `const` block added beside it | DEC-043 explicitly offered both options and recorded that a within-package move changes no contract and needs no `DEC-` row of its own; this row exists only because the move is visible in the diff to a file that holds frozen contracts, and a reviewer should not have to guess whether §5 was touched. It was not: the six frozen §5 types (`Indexer`, `Query`, `Result`, `Caps`, `Trust`, `Mode`) are unchanged, and the change to `indexer.go` is a pure deletion of 15 lines with 0 added (`git show --stat`). `Category` is referenced by §5 but never defined there, so its definition was never frozen. Relocating was preferred over adding constants remotely because the T-010 godoc on the declaration stated that the enum 'has not landed yet' and named T-011 as its owner — text that becomes false the moment this task lands, so the doc comment had to be rewritten regardless, and a type whose values, `String()`, and both mapping helpers all live in `category.go` belongs in that file | T-010, T-011 |
| DEC-050 | 2026-09-13 | **Corrected 2026-09-13 (same day, on QA remediation round 2 of PR #9) — the scan-coverage sentence in the rationale column below was false and is rewritten in place, matching how DEC-022, DEC-028, DEC-040 and DEC-046 were corrected.** **QA remediation of PR #9.** `TestCategoryBucketSetIsClosed` derives the bucket set by parsing the `Category` iota const block out of the package's own non-test `.go` files with `go/parser` (`categoryConstNames`), rather than by QA's suggested walk of `Category.String` until it returns the `category(N)` sentinel. The `String` walk is kept as well — it is what now builds `allCategories`, so the other tests in the file check against buckets read off the implementation too — but it is a cross-check, not the primary derivation | Both approaches fix the reported defect for the case QA reproduced, and the walk is much the simpler of the two. The walk alone leaves one hole, verified by experiment rather than argued: a constant added to the enum **without** a matching `String()` case makes `String` return the sentinel at that value, so the walk stops *before* the new bucket and reports the same seven tokens as before — green. Go does not require a `switch` over a named integer type to be exhaustive and no linter in this repo's `.golangci.yml` enforces exhaustiveness, so that is an ordinary edit, not a contrived one; the bucket is real, reachable, and settable on `Result.Category` whether or not `String` knows its name. Parsing the declaration closes it, because the const block is the one place a bucket cannot be added without appearing. Cost accepted: the test now depends on the *shape* of the declaration, so it fails loudly (`t.Fatalf`, not a skip) on any const block of type `Category` it cannot read unambiguously — a multi-name spec, an explicit value part-way down the iota run, or a second `Category` iota block in another file (iota restarts per block, so declaration order would no longer give the values). A guard that silently ignored a shape it did not understand would be the same class of defect as the one being fixed here. The scan covers every non-test `.go` file in the package directory, not just `category.go`, and a second pass over those same files fails the test on any constant or variable **written with the type `Category`** that the iota block did not declare — so the enum cannot be extended from a neighbouring file by any declaration that spells the type. It can still be extended by a declaration that does not spell it: see DEC-051, which records exactly which shapes stay open and why. All five cases were re-proved by experiment on the fixed test — bucket added with a `String` case, bucket added without one, bucket removed, constant renamed, `String` token renamed — and every one of them is red or a build failure; `internal/indexer` coverage stays at 100.0% since none of this is production code. **What was false:** the original version of this row said "the scan covers every non-test `.go` file in the package directory, not just `category.go`, so **the enum cannot be extended from a neighbouring file**." It could. As first written, the scan discriminated const blocks on `gd.Specs[0].Type` being `Category` and skipped the block otherwise, so a `Category` constant that was not the *first* spec of its block was never looked at, and `var` declarations were never looked at at all. PR #9 QA round 2 (kdta91, 2026-09-13) proved it with a single new file in the package — `const ( CategoryProbeDoc = "probe"; CategoryProbeBucket Category = 7 )` plus an `init` adding it to `categoryWords`, with `category.go` and `category_test.go` both untouched — giving a reachable, content-specific bucket with `go test` `ok` and `golangci-lint` at `0 issues.` That bypass was reproduced on the shipped code before being fixed, and the fix (the second pass described in the decision column) turns it red; the same three attack shapes QA reported — a mixed const block, a `var`, and the neighbouring-file variant — are all red now, and the five cases above are still red. The absolute wording is gone: the row now says what the syntactic match actually reaches, and DEC-051 states the shapes it does not | T-011 |
| DEC-051 | 2026-09-13 | **QA remediation round 2 of PR #9.** `categoryConstNames` gains a second pass that sweeps every `const` and `var` declaration in every non-test file of the package — including ones nested inside a function body — and `t.Fatalf`s on any name declared with the type `Category` that the iota block did not already declare. The match is **syntactic**: the type must be written as the bare identifier `Category`. Closing the remaining shapes with `go/types` was considered and rejected | This is the fix for the false sentence corrected in DEC-050, and it is the remedy PR #9 QA named as preferred. The alternative was prose-only — scope the claim and file the hardening as backlog — but the guard is a §2 backstop and the shapes QA demonstrated (a `Category` const that is not the first spec of its block, a `var`, and either of those in a neighbouring file) are ordinary Go that a future task could write without meaning anything by it, so closing them is worth ~40 lines of test code. **What stays open, stated rather than implied.** A syntactic match cannot see a declaration that never writes the word `Category`, and three shapes were probed and confirmed still green: a const typed through an alias (`type c = Category; const X c = 7`); an untyped const whose value is a conversion (`const X = Category(7)`); and a bare `Category(7)` used inline with no declaration behind it at all. The last of those has no declaration for any parser to read, so no amount of AST work closes it. Closing the first two would need a full type check — `go/types` with an importer over the parsed package — and that was rejected on portability: it makes a unit test depend on export data being present for every import, it behaves differently under `go test -c` and under a cold build cache, and the three CI legs (ubuntu/macOS/Windows) cannot be verified from the macOS dev machine, so a flaky guard would be traded for a leaky one. A value-shape sweep that flagged any `Category(...)` conversion in a declaration's initialiser was also rejected: it would fire on legitimate future code such as `var zero = Category(0)`, and it still would not reach the inline case. **Calibration.** This is a backstop against an accidental or unnoticed addition, not a security boundary — the test lives in the repository and anyone deliberately adding a bucket can edit it. Its value is that the ordinary ways of adding one all fail loudly with a message naming the rule. The godoc on `categoryConstNames` states these same limits at the code, so a reader of the test is told what it does not cover without having to find this row | T-011 |

Append a row whenever you make a choice a future reader would question. Empty date means
inherited from the initial plan.

---

## Blocked

*(empty — append `T-0NN` blocks here with the exact input needed to unblock)*
