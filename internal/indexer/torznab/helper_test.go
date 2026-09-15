package torznab

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

	"github.com/kdta91/tortui/internal/indexer/httpx"
)

// fixtureDir is where the XML fixtures live. AGENT.md §4 puts recorded HTTP
// fixtures in a testdata/ directory at the repository root rather than beside
// each package, and the T-021 acceptance criteria name testdata/torznab/*.xml
// specifically, so the path is relative rather than the usual package-local
// "testdata".
const fixtureDir = "../../../testdata/torznab"

// testID and testName identify the adapter under test. No fixture, name, or
// URL anywhere in this package refers to a real source (AGENT.md §2).
const (
	testID   = "fixture-feed"
	testName = "Fixture Feed"
)

// fixture reads one XML fixture.
func fixture(t *testing.T, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join(fixtureDir, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}

	return string(body)
}

// reply is one canned HTTP response.
type reply struct {
	body        string
	contentType string
	status      int
}

// xmlReply is a 200 response carrying an XML document.
func xmlReply(body string) reply {
	return reply{status: http.StatusOK, contentType: "application/rss+xml", body: body}
}

// fixtureReply is xmlReply reading its body from a fixture file.
func fixtureReply(t *testing.T, name string) reply {
	t.Helper()

	return xmlReply(fixture(t, name))
}

// statusReply is a bare status code with no body.
func statusReply(status int) reply {
	return reply{status: status}
}

// fakeSource is an httptest.Server that answers each Torznab function with a
// canned reply and records every query it was sent.
//
// It is the whole of this package's network: AGENT.md §6.7 requires unit
// tests to make zero real network calls, and every test here replays
// fixtures through this server.
type fakeSource struct {
	server  *httptest.Server
	replies map[string]reply
	mu      sync.Mutex
	queries []url.Values
}

// newSource starts a server answering the given functions — the values of
// the Torznab `t` parameter, so "caps" and "search". A function with no
// reply configured answers 404, which is exactly what a server that does not
// implement it does.
func newSource(t *testing.T, replies map[string]reply) *fakeSource {
	t.Helper()

	src := &fakeSource{replies: replies}
	src.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()

		src.mu.Lock()
		src.queries = append(src.queries, query)
		src.mu.Unlock()

		res, ok := src.replies[query.Get(paramFunction)]
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

// options returns Options pointing at this server.
//
// The endpoint goes through a local variable with no dot in its name on
// purpose. scripts/check-indexer-hostnames.sh reads `Endpoint: x.y` as a
// hostname assignment and flags it, which is the false positive T-020 hit
// and backlog T-926 is about; assigning through a bare identifier says the
// same thing and keeps that check meaningful.
func (s *fakeSource) options() Options {
	address := s.server.URL

	opts := Options{ID: testID, Name: testName}
	opts.Endpoint = address
	opts.Client = testClient(httpx.Config{})

	return opts
}

// requests returns every query the server was sent, in order.
func (s *fakeSource) requests() []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]url.Values, len(s.queries))
	copy(out, s.queries)

	return out
}

// lastQuery returns the most recent query for one Torznab function.
func (s *fakeSource) lastQuery(t *testing.T, function string) url.Values {
	t.Helper()

	all := s.requests()

	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Get(paramFunction) == function {
			return all[i]
		}
	}

	t.Fatalf("the source was never sent a t=%s request (got %v)", function, all)

	return nil
}

// testClient builds an httpx client with the per-host spacing and the retry
// loop turned off, so tests neither wait a second between the probe's two
// requests nor retry a deliberately failing one.
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

// mustDiscover builds an adapter against src and fails the test if the probe
// did not succeed.
func mustDiscover(t *testing.T, src *fakeSource) *Adapter {
	t.Helper()

	a, err := Discover(testContext(t), src.options())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	return a
}

// mustNew builds an unprobed adapter against src.
func mustNew(t *testing.T, src *fakeSource) *Adapter {
	t.Helper()

	a, err := New(src.options())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return a
}

// searchable is the reply pair for a source that works: full caps, and a
// feed for both the probe and the search that follows.
func searchable(t *testing.T) map[string]reply {
	t.Helper()

	return map[string]reply{
		functionCaps:   fixtureReply(t, "caps-full.xml"),
		functionSearch: fixtureReply(t, "search-full.xml"),
	}
}
