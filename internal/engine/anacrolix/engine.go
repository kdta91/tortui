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
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"golang.org/x/time/rate"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/indexer/httpx"
	"github.com/kdta91/tortui/internal/platform"
)

// DefaultMetadataTimeout is how long a torrent may sit in engine.StateChecking
// waiting for its info dictionary before it is failed with a readable message.
// A trackerless magnet with no reachable peers would otherwise spin forever
// behind a spinner, which AGENT.md §13 calls out by name.
const DefaultMetadataTimeout = 60 * time.Second

// DefaultRateSampleInterval is how often transfer rates are resampled from the
// underlying client's byte counters, and therefore the cadence of Updates:
// each sample is followed by at most one coalesced snapshot, so the default
// is the ~2 Hz AGENT.md §5 documents. List reports the most recent sample
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

// ErrLeftData is engine.ErrLeftData: an add refused while an entry for the
// same torrent, refused earlier, still has data at another destination
// (T-9127, DEC-163). The error returned is an *engine.LeftDataError naming
// that data. It is re-exported here so callers of this package can test for
// it without a second import.
var ErrLeftData = engine.ErrLeftData

// Options configures an Engine. The zero value is not usable; every field has
// a documented default, but Config.DownloadDir must name a directory.
type Options struct {
	// Config supplies the user's settings: DownloadDir, SavedDestinations,
	// MaxPeers, MaxDownloadRate, MaxUploadRate, MaxActiveDownloads,
	// ListenPort, SeedPolicy/SeedRatio/SeedDuration and MinFreeSpace. A
	// zero-valued MaxActiveDownloads, SeedPolicy, or MinFreeSpace takes
	// config.Default's value rather than meaning "none".
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

	// RateSampleInterval is how often transfer rates are resampled and a
	// snapshot is considered for Updates. Zero uses
	// DefaultRateSampleInterval.
	RateSampleInterval time.Duration

	// newTicker, when set, replaces time.NewTicker for the sample loop. It
	// returns the tick channel and a stop function. It exists so this
	// package's tests can drive the Updates cadence tick by tick instead of
	// sleeping on a wall clock.
	newTicker func(time.Duration) (<-chan time.Time, func())

	// beforeAttach, when set, is called by fetchAndAttach immediately after
	// a fetched .torrent's metainfo has been parsed successfully and
	// immediately before attach is called. It exists so a package test can
	// land a Remove exactly between those two points, deterministically:
	// closing tr.done (which Remove does) also cancels fetchAndAttach's own
	// HTTP request context, so a Remove during the network wait itself
	// makes the fetch fail before attach is ever reached — it cannot
	// exercise the tr.removed guard inside attach. Pausing here, after the
	// network part has already succeeded, is the only way to land a Remove
	// in the narrow window that guard actually protects (T-944 QA
	// remediation).
	beforeAttach func()

	// afterInfo, when set, is called by awaitInfo with the torrent's ID
	// right after it releases Engine.mu, once the info dictionary has
	// arrived and a pause's transfer gate has been decided. It exists so a
	// package test can land a Resume in exactly that window (T-946).
	afterInfo func(id string)

	// onAttach, when set, is called by attach with the library's torrent
	// at each attachStage, so a package test can read the transfer gate a
	// torrent has at that point, or land a Pause or Resume there (T-9134).
	onAttach func(attachStage, *torrent.Torrent)

	// SpaceCheckInterval is how often downloading torrents' destinations
	// are re-checked for free space. Zero uses DefaultSpaceCheckInterval.
	// It is measured against sample-tick times, so a test driving the
	// ticker drives this too.
	SpaceCheckInterval time.Duration

	// freeSpace, when set, replaces platform.FreeSpace, so a test can
	// report a nearly-full disk without filling one.
	freeSpace func(string) (uint64, error)

	// listenHost, when set together with Offline, keeps a TCP listener on
	// that host (loopback in tests) so the listen-port fallback can be
	// exercised while still never dialling or announcing anywhere.
	listenHost string

	// peers, when set together with Offline and listenHost, also accepts
	// incoming peer connections and dials the peers a test hands a
	// torrent, so two engines can trade data over loopback while DHT,
	// trackers and PEX stay off and nothing is announced (T-9135).
	peers bool

	// keepAliveTimeout, when set, replaces the library's peer keep-alive
	// timeout, which also bounds how long a peer connection's writer can
	// sleep after a wake-up it missed (Backlog T-9137). The loopback peer
	// tests set it short so a missed wake-up costs them under a second
	// instead of a minute.
	keepAliveTimeout time.Duration

	// HTTPClient fetches a .torrent named by AddSource.TorrentURL. A nil
	// HTTPClient builds one with tortui's shared defaults, which also
	// follows a redirect to a subdomain of the requested host.
	HTTPClient *httpx.Client

	// torrentTransport, when set, carries the default HTTPClient's
	// requests, so a test can stage a redirect between named hosts
	// without any network.
	torrentTransport http.RoundTripper

	// webseeds, when set together with Offline, leaves web seeding on, so
	// a test can download from a loopback web seed while DHT, trackers
	// and peer connections all stay off.
	webseeds bool

	// metainfoSources, when set together with Offline, leaves the
	// library's metainfo-source fetcher on, so a test can prove with a
	// loopback server that no magnet's xs= or as= address reaches it
	// (T-9094).
	metainfoSources bool

	// Offline disables every network subsystem of the underlying client:
	// DHT, trackers, peer dialling, incoming connections, PEX, webseeds,
	// webtorrent, metainfo sources and port forwarding. The engine still
	// accepts sources, reads metadata it is handed directly, validates
	// paths and tracks state — it simply never contacts a peer.
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

	// magnet and torrentURL are the source the torrent was added from,
	// and metainfo the bencoded .torrent a restore was handed. They are
	// what ResumeData reports when the torrent's own info dictionary is
	// not (or no longer) available from the client (T-041).
	magnet     string
	torrentURL string
	metainfo   []byte

	// infoHash is the torrent's infohash, hex-encoded, as soon as it is
	// known: at track time for addSpec (a magnet or a local .torrent file
	// both already carry it before tracking begins), or set by attach
	// once a fetched .torrent's metainfo is parsed for a URL source.
	// findOrTrack and claimInfoHash match against this field rather than
	// tr.t.InfoHash() precisely so a torrent that is tracked but not yet
	// attach()ed to the underlying client — the exact window a concurrent
	// Add for the same infohash can land in — is still found (T-944).
	infoHash string

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

	// userPaused is set when Pause paused the torrent, and cleared by
	// Resume: the user's pause, the one pause ResumeData reports so it
	// survives a restart (T-952). A pause by the free-space check, the
	// seed policy, or PauseForShutdown sets paused alone.
	userPaused bool

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

	// spec is a torrent held in the queue before it was ever attached to
	// the client; url (with urlCtx, the Add call's values) is a .torrent
	// URL held in the queue before it was fetched. Both are cleared when
	// the queue promotes the torrent.
	spec   *torrent.TorrentSpec
	url    string
	urlCtx context.Context

	// completedAt is when the torrent's data was first seen complete, the
	// start of any SeedForDuration window.
	completedAt time.Time

	// seedDone is set when the seed policy stopped the torrent's upload
	// (it then shows StatePaused); seedOverride is set when the user
	// resumed it afterwards, which exempts it from the policy from then
	// on — an explicit request to keep seeding is not overridden again.
	seedDone     bool
	seedOverride bool

	// uploadedBefore is what the torrent uploaded in earlier sessions,
	// which the ratio policy adds to this session's count (T-9135).
	uploadedBefore int64

	// spacePaused is set when the periodic free-space check paused the
	// torrent; it shows StateErrored with the shortfall as Err until
	// Resume, which clears both.
	spacePaused bool

	// removed is set by Remove under Engine.mu at the same time done is
	// closed. attach checks it after a client.AddTorrentSpec call that
	// may have run concurrently with, or just after, a Remove — so a
	// torrent whose fetch/attach was still in flight when it was removed
	// is dropped immediately instead of being resurrected into an active,
	// untracked swarm nothing will ever manage or close.
	removed bool

	// refused is set, with the torrent errored, once nothing of it is left
	// in the client: awaitInfo refused it and dropped it, or the queue's
	// start of it failed before it was ever attached. A later Add of the
	// same infohash untracks a refused entry and starts over rather than
	// handing it back (T-948), unless the add is to another destination and
	// the entry left data (leftName below).
	refused bool

	// leftName is set on a refused entry whose data was on disk when it
	// was refused: that data's validated name under savePath. Remove
	// deletes by it when the library holds no info dictionary for the
	// entry, and an add of the same infohash to another destination is
	// refused with ErrLeftData while the entry is tracked (T-9127).
	leftName string

	// sharedName is set instead of leftName when the data on disk under
	// the refused entry's name is another tracked entry's (same name, same
	// destination): never this entry's to delete or name, but a remove
	// with data says it kept it (T-9133).
	sharedName string

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
	spaceInterval   time.Duration
	freeSpace       func(string) (uint64, error)
	maxActive       int
	margin          int64
	seed            engine.SeedPolicy
	listenPort      int
	downloadDir     string
	roots           []string
	http            *httpx.Client
	beforeAttach    func()
	afterInfo       func(id string)
	onAttach        func(attachStage, *torrent.Torrent)

	done      chan struct{}
	wg        sync.WaitGroup
	closeOnce sync.Once
	closeErr  error

	mu       sync.Mutex
	closed   bool
	nextID   int
	torrents map[string]*tracked
	order    []string
	queue    []string
	storages map[string]safeStorage
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

	settings, err := resolvePolicy(opts.Config)
	if err != nil {
		return nil, err
	}

	client, err := newClient(opts, downloadDir, logger)
	if err != nil {
		return nil, err
	}

	metadataTimeout := opts.MetadataTimeout
	if metadataTimeout <= 0 {
		metadataTimeout = DefaultMetadataTimeout
	}

	sampleInterval := opts.RateSampleInterval
	if sampleInterval <= 0 {
		sampleInterval = DefaultRateSampleInterval
	}

	spaceInterval := opts.SpaceCheckInterval
	if spaceInterval <= 0 {
		spaceInterval = DefaultSpaceCheckInterval
	}

	freeSpace := opts.freeSpace
	if freeSpace == nil {
		freeSpace = platform.FreeSpace
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		// A source's download address may hand the .torrent off to a
		// storage host under its own domain (T-9010, DEC-136). This
		// client injects no credentials, so following that hop sends a
		// subdomain nothing but the address the source itself chose. A
		// Torznab aggregator answers a magnet-only result's address with
		// a redirect to the magnet: that is surfaced, never requested,
		// and the torrent is added by it instead (T-9079, DEC-151).
		httpClient = httpx.New(httpx.Config{
			MaxBodyBytes:             maxTorrentFileBytes,
			FollowSubdomainRedirects: true,
			MagnetRedirects:          true,
			Transport:                opts.torrentTransport,
		})
	}

	e := &Engine{
		client:          client,
		logger:          logger,
		updates:         make(chan []engine.TorrentStatus, 1),
		metadataTimeout: metadataTimeout,
		spaceInterval:   spaceInterval,
		freeSpace:       freeSpace,
		maxActive:       settings.maxActive,
		margin:          settings.margin,
		seed:            settings.seed,
		listenPort:      client.LocalPort(),
		downloadDir:     downloadDir,
		roots:           destinationRoots(downloadDir, opts.Config.SavedDestinations),
		http:            httpClient,
		beforeAttach:    opts.beforeAttach,
		afterInfo:       opts.afterInfo,
		onAttach:        opts.onAttach,
		done:            make(chan struct{}),
		torrents:        make(map[string]*tracked),
		storages:        make(map[string]safeStorage),
	}

	newTicker := opts.newTicker
	if newTicker == nil {
		newTicker = realTicker
	}

	ticks, stopTicks := newTicker(sampleInterval)

	e.wg.Add(1)
	go e.sampleRates(ticks, stopTicks)

	logger.Info("anacrolix: engine started",
		"download_dir", downloadDir,
		"listen_port", e.listenPort,
		"max_active_downloads", e.maxActive,
		"min_free_space", e.margin,
		"seed_policy", e.seed.String(),
		"max_peers", opts.Config.MaxPeers,
		"max_download_rate", opts.Config.MaxDownloadRate,
		"max_upload_rate", opts.Config.MaxUploadRate,
		"offline", opts.Offline,
	)

	return e, nil
}

