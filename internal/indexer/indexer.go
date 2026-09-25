// Package indexer defines the source-agnostic contracts every torrent search
// source in tortui implements: the Indexer interface an adapter satisfies, the
// Query it is asked, the Result it returns, and the Caps it declares about
// itself.
//
// These types are the frozen domain contracts of AGENT.md §5. Everything
// downstream — the registry, every adapter, the whole TUI — is written against
// them, so changing a name, a field, or the meaning of a field requires a DEC-
// entry in TASK_TRACKER.md and a note on every affected task (AGENT.md §12).
//
// Nothing here knows about any particular source. An adapter lives entirely
// inside its own sub-package, is reached only through the registry, and adding
// one must never require a change to this file (AGENT.md §1, §4).
package indexer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Trust is uploader/upload trust metadata as reported by a source: the
// VIP / Trusted / Verified badges several public indexers attach to an upload.
//
// It is display metadata and nothing else (AGENT.md §2). It may be shown as a
// badge, sorted on, and filtered on. It must never gate behaviour, unlock a
// capability, or be read as any kind of entitlement — an adapter that makes a
// decision based on a Trust value is wrong.
//
// An adapter must guarantee: report the level the source actually published
// for that upload, and report TrustUnknown when the source publishes no trust
// information at all. Never invent a level, and never map "the source says
// nothing" to TrustNone — TrustNone is the positive statement "this source
// tracks uploader trust and this uploader has none", which is different
// information and sorts differently.
//
// The underlying int ordering is part of the contract: the constants run from
// least to most trusted, so sorting the results table on the trust column
// (AGENT.md §7) is a plain integer sort.
type Trust int

const (
	// TrustUnknown means the indexer does not expose trust info. It is the
	// zero value, so a Result an adapter never touched reads as "no
	// information" rather than as a claim about the uploader.
	TrustUnknown Trust = iota
	// TrustNone means the source does track uploader trust and this
	// uploader holds no badge.
	TrustNone
	// TrustVerified means the upload itself was verified.
	TrustVerified
	// TrustTrusted means the uploader holds a trusted badge.
	TrustTrusted
	// TrustVIP means the uploader holds the indexer's highest trust badge.
	TrustVIP
)

// String returns a lowercase, stable token for the trust level: "unknown",
// "none", "verified", "trusted", or "vip". The tokens are the identifiers used
// wherever a trust level is written down or parsed — log lines, test failures,
// and any future filter expression — so they are deliberately case-uniform and
// not the human-facing rendering. Use Badge for the results table.
//
// An out-of-range value renders as trust(N) rather than panicking or
// masquerading as a known level.
func (t Trust) String() string {
	switch t {
	case TrustUnknown:
		return "unknown"
	case TrustNone:
		return "none"
	case TrustVerified:
		return "verified"
	case TrustTrusted:
		return "trusted"
	case TrustVIP:
		return "vip"
	default:
		return fmt.Sprintf("trust(%d)", int(t))
	}
}

// Badge returns the compact cell for the results table's Trust column
// (AGENT.md §7): "VIP", "TR", "✓", or "" for both TrustUnknown and TrustNone —
// neither of which has anything worth spending a column on.
//
// The returned string is the Unicode form. Choosing an ASCII substitute for a
// terminal that cannot render "✓" is the TUI theme layer's job, not this
// package's; Badge has no knowledge of terminal capability and must not grow
// any (AGENT.md §14).
//
// An out-of-range value returns "" so an unexpected number can never widen a
// fixed-width column.
func (t Trust) Badge() string {
	switch t {
	case TrustVIP:
		return "VIP"
	case TrustTrusted:
		return "TR"
	case TrustVerified:
		return "✓"
	case TrustUnknown, TrustNone:
		return ""
	default:
		return ""
	}
}

// Mode selects what kind of listing a Query asks for. It is explicit rather
// than inferred from an empty Query.Text, so a source that cannot browse fails
// loudly at the registry — which skips it and reports it as skipped — instead
// of silently returning nothing for a latest-feed request.
type Mode int

const (
	// ModeSearch is a keyword query. It is the zero value, so a Query a
	// caller under-populated asks for the mode that requires an explicit
	// Text rather than for a whole feed.
	ModeSearch Mode = iota
	// ModeLatest asks for the most recently added items, with no keyword.
	// Only a source whose Caps.Latest is true may be given one.
	ModeLatest
)

