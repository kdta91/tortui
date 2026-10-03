package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/indexer/scraper/builtin"
	"github.com/kdta91/tortui/internal/lifecycle"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// recordingTransport answers every indexer request with a 404 and records
// it, so the root runs end to end with zero network (AGENT.md §6.7).
type recordingTransport struct {
	mu   sync.Mutex
	urls []string
}

func (rt *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.mu.Lock()
	rt.urls = append(rt.urls, req.URL.Host)
	rt.mu.Unlock()

	return &http.Response{
		StatusCode: http.StatusNotFound,
		Status:     "404 Not Found",
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}, nil
}

func (rt *recordingTransport) count() int {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	return len(rt.urls)
}

// guardDefaultTransport fails the test if anything dials through
// http.DefaultTransport instead of the injected one.
func guardDefaultTransport(t *testing.T) {
	t.Helper()

	old := http.DefaultTransport
	http.DefaultTransport = &http.Transport{
		DialContext: func(_ context.Context, network, addr string) (net.Conn, error) {
			t.Errorf("unexpected network dial to %s %s", network, addr)
			return nil, errors.New("network disabled in tests")
		},
	}

	t.Cleanup(func() { http.DefaultTransport = old })
}

// sandbox points TORTUI_HOME at a fresh temp dir and returns it.
func sandbox(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	t.Setenv("TORTUI_HOME", home)
	t.Setenv(logLevelEnv, "")
	t.Setenv(logFileEnv, "")

	return home
}

const (
	logLevelEnv = "TORTUI_LOG_LEVEL"
	logFileEnv  = "TORTUI_LOG_FILE"
)

// testOptions is a zero-network Options: an offline engine and a recording
// transport for every indexer request.
func testOptions(rt *recordingTransport) Options {
	return Options{
		Capability: theme.Capability{Unicode: true},
		transport:  rt,
		offline:    true,
	}
}

func newTestApp(t *testing.T, rt *recordingTransport) *App {
	t.Helper()

	a, err := New(testOptions(rt))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return a
}

