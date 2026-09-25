package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/engine/fake"
	"github.com/kdta91/tortui/internal/indexer"
	indexerfake "github.com/kdta91/tortui/internal/indexer/fake"
)

// fakeSourceManager is an in-memory SourceManager double: it never touches
// disk or the network (AGENT.md §6.7's unit-test rule applies here too),
// and every method can be scripted to fail so a test drives the settings
// screen's error paths without a real composition root. Every SaveSources
// call runs inside its own tea.Cmd goroutine (AGENT.md §6.1) while View()
// keeps reading Sources() on the bubbletea event-loop goroutine, so — the
// same as any real SourceManager — this double must be safe for
// concurrent use; mu is what makes it so.
type fakeSourceManager struct {
	mu      sync.Mutex
	sources []config.Indexer

	saveErr    error
	testErr    error
	importErr  error
	importID   string
	reloadErr  error
	saveCalls  int
	testCalls  int
	reloadCall int

	// testDelay, when set, makes TestSource block until either it elapses
	// (returning testErr, the "reachable" case when testErr is nil) or ctx
	// is done first (returning ctx.Err() and recording ctxCancelled) — this
	// is what TestListTestKeyRefusesSecondProbeWhileInFlight and
	// TestListTestKeyCancellable (T-081) need an in-flight probe for.
	testDelay    time.Duration
	ctxCancelled bool

	// registrySync, when set, is called by SaveSources with the newly
	// saved set — standing in for "reloads the registry live" (the real
	// contract's own job), so a test can wire it to update a matching
	// Searcher double and prove the TUI's own refresh (root.go's
	// refreshSearchSources) actually reads it back.
	registrySync func([]config.Indexer)
}

func (f *fakeSourceManager) Sources() []config.Indexer {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]config.Indexer(nil), f.sources...)
}

func (f *fakeSourceManager) SaveSources(sources []config.Indexer) error {
	f.mu.Lock()

	f.saveCalls++
	if f.saveErr != nil {
		f.mu.Unlock()
		return f.saveErr
	}

	f.sources = append([]config.Indexer(nil), sources...)
	sync := f.registrySync

	f.mu.Unlock()

	if sync != nil {
		sync(append([]config.Indexer(nil), sources...))
	}

	return nil
}

func (f *fakeSourceManager) TestSource(ctx context.Context, _ config.Indexer) error {
	f.mu.Lock()
	f.testCalls++
	delay := f.testDelay
	err := f.testErr
	f.mu.Unlock()

	if delay <= 0 {
		return err
	}

	select {
	case <-time.After(delay):
		return err
	case <-ctx.Done():
		f.mu.Lock()
		f.ctxCancelled = true
		f.mu.Unlock()

		return ctx.Err()
	}
}

// sawCtxCancelled reports whether some TestSource call so far observed its
// ctx done before testDelay elapsed.
func (f *fakeSourceManager) sawCtxCancelled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.ctxCancelled
}

// waitCtxCancelled polls sawCtxCancelled up to timeout — a bounded wait for
// the async TestSource goroutine to observe cancellation, not a fixed sleep.
func (f *fakeSourceManager) waitCtxCancelled(t *testing.T, timeout time.Duration) bool {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if f.sawCtxCancelled() {
			return true
		}

		time.Sleep(5 * time.Millisecond)
	}

	return f.sawCtxCancelled()
}

func (f *fakeSourceManager) ImportDefinition(_ context.Context, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.importErr != nil {
		return "", f.importErr
	}

	return f.importID, nil
}

func (f *fakeSourceManager) ReloadDefinitions() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.reloadCall++

	return f.reloadErr
}

// saveCallCount and savedSources read the double's state under the same
// lock its methods use, so a test checking the outcome after a
// waitForOutput never races the goroutine that just called SaveSources.
func (f *fakeSourceManager) saveCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.saveCalls
}

func (f *fakeSourceManager) testCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.testCalls
}

func (f *fakeSourceManager) savedSources() []config.Indexer {
	return f.Sources()
}

func (f *fakeSourceManager) reloadCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.reloadCall
}

