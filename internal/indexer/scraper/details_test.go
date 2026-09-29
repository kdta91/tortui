package scraper

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/indexer"
	"github.com/kdta91/tortui/internal/indexer/httpx"
)

// detailsDefinitionFile is the checked-in definition whose listing carries
// only a link to each item's own page (T-9037).
const detailsDefinitionFile = "fixture-details.yml"

// The infohashes the checked-in details pages carry.
const (
	magnetPageHash   = "2001200120012001200120012001200120012001"
	infohashPageHash = "2003200320032003200320032003200320032003"
)

// detailsSource serves the listing and every item page of the invented
// details-only site.
func detailsSource(t *testing.T) *fakeSource {
	t.Helper()

	return newSource(t, map[string]reply{
		"/search":    fixturePage(t, "details-search.html"),
		"/item/2001": fixturePage(t, "details-magnet.html"),
		"/item/2002": fixturePage(t, "details-torrent.html"),
		"/item/2003": fixturePage(t, "details-infohash.html"),
		"/item/2004": fixturePage(t, "details-empty.html"),
		"/item/2005": fixturePage(t, "details-bogus.html"),
	})
}

// detailsAdapter is an adapter for the checked-in details definition
// pointed at src, over client.
func detailsAdapter(t *testing.T, src *fakeSource, client *httpx.Client) *Adapter {
	t.Helper()

	a, err := New(Options{Definition: src.pointAt(loadDefinition(t, detailsDefinitionFile)), Client: client})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return a
}

