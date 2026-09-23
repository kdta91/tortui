package anacrolix

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent/metainfo"
	"go.uber.org/goleak"
	"golang.org/x/time/rate"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/engine"
)

func TestAddAcceptsAMagnetURI(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)

	start := time.Now()

	id, err := e.Add(context.Background(), engine.AddSource{Magnet: magnetURI("magnet-accept")})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Add took %s; it must return as soon as the source is accepted", elapsed)
	}

	if id == "" {
		t.Fatal("Add returned an empty id")
	}

	st := statusOf(t, e, id)
	if st.State != engine.StateChecking {
		t.Errorf("state = %s, want %s while the info dictionary is still unknown", st.State, engine.StateChecking)
	}

	if st.InfoHash == "" {
		t.Error("InfoHash is empty; a magnet carries one from the start")
	}

	if st.TotalBytes != 0 || st.Progress != 0 {
		t.Errorf("TotalBytes = %d, Progress = %v; both must be zero before metadata", st.TotalBytes, st.Progress)
	}
}

func TestAddAcceptsALocalTorrentFile(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)
	path := writeTorrentFile(t, buildInfo("example-fixture", [][]string{{"a.bin"}, {"sub", "b.bin"}}))

	id, err := e.Add(context.Background(), engine.AddSource{FilePath: path})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	st := waitForState(t, e, id, engine.StateDownloading)

	if st.Name != "example-fixture" {
		t.Errorf("Name = %q, want the name from the info dictionary", st.Name)
	}

	if st.TotalBytes != 1024+2048 {
		t.Errorf("TotalBytes = %d, want %d", st.TotalBytes, 1024+2048)
	}

	if st.SavePath == "" || !filepath.IsAbs(st.SavePath) {
		t.Errorf("SavePath = %q, want an absolute path", st.SavePath)
	}
}

func TestAddAcceptsATorrentURL(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)
	url := serveTorrent(t, encodeTorrent(t, buildInfo("served-fixture", [][]string{{"c.bin"}})))

	id, err := e.Add(context.Background(), engine.AddSource{TorrentURL: url})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	st := waitForState(t, e, id, engine.StateDownloading)
	if st.Name != "served-fixture" {
		t.Errorf("Name = %q, want the name from the fetched info dictionary", st.Name)
	}
}

func TestAddRejectsAnEmptyAndAnAmbiguousSource(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)

	if _, err := e.Add(context.Background(), engine.AddSource{}); !errors.Is(err, ErrNoSource) {
		t.Errorf("Add with no source = %v, want ErrNoSource", err)
	}

	both := engine.AddSource{Magnet: magnetURI("ambiguous"), TorrentURL: "https://example.org/x.torrent"}
	if _, err := e.Add(context.Background(), both); !errors.Is(err, ErrAmbiguousSource) {
		t.Errorf("Add with two sources = %v, want ErrAmbiguousSource", err)
	}
}

func TestAddRejectsADestinationOutsideEveryKnownRoot(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)
	outside := filepath.Join(t.TempDir(), "elsewhere")

	src := engine.AddSource{Magnet: magnetURI("outside"), SavePath: outside}
	if _, err := e.Add(context.Background(), src); !errors.Is(err, ErrOutsideRoots) {
		t.Fatalf("Add to %s = %v, want ErrOutsideRoots", outside, err)
	}
}

func TestAddHonoursASavedDestination(t *testing.T) {
	t.Parallel()

	saved := t.TempDir()
	e := newTestEngine(t, func(o *Options) {
		o.Config.SavedDestinations = []string{saved}
	})

	dest := filepath.Join(saved, "nested")

	id, err := e.Add(context.Background(), engine.AddSource{Magnet: magnetURI("saved"), SavePath: dest})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if got := statusOf(t, e, id).SavePath; got != dest {
		t.Errorf("SavePath = %q, want %q", got, dest)
	}
}

func TestAddIsIdempotentForTheSameInfoHash(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)
	src := engine.AddSource{Magnet: magnetURI("idempotent")}

	first, err := e.Add(context.Background(), src)
	if err != nil {
		t.Fatalf("first Add: %v", err)
	}

	second, err := e.Add(context.Background(), src)
	if err != nil {
		t.Fatalf("second Add: %v", err)
	}

	if first != second {
		t.Errorf("second Add returned %q, want the existing id %q", second, first)
	}

	if n := len(e.List()); n != 1 {
		t.Errorf("List() has %d entries, want 1", n)
	}
}

