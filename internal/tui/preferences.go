// Preferences panel (T-082): the settings screen's second view, reached
// with "p" from the source list. It edits every remaining knob AGENT.md §7's
// settings screen owns beyond indexer management (T-080) and connection
// testing (T-081): the default download directory (with existence,
// writability, and free-space validation, reusing destination.go's own
// checkDestination), the saved-destinations list (add/rename/remove,
// most-recent-first), rate limits, max active downloads, max peers, listen
// port, seeding policy/ratio/duration, minimum free space, search timeout,
// theme, and ASCII mode.
//
// Every field is validated inline and rejected with a reason — never
// silently clamped (liveIssues below, checked on every render the same way
// settings.go's sourceForm.liveIssues already is). A field the running
// process can actually pick up live (the download directory, the saved
// destinations, and the free-space margin — all TUI-owned state, never a
// property of the frozen engine.Engine contract, AGENT.md §5) is applied to
// the model immediately on save; every other field needs a restart to take
// effect — its label says so explicitly rather than pretending otherwise
// (DEC-122).
package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/tui/components"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// PreferencesManager is the preferences panel's source of truth: the
// current configuration and the only path that persists edits to disk. A
// concrete implementation lives in the composition root (internal/app),
// which is the only place that may import internal/config's file-writing
// Save — this package only ever sees the interface, the same seam
// SourceManager already establishes for indexer management (AGENT.md §4).
type PreferencesManager interface {
	// Config returns the current configuration. Called fresh whenever the
	// preferences panel opens; Update/View never call it directly
	// afterwards — see Model.configSnapshot's doc comment.
	Config() config.Config

	// SaveConfig persists cfg as the full replacement configuration. A
	// caller building cfg starts from a snapshot this same interface
	// returned and changes only the preference fields this panel owns, so
	// a concurrent indexer edit (T-080's SaveSources) is never clobbered
	// by a stale copy of Indexers.
	SaveConfig(cfg config.Config) error
}

// WithPreferencesManager wires pm as the preferences panel's source of
// truth. Without it, the panel refuses to open and reports "no preferences
// manager configured" via the status bar.
func WithPreferencesManager(pm PreferencesManager) Option {
	return func(m *Model) { m.prefsManager = pm }
}

// settingsView selects which of the settings screen's two panels is shown.
type settingsView int

const (
	settingsViewSources settingsView = iota
	settingsViewPreferences
)

// prefsFieldKind is one row of the preferences form. The fixed fields come
// first, in display order; a row per saved destination follows, then the
// trailing "add a destination" row.
type prefsFieldKind int

const (
	prefFieldDownloadDir prefsFieldKind = iota
	prefFieldMaxDownloadRate
	prefFieldMaxUploadRate
	prefFieldMaxActiveDownloads
	prefFieldMaxPeers
	prefFieldListenPort
	prefFieldSeedPolicy
	prefFieldSeedRatio
	prefFieldSeedDuration
	prefFieldMinFreeSpace
	prefFieldSearchTimeout
	prefFieldTheme
	prefFieldASCII
	// numFixedPrefFields must stay last among the fixed fields: everything
	// at or after this ordinal in the row list is a destination row.
	numFixedPrefFields
)

// restartRequired reports whether changing this field needs a restart to
// take effect: everything that is not TUI-owned model state the settings
// screen can apply directly (DEC-122). The download directory, the saved
// destinations, and the free-space margin are the only exceptions.
func (k prefsFieldKind) restartRequired() bool {
	switch k {
	case prefFieldDownloadDir, prefFieldMinFreeSpace:
		return false
	default:
		return true
	}
}

