package scraper

import (
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kdta91/tortui/internal/indexer"
)

// row is one result row, in whichever shape the response arrived in: an
// element and its descendants for an html definition, one element of the
// rows array for a json one. It is the only thing the mapping below knows
// about the response format.
type row interface {
	// text returns the raw value a field's selector addresses, or the
	// empty string when it addresses nothing.
	text(f *fieldPlan) string
}

// maxInt is the platform's largest int, for saturating a page value that
// does not fit on a 32-bit build.
const maxInt = int(^uint(0) >> 1)

// magnetScheme is the prefix every magnet URI starts with, and btihPrefix
// is the URN that introduces a BitTorrent infohash in one.
const (
	magnetScheme = "magnet:"
	btihPrefix   = "urn:btih:"
)

// Infohash string lengths: 40 hex characters for a v1 hash written in hex,
// 32 for the same hash written in base32.
const (
	hexHashLen    = 40
	base32HashLen = 32
)

// defaultTimeLayouts are tried, in order, after any the definition
// supplied. They are the shapes a listing page actually prints a date in;
// a definition whose site prints something else says so with `layouts`.
//
// An unparseable date leaves Published at the zero time, which
// indexer.Result documents as "the source publishes no date". A wrong date
// would sort into the results table and lie about the age column, so
// guessing is worse than admitting ignorance.
var defaultTimeLayouts = []string{
	time.RFC3339,
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	"02 Jan 2006",
	"2 Jan 2006",
	"Jan 2, 2006",
	"Jan 02, 2006",
	time.RFC1123Z,
	time.RFC1123,
}

// sizeUnits maps a unit suffix onto its multiplier.
//
// Both spellings of every unit are 1024-based. Listing pages are
// inconsistent about writing GB when they mean GiB, and the two readings
// differ by 7% at that scale — invisible in a size column, where the
// number exists so a user can tell a 700MB item from a 4GB one. Reading GB
// as 1000³ instead would misreport every site that meant GiB by the same
// amount in the other direction, with nothing gained.
var sizeUnits = map[string]int64{
	"":      1,
	"b":     1,
	"byte":  1,
	"bytes": 1,
	"k":     1 << 10,
	"kb":    1 << 10,
	"kib":   1 << 10,
	"m":     1 << 20,
	"mb":    1 << 20,
	"mib":   1 << 20,
	"g":     1 << 30,
	"gb":    1 << 30,
	"gib":   1 << 30,
	"t":     1 << 40,
	"tb":    1 << 40,
	"tib":   1 << 40,
	"p":     1 << 50,
	"pb":    1 << 50,
	"pib":   1 << 50,
}

// value reads one field out of one row and applies the field's regex and
// transform chain.
//
// The raw value is whitespace-trimmed first, then narrowed by the regex if
// there is one, then passed through the transforms in order. A field the
// block does not map, a selector that matches nothing, and a regex that
// does not match all produce the empty string.
func value(b *blockPlan, r row, name string) string {
	f, ok := b.fields[name]
	if !ok {
		return ""
	}

	return f.read(r)
}

// read applies one compiled field to one row.
func (f *fieldPlan) read(r row) string {
	out := strings.TrimSpace(r.text(f))

	if f.regex != nil {
		out = firstMatch(f.regex, out)
	}

	for _, fn := range f.transforms {
		out = fn(out)
	}

	return out
}

// firstMatch returns the first capture group of the first match, or the
// whole match when the expression has no group, or the empty string when
// it does not match.
func firstMatch(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if m == nil {
		return ""
	}

	if len(m) > 1 {
		return m[1]
	}

	return m[0]
}

// resultFrom maps one row onto the frozen indexer.Result contract.
//
// It cannot fail. Everything the page did not carry, or carried as
// something unusable, becomes a zero value on one field — never an error
// and never a dropped result, because a result the user asked for is worth
// showing even when half its metadata is missing (AGENT.md §6.3, §13).
//
// The one row it does drop is a row with no title, which is what a table's
// header row, a spacer row, and an advertisement between results all look
// like. The second return reports that.
func (p *plan) resultFrom(b *blockPlan, r row) (indexer.Result, bool) {
	title := value(b, r, fieldTitle)
	if title == "" {
		return indexer.Result{}, false
	}

	magnet := magnetFrom(value(b, r, fieldMagnet))
	hash := infoHashFrom(value(b, r, fieldInfoHash), magnet)
	torrent := p.absolute(value(b, r, fieldTorrent))
	page := p.absolute(value(b, r, fieldSource))

	res := indexer.Result{
		IndexerID:  p.id,
		ID:         identity(value(b, r, fieldID), hash, page, torrent, title),
		Title:      title,
		InfoHash:   hash,
		Magnet:     magnet,
		TorrentURL: torrent,
		SizeBytes:  parseSize(value(b, r, fieldSize)),
		Seeders:    parseCount(value(b, r, fieldSeeders)),
		Leechers:   parseCount(value(b, r, fieldLeechers)),
		Category:   indexer.CategoryFromString(value(b, r, fieldCategory)),
		Published:  parseTime(value(b, r, fieldPublished), b.layoutsFor(fieldPublished)),
		Uploader:   value(b, r, fieldUploader),
		Trust:      b.trustFrom(r),
		SourceURL:  page,
	}

	return res, true
}

