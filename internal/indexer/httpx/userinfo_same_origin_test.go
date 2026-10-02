package httpx

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// A redirect that carries the original request's own userinfo, byte for
// byte, to the original host and effective port (T-9046, DEC-145). A
// userinfo endpoint the user wrote themselves — a reverse proxy in front of
// their own indexer manager — sends a relative Location, which inherits the
// request's userinfo, so DEC-144's blanket refusal broke every redirect it
// made. Everything else with userinfo stays refused exactly as before.

// sameOriginFrom is the endpoint the user configured, userinfo and all.
const sameOriginFrom = "https://alice:s3cret@example.org/api/start"

// redirectHop builds the request pair net/http hands CheckRedirect for one
// hop: the original request built from from, and the next one resolved
// against it from location, with the redirect response that caused it.
func redirectHop(t *testing.T, from, location string) (*http.Request, []*http.Request) {
	t.Helper()

	orig, err := http.NewRequest(http.MethodGet, from, nil)
	if err != nil {
		t.Fatalf("build original request: %v", err)
	}

	to, err := orig.URL.Parse(location)
	if err != nil {
		t.Fatalf("resolve %q: %v", location, err)
	}

	next := &http.Request{
		URL:      to,
		Response: &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{location}}},
	}

	return next, []*http.Request{orig}
}

// sameOriginCase is one row of the rule table: the configured endpoint,
// the Location it answers with, and the sentinel wanted (nil: followed).
type sameOriginCase struct {
	from     string
	location string
	want     error
}

// sameOriginAllowed are the redirects the rule follows.
var sameOriginAllowed = map[string]sameOriginCase{
	"relative path inherits the userinfo":  {sameOriginFrom, "/api/final", nil},
	"relative query inherits the userinfo": {sameOriginFrom, "?page=2", nil},
	"absolute, identical userinfo":         {sameOriginFrom, "https://alice:s3cret@example.org/api/final", nil},
	"scheme-relative, identical userinfo":  {sameOriginFrom, "//alice:s3cret@example.org/api/final", nil},
	"explicit default port equals implicit": {
		sameOriginFrom, "https://alice:s3cret@example.org:443/api/final", nil,
	},
	"implicit default port equals explicit": {
		"http://alice:s3cret@example.org:80/api/start", "http://alice:s3cret@example.org/api/final", nil,
	},
	"same explicit non-default port": {
		"http://alice:s3cret@example.org:8080/api/start", "http://alice:s3cret@example.org:8080/api/final", nil,
	},
	"host differs only in case": {sameOriginFrom, "https://alice:s3cret@EXAMPLE.org/api/final", nil},
	"http to https on the same explicit port": {
		"http://alice:s3cret@example.org:8443/api/start", "https://alice:s3cret@example.org:8443/api/final", nil,
	},
	"bare at, identical":   {"https://@example.org/api/start", "https://@example.org/api/final", nil},
	"user only, identical": {"https://alice@example.org/api/start", "https://alice@example.org/api/final", nil},
	"empty password, identical": {
		"https://alice:@example.org/api/start", "https://alice:@example.org/api/final", nil,
	},
	"escaped reserved byte, identical": {
		"https://alice:p%40ss@example.org/api/start", "https://alice:p%40ss@example.org/api/final", nil,
	},
}

