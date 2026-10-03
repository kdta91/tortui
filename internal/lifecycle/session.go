package lifecycle

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/store"
)

// Session ties the engine's torrents to the store's torrents bucket across
// restarts (T-041): Resume puts every recorded torrent back into the engine
// at startup, and Save records what the engine is tracking now.
//
// The composition root builds one Session after opening the store and the
// engine, calls Resume once before the TUI starts, calls Save after every
// add or remove, and passes it to Shutdown, which closes it (one last save)
// before the final store flush. Save is a no-op until Resume has succeeded,
// because a save prunes records the engine is not tracking — before Resume
// that would be every record, and a crash between startup and resume would
// otherwise wipe the user's session.
//
// The Session is the one owner of the torrents bucket once it is built
// (T-994): Resume, Save, SetTorrent, and Close all run under one lock, so a
// Save in flight on a tea.Cmd goroutine can neither overwrite nor prune a
// record the add flow writes through SetTorrent meanwhile, and no Save runs
// after Close — which Shutdown calls before it closes the store or the
// engine.
type Session struct {
	engine engine.Engine
	store  *store.Store
	logger *slog.Logger
	now    func() time.Time

	mu      sync.Mutex
	resumed bool

	// closed is set by Close under mu, or by Shutdown without it when
	// Close did not finish in time (seal). Every later Save and
	// SetTorrent sees it once it holds mu and touches nothing.
	closed atomic.Bool

	// afterUnlock, when set, runs each time a method lets go of mu, on
	// that method's goroutine, before it returns. Test seam only: it lets
	// a test take the lock at the exact moment it is released (T-9008).
	afterUnlock func()
}

// ErrSessionClosed is returned by SetTorrent after Close.
var ErrSessionClosed = errors.New("lifecycle: session is closed")

// NewSession returns a Session over e and st. A nil logger uses
// slog.Default().
func NewSession(e engine.Engine, st *store.Store, logger *slog.Logger) *Session {
	if logger == nil {
		logger = slog.Default()
	}

	return &Session{engine: e, store: st, logger: logger, now: time.Now}
}

// ResumeReport says what Resume did with each recorded torrent.
type ResumeReport struct {
	// Restored counts torrents put back into the engine, including the
	// ones listed in Missing and Failed: every recorded torrent stays
	// visible in the downloads list.
	Restored int

	// Missing lists torrents whose downloaded data is no longer on disk
	// (engine.ErrDataMissing). Each shows as StateErrored with the reason;
	// the user is offered to remove the entry rather than having it
	// silently dropped or silently downloaded again.
	Missing []engine.TorrentStatus

	// Failed lists torrents restored in StateErrored for any other
	// reason: a destination no longer among the known roots, unreadable
	// or unsafe saved metadata, not enough free space.
	Failed []engine.TorrentStatus

	// Dropped lists records Resume dropped from the store because the
	// engine restored them onto a torrent an older record had already
	// restored: one infohash recorded twice (T-953). Not counted in
	// Restored. Nothing on disk is touched (T-9127).
	Dropped []DroppedRecord
}

// DroppedRecord is a duplicate record Resume dropped.
type DroppedRecord struct {
	// ID, Name and SavePath are the dropped record's own; Name is safe to
	// show (engine.SafeName), never an address.
	ID       string
	Name     string
	SavePath string

	// KeptAs is the ID of the torrent the older record restored.
	KeptAs string

	// Elsewhere is set when SavePath is not where the kept torrent's data
	// lives: whatever the dropped record downloaded there stays on disk,
	// and nothing tracks it any more.
	Elsewhere bool
}

