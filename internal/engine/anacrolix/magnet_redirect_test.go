package anacrolix

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/indexer/httpx"
)

// redirectPasskey stands in for a private tracker's passkey in a magnet's
// tr= address (T-9079). Like urlSentinel it must never reach a name, an
// error, or the log.
const redirectPasskey = "PASSKEY-9079"

// redirectHash is the synthetic infohash every redirect fixture names.
const redirectHash = "9079907990799079907990799079907990799079"

// redirectMagnet is a synthetic magnet-only result's link: invented hash and
// title, an example.org tracker whose path carries the passkey.
func redirectMagnet(hash string) string {
	return "magnet:?xt=urn:btih:" + hash + "&dn=Synthetic+Magnet+Corpus" +
		"&tr=https%3A%2F%2Ftracker.example.org%2F" + redirectPasskey + "%2Fannounce"
}

// serveMagnetRedirect stands in for a Torznab aggregator's download address
// for a magnet-only result: every request is answered with status and a
// Location of location. The address carries the sentinel api key; hits
// counts the requests served.
func serveMagnetRedirect(t *testing.T, status int, location string) (address string, hits *atomic.Int32) {
	t.Helper()

	hits = &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Location", location)
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)

	return srv.URL + "/api?t=get&id=9079&apikey=" + urlSentinel, hits
}

// newRedirectEngine is newLoggedEngine with a metadata timeout long enough
// that a switched torrent stays checking for the whole test; mutate, when
// set, adjusts the options further.
func newRedirectEngine(t *testing.T, mutate func(*Options)) (*Engine, *lockedBuffer) {
	t.Helper()

	log := &lockedBuffer{}
	e := newTestEngine(t, func(o *Options) {
		o.Logger = slog.New(slog.NewTextHandler(log, &slog.HandlerOptions{Level: slog.LevelDebug}))
		o.MetadataTimeout = time.Minute

		if mutate != nil {
			mutate(o)
		}
	})

	return e, log
}

// assertNoRedirectSecrets fails when any of texts carries the api key, the
// passkey, or a magnet link.
func assertNoRedirectSecrets(t *testing.T, what string, texts ...string) {
	t.Helper()

	for _, s := range texts {
		for _, secret := range []string{urlSentinel, redirectPasskey, "magnet:", "/api?"} {
			if strings.Contains(s, secret) {
				t.Errorf("%s carries %q: %q", what, secret, s)
			}
		}
	}
}

// TestMagnetRedirectSwitchesTheTorrentInPlace is T-9079 at the engine: for
// every redirect status, a torrent added by an address that redirects to a
// magnet keeps its id, destination and name, tracks the magnet's infohash,
// and reports the magnet — not the address — as its source. Nothing is
// requested beyond the address, once; no name, error or log line carries the
// api key, the passkey or the magnet.
func TestMagnetRedirectSwitchesTheTorrentInPlace(t *testing.T) {
	t.Parallel()

	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()

			e, log := newRedirectEngine(t, nil)
			magnet := redirectMagnet(redirectHash)
			address, hits := serveMagnetRedirect(t, status, magnet)
			dest := e.downloadDir

			id, err := e.Add(context.Background(), engine.AddSource{TorrentURL: address, SavePath: dest})
			if err != nil {
				t.Fatalf("Add: %v", err)
			}

			st := waitForInfoHash(t, e, id)
			if st.State == engine.StateErrored {
				t.Fatalf("the add failed: %v", st.Err)
			}

			if st.ID != id || st.InfoHash != redirectHash || st.SavePath != dest {
				t.Errorf("status = id %q hash %q path %q; want %q, %q, %q", st.ID, st.InfoHash, st.SavePath, id, redirectHash, dest)
			}

			if st.Name != "torrent file from 127.0.0.1" {
				t.Errorf("name = %q, want the name it had while fetching", st.Name)
			}

			d, err := e.ResumeData(id)
			if err != nil {
				t.Fatalf("ResumeData: %v", err)
			}

			if d.Magnet != magnet || d.TorrentURL != "" {
				t.Errorf("resume data magnet %q, url %q; want the magnet and no address", d.Magnet, d.TorrentURL)
			}

			if n := hits.Load(); n != 1 {
				t.Errorf("the address was requested %d times, want 1", n)
			}

			assertNoRedirectSecrets(t, "the torrent", st.Name, errText(st.Err), d.Name)
			assertNoRedirectSecrets(t, "the log", log.String())
		})
	}
}

