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
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

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
	// path or an http(s) URL (T-025) and returns its id and its base_url,
	// so the add form can pre-fill Definition/ID/URL from it.
	ImportDefinition(ctx context.Context, source string) (id, baseURL string, err error)

	// ReloadDefinitions re-reads every scraper definition file from disk
	// (T-023's Loader.Reload) — the `r` key.
	ReloadDefinitions() error

	// ListAggregatorIndexers lists the indexers a self-hosted aggregator
	// instance (baseURL) is itself configured with, authenticating with
	// the user's own API key for that instance (T-083; AGENT.md §2 — the
	// only auth path). It returns AggregatorIndexer rather than a
	// concrete adapter type, the same reason every other SourceManager
	// method here does (AGENT.md §4): the composition root is the only
	// place that may import a concrete aggregator client package, and it
	// is also the one that fills AggregatorIndexer.FeedURL (e.g. via
	// prowlarr.FeedURL) — this package never builds an aggregator's own
	// URL pattern itself, so a second aggregator is only a second
	// SourceManager implementation, never a TUI change (found in review:
	// the wizard previously hard-coded Prowlarr's own feed-URL pattern).
	ListAggregatorIndexers(ctx context.Context, baseURL, apiKey string) ([]AggregatorIndexer, error)
}

// AggregatorIndexer is one indexer a self-hosted aggregator (T-083)
// reports it is itself configured with: enough for the import wizard to
// show a name to pick and, once selected, a ready-to-save feed URL —
// FeedURL is filled by the SourceManager implementation that produced this
// value (e.g. internal/indexer/prowlarr's FeedURL for a Prowlarr
// instance), never computed inside internal/tui, so this package never
// needs to know any aggregator's own URL shape (AGENT.md §4).
type AggregatorIndexer struct {
	ID      string
	Name    string
	FeedURL string
}

// BuiltinSource is one bundled source (T-9069): compiled into the binary,
// on until the user turns it off, never editable or removable.
type BuiltinSource struct {
	ID      string
	Name    string
	Enabled bool
}

// BuiltinManager is what the settings screen needs to list, switch and test
// the bundled sources. A SourceManager that also implements it gets built-in
// rows; one that does not (a fake, the demo) shows none.
type BuiltinManager interface {
	// BuiltinSources lists the bundled sources and whether each is on.
	BuiltinSources() []BuiltinSource

	// SetBuiltinEnabled persists the on/off state and re-syncs the live
	// registry, so the Search screen offers or drops the source at once.
	SetBuiltinEnabled(id string, enabled bool) error

	// TestBuiltin runs one bounded probe of the bundled source id.
	TestBuiltin(ctx context.Context, id string) error
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
	// fieldAggregatorImport is the blank add form's entry point into the
	// bulk aggregator-import wizard (T-083): enter on this field opens it.
	// It is torznab-only — an aggregator's own indexers are imported as
	// ordinary torznab sources (see prowlarr.FeedURL) — and add-only:
	// editing a single already-saved source has no use for a wizard that
	// creates several new ones.
	fieldAggregatorImport
	fieldID
)