// policySettings is the download policy New resolves from config.
type policySettings struct {
	maxActive int
	margin    int64
	seed      engine.SeedPolicy
}

// resolvePolicy reads the queueing, free-space and seeding settings from
// cfg, taking config.Default's value for any left zero.
func resolvePolicy(cfg config.Config) (policySettings, error) {
	def := config.Default("")

	maxActive := cfg.MaxActiveDownloads
	if maxActive <= 0 {
		maxActive = def.MaxActiveDownloads
	}

	minFree := cfg.MinFreeSpace
	if minFree == "" {
		minFree = def.MinFreeSpace
	}

	margin, err := config.ParseByteSize(minFree)
	if err != nil {
		return policySettings{}, fmt.Errorf("anacrolix: min_free_space: %w", err)
	}

	mode, ratio, duration := cfg.SeedPolicy, cfg.SeedRatio, cfg.SeedDuration
	if mode == "" {
		mode, ratio = def.SeedPolicy, def.SeedRatio
	}

	if duration == "" {
		duration = def.SeedDuration
	}

	seed, err := engine.ParseSeedPolicy(mode, ratio, duration)
	if err != nil {
		return policySettings{}, fmt.Errorf("anacrolix: %w", err)
	}

	return policySettings{maxActive: maxActive, margin: margin, seed: seed}, nil
}