// TestMagnetRedirectRestartResumesByTheMagnet: the resume data of a switched
// torrent restores, in a fresh engine, by the magnet alone — same id and
// infohash, and the address is never requested again.
func TestMagnetRedirectRestartResumesByTheMagnet(t *testing.T) {
	t.Parallel()

	first, _ := newRedirectEngine(t, nil)
	address, hits := serveMagnetRedirect(t, http.StatusFound, redirectMagnet(redirectHash))

	id, err := first.Add(context.Background(), engine.AddSource{TorrentURL: address})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	waitForInfoHash(t, first, id)

	d, err := first.ResumeData(id)
	if err != nil {
		t.Fatalf("ResumeData: %v", err)
	}

	d.SavePath = ""

	second, log := newRedirectEngine(t, nil)

	got, err := second.Restore(context.Background(), d)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	st := statusOf(t, second, got)
	if got != id || st.InfoHash != redirectHash || st.State == engine.StateErrored {
		t.Errorf("restored id %q hash %q state %s err %v; want %q, %q, not errored", got, st.InfoHash, st.State, st.Err, id, redirectHash)
	}

	if n := hits.Load(); n != 1 {
		t.Errorf("the address was requested %d times across the restart, want 1", n)
	}

	assertNoRedirectSecrets(t, "the restored torrent's log", log.String())
}

// TestMagnetRedirectDuplicateFailsLikeAFetchedTorrent: a magnet whose
// infohash is already tracked fails the redirected torrent exactly as a
// fetched .torrent's would, and that torrent keeps its address as its
// source.
func TestMagnetRedirectDuplicateFailsLikeAFetchedTorrent(t *testing.T) {
	t.Parallel()

	e, log := newRedirectEngine(t, nil)
	magnet := redirectMagnet(redirectHash)
	address, _ := serveMagnetRedirect(t, http.StatusFound, magnet)

	existing, err := e.Add(context.Background(), engine.AddSource{Magnet: magnet})
	if err != nil {
		t.Fatalf("Add magnet: %v", err)
	}

	id, err := e.Add(context.Background(), engine.AddSource{TorrentURL: address})
	if err != nil {
		t.Fatalf("Add address: %v", err)
	}

	st := waitForState(t, e, id, engine.StateErrored)
	if want := "already added as " + existing; !strings.Contains(errText(st.Err), want) {
		t.Errorf("Err = %v, want it to say %q", st.Err, want)
	}

	d, err := e.ResumeData(id)
	if err != nil {
		t.Fatalf("ResumeData: %v", err)
	}

	if d.Magnet != "" || d.TorrentURL != address {
		t.Errorf("resume data magnet %q, url %q; want the address kept", d.Magnet, d.TorrentURL)
	}

	if other := statusOf(t, e, existing); other.State == engine.StateErrored {
		t.Errorf("the existing torrent was failed: %v", other.Err)
	}

	assertNoRedirectSecrets(t, "the duplicate", st.Name, errText(st.Err))
	assertNoRedirectSecrets(t, "the log", log.String())
}

