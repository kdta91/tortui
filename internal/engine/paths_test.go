package engine

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kdta91/tortui/internal/platform"
)

func TestCheckPathComponentRejectsEveryHostileCategory(t *testing.T) {
	t.Parallel()

	bad := map[string]string{
		"empty":             "",
		"dot":               ".",
		"traversal":         "..",
		"nul byte":          "a\x00b",
		"absolute posix":    "/etc",
		"absolute windows":  `C:\Windows`,
		"backslash":         `a\b`,
		"drive":             "C:",
		"stream":            "f.txt:ads",
		"trailing dot":      "name.",
		"trailing space":    "name ",
		"reserved":          "AUX",
		"reserved with ext": "lpt1.log",
		"over NAME_MAX":     strings.Repeat("x", MaxComponentBytes+1),
	}

	for name, c := range bad {
		if err := CheckPathComponent(c); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("%s: CheckPathComponent(%.20q) = %v, want ErrUnsafePath", name, c, err)
		}
	}

	for _, c := range []string{"a.iso", "Season 1", "名前.txt", "console.log", strings.Repeat("x", MaxComponentBytes)} {
		if err := CheckPathComponent(c); err != nil {
			t.Errorf("CheckPathComponent(%.20q) = %v, want nil", c, err)
		}
	}
}

func TestCheckTorrentPath(t *testing.T) {
	t.Parallel()

	dest, err := CleanAbsPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	got, err := CheckTorrentPath(dest, "example-fixture", []string{"sub", "file.bin"})
	if err != nil {
		t.Fatalf("CheckTorrentPath(ordinary) = %v", err)
	}

	if want := filepath.Join(dest, "example-fixture", "sub", "file.bin"); got != want {
		t.Fatalf("CheckTorrentPath = %q, want %q", got, want)
	}

	if _, err := CheckTorrentPath(dest, "..", nil); !errors.Is(err, ErrUnsafePath) || !strings.Contains(err.Error(), "torrent name") {
		t.Fatalf("hostile name: %v, want ErrUnsafePath naming the torrent name", err)
	}

	if _, err := CheckTorrentPath(dest, "ok", []string{"a", ".."}); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("hostile component: %v, want ErrUnsafePath", err)
	}
}

func TestCheckTorrentPathEnforcesThePlatformLengthLimit(t *testing.T) {
	t.Parallel()

	dest, err := CleanAbsPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	// Build components of 200 bytes until the whole path is one past the
	// limit; each component alone is well under NAME_MAX.
	var parts []string
	for platform.PathLength(filepath.Join(append([]string{dest, "n"}, parts...)...)) <= platform.MaxPathLength {
		parts = append(parts, strings.Repeat("p", 200))
	}

	_, err = CheckTorrentPath(dest, "n", parts)
	if !errors.Is(err, ErrUnsafePath) || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("over-long path: %v, want ErrUnsafePath naming the limit", err)
	}

	// Trim the last component so the path fits exactly at the limit.
	full := filepath.Join(append([]string{dest, "n"}, parts...)...)
	over := platform.PathLength(full) - platform.MaxPathLength
	last := parts[len(parts)-1]
	parts[len(parts)-1] = last[:len(last)-over]

	if _, err := CheckTorrentPath(dest, "n", parts); err != nil {
		t.Fatalf("path exactly at the limit: %v, want accepted", err)
	}
}

func TestCleanAbsPathAndContainedIn(t *testing.T) {
	t.Parallel()

	for _, p := range []string{"", "  ", "a\x00b"} {
		if _, err := CleanAbsPath(p); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("CleanAbsPath(%q) = %v, want ErrUnsafePath", p, err)
		}
	}

	root, err := CleanAbsPath(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if !ContainedIn(root, root) || !ContainedIn(root, filepath.Join(root, "a", "b")) {
		t.Error("ContainedIn rejected the root or a child")
	}

	if ContainedIn(root, filepath.Dir(root)) || ContainedIn(root, root+"-evil") {
		t.Error("ContainedIn accepted the parent or a prefix-sharing sibling")
	}
}
