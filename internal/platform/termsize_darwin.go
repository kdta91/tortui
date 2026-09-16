// Package platform holds the per-OS integration points that AGENT.md §14
// requires to stay behind build tags: nothing in this tree may branch on
// runtime.GOOS outside this package.
package platform

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// TerminalSize reports the width and height, in columns and rows, of the
// terminal f is attached to via the TIOCGWINSZ ioctl. Callers must confirm
// f is actually a character device (a real terminal) first — passing a
// pipe or regular file makes the ioctl fail, which is reported as an error
// rather than a fabricated size.
func TerminalSize(f *os.File) (width, height int, err error) {
	ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0, fmt.Errorf("ioctl TIOCGWINSZ: %w", err)
	}

	return int(ws.Col), int(ws.Row), nil
}
