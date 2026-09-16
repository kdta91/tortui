# Bundled lawful default sources

This is the T-024 accounting AGENT.md §2 and §16 require: what tortui ships compiled into the
binary, why each one qualifies, and how its definition was verified rather than guessed. Every
definition here is an ordinary scraper definition (`internal/indexer/scraper`), compiled into
the binary with `go:embed` from `internal/indexer/scraper/builtin/definitions/`, and enabled on
first run with no prompt and no configuration step — see that package's doc comment for how it
is loaded and merged with a user's own definitions.

## Internet Archive

**What it is.** The Internet Archive is a non-profit digital library. Since 2012 it has
automatically generated a BitTorrent distribution for public-domain and openly-licensed items
in its own collections, seeded and web-seeded from its own servers (`bt1.archive.org` /
`bt2.archive.org` and direct HTTP fallback). This is the Archive distributing its own content,
which is exactly the category AGENT.md §2 carves out.

**Why it qualifies (AGENT.md §2, §16).** The Archive is the operator of the collection and the
one generating and seeding the torrent. There is no infringement-oriented use case here: the
items available over BitTorrent are the Archive's own uploads, filtered in the definition below
to only the ones the Archive itself has produced a torrent for (`format:"Archive BitTorrent"`).

**Interface used, and how it was verified.** Every field this definition reads was checked
against a real, live request during this task, not inferred from a third-party tool or a forum
post:

- The search endpoint is the officially documented Advanced Search API,
  `https://archive.org/advancedsearch.php` (JSON output via `output=json`; the same behaviour is
  also described at `https://archive.org/services/search/v1`). It accepts a Lucene-style `q`,
  a comma-separated `fl` field list, `sort`, and `rows` — all used here exactly as documented.
- Requesting the `btih` field (verified live, 2026-09-16, against several items carrying
  `format:"Archive BitTorrent"`) returns that item's BitTorrent v1 infohash **directly in the
  search response** — the same value the Archive's separate, per-item Metadata API
  (`https://archive.org/metadata/{identifier}`) lists against a file named
  `{identifier}_archive.torrent`. Getting the infohash from the search response itself, rather
  than a second per-item request, is what makes this source work within the scraper framework's
  one-request-per-query model (T-022): the framework does not chain a details-page fetch, and
  this task does not extend it to (see "What was deliberately not built" below).
- `item_size` (bytes) and `publicdate` (RFC 3339) are ordinary metadata fields returned by the
  same request, also verified live.

**Field mapping** (`internet-archive.yml`):

| Result field | Source | Notes |
|---|---|---|
| `ID` | `identifier` | The Archive's own stable item id. |
| `Title` | `title` | |
| `InfoHash` | `btih` | 40 lowercase hex, matches `normaliseInfoHash` exactly; `Resolve` derives `Magnet` from it. |
| `SizeBytes` | `item_size` | Bytes, no unit suffix to parse. |
| `Published` | `publicdate` | RFC 3339, matched by the adapter's built-in layout list. |

**Fields deliberately left unmapped, and why:**

- `Magnet` / `TorrentURL` — not mapped directly. `InfoHash` is sufficient: `Adapter.Resolve`
  (T-022) derives a working magnet from any valid infohash, so the result is fully actionable
  without either. Mapping `TorrentURL` to a guessed `{identifier}_archive.torrent` filename
  would have required templating a URL out of two fields, which the scraper schema's field
  selectors cannot do (a selector reads one value; there is no field-concatenation transform) —
  building it would have meant hand-formatting a string the search response never actually
  returns, which is exactly the inference AGENT.md §16 says not to do. See DEC- entry below.
- `SourceURL` — not mapped, for the same reason: the human-viewable details page is
  `https://archive.org/details/{identifier}`, and the search response returns the bare
  `identifier`, not that path. Building it would again mean templating a URL from a field value,
  which this schema does not support. The `u` keybind (open source page) has nothing to open for
  this source; every other feature works normally. Revisiting this is backlog **T-945** (a
  field-concatenation or URL-template capability for the scraper schema would help every future
  source with the same shape, not just this one).
