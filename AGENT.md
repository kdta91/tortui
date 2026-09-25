# AGENT.md

Operating contract for the autonomous agent building **tortui** — a terminal UI torrent
search-and-download client.

Read this file in full at the start of every session, before touching `TASK_TRACKER.md`.
If anything in this file conflicts with a task description, **this file wins** — stop and log
the conflict instead of guessing.

---

## 1. Mission

Build a single-binary TUI application that lets a user:

1. Search one or more configurable torrent indexers from inside the terminal.
2. Browse results with size, seeders, leechers, age, category and uploader trust status.
3. Add a result to the built-in torrent engine and watch it download.
4. Manage downloads — pause, resume, open the file, open the containing folder, view the
   originating source, remove (with or without data).

The indexer layer is **source-agnostic**: no indexer is special-cased anywhere outside its own
adapter package. Adding a new source must never require a change to the TUI, the engine, or
the core domain types.

**Standalone contract.** tortui depends on no other software at runtime. Not a torrent daemon,
not an indexer proxy, not a browser, not a package the user has to install first. Installing the
binary and running it must produce a working search-and-download experience immediately, with no
configuration, no account, and no external service. Any task, dependency, or design that would
make some part of the core loop conditional on the user installing something else is wrong —
raise it as a stop condition (§12) rather than documenting a prerequisite.

**Portability contract.** One codebase runs on macOS, Linux, and Windows, launched from any
shell — zsh, bash, fish, sh, PowerShell, or anything else. The binary takes over the TTY on
start and never reads a shell's config, so there is no per-shell code path and no task may
create one. Per-OS differences are real but confined: they live behind build tags in
`internal/platform` and nowhere else. See §14.

---

## 2. Scope boundaries (hard rules)

These are not negotiable and not subject to task-level override.

- **Sources beyond the lawful defaults are user-supplied.** Beyond the small bundled set below,
  the binary contains no endpoints and no preconfigured list. The user adds their own sources at
  runtime, and tortui talks to exactly those.
- **No access-control circumvention.** Do not implement captcha solving, paywall bypass,
  credential scraping, shared-account pools, DRM handling, or any workaround for a site's
  authentication. Private indexers authenticate with credentials **the user supplies from their
  own account** (API key, cookie, or passkey in their config file). That is the only auth path.
- **"VIP" means uploader trust metadata, nothing else.** Several public indexers badge uploads
  from vetted uploaders (VIP / Trusted / Verified). Model this as the `Trust` enum on
  `indexer.Result`, surface it as a badge in the results table, and allow sort/filter on it.
  It is a display field. It never gates behaviour and it never implies an entitlement bypass.
- **No content-specific logic.** The app does not know or care what a torrent contains. No
  bundled category presets for specific media, no scene-release parsers, no metadata enrichment
  from media databases in v1.
- **Bundled sources are limited to unambiguously lawful ones.** The binary ships definitions for
  sources that officially distribute their own content over BitTorrent — public archives,
  research dataset repositories, distro release listings — **compiled into the binary with
  `go:embed` and enabled on first run**, so the standalone contract holds with no files to place
  and nothing to configure. It ships **no definition, endpoint, default, or preset for any
  source whose primary use is distributing infringing content**, regardless of how popular or
  convenient. Everything in that second category is user-supplied at runtime, which the TUI
  makes a sub-minute task (§7).
- **Never name an infringement-oriented site anywhere in this repository.** Not in code,
  comments, tests, fixtures, example configs, documentation, commit messages, branch names,
  issue titles, or PR descriptions. Test fixtures use `example.org` and invented site names.
  Naming the lawful sources tortui bundles is fine and expected — the rule is about what kind
  of site, not about naming sites at all. §16 explains why this line sits where it does.
- **No index of your own.** Do not crawl the DHT, build a searchable infohash database, mirror
  another index, or cache results across users. tortui queries sources and forwards what they
  return. Building an index is a categorically different legal posture and is out of scope
  permanently, no matter how much it would improve search.
- **No telemetry, analytics, crash reporting, or phone-home of any kind.** The app makes network
  connections to exactly two classes of destination: sources the user configured or that ship
  by default, and BitTorrent peers. Nothing else, ever.
