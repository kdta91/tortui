//go:build integration

// This file is the T-091 integration suite's "standalone contract" half.
// See docs/running.md for prerequisites and expected runtime, and
// docs/testing-integration.md for the full picture (reachability, download,
// resume). It is never run by `make check` (AGENT.md §6.7) and is never run
// unattended by an agent (AGENT.md §12): it makes real network calls to a
// bundled source and to real BitTorrent peers, and only the owner triggers
// it, via `make test-integration` or the manual "integration" CI job.
package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/engine/anacrolix"
	"github.com/kdta91/tortui/internal/indexer"
	"github.com/kdta91/tortui/internal/indexer/scraper"
	"github.com/kdta91/tortui/internal/indexer/scraper/builtin"
)

// maxStandaloneDownloadBytes bounds which live result this test will choose
// to download, so the suite stays fast (and CI-affordable) even though the
// bundled source's catalogue is out of tortui's control and changes over
// time. It does not bound what a real user can download — that is
// unlimited — only what this test picks to prove the pipeline works.
const maxStandaloneDownloadBytes = 25 << 20 // 25 MiB

// downloadCompleteTimeout is how long TestZeroConfigStandaloneSearchAddDownload
// waits for a real BitTorrent download to finish before failing. Generous on
// purpose: DHT peer discovery for a magnet with no embedded tracker can take
// longer than a first byte usually would.
const downloadCompleteTimeout = 10 * time.Minute

