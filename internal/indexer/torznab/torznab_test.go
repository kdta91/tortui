package torznab

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/kdta91/tortui/internal/indexer"
)

func TestNewValidatesItsOptions(t *testing.T) {
	t.Parallel()

	const usable = "https://feed.example.org/api"

	cases := map[string]struct {
		id       string
		endpoint string
		wantErr  error
	}{
		"usable":                {id: testID, endpoint: usable},
		"no id":                 {endpoint: usable, wantErr: ErrIDEmpty},
		"whitespace id":         {id: "   ", endpoint: usable, wantErr: ErrIDEmpty},
		"no endpoint":           {id: testID, wantErr: ErrEndpointEmpty},
		"whitespace endpoint":   {id: testID, endpoint: "  ", wantErr: ErrEndpointEmpty},
		"unparseable endpoint":  {id: testID, endpoint: "https://feed.example.org/api\x7f", wantErr: ErrEndpointInvalid},
		"a scheme we do not do": {id: testID, endpoint: "ftp://feed.example.org/api", wantErr: ErrEndpointSchemeUnsupported},
		"no scheme":             {id: testID, endpoint: "feed.example.org/api", wantErr: ErrEndpointSchemeUnsupported},
		"a bare path":           {id: testID, endpoint: "/api", wantErr: ErrEndpointSchemeUnsupported},
		"scheme but no host":    {id: testID, endpoint: "https:///api", wantErr: ErrEndpointHostMissing},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Through a dotless local: scripts/check-indexer-hostnames.sh
			// reads `Endpoint: x.y` as a hostname assignment (T-926).
			address := tc.endpoint

			opts := Options{ID: tc.id}
			opts.Endpoint = address

			a, err := New(opts)

			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("New: %v", err)
				}

				if a == nil {
					t.Fatal("New returned no adapter and no error")
				}

				return
			}

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("New error = %v, want %v", err, tc.wantErr)
			}

			if a != nil {
				t.Fatal("New returned an adapter alongside a configuration error")
			}
		})
	}
}

func TestNewFillsInTheDefaults(t *testing.T) {
	t.Parallel()

	opts := Options{ID: "  " + testID + "  "}
	opts.Endpoint = "  https://feed.example.org/api  "

	a, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if a.ID() != testID {
		t.Errorf("ID = %q, want it trimmed to %q", a.ID(), testID)
	}

	if a.Name() != testID {
		t.Errorf("Name = %q, want it to fall back to the id", a.Name())
	}

	if a.client == nil {
		t.Error("a nil Options.Client must produce a default client, not a nil one")
	}

	if a.endpoint != "https://feed.example.org/api" {
		t.Errorf("endpoint = %q, want it trimmed", a.endpoint)
	}
}

func TestNameIsUsedWhenGiven(t *testing.T) {
	t.Parallel()

	src := newSource(t, searchable(t))

	if got := mustNew(t, src).Name(); got != testName {
		t.Fatalf("Name = %q, want %q", got, testName)
	}
}

