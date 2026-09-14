// Package torznab implements the indexer.Indexer contract against the
// Torznab/Newznab XML API, which is what a Prowlarr or Jackett instance the
// user already runs speaks. Pointing tortui at one makes every source that
// instance aggregates reachable without tortui knowing anything about any of
// them, which is the whole of AGENT.md §1's source-agnosticism in one
// adapter.
//
// Nothing outside this package may import it: the registry is the only
// consumer of an adapter (AGENT.md §4). Nothing in it knows about any
// particular source either — a Torznab endpoint is whatever URL the user
// configured.
//
// Three things about it are load-bearing rather than incidental:
//
//   - Every request goes through internal/indexer/httpx, which owns the
//     user-agent, the timeouts, the per-host spacing, the retry policy, the
//     body cap, and the user's credentials. This package never sees a
//     credential, never builds an HTTP request by hand, and never calls
//     net/http.
//
//   - No error, and no Result field that is not named for a URL, ever
//     carries text this package received from the source. A Torznab request
//     puts the user's api_key in the query string, so a server that echoed
//     it back — into an error description, a guid, an uploader name — would
//     otherwise land it in the user's log file in plaintext, which is the
//     one leak internal/logging cannot mask. See DEC-066, and the
//     credential test in torznab_credentials_test.go.
//
//   - Caps are probed, not assumed, and fixed for the lifetime of an
//     Adapter. Caps.Latest in particular is set from a real request the
//     server answered; see Adapter.probeLatest and DEC-067.
//
// This package writes no log lines at all. Every failure is returned as an
// error the caller can log, which keeps the number of places a credential
// could reach a log file from inside this package at zero.
package torznab

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/kdta91/tortui/internal/indexer"
	"github.com/kdta91/tortui/internal/indexer/httpx"
)

// Torznab request parameters and root element names.
const (
	// paramFunction is `t`, the parameter that selects the API function.
	paramFunction = "t"

	// functionCaps asks the server what it can do.
	functionCaps = "caps"

	// functionSearch is the keyword search, and — with no keyword — the
	// recent-additions feed (see Adapter.probeLatest).
	functionSearch = "search"

	// paramExtended asks for the full attribute set on each item. It is an
	// optional newznab search parameter (docs/newznab_api_specification.txt
	// in the nZEDb/nZEDb repository, branch dev, which lists `extended` on
	// SEARCH: "Return extended information in the search results"), and
	// without it some servers omit the very attributes this adapter maps.
	paramExtended = "extended"

	// rootCaps and rootFeed are the root elements the two responses are
	// expected to have.
	rootCaps = "caps"
	rootFeed = "rss"
)

// Options configures an Adapter. ID and Endpoint are required; everything
// else has a working default.
type Options struct {
	// ID is the stable identifier for this source: the registry key, the
	// config key, and Result.IndexerID.
	ID string

	// Name is the human-readable name shown in the TUI. It defaults to ID.
	Name string

	// Endpoint is the absolute http or https URL of the source's Torznab
	// API — the path a `t=caps` request is made against. Any query it
	// already carries is preserved.
	Endpoint string

	// Client is the HTTP client every request goes through. It carries the
	// user's credentials, its timeouts, and its rate limits, so one client
	// per source is the intended arrangement. A nil Client gets a default
	// one with no credentials, which is enough for a source that needs
	// none.
	Client *httpx.Client

	// RequiresAuth declares that the user configured credentials for this
	// source, which is the only kind of authentication tortui has
	// (AGENT.md §2). It seeds Caps.RequiresAuth. A caps probe can raise it
	// — a source that answers "not without your credentials" has just told
	// us it requires them — but nothing ever lowers it.
	RequiresAuth bool
}

// Adapter is one Torznab source. It is immutable once built and safe for
// concurrent use: Search and Resolve read it and never write to it.
type Adapter struct {
	client       *httpx.Client
	categoryIDs  map[indexer.Category][]int
	id           string
	name         string
	endpoint     string
	caps         indexer.Caps
	requiresAuth bool
}

// Adapter satisfies the frozen contract in AGENT.md §5. The assertion is
// here so a change to either side fails the build rather than the registry.
var _ indexer.Indexer = (*Adapter)(nil)

// New builds an adapter without touching the network.
//
// Its Caps are the fail-closed baseline: a keyword search and nothing else
// (see baselineCaps). Use Discover to get the source's real capabilities;
// this constructor is for a caller that cannot afford a network round trip
// yet, and for one that has already decided to treat the source as
// search-only.
func New(opts Options) (*Adapter, error) {
	id := strings.TrimSpace(opts.ID)
	if id == "" {
		return nil, fmt.Errorf("torznab: %w", ErrIDEmpty)
	}

	target, err := parseEndpoint(opts.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("torznab %s: %w", id, err)
	}

	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = id
	}

	client := opts.Client
	if client == nil {
		client = httpx.New(httpx.Config{})
	}

	return &Adapter{
		client:       client,
		id:           id,
		name:         name,
		endpoint:     target,
		caps:         baselineCaps(opts.RequiresAuth),
		requiresAuth: opts.RequiresAuth,
	}, nil
}

