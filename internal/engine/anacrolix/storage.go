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
	inner *fileStore

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
// The backend is tortui's own fileStore rather than the library's file
// storage, whose data files stay memory-mapped until the process exits — on
// Windows that blocks rewriting or deleting a download (T-097). It is given
// an explicit logger: slog's default handler writes to stderr, and the TUI
// owns the terminal (AGENT.md §13).
//
// Piece completion is the library's persistent default for dest (a hidden
// ".torrent.db" or ".torrent.bolt.db" file there), and there are no part
// files: data is written straight to its final name and the record says
// which pieces of it are good, so a restart resumes an unfinished file from
// its verified pieces (T-041). Where no persistent record can be opened the
// in-memory one is the fallback, logged: the torrent still works, it just
// re-downloads after a restart.
//
// group is the engine's store group, which every destination's backend
// shares so a discard sees every open torrent (T-9131); nil gives the
// backend a group of its own.
func newSafeStorage(dest string, logger *slog.Logger, group *storeGroup) safeStorage {
	completion, err := storage.NewDefaultPieceCompletionForDir(dest)
	if err != nil {
		logger.Warn("anacrolix: no persistent piece-completion record; progress will not survive a restart",
			"destination", dest, "error", err)

		completion = storage.NewMapPieceCompletion()
	}

	inner := newFileStore(dest, completion, logger)
	if group != nil {
		inner.group = group
	}

	return safeStorage{
		dest:       dest,
		inner:      inner,
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

// Close implements storage.ClientImplCloser: it releases every data file the
// backend still holds open and closes the piece-completion record, which the
// backend owns once handed it.
func (s safeStorage) Close() error { return s.inner.Close() }
