package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/engine/fake"
	"github.com/kdta91/tortui/internal/indexer"
	indexerfake "github.com/kdta91/tortui/internal/indexer/fake"
	"github.com/kdta91/tortui/internal/store"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// --- detailsFiles / formatting helpers (no I/O, no teatest) -------------

func TestDetailsFilesParsesNewlineSeparatedList(t *testing.T) {
	r := indexer.Result{Extra: map[string]string{
		indexer.ExtraKeyFiles: "a.iso\n  b.txt  \n\nc.nfo\n",
	}}

	got := detailsFiles(r)
	want := []string{"a.iso", "b.txt", "c.nfo"}

	if len(got) != len(want) {
		t.Fatalf("detailsFiles() = %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Errorf("detailsFiles()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestDetailsFilesNilWhenAbsentOrBlank confirms the "no indexer-provided
// file list" state (nil, not merely empty) whether an adapter never set
// indexer.ExtraKeyFiles at all, or set it to nothing but whitespace — both
// mean the same thing to renderDetailsScreen's "not available" check.
func TestDetailsFilesNilWhenAbsentOrBlank(t *testing.T) {
	if got := detailsFiles(indexer.Result{}); got != nil {
		t.Errorf("detailsFiles(no Extra) = %v, want nil", got)
	}

	blank := indexer.Result{Extra: map[string]string{indexer.ExtraKeyFiles: "   \n  \n"}}
	if got := detailsFiles(blank); got != nil {
		t.Errorf("detailsFiles(blank Extra) = %v, want nil", got)
	}
}

func TestFormatPublishedDateZeroIsUnknown(t *testing.T) {
	if got := formatPublishedDate(time.Time{}); got != "unknown" {
		t.Errorf("formatPublishedDate(zero) = %q, want %q", got, "unknown")
	}

	at := time.Date(2026, 3, 4, 9, 30, 0, 0, time.UTC)
	if got := formatPublishedDate(at); got != "2026-03-04 09:30" {
		t.Errorf("formatPublishedDate(%v) = %q, want %q", at, got, "2026-03-04 09:30")
	}
}

func TestDetailsOrDash(t *testing.T) {
	if got := detailsOrDash(""); got != "-" {
		t.Errorf("detailsOrDash(%q) = %q, want %q", "", got, "-")
	}

	if got := detailsOrDash("   "); got != "-" {
		t.Errorf("detailsOrDash(whitespace) = %q, want %q", got, "-")
	}

	if got := detailsOrDash("alice"); got != "alice" {
		t.Errorf("detailsOrDash(%q) = %q, want unchanged", "alice", got)
	}
}

func TestDetailsInfoHashText(t *testing.T) {
	if got := detailsInfoHashText(""); got != "not yet resolved" {
		t.Errorf("detailsInfoHashText(%q) = %q, want %q", "", got, "not yet resolved")
	}

	if got := detailsInfoHashText("deadbeefcafe"); got != "deadbeefcafe" {
		t.Errorf("detailsInfoHashText(%q) = %q, want unchanged", "deadbeefcafe", got)
	}
}

// TestDetailsTrustText confirms the word form distinguishes every level,
// unlike Trust.Badge() (which renders Unknown and None identically blank).
func TestDetailsTrustText(t *testing.T) {
	cases := []struct {
		trust indexer.Trust
		want  string
	}{
		{indexer.TrustUnknown, "Unknown"},
		{indexer.TrustNone, "None"},
		{indexer.TrustVerified, "Verified"},
		{indexer.TrustTrusted, "Trusted"},
		{indexer.TrustVIP, "VIP"},
	}

	for _, c := range cases {
		if got := detailsTrustText(c.trust); got != c.want {
			t.Errorf("detailsTrustText(%v) = %q, want %q", c.trust, got, c.want)
		}
	}
}

// --- handleOpenDetails ('d') ----------------------------------------------

// TestHandleOpenDetailsSelectsTheHighlightedRow confirms `d` opens whichever
// row is currently highlighted in the results table — not always the same
// result — resolved back to its full indexer.Result, and that moving the
// results cursor changes which row a later `d` opens. It reads the initial
// selection off the table rather than assuming which of the two rows starts
// selected (components.Table.SetRows/SortBy preserve selection by identity,
// and resultsModel.setResults establishes it before applying the mode's
// default sort — see backlog T-963), since that startup detail is not what
// this test means to pin down.
func TestHandleOpenDetailsSelectsTheHighlightedRow(t *testing.T) {
	now := time.Now()
	results := []indexer.Result{
		{IndexerID: "alpha", ID: "1", Title: "first.iso", Seeders: 1, Magnet: "magnet:?xt=urn:btih:aaaa"},
		{IndexerID: "alpha", ID: "2", Title: "second.iso", Seeders: 100, Magnet: "magnet:?xt=urn:btih:bbbb"},
	}

	m := New(fake.New(), testTheme())
	m.lastResults = results
	m.results = m.results.setResults(results, indexer.ModeSearch, now)
	m.screen = ScreenResults

	initialID := m.results.table.SelectedID()

	var initialTitle string
	for _, r := range results {
		if resultRowID(r) == initialID {
			initialTitle = r.Title
		}
	}
	if initialTitle == "" {
		t.Fatalf("setup: could not resolve the initially selected row %q", initialID)
	}

	updated, _ := m.handleOpenDetails()
	m = updated.(Model)

	if !m.details.hasResult || m.details.result.Title != initialTitle {
		t.Fatalf("handleOpenDetails selected %q, want the highlighted row %q", m.details.result.Title, initialTitle)
	}
	if m.screen != ScreenDetails {
		t.Fatalf("screen = %v, want ScreenDetails", m.screen)
	}

	// Move the results cursor and re-open: the other result. MoveDown is a
	// no-op at the last row, so fall back to MoveUp — the test only cares
	// that *some* cursor movement changes the selection, not which
	// direction does it for this particular sort order.
	m.screen = ScreenResults
	m.results.table = m.results.table.MoveDown()
	if m.results.table.SelectedID() == initialID {
		m.results.table = m.results.table.MoveUp()
	}

	if m.results.table.SelectedID() == initialID {
		t.Fatal("setup: neither MoveDown nor MoveUp changed the selected row")
	}

	updated, _ = m.handleOpenDetails()
	m = updated.(Model)

	if m.details.result.Title == initialTitle {
		t.Fatalf("handleOpenDetails still selected %q after MoveDown", m.details.result.Title)
	}
}

func TestHandleOpenDetailsNoRowsIsNoop(t *testing.T) {
	m := New(fake.New(), testTheme())
	m.screen = ScreenResults

	updated, cmd := m.handleOpenDetails()
	m = updated.(Model)

	if cmd != nil {
		t.Errorf("cmd = %v, want nil", cmd)
	}
	if m.details.hasResult {
		t.Fatal("handleOpenDetails selected a result with no rows in the table")
	}
	if m.screen != ScreenResults {
		t.Fatalf("screen = %v, want unchanged ScreenResults", m.screen)
	}
}

// TestActionDetailsOnlyActsOnResultsScreen drives the `d` key through the
// real keymap routing (handleKey) from every screen, confirming it only
// opens details from ScreenResults and is a no-op everywhere else.
func TestActionDetailsOnlyActsOnResultsScreen(t *testing.T) {
	now := time.Now()
	results := []indexer.Result{{IndexerID: "alpha", ID: "1", Title: "x.iso", Magnet: "magnet:?xt=urn:btih:aaaa"}}

	base := New(fake.New(), testTheme())
	base.lastResults = results
	base.results = base.results.setResults(results, indexer.ModeSearch, now)

	for _, screen := range screenOrder {
		m := base
		m.screen = screen

		updated, _ := m.handleKey(keyRune("d"))
		got := updated.(Model)

		if screen == ScreenResults {
			if got.screen != ScreenDetails || !got.details.hasResult {
				t.Errorf("on ScreenResults, d = (screen=%v hasResult=%v), want ScreenDetails with a result",
					got.screen, got.details.hasResult)
			}

			continue
		}

		if got.screen != screen {
			t.Errorf("on %v, d changed screen to %v, want a no-op", screen, got.screen)
		}
	}
}

// --- renderDetailsScreen --------------------------------------------------

func TestRenderDetailsScreenEmptyState(t *testing.T) {
	m := New(fake.New(), testTheme())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	got := m.renderDetailsScreen()
	if !strings.Contains(got, "No result selected") {
		t.Errorf("renderDetailsScreen() = %q, want it to say no result is selected", got)
	}
}

// TestRenderDetailsScreenShowsEveryAcceptanceField pins T-063's acceptance
// list directly: title, size, category, uploader, trust, published date,
// source URL, infohash, and — since Extra[ExtraKeyFiles] is set here — the
// file list.
func TestRenderDetailsScreenShowsEveryAcceptanceField(t *testing.T) {
	published := time.Date(2026, 3, 4, 9, 30, 0, 0, time.UTC)
	r := indexer.Result{
		IndexerID: "alpha",
		ID:        "1",
		Title:     "full-title.iso",
		SizeBytes: 1536,
		Category:  indexer.CategorySoftware,
		Uploader:  "alice",
		Trust:     indexer.TrustTrusted,
		Published: published,
		SourceURL: "https://example.org/torrents/1",
		InfoHash:  "deadbeefcafe",
		Magnet:    "magnet:?xt=urn:btih:deadbeefcafe",
		Extra:     map[string]string{indexer.ExtraKeyFiles: "movie.mkv\nsubs.srt"},
	}

	m := New(fake.New(), testTheme())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	m.details = m.details.withResult(r)

	got := m.renderDetailsScreen()

	for _, want := range []string{
		"full-title.iso", "1.5 KB", r.Category.String(), "Trusted", "alice",
		"2026-03-04 09:30", "https://example.org/torrents/1", "deadbeefcafe",
		"movie.mkv", "subs.srt",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("renderDetailsScreen() missing %q; got:\n%s", want, got)
		}
	}
}

// TestRenderDetailsScreenFilesNotAvailableWhenIndexerProvidesNone is the
// other half of T-063's file-list acceptance: no Extra[ExtraKeyFiles] means
// the explicit "not available from this source" state, not a blank list.
func TestRenderDetailsScreenFilesNotAvailableWhenIndexerProvidesNone(t *testing.T) {
	r := indexer.Result{IndexerID: "alpha", ID: "1", Title: "no-files.iso", Magnet: "magnet:?xt=urn:btih:aaaa"}

	m := New(fake.New(), testTheme())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	m.details = m.details.withResult(r)

	got := m.renderDetailsScreen()
	if !strings.Contains(got, "not available from this source") {
		t.Errorf("renderDetailsScreen() = %q, want the file list's not-available fallback", got)
	}
}

// TestRenderDetailsScreenMissingFieldsShowDash confirms fields a source
// legitimately never published (indexer.Result's own doc comment) render
// as an explicit placeholder rather than an empty, ambiguous blank.
func TestRenderDetailsScreenMissingFieldsShowDash(t *testing.T) {
	r := indexer.Result{IndexerID: "alpha", ID: "1", Title: "sparse.iso", Magnet: "magnet:?xt=urn:btih:aaaa"}

	m := New(fake.New(), testTheme())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	m.details = m.details.withResult(r)

	got := m.renderDetailsScreen()

	if !strings.Contains(got, "not yet resolved") {
		t.Errorf("renderDetailsScreen() missing infohash placeholder; got:\n%s", got)
	}
}

// TestRenderDetailsScreenWrapsLongTitleWithoutTruncating is the regression
// test for the PR #42 review's blocking finding: renderDetailsScreen used
// to run the whole rendered body through truncateLines(..., m.width), so a
// title longer than the terminal was silently cut off with "..." — the
// reviewer's own 80x24 repro produced "...Words.1080p.2...". The title has
// no spaces (a realistic scene-release-style name), so it exercises
// theme.Wrap's hard-break path, not just ordinary word-wrap.
func TestRenderDetailsScreenWrapsLongTitleWithoutTruncating(t *testing.T) {
	title := strings.Repeat("Words.1080p.2.", 8) + "Tail" // 116 columns, no spaces

	m := New(fake.New(), testTheme())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	m.details = m.details.withResult(indexer.Result{Title: title, Magnet: "magnet:?xt=urn:btih:aaaa"})

	got := m.renderDetailsScreen()

	joined := strings.ReplaceAll(got, "\n", "")
	if !strings.Contains(joined, title) {
		t.Fatalf("renderDetailsScreen() lost part of a long title; want %q reproduced in the joined output; got:\n%s",
			title, got)
	}

	for _, line := range strings.Split(got, "\n") {
		if w := theme.Width(line); w > 80 {
			t.Errorf("renderDetailsScreen() line %q is %d columns wide, want <= 80", line, w)
		}
	}
}

// TestRenderDetailsScreenWrapsLongSourceURLWithoutTruncating is the same
// regression for the Source field the review also called out.
func TestRenderDetailsScreenWrapsLongSourceURLWithoutTruncating(t *testing.T) {
	url := "https://example.org/torrents/" + strings.Repeat("a", 90)

	m := New(fake.New(), testTheme())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)
	m.details = m.details.withResult(indexer.Result{
		Title: "short.iso", Magnet: "magnet:?xt=urn:btih:aaaa", SourceURL: url,
	})

	got := m.renderDetailsScreen()

	// writeWrappedField indents every continuation line by two spaces
	// (like the file list below it); strip "\n  " as a unit before the
	// plain "\n" removal below, so that indent is never mistaken for part
	// of the reconstructed URL itself.
	joined := strings.ReplaceAll(got, "\n  ", "")
	joined = strings.ReplaceAll(joined, "\n", "")

	if !strings.Contains(joined, url) {
		t.Fatalf("renderDetailsScreen() lost part of a long source URL; want %q reproduced in the joined output; got:\n%s",
			url, got)
	}

	for _, line := range strings.Split(got, "\n") {
		if w := theme.Width(line); w > 80 {
			t.Errorf("renderDetailsScreen() line %q is %d columns wide, want <= 80", line, w)
		}
	}
}

// --- handleAddFromDetails / handleAddResult (enter) -----------------------

func TestHandleAddFromDetailsNoResultIsNoop(t *testing.T) {
	m := New(fake.New(), testTheme())

	updated, cmd := m.handleAddFromDetails()
	m = updated.(Model)

	if cmd != nil {
		t.Errorf("cmd = %v, want nil", cmd)
	}
	if m.statusBar.Message() != "" {
		t.Errorf("statusBar.Message() = %q, want empty", m.statusBar.Message())
	}
}

func TestHandleAddFromDetailsNoEngineConfigured(t *testing.T) {
	m := New(nil, testTheme())
	m.details = m.details.withResult(indexer.Result{Magnet: "magnet:?xt=urn:btih:aaaa"})

	updated, _ := m.handleAddFromDetails()
	m = updated.(Model)

	if !strings.Contains(m.statusBar.Message(), "no engine configured") {
		t.Errorf("statusBar.Message() = %q, want it to mention no engine configured", m.statusBar.Message())
	}
}

func TestHandleAddFromDetailsInvalidResultPushesError(t *testing.T) {
	m := New(fake.New(), testTheme())
	m.details = m.details.withResult(indexer.Result{Title: "no-link.iso"}) // no Magnet or TorrentURL

	updated, _ := m.handleAddFromDetails()
	m = updated.(Model)

	if !strings.Contains(m.statusBar.Message(), "can't add") {
		t.Errorf("statusBar.Message() = %q, want it to mention the validation failure", m.statusBar.Message())
	}
}

// TestActionSelectOnDetailsScreenAddsTorrentAndSwitchesToDownloads drives
// the full path through the real keymap routing: enter (ActionSelect) on
// ScreenDetails dispatches the add, and once addResultMsg reports success
// (via the real Update, not a direct handleAddResult call), the screen
// switches to downloads and the torrent is genuinely tracked by the engine
// — T-063's acceptance text verified end to end, not just at one seam.
func TestActionSelectOnDetailsScreenAddsTorrentAndSwitchesToDownloads(t *testing.T) {
	eng := fake.New()
	t.Cleanup(func() { _ = eng.Close() })

	m := New(eng, testTheme())
	m.screen = ScreenDetails
	m.details = m.details.withResult(indexer.Result{
		Title: "select-flow.iso", Magnet: "magnet:?xt=urn:btih:cafebabe",
	})

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if cmd == nil {
		t.Fatal("expected a cmd dispatching the add")
	}

	msg := cmd()

	addMsg, ok := msg.(addResultMsg)
	if !ok {
		t.Fatalf("cmd() = %#v (%T), want addResultMsg", msg, msg)
	}
	if addMsg.err != nil {
		t.Fatalf("addResultMsg.err = %v, want nil", addMsg.err)
	}
	if addMsg.name != "select-flow.iso" {
		t.Errorf("addResultMsg.name = %q, want %q", addMsg.name, "select-flow.iso")
	}

	updated, _ = m.Update(addMsg)
	m = updated.(Model)

	if m.screen != ScreenDownloads {
		t.Fatalf("screen after successful add = %v, want ScreenDownloads", m.screen)
	}
	if !strings.Contains(m.statusBar.Message(), "select-flow.iso") {
		t.Errorf("statusBar.Message() = %q, want it to mention the added title", m.statusBar.Message())
	}

	if len(eng.List()) != 1 {
		t.Fatalf("engine.List() has %d entries, want 1", len(eng.List()))
	}
}

// TestHandleAddResultReportsEngineFailureWithoutSwitchingScreen confirms a
// failed Add (e.g. the engine refuses the call) is reported in the status
// bar without leaving the details screen — switching to an empty downloads
// screen on a failed add would be worse than staying put with a readable
// error.
func TestHandleAddResultReportsEngineFailureWithoutSwitchingScreen(t *testing.T) {
	eng := fake.New()
	if err := eng.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	m := New(eng, testTheme())
	m.details = m.details.withResult(indexer.Result{Title: "closed.iso", Magnet: "magnet:?xt=urn:btih:aaaa"})

	updated, cmd := m.handleAddFromDetails()
	m = updated.(Model)

	if cmd == nil {
		t.Fatal("expected a dispatch cmd")
	}

	msg := cmd()

	addMsg, ok := msg.(addResultMsg)
	if !ok {
		t.Fatalf("cmd() = %#v (%T), want addResultMsg", msg, msg)
	}
	if addMsg.err == nil {
		t.Fatal("expected Add against a closed engine to fail")
	}

	wantScreen := m.screen

	updated, _ = m.handleAddResult(addMsg)
	m = updated.(Model)

	if m.screen != wantScreen {
		t.Errorf("screen after failed add = %v, want unchanged %v", m.screen, wantScreen)
	}
	if !strings.Contains(m.statusBar.Message(), "couldn't add torrent") {
		t.Errorf("statusBar.Message() = %q, want it to mention the add failure", m.statusBar.Message())
	}
}

// --- startAdd / finishAdd (T-070: Resolve, dedup, Origin, destination) ----

// resolvingIndexer is a programmable indexer.Indexer test double whose
// Resolve does whatever resolveFn says — a canned successful result, or a
// canned error — so these tests can drive the add flow's Resolve step
// without a network call (AGENT.md §6.7).
type resolvingIndexer struct {
	id        string
	resolveFn func(r indexer.Result) (indexer.Result, error)
	calls     int
}

func (f *resolvingIndexer) ID() string         { return f.id }
func (f *resolvingIndexer) Name() string       { return f.id }
func (f *resolvingIndexer) Caps() indexer.Caps { return indexer.Caps{Search: true} }

func (f *resolvingIndexer) Search(context.Context, indexer.Query) ([]indexer.Result, error) {
	return nil, nil
}

func (f *resolvingIndexer) Resolve(_ context.Context, r indexer.Result) (indexer.Result, error) {
	f.calls++
	if f.resolveFn != nil {
		return f.resolveFn(r)
	}

	return r, nil
}

// stubResolveSearcher is a minimal Searcher test double for the add flow:
// only Get is exercised here — the search screen's own tests already cover
// Enabled/SearchAll — so those two are trivial stubs.
type stubResolveSearcher struct {
	indexers map[string]indexer.Indexer
}

func newStubResolveSearcher(ixs ...indexer.Indexer) *stubResolveSearcher {
	m := make(map[string]indexer.Indexer, len(ixs))
	for _, ix := range ixs {
		m[ix.ID()] = ix
	}

	return &stubResolveSearcher{indexers: m}
}

func (s *stubResolveSearcher) Enabled() []indexer.Indexer { return nil }

func (s *stubResolveSearcher) SearchAll(context.Context, indexer.Query, ...string) ([]indexer.Result, []indexer.SourceError, error) {
	return nil, nil, nil
}

func (s *stubResolveSearcher) Get(id string) (indexer.Indexer, bool) {
	ix, ok := s.indexers[id]
	return ix, ok
}

// stubTorrentStore is a TorrentStore test double recording every
// SetTorrent call, or failing every one when failWith is set.
type stubTorrentStore struct {
	records  []store.TorrentRecord
	failWith error
}

func (s *stubTorrentStore) SetTorrent(rec store.TorrentRecord) error {
	if s.failWith != nil {
		return s.failWith
	}

	s.records = append(s.records, rec)

	return nil
}

// GetTorrent implements the read half of TorrentStore (root.go, T-071):
// the last record SetTorrent recorded for id, if any.
func (s *stubTorrentStore) GetTorrent(id string) (store.TorrentRecord, bool) {
	for i := len(s.records) - 1; i >= 0; i-- {
		if s.records[i].ID == id {
			return s.records[i], true
		}
	}

	return store.TorrentRecord{}, false
}

// stubDedupEngine is a minimal engine.Engine test double whose List()
// returns a fixed set of statuses (including an InfoHash — something
// internal/engine/fake never populates from AddSource, since a real
// engine only learns an infohash once it parses the magnet or fetches
// metadata) and whose Add fails the test outright: duplicateTorrentID's
// whole point is that Add is never reached for a known-duplicate infohash.
type stubDedupEngine struct {
	t        *testing.T
	statuses []engine.TorrentStatus
	updates  chan []engine.TorrentStatus
}

func newStubDedupEngine(t *testing.T, statuses ...engine.TorrentStatus) *stubDedupEngine {
	t.Helper()
	return &stubDedupEngine{t: t, statuses: statuses, updates: make(chan []engine.TorrentStatus)}
}

func (e *stubDedupEngine) Add(context.Context, engine.AddSource) (string, error) {
	e.t.Helper()
	e.t.Fatal("Add called — duplicate detection should have short-circuited it")
	return "", nil
}
func (e *stubDedupEngine) Pause(string) error           { return nil }
func (e *stubDedupEngine) Resume(string) error          { return nil }
func (e *stubDedupEngine) Remove(string, bool) error    { return nil }
func (e *stubDedupEngine) List() []engine.TorrentStatus { return e.statuses }

func (e *stubDedupEngine) Files(string) ([]engine.FileStatus, error) { return nil, nil }
func (e *stubDedupEngine) Updates() <-chan []engine.TorrentStatus    { return e.updates }
func (e *stubDedupEngine) Close() error                              { return nil }

var _ engine.Engine = (*stubDedupEngine)(nil)

// TestStartAddResolvesWhenMagnetIsEmpty is T-070's first acceptance line:
// "Calls Resolve first when the result lacks a magnet." The magnet a
// successful Resolve fills in is what actually reaches engine.Add.
func TestStartAddResolvesWhenMagnetIsEmpty(t *testing.T) {
	eng := fake.New()
	t.Cleanup(func() { _ = eng.Close() })

	const resolvedMagnet = "magnet:?xt=urn:btih:resolved00"

	ix := &resolvingIndexer{id: "src-a", resolveFn: func(r indexer.Result) (indexer.Result, error) {
		r.Magnet = resolvedMagnet
		r.InfoHash = "resolved00"

		return r, nil
	}}

	m := New(eng, testTheme(), WithSearcher(newStubResolveSearcher(ix)))
	m.details = m.details.withResult(indexer.Result{
		Title: "needs-resolve.iso", IndexerID: "src-a", TorrentURL: "https://example.org/t/1",
	})

	updated, cmd := m.handleAddFromDetails()
	m = updated.(Model)

	if cmd == nil {
		t.Fatal("expected a cmd dispatching Resolve")
	}
	if ix.calls != 0 {
		t.Fatalf("Resolve called synchronously inside Update (calls=%d), want it deferred to the returned cmd", ix.calls)
	}

	msg := cmd()

	resolveMsg, ok := msg.(resolveResultMsg)
	if !ok {
		t.Fatalf("cmd() = %#v (%T), want resolveResultMsg", msg, msg)
	}
	if resolveMsg.err != nil {
		t.Fatalf("resolveResultMsg.err = %v, want nil", resolveMsg.err)
	}
	if ix.calls != 1 {
		t.Fatalf("ix.calls = %d, want 1", ix.calls)
	}

	updated, addCmd := m.Update(resolveMsg)
	m = updated.(Model)

	if addCmd == nil {
		t.Fatal("expected a cmd dispatching the add after a successful resolve")
	}

	addMsg, ok := addCmd().(addResultMsg)
	if !ok {
		t.Fatalf("addCmd() did not produce addResultMsg")
	}
	if addMsg.err != nil {
		t.Fatalf("addResultMsg.err = %v, want nil", addMsg.err)
	}
	if addMsg.magnet != resolvedMagnet {
		t.Errorf("addResultMsg.magnet = %q, want the resolved magnet %q", addMsg.magnet, resolvedMagnet)
	}

	updated, _ = m.Update(addMsg)
	m = updated.(Model)

	if m.screen != ScreenDownloads {
		t.Fatalf("screen = %v, want ScreenDownloads", m.screen)
	}
	if len(eng.List()) != 1 {
		t.Fatalf("engine.List() has %d entries, want 1", len(eng.List()))
	}
}

// TestStartAddResolveFailureSurfacesAsStatusBarErrorNotACrash is T-070's
// first acceptance line's second half: a Resolve failure is a status-bar
// message, never a panic, and engine.Add is never reached.
func TestStartAddResolveFailureSurfacesAsStatusBarErrorNotACrash(t *testing.T) {
	eng := fake.New()
	t.Cleanup(func() { _ = eng.Close() })

	resolveErr := errors.New("example.org: resolve failed")
	ix := &resolvingIndexer{id: "src-a", resolveFn: func(r indexer.Result) (indexer.Result, error) {
		return r, resolveErr
	}}

	m := New(eng, testTheme(), WithSearcher(newStubResolveSearcher(ix)))
	m.details = m.details.withResult(indexer.Result{Title: "broken.iso", IndexerID: "src-a"})

	updated, cmd := m.handleAddFromDetails()
	m = updated.(Model)

	resolveMsg, ok := cmd().(resolveResultMsg)
	if !ok {
		t.Fatalf("cmd() did not produce resolveResultMsg")
	}

	updated, _ = m.Update(resolveMsg)
	m = updated.(Model)

	if !strings.Contains(m.statusBar.Message(), "couldn't add") {
		t.Errorf("statusBar.Message() = %q, want it to mention the resolve failure", m.statusBar.Message())
	}
	if len(eng.List()) != 0 {
		t.Fatalf("engine.List() has %d entries, want 0 — Add must not have been called", len(eng.List()))
	}
}

// TestStartAddNoSourceAvailableToResolvePushesCantAdd covers a Result whose
// IndexerID names no currently registered source (or no Searcher was wired
// at all): the add is refused with a readable reason instead of panicking
// on a nil lookup.
func TestStartAddNoSourceAvailableToResolvePushesCantAdd(t *testing.T) {
	m := New(fake.New(), testTheme()) // no WithSearcher
	m.details = m.details.withResult(indexer.Result{Title: "orphan.iso", IndexerID: "gone"})

	updated, _ := m.handleAddFromDetails()
	m = updated.(Model)

	if !strings.Contains(m.statusBar.Message(), "can't add") {
		t.Errorf("statusBar.Message() = %q, want it to mention the add failure", m.statusBar.Message())
	}
}

// TestStartAddDuplicateInfoHashSelectsExistingDownloadInsteadOfAddingTwice
// is T-070's second acceptance line: a Result whose infohash matches an
// already-tracked torrent (case-insensitively) selects that existing entry
// — switches to the downloads screen and points the selection cursor at
// it — instead of calling engine.Add a second time.
func TestStartAddDuplicateInfoHashSelectsExistingDownloadInsteadOfAddingTwice(t *testing.T) {
	eng := newStubDedupEngine(t,
		engine.TorrentStatus{ID: "existing-1", InfoHash: "CAFEBABE01"},
		engine.TorrentStatus{ID: "existing-0", InfoHash: "aaaa"},
	)

	m := New(eng, testTheme())
	m.screen = ScreenSearch // prove startAdd itself switches the screen

	dup := indexer.Result{
		Title: "duplicate.iso", Magnet: "magnet:?xt=urn:btih:CAFEBABE01", InfoHash: "cafebabe01",
	}

	updated, cmd := m.startAdd(dup)
	m = updated.(Model)

	if cmd == nil {
		t.Fatal("expected a status-bar cmd from the duplicate path")
	}
	if m.screen != ScreenDownloads {
		t.Fatalf("screen = %v, want ScreenDownloads", m.screen)
	}
	if !strings.Contains(m.statusBar.Message(), "already downloading") {
		t.Errorf("statusBar.Message() = %q, want it to mention the existing download", m.statusBar.Message())
	}

	wantIndex := m.downloadIndexOf("existing-1")
	if m.selection != wantIndex {
		t.Errorf("selection = %d, want %d (the existing torrent's index)", m.selection, wantIndex)
	}
}

// TestStartAddEmptyInfoHashNeverMatchesADuplicate confirms a Result that
// has not yet resolved an infohash (the common pre-Resolve case) is never
// treated as a duplicate of anything, however it fails afterwards — dedup
// activates only on a genuine, known infohash match.
func TestStartAddEmptyInfoHashNeverMatchesADuplicate(t *testing.T) {
	eng := fake.New()
	t.Cleanup(func() { _ = eng.Close() })

	m := New(eng, testTheme())
	m.details = m.details.withResult(indexer.Result{Title: "fresh.iso", Magnet: "magnet:?xt=urn:btih:ffff0000"})

	updated, cmd := m.handleAddFromDetails()
	m = updated.(Model)

	addMsg, ok := cmd().(addResultMsg)
	if !ok {
		t.Fatalf("cmd() did not produce addResultMsg")
	}
	if addMsg.err != nil {
		t.Fatalf("addResultMsg.err = %v, want nil", addMsg.err)
	}
}

// TestFinishAddValidatesTheResolvedResult covers a Result whose Resolve
// succeeds but still leaves neither a Magnet nor a TorrentURL (a source
// whose Resolve genuinely has nothing to offer this particular result):
// finishAdd's own Validate call catches it, the same way it always has,
// and engine.Add is never reached.
func TestFinishAddValidatesTheResolvedResult(t *testing.T) {
	eng := newStubDedupEngine(t)

	ix := &resolvingIndexer{id: "src-a"} // resolveFn nil: Resolve is a no-op

	m := New(eng, testTheme(), WithSearcher(newStubResolveSearcher(ix)))
	m.details = m.details.withResult(indexer.Result{Title: "no-link.iso", IndexerID: "src-a"})

	updated, cmd := m.handleAddFromDetails()
	m = updated.(Model)

	resolveMsg, ok := cmd().(resolveResultMsg)
	if !ok {
		t.Fatalf("cmd() did not produce resolveResultMsg")
	}

	updated, _ = m.Update(resolveMsg)
	m = updated.(Model)

	if !strings.Contains(m.statusBar.Message(), "can't add") {
		t.Errorf("statusBar.Message() = %q, want it to mention the validation failure", m.statusBar.Message())
	}
}

// TestFinishAddDetectsADuplicateDiscoveredByResolve covers the case where
// the infohash match is only known *after* Resolve runs — the common case,
// since most Results reach the add flow with an empty InfoHash. finishAdd
// re-checks for a duplicate rather than assuming startAdd's earlier,
// pre-Resolve check (which had nothing to match on) was the only chance.
func TestFinishAddDetectsADuplicateDiscoveredByResolve(t *testing.T) {
	eng := newStubDedupEngine(t, engine.TorrentStatus{ID: "existing-1", InfoHash: "AABBCCDD"})

	ix := &resolvingIndexer{id: "src-a", resolveFn: func(r indexer.Result) (indexer.Result, error) {
		r.Magnet = "magnet:?xt=urn:btih:AABBCCDD"
		r.InfoHash = "aabbccdd"

		return r, nil
	}}

	m := New(eng, testTheme(), WithSearcher(newStubResolveSearcher(ix)))
	m.details = m.details.withResult(indexer.Result{Title: "found-by-resolve.iso", IndexerID: "src-a"})

	updated, cmd := m.handleAddFromDetails()
	m = updated.(Model)

	resolveMsg, ok := cmd().(resolveResultMsg)
	if !ok {
		t.Fatalf("cmd() did not produce resolveResultMsg")
	}

	updated, _ = m.Update(resolveMsg)
	m = updated.(Model)

	if m.screen != ScreenDownloads {
		t.Fatalf("screen = %v, want ScreenDownloads", m.screen)
	}
	if !strings.Contains(m.statusBar.Message(), "already downloading") {
		t.Errorf("statusBar.Message() = %q, want it to mention the existing download", m.statusBar.Message())
	}
}

// TestHandleAddResultPersistsOriginViaTheStore is T-070's third acceptance
// line: a successful add records IndexerID and SourceURL via the wired
// TorrentStore.
func TestHandleAddResultPersistsOriginViaTheStore(t *testing.T) {
	eng := fake.New()
	t.Cleanup(func() { _ = eng.Close() })

	ts := &stubTorrentStore{}

	m := New(eng, testTheme(), WithTorrentStore(ts))
	m.details = m.details.withResult(indexer.Result{
		Title: "origin.iso", IndexerID: "src-a", SourceURL: "https://example.org/t/9",
		Magnet: "magnet:?xt=urn:btih:aaaa1111",
	})

	updated, cmd := m.handleAddFromDetails()
	m = updated.(Model)

	addMsg, ok := cmd().(addResultMsg)
	if !ok {
		t.Fatalf("cmd() did not produce addResultMsg")
	}

	updated, _ = m.Update(addMsg)
	m = updated.(Model)

	if len(ts.records) != 1 {
		t.Fatalf("len(records) = %d, want 1", len(ts.records))
	}

	rec := ts.records[0]
	if rec.ID != addMsg.id {
		t.Errorf("rec.ID = %q, want %q", rec.ID, addMsg.id)
	}
	if rec.IndexerID != "src-a" {
		t.Errorf("rec.IndexerID = %q, want %q", rec.IndexerID, "src-a")
	}
	if rec.SourceURL != "https://example.org/t/9" {
		t.Errorf("rec.SourceURL = %q, want %q", rec.SourceURL, "https://example.org/t/9")
	}
	if rec.Name != "origin.iso" {
		t.Errorf("rec.Name = %q, want %q", rec.Name, "origin.iso")
	}
}

// TestHandleAddResultReportsAPersistFailureWithoutUndoingTheAdd confirms a
// broken TorrentStore is surfaced as a status-bar message but never blocks
// or reverses the already-successful engine.Add.
func TestHandleAddResultReportsAPersistFailureWithoutUndoingTheAdd(t *testing.T) {
	eng := fake.New()
	t.Cleanup(func() { _ = eng.Close() })

	ts := &stubTorrentStore{failWith: errors.New("disk full")}

	m := New(eng, testTheme(), WithTorrentStore(ts))
	m.details = m.details.withResult(indexer.Result{Title: "still-added.iso", Magnet: "magnet:?xt=urn:btih:bbbb2222"})

	updated, cmd := m.handleAddFromDetails()
	m = updated.(Model)

	addMsg, ok := cmd().(addResultMsg)
	if !ok {
		t.Fatalf("cmd() did not produce addResultMsg")
	}

	updated, _ = m.Update(addMsg)
	m = updated.(Model)

	if m.screen != ScreenDownloads {
		t.Fatalf("screen = %v, want ScreenDownloads even though persistence failed", m.screen)
	}
	if len(eng.List()) != 1 {
		t.Fatalf("engine.List() has %d entries, want 1 — the add itself must still have succeeded", len(eng.List()))
	}
	if !strings.Contains(m.statusBar.Message(), "disk full") {
		t.Errorf("statusBar.Message() = %q, want it to mention the persist failure", m.statusBar.Message())
	}
}

// TestFinishAddResolvesTheConfiguredDownloadDirIntoSavePath is T-070's
// fourth acceptance line: "the resolved absolute path goes into
// AddSource.SavePath." WithDownloadDir is this task's own resolution (the
// configured default only); T-074 replaces it with an interactive
// per-torrent choice on top of the same flow.
func TestFinishAddResolvesTheConfiguredDownloadDirIntoSavePath(t *testing.T) {
	eng := fake.New()
	t.Cleanup(func() { _ = eng.Close() })

	dir := filepath.Join(t.TempDir(), "downloads")

	m := New(eng, testTheme(), WithDownloadDir(dir))
	m.details = m.details.withResult(indexer.Result{Title: "dest.iso", Magnet: "magnet:?xt=urn:btih:cccc3333"})

	updated, cmd := m.handleAddFromDetails()
	m = updated.(Model)

	addMsg, ok := cmd().(addResultMsg)
	if !ok {
		t.Fatalf("cmd() did not produce addResultMsg")
	}
	if addMsg.err != nil {
		t.Fatalf("addResultMsg.err = %v, want nil", addMsg.err)
	}

	statuses := eng.List()
	if len(statuses) != 1 {
		t.Fatalf("engine.List() has %d entries, want 1", len(statuses))
	}
	if statuses[0].SavePath != filepath.Clean(dir) {
		t.Errorf("SavePath = %q, want %q", statuses[0].SavePath, filepath.Clean(dir))
	}
}

// TestFinishAddLeavesSavePathEmptyWithNoDownloadDirConfigured confirms the
// no-WithDownloadDir case falls back to engine.AddSource.SavePath's own
// documented "empty means use the configured default" rather than this
// flow inventing a path of its own.
func TestFinishAddLeavesSavePathEmptyWithNoDownloadDirConfigured(t *testing.T) {
	eng := fake.New()
	t.Cleanup(func() { _ = eng.Close() })

	m := New(eng, testTheme())
	m.details = m.details.withResult(indexer.Result{Title: "no-dir.iso", Magnet: "magnet:?xt=urn:btih:dddd4444"})

	_, cmd := m.handleAddFromDetails()

	addMsg, ok := cmd().(addResultMsg)
	if !ok {
		t.Fatalf("cmd() did not produce addResultMsg")
	}
	if addMsg.err != nil {
		t.Fatalf("addResultMsg.err = %v, want nil", addMsg.err)
	}

	if got := eng.List()[0].SavePath; got != "" {
		t.Errorf("SavePath = %q, want empty (engine's own default)", got)
	}
}

// --- handleAddFromResults (enter on the results screen) --------------------

// TestHandleAddFromResultsNoSelectionIsNoop confirms an empty results table
// makes enter on the results screen a no-op, exactly like the details
// screen with nothing selected.
func TestHandleAddFromResultsNoSelectionIsNoop(t *testing.T) {
	m := New(fake.New(), testTheme())

	updated, cmd := m.handleAddFromResults()
	m = updated.(Model)

	if cmd != nil {
		t.Errorf("cmd = %v, want nil", cmd)
	}
	if m.statusBar.Message() != "" {
		t.Errorf("statusBar.Message() = %q, want empty", m.statusBar.Message())
	}
}

// TestActionSelectOnResultsScreenAddsTheSelectedResult drives the full path
// through the real keymap routing (AGENT.md §7: "enter | Add torrent
// (results)"): enter on ScreenResults with a populated table adds the
// currently selected row's result.
func TestActionSelectOnResultsScreenAddsTheSelectedResult(t *testing.T) {
	eng := fake.New()
	t.Cleanup(func() { _ = eng.Close() })

	m := New(eng, testTheme())
	m.screen = ScreenResults

	r := indexer.Result{Title: "from-results.iso", Magnet: "magnet:?xt=urn:btih:eeee5555"}
	m.lastResults = []indexer.Result{r}
	m.results = m.results.setResults(m.lastResults, indexer.ModeSearch, time.Now())

	if m.results.table.SelectedID() == "" {
		t.Fatal("expected setResults to select the only row")
	}

	updated, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if cmd == nil {
		t.Fatal("expected a cmd dispatching the add")
	}

	addMsg, ok := cmd().(addResultMsg)
	if !ok {
		t.Fatalf("cmd() did not produce addResultMsg")
	}
	if addMsg.name != "from-results.iso" {
		t.Errorf("addResultMsg.name = %q, want %q", addMsg.name, "from-results.iso")
	}

	updated, _ = m.Update(addMsg)
	m = updated.(Model)

	if m.screen != ScreenDownloads {
		t.Fatalf("screen = %v, want ScreenDownloads", m.screen)
	}
	if len(eng.List()) != 1 {
		t.Fatalf("engine.List() has %d entries, want 1", len(eng.List()))
	}
}

// --- handleOpenSource / openSourceCmd ('u') --------------------------------

func TestHandleOpenSourceNoResultIsNoop(t *testing.T) {
	m := New(fake.New(), testTheme())

	updated, cmd := m.handleOpenSource()
	m = updated.(Model)

	if cmd != nil {
		t.Errorf("cmd = %v, want nil", cmd)
	}
	if m.statusBar.Message() != "" {
		t.Errorf("statusBar.Message() = %q, want empty", m.statusBar.Message())
	}
}

func TestHandleOpenSourceEmptySourceURLPushesMessage(t *testing.T) {
	m := New(fake.New(), testTheme())
	m.details = m.details.withResult(indexer.Result{Title: "no-source.iso"})

	updated, _ := m.handleOpenSource()
	m = updated.(Model)

	if !strings.Contains(m.statusBar.Message(), "no source page") {
		t.Errorf("statusBar.Message() = %q, want it to mention no source page", m.statusBar.Message())
	}
}

// TestHandleOpenSourceCallsOpenURLWithTheResultsSourceURL confirms `u`
// calls through m.openURL (WithOpenURL's seam here) with exactly the
// selected result's SourceURL, never actually shelling out to a real
// browser in this test.
func TestHandleOpenSourceCallsOpenURLWithTheResultsSourceURL(t *testing.T) {
	var gotURL string
	stub := func(rawURL string) error {
		gotURL = rawURL
		return nil
	}

	m := New(fake.New(), testTheme(), WithOpenURL(stub))
	m.details = m.details.withResult(indexer.Result{SourceURL: "https://example.org/torrents/1"})

	updated, cmd := m.handleOpenSource()
	m = updated.(Model)

	if cmd == nil {
		t.Fatal("expected a non-nil cmd to actually call open")
	}

	if msg := cmd(); msg != nil {
		t.Errorf("cmd() = %#v, want nil on success", msg)
	}

	if gotURL != "https://example.org/torrents/1" {
		t.Errorf("openURL called with %q, want %q", gotURL, "https://example.org/torrents/1")
	}
}

// TestActionOpenSourceOnlyActsOnDetailsScreen drives `u` through the real
// keymap routing from every screen: only ScreenDetails calls through to
// openURL.
func TestActionOpenSourceOnlyActsOnDetailsScreen(t *testing.T) {
	for _, screen := range screenOrder {
		var calls int
		stub := func(string) error { calls++; return nil }

		m := New(fake.New(), testTheme(), WithOpenURL(stub))
		m.screen = screen
		m.details = m.details.withResult(indexer.Result{SourceURL: "https://example.org/x"})

		updated, cmd := m.handleKey(keyRune("u"))
		_ = updated.(Model)

		if cmd != nil {
			cmd()
		}

		wantCalls := 0
		if screen == ScreenDetails {
			wantCalls = 1
		}

		if calls != wantCalls {
			t.Errorf("on %v, u called openURL %d time(s), want %d", screen, calls, wantCalls)
		}
	}
}

func TestOpenSourceCmdReportsFailureAsTransientMessage(t *testing.T) {
	wantErr := errors.New("boom")
	cmd := openSourceCmd(func(string) error { return wantErr }, "https://example.org")

	msg := cmd()

	tmMsg, ok := msg.(transientMessageMsg)
	if !ok {
		t.Fatalf("cmd() = %#v (%T), want transientMessageMsg", msg, msg)
	}
	if !strings.Contains(tmMsg.text, "boom") {
		t.Errorf("transientMessageMsg.text = %q, want it to mention the error", tmMsg.text)
	}
}

func TestOpenSourceCmdSuccessReturnsNilMsg(t *testing.T) {
	called := false
	cmd := openSourceCmd(func(string) error {
		called = true
		return nil
	}, "https://example.org")

	if msg := cmd(); msg != nil {
		t.Errorf("cmd() = %#v, want nil on success", msg)
	}
	if !called {
		t.Error("openSourceCmd did not call the supplied open func")
	}
}

// --- end-to-end: search -> details -> add (teatest) ------------------------

// TestDetailsScreenEndToEndSelectAndAdd drives the whole path through a
// real teatest program: dispatch a search against a real *indexer.Registry,
// land on results, press d to open details on the one result, press enter,
// and confirm the screen switches to downloads and the torrent reaches a
// real (fake) engine — proving the key routing wired in root.go, not just
// the individual handlers in isolation.
func TestDetailsScreenEndToEndSelectAndAdd(t *testing.T) {
	reg := indexer.NewRegistry(indexer.Config{})
	ix := indexerfake.New("alpha", "Alpha", indexer.Caps{Search: true, Latest: true}, []indexer.Result{
		{
			IndexerID: "alpha", ID: "1", Title: "details-flow.iso",
			Magnet: "magnet:?xt=urn:btih:cafebabe", Seeders: 10,
			SourceURL: "https://example.org/torrents/1",
		},
	})

	if err := reg.Register(ix); err != nil {
		t.Fatalf("Register: %v", err)
	}

	eng := fake.New()
	t.Cleanup(func() { _ = eng.Close() })

	m := New(eng, testTheme(), WithSearcher(reg))
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	waitForOutput(t, tm, "Query:")

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter}) // empty query -> Latest, jumps to results
	waitForOutput(t, tm, "details-flow.iso")

	tm.Send(keyRune("d"))
	waitForAllOutput(t, tm, "details-flow.iso", "not yet resolved")

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForOutput(t, tm, "added details-flow.iso")

	deadline := time.Now().Add(2 * time.Second)
	for len(eng.List()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("engine never received the added torrent")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
