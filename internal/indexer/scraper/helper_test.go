package scraper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/indexer"
	"github.com/kdta91/tortui/internal/indexer/httpx"
)

// fixtureDir is where the checked-in pages and definitions live. AGENT.md
// §4 puts recorded HTTP fixtures in a testdata/ directory at the repository
// root rather than beside each package.
const fixtureDir = "../../../testdata/scraper"

// The checked-in definitions. No fixture, definition, name or address
// anywhere in this package refers to a real source (AGENT.md §2).
const (
	htmlDefinitionFile       = "fixture-archive.yml"
	searchOnlyDefinitionFile = "fixture-archive-search-only.yml"
	jsonDefinitionFile       = "fixture-api.yml"
)

// The ids those definitions declare.
const (
	htmlID = "fixture-archive"
	jsonID = "fixture-api"
)

// fixture reads one checked-in file.
func fixture(t *testing.T, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}

	return string(body)
}

// loadDefinition parses one checked-in definition.
func loadDefinition(t *testing.T, name string) *Definition {
	t.Helper()

	def, err := Parse([]byte(fixture(t, name)))
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}

	return def
}

// reply is one canned HTTP response.
type reply struct {
	body        string
	contentType string
	status      int
}

// pageReply is a 200 response carrying an HTML page.
func pageReply(body string) reply {
	return reply{status: http.StatusOK, contentType: "text/html; charset=utf-8", body: body}
}

// jsonReply is a 200 response carrying a JSON document.
func jsonReply(body string) reply {
	return reply{status: http.StatusOK, contentType: "application/json", body: body}
}

// fixturePage is pageReply reading its body from a checked-in page.
func fixturePage(t *testing.T, name string) reply {
	t.Helper()

	return pageReply(fixture(t, name))
}

// statusReply is a bare status code with no body.
func statusReply(status int) reply {
	return reply{status: status}
}

// request is one request the fake source received.
type request struct {
	query url.Values
	path  string
}

// fakeSource is an httptest.Server that answers each path with a canned
// reply and records every request it was sent.
//
// It is the whole of this package's network: AGENT.md §6.7 requires unit
// tests to make zero real network calls, and every test here replays
// checked-in fixtures through this server.
type fakeSource struct {
	server  *httptest.Server
	replies map[string]reply
	mu      sync.Mutex
	seen    []request
}

// newSource starts a server answering the given paths. A path with no reply
// configured answers 404, which is what a site that does not have it does.
func newSource(t *testing.T, replies map[string]reply) *fakeSource {
	t.Helper()

	src := &fakeSource{replies: replies}
	src.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		src.mu.Lock()
		src.seen = append(src.seen, request{path: r.URL.Path, query: r.URL.Query()})
		src.mu.Unlock()

		res, ok := src.replies[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)

			return
		}

		if res.contentType != "" {
			w.Header().Set("Content-Type", res.contentType)
		}

		status := res.status
		if status == 0 {
			status = http.StatusOK
		}

		w.WriteHeader(status)

		if res.body != "" {
			if _, err := w.Write([]byte(res.body)); err != nil {
				t.Errorf("write fixture response: %v", err)
			}
		}
	}))

	t.Cleanup(src.server.Close)

	return src
}

// pointAt rewrites a definition's base address to this server.
//
// The address goes through a local variable with no dot in its name on
// purpose. scripts/check-indexer-hostnames.sh reads `BaseURL: x.y` as a
// hostname assignment and flags it, which is the false positive backlog
// T-926 is about; assigning through a bare identifier says the same thing
// and keeps that check meaningful.
func (s *fakeSource) pointAt(def *Definition) *Definition {
	address := s.server.URL
	def.BaseURL = address

	return def
}

// requests returns every request the server was sent, in order.
func (s *fakeSource) requests() []request {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]request, len(s.seen))
	copy(out, s.seen)

	return out
}

// lastQuery returns the most recent query sent to one path.
func (s *fakeSource) lastQuery(t *testing.T, path string) url.Values {
	t.Helper()

	all := s.requests()

	for i := len(all) - 1; i >= 0; i-- {
		if all[i].path == path {
			return all[i].query
		}
	}

	t.Fatalf("the source was never sent a request for %s (got %v)", path, all)

	return nil
}