- **No server-side component.** tortui is a client. There is no hosted index, no tracker, no
  relay, no proxy, no shared cache. Anything that would put the project in the position of
  serving content to other people is out of scope permanently.
- **Ship a legal-notice line** in `README.md` and in the first-run screen: the user is
  responsible for what they search for and download, and for complying with the terms of any
  indexer they configure and the law of their jurisdiction.

---

## 3. Stack (locked — do not re-litigate)

| Concern | Choice | Notes |
|---|---|---|
| Language | Go 1.25+ | Fast compile loop, single static binary, stable APIs |
| TUI framework | `charmbracelet/bubbletea` | Elm-style; `bubbles` for widgets, `lipgloss` for style |
| Torrent engine | `anacrolix/torrent` | Embedded — **no external daemon, no transmission/qbit dependency** |
| HTTP scraping | `net/http` + `PuerkitoBio/goquery` | Only inside scraper adapters |
| Config | `BurntSushi/toml` + `adrg/xdg` | XDG-compliant paths |
| Persistence | `etcd-io/bbolt` | Session/resume state; no SQL dependency |
| Logging | `log/slog` → rotating file | **Never** to stdout/stderr — the TUI owns the terminal |
| Text width | `rivo/uniseg` | Grapheme-aware width — `len()` and rune counts break tables |
| Platform integration | `os/exec` + build tags | Confined to `internal/platform`; see §14 |
| Testing | stdlib `testing` + `charmbracelet/x/exp/teatest` | Golden-file fixtures for adapters |

