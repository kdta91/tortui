package engine

import (
	"net/url"
	"strings"
)

// urlSourceNameBase is the display name of a torrent added by a .torrent
// address whose host cannot be read.
const urlSourceNameBase = "torrent file"

// URLSourceName is the display name for a torrent added by a .torrent
// address, for as long as the torrent's own name is not known: the address's
// host, and nothing else. The path, query, fragment, and userinfo are never
// kept, because an indexer manager puts the user's api key in the query and a
// private tracker its passkey in the path (T-9057). An address with no
// readable host is named "torrent file".
func URLSourceName(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Hostname() == "" {
		return urlSourceNameBase
	}

	return urlSourceNameBase + " from " + strings.ToLower(u.Hostname())
}

// SafeName returns name unchanged unless it is a web address — a scheme and a
// host, as a torrent added by address was named before T-9057 and as a
// session saved then still records it — in which case it returns
// URLSourceName of it. Every place that shows a torrent's name, or keeps one
// to show later, passes it through here.
//
// A name url.Parse refuses is an address too when it opens with a scheme and
// "://" (T-9095, DEC-154): a bad escape, port, bracket or control byte must
// not hand back the api key in its query. Its host cannot be read, so
// URLSourceName names it "torrent file".
func SafeName(name string) string {
	trimmed := strings.TrimSpace(name)

	u, err := url.Parse(trimmed)
	if err != nil {
		if hasSchemePrefix(trimmed) {
			return URLSourceName(name)
		}

		return name
	}

	if u.Scheme == "" || u.Host == "" {
		return name
	}

	return URLSourceName(name)
}

// hasSchemePrefix reports whether s opens with a URI scheme (RFC 3986: a
// letter, then letters, digits, "+", "-" or ".") followed by "://".
func hasSchemePrefix(s string) bool {
	scheme, _, found := strings.Cut(s, "://")
	if !found || scheme == "" {
		return false
	}

	for i, r := range scheme {
		letter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if i == 0 && !letter {
			return false
		}

		if !letter && (r < '0' || r > '9') && r != '+' && r != '-' && r != '.' {
			return false
		}
	}

	return true
}
