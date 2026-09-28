package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/kdta91/tortui/internal/config"
)

// openAggregatorWizard drives a fresh settings test model to a blank add
// form and opens the aggregator-import wizard from it (5 tabs from Name
// lands on the torznab-and-new-only aggregator-import field; see
// visibleFields).
func openAggregatorWizard(t *testing.T, tm *teatest.TestModel) {
	t.Helper()

	tm.Send(keyRune("a"))
	waitForOutput(t, tm, "Add source")

	// "Import from aggregator" is already part of this same rendered
	// frame (every field shows regardless of cursor position), so it is
	// deliberately not waited for again here: teatest's WaitFor drains
	// Output() as it reads, and a second wait for text already inside the
	// frame the first wait matched would consume nothing new and hang
	// until its own timeout (found writing this test).
	for i := 0; i < 5; i++ {
		tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	}

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForOutput(t, tm, "Import from aggregator")
}

// TestAggregatorImportEndToEnd drives the whole T-083 flow: open the
// wizard from the blank add form, enter a base URL and API key, fetch,
// multi-select two of three listed indexers, and import them — each
// becoming an ordinary torznab config.Indexer pointed at that indexer's own
// per-indexer feed URL, saved through the same SaveSources path.
func TestAggregatorImportEndToEnd(t *testing.T) {
	sm := &fakeSourceManager{aggregatorItems: []AggregatorIndexer{
		{ID: "1", Name: "First Indexer", FeedURL: "https://example.org/1/api"},
		{ID: "2", Name: "Second Indexer", FeedURL: "https://example.org/2/api"},
		{ID: "3", Name: "Third Indexer", FeedURL: "https://example.org/3/api"},
	}}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "No sources configured")

	openAggregatorWizard(t, tm)

	tm.Send(keyRune("https://example.org"))
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	tm.Send(keyRune("my-prowlarr-key"))
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// "Third Indexer" is on this same rendered frame (the fetched list
	// shows every item at once), so it is not waited for separately.
	waitForOutput(t, tm, "First Indexer")

	tm.Send(tea.KeyMsg{Type: tea.KeySpace}) // select "First Indexer"
	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	tm.Send(tea.KeyMsg{Type: tea.KeyDown})
	tm.Send(tea.KeyMsg{Type: tea.KeySpace}) // select "Third Indexer"
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	waitForPredicate(t, func() bool { return sm.saveCallCount() > 0 })

	if gotBaseURL, gotAPIKey := sm.aggregatorLastRequest(); gotBaseURL != "https://example.org" || gotAPIKey != "my-prowlarr-key" {
		t.Fatalf("ListAggregatorIndexers called with (%q, %q)", gotBaseURL, gotAPIKey)
	}

	saved := sm.savedSources()
	if len(saved) != 2 {
		t.Fatalf("expected 2 imported sources, got %d: %#v", len(saved), saved)
	}

	byID := map[string]config.Indexer{}
	for _, s := range saved {
		byID[s.ID] = s

		if s.Type != "torznab" {
			t.Errorf("source %q type = %q, want torznab", s.ID, s.Type)
		}
		if s.APIKey != "my-prowlarr-key" {
			t.Errorf("source %q api key = %q, want my-prowlarr-key", s.ID, s.APIKey)
		}
		if !s.Enabled {
			t.Errorf("source %q enabled = false, want true", s.ID)
		}
	}

	first, ok := byID["first-indexer"]
	if !ok || first.URL != "https://example.org/1/api" || first.Name != "First Indexer" {
		t.Fatalf("first-indexer = %#v, ok=%v", first, ok)
	}

	third, ok := byID["third-indexer"]
	if !ok || third.URL != "https://example.org/3/api" || third.Name != "Third Indexer" {
		t.Fatalf("third-indexer = %#v, ok=%v", third, ok)
	}

	// The wizard closes back to the source list on a successful import.
	waitForOutput(t, tm, "First Indexer")
}

