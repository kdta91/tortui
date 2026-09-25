package anacrolix

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/anacrolix/torrent/metainfo"

	"github.com/kdta91/tortui/internal/engine"
)

// DefaultSpaceCheckInterval is how often, during download, every
// downloading torrent's destination is re-checked for free space (T-034).
const DefaultSpaceCheckInterval = 10 * time.Second

// ErrInsufficientSpace reports a destination without room for a torrent plus
// the configured min_free_space margin. It is returned by Add when the
// torrent's size is known up front, and set as a torrent's Err when the
// shortfall is discovered later — when a magnet's info dictionary arrives, or
// by the periodic re-check during download, which pauses the torrent rather
// than letting it fill the disk.
var ErrInsufficientSpace = errors.New("not enough free space")

// ErrNotQueued reports a MoveInQueue for a torrent that is tracked but not
// currently waiting in the queue.
var ErrNotQueued = errors.New("anacrolix: torrent is not queued")

// Compile-time proof that Engine offers the optional queue interface.
var _ engine.Queuer = (*Engine)(nil)

// spaceShortfall reports how many bytes short dest is of need plus margin,
// or 0 when it has room. free is what the filesystem reports available.
func spaceShortfall(free uint64, need, margin int64) int64 {
	want := need + margin
	if want <= 0 {
		return 0
	}

	if free >= uint64(want) {
		return 0
	}

	return want - int64(free)
}

// insufficientSpaceError builds the readable refusal: how much the torrent
// needs, the margin, what is free, and how much is short.
func insufficientSpaceError(dest string, free uint64, need, margin, short int64) error {
	return fmt.Errorf("%w at %s: needs %s plus the %s min_free_space margin, %s free — %s short",
		ErrInsufficientSpace, dest, formatBytes(need), formatBytes(margin),
		formatBytes(int64(min(free, 1<<62))), formatBytes(short))
}

// checkSpace refuses when dest cannot hold need more bytes plus the margin.
func (e *Engine) checkSpace(dest string, need int64) error {
	free, err := e.freeSpace(dest)
	if err != nil {
		return fmt.Errorf("anacrolix: check free space: %w", err)
	}

	if short := spaceShortfall(free, need, e.margin); short > 0 {
		return insufficientSpaceError(dest, free, need, e.margin, short)
	}

	return nil
}

// bytesNeeded is how much more data an info dictionary's files will write
// under dest: each file's length less whatever is already on disk for it, so
// re-adding a torrent whose data is mostly present is not refused for space
// it already occupies. Paths are the ones validateInfoPaths already cleared.
func bytesNeeded(info *metainfo.Info, dest string) int64 {
	var need int64

	name := info.BestName()

	files := info.UpvertedFiles()
	if len(files) == 0 {
		return max(0, info.TotalLength()-existingSize(filepath.Join(dest, name)))
	}

	for _, f := range files {
		p := filepath.Join(append([]string{dest, name}, f.BestPath()...)...)
		need += max(0, f.Length-existingSize(p))
	}

	return need
}

// existingSize is the size of a regular file at p, or 0.
func existingSize(p string) int64 {
	st, err := os.Stat(p)
	if err != nil || !st.Mode().IsRegular() {
		return 0
	}

	return st.Size()
}

// formatBytes renders n in binary units, e.g. "1.5 GiB".
func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}

	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTP"[exp])
}

// activeLocked counts torrents occupying a download slot: checking (a
// metadata fetch joins a swarm) or downloading, and not paused. Seeding,
// paused, errored, and queued torrents do not hold a slot. Engine.mu must be
// held.
func (e *Engine) activeLocked() int {
	n := 0

	for _, tr := range e.torrents {
		if tr.paused {
			continue
		}

		if tr.state == engine.StateChecking || tr.state == engine.StateDownloading {
			n++
		}
	}

	return n
}

// enqueueLocked moves tr to StateQueued at the back of the queue. Engine.mu
// must be held.
func (e *Engine) enqueueLocked(tr *tracked) {
	tr.state = engine.StateQueued
	if !slices.Contains(e.queue, tr.id) {
		e.queue = append(e.queue, tr.id)
	}
}

// dequeueLocked drops id from the queue if it is there. Engine.mu must be
// held.
func (e *Engine) dequeueLocked(id string) {
	if i := slices.Index(e.queue, id); i >= 0 {
		e.queue = slices.Delete(e.queue, i, i+1)
	}
}

// Queue implements engine.Queuer: the IDs of every queued torrent,
// next-to-start first.
func (e *Engine) Queue() []string {
	e.mu.Lock()
	defer e.mu.Unlock()

	return slices.Clone(e.queue)
}

// MoveInQueue implements engine.Queuer.
func (e *Engine) MoveInQueue(id string, position int) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return ErrClosed
	}

	if _, err := e.lookupLocked(id); err != nil {
		return err
	}

	i := slices.Index(e.queue, id)
	if i < 0 {
		return fmt.Errorf("%q: %w", id, ErrNotQueued)
	}

	e.queue = slices.Delete(e.queue, i, i+1)
	position = max(0, min(position, len(e.queue)))
	e.queue = slices.Insert(e.queue, position, id)

	return nil
}

// promote starts queued torrents, in queue order, while a download slot is
// free. It runs after every sample tick and after any call that frees a slot,
// so a slot freed by a completion, a failure, a pause, or a removal is
// refilled promptly without any of those paths having to know about the
// queue.
func (e *Engine) promote() {
	for {
		tr, start := e.promoteOneLocked()
		if tr == nil {
			return
		}

		if start != nil {
			start()
		}
	}
}

