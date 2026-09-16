package lifecycle

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kdta91/tortui/internal/store"
)

// TestOpenStoreCorruptFileIsQuarantinedAndRecreated reproduces "a corrupt
// or unreadable store is renamed aside with a timestamp and recreated
// empty, with the user told what happened and where the old file went"
// (T-042).
func TestOpenStoreCorruptFileIsQuarantinedAndRecreated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tortui.db")

	// Not a bbolt file at all — bolt.Open will refuse it as invalid.
	const garbage = "this is not a bbolt database\x00\x01\x02"
	if err := os.WriteFile(path, []byte(garbage), 0o600); err != nil {
		t.Fatalf("write corrupt store file: %v", err)
	}

	st, notice, err := OpenStore(path, slog.Default())
	if err != nil {
		t.Fatalf("OpenStore() error = %v, want recovery instead of a fatal error", err)
	}
	defer func() { _ = st.Close() }()

	if notice == "" {
		t.Fatal("notice is empty, want a message telling the user what happened")
	}

	// The new store at path must be a fresh, empty, working store.
	if got := st.ListTorrents(); len(got) != 0 {
		t.Fatalf("ListTorrents() = %v, want an empty freshly recreated store", got)
	}
	if err := st.SetTorrent(store.TorrentRecord{ID: "t1"}); err != nil {
		t.Fatalf("SetTorrent on recreated store: %v", err)
	}

	// The corrupt original must have been preserved, not deleted, so the
	// user can inspect or recover it later.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	var quarantined string
	for _, e := range entries {
		if e.Name() != "tortui.db" && strings.HasPrefix(e.Name(), "tortui.db.corrupt-") {
			quarantined = filepath.Join(dir, e.Name())
		}
	}
	if quarantined == "" {
		t.Fatalf("no quarantined file found among %v, want one named tortui.db.corrupt-<timestamp>", entries)
	}
	if !strings.Contains(notice, quarantined) {
		t.Fatalf("notice %q does not mention the quarantined file path %q", notice, quarantined)
	}

	got, err := os.ReadFile(quarantined)
	if err != nil {
		t.Fatalf("read quarantined file: %v", err)
	}
	if string(got) != garbage {
		t.Fatalf("quarantined file content = %q, want the original corrupt bytes %q", got, garbage)
	}
}

// TestOpenStoreValidFileOpensNormally is the non-corrupt control case: an
// existing, healthy store opens with no notice and no quarantining.
func TestOpenStoreValidFileOpensNormally(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tortui.db")

	st1, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	if err := st1.SetTorrent(store.TorrentRecord{ID: "keep-me"}); err != nil {
		t.Fatalf("SetTorrent: %v", err)
	}
	if err := st1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	st2, notice, err := OpenStore(path, slog.Default())
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer func() { _ = st2.Close() }()

	if notice != "" {
		t.Fatalf("notice = %q, want empty for a healthy store", notice)
	}

	rec, ok := st2.GetTorrent("keep-me")
	if !ok || rec.ID != "keep-me" {
		t.Fatalf("GetTorrent(keep-me) = %+v, %v, want the record written before OpenStore", rec, ok)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("dir entries = %v, want exactly the original store file, nothing quarantined", entries)
	}
}

// TestOpenStoreMissingParentDirIsNotQuarantined checks that an open
// failure unrelated to corrupt content (no store file exists yet, and its
// directory cannot be created) is surfaced as a plain error rather than
// treated as something to quarantine.
func TestOpenStoreMissingParentDirIsNotQuarantined(t *testing.T) {
	// A regular file used as a directory component: MkdirAll underneath it
	// must fail, and there is no pre-existing file at the final path to
	// quarantine in the first place.
	base := t.TempDir()
	blocker := filepath.Join(base, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}
	path := filepath.Join(blocker, "sub", "tortui.db")

	_, _, err := OpenStore(path, slog.Default())
	if err == nil {
		t.Fatal("OpenStore() error = nil, want an error when the store's directory cannot be created")
	}
}
