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

	if status.State == engine.StateErrored {
		t.Fatalf("download errored: %v", status.Err)
	}

	if status.State != engine.StateSeeding {
		t.Fatalf("download did not reach StateSeeding within %s: last state %v, progress %.4f", downloadCompleteTimeout, status.State, status.Progress)
	}

	if status.Progress != 1.0 {
		t.Fatalf("StateSeeding with progress %.4f, want 1.0", status.Progress)
	}

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

	if restoredStatus.State == engine.StateErrored {
		t.Fatalf("restored torrent errored (offline, so this can never be a network problem): %v", restoredStatus.Err)
	}

	if restoredStatus.Progress != 1.0 {
		t.Fatalf("restored torrent progress %.4f, want 1.0 — a fully offline engine should never need to redownload anything", restoredStatus.Progress)
	}
}

// pickSmallest returns the result with the smallest SizeBytes at or under
// cap, or, failing that, the smallest positive SizeBytes among all of
// results. ok is false only when nothing in results has a usable size.
func pickSmallest(results []indexer.Result, cap int64) (chosen indexer.Result, ok bool) {
	var underCap, smallestOverall indexer.Result
	haveUnderCap, haveAny := false, false

	for _, r := range results {
		if r.SizeBytes <= 0 {
			continue
		}

		if !haveAny || r.SizeBytes < smallestOverall.SizeBytes {
			smallestOverall = r
			haveAny = true
		}

		if r.SizeBytes <= cap && (!haveUnderCap || r.SizeBytes < underCap.SizeBytes) {
			underCap = r
			haveUnderCap = true
		}
	}

	if haveUnderCap {
		return underCap, true
	}

	return smallestOverall, haveAny
}

// waitForTerminalState polls eng.List() for id until it reaches StateSeeding
// or StateErrored — the two states a download run to completion can end in
// — or timeout elapses. It never waits for one exact intermediate state
// (StateDownloading, StateChecking): those can be skipped entirely for data
// already partially or fully verified, and polling for one exactly would be
// a race (see AGENT.md's T-034 note on this same class of bug).
func waitForTerminalState(t *testing.T, eng engine.Engine, id string, timeout time.Duration) engine.TorrentStatus {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for {
		for _, s := range eng.List() {
			if s.ID != id {
				continue
			}

			if s.State == engine.StateSeeding || s.State == engine.StateErrored {
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
