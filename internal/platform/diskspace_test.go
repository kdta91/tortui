package platform

import (
	"os"
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

// TestFilesystemIDIsSharedWithinOneFilesystem is T-9143: two directories in
// one temp dir, and a path not created yet under it, are on one filesystem.
func TestFilesystemIDIsSharedWithinOneFilesystem(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")

	for _, d := range []string{a, b} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	want, err := FilesystemID(a)
	if err != nil {
		t.Fatalf("FilesystemID(a): %v", err)
	}

	if want == "" {
		t.Fatal("FilesystemID(a) is empty")
	}

	for _, p := range []string{b, filepath.Join(b, "not", "created", "yet")} {
		got, err := FilesystemID(p)
		if err != nil {
			t.Fatalf("FilesystemID(%s): %v", p, err)
		}

		if got != want {
			t.Errorf("FilesystemID(%s) = %q, want %q (the same filesystem as %s)", p, got, want, a)
		}
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