// newClient starts the torrent client on the configured listen port, falling
// back to a random free port when that one cannot be bound (another client,
// or a second program, already holds it). The fallback is logged; the port
// actually bound is what Engine.ListenPort reports.
func newClient(opts Options, downloadDir string, logger *slog.Logger) (*torrent.Client, error) {
	port := opts.Config.ListenPort

	client, err := torrent.NewClient(clientConfig(opts, downloadDir, logger, port))
	if err == nil {
		return client, nil
	}

	if port == 0 {
		return nil, fmt.Errorf("anacrolix: start torrent client: %w", err)
	}

	logger.Warn("anacrolix: listen port unavailable, falling back to a random port",
		"listen_port", port, "error", err)

	client, retryErr := torrent.NewClient(clientConfig(opts, downloadDir, logger, 0))
	if retryErr != nil {
		return nil, fmt.Errorf("anacrolix: start torrent client on port %d (%w) or a random port: %w",
			port, err, retryErr)
	}

	return client, nil
}

// ListenPort reports the port the client actually bound for incoming peer
// connections — the configured one, or the random fallback — or 0 when no
// listener is open (Offline).
func (e *Engine) ListenPort() int { return e.listenPort }

// SeedPolicy reports the seeding policy in effect, whose String is the
// sentence the UI shows so continued upload is never a surprise.
func (e *Engine) SeedPolicy() engine.SeedPolicy { return e.seed }

// clientConfig translates tortui's configuration into the torrent library's,
// listening on port (0 = random).
func clientConfig(opts Options, downloadDir string, logger *slog.Logger, port int) *torrent.ClientConfig {
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = downloadDir
	cfg.Logger = anacrolixLogger(logger)
	cfg.ListenPort = port

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

	// The library uploads a completed torrent only with Seed set: without
	// it a torrent that wants nothing never uploads, and the seed policy
	// had nothing to stop (T-9135, DEC-168). Under "off" it stays unset,
	// so nothing is uploaded once a download completes. Upload while
	// downloading stays the library's reciprocal kind, as before: the
	// seed policy is about what happens after completion.
	cfg.Seed = seedsAfterCompletion(opts.Config)
	cfg.DisableAggressiveUpload = true

	if opts.keepAliveTimeout > 0 {
		cfg.KeepAliveTimeout = opts.keepAliveTimeout
	}

	if opts.Offline {
		cfg.NoDHT = true
		cfg.DisableTrackers = true
		cfg.DisablePEX = true
		cfg.DisableTCP = true
		cfg.DisableUTP = true
		cfg.DisableWebseeds = !opts.webseeds
		cfg.DisableWebtorrent = true
		cfg.NoDefaultPortForwarding = true
		cfg.DialForPeerConns = false
		cfg.AcceptPeerConnections = false

		// The library fetches a torrent's metainfo sources with its own
		// HTTP client, which none of the switches above reach. tortui
		// never hands it one (specFromMagnet), so this only backs that up.
		if !opts.metainfoSources {
			cfg.MetainfoSourcesClient = &http.Client{Transport: refuseMetainfoSources{}}
		}

		if host := opts.listenHost; host != "" {
			cfg.DisableTCP = false
			cfg.DisableIPv6 = true
			cfg.ListenHost = func(string) string { return host }
			cfg.AcceptPeerConnections = opts.peers
			cfg.DialForPeerConns = opts.peers
		}
	}

	return cfg
}

// seedsAfterCompletion reports whether cfg's seed policy keeps a completed
// torrent uploading for a while: any policy but "off". New has already
// refused a policy that does not parse.
func seedsAfterCompletion(cfg config.Config) bool {
	settings, err := resolvePolicy(cfg)

	return err == nil && settings.seed.Mode != engine.SeedOff
}

// errMetainfoSourcesOffline is what the Offline client's metainfo-source
// fetcher answers every request with.
var errMetainfoSourcesOffline = errors.New("anacrolix: metainfo sources are off while offline")

// refuseMetainfoSources is the Offline client's metainfo-source transport: it
// refuses every request before anything is dialled.
type refuseMetainfoSources struct{}

// RoundTrip implements http.RoundTripper.
func (refuseMetainfoSources) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body != nil {
		_ = req.Body.Close()
	}

	return nil, errMetainfoSourcesOffline
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

// Compile-time proof that Engine can grow its known destination roots.
var _ engine.RootAdder = (*Engine)(nil)

// AddRoot implements engine.RootAdder: dir joins the destination roots Add,
// Restore, and Remove's delete check resolve against (AGENT.md §6.12). The
// TUI calls it only for a destination the user chose (T-074).
func (e *Engine) AddRoot(dir string) error {
	abs, err := engine.CheckDestinationRoot(dir)
	if err != nil {
		return fmt.Errorf("anacrolix: add root: %w", err)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return ErrClosed
	}

	for _, r := range e.roots {
		if r == abs {
			return nil
		}
	}

	e.roots = append(e.roots, abs)

	return nil
}

// knownRoots returns a copy of the current destination roots, taken under
// e.mu because AddRoot may grow the set concurrently.
func (e *Engine) knownRoots() []string {
	e.mu.Lock()
	defer e.mu.Unlock()

	return append([]string(nil), e.roots...)
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

	dest, err := resolveDestination(src.SavePath, e.downloadDir, e.knownRoots())
	if err != nil {
		return "", err
	}

	switch kind {
	case sourceMagnet:
		spec, magnet, err := specFromMagnet(src.Magnet)
		if err != nil {
			return "", err
		}

		return e.addSpec(spec, dest, provenance{magnet: magnet})

	case sourceFile:
		spec, err := specFromFile(src.FilePath)
		if err != nil {
			return "", err
		}

		return e.addSpec(spec, dest, provenance{})

	case sourceURL:
		// The address is never the display name: it may carry the user's
		// api key or passkey (T-9057). Until the .torrent is fetched the
		// torrent is named by the address's host alone.
		return e.addFromURL(ctx, src.TorrentURL, engine.URLSourceName(src.TorrentURL), dest,
			provenance{torrentURL: src.TorrentURL})

	default:
		return "", ErrNoSource
	}
}

