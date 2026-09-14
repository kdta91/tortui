package torznab

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kdta91/tortui/internal/indexer"
)

// feedDocument is a Torznab search response: an RSS 2.0 document whose items
// carry the torznab extension attributes.
//
// Every numeric field below is typed as a string and parsed afterwards. That
// is deliberate: encoding/xml fails the *whole document* when it cannot
// convert one value into an int, so a single item with seeders="n/a" would
// cost the user every other result in the feed. Parsing by hand turns that
// into one zero field on one result (AGENT.md §6.3, in miniature).
type feedDocument struct {
	Channel feedChannel `xml:"channel"`
}

// feedChannel is the single <channel> a Torznab feed carries.
type feedChannel struct {
	Items []feedItem `xml:"item"`
}

// feedItem is one <item> in the feed.
//
// The attr field has no namespace in its tag, so it matches <torznab:attr>,
// <newznab:attr>, and a bare <attr> alike: encoding/xml only checks the
// namespace when the struct tag names one. Servers differ on which prefix
// they use and tortui has no reason to care.
type feedItem struct {
	Title      string     `xml:"title"`
	GUID       feedGUID   `xml:"guid"`
	Comments   string     `xml:"comments"`
	Link       string     `xml:"link"`
	PubDate    string     `xml:"pubDate"`
	Size       string     `xml:"size"`
	Files      string     `xml:"files"`
	Grabs      string     `xml:"grabs"`
	Categories []string   `xml:"category"`
	Enclosure  feedLink   `xml:"enclosure"`
	Attrs      []feedAttr `xml:"attr"`
}

// feedGUID is the RSS <guid>, which may or may not claim to be a permalink.
type feedGUID struct {
	IsPermaLink string `xml:"isPermaLink,attr"`
	Value       string `xml:",chardata"`
}

// feedLink is the RSS <enclosure>: where the item's file actually lives.
type feedLink struct {
	Address string `xml:"url,attr"`
	Length  string `xml:"length,attr"`
	Type    string `xml:"type,attr"`
}

// feedAttr is one <torznab:attr name="..." value="..."/>.
type feedAttr struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// Torznab attribute names this adapter reads. They are the ones Jackett's
// src/Jackett.Common/Models/ResultPage.cs (branch master) actually writes,
// plus "leechers", which Jackett does not write but Prowlarr's
// schemas/torznab.xsd (branch develop) lists and both Prowlarr's and
// Sonarr's src/NzbDrone.Core/Indexers/.../TorznabRssParser.cs read. Verified
// by reading those files on 2026-09-14 rather than from memory; see DEC-065.
const (
	attrCategory      = "category"
	attrSize          = "size"
	attrSeeders       = "seeders"
	attrPeers         = "peers"
	attrLeechers      = "leechers"
	attrInfoHash      = "infohash"
	attrMagnet        = "magneturl"
	attrPubDate       = "pubdate"
	attrFiles         = "files"
	attrGrabs         = "grabs"
	attrDownloadRatio = "downloadvolumefactor"
	attrUploadRatio   = "uploadvolumefactor"
	attrMinRatio      = "minimumratio"
	attrMinSeedTime   = "minimumseedtime"
	attrUploader      = "uploader"
	attrPoster        = "poster"
	attrVerified      = "verified"
	attrTrusted       = "trusted"
	attrVIP           = "vip"
)

// extraAttrs are the attributes copied into Result.Extra for the details
// screen, under the key "torznab.<name>".
//
// The list is a whitelist and every value is copied only if it parses as a
// number. Both halves are credential safety, not tidiness: Result.Extra keys
// are free-form, internal/logging masks by key name, and a key like
// "torznab.grabs" is not on its sensitive list — so anything that reached
// this map holding a URL would be a plaintext api_key in the user's log
// file. A number cannot be one (AGENT.md §2; DEC-061, DEC-066).
var extraAttrs = []string{
	attrFiles,
	attrGrabs,
	attrDownloadRatio,
	attrUploadRatio,
	attrMinRatio,
	attrMinSeedTime,
}

