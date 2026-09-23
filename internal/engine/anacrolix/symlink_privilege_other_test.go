//go:build !windows

package anacrolix

import (
	"errors"
	"os"
)

// isUnprivilegedSymlinkError reports whether err is the platform-specific
// signal that the current account lacks permission to create a symlink. On
// every POSIX tier-1 target, os.Symlink's permission failure is always
// os.ErrPermission — see symlink_privilege_windows_test.go for why Windows
// needs its own case.
func isUnprivilegedSymlinkError(err error) bool {
	return errors.Is(err, os.ErrPermission)
}
