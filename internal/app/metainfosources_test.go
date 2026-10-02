package app

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/store"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// newPublishedSourcesApp starts the composition root over the sandbox with
// srv as its one Torznab source.
func newPublishedSourcesApp(t *testing.T, srv *twoLinkServer) *App {
	t.Helper()

	a, err := New(Options{
		Capability: theme.Capability{Unicode: true},
		transport:  labTransport{host: srv.Listener.Addr().String(), lab: srv.Client().Transport},
		offline:    true,
		configure: func(c *config.Config) {
			c.Indexers = append(c.Indexers, labSource(srv.Server))
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return a
}

// closeWithMagnet closes a and fails unless its one persisted record holds
// want as its magnet.
func closeWithMagnet(t *testing.T, a *App, want string) store.TorrentRecord {
	t.Helper()

	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	recs := a.store.ListTorrents()
	if len(recs) != 1 {
		t.Fatalf("store holds %d records, want 1", len(recs))
	}

	if recs[0].Magnet != want {
		t.Errorf("persisted magnet = %q, want %q", recs[0].Magnet, want)
	}

	return recs[0]
}

// TestPublishedMagnetSourcesAreDropped is T-9094 end to end — the real
// Torznab adapter, registry, offline engine, session, store, and TUI. A
// source publishes a magnet whose xs= and as= name a loopback host. The add
// flow hands it to the engine as published, and the session record, written
// from the engine's resume data, holds it without them. A record saved with
// them before T-9094 is rewritten without them by the restart that restores
// it. Their host is asked for nothing; under Offline that is also the
// engine's metainfo-source refusal, so the record is what this pins, and the
// engine package's loopback tests are what pin the fetch itself.
func TestPublishedMagnetSourcesAreDropped(t *testing.T) {
	guardDefaultTransport(t)
	sandbox(t)

	var hits atomic.Int32

	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(host.Close)

	torrent, hash := syntheticTorrent(t)
	plain := "magnet:?xt=urn:btih:" + hash + "&dn=Synthetic+Two+Link+Corpus"
	published := plain +
		"&xs=" + url.QueryEscape(host.URL+"/xs.torrent") +
		"&as=" + url.QueryEscape(host.URL+"/as.torrent")

	srv, _ := newTwoLinkServer(t, torrent,
		`<torznab:attr name="magneturl" value="`+html.EscapeString(published)+`"/>`)

	a := newPublishedSourcesApp(t, srv)
	s := newScreens(t, a)
	addKeyedResult(t, s)

	if list := a.Engine().List(); len(list) != 1 || !strings.EqualFold(list[0].InfoHash, hash) {
		t.Fatalf("engine tracks %+v, want the published magnet's torrent", list)
	}

	s.quit(t)

	rec := closeWithMagnet(t, a, plain)

	// The record as a session saved before T-9094 would have left it.
	st, err := store.Open(filepath.Join(a.Loaded().Paths.StateDir, StoreFileName))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	rec.Magnet = published
	if err := st.SetTorrent(rec); err != nil {
		t.Fatalf("SetTorrent: %v", err)
	}

	if err := st.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	b := newPublishedSourcesApp(t, srv)
	if list := b.Engine().List(); len(list) != 1 || !strings.EqualFold(list[0].InfoHash, hash) {
		t.Fatalf("restored engine tracks %+v, want the recorded torrent", list)
	}

	closeWithMagnet(t, b, plain)

	if n := hits.Load(); n != 0 {
		t.Errorf("the magnet's source host was asked %d time(s), want 0", n)
	}

	if n := srv.torrentFetches(); n != 0 {
		t.Errorf("the enclosure was fetched %d time(s) for a magnet add, want 0", n)
	}
}
