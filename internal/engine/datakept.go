package engine

import "errors"

// ErrDataKept reports a remove with data that removed the torrent but kept
// its data, because another download keeps its own data under the same name
// at the same destination (T-9133). An Engine returns it wrapped in a
// *DataKeptError naming that data.
var ErrDataKept = errors.New("data kept: another download uses it")

// DataKeptError is what Remove returns, after removing the torrent, when it
// was asked to delete the torrent's data but kept it: another download keeps
// its data at Path, so deleting it would delete theirs. The removal itself
// succeeded; only the delete was skipped.
//
// Its text leads with what happened and the path, so both fit an 80-column
// status bar.
type DataKeptError struct {
	// Path is the data that was kept: the destination joined with the
	// name both downloads use.
	Path string

	// Maybe is set when no tracked download is known to keep its data at
	// Path, but one might: a download at the same destination has not
	// named its data yet, or the data was another download's that has
	// since been removed with its data kept (T-9135, DEC-167).
	Maybe bool
}

// Error implements error.
func (e *DataKeptError) Error() string {
	if e.Maybe {
		return "data kept: another download may use " + e.Path
	}

	return "data kept: another download uses " + e.Path
}

// Unwrap makes errors.Is(err, ErrDataKept) hold.
func (e *DataKeptError) Unwrap() error { return ErrDataKept }