// specFromMagnet parses a magnet URI, refusing one that names no infohash:
// the library accepts "magnet:?xt=<anything>" with a zero infohash and then
// panics when that spec is added.
//
// It is the only place this package turns a magnet into a spec, so every
// magnet the client is handed — added, restored, queued, or taken from a
// redirect — has had its xs= and as= dropped first (T-9094, DEC-153). It
// returns the magnet it parsed, which is what the caller records as the
// torrent's source, so resume data and the session record lose them too.
func specFromMagnet(uri string) (*torrent.TorrentSpec, string, error) {
	uri = engine.WithoutMetainfoSources(uri)

	spec, err := torrent.TorrentSpecFromMagnetUri(uri)
	if err != nil {
		return nil, "", fmt.Errorf("anacrolix: parse magnet: %w", err)
	}

	if spec.InfoHash == (metainfo.Hash{}) {
		return nil, "", errors.New("anacrolix: parse magnet: no infohash")
	}

	return spec, uri, nil
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
// it, with no infohash recorded yet — used only by addFromURL, where the
// infohash is not known until the fetch completes. addSpec uses
// findOrTrack instead, which mints under the same critical section as its
// existing-infohash lookup (T-944).
//
// When every download slot is taken the new entry is queued instead, holding
// the URL (and the Add call's context values) until the queue promotes it,
// and queued is true; otherwise the caller starts the fetch, having been
// counted in e.wg under the same lock that checked the engine is open.
//
// name is the display name until the fetched .torrent names the torrent; it
// is never rawURL (T-9057).
func (e *Engine) track(ctx context.Context, dest, rawURL, name string, prov provenance) (tr *tracked, queued bool, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil, false, ErrClosed
	}

	t := prov.newTracked(e.mintIDLocked(prov.id), dest, name)
	t.state = engine.StateChecking

	e.torrents[t.id] = t
	e.order = append(e.order, t.id)

	// A torrent restored paused holds no slot, so it never queues (T-952).
	if t.paused {
		t.prePauseState, t.state = t.state, engine.StatePaused
	} else if e.activeLocked() > e.maxActive {
		t.url, t.urlCtx = rawURL, context.WithoutCancel(ctx)
		e.enqueueLocked(t)

		return t, true, nil
	}

	e.wg.Add(1)

	return t, false, nil
}

// addSpec attaches a spec whose infohash is already known, or queues it when
// every download slot is taken.
//
// When the spec already carries its info dictionary (a local .torrent) its
// paths and its space requirement are checked here, before anything is
// tracked, so an unsafe or oversized torrent is refused by Add itself with a
// readable reason (AGENT.md §6.11, T-034). attach checks both again when the
// torrent actually starts, since a queued torrent may start much later.
func (e *Engine) addSpec(spec *torrent.TorrentSpec, dest string, prov provenance) (string, error) {
	if err := e.precheckSpec(spec, dest); err != nil {
		return "", err
	}

	tr, existingID, queued, err := e.findOrTrack(spec, dest, prov)
	if err != nil {
		return "", err
	}

	if tr == nil {
		// Adding the same torrent twice is a user double-tap, not an
		// error: hand back the ID they already have rather than a second
		// entry pointing at one underlying torrent.
		return existingID, nil
	}

	if queued {
		return tr.id, nil
	}

	if err := e.attach(tr, spec, dest); err != nil {
		// attach can fail (e.g. validateSpecPaths refusing an unsafe
		// path) before it ever calls client.AddTorrentSpec, so there is
		// nothing on the underlying client to clean up here — but tr
		// itself, minted by findOrTrack above, is still sitting in
		// e.torrents/e.order with its infohash recorded. Left there, a
		// retried Add of the same magnet/file would have findOrTrack
		// match that stale, never-attached entry by infohash and hand
		// back its id with a nil error — silently succeeding on a
		// retry of a refused Add, in violation of AGENT.md §6.11's
		// "refused with a clear reason, not silently rewritten" (T-944
		// QA remediation, regression from this task's own findOrTrack
		// change: the pre-T-944 findByInfoHash only matched an
		// already-attached tr.t != nil entry, so this stale entry was
		// never found — this untrack restores that same effect while
		// keeping the concurrency fix). Untracking it here means a
		// retry starts clean: a fresh findOrTrack finds nothing, mints
		// a new entry, and attach fails the same way again, reporting
		// the same error every time the underlying condition holds.
		e.untrackFailedSpec(tr)
		return "", err
	}

	return tr.id, nil
}

// untrackFailedSpec removes tr from e.torrents/e.order after its attach
// call failed, freeing its infohash for a subsequent Add to retry against a
// clean slate. It is a no-op if tr is no longer the entry registered under
// its own id — e.g. a concurrent Remove already untracked it — so it never
// deletes something else's entry.
func (e *Engine) untrackFailedSpec(tr *tracked) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.torrents[tr.id] != tr {
		return
	}

	e.untrackLocked(tr)
}

// untrackLocked removes tr from e.torrents, e.order and the queue.
// Engine.mu must be held.
func (e *Engine) untrackLocked(tr *tracked) {
	delete(e.torrents, tr.id)
	e.dequeueLocked(tr.id)

	for i, id := range e.order {
		if id == tr.id {
			e.order = append(e.order[:i], e.order[i+1:]...)
			break
		}
	}
}

// findOrTrack looks up a tracked torrent by infohash and, if none is found,
// mints and registers a new one — as a single critical section under
// Engine.mu, so concurrent Add calls for the same infohash can never both
// pass the lookup and each mint their own tracked entry. Before T-944,
// addSpec called findByInfoHash and track as two separate calls, each
// taking and releasing e.mu on its own; two goroutines racing Add for the
// same magnet could both see "not found" between those calls and both
// track (and later attach) their own entry for one infohash.
//
// Exactly one of the two meaningful return values is set: a non-nil tr for
// a freshly minted entry the caller must now attach, or a non-empty
// existingID naming the entry already tracked for hex.
//
// The same critical section decides whether the new entry starts now or
// queues (queued true, spec held on the entry until promote attaches it), so
// concurrent Adds can never together overshoot max_active_downloads.
func (e *Engine) findOrTrack(spec *torrent.TorrentSpec, dest string, prov provenance) (tr *tracked, existingID string, queued bool, err error) {
	hex, name := spec.InfoHash.HexString(), displayName(spec)
	gone := e.goneLeftData(hex)

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil, "", false, ErrClosed
	}

	if id, ok, err := e.claimInfoHashLocked(hex, nil, dest, gone); err != nil {
		return nil, "", false, err
	} else if ok {
		return nil, id, false, nil
	}

	t := prov.newTracked(e.mintIDLocked(prov.id), dest, name)
	t.infoHash = hex
	t.state = engine.StateChecking

	// A torrent restored paused holds no slot, so it never queues; it is
	// attached with its transfers held (T-952).
	if t.paused {
		t.prePauseState, t.state = t.state, engine.StatePaused
	} else if e.activeLocked() >= e.maxActive {
		t.spec = spec
		e.enqueueLocked(t)
		queued = true
	}

	e.torrents[t.id] = t
	e.order = append(e.order, t.id)

	return t, "", queued, nil
}

