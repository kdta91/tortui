package app

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/store"
	"github.com/kdta91/tortui/internal/tui"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// keyedTitle is the one result the keyed feed lists (synthetic), and
// keyedTorrentName the name inside its .torrent.
const (
	keyedTitle       = "Synthetic Two Link Corpus"
	keyedTorrentName = "synthetic-two-link-corpus.bin"
)

// How the keyed server answers a .torrent request.
const (
	fetchBlock int32 = iota // wait for release, or for the client to go away
	fetchFail               // HTTP 500
)

// keyedServer is a loopback Torznab source whose one result is added by an
// enclosure carrying the api key in its query (T-9057).
type keyedServer struct {
	*httptest.Server

	enclosure string
	mode      atomic.Int32
	gate      chan struct{}
	release   func()

	mu   sync.Mutex
	gets int
}

func newKeyedServer(t *testing.T, mode int32) *keyedServer {
	t.Helper()

	torrent, _ := syntheticTorrent(t)
	caps := fixture(t, "caps-minimal.xml")
	ks := &keyedServer{gate: make(chan struct{})}
	ks.mode.Store(mode)

	var once sync.Once
	ks.release = func() { once.Do(func() { close(ks.gate) }) }

	var feed []byte

	ks.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := feed

		switch r.URL.Query().Get("t") {
		case "caps":
			body = caps
		case "get":
			ks.mu.Lock()
			ks.gets++
			ks.mu.Unlock()

			if ks.mode.Load() == fetchFail {
				http.Error(w, "synthetic failure", http.StatusInternalServerError)
				return
			}

			select {
			case <-ks.gate:
			case <-r.Context().Done():
				return
			}

			body = torrent
		}

		if _, err := w.Write(body); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(ks.Close)
	t.Cleanup(ks.release) // runs first, so no handler is left waiting

	ks.enclosure = ks.URL + "/api?t=get&id=9057&apikey=" + sentinelKey
	feed = twoLinkFeed(ks.enclosure, "")

	return ks
}

func (ks *keyedServer) fetches() int {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	return ks.gets
}

