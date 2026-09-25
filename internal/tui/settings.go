// Settings screen (T-080): indexer management, so that — per this task's own
// acceptance text — "a user must never have to open the config file."
// Everything about a source (add, edit, test-before-save, enable/disable,
// remove, reload scraper definitions) is reachable from here.
//
// internal/tui may import indexer's frozen domain types but never a
// concrete adapter (AGENT.md §4): SourceManager below is the seam this
// screen actually talks to — satisfied by a composition-root type in
// production and by a fake in settings_test.go — the same pattern
// Searcher/HistoryStore/TorrentStore already established for the other
// screens. config.Indexer is plain data (like store.HistoryEntry and
// store.TorrentRecord already carried into this package), not a concrete
// adapter, so importing it directly here does not violate that rule.
//
// The richer connection-test outcome classification (reachable / auth
// failed / parse failed / timeout, run as a cancellable tea.Cmd) is T-081;
// this task only needs a single pass/fail probe wired to the same key, so
// a bad URL or key is caught before Save rather than at the next search.
package tui

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/tui/components"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// SourceManager is the subset of persistence and live-registry machinery
// the settings screen needs. A concrete implementation lives in the
// composition root (internal/app), which is the only place that may import
// the torznab/scraper adapter packages and internal/config's file-writing
// Save (AGENT.md §4) — this package only ever sees the interface.
type SourceManager interface {
	// Sources returns the current indexer configuration, in the order the
	// list view shows them. Called fresh whenever the settings screen
	// needs to redraw its list — the settings screen keeps no cache of
	// its own between an edit and the next Sources() call.
	Sources() []config.Indexer

	// SaveSources persists sources as the full replacement configured-
	// source set and re-syncs the live search registry to match (T-080
	// acceptance: "reloads the registry live — no restart"). It is the
	// only path that writes indexer configuration to disk.
	SaveSources(sources []config.Indexer) error

	// TestSource runs a short, cancellable probe against src without
	// requiring it to already be saved. Returns nil on success.
	TestSource(ctx context.Context, src config.Indexer) error

	// ImportDefinition installs a scraper definition file from a local
	// path or an http(s) URL (T-025) and returns its id, so the add form
	// can pre-fill Definition/ID from it.
	ImportDefinition(ctx context.Context, source string) (id string, err error)

	// ReloadDefinitions re-reads every scraper definition file from disk
	// (T-023's Loader.Reload) — the `r` key.
	ReloadDefinitions() error
}

// Option wiring for the settings screen.

// WithSourceManager wires sm as the settings screen's source of truth.
// Without it, the settings screen renders an empty list and every action
// reports "no source manager configured" via the status bar.
func WithSourceManager(sm SourceManager) Option {
	return func(m *Model) { m.sources = sm }
}

// sourceFormField is one field of the add/edit form. Which of these are
// visible depends on the form's current Type (AGENT.md/T-080: "fields
// irrelevant to the selected type are hidden, not greyed out").
type sourceFormField int

const (
	fieldName sourceFormField = iota
	fieldType
	fieldURL
	fieldAPIKey
	fieldCookie
	fieldDefinition
	fieldImport
	fieldID
)

// visibleFields returns the fields the form shows for typ ("torznab" or
// "scraper"), in the order arrow/tab navigation moves through them.
// Definition and Import are scraper-only; ID is last since it is an
// override nobody has to touch.
func visibleFields(typ string) []sourceFormField {
	base := []sourceFormField{fieldName, fieldType, fieldURL, fieldAPIKey, fieldCookie}
	if typ == "scraper" {
		base = append(base, fieldDefinition, fieldImport)
	}

	return append(base, fieldID)
}

func (f sourceFormField) label() string {
	switch f {
	case fieldName:
		return "Name"
	case fieldType:
		return "Type"
	case fieldURL:
		return "URL"
	case fieldAPIKey:
		return "API key"
	case fieldCookie:
		return "Cookie"
	case fieldDefinition:
		return "Definition file"
	case fieldImport:
		return "Import from (path or URL)"
	case fieldID:
		return "ID (override)"
	default:
		return ""
	}
}

