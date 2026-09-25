package lifecycle

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
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
// add or remove, and passes it to Shutdown, which saves once more before the
// final store flush. Save is a no-op until Resume has succeeded, because a
// save prunes records the engine is not tracking — before Resume that would
// be every record, and a crash between startup and resume would otherwise
// wipe the user's session.
type Session struct {
	engine engine.Engine
	store  *store.Store
	logger *slog.Logger
	now    func() time.Time

	mu      sync.Mutex
	resumed bool
}

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
}

// Resume re-adds every torrent recorded in the store to the engine, oldest
// first so queue order survives, and re-keys any record whose torrent came
// back under a different ID. It returns an error only when the engine cannot
// resume at all or stopped accepting torrents part-way (closed, or ctx
// cancelled); every per-torrent problem is reported in the ResumeReport and
// shows in the engine as StateErrored instead.
//
// An engine that does not implement engine.Resumer resumes nothing, and the
// store is left untouched.
func (s *Session) Resume(ctx context.Context) (ResumeReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var report ResumeReport

	r, ok := s.engine.(engine.Resumer)
	if !ok {
		s.logger.Warn("lifecycle: engine cannot resume torrents; session left as recorded")
		return report, nil
	}

	records := s.store.ListTorrents()
	slices.SortStableFunc(records, func(a, b store.TorrentRecord) int {
		return cmp.Or(a.AddedAt.Compare(b.AddedAt), cmp.Compare(a.ID, b.ID))
	})

	restoredIDs := make([]string, 0, len(records))

	for _, rec := range records {
		id, err := r.Restore(ctx, resumeDataFrom(rec))
		if err != nil {
			return report, fmt.Errorf("lifecycle: resume torrent %s: %w", rec.ID, err)
		}

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
		"restored", report.Restored, "missing_data", len(report.Missing), "failed", len(report.Failed))

	return report, nil
}

// Save records every torrent the engine tracks into the store, keeping each
// record's added-at time and origin, and deletes records for torrents the
// engine no longer tracks. The store persists them on its own debounce
// (AGENT.md §13); Save itself never writes to disk.
//
// Save does nothing until Resume has succeeded (see Session), and nothing
// for an engine that does not implement engine.Resumer.
func (s *Session) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

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
		Origin:     engine.Origin{IndexerID: rec.IndexerID, SourceURL: rec.SourceURL},
	}
}

// mergeRecord folds fresh resume data into the existing record for the same
// torrent. AddedAt is kept once set. Origin is kept when the engine reports
// none, so an origin the add flow recorded directly in the store survives
// (T-070); otherwise the engine's wins.
func mergeRecord(rec store.TorrentRecord, d engine.ResumeData, now time.Time) store.TorrentRecord {
	if rec.AddedAt.IsZero() {
		rec.AddedAt = now
	}

	if d.Origin != (engine.Origin{}) {
		rec.IndexerID, rec.SourceURL = d.Origin.IndexerID, d.Origin.SourceURL
	}

	rec.ID = d.ID
	rec.Name = d.Name
	rec.SavePath = d.SavePath
	rec.Magnet = d.Magnet
	rec.TorrentURL = d.TorrentURL

	if len(d.Metainfo) > 0 {
		rec.Metainfo = d.Metainfo
	}

	return rec
}
