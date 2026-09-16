package fake

import (
	"time"

	"github.com/kdta91/tortui/internal/engine"
)

// Event is one snapshot in a Script: the TorrentStatus fields to apply once
// a torrent's simulated run time reaches At. Fields left at their zero value
// are applied as zero — a Script is a full replacement of the status fields
// it lists, not a delta.
type Event struct {
	// At is the simulated run time — elapsed time since the torrent was
	// added, excluding any time spent paused — at which this event takes
	// effect.
	At time.Duration

	// State is the lifecycle state to apply. See engine.State.
	State engine.State

	// Progress, DownloadedBytes, TotalBytes, DownRate, UpRate, Peers, and
	// Seeds mirror the identically named engine.TorrentStatus fields.
	Progress        float64
	DownloadedBytes int64
	TotalBytes      int64
	DownRate        int64
	UpRate          int64
	Peers           int
	Seeds           int

	// ETA mirrors engine.TorrentStatus.ETA: -1 when unknown.
	ETA time.Duration

	// Err is the error to apply. It is only meaningful when State is
	// engine.StateErrored; every preset in this package leaves it nil
	// otherwise.
	Err error
}

// Script is an ordered timeline of Events applied to one torrent as
// Engine.Advance moves its simulated run time forward. Advance applies the
// event with the greatest At that is still <= the torrent's run time, so a
// Script is a step function — it need not be sorted, and a run time before
// every event's At leaves the torrent at whatever status Add gave it.
//
// A nil or empty Script leaves a torrent at its Add-time status forever;
// Advance never changes it.
type Script []Event

// AtOrBefore returns the event in s with the greatest At that is <= elapsed,
// and true. It returns the zero Event and false when s is empty or every
// event's At is greater than elapsed.
func (s Script) AtOrBefore(elapsed time.Duration) (Event, bool) {
	var (
		found Event
		ok    bool
	)
	for _, e := range s {
		if e.At > elapsed {
			continue
		}
		if !ok || e.At > found.At {
			found = e
			ok = true
		}
	}
	return found, ok
}

// Downloading returns a Script that checks, then progresses linearly from 0%
// to 100% over total in four even increments, then sits at StateSeeding. It
// models a plausible, uneventful download and is the default Script a new
// torrent gets from Engine when the caller supplies none.
func Downloading(total time.Duration) Script {
	const (
		steps      = 5
		totalBytes = 5_000_000_000 // a plausible ISO-sized download
		downRate   = 1_500_000
		upRate     = 250_000
		peers      = 6
		seeds      = 3
	)

	sc := make(Script, 0, steps+1)
	sc = append(sc, Event{At: 0, State: engine.StateChecking, TotalBytes: totalBytes, ETA: -1})

	for i := 1; i < steps; i++ {
		at := total * time.Duration(i) / steps
		progress := float64(i) / float64(steps)
		sc = append(sc, Event{
			At:              at,
			State:           engine.StateDownloading,
			Progress:        progress,
			DownloadedBytes: int64(progress * float64(totalBytes)),
			TotalBytes:      totalBytes,
			DownRate:        downRate,
			Peers:           peers,
			Seeds:           seeds,
			ETA:             total - at,
		})
	}

	sc = append(sc, Event{
		At:              total,
		State:           engine.StateSeeding,
		Progress:        1,
		DownloadedBytes: totalBytes,
		TotalBytes:      totalBytes,
		UpRate:          upRate,
		Peers:           peers,
		Seeds:           seeds,
		ETA:             0,
	})

	return sc
}

// Stalled returns a Script that checks, reaches progress by at, and then
// never advances again: DownRate and UpRate drop to zero and ETA becomes
// unknown, exactly like a real torrent that lost its peers mid-download.
func Stalled(at time.Duration, progress float64) Script {
	return Script{
		{At: 0, State: engine.StateChecking, ETA: -1},
		{
			At:       at,
			State:    engine.StateDownloading,
			Progress: progress,
			Peers:    1,
			ETA:      -1,
		},
	}
}

// Errored returns a Script that checks, then fails at "at" with err. Err
// must be non-nil; every field on the applied event other than State and Err
// is left at its zero value, matching a download that never got anywhere
// before failing.
func Errored(at time.Duration, err error) Script {
	return Script{
		{At: 0, State: engine.StateChecking, ETA: -1},
		{At: at, State: engine.StateErrored, ETA: -1, Err: err},
	}
}

// Completed returns a Script that is already fully downloaded and seeding
// from the moment it is added — for exercising the downloads screen's
// completed-state rendering without waiting through a simulated download.
func Completed() Script {
	const totalBytes = 5_000_000_000

	return Script{
		{
			At:              0,
			State:           engine.StateSeeding,
			Progress:        1,
			DownloadedBytes: totalBytes,
			TotalBytes:      totalBytes,
			ETA:             0,
		},
	}
}
