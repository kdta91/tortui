package engine

import "errors"

// ErrLeftData reports an add refused because an earlier, failed add of the
// same torrent left data in another folder (T-9127, DEC-163). An Engine
// returns it wrapped in a *LeftDataError naming that folder.
var ErrLeftData = errors.New("left data in another folder")

// LeftDataError refuses an add of a torrent to one destination while an
// errored entry for the same torrent still has data at Path, somewhere else.
// Starting over would leave that data with nothing tracking it, so the user
// removes the errored row first, keeping or deleting its data.
//
// Its text leads with the remedy and the path, so both fit an 80-column
// status bar (the TUI shows it without its usual add-failure prefix).
type LeftDataError struct {
	// Path is the earlier entry's data: its destination joined with the
	// torrent's name.
	Path string
}

// Error implements error.
func (e *LeftDataError) Error() string {
	return "remove its errored row (x) first: left data in " + e.Path
}

// Unwrap makes errors.Is(err, ErrLeftData) hold.
func (e *LeftDataError) Unwrap() error { return ErrLeftData }
