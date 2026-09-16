package scraper

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/kdta91/tortui/internal/config"
)

// definitionExt is the only file extension the loader reads. It is matched
// case-insensitively on every platform, deliberately: macOS's default APFS
// is case-insensitive, so a file called ARCHIVE.YML that loads there would
// otherwise be invisible on Linux (AGENT.md §13).
const definitionExt = ".yml"

// maxDefinitionBytes bounds one definition file. A definition is a page of
// selectors; a megabyte of them is already three orders of magnitude more
// than any real one, and the bound is what stops a stray multi-gigabyte
// file in the definitions directory from being read into memory at
// startup. A file over the bound is skipped like any other bad definition.
const maxDefinitionBytes = 1 << 20

var (
	// ErrDefinitionTooLarge reports a definition file bigger than
	// maxDefinitionBytes. The file is not parsed.
	ErrDefinitionTooLarge = errors.New("the definition file is larger than the limit")

	// ErrDefinitionIDDuplicated reports a definition whose id another
	// file in the same directory already declared. The first file in
	// filename order keeps the id and this one is skipped, so which of
	// the two wins does not depend on the order the filesystem happens
	// to list them in.
	ErrDefinitionIDDuplicated = errors.New("another definition file already declares this id")
)

// Skipped is one definition file a load could not use, and why. It names
// the file's base name rather than its path: the path runs through the
// user's home directory and the base name is what they need to go and fix.
//
// Err is safe to show and safe to log. Every error this package produces
// is free of definition content and of response text by construction
// (DEC-073), and the loader adds only a file name to that.
type Skipped struct {
	// File is the base name of the file that was skipped.
	File string

	// Err is why, wrapping one of this package's sentinels.
	Err error
}

// Snapshot is one immutable set of definitions, as read from disk by a
// single Reload. Nothing ever mutates a Snapshot after Reload publishes
// it, which is what makes the swap atomic: a reader holding one keeps a
// consistent view of the whole set for as long as it wants, and a
// concurrent Reload builds an entirely new Snapshot rather than editing
// this one.
type Snapshot struct {
	definitions []*Definition
	byID        map[string]*Definition
	skipped     []Skipped
}

// Definitions returns the definitions in the snapshot, ordered by id. The
// slice is a fresh copy, so a caller cannot reach into the snapshot by
// editing it; the *Definition values it holds are shared and must not be
// modified.
func (s *Snapshot) Definitions() []*Definition {
	out := make([]*Definition, len(s.definitions))
	copy(out, s.definitions)

	return out
}

// Definition returns the definition with the given id.
func (s *Snapshot) Definition(id string) (*Definition, bool) {
	def, ok := s.byID[id]

	return def, ok
}

// Skipped returns the files this load could not use, in filename order.
// The slice is a fresh copy.
func (s *Snapshot) Skipped() []Skipped {
	out := make([]Skipped, len(s.skipped))
	copy(out, s.skipped)

	return out
}

// LoaderOptions configures a Loader. Every field has a working default.
type LoaderOptions struct {
	// Dir is the directory the definitions are read from. Empty resolves
	// it through internal/config, which is the only place in this
	// repository that knows where a user's config lives: the definitions
	// folder inside the config directory
	// ($XDG_CONFIG_HOME/tortui/definitions on Linux, the same path under
	// $TORTUI_HOME or the per-OS equivalent otherwise — AGENT.md §14).
	Dir string

	// Logger is where a skipped definition is reported. Nil uses
	// slog.Default(), which internal/logging points at a rotating file:
	// the TUI owns the terminal and nothing here may write to stdout or
	// stderr (AGENT.md §3).
	Logger *slog.Logger
}

// Loader reads source definitions out of the user's definitions directory
// and holds the current set.
//
// It is safe for concurrent use. Readers go through an immutable Snapshot
// published with a single atomic store, so a reader either sees the whole
// previous set or the whole new one and never a half-swapped mixture of
// the two; concurrent Reloads are serialised so the directory is read once
// at a time.
//
// A broken file never blocks startup. Every file is loaded independently
// and a file that will not parse or will not validate is skipped, logged,
// and recorded in Snapshot.Skipped — Reload itself only fails when the
// directory as a whole cannot be read, and even then the previously loaded
// set stays in place.
type Loader struct {
	dir    string
	logger *slog.Logger

	// reloading serialises Reload against itself. Readers never take it.
	reloading sync.Mutex

	// current is the published set. Never nil after NewLoader.
	current atomic.Pointer[Snapshot]
}

// NewLoader builds a loader over the definitions directory. It touches no
// disk: the directory is resolved but not read, and not created. Call
// Reload to read it.
func NewLoader(opts LoaderOptions) (*Loader, error) {
	dir := opts.Dir

	if dir == "" {
		paths, err := config.ResolvePaths("")
		if err != nil {
			return nil, fmt.Errorf("scraper: resolve definitions directory: %w", err)
		}

		dir = paths.DefinitionsDir
	}

	l := &Loader{dir: dir, logger: opts.Logger}
	l.current.Store(emptySnapshot())

	return l, nil
}

// Dir is the directory definitions are read from.
func (l *Loader) Dir() string { return l.dir }

