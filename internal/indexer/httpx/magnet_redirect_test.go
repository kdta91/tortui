package httpx

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// magnetPasskey stands in for a private tracker's passkey inside a magnet's
// tr= address (T-9079). No error may ever carry it.
const magnetPasskey = "PASSKEY-9079"

// testMagnet is a synthetic magnet-only result's link: an invented hash, an
// invented title, and an example.org tracker whose path carries the passkey.
const testMagnet = "magnet:?xt=urn:btih:9079907990799079907990799079907990799079" +
	"&dn=Synthetic+Magnet+Corpus&tr=https%3A%2F%2Ftracker.example.org%2F" + magnetPasskey + "%2Fannounce"

// magnetServer answers every request with status and a Location of
// location.
func magnetServer(t *testing.T, status int, location string) *countingServer {
	t.Helper()

	return newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", location)
		w.WriteHeader(status)
	})
}

// magnetClient is the engine's .torrent client shape: no credentials, the
// subdomain rule, magnet redirects surfaced.
func magnetClient() *Client {
	return New(Config{MinHostInterval: -1, MaxAttempts: 1, FollowSubdomainRedirects: true, MagnetRedirects: true})
}

// assertNoEcho fails when err's text carries the Location or the passkey.
func assertNoEcho(t *testing.T, err error, location string) {
	t.Helper()

	if strings.Contains(err.Error(), location) || strings.Contains(err.Error(), magnetPasskey) ||
		strings.Contains(err.Error(), "magnet:") {
		t.Errorf("error echoes the Location: %v", err)
	}
}

// TestMagnetRedirectIsSurfacedForEveryRedirectStatus: on a client with
// MagnetRedirects, each redirect status whose Location is a magnet ends the
// request with a *MagnetRedirectError carrying that magnet byte for byte,
// naming only the host, after exactly one request.
func TestMagnetRedirectIsSurfacedForEveryRedirectStatus(t *testing.T) {
	t.Parallel()

	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()

			srv := magnetServer(t, status, testMagnet)

			_, err := magnetClient().Get(testContext(t), srv.URL+"/api?t=get&id=9079", nil)

			var mr *MagnetRedirectError
			if !errors.As(err, &mr) {
				t.Fatalf("error = %v, want a *MagnetRedirectError", err)
			}

			if !errors.Is(err, ErrMagnetRedirect) {
				t.Errorf("errors.Is(err, ErrMagnetRedirect) = false for %v", err)
			}

			if mr.Magnet() != testMagnet {
				t.Errorf("Magnet() = %q, want the Location byte for byte", mr.Magnet())
			}

			if want := srv.Listener.Addr().String(); mr.Host != want {
				t.Errorf("Host = %q, want %q", mr.Host, want)
			}

			assertNoEcho(t, err, testMagnet)

			if srv.count() != 1 {
				t.Errorf("server saw %d requests, want 1", srv.count())
			}
		})
	}
}

// TestMagnetRedirectAfterASameHostHop: a magnet reached through a same-host
// hop is surfaced too.
func TestMagnetRedirectAfterASameHostHop(t *testing.T) {
	t.Parallel()

	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/first" {
			http.Redirect(w, r, "/second", http.StatusFound)
			return
		}

		w.Header().Set("Location", testMagnet)
		w.WriteHeader(http.StatusFound)
	})

	_, err := magnetClient().Get(testContext(t), srv.URL+"/first", nil)

	var mr *MagnetRedirectError
	if !errors.As(err, &mr) || mr.Magnet() != testMagnet {
		t.Fatalf("error = %v, want the magnet surfaced", err)
	}

	if srv.count() != 2 {
		t.Errorf("server saw %d requests, want 2", srv.count())
	}
}

// TestMagnetRedirectPastTheChainLimitIsTooMany: the chain limit still
// applies to a magnet hop.
func TestMagnetRedirectPastTheChainLimitIsTooMany(t *testing.T) {
	t.Parallel()

	srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		n, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/"))
		if err != nil || n >= maxRedirects-1 {
			w.Header().Set("Location", testMagnet)
			w.WriteHeader(http.StatusFound)

			return
		}

		http.Redirect(w, r, "/"+strconv.Itoa(n+1), http.StatusFound)
	})

	_, err := magnetClient().Get(testContext(t), srv.URL+"/0", nil)
	if !errors.Is(err, ErrTooManyRedirects) {
		t.Fatalf("error = %v, want ErrTooManyRedirects", err)
	}

	assertNoEcho(t, err, testMagnet)
}