// layoutsFor returns the layouts a field's definition supplied.
func (b *blockPlan) layoutsFor(name string) []string {
	if f, ok := b.fields[name]; ok {
		return f.layouts
	}

	return nil
}

// trustFrom reads the row's trust badge and maps it onto indexer.Trust.
//
// A definition with no trust block, a selector that matches nothing, and a
// value the mapping does not list all yield TrustUnknown — "this source
// said nothing", which indexer.Trust documents as different information
// from TrustNone and sorts differently. A definition that wants the
// positive "no badge" answer says so by mapping the value its site prints
// for it, which is why the mapping is the user's rather than inferred.
func (b *blockPlan) trustFrom(r row) indexer.Trust {
	if b.trust == nil {
		return indexer.TrustUnknown
	}

	read := strings.ToLower(strings.TrimSpace(b.trust.field.read(r)))
	if read == "" {
		return indexer.TrustUnknown
	}

	if level, ok := b.trust.values[read]; ok {
		return level
	}

	return indexer.TrustUnknown
}

// identity is the stable id of an item within this source.
//
// The order is strongest-identity-first: what the definition explicitly
// selected, then the infohash, then a details or download address with its
// query string removed, then the title. Only the infohash and the
// query-stripped addresses are derived; see the package doc's field list
// for which branches pass source text through and what that means.
func identity(selected, hash, page, torrent, title string) string {
	if selected != "" {
		return selected
	}

	if hash != "" {
		return hash
	}

	for _, candidate := range []string{page, torrent} {
		if id := withoutQuery(candidate); id != "" {
			return id
		}
	}

	return title
}

// withoutQuery trims a candidate identifier and, when it is an absolute
// URL, strips its query and fragment — which is where a source's own
// per-user token lives when it has one. A value that is not URL-shaped is
// returned as it stands.
func withoutQuery(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" {
		return trimmed
	}

	stripped := *parsed
	stripped.RawQuery = ""
	stripped.ForceQuery = false
	stripped.Fragment = ""
	stripped.RawFragment = ""

	return stripped.String()
}

// absolute resolves a link read off the page against the definition's base
// address, and refuses anything that is not http or https afterwards.
//
// Both halves matter. A listing page's links are usually relative, and a
// relative link is useless to the engine and to the browser the `u` keybind
// opens. And a page is attacker-controlled: an href of "javascript:..." or
// "file:///..." resolves to itself, and Result.SourceURL is a value tortui
// hands to the operating system's opener (AGENT.md §7). Refusing every
// other scheme here is where that stops.
func (p *plan) absolute(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	ref, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}

	resolved := p.base.ResolveReference(ref)

	switch strings.ToLower(resolved.Scheme) {
	case "http", "https":
		return resolved.String()
	default:
		return ""
	}
}

// magnetFrom returns the value only if it really is a magnet URI. A
// definition whose magnet selector is pointed at the wrong attribute would
// otherwise put an ordinary href in Result.Magnet, and the engine would
// fail on it later and further away.
func magnetFrom(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if isMagnet(trimmed) {
		return trimmed
	}

	return ""
}

// isMagnet reports whether raw is a magnet URI.
func isMagnet(raw string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), magnetScheme)
}

// infoHashFrom returns the row's infohash, preferring the mapped field and
// falling back to the xt parameter of a magnet the row already carries.
// Deriving it costs nothing and is worth doing: the registry merges results
// across sources on the infohash, so an item that only published a magnet
// would otherwise never merge with the same torrent from another source.
func infoHashFrom(raw, magnet string) string {
	if hash := normaliseInfoHash(raw); hash != "" {
		return hash
	}

	return normaliseInfoHash(infoHashInMagnet(magnet))
}

// infoHashInMagnet pulls the btih value out of a magnet URI's xt
// parameters, or returns empty when there is none.
func infoHashInMagnet(magnet string) string {
	if !isMagnet(magnet) {
		return ""
	}

	parsed, err := url.Parse(magnet)
	if err != nil {
		return ""
	}

	for _, xt := range parsed.Query()["xt"] {
		if strings.HasPrefix(strings.ToLower(xt), btihPrefix) {
			return xt[len(btihPrefix):]
		}
	}

	return ""
}

// normaliseInfoHash validates an infohash and puts it in one canonical
// form — lowercase hex, or uppercase base32. Anything that is neither
// returns empty: a value this package cannot recognise is not an infohash,
// and passing it on would have the registry merge unrelated results on it.
//
// This duplicates the same helper in the torznab adapter, deliberately.
// AGENT.md §4 forbids one adapter importing another, and the alternative —
// exporting it from internal/indexer — would grow the package that holds
// the frozen §5 contracts for the sake of forty lines. Recorded as backlog
// T-937 so a third adapter makes the decision again with three data
// points rather than two.
func normaliseInfoHash(raw string) string {
	value := strings.TrimSpace(raw)

	switch len(value) {
	case hexHashLen:
		if isHex(value) {
			return strings.ToLower(value)
		}
	case base32HashLen:
		if isBase32(value) {
			return strings.ToUpper(value)
		}
	}

	return ""
}

