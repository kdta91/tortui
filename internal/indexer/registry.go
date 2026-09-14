package indexer

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Defaults for Config. Every one of them is a deliberate ceiling on how hard
// tortui may lean on someone else's server (AGENT.md §2, §6.13), not a
// performance tuning knob.
const (
	// DefaultSearchTimeout is the per-indexer deadline applied to one
	// Search call (AGENT.md §6.2). It is per source, not per fan-out: one
	// slow source is abandoned at this point while every other source
	// keeps its own full budget.
	DefaultSearchTimeout = 15 * time.Second

	// DefaultCacheTTL is how long a source's answer to one exact query
	// stays reusable. Short on purpose: the point is that a user mashing
	// the refresh key re-renders instead of re-fetching, not that tortui
	// keeps an index of its own (AGENT.md §2).
	DefaultCacheTTL = 60 * time.Second

	// DefaultMinRefreshInterval is the floor between two outbound requests
	// to the same source, whatever the query. The cache already absorbs a
	// repeated identical query, so this only ever bites when a *different*
	// query reaches the same source within the interval — which is the
	// hammering AGENT.md §6.13 exists to stop.
	DefaultMinRefreshInterval = 1 * time.Second
)

// ExtraKeySources is the Result.Extra key under which SearchAll records every
// source that contributed a copy of a merged result, as a comma-separated list
// of indexer ids in the order the sources were queried. It is always set on a
// result SearchAll returns, and holds a single id for a result only one source
// produced.
//
// It is bookkeeping the registry writes for display, not something core logic
// may branch on, and no adapter may set it: the registry overwrites it.
const ExtraKeySources = "tortui.sources"

// Registry errors. Callers match them with errors.Is; a SourceError unwraps to
// the one that explains that source's outcome.
var (
	// ErrNilIndexer reports Register being handed a nil Indexer. It cannot
	// catch a non-nil interface holding a nil pointer — that is the
	// caller's problem, and it panics on first use like any other nil
	// dereference.
	ErrNilIndexer = errors.New("indexer is nil")

	// ErrEmptyID reports an Indexer whose ID is empty or only whitespace.
	// The id is the registry key, the config key, and Result.IndexerID, so
	// there is no useful behaviour for a source without one.
	ErrEmptyID = errors.New("indexer id is empty")

	// ErrDuplicateID reports a second Register of an id already held. Ids
	// are the identity of a source across config, results, and errors, so
	// silently replacing one would misattribute both.
	ErrDuplicateID = errors.New("an indexer with this id is already registered")

	// ErrUnknownIndexer reports an id no source is registered under.
	ErrUnknownIndexer = errors.New("no indexer is registered with this id")

	// ErrNoSources reports a SearchAll that had nothing to query: an empty
	// registry, or no enabled source. It is distinct from
	// ErrAllSourcesFailed because nothing failed — there is nothing
	// configured, which is a different thing for the TUI to say.
	ErrNoSources = errors.New("no sources to search")

	// ErrUnsupportedMode reports a source skipped because it does not
	// declare the capability this query mode needs (AGENT.md §6.3). It is
	// carried by a SourceError with Skipped set, and never counts towards
	// ErrAllSourcesFailed.
	ErrUnsupportedMode = errors.New("source does not support this query mode")

	// ErrThrottled reports a source skipped because it was queried more
	// recently than Config.MinRefreshInterval allows (AGENT.md §6.13). As
	// with ErrUnsupportedMode it is a skip, not a failure.
	ErrThrottled = errors.New("source was queried too recently")

	// ErrSourcePanic reports an adapter that panicked. Adapters must not
	// panic (AGENT.md §6.9), but one failing source may never take the
	// application down with it (AGENT.md §6.3), so the registry recovers
	// and reports it as that source's failure.
	ErrSourcePanic = errors.New("indexer panicked")

	// ErrAllSourcesFailed reports that every source that was actually
	// queried failed. It is the only fatal outcome of a fan-out: as long
	// as one source succeeded, SearchAll returns partial results and a nil
	// error (AGENT.md §6.3).
	ErrAllSourcesFailed = errors.New("every source failed")
)