// sourceForm is the add/edit modal's own state (ContextSourceForm).
type sourceForm struct {
	// editingID is the source being edited (its original id), or "" when
	// this is an add rather than an edit.
	editingID string

	name       string
	typ        string // "torznab" or "scraper"
	sourceURL  string
	apiKey     string
	cookie     string
	definition string
	idOverride string
	importText string

	cursor  int
	reveal  bool
	dirty   bool
	testing bool
	testGen int

	// confirmDiscard is true while esc on a dirty form is asking to
	// confirm discarding it (T-080: "confirm before discarding a dirty
	// form").
	confirmDiscard bool

	// err is the most recent validation/test/save/import failure, shown
	// inline until the next edit or attempt.
	err string
	// info is a transient success note (e.g. "imported: <id>").
	info string
}

// newAddForm returns a blank add form.
func newAddForm() sourceForm {
	return sourceForm{typ: "torznab"}
}

// newEditForm returns a form pre-filled from an existing source.
func newEditForm(src config.Indexer) sourceForm {
	return sourceForm{
		editingID:  src.ID,
		name:       src.Name,
		typ:        src.Type,
		sourceURL:  src.URL,
		apiKey:     src.APIKey,
		cookie:     src.Cookie, // gitleaks:allow — a field copy, never a literal credential
		definition: src.Definition,
		idOverride: src.ID,
	}
}

// fields returns this form's currently visible fields.
func (f sourceForm) fields() []sourceFormField { return visibleFields(f.typ) }

// clampCursor keeps cursor in range after Type changes the visible set.
func (f sourceForm) clampCursor() sourceForm {
	n := len(f.fields())
	if f.cursor >= n {
		f.cursor = n - 1
	}
	if f.cursor < 0 {
		f.cursor = 0
	}

	return f
}

// current returns the field the cursor is on.
func (f sourceForm) current() sourceFormField {
	fs := f.fields()
	if len(fs) == 0 {
		return fieldName
	}

	return fs[f.cursor]
}

// fieldValue returns the raw text of field f.
func (f sourceForm) fieldValue(field sourceFormField) string {
	switch field {
	case fieldName:
		return f.name
	case fieldURL:
		return f.sourceURL
	case fieldAPIKey:
		return f.apiKey
	case fieldCookie:
		return f.cookie
	case fieldDefinition:
		return f.definition
	case fieldImport:
		return f.importText
	case fieldID:
		return f.idOverride
	default:
		return ""
	}
}

// setFieldValue returns a copy of f with field set to v.
func (f sourceForm) setFieldValue(field sourceFormField, v string) sourceForm {
	switch field {
	case fieldName:
		f.name = v
	case fieldURL:
		f.sourceURL = v
	case fieldAPIKey:
		f.apiKey = v
	case fieldCookie:
		f.cookie = v
	case fieldDefinition:
		f.definition = v
	case fieldImport:
		f.importText = v
	case fieldID:
		f.idOverride = v
	}

	return f
}

// apikeyQueryKeys are the query parameter names a pasted Torznab feed URL
// commonly carries its API key under.
var apikeyQueryKeys = []string{"apikey", "api_key"}

// splitTorznabURL detects a URL that already embeds an API key as a query
// parameter and returns the URL with that parameter stripped plus the key
// itself. ok is false when raw does not parse as a URL with a recognised
// key parameter, in which case raw is returned unchanged.
func splitTorznabURL(raw string) (stripped, key string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.RawQuery == "" {
		return raw, "", false
	}

	q := u.Query()

	for _, name := range apikeyQueryKeys {
		v := q.Get(name)
		if v == "" {
			continue
		}

		q.Del(name)
		u.RawQuery = q.Encode()

		return u.String(), v, true
	}

	return raw, "", false
}

// slugify turns name into an id: lowercase, non-alphanumeric runs become a
// single '-', leading/trailing '-' trimmed. An empty or all-punctuation
// name falls back to "source" so callers always get a usable id.
func slugify(name string) string {
	var b strings.Builder

	prevDash := false

	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}

	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "source"
	}

	return out
}

// resolvedID is the id this form will save under: the ID override if the
// user typed one, otherwise slugify(name).
func (f sourceForm) resolvedID() string {
	if strings.TrimSpace(f.idOverride) != "" {
		return strings.TrimSpace(f.idOverride)
	}

	return slugify(f.name)
}

