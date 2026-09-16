// Package fake is a fixture-backed, deterministic implementation of
// indexer.Indexer. It exists for the same reason internal/engine/fake
// exists (see that package's doc comment): --demo mode (AGENT.md §15) and
// any test that wants realistic, varied search results needs one without a
// network call, a real indexer account, or a source that could go away.
//
// Nothing here makes an HTTP request, reads a file, or blocks. Search and
// Resolve return canned data (or a canned error) synchronously except for
// honouring ctx cancellation, so a caller — the registry's fan-out, a TUI
// test, --demo's composition root — exercises the exact same code path it
// would against a real adapter, with zero network (AGENT.md §6.7).
//
// Fixture titles, uploader names, and source URLs are all invented or use
// example.org; none names a real service, and nothing here is source-
// specific logic outside this package (AGENT.md §1, §2, §16).
package fake

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/kdta91/tortui/internal/indexer"
)

// ErrDemoSourceDown is the canned failure a failing Indexer's Search always
// returns. It stands in for whatever actually breaks a real adapter —
// unreachable host, rate limit, malformed response — without claiming any
// of those specifically, and it names no real service: example.org is the
// project's standing placeholder domain (AGENT.md §2, §16).
var ErrDemoSourceDown = errors.New("example.org: connection refused (demo fixture — source deliberately fails)")

// Indexer is an in-memory indexer.Indexer that returns a fixed slice of
// indexer.Result values (or a fixed error) and never performs I/O. It is
// safe for concurrent use: Search and Resolve only read the fields New/
// NewFailing set, never mutate them.
type Indexer struct {
	id      string
	name    string
	caps    indexer.Caps
	results []indexer.Result
	err     error
}

// New returns an Indexer that always succeeds, returning a defensive copy
// of results filtered by q (see filterResults) from every Search call.
func New(id, name string, caps indexer.Caps, results []indexer.Result) *Indexer {
	return &Indexer{id: id, name: name, caps: caps, results: results}
}

// NewFailing returns an Indexer whose Search always fails with err (or
// ErrDemoSourceDown when err is nil). It still declares real Caps — a
// source that fails is not the same thing as a source that lacks a
// capability (AGENT.md §6.3 draws exactly this distinction) — so the
// registry actually queries it, and fails, rather than skipping it.
func NewFailing(id, name string, err error) *Indexer {
	if err == nil {
		err = ErrDemoSourceDown
	}

	return &Indexer{
		id:   id,
		name: name,
		caps: indexer.Caps{Search: true, Latest: true},
		err:  err,
	}
}

// ID returns the registry key this Indexer was constructed with.
func (f *Indexer) ID() string { return f.id }

// Name returns the human-readable name this Indexer was constructed with.
func (f *Indexer) Name() string { return f.name }

// Caps returns the capabilities this Indexer was constructed with.
func (f *Indexer) Caps() indexer.Caps { return f.caps }

// Search returns a filtered copy of the fixture results, or the configured
// failure. It honours ctx cancellation like a real adapter must (AGENT.md
// §6.2), even though nothing here can actually block.
func (f *Indexer) Search(ctx context.Context, q indexer.Query) ([]indexer.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if f.err != nil {
		return nil, fmt.Errorf("fake %s: %w", f.id, f.err)
	}

	return filterResults(f.results, q), nil
}

// Resolve is a no-op: every fixture Result already carries a Magnet, so
// there is nothing to fill in. It still honours ctx cancellation.
func (f *Indexer) Resolve(ctx context.Context, r indexer.Result) (indexer.Result, error) {
	if err := ctx.Err(); err != nil {
		return r, err
	}

	return r, nil
}