// Discover builds an adapter and probes the source for its real
// capabilities, which is how Caps stays honest instead of assumed.
//
// The two return values are not the usual pair. The Adapter is nil only when
// opts cannot produce one at all — no id, no usable endpoint — and in that
// case the error says so. Every other error is a probe failure that arrives
// **alongside a perfectly usable adapter** carrying the fail-closed
// baseline caps, and wraps ErrCapsUnavailable. A caller that wants a working
// source regardless of what the caps endpoint did may use the adapter and
// report the error; a caller that is testing a source's configuration (the
// settings screen) shows it. Discarding the adapter on error is the one
// thing not to do.
//
// The probe makes two requests. See Adapter.probe.
func Discover(ctx context.Context, opts Options) (*Adapter, error) {
	a, err := New(opts)
	if err != nil {
		return nil, err
	}

	out, probeErr := a.probe(ctx)

	a.caps = out.caps
	a.categoryIDs = out.categoryIDs

	if probeErr != nil {
		return a, fmt.Errorf("torznab %s: %w", a.id, probeErr)
	}

	return a, nil
}

// parseEndpoint validates the configured API endpoint and returns it in the
// form requests are made against.
//
// The error never repeats the URL back. url.Parse's own error does — it
// returns a *url.Error whose text is the whole string, api_key included if
// the user put one in the configured URL — so only the fact that it did not
// parse is reported (DEC-061, DEC-066).
func parseEndpoint(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ErrEndpointEmpty
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", ErrEndpointInvalid
	}

	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return "", ErrEndpointSchemeUnsupported
	}

	if parsed.Host == "" {
		return "", ErrEndpointHostMissing
	}

	return trimmed, nil
}

// ID returns the source's stable identifier.
func (a *Adapter) ID() string { return a.id }

// Name returns the source's human-readable name.
func (a *Adapter) Name() string { return a.name }

// Caps returns what this source can do. It does no I/O, never blocks, and
// returns the same value for the lifetime of the Adapter — the probe that
// determined it ran once, in Discover.
func (a *Adapter) Caps() indexer.Caps { return a.caps }

// Search runs q against the source and returns its results.
//
// Zero results and a nil error is a valid outcome and the usual one for a
// query that matched nothing. An error means this source failed; the
// registry collects it per-source and degrades (AGENT.md §6.3).
//
// ctx bounds the whole call. The deadline is the caller's — the registry
// applies a per-source one — and httpx adds its own on top, so a call that
// arrives with no deadline still has one (AGENT.md §6.2).
func (a *Adapter) Search(ctx context.Context, q indexer.Query) ([]indexer.Result, error) {
	params, ok, err := a.searchParams(q)
	if err != nil {
		return nil, fmt.Errorf("torznab %s: %w", a.id, err)
	}

	if !ok {
		// The source published its categories and none of them fall in
		// the buckets this query asked for, so it has nothing to
		// match. Answering that without a request is both correct and
		// one less request to someone else's server (AGENT.md §6.13).
		return nil, nil
	}

	var doc feedDocument

	if err := a.fetch(ctx, params, rootFeed, &doc); err != nil {
		return nil, fmt.Errorf("torznab %s: %w", a.id, err)
	}

	results := make([]indexer.Result, 0, len(doc.Channel.Items))

	for _, it := range doc.Channel.Items {
		res := resultFrom(a.id, it)

		// MinSeeders is applied here rather than pushed to the source:
		// Torznab has no parameter for it (indexer.Query allows
		// either).
		if q.MinSeeders > 0 && res.Seeders < q.MinSeeders {
			continue
		}

		results = append(results, res)
	}

	return results, nil
}

