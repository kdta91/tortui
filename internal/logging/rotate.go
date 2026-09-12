package logging

import (
	"fmt"
	"os"
	"sync"
)

// DefaultMaxSizeBytes is the size threshold at which the log file rotates
// when Options.MaxSizeBytes is left at zero (T-003: rotate at 10 MB).
const DefaultMaxSizeBytes int64 = 10 * 1024 * 1024

// DefaultMaxBackups is the number of rotated backup files kept when
// Options.MaxBackups is left at zero (T-003: retain 3 files). The active
// file is not counted against this: at steady state there are up to
// DefaultMaxBackups+1 files on disk (the active file plus its backups) —
// see DEC-024.
const DefaultMaxBackups = 3

// rotatingWriter is an io.WriteCloser that appends to a file and rotates it
// once it would grow past maxSize, keeping at most maxBackups rotated
// copies (path.1 is the newest backup, path.maxBackups the oldest; anything
// older is deleted). It is safe for concurrent use.
type rotatingWriter struct {
	mu         sync.Mutex
	path       string
	maxSize    int64
	maxBackups int
	file       *os.File
	size       int64
	closed     bool
}

// newRotatingWriter opens (creating if necessary) the log file at path,
// picking up its existing size so a restart mid-file does not reset the
// rotation threshold.
func newRotatingWriter(path string, maxSize int64, maxBackups int) (*rotatingWriter, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file %s: %w", path, err)
	}

	info, err := f.Stat()
	if err != nil {
		_ = f.Close()

		return nil, fmt.Errorf("stat log file %s: %w", path, err)
	}

	return &rotatingWriter{
		path:       path,
		maxSize:    maxSize,
		maxBackups: maxBackups,
		file:       f,
		size:       info.Size(),
	}, nil
}

// Write appends p to the log file, rotating first if p would push the file
// past maxSize. A single write larger than maxSize is still written in
// full to a freshly rotated file rather than split or rejected — the
// threshold is a rotation trigger, not a hard per-write cap.
func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, fmt.Errorf("logging: write to closed log file %s", w.path)
	}

	if w.size > 0 && w.size+int64(len(p)) > w.maxSize {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}

	n, err := w.file.Write(p)
	w.size += int64(n)

	if err != nil {
		return n, fmt.Errorf("write log file %s: %w", w.path, err)
	}

	return n, nil
}

// rotate closes the current file, shifts path.1..path.maxBackups-1 up by
// one, drops anything at path.maxBackups (the oldest retained backup), and
// reopens an empty file at path. Caller must hold w.mu.
func (w *rotatingWriter) rotate() error {
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("close log file %s before rotation: %w", w.path, err)
	}

	if w.maxBackups > 0 {
		oldest := fmt.Sprintf("%s.%d", w.path, w.maxBackups)
		if err := os.Remove(oldest); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove oldest log backup %s: %w", oldest, err)
		}

		for i := w.maxBackups - 1; i >= 1; i-- {
			src := fmt.Sprintf("%s.%d", w.path, i)
			dst := fmt.Sprintf("%s.%d", w.path, i+1)

			if err := os.Rename(src, dst); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("rotate log backup %s -> %s: %w", src, dst, err)
			}
		}

		dst := fmt.Sprintf("%s.1", w.path)
		if err := os.Rename(w.path, dst); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("rotate current log file to %s: %w", dst, err)
		}
	} else if err := os.Remove(w.path); err != nil && !os.IsNotExist(err) {
		// No backups retained: rotating just means dropping the current
		// file's contents and starting fresh.
		return fmt.Errorf("remove current log file: %w", err)
	}

	f, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("reopen log file %s after rotation: %w", w.path, err)
	}

	w.file = f
	w.size = 0

	return nil
}

// Close flushes and releases the log file. It is idempotent: a second call
// returns nil rather than an error.
func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}

	w.closed = true

	if err := w.file.Close(); err != nil {
		return fmt.Errorf("close log file %s: %w", w.path, err)
	}

	return nil
}
