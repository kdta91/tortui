package store

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	bolt "go.etcd.io/bbolt"
)

// TorrentRecord is what the store keeps about one torrent the user has
// added, keyed by ID: where it came from, when it was added, and where its
// data is saved. It is deliberately independent of internal/engine's
// TorrentStatus — the store persists identity and provenance, not live
// transfer state, so it has no dependency on the engine package at all.
type TorrentRecord struct {
	// ID is the engine-assigned torrent ID (engine.TorrentStatus.ID) and
	// is the bucket key. Never empty for a stored record.
	ID string `json:"id"`

	// IndexerID is the ID of the indexer.Indexer the torrent was added
	// from, or empty when it was added from a bare magnet/file the user
	// supplied directly.
	IndexerID string `json:"indexer_id,omitempty"`

	// SourceURL is the human-viewable page the torrent was found on, or
	// empty when the source published no such page.
	SourceURL string `json:"source_url,omitempty"`

	// AddedAt is when the torrent was added to the engine.
	AddedAt time.Time `json:"added_at"`

	// SavePath is the absolute destination path chosen for this torrent's
	// data.
	SavePath string `json:"save_path"`
}

// SetTorrent inserts or replaces the record for rec.ID. It returns
// ErrEmptyID if rec.ID is empty and ErrClosed if the store has been
// closed.
func (s *Store) SetTorrent(rec TorrentRecord) error {
	if rec.ID == "" {
		return ErrEmptyID
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	s.torrents[rec.ID] = rec
	s.dirty = true
	return nil
}

// DeleteTorrent removes the record for id, if any. Deleting an id that is
// not present is not an error.
func (s *Store) DeleteTorrent(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if _, ok := s.torrents[id]; !ok {
		return nil
	}
	delete(s.torrents, id)
	s.dirty = true
	return nil
}

// GetTorrent returns the record for id and whether it was found.
func (s *Store) GetTorrent(id string) (TorrentRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.torrents[id]
	return rec, ok
}

// ListTorrents returns every stored torrent record, sorted by ID for a
// deterministic order.
func (s *Store) ListTorrents() []TorrentRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]TorrentRecord, 0, len(s.torrents))
	for _, rec := range s.torrents {
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// writeTorrents rewrites the torrents bucket from scratch with snapshot.
// Called only from flush, which already holds no lock (it operates on a
// point-in-time copy), so this needs none either.
func writeTorrents(tx *bolt.Tx, snapshot map[string]TorrentRecord) error {
	b, err := resetBucket(tx, bucketTorrents)
	if err != nil {
		return err
	}
	for id, rec := range snapshot {
		data, err := json.Marshal(rec)
		if err != nil {
			return fmt.Errorf("store: encode torrent %q: %w", id, err)
		}
		if err := b.Put([]byte(id), data); err != nil {
			return fmt.Errorf("store: write torrent %q: %w", id, err)
		}
	}
	return nil
}
