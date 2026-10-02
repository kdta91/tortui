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

	// An address url.Parse refuses is still an address when it opens with
	// a scheme and "://" (T-9095): returned unchanged, it would show its
	// api key. Its host cannot be read, so it is named "torrent file".
	const key = "K"

	unparseable := map[string]string{
		"bad path escape":           "https://feed.example.org/dl/%zz?apikey=" + key,
		"bad port":                  "https://feed.example.org:port/dl?apikey=" + key,
		"unclosed IPv6 bracket":     "http://[::1/dl?apikey=" + key,
		"unclosed bracket, no host": "http://[abc/dl?apikey=" + key,
		"bad fragment escape":       "https://feed.example.org/dl?apikey=" + key + "#%zz",
		"trailing control byte":     "https://feed.example.org/dl?apikey=" + key + "\x7f",
		"trailing C0 byte":          "https://feed.example.org/dl?apikey=" + key + "\x01",
		"space in the host":         "http://feed.example.org with spaces?apikey=" + key,
		"leading space":             "  https://feed.example.org/dl/%zz?apikey=" + key,
	}

	for name, in := range unparseable {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := SafeName(in)
			if got != "torrent file" {
				t.Errorf("SafeName(%q) = %q, want %q", in, got, "torrent file")
			}

			if strings.Contains(got, key) {
				t.Errorf("SafeName(%q) kept the key: %q", in, got)
			}

			if direct := URLSourceName(in); strings.Contains(direct, key) {
				t.Errorf("URLSourceName(%q) kept the key: %q", in, direct)
			}
		})
	}

	// An invisible rune — a C0 control byte, or a Unicode format character
	// such as U+200B or U+FEFF — before or inside the scheme, or in the
	// separator after it, does not stop the name being an address (T-9099,
	// DEC-155). Neither does an http(s) scheme with no host: url.Parse
	// accepts these, and the key is still in the query.
	hidden := map[string]string{
		"leading C0 byte":         "\x01https://host.example/x?apikey=" + key,
		"leading NUL byte":        "\x00https://host.example/x?apikey=" + key,
		"leading U+200B":          "\u200bhttps://host.example/x?apikey=" + key,
		"leading U+FEFF":          "\ufeffhttps://host.example/x?apikey=" + key,
		"space then U+200B":       " \u200b https://host.example/x?apikey=" + key,
		"C0 byte in the scheme":   "https\x01://host.example/x?apikey=" + key,
		"U+200B in the scheme":    "ht\u200btps://host.example/x?apikey=" + key,
		"C0 byte after the colon": "https:\x01//host.example/x?apikey=" + key,
		"U+FEFF after the colon":  "https:\ufeff//host.example/x?apikey=" + key,
		"opaque, no host":         "https:host?apikey=" + key,
		"one slash, no host":      "https:/host/x?apikey=" + key,
		"three slashes, no host":  "https:///x?apikey=" + key,
		"upper-case, no host":     "HTTP:host?apikey=" + key,
	}

	for name, in := range hidden {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := SafeName(in)
			if !strings.HasPrefix(got, "torrent file") {
				t.Errorf("SafeName(%q) = %q, want it named as a torrent file", in, got)
			}

			if strings.Contains(got, key) {
				t.Errorf("SafeName(%q) kept the key: %q", in, got)
			}
		})
	}

	kept := []string{
		"",
		"Synthetic Corpus 9057",
		"synthetic-corpus.iso",
		"magnet:?xt=urn:btih:9057905790579057905790579057905790579057",
		"Title: with a colon",
		"Title: with a colon and %zz",
		"1http://not a scheme",
		"://no scheme",
		`C:\data\corpus.iso`,
		"Family\u200dphotos: summer",
		"\u200bSynthetic Corpus 9057",
		"HTTP Field Notes 9057",
		"https-corpus 9057",
	}

	for _, in := range kept {
		if got := SafeName(in); got != in {
			t.Errorf("SafeName(%q) = %q, want it unchanged", in, got)
		}
	}
}
