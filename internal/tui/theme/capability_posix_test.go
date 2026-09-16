//go:build !windows

package theme

import "testing"

// TestDetectColorDegradationPOSIX exercises truecolor → 256 → 16 → none
// degradation as termenv resolves it on POSIX platforms: by parsing
// TERM/COLORTERM. See capability_windows_test.go for the Windows
// equivalent, which resolves colour profile from the OS build number
// instead and does not consult TERM at all.
func TestDetectColorDegradationPOSIX(t *testing.T) {
	tests := []struct {
		name  string
		term  string
		color string
		want  ColorLevel
	}{
		{name: "truecolor via COLORTERM", term: "xterm-256color", color: "truecolor", want: ColorTrue},
		{name: "256color TERM", term: "screen-256color", color: "", want: Color256},
		{name: "plain color TERM", term: "xterm-color", color: "", want: Color16},
		{name: "no color info at all", term: "vt100", color: "", want: ColorNone},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			environ := stubEnviron{"TERM": tc.term}
			if tc.color != "" {
				environ["COLORTERM"] = tc.color
			}

			// AssumeTTY: real terminal detection is covered by
			// TestDetectInteractiveFalseOnNonTTYStdout; this test is
			// only about TERM/COLORTERM parsing once a terminal is
			// present.
			capability := Detect(DetectOptions{Out: tempFile(t), Environ: environ, AssumeTTY: true})
			if capability.Color != tc.want {
				t.Fatalf("Color = %v, want %v", capability.Color, tc.want)
			}
		})
	}
}