// dynamicSearcher is a Searcher test double whose Enabled() result can
// change after construction — unlike search_test.go's stubSearcher, which
// is fixed for the test's lifetime. It exists for exactly one thing: proving
// that a settings-screen change is visible on the search screen without a
// restart (T-080 review finding), which needs a Searcher a fakeSourceManager
// can actually update via registrySync.
type dynamicSearcher struct {
	mu      sync.Mutex
	enabled []indexer.Indexer
}

func (s *dynamicSearcher) Enabled() []indexer.Indexer {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]indexer.Indexer(nil), s.enabled...)
}

func (s *dynamicSearcher) Get(id string) (indexer.Indexer, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, ix := range s.enabled {
		if ix.ID() == id {
			return ix, true
		}
	}

	return nil, false
}

func (s *dynamicSearcher) SearchAll(context.Context, indexer.Query, ...string) ([]indexer.Result, []indexer.SourceError, error) {
	return nil, nil, nil
}

func (s *dynamicSearcher) setEnabled(list []indexer.Indexer) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.enabled = list
}

func newSettingsTestModel(t *testing.T, sm SourceManager) *teatest.TestModel {
	t.Helper()

	m := New(fake.New(), testTheme(), WithSourceManager(sm))
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	tm.Send(keyRune("5")) // jump to settings

	return tm
}

// --- pure helpers ----------------------------------------------------------

func TestSlugifyLowercasesAndDashesPunctuation(t *testing.T) {
	cases := map[string]string{
		"My Great Source!":  "my-great-source",
		"  spaced  out  ":   "spaced-out",
		"already-slug":      "already-slug",
		"###":               "source",
		"":                  "source",
		"Über Cool":         "ber-cool",
		"Multiple---Dashes": "multiple-dashes",
	}

	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitTorznabURLExtractsEmbeddedAPIKey(t *testing.T) {
	stripped, key, ok := splitTorznabURL("https://example.org/api?t=search&apikey=SECRET123")
	if !ok {
		t.Fatal("expected ok=true for a URL with an embedded apikey")
	}
	if key != "SECRET123" {
		t.Errorf("key = %q, want SECRET123", key)
	}
	if strings.Contains(stripped, "apikey") {
		t.Errorf("stripped URL %q still contains apikey", stripped)
	}
}

func TestSplitTorznabURLLeavesPlainURLUnchanged(t *testing.T) {
	_, _, ok := splitTorznabURL("https://example.org/api?t=search")
	if ok {
		t.Fatal("expected ok=false for a URL with no apikey parameter")
	}
}

func TestMaskSecretHidesUnlessRevealed(t *testing.T) {
	if got := maskSecret("hunter2", false); got == "hunter2" {
		t.Fatal("expected the raw secret not to appear when reveal is false")
	}
	if got := maskSecret("hunter2", true); got != "hunter2" {
		t.Errorf("revealed secret = %q, want hunter2", got)
	}
	if got := maskSecret("", false); got != "" {
		t.Errorf("empty secret should render empty even unmasked, got %q", got)
	}
}

// --- end-to-end (teatest) ---------------------------------------------------

// TestAddTorznabSourceEndToEnd drives the full add flow for a torznab
// source: open the form, type into name/URL/API key, save, and see it in
// the list.
func TestAddTorznabSourceEndToEnd(t *testing.T) {
	sm := &fakeSourceManager{}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "No sources configured")

	tm.Send(keyRune("a"))
	waitForOutput(t, tm, "Add source")

	tm.Send(keyRune("My Tracker"))
	tm.Send(tea.KeyMsg{Type: tea.KeyTab}) // -> type (leave as torznab)
	tm.Send(tea.KeyMsg{Type: tea.KeyTab}) // -> url
	tm.Send(keyRune("https://example.org/feed"))
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlS})

	waitForPredicate(t, func() bool { return sm.saveCallCount() > 0 })

	if sm.saveCallCount() == 0 {
		t.Fatal("expected SaveSources to have been called")
	}
	if len(sm.savedSources()) != 1 || sm.savedSources()[0].Type != "torznab" || sm.savedSources()[0].ID != "my-tracker" {
		t.Fatalf("unexpected saved sources: %#v", sm.savedSources())
	}
}