// sameOriginRefused are the redirects the rule refuses, and why.
var sameOriginRefused = map[string]sameOriginCase{
	"different password":   {sameOriginFrom, "https://alice:hunter2@example.org/api/final", ErrUserinfoRedirect},
	"different user":       {sameOriginFrom, "https://mallory:s3cret@example.org/api/final", ErrUserinfoRedirect},
	"password dropped":     {sameOriginFrom, "https://alice@example.org/api/final", ErrUserinfoRedirect},
	"empty password":       {sameOriginFrom, "https://alice:@example.org/api/final", ErrUserinfoRedirect},
	"bare at":              {sameOriginFrom, "https://@example.org/api/final", ErrUserinfoRedirect},
	"password added":       {"https://alice@example.org/api/start", "https://alice:s3cret@example.org/api/final", ErrUserinfoRedirect},
	"no password vs empty": {"https://alice@example.org/api/start", "https://alice:@example.org/api/final", ErrUserinfoRedirect},
	"added where the original had none": {
		"https://example.org/api/start", "https://alice:s3cret@example.org/api/final", ErrUserinfoRedirect,
	},
	"bare at added where the original had none": {
		"https://example.org/api/start", "https://@example.org/api/final", ErrUserinfoRedirect,
	},
	"different host": {sameOriginFrom, "https://alice:s3cret@example.com/api/final", ErrUserinfoRedirect},
	"subdomain":      {sameOriginFrom, "https://alice:s3cret@node7.example.org/api/final", ErrUserinfoRedirect},
	"parent domain":  {"https://alice:s3cret@api.example.org/start", "https://alice:s3cret@example.org/final", ErrUserinfoRedirect},
	"trailing dot":   {sameOriginFrom, "https://alice:s3cret@example.org./api/final", ErrUserinfoRedirect},
	"different port": {sameOriginFrom, "https://alice:s3cret@example.org:8443/api/final", ErrUserinfoRedirect},
	"port added":     {"http://alice:s3cret@example.org/api/start", "http://alice:s3cret@example.org:8080/api/final", ErrUserinfoRedirect},
	"port removed":   {"http://alice:s3cret@example.org:8080/api/start", "http://alice:s3cret@example.org/api/final", ErrUserinfoRedirect},
	"http to https changes the effective port": {
		"http://alice:s3cret@example.org/api/start", "https://alice:s3cret@example.org/api/final", ErrUserinfoRedirect,
	},
	"default port of the other scheme": {
		sameOriginFrom, "https://alice:s3cret@example.org:80/api/final", ErrUserinfoRedirect,
	},
	"unknown scheme, no port": {sameOriginFrom, "ftp://alice:s3cret@example.org/api/final", ErrUserinfoRedirect},
	"unknown scheme on both ends": {
		"ftp://alice:s3cret@example.org/api/start", "ftp://alice:s3cret@example.org/api/final", ErrUserinfoRedirect,
	},
	"unknown scheme, same explicit port": {
		"http://alice:s3cret@example.org:8080/api/start", "ftp://alice:s3cret@example.org:8080/api/final", ErrUserinfoRedirect,
	},
	"unknown scheme, explicit http default port": {
		"http://alice:s3cret@example.org/api/start", "ftp://alice:s3cret@example.org:80/api/final", ErrUserinfoRedirect,
	},
	"unknown scheme on both ends, same explicit port": {
		"ftp://alice:s3cret@example.org:2121/api/start", "ftp://alice:s3cret@example.org:2121/api/final", ErrUserinfoRedirect,
	},
	"percent-encoded user letter": {
		sameOriginFrom, "https://%61lice:s3cret@example.org/api/final", ErrUserinfoRedirect,
	},
	"percent-encoded password letter": {
		sameOriginFrom, "https://alice:s3cr%65t@example.org/api/final", ErrUserinfoRedirect,
	},
	"lowercase hex in an escape": {
		"https://alice:p%2Fss@example.org/api/start", "https://alice:p%2fss@example.org/api/final", ErrUserinfoRedirect,
	},
	"reserved byte escaped needlessly": {
		"https://alice:p$ss@example.org/api/start", "https://alice:p%24ss@example.org/api/final", ErrUserinfoRedirect,
	},
	"escaped separator": {sameOriginFrom, "https://alice%3As3cret@example.org/api/final", ErrUserinfoRedirect},
	"https to http, same effective port": {
		sameOriginFrom, "http://alice:s3cret@example.org:443/api/final", ErrInsecureRedirect,
	},
	"https to http, same explicit port": {
		"https://alice:s3cret@example.org:8443/api/start", "http://alice:s3cret@example.org:8443/api/final", ErrInsecureRedirect,
	},
}

// withoutUserinfoUnchanged pins that a redirect without userinfo is
// decided exactly as before T-9046, even from a userinfo endpoint: the
// host is compared as written (T-928), so an explicit default port is
// still another host there.
var withoutUserinfoUnchanged = map[string]sameOriginCase{
	"absolute, same host": {sameOriginFrom, "https://example.org/api/final", nil},
	"absolute, explicit default port": {
		sameOriginFrom, "https://example.org:443/api/final", ErrCrossHostRedirect,
	},
	"absolute, other host": {sameOriginFrom, "https://example.com/api/final", ErrCrossHostRedirect},
	"https to http":        {sameOriginFrom, "http://example.org/api/final", ErrInsecureRedirect},
}

