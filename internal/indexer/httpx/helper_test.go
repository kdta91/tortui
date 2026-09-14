package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeClock is a deterministic Clock: Sleep records the duration and jumps
// time forward instead of waiting, so a test can assert the exact backoff
// schedule without any wall-clock delay. Its zero instant is real "now" so
// that comparisons against a context deadline (which context.WithTimeout
// always sets on real time) behave the way they would in production.
type fakeClock struct {
	mu sync.Mutex
	// frozen keeps Now() fixed while still recording every Sleep. Used by
	// the concurrency tests, where an advancing clock makes the recorded
	// durations depend on how the goroutines interleave.
	frozen bool
	now    time.Time
	slept  []time.Duration
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Now()}
}

func newFrozenClock() *fakeClock {
	return &fakeClock{now: time.Now(), frozen: true}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	c.mu.Lock()
	c.slept = append(c.slept, d)

	if d > 0 && !c.frozen {
		c.now = c.now.Add(d)
	}
	c.mu.Unlock()

	return ctx.Err()
}

// sleeps returns a copy of every duration Sleep was asked for, in order.
func (c *fakeClock) sleeps() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]time.Duration, len(c.slept))
	copy(out, c.slept)

	return out
}

// totalSlept sums every recorded sleep.
func (c *fakeClock) totalSlept() time.Duration {
	var total time.Duration

	for _, d := range c.sleeps() {
		total += d
	}

	return total
}

// countingServer is an httptest.Server that counts the requests it served
// and hands each one to fn.
type countingServer struct {
	*httptest.Server

	calls atomic.Int64
}

func newServer(t *testing.T, fn http.HandlerFunc) *countingServer {
	t.Helper()

	cs := &countingServer{}
	cs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cs.calls.Add(1)
		fn(w, r)
	}))

	t.Cleanup(cs.Close)

	return cs
}

func (s *countingServer) count() int { return int(s.calls.Load()) }

// statusServer answers every request with the given status code.
func statusServer(t *testing.T, code int) *countingServer {
	t.Helper()

	return newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(code)
	})
}

// testContext returns a context with a deadline, as AGENT.md §6.2 requires
// of every network call, and cancels it when the test ends.
func testContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	return ctx
}

// roundTripFunc adapts a function to http.RoundTripper, for the five cases
// httptest.Server cannot stage: a response whose Content-Length lies about
// its body, a body that fails mid-read, a body that fails both Read and
// Close, a transport error that already carries a *url.Error, and a
// same-host https-to-http redirect (a real server sends that Location
// readily enough, but two httptest servers can never share one host:port
// across two schemes).
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