// extraKeyPrefix namespaces every key this adapter writes into Result.Extra,
// so a source-specific field can never collide with the registry's own
// bookkeeping (indexer.ExtraKeySources).
const extraKeyPrefix = "torznab."

// pubDateLayouts are tried in order against an item's date.
//
// The first is the shape Torznab servers actually send: Jackett formats
// pubDate by hand as `ddd, dd MMM yyyy HH:mm:ss` plus a colon-stripped zone
// offset (src/Jackett.Common/Models/ResultPage.cs, XmlDateFormat, branch
// master), e.g. "Sat, 14 Mar 2015 17:10:42 -0400", and the newznab API
// specification's own examples match it ("Sun, 06 Jun 2010 17:29:23 +0100",
// docs/newznab_api_specification.txt in the nZEDb/nZEDb repository, branch
// dev). That is time.RFC1123Z. The rest are tolerance for servers that
// deviate: a named zone instead of an offset, a single-digit day, and
// RFC 3339 for the handful that emit an ISO timestamp.
var pubDateLayouts = []string{
	time.RFC1123Z,
	time.RFC1123,
	time.RFC822Z,
	time.RFC822,
	"Mon, 2 Jan 2006 15:04:05 -0700",
	"Mon, 2 Jan 2006 15:04:05 MST",
	time.RFC3339,
	"2006-01-02 15:04:05",
}

// attrIndex is an item's torznab attributes, lowercased by name. A name may
// map to several values: a server is free to repeat one (categories
// routinely are) and free to send an unusable value before a usable one.
type attrIndex map[string][]string

// index builds the attribute lookup for an item. Empty names and empty
// values are dropped, so "present" always means "present with something in
// it".
func (it feedItem) index() attrIndex {
	idx := make(attrIndex, len(it.Attrs))

	for _, a := range it.Attrs {
		name := strings.ToLower(strings.TrimSpace(a.Name))
		value := strings.TrimSpace(a.Value)

		if name == "" || value == "" {
			continue
		}

		idx[name] = append(idx[name], value)
	}

	return idx
}

// first returns the first value recorded for name.
func (idx attrIndex) first(name string) string {
	if values := idx[name]; len(values) > 0 {
		return values[0]
	}

	return ""
}

// int64At returns the first value of name that parses as an integer, and
// whether there was one. A non-numeric value is skipped rather than failing:
// servers do send "N/A", and a later duplicate is often the real number.
func (idx attrIndex) int64At(name string) (int64, bool) {
	for _, value := range idx[name] {
		if n, err := strconv.ParseInt(value, 10, 64); err == nil {
			return n, true
		}
	}

	return 0, false
}

// intAt is int64At narrowed to int, saturating rather than wrapping on a
// value that does not fit.
func (idx attrIndex) intAt(name string) (int, bool) {
	n, ok := idx.int64At(name)
	if !ok {
		return 0, false
	}

	return int(min(max(n, int64(minInt)), int64(maxInt))), true
}

// Platform-independent int bounds, for saturating a 64-bit feed value on a
// 32-bit build (AGENT.md §14 ships arm64 and amd64, but nothing here may
// assume that).
const (
	maxInt = int(^uint(0) >> 1)
	minInt = -maxInt - 1
)