- `Seeders` / `Leechers` — not mapped. The Archive does not publish live swarm counts through
  this API; its own infrastructure is a permanent web seed rather than a conventional tracker
  swarm. A definition that filters on `MinSeeders > 0` will therefore see every Internet Archive
  result dropped locally — a known, documented limitation of this source rather than a bug.
- `Category`, `Uploader`, `Trust` — not mapped. `Caps.Categories` is `false` for every scraper
  source today (T-022's own limitation, backlog T-938), and the Archive's uploader field is not
  a badge of the kind `Trust` models — mapping it would misrepresent an ordinary contributor
  account as a trust signal AGENT.md §2 reserves for an indexer's own vetted-uploader badges.

**Modes.** Both `search` and `latest` are implemented against the same endpoint: `search`
filters on the user's keyword plus the BitTorrent-format restriction; `latest` drops the keyword
and sorts by `publicdate desc`, giving a genuine recent-additions feed with no query needed —
verified live against both modes (`TestInternetArchiveLiveSearchAndLatest`,
`//go:build integration`).

## Academic Torrents — evaluated, not bundled

AGENT.md §2 named Academic Torrents as a candidate to evaluate alongside the Internet Archive.
It was dropped rather than bundled, for two independent reasons found during verification
(2026-09-16), each sufficient on its own:

1. **No documented per-query search interface.** Academic Torrents' own published documentation
   (its "Searching" guide and its API reference) states plainly that there is no keyword-search
   HTTP endpoint. The documented `/apiv2/*`
   endpoints are for reading, uploading, and modifying a single known entry by infohash and for
   managing collections — none of them accept a keyword and return matches. The documented way
   to search is to download the entire `database.xml` dump and search it locally. Per AGENT.md
   §16, a source with no documented search interface is dropped rather than guessed at, and
   there is nothing to guess here: the maintainers say outright that server-side search does not
   exist.
2. **The documented workaround (mirror the database) is independently out of scope.** Even
   setting the first problem aside, doing what the docs recommend — fetching the full database
   and building a local, queryable copy of it — is exactly what AGENT.md §2 forbids under "no
   index of your own": tortui does not mirror another index or cache results across users. A
   per-query proxy in front of that same local copy would not change what it structurally is.

No definition, endpoint, hostname, or reference to Academic Torrents exists anywhere else in
this repository as a result — dropping a candidate means dropping it entirely, not partially
wiring it and leaving it disabled.

## User overrides

A user-supplied definition placed in the definitions directory
(`$XDG_CONFIG_HOME/tortui/definitions/`, loaded by `internal/indexer/scraper.Loader`, T-023)
with the same `id` as a bundled one **replaces** it — `builtin.Merge` in
`internal/indexer/scraper/builtin/builtin.go` resolves the override by `id`, user side winning.
This is how a broken bundled selector gets repaired by editing a text file rather than waiting
for a release, and how a user can disable a default entirely by pointing its `id` at a
definition of their own that does nothing they don't want. Wiring `Merge` into the running
registry and the first-run flow is the composition root's job (`internal/app`, not yet built —
T-090 and later), following the same "ship the package's full behaviour, defer main-wiring to
the task that has somewhere to wire it into" pattern T-002/T-003 used.

## What was deliberately not built here

- **A second, per-item HTTP request to resolve a magnet or details page.** `Adapter.Resolve`
  (T-022) makes no network call by design; extending it to fetch a details page is backlog
  **T-939**, opened before this task. Nothing about Internet Archive needed it, since `btih` is
  available directly from the search response, but a future bundled source that only publishes
  a details-page link and no direct infohash would need it. Not built here — out of this task's
  scope, and building it "for later" would be building ahead (AGENT.md §11.5).
- **A distro release-listing source.** AGENT.md §2's own examples list "distro release
  listings" alongside archives and dataset repositories. This task did not evaluate a specific
  one: the two named candidates (Internet Archive, Academic Torrents) already produced the
  "two or three, not breadth" target the acceptance criteria describe once Academic Torrents was
  ruled out, and adding a third candidate not named in the task would have been scope creep in
  the other direction. Left as an open candidate for a future task if a second bundled source is
  wanted, rather than picked under time pressure and under-verified.

## Legal notice

As required by AGENT.md §2: you are responsible for what you search for and download through
any source you add to tortui, including the bundled ones above, and for complying with the
terms of that source and the law of your jurisdiction.