// Resume re-adds every torrent recorded in the store to the engine, oldest
// first so queue order survives, and re-keys any record whose torrent came
// back under a different ID. A record the engine restores onto a torrent this
// pass already restored — the same infohash as an older record — is dropped
// from the store, not counted, and listed in ResumeReport.Dropped (T-953,
// T-9127). It returns an error only when the
// engine cannot resume at all or stopped accepting torrents part-way (closed,
// or ctx cancelled); every per-torrent problem is reported in the
// ResumeReport and shows in the engine as StateErrored instead.
//
// An engine that does not implement engine.Resumer resumes nothing, and the
// store is left untouched.
func (s *Session) Resume(ctx context.Context) (ResumeReport, error) {
	s.mu.Lock()
	defer s.unlock()

	var report ResumeReport

	r, ok := s.engine.(engine.Resumer)
	if !ok {
		s.logger.Warn("lifecycle: engine cannot resume torrents; session left as recorded")
		return report, nil
	}

	// Every destination the user chose in an earlier session is a known
	// root again before anything is restored into it (AGENT.md §6.12,
	// T-074), so a torrent saved there resumes rather than coming back
	// errored as "outside every known root".
	if ra, ok := s.engine.(engine.RootAdder); ok {
		for _, dir := range s.store.Destinations() {
			if err := ra.AddRoot(dir); err != nil {
				s.logger.Warn("lifecycle: recorded destination not restored as a root", "dir", dir, "error", err)
			}
		}
	}

	records := s.store.ListTorrents()
	slices.SortStableFunc(records, func(a, b store.TorrentRecord) int {
		return cmp.Or(a.AddedAt.Compare(b.AddedAt), cmp.Compare(a.ID, b.ID))
	})

	restoredIDs := make([]string, 0, len(records))
	restored := make(map[string]bool, len(records))

	for _, rec := range records {
		id, err := r.Restore(ctx, resumeDataFrom(rec))
		if err != nil {
			return report, fmt.Errorf("lifecycle: resume torrent %s: %w", rec.ID, err)
		}

		if restored[id] {
			// The engine handed back a torrent this pass already
			// restored: this record names the same infohash as an
			// earlier one (T-953). Re-keying it would overwrite that
			// record, so it is dropped instead; the earlier, older
			// record stays as it was.
			if id != rec.ID {
				if err := s.store.DeleteTorrent(rec.ID); err != nil {
					return report, fmt.Errorf("lifecycle: drop duplicate torrent %s: %w", rec.ID, err)
				}
			}

			s.logger.Warn("lifecycle: dropped a record naming a torrent already restored",
				"id", rec.ID, "restored_as", id, "save_path", rec.SavePath)

			// A session saved before T-9057 may name a torrent added by
			// address with that address, api key and all; it is shown by
			// host only, as the engine shows it.
			report.Dropped = append(report.Dropped, DroppedRecord{
				ID: rec.ID, Name: engine.SafeName(rec.Name), SavePath: rec.SavePath, KeptAs: id,
			})

			continue
		}

		restored[id] = true

		if id != rec.ID {
			if err := s.store.DeleteTorrent(rec.ID); err != nil {
				return report, fmt.Errorf("lifecycle: re-key torrent %s: %w", rec.ID, err)
			}

			rec.ID = id
			if err := s.store.SetTorrent(rec); err != nil {
				return report, fmt.Errorf("lifecycle: re-key torrent %s: %w", id, err)
			}
		}

		restoredIDs = append(restoredIDs, id)
		report.Restored++
	}

	statuses := make(map[string]engine.TorrentStatus)
	for _, st := range s.engine.List() {
		statuses[st.ID] = st
	}

	for i, d := range report.Dropped {
		kept, ok := statuses[d.KeptAs]
		report.Dropped[i].Elsewhere = ok && d.SavePath != "" &&
			filepath.Clean(d.SavePath) != filepath.Clean(kept.SavePath)
	}

	for _, id := range restoredIDs {
		st, ok := statuses[id]
		if !ok || st.State != engine.StateErrored {
			continue
		}

		if errors.Is(st.Err, engine.ErrDataMissing) {
			report.Missing = append(report.Missing, st)
		} else {
			report.Failed = append(report.Failed, st)
		}
	}

	s.resumed = true

	s.logger.Info("lifecycle: session resumed",
		"restored", report.Restored, "missing_data", len(report.Missing), "failed", len(report.Failed),
		"dropped", len(report.Dropped))

	return report, nil
}

// Save records every torrent the engine tracks into the store, keeping each
// record's added-at time and origin, and deletes records for torrents the
// engine no longer tracks. The store persists them on its own debounce
// (AGENT.md §13); Save itself never writes to disk.
//
// Save does nothing until Resume has succeeded (see Session), nothing after
// Close, and nothing for an engine that does not implement engine.Resumer.
// Concurrent calls run one at a time.
func (s *Session) Save() error {
	s.mu.Lock()
	defer s.unlock()

	if s.closed.Load() {
		s.logger.Debug("lifecycle: session closed; not saving")
		return nil
	}

	return s.saveLocked()
}

// SetTorrent records rec in the store, replacing any record for rec.ID. It
// runs under the same lock as Save, so a Save already in flight finishes
// before the record lands — it can neither overwrite rec with a record it
// read earlier nor prune rec as untracked, since the engine accepted the
// torrent before the add flow calls this. It returns ErrSessionClosed after
// Close, and the store's error otherwise. The add flow's record write goes
// through here (tui.TorrentStore).
func (s *Session) SetTorrent(rec store.TorrentRecord) error {
	s.mu.Lock()
	defer s.unlock()

	if s.closed.Load() {
		return ErrSessionClosed
	}

	if err := s.store.SetTorrent(rec); err != nil {
		return fmt.Errorf("lifecycle: record torrent %s: %w", rec.ID, err)
	}

	return nil
}

