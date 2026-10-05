package platform

import (
	"fmt"
	"strings"
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

// filesystemID is the serial number of the volume holding dir, read from the
// volume's root (GetVolumePathNameW, then GetVolumeInformationW): a volume
// mounted under two drive letters or folders has one serial. When the serial
// cannot be read, the volume root itself, case-folded, names it.
func filesystemID(dir string) (string, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return "", err
	}

	buf := make([]uint16, windows.MAX_LONG_PATH)
	if err := windows.GetVolumePathName(p, &buf[0], uint32(len(buf))); err != nil {
		return "", err
	}

	root := windows.UTF16ToString(buf)

	var serial uint32
	if err := windows.GetVolumeInformation(&buf[0], nil, 0, &serial, nil, nil, nil, 0); err != nil {
		return "root:" + strings.ToLower(root), nil
	}

	return fmt.Sprintf("vol:%08x", serial), nil
}