// isHex reports whether every character is a hexadecimal digit.
func isHex(s string) bool {
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}

	return true
}

// isBase32 reports whether every character is in the RFC 4648 base32
// alphabet, case-insensitively and without padding.
func isBase32(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '2' && r <= '7':
		default:
			return false
		}
	}

	return true
}

// magnetFor builds a magnet URI from an infohash and a display name.
//
// The display name is Result.Title, which this adapter passes through
// verbatim, so the magnet this returns is no cleaner than the title it was
// given: url.QueryEscape encodes that text, it does not remove it, and it
// leaves an opaque token untouched. A magnet Resolve derives therefore sits
// in the same disclosed gap as Title itself — see the package doc's field
// list, DEC-071 and backlog T-934.
func magnetFor(hash, title string) string {
	magnet := magnetScheme + "?xt=" + btihPrefix + hash

	if name := strings.TrimSpace(title); name != "" {
		magnet += "&dn=" + url.QueryEscape(name)
	}

	return magnet
}

// parseSize reads a size in bytes out of what a listing page prints:
// "1.4 GiB", "700 MB", "1,024", "1503238553".
//
// A bare number is bytes. Separators are handled by sizeSeparators. An
// unreadable value, a negative one, and a value with a unit this package
// does not know all return zero, which indexer.Result documents as "the
// source does not publish one".
func parseSize(raw string) int64 {
	number, unit := splitSize(raw)
	if number == "" {
		return 0
	}

	amount, err := strconv.ParseFloat(number, 64)
	if err != nil || amount < 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return 0
	}

	multiplier, ok := sizeUnits[unit]
	if !ok {
		return 0
	}

	bytes := amount * float64(multiplier)
	if bytes >= math.MaxInt64 {
		return math.MaxInt64
	}

	return int64(bytes)
}

// splitSize separates the numeric part of a size from its unit, and
// normalises the separators in the number.
//
// A comma followed by exactly three digits is a thousands separator and is
// removed; any other comma is a decimal point and becomes one. That covers
// both "1,024 MB" and "1,4 GiB" without asking the definition which
// convention its site uses.
func splitSize(raw string) (number, unit string) {
	var digits strings.Builder

	rest := strings.TrimSpace(raw)

	for i := 0; i < len(rest); i++ {
		c := rest[i]

		switch {
		case c >= '0' && c <= '9':
			digits.WriteByte(c)
		case c == '.':
			digits.WriteByte('.')
		case c == ',':
			if isThousandsSeparator(rest, i) {
				continue
			}

			digits.WriteByte('.')
		default:
			return digits.String(), sizeUnit(rest[i:])
		}
	}

	return digits.String(), ""
}

// isThousandsSeparator reports whether the comma at i is followed by
// exactly three digits.
func isThousandsSeparator(s string, i int) bool {
	tail := s[i+1:]

	if len(tail) < 3 {
		return false
	}

	for j := 0; j < 3; j++ {
		if tail[j] < '0' || tail[j] > '9' {
			return false
		}
	}

	return len(tail) == 3 || tail[3] < '0' || tail[3] > '9'
}

// sizeUnit reads the unit suffix out of the tail of a size, lowercased and
// stripped of everything that is not a letter.
func sizeUnit(tail string) string {
	var unit strings.Builder

	for _, r := range tail {
		switch {
		case r >= 'a' && r <= 'z':
			unit.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			unit.WriteRune(r + ('a' - 'A'))
		case r == ' ' || r == '\t' || r == ' ':
			if unit.Len() > 0 {
				return unit.String()
			}
		default:
			return unit.String()
		}
	}

	return unit.String()
}

// parseCount reads a swarm count: the first run of digits in the value,
// saturating rather than wrapping, and zero for anything unreadable. A
// count is never negative, so a value that reads as one is zero.
func parseCount(raw string) int {
	var digits strings.Builder

	for _, r := range raw {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)

			continue
		}

		if digits.Len() > 0 {
			break
		}
	}

	if digits.Len() == 0 {
		return 0
	}

	n, err := strconv.ParseInt(digits.String(), 10, 64)
	if err != nil {
		// The only way ParseInt fails on a run of digits is a range
		// error, which means the page printed a number bigger than a
		// swarm can be.
		return maxInt
	}

	return int(min(max(n, 0), int64(maxInt)))
}

// parseTime parses a published date, trying the definition's layouts first
// and the built-in ones after. An unparseable value leaves the zero time.
func parseTime(raw string, layouts []string) time.Time {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}
	}

	for _, layout := range layouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t
		}
	}

	for _, layout := range defaultTimeLayouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t
		}
	}

	return time.Time{}
}