// Config tunes a Registry's fan-out. A zero or negative value in any field
// selects that field's default, so the zero Config is the documented one.
type Config struct {
	// Timeout is the per-indexer deadline for one Search call. Default
	// DefaultSearchTimeout.
	Timeout time.Duration

	// CacheTTL is how long a cached answer to one exact query is served
	// without re-fetching. Default DefaultCacheTTL.
	CacheTTL time.Duration

	// MinRefreshInterval is the minimum time between two outbound requests
	// to the same source. Default DefaultMinRefreshInterval.
	MinRefreshInterval time.Duration
}

// SourceError is one source's non-result outcome in a fan-out: it either
// failed, or it was skipped without being queried at all.
//
// The distinction is the whole point of the type. A skip is not a failure: it
// never counts towards ErrAllSourcesFailed, and the TUI should report it
// differently from a source that is actually broken (AGENT.md §6.3).
type SourceError struct {
	// IndexerID is the source this outcome belongs to. For an id no source
	// is registered under, it is the id the caller asked for.
	IndexerID string

	// Skipped is true when the source was never queried — it lacks a
	// capability this query needs (ErrUnsupportedMode), or it was queried
	// too recently (ErrThrottled).
	Skipped bool

	// Err is why. Match it with errors.Is; SourceError unwraps to it.
	Err error
}

// Error names the source and whether it failed or was skipped, so a collected
// error reads usefully on its own (AGENT.md §6.9).
func (e SourceError) Error() string {
	outcome := "failed"
	if e.Skipped {
		outcome = "skipped"
	}
	return fmt.Sprintf("indexer %q %s: %v", e.IndexerID, outcome, e.Err)
}

// Unwrap returns the underlying cause so errors.Is and errors.As reach it.
func (e SourceError) Unwrap() error { return e.Err }

// source is one registered Indexer plus the registry's bookkeeping about it.
type source struct {
	ix      Indexer
	enabled bool

	// lastFetch is when an outbound request to this source was last
	// started — reserved before the request is made, so concurrent
	// callers cannot both slip under the floor.
	lastFetch time.Time
}

// cacheKey identifies one source's answer to one exact query. Every field of
// Query that changes what a source would return is part of it; categories are
// canonicalised so that the same set in a different order is the same key.
type cacheKey struct {
	indexerID  string
	mode       Mode
	text       string
	categories string
	minSeeders int
	limit      int
	offset     int
}

type cacheEntry struct {
	results []Result
	fetched time.Time
}

// Registry is the named set of search sources and the fan-out across them. It
// is the only consumer of adapters (AGENT.md §4): nothing else in tortui may
// import an adapter package, and nothing here knows what kind of source it is
// talking to.
//
// A Registry is safe for concurrent use. The zero value is not usable; call
// NewRegistry.
type Registry struct {
	cfg Config

	// now is the registry's clock, injectable so the cache and the refresh
	// floor are testable without sleeping. Read only under mu.
	now func() time.Time

	mu      sync.Mutex
	sources map[string]*source
	order   []string
	cache   map[cacheKey]cacheEntry
}

// NewRegistry returns an empty Registry configured by cfg, with any zero or
// negative field replaced by its default.
func NewRegistry(cfg Config) *Registry {
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultSearchTimeout
	}
	if cfg.CacheTTL <= 0 {
		cfg.CacheTTL = DefaultCacheTTL
	}
	if cfg.MinRefreshInterval <= 0 {
		cfg.MinRefreshInterval = DefaultMinRefreshInterval
	}
	return &Registry{
		cfg:     cfg,
		now:     time.Now,
		sources: make(map[string]*source),
		cache:   make(map[cacheKey]cacheEntry),
	}
}

