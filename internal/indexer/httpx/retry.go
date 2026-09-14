package httpx

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxDurationHalf is the largest duration that can be doubled without
// overflowing time.Duration's int64.
const maxDurationHalf = time.Duration(1) << 61

// maxDuration is the longest representable time.Duration (~292 years).
// An absurd Retry-After saturates here instead of wrapping negative.
const maxDuration = time.Duration(math.MaxInt64)

// maxRetryAfterSeconds is the largest delta-seconds value that still fits
// in a time.Duration once multiplied by time.Second. Anything above it
// would wrap: 31536000000 ("a thousand years") becomes -1488191h, which
// then reads as *shorter* than MaxRetryAfter and is slept for no time at
// all — MaxAttempts back-to-back requests at a server that just asked to be
// left alone, the opposite of what DEC-059 says happens.
const maxRetryAfterSeconds = int64(math.MaxInt64) / int64(time.Second)

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
//
// The returned duration is never negative. A delta-seconds value too large
// for a time.Duration saturates at maxDuration rather than wrapping, so an
// absurd wait stays absurd all the way to retryDelay, which stops the
// attempt loop on it (DEC-059). time.Time.Sub already saturates the same
// way, so the HTTP-date form needs no separate guard.
func parseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}

	// ErrRange is accepted alongside a clean parse: a run of digits too
	// long for an int64 is still a delta-seconds value, just an enormous
	// one, and ParseInt clamps it to MaxInt64 (or MinInt64) which lands on
	// the saturating branches below. Only a syntactically malformed value
	// falls through to the HTTP-date form.
	if secs, err := strconv.ParseInt(value, 10, 64); err == nil || errors.Is(err, strconv.ErrRange) {
		if secs <= 0 {
			return 0, true
		}

		if secs > maxRetryAfterSeconds {
			return maxDuration, true
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