// label names the field for the panel's row list.
func (k prefsFieldKind) label() string {
	switch k {
	case prefFieldDownloadDir:
		return "Download directory"
	case prefFieldMaxDownloadRate:
		return "Max download rate"
	case prefFieldMaxUploadRate:
		return "Max upload rate"
	case prefFieldMaxActiveDownloads:
		return "Max active downloads"
	case prefFieldMaxPeers:
		return "Max peers"
	case prefFieldListenPort:
		return "Listen port"
	case prefFieldSeedPolicy:
		return "Seed policy"
	case prefFieldSeedRatio:
		return "Seed ratio"
	case prefFieldSeedDuration:
		return "Seed duration"
	case prefFieldMinFreeSpace:
		return "Minimum free space"
	case prefFieldSearchTimeout:
		return "Search timeout"
	case prefFieldTheme:
		return "Theme"
	case prefFieldASCII:
		return "ASCII mode"
	default:
		return ""
	}
}

// prefsForm is the preferences panel's own editable state.
type prefsForm struct {
	downloadDir        string
	maxDownloadRate    string
	maxUploadRate      string
	maxActiveDownloads string
	maxPeers           string
	listenPort         string
	seedPolicy         string
	seedRatio          string
	seedDuration       string
	minFreeSpace       string
	searchTimeout      string
	theme              string
	ascii              bool

	// destinations is the saved-destinations working copy, most-recent-
	// first (initialised from savedByRecency so the panel's order matches
	// the destination picker's own).
	destinations []string

	cursor         int
	dirty          bool
	confirmDiscard bool

	// downloadDirCheck is the most recent existence/writability/free-space
	// validation for the download-dir field, run off a tea.Cmd
	// (AGENT.md §6.1) the same way destination.go's own picker validates a
	// candidate. checkedPath is which path it is for, so a stale result
	// for a path the user has since typed past is dropped.
	downloadDirCheck destCheck
	checkedPath      string

	err  string
	info string

	// removeConfirm asks before dropping a saved destination; removeIndex
	// is which one (T-082 acceptance: "warns if any active torrent is
	// downloading there").
	removeConfirm components.Dialog
	removeIndex   int
}

// newPrefsForm builds the form from cfg and the model's own used-
// destinations history (for most-recent-first ordering).
func newPrefsForm(cfg config.Config, used []string) prefsForm {
	return prefsForm{
		downloadDir:        cfg.DownloadDir,
		maxDownloadRate:    strconv.FormatInt(cfg.MaxDownloadRate, 10),
		maxUploadRate:      strconv.FormatInt(cfg.MaxUploadRate, 10),
		maxActiveDownloads: strconv.Itoa(cfg.MaxActiveDownloads),
		maxPeers:           strconv.Itoa(cfg.MaxPeers),
		listenPort:         strconv.Itoa(cfg.ListenPort),
		seedPolicy:         cfg.SeedPolicy,
		seedRatio:          strconv.FormatFloat(cfg.SeedRatio, 'g', -1, 64),
		seedDuration:       cfg.SeedDuration,
		minFreeSpace:       cfg.MinFreeSpace,
		searchTimeout:      cfg.SearchTimeout,
		theme:              cfg.Theme,
		ascii:              cfg.ASCII,
		destinations:       savedByRecency(cfg.SavedDestinations, used),
		removeConfirm:      newDestRemoveDialog(),
	}
}

func newDestRemoveDialog() components.Dialog {
	return components.NewDialog("Remove destination?", "", []components.DialogOption{
		0: {Label: "Remove"},
		1: {Label: "Cancel"},
	}, 1)
}

const (
	destRemoveDialogRemove = 0
	destRemoveDialogCancel = 1
)

// rowCount is the total number of navigable rows: the fixed fields, one
// per saved destination, and the trailing "add" row.
func (f prefsForm) rowCount() int { return int(numFixedPrefFields) + len(f.destinations) + 1 }

// isAddRow reports whether cursor is the trailing "add a destination" row.
func (f prefsForm) isAddRow(cursor int) bool {
	return cursor == int(numFixedPrefFields)+len(f.destinations)
}

