package platform

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// openFile runs `explorer "<path>"`, which opens path with its associated
// application. See explorerCommand for why the command line is quoted here
// rather than left to Go's default argument escaping.
func openFile(path string) error {
	cmd, err := explorerCommand(nil, path)
	if err != nil {
		return err
	}

	return runExplorer(cmd)
}

// revealFile runs `explorer /select,"<path>"`, which opens the containing
// folder with path selected.
func revealFile(path string) error {
	cmd, err := explorerCommand([]string{"/select,"}, path)
	if err != nil {
		return err
	}

	return runExplorer(cmd)
}

// explorerCommand builds an explorer.exe invocation for path, preceded by
// flags. Args is the logical argument slice; SysProcAttr.CmdLine pins the
// exact command line CreateProcess receives (there is no shell on this path
// — no cmd.exe, so &, |, ^, % and friends are inert).
//
// The explicit command line exists because explorer does its own parsing
// and treats an unquoted comma as a separator: Go's default escaping only
// quotes an argument containing a space or tab, so a file named "a,b.iso"
// would reach explorer as two objects. Quoting the path unconditionally
// fixes that, and it is safe because a Windows path cannot contain a double
// quote; one that does anyway is refused rather than escaped. A trailing
// backslash is refused too, since it would escape the closing quote.
func explorerCommand(flags []string, path string) (*exec.Cmd, error) {
	if strings.ContainsRune(path, '"') || strings.HasSuffix(path, `\`) {
		return nil, fmt.Errorf("%w: %q cannot be quoted for explorer", ErrUnsafeOpenPath, path)
	}

	args := append(append([]string(nil), flags...), path)
	cmd := exec.Command("explorer", args...)

	line := "explorer "
	if len(flags) > 0 {
		line += strings.Join(flags, " ")
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: line + `"` + path + `"`}

	return cmd, nil
}

// runExplorer runs cmd, treating exit status 1 as success: explorer.exe
// reports 1 even when it opened the target. Any other non-zero status, and
// a failure to start the process at all, is a real error.
func runExplorer(cmd *exec.Cmd) error {
	err := runCommandFunc(cmd)

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return nil
	}

	return err
}
