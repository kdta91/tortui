package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/engine/anacrolix"
	"github.com/kdta91/tortui/internal/engine/fake"
	"github.com/kdta91/tortui/internal/store"
)

// resumeEngine is engine/fake plus an in-memory engine.Resumer, so Session's
// own logic — ordering, re-keying, reporting, pruning — is tested without a
// real client. A restored torrent named "gone" comes back with its data
// missing, and one named "broken" fails for another reason.
type resumeEngine struct {
	*fake.Engine
	data map[string]engine.ResumeData
	next fake.Script
}

func newResumeEngine() *resumeEngine {
	r := &resumeEngine{Engine: fake.New(), data: map[string]engine.ResumeData{}}
	r.ScriptFor = func(engine.AddSource) fake.Script { return r.next }

	return r
}

func (r *resumeEngine) Restore(ctx context.Context, d engine.ResumeData) (string, error) {
	switch d.Name {
	case "gone":
		r.next = fake.Errored(time.Millisecond, fmt.Errorf("fake: %w", engine.ErrDataMissing))
	case "broken":
		r.next = fake.Errored(time.Millisecond, errors.New("fake: unreadable metadata"))
	default:
		r.next = fake.Downloading(time.Minute)
	}

	id, err := r.Add(ctx, engine.AddSource{Magnet: d.Magnet, SavePath: d.SavePath})
	if err != nil {
		return "", err
	}

	// Errored scripts fail just after the start; step every torrent
	// past it (harmless for the others).
	r.Advance(time.Second)

	d.ID = id
	r.data[id] = d

	return id, nil
}

func (r *resumeEngine) ResumeData(id string) (engine.ResumeData, error) {
	for _, st := range r.List() {
		if st.ID == id {
			return r.data[id], nil
		}
	}

	return engine.ResumeData{}, fake.ErrNotFound
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func openStore(t *testing.T, path string) *store.Store {
	t.Helper()

	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}

	t.Cleanup(func() { _ = st.Close() })

	return st
}

func setRecords(t *testing.T, st *store.Store, recs ...store.TorrentRecord) {
	t.Helper()

	for _, rec := range recs {
		if err := st.SetTorrent(rec); err != nil {
			t.Fatalf("SetTorrent: %v", err)
		}
	}
}

func TestSessionResumeRestoresRekeysAndReportsMissing(t *testing.T) {
	st := openStore(t, filepath.Join(t.TempDir(), "tortui.db"))
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	setRecords(t, st,
		// Out of ID order on purpose: Resume restores oldest first.
		store.TorrentRecord{ID: "an-1", Name: "broken", Magnet: "magnet:?xt=urn:btih:c", AddedAt: base.Add(2 * time.Hour)},
		store.TorrentRecord{
			ID: "an-2", Name: "ok", Magnet: "magnet:?xt=urn:btih:a", AddedAt: base,
			IndexerID: "example-src", SourceURL: "https://example.org/t/1", SavePath: "/dl",
		},
		store.TorrentRecord{ID: "an-3", Name: "gone", Magnet: "magnet:?xt=urn:btih:b", AddedAt: base.Add(time.Hour)},
	)

	eng := newResumeEngine()
	s := NewSession(eng, st, quietLogger())

	report, err := s.Resume(context.Background())
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if report.Restored != 3 || len(report.Missing) != 1 || len(report.Failed) != 1 {
		t.Fatalf("report = %+v, want 3 restored, 1 missing, 1 failed", report)
	}

	if !errors.Is(report.Missing[0].Err, engine.ErrDataMissing) || report.Missing[0].State != engine.StateErrored {
		t.Fatalf("missing entry = %+v", report.Missing[0])
	}

	// Nothing was dropped: all three are visible in the engine.
	if n := len(eng.List()); n != 3 {
		t.Fatalf("engine tracks %d torrents, want 3", n)
	}

	// Records were re-keyed to the engine's new IDs, oldest first, with
	// origin and added-at intact.
	for _, old := range []string{"an-1", "an-2", "an-3"} {
		if _, ok := st.GetTorrent(old); ok {
			t.Fatalf("stale record %s still in store", old)
		}
	}

	rec, ok := st.GetTorrent("fake-1")
	if !ok || rec.Name != "ok" || rec.IndexerID != "example-src" || rec.SourceURL != "https://example.org/t/1" ||
		!rec.AddedAt.Equal(base) || rec.SavePath != "/dl" {
		t.Fatalf("fake-1 record = %+v, %v; want the oldest (ok) record with its origin", rec, ok)
	}

	if rec, _ := st.GetTorrent("fake-2"); rec.Name != "gone" {
		t.Fatalf("fake-2 = %+v, want gone", rec)
	}
}

