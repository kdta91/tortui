package store

import (
	"encoding/binary"
	"errors"
	"fmt"

	bolt "go.etcd.io/bbolt"
)

// bucketMeta holds bookkeeping keys that are not user data, currently just
// the schema version.
var bucketMeta = []byte("meta")

// schemaVersionKey is the meta-bucket key holding the current schema
// version as a big-endian uint64.
var schemaVersionKey = []byte("schema_version")

// currentSchemaVersion is the schema version this build of tortui writes
// and understands. Bump it and append a step to migrations when the
// on-disk layout changes in a way older code cannot read.
const currentSchemaVersion uint64 = 1

// migrations holds one upgrade step per schema version, in order:
// migrations[i] upgrades a database from version i+1 to version i+2. It is
// empty today because currentSchemaVersion is still 1 — this is the hook
// the next schema change appends to, not dead code. ensureSchema returns
// an error if it ever needs a step that is not registered here.
var migrations []func(tx *bolt.Tx) error

// ErrUnsupportedSchemaVersion is returned by Open when the on-disk schema
// version is newer than this build supports. Opening refuses outright
// rather than guessing at a layout it does not understand and silently
// corrupting it.
var ErrUnsupportedSchemaVersion = errors.New("store: on-disk schema version is newer than this build of tortui supports")

// ensureSchema runs inside the first write transaction after Open. It
// creates every bucket the store needs (so later code can assume they
// exist), then checks the schema version: a missing version key means a
// brand-new database and is stamped with currentSchemaVersion directly; an
// older version is migrated forward step by step; a newer version refuses
// to open.
func ensureSchema(tx *bolt.Tx) error {
	for _, name := range [][]byte{bucketTorrents, bucketHistory, bucketPrefs} {
		if _, err := tx.CreateBucketIfNotExists(name); err != nil {
			return fmt.Errorf("store: create bucket %q: %w", name, err)
		}
	}

	meta, err := tx.CreateBucketIfNotExists(bucketMeta)
	if err != nil {
		return fmt.Errorf("store: create meta bucket: %w", err)
	}

	raw := meta.Get(schemaVersionKey)
	if raw == nil {
		// Fresh database: nothing to migrate, just stamp the version.
		return putSchemaVersion(meta, currentSchemaVersion)
	}
	if len(raw) != 8 {
		return fmt.Errorf("store: schema version value is %d bytes, want 8", len(raw))
	}
	version := binary.BigEndian.Uint64(raw)

	if version > currentSchemaVersion {
		return fmt.Errorf("%w: on-disk version %d, this build supports %d",
			ErrUnsupportedSchemaVersion, version, currentSchemaVersion)
	}

	for version < currentSchemaVersion {
		idx := int(version) - 1
		if idx < 0 || idx >= len(migrations) {
			return fmt.Errorf("store: no migration registered from schema version %d to %d", version, version+1)
		}
		if err := migrations[idx](tx); err != nil {
			return fmt.Errorf("store: migrate schema version %d to %d: %w", version, version+1, err)
		}
		version++
	}

	return putSchemaVersion(meta, version)
}

func putSchemaVersion(b *bolt.Bucket, version uint64) error {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, version)
	if err := b.Put(schemaVersionKey, buf); err != nil {
		return fmt.Errorf("store: write schema version: %w", err)
	}
	return nil
}