// Resolve fills in what a result is missing so the engine can act on it.
//
// It makes no network call. Torznab returns everything it knows about an
// item in the feed itself — there is no per-item endpoint to fetch — so
// there is nothing to go and get, and the work is arithmetic:
//
//   - A result that already carries a magnet is returned unchanged. This is
//     the no-op the frozen contract in AGENT.md §5 requires, and it is
//     checked first so it holds regardless of anything else on the result.
//   - A result with an infohash but no magnet gets one built from the hash
//     and its title. That is the standard magnet form, it is what the
//     engine needs, and it is derived entirely from data the source already
//     published.
//   - A result with only a torrent URL is already usable — the engine
//     fetches the file — so it too is returned unchanged.
//   - A result with none of the three cannot be resolved by anything this
//     adapter knows, and returns ErrUnresolvable rather than a result that
//     will fail later somewhere less obvious.
func (a *Adapter) Resolve(_ context.Context, r indexer.Result) (indexer.Result, error) {
	if strings.TrimSpace(r.Magnet) != "" {
		return r, nil
	}

	if hash := normaliseInfoHash(r.InfoHash); hash != "" {
		resolved := r
		resolved.InfoHash = hash
		resolved.Magnet = magnetFor(hash, r.Title)

		return resolved, nil
	}

	if strings.TrimSpace(r.TorrentURL) != "" {
		return r, nil
	}

	// The failing result is not named. Every field that could name it —
	// ID, Title, SourceURL — is text this adapter got from the source, and
	// on a result the adapter did not itself produce the ID may be a raw
	// download URL with the user's api_key in its query string. Caught by
	// TestNoErrorFromThisAdapterCarriesTheCredential, which had this error
	// printing one (DEC-066). The caller knows which result it passed.
	return r, fmt.Errorf("torznab %s: %w", a.id, ErrUnresolvable)
}

// magnetFor builds a magnet URI from an infohash and a display name. The
// display name is optional in the format and is included because it is what
// a torrent client shows before any metadata arrives.
func magnetFor(hash, title string) string {
	magnet := magnetScheme + "?xt=" + btihPrefix + hash

	if name := strings.TrimSpace(title); name != "" {
		magnet += "&dn=" + url.QueryEscape(name)
	}

	return magnet
}

// searchParams builds the query string for one Query.
//
// The second return reports whether the request is worth making at all: it
// is false when the query asked for categories, the source declared its own,
// and none of them fall in any bucket the query wants.
func (a *Adapter) searchParams(q indexer.Query) (url.Values, bool, error) {
	params := a.params(functionSearch)
	params.Set(paramExtended, "1")

	switch q.Mode {
	case indexer.ModeSearch:
		text := strings.TrimSpace(q.Text)
		if text == "" {
			// indexer.Query is explicit that an adapter must not turn
			// an empty keyword into a browse request. A caller that
			// wants the feed asks for ModeLatest.
			return nil, false, ErrQueryTextEmpty
		}

		params.Set("q", text)
	case indexer.ModeLatest:
		if !a.caps.Latest {
			return nil, false, ErrLatestUnsupported
		}
		// A Torznab recent-additions feed is a search with no keyword;
		// the q parameter is simply left off.
	default:
		return nil, false, fmt.Errorf("%w (%d)", ErrModeUnsupported, int(q.Mode))
	}

	if q.Limit > 0 {
		params.Set("limit", strconv.Itoa(q.Limit))
	}

	if q.Offset > 0 && a.caps.Pagination {
		params.Set("offset", strconv.Itoa(q.Offset))
	}

	cats, ok := a.categoryParam(q.Categories)
	if !ok {
		return nil, false, nil
	}

	if cats != "" {
		params.Set("cat", cats)
	}

	return params, true, nil
}

// categoryParam translates the query's buckets into the source's own
// category ids.
//
// Only ids the source published in its own caps document are ever sent, so
// tortui never invents a number for a server. A source that declared no
// categories (Caps.Categories false) gets no cat parameter and the query's
// categories are ignored, which indexer.Query explicitly allows.
//
// Results are not filtered again locally afterwards. The source's own
// classification is authoritative for its own ids, and re-filtering on
// tortui's coarse buckets would drop exactly the items whose category the
// source has a custom id for — the ones that map to CategoryOther on the way
// in and that the source itself considered in-category.
//
// The second return is false when the query named buckets, the source
// declared categories, and none of them intersect: the honest answer is then
// no results, without a request.
func (a *Adapter) categoryParam(categories []indexer.Category) (string, bool) {
	if len(categories) == 0 || len(a.categoryIDs) == 0 {
		return "", true
	}

	var ids []int

	for _, bucket := range categories {
		ids = append(ids, a.categoryIDs[bucket]...)
	}

	if len(ids) == 0 {
		return "", false
	}

	slices.Sort(ids)
	ids = slices.Compact(ids)

	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.Itoa(id))
	}

	return strings.Join(parts, ","), true
}

// params starts the parameter set for one API function.
func (a *Adapter) params(function string) url.Values {
	values := url.Values{}
	values.Set(paramFunction, function)

	return values
}

// fetch performs one API request and decodes the response into into.
//
// It adds nothing to the error but the decode context: httpx's errors
// already name the method and host and are already guaranteed to carry no
// URL, no header, and no body excerpt, and re-wrapping one with anything
// request-shaped would undo that (DEC-061).
func (a *Adapter) fetch(ctx context.Context, params url.Values, root string, into any) error {
	res, err := a.client.Get(ctx, a.endpoint, params)
	if err != nil {
		return err
	}

	return decodeDocument(res.Body, root, into)
}
