package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/indexer"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// sentinelKey is the API key every test source is configured with, and
// sentinelSession its session value; neither may reach the log file. Both
// are deliberately low-entropy placeholders, not credential-shaped strings.
const (
	sentinelKey     = "t096t096t096t096"
	sentinelSession = "t096sessiont096"
)

// fixture reads one of the recorded torznab fixtures.
func fixture(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "torznab", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}

	return data
}

// torznabServer answers t=caps with caps and every other request with feed,
// recording the api keys it was sent.
type torznabServer struct {
	*httptest.Server

	mu   sync.Mutex
	keys []string
}

func newTorznabServer(t *testing.T, caps, feed []byte) *torznabServer {
	t.Helper()

	ts := &torznabServer{}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts.mu.Lock()
		ts.keys = append(ts.keys, r.URL.Query().Get("apikey"))
		ts.mu.Unlock()

		w.Header().Set("Content-Type", "application/xml")

		body := feed
		if r.URL.Query().Get("t") == "caps" {
			body = caps
		}

		if _, err := w.Write(body); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(ts.Close)

	return ts
}

func (ts *torznabServer) requests() int {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	return len(ts.keys)
}

// newSettingsApp starts an offline App whose indexer requests go to srv.
func newSettingsApp(t *testing.T, srv *httptest.Server) *App {
	t.Helper()

	sandbox(t)

	a, err := New(Options{
		Capability:      theme.Capability{Unicode: true},
		transport:       srv.Client().Transport,
		minHostInterval: -1,
		offline:         true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() {
		if err := a.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	return a
}

func labSource(srv *httptest.Server) config.Indexer {
	feed := srv.URL + "/api"

	return config.Indexer{ID: "lab", Name: "Lab Feed", Type: "torznab", URL: feed, APIKey: sentinelKey, Enabled: true}
}

// reload reads config.toml back the way the next start will.
func reload(t *testing.T) config.Config {
	t.Helper()

	loaded, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	return loaded.Config
}

// assertConfigMode checks config.toml is readable by its owner only
// (AGENT.md §6.6); Windows has no POSIX permission bits.
func assertConfigMode(t *testing.T, path string) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 && !isWindowsPerm(perm) {
		t.Errorf("config.toml mode = %v after a settings save, want 0600", perm)
	}
}

func registered(a *App, id string) bool {
	_, ok := a.Registry().Get(id)
	return ok
}

func TestSettingsAddedTorznabSourceIsSearchableWithoutRestart(t *testing.T) {
	srv := newTorznabServer(t, fixture(t, "caps-minimal.xml"), fixture(t, "search-full.xml"))
	a := newSettingsApp(t, srv.Server)
	sm := a.settings

	if registered(a, "lab") {
		t.Fatal("lab is registered before it was added")
	}

	if err := sm.SaveSources([]config.Indexer{labSource(srv.Server)}); err != nil {
		t.Fatalf("SaveSources: %v", err)
	}

	results, srcErrs, err := a.Registry().SearchAll(context.Background(), indexer.Query{Text: "corpus"}, "lab")
	if err != nil || len(srcErrs) != 0 {
		t.Fatalf("SearchAll(lab): err %v, source errors %v", err, srcErrs)
	}

	if len(results) == 0 || results[0].IndexerID != "lab" {
		t.Fatalf("SearchAll(lab) = %+v, want the feed's results from lab", results)
	}

	if got := sm.Sources(); len(got) != 1 || got[0] != labSource(srv.Server) {
		t.Errorf("Sources() = %+v, want the saved source", got)
	}

	if got := reload(t).Indexers; len(got) != 1 || got[0] != labSource(srv.Server) {
		t.Errorf("config.toml indexers = %+v, want the saved source", got)
	}

	assertConfigMode(t, a.Loaded().Paths.ConfigFile)
}

func TestSettingsDisabledOrRemovedSourceLeavesTheRegistry(t *testing.T) {
	srv := newTorznabServer(t, fixture(t, "caps-minimal.xml"), fixture(t, "search-full.xml"))
	a := newSettingsApp(t, srv.Server)
	sm := a.settings
	src := labSource(srv.Server)

	save := func(sources ...config.Indexer) {
		t.Helper()

		if err := sm.SaveSources(sources); err != nil {
			t.Fatalf("SaveSources: %v", err)
		}
	}

	save(src)

	disabled := src
	disabled.Enabled = false
	save(disabled)

	if registered(a, "lab") {
		t.Error("lab is still registered after it was disabled")
	}

	if got := reload(t).Indexers; len(got) != 1 || got[0].Enabled {
		t.Errorf("config.toml indexers = %+v, want lab kept, disabled", got)
	}

	save(src)

	if !registered(a, "lab") {
		t.Error("lab is not registered after it was re-enabled")
	}

	renamed := src
	renamed.Name = "Lab Feed Renamed"
	save(renamed)

	if ix, ok := a.Registry().Get("lab"); !ok || ix.Name() != renamed.Name {
		t.Errorf("after an edit the registry holds %v (ok %v), want the edited source", ix, ok)
	}

	save()

	if registered(a, "lab") {
		t.Error("lab is still registered after it was removed")
	}

	if got := reload(t).Indexers; len(got) != 0 {
		t.Errorf("config.toml indexers = %+v after removing the only source, want none", got)
	}
}

func TestSettingsEntryOverBundledIDTurnsTheDefaultOffAndBack(t *testing.T) {
	srv := newTorznabServer(t, fixture(t, "caps-minimal.xml"), fixture(t, "search-full.xml"))
	a := newSettingsApp(t, srv.Server)
	id := bundledID(t)

	if !registered(a, id) {
		t.Fatalf("bundled source %s is not registered at startup", id)
	}

	feed := srv.URL + "/api"
	off := config.Indexer{ID: id, Name: "off", Type: "torznab", URL: feed, Enabled: false}
	if err := a.settings.SaveSources([]config.Indexer{off}); err != nil {
		t.Fatalf("SaveSources: %v", err)
	}

	if registered(a, id) {
		t.Errorf("bundled source %s is still registered under a disabled entry of the same id", id)
	}

	if err := a.settings.SaveSources(nil); err != nil {
		t.Fatalf("SaveSources: %v", err)
	}

	if !registered(a, id) {
		t.Errorf("bundled source %s did not come back once the overriding entry was removed", id)
	}
}

// TestSettingsFailedBuiltinSaveLeavesMemoryAlone: a toggle whose write to
// disk fails changes nothing in memory. Without the clone of the disabled
// list, the removal edits the shared backing array before the save is even
// tried, so Config() would show the half-applied list (T-9074).
func TestSettingsFailedBuiltinSaveLeavesMemoryAlone(t *testing.T) {
	srv := newTorznabServer(t, nil, nil)
	a := newSettingsApp(t, srv.Server)
	sm := a.settings
	id := bundledID(t)

	// A regular file where the config directory should be: the write fails
	// on every platform.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	sm.mu.Lock()
	sm.path = filepath.Join(blocker, "config.toml")
	sm.cfg.DisabledBuiltins = []string{"first-other", id, "last-other"}
	sm.mu.Unlock()

	want := []string{"first-other", id, "last-other"}

	if err := sm.SetBuiltinEnabled(id, true); err == nil {
		t.Fatal("SetBuiltinEnabled saved to an unwritable path; the failure this test needs did not happen")
	}

	if got := sm.Config().DisabledBuiltins; !slices.Equal(got, want) {
		t.Errorf("DisabledBuiltins after a failed save = %q, want %q", got, want)
	}
}

// Duck-typed the same way internal/tui's classifyProbeError reads them.
type (
	authFailure  interface{ AuthFailed() bool }
	parseFailure interface{ ParseFailed() bool }
	timeoutErr   interface{ Timeout() bool }
)

func TestSettingsConnectionTestClassifiesAuthAndTimeout(t *testing.T) {
	good := newTorznabServer(t, fixture(t, "caps-minimal.xml"), fixture(t, "search-full.xml"))
	a := newSettingsApp(t, good.Server)
	sm := a.settings

	t.Run("reachable", func(t *testing.T) {
		if err := sm.TestSource(context.Background(), labSource(good.Server)); err != nil {
			t.Fatalf("TestSource = %v, want nil", err)
		}

		if registered(a, "lab") {
			t.Error("a connection test registered the source it probed")
		}
	})

	t.Run("auth", func(t *testing.T) {
		deny := fixture(t, "caps-error.xml")
		srv := newTorznabServer(t, deny, deny)

		err := sm.TestSource(context.Background(), labSource(srv.Server))

		var auth authFailure
		if !errors.As(err, &auth) || !auth.AuthFailed() {
			t.Fatalf("TestSource = %v, want an error reporting AuthFailed", err)
		}
	})

	t.Run("parse", func(t *testing.T) {
		// A sign-in page in place of the caps document: it arrived, and
		// it is not a torznab response (T-981).
		page := []byte("<html><body>Sign in</body></html>")
		srv := newTorznabServer(t, page, page)

		err := sm.TestSource(context.Background(), labSource(srv.Server))

		var parse parseFailure
		if !errors.As(err, &parse) || !parse.ParseFailed() {
			t.Fatalf("TestSource = %v, want an error reporting ParseFailed", err)
		}

		var auth authFailure
		if errors.As(err, &auth) && auth.AuthFailed() {
			t.Errorf("a parse failure reported AuthFailed: %v", err)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		release := make(chan struct{})
		hang := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-release:
			}
		}))
		t.Cleanup(hang.Close)
		t.Cleanup(func() { close(release) })

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		err := sm.TestSource(ctx, labSource(hang))

		var te timeoutErr
		if !errors.Is(err, context.DeadlineExceeded) && (!errors.As(err, &te) || !te.Timeout()) {
			t.Fatalf("TestSource = %v, want a timeout", err)
		}

		var auth authFailure
		if errors.As(err, &auth) && auth.AuthFailed() {
			t.Errorf("a timeout reported AuthFailed: %v", err)
		}
	})
}

