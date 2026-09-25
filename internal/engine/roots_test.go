package engine

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestCheckDestinationRoot(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	got, err := CheckDestinationRoot(dir + string(filepath.Separator) + ".")
	if err != nil {
		t.Fatalf("CheckDestinationRoot(%q): %v", dir, err)
	}

	if got != filepath.Clean(dir) {
		t.Errorf("CheckDestinationRoot = %q, want %q", got, filepath.Clean(dir))
	}

	volumeRoot := filepath.VolumeName(dir) + string(filepath.Separator)

	for _, bad := range []string{"", "rel", volumeRoot, dir + "\x00x"} {
		if _, err := CheckDestinationRoot(bad); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("CheckDestinationRoot(%q) = %v, want ErrUnsafePath", bad, err)
		}
	}
}
