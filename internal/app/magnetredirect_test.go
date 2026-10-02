package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kdta91/tortui/internal/engine"
)

// redirectPasskey stands in for a private tracker's passkey in the tr=
// address of the magnet the keyed source redirects to (T-9079).
const redirectPasskey = "PASSKEY-9079"

// redirectHash is the synthetic infohash of that magnet.
const redirectHash = "9079907990799079907990799079907990799079"

// redirectMagnet is a synthetic magnet-only result's link: an invented hash
// and title, an example.org tracker whose path carries the passkey.
const redirectMagnet = "magnet:?xt=urn:btih:" + redirectHash + "&dn=Synthetic+Magnet+Corpus" +
	"&tr=https%3A%2F%2Ftracker.example.org%2F" + redirectPasskey + "%2Fannounce"

// redirectSecrets are the texts no screen and no log line may ever hold: the
// api key, the passkey, and the magnet itself.
var redirectSecrets = []string{sentinelKey, redirectPasskey, "magnet:", "btih"}

// assertNoRedirectSecret fails when text holds any of redirectSecrets.
func assertNoRedirectSecret(t *testing.T, what, text string) {
	t.Helper()

	for _, secret := range redirectSecrets {
		if strings.Contains(text, secret) {
			t.Errorf("%s holds %q:\n%s", what, secret, text)
		}
	}
}

// hasRedirectHash reports whether the torrent was switched to the magnet.
func hasRedirectHash(st engine.TorrentStatus) bool {
	return st.InfoHash == redirectHash || st.State == engine.StateErrored
}

// closeRedirectApp closes a, then checks its one persisted record — the
// magnet is its only source, the address with its api key is gone, and its
// name is the result's title — and that the log holds no secret.
func closeRedirectApp(t *testing.T, a *App) {
	t.Helper()

	logPath := filepath.Join(a.Loaded().Paths.StateDir, "tortui.log")

	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := a.store.ListTorrents()
	if len(got) != 1 {
		t.Fatalf("store holds %d records, want 1", len(got))
	}

	rec := got[0]
	if rec.Magnet != redirectMagnet || rec.TorrentURL != "" {
		t.Errorf("persisted magnet %q, address %q; want the magnet and no address", rec.Magnet, rec.TorrentURL)
	}

	if rec.Name != keyedTitle {
		t.Errorf("persisted name = %q, want %q", rec.Name, keyedTitle)
	}

	if all := fmt.Sprintf("%+v %s", rec, rec.Metainfo); strings.Contains(all, sentinelKey) {
		t.Errorf("the api key is persisted: %s", all)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	if !strings.Contains(string(data), "redirected to a magnet link") {
		t.Fatalf("the log has no entry for the switch, so this checks nothing:\n%s", data)
	}

	assertNoRedirectSecret(t, "the log", string(data))
}

// TestMagnetRedirectAddEndToEnd is T-9079 end to end — the real Torznab
// adapter, registry, offline engine, session, store, and TUI. A result whose
// enclosure (api key in its query) is answered by a redirect to a magnet is
// added by that magnet: the row keeps the result's title, the session keeps
// the magnet instead of the address, and a restart resumes by the magnet
// without requesting the address again. No screen or log line ever holds the
// api key, the passkey, or the magnet.
func TestMagnetRedirectAddEndToEnd(t *testing.T) {
	guardDefaultTransport(t)
	sandbox(t)
	t.Setenv(logLevelEnv, "debug")

	ks := newKeyedServer(t, fetchMagnet)

	a := newKeyedApp(t, ks)
	s := newScreens(t, a)
	addKeyedResult(t, s)

	st := waitForEngine(t, a, "switched to the magnet", hasRedirectHash)
	if st.State == engine.StateErrored {
		t.Fatalf("the add failed: %v", st.Err)
	}

	final := s.quit(t)
	if !strings.Contains(final, keyedTitle) {
		t.Errorf("the row does not show the title:\n%s", final)
	}

	assertNoRedirectSecret(t, "the screens", s.seen.String()+final)
	closeRedirectApp(t, a)

	if n := ks.fetches(); n != 1 {
		t.Fatalf("the address was requested %d times, want 1", n)
	}

	b := newKeyedApp(t, ks)
	s = newScreens(t, b)
	s.key("4")
	s.waitFor(t, keyedTitle)

	st = waitForEngine(t, b, "restored by the magnet", hasRedirectHash)
	if st.State == engine.StateErrored {
		t.Fatalf("the restore failed: %v", st.Err)
	}

	final = s.quit(t)
	assertNoRedirectSecret(t, "the restored screens", s.seen.String()+final)
	closeRedirectApp(t, b)

	if n := ks.fetches(); n != 1 {
		t.Errorf("the address was requested %d times across the restart, want 1", n)
	}
}
