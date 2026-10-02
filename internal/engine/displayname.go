package engine

import (
	"net/url"
	"strings"
	"unicode"
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
//
// The test is made on the name with its invisible runes removed (T-9099,
// DEC-155): control and format (Cf) runes, variation selectors, and the
// other default-ignorable runes. So a NUL, a C0 byte, U+200B, U+FEFF or
// U+034F before, inside or just after the scheme cannot hide an address. An
// http or https scheme is an address even with no host
// ("https:host?apikey=..."). The name returned when it is not an address is
// the one given, invisible runes and all.
func SafeName(name string) string {
	if isAddress(strings.TrimSpace(strings.Map(dropInvisible, name))) {
		return URLSourceName(name)
	}

	return name
}

// isAddress reports whether s, already stripped of invisible runes, is an
// address SafeName must not show: a scheme and a host; an http or https
// scheme with no host; or, when url.Parse refuses s, a scheme and "://".
func isAddress(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return hasSchemePrefix(s)
	}

	if u.Scheme == "http" || u.Scheme == "https" {
		return true
	}

	return u.Scheme != "" && u.Host != ""
}

// dropInvisible is a strings.Map function that removes a control rune, a
// Unicode format (Cf) rune — zero-width spaces, joiners, byte order marks —,
// a variation selector, or another default-ignorable rune such as U+034F,
// and keeps every other rune.
func dropInvisible(r rune) rune {
	if unicode.IsControl(r) ||
		unicode.In(r, unicode.Cf, unicode.Variation_Selector,
			unicode.Other_Default_Ignorable_Code_Point) {
		return -1
	}

	return r
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
