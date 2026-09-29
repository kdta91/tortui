package tui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func touch(t *testing.T, elems ...string) {
	t.Helper()

	p := filepath.Join(elems...)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

// importFormModel returns a Model with the scraper add form open, its cursor
// on the import field.
func importFormModel(t *testing.T, text, defsDir string) Model {
	t.Helper()

	m := New(newTestEngine(t), testTheme(), WithDefinitionsDir(defsDir))
	f := newAddForm()
	f.typ = "scraper"
	f.importText = text

	for i, fld := range f.fields() {
		if fld == fieldImport {
			f.cursor = i
		}
	}

	m.settings.form = &f

	return m
}

// pressKey sends one key and, when it returns a completion read, runs it and
// feeds the result back, the way the runtime would.
func pressKey(t *testing.T, m Model, key tea.KeyMsg) Model {
	t.Helper()

	next, cmd := m.Update(key)
	m = next.(Model)

	if cmd != nil {
		if msg, ok := cmd().(formCompleteMsg); ok {
			next, _ = m.Update(msg)
			m = next.(Model)
		}
	}

	return m
}

var tabKey = tea.KeyMsg{Type: tea.KeyTab}

func TestCompletionUniqueMatch(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "alpha.yml")
	touch(t, dir, "notes.txt")

	m := importFormModel(t, filepath.Join(dir, "al"), "")
	m = pressKey(t, m, tabKey)

	if got, want := m.settings.form.importText, filepath.Join(dir, "alpha.yml"); got != want {
		t.Fatalf("importText = %q, want %q", got, want)
	}
}

func TestCompletionOffersOnlyDirsAndYAML(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "a.yml")
	touch(t, dir, "b.yaml")
	touch(t, dir, "c.txt")
	touch(t, dir, "sub", "x.yml")

	sep := string(filepath.Separator)
	got := completionMatches(dir+sep, "")
	want := []string{dir + sep + "a.yml", dir + sep + "b.yaml", dir + sep + "sub" + sep}

	if len(got) != len(want) {
		t.Fatalf("matches = %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("matches = %v, want %v", got, want)
		}
	}
}

func TestCompletionCommonPrefixThenCycles(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "source-a.yml")
	touch(t, dir, "source-b.yml")

	m := importFormModel(t, filepath.Join(dir, "s"), "")

	for i, want := range []string{"source-", "source-a.yml", "source-b.yml", "source-a.yml"} {
		m = pressKey(t, m, tabKey)

		if got := m.settings.form.importText; got != filepath.Join(dir, want) {
			t.Fatalf("tab %d: text = %q, want %q", i+1, got, filepath.Join(dir, want))
		}
	}

	if m.settings.form.current() != fieldImport {
		t.Fatal("cycling must not leave the import field")
	}
}

func TestCompletionNoMatchMovesToNextField(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "alpha.yml")

	m := importFormModel(t, filepath.Join(dir, "zzz"), "")
	m = pressKey(t, m, tabKey)

	if m.settings.form.current() != fieldID {
		t.Fatalf("current = %v, want fieldID (next field)", m.settings.form.current())
	}

	if m.settings.form.importText != filepath.Join(dir, "zzz") {
		t.Fatalf("text changed: %q", m.settings.form.importText)
	}
}

func TestCompletionFullyTypedFileMovesOn(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "alpha.yml")

	m := importFormModel(t, filepath.Join(dir, "alpha.yml"), "")
	m = pressKey(t, m, tabKey)

	if m.settings.form.current() != fieldID {
		t.Fatalf("current = %v, want next field", m.settings.form.current())
	}
}

func TestCompletionShiftTabAlwaysMovesBack(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "alpha.yml")

	m := importFormModel(t, filepath.Join(dir, "al"), "")
	m = pressKey(t, m, tea.KeyMsg{Type: tea.KeyShiftTab})

	if m.settings.form.current() != fieldDefinition {
		t.Fatalf("current = %v, want fieldDefinition", m.settings.form.current())
	}
}

func TestCompletionExpandsHome(t *testing.T) {
	home := t.TempDir()
	touch(t, home, "defs", "mine.yml")

	old := userHomeDir
	userHomeDir = func() (string, error) { return home, nil }

	t.Cleanup(func() { userHomeDir = old })

	m := importFormModel(t, "~/defs/mi", "")
	m = pressKey(t, m, tabKey)

	if got := m.settings.form.importText; got != "~/defs/mine.yml" {
		t.Fatalf("importText = %q, want ~/defs/mine.yml (spelling preserved)", got)
	}

	if got, want := expandHome("~/defs/mine.yml"), filepath.Join(home, "defs", "mine.yml"); got != want {
		t.Fatalf("expandHome = %q, want %q", got, want)
	}

	if got := expandHome("~other/x"); got != "~other/x" {
		t.Fatalf("expandHome touched ~other: %q", got)
	}
}

func TestCompletionEmptyStartsInDefinitionsDir(t *testing.T) {
	defs := t.TempDir()
	touch(t, defs, "only.yml")

	m := importFormModel(t, "", defs)
	m = pressKey(t, m, tabKey)

	if got, want := m.settings.form.importText, filepath.Join(defs, "only.yml"); got != want {
		t.Fatalf("importText = %q, want %q", got, want)
	}
}

func TestCompletionEmptyWithoutDefinitionsDirMovesOn(t *testing.T) {
	m := importFormModel(t, "", "")
	m = pressKey(t, m, tabKey)

	if m.settings.form.current() != fieldID {
		t.Fatalf("current = %v, want next field", m.settings.form.current())
	}
}

func TestCompletionNeverCompletesURLs(t *testing.T) {
	if got := completionMatches("https://example.org/de", t.TempDir()); got != nil {
		t.Fatalf("matches = %v, want none", got)
	}

	m := importFormModel(t, "https://example.org/de", "")

	next, cmd := m.Update(tabKey)
	m = next.(Model)

	if cmd != nil {
		t.Fatal("tab on a URL must not start a directory read")
	}

	if m.settings.form.current() != fieldID {
		t.Fatalf("current = %v, want next field", m.settings.form.current())
	}
}

func TestCompletionStaleResultIsDropped(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "alpha.yml")

	m := importFormModel(t, filepath.Join(dir, "al"), "")

	next, cmd := m.Update(tabKey)
	m = next.(Model)

	if cmd == nil {
		t.Fatal("expected a completion read command")
	}

	// The user types before the read returns.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = next.(Model)
	typed := m.settings.form.importText

	next, _ = m.Update(cmd())
	m = next.(Model)

	if m.settings.form.importText != typed {
		t.Fatalf("stale completion applied: %q", m.settings.form.importText)
	}

	if m.settings.form.current() != fieldImport {
		t.Fatal("stale completion must not move the cursor either")
	}
}