// Register adds ix under its own ID, enabled. It returns ErrNilIndexer,
// ErrEmptyID, or ErrDuplicateID rather than replacing or ignoring a source,
// because every one of those cases is a wiring bug the caller should see.
func (r *Registry) Register(ix Indexer) error {
	if ix == nil {
		return ErrNilIndexer
	}
	id := ix.ID()
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("registering indexer %q: %w", ix.Name(), ErrEmptyID)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.sources[id]; exists {
		return fmt.Errorf("registering indexer %q: %w", id, ErrDuplicateID)
	}
	r.sources[id] = &source{ix: ix, enabled: true}
	r.order = append(r.order, id)
	return nil
}

// Get returns the source registered under id, and whether there was one.
func (r *Registry) Get(id string) (Indexer, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sources[id]
	if !ok {
		return nil, false
	}
	return s.ix, true
}

// List returns every registered source, enabled or not, in registration order.
// The slice is a fresh copy the caller may keep and reorder.
func (r *Registry) List() []Indexer {
	return r.snapshot(false)
}

// Enabled returns the registered sources that are enabled, in registration
// order. It is what SearchAll fans out to when the caller names no ids.
func (r *Registry) Enabled() []Indexer {
	return r.snapshot(true)
}

func (r *Registry) snapshot(enabledOnly bool) []Indexer {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Indexer, 0, len(r.order))
	for _, id := range r.order {
		s := r.sources[id]
		if enabledOnly && !s.enabled {
			continue
		}
		out = append(out, s.ix)
	}
	return out
}

// SetEnabled turns a registered source on or off for the default fan-out. It
// returns ErrUnknownIndexer for an id that was never registered.
//
// Disabling is not deregistering: a disabled source stays in List, keeps its
// place in registration order, and is still queried when a caller names it
// explicitly in SearchAll.
func (r *Registry) SetEnabled(id string, enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sources[id]
	if !ok {
		return fmt.Errorf("indexer %q: %w", id, ErrUnknownIndexer)
	}
	s.enabled = enabled
	return nil
}

// selection is one entry of a fan-out. A nil ix means the caller named an id
// no source is registered under.
type selection struct {
	id string
	ix Indexer
}

// selectSources resolves the fan-out's targets. With no ids it is every
// enabled source in registration order; with ids it is exactly those, in the
// order given, with repeats collapsed and unknown ids kept so they can be
// reported.
func (r *Registry) selectSources(ids []string) []selection {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(ids) == 0 {
		out := make([]selection, 0, len(r.order))
		for _, id := range r.order {
			if s := r.sources[id]; s.enabled {
				out = append(out, selection{id: id, ix: s.ix})
			}
		}
		return out
	}

	seen := make(map[string]bool, len(ids))
	out := make([]selection, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if s, ok := r.sources[id]; ok {
			out = append(out, selection{id: id, ix: s.ix})
			continue
		}
		out = append(out, selection{id: id})
	}
	return out
}