// TestMagnetRedirectUnusableMagnetIsRefused: a magnet: Location httpx will
// not accept, and one it accepts but the torrent library cannot parse, both
// fail the torrent with a reason that echoes nothing of the Location, and the
// torrent keeps its address as its source.
func TestMagnetRedirectUnusableMagnetIsRefused(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		location string
		want     error
	}{
		"hash too short": {
			location: "magnet:?xt=urn:btih:" + redirectHash[:39] + "&tr=https%3A%2F%2Ftracker.example.org%2F" + redirectPasskey,
			want:     httpx.ErrMagnetRedirectInvalid,
		},
		"bad v2 hash beside the v1 one": {
			location: redirectMagnet(redirectHash) + "&xt=urn:btmh:" + redirectPasskey,
			want:     errRedirectMagnetUnusable,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			e, log := newRedirectEngine(t, nil)
			address, _ := serveMagnetRedirect(t, http.StatusFound, tc.location)

			id, err := e.Add(context.Background(), engine.AddSource{TorrentURL: address})
			if err != nil {
				t.Fatalf("Add: %v", err)
			}

			st := waitForState(t, e, id, engine.StateErrored)
			if !errors.Is(st.Err, tc.want) {
				t.Errorf("Err = %v, want %v", st.Err, tc.want)
			}

			if !strings.Contains(errText(st.Err), "fetch torrent file") || !strings.Contains(errText(st.Err), "127.0.0.1") {
				t.Errorf("Err = %v, want the fetch named with its host", st.Err)
			}

			d, err := e.ResumeData(id)
			if err != nil {
				t.Fatalf("ResumeData: %v", err)
			}

			if d.Magnet != "" || d.TorrentURL != address {
				t.Errorf("resume data magnet %q, url %q; want the address kept", d.Magnet, d.TorrentURL)
			}

			assertNoRedirectSecrets(t, "the refused torrent", st.Name, errText(st.Err), fmt.Sprint(st.Err), d.Name)
			assertNoRedirectSecrets(t, "the log", log.String())
		})
	}
}

// TestMagnetRedirectKeepsQueueRules: a switched torrent keeps the download
// slot its fetch held, so with one slot a later magnet and a later address
// both queue — the queued address is not even requested — and the queued
// address, once promoted, is fetched and switched in turn.
func TestMagnetRedirectKeepsQueueRules(t *testing.T) {
	t.Parallel()

	e, _ := newRedirectEngine(t, func(o *Options) { o.Config.MaxActiveDownloads = 1 })
	first, firstHits := serveMagnetRedirect(t, http.StatusFound, redirectMagnet(redirectHash))

	const laterHash = "1979197919791979197919791979197919791979"

	later, laterHits := serveMagnetRedirect(t, http.StatusSeeOther, redirectMagnet(laterHash))

	a, err := e.Add(context.Background(), engine.AddSource{TorrentURL: first})
	if err != nil {
		t.Fatalf("Add first: %v", err)
	}

	waitForInfoHash(t, e, a)

	b, err := e.Add(context.Background(), engine.AddSource{Magnet: magnetURI("queued-9079")})
	if err != nil {
		t.Fatalf("Add magnet: %v", err)
	}

	c, err := e.Add(context.Background(), engine.AddSource{TorrentURL: later})
	if err != nil {
		t.Fatalf("Add later: %v", err)
	}

	for _, id := range []string{b, c} {
		if st := statusOf(t, e, id); st.State != engine.StateQueued {
			t.Errorf("%s state = %s, want queued behind the switched torrent", id, st.State)
		}
	}

	if laterHits.Load() != 0 || firstHits.Load() != 1 {
		t.Fatalf("requests: first %d, later %d; want 1 and 0", firstHits.Load(), laterHits.Load())
	}

	for _, id := range []string{a, b} {
		if err := e.Remove(id, false); err != nil {
			t.Fatalf("Remove %s: %v", id, err)
		}
	}

	st := waitForInfoHash(t, e, c)
	if st.InfoHash != laterHash || st.State == engine.StateErrored {
		t.Errorf("promoted torrent hash %q state %s err %v; want %q", st.InfoHash, st.State, st.Err, laterHash)
	}

	if laterHits.Load() != 1 {
		t.Errorf("the promoted address was requested %d times, want 1", laterHits.Load())
	}
}
