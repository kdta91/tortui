package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func openTest(t *testing.T, interval time.Duration) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tortui.db")
	s, err := open(path, interval)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})
	return s, path
}

func TestSetGetDeleteTorrent(t *testing.T) {
	s, _ := openTest(t, time.Hour)

	rec := TorrentRecord{
		ID:        "t1",
		IndexerID: "example",
		SourceURL: "https://example.org/t1",
		AddedAt:   time.Now().UTC().Truncate(time.Second),
		SavePath:  "/downloads/t1",
	}
	if err := s.SetTorrent(rec); err != nil {
		t.Fatalf("SetTorrent: %v", err)
	}

	got, ok := s.GetTorrent("t1")
	if !ok {
		t.Fatalf("GetTorrent: not found")
	}
	if !reflect.DeepEqual(got, rec) {
		t.Fatalf("GetTorrent = %+v, want %+v", got, rec)
	}

	list := s.ListTorrents()
	if len(list) != 1 || !reflect.DeepEqual(list[0], rec) {
		t.Fatalf("ListTorrents = %+v", list)
	}

	if err := s.DeleteTorrent("t1"); err != nil {
		t.Fatalf("DeleteTorrent: %v", err)
	}
	if _, ok := s.GetTorrent("t1"); ok {
		t.Fatalf("GetTorrent found deleted record")
	}
	if err := s.DeleteTorrent("does-not-exist"); err != nil {
		t.Fatalf("DeleteTorrent on missing id: %v", err)
	}
}

func TestSetTorrentEmptyID(t *testing.T) {
	s, _ := openTest(t, time.Hour)
	if err := s.SetTorrent(TorrentRecord{}); !errors.Is(err, ErrEmptyID) {
		t.Fatalf("SetTorrent(empty id) = %v, want ErrEmptyID", err)
	}
}

func TestHistoryOrderAndCap(t *testing.T) {
	s, _ := openTest(t, time.Hour)

	for i := 0; i < maxHistoryEntries+10; i++ {
		if err := s.AddHistory(string(rune('a' + i%26))); err != nil {
			t.Fatalf("AddHistory: %v", err)
		}
	}

	hist := s.ListHistory()
	if len(hist) != maxHistoryEntries {
		t.Fatalf("len(ListHistory) = %d, want %d", len(hist), maxHistoryEntries)
	}
	// Most recent first: the very last AddHistory call used i = cap+9.
	want := string(rune('a' + (maxHistoryEntries+9)%26))
	if hist[0].Text != want {
		t.Fatalf("ListHistory[0].Text = %q, want %q", hist[0].Text, want)
	}
}

func TestPrefsRoundTrip(t *testing.T) {
	s, _ := openTest(t, time.Hour)

	p := Prefs{SortColumn: "seeders", LastScreen: "downloads", SelectedSources: []string{"a", "b"}}
	if err := s.SetPrefs(p); err != nil {
		t.Fatalf("SetPrefs: %v", err)
	}

	got := s.GetPrefs()
	if got.SortColumn != p.SortColumn || got.LastScreen != p.LastScreen {
		t.Fatalf("GetPrefs = %+v, want %+v", got, p)
	}
	if len(got.SelectedSources) != 2 || got.SelectedSources[0] != "a" {
		t.Fatalf("GetPrefs.SelectedSources = %v", got.SelectedSources)
	}

	// Mutating the returned slice must not corrupt the store's copy.
	got.SelectedSources[0] = "mutated"
	again := s.GetPrefs()
	if again.SelectedSources[0] != "a" {
		t.Fatalf("GetPrefs leaked internal slice: %v", again.SelectedSources)
	}
}

func TestFlushAndReopenPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tortui.db")

	s, err := open(path, time.Hour)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	rec := TorrentRecord{ID: "t1", SavePath: "/downloads/t1", AddedAt: time.Now().UTC().Truncate(time.Second)}
	if err := s.SetTorrent(rec); err != nil {
		t.Fatalf("SetTorrent: %v", err)
	}
	if err := s.AddHistory("ubuntu"); err != nil {
		t.Fatalf("AddHistory: %v", err)
	}
	if err := s.SetPrefs(Prefs{SortColumn: "size"}); err != nil {
		t.Fatalf("SetPrefs: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := open(path, time.Hour)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = s2.Close() }()

	got, ok := s2.GetTorrent("t1")
	if !ok || !reflect.DeepEqual(got, rec) {
		t.Fatalf("GetTorrent after reopen = %+v, %v", got, ok)
	}
	hist := s2.ListHistory()
	if len(hist) != 1 || hist[0].Text != "ubuntu" {
		t.Fatalf("ListHistory after reopen = %+v", hist)
	}
	if s2.GetPrefs().SortColumn != "size" {
		t.Fatalf("GetPrefs after reopen = %+v", s2.GetPrefs())
	}
}

func TestClosedStoreRejectsWrites(t *testing.T) {
	s, _ := openTest(t, time.Hour)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := s.SetTorrent(TorrentRecord{ID: "t1"}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetTorrent after Close = %v, want ErrClosed", err)
	}
	if err := s.DeleteTorrent("t1"); !errors.Is(err, ErrClosed) {
		t.Fatalf("DeleteTorrent after Close = %v, want ErrClosed", err)
	}
	if err := s.AddHistory("x"); !errors.Is(err, ErrClosed) {
		t.Fatalf("AddHistory after Close = %v, want ErrClosed", err)
	}
	if err := s.SetPrefs(Prefs{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetPrefs after Close = %v, want ErrClosed", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestOpenRefusesFutureSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tortui.db")

	// Write a database stamped with a schema version newer than this
	// build understands.
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("bolt.Open: %v", err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(bucketMeta)
		if err != nil {
			return err
		}
		return putSchemaVersion(b, currentSchemaVersion+1)
	})
	if err != nil {
		t.Fatalf("seed future schema version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	_, err = Open(path)
	if !errors.Is(err, ErrUnsupportedSchemaVersion) {
		t.Fatalf("Open(future schema) = %v, want ErrUnsupportedSchemaVersion", err)
	}
}

func TestFlushIsANoOpWhenNotDirty(t *testing.T) {
	s, _ := openTest(t, time.Hour)
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush on clean store: %v", err)
	}
}

func TestTorrentResumeFieldsSurviveReopen(t *testing.T) {
	s, path := openTest(t, time.Hour)

	rec := TorrentRecord{
		ID: "t1", Name: "example", Magnet: "magnet:?xt=urn:btih:aa", TorrentURL: "https://example.org/t1.torrent",
		Metainfo: []byte("d4:infod4:name7:exampleee"), SavePath: "/downloads", AddedAt: time.Unix(1700000000, 0).UTC(),
	}
	if err := s.SetTorrent(rec); err != nil {
		t.Fatalf("SetTorrent: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := open(path, time.Hour)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	got, ok := reopened.GetTorrent("t1")
	if !ok || got.Name != rec.Name || got.Magnet != rec.Magnet || got.TorrentURL != rec.TorrentURL ||
		string(got.Metainfo) != string(rec.Metainfo) || !got.AddedAt.Equal(rec.AddedAt) {
		t.Fatalf("reopened record = %+v, want %+v", got, rec)
	}
}

func TestClearHistoryPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tortui.db")

	s, err := open(path, time.Hour)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, q := range []string{"one", "two"} {
		if err := s.AddHistory(q); err != nil {
			t.Fatalf("AddHistory: %v", err)
		}
	}
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if err := s.ClearHistory(); err != nil {
		t.Fatalf("ClearHistory: %v", err)
	}
	if got := s.ListHistory(); len(got) != 0 {
		t.Fatalf("ListHistory after clear = %+v, want empty", got)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := open(path, time.Hour)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = s2.Close() }()

	if got := s2.ListHistory(); len(got) != 0 {
		t.Fatalf("ListHistory after reopen = %+v, want empty", got)
	}
	// New queries still record after a clear.
	if err := s2.AddHistory("three"); err != nil {
		t.Fatalf("AddHistory: %v", err)
	}
	if got := s2.ListHistory(); len(got) != 1 || got[0].Text != "three" {
		t.Fatalf("ListHistory = %+v", got)
	}
}

func TestClearHistoryOnClosedStore(t *testing.T) {
	s, _ := openTest(t, time.Hour)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.ClearHistory(); !errors.Is(err, ErrClosed) {
		t.Fatalf("ClearHistory after Close = %v, want ErrClosed", err)
	}
}

// TestTorrentPausedSurvivesReopen is T-952: the user's pause is stored with
// the record. A record written before the field existed — the same schema
// version, no "paused" key — still loads, unpaused, with its other fields
// intact, so no migration is needed.
func TestTorrentPausedSurvivesReopen(t *testing.T) {
	s, path := openTest(t, time.Hour)

	if err := s.SetTorrent(TorrentRecord{ID: "t1", SavePath: "/downloads", Paused: true}); err != nil {
		t.Fatalf("SetTorrent: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// An older build's record, written straight into the bucket.
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("bolt.Open: %v", err)
	}

	legacy := `{"id":"old","added_at":"2026-01-02T03:04:05Z","save_path":"/downloads","name":"older"}`

	err = db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(bucketTorrents).Put([]byte("old"), []byte(legacy))
	})
	if err != nil {
		t.Fatalf("write legacy record: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	reopened, err := open(path, time.Hour)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	if got, ok := reopened.GetTorrent("t1"); !ok || !got.Paused {
		t.Errorf("reopened paused record = %+v (found %v), want Paused", got, ok)
	}

	got, ok := reopened.GetTorrent("old")
	if !ok || got.Paused || got.Name != "older" || got.SavePath != "/downloads" ||
		!got.AddedAt.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Errorf("legacy record = %+v (found %v), want it loaded unpaused with its fields", got, ok)
	}
}
