package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/engine/anacrolix"
	"github.com/kdta91/tortui/internal/indexer"
	"github.com/kdta91/tortui/internal/lifecycle"
	"github.com/kdta91/tortui/internal/logging"
	"github.com/kdta91/tortui/internal/platform"
	"github.com/kdta91/tortui/internal/store"
	"github.com/kdta91/tortui/internal/tui"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// StoreFileName is the bbolt session store's file name inside the state
// directory, next to tortui.lock and tortui.log.
const StoreFileName = "tortui.db"

// DefaultResumeTimeout bounds lifecycle.Session.Resume at startup.
const DefaultResumeTimeout = 30 * time.Second

// Options configures New. Every exported field mirrors a command-line flag;
// the zero value is a plain `tortui` run.
type Options struct {
	// ConfigPath is --config: it overrides only the config file location.
	ConfigPath string
	// LogLevel and LogFile are --log-level and --log-file, resolved against
	// TORTUI_LOG_LEVEL/TORTUI_LOG_FILE by internal/logging.
	LogLevel string
	LogFile  string
	// ASCII is --ascii. It, or ascii = true in config.toml, selects the
	// ASCII glyph fallback (AGENT.md §14).
	ASCII bool
	// Capability is the terminal capability cmd/tortui detected before
	// deciding the TUI may start at all.
	Capability theme.Capability

	// transport, when set, carries every indexer request instead of the
	// network; offline disables every engine network subsystem; configure
	// adjusts the loaded config before anything uses it. Test seams only
	// (AGENT.md §6.7): cmd/tortui never sets them.
	transport http.RoundTripper
	offline   bool
	configure func(*config.Config)
}

// App is one running tortui: the loaded config, the file log, the
// single-instance lock, the store, the real engine, the indexer registry,
// the session, and the TUI model built over them. Build one with New; Run
// it, or Close it if it will not be run.
type App struct {
	loaded   config.LoadResult
	logger   *slog.Logger
	logClose io.Closer
	prevLog  *slog.Logger
	lock     *lifecycle.Lock
	store    *store.Store
	engine   *anacrolix.Engine
	registry *indexer.Registry
	session  *lifecycle.Session
	report   lifecycle.ResumeReport
	model    tui.Model

	closeOnce sync.Once
	closeErr  error

	// notifySignals and programHook are test seams: the first replaces
	// lifecycle.NotifySignals, the second receives the program Run builds
	// just before it runs (see Demo.programHook for why).
	notifySignals func() (context.Context, context.CancelFunc)
	programHook   func(*tea.Program)
}

// New runs tortui's startup sequence, in order: load config (writing
// defaults on a first run), open the rotating file log (the engine's
// library logging goes there too), take the single-instance lock, raise the
// file-descriptor soft limit, open the store, build the engine over every
// known destination root, build the indexer registry from the bundled and
// user-configured sources, resume the session, and build the TUI model.
//
// A failure at any step undoes the steps before it. A second instance
// against the same state directory fails with lifecycle.ErrAlreadyRunning.
func New(opts Options) (*App, error) {
	level := logging.ResolveLevel("", opts.LogLevel)
	if _, err := logging.ParseLevel(level); err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}

	loaded, err := config.Load(opts.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("app: load config: %w", err)
	}

	if opts.configure != nil {
		opts.configure(&loaded.Config)
	}

	a := &App{loaded: loaded, prevLog: slog.Default(), notifySignals: lifecycle.NotifySignals}

	if err := a.start(opts, level); err != nil {
		return nil, errors.Join(err, a.Close())
	}

	return a, nil
}

// start is New's steps after config load; New closes a on any error.
func (a *App) start(opts Options, level string) error {
	paths := a.loaded.Paths
	cfg := a.loaded.Config

	logger, logClose, err := logging.New(logging.Options{
		StateDir: paths.StateDir,
		File:     logging.ResolveFile("", opts.LogFile),
		Level:    level,
	})
	if err != nil {
		return fmt.Errorf("app: open log: %w", err)
	}

	a.logger, a.logClose = logger, logClose
	logger.Info("app: starting", "config", paths.ConfigFile, "first_run", a.loaded.FirstRun)

	for _, p := range a.loaded.Problems {
		logger.Warn("app: config problem", "problem", p)
	}

	for _, w := range a.loaded.Warnings {
		logger.Warn("app: config warning", "warning", w)
	}

	if a.lock, err = lifecycle.AcquireLock(paths.StateDir); err != nil {
		return err
	}

	if fd, err := platform.RaiseFDLimit(); err != nil {
		logger.Warn("app: read file descriptor limit", "error", err)
	} else {
		logger.Info("app: file descriptor limit", "soft", fd.Soft, "hard", fd.Hard, "raised", fd.Raised, "supported", fd.Supported)
	}

	st, recovered, err := lifecycle.OpenStore(filepath.Join(paths.StateDir, StoreFileName), logger)
	if err != nil {
		return fmt.Errorf("app: open store: %w", err)
	}

	a.store = st

	engCfg := cfg
	engCfg.SavedDestinations = append(append([]string(nil), cfg.SavedDestinations...), st.Destinations()...)

	if a.engine, err = anacrolix.New(anacrolix.Options{Config: engCfg, Logger: logger, Offline: opts.offline}); err != nil {
		return fmt.Errorf("app: start engine: %w", err)
	}

	a.registry = buildRegistry(cfg, paths.DefinitionsDir, logger, opts.transport)
	a.session = lifecycle.NewSession(a.engine, st, logger)

	ctx, cancel := context.WithTimeout(context.Background(), DefaultResumeTimeout)
	defer cancel()

	if a.report, err = a.session.Resume(ctx); err != nil {
		return fmt.Errorf("app: resume session: %w", err)
	}

	logger.Info("app: session resumed", "restored", a.report.Restored, "missing", len(a.report.Missing), "failed", len(a.report.Failed))

	a.model = a.buildModel(opts, recovered)

	return nil
}