// isDestRow reports whether cursor is one of the saved-destination rows,
// and if so, its index into f.destinations.
func (f prefsForm) isDestRow(cursor int) (int, bool) {
	i := cursor - int(numFixedPrefFields)
	if i >= 0 && i < len(f.destinations) {
		return i, true
	}

	return 0, false
}

// clampCursor keeps cursor in range after a destination is added or removed.
func (f prefsForm) clampCursor() prefsForm {
	n := f.rowCount()
	if f.cursor >= n {
		f.cursor = n - 1
	}
	if f.cursor < 0 {
		f.cursor = 0
	}

	return f
}

// valueAt returns the raw text of row cursor, or "" for a row with no text
// (the add row, or a boolean field rendered through boolLabel instead).
func (f prefsForm) valueAt(cursor int) string {
	if i, ok := f.isDestRow(cursor); ok {
		return f.destinations[i]
	}

	if f.isAddRow(cursor) {
		return ""
	}

	switch prefsFieldKind(cursor) {
	case prefFieldDownloadDir:
		return f.downloadDir
	case prefFieldMaxDownloadRate:
		return f.maxDownloadRate
	case prefFieldMaxUploadRate:
		return f.maxUploadRate
	case prefFieldMaxActiveDownloads:
		return f.maxActiveDownloads
	case prefFieldMaxPeers:
		return f.maxPeers
	case prefFieldListenPort:
		return f.listenPort
	case prefFieldSeedPolicy:
		return f.seedPolicy
	case prefFieldSeedRatio:
		return f.seedRatio
	case prefFieldSeedDuration:
		return f.seedDuration
	case prefFieldMinFreeSpace:
		return f.minFreeSpace
	case prefFieldSearchTimeout:
		return f.searchTimeout
	case prefFieldTheme:
		return f.theme
	case prefFieldASCII:
		return boolLabel(f.ascii)
	default:
		return ""
	}
}

func boolLabel(b bool) string {
	if b {
		return "on"
	}

	return "off"
}

// typedField reports whether row cursor accepts free-text typing.
// seed_policy, theme, and ascii are cycled with left/right instead
// (typedField reports false for them), the same convention sourceForm's
// Type field already uses; the add row has nothing to type into either.
func (f prefsForm) typedField(cursor int) bool {
	if f.isAddRow(cursor) {
		return false
	}

	if _, ok := f.isDestRow(cursor); ok {
		return true
	}

	switch prefsFieldKind(cursor) {
	case prefFieldSeedPolicy, prefFieldTheme, prefFieldASCII:
		return false
	default:
		return true
	}
}

// setValueAt returns a copy of f with row cursor's text set to v. A no-op
// for a row typedField reports false for.
func (f prefsForm) setValueAt(cursor int, v string) prefsForm {
	if i, ok := f.isDestRow(cursor); ok {
		f.destinations[i] = v
		return f
	}

	switch prefsFieldKind(cursor) {
	case prefFieldDownloadDir:
		f.downloadDir = v
	case prefFieldMaxDownloadRate:
		f.maxDownloadRate = v
	case prefFieldMaxUploadRate:
		f.maxUploadRate = v
	case prefFieldMaxActiveDownloads:
		f.maxActiveDownloads = v
	case prefFieldMaxPeers:
		f.maxPeers = v
	case prefFieldListenPort:
		f.listenPort = v
	case prefFieldSeedRatio:
		f.seedRatio = v
	case prefFieldSeedDuration:
		f.seedDuration = v
	case prefFieldMinFreeSpace:
		f.minFreeSpace = v
	case prefFieldSearchTimeout:
		f.searchTimeout = v
	}

	return f
}

// cycleSeedPolicy advances seed_policy through its fixed enum.
func cycleSeedPolicy(cur string, forward bool) string {
	order := []string{"ratio", "duration", "off"}

	idx := 0
	for i, v := range order {
		if v == cur {
			idx = i
			break
		}
	}

	if forward {
		idx = (idx + 1) % len(order)
	} else {
		idx = (idx - 1 + len(order)) % len(order)
	}

	return order[idx]
}

