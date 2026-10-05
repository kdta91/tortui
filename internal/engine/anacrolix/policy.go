package anacrolix

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/anacrolix/torrent"
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

// sharedNeed is what other downloads writing to the same filesystem still
// need (T-9143): their count and the bytes they have left to write.
type sharedNeed struct {
	downloads int
	bytes     int64
}

// add counts one more download with n bytes left.
func (s *sharedNeed) add(n int64) {
	s.downloads++
	s.bytes += n
}

// insufficientSpaceError builds the readable refusal: how much the torrent
// needs, what the other downloads on the same disk still need from the same
// free space, the margin, what is free, and how much is short.
func insufficientSpaceError(dest string, free uint64, need int64, others sharedNeed, margin, short int64) error {
	shared := ""
	if others.downloads > 0 {
		noun, verb := "downloads", "need"
		if others.downloads == 1 {
			noun, verb = "download", "needs"
		}

		shared = fmt.Sprintf(", sharing the disk with %d other %s that still %s %s,",
			others.downloads, noun, verb, formatBytes(others.bytes))
	}

	return fmt.Errorf("%w at %s: needs %s%s plus the %s min_free_space margin, %s free — %s short",
		ErrInsufficientSpace, dest, formatBytes(need), shared, formatBytes(margin),
		formatBytes(int64(min(free, 1<<62))), formatBytes(short))
}

// checkSpace refuses when dest cannot hold need more bytes, plus what the
// other downloads writing to the same filesystem still need, plus the margin
// (T-9143). self, when set, is the torrent being checked, never counted as
// one of the others.
func (e *Engine) checkSpace(dest string, need int64, self *tracked) error {
	writers := e.writers(self)

	free, err := e.freeSpace(dest)
	if err != nil {
		return fmt.Errorf("anacrolix: check free space: %w", err)
	}

	keys := make(map[string]string)
	key := e.filesystemKey(dest, keys)

	var others sharedNeed

	for _, w := range writers {
		if e.filesystemKey(w.dest, keys) == key {
			others.add(w.remaining)
		}
	}

	if short := spaceShortfall(free, need+others.bytes, e.margin); short > 0 {
		return insufficientSpaceError(dest, free, need, others, e.margin, short)
	}

	return nil
}

// filesystemKey names the filesystem holding dest, so the destinations that
// draw on one free space are summed together (T-9143): the platform's
// filesystem identity, or, when that cannot be read, dest resolved through
// any symlink. keys caches it per destination for one check. It does I/O, so
// it never runs under Engine.mu.
func (e *Engine) filesystemKey(dest string, keys map[string]string) string {
	if k, ok := keys[dest]; ok {
		return k
	}

	k, err := e.filesystemID(dest)
	if err != nil {
		e.logger.Debug("anacrolix: filesystem identity unreadable; summing by destination", "destination", dest, "error", err)

		k = dest
		if resolved, rerr := resolveSymlinks(dest); rerr == nil {
			k = resolved
		}

		k = "path:" + k
	}

	keys[dest] = k

	return k
}

// writer is one torrent writing data, as the free-space checks count it: its
// destination and how much it still has to write.
type writer struct {
	tr        *tracked
	dest      string
	remaining int64
}

// writers lists, in add order, every torrent but self that is writing data
// (writingLocked), taken under Engine.mu.
func (e *Engine) writers(self *tracked) []writer {
	e.mu.Lock()
	defer e.mu.Unlock()

	var out []writer

	for _, id := range e.order {
		tr := e.torrents[id]
		if tr == self || !writingLocked(tr) {
			continue
		}

		out = append(out, writer{tr: tr, dest: tr.savePath, remaining: max(0, torrentLength(tr.t)-tr.t.BytesCompleted())})
	}

	return out
}

