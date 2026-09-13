package indexer

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// test doubles
//
// Every source in this file is a fake defined here. The registry is the only
// consumer of adapters (AGENT.md §4) and no adapter exists yet anyway (T-021,
// T-022), so nothing here imports one. Invented names and example.org URLs
// satisfy AGENT.md §2.
// ---------------------------------------------------------------------------

var (
	searchCaps = Caps{Search: true}
	bothCaps   = Caps{Search: true, Latest: true}
	latestCaps = Caps{Latest: true}
)

// stubIndexer is a programmable Indexer. fn is what Search does; a nil fn
// returns no results and no error, which is the "source has nothing for this
// query" case the registry must treat as success.
type stubIndexer struct {
	id    string
	name  string
	caps  Caps
	fn    func(ctx context.Context, q Query) ([]Result, error)
	calls atomic.Int32
}

func (s *stubIndexer) ID() string { return s.id }

func (s *stubIndexer) Name() string {
	if s.name == "" {
		return s.id
	}
	return s.name
}

func (s *stubIndexer) Caps() Caps { return s.caps }

func (s *stubIndexer) Search(ctx context.Context, q Query) ([]Result, error) {
	s.calls.Add(1)
	if s.fn == nil {
		return nil, nil
	}
	return s.fn(ctx, q)
}

func (s *stubIndexer) Resolve(_ context.Context, r Result) (Result, error) { return r, nil }

var _ Indexer = (*stubIndexer)(nil)

// okSource returns a source that always answers with rs.
func okSource(id string, rs ...Result) *stubIndexer {
	return &stubIndexer{id: id, caps: bothCaps, fn: func(_ context.Context, _ Query) ([]Result, error) {
		return rs, nil
	}}
}

// failSource returns a source whose Search always fails with err.
func failSource(id string, err error) *stubIndexer {
	return &stubIndexer{id: id, caps: bothCaps, fn: func(_ context.Context, _ Query) ([]Result, error) {
		return nil, err
	}}
}

// hit builds a minimal usable Result.
func hit(indexerID, id, title string, seeders int) Result {
	return Result{
		IndexerID: indexerID,
		ID:        id,
		Title:     title,
		Magnet:    testMagnet,
		Seeders:   seeders,
	}
}

// fakeClock is the registry's time source in every test that exercises the
// cache or the minimum refresh interval, so those tests assert on elapsed time
// the test controls exactly rather than on wall clock.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, time.September, 13, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// newTestRegistry builds a registry on a fake clock with sources registered in
// the order given.
func newTestRegistry(t *testing.T, cfg Config, sources ...Indexer) (*Registry, *fakeClock) {
	t.Helper()
	r := NewRegistry(cfg)
	clock := newFakeClock()
	r.now = clock.now
	for _, s := range sources {
		if err := r.Register(s); err != nil {
			t.Fatalf("Register(%s): %v", s.ID(), err)
		}
	}
	return r, clock
}

// titles is the shorthand every ordering assertion uses.
func titles(rs []Result) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.Title
	}
	return out
}

func joined(ss []string) string { return strings.Join(ss, ",") }

func sourceErrIDs(errs []SourceError) []string {
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.IndexerID
	}
	return out
}

// ---------------------------------------------------------------------------
// Register / Get / List / Enabled
// ---------------------------------------------------------------------------

func TestRegistryRegisterAndGet(t *testing.T) {
	a, b := okSource("alpha"), okSource("beta")
	r, _ := newTestRegistry(t, Config{}, a, b)

	got, ok := r.Get("alpha")
	if !ok || got != Indexer(a) {
		t.Errorf("Get(alpha) = %v, %v; want the registered alpha source, true", got, ok)
	}
	if _, ok := r.Get("missing"); ok {
		t.Error("Get(missing) reported ok for an id that was never registered")
	}

	if names := ids(r.List()); joined(names) != "alpha,beta" {
		t.Errorf("List() = %v, want [alpha beta] in registration order", names)
	}
	if names := ids(r.Enabled()); joined(names) != "alpha,beta" {
		t.Errorf("Enabled() = %v, want every source enabled on registration", names)
	}
}

func ids(ixs []Indexer) []string {
	out := make([]string, len(ixs))
	for i, ix := range ixs {
		out[i] = ix.ID()
	}
	return out
}

func TestRegistryRegisterRejects(t *testing.T) {
	r := NewRegistry(Config{})

	if err := r.Register(nil); !errors.Is(err, ErrNilIndexer) {
		t.Errorf("Register(nil) = %v, want ErrNilIndexer", err)
	}
	if err := r.Register(&stubIndexer{id: "  "}); !errors.Is(err, ErrEmptyID) {
		t.Errorf("Register(whitespace id) = %v, want ErrEmptyID", err)
	}
	if err := r.Register(okSource("alpha")); err != nil {
		t.Fatalf("Register(alpha): %v", err)
	}
	err := r.Register(okSource("alpha"))
	if !errors.Is(err, ErrDuplicateID) {
		t.Errorf("re-Register(alpha) = %v, want ErrDuplicateID", err)
	}
	if err != nil && !strings.Contains(err.Error(), "alpha") {
		t.Errorf("duplicate error %q does not name the offending id (AGENT.md §6.9)", err)
	}
	if n := len(r.List()); n != 1 {
		t.Errorf("List() has %d sources after a rejected duplicate, want 1", n)
	}
}

func TestRegistrySetEnabled(t *testing.T) {
	r, _ := newTestRegistry(t, Config{}, okSource("alpha"), okSource("beta"))

	if err := r.SetEnabled("beta", false); err != nil {
		t.Fatalf("SetEnabled(beta, false): %v", err)
	}
	if names := ids(r.Enabled()); joined(names) != "alpha" {
		t.Errorf("Enabled() = %v, want [alpha] after disabling beta", names)
	}
	if names := ids(r.List()); joined(names) != "alpha,beta" {
		t.Errorf("List() = %v, want both sources; List is not filtered by enablement", names)
	}
	if err := r.SetEnabled("beta", true); err != nil {
		t.Fatalf("SetEnabled(beta, true): %v", err)
	}
	if names := ids(r.Enabled()); joined(names) != "alpha,beta" {
		t.Errorf("Enabled() = %v, want both after re-enabling beta", names)
	}
	if err := r.SetEnabled("missing", true); !errors.Is(err, ErrUnknownIndexer) {
		t.Errorf("SetEnabled(missing) = %v, want ErrUnknownIndexer", err)
	}
}

