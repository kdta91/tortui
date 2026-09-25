// Package store is tortui's bbolt-backed persistence layer: the torrents a
// user has added (for session resume, T-041), recent search queries, and
// screen/sort/source preferences. It is the only package that touches the
// on-disk database file, and it depends on nothing above internal/config —
// not the engine, not the indexer registry, not the TUI — so it can be
// built, tested, and reasoned about in isolation from all of them.
//
// Every exported mutation (SetTorrent, DeleteTorrent, AddHistory, SetPrefs)
// only updates an in-memory copy of the data and marks the store dirty. A
// dedicated goroutine started by Open flushes dirty state to the bbolt file
// on a fixed interval (five seconds in production, AGENT.md §13) rather
// than blocking the caller on disk I/O for every single change. Close
// stops that goroutine and performs one last synchronous flush so no
// pending write is lost.
package store

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"
	bolterrors "go.etcd.io/bbolt/errors"
)

// Bucket names. A fifth bucket, meta, holds the schema version (migrate.go).
var (
	bucketTorrents = []byte("torrents")
	bucketHistory  = []byte("history")
	bucketPrefs    = []byte("prefs")
)

// prefsKey is the single key the prefs bucket holds; there is exactly one
// Prefs value for the whole application, not one per screen.
var prefsKey = []byte("prefs")

// defaultFlushInterval is how often the background goroutine persists
// dirty in-memory state to disk (AGENT.md §13: "bbolt writes block ...
// persist on a debounce, not per update").
const defaultFlushInterval = 5 * time.Second

// maxHistoryEntries caps how many recent queries are kept. Older entries
// are dropped as new ones are added.
const maxHistoryEntries = 50

// ErrEmptyID is returned by SetTorrent when the record's ID is empty —
// there is nothing to key the bucket on.
var ErrEmptyID = errors.New("store: torrent record id must not be empty")

// ErrClosed is returned by any call made on a Store after Close.
var ErrClosed = errors.New("store: store is closed")

// historyRecord pairs a HistoryEntry with the monotonically increasing
// sequence number it is keyed by on disk, so re-flushing preserves order
// without depending on wall-clock time (which is not guaranteed strictly
// increasing across entries added in the same instant).
type historyRecord struct {
	seq   uint64
	entry HistoryEntry
}

// Store is a bbolt-backed persistence handle. The zero value is not
// usable; construct one with Open.
type Store struct {
	mu sync.Mutex

	db     *bolt.DB
	logger *slog.Logger

	torrents   map[string]TorrentRecord
	history    []historyRecord
	historySeq uint64
	prefs      Prefs
	// destinations is the used-destinations list, most recent first
	// (destinations.go).
	destinations []string

	dirty  bool
	closed bool

	flushInterval time.Duration
	stopFlush     chan struct{}
	flushDone     chan struct{}
}

// Open opens (creating if necessary) the bbolt file at path, verifies its
// schema version (migrate.go), loads its contents into memory, and starts
// the debounced background flush goroutine. Callers must call Close.
func Open(path string) (*Store, error) {
	return open(path, defaultFlushInterval)
}

// open is the shared constructor; tests use a short flushInterval so they
// do not have to wait on the production five-second debounce.
func open(path string, flushInterval time.Duration) (*Store, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}

	s := &Store{
		db:            db,
		logger:        slog.Default(),
		torrents:      make(map[string]TorrentRecord),
		flushInterval: flushInterval,
		stopFlush:     make(chan struct{}),
		flushDone:     make(chan struct{}),
	}

	if err := db.Update(ensureSchema); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.load(); err != nil {
		_ = db.Close()
		return nil, err
	}

	go s.flushLoop()
	return s, nil
}

