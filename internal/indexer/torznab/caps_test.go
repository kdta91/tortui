package torznab

import (
	"errors"
	"net/http"
	"slices"
	"testing"

	"github.com/kdta91/tortui/internal/indexer"
)

func TestDiscoverReadsCapsAndProbesForALatestFeed(t *testing.T) {
	t.Parallel()

	src := newSource(t, map[string]reply{
		functionCaps:   fixtureReply(t, "caps-full.xml"),
		functionSearch: fixtureReply(t, "latest-feed.xml"),
	})

	a := mustDiscover(t, src)

	want := indexer.Caps{
		Search:         true,
		Latest:         true,
		Categories:     true,
		Pagination:     true,
		ProvidesMagnet: true,
	}

	if got := a.Caps(); got != want {
		t.Fatalf("Caps = %+v, want %+v", got, want)
	}

	requests := src.requests()
	if len(requests) != 2 {
		t.Fatalf("the probe made %d requests, want 2 (caps, then the latest probe): %v", len(requests), requests)
	}

	if got := requests[0].Get(paramFunction); got != functionCaps {
		t.Errorf("first request was t=%s, want t=%s", got, functionCaps)
	}

	probe := requests[1]

	if got := probe.Get(paramFunction); got != functionSearch {
		t.Errorf("second request was t=%s, want t=%s", got, functionSearch)
	}

	if _, present := probe["q"]; present {
		t.Errorf("the latest probe sent q=%q; a recent-additions request has no keyword", probe.Get("q"))
	}

	if got := probe.Get("limit"); got != "1" {
		t.Errorf("the latest probe asked for limit=%q, want 1: it only needs to know whether anything comes back", got)
	}

	if got := probe.Get(paramExtended); got != "1" {
		t.Errorf("the latest probe sent extended=%q, want 1, or the magneturl attribute it reads may be omitted", got)
	}

	// The caps document's own ids, mapped through the shared taxonomy.
	wantIDs := map[indexer.Category][]int{
		indexer.CategorySoftware: {1000},
		indexer.CategoryAudio:    {3000},
		indexer.CategoryText:     {7000},
	}

	if len(a.categoryIDs) != len(wantIDs) {
		t.Fatalf("categoryIDs = %v, want %v", a.categoryIDs, wantIDs)
	}

	for bucket, ids := range wantIDs {
		if !slices.Equal(a.categoryIDs[bucket], ids) {
			t.Errorf("categoryIDs[%s] = %v, want %v", bucket, a.categoryIDs[bucket], ids)
		}
	}
}

func TestDiscoverOnAMinimalCapsDocument(t *testing.T) {
	t.Parallel()

	src := newSource(t, map[string]reply{
		functionCaps:   fixtureReply(t, "caps-minimal.xml"),
		functionSearch: fixtureReply(t, "search-empty.xml"),
	})

	a := mustDiscover(t, src)

	want := indexer.Caps{Search: true}

	if got := a.Caps(); got != want {
		t.Fatalf("Caps = %+v, want %+v: nothing the document did not declare may be claimed", got, want)
	}
}

func TestDiscoverFailsClosedOnAnEmptyLatestFeed(t *testing.T) {
	t.Parallel()

	src := newSource(t, map[string]reply{
		functionCaps:   fixtureReply(t, "caps-full.xml"),
		functionSearch: fixtureReply(t, "search-empty.xml"),
	})

	a := mustDiscover(t, src)

	if a.Caps().Latest {
		t.Fatal("Caps.Latest = true for a server that answered the probe with an empty feed; " +
			"an empty index and a server that ignores an empty keyword are indistinguishable, so this must fail closed")
	}

	if a.Caps().ProvidesMagnet {
		t.Fatal("Caps.ProvidesMagnet = true with no item to have seen a magnet on")
	}

	if !a.Caps().Search {
		t.Fatal("Caps.Search = false; the empty feed says nothing about keyword search")
	}
}