func TestRegistryListReturnsACopy(t *testing.T) {
	r, _ := newTestRegistry(t, Config{}, okSource("alpha"), okSource("beta"))

	got := r.List()
	got[0] = nil

	if names := ids(r.List()); joined(names) != "alpha,beta" {
		t.Errorf("List() = %v after a caller mutated an earlier List result; want [alpha beta]", names)
	}
	if names := ids(r.Enabled()); joined(names) != "alpha,beta" {
		t.Errorf("Enabled() = %v after a caller mutated an earlier List result; want [alpha beta]", names)
	}
}

func TestNewRegistryDefaults(t *testing.T) {
	r := NewRegistry(Config{})
	if r.cfg.Timeout != DefaultSearchTimeout {
		t.Errorf("zero Config.Timeout = %s, want the %s default", r.cfg.Timeout, DefaultSearchTimeout)
	}
	if r.cfg.CacheTTL != DefaultCacheTTL {
		t.Errorf("zero Config.CacheTTL = %s, want the %s default", r.cfg.CacheTTL, DefaultCacheTTL)
	}
	if r.cfg.MinRefreshInterval != DefaultMinRefreshInterval {
		t.Errorf("zero Config.MinRefreshInterval = %s, want the %s default", r.cfg.MinRefreshInterval, DefaultMinRefreshInterval)
	}

	neg := NewRegistry(Config{Timeout: -time.Second, CacheTTL: -time.Second, MinRefreshInterval: -time.Second})
	if neg.cfg.Timeout != DefaultSearchTimeout || neg.cfg.CacheTTL != DefaultCacheTTL || neg.cfg.MinRefreshInterval != DefaultMinRefreshInterval {
		t.Errorf("negative durations = %+v, want every one of them replaced by its default", neg.cfg)
	}

	set := Config{Timeout: time.Second, CacheTTL: 2 * time.Second, MinRefreshInterval: 3 * time.Second}
	if got := NewRegistry(set).cfg; got != set {
		t.Errorf("explicit Config = %+v, want it honoured unchanged (%+v)", got, set)
	}
}

// ---------------------------------------------------------------------------
// SearchAll: the happy paths
// ---------------------------------------------------------------------------

func TestSearchAllAllSourcesSucceed(t *testing.T) {
	a := okSource("alpha", hit("alpha", "1", "alpha one", 10))
	b := okSource("beta", hit("beta", "1", "beta one", 30), hit("beta", "2", "beta two", 20))
	r, _ := newTestRegistry(t, Config{}, a, b)

	results, srcErrs, err := r.SearchAll(context.Background(), Query{Mode: ModeSearch, Text: "one"})
	if err != nil {
		t.Fatalf("SearchAll: unexpected error %v", err)
	}
	if len(srcErrs) != 0 {
		t.Errorf("SearchAll reported %v, want no per-source errors", srcErrs)
	}
	if got := joined(titles(results)); got != "beta one,beta two,alpha one" {
		t.Errorf("results = %v, want seeders descending [beta one beta two alpha one]", titles(results))
	}
	for _, res := range results {
		if res.Extra[ExtraKeySources] == "" {
			t.Errorf("result %q has no %s entry; every result records its contributing sources", res.Title, ExtraKeySources)
		}
	}
}

func TestSearchAllQueriesOnlyTheSelectedSources(t *testing.T) {
	a := okSource("alpha", hit("alpha", "1", "alpha one", 1))
	b := okSource("beta", hit("beta", "1", "beta one", 1))
	c := okSource("gamma", hit("gamma", "1", "gamma one", 1))
	r, _ := newTestRegistry(t, Config{}, a, b, c)

	// A repeated id must not produce a second query or a duplicate row.
	results, srcErrs, err := r.SearchAll(context.Background(), Query{Text: "x"}, "gamma", "alpha", "gamma")
	if err != nil {
		t.Fatalf("SearchAll: %v", err)
	}
	if len(srcErrs) != 0 {
		t.Errorf("SearchAll reported %v, want none", srcErrs)
	}
	if b.calls.Load() != 0 {
		t.Errorf("beta was queried %d times, want 0: it was not in the id list", b.calls.Load())
	}
	if got := c.calls.Load(); got != 1 {
		t.Errorf("gamma was queried %d times, want 1: a repeated id must be de-duplicated", got)
	}
	if len(results) != 2 {
		t.Errorf("results = %v, want one row per selected source", titles(results))
	}
}

func TestSearchAllZeroResults(t *testing.T) {
	// A nil slice with a nil error is a valid, non-error outcome.
	r, _ := newTestRegistry(t, Config{}, &stubIndexer{id: "alpha", caps: bothCaps}, okSource("beta"))

	results, srcErrs, err := r.SearchAll(context.Background(), Query{Text: "nothing matches"})
	if err != nil {
		t.Fatalf("SearchAll over sources with no matches = error %v, want nil", err)
	}
	if len(srcErrs) != 0 {
		t.Errorf("SearchAll reported %v, want no per-source errors", srcErrs)
	}
	if len(results) != 0 {
		t.Errorf("results = %v, want none", titles(results))
	}
}

func TestSearchAllNoSourcesSelected(t *testing.T) {
	empty := NewRegistry(Config{})
	if _, _, err := empty.SearchAll(context.Background(), Query{Text: "x"}); !errors.Is(err, ErrNoSources) {
		t.Errorf("SearchAll on an empty registry = %v, want ErrNoSources", err)
	}

	r, _ := newTestRegistry(t, Config{}, okSource("alpha"))
	if err := r.SetEnabled("alpha", false); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	_, _, err := r.SearchAll(context.Background(), Query{Text: "x"})
	if !errors.Is(err, ErrNoSources) {
		t.Errorf("SearchAll with every source disabled = %v, want ErrNoSources", err)
	}
}

func TestSearchAllUnknownID(t *testing.T) {
	a := okSource("alpha", hit("alpha", "1", "alpha one", 5))
	r, _ := newTestRegistry(t, Config{}, a)

	results, srcErrs, err := r.SearchAll(context.Background(), Query{Text: "x"}, "alpha", "ghost")
	if err != nil {
		t.Fatalf("SearchAll = %v, want nil: one source succeeded", err)
	}
	if len(results) != 1 {
		t.Errorf("results = %v, want alpha's single row", titles(results))
	}
	if len(srcErrs) != 1 || srcErrs[0].IndexerID != "ghost" || !errors.Is(srcErrs[0], ErrUnknownIndexer) {
		t.Fatalf("source errors = %v, want one ErrUnknownIndexer for ghost", srcErrs)
	}
	if srcErrs[0].Skipped {
		t.Error("an unknown id is reported as skipped; it is a caller error, not a capability skip")
	}

	// An unknown id on its own is a failure with no successes.
	_, srcErrs, err = r.SearchAll(context.Background(), Query{Text: "x"}, "ghost")
	if !errors.Is(err, ErrAllSourcesFailed) {
		t.Errorf("SearchAll(ghost) = %v, want ErrAllSourcesFailed", err)
	}
	if len(srcErrs) != 1 {
		t.Errorf("source errors = %v, want exactly one", srcErrs)
	}
}

