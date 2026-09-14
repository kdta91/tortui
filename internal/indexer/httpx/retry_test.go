package httpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRetriesOn429AndRecovers(t *testing.T) {
	t.Parallel()

	var served atomic.Int64

	server := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if served.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)

			return
		}

		if _, err := io.WriteString(w, "ok"); err != nil {
			t.Errorf("write body: %v", err)
		}
	})

	clock := newFakeClock()
	client := New(Config{MinHostInterval: -1, Clock: clock, BaseBackoff: 500 * time.Millisecond})

	res, err := client.Get(testContext(t), server.URL, nil)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if string(res.Body) != "ok" {
		t.Fatalf("body = %q, want %q", res.Body, "ok")
	}

	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}

	if got, want := server.count(), 2; got != want {
		t.Fatalf("requests = %d, want %d", got, want)
	}

	if got := clock.sleeps(); len(got) != 1 || got[0] != 500*time.Millisecond {
		t.Fatalf("sleeps = %v, want one 500ms backoff", got)
	}
}

func TestRetriesOn5xxStatuses(t *testing.T) {
	t.Parallel()

	for _, code := range []int{
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
		http.StatusNotImplemented,
	} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			t.Parallel()

			server := statusServer(t, code)
			clock := newFakeClock()
			client := New(Config{MinHostInterval: -1, MaxAttempts: 3, Clock: clock})

			_, err := client.Get(testContext(t), server.URL, nil)

			var statusErr *StatusError
			if !errors.As(err, &statusErr) {
				t.Fatalf("error = %v, want a *StatusError", err)
			}

			if statusErr.StatusCode != code {
				t.Fatalf("status = %d, want %d", statusErr.StatusCode, code)
			}

			if statusErr.Attempts != 3 {
				t.Fatalf("attempts recorded = %d, want 3", statusErr.Attempts)
			}

			if got, want := server.count(), 3; got != want {
				t.Fatalf("requests = %d, want %d", got, want)
			}
		})
	}
}

func TestNeverRetriesAnyClientError(t *testing.T) {
	t.Parallel()

	// 408 is in this list deliberately: it is the one 4xx a retry could
	// plausibly help, and T-020's criteria say "429/5xx only, never on
	// 4xx". See DEC-058.
	for _, code := range []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusRequestTimeout,
		http.StatusGone,
		http.StatusUnprocessableEntity,
		http.StatusTeapot,
	} {
		t.Run(fmt.Sprintf("%d", code), func(t *testing.T) {
			t.Parallel()

			server := statusServer(t, code)
			clock := newFakeClock()
			client := New(Config{MinHostInterval: -1, MaxAttempts: 4, Clock: clock})

			_, err := client.Get(testContext(t), server.URL, nil)

			var statusErr *StatusError
			if !errors.As(err, &statusErr) {
				t.Fatalf("error = %v, want a *StatusError", err)
			}

			if got, want := server.count(), 1; got != want {
				t.Fatalf("requests = %d, want %d: %d must never be retried", got, want, code)
			}

			if statusErr.Attempts != 1 {
				t.Fatalf("attempts recorded = %d, want 1", statusErr.Attempts)
			}

			if len(clock.sleeps()) != 0 {
				t.Fatalf("sleeps = %v, want none", clock.sleeps())
			}
		})
	}
}

func TestBackoffIsExponentialAndCapped(t *testing.T) {
	t.Parallel()

	server := statusServer(t, http.StatusInternalServerError)

	clock := newFakeClock()
	client := New(Config{
		MinHostInterval: -1,
		MaxAttempts:     5,
		BaseBackoff:     100 * time.Millisecond,
		MaxBackoff:      300 * time.Millisecond,
		Clock:           clock,
	})

	if _, err := client.Get(testContext(t), server.URL, nil); err == nil {
		t.Fatal("want an error")
	}

	want := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		300 * time.Millisecond,
		300 * time.Millisecond,
	}

	got := clock.sleeps()
	if len(got) != len(want) {
		t.Fatalf("sleeps = %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sleeps = %v, want %v (doubling, clamped at MaxBackoff)", got, want)
		}
	}
}

func TestJitterIsAppliedWhenInjected(t *testing.T) {
	t.Parallel()

	server := statusServer(t, http.StatusInternalServerError)

	clock := newFakeClock()
	client := New(Config{
		MinHostInterval: -1,
		MaxAttempts:     2,
		BaseBackoff:     time.Second,
		Clock:           clock,
		Jitter:          func(d time.Duration) time.Duration { return d / 4 },
	})

	if _, err := client.Get(testContext(t), server.URL, nil); err == nil {
		t.Fatal("want an error")
	}

	if got := clock.sleeps(); len(got) != 1 || got[0] != 250*time.Millisecond {
		t.Fatalf("sleeps = %v, want one 250ms wait (jitter applied to the 1s backoff)", got)
	}
}

