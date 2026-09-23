// Package anacrolix implements engine.Engine against github.com/anacrolix/torrent,
// the embedded BitTorrent engine AGENT.md §3 locks. It is the only package in
// tortui that knows that library exists: internal/tui talks to the
// engine.Engine interface and nothing else (AGENT.md §4, §6.4).
//
// Two things about this package are load-bearing rather than incidental.
//
// First, anacrolix/torrent and its sibling libraries log to os.Stderr by
// default, and the TUI owns the terminal. New redirects that logging into
// slog's file sink before it constructs a client, so no torrent can ever be
// added while stderr is still a live output (AGENT.md §13).
//
// Second, a .torrent is attacker-controlled data. Every path it declares is
// cleaned and confirmed to resolve inside the destination before the torrent
// is allowed to start downloading, and a torrent that declares an unsafe path
// is refused with a readable reason rather than silently rewritten
// (AGENT.md §6.11, §13).
package anacrolix

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
	"golang.org/x/time/rate"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/indexer/httpx"
)

// DefaultMetadataTimeout is how long a torrent may sit in engine.StateChecking
// waiting for its info dictionary before it is failed with a readable message.
// A trackerless magnet with no reachable peers would otherwise spin forever
// behind a spinner, which AGENT.md §13 calls out by name.
const DefaultMetadataTimeout = 60 * time.Second

// DefaultRateSampleInterval is how often transfer rates are resampled from the
// underlying client's byte counters. List reports the most recent sample
// rather than computing a rate itself, so the numbers it returns do not depend
// on how often a caller happens to call it.
const DefaultRateSampleInterval = 500 * time.Millisecond

// maxTorrentFileBytes caps a .torrent fetched over HTTP. Real ones are
// kilobytes; anything approaching this is either not a .torrent or is an
// attempt to make the client allocate.
const maxTorrentFileBytes = 8 << 20

// ErrClosed reports a call made after Close.
var ErrClosed = errors.New("anacrolix: engine is closed")

// ErrNoSource reports an engine.AddSource with none of Magnet, TorrentURL, or
// FilePath set.
var ErrNoSource = errors.New("anacrolix: no magnet, torrent URL, or file path given")

// ErrAmbiguousSource reports an engine.AddSource with more than one of Magnet,
// TorrentURL, and FilePath set. Guessing which one the caller meant is exactly
// the kind of silent choice that produces a download nobody asked for.
var ErrAmbiguousSource = errors.New("anacrolix: set exactly one of Magnet, TorrentURL, and FilePath")

// ErrNotFound reports an id that does not name a currently-tracked torrent.
var ErrNotFound = errors.New("anacrolix: torrent not found")

// ErrMetadataTimeout reports a torrent whose info dictionary did not arrive
// within the configured metadata timeout.
var ErrMetadataTimeout = errors.New("anacrolix: metadata fetch timed out")

// Options configures an Engine. The zero value is not usable; every field has
// a documented default, but Config.DownloadDir must name a directory.
type Options struct {
	// Config supplies the user's settings. DownloadDir, MaxPeers,
	// MaxDownloadRate, MaxUploadRate and SavedDestinations are the fields
	// this task reads; the rest (queueing, seeding policy, listen port) are
	// applied by T-034.
	Config config.Config

	// Logger receives the engine's own lines and, via RedirectLogging,
	// everything the underlying torrent library would otherwise have
	// written to stderr. A nil Logger resolves slog's default at call time,
	// which is the file sink internal/logging installs.
	Logger *slog.Logger

	// MetadataTimeout bounds how long a torrent waits for its info
	// dictionary. Zero uses DefaultMetadataTimeout. It is injectable so a
	// test can assert the timeout path without waiting a real minute.
	MetadataTimeout time.Duration

	// RateSampleInterval is how often transfer rates are resampled. Zero
	// uses DefaultRateSampleInterval.
	RateSampleInterval time.Duration

	// HTTPClient fetches a .torrent named by AddSource.TorrentURL. A nil
	// HTTPClient builds one with tortui's shared defaults.
	HTTPClient *httpx.Client

	// Offline disables every network subsystem of the underlying client:
	// DHT, trackers, peer dialling, incoming connections, PEX, webseeds,
	// webtorrent and port forwarding. The engine still accepts sources,
	// reads metadata it is handed directly, validates paths and tracks
	// state — it simply never contacts a peer.
	//
	// It exists so this package's own tests can exercise the real client
	// end to end while making zero network calls (AGENT.md §6.7). It is a
	// supported configuration rather than a test hook: `doctor` and any
	// future dry-run path can use it for the same reason.
	Offline bool
}