// searchDetails runs one search against the details site and returns the
// results keyed by the item path each links to.
func searchDetails(t *testing.T, src *fakeSource, a *Adapter) map[string]indexer.Result {
	t.Helper()

	results, err := a.Search(testContext(t), indexer.Query{Mode: indexer.ModeSearch, Text: "invented"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	out := make(map[string]indexer.Result, len(results))

	for _, r := range results {
		out[strings.TrimPrefix(r.SourceURL, src.server.URL)] = r
	}

	return out
}

// requestsTo counts the requests src received for one path.
func requestsTo(src *fakeSource, path string) int {
	n := 0

	for _, r := range src.requests() {
		if r.path == path {
			n++
		}
	}

	return n
}

// TestASearchWithManyResultsMakesExactlyOneRequest is criterion 3: Search
// never fetches a details page, however many rows need one. Every row still
// comes back — with no link yet — so the table can list it.
func TestASearchWithManyResultsMakesExactlyOneRequest(t *testing.T) {
	src := detailsSource(t)
	a := detailsAdapter(t, src, testClient(httpx.Config{}))

	results, err := a.Search(testContext(t), indexer.Query{Mode: indexer.ModeSearch, Text: "invented"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != 5 {
		t.Fatalf("Search returned %d results, want all 5 rows listed", len(results))
	}

	for _, r := range results {
		if r.Magnet != "" || r.TorrentURL != "" || r.InfoHash != "" {
			t.Errorf("%q came back resolved (%+v); the listing carries no link", r.Title, r)
		}

		if !strings.HasPrefix(r.SourceURL, src.server.URL+"/item/") {
			t.Errorf("%q has source_url %q, want its details page", r.Title, r.SourceURL)
		}
	}

	if got := src.requests(); len(got) != 1 || got[0].path != "/search" {
		t.Fatalf("a search with %d results made requests %v, want exactly one, to /search", len(results), got)
	}

	if a.Caps().ProvidesMagnet {
		t.Error("Caps.ProvidesMagnet = true for a listing that carries no magnet")
	}
}

// TestResolveReadsTheMagnetOffTheDetailsPage is the magnet happy path: one
// request, to that item's page, and the page's own magnet — with its
// infohash — on the result. Nothing else about the result changes.
func TestResolveReadsTheMagnetOffTheDetailsPage(t *testing.T) {
	src := detailsSource(t)
	a := detailsAdapter(t, src, testClient(httpx.Config{}))
	listed := searchDetails(t, src, a)["/item/2001"]

	got, err := a.Resolve(testContext(t), listed)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	want := listed
	want.Magnet = "magnet:?xt=urn:btih:" + magnetPageHash + "&dn=Invented+Survey+Data+2001"
	want.InfoHash = magnetPageHash
	assertResult(t, got, want)

	if n := requestsTo(src, "/item/2001"); n != 1 {
		t.Errorf("the details page was fetched %d times, want once", n)
	}

	if n := len(src.requests()); n != 2 {
		t.Errorf("search plus one resolve made %d requests, want 2", n)
	}
}

// TestResolveReadsARelativeTorrentLinkAgainstTheDetailsPage is the
// torrent_url happy path, and pins that a relative link resolves against
// the details page's own address rather than against base_url: the page is
// /item/2002 and its link is "download/2002.torrent".
func TestResolveReadsARelativeTorrentLinkAgainstTheDetailsPage(t *testing.T) {
	src := detailsSource(t)
	a := detailsAdapter(t, src, testClient(httpx.Config{}))
	listed := searchDetails(t, src, a)["/item/2002"]

	got, err := a.Resolve(testContext(t), listed)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	link := src.server.URL + "/item/download/2002.torrent"
	want := listed
	want.TorrentURL = link
	assertResult(t, got, want)

	if got.Magnet != "" {
		t.Errorf("Magnet = %q, want empty: a result reaches the engine with exactly one link", got.Magnet)
	}
}

// TestResolveBuildsAMagnetFromAnInfohashOnTheDetailsPage is the infohash
// happy path.
func TestResolveBuildsAMagnetFromAnInfohashOnTheDetailsPage(t *testing.T) {
	src := detailsSource(t)
	a := detailsAdapter(t, src, testClient(httpx.Config{}))
	listed := searchDetails(t, src, a)["/item/2003"]

	got, err := a.Resolve(testContext(t), listed)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	want := listed
	want.InfoHash = infohashPageHash
	want.Magnet = magnetFor(infohashPageHash, listed.Title)
	assertResult(t, got, want)
}

// TestADetailsPageWithNoLinkIsAReadableError is criterion 5's last clause:
// a page that matched nothing is an error saying so, never a silently
// empty add, and the result comes back unchanged.
func TestADetailsPageWithNoLinkIsAReadableError(t *testing.T) {
	src := detailsSource(t)
	a := detailsAdapter(t, src, testClient(httpx.Config{}))
	listed := searchDetails(t, src, a)["/item/2004"]

	got, err := a.Resolve(testContext(t), listed)
	if !errors.Is(err, ErrDetailsNoLink) {
		t.Fatalf("Resolve error = %v, want ErrDetailsNoLink", err)
	}

	if !strings.Contains(err.Error(), "details page had no magnet, torrent link or infohash") {
		t.Errorf("error %q does not say what the page lacked", err)
	}

	assertResult(t, got, listed)
}

// TestANonMagnetMagnetAndANonWebTorrentLinkAreDropped is criterion 5's
// hostile-input half: a "magnet" that is a javascript: link and a torrent
// link to a local file are both dropped, so the page has nothing usable.
func TestANonMagnetMagnetAndANonWebTorrentLinkAreDropped(t *testing.T) {
	src := detailsSource(t)
	a := detailsAdapter(t, src, testClient(httpx.Config{}))
	listed := searchDetails(t, src, a)["/item/2005"]

	got, err := a.Resolve(testContext(t), listed)
	if !errors.Is(err, ErrDetailsNoLink) {
		t.Fatalf("Resolve error = %v, want ErrDetailsNoLink", err)
	}

	if got.Magnet != "" || got.TorrentURL != "" {
		t.Errorf("a hostile link survived: magnet %q, torrent %q", got.Magnet, got.TorrentURL)
	}
}

// TestANonMagnetMagnetDoesNotShadowAnInfohash pins that a dropped magnet
// value is dropped, not used: the page's valid infohash builds the magnet.
func TestANonMagnetMagnetDoesNotShadowAnInfohash(t *testing.T) {
	src := newSource(t, map[string]reply{
		"/item/1": pageReply(`<html><body>
			<a class="magnet" href="https://details.example.org/not-a-magnet">magnet</a>
			<span class="hash">` + infohashPageHash + `</span>
			<span class="hash">not-a-hash</span>
		</body></html>`),
	})
	a := detailsAdapter(t, src, testClient(httpx.Config{}))

	page := src.server.URL + "/item/1"

	got, err := a.Resolve(testContext(t), indexer.Result{Title: "One", SourceURL: page})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if want := magnetFor(infohashPageHash, "One"); got.Magnet != want {
		t.Errorf("Magnet = %q, want %q built from the page's infohash", got.Magnet, want)
	}
}

// TestAnInvalidInfohashOnTheDetailsPageIsNotUsed pins the 40-hex/32-base32
// check on a details page's infohash.
func TestAnInvalidInfohashOnTheDetailsPageIsNotUsed(t *testing.T) {
	src := newSource(t, map[string]reply{
		"/item/1": pageReply(`<html><body><span class="hash">2003-not-forty-hex</span></body></html>`),
	})
	a := detailsAdapter(t, src, testClient(httpx.Config{}))

	page := src.server.URL + "/item/1"

	_, err := a.Resolve(testContext(t), indexer.Result{Title: "One", SourceURL: page})
	if !errors.Is(err, ErrDetailsNoLink) {
		t.Fatalf("Resolve error = %v, want ErrDetailsNoLink", err)
	}
}

// countingTransport fails every request it is handed and counts them, so a
// test can prove a refusal happened before any request — without a server
// and without any chance of a real network call.
type countingTransport struct {
	calls atomic.Int32
}

func (c *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.calls.Add(1)

	return nil, errors.New("countingTransport: no request should have been made")
}

// TestADetailsAddressOffTheSourceIsRefusedBeforeAnyRequest is criterion 4:
// a source_url that is not http(s) on the base_url's own host is refused
// with ErrDetailsAddressRefused and no request is made. The error never
// repeats the address.
func TestADetailsAddressOffTheSourceIsRefusedBeforeAnyRequest(t *testing.T) {
	cases := map[string]string{
		"a different host":       "https://elsewhere.example/item/1",
		"a subdomain":            "https://cdn.details.example.org/item/1",
		"a different port":       "https://details.example.org:8443/item/1",
		"userinfo naming a host": "https://details.example.org@elsewhere.example/item/1",
		"https dropped for http": "http://details.example.org/item/1",
		"a javascript link":      "javascript:alert(1)",
		"a local file":           "file:///etc/passwd",
		"a relative path":        "/item/1",
		"no scheme":              "details.example.org/item/1",
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

			if strings.Contains(err.Error(), "elsewhere") || strings.Contains(err.Error(), "/item/1") {
				t.Errorf("error %q repeats the refused address", err)
			}

			assertResult(t, got, listed)
		})
	}
}

// TestTheDetailsAddressHostMatchIgnoresCase pins that containment compares
// hosts case-insensitively, as DNS does.
func TestTheDetailsAddressHostMatchIgnoresCase(t *testing.T) {
	p, err := loadDefinition(t, detailsDefinitionFile).plan()
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	if _, err := p.detailsAddress("https://DETAILS.Example.ORG/item/1"); err != nil {
		t.Errorf("detailsAddress refused the base host in another case: %v", err)
	}
}

// TestADetailsRedirectToAnotherHostIsNotFollowed is criterion 4's redirect
// half: the details request follows httpx's strict same-host rule, so a
// redirect to another host fails and that host is never asked.
func TestADetailsRedirectToAnotherHostIsNotFollowed(t *testing.T) {
	var elsewhereHits atomic.Int32

	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		elsewhereHits.Add(1)
		_, _ = w.Write([]byte(`<a class="magnet" href="magnet:?xt=urn:btih:` + magnetPageHash + `">m</a>`))
	}))
	t.Cleanup(elsewhere.Close)

	target := elsewhere.URL + "/item/1"

	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusFound)
	}))
	t.Cleanup(src.Close)

	def := loadDefinition(t, detailsDefinitionFile)
	address := src.URL
	def.BaseURL = address

	a, err := New(Options{Definition: def, Client: testClient(httpx.Config{})})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = a.Resolve(testContext(t), indexer.Result{Title: "Moved", SourceURL: src.URL + "/item/1"})
	if !errors.Is(err, httpx.ErrCrossHostRedirect) {
		t.Fatalf("Resolve error = %v, want httpx.ErrCrossHostRedirect", err)
	}

	if n := elsewhereHits.Load(); n != 0 {
		t.Fatalf("the other host was asked %d times, want never", n)
	}
}