// TestAggregatorImportSecondEnterWhileSavingIsRefused proves pressing
// enter (or ctrl+s) a second time while the first import is still saving
// does not import the same selection twice — the reviewer reproduced this
// as duplicate `first`/`first-2` sources before the fix (T-083 review
// remediation). saveDelay makes the race deterministic: the first enter
// starts a save that blocks, the second enter arrives while it is still in
// flight and must be refused outright, and only then does the first save
// complete.
func TestAggregatorImportSecondEnterWhileSavingIsRefused(t *testing.T) {
	sm := &fakeSourceManager{
		aggregatorItems: []AggregatorIndexer{{ID: "1", Name: "First Indexer", FeedURL: "https://example.org/1/api"}},
		saveDelay:       150 * time.Millisecond,
	}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "No sources configured")

	openAggregatorWizard(t, tm)

	tm.Send(keyRune("https://example.org"))
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	tm.Send(keyRune("key"))
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForOutput(t, tm, "First Indexer")

	tm.Send(tea.KeyMsg{Type: tea.KeySpace}) // select "First Indexer"
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter}) // starts the (delayed) save
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter}) // must be refused: a save is in flight
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlS}) // must also be refused

	// Wait for the first save's result to reach Update() (the wizard
	// closes and reports the import), not for a fixed interval. The two
	// refused keys were queued ahead of that result, so by now Update()
	// has already handled them: a missing guard would have dispatched a
	// second save and grown the snapshot synchronously, before this point.
	waitForOutput(t, tm, "imported 1 source(s) from aggregator")

	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}

	final := tm.FinalModel(t, teatest.WithFinalTimeout(3*time.Second)).(Model)
	if final.settings.aggImport != nil {
		t.Fatal("expected the wizard to be closed after the import's save result")
	}

	if rows := final.sourceRows(); len(rows) != 1 || rows[0].ID != "first-indexer" {
		t.Fatalf("source snapshot = %#v, want exactly [first-indexer] (no second, suffixed duplicate)", rows)
	}

	if calls := sm.saveCallCount(); calls != 1 {
		t.Fatalf("SaveSources was called %d times, want exactly 1", calls)
	}

	saved := sm.savedSources()
	if len(saved) != 1 {
		t.Fatalf("expected exactly 1 imported source, got %d: %#v", len(saved), saved)
	}
	if saved[0].ID != "first-indexer" {
		t.Fatalf("imported source id = %q, want first-indexer (not a second, suffixed duplicate)", saved[0].ID)
	}
}

// TestAggregatorImportDedupesAgainstExistingIDs proves an imported
// indexer's id is slugified from its name and de-duplicated against a
// source that already exists under that id — the same rule the add/edit
// form's own resolvedID already follows (T-083 acceptance).
func TestAggregatorImportDedupesAgainstExistingIDs(t *testing.T) {
	sm := &fakeSourceManager{
		sources: []config.Indexer{
			{ID: "first-indexer", Name: "Something Else", Type: "torznab", URL: "https://example.org/existing", Enabled: true},
		},
		aggregatorItems: []AggregatorIndexer{{ID: "9", Name: "First Indexer", FeedURL: "https://example.org/9/api"}},
	}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "Something Else")

	openAggregatorWizard(t, tm)

	tm.Send(keyRune("https://example.org"))
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	tm.Send(keyRune("key"))
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForOutput(t, tm, "First Indexer")

	tm.Send(tea.KeyMsg{Type: tea.KeySpace})
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	waitForPredicate(t, func() bool { return sm.saveCallCount() > 0 })

	var got config.Indexer
	var found bool

	for _, s := range sm.savedSources() {
		if s.Name == "First Indexer" {
			got = s
			found = true
		}
	}

	if !found {
		t.Fatalf("imported source not found among %#v", sm.savedSources())
	}

	if got.ID != "first-indexer-2" {
		t.Fatalf("imported source id = %q, want first-indexer-2 (deduped against the existing first-indexer)", got.ID)
	}

	// The saved URL is the FeedURL the SourceManager reported, used
	// verbatim — this package never builds an aggregator's own per-indexer
	// URL pattern itself (T-083 review remediation).
	if got.URL != "https://example.org/9/api" {
		t.Fatalf("imported source URL = %q, want https://example.org/9/api (AggregatorIndexer.FeedURL passed through verbatim)", got.URL)
	}
}

