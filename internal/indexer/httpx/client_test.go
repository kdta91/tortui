package httpx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// testAPIKey and testSessionValue stand in for what a user pastes into
// config.toml from their own account. Both are invented and inert.
const (
	testAPIKey       = "opaque-value-from-the-users-own-account-0192837465"
	testSessionValue = "sid=zzzz1111yyyy2222; uid=4242"
)

func testCredentials() Credentials {
	creds := Credentials{}
	creds.APIKey = testAPIKey
	creds.CookieHeader = testSessionValue

	return creds
}

func TestUserAgentIsConfigurable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		configure string
		header    http.Header
		want      string
	}{
		{name: "default", want: DefaultUserAgent},
		{name: "configured", configure: "tortui-test/9.9", want: "tortui-test/9.9"},
		{
			name:   "per-request header wins",
			header: http.Header{"User-Agent": []string{"caller/1.0"}},
			want:   "caller/1.0",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var got atomic.Value

			server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
				got.Store(r.Header.Get("User-Agent"))
				w.WriteHeader(http.StatusOK)
			})

			client := New(Config{UserAgent: tc.configure, MinHostInterval: -1})

			target := server.URL

			if _, err := client.Do(testContext(t), Request{URL: target, Header: tc.header}); err != nil {
				t.Fatalf("do: %v", err)
			}

			if got.Load() != tc.want {
				t.Fatalf("User-Agent = %q, want %q", got.Load(), tc.want)
			}
		})
	}
}

func TestInjectsUserSuppliedCredentials(t *testing.T) {
	t.Parallel()

	var (
		gotKey     atomic.Value
		gotSession atomic.Value
		gotQuery   atomic.Value
	)

	server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotKey.Store(r.URL.Query().Get(DefaultAPIKeyParam))
		gotSession.Store(r.Header.Get(headerCookie))
		gotQuery.Store(r.URL.RawQuery)
		w.WriteHeader(http.StatusOK)
	})

	client := New(Config{MinHostInterval: -1, Credentials: testCredentials()})

	target := server.URL + "/api?t=search&cat=5000"

	req := Request{
		URL:   target,
		Query: url.Values{"q": []string{"debian iso"}},
	}

	if _, err := client.Do(testContext(t), req); err != nil {
		t.Fatalf("do: %v", err)
	}

	if gotKey.Load() != testAPIKey {
		t.Fatalf("apikey parameter = %q, want the configured value", gotKey.Load())
	}

	if gotSession.Load() != testSessionValue {
		t.Fatalf("session header = %q, want the configured value", gotSession.Load())
	}

	raw, ok := gotQuery.Load().(string)
	if !ok {
		t.Fatal("no query recorded")
	}

	values, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("parse recorded query: %v", err)
	}

	if values.Get("t") != "search" || values.Get("cat") != "5000" {
		t.Fatalf("query = %q, want the URL's own parameters preserved", raw)
	}

	if values.Get("q") != "debian iso" {
		t.Fatalf("query = %q, want Request.Query merged in", raw)
	}
}

func TestInjectsNothingWhenNoCredentialsAreConfigured(t *testing.T) {
	t.Parallel()

	var (
		gotQuery   atomic.Value
		gotSession atomic.Value
	)

	server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery.Store(r.URL.RawQuery)
		gotSession.Store(r.Header.Get(headerCookie))
		w.WriteHeader(http.StatusOK)
	})

	client := New(Config{MinHostInterval: -1})

	if _, err := client.Get(testContext(t), server.URL+"/api?t=caps", nil); err != nil {
		t.Fatalf("get: %v", err)
	}

	if got := gotQuery.Load(); got != "t=caps" {
		t.Fatalf("query = %q, want only the caller's own parameters", got)
	}

	if got := gotSession.Load(); got != "" {
		t.Fatalf("session header = %q, want none", got)
	}
}

