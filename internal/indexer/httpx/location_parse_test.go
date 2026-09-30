package httpx

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// A redirect whose Location header will not parse fails inside net/http,
// before any CheckRedirect call, with an error that quotes the whole header
// ("failed to parse Location header ..."). httpx replaces that cause with a
// fixed one naming only the request host (T-9049, DEC-146), so a Location's
// userinfo, path and query — including the user's own Basic credentials
// echoed back by the server — never reach an error string or a log line.

// locationFrom is the endpoint the user configured, with their own
// credentials in it: the server echoing them into a broken Location is the
// case the redactor cannot catch, since it scrubs only the api_key and the
// cookie.
const locationFrom = "https://alice:s3cret@example.org/api/start"

// malformedLocations each fail url.Parse inside net/http, and each carries
// userinfo, a path, a query, or all three.
var malformedLocations = map[string]string{
	"space in the host":           "https://alice:s3cret@exa mple.org/vault/secretpath?tok=q1value",
	"bad escape in the password":  "https://alice:s3%zzcret@example.org/vault/secretpath?tok=q1value",
	"bad escape in the path":      "https://alice:s3cret@example.org/vault/secretpath%zz?tok=q1value",
	"non-numeric port":            "https://alice:s3cret@example.org:eighty/vault/secretpath?tok=q1value",
	"control character":           "https://alice:s3cret@example.org/vault/secretpath?tok=q1value\x01",
	"other user, bad escape":      "https://mallory:hunter2%zz@example.org/vault/secretpath?tok=q1value",
	"relative, bad escape":        "/vault/secretpath%zz?tok=q1value",
	"scheme-relative, bad escape": "//alice:s3cret@example.org/vault/secretpath%zz?tok=q1value",
}

// locationLeaks are the pieces of the configured endpoint and of every
// malformed Location that must never be repeated.
func locationLeaks() []string {
	return []string{
		"alice", "s3cret", base64.StdEncoding.EncodeToString([]byte("alice:s3cret")),
		"mallory", "hunter2", base64.StdEncoding.EncodeToString([]byte("mallory:hunter2")),
		"@", "vault", "secretpath", "tok=", "q1value", "%zz", "eighty", "exa mple", "Location header \"",
	}
}

// brokenLocationHop answers the configured address with retryFirst 503s,
// then with a redirect to location. Any other request is a test failure:
// net/http must never issue the hop.
type brokenLocationHop struct {
	location   string
	retryFirst int

	mu    sync.Mutex
	calls int
	stray []string
}

func (h *brokenLocationHop) RoundTrip(r *http.Request) (*http.Response, error) {
	h.mu.Lock()
	h.calls++
	call := h.calls

	if r.URL.Path != "/api/start" {
		h.stray = append(h.stray, r.URL.Path)
	}
	h.mu.Unlock()

	status, header := http.StatusFound, http.Header{"Location": []string{h.location}}
	if call <= h.retryFirst {
		status, header = http.StatusServiceUnavailable, http.Header{}
	}

	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    r,
	}, nil
}

func (h *brokenLocationHop) seen() (calls int, stray []string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.calls, append([]string(nil), h.stray...)
}

// TestUnparseableRedirectLocationIsNeverEchoed drives every malformed
// Location through both client shapes, after a retried 503 so the client's
// debug log has a line to inspect: the error is classifiable, names the host,
// and neither it nor the log repeats any part of the Location or the
// credentials.
func TestUnparseableRedirectLocationIsNeverEchoed(t *testing.T) {
	t.Parallel()

	for clientName, base := range endToEndClients {
		for name, location := range malformedLocations {
			t.Run(clientName+"/"+name, func(t *testing.T) {
				t.Parallel()

				var logs bytes.Buffer

				var logMu sync.Mutex

				transport := &brokenLocationHop{location: location, retryFirst: 1}
				cfg := base
				cfg.MinHostInterval = -1
				cfg.MaxAttempts = 3
				cfg.Clock = newFakeClock()
				cfg.Transport = transport
				cfg.Logger = slog.New(slog.NewTextHandler(&lockedWriter{w: &logs, mu: &logMu}, &slog.HandlerOptions{Level: slog.LevelDebug}))

				_, err := New(cfg).Get(testContext(t), locationFrom, nil)
				if !errors.Is(err, ErrRedirectLocationInvalid) {
					t.Fatalf("Get = %v, want ErrRedirectLocationInvalid", err)
				}

				if !strings.Contains(err.Error(), "example.org") {
					t.Errorf("error %q does not name the request host", err)
				}

				calls, stray := transport.seen()
				if calls != 2 || len(stray) != 0 {
					t.Fatalf("calls = %d, stray = %v; want the 503 and the redirect, no hop", calls, stray)
				}

				logMu.Lock()
				logged := logs.String()
				logMu.Unlock()

				if !strings.Contains(logged, "retrying") {
					t.Fatalf("log = %q, want the retry line this test inspects", logged)
				}

				for _, text := range []string{err.Error(), logged} {
					for _, leak := range locationLeaks() {
						if strings.Contains(text, leak) {
							t.Errorf("%q repeats %q", text, leak)
						}
					}
				}
			})
		}
	}
}