// ---------------------------------------------------------------------------
// SearchAll: failure, partial failure, and the all-failed contract
// ---------------------------------------------------------------------------

func TestSearchAllOneSourceErrors(t *testing.T) {
	boom := errors.New("upstream returned 503")
	a := okSource("alpha", hit("alpha", "1", "alpha one", 7))
	b := failSource("beta", boom)
	r, _ := newTestRegistry(t, Config{}, a, b)

	results, srcErrs, err := r.SearchAll(context.Background(), Query{Text: "x"})
	if err != nil {
		t.Fatalf("SearchAll = %v, want nil: alpha succeeded (AGENT.md §6.3)", err)
	}
	if got := joined(titles(results)); got != "alpha one" {
		t.Errorf("results = %v, want alpha's row", titles(results))
	}
	if len(srcErrs) != 1 {
		t.Fatalf("source errors = %v, want exactly one", srcErrs)
	}
	if srcErrs[0].IndexerID != "beta" || srcErrs[0].Skipped || !errors.Is(srcErrs[0], boom) {
		t.Errorf("source error = %+v, want a non-skipped beta error wrapping the source's own error", srcErrs[0])
	}
	if !strings.Contains(srcErrs[0].Error(), "beta") {
		t.Errorf("source error message %q does not name the source (AGENT.md §6.9)", srcErrs[0])
	}
}

func TestSearchAllEverySourceFails(t *testing.T) {
	r, _ := newTestRegistry(t, Config{},
		failSource("alpha", errors.New("dns failure")),
		failSource("beta", errors.New("bad xml")),
	)

	results, srcErrs, err := r.SearchAll(context.Background(), Query{Text: "x"})
	if !errors.Is(err, ErrAllSourcesFailed) {
		t.Fatalf("SearchAll = %v, want ErrAllSourcesFailed", err)
	}
	if len(results) != 0 {
		t.Errorf("results = %v, want none", titles(results))
	}
	if got := joined(sourceErrIDs(srcErrs)); got != "alpha,beta" {
		t.Errorf("source errors = %v, want one per source in selection order", srcErrs)
	}
}

func TestSearchAllPanickingSourceIsAnErrorNotACrash(t *testing.T) {
	panicker := &stubIndexer{id: "alpha", caps: bothCaps, fn: func(_ context.Context, _ Query) ([]Result, error) {
		panic("adapter bug: index out of range")
	}}
	r, _ := newTestRegistry(t, Config{}, panicker, okSource("beta", hit("beta", "1", "beta one", 3)))

	results, srcErrs, err := r.SearchAll(context.Background(), Query{Text: "x"})
	if err != nil {
		t.Fatalf("SearchAll = %v, want nil: beta succeeded", err)
	}
	if got := joined(titles(results)); got != "beta one" {
		t.Errorf("results = %v, want beta's row", titles(results))
	}
	if len(srcErrs) != 1 || !errors.Is(srcErrs[0], ErrSourcePanic) {
		t.Fatalf("source errors = %v, want one ErrSourcePanic", srcErrs)
	}
	if !strings.Contains(srcErrs[0].Error(), "index out of range") {
		t.Errorf("panic error %q does not carry the panic value", srcErrs[0])
	}
}

func TestSearchAllParentContextAlreadyCancelled(t *testing.T) {
	r, _ := newTestRegistry(t, Config{}, okSource("alpha", hit("alpha", "1", "a", 1)))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, srcErrs, err := r.SearchAll(ctx, Query{Text: "x"})
	if !errors.Is(err, ErrAllSourcesFailed) {
		t.Fatalf("SearchAll with a cancelled context = %v, want ErrAllSourcesFailed", err)
	}
	if len(srcErrs) != 1 || !errors.Is(srcErrs[0], context.Canceled) {
		t.Errorf("source errors = %v, want one wrapping context.Canceled", srcErrs)
	}
}

// ---------------------------------------------------------------------------
// SearchAll: per-indexer timeout, concurrency, goroutine lifetime
// ---------------------------------------------------------------------------

// blockingSource blocks in Search until release is closed, or until its
// context is done. exited is closed once its Search call has fully returned,
// which is what proves the registry left nothing blocked behind it.
type blockingSource struct {
	*stubIndexer
	release chan struct{}
	exited  chan struct{}
}

func newBlockingSource(id string) *blockingSource {
	b := &blockingSource{
		release: make(chan struct{}),
		exited:  make(chan struct{}),
	}
	b.stubIndexer = &stubIndexer{id: id, caps: bothCaps, fn: func(_ context.Context, _ Query) ([]Result, error) {
		defer close(b.exited)
		<-b.release
		return []Result{hit(id, "1", id+" late row", 99)}, nil
	}}
	return b
}

func TestSearchAllOneSourceTimesOut(t *testing.T) {
	slow := newBlockingSource("slow")
	fast := okSource("fast", hit("fast", "1", "fast one", 4))
	r, _ := newTestRegistry(t, Config{Timeout: 50 * time.Millisecond}, slow, fast)

	results, srcErrs, err := r.SearchAll(context.Background(), Query{Text: "x"})
	if err != nil {
		t.Fatalf("SearchAll = %v, want nil: fast succeeded while slow timed out", err)
	}
	if got := joined(titles(results)); got != "fast one" {
		t.Errorf("results = %v, want only the fast source's row", titles(results))
	}
	if len(srcErrs) != 1 || srcErrs[0].IndexerID != "slow" {
		t.Fatalf("source errors = %v, want one for slow", srcErrs)
	}
	if !errors.Is(srcErrs[0], context.DeadlineExceeded) {
		t.Errorf("slow's error = %v, want it to wrap context.DeadlineExceeded", srcErrs[0])
	}
	if srcErrs[0].Skipped {
		t.Error("a timed-out source is reported as skipped; a timeout is a failure")
	}

	// The registry returned while the source was still blocked. Releasing it
	// now must let everything the registry started on its behalf finish:
	// the source's own call, and the goroutine the registry runs it on. The
	// second is the one that leaks if the answer has nowhere to go.
	close(slow.release)
	select {
	case <-slow.exited:
	case <-time.After(2 * time.Second):
		t.Fatal("the timed-out source's Search call never returned after being released")
	}
	waitForNoGoroutineIn(t, "indexer.callSearch.func1")
}

