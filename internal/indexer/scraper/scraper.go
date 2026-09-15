// Package scraper implements the indexer.Indexer contract against a source
// described entirely by a user-supplied YAML definition. There is no
// selector, no parameter name, no path and no endpoint for any particular
// site anywhere in this package — a source is a file the user wrote, and
// pointing tortui at one takes a text editor rather than a release
// (AGENT.md §2, §13; DEC-003).
//
// That is also the whole of why it is YAML. goquery selectors rot when a
// site changes its markup, and the user who notices is the one who can fix
// it in thirty seconds if the selectors are data — and who has to wait for
// a new binary if they are Go. Nothing in this package hardcodes one.
//
// Nothing outside this package may import it: the registry is the only
// consumer of an adapter (AGENT.md §4).
//
// Four things about it are load-bearing rather than incidental:
//
//   - Every request goes through internal/indexer/httpx, which owns the
//     user-agent, the timeouts, the per-host spacing, the retry policy, the
//     body cap, and the user's credentials. This package never sees a
//     credential, never builds an HTTP request by hand, and never calls
//     net/http.
//
//   - A definition is not a place to put a credential. The schema has no
//     placeholder for one and validation refuses every placeholder it does
//     not define, so "{{apikey}}" is an error rather than a feature: an api
//     key or a session cookie lives in the user's own config and is
//     injected by httpx (DEC-074). Nothing here discovers a credential,
//     harvests a session, solves a challenge, or negotiates around an
//     access control (AGENT.md §2).
//
//   - No error this package produces carries text it received from the
//     source, or a value out of the definition other than a selector, a
//     regex, a transform name or a mode. A definition is user-supplied
//     config whose param values and base_url are free to contain the
//     user's own api key, and a response is written by the source, so
//     both are treated as text that must never reach a log line:
//     internal/logging masks by key name and by value shape, and an
//     opaque token inside an error string has neither (DEC-073).
//
//   - The response is hostile input. An HTML document nested deeper than
//     maxHTMLDepth is refused before the parser sees it, because parsing
//     one costs time quadratic in its depth; rows are capped at maxRows;
//     regexes are RE2 and cannot backtrack catastrophically; a transform
//     chain is a bounded list walked once; and the definition itself is
//     decoded strictly, so an unknown key is an error rather than a place
//     to hang a YAML alias bomb (DEC-075).
//
// This package writes no log lines at all. Every failure is returned as an
// error the caller can log, which keeps the number of places a credential
// could reach a log file from inside this package at zero.
//
// # What each Result field carries
//
// This is written out field by field on purpose, and it says what the code
// does rather than counting how many fields are safe: the torznab adapter
// shipped three successive summaries of its own version of this and QA
// falsified all three (PR #12, rounds 1 to 3). Every field of the frozen
// §5 indexer.Result, and what this adapter puts in it:
//
//	IndexerID   Derived. The id the user configured for this source; no
//	            page text reaches it.
//	ID          Derived only on the infohash branch, which is validated as
//	            40 hex or 32 base32 characters. PASSED THROUGH on every
//	            other branch: an `id` field the definition selected is the
//	            page's text verbatim; the details/download address branch
//	            goes through withoutQuery, which removes the query string
//	            and fragment of a value url.Parse gives a scheme to and
//	            returns anything else exactly as it stands; and the
//	            last-resort branch is the title.
//	Title       PASSED THROUGH, whitespace-trimmed, narrowed by the
//	            definition's own regex and transforms if it set any, and
//	            otherwise verbatim.
//	InfoHash    Derived. Only 40 hex or 32 base32 characters survive
//	            normaliseInfoHash, so no other text can reach it.
//	Magnet      PASSED THROUGH. The page's magnet URI, kept only if it
//	            really starts "magnet:" but otherwise verbatim, dn=
//	            included. The magnet Resolve derives for a row that
//	            published none is NOT clean either: magnetFor writes
//	            Result.Title into its dn=, and url.QueryEscape
//	            percent-encodes that text rather than removing it, so an
//	            opaque token survives unchanged.
//	TorrentURL  The page's own download address, resolved against base_url
//	            and refused unless it is http or https — and safe to log
//	            regardless: internal/logging masks on the name.
//	SizeBytes   Derived. Parsed as a number of bytes.
//	Seeders     Derived. Parsed as an integer.
//	Leechers    Derived. Parsed as an integer.
//	Category    Derived. An indexer.Category enum value, mapped by
//	            indexer.CategoryFromString.
//	Published   Derived. A parsed time.Time.
//	Uploader    PASSED THROUGH, verbatim. There is not even the "://"
//	            refusal the torznab adapter applies: a definition's
//	            uploader selector is the user's own choice, and
//	            internal/logging's value-shape pattern already redacts a
//	            URL under any key name. What neither catches is an opaque
//	            token, which is what an api key is.
//	Trust       Derived. An indexer.Trust enum value, from the
//	            definition's own value map.
//	SourceURL   The page's own details address, resolved against base_url
//	            and refused unless it is http or https — and safe to log:
//	            internal/logging masks on the name.
//	Extra       Never set. This schema has no extra block, so the map is
//	            always nil.
//
// The fields that carry page text under a name internal/logging does not
// mask are therefore, exhaustively: Title, Magnet, Uploader, and ID on
// every branch but the infohash one. A source that echoes the user's api
// key into a title cell, a magnet's dn=, an uploader cell or an id cell
// puts it in that field verbatim — and into Magnet a second time if the
// row published no magnet and Resolve derives one, since the derived
// magnet's dn= is the title. Logging the field, or the whole Result,
// writes it to the log file in plaintext. The one part of that surface
// something still catches is a value that is itself a full http(s)
// address: internal/logging's URL pattern matches on the value, whatever
// the key is called. Nothing catches a bare token.
//
// None of that is fixable from inside this adapter, and it is not this
// adapter's gap to fix: every adapter produces the same frozen §5
// indexer.Result, the title is the value the user reads, the magnet must
// reach the engine as published, and this package never sees the
// credential it would have to scrub with. It is recorded as backlog T-934,
// opened by QA on the torznab adapter (PR #12), and explained in DEC-071.
// The credential test in credentials_test.go asserts both directions of
// that boundary: the derived fields must stay clean, and the four above
// must keep carrying what the page sent, so a change to either goes red.
package scraper