// precheckSpec validates a spec's declared paths and its space requirement
// when it already carries an info dictionary; a magnet carries none yet, and
// is checked by awaitInfo when its dictionary arrives.
func (e *Engine) precheckSpec(spec *torrent.TorrentSpec, dest string) error {
	info, err := specInfo(spec)
	if err != nil || info == nil {
		return err
	}

	if err := validateInfoPaths(info, dest); err != nil {
		return err
	}

	return e.checkSpace(dest, bytesNeeded(info, dest))
}

// addFromURL accepts a .torrent URL immediately and fetches it in the
// background, so Add never blocks on the network (AGENT.md §6.1). The
// background fetch carries ctx's values (e.g. tracing) forward but not its
// cancellation or deadline: ctx belongs to the Add call, which has already
// returned by the time the fetch runs, while the fetch's own lifetime is
// governed by the metadata timeout instead. name is the display name until
// the fetched .torrent supplies the torrent's own.
func (e *Engine) addFromURL(ctx context.Context, rawURL, name, dest string, prov provenance) (string, error) {
	tr, queued, err := e.track(ctx, dest, rawURL, name, prov)
	if err != nil {
		return "", err
	}

	if queued {
		return tr.id, nil
	}

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

	var redirect *httpx.MagnetRedirectError
	if errors.As(err, &redirect) {
		e.attachRedirectMagnet(tr, redirect, dest)
		return
	}

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

	if existing, ok, err := e.claimInfoHash(spec.InfoHash.HexString(), tr, dest); err != nil {
		e.fail(tr, err)
		return
	} else if ok {
		e.fail(tr, fmt.Errorf("already added as %s", existing))
		return
	}

	if e.beforeAttach != nil {
		e.beforeAttach()
	}

	if err := e.attach(tr, spec, dest); err != nil {
		e.fail(tr, err)
	}
}

// errRedirectMagnetUnusable reports a magnet a .torrent address redirected to
// that httpx accepted but the torrent library will not parse. The library's
// own error quotes the link, so it is not kept (T-9079, DEC-151).
var errRedirectMagnetUnusable = errors.New("the magnet link it redirected to is not usable")

// attachRedirectMagnet adds, in place of the .torrent it was fetching, the
// magnet a torrent's address redirected to (T-9079, DEC-151): the same
// tracked entry, so the id, destination and slot are kept, and the same
// display name until metadata arrives. A torrent already tracked under its
// infohash fails this one exactly as a fetched .torrent's would. From here
// on the torrent's source is the magnet, so ResumeData — and so the session
// record — holds the magnet instead of the address, and a restart adds by
// the magnet without fetching the address again. Neither the magnet nor the
// address is ever logged or put in an error: a magnet's tracker addresses
// can carry a passkey.
func (e *Engine) attachRedirectMagnet(tr *tracked, redirect *httpx.MagnetRedirectError, dest string) {
	spec, magnet, err := specFromMagnet(redirect.Magnet())
	if err != nil {
		e.fail(tr, fmt.Errorf("fetch torrent file: %s: %w", redirect.Host, errRedirectMagnetUnusable))
		return
	}

	if existing, ok, err := e.claimInfoHash(spec.InfoHash.HexString(), tr, dest); err != nil {
		e.fail(tr, err)
		return
	} else if ok {
		e.fail(tr, fmt.Errorf("already added as %s", existing))
		return
	}

	e.mu.Lock()
	spec.DisplayName = tr.name
	tr.magnet, tr.torrentURL = magnet, ""
	e.mu.Unlock()

	e.logger.Debug("anacrolix: torrent address redirected to a magnet link; adding by it",
		"id", tr.id, "host", redirect.Host)

	if e.beforeAttach != nil {
		e.beforeAttach()
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
	if err := e.precheckSpec(spec, dest); err != nil {
		return err
	}

	store, err := e.storageFor(dest)
	if err != nil {
		return err
	}

	spec.Storage = store

	t, held, err := e.addToClient(tr, spec)
	if err != nil {
		return err
	}

	e.attachHook(stageJoined, t)

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

	// Set for a magnet or local file at track time already (findOrTrack);
	// for a URL source, this is the first point the infohash is known, so
	// record it here — otherwise a torrent added by URL would never be
	// findable by infohash until its info dictionary arrives, well after
	// attach.
	if e.closed {
		// Close began while this attach was in flight; Close's own
		// client.Close reaps t, and no new goroutine may join e.wg now.
		e.mu.Unlock()
		return ErrClosed
	}

	tr.infoHash = t.InfoHash().HexString()
	tr.t = t
	if tr.state == engine.StateChecking {
		tr.name = t.Name()
	}

	// A Pause or Resume that ran since addToClient read the pause found
	// tr.t nil and could not touch the gate: settle it here, under the lock
	// that now publishes tr.t, so a peer is never sent data between here
	// and awaitInfo's own gate.
	switch {
	case !held && tr.paused:
		t.DisallowDataDownload()
		t.DisallowDataUpload()
	case held && !tr.paused && tr.state != engine.StateQueued:
		t.AllowDataDownload()
		t.AllowDataUpload()
	}

	// Counted under the same lock that saw the engine open, so it can
	// never race Close's wg.Wait.
	e.wg.Add(1)
	e.mu.Unlock()

	e.attachHook(stagePublished, t)

	go func() {
		defer e.wg.Done()
		e.awaitInfo(tr, t, dest)
	}()

	return nil
}

// attachStage names a point in attach at which Options.onAttach is called.
type attachStage int

const (
	// stageAdded: AddTorrentSpec has returned, before any gate is set.
	stageAdded attachStage = iota
	// stageJoined: addToClient is done, before Engine.mu publishes tr.t.
	stageJoined
	// stagePublished: tr.t is published and the gate settled, before
	// awaitInfo starts.
	stagePublished
)

// attachHook calls Options.onAttach, when set.
func (e *Engine) attachHook(stage attachStage, t *torrent.Torrent) {
	if e.onAttach != nil {
		e.onAttach(stage, t)
	}
}

// addToClient hands spec to the client. A torrent paused before it joins —
// restored paused (T-952), or paused while its .torrent was fetched — joins
// with its transfers held (held true) before its info dictionary is set:
// until then it has no data to offer, so no peer is sent a byte of it before
// the gate is shut (T-9134, DEC-169). The library's own add-time options for
// this are declared but never read in v1.61.0.
func (e *Engine) addToClient(tr *tracked, spec *torrent.TorrentSpec) (*torrent.Torrent, bool, error) {
	e.mu.Lock()
	held := tr.paused
	e.mu.Unlock()

	add := *spec
	if held {
		add.InfoBytes = nil
	}

	t, isNew, err := e.client.AddTorrentSpec(&add)
	if err != nil {
		return nil, false, fmt.Errorf("anacrolix: add torrent: %w", err)
	}

	e.attachHook(stageAdded, t)

	// Not reached for a held torrent: one tracked entry per infohash
	// (findOrTrack, claimInfoHash), and a refused one was dropped before it
	// was refused. Were it reached, the torrent already has its info, and
	// is gated below all the same.
	if !isNew {
		e.logger.Debug("anacrolix: torrent already present in client", "id", tr.id)
	}

	if !held {
		return t, false, nil
	}

	t.DisallowDataDownload()
	t.DisallowDataUpload()

	if spec.InfoBytes != nil {
		if err := t.SetInfoBytes(spec.InfoBytes); err != nil {
			if isNew {
				t.Drop()
			}

			return nil, false, fmt.Errorf("anacrolix: add torrent: %w", err)
		}
	}

	return t, true, nil
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
		e.refuse(tr, t, nil, fmt.Errorf("%w after %s: no peer supplied the torrent's info dictionary",
			ErrMetadataTimeout, e.metadataTimeout))

		return

	case <-t.GotInfo():
	}

	info := t.Info()
	if info == nil {
		e.refuse(tr, t, nil, errors.New("info dictionary arrived empty"))

		return
	}

	if err := validateInfoPaths(info, dest); err != nil {
		e.refuse(tr, t, info, err)
		e.logger.Warn("anacrolix: refused a torrent declaring an unsafe path",
			"id", tr.id, "destination", dest, "error", err)

		return
	}

	// A magnet's size is unknown until now; this is its add-time
	// free-space precheck. (A .torrent's already ran in addSpec/attach, and
	// passes again here trivially unless the disk filled in between.)
	if err := e.checkSpace(dest, bytesNeeded(info, dest)); err != nil {
		e.refuse(tr, t, info, err)
		e.logger.Warn("anacrolix: refused a torrent for lack of free space",
			"id", tr.id, "destination", dest, "error", err)

		return
	}

	e.mu.Lock()
	tr.name = t.Name()
	// A queued torrent (resumed into the queue while its metadata was
	// still pending) holds its transfers exactly like a paused one.
	paused := tr.paused || tr.state == engine.StateQueued
	switch {
	case tr.state == engine.StateQueued:
		// promote picks StateDownloading itself once Info is known.
	case paused:
		// The torrent is displaying engine.StatePaused and stays there;
		// record what it would have become so Resume restores
		// StateDownloading rather than the stale StateChecking it was
		// paused from (DEC-102).
		tr.prePauseState = engine.StateDownloading
	case tr.state == engine.StateChecking:
		tr.state = engine.StateDownloading
	}

	// Apply the gate a pause requested before metadata ever arrived
	// (DEC-102) under the same lock that read the pause, as Pause does: a
	// Resume or promote that lifts it can then only run after it, never
	// between the read and the gate, which left a torrent showing
	// StateDownloading with its transfers held (T-946).
	if paused {
		t.DisallowDataDownload()
		t.DisallowDataUpload()
	}
	e.mu.Unlock()

	if e.afterInfo != nil {
		e.afterInfo(tr.id)
	}

	// Register the torrent's data as wanted regardless of pause state:
	// priorities and file wantedness are independent of the transfer
	// gate above.
	t.DownloadAll()
}