// SearchAll runs q against the selected sources concurrently and returns their
// merged, ordered results, one SourceError per source that failed or was
// skipped, and a fatal error.
//
// With no ids it queries every enabled source; with ids it queries exactly
// those, whether or not they are enabled, since naming a source is a more
// specific instruction than the enabled flag.
//
// Concurrency and deadlines (AGENT.md §6.2). Every source is queried in its
// own goroutine under its own Config.Timeout derived from ctx, so one slow
// source is abandoned at its deadline while the others return normally.
// SearchAll never outlives the slowest source's timeout, and never leaves a
// goroutine of its own running after it returns.
//
// Degradation (AGENT.md §6.3). A source that fails is collected as a
// SourceError and the fan-out continues. A source that lacks the capability
// this mode needs, or that was queried inside Config.MinRefreshInterval, is
// skipped: it is reported as a SourceError with Skipped set, and it counts
// neither as a success nor as a failure. The returned error is non-nil in
// exactly two cases:
//
//   - ErrNoSources, when there was nothing to query at all; and
//   - ErrAllSourcesFailed, when at least one source failed and none succeeded.
//     Skips do not count, so "all skipped" is a nil error with no results, and
//     "some skipped, the rest failed" is ErrAllSourcesFailed.
//
// Results are deduplicated and ordered; see mergeResults. Both are
// deterministic: the same answers from the same sources produce the same
// output regardless of which source replied first.
func (r *Registry) SearchAll(ctx context.Context, q Query, ids ...string) ([]Result, []SourceError, error) {
	selected := r.selectSources(ids)
	if len(selected) == 0 {
		return nil, nil, ErrNoSources
	}

	var (
		groups = make([][]Result, len(selected))
		errs   = make([]*SourceError, len(selected))
		wg     sync.WaitGroup
	)
	for i, sel := range selected {
		if sel.ix == nil {
			errs[i] = &SourceError{IndexerID: sel.id, Err: ErrUnknownIndexer}
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			groups[i], errs[i] = r.searchOne(ctx, sel.ix, q)
		}()
	}
	wg.Wait()

	var (
		sourceErrs []SourceError
		okIDs      = make([]string, 0, len(selected))
		okGroups   = make([][]Result, 0, len(selected))
		failed     int
	)
	for i, sel := range selected {
		if e := errs[i]; e != nil {
			sourceErrs = append(sourceErrs, *e)
			if !e.Skipped {
				failed++
			}
			continue
		}
		okIDs = append(okIDs, sel.id)
		okGroups = append(okGroups, groups[i])
	}

	results := mergeResults(q.Mode, okIDs, okGroups)
	if failed > 0 && len(okIDs) == 0 {
		return results, sourceErrs, fmt.Errorf("%w (%d of %d sources)", ErrAllSourcesFailed, failed, len(selected))
	}
	return results, sourceErrs, nil
}

// searchOne is one source's half of a fan-out: capability check, cache, the
// refresh floor, then the request itself. It returns either results or a
// SourceError, never both.
func (r *Registry) searchOne(ctx context.Context, ix Indexer, q Query) ([]Result, *SourceError) {
	id := ix.ID()

	if err := supports(ix.Caps(), q.Mode); err != nil {
		return nil, &SourceError{IndexerID: id, Skipped: true, Err: err}
	}

	key := newCacheKey(id, q)
	if cached, hit := r.cachedResults(key); hit {
		return cached, nil
	}

	if !r.reserveFetch(id) {
		return nil, &SourceError{
			IndexerID: id,
			Skipped:   true,
			Err:       fmt.Errorf("%w (minimum %s between requests to one source)", ErrThrottled, r.cfg.MinRefreshInterval),
		}
	}

	sctx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()

	results, err := callSearch(sctx, ix, q, r.cfg.Timeout)
	if err != nil {
		return nil, &SourceError{IndexerID: id, Err: err}
	}
	r.storeResults(key, results)
	return results, nil
}

// supports reports whether caps allow mode, as the error explaining the skip
// if they do not. An unrecognised mode is a skip too: the registry will not
// send a source a request it cannot describe.
func supports(caps Caps, mode Mode) error {
	switch mode {
	case ModeSearch:
		if !caps.Search {
			return fmt.Errorf("%w: keyword search", ErrUnsupportedMode)
		}
	case ModeLatest:
		if !caps.Latest {
			return fmt.Errorf("%w: latest feed", ErrUnsupportedMode)
		}
	default:
		return fmt.Errorf("%w: mode(%d)", ErrUnsupportedMode, int(mode))
	}
	return nil
}

// searchOutcome is what one Search call produced, carried back over a buffered
// channel so the send never blocks even when nobody is listening any more.
type searchOutcome struct {
	results []Result
	err     error
}

