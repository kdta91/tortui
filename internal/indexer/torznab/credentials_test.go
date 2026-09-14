package torznab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kdta91/tortui/internal/indexer"
	"github.com/kdta91/tortui/internal/indexer/httpx"
	"github.com/kdta91/tortui/internal/logging"
)

// testKey is the value a user would have pasted in from their own account.
// It is the same string httpx's own leak sweep uses, and it is not a
// credential for anything.
const testKey = "opaque-value-from-the-users-own-account-0192837465"

// assertNoCredentialLeak is the assertion this file exists for.
//
// Text that reaches a user's log file or screen must contain neither the
// credential nor the query parameter it travels in, and no absolute URL at
// all: a Torznab request carries the api_key in its query string, so a URL
// is the vehicle, and its absence is the stronger property to assert.
func assertNoCredentialLeak(t *testing.T, what, text string) {
	t.Helper()

	if strings.Contains(text, testKey) {
		t.Fatalf("%s leaked the api key: %q", what, text)
	}

	if strings.Contains(strings.ToLower(text), httpx.DefaultAPIKeyParam+"=") {
		t.Fatalf("%s contains an %s= parameter: %q", what, httpx.DefaultAPIKeyParam, text)
	}

	if strings.Contains(text, "http://") || strings.Contains(text, "https://") {
		t.Fatalf("%s contains an absolute URL, which is how a key reaches a log line: %q", what, text)
	}
}

// credentialledSource is a source the adapter talks to with the user's
// api_key configured, answering with documents that echo the request — and
// therefore the key — back in every place a real server plausibly would.
func credentialledSource(t *testing.T, replies map[string]reply) (*fakeSource, Options) {
	t.Helper()

	src := newSource(t, replies)

	opts := src.options()
	opts.Client = testClient(httpx.Config{
		Credentials: httpx.Credentials{APIKey: testKey},
	})

	return src, opts
}

// echoingErrorDocument is what a server that pastes the request into its
// error description produces. Jackett puts a whole .NET exception in there
// and Prowlarr puts ex.Message, so this is the realistic hostile case rather
// than a contrived one.
func echoingErrorDocument() string {
	return fmt.Sprintf(
		`<error code="201" description="Incorrect parameter in request t=search&amp;%s=%s"/>`,
		httpx.DefaultAPIKeyParam, testKey,
	)
}

// echoingFeed is a feed whose every link carries the key, which is what a
// real Torznab feed looks like: the download URL has to carry it.
func echoingFeed() string {
	base := "https://feed.example.org"
	download := base + "/dl/1?" + httpx.DefaultAPIKeyParam + "=" + testKey

	return `<rss version="2.0"><channel><item>` +
		`<title>Invented Release</title>` +
		`<guid isPermaLink="true">` + download + `</guid>` +
		`<comments>` + base + `/details/1?` + httpx.DefaultAPIKeyParam + `=` + testKey + `</comments>` +
		`<link>` + download + `</link>` +
		`<enclosure url="` + download + `" length="10" type="application/x-bittorrent"/>` +
		`<attr name="seeders" value="1"/>` +
		`<attr name="uploader" value="` + base + `/u/` + testKey + `"/>` +
		`<attr name="grabs" value="` + testKey + `"/>` +
		`</item></channel></rss>`
}

// probeHash is a made-up infohash, used so the magnet in
// echoingEverythingFeed is a well-formed one.
const probeHash = "0123456789abcdef0123456789abcdef01234567"

// echoingEverythingFeed is echoingFeed turned fully hostile: the same
// credential-bearing links, plus the key echoed into the two places the
// adapter passes through verbatim and therefore cannot clean — the <title>
// and a magneturl's dn= parameter.
//
// It exists because echoingFeed does neither, which made
// TestOnlyTheURLNamedResultFieldsCarryTheCredential's assertion vacuous for
// exactly the two fields that can carry a credential (found by QA on
// PR #12). The two are asserted here as *expected* to carry it, which is
// what the code actually does and what DEC-071 records.
func echoingEverythingFeed() string {
	base := "https://feed.example.org"
	download := base + "/dl/1?" + httpx.DefaultAPIKeyParam + "=" + testKey
	magnet := magnetScheme + "?xt=" + btihPrefix + probeHash + "&amp;dn=" + testKey

	return `<rss version="2.0"><channel><item>` +
		`<title>Invented Release ` + testKey + `</title>` +
		`<guid isPermaLink="true">` + download + `</guid>` +
		`<comments>` + base + `/details/1?` + httpx.DefaultAPIKeyParam + `=` + testKey + `</comments>` +
		`<link>` + download + `</link>` +
		`<enclosure url="` + download + `" length="10" type="application/x-bittorrent"/>` +
		`<attr name="magneturl" value="` + magnet + `"/>` +
		`<attr name="seeders" value="1"/>` +
		`<attr name="uploader" value="` + base + `/u/` + testKey + `"/>` +
		`<attr name="grabs" value="` + testKey + `"/>` +
		`</item></channel></rss>`
}