// redirectChecks are both redirect rules the userinfo rule applies to.
var redirectChecks = map[string]func(*http.Request, []*http.Request) error{
	"strict":    checkRedirect,
	"subdomain": checkRedirectToSubdomain,
}

// TestSameOriginUserinfoRedirectRule is the rule table, on both the strict
// and the subdomain redirect checks.
func TestSameOriginUserinfoRedirectRule(t *testing.T) {
	t.Parallel()

	tables := map[string]map[string]sameOriginCase{
		"allowed":          sameOriginAllowed,
		"refused":          sameOriginRefused,
		"without userinfo": withoutUserinfoUnchanged,
	}

	for checkName, check := range redirectChecks {
		for tableName, table := range tables {
			for name, tc := range table {
				t.Run(checkName+"/"+tableName+"/"+name, func(t *testing.T) {
					t.Parallel()

					req, via := redirectHop(t, tc.from, tc.location)

					err := check(req, via)
					if tc.want == nil {
						if err != nil {
							t.Fatalf("%s -> %s = %v, want it followed", tc.from, tc.location, err)
						}

						return
					}

					if !errors.Is(err, tc.want) {
						t.Fatalf("%s -> %s = %v, want %v", tc.from, tc.location, err, tc.want)
					}

					assertNoUserinfoEcho(t, err.Error(), tc)
				})
			}
		}
	}
}

// TestSameOriginUserinfoRedirectLaterHop checks the rule against the first
// request of the chain, not the previous hop: a relative Location after an
// allowed hop still inherits the original credentials, and an absolute
// Location on a later hop is still compared with the original — including
// after a middle hop that carried no userinfo at all.
func TestSameOriginUserinfoRedirectLaterHop(t *testing.T) {
	t.Parallel()

	chains := map[string]struct {
		middle string
		final  map[string]error
	}{
		"after a hop with the same userinfo": {
			middle: "https://alice:s3cret@example.org:443/api/middle",
			final: map[string]error{
				"/api/final": nil,
				"https://alice:s3cret@example.org/api/final":   nil,
				"https://alice:hunter2@example.org/api/final":  ErrUserinfoRedirect,
				"https://%61lice:s3cret@example.org/api/final": ErrUserinfoRedirect,
			},
		},
		"after a hop without userinfo": {
			middle: "https://example.org/api/middle",
			final: map[string]error{
				"/api/final": nil,
				"https://alice:s3cret@example.org/api/final":  nil,
				"https://alice:hunter2@example.org/api/final": ErrUserinfoRedirect,
			},
		},
	}

	for checkName, check := range redirectChecks {
		for chainName, chain := range chains {
			t.Run(checkName+"/"+chainName, func(t *testing.T) {
				t.Parallel()

				hop1, via := redirectHop(t, sameOriginFrom, chain.middle)
				if err := check(hop1, via); err != nil {
					t.Fatalf("first hop = %v, want it followed", err)
				}

				via = append(via, hop1)

				for location, want := range chain.final {
					to, err := hop1.URL.Parse(location)
					if err != nil {
						t.Fatalf("resolve %q: %v", location, err)
					}

					hop2 := &http.Request{
						URL:      to,
						Response: &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{location}}},
					}

					if err := check(hop2, via); !errors.Is(err, want) {
						t.Errorf("second hop to %s = %v, want %v", location, err, want)
					}
				}
			})
		}
	}
}

