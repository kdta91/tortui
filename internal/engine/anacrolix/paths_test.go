package anacrolix

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckComponentRejectsHostileNames(t *testing.T) {
	t.Parallel()

	bad := map[string]string{
		"empty":                "",
		"dot":                  ".",
		"parent":               "..",
		"nul byte":             "a\x00b",
		"posix separator":      "a/b",
		"windows separator":    `a\b`,
		"drive separator":      "C:",
		"alternate stream":     "file.txt:stream",
		"trailing dot":         "name.",
		"trailing space":       "name ",
		"reserved con":         "CON",
		"reserved con mixed":   "CoN",
		"reserved with suffix": "nul.txt",
		"reserved com1":        "com1",
		"reserved lpt9":        "LPT9.log",
	}

	for name, component := range bad {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := checkComponent(component)
			if !errors.Is(err, ErrUnsafePath) {
				t.Fatalf("checkComponent(%q) = %v, want ErrUnsafePath", component, err)
			}
		})
	}
}

func TestCheckComponentAcceptsOrdinaryNames(t *testing.T) {
	t.Parallel()

	good := []string{
		"ubuntu-24.04-desktop-amd64.iso",
		"Season 1",
		"data.tar.gz",
		"名前.txt",
		"conference-notes.md", // starts with "con" but is not the device name
		"nullable.json",
		"a.b.c",
	}

	for _, component := range good {
		if err := checkComponent(component); err != nil {
			t.Errorf("checkComponent(%q) = %v, want nil", component, err)
		}
	}
}

func TestCheckTorrentPathContainsEveryResult(t *testing.T) {
	t.Parallel()

	dest := filepath.Join(string(filepath.Separator), "downloads", "tortui")

	got, err := checkTorrentPath(dest, "release", []string{"disc1", "a.bin"})
	if err != nil {
		t.Fatalf("checkTorrentPath: %v", err)
	}

	want := filepath.Join(dest, "release", "disc1", "a.bin")
	if got != want {
		t.Errorf("checkTorrentPath = %q, want %q", got, want)
	}
}

func TestCheckTorrentPathRejectsAnEscapingName(t *testing.T) {
	t.Parallel()

	dest := filepath.Join(string(filepath.Separator), "downloads", "tortui")

	if _, err := checkTorrentPath(dest, "..", []string{"a.bin"}); !errors.Is(err, ErrUnsafePath) {
		t.Errorf("escaping torrent name = %v, want ErrUnsafePath", err)
	}

	if _, err := checkTorrentPath(dest, "release", []string{"..", "..", "a.bin"}); !errors.Is(err, ErrUnsafePath) {
		t.Errorf("escaping file path = %v, want ErrUnsafePath", err)
	}
}

func TestContainedIn(t *testing.T) {
	t.Parallel()

	sep := string(filepath.Separator)
	root := filepath.Join(sep, "downloads", "tortui")

	cases := map[string]struct {
		candidate string
		want      bool
	}{
		"the root itself":   {root, true},
		"a child":           {filepath.Join(root, "a.bin"), true},
		"a deep child":      {filepath.Join(root, "a", "b", "c.bin"), true},
		"the parent":        {filepath.Join(sep, "downloads"), false},
		"a sibling":         {filepath.Join(sep, "downloads", "elsewhere"), false},
		"a prefix sibling":  {filepath.Join(sep, "downloads", "tortui-evil"), false},
		"an unrelated tree": {filepath.Join(sep, "etc", "passwd"), false},
	}

	for name, tc := range cases {
		if got := containedIn(root, tc.candidate); got != tc.want {
			t.Errorf("%s: containedIn(%q, %q) = %t, want %t", name, root, tc.candidate, got, tc.want)
		}
	}
}

func TestCleanAbsPathRejectsEmptyAndNulPaths(t *testing.T) {
	t.Parallel()

	if _, err := cleanAbsPath("   "); !errors.Is(err, ErrUnsafePath) {
		t.Errorf("empty path = %v, want ErrUnsafePath", err)
	}

	if _, err := cleanAbsPath("downloads\x00/etc"); !errors.Is(err, ErrUnsafePath) {
		t.Errorf("NUL path = %v, want ErrUnsafePath", err)
	}

	got, err := cleanAbsPath(filepath.Join(string(filepath.Separator), "downloads", "..", "downloads", "tortui"))
	if err != nil {
		t.Fatalf("cleanAbsPath: %v", err)
	}

	if want := filepath.Join(string(filepath.Separator), "downloads", "tortui"); got != want {
		t.Errorf("cleanAbsPath = %q, want %q", got, want)
	}
}

func TestResolveDestination(t *testing.T) {
	t.Parallel()

	sep := string(filepath.Separator)
	fallback := filepath.Join(sep, "downloads", "tortui")
	saved := filepath.Join(sep, "media", "archive")
	roots := []string{fallback, saved}

	got, err := resolveDestination("", fallback, roots)
	if err != nil {
		t.Fatalf("empty save path: %v", err)
	}

	if got != fallback {
		t.Errorf("empty save path resolved to %q, want the configured default %q", got, fallback)
	}

	nested := filepath.Join(saved, "series")
	if got, err := resolveDestination(nested, fallback, roots); err != nil || got != nested {
		t.Errorf("saved destination = (%q, %v), want (%q, nil)", got, err, nested)
	}

	outside := filepath.Join(sep, "etc")
	if _, err := resolveDestination(outside, fallback, roots); !errors.Is(err, ErrOutsideRoots) {
		t.Errorf("destination outside every root = %v, want ErrOutsideRoots", err)
	}

	traversal := filepath.Join(fallback, "..", "..", "etc")
	if _, err := resolveDestination(traversal, fallback, roots); !errors.Is(err, ErrOutsideRoots) {
		t.Errorf("traversal out of a root = %v, want ErrOutsideRoots", err)
	}

	if _, err := resolveDestination("", "", nil); err == nil {
		t.Error("no save path and no default returned no error")
	}
}

func TestUnsafePathErrorsNameTheOffendingComponent(t *testing.T) {
	t.Parallel()

	err := checkComponent("CON")
	if err == nil || !strings.Contains(err.Error(), "CON") {
		t.Fatalf("checkComponent error = %v, want it to name the component", err)
	}
}

func TestDestinationRootsAlwaysIncludeTheDownloadDir(t *testing.T) {
	t.Parallel()

	sep := string(filepath.Separator)
	dir := filepath.Join(sep, "downloads", "tortui")

	roots := destinationRoots(dir, []string{filepath.Join(sep, "media"), "   "})
	if len(roots) != 2 || roots[0] != dir {
		t.Fatalf("destinationRoots = %v, want the download dir first and the unusable entry dropped", roots)
	}
}
