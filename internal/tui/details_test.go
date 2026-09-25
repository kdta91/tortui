package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/kdta91/tortui/internal/engine/fake"
	"github.com/kdta91/tortui/internal/indexer"
	indexerfake "github.com/kdta91/tortui/internal/indexer/fake"
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
