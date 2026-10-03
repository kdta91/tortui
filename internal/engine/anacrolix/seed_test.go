package anacrolix

import (
	"context"
	"log/slog"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/engine"
)

// peerEngine is an offline test engine that still trades data with the peers
// a test hands it, over loopback only: DHT, trackers and PEX stay off, and
// nothing is announced anywhere (AGENT.md §6.7). It seeds by duration, so a
// ratio reached by chunks sent twice never stops it mid-test, and its peer
// keep-alive is short, so the library's missed writer wake-up (Backlog
// T-9137) costs half a second rather than the test's whole wait.
func peerEngine(t *testing.T, mutate func(*Options)) *Engine {
	t.Helper()

	log := &lockedBuffer{}
	t.Cleanup(func() {
		if t.Failed() {
			out := log.String()
			t.Logf("peer engine log (last 6 KiB):\n%s", out[max(0, len(out)-6<<10):])
		}
	})

	return newTestEngine(t, func(o *Options) {
		o.Logger = slog.New(slog.NewTextHandler(log, &slog.HandlerOptions{Level: slog.LevelDebug}))
		o.MetadataTimeout = time.Hour
		o.listenHost = "127.0.0.1"
		o.peers = true
		o.keepAliveTimeout = 500 * time.Millisecond
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

// waitForSeeded is waitForComplete for a download whose one peer is seeder:
// on failure it reports both sides' peer counts, so a run on another OS says
// whether they ever connected.
func waitForSeeded(t *testing.T, leecher *Engine, id string, seeder *Engine, seedID string) {
	t.Helper()

	t.Cleanup(func() {
		if t.Failed() {
			lt, st := attachedTorrent(t, leecher, id), attachedTorrent(t, seeder, seedID)
			t.Logf("leecher %+v pieces %v", lt.Stats().TorrentGauges, lt.PieceStateRuns())
			t.Logf("seeder %+v pieces %v", st.Stats().TorrentGauges, st.PieceStateRuns())

			var status strings.Builder
			leecher.client.WriteStatus(&status)
			status.WriteString("\n--- seeder ---\n")
			seeder.client.WriteStatus(&status)
			t.Logf("client status:\n%s", status.String())
		}
	})

	waitForComplete(t, leecher, id)
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
	waitForSeeded(t, leecher, id, seeder, seeding)
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
	waitForSeeded(t, after, again, seeder, id)
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

// gateAt is what onAttach saw of a torrent at one attach stage.
type gateAt struct {
	stage         attachStage
	info, seeding bool
}

// TestRestoredPausedTorrentJoinsTheClientHeld is T-9134. A completed torrent
// restored paused is in the client with no info dictionary until its
// transfers are held, and with them held from then on, before awaitInfo ever
// runs: with seeding on, a torrent with its info and its upload allowed
// seeds from that moment, so a peer could be sent data the user paused.
func TestRestoredPausedTorrentJoinsTheClientHeld(t *testing.T) {
	t.Parallel()

	seen := make(chan gateAt, 3)

	e := newTestEngine(t, func(o *Options) {
		o.MetadataTimeout = time.Hour
		o.onAttach = func(stage attachStage, tt *torrent.Torrent) {
			seen <- gateAt{stage: stage, info: tt.Info() != nil, seeding: tt.Seeding()}
		}
	})

	file := completeTorrent(t, e.downloadDir, "held-restore", 3*testPieceLength)

	id, err := e.Restore(context.Background(), restoreRecord(t, "an-60", file, e.downloadDir))
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	for want := stageAdded; want <= stagePublished; want++ {
		var g gateAt

		select {
		case g = <-seen:
		case <-time.After(5 * time.Second):
			t.Fatalf("attach never reached stage %d", want)
		}

		switch {
		case g.stage != want:
			t.Fatalf("attach stage %d, want %d", g.stage, want)
		case g.stage == stageAdded && g.info:
			t.Fatalf("restored paused torrent joined the client with its info, seeding %v; want no info yet", g.seeding)
		case g.seeding:
			t.Fatalf("restored paused torrent is seeding at attach stage %d", g.stage)
		case g.stage != stageAdded && !g.info:
			t.Fatalf("restored paused torrent has no info at attach stage %d", g.stage)
		}
	}

	if st := statusOf(t, e, id); st.State != engine.StatePaused {
		t.Fatalf("restored state = %s, want paused", st.State)
	}
}

// TestPauseWhileATorrentJoinsHoldsItsGate: a Pause that lands after attach
// read the pause, but before the engine knows the library torrent, cannot
// shut the gate itself; attach must, before awaitInfo runs, or a completed
// torrent seeds in between.
func TestPauseWhileATorrentJoinsHoldsItsGate(t *testing.T) {
	t.Parallel()

	const id = "an-1"

	var e *Engine

	paused := make(chan error, 1)
	published := make(chan bool, 1)

	e = newTestEngine(t, func(o *Options) {
		o.MetadataTimeout = time.Hour
		o.onAttach = func(stage attachStage, tt *torrent.Torrent) {
			switch stage {
			case stageJoined:
				paused <- e.Pause(id)
			case stagePublished:
				published <- tt.Seeding()
			}
		}
	})

	file := completeTorrent(t, e.downloadDir, "pause-window", 2*testPieceLength)

	got, err := e.Add(context.Background(), engine.AddSource{FilePath: file})
	if err != nil || got != id {
		t.Fatalf("Add = %q, %v; want %q", got, err, id)
	}

	if err := <-paused; err != nil {
		t.Fatalf("Pause in the window: %v", err)
	}

	if <-published {
		t.Fatal("a torrent paused as it joined the client is seeding before awaitInfo runs")
	}

	if st := statusOf(t, e, id); st.State != engine.StatePaused {
		t.Fatalf("state = %s, want paused", st.State)
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
	waitForSeeded(t, after, again, seeder, id)
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
// or the torrent shows running with its transfers held for good: downloads
// for one missing data, seeding for one complete.
func TestResumeWhileARestoredPausedTorrentJoinsLiftsItsGate(t *testing.T) {
	t.Parallel()

	for _, complete := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing data", true: "complete"}[complete], func(t *testing.T) {
			t.Parallel()

			const id = "an-80"

			var e *Engine

			resumed := make(chan error, 1)

			e = newTestEngine(t, func(o *Options) {
				o.MetadataTimeout = time.Hour
				o.onAttach = func(stage attachStage, _ *torrent.Torrent) {
					if stage == stageJoined {
						resumed <- e.Resume(id)
					}
				}
			})

			file := writeTorrentFile(t, buildInfo("resume-window", [][]string{{"w.bin"}}))
			if complete {
				file = completeTorrent(t, e.downloadDir, "resume-window", 2*testPieceLength)
			}

			if _, err := e.Restore(context.Background(), restoreRecord(t, id, file, e.downloadDir)); err != nil {
				t.Fatalf("Restore: %v", err)
			}

			if err := <-resumed; err != nil {
				t.Fatalf("Resume in the window: %v", err)
			}

			if st := statusOf(t, e, id); st.State == engine.StatePaused {
				t.Fatalf("state after Resume = %s, want it out of the pause", st.State)
			}

			tt := attachedTorrent(t, e, id)

			if complete {
				waitUntil(t, "the resumed complete torrent seeding", tt.Seeding)
				return
			}

			if !downloadGateOpen(t, tt, time.Second) {
				t.Fatal("downloads are disallowed on a torrent resumed as it joined the client")
			}
		})
	}
}
