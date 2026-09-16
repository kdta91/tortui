package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sandboxHome points TORTUI_HOME at a fresh temp directory so a doctor run
// never touches the real user config/state/downloads (AGENT.md §15).
func sandboxHome(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	t.Setenv("TORTUI_HOME", home)

	return home
}

func TestRunDoctorHealthySandboxExitsZero(t *testing.T) {
	sandboxHome(t)
	t.Setenv("TERM", "xterm-256color")

	out, code := captureOutput(t, func(w *os.File) int {
		return run([]string{"doctor"}, w)
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0, output:\n%s", code, out)
	}

	if !strings.Contains(out, "tortui doctor") {
		t.Fatalf("output missing header:\n%s", out)
	}
	if !strings.Contains(out, "Download dir writable: yes") {
		t.Fatalf("output does not report a writable download dir:\n%s", out)
	}
}

func TestRunDoctorTermDumbExitsNonZero(t *testing.T) {
	sandboxHome(t)
	t.Setenv("TERM", "dumb")

	out, code := captureOutput(t, func(w *os.File) int {
		return run([]string{"doctor"}, w)
	})

	if code != 1 {
		t.Fatalf("exit code = %d, want 1 for TERM=dumb, output:\n%s", code, out)
	}

	// It must still print a full report — doctor never refuses to run,
	// unlike the TUI itself (AGENT.md §15: "safe to pipe").
	if !strings.Contains(out, "tortui doctor") {
		t.Fatalf("TERM=dumb run produced no report:\n%s", out)
	}
	if !strings.Contains(out, "TERM:           dumb") {
		t.Fatalf("output does not echo TERM=dumb:\n%s", out)
	}
}

func TestRunDoctorUnwritableDownloadDirExitsNonZero(t *testing.T) {
	home := sandboxHome(t)
	t.Setenv("TERM", "xterm-256color")

	// Replacing the download_dir path component with a regular file makes
	// it deterministically uncreatable on every OS, without relying on
	// permission bits CI might run as root and ignore. An explicit
	// config.toml is written first (rather than letting Load first-run a
	// default one) so config.Load itself treats the resulting MkdirAll
	// failure as a non-fatal Problem — exactly like a real config.toml a
	// user hand-edited to point at a bad path — and doctor's own
	// writability probe (not config.Load's first-run bootstrap) is what
	// this test actually exercises.
	blocked := filepath.Join(home, "downloads")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	configDir := filepath.Join(home, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	body := fmt.Sprintf(`download_dir = %q
max_active_downloads = 3
max_peers = 50
seed_policy = "ratio"
seed_ratio = 1.0
min_free_space = "1GB"
search_timeout = "15s"
theme = "default"
`, blocked)

	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out, code := captureOutput(t, func(w *os.File) int {
		return run([]string{"doctor"}, w)
	})

	if code != 1 {
		t.Fatalf("exit code = %d, want 1 for an unwritable download dir, output:\n%s", code, out)
	}
	if !strings.Contains(out, "Download dir writable: no") {
		t.Fatalf("output does not report the unwritable download dir:\n%s", out)
	}
}

// TestRunDoctorMasksIndexerCredentials is the direct regression test for
// the T-055 acceptance criterion "Credentials are masked": a config.toml
// with a real-looking api_key/cookie must never have either value appear
// in doctor's plain-text, pipeable output.
func TestRunDoctorMasksIndexerCredentials(t *testing.T) {
	home := sandboxHome(t)
	t.Setenv("TERM", "xterm-256color")

	configDir := filepath.Join(home, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	const secretKey = "sh0uld-never-leak-doctor-test"
	const secretCookie = "session=doctor-test-cookie-value"

	body := fmt.Sprintf(`download_dir = %q
max_active_downloads = 3
max_peers = 50
seed_policy = "ratio"
seed_ratio = 1.0
min_free_space = "1GB"
search_timeout = "15s"
theme = "default"

[[indexer]]
id      = "unreachable"
name    = "Unreachable Source"
type    = "torznab"
url     = "http://127.0.0.1:1"
api_key = %q
cookie  = %q
enabled = true
`, filepath.Join(home, "downloads"), secretKey, secretCookie)

	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out, code := captureOutput(t, func(w *os.File) int {
		return run([]string{"doctor"}, w)
	})

	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (an unreachable indexer is informational, not a hard problem), output:\n%s", code, out)
	}

	if strings.Contains(out, secretKey) {
		t.Fatalf("doctor output leaked the api key:\n%s", out)
	}
	if strings.Contains(out, secretCookie) || strings.Contains(out, "doctor-test-cookie-value") {
		t.Fatalf("doctor output leaked the cookie:\n%s", out)
	}
	if !strings.Contains(out, "Unreachable Source") {
		t.Fatalf("doctor output missing the indexer's name:\n%s", out)
	}
}

func TestRunDoctorInvalidFlag(t *testing.T) {
	sandboxHome(t)

	_, code := captureOutput(t, func(w *os.File) int {
		return run([]string{"doctor", "--not-a-real-flag"}, w)
	})

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
}

func TestRunDoctorOutputHasNoANSIEscapes(t *testing.T) {
	sandboxHome(t)
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("COLORTERM", "truecolor")

	out, _ := captureOutput(t, func(w *os.File) int {
		return run([]string{"doctor"}, w)
	})

	if strings.Contains(out, "\x1b[") {
		t.Fatalf("doctor output contains an ANSI escape sequence, want plain pipeable text:\n%q", out)
	}
}
