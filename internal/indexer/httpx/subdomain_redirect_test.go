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

// TestSubdomainRedirectRules is the rule table for a client built with
// FollowSubdomainRedirects (T-9010, DEC-136): a hop to a subdomain of the
// requested host on the same port is followed; everything the strict rule
// refuses otherwise is still refused.
func TestSubdomainRedirectRules(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		from string
		to   string
		want error
	}{
		{name: "same host", from: "https://files.example.org/d/x", to: "https://files.example.org/e/x"},
		{name: "storage subdomain", from: "https://files.example.org/d/x", to: "https://node7.us.files.example.org/items/x"},
		{name: "subdomain case and trailing dot", from: "https://Files.Example.org/d/x", to: "https://NODE7.files.example.org./items/x"},
		{name: "a sibling host is refused", from: "https://files.example.org/d/x", to: "https://other.example.org/x", want: ErrCrossHostRedirect},
		{name: "the parent domain is refused", from: "https://files.example.org/d/x", to: "https://example.org/x", want: ErrCrossHostRedirect},
		{name: "a lookalike suffix is refused", from: "https://files.example.org/d/x", to: "https://evilfiles.example.org/x", want: ErrCrossHostRedirect},
		{name: "a subdomain on another port is refused", from: "https://files.example.org/d/x", to: "https://node7.files.example.org:8443/x", want: ErrCrossHostRedirect},
		{name: "a downgrade to a subdomain is refused", from: "https://files.example.org/d/x", to: "http://node7.files.example.org/x", want: ErrInsecureRedirect},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			from, err := url.Parse(tc.from)
			if err != nil {
				t.Fatalf("parse %q: %v", tc.from, err)
			}

			to, err := url.Parse(tc.to)
			if err != nil {
				t.Fatalf("parse %q: %v", tc.to, err)
			}

			err = checkRedirectToSubdomain(&http.Request{URL: to}, []*http.Request{{URL: from}})
			if tc.want == nil {
				if err != nil {
					t.Fatalf("%s -> %s = %v, want it followed", tc.from, tc.to, err)
				}

				return
			}

			if !errors.Is(err, tc.want) {
				t.Fatalf("%s -> %s = %v, want %v", tc.from, tc.to, err, tc.want)
			}
		})
	}
}

// TestIsSubdomainOfRefusesWhatIsNotADomainOfItsOwn covers the rules the
// URL table cannot spell: scripts/check-indexer-hostnames.sh reads a label
// in front of an IP literal, or a bare IP target, in a URL as a public
// hostname (backlog T-926), so these hosts are given as bare url.URL hosts.
func TestIsSubdomainOfRefusesWhatIsNotADomainOfItsOwn(t *testing.T) {
	t.Parallel()

	const privateIP = "10.0.0.10"

	cases := []struct {
		name   string
		dest   string
		origin string
	}{
		{name: "a label in front of an IP origin", dest: "node." + privateIP, origin: privateIP},
		{name: "an IPv4 target under a numeric-looking origin", dest: privateIP, origin: "0.10"},
		{name: "an IPv4 target under a single-label origin", dest: privateIP, origin: "10"},
		{name: "a mapped IPv6 target", dest: "[::ffff:" + privateIP + "]", origin: "0.10"},
		{name: "a bare top-level name as the parent", dest: "example.org", origin: "org"},
		{name: "an empty label in the target", dest: "a..files.example.org", origin: "files.example.org"},
		{name: "an empty leading label in the target", dest: ".files.example.org", origin: "files.example.org"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dest, origin := tc.dest, tc.origin

			if isSubdomainOf(&url.URL{Host: dest}, &url.URL{Host: origin}) {
				t.Fatalf("isSubdomainOf(%q, %q) = true, want false", tc.dest, tc.origin)
			}
		})
	}

	if !isSubdomainOf(&url.URL{Host: "node7.us.files.example.org"}, &url.URL{Host: "files.example.org"}) {
		t.Fatal("an ordinary storage subdomain was refused")
	}
}

// storageHandoff is a transport standing in for a source whose download
// address answers with a redirect to a storage host under its own domain,
// which then serves the file. It records every host it was asked for.
type storageHandoff struct {
	mu    sync.Mutex
	hosts []string
}

func (s *storageHandoff) RoundTrip(r *http.Request) (*http.Response, error) {
	s.mu.Lock()
	s.hosts = append(s.hosts, r.URL.Host)
	s.mu.Unlock()

	if r.URL.Host == "files.example.org" {
		return &http.Response{
			StatusCode: http.StatusFound,
			Status:     "302 Found",
			Header:     http.Header{"Location": []string{"https://node7.us.files.example.org/items/x/x.torrent"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    r,
		}, nil
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader("torrent-bytes")),
		Request:    r,
	}, nil
}

func (s *storageHandoff) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.hosts...)
}

// TestFollowSubdomainRedirectsFetchesFromTheStorageHost drives the option
// end to end through the client: with it, the storage host's body comes
// back; without it — and with it on a client that carries credentials —
// the hop is refused before the storage host is contacted.
func TestFollowSubdomainRedirectsFetchesFromTheStorageHost(t *testing.T) {
	t.Parallel()

	const target = "https://files.example.org/download/x/x.torrent"

	t.Run("followed without credentials", func(t *testing.T) {
		t.Parallel()

		transport := &storageHandoff{}
		client := New(Config{MinHostInterval: -1, MaxAttempts: 1, Transport: transport, FollowSubdomainRedirects: true})

		resp, err := client.Get(testContext(t), target, nil)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}

		if string(resp.Body) != "torrent-bytes" {
			t.Errorf("Body = %q, want the storage host's file", resp.Body)
		}

		if got := transport.seen(); len(got) != 2 || got[1] != "node7.us.files.example.org" {
			t.Errorf("hosts requested = %v, want the source then its storage host", got)
		}
	})

	refused := map[string]Config{
		"refused by default":         {},
		"refused with a credential":  {FollowSubdomainRedirects: true, Credentials: testCredentials()},
		"refused with only a cookie": {FollowSubdomainRedirects: true, Credentials: Credentials{CookieHeader: testSessionValue}},
	}

	for name, cfg := range refused {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			transport := &storageHandoff{}
			cfg.MinHostInterval = -1
			cfg.MaxAttempts = 1
			cfg.Transport = transport

			_, err := New(cfg).Get(testContext(t), target, nil)
			if !errors.Is(err, ErrCrossHostRedirect) {
				t.Fatalf("error = %v, want ErrCrossHostRedirect", err)
			}

			if got := transport.seen(); len(got) != 1 {
				t.Errorf("hosts requested = %v, want only the source", got)
			}
		})
	}
}