func TestSettingsAggregatorImportFillsEveryFeedURL(t *testing.T) {
	var (
		mu     sync.Mutex
		gotKey string
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotKey = r.Header.Get("X-Api-Key")
		mu.Unlock()

		if r.URL.Path != "/api/v1/indexer" {
			http.NotFound(w, r)
			return
		}

		_, err := w.Write([]byte(`[
			{"id": 7, "name": "Invented Archive", "protocol": "torrent", "enable": true},
			{"id": 9, "name": "Invented Usenet", "protocol": "usenet", "enable": true},
			{"id": 12, "name": "Invented Mirror", "protocol": "torrent", "enable": true}
		]`))
		if err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	a := newSettingsApp(t, srv)

	got, err := a.settings.ListAggregatorIndexers(context.Background(), srv.URL+"/", sentinelKey)
	if err != nil {
		t.Fatalf("ListAggregatorIndexers: %v", err)
	}

	mu.Lock()
	if gotKey != sentinelKey {
		t.Errorf("aggregator got api key %q, want the user's own key", gotKey)
	}
	mu.Unlock()

	want := []string{srv.URL + "/7/api", srv.URL + "/12/api"}
	if len(got) != len(want) {
		t.Fatalf("ListAggregatorIndexers = %+v, want %d torrent indexers", got, len(want))
	}

	for i, ix := range got {
		if ix.FeedURL == "" || ix.FeedURL != want[i] {
			t.Errorf("indexer %s FeedURL = %q, want %q", ix.ID, ix.FeedURL, want[i])
		}
	}

	if _, err := a.settings.ListAggregatorIndexers(context.Background(), " ", sentinelKey); !errors.Is(err, errNoAggregatorURL) {
		t.Errorf("empty address: err = %v, want errNoAggregatorURL", err)
	}
}

func TestSettingsImportedDefinitionBecomesASearchableSource(t *testing.T) {
	srv := newTorznabServer(t, nil, nil)
	a := newSettingsApp(t, srv.Server)
	sm := a.settings

	file := filepath.Join(t.TempDir(), "example-user.yml")
	if err := os.WriteFile(file, []byte(userDefinition), 0o600); err != nil {
		t.Fatal(err)
	}

	id, baseURL, err := sm.ImportDefinition(context.Background(), file)
	if err != nil || id != "example-user" {
		t.Fatalf("ImportDefinition = %q, %v; want example-user", id, err)
	}

	if baseURL != "https://example.org" {
		t.Errorf("ImportDefinition base_url = %q, want the definition's base_url", baseURL)
	}

	if _, err := os.Stat(filepath.Join(a.Loaded().Paths.DefinitionsDir, id+".yml")); err != nil {
		t.Fatalf("imported definition not installed: %v", err)
	}

	src := config.Indexer{ID: id, Name: "Example User Source", Type: "scraper", Definition: id + ".yml", Enabled: true}
	if err := sm.SaveSources([]config.Indexer{src}); err != nil {
		t.Fatalf("SaveSources: %v", err)
	}

	if !registered(a, id) {
		t.Fatal("the imported scraper source is not registered after saving it")
	}

	// A reload rebuilds definition-backed sources from disk: with the file
	// gone, the source can no longer be built and leaves the registry.
	if err := os.Remove(filepath.Join(a.Loaded().Paths.DefinitionsDir, id+".yml")); err != nil {
		t.Fatal(err)
	}

	if err := sm.ReloadDefinitions(); err == nil {
		t.Error("ReloadDefinitions = nil with a source that can no longer be built, want it reported")
	}

	if registered(a, id) {
		t.Error("a scraper source whose definition was removed survived ReloadDefinitions")
	}

	if srv.requests() != 0 {
		t.Errorf("import and reload made %d indexer requests, want 0", srv.requests())
	}
}

func TestSettingsPreferencesRoundTripAndAdmitDestinationRoots(t *testing.T) {
	srv := newTorznabServer(t, fixture(t, "caps-minimal.xml"), fixture(t, "search-full.xml"))
	a := newSettingsApp(t, srv.Server)
	sm := a.settings

	if err := sm.SaveSources([]config.Indexer{labSource(srv.Server)}); err != nil {
		t.Fatalf("SaveSources: %v", err)
	}

	dest := filepath.Join(t.TempDir(), "saved-dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}

	magnet := "magnet:?xt=urn:btih:89abcdef0123456789abcdef0123456789abcdef&dn=t096.iso"
	if _, err := a.Engine().Add(context.Background(), engine.AddSource{Magnet: magnet, SavePath: dest}); err == nil {
		t.Fatal("the engine accepted a destination that is not yet a known root; the admit check below proves nothing")
	}

	// A stale snapshot: taken with no sources, so it must not wipe lab.
	cfg := config.Default(a.Loaded().Config.DownloadDir)
	cfg.SavedDestinations = []string{dest}
	cfg.MaxPeers = 17
	cfg.SeedPolicy = "off"
	cfg.SearchTimeout = "9s"
	cfg.Theme = "default"

	if err := sm.SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	got := reload(t)
	if !slices.Equal(got.SavedDestinations, []string{dest}) || got.MaxPeers != 17 || got.SeedPolicy != "off" || got.SearchTimeout != "9s" {
		t.Errorf("config.Load after SaveConfig = %+v, want the saved preferences", got)
	}

	if len(got.Indexers) != 1 || got.Indexers[0].ID != "lab" {
		t.Errorf("SaveConfig from a stale snapshot left indexers %+v, want lab kept", got.Indexers)
	}

	if c := sm.Config(); c.MaxPeers != 17 || len(c.Indexers) != 1 {
		t.Errorf("Config() after SaveConfig = %+v, want the saved preferences and lab", c)
	}

	assertConfigMode(t, a.Loaded().Paths.ConfigFile)

	if _, err := a.Engine().Add(context.Background(), engine.AddSource{Magnet: magnet, SavePath: dest}); err != nil {
		t.Errorf("Add into the saved destination after SaveConfig: %v; want it admitted as a root", err)
	}
}

func TestSettingsSaveConfigRefusesANonRootDestinationWithoutWriting(t *testing.T) {
	srv := newTorznabServer(t, nil, nil)
	a := newSettingsApp(t, srv.Server)
	path := a.Loaded().Paths.ConfigFile

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	cfg := a.settings.Config()
	cfg.SavedDestinations = []string{"relative/dir"}
	cfg.MaxPeers = 3

	if err := a.settings.SaveConfig(cfg); !errors.Is(err, engine.ErrUnsafePath) {
		t.Fatalf("SaveConfig(relative destination) = %v, want ErrUnsafePath", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if string(before) != string(after) {
		t.Error("a refused SaveConfig still rewrote config.toml")
	}

	if a.settings.Config().MaxPeers == 3 {
		t.Error("a refused SaveConfig still changed the in-memory config")
	}
}

func TestSettingsCredentialsNeverReachTheLog(t *testing.T) {
	srv := newTorznabServer(t, fixture(t, "caps-minimal.xml"), fixture(t, "search-full.xml"))
	sandbox(t)

	a, err := New(Options{Capability: theme.Capability{Unicode: true}, transport: srv.Client().Transport, minHostInterval: -1, offline: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	sm := a.settings
	src := labSource(srv.Server)
	v := sentinelSession // a short name keeps the secret scan's assignment rule quiet
	src.Cookie = v

	if err := sm.SaveSources([]config.Indexer{src}); err != nil {
		t.Fatalf("SaveSources: %v", err)
	}

	if err := sm.TestSource(context.Background(), src); err != nil {
		t.Fatalf("TestSource: %v", err)
	}

	if _, _, err := a.Registry().SearchAll(context.Background(), indexer.Query{Text: "corpus"}, "lab"); err != nil {
		t.Fatalf("SearchAll: %v", err)
	}

	logPath := filepath.Join(a.Loaded().Paths.StateDir, "tortui.log")

	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	log := string(data)
	if !strings.Contains(log, "settings: sources saved") || !strings.Contains(log, "settings: source tested") {
		t.Fatalf("the log has no settings entries, so this test checks nothing:\n%s", log)
	}

	for _, secret := range []string{sentinelKey, sentinelSession} {
		if strings.Contains(log, secret) {
			t.Errorf("the log contains the credential %q", secret)
		}
	}

	if srv.requests() == 0 {
		t.Error("the source was never queried, so its credentials never had a chance to leak")
	}
}