// cycleTheme advances the theme name through theme.Names().
func cycleTheme(cur string, forward bool) string {
	names := theme.Names()
	if len(names) == 0 {
		return cur
	}

	idx := 0
	for i, n := range names {
		if n == cur {
			idx = i
			break
		}
	}

	if forward {
		idx = (idx + 1) % len(names)
	} else {
		idx = (idx - 1 + len(names)) % len(names)
	}

	return names[idx]
}

// fieldIssue reports why row cursor's current value is invalid, or "" when
// it is fine. Every problem is a rejection reason shown inline — nothing
// here is ever silently clamped (T-082 acceptance).
func (f prefsForm) fieldIssue(cursor int) string {
	if i, ok := f.isDestRow(cursor); ok {
		v := strings.TrimSpace(f.destinations[i])
		if v == "" {
			return "" // an untouched blank row is dropped silently on save
		}

		if _, err := engine.CheckDestinationRoot(v); err != nil {
			return strings.TrimPrefix(err.Error(), engine.ErrUnsafePath.Error()+": ")
		}

		return ""
	}

	if f.isAddRow(cursor) {
		return ""
	}

	switch prefsFieldKind(cursor) {
	case prefFieldDownloadDir:
		if strings.TrimSpace(f.downloadDir) == "" {
			return "must not be empty"
		}

		if _, err := engine.CheckDestinationRoot(f.downloadDir); err != nil {
			return strings.TrimPrefix(err.Error(), engine.ErrUnsafePath.Error()+": ")
		}

		if f.checkedPath == f.downloadDir {
			if reason := f.downloadDirCheck.blockReason(); reason != "" {
				return reason
			}
		}

		return ""
	case prefFieldMaxDownloadRate:
		return byteRateIssue(f.maxDownloadRate)
	case prefFieldMaxUploadRate:
		return byteRateIssue(f.maxUploadRate)
	case prefFieldMaxActiveDownloads:
		n, err := strconv.Atoi(f.maxActiveDownloads)
		if err != nil {
			return "must be a whole number"
		}
		if n < 1 {
			return "must be at least 1"
		}

		return ""
	case prefFieldMaxPeers:
		n, err := strconv.Atoi(f.maxPeers)
		if err != nil {
			return "must be a whole number"
		}
		if n < 1 {
			return "must be at least 1"
		}

		return ""
	case prefFieldListenPort:
		n, err := strconv.Atoi(f.listenPort)
		if err != nil {
			return "must be a whole number"
		}
		if n < 0 || n > 65535 {
			return "must be between 0 and 65535"
		}

		return ""
	case prefFieldSeedRatio:
		v, err := strconv.ParseFloat(f.seedRatio, 64)
		if err != nil {
			return "must be a number"
		}
		if v < 0 {
			return "must not be negative"
		}

		return ""
	case prefFieldSeedDuration:
		d, err := time.ParseDuration(f.seedDuration)
		if err != nil {
			return err.Error()
		}
		if d <= 0 {
			return "must be positive"
		}

		return ""
	case prefFieldMinFreeSpace:
		if _, err := config.ParseByteSize(f.minFreeSpace); err != nil {
			return err.Error()
		}

		return ""
	case prefFieldSearchTimeout:
		d, err := time.ParseDuration(f.searchTimeout)
		if err != nil {
			return err.Error()
		}
		if d <= 0 {
			return "must be positive"
		}

		return ""
	default:
		return ""
	}
}

// byteRateIssue validates a rate field: a plain non-negative byte count or
// a human size (config.ParseByteSize handles both), or "" (empty means
// "leave at 0/unlimited" is not itself allowed — an explicit "0" is
// required so a blank field is never silently treated as a value).
func byteRateIssue(v string) string {
	n, err := config.ParseByteSize(v)
	if err != nil {
		return err.Error()
	}
	if n < 0 {
		return "must not be negative"
	}

	return ""
}