func TestAddRespectsACancelledContext(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := e.Add(ctx, engine.AddSource{Magnet: magnetURI("cancelled")}); !errors.Is(err, context.Canceled) {
		t.Errorf("Add with a cancelled context = %v, want context.Canceled", err)
	}
}

func TestMetadataTimeoutMovesTheTorrentToErrored(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, func(o *Options) { o.MetadataTimeout = 60 * time.Millisecond })

	id, err := e.Add(context.Background(), engine.AddSource{Magnet: magnetURI("no-metadata")})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	st := waitForState(t, e, id, engine.StateErrored)

	if st.Err == nil {
		t.Fatal("errored torrent carries no Err")
	}

	if !errors.Is(st.Err, ErrMetadataTimeout) {
		t.Errorf("Err = %v, want it to wrap ErrMetadataTimeout", st.Err)
	}

	if msg := st.Err.Error(); !strings.Contains(msg, "info dictionary") {
		t.Errorf("Err = %q, want a message a user can act on", msg)
	}
}

func TestMetadataTimeoutIsInjectableAndDefaultsToSixtySeconds(t *testing.T) {
	t.Parallel()

	if DefaultMetadataTimeout != 60*time.Second {
		t.Errorf("DefaultMetadataTimeout = %s, want 60s (AGENT.md §13)", DefaultMetadataTimeout)
	}

	e := newTestEngine(t, func(o *Options) { o.MetadataTimeout = 0 })
	if e.metadataTimeout != DefaultMetadataTimeout {
		t.Errorf("metadataTimeout = %s, want the default", e.metadataTimeout)
	}
}

func TestAddRefusesATorrentDeclaringAnUnsafePath(t *testing.T) {
	t.Parallel()

	cases := map[string][][]string{
		"parent traversal":  {{"..", "..", "escaped.bin"}},
		"absolute looking":  {{"/etc", "passwd"}},
		"windows separator": {{`..\..\escaped.bin`}},
		"nul byte":          {{"evil\x00.bin"}},
		"reserved name":     {{"CON"}},
		"trailing dot":      {{"sneaky."}},
	}

	for name, paths := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			e := newTestEngine(t, nil)
			path := writeTorrentFile(t, buildInfo("example-fixture", paths))

			id, addErr := e.Add(context.Background(), engine.AddSource{FilePath: path})
			if addErr != nil {
				// Refused outright is an acceptable outcome; what must
				// never happen is the torrent starting.
				return
			}

			st := waitForState(t, e, id, engine.StateErrored)
			if !errors.Is(st.Err, ErrUnsafePath) {
				t.Fatalf("Err = %v, want it to wrap ErrUnsafePath", st.Err)
			}
		})
	}
}

func TestAddRefusesATorrentWhoseOwnNameEscapes(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)
	path := writeTorrentFile(t, buildInfo("../escaped", [][]string{{"a.bin"}}))

	id, err := e.Add(context.Background(), engine.AddSource{FilePath: path})
	if err != nil {
		return
	}

	st := waitForState(t, e, id, engine.StateErrored)
	if !errors.Is(st.Err, ErrUnsafePath) {
		t.Fatalf("Err = %v, want it to wrap ErrUnsafePath", st.Err)
	}
}

func TestAddRejectsAMissingOrIrregularTorrentFile(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)

	missing := filepath.Join(t.TempDir(), "nope.torrent")
	if _, err := e.Add(context.Background(), engine.AddSource{FilePath: missing}); err == nil {
		t.Error("Add with a missing file returned no error")
	}

	dir := t.TempDir()
	if _, err := e.Add(context.Background(), engine.AddSource{FilePath: dir}); err == nil {
		t.Error("Add with a directory returned no error")
	}

	if _, err := e.Add(context.Background(), engine.AddSource{FilePath: "bad\x00path"}); !errors.Is(err, ErrUnsafePath) {
		t.Error("Add with a NUL in the path did not report ErrUnsafePath")
	}
}

