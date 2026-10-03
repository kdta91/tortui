package anacrolix

import (
	"context"
	"net/netip"
	"os"
	"testing"
	"time"

	"github.com/anacrolix/torrent"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/engine"
)

// peerEngine is an offline test engine that still trades data with the peers
// a test hands it, over loopback only: DHT, trackers and PEX stay off, and
// nothing is announced anywhere (AGENT.md §6.7). It seeds by duration, so a
// ratio reached by chunks sent twice never stops it mid-test.
func peerEngine(t *testing.T, mutate func(*Options)) *Engine {
	t.Helper()

	return newTestEngine(t, func(o *Options) {
		o.MetadataTimeout = time.Hour
		o.listenHost = "127.0.0.1"
		o.peers = true
		o.Config.SeedPolicy = "duration"
		o.Config.SeedDuration = "24h"

		if mutate != nil {
			mutate(o)
		}
	})
}

// connectTo hands from's torrent id the loopback address of engine to as its
// one peer.
func connectTo(t *testing.T, from *Engine, id string, to *Engine) {
	t.Helper()

	port := to.ListenPort()
	if port == 0 {
		t.Fatal("the peer engine has no listen port")
	}

	addr := netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(port))
	attachedTorrent(t, from, id).AddPeers([]torrent.PeerInfo{{Addr: addr, Trusted: true}})
}

// TestCompletedTorrentSeedsToAPeer is PR #101 review note 2. A torrent whose
// data is complete keeps uploading under the seed policy (README, config): a
// second engine with nothing on disk, whose only peer is the seeding engine
// on loopback, downloads the whole torrent from it.
func TestCompletedTorrentSeedsToAPeer(t *testing.T) {
	t.Parallel()

	seeder := peerEngine(t, nil)
	file := completeTorrent(t, seeder.downloadDir, "seed-fixture", 5*testPieceLength+123)

	seeding, err := seeder.Add(context.Background(), engine.AddSource{FilePath: file})
	if err != nil {
		t.Fatalf("Add(seeder): %v", err)
	}

	waitForState(t, seeder, seeding, engine.StateSeeding)

	leecher := peerEngine(t, nil)

	id, err := leecher.Add(context.Background(), engine.AddSource{FilePath: file})
	if err != nil {
		t.Fatalf("Add(leecher): %v", err)
	}

	connectTo(t, leecher, id, seeder)
	waitForComplete(t, leecher, id)
}

// TestSeedPolicyOffNeverSeeds: the library uploads a completed torrent only
// when told to seed, which every policy but "off" asks for (DEC-168).
func TestSeedPolicyOffNeverSeeds(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	for policy, want := range map[string]bool{"": true, "ratio": true, "duration": true, "off": false} {
		cfg := clientConfig(Options{Config: config.Config{DownloadDir: dir, SeedPolicy: policy}}, dir, discardLogger(), 0)

		if cfg.Seed != want {
			t.Errorf("seed_policy %q: client Seed = %v, want %v", policy, cfg.Seed, want)
		}

		if !cfg.DisableAggressiveUpload {
			t.Errorf("seed_policy %q: upload while downloading is not the reciprocal kind", policy)
		}
	}
}

// TestUserPausedCompletedTorrentUploadsNothing: with seeding on, a torrent
// the user paused after it completed still uploads nothing; a Resume lets the
// peer finish from it.
func TestUserPausedCompletedTorrentUploadsNothing(t *testing.T) {
	t.Parallel()

	seeder := peerEngine(t, nil)
	file := completeTorrent(t, seeder.downloadDir, "paused-seed", 5*testPieceLength+7)

	id, err := seeder.Add(context.Background(), engine.AddSource{FilePath: file})
	if err != nil {
		t.Fatalf("Add(seeder): %v", err)
	}

	waitForState(t, seeder, id, engine.StateSeeding)

	if err := seeder.Pause(id); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	if attachedTorrent(t, seeder, id).Seeding() {
		t.Fatal("a paused completed torrent is seeding")
	}

	leecher := peerEngine(t, nil)

	got, err := leecher.Add(context.Background(), engine.AddSource{FilePath: file})
	if err != nil {
		t.Fatalf("Add(leecher): %v", err)
	}

	connectTo(t, leecher, got, seeder)
	assertNothingDownloaded(t, leecher, got, 300*time.Millisecond)

	if err := seeder.Resume(id); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	// A fresh peer: the first one's view of a peer that refused it is the
	// library's business, not the seeder's.
	after := peerEngine(t, nil)

	again, err := after.Add(context.Background(), engine.AddSource{FilePath: file})
	if err != nil {
		t.Fatalf("Add(after): %v", err)
	}

	connectTo(t, after, again, seeder)
	waitForComplete(t, after, again)
}

