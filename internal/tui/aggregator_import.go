// Aggregator import wizard (T-083): the blank add form's "import from
// aggregator" path (settings.go's fieldAggregatorImport). It takes a
// self-hosted aggregator's base URL and the user's own API key for that
// instance, lists the indexers it is itself configured with
// (SourceManager.ListAggregatorIndexers), and lets the user multi-select
// which to add. Each one imported becomes an ordinary [[indexer]] entry —
// an ordinary torznab source pointed at that aggregator's per-indexer feed
// URL, saved through the exact same SaveSources path the add/edit form
// uses — nothing about it is special-cased afterwards.
//
// Scoped to Prowlarr only (DEC-123, T-083's own acceptance text): Jackett
// and NZBHydra2 are Backlog T-984. Nothing here names either concrete
// aggregator by hostname or endpoint shape beyond what SourceManager's
// ListAggregatorIndexers/FeedURL seam already carries — see
// internal/indexer/prowlarr for the concrete client, wired in by the
// composition root this task's own SourceManager seam still awaits
// (Backlog T-975, same as T-080/T-081/T-082 before it).
package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// aggregatorPhase is which half of the wizard is showing.
type aggregatorPhase int

const (
	// aggregatorPhaseInput is the base URL / API key entry step.
	aggregatorPhaseInput aggregatorPhase = iota
	// aggregatorPhaseList is the fetched indexer list, multi-select.
	aggregatorPhaseList
)

// aggregatorInputField is one field of the input step.
type aggregatorInputField int

const (
	aggregatorFieldBaseURL aggregatorInputField = iota
	aggregatorFieldAPIKey
	numAggregatorInputFields
)

func (f aggregatorInputField) label() string {
	if f == aggregatorFieldAPIKey {
		return "API key"
	}

	return "Base URL"
}

// aggregatorForm is the wizard's own state.
type aggregatorForm struct {
	phase aggregatorPhase

	baseURL string
	apiKey  string
	reveal  bool

	// cursor is the input step's field cursor while phase is
	// aggregatorPhaseInput, or the highlighted row while phase is
	// aggregatorPhaseList.
	cursor int

	fetching  bool
	fetchGen  int
	importing bool

	// items is the fetched indexer list, in the order the aggregator
	// returned it. selected tracks which AggregatorIndexer.ID values are
	// checked for import, by id rather than by row index so a later
	// re-fetch (there is none today, but nothing here assumes items is
	// only ever set once) could not silently keep a stale selection.
	items    []AggregatorIndexer
	selected map[string]bool

	// err is the most recent fetch/import failure, shown inline until the
	// next attempt.
	err string
}

// newAggregatorForm returns a blank wizard, at the input step.
func newAggregatorForm() aggregatorForm {
	return aggregatorForm{selected: map[string]bool{}}
}

// aggregatorFetchTimeout bounds one list request — the same ceiling
// sourceTestTimeout applies to a connection-test probe (T-081), since this
// is the same shape of "one bounded round trip to something the user
// configured."
const aggregatorFetchTimeout = sourceTestTimeout

// aggregatorFetchResultMsg reports ListAggregatorIndexers' outcome. gen
// ties it to whichever fetch dispatched it, the same generation-guard
// pattern search.go and settings.go's own probes already use, so a
// superseded fetch (there is no re-fetch trigger today, but esc back to the
// input step then fetching again must not let a slow first response land
// on top of the second) is recognised as stale rather than applied.
type aggregatorFetchResultMsg struct {
	gen   int
	items []AggregatorIndexer
	err   error
}

func fetchAggregatorIndexersCmd(sm SourceManager, baseURL, apiKey string, gen int) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), aggregatorFetchTimeout)
		defer cancel()

		items, err := sm.ListAggregatorIndexers(ctx, baseURL, apiKey)

		return aggregatorFetchResultMsg{gen: gen, items: items, err: err}
	}
}

// aggregatorImportResultMsg reports the wizard's own bulk SaveSources
// attempt.
type aggregatorImportResultMsg struct {
	err      error
	applied  []config.Indexer
	previous []config.Indexer
	count    int
}

