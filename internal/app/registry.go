package app

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/indexer"
	"github.com/kdta91/tortui/internal/indexer/httpx"
	"github.com/kdta91/tortui/internal/indexer/scraper"
	"github.com/kdta91/tortui/internal/indexer/scraper/builtin"
	"github.com/kdta91/tortui/internal/indexer/torznab"
)

// maxUserDefinitionBytes bounds a user scraper definition file read for an
// [[indexer]] entry, matching the Loader's own cap.
const maxUserDefinitionBytes = 1 << 20

// errDefinitionEscapes reports an [[indexer]] definition that is not a plain
// path inside the definitions directory.
var errDefinitionEscapes = errors.New("definition must be a file inside the definitions directory")

// buildRegistry registers the sources a run searches:
//
//   - every bundled lawful source (T-024), enabled with no setup (AGENT.md
//     §1), using the user's own definition of the same id from the
//     definitions directory when there is one (builtin.Merge's override);
//   - every enabled [[indexer]] entry from config.toml. An entry whose id
//     matches a bundled source replaces it, so `enabled = false` under that
//     id turns a default off.
//
// A source that cannot be built is logged and skipped, never fatal: one bad
// entry must not stop the rest from searching (AGENT.md §6.3). Nothing here
// touches the network.
func buildRegistry(cfg config.Config, defsDir string, logger *slog.Logger, transport http.RoundTripper) *indexer.Registry {
	reg := indexer.NewRegistry(indexer.Config{Timeout: searchTimeout(cfg.SearchTimeout, logger)})

	configured := make(map[string]bool, len(cfg.Indexers))
	for _, ix := range cfg.Indexers {
		configured[ix.ID] = true
	}

	for _, def := range bundledDefinitions(defsDir, logger) {
		if configured[def.ID] {
			continue
		}

		a, err := scraper.New(scraper.Options{Definition: def, Client: newClient(httpx.Credentials{}, transport)})
		if err == nil {
			err = reg.Register(a)
		}

		if err != nil {
			logger.Warn("app: skipping bundled source", "source", def.ID, "error", err)
		}
	}

	for _, ix := range cfg.Indexers {
		if !ix.Enabled {
			continue
		}

		a, err := userSource(ix, defsDir, transport)
		if err == nil {
			err = reg.Register(a)
		}

		if err != nil {
			logger.Warn("app: skipping configured source", "source", ix.ID, "type", ix.Type, "error", err)
		}
	}

	return reg
}

// bundledDefinitions returns the bundled definitions, each replaced by the
// user's definition of the same id when the definitions directory has one.
func bundledDefinitions(defsDir string, logger *slog.Logger) []*scraper.Definition {
	bundled, err := builtin.Definitions()
	if err != nil {
		logger.Error("app: load bundled definitions", "error", err)
		return nil
	}

	var user []*scraper.Definition

	loader, err := scraper.NewLoader(scraper.LoaderOptions{Dir: defsDir, Logger: logger})
	if err == nil {
		err = loader.Reload()
		user = loader.Definitions()
	}

	if err != nil {
		logger.Warn("app: read user definitions", "dir", defsDir, "error", err)
	}

	merged, err := builtin.Merge(user)
	if err != nil {
		logger.Warn("app: merge user definitions over bundled ones", "error", err)
		return bundled
	}

	byID := make(map[string]*scraper.Definition, len(merged))
	for _, d := range merged {
		byID[d.ID] = d
	}

	out := make([]*scraper.Definition, 0, len(bundled))
	for _, b := range bundled {
		out = append(out, byID[b.ID])
	}

	return out
}

// userSource builds the adapter for one enabled [[indexer]] entry, with the
// credentials the user supplied from their own account (AGENT.md §2).
func userSource(ix config.Indexer, defsDir string, transport http.RoundTripper) (indexer.Indexer, error) {
	creds := httpx.Credentials{APIKey: ix.APIKey, CookieHeader: ix.Cookie}
	auth := ix.APIKey != "" || ix.Cookie != ""
	client := newClient(creds, transport)

	switch ix.Type {
	case "torznab":
		return torznab.New(torznab.Options{ID: ix.ID, Name: ix.Name, Endpoint: ix.URL, Client: client, RequiresAuth: auth})
	case "scraper":
		def, err := readDefinition(defsDir, ix.Definition)
		if err != nil {
			return nil, err
		}

		return scraper.New(scraper.Options{Definition: def, Client: client, RequiresAuth: auth})
	default:
		return nil, fmt.Errorf("unknown source type %q", ix.Type)
	}
}

// readDefinition parses the definition file name names inside defsDir.
// name must be a local path (AGENT.md §6.11): no absolute path, no "..".
func readDefinition(defsDir, name string) (*scraper.Definition, error) {
	if !filepath.IsLocal(name) {
		return nil, fmt.Errorf("definition %q: %w", name, errDefinitionEscapes)
	}

	f, err := os.Open(filepath.Join(defsDir, name))
	if err != nil {
		return nil, fmt.Errorf("open definition %q: %w", name, err)
	}
	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, maxUserDefinitionBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read definition %q: %w", name, err)
	}

	if len(data) > maxUserDefinitionBytes {
		return nil, fmt.Errorf("definition %q: %w", name, scraper.ErrDefinitionTooLarge)
	}

	def, err := scraper.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse definition %q: %w", name, err)
	}

	return def, nil
}

// newClient builds one source's HTTP client. transport is nil in
// production (httpx builds its own) and a test double in tests.
func newClient(creds httpx.Credentials, transport http.RoundTripper) *httpx.Client {
	return httpx.New(httpx.Config{Credentials: creds, Transport: transport})
}

// searchTimeout parses search_timeout; an unparseable value (already
// reported by config validation) keeps the registry default.
func searchTimeout(s string, logger *slog.Logger) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		logger.Warn("app: search_timeout unusable; using the default", "value", s, "error", err)
		return 0
	}

	return d
}
