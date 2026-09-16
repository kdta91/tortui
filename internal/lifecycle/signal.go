package lifecycle

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// NotifySignals returns a context that is cancelled the first time the
// process receives SIGINT or SIGTERM, plus a stop function the caller must
// invoke once it no longer wants to be notified (mirroring
// signal.NotifyContext, which this wraps). It exists so main and the TUI
// share one definition of "what counts as a shutdown signal" instead of
// each registering its own os/signal.Notify call (AGENT.md §13, T-042: the
// graceful shutdown sequence must run identically on quit and on
// SIGINT/SIGTERM).
func NotifySignals() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
