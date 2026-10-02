package app

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// twoLinkFeed is a one-item Torznab feed whose item carries a download
// enclosure at enclosure, plus whatever links extra adds: a magneturl attr,
// or only an infohash attr. Invented source, synthetic title.
func twoLinkFeed(enclosure, extra string) []byte {
	return fmt.Appendf(nil, `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Lab Feed</title>
    <item>
      <title>Synthetic Two Link Corpus</title>
      <guid isPermaLink="true">https://feed.example.org/details/9056</guid>
      <comments>https://feed.example.org/details/9056</comments>
      <pubDate>Thu, 01 Oct 2026 12:00:00 +0000</pubDate>
      <size>1048576</size>
      <enclosure url="%s" length="1048576" type="application/x-bittorrent"/>
      <torznab:attr name="seeders" value="5"/>
      <torznab:attr name="peers" value="5"/>
      %s
    </item>
  </channel>
</rss>
`, html.EscapeString(enclosure), extra)
}

// syntheticTorrent bencodes a one-file .torrent and returns it with its
// infohash.
func syntheticTorrent(t *testing.T) ([]byte, string) {
	t.Helper()

	info := metainfo.Info{Name: "synthetic-two-link-corpus.bin", PieceLength: 32 << 10, Length: 1024, Pieces: make([]byte, sha1.Size)}

	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("bencode info: %v", err)
	}

	mi := metainfo.MetaInfo{InfoBytes: infoBytes}

	var buf bytes.Buffer
	if err := mi.Write(&buf); err != nil {
		t.Fatalf("write metainfo: %v", err)
	}

	return buf.Bytes(), mi.HashInfoBytes().HexString()
}

// twoLinkServer is a loopback Torznab source: t=caps answers caps, t=get
// answers the .torrent and is counted, anything else answers the feed,
// which links its enclosure back to t=get with the api key in the query.
type twoLinkServer struct {
	*httptest.Server

	mu   sync.Mutex
	gets int
}

func newTwoLinkServer(t *testing.T, torrent []byte, extra string) (*twoLinkServer, string) {
	t.Helper()

	caps := fixture(t, "caps-minimal.xml")
	ts := &twoLinkServer{}

	var feed []byte

	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := feed

		switch r.URL.Query().Get("t") {
		case "caps":
			body = caps
		case "get":
			ts.mu.Lock()
			ts.gets++
			ts.mu.Unlock()

			body = torrent
		}

		if _, err := w.Write(body); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(ts.Close)

	enclosure := ts.URL + "/api?t=get&id=9056&apikey=" + sentinelKey
	feed = twoLinkFeed(enclosure, extra)

	return ts, enclosure
}

func (ts *twoLinkServer) torrentFetches() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	return ts.gets
}

// TestAddingATwoLinkTorznabResult is T-9056 end to end: the real Torznab
// adapter, registry, offline engine (which refuses a source with two links)
// and TUI. A result whose feed published a magnet next to an
// api-key-bearing enclosure is added by the magnet alone, and neither the
// session record nor the engine's resume data holds the enclosure. A
// result with only an infohash next to the enclosure is added by the
// enclosure: a magnet built from a bare hash names no tracker (DEC-147).
func TestAddingATwoLinkTorznabResult(t *testing.T) {
	torrent, hash := syntheticTorrent(t)

	for name, tc := range map[string]struct {
		extra   string
		wantURL bool
	}{
		"magneturl attr": {extra: `<torznab:attr name="magneturl" value="magnet:?xt=urn:btih:` + hash + `&amp;dn=Synthetic+Two+Link+Corpus"/>`},
		"infohash attr":  {extra: `<torznab:attr name="infohash" value="` + hash + `"/>`, wantURL: true},
	} {
		t.Run(name, func(t *testing.T) {
			guardDefaultTransport(t)
			sandbox(t)

			srv, enclosure := newTwoLinkServer(t, torrent, tc.extra)

			a, err := New(Options{
				Capability: theme.Capability{Unicode: true},
				transport:  labTransport{host: srv.Listener.Addr().String(), lab: srv.Client().Transport},
				offline:    true,
				configure: func(c *config.Config) {
					c.Indexers = append(c.Indexers, labSource(srv.Server))
				},
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			t.Cleanup(func() {
				if err := a.Close(); err != nil {
					t.Errorf("Close: %v", err)
				}
			})

			tm := teatest.NewTestModel(t, a.Model(), teatest.WithInitialTermSize(240, 40))

			waitForAny(t, tm, "Welcome to tortui")
			tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
			tm.Type("/corpus")
			tm.Send(tea.KeyMsg{Type: tea.KeyEnter}) // one enter submits the search
			waitForAny(t, tm, "Synthetic Two Link Corpus")

			tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
			waitForAny(t, tm, "writable")
			tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

			// Downloads shows once the add's result is handled, and so
			// after its record was written.
			waitForAny(t, tm, "Active (1)")

			if err := tm.Quit(); err != nil {
				t.Fatalf("Quit: %v", err)
			}

			list := a.Engine().List()
			if len(list) != 1 {
				t.Fatalf("engine tracks %d torrents, want 1", len(list))
			}

			d, err := a.Engine().ResumeData(list[0].ID)
			if err != nil {
				t.Fatalf("ResumeData: %v", err)
			}

			recs := a.store.ListTorrents()
			if len(recs) != 1 {
				t.Fatalf("store holds %d records, want 1", len(recs))
			}

			rec := recs[0]

			if tc.wantURL {
				if d.Magnet != "" || d.TorrentURL != enclosure {
					t.Errorf("engine resume source: Magnet %q, TorrentURL %q; want the enclosure only", d.Magnet, d.TorrentURL)
				}

				if rec.Magnet != "" || rec.TorrentURL != enclosure {
					t.Errorf("persisted source: Magnet %q, TorrentURL %q; want the enclosure only", rec.Magnet, rec.TorrentURL)
				}

				return
			}

			if !strings.EqualFold(list[0].InfoHash, hash) {
				t.Errorf("engine infohash = %q, want %q", list[0].InfoHash, hash)
			}

			if !strings.Contains(d.Magnet, hash) || d.TorrentURL != "" {
				t.Errorf("engine resume source: Magnet %q, TorrentURL %q; want the magnet only", d.Magnet, d.TorrentURL)
			}

			if !strings.Contains(rec.Magnet, hash) || rec.TorrentURL != "" {
				t.Errorf("persisted source: Magnet %q, TorrentURL %q; want the magnet only", rec.Magnet, rec.TorrentURL)
			}

			if both := fmt.Sprintf("%+v %+v", rec, d); strings.Contains(both, sentinelKey) {
				t.Errorf("the api key reached the record or resume data: %s", both)
			}

			if n := srv.torrentFetches(); n != 0 {
				t.Errorf("the enclosure was fetched %d time(s) for a magnet add, want 0", n)
			}
		})
	}
}
