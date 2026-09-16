package fake

import "github.com/kdta91/tortui/internal/indexer"

// FailingIndexerID is the id of the source NewDemoRegistry registers that
// always fails, so a caller (a test, --demo's status line) can name it
// without hardcoding the string a second time.
const FailingIndexerID = "demo-offline"

// NewDemoRegistry returns an indexer.Registry pre-populated with three
// fixture sources: "demo-archive" and "demo-mirror" — both real,
// succeeding, Search-capable sources whose combined fixtures span every
// indexer.Trust value, a wide CJK title, an emoji title, a huge and a tiny
// size, and a zero-seeder entry (T-056 acceptance) — and "demo-offline",
// which always fails, so a fan-out across the three exercises the "N/M
// sources failed" degrade path (AGENT.md §6.3) without ever touching a
// network.
//
// The Registry it returns is fully functional today — SearchAll against it
// is exercised directly by this package's own tests and by
// internal/app.RunDemo's startup self-check — but nothing in internal/tui
// consumes it yet: the search/results screens that would (T-060, T-061)
// don't exist, and internal/tui may only import indexer *interfaces*, never
// a concrete *indexer.Registry (AGENT.md §4) — no such interface exists
// until those tasks define what a screen actually needs from one. See the
// T-056 tracker notes for the precise hand-off.
func NewDemoRegistry() (*indexer.Registry, error) {
	reg := indexer.NewRegistry(indexer.Config{})

	archive := New("demo-archive", "Demo Archive", indexer.Caps{
		Search: true, Latest: true, Categories: true, ProvidesMagnet: true,
	}, ArchiveResults())

	mirror := New("demo-mirror", "Demo Mirror", indexer.Caps{
		Search: true, Latest: false, ProvidesMagnet: true,
	}, MirrorResults())

	offline := NewFailing(FailingIndexerID, "Demo Offline Mirror", nil)

	for _, ix := range []indexer.Indexer{archive, mirror, offline} {
		if err := reg.Register(ix); err != nil {
			return nil, err
		}
	}

	return reg, nil
}
