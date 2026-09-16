// Package platform holds the per-OS integration points that AGENT.md §14
// requires to stay behind build tags: nothing in this tree may branch on
// runtime.GOOS outside this package.
package platform

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// TerminalSize reports the width and height, in columns and rows, of the
// console f is attached to, via GetConsoleScreenBufferInfo. It reports the
// visible window's extent (srWindow), not the scrollback buffer size
// (dwSize), which is what a user actually means by "terminal size".
func TerminalSize(f *os.File) (width, height int, err error) {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(f.Fd()), &info); err != nil {
		return 0, 0, fmt.Errorf("GetConsoleScreenBufferInfo: %w", err)
	}

	width = int(info.Window.Right-info.Window.Left) + 1
	height = int(info.Window.Bottom-info.Window.Top) + 1

	return width, height, nil
}