// addTestTorrent adds a magnet to a's engine; offline it stays in Checking.
func addTestTorrent(t *testing.T, a *App) string {
	t.Helper()

	id, err := a.Engine().Add(context.Background(), engine.AddSource{
		Magnet: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=t095.iso",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	return id
}

// assertRestarts proves the previous instance released the lock and saved
// its session: a fresh root starts against the same home and resumes
// exactly want torrents.
func assertRestarts(t *testing.T, rt *recordingTransport, want int) {
	t.Helper()

	a := newTestApp(t, rt)
	defer func() {
		if err := a.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	if a.Loaded().FirstRun {
		t.Fatal("restart reported FirstRun; the first run's config was not kept")
	}

	if got := a.ResumeReport().Restored; got != want {
		t.Fatalf("restart restored %d torrents, want %d", got, want)
	}
}

// TestFirstRunWritesDefaultsAndTakesTheLock: an empty TORTUI_HOME gets a
// default config, a log, and the lock; a second instance is refused while
// the first runs; a clean Close saves the session and releases the lock.
func TestFirstRunWritesDefaultsAndTakesTheLock(t *testing.T) {
	guardDefaultTransport(t)
	sandbox(t)

	rt := &recordingTransport{}
	a := newTestApp(t, rt)

	paths := a.Loaded().Paths
	if !a.Loaded().FirstRun {
		t.Fatal("FirstRun = false under an empty TORTUI_HOME")
	}

	info, err := os.Stat(paths.ConfigFile)
	if err != nil {
		t.Fatalf("default config not written: %v", err)
	}

	if perm := info.Mode().Perm(); perm&0o077 != 0 && !isWindowsPerm(perm) {
		t.Fatalf("config.toml mode = %v, want 0600", perm)
	}

	for _, name := range []string{"tortui.lock", "tortui.log", StoreFileName} {
		if _, err := os.Stat(filepath.Join(paths.StateDir, name)); err != nil {
			t.Errorf("state file %s missing: %v", name, err)
		}
	}

	second, err := New(testOptions(rt))
	if !errors.Is(err, lifecycle.ErrAlreadyRunning) {
		if second != nil {
			_ = second.Close()
		}

		t.Fatalf("second instance: err = %v, want ErrAlreadyRunning", err)
	}

	addTestTorrent(t, a)

	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := a.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	assertRestarts(t, rt, 1)

	if n := rt.count(); n != 0 {
		t.Fatalf("startup made %d indexer requests without a program running, want 0", n)
	}
}

// isWindowsPerm reports the mode Windows reports for a 0600 file, where
// POSIX permission bits do not exist.
func isWindowsPerm(perm os.FileMode) bool { return perm == 0o666 }

// TestZeroConfigRegistersTheBundledSources: with no config, every bundled
// lawful source is registered and enabled, and at least one serves Latest
// (AGENT.md §1).
func TestZeroConfigRegistersTheBundledSources(t *testing.T) {
	guardDefaultTransport(t)
	sandbox(t)

	a := newTestApp(t, &recordingTransport{})
	t.Cleanup(func() { _ = a.Close() })

	defs, err := builtin.Definitions()
	if err != nil {
		t.Fatalf("builtin.Definitions: %v", err)
	}

	enabled := make(map[string]bool)
	latest := false

	for _, ix := range a.Registry().Enabled() {
		enabled[ix.ID()] = true
		latest = latest || ix.Caps().Latest
	}

	for _, d := range defs {
		if !enabled[d.ID] {
			t.Errorf("bundled source %q is not enabled", d.ID)
		}
	}

	if !latest {
		t.Error("no enabled source serves Latest")
	}
}

// TestZeroConfigOpensOnSearchAndQueriesNothing: the first render is the
// first-run overlay, dismissing it leaves an empty Search screen, and no
// request reaches a bundled source until the user acts (T-9011).
func TestZeroConfigOpensOnSearchAndQueriesNothing(t *testing.T) {
	guardDefaultTransport(t)
	sandbox(t)

	rt := &recordingTransport{}
	a := newTestApp(t, rt)
	t.Cleanup(func() { _ = a.Close() })

	tm := teatest.NewTestModel(t, a.Model(), teatest.WithInitialTermSize(80, 24))

	waitForAny(t, tm, "Welcome to tortui")
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	waitForAny(t, tm, "enter runs Latest")

	if err := tm.Quit(); err != nil {
		t.Fatalf("Quit: %v", err)
	}

	tm.WaitFinished(t, teatest.WithFinalTimeout(3*time.Second))

	if n := rt.count(); n != 0 {
		t.Fatalf("%d request(s) reached the bundled sources at startup, want 0", n)
	}
}

func waitForAny(t *testing.T, tm *teatest.TestModel, subs ...string) {
	t.Helper()

	teatest.WaitFor(t, tm.Output(), func(bts []byte) bool {
		for _, s := range subs {
			if bytes.Contains(bts, []byte(s)) {
				return true
			}
		}

		return false
	}, teatest.WithCheckInterval(10*time.Millisecond), teatest.WithDuration(5*time.Second))
}

// runWith runs a with hook as its program hook, output discarded, and
// returns Run's error.
func runWith(t *testing.T, a *App, hook func(*tea.Program)) error {
	t.Helper()

	a.programHook = hook

	done := make(chan error, 1)

	go func() { done <- a.Run(tea.WithInput(nil), tea.WithOutput(io.Discard)) }()

	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
		return nil
	}
}

// TestRunQuitShutsDownCleanly: quitting runs the shutdown sequence — the
// session is saved and the lock released, so a restart resumes the torrent.
func TestRunQuitShutsDownCleanly(t *testing.T) {
	guardDefaultTransport(t)
	sandbox(t)

	rt := &recordingTransport{}
	a := newTestApp(t, rt)
	addTestTorrent(t, a)

	if err := runWith(t, a, func(p *tea.Program) { go p.Quit() }); err != nil {
		t.Fatalf("Run after quit: %v", err)
	}

	assertRestarts(t, rt, 1)
}

// TestRunSignalShutsDownCleanly: SIGINT/SIGTERM (the signal context being
// cancelled) is a clean exit through the same shutdown sequence.
func TestRunSignalShutsDownCleanly(t *testing.T) {
	guardDefaultTransport(t)
	sandbox(t)

	rt := &recordingTransport{}
	a := newTestApp(t, rt)
	addTestTorrent(t, a)

	ctx, cancel := context.WithCancel(context.Background())
	a.notifySignals = func() (context.Context, context.CancelFunc) { return ctx, cancel }

	if err := runWith(t, a, func(*tea.Program) { cancel() }); err != nil {
		t.Fatalf("Run after signal: %v, want a clean exit", err)
	}

	assertRestarts(t, rt, 1)
}

// TestRunPanicInTheProgramStillShutsDown: a panic inside the running
// program (a command panicking) is reported as an error, and the shutdown
// sequence still saves the session and releases the lock.
func TestRunPanicInTheProgramStillShutsDown(t *testing.T) {
	guardDefaultTransport(t)
	sandbox(t)

	rt := &recordingTransport{}
	a := newTestApp(t, rt)
	addTestTorrent(t, a)

	err := runWith(t, a, func(p *tea.Program) {
		go p.Send(tea.BatchMsg{func() tea.Msg { panic("t095 program panic") }})
	})
	if !errors.Is(err, tea.ErrProgramPanic) {
		t.Fatalf("Run = %v, want an ErrProgramPanic error", err)
	}

	assertRestarts(t, rt, 1)
}

// TestRunPanicOutsideTheProgramStillShutsDown: a panic in Run's own code is
// recovered into an error, never re-raised, after the shutdown sequence.
func TestRunPanicOutsideTheProgramStillShutsDown(t *testing.T) {
	guardDefaultTransport(t)
	sandbox(t)

	rt := &recordingTransport{}
	a := newTestApp(t, rt)
	addTestTorrent(t, a)

	err := runWith(t, a, func(*tea.Program) { panic("t095 root panic") })
	if err == nil || !strings.Contains(err.Error(), "t095 root panic") {
		t.Fatalf("Run = %v, want the recovered panic", err)
	}

	assertRestarts(t, rt, 1)
}

// TestNewRejectsABadLogLevelBeforeWritingAnything: an invalid level fails
// before config.Load, so nothing lands in TORTUI_HOME.
func TestNewRejectsABadLogLevelBeforeWritingAnything(t *testing.T) {
	home := sandbox(t)

	opts := testOptions(&recordingTransport{})
	opts.LogLevel = "loud"

	if a, err := New(opts); err == nil {
		_ = a.Close()
		t.Fatal("New accepted log level \"loud\"")
	}

	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("read home: %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf("a rejected start wrote %d entries, want 0", len(entries))
	}
}

// TestNewUndoesEarlierStepsOnFailure: a failure after the lock is taken
// (an unusable seed_policy stops the engine) releases the lock again.
func TestNewUndoesEarlierStepsOnFailure(t *testing.T) {
	guardDefaultTransport(t)
	sandbox(t)

	rt := &recordingTransport{}
	opts := testOptions(rt)
	opts.configure = func(c *config.Config) { c.SeedPolicy = "forever" }

	if a, err := New(opts); err == nil {
		_ = a.Close()
		t.Fatal("New accepted an unusable seed policy")
	}

	assertRestarts(t, rt, 0)
}

func TestGlyphCapability(t *testing.T) {
	uni := theme.Capability{Unicode: true}

	for _, tc := range []struct {
		flag, cfg, want bool
	}{
		{false, false, true},
		{true, false, false},
		{false, true, false},
		{true, true, false},
	} {
		if got := glyphCapability(uni, tc.flag, tc.cfg).Unicode; got != tc.want {
			t.Errorf("glyphCapability(flag=%v, cfg=%v).Unicode = %v, want %v", tc.flag, tc.cfg, got, tc.want)
		}
	}
}

// TestStartupNoticesNameWhatStartupFound: the first render's notices say
// where a first run wrote config, count config problems, pass on a store
// recovery, count resumed torrents with missing data or that failed, and name
// where a dropped duplicate record's data is left unmanaged (T-9127).
func TestStartupNoticesNameWhatStartupFound(t *testing.T) {
	loaded := config.LoadResult{
		FirstRun: true,
		Paths:    config.Paths{ConfigFile: filepath.Join("home", "config.toml")},
		Problems: []string{"a", "b"},
	}
	report := lifecycle.ResumeReport{
		Missing: []engine.TorrentStatus{{ID: "m"}},
		Failed:  []engine.TorrentStatus{{ID: "f1"}, {ID: "f2"}},
		Dropped: []lifecycle.DroppedRecord{
			{ID: "an-2", Name: "elsewhere", SavePath: filepath.Join("data", "old"), KeptAs: "an-1", Elsewhere: true},
			{ID: "an-4", SavePath: filepath.Join("data", "other"), KeptAs: "an-3", Elsewhere: true},
			{ID: "an-6", Name: "same", SavePath: filepath.Join("data", "new"), KeptAs: "an-5"},
		},
	}

	got := startupNotices(loaded, "store was unreadable; moved aside", report)
	want := []string{
		"wrote a default config to " + filepath.Join("home", "config.toml"),
		"config.toml: 2 problems — run `tortui doctor` for details",
		"store was unreadable; moved aside",
		"1 resumed download with missing data on disk — see Downloads",
		"2 resumed downloads could not be restored — see Downloads",
		"unmanaged data in " + filepath.Join("data", "old") + ": dropped duplicate record of elsewhere",
		"unmanaged data in " + filepath.Join("data", "other") + ": dropped duplicate record of an-4",
		"dropped 1 duplicate record of downloads already resumed",
	}

	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("notices =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	if n := startupNotices(config.LoadResult{}, "", lifecycle.ResumeReport{}); len(n) != 0 {
		t.Fatalf("a clean restart has notices %v, want none", n)
	}
}
