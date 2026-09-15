package scraper

import (
	"context"
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
// It is the same string httpx's and torznab's leak sweeps use, and it is
// not a credential for anything.
const testKey = "opaque-value-from-the-users-own-account-0192837465"

// testCookie is the other credential httpx injects, as a user would have
// copied it out of their own logged-in browser session.
const testCookie = "session=opaque-cookie-from-the-users-own-browser-5647382910"

// assertNoCredentialLeak is the assertion this file exists for.
//
// Text that reaches a user's log file or screen must contain neither
// credential, nor the query parameter the api key travels in, and no
// absolute URL at all: every request this adapter makes carries the key in
// its query string, so a URL is the vehicle, and its absence is the
// stronger property to assert.
func assertNoCredentialLeak(t *testing.T, what, text string) {
	t.Helper()

	if strings.Contains(text, testKey) {
		t.Fatalf("%s leaked the api key: %q", what, text)
	}

	if strings.Contains(text, testCookie) {
		t.Fatalf("%s leaked the cookie: %q", what, text)
	}

	if strings.Contains(strings.ToLower(text), httpx.DefaultAPIKeyParam+"=") {
		t.Fatalf("%s contains an %s= parameter: %q", what, httpx.DefaultAPIKeyParam, text)
	}

	if strings.Contains(text, "http://") || strings.Contains(text, "https://") {
		t.Fatalf("%s contains an absolute URL, which is how a key reaches a log line: %q", what, text)
	}
}

// credentialledClient is a client carrying both of the credentials a user
// can configure.
func credentialledClient() *httpx.Client {
	return testClient(httpx.Config{
		Credentials: httpx.Credentials{APIKey: testKey, CookieHeader: testCookie},
	})
}

// definitionCarryingTheKey is the other half of the hazard: a definition in
// which the user hardcoded their own credential into a param value, which
// nothing stops them doing and which the schema's own placeholder rules
// discourage but cannot prevent.
func definitionCarryingTheKey(t *testing.T, src *fakeSource) *Definition {
	t.Helper()

	def := src.pointAt(loadDefinition(t, htmlDefinitionFile))
	def.Search.Params["token"] = testKey

	return def
}

// leakCases returns every error this adapter can produce, built so that the
// credential is present wherever it plausibly could be: in the client's
// configuration, in a param value in the definition, and in the query
// string of the base address.
func leakCases(t *testing.T) map[string]error {
	t.Helper()

	out := make(map[string]error)

	record := func(name string, err error) {
		t.Helper()

		if err == nil {
			t.Fatalf("%s: expected an error and got none", name)
		}

		out[name] = err
	}

	// Validation failures, with the credential written into the values a
	// validation error is closest to.
	withKey := func(source string) string {
		return strings.NewReplacer(
			"https://feed.example.org", "https://feed.example.org/?"+httpx.DefaultAPIKeyParam+"="+testKey,
			"  path: /s", "  path: /s\n  params:\n    token: \""+testKey+"\"",
		).Replace(source)
	}

	validationCases := map[string]string{
		"bad base address": strings.Replace(minimalDefinition,
			"https://feed.example.org",
			"http://feed.example.org:not-a-port/?"+httpx.DefaultAPIKeyParam+"="+testKey, 1),
		"unsupported scheme": strings.Replace(minimalDefinition,
			"https://feed.example.org", "ftp://feed.example.org/?"+httpx.DefaultAPIKeyParam+"="+testKey, 1),
		"no host": strings.Replace(minimalDefinition,
			"https://feed.example.org", "https:///a?"+httpx.DefaultAPIKeyParam+"="+testKey, 1),
		"bad selector":        withKey(strings.Replace(minimalDefinition, "rows: tr", `rows: "tr["`, 1)),
		"bad regex":           withKey(strings.Replace(minimalDefinition, "    selector: a\n  magnet:", "    selector: a\n    regex: \"([\"\n  magnet:", 1)),
		"unknown transform":   withKey(strings.Replace(minimalDefinition, "    selector: a\n  magnet:", "    selector: a\n    transform: [shout]\n  magnet:", 1)),
		"unknown placeholder": strings.Replace(minimalDefinition, "  path: /s", "  path: /s\n  params:\n    token: \"{{"+testKey+"}}\"", 1),
		"unterminated placeholder": strings.Replace(minimalDefinition, "  path: /s",
			"  path: /s\n  params:\n    token: \"{{"+testKey+"\"", 1),
		"malformed yaml": strings.Replace(minimalDefinition, "  path: /s",
			"  path: /s\n  params:\n\ttoken: \""+testKey+"\"", 1),
		"unknown key": minimalDefinition + "credentials: \"" + testKey + "\"\n",
		"no title":    withKey(strings.Replace(minimalDefinition, "  title:\n    selector: a\n", "", 1)),
	}

	for name, source := range validationCases {
		_, err := Parse([]byte(source))
		record("definition: "+name, err)
	}

	// Failures that need a live source, all of them with the credential
	// configured on the client and hardcoded into a param.
	src := newSource(t, map[string]reply{
		"/search":  statusReply(http.StatusUnauthorized),
		"/deep":    pageReply(nestedDivs(100000)),
		"/broken":  jsonReply(`{"page":{"items":`),
		"/notlist": jsonReply(`{"page":{"items":"nope"}}`),
	})

	a, err := New(Options{Definition: definitionCarryingTheKey(t, src), Client: credentialledClient()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = a.Search(testContext(t), keywordQuery())
	record("an unauthorised source", err)

	_, err = a.Search(testContext(t), indexer.Query{Mode: indexer.ModeSearch})
	record("a keyword search with no keyword", err)

	_, err = a.Search(testContext(t), indexer.Query{Mode: indexer.Mode(9)})
	record("an unknown mode", err)

	_, err = a.Resolve(testContext(t), indexer.Result{
		IndexerID: htmlID,
		ID:        src.server.URL + "/dl/1?" + httpx.DefaultAPIKeyParam + "=" + testKey,
		Title:     "Invented Release " + testKey,
	})
	record("an unresolvable result", err)

	// A definition with no latest block, so the latest refusal is
	// reachable.
	only, err := New(Options{
		Definition: src.pointAt(loadDefinition(t, searchOnlyDefinitionFile)),
		Client:     credentialledClient(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = only.Search(testContext(t), indexer.Query{Mode: indexer.ModeLatest})
	record("a latest query with no latest block", err)

	// Response-shaped failures, each pointed at its own path.
	for name, path := range map[string]string{
		"a document nested too deep":     "/deep",
		"a truncated json body":          "/broken",
		"a rows path that is not a list": "/notlist",
	} {
		def := definitionCarryingTheKey(t, src)
		def.Search.Path = path

		if strings.Contains(path, "json") || path == "/broken" || path == "/notlist" {
			def.Mode = ModeJSON
			def.Rows = "page.items"
			def.Fields = map[string]Field{
				fieldTitle:  {Selector: "name"},
				fieldMagnet: {Selector: "magnet"},
			}
			def.Trust = nil
			def.Latest = nil
		}

		probe, err := New(Options{Definition: def, Client: credentialledClient()})
		if err != nil {
			t.Fatalf("New for %s: %v", name, err)
		}

		_, err = probe.Search(testContext(t), keywordQuery())
		record(name, err)
	}

	// A context that is already done, which is the one error that comes
	// from the transport rather than from a status.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = a.Search(cancelled, keywordQuery())
	record("a cancelled search", err)

	return out
}

// TestNoErrorFromThisAdapterCarriesTheCredential sweeps every failure mode
// and asserts none of them names the user's credential, the parameter it
// travels in, or any absolute address.
func TestNoErrorFromThisAdapterCarriesTheCredential(t *testing.T) {
	t.Parallel()

	cases := leakCases(t)

	if len(cases) < 18 {
		t.Fatalf("the sweep only produced %d failures; it is supposed to cover every one this adapter can return", len(cases))
	}

	for name, err := range cases {
		assertNoCredentialLeak(t, "the error from "+name, err.Error())

		// Unwrapping is how a caller renders a cause on its own, so the
		// whole chain has to hold, not just the outermost message.
		for cause := err; cause != nil; {
			assertNoCredentialLeak(t, "an unwrapped cause of "+name, cause.Error())

			unwrapped, ok := cause.(interface{ Unwrap() error })
			if !ok {
				break
			}

			cause = unwrapped.Unwrap()
		}
	}
}

// echoingPage builds a results page in the shape the checked-in definition
// reads, with the credential echoed back wherever the caller asks for it.
//
// The link parameters always carry the key, because that is what a real
// download link on a credentialled source looks like: it has to carry it or
// the file cannot be fetched.
func echoingPage(inTitle, inMagnet, inUploader, inID bool) string {
	title := "Invented Echoing Release"
	if inTitle {
		title += " " + testKey
	}

	magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"
	if inMagnet {
		magnet += "&amp;dn=" + testKey
	}

	uploader := "avery"
	if inUploader {
		uploader = testKey
	}

	id := "1001"
	if inID {
		id = testKey
	}

	query := "?" + httpx.DefaultAPIKeyParam + "=" + testKey

	return `<html><body><table class="results">` +
		`<tr class="row">` +
		`<td class="name"><a href="/item/1001` + query + `" data-item="` + id + `">` + title + `</a></td>` +
		`<td class="size">1 GiB</td>` +
		`<td class="seeders">5</td>` +
		`<td class="leechers">1</td>` +
		`<td class="kind">Data</td>` +
		`<td class="added" datetime="2026-03-04 11:20:00">4 March 2026</td>` +
		`<td class="by">` + uploader + ` <span class="badge" data-trust="gold">VIP</span></td>` +
		`<td class="links">` +
		// The infohash attribute carries the credential in every
		// variant, not a hash. A hostile source controls that attribute
		// as much as any other, and putting a real hash there made
		// assertDerivedFieldsAreClean's InfoHash assertion vacuous:
		// normaliseInfoHash could have been removed entirely and the
		// test would have stayed green (caught by mutation before this
		// branch was pushed). With the key there, the derivation is what
		// keeps the field clean, and removing it turns the test red.
		`<a class="magnet" data-infohash="` + testKey + `" href="` + magnet + `">magnet</a>` +
		`<a class="torrent" href="/dl/1001.torrent` + query + `">torrent</a>` +
		`</td></tr></table></body></html>`
}

// searchEchoing runs one search against a page built by echoingPage.
func searchEchoing(t *testing.T, page string) indexer.Result {
	t.Helper()

	src := newSource(t, map[string]reply{"/search": pageReply(page)})

	a, err := New(Options{Definition: definitionCarryingTheKey(t, src), Client: credentialledClient()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	results, err := a.Search(testContext(t), keywordQuery())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	return results[0]
}

// assertDerivedFieldsAreClean checks every Result field this adapter
// derives rather than passes through. These are the fields the package doc
// claims no source text can reach, and the claim is only worth anything if
// something goes red when it stops being true.
func assertDerivedFieldsAreClean(t *testing.T, res indexer.Result) {
	t.Helper()

	assertNoCredentialLeak(t, "Result.IndexerID", res.IndexerID)
	assertNoCredentialLeak(t, "Result.InfoHash", res.InfoHash)

	// The page offers the credential as the infohash attribute and a real
	// hash only inside the magnet's xt, so InfoHash being right is the
	// derivation working rather than the page being harmless.
	if res.InfoHash != "0123456789abcdef0123456789abcdef01234567" {
		t.Errorf("Result.InfoHash = %q, want the hash out of the magnet's xt", res.InfoHash)
	}

	if res.Extra != nil {
		t.Errorf("Result.Extra = %v, want nil: this schema never sets it", res.Extra)
	}

	if res.Category != indexer.CategoryData {
		t.Errorf("Result.Category = %v: it is an enum and cannot carry text", res.Category)
	}

	if res.Trust != indexer.TrustVIP {
		t.Errorf("Result.Trust = %v: it is an enum and cannot carry text", res.Trust)
	}

	if res.SizeBytes != 1<<30 || res.Seeders != 5 || res.Leechers != 1 {
		t.Errorf("the parsed numbers are wrong: %+v", res)
	}
}

// TestWhichResultFieldsCanCarryTheCredential asserts both directions of the
// boundary the package doc documents.
//
// The derived fields must never carry the source's text. Title, Magnet,
// Uploader and the non-infohash branches of ID **do** carry it, verbatim,
// and that is asserted rather than fixed: it is a property of the frozen §5
// indexer.Result rather than of this adapter (backlog T-934, DEC-071), and
// asserting it means a future change to any of the four forces this test,
// the package doc and that row to be revisited together.
func TestWhichResultFieldsCanCarryTheCredential(t *testing.T) {
	t.Parallel()

	t.Run("links only", func(t *testing.T) {
		t.Parallel()

		res := searchEchoing(t, echoingPage(false, false, false, false))

		assertDerivedFieldsAreClean(t, res)

		for name, value := range map[string]string{
			"Result.Title":    res.Title,
			"Result.Magnet":   res.Magnet,
			"Result.Uploader": res.Uploader,
			"Result.ID":       res.ID,
		} {
			assertNoCredentialLeak(t, name+" on a page that echoes nothing into it", value)
		}

		// The two fields that do carry the key, by necessity, and that
		// internal/logging masks on their names.
		if !strings.Contains(res.SourceURL, testKey) || !strings.Contains(res.TorrentURL, testKey) {
			t.Fatalf("the fixture does not put the key in the link query strings, so this test proves nothing: %+v", res)
		}
	})

	t.Run("echoed into every field that passes text through", func(t *testing.T) {
		t.Parallel()

		res := searchEchoing(t, echoingPage(true, true, true, true))

		assertDerivedFieldsAreClean(t, res)

		for name, value := range map[string]string{
			"Result.Title":    res.Title,
			"Result.Magnet":   res.Magnet,
			"Result.Uploader": res.Uploader,
			"Result.ID":       res.ID,
		} {
			if !strings.Contains(value, testKey) {
				t.Errorf(
					"%s = %q no longer carries what the page sent. If that is deliberate it is an "+
						"improvement, but the package doc's field list, the T-022 tracker row and DEC-071 "+
						"all describe the old behaviour and must be updated with it",
					name, value,
				)
			}
		}
	})
}

// TestResolveDerivesAMagnetThatInheritsTheTitlesGap pins the second-order
// case: a row that published no magnet gets one built from its title, so
// the derived magnet is no cleaner than the title was.
func TestResolveDerivesAMagnetThatInheritsTheTitlesGap(t *testing.T) {
	t.Parallel()

	src := newSource(t, nil)

	a, err := New(Options{Definition: definitionCarryingTheKey(t, src), Client: credentialledClient()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	before := indexer.Result{
		IndexerID: htmlID,
		ID:        "1001",
		InfoHash:  "0123456789abcdef0123456789abcdef01234567",
		Title:     "Invented Echoing Release " + testKey,
	}

	if before.Magnet != "" {
		t.Fatal("the input already carries a magnet, so Resolve is a no-op and this test proves nothing")
	}

	after, err := a.Resolve(testContext(t), before)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if !strings.Contains(after.Magnet, testKey) {
		t.Errorf(
			"Result.Magnet = %q no longer carries what the page put in the title; the package doc's "+
				"Magnet entry, magnetFor's comment, the T-022 row and DEC-071 all describe the old "+
				"behaviour and must be updated with it",
			after.Magnet,
		)
	}
}

// TestNoCredentialReachesTheLogFile drives everything this adapter produces
// through the real internal/logging sink and greps the file.
//
// Its scope is exactly what this adapter controls: every error it can
// return, and a Result whose credential-bearing text is confined to the
// fields the adapter derives or names for masking. It deliberately uses the
// page that echoes the key into the links only, because a Result whose
// Title, Magnet, Uploader or ID carries the key does put it in the log file
// — the disclosed gap above, left to backlog T-934, which is the only place
// it can be fixed since it is a property of the frozen §5 type. Asserting
// the leak here instead would pin a defect the moment T-934 lands.
func TestNoCredentialReachesTheLogFile(t *testing.T) {
	// Not parallel: logging.New installs the process-wide slog default.
	path := filepath.Join(t.TempDir(), "tortui.log")

	logger, closer, err := logging.New(logging.Options{File: path, Level: "debug"})
	if err != nil {
		t.Fatalf("build logger: %v", err)
	}

	for name, reqErr := range leakCases(t) {
		logger.Error("indexer search failed", "error", reqErr)
		logger.Error("indexer search failed", "err", reqErr)
		logger.Info("indexer detail", "detail", reqErr.Error())
		logger.Debug("indexer case", "case", name)
	}

	res := searchEchoing(t, echoingPage(false, false, false, false))

	logger.Info("indexer result", "result", res)
	logger.Info("indexer results", "results", []indexer.Result{res})

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

	for _, marker := range []string{"indexer search failed", "indexer result"} {
		if !strings.Contains(string(contents), marker) {
			t.Fatalf("the log file does not contain the lines under test:\n%s", contents)
		}
	}

	assertNoCredentialLeak(t, "the log file", string(contents))
}

// TestTheCredentialIsSentByHTTPXAndNotByTheDefinition is the other half of
// DEC-073: the schema has no credential placeholder, and the credential
// that does reach the source comes from the client's configuration.
func TestTheCredentialIsSentByHTTPXAndNotByTheDefinition(t *testing.T) {
	t.Parallel()

	src := newSource(t, map[string]reply{"/search": fixturePage(t, "search.html")})

	a, err := New(Options{
		Definition: src.pointAt(loadDefinition(t, htmlDefinitionFile)),
		Client:     credentialledClient(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := a.Search(testContext(t), keywordQuery()); err != nil {
		t.Fatalf("Search: %v", err)
	}

	if got := src.lastQuery(t, "/search").Get(httpx.DefaultAPIKeyParam); got != testKey {
		t.Errorf("the api key httpx injects did not reach the source: %q", got)
	}

	// And the schema cannot express one itself.
	source := strings.Replace(minimalDefinition, "  path: /s", "  path: /s\n  params:\n    k: \"{{apikey}}\"", 1)

	if _, err := Parse([]byte(source)); err == nil {
		t.Error("a definition naming an {{apikey}} placeholder was accepted")
	}
}