func saveAggregatorImportCmd(sm SourceManager, all, previous []config.Indexer, count int) tea.Cmd {
	return func() tea.Msg {
		return aggregatorImportResultMsg{err: sm.SaveSources(all), applied: all, previous: previous, count: count}
	}
}

// handleAggregatorImportKey handles every key while the wizard
// (ContextAggregatorImport) is open. Like the add/edit form, it claims
// almost every key itself: the input step needs runes and backspace for
// two free-text fields, and the list step needs its own space/enter
// meanings distinct from the settings list's.
func (m Model) handleAggregatorImportKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := *m.settings.aggImport

	if f.phase == aggregatorPhaseList {
		return m.handleAggregatorListKey(msg, f)
	}

	return m.handleAggregatorInputKey(msg, f)
}

func (m Model) handleAggregatorInputKey(msg tea.KeyMsg, f aggregatorForm) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.settings.aggImport = nil
		return m, nil

	case "tab", "down":
		f.cursor = (f.cursor + 1) % int(numAggregatorInputFields)
		m.settings.aggImport = &f

		return m, nil

	case "shift+tab", "up":
		f.cursor = (f.cursor - 1 + int(numAggregatorInputFields)) % int(numAggregatorInputFields)
		m.settings.aggImport = &f

		return m, nil

	case "ctrl+r":
		f.reveal = !f.reveal
		m.settings.aggImport = &f

		return m, nil

	case "enter":
		return m.handleAggregatorFetch(f)
	}

	switch msg.Type {
	case tea.KeyRunes, tea.KeySpace:
		text := string(msg.Runes)
		if msg.Type == tea.KeySpace {
			text = " "
		}

		if f.cursor == int(aggregatorFieldAPIKey) {
			f.apiKey += text
		} else {
			f.baseURL += text
		}

		m.settings.aggImport = &f

		return m, nil

	case tea.KeyBackspace:
		if f.cursor == int(aggregatorFieldAPIKey) {
			f.apiKey = trimLastRune(f.apiKey)
		} else {
			f.baseURL = trimLastRune(f.baseURL)
		}

		m.settings.aggImport = &f

		return m, nil
	}

	return m, nil
}

// handleAggregatorFetch validates the input step and, if it passes,
// dispatches the bounded, cancellable-by-generation list request. A second
// enter while a fetch is already in flight is refused outright — the same
// discipline handleSourceTest already applies to a second `t` (DEC-115) —
// rather than dispatching a second overlapping request.
func (m Model) handleAggregatorFetch(f aggregatorForm) (tea.Model, tea.Cmd) {
	if f.fetching {
		return m, nil
	}

	baseURL := strings.TrimSpace(f.baseURL)

	if baseURL == "" {
		f.err = "base URL is required"
		m.settings.aggImport = &f

		return m, nil
	}

	if reason := invalidURLReason(baseURL); reason != "" {
		f.err = reason
		m.settings.aggImport = &f

		return m, nil
	}

	apiKey := strings.TrimSpace(f.apiKey)
	if apiKey == "" {
		f.err = "API key is required"
		m.settings.aggImport = &f

		return m, nil
	}

	if m.sources == nil {
		f.err = "no source manager configured"
		m.settings.aggImport = &f

		return m, nil
	}

	f.baseURL = baseURL
	// Trimmed once, here, and used everywhere downstream — the saved
	// source's own credential (aggregatorIndexerToSource) must be the same
	// value that was actually sent to the aggregator, not a re-trim of
	// whatever the field happens to hold later (found in review).
	f.apiKey = apiKey
	f.fetching = true
	f.fetchGen++
	f.err = ""
	gen := f.fetchGen
	m.settings.aggImport = &f

	return m, fetchAggregatorIndexersCmd(m.sources, f.baseURL, f.apiKey, gen)
}

// handleAggregatorFetchResult applies a fetch's outcome: the same
// reachable/timeout/auth-failed/parse-failed/unreachable taxonomy T-081
// already classifies a connection-test probe into (T-083 acceptance: "same
// taxonomy as T-081").
func (m Model) handleAggregatorFetchResult(msg aggregatorFetchResultMsg) (tea.Model, tea.Cmd) {
	f := m.settings.aggImport
	if f == nil || msg.gen != f.fetchGen {
		return m, nil
	}

	f.fetching = false

	if msg.err != nil {
		outcome := classifyProbeError(msg.err)
		f.err = outcome.label() + ": " + msg.err.Error()

		return m, nil
	}

	f.items = msg.items
	f.selected = map[string]bool{}
	f.phase = aggregatorPhaseList
	f.cursor = 0
	f.err = ""

	return m, nil
}

