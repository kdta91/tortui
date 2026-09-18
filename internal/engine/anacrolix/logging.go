package anacrolix

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	alog "github.com/anacrolix/log"
)

// logRedirectMu guards the process-wide mutation RedirectLogging performs.
// anacrolix/log keeps its default logger in a package variable, so the
// redirect is global by construction — there is exactly one stderr to take
// away from it.
var logRedirectMu sync.Mutex

// RedirectLogging routes github.com/anacrolix/log — the logging façade every
// library in the anacrolix family writes through, including the torrent
// client, its DHT server and its uTP transport — into logger, which is
// expected to be the rotating file sink internal/logging installs as slog's
// default.
//
// This matters more than it looks. anacrolix/log's default handler writes to
// os.Stderr, and the TUI owns the terminal: a single tracker warning printed
// mid-render corrupts the screen and looks like a rendering bug rather than a
// log line (AGENT.md §3, §13). New calls this before it constructs a client,
// so the redirect is in place before any torrent can be added.
//
// It is safe to call more than once and from multiple goroutines; the last
// call wins. A nil logger resolves slog's default at call time.
func RedirectLogging(logger *slog.Logger) {
	logRedirectMu.Lock()
	defer logRedirectMu.Unlock()

	alog.Default.SetHandlers(slogSink{logger: logger})
}

// anacrolixLogger returns an anacrolix/log Logger that writes into logger,
// for handing to torrent.ClientConfig.Logger. Setting it on the config is not
// redundant with RedirectLogging: a client whose config carries no logger
// falls back to alog.Default, and one that carries this one is immune to
// anything that later reassigns alog.Default's handlers.
func anacrolixLogger(logger *slog.Logger) alog.Logger {
	l := alog.Logger{}
	l.SetHandlers(slogSink{logger: logger})

	// anacrolix/log filters at Warning when no level is set, and messages
	// logged without an explicit level arrive as NotSet. Default them to
	// debug so they are classified by the slog sink's own level instead of
	// silently vanishing or being promoted to a warning.
	return l.WithDefaultLevel(alog.Debug).WithFilterLevel(alog.Debug)
}

// slogSink is an anacrolix/log Handler that forwards every record to a
// slog.Logger.
type slogSink struct {
	logger *slog.Logger
}

// Handle implements alog.Handler.
func (s slogSink) Handle(r alog.Record) {
	level, ok := slogLevel(r.Level)
	if !ok {
		return
	}

	logger := s.logger
	if logger == nil {
		// Resolved at call time rather than at construction so a sink built
		// before internal/logging installed the file sink still lands in the
		// file rather than in whatever slog's default was at the time.
		logger = slog.Default()
	}

	if !logger.Enabled(context.Background(), level) {
		return
	}

	logger.LogAttrs(context.Background(), level, r.Msg.String(),
		slog.String("source", "anacrolix"),
		slog.String("names", strings.Join(r.Names, "/")),
	)
}

// slogLevel maps an anacrolix/log level onto a slog level. The second result
// is false for the levels that mean "never record this" — anacrolix/log's own
// conversion panics on them, which is not a reasonable thing for a logging
// path to do to a running TUI.
func slogLevel(l alog.Level) (slog.Level, bool) {
	switch l {
	case alog.Debug, alog.NotSet:
		return slog.LevelDebug, true
	case alog.Info:
		return slog.LevelInfo, true
	case alog.Warning:
		return slog.LevelWarn, true
	case alog.Error:
		return slog.LevelError, true
	case alog.Critical:
		return slog.LevelError + 1, true
	case alog.Never, alog.Disabled:
		return 0, false
	default:
		return slog.LevelDebug, true
	}
}
