package anacrolix

import (
	"context"
	"crypto/sha1"
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anacrolix/torrent/metainfo"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/engine"
)

// waitUntil polls cond until it holds or a short budget runs out.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", what)
}

// completeTorrent writes a real single-file payload into dest and returns a
// .torrent path whose piece hashes match it, so an offline engine can verify
// the data and see the torrent complete without any peer.
func completeTorrent(t *testing.T, dest, name string, size int) string {
	t.Helper()

	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i * 7)
	}

	if err := os.WriteFile(filepath.Join(dest, name), data, 0o600); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	var pieces []byte
	for off := 0; off < size; off += testPieceLength {
		sum := sha1.Sum(data[off:min(off+testPieceLength, size)])
		pieces = append(pieces, sum[:]...)
	}

	return writeTorrentFile(t, metainfo.Info{
		Name:        name,
		PieceLength: testPieceLength,
		Pieces:      pieces,
		Length:      int64(size),
	})
}

// waitForStarted waits until a torrent whose data is already on disk has
// attached and got its info dictionary: StateDownloading, or already past it.
// The policy tick can move complete data on to StateSeeding (or, with the
// "off" policy, StatePaused) before a poll ever sees StateDownloading, so
// waiting for exactly that state is a race.
func waitForStarted(t *testing.T, e *Engine, id string) {
	t.Helper()

	waitUntil(t, id+" started", func() bool {
		switch statusOf(t, e, id).State {
		case engine.StateDownloading, engine.StateSeeding, engine.StatePaused:
			return true
		default:
			return false
		}
	})
}

// verify re-hashes a tracked torrent's on-disk data.
func verify(t *testing.T, e *Engine, id string) {
	t.Helper()

	e.mu.Lock()
	tt := e.torrents[id].t
	e.mu.Unlock()

	if err := tt.VerifyData(); err != nil { //nolint:staticcheck // VerifyDataContext needs no ctx here.
		t.Fatalf("VerifyData: %v", err)
	}
}

func TestSpaceShortfall(t *testing.T) {
	t.Parallel()

	cases := []struct {
		free         uint64
		need, margin int64
		want         int64
	}{
		{free: 100, need: 50, margin: 50, want: 0},
		{free: 100, need: 60, margin: 50, want: 10},
		{free: 0, need: 0, margin: 0, want: 0},
		{free: 0, need: 1, margin: 0, want: 1},
		{free: 1 << 40, need: 1 << 30, margin: 1 << 30, want: 0},
	}

	for _, c := range cases {
		if got := spaceShortfall(c.free, c.need, c.margin); got != c.want {
			t.Errorf("spaceShortfall(%d, %d, %d) = %d, want %d", c.free, c.need, c.margin, got, c.want)
		}
	}
}

func TestFormatBytes(t *testing.T) {
	t.Parallel()

	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1536: "1.5 KiB", 1 << 30: "1.0 GiB", 5 << 40: "5.0 TiB"} {
		if got := formatBytes(n); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestBytesNeededDiscountsDataAlreadyOnDisk(t *testing.T) {
	t.Parallel()

	dest := t.TempDir()
	info := buildInfo("example-fixture", [][]string{{"a.bin"}, {"sub", "b.bin"}}) // 1024 + 2048

	if got := bytesNeeded(&info, dest); got != 3072 {
		t.Fatalf("bytesNeeded(empty dest) = %d, want 3072", got)
	}

	if err := os.MkdirAll(filepath.Join(dest, "example-fixture"), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dest, "example-fixture", "a.bin"), make([]byte, 1000), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := bytesNeeded(&info, dest); got != 2072 {
		t.Fatalf("bytesNeeded(partial) = %d, want 2072", got)
	}

	single := metainfo.Info{Name: "one.bin", Length: 500, PieceLength: testPieceLength, Pieces: make([]byte, sha1.Size)}
	if got := bytesNeeded(&single, dest); got != 500 {
		t.Fatalf("bytesNeeded(single) = %d, want 500", got)
	}
}

func TestAddRefusesATorrentThatDoesNotFitAndSaysHowMuchIsShort(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, func(o *Options) {
		o.Config.MinFreeSpace = "1KB"
		o.freeSpace = func(string) (uint64, error) { return 2048, nil }
	})

	info := buildInfo("example-fixture", [][]string{{"a.bin"}, {"b.bin"}}) // 3072 bytes

	_, err := e.Add(context.Background(), engine.AddSource{FilePath: writeTorrentFile(t, info)})
	if !errors.Is(err, ErrInsufficientSpace) {
		t.Fatalf("Add = %v, want ErrInsufficientSpace", err)
	}

	// needs 3 KiB + 1 KiB margin, 2 KiB free: 2 KiB short.
	for _, want := range []string{"needs 3.0 KiB", "1.0 KiB min_free_space margin", "2.0 KiB free", "2.0 KiB short"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Add error %q does not say %q", err, want)
		}
	}

	if n := len(e.List()); n != 0 {
		t.Fatalf("a refused Add left %d tracked torrents", n)
	}
}