// liveIssues reports every current problem across every row, for the
// panel's inline validation list (mirrors sourceForm.liveIssues).
func (f prefsForm) liveIssues() []string {
	var issues []string

	for i := 0; i < f.rowCount(); i++ {
		if reason := f.fieldIssue(i); reason != "" {
			issues = append(issues, fmt.Sprintf("%s: %s", f.rowLabel(i), reason))
		}
	}

	return issues
}

// rowLabel names row cursor for the issue list and the row itself.
func (f prefsForm) rowLabel(cursor int) string {
	if i, ok := f.isDestRow(cursor); ok {
		return fmt.Sprintf("Destination %d", i+1)
	}

	if f.isAddRow(cursor) {
		return "+ add a destination"
	}

	return prefsFieldKind(cursor).label()
}

// applyTo returns a copy of base with every field this panel owns set from
// f, and the cleaned, de-duplicated, non-blank destination list. base
// carries whatever else the configuration holds (Indexers, most of all)
// unchanged.
func (f prefsForm) applyTo(base config.Config) config.Config {
	cfg := base

	cfg.DownloadDir = strings.TrimSpace(f.downloadDir)
	cfg.MaxDownloadRate, _ = config.ParseByteSize(f.maxDownloadRate)
	cfg.MaxUploadRate, _ = config.ParseByteSize(f.maxUploadRate)
	cfg.MaxActiveDownloads, _ = strconv.Atoi(f.maxActiveDownloads)
	cfg.MaxPeers, _ = strconv.Atoi(f.maxPeers)
	cfg.ListenPort, _ = strconv.Atoi(f.listenPort)
	cfg.SeedPolicy = f.seedPolicy
	cfg.SeedRatio, _ = strconv.ParseFloat(f.seedRatio, 64)
	cfg.SeedDuration = f.seedDuration
	cfg.MinFreeSpace = strings.TrimSpace(f.minFreeSpace)
	cfg.SearchTimeout = f.searchTimeout
	cfg.Theme = f.theme
	cfg.ASCII = f.ascii

	dests := make([]string, 0, len(f.destinations))

	seen := make(map[string]bool, len(f.destinations))

	for _, d := range f.destinations {
		v := strings.TrimSpace(d)
		if v == "" || seen[v] {
			continue
		}

		seen[v] = true
		dests = append(dests, v)
	}

	cfg.SavedDestinations = dests

	return cfg
}

// changedRestartFields reports which restart-required fields differ
// between old and next, for the save-success status message.
func changedRestartFields(old, next config.Config) []string {
	var changed []string

	add := func(cond bool, name string) {
		if cond {
			changed = append(changed, name)
		}
	}

	add(old.MaxDownloadRate != next.MaxDownloadRate, "max_download_rate")
	add(old.MaxUploadRate != next.MaxUploadRate, "max_upload_rate")
	add(old.MaxActiveDownloads != next.MaxActiveDownloads, "max_active_downloads")
	add(old.MaxPeers != next.MaxPeers, "max_peers")
	add(old.ListenPort != next.ListenPort, "listen_port")
	add(old.SeedPolicy != next.SeedPolicy, "seed_policy")
	add(old.SeedRatio != next.SeedRatio, "seed_ratio")
	add(old.SeedDuration != next.SeedDuration, "seed_duration")
	add(old.SearchTimeout != next.SearchTimeout, "search_timeout")
	add(old.Theme != next.Theme, "theme")
	add(old.ASCII != next.ASCII, "ascii")

	return changed
}

// handlePreferencesOpen implements "p" from the source list: opens the
// preferences panel pre-filled from the current configuration.
func (m Model) handlePreferencesOpen() (tea.Model, tea.Cmd) {
	if m.prefsManager == nil {
		return m.pushStatus("no preferences manager configured")
	}

	f := newPrefsForm(m.configSnapshot, m.usedDestinations)
	m.settings.view = settingsViewPreferences
	m.settings.prefsForm = &f

	return m.revalidatePrefsDownloadDir()
}