// toIndexer builds the config.Indexer this form would save.
func (f sourceForm) toIndexer() config.Indexer {
	return config.Indexer{
		ID:         f.resolvedID(),
		Name:       strings.TrimSpace(f.name),
		Type:       f.typ,
		URL:        strings.TrimSpace(f.sourceURL),
		APIKey:     f.apiKey,
		Cookie:     f.cookie, // gitleaks:allow — a field copy, never a literal credential
		Definition: f.definition,
		Enabled:    true,
	}
}

// validate checks the form's own required fields, ahead of the duplicate-id
// check that needs the rest of the source list (see handleSourceFormSave).
func (f sourceForm) validate() string {
	if strings.TrimSpace(f.name) == "" {
		return "name is required"
	}

	if strings.TrimSpace(f.sourceURL) == "" {
		return "URL is required"
	}

	if f.typ == "scraper" && strings.TrimSpace(f.definition) == "" {
		return "a scraper source needs a definition file (or import one)"
	}

	return ""
}

// settingsModel is ScreenSettings' own state: the list cursor, an open
// add/edit form (nil when the list itself has focus), and the remove
// confirmation.
type settingsModel struct {
	cursor        int
	form          *sourceForm
	removeConfirm components.Dialog
	removeID      string
}

func newSettingsModel() settingsModel {
	return settingsModel{removeConfirm: newSourceRemoveDialog()}
}

func newSourceRemoveDialog() components.Dialog {
	return components.NewDialog("Remove source?", "", []components.DialogOption{
		0: {Label: "Remove"},
		1: {Label: "Cancel"},
	}, 1)
}

const (
	sourceRemoveDialogRemove = 0
	sourceRemoveDialogCancel = 1
)

// sourceRows returns the sources this screen lists, from the SourceManager,
// or nil when none is wired.
func (m Model) sourceRows() []config.Indexer {
	if m.sources == nil {
		return nil
	}

	return m.sources.Sources()
}

// lastOutcome reports the most recent search fan-out's outcome for
// indexerID: "ok", "failed: <reason>", or "" when it was never queried
// (root.go already tracks lastQueriedIDs/lastSourceErrs for the status
// bar's own source-error display — this reuses that same hand-off rather
// than tracking a second copy of it).
func (m Model) lastOutcome(indexerID string) string {
	queried := false

	for _, id := range m.lastQueriedIDs {
		if id == indexerID {
			queried = true
			break
		}
	}

	if !queried {
		return ""
	}

	for _, se := range m.lastSourceErrs {
		if se.IndexerID == indexerID {
			return "failed: " + se.Err.Error()
		}
	}

	return "ok"
}

// handleSettingsMoveCursor moves the list cursor by delta, clamped.
func (m Model) handleSettingsMoveCursor(delta int) (tea.Model, tea.Cmd) {
	n := len(m.sourceRows())
	if n == 0 {
		m.settings.cursor = 0
		return m, nil
	}

	m.settings.cursor += delta
	if m.settings.cursor < 0 {
		m.settings.cursor = 0
	}
	if m.settings.cursor > n-1 {
		m.settings.cursor = n - 1
	}

	return m, nil
}

// selectedSource returns the row the settings cursor points at.
func (m Model) selectedSource() (config.Indexer, bool) {
	rows := m.sourceRows()
	if m.settings.cursor < 0 || m.settings.cursor >= len(rows) {
		return config.Indexer{}, false
	}

	return rows[m.settings.cursor], true
}

// handleSourceAdd opens a blank add form ("a").
func (m Model) handleSourceAdd() (tea.Model, tea.Cmd) {
	if m.sources == nil {
		return m.pushStatus("no source manager configured")
	}

	f := newAddForm()
	m.settings.form = &f

	return m, nil
}

// handleSourceEdit opens an edit form pre-filled from the selected row
// ("e").
func (m Model) handleSourceEdit() (tea.Model, tea.Cmd) {
	src, ok := m.selectedSource()
	if !ok {
		return m.pushStatus("no source selected")
	}

	f := newEditForm(src)
	m.settings.form = &f

	return m, nil
}

// handleSourceToggleEnabled flips the selected row's Enabled bit and saves
// immediately (space).
func (m Model) handleSourceToggleEnabled() (tea.Model, tea.Cmd) {
	src, ok := m.selectedSource()
	if !ok || m.sources == nil {
		return m, nil
	}

	all := append([]config.Indexer(nil), m.sourceRows()...)
	for i := range all {
		if all[i].ID == src.ID {
			all[i].Enabled = !all[i].Enabled
		}
	}

	return m, saveSourcesCmd(m.sources, all)
}

