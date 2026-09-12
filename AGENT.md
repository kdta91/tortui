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
| Language | Go 1.23+ | Fast compile loop, single static binary, stable APIs |
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
(MIT / Apache-2.0 / BSD only — **no GPL/AGPL**).

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
  1337x · added 14:02                                          [o]pen [f]older [p]ause [x] remove
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
```

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
- [ ] Tracker row updated: status → `done`, notes filled, decision log appended if a choice
      was made that a future reader would question.

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
- PR body: task ID, what changed, how it was verified, anything deferred. Link the issue with
  `Closes #N` so it closes on merge.
- Open the PR with `gh pr create`; apply label `qa::pending`.
- **Never merge your own PR.** Auto-merge is gated on `qa::passed` applied by the QA stage.
  If QA fails, remediate on the same branch — do not open a second PR.
- Never force-push a branch that has an open PR with review comments on it.
- Push tracker status flips (§11 step 2) straight to `main`. Everything else goes through a PR.
  Do not enable "require a pull request before merging" on `main` — it would block those flips
  and stall the loop on the first task.

---

## 11. Execution loop

Repeat until no eligible tasks remain:

1. Read `TASK_TRACKER.md`. Pick the lowest-numbered task where `status: todo` and every
   dependency is `done`. Never work two tasks at once.
2. Set `status: in-progress`, commit the tracker change to `main` before branching.
3. Create the branch with `git switch -c` (§10 — never `git worktree add`).
4. Write the test first where the task has observable behaviour.
5. Implement the smallest change that satisfies the acceptance criteria. Do not build ahead —
   if you find yourself implementing something that belongs to a later task, stop and leave it.
6. `make check`. Iterate until green.
7. Update the tracker row, decision log, docs.
8. Commit, push, open the PR with `gh pr create --label qa::pending`. Do not merge it.
9. Return to step 1.

Keep a running `docs/session-log.md` — one line per task with the timestamp, task ID, and
outcome. This is what a human reads to catch up.

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

### Support matrix

| OS | Arch | Tier | Meaning |
|---|---|---|---|
| macOS 13+ | arm64, amd64 | 1 | Primary development target. Verified by hand before release. |
| Linux | amd64, arm64 | 1 | Main CI target. Full test suite runs here. |
| Windows 10+ | amd64, arm64 | 1 | CI target. Verified by hand in Windows Terminal before release. |

All three are first-class. A feature that works on two of them is not done. Every task with
OS-visible behaviour needs its Windows path implemented and tested in the same task — not
deferred, not stubbed.

### Shell versus terminal — do not conflate these

The binary is **shell-agnostic**. `zsh`, `bash`, `fish`, `sh`, and `nushell` all launch it the
same way: it reads `argv` and the environment, takes over the TTY, and hands it back on exit.
Nothing in the application may depend on shell features, aliases, functions, or rc files.
There is no per-shell code path and no task should create one.

What actually varies between environments is the **terminal emulator**, and that is where all
compatibility effort goes.

Shell choice *does* matter in exactly four places:

1. **Makefile recipes.** Set `SHELL := /bin/sh` and `.SHELLFLAGS := -eu -c`. macOS ships GNU
   make 3.81, so no `.ONESHELL` (3.82+) and no `$(file ...)` (4.0+).
2. **Scripts and git hooks.** `#!/usr/bin/env sh`, POSIX only. macOS ships **bash 3.2** from
   2007 — no `declare -A`, no `mapfile`, no `${var^^}`, no `&>>`.
3. **Shell completions.** Generate for zsh, bash, and fish from the flag definitions so they
   cannot drift.
4. **Install instructions.** Cover zsh and bash `PATH` setup separately in the README.

### Terminal compatibility requirements

Must render correctly in: Terminal.app, iTerm2, Ghostty, WezTerm, Alacritty, kitty, tmux,
GNU screen, Windows Terminal, the VS Code integrated terminal, and over SSH.

- **Terminal.app is the floor.** `xterm-256color`, no truecolor, limited glyph coverage. If it
  looks wrong there it is wrong, regardless of how it looks in Ghostty.
- Colour comes from the lipgloss/termenv profile. Never emit a hardcoded escape sequence.
  Honour `NO_COLOR` and `CLICOLOR_FORCE`.
- `TERM=dumb` or a non-TTY stdout → do not start the TUI. Print a one-line explanation and
  exit 1. Piping the binary must not produce escape-sequence garbage.