// TestUnusableMagnetRedirectIsRefusedWithoutEcho: a magnet: Location that
// fails validation is refused with ErrMagnetRedirectInvalid, naming the host
// and nothing of the Location.
func TestUnusableMagnetRedirectIsRefusedWithoutEcho(t *testing.T) {
	t.Parallel()

	const hex40 = "9079907990799079907990799079907990799079"

	tr := "&tr=https%3A%2F%2Ftracker.example.org%2F" + magnetPasskey + "%2Fannounce"

	cases := map[string]string{
		"no question mark":     "magnet:xt=urn:btih:" + hex40 + tr,
		"no xt":                "magnet:?dn=Synthetic" + tr,
		"xt is not btih":       "magnet:?xt=urn:sha1:" + hex40 + tr,
		"btih prefix in caps":  "magnet:?xt=urn:BTIH:" + hex40 + tr,
		"hash too short":       "magnet:?xt=urn:btih:" + hex40[:39] + tr,
		"hash too long":        "magnet:?xt=urn:btih:" + hex40 + "9" + tr,
		"hash not hex":         "magnet:?xt=urn:btih:" + hex40[:39] + "g" + tr,
		"base32 lower case":    "magnet:?xt=urn:btih:" + strings.Repeat("abcd", 8) + tr,
		"base32 bad character": "magnet:?xt=urn:btih:" + strings.Repeat("ABC1", 8) + tr,
		"two hashes":           "magnet:?xt=urn:btih:" + hex40 + "&xt=urn:btih:" + strings.Repeat("ab", 20) + tr,
		"bad escape":           "magnet:?xt=urn:btih:" + hex40 + "&dn=%zz" + tr,
		"semicolon":            "magnet:?xt=urn:btih:" + hex40 + ";dn=x" + tr,
		"fragment":             "magnet:?xt=urn:btih:" + hex40 + tr + "#frag",
		"space":                "magnet:?xt=urn:btih:" + hex40 + "&dn=a b" + tr,
		"over the length cap":  "magnet:?xt=urn:btih:" + hex40 + tr + "&dn=" + strings.Repeat("x", maxMagnetBytes),
		"magnet with a host":   "magnet://tracker.example.org/?xt=urn:btih:" + hex40 + tr,
	}

	for name, location := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := magnetServer(t, http.StatusFound, location)

			_, err := magnetClient().Get(testContext(t), srv.URL+"/api", nil)
			if !errors.Is(err, ErrMagnetRedirectInvalid) {
				t.Fatalf("error = %v, want ErrMagnetRedirectInvalid", err)
			}

			var mr *MagnetRedirectError
			if errors.As(err, &mr) {
				t.Errorf("an unusable magnet was surfaced: %v", err)
			}

			if !strings.Contains(err.Error(), srv.Listener.Addr().String()) {
				t.Errorf("error does not name the host: %v", err)
			}

			assertNoEcho(t, err, location)
		})
	}
}

// TestValidMagnetAcceptsBothInfohashEncodings pins the two accepted shapes,
// with a v2 hash beside the v1 one left to the engine.
func TestValidMagnetAcceptsBothInfohashEncodings(t *testing.T) {
	t.Parallel()

	valid := []string{
		"magnet:?xt=urn:btih:" + strings.Repeat("aB", 20),
		"magnet:?xt=urn:btih:" + strings.Repeat("ABCDEFGHIJKLMNOPQRSTUVWXYZ234567", 1),
		"magnet:?xt=urn:btih:" + strings.Repeat("ab", 20) + "&xt=urn:btmh:1220" + strings.Repeat("cd", 32),
		testMagnet,
	}

	for _, s := range valid {
		if !validMagnet(s) {
			t.Errorf("validMagnet(%q) = false, want true", s)
		}
	}

	if validMagnet(testMagnet[:len(magnetPrefix)-1]) || validMagnet("") {
		t.Error("validMagnet accepted a truncated or empty link")
	}

	atCap := "magnet:?xt=urn:btih:" + strings.Repeat("ab", 20) + "&dn="
	atCap += strings.Repeat("x", maxMagnetBytes-len(atCap))

	if !validMagnet(atCap) || validMagnet(atCap+"x") {
		t.Errorf("the length cap is not exactly %d bytes", maxMagnetBytes)
	}
}

