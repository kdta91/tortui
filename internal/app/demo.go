// Package app is tortui's composition root: the one package allowed to wire
// concrete internal/engine and internal/indexer implementations together
// with internal/tui.Model and run the resulting bubbletea program. Nothing
// in the rest of the codebase may import internal/app (AGENT.md §4) —
// cmd/tortui calls into it, and it calls back to nothing.
//
// Today it holds exactly one entry point, Demo (T-056; see AGENT.md §15):
// --demo's composition of internal/engine/fake and internal/indexer/fake
// into the real internal/tui.Model. Wiring the real engine/indexer adapters
// together for a production run is a later, still-open task's job and is
// deliberately not built here.
//
// # What --demo actually exercises today, and what it does not yet
//
// The engine half is fully live: Demo seeds a fake engine with five
// torrents whose scripted progress (T-030's controllable clock — Advance,
// driven here by a real-time goroutine, never a second clock abstraction)
// covers a normal completion, a stall, a metadata timeout, a generic error,
// and an already-finished download — and internal/tui.Model already reads
// engine.Engine.Updates() to drive the status bar's active-download count,
// aggregate rate, and the quit-confirmation prompt (all wired by T-051/
// T-052, not this task). Pressing tab/1-5/?/q and confirming quit are all
// live against this real data today.
//
// The indexer half is built and independently proven (Search spans every
// indexer.Trust value, a wide CJK title, an emoji title, a huge and a tiny
// size, and a zero-seeder entry; one of three fixture sources always fails,
// exercising the registry's "N/M sources failed" degrade path, AGENT.md
// §6.3) and is now reachable from the running --demo TUI's search screen
// (T-060): NewDemo passes Registry straight to tui.New via
// tui.WithSearcher, which *indexer.Registry satisfies without this package
// or internal/tui ever naming a concrete indexer type outside this one
// call (AGENT.md §4 — internal/tui imports indexer's frozen domain types
// and the tui.Searcher interface T-060 defines, never *indexer.Registry
// itself). Pressing `/`, moving the cursor, and dispatching a query are
// all live against this fixture data today; the results screen that would
// render a full row per hit is still T-061's job, so a completed query
// lands on Results' placeholder body for now.
//
// `o` (open file) and `f` (open folder) on the downloads screen (T-073)
// work here too: every seeded torrent's SavePath is a directory inside this
// sandbox's downloads directory, and the one that finishes immediately
// (StateSeeding from t=0) has a real, readable placeholder file at
// SavePath/<name> — exactly where a real engine writes a single-file
// torrent, and where the TUI looks for it.
package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/engine"
	fakeengine "github.com/kdta91/tortui/internal/engine/fake"
	"github.com/kdta91/tortui/internal/indexer"
	fakeindexer "github.com/kdta91/tortui/internal/indexer/fake"
	"github.com/kdta91/tortui/internal/tui"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// DemoBanner is the fixed line internal/tui.Model.Banner is set to by
// NewDemo, rendered above every screen and modal so it is unmistakable
// that what's on screen is synthetic (T-056 acceptance: "a banner makes it
// unmistakable that this is demo data").
const DemoBanner = "DEMO MODE — synthetic data, zero network, nothing written outside a temp sandbox"

// DefaultDemoTickInterval and DefaultDemoTickStep drive Demo's real-time
// clock: every DefaultDemoTickInterval of wall-clock time, the underlying
// fake engine's simulated run time advances by DefaultDemoTickStep. Every
// seeded script (see demoTorrentSpecs) resolves within well under a
// minute of *simulated* time; at these defaults that is a handful of
// seconds of wall-clock time for an interactive session — comfortably
// inside the "whole cycle runs in under a minute" acceptance criterion,
// which is what DemoTest exercises directly via Advance rather than by
// waiting on this goroutine (AGENT.md §6.7 — no test sleeps on real time).
const (
	DefaultDemoTickInterval = 200 * time.Millisecond
	DefaultDemoTickStep     = 1 * time.Second
)

// Demo-only errors used by the scripted "metadata timeout" and "error
// state" torrents. Neither names a real service (AGENT.md §2, §16); both
// match the language AGENT.md §13 itself uses for these two hazards.
var (
	errDemoMetadataTimeout = errors.New("metadata timeout: no peers responded within 60s (demo fixture)")
	errDemoTrackerRefused  = errors.New("tracker refused connection: 500 Internal Server Error (demo fixture)")
)