func TestAPIKeyParameterNameIsConfigurable(t *testing.T) {
	t.Parallel()

	var got atomic.Value

	server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		got.Store(r.URL.Query().Get("passkey"))
		w.WriteHeader(http.StatusOK)
	})

	creds := Credentials{}
	creds.APIKey = testAPIKey

	client := New(Config{MinHostInterval: -1, APIKeyParam: "passkey", Credentials: creds})

	if _, err := client.Get(testContext(t), server.URL, nil); err != nil {
		t.Fatalf("get: %v", err)
	}

	if got.Load() != testAPIKey {
		t.Fatalf("passkey parameter = %q, want the configured value", got.Load())
	}
}

func TestBodyUnderTheCapIsReturnedWhole(t *testing.T) {
	t.Parallel()

	payload := strings.Repeat("x", 64)

	server := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if _, err := io.WriteString(w, payload); err != nil {
			t.Errorf("write body: %v", err)
		}
	})

	client := New(Config{MinHostInterval: -1, MaxBodyBytes: 64})

	res, err := client.Get(testContext(t), server.URL, nil)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if string(res.Body) != payload {
		t.Fatalf("body length = %d, want %d (a body exactly at the cap is fine)", len(res.Body), len(payload))
	}
}

func TestBodyOverTheCapIsRefusedNotTruncated(t *testing.T) {
	t.Parallel()

	payload := strings.Repeat("x", 100)

	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{
			name: "declared content length",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				// Small single write: net/http sets Content-Length itself,
				// so this exercises the pre-read refusal.
				if _, err := io.WriteString(w, payload); err != nil {
					panic(err)
				}
			},
		},
		{
			name: "chunked with no content length",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				flusher, ok := w.(http.Flusher)
				if !ok {
					panic("test server response writer is not a Flusher")
				}

				for i := 0; i < 10; i++ {
					if _, err := io.WriteString(w, strings.Repeat("x", 10)); err != nil {
						panic(err)
					}

					flusher.Flush()
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := newServer(t, tc.handler)
			client := New(Config{MinHostInterval: -1, MaxBodyBytes: 64})

			res, err := client.Get(testContext(t), server.URL, nil)
			if !errors.Is(err, ErrBodyTooLarge) {
				t.Fatalf("error = %v, want ErrBodyTooLarge", err)
			}

			if res != nil {
				t.Fatalf("response = %+v, want nil: a body over the cap must not come back truncated", res)
			}

			if strings.Contains(err.Error(), payload[:20]) {
				t.Fatalf("error = %q, want no body content echoed back", err)
			}
		})
	}
}

func TestBodyCapCatchesAServerLyingAboutContentLength(t *testing.T) {
	t.Parallel()

	// A real server cannot send more than it declares — net/http enforces
	// that on the way out — so the lie is staged at the transport instead.
	// This is the case the pre-read Content-Length check cannot catch and
	// the limited read must.
	oversized := bytes.Repeat([]byte("x"), 4096)

	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Status:        "200 OK",
			Header:        http.Header{},
			ContentLength: 8,
			Body:          io.NopCloser(bytes.NewReader(oversized)),
			Request:       r,
		}, nil
	})

	client := New(Config{MinHostInterval: -1, MaxBodyBytes: 64, Transport: transport})

	_, err := client.Get(testContext(t), "https://feed.example.org/api", nil)
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("error = %v, want ErrBodyTooLarge", err)
	}
}

func TestBodyCapCanBeDisabled(t *testing.T) {
	t.Parallel()

	payload := strings.Repeat("x", 4096)

	server := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if _, err := io.WriteString(w, payload); err != nil {
			t.Errorf("write body: %v", err)
		}
	})

	client := New(Config{MinHostInterval: -1, MaxBodyBytes: -1})

	res, err := client.Get(testContext(t), server.URL, nil)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if len(res.Body) != len(payload) {
		t.Fatalf("body length = %d, want %d", len(res.Body), len(payload))
	}
}