// callSearch runs ix.Search under ctx and returns as soon as either the source
// answers or ctx is done — whichever happens first.
//
// The call runs in its own goroutine for two reasons. An adapter that ignores
// its context cannot be allowed to hold the whole fan-out open past its
// deadline, and Go offers no way to abandon a blocked call in place. And an
// adapter that panics must not take the application down (AGENT.md §6.3), so
// the panic is recovered here, on the goroutine that can actually see it, and
// reported as that source's failure.
//
// A source that answers after its deadline has passed is harmless: the channel
// is buffered, so the late send completes and the goroutine exits with its
// answer discarded. Nothing leaks that the adapter itself was not already
// holding open. A source that answers in the same instant its deadline expires
// may be reported either way; both are true, and neither loses a source that
// is actually working.
func callSearch(ctx context.Context, ix Indexer, q Query, timeout time.Duration) ([]Result, error) {
	done := make(chan searchOutcome, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				done <- searchOutcome{err: fmt.Errorf("%w: %v", ErrSourcePanic, p)}
			}
		}()
		results, err := ix.Search(ctx, q)
		done <- searchOutcome{results: results, err: err}
	}()

	select {
	case out := <-done:
		return out.results, out.err
	case <-ctx.Done():
		return nil, fmt.Errorf("no answer within %s: %w", timeout, ctx.Err())
	}
}

// newCacheKey builds the cache key for one source's answer to q.
func newCacheKey(indexerID string, q Query) cacheKey {
	return cacheKey{
		indexerID:  indexerID,
		mode:       q.Mode,
		text:       q.Text,
		categories: categoryKey(q.Categories),
		minSeeders: q.MinSeeders,
		limit:      q.Limit,
		offset:     q.Offset,
	}
}

// categoryKey canonicalises a category filter into a comparable string:
// deduplicated and sorted, so the same set asked for in a different order is
// the same cache entry rather than a second request.
func categoryKey(cats []Category) string {
	if len(cats) == 0 {
		return ""
	}
	tokens := make([]string, 0, len(cats))
	seen := make(map[Category]bool, len(cats))
	for _, c := range cats {
		if seen[c] {
			continue
		}
		seen[c] = true
		tokens = append(tokens, c.String())
	}
	sort.Strings(tokens)
	return strings.Join(tokens, ",")
}

// cachedResults returns a private copy of the cached answer for key, if there
// is one and it is still inside the TTL.
//
// The cache is process-local memory and nothing else: it is never written to
// disk, never shared between users, and holds only what the user's own sources
// answered during this run (AGENT.md §2).
func (r *Registry) cachedResults(key cacheKey) ([]Result, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.cache[key]
	if !ok {
		return nil, false
	}
	if r.now().Sub(entry.fetched) >= r.cfg.CacheTTL {
		delete(r.cache, key)
		return nil, false
	}
	return cloneResults(entry.results), true
}

// storeResults caches a private copy of a source's answer and drops every
// entry that has aged out, so the map cannot grow across a long session.
//
// Only a successful fetch is cached. A failure is not: caching it would keep a
// source dark for the whole TTL over one transient error.
func (r *Registry) storeResults(key cacheKey, results []Result) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now()
	for k, entry := range r.cache {
		if now.Sub(entry.fetched) >= r.cfg.CacheTTL {
			delete(r.cache, k)
		}
	}
	r.cache[key] = cacheEntry{results: cloneResults(results), fetched: now}
}

// reserveFetch reports whether an outbound request to id may be made now, and
// claims the slot if so.
//
// Claiming happens before the request rather than after it, and under the same
// lock as the check, so two concurrent fan-outs cannot both decide they are
// first. A cache hit never calls this: it makes no request, so it must not
// push the next allowed one further out.
func (r *Registry) reserveFetch(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.sources[id]
	if !ok {
		return false
	}
	now := r.now()
	if !s.lastFetch.IsZero() && now.Sub(s.lastFetch) < r.cfg.MinRefreshInterval {
		return false
	}
	s.lastFetch = now
	return true
}