// resultFrom maps one feed item onto the frozen indexer.Result contract.
//
// It cannot fail. Everything a source did not publish, published twice, or
// published as garbage becomes a zero value on one field; nothing here
// returns an error and nothing here drops an item, because a result the user
// asked for is worth showing even when half its metadata is unusable
// (AGENT.md §6.3, §13).
func resultFrom(indexerID string, it feedItem) indexer.Result {
	idx := it.index()

	magnet := magnetFrom(idx, it)
	hash := infoHashFrom(idx, magnet)
	seeders, leechers := swarmFrom(idx)

	res := indexer.Result{
		IndexerID:  indexerID,
		ID:         resultID(hash, it),
		Title:      strings.TrimSpace(it.Title),
		InfoHash:   hash,
		Magnet:     magnet,
		TorrentURL: torrentAddress(it),
		SizeBytes:  sizeFrom(idx, it),
		Seeders:    seeders,
		Leechers:   leechers,
		Category:   categoryFrom(idx, it),
		Published:  publishedFrom(idx, it),
		Uploader:   uploaderFrom(idx),
		Trust:      trustFrom(idx),
		SourceURL:  sourceAddress(it),
		Extra:      extraFrom(idx, it),
	}

	return res
}

// resultID is the stable identity of an item within this source.
//
// The infohash comes first because it is the strongest identity a torrent
// has and it cannot carry a credential: normaliseInfoHash returns a value
// only when it is 40 hex or 32 base32 characters, so no other text survives
// it. Everything after it can carry one, and only some of it is reduced:
//
//   - A URL-shaped guid, comments or link is reduced to scheme, host and
//     path by withoutQuery. That is the case worth reducing, because a
//     Torznab download URL carries the user's api_key in its query string
//     by protocol design.
//   - A guid, comments or link that is *not* URL-shaped is returned as it
//     stands. `<guid isPermaLink="false">…</guid>` is an opaque
//     source-chosen string and an api_key is an opaque token, so a source
//     that echoes the key there puts it in Result.ID verbatim — the item
//     does not have to be identity-less for that to happen. Reproduced by
//     QA on PR #12.
//   - The last fallback is the title, reduced to nothing either: for an
//     item that published no infohash, no guid, no comments and no link,
//     the title is the only identity left, and it is whatever text the
//     source sent.
//
// Result.ID is not one of the names internal/logging masks on, so any of
// those unreduced values is a plaintext key in the log file the moment
// anything logs the result (DEC-061, DEC-066).
//
// Keeping the title fallback rather than leaving such an item with an empty
// ID is deliberate: the leak is already present in Title verbatim by
// necessity, so dropping the fallback would remove a duplicate of text the
// Result carries anyway and buy no safety, while costing the only identity
// an otherwise-unidentifiable item has. Refusing a non-URL guid instead
// would throw away the source's own stable identity — the thing this
// function exists to produce — on a value shape that is ordinary rather
// than suspicious. Disclosed in the package doc and in DEC-071; the
// structural fix is backlog T-934.
func resultID(hash string, it feedItem) string {
	if hash != "" {
		return hash
	}

	for _, candidate := range []string{it.GUID.Value, it.Comments, it.Link} {
		if id := withoutQuery(candidate); id != "" {
			return id
		}
	}

	return strings.TrimSpace(it.Title)
}

// withoutQuery trims a candidate identifier and, when it is an absolute URL,
// strips its query and fragment. A non-URL is returned as it stands —
// including one that is an api_key a source echoed back, which is why
// resultID's doc comment lists this branch as a pass-through rather than a
// derivation.
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

// swarmFrom reads the swarm counts.
//
// seeders is unambiguous. The second number is not: Torznab's "peers" is
// documented as seeders + leechers, not leechers — verified in Prowlarr's
// schemas/torznab.xsd (branch develop), which annotates the attribute
// `<!-- seeders + leechers -->`; in Jackett, which accumulates
// `release.Peers = seeders + leechers` in its Cardigann engine
// (src/Jackett.Common/Indexers/Definitions/CardigannIndexer.cs, branch
// master); and in the consumer direction in Sonarr's and Prowlarr's
// TorznabRssParser.GetPeers, which falls back to seeders + leechers when no
// peers attribute is present. So leechers is the difference.
//
// Three cases, in this order (DEC-065):
//
//   - an explicit leechers attribute wins outright. Jackett does not write
//     one, but torznab.xsd lists it and both consumers above read it.
//   - otherwise leechers is peers - seeders.
//   - a peers value *below* seeders contradicts the attribute's documented
//     meaning, so nothing is inferred from it and leechers stays zero, which
//     is what indexer.Result already means by "the source reports none".
//     Reading it as a leecher count instead would be inventing a second
//     interpretation of an attribute that has one.
func swarmFrom(idx attrIndex) (seeders, leechers int) {
	seeders = nonNegative(idx, attrSeeders)

	if explicit, ok := idx.intAt(attrLeechers); ok {
		return seeders, max(explicit, 0)
	}

	peers, ok := idx.intAt(attrPeers)
	if !ok || peers <= seeders {
		return seeders, 0
	}

	return seeders, peers - seeders
}