// GetTorrent returns the store's record for id. It reads the store
// directly, without waiting for a Save in flight, so the downloads screen
// never blocks on one.
func (s *Session) GetTorrent(id string) (store.TorrentRecord, bool) {
	return s.store.GetTorrent(id)
}

// Close saves the session one last time, after any Save already in flight,
// then makes every later Save a no-op and every later SetTorrent fail with
// ErrSessionClosed. Shutdown calls it before it flushes and closes the store
// and closes the engine, so no save runs after either closes. Calling it
// again does nothing.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.unlock()

	if s.closed.Load() {
		return nil
	}

	err := s.saveLocked()
	s.closed.Store(true)

	return err
}

// unlock releases mu, then runs the afterUnlock test seam, if any.
func (s *Session) unlock() {
	s.mu.Unlock()

	if s.afterUnlock != nil {
		s.afterUnlock()
	}
}

// seal makes every Save or Close that has not yet taken the lock do
// nothing. Shutdown calls it after its Close step even when that step timed
// out waiting behind a hung Save, so the Close left waiting cannot run
// against the store and engine Shutdown then closes.
func (s *Session) seal() { s.closed.Store(true) }

// saveLocked is Save's body; the caller holds s.mu.
func (s *Session) saveLocked() error {
	if !s.resumed {
		s.logger.Debug("lifecycle: session not resumed yet; not saving")
		return nil
	}

	r, ok := s.engine.(engine.Resumer)
	if !ok {
		return nil
	}

	tracked := make(map[string]bool)
	failed := make(map[string]error)

	var errs []error

	for _, st := range s.engine.List() {
		d, err := r.ResumeData(st.ID)
		if err != nil {
			failed[st.ID] = err
			continue
		}

		tracked[st.ID] = true

		rec, _ := s.store.GetTorrent(st.ID)
		if err := s.store.SetTorrent(mergeRecord(rec, d, s.now())); err != nil {
			errs = append(errs, fmt.Errorf("lifecycle: save torrent %s: %w", st.ID, err))
		}
	}

	if len(failed) > 0 {
		// A torrent removed between List and ResumeData is simply gone,
		// and the prune below drops its record. One still tracked keeps
		// its previous record rather than losing it.
		for _, st := range s.engine.List() {
			if err, ok := failed[st.ID]; ok {
				tracked[st.ID] = true
				errs = append(errs, fmt.Errorf("lifecycle: save torrent %s: %w", st.ID, err))
			}
		}
	}

	for _, rec := range s.store.ListTorrents() {
		if tracked[rec.ID] {
			continue
		}

		if err := s.store.DeleteTorrent(rec.ID); err != nil {
			errs = append(errs, fmt.Errorf("lifecycle: forget torrent %s: %w", rec.ID, err))
		}
	}

	return errors.Join(errs...)
}

// resumeDataFrom converts a store record into what engine.Resumer.Restore
// takes.
func resumeDataFrom(rec store.TorrentRecord) engine.ResumeData {
	return engine.ResumeData{
		ID:         rec.ID,
		Name:       rec.Name,
		Magnet:     rec.Magnet,
		TorrentURL: rec.TorrentURL,
		Metainfo:   rec.Metainfo,
		SavePath:   rec.SavePath,
		Paused:     rec.Paused,
		Origin: engine.Origin{
			IndexerID: rec.IndexerID,
			SourceURL: rec.SourceURL,
		},
	}
}

// mergeRecord folds fresh resume data into the existing record for the same
// torrent. AddedAt is kept once set. Origin is kept when the engine reports
// none, so an origin the add flow recorded directly in the store survives
// (T-070); otherwise the engine's wins.
//
// Name is likewise kept — the result's title the add flow recorded — until
// the engine knows the torrent's own name, which is when it has the
// torrent's metainfo: before then the engine names a torrent added by
// address only by the address's host (T-9057). A recorded name that is a web
// address, as a session saved before T-9057 holds, is never kept.
func mergeRecord(rec store.TorrentRecord, d engine.ResumeData, now time.Time) store.TorrentRecord {
	if rec.AddedAt.IsZero() {
		rec.AddedAt = now
	}

	if d.Origin != (engine.Origin{}) {
		rec.IndexerID = d.Origin.IndexerID
		rec.SourceURL = d.Origin.SourceURL
	}

	recordedTitle := strings.TrimSpace(rec.Name) != "" && engine.SafeName(rec.Name) == rec.Name
	if !recordedTitle || len(d.Metainfo) > 0 {
		rec.Name = engine.SafeName(d.Name)
	}

	rec.ID = d.ID
	rec.SavePath = d.SavePath
	rec.Magnet = d.Magnet
	rec.TorrentURL = d.TorrentURL
	rec.Paused = d.Paused

	if len(d.Metainfo) > 0 {
		rec.Metainfo = d.Metainfo
	}

	return rec
}
