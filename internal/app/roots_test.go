package app

import (
	"context"
	"errors"
	"testing"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/engine/anacrolix"
)

// Distinct offline magnets: rootsMagnet is the torrent that must survive the
// restart, rootsProbeMagnet only probes the engine before Resume and is
// removed again at once.
const (
	rootsMagnet      = "magnet:?xt=urn:btih:1111111111111111111111111111111111111111&dn=t993.iso"
	rootsProbeMagnet = "magnet:?xt=urn:btih:2222222222222222222222222222222222222222&dn=t993-probe.iso"
)

// TestEngineRootsIncludeEveryDestinationSource covers both of the root's
// destination sources for the engine (AGENT.md §6.12): a destination the
// store recorded (st.Destinations) and one saved in config.toml
// (cfg.SavedDestinations). For each, a torrent added under a destination
// outside download_dir survives a close and restart of the composition root
// with nothing failed or errored, and the restarted engine already accepts
// that destination when it is built, before Session.Resume re-admits
// anything. The second half is what pins the store source: Resume re-adds
// every recorded destination as a root itself (T-074), so the restart alone
// would pass without it.
func TestEngineRootsIncludeEveryDestinationSource(t *testing.T) {
	cases := []struct {
		name string
		// config, when true, saves the destination in config.toml;
		// otherwise it is recorded in the store only, as the add flow's
		// destination picker does (T-074).
		config bool
	}{
		{name: "store destination", config: false},
		{name: "config saved destination", config: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sandbox(t)
			guardDefaultTransport(t)

			dest := t.TempDir()
			rt := &recordingTransport{}

			opts := testOptions(rt)
			if tc.config {
				opts.configure = func(c *config.Config) {
					c.SavedDestinations = append(c.SavedDestinations, dest)
				}
			}

			first, err := New(opts)
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			if !tc.config {
				if err := first.store.TouchDestination(dest); err != nil {
					t.Fatalf("TouchDestination: %v", err)
				}

				if err := first.Engine().AddRoot(dest); err != nil {
					t.Fatalf("AddRoot: %v", err)
				}
			}

			if _, err := first.Engine().Add(context.Background(), engine.AddSource{Magnet: rootsMagnet, SavePath: dest}); err != nil {
				t.Fatalf("Add under %s: %v", dest, err)
			}

			if err := first.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			var probeErr error

			opts.beforeResume = func(e *anacrolix.Engine) {
				id, err := e.Add(context.Background(), engine.AddSource{Magnet: rootsProbeMagnet, SavePath: dest})
				if err != nil {
					probeErr = err
					return
				}

				probeErr = e.Remove(id, false)
			}

			second, err := New(opts)
			if err != nil {
				t.Fatalf("restart New: %v", err)
			}

			defer func() {
				if err := second.Close(); err != nil {
					t.Errorf("restart Close: %v", err)
				}
			}()

			if probeErr != nil {
				t.Fatalf("engine built by the restarted root refused %s before Resume: %v (errors.Is ErrOutsideRoots = %t)",
					dest, probeErr, errors.Is(probeErr, anacrolix.ErrOutsideRoots))
			}

			report := second.ResumeReport()
			if report.Restored != 1 {
				t.Fatalf("restart restored %d torrents, want 1", report.Restored)
			}

			if len(report.Failed) != 0 || len(report.Missing) != 0 {
				t.Fatalf("restart failed=%v missing=%v, want none", report.Failed, report.Missing)
			}

			want, err := engine.CleanAbsPath(dest)
			if err != nil {
				t.Fatalf("CleanAbsPath: %v", err)
			}

			list := second.Engine().List()
			if len(list) != 1 {
				t.Fatalf("restarted engine tracks %d torrents, want 1", len(list))
			}

			for _, st := range list {
				if st.State == engine.StateErrored {
					t.Fatalf("restored torrent %s errored: %v", st.ID, st.Err)
				}

				if st.SavePath != want {
					t.Errorf("restored torrent SavePath = %q, want %q", st.SavePath, want)
				}
			}
		})
	}
}