// assertNothingDownloaded fails if id gets a byte of data within window.
func assertNothingDownloaded(t *testing.T, e *Engine, id string, window time.Duration) {
	t.Helper()

	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		if st := statusOf(t, e, id); st.DownloadedBytes > 0 {
			t.Fatalf("%s downloaded %d bytes from a peer that must upload nothing", id, st.DownloadedBytes)
		}

		time.Sleep(10 * time.Millisecond)
	}
}

// restoreRecord is the session record of a torrent whose .torrent is at
// file, saved at dest with the user's pause.
func restoreRecord(t *testing.T, id, file, dest string) engine.ResumeData {
	t.Helper()

	mi, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read torrent: %v", err)
	}

	return engine.ResumeData{ID: id, Name: id, Metainfo: mi, SavePath: dest, Paused: true}
}

// TestRestoredPausedTorrentJoinsTheClientHeld is T-9134. A completed torrent
// restored paused is in the client with its info dictionary and its transfers
// already held, before awaitInfo ever runs: with seeding on, a torrent
// attached with its upload allowed seeds from that moment, so a peer could be
// sent data the user paused.
func TestRestoredPausedTorrentJoinsTheClientHeld(t *testing.T) {
	t.Parallel()

	type joined struct{ info, seeding bool }

	at := make(chan joined, 1)

	e := newTestEngine(t, func(o *Options) {
		o.MetadataTimeout = time.Hour
		o.afterClientAdd = func(tt *torrent.Torrent) {
			at <- joined{info: tt.Info() != nil, seeding: tt.Seeding()}
		}
	})

	file := completeTorrent(t, e.downloadDir, "held-restore", 3*testPieceLength)

	id, err := e.Restore(context.Background(), restoreRecord(t, "an-60", file, e.downloadDir))
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	select {
	case j := <-at:
		if !j.info || j.seeding {
			t.Fatalf("restored paused torrent joined the client with info %v, seeding %v; want info, not seeding",
				j.info, j.seeding)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the restored torrent never joined the client")
	}

	if st := statusOf(t, e, id); st.State != engine.StatePaused {
		t.Fatalf("restored state = %s, want paused", st.State)
	}
}

// TestRestoredPausedSeedUploadsNothingUntilResumed is T-9134 end to end: a
// completed torrent restored paused sends a connected peer nothing, and a
// Resume lets the peer finish from it.
func TestRestoredPausedSeedUploadsNothingUntilResumed(t *testing.T) {
	t.Parallel()

	seeder := peerEngine(t, nil)
	file := completeTorrent(t, seeder.downloadDir, "restored-seed", 5*testPieceLength+11)

	id, err := seeder.Restore(context.Background(), restoreRecord(t, "an-61", file, seeder.downloadDir))
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if attachedTorrent(t, seeder, id).Seeding() {
		t.Fatal("a completed torrent restored paused is seeding")
	}

	leecher := peerEngine(t, nil)

	got, err := leecher.Add(context.Background(), engine.AddSource{FilePath: file})
	if err != nil {
		t.Fatalf("Add(leecher): %v", err)
	}

	connectTo(t, leecher, got, seeder)
	assertNothingDownloaded(t, leecher, got, 300*time.Millisecond)

	if err := seeder.Resume(id); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	// A fresh peer: the first one's view of a peer that refused it is the
	// library's business, not the seeder's.
	after := peerEngine(t, nil)

	again, err := after.Add(context.Background(), engine.AddSource{FilePath: file})
	if err != nil {
		t.Fatalf("Add(after): %v", err)
	}

	connectTo(t, after, again, seeder)
	waitForComplete(t, after, again)
}

// TestResumeOfARestoredPausedTorrentOpensItsGate is PR #101 review note 5: a
// torrent restored paused, resumed while a download slot is free, starts at
// once with its gate open — downloads allowed for one still missing data,
// uploads (seeding) for one already complete.
func TestResumeOfARestoredPausedTorrentOpensItsGate(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, func(o *Options) { o.MetadataTimeout = time.Hour })
	ctx := context.Background()

	missing := restoreRecord(t, "an-70", writeTorrentFile(t, buildInfo("resume-missing", [][]string{{"m.bin"}})), e.downloadDir)
	complete := restoreRecord(t, "an-71", completeTorrent(t, e.downloadDir, "resume-complete", 2*testPieceLength), e.downloadDir)

	ids := make([]string, 0, 2)

	for _, d := range []engine.ResumeData{missing, complete} {
		id, err := e.Restore(ctx, d)
		if err != nil {
			t.Fatalf("Restore(%s): %v", d.ID, err)
		}

		// A read of data already on disk succeeds whatever the gate, so
		// the complete one is probed through its upload gate instead.
		tt := attachedTorrent(t, e, id)
		if d.ID == missing.ID && downloadGateOpen(t, tt, 100*time.Millisecond) {
			t.Fatalf("%s restored paused with its downloads allowed", id)
		}

		if tt.Seeding() {
			t.Fatalf("%s restored paused is seeding", id)
		}

		ids = append(ids, id)
	}

	for _, id := range ids {
		if err := e.Resume(id); err != nil {
			t.Fatalf("Resume(%s): %v", id, err)
		}
	}

	if q := e.Queue(); len(q) != 0 {
		t.Fatalf("Queue() = %v with slots free, want both started", q)
	}

	// Its data can never arrive (no peers, no web seed), so downloading is
	// where it stays; it shows checking until awaitInfo has run.
	waitUntil(t, ids[0]+" downloading", func() bool {
		return statusOf(t, e, ids[0]).State == engine.StateDownloading
	})

	if !downloadGateOpen(t, attachedTorrent(t, e, ids[0]), time.Second) {
		t.Error("downloads are disallowed on a restored torrent resumed with a slot free")
	}

	waitForState(t, e, ids[1], engine.StateSeeding)

	if !attachedTorrent(t, e, ids[1]).Seeding() {
		t.Error("a completed restored torrent resumed with a slot free is not seeding")
	}
}