// Demo owns everything --demo wires up: the sandbox directory nothing
// outside it is ever written to, the scripted fake engine, the fixture
// indexer registry, and the internal/tui.Model built from them. Construct
// one with NewDemo; always call Close (Run does this for you) so the
// sandbox directory and the clock goroutine don't outlive the process.
type Demo struct {
	model    tui.Model
	engine   *fakeengine.Engine
	registry *indexer.Registry
	dir      string

	closeOnce sync.Once
	closeErr  error
	stopClock func()

	// programHook, when set, is called with the *tea.Program Run builds,
	// immediately before Run calls its Run method. It exists solely for
	// this package's own tests: reliably ending a real bubbletea program
	// from a test needs a handle to call Program.Quit on directly —
	// raw-byte key injection through a piped input reader was observed to
	// be unreliable with no real controlling terminal attached to the
	// test process. Production code (cmd/tortui) never sets this field.
	programHook func(*tea.Program)
}

// DemoOptions configures NewDemo's Theme construction. Every field is
// optional; the zero value builds the default theme at whatever
// Capability was detected (or the zero Capability, which degrades to no
// colour and ASCII glyphs — safe for a non-terminal caller such as a test).
type DemoOptions struct {
	// ThemeName selects a built-in palette (see internal/tui/theme.Names).
	// Empty selects theme.DefaultThemeName.
	ThemeName string
	// Capability is the detected terminal capability (see
	// theme.Detect). The zero value is the maximally-conservative one:
	// no colour, ASCII glyphs.
	Capability theme.Capability

	// BaseDir overrides the parent directory NewDemo creates its sandbox
	// under (os.MkdirTemp's dir argument). Empty selects the OS default
	// temp directory, exactly like os.MkdirTemp("", ...) — the only thing
	// production code (cmd/tortui) ever leaves it as. Tests set it to an
	// isolated directory (e.g. t.TempDir()) so a test that asserts on the
	// *system* temp directory's exact contents isn't racing every other
	// package's own temp-file activity when `go test ./...` runs
	// packages concurrently.
	BaseDir string
}

// NewDemo builds a Demo: a fresh temp-dir sandbox (nothing is ever written
// anywhere else), a fake engine seeded with five scripted torrents whose
// SavePaths live inside that sandbox, and a fixture indexer.Registry
// spanning every indexer.Trust value plus a deliberately-failing source —
// wired into a real internal/tui.Model with Banner set to DemoBanner. It
// makes zero network calls: engine/fake and indexer/fake never dial out,
// and the one self-check SearchAll call below only ever reaches those
// fixtures.
//
// The caller must eventually call Close (Run does this automatically) so
// the sandbox directory is removed and the demo clock goroutine, once
// started, is stopped.
func NewDemo(opts DemoOptions) (*Demo, error) {
	dir, err := os.MkdirTemp(opts.BaseDir, "tortui-demo-*")
	if err != nil {
		return nil, fmt.Errorf("app: create demo sandbox: %w", err)
	}

	downloadsDir := filepath.Join(dir, "downloads")
	if err := os.MkdirAll(downloadsDir, 0o700); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("app: create demo downloads directory: %w", err)
	}

	eng := fakeengine.New()

	if err := seedDemoTorrents(eng, downloadsDir); err != nil {
		_ = eng.Close()
		_ = os.RemoveAll(dir)
		return nil, err
	}

	reg, err := fakeindexer.NewDemoRegistry()
	if err != nil {
		_ = eng.Close()
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("app: build demo indexer registry: %w", err)
	}

	// Self-check, not a real search: proves the fixture registry actually
	// merges results across sources and degrades on the one that always
	// fails (AGENT.md §6.3), end to end, with zero network — even though
	// nothing in the TUI wires this registry in yet (see package doc).
	if _, _, err := reg.SearchAll(context.Background(), indexer.Query{Mode: indexer.ModeSearch, Text: "demo"}); err != nil {
		_ = eng.Close()
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("app: demo indexer registry self-check: %w", err)
	}

	th := theme.New(opts.ThemeName, opts.Capability)
	// WithSearcher wires the fixture registry straight into T-060's search
	// screen — *indexer.Registry satisfies tui.Searcher, the interface
	// this package doc comment and T-056's tracker notes said T-060 would
	// define, so this is the only change this file needed to make the
	// indexer half of --demo visually reachable.
	model := tui.New(eng, th, tui.WithSearcher(reg))
	model.Banner = DemoBanner

	return &Demo{
		model:    model,
		engine:   eng,
		registry: reg,
		dir:      dir,
	}, nil
}

