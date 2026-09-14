package httpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/logging"
)

// assertNoCredentialLeak is the assertion this whole file exists for. Text
// that is about to be shown to a user or written to their log file must
// contain neither credential, no trace of the query parameter the api_key
// travels in, and no absolute URL at all — a URL is the vehicle the key
// rides in, so its absence is the property worth asserting rather than the
// key's absence alone.
func assertNoCredentialLeak(t *testing.T, what, text string) {
	t.Helper()

	if strings.Contains(text, testAPIKey) {
		t.Fatalf("%s leaked the api key verbatim: %q", what, text)
	}

	if strings.Contains(text, testSessionValue) {
		t.Fatalf("%s leaked the session value verbatim: %q", what, text)
	}

	if strings.Contains(text, "zzzz1111yyyy2222") {
		t.Fatalf("%s leaked part of the session value: %q", what, text)
	}

	if strings.Contains(strings.ToLower(text), DefaultAPIKeyParam+"=") {
		t.Fatalf("%s contains an %s= parameter: %q", what, DefaultAPIKeyParam, text)
	}

	if strings.Contains(text, "http://") || strings.Contains(text, "https://") {
		t.Fatalf("%s contains an absolute URL, which is how a key reaches a log line: %q", what, text)
	}
}

// leakCases builds one error per failure mode the client can produce, each
// from a client configured with the user's credentials.
func leakCases(t *testing.T) map[string]error {
	t.Helper()

	ctx := testContext(t)
	errs := make(map[string]error)

	notFound := statusServer(t, http.StatusNotFound)
	errs["4xx status"] = mustFail(t, newLeakClient(Config{}), ctx, notFound.URL+"/api?t=search")

	serverError := statusServer(t, http.StatusInternalServerError)
	errs["5xx after retries"] = mustFail(t, newLeakClient(Config{MaxAttempts: 2, Clock: newFakeClock()}), ctx, serverError.URL+"/api?t=search")

	throttled := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "9000")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	errs["retry-after too long"] = mustFail(t, newLeakClient(Config{Clock: newFakeClock()}), ctx, throttled.URL+"/api")

	oversized := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if _, err := io.WriteString(w, strings.Repeat("x", 512)); err != nil {
			t.Errorf("write body: %v", err)
		}
	})
	errs["body over the cap"] = mustFail(t, newLeakClient(Config{MaxBodyBytes: 32}), ctx, oversized.URL+"/api")

	elsewhere := newServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	redirector := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/collect", http.StatusFound)
	})
	errs["cross-host redirect"] = mustFail(t, newLeakClient(Config{}), ctx, redirector.URL+"/api")

	// A dead endpoint: net/http fails the dial and hands back a *url.Error
	// whose text is the whole URL, query string included.
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	deadURL := dead.URL
	dead.Close()
	errs["dial failure"] = mustFail(t, newLeakClient(Config{}), ctx, deadURL+"/api?t=search")

	errs["unparseable url"] = mustFail(t, newLeakClient(Config{}), ctx, "http://feed.example.org/\x7f/api")

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newLeakClient(Config{}).Get(cancelled, notFound.URL+"/api", nil)
	errs["cancelled context"] = err

	return errs
}

// newLeakClient builds a client carrying the test credentials, with the
// per-host spacing off so the tests do not wait on it.
func newLeakClient(cfg Config) *Client {
	cfg.Credentials = testCredentials()
	if cfg.MinHostInterval == 0 {
		cfg.MinHostInterval = -1
	}

	return New(cfg)
}

func mustFail(t *testing.T, client *Client, ctx context.Context, target string) error {
	t.Helper()

	_, err := client.Get(ctx, target, nil)
	if err == nil {
		t.Fatalf("request to %s unexpectedly succeeded", target)
	}

	return err
}

func TestNoErrorEverCarriesACredential(t *testing.T) {
	t.Parallel()

	for name, err := range leakCases(t) {
		if err == nil {
			t.Fatalf("%s: no error produced", name)
		}

		assertNoCredentialLeak(t, name+" error text", err.Error())
		assertNoCredentialLeak(t, name+" formatted with %v", fmt.Sprintf("%v", err))
		assertNoCredentialLeak(t, name+" formatted with %+v", fmt.Sprintf("%+v", err))

		// Unwrapping is the interesting case: a caller that walks the chain
		// and prints a cause must not find a URL down there either, which is
		// exactly what a surviving *url.Error would give it.
		for cause := errors.Unwrap(err); cause != nil; cause = errors.Unwrap(cause) {
			assertNoCredentialLeak(t, name+" unwrapped cause", cause.Error())
		}
	}
}

