package anacrolix

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package's test binary if any goroutine outlives it.
//
// This is the acceptance criterion for Close: an engine that leaves a
// goroutine behind leaks a whole BitTorrent client per restart, and the
// symptom (a slow, memory-hungry process after a few reconfigurations) is
// nothing like the cause. goleak turns that into an immediate, local failure.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
