package tui

import (
	"testing"

	"go.uber.org/goleak"

	"github.com/kdta91/tortui/internal/engine/fake"
)

// TestMain fails the package's test binary if any goroutine outlives it
// (T-093). The ignores are all third-party goroutines, never this package's:
//
//   - teatest.NewTestModel.func2 waits on a signal.Notify channel that
//     teatest never stops, so every TestModel leaves it behind by design.
//   - bubbletea.Tick.func1 is a Cmd blocked on its own time.Timer (the
//     status bar's 4s transient timeout, the search spinner's 120ms); it
//     always returns once the timer fires, and Program.Send drops the
//     message once the program has exited. execBatchMsg is the parent that
//     waits on a tea.Batch's children; each child is still checked on its
//     own stack, so ignoring the parent hides nothing of ours.
//
// Anything of ours — a Cmd blocked on Engine.Updates, a test double that
// never returns — still fails the run.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		goleak.IgnoreTopFunction("github.com/charmbracelet/x/exp/teatest.NewTestModel.func2"),
		goleak.IgnoreTopFunction("github.com/charmbracelet/bubbletea.Tick.func1"),
		goleak.IgnoreAnyFunction("github.com/charmbracelet/bubbletea.(*Program).execBatchMsg"),
	)
}

// newTestEngine returns a fake engine that is closed when tb ends. Closing
// it closes Updates, which is what lets a waitForEngineUpdate Cmd still
// blocked on it after the program quit return instead of leaking.
func newTestEngine(tb testing.TB) *fake.Engine {
	tb.Helper()

	e := fake.New()
	tb.Cleanup(func() {
		if err := e.Close(); err != nil {
			tb.Errorf("close fake engine: %v", err)
		}
	})

	return e
}
