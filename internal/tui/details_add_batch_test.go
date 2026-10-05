package tui

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/engine/fake"
	"github.com/kdta91/tortui/internal/indexer"
)

// ctxIndexer is a source whose Resolve reports the deadline of the context it
// was given, then blocks until that context ends when block is set.
type ctxIndexer struct {
	resolvingIndexer
	block    bool
	deadline chan time.Time
}

func (c *ctxIndexer) Resolve(ctx context.Context, r indexer.Result) (indexer.Result, error) {
	if d, ok := ctx.Deadline(); ok {
		c.deadline <- d
	} else {
		c.deadline <- time.Time{}
	}

	if c.block {
		<-ctx.Done()

		return r, ctx.Err()
	}

	r.Magnet = "magnet:?xt=urn:btih:ctx00"
	r.InfoHash = "ctx00"

	return r, nil
}

func newCtxIndexer(block bool) *ctxIndexer {
	return &ctxIndexer{resolvingIndexer: resolvingIndexer{id: "src-ctx"}, block: block, deadline: make(chan time.Time, 1)}
}

func linklessResult() indexer.Result {
	return indexer.Result{IndexerID: "src-ctx", ID: "1", Title: "slow.iso", TorrentURL: "https://example.org/t/1"}
}

// T-966: the Resolve the add flow dispatches carries a deadline.
func TestResolveCarriesADeadline(t *testing.T) {
	ix := newCtxIndexer(false)
	m := New(newTestEngine(t), testTheme(), WithSearcher(newStubResolveSearcher(ix)))

	start := time.Now()
	_, cmd := m.startAdd(linklessResult())

	if _, ok := cmd().(resolveResultMsg); !ok {
		t.Fatal("cmd() did not produce resolveResultMsg")
	}

	d := <-ix.deadline
	if d.IsZero() {
		t.Fatal("Resolve got a context with no deadline")
	}

	if got := d.Sub(start); got <= 0 || got > resolveTimeout+time.Second {
		t.Fatalf("deadline in %v, want within %v", got, resolveTimeout)
	}
}

// ctxEngine records the context Add was called with.
type ctxEngine struct {
	*fake.Engine
	hadDeadline bool
}

func (c *ctxEngine) Add(ctx context.Context, src engine.AddSource) (string, error) {
	_, c.hadDeadline = ctx.Deadline()

	return c.Engine.Add(ctx, src)
}

// T-966: the Engine.Add the add flow dispatches carries a deadline.
func TestAddCarriesADeadline(t *testing.T) {
	eng := &ctxEngine{Engine: newTestEngine(t)}
	src := engine.AddSource{Magnet: "magnet:?xt=urn:btih:abc123", SavePath: t.TempDir()}

	addTorrentCmd(eng, src, false, "x", "ix", "")()

	if !eng.hadDeadline {
		t.Fatal("Engine.Add got a context with no deadline")
	}
}

// T-9043: esc cancels a slow details-page fetch; the late result is dropped
// and no destination picker opens.
func TestEscCancelsAnInFlightResolve(t *testing.T) {
	ix := newCtxIndexer(true)
	m := New(newTestEngine(t), testTheme(), WithSearcher(newStubResolveSearcher(ix)))
	m.details = m.details.withResult(linklessResult())

	next, cmd := m.handleAddFromDetails()
	m = next.(Model)

	if !m.resolving() {
		t.Fatal("model not resolving after enter on a link-less result")
	}

	done := make(chan tea.Msg, 1)

	go func() { done <- cmd() }()

	<-ix.deadline // the fetch is running

	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)

	if m.resolving() {
		t.Fatal("still resolving after esc")
	}

	if got := m.statusBar.Message(); !strings.Contains(got, "add cancelled") {
		t.Fatalf("status = %q, want an add cancelled message", got)
	}

	var msg tea.Msg

	select {
	case msg = <-done: // the cancelled context released the blocked Resolve
	case <-time.After(5 * time.Second):
		t.Fatal("Resolve was not released by the cancel; the context was not passed down")
	}

	next, _ = m.Update(msg)
	m = next.(Model)

	if m.dest.open {
		t.Fatal("a cancelled resolve opened the destination picker")
	}

	if strings.Contains(m.statusBar.Message(), "couldn't add") {
		t.Fatalf("cancelled resolve reported an error: %q", m.statusBar.Message())
	}

	// The late error would queue behind "add cancelled", not replace it.
	for _, p := range m.statusBar.Pending() {
		if strings.Contains(p, "couldn't add") {
			t.Fatalf("cancelled resolve queued an error: %q", p)
		}
	}
}

// startTwo starts a details fetch, then a second one, and returns the model
// with the first fetch's generation.
func startTwo(t *testing.T) (m Model, oldGen int) {
	t.Helper()

	ix := newCtxIndexer(false)
	ix.deadline = make(chan time.Time, 4)
	m = New(newTestEngine(t), testTheme(), WithSearcher(newStubResolveSearcher(ix)))

	next, _ := m.startAdd(linklessResult())
	m = next.(Model)
	oldGen = m.resolveGen

	next, _ = m.startAdd(linklessResult())
	m = next.(Model)

	if m.resolveGen == oldGen {
		t.Fatal("a second fetch did not get a new generation")
	}

	return m, oldGen
}

