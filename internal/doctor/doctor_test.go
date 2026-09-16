package doctor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/indexer/httpx"
	"github.com/kdta91/tortui/internal/tui/theme"
)

const secretAPIKey = "sh0uld-never-leak-1234567890"

func testCapability() theme.Capability {
	return theme.Capability{
		Color:       theme.Color256,
		Unicode:     true,
		Interactive: true,
		Width:       120,
		Height:      40,
		Multiplexer: "",
	}
}

func testPaths(t *testing.T, downloadDir string) config.Paths {
	t.Helper()

	base := t.TempDir()

	return config.Paths{
		ConfigDir:      filepath.Join(base, "config"),
		ConfigFile:     filepath.Join(base, "config", "config.toml"),
		StateDir:       filepath.Join(base, "state"),
		DownloadDir:    downloadDir,
		DefinitionsDir: filepath.Join(base, "config", "definitions"),
	}
}

// TestBuildNeverLeaksCredentials is the hard requirement from the T-055
// acceptance criteria: a configured indexer's api_key/cookie must never
// appear anywhere in a Report, however that indexer behaves on the wire.
// It exercises three server behaviours — success, a non-2xx status, and a
// hard network failure via an unresolvable host — because each is a
// different code path in probeOne that could leak the credential a
// different way.
func TestBuildNeverLeaksCredentials(t *testing.T) {
	var gotAPIKey, gotCookie string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.URL.Query().Get("apikey")
		gotCookie = r.Header.Get("Cookie")
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	cfg := config.Config{
		DownloadDir: t.TempDir(),
		Indexers: []config.Indexer{
			{
				ID:      "leaky",
				Name:    "Leaky Source",
				URL:     srv.URL,
				APIKey:  secretAPIKey,
				Cookie:  "session=topsecretcookievalue",
				Enabled: true,
			},
		},
	}

	report := Build(context.Background(), Options{
		Capability: testCapability(),
		Paths:      testPaths(t, cfg.DownloadDir),
		Config:     cfg,
		Timeout:    2 * time.Second,
	})

	// Sanity: the client really did send the credentials (proves the probe
	// is exercising the real credential-injection path, not skipping it).
	if gotAPIKey != secretAPIKey {
		t.Fatalf("server saw apikey=%q, want %q — probe did not send credentials", gotAPIKey, secretAPIKey)
	}
	if !strings.Contains(gotCookie, "topsecretcookievalue") {
		t.Fatalf("server saw Cookie=%q, want it to contain the configured cookie", gotCookie)
	}

	rendered := Format(report)
	for _, secret := range []string{secretAPIKey, "topsecretcookievalue"} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("Format(report) contains the secret %q:\n%s", secret, rendered)
		}
	}

	for _, v := range report.Indexers {
		if strings.Contains(v.Detail, secretAPIKey) || strings.Contains(v.Detail, "topsecretcookievalue") {
			t.Fatalf("IndexerVerdict.Detail leaked a credential: %q", v.Detail)
		}
	}
}

// TestBuildNeverLeaksCredentialsOnNetworkFailure covers the other error
// path: a request that fails before any HTTP response exists at all (here,
// a URL an httptest listener never bound), whose underlying net/http error
// text embeds the full request URL — and therefore the api_key query
// parameter — unless probeOne's redaction catches it.
func TestBuildNeverLeaksCredentialsOnNetworkFailure(t *testing.T) {
	cfg := config.Config{
		DownloadDir: t.TempDir(),
		Indexers: []config.Indexer{
			{
				ID:      "dead",
				Name:    "Dead Source",
				URL:     "http://127.0.0.1:1", // reserved, nothing ever listens here
				APIKey:  secretAPIKey,
				Enabled: true,
			},
		},
	}

	report := Build(context.Background(), Options{
		Capability: testCapability(),
		Paths:      testPaths(t, cfg.DownloadDir),
		Config:     cfg,
		Timeout:    2 * time.Second,
	})

	if len(report.Indexers) != 1 {
		t.Fatalf("len(report.Indexers) = %d, want 1", len(report.Indexers))
	}

	v := report.Indexers[0]
	if v.Reachable {
		t.Fatal("Reachable = true for a host nothing listens on, want false")
	}

	if strings.Contains(v.Detail, secretAPIKey) {
		t.Fatalf("IndexerVerdict.Detail leaked the api key: %q", v.Detail)
	}
}

