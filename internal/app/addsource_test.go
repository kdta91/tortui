package app

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// twoLinkItem is a one-item Torznab feed whose item carries a download
// enclosure with the api key in its query, plus whatever links extra adds:
// a magneturl attr, or only an infohash attr that Resolve turns into a
// magnet. Invented example.org source, synthetic title.
func twoLinkFeed(extra string) []byte {
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
      <enclosure url="https://feed.example.org/api?t=get&amp;id=9056&amp;apikey=%s" length="1048576" type="application/x-bittorrent"/>
      <torznab:attr name="seeders" value="5"/>
      <torznab:attr name="peers" value="5"/>
      %s
    </item>
  </channel>
</rss>
`, sentinelKey, extra)
}

// TestAddingATwoLinkTorznabResultAddsByMagnet is T-9056 end to end: the
// real Torznab adapter, registry, offline engine (which refuses a source
// with two links) and TUI. A result with a magnet and an api-key-bearing
// enclosure is added by its magnet alone, reaches Downloads, and neither
// the session record nor the engine's resume data holds the enclosure.
func TestAddingATwoLinkTorznabResultAddsByMagnet(t *testing.T) {
	const hash = "9056905690569056905690569056905690569056"

	for name, extra := range map[string]string{
		"magneturl attr": `<torznab:attr name="magneturl" value="magnet:?xt=urn:btih:` + hash + `&amp;dn=Synthetic+Two+Link+Corpus"/>`,
		// No magnet in the feed: Resolve derives one from the infohash.
		"infohash attr": `<torznab:attr name="infohash" value="` + hash + `"/>`,
	} {
		t.Run(name, func(t *testing.T) {
			guardDefaultTransport(t)
			sandbox(t)

			srv := newTorznabServer(t, fixture(t, "caps-minimal.xml"), twoLinkFeed(extra))

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
			tm.Send(tea.KeyMsg{Type: tea.KeyEnter}) // commit the query field
			tm.Send(tea.KeyMsg{Type: tea.KeyEnter}) // submit the search
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

			if !strings.EqualFold(list[0].InfoHash, hash) {
				t.Errorf("engine infohash = %q, want %q", list[0].InfoHash, hash)
			}

			d, err := a.Engine().ResumeData(list[0].ID)
			if err != nil {
				t.Fatalf("ResumeData: %v", err)
			}

			if !strings.Contains(d.Magnet, hash) || d.TorrentURL != "" {
				t.Errorf("engine resume source: Magnet %q, TorrentURL %q; want the magnet only", d.Magnet, d.TorrentURL)
			}

			recs := a.store.ListTorrents()
			if len(recs) != 1 {
				t.Fatalf("store holds %d records, want 1", len(recs))
			}

			if rec := recs[0]; !strings.Contains(rec.Magnet, hash) || rec.TorrentURL != "" {
				t.Errorf("persisted source: Magnet %q, TorrentURL %q; want the magnet only", rec.Magnet, rec.TorrentURL)
			}

			if rec := fmt.Sprintf("%+v %+v", recs[0], d); strings.Contains(rec, sentinelKey) {
				t.Errorf("the api key reached the record or resume data: %s", rec)
			}
		})
	}
}