// load populates the in-memory cache from the on-disk buckets. Called once
// from Open, before the flush goroutine starts, so it needs no locking.
func (s *Store) load() error {
	return s.db.View(func(tx *bolt.Tx) error {
		tb := tx.Bucket(bucketTorrents)
		if err := tb.ForEach(func(k, v []byte) error {
			var rec TorrentRecord
			if err := json.Unmarshal(v, &rec); err != nil {
				return fmt.Errorf("store: decode torrent %q: %w", k, err)
			}
			s.torrents[string(k)] = rec
			return nil
		}); err != nil {
			return err
		}

		hb := tx.Bucket(bucketHistory)
		if err := hb.ForEach(func(k, v []byte) error {
			if len(k) != 8 {
				return fmt.Errorf("store: history key %q is not an 8-byte sequence number", k)
			}
			seq := binary.BigEndian.Uint64(k)
			var entry HistoryEntry
			if err := json.Unmarshal(v, &entry); err != nil {
				return fmt.Errorf("store: decode history entry %d: %w", seq, err)
			}
			s.history = append(s.history, historyRecord{seq: seq, entry: entry})
			if seq > s.historySeq {
				s.historySeq = seq
			}
			return nil
		}); err != nil {
			return err
		}

		pb := tx.Bucket(bucketPrefs)
		if raw := pb.Get(prefsKey); raw != nil {
			if err := json.Unmarshal(raw, &s.prefs); err != nil {
				return fmt.Errorf("store: decode prefs: %w", err)
			}
		}

		dests, err := loadDestinations(pb)
		if err != nil {
			return err
		}
		s.destinations = dests
		return nil
	})
}

// flushLoop runs on its own goroutine for the life of the Store, flushing
// dirty state to disk on a fixed interval until Close signals stopFlush.
func (s *Store) flushLoop() {
	defer close(s.flushDone)

	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := s.flush(); err != nil {
				s.logger.Error("store: periodic flush failed", "error", err)
			}
		case <-s.stopFlush:
			return
		}
	}
}

// Flush persists pending in-memory changes to disk immediately, without
// waiting for the debounce interval. It is safe to call at any time before
// Close; most callers do not need to, since Close flushes on its own.
func (s *Store) Flush() error {
	return s.flush()
}

// flush snapshots the in-memory state under the lock, then writes it to
// bbolt outside the lock so a slow disk never blocks readers or writers of
// the in-memory cache.
func (s *Store) flush() error {
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return nil
	}

	torrents := make(map[string]TorrentRecord, len(s.torrents))
	for id, rec := range s.torrents {
		torrents[id] = rec
	}
	history := make([]historyRecord, len(s.history))
	copy(history, s.history)
	prefs := s.prefs
	destinations := append([]string(nil), s.destinations...)
	s.mu.Unlock()

	err := s.db.Update(func(tx *bolt.Tx) error {
		if err := writeTorrents(tx, torrents); err != nil {
			return err
		}
		if err := writeHistory(tx, history); err != nil {
			return err
		}
		if err := writePrefs(tx, prefs); err != nil {
			return err
		}
		return writeDestinations(tx, destinations)
	})
	if err != nil {
		// The write failed; leave dirty set so the next tick (or an
		// explicit Flush/Close) retries rather than silently losing it.
		s.mu.Lock()
		s.dirty = true
		s.mu.Unlock()
		return fmt.Errorf("store: flush: %w", err)
	}
	return nil
}

// resetBucket clears name to an empty bucket, creating it if absent. Used
// by flush's writers so a full in-memory snapshot can be written back
// without leaving stale keys (e.g. a deleted torrent) behind.
func resetBucket(tx *bolt.Tx, name []byte) (*bolt.Bucket, error) {
	if err := tx.DeleteBucket(name); err != nil && !errors.Is(err, bolterrors.ErrBucketNotFound) {
		return nil, fmt.Errorf("store: reset bucket %q: %w", name, err)
	}
	b, err := tx.CreateBucket(name)
	if err != nil {
		return nil, fmt.Errorf("store: recreate bucket %q: %w", name, err)
	}
	return b, nil
}

// Close stops the background flush goroutine, performs one last
// synchronous flush, and closes the underlying bbolt file. It is safe to
// call more than once.
func (s *Store) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.mu.Unlock()

	close(s.stopFlush)
	<-s.flushDone

	flushErr := s.flush()
	closeErr := s.db.Close()
	if flushErr != nil {
		return flushErr
	}
	if closeErr != nil {
		return fmt.Errorf("store: close: %w", closeErr)
	}
	return nil
}