// tracked is one torrent's mutable bookkeeping, guarded by Engine.mu.
type tracked struct {
	id       string
	savePath string
	origin   engine.Origin

	// t is the underlying torrent, nil until the source has been accepted
	// into the client (a .torrent fetched over HTTP is attached
	// asynchronously, so there is a window where this is nil).
	t *torrent.Torrent

	// name is the display name to report before the info dictionary
	// arrives, at which point the torrent's own name takes over.
	name string

	state engine.State
	err   error

	// paused is set by Pause and cleared by Resume. It is checked
	// independently of state because a torrent can be paused before its
	// info dictionary ever arrives (state engine.StateChecking) — see
	// awaitInfo and DEC-102.
	paused bool

	// prePauseState is the state to restore on Resume: whatever state was
	// showing at the moment Pause was called, so resuming a
	// still-checking torrent goes back to StateChecking and resuming a
	// downloading one goes back to StateDownloading, never the other way
	// around.
	prePauseState engine.State

	// done is closed exactly once, by Remove, so every goroutine that
	// exists solely to service this one torrent — awaitInfo waiting on
	// metadata, fetchAndAttach's cancel-on-shutdown watcher — can wake up
	// and exit as soon as the torrent is removed, rather than only when
	// the whole Engine closes or the metadata timeout eventually fires.
	// anacrolix/torrent v1.61.0's Torrent.Drop does not close the
	// channel GotInfo waits on (only receiving an info dictionary does),
	// so without this a Remove of a still-pending torrent leaked
	// awaitInfo for up to the metadata timeout.
	//
	// Remove is the only writer: once it removes tr.id from e.torrents,
	// lookupLocked can never find tr again, so nothing can call Remove a
	// second time for the same tracked torrent and close this twice.
	done chan struct{}

	// removed is set by Remove under Engine.mu at the same time done is
	// closed. attach checks it after a client.AddTorrentSpec call that
	// may have run concurrently with, or just after, a Remove — so a
	// torrent whose fetch/attach was still in flight when it was removed
	// is dropped immediately instead of being resurrected into an active,
	// untracked swarm nothing will ever manage or close.
	removed bool

	down rateMeter
	up   rateMeter
}

// Engine is a engine.Engine backed by a real BitTorrent client. Construct one
// with New; the zero value is not usable. Every method is safe for concurrent
// use.
type Engine struct {
	client  *torrent.Client
	logger  *slog.Logger
	updates chan []engine.TorrentStatus

	metadataTimeout time.Duration
	downloadDir     string
	roots           []string
	http            *httpx.Client

	done      chan struct{}
	wg        sync.WaitGroup
	closeOnce sync.Once
	closeErr  error

	mu       sync.Mutex
	closed   bool
	nextID   int
	torrents map[string]*tracked
	order    []string
	storages map[string]storage.ClientImplCloser
}

// Compile-time proof that Engine satisfies the frozen contract.
var _ engine.Engine = (*Engine)(nil)

// New builds an Engine from opts and starts its background workers.
//
// It redirects the torrent library's logging into opts.Logger before it
// constructs the client, so nothing the library logs — at construction or
// afterwards — can reach stderr while the TUI owns the terminal
// (AGENT.md §13).
func New(opts Options) (*Engine, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Before the client exists, and therefore before any torrent can be
	// added. This ordering is the whole point.
	RedirectLogging(logger)

	downloadDir, err := cleanAbsPath(opts.Config.DownloadDir)
	if err != nil {
		return nil, fmt.Errorf("anacrolix: download_dir: %w", err)
	}

	if err := os.MkdirAll(downloadDir, 0o700); err != nil {
		return nil, fmt.Errorf("anacrolix: create download directory %s: %w", downloadDir, err)
	}

	cfg := clientConfig(opts, downloadDir, logger)

	client, err := torrent.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("anacrolix: start torrent client: %w", err)
	}

	metadataTimeout := opts.MetadataTimeout
	if metadataTimeout <= 0 {
		metadataTimeout = DefaultMetadataTimeout
	}

	sampleInterval := opts.RateSampleInterval
	if sampleInterval <= 0 {
		sampleInterval = DefaultRateSampleInterval
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = httpx.New(httpx.Config{MaxBodyBytes: maxTorrentFileBytes})
	}

	e := &Engine{
		client:          client,
		logger:          logger,
		updates:         make(chan []engine.TorrentStatus, 1),
		metadataTimeout: metadataTimeout,
		downloadDir:     downloadDir,
		roots:           destinationRoots(downloadDir, opts.Config.SavedDestinations),
		http:            httpClient,
		done:            make(chan struct{}),
		torrents:        make(map[string]*tracked),
		storages:        make(map[string]storage.ClientImplCloser),
	}

	e.wg.Add(1)
	go e.sampleRates(sampleInterval)

	logger.Info("anacrolix: engine started",
		"download_dir", downloadDir,
		"max_peers", opts.Config.MaxPeers,
		"max_download_rate", opts.Config.MaxDownloadRate,
		"max_upload_rate", opts.Config.MaxUploadRate,
		"offline", opts.Offline,
	)

	return e, nil
}