// Reload re-reads every definition from disk and replaces the current set
// with the result in one atomic store. A reader concurrent with it sees
// either the whole old set or the whole new one.
//
// This is the seam the settings screen calls when the user edits a
// definition and asks for it to be picked up: T-023 ships the method, and
// the settings screen that calls it is T-080/T-082, which is not built
// here.
//
// An error means the directory itself could not be listed — a permission
// problem, or a path that exists and is not a directory. A directory that
// does not exist is not an error: a fresh install has no definitions
// folder (the lawful default sources are compiled in, T-024) and starting
// with no user-supplied source is an ordinary state. When Reload returns
// an error, the set it published last time is left exactly as it was.
//
// An individual file that cannot be read, parsed, or validated is never an
// error here. It is logged and recorded in Skipped, and every other file
// in the directory still loads.
func (l *Loader) Reload() error {
	l.reloading.Lock()
	defer l.reloading.Unlock()

	next, err := l.read()
	if err != nil {
		return err
	}

	l.current.Store(next)

	return nil
}

// Snapshot returns the current set. It is a single atomic load, so the
// result is always one whole set.
func (l *Loader) Snapshot() *Snapshot { return l.current.Load() }

// Definitions returns the current definitions, ordered by id.
func (l *Loader) Definitions() []*Definition { return l.Snapshot().Definitions() }

// Definition returns the current definition with the given id.
func (l *Loader) Definition(id string) (*Definition, bool) { return l.Snapshot().Definition(id) }

// Skipped returns the files the last load could not use.
func (l *Loader) Skipped() []Skipped { return l.Snapshot().Skipped() }

// log resolves the logger at call time, so a logger installed after this
// loader was built (internal/logging.New sets slog's default) is still the
// one used. Same arrangement as httpx.Client.log.
func (l *Loader) log() *slog.Logger {
	if l.logger != nil {
		return l.logger
	}

	return slog.Default()
}

// emptySnapshot is the set a loader holds before its first Reload, and
// after a Reload of a directory that does not exist.
func emptySnapshot() *Snapshot {
	return &Snapshot{byID: map[string]*Definition{}}
}

// read builds the next snapshot from the directory. It publishes nothing.
func (l *Loader) read() (*Snapshot, error) {
	// os.ReadDir sorts by filename, which is what makes "the first file
	// wins a duplicated id" a stable rule rather than a coin toss.
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			l.log().Debug("scraper: no definitions directory, no user-supplied sources loaded", "dir", l.dir)

			return emptySnapshot(), nil
		}

		return nil, fmt.Errorf("scraper: read definitions directory: %w", err)
	}

	next := emptySnapshot()

	for _, entry := range entries {
		name := entry.Name()

		if entry.IsDir() || !hasDefinitionExt(name) {
			continue
		}

		def, err := l.readDefinition(filepath.Join(l.dir, name))
		if err == nil && next.byID[def.ID] != nil {
			err = fmt.Errorf("scraper: %w", ErrDefinitionIDDuplicated)
		}

		if err != nil {
			next.skipped = append(next.skipped, Skipped{File: name, Err: err})

			// The file name is the user's own and the error carries no
			// definition content (DEC-073), so both are safe here.
			l.log().Error("scraper: skipping a definition that cannot be used",
				"file", name,
				"error", err.Error(),
			)

			continue
		}

		next.definitions = append(next.definitions, def)
		next.byID[def.ID] = def
	}

	sortByID(next.definitions)

	return next, nil
}

// readDefinition reads and parses one file.
func (l *Loader) readDefinition(path string) (*Definition, error) {
	data, err := readCapped(path)
	if err != nil {
		return nil, err
	}

	// Parse validates too, so a definition that reaches the set is one
	// New can compile.
	return Parse(data)
}

// readCapped reads a file, refusing one over maxDefinitionBytes without
// reading it all: the limited reader stops one byte past the bound, so the
// size that decides is the number of bytes actually read rather than a
// size the directory listing reported earlier.
//
// The path is never repeated into an error — it runs through the user's
// home directory, and the caller names the file instead.
func readCapped(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("scraper: cannot open the definition file: %w", errWithoutPath(err))
	}

	data, readErr := io.ReadAll(io.LimitReader(f, maxDefinitionBytes+1))
	closeErr := f.Close()

	switch {
	case readErr != nil:
		return nil, fmt.Errorf("scraper: cannot read the definition file: %w", errWithoutPath(readErr))
	case closeErr != nil:
		return nil, fmt.Errorf("scraper: cannot close the definition file: %w", errWithoutPath(closeErr))
	case len(data) > maxDefinitionBytes:
		return nil, fmt.Errorf("scraper: %w (%d bytes)", ErrDefinitionTooLarge, maxDefinitionBytes)
	}

	return data, nil
}

// errWithoutPath strips the path out of a *fs.PathError, keeping the
// operation and the underlying reason. The reason ("permission denied")
// is the useful half and the path is the half that names the user's home
// directory.
func errWithoutPath(err error) error {
	var perr *fs.PathError
	if errors.As(err, &perr) {
		return fmt.Errorf("%s: %w", perr.Op, perr.Err)
	}

	return err
}

// hasDefinitionExt reports whether a file name is one the loader reads.
func hasDefinitionExt(name string) bool {
	return strings.EqualFold(filepath.Ext(name), definitionExt)
}

// sortByID orders definitions by id, so the set a caller iterates is in the
// same order on every platform and after every reload regardless of what
// the files are called.
func sortByID(defs []*Definition) {
	slices.SortFunc(defs, func(a, b *Definition) int {
		return strings.Compare(a.ID, b.ID)
	})
}
