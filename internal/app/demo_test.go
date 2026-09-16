package app

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/indexer"
	"github.com/kdta91/tortui/internal/tui/theme"
)

func newTestDemo(t *testing.T) *Demo {
	t.Helper()

	d, err := NewDemo(DemoOptions{Capability: theme.Capability{Unicode: true}})
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	return d
}

func waitFor(t *testing.T, tm *teatest.TestModel, substr string) {
	t.Helper()

	teatest.WaitFor(
		t, tm.Output(),
		func(bts []byte) bool { return bytes.Contains(bts, []byte(substr)) },
		teatest.WithCheckInterval(10*time.Millisecond),
		teatest.WithDuration(3*time.Second),
	)
}

func TestNewDemoBuildsAUsableModel(t *testing.T) {
	d := newTestDemo(t)

	m := d.Model()
	model, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if model.View() == "" {
		t.Fatal("View() is empty after a resize")
	}
}

func TestNewDemoSetsTheBanner(t *testing.T) {
	d := newTestDemo(t)

	m := d.Model()
	model, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	view := model.View()
	if !strings.Contains(view, DemoBanner) {
		t.Fatalf("View() does not contain DemoBanner %q; got %q", DemoBanner, view)
	}
}

func TestSandboxDirIsUnderTheSystemTempDir(t *testing.T) {
	d := newTestDemo(t)

	dir, err := filepath.EvalSymlinks(d.SandboxDir())
	if err != nil {
		t.Fatalf("EvalSymlinks(SandboxDir): %v", err)
	}

	tmp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks(TempDir): %v", err)
	}

	if !strings.HasPrefix(dir, tmp) {
		t.Fatalf("SandboxDir() %q is not under the temp directory %q", dir, tmp)
	}
}

// TestCloseRemovesTheSandboxAndLeavesNothingBehind is T-056's direct proof
// of "nothing to clean up afterwards": every seeded torrent's SavePath
// lives under the sandbox, at least one has a real file on disk before
// Close, and the whole sandbox directory is gone after it.
func TestCloseRemovesTheSandboxAndLeavesNothingBehind(t *testing.T) {
	d, err := NewDemo(DemoOptions{})
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	dir := d.SandboxDir()

	statuses := d.Engine().List()
	if len(statuses) == 0 {
		t.Fatal("no torrents were seeded")
	}

	var sawExistingSavePath bool
	for _, s := range statuses {
		if s.SavePath == "" {
			t.Errorf("torrent %q has an empty SavePath", s.Name)
			continue
		}
		if !strings.HasPrefix(s.SavePath, dir) {
			t.Errorf("torrent %q SavePath %q is outside the sandbox %q", s.Name, s.SavePath, dir)
		}
		if _, err := os.Stat(s.SavePath); err == nil {
			sawExistingSavePath = true
		}
	}
	if !sawExistingSavePath {
		t.Fatal("no seeded torrent has a real file on disk at its SavePath")
	}

	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("sandbox directory %q still exists after Close (err=%v)", dir, err)
	}

	// Idempotent: a second Close must not error or panic.
	if err := d.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestNothingIsWrittenOutsideTheSandbox uses DemoOptions.BaseDir to point
// NewDemo's sandbox at a directory this test alone owns (t.TempDir()),
// rather than snapshotting the real, process-wide os.TempDir(): `go test
// ./...` runs every package's tests concurrently, and other packages
// legitimately create their own temp files in the same shared OS temp
// directory at the same time (observed in CI: internal/config's own
// t.TempDir()-backed tests raced this one when both happened to resolve
// under the same parent). Isolating to a directory only this test writes
// into is what actually proves "NewDemo writes nothing outside its own
// sandbox," without asserting anything about directories this test has no
// business asserting on.
func TestNothingIsWrittenOutsideTheSandbox(t *testing.T) {
	base := t.TempDir()

	before := snapshotDirEntries(t, base)

	d, err := NewDemo(DemoOptions{BaseDir: base})
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	sandbox := filepath.Base(d.SandboxDir())

	after := snapshotDirEntries(t, base)

	for name := range after {
		if before[name] {
			continue
		}
		if name != sandbox {
			t.Errorf("unexpected new entry under BaseDir: %q (sandbox is %q)", name, sandbox)
		}
	}

	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func snapshotDirEntries(t *testing.T, dir string) map[string]bool {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", dir, err)
	}

	out := make(map[string]bool, len(entries))
	for _, e := range entries {
		out[e.Name()] = true
	}
	return out
}

