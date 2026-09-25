package platform

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFreeSpaceReportsAPositiveValueForATempDir(t *testing.T) {
	t.Parallel()

	n, err := FreeSpace(t.TempDir())
	if err != nil {
		t.Fatalf("FreeSpace: %v", err)
	}

	if n == 0 {
		t.Fatal("FreeSpace = 0 for a writable temp dir, want > 0")
	}
}

func TestFreeSpaceWalksUpToAnExistingAncestor(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	want, err := FreeSpace(dir)
	if err != nil {
		t.Fatalf("FreeSpace(existing): %v", err)
	}

	got, err := FreeSpace(filepath.Join(dir, "not", "created", "yet"))
	if err != nil {
		t.Fatalf("FreeSpace(missing): %v", err)
	}

	// Other processes write to the same disk; allow drift, but the
	// answer must come from the same filesystem, not be zero or absurd.
	if got == 0 || (got > want*2 && want > 0) {
		t.Fatalf("FreeSpace(missing child) = %d, want about %d", got, want)
	}
}

func TestPathLengthAndLimitAreSane(t *testing.T) {
	t.Parallel()

	if MaxPathLength < 255 {
		t.Fatalf("MaxPathLength = %d, implausibly small", MaxPathLength)
	}

	if got := PathLength(strings.Repeat("a", 10)); got != 10 {
		t.Fatalf("PathLength(10 ASCII) = %d, want 10", got)
	}
}
