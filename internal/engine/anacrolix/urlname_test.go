package anacrolix

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/engine"
)

// urlSentinel stands in for an indexer api key in a .torrent address
// (T-9057). It must never reach a name, an error, or the log.
const urlSentinel = "SENTINEL-9057"

// lockedBuffer is a log sink safe for the engine's goroutines to write to.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// newLoggedEngine is newTestEngine with a debug-level log captured.
func newLoggedEngine(t *testing.T) (*Engine, *lockedBuffer) {
	t.Helper()

	log := &lockedBuffer{}
	e := newTestEngine(t, func(o *Options) {
		o.Logger = slog.New(slog.NewTextHandler(log, &slog.HandlerOptions{Level: slog.LevelDebug}))
		o.MetadataTimeout = 2 * time.Second
	})

	return e, log
}

// serveGatedTorrent serves body only once release is closed, so a test can
// look at a URL-added torrent while its fetch is still in flight. The
// address carries the sentinel api key in its query.
func serveGatedTorrent(t *testing.T, body []byte) (address string, release func()) {
	t.Helper()

	gate := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}

		_, _ = w.Write(body)
	}))

	var once sync.Once
	release = func() { once.Do(func() { close(gate) }) }

	t.Cleanup(srv.Close)
	t.Cleanup(release) // runs first: a handler still waiting returns

	return srv.URL + "/api?t=get&id=9057&apikey=" + urlSentinel, release
}

// assertNoSentinel fails when any of texts carries the sentinel.
func assertNoSentinel(t *testing.T, what string, texts ...string) {
	t.Helper()

	for _, s := range texts {
		if strings.Contains(s, urlSentinel) {
			t.Errorf("%s carries the api key: %q", what, s)
		}
	}
}

// waitForInfoHash polls until the torrent's .torrent has been fetched and
// attached, whatever state it then moves on to.
func waitForInfoHash(t *testing.T, e *Engine, id string) engine.TorrentStatus {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)

	var last engine.TorrentStatus
	for time.Now().Before(deadline) {
		last = statusOf(t, e, id)
		if last.InfoHash != "" || last.State == engine.StateErrored {
			return last
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("torrent %s never attached: state %s, err %v", id, last.State, last.Err)

	return last
}

// TestURLAddIsNamedByHostUntilItsTorrentArrives is T-9057 at the engine: a
// torrent added by an address with an api key in its query is named by the
// address's host while the .torrent is fetched, then by the torrent's own
// name. The resume data keeps the address itself, since a restart needs it,
// but never as the name; the debug log never holds the key.
func TestURLAddIsNamedByHostUntilItsTorrentArrives(t *testing.T) {
	t.Parallel()

	e, log := newLoggedEngine(t)
	address, release := serveGatedTorrent(t, encodeTorrent(t, buildInfo("url-name-9057", [][]string{{"a.bin"}})))

	id, err := e.Add(context.Background(), engine.AddSource{TorrentURL: address})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	st := statusOf(t, e, id)
	if st.Name != "torrent file from 127.0.0.1" {
		t.Errorf("name while fetching = %q, want %q", st.Name, "torrent file from 127.0.0.1")
	}

	d, err := e.ResumeData(id)
	if err != nil {
		t.Fatalf("ResumeData: %v", err)
	}

	if d.TorrentURL != address {
		t.Errorf("resume data TorrentURL = %q, want the address it was added by", d.TorrentURL)
	}

	assertNoSentinel(t, "status and resume name while fetching", st.Name, errText(st.Err), d.Name)

	release()

	st = waitForInfoHash(t, e, id)
	if st.Name != "url-name-9057" || st.Err != nil {
		t.Errorf("after the fetch: name %q, err %v; want the torrent's own name", st.Name, st.Err)
	}

	assertNoSentinel(t, "the log", log.String())
}

// TestURLAddFetchFailuresNeverEchoTheAddress covers the fail path: whatever
// stops a .torrent fetch, the errored torrent's name and reason, and the
// log, never carry the address or its key.
func TestURLAddFetchFailuresNeverEchoTheAddress(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		handler http.HandlerFunc // nil: nothing listens at the address
		want    string
	}{
		"http 500": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "upstream failed "+urlSentinel, http.StatusInternalServerError)
			},
			want: "fetch torrent file",
		},
		"not a torrent": {
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("not bencode " + urlSentinel))
			},
			want: "parse fetched torrent file",
		},
		"connection refused": {want: "fetch torrent file"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(tc.handler)
			if tc.handler == nil {
				srv.Close()
			} else {
				t.Cleanup(srv.Close)
			}

			e, log := newLoggedEngine(t)
			address := srv.URL + "/api?t=get&id=9057&apikey=" + urlSentinel

			id, err := e.Add(context.Background(), engine.AddSource{TorrentURL: address})
			if err != nil {
				t.Fatalf("Add: %v", err)
			}

			st := waitForState(t, e, id, engine.StateErrored)
			if !strings.Contains(errText(st.Err), tc.want) {
				t.Errorf("Err = %v, want it to say %q", st.Err, tc.want)
			}

			if st.Name != "torrent file from 127.0.0.1" {
				t.Errorf("errored name = %q, want %q", st.Name, "torrent file from 127.0.0.1")
			}

			d, err := e.ResumeData(id)
			if err != nil {
				t.Fatalf("ResumeData: %v", err)
			}

			assertNoSentinel(t, "the errored torrent", st.Name, errText(st.Err), fmt.Sprint(st.Err), d.Name)
			assertNoSentinel(t, "the log", log.String())

			if strings.Contains(errText(st.Err), "/api?") {
				t.Errorf("Err echoes the address: %v", st.Err)
			}
		})
	}
}

// TestRestoreByURLNeverNamesTheTorrentByItsAddress: a restored torrent that
// is still to be fetched again is named by its saved title; a session saved
// before T-9057, which named it by its address, gets the host instead —
// whether the restore starts the fetch or fails outright.
func TestRestoreByURLNeverNamesTheTorrentByItsAddress(t *testing.T) {
	t.Parallel()

	e, log := newLoggedEngine(t)
	address, _ := serveGatedTorrent(t, encodeTorrent(t, buildInfo("restored-9057", [][]string{{"a.bin"}})))
	outside := t.TempDir()

	cases := []struct {
		name string
		data engine.ResumeData
		want string
	}{
		{"saved title", engine.ResumeData{ID: "an-1", Name: "Synthetic Corpus 9057", TorrentURL: address}, "Synthetic Corpus 9057"},
		{"saved address", engine.ResumeData{ID: "an-2", Name: address, TorrentURL: address}, "torrent file from 127.0.0.1"},
		{"no saved name", engine.ResumeData{ID: "an-3", TorrentURL: address}, "torrent file from 127.0.0.1"},
		{"saved address, unrestorable", engine.ResumeData{ID: "an-4", Name: address, TorrentURL: address, SavePath: outside}, "torrent file from 127.0.0.1"},
	}

	for _, tc := range cases {
		id, err := e.Restore(context.Background(), tc.data)
		if err != nil {
			t.Fatalf("%s: Restore: %v", tc.name, err)
		}

		st := statusOf(t, e, id)
		if st.Name != tc.want {
			t.Errorf("%s: name = %q, want %q", tc.name, st.Name, tc.want)
		}

		d, err := e.ResumeData(id)
		if err != nil {
			t.Fatalf("%s: ResumeData: %v", tc.name, err)
		}

		assertNoSentinel(t, tc.name, st.Name, errText(st.Err), d.Name)
	}

	assertNoSentinel(t, "the log", log.String())
}
