package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/config"
)

// saveImportModel returns a model whose open add form is a scraper (or
// torznab) form with importText typed and the cursor on Name, the state T-9052
// is about: text in the import field, cursor moved away without enter.
func saveImportModel(t *testing.T, sm *fakeSourceManager, typ, importText string) Model {
	t.Helper()

	m := New(newTestEngine(t), testTheme(), WithSourceManager(sm))
	f := newAddForm()
	f.typ = typ
	f.importText = importText
	f.name = ""
	f.sourceURL = "https://example.org"

	if typ == "torznab" {
		f.name = "Feed"
		f.apiKey = "k"
	}

	for i, fld := range f.fields() {
		if fld == fieldName {
			f.cursor = i
		}
	}

	m.settings.form = &f

	return m
}

// step sends msg and returns the new model and the command it produced.
func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()

	next, cmd := m.Update(msg)

	return next.(Model), cmd
}

// drain runs cmd and feeds every message it yields back in, following the
// commands those produce, until none is left.
func drain(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()

	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}

		m, cmd = step(t, m, msg)
	}

	return m
}

var (
	enterKey = tea.KeyMsg{Type: tea.KeyEnter}
	ctrlSKey = tea.KeyMsg{Type: tea.KeyCtrlS}
)

func TestSaveRunsPendingImportThenSaves(t *testing.T) {
	for name, key := range map[string]tea.KeyMsg{"enter": enterKey, "ctrl+s": ctrlSKey} {
		t.Run(name, func(t *testing.T) {
			sm := &fakeSourceManager{importID: "ex-def", importBaseURL: "https://example.org/base"}
			m := saveImportModel(t, sm, "scraper", " ~/defs/ex.yml ")
			m.settings.form.sourceURL = ""

			m, cmd := step(t, m, key)
			if cmd == nil {
				t.Fatal("save with pending import text returned no command")
			}

			if sm.saveCallCount() != 0 {
				t.Fatal("saved before the import finished")
			}

			m = drain(t, m, cmd)

			if len(sm.importedSources) != 1 || strings.HasPrefix(sm.importedSources[0], "~") || !strings.HasSuffix(sm.importedSources[0], "defs/ex.yml") {
				t.Fatalf("imported = %q, want one expanded, trimmed path", sm.importedSources)
			}

			saved := sm.savedSources()
			if len(saved) != 1 {
				t.Fatalf("saved %d sources, want 1: %#v", len(saved), saved)
			}

			if got := saved[0]; got.Definition != "ex-def.yml" || got.ID != "ex-def" || got.Name != "ex-def" || got.URL != "https://example.org/base" || got.Type != "scraper" {
				t.Fatalf("saved = %#v", got)
			}

			if m.settings.form != nil {
				t.Fatal("form still open after the save")
			}
		})
	}
}

func TestSaveImportFailureShowsErrorAndDoesNotSave(t *testing.T) {
	sm := &fakeSourceManager{importErr: errors.New("no such file")}
	m := saveImportModel(t, sm, "scraper", "/nope.yml")

	m, cmd := step(t, m, enterKey)
	m = drain(t, m, cmd)

	if sm.saveCallCount() != 0 {
		t.Fatal("saved after a failed import")
	}

	f := m.settings.form
	if f == nil || f.err != "import failed: no such file" || f.importing || f.saveAfterImport {
		t.Fatalf("form = %#v", f)
	}

	m.width, m.height = 100, 40

	if !strings.Contains(m.renderSourceForm(), "import failed: no such file") {
		t.Fatalf("import failure not rendered:\n%s", m.renderSourceForm())
	}

	// A later plain save does not re-run a stale flag: it imports afresh.
	sm.importErr = nil
	sm.importID = "ok"
	m, cmd = step(t, m, enterKey)
	m = drain(t, m, cmd)

	if sm.saveCallCount() != 1 || len(sm.importedSources) != 2 {
		t.Fatalf("saves=%d imports=%d", sm.saveCallCount(), len(sm.importedSources))
	}
}