// TestSameOriginUserinfoRedirectNeedsTheLocation fails closed: net/http
// always hands CheckRedirect the response carrying the Location, and
// without it the bytes the server wrote cannot be compared.
func TestSameOriginUserinfoRedirectNeedsTheLocation(t *testing.T) {
	t.Parallel()

	for checkName, check := range redirectChecks {
		t.Run(checkName, func(t *testing.T) {
			t.Parallel()

			const absolute = "https://alice:s3cret@example.org/api/final"

			for name, tc := range map[string]struct {
				location string
				mutate   func(*http.Request)
			}{
				"no response": {absolute, func(r *http.Request) { r.Response = nil }},
				"no response, empty userinfo on both ends": {"https://@example.org/api/final", func(r *http.Request) {
					r.Response = nil
				}},
				"no Location": {absolute, func(r *http.Request) { r.Response.Header.Del("Location") }},
				"other Location": {absolute, func(r *http.Request) {
					r.Response.Header.Set("Location", "https://mallory:x@example.org/")
				}},
				"unparseable Location": {absolute, func(r *http.Request) {
					r.Response.Header.Set("Location", "https://alice:s3cret@example.org/%zz")
				}},
				"relative Location, userinfo not inherited": {absolute, func(r *http.Request) {
					r.Response.Header.Set("Location", "/api/final")
				}},
				"target differs from the Location": {absolute, func(r *http.Request) {
					r.URL.User = url.UserPassword("mallory", "x")
				}},
				"inherited, but the Location names a scheme": {"/api/final", func(r *http.Request) {
					r.Response.Header.Set("Location", "https:/api/final")
				}},
				"inherited, but the Location names a host": {"/api/final", func(r *http.Request) {
					r.Response.Header.Set("Location", "//example.org/api/final")
				}},
			} {
				from := sameOriginFrom
				if strings.HasPrefix(tc.location, "https://@") {
					from = "https://@example.org/api/start"
				}

				req, via := redirectHop(t, from, tc.location)
				tc.mutate(req)

				if err := check(req, via); !errors.Is(err, ErrUserinfoRedirect) {
					t.Errorf("%s: = %v, want ErrUserinfoRedirect", name, err)
				}
			}
		})
	}
}

// assertNoUserinfoEcho fails when text repeats a credential, the userinfo
// separator, or a path from either end of the redirect.
func assertNoUserinfoEcho(t *testing.T, text string, tc sameOriginCase) {
	t.Helper()

	leaks := []string{"alice", "mallory", "s3cret", "hunter2", "%", "@", "://", "/api/"}
	for _, leak := range leaks {
		if strings.Contains(text, leak) {
			t.Errorf("%q repeats %q (%s -> %s)", text, leak, tc.from, tc.location)
		}
	}
}

// sameOriginHop is a transport for the end-to-end tests: the configured
// address answers with a redirect to location, and the redirect target
// answers with finalStatus (or finalErr). It records every path and
// Authorization header it was sent.
type sameOriginHop struct {
	location    string
	finalStatus int
	finalErr    error

	mu    sync.Mutex
	paths []string
	auth  []string
}

func (h *sameOriginHop) RoundTrip(r *http.Request) (*http.Response, error) {
	h.mu.Lock()
	h.paths = append(h.paths, r.URL.Path)
	h.auth = append(h.auth, r.Header.Get("Authorization"))
	h.mu.Unlock()

	if r.URL.Path == "/api/start" {
		return &http.Response{
			StatusCode: http.StatusFound,
			Status:     "302 Found",
			Header:     http.Header{"Location": []string{h.location}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    r,
		}, nil
	}

	if h.finalErr != nil {
		return nil, h.finalErr
	}

	status := h.finalStatus
	if status == 0 {
		status = http.StatusOK
	}

	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader("followed")),
		Request:    r,
	}, nil
}

func (h *sameOriginHop) seen() (paths, auth []string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	return append([]string(nil), h.paths...), append([]string(nil), h.auth...)
}

// endToEndClients are the two client shapes the rule applies to: a plain
// one (scraper, torznab) and the engine's credential-free subdomain one.
var endToEndClients = map[string]Config{
	"plain client":     {},
	"subdomain client": {FollowSubdomainRedirects: true},
}

