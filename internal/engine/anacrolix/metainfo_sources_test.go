package anacrolix

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anacrolix/torrent"

	"github.com/kdta91/tortui/internal/engine"
)

// sourceFetchGrace is how long a test keeps listening for a stray xs=/as=
// fetch after its positive control was fetched. The library starts every
// source's fetch the moment the torrent is added, with no delay, and the
// torrent under test is always added before the control, so a source left in
// a magnet is requested well inside this window.
const sourceFetchGrace = 250 * time.Millisecond

// sourcesServer is a loopback host standing in for the arbitrary web host a
// magnet's xs= or as= address can name (T-9094). It records every path it
// is asked for; it never serves a .torrent, so nothing it is asked for ever
// completes a torrent.
type sourcesServer struct {
	*httptest.Server

	mu   sync.Mutex
	hits []string
	hit  chan struct{}
}

func newSourcesServer(t *testing.T) *sourcesServer {
	t.Helper()

	s := &sourcesServer{hit: make(chan struct{}, 64)}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.hits = append(s.hits, r.URL.Path)
		s.mu.Unlock()

		select {
		case s.hit <- struct{}{}:
		default:
		}

		http.NotFound(w, r)
	}))
	t.Cleanup(s.Close)

	return s
}

// requested lists the paths asked for so far.
func (s *sourcesServer) requested() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.hits...)
}

// sourced appends xs= and as= parameters naming this server to magnet,
// under paths that say which key and which torrent asked.
func (s *sourcesServer) sourced(magnet, seed string) string {
	return magnet +
		"&xs=" + url.QueryEscape(s.URL+"/xs/"+seed+".torrent") +
		"&as=" + url.QueryEscape(s.URL+"/as/"+seed+".torrent")
}

// assertNeverFetched proves the torrents added before it asked this server
// for nothing. As a positive control it hands the client directly a torrent
// whose only source is this server, waits until that is fetched — so the
// client's source fetching is live and a source already given to it has had
// its turn — then keeps listening for sourceFetchGrace. Any path other than
// the control's fails the test.
func (s *sourcesServer) assertNeverFetched(t *testing.T, e *Engine) {
	t.Helper()

	const control = "/control.torrent"

	spec, err := torrent.TorrentSpecFromMagnetUri(magnetURI("sources-control-" + t.Name()))
	if err != nil {
		t.Fatalf("control magnet: %v", err)
	}

	store, err := e.storageFor(e.downloadDir)
	if err != nil {
		t.Fatalf("control storage: %v", err)
	}

	spec.Sources = []string{s.URL + control}
	spec.Storage = store

	if _, _, err := e.client.AddTorrentSpec(spec); err != nil {
		t.Fatalf("add control torrent: %v", err)
	}

	controlled := func() bool {
		for _, p := range s.requested() {
			if p == control {
				return true
			}
		}

		return false
	}

	deadline := time.After(5 * time.Second)
	for !controlled() {
		select {
		case <-s.hit:
		case <-deadline:
			t.Fatalf("the control source was never fetched, so this checks nothing; requested %q", s.requested())
		}
	}

	grace := time.After(sourceFetchGrace)

	for {
		for _, p := range s.requested() {
			if p != control {
				t.Fatalf("the client fetched a magnet's source %q; requested %q", p, s.requested())
			}
		}

		select {
		case <-s.hit:
		case <-grace:
			return
		}
	}
}

// newSourcesEngine is an offline engine whose client still fetches a
// torrent's metainfo sources, so a source left in a magnet would reach the
// loopback server; the metadata timeout outlasts the test.
func newSourcesEngine(t *testing.T, mutate func(*Options)) *Engine {
	t.Helper()

	return newTestEngine(t, func(o *Options) {
		o.metainfoSources = true
		o.MetadataTimeout = time.Minute

		if mutate != nil {
			mutate(o)
		}
	})
}

// assertResumeMagnet fails unless id's resume data — what the session
// record is rewritten from — holds want as its magnet.
func assertResumeMagnet(t *testing.T, e *Engine, id, want string) {
	t.Helper()

	d, err := e.ResumeData(id)
	if err != nil {
		t.Fatalf("ResumeData: %v", err)
	}

	if d.Magnet != want {
		t.Errorf("resume magnet = %q, want %q", d.Magnet, want)
	}
}