// TestAnAlreadyResolvedResultFetchesNoDetailsPage is criterion 2's no-op:
// a result carrying a magnet, a torrent link or a valid infohash is handled
// exactly as before the details block existed, with no request.
func TestAnAlreadyResolvedResultFetchesNoDetailsPage(t *testing.T) {
	cases := map[string]indexer.Result{
		"a magnet":       {Title: "M", Magnet: "magnet:?xt=urn:btih:" + magnetPageHash},
		"a torrent link": {Title: "T", TorrentURL: "https://details.example.org/dl/1.torrent"},
		"an infohash":    {Title: "H", InfoHash: infohashPageHash},
	}

	for name, listed := range cases {
		t.Run(name, func(t *testing.T) {
			transport := &countingTransport{}

			a, err := New(Options{
				Definition: loadDefinition(t, detailsDefinitionFile),
				Client:     testClient(httpx.Config{Transport: transport}),
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			listed.SourceURL = "https://details.example.org/item/1"

			got, err := a.Resolve(testContext(t), listed)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}

			if n := transport.calls.Load(); n != 0 {
				t.Fatalf("Resolve made %d requests for a result that needed none", n)
			}

			if listed.Magnet != "" || listed.TorrentURL != "" {
				assertResult(t, got, listed)
			}
		})
	}
}

// TestWithoutADetailsBlockResolveIsUnchanged is criterion 1's other half: a
// definition without a details block never fetches the source_url, and a
// link-less result is ErrUnresolvable exactly as before.
func TestWithoutADetailsBlockResolveIsUnchanged(t *testing.T) {
	transport := &countingTransport{}

	a, err := New(Options{
		Definition: loadDefinition(t, htmlDefinitionFile),
		Client:     testClient(httpx.Config{Transport: transport}),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = a.Resolve(testContext(t), indexer.Result{Title: "Page only", SourceURL: "https://archive.example.org/item/1"})
	if !errors.Is(err, ErrUnresolvable) {
		t.Fatalf("Resolve error = %v, want ErrUnresolvable", err)
	}

	if n := transport.calls.Load(); n != 0 {
		t.Fatalf("Resolve made %d requests with no details block", n)
	}
}

// TestTheDetailsRequestCarriesTheUsersCredentials is criterion 6: the
// details page is fetched by the same client as the search, so the cookie
// and api key the user configured go with it.
func TestTheDetailsRequestCarriesTheUsersCredentials(t *testing.T) {
	src := detailsSource(t)
	a := detailsAdapter(t, src, credentialledClient())
	listed := searchDetails(t, src, a)["/item/2001"]

	if _, err := a.Resolve(testContext(t), listed); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	for _, r := range src.requests() {
		if r.cookieHdr != testCookie {
			t.Errorf("request to %s carried cookie %q, want the user's", r.path, r.cookieHdr)
		}

		if got := r.query.Get(httpx.DefaultAPIKeyParam); got != testKey {
			t.Errorf("request to %s carried api key %q, want the user's", r.path, got)
		}
	}

	if n := requestsTo(src, "/item/2001"); n != 1 {
		t.Fatalf("the details page was fetched %d times, want once", n)
	}
}

// recordingClock is an httpx.Clock that never sleeps and records every wait
// it was asked for.
type recordingClock struct {
	now    time.Time
	mu     sync.Mutex
	sleeps []time.Duration
}

func (c *recordingClock) Now() time.Time { return c.now }

func (c *recordingClock) Sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	c.sleeps = append(c.sleeps, d)
	c.mu.Unlock()

	return ctx.Err()
}

// TestTheDetailsRequestWaitsOutThePerHostInterval is criterion 5's spacing
// clause: the details request goes through the same per-host limiter as the
// search, so a resolve straight after a search waits the interval.
func TestTheDetailsRequestWaitsOutThePerHostInterval(t *testing.T) {
	const interval = 1500 * time.Millisecond

	clock := &recordingClock{now: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)}
	client := httpx.New(httpx.Config{MinHostInterval: interval, MaxAttempts: 1, Clock: clock})

	src := detailsSource(t)
	a := detailsAdapter(t, src, client)
	listed := searchDetails(t, src, a)["/item/2001"]

	if _, err := a.Resolve(testContext(t), listed); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	clock.mu.Lock()
	defer clock.mu.Unlock()

	if len(clock.sleeps) != 1 || clock.sleeps[0] != interval {
		t.Fatalf("waits = %v, want exactly one %v wait before the details request", clock.sleeps, interval)
	}
}

// TestTheDetailsResponseIsSizeCapped is criterion 5's size clause.
func TestTheDetailsResponseIsSizeCapped(t *testing.T) {
	src := detailsSource(t)
	a := detailsAdapter(t, src, testClient(httpx.Config{MaxBodyBytes: 64}))
	page := src.server.URL + "/item/2001"
	listed := indexer.Result{Title: "Big", SourceURL: page}

	got, err := a.Resolve(testContext(t), listed)
	if !errors.Is(err, httpx.ErrBodyTooLarge) {
		t.Fatalf("Resolve error = %v, want httpx.ErrBodyTooLarge", err)
	}

	assertResult(t, got, listed)
}

// TestTheDetailsRequestHonoursTheContext is criterion 5's deadline clause:
// an already-cancelled context makes no request.
func TestTheDetailsRequestHonoursTheContext(t *testing.T) {
	src := detailsSource(t)
	a := detailsAdapter(t, src, testClient(httpx.Config{}))

	ctx, cancel := contextCancelled(t)
	cancel()

	page := src.server.URL + "/item/2001"

	_, err := a.Resolve(ctx, indexer.Result{Title: "Late", SourceURL: page})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve error = %v, want context.Canceled", err)
	}

	if n := len(src.requests()); n != 0 {
		t.Fatalf("a cancelled resolve made %d requests", n)
	}
}

