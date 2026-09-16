// Package fake is an in-memory implementation of engine.Engine that drives
// scripted, deterministic progress instead of a real BitTorrent swarm. It
// exists so the TUI can be developed and tested end-to-end — every screen,
// every keybind, every state transition — without a network, a real
// download, or a wall-clock sleep in a test (AGENT.md §6.4, §6.7).
//
// Progress is driven by an explicit Advance call rather than real time: a
// caller controls exactly how far each torrent's simulated run time moves,
// which is what makes a test deterministic instead of racing a timer.
// --demo mode (AGENT.md §15) wraps this engine with a goroutine that calls
// Advance on a real ticker; tests call Advance directly.
package fake

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kdta91/tortui/internal/engine"
)

// ErrNoSource reports an AddSource with none of Magnet, TorrentURL, or
// FilePath set — there is nothing for even a fake engine to track.
var ErrNoSource = errors.New("fake: no magnet, torrent URL, or file path given")

// ErrClosed reports a call made after Close.
var ErrClosed = errors.New("fake: engine is closed")

// ErrNotFound reports an id that does not name a currently-tracked torrent,
// whether because it was never added, or because it was removed.
var ErrNotFound = errors.New("fake: torrent not found")

// tracked is one torrent's mutable simulation state, guarded by Engine.mu.
type tracked struct {
	status     engine.TorrentStatus
	script     Script
	runElapsed time.Duration // simulated run time, excluding time spent paused
	paused     bool
}

// Engine is an in-memory, scripted engine.Engine. The zero value is not
// usable; construct one with New.
type Engine struct {
	mu sync.Mutex

	nextID   int
	torrents map[string]*tracked
	order    []string // insertion order, so List/Files/Advance are deterministic
	updates  chan []engine.TorrentStatus
	closed   bool

	// ScriptFor chooses the Script a newly added torrent will follow. It
	// is called synchronously from Add and must not itself call back into
	// the Engine. A nil ScriptFor (the default from New) gives every
	// torrent Downloading(30 * time.Second).
	//
	// Set it before any call to Add; it is read without a lock and is not
	// safe to change concurrently with one.
	ScriptFor func(src engine.AddSource) Script
}

// New returns a ready-to-use Engine tracking no torrents.
func New() *Engine {
	return &Engine{
		torrents: make(map[string]*tracked),
		updates:  make(chan []engine.TorrentStatus, 1),
	}
}

// Add records a new torrent and assigns it a Script — from ScriptFor if set,
// otherwise Downloading(30 * time.Second) — then applies that Script's event
// at run time zero, if any, before returning.
func (e *Engine) Add(ctx context.Context, src engine.AddSource) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(src.Magnet) == "" &&
		strings.TrimSpace(src.TorrentURL) == "" &&
		strings.TrimSpace(src.FilePath) == "" {
		return "", ErrNoSource
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return "", ErrClosed
	}

	e.nextID++
	id := fmt.Sprintf("fake-%d", e.nextID)

	t := &tracked{
		script: e.scriptFor(src),
		status: engine.TorrentStatus{
			ID:       id,
			Name:     nameFor(src, id),
			State:    engine.StateQueued,
			SavePath: src.SavePath,
			ETA:      -1,
		},
	}
	e.applyScriptLocked(t)

	e.torrents[id] = t
	e.order = append(e.order, id)
	e.publishLocked()

	return id, nil
}

// Pause marks a torrent paused: its simulated run time stops advancing and
// its reported state, download rate, and upload rate read as paused/zero
// until Resume. Pausing an already-paused torrent is a no-op.
func (e *Engine) Pause(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	t, err := e.lookupLocked(id)
	if err != nil {
		return err
	}
	if t.paused {
		return nil
	}
	t.paused = true
	e.applyScriptLocked(t)
	e.publishLocked()
	return nil
}

// Resume un-pauses a torrent: its simulated run time resumes advancing on
// the next Advance call. Resuming a torrent that is not paused is a no-op.
func (e *Engine) Resume(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	t, err := e.lookupLocked(id)
	if err != nil {
		return err
	}
	if !t.paused {
		return nil
	}
	t.paused = false
	e.applyScriptLocked(t)
	e.publishLocked()
	return nil
}

// Remove stops tracking a torrent. deleteData is accepted to satisfy
// engine.Engine but otherwise ignored: a fake torrent has no data on disk to
// delete.
func (e *Engine) Remove(id string, _ bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, err := e.lookupLocked(id); err != nil {
		return err
	}
	delete(e.torrents, id)
	for i, existing := range e.order {
		if existing == id {
			e.order = append(e.order[:i], e.order[i+1:]...)
			break
		}
	}
	e.publishLocked()
	return nil
}

// List returns a snapshot of every currently-tracked torrent, in the order
// each was added.
func (e *Engine) List() []engine.TorrentStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.snapshotLocked()
}

