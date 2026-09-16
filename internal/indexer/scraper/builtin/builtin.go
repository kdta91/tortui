// Package builtin ships the small set of lawful default source definitions
// tortui bundles into the binary, so a fresh install can search and
// download immediately with nothing installed but tortui itself
// (AGENT.md §1, §2, T-024).
//
// Every definition under definitions/ is an ordinary scraper definition —
// see internal/indexer/scraper for the schema — using no field, selector,
// or endpoint that isn't documented by the source itself. There is no
// special-casing anywhere else in the codebase: a bundled definition is
// loaded, validated, and compiled exactly like a user-supplied one, and the
// registry cannot tell the two apart once Merge has combined them.
//
// The definitions are compiled into the binary with go:embed rather than
// installed as loose files, so there is nothing to unpack and nothing to
// keep next to the executable — see docs/bundled-sources.md for why each
// one qualifies under AGENT.md §2 and how its fields were verified against
// the source's own documentation.
package builtin

import (
	"embed"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"github.com/kdta91/tortui/internal/indexer/scraper"
)

// definitionsFS embeds every *.yml file this package ships. Adding a
// bundled source is: add a file here, add it to docs/bundled-sources.md,
// and add its hostname to docs/indexer-hostname-allowlist.md.
//
//go:embed definitions/*.yml
var definitionsFS embed.FS

// definitionsDir is the embedded directory Definitions reads.
const definitionsDir = "definitions"

// Definitions parses and validates every bundled definition, ordered by
// id. It touches no disk and no network — the definitions are compiled
// into the binary — and it never returns a partial result: unlike
// scraper.Loader, which must keep running when a user's own definitions
// directory has a bad file in it, a bundled definition that fails to parse
// or validate is a bug in this repository, not something a caller can work
// around, so it is reported rather than silently dropped.
func Definitions() ([]*scraper.Definition, error) {
	entries, err := fs.ReadDir(definitionsFS, definitionsDir)
	if err != nil {
		// Unreachable outside a broken build: the directory is compiled
		// into the binary by the go:embed directive above.
		return nil, fmt.Errorf("builtin: read embedded definitions: %w", err)
	}

	out := make([]*scraper.Definition, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".yml") {
			continue
		}

		data, err := fs.ReadFile(definitionsFS, definitionsDir+"/"+entry.Name())
		if err != nil {
			return nil, fmt.Errorf("builtin: read %s: %w", entry.Name(), err)
		}

		def, err := scraper.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("builtin: %s: %w", entry.Name(), err)
		}

		out = append(out, def)
	}

	slices.SortFunc(out, func(a, b *scraper.Definition) int {
		return strings.Compare(a.ID, b.ID)
	})

	return out, nil
}

// Merge combines the bundled definitions with a set of user-supplied ones,
// returning the set the registry should build sources from.
//
// A user-supplied definition wins over a bundled one of the same id — this
// is how a broken bundled selector gets repaired without waiting for a
// release, and how a user replaces a default they don't want with their
// own version of it under the same name (see docs/bundled-sources.md). Any
// id unique to either side is kept as it is. The result is ordered by id,
// same as Definitions and scraper.Snapshot.Definitions, so iteration order
// never depends on which side of the merge a definition came from.
//
// Merge does not itself decide whether a bundled source is enabled; that
// is a first-run/config concern for the composition root that does not
// exist yet (T-090+, following the same pattern T-002/T-003 used to ship a
// package's full behaviour ahead of the task that wires it in).
func Merge(user []*scraper.Definition) ([]*scraper.Definition, error) {
	bundled, err := Definitions()
	if err != nil {
		return nil, err
	}

	return merge(bundled, user), nil
}

// merge is Merge's pure half, split out so a test can exercise the
// override rule against fixture definitions without going through the
// embedded filesystem.
func merge(bundled, user []*scraper.Definition) []*scraper.Definition {
	byID := make(map[string]*scraper.Definition, len(bundled)+len(user))

	for _, def := range bundled {
		byID[def.ID] = def
	}

	for _, def := range user {
		byID[def.ID] = def
	}

	out := make([]*scraper.Definition, 0, len(byID))
	for _, def := range byID {
		out = append(out, def)
	}

	slices.SortFunc(out, func(a, b *scraper.Definition) int {
		return strings.Compare(a.ID, b.ID)
	})

	return out
}