// TestADetailsBlockReadsItsOwnMode pins that details.mode overrides the
// definition's: an html listing whose item pages are JSON.
func TestADetailsBlockReadsItsOwnMode(t *testing.T) {
	src := newSource(t, map[string]reply{
		"/item/1": jsonReply(`{"item": {"links": {"magnet": "magnet:?xt=urn:btih:` + magnetPageHash + `"}}}`),
	})

	def := mustParse(t, detailsDefinitionHead+`
details:
  mode: json
  fields:
    magnet:
      selector: item.links.magnet
`)

	a, err := New(Options{Definition: src.pointAt(def), Client: testClient(httpx.Config{})})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	page := src.server.URL + "/item/1"

	got, err := a.Resolve(testContext(t), indexer.Result{Title: "J", SourceURL: page})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if want := "magnet:?xt=urn:btih:" + magnetPageHash; got.Magnet != want || got.InfoHash != magnetPageHash {
		t.Errorf("got magnet %q hash %q, want %q and %q", got.Magnet, got.InfoHash, want, magnetPageHash)
	}
}

// detailsDefinitionHead is a minimal definition whose search block relies on
// a details block, for the validation table below to append one to.
const detailsDefinitionHead = `id: details-validation
base_url: https://details.example.org
search:
  path: /search
  rows: li
  fields:
    title:
      selector: a
    source_url:
      selector: a
      attr: href
`