// writingLocked reports whether tr counts toward the free space others need
// (T-9143): still tracked, attached with its size known, and downloading or
// about to (checking with its info dictionary in hand). A paused torrent
// (StatePaused, or StateErrored when the space check paused it) writes
// nothing until resumed, when the periodic re-check counts it again; a queued
// one is checked when it starts; a seeding or completed one needs nothing
// more; a refused one is errored; and one with no info dictionary yet has no
// known size. Engine.mu must be held.
func writingLocked(tr *tracked) bool {
	if tr.removed || tr.t == nil || tr.t.Info() == nil {
		return false
	}

	return tr.state == engine.StateDownloading || tr.state == engine.StateChecking
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

// torrentLength is the total length of t's data from its info dictionary, or 0
// before it has one. The library's own Length reads a cached value it writes,
// with no lock, after it publishes the info dictionary, so reading it as soon
// as Info is set races with that write; Info and the dictionary it returns
// are safe to read (T-9143).
func torrentLength(t *torrent.Torrent) int64 {
	if info := t.Info(); info != nil {
		return info.TotalLength()
	}

	return 0
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
			// attach failed before handing the spec to the client, so
			// there is nothing to drop; a re-Add starts over (T-948).
			if err := e.attach(tr, spec, tr.savePath); err != nil {
				info, infoErr := specInfo(spec)
				if infoErr != nil {
					e.logger.Warn("anacrolix: read a refused torrent's info dictionary", "id", tr.id, "error", infoErr)
				}

				e.refuse(tr, nil, info, err)
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

	length := torrentLength(tr.t)

	if tr.state == engine.StateDownloading && length > 0 && tr.t.BytesCompleted() >= length {
		tr.state = engine.StateSeeding
		// A restored torrent keeps the time it first completed, so the
		// duration policy counts across restarts (T-9135).
		if tr.completedAt.IsZero() {
			tr.completedAt = now
		}
		e.logger.Info("anacrolix: download complete", "id", tr.id, "seed_policy", e.seed.String())
	}

	if tr.state != engine.StateSeeding || tr.seedOverride {
		return
	}

	stats := tr.t.Stats()
	uploaded := tr.uploadedBefore + stats.BytesWrittenData.Int64()
	if !seedingDone(e.seed, uploaded, length, tr.completedAt, now) {
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

// recheckSpace re-checks free space for every torrent writing data and
// pauses downloads the disk can no longer hold, with a readable reason,
// rather than letting them fill it. Torrents writing to one filesystem share
// its free space (T-9143): taken in add order, each counts what it still has
// to write on top of what the ones before it still need, and a download that
// no longer fits with them and the margin is paused, after which it needs
// nothing and the ones after it are counted without it. So the oldest
// downloads keep going and a disk that each one fits alone, but not all of
// them together, is never overcommitted. The filesystem queries run outside
// Engine.mu.
func (e *Engine) recheckSpace() {
	targets := e.writers(nil)
	if len(targets) == 0 {
		return
	}

	keys := make(map[string]string)
	free := make(map[string]uint64)
	failed := make(map[string]bool)

	for _, tg := range targets {
		k := e.filesystemKey(tg.dest, keys)
		if _, ok := free[k]; ok || failed[k] {
			continue
		}

		n, err := e.freeSpace(tg.dest)
		if err != nil {
			// A destination that cannot be queried is logged, not
			// treated as full: an unmounted network share should not
			// pause every torrent on a transient error.
			e.logger.Warn("anacrolix: free-space re-check failed", "destination", tg.dest, "error", err)
			failed[k] = true

			continue
		}

		free[k] = n
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	ahead := make(map[string]sharedNeed)

	for _, tg := range targets {
		k := keys[tg.dest]

		n, ok := free[k]
		if !ok || !writingLocked(tg.tr) {
			continue
		}

		others := ahead[k]

		short := spaceShortfall(n, others.bytes+tg.remaining, e.margin)
		if short == 0 || tg.tr.state != engine.StateDownloading {
			// It fits, or it is not downloading yet and cannot be paused
			// here: either way it writes, so the ones after it count it.
			others.add(tg.remaining)
			ahead[k] = others

			continue
		}

		tr := tg.tr
		tr.paused = true
		tr.spacePaused = true
		tr.prePauseState = engine.StateDownloading
		tr.state = engine.StateErrored
		tr.err = fmt.Errorf("anacrolix: torrent %s paused: %w; free up space, then resume",
			tr.id, insufficientSpaceError(tg.dest, n, tg.remaining, others, e.margin, short))
		tr.down.reset()
		tr.up.reset()
		tr.t.DisallowDataDownload()
		e.logger.Warn("anacrolix: paused a download for lack of free space", "id", tr.id, "error", tr.err)
	}
}