func TestReadTimeoutEndsAStalledResponse(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}

		w.WriteHeader(http.StatusOK)
	})

	client := New(Config{
		MinHostInterval: -1,
		ConnectTimeout:  50 * time.Millisecond,
		ReadTimeout:     50 * time.Millisecond,
		MaxAttempts:     1,
	})

	started := time.Now()

	_, err := client.Get(testContext(t), server.URL, nil)
	if err == nil {
		t.Fatal("want a timeout error from a server that never answers")
	}

	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("returned after %s, want the configured read timeout to end it", elapsed)
	}

	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("error = %v, want a timeout error", err)
	}
}

func TestTransportCarriesTheConfiguredTimeouts(t *testing.T) {
	t.Parallel()

	// The connect timeout lands on the dialer, which a unit test cannot
	// exercise behaviourally without a host that swallows connections
	// (AGENT.md §6.7 bars the network), so it is asserted where it is set.
	if got := newDialer(3 * time.Second).Timeout; got != 3*time.Second {
		t.Fatalf("dialer timeout = %s, want 3s", got)
	}

	transport := newTransport(3*time.Second, 7*time.Second)

	if transport.TLSHandshakeTimeout != 3*time.Second {
		t.Fatalf("TLS handshake timeout = %s, want 3s", transport.TLSHandshakeTimeout)
	}

	if transport.ResponseHeaderTimeout != 7*time.Second {
		t.Fatalf("response header timeout = %s, want 7s", transport.ResponseHeaderTimeout)
	}

	if transport.DialContext == nil {
		t.Fatal("transport has no DialContext, so the connect timeout would not apply")
	}

	client := New(Config{ConnectTimeout: 3 * time.Second, ReadTimeout: 7 * time.Second})

	built, ok := client.http.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("client transport is %T, want *http.Transport", client.http.Transport)
	}

	if built.ResponseHeaderTimeout != 7*time.Second || built.TLSHandshakeTimeout != 3*time.Second {
		t.Fatalf("client transport timeouts = (%s, %s), want (3s, 7s)", built.TLSHandshakeTimeout, built.ResponseHeaderTimeout)
	}
}

func TestSameHostRedirectIsFollowed(t *testing.T) {
	t.Parallel()

	server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/moved" {
			http.Redirect(w, r, "/final", http.StatusFound)

			return
		}

		if _, err := io.WriteString(w, "arrived"); err != nil {
			t.Errorf("write body: %v", err)
		}
	})

	client := New(Config{MinHostInterval: -1})

	res, err := client.Get(testContext(t), server.URL+"/moved", nil)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if string(res.Body) != "arrived" {
		t.Fatalf("body = %q, want %q", res.Body, "arrived")
	}
}

func TestCrossHostRedirectIsRefused(t *testing.T) {
	t.Parallel()

	elsewhere := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/collect", http.StatusFound)
	})

	client := New(Config{MinHostInterval: -1, Credentials: testCredentials()})

	_, err := client.Get(testContext(t), server.URL, nil)
	if !errors.Is(err, ErrCrossHostRedirect) {
		t.Fatalf("error = %v, want ErrCrossHostRedirect", err)
	}

	if elsewhere.count() != 0 {
		t.Fatalf("the redirect target was contacted %d times, want 0", elsewhere.count())
	}
}

func TestSchemeDowngradeRedirectIsRefused(t *testing.T) {
	t.Parallel()

	// Two real servers can never share one host:port across schemes, so the
	// downgrade is staged at the transport: the https request is answered
	// with a 302 whose Location keeps the host *and the query* and changes
	// only the scheme, which is what an apex-to-www or path-rewrite
	// redirect looks like. net/http's own redirect machinery then calls
	// CheckRedirect exactly as it would against a real server.
	var schemes []string

	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		schemes = append(schemes, r.URL.Scheme)

		cleartext := *r.URL
		cleartext.Scheme = "http"

		return &http.Response{
			StatusCode: http.StatusFound,
			Status:     "302 Found",
			Header:     http.Header{"Location": []string{cleartext.String()}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    r,
		}, nil
	})

	client := New(Config{MinHostInterval: -1, Transport: transport, Credentials: testCredentials()})

	_, err := client.Get(testContext(t), "https://feed.example.org/api", nil)
	if !errors.Is(err, ErrInsecureRedirect) {
		t.Fatalf("error = %v, want ErrInsecureRedirect", err)
	}

	if len(schemes) != 1 || schemes[0] != "https" {
		t.Fatalf("schemes requested = %v, want exactly one https request and no cleartext one", schemes)
	}

	if strings.Contains(err.Error(), testAPIKey) || strings.Contains(err.Error(), "apikey=") {
		t.Fatalf("the refusal error carries the credential: %v", err)
	}
}

