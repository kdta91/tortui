package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/store"
)

// keyedAddress is a .torrent address with an api key in its query, the way
// an indexer manager publishes one (T-9057). Invented host.
const (
	nameSentinel = "SENTINEL-9057"
	keyedAddress = "https://feed.example.org/api?t=get&id=9057&apikey=" + nameSentinel
	keyedHost    = "torrent file from feed.example.org"
	keyedTitle   = "Synthetic Corpus 9057"
)

// TestDownloadNameNeverShowsTheAddress drives every place the downloads
// screen names a torrent — the row, the error detail, the remove dialog, and
// the pause, open, and source-page messages — for a torrent the engine names
// by its keyed address, as it did before T-9057. The row shows the title the
// add flow recorded until metadata arrives, then the engine's name; with no
// usable title, the address's host. The api key never renders.
func TestDownloadNameNeverShowsTheAddress(t *testing.T) {
	fetchErr := errors.New("anacrolix: torrent fake-1: fetch torrent file: httpx: GET feed.example.org: HTTP 500")

	cases := []struct {
		name     string
		record   *store.TorrentRecord // nil: no record
		status   func(*engine.TorrentStatus)
		wantName string
	}{
		{
			name:     "title before metadata",
			record:   &store.TorrentRecord{Name: keyedTitle},
			wantName: keyedTitle,
		},
		{
			name:   "title after a failed fetch",
			record: &store.TorrentRecord{Name: keyedTitle},
			status: func(s *engine.TorrentStatus) {
				s.State, s.Err = engine.StateErrored, fetchErr
			},
			wantName: keyedTitle,
		},
		{
			name:   "engine name once metadata is known",
			record: &store.TorrentRecord{Name: keyedTitle},
			status: func(s *engine.TorrentStatus) {
				s.Name, s.TotalBytes = "corpus-9057.iso", 1024
			},
			wantName: "corpus-9057.iso",
		},
		{
			name:     "no record",
			wantName: keyedHost,
		},
		{
			name:     "blank recorded title",
			record:   &store.TorrentRecord{Name: "  "},
			wantName: keyedHost,
		},
		{
			name:     "recorded address from an older session",
			record:   &store.TorrentRecord{Name: keyedAddress},
			wantName: keyedHost,
		},
		{
			name:   "engine name an address after metadata",
			record: &store.TorrentRecord{Name: keyedTitle},
			status: func(s *engine.TorrentStatus) {
				s.TotalBytes = 1024
			},
			wantName: keyedHost,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eng := &recordingEngine{Engine: newTestEngine(t), failPause: errors.New("synthetic pause failure")}
			t.Cleanup(func() { _ = eng.Close() })

			id, err := eng.Add(context.Background(), engine.AddSource{TorrentURL: keyedAddress})
			if err != nil {
				t.Fatalf("Add: %v", err)
			}

			ts := &stubTorrentStore{}
			if tc.record != nil {
				rec := *tc.record
				rec.ID, rec.TorrentURL = id, keyedAddress
				ts.records = append(ts.records, rec)
			}

			m := New(eng, testTheme(), WithTorrentStore(ts))
			m.screen = ScreenDownloads
			m.width, m.height = 200, 40

			// The engine's name before T-9057, and no metadata yet.
			statuses := eng.List()
			statuses[0].Name = keyedAddress
			statuses[0].TotalBytes, statuses[0].DownloadedBytes, statuses[0].Progress = 0, 0, 0
			if tc.status != nil {
				tc.status(&statuses[0])
			}

			updated, _ := m.Update(engineUpdateMsg{statuses: statuses})
			m = updated.(Model)

			assertShowsName(t, "the row", m.View(), tc.wantName)

			// enter expands an errored row's reason in full.
			detail, _ := press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
			assertNoKey(t, "the error detail", detail.View())

			if statuses[0].State == engine.StateErrored {
				if !strings.Contains(detail.View(), "error: "+fetchErr.Error()) {
					t.Errorf("the expanded error detail is not shown:\n%s", detail.View())
				}
			}

			removing, _ := press(t, m, keyRune("x"))
			assertShowsName(t, "the remove dialog", removing.View(), tc.wantName)

			// p on an errored row says so; on any other it pauses, and the
			// engine's refusal names the torrent.
			paused, cmd := press(t, m, keyRune("p"))
			if statuses[0].State != engine.StateErrored {
				paused, _ = runCmd(t, paused, cmd)
			}

			assertMessageNames(t, "p", paused, tc.wantName)

			opened, _ := press(t, m, keyRune("o"))
			assertMessageNames(t, "o", opened, tc.wantName)

			source, _ := press(t, m, keyRune("u"))
			assertMessageNames(t, "u", source, tc.wantName)
		})
	}
}

// assertMessageNames checks the status message an action left names want,
// and neither it nor the screen shows the keyed address.
func assertMessageNames(t *testing.T, key string, m Model, want string) {
	t.Helper()

	if msg := m.statusBar.Message(); !strings.Contains(msg, want) {
		t.Errorf("the message after %s, %q, does not name %q", key, msg, want)
	}

	assertNoKey(t, "the message after "+key, m.statusBar.Message())
	assertNoKey(t, "the screen after "+key, m.View())
}

func assertShowsName(t *testing.T, what, view, want string) {
	t.Helper()

	if !strings.Contains(view, want) {
		t.Errorf("%s does not show %q:\n%s", what, want, view)
	}

	assertNoKey(t, what, view)
}

func assertNoKey(t *testing.T, what, text string) {
	t.Helper()

	if strings.Contains(text, nameSentinel) || strings.Contains(text, "apikey=") {
		t.Errorf("%s shows the keyed address:\n%s", what, text)
	}
}
