package torznab

import (
	"net/url"
	"strings"
	"testing"

	"github.com/kdta91/tortui/internal/indexer"
)

// TestFeedLinksNeverCarryUserinfo is T-9045 criterion 3: a feed item's
// enclosure, <link>, <comments> and permalink guid reach the result with
// their userinfo dropped and the rest of the link kept — the same rule the
// scraper follows (DEC-143) — and a magnet comes back exactly as published.
func TestFeedLinksNeverCarryUserinfo(t *testing.T) {
	t.Parallel()

	src := newSource(t, map[string]reply{
		functionCaps:   fixtureReply(t, "caps-full.xml"),
		functionSearch: fixtureReply(t, "search-userinfo.xml"),
	})

	results, err := mustNew(t, src).Search(testContext(t), indexer.Query{Text: "invented"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	byTitle := make(map[string]indexer.Result, len(results))
	for _, r := range results {
		byTitle[r.Title] = r
	}

	cases := map[string]struct {
		torrent, source, id string
	}{
		"Invented Userinfo Enclosure": {
			torrent: "https://feed.example.org/download/3001.torrent?apikey=redacted-in-fixture",
			source:  "https://feed.example.org/details/3001",
			id:      "3001300130013001300130013001300130013001",
		},
		"Invented Userinfo Link": {
			torrent: "http://feed.example.org:8080/download/3002.torrent",
			source:  "https://feed.example.org/details/3002",
			id:      "https://feed.example.org/details/3002",
		},
	}

	for title, want := range cases {
		t.Run(title, func(t *testing.T) {
			r, ok := byTitle[title]
			if !ok {
				t.Fatalf("no result titled %q in %d results", title, len(results))
			}

			if r.TorrentURL != want.torrent {
				t.Errorf("TorrentURL = %q, want %q", r.TorrentURL, want.torrent)
			}

			if r.SourceURL != want.source {
				t.Errorf("SourceURL = %q, want %q", r.SourceURL, want.source)
			}

			if r.ID != want.id {
				t.Errorf("ID = %q, want %q", r.ID, want.id)
			}

			for field, raw := range map[string]string{"TorrentURL": r.TorrentURL, "SourceURL": r.SourceURL, "ID": r.ID} {
				if strings.Contains(raw, "@") || strings.Contains(raw, "alice") || strings.Contains(raw, "s3cret") {
					t.Errorf("%s = %q carries userinfo", field, raw)
				}
			}
		})
	}

	t.Run("a magnet is untouched", func(t *testing.T) {
		r, ok := byTitle["Invented Userinfo Magnet"]
		if !ok {
			t.Fatalf("no magnet result in %d results", len(results))
		}

		want := "magnet:?xt=urn:btih:3003300330033003300330033003300330033003&tr=" +
			url.QueryEscape("https://alice:s3cret@tracker.example.org/announce")
		if r.Magnet != want {
			t.Errorf("Magnet = %q, want it exactly as published %q", r.Magnet, want)
		}
	})
}

// TestLinkWithoutUserinfo pins the helper every link goes through.
func TestLinkWithoutUserinfo(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"https://feed.example.org/a.torrent":              "https://feed.example.org/a.torrent",
		"  https://feed.example.org/a.torrent  ":          "https://feed.example.org/a.torrent",
		"https://bob:pw@feed.example.org/a.torrent?k=v":   "https://feed.example.org/a.torrent?k=v",
		"https://bob@feed.example.org/a":                  "https://feed.example.org/a",
		"https://@feed.example.org/a":                     "https://feed.example.org/a",
		"http://:pw@feed.example.org:8080/a#frag":         "http://feed.example.org:8080/a#frag",
		"https://bob:pw@feed.example.org/%zz/unparseable": "",
	}

	for in, want := range cases {
		if got := linkWithoutUserinfo(in); got != want {
			t.Errorf("linkWithoutUserinfo(%q) = %q, want %q", in, got, want)
		}
	}
}
