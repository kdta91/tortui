package platform

import (
	"errors"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

// TestOpenFileArgvWindows asserts the exact argv and command line `o` would
// exec on Windows, without launching anything. The name carries cmd.exe
// metacharacters and a comma (explorer's own separator); with no shell on
// the path and the argument quoted, all of it is inert.
func TestOpenFileArgvWindows(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, `x & del %USERPROFILE% ^ a,b.iso`)
	writeFile(t, file)
	want := resolved(t, file)

	got := recordLaunches(t)
	if err := OpenFile(file, []string{root}); err != nil {
		t.Fatalf("OpenFile error = %v", err)
	}

	cmd := (*got)[0]
	if !reflect.DeepEqual(cmd.Args, []string{"explorer", want}) {
		t.Errorf("argv = %q, want %q", cmd.Args, []string{"explorer", want})
	}

	if wantLine := `explorer "` + want + `"`; cmd.SysProcAttr == nil || cmd.SysProcAttr.CmdLine != wantLine {
		t.Errorf("command line = %+v, want %q", cmd.SysProcAttr, wantLine)
	}
}

// TestRevealFileArgvWindows asserts `f` execs `explorer /select,"<file>"`.
func TestRevealFileArgvWindows(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "d;ir", `a&b,c.iso`)
	writeFile(t, file)
	want := resolved(t, file)

	got := recordLaunches(t)
	if err := RevealFile(file, []string{root}); err != nil {
		t.Fatalf("RevealFile error = %v", err)
	}

	cmd := (*got)[0]
	if !reflect.DeepEqual(cmd.Args, []string{"explorer", "/select,", want}) {
		t.Errorf("argv = %q, want %q", cmd.Args, []string{"explorer", "/select,", want})
	}

	if wantLine := `explorer /select,"` + want + `"`; cmd.SysProcAttr == nil || cmd.SysProcAttr.CmdLine != wantLine {
		t.Errorf("command line = %+v, want %q", cmd.SysProcAttr, wantLine)
	}
}

// exitError returns the *exec.ExitError a real process exiting with code
// produces. cmd.exe is always present on Windows; nothing touches the
// network.
func exitError(t *testing.T, code int) error {
	t.Helper()

	err := exec.Command("cmd", "/c", "exit", strconv.Itoa(code)).Run()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != code {
		t.Fatalf("cmd /c exit %d error = %v, want an ExitError with that code", code, err)
	}

	return err
}

// TestExplorerExitOneIsSuccess: explorer.exe exits 1 even when it opened
// the target, so exit status 1 is not reported as a failure — but any other
// non-zero status, and a failure to start the process, is.
func TestExplorerExitOneIsSuccess(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "a.iso")
	writeFile(t, file)

	orig := runCommandFunc
	t.Cleanup(func() { runCommandFunc = orig })

	exitOne := exitError(t, 1)
	runCommandFunc = func(*exec.Cmd) error { return exitOne }
	if err := OpenFile(file, []string{root}); err != nil {
		t.Errorf("OpenFile with explorer exit 1 error = %v, want nil", err)
	}

	exitTwo := exitError(t, 2)
	runCommandFunc = func(*exec.Cmd) error { return exitTwo }
	if err := OpenFile(file, []string{root}); !errors.Is(err, exitTwo) {
		t.Errorf("OpenFile with explorer exit 2 error = %v, want it wrapped", err)
	}

	notFound := errors.New("executable file not found")
	runCommandFunc = func(*exec.Cmd) error { return notFound }
	if err := RevealFile(file, []string{root}); !errors.Is(err, notFound) {
		t.Errorf("RevealFile start failure error = %v, want it wrapped", err)
	}
}

// TestExplorerRefusesUnquotablePaths: a double quote or trailing backslash
// would break out of the quoted argument, so it is refused, not escaped.
func TestExplorerRefusesUnquotablePaths(t *testing.T) {
	forbidLaunches(t)

	for _, p := range []string{`C:\a"b.iso`, `C:\dir\`} {
		if _, err := explorerCommand(nil, p); !errors.Is(err, ErrUnsafeOpenPath) {
			t.Errorf("explorerCommand(%q) error = %v, want ErrUnsafeOpenPath", p, err)
		}
	}
}