func TestCheckRedirectSchemeAndHostRules(t *testing.T) {
	t.Parallel()

	mustParse := func(raw string) *url.URL {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}

		return u
	}

	cases := []struct {
		name string
		from string
		to   string
		want error
	}{
		{name: "same host same scheme", from: "https://feed.example.org/api", to: "https://feed.example.org/final"},
		{name: "http stays http", from: "http://feed.example.org/api", to: "http://feed.example.org/final"},
		{name: "upgrade to https is followed", from: "http://feed.example.org/api", to: "https://feed.example.org/api"},
		{name: "downgrade to http is refused", from: "https://feed.example.org/api?apikey=k", to: "http://feed.example.org/api?apikey=k", want: ErrInsecureRedirect},
		{name: "scheme case is ignored", from: "HTTPS://feed.example.org/api", to: "https://feed.example.org/api"},
		{name: "different host is refused", from: "https://feed.example.org/api", to: "https://other.example.org/api", want: ErrCrossHostRedirect},
		{name: "different port is refused", from: "https://feed.example.org/api", to: "https://feed.example.org:8443/api", want: ErrCrossHostRedirect},
		{name: "host check wins over scheme check", from: "https://feed.example.org/api", to: "http://other.example.org/api", want: ErrCrossHostRedirect},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			origin := &http.Request{URL: mustParse(tc.from)}
			next := &http.Request{URL: mustParse(tc.to)}

			err := checkRedirect(next, []*http.Request{origin})
			if tc.want == nil {
				if err != nil {
					t.Fatalf("checkRedirect(%s -> %s) = %v, want nil", tc.from, tc.to, err)
				}

				return
			}

			if !errors.Is(err, tc.want) {
				t.Fatalf("checkRedirect(%s -> %s) = %v, want %v", tc.from, tc.to, err, tc.want)
			}
		})
	}
}

func TestRedirectChainIsBounded(t *testing.T) {
	t.Parallel()

	server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/again", http.StatusFound)
	})

	client := New(Config{MinHostInterval: -1})

	_, err := client.Get(testContext(t), server.URL, nil)
	if !errors.Is(err, ErrTooManyRedirects) {
		t.Fatalf("error = %v, want ErrTooManyRedirects", err)
	}
}

func TestRequestURLValidation(t *testing.T) {
	t.Parallel()

	client := New(Config{MinHostInterval: -1})
	ctx := testContext(t)

	cases := []struct {
		name string
		url  string
		want error
	}{
		{name: "empty", url: "", want: ErrURLEmpty},
		{name: "whitespace", url: "   ", want: ErrURLEmpty},
		{name: "unparseable", url: "http://feed.example.org/\x7f\x00", want: ErrURLInvalid},
		{name: "wrong scheme", url: "ftp://feed.example.org/feed", want: ErrURLSchemeUnsupported},
		{name: "no scheme", url: "feed.example.org/feed", want: ErrURLSchemeUnsupported},
		{name: "no host", url: "http:///feed", want: ErrURLHostMissing},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := client.Get(ctx, tc.url, nil); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestNonGetMethodIsSentAsWritten(t *testing.T) {
	t.Parallel()

	var got atomic.Value

	server := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		got.Store(r.Method)
		w.WriteHeader(http.StatusOK)
	})

	client := New(Config{MinHostInterval: -1})

	target := server.URL

	if _, err := client.Do(testContext(t), Request{Method: "head", URL: target}); err != nil {
		t.Fatalf("do: %v", err)
	}

	if got.Load() != http.MethodHead {
		t.Fatalf("method = %q, want HEAD (lowercase input is normalised)", got.Load())
	}
}