// TestMagnetRedirectRefusedWithoutTheOption: every client that does not opt
// in — the default, and an indexer client carrying the user's credentials —
// still refuses a magnet redirect as a hop to another host, unchanged, and
// without echoing it.
func TestMagnetRedirectRefusedWithoutTheOption(t *testing.T) {
	t.Parallel()

	cases := map[string]Config{
		"default":               {},
		"indexer with a key":    {Credentials: testCredentials()},
		"subdomain client only": {FollowSubdomainRedirects: true},
	}

	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := magnetServer(t, http.StatusFound, testMagnet)
			cfg.MinHostInterval = -1
			cfg.MaxAttempts = 1

			_, err := New(cfg).Get(testContext(t), srv.URL+"/api", nil)
			if !errors.Is(err, ErrCrossHostRedirect) {
				t.Fatalf("error = %v, want ErrCrossHostRedirect", err)
			}

			if errors.Is(err, ErrMagnetRedirect) {
				t.Errorf("a client without the option surfaced the magnet: %v", err)
			}

			assertNoEcho(t, err, testMagnet)
		})
	}
}

// TestMagnetRedirectsLeaveHTTPHopsAlone: with the option on, a cross-host
// http(s) hop is still refused exactly as before (DEC-136) and a same-host
// one is still followed.
func TestMagnetRedirectsLeaveHTTPHopsAlone(t *testing.T) {
	t.Parallel()

	t.Run("cross-host refused", func(t *testing.T) {
		t.Parallel()

		other := statusServer(t, http.StatusOK)
		srv := magnetServer(t, http.StatusFound, other.URL+"/x.torrent")

		_, err := magnetClient().Get(testContext(t), srv.URL+"/api", nil)
		if !errors.Is(err, ErrCrossHostRedirect) {
			t.Fatalf("error = %v, want ErrCrossHostRedirect", err)
		}

		if other.count() != 0 {
			t.Errorf("the other host was contacted %d times", other.count())
		}
	})

	t.Run("same-host followed", func(t *testing.T) {
		t.Parallel()

		srv := newServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api" {
				http.Redirect(w, r, "/x.torrent", http.StatusFound)
				return
			}

			_, _ = w.Write([]byte("torrent-bytes"))
		})

		resp, err := magnetClient().Get(testContext(t), srv.URL+"/api", nil)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}

		if string(resp.Body) != "torrent-bytes" {
			t.Errorf("Body = %q, want the same-host file", resp.Body)
		}
	})
}

// TestMagnetRedirectsNeedACredentialFreeClient: MagnetRedirects takes
// effect only on a client with no Credentials, the way
// FollowSubdomainRedirects does (T-9095). A client carrying an api key or a
// cookie refuses a magnet Location as a hop to another host even with the
// option set, while the credential-free client surfaces it.
func TestMagnetRedirectsNeedACredentialFreeClient(t *testing.T) {
	t.Parallel()

	refused := map[string]Credentials{
		"api key":        {APIKey: testAPIKey},
		"cookie":         {CookieHeader: testSessionValue},
		"key and cookie": testCredentials(),
	}

	for name, creds := range refused {
		t.Run("refused with "+name, func(t *testing.T) {
			t.Parallel()

			srv := magnetServer(t, http.StatusFound, testMagnet)

			client := New(Config{MinHostInterval: -1, MaxAttempts: 1, Credentials: creds, MagnetRedirects: true})

			_, err := client.Get(testContext(t), srv.URL+"/api", nil)
			if !errors.Is(err, ErrCrossHostRedirect) {
				t.Fatalf("error = %v, want ErrCrossHostRedirect", err)
			}

			var mr *MagnetRedirectError
			if errors.Is(err, ErrMagnetRedirect) || errors.As(err, &mr) {
				t.Errorf("a credentialed client surfaced the magnet: %v", err)
			}

			assertNoEcho(t, err, testMagnet)

			if srv.count() != 1 {
				t.Errorf("server saw %d requests, want 1", srv.count())
			}
		})
	}

	t.Run("followed without credentials", func(t *testing.T) {
		t.Parallel()

		srv := magnetServer(t, http.StatusFound, testMagnet)

		client := New(Config{MinHostInterval: -1, MaxAttempts: 1, MagnetRedirects: true})

		_, err := client.Get(testContext(t), srv.URL+"/api", nil)

		var mr *MagnetRedirectError
		if !errors.As(err, &mr) {
			t.Fatalf("error = %v, want a *MagnetRedirectError", err)
		}

		if mr.Magnet() != testMagnet {
			t.Errorf("Magnet() = %q, want the Location byte for byte", mr.Magnet())
		}
	})
}
