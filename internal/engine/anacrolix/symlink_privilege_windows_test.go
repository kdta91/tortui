//go:build windows

package anacrolix

import (
	"errors"
	"os"
	"syscall"
)

// errPrivilegeNotHeld is Windows' ERROR_PRIVILEGE_NOT_HELD (1314): what
// os.Symlink actually returns when the calling account lacks
// SeCreateSymbolicLinkPrivilege, which is the default for a non-elevated
// account without Developer Mode enabled. Go does not map this errno onto
// os.ErrPermission, so a test that only checks errors.Is(err,
// os.ErrPermission) fails outright on a stock Windows CI runner instead of
// skipping.
const errPrivilegeNotHeld = syscall.Errno(1314)

// isUnprivilegedSymlinkError reports whether err is the platform-specific
// signal that the current account lacks permission to create a symlink, so a
// test can skip conditionally — never unconditionally (AGENT.md's
// cross-platform testing expectations) — rather than failing on every
// Windows runner that has not been granted the privilege.
func isUnprivilegedSymlinkError(err error) bool {
	return errors.Is(err, os.ErrPermission) || errors.Is(err, errPrivilegeNotHeld)
}