// TestBadDetailsBlocksAreRefused is criterion 1's strict validation: each
// bad details block is refused, naming the key.
func TestBadDetailsBlocksAreRefused(t *testing.T) {
	cases := []struct {
		name     string
		details  string
		head     string
		location string
		want     error
	}{
		{
			name:     "a listing field",
			details:  "details:\n  fields:\n    title:\n      selector: h1\n",
			location: "details.fields.title",
			want:     ErrDetailsFieldUnsupported,
		},
		{
			name:     "a field the schema does not have",
			details:  "details:\n  fields:\n    magnet:\n      selector: a\n    bogus:\n      selector: b\n",
			location: "details.fields.bogus",
			want:     ErrDetailsFieldUnsupported,
		},
		{
			name:     "no fields",
			details:  "details:\n  mode: html\n",
			location: "details.fields",
			want:     ErrDetailsFieldsMissing,
		},
		{
			name:     "an unknown mode",
			details:  "details:\n  mode: xml\n  fields:\n    magnet:\n      selector: a\n",
			location: "details.mode",
			want:     ErrModeUnknown,
		},
		{
			name:     "a bad selector",
			details:  "details:\n  fields:\n    magnet:\n      selector: \"a[\"\n",
			location: "details.fields.magnet.selector",
			want:     ErrSelectorInvalid,
		},
		{
			name:     "a bad regex",
			details:  "details:\n  fields:\n    infohash:\n      selector: code\n      regex: \"(\"\n",
			location: "details.fields.infohash.regex",
			want:     ErrRegexInvalid,
		},
		{
			name:     "an unknown transform",
			details:  "details:\n  fields:\n    infohash:\n      selector: code\n      transform: [shout]\n",
			location: "details.fields.infohash.transform",
			want:     ErrTransformUnknown,
		},
		{
			name:     "a template without {{value}}",
			details:  "details:\n  fields:\n    torrent_url:\n      selector: a\n      template: /dl/fixed.torrent\n",
			location: "details.fields.torrent_url.template",
			want:     ErrTemplateValueMissing,
		},
		{
			name:     "layouts",
			details:  "details:\n  fields:\n    magnet:\n      selector: a\n      layouts: [\"2006\"]\n",
			location: "details.fields.magnet.layouts",
			want:     ErrLayoutsOutsidePublished,
		},
		{
			name:     "attr in a json details block",
			details:  "details:\n  mode: json\n  fields:\n    magnet:\n      selector: links.magnet\n      attr: href\n",
			location: "details.fields.magnet",
			want:     ErrAttrInJSONMode,
		},
		{
			name: "a relying block with no source_url",
			head: `id: details-validation
base_url: https://details.example.org
search:
  rows: li
  fields:
    title:
      selector: a
`,
			details:  "details:\n  fields:\n    magnet:\n      selector: a\n",
			location: "search.fields.source_url",
			want:     ErrDetailsSourceMissing,
		},
		{
			name: "no link and no details block",
			head: detailsDefinitionHead,
			// No details block at all: the pre-T-9037 rule stands.
			details:  "",
			location: "search.fields",
			want:     ErrLinkFieldMissing,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			head := tc.head
			if head == "" {
				head = detailsDefinitionHead
			}

			_, err := Parse([]byte(head + tc.details))
			if !errors.Is(err, tc.want) {
				t.Fatalf("Parse error = %v, want %v", err, tc.want)
			}

			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("Parse error %v is not a *ValidationError", err)
			}

			if verr.Location != tc.location {
				t.Errorf("Location = %q, want %q", verr.Location, tc.location)
			}
		})
	}
}

// TestAnUnknownDetailsKeyIsRefusedByTheStrictDecoder pins that a key the
// details block does not define — a rows selector, a path — is a decode
// error, not something silently ignored.
func TestAnUnknownDetailsKeyIsRefusedByTheStrictDecoder(t *testing.T) {
	for _, key := range []string{"rows: div", "path: /item", "trust: {}"} {
		_, err := Parse([]byte(detailsDefinitionHead + "details:\n  " + key + "\n  fields:\n    magnet:\n      selector: a\n"))
		if !errors.Is(err, ErrDefinitionMalformed) {
			t.Errorf("details.%s: Parse error = %v, want ErrDefinitionMalformed", key, err)
		}
	}
}