// specInfo decodes the info dictionary a spec already carries, or returns nil
// when it carries none. A magnet has no info dictionary yet, so there is
// nothing to check until it arrives — awaitInfo and safeStorage catch that
// case.
func specInfo(spec *torrent.TorrentSpec) (*metainfo.Info, error) {
	if len(spec.InfoBytes) == 0 {
		return nil, nil
	}

	var info metainfo.Info
	if err := bencode.Unmarshal(spec.InfoBytes, &info); err != nil {
		return nil, fmt.Errorf("anacrolix: read info dictionary: %w", err)
	}

	return &info, nil
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
func (e *Engine) storageFor(dest string) (safeStorage, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return safeStorage{}, ErrClosed
	}

	if s, ok := e.storages[dest]; ok {
		return s, nil
	}

	if err := os.MkdirAll(dest, 0o700); err != nil {
		return safeStorage{}, fmt.Errorf("anacrolix: create destination %s: %w", dest, err)
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

	e.failLocked(tr, err)
}

// failLocked is fail's body. Engine.mu must be held.
func (e *Engine) failLocked(tr *tracked, err error) {
	if tr.state == engine.StateErrored {
		return
	}

	tr.state = engine.StateErrored
	tr.err = fmt.Errorf("anacrolix: torrent %s: %w", tr.id, err)
	tr.down.reset()
	tr.up.reset()
}

// refuse drops t, when there is one, from the client, then fails tr with err
// and marks it refused, in one critical section: by the time the torrent
// shows StateErrored nothing of it is left in the client, so a re-Add of its
// infohash can start over without the client handing back the torrent being
// dropped (T-948). info, when known, is the torrent's info dictionary: if its
// data is on disk at tr's destination, the entry keeps its name (leftName).
func (e *Engine) refuse(tr *tracked, t *torrent.Torrent, info *metainfo.Info, err error) {
	if t != nil {
		t.Drop()
	}

	left := leftData(info, tr.savePath)

	e.mu.Lock()
	defer e.mu.Unlock()

	e.failLocked(tr, err)
	tr.refused = true
	e.keepLeftLocked(tr, left)
}

// keepLeftLocked records left, the name of data on disk at refused entry tr's
// destination, as tr's leftName — or as its sharedName when another tracked
// entry keeps its data under that name there, which is not tr's to delete or
// to name (T-9127, T-9133). Engine.mu must be held.
func (e *Engine) keepLeftLocked(tr *tracked, left string) {
	tr.leftName, tr.sharedName = "", ""

	if left == "" {
		return
	}

	if taken, _ := e.dataNameUsedLocked(tr, left); taken {
		tr.sharedName = left
		return
	}

	tr.leftName = left
}

// dataNameUsedLocked reports whether a tracked entry other than self, at
// self's destination, keeps its data under name (taken), or has not named its
// data there yet and so may come to (pending) — a magnet still waiting for its
// info dictionary, a .torrent still queued or being fetched, a torrent still
// joining the client (T-9135, DEC-167). Engine.mu must be held.
func (e *Engine) dataNameUsedLocked(self *tracked, name string) (taken, pending bool) {
	for _, tr := range e.torrents {
		if tr == self || tr.savePath != self.savePath {
			continue
		}

		n, known := dataNameLocked(tr)
		if !known {
			pending = true
			continue
		}

		if n != "" && n == name {
			return true, false
		}
	}

	return false, pending
}

// dataNameLocked is the name tr's data has, or will have, under its
// destination, and whether that is known yet: a refused entry's left name
// (it has nothing in the client); else the torrent's own name once the
// library or a queued spec holds its info dictionary; "" for an entry that
// failed before it ever joined the client. A magnet waiting for its info
// dictionary, a .torrent still queued or being fetched, and a torrent still
// joining the client have not named their data yet (known false). Engine.mu
// must be held.
func dataNameLocked(tr *tracked) (name string, known bool) {
	switch {
	case tr.refused:
		return tr.leftName, true
	case tr.t != nil:
		if info := tr.t.Info(); info != nil {
			return info.BestName(), true
		}

		return "", false
	case tr.spec != nil:
		if info, err := specInfo(tr.spec); err == nil && info != nil {
			return info.BestName(), true
		}

		return "", false
	case tr.state == engine.StateErrored:
		return "", true
	default:
		return "", false
	}
}

// leftData returns the name info gives its data under dest when that name is
// safe there (AGENT.md §6.11) and something is on disk at it, and "" when
// info is nil or nothing is there.
func leftData(info *metainfo.Info, dest string) string {
	if info == nil || validateInfoPaths(info, dest) != nil {
		return ""
	}

	name := info.BestName()
	if _, err := os.Lstat(filepath.Join(dest, name)); errors.Is(err, fs.ErrNotExist) {
		return ""
	}

	return name
}

// claimInfoHash is claimInfoHashLocked under Engine.mu, after
// goneLeftData.
func (e *Engine) claimInfoHash(hex string, self *tracked, dest string) (string, bool, error) {
	gone := e.goneLeftData(hex)

	e.mu.Lock()
	defer e.mu.Unlock()

	return e.claimInfoHashLocked(hex, self, dest, gone)
}

// goneLeftData returns the left-data paths of the refused entries for hex
// that are no longer on disk: the user deleted them by hand (T-9129). It
// reads the disk without Engine.mu held, so the claim that takes the result
// does no I/O under the lock.
func (e *Engine) goneLeftData(hex string) map[string]bool {
	if hex == "" {
		return nil
	}

	var paths []string

	e.mu.Lock()
	for _, tr := range e.torrents {
		if tr.refused && tr.infoHash == hex && tr.leftName != "" {
			paths = append(paths, filepath.Join(tr.savePath, tr.leftName))
		}
	}
	e.mu.Unlock()

	gone := make(map[string]bool)

	for _, p := range paths {
		if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
			gone[p] = true
		}
	}

	return gone
}

