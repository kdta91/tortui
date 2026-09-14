package httpx

import (
	"context"
	"time"
)

// Clock is the time source the client uses for every wait it performs — the
// per-host rate limiter's spacing and the retry backoff. It exists so tests
// can drive those waits deterministically instead of sleeping on the wall
// clock: a test that has to sleep out a real backoff is either slow or
// flaky, and a flaky test is a defect.
//
// Implementations must be safe for concurrent use.
type Clock interface {
	// Now returns the current time.
	Now() time.Time

	// Sleep blocks for d, or until ctx is done, whichever happens first.
	// It returns ctx.Err() when the context ended first and nil when the
	// full duration elapsed. A non-positive d returns immediately with
	// ctx.Err() (nil for a live context), so a cancelled context is
	// reported even when there was nothing to wait for.
	Sleep(ctx context.Context, d time.Duration) error
}

// systemClock is the default Clock: real time, and a Sleep that is
// interruptible by context cancellation so a cancelled search abandons a
// pending backoff immediately instead of sleeping it out (AGENT.md §6.2).
type systemClock struct{}

// Now returns the current wall-clock time.
func (systemClock) Now() time.Time { return time.Now() }

// Sleep waits for d or until ctx is done, whichever comes first.
func (systemClock) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}

	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
