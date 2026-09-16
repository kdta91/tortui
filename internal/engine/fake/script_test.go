package fake

import (
	"errors"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/engine"
)

func TestScriptAtOrBeforeEmpty(t *testing.T) {
	var s Script
	if _, ok := s.AtOrBefore(time.Hour); ok {
		t.Fatal("AtOrBefore on empty Script returned ok = true")
	}
}

func TestScriptAtOrBeforeBeforeFirstEvent(t *testing.T) {
	s := Script{{At: 5 * time.Second, State: engine.StateDownloading}}
	if _, ok := s.AtOrBefore(time.Second); ok {
		t.Fatal("AtOrBefore before the first event returned ok = true")
	}
}

func TestScriptAtOrBeforePicksLatestNotLast(t *testing.T) {
	// Deliberately out of order: AtOrBefore must not assume the slice is
	// sorted.
	s := Script{
		{At: 10 * time.Second, State: engine.StateSeeding},
		{At: 0, State: engine.StateChecking},
		{At: 5 * time.Second, State: engine.StateDownloading},
	}

	got, ok := s.AtOrBefore(7 * time.Second)
	if !ok {
		t.Fatal("AtOrBefore(7s) returned ok = false")
	}
	if got.State != engine.StateDownloading {
		t.Errorf("AtOrBefore(7s).State = %v, want %v", got.State, engine.StateDownloading)
	}

	got, ok = s.AtOrBefore(10 * time.Second)
	if !ok || got.State != engine.StateSeeding {
		t.Errorf("AtOrBefore(10s) = %+v, ok=%v, want StateSeeding, true", got, ok)
	}
}

func TestDownloadingReachesCompletion(t *testing.T) {
	total := 10 * time.Second
	sc := Downloading(total)

	start, ok := sc.AtOrBefore(0)
	if !ok || start.State != engine.StateChecking {
		t.Fatalf("Downloading at t=0: got %+v, ok=%v, want StateChecking", start, ok)
	}

	end, ok := sc.AtOrBefore(total)
	if !ok {
		t.Fatal("Downloading at t=total: ok = false")
	}
	if end.State != engine.StateSeeding {
		t.Errorf("Downloading at t=total: State = %v, want %v", end.State, engine.StateSeeding)
	}
	if end.Progress != 1 {
		t.Errorf("Downloading at t=total: Progress = %v, want 1", end.Progress)
	}
	if end.DownloadedBytes != end.TotalBytes {
		t.Errorf("Downloading at t=total: DownloadedBytes = %d, TotalBytes = %d, want equal",
			end.DownloadedBytes, end.TotalBytes)
	}

	mid, ok := sc.AtOrBefore(total / 2)
	if !ok {
		t.Fatal("Downloading at t=total/2: ok = false")
	}
	if mid.Progress <= 0 || mid.Progress >= 1 {
		t.Errorf("Downloading at t=total/2: Progress = %v, want strictly between 0 and 1", mid.Progress)
	}
	if mid.DownRate <= 0 {
		t.Errorf("Downloading at t=total/2: DownRate = %d, want > 0", mid.DownRate)
	}
}

func TestStalledNeverAdvancesAfterAt(t *testing.T) {
	sc := Stalled(5*time.Second, 0.4)

	got, ok := sc.AtOrBefore(5 * time.Second)
	if !ok {
		t.Fatal("Stalled at t=5s: ok = false")
	}
	if got.Progress != 0.4 {
		t.Errorf("Stalled at t=5s: Progress = %v, want 0.4", got.Progress)
	}

	later, ok := sc.AtOrBefore(time.Hour)
	if !ok {
		t.Fatal("Stalled at t=1h: ok = false")
	}
	if later != got {
		t.Errorf("Stalled at t=1h: %+v, want same event as t=5s: %+v", later, got)
	}
	if later.DownRate != 0 || later.UpRate != 0 {
		t.Errorf("Stalled: DownRate=%d UpRate=%d, want both 0", later.DownRate, later.UpRate)
	}
}

func TestErroredCarriesErr(t *testing.T) {
	wantErr := errors.New("canary")
	sc := Errored(3*time.Second, wantErr)

	before, ok := sc.AtOrBefore(time.Second)
	if !ok || before.State != engine.StateChecking {
		t.Fatalf("Errored before failure: got %+v, ok=%v, want StateChecking", before, ok)
	}

	after, ok := sc.AtOrBefore(3 * time.Second)
	if !ok {
		t.Fatal("Errored at failure time: ok = false")
	}
	if after.State != engine.StateErrored {
		t.Errorf("Errored at failure time: State = %v, want %v", after.State, engine.StateErrored)
	}
	if !errors.Is(after.Err, wantErr) {
		t.Errorf("Errored at failure time: Err = %v, want %v", after.Err, wantErr)
	}
}

func TestCompletedIsSeedingFromZero(t *testing.T) {
	sc := Completed()

	got, ok := sc.AtOrBefore(0)
	if !ok {
		t.Fatal("Completed at t=0: ok = false")
	}
	if got.State != engine.StateSeeding || got.Progress != 1 {
		t.Errorf("Completed at t=0: got %+v, want StateSeeding, Progress 1", got)
	}
}
