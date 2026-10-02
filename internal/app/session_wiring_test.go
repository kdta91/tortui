package app

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/tui/components"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// labTransport sends requests for the lab feed's host to that feed and
// answers every other source (the bundled ones) with a 404, so the root runs
// end to end with zero network (AGENT.md §6.7).
type labTransport struct {
	host string
	lab  http.RoundTripper
}

func (lt labTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == lt.host {
		return lt.lab.RoundTrip(req)
	}

	return &http.Response{
		StatusCode: http.StatusNotFound,
		Status:     "404 Not Found",
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}, nil
}

// magnetFeed is a one-item Torznab feed on an invented example.org source.
// The item carries a magnet only, so the offline engine accepts the add
// without fetching anything.
const magnetFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Lab Feed</title>
    <item>
      <title>Invented Wiring Corpus</title>
      <guid isPermaLink="true">https://feed.example.org/details/9008</guid>
      <comments>https://feed.example.org/details/9008</comments>
      <pubDate>Mon, 28 Sep 2026 12:00:00 +0000</pubDate>
      <size>1048576</size>
      <torznab:attr name="seeders" value="5"/>
      <torznab:attr name="peers" value="5"/>
      <torznab:attr name="magneturl" value="magnet:?xt=urn:btih:9008900890089008900890089008900890089008&amp;dn=Invented+Wiring+Corpus"/>
    </item>
  </channel>
</rss>
`

// TestAddFlowRecordsThroughTheSession pins T-994's wiring in the composition
// root: the TUI's add flow must write its torrent record through the
// Session, whose SetTorrent is serialised with every save, never straight to
// the store, where a save in flight can overwrite or prune it (T-9008).
//
// The probe is a closed session: a record written through it is refused
// with ErrSessionClosed, while one written to the store lands. So the test
// closes the session, adds the feed's first result through the real TUI —
// search, picker, engine — and checks the refusal reached the status bar
// and no record reached the store.
func TestAddFlowRecordsThroughTheSession(t *testing.T) {
	guardDefaultTransport(t)
	sandbox(t)

	srv := newTorznabServer(t, fixture(t, "caps-minimal.xml"), []byte(magnetFeed))

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

	t.Cleanup(func() {
		if err := a.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	if err := a.session.Close(); err != nil {
		t.Fatalf("Session.Close: %v", err)
	}

	// Wide enough that the status bar shows the whole refusal.
	tm := teatest.NewTestModel(t, a.Model(), teatest.WithInitialTermSize(240, 40))

	waitForAny(t, tm, "Welcome to tortui")
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	// The program opens on Search with nothing queried (T-9011): search
	// for the lab feed's one result.
	tm.Type("/corpus")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter}) // one enter submits the search
	waitForAny(t, tm, "Invented Wiring Corpus")

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForAny(t, tm, "writable")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// The downloads screen shows once the add's result is handled, so the
	// add flow's record write has run by then.
	waitForAny(t, tm, "Active (1)")

	if n := len(a.Engine().List()); n != 1 {
		t.Fatalf("engine tracks %d torrents after the add, want 1", n)
	}

	if recs := a.store.ListTorrents(); len(recs) != 0 {
		t.Fatalf("the add flow wrote %d record(s) straight to the store past a closed session, want 0: it must write through the Session (T-994)", len(recs))
	}

	if err := tm.Quit(); err != nil {
		t.Fatalf("Quit: %v", err)
	}

	// The refusal sits in the status bar's queue behind the startup and
	// search messages. Step the stopped model's queue along by hand and
	// read each message off its View, rather than race the live renderer.
	const refusal = "couldn't save torrent record: lifecycle: session is closed"

	m := tm.FinalModel(t, teatest.WithFinalTimeout(3*time.Second))
	for range 10 {
		if strings.Contains(m.View(), refusal) {
			return
		}

		m, _ = m.Update(components.TickMsg{})
	}

	t.Fatalf("the status bar never showed %q: the add's record write did not go through the session", refusal)
}