// TestSameOriginUserinfoNeedsAWebScheme checks the rule's own scheme
// allowlist (T-9095): a target that is not http or https is refused even
// with the original userinfo, host and effective port, so the decision
// never rests on the transport refusing the scheme later. An http(s) target
// with the same bytes is still followed. An ftp origin redirecting to http on
// its own host and port is refused by the origin check alone (T-9099).
func TestSameOriginUserinfoNeedsAWebScheme(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		from, location string
		want           bool
	}{
		"ftp, same explicit port":        {"http://alice:s3cret@example.org:8080/a", "ftp://alice:s3cret@example.org:8080/b", false},
		"ftp, explicit http port":        {"http://alice:s3cret@example.org/a", "ftp://alice:s3cret@example.org:80/b", false},
		"ws, same explicit port":         {"http://alice:s3cret@example.org:8080/a", "ws://alice:s3cret@example.org:8080/b", false},
		"ftp origin, same explicit port": {"ftp://alice:s3cret@example.org:2121/a", "ftp://alice:s3cret@example.org:2121/b", false},
		"ftp origin, http target":        {"ftp://alice:s3cret@example.org:2121/a", "http://alice:s3cret@example.org:2121/b", false},
		"http, same explicit port":       {"http://alice:s3cret@example.org:8080/a", "http://alice:s3cret@example.org:8080/b", true},
		"HTTPS, same explicit port":      {"http://alice:s3cret@example.org:8443/a", "HTTPS://alice:s3cret@example.org:8443/b", true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			req, via := redirectHop(t, tc.from, tc.location)
			if got := sameOriginUserinfo(req, via); got != tc.want {
				t.Errorf("sameOriginUserinfo(%s -> %s) = %v, want %v", tc.from, tc.location, got, tc.want)
			}
		})
	}
}

// TestSameOriginUserinfoNonWebSchemeRefusedEndToEnd drives an http endpoint
// with userinfo and an explicit port that redirects to ftp on the same host
// and port: the client refuses it at the redirect check, so the transport
// never sees a second request carrying the credentials.
func TestSameOriginUserinfoNonWebSchemeRefusedEndToEnd(t *testing.T) {
	t.Parallel()

	const from = "http://alice:s3cret@example.org:8080/api/start"

	const location = "ftp://alice:s3cret@example.org:8080/api/final"

	for clientName, base := range endToEndClients {
		t.Run(clientName, func(t *testing.T) {
			t.Parallel()

			transport := &sameOriginHop{location: location}
			cfg := base
			cfg.MinHostInterval = -1
			cfg.MaxAttempts = 1
			cfg.Transport = transport

			_, err := New(cfg).Get(testContext(t), from, nil)
			if !errors.Is(err, ErrUserinfoRedirect) {
				t.Fatalf("Get = %v, want ErrUserinfoRedirect", err)
			}

			assertNoUserinfoEcho(t, err.Error(), sameOriginCase{from: from, location: location})

			if paths, _ := transport.seen(); len(paths) != 1 {
				t.Fatalf("requests = %v, want only the first one", paths)
			}
		})
	}
}

// TestSameOriginUserinfoRedirectIsFollowed drives an allowed redirect
// through the client: the second request goes out, with the same Basic
// credentials as the first and no other.
func TestSameOriginUserinfoRedirectIsFollowed(t *testing.T) {
	t.Parallel()

	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:s3cret"))

	for clientName, base := range endToEndClients {
		for _, location := range []string{
			"/api/final",
			"https://alice:s3cret@example.org/api/final",
			"https://alice:s3cret@example.org:443/api/final",
		} {
			t.Run(clientName+"/"+location, func(t *testing.T) {
				t.Parallel()

				transport := &sameOriginHop{location: location}
				cfg := base
				cfg.MinHostInterval = -1
				cfg.MaxAttempts = 1
				cfg.Transport = transport

				res, err := New(cfg).Get(testContext(t), sameOriginFrom, nil)
				if err != nil {
					t.Fatalf("Get = %v, want the redirect followed", err)
				}

				if string(res.Body) != "followed" {
					t.Fatalf("body = %q, want the redirect target's", res.Body)
				}

				paths, auth := transport.seen()
				if len(paths) != 2 || paths[1] != "/api/final" {
					t.Fatalf("requests = %v, want the start and the redirect target", paths)
				}

				for i, header := range auth {
					if header != wantAuth {
						t.Fatalf("request %d Authorization = %q, want the original credentials", i, header)
					}
				}
			})
		}
	}
}

