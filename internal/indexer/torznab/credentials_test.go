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

func TestOnlyTheURLNamedResultFieldsCarryTheCredential(t *testing.T) {
	t.Parallel()

	src, opts := credentialledSource(t, map[string]reply{functionSearch: xmlReply(echoingFeed())})

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

	r := results[0]

	// The two fields that legitimately hold a credential-bearing URL. Both
	// are named for internal/logging's key-name rule, which is what makes
	// them safe to hold one, and the download URL has to carry the key or
	// the file cannot be fetched.
	if !strings.Contains(r.TorrentURL, testKey) {
		t.Errorf("TorrentURL = %q, want the download address as published, key and all", r.TorrentURL)
	}

	if !strings.Contains(r.SourceURL, testKey) {
		t.Errorf("SourceURL = %q, want the page address as published", r.SourceURL)
	}

	// Everything else. None of these names is on internal/logging's list,
	// so a credential in any of them would reach the log file in plaintext.
	unmasked := map[string]string{
		"ID":        r.ID,
		"Title":     r.Title,
		"InfoHash":  r.InfoHash,
		"Magnet":    r.Magnet,
		"Uploader":  r.Uploader,
		"IndexerID": r.IndexerID,
	}

	for field, value := range unmasked {
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

	if len(src.requests()) != 1 {
		t.Fatalf("Search made %d requests, want 1", len(src.requests()))
	}
}

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
