package engine

import (
	"strings"
	"testing"
)

// sentinel stands in for an indexer api key in a .torrent address.
const sentinel = "SENTINEL-9057"

func TestURLSourceNameKeepsOnlyTheHost(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"query key":         "https://feed.example.org/api?t=get&id=9057&apikey=" + sentinel,
		"path key":          "https://feed.example.org/dl/" + sentinel + "/file.torrent",
		"userinfo":          "https://user:" + sentinel + "@feed.example.org/file.torrent",
		"port and fragment": "http://FEED.example.org:9696/file.torrent#" + sentinel,
	}

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := URLSourceName(raw)
			if got != "torrent file from feed.example.org" {
				t.Errorf("URLSourceName = %q, want %q", got, "torrent file from feed.example.org")
			}

			if strings.Contains(got, sentinel) {
				t.Errorf("URLSourceName kept the key: %q", got)
			}
		})
	}

	for _, raw := range []string{"", "   ", "not a url " + sentinel, "/relative/" + sentinel, "%zz" + sentinel} {
		if got := URLSourceName(raw); got != "torrent file" {
			t.Errorf("URLSourceName(%q) = %q, want %q", raw, got, "torrent file")
		}
	}
}

func TestSafeNameReducesOnlyWebAddresses(t *testing.T) {
	t.Parallel()

	reduced := map[string]string{
		"https://feed.example.org/api?apikey=" + sentinel:  "torrent file from feed.example.org",
		" http://feed.example.org/x.torrent?k=" + sentinel: "torrent file from feed.example.org",
		"ftp://mirror.example.org/" + sentinel:             "torrent file from mirror.example.org",
	}

	for in, want := range reduced {
		if got := SafeName(in); got != want {
			t.Errorf("SafeName(%q) = %q, want %q", in, got, want)
		}
	}

	kept := []string{
		"",
		"Synthetic Corpus 9057",
		"synthetic-corpus.iso",
		"magnet:?xt=urn:btih:9057905790579057905790579057905790579057",
		"Title: with a colon",
		"http://example.org with spaces is no address",
		`C:\data\corpus.iso`,
	}

	for _, in := range kept {
		if got := SafeName(in); got != in {
			t.Errorf("SafeName(%q) = %q, want it unchanged", in, got)
		}
	}
}