// visibleFields returns the fields the form shows for typ ("torznab" or
// "scraper") on a blank add form (isNew) or an edit form, in the order
// arrow/tab navigation moves through them. Definition and Import are
// scraper-only; aggregator import is torznab-and-add-only; ID is last
// since it is an override nobody has to touch.
func visibleFields(typ string, isNew bool) []sourceFormField {
	base := []sourceFormField{fieldName, fieldType, fieldURL, fieldAPIKey, fieldCookie}

	switch {
	case typ == "scraper":
		base = append(base, fieldDefinition, fieldImport)
	case isNew:
		base = append(base, fieldAggregatorImport)
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
	case fieldAggregatorImport:
		return "Import from aggregator (enter opens it)"
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

	// importing is true while an import started from this form is in
	// flight; saveAfterImport is true when that import was started by a
	// save (T-9052) and so finishes with the save. Both are cleared by the
	// import's result.
	importing       bool
	saveAfterImport bool
	// importGen is the generation of the import whose result this form is
	// waiting for; formImportResultMsg.gen must match it (T-9052).
	importGen int

	// completion and completeGen drive tab-completion on the import field
	// (pathcomplete.go, T-9019).
	completion  *pathCompletion
	completeGen int

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
func (f sourceForm) fields() []sourceFormField { return visibleFields(f.typ, f.editingID == "") }

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
// It is also the first entry liveIssues checks, so save/test-time gating
// and the as-you-type hints never disagree about what "required" means.
func (f sourceForm) validate() string {
	if strings.TrimSpace(f.name) == "" {
		return "name is required"
	}

	// A scraper source is built from its definition, whose own base_url is
	// what is fetched, so its URL field is optional; a typed one is still
	// checked so a typo is flagged. Torznab always needs its feed URL.
	url := strings.TrimSpace(f.sourceURL)
	if url == "" && f.typ != "scraper" {
		return "URL is required"
	}

	if url != "" {
		if reason := invalidURLReason(url); reason != "" {
			return reason
		}
	}

	if f.typ == "scraper" && strings.TrimSpace(f.definition) == "" {
		return errNeedsDefinition
	}

	return ""
}

// importFailedPrefix starts the form error shown for a failed import.
const importFailedPrefix = "import failed: "

// errNeedsDefinition is validate's reason for a scraper form with no
// definition file.
const errNeedsDefinition = "a scraper source needs a definition file (or import one)"

// hasUnrunImport reports whether f is a scraper form with no definition
// yet but text waiting in the import field: the user typed a path or URL and
// moved on without pressing enter there. A save runs that import first
// (T-9052).
func (f sourceForm) hasUnrunImport() bool {
	return f.typ == "scraper" &&
		strings.TrimSpace(f.definition) == "" &&
		strings.TrimSpace(f.importText) != ""
}

// invalidURLReason reports why raw is not a usable source URL, or "" when
// it is: it must parse and use an http or https scheme with a non-empty
// host. A definition-driven scraper source and a Torznab feed are both
// always fetched over plain HTTP(S) (AGENT.md §2 — no other transport is
// ever wired), so anything else is rejected before it ever reaches a
// SourceManager.
func invalidURLReason(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Sprintf("URL is not valid: %v", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return "URL must start with http:// or https://"
	}

	if u.Host == "" {
		return "URL must include a host"
	}

	return ""
}

// liveIssues reports every problem with f exactly as currently typed,
// checked against existing (the settings screen's current source
// snapshot, sourceRows) for the duplicate-id case. renderSourceForm calls
// this on every render, so a required-field, invalid-URL, or duplicate-id
// hint appears — or clears — as the user types, with no need to press
// save or test first (T-080 review finding).
func (f sourceForm) liveIssues(existing []config.Indexer) []string {
	var issues []string

	// Import text waiting in the field is not a missing definition: a save
	// runs it (T-9052), and a failed import's own message must stay visible
	// rather than sit under this hint.
	if reason := f.validate(); reason != "" && (reason != errNeedsDefinition || !f.hasUnrunImport()) {
		issues = append(issues, reason)
	}

	if id := f.resolvedID(); id != "" {
		for _, s := range existing {
			if s.ID == id && s.ID != f.editingID {
				issues = append(issues, fmt.Sprintf("id %q is already used by another source", id))
				break
			}
		}
	}

	return issues
}

// settingsModel is ScreenSettings' own state: the list cursor, an open
// add/edit form (nil when the list itself has focus), and the remove
// confirmation.
type settingsModel struct {
	cursor        int
	form          *sourceForm
	removeConfirm components.Dialog
	removeID      string

	// T-081 connection test. lastProbe holds the most recent classified
	// outcome per source id, kept until the next test for that id so the
	// `d` detail panel can show it even after the status bar's own
	// transient message has cleared. testingID/probeCancel/probeGen track
	// whichever probe (if any) is currently in flight; probeGen guards a
	// cancelled or superseded probe's eventual result the same way
	// search.go's own generation counter guards a stale search.
	lastProbe   map[string]probeResult
	testingID   string
	probeCancel context.CancelFunc
	probeGen    int

	// importSeq numbers every import dispatched from any form. It lives
	// here, not on the form, so a form opened later never reuses the
	// generation of an import that was in flight when an earlier form
	// closed.
	importSeq int
	// detailOpen is true while the `d` connection-test detail panel
	// (ContextSourceTestDetail) is open.
	detailOpen bool

	// view selects which of the settings screen's two panels is shown:
	// the source list (default) or the preferences panel (T-082,
	// preferences.go), reached with "p".
	view settingsView
	// prefsForm is the open preferences panel's own state, nil when the
	// source list has focus.
	prefsForm *prefsForm

	// aggImport is the open aggregator-import wizard's own state (T-083,
	// aggregator_import.go), nil when it is closed. Mutually exclusive
	// with form: opening it replaces the add form rather than stacking a
	// second modal on top of it (AGENT.md §7).
	aggImport *aggregatorForm
}

func newSettingsModel() settingsModel {
	return settingsModel{
		removeConfirm: newSourceRemoveDialog(),
		lastProbe:     map[string]probeResult{},
	}
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

// sourceRows returns the sources this screen lists: the model-side
// sourcesSnapshot (root.go), never a live SourceManager.Sources() call —
// Update() and View() must not make a synchronous call that could block on
// whatever lock a real SourceManager's save path holds (AGENT.md §6.1/§6.8;
// found in review). The snapshot is populated once at construction and kept
// current purely by messages afterwards; see sourcesSaveResultMsg and
// formSaveResultMsg.
func (m Model) sourceRows() []config.Indexer {
	return append([]config.Indexer(nil), m.sourcesSnapshot...)
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
	n := len(m.sourceRows()) + len(m.builtinSnapshot)
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

// builtinManager returns the source manager's built-in support, or nil.
func (m Model) builtinManager() BuiltinManager {
	if m.sources == nil {
		return nil
	}

	bm, _ := m.sources.(BuiltinManager)

	return bm
}

// selectedBuiltin returns the built-in row the settings cursor points at.
// Built-in rows follow the configured ones.
func (m Model) selectedBuiltin() (BuiltinSource, bool) {
	i := m.settings.cursor - len(m.sourceRows())
	if i < 0 || i >= len(m.builtinSnapshot) {
		return BuiltinSource{}, false
	}

	return m.builtinSnapshot[i], true
}

// selectedRow names the row under the cursor, configured or built-in.
func (m Model) selectedRow() (id, name string, ok bool) {
	if src, found := m.selectedSource(); found {
		return src.ID, src.Name, true
	}

	if b, found := m.selectedBuiltin(); found {
		return b.ID, b.Name, true
	}

	return "", "", false
}

// selectedSource returns the configured row the settings cursor points at.
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
	if _, ok := m.selectedBuiltin(); ok {
		return m.pushStatus(builtinLockedMsg("edited"))
	}

	src, ok := m.selectedSource()
	if !ok {
		return m.pushStatus("no source selected")
	}

	f := newEditForm(src)
	m.settings.form = &f

	return m, nil
}

// handleSourceToggleEnabled flips the selected row's Enabled bit and saves
// immediately (space). The snapshot is updated optimistically, before the
// save even dispatches: sourceRows() never calls the SourceManager
// directly (see its own doc comment), so a second toggle pressed before the
// first save's result arrives must still see the just-applied change, or
// the two toggles cancel each other out into a lost update (found in
// review). A failed save reverts the snapshot in handleSourcesSaveResult.
func (m Model) handleSourceToggleEnabled() (tea.Model, tea.Cmd) {
	if b, ok := m.selectedBuiltin(); ok {
		return m.toggleBuiltin(b)
	}

	src, ok := m.selectedSource()
	if !ok || m.sources == nil {
		return m, nil
	}

	previous := m.sourceRows()
	all := append([]config.Indexer(nil), previous...)

	for i := range all {
		if all[i].ID == src.ID {
			all[i].Enabled = !all[i].Enabled
		}
	}

	m.sourcesSnapshot = all

	return m, saveSourcesCmd(m.sources, all, previous)
}

// handleSourceRemove opens the remove confirmation ("x").
func (m Model) handleSourceRemove() (tea.Model, tea.Cmd) {
	if _, ok := m.selectedBuiltin(); ok {
		return m.pushStatus(builtinLockedMsg("removed"))
	}

	src, ok := m.selectedSource()
	if !ok {
		return m.pushStatus("no source selected")
	}

	m.settings.removeID = src.ID
	m.settings.removeConfirm.Message = fmt.Sprintf("Remove %q?", src.Name)
	m.settings.removeConfirm = m.settings.removeConfirm.Open()

	return m, nil
}

// builtinLockedMsg is the status line for an edit or remove on a built-in
// row: its definition is compiled in, so the only off switch is disabling it.
func builtinLockedMsg(verb string) string {
	return "built-in sources are compiled in and can't be " + verb + " - space turns one off"
}

// builtinToggleResultMsg reports SetBuiltinEnabled's outcome; previous is
// the built-in snapshot before the optimistic flip, restored on failure.
type builtinToggleResultMsg struct {
	err      error
	previous []BuiltinSource
}

func setBuiltinCmd(bm BuiltinManager, id string, enabled bool, previous []BuiltinSource) tea.Cmd {
	return func() tea.Msg {
		return builtinToggleResultMsg{err: bm.SetBuiltinEnabled(id, enabled), previous: previous}
	}
}

// toggleBuiltin flips a built-in row and saves at once, optimistically for
// the same reason handleSourceToggleEnabled is.
func (m Model) toggleBuiltin(b BuiltinSource) (tea.Model, tea.Cmd) {
	bm := m.builtinManager()
	if bm == nil {
		return m, nil
	}

	previous := append([]BuiltinSource(nil), m.builtinSnapshot...)
	next := append([]BuiltinSource(nil), previous...)

	for i := range next {
		if next[i].ID == b.ID {
			next[i].Enabled = !next[i].Enabled
		}
	}

	m.builtinSnapshot = next

	return m, setBuiltinCmd(bm, b.ID, !b.Enabled, previous)
}

// handleBuiltinToggleResult applies a built-in toggle's outcome: a failed
// save puts the row back; a saved one refreshes the Search screen's source
// list from the registry, so both screens show the same state.
func (m Model) handleBuiltinToggleResult(msg builtinToggleResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.builtinSnapshot = msg.previous

		return m.pushStatus(fmt.Sprintf("couldn't save built-in source: %v", msg.err))
	}

	return m.refreshSearchSources(), nil
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

		previous := m.sourceRows()

		var remaining []config.Indexer
		for _, s := range previous {
			if s.ID != id {
				remaining = append(remaining, s)
			}
		}

		m.sourcesSnapshot = remaining

		// Keep the cursor on a row that still exists.
		m.settings.cursor = max(min(m.settings.cursor, len(remaining)+len(m.builtinSnapshot)-1), 0)

		return m, saveSourcesCmd(m.sources, remaining, previous)
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

// probeOutcome classifies a TestSource result into the fixed set of
// outcomes T-081 requires the settings screen to distinguish: reachable
// (nil error), timeout, auth failed, parse failed, or a generic
// unreachable/other failure.
type probeOutcome int

const (
	probeReachable probeOutcome = iota
	probeTimeout
	probeAuthFailed
	probeParseFailed
	probeUnreachable
)

// label is the word the settings screen shows for this outcome.
func (o probeOutcome) label() string {
	switch o {
	case probeReachable:
		return "reachable"
	case probeTimeout:
		return "timeout"
	case probeAuthFailed:
		return "auth failed"
	case probeParseFailed:
		return "parse failed"
	default:
		return "unreachable"
	}
}

// probeResult is one source's most recent classified connection-test
// outcome, kept in settingsModel.lastProbe for the `d` detail panel. err is
// nil exactly when outcome is probeReachable.
type probeResult struct {
	outcome probeOutcome
	err     error
}

// probeAuthFailure is implemented by an error that means the source
// rejected the request over the user's own credentials — a missing or
// wrong api key, cookie, or passkey — never a report on whether tortui
// could get around that (AGENT.md §2: it never tries). This package cannot
// import a concrete indexer adapter (AGENT.md §4 — only the registry may),
// so classification is duck-typed via errors.As against this small marker
// interface rather than a concrete adapter error type: any current or
// future adapter error participates just by implementing it.
type probeAuthFailure interface {
	AuthFailed() bool
}

// probeParseFailure is implemented by an error that means a response
// arrived but could not be parsed as the source's expected feed shape —
// see probeAuthFailure's doc comment for why this is duck-typed too.
type probeParseFailure interface {
	ParseFailed() bool
}

// probeTimeoutError is the standard net.Error-family shape (net.OpError,
// url.Error, and friends all implement it) for "this failed because it
// timed out," distinct from context.DeadlineExceeded itself: an adapter's
// own HTTP client can return a timeout that never actually observes this
// package's context deadline — a dial timeout or a lower, per-request
// timeout of the adapter's own — so relying on errors.Is(context.
// DeadlineExceeded) alone would misclassify those as probeUnreachable.
type probeTimeoutError interface {
	Timeout() bool
}

// classifyProbeError sorts a TestSource error into one of the outcomes
// above. A timeout — either this package's own context deadline, or an
// adapter's own transport-level timeout (probeTimeoutError) — always wins
// over the auth/parse marker interfaces below: an error can plausibly
// implement one of them *and* have been produced only because something
// timed out, and showing a probe that was cut short by a timeout as some
// other failure would be misleading.
func classifyProbeError(err error) probeOutcome {
	if err == nil {
		return probeReachable
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return probeTimeout
	}

	var timeout probeTimeoutError
	if errors.As(err, &timeout) && timeout.Timeout() {
		return probeTimeout
	}

	var auth probeAuthFailure
	if errors.As(err, &auth) && auth.AuthFailed() {
		return probeAuthFailed
	}

	var parse probeParseFailure
	if errors.As(err, &parse) && parse.ParseFailed() {
		return probeParseFailed
	}

	return probeUnreachable
}

// handleSourceTest implements `t` on the list: a cancellable probe against
// the already-saved selected source, bounded by sourceTestTimeout. Refuses
// a second probe while one is already in flight — two tea.Cmds racing the
// same in-flight state would have no ordering guarantee (the same
// discipline download_actions.go's pause/resume already applies, DEC-115).
func (m Model) handleSourceTest() (tea.Model, tea.Cmd) {
	if b, ok := m.selectedBuiltin(); ok {
		return m.testBuiltin(b)
	}

	src, ok := m.selectedSource()
	if !ok || m.sources == nil {
		return m.pushStatus("no source selected")
	}

	if m.settings.testingID != "" {
		return m.pushStatus("a test is already running — esc cancels it")
	}

	ctx, cancel := context.WithTimeout(context.Background(), sourceTestTimeout)

	m.settings.testingID = src.ID
	m.settings.probeCancel = cancel
	m.settings.probeGen++
	gen := m.settings.probeGen

	updated, cmd := m.pushStatus("testing " + src.Name + "…")

	return updated, tea.Batch(cmd, testSourceCmd(m.sources, src, ctx, cancel, gen))
}

// testBuiltin starts the `t` probe for a built-in row, with the same
// one-at-a-time rule and bookkeeping as a configured source's.
func (m Model) testBuiltin(b BuiltinSource) (tea.Model, tea.Cmd) {
	bm := m.builtinManager()
	if bm == nil {
		return m.pushStatus("no source selected")
	}

	if m.settings.testingID != "" {
		return m.pushStatus("a test is already running — esc cancels it")
	}

	ctx, cancel := context.WithTimeout(context.Background(), sourceTestTimeout)

	m.settings.testingID = b.ID
	m.settings.probeCancel = cancel
	m.settings.probeGen++
	gen := m.settings.probeGen

	updated, cmd := m.pushStatus("testing " + b.Name + "…")

	run := func() tea.Msg {
		defer cancel()

		return sourceProbeResultMsg{id: b.ID, name: b.Name, gen: gen, err: bm.TestBuiltin(ctx, b.ID)}
	}

	return updated, tea.Batch(cmd, run)
}

// handleSourceTestCancel implements esc while a probe is in flight on the
// settings list: stops it via its own context and clears the in-flight
// state, so a new `t` (or the `d` detail panel for whatever the last
// *completed* probe found) is immediately available again. A no-op when
// nothing is in flight — esc has no other meaning on this screen's list.
func (m Model) handleSourceTestCancel() (tea.Model, tea.Cmd) {
	if m.settings.testingID == "" {
		return m, nil
	}

	if m.settings.probeCancel != nil {
		m.settings.probeCancel()
	}

	name := m.settings.testingID

	for _, s := range m.sourceRows() {
		if s.ID == m.settings.testingID {
			name = s.Name
			break
		}
	}

	for _, b := range m.builtinSnapshot {
		if b.ID == m.settings.testingID {
			name = b.Name
			break
		}
	}

	m.settings.testingID = ""
	m.settings.probeCancel = nil

	return m.pushStatus(name + ": test cancelled")
}

// handleSourceTestDetailOpen implements `d` on the settings list: opens the
// connection-test detail panel for the selected source, if it has one.
func (m Model) handleSourceTestDetailOpen() (tea.Model, tea.Cmd) {
	id, _, ok := m.selectedRow()
	if !ok {
		return m.pushStatus("no source selected")
	}

	if _, ok := m.settings.lastProbe[id]; !ok {
		return m.pushStatus("no test result for this source yet — press t first")
	}

	m.settings.detailOpen = true

	return m, nil
}

// sourcesSaveResultMsg reports SaveSources' outcome for the list's own
// toggle-enabled and remove actions (the add/edit form's own save uses
// formSaveResultMsg instead, since it needs to close the form on success).
// previous is what sourcesSnapshot held before the optimistic update that
// preceded this save, so a failure can put it back exactly (handleSourcesSaveResult).
type sourcesSaveResultMsg struct {
	err      error
	previous []config.Indexer
}

func saveSourcesCmd(sm SourceManager, sources, previous []config.Indexer) tea.Cmd {
	return func() tea.Msg {
		return sourcesSaveResultMsg{err: sm.SaveSources(sources), previous: previous}
	}
}

// formSaveResultMsg reports the add/edit form's own save attempt. applied is
// the full source set the form just saved (so a successful result can
// become the new sourcesSnapshot without a second SourceManager.Sources()
// call); previous is what the snapshot held before, for a failed save to
// restore.
type formSaveResultMsg struct {
	err      error
	applied  []config.Indexer
	previous []config.Indexer
}

func saveFormCmd(sm SourceManager, sources, previous []config.Indexer) tea.Cmd {
	return func() tea.Msg {
		return formSaveResultMsg{err: sm.SaveSources(sources), applied: sources, previous: previous}
	}
}

// sourceProbeResultMsg reports one `t` probe's outcome (T-081). id and gen
// tie it to whichever dispatch started it, in the list context; the form
// uses formTestResultMsg instead. A superseded or cancelled probe's result
// is recognised as stale by comparing both against the current
// settingsModel state (handleSourceProbeResult) rather than gen alone,
// since a cancelled probe's testingID is cleared immediately but its
// eventual result — dispatched before the cancel — could otherwise still
// match a later gen that a *different* source's test bumped to.
type sourceProbeResultMsg struct {
	id   string
	name string
	gen  int
	err  error
}

// testSourceCmd runs sm.TestSource(ctx, src) off Update's own goroutine
// (AGENT.md §6.1) and always releases ctx's resources via cancel once it
// returns — whether that is a real completion, the timeout firing, or an
// esc-triggered cancel calling the same cancel func early (context's
// CancelFunc is idempotent, so calling it twice here is harmless).
func testSourceCmd(sm SourceManager, src config.Indexer, ctx context.Context, cancel context.CancelFunc, gen int) tea.Cmd {
	return func() tea.Msg {
		defer cancel()

		return sourceProbeResultMsg{id: src.ID, name: src.Name, gen: gen, err: sm.TestSource(ctx, src)}
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
const sourceTestTimeout = 15 * time.Second

func testFormCmd(sm SourceManager, src config.Indexer, gen int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), sourceTestTimeout)
		defer cancel()

		return formTestResultMsg{gen: gen, err: sm.TestSource(ctx, src)}
	}
}

// formImportResultMsg reports the form's import-a-definition attempt.
type formImportResultMsg struct {
	gen     int
	id      string
	baseURL string
	err     error
}

func importDefinitionCmd(sm SourceManager, source string, gen int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), sourceTestTimeout)
		defer cancel()

		id, baseURL, err := sm.ImportDefinition(ctx, source)

		return formImportResultMsg{gen: gen, id: id, baseURL: baseURL, err: err}
	}
}

