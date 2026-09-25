package platform

import "golang.org/x/sys/unix"

// MaxPathLength is the longest absolute path, in bytes, this platform's
// path APIs accept (PATH_MAX, less the terminating NUL). A torrent whose
// files would land at a longer path is refused rather than failing halfway
// through a download (T-034).
const MaxPathLength = 4095

// PathLength measures p the way MaxPathLength is expressed: in bytes, which
// is what the kernel counts on POSIX.
func PathLength(p string) int { return len(p) }

// freeSpace is statfs(2)'s f_bavail × f_bsize: blocks available to an
// unprivileged caller, excluding the root reserve.
func freeSpace(dir string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, err
	}

	return st.Bavail * uint64(st.Bsize), nil //nolint:gosec // Bsize is a positive block size.
}
