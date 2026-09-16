package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTryLockFileExclusiveAcrossDescriptors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tortui.lock")

	f1, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open first fd: %v", err)
	}
	defer func() { _ = f1.Close() }()

	ok, err := TryLockFile(f1)
	if err != nil {
		t.Fatalf("TryLockFile(f1) error = %v", err)
	}
	if !ok {
		t.Fatal("TryLockFile(f1) = false, want the first handle to win the lock")
	}

	f2, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open second fd: %v", err)
	}
	defer func() { _ = f2.Close() }()

	ok2, err := TryLockFile(f2)
	if err != nil {
		t.Fatalf("TryLockFile(f2) error = %v", err)
	}
	if ok2 {
		t.Fatal("TryLockFile(f2) = true, want the second handle to be denied while the first holds the lock")
	}

	if err := f1.Close(); err != nil {
		t.Fatalf("close f1: %v", err)
	}

	// A fresh handle must be able to acquire the lock now that the holder's
	// handle has closed.
	f3, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open third fd: %v", err)
	}
	defer func() { _ = f3.Close() }()

	ok3, err := TryLockFile(f3)
	if err != nil {
		t.Fatalf("TryLockFile(f3) error = %v", err)
	}
	if !ok3 {
		t.Fatal("TryLockFile(f3) = false, want the lock to be free after the holder closed its handle")
	}
}
