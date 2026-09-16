package lifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestAcquireLockConcurrentStartAttempt reproduces "a second instance
// exits immediately with a clear message naming the running process"
// (T-042): a second AcquireLock against the same state directory, while
// the first holder is still alive, must fail with ErrAlreadyRunning and
// name the first holder's pid.
func TestAcquireLockConcurrentStartAttempt(t *testing.T) {
	stateDir := t.TempDir()

	first, err := AcquireLock(stateDir)
	if err != nil {
		t.Fatalf("first AcquireLock() error = %v", err)
	}
	defer func() { _ = first.Release() }()

	_, err = AcquireLock(stateDir)
	if err == nil {
		t.Fatal("second AcquireLock() error = nil, want ErrAlreadyRunning")
	}
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second AcquireLock() error = %v, want it to wrap ErrAlreadyRunning", err)
	}

	// The test process's own pid is the only one there is to name here
	// (both AcquireLock calls run in this same process), so the message
	// must contain it.
	pid := strconv.Itoa(os.Getpid())
	if !strings.Contains(err.Error(), pid) {
		t.Fatalf("error %q does not name the running process's pid %q", err.Error(), pid)
	}
}

// TestAcquireLockStaleLockRecovery reproduces "stale locks from a crashed
// process are detected and cleared, not inherited" (T-042). A crashed
// process leaves the lock *file* behind (with a pid recorded in it) but,
// because the OS releases the advisory lock the instant every descriptor
// referencing it closes, holds no actual OS-level lock any more. A fresh
// AcquireLock call must succeed and take over cleanly rather than being
// blocked by — or blindly trusting — that leftover content.
func TestAcquireLockStaleLockRecovery(t *testing.T) {
	stateDir := t.TempDir()

	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	lockPath := filepath.Join(stateDir, lockFileName)

	// Simulate a crashed instance: write a stale lock file naming a pid
	// that is almost certainly not running, then close it without ever
	// holding the OS lock — exactly the state a killed process leaves
	// behind (the kernel already dropped its flock/LockFileEx on exit).
	const stalePID = 999999999
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(stalePID)+"\n"), 0o600); err != nil {
		t.Fatalf("write stale lock file: %v", err)
	}

	lock, err := AcquireLock(stateDir)
	if err != nil {
		t.Fatalf("AcquireLock() over a stale lock file error = %v, want the stale lock to be cleared, not inherited", err)
	}
	defer func() { _ = lock.Release() }()

	got, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock file: %v", err)
	}
	wantPID := strconv.Itoa(os.Getpid())
	if strings.TrimSpace(string(got)) != wantPID {
		t.Fatalf("lock file content = %q, want it overwritten with this process's pid %q", got, wantPID)
	}

	// And a second, real attempt against the now-live lock is correctly
	// refused — recovering a stale lock must not leave the file unlocked
	// for anyone to take.
	if _, err := AcquireLock(stateDir); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("AcquireLock() after recovery error = %v, want ErrAlreadyRunning", err)
	}
}

// TestAcquireLockReleaseThenReacquire checks the ordinary, non-crash exit
// path: Release must free the lock for a subsequent AcquireLock in the
// same process.
func TestAcquireLockReleaseThenReacquire(t *testing.T) {
	stateDir := t.TempDir()

	lock, err := AcquireLock(stateDir)
	if err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	second, err := AcquireLock(stateDir)
	if err != nil {
		t.Fatalf("AcquireLock() after Release() error = %v", err)
	}
	defer func() { _ = second.Release() }()

	// Release is safe to call more than once, and safe on a nil receiver.
	if err := lock.Release(); err != nil {
		t.Fatalf("second Release() on already-released lock error = %v", err)
	}
	var nilLock *Lock
	if err := nilLock.Release(); err != nil {
		t.Fatalf("Release() on nil *Lock error = %v, want nil", err)
	}
}

func TestAcquireLockCreatesStateDir(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "nested", "state")

	lock, err := AcquireLock(stateDir)
	if err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}
	defer func() { _ = lock.Release() }()

	if info, err := os.Stat(stateDir); err != nil || !info.IsDir() {
		t.Fatalf("state directory %s was not created", stateDir)
	}
}