Any additional dependency requires a `DEC-` entry in the decision log with a license check
(MIT / Apache-2.0 / BSD / ISC / **MPL-2.0** only — **no GPL/AGPL/LGPL**). MPL-2.0 is admitted as
an exception for an **explicitly named set of module paths**, not as a license family: the locked
torrent engine on this table plus the modules it actually compiles against — see §16 for the list
and the reasoning. It does not open the door to copyleft dependencies generally; GPL, AGPL and
LGPL remain barred without exception. **That scope is enforced, not just stated:** `go-licenses
check --allowed_licenses` (what `make licenses` runs) has no per-module scoping, so once MPL-2.0
is on the allowlist it would pass any MPL-2.0 module by itself.
`scripts/check-license-scope.sh` closes that gap — it reads the `go-licenses report` data the
`licenses` target already generates and fails the build, naming the offender, if any MPL-2.0 row
names a module outside `ALLOWED_MPL_MODULES` (the `Makefile`'s authoritative list). The set is an
enumeration, never an `anacrolix/*` prefix: a wildcard would silently admit any future module
published under that path, and admitting a module must stay a deliberate, reviewed act with a
`DEC-` entry behind it. See §16, DEC-099 (the mechanism and its residual gap) and DEC-100 (the
widened set).

---

## 4. Repository layout

```
cmd/tortui/main.go           # entrypoint, flag parsing, wiring only
internal/
  app/                       # composition root; builds engine+registry+tui
  config/                    # TOML load/validate/defaults, XDG paths
  indexer/
    indexer.go               # Indexer interface, Result, Query, Trust, Caps
    registry.go              # named registry, fan-out search, per-source timeouts
    torznab/                 # Torznab/Newznab XML adapter
    scraper/                 # YAML-definition-driven HTML/JSON adapter
  engine/
    engine.go                # Engine interface, TorrentStatus, AddSource
    anacrolix/               # concrete impl
    fake/                    # in-memory impl for TUI tests
  store/                     # bbolt persistence (session, history, resume)
  platform/                  # open-file / reveal-in-folder per OS
  tui/
    root.go                  # top-level model, screen routing, keymap
    search.go results.go downloads.go details.go settings.go
    theme/                   # lipgloss styles, single accent colour
    components/              # progressbar, table, statusbar, confirm dialog
testdata/                    # recorded HTTP fixtures, golden renders
docs/
config.example.toml
Makefile
```

Rules:
- `cmd/` contains wiring only — no business logic, no more than ~80 lines.
- Nothing outside `internal/indexer/<adapter>/` may import that adapter directly. The registry
  is the only consumer.
- `internal/tui/` imports `indexer` and `engine` **interfaces only**, never concrete impls.
- No package may import `internal/app`.

---

## 5. Domain contracts

These types are the spine of the project. Once implemented in Phase 1 they are **frozen** —
changing them requires a `DEC-` entry and a note on the affected tasks.

```go
// internal/indexer/indexer.go

type Trust int

const (
    TrustUnknown Trust = iota // indexer does not expose trust info
    TrustNone                 // ordinary uploader
    TrustVerified             // upload itself was verified
    TrustTrusted              // uploader holds a trusted badge
    TrustVIP                  // uploader holds the indexer's highest trust badge
)

type Mode int

const (
    ModeSearch Mode = iota // keyword query
    ModeLatest             // most recently added, no keyword
)

type Query struct {
    Mode       Mode
    Text       string // empty when Mode is ModeLatest
    Categories []Category
    MinSeeders int
    Limit      int
    Offset     int
}

type Result struct {
    IndexerID  string            // which source produced this
    ID         string            // stable id within that source
    Title      string
    InfoHash   string            // may be empty until Resolve
    Magnet     string            // may be empty if TorrentURL is set
    TorrentURL string            // may be empty if Magnet is set
    SizeBytes  int64
    Seeders    int
    Leechers   int
    Category   Category
    Published  time.Time
    Uploader   string
    Trust      Trust
    SourceURL  string            // human-viewable page this came from
    Extra      map[string]string
}

type Caps struct {
    Search      bool
    Latest      bool // can return a recent-additions feed with no keyword
    Categories  bool
    Pagination  bool
    RequiresAuth bool
    ProvidesMagnet bool
}

type Indexer interface {
    ID() string
    Name() string
    Caps() Caps
    Search(ctx context.Context, q Query) ([]Result, error)
    // Resolve fills Magnet/InfoHash for indexers that only return a details page.
    // Must be a no-op returning r unchanged when already resolved.
    Resolve(ctx context.Context, r Result) (Result, error)
}
```

```go
// internal/engine/engine.go

type State int
const (
    StateQueued State = iota
    StateChecking
    StateDownloading
    StateSeeding
    StatePaused
    StateErrored
)

type TorrentStatus struct {
    ID              string
    Name            string
    InfoHash        string
    State           State
    Progress        float64 // 0.0 – 1.0
    DownloadedBytes int64
    TotalBytes      int64
    DownRate        int64 // bytes/sec
    UpRate          int64
    Peers           int
    Seeds           int
    ETA             time.Duration // -1 when unknown
    SavePath        string
    Origin          Origin        // indexer id + source URL that produced it
    Err             error
}

// AddSource carries the magnet/URL/path plus the destination chosen for this
// torrent. SavePath is always an absolute, cleaned path that has been verified
// to sit inside one of the user's known destination roots.
type AddSource struct {
    Magnet     string
    TorrentURL string
    FilePath   string
    SavePath   string // per-torrent destination; falls back to the configured default
}

type Engine interface {
    Add(ctx context.Context, src AddSource) (string, error) // returns torrent ID
    Pause(id string) error
    Resume(id string) error
    Remove(id string, deleteData bool) error
    List() []TorrentStatus
    Files(id string) ([]FileStatus, error)
    Updates() <-chan []TorrentStatus // coalesced, ~2 Hz
    Close() error
}
```

---

## 6. Architectural invariants

Violating any of these fails review regardless of whether tests pass.

1. **No blocking calls in `Update()`.** All I/O returns a `tea.Cmd`. If a bubbletea `Update` can
   block on the network, disk, or a channel receive, it is wrong.
2. **Every network call takes a `context.Context` with a deadline.** Registry fan-out applies a
   per-indexer timeout (default 15s) and returns partial results — one slow source must never
   stall the whole search.
3. **One failing indexer degrades, never crashes.** Errors are collected per-source and shown in
   the status bar as `2/4 sources failed (tab to view)`. `Search` never returns a fatal error
   when at least one source succeeded. A source that lacks a capability the query needs
   (`Caps.Latest` for a `ModeLatest` query) is skipped and noted, not treated as a failure.
4. **The engine is interface-only to the TUI.** The TUI must run end-to-end against
   `engine/fake`. If a screen can't be tested without a real BitTorrent swarm, refactor.
5. **Status updates are pushed, not polled.** The TUI subscribes to `Updates()`; it never loops
   over `List()` on a ticker.
6. **No secrets in the repo.** Config lives at `$XDG_CONFIG_HOME/tortui/config.toml`, mode 0600.
   `config.example.toml` contains placeholders only. Add a pre-commit secret scan in Phase 0.
7. **Unit tests make zero network calls.** Adapter tests replay fixtures from `testdata/` via
   `httptest.Server`. Any test needing live network is tagged `//go:build integration` and
   excluded from `make check`.
8. **Rendering is pure.** `View()` reads model state and returns a string. No I/O, no mutation.
9. **Errors carry context.** `fmt.Errorf("torznab %s: %w", id, err)`. Never `panic` outside
   `main()`. Never swallow an error with `_`.
10. **Nothing in the core loop requires external software.** Search, add, download, open, and
    remove must all work on a machine with nothing installed but tortui itself. Integrations
    with other tools are conveniences layered on top and may never become the only path to a
    capability. If a task's design implies "the user must first install X," stop (§12).
11. **Every filesystem path derived from remote data is validated before use.** Torrent file
    names are attacker-controlled and routinely contain `..`, absolute paths, reserved Windows
    names, and NUL bytes. Clean, resolve, and confirm containment inside a known destination
    root before any create, write, open, or delete. This applies to writing downloads, opening
    files, revealing folders, and deleting data — a check in one of those places is not a check
    in the others.
12. **Destination roots are the security boundary, not "the download dir".** Once a user can
    choose a per-torrent destination there are several valid roots. Containment checks resolve
    against the set of known roots (the default plus every saved destination plus the path of
    any active torrent), never against a single hardcoded directory.
13. **Never poll a source on a timer.** Latest feeds invite repeated fetching; refresh is
    user-initiated, results are cached briefly, and a minimum interval is enforced per source.
    Hammering someone's server is both rude and the fastest way to get a user's IP blocked.

---

## 7. TUI specification

### Screens

| Screen | Purpose |
|---|---|
| `search` | Query input, mode selector (Search / Latest), source multi-select, category filter |
| `results` | Sortable table of merged results across sources |
| `details` | Single result: full title, files, trackers, uploader, trust, source URL |
| `downloads` | Active/completed torrents with progress and actions |
| `settings` | Manage indexers (add/edit/test/remove), download dir, rate limits, theme |

**Settings is not a viewer.** Every configurable thing — especially adding a source — is fully
editable from inside the TUI, with inline validation and a test-before-save. Editing the config
file by hand stays supported but is never the documented path, and no feature may be reachable
only by editing TOML.

### Results table columns

`Title` (flex) · `Size` · `S/L` · `Trust` · `Age` · `Source`

Trust renders as a compact badge, not a word: `VIP` / `TR` / `✓` / blank. Colour-coded via the
single accent plus dim. Sortable on any column; default sort is seeders descending.

### Downloads row

```
▸ ubuntu-24.04-desktop-amd64.iso
  ████████████████░░░░░░░░  62%   3.1/5.0 GB   ↓ 12.4 MB/s   ↑ 880 KB/s   18 peers   ETA 2m
  archive-src · added 14:02                                          [o]pen [f]older [p]ause [x] remove
```

### Keymap (global unless noted)

| Key | Action |
|---|---|
| `/` | Focus search input |
| `L` | Latest — recent additions across sources, no keyword needed |
| `R` | Refresh current results |
| `tab` / `shift+tab` | Cycle screens |
| `1`–`5` | Jump to screen |
| `j`/`k`, `↑`/`↓` | Move selection |
| `enter` | Add torrent (results) / open details (downloads) |
| `d` | Details |
| `s` | Cycle sort column · `S` reverse |
| `o` | Open downloaded file (downloads) |
| `f` | Open containing folder (downloads) |
| `u` | Open source page in browser |
| `p` | Pause/resume |
| `x` | Remove — always opens a confirm dialog offering *keep data* / *delete data* |
| `?` | Help overlay |
| `q` / `ctrl+c` | Quit (prompts if downloads active) |

### Visual rules — "minimal" is a constraint, not a vibe

- One accent colour from the theme. Everything else is foreground, muted, or dim.
- No box borders except around the focused pane and modals. Separate with whitespace.
- Progress bars use block glyphs `█░`, never ASCII `[###   ]`.
- Never more than one modal deep.
- Must render legibly at 80×24 and degrade gracefully below (hide columns right-to-left:
  Source → Age → Trust).
- Respect `NO_COLOR`. Detect truecolor via `lipgloss`; fall back to 256 then 16 colours.

---

## 8. Commands

```
make build     # go build -o bin/tortui ./cmd/tortui
make run       # build + run with ./dev-config.toml
make test      # go test ./...
make lint      # golangci-lint run
make fmt       # gofumpt -w . && goimports -w .
make check     # fmt-check + lint + test + go vet  ← the gate
make cover     # coverage report, fails under threshold
make race      # go test -race -count=1 $(PKG) — e.g. make race PKG=./internal/engine/...
make lint-cross # golangci-lint for GOOS=darwin|linux|windows + GOOS=windows go vet
make vuln      # govulncheck ./...
make licenses  # go-licenses on all LICENSE_OSES, regenerates NOTICE, scope check
make build-all # CGO_ENABLED=0 cross-build of every release target
make next      # next eligible task + done/todo/blocked counts, derived from TASK_TRACKER.md
```

Always call the make target rather than the raw command with an env prefix
(`GOOS=windows go vet …`): the targets are what the permission allowlist covers, so an
unattended run never stalls on a prompt.

`make check` must pass before any commit. No exceptions, no `--no-verify`.

For how to actually launch and exercise a built binary, see §15.

---

## 9. Definition of Done

A task is done only when **all** hold:

- [ ] Acceptance criteria in `TASK_TRACKER.md` are all satisfied.
- [ ] `make check` is green.
- [ ] New logic has tests. Coverage on `internal/indexer/` and `internal/engine/` ≥ 75%;
      `internal/tui/` ≥ 50%.
- [ ] Exported identifiers have doc comments.
- [ ] No new `TODO` without a matching tracker task ID: `// TODO(T-042): ...`.
- [ ] `config.example.toml` and `README.md` updated if user-facing behaviour changed.
- [ ] The task's own PR carries the tracker update, exactly as the `TASK_TRACKER.md` Protocol
      lists it: block flipped to `done` with notes (≤ 10 lines), block moved verbatim to
      `docs/tracker-archive.md`, any `DEC-` entry (≤ 8 lines) in `docs/decisions.md` plus its
      index row, one `docs/session-log.md` line. Long evidence belongs in the PR body.

---

## 10. Git and issue protocol

**Origin:** `https://github.com/kdta91/tortui.git`
**Module path:** `github.com/kdta91/tortui`

- One task = one branch = one pull request. `TASK_TRACKER.md` is the backlog; do not also open
  GitHub issues, they would just be a second copy of the tracker to keep in sync.
- Branch: `task/T-0NN-short-slug`, created in the current working tree with `git switch -c`.
- **Do not create git worktrees.** The environment already runs each workspace in its own
  isolated worktree. Creating another inside it produces nested worktrees, confusing paths, and
  branches that are checked out twice. If parallel work is wanted, that's a second workspace,
  not a second worktree from inside this one.
- Conventional commits, scoped to the package:
  `feat(indexer): add torznab XML search client (T-012)`
  Types: `feat` `fix` `refactor` `test` `docs` `chore` `perf`.
- Commit at each green checkpoint, not once at the end. A commit that doesn't pass
  `make check` doesn't get made.
- PR body: task ID, tier, what changed, how it was verified, anything deferred.
- Open the PR with `gh pr create`; apply label `qa::pending`.
- **Never merge your own PR.** The orchestrator merges only after a separate reviewer passed it
  (§11). If review fails, remediate on the same branch — do not open a second PR.
- Never force-push a branch that has an open PR with review comments on it.
- Only a **Blocked** entry (§12) is pushed straight to `main`. Everything else, including the
  tracker update that finishes a task, goes through the task's PR. Do not enable "require a pull
  request before merging" on `main` — it would block the Blocked-entry push.

### Standing authorisation (owner-granted, DEC-103)

When the owner starts an autonomous run (the `START.md` prompt, or any instruction to run the
build), the following are pre-authorised and need no per-step confirmation, **within this
repository only**: creating `task/*` branches, committing and pushing them; opening PRs and
changing their labels; squash-merging a PR once a separate reviewer returned PASS at the PR's
current head SHA and every CI check passed; deleting merged task branches; pulling `main`;
pushing a Blocked entry to `main`; adding `T-9NN` backlog entries in a task's PR. Anything on
the owner-only list in §12 is **not** covered.

---

## 11. Execution loop

One orchestrator session delegates each task to a fresh **implementer** subagent and each PR to a
separate, fresh **reviewer** subagent. The orchestrator never writes code; the implementer never
reviews its own work. All agents share one worktree, so **exactly one agent touches it at a time**
and nobody runs `git switch` while another agent is working.

### Tiers

Every open task carries `tier:` in its block. The tier sets the agent, the budget, and how much
verification is enough to stop.

| Tier | Meaning | Implementer | Reviewer | Target budget |
|---|---|---|---|---|
| **H** | concurrency, security boundary, persistence, anything deleting or opening paths | `implementer-h` (Opus, high) | `reviewer-h` (Opus, high) | 30 min |
| **M** | features against `engine/fake`, adapters, settings, release plumbing | `implementer` (Sonnet, medium) | `reviewer` (Opus, medium) | 20 min |
| **L** | display-only or documentation | `implementer` (Sonnet, medium) | `reviewer` (Opus, medium) | 10 min |

Agent definitions live in `.claude/agents/`. **Escalation:** a tier M/L task that fails review twice,
or fails CI three times for the same cause, is re-run with `implementer-h`. Budgets are targets
for pacing, not stop conditions — the hard stop is §12's two hours.

### Verification — who proves what

Each gate is run by exactly one party. Re-running someone else's gate is waste, not rigour.

| Party | Runs | Enough to stop when |
|---|---|---|
| Implementer | `make check`; `make race PKG=<touched pkgs>`; `make cover` if it touched `internal/{indexer,engine,tui}`; `make lint-cross` **only** if it touched a build-tagged file; `make licenses` + `make vuln` **only** if `go.mod`/`go.sum` changed | every acceptance criterion has a test or quoted evidence, and those commands are green |
| CI | all three OSes' `make check`, `build-all`, `licenses` | every required check is `pass` (pending or skipped ≠ pass) |
| Reviewer | reads the diff against each acceptance criterion and §2/§6; **mutation-tests the load-bearing assertion** and quotes the failing output (required for H and M, optional for L); `gh pr checks <N> --watch` | a verdict per criterion, the mutation result, green CI, and the reviewed head SHA |
| Orchestrator | `gh pr view` / `gh pr checks` only | reviewer PASS at the current head SHA + all checks pass |

Tier H reviewers additionally look for data races, goroutine leaks (`goleak`), unreaped
per-object goroutines, and path containment on every create/open/delete.

### The loop

1. **Orchestrator:** `make next`, plus `gh pr list --state open`. An open task PR means a task is
   in flight — finish it first. Otherwise take the task `make next` names. None eligible → stop
   and say why.
2. **Orchestrator → implementer** for the task's tier. Brief: task id, tier, branch name
   `task/T-0NN-slug`. The standing rules are in the agent definition; do not repeat them.
3. **Implementer:** `git switch -c`; test first where behaviour is observable; smallest change that
   satisfies the criteria (no building ahead); its verification row above; the tracker update per
   the Protocol; commit, push, `gh pr create --label qa::pending`; report the PR URL, head SHA,
   elapsed minutes, and quoted command output. It does **not** wait for CI.
4. **Orchestrator → reviewer** (fresh agent) as soon as the PR exists — review overlaps CI.
5. **Reviewer:** checks out the branch, reviews, mutation-tests, restores the tree (`git status`
   clean), then waits on CI. Returns `PASS` or `FAIL` with numbered findings and the head SHA.
6. **PASS** → orchestrator confirms the SHA is still the PR head and checks pass, then
   `gh pr edit <N> --add-label qa::passed --remove-label qa::pending`,
   `gh pr merge <N> --squash --delete-branch`, `git switch main && git pull --ff-only`. Next task.
7. **FAIL** (including red CI) → orchestrator sends the findings to the **same implementer**
   (context intact) to fix on the same branch; then a **fresh** reviewer checks only the findings
   and the new commits. Back to step 6.
8. **BLOCKED** from any agent → orchestrator writes the Blocked entry (§12) on `main`, pushes,
   prints `BLOCKED: <task> <reason>`, and stops the run.

Progress updates are informational: no agent stops merely to report status. The session log
(`docs/session-log.md`, written in each task's PR) is what a human reads to catch up, and
records actual minutes per task so the tier budgets can be recalibrated.

---

## 12. Stop conditions

Halt, write the reason into the **Blocked** section of the tracker, and do not proceed to
another task:

- A task needs credentials, API keys, or an account you do not have.
- Acceptance criteria are ambiguous enough that two reasonable implementations differ materially.
- A required dependency is GPL/AGPL, unmaintained (>24 months), or has an open CVE.
- The same task fails `make check` three times consecutively for the same root cause.
- Completing a task would require violating §2 or §6.
- A change would alter the frozen contracts in §5.
- Total work on one task exceeds ~2 hours of wall clock without a green checkpoint.

When blocked: leave the branch pushed, mark the task `blocked`, state precisely what input would
unblock it, and move to the next **independent** task only if one exists. Otherwise stop cleanly.

### Owner-only actions — never taken autonomously

Stop and ask, even mid-run, for: pushing a tag or publishing a release (`git push --tags`,
`gh release`, a `goreleaser` publish); creating or changing any repository other than this one
(including the Homebrew tap and Scoop bucket); changing repository settings, branch protection, or
`gh` authentication; running `//go:build integration` tests or anything else against the live
network; manual steps needing a human at a real terminal (the T-094 matrix); anything that changes
§2, §3, the frozen §5 contracts, or the MPL-2.0 module set. Build everything up to that point,
merge it, and leave the owner-only step as a checklist in the task's notes.

---

## 13. Known hazards

- **anacrolix/torrent is chatty.** It logs to stderr by default and will corrupt the TUI. Wire
  its logger into the slog file sink in the very first engine task or you'll chase ghosts.
- **Trackerless magnets stall.** `Add` must return as soon as the magnet is accepted; metadata
  fetch happens async and the row shows `StateChecking` until the info dict arrives. Give it a
  60s timeout and surface `StateErrored` with a readable message, not a spinner forever.
- **Torznab category numbers vary.** Map indexer categories to the internal `Category` enum in
  the adapter, not in the TUI. Unknown categories map to `CategoryOther`, never dropped.
- **`goquery` selectors rot.** That's exactly why the scraper adapter is YAML-driven — selectors
  are data the user can fix without a rebuild. Do not hardcode selectors in Go.
- **Opening files cross-platform.** `xdg-open` / `open` / `rundll32`. Validate the path exists
  and is inside the configured download dir before shelling out. Never pass user text to a shell;
  use `exec.Command` with an argument slice.
- **Terminal resize during download.** Recompute layout on `tea.WindowSizeMsg`; cached widths in
  the table component are a common source of garbled output.
- **bbolt writes block.** Persist on a debounce (5s) from a dedicated goroutine, not per update.
- **Torrent file names are hostile input.** A `.torrent` can declare paths like `../../.ssh/`
  or `C:\Windows\...`. This is the zip-slip class of bug and it is the single most serious
  security defect this app could ship. Sanitize before writing, and re-check before deleting.
- **Two instances will corrupt each other.** Same store, same download dir, two engines on the
  same data. Take a lock file at startup and refuse to start twice rather than discovering it
  the hard way.
- **Unbounded concurrent torrents.** Adding twenty magnets starts twenty swarms, exhausts file
  descriptors and bandwidth, and makes every one of them slow. Queue beyond a configured limit.
- **Seeding surprises people.** A client that keeps uploading after completion without saying
  so burns a metered connection. Make the policy visible and configurable.
- **macOS file descriptor limits.** The soft `ulimit -n` is often 256. A torrent swarm exhausts
  that fast and the failure looks like random peer errors, not a limit. Raise the soft limit to
  the hard limit at startup on darwin and log the result.
- **macOS ships bash 3.2.** Any bash-4 syntax in a script works on the agent's Linux container
  and fails on the user's Mac. Write POSIX `sh` (§14).
- **APFS is case-insensitive by default.** A test that distinguishes `Foo.txt` from `foo.txt`
  passes in CI and fails on macOS.
- **Terminal.app has no truecolor.** Hardcoded hex colours silently collapse. Always go through
  the lipgloss profile.
- **Raw mode leaks on panic.** A crash that skips terminal restore leaves the user with a dead
  shell. Restore on every exit path — normal, signal, and panic.

---

## 14. Cross-platform targets and terminal compatibility

**Full text: [`docs/platforms.md`](docs/platforms.md)** — read it for any task with OS-, shell-, or
terminal-visible behaviour. The binding summary:

| OS | Arch | Tier |
|---|---|---|
| macOS 13+ | arm64, amd64 | 1 |
| Linux | amd64, arm64 | 1 |
| Windows 10+ | amd64, arm64 | 1 |

- All three are first-class. A feature that works on two of them is not done; the Windows path is
  implemented and tested in the same task, never deferred or stubbed.
- The binary is shell-agnostic: no per-shell code path, ever. Shell matters only in Makefile
  recipes (`SHELL := /bin/sh`, GNU make 3.81 — no `.ONESHELL`, no `$(file ...)`), scripts and hooks
  (`#!/usr/bin/env sh`, POSIX only — macOS ships bash 3.2), completions (generated for
  zsh/bash/fish), and README install steps.
- Terminal.app is the floor. Colour only through the lipgloss/termenv profile; honour `NO_COLOR`
  and `CLICOLOR_FORCE`; `TERM=dumb` or non-TTY stdout → one-line refusal, exit 1; ASCII glyph
  fallback (`--ascii`, `ascii = true`); widths via `rivo/uniseg`; restore the terminal on every exit
  path including `SIGINT`, `SIGTERM`, and panic.
- **Invariant:** every OS-specific line lives in `internal/platform` behind `_darwin.go`,
  `_linux.go`, `_windows.go` build tags. A `runtime.GOOS` switch anywhere else fails review.
- **Windows CI is the one that bites** (volume prefixes in `filepath.Abs`, symlink privilege
  `ERROR_PRIVILEGE_NOT_HELD`, CRLF). Native darwin `make check` does not lint other platforms'
  build-tagged files — run `make lint-cross` when you touch any.

---

## 15. Running and verifying a build

**Full text: [`docs/running.md`](docs/running.md)** — launch modes, manual smoke test, terminal
matrix. The binding summary:

- `./bin/tortui --version` · `./bin/tortui doctor` (environment report, safe to pipe) ·
  `./bin/tortui --demo` (full UI on `engine/fake` + fixture indexer, zero network — the primary way
  to eyeball rendering) · `make run` (real engine, `./dev-config.toml` sandbox).
- Use `TORTUI_HOME=$(mktemp -d)` for any manual run so real config is never touched.
- TUI rendering is covered by `teatest` golden files at 80×24, 120×40, and 60×20. A golden diff is a
  real failure — inspect it, never regenerate blindly.
- Anything needing a real swarm, real network, or a human at a real terminal is tagged
  `//go:build integration` or listed as a manual step — never run unattended (§12).

---

## 16. Licensing, legal posture, and distribution

**Full text: [`docs/licensing.md`](docs/licensing.md)** — the MPL-2.0 mechanism, the admitted
module table with reasons, the legal rationale for §2, and distribution channels. Read it for any
task that adds or bumps a dependency, touches `NOTICE`, the `Makefile` license variables, or
`scripts/check-license-scope*.sh`. The binding summary:

- Project license MIT. Dependencies MIT / Apache-2.0 / BSD / ISC, plus MPL-2.0 **only** for the
  enumerated `ALLOWED_MPL_MODULES` in the `Makefile` (DEC-098, DEC-099, DEC-100). GPL, AGPL, and
  LGPL are barred without exception. Widening the set needs a `DEC-` entry and an owner decision.
- `NOTICE` is generated by `make licenses`, never edited by hand. `go-llsqlite/adapter` stays at
  `v0.2.0`+; `anacrolix/utp` and `anacrolix/mmsg` are both required (selected by `CGO_ENABLED`).
- **Why §2 is hard law, not style:** liability for a tool turns on *inducement* (*MGM v. Grokster*)
  proven from the developer's own words and design choices — a bundled piracy source, a test
  fixture naming a real site, a category preset for pirated media. The youtube-dl takedown hinged
  on test strings naming copyrighted tracks. DMCA §1201 separately makes circumventing access
  controls unlawful even without infringement. An agent that adds a convenient default source has
  not added a feature; it has removed the defense.
- Releases are unsigned, built by `goreleaser` on tag. Publishing a release is owner-only (§12).