func (m Model) handleAggregatorListKey(msg tea.KeyMsg, f aggregatorForm) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Back to the input step rather than closing outright: nothing
		// has been imported yet, and the user may just want to fix a
		// typo'd URL or key.
		f.phase = aggregatorPhaseInput
		f.cursor = 0
		f.err = ""
		m.settings.aggImport = &f

		return m, nil

	case "down", "j":
		if len(f.items) > 0 {
			f.cursor = (f.cursor + 1) % len(f.items)
		}

		m.settings.aggImport = &f

		return m, nil

	case "up", "k":
		if len(f.items) > 0 {
			f.cursor = (f.cursor - 1 + len(f.items)) % len(f.items)
		}

		m.settings.aggImport = &f

		return m, nil

	case " ":
		if f.cursor >= 0 && f.cursor < len(f.items) {
			id := f.items[f.cursor].ID

			next := make(map[string]bool, len(f.selected)+1)
			for k, v := range f.selected {
				next[k] = v
			}

			next[id] = !next[id]
			f.selected = next
		}

		m.settings.aggImport = &f

		return m, nil

	case "enter", "ctrl+s":
		return m.handleAggregatorImport(f)
	}

	return m, nil
}

// handleAggregatorImport builds one ordinary config.Indexer per selected
// item and saves them all as a single SaveSources call, the same
// full-replacement-set contract every other settings save already uses. A
// second enter/ctrl+s while the first import is still saving is refused
// outright: the optimistic snapshot (m.sourcesSnapshot) already holds the
// just-added sources by the time this runs once, so re-entering it before
// the first SaveSources result lands would append the same chosen items a
// second time under new, incrementally-suffixed ids (found in review — the
// reviewer reproduced it as duplicate `first`/`first-2` rows).
func (m Model) handleAggregatorImport(f aggregatorForm) (tea.Model, tea.Cmd) {
	if f.importing {
		return m, nil
	}

	if m.sources == nil {
		f.err = "no source manager configured"
		m.settings.aggImport = &f

		return m, nil
	}

	var chosen []AggregatorIndexer

	for _, it := range f.items {
		if f.selected[it.ID] {
			chosen = append(chosen, it)
		}
	}

	if len(chosen) == 0 {
		f.err = "select at least one indexer (space toggles)"
		m.settings.aggImport = &f

		return m, nil
	}

	previous := m.sourceRows()
	all := append([]config.Indexer(nil), previous...)

	for _, it := range chosen {
		all = append(all, aggregatorIndexerToSource(it, f.apiKey, all))
	}

	f.importing = true
	f.err = ""
	m.settings.aggImport = &f
	m.sourcesSnapshot = all

	return m, saveAggregatorImportCmd(m.sources, all, previous, len(chosen))
}

// handleAggregatorImportResult applies the wizard's own bulk SaveSources
// outcome: reverts the optimistic snapshot and reports the failure inline
// on error, or refreshes the search screen's live source list and closes
// the wizard on success — the same shape handleFormSaveResult already
// applies to the add/edit form's own save.
func (m Model) handleAggregatorImportResult(msg aggregatorImportResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.sourcesSnapshot = msg.previous

		if m.settings.aggImport != nil {
			m.settings.aggImport.importing = false
			m.settings.aggImport.err = msg.err.Error()
		}

		return m, nil
	}

	m = m.refreshSearchSources()
	m.settings.aggImport = nil

	return m.pushStatus(fmt.Sprintf("imported %d source(s) from aggregator", msg.count))
}

