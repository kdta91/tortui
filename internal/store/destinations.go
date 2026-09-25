package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	bolt "go.etcd.io/bbolt"
)

// destinationsKey holds the used-destinations list inside the prefs bucket.
// It is a key of its own rather than a Prefs field so a SetPrefs caller
// replacing the whole Prefs value can never drop a destination by accident:
// the list is part of the known-roots set (AGENT.md §6.12) and must only
// ever grow through TouchDestination.
var destinationsKey = []byte("destinations")

// ErrEmptyDestination is returned by TouchDestination for an empty path.
var ErrEmptyDestination = errors.New("store: destination must not be empty")

// Destinations returns every download destination the user has added a
// torrent to (T-074), most recently used first. Each is the absolute path
// the caller recorded. The list is never trimmed: every entry is a known
// destination root.
func (s *Store) Destinations() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.destinations...)
}

// TouchDestination records path as the most recently used destination,
// moving it to the front if it is already known.
func (s *Store) TouchDestination(path string) error {
	if strings.TrimSpace(path) == "" {
		return ErrEmptyDestination
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrClosed
	}

	next := make([]string, 0, len(s.destinations)+1)
	next = append(next, path)

	for _, d := range s.destinations {
		if d != path {
			next = append(next, d)
		}
	}

	s.destinations = next
	s.dirty = true

	return nil
}

// loadDestinations decodes the used-destinations list from the prefs bucket.
func loadDestinations(pb *bolt.Bucket) ([]string, error) {
	raw := pb.Get(destinationsKey)
	if raw == nil {
		return nil, nil
	}

	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("store: decode destinations: %w", err)
	}

	return out, nil
}

// writeDestinations writes the used-destinations list into the prefs bucket.
func writeDestinations(tx *bolt.Tx, snapshot []string) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("store: encode destinations: %w", err)
	}

	if err := tx.Bucket(bucketPrefs).Put(destinationsKey, data); err != nil {
		return fmt.Errorf("store: write destinations: %w", err)
	}

	return nil
}