// filterResults applies the subset of Query a fixture source can honour
// locally: MinSeeders, then Limit. Mode/Text/Categories are not filtered on
// — the fixtures are deliberately returned regardless of keyword, the same
// as a real adapter would be free to do server-side, and doing it here
// would make the demo dataset keyword-fragile for no benefit. The result is
// always a fresh slice, never an alias into the fixture backing array, so a
// caller can't mutate a shared fixture.
func filterResults(in []indexer.Result, q indexer.Query) []indexer.Result {
	out := make([]indexer.Result, 0, len(in))

	for _, r := range in {
		if q.MinSeeders > 0 && r.Seeders < q.MinSeeders {
			continue
		}

		out = append(out, r)
	}

	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}

	return out
}

// demoEpoch anchors every fixture's Published time so results, and any
// derived "age" rendering, are identical on every run (AGENT.md §6.7 —
// deterministic, no wall-clock dependency baked into fixture data).
var demoEpoch = time.Date(2026, time.January, 15, 12, 0, 0, 0, time.UTC)

// ArchiveResults is the primary demo/test fixture set: it alone spans every
// indexer.Trust value, a wide CJK title, an emoji title, a huge size, and a
// zero-seeder entry, so a caller that only registers this one source still
// gets the full variety T-056's acceptance criteria require. IndexerID is
// always "demo-archive".
//
// Titles are invented compilation/dataset names; none names a real work,
// site, or media title (AGENT.md §2, §16).
func ArchiveResults() []indexer.Result {
	const src = "demo-archive"

	return []indexer.Result{
		{
			IndexerID: src, ID: "arc-1",
			Title:     "Regional Cloud Cover Dataset (1998-2024)",
			Magnet:    demoMagnet("Regional Cloud Cover Dataset (1998-2024)"),
			SizeBytes: 1_400_000_000, Seeders: 42, Leechers: 3,
			Category: indexer.CategoryData, Published: demoEpoch.AddDate(0, 0, -1),
			Uploader: "archivist-01", Trust: indexer.TrustUnknown,
			SourceURL: "https://example.org/demo-archive/arc-1",
		},
		{
			IndexerID: src, ID: "arc-2",
			Title:     "Community Radio Broadcast Recordings Vol. 3",
			Magnet:    demoMagnet("Community Radio Broadcast Recordings Vol. 3"),
			SizeBytes: 780_000_000, Seeders: 12, Leechers: 1,
			Category: indexer.CategoryAudio, Published: demoEpoch.AddDate(0, 0, -3),
			Uploader: "radiofan", Trust: indexer.TrustNone,
			SourceURL: "https://example.org/demo-archive/arc-2",
		},
		{
			IndexerID: src, ID: "arc-3",
			Title:     "Open Firmware Toolchain Snapshot 2026.01",
			Magnet:    demoMagnet("Open Firmware Toolchain Snapshot 2026.01"),
			SizeBytes: 2_100_000_000, Seeders: 88, Leechers: 6,
			Category: indexer.CategorySoftware, Published: demoEpoch,
			Uploader: "buildbot", Trust: indexer.TrustVerified,
			SourceURL: "https://example.org/demo-archive/arc-3",
		},
		{
			IndexerID: src, ID: "arc-4",
			// Wide CJK title: exercises grapheme-aware width measurement
			// (rivo/uniseg, AGENT.md §14) in the results table — this is a
			// dataset name, not a media title.
			Title:     "気象観測データセット・アーカイブ集成 全巻",
			Magnet:    demoMagnet("kishou-kansoku-dataset-archive"),
			SizeBytes: 512_000_000, Seeders: 25, Leechers: 2,
			Category: indexer.CategoryData, Published: demoEpoch.AddDate(0, 0, -7),
			Uploader: "hozon-kai", Trust: indexer.TrustTrusted,
			SourceURL: "https://example.org/demo-archive/arc-4",
		},
		{
			IndexerID: src, ID: "arc-5",
			// Emoji title: exercises the same width path for a different
			// Unicode category (wide-but-narrow-cell emoji vs. CJK
			// full-width), plus the trust badge's highest tier.
			Title:     "🚀 Space Mission Telemetry Archive 🛰️ Complete Set",
			Magnet:    demoMagnet("space-mission-telemetry-archive"),
			SizeBytes: 96_000_000, Seeders: 150, Leechers: 20,
			Category: indexer.CategoryData, Published: demoEpoch.AddDate(0, 0, -2),
			Uploader: "orbital-uploader", Trust: indexer.TrustVIP,
			SourceURL: "https://example.org/demo-archive/arc-5",
		},
		{
			IndexerID: src, ID: "arc-6",
			// Huge size: a multi-terabyte satellite imagery bundle.
			Title:     "Complete Regional Satellite Imagery Archive 2010-2026",
			Magnet:    demoMagnet("complete-regional-satellite-imagery-archive"),
			SizeBytes: 2_500_000_000_000, Seeders: 9, Leechers: 4,
			Category: indexer.CategoryData, Published: demoEpoch.AddDate(0, -1, 0),
			Uploader: "geo-mirror", Trust: indexer.TrustTrusted,
			SourceURL: "https://example.org/demo-archive/arc-6",
		},
		{
			IndexerID: src, ID: "arc-7",
			// Zero seeders: a dead-looking entry, still a valid result.
			Title:     "Archived Utility Collection Vol. 9 (unseeded)",
			Magnet:    demoMagnet("archived-utility-collection-vol-9"),
			SizeBytes: 45_000_000, Seeders: 0, Leechers: 0,
			Category: indexer.CategorySoftware, Published: demoEpoch.AddDate(0, -6, 0),
			Uploader: "old-mirror", Trust: indexer.TrustNone,
			SourceURL: "https://example.org/demo-archive/arc-7",
		},
	}
}