// nonNegative reads an integer attribute, clamping a negative value to zero.
func nonNegative(idx attrIndex, name string) int {
	n, ok := idx.intAt(name)
	if !ok {
		return 0
	}

	return max(n, 0)
}

// sizeFrom reads the item's size in bytes: the torznab attribute first, then
// the plain <size> element Jackett writes alongside it, then the enclosure's
// declared length. A negative or unparseable value is treated as absent.
func sizeFrom(idx attrIndex, it feedItem) int64 {
	if n, ok := idx.int64At(attrSize); ok && n >= 0 {
		return n
	}

	for _, raw := range []string{it.Size, it.Enclosure.Length} {
		if n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil && n >= 0 {
			return n
		}
	}

	return 0
}

// categoryFrom maps the source's own category onto tortui's taxonomy.
//
// The mapping itself lives in internal/indexer (CategoryFromTorznab,
// CategoryFromString) and is deliberately not duplicated here: AGENT.md §13
// puts it in the adapter rather than the TUI, and one adapter reimplementing
// it would defeat that. Numeric ids are tried first, in the order the server
// listed them, and the first that lands on a real bucket wins; a worded
// <category> element is the fallback for a server that publishes labels.
// Nothing is ever dropped — an unplaceable category is CategoryOther.
func categoryFrom(idx attrIndex, it feedItem) indexer.Category {
	for _, raw := range idx[attrCategory] {
		if id, err := strconv.Atoi(raw); err == nil {
			if c := indexer.CategoryFromTorznab(id); c != indexer.CategoryOther {
				return c
			}
		}
	}

	for _, raw := range it.Categories {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}

		if id, err := strconv.Atoi(value); err == nil {
			if c := indexer.CategoryFromTorznab(id); c != indexer.CategoryOther {
				return c
			}

			continue
		}

		if c := indexer.CategoryFromString(value); c != indexer.CategoryOther {
			return c
		}
	}

	return indexer.CategoryOther
}

// publishedFrom parses the item's date, trying the layouts real servers
// send. An absent or unparseable date leaves Published at the zero time,
// which indexer.Result documents as "the source publishes no date" — a wrong
// date would sort into the results table and lie about the age column, so
// guessing is worse than admitting ignorance.
func publishedFrom(idx attrIndex, it feedItem) time.Time {
	for _, raw := range []string{it.PubDate, idx.first(attrPubDate)} {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}

		for _, layout := range pubDateLayouts {
			if t, err := time.Parse(layout, value); err == nil {
				return t
			}
		}
	}

	return time.Time{}
}

