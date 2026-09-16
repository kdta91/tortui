package scraper

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/indexer/httpx"
)

// importMaxBytes bounds a definition Import reads or fetches, whether from
// disk or from a URL. It matches maxDefinitionBytes (T-023) so an imported
// definition is never held to a looser size rule than one a user places
// into the directory by hand.
const importMaxBytes = maxDefinitionBytes

// importFetchTimeout bounds fetching a definition from a URL end to end,
// retries and redirects included. A definition is one small YAML document;
// there is no reason importing one should be allowed to run any longer than
// an ordinary search request.
const importFetchTimeout = 30 * time.Second

// idFilenamePattern is what a definition id must look like before Import
// will use it to build a file name.
//
// Definition.Validate only requires an id to be non-empty (definition.go,
// plan.go) — nothing about its charset. That is fine for an id read out of
// a file the user placed by hand, but Import's id can come from a URL the
// user pointed tortui at or from a file downloaded from one, which makes it
// exactly the remote-derived value AGENT.md §6.11 has in mind, and it is
// about to become part of a filesystem path. The pattern excludes any path
// separator and requires the first character to be alphanumeric, which
// alone rules out "." and ".." as complete file-name stems; there is
// nothing in the allowed charset that can walk the resulting path outside
// Dir once it is joined with a fixed ".yml" suffix. A definition that fails
// this check is rejected with ErrImportIDUnsafe and nothing is written —
// the id itself is free to stay whatever it is inside the file, this only
// governs what Import will use as a file name.
var idFilenamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

var (
	// ErrImportSourceEmpty reports Import called with no path or URL.
	ErrImportSourceEmpty = errors.New("no file path or URL was given to import")

	// ErrImportSourceInvalid reports an import source that looks like a
	// URL but will not parse as one.
	ErrImportSourceInvalid = errors.New("import source is not a usable file path or URL")

	// ErrImportSchemeUnsupported reports an import source that looks like
	// a URL but is neither http nor https.
	ErrImportSchemeUnsupported = errors.New("import URL scheme must be http or https")

	// ErrImportIDUnsafe reports a definition id that Import refuses to
	// turn into a file name: empty after trimming, containing a path
	// separator, or otherwise outside idFilenamePattern.
	ErrImportIDUnsafe = errors.New("definition id cannot be used as a file name")

	// ErrImportDestinationExists reports an id that already names a file
	// in the definitions directory. Import never overwrites an existing
	// definition; the user removes or renames the existing one first.
	ErrImportDestinationExists = errors.New("a definition with this id is already installed")
)

// ImporterOptions configures an Importer. Every field has a working
// default, the same arrangement as LoaderOptions.
type ImporterOptions struct {
	// Dir is the definitions directory a successful Import writes into.
	// Empty resolves it the same way NewLoader does: through
	// internal/config, so Import and the Loader that will pick the result
	// up always agree on where it lives.
	Dir string

	// HTTPClient fetches a URL source. Nil builds a default client with
	// no configured credentials and Import's own size and time bounds.
	// A definition file belongs to no one's account, so Import never
	// attaches a credential to this request regardless of what the
	// user's config holds for any indexer.
	HTTPClient *httpx.Client
}

// Importer installs a definition the user points tortui at, from a local
// file path or an http(s) URL, into a definitions directory a Loader reads.
//
// It fetches or reads **only** the one address it is given: no directory
// listing, no discovery request, no following a link found on a fetched
// page, and no repository or index of available definitions anywhere
// (AGENT.md §2). A URL is requested once, through httpx.Client's ordinary
// safety rules — a bounded response size, a bounded time, http or https
// only, no cross-host or downgrading redirect (T-020/T-021) — and with no
// credential attached.
//
// Import validates before it writes anything: the fetched or read bytes go
// through the same Parse this package uses for a file already on disk, so
// a definition that fails validation is rejected with the failing field
// (and selector, when the failure is about one) named, and Import writes
// nothing (T-025's own acceptance). Only a definition Parse accepts is
// ever written, and it is written once, atomically, under a name derived
// from its own id.
type Importer struct {
	dir    string
	client *httpx.Client
}

// NewImporter builds an importer over a definitions directory. It touches
// no disk itself: the directory is resolved but neither read nor created.
func NewImporter(opts ImporterOptions) (*Importer, error) {
	dir := opts.Dir

	if dir == "" {
		paths, err := config.ResolvePaths("")
		if err != nil {
			return nil, fmt.Errorf("scraper: resolve definitions directory: %w", err)
		}

		dir = paths.DefinitionsDir
	}

	client := opts.HTTPClient
	if client == nil {
		client = httpx.New(httpx.Config{
			RequestTimeout: importFetchTimeout,
			MaxBodyBytes:   importMaxBytes,
		})
	}

	return &Importer{dir: dir, client: client}, nil
}