func TestCheckIndexersSkipsDisabledAndURLless(t *testing.T) {
	cfg := config.Config{
		Indexers: []config.Indexer{
			{ID: "off", Name: "Disabled", URL: "http://example.invalid", Enabled: false},
			{ID: "nourl", Name: "No URL", URL: "", Enabled: true},
		},
	}

	report := Build(context.Background(), Options{
		Capability: testCapability(),
		Paths:      testPaths(t, t.TempDir()),
		Config:     cfg,
	})

	if len(report.Indexers) != 2 {
		t.Fatalf("len(Indexers) = %d, want 2", len(report.Indexers))
	}
	if report.Indexers[0].Checked {
		t.Fatal("disabled indexer was Checked, want it skipped without a network call")
	}
	if !strings.Contains(report.Indexers[0].Detail, "disabled") {
		t.Fatalf("disabled indexer Detail = %q, want it to mention disabled", report.Indexers[0].Detail)
	}
	if report.Indexers[1].Checked {
		t.Fatal("URL-less indexer was Checked, want it skipped without a network call")
	}
}

func TestCheckIndexersReachableOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := config.Config{
		Indexers: []config.Indexer{
			{
				ID:      "ok",
				Name:    "OK Source",
				URL:     srv.URL,
				Enabled: true,
			},
		},
	}

	report := Build(context.Background(), Options{
		Capability: testCapability(),
		Paths:      testPaths(t, t.TempDir()),
		Config:     cfg,
		Timeout:    2 * time.Second,
	})

	v := report.Indexers[0]
	if !v.Checked || !v.Reachable {
		t.Fatalf("verdict = %+v, want Checked and Reachable", v)
	}
	if !strings.Contains(v.Detail, "200") {
		t.Fatalf("Detail = %q, want it to mention HTTP 200", v.Detail)
	}
}

func TestCheckIndexersReachableOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	cfg := config.Config{
		Indexers: []config.Indexer{
			{
				ID:      "forbidden",
				Name:    "Forbidden Source",
				URL:     srv.URL,
				Enabled: true,
			},
		},
	}

	report := Build(context.Background(), Options{
		Capability: testCapability(),
		Paths:      testPaths(t, t.TempDir()),
		Config:     cfg,
		Timeout:    2 * time.Second,
	})

	v := report.Indexers[0]
	if !v.Reachable {
		t.Fatalf("verdict = %+v, want Reachable=true — the host answered, even with a 403", v)
	}
	if !strings.Contains(v.Detail, "403") {
		t.Fatalf("Detail = %q, want it to mention HTTP 403", v.Detail)
	}
}

func TestCheckIndexersZeroNetworkCallsForEmptyConfig(t *testing.T) {
	// No servers started at all in this test: with no indexers configured,
	// Build must not attempt any network call (AGENT.md §6.7).
	report := Build(context.Background(), Options{
		Capability: testCapability(),
		Paths:      testPaths(t, t.TempDir()),
		Config:     config.Config{},
	})

	if len(report.Indexers) != 0 {
		t.Fatalf("len(Indexers) = %d, want 0", len(report.Indexers))
	}
}

func TestBuildDownloadDirWritable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "downloads")

	report := Build(context.Background(), Options{
		Capability: testCapability(),
		Paths:      testPaths(t, dir),
		Config:     config.Config{DownloadDir: dir},
	})

	if !report.DownloadDirWritable {
		t.Fatalf("DownloadDirWritable = false, problem = %q, want true for a freshly creatable temp dir", report.DownloadDirProblem)
	}

	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("download dir was not created: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("download dir has %d leftover entries after the writability probe, want 0: %v", len(entries), entries)
	}
}