// TestZeroConfigStandaloneSearchAddDownload is the executable form of the
// standalone contract (AGENT.md §1, T-091): on an empty $TORTUI_HOME, with
// no user configuration and nothing installed but tortui's own packages, it
// loads defaults, merges in the bundled source definitions exactly as a
// fresh install would, searches with no keyword (Latest), adds the smallest
// suitable result to a real engine, and drives it to completion. It then
// proves resume-across-restart: the same torrent is restored into a brand
// new, fully offline engine instance and is immediately complete from disk
// alone, with no network subsystem even enabled.
//
// This exercises every package a real run wires together (config, the
// bundled scraper definitions, the indexer registry, and the real
// anacrolix engine) without going through cmd/tortui: production wiring of
// these into main (Backlog T-950) is a separate, later task, and this test
// does not anticipate it.
func TestZeroConfigStandaloneSearchAddDownload(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TORTUI_HOME", home)

	// Step 1: zero-config load, exactly like a fresh install's first run.
	loaded, err := config.Load("")
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	if !loaded.FirstRun {
		t.Fatal("want FirstRun=true under a fresh $TORTUI_HOME")
	}

	// Pin the seed policy rather than leave config.Default's "ratio" 1.0 in
	// effect: on a tiny, well-seeded item this test's own client can satisfy
	// a 1.0 ratio and move Seeding -> Paused inside a single policy tick
	// (applyPolicyLocked), sometimes before waitForTerminalState's poll ever
	// observes Seeding. "off" makes that same Paused transition happen
	// immediately and deterministically instead of racily; waitForTerminalState
	// additionally treats Paused as terminal so either timing is accepted
	// (PR #53 review — the same "don't wait for one exact intermediate/next
	// state a background tick can skip" rule AGENT.md's T-034 note states).
	loaded.Config.SeedPolicy = "off"

	// Step 2: the bundled source definitions, merged with no user-supplied
	// ones — "no configuration" means literally none here.
	defs, err := builtin.Merge(nil)
	if err != nil {
		t.Fatalf("builtin.Merge: %v", err)
	}

	if len(defs) == 0 {
		t.Fatal("no bundled source definitions to search")
	}

	reg := indexer.NewRegistry(indexer.Config{})

	for _, def := range defs {
		a, err := scraper.New(scraper.Options{Definition: def})
		if err != nil {
			t.Fatalf("build adapter for %s: %v", def.ID, err)
		}

		if err := reg.Register(a); err != nil {
			t.Fatalf("register %s: %v", def.ID, err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Step 3: search with no keyword — Latest, exactly as a first-time user
	// would before typing anything.
	results, sourceErrs, err := reg.SearchAll(ctx, indexer.Query{Mode: indexer.ModeLatest, Limit: 50})
	if err != nil {
		t.Fatalf("SearchAll: %v", err)
	}

	for _, se := range sourceErrs {
		t.Logf("source error (informational): %s", se.Error())
	}

	if len(results) == 0 {
		t.Fatal("zero-config Latest search against the bundled sources returned nothing")
	}

	// Step 4: pick a result to download — the smallest one at or under
	// maxStandaloneDownloadBytes, so the test completes in bounded time
	// regardless of what the live catalogue currently holds.
	chosen, ok := pickSmallest(results, maxStandaloneDownloadBytes)
	if !ok {
		t.Fatalf("no result at or under %d bytes among %d candidates; every live result was larger than this test's cap", maxStandaloneDownloadBytes, len(results))
	}

	src, ok := reg.Get(chosen.IndexerID)
	if !ok {
		t.Fatalf("chosen result names indexer %q, which is not registered", chosen.IndexerID)
	}

	resolved, err := src.Resolve(ctx, chosen)
	if err != nil {
		t.Fatalf("Resolve %q: %v", chosen.Title, err)
	}

	addSrc := engine.AddSource{}

	switch {
	case resolved.Magnet != "":
		addSrc.Magnet = resolved.Magnet
	case resolved.TorrentURL != "":
		addSrc.TorrentURL = resolved.TorrentURL
	default:
		t.Fatalf("resolved result %q has neither a magnet nor a torrent URL", resolved.Title)
	}

	t.Logf("downloading %q (%d bytes) from %s", resolved.Title, resolved.SizeBytes, resolved.IndexerID)

	// Step 5: a real engine, real network, downloading to a directory
	// under $TORTUI_HOME — nothing touches the caller's real config/state.
	eng, err := anacrolix.New(anacrolix.Options{Config: loaded.Config})
	if err != nil {
		t.Fatalf("anacrolix.New: %v", err)
	}

	closed := false
	defer func() {
		if !closed {
			_ = eng.Close()
		}
	}()

	addCtx, addCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer addCancel()

	id, err := eng.Add(addCtx, addSrc)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	status := waitForTerminalState(t, eng, id, downloadCompleteTimeout)
	assertDownloadComplete(t, status, downloadCompleteTimeout)

	assertFilesExistUnder(t, status.SavePath, status.Name)

	// Step 6: resume across a restart. Save what a real session would
	// persist, close this engine, and restore into a brand new one that
	// has every network subsystem disabled (Offline) — if the restored
	// torrent still shows complete without a single network call being
	// possible, the data was verified from disk alone, not re-downloaded.
	resumer, ok := any(eng).(engine.Resumer)
	if !ok {
		t.Fatal("anacrolix.Engine does not implement engine.Resumer")
	}

	resumeData, err := resumer.ResumeData(id)
	if err != nil {
		t.Fatalf("ResumeData: %v", err)
	}

	if err := eng.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	closed = true

	eng2, err := anacrolix.New(anacrolix.Options{Config: loaded.Config, Offline: true})
	if err != nil {
		t.Fatalf("anacrolix.New (offline restore): %v", err)
	}
	defer func() { _ = eng2.Close() }()

	resumer2, ok := any(eng2).(engine.Resumer)
	if !ok {
		t.Fatal("restored anacrolix.Engine does not implement engine.Resumer")
	}

	restoreCtx, restoreCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer restoreCancel()

	restoredID, err := resumer2.Restore(restoreCtx, resumeData)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	restoredStatus := waitForTerminalState(t, eng2, restoredID, 30*time.Second)
	assertDownloadComplete(t, restoredStatus, 30*time.Second)
}

// assertDownloadComplete fails the test unless status is a successfully
// completed download: no Err, and Progress == 1.0. It accepts either
// StateSeeding or StatePaused as the terminal state — see
// waitForTerminalState's doc comment for why both are valid outcomes of a
// real completed download, not just Seeding.
func assertDownloadComplete(t *testing.T, status engine.TorrentStatus, timeout time.Duration) {
	t.Helper()

	if status.State == engine.StateErrored {
		t.Fatalf("download errored: %v", status.Err)
	}

	if status.Err != nil {
		t.Fatalf("download in state %v carries an unexpected error: %v", status.State, status.Err)
	}

	if status.State != engine.StateSeeding && status.State != engine.StatePaused {
		t.Fatalf("download did not reach a terminal state within %s: last state %v, progress %.4f", timeout, status.State, status.Progress)
	}

	if status.Progress != 1.0 {
		t.Fatalf("terminal state %v with progress %.4f, want 1.0", status.State, status.Progress)
	}
}

// pickSmallest returns the result with the smallest SizeBytes at or under
// cap. ok is false when no result in results has a usable, in-budget size —
// there is deliberately no fallback to a result over cap: the cap exists to
// keep this test's runtime bounded, and silently ignoring it would make that
// bound a fiction (PR #53 review).
//
// It picks the smallest in-budget live result rather than a specific,
// pinned identifier because pinning one would mean inventing knowledge of
// the bundled source's catalogue this repository has no other reason to
// have (AGENT.md §2's spirit: verify behaviour against the source's own
// documented API, never guess or hardcode its content). A specific,
// verified-durable item could still be a worthwhile follow-up — see
// Backlog T-989.
func pickSmallest(results []indexer.Result, cap int64) (chosen indexer.Result, ok bool) {
	have := false

	for _, r := range results {
		if r.SizeBytes <= 0 || r.SizeBytes > cap {
			continue
		}

		if !have || r.SizeBytes < chosen.SizeBytes {
			chosen = r
			have = true
		}
	}

	return chosen, have
}

// waitForTerminalState polls eng.List() for id until it reaches StateSeeding,
// StatePaused, or StateErrored, or timeout elapses.
//
// Seeding and Paused are both "download finished" outcomes here, not just
// Seeding: applyPolicyLocked can move a completed torrent from Seeding to
// Paused inside the very next policy tick once the configured seed policy is
// satisfied (T-091 pins seed_policy to "off", which is satisfied
// immediately on completion), and this poll has no way to observe the
// instant in between. Waiting on Seeding alone would be exactly the race
// AGENT.md's T-034 note warns about — a background loop skipping past the
// one state a test insists on. assertDownloadComplete is what actually
// checks the outcome (Progress == 1.0, no Err) once a terminal state is
// reached, regardless of which of the two it was.
//
// It never waits for one exact intermediate state (StateDownloading,
// StateChecking) either: those can be skipped entirely for data already
// partially or fully verified.
func waitForTerminalState(t *testing.T, eng engine.Engine, id string, timeout time.Duration) engine.TorrentStatus {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for {
		for _, s := range eng.List() {
			if s.ID != id {
				continue
			}

			if s.State == engine.StateSeeding || s.State == engine.StatePaused || s.State == engine.StateErrored {
				return s
			}
		}

		if time.Now().After(deadline) {
			t.Fatalf("torrent %q did not reach a terminal state within %s", id, timeout)
		}

		time.Sleep(500 * time.Millisecond)
	}
}

// assertFilesExistUnder fails the test if name (the torrent's declared
// name) is missing from disk under savePath after a download reports
// StateSeeding.
func assertFilesExistUnder(t *testing.T, savePath, name string) {
	t.Helper()

	if savePath == "" {
		t.Fatal("completed torrent has no SavePath")
	}

	target := filepath.Join(savePath, name)

	if _, err := os.Stat(target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Fatalf("completed torrent's data is missing on disk at %s", target)
		}

		t.Fatalf("stat %s: %v", target, err)
	}
}