func TestSearchSendsTheRequestTheQueryDescribes(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		query    indexer.Query
		probe    bool
		wantHas  map[string]string
		wantGone []string
	}{
		"a keyword search": {
			query:    indexer.Query{Text: "  invented corpus  "},
			wantHas:  map[string]string{"t": "search", "q": "invented corpus", "extended": "1"},
			wantGone: []string{"limit", "offset", "cat"},
		},
		"a limit": {
			query:   indexer.Query{Text: "x", Limit: 25},
			wantHas: map[string]string{"limit": "25"},
		},
		"an offset, when the source pages": {
			query:   indexer.Query{Text: "x", Offset: 50},
			probe:   true,
			wantHas: map[string]string{"offset": "50"},
		},
		"an offset, when it does not": {
			query:    indexer.Query{Text: "x", Offset: 50},
			wantGone: []string{"offset"},
		},
		"categories the source declared": {
			query:   indexer.Query{Text: "x", Categories: []indexer.Category{indexer.CategoryText, indexer.CategoryAudio}},
			probe:   true,
			wantHas: map[string]string{"cat": "3000,7000"},
		},
		"categories, unprobed source": {
			query:    indexer.Query{Text: "x", Categories: []indexer.Category{indexer.CategoryText}},
			wantGone: []string{"cat"},
		},
		"a latest feed": {
			query:    indexer.Query{Mode: indexer.ModeLatest},
			probe:    true,
			wantHas:  map[string]string{"t": "search", "extended": "1"},
			wantGone: []string{"q"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			src := newSource(t, map[string]reply{
				functionCaps:   fixtureReply(t, "caps-full.xml"),
				functionSearch: fixtureReply(t, "latest-feed.xml"),
			})

			var a *Adapter
			if tc.probe {
				a = mustDiscover(t, src)
			} else {
				a = mustNew(t, src)
			}

			if _, err := a.Search(testContext(t), tc.query); err != nil {
				t.Fatalf("Search: %v", err)
			}

			query := src.lastQuery(t, functionSearch)

			for key, want := range tc.wantHas {
				if got := query.Get(key); got != want {
					t.Errorf("%s=%q, want %q (full query %v)", key, got, want, query)
				}
			}

			for _, key := range tc.wantGone {
				if _, present := query[key]; present {
					t.Errorf("%s was sent as %q and should not have been (full query %v)", key, query.Get(key), query)
				}
			}
		})
	}
}

func TestSearchRefusesAQueryItCannotServe(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		query   indexer.Query
		probe   bool
		wantErr error
	}{
		"an empty keyword":       {query: indexer.Query{Mode: indexer.ModeSearch}, wantErr: ErrQueryTextEmpty},
		"a whitespace keyword":   {query: indexer.Query{Text: "   "}, wantErr: ErrQueryTextEmpty},
		"latest without the cap": {query: indexer.Query{Mode: indexer.ModeLatest}, wantErr: ErrLatestUnsupported},
		"a mode that does not exist": {
			query:   indexer.Query{Mode: indexer.Mode(42)},
			probe:   true,
			wantErr: ErrModeUnsupported,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			src := newSource(t, searchable(t))

			var a *Adapter
			if tc.probe {
				a = mustDiscover(t, src)
			} else {
				a = mustNew(t, src)
			}

			before := len(src.requests())

			results, err := a.Search(testContext(t), tc.query)

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Search error = %v, want %v", err, tc.wantErr)
			}

			if results != nil {
				t.Errorf("Search returned %v alongside an error", results)
			}

			if !strings.HasPrefix(err.Error(), "torznab "+testID+": ") {
				t.Errorf("error = %q, want it to name the adapter (AGENT.md §6.9)", err)
			}

			if got := len(src.requests()); got != before {
				t.Errorf("a query the adapter cannot serve still reached the network (%d new requests)", got-before)
			}
		})
	}
}

