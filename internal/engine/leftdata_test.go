package engine

import (
	"errors"
	"fmt"
	"testing"
)

// TestLeftDataErrorLeadsWithTheRemedyAndPath: the text a refused add shows
// starts with what to do, then where the data is, and it still matches
// ErrLeftData however it is wrapped (T-9127).
func TestLeftDataErrorLeadsWithTheRemedyAndPath(t *testing.T) {
	err := fmt.Errorf("add: %w", &LeftDataError{Path: "/data/old/name"})

	if !errors.Is(err, ErrLeftData) {
		t.Errorf("errors.Is(%v, ErrLeftData) = false", err)
	}

	want := "remove its errored row (x) first: left data in /data/old/name"
	if got := (&LeftDataError{Path: "/data/old/name"}).Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