// TestAddScraperSourceEndToEnd drives the add flow for a scraper source,
// which needs a Definition file and shows the extra fields the torznab form
// doesn't.
func TestAddScraperSourceEndToEnd(t *testing.T) {
	sm := &fakeSourceManager{}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "No sources configured")

	tm.Send(keyRune("a"))
	waitForOutput(t, tm, "Add source")

	tm.Send(keyRune("Scrape Site"))
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	tm.Send(tea.KeyMsg{Type: tea.KeyLeft}) // torznab -> scraper
	waitForOutput(t, tm, "Definition file")

	tm.Send(tea.KeyMsg{Type: tea.KeyTab}) // -> url
	tm.Send(keyRune("https://example.org/scrape"))
	tm.Send(tea.KeyMsg{Type: tea.KeyTab}) // -> apikey
	tm.Send(tea.KeyMsg{Type: tea.KeyTab}) // -> cookie
	tm.Send(tea.KeyMsg{Type: tea.KeyTab}) // -> definition
	tm.Send(keyRune("scrape-site.yml"))
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlS})

	waitForPredicate(t, func() bool { return sm.saveCallCount() > 0 })

	if len(sm.savedSources()) != 1 || sm.savedSources()[0].Type != "scraper" || sm.savedSources()[0].Definition != "scrape-site.yml" {
		t.Fatalf("unexpected saved sources: %#v", sm.savedSources())
	}
}

// TestPasteURLWithEmbeddedKeySplitsAutomatically simulates a bracketed
// paste — one KeyRunes event carrying the whole string — landing on the URL
// field with an apikey query parameter already in it.
func TestPasteURLWithEmbeddedKeySplitsAutomatically(t *testing.T) {
	sm := &fakeSourceManager{}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "No sources configured")

	tm.Send(keyRune("a"))
	waitForOutput(t, tm, "Add source")

	tm.Send(keyRune("Feed"))
	tm.Send(tea.KeyMsg{Type: tea.KeyTab}) // -> type
	tm.Send(tea.KeyMsg{Type: tea.KeyTab}) // -> url
	tm.Send(keyRune("https://example.org/api?t=search&apikey=ABC123"))
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlR}) // reveal, so the split key is checkable on screen

	waitForOutput(t, tm, "ABC123")

	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}

	final := tm.FinalModel(t, teatest.WithFinalTimeout(3*time.Second)).(Model)
	if strings.Contains(final.settings.form.sourceURL, "apikey") {
		t.Errorf("URL field %q still carries the apikey query parameter", final.settings.form.sourceURL)
	}
	if final.settings.form.apiKey != "ABC123" {
		t.Errorf("API key field = %q, want ABC123", final.settings.form.apiKey)
	}
}

// TestDuplicateIDIsRejected proves a save whose derived id collides with an
// existing source is refused with the form left open and the error shown,
// rather than silently overwriting or renaming.
func TestDuplicateIDIsRejected(t *testing.T) {
	sm := &fakeSourceManager{sources: []config.Indexer{
		{ID: "my-tracker", Name: "My Tracker", Type: "torznab", URL: "https://example.org/a", Enabled: true},
	}}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "My Tracker")

	tm.Send(keyRune("a"))
	waitForOutput(t, tm, "Add source")

	tm.Send(keyRune("My Tracker")) // same name -> same slug -> same id
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	tm.Send(keyRune("https://example.org/b"))
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlS})

	waitForOutput(t, tm, "already used by another source")

	if len(sm.savedSources()) != 1 {
		t.Fatalf("expected the duplicate save to be rejected, got %d sources", len(sm.savedSources()))
	}
}

// TestEditSourceUpdatesInPlace opens an existing source, changes its URL,
// and saves — the same id, updated fields.
func TestEditSourceUpdatesInPlace(t *testing.T) {
	sm := &fakeSourceManager{sources: []config.Indexer{
		{ID: "my-tracker", Name: "My Tracker", Type: "torznab", URL: "https://example.org/old", Enabled: true},
	}}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "My Tracker")

	tm.Send(keyRune("e"))
	waitForOutput(t, tm, "Edit source")

	// Move to the URL field and append text (simplest edit: append, since
	// this test only proves the edit path updates the existing id in
	// place rather than creating a second row).
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	tm.Send(keyRune("2"))
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlS})

	waitForPredicate(t, func() bool { return sm.saveCallCount() > 0 })

	if len(sm.savedSources()) != 1 {
		t.Fatalf("expected exactly one source after edit, got %d", len(sm.savedSources()))
	}
	if sm.savedSources()[0].URL != "https://example.org/old2" {
		t.Errorf("URL = %q, want the old URL with 2 appended", sm.savedSources()[0].URL)
	}
}