// waitForNoGoroutineIn fails the test if any goroutine is still inside frame
// after a couple of seconds. It polls rather than sampling once, so a
// goroutine that is merely on its way out is not mistaken for a leak.
func waitForNoGoroutineIn(t *testing.T, frame string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		buf := make([]byte, 1<<20)
		stacks := string(buf[:runtime.Stack(buf, true)])
		if !strings.Contains(stacks, frame) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("a goroutine is still parked in %s two seconds after the source was released; the registry leaked it:\n%s", frame, stacks)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSearchAllQueriesSourcesConcurrently(t *testing.T) {
	const n = 3

	// Each source parks until every other source has also been entered. If
	// the registry queried them one at a time this deadlocks until the
	// per-indexer timeout and the assertions below fail; there is no timing
	// window to tune.
	arrived := make(chan struct{}, n)
	release := make(chan struct{})
	sources := make([]Indexer, 0, n)
	for i := range n {
		id := fmt.Sprintf("source%d", i)
		sources = append(sources, &stubIndexer{id: id, caps: bothCaps, fn: func(ctx context.Context, _ Query) ([]Result, error) {
			arrived <- struct{}{}
			select {
			case <-release:
				return []Result{hit(id, "1", id+" row", 1)}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}})
	}
	r, _ := newTestRegistry(t, Config{Timeout: 2 * time.Second}, sources...)

	go func() {
		for range n {
			<-arrived
		}
		close(release)
	}()

	results, srcErrs, err := r.SearchAll(context.Background(), Query{Text: "x"})
	if err != nil || len(srcErrs) != 0 {
		t.Fatalf("SearchAll = %v / %v, want no errors: the sources are only released once all %d have been entered, so this fails if the fan-out is sequential", err, srcErrs, n)
	}
	if len(results) != n {
		t.Errorf("results = %v, want one row per source", titles(results))
	}
}

func TestSearchAllIsRaceFreeAgainstRegistration(t *testing.T) {
	r, _ := newTestRegistry(t, Config{Timeout: time.Second}, okSource("alpha", hit("alpha", "1", "alpha one", 1)))

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.Register(okSource(fmt.Sprintf("late%d", i), hit("late", "1", "late one", 1))); err != nil {
				t.Errorf("Register: %v", err)
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := r.SearchAll(context.Background(), Query{Text: "x"}); err != nil && !errors.Is(err, ErrAllSourcesFailed) {
				t.Errorf("SearchAll: %v", err)
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.List()
			_ = r.Enabled()
			_, _ = r.Get("alpha")
		}()
	}
	wg.Wait()

	if n := len(r.List()); n != 9 {
		t.Errorf("List() = %d sources, want 9", n)
	}
}

func TestSearchAllConcurrentCallsShareTheCache(t *testing.T) {
	counted := &stubIndexer{id: "alpha", caps: bothCaps, fn: func(_ context.Context, _ Query) ([]Result, error) {
		return []Result{hit("alpha", "1", "alpha one", 2)}, nil
	}}
	r, _ := newTestRegistry(t, Config{}, counted)

	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := r.SearchAll(context.Background(), Query{Text: "same"}); err != nil {
				t.Errorf("SearchAll: %v", err)
			}
		}()
	}
	wg.Wait()

	// The cache and the per-source refresh floor are both shared state: with
	// sixteen identical concurrent queries at one instant, at most one of
	// them may reach the source.
	if got := counted.calls.Load(); got != 1 {
		t.Errorf("the source was queried %d times by 16 concurrent identical searches, want exactly 1", got)
	}
}

// ---------------------------------------------------------------------------
// SearchAll: capability skips (AGENT.md §6.3)
// ---------------------------------------------------------------------------

func TestSearchAllLatestSkipsSourcesWithoutTheCapability(t *testing.T) {
	capable := okSource("alpha", Result{IndexerID: "alpha", ID: "1", Title: "alpha latest", Magnet: testMagnet})
	incapable := &stubIndexer{id: "beta", caps: searchCaps, fn: func(_ context.Context, _ Query) ([]Result, error) {
		t.Error("a source without Caps.Latest was queried for a ModeLatest query")
		return nil, nil
	}}
	r, _ := newTestRegistry(t, Config{}, capable, incapable)

	results, srcErrs, err := r.SearchAll(context.Background(), Query{Mode: ModeLatest})
	if err != nil {
		t.Fatalf("SearchAll = %v, want nil: a skip is not a failure", err)
	}
	if got := joined(titles(results)); got != "alpha latest" {
		t.Errorf("results = %v, want the capable source's row", titles(results))
	}
	if len(srcErrs) != 1 {
		t.Fatalf("source errors = %v, want one skip note", srcErrs)
	}
	if !srcErrs[0].Skipped || !errors.Is(srcErrs[0], ErrUnsupportedMode) {
		t.Errorf("beta's note = %+v, want Skipped with ErrUnsupportedMode", srcErrs[0])
	}
}

func TestSearchAllSearchSkipsSourcesWithoutTheCapability(t *testing.T) {
	r, _ := newTestRegistry(t, Config{}, &stubIndexer{id: "feedonly", caps: latestCaps, fn: func(_ context.Context, _ Query) ([]Result, error) {
		t.Error("a source without Caps.Search was queried for a ModeSearch query")
		return nil, nil
	}})

	_, srcErrs, err := r.SearchAll(context.Background(), Query{Mode: ModeSearch, Text: "x"})
	if err != nil {
		t.Fatalf("SearchAll = %v, want nil: every source was skipped, none failed", err)
	}
	if len(srcErrs) != 1 || !srcErrs[0].Skipped {
		t.Fatalf("source errors = %v, want one skip", srcErrs)
	}
}

func TestSearchAllUnknownModeSkips(t *testing.T) {
	r, _ := newTestRegistry(t, Config{}, &stubIndexer{id: "alpha", caps: bothCaps, fn: func(_ context.Context, _ Query) ([]Result, error) {
		t.Error("a source was queried for a mode the registry does not know")
		return nil, nil
	}})

	_, srcErrs, err := r.SearchAll(context.Background(), Query{Mode: Mode(7)})
	if err != nil {
		t.Fatalf("SearchAll = %v, want nil", err)
	}
	if len(srcErrs) != 1 || !srcErrs[0].Skipped || !errors.Is(srcErrs[0], ErrUnsupportedMode) {
		t.Fatalf("source errors = %v, want one ErrUnsupportedMode skip", srcErrs)
	}
}

