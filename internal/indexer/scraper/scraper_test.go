package scraper

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/indexer"
	"github.com/kdta91/tortui/internal/indexer/httpx"
)

// keywordQuery is the query most tests run.
func keywordQuery() indexer.Query {
	return indexer.Query{Mode: indexer.ModeSearch, Text: "invented"}
}

// TestSearchMapsEveryResultField walks the checked-in page and asserts
// every field of the frozen indexer.Result the definition maps, on the row
// that carries all of them.
func TestSearchMapsEveryResultField(t *testing.T) {
	t.Parallel()

	src, a := archive(t)

	results, err := a.Search(testContext(t), keywordQuery())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != 4 {
		t.Fatalf("got %d results, want 4 (the page has four result rows, a header and a promo row)", len(results))
	}

	got := results[0]
	base := src.server.URL

	want := indexer.Result{
		IndexerID:  htmlID,
		ID:         "1001",
		Title:      "Invented Reference Corpus 2026",
		InfoHash:   "0123456789abcdef0123456789abcdef01234567",
		Magnet:     "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Invented+Reference+Corpus+2026",
		TorrentURL: base + "/dl/1001.torrent",
		SizeBytes:  1503238553,
		Seeders:    1204,
		Leechers:   37,
		Category:   indexer.CategoryData,
		Published:  time.Date(2026, time.March, 4, 11, 20, 0, 0, time.UTC),
		Uploader:   "avery",
		Trust:      indexer.TrustVIP,
		SourceURL:  base + "/item/1001",
	}

	assertResult(t, got, want)

	if got.Extra != nil {
		t.Errorf("Result.Extra = %v, want nil: this schema has no extra block", got.Extra)
	}

	if err := got.Validate(); err != nil {
		t.Errorf("the mapped result is not usable: %v", err)
	}
}

// TestAMissingOptionalSelectorYieldsAZeroValue pins the acceptance
// criterion: a selector that matches nothing is a zero value on one field,
// never an error and never a dropped result.
func TestAMissingOptionalSelectorYieldsAZeroValue(t *testing.T) {
	t.Parallel()

	_, a := archive(t)

	results, err := a.Search(testContext(t), keywordQuery())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	// The third row publishes a name and a magnet and nothing else: no
	// size the parser can read, a non-numeric seeder count, and empty
	// cells for leechers, category, date, uploader and badge.
	got := results[2]

	if got.Title == "" {
		t.Fatal("the row with only a name and a magnet was dropped; a missing optional field must not drop a row")
	}

	for name, check := range map[string]bool{
		"SizeBytes":  got.SizeBytes == 0,
		"Seeders":    got.Seeders == 0,
		"Leechers":   got.Leechers == 0,
		"Category":   got.Category == indexer.CategoryOther,
		"Published":  got.Published.IsZero(),
		"Uploader":   got.Uploader == "",
		"Trust":      got.Trust == indexer.TrustUnknown,
		"TorrentURL": got.TorrentURL == "",
	} {
		if !check {
			t.Errorf("%s is not its zero value on the sparse row: %+v", name, got)
		}
	}

	if got.Magnet == "" || got.InfoHash == "" {
		t.Errorf("the fields the sparse row does publish are missing: Magnet=%q InfoHash=%q", got.Magnet, got.InfoHash)
	}
}

// TestRowsWithNoTitleAreSkipped covers the header row and the promotional
// row the rows selector deliberately matches.
func TestRowsWithNoTitleAreSkipped(t *testing.T) {
	t.Parallel()

	_, a := archive(t)

	results, err := a.Search(testContext(t), keywordQuery())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	for _, res := range results {
		if strings.TrimSpace(res.Title) == "" {
			t.Fatalf("a titleless row reached the results: %+v", res)
		}

		if strings.Contains(res.Title, "promotional") {
			t.Fatalf("the promo row reached the results: %+v", res)
		}
	}
}