// TestAddedMagnetSourcesAreNeverFetched is T-9094 for Add: a magnet a user
// typed or a source published, carrying xs= and as= addresses on a loopback
// host, is added without that host being asked for anything — whether it
// starts at once or waits in the queue — and its resume data holds the
// magnet without them.
func TestAddedMagnetSourcesAreNeverFetched(t *testing.T) {
	t.Parallel()

	t.Run("started", func(t *testing.T) {
		t.Parallel()

		srv := newSourcesServer(t)
		e := newSourcesEngine(t, nil)
		plain := magnetURI("sources-added")

		id, err := e.Add(context.Background(), engine.AddSource{Magnet: srv.sourced(plain, "added")})
		if err != nil {
			t.Fatalf("Add: %v", err)
		}

		srv.assertNeverFetched(t, e)
		assertResumeMagnet(t, e, id, plain)
	})

	t.Run("queued then promoted", func(t *testing.T) {
		t.Parallel()

		srv := newSourcesServer(t)
		e := newSourcesEngine(t, func(o *Options) { o.Config.MaxActiveDownloads = 1 })

		first, err := e.Add(context.Background(), engine.AddSource{Magnet: magnetURI("sources-first")})
		if err != nil {
			t.Fatalf("Add first: %v", err)
		}

		plain := magnetURI("sources-queued")

		id, err := e.Add(context.Background(), engine.AddSource{Magnet: srv.sourced(plain, "queued")})
		if err != nil {
			t.Fatalf("Add queued: %v", err)
		}

		if st := statusOf(t, e, id); st.State != engine.StateQueued {
			t.Fatalf("state = %s, want queued behind the first torrent", st.State)
		}

		if err := e.Remove(first, false); err != nil {
			t.Fatalf("Remove: %v", err)
		}

		waitUntil(t, "promoted", func() bool { return statusOf(t, e, id).State != engine.StateQueued })
		srv.assertNeverFetched(t, e)
		assertResumeMagnet(t, e, id, plain)
	})
}

// TestRestoredMagnetSourcesAreNeverFetched is T-9094 for Restore: a magnet
// persisted with xs= and as= before they were dropped is restored without
// their host being asked for anything, and its resume data — from which the
// next save rewrites the record — no longer holds them. An entry that cannot
// restore is rewritten the same way.
func TestRestoredMagnetSourcesAreNeverFetched(t *testing.T) {
	t.Parallel()

	t.Run("restored", func(t *testing.T) {
		t.Parallel()

		srv := newSourcesServer(t)
		e := newSourcesEngine(t, nil)
		plain := magnetURI("sources-restored")

		id, err := e.Restore(context.Background(), engine.ResumeData{
			ID:       "an-7",
			Name:     "Synthetic Restored Corpus",
			Magnet:   srv.sourced(plain, "restored"),
			SavePath: e.downloadDir,
		})
		if err != nil {
			t.Fatalf("Restore: %v", err)
		}

		if st := statusOf(t, e, id); st.State == engine.StateErrored {
			t.Fatalf("the restore failed: %v", st.Err)
		}

		srv.assertNeverFetched(t, e)
		assertResumeMagnet(t, e, id, plain)
	})

	t.Run("cannot restore", func(t *testing.T) {
		t.Parallel()

		e := newTestEngine(t, nil)
		plain := magnetURI("sources-outside")
		magnet := plain + "&xs=" + url.QueryEscape("http://other.example.org/x.torrent")

		id, err := e.Restore(context.Background(), engine.ResumeData{
			ID:       "an-8",
			Magnet:   magnet,
			SavePath: t.TempDir(), // outside every known root
		})
		if err != nil {
			t.Fatalf("Restore: %v", err)
		}

		if st := statusOf(t, e, id); st.State != engine.StateErrored {
			t.Fatalf("state = %s, want errored outside every root", st.State)
		}

		assertResumeMagnet(t, e, id, plain)
	})
}

// TestOfflineRefusesMetainfoSources: under Offline the client's
// metainfo-source fetcher refuses every request before it dials, so even a
// source handed to the library directly reaches nothing; without Offline,
// or with the test switch, the library's own client is kept.
func TestOfflineRefusesMetainfoSources(t *testing.T) {
	t.Parallel()

	srv := newSourcesServer(t)
	dir := t.TempDir()

	cfg := clientConfig(Options{Offline: true}, dir, discardLogger(), 0)
	if cfg.MetainfoSourcesClient == nil {
		t.Fatal("Offline left the metainfo-source client to the library's default")
	}

	resp, err := cfg.MetainfoSourcesClient.Get(srv.URL + "/offline.torrent")
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("Offline fetched a metainfo source")
	}

	if !strings.Contains(err.Error(), errMetainfoSourcesOffline.Error()) {
		t.Errorf("error = %v, want %v", err, errMetainfoSourcesOffline)
	}

	if got := srv.requested(); len(got) != 0 {
		t.Errorf("the source host was asked for %q under Offline", got)
	}

	for name, opts := range map[string]Options{
		"online":           {},
		"offline, test on": {Offline: true, metainfoSources: true},
	} {
		if c := clientConfig(opts, dir, discardLogger(), 0); c.MetainfoSourcesClient != nil {
			t.Errorf("%s: metainfo-source client replaced; want the library's", name)
		}
	}
}