// cloneResults copies results deeply enough that the copy and the original
// share nothing mutable: the slice is fresh and so is each Result's Extra map.
func cloneResults(in []Result) []Result {
	out := slices.Clone(in)
	for i := range out {
		out[i].Extra = maps.Clone(out[i].Extra)
	}
	return out
}

// merged is one deduplicated result plus the sources that contributed a copy.
type merged struct {
	res     Result
	sources []string
}

// mergeResults deduplicates results across sources and orders them.
//
// Two results are the same item when they carry the same infohash, and
// otherwise when they carry the same normalised title and the same size. A
// result with neither an infohash nor a title with anything in it is left
// alone: there is nothing to match on, and merging on emptiness would fold
// unrelated rows together.
//
// The surviving copy is the one with the most seeders, which is the copy whose
// magnet is most likely to actually resolve into a swarm. Every contributing
// source id is recorded in Extra under ExtraKeySources, in the order the
// sources were queried, so the TUI can show that a row came from several
// places.
//
// Ordering is by mode: seeders descending for ModeSearch, published descending
// for ModeLatest (AGENT.md §7 makes seeders the default sort of the results
// table). Ties break on the other of those two, then title, then indexer id,
// then result id, so the output is fully determined by the answers and never
// by which source replied first.
func mergeResults(mode Mode, ids []string, groups [][]Result) []Result {
	var (
		order   []*merged
		index   = make(map[string]*merged)
		unnamed int
	)
	for i, group := range groups {
		id := ids[i]
		for _, res := range group {
			key, ok := dedupKey(res)
			if !ok {
				// Nothing to match on: give it a key nothing else
				// can collide with.
				unnamed++
				key = fmt.Sprintf("unmatchable:%d", unnamed)
			}

			existing, seen := index[key]
			if !seen {
				m := &merged{res: res, sources: []string{id}}
				index[key] = m
				order = append(order, m)
				continue
			}
			if !slices.Contains(existing.sources, id) {
				existing.sources = append(existing.sources, id)
			}
			if res.Seeders > existing.res.Seeders {
				existing.res = res
			}
		}
	}

	out := make([]Result, 0, len(order))
	for _, m := range order {
		res := m.res
		extra := make(map[string]string, len(res.Extra)+1)
		maps.Copy(extra, res.Extra)
		extra[ExtraKeySources] = strings.Join(m.sources, ",")
		res.Extra = extra
		out = append(out, res)
	}
	sortResults(out, mode)
	return out
}

// dedupKey returns the identity a result is merged on, and whether it has one
// at all.
func dedupKey(r Result) (string, bool) {
	if hash := strings.ToLower(strings.TrimSpace(r.InfoHash)); hash != "" {
		return "infohash:" + hash, true
	}
	if title := normaliseTitle(r.Title); title != "" {
		return fmt.Sprintf("title:%s|%d", title, r.SizeBytes), true
	}
	return "", false
}

// normaliseTitle reduces a title to the form two sources are likely to agree
// on: lowercased, with every run of anything that is not a letter or a digit
// collapsed to a single space. Sources differ in separators, bracketing and
// case far more often than they differ in words.
func normaliseTitle(s string) string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return strings.Join(fields, " ")
}

// sortResults applies the mode's default ordering in place.
func sortResults(rs []Result, mode Mode) {
	sort.SliceStable(rs, func(i, j int) bool {
		a, b := rs[i], rs[j]
		if mode == ModeLatest {
			if !a.Published.Equal(b.Published) {
				return a.Published.After(b.Published)
			}
			if a.Seeders != b.Seeders {
				return a.Seeders > b.Seeders
			}
		} else {
			if a.Seeders != b.Seeders {
				return a.Seeders > b.Seeders
			}
			if !a.Published.Equal(b.Published) {
				return a.Published.After(b.Published)
			}
		}
		if a.Title != b.Title {
			return a.Title < b.Title
		}
		if a.IndexerID != b.IndexerID {
			return a.IndexerID < b.IndexerID
		}
		return a.ID < b.ID
	})
}
