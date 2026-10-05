package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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

	msg := <-done // the cancelled context released the blocked Resolve

	next, _ = m.Update(msg)
	m = next.(Model)

	if m.dest.open {
		t.Fatal("a cancelled resolve opened the destination picker")
	}

	if strings.Contains(m.statusBar.Message(), "couldn't add") {
		t.Fatalf("cancelled resolve reported an error: %q", m.statusBar.Message())
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