// handleSourceRemove opens the remove confirmation ("x").
func (m Model) handleSourceRemove() (tea.Model, tea.Cmd) {
	src, ok := m.selectedSource()
	if !ok {
		return m.pushStatus("no source selected")
	}

	m.settings.removeID = src.ID
	m.settings.removeConfirm.Message = fmt.Sprintf("Remove %q?", src.Name)
	m.settings.removeConfirm = m.settings.removeConfirm.Open()

	return m, nil
}

// handleSourceRemoveConfirmAction routes a key while ContextSourceRemoveConfirm
// is open.
func (m Model) handleSourceRemoveConfirmAction(action Action) (tea.Model, tea.Cmd) {
	switch action {
	case ActionMoveDown:
		m.settings.removeConfirm = m.settings.removeConfirm.MoveNext()
	case ActionMoveUp:
		m.settings.removeConfirm = m.settings.removeConfirm.MovePrev()
	case ActionCancel, ActionConfirmNo:
		m.settings.removeConfirm = m.settings.removeConfirm.Cancel()
		m.settings.removeID = ""
	case ActionConfirmYes:
		var choice int
		m.settings.removeConfirm, choice = m.settings.removeConfirm.Confirm()
		id := m.settings.removeID
		m.settings.removeID = ""

		if choice != sourceRemoveDialogRemove || id == "" || m.sources == nil {
			return m, nil
		}

		var remaining []config.Indexer
		for _, s := range m.sourceRows() {
			if s.ID != id {
				remaining = append(remaining, s)
			}
		}

		return m, saveSourcesCmd(m.sources, remaining)
	}

	return m, nil
}

// reloadResultMsg reports ReloadDefinitions' outcome.
type reloadResultMsg struct{ err error }

// reloadDefinitionsCmd runs off Update's own goroutine (AGENT.md §6.1):
// re-reading every scraper definition file is disk I/O, and Update must
// never block on it.
func reloadDefinitionsCmd(sm SourceManager) tea.Cmd {
	return func() tea.Msg {
		return reloadResultMsg{err: sm.ReloadDefinitions()}
	}
}

// handleSourceReload implements `r`: re-read every scraper definition file
// from disk (T-023).
func (m Model) handleSourceReload() (tea.Model, tea.Cmd) {
	if m.sources == nil {
		return m.pushStatus("no source manager configured")
	}

	return m, reloadDefinitionsCmd(m.sources)
}

// handleReloadResult applies ReloadDefinitions' outcome.
func (m Model) handleReloadResult(msg reloadResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m.pushStatus(fmt.Sprintf("reload failed: %v", msg.err))
	}

	return m.pushStatus("definitions reloaded")
}

// handleSourceTest implements `t` on the list: a quick probe against the
// already-saved selected source.
func (m Model) handleSourceTest() (tea.Model, tea.Cmd) {
	src, ok := m.selectedSource()
	if !ok || m.sources == nil {
		return m.pushStatus("no source selected")
	}

	updated, cmd := m.pushStatus("testing " + src.Name + "…")

	return updated, tea.Batch(cmd, testSourceCmd(m.sources, src, m.settings.cursor))
}

// sourcesSaveResultMsg reports SaveSources' outcome for the list's own
// toggle-enabled and remove actions (the add/edit form's own save uses
// formSaveResultMsg instead, since it needs to close the form on success).
type sourcesSaveResultMsg struct {
	err error
}

func saveSourcesCmd(sm SourceManager, sources []config.Indexer) tea.Cmd {
	return func() tea.Msg {
		return sourcesSaveResultMsg{err: sm.SaveSources(sources)}
	}
}

// formSaveResultMsg reports the add/edit form's own save attempt.
type formSaveResultMsg struct{ err error }

func saveFormCmd(sm SourceManager, sources []config.Indexer) tea.Cmd {
	return func() tea.Msg {
		return formSaveResultMsg{err: sm.SaveSources(sources)}
	}
}

// sourceTestResultMsg reports one `t` probe's outcome. gen ties it to
// whichever list row was selected when it was dispatched, in the list
// context; the form uses formTestResultMsg instead.
type sourceTestResultMsg struct {
	name string
	err  error
}

