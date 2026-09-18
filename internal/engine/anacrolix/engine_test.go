package anacrolix

import (
	"context"
	"errors"
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

func TestLaterTaskMethodsReportNotImplemented(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, nil)

	if err := e.Pause("an-1"); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Pause = %v, want ErrNotImplemented", err)
	}

	if err := e.Resume("an-1"); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Resume = %v, want ErrNotImplemented", err)
	}

	if err := e.Remove("an-1", true); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Remove = %v, want ErrNotImplemented", err)
	}

	if _, err := e.Files("an-1"); !errors.Is(err, ErrNotImplemented) {
		t.Errorf("Files = %v, want ErrNotImplemented", err)
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