func TestResponseHeadersAreReturned(t *testing.T) {
	t.Parallel()

	server := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.WriteHeader(http.StatusAccepted)
	})

	client := New(Config{MinHostInterval: -1})

	res, err := client.Get(testContext(t), server.URL, nil)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", res.StatusCode)
	}

	if got := res.Header.Get("Content-Type"); got != "application/rss+xml" {
		t.Fatalf("content type = %q, want application/rss+xml", got)
	}
}

func TestZeroConfigAppliesEveryDocumentedDefault(t *testing.T) {
	t.Parallel()

	client := New(Config{})

	if client.userAgent != DefaultUserAgent {
		t.Fatalf("user agent = %q, want %q", client.userAgent, DefaultUserAgent)
	}

	if client.connectTimeout != DefaultConnectTimeout || client.readTimeout != DefaultReadTimeout {
		t.Fatalf("timeouts = (%s, %s), want (%s, %s)", client.connectTimeout, client.readTimeout, DefaultConnectTimeout, DefaultReadTimeout)
	}

	if client.requestTimeout != DefaultRequestTimeout {
		t.Fatalf("request timeout = %s, want %s", client.requestTimeout, DefaultRequestTimeout)
	}

	if client.limiter.interval != DefaultMinHostInterval {
		t.Fatalf("host interval = %s, want %s", client.limiter.interval, DefaultMinHostInterval)
	}

	if client.maxAttempts != DefaultMaxAttempts {
		t.Fatalf("max attempts = %d, want %d", client.maxAttempts, DefaultMaxAttempts)
	}

	if client.baseBackoff != DefaultBaseBackoff || client.maxBackoff != DefaultMaxBackoff {
		t.Fatalf("backoff = (%s, %s), want (%s, %s)", client.baseBackoff, client.maxBackoff, DefaultBaseBackoff, DefaultMaxBackoff)
	}

	if client.maxRetryAfter != DefaultMaxRetryAfter {
		t.Fatalf("max retry-after = %s, want %s", client.maxRetryAfter, DefaultMaxRetryAfter)
	}

	if client.maxBodyBytes != DefaultMaxBodyBytes {
		t.Fatalf("body cap = %d, want %d", client.maxBodyBytes, DefaultMaxBodyBytes)
	}

	if client.maxBodyBytes != 8*1024*1024 {
		t.Fatalf("body cap = %d, want 8 MB", client.maxBodyBytes)
	}

	if client.apiKeyParam != DefaultAPIKeyParam {
		t.Fatalf("api key parameter = %q, want %q", client.apiKeyParam, DefaultAPIKeyParam)
	}

	if _, ok := client.clock.(systemClock); !ok {
		t.Fatalf("clock = %T, want the system clock", client.clock)
	}

	if client.log() == nil {
		t.Fatal("log() returned nil")
	}
}

func TestMaxBackoffIsRaisedToTheBaseWhenSmaller(t *testing.T) {
	t.Parallel()

	client := New(Config{BaseBackoff: 5 * time.Second, MaxBackoff: time.Second})

	if client.maxBackoff != 5*time.Second {
		t.Fatalf("max backoff = %s, want it raised to the 5s base", client.maxBackoff)
	}
}

func TestCredentialsLogValueHidesBothFields(t *testing.T) {
	t.Parallel()

	got := testCredentials().LogValue().String()

	if strings.Contains(got, testAPIKey) || strings.Contains(got, testSessionValue) {
		t.Fatalf("LogValue() = %q, want no credential in it", got)
	}
}