// titleOnlyEchoingFeed is an item that published no infohash, no magnet, no
// guid, no comments and no link — so resultID has nothing left but the
// title, which here carries the key.
func titleOnlyEchoingFeed() string {
	return `<rss version="2.0"><channel><item>` +
		`<title>Invented Release ` + testKey + `</title>` +
		`<attr name="seeders" value="1"/>` +
		`</item></channel></rss>`
}

// leakCases builds one error per failure mode the adapter can produce, each
// from an adapter configured with the user's credentials.
func leakCases(t *testing.T) map[string]error {
	t.Helper()

	ctx := testContext(t)
	errs := make(map[string]error)

	fail := func(name string, replies map[string]reply, run func(*Adapter) error) {
		t.Helper()

		_, opts := credentialledSource(t, replies)

		a, err := New(opts)
		if err != nil {
			t.Fatalf("%s: New: %v", name, err)
		}

		if err := run(a); err != nil {
			errs[name] = err

			return
		}

		t.Fatalf("%s: the call unexpectedly succeeded", name)
	}

	search := func(a *Adapter) error {
		_, err := a.Search(ctx, indexer.Query{Text: "invented"})

		return err
	}

	fail("http 404", map[string]reply{}, search)
	fail("http 500", map[string]reply{functionSearch: statusReply(http.StatusInternalServerError)}, search)
	fail("http 401", map[string]reply{functionSearch: statusReply(http.StatusUnauthorized)}, search)
	fail("an api error echoing the request", map[string]reply{functionSearch: xmlReply(echoingErrorDocument())}, search)
	fail("malformed xml", map[string]reply{functionSearch: fixtureReply(t, "search-truncated.xml")}, search)
	fail("an html page", map[string]reply{functionSearch: fixtureReply(t, "search-html.xml")}, search)
	fail("an empty body", map[string]reply{functionSearch: xmlReply("")}, search)
	fail("a root element named after the key",
		map[string]reply{functionSearch: xmlReply("<" + testKey + "/>")}, search)
	fail("an entity named after the key",
		map[string]reply{functionSearch: xmlReply("<rss><channel><item><title>&" + testKey + ";</title></item></channel></rss>")},
		search)
	fail("an empty keyword", map[string]reply{}, func(a *Adapter) error {
		_, err := a.Search(ctx, indexer.Query{Mode: indexer.ModeSearch})

		return err
	})
	fail("a latest query the source cannot serve", map[string]reply{}, func(a *Adapter) error {
		_, err := a.Search(ctx, indexer.Query{Mode: indexer.ModeLatest})

		return err
	})
	fail("an unresolvable result", map[string]reply{}, func(a *Adapter) error {
		_, err := a.Resolve(ctx, indexer.Result{ID: "https://feed.example.org/dl/1?apikey=" + testKey})

		return err
	})
	fail("a cancelled search", map[string]reply{functionSearch: fixtureReply(t, "search-full.xml")}, func(a *Adapter) error {
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := a.Search(cancelled, indexer.Query{Text: "invented"})

		return err
	})

	// A caps probe that fails, which is the one error Discover returns.
	src, opts := credentialledSource(t, map[string]reply{})
	if _, err := Discover(ctx, opts); err != nil {
		errs["a failed caps probe"] = err
	} else {
		t.Fatal("the caps probe unexpectedly succeeded")
	}

	_ = src

	// An endpoint the user typed wrongly, with their key already in it.
	badOpts := Options{ID: testID}
	badOpts.Endpoint = "https://feed.example.org/api?" + httpx.DefaultAPIKeyParam + "=" + testKey + "\x7f"

	if _, err := New(badOpts); err != nil {
		errs["an unparseable endpoint carrying the key"] = err
	} else {
		t.Fatal("an endpoint with a control character in it was accepted")
	}

	return errs
}

func TestNoErrorFromThisAdapterCarriesTheCredential(t *testing.T) {
	t.Parallel()

	cases := leakCases(t)
	if len(cases) < 14 {
		t.Fatalf("the sweep built only %d cases, which is fewer than this file writes", len(cases))
	}

	for name, err := range cases {
		if err == nil {
			t.Fatalf("%s: no error produced", name)
		}

		assertNoCredentialLeak(t, name+" error text", err.Error())
		assertNoCredentialLeak(t, name+" formatted with %v", fmt.Sprintf("%v", err))
		assertNoCredentialLeak(t, name+" formatted with %+v", fmt.Sprintf("%+v", err))

		// A caller that walks the chain and prints a cause must not find a
		// URL down there either.
		for cause := errors.Unwrap(err); cause != nil; cause = errors.Unwrap(cause) {
			assertNoCredentialLeak(t, name+" unwrapped cause", cause.Error())
		}
	}
}

