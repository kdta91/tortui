package platform

import (
	"runtime"
	"testing"
)

// TestOSMatchesRuntimeGOOS is the one place in the module allowed to name
// runtime.GOOS outside this package's own non-test sources — a test
// asserting this package's wrapper is correct is exactly what
// scripts/check-goos-scope.sh means to leave room for (see its header).
func TestOSMatchesRuntimeGOOS(t *testing.T) {
	if got := OS(); got != runtime.GOOS {
		t.Errorf("OS() = %q, want %q", got, runtime.GOOS)
	}
}

func TestIsWindowsMatchesRuntimeGOOS(t *testing.T) {
	want := runtime.GOOS == "windows"
	if got := IsWindows(); got != want {
		t.Errorf("IsWindows() = %v, want %v", got, want)
	}
}