// TestAPageLinkThatIsNotHTTPIsRefused covers the hostile row: a details
// link with a javascript: scheme must not reach Result.SourceURL, which is
// a value tortui hands to the operating system's opener.
func TestAPageLinkThatIsNotHTTPIsRefused(t *testing.T) {
	t.Parallel()

	src, a := archive(t)

	results, err := a.Search(testContext(t), keywordQuery())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	got := results[3]

	if got.ID != "1004" {
		t.Fatalf("expected the hostile row at index 3, got %+v", got)
	}

	if got.SourceURL != "" {
		t.Errorf("SourceURL = %q, want empty: a javascript: link must be refused", got.SourceURL)
	}

	if got.Magnet != "" {
		t.Errorf("Magnet = %q, want empty: an href that is not a magnet URI is not a magnet", got.Magnet)
	}

	if got.Trust != indexer.TrustUnknown {
		t.Errorf("Trust = %v, want unknown: the badge value is not in the definition's mapping", got.Trust)
	}

	if !got.Published.IsZero() {
		t.Errorf("Published = %v, want the zero time: the date is unparseable", got.Published)
	}

	if want := "https://mirror.example.org/dl/1004.torrent"; got.TorrentURL != want {
		t.Errorf("TorrentURL = %q, want %q: an absolute link is kept as it stands", got.TorrentURL, want)
	}

	if len(src.requests()) != 1 {
		t.Errorf("one search made %d requests", len(src.requests()))
	}
}

// TestLatestUsesItsOwnBlockAndInheritsSharedFields is the latest-block
// inheritance criterion in both directions: the three fields the block
// overrides come from the block, and every other field comes from the
// shared set and still works against different markup.
func TestLatestUsesItsOwnBlockAndInheritsSharedFields(t *testing.T) {
	t.Parallel()

	src, a := archive(t)

	results, err := a.Search(testContext(t), indexer.Query{Mode: indexer.ModeLatest})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2 (the feed has three entries, one without a heading)", len(results))
	}

	if src.lastQuery(t, "/latest") == nil {
		t.Fatal("the latest block's own path was not requested")
	}

	got := results[0]
	base := src.server.URL

	// Overridden by the latest block: the heading markup the feed uses.
	if got.Title != "Invented Weather Station Dump, March" {
		t.Errorf("Title = %q: the latest block's own title selector was not used", got.Title)
	}

	if got.ID != "2001" || got.SourceURL != base+"/item/2001" {
		t.Errorf("ID = %q, SourceURL = %q: the latest block's own overrides were not used", got.ID, got.SourceURL)
	}

	// Inherited from the shared field set, and matching the feed's own
	// markup because the classes are the same.
	if got.SizeBytes != 536870912 {
		t.Errorf("SizeBytes = %d, want 536870912: the shared size selector was not inherited", got.SizeBytes)
	}

	for name, check := range map[string]bool{
		"Seeders":   got.Seeders == 88,
		"Category":  got.Category == indexer.CategoryData,
		"Uploader":  got.Uploader == "avery",
		"Trust":     got.Trust == indexer.TrustVIP,
		"Published": got.Published.Equal(time.Date(2026, time.March, 9, 8, 0, 0, 0, time.UTC)),
		"InfoHash":  got.InfoHash == "1111111111111111111111111111111111111111",
	} {
		if !check {
			t.Errorf("%s was not inherited from the shared field set: %+v", name, got)
		}
	}

	// Inherited and matching nothing on this page, which is a zero value
	// rather than an error.
	if got.Leechers != 0 || got.TorrentURL != "" {
		t.Errorf("Leechers = %d, TorrentURL = %q: the feed publishes neither", got.Leechers, got.TorrentURL)
	}

	// The second entry publishes only a heading, a size, a seeder count
	// and a magnet: every other inherited selector matches nothing.
	sparse := results[1]

	if sparse.Title != "Invented Typeface Family 1.2" || sparse.SizeBytes != 9437184 || sparse.Seeders != 4 {
		t.Errorf("the sparse feed entry mapped wrongly: %+v", sparse)
	}

	if sparse.Trust != indexer.TrustUnknown || sparse.Uploader != "" || !sparse.Published.IsZero() {
		t.Errorf("the sparse feed entry invented values for fields it does not publish: %+v", sparse)
	}
}

// TestCapsComeFromTheDefinition covers every Caps field for a definition
// that has a latest block and one that does not.
func TestCapsComeFromTheDefinition(t *testing.T) {
	t.Parallel()

	src := newSource(t, nil)

	full := mustAdapter(t, src, loadDefinition(t, htmlDefinitionFile))

	want := indexer.Caps{
		Search:         true,
		Latest:         true,
		Categories:     false,
		Pagination:     true,
		RequiresAuth:   false,
		ProvidesMagnet: true,
	}

	if got := full.Caps(); got != want {
		t.Errorf("Caps() = %+v, want %+v", got, want)
	}

	if full.ID() != htmlID || full.Name() != "Fixture Archive" {
		t.Errorf("ID() = %q, Name() = %q", full.ID(), full.Name())
	}

	only := mustAdapter(t, src, loadDefinition(t, searchOnlyDefinitionFile))

	wantOnly := indexer.Caps{
		Search:         true,
		Latest:         false,
		Categories:     false,
		Pagination:     false,
		RequiresAuth:   false,
		ProvidesMagnet: true,
	}

	if got := only.Caps(); got != wantOnly {
		t.Errorf("a definition with no latest block and no offset param: Caps() = %+v, want %+v", got, wantOnly)
	}
}