func TestAddAcceptsATorrentThatFitsWithTheMargin(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, func(o *Options) {
		o.Config.MinFreeSpace = "1KB"
		o.freeSpace = func(string) (uint64, error) { return 4096, nil }
	})

	info := buildInfo("example-fixture", [][]string{{"a.bin"}, {"b.bin"}}) // exactly 3072 + 1024

	if _, err := e.Add(context.Background(), engine.AddSource{FilePath: writeTorrentFile(t, info)}); err != nil {
		t.Fatalf("Add = %v, want accepted", err)
	}
}

func TestAddSurfacesAFreeSpaceQueryFailure(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, func(o *Options) {
		o.freeSpace = func(string) (uint64, error) { return 0, errors.New("device gone") }
	})

	info := buildInfo("example-fixture", [][]string{{"a.bin"}})

	_, err := e.Add(context.Background(), engine.AddSource{FilePath: writeTorrentFile(t, info)})
	if err == nil || !strings.Contains(err.Error(), "device gone") {
		t.Fatalf("Add = %v, want the free-space error", err)
	}
}

func TestDownloadIsPausedWhenTheDiskFillsAndResumes(t *testing.T) {
	t.Parallel()

	var free atomic.Uint64
	free.Store(1 << 40)

	e := newTestEngine(t, func(o *Options) {
		o.Config.MinFreeSpace = "1MB"
		o.SpaceCheckInterval = time.Millisecond
		o.MetadataTimeout = 10 * time.Second
		o.freeSpace = func(string) (uint64, error) { return free.Load(), nil }
	})

	info := buildInfo("example-fixture", [][]string{{"a.bin"}, {"b.bin"}})

	id, err := e.Add(context.Background(), engine.AddSource{FilePath: writeTorrentFile(t, info)})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	waitForState(t, e, id, engine.StateDownloading)

	free.Store(512 << 10) // under the 1 MiB margin alone

	st := waitForState(t, e, id, engine.StateErrored)
	if !errors.Is(st.Err, ErrInsufficientSpace) || !strings.Contains(st.Err.Error(), "short") ||
		!strings.Contains(st.Err.Error(), "resume") {
		t.Fatalf("Err = %v, want an ErrInsufficientSpace naming the shortfall and the way out", st.Err)
	}

	// Pausing a space-paused torrent changes nothing.
	if err := e.Pause(id); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	free.Store(1 << 40)

	if err := e.Resume(id); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	st = statusOf(t, e, id)
	if st.State != engine.StateDownloading || st.Err != nil {
		t.Fatalf("after Resume: state %s err %v, want downloading and no error", st.State, st.Err)
	}
}

func TestRecheckSpaceLogsAQueryFailureWithoutPausing(t *testing.T) {
	t.Parallel()

	var broken atomic.Bool

	e := newTestEngine(t, func(o *Options) {
		o.SpaceCheckInterval = time.Millisecond
		o.MetadataTimeout = 10 * time.Second
		o.freeSpace = func(string) (uint64, error) {
			if broken.Load() {
				return 0, errors.New("share unmounted")
			}

			return 1 << 40, nil
		}
	})

	info := buildInfo("example-fixture", [][]string{{"a.bin"}})

	id, err := e.Add(context.Background(), engine.AddSource{FilePath: writeTorrentFile(t, info)})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	waitForState(t, e, id, engine.StateDownloading)
	broken.Store(true)
	e.recheckSpace()

	if st := statusOf(t, e, id); st.State != engine.StateDownloading {
		t.Fatalf("state = %s after a failed free-space query, want downloading", st.State)
	}
}

// queueEngine is an engine with one download slot and a metadata timeout
// long enough that an offline magnet holds its slot for the whole test.
func queueEngine(t *testing.T) *Engine {
	t.Helper()

	return newTestEngine(t, func(o *Options) {
		o.Config.MaxActiveDownloads = 1
		o.MetadataTimeout = 30 * time.Second
	})
}