// revalidatePrefsDownloadDir dispatches the download-dir field's existence/
// writability/free-space check (AGENT.md §6.1: never inline in Update).
func (m Model) revalidatePrefsDownloadDir() (tea.Model, tea.Cmd) {
	f := m.settings.prefsForm
	if f == nil {
		return m, nil
	}

	path := strings.TrimSpace(f.downloadDir)
	if _, err := engine.CheckDestinationRoot(path); err != nil {
		return m, nil
	}

	return m, checkPrefsDownloadDirCmd(path, m.minFreeSpace, m.destProbe)
}

// prefsDownloadDirCheckMsg carries one download-dir validation outcome,
// distinct from destCheckMsg (the add flow's own picker) so the two never
// cross-apply to the wrong panel.
type prefsDownloadDirCheckMsg struct{ check destCheck }

func checkPrefsDownloadDirCmd(path string, margin int64, probe destProbe) tea.Cmd {
	return func() tea.Msg {
		return prefsDownloadDirCheckMsg{check: checkDestination(path, 0, margin, probe)}
	}
}

// handlePrefsDownloadDirCheck records a validation outcome, provided it is
// still for the path currently typed — a result for a path the user has
// since edited past is stale and dropped, the same rule
// handleDestCheck already applies to the add-flow picker.
func (m Model) handlePrefsDownloadDirCheck(msg prefsDownloadDirCheckMsg) (tea.Model, tea.Cmd) {
	f := m.settings.prefsForm
	if f == nil {
		return m, nil
	}

	if strings.TrimSpace(f.downloadDir) == msg.check.path {
		f.downloadDirCheck = msg.check
		f.checkedPath = msg.check.path
	}

	return m, nil
}

// handlePreferencesKey handles every key while the preferences panel
// (ContextPreferences) is open and its own destination-remove confirm is
// not. Typing on a text row is claimed first, the same "dedicated handler
// owns the whole modal" pattern settings.go's handleSourceFormKey already
// established.
func (m Model) handlePreferencesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := *m.settings.prefsForm

	if f.confirmDiscard {
		switch msg.String() {
		case "y", "enter":
			m.settings.prefsForm = nil
			m.settings.view = settingsViewSources
		case "n", "esc":
			f.confirmDiscard = false
			m.settings.prefsForm = &f
		}

		return m, nil
	}

	if f.removeConfirm.IsOpen() {
		switch msg.String() {
		case "j", "down":
			f.removeConfirm = f.removeConfirm.MoveNext()
		case "k", "up":
			f.removeConfirm = f.removeConfirm.MovePrev()
		case "esc", "n":
			f.removeConfirm = f.removeConfirm.Cancel()
		case "enter":
			var choice int
			f.removeConfirm, choice = f.removeConfirm.Confirm()

			if choice == destRemoveDialogRemove && f.removeIndex < len(f.destinations) {
				f.destinations = append(f.destinations[:f.removeIndex], f.destinations[f.removeIndex+1:]...)
				f.dirty = true
				f = f.clampCursor()
			}
		}

		m.settings.prefsForm = &f

		return m, nil
	}

	switch msg.String() {
	case "esc":
		if f.dirty {
			f.confirmDiscard = true
			m.settings.prefsForm = &f

			return m, nil
		}

		m.settings.prefsForm = nil
		m.settings.view = settingsViewSources

		return m, nil

	case "tab", "down":
		f = f.clampCursor()
		f.cursor = (f.cursor + 1) % f.rowCount()
		m.settings.prefsForm = &f

		return m, nil

	case "shift+tab", "up":
		f = f.clampCursor()
		n := f.rowCount()
		f.cursor = (f.cursor - 1 + n) % n
		m.settings.prefsForm = &f

		return m, nil

	case "ctrl+s":
		return m.handlePrefsSave(f)

	case "ctrl+x":
		if i, ok := f.isDestRow(f.cursor); ok {
			f.removeIndex = i
			f.removeConfirm.Message = fmt.Sprintf("Remove %q?", f.destinations[i])
			f.removeConfirm = f.removeConfirm.Open()
			m.settings.prefsForm = &f
		}

		return m, nil

	case "enter":
		if f.isAddRow(f.cursor) {
			f.destinations = append(f.destinations, "")
			f.cursor = int(numFixedPrefFields) + len(f.destinations) - 1
			f.dirty = true
			m.settings.prefsForm = &f

			return m, nil
		}

		return m.handlePrefsSave(f)

	case "left", "right", " ":
		switch prefsFieldKind(f.cursor) {
		case prefFieldSeedPolicy:
			f.seedPolicy = cycleSeedPolicy(f.seedPolicy, msg.String() != "left")
			f.dirty = true
			m.settings.prefsForm = &f

			return m, nil
		case prefFieldTheme:
			f.theme = cycleTheme(f.theme, msg.String() != "left")
			f.dirty = true
			m.settings.prefsForm = &f

			return m, nil
		case prefFieldASCII:
			f.ascii = !f.ascii
			f.dirty = true
			m.settings.prefsForm = &f

			return m, nil
		}
	}

	if !f.typedField(f.cursor) {
		return m, nil
	}

	switch msg.Type {
	case tea.KeyRunes:
		text := string(msg.Runes)
		f = f.setValueAt(f.cursor, f.valueAt(f.cursor)+text)
		f.dirty = true
		m.settings.prefsForm = &f

		if prefsFieldKind(f.cursor) == prefFieldDownloadDir {
			return m.revalidatePrefsDownloadDir()
		}

		return m, nil

	case tea.KeyBackspace:
		f = f.setValueAt(f.cursor, trimLastRune(f.valueAt(f.cursor)))
		f.dirty = true
		m.settings.prefsForm = &f

		if prefsFieldKind(f.cursor) == prefFieldDownloadDir {
			return m.revalidatePrefsDownloadDir()
		}

		return m, nil
	}

	return m, nil
}

