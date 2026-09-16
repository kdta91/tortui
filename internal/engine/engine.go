// Package engine defines the source-agnostic contract between the TUI and
// whatever actually moves bytes: the Engine interface, the TorrentStatus a
// caller polls or subscribes to, and the AddSource a caller hands in to start
// a download.
//
// These types are the frozen domain contracts of AGENT.md §5. The concrete
// implementation lives in internal/engine/anacrolix; a fully working
// in-memory implementation for tests lives in internal/engine/fake. Nothing
// in internal/tui may import either concrete package — it depends on this
// one only (AGENT.md §4, §6.4), which is what makes "run the whole TUI
// end-to-end against engine/fake" possible.
package engine

import (
	"context"
	"fmt"
	"time"
)

// State is where one torrent sits in its lifecycle.
type State int

const (
	// StateQueued means the torrent has been accepted but has not started
	// checking or fetching metadata yet. It is the zero value, so a
	// TorrentStatus a caller under-populated reads as "not started" rather
	// than as any specific in-progress state.
	StateQueued State = iota
	// StateChecking means metadata (the info dict) or existing on-disk data
	// is being verified. A trackerless magnet sits here until its info dict
	// arrives (AGENT.md §13).
	StateChecking
	// StateDownloading means data is actively being fetched.
	StateDownloading
	// StateSeeding means the torrent is complete and uploading to peers.
	StateSeeding
	// StatePaused means the user paused the torrent; no data moves in
	// either direction until Resume.
	StatePaused
	// StateErrored means the torrent stopped due to a failure. Err on the
	// TorrentStatus carries a readable reason (AGENT.md §13).
	StateErrored
)

// String returns a lowercase, stable token for the state: "queued",
// "checking", "downloading", "seeding", "paused", or "errored". These tokens
// are what gets written to logs and test failure messages; the TUI's own
// rendering (icons, colour, copy) is a presentation concern layered on top,
// not this method's job.
//
// An out-of-range value renders as state(N) rather than panicking or
// masquerading as a known state.
func (s State) String() string {
	switch s {
	case StateQueued:
		return "queued"
	case StateChecking:
		return "checking"
	case StateDownloading:
		return "downloading"
	case StateSeeding:
		return "seeding"
	case StatePaused:
		return "paused"
	case StateErrored:
		return "errored"
	default:
		return fmt.Sprintf("state(%d)", int(s))
	}
}

// Origin records where a torrent came from: the indexer that produced the
// result it was added from, and the human-viewable page for that result. It
// is what the downloads screen's "view source" action (AGENT.md §7) opens,
// and what lets a user tell two similarly-named downloads apart.
//
// Both fields are best-effort. A torrent added from a bare magnet URI typed
// by the user (or resumed from a session predating this field) carries a
// zero Origin — every field empty — rather than a fabricated one.
type Origin struct {
	// IndexerID is the ID of the indexer.Indexer that produced the result
	// this torrent was added from, or empty when the torrent did not come
	// from a search result.
	IndexerID string

	// SourceURL is the human-viewable page for that result — the same
	// value as indexer.Result.SourceURL at the time it was added — or
	// empty when the source published no such page.
	SourceURL string
}

// FileStatus is the download progress of one file inside a torrent, as
// returned by Engine.Files for the details screen's file listing
// (AGENT.md §7).
//
// Path is attacker-influenced (AGENT.md §6.11, §13): it comes from the
// torrent's own metadata, may contain "..", absolute-looking segments, or
// reserved names, and must be cleaned and checked for containment before it
// is ever used to open, reveal, or delete anything. An Engine implementation
// returns the path as declared by the torrent; validating it before a
// filesystem operation is the caller's responsibility, and it applies at
// every call site independently (AGENT.md §6.11) — a check here is not a
// check there.
type FileStatus struct {
	// Path is the file's path within the torrent, as declared by its
	// metadata. It is relative to the torrent's own root, not yet resolved
	// against any destination directory.
	Path string

	// SizeBytes is the file's total size.
	SizeBytes int64

	// DownloadedBytes is how much of this file has been fetched so far.
	// It never exceeds SizeBytes.
	DownloadedBytes int64

	// Progress is DownloadedBytes / SizeBytes, in [0.0, 1.0]. It is 0 for
	// a zero-size file rather than NaN or a division-by-zero panic.
	Progress float64
}

