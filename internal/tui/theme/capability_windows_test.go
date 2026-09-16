//go:build windows

package theme

import "testing"

// TestDetectColorDegradationWindows exercises colour-profile resolution
// as termenv resolves it on Windows: by OS build number and well-known
// environment markers, never by parsing TERM/COLORTERM (see
// capability_posix_test.go for the POSIX equivalent). ConEmuANSI is the
// one override checked before the build-number branch, so it is the only
// deterministic case that does not depend on the CI runner's Windows
// build.
func TestDetectColorDegradationWindows(t *testing.T) {
	environ := stubEnviron{"ConEmuANSI": "ON"}

	capability := Detect(DetectOptions{Out: tempFile(t), Environ: environ, AssumeTTY: true})
	if capability.Color != ColorTrue {
		t.Fatalf("Color = %v, want ColorTrue with ConEmuANSI=ON", capability.Color)
	}
}

// TestDetectColorDegradationWindowsModernBuild documents (rather than
// hardcodes as a magic assumption elsewhere) that every Windows tier-1 CI
// runner (AGENT.md §14: Windows 10+) is well past the build 14931
// truecolor threshold, so a plain TTY with no special markers resolves to
// full truecolor there.
func TestDetectColorDegradationWindowsModernBuild(t *testing.T) {
	capability := Detect(DetectOptions{Out: tempFile(t), Environ: stubEnviron{}, AssumeTTY: true})
	if capability.Color != ColorTrue {
		t.Fatalf("Color = %v, want ColorTrue on a modern (build >= 14931) Windows TTY", capability.Color)
	}
}