// The superseded fetch's successful result must not continue into the add flow.
func TestSupersededResolveSuccessIsDropped(t *testing.T) {
	m, old := startTwo(t)

	r := linklessResult()
	r.Magnet, r.InfoHash = "magnet:?xt=urn:btih:old00", "old00"

	next, _ := m.Update(resolveResultMsg{gen: old, result: r})
	m = next.(Model)

	if m.dest.open {
		t.Fatal("a superseded resolve opened the destination picker")
	}

	if !m.resolving() {
		t.Fatal("a superseded result cleared the newer fetch")
	}
}

// A late error from the superseded fetch must not report or clear the newer one.
func TestSupersededResolveErrorIsDropped(t *testing.T) {
	m, old := startTwo(t)

	next, _ := m.Update(resolveResultMsg{gen: old, result: linklessResult(), err: errors.New("late failure")})
	m = next.(Model)

	if strings.Contains(m.statusBar.Message(), "couldn't add") {
		t.Fatalf("status = %q", m.statusBar.Message())
	}

	for _, p := range m.statusBar.Pending() {
		if strings.Contains(p, "couldn't add") {
			t.Fatalf("queued %q", p)
		}
	}

	if !m.resolving() {
		t.Fatal("a late error cleared the newer fetch")
	}
}

// T-971: with downloads active, ctrl+c over any text-entry modal opens the
// quit prompt; the prompt takes the keys (y quits and reaches no field) and
// esc closes it, keeping the modal.
func TestCtrlCPromptYieldsForEveryModal(t *testing.T) {
	cases := []struct {
		name  string
		open  func(t *testing.T) Model
		state func(Model) any
		is    func(Model) bool
	}{
		{
			"destination picker", func(t *testing.T) Model { return openPickerModel(t, 1) },
			func(m Model) any { return m.dest }, func(m Model) bool { return m.dest.open },
		},
		{"source form", func(t *testing.T) Model {
			m := openFormModel(t)
			m.activeDownloads = 1

			return m
		}, func(m Model) any { return *m.settings.form }, func(m Model) bool { return m.settings.form != nil }},
		{"preferences", func(t *testing.T) Model {
			m := New(newTestEngine(t), testTheme())
			m.activeDownloads = 1
			m.settings.prefsForm = &prefsForm{}

			return m
		}, func(m Model) any { return *m.settings.prefsForm }, func(m Model) bool { return m.settings.prefsForm != nil }},
		{"aggregator import", func(t *testing.T) Model {
			m := New(newTestEngine(t), testTheme())
			m.activeDownloads = 1
			ag := newAggregatorForm()
			m.settings.aggImport = &ag

			return m
		}, func(m Model) any { return *m.settings.aggImport }, func(m Model) bool { return m.settings.aggImport != nil }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.open(t)

			next, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
			m = next.(Model)

			if cmd != nil || !m.quitConfirm.IsOpen() {
				t.Fatalf("want the quit prompt open and no quit, got open=%v", m.quitConfirm.IsOpen())
			}

			before := tc.state(m)

			_, cmd = m.handleKey(keyRune("y"))
			if cmd == nil {
				t.Fatal("y on the quit prompt returned no command")
			}

			if _, ok := cmd().(tea.QuitMsg); !ok {
				t.Fatalf("y on the quit prompt returned %T, want tea.QuitMsg", cmd())
			}

			next, _ = m.handleKey(keyRune("y"))
			if !reflect.DeepEqual(before, tc.state(next.(Model))) {
				t.Fatal("y reached the modal's field")
			}

			next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
			m = next.(Model)

			if m.quitConfirm.IsOpen() || !tc.is(m) {
				t.Fatalf("esc should close the prompt and keep the modal: prompt=%v modal=%v", m.quitConfirm.IsOpen(), tc.is(m))
			}
		})
	}
}

// esc keeps its own meaning when nothing is being resolved.
func TestEscWithoutAResolveIsNotClaimed(t *testing.T) {
	m := New(newTestEngine(t), testTheme())
	m.screen = ScreenDetails

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if strings.Contains(next.(Model).statusBar.Message(), "add cancelled") {
		t.Fatal("esc reported a cancel with no resolve in flight")
	}
}

func openPickerModel(t *testing.T, active int) Model {
	t.Helper()

	m := New(newTestEngine(t), testTheme(), WithDownloadDir(t.TempDir()))
	m.activeDownloads = active
	m.width, m.height = 80, 24

	next, cmd := m.openDestinationPicker(indexer.Result{Title: "x.iso", Magnet: "magnet:?xt=urn:btih:abc123"})
	m = next.(Model)

	if !m.dest.open {
		t.Fatalf("picker not open (cmd %v)", cmd)
	}

	return m
}