func TestProvidesMagnetNeedsEveryProbeItemToHaveOne(t *testing.T) {
	t.Parallel()

	// search-full.xml's second item publishes no magneturl, which is what
	// Caps.ProvidesMagnet is about: Resolve is only a no-op if every result
	// already carries one.
	src := newSource(t, searchable(t))

	a := mustDiscover(t, src)

	if !a.Caps().Latest {
		t.Fatal("Caps.Latest = false for a probe that returned items")
	}

	if a.Caps().ProvidesMagnet {
		t.Fatal("Caps.ProvidesMagnet = true although one probe item carried no magnet")
	}
}

func TestDiscoverHandlesAServerThatOmitsCaps(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		caps         reply
		wantAuth     bool
		wantInnerErr error
	}{
		"no caps endpoint at all": {caps: statusReply(http.StatusNotFound)},
		"server error":            {caps: statusReply(http.StatusInternalServerError)},
		"empty body":              {caps: xmlReply(""), wantInnerErr: ErrDocumentEmpty},
		"an html error page":      {caps: xmlReply(fixture(t, "caps-html.xml")), wantInnerErr: ErrDocumentUnexpectedRoot},
		"truncated xml":           {caps: xmlReply(fixture(t, "caps-truncated.xml")), wantInnerErr: ErrDocumentMalformed},
		"a feed instead of caps":  {caps: fixtureReply(t, "search-empty.xml"), wantInnerErr: ErrDocumentUnexpectedRoot},
		"an api error":            {caps: fixtureReply(t, "caps-error.xml"), wantAuth: true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			src := newSource(t, map[string]reply{
				functionCaps:   tc.caps,
				functionSearch: fixtureReply(t, "search-full.xml"),
			})

			a, err := Discover(testContext(t), src.options())

			if a == nil {
				t.Fatal("Discover returned no adapter; a source whose caps document is unusable is still searchable")
			}

			if err == nil {
				t.Fatal("Discover returned no error; the caller has to be able to tell the user the probe failed")
			}

			if !errors.Is(err, ErrCapsUnavailable) {
				t.Fatalf("Discover error = %v, want it to wrap ErrCapsUnavailable", err)
			}

			if tc.wantInnerErr != nil && !errors.Is(err, tc.wantInnerErr) {
				t.Errorf("Discover error = %v, want it to wrap %v", err, tc.wantInnerErr)
			}

			want := indexer.Caps{Search: true, RequiresAuth: tc.wantAuth}

			if got := a.Caps(); got != want {
				t.Fatalf("Caps = %+v, want the fail-closed baseline %+v", got, want)
			}

			// The point of handing back an adapter: it still works.
			results, err := a.Search(testContext(t), indexer.Query{Text: "invented"})
			if err != nil {
				t.Fatalf("Search on the unprobed adapter: %v", err)
			}

			if len(results) != 2 {
				t.Fatalf("Search returned %d results, want 2", len(results))
			}
		})
	}
}

func TestAnAuthErrorFromTheCapsProbeRaisesRequiresAuth(t *testing.T) {
	t.Parallel()

	src := newSource(t, map[string]reply{functionCaps: fixtureReply(t, "caps-error.xml")})

	a, err := Discover(testContext(t), src.options())
	if err == nil {
		t.Fatal("want an error")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Discover error = %v, want an *APIError in the chain", err)
	}

	if apiErr.Code != 100 || !apiErr.IsAuth() {
		t.Fatalf("APIError = %+v, want code 100 in the auth family", apiErr)
	}

	if !a.Caps().RequiresAuth {
		t.Fatal("Caps.RequiresAuth = false although the source answered that it needs credentials")
	}
}

func TestRequiresAuthIsNeverLoweredByAProbe(t *testing.T) {
	t.Parallel()

	src := newSource(t, map[string]reply{
		functionCaps:   fixtureReply(t, "caps-full.xml"),
		functionSearch: fixtureReply(t, "latest-feed.xml"),
	})

	opts := src.options()
	opts.RequiresAuth = true

	a, err := Discover(testContext(t), opts)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if !a.Caps().RequiresAuth {
		t.Fatal("a successful probe cleared RequiresAuth; the user configured credentials for this source")
	}
}

