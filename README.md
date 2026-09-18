# tortui

A terminal UI for searching torrent indexers and downloading in one place. Search your
configured sources, compare seeders and leechers, add a torrent, and watch it download —
without leaving the terminal.

Single static binary that works on its own. No daemon, no indexer proxy, no Transmission or
qBittorrent behind it, nothing to install first — download it and you can search and download
straight away. Runs on **macOS, Linux, and Windows**.

> **Status:** under active development. Not all features described below are implemented yet —
> see [`TASK_TRACKER.md`](TASK_TRACKER.md) for current build state.

---

## What it does

- **Source-agnostic search.** Query multiple indexers at once and get one merged, deduplicated
  result list. No indexer is special-cased anywhere in the code.
- **Browse the latest.** Press `L` for recent additions across your sources with no keyword —
  useful when you want to see what's new rather than look for something specific.
- **Two ways to add a source.** Point it at any [Torznab](#option-a--torznab-recommended)
  endpoint (Prowlarr, Jackett, or anything else speaking the API), or write a small YAML
  definition for a site that doesn't have one.
- **Results you can actually read.** Title, size, seeders/leechers, uploader trust badge, age,
  and which source it came from. Sort on any column.
- **Built-in torrent engine.** Add a magnet or `.torrent` and it downloads in-process.
- **Pick where it goes.** Choose a destination per torrent when you add it — the default, a
  saved location, or any path you type. Validated for space and writability before it starts.
- **Download management.** Progress, rates, peers, ETA, queue position. Open the file, reveal
  the folder, open the source page, pause, resume, or remove with or without the data.
- **Minimal by design.** One accent colour, no boxes, readable at 80×24.

## What it deliberately doesn't do

tortui ships with a couple of **lawful default sources** — public archives and dataset
repositories that officially distribute over BitTorrent — so it works the moment you install it.
Beyond those, there are no built-in endpoints and no default source list. You add whatever else
you want, and tortui talks to exactly that.

It also doesn't build an index of its own. No DHT crawling, no infohash database, no mirroring
someone else's index. It queries the sources you point it at and shows you what they return.

It also contains nothing for working around a site's access controls — no captcha solving, no
paywall bypass, no credential sharing. Private indexers authenticate with an API key or cookie
**from your own account**, which you put in your own config file. That is the only auth path
and it is not going to grow another one.

### Legal notice

tortui is a BitTorrent client and search front-end. It does not host, index, or distribute any
content. **You are responsible for what you search for and download, and for complying with the
terms of service of any indexer you configure and with the copyright law of your jurisdiction.**
BitTorrent is widely used for lawful distribution — Linux ISOs, public-domain archives, game
patches, scientific datasets — and it is equally capable of infringing use. Which one you do
with it is on you.

---

## Install

### Prebuilt binary

Download the archive for your platform from the
[latest release](https://github.com/kdta91/tortui/releases/latest).

**macOS**

```sh
tar xzf tortui_*_darwin_arm64.tar.gz      # or _amd64 on Intel
sudo mv tortui /usr/local/bin/
xattr -d com.apple.quarantine /usr/local/bin/tortui   # see Troubleshooting
```

**Linux**

```sh
tar xzf tortui_*_linux_amd64.tar.gz
sudo mv tortui /usr/local/bin/
```

**Windows (PowerShell)**

```powershell
Expand-Archive tortui_*_windows_amd64.zip -DestinationPath "$env:LOCALAPPDATA\Programs\tortui"
$env:PATH += ";$env:LOCALAPPDATA\Programs\tortui"
```

To make the `PATH` change permanent, add that directory under
*Settings → System → About → Advanced system settings → Environment Variables*.

### Homebrew (macOS and Linux)

```sh
brew install kdta91/tap/tortui
```

### Scoop (Windows)

```powershell
scoop bucket add kdta91 https://github.com/kdta91/scoop-bucket
scoop install tortui
```

### Verifying a download

Every release artifact is signed with Sigstore and carries a GitHub build attestation:

```sh
gh attestation verify tortui_*.tar.gz --repo kdta91/tortui
```

This proves the binary was built by this repository's CI. It does **not** suppress the macOS
and Windows warnings described under [Troubleshooting](#troubleshooting) — releases are not
yet signed with an Apple or Windows certificate.

### From source

Requires Go 1.25 or newer.

```sh
go install github.com/kdta91/tortui/cmd/tortui@latest
```

Or clone and build:

```sh
git clone https://github.com/kdta91/tortui.git
cd tortui
make build          # → bin/tortui
```

On Windows, `make` targets work under Git Bash. Without make:

```powershell
go build -o bin\tortui.exe .\cmd\tortui
```

---

## Quick start

```sh
tortui --version    # confirm it runs
tortui doctor       # environment report — terminal, paths, limits, sources
tortui --demo       # full UI with fake data, no network, nothing to clean up
```

`--demo` is the fastest way to see whether tortui is worth your time and whether it renders
correctly in your terminal. It uses a simulated engine and canned results — nothing is
downloaded and nothing is written outside a temp directory.

When you're ready for real use:

```sh
tortui
```

On first run it writes a default config and tells you where. You'll need to add at least one
source before search does anything.

---

## Configuring a source

tortui works out of the box. A few sources are built into the binary and enabled on first run,
so you can search and download without configuring anything or installing anything else. They
cover public archives, research datasets, and distro releases — the material that's officially
distributed over BitTorrent.

Anything beyond that, you add yourself. Everything below is how.

**The easiest way is inside the app.** Press `5` for Settings, then `a` to add a source. You get
a form with validation as you type and a `t` key to test the connection before saving — no need
to touch a config file.

If you'd rather edit the file directly, it lives at:

| OS | Path |
|---|---|
| macOS | `~/.config/tortui/config.toml` |
| Linux | `~/.config/tortui/config.toml` (or `$XDG_CONFIG_HOME/tortui/`) |
| Windows | `%AppData%\tortui\config.toml` |

`tortui doctor` prints the resolved path if you're unsure.

### Option A — Torznab (recommended)

Torznab is a small HTTP API that torrent indexers and indexer proxies speak. It's an adaptation
of Newznab, which did the same job for Usenet, and it's the de-facto standard — **Prowlarr**,
**Jackett**, and **NZBHydra** all expose every source they support as a Torznab endpoint, and
tools like Sonarr and Radarr consume it.

That matters here: if you already run Prowlarr or Jackett, tortui works with everything you've
set up there, immediately, with no per-site work. Those tools handle the scraping and keep the
site definitions on your machine; tortui just speaks the API.

Grab the URL and key from Prowlarr's indexer page or Jackett's *Copy Torznab Feed* button and
paste the whole thing into the add form — if the URL already has `?apikey=...` on the end,
tortui splits it into the right fields for you. Or in the file:

```toml
[[indexer]]
id      = "my-indexer"
name    = "My Indexer"
type    = "torznab"
url     = "http://localhost:9696/api/v1/indexer/1/newznab"
api_key = "paste-your-own-key-here"
enabled = true
```

Settings also has an import option: point it at your aggregator once and pick which of its
indexers to add, rather than adding them one at a time.

### Option B — scraper definition

For a source with no Torznab endpoint, tortui uses a YAML definition describing where the fields
live on the page. You can import one from a file or URL via Settings → `a` → import, or write
your own. Definitions live in the `definitions/` directory next to your config:

| OS | Path |
|---|---|
| macOS / Linux | `~/.config/tortui/definitions/` |
| Windows | `%AppData%\tortui\definitions\` |

```toml
[[indexer]]
id         = "example"
name       = "Example"
type       = "scraper"
url        = "https://example.org"
definition = "example.yml"
enabled    = true
```

The add form in Settings picks up any definition file it finds in that directory, so once the
YAML is in place you can wire it up without editing TOML. `r` in Settings reloads definitions
after you've edited one.

Selectors live in the YAML rather than in compiled code, so when a site changes its markup you
fix a text file instead of waiting for a release. See
[`docs/indexer-definitions.md`](docs/indexer-definitions.md) for the schema and a worked example.

### Other settings

```toml
download_dir         = "~/Downloads/tortui"
saved_destinations   = ["~/Media", "/Volumes/External/torrents"]
max_active_downloads = 3     # further torrents queue
max_download_rate    = 0     # bytes/sec, 0 = unlimited
max_upload_rate      = 0
max_peers            = 50
listen_port          = 0     # 0 = pick a free port
seed_policy          = "ratio"   # ratio | duration | off
seed_ratio           = 1.0
min_free_space       = "1GB" # refuse to start a download that would cut it closer than this
search_timeout       = "15s" # per source; slow sources are dropped, not waited on
theme                = "default"
ascii                = false # force ASCII glyphs instead of block characters
```

All of these are editable in Settings — you never have to open this file.

The config file is written mode 0600 because it may hold API keys. Keep it that way, and don't
commit it anywhere.

---

## Keys

| Key | Action |
|---|---|
| `/` | Search |
| `L` | Latest — recent additions, no keyword |
| `R` | Refresh results |
| `tab` / `shift+tab` | Cycle screens |
| `1`–`5` | Jump to screen |
| `j` `k` / `↑` `↓` | Move selection |
| `enter` | Add torrent (results) · open details (downloads) |
| `d` | Details |
| `s` / `S` | Cycle sort column / reverse |
| `o` | Open downloaded file |
| `f` | Open containing folder |
| `u` | Open source page in browser |
| `p` | Pause / resume |
| `x` | Remove (asks whether to keep or delete data) |
| `?` | Help |
| `q` / `ctrl+c` | Quit |

### Trust badges

Some indexers mark uploads from vetted accounts. Where a source reports this, it shows in the
Trust column:

| Badge | Meaning |
|---|---|
| `VIP` | The source's highest uploader trust tier |
| `TR` | Trusted uploader |
| `✓` | Upload verified |
| *(blank)* | No trust information, or the source doesn't report any |

This is **reputation metadata reported by the indexer, nothing more**. It is not a quality
guarantee, it says nothing about what a torrent actually contains, and it has no effect on
download behaviour — it's a column you can sort and filter on.

---

## Terminal support

tortui works the same under **zsh, bash, fish, sh, and PowerShell** — it takes over the terminal
on start and never touches your shell's config. Your shell is not a compatibility concern.

Your **terminal emulator** is. Verified on Terminal.app, iTerm2, Ghostty, WezTerm, Alacritty,
kitty, Windows Terminal, the VS Code integrated terminal, tmux, GNU screen, and over SSH.

- Colour degrades automatically: truecolor → 256 → 16 → monochrome. `NO_COLOR=1` forces
  monochrome.
- `--ascii` swaps block glyphs for plain ASCII if your font renders them badly.
- Piping output or running with `TERM=dumb` prints a message and exits rather than emitting
  escape-sequence garbage.
- Minimum usable size is 80×24. Below that, columns drop right-to-left rather than wrapping.

**Windows:** use [Windows Terminal](https://aka.ms/terminal). Legacy `conhost` (the old console
window) can't render the UI properly, and tortui will tell you so instead of trying.

---

## Troubleshooting

Run `tortui doctor` first. It reports your terminal capabilities, resolved paths, file-descriptor
limits, and whether each configured source is reachable — which covers most problems. Its output
is plain text and safe to paste into an issue; credentials are masked.

**macOS: "cannot be opened because the developer cannot be verified"**
Gatekeeper quarantines downloaded binaries. Releases aren't notarised yet:
```sh
xattr -d com.apple.quarantine /usr/local/bin/tortui
```

**macOS: a firewall prompt appears on first download**
Expected — the torrent engine binds a port for incoming peer connections. Allow it, or peers can
only connect outbound and downloads will be slower.

**macOS: downloads stall with peer errors**
The default open-file limit (often 256) is too low for a busy swarm. tortui raises it at startup;
`doctor` shows the before and after. If it's still low, raise the hard limit with `ulimit -n`.

**Search returns nothing**
Check Settings (`5`) → select the source → `t` to test it. Distinguishes *unreachable* from
*auth failed* from *parse failed*. Auth failures mean your API key or cookie is wrong or expired.
Parse failures on a scraper source usually mean the site changed its markup — update the YAML.

**Some sources fail but others work**
Intended. A failing source never blocks the rest; the status bar shows `2/4 sources failed` and
`tab` expands the detail.

**Columns are misaligned or the table looks garbled**
Usually a font without block-glyph coverage. Try `tortui --ascii`. If it happens after resizing,
that's a bug — please file it with `doctor` output and your terminal name.

**Colours look flat inside tmux**
tmux masks truecolor by default. Add to `~/.tmux.conf`:
```
set -ga terminal-overrides ",xterm-256color:Tc"
```

**The terminal is broken after a crash**
Run `reset`. tortui restores terminal state on exit, including on signals — if a crash left it
in raw mode, that's a bug worth reporting.

---

## Development

```sh
git clone https://github.com/kdta91/tortui.git
cd tortui
make check      # fmt + lint + vet + tests — the commit gate
make build      # → bin/tortui
make run        # run against ./dev-config.toml
make build-all  # cross-compile all six OS/arch targets
```

Use a scratch directory so development never touches your real config:

```sh
TORTUI_HOME=$(mktemp -d) ./bin/tortui
```

Contributor notes:

- All scripts are POSIX `sh` — macOS ships bash 3.2, so bash-4 syntax breaks there even though
  it passes in CI containers.
- Unit tests make no network calls. Indexer tests replay fixtures from `testdata/`.
- A pre-commit hook scans for credentials. Never commit a real `config.toml` or a definition
  containing a key.
- [`AGENT.md`](AGENT.md) holds the architecture, invariants, and coding standards.
  [`TASK_TRACKER.md`](TASK_TRACKER.md) is the build plan and current state.

## License

MIT. See [`LICENSE`](LICENSE) — this project's own license is unchanged. Third-party Go module
dependencies and their licenses are listed in [`NOTICE`](NOTICE), generated by `make licenses`.
Dependencies are otherwise restricted to permissive licenses (MIT / Apache-2.0 / BSD / ISC),
with one exception, admitted for a **named set of ten modules**: `github.com/anacrolix/torrent`,
the torrent engine this project depends on, is MPL-2.0, as are the sibling libraries it compiles
against (`anacrolix/dht/v2`, `generics`, `log`, `mmsg`, `multiless`, `sync`, `upnp`, `utp`) and
`github.com/go-llsqlite/adapter`, which its storage layer reaches. MPL-2.0 is a file-level
copyleft license whose obligations attach to those dependencies' own source files and do not
extend to tortui's MIT-licensed code. See [`AGENT.md`](AGENT.md) §16 for the full list, the
reasoning and the scope. (None of them is a `go.mod` dependency as of this writing — they land
with the engine implementation — so they do not yet appear in `NOTICE`.)

**How the named exception is actually enforced.** `go-licenses check --allowed_licenses`,
which `make licenses` runs, is a global license allowlist with no per-module scoping — putting
MPL-2.0 on that list makes it pass for *any* MPL-2.0 module, not only the ten admitted ones. The
narrowness is enforced by a second, separate check: `scripts/check-license-scope.sh` reads the
same `go-licenses report` data `NOTICE` is built from and fails the build, naming the offender,
if any MPL-2.0 row belongs to a module outside that enumerated set — including a module sharing
the `github.com/anacrolix/` path, since the set is a list of names and deliberately not a
wildcard. Both checks run on every `make licenses` invocation, for all three tier-1 operating
systems. GPL, AGPL and LGPL are not on the allowlist and fail the build on sight. See DEC-099 for
the residual gap this still leaves (the allowlist itself stays global; only the second check is
scoped) and DEC-100 for why the set is ten modules rather than one.