// handlePrefsSave validates every row and, only once none has a problem,
// dispatches the save.
func (m Model) handlePrefsSave(f prefsForm) (tea.Model, tea.Cmd) {
	if issues := f.liveIssues(); len(issues) > 0 {
		f.err = issues[0]
		m.settings.prefsForm = &f

		return m, nil
	}

	if m.prefsManager == nil {
		f.err = "no preferences manager configured"
		m.settings.prefsForm = &f

		return m, nil
	}

	previous := m.configSnapshot
	next := f.applyTo(previous)

	f.err = ""
	m.settings.prefsForm = &f

	return m, savePrefsCmd(m.prefsManager, next, previous)
}

// prefsSaveResultMsg reports SaveConfig's outcome.
type prefsSaveResultMsg struct {
	err      error
	applied  config.Config
	previous config.Config
}

func savePrefsCmd(pm PreferencesManager, next, previous config.Config) tea.Cmd {
	return func() tea.Msg {
		return prefsSaveResultMsg{err: pm.SaveConfig(next), applied: next, previous: previous}
	}
}

// handlePrefsSaveResult applies a save attempt: on success, updates the
// snapshot, applies the live-appliable fields to the model immediately, and
// reports which restart-required fields changed, if any; on failure, shows
// the error inline and leaves the form open with the snapshot unchanged.
func (m Model) handlePrefsSaveResult(msg prefsSaveResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		if m.settings.prefsForm != nil {
			f := *m.settings.prefsForm
			f.err = msg.err.Error()
			m.settings.prefsForm = &f
		}

		return m.pushStatus(fmt.Sprintf("couldn't save preferences: %v", msg.err))
	}

	m.configSnapshot = msg.applied
	m.downloadDir = msg.applied.DownloadDir
	m.savedDestinations = append([]string(nil), msg.applied.SavedDestinations...)
	if n, err := config.ParseByteSize(msg.applied.MinFreeSpace); err == nil {
		m.minFreeSpace = n
	}

	m.settings.prefsForm = nil
	m.settings.view = settingsViewSources

	status := "preferences saved"
	if changed := changedRestartFields(msg.previous, msg.applied); len(changed) > 0 {
		status += " — restart to apply: " + strings.Join(changed, ", ")
	}

	return m.pushStatus(status)
}