// TestRequiresAuthIsRaisedByEitherSide covers both the definition's own
// requires_auth and the option, and that neither lowers the other.
func TestRequiresAuthIsRaisedByEitherSide(t *testing.T) {
	t.Parallel()

	src := newSource(t, nil)

	fromOption, err := New(Options{
		Definition:   src.pointAt(loadDefinition(t, htmlDefinitionFile)),
		Client:       testClient(httpx.Config{}),
		RequiresAuth: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if !fromOption.Caps().RequiresAuth {
		t.Error("Options.RequiresAuth did not reach Caps")
	}

	def := src.pointAt(loadDefinition(t, htmlDefinitionFile))
	def.RequiresAuth = true

	fromDefinition, err := New(Options{Definition: def, Client: testClient(httpx.Config{})})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if !fromDefinition.Caps().RequiresAuth {
		t.Error("the definition's requires_auth did not reach Caps")
	}
}

// TestALatestQueryIsRefusedWithoutALatestBlock is the adapter's backstop
// for a caller that ignored Caps.Latest. The registry already skips such a
// source (AGENT.md §6.3).
func TestALatestQueryIsRefusedWithoutALatestBlock(t *testing.T) {
	t.Parallel()

	src := newSource(t, nil)
	a := mustAdapter(t, src, loadDefinition(t, searchOnlyDefinitionFile))

	_, err := a.Search(testContext(t), indexer.Query{Mode: indexer.ModeLatest})
	if !errors.Is(err, ErrLatestUnsupported) {
		t.Fatalf("Search(ModeLatest) error = %v, want ErrLatestUnsupported", err)
	}

	if len(src.requests()) != 0 {
		t.Errorf("a refused query still made %d requests", len(src.requests()))
	}
}

// TestAKeywordSearchWithNoKeywordIsRefused pins indexer.Query's own rule
// that an adapter must not turn an empty keyword into a browse request.
func TestAKeywordSearchWithNoKeywordIsRefused(t *testing.T) {
	t.Parallel()

	src, a := archive(t)

	for _, text := range []string{"", "   "} {
		_, err := a.Search(testContext(t), indexer.Query{Mode: indexer.ModeSearch, Text: text})
		if !errors.Is(err, ErrQueryTextEmpty) {
			t.Errorf("Search(text=%q) error = %v, want ErrQueryTextEmpty", text, err)
		}
	}

	if len(src.requests()) != 0 {
		t.Errorf("a refused query still made %d requests", len(src.requests()))
	}
}

// TestAnUnknownQueryModeIsRefused covers a Mode value neither constant
// names, which is what a future mode looks like to this adapter.
func TestAnUnknownQueryModeIsRefused(t *testing.T) {
	t.Parallel()

	_, a := archive(t)

	_, err := a.Search(testContext(t), indexer.Query{Mode: indexer.Mode(7), Text: "x"})
	if !errors.Is(err, ErrModeUnsupported) {
		t.Fatalf("error = %v, want ErrModeUnsupported", err)
	}
}

// TestTheRequestCarriesTheTemplatedParams covers the substitutions and the
// rule that a param whose placeholder had nothing to put there is dropped
// rather than sent empty.
func TestTheRequestCarriesTheTemplatedParams(t *testing.T) {
	t.Parallel()

	src, a := archive(t)

	if _, err := a.Search(testContext(t), indexer.Query{Text: "corpus 2026", Limit: 25}); err != nil {
		t.Fatalf("Search: %v", err)
	}

	query := src.lastQuery(t, "/search")

	if got := query.Get("q"); got != "corpus 2026" {
		t.Errorf("q = %q, want the keyword", got)
	}

	if got := query.Get("n"); got != "25" {
		t.Errorf("n = %q, want 25", got)
	}

	if _, sent := query["from"]; sent {
		t.Errorf("from was sent as %q with Offset unset; a param whose placeholder is empty is dropped", query.Get("from"))
	}

	if _, err := a.Search(testContext(t), indexer.Query{Text: "corpus", Offset: 40}); err != nil {
		t.Fatalf("Search: %v", err)
	}

	if got := src.lastQuery(t, "/search").Get("from"); got != "40" {
		t.Errorf("from = %q, want 40", got)
	}
}

// TestMinSeedersAndLimitAreAppliedLocally covers the two Query fields this
// schema cannot push to the source.
func TestMinSeedersAndLimitAreAppliedLocally(t *testing.T) {
	t.Parallel()

	_, a := archive(t)

	results, err := a.Search(testContext(t), indexer.Query{Text: "invented", MinSeeders: 40})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results with MinSeeders=40, want 2 (1204 and 42 seeders)", len(results))
	}

	for _, res := range results {
		if res.Seeders < 40 {
			t.Errorf("a result below MinSeeders survived: %+v", res)
		}
	}

	capped, err := a.Search(testContext(t), indexer.Query{Text: "invented", Limit: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(capped) != 1 {
		t.Fatalf("got %d results with Limit=1, want 1", len(capped))
	}
}

// TestJSONModeMapsEveryResultField covers the json response mode against
// the checked-in endpoint fixture.
func TestJSONModeMapsEveryResultField(t *testing.T) {
	t.Parallel()

	src, a := api(t)

	results, err := a.Search(testContext(t), keywordQuery())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2 (the endpoint returns three items, one without a name)", len(results))
	}

	base := src.server.URL

	want := indexer.Result{
		IndexerID: jsonID,
		ID:        "3001",
		Title:     "Invented Satellite Imagery Set A",
		InfoHash:  "4444444444444444444444444444444444444444",
		Magnet: "magnet:?xt=urn:btih:4444444444444444444444444444444444444444" +
			"&dn=Invented+Satellite+Imagery+Set+A",
		SizeBytes: 2147483648,
		Seeders:   61,
		Leechers:  5,
		Category:  indexer.CategoryImage,
		Published: time.Date(2026, time.January, 22, 9, 15, 0, 0, time.UTC),
		Uploader:  "morgan",
		Trust:     indexer.TrustVIP,
		SourceURL: base + "/item/3001",
	}

	assertResult(t, results[0], want)

	second := results[1]

	// A number that arrived as a JSON string, a missing nested key, a
	// missing object, and a torrent link instead of a magnet.
	if second.SizeBytes != 734003200 || second.Seeders != 7 || second.Leechers != 0 {
		t.Errorf("result 1 numbers: %+v", second)
	}

	if second.Magnet != "" || second.TorrentURL != base+"/dl/3002.torrent" {
		t.Errorf("result 1 links: Magnet=%q TorrentURL=%q", second.Magnet, second.TorrentURL)
	}

	if second.Trust != indexer.TrustUnknown || second.Uploader != "" {
		t.Errorf("result 1 invented values for keys the item does not carry: %+v", second)
	}
}

// TestARowsSelectorThatMatchesNothingIsNotAnError covers both modes: an
// empty result set is a valid outcome and is indistinguishable from a page
// with no results.
func TestARowsSelectorThatMatchesNothingIsNotAnError(t *testing.T) {
	t.Parallel()

	src := newSource(t, map[string]reply{
		"/search":     pageReply("<html><body><p>Nothing matched.</p></body></html>"),
		"/api/search": jsonReply(`{"page":{"other":[]}}`),
	})

	for _, name := range []string{htmlDefinitionFile, jsonDefinitionFile} {
		a := mustAdapter(t, src, loadDefinition(t, name))

		results, err := a.Search(testContext(t), keywordQuery())
		if err != nil {
			t.Fatalf("%s: Search: %v", name, err)
		}

		if len(results) != 0 {
			t.Errorf("%s: got %d results, want none", name, len(results))
		}
	}
}

// TestAFailingSourceIsReportedWithoutAURL covers the transport path: the
// error names the source and comes from httpx, which guarantees it carries
// no address.
func TestAFailingSourceIsReportedWithoutAURL(t *testing.T) {
	t.Parallel()

	src := newSource(t, map[string]reply{"/search": statusReply(http.StatusForbidden)})
	a := mustAdapter(t, src, loadDefinition(t, htmlDefinitionFile))

	_, err := a.Search(testContext(t), keywordQuery())
	if err == nil {
		t.Fatal("a 403 was not reported as an error")
	}

	var statusErr *httpx.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error = %v, want an *httpx.StatusError", err)
	}

	if !strings.HasPrefix(err.Error(), "scraper "+htmlID+": ") {
		t.Errorf("error = %q, want it to name the source first", err.Error())
	}
}

// TestSearchHonoursACancelledContext covers AGENT.md §6.2 from this
// adapter's side: the deadline is the caller's and cancelling it ends the
// call.
func TestSearchHonoursACancelledContext(t *testing.T) {
	t.Parallel()

	src, a := archive(t)

	ctx, cancel := contextCancelled(t)
	cancel()

	if _, err := a.Search(ctx, keywordQuery()); err == nil {
		t.Fatal("a cancelled context did not end the search")
	}

	if len(src.requests()) != 0 {
		t.Errorf("a cancelled search still reached the source %d times", len(src.requests()))
	}
}

// TestResolveIsANoOpWhenAlreadyResolved is the frozen contract's
// requirement.
func TestResolveIsANoOpWhenAlreadyResolved(t *testing.T) {
	t.Parallel()

	_, a := archive(t)

	before := indexer.Result{
		IndexerID: htmlID,
		ID:        "1001",
		Title:     "Invented Reference Corpus 2026",
		Magnet:    "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567",
	}

	after, err := a.Resolve(testContext(t), before)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	assertResult(t, after, before)
}

// TestResolveDerivesAMagnetFromAnInfohash covers the one thing Resolve
// computes, and that it makes no request while doing it.
func TestResolveDerivesAMagnetFromAnInfohash(t *testing.T) {
	t.Parallel()

	src, a := archive(t)

	before := indexer.Result{
		IndexerID: htmlID,
		ID:        "1002",
		Title:     "Invented Distro Image 42.1",
		InfoHash:  "89ABCDEF0123456789ABCDEF0123456789ABCDEF",
	}

	after, err := a.Resolve(testContext(t), before)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if after.InfoHash != strings.ToLower(before.InfoHash) {
		t.Errorf("InfoHash = %q, want it normalised to lowercase hex", after.InfoHash)
	}

	want := "magnet:?xt=urn:btih:" + strings.ToLower(before.InfoHash) + "&dn=Invented+Distro+Image+42.1"
	if after.Magnet != want {
		t.Errorf("Magnet = %q, want %q", after.Magnet, want)
	}

	if err := after.Validate(); err != nil {
		t.Errorf("the resolved result is not usable: %v", err)
	}

	if len(src.requests()) != 0 {
		t.Errorf("Resolve made %d requests; it resolves from the result it was handed", len(src.requests()))
	}
}

// TestResolveLeavesATorrentURLAloneAndRefusesNothing covers the remaining
// two branches.
func TestResolveLeavesATorrentURLAloneAndRefusesNothing(t *testing.T) {
	t.Parallel()

	_, a := archive(t)

	withFile := indexer.Result{IndexerID: htmlID, ID: "1004", Title: "Invented Hostile Row"}
	withFile.TorrentURL = "https://mirror.example.org/dl/1004.torrent"

	after, err := a.Resolve(testContext(t), withFile)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	assertResult(t, after, withFile)

	empty := indexer.Result{IndexerID: htmlID, ID: "x", Title: "Invented Nothing", InfoHash: "not-a-hash"}

	if _, err := a.Resolve(testContext(t), empty); !errors.Is(err, ErrUnresolvable) {
		t.Fatalf("error = %v, want ErrUnresolvable", err)
	}
}

// TestNewRefusesOptionsItCannotUse covers the two ways New fails.
func TestNewRefusesOptionsItCannotUse(t *testing.T) {
	t.Parallel()

	if _, err := New(Options{}); !errors.Is(err, ErrDefinitionEmpty) {
		t.Errorf("New with no definition: error = %v, want ErrDefinitionEmpty", err)
	}

	if _, err := New(Options{Definition: &Definition{ID: "x"}}); !errors.Is(err, ErrBaseAddressEmpty) {
		t.Errorf("New with an invalid definition: error = %v, want ErrBaseAddressEmpty", err)
	}
}

// TestNewWithoutAClientStillWorks covers the nil-Client default, which is
// enough for a source that needs no credentials.
func TestNewWithoutAClientStillWorks(t *testing.T) {
	t.Parallel()

	src := newSource(t, nil)

	a, err := New(Options{Definition: src.pointAt(loadDefinition(t, htmlDefinitionFile))})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if a.client == nil {
		t.Fatal("New left the client nil")
	}
}
