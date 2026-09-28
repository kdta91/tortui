package app

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package's test binary if any goroutine outlives it
// (T-093): a demo that is built, run, quit, and closed must leave nothing
// running — not the demo clock, not a Cmd blocked on the fake engine's
// Updates, not the bubbletea program. The one ignore is teatest's own
// signal.Notify goroutine, which teatest never stops (see
// internal/tui/leak_test.go).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		goleak.IgnoreTopFunction("github.com/charmbracelet/x/exp/teatest.NewTestModel.func2"),
	)
}