func TestBuildDownloadDirUnwritable(t *testing.T) {
	// A regular file can never be MkdirAll'd into, so pointing DownloadDir
	// at one deterministically reproduces an unwritable destination
	// without needing root or chmod tricks that behave differently across
	// platforms (and CI often runs as root, where chmod 000 is not
	// actually enforced).
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	dir := filepath.Join(blocker, "downloads")

	report := Build(context.Background(), Options{
		Capability: testCapability(),
		Paths:      testPaths(t, dir),
		Config:     config.Config{DownloadDir: dir},
	})

	if report.DownloadDirWritable {
		t.Fatal("DownloadDirWritable = true, want false when the parent is a regular file")
	}
	if report.DownloadDirProblem == "" {
		t.Fatal("DownloadDirProblem is empty, want an explanation")
	}
}

func TestHasHardProblem(t *testing.T) {
	tests := []struct {
		name string
		r    Report
		want bool
	}{
		{name: "healthy", r: Report{Term: "xterm-256color", DownloadDirWritable: true}, want: false},
		{name: "TERM=dumb", r: Report{Term: "dumb", DownloadDirWritable: true}, want: true},
		{name: "unwritable download dir", r: Report{Term: "xterm-256color", DownloadDirWritable: false}, want: true},
		{name: "unreachable indexer alone is not hard", r: Report{
			Term: "xterm-256color", DownloadDirWritable: true,
			Indexers: []IndexerVerdict{{ID: "x", Checked: true, Reachable: false, Detail: "unreachable: x"}},
		}, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasHardProblem(tc.r); got != tc.want {
				t.Fatalf("HasHardProblem(%+v) = %v, want %v", tc.r, got, tc.want)
			}
		})
	}
}

func TestFormatIsPlainText(t *testing.T) {
	report := Build(context.Background(), Options{
		Capability: testCapability(),
		Paths:      testPaths(t, t.TempDir()),
		Config: config.Config{
			DownloadDir: t.TempDir(),
			Indexers:    []config.Indexer{{ID: "none", Name: "Unset", URL: "", Enabled: true}},
		},
	})

	out := Format(report)

	if strings.Contains(out, "\x1b[") {
		t.Fatalf("Format output contains an ANSI escape sequence:\n%q", out)
	}
	if !strings.Contains(out, "tortui doctor") {
		t.Fatal("Format output missing header line")
	}
	if !strings.Contains(out, "OS/Arch:") {
		t.Fatal("Format output missing OS/Arch line")
	}
}

func TestFormatEmptyIndexerList(t *testing.T) {
	report := Build(context.Background(), Options{
		Capability: testCapability(),
		Paths:      testPaths(t, t.TempDir()),
		Config:     config.Config{DownloadDir: t.TempDir()},
	})

	out := Format(report)
	if !strings.Contains(out, "Indexers: none configured") {
		t.Fatalf("Format output = %q, want it to say no indexers are configured", out)
	}
}

// TestBuildRespectsCustomNewClient confirms Options.NewClient is honoured,
// which is what lets tests point probes at an httptest.Server through the
// exact same construction path production uses (real credential
// injection), rather than a parallel test-only probe implementation.
func TestBuildRespectsCustomNewClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var built int
	cfg := config.Config{
		Indexers: []config.Indexer{
			{
				ID:      "custom",
				Name:    "Custom",
				URL:     srv.URL,
				Enabled: true,
			},
		},
	}

	report := Build(context.Background(), Options{
		Capability: testCapability(),
		Paths:      testPaths(t, t.TempDir()),
		Config:     cfg,
		NewClient: func(ix config.Indexer, timeout time.Duration) *httpx.Client {
			built++
			return httpx.New(httpx.Config{RequestTimeout: timeout, MaxAttempts: 1})
		},
	})

	if built != 1 {
		t.Fatalf("custom NewClient called %d times, want 1", built)
	}
	if !report.Indexers[0].Reachable {
		t.Fatal("Reachable = false, want true")
	}
}