// TestUnparseableRedirectLocationCauseCarriesNoText pins the cause chain: a
// caller that reaches the cause through errors.Unwrap and formats it prints
// nothing of the Location either, and the sentinel is the replacement, not
// net/http's text wrapped.
func TestUnparseableRedirectLocationCauseCarriesNoText(t *testing.T) {
	t.Parallel()

	transport := &brokenLocationHop{location: malformedLocations["space in the host"]}

	_, err := New(Config{MinHostInterval: -1, MaxAttempts: 1, Transport: transport}).Get(testContext(t), locationFrom, nil)
	if err == nil {
		t.Fatal("Get succeeded, want the Location failure")
	}

	for cause := err; cause != nil; cause = errors.Unwrap(cause) {
		for _, leak := range locationLeaks() {
			if strings.Contains(cause.Error(), leak) {
				t.Errorf("cause %q repeats %q", cause, leak)
			}
		}
	}
}

// TestWithoutLocationEcho pins the guard itself: only net/http's own
// Location parse failure is replaced, with the host given and nothing else;
// every other cause passes through untouched, so its text and its
// errors.Is identity stay what they were.
func TestWithoutLocationEcho(t *testing.T) {
	t.Parallel()

	parseFailure := errors.New(`failed to parse Location header "https://alice:s3cret@exa mple.org/vault": parse error`)
	reset := errors.New("connection reset by peer")

	for name, tc := range map[string]struct {
		cause    error
		replaced bool
	}{
		"net/http's Location parse failure":    {parseFailure, true},
		"a transport error":                    {reset, false},
		"a redirect refusal":                   {ErrUserinfoRedirect, false},
		"the phrase not at the start":          {errors.New("proxy said: failed to parse Location header \"x\""), false},
		"a different parse failure":            {errors.New(`failed to parse Content-Type header "x"`), false},
		"a cause wrapping the parse failure":   {errors.Join(reset, parseFailure), false},
		"lowercase location is someone else's": {errors.New(`failed to parse location header "x"`), false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := withoutLocationEcho(tc.cause, "example.org")

			if !tc.replaced {
				if !errors.Is(got, tc.cause) || got.Error() != tc.cause.Error() {
					t.Fatalf("withoutLocationEcho(%q) = %q, want the cause unchanged", tc.cause, got)
				}

				return
			}

			if !errors.Is(got, ErrRedirectLocationInvalid) {
				t.Fatalf("withoutLocationEcho = %v, want ErrRedirectLocationInvalid", got)
			}

			want := "httpx: " + ErrRedirectLocationInvalid.Error() + " (at example.org)"
			if got.Error() != want {
				t.Fatalf("withoutLocationEcho = %q, want %q", got, want)
			}
		})
	}
}

// TestOtherTransportErrorsAreUnchanged checks a failure that is not the
// Location parse keeps its cause text and identity end to end.
func TestOtherTransportErrorsAreUnchanged(t *testing.T) {
	t.Parallel()

	transport := &sameOriginHop{location: "/api/final", finalErr: io.ErrUnexpectedEOF}

	_, err := New(Config{MinHostInterval: -1, MaxAttempts: 1, Transport: transport}).Get(testContext(t), sameOriginFrom, nil)
	if !errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, ErrRedirectLocationInvalid) {
		t.Fatalf("Get = %v, want io.ErrUnexpectedEOF and not ErrRedirectLocationInvalid", err)
	}

	if !strings.Contains(err.Error(), io.ErrUnexpectedEOF.Error()) {
		t.Fatalf("error %q lost the transport cause's text", err)
	}
}