// Every source skipped: nothing failed, so there is no error.
func TestSearchAllEverySourceSkipped(t *testing.T) {
	r, _ := newTestRegistry(t, Config{},
		&stubIndexer{id: "alpha", caps: searchCaps},
		&stubIndexer{id: "beta", caps: searchCaps},
	)

	results, srcErrs, err := r.SearchAll(context.Background(), Query{Mode: ModeLatest})
	if err != nil {
		t.Fatalf("SearchAll with every source skipped = %v, want nil: a skip never counts as a failure", err)
	}
	if len(results) != 0 {
		t.Errorf("results = %v, want none", titles(results))
	}
	if len(srcErrs) != 2 {
		t.Fatalf("source errors = %v, want one skip note per source", srcErrs)
	}
	for _, e := range srcErrs {
		if !e.Skipped {
			t.Errorf("%+v is not marked skipped", e)
		}
	}
}

// Skipped plus failed, with no successes: the sources that were actually
// queried all failed, so the call failed. The skip neither causes nor prevents
// that — it is simply not counted.
func TestSearchAllSkippedPlusFailed(t *testing.T) {
	r, _ := newTestRegistry(t, Config{},
		&stubIndexer{id: "skipme", caps: searchCaps},
		failSource("failme", errors.New("upstream 500")),
	)

	_, srcErrs, err := r.SearchAll(context.Background(), Query{Mode: ModeLatest})
	if !errors.Is(err, ErrAllSourcesFailed) {
		t.Fatalf("SearchAll = %v, want ErrAllSourcesFailed: every source that was queried failed", err)
	}
	if len(srcErrs) != 2 {
		t.Fatalf("source errors = %v, want the skip note and the failure", srcErrs)
	}
	if !srcErrs[0].Skipped || srcErrs[1].Skipped {
		t.Errorf("source errors = %v, want [skipped, failed] in selection order", srcErrs)
	}
}

// Skipped plus succeeded: a partial success, never an error.
func TestSearchAllSkippedPlusSucceeded(t *testing.T) {
	good := &stubIndexer{id: "good", caps: bothCaps, fn: func(_ context.Context, _ Query) ([]Result, error) {
		return []Result{hit("good", "1", "good row", 1)}, nil
	}}
	r, _ := newTestRegistry(t, Config{}, &stubIndexer{id: "skipme", caps: searchCaps}, good)

	results, srcErrs, err := r.SearchAll(context.Background(), Query{Mode: ModeLatest})
	if err != nil {
		t.Fatalf("SearchAll = %v, want nil", err)
	}
	if got := joined(titles(results)); got != "good row" {
		t.Errorf("results = %v, want the capable source's row", titles(results))
	}
	if len(srcErrs) != 1 || !srcErrs[0].Skipped {
		t.Errorf("source errors = %v, want a single skip note", srcErrs)
	}
}

// ---------------------------------------------------------------------------
// Cache and per-source minimum refresh interval (AGENT.md §6.13)
// ---------------------------------------------------------------------------

func TestSearchAllCacheServesRepeatedQuery(t *testing.T) {
	src := okSource("alpha", hit("alpha", "1", "alpha one", 6))
	r, clock := newTestRegistry(t, Config{CacheTTL: time.Minute, MinRefreshInterval: 10 * time.Second}, src)
	q := Query{Text: "same query"}

	first, _, err := r.SearchAll(context.Background(), q)
	if err != nil {
		t.Fatalf("first SearchAll: %v", err)
	}

	// Mashing R: repeated identical searches inside the TTL re-render from
	// cache instead of re-fetching, and they are neither skipped nor errors.
	for i := range 5 {
		clock.advance(time.Second)
		again, srcErrs, err := r.SearchAll(context.Background(), q)
		if err != nil || len(srcErrs) != 0 {
			t.Fatalf("repeat %d: err %v, source errors %v; want a clean cache hit", i, err, srcErrs)
		}
		if joined(titles(again)) != joined(titles(first)) {
			t.Errorf("repeat %d returned %v, want the cached %v", i, titles(again), titles(first))
		}
	}
	if got := src.calls.Load(); got != 1 {
		t.Errorf("the source was queried %d times for six identical searches, want 1", got)
	}

	// Past the TTL the cache no longer answers and the source is queried again.
	clock.advance(time.Minute)
	if _, _, err := r.SearchAll(context.Background(), q); err != nil {
		t.Fatalf("post-expiry SearchAll: %v", err)
	}
	if got := src.calls.Load(); got != 2 {
		t.Errorf("the source was queried %d times after the TTL expired, want 2", got)
	}
}

func TestSearchAllCacheKeyCoversTheWholeQuery(t *testing.T) {
	src := okSource("alpha", hit("alpha", "1", "alpha one", 6))
	r, clock := newTestRegistry(t, Config{CacheTTL: time.Hour, MinRefreshInterval: time.Second}, src)

	queries := []Query{
		{Mode: ModeSearch, Text: "one"},
		{Mode: ModeSearch, Text: "two"},
		{Mode: ModeLatest},
		{Mode: ModeSearch, Text: "one", Categories: []Category{CategoryData}},
		{Mode: ModeSearch, Text: "one", Categories: []Category{CategoryData, CategoryAudio}},
		{Mode: ModeSearch, Text: "one", MinSeeders: 5},
		{Mode: ModeSearch, Text: "one", Limit: 10},
		{Mode: ModeSearch, Text: "one", Offset: 10},
	}
	for i, q := range queries {
		clock.advance(time.Second) // clear the refresh floor between distinct queries
		if _, _, err := r.SearchAll(context.Background(), q); err != nil {
			t.Fatalf("query %d (%+v): %v", i, q, err)
		}
	}
	if got, want := int(src.calls.Load()), len(queries); got != want {
		t.Errorf("the source was queried %d times for %d distinct queries, want one fetch each", got, want)
	}

	// Categories are order- and duplicate-insensitive: the same set is the
	// same cache entry.
	clock.advance(time.Second)
	if _, _, err := r.SearchAll(context.Background(), Query{Text: "one", Categories: []Category{CategoryAudio, CategoryData, CategoryAudio}}); err != nil {
		t.Fatalf("reordered categories: %v", err)
	}
	if got, want := int(src.calls.Load()), len(queries); got != want {
		t.Errorf("the source was queried %d times, want %d: reordering the same category set must hit the same cache entry", got, want)
	}
}