// TestSpaceTogglesEnabled proves the list's space key flips Enabled and
// saves immediately, with no form involved.
func TestSpaceTogglesEnabled(t *testing.T) {
	sm := &fakeSourceManager{sources: []config.Indexer{
		{ID: "my-tracker", Name: "My Tracker", Type: "torznab", URL: "https://example.org/a", Enabled: true},
	}}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "My Tracker")

	tm.Send(tea.KeyMsg{Type: tea.KeySpace})

	waitForOutput(t, tm, "off")

	if len(sm.savedSources()) != 1 || sm.savedSources()[0].Enabled {
		t.Fatalf("expected the source to be disabled, got %#v", sm.savedSources())
	}
}

// TestRemoveWithConfirm proves `x` opens a confirmation and only removes
// once Remove is confirmed.
func TestRemoveWithConfirm(t *testing.T) {
	sm := &fakeSourceManager{sources: []config.Indexer{
		{ID: "my-tracker", Name: "My Tracker", Type: "torznab", URL: "https://example.org/a", Enabled: true},
	}}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "My Tracker")

	tm.Send(keyRune("x"))
	waitForOutput(t, tm, "Remove source?")

	// Default highlight is Cancel; move up to Remove and confirm.
	tm.Send(tea.KeyMsg{Type: tea.KeyUp})
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	waitForOutput(t, tm, "No sources configured")

	if len(sm.savedSources()) != 0 {
		t.Fatalf("expected the source to be removed, got %#v", sm.savedSources())
	}
}

// TestRemoveCancelKeepsSource proves cancelling the confirmation leaves the
// source untouched.
func TestRemoveCancelKeepsSource(t *testing.T) {
	sm := &fakeSourceManager{sources: []config.Indexer{
		{ID: "my-tracker", Name: "My Tracker", Type: "torznab", URL: "https://example.org/a", Enabled: true},
	}}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "My Tracker")

	tm.Send(keyRune("x"))
	waitForOutput(t, tm, "Remove source?")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter}) // default is Cancel

	waitForOutput(t, tm, "My Tracker")

	if len(sm.savedSources()) != 1 {
		t.Fatalf("expected the source to still be present, got %#v", sm.savedSources())
	}
}

// TestCancelDirtyFormAsksToConfirm proves esc on a form with unsaved edits
// opens a discard confirmation rather than closing immediately, and that
// confirming it discards the form with no save.
func TestCancelDirtyFormAsksToConfirm(t *testing.T) {
	sm := &fakeSourceManager{}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "No sources configured")

	tm.Send(keyRune("a"))
	waitForOutput(t, tm, "Add source")

	tm.Send(keyRune("x")) // dirties the Name field
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})

	waitForOutput(t, tm, "Discard changes?")

	tm.Send(keyRune("y"))

	waitForOutput(t, tm, "No sources configured")

	if sm.saveCallCount() != 0 {
		t.Fatalf("expected no save to have happened, got %d calls", sm.saveCallCount())
	}
}

