package anacrolix

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kdta91/tortui/internal/engine"
)

// testOrigin is an invented origin; it names no real source (AGENT.md §2).
var testOrigin = engine.Origin{IndexerID: "example-src", SourceURL: "https://example.org/t/1"}

// sessionOne runs a first session in dir: adds a torrent whose data is
// already on disk, verifies it to completion, and returns what it would save
// for a restart, after closing the engine the way quitting would.
func sessionOne(t *testing.T, dir string) engine.ResumeData {
	t.Helper()

	e := newTestEngine(t, func(o *Options) { o.Config.DownloadDir = dir })

	id, err := e.Add(context.Background(), engine.AddSource{FilePath: completeTorrent(t, dir, "payload.bin", 100000)})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	waitForStarted(t, e, id)
	verify(t, e, id)
	waitUntil(t, "session one complete", func() bool { return statusOf(t, e, id).Progress == 1 })

	d, err := e.ResumeData(id)
	if err != nil {
		t.Fatalf("ResumeData: %v", err)
	}

	if len(d.Metainfo) == 0 || d.SavePath != dir || d.ID != id {
		t.Fatalf("ResumeData = id %q save %q metainfo %d bytes; want id %q, save %q, metainfo set",
			d.ID, d.SavePath, len(d.Metainfo), id, dir)
	}

	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Stands in for the store: origin is what the add flow recorded.
	d.Origin = testOrigin

	return d
}

// flipFirstByte corrupts a verified piece without changing the file's size.
// Only the persistent piece record can still report that piece complete; an
// engine that re-hashed the data instead (what the library does when it has
// no record) would find it bad. That is what lets a test prove a resume came
// from the record, not from a re-hash.
func flipFirstByte(t *testing.T, path string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	data[0] ^= 0xff
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("rewrite %s: %v", path, err)
	}
}

// TestRestoreResumesFromExistingDataWithoutDownloading is the T-041 core
// criterion. The second engine is offline — it can reach no peer — and is
// never asked to re-hash, so the only way the torrent can read complete is
// from the piece record and data the first session left on disk (see
// flipFirstByte for why a re-hash cannot fake it).
func TestRestoreResumesFromExistingDataWithoutDownloading(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	d := sessionOne(t, dir)
	flipFirstByte(t, filepath.Join(dir, "payload.bin"))

	e := newTestEngine(t, func(o *Options) { o.Config.DownloadDir = dir })

	id, err := e.Restore(context.Background(), d)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if id != d.ID {
		t.Fatalf("Restore id = %q, want the saved id %q", id, d.ID)
	}

	waitUntil(t, "restored torrent complete from disk", func() bool {
		st := statusOf(t, e, id)
		return st.Progress == 1 && (st.State == engine.StateSeeding || st.State == engine.StatePaused)
	})

	st := statusOf(t, e, id)
	if st.Origin != testOrigin {
		t.Fatalf("restored Origin = %+v, want %+v", st.Origin, testOrigin)
	}

	if st.Err != nil || st.Name != "payload.bin" || st.SavePath != dir {
		t.Fatalf("restored status = %+v", st)
	}
}

