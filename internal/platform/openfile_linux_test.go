package platform

import (
	"path/filepath"
	"reflect"
	"testing"
)

// TestOpenFileArgvLinux asserts the exact argv `o` would exec on Linux,
// without launching anything, for a name full of shell metacharacters: it
// must arrive as one inert argument.
func TestOpenFileArgvLinux(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, `x; rm -rf ~ && echo $(id) | cat 'q'.iso`)
	writeFile(t, file)
	want := resolved(t, file)

	got := recordLaunches(t)
	if err := OpenFile(file, []string{root}); err != nil {
		t.Fatalf("OpenFile error = %v", err)
	}

	if args := (*got)[0].Args; !reflect.DeepEqual(args, []string{"xdg-open", want}) {
		t.Errorf("argv = %q, want %q", args, []string{"xdg-open", want})
	}
}

// TestRevealFileArgvLinux asserts `f` execs `xdg-open <parent dir>` on
// Linux — xdg-open has no select-this-file form.
func TestRevealFileArgvLinux(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "d;ir", `a&b|c.iso`)
	writeFile(t, file)
	want := filepath.Dir(resolved(t, file))

	got := recordLaunches(t)
	if err := RevealFile(file, []string{root}); err != nil {
		t.Fatalf("RevealFile error = %v", err)
	}

	if args := (*got)[0].Args; !reflect.DeepEqual(args, []string{"xdg-open", want}) {
		t.Errorf("argv = %q, want %q", args, []string{"xdg-open", want})
	}
}