// Model returns the internal/tui.Model wired for this demo, ready to hand
// to a bubbletea Program (Run does this) or to a teatest harness in a test.
func (d *Demo) Model() tui.Model { return d.model }

// Engine returns the concrete fake engine backing this demo. Tests use it
// to call Advance directly instead of waiting on the real-time clock Run
// starts (AGENT.md §6.7 — a test controls the clock, it doesn't sleep on
// one); it is the same *fakeengine.Engine, and Advance is the sole thing
// that ever moves its simulated time forward (see internal/engine/fake's
// package doc).
func (d *Demo) Engine() *fakeengine.Engine { return d.engine }

// Registry returns the fixture indexer.Registry this demo built. It is
// fully functional today (see this package's doc comment for what that
// means and what still depends on T-060) — exposed so a test, or a future
// task's composition, can drive it directly without duplicating the
// fixture wiring.
func (d *Demo) Registry() *indexer.Registry { return d.registry }

// SandboxDir returns the temp directory every demo file lives under. It
// exists only for tests that want to assert on its contents or its
// removal after Close.
func (d *Demo) SandboxDir() string { return d.dir }

// Run starts the demo clock (a goroutine calling Engine.Advance on a real
// ticker, per internal/engine/fake's documented --demo pattern), runs the
// bubbletea program to completion with the alternate screen enabled, then
// tears everything down via Close. extra is appended after that default so
// a caller — or a test using teatest's input/output plumbing — can override
// or add ProgramOptions; it is never required for interactive use.
func (d *Demo) Run(extra ...tea.ProgramOption) error {
	d.startClock(DefaultDemoTickInterval, DefaultDemoTickStep)
	defer func() { _ = d.Close() }()

	opts := append([]tea.ProgramOption{tea.WithAltScreen()}, extra...)

	p := tea.NewProgram(d.model, opts...)
	if d.programHook != nil {
		d.programHook(p)
	}

	_, err := p.Run()

	return err
}

// startClock launches the goroutine that is the demo's only source of
// simulated time passing: every interval of wall-clock time it calls
// d.engine.Advance(step) exactly once. It is idempotent — a second call is
// a no-op — and Close stops it.
func (d *Demo) startClock(interval, step time.Duration) {
	if d.stopClock != nil {
		return
	}

	if interval <= 0 {
		interval = DefaultDemoTickInterval
	}

	if step <= 0 {
		step = DefaultDemoTickStep
	}

	done := make(chan struct{})

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				d.engine.Advance(step)
			}
		}
	}()

	d.stopClock = func() { close(done) }
}

// Close stops the demo clock goroutine (if Run ever started one), closes
// the fake engine, and removes the sandbox directory — the "nothing to
// clean up afterwards" half of T-056's acceptance. It is idempotent and
// safe to call more than once, including without Run ever having been
// called (a test that only exercises Model()/Engine() directly still must
// call Close to remove the sandbox directory).
func (d *Demo) Close() error {
	d.closeOnce.Do(func() {
		if d.stopClock != nil {
			d.stopClock()
		}

		d.closeErr = errors.Join(d.engine.Close(), os.RemoveAll(d.dir))
	})

	return d.closeErr
}

// demoTorrentSpec is one seeded torrent: its stable key (carried in the
// magnet's own "x.demo" query parameter so ScriptFor can recover it
// without a second lookup structure), its display name, the Script driving
// it, and whether a real placeholder file should exist inside its
// SavePath from the start (T-056's open-file/open-folder demo-side support — see
// package doc).
type demoTorrentSpec struct {
	key      string
	title    string
	script   fakeengine.Script
	seedFile bool
}

// demoTorrentSpecs is the fixed set of five torrents T-056's acceptance
// criteria require: a normal completion, a stall, a metadata timeout, a
// generic error, and an already-finished download. Every script resolves
// within 25 simulated seconds, comfortably inside "the whole cycle runs in
// under a minute."
func demoTorrentSpecs() []demoTorrentSpec {
	return []demoTorrentSpec{
		{
			key:    "demo-normal",
			title:  "tortui-demo-normal-download.iso",
			script: fakeengine.Downloading(20 * time.Second),
		},
		{
			key:    "demo-stalled",
			title:  "tortui-demo-stalled-download.iso",
			script: fakeengine.Stalled(8*time.Second, 0.35),
		},
		{
			key:    "demo-metadata-timeout",
			title:  "tortui-demo-metadata-timeout.iso",
			script: fakeengine.Errored(25*time.Second, errDemoMetadataTimeout),
		},
		{
			key:    "demo-errored",
			title:  "tortui-demo-tracker-error.iso",
			script: fakeengine.Errored(5*time.Second, errDemoTrackerRefused),
		},
		{
			key:      "demo-completed",
			title:    "tortui-demo-completed-download.iso",
			script:   fakeengine.Completed(),
			seedFile: true,
		},
	}
}