// uploaderFrom reads the uploader's name.
//
// No Torznab implementation writes an "uploader" attribute; the closest
// thing in the newznab API specification's attribute list is "poster", the
// NNTP poster (docs/newznab_api_specification.txt §4.1 in the nZEDb/nZEDb
// repository, branch dev). Both are read, and a URL-shaped value is refused:
// Result.Uploader is not a name internal/logging masks on, so a source that
// put a link there would otherwise write it — and any api_key in it — to the
// user's log file (DEC-066).
//
// What that guard does NOT do is make this a derived field. Any value
// without a "://" in it is returned exactly as the source wrote it, and an
// api_key is an opaque token with no "://" in it — so a source that echoes
// the key into <attr name="uploader" value="…"/> puts it in
// Result.Uploader verbatim, and logging the Result writes it out in
// plaintext. Reproduced by QA on PR #12 against the real internal/logging
// sink. The refusal is still worth keeping — a Torznab download URL is the
// one value here that carries the user's credential by protocol design, so
// keeping links out removes the likely case — but Uploader belongs with
// Title and Magnet on the pass-through side of the package doc's list, not
// with ID's stripping or Extra's numeric whitelist. Scrubbing it is not
// available: this package never sees the credential (DEC-061) and an
// uploader's name is legitimately an opaque token, so there is nothing to
// match on. Disclosed in the package doc, DEC-066 and DEC-071; the
// structural fix is backlog T-934.
func uploaderFrom(idx attrIndex) string {
	for _, name := range []string{attrUploader, attrPoster} {
		value := idx.first(name)
		if value == "" || strings.Contains(value, "://") {
			continue
		}

		return value
	}

	return ""
}

// trustFrom derives the uploader/upload trust badge.
//
// Nothing in the Torznab or Newznab world publishes one. Verified on
// 2026-09-14 by reading Jackett's src/Jackett.Common/Models/ResultPage.cs
// (branch master), Prowlarr's schemas/torznab.xsd (branch develop), and the
// attribute list in §4.1 of docs/newznab_api_specification.txt in the
// nZEDb/nZEDb repository (branch dev): none of the three contains a
// verified, trusted, or vip attribute. So this reads the three names a
// server would plausibly invent, and the honest answer for every server that
// exists today is TrustUnknown (DEC-068).
//
// The distinction between the two "no badge" answers is the point of the
// function. An attribute that is present and false is the source saying this
// uploader holds no badge (TrustNone); an attribute that is absent is the
// source saying nothing at all (TrustUnknown). indexer.Trust documents them
// as different information that sorts differently, so a value that is
// neither recognisably true nor recognisably false is treated as no
// statement rather than as a false one.
//
// Trust is display metadata. It gates nothing here and may gate nothing
// anywhere (AGENT.md §2).
func trustFrom(idx attrIndex) indexer.Trust {
	levels := []struct {
		name  string
		trust indexer.Trust
	}{
		{attrVIP, indexer.TrustVIP},
		{attrTrusted, indexer.TrustTrusted},
		{attrVerified, indexer.TrustVerified},
	}

	stated := false

	for _, level := range levels {
		set, ok := boolAttr(idx.first(level.name))
		if !ok {
			continue
		}

		if set {
			return level.trust
		}

		stated = true
	}

	if stated {
		return indexer.TrustNone
	}

	return indexer.TrustUnknown
}

// boolAttr reads a flag-shaped attribute value, reporting both what it says
// and whether it said anything recognisable at all.
func boolAttr(value string) (set, recognised bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y":
		return true, true
	case "0", "false", "no", "n":
		return false, true
	default:
		return false, false
	}
}

// magnetFrom returns the item's magnet URI: the magneturl attribute, or a
// <link> that is itself a magnet. Anything that is not a magnet URI is not
// one, however the server labelled it.
func magnetFrom(idx attrIndex, it feedItem) string {
	for _, candidate := range []string{idx.first(attrMagnet), strings.TrimSpace(it.Link)} {
		if isMagnet(candidate) {
			return candidate
		}
	}

	return ""
}

// magnetScheme is the prefix every magnet URI starts with.
const magnetScheme = "magnet:"

// isMagnet reports whether raw is a magnet URI.
func isMagnet(raw string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), magnetScheme)
}

// infoHashFrom returns the item's infohash, preferring the attribute and
// falling back to the xt parameter of the magnet the item already carries.
// Deriving it costs nothing and is worth doing: the registry merges results
// across sources on the infohash, so an item that only published a magnet
// would otherwise never merge with the same torrent from another source.
func infoHashFrom(idx attrIndex, magnet string) string {
	if hash := normaliseInfoHash(idx.first(attrInfoHash)); hash != "" {
		return hash
	}

	return normaliseInfoHash(infoHashInMagnet(magnet))
}