// clientConfig translates tortui's configuration into the torrent library's.
func clientConfig(opts Options, downloadDir string, logger *slog.Logger) *torrent.ClientConfig {
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = downloadDir
	cfg.Logger = anacrolixLogger(logger)

	if n := opts.Config.MaxPeers; n > 0 {
		cfg.EstablishedConnsPerTorrent = n
		// The library's default half-open allowance is half its
		// established allowance; keeping that ratio means a small
		// max_peers does not leave a disproportionate number of
		// connections in flight.
		cfg.HalfOpenConnsPerTorrent = max(1, n/2)
	}

	cfg.DownloadRateLimiter = rateLimiter(opts.Config.MaxDownloadRate)
	cfg.UploadRateLimiter = rateLimiter(opts.Config.MaxUploadRate)

	if opts.Offline {
		cfg.NoDHT = true
		cfg.DisableTrackers = true
		cfg.DisablePEX = true
		cfg.DisableTCP = true
		cfg.DisableUTP = true
		cfg.DisableWebseeds = true
		cfg.DisableWebtorrent = true
		cfg.NoDefaultPortForwarding = true
		cfg.DialForPeerConns = false
		cfg.AcceptPeerConnections = false
	}

	return cfg
}

// rateLimiter builds a byte-per-second limiter, or an unlimited one when
// bytesPerSecond is zero or negative — which is what config.Config documents
// zero to mean.
func rateLimiter(bytesPerSecond int64) *rate.Limiter {
	if bytesPerSecond <= 0 {
		return rate.NewLimiter(rate.Inf, 0)
	}

	// The burst has to be at least one chunk or a transfer can deadlock
	// waiting for an allowance it will never accumulate.
	burst := int(min(bytesPerSecond, 1<<20))

	return rate.NewLimiter(rate.Limit(bytesPerSecond), max(burst, 1<<14))
}

// destinationRoots is the set of directories a per-torrent destination is
// allowed to sit inside: the configured download directory plus every
// destination the user chose to remember (AGENT.md §6.12).
func destinationRoots(downloadDir string, saved []string) []string {
	roots := make([]string, 0, len(saved)+1)
	roots = append(roots, downloadDir)

	for _, s := range saved {
		if abs, err := cleanAbsPath(s); err == nil {
			roots = append(roots, abs)
		}
	}

	return roots
}

// Add accepts src and starts tracking it, returning the new torrent's ID.
//
// It returns as soon as the source is accepted. Metadata fetch is always
// asynchronous: a magnet has no info dictionary until a peer supplies one, and
// a .torrent named by URL has to be fetched first. Either way the torrent sits
// in engine.StateChecking until its info dictionary arrives, and transitions
// to engine.StateErrored with a readable reason if it has not arrived within
// the configured metadata timeout (AGENT.md §13).
func (e *Engine) Add(ctx context.Context, src engine.AddSource) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	kind, err := classify(src)
	if err != nil {
		return "", err
	}

	dest, err := resolveDestination(src.SavePath, e.downloadDir, e.roots)
	if err != nil {
		return "", err
	}

	switch kind {
	case sourceMagnet:
		spec, err := torrent.TorrentSpecFromMagnetUri(src.Magnet)
		if err != nil {
			return "", fmt.Errorf("anacrolix: parse magnet: %w", err)
		}

		return e.addSpec(spec, dest)

	case sourceFile:
		spec, err := specFromFile(src.FilePath)
		if err != nil {
			return "", err
		}

		return e.addSpec(spec, dest)

	case sourceURL:
		return e.addFromURL(ctx, src.TorrentURL, dest)

	default:
		return "", ErrNoSource
	}
}

// sourceKind names which of AddSource's three mutually exclusive fields was
// set.
type sourceKind int

const (
	sourceNone sourceKind = iota
	sourceMagnet
	sourceURL
	sourceFile
)