func TestTorrentsBeyondMaxActiveQueueInOrderAndStartAsSlotsFree(t *testing.T) {
	t.Parallel()

	e := queueEngine(t)
	ctx := context.Background()

	add := func(seed string) string {
		t.Helper()

		id, err := e.Add(ctx, engine.AddSource{Magnet: magnetURI(seed)})
		if err != nil {
			t.Fatalf("Add(%s): %v", seed, err)
		}

		return id
	}

	a, b, c := add("queue-a"), add("queue-b"), add("queue-c")

	if st := statusOf(t, e, a); st.State != engine.StateChecking {
		t.Fatalf("first torrent state = %s, want checking", st.State)
	}

	for _, id := range []string{b, c} {
		if st := statusOf(t, e, id); st.State != engine.StateQueued {
			t.Fatalf("%s state = %s, want queued", id, st.State)
		}
	}

	if got := e.Queue(); !slices.Equal(got, []string{b, c}) {
		t.Fatalf("Queue() = %v, want [%s %s]", got, b, c)
	}

	// Reorder: c goes first.
	if err := e.MoveInQueue(c, 0); err != nil {
		t.Fatalf("MoveInQueue: %v", err)
	}

	if got := e.Queue(); !slices.Equal(got, []string{c, b}) {
		t.Fatalf("Queue() after move = %v, want [%s %s]", got, c, b)
	}

	// Removing the active torrent starts the head of the queue.
	if err := e.Remove(a, false); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if st := statusOf(t, e, c); st.State != engine.StateChecking {
		t.Fatalf("promoted torrent state = %s, want checking", st.State)
	}

	if got := e.Queue(); !slices.Equal(got, []string{b}) {
		t.Fatalf("Queue() after promotion = %v, want [%s]", got, b)
	}

	// Pausing the active torrent frees its slot too.
	if err := e.Pause(c); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	waitUntil(t, "b promoted after pausing c", func() bool { return statusOf(t, e, b).State == engine.StateChecking })

	// Resuming c while the only slot is taken puts it back in the queue.
	if err := e.Resume(c); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if st := statusOf(t, e, c); st.State != engine.StateQueued {
		t.Fatalf("resumed-while-full state = %s, want queued", st.State)
	}

	if got := e.Queue(); !slices.Equal(got, []string{c}) {
		t.Fatalf("Queue() = %v, want [%s]", got, c)
	}

	// And it starts once b goes away, with its transfers let through.
	if err := e.Remove(b, false); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if st := statusOf(t, e, c); st.State != engine.StateChecking {
		t.Fatalf("re-promoted state = %s, want checking", st.State)
	}
}

func TestQueuedTorrentCanBePausedAndResumed(t *testing.T) {
	t.Parallel()

	e := queueEngine(t)
	ctx := context.Background()

	if _, err := e.Add(ctx, engine.AddSource{Magnet: magnetURI("pq-a")}); err != nil {
		t.Fatal(err)
	}

	b, err := e.Add(ctx, engine.AddSource{Magnet: magnetURI("pq-b")})
	if err != nil {
		t.Fatal(err)
	}

	if err := e.Pause(b); err != nil {
		t.Fatalf("Pause: %v", err)
	}

	if st := statusOf(t, e, b); st.State != engine.StatePaused {
		t.Fatalf("paused queued torrent state = %s, want paused", st.State)
	}

	if q := e.Queue(); len(q) != 0 {
		t.Fatalf("Queue() = %v, want a paused torrent out of the queue", q)
	}

	if err := e.Resume(b); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if st := statusOf(t, e, b); st.State != engine.StateQueued {
		t.Fatalf("resumed queued torrent state = %s, want queued", st.State)
	}
}