func TestSearchAllMinimumRefreshIntervalSkipsTheSource(t *testing.T) {
	src := okSource("alpha", hit("alpha", "1", "alpha one", 6))
	r, clock := newTestRegistry(t, Config{CacheTTL: time.Hour, MinRefreshInterval: 30 * time.Second}, src)

	if _, _, err := r.SearchAll(context.Background(), Query{Text: "first"}); err != nil {
		t.Fatalf("first SearchAll: %v", err)
	}

	// A *different* query misses the cache, so only the per-source floor
	// stands between it and a second request.
	clock.advance(5 * time.Second)
	results, srcErrs, err := r.SearchAll(context.Background(), Query{Text: "second"})
	if err != nil {
		t.Fatalf("throttled SearchAll = %v, want nil: a throttled source is skipped, not failed", err)
	}
	if len(results) != 0 {
		t.Errorf("results = %v, want none: the only source was skipped", titles(results))
	}
	if len(srcErrs) != 1 || !srcErrs[0].Skipped || !errors.Is(srcErrs[0], ErrThrottled) {
		t.Fatalf("source errors = %v, want one ErrThrottled skip", srcErrs)
	}
	if got := src.calls.Load(); got != 1 {
		t.Errorf("the source was queried %d times, want 1: the floor was not enforced", got)
	}

	// Once the interval has elapsed the source is queried again.
	clock.advance(30 * time.Second)
	if _, srcErrs, err = r.SearchAll(context.Background(), Query{Text: "second"}); err != nil || len(srcErrs) != 0 {
		t.Fatalf("post-interval SearchAll = %v / %v, want a clean fetch", err, srcErrs)
	}
	if got := src.calls.Load(); got != 2 {
		t.Errorf("the source was queried %d times after the interval elapsed, want 2", got)
	}
}

func TestSearchAllCacheHitDoesNotConsumeTheRefreshFloor(t *testing.T) {
	// A cache hit makes no request, so it must not push the next allowed
	// fetch further out.
	src := okSource("alpha", hit("alpha", "1", "alpha one", 6))
	r, clock := newTestRegistry(t, Config{CacheTTL: time.Hour, MinRefreshInterval: 10 * time.Second}, src)
	q := Query{Text: "same"}

	if _, _, err := r.SearchAll(context.Background(), q); err != nil {
		t.Fatalf("first SearchAll: %v", err)
	}
	for range 3 {
		clock.advance(4 * time.Second) // 12s of cache hits, past the 10s floor
		if _, _, err := r.SearchAll(context.Background(), q); err != nil {
			t.Fatalf("cache hit: %v", err)
		}
	}
	_, srcErrs, err := r.SearchAll(context.Background(), Query{Text: "different"})
	if err != nil || len(srcErrs) != 0 {
		t.Fatalf("SearchAll = %v / %v, want a clean fetch: 12s of cache hits must not have reset the floor", err, srcErrs)
	}
	if got := src.calls.Load(); got != 2 {
		t.Errorf("the source was queried %d times, want 2", got)
	}
}

func TestSearchAllFailedFetchDoesNotPoisonTheCache(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	src := &stubIndexer{id: "alpha", caps: bothCaps, fn: func(_ context.Context, _ Query) ([]Result, error) {
		if fail.Load() {
			return nil, errors.New("upstream 500")
		}
		return []Result{hit("alpha", "1", "alpha one", 1)}, nil
	}}
	r, clock := newTestRegistry(t, Config{CacheTTL: time.Hour, MinRefreshInterval: time.Second}, src)

	if _, _, err := r.SearchAll(context.Background(), Query{Text: "x"}); !errors.Is(err, ErrAllSourcesFailed) {
		t.Fatalf("first SearchAll = %v, want ErrAllSourcesFailed", err)
	}
	fail.Store(false)
	clock.advance(time.Second)
	results, _, err := r.SearchAll(context.Background(), Query{Text: "x"})
	if err != nil {
		t.Fatalf("retry = %v, want nil: a failure must not be cached", err)
	}
	if got := joined(titles(results)); got != "alpha one" {
		t.Errorf("retry results = %v, want the recovered source's row", titles(results))
	}
}

func TestSearchAllCachedResultsAreIsolatedFromCallers(t *testing.T) {
	shared := map[string]string{"quality": "original"}
	src := &stubIndexer{id: "alpha", caps: bothCaps, fn: func(_ context.Context, _ Query) ([]Result, error) {
		return []Result{{IndexerID: "alpha", ID: "1", Title: "alpha one", Magnet: testMagnet, Extra: shared}}, nil
	}}
	r, _ := newTestRegistry(t, Config{CacheTTL: time.Hour}, src)
	q := Query{Text: "x"}

	first, _, err := r.SearchAll(context.Background(), q)
	if err != nil {
		t.Fatalf("first SearchAll: %v", err)
	}

	// The registry must not have written its own bookkeeping into the map the
	// adapter handed it.
	if len(shared) != 1 || shared["quality"] != "original" {
		t.Errorf("the adapter's own Extra map is now %v; the registry mutated it", shared)
	}

	first[0].Title = "vandalised"
	first[0].Extra["quality"] = "vandalised"

	second, _, err := r.SearchAll(context.Background(), q)
	if err != nil {
		t.Fatalf("second SearchAll: %v", err)
	}
	if second[0].Title != "alpha one" || second[0].Extra["quality"] != "original" {
		t.Errorf("cached result came back as %q/%v after the caller mutated the first copy; the cache is not isolated", second[0].Title, second[0].Extra)
	}
}

func TestSearchAllCachedResultsAreIsolatedFromTheAdapter(t *testing.T) {
	// An adapter is free to keep a reference to the slice and the maps it
	// returned. If the cache holds those same objects, whatever the adapter
	// does to them afterwards silently rewrites history.
	own := []Result{{IndexerID: "alpha", ID: "1", Title: "alpha one", Magnet: testMagnet, Extra: map[string]string{"quality": "original"}}}
	src := &stubIndexer{id: "alpha", caps: bothCaps, fn: func(_ context.Context, _ Query) ([]Result, error) {
		return own, nil
	}}
	r, _ := newTestRegistry(t, Config{CacheTTL: time.Hour}, src)
	q := Query{Text: "x"}

	if _, _, err := r.SearchAll(context.Background(), q); err != nil {
		t.Fatalf("first SearchAll: %v", err)
	}
	own[0].Title = "rewritten by the adapter"
	own[0].Extra["quality"] = "rewritten by the adapter"

	cached, _, err := r.SearchAll(context.Background(), q)
	if err != nil {
		t.Fatalf("second SearchAll: %v", err)
	}
	if cached[0].Title != "alpha one" || cached[0].Extra["quality"] != "original" {
		t.Errorf("cached result came back as %q/%v after the adapter mutated what it had returned; the cache holds the adapter's own objects", cached[0].Title, cached[0].Extra)
	}
}