func TestSessionSaveRecordsMergesAndPrunes(t *testing.T) {
	st := openStore(t, filepath.Join(t.TempDir(), "tortui.db"))
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	setRecords(t, st,
		store.TorrentRecord{ID: "fake-1", Name: "kept", Magnet: "magnet:?xt=urn:btih:a", AddedAt: base},
		store.TorrentRecord{ID: "fake-2", Name: "removed", Magnet: "magnet:?xt=urn:btih:b", AddedAt: base},
	)

	eng := newResumeEngine()
	s := NewSession(eng, st, quietLogger())
	now := base.Add(24 * time.Hour)
	s.now = func() time.Time { return now }

	// Before Resume, Save must not prune a session nothing restored yet.
	if err := s.Save(); err != nil {
		t.Fatalf("Save before Resume: %v", err)
	}

	if n := len(st.ListTorrents()); n != 2 {
		t.Fatalf("Save before Resume changed the store: %d records, want 2", n)
	}

	if _, err := s.Resume(context.Background()); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if err := eng.Remove("fake-2", false); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// A new add whose origin the add flow wrote straight to the store.
	id, err := eng.Restore(context.Background(), engine.ResumeData{Name: "new", Magnet: "magnet:?xt=urn:btih:c"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	setRecords(t, st, store.TorrentRecord{ID: id, IndexerID: "example-src", SourceURL: "https://example.org/t/3"})

	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, ok := st.GetTorrent("fake-2"); ok {
		t.Fatal("record for a removed torrent survived Save")
	}

	if rec, _ := st.GetTorrent("fake-1"); !rec.AddedAt.Equal(base) || rec.Name != "kept" {
		t.Fatalf("existing record = %+v, want added-at kept", rec)
	}

	rec, _ := st.GetTorrent(id)
	if !rec.AddedAt.Equal(now) || rec.IndexerID != "example-src" || rec.Magnet != "magnet:?xt=urn:btih:c" || rec.Name != "new" {
		t.Fatalf("new record = %+v, want added now with the stored origin kept", rec)
	}
}

func TestSessionWithoutResumerLeavesStoreAlone(t *testing.T) {
	st := openStore(t, filepath.Join(t.TempDir(), "tortui.db"))
	setRecords(t, st, store.TorrentRecord{ID: "x", Magnet: "magnet:?xt=urn:btih:a"})

	s := NewSession(fake.New(), st, quietLogger())

	report, err := s.Resume(context.Background())
	if err != nil || report.Restored != 0 {
		t.Fatalf("Resume = %+v, %v; want nothing restored", report, err)
	}

	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, ok := st.GetTorrent("x"); !ok {
		t.Fatal("record dropped by a session whose engine cannot resume")
	}
}

func TestSessionResumeFailsOnAClosedEngine(t *testing.T) {
	st := openStore(t, filepath.Join(t.TempDir(), "tortui.db"))
	setRecords(t, st, store.TorrentRecord{ID: "x", Magnet: "magnet:?xt=urn:btih:a"})

	eng := newResumeEngine()
	if err := eng.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s := NewSession(eng, st, quietLogger())
	if _, err := s.Resume(context.Background()); !errors.Is(err, fake.ErrClosed) {
		t.Fatalf("Resume on closed engine = %v, want ErrClosed", err)
	}

	// Not resumed, so a later Save still leaves the record alone.
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if _, ok := st.GetTorrent("x"); !ok {
		t.Fatal("record dropped after a failed Resume")
	}
}

// TestOriginSurvivesRestart runs two real sessions end to end — anacrolix
// engine, bbolt store, Shutdown in between — and checks the second one's
// engine reports the origin the first one recorded.
func TestOriginSurvivesRestart(t *testing.T) {
	dir, dbPath := t.TempDir(), filepath.Join(t.TempDir(), "tortui.db")
	origin := engine.Origin{IndexerID: "example-src", SourceURL: "https://example.org/t/9"}
	magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=example-fixture"

	newEngine := func() *anacrolix.Engine {
		e, err := anacrolix.New(anacrolix.Options{
			Config:  config.Config{DownloadDir: dir},
			Logger:  quietLogger(),
			Offline: true,
		})
		if err != nil {
			t.Fatalf("anacrolix.New: %v", err)
		}

		t.Cleanup(func() { _ = e.Close() })

		return e
	}

	// Session one: add, record the origin as the add flow does, quit.
	st1, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}

	e1 := newEngine()
	s1 := NewSession(e1, st1, quietLogger())

	if _, err := s1.Resume(context.Background()); err != nil {
		t.Fatalf("Resume (empty): %v", err)
	}

	id, err := e1.Add(context.Background(), engine.AddSource{Magnet: magnet})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	setRecords(t, st1, store.TorrentRecord{
		ID:        id,
		IndexerID: origin.IndexerID,
		SourceURL: origin.SourceURL,
	})

	if err := s1.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if errs := Shutdown(ShutdownOptions{Engine: e1, Store: st1, Session: s1, Logger: quietLogger()}); len(errs) != 0 {
		t.Fatalf("Shutdown: %v", errs)
	}

	// Session two: reopen and resume.
	st2 := openStore(t, dbPath)
	e2 := newEngine()

	if _, err := NewSession(e2, st2, quietLogger()).Resume(context.Background()); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	list := e2.List()
	if len(list) != 1 || list[0].ID != id || list[0].Origin != origin || list[0].State == engine.StateErrored {
		t.Fatalf("after restart List = %+v, want %s with origin %+v", list, id, origin)
	}
}