func TestQueuedTorrentFileAndURLStartWhenPromoted(t *testing.T) {
	t.Parallel()

	e := queueEngine(t)
	ctx := context.Background()

	a, err := e.Add(ctx, engine.AddSource{Magnet: magnetURI("qf-a")})
	if err != nil {
		t.Fatal(err)
	}

	file, err := e.Add(ctx, engine.AddSource{FilePath: writeTorrentFile(t, buildInfo("queued-file", [][]string{{"x.bin"}}))})
	if err != nil {
		t.Fatalf("Add(file): %v", err)
	}

	url, err := e.Add(ctx, engine.AddSource{TorrentURL: serveTorrent(t, encodeTorrent(t, buildInfo("queued-url", [][]string{{"y.bin"}})))})
	if err != nil {
		t.Fatalf("Add(url): %v", err)
	}

	for _, id := range []string{file, url} {
		if st := statusOf(t, e, id); st.State != engine.StateQueued {
			t.Fatalf("%s state = %s, want queued", id, st.State)
		}
	}

	if err := e.Remove(a, false); err != nil {
		t.Fatal(err)
	}

	waitForState(t, e, file, engine.StateDownloading)

	if err := e.Remove(file, false); err != nil {
		t.Fatal(err)
	}

	waitForState(t, e, url, engine.StateDownloading)
}

func TestMoveInQueueErrors(t *testing.T) {
	t.Parallel()

	e := queueEngine(t)

	a, err := e.Add(context.Background(), engine.AddSource{Magnet: magnetURI("mq-a")})
	if err != nil {
		t.Fatal(err)
	}

	if err := e.MoveInQueue("an-999", 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("MoveInQueue(unknown) = %v, want ErrNotFound", err)
	}

	if err := e.MoveInQueue(a, 0); !errors.Is(err, ErrNotQueued) {
		t.Errorf("MoveInQueue(active) = %v, want ErrNotQueued", err)
	}

	b, err := e.Add(context.Background(), engine.AddSource{Magnet: magnetURI("mq-b")})
	if err != nil {
		t.Fatal(err)
	}

	if err := e.MoveInQueue(b, 99); err != nil {
		t.Errorf("MoveInQueue(past the end) = %v, want clamped", err)
	}

	if err := e.Close(); err != nil {
		t.Fatal(err)
	}

	if err := e.MoveInQueue(b, 0); !errors.Is(err, ErrClosed) {
		t.Errorf("MoveInQueue after Close = %v, want ErrClosed", err)
	}
}

func TestConcurrentAddsNeverOvershootMaxActive(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, func(o *Options) {
		o.Config.MaxActiveDownloads = 2
		o.MetadataTimeout = 30 * time.Second
	})

	done := make(chan struct{})
	for i := range 12 {
		go func() {
			defer func() { done <- struct{}{} }()

			if _, err := e.Add(context.Background(), engine.AddSource{Magnet: magnetURI("burst-" + string(rune('a'+i)))}); err != nil {
				t.Errorf("Add: %v", err)
			}
		}()
	}

	for range 12 {
		<-done
	}

	active := 0
	for _, st := range e.List() {
		if st.State == engine.StateChecking || st.State == engine.StateDownloading {
			active++
		}
	}

	if active != 2 || len(e.Queue()) != 10 {
		t.Fatalf("active = %d, queued = %d; want 2 and 10", active, len(e.Queue()))
	}
}

func TestSeedingDone(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name     string
		policy   engine.SeedPolicy
		uploaded int64
		now      time.Time
		want     bool
	}{
		{"ratio not reached", engine.SeedPolicy{Mode: engine.SeedToRatio, Ratio: 1}, 999, at, false},
		{"ratio reached", engine.SeedPolicy{Mode: engine.SeedToRatio, Ratio: 1}, 1000, at, true},
		{"ratio zero", engine.SeedPolicy{Mode: engine.SeedToRatio}, 0, at, true},
		{"duration not elapsed", engine.SeedPolicy{Mode: engine.SeedForDuration, Duration: time.Hour}, 0, at.Add(59 * time.Minute), false},
		{"duration elapsed", engine.SeedPolicy{Mode: engine.SeedForDuration, Duration: time.Hour}, 0, at.Add(time.Hour), true},
		{"off", engine.SeedPolicy{Mode: engine.SeedOff}, 0, at, true},
	}

	for _, c := range cases {
		if got := seedingDone(c.policy, c.uploaded, 1000, at, c.now); got != c.want {
			t.Errorf("%s: seedingDone = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCompletedTorrentSeedsUnderTheRatioPolicy(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, func(o *Options) {
		o.Config.SeedPolicy = "ratio"
		o.Config.SeedRatio = 1
	})

	id, err := e.Add(context.Background(), engine.AddSource{FilePath: completeTorrent(t, e.downloadDir, "payload.bin", 40000)})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	waitForStarted(t, e, id)
	verify(t, e, id)

	// Offline nothing is ever uploaded, so ratio 1 is never reached: it
	// keeps seeding, visibly, rather than stopping.
	st := waitForState(t, e, id, engine.StateSeeding)
	if st.Progress != 1 {
		t.Fatalf("seeding torrent progress = %v, want 1", st.Progress)
	}

	if got := e.SeedPolicy().String(); got != "seeds to ratio 1.0, then stops" {
		t.Fatalf("SeedPolicy() = %q", got)
	}
}

