package torznab

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/kdta91/tortui/internal/indexer"
)

// capsDocument is a Torznab `t=caps` response.
//
// The shape is what servers actually publish, read on 2026-09-14 from
// Jackett's src/Jackett.Common/Models/TorznabCapabilities.cs (GetXDocument,
// branch master) and from §2.1 of docs/newznab_api_specification.txt in the
// nZEDb/nZEDb repository (branch dev). Only the parts tortui can act on are
// modelled: the rest of the document — server identity, retention,
// registration, the per-media search modes — describes things this
// application deliberately has no concept of (AGENT.md §2).
//
// As in the feed, every value is a string and is parsed afterwards, so one
// unparseable number never costs the whole document.
type capsDocument struct {
	Limits     capsLimits     `xml:"limits"`
	Searching  capsSearching  `xml:"searching"`
	Categories capsCategories `xml:"categories"`
}

// capsLimits is the <limits> element, which declares how many results a
// request may ask for. Jackett omits the element entirely when it has
// neither number to publish.
type capsLimits struct {
	Max     string `xml:"max,attr"`
	Default string `xml:"default,attr"`
}

// capsSearching is the <searching> element. Only plain keyword <search> is
// modelled; the media-specific modes next to it (tv-search, movie-search and
// friends) are subject-matter searches tortui does not offer.
type capsSearching struct {
	Search capsSearchMode `xml:"search"`
}

// capsSearchMode is one search mode's availability. The available attribute
// is the literal string "yes" or "no".
type capsSearchMode struct {
	Available       string `xml:"available,attr"`
	SupportedParams string `xml:"supportedParams,attr"`
}

// capsCategories is the <categories> element.
type capsCategories struct {
	Categories []capsCategory `xml:"category"`
}

// capsCategory is one top-level <category>. Its <subcat> children are not
// read: a Torznab query for a top-level id covers its subcategories, and the
// sub-ids only refine a source's own subject-matter labelling, which tortui
// has no use for (see internal/indexer's category taxonomy).
type capsCategory struct {
	ID string `xml:"id,attr"`
}

// available reports whether an availability attribute says yes. Anything
// else — "no", an empty attribute, an absent element — is not a yes, which
// is the whole point: an undeclared capability is an absent one (see
// indexer.Caps).
func (m capsSearchMode) available() bool {
	set, ok := boolAttr(m.Available)

	return ok && set
}

// baselineCaps is what this adapter claims about a source it has not
// successfully probed: it can serve a keyword search, and nothing else is
// assumed.
//
// Search is the one capability a Torznab endpoint has by definition — it is
// the API's only mandatory function — so claiming it is not a guess. Every
// other field is false, which the registry reads as "do not send this
// source that kind of query" and turns into a skip rather than a failure
// (AGENT.md §6.3). Latest in particular is never assumed; see probeLatest.
func baselineCaps(requiresAuth bool) indexer.Caps {
	return indexer.Caps{
		Search:       true,
		RequiresAuth: requiresAuth,
	}
}

// probeOutcome is everything one caps probe learned about a source.
type probeOutcome struct {
	caps indexer.Caps
	// categoryIDs maps a tortui bucket onto the source's own top-level
	// category ids, built from the caps document. It is what a category
	// filter is translated through, so the ids sent to a server are only
	// ever ids that server itself published.
	categoryIDs map[indexer.Category][]int
}

// probe asks the source what it can do and returns the outcome.
//
// It makes two requests, in this order:
//
//  1. `t=caps`, which is parsed into Search, Categories, Pagination and the
//     category id map.
//  2. `t=search` with no keyword — but only if step 1 says search works.
//     This is the Latest probe; see probeLatest.
//
// A failure at step 1 is not fatal. The returned outcome is the fail-closed
// baseline and the error explains why, which is what Discover hands back
// alongside a working adapter: a source whose caps document is missing,
// broken, or behind an error is still perfectly able to answer a keyword
// search, and refusing to build the adapter would take the source away from
// the user over metadata (AGENT.md §6.3).
func (a *Adapter) probe(ctx context.Context) (probeOutcome, error) {
	out := probeOutcome{caps: baselineCaps(a.requiresAuth)}

	var doc capsDocument

	if err := a.fetch(ctx, a.params(functionCaps), rootCaps, &doc); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.IsAuth() {
			// The source answered, and what it said was "not without
			// your credentials". That is a real capability fact even
			// though the probe failed, and it is the only one this
			// adapter can learn about authentication — it never
			// discovers or works around a credential (AGENT.md §2).
			out.caps.RequiresAuth = true
		}

		return out, fmt.Errorf("%w: %w", ErrCapsUnavailable, err)
	}

	out.caps.Search = doc.Searching.Search.available()
	out.categoryIDs = categoryIDs(doc)
	out.caps.Categories = len(out.categoryIDs) > 0
	out.caps.Pagination = paginated(doc.Limits)

	if !out.caps.Search {
		return out, nil
	}

	latest, magnets, err := a.probeLatest(ctx)
	if err != nil {
		return out, fmt.Errorf("probing for a recent-additions feed: %w", err)
	}

	out.caps.Latest = latest
	out.caps.ProvidesMagnet = magnets

	return out, nil
}