func TestRetryAfterDeltaSecondsIsHonoured(t *testing.T) {
	t.Parallel()

	server := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusTooManyRequests)
	})

	clock := newFakeClock()
	client := New(Config{MinHostInterval: -1, MaxAttempts: 2, BaseBackoff: time.Hour, Clock: clock})

	if _, err := client.Get(testContext(t), server.URL, nil); err == nil {
		t.Fatal("want an error")
	}

	if got := clock.sleeps(); len(got) != 1 || got[0] != 2*time.Second {
		t.Fatalf("sleeps = %v, want one 2s wait taken from Retry-After, not the 1h backoff", got)
	}
}

func TestRetryAfterHTTPDateIsHonoured(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	clock.now = clock.now.Truncate(time.Second)
	when := clock.Now().Add(3 * time.Second).UTC().Format(http.TimeFormat)

	server := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", when)
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	client := New(Config{MinHostInterval: -1, MaxAttempts: 2, BaseBackoff: time.Hour, Clock: clock})

	if _, err := client.Get(testContext(t), server.URL, nil); err == nil {
		t.Fatal("want an error")
	}

	if got := clock.sleeps(); len(got) != 1 || got[0] != 3*time.Second {
		t.Fatalf("sleeps = %v, want one 3s wait taken from the HTTP-date Retry-After", got)
	}
}

func TestRetryAfterMalformedFallsBackToBackoff(t *testing.T) {
	t.Parallel()

	server := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "in a little while")
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	clock := newFakeClock()
	client := New(Config{MinHostInterval: -1, MaxAttempts: 2, BaseBackoff: 750 * time.Millisecond, Clock: clock})

	if _, err := client.Get(testContext(t), server.URL, nil); err == nil {
		t.Fatal("want an error")
	}

	if got := clock.sleeps(); len(got) != 1 || got[0] != 750*time.Millisecond {
		t.Fatalf("sleeps = %v, want the 750ms backoff: an unparseable Retry-After is no advice at all", got)
	}
}

func TestRetryAfterInThePastRetriesImmediately(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	when := clock.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)

	server := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", when)
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	client := New(Config{MinHostInterval: -1, MaxAttempts: 2, BaseBackoff: time.Hour, Clock: clock})

	if _, err := client.Get(testContext(t), server.URL, nil); err == nil {
		t.Fatal("want an error")
	}

	if got := clock.sleeps(); len(got) != 1 || got[0] != 0 {
		t.Fatalf("sleeps = %v, want a single zero wait: a past date means 'now', not 'never'", got)
	}
}

func TestRetryAfterLongerThanTheLimitStopsInsteadOfWaiting(t *testing.T) {
	t.Parallel()

	server := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	})

	clock := newFakeClock()
	client := New(Config{MinHostInterval: -1, MaxAttempts: 4, MaxRetryAfter: 30 * time.Second, Clock: clock})

	_, err := client.Get(testContext(t), server.URL, nil)

	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error = %v, want a *StatusError in the chain", err)
	}

	if statusErr.RetryAfter != time.Hour || !statusErr.RetryAfterSet {
		t.Fatalf("RetryAfter = %s (set=%t), want 1h0m0s set", statusErr.RetryAfter, statusErr.RetryAfterSet)
	}

	if got, want := server.count(), 1; got != want {
		t.Fatalf("requests = %d, want %d: an hour-long Retry-After must not be capped and retried", got, want)
	}

	if len(clock.sleeps()) != 0 {
		t.Fatalf("sleeps = %v, want none", clock.sleeps())
	}

	if !strings.Contains(err.Error(), "longer than the 30s limit") {
		t.Fatalf("error = %q, want it to say why it stopped", err)
	}
}

func TestRetryDelayLongerThanTheDeadlineStopsImmediately(t *testing.T) {
	t.Parallel()

	server := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "20")
		w.WriteHeader(http.StatusServiceUnavailable)
	})

	clock := newFakeClock()
	client := New(Config{
		MinHostInterval: -1,
		MaxAttempts:     4,
		MaxRetryAfter:   time.Minute,
		RequestTimeout:  5 * time.Second,
		Clock:           clock,
	})

	_, err := client.Get(context.Background(), server.URL, nil)

	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("error = %v, want a *StatusError in the chain", err)
	}

	if got, want := server.count(), 1; got != want {
		t.Fatalf("requests = %d, want %d", got, want)
	}

	if len(clock.sleeps()) != 0 {
		t.Fatalf("sleeps = %v, want none: a wait that outlasts the deadline is not worth taking", clock.sleeps())
	}

	if !strings.Contains(err.Error(), "outlast the request deadline") {
		t.Fatalf("error = %q, want it to say the deadline was the reason", err)
	}
}