func testSourceCmd(sm SourceManager, src config.Indexer, _ int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), sourceTestTimeout)
		defer cancel()

		return sourceTestResultMsg{name: src.Name, err: sm.TestSource(ctx, src)}
	}
}

// formTestResultMsg reports the add/edit form's own test-before-save probe.
type formTestResultMsg struct {
	gen int
	err error
}

// sourceTestTimeout bounds one probe. The richer, distinct-outcome
// classification (reachable / auth failed / parse failed / timeout) is
// T-081; this is only the short-timeout pass/fail this task needs.
const sourceTestTimeout = 15 * 1e9 // 15 seconds, spelled as nanoseconds to avoid importing time solely for this constant

func testFormCmd(sm SourceManager, src config.Indexer, gen int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), sourceTestTimeout)
		defer cancel()

		return formTestResultMsg{gen: gen, err: sm.TestSource(ctx, src)}
	}
}

// formImportResultMsg reports the form's import-a-definition attempt.
type formImportResultMsg struct {
	id  string
	err error
}

func importDefinitionCmd(sm SourceManager, source string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), sourceTestTimeout)
		defer cancel()

		id, err := sm.ImportDefinition(ctx, source)

		return formImportResultMsg{id: id, err: err}
	}
}

// handleSourcesSaveResult applies a toggle-enabled or remove's outcome.
func (m Model) handleSourcesSaveResult(msg sourcesSaveResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m.pushStatus(fmt.Sprintf("couldn't save sources: %v", msg.err))
	}

	return m.handleSettingsMoveCursor(0)
}

// handleFormSaveResult applies the add/edit form's own save attempt: closes
// the form on success, keeps it open with the error shown otherwise.
func (m Model) handleFormSaveResult(msg formSaveResultMsg) (tea.Model, tea.Cmd) {
	if m.settings.form == nil {
		return m, nil
	}

	if msg.err != nil {
		m.settings.form.err = msg.err.Error()
		return m, nil
	}

	m.settings.form = nil

	return m.pushStatus("source saved")
}

// handleSourceTestResult applies the list's own `t` probe outcome.
func (m Model) handleSourceTestResult(msg sourceTestResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m.pushStatus(fmt.Sprintf("%s: test failed: %v", msg.name, msg.err))
	}

	return m.pushStatus(msg.name + ": test ok")
}

// handleFormTestResult applies the form's test-before-save outcome. Stale
// (superseded) results are dropped the same generation-guard way search.go
// already does.
func (m Model) handleFormTestResult(msg formTestResultMsg) (tea.Model, tea.Cmd) {
	if m.settings.form == nil || msg.gen != m.settings.form.testGen {
		return m, nil
	}

	m.settings.form.testing = false

	if msg.err != nil {
		m.settings.form.err = "test failed: " + msg.err.Error()
		m.settings.form.info = ""
	} else {
		m.settings.form.err = ""
		m.settings.form.info = "test ok"
	}

	return m, nil
}

// handleFormImportResult applies the form's import attempt: pre-fills
// Definition (and Name/ID when still blank) from the imported id.
func (m Model) handleFormImportResult(msg formImportResultMsg) (tea.Model, tea.Cmd) {
	if m.settings.form == nil {
		return m, nil
	}

	f := m.settings.form

	if msg.err != nil {
		f.err = "import failed: " + msg.err.Error()
		f.info = ""

		return m, nil
	}

	f.definition = msg.id + ".yml"
	if strings.TrimSpace(f.name) == "" {
		f.name = msg.id
	}
	if strings.TrimSpace(f.idOverride) == "" {
		f.idOverride = msg.id
	}

	f.importText = ""
	f.err = ""
	f.info = "imported: " + msg.id
	f.dirty = true

	return m, nil
}

