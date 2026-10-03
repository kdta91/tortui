package engine

import (
	"errors"
	"fmt"
	"testing"
)

// TestDataKeptErrorLeadsWithWhatHappenedAndThePath: a remove with data that
// kept the data says so first, then where it is, and it still matches
// ErrDataKept however it is wrapped (T-9133).
func TestDataKeptErrorLeadsWithWhatHappenedAndThePath(t *testing.T) {
	err := fmt.Errorf("remove: %w", &DataKeptError{Path: "/data/shared/name"})

	if !errors.Is(err, ErrDataKept) {
		t.Errorf("errors.Is(%v, ErrDataKept) = false", err)
	}

	want := "data kept: another download uses /data/shared/name"
	if got := (&DataKeptError{Path: "/data/shared/name"}).Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}

	// T-9135: a download that may use the data is not said to use it.
	want = "data kept: another download may use /data/shared/name"
	if got := (&DataKeptError{Path: "/data/shared/name", Maybe: true}).Error(); got != want {
		t.Errorf("Error() with Maybe = %q, want %q", got, want)
	}
}