// probeLatest asks the source for a recent-additions feed and reports
// whether it actually served one.
//
// The criterion this implements is that Caps.Latest must come from what the
// server supports rather than from an assumption, and Torznab gives no way
// to ask: there is no `latest` function and the caps document has no field
// for it. What there is, is a documented behaviour — the newznab API
// specification says that "if the input string for search is empty all items
// (within the server/query limits) are returned for the matching
// categories" (docs/newznab_api_specification.txt in the nZEDb/nZEDb
// repository, branch dev), Jackett gives the case its own name
// (`IsRssSearch`, src/Jackett.Common/Models/TorznabQuery.cs, branch master)
// and neither Jackett's nor Prowlarr's controller rejects a request with no
// q. So the probe is to make exactly the request a ModeLatest query would
// make, and see what comes back.
//
// The answer is true only for a feed that parsed and had at least one item
// in it. Everything else is false, and the ambiguous case is deliberate: a
// server that returns an empty feed for an empty keyword is
// indistinguishable from a server whose index happens to be empty, and
// claiming Latest for it would give the user a feed key that silently
// returns nothing on every press. False costs a skip the registry reports
// (AGENT.md §6.3); true costs a feature that looks broken. See DEC-067.
//
// The second return says whether every item carried a magnet, which is what
// Caps.ProvidesMagnet means. It is false for an empty feed for the same
// reason.
func (a *Adapter) probeLatest(ctx context.Context) (latest, magnets bool, err error) {
	params := a.params(functionSearch)
	params.Set(paramExtended, "1")
	params.Set("limit", strconv.Itoa(probeLimit))

	var doc feedDocument

	if err := a.fetch(ctx, params, rootFeed, &doc); err != nil {
		return false, false, err
	}

	items := doc.Channel.Items
	if len(items) == 0 {
		return false, false, nil
	}

	for _, it := range items {
		if magnetFrom(it.index(), it) == "" {
			return true, false, nil
		}
	}

	return true, true, nil
}

// probeLimit is how many items the Latest probe asks for. One is enough to
// answer the question, and asking for one is the politest way to ask it
// (AGENT.md §6.13). A server that does not implement limit ignores it and
// sends a page, which costs nothing but bytes.
const probeLimit = 1

// paginated reports whether the source's declared limits imply it honours
// offset. A <limits max="..."> element is the only thing in a caps document
// that speaks to paging at all, so its absence is read as "no" rather than
// guessed at.
func paginated(limits capsLimits) bool {
	declared, err := strconv.Atoi(strings.TrimSpace(limits.Max))

	return err == nil && declared > 0
}

// categoryIDs builds the bucket-to-source-ids map from a caps document.
//
// The mapping goes through indexer.CategoryFromTorznab, so a source's own
// numbering is interpreted exactly once, in the one place AGENT.md §13 puts
// it. Only ids the server itself published end up here, which is what makes
// it safe to send them back: tortui never invents a category id for a source
// that did not declare it. Ids that land on CategoryOther are skipped —
// "other" is a bucket for results that arrive unclassified, not a filter
// anyone can usefully ask a server for.
func categoryIDs(doc capsDocument) map[indexer.Category][]int {
	ids := make(map[indexer.Category][]int)

	for _, c := range doc.Categories.Categories {
		id, err := strconv.Atoi(strings.TrimSpace(c.ID))
		if err != nil {
			continue
		}

		bucket := indexer.CategoryFromTorznab(id)
		if bucket == indexer.CategoryOther {
			continue
		}

		ids[bucket] = append(ids[bucket], id)
	}

	if len(ids) == 0 {
		return nil
	}

	return ids
}
