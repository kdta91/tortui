package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/indexer"
	"github.com/kdta91/tortui/internal/indexer/httpx"
	"github.com/kdta91/tortui/internal/indexer/prowlarr"
	"github.com/kdta91/tortui/internal/indexer/scraper"
	"github.com/kdta91/tortui/internal/indexer/torznab"
	"github.com/kdta91/tortui/internal/tui"
)

// scraperProbeText is the keyword a scraper connection test searches for. A
// probe only needs the source to answer; what it answers is ignored.
const scraperProbeText = "test"

// errNoAggregatorURL reports an aggregator import with no instance address.
var errNoAggregatorURL = errors.New("aggregator address is empty")

// settingsManager is the settings screen's production back end (T-096): the
// tui.SourceManager and tui.PreferencesManager over one in-memory copy of
// config.toml, the live source registry, and the engine's destination roots.
//
// Every method that reads or writes that copy holds mu, so the settings
// screen's tea.Cmds — which run on their own goroutines — see saves in one
// order and never interleave a write. A save writes config.toml first
// (config.Save: atomic, mode 0600, AGENT.md §6.6) and changes the in-memory
// copy, the registry, or the engine's roots only once that succeeded.
//
// Nothing here logs a URL, an API key, or a session value. Sources are
// logged by id and type only.
type settingsManager struct {
	mu   sync.Mutex
	path string
	cfg  config.Config
	live *liveSources

	// roots is the engine's root set, or nil for an engine that does not
	// check destinations against roots.
	roots engine.RootAdder

	defsDir   string
	transport http.RoundTripper
	logger    *slog.Logger
}

var (
	_ tui.SourceManager      = (*settingsManager)(nil)
	_ tui.PreferencesManager = (*settingsManager)(nil)
)

// newSettingsManager builds the manager over cfg as loaded from path.
func newSettingsManager(path string, cfg config.Config, live *liveSources, roots engine.RootAdder, logger *slog.Logger) *settingsManager {
	return &settingsManager{
		path:      path,
		cfg:       cloneConfig(cfg),
		live:      live,
		roots:     roots,
		defsDir:   live.defsDir,
		transport: live.transport,
		logger:    logger,
	}
}

// Sources implements tui.SourceManager.
func (s *settingsManager) Sources() []config.Indexer {
	s.mu.Lock()
	defer s.mu.Unlock()

	return slices.Clone(s.cfg.Indexers)
}

// SaveSources implements tui.SourceManager: sources replace the configured
// source list on disk, then the registry is re-synced to match, so an added
// source is searchable and a disabled or removed one is gone with no
// restart. A source that cannot be built is logged and skipped (AGENT.md
// §6.3); only a failed write is an error.
func (s *settingsManager) SaveSources(sources []config.Indexer) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := cloneConfig(s.cfg)
	next.Indexers = slices.Clone(sources)

	if err := config.Save(s.path, next); err != nil {
		return fmt.Errorf("save sources: %w", err)
	}

	s.cfg = next

	if err := s.live.sync(next.Indexers, false); err != nil {
		s.logger.Warn("settings: re-sync sources", "error", err)
	}

	s.logger.Info("settings: sources saved", "configured", len(next.Indexers), "registered", len(s.live.reg.List()))

	return nil
}

// TestSource implements tui.SourceManager: one bounded probe of src, saved
// or not. A torznab source is asked for its capabilities (torznab.Discover);
// a scraper source runs one small search. The adapter's own error comes back
// unchanged, so the settings screen can classify it as an auth failure, a
// parse failure, or a timeout.
func (s *settingsManager) TestSource(ctx context.Context, src config.Indexer) error {
	err := s.probe(ctx, src)

	outcome := "reachable"
	if err != nil {
		outcome = "failed"
	}

	s.logger.Info("settings: source tested", "source", src.ID, "type", src.Type, "outcome", outcome)

	return err
}

func (s *settingsManager) probe(ctx context.Context, src config.Indexer) error {
	if src.Type == "torznab" {
		_, err := torznab.Discover(ctx, torznabOptions(src, s.transport))
		return err
	}

	a, err := userSource(src, s.defsDir, s.transport)
	if err != nil {
		return err
	}

	q := indexer.Query{Mode: indexer.ModeSearch, Text: scraperProbeText, Limit: 1}
	if caps := a.Caps(); !caps.Search && caps.Latest {
		q = indexer.Query{Mode: indexer.ModeLatest, Limit: 1}
	}

	_, err = a.Search(ctx, q)

	return err
}