// handleSourceFormKey handles every key while the add/edit form
// (ContextSourceForm) is open. Typing on a text field is claimed first, the
// same "dedicated handler owns the whole modal" pattern destination.go's
// handleDestinationKey already established, since almost every key means
// "type this character" here rather than a global hotkey.
func (m Model) handleSourceFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := *m.settings.form

	if f.confirmDiscard {
		switch msg.String() {
		case "y", "enter":
			m.settings.form = nil
		case "n", "esc":
			f.confirmDiscard = false
			m.settings.form = &f
		}

		return m, nil
	}

	switch msg.String() {
	case "esc":
		if f.dirty {
			f.confirmDiscard = true
			m.settings.form = &f

			return m, nil
		}

		m.settings.form = nil

		return m, nil

	case "tab":
		f = f.clampCursor()
		f.cursor = (f.cursor + 1) % len(f.fields())
		m.settings.form = &f

		return m, nil

	case "shift+tab":
		f = f.clampCursor()
		n := len(f.fields())
		f.cursor = (f.cursor - 1 + n) % n
		m.settings.form = &f

		return m, nil

	case "ctrl+r":
		f.reveal = !f.reveal
		m.settings.form = &f

		return m, nil

	case "ctrl+s":
		return m.handleSourceFormSave(f)

	case "enter":
		// On the import field, enter runs the import instead of saving —
		// ctrl+i is not usable here (it is literally the same key as tab
		// on every real terminal: both send ASCII 0x09), so this field
		// gets its own dedicated trigger rather than one that would
		// silently double as "next field."
		if f.current() == fieldImport {
			if strings.TrimSpace(f.importText) == "" {
				return m, nil
			}

			f.err = ""
			f.info = ""
			m.settings.form = &f

			return m, importDefinitionCmd(m.sources, f.importText)
		}

		return m.handleSourceFormSave(f)

	case "ctrl+t":
		return m.handleSourceFormTest(f)

	case "left", "right", " ":
		if f.current() == fieldType {
			if f.typ == "torznab" {
				f.typ = "scraper"
			} else {
				f.typ = "torznab"
			}

			f.dirty = true
			f = f.clampCursor()
			m.settings.form = &f

			return m, nil
		}
	}

	switch msg.Type {
	case tea.KeyRunes:
		field := f.current()
		if field == fieldType {
			return m, nil
		}

		text := string(msg.Runes)
		f = f.setFieldValue(field, f.fieldValue(field)+text)
		f.dirty = true

		// T-080: "pasting a complete Torznab feed URL that already
		// contains ?apikey=... splits it automatically." A paste arrives
		// as one multi-rune KeyRunes event; a single typed character
		// never triggers this, so ordinary typing of a URL with no key in
		// it behaves exactly as before.
		if field == fieldURL && len(msg.Runes) > 1 {
			if stripped, key, ok := splitTorznabURL(f.sourceURL); ok {
				f.sourceURL = stripped
				f.apiKey = key
			}
		}

		m.settings.form = &f

		return m, nil

	case tea.KeyBackspace:
		field := f.current()
		if field == fieldType {
			return m, nil
		}

		f = f.setFieldValue(field, trimLastRune(f.fieldValue(field)))
		f.dirty = true
		m.settings.form = &f

		return m, nil
	}

	return m, nil
}

// handleSourceFormSave validates f, checks its resolved id for a collision
// with every other configured source, and — only once both pass — persists
// it via SaveSources.
func (m Model) handleSourceFormSave(f sourceForm) (tea.Model, tea.Cmd) {
	if reason := f.validate(); reason != "" {
		f.err = reason
		m.settings.form = &f

		return m, nil
	}

	id := f.resolvedID()

	all := append([]config.Indexer(nil), m.sourceRows()...)

	var replaced bool

	for i, s := range all {
		if s.ID == id && s.ID != f.editingID {
			f.err = fmt.Sprintf("id %q is already used by another source", id)
			m.settings.form = &f

			return m, nil
		}

		if s.ID == f.editingID && f.editingID != "" {
			all[i] = f.toIndexerWithID(id)
			replaced = true
		}
	}

	if !replaced {
		all = append(all, f.toIndexerWithID(id))
	}

	if m.sources == nil {
		f.err = "no source manager configured"
		m.settings.form = &f

		return m, nil
	}

	f.err = ""
	m.settings.form = &f

	return m, saveFormCmd(m.sources, all)
}

// toIndexerWithID is toIndexer with id substituted for the resolved id
// already computed by the caller, so it is computed exactly once per save.
func (f sourceForm) toIndexerWithID(id string) config.Indexer {
	ix := f.toIndexer()
	ix.ID = id

	return ix
}

// handleSourceFormTest runs the test-before-save probe (T-080 acceptance:
// "t tests the source before saving").
func (m Model) handleSourceFormTest(f sourceForm) (tea.Model, tea.Cmd) {
	if reason := f.validate(); reason != "" {
		f.err = reason
		m.settings.form = &f

		return m, nil
	}

	if m.sources == nil {
		f.err = "no source manager configured"
		m.settings.form = &f

		return m, nil
	}

	f.testing = true
	f.testGen++
	f.err = ""
	f.info = "testing…"
	gen := f.testGen
	m.settings.form = &f

	return m, testFormCmd(m.sources, f.toIndexerWithID(f.resolvedID()), gen)
}