func TestCachedResultsHandsOutPrivateCopies(t *testing.T) {
	// cachedResults is the single door out of the cache; whatever comes
	// through it must be the caller's alone, whether or not today's only
	// caller happens to copy again downstream.
	r := NewRegistry(Config{CacheTTL: time.Hour})
	key := newCacheKey("alpha", Query{Text: "x"})
	r.storeResults(key, []Result{{IndexerID: "alpha", ID: "1", Title: "alpha one", Extra: map[string]string{"quality": "original"}}})

	first, ok := r.cachedResults(key)
	if !ok {
		t.Fatal("cachedResults reported a miss for an entry just stored")
	}
	first[0].Title = "vandalised"
	first[0].Extra["quality"] = "vandalised"

	second, ok := r.cachedResults(key)
	if !ok {
		t.Fatal("cachedResults reported a miss on the second read")
	}
	if second[0].Title != "alpha one" || second[0].Extra["quality"] != "original" {
		t.Errorf("second read = %q/%v, want the stored values; cachedResults handed out the cache's own objects", second[0].Title, second[0].Extra)
	}
}

// ---------------------------------------------------------------------------
// Deduplication and ordering
// ---------------------------------------------------------------------------

func TestSearchAllDeduplicatesByInfoHash(t *testing.T) {
	const infoHash = "AABBCCDDEEFF00112233445566778899AABBCCDD"
	mk := func(id, title string, seeders int, hash string) *stubIndexer {
		return okSource(id, Result{
			IndexerID: id, ID: "1", Title: title, InfoHash: hash,
			Magnet: testMagnet, SizeBytes: 100, Seeders: seeders,
			Extra: map[string]string{"origin": id},
		})
	}
	r, _ := newTestRegistry(t, Config{},
		mk("alpha", "Example Dataset 2026", 5, infoHash),
		mk("beta", "example.dataset.2026", 42, strings.ToLower(infoHash)),
		mk("gamma", "wholly different title", 9, " "+infoHash+" "),
	)

	results, _, err := r.SearchAll(context.Background(), Query{Text: "x"})
	if err != nil {
		t.Fatalf("SearchAll: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %v, want a single merged row: all three share an infohash", titles(results))
	}
	got := results[0]
	if got.Seeders != 42 || got.IndexerID != "beta" {
		t.Errorf("surviving copy = %s with %d seeders, want beta's with 42 (highest seeders wins)", got.IndexerID, got.Seeders)
	}
	if want := "alpha,beta,gamma"; got.Extra[ExtraKeySources] != want {
		t.Errorf("%s = %q, want %q: every contributing source, in selection order", ExtraKeySources, got.Extra[ExtraKeySources], want)
	}
	if got.Extra["origin"] != "beta" {
		t.Errorf("surviving copy kept Extra %v, want the survivor's own adapter fields", got.Extra)
	}
}

func TestSearchAllDeduplicatesByNormalisedTitleAndSize(t *testing.T) {
	r, _ := newTestRegistry(t, Config{},
		okSource("alpha", Result{IndexerID: "alpha", ID: "1", Title: "Example  Dataset (2026)", Magnet: testMagnet, SizeBytes: 4096, Seeders: 3}),
		okSource("beta", Result{IndexerID: "beta", ID: "9", Title: "example_dataset-2026", Magnet: testMagnet, SizeBytes: 4096, Seeders: 8}),
		// Same title, different size: a different item.
		okSource("gamma", Result{IndexerID: "gamma", ID: "7", Title: "Example Dataset 2026", Magnet: testMagnet, SizeBytes: 8192, Seeders: 100}),
	)

	results, _, err := r.SearchAll(context.Background(), Query{Text: "x"})
	if err != nil {
		t.Fatalf("SearchAll: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %v, want two rows: the 4096-byte pair merges, the 8192-byte one does not", titles(results))
	}
	merged := results[1]
	if merged.SizeBytes != 4096 {
		merged = results[0]
	}
	if merged.Seeders != 8 || merged.IndexerID != "beta" {
		t.Errorf("merged row = %s with %d seeders, want beta's with 8", merged.IndexerID, merged.Seeders)
	}
	if want := "alpha,beta"; merged.Extra[ExtraKeySources] != want {
		t.Errorf("%s = %q, want %q", ExtraKeySources, merged.Extra[ExtraKeySources], want)
	}
}

func TestSearchAllDoesNotMergeUnidentifiableResults(t *testing.T) {
	// No infohash, no title worth normalising, no size: nothing to match on,
	// so these must stay separate rows rather than collapsing into one.
	blank := func(id string) *stubIndexer {
		return okSource(id, Result{IndexerID: id, Title: "  ***  ", Magnet: testMagnet})
	}
	r, _ := newTestRegistry(t, Config{}, blank("alpha"), blank("beta"))

	results, _, err := r.SearchAll(context.Background(), Query{Text: "x"})
	if err != nil {
		t.Fatalf("SearchAll: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d rows, want 2: results with nothing to match on must not be merged", len(results))
	}
	for _, res := range results {
		if strings.Contains(res.Extra[ExtraKeySources], ",") {
			t.Errorf("row from %s claims sources %q; it contributed alone", res.IndexerID, res.Extra[ExtraKeySources])
		}
	}
}

func TestSearchAllDeduplicatesWithinASingleSource(t *testing.T) {
	const infoHash = "0011223344556677889900112233445566778899"
	r, _ := newTestRegistry(t, Config{}, okSource("alpha",
		Result{IndexerID: "alpha", ID: "1", Title: "one", InfoHash: infoHash, Magnet: testMagnet, Seeders: 1},
		Result{IndexerID: "alpha", ID: "2", Title: "one again", InfoHash: infoHash, Magnet: testMagnet, Seeders: 11},
	))

	results, _, err := r.SearchAll(context.Background(), Query{Text: "x"})
	if err != nil {
		t.Fatalf("SearchAll: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %v, want one row", titles(results))
	}
	if results[0].Seeders != 11 {
		t.Errorf("surviving copy has %d seeders, want 11", results[0].Seeders)
	}
	if got := results[0].Extra[ExtraKeySources]; got != "alpha" {
		t.Errorf("%s = %q, want %q listed once", ExtraKeySources, got, "alpha")
	}
}