// promoteOneLocked takes the next queued torrent off the queue if a slot is
// free and returns it with the work that starts it (nil when resuming an
// already-attached torrent needs nothing outside the lock). It takes
// Engine.mu itself.
func (e *Engine) promoteOneLocked() (*tracked, func()) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed || len(e.queue) == 0 || e.activeLocked() >= e.maxActive {
		return nil, nil
	}

	id := e.queue[0]
	e.queue = e.queue[1:]
	tr := e.torrents[id]

	switch {
	case tr.t != nil:
		// Attached before (resumed from a pause while every slot was
		// taken): lift the transfer gate the queue was holding.
		tr.state = engine.StateChecking
		if tr.t.Info() != nil {
			tr.state = engine.StateDownloading
		}

		tr.t.AllowDataDownload()
		tr.t.AllowDataUpload()

		return tr, nil

	case tr.url != "":
		tr.state = engine.StateChecking
		rawURL, ctx := tr.url, tr.urlCtx
		tr.url, tr.urlCtx = "", nil
		e.wg.Add(1)

		return tr, func() {
			go func() {
				defer e.wg.Done()
				e.fetchAndAttach(ctx, tr, rawURL, tr.savePath)
			}()
		}

	default:
		tr.state = engine.StateChecking
		spec := tr.spec
		tr.spec = nil

		return tr, func() {
			if err := e.attach(tr, spec, tr.savePath); err != nil {
				e.fail(tr, err)
			}
		}
	}
}

// seedingDone reports whether a completed torrent has satisfied policy:
// uploaded is the bytes it has uploaded, length its size, completedAt when it
// finished downloading.
func seedingDone(policy engine.SeedPolicy, uploaded, length int64, completedAt, now time.Time) bool {
	switch policy.Mode {
	case engine.SeedToRatio:
		return length <= 0 || float64(uploaded) >= policy.Ratio*float64(length)
	case engine.SeedForDuration:
		return !now.Before(completedAt.Add(policy.Duration))
	default:
		return true
	}
}

// applyPolicyLocked moves a torrent whose data is complete from
// StateDownloading to StateSeeding, and stops a seeding torrent once the
// seed policy is satisfied: it stops uploading and shows StatePaused, which
// Resume undoes (the user asking to keep seeding overrides the policy for
// that torrent). Engine.mu must be held.
func (e *Engine) applyPolicyLocked(tr *tracked, now time.Time) {
	if tr.t == nil || tr.paused || tr.t.Info() == nil {
		return
	}

	length := tr.t.Length()

	if tr.state == engine.StateDownloading && length > 0 && tr.t.BytesCompleted() >= length {
		tr.state = engine.StateSeeding
		tr.completedAt = now
		e.logger.Info("anacrolix: download complete", "id", tr.id, "seed_policy", e.seed.String())
	}

	if tr.state != engine.StateSeeding || tr.seedOverride {
		return
	}

	stats := tr.t.Stats()
	if !seedingDone(e.seed, stats.BytesWrittenData.Int64(), length, tr.completedAt, now) {
		return
	}

	tr.paused = true
	tr.seedDone = true
	tr.prePauseState = engine.StateSeeding
	tr.state = engine.StatePaused
	tr.up.reset()
	tr.down.reset()
	tr.t.DisallowDataUpload()
	e.logger.Info("anacrolix: seed policy satisfied, stopped uploading", "id", tr.id, "seed_policy", e.seed.String())
}

// spaceTarget is one downloading torrent the periodic free-space check looks
// at: its destination and how much it still has to write.
type spaceTarget struct {
	tr        *tracked
	dest      string
	remaining int64
}

// recheckSpace re-checks free space for every downloading torrent and pauses
// any whose destination can no longer hold what it still has to write plus
// the margin, with a readable reason, rather than letting it fill the disk.
// The filesystem queries run outside Engine.mu.
func (e *Engine) recheckSpace() {
	e.mu.Lock()

	var targets []spaceTarget

	for _, id := range e.order {
		tr := e.torrents[id]
		if tr.paused || tr.state != engine.StateDownloading || tr.t == nil || tr.t.Info() == nil {
			continue
		}

		targets = append(targets, spaceTarget{
			tr:        tr,
			dest:      tr.savePath,
			remaining: max(0, tr.t.Length()-tr.t.BytesCompleted()),
		})
	}

	e.mu.Unlock()

	if len(targets) == 0 {
		return
	}

	free := make(map[string]uint64, len(targets))
	failed := make(map[string]bool)

	for _, tg := range targets {
		if _, ok := free[tg.dest]; ok || failed[tg.dest] {
			continue
		}

		n, err := e.freeSpace(tg.dest)
		if err != nil {
			// A destination that cannot be queried is logged, not
			// treated as full: an unmounted network share should not
			// pause every torrent on a transient error.
			e.logger.Warn("anacrolix: free-space re-check failed", "destination", tg.dest, "error", err)
			failed[tg.dest] = true

			continue
		}

		free[tg.dest] = n
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	for _, tg := range targets {
		n, ok := free[tg.dest]
		if !ok {
			continue
		}

		short := spaceShortfall(n, tg.remaining, e.margin)
		if short == 0 || tg.tr.paused || tg.tr.state != engine.StateDownloading || tg.tr.removed {
			continue
		}

		tr := tg.tr
		tr.paused = true
		tr.spacePaused = true
		tr.prePauseState = engine.StateDownloading
		tr.state = engine.StateErrored
		tr.err = fmt.Errorf("anacrolix: torrent %s paused: %w; free up space, then resume",
			tr.id, insufficientSpaceError(tg.dest, n, tg.remaining, e.margin, short))
		tr.down.reset()
		tr.up.reset()
		tr.t.DisallowDataDownload()
		e.logger.Warn("anacrolix: paused a download for lack of free space", "id", tr.id, "error", tr.err)
	}
}