func TestNoCredentialReachesTheLogFile(t *testing.T) {
	// Not parallel: logging.New installs the process-wide slog default.
	path := filepath.Join(t.TempDir(), "tortui.log")

	logger, closer, err := logging.New(logging.Options{File: path, Level: "debug"})
	if err != nil {
		t.Fatalf("build logger: %v", err)
	}

	server := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	})

	client := New(Config{
		MinHostInterval: -1,
		MaxAttempts:     3,
		Clock:           newFakeClock(),
		Credentials:     testCredentials(),
		Logger:          logger,
	})

	_, reqErr := client.Get(testContext(t), server.URL+"/api?t=search", nil)
	if reqErr == nil {
		t.Fatal("want an error from a server that always answers 429")
	}

	// Log it every way an adapter plausibly would.
	logger.Error("indexer search failed", "error", reqErr)
	logger.Error("indexer search failed", "err", reqErr)
	logger.Info("request detail", "detail", reqErr.Error())
	logger.Debug("client configuration", "credentials", testCredentials())

	if err := closer.Close(); err != nil {
		t.Fatalf("close log: %v", err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	if len(contents) == 0 {
		t.Fatal("log file is empty, so this test proves nothing")
	}

	assertNoCredentialLeak(t, "the log file", string(contents))

	if !strings.Contains(string(contents), "indexer search failed") {
		t.Fatalf("log file does not contain the lines under test:\n%s", contents)
	}
}

func TestURLErrorIsStrippedFromTheCauseChain(t *testing.T) {
	t.Parallel()

	// net/http hands back a *url.Error for essentially every failed
	// request, and its Error() prints the full URL — api_key and all. This
	// stages one directly to prove the layer is removed rather than merely
	// wrapped.
	boom := errors.New("connection reset by peer")
	leaky := &url.Error{
		Op:  "Get",
		URL: "https://feed.example.org/api?t=search&" + DefaultAPIKeyParam + "=" + testAPIKey,
		Err: boom,
	}

	if !strings.Contains(leaky.Error(), testAPIKey) {
		t.Fatal("precondition failed: the staged *url.Error should print the key")
	}

	client := New(Config{
		MinHostInterval: -1,
		MaxAttempts:     1,
		Credentials:     testCredentials(),
		Transport:       roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, leaky }),
	})

	_, err := client.Get(testContext(t), "https://feed.example.org/api", nil)
	if err == nil {
		t.Fatal("want an error")
	}

	assertNoCredentialLeak(t, "the returned error", err.Error())

	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want the underlying cause still reachable with errors.Is", err)
	}

	var stillThere *url.Error
	if errors.As(err, &stillThere) {
		t.Fatalf("a *url.Error survived in the chain: %q", stillThere.Error())
	}
}

func TestRedactorScrubsEveryConfiguredValue(t *testing.T) {
	t.Parallel()

	r := newRedactor("", "first-secret", "second-secret", "")

	if len(r.secrets) != 2 {
		t.Fatalf("kept %d secrets, want 2: empty values are not secrets", len(r.secrets))
	}

	got := r.scrub("saw first-secret twice: first-secret, and second-secret once")
	if strings.Contains(got, "first-secret") || strings.Contains(got, "second-secret") {
		t.Fatalf("scrub left a secret behind: %q", got)
	}

	if strings.Count(got, redactedPlaceholder) != 3 {
		t.Fatalf("scrub = %q, want three replacements", got)
	}
}

func TestSafefScrubsAndKeepsTheCause(t *testing.T) {
	t.Parallel()

	cause := errors.New("underlying")
	r := newRedactor("top-secret")

	err := r.safef(cause, "httpx: something went wrong with top-secret")

	if strings.Contains(err.Error(), "top-secret") {
		t.Fatalf("error = %q, want the secret scrubbed", err)
	}

	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want the cause reachable", err)
	}
}

func TestUnwrapURLError(t *testing.T) {
	t.Parallel()

	inner := errors.New("inner")

	nested := &url.Error{
		Op:  "Get",
		URL: "https://feed.example.org/a",
		Err: &url.Error{Op: "Get", URL: "https://feed.example.org/b", Err: inner},
	}

	if got := unwrapURLError(nested); !errors.Is(got, inner) {
		t.Fatalf("unwrapURLError = %v, want the innermost cause", got)
	}

	var leftover *url.Error
	if errors.As(unwrapURLError(nested), &leftover) {
		t.Fatalf("a *url.Error survived: %v", leftover)
	}

	if got := unwrapURLError(inner); !errors.Is(got, inner) {
		t.Fatalf("unwrapURLError on a plain error = %v, want it unchanged", got)
	}

	if got := unwrapURLError(nil); got != nil {
		t.Fatalf("unwrapURLError(nil) = %v, want nil", got)
	}

	empty := unwrapURLError(&url.Error{Op: "Get", URL: "https://feed.example.org/x"})
	if empty == nil || strings.Contains(empty.Error(), "feed.example.org") {
		t.Fatalf("unwrapURLError on a *url.Error with no cause = %v, want a URL-free stand-in", empty)
	}
}

func TestHostOf(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "host and port", in: "https://Feed.Example.Org:8443/api?t=search", want: "feed.example.org:8443"},
		{name: "userinfo is dropped", in: "https://user:pass@feed.example.org/api", want: "feed.example.org"},
		{name: "no host", in: "/relative/path", want: "unknown host"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			parsed, err := url.Parse(tc.in)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}

			if got := hostOf(parsed); got != tc.want {
				t.Fatalf("hostOf(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	if got := hostOf(nil); got != "unknown host" {
		t.Fatalf("hostOf(nil) = %q, want %q", got, "unknown host")
	}
}

func TestRateLimitWaitErrorIsSafe(t *testing.T) {
	t.Parallel()

	server := statusServer(t, http.StatusOK)

	client := New(Config{
		MinHostInterval: time.Hour,
		Credentials:     testCredentials(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if _, err := client.Get(ctx, server.URL+"/api?t=search", nil); err != nil {
		t.Fatalf("first request: %v", err)
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	_, err := client.Get(ctx, server.URL+"/api?t=search", nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}

	assertNoCredentialLeak(t, "the rate-limit wait error", err.Error())
}