// classify reports which single source field src carries, rejecting both
// "none of them" and "more than one of them".
func classify(src engine.AddSource) (sourceKind, error) {
	var (
		kind sourceKind
		n    int
	)

	if trimmed(src.Magnet) != "" {
		kind, n = sourceMagnet, n+1
	}

	if trimmed(src.TorrentURL) != "" {
		kind, n = sourceURL, n+1
	}

	if trimmed(src.FilePath) != "" {
		kind, n = sourceFile, n+1
	}

	switch {
	case n == 0:
		return sourceNone, ErrNoSource
	case n > 1:
		return sourceNone, ErrAmbiguousSource
	default:
		return kind, nil
	}
}

// specFromFile reads a local .torrent file into a spec, validating the path it
// was given first: a NUL byte truncates a path at the syscall boundary, and a
// directory or a device node is not a torrent.
func specFromFile(path string) (*torrent.TorrentSpec, error) {
	abs, err := cleanAbsPath(path)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("anacrolix: read torrent file: %w", err)
	}

	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("anacrolix: %s is not a regular file", abs)
	}

	if info.Size() > maxTorrentFileBytes {
		return nil, fmt.Errorf("anacrolix: torrent file %s is %d bytes, over the %d byte limit",
			abs, info.Size(), maxTorrentFileBytes)
	}

	mi, err := metainfo.LoadFromFile(abs)
	if err != nil {
		return nil, fmt.Errorf("anacrolix: parse torrent file %s: %w", abs, err)
	}

	spec, err := torrent.TorrentSpecFromMetaInfoErr(mi)
	if err != nil {
		return nil, fmt.Errorf("anacrolix: read torrent file %s: %w", abs, err)
	}

	return spec, nil
}

// track registers a new tracked torrent in engine.StateChecking and returns
// it. It is the one place an ID is minted.
func (e *Engine) track(dest, name string) (*tracked, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil, ErrClosed
	}

	e.nextID++
	t := &tracked{
		id:       fmt.Sprintf("an-%d", e.nextID),
		savePath: dest,
		name:     name,
		state:    engine.StateChecking,
		done:     make(chan struct{}),
	}

	e.torrents[t.id] = t
	e.order = append(e.order, t.id)

	return t, nil
}

// addSpec attaches a spec whose infohash is already known.
func (e *Engine) addSpec(spec *torrent.TorrentSpec, dest string) (string, error) {
	if existing, ok := e.findByInfoHash(spec.InfoHash.HexString()); ok {
		// Adding the same torrent twice is a user double-tap, not an
		// error: hand back the ID they already have rather than a second
		// entry pointing at one underlying torrent.
		return existing, nil
	}

	tr, err := e.track(dest, displayName(spec))
	if err != nil {
		return "", err
	}

	if err := e.attach(tr, spec, dest); err != nil {
		return "", err
	}

	return tr.id, nil
}

// addFromURL accepts a .torrent URL immediately and fetches it in the
// background, so Add never blocks on the network (AGENT.md §6.1). The
// background fetch carries ctx's values (e.g. tracing) forward but not its
// cancellation or deadline: ctx belongs to the Add call, which has already
// returned by the time the fetch runs, while the fetch's own lifetime is
// governed by the metadata timeout instead.
func (e *Engine) addFromURL(ctx context.Context, rawURL, dest string) (string, error) {
	tr, err := e.track(dest, rawURL)
	if err != nil {
		return "", err
	}

	e.wg.Add(1)

	go func() {
		defer e.wg.Done()
		e.fetchAndAttach(ctx, tr, rawURL, dest)
	}()

	return tr.id, nil
}

// fetchAndAttach downloads a .torrent and attaches it, failing the tracked
// entry with a readable reason if either step does not work out. The fetch is
// bounded by the metadata timeout — a .torrent that will not download is the
// same user-visible problem as an info dictionary that never arrives.
func (e *Engine) fetchAndAttach(ctx context.Context, tr *tracked, rawURL, dest string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), e.metadataTimeout)
	defer cancel()

	// Abandon the fetch if the engine closes, or this specific torrent is
	// removed, out from under it — either way there is no longer anyone
	// who will read the result.
	stop := make(chan struct{})
	defer close(stop)

	go func() {
		select {
		case <-e.done:
			cancel()
		case <-tr.done:
			cancel()
		case <-stop:
		}
	}()

	resp, err := e.http.Get(ctx, rawURL, nil)
	if err != nil {
		e.fail(tr, fmt.Errorf("fetch torrent file: %w", err))
		return
	}

	mi, err := metainfo.Load(bytesReader(resp.Body))
	if err != nil {
		e.fail(tr, fmt.Errorf("parse fetched torrent file: %w", err))
		return
	}

	spec, err := torrent.TorrentSpecFromMetaInfoErr(mi)
	if err != nil {
		e.fail(tr, fmt.Errorf("read fetched torrent file: %w", err))
		return
	}

	if existing, ok := e.findByInfoHash(spec.InfoHash.HexString()); ok && existing != tr.id {
		e.fail(tr, fmt.Errorf("already added as %s", existing))
		return
	}

	if err := e.attach(tr, spec, dest); err != nil {
		e.fail(tr, err)
	}
}