// claimInfoHashLocked returns the ID of a tracked torrent other than self
// with the given infohash, matching whether or not that torrent has been
// attach()ed to the underlying client yet (see tracked.infoHash). A refused
// entry for hex is not a match: it is untracked on the way, so adding a
// refused torrent again evaluates it afresh instead of handing back the
// stale, errored entry (T-948) — unless that entry left data on disk at a
// destination other than dest, when the claim fails with ErrLeftData and
// nothing is untracked (T-9127, DEC-163). Left data whose path is in gone
// (goneLeftData) was deleted by hand: that entry left nothing, and is
// untracked like any other (T-9129). Engine.mu must be held.
func (e *Engine) claimInfoHashLocked(hex string, self *tracked, dest string, gone map[string]bool) (string, bool, error) {
	if hex == "" {
		return "", false, nil
	}

	var refused []*tracked

	for _, id := range e.order {
		tr := e.torrents[id]
		if tr == self || tr.infoHash != hex {
			continue
		}

		if !tr.refused {
			return id, true, nil
		}

		refused = append(refused, tr)
	}

	for _, tr := range refused {
		if tr.leftName == "" || tr.savePath == dest {
			continue
		}

		left := filepath.Join(tr.savePath, tr.leftName)
		if !gone[left] {
			return "", false, &engine.LeftDataError{Path: left}
		}
	}

	for _, tr := range refused {
		e.untrackLocked(tr)
	}

	return "", false, nil
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
		InfoHash: tr.infoHash,
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

	// ETA uses the rolling average, not the instantaneous DownRate, so a
	// single bursty or idle sample does not make the estimate jump.
	st.ETA = eta(st.State, st.TotalBytes, st.DownloadedBytes, tr.down.avg)

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

// eta estimates the time to completion from a download rate — the rolling
// average, at the one call site — or -1 when it cannot be known: no metadata,
// no rate, not downloading, or already complete.
func eta(state engine.State, total, done, downRate int64) time.Duration {
	if state != engine.StateDownloading || total <= 0 || downRate <= 0 || done >= total {
		return -1
	}

	return time.Duration(float64(total-done) / float64(downRate) * float64(time.Second))
}

// realTicker is the production ticker for the sample loop.
func realTicker(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTicker(d)
	return t.C, t.Stop
}

// sampleRates refreshes every torrent's transfer rates on each tick, so the
// rates List reports do not depend on how often List is called, and then
// publishes the resulting snapshot on Updates if anything changed.
//
// This goroutine is the only sender on e.updates, and Close closes the
// channel only after it has exited (wg.Wait), so a send can never race the
// close.
func (e *Engine) sampleRates(ticks <-chan time.Time, stop func()) {
	defer e.wg.Done()
	defer stop()

	var (
		last        []engine.TorrentStatus
		lastSpaceAt time.Time
	)

	for {
		select {
		case <-e.done:
			return
		case now := <-ticks:
			if lastSpaceAt.IsZero() || now.Sub(lastSpaceAt) >= e.spaceInterval {
				e.recheckSpace()
				lastSpaceAt = now
			}

			e.promote()

			snap := e.sampleOnce(now)
			if last == nil || !snapshotsEqual(last, snap) {
				// The consumer owns what it receives and may sort or
				// edit it; hand it a copy so last stays private to
				// this goroutine. A shared backing array would be a
				// data race and would corrupt change detection.
				e.publish(slices.Clone(snap))
				last = snap
			}
		}
	}
}

// sampleOnce takes one rate sample for every tracked torrent and returns the
// full snapshot as it stands immediately afterwards.
func (e *Engine) sampleOnce(now time.Time) []engine.TorrentStatus {
	e.mu.Lock()
	defer e.mu.Unlock()

	out := make([]engine.TorrentStatus, 0, len(e.order))

	for _, id := range e.order {
		tr := e.torrents[id]
		if tr.t != nil {
			stats := tr.t.Stats()
			tr.down.observe(now, stats.BytesReadUsefulData.Int64())
			tr.up.observe(now, stats.BytesWrittenData.Int64())
			e.applyPolicyLocked(tr, now)
		}

		out = append(out, e.statusLocked(tr))
	}

	return out
}

// publish hands snap to the Updates channel without ever blocking the
// engine. The channel holds one snapshot; if the consumer has not taken the
// previous one yet, that stale snapshot is discarded and replaced, so a slow
// consumer always reads the newest state and never makes the sampler wait.
//
// Only sampleRates calls this, so after the drain the buffer is guaranteed
// to have room; the second select's default is a belt-and-braces guard, not
// a path expected to run.
func (e *Engine) publish(snap []engine.TorrentStatus) {
	select {
	case <-e.updates:
	default:
	}

	select {
	case e.updates <- snap:
	default:
	}
}

// snapshotsEqual reports whether two snapshots describe the same state, so
// an idle engine does not wake its subscriber twice a second with nothing
// new. Err is compared by message: an error's dynamic type need not be
// comparable, and == on one that is not panics.
func snapshotsEqual(a, b []engine.TorrentStatus) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		x, y := a[i], b[i]
		if errText(x.Err) != errText(y.Err) {
			return false
		}

		x.Err, y.Err = nil, nil
		if x != y {
			return false
		}
	}

	return true
}