// Query is a single search request, as handed to one indexer. The registry
// builds it once and gives the same value to every selected source; an adapter
// translates it into whatever its own source speaks and must not mutate it.
type Query struct {
	// Mode selects a keyword search or a latest-additions feed. See Mode.
	Mode Mode

	// Text is the keyword query. It is empty when Mode is ModeLatest, and
	// an adapter must not fall back to a browse/feed request when it is
	// empty under ModeSearch.
	Text string

	// Categories restricts the query to these buckets. Empty means no
	// category restriction. A source whose Caps.Categories is false may
	// ignore this field; the registry does not filter on its behalf.
	Categories []Category

	// MinSeeders drops results below this seeder count. Zero means no
	// minimum. An adapter may push this to the source when the source
	// supports it, or apply it locally; either is correct.
	MinSeeders int

	// Limit caps how many results to return. Zero means "the source's own
	// default page size" — an adapter must not read it as "return none".
	Limit int

	// Offset is how many results to skip, for pagination. It is meaningful
	// only to a source whose Caps.Pagination is true.
	Offset int
}

// Result is one search hit, normalised out of whatever shape the source
// returned it in.
//
// An adapter must guarantee: IndexerID is its own ID, ID is stable for that
// item within that source, and at least one of Magnet or TorrentURL is set by
// the time the result reaches the engine — either directly from Search or
// after Resolve. Everything else is best-effort; a source that does not
// publish a field leaves it at its zero value rather than guessing.
type Result struct {
	// IndexerID is the ID of the source that produced this result. The
	// registry relies on it to attribute results and errors, so it is
	// never empty.
	IndexerID string

	// ID is a stable identifier for this item within that source — stable
	// enough that the same item fetched again carries the same ID.
	ID string

	// Title is the item's name as the source published it. It is
	// attacker-influenced text: it may contain CJK, emoji, combining
	// marks, and control characters, so measure it with rivo/uniseg and
	// never with len (AGENT.md §14), and never use it to build a
	// filesystem path (AGENT.md §6.11).
	Title string

	// InfoHash is the torrent's infohash. It may be empty until Resolve.
	InfoHash string

	// Magnet is a magnet URI for the item. It may be empty if TorrentURL
	// is set.
	Magnet string

	// TorrentURL is a URL to a .torrent file. It may be empty if Magnet is
	// set.
	TorrentURL string

	// SizeBytes is the total size in bytes, or zero when the source does
	// not publish one.
	SizeBytes int64

	// Seeders and Leechers are the swarm counts the source reported, or
	// zero when it reports none. They are a snapshot, not a promise.
	Seeders  int
	Leechers int

	// Category is the bucket this item was mapped into by the adapter.
	// Unrecognised source categories map to the zero value, never dropped.
	Category Category

	// Published is when the source says the item was added. It is the zero
	// time when the source publishes no date.
	Published time.Time

	// Uploader is the uploader's name as published, or empty.
	Uploader string

	// Trust is the uploader/upload trust metadata. See Trust — it is
	// display-only.
	Trust Trust

	// SourceURL is the human-viewable page this result came from, opened
	// by the `u` keybind. Empty when the source has no such page.
	SourceURL string

	// Extra carries source-specific fields that have no home above, for
	// display on the details screen. It is nil unless an adapter sets it.
	// Nothing outside an adapter may depend on a particular key being
	// present, and no core logic may branch on one — that would be
	// special-casing a source outside its own package (AGENT.md §1).
	Extra map[string]string
}

// ErrNoLink reports a Result that carries neither a magnet URI nor a torrent
// URL, and so cannot be handed to the engine. Callers match it with errors.Is;
// Validate wraps it with the indexer and result ids.
var ErrNoLink = errors.New("result has neither a magnet nor a torrent URL")

// ExtraKeyFiles is a well-known, optional Result.Extra key an adapter may
// set to the item's file list, one path per line, when the source publishes
// one — the details screen's file listing (T-063; AGENT.md §7). Setting it
// is entirely optional: no adapter is required to populate it, and it
// exists in every adapter's package the same way, so a caller checking for
// it is checking a documented, source-agnostic convention — not
// special-casing one source's own field, which is what the Extra field's
// doc comment above actually warns against. It mirrors registry.go's
// ExtraKeySources/ExtraKeyCacheHit in that sense, except an adapter (not
// the registry) sets it. The listed names are for display only: they
// describe a search result no download yet exists to validate them
// against, and must never be resolved against a filesystem path
// (AGENT.md §6.11).
const ExtraKeyFiles = "tortui.files"