// aggregatorIndexerToSource builds the ordinary torznab config.Indexer one
// imported aggregator indexer becomes (T-083 acceptance: "each import
// becomes an ordinary [[indexer]] entry"). Its id comes from the
// aggregator's own name, slugified and de-duplicated against existing —
// the same rule the add/edit form's own resolvedID already follows,
// applied here against the growing "existing" set so two imports whose
// names collide with each other, not just with a saved source, still get
// distinct ids. it.FeedURL is used verbatim: building an aggregator's own
// per-indexer URL pattern is the SourceManager implementation's job, never
// this package's (see AggregatorIndexer's doc comment).
func aggregatorIndexerToSource(it AggregatorIndexer, apiKey string, existing []config.Indexer) config.Indexer {
	base := slugify(it.Name)
	id := base

	for n := 2; idCollides(id, existing); n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}

	return config.Indexer{
		ID:      id,
		Name:    it.Name,
		Type:    "torznab",
		URL:     it.FeedURL,
		APIKey:  apiKey,
		Enabled: true,
	}
}

// idCollides reports whether id is already used by one of existing.
func idCollides(id string, existing []config.Indexer) bool {
	for _, s := range existing {
		if s.ID == id {
			return true
		}
	}

	return false
}

// aggregatorImportLegend documents the wizard's own keys, the same
// one-line-legend convention settingsScreenLegend and preferencesLegend
// already use.
const (
	aggregatorImportInputLegend = "tab/shift+tab move · ctrl+r reveal key · enter fetch · esc cancel"
	aggregatorImportListLegend  = "j/k move · space select · enter/ctrl+s import selected · esc back"
)

// renderAggregatorImport draws the wizard: the input step or the fetched
// list, whichever is current. Pure (AGENT.md §6.8).
func (m Model) renderAggregatorImport() string {
	th := m.theme
	f := m.settings.aggImport

	if f == nil {
		return truncateLines(th.Muted.Render("aggregator import is not open"), m.width)
	}

	inner := max(m.width-4, 20)
	textW := inner - 2

	var b strings.Builder

	b.WriteString(th.Accent.Render("Import from aggregator"))
	b.WriteString("\n\n")

	if f.phase == aggregatorPhaseList {
		m.renderAggregatorList(&b, th, textW, *f)
	} else {
		m.renderAggregatorInputFields(&b, th, textW, *f)
	}

	if f.err != "" {
		b.WriteString("\n")
		b.WriteString(th.Error.Render(f.err))
		b.WriteString("\n")
	} else if f.fetching {
		b.WriteString("\n")
		b.WriteString(th.Muted.Render("fetching…"))
		b.WriteString("\n")
	} else if f.importing {
		b.WriteString("\n")
		b.WriteString(th.Muted.Render("importing…"))
		b.WriteString("\n")
	}

	body := th.Border.Width(inner).Render(strings.TrimRight(b.String(), "\n"))

	legend := aggregatorImportInputLegend
	if f.phase == aggregatorPhaseList {
		legend = aggregatorImportListLegend
	}

	return body + "\n" + th.Muted.Render(wrapLegend(legend, m.width))
}

func (m Model) renderAggregatorInputFields(b *strings.Builder, th theme.Theme, textW int, f aggregatorForm) {
	for i := aggregatorInputField(0); i < numAggregatorInputFields; i++ {
		val := f.baseURL
		if i == aggregatorFieldAPIKey {
			val = maskSecret(f.apiKey, f.reveal)
		}

		if val == "" {
			val = "(empty)"
		}

		line := theme.Truncate(fmt.Sprintf("%-16s %s", i.label()+":", val), textW)

		if int(i) == f.cursor {
			b.WriteString(th.Accent.Render(line))
		} else {
			b.WriteString(th.Foreground.Render(line))
		}

		b.WriteString("\n")
	}
}

func (m Model) renderAggregatorList(b *strings.Builder, th theme.Theme, textW int, f aggregatorForm) {
	if len(f.items) == 0 {
		b.WriteString(th.Muted.Render("the aggregator reported no indexers"))
		b.WriteString("\n")

		return
	}

	for i, it := range f.items {
		box := "[ ]"
		if f.selected[it.ID] {
			box = "[x]"
		}

		line := theme.Truncate(fmt.Sprintf("%s %s", box, it.Name), textW)

		if i == f.cursor {
			b.WriteString(th.Accent.Render(line))
		} else {
			b.WriteString(th.Foreground.Render(line))
		}

		b.WriteString("\n")
	}
}