import (
	"context"
	"fmt"
	"strings"

	"github.com/kdta91/tortui/internal/indexer"
	"github.com/kdta91/tortui/internal/indexer/httpx"
)

// Options configures an Adapter. Definition is required; everything else
// has a working default.
type Options struct {
	// Definition is the parsed source definition. It is validated again
	// here, so a Definition a caller built by hand gets the same checks a
	// parsed one does, and it is compiled into a form the Adapter keeps —
	// changing the Definition afterwards has no effect on the Adapter.
	Definition *Definition

	// Client is the HTTP client every request goes through. It carries
	// the user's credentials, its timeouts and its rate limits, so one
	// client per source is the intended arrangement. A nil Client gets a
	// default one with no credentials, which is enough for a source that
	// needs none.
	Client *httpx.Client

	// RequiresAuth declares that the user configured credentials for this
	// source, which is the only kind of authentication tortui has
	// (AGENT.md §2). It raises Caps.RequiresAuth; the definition's own
	// requires_auth raises it too, and nothing lowers it.
	RequiresAuth bool
}

// Adapter is one YAML-defined source. It is immutable once built and safe
// for concurrent use: Search and Resolve read it and never write to it.
type Adapter struct {
	client *httpx.Client
	plan   *plan
	caps   indexer.Caps
}

// Adapter satisfies the frozen contract in AGENT.md §5. The assertion is
// here so a change to either side fails the build rather than the registry.
var _ indexer.Indexer = (*Adapter)(nil)

// New compiles a definition into an adapter. It touches no network.
//
// Caps are read off the definition rather than probed, because for this
// adapter the definition *is* the statement of what the source can do:
// Caps.Latest is whether it has a latest block, Caps.Pagination is whether
// a block has somewhere to put an offset, and Caps.ProvidesMagnet is
// whether every block maps a magnet field. A probe could only tell us
// whether one page happened to answer today, which is what the `t`est-a-
// source command is for. See Caps for the per-field reasoning.
func New(opts Options) (*Adapter, error) {
	if opts.Definition == nil {
		return nil, fmt.Errorf("scraper: %w", ErrDefinitionEmpty)
	}

	compiled, err := opts.Definition.plan()
	if err != nil {
		return nil, fmt.Errorf("scraper: %w", err)
	}

	client := opts.Client
	if client == nil {
		client = httpx.New(httpx.Config{})
	}

	return &Adapter{
		client: client,
		plan:   compiled,
		caps:   capsFor(compiled, opts.RequiresAuth),
	}, nil
}

// capsFor derives the source's capabilities from its definition.
func capsFor(p *plan, requiresAuth bool) indexer.Caps {
	return indexer.Caps{
		// A definition without a search block does not validate, so a
		// source that exists can be searched.
		Search: true,

		// The acceptance criterion for the latest block: a definition
		// that omits it reports Caps.Latest = false, and the registry
		// then skips this source for a ModeLatest query and reports the
		// skip rather than treating it as a failure (AGENT.md §6.3).
		Latest: p.latest != nil,

		// This schema has no way to say which of the source's own
		// category values correspond to tortui's buckets, so a category
		// filter cannot be pushed to the source and is not applied
		// locally either — indexer.Query allows a source without the
		// capability to ignore the field. Backlog T-938.
		Categories: false,

		// Query.Offset is meaningful exactly when the search block has
		// somewhere to put it.
		Pagination: p.search.pagination,

		RequiresAuth: p.requiresAuth || requiresAuth,

		// Honest for what it can be: whether the definition maps a
		// magnet field in every block it has. Whether the page actually
		// carries one on any given day is not knowable without asking,
		// and Caps must not do I/O (indexer.Indexer's own contract).
		ProvidesMagnet: mapsMagnet(p.search) && (p.latest == nil || mapsMagnet(p.latest)),
	}
}

