package store

import (
	"encoding/json"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

// Prefs is the small set of UI preferences the store remembers across
// restarts: sort column, last screen visited, and which sources were
// selected for search. There is exactly one Prefs value for the whole
// application.
type Prefs struct {
	// SortColumn is the results-table column last sorted on.
	SortColumn string `json:"sort_column,omitempty"`

	// LastScreen is the screen name active when the app last closed.
	LastScreen string `json:"last_screen,omitempty"`

	// SelectedSources is the set of indexer IDs last chosen in the
	// source multi-select.
	SelectedSources []string `json:"selected_sources,omitempty"`
}

// GetPrefs returns the current preferences.
func (s *Store) GetPrefs() Prefs {
	s.mu.Lock()
	defer s.mu.Unlock()
	return copyPrefs(s.prefs)
}

// SetPrefs replaces the stored preferences.
func (s *Store) SetPrefs(p Prefs) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	s.prefs = copyPrefs(p)
	s.dirty = true
	return nil
}

func copyPrefs(p Prefs) Prefs {
	out := p
	if p.SelectedSources != nil {
		out.SelectedSources = append([]string(nil), p.SelectedSources...)
	}
	return out
}

// writePrefs writes the single prefs value from scratch. Unlike the other
// buckets, prefs is a single key, so no reset-then-repopulate is needed —
// a Put simply overwrites it.
func writePrefs(tx *bolt.Tx, snapshot Prefs) error {
	b := tx.Bucket(bucketPrefs)
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("store: encode prefs: %w", err)
	}
	if err := b.Put(prefsKey, data); err != nil {
		return fmt.Errorf("store: write prefs: %w", err)
	}
	return nil
}
