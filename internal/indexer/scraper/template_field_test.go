package scraper

import (
	"errors"
	"strings"
	"testing"

	"github.com/kdta91/tortui/internal/indexer"
)

// templatedDefinition is a json definition whose only link is a
// torrent_url built by a field template out of each row's bare id — the
// shape of a source that returns an item id and documents where that
// item's .torrent lives, and nothing more.
const templatedDefinition = `
id: fixture-templated
base_url: https://files.example.org
mode: json
rows: items
fields:
  id:
    selector: id
  title:
    selector: name
  torrent_url:
    selector: id
    template: /download/{{value}}/{{ value }}_generated.torrent
search:
  path: /search
  params:
    q: "{{query}}"
`

// templatedRows are the rows the fixture source answers with: an ordinary
// id, one that tries to add path segments and a query of its own, and one
// with no id at all.
const templatedRows = `{"items": [
	{"id": "item-0001", "name": "Invented Public Domain Film"},
	{"id": "a b/../../elsewhere?x=1#f", "name": "Invented Hostile Id"},
	{"id": "", "name": "Invented Row Without Id"}
]}`

// TestFieldTemplateBuildsALinkFromAReadValue is the T-9010 capability: a
// field template turns a bare id into an absolute .torrent address under
// base_url, the id is path-escaped so it stays one segment, and a row with
// no id gets no link rather than the template's literal text.
func TestFieldTemplateBuildsALinkFromAReadValue(t *testing.T) {
	t.Parallel()

	def, err := Parse([]byte(templatedDefinition))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	src := newSource(t, map[string]reply{"/search": jsonReply(templatedRows)})
	a := mustAdapter(t, src, def)

	results, err := a.Search(testContext(t), indexer.Query{Mode: indexer.ModeSearch, Text: "film"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}

	base := src.server.URL

	if want := base + "/download/item-0001/item-0001_generated.torrent"; results[0].TorrentURL != want {
		t.Errorf("TorrentURL = %q, want %q", results[0].TorrentURL, want)
	}

	if err := results[0].Validate(); err != nil {
		t.Errorf("a result whose only link is a templated torrent_url is not usable: %v", err)
	}

	hostile := results[1].TorrentURL
	if want := base + "/download/a%20b%2F..%2F..%2Felsewhere%3Fx=1%23f/a%20b%2F..%2F..%2Felsewhere%3Fx=1%23f_generated.torrent"; hostile != want {
		t.Errorf("hostile TorrentURL = %q, want %q (the value path-escaped into one segment)", hostile, want)
	}

	if strings.Contains(hostile, "?") || strings.Contains(hostile, "#") || strings.Contains(hostile, "/elsewhere") {
		t.Errorf("a read value reshaped the address the template builds: %q", hostile)
	}

	if results[2].TorrentURL != "" {
		t.Errorf("TorrentURL = %q for a row with no id, want empty", results[2].TorrentURL)
	}
}

// TestFieldTemplateIsValidated covers the three ways a field template is
// refused, each naming the key and never the template's own text.
func TestFieldTemplateIsValidated(t *testing.T) {
	t.Parallel()

	const good = "template: /download/{{value}}/{{ value }}_generated.torrent"

	cases := map[string]struct {
		template string
		want     error
	}{
		"a request placeholder": {template: "template: /download/{{query}}", want: ErrPlaceholderUnknown},
		"no value placeholder":  {template: "template: /download/fixed.torrent", want: ErrTemplateValueMissing},
		"an unclosed value":     {template: "template: /download/{{value", want: ErrPlaceholderUnterminated},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source := strings.Replace(templatedDefinition, good, tc.template, 1)
			if source == templatedDefinition {
				t.Fatal("the case did not change the definition")
			}

			_, err := Parse([]byte(source))
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want it to wrap %v", err, tc.want)
			}

			var verr *ValidationError
			if !errors.As(err, &verr) || verr.Location != "search.fields.torrent_url.template" {
				t.Fatalf("error = %v, want a *ValidationError at search.fields.torrent_url.template", err)
			}

			if strings.Contains(err.Error(), "/download/") {
				t.Errorf("the message repeats the template's text: %q", err.Error())
			}
		})
	}
}

// TestResolvePrefersATorrentURLOverAnInfohash pins the Resolve order T-9010
// depends on: a result carrying both an infohash and a torrent URL is
// returned unchanged, so it reaches the engine by its .torrent (with that
// file's trackers and web seeds) and with exactly one link, instead of
// gaining a magnet built from the bare hash next to the URL.
func TestResolvePrefersATorrentURLOverAnInfohash(t *testing.T) {
	t.Parallel()

	src, a := archive(t)

	before := indexer.Result{
		IndexerID:  htmlID,
		ID:         "1005",
		Title:      "Invented Archive Item",
		InfoHash:   "0123456789abcdef0123456789abcdef01234567",
		TorrentURL: "https://files.example.org/download/1005/1005_generated.torrent",
	}

	after, err := a.Resolve(testContext(t), before)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	assertResult(t, after, before)

	if after.Magnet != "" {
		t.Errorf("Magnet = %q, want none: the torrent URL already makes the result usable", after.Magnet)
	}

	if len(src.requests()) != 0 {
		t.Errorf("Resolve made %d requests, want 0", len(src.requests()))
	}
}
