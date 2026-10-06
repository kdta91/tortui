package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/url"
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

// An explicit default port, a trailing dot, letter case and the scheme all
// name the same server, so they share one spacing bucket (T-928). Before,
// the bucket was the host as written: http and https to one host already
// shared it, but each port spelling was its own and a source's budget
// doubled. Every old bucket must sit inside one new bucket, so the change
// only adds spacing. A non-default port is still its own bucket.
func TestClientRateLimitBucketIgnoresDefaultPortTrailingDotAndScheme(t *testing.T) {
	t.Parallel()

	ok := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: http.Header{}, Request: r}, nil
	})

	// Frozen, so each wait shows how many earlier requests share its
	// bucket: 1s behind one, 2s behind two, and so on.
	clock := newFrozenClock()
	client := New(Config{MinHostInterval: time.Second, Clock: clock, Transport: ok})
	ctx := testContext(t)

	for _, target := range []string{
		"http://feed.example.org/a",
		"http://feed.example.org:80/b",
		"http://FEED.example.org./c",
		"https://feed.example.org/d",
		"https://feed.example.org:443/e",
		"http://feed.example.org:8080/f",
	} {
		if _, err := client.Get(ctx, target, nil); err != nil {
			t.Fatalf("get %s: %v", target, err)
		}
	}

	want := []time.Duration{time.Second, 2 * time.Second, 3 * time.Second, 4 * time.Second}
	got := clock.sleeps()
	if len(got) != len(want) {
		t.Fatalf("sleeps = %v, want %v: every default-port spelling over either scheme is one bucket, port 8080 its own", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sleeps = %v, want %v", got, want)
		}
	}
}

// The review probe for T-928: https then http to one host is one server and
// must be spaced, as it was when the bucket was the host as written. A
// same-host upgrade or a details fetch on the other scheme takes this path.
func TestClientRateLimitSpacesHTTPAndHTTPSToOneHost(t *testing.T) {
	t.Parallel()

	ok := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: http.Header{}, Request: r}, nil
	})

	clock := newFakeClock()
	client := New(Config{MinHostInterval: time.Second, Clock: clock, Transport: ok})
	ctx := testContext(t)

	for _, target := range []string{"https://feed.example.org/s", "http://feed.example.org/s"} {
		if _, err := client.Get(ctx, target, nil); err != nil {
			t.Fatalf("get %s: %v", target, err)
		}
	}

	if got := clock.sleeps(); len(got) != 1 || got[0] != time.Second {
		t.Fatalf("sleeps = %v, want [1s]: http and https to one host share one bucket", got)
	}
}

func TestRateLimitKey(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ in, want string }{
		{"http://feed.example.org/x", "feed.example.org"},
		{"http://feed.example.org:80/x", "feed.example.org"},
		{"HTTP://Feed.Example.Org./x", "feed.example.org"},
		{"https://feed.example.org/x", "feed.example.org"},
		{"https://feed.example.org:443/x", "feed.example.org"},
		{"http://feed.example.org:443/x", "feed.example.org"},
		{"https://feed.example.org:8443/x", "feed.example.org:8443"},
		{"http://127.0.0.1:9117/x", "127.0.0.1:9117"},
	} {
		u, err := url.Parse(tt.in)
		if err != nil {
			t.Fatalf("parse %q: %v", tt.in, err)
		}
		if got := rateLimitKey(u); got != tt.want {
			t.Errorf("rateLimitKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
