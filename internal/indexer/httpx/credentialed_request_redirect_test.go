package httpx

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
)

// requestAPIKey stands in for a credential a caller puts on one request's
// header rather than in Config.Credentials (T-9099).
const requestAPIKey = "REQUEST-KEY-9099"

// headerHandoff is storageHandoff that also records, for every request, the
// header map it carried and the userinfo of its URL, so a test can show a
// credential never reached the storage host.
type headerHandoff struct {
	mu       sync.Mutex
	hosts    []string
	headers  []http.Header
	userinfo []bool
}

func (s *headerHandoff) RoundTrip(r *http.Request) (*http.Response, error) {
	s.mu.Lock()
	s.hosts = append(s.hosts, r.URL.Host)
	s.headers = append(s.headers, r.Header.Clone())
	s.userinfo = append(s.userinfo, r.URL.User != nil)
	s.mu.Unlock()

	if r.URL.Host == "files.example.org" && r.URL.Path == "/start" {
		return &http.Response{
			StatusCode: http.StatusFound,
			Status:     "302 Found",
			Header:     http.Header{"Location": []string{"/download/x.torrent"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    r,
		}, nil
	}

	if r.URL.Host == "files.example.org" {
		return &http.Response{
			StatusCode: http.StatusFound,
			Status:     "302 Found",
			Header:     http.Header{"Location": []string{"https://node7.files.example.org/items/x.torrent"}},
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

// credentialedURL is scheme://host+path with the userinfo alice and
// requestAPIKey, built through the net/url package rather than spelled as
// a literal.
func credentialedURL(scheme, host, path string) string {
	u := url.URL{Scheme: scheme, User: url.UserPassword("alice", requestAPIKey), Host: host, Path: path}

	return u.String()
}

func (s *headerHandoff) seen() ([]string, []http.Header) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.hosts...), append([]http.Header(nil), s.headers...)
}

// TestSubdomainRedirectRefusesACredentialedRequest: on a credential-free
// client built with FollowSubdomainRedirects, a request that carries its
// own credential — an X-Api-Key header, any other caller header, or
// userinfo — keeps the strict same-host rule (T-9099, DEC-156). net/http
// forwards an X-Api-Key header on every hop it follows, so the subdomain hop
// is refused before the storage host is contacted and the key never reaches
// it.
func TestSubdomainRedirectRefusesACredentialedRequest(t *testing.T) {
	t.Parallel()

	const target = "https://files.example.org/download/x.torrent"

	client := func(transport http.RoundTripper) *Client {
		return New(Config{MinHostInterval: -1, MaxAttempts: 1, Transport: transport, FollowSubdomainRedirects: true})
	}

	t.Run("X-Api-Key header", func(t *testing.T) {
		t.Parallel()

		transport := &headerHandoff{}

		_, err := client(transport).Do(testContext(t), Request{
			URL:    target,
			Header: http.Header{"X-Api-Key": {requestAPIKey}},
		})
		hosts, headers := transport.seen()
		for i, host := range hosts {
			if host != "files.example.org" && headers[i].Get("X-Api-Key") != "" {
				t.Errorf("the X-Api-Key header was forwarded to %s", host)
			}
		}

		if !errors.Is(err, ErrCrossHostRedirect) {
			t.Fatalf("error = %v, want ErrCrossHostRedirect", err)
		}

		if len(hosts) != 1 || hosts[0] != "files.example.org" {
			t.Fatalf("hosts requested = %v, want only the source", hosts)
		}

		if headers[0].Get("X-Api-Key") != requestAPIKey {
			t.Errorf("the source did not get the header: %v", headers[0])
		}

		if strings.Contains(err.Error(), requestAPIKey) {
			t.Errorf("error echoes the key: %v", err)
		}
	})

	refused := map[string]Request{
		"Authorization header":    {URL: target, Header: http.Header{"Authorization": {"Bearer " + requestAPIKey}}},
		"Cookie header":           {URL: target, Header: http.Header{"Cookie": {"session=" + requestAPIKey}}},
		"a vendor's token header": {URL: target, Header: http.Header{"X-Indexer-Token": {requestAPIKey}}},
		"lower-case header name":  {URL: target, Header: http.Header{"x-api-key": {requestAPIKey}}},
		"userinfo":                {URL: credentialedURL("https", "files.example.org", "/download/x.torrent")},
	}

	for name, req := range refused {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			transport := &headerHandoff{}

			_, err := client(transport).Do(testContext(t), req)
			if !errors.Is(err, ErrCrossHostRedirect) {
				t.Fatalf("error = %v, want ErrCrossHostRedirect", err)
			}

			if hosts, _ := transport.seen(); len(hosts) != 1 {
				t.Errorf("hosts requested = %v, want only the source", hosts)
			}
		})
	}

	// net/http puts a Referer on the request it builds for a redirect, so
	// the second hop's previous request carries a header the first did
	// not. The rule judges the chain's first request, which carries none:
	// a same-host hop then a subdomain hop is followed.
	t.Run("followed after a same-host hop", func(t *testing.T) {
		t.Parallel()

		transport := &headerHandoff{}

		resp, err := client(transport).Get(testContext(t), "https://files.example.org/start", nil)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}

		if string(resp.Body) != "torrent-bytes" {
			t.Errorf("Body = %q, want the storage host's file", resp.Body)
		}

		want := []string{"files.example.org", "files.example.org", "node7.files.example.org"}
		if hosts, _ := transport.seen(); !slices.Equal(hosts, want) {
			t.Errorf("hosts requested = %v, want %v", hosts, want)
		}
	})

	t.Run("followed with only a User-Agent", func(t *testing.T) {
		t.Parallel()

		transport := &headerHandoff{}

		resp, err := client(transport).Do(testContext(t), Request{
			URL:    target,
			Header: http.Header{"User-Agent": {"tortui-test"}},
		})
		if err != nil {
			t.Fatalf("Do: %v", err)
		}

		if string(resp.Body) != "torrent-bytes" {
			t.Errorf("Body = %q, want the storage host's file", resp.Body)
		}

		if hosts, _ := transport.seen(); len(hosts) != 2 || hosts[1] != "node7.files.example.org" {
			t.Errorf("hosts requested = %v, want the source then its storage host", hosts)
		}
	})
}

// TestMagnetRedirectRefusesACredentialedRequest: on a credential-free
// client built with MagnetRedirects, a request carrying an X-Api-Key header
// or userinfo refuses a magnet Location as a hop to another host, exactly
// as a client with Config.Credentials does (T-9099, DEC-156).
func TestMagnetRedirectRefusesACredentialedRequest(t *testing.T) {
	t.Parallel()

	cases := map[string]func(base string) Request{
		"X-Api-Key header": func(base string) Request {
			return Request{URL: base + "/api", Header: http.Header{"X-Api-Key": {requestAPIKey}}}
		},
		"userinfo": func(base string) Request {
			return Request{URL: credentialedURL("http", strings.TrimPrefix(base, "http://"), "/api")}
		},
	}

	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := magnetServer(t, http.StatusFound, testMagnet)

			_, err := magnetClient().Do(testContext(t), build(srv.URL))
			if !errors.Is(err, ErrCrossHostRedirect) {
				t.Fatalf("error = %v, want ErrCrossHostRedirect", err)
			}

			var mr *MagnetRedirectError
			if errors.As(err, &mr) {
				t.Errorf("a credentialed request surfaced the magnet: %v", err)
			}

			assertNoEcho(t, err, testMagnet)

			if srv.count() != 1 {
				t.Errorf("server saw %d requests, want 1", srv.count())
			}
		})
	}
}