// TestRestoreResumesAPartialDownloadFromItsVerifiedPieces is the half-way
// case: session one has verified only the first half of the pieces. Session
// two — offline, never asked to re-hash — must come back at that progress
// from the persistent piece record. (With the library's default part files,
// every open marked a ".part" file's pieces incomplete and this came back at
// 0.)
func TestRestoreResumesAPartialDownloadFromItsVerifiedPieces(t *testing.T) {
	t.Parallel()

	const pieces, size = 8, 8 * testPieceLength

	dir := t.TempDir()
	e := newTestEngine(t, func(o *Options) { o.Config.DownloadDir = dir })

	// Build the whole payload's piece table, then leave only the first
	// half of it on disk.
	path := completeTorrent(t, dir, "payload.bin", size)
	full, err := os.ReadFile(filepath.Join(dir, "payload.bin"))
	if err != nil {
		t.Fatalf("read payload: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "payload.bin"), full[:size/2], 0o600); err != nil {
		t.Fatalf("truncate payload: %v", err)
	}

	id, err := e.Add(context.Background(), engine.AddSource{FilePath: path})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	waitForStarted(t, e, id)
	verify(t, e, id)
	waitUntil(t, "session one half verified", func() bool { return statusOf(t, e, id).Progress == 0.5 })

	d, err := e.ResumeData(id)
	if err != nil {
		t.Fatalf("ResumeData: %v", err)
	}

	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// A re-hash would report 3/8 here, not 4/8 (see flipFirstByte).
	flipFirstByte(t, filepath.Join(dir, "payload.bin"))

	e2 := newTestEngine(t, func(o *Options) { o.Config.DownloadDir = dir })

	rid, err := e2.Restore(context.Background(), d)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	waitUntil(t, "restored partial download at its verified progress", func() bool {
		st := statusOf(t, e2, rid)
		return st.State == engine.StateDownloading && st.Progress == 0.5
	})

	if st := statusOf(t, e2, rid); st.DownloadedBytes != int64(pieces/2*testPieceLength) {
		t.Fatalf("restored downloaded bytes = %d, want %d", st.DownloadedBytes, pieces/2*testPieceLength)
	}
}

func TestRestoreSurfacesMissingDataAsErroredNotDropped(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	d := sessionOne(t, dir)

	if err := os.Remove(filepath.Join(dir, "payload.bin")); err != nil {
		t.Fatalf("delete payload: %v", err)
	}

	e := newTestEngine(t, func(o *Options) { o.Config.DownloadDir = dir })

	id, err := e.Restore(context.Background(), d)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}

	st := statusOf(t, e, id)
	if st.State != engine.StateErrored || !errors.Is(st.Err, engine.ErrDataMissing) {
		t.Fatalf("restored with data missing: state %s err %v, want errored with ErrDataMissing", st.State, st.Err)
	}

	if msg := st.Err.Error(); !strings.Contains(msg, "remove this entry") || !strings.Contains(msg, "payload.bin") {
		t.Fatalf("missing-data message %q does not name the data or offer removal", msg)
	}

	if st.Origin != testOrigin || st.Name != "payload.bin" {
		t.Fatalf("errored entry lost its identity: %+v", st)
	}

	// It must not quietly start the download over.
	if _, err := os.Stat(filepath.Join(dir, "payload.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing torrent started re-downloading: stat = %v", err)
	}

	// The entry survives another restart until the user removes it.
	again, err := e.ResumeData(id)
	if err != nil || len(again.Metainfo) == 0 || again.Origin != testOrigin {
		t.Fatalf("ResumeData of errored entry = %+v, %v; want metainfo and origin kept", again, err)
	}

	if err := e.Remove(id, true); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if n := len(e.List()); n != 0 {
		t.Fatalf("List after Remove has %d torrents, want 0", n)
	}
}

func TestResumeDataCarriesTheSourceBeforeMetadata(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)
	magnet := magnetURI("resume-magnet")

	id, err := e.Add(context.Background(), engine.AddSource{Magnet: magnet})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	d, err := e.ResumeData(id)
	if err != nil {
		t.Fatalf("ResumeData: %v", err)
	}

	if d.Magnet != magnet || len(d.Metainfo) != 0 || d.SavePath != e.downloadDir {
		t.Fatalf("ResumeData = %+v, want the magnet and no metainfo", d)
	}

	url := serveTorrent(t, encodeTorrent(t, buildInfo("fetched", [][]string{{"a.bin"}})))

	uid, err := e.Add(context.Background(), engine.AddSource{TorrentURL: url})
	if err != nil {
		t.Fatalf("Add URL: %v", err)
	}

	if d, err := e.ResumeData(uid); err != nil || d.TorrentURL != url {
		t.Fatalf("ResumeData(url) = %+v, %v; want the URL", d, err)
	}
}

