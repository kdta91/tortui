package httpx

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// statusTransport answers every request with the given status and header. It
// never touches the network (AGENT.md §6.7).
type statusTransport struct {
	status int
	header http.Header
}

func (s statusTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: s.status,
		Status:     http.StatusText(s.status),
		Header:     s.header,
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    r,
	}, nil
}

// TestRetryLimitErrorsCarryTheHttpxPrefixOnce pins the whole text of both
// "not retrying" errors: one "httpx:" prefix at the front, the reason, then
// the status error's own text without a second prefix (T-9119). The status
// error is still reachable with errors.As.
func TestRetryLimitErrorsCarryTheHttpxPrefixOnce(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		cfg    Config
		header http.Header
		text   string
	}{
		"wait longer than the limit": {
			cfg:    Config{MaxRetryAfter: 30 * time.Second},
			header: http.Header{"Retry-After": []string{"3600"}},
			text: "httpx: not retrying, the server asked to wait 1h0m0s which is longer than the 30s limit: " +
				"GET example.org: HTTP 429 Too Many Requests (1 attempt, server asked to retry after 1h0m0s)",
		},
		"wait past the deadline": {
			cfg:    Config{MaxRetryAfter: time.Minute, RequestTimeout: 5 * time.Second},
			header: http.Header{"Retry-After": []string{"20"}},
			text: "httpx: not retrying, a 20s wait would outlast the request deadline: " +
				"GET example.org: HTTP 429 Too Many Requests (1 attempt, server asked to retry after 20s)",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := tc.cfg
			cfg.MinHostInterval = -1
			cfg.MaxAttempts = 4
			cfg.Clock = newFakeClock()
			cfg.Transport = statusTransport{status: http.StatusTooManyRequests, header: tc.header}

			_, err := New(cfg).Get(testContext(t), "https://example.org/a", nil)

			var statusErr *StatusError
			if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusTooManyRequests {
				t.Fatalf("Get = %v, want a 429 *StatusError in the chain", err)
			}

			if err.Error() != tc.text {
				t.Fatalf("Get error = %q, want %q", err, tc.text)
			}

			if n := strings.Count(err.Error(), "httpx:"); n != 1 {
				t.Fatalf("error %q carries the httpx prefix %d times, want once", err, n)
			}
		})
	}
}
