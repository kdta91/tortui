package platform

import "os/exec"

// openFile runs `open <path>` — macOS's open-with-default-application verb.
// path is absolute (so it can never be mistaken for a flag) and already
// checked by OpenFile; it is passed as its own argv element, never through a
// shell.
func openFile(path string) error {
	return runCommandFunc(exec.Command("open", path))
}

// revealFile runs `open -R <path>` — Finder opens the containing folder with
// path selected.
func revealFile(path string) error {
	return runCommandFunc(exec.Command("open", "-R", path))
}