- **Glyph fallback.** Block characters `█░` and the badge `✓` need an ASCII fallback set
  (`#`, `-`, `+`) chosen by capability detection, forceable with `--ascii` and
  `ascii = true` in config.
- **Width measurement uses `rivo/uniseg`.** Torrent titles contain CJK, emoji, and combining
  marks. `len()` and `utf8.RuneCountInString` both produce misaligned tables.
- Under tmux, `TERM=screen-256color` masks truecolor unless the user enables `Tc`. Detect,
  degrade silently, do not nag.
- Enter the alternate screen and enable bracketed paste on start; restore terminal state on
  **every** exit path including `SIGINT`, `SIGTERM`, and panic.

### Per-OS behaviour

**macOS**
- Config at `~/.config/tortui/` (honouring `XDG_CONFIG_HOME` when set), *not*
  `~/Library/Application Support` — see DEC-005.
- Default download dir `~/Downloads/tortui`.
- Open file: `open <path>`. Reveal in Finder: `open -R <path>`.
- Raise the soft file-descriptor limit to the hard limit at startup.
- First bind triggers the macOS firewall prompt — document this in the README so it does not
  read as a hang.
- Downloaded release binaries are quarantined by Gatekeeper; README documents
  `xattr -d com.apple.quarantine ./tortui` until notarisation is in place.

**Linux**
- Native XDG paths. Open file and folder: `xdg-open`.
- Default download dir `~/Downloads/tortui`, falling back to `$XDG_DOWNLOAD_DIR`.

**Windows**
- Config under `%AppData%\tortui\`. Open file: `explorer <path>`. Reveal:
  `explorer /select,<path>`.
- Enable virtual terminal processing at startup. Detect legacy conhost and print a message
  recommending Windows Terminal rather than rendering badly.
- All paths via `path/filepath`. Never concatenate with `/`.

**Invariant:** every OS-specific line lives in `internal/platform` behind `_darwin.go`,
`_linux.go`, `_windows.go` build tags. A `runtime.GOOS` switch anywhere else fails review.

---

## 15. Running and verifying a build

### The four ways to launch

```sh
make build                      # → bin/tortui