func TestSystemClockSleep(t *testing.T) {
	t.Parallel()

	ctx := testContext(t)

	if err := (systemClock{}).Sleep(ctx, 0); err != nil {
		t.Fatalf("zero sleep: %v", err)
	}

	if err := (systemClock{}).Sleep(ctx, time.Millisecond); err != nil {
		t.Fatalf("short sleep: %v", err)
	}

	if got := (systemClock{}).Now(); got.IsZero() {
		t.Fatal("Now() returned the zero time")
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	if err := (systemClock{}).Sleep(cancelled, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("zero sleep on a cancelled context = %v, want context.Canceled", err)
	}

	if err := (systemClock{}).Sleep(cancelled, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("long sleep on a cancelled context = %v, want context.Canceled", err)
	}
}

// failingBody is a response body that fails the way a truncated or reset
// connection does, so the read/drain/close error paths are exercised
// without a network.
type failingBody struct {
	reader   io.Reader
	readErr  error
	closeErr error
}

func (b *failingBody) Read(p []byte) (int, error) {
	if b.reader != nil {
		n, err := b.reader.Read(p)
		if err == io.EOF && b.readErr != nil {
			return n, b.readErr
		}

		return n, err
	}

	if b.readErr != nil {
		return 0, b.readErr
	}

	return 0, io.EOF
}

func (b *failingBody) Close() error { return b.closeErr }

func TestInvalidRequestMethodIsReported(t *testing.T) {
	t.Parallel()

	server := statusServer(t, http.StatusOK)
	client := New(Config{MinHostInterval: -1})

	target := server.URL

	_, err := client.Do(testContext(t), Request{Method: "BAD METHOD", URL: target})
	if err == nil {
		t.Fatal("want an error for a method containing a space")
	}

	if !strings.Contains(err.Error(), "cannot build request") {
		t.Fatalf("error = %q, want it to say the request could not be built", err)
	}

	if server.count() != 0 {
		t.Fatalf("server saw %d requests, want 0", server.count())
	}
}

func TestBodyReadFailureIsReported(t *testing.T) {
	t.Parallel()

	readErr := errors.New("unexpected EOF")

	for _, cap := range []int64{64, -1} {
		client := New(Config{
			MinHostInterval: -1,
			MaxBodyBytes:    cap,
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode:    http.StatusOK,
					Header:        http.Header{},
					ContentLength: -1,
					Body:          &failingBody{readErr: readErr},
					Request:       r,
				}, nil
			}),
		})

		_, err := client.Get(testContext(t), "https://feed.example.org/api", nil)
		if !errors.Is(err, readErr) {
			t.Fatalf("cap %d: error = %v, want the read failure reported", cap, err)
		}

		if !strings.Contains(err.Error(), "reading response body") {
			t.Fatalf("cap %d: error = %q, want it to name the failing step", cap, err)
		}
	}
}

func TestDrainAndCloseFailuresAreLoggedNotSwallowed(t *testing.T) {
	t.Parallel()

	var logged strings.Builder

	logger := slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))

	client := New(Config{
		MinHostInterval: -1,
		MaxAttempts:     1,
		Logger:          logger,
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode:    http.StatusNotFound,
				Header:        http.Header{},
				ContentLength: -1,
				Body:          &failingBody{readErr: errors.New("read reset"), closeErr: errors.New("close reset")},
				Request:       r,
			}, nil
		}),
	})

	if _, err := client.Get(testContext(t), "https://feed.example.org/api", nil); err == nil {
		t.Fatal("want the 404 reported")
	}

	out := logged.String()

	if !strings.Contains(out, "discarding an unused response body failed") {
		t.Fatalf("log = %q, want the drain failure recorded", out)
	}

	if !strings.Contains(out, "closing a response body failed") {
		t.Fatalf("log = %q, want the close failure recorded", out)
	}
}

func TestFitsDeadlineWithoutADeadline(t *testing.T) {
	t.Parallel()

	client := New(Config{})

	if !client.fitsDeadline(context.Background(), time.Hour) {
		t.Fatal("a context with no deadline can always accommodate a wait")
	}
}
