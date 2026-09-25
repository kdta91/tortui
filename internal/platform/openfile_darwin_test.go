package platform

import (
	"path/filepath"
	"reflect"
	"testing"
)

// TestOpenFileArgvDarwin asserts the exact argv `o` would exec on macOS,
// without launching anything, for a name full of shell metacharacters: it
// must arrive as one inert argument.
func TestOpenFileArgvDarwin(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, `x; rm -rf ~ && echo $(id) | cat 'q'.iso`)
	writeFile(t, file)
	want := resolved(t, file)

	got := recordLaunches(t)
	if err := OpenFile(file, []string{root}); err != nil {
		t.Fatalf("OpenFile error = %v", err)
	}

	if args := (*got)[0].Args; !reflect.DeepEqual(args, []string{"open", want}) {
		t.Errorf("argv = %q, want %q", args, []string{"open", want})
	}
}

// TestRevealFileArgvDarwin asserts `f` execs `open -R <file>` on macOS.
func TestRevealFileArgvDarwin(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "dir", `a&b|c.iso`)
	writeFile(t, file)
	want := resolved(t, file)

	got := recordLaunches(t)
	if err := RevealFile(file, []string{root}); err != nil {
		t.Fatalf("RevealFile error = %v", err)
	}

	if args := (*got)[0].Args; !reflect.DeepEqual(args, []string{"open", "-R", want}) {
		t.Errorf("argv = %q, want %q", args, []string{"open", "-R", want})
	}
}
