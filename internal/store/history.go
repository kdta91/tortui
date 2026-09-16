package store

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

// HistoryEntry is one recent search query, shown so a user can re-run a
// past search from the search screen.
type HistoryEntry struct {
	// Text is the query text as typed. Empty for a Latest-mode query.
	Text string `json:"text"`

	// At is when the query was run.
	At time.Time `json:"at"`
}

// AddHistory records text as the most recent query. The oldest entry is
// dropped once the count exceeds maxHistoryEntries.
func (s *Store) AddHistory(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}

	s.historySeq++
	s.history = append(s.history, historyRecord{
		seq:   s.historySeq,
		entry: HistoryEntry{Text: text, At: time.Now()},
	})
	if len(s.history) > maxHistoryEntries {
		s.history = s.history[len(s.history)-maxHistoryEntries:]
	}
	s.dirty = true
	return nil
}

// ListHistory returns recorded queries, most recent first.
func (s *Store) ListHistory() []HistoryEntry {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]HistoryEntry, len(s.history))
	for i, rec := range s.history {
		out[len(s.history)-1-i] = rec.entry
	}
	return out
}

// writeHistory rewrites the history bucket from scratch with snapshot,
// keyed by each entry's big-endian sequence number so iteration order on
// disk matches insertion order.
func writeHistory(tx *bolt.Tx, snapshot []historyRecord) error {
	b, err := resetBucket(tx, bucketHistory)
	if err != nil {
		return err
	}
	for _, rec := range snapshot {
		data, err := json.Marshal(rec.entry)
		if err != nil {
			return fmt.Errorf("store: encode history entry %d: %w", rec.seq, err)
		}
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, rec.seq)
		if err := b.Put(key, data); err != nil {
			return fmt.Errorf("store: write history entry %d: %w", rec.seq, err)
		}
	}
	return nil
}
