package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/engine/fake"
	"github.com/kdta91/tortui/internal/indexer"
	"github.com/kdta91/tortui/internal/store"
)

// Two-link fixtures for T-9056: a Torznab-style result publishes a magnet
// and an enclosure whose address carries the user's api key.
const (
	twoLinkMagnet = "magnet:?xt=urn:btih:9056905690569056905690569056905690569056&dn=Synthetic+Two+Link+Corpus"
	twoLinkURL    = "https://feed.example.org/api?t=get&id=9056&apikey=SENTINEL-9056"
	twoLinkHash   = "9056905690569056905690569056905690569056"
)

// TestAddSourceForChoosesExactlyOneLink pins DEC-147's rule: a non-blank
// Magnet wins and TorrentURL is dropped; otherwise TorrentURL is used.
func TestAddSourceForChoosesExactlyOneLink(t *testing.T) {
	const dest = "dest"

	cases := []struct {
		name       string
		r          indexer.Result
		wantMagnet string
		wantURL    string
	}{
		{"both links", indexer.Result{Magnet: twoLinkMagnet, TorrentURL: twoLinkURL}, twoLinkMagnet, ""},
		{"magnet only", indexer.Result{Magnet: twoLinkMagnet}, twoLinkMagnet, ""},
		{"url only", indexer.Result{TorrentURL: twoLinkURL}, "", twoLinkURL},
		{"blank magnet beside a url", indexer.Result{Magnet: "  ", TorrentURL: twoLinkURL}, "", twoLinkURL},
		{"magnet sent trimmed", indexer.Result{Magnet: " " + twoLinkMagnet + "\n", TorrentURL: twoLinkURL}, twoLinkMagnet, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := addSourceFor(tc.r, dest)
			want := engine.AddSource{Magnet: tc.wantMagnet, TorrentURL: tc.wantURL, SavePath: dest}

			if got != want {
				t.Fatalf("addSourceFor = %+v, want %+v", got, want)
			}
		})
	}
}

// TestAddFlowSendsTheEngineExactlyOneSource drives enter on the Results
// and Details screens through the picker into a fake engine that refuses
// more than one source, as the real one does (T-9056). Every case must
// add, switch to Downloads, and send and persist only the chosen link.
func TestAddFlowSendsTheEngineExactlyOneSource(t *testing.T) {
	cases := []struct {
		name string
		r    indexer.Result
		// resolve, when set, is the source's Resolve.
		resolve    func(indexer.Result) (indexer.Result, error)
		wantMagnet string
		wantURL    string
	}{
		{
			name:       "magnet and url",
			r:          indexer.Result{Magnet: twoLinkMagnet, TorrentURL: twoLinkURL, InfoHash: twoLinkHash},
			wantMagnet: twoLinkMagnet,
		},
		{
			name:       "magnet only",
			r:          indexer.Result{Magnet: twoLinkMagnet},
			wantMagnet: twoLinkMagnet,
		},
		{
			name:    "url only",
			r:       indexer.Result{TorrentURL: twoLinkURL},
			wantURL: twoLinkURL,
		},
		{
			// Torznab's Resolve builds no magnet from an infohash beside
			// a torrent URL (DEC-147); it only normalises the hash.
			name: "infohash beside a url",
			r:    indexer.Result{TorrentURL: twoLinkURL, InfoHash: strings.ToUpper(twoLinkHash)},
			resolve: func(r indexer.Result) (indexer.Result, error) {
				r.InfoHash = strings.ToLower(r.InfoHash)
				return r, nil
			},
			wantURL: twoLinkURL,
		},
		{
			// A source with nothing but a hash: Resolve builds the magnet.
			name: "magnet derived by resolve from a bare infohash",
			r:    indexer.Result{InfoHash: twoLinkHash},
			resolve: func(r indexer.Result) (indexer.Result, error) {
				r.Magnet = twoLinkMagnet
				return r, nil
			},
			wantMagnet: twoLinkMagnet,
		},
	}

	for _, screen := range []Screen{ScreenResults, ScreenDetails} {
		for _, tc := range cases {
			t.Run(screen.String()+"/"+tc.name, func(t *testing.T) {
				r := tc.r
				r.IndexerID = "src-a"
				r.Title = "Synthetic Two Link Corpus"
				r.SourceURL = "https://feed.example.org/details/9056"

				got, rec, m := addThroughScreen(t, screen, r, tc.resolve)

				if m.screen != ScreenDownloads {
					t.Fatalf("screen after the add = %v, want ScreenDownloads (status %q)", m.screen, m.statusBar.Message())
				}

				if got.Magnet != tc.wantMagnet || got.TorrentURL != tc.wantURL || got.FilePath != "" {
					t.Fatalf("engine got %+v, want Magnet %q and TorrentURL %q only", got, tc.wantMagnet, tc.wantURL)
				}

				if rec.Magnet != tc.wantMagnet || rec.TorrentURL != tc.wantURL {
					t.Fatalf("persisted Magnet %q, TorrentURL %q; want %q, %q", rec.Magnet, rec.TorrentURL, tc.wantMagnet, tc.wantURL)
				}

				if rec.IndexerID != "src-a" || rec.SourceURL != r.SourceURL {
					t.Errorf("persisted origin %q, %q; want %q, %q", rec.IndexerID, rec.SourceURL, "src-a", r.SourceURL)
				}
			})
		}
	}
}

// addThroughScreen presses enter on screen with r selected, runs Resolve
// when the flow asks for it, accepts the picker, and applies the add. It
// returns the AddSource the engine received, the record the store got, and
// the model after the add.
func addThroughScreen(t *testing.T, screen Screen, r indexer.Result, resolve func(indexer.Result) (indexer.Result, error)) (engine.AddSource, store.TorrentRecord, Model) {
	t.Helper()

	eng := newTestEngine(t)

	var sources []engine.AddSource

	eng.ScriptFor = func(src engine.AddSource) fake.Script {
		sources = append(sources, src)
		return fake.Downloading(30 * time.Second)
	}

	ix := &resolvingIndexer{id: "src-a", resolveFn: resolve}
	ts := &stubTorrentStore{}

	m := New(eng, testTheme(), WithSearcher(newStubResolveSearcher(ix)), WithTorrentStore(ts))
	m.screen = screen

	switch screen {
	case ScreenResults:
		m.lastResults = []indexer.Result{r}
		m.results = m.results.setResults(m.lastResults, indexer.ModeSearch, time.Now())
	case ScreenDetails:
		m.details = m.details.withResult(r)
	default:
		t.Fatalf("addThroughScreen: no add on %v", screen)
	}

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m = updated.(Model); !m.dest.open {
		if cmd == nil {
			t.Fatalf("enter neither opened the picker nor dispatched Resolve: %q", m.statusBar.Message())
		}

		updated, cmd = m.Update(cmd())
	}

	m, cmd = acceptDestination(t, updated, cmd)
	if cmd == nil {
		t.Fatal("accepting the picker dispatched no add")
	}

	msg, ok := cmd().(addResultMsg)
	if !ok {
		t.Fatal("the add cmd did not produce an addResultMsg")
	}

	if msg.err != nil {
		t.Fatalf("add failed: %v", msg.err)
	}

	updated, _ = m.Update(msg)
	m = updated.(Model)

	if len(sources) != 1 {
		t.Fatalf("engine received %d add(s), want 1", len(sources))
	}

	if len(ts.records) != 1 {
		t.Fatalf("store got %d record(s), want 1", len(ts.records))
	}

	return sources[0], ts.records[0], m
}
