package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/indexer/httpx"
)

// TestProductionSettingsWiringIsPresent pins what buildModel hands the TUI
// for the settings screen (T-997). A root built without the source or
// preferences manager still starts, searches and downloads; only the settings
// actions report "no ... manager configured", so every other test stays
// green. Here the add-source form and the preferences panel must both open,
// and neither refusal may ever have been on screen.
func TestProductionSettingsWiringIsPresent(t *testing.T) {
	guardDefaultTransport(t)
	sandbox(t)

	a := newTestApp(t, &recordingTransport{})
	t.Cleanup(func() { _ = a.Close() })

	s := newScreens(t, a)

	s.waitFor(t, "Welcome to tortui")
	s.key(" ")
	s.key("5") // jump to Settings
	s.waitFor(t, "p preferences")

	s.key("a")
	s.waitFor(t, "Add source")
	s.tm.Send(tea.KeyMsg{Type: tea.KeyEsc})

	s.key("p")
	s.waitFor(t, "Preferences")
	s.tm.Send(tea.KeyMsg{Type: tea.KeyEsc})

	final := s.quit(t)

	for _, refusal := range []string{"no source manager configured", "no preferences manager configured"} {
		if strings.Contains(s.seen.String(), refusal) || strings.Contains(final, refusal) {
			t.Errorf("the settings screen showed %q: the composition root did not wire its manager", refusal)
		}
	}
}

// refusingRoots is an engine root set that refuses every admission, as a
// closed engine does during shutdown.
type refusingRoots struct{ calls []string }

func (r *refusingRoots) AddRoot(dir string) error {
	r.calls = append(r.calls, dir)
	return errors.New("engine closed")
}

// TestSaveConfigLogsAFailedRootAdmission: once config.toml and the in-memory
// config hold the new preferences, a root the engine then refuses is logged,
// not returned, so the TUI never keeps its old snapshot against a new one on
// disk (T-998).
func TestSaveConfigLogsAFailedRootAdmission(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := config.Default(t.TempDir())

	var logs bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&logs, nil))
	roots := &refusingRoots{}
	live := buildSources(cfg, t.TempDir(), logger, httpEnv{transport: &recordingTransport{}})
	m := newSettingsManager(path, cfg, live, roots, logger)

	next := m.Config()
	next.MaxPeers = 7

	if err := m.SaveConfig(next); err != nil {
		t.Fatalf("SaveConfig = %v, want nil: the save itself succeeded", err)
	}

	if len(roots.calls) == 0 {
		t.Fatal("SaveConfig never offered the download directory to the engine")
	}

	if got := m.Config().MaxPeers; got != 7 {
		t.Errorf("in-memory MaxPeers = %d, want 7", got)
	}

	onDisk, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	if got := onDisk.Config.MaxPeers; got != 7 {
		t.Errorf("on-disk MaxPeers = %d, want 7", got)
	}

	if !strings.Contains(logs.String(), "admit destination as an engine root") || !strings.Contains(logs.String(), "engine closed") {
		t.Errorf("the refused admission was not logged:\n%s", logs.String())
	}
}

// TestHostIntervalSeamSkipsTheRealWait: a client built over an httpEnv with
// no spacing sends two requests to one host back to back, where the default
// would hold the second for a full second (T-996), and Options carries the
// seam to the settings manager's clients.
func TestHostIntervalSeamSkipsTheRealWait(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(srv.Close)

	client := newClient(httpx.Credentials{}, httpEnv{transport: srv.Client().Transport, minHostInterval: -1})

	start := time.Now()

	for range 2 {
		if _, err := client.Get(context.Background(), srv.URL, nil); err != nil {
			t.Fatalf("Get: %v", err)
		}
	}

	if took := time.Since(start); took >= httpx.DefaultMinHostInterval {
		t.Errorf("two requests to one host took %v; the zero-spacing env still waited the default interval", took)
	}

	guardDefaultTransport(t)
	sandbox(t)

	a := newTestApp(t, &recordingTransport{})
	t.Cleanup(func() { _ = a.Close() })

	if got := a.settings.env.minHostInterval; got != -1 {
		t.Errorf("settings manager minHostInterval = %v, want -1 from Options", got)
	}
}
