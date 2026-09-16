// Package lifecycle implements the process-level guarantees a single
// tortui instance needs around its own start and stop: refusing to run
// twice against the same state directory, recovering from a corrupt or
// unreadable store file instead of blocking startup on it, and shutting
// down in a fixed, timeout-bounded sequence so the terminal is always
// restored — even when the engine hangs (AGENT.md §13, T-042).
//
// This package depends only on the frozen engine.Engine interface (AGENT.md
// §5) and internal/store, never on a concrete engine implementation, so it
// can be exercised end-to-end against internal/engine/fake exactly like the
// TUI is (AGENT.md §6.4).
package lifecycle

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kdta91/tortui/internal/platform"
)

// lockFileName is the single-instance lock file's name inside the state
// directory.
const lockFileName = "tortui.lock"

// ErrAlreadyRunning is returned by AcquireLock when another instance
// already holds the lock. errors.As/errors.Is against this sentinel is how
// a caller (main, or a test) tells this apart from an unrelated I/O
// failure.
var ErrAlreadyRunning = errors.New("lifecycle: tortui is already running against this state directory")

// Lock is a held single-instance lock. The zero value is not usable;
// obtain one with AcquireLock. Release (or Close) must be called exactly
// once when the process is shutting down — Shutdown does this as its final
// step, but a caller not using Shutdown must do it directly.
type Lock struct {
	path string
	file *os.File
}

// AcquireLock takes the single-instance lock for stateDir, creating the
// directory and the lock file itself if either is missing.
//
// The lock is a real OS-level advisory lock (platform.TryLockFile: flock(2)
// on macOS/Linux, LockFileEx on Windows — AGENT.md §14), not a hand-rolled
// PID check: the operating system releases it automatically when every
// file descriptor/handle referencing it closes, including when the holding
// process is killed without a chance to clean up. That is what makes a
// lock left behind by a crash self-correcting — a fresh AcquireLock call
// simply succeeds, no stale-file detection logic required — rather than a
// stale lock being inherited by the new instance (AGENT.md §13, T-042).
//
// When the lock is already held, AcquireLock reads the PID the holder
// recorded and, if platform.ProcessAlive confirms it is still running,
// names it in the wrapped error so the message the user sees is concrete
// ("tortui is already running (pid 4213)") rather than generic.
func AcquireLock(stateDir string) (*Lock, error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("lifecycle: create state directory %s: %w", stateDir, err)
	}

	path := filepath.Join(stateDir, lockFileName)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("lifecycle: open lock file %s: %w", path, err)
	}

	ok, err := platform.TryLockFile(f)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lifecycle: lock %s: %w", path, err)
	}
	if !ok {
		holder := readHolderPID(f)
		_ = f.Close()
		return nil, alreadyRunningError(holder)
	}

	if err := writeHolderPID(f, os.Getpid()); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lifecycle: record pid in lock file %s: %w", path, err)
	}

	return &Lock{path: path, file: f}, nil
}

// alreadyRunningError builds the ErrAlreadyRunning-wrapping message shown
// to the user, naming holder only when it resolves to a currently-running
// process.
func alreadyRunningError(holder int) error {
	if holder > 0 {
		if alive, _ := platform.ProcessAlive(holder); alive {
			return fmt.Errorf("%w (pid %d)", ErrAlreadyRunning, holder)
		}
	}
	return fmt.Errorf("%w", ErrAlreadyRunning)
}

// Release releases the lock and closes its file. It is safe to call more
// than once; a nil receiver is also a safe no-op, so deferring
// lock.Release() after a failed AcquireLock never needs a nil check at the
// call site.
func (l *Lock) Release() error {
	if l == nil || l.file == nil {
		return nil
	}
	f := l.file
	l.file = nil
	if err := f.Close(); err != nil {
		return fmt.Errorf("lifecycle: release lock %s: %w", l.path, err)
	}
	return nil
}

// readHolderPID best-effort reads the PID a lock file's current holder
// recorded. It returns 0 when the content is empty, unreadable, or not a
// valid PID — every caller treats that as "unknown holder" rather than an
// error, since it is purely used to enrich a message that is already going
// to say "already running" regardless.
func readHolderPID(f *os.File) int {
	if _, err := f.Seek(0, 0); err != nil {
		return 0
	}
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

// writeHolderPID overwrites the lock file's content with pid, truncating
// any previous holder's (possibly stale) content first.
func writeHolderPID(f *os.File, pid int) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "%d\n", pid); err != nil {
		return err
	}
	return f.Sync()
}