// newKeyedApp starts the composition root over the sandbox with the keyed
// source configured.
func newKeyedApp(t *testing.T, ks *keyedServer) *App {
	t.Helper()

	a, err := New(Options{
		Capability: theme.Capability{Unicode: true},
		transport:  labTransport{host: ks.Listener.Addr().String(), lab: ks.Client().Transport},
		offline:    true,
		configure: func(c *config.Config) {
			c.Indexers = append(c.Indexers, labSource(ks.Server))
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return a
}

// screens runs a's model under teatest and keeps every byte it renders, so a
// test can check that something was never on screen at any point.
type screens struct {
	tm   *teatest.TestModel
	seen bytes.Buffer
}

func newScreens(t *testing.T, a *App) *screens {
	t.Helper()

	return &screens{tm: teatest.NewTestModel(t, a.Model(), teatest.WithInitialTermSize(240, 40))}
}

func (s *screens) waitFor(t *testing.T, sub string) {
	t.Helper()

	teatest.WaitFor(t, io.TeeReader(s.tm.Output(), &s.seen), func(bts []byte) bool {
		return bytes.Contains(bts, []byte(sub))
	}, teatest.WithCheckInterval(10*time.Millisecond), teatest.WithDuration(5*time.Second))
}

func (s *screens) key(k string) {
	if k == "enter" {
		s.tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
		return
	}

	s.tm.Type(k)
}

// quit ends the program and returns its last screen, after checking that
// neither it nor anything rendered before it showed the api key.
func (s *screens) quit(t *testing.T) string {
	t.Helper()

	if err := s.tm.Quit(); err != nil {
		t.Fatalf("Quit: %v", err)
	}

	rest, err := io.ReadAll(s.tm.FinalOutput(t, teatest.WithFinalTimeout(5*time.Second)))
	if err != nil {
		t.Fatalf("read final output: %v", err)
	}

	s.seen.Write(rest)

	final := s.tm.FinalModel(t).(tui.Model).View()

	if strings.Contains(s.seen.String(), sentinelKey) || strings.Contains(final, sentinelKey) {
		t.Errorf("the api key was rendered on screen; last screen:\n%s", final)
	}

	return final
}

// addKeyedResult searches for the keyed result from a first run's welcome
// screen and adds it, ending on Downloads.
func addKeyedResult(t *testing.T, s *screens) {
	t.Helper()

	s.waitFor(t, "Welcome to tortui")
	s.key(" ")
	s.key("/")
	s.key("corpus")
	s.key("enter") // commit the query field
	s.key("enter") // submit the search
	s.waitFor(t, keyedTitle)
	s.key("enter")
	s.waitFor(t, "writable")
	s.key("enter")
	s.waitFor(t, "Active (1)")
}

// waitForEngine polls a's engine until its one torrent satisfies ok.
func waitForEngine(t *testing.T, a *App, what string, ok func(engine.TorrentStatus) bool) engine.TorrentStatus {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)

	var last engine.TorrentStatus
	for time.Now().Before(deadline) {
		if list := a.Engine().List(); len(list) == 1 {
			last = list[0]
			if ok(last) {
				return last
			}
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("the torrent never became %s: %+v", what, last)

	return last
}

func isErrored(s engine.TorrentStatus) bool { return s.State == engine.StateErrored }

// closeKeyedApp closes a, then checks its log and its one persisted record:
// the record keeps the enclosure, api key and all, as its TorrentURL — a
// restart needs it to fetch the .torrent again — and nowhere else, and is
// named wantName.
func closeKeyedApp(t *testing.T, a *App, ks *keyedServer, wantName string) {
	t.Helper()

	logPath := filepath.Join(a.Loaded().Paths.StateDir, "tortui.log")
	recs := func() []store.TorrentRecord { return a.store.ListTorrents() }

	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := recs()
	if len(got) != 1 {
		t.Fatalf("store holds %d records, want 1", len(got))
	}

	rec := got[0]
	if rec.TorrentURL != ks.enclosure {
		t.Errorf("persisted TorrentURL = %q, want the enclosure the add used", rec.TorrentURL)
	}

	if rec.Name != wantName {
		t.Errorf("persisted name = %q, want %q", rec.Name, wantName)
	}

	rec.TorrentURL = ""
	if all := fmt.Sprintf("%+v %s", rec, rec.Metainfo); strings.Contains(all, sentinelKey) {
		t.Errorf("the api key reached a persisted field other than TorrentURL: %s", all)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	if !strings.Contains(string(data), "lifecycle: session resumed") {
		t.Fatalf("the log has no session entries, so this checks nothing:\n%s", data)
	}

	if strings.Contains(string(data), sentinelKey) {
		t.Errorf("the log holds the api key:\n%s", data)
	}
}

// TestKeyedURLAddNeverShowsTheKey is T-9057 end to end — the real Torznab
// adapter, registry, offline engine, session, store, and TUI. A result added
// by an enclosure whose query carries the api key is shown by its title
// while the .torrent is fetched, after the fetch fails (error detail
// included), across a restart, and by the torrent's own name once fetched.
// No screen, log line, or persisted field but the record's TorrentURL ever
// holds the key.
func TestKeyedURLAddNeverShowsTheKey(t *testing.T) {
	t.Run("fetching, then restarted and fetched", func(t *testing.T) {
		guardDefaultTransport(t)
		sandbox(t)
		t.Setenv(logLevelEnv, "debug")

		ks := newKeyedServer(t, fetchBlock)

		a := newKeyedApp(t, ks)
		s := newScreens(t, a)
		addKeyedResult(t, s)

		waitForEngine(t, a, "fetching", func(engine.TorrentStatus) bool { return ks.fetches() > 0 })

		s.key("x")
		s.waitFor(t, "Remove torrent?")
		s.key("n")
		s.key("p")

		if final := s.quit(t); !strings.Contains(final, keyedTitle) {
			t.Errorf("the row does not show the title while fetching:\n%s", final)
		}

		if st := a.Engine().List()[0]; st.InfoHash != "" || st.TotalBytes != 0 {
			t.Fatalf("the .torrent arrived before the restart: %+v", st)
		}

		closeKeyedApp(t, a, ks, keyedTitle)

		// The restart restores by the enclosure; its fetch waits again.
		b := newKeyedApp(t, ks)
		s = newScreens(t, b)
		s.key("4")
		s.waitFor(t, keyedTitle)

		ks.release()
		waitForEngine(t, b, "fetched", func(st engine.TorrentStatus) bool { return st.TotalBytes > 0 })
		s.waitFor(t, keyedTorrentName)

		if final := s.quit(t); !strings.Contains(final, keyedTorrentName) {
			t.Errorf("the row does not show the torrent's own name once fetched:\n%s", final)
		}

		closeKeyedApp(t, b, ks, keyedTorrentName)
	})

	t.Run("fetch fails, then restarted and fails again", func(t *testing.T) {
		guardDefaultTransport(t)
		sandbox(t)
		t.Setenv(logLevelEnv, "debug")

		ks := newKeyedServer(t, fetchFail)

		a := newKeyedApp(t, ks)
		s := newScreens(t, a)
		addKeyedResult(t, s)

		failed := waitForEngine(t, a, "errored", isErrored)
		if strings.Contains(failed.Err.Error(), sentinelKey) {
			t.Fatalf("the engine's error holds the api key: %v", failed.Err)
		}

		s.waitFor(t, "enter to expand")
		s.key("enter")
		s.waitFor(t, "HTTP 500")

		if final := s.quit(t); !strings.Contains(final, keyedTitle) || !strings.Contains(final, "fetch torrent file") {
			t.Errorf("the errored row does not show the title and its full reason:\n%s", final)
		}

		closeKeyedApp(t, a, ks, keyedTitle)

		b := newKeyedApp(t, ks)
		s = newScreens(t, b)
		s.key("4")
		s.waitFor(t, keyedTitle)

		waitForEngine(t, b, "errored", isErrored)
		s.waitFor(t, "enter to expand")
		s.key("enter")
		s.waitFor(t, "HTTP 500")

		if final := s.quit(t); !strings.Contains(final, keyedTitle) {
			t.Errorf("the restored errored row does not show the title:\n%s", final)
		}

		closeKeyedApp(t, b, ks, keyedTitle)
	})
}
