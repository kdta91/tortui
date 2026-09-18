package anacrolix

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/engine"
)

// testPieceLength is the piece size every fixture torrent in this package
// declares. It is small so a fixture stays a few hundred bytes.
const testPieceLength = 32 << 10

// buildInfo assembles a metainfo.Info for a multi-file torrent from the
// per-file path components given, with a piece table sized to match so the
// library accepts it.
//
// Fixtures are built here rather than committed as binary blobs so a hostile
// path can be expressed as data in the test that needs it, and so no fixture
// in this repository ever has to name a real source (AGENT.md §2).
func buildInfo(name string, paths [][]string) metainfo.Info {
	files := make([]metainfo.FileInfo, 0, len(paths))
	var total int64

	for i, p := range paths {
		length := int64(1024 * (i + 1))
		total += length
		files = append(files, metainfo.FileInfo{Length: length, Path: p})
	}

	pieces := int((total + testPieceLength - 1) / testPieceLength)
	if pieces == 0 {
		pieces = 1
	}

	return metainfo.Info{
		Name:        name,
		PieceLength: testPieceLength,
		Pieces:      make([]byte, sha1.Size*pieces),
		Files:       files,
	}
}

// encodeTorrent bencodes an info dictionary into a complete .torrent file.
func encodeTorrent(t *testing.T, info metainfo.Info) []byte {
	t.Helper()

	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("bencode info: %v", err)
	}

	mi := metainfo.MetaInfo{InfoBytes: infoBytes}

	var buf bytes.Buffer
	if err := mi.Write(&buf); err != nil {
		t.Fatalf("write metainfo: %v", err)
	}

	return buf.Bytes()
}

// writeTorrentFile writes a .torrent fixture into the test's temp directory
// and returns its path.
func writeTorrentFile(t *testing.T, info metainfo.Info) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "fixture.torrent")
	if err := os.WriteFile(path, encodeTorrent(t, info), 0o600); err != nil {
		t.Fatalf("write torrent fixture: %v", err)
	}

	return path
}

// serveTorrent starts an httptest.Server that returns the encoded torrent.
// AGENT.md §6.7 forbids network calls from unit tests; a loopback
// httptest.Server is the sanctioned way to exercise an HTTP path without one,
// the same pattern the indexer adapters' tests use.
func serveTorrent(t *testing.T, body []byte) string {
	t.Helper()

	url, stop := serveTorrentUntil(t, body)
	t.Cleanup(stop)

	return url
}

// serveTorrentUntil is serveTorrent with an explicit stop, for the tests that
// have to shut the server down before taking a goroutine snapshot.
func serveTorrentUntil(t *testing.T, body []byte) (url string, stop func()) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-bittorrent")
		_, _ = w.Write(body)
	}))

	return srv.URL + "/fixture.torrent", srv.Close
}

// magnetURI builds a syntactically valid magnet URI for an invented torrent.
// It names no real source: the infohash is derived from seed and it carries no
// tracker (AGENT.md §2).
func magnetURI(seed string) string {
	sum := sha1.Sum([]byte(seed))
	return fmt.Sprintf("magnet:?xt=urn:btih:%x&dn=%s", sum, "example-fixture")
}

// newTestEngine builds an offline Engine writing under a fresh temp directory,
// with the metadata timeout and rate sample interval wound right down so no
// test ever waits out a real 60 seconds.
func newTestEngine(t *testing.T, mutate func(*Options)) *Engine {
	t.Helper()

	dir := t.TempDir()
	opts := Options{
		Config: config.Config{
			DownloadDir: dir,
			MaxPeers:    7,
		},
		Logger:             discardLogger(),
		MetadataTimeout:    150 * time.Millisecond,
		RateSampleInterval: 10 * time.Millisecond,
		Offline:            true,
	}

	if mutate != nil {
		mutate(&opts)
	}

	e, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() {
		if err := e.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	return e
}

// statusOf returns the tracked status for id, failing the test if it is gone.
func statusOf(t *testing.T, e *Engine, id string) engine.TorrentStatus {
	t.Helper()

	for _, st := range e.List() {
		if st.ID == id {
			return st
		}
	}

	t.Fatalf("torrent %s is not in List()", id)

	return engine.TorrentStatus{}
}

// waitForState polls List until the torrent reaches want, or the deadline
// passes. Polling is acceptable here because Updates() does not emit yet
// (T-033 owns that); the interval is short and the budget is small.
func waitForState(t *testing.T, e *Engine, id string, want engine.State) engine.TorrentStatus {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	var last engine.TorrentStatus

	for time.Now().Before(deadline) {
		last = statusOf(t, e, id)
		if last.State == want {
			return last
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("torrent %s state = %s, want %s (err %v)", id, last.State, want, last.Err)

	return last
}

// discardLogger returns a slog.Logger that records nothing, so a test's
// expectations are about behaviour rather than about log output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// waitForRateSample reports whether the background sampler took at least one
// sample for id within a short budget.
func waitForRateSample(t *testing.T, e *Engine, id string) bool {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		tr, ok := e.torrents[id]
		sampled := ok && !tr.down.last.IsZero()
		e.mu.Unlock()

		if sampled {
			return true
		}

		time.Sleep(5 * time.Millisecond)
	}

	return false
}