// attach hands a spec to the client with a per-destination storage backend and
// starts the goroutine that waits for its info dictionary.
func (e *Engine) attach(tr *tracked, spec *torrent.TorrentSpec, dest string) error {
	// When the info dictionary is already in hand — a .torrent read from
	// disk or fetched over HTTP — check it here, so an unsafe torrent is
	// refused with our own readable error instead of surfacing as whatever
	// the library makes of a storage backend that said no.
	if err := validateSpecPaths(spec, dest); err != nil {
		return err
	}

	store, err := e.storageFor(dest)
	if err != nil {
		return err
	}

	spec.Storage = store

	t, isNew, err := e.client.AddTorrentSpec(spec)
	if err != nil {
		return fmt.Errorf("anacrolix: add torrent: %w", err)
	}

	if !isNew {
		e.logger.Debug("anacrolix: torrent already present in client", "id", tr.id)
	}

	e.mu.Lock()
	if tr.removed {
		// Remove ran while this torrent's URL fetch (or, for a magnet
		// or local file, an implausibly fast concurrent Remove right
		// at attach time) was still in flight. tr.t was nil when
		// Remove looked at it, so there was nothing to Drop then — do
		// it now instead of resurrecting a torrent the caller already
		// asked to forget, and never spawn awaitInfo for it.
		e.mu.Unlock()
		t.Drop()

		return nil
	}

	tr.t = t
	if tr.state == engine.StateChecking {
		tr.name = t.Name()
	}
	e.mu.Unlock()

	e.wg.Add(1)

	go func() {
		defer e.wg.Done()
		e.awaitInfo(tr, t, dest)
	}()

	return nil
}

// awaitInfo waits for the torrent's info dictionary, then validates every path
// it declares before letting a single byte be requested. A torrent that
// declares an unsafe path is dropped and reported, never silently rewritten
// (AGENT.md §6.11, §13).
func (e *Engine) awaitInfo(tr *tracked, t *torrent.Torrent, dest string) {
	select {
	case <-e.done:
		return

	case <-tr.done:
		// Removed while its info dictionary was still pending.
		// anacrolix/torrent's Drop does not close the channel GotInfo
		// waits on (only a real info dictionary arriving does), so
		// without this case this goroutine would otherwise sit here
		// until the metadata timeout regardless of Remove. Remove
		// itself already called t.Drop() when it found tr.t non-nil,
		// which it always is here — attach sets it before spawning
		// this goroutine — so there is nothing left to clean up
		// beyond exiting.
		return

	case <-time.After(e.metadataTimeout):
		e.fail(tr, fmt.Errorf("%w after %s: no peer supplied the torrent's info dictionary",
			ErrMetadataTimeout, e.metadataTimeout))
		t.Drop()

		return

	case <-t.GotInfo():
	}

	info := t.Info()
	if info == nil {
		e.fail(tr, errors.New("info dictionary arrived empty"))
		t.Drop()

		return
	}

	if err := validateInfoPaths(info, dest); err != nil {
		e.fail(tr, err)
		t.Drop()
		e.logger.Warn("anacrolix: refused a torrent declaring an unsafe path",
			"id", tr.id, "destination", dest, "error", err)

		return
	}

	e.mu.Lock()
	tr.name = t.Name()
	paused := tr.paused
	switch {
	case paused:
		// The torrent is displaying engine.StatePaused and stays there;
		// record what it would have become so Resume restores
		// StateDownloading rather than the stale StateChecking it was
		// paused from (DEC-102).
		tr.prePauseState = engine.StateDownloading
	case tr.state == engine.StateChecking:
		tr.state = engine.StateDownloading
	}
	e.mu.Unlock()

	// Register the torrent's data as wanted regardless of pause state —
	// priorities and file wantedness are independent of the transfer
	// gate below — then apply the gate a pause requested before metadata
	// ever arrived (DEC-102).
	t.DownloadAll()

	if paused {
		t.DisallowDataDownload()
		t.DisallowDataUpload()
	}
}