./bin/tortui --version          # 1. does it link and run at all
./bin/tortui doctor             # 2. environment report, no TUI, safe to pipe
./bin/tortui --demo             # 3. full UI, fake engine + fake indexer, zero network
make run                        # 4. real engine against ./dev-config.toml sandbox
```

**`doctor`** prints and exits: OS/arch, `TERM`, `COLORTERM`, detected colour profile, Unicode
capability, terminal size, resolved config/state/download paths, file-descriptor limit, and
each configured indexer with a reachability verdict. This is the first thing to run when
something looks wrong, and the first thing to ask a user for in a bug report.

**`--demo`** wires `engine/fake` and a fixture-backed indexer into the real TUI. Search returns
canned results, downloads progress on a simulated clock and include a stall, an error, and a
completion. Every screen, keybind, and dialog is reachable. No network, no disk writes outside
a temp dir, nothing to clean up. **This is the primary way to eyeball the UI after a build** and
the only way an agent can meaningfully self-verify rendering.

**`TORTUI_HOME`** redirects config, state, and downloads under one directory. Use it for any
manual testing so real config is never touched:

```sh
TORTUI_HOME=$(mktemp -d) ./bin/tortui
```

### Manual smoke test after a real build

```sh
make build
./bin/tortui doctor                                   # sanity
./bin/tortui --demo                                   # walk every screen
TORTUI_HOME=./tmp/smoke ./bin/tortui                  # first-run flow, add a source
```

Then one real download against a freely and officially distributed torrent — a current Debian
netinst ISO is the canonical target: small, always well-seeded, and unambiguously legal. Verify
progress advances, `o` opens the file, `f` reveals the folder, `p` pauses and resumes, restart
resumes from existing data, and `x` removes with and without data.

### Terminal matrix pass (before any release)

Run `--demo` in each and confirm alignment, colour, and clean exit:

```sh
./bin/tortui --demo                     # Terminal.app  ← the floor
./bin/tortui --demo                     # iTerm2 or Ghostty
tmux new-session ./bin/tortui --demo    # inside tmux
NO_COLOR=1 ./bin/tortui --demo          # monochrome
TERM=xterm ./bin/tortui --demo          # 16-colour
./bin/tortui --ascii --demo             # glyph fallback
printf '' | ./bin/tortui                # non-TTY → clean refusal, exit 1
```

Resize each to 80×24 and to ~60 columns while running; columns must drop in the documented
order without garbling.

Shell-agnosticism is confirmed once, not per feature:

```sh
zsh  -lc './bin/tortui --version'
bash -lc './bin/tortui --version'
sh   -c  './bin/tortui --version'
```

### Automated

```sh
make check                # fmt + lint + vet + unit tests — the commit gate
go test -race ./...       # concurrency
make cover                # thresholds from §9
make test-integration     # build-tagged, real network, manual
```

TUI rendering is covered by `teatest` golden files driven by `engine/fake`, at 80×24, 120×40,
and 60×20. A golden-file diff is a real failure — inspect the diff, do not regenerate blindly.

---

## 16. Licensing, legal posture, and distribution

*Not legal advice. This section exists so the agent understands why the §2 rules are hard
constraints rather than style preferences, and does not "helpfully" relax one.*

### Project license

MIT, in `LICENSE` at the repo root. Dependencies are restricted to MIT / Apache-2.0 / BSD /
ISC (§3) so the license stays clean and a `NOTICE` file can enumerate them accurately.
`go-licenses` runs in CI and fails the build on a copyleft dependency.

### Why the §2 rules exist

A BitTorrent client is lawful software. Transmission, qBittorrent, Deluge, libtorrent, and
aria2 all ship in Debian, Homebrew, and the Mac App Store. Building one is not the risk.

The risk is **inducement**. Under *MGM v. Grokster* (US Supreme Court, 2005), distributing a
tool with the object of promoting infringing use creates liability even where the tool has
lawful uses — and that object is proven with the developer's own words and design choices, not
with the code's capabilities. The evidence prosecutors and plaintiffs reach for is exactly the
kind of thing an agent might add without thinking: a bundled list of piracy sites, a test
fixture naming a real one, a README example pointing at one, a category preset for pirated
media, marketing that leans on infringing use.

This is not hypothetical. When the RIAA moved against **youtube-dl** on GitHub in 2020, one of
its strongest factual hooks was that the project's unit tests referenced specific copyrighted
tracks by name. GitHub restored the project after the EFF pushed back, on the reasoning that
code able to reach copyrighted works can also reach non-infringing ones and that the tool had
many legitimate purposes — but the maintainers stripped those test references as part of the
resolution. A handful of strings in a test file nearly cost the project its home.

So: the no-named-sites rule, the no-bundled-endpoints rule, the no-content-specific-logic rule,
and the no-auth-circumvention rule are the substantive legal posture of this project. They are
what makes tortui indistinguishable in kind from Prowlarr or Jackett — plumbing the user points
wherever they choose — rather than a curated piracy front-end. An agent that adds a convenient
default source has not added a feature; it has removed the defense.

**DMCA §1201** is a separate hazard from infringement. Circumventing an access control is
independently unlawful in the US even when no infringement follows, and it is the provision
under which takedowns against developer tools are usually filed. This is why §2 bars captcha
solving, paywall bypass, and credential workarounds absolutely, with no research or
convenience exception.

### Distribution

All channels are free and automated from `goreleaser` on tag:

| Channel | Cost | Mechanism |
|---|---|---|
| GitHub Releases | Free | Primary. Unlimited bandwidth on public repos. |
| `go install` | Free | Works from the module proxy with zero setup. |
| Homebrew tap | Free | A second public repo, `kdta91/homebrew-tap`. `goreleaser` commits the formula. |
| Scoop bucket | Free | A third public repo, `kdta91/scoop-bucket`, for Windows. |
| WinGet | Free | PR to `microsoft/winget-pkgs`; `goreleaser` can open it. Moderated, so expect delay. |
| GitHub Actions | Free | Unlimited minutes on public repos, including macOS and Windows runners. |

Homebrew *core* is a different thing from a tap and is not a target — it has notability
requirements and a maintenance burden a tap does not. `brew install kdta91/tap/tortui` works
from day one with no approval from anyone.

Release artifacts are unsigned. Code signing is the one thing here that costs money (Apple
Developer Program for notarisation; a certificate or signing service for Windows), and the
README documents the resulting OS warnings and their workarounds instead. Use Sigstore keyless
signing and GitHub build attestations, both free, for provenance — they do not suppress OS
warnings but they let anyone verify an artifact came from this repo's CI.