// T-971: ctrl+c quits from the destination picker.
func TestCtrlCQuitsFromDestinationPicker(t *testing.T) {
	m := openPickerModel(t, 0)

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c in the destination picker returned no command")
	}

	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("ctrl+c returned %T, want tea.QuitMsg", cmd())
	}
}

// T-971: with downloads active the quit prompt opens over the picker, takes
// the keys, and esc returns to the picker.
func TestCtrlCInDestinationPickerPromptsWhenDownloadsAreActive(t *testing.T) {
	m := openPickerModel(t, 1)

	next, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = next.(Model)

	if cmd != nil || !m.quitConfirm.IsOpen() {
		t.Fatalf("want the quit prompt open and no quit, got open=%v cmd=%v", m.quitConfirm.IsOpen(), cmd)
	}

	if !strings.Contains(m.View(), "uit") || m.context() != ContextQuitConfirm {
		t.Fatalf("quit prompt is not the active modal: %v", m.context())
	}

	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)

	if m.quitConfirm.IsOpen() || !m.dest.open {
		t.Fatalf("esc should close the prompt and keep the picker: prompt=%v picker=%v", m.quitConfirm.IsOpen(), m.dest.open)
	}
}

func openFormModel(t *testing.T) Model {
	t.Helper()

	m := New(newTestEngine(t), testTheme())
	m.screen = ScreenSettings
	f := newAddForm()
	m.settings.form = &f

	return m
}

// T-971: ctrl+c quits from the add-source form, even with typed text.
func TestCtrlCQuitsFromSourceForm(t *testing.T) {
	m := openFormModel(t)
	m.settings.form.name = "typed"
	m.settings.form.dirty = true

	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c in the source form returned no command")
	}

	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("ctrl+c returned %T, want tea.QuitMsg", cmd())
	}
}

func focusImport(t *testing.T, f sourceForm) sourceForm {
	t.Helper()

	f.typ = "scraper"

	for i := range f.fields() {
		f.cursor = i
		if f.current() == fieldImport {
			return f
		}
	}

	t.Fatal("scraper form has no import field")

	return f
}

// T-9016: enter on an empty import field says what to type.
func TestEnterOnEmptyImportFieldShowsAHint(t *testing.T) {
	m := openFormModel(t)
	f := focusImport(t, *m.settings.form)
	m.settings.form = &f

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if got := m.statusBar.Message(); !strings.Contains(got, "file path or URL") {
		t.Fatalf("status = %q, want the import hint", got)
	}

	if m.settings.form.importing {
		t.Fatal("an empty import started")
	}
}

// T-9017: a waiting import fills the Name, so a blank Name is not flagged;
// without one it still is.
func TestBlankNameIsNotFlaggedWhileAnImportWaits(t *testing.T) {
	f := newAddForm()
	f.typ = "scraper"

	if issues := f.liveIssues(nil); len(issues) == 0 || issues[0] != "name is required" {
		t.Fatalf("no import text: issues = %v, want name is required first", issues)
	}

	f.importText = "https://example.org/defs/one.yml"

	for _, issue := range f.liveIssues(nil) {
		if strings.Contains(issue, "name is required") || strings.Contains(issue, "already used") {
			t.Fatalf("issues = %v, want no name or id hint while an import waits", f.liveIssues(nil))
		}
	}

	// With a source whose id is "source" (a blank name slugs to it), the
	// waiting import still raises no duplicate-id hint.
	taken := []config.Indexer{{ID: "source", Name: "Other", Type: "torznab", URL: "https://example.org/a"}}

	for _, issue := range f.liveIssues(taken) {
		if strings.Contains(issue, "already used") {
			t.Fatalf("issues = %v, want no duplicate-id hint while an import waits", f.liveIssues(taken))
		}
	}

	f.importText = ""

	found := false

	for _, issue := range f.liveIssues(taken) {
		found = found || strings.Contains(issue, "already used")
	}

	if !found {
		t.Fatal("control: the blank-name id collision is not reported without an import")
	}

	f.importText = "https://example.org/defs/one.yml"

	// A typed bad URL is still reported.
	f.sourceURL = "not-a-url"

	if len(f.liveIssues(nil)) == 0 {
		t.Fatal("an invalid URL was hidden by the pending import")
	}
}

// T-971: the preferences panel and the aggregator-import wizard swallow
// every key the same way, so ctrl+c quits from them too.
func TestCtrlCQuitsFromPreferencesAndAggregatorImport(t *testing.T) {
	for name, open := range map[string]func(Model) Model{
		"preferences": func(m Model) Model {
			m.settings.prefsForm = &prefsForm{}
			return m
		},
		"aggregator import": func(m Model) Model {
			ag := newAggregatorForm()
			m.settings.aggImport = &ag

			return m
		},
	} {
		m := open(New(newTestEngine(t), testTheme()))

		_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlC})
		if cmd == nil {
			t.Fatalf("%s: ctrl+c returned no command", name)
		}

		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%s: ctrl+c returned %T, want tea.QuitMsg", name, cmd())
		}
	}
}
