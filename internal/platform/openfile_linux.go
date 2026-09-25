package platform

import (
	"os/exec"
	"path/filepath"
)

// openFile runs `xdg-open <path>` — the freedesktop.org open-with-default-
// application verb. path is absolute (so it can never be mistaken for a
// flag) and already checked by OpenFile; it is passed as its own argv
// element, never through a shell.
func openFile(path string) error {
	return runCommandFunc(exec.Command("xdg-open", path))
}

// revealFile runs `xdg-open <dir>` on path's parent directory: xdg-open has
// no select-this-file form, so opening the containing folder is as close as
// the freedesktop verb gets. The parent of a path RevealFile confirmed is
// strictly inside a destination root is that root or a directory under it.
func revealFile(path string) error {
	return runCommandFunc(exec.Command("xdg-open", filepath.Dir(path)))
}