// maskSecret renders a credential field: the real value when reveal is set
// (or the field is empty), a same-length run of bullets otherwise — never
// logged, never shown elsewhere (AGENT.md §6.6).
func maskSecret(v string, reveal bool) string {
	if reveal || v == "" {
		return v
	}

	return strings.Repeat("•", len([]rune(v)))
}

// settingsScreenLegend documents the list-view keys keymap.go deliberately
// keeps out of the `?` overlay (the 80×24 budget) — shown here instead so
// they stay discoverable without ever needing the config file.
const settingsScreenLegend = "a add · e edit · t test · space enable/disable · x remove · r reload definitions"

// renderSettingsScreen draws ScreenSettings' real body: the source list, or
// the open add/edit form on top of it. Pure (AGENT.md §6.8).
func (m Model) renderSettingsScreen() string {
	th := m.theme

	var b strings.Builder

	rows := m.sourceRows()

	if len(rows) == 0 {
		b.WriteString(th.Muted.Render("No sources configured. Press 'a' to add one."))
		return truncateLines(b.String(), m.width)
	}

	for i, s := range rows {
		marker := "  "
		if i == m.settings.cursor {
			marker = "> "
		}

		enabled := "off"
		if s.Enabled {
			enabled = "on"
		}

		outcome := m.lastOutcome(s.ID)
		if outcome == "" {
			outcome = "not searched yet"
		}

		line := fmt.Sprintf("%s%-20s %-8s %-4s %s", marker, s.Name, s.Type, enabled, outcome)
		line = theme.Truncate(line, m.width)

		if i == m.settings.cursor {
			b.WriteString(th.Accent.Render(line))
		} else {
			b.WriteString(th.Foreground.Render(line))
		}

		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(th.Muted.Render(settingsScreenLegend))

	return truncateLines(strings.TrimRight(b.String(), "\n"), m.width)
}

// renderSourceForm draws the open add/edit form: every visible field, the
// currently focused one accented, credentials masked unless revealed, plus
// any inline error/info and the key hints.
func (m Model) renderSourceForm() string {
	th := m.theme
	f := m.settings.form

	inner := max(m.width-4, 20)
	textW := inner - 2

	var b strings.Builder

	title := "Add source"
	if f.editingID != "" {
		title = "Edit source"
	}

	b.WriteString(th.Accent.Render(title))
	b.WriteString("\n\n")

	for i, field := range f.fields() {
		val := f.fieldValue(field)

		switch field {
		case fieldType:
			val = f.typ
		case fieldAPIKey, fieldCookie:
			val = maskSecret(val, f.reveal)
		}

		if val == "" && field != fieldType {
			val = "(empty)"
		}

		line := theme.Truncate(fmt.Sprintf("%-16s %s", field.label()+":", val), textW)

		if i == f.cursor {
			b.WriteString(th.Accent.Render(line))
		} else {
			b.WriteString(th.Foreground.Render(line))
		}

		b.WriteString("\n")
	}

	if f.confirmDiscard {
		b.WriteString("\n")
		b.WriteString(th.Accent.Render("Discard changes? y/enter confirm · n/esc back"))
		b.WriteString("\n")
	}

	if f.err != "" {
		b.WriteString("\n")
		b.WriteString(th.Error.Render(f.err))
		b.WriteString("\n")
	} else if f.info != "" {
		b.WriteString("\n")
		b.WriteString(th.Muted.Render(f.info))
		b.WriteString("\n")
	}

	body := th.Border.Width(inner).Render(strings.TrimRight(b.String(), "\n"))
	help := "tab/shift+tab move · left/right toggle type · ctrl+s/enter save · enter on import field imports · ctrl+t test · ctrl+r reveal · esc cancel"

	return body + "\n" + th.Muted.Render(theme.Truncate(help, m.width))
}

// renderSourceRemoveConfirm draws the `x` confirmation dialog.
func (m Model) renderSourceRemoveConfirm() string {
	return m.settings.removeConfirm.View(m.theme, m.width)
}