// ImportDefinition implements tui.SourceManager: it installs the definition
// at source (a local path or an http(s) URL) into the definitions directory
// and returns its id.
func (s *settingsManager) ImportDefinition(ctx context.Context, source string) (string, error) {
	opts := scraper.ImporterOptions{Dir: s.defsDir}
	if s.transport != nil {
		opts.HTTPClient = newClient(httpx.Credentials{}, s.transport)
	}

	im, err := scraper.NewImporter(opts)
	if err != nil {
		return "", err
	}

	def, _, err := im.Import(ctx, source)
	if err != nil {
		return "", err
	}

	s.logger.Info("settings: definition imported", "definition", def.ID)

	return def.ID, nil
}

// ReloadDefinitions implements tui.SourceManager: every source built from a
// definition file is rebuilt from what is on disk now.
func (s *settingsManager) ReloadDefinitions() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.live.sync(s.cfg.Indexers, true); err != nil {
		return fmt.Errorf("reload definitions: %w", err)
	}

	s.logger.Info("settings: definitions reloaded", "registered", len(s.live.reg.List()))

	return nil
}

// ListAggregatorIndexers implements tui.SourceManager for a Prowlarr
// instance, with the user's own API key for it (AGENT.md §2). Each indexer's
// FeedURL is its per-indexer Torznab feed on that instance.
func (s *settingsManager) ListAggregatorIndexers(ctx context.Context, baseURL, apiKey string) ([]tui.AggregatorIndexer, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, errNoAggregatorURL
	}

	list, err := prowlarr.ListIndexers(ctx, newClient(httpx.Credentials{}, s.transport), baseURL, apiKey)
	if err != nil {
		return nil, fmt.Errorf("list aggregator indexers: %w", err)
	}

	out := make([]tui.AggregatorIndexer, 0, len(list))
	for _, idx := range list {
		feed := prowlarr.FeedURL(baseURL, idx.ID)
		out = append(out, tui.AggregatorIndexer{ID: idx.ID, Name: idx.Name, FeedURL: feed})
	}

	return out, nil
}

// Config implements tui.PreferencesManager.
func (s *settingsManager) Config() config.Config {
	s.mu.Lock()
	defer s.mu.Unlock()

	return cloneConfig(s.cfg)
}

// SaveConfig implements tui.PreferencesManager: cfg replaces every
// preference on disk. The configured sources are always the manager's own
// current list, never cfg.Indexers, so a snapshot taken before a source
// edit cannot undo it.
//
// The download directory and every saved destination are admitted as
// engine roots (AGENT.md §6.12) — each is checked as a root before anything
// is written, and admitted only after the write succeeded, so a refused
// save widens nothing.
func (s *settingsManager) SaveConfig(cfg config.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := cloneConfig(cfg)
	next.Indexers = slices.Clone(s.cfg.Indexers)

	dirs := make([]string, 0, len(next.SavedDestinations)+1)

	for _, d := range append([]string{next.DownloadDir}, next.SavedDestinations...) {
		abs, err := engine.CheckDestinationRoot(d)
		if err != nil {
			return fmt.Errorf("save preferences: %w", err)
		}

		dirs = append(dirs, abs)
	}

	if err := config.Save(s.path, next); err != nil {
		return fmt.Errorf("save preferences: %w", err)
	}

	s.cfg = next

	var errs []error

	if s.roots != nil {
		for _, d := range dirs {
			if err := s.roots.AddRoot(d); err != nil {
				errs = append(errs, fmt.Errorf("admit destination %s: %w", d, err))
			}
		}
	}

	s.logger.Info("settings: preferences saved", "saved_destinations", len(next.SavedDestinations))

	return errors.Join(errs...)
}

// torznabOptions is the torznab adapter configuration for one entry, with
// the credentials the user supplied from their own account (AGENT.md §2).
func torznabOptions(ix config.Indexer, transport http.RoundTripper) torznab.Options {
	return torznab.Options{
		ID:           ix.ID,
		Name:         ix.Name,
		Endpoint:     ix.URL,
		Client:       newClient(httpx.Credentials{APIKey: ix.APIKey, CookieHeader: ix.Cookie}, transport),
		RequiresAuth: ix.APIKey != "" || ix.Cookie != "",
	}
}

// cloneConfig copies cfg so the copy shares no slice with it.
func cloneConfig(cfg config.Config) config.Config {
	out := cfg
	out.SavedDestinations = slices.Clone(cfg.SavedDestinations)
	out.Indexers = slices.Clone(cfg.Indexers)

	return out
}