// TestDiscardConfirmNoKeepsTheFormAndItsContent drives Model.Update
// directly (not through teatest) so it can inspect intermediate state a
// terminal-diffing renderer may not re-draw: declining the discard
// confirmation ("n"/esc) must reopen the exact same form, dirty content
// intact, rather than losing it.
func TestDiscardConfirmNoKeepsTheFormAndItsContent(t *testing.T) {
	sm := &fakeSourceManager{}
	m := New(fake.New(), testTheme(), WithSourceManager(sm))

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	updated, _ = m.Update(keyRune("5"))
	m = updated.(Model)
	updated, _ = m.Update(keyRune("a"))
	m = updated.(Model)
	updated, _ = m.Update(keyRune("x"))
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.settings.form == nil || !m.settings.form.confirmDiscard {
		t.Fatalf("expected the discard confirmation to be open, got %+v", m.settings.form)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.settings.form == nil {
		t.Fatal("expected the form to still be open after declining discard")
	}
	if m.settings.form.confirmDiscard {
		t.Fatal("expected the discard confirmation to have closed")
	}
	if m.settings.form.name != "x" {
		t.Fatalf("expected the dirty content to survive, name = %q", m.settings.form.name)
	}
}

// TestCancelCleanFormClosesImmediately proves esc on a form nobody typed
// into closes with no confirmation.
func TestCancelCleanFormClosesImmediately(t *testing.T) {
	sm := &fakeSourceManager{}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "No sources configured")

	tm.Send(keyRune("a"))
	waitForOutput(t, tm, "Add source")

	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})

	waitForOutput(t, tm, "No sources configured")
}

// TestFormTestBeforeSaveShowsFailure proves ctrl+t runs a probe against the
// draft form (not yet saved) and shows its outcome inline.
func TestFormTestBeforeSaveShowsFailure(t *testing.T) {
	sm := &fakeSourceManager{testErr: errors.New("connection refused")}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "No sources configured")

	tm.Send(keyRune("a"))
	waitForOutput(t, tm, "Add source")

	tm.Send(keyRune("My Tracker"))
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	tm.Send(keyRune("https://example.org/feed"))
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlT})

	waitForOutput(t, tm, "test failed: connection refused")

	if sm.saveCallCount() != 0 {
		t.Fatal("expected ctrl+t not to save")
	}
	if sm.testCallCount() == 0 {
		t.Fatal("expected TestSource to have been called")
	}
}

// TestListTestKeyProbesTheSelectedSource proves `t` on the list runs a
// probe against the already-saved selected source and reports the outcome
// via the status bar.
func TestListTestKeyProbesTheSelectedSource(t *testing.T) {
	sm := &fakeSourceManager{sources: []config.Indexer{
		{ID: "my-tracker", Name: "My Tracker", Type: "torznab", URL: "https://example.org/a", Enabled: true},
	}}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "My Tracker")

	tm.Send(keyRune("t"))

	// "testing My Tracker…" shows first and stays queued for
	// components.DefaultTransientTimeout (4s) before "My Tracker: reachable"
	// takes its place, so this one wait needs a longer budget than the
	// package's usual 3s helper.
	teatest.WaitFor(
		t, tm.Output(),
		func(bts []byte) bool { return strings.Contains(string(bts), "My Tracker: reachable") },
		teatest.WithCheckInterval(10*time.Millisecond),
		teatest.WithDuration(6*time.Second),
	)

	if sm.testCallCount() == 0 {
		t.Fatal("expected TestSource to have been called")
	}
}

// TestListTestKeyClassifiesOutcomes proves T-081's acceptance: a probe
// error is reported as one of the distinct outcomes (timeout, auth failed,
// parse failed), not just a generic failure, with the underlying error
// still available afterwards via the `d` detail panel.
func TestListTestKeyClassifiesOutcomes(t *testing.T) {
	cases := []struct {
		name    string
		testErr error
		want    string
	}{
		{name: "timeout", testErr: context.DeadlineExceeded, want: "My Tracker: timeout"},
		{name: "transport timeout", testErr: timeoutErr{}, want: "My Tracker: timeout"},
		{name: "auth failed", testErr: authFailureErr{}, want: "My Tracker: auth failed"},
		{name: "parse failed", testErr: parseFailureErr{}, want: "My Tracker: parse failed"},
		{name: "unreachable", testErr: errors.New("connection refused"), want: "My Tracker: unreachable"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sm := &fakeSourceManager{
				sources: []config.Indexer{
					{ID: "my-tracker", Name: "My Tracker", Type: "torznab", URL: "https://example.org/a", Enabled: true},
				},
				testErr: tc.testErr,
			}
			tm := newSettingsTestModel(t, sm)

			waitForOutput(t, tm, "My Tracker")

			tm.Send(keyRune("t"))
			teatest.WaitFor(
				t, tm.Output(),
				func(bts []byte) bool { return strings.Contains(string(bts), tc.want) },
				teatest.WithCheckInterval(10*time.Millisecond),
				teatest.WithDuration(6*time.Second),
			)

			// The underlying error stays available in the `d` detail panel
			// even after the transient status bar message would have
			// cleared on its own.
			tm.Send(keyRune("d"))
			waitForOutput(t, tm, tc.testErr.Error())
		})
	}
}

