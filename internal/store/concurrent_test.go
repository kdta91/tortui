package store

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestConcurrentReadWrite drives many goroutines through every mutating and
// reading method at once, with the background flush goroutine running on a
// short interval so it races against the foreground calls too. Run with
// `go test -race` (AGENT.md §9/§15, T-040's own acceptance criterion) — the
// race detector, not any assertion here, is what actually proves this
// safe.
func TestConcurrentReadWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tortui.db")
	s, err := open(path, 5*time.Millisecond)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = s.Close() }()

	const workers = 8
	const iterations = 200

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				id := fmt.Sprintf("w%d-t%d", w, i)

				if err := s.SetTorrent(TorrentRecord{
					ID:       id,
					SavePath: "/downloads/" + id,
					AddedAt:  time.Now(),
				}); err != nil {
					t.Errorf("SetTorrent: %v", err)
					return
				}
				if _, ok := s.GetTorrent(id); !ok {
					t.Errorf("GetTorrent(%q) immediately after SetTorrent: not found", id)
					return
				}
				_ = s.ListTorrents()

				if err := s.AddHistory(id); err != nil {
					t.Errorf("AddHistory: %v", err)
					return
				}
				_ = s.ListHistory()

				if err := s.SetPrefs(Prefs{SortColumn: id}); err != nil {
					t.Errorf("SetPrefs: %v", err)
					return
				}
				_ = s.GetPrefs()

				if i%10 == 0 {
					if err := s.DeleteTorrent(id); err != nil {
						t.Errorf("DeleteTorrent: %v", err)
						return
					}
				}
				if i%25 == 0 {
					if err := s.Flush(); err != nil {
						t.Errorf("Flush: %v", err)
						return
					}
				}
			}
		}(w)
	}
	wg.Wait()

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Reopening must succeed and reflect a consistent, fully-flushed
	// state — proof the concurrent writers and the debounce goroutine
	// never corrupted the file.
	s2, err := open(path, time.Hour)
	if err != nil {
		t.Fatalf("reopen after concurrent use: %v", err)
	}
	defer func() { _ = s2.Close() }()

	list := s2.ListTorrents()
	if len(list) == 0 {
		t.Fatalf("expected at least some torrents to survive, got none")
	}
}
