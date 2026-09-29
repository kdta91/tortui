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
  one-request-per-query model (T-022): a search is one request, and this source needs no
  details-page fetch at add time either (T-9037 added one for sources that do).
- `item_size` (bytes) and `publicdate` (RFC 3339) are ordinary metadata fields returned by the
  same request, also verified live.
- **The `.torrent` address (T-9010).** Each result's `TorrentURL` is
  `download/<identifier>/<identifier>_archive.torrent` under the base URL, built by a field
  template from `identifier`. Both halves come from the Archive's own documentation (checked
  2026-09-29):
  - The Archive's developer documentation on items
    (`https://archive.org/developers/items.html`) gives every file of an item at
    `https://archive.org/download/<identifier>/<filename>`, and warns that such an address
    "may redirect to an actual server that contains the content".
  - The torrent the Archive generates for an item is the file `<identifier>_archive.torrent`:
    the Archive's own Python library, `internetarchive`, documents downloading item `nasa` as
    "downloaded nasa/nasa_archive.torrent" (the Downloading section of its Quickstart, published
    on Read the Docs and linked from the Archive's developer portal), and the Metadata API
    (`https://archive.org/metadata/{identifier}`) lists a file of that name under format
    `Archive BitTorrent` (the T-024 verification above).
  - The Archive's BitTorrent help page (`https://help.archive.org/help/archive-bittorrents/`)
    says its torrents "rely heavily on webseeding (download directly from our servers, when no
    peers have the files you are seeking)" and are tracked by `bt1.archive.org` and
    `bt2.archive.org`. That is why the URL matters: a magnet built from `btih` alone carries
    neither trackers nor web seeds, and in a real run every such download failed with
    "no peer supplied the torrent's info dictionary". The `.torrent` carries both, and the
    engine hands its web seeds to the client (`TestTorrentURLDownloadCompletesFromItsWebSeedWithNoPeers`),
    so an item downloads from the Archive's own servers even with zero peers.
  - The redirect the items page warns about goes to a storage host under `archive.org`. The
    engine's `.torrent` fetch follows a redirect to a subdomain of the requested host (and no
    other cross-host redirect, and never https to http) — DEC-136.

**Field mapping** (`internet-archive.yml`):

| Result field | Source | Notes |
|---|---|---|
| `ID` | `identifier` | The Archive's own stable item id. |
| `Title` | `title` | |
| `InfoHash` | `btih` | 40 lowercase hex, matches `normaliseInfoHash` exactly; used for duplicate detection and cross-source merging. |
| `TorrentURL` | `identifier`, templated | `/download/{{value}}/{{value}}_archive.torrent`, resolved against `base_url`. `Resolve` leaves a result with a `TorrentURL` unchanged, so the engine adds it by this URL, never by a bare-infohash magnet. |
| `SizeBytes` | `item_size` | Bytes, no unit suffix to parse. |
| `Published` | `publicdate` | RFC 3339, matched by the adapter's built-in layout list. |

**Fields deliberately left unmapped, and why:**

- `Magnet` — not mapped. T-024 left both links unmapped and relied on `Resolve` deriving a
  magnet from `btih`, because the schema then had no way to build a URL from a field value.
  That magnet carried no trackers and no web seeds, and in practice no Internet Archive
  download ever got its metadata. T-9010 added the general `template` field key and maps
  `TorrentURL` (above) instead.
- `SourceURL` — not mapped yet. The human-viewable details page is
  `https://archive.org/details/{identifier}`; the `template` key can now build it, which is
  backlog **T-9013**. Until then the `u` keybind (open source page) has nothing to open for
  this source; every other feature works normally.
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
definition of their own that does nothing they don't want. The composition root
(`internal/app`, T-095) registers every bundled source through `Merge` on every start, enabled
with no setup; a `[[indexer]]` entry in `config.toml` with a bundled source's `id` takes its
place, so `enabled = false` under that `id` turns the default off.

## What was deliberately not built here

- **A second, per-item HTTP request to resolve a magnet or details page.** `Adapter.Resolve`
  (T-022) made no network call by design; fetching a details page was backlog **T-939**, since
  built as the scraper's `details:` block (T-9037). Nothing about Internet Archive needed it,
  since `btih` is available directly from the search response, so its definition has none.
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
