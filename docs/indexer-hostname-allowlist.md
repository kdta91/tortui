# Indexer hostname allowlist

This file is the single allowlist consulted by `scripts/check-indexer-hostnames.sh`, the CI
check (see the `indexer-hostnames` job in `.github/workflows/ci.yml`) that fails a pull request
if it introduces a hostname that looks like a newly bundled/default indexer source. It backs the
policy in `CONTRIBUTING.md`: pull requests adding indexer definitions, endpoints, or default
sources for specific sites are closed without review (AGENT.md §2, §16).

## What belongs here

Only the hostnames of the small set of **bundled lawful sources** tortui ships compiled into the
binary via `go:embed` — public archives, research dataset repositories, distro release listings
(AGENT.md §2). That set does not exist yet: it lands in **T-024**. Until then, this file lists no
indexer hosts at all. It exists now, in T-007, purely so:

- the CI check has one obvious, documented place to grow into instead of a scattered set of
  exceptions across the codebase, and
- T-024 has a single file to add to, each entry with a one-line comment naming which bundled
  source it is and pointing at `docs/bundled-sources.md` for the full justification.

## What does NOT belong here

- **Anything the user supplies themselves.** tortui ships no endpoint for user-supplied sources
  — there is nothing to allowlist for those, ever. A user's own indexer URL lives only in their
  own `config.toml` or their own scraper definition file, never in this repository.
- **Infrastructure or documentation hosts** (GitHub, the Go module proxy, this project's own
  docs, CI tool download URLs, and so on). The checker already treats plain, structurally
  non-resolving placeholders as always allowed without needing an entry here: `localhost`, a
  private/loopback/link-local IPv4 literal (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`,
  `127.0.0.0/8`, `169.254.0.0/16`), and the IANA-reserved `example.com` / `example.net` /
  `example.org` and the `.test` / `.invalid` / `.localhost` TLDs from RFC 2606. A **public** IP
  literal does not get this pass — unlike a reserved TLD, it can be a real production endpoint,
  so it needs an entry here like any other host. It also only ever looks at lines inside
  files where tortui actually defines indexer sources in the first place
  (`internal/indexer/**`, `config.example.toml`, this file, `docs/bundled-sources.md`, and
  `testdata/**` fixtures) or lines that mention `indexer`, `torznab`, `scraper`, or `base_url` —
  see `scripts/check-indexer-hostnames.sh`'s own header comment for the exact rule. A dependency
  URL in `NOTICE`, a link in `README.md`, or a CI action reference is never even scanned.

## Format

One hostname per line — no scheme, no path, no trailing slash — lowercase. A line starting with
`#` is a comment; blank lines are ignored. Listing a hostname here also allows its subdomains
(an entry of `example-bundled-source.org` would also allow `search.example-bundled-source.org`)
— list the most specific host that is actually needed by the bundled definition.

<!--
T-024 adds entries below this line, one per bundled lawful source, each with a one-line comment
naming which bundled source it is and a pointer to docs/bundled-sources.md for the full
"why it qualifies under AGENT.md §2" writeup. Do not add anything here that isn't one of those
bundled sources -- see "What does NOT belong here" above.
-->