// handleSourcesSaveResult applies a toggle-enabled or remove's outcome.
func (m Model) handleSourcesSaveResult(msg sourcesSaveResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		// Revert the optimistic update (handleSourceToggleEnabled,
		// handleSourceRemoveConfirmAction) — the save that would have made
		// it real never landed.
		m.sourcesSnapshot = msg.previous

		return m.pushStatus(fmt.Sprintf("couldn't save sources: %v", msg.err))
	}

	// The save succeeded and the concrete SourceManager already re-synced
	// the live registry to match (its own contract); refresh the search
	// screen's source list from it now, rather than leaving it showing
	// whatever was enabled at startup (T-080 review finding: "reloads the
	// registry live" must be visible on the search screen too, not just
	// on disk).
	m = m.refreshSearchSources()

	return m.handleSettingsMoveCursor(0)
}

// handleFormSaveResult applies the add/edit form's own save attempt: closes
// the form on success, keeps it open with the error shown otherwise. It
// still applies the outcome (reverting the snapshot on failure, refreshing
// search on success, and reporting either via the status bar) even if the
// form was already closed by a discard confirmed while the save was still
// in flight — dropping the result silently would leave the snapshot wrong
// or the user unaware a save that already reached disk failed (found in
// review).
func (m Model) handleFormSaveResult(msg formSaveResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.sourcesSnapshot = msg.previous

		if m.settings.form != nil {
			m.settings.form.err = msg.err.Error()
			return m, nil
		}

		return m.pushStatus(fmt.Sprintf("couldn't save source: %v", msg.err))
	}

	m = m.refreshSearchSources()

	if m.settings.form != nil {
		m.settings.form = nil
	}

	return m.pushStatus("source saved")
}

