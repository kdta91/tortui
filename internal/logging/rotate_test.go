package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultRotationThresholds(t *testing.T) {
	if DefaultMaxSizeBytes != 10*1024*1024 {
		t.Fatalf("DefaultMaxSizeBytes = %d, want 10 MiB", DefaultMaxSizeBytes)
	}

	if DefaultMaxBackups != 3 {
		t.Fatalf("DefaultMaxBackups = %d, want 3", DefaultMaxBackups)
	}
}

// TestRotatingWriterRotatesAndRetainsBackups writes enough data to force
// several rotations at a small threshold and checks that at most
// maxBackups rotated files are kept, the oldest is dropped, and the active
// file never holds more than one write cycle's worth of data.
func TestRotatingWriterRotatesAndRetainsBackups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tortui.log")

	const (
		maxSize    = 50 // bytes; tiny so the test rotates quickly
		maxBackups = 2
	)

	w, err := newRotatingWriter(path, maxSize, maxBackups)
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}

	// Each line is well under maxSize alone, so writes accumulate before
	// rotation triggers, exercising the size-tracking path rather than
	// only the "single write already over threshold" edge case.
	line := strings.Repeat("x", 20) + "\n"
	for i := 0; i < 20; i++ {
		if _, err := w.Write([]byte(line)); err != nil {
			t.Fatalf("Write #%d: %v", i, err)
		}
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Active file plus exactly maxBackups rotated files should exist.
	mustExist := []string{path, path + ".1", path + ".2"}
	for _, p := range mustExist {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected %s to exist: %v", p, err)
		}
	}

	// Anything beyond maxBackups must have been pruned.
	stale := path + ".3"
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("expected %s to have been pruned, stat err = %v", stale, err)
	}

	// Every file on disk must be at or under the threshold — rotation
	// happens before a write would push a file over the limit, not after.
	for _, p := range mustExist {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}

		if info.Size() > maxSize {
			t.Fatalf("%s is %d bytes, want <= %d (maxSize)", p, info.Size(), maxSize)
		}
	}
}

// TestRotatingWriterClosedRejectsWrites confirms Close is idempotent and a
// write after Close fails cleanly instead of panicking or silently
// succeeding against a closed file descriptor.
func TestRotatingWriterClosedRejectsWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tortui.log")

	w, err := newRotatingWriter(path, DefaultMaxSizeBytes, DefaultMaxBackups)
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("second Close should be a no-op, got: %v", err)
	}

	if _, err := w.Write([]byte("after close")); err == nil {
		t.Fatalf("Write after Close: got nil error, want one")
	}
}

// TestRotatingWriterSurvivesReopen confirms an existing log file's size is
// picked up on construction, so restarting the process mid-file doesn't
// reset the rotation threshold.
func TestRotatingWriterSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tortui.log")

	existing := strings.Repeat("y", 40)
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatalf("seed existing log file: %v", err)
	}

	w, err := newRotatingWriter(path, 50, 1)
	if err != nil {
		t.Fatalf("newRotatingWriter: %v", err)
	}
	defer func() { _ = w.Close() }()

	// 40 existing + 20 new > 50, so this write must trigger a rotation
	// rather than appending past the threshold.
	if _, err := w.Write([]byte(strings.Repeat("z", 20))); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("expected rotation on first write given pre-existing size: %v", err)
	}
}