// authFailureErr and parseFailureErr are minimal test doubles implementing
// settings.go's duck-typed probeAuthFailure/probeParseFailure marker
// interfaces — this package cannot import a concrete adapter's own error
// type (AGENT.md §4), so this is exactly the shape any real adapter error
// would need to implement for its own failures to classify correctly.
type authFailureErr struct{}

func (authFailureErr) Error() string    { return "bad credentials" }
func (authFailureErr) AuthFailed() bool { return true }

type parseFailureErr struct{}

func (parseFailureErr) Error() string     { return "malformed response" }
func (parseFailureErr) ParseFailed() bool { return true }

// timeoutErr is a minimal test double for the standard net.Error-family
// Timeout() bool shape (net.OpError, url.Error, ...) — not
// context.DeadlineExceeded itself, proving classifyProbeError's second,
// duck-typed timeout path.
type timeoutErr struct{}

func (timeoutErr) Error() string { return "dial tcp: i/o timeout" }
func (timeoutErr) Timeout() bool { return true }

// TestListTestKeyRefusesSecondProbeWhileInFlight proves a second `t` while
// one is already running is refused rather than racing it (DEC-115's same
// discipline, applied here). The assertion is functional (testCallCount),
// not text-matched: the status bar serialises transient messages one at a
// time (components.StatusBar.Push queues behind whatever is still showing),
// so the refusal notice can take components.DefaultTransientTimeout (4s) to
// actually render — testCalls, by contrast, increments synchronously at
// TestSource's own entry, well before the fake's testDelay elapses, so a
// short bounded poll proves the refusal without waiting on that queue.
func TestListTestKeyRefusesSecondProbeWhileInFlight(t *testing.T) {
	sm := &fakeSourceManager{
		sources: []config.Indexer{
			{ID: "my-tracker", Name: "My Tracker", Type: "torznab", URL: "https://example.org/a", Enabled: true},
		},
		testDelay: 300 * time.Millisecond,
	}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "My Tracker")

	tm.Send(keyRune("t"))
	waitForOutput(t, tm, "testing My Tracker")

	tm.Send(keyRune("t"))

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && sm.testCallCount() < 2 {
		time.Sleep(10 * time.Millisecond)
	}

	if got := sm.testCallCount(); got != 1 {
		t.Fatalf("testCallCount = %d, want 1 (second t refused)", got)
	}
}

// TestListTestKeyCancellable proves esc cancels an in-flight probe (T-081):
// its context is actually cancelled (not just abandoned in the UI), and a
// fresh `t` right after is not refused as "already running". See
// TestListTestKeyRefusesSecondProbeWhileInFlight's doc comment for why this
// checks testCallCount rather than waiting on the status bar's own queued
// text.
func TestListTestKeyCancellable(t *testing.T) {
	sm := &fakeSourceManager{
		sources: []config.Indexer{
			{ID: "my-tracker", Name: "My Tracker", Type: "torznab", URL: "https://example.org/a", Enabled: true},
		},
		testDelay: 2 * time.Second,
	}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "My Tracker")

	tm.Send(keyRune("t"))
	waitForOutput(t, tm, "testing My Tracker")

	if got := sm.testCallCount(); got != 1 {
		t.Fatalf("testCallCount = %d, want 1 before cancelling", got)
	}

	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})

	if !sm.waitCtxCancelled(t, time.Second) {
		t.Fatal("expected the probe's context to be cancelled")
	}

	// A fresh `t` right after is not refused: a second TestSource call
	// starts immediately rather than being blocked by leftover in-flight
	// state.
	tm.Send(keyRune("t"))

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && sm.testCallCount() < 2 {
		time.Sleep(10 * time.Millisecond)
	}

	if got := sm.testCallCount(); got != 2 {
		t.Fatalf("testCallCount = %d, want 2 (esc allowed a fresh probe)", got)
	}
}