func TestCompletedTorrentStopsUploadingUnderTheOffPolicyAndResumeOverrides(t *testing.T) {
	t.Parallel()

	e := newTestEngine(t, func(o *Options) { o.Config.SeedPolicy = "off" })

	id, err := e.Add(context.Background(), engine.AddSource{FilePath: completeTorrent(t, e.downloadDir, "payload.bin", 40000)})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	waitForStarted(t, e, id)
	verify(t, e, id)

	st := waitForState(t, e, id, engine.StatePaused)
	if st.Progress != 1 || st.Err != nil {
		t.Fatalf("stopped torrent: progress %v err %v, want complete and no error", st.Progress, st.Err)
	}

	// The user explicitly asking to keep seeding wins, and the policy
	// does not stop it again on the next tick.
	if err := e.Resume(id); err != nil {
		t.Fatalf("Resume: %v", err)
	}

	if st := statusOf(t, e, id); st.State != engine.StateSeeding {
		t.Fatalf("after Resume state = %s, want seeding", st.State)
	}

	// Drive the policy directly rather than sleeping through ticks.
	for range 3 {
		e.sampleOnce(time.Now())
	}

	if st := statusOf(t, e, id); st.State != engine.StateSeeding {
		t.Fatalf("policy re-stopped an explicitly resumed torrent: state %s", st.State)
	}
}

func TestResolvePolicy(t *testing.T) {
	t.Parallel()

	got, err := resolvePolicy(config.Config{})
	if err != nil {
		t.Fatalf("resolvePolicy(zero): %v", err)
	}

	def := config.Default("")
	if got.maxActive != def.MaxActiveDownloads || got.margin != 1<<30 || got.seed.Mode != engine.SeedToRatio || got.seed.Ratio != def.SeedRatio {
		t.Fatalf("resolvePolicy(zero) = %+v, want config.Default's values", got)
	}

	got, err = resolvePolicy(config.Config{MaxActiveDownloads: 5, MinFreeSpace: "2MB", SeedPolicy: "duration", SeedDuration: "90m"})
	if err != nil {
		t.Fatalf("resolvePolicy: %v", err)
	}

	if got.maxActive != 5 || got.margin != 2<<20 || got.seed.Duration != 90*time.Minute {
		t.Fatalf("resolvePolicy = %+v", got)
	}

	if _, err := resolvePolicy(config.Config{MinFreeSpace: "lots"}); err == nil {
		t.Error("an unparseable min_free_space was accepted")
	}

	if _, err := resolvePolicy(config.Config{SeedPolicy: "forever"}); err == nil {
		t.Error("an unknown seed_policy was accepted")
	}

	if _, err := New(Options{Config: config.Config{DownloadDir: t.TempDir(), MinFreeSpace: "lots"}, Logger: discardLogger(), Offline: true}); err == nil {
		t.Error("New accepted an unparseable min_free_space")
	}
}

func TestListenPortIsTheConfiguredOneWhenFree(t *testing.T) {
	t.Parallel()

	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	port := probe.Addr().(*net.TCPAddr).Port
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}

	e := newTestEngine(t, func(o *Options) {
		o.listenHost = "127.0.0.1"
		o.Config.ListenPort = port
	})

	if got := e.ListenPort(); got != port {
		t.Fatalf("ListenPort() = 0, want a bound port (configured %d)", port)
	}
}

func TestListenPortFallsBackToARandomPortWhenTaken(t *testing.T) {
	t.Parallel()

	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()

	taken := held.Addr().(*net.TCPAddr).Port

	e := newTestEngine(t, func(o *Options) {
		o.listenHost = "127.0.0.1"
		o.Config.ListenPort = taken
	})

	if got := e.ListenPort(); got == 0 || got == taken {
		t.Fatalf("ListenPort() = %d with %d taken, want a different random port", got, taken)
	}
}

func TestOfflineEngineHasNoListenPort(t *testing.T) {
	t.Parallel()

	if got := newTestEngine(t, nil).ListenPort(); got != 0 {
		t.Fatalf("offline ListenPort() = %d, want 0", got)
	}
}
