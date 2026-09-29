package httpx

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// Userinfo in a redirect target (T-9045). Go's http client turns
// `https://user:pw@host/` into a Basic Authorization header on the next
// hop, so a Location carrying userinfo is the server choosing credentials
// tortui would send. Every client built here refuses it, whatever the host.

// userinfoLocations are redirect targets on the requested host (or, for
// the subdomain client, under it) that carry userinfo in some shape.
var userinfoLocations = map[string]string{
	"user and password": "https://alice:s3cret@files.example.org/final",
	"user only":         "https://alice@files.example.org/final",
	"empty password":    "https://alice:@files.example.org/final",
	"empty user":        "https://:s3cret@files.example.org/final",
	"bare at":           "https://@files.example.org/final",
}

// assertNoRedirectEcho fails when a refusal repeats the refused target:
// its credential, the separator, or its path.
func assertNoRedirectEcho(t *testing.T, err error) {
	t.Helper()

	for _, leak := range []string{"alice", "s3cret", "@", "/final", "://"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("error %q repeats %q from the refused redirect", err, leak)
		}
	}
}

// TestRedirectCheckRefusesUserinfo is the rule table for both redirect
// checks: a same-host (and, for the subdomain rule, a subdomain) target
// with userinfo is refused with ErrUserinfoRedirect.
func TestRedirectCheckRefusesUserinfo(t *testing.T) {
	t.Parallel()

	from, err := url.Parse("https://files.example.org/download/x")
	if err != nil {
		t.Fatalf("parse origin: %v", err)
	}

	checks := map[string]func(*http.Request, []*http.Request) error{
		"strict":    checkRedirect,
		"subdomain": checkRedirectToSubdomain,
	}

	targets := map[string]string{}
	for name, loc := range userinfoLocations {
		targets[name] = loc
	}

	targets["subdomain with userinfo"] = "https://alice:s3cret@node7.files.example.org/final"

	for checkName, check := range checks {
		for name, raw := range targets {
			t.Run(checkName+"/"+name, func(t *testing.T) {
				t.Parallel()

				to, err := url.Parse(raw)
				if err != nil {
					t.Fatalf("parse %q: %v", raw, err)
				}

				err = check(&http.Request{URL: to}, []*http.Request{{URL: from}})
				if checkName == "strict" && name == "subdomain with userinfo" {
					// The strict rule refuses it as another host already;
					// either refusal keeps the hop from happening.
					if err == nil {
						t.Fatalf("%s -> %s followed, want it refused", from, raw)
					}

					return
				}

				if !errors.Is(err, ErrUserinfoRedirect) {
					t.Fatalf("%s -> %s = %v, want ErrUserinfoRedirect", from, raw, err)
				}

				assertNoRedirectEcho(t, err)
			})
		}
	}
}

// userinfoHop is a transport standing in for a source whose download
// address answers with a same-host redirect whose Location carries
// userinfo. It records every request it was sent.
type userinfoHop struct {
	location string

	mu       sync.Mutex
	requests []string
	auth     []string
}

func (h *userinfoHop) RoundTrip(r *http.Request) (*http.Response, error) {
	h.mu.Lock()
	h.requests = append(h.requests, r.URL.Path)
	h.auth = append(h.auth, r.Header.Get("Authorization"))
	first := len(h.requests) == 1
	h.mu.Unlock()

	if first {
		return &http.Response{
			StatusCode: http.StatusFound,
			Status:     "302 Found",
			Header:     http.Header{"Location": []string{h.location}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    r,
		}, nil
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader("followed")),
		Request:    r,
	}, nil
}

func (h *userinfoHop) seen() (paths, auth []string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	return append([]string(nil), h.requests...), append([]string(nil), h.auth...)
}

// TestARedirectWithUserinfoIsNeverFollowed drives the refusal end to end
// through the client, for a plain client (the scraper and torznab) and a
// FollowSubdomainRedirects one (the engine's .torrent client): the
// redirect is refused, no follow-up request is sent, and so no
// Authorization header ever goes out.
func TestARedirectWithUserinfoIsNeverFollowed(t *testing.T) {
	t.Parallel()

	const target = "https://files.example.org/download/x.torrent"

	clients := map[string]Config{
		"plain client":     {},
		"subdomain client": {FollowSubdomainRedirects: true},
	}

	for clientName, base := range clients {
		for name, location := range userinfoLocations {
			t.Run(clientName+"/"+name, func(t *testing.T) {
				t.Parallel()

				transport := &userinfoHop{location: location}
				cfg := base
				cfg.MinHostInterval = -1
				cfg.MaxAttempts = 1
				cfg.Transport = transport

				_, err := New(cfg).Get(testContext(t), target, nil)
				if !errors.Is(err, ErrUserinfoRedirect) {
					t.Fatalf("error = %v, want ErrUserinfoRedirect", err)
				}

				assertNoRedirectEcho(t, err)

				paths, auth := transport.seen()
				if len(paths) != 1 {
					t.Fatalf("requests sent = %v, want only the first one (no follow-up)", paths)
				}

				for _, header := range auth {
					if header != "" {
						t.Fatalf("an Authorization header was sent: %q", header)
					}
				}
			})
		}
	}

	t.Run("subdomain client/subdomain with userinfo", func(t *testing.T) {
		t.Parallel()

		transport := &userinfoHop{location: "https://alice:s3cret@node7.files.example.org/final"}

		_, err := New(Config{
			MinHostInterval: -1, MaxAttempts: 1, Transport: transport, FollowSubdomainRedirects: true,
		}).Get(testContext(t), target, nil)
		if !errors.Is(err, ErrUserinfoRedirect) {
			t.Fatalf("error = %v, want ErrUserinfoRedirect", err)
		}

		if paths, _ := transport.seen(); len(paths) != 1 {
			t.Fatalf("requests sent = %v, want only the first one (no follow-up)", paths)
		}
	})
}