// demoMagnetKey is the magnet query parameter demoTorrentSpec.key round-
// trips through, so the engine's ScriptFor callback can recover which
// script a given AddSource was seeded with without a second, parallel
// lookup keyed by something else. It is demo-only plumbing: no real magnet
// a source hands back carries this parameter, and nothing outside this
// file reads it.
const demoMagnetKey = "x.demo"

// seedDemoTorrents wires eng.ScriptFor to dispatch on demoMagnetKey and
// then adds every demoTorrentSpecs() entry, creating a real placeholder
// file at SavePath/<title> for the one spec that asks for it. All work is local:
// no network call, no write outside downloadsDir.
func seedDemoTorrents(eng *fakeengine.Engine, downloadsDir string) error {
	specs := demoTorrentSpecs()

	scripts := make(map[string]fakeengine.Script, len(specs))
	for _, s := range specs {
		scripts[s.key] = s.script
	}

	eng.ScriptFor = func(src engine.AddSource) fakeengine.Script {
		if key := demoKeyFromMagnet(src.Magnet); key != "" {
			if sc, ok := scripts[key]; ok {
				return sc
			}
		}
		// Unreached in practice — every AddSource seedDemoTorrents builds
		// carries a known key — but ScriptFor documents that it is called
		// for every Add, so a safe, harmless fallback is still required.
		return fakeengine.Downloading(30 * time.Second)
	}

	for i, s := range specs {
		savePath := filepath.Join(downloadsDir, s.title)

		if s.seedFile {
			if err := writeDemoPlaceholderFile(filepath.Join(savePath, s.title)); err != nil {
				return fmt.Errorf("app: seed demo placeholder file for %q: %w", s.key, err)
			}
		}

		magnet := demoMagnet(i, s.key, s.title)

		if _, err := eng.Add(context.Background(), engine.AddSource{Magnet: magnet, SavePath: savePath}); err != nil {
			return fmt.Errorf("app: add demo torrent %q: %w", s.key, err)
		}
	}

	return nil
}

// demoMagnet builds a syntactically valid, obviously-fake magnet URI for
// seed index i: a fixed, invented 40-hex-digit infohash derived from i (so
// each of the five is distinct but none is ever resolved against a real
// swarm — AGENT.md §6.7), a dn= display name, and the x.demo key
// seedDemoTorrents' ScriptFor reads back.
func demoMagnet(i int, key, title string) string {
	return fmt.Sprintf(
		"magnet:?xt=urn:btih:%040x&dn=%s&%s=%s",
		i+1, url.QueryEscape(title), demoMagnetKey, url.QueryEscape(key),
	)
}

// demoKeyFromMagnet recovers the x.demo query parameter demoMagnet wrote,
// or "" for a magnet that doesn't have one (e.g. if a future caller ever
// adds a torrent through this same engine without going through
// seedDemoTorrents).
func demoKeyFromMagnet(magnet string) string {
	u, err := url.Parse(magnet)
	if err != nil {
		return ""
	}

	return u.Query().Get(demoMagnetKey)
}

// demoPlaceholderContent is what writeDemoPlaceholderFile writes. It is
// real, readable file content — enough that a later `o` (open file) once
// T-073 wires it does not open a zero-byte mystery — and it says plainly
// that it isn't a real download.
const demoPlaceholderContent = "This is a tortui --demo placeholder file.\n" +
	"It is not a real download; --demo makes zero network calls.\n" +
	"Safe to delete: it lives inside a temp directory removed on exit.\n"

// writeDemoPlaceholderFile creates path (and its parent directory) with
// demoPlaceholderContent. path is always inside the sandbox directory
// NewDemo created, never anywhere else.
func writeDemoPlaceholderFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	return os.WriteFile(path, []byte(demoPlaceholderContent), 0o600)
}
