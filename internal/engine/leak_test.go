package engine

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package's test binary if any goroutine outlives it
// (T-093).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