// btihPrefix is the URN that introduces a BitTorrent infohash in a magnet's
// xt parameter.
const btihPrefix = "urn:btih:"

// infoHashInMagnet pulls the btih value out of a magnet URI's xt parameters,
// or returns empty when there is none.
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

// Infohash string lengths: 40 hex characters for a v1 hash written in hex,
// 32 characters for the same hash written in base32.
const (
	hexHashLen    = 40
	base32HashLen = 32
)

// normaliseInfoHash validates an infohash and puts it in one canonical
// form — lowercase hex, or uppercase base32, which is how each encoding is
// conventionally written. Anything that is neither returns empty: a value
// this package cannot recognise is not an infohash, and passing it on would
// have the registry merge unrelated results on it.
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
// alphabet, case-insensitively and without padding (a 32-character hash
// never needs any).
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

// torrentAddress returns the URL of the item's .torrent file: the
// enclosure's, or a <link> that is an http(s) URL rather than a magnet.
//
// This value routinely carries the user's api_key in its query string, and
// that is correct — it has to, or the file cannot be fetched. It is safe
// because Result.TorrentURL is one of the field names internal/logging masks
// on. Nothing else in this package may hold a value like it under a name
// that is not (DEC-066).
func torrentAddress(it feedItem) string {
	for _, candidate := range []string{it.Enclosure.Address, it.Link} {
		if isWebAddress(candidate) {
			return strings.TrimSpace(candidate)
		}
	}

	return ""
}

// sourceAddress returns the human-viewable page for the item, which the `u`
// keybind opens.
//
// It is <comments> — the page a Torznab item points at for a human — or a
// guid that claims to be a permalink. It is deliberately never the
// enclosure or <link>, which are download URLs: opening one in a browser
// downloads a file instead of showing a page, and indexer.Result documents
// this field as the page.
func sourceAddress(it feedItem) string {
	if isWebAddress(it.Comments) {
		return strings.TrimSpace(it.Comments)
	}

	if set, ok := boolAttr(it.GUID.IsPermaLink); ok && set && isWebAddress(it.GUID.Value) {
		return strings.TrimSpace(it.GUID.Value)
	}

	return ""
}

// isWebAddress reports whether raw is an absolute http or https URL.
func isWebAddress(raw string) bool {
	value := strings.ToLower(strings.TrimSpace(raw))

	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

// extraFrom copies the whitelisted numeric attributes into Result.Extra for
// the details screen, and returns nil when there is nothing to copy so that
// a result carries no empty map.
//
// Only values that parse as a number are copied. See extraAttrs for why that
// restriction is credential safety rather than fussiness.
func extraFrom(idx attrIndex, it feedItem) map[string]string {
	extra := make(map[string]string, len(extraAttrs))

	for _, name := range extraAttrs {
		if value := idx.first(name); isNumeric(value) {
			extra[extraKeyPrefix+name] = value
		}
	}

	// Jackett writes files and grabs as plain elements rather than as
	// torznab attributes (src/Jackett.Common/Models/ResultPage.cs, branch
	// master), so both spellings are read; the attribute wins.
	for name, raw := range map[string]string{attrFiles: it.Files, attrGrabs: it.Grabs} {
		key := extraKeyPrefix + name
		if _, seen := extra[key]; seen {
			continue
		}

		if value := strings.TrimSpace(raw); isNumeric(value) {
			extra[key] = value
		}
	}

	if len(extra) == 0 {
		return nil
	}

	return extra
}

// isNumeric reports whether value is a plain number. Floats are accepted
// because the ratio attributes are fractional.
func isNumeric(value string) bool {
	if value == "" {
		return false
	}

	_, err := strconv.ParseFloat(value, 64)

	return err == nil
}
