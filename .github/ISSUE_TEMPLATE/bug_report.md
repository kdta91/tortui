---
name: Bug report
about: Something in tortui doesn't work the way it should
title: ""
labels: bug
---

<!--
Before you paste anything below: this repo bars naming any indexer, site, or
source in issue text (see CONTRIBUTING.md and AGENT.md §2). Please do NOT
paste:

  - indexer URLs, hostnames, or site names (yours or anyone else's)
  - API keys, cookies, session tokens, or any other credential
  - torrent titles, magnet links, or infohashes

If a bug only reproduces against a specific indexer, describe it generically
(e.g. "a private Torznab-compatible indexer", "a scraper-defined source") and
say which capability/response shape is involved instead of naming or linking
the site. A maintainer may still ask for more detail privately if needed, but
issue text itself needs to stay site-name-free.
-->

## What happened

<!-- A clear, concise description of the bug. What did you expect instead? -->

## Steps to reproduce

1.
2.
3.

## Environment

**`tortui doctor` output** (if your build includes it): paste it here inside a
code block. `doctor` isn't implemented yet as of this writing — it lands in
T-055 — so if your build predates it, please give us instead:

- OS and architecture (e.g. macOS 14 / arm64, Windows 11 / amd64)
- **Terminal emulator and version** (e.g. iTerm2 3.5, Windows Terminal 1.19,
  Ghostty, tmux on top of X) — this matters a lot for rendering bugs
- `tortui --version` output
- Whether you're inside `tmux`/`screen`, and your `TERM` value

## Logs

<!--
tortui never writes to stdout/stderr after startup (AGENT.md §6) -- its log
file lives under your state directory (path reported by `tortui doctor` once
that exists). Paste the relevant lines, with any URL, key, or cookie value
redacted if you spot one that slipped past masking -- and tell us if it did,
since that's a bug in its own right.
-->

## Anything else

<!-- Screenshots, related issues, workarounds you've tried, etc. -->
