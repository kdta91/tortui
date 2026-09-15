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
- *Parses `caps` into `Caps`; handles servers that omit it* — `caps.go` reads `<searching>`,
  `<limits>` and `<categories>` into Search, Pagination and Categories.
  `TestDiscoverHandlesAServerThatOmitsCaps` covers seven ways a server can fail to publish one
  (404, 500, an empty body, an HTML page, truncated XML, a feed in place of caps, an `<error>`
  document): each returns the **fail-closed baseline caps and a usable adapter** alongside an
  error wrapping `ErrCapsUnavailable`, and each case then runs a successful search through that
  adapter to prove it. See DEC-069 for why the adapter comes back with the error.
- *`ModeLatest` probed, not assumed* — see "The caps probe" below and DEC-067.
- *Maps `seeders`, `peers`, `size`, `pubDate`, `category`, `magneturl`, `infohash`* — plus
  `leechers`, `uploader`/`poster`, the trust attributes, and the plain `<size>`, `<files>`,
  `<grabs>` elements Jackett writes outside the torznab namespace. `search-full.xml` asserts
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
  page, a caps document, an unknown root, a nested `<rss>`, and 60,000 levels of nesting counted
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
| `Magnet` | **Passed through** — the `magneturl` attribute (or a magnet `<link>`) exactly as published, `dn=` included. The magnet `Resolve` derives for an item that published none is **not clean either**: `magnetFor` writes `Result.Title` into its `dn=`, `url.QueryEscape` encodes that text rather than removing it, and an opaque token survives the encoding unchanged — so a derived magnet carries whatever the title carries. Reproduced by QA on round 3 and pinned by `TestResolveDerivesAMagnetThatInheritsTheTitlesGap`. |
| `TorrentURL` | The source's download URL verbatim — safe, `internal/logging` masks on the name. |
| `SizeBytes`, `Seeders`, `Leechers` | Derived — parsed integers (`Leechers` also `peers - seeders`, DEC-065). |
| `Category`, `Trust` | Derived — enum values. |
| `Published` | Derived — a parsed `time.Time`. |
| `Uploader` | **Passed through**, except that a value containing `://` is dropped. The refusal keeps a link out; it cannot keep an opaque token out. |
| `SourceURL` | The source's page URL verbatim — safe, masked on the name. |
| `Extra` | Derived — fixed `torznab.`-prefixed keys, values that parse as a number only. |

So the fields that carry source text under a name `internal/logging` does **not** mask are,
exhaustively: `Title`, `Magnet`, `Uploader`, and `ID` on every branch but the infohash. A source
that echoes the user's key into a `<title>`, a magnet's `dn=`, an `<attr name="uploader">` or a
`<guid isPermaLink="false">` puts it in that field verbatim — and into `Magnet` a second time
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
test run, and the mutation reverted: naming the result in `Resolve`'s error, keeping the `<error>`
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

