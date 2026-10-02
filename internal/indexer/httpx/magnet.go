package httpx

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// magnetPrefix is how every magnet URI this package surfaces begins.
const magnetPrefix = "magnet:?"

// magnetBTIH is the prefix of the xt value that carries a v1 infohash.
const magnetBTIH = "urn:btih:"

// maxMagnetBytes caps a magnet URI taken from a redirect. Real ones are a
// few hundred bytes, a long tracker list a few kilobytes; anything near
// this is not a magnet a source meant a user to add.
const maxMagnetBytes = 8 << 10

// ErrMagnetRedirect is what a *MagnetRedirectError matches with errors.Is.
var ErrMagnetRedirect = errors.New("redirected to a magnet link")

// ErrMagnetRedirectInvalid reports a redirect to a magnet: Location that is
// not a usable magnet URI: it does not begin "magnet:?", names no single
// valid v1 infohash, carries a fragment, whitespace or a control byte, or
// is over the length cap. Like every refusal here it names the host only:
// the Location is server-chosen text and a magnet's tracker addresses can
// carry a passkey (T-9079, DEC-151).
var ErrMagnetRedirectInvalid = errors.New("refusing a redirect to a magnet link that is not usable")

// MagnetRedirectError is returned, on a credential-free client built with
// Config.MagnetRedirects, when a request is answered by a redirect (301,
// 302, 303, 307 or 308) whose Location is a magnet URI. It is not a
// transport failure: a Torznab aggregator answers a result's download
// address this way when the indexer behind it publishes only magnets.
// Nothing is requested from the magnet; it is handed to the caller, already
// validated (see validMagnet), through Magnet.
//
// Its message names the host that answered and never the magnet, whose
// tracker addresses can carry a passkey (T-9079, DEC-151). It carries no
// "httpx:" prefix of its own: the client's error adds that once (T-9055).
type MagnetRedirectError struct {
	// Host is the host:port that answered with the redirect.
	Host string

	magnet string
}

// Error names the host and never the magnet.
func (e *MagnetRedirectError) Error() string {
	return fmt.Sprintf("%v (at %s)", ErrMagnetRedirect, e.Host)
}

// Unwrap makes errors.Is(err, ErrMagnetRedirect) hold.
func (e *MagnetRedirectError) Unwrap() error { return ErrMagnetRedirect }

// Magnet returns the validated magnet URI exactly as the Location carried
// it. It is credential-bearing text: never log or display it.
func (e *MagnetRedirectError) Magnet() string { return e.magnet }

// withMagnetRedirects wraps a redirect rule so a hop to a magnet: target
// ends the request with a *MagnetRedirectError (or ErrMagnetRedirectInvalid)
// instead of being judged, and refused, as a hop to another host. Every
// other hop, and a magnet one past the chain limit, goes to next unchanged.
func withMagnetRedirects(next func(*http.Request, []*http.Request) error) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects || !strings.EqualFold(req.URL.Scheme, "magnet") {
			return next(req, via)
		}

		host := hostOf(via[len(via)-1].URL)

		var location string
		if req.Response != nil {
			location = req.Response.Header.Get("Location")
		}

		if !validMagnet(location) {
			return fmt.Errorf("%w (at %s)", ErrMagnetRedirectInvalid, host)
		}

		return &MagnetRedirectError{Host: host, magnet: location}
	}
}

// validMagnet reports whether s is a magnet URI tortui will add: under the
// length cap, beginning "magnet:?", with no fragment, whitespace or control
// byte, a query that parses, and exactly one xt=urn:btih: value naming a v1
// infohash as 40 hex digits or 32 base32 characters. Other xt values (a v2
// urn:btmh: beside the v1 one) are left to the engine's own parser.
func validMagnet(s string) bool {
	if len(s) > maxMagnetBytes || !strings.HasPrefix(s, magnetPrefix) {
		return false
	}

	if strings.ContainsFunc(s, func(r rune) bool { return r <= ' ' || r == 0x7f || r == '#' }) {
		return false
	}

	query, err := url.ParseQuery(s[len(magnetPrefix):])
	if err != nil {
		return false
	}

	hashes := 0

	for _, xt := range query["xt"] {
		encoded, ok := strings.CutPrefix(xt, magnetBTIH)
		if !ok {
			continue
		}

		if !validInfohash(encoded) {
			return false
		}

		hashes++
	}

	return hashes == 1
}

// validInfohash reports whether s is a v1 infohash as a magnet writes one:
// 40 hex digits, or 32 characters of the RFC 4648 base32 alphabet
// (upper case, as the engine's decoder requires).
func validInfohash(s string) bool {
	switch len(s) {
	case 40:
		return !strings.ContainsFunc(s, func(r rune) bool {
			return (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F')
		})
	case 32:
		return !strings.ContainsFunc(s, func(r rune) bool {
			return (r < 'A' || r > 'Z') && (r < '2' || r > '7')
		})
	default:
		return false
	}
}