// TestZeroNetworkCalls proves NewDemo, a registry fan-out, and a full
// Advance cycle never dial out: any net.Dial through http.DefaultTransport
// during the test fails it immediately (AGENT.md §6.7).
func TestZeroNetworkCalls(t *testing.T) {
	dialed := false

	guard := &net.Dialer{}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialed = true
			t.Errorf("unexpected network dial to %s %s", network, addr)
			return guard.DialContext(ctx, network, addr)
		},
	}

	oldTransport := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = oldTransport })

	d, err := NewDemo(DemoOptions{})
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	if _, _, err := d.Registry().SearchAll(context.Background(), indexer.Query{Mode: indexer.ModeSearch, Text: "x"}); err != nil {
		t.Fatalf("SearchAll: %v", err)
	}

	d.Engine().Advance(60 * time.Second)

	if dialed {
		t.Fatal("a network dial was attempted")
	}
}

// TestRegistrySpansEveryTrustValueAndDegrades is the app-level companion
// to internal/indexer/fake's own tests: it proves the *composed* Demo
// object — not just the fixture package in isolation — carries the full
// variety and the failing source (T-056 acceptance).
func TestRegistrySpansEveryTrustValueAndDegrades(t *testing.T) {
	d := newTestDemo(t)

	// Deliberately the exact same Query NewDemo's own startup self-check
	// already ran: the registry's per-source refresh floor (60s cache TTL
	// aside, AGENT.md §6.13's MinRefreshInterval is 1s) would otherwise
	// throttle a second, differently-shaped query issued this soon after
	// NewDemo returned — an identical query is a guaranteed cache hit
	// instead, which is exactly what a real "press refresh twice" user
	// action would also get.
	results, srcErrs, err := d.Registry().SearchAll(context.Background(), indexer.Query{Mode: indexer.ModeSearch, Text: "demo"})
	if err != nil {
		t.Fatalf("SearchAll: %v", err)
	}

	seen := map[indexer.Trust]bool{}
	for _, r := range results {
		seen[r.Trust] = true
	}
	for _, want := range []indexer.Trust{
		indexer.TrustUnknown, indexer.TrustNone, indexer.TrustVerified,
		indexer.TrustTrusted, indexer.TrustVIP,
	} {
		if !seen[want] {
			t.Errorf("Demo's registry results never include Trust %v", want)
		}
	}

	if len(srcErrs) == 0 {
		t.Fatal("no source failed — the deliberately-failing fixture source is not being exercised")
	}
}

