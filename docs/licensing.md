# Licensing, legal posture, and distribution

Moved **verbatim** from AGENT.md §16 by T-945 so the operating contract stays short.
AGENT.md keeps the binding summary; this file is the full text and is equally binding.
Read it whenever a task touches this area.

## 16. Licensing, legal posture, and distribution

*Not legal advice. This section exists so the agent understands why the §2 rules are hard
constraints rather than style preferences, and does not "helpfully" relax one.*

### Project license

MIT, in `LICENSE` at the repo root, and unchanged by anything below. Dependencies are
restricted to MIT / Apache-2.0 / BSD / ISC, plus one named exception, MPL-2.0, admitted for an
enumerated set of module paths (§3, DEC-098, DEC-100) so a `NOTICE` file can enumerate them
accurately. `go-licenses` runs in CI and still fails the build on any other copyleft dependency —
the allowlist gained one entry, not a category.

**The admitted set**, authoritative copy in the `Makefile`'s `ALLOWED_MPL_MODULES`:

| Module | Why it is in the set |
|---|---|
| `github.com/anacrolix/torrent` | The locked torrent engine itself (§3, DEC-001, DEC-098). |
| `github.com/anacrolix/dht/v2` | Compile-time import of the engine — peer discovery. |
| `github.com/anacrolix/generics` | Compile-time import of the engine — generic containers. |
| `github.com/anacrolix/log` | Compile-time import of the engine — its logging façade. |
| `github.com/anacrolix/mmsg` | Reached on any **cgo-enabled** build via `anacrolix/go-libutp` (itself MIT). |
| `github.com/anacrolix/multiless` | Compile-time import of the engine — multi-key comparison. |
| `github.com/anacrolix/sync` | Compile-time import of the engine — instrumented sync primitives. |
| `github.com/anacrolix/upnp` | Compile-time import of the engine — port mapping. |
| `github.com/anacrolix/utp` | The pure-Go uTP transport, used whenever `CGO_ENABLED=0`, on every `GOOS`. |
| `github.com/go-llsqlite/adapter` | Reached through `anacrolix/torrent/storage`; MPL-2.0 from `v0.2.0` on — `v0.1.0` and the earlier pseudo-version ship no LICENSE, so **T-031 must pin `v0.2.0` or later**. |

All ten are MPL-2.0, unmodified, and unavoidable: the set was derived from
`go-licenses report ./...` run over all three `LICENSE_OSES` with the engine in `go.mod`, not from
a guess. No single report contains all ten — `utp` and `mmsg` are alternative uTP transports, and
which one is in the graph is decided by **`CGO_ENABLED`, not by `GOOS`**: measured over all six
combinations, cgo-enabled builds pull `mmsg` (via `anacrolix/go-libutp`) on darwin, linux *and*
windows alike, and cgo-disabled builds pull `utp` on all three. Darwin only looks special because
on a Mac `GOOS=darwin` is the native target, where cgo defaults on, while the other two are
cross-compiled with it off; on the Linux CI runner it is `GOOS=linux` that is native. Neither
`make licenses` nor `ci.yml` pins `CGO_ENABLED` (only `build-all` does, at `CGO_ENABLED=0`), so
both modules must be listed or the gate fails on one host or the other. The set is therefore the
union across build configurations, not across operating systems. It is a **set of module names,
not a license family and not a path prefix**: `github.com/anacrolix/*` would be shorter and would
silently admit any future module published under that path, which is precisely the
reviewed-decision property this gate exists to preserve.

**The named scope is a two-part mechanism, and only one part is a true allowlist.**
`go-licenses check --allowed_licenses=...` (`Makefile`'s `ALLOWED_LICENSES`) has no per-module
targeting flag — confirmed against `go-licenses check --help` (v2.0.1): `--allowed_licenses` is a
flat list of license names, full stop. Once MPL-2.0 is in that list, the check by itself would
pass **any** MPL-2.0 module, present or future, not only the ones named here. The narrowness is
enforced by a second, independent check on the same data: `scripts/check-license-scope.sh` reads
the CSV `go-licenses report` already produces (the same data `make licenses` merges into `NOTICE`
— no second scan) and fails the build, naming the offender, if any MPL-2.0 row belongs to a
module outside `ALLOWED_MPL_MODULES`. It runs on all three `LICENSE_OSES`, and its own test
(`scripts/check-license-scope_test.sh`, wired into `make check`/`test-scripts`) proves every
listed module passes, an unlisted MPL-2.0 module still fails and is named — including one sharing
the `github.com/anacrolix/` prefix — and a partially-listed set fails on exactly its unlisted
members. **The residual gap, unchanged by widening the set:** `ALLOWED_LICENSES` itself remains a
global list — a future MIT/Apache-2.0/BSD/ISC dependency is still checked only against that flat
list, which is correct, since the per-module check exists solely to narrow MPL-2.0, the one
license family admitted by name rather than by permissiveness. Nothing wider than that is scoped
or needs to be. See DEC-099 for the full accounting and DEC-100 for the widening.

**Why MPL-2.0 and not GPL/AGPL.** MPL-2.0 is *file-level* ("weak") copyleft: its obligations
attach to the individual source files that carry the MPL notice, not to every file that is
merely compiled or linked alongside them. Modifying one of those files and distributing the
result requires releasing that file's source under MPL-2.0 (§3.1); distributing tortui as a
compiled binary that includes unmodified code from the admitted modules requires only that
recipients be told where that Covered Software's source is available (§3.2) — which `NOTICE`'s
`license_url` column already does for every dependency, MPL-2.0 or not. Critically, MPL-2.0
explicitly permits combining Covered Software with code under other licenses into a "Larger
Work" (§3.3) without pulling that other code under MPL. It does **not** reach tortui's own
MIT-licensed source, and tortui accepts no obligation to publish source it would not publish
anyway. GPL and AGPL are *strong* copyleft: they extend to the whole combined work (GPL) or to
network use of the whole combined work (AGPL), which would force tortui's own source under
GPL/AGPL terms and is exactly what §3's "no GPL/AGPL" line exists to keep out. That distinction
— not a general softening on copyleft — is why this exception names a fixed set of modules and
one license family rather than widening the gate. **LGPL is barred for the same reason as GPL**
and is not a borderline case here: it was proven empirically to fail `go-licenses check` under
the current `ALLOWED_LICENSES` (DEC-100), including against the real `github.com/juju/ratelimit`,
the LGPL-3.0 module that surfaced while evaluating a replacement engine. **Not legal advice**;
see DEC-098 for the original authorisation and DEC-100 for the widened set.

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
