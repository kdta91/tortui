# Integration test suite (T-091)

The `//go:build integration` suite proves the things a unit test cannot: a
real end-to-end download over the real BitTorrent network, resume across a
process restart, and that every bundled source is still reachable and
still shaped the way its adapter expects. It is intentionally kept out of
`make check` (AGENT.md §6.7 — unit tests make zero network calls) and is
never run unattended by an agent (AGENT.md §12) — only the owner runs it,
by hand or by dispatching the manual CI job.

## What it covers

- **`internal/indexer/scraper/builtin`** — `TestBundledSourcesAreReachable`
  probes every bundled source definition (today: Internet Archive) with a
  live search, so a rotted selector or a field a source stopped returning
  is caught here instead of by a user's first search.
  `TestInternetArchiveLiveSearchAndLatest` does the same with stronger,
  source-specific assertions.
- **`internal/app`** — `TestZeroConfigStandaloneSearchAddDownload` is the
  executable form of the standalone contract (AGENT.md §1): on an empty
  `$TORTUI_HOME` with no user configuration, it loads defaults, merges in
  the bundled definitions, runs a keyword-less Latest search, adds the
  smallest suitable live result to a real engine, and drives it to
  completion. It then saves the torrent's resume data, closes that engine,
  and restores the torrent into a brand new engine instance with every
  network subsystem disabled (`Offline: true`) — since that engine can
  never make a network call, a restored torrent that immediately reports
  complete proves the data was verified from disk, not re-downloaded. This
  is the release-criteria item "a fresh install searches and downloads
  successfully with no configuration... verified on all three OSes."

## Prerequisites

- Outbound internet access: HTTP(S) to the bundled source's API, and
  BitTorrent traffic (TCP and UDP, DHT bootstrap included) to whatever
  peers a magnet with no embedded tracker discovers via DHT.
- No firewall or VPN blocking outbound BitTorrent ports. A restrictive
  corporate network is the most common cause of a hang or timeout here
  that is not a real regression.
- Nothing else required — no account, no API key, no software besides the
  Go toolchain. That absence is exactly what the standalone-contract test
  is proving.

## Running it

```sh
make test-integration
```

Runs the whole module with the `integration` build tag and a 20-minute
timeout (`INTEGRATION_TIMEOUT` in the `Makefile`). The download step alone
budgets up to 10 minutes (`downloadCompleteTimeout` in
`internal/app/standalone_integration_test.go`) since DHT peer discovery for
a trackerless magnet is not instant even for a well-seeded item; the
reachability checks are bounded to 20 seconds each. A full run typically
finishes in a couple of minutes when the network cooperates, and is bounded
well under the 20-minute cap when it does not.

## CI

The manual `integration` job in `.github/workflows/ci.yml` runs this same
target on `ubuntu-latest`. It only runs when someone dispatches the
workflow by hand (`workflow_dispatch`) — never on a push or a PR — since it
makes real network calls and its outcome depends on conditions outside this
repository's control (a source's current catalogue, DHT reachability from
GitHub's runner network). It is not part of the required-check set for
merging (AGENT.md §11).