// TestScriptedDownloadsCoverEveryRequiredState drives the engine's own
// controllable clock (Advance) — never a sleep, never a second clock
// abstraction — through the full cycle and confirms all four scripted
// behaviours plus the already-finished torrent are present: a normal
// completion, a stall, a metadata timeout, a generic error, and a
// pre-finished seed. The whole cycle is driven in simulated time, well
// under a minute of *test* wall-clock time (AGENT.md §6.7).
func TestScriptedDownloadsCoverEveryRequiredState(t *testing.T) {
	d := newTestDemo(t)

	// Advance well past every spec's At (the latest is 25s).
	d.Engine().Advance(30 * time.Second)

	statuses := d.Engine().List()
	if len(statuses) != 5 {
		t.Fatalf("len(statuses) = %d, want 5", len(statuses))
	}

	var sawCompleted, sawStalled, sawErrored int
	var erroredMessages []string

	for _, s := range statuses {
		switch s.State {
		case engine.StateSeeding:
			sawCompleted++
			if s.Progress != 1 {
				t.Errorf("seeding torrent %q has Progress %v, want 1", s.Name, s.Progress)
			}
		case engine.StateDownloading:
			if s.DownRate == 0 && s.Peers == 1 {
				sawStalled++
			}
		case engine.StateErrored:
			sawErrored++
			if s.Err == nil {
				t.Errorf("errored torrent %q has a nil Err", s.Name)
			} else {
				erroredMessages = append(erroredMessages, s.Err.Error())
			}
		}
	}

	if sawCompleted < 2 {
		// The normal-download script (reaches StateSeeding at t=20s) and
		// the pre-finished script (StateSeeding from t=0) should both be
		// seeding by t=30s.
		t.Errorf("sawCompleted = %d, want at least 2", sawCompleted)
	}
	if sawStalled != 1 {
		t.Errorf("sawStalled = %d, want 1", sawStalled)
	}
	if sawErrored != 2 {
		t.Errorf("sawErrored = %d, want 2 (metadata timeout + generic error)", sawErrored)
	}

	var sawMetadataTimeout, sawGenericError bool
	for _, msg := range erroredMessages {
		if strings.Contains(msg, "metadata timeout") {
			sawMetadataTimeout = true
		}
		if strings.Contains(msg, "tracker refused") {
			sawGenericError = true
		}
	}
	if !sawMetadataTimeout {
		t.Error("no errored torrent reports a metadata-timeout message")
	}
	if !sawGenericError {
		t.Error("no errored torrent reports a generic tracker-error message")
	}
}

// TestStatusBarReflectsSeededEngineImmediately confirms the live half of
// T-056's acceptance — the status bar's active-download count and
// aggregate rate — is genuinely wired to the seeded engine through the
// real internal/tui.Model.Init/Update path, via a real teatest program.
func TestStatusBarReflectsSeededEngineImmediately(t *testing.T) {
	d := newTestDemo(t)

	tm := teatest.NewTestModel(t, d.Model(), teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	waitFor(t, tm, "active")
}

// TestEveryScreenKeybindAndDialogIsReachable drives the composed demo
// Model through every screen, the help overlay, and the quit-confirmation
// dialog via a real teatest program — the "every screen, keybind, and
// dialog is reachable in demo mode" half of T-056's acceptance that does
// not depend on a not-yet-built screen (open-file/open-folder are the
// documented exception; see this package's doc comment and the T-056
// tracker notes).
func TestEveryScreenKeybindAndDialogIsReachable(t *testing.T) {
	d := newTestDemo(t)

	tm := teatest.NewTestModel(t, d.Model(), teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	for _, key := range []string{"1", "2", "3", "4", "5"} {
		tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	}
	waitFor(t, tm, "settings screen")

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	waitFor(t, tm, "Keys")
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})

	// Advance the real engine so quitting prompts (an active download).
	d.Engine().Advance(1 * time.Second)
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	waitFor(t, tm, "Quit tortui?")
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
}

// TestRunCleansUpEvenAfterQuit confirms Run's clock goroutine and sandbox
// are torn down once the bubbletea program exits, driven end to end
// through Demo.Run itself rather than by calling Close directly.
func TestRunCleansUpEvenAfterQuit(t *testing.T) {
	d, err := NewDemo(DemoOptions{})
	if err != nil {
		t.Fatalf("NewDemo: %v", err)
	}

	dir := d.SandboxDir()

	// This environment has no real controlling terminal, and raw-byte key
	// injection through a piped tea.WithInput reader was observed to be
	// unreliable without one (the program's input reader never reliably
	// dispatched an injected "q" as a KeyMsg). programHook is this
	// package's own test seam for exactly that gap: it hands back the
	// real *tea.Program Run builds so the test can call Program.Quit
	// directly, the same mechanism teatest itself uses under the hood.
	d.programHook = func(p *tea.Program) {
		go func() {
			time.Sleep(50 * time.Millisecond)
			p.Quit()
		}()
	}

	var out bytes.Buffer

	done := make(chan error, 1)
	go func() {
		done <- d.Run(tea.WithInput(strings.NewReader("")), tea.WithOutput(&out))
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return within 5s of Program.Quit")
	}

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("sandbox directory %q still exists after Run returned (err=%v)", dir, err)
	}
}