// Dir is the directory a successful Import writes into.
func (im *Importer) Dir() string { return im.dir }

// Import validates and installs a definition from source, which is either
// an absolute or relative local file path or an http(s) URL.
//
// It returns the parsed definition and the path it was written to. Any
// error — an empty source, an unreachable URL, a file Parse rejects, an id
// that cannot be turned into a safe file name, or an id already installed
// — leaves the definitions directory exactly as it was; Import never
// partially writes a file.
func (im *Importer) Import(ctx context.Context, source string) (*Definition, string, error) {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return nil, "", fmt.Errorf("scraper: %w", ErrImportSourceEmpty)
	}

	data, err := im.fetch(ctx, trimmed)
	if err != nil {
		return nil, "", err
	}

	def, err := Parse(data)
	if err != nil {
		return nil, "", err
	}

	name, err := filenameForID(def.ID)
	if err != nil {
		return nil, "", err
	}

	if err := checkNoConflict(im.dir, name); err != nil {
		return nil, "", err
	}

	path, err := writeDefinitionAtomically(im.dir, name, data)
	if err != nil {
		return nil, "", err
	}

	return def, path, nil
}

// fetch reads source's bytes, from disk or over the network, capped at
// importMaxBytes either way.
func (im *Importer) fetch(ctx context.Context, source string) ([]byte, error) {
	if !looksLikeURL(source) {
		return readCapped(source)
	}

	parsed, err := url.Parse(source)
	if err != nil {
		return nil, fmt.Errorf("scraper: %w: %w", ErrImportSourceInvalid, err)
	}

	switch parsed.Scheme {
	case "http", "https":
	default:
		return nil, fmt.Errorf("scraper: %w", ErrImportSchemeUnsupported)
	}

	res, err := im.client.Get(ctx, source, nil)
	if err != nil {
		return nil, fmt.Errorf("scraper: fetch definition: %w", err)
	}

	return res.Body, nil
}

// looksLikeURL reports whether source has the "scheme://" shape of a URL
// rather than a local file path. A Windows path such as "C:\defs\a.yml" has
// no "://" and is never mistaken for one.
func looksLikeURL(source string) bool {
	return strings.Contains(source, "://")
}

// filenameForID turns a definition id into the file name Import writes it
// under, refusing one that is not safe to use as-is.
func filenameForID(id string) (string, error) {
	trimmed := strings.TrimSpace(id)
	if !idFilenamePattern.MatchString(trimmed) {
		return "", fmt.Errorf("scraper: %w", ErrImportIDUnsafe)
	}

	return trimmed + definitionExt, nil
}

// checkNoConflict reports ErrImportDestinationExists when dir already has
// an entry with this name, compared without regard to case. The
// case-insensitive comparison matters on APFS (AGENT.md §13): a directory
// that already holds "Example.yml" would silently take a second write
// named "example.yml" as the very same file on macOS while creating a
// genuinely separate one on Linux, so Import treats the two as a conflict
// on every platform rather than letting the result depend on the
// filesystem underneath it.
func checkNoConflict(dir, name string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return fmt.Errorf("scraper: list definitions directory: %w", errWithoutPath(err))
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(entry.Name(), name) {
			return fmt.Errorf("scraper: %w", ErrImportDestinationExists)
		}
	}

	return nil
}

// writeDefinitionAtomically writes data under dir/name, creating dir if
// this is the first definition ever installed (T-023's loader tolerates a
// missing directory as an ordinary "no user-supplied source yet" state, so
// nothing has created it before now). The write goes through a temporary
// file in the same directory followed by a rename, so a reader — including
// a concurrent Loader.Reload — never observes a partially written file.
func writeDefinitionAtomically(dir, name string, data []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("scraper: create definitions directory: %w", errWithoutPath(err))
	}

	tmp, err := os.CreateTemp(dir, ".tortui-import-*.tmp")
	if err != nil {
		return "", fmt.Errorf("scraper: create temporary file: %w", errWithoutPath(err))
	}

	tmpPath := tmp.Name()

	defer func() {
		// A no-op once the rename below has succeeded — os.Remove on a
		// path that no longer exists is silently ignored the same way
		// readCapped's callers already tolerate a missing file.
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()

		return "", fmt.Errorf("scraper: write definition file: %w", errWithoutPath(err))
	}

	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()

		return "", fmt.Errorf("scraper: set definition file permissions: %w", errWithoutPath(err))
	}

	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("scraper: close definition file: %w", errWithoutPath(err))
	}

	finalPath := filepath.Join(dir, name)

	if err := os.Rename(tmpPath, finalPath); err != nil {
		return "", fmt.Errorf("scraper: install definition file: %w", errWithoutPath(err))
	}

	return finalPath, nil
}