### T-022 · Scraper adapter framework
```
status: blocked
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
- `T-920` Share an in-flight fetch between concurrent fan-outs (single-flight per cache key). As
  built in T-012 the per-source minimum refresh interval is claimed before the request, so two
  concurrent *distinct* queries reaching one source inside the interval get one fetch and one
  `ErrThrottled` skip. Pinned by `TestSearchAllConcurrentCallsShareTheCache` and documented in
  DEC-054; harmless while the TUI issues one search at a time, worth closing before anything
  issues two.
- `T-921` `SearchAll` does not tell the caller which sources answered from cache. T-061's
  acceptance criteria require the status bar to show "when results came from cache rather than a
  fresh fetch", and the T-012 return shape (`[]Result`, `[]SourceError`, `error`) has nowhere to
  put it. Decide the shape when T-061 lands rather than guessing now.
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
  (Go's `encoding/xml` accepts an undeclared prefix and this adapter matches `<attr>` by local
  name regardless of namespace, both asserted by
  `TestAttrElementParsesUnderAnyNamespacePrefix`) but does make the fixtures slightly less
  faithful to the wire. Skipping the quoted value of an attribute literally named `xmlns` or
  `xmlns:<prefix>`, and nothing else on the line, would fix it. Deliberately **not** done inside
  T-021: it is a change to a §2 safety gate, and one belongs in its own reviewed task rather than
  as a side effect of an adapter. Every scraper fixture in T-022 will hit the same wall. Found
  while building T-021.

- `T-934` `indexer.Result` has no safe logging path. `Title`, `Magnet`, `Uploader` and `ID` — on
  the branches where it falls back to a non-URL guid, comments, link or the title — all carry
  source-controlled free text under key names `internal/logging` does not mask
  (`sensitiveKeySubstrings` is `url, apikey, cookie, token, secret, password, passkey,
  authorization`), and an opaque credential has no value shape `maskText` recognises. So logging
  a whole `Result` writes attacker-controlled text to the log file in plaintext, including an
  api_key a hostile or broken source echoed back into a `<title>`, a magnet's `dn=`, an
  `<attr name="uploader">` or a guid. It is not a Torznab problem: every adapter from T-022 on
  produces the same frozen §5 type, and the leak is in the type's relationship to the logger
  rather than in any adapter. A real fix is a `LogValue()` on `Result` that renders the risky
  fields under masked names, or a registry-level rule that a result is only ever logged under a
  masked key; either touches the frozen §5 contract, so it needs its own `DEC-` and a note on
  every affected task (AGENT.md §5, §12). Found by QA on T-021 (PR #12), rounds 1 and 2;
  disclosed rather than fixed there, per DEC-066 and DEC-071.
- `T-935` `Adapter.Search` does not consult `Caps.Search`, so a source that declared
  `<search available="no"/>` still receives a request from a direct caller. The registry already
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
| DEC-052 | 2026-09-13 | `SourceError` is one struct carrying both outcomes — `IndexerID`, `Err`, and a `Skipped` bool — rather than two return slices or a skip reported as an ordinary error. It unwraps to a sentinel (`ErrUnsupportedMode`, `ErrThrottled`, `ErrUnknownIndexer`, `ErrSourcePanic`, or the adapter's own error) and its `Error()` names the source and says whether it failed or was skipped | The T-012 acceptance criteria fix the return shape at `([]Result, []SourceError, error)`, so a separate `[]Skipped` return was not available; and AGENT.md §6.3 requires a source that lacks a needed capability to be reported as skipped *and* to not count as a failure, so collapsing it into a plain error was not available either. A bool plus a sentinel keeps both facts on one value: callers that only want to count failures read `Skipped`, callers that want the reason use `errors.Is`. The alternative considered was a `Kind` enum (failed/unsupported/throttled/unknown); it was rejected because it duplicates information the sentinel already carries and would need extending every time a new skip reason appears, whereas a new sentinel does not change the type. An unknown indexer id is deliberately NOT a skip: it is a caller error, so it counts towards the all-failed condition | T-012 |
| DEC-053 | 2026-09-13 | `SearchAll` returns a non-nil error in exactly two cases: `ErrAllSourcesFailed` when at least one source failed and none succeeded, and `ErrNoSources` when the selection was empty (empty registry, or every source disabled). Skips count as neither success nor failure, so all-skipped is a nil error and skipped-plus-failed is `ErrAllSourcesFailed` | The criterion says the error is non-nil only when every source failed, and the three skip edge cases it does not spell out have to resolve somewhere. Treating a skip as neutral is the only reading consistent with §6.3's 'skipped and noted, not treated as a failure': counting it as a success would suppress a genuine all-failed report, and counting it as a failure is what §6.3 forbids. `ErrNoSources` is the one place this goes beyond the literal wording, and it is deliberate: with nothing queried there is no partial success to protect, and returning `(nil, nil, nil)` would render in the TUI as 'no results found' when the truth is 'you have no sources enabled' — a different message and a different fix for the user. It is a distinct sentinel precisely so a caller can tell the two apart, and the godoc on `SearchAll` states both cases. The alternative — nil error and an empty result set — was rejected for that reason | T-012, T-061 |
| DEC-054 | 2026-09-13 | The per-source minimum refresh interval (default 1s) is enforced by *skipping* the source with `ErrThrottled`, and the slot is claimed under the registry lock **before** the request is made, not after it completes. A cache hit never claims a slot | Claiming before rather than after is what makes the floor hold under concurrency: two fan-outs that both check a stale timestamp would both fetch. Skipping rather than waiting was chosen over two alternatives. (1) Waiting out the remainder of the interval, bounded by the context: it needs the slot reserved for a future instant and released again if the caller gives up, and an abandoned reservation leaves a phantom slot blocking a source that is idle — real complexity for a case the cache already covers. (2) Serving a stale cache entry when throttled: dead code by construction, because with the TTL (60s) longer than the floor (1s), a query whose entry is inside the floor is necessarily still inside the TTL and was already served from cache. The default is 1s rather than something larger precisely because the floor only ever bites on a *different* query within the interval: a user retyping a search cannot go faster than that by hand, but a loop can. The known cost is that two concurrent distinct queries against one source skip one of them, which is pinned by a test and filed as `T-920` rather than left to be discovered | T-012, T-061 |
| DEC-055 | 2026-09-13 | Each source's `Search` runs on a goroutine of its own with the answer handed back over a **buffered** channel, and that goroutine recovers a panicking adapter and turns it into that source's error (`ErrSourcePanic`) | Two things §6.2 and §6.3 ask for cannot be had from a direct call. An adapter that ignores its context would otherwise hold the whole fan-out open past its deadline, and Go offers no way to abandon a blocked call in place — so the deadline is honoured by ceasing to wait, not by killing anything, and the buffer is what keeps that honest: a late answer is delivered into the buffer and discarded, so the adapter's goroutine finishes instead of parking forever on a send nobody will receive. That distinction is not theoretical — with an unbuffered channel the goroutine leaks, and the first version of the timeout test did not catch it (it watched the fake's own call return, which still happens); the test now polls the goroutine dump for a goroutine parked in `callSearch`, and is red with the buffer removed. Recovering the panic is not a violation of §6.9: that rule bars *raising* a panic outside `main`, while §6.3 requires one broken source not to crash the application, and an adapter is exactly the untrusted code that might. The recover sits on the goroutine that can see the panic, and the recovered value is included in the error text | T-012, T-021, T-022 |
| DEC-056 | 2026-09-13 | Dedup identity is the infohash (trimmed, lowercased) when present, else the normalised title (lowercased, every run of non-letter/non-digit collapsed to one space) plus `SizeBytes`. A result with neither an infohash nor a title containing a letter or digit is never merged with anything. The survivor is the copy with the most seeders; contributing ids are joined into `Result.Extra` under the exported key `tortui.sources`, always set, in the order the sources were queried | Sources differ in separators, bracketing and case far more often than they differ in words, so normalising those is what makes a title match at all; including the size keeps two genuinely different items with the same name apart. The 'no identity, no merge' rule exists because the empty key is otherwise shared by every unidentifiable row, which would fold unrelated results from different sources into one — the degenerate case is given a per-row unique key instead. Highest-seeders-wins is the criterion's own rule and is also the copy most likely to resolve into a live swarm. The key is exported (`ExtraKeySources`) rather than a bare string so the TUI has one name to read, and it is always set — including for a result only one source produced — so consumers have a single code path; the registry overwrites whatever an adapter put there, which is stated in the godoc. §5 says nothing outside an adapter may *depend on* a particular `Extra` key and no core logic may *branch* on one; writing a display-only annotation is neither, and the criterion requires the ids to be recorded in `Extra` specifically | T-012, T-061 |
| DEC-057 | 2026-09-13 | `SetEnabled(id, bool)` was added alongside the four methods the criteria name, and naming ids explicitly in `SearchAll` queries those sources whether or not they are enabled | `Enabled()` is unanswerable without something that sets enablement — with no setter it would be a permanent synonym for `List()` — and `internal/config`'s `Indexer.Enabled` field — merged in T-002 (`internal/config/config.go`) with the comment 'controls whether the registry queries this source' — shows the flag is expected to exist. It is the minimum addition: no removal, no reordering, and a disabled source keeps its place in `List()` and its registration order. Explicit ids overriding the flag is the other half of the same decision: the enabled set is the *default* fan-out, and a caller that names sources (the search screen's multi-select, T-060) has made the more specific statement. The alternative — silently dropping a named-but-disabled source — would give the TUI a selection whose result depends on invisible state | T-012, T-002, T-060 |
| DEC-058 | 2026-09-14 | Only 429 and 5xx are retried. Every other 4xx is permanent, **408 Request Timeout included**, and a transport-level failure (dial refused, connection reset, read timeout) is not retried either | T-020's criterion is "on 429/5xx only; never on 4xx", and 408 is the one 4xx where a retry is arguably useful, so the choice had to be made explicitly rather than left to fall out of the code. Retrying it would be an unwritten third case, which is exactly the two-reasonable-implementations-differ situation AGENT.md §12 says not to guess at; the criterion carves out 429 by name and does not carve out 408, so 408 stays permanent. The same "only" is why a transport error is not retried: a dial failure against a source the user configured is nearly always an outage, a typo in their URL, or DNS, and none of those improve within one backoff — meanwhile §6.3 already has the registry degrade gracefully around a source that failed, and §6.13 would rather tortui not re-dial a struggling server three more times. `StatusError.Retryable()` exposes the rule so an adapter never has to re-derive it | T-020, T-021, T-022 |
| DEC-059 | 2026-09-14 | **Corrected 2026-09-14 (QA remediation of PR #11) — the "ends the attempt loop" claim in this column was false for one class of value and the row is rewritten in place, matching how DEC-040, DEC-046 and DEC-050 were corrected.** `Retry-After` in either RFC 9110 form beats the backoff schedule when it parses. Malformed is treated as no advice (fall back to backoff), a past HTTP-date or a non-positive delta is treated as "now" (zero delay, still one retry), and a value longer than `MaxRetryAfter` (default 30s) **ends the attempt loop** rather than being capped and retried or slept out, and a delta-seconds value too large for a `time.Duration` **saturates** at the longest representable duration instead of wrapping, so it lands on that same rule rather than under it | Three of the four cases have an obvious answer; the long one does not, and it is the one a real throttled source produces. Capping an hour-long `Retry-After` to 30s and retrying anyway is precisely the hammering §6.13 forbids — the server said when to come back and tortui would be ignoring it. Sleeping the full hour inside a search is not a retry either: the caller is a TUI search with a per-source timeout (`indexer.DefaultSearchTimeout`, 15s), so it would be cancelled long before, having reported nothing useful. Stopping reports the truth immediately, and the duration the server asked for is preserved on the `StatusError` (`RetryAfter`, `RetryAfterSet`) so the TUI can say "this source asked us to wait an hour" rather than "this source failed". `RetryAfterSet` exists as a separate bool because a zero duration that was sent and a zero duration that was not are different facts. Separately, no delay is ever slept if it would outlast the context deadline: the client returns the status error rather than sleeping into a cancellation, which turns a diagnosable 503 into a bare timeout **What was false:** as shipped in round 1 the rule held only up to ~9.22e9 seconds. `parseRetryAfter` computed `time.Duration(secs) * time.Second` with no range check, so `Retry-After: 31536000000` wrapped to `-1488191h9m7s`; `retryDelay` compared that negative against `MaxRetryAfter`, found it *shorter*, and returned it as valid advice, and `Clock.Sleep` on a negative duration returns immediately. A server answering 429 with an absurd delta therefore got `MaxAttempts` requests back to back with no delay at all — the exact opposite of what this row claimed, and the hammering §6.13 forbids. QA (PR #11, D2) reproduced it; the 12-case table missed it because its "absurdly large" value, 31536000, sits just under the overflow boundary. Corrected: an out-of-range delta saturates at `maxDuration`, a `strconv.ErrRange` parse is treated as an enormous value rather than as malformed (so a run of digits too long for an int64 saturates too, instead of falling back to backoff), and `retryDelay` clamps any negative `RetryAfter` to zero as a second line of defence. Saturating rather than rejecting is deliberate: rejecting would mean "no advice given", which falls back to backoff and retries a server that just asked to be left alone for a year. Six table cases now sit at the boundary (`9223372036`), one past it (`9223372037`), absurdly past it, past int64 in both directions, and on an HTTP-date beyond the duration ceiling, plus `TestAbsurdRetryAfterStopsInsteadOfRetryingWithNoDelay` end to end; both were proven red against the unfixed parse and green against the fixed one | T-020, T-061 |
| DEC-060 | 2026-09-14 | The per-host rate limiter is ~60 lines of `sync.Mutex` + `map[string]time.Time` in `ratelimit.go`. No dependency was added — in particular not `golang.org/x/time/rate`, whose source was **not** read, so nothing here is claimed about its API or behaviour | The decision rests on the requirement and on AGENT.md §3, not on a comparison with a library this agent did not open. What T-020 needs is a minimum interval between requests to one host, with the wait interruptible by context and the time source injectable so the backoff and spacing tests are deterministic rather than wall-clock-timed — that is a map, a mutex, and a `Sleep` behind the `Clock` interface, all of which this package needs anyway for the retry path. Against that, §3 locks the stack and any addition costs a license verification, a `NOTICE` regeneration, and a new module in the dependency graph for every platform `make licenses` checks. A dependency justified by "probably fits" and an unread LICENSE file is the shape of mistake T-011's QA already caught once in this repo, so the honest options were "read the library first" or "don't claim anything about it"; the second was cheaper than the first for sixty lines. If a future task needs real token-bucket burst behaviour, that is the moment to open the library, read its LICENSE, and file a DEC row that can actually justify it | T-020 |
| DEC-061 | 2026-09-14 | Credential safety in `httpx` is structural, not incidental: credential fields are named so `internal/logging`'s key-name rule fires on them (`APIKey`, `CookieHeader`), `Credentials` implements `slog.LogValuer`, no error message ever contains a URL / query string / header / body excerpt, and `unwrapURLError` strips every `*url.Error` out of the cause chain before it is stored | T-003's own package doc states the gap this closes: masking by key name and value shape cannot catch an opaque credential logged under an unremarkable key, and every tortui source is user-supplied, so an api_key has no fixed shape to match. `httpx` is the first package to hold one. Layer by layer: the field names are the only thing that makes the *one* guarantee that does not depend on guessing a value's shape apply, which is why the godoc warns against renaming them; `LogValue` catches a `Credentials` logged whole; the structural rule means there is nothing to mask in the first place; and the `*url.Error` strip exists because `net/http` returns one from essentially every failed request and its `Error()` prints the entire URL — with the api_key in the query string — so leaving it reachable would put a plaintext credential one `fmt.Errorf("%v", cause)` away from the user's log file, however careful this package's own strings were. Dropping that layer costs `errors.As(err, &urlErr)`, which no caller needs, and keeps `errors.Is` working for `context.DeadlineExceeded` and `net.Error`. A `scrub` of the configured values over the remaining text is the backstop, with no minimum length: a short credential garbling a message is a better outcome than a leaked one. Cost accepted: an error names only the method and `host:port`, so a URL-level mistake (wrong path, wrong parameter) is diagnosed from the config screen rather than the error text — the alternative is a log file with a working credential in it | T-020, T-021, T-022, T-003 |
| DEC-062 | 2026-09-14 | **Corrected twice on 2026-09-14 (QA remediation of PR #11, rounds 1 and 2) — round 1: this row described the check as host-only and said same-host redirects are simply followed, which left a hole the row's own rationale argues against. Round 2: the round-1 correction itself asserted, falsely, that `net/http` strips the `Cookie` header across a scheme change. Both are rewritten in place, matching how DEC-040, DEC-046 and DEC-050 were corrected.** A redirect that leaves the host the request was addressed to is refused (`ErrCrossHostRedirect`), not followed. A same-host redirect that drops from `https` to `http` is refused too (`ErrInsecureRedirect`). A same-host `http` to `https` upgrade is followed, as is any same-scheme hop; the chain is bounded at five hops | Not in T-020's criteria, and added anyway because the credential design makes it load-bearing: the api_key travels in the query string, so following a redirect off-host hands the user's own credential to a server they never configured, and `net/http` only strips *header*-borne credentials on a cross-host redirect, not query parameters. Refusing is also the conservative reading of §2 — tortui talks to exactly the sources the user configured. The cost is a source that legitimately redirects to a different hostname (a CDN, an apex-to-www move) failing until the user updates the URL, which is visible and fixable from the settings screen; the alternative failure mode is invisible and hands out a credential **What was false:** the round-1 check compared only `req.URL.Host`, so `https://feed.example.org/api?apikey=…` redirecting to `http://feed.example.org/api?apikey=…` returned `nil` and was followed — and a `Location` that preserves the query is the common case for exactly the apex-to-www and path-rewrite redirects this row is about. The api_key then travels in cleartext, which is the same harm the cross-host refusal exists to prevent, and none of the godoc, this row, or the PR body said so. **Corrected again 2026-09-14 (QA remediation of PR #11, round 2, finding D0):** this passage originally continued "(`net/http` does strip the `Cookie` header across a scheme change, so it was the api_key half only)", and that was false — `net/http` strips nothing on a scheme change. Read in the Go source on the machine this was written on (`shouldCopyHeaderOnRedirect` in `src/net/http/client.go`, go1.27.1): the decision is `isDomainOrSubdomain` over the punycoded, lower-cased `initial.Hostname()` and `dest.Hostname()`, and since `Hostname()` drops the port, neither the scheme nor the port is ever consulted. That function is only reached when `reqs[0].URL.Host != req.URL.Host`, so a same-host downgrade does not even test it and `stripSensitiveHeaders` stays false. Confirmed on the wire too, against the round-1 code with both a `Cookie` header and an `apikey` query set: both arrived at the `http` hop in cleartext. The round-1 defect therefore exposed **both** credentials `httpx` injects — every credential this package can carry — not the api_key half only. The shipped code refuses that hop, so nothing in the fix changes; what was understated was the blast radius of the defect this row exists to record. QA (PR #11, D1) reproduced it. Corrected in `checkRedirect` via `isSchemeDowngrade`; see DEC-064 for why the upgrade direction is followed rather than refused with it | T-020, T-021, T-022 |
| DEC-063 | 2026-09-14 | A response body over the cap is an **error** (`ErrBodyTooLarge`), never a truncated body, and the cap is enforced twice: a declared `Content-Length` above it is refused before any read, and the read itself runs through `io.LimitReader(body, cap+1)` | Truncation is the dangerous option and it is dangerous quietly: a torznab XML feed cut off mid-document parses as a *shorter* feed rather than as a failure, so the user silently loses results and nothing anywhere reports a problem. Refusing turns that into one visible failed source, which §6.3 already degrades around. The double enforcement is because neither check alone is sufficient: `Content-Length` is absent on any chunked response (so the limited read is what actually holds the line, and is what a test drives with a flushing handler), while the pre-read check avoids pulling 8 MB off the wire from a server that already declared it would send more. A server that declares a small length and sends a large body is caught by the limited read, proven with an injected `RoundTripper` because `net/http`'s own server will not emit that lie | T-020, T-021, T-022 |
| DEC-064 | 2026-09-14 | The redirect scheme rule is asymmetric on purpose: a same-host `https` to `http` hop is refused, a same-host `http` to `https` hop is followed, and a change of port is a change of host (the comparison is on the host as written, so `example.org` and `example.org:443` are different hosts and the hop is refused) | Refusing every scheme change would be the simpler rule and it was rejected on what the check is actually for: the api_key in the query string. A downgrade puts that credential on the wire in cleartext, which is the harm; an upgrade moves the *same* request to the *same* host the user configured and only adds TLS, so refusing it would break an ordinary, widespread redirect (a source configured by its apex `http` URL whose server upgrades every request) while protecting nothing — the first hop already went out in cleartext by the user's own configuration, and refusing the upgrade would leave them on the worse of the two schemes. Symmetry would be a rule that reads tidier and defends less. The port literal is the opposite trade: `example.org` and `example.org:443` are the same server, so refusing that hop is a false positive, but it fails **closed** — a legitimate redirect stops with a named error the user can see and fix from the settings screen, and no credential moves — so normalising it is backlog `T-928` rather than round-2 work. A non-http scheme in a `Location` is treated as a downgrade from `https` for the same reason, though `net/http` refuses to follow one anyway | T-020, T-021, T-022 |
| DEC-065 | 2026-09-14 | A Torznab item's `peers` attribute is the **total** swarm (seeders + leechers), so `Result.Leechers` is `peers - seeders`. An explicit `leechers` attribute, when present, wins over the subtraction. A `peers` value **below** `seeders` yields `Leechers = 0` rather than being read as a leecher count | This is the classic Torznab trap and the semantics were read on 2026-09-14 in three primary sources rather than recalled: (1) Prowlarr's `schemas/torznab.xsd` (branch `develop`) annotates the attribute in the schema itself — `<xs:enumeration value="peers" />` carries the comment `seeders + leechers`; (2) Jackett builds it that way, e.g. `release.Peers = release.Seeders + <leechers cell>` in `src/Jackett.Common/Indexers/Definitions/XSpeeds.cs` and the same accumulation in the generic Cardigann engine (`src/Jackett.Common/Indexers/Definitions/CardigannIndexer.cs`), branch `master`; (3) on the consumer side, `GetPeers` in `src/NzbDrone.Core/Indexers/.../Torznab/TorznabRssParser.cs` (Prowlarr and Sonarr, branch `develop`) returns `peers` when present and otherwise **computes** `seeders + leechers`, which only makes sense if the two are the same quantity. `leechers` is honoured first because the same `torznab.xsd` lists it and both consumers read it, even though Jackett's `ResultPage.cs` does not write one. The third case is the interesting one: since `peers` is *defined* as a total, a value below `seeders` is a contradiction, not a second dialect. Reading it as a leecher count would be inventing a meaning the attribute does not have, on no evidence, and would put a plausible-looking wrong number in the S/L column; zero is what `indexer.Result` already means by "the source reports none". Tested at, above and below the boundary in `TestSwarmCounts` and in the `search-messy.xml` fixture | T-021 |
| DEC-066 | 2026-09-14 | **Corrected twice on 2026-09-15 (QA remediation of PR #12, rounds 1 and 2) — round 1: the second clause of this column said no `Result` field that is not named for a URL ever carries a credential-bearing value, which was false for `Title` and `Magnet`. Round 2: the round-1 correction was itself false by one more field — it left `Result.Uploader` on the derived-and-therefore-clean side on the strength of a `://` refusal that stops a URL and not an opaque token, and it scoped `Result.ID`'s gap to the last-resort title branch when any non-URL guid, comments or link value passes through too. Both are rewritten in place, matching how DEC-040, DEC-046, DEC-050 and DEC-062 were corrected. The row now enumerates every field instead of counting them, because a count is what was wrong both times.** No text a Torznab source sent ever appears in an error this adapter produces: the `<error>` document's `description` is never read into memory; an `xml.SyntaxError` contributes its **line number** and not its message; an unexpected root element is classified (`<html>`, `<rss>`, `<caps>`, `<error>`, or "an unrecognised element") rather than quoted. On the `Result` side, field by field: `IndexerID` is the id the user configured; `InfoHash` is validated as 40 hex or 32 base32 characters, so nothing else survives it; `SizeBytes`, `Seeders` and `Leechers` are parsed integers; `Category` and `Trust` are enum values; `Published` is a parsed `time.Time`; `Extra` copies only whitelisted attributes whose value parses as a number, under fixed `torznab.`-prefixed keys; `TorrentURL` and `SourceURL` hold the source's own URLs verbatim and are safe because `internal/logging` masks on their names; `ID` is derived from the infohash, or from a URL-shaped guid/comments/link reduced to scheme, host and path, **and passed through as it stands for a candidate that is not URL-shaped and for the last-resort title fallback**; `Title` is passed through verbatim; `Magnet` is the `magneturl` attribute (or a magnet `<link>`) passed through verbatim, `dn=` included; and `Uploader` is passed through verbatim unless the value contains `://`, which is refused. So the fields carrying source text under a name `internal/logging` does not mask are, exhaustively, `Title`, `Magnet`, `Uploader`, and `ID` on its two unreduced branches — the residual gap DEC-071 records and backlog `T-934` is the fix for | T-020's DEC-061 closed this from the `httpx` side; this row is the same rule one layer up, and it is needed because the *content* of a Torznab response is attacker-influenced in a way `httpx` never sees. Every request carries the user's api_key in its query string, `internal/logging` masks by key name and value shape, and none of `ID`, `Title`, `Uploader` or an `Extra` key is on that list — so a credential in any of them is a plaintext key in the user's log file. The description is the sharpest case and it is not hypothetical: Jackett puts a whole .NET exception in it (`GetErrorXML(900, e.ToString())`, `src/Jackett.Server/Controllers/ResultsController.cs`, branch `master`) and Prowlarr puts `ex.Message` (`src/Prowlarr.Api.V1/Indexers/NewznabController.cs`, branch `develop`), both unfiltered, both able to contain the request. Rather than scrub it — this package has no access to the credential to scrub *with*, by design — the field is simply never decoded. The same reasoning covers element and entity names, which a hostile source could name after the key it was just sent. The cost is diagnostic detail: a malformed feed reports a line number rather than "element <item> closed by </channel>", and a wrong endpoint is diagnosed from the settings screen rather than the error text. The alternative is a log file with a working credential in it. `TestNoErrorFromThisAdapterCarriesTheCredential` and `TestWhichResultFieldsCanCarryTheCredential` enforce every clause, and they earned their keep during T-021 itself: `Resolve`'s "nothing to resolve" error was naming the result with `%q` on `Result.ID`, which for a caller-supplied result can be a raw download URL, and the sweep caught it before the first commit. **What was false:** the second clause originally read "no `Result` field that is not named for a URL ever carries a credential-bearing value", and the package doc said the same thing in stronger words ("No error, and no Result field that is not named for a URL, ever carries text this package received from the source"). `Result.Title` is `strings.TrimSpace(it.Title)` and `Result.Magnet` is the `magneturl` attribute — both the source's own text, unaltered — and neither name is on `internal/logging`'s `sensitiveKeySubstrings` list, while an opaque api_key has no value shape `maskText` recognises. QA (PR #12, 2026-09-15) pointed a server that echoes the key into `<title>` and into a magnet's `dn=` at the adapter, logged the resulting `Result` through the real `internal/logging` sink, and read the key out of the log file in plaintext; the same probe was re-run on the remediation branch before this row was rewritten and reproduced identically, with `SourceURL` and `TorrentURL` coming back `[REDACTED]` beside them. The test named in this row was worse than silent about it: `TestOnlyTheURLNamedResultFieldsCarryTheCredential` listed `Title` and `Magnet` in its forbidden map, but `echoingFeed()` never put the key in the title and wrote no `magneturl` at all, so the assertion was vacuous for precisely the two fields that leak. Fixed here as prose and tests only, which is the whole of what this adapter can fix: the claim is scoped to derived fields, the test is renamed `TestWhichResultFieldsCanCarryTheCredential` and now asserts `Title` and `Magnet` **do** carry the source's text (so it goes red if that ever changes) while every derived field must not, and `TestResultIDFallsBackToTheTitleAndInheritsItsGap` pins the ID branch. Proved non-vacuous by mutation: scrubbing `Title`, or truncating `Magnet` before its `dn=`, turns the new test red while the old one stays green. **Corrected again 2026-09-15 (QA remediation of PR #12, round 2, finding D4):** the round-1 rewrite of this column read "no `Result` field this adapter **derives** ever carries a credential-bearing value" and listed "`Result.Uploader` refuses a value containing `://`" among the derived-and-therefore-clean fields, and it scoped ID's gap to "the last-resort branch where an item published no infohash, no guid, no comments and no link". Both were false. `uploaderFrom` skips a value only when it contains `://`, and an api_key is an opaque token with no `://` in it, so `<attr name="uploader" value="KEY"/>` — no harder for a hostile source than echoing the key into a `<title>` — lands the key in `Result.Uploader` verbatim, under a name that is not on `sensitiveKeySubstrings`. `withoutQuery` likewise returns any candidate without a URL scheme as it stands, so a *present* `<guid isPermaLink="false">KEY</guid>` reaches `Result.ID` verbatim, with no need for the item to be identity-less. QA reproduced both against the real `internal/logging` sink and read the key out of the log file in plaintext beside a `[REDACTED]` `SourceURL` and `TorrentURL`; both were reproduced again on the remediation branch before this row was rewritten. The test was vacuous in the round-1 pattern once more: `echoingFeed()` and `echoingEverythingFeed()` both write the uploader as an `https` link ending in `/u/<key>`, so `uploaderFrom` returned `""` and `assertDerivedFieldsAreClean`'s `Uploader` entry could never fail. Fixed here as prose and tests only, again: this column now enumerates every field rather than asserting a count, `bareTokenEchoingFeed` echoes the key back as a bare token in an uploader attribute and a non-permalink guid, `Uploader` and the bare-token `ID` are asserted to **carry** it, and `assertLinkShapedUploaderIsRefused` keeps the `://` refusal pinned separately. Proved non-vacuous by mutation with a verbatim copy of the round-1 test running beside the new one: scrubbing `Uploader` to a constant, and making `withoutQuery` refuse a non-URL candidate, each turn the new test red while the old one stays green; removing the `://` refusal and dropping the query-strip turn both red, so moving `Uploader` out of the derived map lost no coverage | T-021, T-022 |
| DEC-067 | 2026-09-14 | `Caps.Latest` is set by **making a real keyword-less `t=search` request** during `Discover`, and is true only when that request returned a well-formed feed containing at least one item. An empty feed, a parse failure, an HTTP failure, or an error document all leave it false. `Caps.ProvidesMagnet` is decided by the same response and requires every returned item to carry a magnet | The criterion says to probe rather than assume, and Torznab offers nothing to read: there is no `latest` function, and `<searching><search available="yes"/>` speaks to keyword search only. What exists is a documented behaviour — "if the input string for search is empty all items (within the server/query limits) are returned for the matching categories" (§3 of `docs/newznab_api_specification.txt`, nZEDb/nZEDb, branch `dev`), Jackett's `TorznabQuery.IsRssSearch` (branch `master`), and no empty-`q` rejection in either Jackett's `ResultsController` or Prowlarr's `NewznabController` — so the only honest probe is to send the request a `ModeLatest` query would send and look at the answer. The ambiguous case decides the design: a server that returns an empty feed for an empty keyword is indistinguishable from a server whose index is empty, and there is no third signal to break the tie. Failing **closed** makes the registry skip the source for `ModeLatest` and *report the skip* (§6.3), which the user can see and act on; failing open gives them an `L` key that returns nothing for that source on every press, with nothing anywhere saying why. The probe costs exactly one extra request per source at construction, asks for `limit=1`, and is skipped entirely when caps says search is unavailable (§6.13) | T-021 |
| DEC-068 | 2026-09-14 | There is no uploader-trust attribute anywhere in the Torznab/Newznab protocol, so a Torznab source's results are `TrustUnknown` unless the server invented an attribute of its own. The adapter maps `vip`, `trusted` and `verified` if it sees them, and reads `uploader`/`poster` for the name | Stated as a finding rather than a guess, because the criterion asks for trust "from any uploader/verified attribute the server exposes" and the correct answer turned out to be "there is not one". Checked on 2026-09-14 in Jackett's `src/Jackett.Common/Models/ResultPage.cs` (branch `master`), which writes `category, rageid, tvdbid, imdb, imdbid, tmdbid, tvmazeid, traktid, doubanid, genre, language, subs, year, author, booktitle, publisher, artist, album, label, track, seeders, peers, coverurl, infohash, magneturl, minimumratio, minimumseedtime, downloadvolumefactor, uploadvolumefactor` and nothing trust-shaped; in Prowlarr's `schemas/torznab.xsd` (branch `develop`); and in the attribute list in §4.1 of `docs/newznab_api_specification.txt` (nZEDb/nZEDb, branch `dev`), whose closest entry is `poster` — the **NNTP poster**, a Usenet posting identity, not an image and not a badge. So `poster` is read as an uploader name and the three badge names are supported speculatively, at a cost of about fifteen lines, for a private tracker that adds one. The distinction the code does make is real and worth having: an attribute present and false is `TrustNone`, absent is `TrustUnknown`, which `indexer.Trust` documents as different information that sorts differently. Trust remains display metadata that gates nothing (§2) | T-021 |
| DEC-069 | 2026-09-14 | **Corrected 2026-09-15 (QA remediation of PR #12) — the last clause of this column was false and is rewritten in place, matching how DEC-040, DEC-046, DEC-050 and DEC-062 were corrected.** Construction is split in two. `New` never touches the network and returns fail-closed caps (search only). `Discover` probes, and returns a **usable adapter alongside its error** when the probe fails: the `*Adapter` is nil only when the options themselves are unusable. The two probe steps then report themselves **differently, on purpose** — a `t=caps` failure wraps `ErrCapsUnavailable` and leaves the adapter on the fail-closed baseline, while a Latest-probe failure deliberately does **not** wrap it and leaves the adapter carrying everything the caps document said, with `Caps.Latest` and `Caps.ProvidesMagnet` false. `errors.Is(err, ErrCapsUnavailable)` is therefore how a caller tells "this source published no usable caps document" from "its caps are known and only the recent-additions probe failed" | Two constraints pull against each other. `indexer.Indexer` requires `Caps()` to do no I/O, never block, and stay constant for the lifetime of the value, so the probe cannot live inside `Caps()` and cannot mutate an adapter afterwards without breaking the "constant" half. But a source that is offline, or simply does not publish a caps document, must not become unconfigurable — refusing to build the adapter would take a perfectly searchable source away from the user over metadata (§6.3), and Jackett's own caps output omits `<limits>` and `<registration>` entirely, so partial documents are normal. Returning both is unusual enough to be a documented contract rather than an accident: the godoc says the adapter is nil only for bad options, says discarding it on error is the one thing not to do, and the sentinel lets a caller tell the two apart with `errors.Is`. The settings screen shows the error; the search path uses the adapter. The alternative designs were rejected: a mutable `Probe()` method breaks the frozen contract's constancy guarantee, and a three-value return puts the awkwardness in every call site instead of one godoc. **What was false:** this row originally ended "and every other error wraps `ErrCapsUnavailable`", and `Discover`'s godoc said the same, adding that every such error arrives with an adapter "carrying the fail-closed baseline caps". Neither holds for the Latest probe. `probe` returns `fmt.Errorf("probing for a recent-additions feed: %w", err)` with no sentinel in it, because at that point the caps document has already parsed and its contents are on the adapter — and the code's own `TestALatestProbeFailureLeavesTheRestOfCapsIntact` asserts both halves: `!errors.Is(err, ErrCapsUnavailable)`, and `Caps{Search: true, Categories: true, Pagination: true}` rather than the baseline. Re-run on 2026-09-15 against a source serving `caps-full.xml` and a 503 on `t=search`, `Discover` returned `torznab fixture-feed: probing for a recent-additions feed: httpx: GET <host>: HTTP 503 Service Unavailable (1 attempt)`, `errors.Is(err, ErrCapsUnavailable) = false`, and `Caps{Search:true Latest:false Categories:true Pagination:true}`. So the design was right and the description was wrong, in the direction that matters: a caller following this row would have read a Latest-probe failure as "the caps document is fine" being impossible, and lost the one discrimination the sentinel exists to give. Corrected in this column and in `Discover`'s godoc; nothing in the code changed. QA (PR #12, BLOCKING 3) found it | T-021 |
| DEC-070 | 2026-09-14 | A category filter is translated into **only the category ids the source published in its own caps document**, sent as `cat=`, and the results are not filtered again locally. A query naming categories that the source declared none of returns zero results without making a request | tortui's taxonomy is deliberately coarse (seven data-kind buckets) and a Torznab server's is not, so the translation has to happen against that server's own numbering — which the caps document conveniently hands over. Building the map from the document means tortui never invents an id for a server, and `indexer.CategoryFromTorznab` interprets the numbering exactly once, in the adapter, where §13 puts it. Not re-filtering locally is the deliberate half: the source's classification is authoritative for its own ids, and re-checking against tortui's buckets would drop precisely the items the source has a custom id for — those map to `CategoryOther` on the way in and would fail a filter the source itself considered satisfied. The no-intersection case answers without a request because the answer is knowable without one, and one fewer request to someone else's server is the right default (§6.13). A source that declared no categories at all reports `Caps.Categories = false` and its `cat` parameter is omitted, which `indexer.Query` explicitly permits | T-021 |
| DEC-071 | 2026-09-15 | **Corrected 2026-09-15 (QA remediation of PR #12, round 2) — this row was written on the round-1 finding and named two pass-through fields; there are four sites, and the correction is in place in the style of DEC-040/046/050/062/066.** `Result.Title`, `Result.Magnet` and `Result.Uploader` keep carrying the source's own text **verbatim**, as does `Result.ID` whenever the identity it picks is a non-URL guid, comments or link value or the title fallback, and the remediation of PR #12 is a disclosure rather than a fix: the package doc, DEC-066 and the PR body are scoped to what the adapter actually guarantees, the credential test is rewritten to assert both directions of the real boundary, and the structural fix is left to backlog `T-934` (now a filed row, along with `T-935` and `T-936`). `resultID`'s last-resort fallback to the title is kept as well, so an item that published no infohash, no guid, no comments and no link still has an identity, and that identity inherits `Title`'s gap | QA (PR #12) reproduced the leak this row exists to record: a source that echoes the user's api_key into a `<title>` or into a magnet's `dn=` parameter gets it written to the log file in plaintext the moment anything logs the `Result`, because neither field name is on `internal/logging`'s `sensitiveKeySubstrings` list and an opaque key has no value shape `maskText` matches. Reproduced again on the remediation branch before anything was written here. Three routes were open. **Stripping or scrubbing the fields** is wrong on its own terms: the title is the value the user reads in the results table, and the magnet has to reach the engine exactly as published or the torrent does not start — and this package has no access to the credential to scrub with, by design (DEC-061). **Fixing it properly** means a `LogValue()` on `indexer.Result`, or a registry-level rule that a result is only ever logged under a masked key. Both touch the frozen §5 `Result` contract, which AGENT.md §12 makes a stop condition and §5 makes a `DEC-` plus a note on every affected task; it is also not a Torznab problem — every adapter T-022 onward produces the same `Result` — so doing it inside an adapter task would put a cross-cutting change behind a review that is looking at one adapter. That is `T-934`. **Disclosing it accurately** is what is left, and it is what was actually missing: the code was already correct, and the only defect QA found in this PR was prose claiming a guarantee the code does not provide. So the claim is now scoped to derived fields, the two exceptions are named wherever the old claim appeared, and the test that appeared to cover them — and did not, because `echoingFeed()` put the key in neither — asserts that they **do** carry the source's text, which turns red if a future change ever alters that and forces this row to be revisited with it. The `resultID` fallback was evaluated for removal on the same occasion and kept, on the merits rather than for convenience: the leaked text is already present in `Title` verbatim by necessity, so removing the duplicate narrows nothing an attacker can reach, while an item with no other identity would be left with an empty `Result.ID`. It is asserted by `TestResultIDFallsBackToTheTitleAndInheritsItsGap` rather than left implicit, so `T-934` inherits a written record of every field in the gap. Cost accepted: until `T-934` lands, a caller that logs a whole `Result` from a hostile or broken source can write a credential to the log file, and nothing in `internal/logging` will catch it. **What was false:** this row originally said "the two exceptions are named wherever the old claim appeared", and the fields it named were `Title` and `Magnet` only. `Result.Uploader` is a third: `uploaderFrom` refuses a value containing `://`, which stops a link and not an opaque token, so an api_key echoed into `<attr name="uploader" value="KEY"/>` is passed through verbatim under a name `internal/logging` does not mask. And ID's gap is wider than the last-resort title branch this row described: `withoutQuery` reduces URL-shaped candidates only, so a present `<guid isPermaLink="false">KEY</guid>` reaches `Result.ID` as it stands. QA reproduced both against the real `internal/logging` sink on round 2 of PR #12, and both were reproduced again on the remediation branch first. The same three routes were re-weighed for each. **Scrubbing** is no more available here than it was for `Title`: this package never sees the credential (DEC-061), an uploader's name is legitimately an opaque token so there is no shape to match on, and refusing a non-URL guid would throw away the source's own stable identity — the very thing `resultID` exists to produce — leaving items with an identity that changes whenever their title does. **Fixing it properly** is the same `LogValue()`-on-`Result` change, so the same §12 stop condition, and `T-934`'s description now covers all four sites. **Disclosing it accurately** is again what was missing, and the lesson taken is a mechanical one: the package doc, DEC-066 and the T-021 row now write out every `Result` field and what the adapter does with it, because both false claims were counts ("no field not named for a URL", then "two fields"), and a count is what nobody re-derives when the code moves. `bareTokenEchoingFeed` and `assertLinkShapedUploaderIsRefused` make both halves of the uploader story fail loudly if either changes | T-021, T-022 |

Append a row whenever you make a choice a future reader would question. Empty date means
inherited from the initial plan.

---

## Blocked

### `T-022` — the HTML parser the §3 stack mandates cannot be taken without a known-vulnerable
### dependency under the project's pinned Go version

**What is blocked.** Merging `T-022`. The scraper framework is complete, tested (99.9% statement
coverage) and documented on branch `task/T-022-scraper-framework`, and every acceptance
criterion is met. What it cannot do is ship without a dependency AGENT.md §12 forbids.

**The finding.** AGENT.md §3 locks `PuerkitoBio/goquery` as the HTML scraping library, and the
T-022 criteria require a goquery HTML mode. `goquery` requires `golang.org/x/net/html`.
`govulncheck` (`golang.org/x/vuln` v1.8.0, run on 2026-09-15 against this branch) reports the
pinned `golang.org/x/net v0.39.0` as carrying **seven vulnerabilities whose call traces it
resolves into this package's own `html.Parse` call** (`internal/indexer/scraper/html.go:80`):

| ID | Summary | Fixed in |
|---|---|---|
| `GO-2026-4440` | Quadratic parsing complexity in `golang.org/x/net/html` | `v0.45.0` |
| `GO-2026-4441` | Infinite parsing loop in `golang.org/x/net` | `v0.45.0` |
| `GO-2026-5025` | Incorrect handling of namespaced elements in foreign content | `v0.55.0` |
| `GO-2026-5027` | Incorrect handling of HTML elements in foreign content | `v0.55.0` |
| `GO-2026-5028` | Denial of service when parsing arbitrary HTML | `v0.55.0` |
| `GO-2026-5029` | Incorrect handling of character references in DOCTYPE nodes | `v0.55.0` |
| `GO-2026-5030` | Duplicate attributes can cause XSS | `v0.55.0` |

These are not theoretical for this package: its entire job is calling `html.Parse` on a page an
arbitrary source served. `GO-2026-4440` is the quadratic behaviour this task measured
independently before knowing the advisory existed (39 seconds to parse a 1.1MB page of nested
`<div>` elements), and `guardHTMLDepth` mitigates exactly that one. `GO-2026-4441`, an
**infinite** parsing loop, is not mitigated by a depth bound and would hang a search goroutine
for the life of the process.

**Why it cannot be fixed inside this task.** The first fixed release, `x/net v0.45.0`, declares
`go 1.24.0`; the release that fixes all seven, `v0.55.0`, declares `go 1.25.0` (read from each
module's own `go.mod` in the module cache). This repository declares `go 1.23.0` and CI pins
`GO_VERSION: "1.23"`, matching AGENT.md §3's `Go 1.23+` row. Taking a fixed `x/net` therefore
requires raising the project's minimum Go version in `go.mod`, in `.github/workflows/ci.yml`,
and in AGENT.md §3 — a change to a locked stack row with consequences for the support matrix,
for CI, and for anyone building from source, which AGENT.md §3 ("do not re-litigate") and §12
("a change would alter the frozen contracts", "a required dependency … has an open CVE") both
put outside a single adapter task.

**What would unblock it — exactly one decision.** Approve raising the project's minimum Go
version to **1.25** and the accompanying edits:

1. AGENT.md §3, the Language row: `Go 1.23+` → `Go 1.25+`.
2. `.github/workflows/ci.yml`: `GO_VERSION: "1.23"` → `"1.25"`.
3. `go.mod`: `go 1.23.0` → `go 1.25.0`, and `golang.org/x/net v0.39.0` → `v0.55.0` or later.
4. `make licenses` re-run (`x/net` stays BSD-3-Clause, so `NOTICE` changes only in the version
   inside its license URL).

Approving **1.24** instead closes `GO-2026-4440` and `GO-2026-4441` — the two that matter most
here, since they are the denial-of-service pair — and leaves the five `GO-2026-502x` advisories
open; four of them are correctness bugs in foreign-content and DOCTYPE handling and the fifth is
an XSS vector that does not apply to a program that never renders the HTML it parses. It is a
defensible half-step, but it is still a knowing ship of open advisories and the decision is not
this task's to take.

**What the branch already does about it, so the change is small.** The branch was tested against
both versions. `maxHTMLDepth` is set to **512**, deliberately the same bound `x/net v0.45.0+`
imposes on its own open-element stack (`html: open stack of elements exceeds 512 nodes` in
`parse.go`), so the guard behaves identically before and after the upgrade. With `go.mod` moved
to `go 1.25.0` and `x/net v0.55.0`, `go build ./...`, the full package test suite and
`govulncheck ./...` (`0 vulnerabilities`) were all green on this machine with no change to any
source file. The branch is left pinned at `v0.39.0` so that it does not raise the project's
minimum Go version by side effect.

**Not verified.** Only the darwin/arm64 leg was run here. Whether a GitHub `ubuntu`/`windows`
runner provisioned with Go 1.23 would auto-upgrade its toolchain from a `go 1.25.0` directive
under the default `GOTOOLCHAIN=auto`, rather than failing, was **not** tested — which is why
step 2 above changes `GO_VERSION` explicitly rather than relying on it.