// TestSourceTestDetailBeforeAnyTestShowsHint proves `d` on a source that has
// never been tested reports a hint instead of opening an empty panel.
func TestSourceTestDetailBeforeAnyTestShowsHint(t *testing.T) {
	sm := &fakeSourceManager{sources: []config.Indexer{
		{ID: "my-tracker", Name: "My Tracker", Type: "torznab", URL: "https://example.org/a", Enabled: true},
	}}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "My Tracker")

	tm.Send(keyRune("d"))
	waitForOutput(t, tm, "no test result for this source yet")
}

// TestReloadDefinitionsKey proves `r` re-reads scraper definitions from disk
// (T-023) and reports the outcome.
func TestReloadDefinitionsKey(t *testing.T) {
	sm := &fakeSourceManager{}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "No sources configured")

	tm.Send(keyRune("r"))
	waitForOutput(t, tm, "definitions reloaded")

	if sm.reloadCallCount() == 0 {
		t.Fatal("expected ReloadDefinitions to have been called")
	}
}

// TestImportDefinitionPrefillsForm proves the scraper form's import field
// (enter, once it has focus) imports a definition and pre-fills
// Definition/ID/Name from it, alongside manual entry (T-080: "the add form
// offers 'import a definition' alongside manual entry").
func TestImportDefinitionPrefillsForm(t *testing.T) {
	sm := &fakeSourceManager{importID: "imported-source"}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "No sources configured")

	tm.Send(keyRune("a"))
	waitForOutput(t, tm, "Add source")

	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	tm.Send(tea.KeyMsg{Type: tea.KeyLeft}) // torznab -> scraper
	waitForOutput(t, tm, "Import from")

	tm.Send(tea.KeyMsg{Type: tea.KeyTab}) // -> url
	tm.Send(tea.KeyMsg{Type: tea.KeyTab}) // -> apikey
	tm.Send(tea.KeyMsg{Type: tea.KeyTab}) // -> cookie
	tm.Send(tea.KeyMsg{Type: tea.KeyTab}) // -> definition
	tm.Send(tea.KeyMsg{Type: tea.KeyTab}) // -> import

	tm.Send(keyRune("https://example.org/def.yml"))
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// The form's own URL field is still blank in this test, so live
	// validation (T-080 review finding) takes priority over the import's
	// "imported: ..." info line — the import's actual effect is checked
	// below via FinalModel instead.
	waitForOutput(t, tm, "imported-source.yml")

	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}

	final := tm.FinalModel(t, teatest.WithFinalTimeout(3*time.Second)).(Model)
	if final.settings.form == nil {
		t.Fatal("expected the form to still be open after a successful import")
	}
	if final.settings.form.definition != "imported-source.yml" {
		t.Errorf("definition = %q, want imported-source.yml", final.settings.form.definition)
	}
	if final.settings.form.idOverride != "imported-source" {
		t.Errorf("idOverride = %q, want imported-source", final.settings.form.idOverride)
	}
}

// TestSearchScreenEmptyStateJumpsToAddForm proves the search screen's own
// empty-state prompt (T-080 acceptance) — no sources configured at all —
// names the way out and "a" jumps straight into the settings add form.
func TestSearchScreenEmptyStateJumpsToAddForm(t *testing.T) {
	sm := &fakeSourceManager{}
	m := New(fake.New(), testTheme(), WithSourceManager(sm))
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	waitForOutput(t, tm, "No sources configured. Press 'a' to add one.")

	tm.Send(keyRune("a"))

	waitForOutput(t, tm, "Add source")
}