func TestSaveWithDefinitionSetDoesNotImport(t *testing.T) {
	sm := &fakeSourceManager{importID: "other"}
	m := saveImportModel(t, sm, "scraper", "/ignored.yml")
	m.settings.form.name = "Mine"
	m.settings.form.definition = "mine.yml"

	m, cmd := step(t, m, enterKey)
	m = drain(t, m, cmd)

	if len(sm.importedSources) != 0 {
		t.Fatalf("imported %q although a definition was set", sm.importedSources)
	}

	if saved := sm.savedSources(); len(saved) != 1 || saved[0].Definition != "mine.yml" {
		t.Fatalf("saved = %#v", saved)
	}
}

func TestSaveTorznabIgnoresImportText(t *testing.T) {
	sm := &fakeSourceManager{importID: "other"}
	m := saveImportModel(t, sm, "torznab", "/leftover.yml")

	_, cmd := step(t, m, enterKey)
	drain(t, m, cmd)

	if len(sm.importedSources) != 0 {
		t.Fatalf("torznab save imported %q", sm.importedSources)
	}

	if saved := sm.savedSources(); len(saved) != 1 || saved[0].Type != "torznab" || saved[0].Definition != "" {
		t.Fatalf("saved = %#v", saved)
	}
}

func TestSaveBlankImportTextStillNeedsDefinition(t *testing.T) {
	sm := &fakeSourceManager{}
	m := saveImportModel(t, sm, "scraper", "   ")
	m.settings.form.name = "Mine"

	m, cmd := step(t, m, enterKey)
	if cmd != nil {
		m = drain(t, m, cmd)
	}

	if len(sm.importedSources) != 0 || sm.saveCallCount() != 0 {
		t.Fatal("blank import text triggered an import or a save")
	}

	if m.settings.form.err != errNeedsDefinition {
		t.Fatalf("err = %q", m.settings.form.err)
	}
}

func TestSecondSaveWhileImportPendingIsRefused(t *testing.T) {
	sm := &fakeSourceManager{importID: "ex-def"}
	m := saveImportModel(t, sm, "scraper", "/ex.yml")

	m, first := step(t, m, enterKey)
	m, second := step(t, m, ctrlSKey)
	m, third := step(t, m, enterKey)

	if second != nil || third != nil {
		t.Fatal("a save while the import was in flight dispatched a command")
	}

	m = drain(t, m, first)

	if len(sm.importedSources) != 1 || sm.saveCallCount() != 1 {
		t.Fatalf("imports=%d saves=%d, want 1 and 1", len(sm.importedSources), sm.saveCallCount())
	}

	if rows := m.sourceRows(); len(rows) != 1 {
		t.Fatalf("snapshot = %#v", rows)
	}
}

func TestPendingImportResultAfterEscDoesNotSave(t *testing.T) {
	sm := &fakeSourceManager{importID: "ex-def"}
	m := saveImportModel(t, sm, "scraper", "/ex.yml")

	m, cmd := step(t, m, enterKey)
	m.settings.form = nil // esc (and confirmed discard) while the import runs

	m = drain(t, m, cmd)

	if sm.saveCallCount() != 0 || m.settings.form != nil {
		t.Fatal("a closed form was saved or reopened by a late import result")
	}
}

func TestEnterOnImportFieldStillOnlyImports(t *testing.T) {
	sm := &fakeSourceManager{importID: "ex-def"}
	m := saveImportModel(t, sm, "scraper", "/ex.yml")

	for i, fld := range m.settings.form.fields() {
		if fld == fieldImport {
			m.settings.form.cursor = i
		}
	}

	m, cmd := step(t, m, enterKey)
	m = drain(t, m, cmd)

	if sm.saveCallCount() != 0 {
		t.Fatal("enter on the import field saved")
	}

	f := m.settings.form
	if f == nil || f.definition != "ex-def.yml" || f.info != "imported: ex-def" || f.importing {
		t.Fatalf("form = %#v", f)
	}
}

func TestSaveImportWithTakenIDDoesNotSave(t *testing.T) {
	sm := &fakeSourceManager{importID: "dup", sources: []config.Indexer{{ID: "dup", Name: "Old", Type: "torznab", URL: "https://example.org/x"}}}
	m := saveImportModel(t, sm, "scraper", "/ex.yml")
	m.sourcesSnapshot = sm.Sources()

	m, cmd := step(t, m, enterKey)
	m = drain(t, m, cmd)

	if sm.saveCallCount() != 0 || m.settings.form == nil || !strings.Contains(m.settings.form.err, "already used") {
		t.Fatalf("saves=%d form=%#v", sm.saveCallCount(), m.settings.form)
	}
}
