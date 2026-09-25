package engine

import (
	"context"
	"errors"
)

// ErrDataMissing reports a torrent restored from an earlier session whose
// data had been recorded as downloaded but is no longer on disk — the files
// were moved or deleted while tortui was not running. A Resumer surfaces such
// a torrent as StateErrored with an Err wrapping this sentinel, rather than
// silently dropping it or quietly downloading everything again, so the user
// can choose: remove the entry, or put the files back and restart.
var ErrDataMissing = errors.New("downloaded data is missing on disk")

// ResumeData is everything an engine needs to put one torrent back after a
// restart. The session store persists it; Resumer.Restore consumes it.
//
// At least one of Metainfo, Magnet, and TorrentURL is set for a torrent that
// can be resumed. Metainfo wins when present: it carries the info dictionary,
// so the torrent resumes from its data on disk without fetching anything.
type ResumeData struct {
	// ID is the torrent's ID in the session that saved this. Restore
	// reuses it when it is free, so a caller keying records by ID keeps
	// the same key across restarts.
	ID string

	// Name is the torrent's display name when it was saved, shown for a
	// torrent that cannot be restored (e.g. its data is missing).
	Name string

	// Magnet is the magnet URI the torrent was added from, if any.
	Magnet string

	// TorrentURL is the .torrent URL the torrent was added from, if any.
	TorrentURL string

	// Metainfo is the bencoded .torrent, once the info dictionary is
	// known. Like every torrent's metadata it is attacker-controlled: a
	// Resumer re-validates every path it declares before any file is
	// touched (AGENT.md §6.11).
	Metainfo []byte

	// SavePath is the absolute destination the torrent's data lives in. A
	// Resumer re-checks that it still sits inside a known destination root
	// (AGENT.md §6.12).
	SavePath string

	// Origin is where the torrent was originally added from.
	Origin Origin
}

// Resumer is implemented by an Engine that can save and restore torrents
// across restarts. Like Queuer it is a separate, optional interface rather
// than a change to the frozen Engine contract (AGENT.md §5): a caller
// discovers it with a type assertion.
type Resumer interface {
	// ResumeData reports what Restore would need to put id back after a
	// restart. It returns an error identifying an unknown id.
	ResumeData(id string) (ResumeData, error)

	// Restore starts tracking a torrent saved by ResumeData in an earlier
	// session, resuming from whatever data is already on disk, and returns
	// its ID — d.ID when that is free. Like Add it returns promptly.
	//
	// A torrent that cannot be resumed — its data is missing
	// (ErrDataMissing), its destination is no longer a known root, its
	// metadata is unreadable or unsafe — is still tracked, in
	// StateErrored with the reason as Err, so it is never silently
	// dropped and the user can remove it. Restore returns an error only
	// when it tracked nothing: a closed engine or a cancelled ctx.
	Restore(ctx context.Context, d ResumeData) (string, error)
}
