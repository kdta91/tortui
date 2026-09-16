package theme

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// stubEnviron is a termenv.Environ backed by a map, so tests can set
// NO_COLOR, CLICOLOR_FORCE, TERM, and locale variables without touching
// the real process environment (which would make tests order-dependent
// and unsafe to run with -race/-parallel).
type stubEnviron map[string]string

func (e stubEnviron) Getenv(key string) string { return e[key] }

func (e stubEnviron) Environ() []string {
	out := make([]string, 0, len(e))
	for k, v := range e {
		out = append(out, k+"="+v)
	}

	return out
}

// tempFile returns an *os.File backed by a real (non-terminal) file, so
// isCharDevice sees a genuine *os.File and still reports false — exactly
// what "non-TTY stdout" means for a redirected or piped binary.
func tempFile(t *testing.T) *os.File {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), "tortui-theme-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}

	t.Cleanup(func() { _ = f.Close() })

	return f
}

func TestDetectNoColorForcesColorNone(t *testing.T) {
	environ := stubEnviron{
		"NO_COLOR":       "1",
		"TERM":           "xterm-256color",
		"CLICOLOR_FORCE": "1", // NO_COLOR must win even when this is also set
	}

	capability := Detect(DetectOptions{Out: tempFile(t), Environ: environ})

	if capability.Color != ColorNone {
		t.Fatalf("Color = %v, want ColorNone under NO_COLOR", capability.Color)
	}
}

func TestDetectNoColorRendersZeroEscapeCodes(t *testing.T) {
	environ := stubEnviron{"NO_COLOR": "1", "TERM": "xterm-256color"}
	capability := Detect(DetectOptions{Out: tempFile(t), Environ: environ})

	th := New(DefaultThemeName, capability)

	rendered := th.Accent.Bold(true).Render("hello world")
	if strings.ContainsRune(rendered, '\x1b') {
		t.Fatalf("Accent.Render under NO_COLOR contains an escape code: %q", rendered)
	}

	rendered = th.Error.Render("bad")
	if strings.ContainsRune(rendered, '\x1b') {
		t.Fatalf("Error.Render under NO_COLOR contains an escape code: %q", rendered)
	}

	if rendered != "bad" {
		t.Fatalf("Error.Render under NO_COLOR = %q, want plain text unchanged", rendered)
	}
}

func TestDetectCliColorForceHonoured(t *testing.T) {
	// A non-tty *os.File with no colour-implying TERM would normally
	// resolve to Ascii; CLICOLOR_FORCE overrides that to ANSI.
	environ := stubEnviron{"CLICOLOR_FORCE": "1", "TERM": "vt100"}

	capability := Detect(DetectOptions{Out: tempFile(t), Environ: environ})

	if capability.Color != Color16 {
		t.Fatalf("Color = %v, want Color16 under CLICOLOR_FORCE", capability.Color)
	}
}

func TestDetectCliColorForceZeroDoesNotForce(t *testing.T) {
	environ := stubEnviron{"CLICOLOR_FORCE": "0", "TERM": "vt100"}

	capability := Detect(DetectOptions{Out: tempFile(t), Environ: environ})

	if capability.Color != ColorNone {
		t.Fatalf("Color = %v, want ColorNone when CLICOLOR_FORCE=0", capability.Color)
	}
}

// TERM/COLORTERM-driven degradation (below) is exercised in
// capability_posix_test.go / capability_windows_test.go: termenv itself
// resolves colour profile very differently per OS — POSIX parses
// TERM/COLORTERM strings, Windows queries the OS build number instead and
// ignores TERM entirely — so a single cross-platform table over those
// variables cannot hold both truths at once (this is the same OS-specific
// behaviour AGENT.md §14's build-tag invariant is about, just already
// encapsulated inside termenv's own `_windows.go`/POSIX build-tagged files
// rather than ours).

func TestDetectInteractiveFalseOnTermDumb(t *testing.T) {
	environ := stubEnviron{"TERM": "dumb"}

	capability := Detect(DetectOptions{Out: tempFile(t), Environ: environ})
	if capability.Interactive {
		t.Fatal("Interactive = true, want false when TERM=dumb")
	}
}

func TestDetectInteractiveFalseOnNonTTYStdout(t *testing.T) {
	environ := stubEnviron{"TERM": "xterm-256color"}

	// A real, non-char-device file: exactly what a redirected/piped
	// stdout looks like.
	capability := Detect(DetectOptions{Out: tempFile(t), Environ: environ})
	if capability.Interactive {
		t.Fatal("Interactive = true, want false for a non-TTY *os.File")
	}

	// A writer that isn't even an *os.File (e.g. a bytes.Buffer some
	// code substituted for testing) must also read as non-interactive.
	capability = Detect(DetectOptions{Out: &bytes.Buffer{}, Environ: environ})
	if capability.Interactive {
		t.Fatal("Interactive = true, want false for a non-*os.File writer")
	}
}

func TestDetectUnicodeCapability(t *testing.T) {
	tests := []struct {
		name       string
		environ    stubEnviron
		forceASCII bool
		want       bool
	}{
		{name: "UTF-8 locale", environ: stubEnviron{"LANG": "en_US.UTF-8"}, want: true},
		{name: "non-UTF-8 locale", environ: stubEnviron{"LANG": "C"}, want: false},
		{name: "LC_ALL wins over LANG", environ: stubEnviron{"LC_ALL": "C", "LANG": "en_US.UTF-8"}, want: false},
		{name: "no locale vars set defaults to Unicode", environ: stubEnviron{}, want: true},
		{name: "force ASCII overrides a UTF-8 locale", environ: stubEnviron{"LANG": "en_US.UTF-8"}, forceASCII: true, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			capability := Detect(DetectOptions{Out: tempFile(t), Environ: tc.environ, ForceASCII: tc.forceASCII})
			if capability.Unicode != tc.want {
				t.Fatalf("Unicode = %v, want %v", capability.Unicode, tc.want)
			}
		})
	}
}

func TestRefusalMessageIsOneLine(t *testing.T) {
	msg := RefusalMessage()
	if msg == "" {
		t.Fatal("RefusalMessage() is empty")
	}

	if strings.Contains(msg, "\n") {
		t.Fatalf("RefusalMessage() = %q, want a single line", msg)
	}
}

func TestColorLevelString(t *testing.T) {
	tests := map[ColorLevel]string{
		ColorNone:      "none",
		Color16:        "16",
		Color256:       "256",
		ColorTrue:      "truecolor",
		ColorLevel(99): "unknown",
	}

	for level, want := range tests {
		if got := level.String(); got != want {
			t.Errorf("ColorLevel(%d).String() = %q, want %q", level, got, want)
		}
	}
}