// errText is err's message, or "" for nil.
func errText(err error) string {
	if err == nil {
		return ""
	}

	return err.Error()
}

// Updates returns the engine's snapshot channel. After every rate sample
// (DefaultRateSampleInterval, ~2 Hz) the engine sends one full snapshot of
// every tracked torrent, if it differs from the last one sent — never one
// message per torrent or per event. The channel buffers a single snapshot and
// a newer one replaces an unread older one, so a slow consumer loses only
// stale state and never blocks the engine. It is closed exactly once, by
// Close.
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

	e.pauseLocked(tr)
	tr.userPaused = true

	// A paused download frees its slot for the next queued torrent.
	e.wg.Add(1)

	go func() {
		defer e.wg.Done()
		e.promote()
	}()

	return nil
}

// pauseLocked holds tr's transfers and shows it paused, keeping the state to
// restore on Resume. tr must not already be paused. Engine.mu must be held.
func (e *Engine) pauseLocked(tr *tracked) {
	tr.paused = true
	e.dequeueLocked(tr.id)

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
}

// PauseForShutdown implements engine.ShutdownPauser: it pauses every torrent
// not already paused, as Pause would, without marking any of them paused by
// the user, so ResumeData still reports exactly the user's pauses (T-952).
// Nothing is promoted from the queue: every queued torrent is paused too.
func (e *Engine) PauseForShutdown() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return ErrClosed
	}

	for _, id := range e.order {
		if tr := e.torrents[id]; !tr.paused {
			e.pauseLocked(tr)
		}
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

	tr.paused, tr.userPaused = false, false

	switch {
	case tr.spacePaused:
		// Paused by the free-space re-check: clear the shortfall and
		// try again; the next re-check pauses it again if it still
		// does not fit.
		tr.spacePaused = false
		tr.err = nil
		tr.state = tr.prePauseState
	case tr.seedDone && tr.state != engine.StateErrored:
		// Stopped by the seed policy: the user asked to keep seeding,
		// which overrides the policy for this torrent from now on. (A
		// restore that failed keeps the stop only to save it again.)
		tr.seedDone = false
		tr.seedOverride = true
		tr.state = engine.StateSeeding
	case tr.state == engine.StatePaused:
		tr.state = tr.prePauseState
	}

	// A torrent that would take a download slot waits in the queue when
	// every slot is taken, with its transfers still held — provided the
	// queue has something to start it with (attached, or holding a spec or
	// URL). One whose .torrent fetch is still in flight is not queued: its
	// fetch is already running and attach picks it up from there.
	startable := tr.t != nil || tr.spec != nil || tr.url != ""
	wantsSlot := tr.state == engine.StateChecking || tr.state == engine.StateDownloading

	if startable && (tr.state == engine.StateQueued || (wantsSlot && e.activeLocked() > e.maxActive)) {
		e.enqueueLocked(tr)

		return nil
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
	name, kept, maybe := e.removeTargetLocked(tr, deleteData)

	tr.removed = true
	close(tr.done)

	delete(e.torrents, id)
	e.dequeueLocked(id)

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

	// A removed download frees its slot for the next queued torrent.
	e.promote()

	if !deleteData {
		return nil
	}

	if kept != "" {
		path := filepath.Join(savePath, kept)

		// Nothing there any more: nothing was kept, and nothing of this
		// entry is left on disk.
		if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
			return nil
		}

		e.logger.Warn("anacrolix: removed a torrent but kept its data: another download uses or may use it",
			"id", id, "path", path, "maybe", maybe)

		return &engine.DataKeptError{Path: path, Maybe: maybe}
	}

	return e.deleteTorrentData(id, savePath, name, roots)
}

// removeTargetLocked decides what a Remove of tr deletes when deleteData is
// set: name, the data's name under tr's destination ("" for nothing), or
// kept, the name of data it keeps because it is not tr's to delete — maybe
// set when no tracked entry is known to keep data there, but one might
// (T-9133, T-9135, DEC-167).
//
// An entry that is not refused deletes its torrent's own data, once the
// library knows its name. A refused entry deletes the data it left, unless
// another tracked entry at the destination keeps data under that name or has
// not named its data yet. Data whose name another entry used when this one
// was refused (sharedName) was never this entry's: it is kept even after that
// entry is gone, which may have kept it on purpose. Engine.mu must be held.
func (e *Engine) removeTargetLocked(tr *tracked, deleteData bool) (name, kept string, maybe bool) {
	switch {
	case !deleteData:
		return "", "", false
	case !tr.refused:
		if tr.t != nil {
			if info := tr.t.Info(); info != nil {
				return info.BestName(), "", false
			}
		}

		return "", "", false
	case tr.leftName != "":
		taken, pending := e.dataNameUsedLocked(tr, tr.leftName)
		if taken || pending {
			return "", tr.leftName, !taken
		}

		return tr.leftName, "", false
	case tr.sharedName != "":
		taken, _ := e.dataNameUsedLocked(tr, tr.sharedName)

		return "", tr.sharedName, !taken
	default:
		return "", "", false
	}
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

	// The name is as hostile as any path a torrent declares, and a check
	// where it was recorded is not a check here (AGENT.md §6.11): with ".."
	// the target is savePath's parent, which can still sit inside a root.
	if err := checkComponent(name); err != nil {
		e.logger.Warn("anacrolix: refused to delete torrent data under an unsafe name", "id", id, "error", err)

		return fmt.Errorf("anacrolix: refusing to delete data for torrent %s: %w", id, err)
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
		e.storages = map[string]safeStorage{}
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