// TestSameOriginUserinfoRedirectRefusedEndToEnd drives refused redirects
// through the client: one request only, so the other credentials never go
// out, and an error that repeats none of them.
func TestSameOriginUserinfoRedirectRefusedEndToEnd(t *testing.T) {
	t.Parallel()

	for clientName, base := range endToEndClients {
		for _, location := range []string{
			"https://alice:hunter2@example.org/api/final",
			"https://%61lice:s3cret@example.org/api/final",
			"https://alice:s3cret@node7.example.org/api/final",
			"https://alice:s3cret@example.org:8443/api/final",
		} {
			t.Run(clientName+"/"+location, func(t *testing.T) {
				t.Parallel()

				transport := &sameOriginHop{location: location}
				cfg := base
				cfg.MinHostInterval = -1
				cfg.MaxAttempts = 1
				cfg.Transport = transport

				_, err := New(cfg).Get(testContext(t), sameOriginFrom, nil)
				if !errors.Is(err, ErrUserinfoRedirect) {
					t.Fatalf("Get = %v, want ErrUserinfoRedirect", err)
				}

				assertNoUserinfoEcho(t, err.Error(), sameOriginCase{from: sameOriginFrom, location: location})

				if paths, _ := transport.seen(); len(paths) != 1 {
					t.Fatalf("requests = %v, want only the first one", paths)
				}
			})
		}
	}
}

// TestSameOriginUserinfoRedirectRedaction follows an allowed redirect to a
// target that fails — a retried 503, then a transport error — and checks
// neither the error nor the client's log ever carries the userinfo.
func TestSameOriginUserinfoRedirectRedaction(t *testing.T) {
	t.Parallel()

	encoded := base64.StdEncoding.EncodeToString([]byte("alice:s3cret"))

	for name, transport := range map[string]*sameOriginHop{
		"status":    {location: "/api/final", finalStatus: http.StatusServiceUnavailable},
		"transport": {location: "https://alice:s3cret@example.org/api/final", finalErr: errors.New("connection reset by peer")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var logs bytes.Buffer

			var logMu sync.Mutex

			logger := slog.New(slog.NewTextHandler(&lockedWriter{w: &logs, mu: &logMu}, &slog.HandlerOptions{Level: slog.LevelDebug}))

			_, err := New(Config{
				MinHostInterval: -1,
				MaxAttempts:     2,
				Clock:           newFakeClock(),
				Logger:          logger,
				Transport:       transport,
			}).Get(testContext(t), sameOriginFrom, nil)
			if err == nil {
				t.Fatal("Get succeeded, want the failing target's error")
			}

			if paths, _ := transport.seen(); len(paths) < 2 || paths[1] != "/api/final" {
				t.Fatalf("requests = %v, want the redirect followed", paths)
			}

			logMu.Lock()
			logged := logs.String()
			logMu.Unlock()

			if name == "status" && !strings.Contains(logged, "retrying") {
				t.Fatalf("log = %q, want the retry line this test inspects", logged)
			}

			for _, text := range []string{err.Error(), logged} {
				for _, leak := range []string{"alice", "s3cret", encoded, "@"} {
					if strings.Contains(text, leak) {
						t.Errorf("%q repeats %q", text, leak)
					}
				}
			}
		})
	}
}

// lockedWriter serialises writes to a buffer shared with the test.
type lockedWriter struct {
	w  io.Writer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.w.Write(p)
}

// TestRawUserinfo pins that the userinfo is cut out of a Location the way
// url.Parse cuts it, bytes untouched, and that anything without one is
// reported as such.
func TestRawUserinfo(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		ref, scheme, want string
		ok                bool
	}{
		{"https://alice:s3cret@example.org/x?q=1#f", "https", "alice:s3cret", true},
		{"HTTPS://%61lice:p%2fss@example.org:443", "https", "%61lice:p%2fss", true},
		{"//alice@example.org/x", "", "alice", true},
		{"https://a@b:c@example.org/x", "https", "a@b:c", true},
		{"https://@example.org/", "https", "", true},
		{"https://example.org/?next=alice@example.org", "https", "", false},
		{"https://example.org/#alice@example.org", "https", "", false},
		{"https://example.org/alice@x", "https", "", false},
		{"/api/final", "", "", false},
		{"https:/api/final", "https", "", false},
		{"http", "https", "", false},
		{"httpx//alice@example.org", "https", "", false},
	} {
		got, ok := rawUserinfo(tc.ref, tc.scheme)
		if got != tc.want || ok != tc.ok {
			t.Errorf("rawUserinfo(%q, %q) = %q, %v; want %q, %v", tc.ref, tc.scheme, got, ok, tc.want, tc.ok)
		}
	}
}
