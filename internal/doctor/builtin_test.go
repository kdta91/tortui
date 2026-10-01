package doctor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/config"
)

func builtinOptions(t *testing.T, cfg config.Config, srvURL string) Options {
	t.Helper()

	cfg.DownloadDir = t.TempDir()

	return Options{
		Capability: testCapability(),
		Paths:      testPaths(t, t.TempDir()),
		Config:     cfg,
		Builtins:   []Builtin{{ID: "archive-src", Name: "Archive Source", URL: srvURL}},
		Timeout:    2 * time.Second,
	}
}

// TestBuiltinSourceIsListedAndProbed: with nothing configured, a bundled
// source still gets a verdict, labelled built-in, from a real probe against
// a local test server.
func TestBuiltinSourceIsListedAndProbed(t *testing.T) {
	var hits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	report := Build(context.Background(), builtinOptions(t, config.Config{}, srv.URL))

	if len(report.Indexers) != 1 {
		t.Fatalf("verdicts = %+v, want the one built-in", report.Indexers)
	}

	v := report.Indexers[0]
	if !v.Builtin || !v.Checked || !v.Reachable || v.ID != "archive-src" {
		t.Fatalf("verdict = %+v, want a reachable built-in", v)
	}

	if hits.Load() != 1 {
		t.Fatalf("server saw %d requests, want 1", hits.Load())
	}

	out := Format(report)
	if strings.Contains(out, "none configured") {
		t.Fatalf("output still says none configured:\n%s", out)
	}

	if !strings.Contains(out, "Archive Source (archive-src) [built-in]: reachable") {
		t.Fatalf("output lacks the labelled built-in line:\n%s", out)
	}

	if strings.Contains(out, srv.URL) {
		t.Fatalf("output names the probed address:\n%s", out)
	}
}

// TestBuiltinSourceFollowsTheDisabledList: a disabled built-in is listed
// but not probed; one an [[indexer]] entry replaces is listed once, as the
// entry.
func TestBuiltinSourceFollowsTheDisabledList(t *testing.T) {
	var hits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()

	off := Build(context.Background(), builtinOptions(t, config.Config{DisabledBuiltins: []string{"archive-src"}}, srv.URL))

	if v := off.Indexers[0]; v.Checked || !v.Builtin || !strings.Contains(v.Detail, "disabled") {
		t.Fatalf("disabled built-in verdict = %+v", v)
	}

	replaced := Build(context.Background(), builtinOptions(t, config.Config{
		Indexers: []config.Indexer{{ID: "archive-src", Name: "Mine", URL: srv.URL, Enabled: true}},
	}, srv.URL))

	if len(replaced.Indexers) != 1 || replaced.Indexers[0].Builtin {
		t.Fatalf("verdicts = %+v, want only the configured entry", replaced.Indexers)
	}

	if hits.Load() != 1 {
		t.Fatalf("server saw %d requests, want 1 (the replaced entry only)", hits.Load())
	}
}

// TestBuiltinSourceFollowsConfiguredOnes: configured sources keep their
// order and the built-in comes after them.
func TestBuiltinSourceFollowsConfiguredOnes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	report := Build(context.Background(), builtinOptions(t, config.Config{
		Indexers: []config.Indexer{{ID: "mine", Name: "Mine", URL: srv.URL, Enabled: true}},
	}, srv.URL))

	if len(report.Indexers) != 2 || report.Indexers[0].ID != "mine" || report.Indexers[0].Builtin || !report.Indexers[1].Builtin {
		t.Fatalf("verdicts = %+v", report.Indexers)
	}
}