// Validate reports whether a Result is usable, which means exactly one thing:
// it has a link the engine could act on. A Result with neither Magnet nor
// TorrentURL is rejected with an error wrapping ErrNoLink; a value that is only
// whitespace does not count as set.
//
// It deliberately checks nothing else. Every other field is legitimately empty
// for some source — a source may publish no size, no date, no uploader, no
// details page — so validating them would reject results that are perfectly
// fine to show. Callers that need more than a link must check for it
// themselves.
//
// A Result that Search returns unresolved (no link yet, to be filled in by
// Resolve) will not pass Validate; validate after Resolve, not before.
func (r Result) Validate() error {
	if strings.TrimSpace(r.Magnet) == "" && strings.TrimSpace(r.TorrentURL) == "" {
		return fmt.Errorf("indexer %q: result %q: %w", r.IndexerID, r.ID, ErrNoLink)
	}
	return nil
}

// Caps is what a source declares it can do. Every field defaults to false, so
// an adapter that forgets to declare a capability is treated as lacking it —
// the registry skips it for a query that needs it rather than sending a request
// the source cannot serve.
//
// An adapter must guarantee these are honest and constant for the lifetime of
// the value: the registry reads them to decide what to send where, and a
// capability claimed but not delivered surfaces to the user as a failed source.
type Caps struct {
	// Search means the source can serve a ModeSearch keyword query.
	Search bool

	// Latest means the source can return a recent-additions feed with no
	// keyword. A ModeLatest query is never sent to a source without it;
	// such a source is skipped and reported as skipped, not as a failure
	// (AGENT.md §6.3).
	Latest bool

	// Categories means the source can restrict a query to Query.Categories.
	Categories bool

	// Pagination means Query.Offset is meaningful for this source.
	Pagination bool

	// RequiresAuth means the source needs credentials the user supplied
	// from their own account — an API key, cookie, or passkey in their
	// config. That is the only auth path there is (AGENT.md §2); nothing
	// in tortui works around a source's access controls.
	RequiresAuth bool

	// ProvidesMagnet means Search results already carry a Magnet, so
	// Resolve is a no-op. A source without it returns results that need
	// Resolve before they can be added to the engine.
	ProvidesMagnet bool
}

// Indexer is one search source. Every adapter — Torznab, the YAML-driven
// scraper, the fake used in tests — implements this and nothing wider, which is
// what keeps the rest of the application source-agnostic (AGENT.md §1).
//
// An implementation must guarantee:
//
//   - ID, Name, and Caps are cheap, pure, and safe to call concurrently. They
//     do no I/O and never block.
//   - Search and Resolve honour the ctx deadline the registry sets and return
//     promptly when it is cancelled (AGENT.md §6.2).
//   - Both are safe for concurrent use: the registry fans a single query out
//     across sources at once.
//   - Errors are wrapped with enough context to name the source, e.g.
//     fmt.Errorf("torznab %s: %w", id, err) (AGENT.md §6.9). Never panic.
//   - Neither method makes a network call outside the source the user pointed
//     it at. No telemetry, no third-party lookups (AGENT.md §2).
type Indexer interface {
	// ID is the stable, unique identifier for this source, used in config,
	// in Result.IndexerID, and as the registry key.
	ID() string

	// Name is the human-readable name shown in the TUI.
	Name() string

	// Caps declares what this source can do. See Caps.
	Caps() Caps

	// Search runs the query against this source and returns its results.
	// It returns an error only for a failure of this source; the registry
	// collects those per-source and degrades rather than failing the whole
	// search (AGENT.md §6.3). Zero results with a nil error is a valid,
	// non-error outcome.
	Search(ctx context.Context, q Query) ([]Result, error)

	// Resolve fills Magnet/InfoHash for indexers that only return a details
	// page. Must be a no-op returning r unchanged when already resolved.
	Resolve(ctx context.Context, r Result) (Result, error)
}
