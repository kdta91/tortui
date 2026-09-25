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

	// completion is the piece-completion record inner writes to. It is
	// persistent (a database file in dest), which is what lets a torrent
	// restored after a restart resume from the pieces it already has
	// instead of downloading them again (T-041), and what Restore reads to
	// tell "never downloaded anything" from "downloaded data now missing".
	completion storage.PieceCompletion
}

// newSafeStorage builds a file-backed storage rooted at dest, wrapped in the
// path check.
//
// The backend is given an explicit logger rather than being left to resolve
// slog's default: the default handler writes to stderr, and a torrent whose
// files cannot be statted makes the backend log about it (AGENT.md §13 — the
// TUI owns the terminal).
//
// Piece completion is opened explicitly as the library's persistent default
// for dest. Left to itself, the library picks an in-memory record whenever
// part files are in use (its default), so every restart would forget every
// verified piece. Where no persistent record can be opened the in-memory one
// is the fallback, logged: the torrent still works, it just re-downloads
// after a restart.
func newSafeStorage(dest string, logger *slog.Logger) safeStorage {
	completion, err := storage.NewDefaultPieceCompletionForDir(dest)
	if err != nil {
		logger.Warn("anacrolix: no persistent piece-completion record; progress will not survive a restart",
			"destination", dest, "error", err)

		completion = storage.NewMapPieceCompletion()
	}

	return safeStorage{
		dest: dest,
		inner: storage.NewFileOpts(storage.NewFileClientOpts{
			ClientBaseDir:   dest,
			Logger:          logger,
			PieceCompletion: completion,
		}),
		completion: completion,
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

// Close implements storage.ClientImplCloser. It also closes the
// piece-completion record, which the file backend owns once handed it.
func (s safeStorage) Close() error { return s.inner.Close() }