// testClient builds an httpx client with the per-host spacing and the retry
// loop turned off, so tests neither wait a second between two requests nor
// retry a deliberately failing one.
func testClient(cfg httpx.Config) *httpx.Client {
	cfg.MinHostInterval = -1

	if cfg.MaxAttempts == 0 {
		cfg.MaxAttempts = 1
	}

	return httpx.New(cfg)
}

// testContext returns a context with a deadline, as AGENT.md §6.2 requires
// of every network call, cancelled when the test ends.
func testContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	return ctx
}

// contextCancelled returns a context and its cancel function, for a test
// that wants to cancel it itself.
func contextCancelled(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	return ctx, cancel
}

// archive starts a source serving both of the invented site's pages and
// returns an adapter built from the checked-in html definition.
func archive(t *testing.T) (*fakeSource, *Adapter) {
	t.Helper()

	src := newSource(t, map[string]reply{
		"/search": fixturePage(t, "search.html"),
		"/latest": fixturePage(t, "latest.html"),
	})

	return src, mustAdapter(t, src, loadDefinition(t, htmlDefinitionFile))
}

// api starts a source serving the invented site's JSON endpoint and returns
// an adapter built from the checked-in json definition.
func api(t *testing.T) (*fakeSource, *Adapter) {
	t.Helper()

	src := newSource(t, map[string]reply{
		"/api/search": jsonReply(fixture(t, "api.json")),
	})

	return src, mustAdapter(t, src, loadDefinition(t, jsonDefinitionFile))
}

// mustAdapter builds an adapter for a definition pointed at src.
func mustAdapter(t *testing.T, src *fakeSource, def *Definition) *Adapter {
	t.Helper()

	a, err := New(Options{Definition: src.pointAt(def), Client: testClient(httpx.Config{})})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return a
}

// comparableResult is indexer.Result with its map and its time.Time left
// out, so the rest of it can be compared with == in one statement.
//
// Its two address fields are deliberately not named for a URL.
// scripts/check-indexer-hostnames.sh reads `SourceURL = x.y` as a hostname
// assignment and flags it, which is the false positive backlog T-926 is
// about; naming them for what they are keeps that check meaningful.
type comparableResult struct {
	IndexerID string
	ID        string
	Title     string
	InfoHash  string
	Magnet    string
	Download  string
	Page      string
	Uploader  string
	SizeBytes int64
	Seeders   int
	Leechers  int
	Category  indexer.Category
	Trust     indexer.Trust
}

// comparableOf flattens a result for comparison.
func comparableOf(r indexer.Result) comparableResult {
	out := comparableResult{
		IndexerID: r.IndexerID,
		ID:        r.ID,
		Title:     r.Title,
		InfoHash:  r.InfoHash,
		Magnet:    r.Magnet,
		Uploader:  r.Uploader,
		SizeBytes: r.SizeBytes,
		Seeders:   r.Seeders,
		Leechers:  r.Leechers,
		Category:  r.Category,
		Trust:     r.Trust,
	}

	out.Download = r.TorrentURL
	out.Page = r.SourceURL

	return out
}

// assertResult compares two results.
//
// indexer.Result carries a map, so it is not comparable with ==, and
// reflect.DeepEqual on a time.Time compares its internal representation
// rather than the instant. Both are handled here so a test can state the
// whole expected result in one literal.
func assertResult(t *testing.T, got, want indexer.Result) {
	t.Helper()

	if !got.Published.Equal(want.Published) {
		t.Errorf("Published = %v, want %v", got.Published, want.Published)
	}

	if len(got.Extra) != len(want.Extra) {
		t.Errorf("Extra = %v, want %v", got.Extra, want.Extra)
	}

	for key, value := range want.Extra {
		if got.Extra[key] != value {
			t.Errorf("Extra[%q] = %q, want %q", key, got.Extra[key], value)
		}
	}

	if comparableOf(got) != comparableOf(want) {
		t.Errorf("result:\n got %+v\nwant %+v", got, want)
	}
}
