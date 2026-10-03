package anacrolix

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/anacrolix/torrent"

	"github.com/kdta91/tortui/internal/engine"
)

// ratioPolicy makes a peer engine seed to ratio 1.0, the default.
func ratioPolicy(o *Options) {
	o.Config.SeedPolicy, o.Config.SeedRatio = "ratio", 1.0
}

// seedStopped reports whether the seed policy has stopped id: paused, and
// saved as stopped.
func seedStopped(t *testing.T, e *Engine, id string) bool {
	t.Helper()

	if statusOf(t, e, id).State != engine.StatePaused {
		return false
	}

	d, err := e.ResumeData(id)
	if err != nil {
		t.Fatalf("ResumeData(%s): %v", id, err)
	}

	return d.SeedDone
}

// TestSeedPolicyStopSurvivesARestart is PR #101 review finding 3: a torrent
// the ratio policy stopped is saved stopped, with what it uploaded, and comes
// back stopped after a restart, its upload held from the moment it joins the
// client, so a new peer gets nothing. Without that it would seed another
// full ratio each session, and the README promises it never seeds forever.
func TestSeedPolicyStopSurvivesARestart(t *testing.T) {
	t.Parallel()

	first := peerEngine(t, ratioPolicy)
	dir := first.downloadDir
	file := completeTorrent(t, dir, "ratio-restart", 5*testPieceLength+3)

	id, err := first.Add(context.Background(), engine.AddSource{FilePath: file})
	if err != nil {
		t.Fatalf("Add(seeder): %v", err)
	}

	waitForState(t, first, id, engine.StateSeeding)

	leecher := peerEngine(t, nil)

	got, err := leecher.Add(context.Background(), engine.AddSource{FilePath: file})
	if err != nil {
		t.Fatalf("Add(leecher): %v", err)
	}

	connectTo(t, leecher, got, first)
	waitUntil(t, "the ratio policy to stop the seeder", func() bool { return seedStopped(t, first, id) })

	d, err := first.ResumeData(id)
	if err != nil {
		t.Fatalf("ResumeData: %v", err)
	}

	if size := statusOf(t, first, id).TotalBytes; d.Uploaded < size || d.CompletedAt.IsZero() || d.Paused {
		t.Fatalf("saved SeedDone %v, Uploaded %d, CompletedAt %v, Paused %v; want SeedDone, at least %d "+
			"bytes uploaded, a completion time, no user pause", d.SeedDone, d.Uploaded, d.CompletedAt, d.Paused, size)
	}

	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	seeding := make(chan bool, 1)

	second := peerEngine(t, func(o *Options) {
		ratioPolicy(o)
		o.Config.DownloadDir = dir
		o.onAttach = func(stage attachStage, tt *torrent.Torrent) {
			if stage == stagePublished {
				seeding <- tt.Seeding()
			}
		}
	})

	restored, err := second.Restore(context.Background(), d)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if <-seeding {
		t.Fatal("a torrent the seed policy stopped joined the client seeding after a restart")
	}

	if !seedStopped(t, second, restored) {
		t.Fatalf("restored state = %s, want stopped by the seed policy", statusOf(t, second, restored).State)
	}

	fresh := peerEngine(t, nil)

	again, err := fresh.Add(context.Background(), engine.AddSource{FilePath: file})
	if err != nil {
		t.Fatalf("Add(fresh): %v", err)
	}

	connectTo(t, fresh, again, second)
	assertNothingDownloaded(t, fresh, again, 300*time.Millisecond)

	if d2, err := second.ResumeData(restored); err != nil || d2.Uploaded != d.Uploaded || !d2.CompletedAt.Equal(d.CompletedAt) {
		t.Fatalf("ResumeData after the restart: Uploaded %d, CompletedAt %v, err %v; want %d and %v kept",
			d2.Uploaded, d2.CompletedAt, err, d.Uploaded, d.CompletedAt)
	}
}

// restoreComplete restores, into e, a torrent whose data is complete on disk,
// carrying the seed progress in d's fields.
func restoreComplete(t *testing.T, e *Engine, name string, progress engine.ResumeData) string {
	t.Helper()

	mi, err := os.ReadFile(completeTorrent(t, e.downloadDir, name, 2*testPieceLength))
	if err != nil {
		t.Fatalf("read torrent: %v", err)
	}

	progress.ID, progress.Name, progress.Metainfo, progress.SavePath = name, name, mi, e.downloadDir

	id, err := e.Restore(context.Background(), progress)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	return id
}

// TestSeedPolicyCountsAcrossARestart is PR #101 review finding 3: the ratio
// policy counts what was uploaded in earlier sessions, and the duration
// policy counts from when the data first completed, so a restart does not
// start either afresh. A record saved before the fields existed does start
// afresh.
func TestSeedPolicyCountsAcrossARestart(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		policy   func(*Options)
		progress engine.ResumeData
		stops    bool
	}{
		{
			name: "ratio met in an earlier session", policy: ratioPolicy,
			progress: engine.ResumeData{Uploaded: 2 * testPieceLength}, stops: true,
		},
		{name: "duration over since the first completion", policy: func(o *Options) {
			o.Config.SeedPolicy, o.Config.SeedDuration = "duration", "24h"
		}, progress: engine.ResumeData{CompletedAt: time.Now().Add(-25 * time.Hour)}, stops: true},
		{name: "record without the fields", policy: ratioPolicy},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			e := newTestEngine(t, func(o *Options) {
				o.MetadataTimeout = time.Hour
				tc.policy(o)
			})

			id := restoreComplete(t, e, "count-restart", tc.progress)

			if !tc.stops {
				waitForState(t, e, id, engine.StateSeeding)

				if seedStopped(t, e, id) {
					t.Fatal("a record without seed progress was stopped at once")
				}

				return
			}

			waitUntil(t, "the seed policy to stop the restored torrent", func() bool { return seedStopped(t, e, id) })
		})
	}
}