// validateSpecPaths validates the info dictionary a spec already carries, if
// it carries one. A magnet has no info dictionary yet, so there is nothing to
// check until it arrives — safeStorage is what catches that case.
func validateSpecPaths(spec *torrent.TorrentSpec, dest string) error {
	if len(spec.InfoBytes) == 0 {
		return nil
	}

	var info metainfo.Info
	if err := bencode.Unmarshal(spec.InfoBytes, &info); err != nil {
		return fmt.Errorf("anacrolix: read info dictionary: %w", err)
	}

	return validateInfoPaths(&info, dest)
}

// validateInfoPaths checks every file an info dictionary declares, plus the
// torrent's own name, and fails on the first unsafe one.
func validateInfoPaths(info *metainfo.Info, dest string) error {
	name := info.BestName()

	files := info.UpvertedFiles()
	if len(files) == 0 {
		_, err := checkTorrentPath(dest, name, nil)
		return err
	}

	for _, f := range files {
		if _, err := checkTorrentPath(dest, name, f.BestPath()); err != nil {
			return err
		}
	}

	return nil
}

// storageFor returns the storage backend rooted at dest, creating it on first
// use. One backend per destination rather than one per torrent: the backend
// owns a piece-completion database, and two of them rooted at the same
// directory would be two writers to one file.
func (e *Engine) storageFor(dest string) (storage.ClientImplCloser, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil, ErrClosed
	}

	if s, ok := e.storages[dest]; ok {
		return s, nil
	}

	if err := os.MkdirAll(dest, 0o700); err != nil {
		return nil, fmt.Errorf("anacrolix: create destination %s: %w", dest, err)
	}

	s := newSafeStorage(dest, e.logger)
	e.storages[dest] = s

	return s, nil
}

// fail moves a tracked torrent to engine.StateErrored with a readable reason.
// A torrent that has already failed keeps its first error: the first thing
// that went wrong is the one worth showing.
func (e *Engine) fail(tr *tracked, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if tr.state == engine.StateErrored {
		return
	}

	tr.state = engine.StateErrored
	tr.err = fmt.Errorf("anacrolix: torrent %s: %w", tr.id, err)
	tr.down.reset()
	tr.up.reset()
}

// findByInfoHash returns the ID of a tracked torrent with the given infohash.
func (e *Engine) findByInfoHash(hex string) (string, bool) {
	if hex == "" {
		return "", false
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	for _, id := range e.order {
		tr := e.torrents[id]
		if tr.t != nil && tr.t.InfoHash().HexString() == hex {
			return id, true
		}
	}

	return "", false
}

// List returns a snapshot of every tracked torrent. It reads counters the
// client already maintains and the most recent rate sample; it performs no
// network or disk I/O, so it is safe to call from a bubbletea command
// (AGENT.md §6.1).
func (e *Engine) List() []engine.TorrentStatus {
	e.mu.Lock()
	defer e.mu.Unlock()

	out := make([]engine.TorrentStatus, 0, len(e.order))
	for _, id := range e.order {
		out = append(out, e.statusLocked(e.torrents[id]))
	}

	return out
}

// statusLocked maps one tracked torrent onto the frozen TorrentStatus shape.
// Engine.mu must be held.
func (e *Engine) statusLocked(tr *tracked) engine.TorrentStatus {
	st := engine.TorrentStatus{
		ID:       tr.id,
		Name:     tr.name,
		State:    tr.state,
		SavePath: tr.savePath,
		Origin:   tr.origin,
		Err:      tr.err,
		ETA:      -1,
	}

	if tr.t == nil {
		return st
	}

	st.InfoHash = tr.t.InfoHash().HexString()

	if name := tr.t.Name(); name != "" {
		st.Name = name
	}

	if tr.t.Info() == nil {
		// No info dictionary yet: length and completion are both
		// meaningless, and reporting zeros as "0% of 0 bytes" is more
		// honest than inventing a total.
		return st
	}

	st.TotalBytes = tr.t.Length()
	st.DownloadedBytes = min(tr.t.BytesCompleted(), st.TotalBytes)
	st.Progress = progress(st.DownloadedBytes, st.TotalBytes)

	stats := tr.t.Stats()
	st.Peers = stats.ActivePeers
	st.Seeds = stats.ConnectedSeeders

	if tr.state == engine.StateDownloading || tr.state == engine.StateSeeding {
		st.DownRate = tr.down.rate
		st.UpRate = tr.up.rate
	}

	st.ETA = eta(st.State, st.TotalBytes, st.DownloadedBytes, st.DownRate)

	return st
}

// progress is done/total clamped to [0,1], and 0 rather than NaN when the
// total is unknown.
func progress(done, total int64) float64 {
	if total <= 0 || done <= 0 {
		return 0
	}

	if done >= total {
		return 1
	}

	return float64(done) / float64(total)
}

// eta estimates the time to completion from the current download rate, or -1
// when it cannot be known: no metadata, no rate, not downloading, or already
// complete.
func eta(state engine.State, total, done, downRate int64) time.Duration {
	if state != engine.StateDownloading || total <= 0 || downRate <= 0 || done >= total {
		return -1
	}

	return time.Duration(float64(total-done) / float64(downRate) * float64(time.Second))
}

// sampleRates refreshes every torrent's transfer rates on a fixed interval, so
// the rates List reports do not depend on how often List is called.
func (e *Engine) sampleRates(interval time.Duration) {
	defer e.wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-e.done:
			return
		case now := <-ticker.C:
			e.sampleOnce(now)
		}
	}
}

