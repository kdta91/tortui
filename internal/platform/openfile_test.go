package platform

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// recordLaunches swaps runCommandFunc for a recorder for the rest of the
// test, returning a pointer to every command that would have run. Nothing is
// ever actually launched.
func recordLaunches(t *testing.T) *[]*exec.Cmd {
	t.Helper()

	orig := runCommandFunc
	t.Cleanup(func() { runCommandFunc = orig })

	var got []*exec.Cmd
	runCommandFunc = func(cmd *exec.Cmd) error {
		got = append(got, cmd)
		return nil
	}

	return &got
}

// forbidLaunches swaps runCommandFunc for a guard that fails the test the
// moment any command reaches it — the proof that a refused path is refused
// before a real process could start, not merely reported afterwards.
func forbidLaunches(t *testing.T) {
	t.Helper()

	orig := runCommandFunc
	t.Cleanup(func() { runCommandFunc = orig })

	runCommandFunc = func(cmd *exec.Cmd) error {
		t.Fatalf("runCommandFunc called with %v — a refused path reached the launcher", cmd.Args)
		return nil
	}
}

// writeFile creates path (and its parents) with a little content.
func writeFile(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// symlinkOrSkip creates link → target, skipping the test on Windows when
// the account lacks the symlink privilege (AGENT.md §14).
func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()

	if err := os.Symlink(target, link); err != nil {
		if IsWindows() {
			t.Skipf("creating a symlink needs a privilege this environment lacks: %v", err)
		}

		t.Fatalf("Symlink: %v", err)
	}
}

func resolved(t *testing.T, p string) string {
	t.Helper()

	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", p, err)
	}

	return r
}

func TestOpenAndRevealLaunchForAFileInsideARoot(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "ubuntu", "disc.iso")
	writeFile(t, file)

	for name, fn := range map[string]func(string, []string) error{"OpenFile": OpenFile, "RevealFile": RevealFile} {
		got := recordLaunches(t)

		if err := fn(file, []string{root}); err != nil {
			t.Fatalf("%s(inside root) error = %v", name, err)
		}

		if len(*got) != 1 {
			t.Fatalf("%s launched %d commands, want 1", name, len(*got))
		}
	}
}

// TestContainmentChecksEveryKnownRoot: destinations are per-torrent, so the
// file only has to sit inside one of the roots, not the first one
// (AGENT.md §6.12).
func TestContainmentChecksEveryKnownRoot(t *testing.T) {
	defaultRoot := t.TempDir()
	savedRoot := t.TempDir()
	file := filepath.Join(savedRoot, "set", "data.bin")
	writeFile(t, file)

	got := recordLaunches(t)

	if err := OpenFile(file, []string{defaultRoot, savedRoot}); err != nil {
		t.Fatalf("OpenFile(file in second root) error = %v", err)
	}

	if len(*got) != 1 {
		t.Fatalf("launched %d commands, want 1", len(*got))
	}
}

func TestOpenAndRevealRefuseAFileOutsideEveryRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	writeFile(t, outside)

	forbidLaunches(t)

	for name, fn := range map[string]func(string, []string) error{"OpenFile": OpenFile, "RevealFile": RevealFile} {
		err := fn(outside, []string{root})
		if !errors.Is(err, ErrOutsideRoots) {
			t.Errorf("%s(outside) error = %v, want ErrOutsideRoots", name, err)
		}
	}
}

// TestRefusesDotDotEscape: a lexical ".." out of the root is caught after
// cleaning, not trusted because the string starts with the root.
func TestRefusesDotDotEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "downloads")
	writeFile(t, filepath.Join(root, "keep"))
	writeFile(t, filepath.Join(base, "outside.txt"))

	forbidLaunches(t)

	err := OpenFile(filepath.Join(root, "..", "outside.txt"), []string{root})
	if !errors.Is(err, ErrOutsideRoots) {
		t.Fatalf("OpenFile(root/../outside) error = %v, want ErrOutsideRoots", err)
	}
}

// TestRefusesSiblingSharingTheRootPrefix: /x/downloads-evil is not inside
// /x/downloads — the mistake a plain prefix comparison makes.
func TestRefusesSiblingSharingTheRootPrefix(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "downloads")
	writeFile(t, filepath.Join(root, "keep"))
	evil := filepath.Join(base, "downloads-evil", "f.txt")
	writeFile(t, evil)

	forbidLaunches(t)

	if err := OpenFile(evil, []string{root}); !errors.Is(err, ErrOutsideRoots) {
		t.Fatalf("OpenFile(prefix sibling) error = %v, want ErrOutsideRoots", err)
	}
}

// TestRefusesTheRootItself: the launch target is a torrent's own file,
// never a whole destination root.
func TestRefusesTheRootItself(t *testing.T) {
	root := t.TempDir()

	forbidLaunches(t)

	if err := RevealFile(root, []string{root}); !errors.Is(err, ErrOutsideRoots) {
		t.Fatalf("RevealFile(root) error = %v, want ErrOutsideRoots", err)
	}
}

