package store

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// TestDestinationsMostRecentFirstAndSurviveReopen covers T-074's store half:
// used destinations are kept most-recent-first, a re-used one moves to the
// front without duplicating, SetPrefs cannot drop them, and they survive a
// restart so the known-roots set does too.
func TestDestinationsMostRecentFirstAndSurviveReopen(t *testing.T) {
	s, path := openTest(t, time.Hour)

	for _, d := range []string{"/a", "/b", "/c", "/a"} {
		if err := s.TouchDestination(d); err != nil {
			t.Fatalf("TouchDestination(%q): %v", d, err)
		}
	}

	want := []string{"/a", "/c", "/b"}
	if got := s.Destinations(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Destinations() = %v, want %v", got, want)
	}

	if err := s.SetPrefs(Prefs{SortColumn: "size"}); err != nil {
		t.Fatalf("SetPrefs: %v", err)
	}

	got := s.Destinations()
	got[0] = "mutated"

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := open(path, time.Hour)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	if got := reopened.Destinations(); !reflect.DeepEqual(got, want) {
		t.Fatalf("reopened Destinations() = %v, want %v", got, want)
	}
}

func TestTouchDestinationRejectsEmptyAndClosed(t *testing.T) {
	s, _ := openTest(t, time.Hour)

	if err := s.TouchDestination("  "); !errors.Is(err, ErrEmptyDestination) {
		t.Fatalf("TouchDestination(blank) = %v, want ErrEmptyDestination", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := s.TouchDestination("/a"); !errors.Is(err, ErrClosed) {
		t.Fatalf("TouchDestination after Close = %v, want ErrClosed", err)
	}
}