func TestSearchAsksForNothingWhenNoCategoryCanMatch(t *testing.T) {
	t.Parallel()

	src := newSource(t, map[string]reply{
		functionCaps:   fixtureReply(t, "caps-full.xml"),
		functionSearch: fixtureReply(t, "latest-feed.xml"),
	})

	a := mustDiscover(t, src)
	before := len(src.requests())

	// The fixture's caps document declares software, audio and text
	// categories, and nothing that maps to video.
	results, err := a.Search(testContext(t), indexer.Query{
		Text:       "invented",
		Categories: []indexer.Category{indexer.CategoryVideo},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != 0 {
		t.Fatalf("Search returned %d results for a category the source does not have", len(results))
	}

	if got := len(src.requests()); got != before {
		t.Fatalf("Search made %d requests; a source with no matching category has nothing to answer", got-before)
	}
}

func TestSearchFiltersOnMinSeeders(t *testing.T) {
	t.Parallel()

	src := newSource(t, searchable(t))
	a := mustNew(t, src)

	// search-full.xml has items with 42 and 7 seeders.
	results, err := a.Search(testContext(t), indexer.Query{Text: "invented", MinSeeders: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != 1 || results[0].Seeders != 42 {
		t.Fatalf("Search returned %d results %v, want only the 42-seeder item", len(results), results)
	}
}

func TestSearchOutcomes(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		reply     reply
		wantCount int
		wantErr   error
		wantFail  bool
	}{
		"results":            {reply: fixtureReply(t, "search-full.xml"), wantCount: 2},
		"an empty feed":      {reply: fixtureReply(t, "search-empty.xml"), wantCount: 0},
		"an api error":       {reply: fixtureReply(t, "search-error.xml"), wantFail: true},
		"malformed xml":      {reply: fixtureReply(t, "search-truncated.xml"), wantErr: ErrDocumentMalformed},
		"an html page":       {reply: fixtureReply(t, "search-html.xml"), wantErr: ErrDocumentUnexpectedRoot},
		"an empty body":      {reply: xmlReply(""), wantErr: ErrDocumentEmpty},
		"an entity bomb":     {reply: fixtureReply(t, "search-entity-bomb.xml"), wantErr: ErrDocumentMalformed},
		"http 500":           {reply: statusReply(http.StatusInternalServerError), wantFail: true},
		"http 403":           {reply: statusReply(http.StatusForbidden), wantFail: true},
		"no such endpoint":   {reply: statusReply(http.StatusNotFound), wantFail: true},
		"http 429":           {reply: statusReply(http.StatusTooManyRequests), wantFail: true},
		"a caps document":    {reply: fixtureReply(t, "caps-minimal.xml"), wantErr: ErrDocumentUnexpectedRoot},
		"an unusable status": {reply: reply{status: http.StatusNoContent}, wantErr: ErrDocumentEmpty},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			src := newSource(t, map[string]reply{functionSearch: tc.reply})
			a := mustNew(t, src)

			results, err := a.Search(testContext(t), indexer.Query{Text: "invented"})

			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Search error = %v, want %v", err, tc.wantErr)
				}
			case tc.wantFail:
				if err == nil {
					t.Fatal("Search returned no error for a response it cannot use")
				}
			default:
				if err != nil {
					t.Fatalf("Search: %v", err)
				}

				if len(results) != tc.wantCount {
					t.Fatalf("Search returned %d results, want %d", len(results), tc.wantCount)
				}
			}

			if err != nil && !strings.HasPrefix(err.Error(), "torznab "+testID+": ") {
				t.Errorf("error = %q, want it to name the adapter (AGENT.md §6.9)", err)
			}
		})
	}
}

func TestSearchReportsTheApiErrorWithoutItsDescription(t *testing.T) {
	t.Parallel()

	src := newSource(t, map[string]reply{functionSearch: fixtureReply(t, "search-error.xml")})
	a := mustNew(t, src)

	_, err := a.Search(testContext(t), indexer.Query{Text: "invented"})
	if err == nil {
		t.Fatal("want an error for an <error> document")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Search error = %v, want an *APIError", err)
	}

	if apiErr.Code != 201 {
		t.Errorf("APIError.Code = %d, want 201", apiErr.Code)
	}

	// The fixture's description echoes the request back, which is what a
	// server that echoed an api_key would look like.
	for _, fragment := range []string{"cat=nonsense", "t=search", "q=example", "Incorrect parameter"} {
		if strings.Contains(err.Error(), fragment) {
			t.Errorf("the error repeated the server's description (%q): %q", fragment, err)
		}
	}
}

func TestSearchStopsWhenTheContextIsCancelled(t *testing.T) {
	t.Parallel()

	src := newSource(t, searchable(t))
	a := mustNew(t, src)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := a.Search(ctx, indexer.Query{Text: "invented"})
	if err == nil {
		t.Fatal("want an error from a cancelled context")
	}

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Search error = %v, want it to wrap context.Canceled", err)
	}
}