func TestCancellingTheContextAbortsTheRetryLoopPromptly(t *testing.T) {
	t.Parallel()

	server := statusServer(t, http.StatusServiceUnavailable)

	// Real clock: the point of this test is that the wait is abandoned
	// rather than slept out. Ten seconds of backoff against a two-second
	// deadline is a wide enough margin not to be timing-sensitive.
	client := New(Config{
		MinHostInterval: -1,
		MaxAttempts:     4,
		BaseBackoff:     10 * time.Second,
		MaxBackoff:      10 * time.Second,
		MaxRetryAfter:   time.Minute,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	done := make(chan error, 1)

	go func() {
		_, err := client.Get(ctx, server.URL, nil)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled in the chain", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Do did not return within 2s of cancellation; it slept out the full backoff")
	}
}

func TestSingleAttemptConfigurationDisablesRetrying(t *testing.T) {
	t.Parallel()

	server := statusServer(t, http.StatusServiceUnavailable)
	clock := newFakeClock()
	client := New(Config{MinHostInterval: -1, MaxAttempts: 1, Clock: clock})

	if _, err := client.Get(testContext(t), server.URL, nil); err == nil {
		t.Fatal("want an error")
	}

	if got, want := server.count(), 1; got != want {
		t.Fatalf("requests = %d, want %d", got, want)
	}
}

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name  string
		value string
		want  time.Duration
		ok    bool
	}{
		{name: "empty", value: "", want: 0, ok: false},
		{name: "whitespace only", value: "   ", want: 0, ok: false},
		{name: "delta seconds", value: "120", want: 2 * time.Minute, ok: true},
		{name: "delta seconds padded", value: " 5 ", want: 5 * time.Second, ok: true},
		{name: "zero seconds", value: "0", want: 0, ok: true},
		{name: "negative seconds", value: "-30", want: 0, ok: true},
		{name: "http date in the future", value: now.Add(90 * time.Second).Format(http.TimeFormat), want: 90 * time.Second, ok: true},
		{name: "http date in the past", value: now.Add(-time.Hour).Format(http.TimeFormat), want: 0, ok: true},
		{name: "absurdly large", value: "31536000", want: 365 * 24 * time.Hour, ok: true},
		{name: "malformed word", value: "soon", want: 0, ok: false},
		{name: "malformed float", value: "1.5", want: 0, ok: false},
		{name: "malformed date", value: "Wed, 99 Foo 2026 07:28:00 GMT", want: 0, ok: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := parseRetryAfter(tc.value, now)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("parseRetryAfter(%q) = (%s, %t), want (%s, %t)", tc.value, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestBackoffFor(t *testing.T) {
	t.Parallel()

	base := 100 * time.Millisecond
	maxDelay := time.Second

	cases := []struct {
		n    int
		want time.Duration
	}{
		{n: 0, want: 100 * time.Millisecond},
		{n: 1, want: 100 * time.Millisecond},
		{n: 2, want: 200 * time.Millisecond},
		{n: 3, want: 400 * time.Millisecond},
		{n: 4, want: 800 * time.Millisecond},
		{n: 5, want: time.Second},
		{n: 64, want: time.Second},
		{n: 1000, want: time.Second},
	}

	for _, tc := range cases {
		if got := backoffFor(tc.n, base, maxDelay); got != tc.want {
			t.Fatalf("backoffFor(%d) = %s, want %s", tc.n, got, tc.want)
		}
	}

	if got := backoffFor(3, 0, maxDelay); got != 0 {
		t.Fatalf("backoffFor with a zero base = %s, want 0", got)
	}
}

func TestIsRetryableStatus(t *testing.T) {
	t.Parallel()

	retryable := []int{429, 500, 502, 503, 504, 599}
	for _, code := range retryable {
		if !isRetryableStatus(code) {
			t.Fatalf("isRetryableStatus(%d) = false, want true", code)
		}
	}

	permanent := []int{200, 204, 301, 400, 401, 403, 404, 408, 410, 418, 422, 451, 600}
	for _, code := range permanent {
		if isRetryableStatus(code) {
			t.Fatalf("isRetryableStatus(%d) = true, want false", code)
		}
	}
}

func TestStatusErrorMessage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  *StatusError
		want string
	}{
		{
			name: "single attempt",
			err:  &StatusError{Method: "GET", Host: "feed.example.org", StatusCode: 404, Attempts: 1},
			want: "httpx: GET feed.example.org: HTTP 404 Not Found (1 attempt)",
		},
		{
			name: "several attempts with retry-after",
			err: &StatusError{
				Method: "GET", Host: "feed.example.org:8443", StatusCode: 429,
				RetryAfter: 90 * time.Second, RetryAfterSet: true, Attempts: 4,
			},
			want: "httpx: GET feed.example.org:8443: HTTP 429 Too Many Requests (4 attempts, server asked to retry after 1m30s)",
		},
		{
			name: "unknown status code",
			err:  &StatusError{Method: "POST", Host: "feed.example.org", StatusCode: 599, Attempts: 2},
			want: "httpx: POST feed.example.org: HTTP 599 (2 attempts)",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.err.Error(); got != tc.want {
				t.Fatalf("Error() = %q, want %q", got, tc.want)
			}
		})
	}

	if !(&StatusError{StatusCode: 503}).Retryable() {
		t.Fatal("503 must report as retryable")
	}

	if (&StatusError{StatusCode: 408}).Retryable() {
		t.Fatal("408 must not report as retryable")
	}
}