func TestDiscoverDoesNotProbeLatestWhenTheSourceCannotSearch(t *testing.T) {
	t.Parallel()

	src := newSource(t, map[string]reply{
		functionCaps:   fixtureReply(t, "caps-no-search.xml"),
		functionSearch: fixtureReply(t, "latest-feed.xml"),
	})

	a := mustDiscover(t, src)

	want := indexer.Caps{Categories: true, Pagination: true}

	if got := a.Caps(); got != want {
		t.Fatalf("Caps = %+v, want %+v: a server that declares search unavailable has no feed either", got, want)
	}

	if requests := src.requests(); len(requests) != 1 {
		t.Fatalf("the probe made %d requests, want 1: there is nothing to probe for", len(requests))
	}
}

func TestALatestProbeFailureLeavesTheRestOfCapsIntact(t *testing.T) {
	t.Parallel()

	src := newSource(t, map[string]reply{
		functionCaps:   fixtureReply(t, "caps-full.xml"),
		functionSearch: statusReply(http.StatusServiceUnavailable),
	})

	a, err := Discover(testContext(t), src.options())
	if err == nil {
		t.Fatal("want the probe failure reported")
	}

	if errors.Is(err, ErrCapsUnavailable) {
		t.Fatalf("error = %v, want it NOT to claim the caps document was unavailable: it parsed fine", err)
	}

	want := indexer.Caps{Search: true, Categories: true, Pagination: true}

	if got := a.Caps(); got != want {
		t.Fatalf("Caps = %+v, want %+v: what the caps document said survives, Latest fails closed", got, want)
	}
}

func TestDiscoverRefusesUnusableOptions(t *testing.T) {
	t.Parallel()

	a, err := Discover(testContext(t), Options{})

	if !errors.Is(err, ErrIDEmpty) {
		t.Fatalf("Discover error = %v, want ErrIDEmpty", err)
	}

	if a != nil {
		t.Fatal("Discover returned an adapter for options that cannot produce one")
	}
}

func TestNewMakesNoRequestAtAll(t *testing.T) {
	t.Parallel()

	src := newSource(t, searchable(t))

	a := mustNew(t, src)

	if requests := src.requests(); len(requests) != 0 {
		t.Fatalf("New made %d requests, want none", len(requests))
	}

	want := indexer.Caps{Search: true}

	if got := a.Caps(); got != want {
		t.Fatalf("Caps = %+v, want the baseline %+v", got, want)
	}
}

func TestSearchModeAvailability(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"yes": true, "YES": true, "true": true, "1": true, "y": true,
		"no": false, "false": false, "0": false, "": false, "maybe": false,
	}

	for value, want := range cases {
		if got := (capsSearchMode{Available: value}).available(); got != want {
			t.Errorf("available(%q) = %t, want %t", value, got, want)
		}
	}
}

func TestPaginationComesFromTheDeclaredLimits(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"100": true,
		"1":   true,
		"0":   false,
		"-1":  false,
		"":    false,
		"all": false,
	}

	for value, want := range cases {
		if got := paginated(capsLimits{Max: value}); got != want {
			t.Errorf("paginated(max=%q) = %t, want %t", value, got, want)
		}
	}
}

func TestCategoryIDsSkipWhatTortuiCannotPlace(t *testing.T) {
	t.Parallel()

	doc := capsDocument{Categories: capsCategories{Categories: []capsCategory{
		{ID: "3000"},
		{ID: "3010"},
		{ID: "8000"},   // the catch-all block: CategoryOther, so not a filter
		{ID: "120000"}, // a site-specific id: also CategoryOther
		{ID: "banana"},
		{ID: ""},
	}}}

	ids := categoryIDs(doc)

	if !slices.Equal(ids[indexer.CategoryAudio], []int{3000, 3010}) {
		t.Fatalf("audio ids = %v, want [3000 3010]", ids[indexer.CategoryAudio])
	}

	if len(ids) != 1 {
		t.Fatalf("categoryIDs = %v, want only the audio bucket", ids)
	}

	if categoryIDs(capsDocument{}) != nil {
		t.Fatal("a caps document with no categories must produce a nil map, not an empty one")
	}
}
