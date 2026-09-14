package httpx

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxDurationHalf is the largest duration that can be doubled without
// overflowing time.Duration's int64.
const maxDurationHalf = time.Duration(1) << 61

// isRetryableStatus reports whether a response status is worth trying
// again: 429 Too Many Requests, or any 5xx.
//
// Every 4xx other than 429 is permanent by definition — the request as sent
// is what the server objects to, so re-sending it unchanged can only annoy
// it. That includes 408 Request Timeout, which is the one 4xx a retry could
// arguably help: T-020's acceptance criteria say "on 429/5xx only; never on
// 4xx", and treating 408 as an unwritten third case would be exactly the
// kind of divergence between two reasonable implementations that AGENT.md
// §12 asks not to guess at. See DEC-058.
func isRetryableStatus(code int) bool {
	if code == http.StatusTooManyRequests {
		return true
	}

	return code >= 500 && code <= 599
}

// parseRetryAfter interprets a Retry-After header value in either form RFC
// 9110 allows — delta-seconds ("120") or an HTTP-date ("Wed, 21 Oct 2026
// 07:28:00 GMT") — relative to now.
//
// It reports ok=false for an absent, empty, or malformed value, which the
// caller treats as "no advice given" and falls back to exponential backoff.
// A value in the past, or a negative delta, is not malformed: the server is
// saying "now", so it yields a zero delay rather than a negative one.
func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}

	if secs, err := strconv.Atoi(value); err == nil {
		if secs <= 0 {
			return 0, true
		}

		return time.Duration(secs) * time.Second, true
	}

	if when, err := http.ParseTime(value); err == nil {
		d := when.Sub(now)
		if d <= 0 {
			return 0, true
		}

		return d, true
	}

	return 0, false
}

// backoffFor returns the exponential backoff delay to wait after attempt
// number n (1 for the first attempt): base, base*2, base*4, ... clamped to
// maxDelay. The doubling stops as soon as the cap is reached, so a large n
// saturates rather than overflowing into a negative duration.
func backoffFor(n int, base, maxDelay time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}

	delay := base

	for i := 1; i < n; i++ {
		if delay >= maxDelay || delay > maxDurationHalf {
			return maxDelay
		}

		delay *= 2
	}

	if delay > maxDelay {
		return maxDelay
	}

	return delay
}
