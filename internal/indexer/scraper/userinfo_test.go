package scraper

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/kdta91/tortui/internal/indexer"
	"github.com/kdta91/tortui/internal/indexer/httpx"
)

// Userinfo in an address read off a page (T-9041). Go's http client turns
// `https://user:pw@feed.example.org/` into a Basic Authorization header, so a page
// that picks the userinfo picks credentials tortui would send; and a link
// carrying it would land in Result.TorrentURL, and from there in resume
// data. None of the addresses below is ever fetched.

// hasUserinfo reports whether raw parses as an address with userinfo.
func hasUserinfo(t *testing.T, raw string) bool {
	t.Helper()

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}

	return parsed.User != nil
}

// TestADetailsAddressWithUserinfoIsRefusedBeforeAnyRequest is criterion 1:
// a same-host details address with any userinfo, including an empty `@`,
// is refused with ErrDetailsAddressRefused, no request is made, and the
// error does not repeat the address or the credentials in it.
func TestADetailsAddressWithUserinfoIsRefusedBeforeAnyRequest(t *testing.T) {
	cases := map[string]string{
		"user and password": "https://alice:s3cret@details.example.org/item/1",
		"user only":         "https://alice@details.example.org/item/1",
		"empty password":    "https://alice:@details.example.org/item/1",
		"empty userinfo":    "https://@details.example.org/item/1",
		"empty user":        "https://:s3cret@details.example.org/item/1",
	}

	for name, page := range cases {
		t.Run(name, func(t *testing.T) {
			transport := &countingTransport{}

			a, err := New(Options{
				Definition: loadDefinition(t, detailsDefinitionFile),
				Client:     testClient(httpx.Config{Transport: transport}),
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			listed := indexer.Result{Title: "Refused", SourceURL: page}

			got, err := a.Resolve(testContext(t), listed)
			if !errors.Is(err, ErrDetailsAddressRefused) {
				t.Fatalf("Resolve(%q) error = %v, want ErrDetailsAddressRefused", page, err)
			}

			if n := transport.calls.Load(); n != 0 {
				t.Fatalf("Resolve(%q) made %d requests, want none", page, n)
			}

			for _, leak := range []string{"alice", "s3cret", "@", "details.example.org", "/item/1"} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("error %q repeats %q from the refused address", err, leak)
				}
			}

			assertResult(t, got, listed)
		})
	}
}

// TestAResolvedTorrentLinkNeverCarriesUserinfo is criterion 2 on the
// details path: a torrent link on the page that carries userinfo reaches
// the result without it, and a relative link resolved against the page
// carries none.
func TestAResolvedTorrentLinkNeverCarriesUserinfo(t *testing.T) {
	link := func(href string) reply {
		return pageReply(`<html><body><a class="torrent" href="` + href + `">torrent</a></body></html>`)
	}

	src := newSource(t, map[string]reply{
		"/item/1": link("https://alice:s3cret@elsewhere.example/1.torrent"),
		"/item/2": link("https://@elsewhere.example/2.torrent"),
		"/item/3": link("//alice@elsewhere.example/3.torrent"),
		"/item/4": link("download/4.torrent"),
	})
	a := detailsAdapter(t, src, testClient(httpx.Config{}))
	address := src.server.URL

	cases := map[string]string{
		"/item/1": "https://elsewhere.example/1.torrent",
		"/item/2": "https://elsewhere.example/2.torrent",
		"/item/3": "http://elsewhere.example/3.torrent",
		"/item/4": address + "/item/download/4.torrent",
	}

	for path, want := range cases {
		t.Run(path, func(t *testing.T) {
			page := address + path

			got, err := a.Resolve(testContext(t), indexer.Result{Title: "Invented " + path, SourceURL: page})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}

			if got.TorrentURL != want {
				t.Errorf("TorrentURL = %q, want %q", got.TorrentURL, want)
			}

			if hasUserinfo(t, got.TorrentURL) {
				t.Errorf("TorrentURL %q carries userinfo", got.TorrentURL)
			}
		})
	}
}

// TestWebAddressDropsUserinfo pins the one place both paths resolve a
// link: whatever the link or its base carried, the address it returns has
// no userinfo — including a relative link resolved against a base that
// has some, which inherits it without this.
func TestWebAddressDropsUserinfo(t *testing.T) {
	t.Parallel()

	base, err := url.Parse("https://alice:s3cret@feed.example.org/browse/")
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}

	cases := map[string]string{
		"item/1":                                 "https://feed.example.org/browse/item/1",
		"/item/1":                                "https://feed.example.org/item/1",
		"?page=2":                                "https://feed.example.org/browse/?page=2",
		"https://bob:pw@other.example.org/x":     "https://other.example.org/x",
		"https://@other.example.org/x":           "https://other.example.org/x",
		"//carol@other.example.org/x":            "https://other.example.org/x",
		"http://bob:@other.example.org:8080/x?y": "http://other.example.org:8080/x?y",
		"javascript:alert(1)":                    "",
	}

	for in, want := range cases {
		got := webAddress(base, in)
		if got != want {
			t.Errorf("webAddress(%q) = %q, want %q", in, got, want)
		}

		if got != "" && hasUserinfo(t, got) {
			t.Errorf("webAddress(%q) = %q carries userinfo", in, got)
		}
	}
}

// TestListedLinksNeverCarryUserinfo is criterion 2 on the listing path:
// torrent_url and source_url read off a listing reach the result without
// userinfo, whether the page wrote it into the link or a relative link
// would have inherited it from a base_url that carries some.
func TestListedLinksNeverCarryUserinfo(t *testing.T) {
	const listing = `
id: fixture-userinfo
base_url: https://feed.example.org
rows: li
fields:
  title:
    selector: a.name
  source_url:
    selector: a.name
    attr: href
  torrent_url:
    selector: a.torrent
    attr: href
search:
  path: /s
`

	page := `<html><body><ul>
<li><a class="name" href="https://alice:s3cret@elsewhere.example/item/1">Invented One</a>
    <a class="torrent" href="https://@elsewhere.example/1.torrent">t</a></li>
<li><a class="name" href="/item/2">Invented Two</a>
    <a class="torrent" href="download/2.torrent">t</a></li>
</ul></body></html>`

	src := newSource(t, map[string]reply{"/s": pageReply(page)})

	def, err := Parse([]byte(listing))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	address := strings.Replace(src.server.URL, "://", "://alice:s3cret@", 1)
	def.BaseURL = address

	a, err := New(Options{Definition: def, Client: testClient(httpx.Config{})})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	results, err := a.Search(testContext(t), keywordQuery())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("Search returned %d results, want 2", len(results))
	}

	for _, r := range results {
		for field, raw := range map[string]string{"source_url": r.SourceURL, "torrent_url": r.TorrentURL} {
			if raw == "" {
				t.Errorf("%q: %s dropped entirely, want it kept without userinfo", r.Title, field)

				continue
			}

			if hasUserinfo(t, raw) || strings.Contains(raw, "s3cret") || strings.Contains(raw, "@") {
				t.Errorf("%q: %s = %q carries userinfo", r.Title, field, raw)
			}
		}

		if strings.Contains(r.ID, "s3cret") || strings.Contains(r.ID, "@") {
			t.Errorf("%q: ID = %q carries userinfo", r.Title, r.ID)
		}
	}
}