// MirrorResults is a second, smaller fixture set from a second, distinct
// indexer id ("demo-mirror") with narrower Caps (no Latest) and its own
// tiny-size entry — so the registry's per-capability skip path (AGENT.md
// §6.3) and multi-source merge both have something real to exercise, not
// just ArchiveResults alone.
func MirrorResults() []indexer.Result {
	const src = "demo-mirror"

	return []indexer.Result{
		{
			IndexerID: src, ID: "mir-1",
			// Tiny size: a single small text file.
			Title:     "example.org Mirror Index README",
			Magnet:    demoMagnet("example-org-mirror-index-readme"),
			SizeBytes: 512, Seeders: 3, Leechers: 0,
			Category: indexer.CategoryText, Published: demoEpoch.AddDate(0, 0, -10),
			Uploader: "mirror-admin", Trust: indexer.TrustUnknown,
			SourceURL: "https://example.org/demo-mirror/mir-1",
		},
		{
			IndexerID: src, ID: "mir-2",
			Title:     "Historical Weather Station Log Bundle",
			Magnet:    demoMagnet("historical-weather-station-log-bundle"),
			SizeBytes: 63_000_000, Seeders: 7, Leechers: 1,
			Category: indexer.CategoryData, Published: demoEpoch.AddDate(0, 0, -4),
			Uploader: "mirror-admin", Trust: indexer.TrustVerified,
			SourceURL: "https://example.org/demo-mirror/mir-2",
		},
	}
}

// demoMagnet builds a syntactically valid magnet URI carrying dn (display
// name) so any code path that reads it back (engine/fake's nameFor, for
// instance) shows something readable. The infohash segment is a fixed,
// clearly-fake 40-hex-digit value — it is never resolved against a real
// swarm (AGENT.md §6.7 — zero network).
func demoMagnet(name string) string {
	return "magnet:?xt=urn:btih:" + demoInfoHash + "&dn=" + url.QueryEscape(name)
}

// demoInfoHash is a fixed, invented 40-hex-character value shared by every
// demo fixture magnet. It deliberately never corresponds to a real torrent.
const demoInfoHash = "0000000000000000000000000000000000d3d0"