func TestRestoreFromMagnetAndURLKeepsIDAndOrigin(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)

	id, err := e.Restore(context.Background(), engine.ResumeData{
		ID: "an-7", Magnet: magnetURI("restore-magnet"), SavePath: e.downloadDir, Origin: testOrigin,
	})
	if err != nil || id != "an-7" {
		t.Fatalf("Restore(magnet) = %q, %v; want an-7", id, err)
	}

	if st := statusOf(t, e, id); st.Origin != testOrigin || st.State == engine.StateErrored {
		t.Fatalf("restored magnet status = %+v", st)
	}

	// A later Add never collides with a restored ID.
	next, err := e.Add(context.Background(), engine.AddSource{Magnet: magnetURI("after-restore")})
	if err != nil || next != "an-8" {
		t.Fatalf("Add after restoring an-7 = %q, %v; want an-8", next, err)
	}

	// A saved ID already in use is not reused.
	url := serveTorrent(t, encodeTorrent(t, buildInfo("fetched", [][]string{{"a.bin"}})))

	uid, err := e.Restore(context.Background(), engine.ResumeData{ID: "an-7", TorrentURL: url})
	if err != nil || uid == "an-7" || uid == "" {
		t.Fatalf("Restore(url, taken id) = %q, %v; want a fresh id", uid, err)
	}

	waitForStarted(t, e, uid)
}

func TestRestoreTracksUnresumableTorrentsAsErrored(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)
	outside := t.TempDir()

	malicious, err := os.ReadFile(fixturePath("traversal.torrent"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	cases := []struct {
		name string
		data engine.ResumeData
		want error
	}{
		{"no source", engine.ResumeData{ID: "an-1"}, ErrNoSource},
		{"outside roots", engine.ResumeData{ID: "an-2", Magnet: magnetURI("outside"), SavePath: outside}, ErrOutsideRoots},
		{"unsafe metainfo", engine.ResumeData{ID: "an-3", Metainfo: malicious}, ErrUnsafePath},
		{"garbage metainfo", engine.ResumeData{ID: "an-4", Metainfo: []byte("not bencode")}, nil},
		{"bad magnet", engine.ResumeData{ID: "an-5", Magnet: "magnet:?xt=nonsense"}, nil},
	}

	for _, tc := range cases {
		id, err := e.Restore(context.Background(), tc.data)
		if err != nil {
			t.Fatalf("%s: Restore error %v, want the torrent tracked as errored", tc.name, err)
		}

		st := statusOf(t, e, id)
		if st.State != engine.StateErrored || st.Err == nil {
			t.Fatalf("%s: state %s err %v, want errored", tc.name, st.State, st.Err)
		}

		if tc.want != nil && !errors.Is(st.Err, tc.want) {
			t.Fatalf("%s: err %v, want %v", tc.name, st.Err, tc.want)
		}
	}

	// Nothing unsafe was written: the traversal fixture left no file
	// behind outside the download dir.
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("outside dir has %d entries, want 0", len(entries))
	}
}

func TestRestoreAndResumeDataErrors(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)

	if _, err := e.ResumeData("an-404"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ResumeData(unknown) = %v, want ErrNotFound", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := e.Restore(ctx, engine.ResumeData{Magnet: magnetURI("x")}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Restore(cancelled) = %v, want context.Canceled", err)
	}

	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := e.Restore(context.Background(), engine.ResumeData{Magnet: magnetURI("x")}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Restore after Close = %v, want ErrClosed", err)
	}

	if _, err := e.Restore(context.Background(), engine.ResumeData{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("Restore(no source) after Close = %v, want ErrClosed", err)
	}

	if _, err := e.ResumeData("an-1"); !errors.Is(err, ErrClosed) {
		t.Fatalf("ResumeData after Close = %v, want ErrClosed", err)
	}
}

// TestAddRefusesAMagnetWithNoInfohash: the library parses such a magnet and
// then panics when it is added, so both Add and Restore refuse it first.
func TestAddRefusesAMagnetWithNoInfohash(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)

	if _, err := e.Add(context.Background(), engine.AddSource{Magnet: "magnet:?xt=nonsense"}); err == nil ||
		!strings.Contains(err.Error(), "no infohash") {
		t.Fatalf("Add(no infohash) = %v, want a refusal", err)
	}
}
