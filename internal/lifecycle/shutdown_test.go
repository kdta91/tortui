package lifecycle

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/engine/fake"
	"github.com/kdta91/tortui/internal/store"
)

// TestShutdownHappyPath runs the full sequence against engine/fake and a
// real store, and checks every step ran in order and the terminal was
// restored.
func TestShutdownHappyPath(t *testing.T) {
	eng := fake.New()
	if _, err := eng.Add(context.Background(), engine.AddSource{Magnet: "magnet:?xt=urn:btih:aaaa&dn=one"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	st, err := store.Open(filepath.Join(t.TempDir(), "tortui.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}

	var (
		mu    sync.Mutex
		order []string
	)
	record := func(name string) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, name)
	}

	var inputStopped, terminalRestored atomic.Bool

	errs := Shutdown(ShutdownOptions{
		Engine: eng,
		Store:  st,
		StopInput: func() {
			inputStopped.Store(true)
			record("stop-input")
		},
		RestoreTerminal: func() {
			terminalRestored.Store(true)
			record("restore-terminal")
		},
		StepTimeout: time.Second,
	})
	if len(errs) != 0 {
		t.Fatalf("Shutdown() errors = %v, want none on the happy path", errs)
	}

	if !inputStopped.Load() {
		t.Fatal("StopInput was never called")
	}
	if !terminalRestored.Load() {
		t.Fatal("RestoreTerminal was never called")
	}

	// The added torrent must have been paused before the engine closed.
	list := eng.List()
	if len(list) != 1 || list[0].State != engine.StatePaused {
		t.Fatalf("engine torrents after Shutdown = %+v, want one paused torrent", list)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "stop-input" || order[1] != "restore-terminal" {
		t.Fatalf("callback order = %v, want [stop-input restore-terminal]", order)
	}
}

// hungEngine wraps a fake.Engine but blocks forever in Close, standing in
// for a real engine implementation that has wedged — the scenario T-042
// calls "shutdown-with-hung-engine". Every other method delegates to the
// wrapped fake so List/Pause still behave normally.
type hungEngine struct {
	*fake.Engine
	closeCalled chan struct{}
}

func newHungEngine() *hungEngine {
	return &hungEngine{Engine: fake.New(), closeCalled: make(chan struct{})}
}

// Close signals closeCalled (so the test can prove it was actually invoked)
// and then blocks forever, simulating an engine whose shutdown never
// completes.
func (h *hungEngine) Close() error {
	close(h.closeCalled)
	select {} // deliberately hang
}

// TestShutdownWithHungEngine is T-042's required
// shutdown-with-hung-engine test: Engine.Close never returns, and Shutdown
// must still complete (within the step timeout budget) and still restore
// the terminal, rather than hanging forever itself.
func TestShutdownWithHungEngine(t *testing.T) {
	eng := newHungEngine()

	st, err := store.Open(filepath.Join(t.TempDir(), "tortui.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}

	var terminalRestored atomic.Bool
	const stepTimeout = 50 * time.Millisecond

	done := make(chan []error, 1)
	go func() {
		done <- Shutdown(ShutdownOptions{
			Engine: eng,
			Store:  st,
			RestoreTerminal: func() {
				terminalRestored.Store(true)
			},
			StepTimeout: stepTimeout,
		})
	}()

	select {
	case errs := <-done:
		if !terminalRestored.Load() {
			t.Fatal("RestoreTerminal was not called when the engine hung")
		}

		var foundTimeout bool
		for _, err := range errs {
			if errors.Is(err, ErrStepTimedOut) {
				foundTimeout = true
			}
		}
		if !foundTimeout {
			t.Fatalf("Shutdown() errors = %v, want at least one ErrStepTimedOut for the hung close engine step", errs)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown() did not return within 5s despite a hung engine — a hung engine must never strand the caller")
	}

	select {
	case <-eng.closeCalled:
	default:
		t.Fatal("engine.Close was never called")
	}
}

// TestShutdownRestoresTerminalEvenWhenEverythingElseIsNil checks the
// minimal case a caller invoking Shutdown before anything was wired up
// (or from a defer after an early failure) needs: only RestoreTerminal is
// set, and it still runs.
func TestShutdownRestoresTerminalEvenWhenEverythingElseIsNil(t *testing.T) {
	var restored atomic.Bool

	errs := Shutdown(ShutdownOptions{
		RestoreTerminal: func() { restored.Store(true) },
	})
	if len(errs) != 0 {
		t.Fatalf("Shutdown() errors = %v, want none", errs)
	}
	if !restored.Load() {
		t.Fatal("RestoreTerminal was not called")
	}
}

// TestShutdownNoRestoreTerminalIsSafe checks that a nil RestoreTerminal
// (a caller that never took over the terminal) does not panic.
func TestShutdownNoRestoreTerminalIsSafe(t *testing.T) {
	errs := Shutdown(ShutdownOptions{})
	if len(errs) != 0 {
		t.Fatalf("Shutdown() errors = %v, want none", errs)
	}
}