func TestAddReportsAnUnfetchableTorrentURL(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)
	url := serveTorrent(t, []byte("this is not a bencoded torrent"))

	id, err := e.Add(context.Background(), engine.AddSource{TorrentURL: url})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	st := waitForState(t, e, id, engine.StateErrored)
	if st.Err == nil || !strings.Contains(st.Err.Error(), "parse fetched torrent file") {
		t.Errorf("Err = %v, want a parse failure naming the fetched file", st.Err)
	}
}

func TestConfigIsAppliedToTheClient(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := config.Config{
		DownloadDir:     dir,
		MaxPeers:        11,
		MaxDownloadRate: 4096,
		MaxUploadRate:   2048,
	}

	clientCfg := clientConfig(Options{Config: cfg}, dir, discardLogger())

	if clientCfg.DataDir != dir {
		t.Errorf("DataDir = %q, want %q", clientCfg.DataDir, dir)
	}

	if clientCfg.EstablishedConnsPerTorrent != 11 {
		t.Errorf("EstablishedConnsPerTorrent = %d, want 11", clientCfg.EstablishedConnsPerTorrent)
	}

	if got := clientCfg.DownloadRateLimiter.Limit(); float64(got) != 4096 {
		t.Errorf("download limit = %v, want 4096", got)
	}

	if got := clientCfg.UploadRateLimiter.Limit(); float64(got) != 2048 {
		t.Errorf("upload limit = %v, want 2048", got)
	}

	unlimited := clientConfig(Options{Config: config.Config{DownloadDir: dir}}, dir, discardLogger())
	if unlimited.DownloadRateLimiter.Limit() != rate.Inf {
		t.Error("a zero max_download_rate must mean unlimited")
	}

	if unlimited.UploadRateLimiter.Limit() != rate.Inf {
		t.Error("a zero max_upload_rate must mean unlimited")
	}
}

func TestNewCreatesTheDownloadDirectory(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "downloads", "nested")

	e := newTestEngine(t, func(o *Options) { o.Config.DownloadDir = dir })

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat download dir: %v", err)
	}

	if !info.IsDir() {
		t.Fatalf("%s is not a directory", dir)
	}

	if e.downloadDir != dir {
		t.Errorf("downloadDir = %q, want %q", e.downloadDir, dir)
	}
}

func TestNewRejectsAnEmptyDownloadDirectory(t *testing.T) {
	t.Parallel()

	if _, err := New(Options{Logger: discardLogger(), Offline: true}); err == nil {
		t.Fatal("New with no download dir returned no error")
	}
}

func TestListMapsProgressAndRates(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)
	path := writeTorrentFile(t, buildInfo("rates-fixture", [][]string{{"a.bin"}}))

	id, err := e.Add(context.Background(), engine.AddSource{FilePath: path})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	st := waitForState(t, e, id, engine.StateDownloading)

	if st.TotalBytes != 1024 {
		t.Errorf("TotalBytes = %d, want 1024", st.TotalBytes)
	}

	if st.Progress != 0 {
		t.Errorf("Progress = %v, want 0 with nothing downloaded", st.Progress)
	}

	if st.DownRate != 0 || st.UpRate != 0 {
		t.Errorf("rates = %d/%d, want 0/0 with no peers", st.DownRate, st.UpRate)
	}

	if st.ETA != -1 {
		t.Errorf("ETA = %s, want -1 when the rate is unknown", st.ETA)
	}

	// The sampler must actually be running: it is what keeps List's rates
	// independent of how often List is called.
	if !waitForRateSample(t, e, id) {
		t.Error("no rate sample was taken; the sampler is not running")
	}
}