// Files returns a single synthetic FileStatus standing in for the whole
// torrent — this fake does not model multi-file torrents — or an empty
// slice while TotalBytes is not yet known (e.g. still StateChecking).
func (e *Engine) Files(id string) ([]engine.FileStatus, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	t, err := e.lookupLocked(id)
	if err != nil {
		return nil, err
	}
	if t.status.TotalBytes == 0 {
		return []engine.FileStatus{}, nil
	}

	progress := 0.0
	if t.status.TotalBytes > 0 {
		progress = float64(t.status.DownloadedBytes) / float64(t.status.TotalBytes)
	}

	return []engine.FileStatus{
		{
			Path:            t.status.Name,
			SizeBytes:       t.status.TotalBytes,
			DownloadedBytes: t.status.DownloadedBytes,
			Progress:        progress,
		},
	}, nil
}

// Updates returns the channel every coalesced status snapshot is delivered
// on. It is closed when Close is called.
func (e *Engine) Updates() <-chan []engine.TorrentStatus {
	return e.updates
}

// Close stops the Engine. It is idempotent: a second or concurrent call
// observes the same closed state and returns nil.
func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil
	}
	e.closed = true
	close(e.updates)
	return nil
}

// Advance moves every non-paused torrent's simulated run time forward by d
// and re-applies its Script at the new run time, then publishes one
// coalesced snapshot. It is the only thing that makes simulated time pass —
// nothing in this package reads the wall clock. A non-positive d is a no-op.
func (e *Engine) Advance(d time.Duration) {
	if d <= 0 {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return
	}

	for _, id := range e.order {
		t, ok := e.torrents[id]
		if !ok {
			continue
		}
		if !t.paused {
			t.runElapsed += d
		}
		e.applyScriptLocked(t)
	}
	e.publishLocked()
}

// scriptFor resolves the Script a new AddSource gets: ScriptFor if set,
// otherwise a 30-second Downloading default.
func (e *Engine) scriptFor(src engine.AddSource) Script {
	if e.ScriptFor != nil {
		return e.ScriptFor(src)
	}
	return Downloading(30 * time.Second)
}

// applyScriptLocked applies t.script's event at t.runElapsed onto t.status,
// then overrides the lifecycle-visible fields when paused. Callers must hold
// e.mu.
func (e *Engine) applyScriptLocked(t *tracked) {
	ev, ok := t.script.AtOrBefore(t.runElapsed)
	if ok {
		t.status.State = ev.State
		t.status.Progress = ev.Progress
		t.status.DownloadedBytes = ev.DownloadedBytes
		t.status.TotalBytes = ev.TotalBytes
		t.status.DownRate = ev.DownRate
		t.status.UpRate = ev.UpRate
		t.status.Peers = ev.Peers
		t.status.Seeds = ev.Seeds
		t.status.ETA = ev.ETA
		t.status.Err = ev.Err
	}

	if t.paused {
		t.status.State = engine.StatePaused
		t.status.DownRate = 0
		t.status.UpRate = 0
	}
}

// lookupLocked resolves id to its tracked torrent. Callers must hold e.mu.
func (e *Engine) lookupLocked(id string) (*tracked, error) {
	t, ok := e.torrents[id]
	if !ok {
		return nil, fmt.Errorf("%q: %w", id, ErrNotFound)
	}
	return t, nil
}

// snapshotLocked builds the []engine.TorrentStatus returned by List and sent
// on Updates. Callers must hold e.mu.
func (e *Engine) snapshotLocked() []engine.TorrentStatus {
	out := make([]engine.TorrentStatus, 0, len(e.order))
	for _, id := range e.order {
		if t, ok := e.torrents[id]; ok {
			out = append(out, t.status)
		}
	}
	return out
}

// publishLocked sends the current snapshot on updates without blocking,
// dropping any snapshot that was sitting unread — Updates is documented as
// coalesced, so only the latest value matters. Callers must hold e.mu.
func (e *Engine) publishLocked() {
	if e.closed {
		return
	}
	snap := e.snapshotLocked()
	select {
	case <-e.updates:
	default:
	}
	select {
	case e.updates <- snap:
	default:
	}
}

// nameFor derives a display name for a newly added torrent from whichever
// of Magnet, TorrentURL, or FilePath is set, falling back to id when none of
// them yields anything useful.
func nameFor(src engine.AddSource, id string) string {
	switch {
	case src.FilePath != "":
		return filepath.Base(src.FilePath)
	case src.TorrentURL != "":
		if base := path.Base(src.TorrentURL); base != "" && base != "." && base != "/" {
			return base
		}
		return id
	case src.Magnet != "":
		return magnetName(src.Magnet, id)
	default:
		return id
	}
}

// magnetName extracts the dn= display-name parameter from a magnet URI,
// falling back to fallback when the URI does not parse or carries no dn.
func magnetName(magnet, fallback string) string {
	u, err := url.Parse(magnet)
	if err != nil {
		return fallback
	}
	if dn := u.Query().Get("dn"); dn != "" {
		return dn
	}
	return fallback
}
