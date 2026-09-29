package scraper

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/kdta91/tortui/internal/indexer"
)

// resolveDetails fetches one result's details page and reads the magnet,
// torrent link or infohash the listing did not carry (T-9037, DEC-142).
//
// Resolve calls it only for a result with none of the three, a source_url,
// and a definition with a details block. It makes exactly one request,
// through the same httpx client Search uses — so the user's credentials,
// the body cap, the timeouts, the per-host spacing and the strict same-host
// redirect rule all apply exactly as they do to a search — and only after
// the address has passed detailsAddress.
//
// The result gets exactly one link, as the engine requires (T-9010): the
// page's own magnet when it has one, otherwise its torrent link, otherwise
// a magnet built from its infohash. An infohash found on the page (or in
// its magnet) is kept alongside either link, so the add flow's duplicate
// check sees it.
func (a *Adapter) resolveDetails(ctx context.Context, r indexer.Result) (indexer.Result, error) {
	page, err := a.plan.detailsAddress(r.SourceURL)
	if err != nil {
		return r, fmt.Errorf("scraper %s: %w", a.plan.id, err)
	}

	res, err := a.client.Get(ctx, page.String(), nil)
	if err != nil {
		// httpx's errors name the method and host and never the URL
		// (DEC-061); nothing request-shaped is added here.
		return r, fmt.Errorf("scraper %s: details page: %w", a.plan.id, err)
	}

	doc, err := documentRow(res.Body, a.plan.details.mode)
	if err != nil {
		return r, fmt.Errorf("scraper %s: details page: %w", a.plan.id, err)
	}

	read := func(name string) string {
		if f, ok := a.plan.details.fields[name]; ok {
			return f.read(doc)
		}

		return ""
	}

	magnet := magnetFrom(read(fieldMagnet))
	torrent := webAddress(page, read(fieldTorrent))
	hash := infoHashFrom(read(fieldInfoHash), magnet)

	resolved := r

	switch {
	case magnet != "":
		resolved.Magnet = magnet
	case torrent != "":
		resolved.TorrentURL = torrent
	case hash != "":
		resolved.Magnet = magnetFor(hash, r.Title)
	default:
		return r, fmt.Errorf("scraper %s: %w", a.plan.id, ErrDetailsNoLink)
	}

	if hash != "" {
		resolved.InfoHash = hash
	}

	return resolved, nil
}

// detailsAddress parses a result's source_url and refuses it unless it is
// an http or https address on the definition's own base_url host — the same
// host and port — without dropping https for http.
//
// A Result reaching Resolve is not necessarily one this adapter produced,
// and one it did produce carries an address a hostile page chose. Fetching
// it would send the user's credentials for this source to wherever that
// page pointed, so containment is checked here, before any request, rather
// than left to the redirect rule. The address is never repeated back in
// the error: it is text off the page (DEC-073).
func (p *plan) detailsAddress(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("%w (it does not parse)", ErrDetailsAddressRefused)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("%w (it is not an http or https address)", ErrDetailsAddressRefused)
	}

	if parsed.Host == "" || !strings.EqualFold(parsed.Host, p.base.Host) {
		return nil, fmt.Errorf("%w (it is on a different host)", ErrDetailsAddressRefused)
	}

	if strings.EqualFold(p.base.Scheme, "https") && scheme != "https" {
		return nil, fmt.Errorf("%w (it drops https for http)", ErrDetailsAddressRefused)
	}

	return parsed, nil
}

// documentRow parses a whole response as a single row, in the given mode:
// the document root for html, the decoded value for json. A details field
// with an empty selector therefore reads the whole page.
func documentRow(body []byte, mode string) (row, error) {
	if mode == ModeJSON {
		doc, err := decodeJSON(body)
		if err != nil {
			return nil, err
		}

		return jsonRow{value: doc}, nil
	}

	sel, err := parseHTML(body)
	if err != nil {
		return nil, err
	}

	return htmlRow{sel: sel}, nil
}
