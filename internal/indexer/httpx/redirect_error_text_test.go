package httpx

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// locationByHost is a transport for redirect-error tests: a request answers
// with a redirect to next(r) when that returns a Location, and with 200 when
// it returns "". It never touches the network (AGENT.md §6.7).
type locationByHost struct {
	next  func(r *http.Request) string
	hosts []string
}

func (l *locationByHost) RoundTrip(r *http.Request) (*http.Response, error) {
	l.hosts = append(l.hosts, r.URL.Host)

	status, header := http.StatusOK, http.Header{}
	if loc := l.next(r); loc != "" {
		status, header = http.StatusFound, http.Header{"Location": []string{loc}}
	}

	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    r,
	}, nil
}

// TestRedirectRefusalsCarryTheHttpxPrefixOnce pins the whole message of every
// redirect refusal: one "httpx:" prefix at the front, then the request, then
// the sentinel's reason (T-9055). The sentinel still matches with the Is
// function in errors, and the message still names the host.
func TestRedirectRefusalsCarryTheHttpxPrefixOnce(t *testing.T) {
	t.Parallel()

	const magnet = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"

	subdomain := Config{FollowSubdomainRedirects: true, MagnetRedirects: true}

	for name, tc := range map[string]struct {
		cfg      Config
		from     string
		location func(r *http.Request) string
		want     error
		text     string
	}{
		"too many hops": {
			cfg: Config{}, from: "https://example.org/a",
			location: func(*http.Request) string { return "/again" },
			want:     ErrTooManyRedirects,
			text:     "httpx: GET example.org: " + ErrTooManyRedirects.Error() + " (" + strconv.Itoa(maxRedirects) + " hops)",
		},
		"another host": {
			cfg: Config{}, from: "https://example.org/a",
			location: func(*http.Request) string { return "https://other.example.net/b" },
			want:     ErrCrossHostRedirect,
			text:     "httpx: GET example.org: " + ErrCrossHostRedirect.Error() + " (example.org to other.example.net)",
		},
		"userinfo": {
			cfg: Config{}, from: "https://example.org/a",
			location: func(*http.Request) string { return "https://bob:pw@example.org/b" },
			want:     ErrUserinfoRedirect,
			text:     "httpx: GET example.org: " + ErrUserinfoRedirect.Error() + " (at example.org)",
		},
		"scheme downgrade": {
			cfg: subdomain, from: "https://example.org/a",
			location: func(*http.Request) string { return "http://sub.example.org/b" },
			want:     ErrInsecureRedirect,
			text:     "httpx: GET example.org: " + ErrInsecureRedirect.Error() + " (at sub.example.org)",
		},
		"invalid magnet": {
			cfg: subdomain, from: "https://example.org/a",
			location: func(*http.Request) string { return "magnet:?xt=bogus" },
			want:     ErrMagnetRedirectInvalid,
			text:     "httpx: GET example.org: " + ErrMagnetRedirectInvalid.Error() + " (at example.org)",
		},
		"valid magnet": {
			cfg: subdomain, from: "https://example.org/a",
			location: func(*http.Request) string { return magnet },
			want:     ErrMagnetRedirect,
			text:     "httpx: GET example.org: " + ErrMagnetRedirect.Error() + " (at example.org)",
		},
		"unparseable Location": {
			cfg: Config{}, from: "https://example.org/a",
			location: func(*http.Request) string { return "/x%zz" },
			want:     ErrRedirectLocationInvalid,
			text:     "httpx: GET example.org: " + ErrRedirectLocationInvalid.Error() + " (at example.org)",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			cfg := tc.cfg
			cfg.MinHostInterval = -1
			cfg.MaxAttempts = 1
			cfg.Transport = &locationByHost{next: tc.location}

			_, err := New(cfg).Get(testContext(t), tc.from, nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Get = %v, want %v", err, tc.want)
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

// TestLocationParseErrorNamesTheHopThatSentIt: a bad Location on the second
// hop of a subdomain chain names that hop's host, not the host the request
// started at, and still repeats no part of the Location (T-9054, DEC-146).
func TestLocationParseErrorNamesTheHopThatSentIt(t *testing.T) {
	t.Parallel()

	const bad = "https://alice:s3cret@one.example.org /vault/secretpath%zz?tok=q1value"

	transport := &locationByHost{next: func(r *http.Request) string {
		if r.URL.Host == "example.org" {
			return "https://one.example.org/next"
		}

		return bad
	}}

	_, err := New(Config{
		MinHostInterval: -1, MaxAttempts: 1, FollowSubdomainRedirects: true, Transport: transport,
	}).Get(testContext(t), "https://example.org/start", nil)
	if !errors.Is(err, ErrRedirectLocationInvalid) {
		t.Fatalf("Get = %v, want ErrRedirectLocationInvalid", err)
	}

	if want := "(at one.example.org)"; !strings.HasSuffix(err.Error(), want) {
		t.Fatalf("error %q does not end %q, the host of the hop that sent the bad Location", err, want)
	}

	if len(transport.hosts) != 2 {
		t.Fatalf("transport saw hosts %v, want exactly the two hops", transport.hosts)
	}

	for _, leak := range append(locationLeaks(), "one.example.org /", "next") {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("error %q repeats %q", err, leak)
		}
	}
}