// sampleOnce takes one rate sample for every tracked torrent.
func (e *Engine) sampleOnce(now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, id := range e.order {
		tr := e.torrents[id]
		if tr.t == nil {
			continue
		}

		stats := tr.t.Stats()
		tr.down.observe(now, stats.BytesReadUsefulData.Int64())
		tr.up.observe(now, stats.BytesWrittenData.Int64())
	}
}

// Updates returns the engine's snapshot channel. The channel is created here
// and closed exactly once by Close.
//
// TODO(T-033): emit coalesced ~2 Hz snapshots on this channel; T-031 only
// establishes and closes it.
func (e *Engine) Updates() <-chan []engine.TorrentStatus {
	return e.updates
}

// lookupLocked resolves id to its tracked torrent, or a wrapped ErrNotFound.
// Engine.mu must be held.
func (e *Engine) lookupLocked(id string) (*tracked, error) {
	tr, ok := e.torrents[id]
	if !ok {
		return nil, fmt.Errorf("%q: %w", id, ErrNotFound)
	}

	return tr, nil
}

// Pause stops a torrent's transfers without removing it. Pausing an
// already-paused torrent is a no-op, and pausing a torrent whose info
// dictionary has not arrived yet is accepted immediately: the torrent shows
// engine.StatePaused right away, and awaitInfo (DEC-102) holds its transfers
// as soon as the info dictionary does arrive, instead of racing to
// StateDownloading first.
func (e *Engine) Pause(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return ErrClosed
	}

	tr, err := e.lookupLocked(id)
	if err != nil {
		return err
	}

	if tr.paused {
		return nil
	}

	tr.paused = true

	if tr.state != engine.StateErrored {
		tr.prePauseState = tr.state
		tr.state = engine.StatePaused
	}

	tr.down.reset()
	tr.up.reset()

	if tr.t != nil {
		tr.t.DisallowDataDownload()
		tr.t.DisallowDataUpload()
	}

	return nil
}

// Resume restarts a paused torrent's transfers, restoring whatever state it
// showed at the moment it was paused (StateChecking, StateDownloading, or
// StateSeeding). Resuming a torrent that is not paused is a no-op.
func (e *Engine) Resume(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return ErrClosed
	}

	tr, err := e.lookupLocked(id)
	if err != nil {
		return err
	}

	if !tr.paused {
		return nil
	}

	tr.paused = false

	if tr.state == engine.StatePaused {
		tr.state = tr.prePauseState
	}

	if tr.t != nil {
		tr.t.AllowDataDownload()
		tr.t.AllowDataUpload()
	}

	return nil
}

// Remove stops and forgets a torrent. When deleteData is true, its
// downloaded data is also deleted, but only after re-checking — at delete
// time, not trusting the check Add already did (AGENT.md §6.11) — that the
// torrent's own data directory resolves inside one of the engine's currently
// known destination roots, following any symlink in the path first
// (AGENT.md §6.12). A destination that no longer belongs to the known root
// set, or that resolves outside every root via a symlinked component,
// refuses the delete with a logged, wrapped ErrOutsideRoots rather than
// deleting nothing found there but also never touching data it should not.
func (e *Engine) Remove(id string, deleteData bool) error {
	e.mu.Lock()

	if e.closed {
		e.mu.Unlock()
		return ErrClosed
	}

	tr, err := e.lookupLocked(id)
	if err != nil {
		e.mu.Unlock()
		return err
	}

	t := tr.t
	savePath := tr.savePath

	var name string
	if t != nil && t.Info() != nil {
		name = t.Info().BestName()
	}

	tr.removed = true
	close(tr.done)

	delete(e.torrents, id)

	for i, existing := range e.order {
		if existing == id {
			e.order = append(e.order[:i], e.order[i+1:]...)
			break
		}
	}

	roots := append([]string(nil), e.roots...)
	e.mu.Unlock()

	if t != nil {
		t.Drop()
	}

	if !deleteData {
		return nil
	}

	return e.deleteTorrentData(id, savePath, name, roots)
}