// mapsMagnet reports whether a block maps the magnet field.
func mapsMagnet(b *blockPlan) bool {
	_, ok := b.fields[fieldMagnet]

	return ok
}

// ID returns the source's stable identifier.
func (a *Adapter) ID() string { return a.plan.id }

// Name returns the source's human-readable name.
func (a *Adapter) Name() string { return a.plan.name }

// Caps returns what this source can do. It does no I/O, never blocks, and
// returns the same value for the lifetime of the Adapter.
func (a *Adapter) Caps() indexer.Caps { return a.caps }

// Search runs q against the source and returns its results.
//
// Zero results and a nil error is a valid outcome and the usual one for a
// query that matched nothing — and also what a rows selector that matches
// nothing produces, because the two are indistinguishable from here. An
// error means this source failed; the registry collects it per-source and
// degrades (AGENT.md §6.3).
//
// ctx bounds the whole call. The deadline is the caller's — the registry
// applies a per-source one — and httpx adds its own on top, so a call that
// arrives with no deadline still has one (AGENT.md §6.2).
func (a *Adapter) Search(ctx context.Context, q indexer.Query) ([]indexer.Result, error) {
	block, err := a.blockFor(q)
	if err != nil {
		return nil, fmt.Errorf("scraper %s: %w", a.plan.id, err)
	}

	target, params, err := block.request(a.plan.base, q)
	if err != nil {
		return nil, fmt.Errorf("scraper %s: %w", a.plan.id, err)
	}

	res, err := a.client.Get(ctx, target, params)
	if err != nil {
		// httpx's errors already name the method and host and are
		// already guaranteed to carry no URL, no header and no body
		// excerpt. Re-wrapping one with anything request-shaped would
		// undo that (DEC-061).
		return nil, fmt.Errorf("scraper %s: %w", a.plan.id, err)
	}

	rows, err := a.rows(res.Body, block)
	if err != nil {
		return nil, fmt.Errorf("scraper %s: %w", a.plan.id, err)
	}

	return a.results(rows, block, q), nil
}

// blockFor picks the block a query asks for and refuses a query this
// definition cannot serve.
func (a *Adapter) blockFor(q indexer.Query) (*blockPlan, error) {
	switch q.Mode {
	case indexer.ModeSearch:
		if strings.TrimSpace(q.Text) == "" {
			// indexer.Query is explicit that an adapter must not turn
			// an empty keyword into a browse request. A caller that
			// wants the feed asks for ModeLatest.
			return nil, ErrQueryTextEmpty
		}

		return a.plan.search, nil
	case indexer.ModeLatest:
		if a.plan.latest == nil {
			return nil, ErrLatestUnsupported
		}

		return a.plan.latest, nil
	default:
		return nil, fmt.Errorf("%w (%d)", ErrModeUnsupported, int(q.Mode))
	}
}

// rows parses the response body in the definition's mode.
func (a *Adapter) rows(body []byte, block *blockPlan) ([]row, error) {
	if a.plan.mode == ModeJSON {
		return jsonRows(body, block.rows)
	}

	return htmlRows(body, block.rows)
}

// results maps the parsed rows onto indexer.Result, applying the query's
// own limits.
//
// MinSeeders is applied here rather than pushed to the source: this schema
// has no placeholder for it, and indexer.Query allows either. Limit is
// applied here as well as being available to the request template, because
// a site is free to ignore the parameter it was sent.
func (a *Adapter) results(rows []row, block *blockPlan, q indexer.Query) []indexer.Result {
	out := make([]indexer.Result, 0, len(rows))

	for _, r := range rows {
		res, ok := a.plan.resultFrom(block, r)
		if !ok {
			continue
		}

		if q.MinSeeders > 0 && res.Seeders < q.MinSeeders {
			continue
		}

		out = append(out, res)

		if q.Limit > 0 && len(out) >= q.Limit {
			break
		}
	}

	return out
}

// Resolve fills in what a result is missing so the engine can act on it.
//
// It makes no network call, and the reasoning is the same as the torznab
// adapter's: what this adapter knows about an item is what the listing page
// carried, and there is nothing to derive that is not already in the
// Result it was handed.
//
//   - A result that already carries a magnet is returned unchanged. This is
//     the no-op the frozen contract in AGENT.md §5 requires, and it is
//     checked first so it holds regardless of anything else on the result.
//   - A result with an infohash but no magnet gets one built from the hash
//     and its title.
//   - A result with only a torrent URL is already usable — the engine
//     fetches the file — so it too is returned unchanged.
//   - A result with none of the three returns ErrUnresolvable.
//
// Fetching the details page to find a magnet that the listing page did not
// carry is the obvious next thing and is deliberately not here: it needs a
// `detail` block in the schema, and the schema this task ships is the one
// its acceptance criteria enumerate. Backlog T-939.
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
	// ID, Title, SourceURL — is text this adapter got from the page, and
	// on a result the adapter did not itself produce the ID may be a raw
	// download address with the user's api key in its query string. The
	// caller knows which result it passed.
	return r, fmt.Errorf("scraper %s: %w", a.plan.id, ErrUnresolvable)
}