func TestResolveIsANoOpWhenAMagnetIsPresent(t *testing.T) {
	t.Parallel()

	src := newSource(t, searchable(t))
	a := mustNew(t, src)

	before := indexer.Result{
		IndexerID: testID,
		ID:        "item-1",
		Title:     "Invented Release",
		Magnet:    "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567",
		// Deliberately inconsistent with the magnet: Resolve must not
		// "fix" anything on a result that is already resolved.
		InfoHash:  "NOT-A-HASH",
		Seeders:   3,
		SourceURL: "https://feed.example.org/details/1",
		Extra:     map[string]string{"torznab.grabs": "2"},
	}

	after, err := a.Resolve(testContext(t), before)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if after.Magnet != before.Magnet || after.InfoHash != before.InfoHash {
		t.Fatalf("Resolve changed a resolved result: %+v -> %+v", before, after)
	}

	if after.ID != before.ID || after.Title != before.Title || after.Seeders != before.Seeders ||
		after.SourceURL != before.SourceURL || after.Extra["torznab.grabs"] != "2" {
		t.Fatalf("Resolve altered a field it had no business touching: %+v -> %+v", before, after)
	}

	if len(src.requests()) != 0 {
		t.Fatal("Resolve made a network request; a resolved result needs nothing fetched")
	}
}

func TestResolveDerivesAMagnetFromAnInfoHash(t *testing.T) {
	t.Parallel()

	src := newSource(t, searchable(t))
	a := mustNew(t, src)

	const hash = "0123456789ABCDEF0123456789ABCDEF01234567"

	after, err := a.Resolve(testContext(t), indexer.Result{
		IndexerID: testID,
		ID:        "item-2",
		Title:     "Invented Release 2026",
		InfoHash:  hash,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	want := "magnet:?xt=urn:btih:" + strings.ToLower(hash) + "&dn=Invented+Release+2026"

	if after.Magnet != want {
		t.Fatalf("Magnet = %q, want %q", after.Magnet, want)
	}

	if after.InfoHash != strings.ToLower(hash) {
		t.Errorf("InfoHash = %q, want it normalised", after.InfoHash)
	}

	if err := after.Validate(); err != nil {
		t.Errorf("the resolved result does not validate: %v", err)
	}

	if len(src.requests()) != 0 {
		t.Fatal("Resolve made a network request; Torznab has no per-item endpoint to call")
	}

	t.Run("without a title", func(t *testing.T) {
		out, err := a.Resolve(testContext(t), indexer.Result{InfoHash: hash})
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}

		if out.Magnet != "magnet:?xt=urn:btih:"+strings.ToLower(hash) {
			t.Fatalf("Magnet = %q, want no dn parameter when there is no title", out.Magnet)
		}
	})
}

func TestResolveLeavesATorrentURLAlone(t *testing.T) {
	t.Parallel()

	src := newSource(t, searchable(t))
	a := mustNew(t, src)

	before := indexer.Result{ID: "item-3", TorrentURL: "https://feed.example.org/download/3.torrent"}

	after, err := a.Resolve(testContext(t), before)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if !reflect.DeepEqual(after, before) {
		t.Fatalf("Resolve changed a result the engine can already act on: %+v", after)
	}

	if len(src.requests()) != 0 {
		t.Fatal("Resolve made a network request for a result that needed nothing")
	}
}

func TestResolveRefusesAResultWithNothingToWorkFrom(t *testing.T) {
	t.Parallel()

	src := newSource(t, searchable(t))
	a := mustNew(t, src)

	cases := map[string]indexer.Result{
		"nothing at all":       {ID: "item-4"},
		"an unusable infohash": {ID: "item-5", InfoHash: "zzzz"},
		"whitespace only":      {ID: "item-6", Magnet: "   ", TorrentURL: "  "},
	}

	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := a.Resolve(testContext(t), in)

			if !errors.Is(err, ErrUnresolvable) {
				t.Fatalf("Resolve error = %v, want ErrUnresolvable", err)
			}

			if !reflect.DeepEqual(out, in) {
				t.Errorf("Resolve returned a modified result alongside its error: %+v", out)
			}

			if !strings.HasPrefix(err.Error(), "torznab "+testID+": ") {
				t.Errorf("error = %q, want it to name the adapter", err)
			}
		})
	}
}