// TestAggregatorImportRequiresBothFields proves enter on the input step
// with a blank field is refused with an inline reason rather than
// dispatching a fetch.
func TestAggregatorImportRequiresBothFields(t *testing.T) {
	sm := &fakeSourceManager{}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "No sources configured")

	openAggregatorWizard(t, tm)

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForOutput(t, tm, "base URL is required")

	tm.Send(keyRune("https://example.org"))
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForOutput(t, tm, "API key is required")

	if calls := sm.aggregatorCallCount(); calls != 0 {
		t.Fatalf("ListAggregatorIndexers was called %d times, want 0", calls)
	}
}

// TestAggregatorImportClassifiesAFailedProbe proves a failed fetch is
// classified into the same taxonomy T-081's connection test already uses
// (T-083 acceptance: "same taxonomy as T-081"), by driving an auth failure.
func TestAggregatorImportClassifiesAFailedProbe(t *testing.T) {
	sm := &fakeSourceManager{aggregatorErr: fakeAuthFailure{}}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "No sources configured")

	openAggregatorWizard(t, tm)

	tm.Send(keyRune("https://example.org"))
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	tm.Send(keyRune("wrong-key"))
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	waitForOutput(t, tm, "auth failed")
}

// fakeAuthFailure implements the duck-typed probeAuthFailure shape
// classifyProbeError matches via errors.As (T-081).
type fakeAuthFailure struct{}

func (fakeAuthFailure) Error() string    { return "unauthorized" }
func (fakeAuthFailure) AuthFailed() bool { return true }

// TestAggregatorImportEscFromInputClosesWizard proves esc on the input
// step abandons the wizard rather than saving anything.
func TestAggregatorImportEscFromInputClosesWizard(t *testing.T) {
	sm := &fakeSourceManager{}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "No sources configured")

	openAggregatorWizard(t, tm)

	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	waitForOutput(t, tm, "No sources configured")

	if sm.saveCallCount() != 0 {
		t.Fatalf("expected no save, got %d", sm.saveCallCount())
	}
}

// TestAggregatorImportEscFromListGoesBackToInput proves esc on the list
// step returns to the input step rather than closing outright.
func TestAggregatorImportEscFromListGoesBackToInput(t *testing.T) {
	sm := &fakeSourceManager{aggregatorItems: []AggregatorIndexer{{ID: "1", Name: "Only One", FeedURL: "https://example.org/1/api"}}}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "No sources configured")

	openAggregatorWizard(t, tm)

	tm.Send(keyRune("https://example.org"))
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	tm.Send(keyRune("key"))
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForOutput(t, tm, "Only One")

	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	waitForOutput(t, tm, "Base URL")
}

// TestAggregatorImportRequiresASelection proves enter on the list step
// with nothing checked is refused with an inline reason.
func TestAggregatorImportRequiresASelection(t *testing.T) {
	sm := &fakeSourceManager{aggregatorItems: []AggregatorIndexer{{ID: "1", Name: "Only One", FeedURL: "https://example.org/1/api"}}}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "No sources configured")

	openAggregatorWizard(t, tm)

	tm.Send(keyRune("https://example.org"))
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	tm.Send(keyRune("key"))
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForOutput(t, tm, "Only One")

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForOutput(t, tm, "select at least one indexer")

	if sm.saveCallCount() != 0 {
		t.Fatalf("expected no save, got %d", sm.saveCallCount())
	}
}

// TestEditFormNeverOffersAggregatorImport proves the aggregator-import
// field only appears on a blank add form, never on an edit form — a wizard
// that creates several new sources has no use editing one existing one
// (T-083's own acceptance: "from the add form").
func TestEditFormNeverOffersAggregatorImport(t *testing.T) {
	sm := &fakeSourceManager{sources: []config.Indexer{
		{ID: "existing", Name: "Existing", Type: "torznab", URL: "https://example.org/e", Enabled: true},
	}}
	tm := newSettingsTestModel(t, sm)

	waitForOutput(t, tm, "Existing")

	tm.Send(keyRune("e"))
	waitForOutput(t, tm, "Edit source")

	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}

	final := tm.FinalModel(t, teatest.WithFinalTimeout(3*time.Second)).(Model)
	if final.settings.form == nil {
		t.Fatal("expected the edit form to still be open")
	}

	for _, f := range final.settings.form.fields() {
		if f == fieldAggregatorImport {
			t.Fatal("edit form must never offer the aggregator-import field")
		}
	}
}