// TorrentStatus is a point-in-time snapshot of one torrent the engine is
// tracking. It is the only shape the TUI renders from — no screen reaches
// into an implementation's internals.
type TorrentStatus struct {
	// ID is the engine-assigned identifier for this torrent, stable for
	// its lifetime and returned by Add.
	ID string

	// Name is the torrent's display name — from its metadata once known,
	// or a placeholder while still in StateChecking.
	Name string

	// InfoHash is the torrent's infohash, or empty before it is known
	// (e.g. while a trackerless magnet is still fetching its info dict).
	InfoHash string

	// State is where this torrent sits in its lifecycle. See State.
	State State

	// Progress is the fraction of TotalBytes downloaded, in [0.0, 1.0].
	// It is 0 while TotalBytes is unknown, never NaN.
	Progress float64

	// DownloadedBytes and TotalBytes describe overall progress in bytes.
	// TotalBytes is 0 until metadata arrives.
	DownloadedBytes int64
	TotalBytes      int64

	// DownRate and UpRate are the current transfer rates, in bytes per
	// second. Both are 0 while paused, queued, or checking.
	DownRate int64
	UpRate   int64

	// Peers and Seeds are the current swarm counts as the engine sees
	// them — not a promise the source will still show the same numbers.
	Peers int
	Seeds int

	// ETA is the estimated time to completion, or -1 when unknown (no
	// metadata yet, stalled, or already complete).
	ETA time.Duration

	// SavePath is the absolute, cleaned destination path data is being
	// written to. It always sits inside one of the user's known
	// destination roots (AGENT.md §6.12).
	SavePath string

	// Origin records where this torrent was added from. See Origin.
	Origin Origin

	// Err is the reason for StateErrored, or nil in every other state.
	Err error
}

// AddSource carries the magnet/URL/path plus the destination chosen for this
// torrent. SavePath is always an absolute, cleaned path that has been
// verified to sit inside one of the user's known destination roots
// (AGENT.md §6.11, §6.12) — that verification happens before an AddSource is
// constructed, not inside Add.
type AddSource struct {
	// Magnet is a magnet URI. Exactly one of Magnet, TorrentURL, or
	// FilePath is expected to be set; an implementation that receives none
	// of the three rejects the call rather than guessing.
	Magnet string

	// TorrentURL is a URL to a .torrent file to fetch before adding.
	TorrentURL string

	// FilePath is the local filesystem path to an existing .torrent file.
	FilePath string

	// SavePath is the per-torrent destination. Empty means "use the
	// configured default" — an implementation, not this struct, resolves
	// that default.
	SavePath string
}

// Engine is the source-agnostic contract between the TUI and whatever
// actually moves bytes. internal/engine/anacrolix implements it against a
// real BitTorrent swarm; internal/engine/fake implements it in memory with
// scripted, deterministic progress so the TUI can be tested end-to-end
// without a network (AGENT.md §6.4).
//
// An implementation must guarantee:
//
//   - Every method is safe for concurrent use.
//   - Add returns as soon as the source is accepted; it never blocks on
//     metadata fetch or on the download itself (AGENT.md §6.1, §13).
//   - Pause, Resume, Remove, List, and Files never block on network or disk
//     I/O long enough to matter to a bubbletea Update loop; slow work
//     happens on a background goroutine (AGENT.md §6.1).
//   - Updates delivers coalesced, ~2 Hz snapshots; nothing subscribes to it
//     and then also polls List on a ticker (AGENT.md §6.5).
//   - Close is idempotent and leaves no goroutines running.
type Engine interface {
	// Add starts tracking a new torrent from src and returns its assigned
	// ID. It returns promptly — metadata fetch and the download itself
	// happen asynchronously, observed through List or Updates.
	Add(ctx context.Context, src AddSource) (string, error)

	// Pause stops a torrent's transfers without removing it. It returns an
	// error identifying an unknown id; pausing an already-paused torrent
	// is a no-op, not an error.
	Pause(id string) error

	// Resume restarts a paused torrent's transfers. It returns an error
	// identifying an unknown id; resuming a torrent that is not paused is
	// a no-op, not an error.
	Resume(id string) error

	// Remove stops and forgets a torrent. When deleteData is true, its
	// downloaded data is also deleted, subject to the destination-root
	// containment check (AGENT.md §6.11, §6.12); when false, the data is
	// left in place. It returns an error identifying an unknown id.
	Remove(id string, deleteData bool) error

	// List returns a snapshot of every torrent currently tracked, in no
	// particular guaranteed order. It is safe to call at any time and does
	// not block on network or disk I/O.
	List() []TorrentStatus

	// Files returns the per-file progress for one torrent, or an error
	// identifying an unknown id. It returns an empty slice, not an error,
	// for a torrent whose file list is not yet known (e.g. still
	// StateChecking).
	Files(id string) ([]FileStatus, error)

	// Updates delivers a coalesced snapshot of every tracked torrent
	// roughly every 500ms while anything has changed, so the TUI can
	// subscribe once instead of polling List (AGENT.md §6.5). The channel
	// is closed when Close is called; a caller must not close it.
	Updates() <-chan []TorrentStatus

	// Close stops all background work and releases every resource the
	// engine holds. It is idempotent: calling it more than once, or
	// concurrently, has the same effect as calling it once and returns nil
	// every time.
	Close() error
}
