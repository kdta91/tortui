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

// buildSources returns the live source set a run starts with: a registry
// holding the sources liveSources.sync names for cfg.
func buildSources(cfg config.Config, defsDir string, logger *slog.Logger, transport http.RoundTripper) *liveSources {
	live := newLiveSources(cfg, defsDir, logger, transport)
	if err := live.sync(cfg.Indexers, false); err != nil {
		logger.Warn("app: build the source registry", "error", err)
	}

	return live
}

// liveSources keeps an indexer registry in step with the configured sources,
// at startup and each time the settings screen saves them (T-096), so an
// added, edited, disabled, or removed source takes effect with no restart.
// It is not safe for concurrent use; settingsManager serialises its calls.
type liveSources struct {
	reg       *indexer.Registry
	defsDir   string
	logger    *slog.Logger
	transport http.RoundTripper

	// built is what each registered id was built from, so a sync rebuilds
	// only the sources whose configuration changed.
	built map[string]sourceSpec
}

// sourceSpec is what one registered source was built from: a bundled
// definition, or one [[indexer]] entry.
type sourceSpec struct {
	bundled bool
	entry   config.Indexer
}

// scraperBacked reports whether the source reads a definition file, so a
// definitions reload has to rebuild it.
func (s sourceSpec) scraperBacked() bool { return s.bundled || s.entry.Type == "scraper" }

func newLiveSources(cfg config.Config, defsDir string, logger *slog.Logger, transport http.RoundTripper) *liveSources {
	return &liveSources{
		reg:       indexer.NewRegistry(indexer.Config{Timeout: searchTimeout(cfg.SearchTimeout, logger)}),
		defsDir:   defsDir,
		logger:    logger,
		transport: transport,
		built:     make(map[string]sourceSpec),
	}
}

// sync makes the registry hold exactly these sources:
//
//   - every bundled lawful source (T-024), enabled with no setup (AGENT.md
//     §1), using the user's own definition of the same id from the
//     definitions directory when there is one (builtin.Merge's override);
//   - every enabled entry. An entry whose id matches a bundled source
//     replaces it, so `enabled = false` under that id turns a default off.
//     A disabled entry is not registered at all.
//
// A registered source whose spec is unchanged is kept as it is, unless
// reload is set and it reads a definition file. A source that cannot be
// built is logged and skipped, never fatal: one bad entry must not stop the
// rest from searching (AGENT.md §6.3). The error reports the definitions
// directory being unreadable, or how many sources were skipped. Nothing
// here touches the network, and nothing logged carries a URL or credential.
func (l *liveSources) sync(entries []config.Indexer, reload bool) error {
	bundled, defsErr := bundledDefinitions(l.defsDir, l.logger)

	configured := make(map[string]bool, len(entries))
	for _, ix := range entries {
		configured[ix.ID] = true
	}

	desired := make(map[string]sourceSpec, len(bundled)+len(entries))

	for _, def := range bundled {
		if !configured[def.ID] {
			desired[def.ID] = sourceSpec{bundled: true}
		}
	}

	for _, ix := range entries {
		if _, dup := desired[ix.ID]; ix.Enabled && !dup {
			desired[ix.ID] = sourceSpec{entry: ix}
		}
	}

	for id, spec := range l.built {
		if want, ok := desired[id]; ok && want == spec && (!reload || !spec.scraperBacked()) {
			continue
		}

		if err := l.reg.Unregister(id); err != nil {
			l.logger.Warn("app: unregister source", "source", id, "error", err)
		}

		delete(l.built, id)
	}

	failed := 0

	for _, def := range bundled {
		if spec, ok := desired[def.ID]; ok && spec.bundled {
			failed += l.register(def.ID, spec, func() (indexer.Indexer, error) {
				return scraper.New(scraper.Options{Definition: def, Client: newClient(httpx.Credentials{}, l.transport)})
			})
		}
	}

	for _, ix := range entries {
		if spec, ok := desired[ix.ID]; ok && spec == (sourceSpec{entry: ix}) {
			failed += l.register(ix.ID, spec, func() (indexer.Indexer, error) {
				return userSource(ix, l.defsDir, l.transport)
			})
		}
	}

	var errs []error
	if defsErr != nil {
		errs = append(errs, defsErr)
	}

	if failed > 0 {
		errs = append(errs, fmt.Errorf("%d source(s) could not be built; see the log", failed))
	}

	return errors.Join(errs...)
}

// register builds and registers the source spec describes under id, unless
// it is already registered. It returns 1 when the source had to be skipped.
func (l *liveSources) register(id string, spec sourceSpec, build func() (indexer.Indexer, error)) int {
	if _, ok := l.built[id]; ok {
		return 0
	}

	a, err := build()
	if err == nil {
		err = l.reg.Register(a)
	}

	if err != nil {
		kind := "bundled"
		if !spec.bundled {
			kind = spec.entry.Type
		}

		l.logger.Warn("app: skipping source", "source", id, "type", kind, "error", err)

		return 1
	}

	l.built[id] = spec

	return 0
}

// bundledDefinitions returns the bundled definitions, each replaced by the
// user's definition of the same id when the definitions directory has one.
// The error reports a definitions directory that could not be read; the
// bundled definitions are still returned.
func bundledDefinitions(defsDir string, logger *slog.Logger) ([]*scraper.Definition, error) {
	bundled, err := builtin.Definitions()
	if err != nil {
		logger.Error("app: load bundled definitions", "error", err)
		return nil, fmt.Errorf("load bundled definitions: %w", err)
	}

	var user []*scraper.Definition

	loader, err := scraper.NewLoader(scraper.LoaderOptions{Dir: defsDir, Logger: logger})
	if err == nil {
		err = loader.Reload()
		user = loader.Definitions()
	}

	if err != nil {
		logger.Warn("app: read user definitions", "dir", defsDir, "error", err)
		return bundled, fmt.Errorf("read user definitions: %w", err)
	}

	merged, err := builtin.Merge(user)
	if err != nil {
		logger.Warn("app: merge user definitions over bundled ones", "error", err)
		return bundled, fmt.Errorf("merge user definitions: %w", err)
	}

	byID := make(map[string]*scraper.Definition, len(merged))
	for _, d := range merged {
		byID[d.ID] = d
	}

	out := make([]*scraper.Definition, 0, len(bundled))
	for _, b := range bundled {
		out = append(out, byID[b.ID])
	}

	return out, nil
}

// userSource builds the adapter for one enabled [[indexer]] entry, with the
// credentials the user supplied from their own account (AGENT.md §2).
func userSource(ix config.Indexer, defsDir string, transport http.RoundTripper) (indexer.Indexer, error) {
	switch ix.Type {
	case "torznab":
		return torznab.New(torznabOptions(ix, transport))
	case "scraper":
		def, err := readDefinition(defsDir, ix.Definition)
		if err != nil {
			return nil, err
		}

		creds := httpx.Credentials{APIKey: ix.APIKey, CookieHeader: ix.Cookie}
		auth := ix.APIKey != "" || ix.Cookie != ""

		return scraper.New(scraper.Options{Definition: def, Client: newClient(creds, transport), RequiresAuth: auth})
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