// destinationInUse reports whether any currently tracked torrent's save
// path resolves inside dir — the T-082 acceptance's "warns if any active
// torrent is downloading there." Removing dir from SavedDestinations never
// itself narrows the known-roots set those torrents already widened
// (destinationRoots/destinationEntries always add every tracked torrent's
// own SavePath regardless of SavedDestinations, AGENT.md §6.12) — this
// check exists purely to warn the user, not to protect that invariant,
// which already holds structurally.
func (m Model) destinationInUse(dir string) bool {
	abs, err := engine.CheckDestinationRoot(dir)
	if err != nil {
		return false
	}

	for _, s := range m.torrentStatuses {
		if s.SavePath == "" {
			continue
		}

		switch s.State {
		case engine.StateDownloading, engine.StateChecking, engine.StateQueued, engine.StateSeeding:
		default:
			continue
		}

		if path, err := engine.CheckDestinationRoot(s.SavePath); err == nil && engine.ContainedIn(abs, path) {
			return true
		}
	}

	return false
}

// preferencesLegend documents the panel's own keys, the same one-line
// legend convention settingsScreenLegend already uses for the source list.
const preferencesLegend = "tab/shift+tab move · left/right cycle/toggle · enter on \"add\" adds a destination, saves elsewhere · ctrl+x remove destination · ctrl+s save · esc back"

// renderPreferencesScreen draws the open preferences panel. Pure
// (AGENT.md §6.8).
func (m Model) renderPreferencesScreen() string {
	th := m.theme
	f := m.settings.prefsForm

	if f == nil {
		return truncateLines(th.Muted.Render("preferences panel is not open"), m.width)
	}

	inner := max(m.width-4, 20)
	textW := inner - 2

	var b strings.Builder

	b.WriteString(th.Accent.Render("Preferences"))
	b.WriteString("\n\n")

	for i := 0; i < f.rowCount(); i++ {
		label := f.rowLabel(i)
		if i < int(numFixedPrefFields) && prefsFieldKind(i).restartRequired() {
			label += " (restart required)"
		}

		val := f.valueAt(i)
		if val == "" && !f.isAddRow(i) {
			val = "(empty)"
		}

		line := theme.Truncate(fmt.Sprintf("%-32s %s", label+":", val), textW)
		if f.isAddRow(i) {
			line = theme.Truncate(f.rowLabel(i), textW)
		}

		if i == f.cursor {
			b.WriteString(th.Accent.Render(line))
		} else {
			b.WriteString(th.Foreground.Render(line))
		}

		b.WriteString("\n")
	}

	if f.removeConfirm.IsOpen() {
		b.WriteString("\n")

		dest := ""
		if f.removeIndex < len(f.destinations) {
			dest = f.destinations[f.removeIndex]
		}

		if m.destinationInUse(dest) {
			b.WriteString(th.Error.Render("! an active download is using this destination"))
			b.WriteString("\n")
		}

		b.WriteString(f.removeConfirm.View(th, textW))
		b.WriteString("\n")
	}

	if f.confirmDiscard {
		b.WriteString("\n")
		b.WriteString(th.Accent.Render("Discard changes? y/enter confirm · n/esc back"))
		b.WriteString("\n")
	}

	if live := f.liveIssues(); len(live) > 0 {
		b.WriteString("\n")

		for _, issue := range live {
			b.WriteString(th.Error.Render("! " + issue))
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

	return body + "\n" + th.Muted.Render(theme.Truncate(preferencesLegend, m.width))
}