func TestListIsEmptyForAFreshEngine(t *testing.T) {
	t.Parallel()

	if got := newTestEngine(t, nil).List(); len(got) != 0 {
		t.Errorf("List() = %d entries, want 0", len(got))
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	e, err := New(Options{
		Config:          config.Config{DownloadDir: dir},
		Logger:          discardLogger(),
		MetadataTimeout: 50 * time.Millisecond,
		Offline:         true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := e.Add(context.Background(), engine.AddSource{Magnet: magnetURI("close")}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	for i := range 3 {
		if err := e.Close(); err != nil {
			t.Fatalf("Close #%d: %v", i+1, err)
		}
	}

	if _, err := e.Add(context.Background(), engine.AddSource{Magnet: magnetURI("after-close")}); !errors.Is(err, ErrClosed) {
		t.Errorf("Add after Close = %v, want ErrClosed", err)
	}
}

func TestCloseIsSafeFromManyGoroutines(t *testing.T) {
	t.Parallel()

	e, err := New(Options{
		Config:  config.Config{DownloadDir: t.TempDir()},
		Logger:  discardLogger(),
		Offline: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	done := make(chan error, 8)
	for range cap(done) {
		go func() { done <- e.Close() }()
	}

	for range cap(done) {
		if err := <-done; err != nil {
			t.Errorf("concurrent Close: %v", err)
		}
	}
}

func TestUpdatesChannelClosesExactlyOnce(t *testing.T) {
	t.Parallel()

	e, err := New(Options{
		Config:  config.Config{DownloadDir: t.TempDir()},
		Logger:  discardLogger(),
		Offline: true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ch := e.Updates()
	if ch == nil {
		t.Fatal("Updates() returned a nil channel")
	}

	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := e.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	if _, open := <-ch; open {
		t.Error("Updates() channel is still open after Close")
	}
}

func TestUnknownIDReportsErrNotFoundNeverPanics(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)

	if err := e.Pause("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Pause = %v, want ErrNotFound", err)
	}

	if err := e.Resume("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Resume = %v, want ErrNotFound", err)
	}

	if err := e.Remove("nope", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("Remove = %v, want ErrNotFound", err)
	}

	if _, err := e.Files("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Files = %v, want ErrNotFound", err)
	}
}

func TestConcurrentAddAndList(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)

	done := make(chan struct{})

	go func() {
		defer close(done)

		for i := range 8 {
			if _, err := e.Add(context.Background(), engine.AddSource{Magnet: magnetURI(string(rune('a' + i)))}); err != nil {
				t.Errorf("Add: %v", err)
			}
		}
	}()

	for range 50 {
		_ = e.List()
		time.Sleep(time.Millisecond)
	}

	<-done

	if n := len(e.List()); n != 8 {
		t.Errorf("List() = %d entries, want 8", n)
	}
}

func TestValidateInfoPathsAcceptsASingleFileTorrent(t *testing.T) {
	t.Parallel()

	dest := t.TempDir()
	info := metainfo.Info{Name: "single.bin", PieceLength: testPieceLength, Length: 10, Pieces: make([]byte, 20)}

	if err := validateInfoPaths(&info, dest); err != nil {
		t.Fatalf("validateInfoPaths: %v", err)
	}
}

func TestCloseLeavesNoGoroutines(t *testing.T) {
	// Deliberately not parallel: the snapshot below has to be of this
	// test's own goroutines, not of whatever else is mid-flight.
	ignore := goleak.IgnoreCurrent()

	e, err := New(Options{
		Config:             config.Config{DownloadDir: t.TempDir()},
		Logger:             discardLogger(),
		MetadataTimeout:    40 * time.Millisecond,
		RateSampleInterval: 5 * time.Millisecond,
		Offline:            true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// One torrent still waiting for metadata, one that has already failed,
	// and one with a real info dictionary: three different goroutines to
	// reap.
	if _, err := e.Add(context.Background(), engine.AddSource{Magnet: magnetURI("leak-a")}); err != nil {
		t.Fatalf("Add magnet: %v", err)
	}

	path := writeTorrentFile(t, buildInfo("leak-fixture", [][]string{{"a.bin"}}))
	if _, err := e.Add(context.Background(), engine.AddSource{FilePath: path}); err != nil {
		t.Fatalf("Add file: %v", err)
	}

	url, stopServer := serveTorrentUntil(t, encodeTorrent(t, buildInfo("leak-served", [][]string{{"b.bin"}})))

	id, err := e.Add(context.Background(), engine.AddSource{TorrentURL: url})
	if err != nil {
		t.Fatalf("Add url: %v", err)
	}

	waitForState(t, e, id, engine.StateDownloading)
	stopServer()

	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	goleak.VerifyNone(t, ignore)
}

func TestCloseWhileMetadataIsStillPending(t *testing.T) {
	ignore := goleak.IgnoreCurrent()

	e, err := New(Options{
		Config:          config.Config{DownloadDir: t.TempDir()},
		Logger:          discardLogger(),
		MetadataTimeout: time.Hour, // never fires; only Close can end this
		Offline:         true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := e.Add(context.Background(), engine.AddSource{Magnet: magnetURI("pending")}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- e.Close() }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close blocked behind a torrent waiting for metadata")
	}

	goleak.VerifyNone(t, ignore)
}

func TestPauseAndResumeReflectInListAndAreIdempotent(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)
	path := writeTorrentFile(t, buildInfo("pause-fixture", [][]string{{"a.bin"}}))

	id, err := e.Add(context.Background(), engine.AddSource{FilePath: path})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	waitForState(t, e, id, engine.StateDownloading)

	if err := e.Pause(id); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	st := statusOf(t, e, id)
	if st.State != engine.StatePaused {
		t.Fatalf("State after Pause = %s, want %s", st.State, engine.StatePaused)
	}

	if st.DownRate != 0 || st.UpRate != 0 {
		t.Errorf("rates = %d/%d while paused, want 0/0", st.DownRate, st.UpRate)
	}

	// Pausing an already-paused torrent is a no-op, not an error.
	if err := e.Pause(id); err != nil {
		t.Fatalf("second Pause: %v", err)
	}

	if got := statusOf(t, e, id).State; got != engine.StatePaused {
		t.Fatalf("State after a second Pause = %s, want %s", got, engine.StatePaused)
	}

	if err := e.Resume(id); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if got := statusOf(t, e, id).State; got != engine.StateDownloading {
		t.Fatalf("State after Resume = %s, want %s", got, engine.StateDownloading)
	}

	// Resuming a torrent that is not paused is a no-op, not an error.
	if err := e.Resume(id); err != nil {
		t.Fatalf("second Resume: %v", err)
	}
}

// TestPauseWhileTorrentURLIsStillFetchingHoldsDownloadOnceAttached exercises
// DEC-102: pausing a torrent before its info dictionary has arrived is
// accepted immediately (the torrent reads StatePaused right away) and the
// transfer is held, never briefly starting, once metadata does arrive.
func TestPauseWhileTorrentURLIsStillFetchingHoldsDownloadOnceAttached(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	body := encodeTorrent(t, buildInfo("pause-url-fixture", [][]string{{"a.bin"}}))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.Header().Set("Content-Type", "application/x-bittorrent")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	e := newTestEngine(t, nil)

	id, err := e.Add(context.Background(), engine.AddSource{TorrentURL: srv.URL + "/fixture.torrent"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if st := statusOf(t, e, id); st.State != engine.StateChecking {
		t.Fatalf("State before the fetch completes = %s, want %s", st.State, engine.StateChecking)
	}

	if err := e.Pause(id); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	if st := statusOf(t, e, id); st.State != engine.StatePaused {
		t.Fatalf("State right after Pause = %s, want %s", st.State, engine.StatePaused)
	}

	close(release)

	deadline := time.Now().Add(2 * time.Second)
	var last engine.TorrentStatus
	for time.Now().Before(deadline) {
		last = statusOf(t, e, id)
		if last.State == engine.StateDownloading {
			t.Fatal("torrent started downloading despite being paused before its metadata arrived")
		}
		if last.State == engine.StatePaused && last.TotalBytes > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if last.State != engine.StatePaused || last.TotalBytes == 0 {
		t.Fatalf("final state = %+v, want StatePaused with metadata arrived", last)
	}

	if err := e.Resume(id); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	waitForState(t, e, id, engine.StateDownloading)
}

func TestFilesIsEmptyBeforeMetadataArrives(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, func(o *Options) { o.MetadataTimeout = time.Hour })

	id, err := e.Add(context.Background(), engine.AddSource{Magnet: magnetURI("files-before-metadata")})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	files, err := e.Files(id)
	if err != nil {
		t.Fatalf("Files: %v", err)
	}

	if len(files) != 0 {
		t.Errorf("Files = %v, want an empty slice before metadata arrives", files)
	}
}

func TestFilesMapsPerFileProgress(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)
	path := writeTorrentFile(t, buildInfo("files-fixture", [][]string{{"a.bin"}, {"sub", "b.bin"}}))

	id, err := e.Add(context.Background(), engine.AddSource{FilePath: path})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	waitForState(t, e, id, engine.StateDownloading)

	files, err := e.Files(id)
	if err != nil {
		t.Fatalf("Files: %v", err)
	}

	if len(files) != 2 {
		t.Fatalf("Files returned %d entries, want 2", len(files))
	}

	var total int64
	for _, f := range files {
		total += f.SizeBytes

		if f.Path == "" {
			t.Error("file has an empty Path")
		}

		if f.Progress != 0 {
			t.Errorf("Progress = %v, want 0 with nothing downloaded", f.Progress)
		}

		if f.DownloadedBytes != 0 {
			t.Errorf("DownloadedBytes = %d, want 0", f.DownloadedBytes)
		}
	}

	if total != 1024+2048 {
		t.Errorf("total size = %d, want %d", total, 1024+2048)
	}
}

func TestRemoveWithoutDeleteDataLeavesFilesOnDisk(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)
	path := writeTorrentFile(t, buildInfo("remove-keep-fixture", [][]string{{"a.bin"}}))

	id, err := e.Add(context.Background(), engine.AddSource{FilePath: path})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	st := waitForState(t, e, id, engine.StateDownloading)
	target := filepath.Join(st.SavePath, "remove-keep-fixture")

	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatalf("seed target dir: %v", err)
	}

	if err := e.Remove(id, false); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, err := os.Stat(target); err != nil {
		t.Errorf("stat %s after Remove(false) = %v, want the data left in place", target, err)
	}

	if _, err := e.Files(id); !errors.Is(err, ErrNotFound) {
		t.Errorf("Files after Remove = %v, want ErrNotFound", err)
	}
}

func TestRemoveWithDeleteDataDeletesOnlyTheTorrentsOwnDirectory(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)
	path := writeTorrentFile(t, buildInfo("remove-delete-fixture", [][]string{{"a.bin"}}))

	id, err := e.Add(context.Background(), engine.AddSource{FilePath: path})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	st := waitForState(t, e, id, engine.StateDownloading)
	target := filepath.Join(st.SavePath, "remove-delete-fixture")
	sibling := filepath.Join(st.SavePath, "sibling-should-survive")

	if err := os.MkdirAll(filepath.Join(target, "sub"), 0o700); err != nil {
		t.Fatalf("seed target dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(target, "a.bin"), []byte("data"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	if err := os.MkdirAll(sibling, 0o700); err != nil {
		t.Fatalf("seed sibling: %v", err)
	}

	if err := e.Remove(id, true); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("stat %s after Remove(true) = %v, want it gone", target, err)
	}

	if _, err := os.Stat(st.SavePath); err != nil {
		t.Errorf("the destination root itself must survive Remove: %v", err)
	}

	if _, err := os.Stat(sibling); err != nil {
		t.Errorf("a sibling entry under the same root must survive Remove: %v", err)
	}
}

// TestDeleteTorrentDataRefusesATraversingName proves the delete-time
// containment check is sound on its own terms, independent of the
// checkComponent gate Add already applies to a torrent's declared name
// (AGENT.md §6.11 — a check in one place is not a check in another). A name
// containing ".." cannot reach this code path through the public Add/Remove
// flow (checkComponent already refuses it), so this calls the unexported
// helper directly to prove it would refuse the escape anyway.
func TestDeleteTorrentDataRefusesATraversingName(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)
	root := t.TempDir()

	err := e.deleteTorrentData("traversal-test", root, "../../escaped", []string{root})
	if !errors.Is(err, ErrOutsideRoots) {
		t.Fatalf("deleteTorrentData with a traversing name = %v, want ErrOutsideRoots", err)
	}
}

// TestDeleteTorrentDataRefusesASymlinkedDirectoryComponent covers a
// symlinked *directory* component of the delete target, not merely a
// symlinked leaf: the recorded destination itself is a symlink pointing
// outside every known root.
func TestDeleteTorrentDataRefusesASymlinkedDirectoryComponent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()

	link := filepath.Join(root, "linked")
	if err := os.Symlink(outside, link); err != nil {
		if isUnprivilegedSymlinkError(err) {
			t.Skip("creating a symlink requires a privilege this environment does not grant")
		}
		t.Fatalf("Symlink: %v", err)
	}

	e := newTestEngine(t, nil)

	err := e.deleteTorrentData("symlink-test", link, "escaped-name", []string{root})
	if !errors.Is(err, ErrOutsideRoots) {
		t.Fatalf("deleteTorrentData through a symlinked directory component = %v, want ErrOutsideRoots", err)
	}

	if _, err := os.Stat(outside); err != nil {
		t.Errorf("the symlink target must survive a refused delete: %v", err)
	}
}

// TestRemoveRefusesWhenTheDestinationRootWasWithdrawn covers a torrent whose
// recorded destination has since been removed from the known-root set. T-032
// ships no live-reconfiguration API — that belongs to whichever later task
// wires config reloads into a running Engine — so this white-box test
// mutates the engine's own unexported root set directly to exercise Remove's
// delete-time re-check (AGENT.md §6.11: it must not trust the set captured
// when the torrent was added).
func TestRemoveRefusesWhenTheDestinationRootWasWithdrawn(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)
	path := writeTorrentFile(t, buildInfo("withdrawn-root-fixture", [][]string{{"a.bin"}}))

	id, err := e.Add(context.Background(), engine.AddSource{FilePath: path})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	st := waitForState(t, e, id, engine.StateDownloading)
	target := filepath.Join(st.SavePath, "withdrawn-root-fixture")

	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatalf("seed target dir: %v", err)
	}

	e.mu.Lock()
	e.roots = nil
	e.mu.Unlock()

	if err := e.Remove(id, true); !errors.Is(err, ErrOutsideRoots) {
		t.Fatalf("Remove after the root was withdrawn = %v, want ErrOutsideRoots", err)
	}

	if _, err := os.Stat(target); err != nil {
		t.Errorf("data must survive a refused delete: %v", err)
	}
}