// TestDisablingLastSourceUpdatesSearchScreenLive proves the search screen's
// enabled-source list is not a permanent startup snapshot: disabling the
// only configured source from settings must make the search screen's own
// empty-state prompt appear without a restart (T-080 review finding —
// "reloads the registry live" was previously only true on disk, never on
// the search screen). fakeSourceManager.registrySync stands in for a real
// SourceManager's own registry re-sync; dynamicSearcher is what lets this
// test observe the TUI-side half (refreshSearchSources) actually reading
// it back.
func TestDisablingLastSourceUpdatesSearchScreenLive(t *testing.T) {
	ix := indexerfake.New("my-tracker", "My Tracker", indexer.Caps{Search: true, Latest: true}, nil)

	searcher := &dynamicSearcher{}
	searcher.setEnabled([]indexer.Indexer{ix})

	sm := &fakeSourceManager{sources: []config.Indexer{
		{ID: "my-tracker", Name: "My Tracker", Type: "torznab", URL: "https://example.org/a", Enabled: true},
	}}
	sm.registrySync = func(all []config.Indexer) {
		var live []indexer.Indexer
		for _, s := range all {
			if s.ID == "my-tracker" && s.Enabled {
				live = append(live, ix)
			}
		}
		searcher.setEnabled(live)
	}

	m := New(fake.New(), testTheme(), WithSourceManager(sm), WithSearcher(searcher))
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	waitForOutput(t, tm, "Query:") // starts on the search screen
	tm.Send(keyRune("5"))          // -> settings
	waitForOutput(t, tm, "My Tracker")

	tm.Send(tea.KeyMsg{Type: tea.KeySpace}) // disable the only source
	waitForPredicate(t, func() bool { return sm.saveCallCount() > 0 })

	tm.Send(keyRune("1")) // -> search
	waitForOutput(t, tm, "No sources configured. Press 'a' to add one.")
}

// TestFormArrowKeysMoveBetweenFields proves up/down move the form's field
// cursor exactly like tab/shift-tab (T-080 review finding: the form had no
// arrow navigation at all). Driven directly through Model.Update, the same
// style TestDiscardConfirmNoKeepsTheFormAndItsContent uses, since the
// cursor position isn't otherwise observable from rendered text alone.
func TestFormArrowKeysMoveBetweenFields(t *testing.T) {
	sm := &fakeSourceManager{}
	m := New(fake.New(), testTheme(), WithSourceManager(sm))

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	updated, _ = m.Update(keyRune("5"))
	m = updated.(Model)
	updated, _ = m.Update(keyRune("a"))
	m = updated.(Model)

	if m.settings.form.cursor != 0 {
		t.Fatalf("expected a fresh form to start at field 0, got %d", m.settings.form.cursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.settings.form.cursor != 1 {
		t.Fatalf("down: cursor = %d, want 1", m.settings.form.cursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(Model)
	if m.settings.form.cursor != 2 {
		t.Fatalf("down: cursor = %d, want 2", m.settings.form.cursor)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(Model)
	if m.settings.form.cursor != 1 {
		t.Fatalf("up: cursor = %d, want 1", m.settings.form.cursor)
	}
}

// TestFormLiveValidationAppearsAndClearsAsYouType proves required-field,
// URL, and duplicate-id hints show up and disappear as the user types, with
// no need to press save or test first (T-080 review finding).
func TestFormLiveValidationAppearsAndClearsAsYouType(t *testing.T) {
	sm := &fakeSourceManager{sources: []config.Indexer{
		{ID: "my-tracker", Name: "My Tracker", Type: "torznab", URL: "https://example.org/a", Enabled: true},
	}}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "My Tracker")

	tm.Send(keyRune("a"))

	// Nothing typed yet: name and URL are both required. Checked together
	// (waitForAllOutput, search_test.go) rather than as two separate waits
	// — both lines land in the very same render, so a second, independent
	// wait with nothing sent in between would look for bytes the first
	// wait already drained (the lesson from this task's own
	// TestCancelDirtyFormAsksToConfirm fix).
	waitForAllOutput(t, tm, "Add source", "name is required")

	tm.Send(keyRune("My Tracker")) // same name as the existing source
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	tm.Send(keyRune("not-a-url"))

	waitForOutput(t, tm, "URL must start with http:// or https://")

	// Fix the URL; the id (slugified from "My Tracker") still collides
	// with the existing source, so that hint should take its place.
	for range "not-a-url" {
		tm.Send(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	tm.Send(keyRune("https://example.org/b"))

	waitForOutput(t, tm, `id "my-tracker" is already used by another source`)

	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}

	final := tm.FinalModel(t, teatest.WithFinalTimeout(3*time.Second)).(Model)
	if len(final.settings.form.liveIssues(final.sourceRows())) == 0 {
		t.Fatal("expected the duplicate-id issue to still be live")
	}
}