// buildModel wires every production tui option over the pieces start built.
func (a *App) buildModel(opts Options, storeRecovered string) tui.Model {
	cfg := a.loaded.Config

	capability := glyphCapability(opts.Capability, opts.ASCII, cfg.ASCII)

	tuiOpts := []tui.Option{
		tui.WithSearcher(a.registry),
		tui.WithHistory(a.store),
		tui.WithTorrentStore(a.store),
		tui.WithDestinationStore(a.store),
		tui.WithSavedDestinations(absPaths(cfg.SavedDestinations, a.logger)),
		tui.WithMinFreeSpace(minFreeSpace(cfg.MinFreeSpace, a.logger)),
		tui.WithSessionSaver(a.session),
		tui.WithStartupLatest(true),
		tui.WithStartupNotice(startupNotices(a.loaded, storeRecovered, a.report)...),
	}

	if dir, err := engine.CleanAbsPath(cfg.DownloadDir); err == nil {
		tuiOpts = append(tuiOpts, tui.WithDownloadDir(dir))
	} else {
		a.logger.Warn("app: download_dir is not usable as the picker default", "error", err)
	}

	if a.loaded.FirstRun {
		tuiOpts = append(tuiOpts, tui.WithFirstRun(true))
	}

	return tui.New(a.engine, theme.New(cfg.Theme, capability), tuiOpts...)
}

// glyphCapability applies --ascii (flag) and ascii = true (cfg) to c:
// either selects the ASCII glyph fallback, whatever was detected.
func glyphCapability(c theme.Capability, flag, cfg bool) theme.Capability {
	if flag || cfg {
		c.Unicode = false
	}

	return c
}

// Model returns the TUI model New built.
func (a *App) Model() tui.Model { return a.model }

// Engine returns the running engine.
func (a *App) Engine() *anacrolix.Engine { return a.engine }

// Registry returns the indexer registry.
func (a *App) Registry() *indexer.Registry { return a.registry }

// Loaded returns what config.Load returned (after any test adjustment).
func (a *App) Loaded() config.LoadResult { return a.loaded }

// ResumeReport returns what startup's Session.Resume reported.
func (a *App) ResumeReport() lifecycle.ResumeReport { return a.report }

// Run runs the TUI until the user quits or the process receives SIGINT or
// SIGTERM, then runs the lifecycle shutdown sequence — pause, save the
// session, flush and close the store, close the engine — releases the lock
// and closes the log. It does so on every exit path, including a panic,
// which it reports as an error rather than re-raising. bubbletea restores
// the terminal whenever its program exits, panics included; Shutdown's final
// step waits for that before returning. A signal is a clean exit (nil).
func (a *App) Run(extra ...tea.ProgramOption) (err error) {
	ctx, stopSignals := a.notifySignals()

	opts := append([]tea.ProgramOption{tea.WithAltScreen(), tea.WithContext(ctx), tea.WithoutSignalHandler()}, extra...)
	p := tea.NewProgram(a.model, opts...)
	started := false

	defer func() {
		// A second signal during shutdown takes the default action again.
		stopSignals()

		if r := recover(); r != nil {
			a.logger.Error("app: panic", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			err = fmt.Errorf("app: panic: %v", r)
		}

		err = errors.Join(err, a.shutdown(p, started))
	}()

	if a.programHook != nil {
		a.programHook(p)
	}

	started = true

	if _, runErr := p.Run(); runErr != nil {
		if ctx.Err() != nil && !errors.Is(runErr, tea.ErrProgramPanic) {
			a.logger.Info("app: shutting down on signal")
			return nil
		}

		return fmt.Errorf("app: tui: %w", runErr)
	}

	return nil
}

// Close runs the same shutdown Run does, for an App that is not running.
// It is idempotent, and safe on a partly started App.
func (a *App) Close() error { return a.shutdown(nil, false) }

// shutdown runs lifecycle.Shutdown over whatever start built, then releases
// the lock and closes the log. p is the program Run built, or nil; started
// says whether p.Run was reached, so waiting on it cannot block.
func (a *App) shutdown(p *tea.Program, started bool) error {
	a.closeOnce.Do(func() {
		opts := lifecycle.ShutdownOptions{Store: a.store, Session: a.session, Logger: a.logger}

		// A nil *anacrolix.Engine in the interface field would not be nil.
		if a.engine != nil {
			opts.Engine = a.engine
		}

		if p != nil {
			opts.StopInput = p.Kill
			opts.RestoreTerminal = func() {
				if started {
					p.Wait()
				}
			}
		}

		errs := lifecycle.Shutdown(opts)

		if err := a.lock.Release(); err != nil {
			errs = append(errs, fmt.Errorf("release lock: %w", err))
		}

		if a.logClose != nil {
			slog.SetDefault(a.prevLog)

			if err := a.logClose.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close log: %w", err))
			}
		}

		if len(errs) > 0 {
			a.closeErr = fmt.Errorf("app: shutdown: %w", errors.Join(errs...))
		}
	})

	return a.closeErr
}