func TestCloseWithPausedAndRemovedTorrentsLeavesNoGoroutines(t *testing.T) {
	ignore := goleak.IgnoreCurrent()

	e, err := New(Options{
		Config:             config.Config{DownloadDir: t.TempDir()},
		Logger:             discardLogger(),
		MetadataTimeout:    200 * time.Millisecond,
		RateSampleInterval: 5 * time.Millisecond,
		Offline:            true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	pausedID, err := e.Add(context.Background(), engine.AddSource{Magnet: magnetURI("goroutine-paused")})
	if err != nil {
		t.Fatalf("Add magnet: %v", err)
	}

	if err := e.Pause(pausedID); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	path := writeTorrentFile(t, buildInfo("goroutine-removed-fixture", [][]string{{"a.bin"}}))

	removedID, err := e.Add(context.Background(), engine.AddSource{FilePath: path})
	if err != nil {
		t.Fatalf("Add file: %v", err)
	}

	waitForState(t, e, removedID, engine.StateDownloading)

	if err := e.Remove(removedID, true); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	goleak.VerifyNone(t, ignore)
}

// engineLifetimeGoroutines names the goroutines an *Engine (and the
// torrent.Client it wraps) starts once at construction and only ever reaps
// on Close: the rate sampler this package starts, and one the underlying
// torrent.Client starts internally. The tests that use waitForNoLeaks
// deliberately never call Close before asserting — the whole point is
// proving Remove alone reaps a torrent's own goroutines, not that Close
// eventually would — so these two must be named rather than mistaken for
// the leak under test.
var engineLifetimeGoroutines = []goleak.Option{
	goleak.IgnoreTopFunction("github.com/kdta91/tortui/internal/engine/anacrolix.(*Engine).sampleRates"),
	goleak.IgnoreTopFunction("github.com/anacrolix/torrent.(*Client).acceptLimitClearer"),
}

// waitForNoLeaks polls goleak.Find until it reports no leaked goroutines
// (ignoring those named by ignore and by engineLifetimeGoroutines) or the
// budget runs out, without ever calling Close on the engine under test.
func waitForNoLeaks(t *testing.T, ignore goleak.Option) {
	t.Helper()

	opts := append([]goleak.Option{ignore}, engineLifetimeGoroutines...)

	deadline := time.Now().Add(2 * time.Second)

	var last error
	for time.Now().Before(deadline) {
		if err := goleak.Find(opts...); err == nil {
			return
		} else {
			last = err
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("goroutine(s) still running: %v", last)
}

// TestRemoveOfAPendingMetadataTorrentLeavesNoGoroutine is the regression
// test for the QA-reported leak: anacrolix/torrent v1.61.0's Torrent.Drop
// does not close the channel GotInfo waits on (only a real info dictionary
// arriving does), so awaitInfo for a torrent whose metadata never arrived
// used to sit in its select until either the metadata timeout or Close —
// never in response to Remove itself. The metadata timeout here is an hour,
// so only the fix (awaitInfo also selecting on tr.done, closed by Remove)
// can make this pass; Close is deliberately never called before the
// assertion.
func TestRemoveOfAPendingMetadataTorrentLeavesNoGoroutine(t *testing.T) {
	ignore := goleak.IgnoreCurrent()

	e, err := New(Options{
		Config:          config.Config{DownloadDir: t.TempDir()},
		Logger:          discardLogger(),
		MetadataTimeout: time.Hour,
		Offline:         true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() {
		if err := e.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	id, err := e.Add(context.Background(), engine.AddSource{Magnet: magnetURI("remove-pending-metadata")})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if st := statusOf(t, e, id); st.State != engine.StateChecking {
		t.Fatalf("State before Remove = %s, want %s", st.State, engine.StateChecking)
	}

	if err := e.Remove(id, false); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	waitForNoLeaks(t, ignore)
}

// TestRemoveDuringAnInFlightTorrentURLFetchDoesNotLeakOrReattach covers the
// other half of the QA finding: a .torrent URL fetch still in flight when
// Remove runs has no *torrent.Torrent yet (tr.t is nil), so Remove cannot
// Drop anything at that moment. Once the fetch completes and attach() does
// get a real *torrent.Torrent, it must see the torrent was already removed,
// drop it immediately, and never spawn awaitInfo or leave it reachable from
// List/Files — not resurrect it into an active, untracked swarm.
func TestRemoveDuringAnInFlightTorrentURLFetchDoesNotLeakOrReattach(t *testing.T) {
	ignore := goleak.IgnoreCurrent()

	release := make(chan struct{})
	body := encodeTorrent(t, buildInfo("remove-during-fetch-fixture", [][]string{{"a.bin"}}))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.Header().Set("Content-Type", "application/x-bittorrent")
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close) // idempotent; closed explicitly below before the leak check

	e, err := New(Options{
		Config:          config.Config{DownloadDir: t.TempDir()},
		Logger:          discardLogger(),
		MetadataTimeout: time.Hour,
		Offline:         true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t.Cleanup(func() {
		if err := e.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	id, err := e.Add(context.Background(), engine.AddSource{TorrentURL: srv.URL + "/fixture.torrent"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	if st := statusOf(t, e, id); st.State != engine.StateChecking {
		t.Fatalf("State before the fetch completes = %s, want %s", st.State, engine.StateChecking)
	}

	if err := e.Remove(id, false); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Let the deferred HTTP handler respond now that the torrent has
	// already been removed, giving fetchAndAttach/attach every chance to
	// resurrect it if the removed-check were missing.
	close(release)

	if _, err := e.Files(id); !errors.Is(err, ErrNotFound) {
		t.Errorf("Files after Remove during an in-flight fetch = %v, want ErrNotFound", err)
	}

	// Close the test server itself before the leak check: its accept
	// loop is this test's own goroutine to account for, not the engine's,
	// and it is no longer needed once the (possibly cancelled) fetch has
	// settled one way or the other.
	srv.Close()

	waitForNoLeaks(t, ignore)

	if n := len(e.List()); n != 0 {
		t.Errorf("List() = %d entries after Remove during an in-flight fetch, want 0", n)
	}
}
