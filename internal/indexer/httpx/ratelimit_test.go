package httpx

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestHostLimiterSpacesRequestsPerHost(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	limiter := newHostLimiter(time.Second, clock)
	ctx := testContext(t)

	for i := 0; i < 3; i++ {
		if err := limiter.wait(ctx, "first.example.org"); err != nil {
			t.Fatalf("wait %d: %v", i, err)
		}
	}

	want := []time.Duration{1 * time.Second, 1 * time.Second}
	if got := clock.sleeps(); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("sleeps = %v, want %v (first request must not wait, the next two must)", got, want)
	}
}

func TestHostLimiterIsPerHostNotGlobal(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	limiter := newHostLimiter(time.Second, clock)
	ctx := testContext(t)

	for _, host := range []string{"first.example.org", "second.example.org", "third.example.org:8080"} {
		if err := limiter.wait(ctx, host); err != nil {
			t.Fatalf("wait %s: %v", host, err)
		}
	}

	if got := clock.sleeps(); len(got) != 0 {
		t.Fatalf("sleeps = %v, want none: a limit on one host must not delay another", got)
	}
}

func TestHostLimiterTreatsHostCaseInsensitively(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	limiter := newHostLimiter(time.Second, clock)
	ctx := testContext(t)

	for _, host := range []string{"Feed.Example.Org", "feed.example.org"} {
		if err := limiter.wait(ctx, host); err != nil {
			t.Fatalf("wait %s: %v", host, err)
		}
	}

	if got := clock.sleeps(); len(got) != 1 || got[0] != time.Second {
		t.Fatalf("sleeps = %v, want one 1s wait: the same host in different case is one host", got)
	}
}

func TestHostLimiterDisabledByNonPositiveInterval(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	limiter := newHostLimiter(0, clock)
	ctx := testContext(t)

	for i := 0; i < 3; i++ {
		if err := limiter.wait(ctx, "feed.example.org"); err != nil {
			t.Fatalf("wait %d: %v", i, err)
		}
	}

	if got := clock.sleeps(); len(got) != 0 {
		t.Fatalf("sleeps = %v, want none", got)
	}
}

func TestHostLimiterReturnsContextErrorWithoutWaiting(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	limiter := newHostLimiter(time.Second, clock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := limiter.wait(ctx, "feed.example.org"); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait error = %v, want context.Canceled", err)
	}

	if got := clock.sleeps(); len(got) != 0 {
		t.Fatalf("sleeps = %v, want none on an already-cancelled context", got)
	}
}

func TestHostLimiterWaitIsInterruptedByCancellation(t *testing.T) {
	t.Parallel()

	// Real clock on purpose: this is the assertion that a cancelled context
	// aborts a pending wait promptly instead of sleeping it out. The margin
	// between the interval (10s) and the deadline below (2s) is what keeps
	// it from being timing-sensitive.
	limiter := newHostLimiter(10*time.Second, systemClock{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := limiter.wait(ctx, "feed.example.org"); err != nil {
		t.Fatalf("first wait: %v", err)
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	done := make(chan error, 1)

	go func() { done <- limiter.wait(ctx, "feed.example.org") }()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("wait error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wait did not return within 2s of cancellation; it slept out the full 10s interval")
	}
}

func TestHostLimiterClaimsSlotsUnderConcurrency(t *testing.T) {
	t.Parallel()

	// Frozen clock: with an advancing one the recorded durations depend on
	// how the eight goroutines interleave with each other's simulated
	// waits, and a test whose expected value depends on scheduling is a
	// flaky test. Holding Now() still makes the claim sequence — and so the
	// assertion below — fully determined.
	clock := newFrozenClock()
	limiter := newHostLimiter(time.Second, clock)
	ctx := testContext(t)

	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			if err := limiter.wait(ctx, "feed.example.org"); err != nil {
				t.Errorf("wait: %v", err)
			}
		}()
	}

	wg.Wait()

	// Eight concurrent requests to one host, with time held still: the
	// first is admitted immediately and the other seven each claim a
	// distinct later slot, so their waits are 1s, 2s, ... 7s in some order
	// and sum to 28s whatever order the goroutines ran in. A limiter that
	// only *read* the stored instant without claiming it under the same
	// lock would let all eight through with no wait at all.
	got := clock.sleeps()
	if len(got) != 7 {
		t.Fatalf("waits = %v (%d of them), want 7: slots must be claimed under the lock, not merely checked", got, len(got))
	}

	if total, want := clock.totalSlept(), 28*time.Second; total != want {
		t.Fatalf("total waited = %s, want %s (waits of 1s..7s)", total, want)
	}
}

func TestClientRateLimitsPerHostAcrossRequests(t *testing.T) {
	t.Parallel()

	first := newServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	second := newServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	clock := newFakeClock()
	client := New(Config{MinHostInterval: 2 * time.Second, Clock: clock})
	ctx := testContext(t)

	for _, target := range []string{first.URL, first.URL, second.URL} {
		if _, err := client.Get(ctx, target, nil); err != nil {
			t.Fatalf("get: %v", err)
		}
	}

	got := clock.sleeps()
	if len(got) != 1 || got[0] != 2*time.Second {
		t.Fatalf("sleeps = %v, want exactly one 2s wait (the second request to the first host)", got)
	}
}

func TestClientRateLimitAppliesAcrossRetries(t *testing.T) {
	t.Parallel()

	server := statusServer(t, http.StatusServiceUnavailable)

	clock := newFakeClock()
	client := New(Config{
		MinHostInterval: time.Second,
		BaseBackoff:     time.Millisecond,
		MaxBackoff:      time.Millisecond,
		MaxAttempts:     3,
		Clock:           clock,
	})

	if _, err := client.Get(testContext(t), server.URL, nil); err == nil {
		t.Fatal("want an error from a server that always answers 503")
	}

	if got, want := server.count(), 3; got != want {
		t.Fatalf("requests = %d, want %d", got, want)
	}

	// Three attempts one second apart: the two gaps are the limiter's, minus
	// the 1ms backoff already waited inside each gap.
	if got, want := clock.totalSlept(), 2*time.Second; got != want {
		t.Fatalf("total waited = %s, want %s (the rate limit must apply to retries too)", got, want)
	}
}