// handleSourceProbeResult applies the list's own `t` probe outcome (T-081):
// classifies the error, records it in lastProbe for the `d` detail panel,
// and reports the classified label via the status bar. A stale result — one
// whose gen no longer matches the in-flight probe, whether superseded by a
// newer `t` or dropped by esc's cancel — is discarded, the same generation-
// guard pattern search.go and the form's own formTestResultMsg already use.
func (m Model) handleSourceProbeResult(msg sourceProbeResultMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.settings.probeGen || m.settings.testingID != msg.id {
		return m, nil
	}

	m.settings.testingID = ""
	m.settings.probeCancel = nil

	outcome := classifyProbeError(msg.err)

	next := make(map[string]probeResult, len(m.settings.lastProbe)+1)
	for k, v := range m.settings.lastProbe {
		next[k] = v
	}

	next[msg.id] = probeResult{outcome: outcome, err: msg.err}
	m.settings.lastProbe = next

	label := msg.name + ": " + outcome.label()
	if msg.err != nil {
		label += " (d for details)"
	}

	return m.pushStatus(label)
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
// Definition (and Name/ID/URL when still blank) from the imported id and
// its base_url.
func (m Model) handleFormImportResult(msg formImportResultMsg) (tea.Model, tea.Cmd) {
	// A result from an import this form did not start (an earlier form, or
	// one superseded by a newer import) is dropped.
	if m.settings.form == nil || msg.gen != m.settings.form.importGen {
		return m, nil
	}

	f := m.settings.form

	// A save that started this import must not finish while the user is
	// answering the discard prompt.
	saveAfter := f.saveAfterImport && !f.confirmDiscard
	f.importing = false
	f.saveAfterImport = false

	if msg.err != nil {
		f.err = importFailedPrefix + msg.err.Error()
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

	if strings.TrimSpace(f.sourceURL) == "" {
		f.sourceURL = msg.baseURL
	}

	f.importText = ""
	f.err = ""
	f.info = "imported: " + msg.id
	f.dirty = true

	if saveAfter {
		return m.handleSourceFormSave(*f)
	}

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

	case "tab", "down":
		if msg.String() == "tab" && f.current() == fieldImport && f.importText != "" && !isURLSource(f.importText) {
			return m.handleImportTab(f)
		}

		f = f.advanceField()
		m.settings.form = &f

		return m, nil

	case "shift+tab", "up":
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
			f.importing = true
			m.settings.importSeq++
			f.importGen = m.settings.importSeq
			m.settings.form = &f

			return m, importDefinitionCmd(m.sources, expandHome(strings.TrimSpace(f.importText)), f.importGen)
		}

		// On the aggregator-import field, enter replaces the add form with
		// the import wizard (T-083) instead of saving — never more than
		// one modal deep (AGENT.md §7), so this swaps the modal's content
		// rather than opening a second one on top of it.
		if f.current() == fieldAggregatorImport {
			m.settings.form = nil
			ag := newAggregatorForm()
			m.settings.aggImport = &ag

			return m, nil
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
	case tea.KeyRunes, tea.KeySpace:
		field := f.current()
		if field == fieldType {
			return m, nil
		}

		// Bubble Tea reports the space bar as tea.KeySpace, not KeyRunes.
		text := string(msg.Runes)
		if msg.Type == tea.KeySpace {
			text = " "
		}

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
	// An import is in flight (started by enter on the import field or by an
	// earlier save): a second save would import, and then save, twice.
	if f.importing {
		return m, nil
	}

	// A scraper form with import text but no definition: run that import
	// first, then finish this save from its result (T-9052).
	if f.hasUnrunImport() && m.sources != nil {
		f.err = ""
		f.info = ""
		f.importing = true
		f.saveAfterImport = true
		m.settings.importSeq++
		f.importGen = m.settings.importSeq
		m.settings.form = &f

		return m, importDefinitionCmd(m.sources, expandHome(strings.TrimSpace(f.importText)), f.importGen)
	}

	if reason := f.validate(); reason != "" {
		f.err = reason
		m.settings.form = &f

		return m, nil
	}

	id := f.resolvedID()

	previous := m.sourceRows()
	all := append([]config.Indexer(nil), previous...)

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

	// Optimistic, same reason as handleSourceToggleEnabled: sourceRows()
	// only ever reads the model-side snapshot now, so a later read (a
	// second save attempt, a list redraw) must see this one applied
	// immediately rather than the pre-save state. handleFormSaveResult
	// reverts it on failure.
	m.sourcesSnapshot = all

	return m, saveFormCmd(m.sources, all, previous)
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
// builtinTag is the type column text of a built-in source's row.
const builtinTag = "built-in"

const settingsScreenLegend = "a add · e edit · t test (esc cancels) · d test detail · space enable/disable · x remove · r reload definitions · p preferences"

// renderSettingsScreen draws ScreenSettings' real body: the source list, or
// the open add/edit form on top of it. Pure (AGENT.md §6.8).
func (m Model) renderSettingsScreen() string {
	th := m.theme

	var b strings.Builder

	rows := m.sourceRows()

	if len(rows) == 0 && len(m.builtinSnapshot) == 0 {
		b.WriteString(th.Muted.Render("No sources configured. Press 'a' to add one."))
		return truncateLines(b.String(), m.width)
	}

	// Built-in sources follow the configured ones, tagged in the type column.
	for _, bs := range m.builtinSnapshot {
		rows = append(rows, config.Indexer{ID: bs.ID, Name: bs.Name, Type: builtinTag, Enabled: bs.Enabled})
	}

	legend := wrapLegend(settingsScreenLegend, m.width)
	first, last := m.sourceWindow(len(rows), strings.Count(legend, "\n")+1)

	for i := first; i < last; i++ {
		s := rows[i]
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
	b.WriteString(th.Muted.Render(legend))

	return truncateLines(strings.TrimRight(b.String(), "\n"), m.width)
}

// sourceWindow is the range of source rows shown: all of them when they fit,
// otherwise a window that follows the cursor (the same scroll-with-selection
// as destWindow) so the legend below the list and the status bar stay on
// screen. legendLines is how many lines the wrapped legend takes.
func (m Model) sourceWindow(total, legendLines int) (int, int) {
	visible := total

	if m.height > 0 {
		// The blank line between the list and the legend is the "- 1".
		visible = min(total, max(m.bodyBudget(true)-1-legendLines, 1))
	}

	cursor := max(min(m.settings.cursor, total-1), 0)

	start := 0
	if cursor >= visible {
		start = cursor - visible + 1
	}

	return start, min(start+visible, total)
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
		case fieldAggregatorImport:
			val = "press enter to list an aggregator's sources"
		}

		if val == "" && field != fieldType && field != fieldAggregatorImport {
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

	// Live validation (T-080 review finding): recomputed every render, so
	// a required-field, invalid-URL, or duplicate-id hint appears or
	// clears as the user types, with no save or test needed. It takes
	// priority over a stale save/test/import outcome (f.err/f.info) —
	// once every live issue is fixed, whatever f.err said before is no
	// longer the most useful thing on screen.
	if live := f.liveIssues(m.sourceRows()); len(live) > 0 {
		b.WriteString("\n")

		for _, issue := range live {
			b.WriteString(th.Error.Render("! " + issue))
			b.WriteString("\n")
		}

		// A failed import is the answer to the user's last action (a save
		// now runs a pending import, T-9052), so it is shown even while
		// other hints are; a blank Name, say, must not hide it.
		if strings.HasPrefix(f.err, importFailedPrefix) {
			b.WriteString(th.Error.Render(f.err))
			b.WriteString("\n")
		}
	} else if f.err != "" {
		b.WriteString("\n")
		b.WriteString(th.Error.Render(f.err))
		b.WriteString("\n")
	} else if f.info != "" {
		b.WriteString("\n")
		b.WriteString(th.Muted.Render(f.info))
		b.WriteString("\n")
	}

	body := th.Border.Width(inner).Render(strings.TrimRight(b.String(), "\n"))
	help := "tab/shift+tab/↑/↓ move · left/right toggle type · ctrl+s/enter save · tab completes import path · enter on import field imports · ctrl+t test · ctrl+r reveal · esc cancel"

	return body + "\n" + th.Muted.Render(wrapLegend(help, m.width))
}

// renderSourceRemoveConfirm draws the `x` confirmation dialog.
func (m Model) renderSourceRemoveConfirm() string {
	return m.settings.removeConfirm.View(m.theme, m.width)
}

// renderSourceTestDetail draws the `d` connection-test detail panel
// (T-081): the selected source's most recent classified outcome, plus the
// full underlying error text, word-wrapped, when it failed.
func (m Model) renderSourceTestDetail() string {
	th := m.theme

	var b strings.Builder

	id, rowName, ok := m.selectedRow()

	name := "source"
	if ok {
		name = rowName
	}

	result, ok := m.settings.lastProbe[id]
	if !ok {
		b.WriteString(th.Muted.Render("no test result for this source"))
		return truncateLines(b.String(), m.width)
	}

	b.WriteString(th.Accent.Render(name + ": " + result.outcome.label()))
	b.WriteString("\n\n")

	if result.err != nil {
		for _, line := range theme.Wrap(result.err.Error(), max(m.width-2, 20)) {
			b.WriteString(th.Foreground.Render(line))
			b.WriteString("\n")
		}
	} else {
		b.WriteString(th.Foreground.Render("the probe search succeeded."))
		b.WriteString("\n")
	}

	b.WriteString("\n")

	for _, line := range m.keys.HelpFor(ContextSourceTestDetail) {
		b.WriteString(th.Muted.Render(line))
		b.WriteString("\n")
	}

	return truncateLines(strings.TrimRight(b.String(), "\n"), m.width)
}

// wrapLegend wraps a " · "-separated key legend onto as many lines as width
// needs, breaking only between hints, so no hint is cut and none is lost on
// a narrow terminal. A single hint wider than width is truncated.
func wrapLegend(legend string, width int) string {
	if width <= 0 {
		return legend
	}

	const sep = " · "

	var (
		lines []string
		line  string
	)

	for _, hint := range strings.Split(legend, sep) {
		switch {
		case line == "":
			line = hint
		case theme.Width(line+sep+hint) <= width:
			line += sep + hint
		default:
			lines = append(lines, theme.Truncate(line, width))
			line = hint
		}
	}

	if line != "" {
		lines = append(lines, theme.Truncate(line, width))
	}

	return strings.Join(lines, "\n")
}
