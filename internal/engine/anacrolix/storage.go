package anacrolix

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

// safeStorage is the hard gate between an attacker-controlled info dictionary
// and the filesystem.
//
// The torrent library opens a torrent's storage as soon as its info dictionary
// is known — which, for a magnet, is long after Add returned — and that open
// is what stats, creates and later writes files. Wrapping the backend means
// every declared path is cleaned and confirmed to resolve inside the
// destination *before* a single filesystem operation happens, rather than
// after, which is what AGENT.md §6.11 actually asks for.
type safeStorage struct {
	dest  string
	inner storage.ClientImplCloser
}

// newSafeStorage builds a file-backed storage rooted at dest, wrapped in the
// path check.
//
// The backend is given an explicit logger rather than being left to resolve
// slog's default: the default handler writes to stderr, and a torrent whose
// files cannot be statted makes the backend log about it (AGENT.md §13 — the
// TUI owns the terminal).
func newSafeStorage(dest string, logger *slog.Logger) storage.ClientImplCloser {
	return safeStorage{
		dest: dest,
		inner: storage.NewFileOpts(storage.NewFileClientOpts{
			ClientBaseDir: dest,
			Logger:        logger,
		}),
	}
}

// OpenTorrent implements storage.ClientImpl.
func (s safeStorage) OpenTorrent(
	ctx context.Context,
	info *metainfo.Info,
	infoHash metainfo.Hash,
) (storage.TorrentImpl, error) {
	if err := validateInfoPaths(info, s.dest); err != nil {
		return storage.TorrentImpl{}, fmt.Errorf(
			"anacrolix: refusing torrent %s: %w", infoHash.HexString(), err)
	}

	return s.inner.OpenTorrent(ctx, info, infoHash)
}

// Close implements storage.ClientImplCloser.
func (s safeStorage) Close() error { return s.inner.Close() }