// searchOnce runs one search against a source answering with body, and
// returns the single result it produced.
func searchOnce(t *testing.T, body string) (indexer.Result, *fakeSource) {
	t.Helper()

	src, opts := credentialledSource(t, map[string]reply{functionSearch: xmlReply(body)})

	a, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	results, err := a.Search(testContext(t), indexer.Query{Text: "invented"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("parsed %d results, want 1", len(results))
	}

	return results[0], src
}

// TestWhichResultFieldsCanCarryTheCredential draws the real boundary, in
// both directions, against a source that echoes the user's api_key into
// every field it can reach.
//
// It replaced TestOnlyTheURLNamedResultFieldsCarryTheCredential, whose name
// stated something untrue and whose assertion was vacuous where it mattered:
// it forbade the key in Title and Magnet while feeding the adapter a
// document that put the key in neither (QA, PR #12). Both directions are
// asserted here, and the "does carry it" half is the load-bearing one — it
// goes red if the adapter ever starts scrubbing either field, which is what
// makes the "does not carry it" half mean something.
//
//   - TorrentURL and SourceURL carry the key and are safe doing so: their
//     names are on internal/logging's masked-key list.
//   - Title and Magnet carry the key and are NOT safe: they are the
//     source's own text verbatim, by necessity, under names that package
//     does not mask. This is the residual gap in DEC-071, whose structural
//     fix is backlog T-934.
//   - Everything else the adapter derives — ID (given any other identity),
//     InfoHash, Uploader, Extra — is clean, and that is this adapter's own
//     work rather than a property of the protocol.
func TestWhichResultFieldsCanCarryTheCredential(t *testing.T) {
	t.Parallel()

	r, src := searchOnce(t, echoingEverythingFeed())

	// Fields that must carry the key exactly as the source published it.
	// Two are safe because internal/logging masks on their names; two are
	// the disclosed gap. Either way, an adapter that quietly altered them
	// would be a different (and, for Magnet, a broken) adapter.
	carries := map[string]struct {
		value  string
		masked bool
	}{
		"TorrentURL": {r.TorrentURL, true},
		"SourceURL":  {r.SourceURL, true},
		"Title":      {r.Title, false},
		"Magnet":     {r.Magnet, false},
	}

	for field, want := range carries {
		if strings.Contains(want.value, testKey) {
			continue
		}

		if want.masked {
			t.Errorf("Result.%s = %q, want the address as published, key and all", field, want.value)

			continue
		}

		t.Errorf(
			"Result.%s = %q no longer carries the source's own text verbatim; if that is deliberate, "+
				"the package doc, DEC-071 and T-934 all describe the old behaviour and must be updated with it",
			field, want.value,
		)
	}

	assertDerivedFieldsAreClean(t, r)

	// ID is derived here even though the title carries the key, because
	// the item published an identity the adapter prefers to the title.
	if r.ID != probeHash {
		t.Errorf("Result.ID = %q, want the infohash %q", r.ID, probeHash)
	}

	if len(src.requests()) != 1 {
		t.Fatalf("Search made %d requests, want 1", len(src.requests()))
	}

	// The same sweep against an item whose only identity is a
	// credential-bearing guid, so ID's URL-stripping is exercised rather
	// than short-circuited by the infohash above. Dropping this case is
	// what would make the ID half of the assertion vacuous in the other
	// direction.
	viaGUID, _ := searchOnce(t, echoingFeed())

	assertDerivedFieldsAreClean(t, viaGUID)

	if want := "https://feed.example.org/dl/1"; viaGUID.ID != want {
		t.Errorf("Result.ID = %q, want the guid reduced to %q", viaGUID.ID, want)
	}

	if !strings.Contains(viaGUID.TorrentURL, testKey) || !strings.Contains(viaGUID.SourceURL, testKey) {
		t.Errorf(
			"TorrentURL = %q and SourceURL = %q, want both as published: an item whose links stopped "+
				"carrying the key would make every assertion above vacuous",
			viaGUID.TorrentURL, viaGUID.SourceURL,
		)
	}
}

// assertDerivedFieldsAreClean checks every Result field this adapter derives
// rather than passes through. None of these names is on internal/logging's
// list, so a credential in any of them would reach the log file in
// plaintext — which is precisely why the adapter constrains each one.
func assertDerivedFieldsAreClean(t *testing.T, r indexer.Result) {
	t.Helper()

	derived := map[string]string{
		"ID":        r.ID,
		"InfoHash":  r.InfoHash,
		"Uploader":  r.Uploader,
		"IndexerID": r.IndexerID,
	}

	for field, value := range derived {
		if strings.Contains(value, testKey) {
			t.Errorf("Result.%s = %q carries the api key and is not a field internal/logging masks", field, value)
		}
	}

	for key, value := range r.Extra {
		if strings.Contains(key, testKey) || strings.Contains(value, testKey) {
			t.Errorf("Result.Extra[%q] = %q carries the api key", key, value)
		}
	}

	if _, present := r.Extra["torznab."+attrGrabs]; present {
		t.Error("a non-numeric grabs value reached Extra; the numeric whitelist is what keeps a URL out of it")
	}
}

// TestResultIDFallsBackToTheTitleAndInheritsItsGap pins the one branch on
// which a *derived* field carries source text verbatim: an item with no
// infohash, no magnet, no guid, no comments and no link leaves resultID
// nothing but the title.
//
// It is asserted rather than fixed. Dropping the fallback would leave such
// an item with no identity at all and would remove a duplicate of text the
// same Result already carries in Title, so it buys no safety; see DEC-071
// and resultID's own doc comment. This test is what keeps that statement
// true: it goes red if the fallback is removed or if the title stops
// reaching ID.
func TestResultIDFallsBackToTheTitleAndInheritsItsGap(t *testing.T) {
	t.Parallel()

	r, _ := searchOnce(t, titleOnlyEchoingFeed())

	if r.ID != r.Title {
		t.Fatalf("Result.ID = %q, want the title %q: the fallback is the documented behaviour", r.ID, r.Title)
	}

	if !strings.Contains(r.ID, testKey) {
		t.Fatalf(
			"Result.ID = %q no longer carries what the source put in the title; if that is deliberate, "+
				"the package doc, resultID's comment and DEC-071 describe the old behaviour and must be updated",
			r.ID,
		)
	}
}

// TestNoCredentialReachesTheLogFile drives everything this adapter produces
// through the real internal/logging sink and greps the file.
//
// Its scope is exactly what this adapter controls, and no more: every error
// it can return, and a Result whose credential-bearing text is confined to
// the fields the adapter derives or names for masking. It deliberately uses
// echoingFeed rather than echoingEverythingFeed, because a Result whose
// Title or Magnet carries a key does put that key in the log file — proven
// by QA on PR #12, disclosed in the package doc and DEC-071, and left to
// backlog T-934, which is the only place it can be fixed since it is a
// property of the frozen §5 Result type. Asserting the leak here instead
// would pin a defect the moment T-934 lands; asserting the maskable surface
// is what this package can actually promise.
func TestNoCredentialReachesTheLogFile(t *testing.T) {
	// Not parallel: logging.New installs the process-wide slog default.
	path := filepath.Join(t.TempDir(), "tortui.log")

	logger, closer, err := logging.New(logging.Options{File: path, Level: "debug"})
	if err != nil {
		t.Fatalf("build logger: %v", err)
	}

	// Every error this adapter can produce, logged the ways an adapter's
	// caller plausibly logs one.
	for name, reqErr := range leakCases(t) {
		logger.Error("indexer search failed", "error", reqErr)
		logger.Error("indexer search failed", "err", reqErr)
		logger.Info("indexer detail", "detail", reqErr.Error())
		logger.Debug("indexer case", "case", name)
	}

	// And a whole Result parsed out of a feed whose links carry the key,
	// which is the other thing a caller holds.
	src, opts := credentialledSource(t, map[string]reply{functionSearch: xmlReply(echoingFeed())})

	a, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	results, err := a.Search(testContext(t), indexer.Query{Text: "invented"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	logger.Info("indexer result", "result", results[0])
	logger.Info("indexer results", "results", results)

	if err := closer.Close(); err != nil {
		t.Fatalf("close log: %v", err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	if len(contents) == 0 {
		t.Fatal("the log file is empty, so this test proves nothing")
	}

	if !strings.Contains(string(contents), "indexer search failed") {
		t.Fatalf("the log file does not contain the lines under test:\n%s", contents)
	}

	if !strings.Contains(string(contents), "indexer result") {
		t.Fatalf("the log file does not contain the logged result:\n%s", contents)
	}

	assertNoCredentialLeak(t, "the log file", string(contents))

	if len(src.requests()) == 0 {
		t.Fatal("the feed was never fetched")
	}
}
