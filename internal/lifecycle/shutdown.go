package lifecycle

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/store"
)

// DefaultStepTimeout bounds how long any single shutdown step is given
// before Shutdown moves on without it. It exists so a hung engine — or a
// slow disk during the final store flush — cannot strand the user in raw
// mode forever (AGENT.md §13, T-042).
const DefaultStepTimeout = 5 * time.Second

// ErrStepTimedOut wraps a shutdown step's error when it did not complete
// within its timeout. The step's goroutine is left running in the
// background when this happens — the process is about to exit regardless,
// and abandoning it is strictly better than blocking terminal restore on
// code that has already proven itself unresponsive.
var ErrStepTimedOut = errors.New("lifecycle: shutdown step timed out")

// ShutdownOptions bundles everything Shutdown needs to run the graceful
// shutdown sequence: stop accepting input, pause torrents, flush the
// store, close the engine, close the store, then restore the terminal
// (AGENT.md §13, T-042). Every field is optional except RestoreTerminal;
// a nil Engine or Store simply skips the steps that need it.
type ShutdownOptions struct {
	// Engine is paused (every tracked torrent) and then closed. May be nil
	// (e.g. a shutdown invoked before an engine was ever wired up).
	Engine engine.Engine

	// Store is flushed and then closed. May be nil.
	Store *store.Store

	// Session, when set, is saved after torrents are paused and before
	// the store is flushed, so the final flush carries the session a
	// restart resumes (T-041). May be nil.
	Session *Session

	// StopInput, when set, is called first and is expected to stop any
	// further input from reaching the application (e.g. cancel a
	// bubbletea program's input reader). It is expected to return quickly;
	// like every other step it is still timeout-bounded.
	StopInput func()

	// RestoreTerminal restores the terminal to its normal (non-raw,
	// non-alternate-screen) state. It always runs, last, unconditionally —
	// even if every earlier step failed or timed out, and even when
	// Shutdown is called from a deferred recover() after a panic
	// (AGENT.md §13: "Restore on every exit path — normal, signal, and
	// panic"). A nil RestoreTerminal is a no-op, which is only appropriate
	// when the caller never took over the terminal in the first place
	// (e.g. exercising this sequence from a non-TUI test).
	RestoreTerminal func()

	// StepTimeout bounds each individual step. Zero uses
	// DefaultStepTimeout.
	StepTimeout time.Duration

	// Logger receives one line per step, plus a warning for any
	// individual Pause call that failed. A nil Logger uses slog.Default().
	Logger *slog.Logger
}

// Shutdown runs the graceful shutdown sequence described by opts and
// returns every error encountered, one per failed or timed-out step —
// never fewer than zero and never a single combined error, so a caller
// can log or report each independently. RestoreTerminal is guaranteed to
// run before Shutdown returns, regardless of how many earlier steps failed
// or timed out.
func Shutdown(opts ShutdownOptions) []error {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	timeout := opts.StepTimeout
	if timeout <= 0 {
		timeout = DefaultStepTimeout
	}

	var errs []error

	defer func() {
		if opts.RestoreTerminal != nil {
			// The terminal restore itself is not allowed to hang the
			// process either: run it through the same timeout machinery
			// as everything else instead of calling it bare.
			if err := runStep(logger, timeout, "restore terminal", func() error {
				opts.RestoreTerminal()
				return nil
			}); err != nil {
				errs = append(errs, err)
			}
		}
	}()

	if opts.StopInput != nil {
		if err := runStep(logger, timeout, "stop input", func() error {
			opts.StopInput()
			return nil
		}); err != nil {
			errs = append(errs, err)
		}
	}

	if opts.Engine != nil {
		if err := runStep(logger, timeout, "pause torrents", func() error {
			return pauseAll(opts.Engine, logger)
		}); err != nil {
			errs = append(errs, err)
		}
	}

	if opts.Session != nil {
		if err := runStep(logger, timeout, "save session", opts.Session.Save); err != nil {
			errs = append(errs, err)
		}
	}

	if opts.Store != nil {
		if err := runStep(logger, timeout, "flush store", opts.Store.Flush); err != nil {
			errs = append(errs, err)
		}
	}

	if opts.Engine != nil {
		if err := runStep(logger, timeout, "close engine", opts.Engine.Close); err != nil {
			errs = append(errs, err)
		}
	}

	if opts.Store != nil {
		if err := runStep(logger, timeout, "close store", opts.Store.Close); err != nil {
			errs = append(errs, err)
		}
	}

	return errs
}

// pauseAll pauses every torrent the engine currently tracks, logging (but
// not failing the step on) any individual Pause error — one bad torrent ID
// must not stop the rest from being paused before the engine closes.
func pauseAll(e engine.Engine, logger *slog.Logger) error {
	for _, ts := range e.List() {
		if err := e.Pause(ts.ID); err != nil {
			logger.Warn("lifecycle: pause failed during shutdown", "id", ts.ID, "error", err)
		}
	}
	return nil
}

// runStep runs fn on its own goroutine and waits up to timeout for it to
// finish, logging the outcome either way. If fn has not returned by the
// deadline, runStep returns ErrStepTimedOut immediately and leaves fn's
// goroutine running in the background rather than waiting on it further —
// see ErrStepTimedOut's doc comment for why that is the right trade-off
// here.
func runStep(logger *slog.Logger, timeout time.Duration, name string, fn func() error) error {
	done := make(chan error, 1)
	go func() {
		done <- fn()
	}()

	select {
	case err := <-done:
		if err != nil {
			logger.Error("lifecycle: shutdown step failed", "step", name, "error", err)
			return fmt.Errorf("%s: %w", name, err)
		}
		logger.Debug("lifecycle: shutdown step completed", "step", name)
		return nil
	case <-time.After(timeout):
		logger.Error("lifecycle: shutdown step timed out", "step", name, "timeout", timeout)
		return fmt.Errorf("%s: %w", name, ErrStepTimedOut)
	}
}