// TestRefusesASymlinkInsideTheRootThatPointsOutside is the symlink check:
// the path looks contained, but what it actually opens is not.
func TestRefusesASymlinkInsideTheRootThatPointsOutside(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	writeFile(t, outside)

	link := filepath.Join(root, "innocent.iso")
	symlinkOrSkip(t, outside, link)

	forbidLaunches(t)

	for name, fn := range map[string]func(string, []string) error{"OpenFile": OpenFile, "RevealFile": RevealFile} {
		if err := fn(link, []string{root}); !errors.Is(err, ErrOutsideRoots) {
			t.Errorf("%s(symlink escaping root) error = %v, want ErrOutsideRoots", name, err)
		}
	}
}

// TestRefusesASymlinkedDirectoryComponentPointingOutside: the escape is an
// intermediate directory, not the leaf.
func TestRefusesASymlinkedDirectoryComponentPointingOutside(t *testing.T) {
	root := t.TempDir()
	outsideDir := t.TempDir()
	writeFile(t, filepath.Join(outsideDir, "secret.txt"))

	symlinkOrSkip(t, outsideDir, filepath.Join(root, "torrent"))

	forbidLaunches(t)

	err := OpenFile(filepath.Join(root, "torrent", "secret.txt"), []string{root})
	if !errors.Is(err, ErrOutsideRoots) {
		t.Fatalf("OpenFile(through symlinked dir) error = %v, want ErrOutsideRoots", err)
	}
}

// TestSymlinkedRootStillContainsItsFiles: a download directory that is
// itself a symlink (to another drive, say) is resolved too, so its own
// files are still recognised as inside it — and the launcher receives the
// resolved path, not the one that was checked by name.
func TestSymlinkedRootStillContainsItsFiles(t *testing.T) {
	real := t.TempDir()
	file := filepath.Join(real, "a.iso")
	writeFile(t, file)

	link := filepath.Join(t.TempDir(), "downloads")
	symlinkOrSkip(t, real, link)

	got := recordLaunches(t)

	if err := OpenFile(filepath.Join(link, "a.iso"), []string{link}); err != nil {
		t.Fatalf("OpenFile(file under symlinked root) error = %v", err)
	}

	if len(*got) != 1 {
		t.Fatalf("launched %d commands, want 1", len(*got))
	}

	args := (*got)[0].Args
	if last := args[len(args)-1]; last != resolved(t, file) && last != filepath.Dir(resolved(t, file)) {
		t.Errorf("launcher got %q, want the resolved path %q (or its folder)", last, resolved(t, file))
	}
}

func TestRefusesMalformedPaths(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.iso"))

	forbidLaunches(t)

	cases := map[string]string{
		"empty":       "",
		"relative":    "a.iso",
		"nul byte":    filepath.Join(root, "a.iso\x00.txt"),
		"newline":     filepath.Join(root, "a\n.iso"),
		"nonexistent": filepath.Join(root, "missing.iso"),
	}

	for name, p := range cases {
		err := OpenFile(p, []string{root})
		if !errors.Is(err, ErrUnsafeOpenPath) {
			t.Errorf("%s: OpenFile(%q) error = %v, want ErrUnsafeOpenPath", name, p, err)
		}
	}
}

// TestNoRootsMeansNothingIsContained: an empty or unusable root set refuses
// everything rather than allowing everything.
func TestNoRootsMeansNothingIsContained(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "a.iso")
	writeFile(t, file)

	forbidLaunches(t)

	for _, roots := range [][]string{nil, {""}, {"relative/root"}, {filepath.Join(root, "does-not-exist")}} {
		if err := OpenFile(file, roots); !errors.Is(err, ErrOutsideRoots) {
			t.Errorf("OpenFile(roots=%q) error = %v, want ErrOutsideRoots", roots, err)
		}
	}
}

// TestShellMetacharactersPassThroughInertly: a file name full of shell
// syntax reaches the launcher as one literal argv element — exec.Command
// with an argument slice, no shell anywhere in between. The per-OS tests
// assert the exact argv; this one proves the name survives the containment
// check untouched and lands as the final argument.
func TestShellMetacharactersPassThroughInertly(t *testing.T) {
	root := t.TempDir()
	// Characters every supported filesystem accepts in a name; `"`, `*`,
	// `?`, `<`, `>` and `|` are invalid on Windows and so left out.
	name := "a;b&c$(rm -rf ~)`id`'q' %PATH% ^x!.iso"
	file := filepath.Join(root, name)
	writeFile(t, file)

	got := recordLaunches(t)

	if err := OpenFile(file, []string{root}); err != nil {
		t.Fatalf("OpenFile(metacharacter name) error = %v", err)
	}

	args := (*got)[0].Args
	if last := args[len(args)-1]; filepath.Base(last) != name {
		t.Errorf("final argv element = %q, want the literal file name %q", last, name)
	}
}

// TestLauncherFailureIsWrapped: a launcher that cannot start is reported,
// not swallowed (AGENT.md §6.9).
func TestLauncherFailureIsWrapped(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "a.iso")
	writeFile(t, file)

	orig := runCommandFunc
	t.Cleanup(func() { runCommandFunc = orig })

	boom := errors.New("boom")
	runCommandFunc = func(*exec.Cmd) error { return boom }

	for name, fn := range map[string]func(string, []string) error{"OpenFile": OpenFile, "RevealFile": RevealFile} {
		if err := fn(file, []string{root}); !errors.Is(err, boom) {
			t.Errorf("%s error = %v, want it to wrap the launcher failure", name, err)
		}
	}
}