// deleteTorrentData removes a torrent's own file or directory — savePath
// joined with the torrent's own declared name, exactly the layout
// checkTorrentPath validated at Add time — from disk, refusing (and
// logging) if that target does not resolve, through any symlink, inside any
// of roots. roots is read fresh from the engine at the moment of the call
// (Remove's caller), not cached from Add time, so a root removed from the
// known set since Add is honoured here.
func (e *Engine) deleteTorrentData(id, savePath, name string, roots []string) error {
	if name == "" {
		// No info dictionary ever arrived (or the torrent was dropped
		// before it did): nothing was ever written to disk for it.
		return nil
	}

	target := filepath.Join(savePath, name)

	var (
		insideAny bool
		lastErr   error
	)

	for _, root := range roots {
		cleanRoot, err := cleanAbsPath(root)
		if err != nil {
			lastErr = err
			continue
		}

		ok, err := containedInRoot(cleanRoot, target)
		if err != nil {
			lastErr = err
			continue
		}

		if ok {
			insideAny = true
			break
		}
	}

	if !insideAny {
		refusal := fmt.Errorf("%w: refusing to delete data for torrent %s at %s", ErrOutsideRoots, id, target)
		e.logger.Warn("anacrolix: refused to delete torrent data outside every known destination root",
			"id", id, "target", target, "roots", roots, "error", lastErr)

		return refusal
	}

	resolvedTarget, err := resolveSymlinks(target)
	if err != nil {
		return fmt.Errorf("anacrolix: resolve delete target for torrent %s: %w", id, err)
	}

	if err := os.RemoveAll(resolvedTarget); err != nil {
		return fmt.Errorf("anacrolix: delete data for torrent %s: %w", id, err)
	}

	return nil
}

// Files returns the per-file progress for one torrent: its path relative to
// the torrent's own root, size, bytes downloaded, and fractional progress
// (AGENT.md §5's engine.FileStatus — which carries no priority field, so
// none is reported; see DEC-102). It returns an empty slice, not an error,
// for a torrent whose info dictionary has not arrived yet: there is no file
// list to report, and that is not a failure — it is simply not known yet.
func (e *Engine) Files(id string) ([]engine.FileStatus, error) {
	e.mu.Lock()

	if e.closed {
		e.mu.Unlock()
		return nil, ErrClosed
	}

	tr, err := e.lookupLocked(id)
	if err != nil {
		e.mu.Unlock()
		return nil, err
	}

	t := tr.t
	e.mu.Unlock()

	if t == nil || t.Info() == nil {
		return []engine.FileStatus{}, nil
	}

	files := t.Files()
	out := make([]engine.FileStatus, 0, len(files))

	for _, f := range files {
		length := f.Length()
		downloaded := min(f.BytesCompleted(), length)

		out = append(out, engine.FileStatus{
			Path:            f.Path(),
			SizeBytes:       length,
			DownloadedBytes: downloaded,
			Progress:        progress(downloaded, length),
		})
	}

	return out, nil
}

// Close stops every background worker, closes the underlying client and its
// storage backends, and closes the updates channel. It is idempotent:
// concurrent and repeated calls do the work once and every caller gets the
// same result.
func (e *Engine) Close() error {
	e.closeOnce.Do(func() {
		e.mu.Lock()
		e.closed = true
		e.mu.Unlock()

		close(e.done)
		e.wg.Wait()

		errs := e.client.Close()

		e.mu.Lock()
		for dest, s := range e.storages {
			if err := s.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close storage %s: %w", dest, err))
			}
		}
		e.storages = map[string]storage.ClientImplCloser{}
		e.mu.Unlock()

		close(e.updates)

		if len(errs) > 0 {
			e.closeErr = fmt.Errorf("anacrolix: close engine: %w", errors.Join(errs...))
			e.logger.Error("anacrolix: engine closed with errors", "error", e.closeErr)
		} else {
			e.logger.Info("anacrolix: engine closed")
		}
	})

	return e.closeErr
}
