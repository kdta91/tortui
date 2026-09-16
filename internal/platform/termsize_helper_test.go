package platform

import (
	"os"
	"testing"
)

// newRegularFile returns a real, non-terminal *os.File so TerminalSize's
// per-OS tests can confirm it errors on something that plainly isn't a
// terminal, without requiring a real TTY to be attached to the test
// process (CI runners frequently have none).
func newRegularFile(t *testing.T) (*os.File, error) {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), "tortui-termsize-*")
	if err != nil {
		return nil, err
	}

	t.Cleanup(func() { _ = f.Close() })

	return f, nil
}