// TestResumeWhileARestoredPausedTorrentJoinsLiftsItsGate: a Resume that lands
// after the restored paused torrent joined the client held, but before the
// engine knows its library torrent, cannot lift the gate itself; attach must,
// or the torrent shows running with its transfers held for good.
func TestResumeWhileARestoredPausedTorrentJoinsLiftsItsGate(t *testing.T) {
	t.Parallel()

	const id = "an-80"

	var e *Engine

	resumed := make(chan error, 1)

	e = newTestEngine(t, func(o *Options) {
		o.MetadataTimeout = time.Hour
		o.afterClientAdd = func(*torrent.Torrent) { resumed <- e.Resume(id) }
	})

	file := writeTorrentFile(t, buildInfo("resume-window", [][]string{{"w.bin"}}))

	if _, err := e.Restore(context.Background(), restoreRecord(t, id, file, e.downloadDir)); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if err := <-resumed; err != nil {
		t.Fatalf("Resume in the window: %v", err)
	}

	if st := statusOf(t, e, id); st.State == engine.StatePaused {
		t.Fatalf("state after Resume = %s, want it out of the pause", st.State)
	}

	if !downloadGateOpen(t, attachedTorrent(t, e, id), time.Second) {
		t.Fatal("downloads are disallowed on a torrent resumed as it joined the client")
	}
}
