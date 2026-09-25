// Package platform holds the per-OS integration points that AGENT.md §14
// requires to stay behind build tags: nothing in this tree may branch on
// runtime.GOOS outside this package.
package platform

import "runtime"

// OS reports the operating system the process is running on — the same
// value runtime.GOOS itself would report. It exists so that a caller that
// only needs to display or log the OS (doctor's environment report is the
// current one) never has to import "runtime" and reference GOOS directly;
// every occurrence of that identifier funnels through here instead, which
// is what scripts/check-goos-scope.sh enforces (T-944).
//
// This file carries no build tag: reading runtime.GOOS is itself
// platform-independent, unlike the files beside it named _darwin.go,
// _linux.go, and _windows.go, which each compile for one OS only.
func OS() string {
	return runtime.GOOS
}

// IsWindows reports whether OS() is "windows". Several packages outside
// this one need exactly this one bit — POSIX permission bits and symlink
// privilege errors do not apply on Windows — without needing a full
// build-tagged implementation of their own; this is the one place that
// decision is made (T-944).
func IsWindows() bool {
	return runtime.GOOS == "windows"
}
