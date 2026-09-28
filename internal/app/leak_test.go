package app

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package's test binary if any goroutine outlives it
// (T-093): a demo or a production root that is built, run, quit, and closed
// must leave nothing running — not the demo clock, not the engine, the
// store's flush loop, or a Cmd blocked on Updates. The ignores are
// third-party goroutines only, the same set internal/tui/leak_test.go
// documents: teatest's own signal.Notify goroutine, which teatest never
// stops, and bubbletea.Tick commands (the status bar's transient timeout)
// that return once their timer fires, with execBatchMsg their parent.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		goleak.IgnoreTopFunction("github.com/charmbracelet/x/exp/teatest.NewTestModel.func2"),
		goleak.IgnoreTopFunction("github.com/charmbracelet/bubbletea.Tick.func1"),
		goleak.IgnoreAnyFunction("github.com/charmbracelet/bubbletea.(*Program).execBatchMsg"),
	)
}
