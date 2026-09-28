package prowlarr

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/indexer/httpx"
)

func TestListIndexersSendsAPIKeyHeaderAndParsesTheResponse(t *testing.T) {
	t.Parallel()

	var gotHeader string
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Api-Key")
		gotPath = r.URL.Path

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":1,"name":"First Indexer","protocol":"torrent","enable":true},
			{"id":2,"name":"Second Indexer","protocol":"torrent","enable":true}
		]`))
	}))
	defer srv.Close()

	got, err := ListIndexers(context.Background(), nil, srv.URL, "my-api-key")
	if err != nil {
		t.Fatalf("ListIndexers: %v", err)
	}

	if gotHeader != "my-api-key" {
		t.Fatalf("X-Api-Key header = %q, want my-api-key", gotHeader)
	}
	if gotPath != indexerListPath {
		t.Fatalf("request path = %q, want %q", gotPath, indexerListPath)
	}

	want := []Indexer{{ID: "1", Name: "First Indexer"}, {ID: "2", Name: "Second Indexer"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ListIndexers = %#v, want %#v", got, want)
	}
}

// TestListIndexersFiltersOutUsenetProtocolIndexers proves a usenet-protocol
// indexer is left out: it has no Torznab feed at all, so importing it as
// one would silently produce a source that never returns a result (found
// in review).
func TestListIndexersFiltersOutUsenetProtocolIndexers(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":1,"name":"Torrent Indexer","protocol":"torrent","enable":true},
			{"id":2,"name":"Usenet Indexer","protocol":"usenet","enable":true},
			{"id":3,"name":"Unknown Protocol Indexer","protocol":"unknown","enable":true}
		]`))
	}))
	defer srv.Close()

	got, err := ListIndexers(context.Background(), nil, srv.URL, "key")
	if err != nil {
		t.Fatalf("ListIndexers: %v", err)
	}

	want := []Indexer{{ID: "1", Name: "Torrent Indexer"}}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("ListIndexers = %#v, want %#v (usenet/unknown-protocol indexers filtered out)", got, want)
	}
}

// TestListIndexersFiltersOutDisabledIndexers proves an indexer the
// aggregator itself has disabled is left out rather than imported
// (Backlog T-985; found in review).
func TestListIndexersFiltersOutDisabledIndexers(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":1,"name":"Enabled Indexer","protocol":"torrent","enable":true},
			{"id":2,"name":"Disabled Indexer","protocol":"torrent","enable":false}
		]`))
	}))
	defer srv.Close()

	got, err := ListIndexers(context.Background(), nil, srv.URL, "key")
	if err != nil {
		t.Fatalf("ListIndexers: %v", err)
	}

	want := []Indexer{{ID: "1", Name: "Enabled Indexer"}}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("ListIndexers = %#v, want %#v (disabled indexer filtered out)", got, want)
	}
}

func TestListIndexersTrimsATrailingSlashOnBaseURL(t *testing.T) {
	t.Parallel()

	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	if _, err := ListIndexers(context.Background(), nil, srv.URL+"/", "key"); err != nil {
		t.Fatalf("ListIndexers: %v", err)
	}

	if gotPath != indexerListPath {
		t.Fatalf("request path = %q, want %q (no double slash)", gotPath, indexerListPath)
	}
}

func TestListIndexersClassifiesAnUnauthenticatedProbe(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := ListIndexers(context.Background(), nil, srv.URL, "wrong-key")
	if err == nil {
		t.Fatal("expected an error for a 401 response")
	}

	var statusErr *httpx.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error chain does not carry *httpx.StatusError: %v", err)
	}

	if !statusErr.AuthFailed() {
		t.Fatalf("StatusError.AuthFailed() = false for a 401, want true")
	}
}

func TestListIndexersReportsAParseFailureOnAMalformedBody(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	_, err := ListIndexers(context.Background(), nil, srv.URL, "key")
	if err == nil {
		t.Fatal("expected a parse error for a non-JSON body")
	}

	var parseErr *ParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("error chain does not carry *ParseError: %v", err)
	}

	if !parseErr.ParseFailed() {
		t.Fatalf("ParseError.ParseFailed() = false, want true")
	}
}

func TestListIndexersATimeoutIsClassifiableAsATimeout(t *testing.T) {
	t.Parallel()

	block := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	// Close(), unlike Shutdown(), waits for every still-open connection —
	// including this one, whose handler is blocked until block is closed —
	// so that defer must run (unblocking the handler) before this one does,
	// not after. Deferred in this order so LIFO unwinds close(block) first.
	defer srv.Close()
	defer close(block)

	client := httpx.New(httpx.Config{
		ConnectTimeout: 20 * time.Millisecond,
		ReadTimeout:    20 * time.Millisecond,
		MaxAttempts:    1,
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := ListIndexers(ctx, client, srv.URL, "key")
	if err == nil {
		t.Fatal("expected a timeout error")
	}

	var netErr interface{ Timeout() bool }
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("error chain is not classifiable as a timeout: %v", err)
	}
}

func TestFeedURLBuildsProwlarrsOwnPerIndexerTorznabPattern(t *testing.T) {
	t.Parallel()

	cases := []struct {
		base, id, want string
	}{
		{"http://127.0.0.1:9696", "3", "http://127.0.0.1:9696/3/api"},
		{"http://127.0.0.1:9696/", "3", "http://127.0.0.1:9696/3/api"},
	}

	for _, tc := range cases {
		if got := FeedURL(tc.base, tc.id); got != tc.want {
			t.Errorf("FeedURL(%q, %q) = %q, want %q", tc.base, tc.id, got, tc.want)
		}
	}
}

func TestListIndexersNilClientDoesNotPanic(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	got, err := ListIndexers(context.Background(), nil, srv.URL, "key")
	if err != nil {
		t.Fatalf("ListIndexers: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d indexers, want 0", len(got))
	}
}