func TestSearchAllOrdering(t *testing.T) {
	old := time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
	mid := time.Date(2024, time.June, 1, 0, 0, 0, 0, time.UTC)
	recent := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)

	sources := []Indexer{
		okSource("alpha", Result{IndexerID: "alpha", ID: "1", Title: "low seeders, newest", Magnet: testMagnet, Seeders: 1, Published: recent, InfoHash: "a1"}),
		okSource("beta", Result{IndexerID: "beta", ID: "1", Title: "most seeders, oldest", Magnet: testMagnet, Seeders: 90, Published: old, InfoHash: "b1"}),
		okSource("gamma", Result{IndexerID: "gamma", ID: "1", Title: "middling", Magnet: testMagnet, Seeders: 50, Published: mid, InfoHash: "c1"}),
	}

	r, clock := newTestRegistry(t, Config{}, sources...)
	bySeeders, _, err := r.SearchAll(context.Background(), Query{Mode: ModeSearch, Text: "x"})
	if err != nil {
		t.Fatalf("ModeSearch: %v", err)
	}
	if got := joined(titles(bySeeders)); got != "most seeders, oldest,middling,low seeders, newest" {
		t.Errorf("ModeSearch order = %v, want seeders descending", titles(bySeeders))
	}

	clock.advance(time.Hour)
	for _, s := range sources {
		if !s.Caps().Latest {
			t.Fatalf("%s cannot serve ModeLatest", s.ID())
		}
	}
	byDate, _, err := r.SearchAll(context.Background(), Query{Mode: ModeLatest})
	if err != nil {
		t.Fatalf("ModeLatest: %v", err)
	}
	if got := joined(titles(byDate)); got != "low seeders, newest,middling,most seeders, oldest" {
		t.Errorf("ModeLatest order = %v, want published date descending", titles(byDate))
	}
}

func TestSearchAllOrderingIsDeterministic(t *testing.T) {
	// Identical seeders and dates across sources: the order must not depend
	// on which goroutine happened to finish first.
	mk := func(id string) *stubIndexer {
		return okSource(id,
			Result{IndexerID: id, ID: "2", Title: "shared title", InfoHash: id + "-2", Magnet: testMagnet, Seeders: 5},
			Result{IndexerID: id, ID: "1", Title: "another title", InfoHash: id + "-1", Magnet: testMagnet, Seeders: 5},
		)
	}
	var want string
	for i := range 25 {
		r, _ := newTestRegistry(t, Config{}, mk("alpha"), mk("beta"), mk("gamma"))
		results, _, err := r.SearchAll(context.Background(), Query{Text: "x"})
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		got := joined(sourceOf(results))
		if i == 0 {
			want = got
			continue
		}
		if got != want {
			t.Fatalf("run %d ordered results as %q, run 0 ordered them as %q; the fan-out order leaks into the output", i, got, want)
		}
	}
	if want != "alpha/1,beta/1,gamma/1,alpha/2,beta/2,gamma/2" {
		t.Errorf("tie-broken order = %q, want title then indexer id then result id", want)
	}
}

func TestSortResultsTieBreaks(t *testing.T) {
	early := time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		mode Mode
		in   []Result
		want []string // Result.ID, in the order expected
	}{
		{
			name: "latest breaks an equal date on seeders",
			mode: ModeLatest,
			in: []Result{
				{ID: "low", Published: late, Seeders: 1},
				{ID: "high", Published: late, Seeders: 9},
			},
			want: []string{"high", "low"},
		},
		{
			name: "search breaks equal seeders on date",
			mode: ModeSearch,
			in: []Result{
				{ID: "old", Seeders: 5, Published: early},
				{ID: "new", Seeders: 5, Published: late},
			},
			want: []string{"new", "old"},
		},
		{
			name: "identical rows break on title, then indexer, then id",
			mode: ModeSearch,
			in: []Result{
				{ID: "2", IndexerID: "beta", Title: "same"},
				{ID: "1", IndexerID: "beta", Title: "same"},
				{ID: "1", IndexerID: "alpha", Title: "same"},
				{ID: "1", IndexerID: "alpha", Title: "different"},
			},
			want: []string{"1", "1", "1", "2"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sortResults(tc.in, tc.mode)
			got := make([]string, len(tc.in))
			for i, r := range tc.in {
				got[i] = r.ID
			}
			if joined(got) != joined(tc.want) {
				t.Errorf("order = %v, want %v", got, tc.want)
			}
		})
	}

	// The last case above is only meaningful if the ordering really did
	// fall all the way through to the result id.
	last := []Result{{ID: "b", IndexerID: "alpha", Title: "same"}, {ID: "a", IndexerID: "alpha", Title: "same"}}
	sortResults(last, ModeSearch)
	if last[0].ID != "a" {
		t.Errorf("rows identical but for their id sorted as %s,%s; want a,b", last[0].ID, last[1].ID)
	}
}

func TestReserveFetchRejectsAnUnknownSource(t *testing.T) {
	// Unreachable through SearchAll, which only ever reserves for a source
	// it just resolved; it is here so that a future caller cannot silently
	// get a free pass past the refresh floor.
	r := NewRegistry(Config{})
	if r.reserveFetch("ghost") {
		t.Error("reserveFetch(ghost) = true for an unregistered source, want false")
	}
}

func sourceOf(rs []Result) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.IndexerID + "/" + r.ID
	}
	return out
}

// ---------------------------------------------------------------------------
// SourceError
// ---------------------------------------------------------------------------

func TestSourceError(t *testing.T) {
	inner := errors.New("connection refused")

	failed := SourceError{IndexerID: "alpha", Err: inner}
	if !strings.Contains(failed.Error(), "alpha") || !strings.Contains(failed.Error(), "connection refused") {
		t.Errorf("failed.Error() = %q, want it to name the source and the cause", failed.Error())
	}
	if !strings.Contains(failed.Error(), "failed") {
		t.Errorf("failed.Error() = %q, want it to say the source failed", failed.Error())
	}
	if !errors.Is(failed, inner) {
		t.Error("errors.Is(failed, inner) = false, want the wrapped cause to be reachable")
	}
	if got := errors.Unwrap(failed); !errors.Is(got, inner) {
		t.Errorf("errors.Unwrap(failed) = %v, want the inner error", got)
	}

	skipped := SourceError{IndexerID: "beta", Skipped: true, Err: ErrUnsupportedMode}
	if !strings.Contains(skipped.Error(), "skipped") {
		t.Errorf("skipped.Error() = %q, want it to say the source was skipped, not that it failed", skipped.Error())
	}
}
