package platform

import (
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

// MaxPathLength is the longest absolute path, in UTF-16 code units, tortui
// will write to on Windows: MAX_PATH (260) less the terminating NUL. Go's
// own os package can reach longer paths through the \\?\ prefix, but
// Explorer, the shell "open" verb and most applications cannot, so a file
// written past MAX_PATH is one the user could not open or delete from their
// own desktop. A torrent that would need one is refused instead (T-034,
// DEC-106).
const MaxPathLength = 259

// PathLength measures p the way MaxPathLength is expressed: in UTF-16 code
// units, which is what Win32 counts.
func PathLength(p string) int { return len(utf16.Encode([]rune(p))) }

// freeSpace is GetDiskFreeSpaceExW's "free bytes available to caller",
// which honours per-user disk quotas.
func freeSpace(dir string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}

	var avail, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &total, &free); err != nil {
		return 0, err
	}

	return avail, nil
}