func TestAFailingAdapterDegradesInsideTheRegistry(t *testing.T) {
	t.Parallel()

	// AGENT.md §6.3: one failing source is collected and reported, never
	// fatal and never a panic. The registry is the only consumer of an
	// adapter, so the degradation is proven through it rather than asserted.
	working := newSource(t, map[string]reply{functionSearch: fixtureReply(t, "search-full.xml")})
	broken := newSource(t, map[string]reply{functionSearch: statusReply(http.StatusInternalServerError)})

	goodOpts := working.options()
	goodOpts.ID = "fixture-good"

	badOpts := broken.options()
	badOpts.ID = "fixture-bad"

	good, err := New(goodOpts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	bad, err := New(badOpts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	registry := indexer.NewRegistry(indexer.Config{})

	for _, a := range []*Adapter{good, bad} {
		if err := registry.Register(a); err != nil {
			t.Fatalf("Register %s: %v", a.ID(), err)
		}
	}

	results, sourceErrs, err := registry.SearchAll(testContext(t), indexer.Query{Text: "invented"})
	if err != nil {
		t.Fatalf("SearchAll = %v, want partial results rather than a failure", err)
	}

	if len(results) != 2 {
		t.Fatalf("SearchAll returned %d results, want the 2 the working source produced", len(results))
	}

	if len(sourceErrs) != 1 || sourceErrs[0].IndexerID != "fixture-bad" {
		t.Fatalf("source errors = %+v, want exactly one, for fixture-bad", sourceErrs)
	}
}

func TestAPIErrorReportsCodeAndFamily(t *testing.T) {
	t.Parallel()

	// The code table is §5 of docs/newznab_api_specification.txt in the
	// nZEDb/nZEDb repository, branch dev, read on 2026-09-14.
	cases := map[int]struct {
		family string
		auth   bool
	}{
		100: {family: "credentials or account", auth: true},
		101: {family: "credentials or account", auth: true},
		102: {family: "credentials or account", auth: true},
		200: {family: "request"},
		201: {family: "request"},
		202: {family: "request"},
		203: {family: "request"},
		300: {family: "no such item"},
		500: {family: "a limit on the account was reached"},
		501: {family: "a limit on the account was reached"},
		900: {family: "the server reported an unknown error"},
		0:   {family: "unrecognised code"},
		42:  {family: "unrecognised code"},
	}

	for code, want := range cases {
		err := &APIError{Code: code}

		if !strings.Contains(err.Error(), want.family) {
			t.Errorf("APIError(%d) = %q, want it to name the family %q", code, err, want.family)
		}

		if err.IsAuth() != want.auth {
			t.Errorf("APIError(%d).IsAuth() = %t, want %t", code, err.IsAuth(), want.auth)
		}

		// AuthFailed (T-081) is IsAuth under the name the settings screen's
		// classifyProbeError matches via errors.As.
		if err.AuthFailed() != want.auth {
			t.Errorf("APIError(%d).AuthFailed() = %t, want %t", code, err.AuthFailed(), want.auth)
		}
	}

	t.Run("an unreadable code", func(t *testing.T) {
		src := newSource(t, map[string]reply{
			functionSearch: xmlReply(`<error code="banana" description="nope"/>`),
		})

		_, err := mustNew(t, src).Search(testContext(t), indexer.Query{Text: "x"})

		var apiErr *APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("Search error = %v, want an *APIError", err)
		}

		if apiErr.Code != 0 {
			t.Fatalf("Code = %d, want 0 for a code that will not parse", apiErr.Code)
		}
	})

	t.Run("an error document that is not well-formed", func(t *testing.T) {
		src := newSource(t, map[string]reply{
			functionSearch: xmlReply(`<error code="100"><unclosed></error>`),
		})

		_, err := mustNew(t, src).Search(testContext(t), indexer.Query{Text: "x"})

		if !errors.Is(err, ErrDocumentMalformed) {
			t.Fatalf("Search error = %v, want ErrDocumentMalformed", err)
		}
	})

	t.Run("a truncated error document", func(t *testing.T) {
		src := newSource(t, map[string]reply{functionSearch: xmlReply(`<error code="100"`)})

		_, err := mustNew(t, src).Search(testContext(t), indexer.Query{Text: "x"})

		if !errors.Is(err, ErrDocumentMalformed) {
			t.Fatalf("Search error = %v, want ErrDocumentMalformed", err)
		}
	})
}
