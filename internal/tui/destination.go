// Destination picker (T-074): the step of the add flow (details.go) that
// shows where a torrent's data will go and lets the user change it before
// the engine is handed the torrent. It offers the configured default, the
// saved destinations (most recently used first), the most recently used
// other destinations, and a free-text path; every candidate is validated
// live — exists or will be created, writable, free space against the
// torrent's size — and a failing one blocks the add with the reason on
// screen. A folder that does not exist yet is created, with its parents,
// only after the user confirms.
//
// Every destination the user adds a torrent to joins the known-roots set
// (AGENT.md §6.12): the engine's, through engine.RootAdder, and the TUI's
// own for open/reveal, recorded through DestinationStore so both survive a
// restart. Nothing else ever widens that set.
package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/indexer"
	"github.com/kdta91/tortui/internal/platform"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// DestinationStore is the subset of *store.Store that remembers which
// destinations the user has added torrents to, most recently used first.
// nil is valid: the picker then offers only the default and the saved
// destinations, and a used destination is a known root for this session
// only.
type DestinationStore interface {
	Destinations() []string
	TouchDestination(path string) error
}

// maxRecentDestinations bounds how many "recent" rows the picker offers.
// It bounds the list only: every used destination stays a known root.
const maxRecentDestinations = 3

// destKind labels a picker row.
type destKind string

const (
	destDefault destKind = "Default"
	destSaved   destKind = "Saved"
	destRecent  destKind = "Recent"
)

// destEntry is one fixed row of the picker: a labelled absolute path.
type destEntry struct {
	kind destKind
	path string
}

// destPicker is the picker's state while it is open. cursor indexes entries;
// cursor == len(entries) is the free-text path field.
type destPicker struct {
	open    bool
	result  indexer.Result
	entries []destEntry
	cursor  int
	input   string
	// check is the most recent validation for the highlighted candidate;
	// it is ignored unless check.path is that candidate.
	check destCheck
	// confirmCreate is true while the picker asks whether to create a
	// destination that does not exist yet.
	confirmCreate bool
}

// onField reports whether the cursor is on the free-text path field.
func (p destPicker) onField() bool { return p.cursor >= len(p.entries) }

// destProbe holds the filesystem queries a validation makes, so a test can
// report a full disk or an unwritable folder without needing one.
type destProbe struct {
	freeSpace func(path string) (uint64, error)
	writable  func(dir string) error
}

// defaultDestProbe queries the real filesystem.
func defaultDestProbe() destProbe {
	return destProbe{freeSpace: platform.FreeSpace, writable: probeWritable}
}

// destCheck is the outcome of validating one candidate destination.
type destCheck struct {
	path string
	// problem is set when the path cannot be a destination at all (a file
	// is in the way, it cannot be read).
	problem  string
	exists   bool
	writeErr error
	free     uint64
	freeErr  error
	// size is the torrent's size (0 when the source did not say) and
	// margin the configured minimum free space kept on top of it (T-034).
	size   int64
	margin int64
}

// need is how many free bytes the destination must have, or 0 when the
// torrent's size is unknown.
func (c destCheck) need() int64 {
	if c.size <= 0 {
		return 0
	}

	return c.size + c.margin
}

// blockReason is why the add must not go ahead into c.path, or "".
func (c destCheck) blockReason() string {
	switch {
	case c.problem != "":
		return c.problem
	case c.writeErr != nil:
		return fmt.Sprintf("not writable: %v", c.writeErr)
	case c.freeErr != nil:
		return fmt.Sprintf("couldn't read free space: %v", c.freeErr)
	case c.need() > 0 && c.free < uint64(c.need()):
		return fmt.Sprintf("not enough space: needs %s, %s free (short by %s)",
			formatSize(c.need()), formatSize(freeBytes(c.free)), formatSize(c.need()-freeBytes(c.free)))
	default:
		return ""
	}
}

// freeBytes converts a free-space reading for formatSize, saturating rather
// than wrapping for a value past int64.
func freeBytes(n uint64) int64 {
	return int64(min(n, uint64(1<<62)))
}

// checkDestination validates path (a cleaned absolute path) for a torrent of
// size bytes. It touches the filesystem, so it only ever runs inside a
// tea.Cmd (AGENT.md §6.1).
func checkDestination(path string, size, margin int64, probe destProbe) destCheck {
	c := destCheck{path: path, size: size, margin: margin}

	info, err := os.Stat(path)

	switch {
	case err == nil && !info.IsDir():
		c.problem = "a file, not a folder, is already at this path"
		return c
	case err == nil:
		c.exists = true
	case errors.Is(err, os.ErrNotExist):
		// Will be created on confirm; the writable probe below checks the
		// nearest existing ancestor instead.
	default:
		// ENOTDIR and friends: a file somewhere up the chain says it
		// best; anything else is reported as it is.
		if _, aerr := nearestExistingDir(path); aerr != nil {
			c.problem = aerr.Error()
		} else {
			c.problem = fmt.Sprintf("can't read this path: %v", err)
		}

		return c
	}

	dir := path
	if !c.exists {
		ancestor, err := nearestExistingDir(path)
		if err != nil {
			c.problem = err.Error()
			return c
		}

		dir = ancestor
	}

	c.writeErr = probe.writable(dir)
	c.free, c.freeErr = probe.freeSpace(path)

	return c
}

// nearestExistingDir walks up from path to the first entry that exists and
// requires it to be a folder: a file anywhere up the chain means the
// destination could never be created.
func nearestExistingDir(path string) (string, error) {
	dir := filepath.Dir(path)

	for {
		info, err := os.Stat(dir)
		if err == nil {
			if !info.IsDir() {
				return "", fmt.Errorf("can't create a folder here: %s is a file", dir)
			}

			return dir, nil
		}

		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("can't read %s: %w", dir, err)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no existing folder above %s", path)
		}

		dir = parent
	}
}

// probeWritable reports whether a file can be created in dir, by creating
// and removing an empty one: the only check that is truthful across POSIX
// permissions, Windows ACLs, and read-only mounts alike.
func probeWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".tortui-write-probe-*")
	if err != nil {
		return err
	}

	name := f.Name()

	if err := f.Close(); err != nil {
		return errors.Join(err, os.Remove(name))
	}

	return os.Remove(name)
}

// windowsAbsPattern matches a Windows drive-letter (C:\, C:/) or UNC
// (\\server) path, which filepath only treats as absolute on Windows.
var windowsAbsPattern = regexp.MustCompile(`^([A-Za-z]:[\\/]|\\\\)`)

// envPattern matches $NAME, ${NAME}, and %NAME%.
var envPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)|%([A-Za-z_][A-Za-z0-9_()]*)%`)

// expandDestination turns what the user typed into a cleaned absolute
// destination path:
//
//   - $NAME, ${NAME}, and %NAME% are environment variables, and an unset
//     one is an error rather than silently empty;
//   - then a leading ~ is the home directory (~user is refused), whose own
//     text is never itself expanded;
//   - both / and \ are separators, normalised to the OS's own;
//   - a Windows drive-letter or UNC path is accepted where the OS
//     understands it (filepath.IsAbs) and refused elsewhere;
//   - a relative path resolves against base, the default download
//     directory — never the process working directory.
//
// The result must be fit to be a destination root
// (engine.CheckDestinationRoot): not a drive root, no NUL byte.
func expandDestination(input, base string, lookupEnv func(string) (string, bool), home func() (string, error)) (string, error) {
	p := strings.TrimSpace(input)
	if p == "" {
		return "", errors.New("type a folder path")
	}

	if strings.ContainsRune(p, 0) {
		return "", errors.New("path contains a NUL byte")
	}

	var missing []string

	p = envPattern.ReplaceAllStringFunc(p, func(m string) string {
		sub := envPattern.FindStringSubmatch(m)
		name := sub[1] + sub[2] + sub[3]

		v, ok := lookupEnv(name)
		if !ok {
			missing = append(missing, name)
		}

		return v
	})

	if len(missing) > 0 {
		return "", fmt.Errorf("environment variable %s is not set", missing[0])
	}

	if strings.HasPrefix(p, "~") {
		rest := p[1:]
		if rest != "" && rest[0] != '/' && rest[0] != '\\' {
			return "", errors.New("~user paths are not supported; use ~ or a full path")
		}

		h, err := home()
		if err != nil {
			return "", fmt.Errorf("can't find your home folder: %w", err)
		}

		p = h + rest
	}

	raw := p
	p = filepath.FromSlash(strings.ReplaceAll(p, `\`, "/"))

	// filepath only gives a volume name to a drive or UNC path on Windows,
	// so this refuses them exactly where the OS cannot use them.
	if windowsAbsPattern.MatchString(raw) && filepath.VolumeName(p) == "" {
		return "", errors.New("a Windows drive or network path is not valid on this system")
	}

	if filepath.VolumeName(p) != "" && !filepath.IsAbs(p) {
		return "", errors.New("drive-relative paths are not supported; use a full path like C:\\Downloads")
	}

	if !filepath.IsAbs(p) {
		if !filepath.IsAbs(base) {
			return "", errors.New("no default download folder to resolve a relative path against; type a full path")
		}

		p = filepath.Join(base, p)
	}

	abs, err := engine.CheckDestinationRoot(filepath.Clean(p))
	if err != nil {
		return "", errors.New(strings.TrimPrefix(err.Error(), engine.ErrUnsafePath.Error()+": "))
	}

	return abs, nil
}

// destinationEntries builds the picker's fixed rows: the default download
// directory, the saved destinations ordered most recently used first (then
// in their configured order), and up to maxRecentDestinations other used
// destinations. Each path appears once; one that could not be a destination
// root (relative, a drive root) is left out.
func (m Model) destinationEntries() []destEntry {
	var out []destEntry

	seen := make(map[string]bool)

	add := func(kind destKind, p string) bool {
		abs, err := engine.CheckDestinationRoot(p)
		if err != nil || seen[abs] {
			return false
		}

		seen[abs] = true
		out = append(out, destEntry{kind: kind, path: abs})

		return true
	}

	add(destDefault, m.downloadDir)

	for _, s := range savedByRecency(m.savedDestinations, m.usedDestinations) {
		add(destSaved, s)
	}

	recent := 0
	for _, u := range m.usedDestinations {
		if recent == maxRecentDestinations {
			break
		}

		if add(destRecent, u) {
			recent++
		}
	}

	return out
}

// savedByRecency orders saved most recently used first (by position in
// used, which is most recent first); saved destinations never used keep
// their configured order after them.
func savedByRecency(saved, used []string) []string {
	rank := make(map[string]int, len(used))
	for i, u := range used {
		if _, ok := rank[filepath.Clean(u)]; !ok {
			rank[filepath.Clean(u)] = i
		}
	}

	var usedSaved, rest []string

	for _, s := range saved {
		if _, ok := rank[filepath.Clean(s)]; ok {
			usedSaved = append(usedSaved, s)
		} else {
			rest = append(rest, s)
		}
	}

	for i := 1; i < len(usedSaved); i++ {
		for j := i; j > 0 && rank[filepath.Clean(usedSaved[j])] < rank[filepath.Clean(usedSaved[j-1])]; j-- {
			usedSaved[j], usedSaved[j-1] = usedSaved[j-1], usedSaved[j]
		}
	}

	return append(usedSaved, rest...)
}

// destCandidate is the destination the picker's cursor currently names.
func (m Model) destCandidate() (string, error) {
	if !m.dest.onField() {
		return m.dest.entries[m.dest.cursor].path, nil
	}

	return expandDestination(m.dest.input, m.downloadDir, m.lookupEnv, m.homeDir)
}

// destCheckMsg carries one checkDestinationCmd outcome back into Update.
type destCheckMsg struct{ check destCheck }

// checkDestinationCmd validates path off Update's goroutine.
func checkDestinationCmd(path string, size, margin int64, probe destProbe) tea.Cmd {
	return func() tea.Msg {
		return destCheckMsg{check: checkDestination(path, size, margin, probe)}
	}
}

// openDestinationPicker is the add flow's destination step (finishAdd): it
// opens the picker for r with the cursor on the default and starts
// validating it.
func (m Model) openDestinationPicker(r indexer.Result) (tea.Model, tea.Cmd) {
	m.dest = destPicker{open: true, result: r, entries: m.destinationEntries()}

	return m.revalidateDestination()
}

// revalidateDestination starts validating the highlighted candidate after
// the cursor moved or the typed path changed. An unparseable typed path has
// nothing to check on disk: its error is shown straight from destCandidate.
func (m Model) revalidateDestination() (tea.Model, tea.Cmd) {
	m.dest.confirmCreate = false

	path, err := m.destCandidate()
	if err != nil {
		return m, nil
	}

	return m, checkDestinationCmd(path, m.dest.result.SizeBytes, m.minFreeSpace, m.destProbe)
}

// handleDestCheck records a validation outcome, provided it is for the
// candidate still highlighted — a result for a path the user has since
// typed past or moved off is stale and dropped.
func (m Model) handleDestCheck(msg destCheckMsg) (tea.Model, tea.Cmd) {
	if !m.dest.open {
		return m, nil
	}

	if path, err := m.destCandidate(); err == nil && path == msg.check.path {
		m.dest.check = msg.check
	}

	return m, nil
}

// handleDestinationKey handles every key while the picker is open. Typing
// on the path field is claimed before the keymap lookup, so letters that
// mean something elsewhere (j, k, q, ...) type into the path instead.
func (m Model) handleDestinationKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.dest.confirmCreate {
		switch msg.String() {
		case "enter", "y":
			return m.confirmDestination(true)
		case "esc", "n":
			m.dest.confirmCreate = false
		}

		return m, nil
	}

	if m.dest.onField() {
		switch msg.Type {
		case tea.KeyRunes:
			m.dest.input += strings.Map(dropNewlines, string(msg.Runes))
			return m.revalidateDestination()
		case tea.KeySpace:
			m.dest.input += " "
			return m.revalidateDestination()
		case tea.KeyBackspace:
			if r := []rune(m.dest.input); len(r) > 0 {
				m.dest.input = string(r[:len(r)-1])
			}

			return m.revalidateDestination()
		}
	}

	action, ok := m.keys.Lookup(ContextDestination, msg.String())
	if !ok {
		return m, nil
	}

	switch action {
	case ActionMoveUp:
		if m.dest.cursor > 0 {
			m.dest.cursor--
		}

		return m.revalidateDestination()
	case ActionMoveDown:
		if !m.dest.onField() {
			m.dest.cursor++
		}

		return m.revalidateDestination()
	case ActionConfirmYes:
		return m.confirmDestination(false)
	case ActionCancel:
		title := m.dest.result.Title
		m.dest = destPicker{}

		return m.pushStatus("add cancelled: " + title)
	default:
		return m, nil
	}
}

// dropNewlines removes line breaks from pasted text: a path copied with its
// trailing newline must not become part of the folder name.
func dropNewlines(r rune) rune {
	if r == '\n' || r == '\r' {
		return -1
	}

	return r
}

// confirmDestination is enter on the picker. The add goes ahead only for a
// candidate whose validation has come back clean; a destination that does
// not exist yet first asks to be created (create is true once it has).
func (m Model) confirmDestination(create bool) (tea.Model, tea.Cmd) {
	path, err := m.destCandidate()
	if err != nil {
		return m.pushStatus(fmt.Sprintf("can't use this destination: %v", err))
	}

	c := m.dest.check
	if c.path != path {
		return m.pushStatus("still checking " + path)
	}

	if reason := c.blockReason(); reason != "" {
		return m.pushStatus("can't add here: " + reason)
	}

	if !c.exists && !create {
		m.dest.confirmCreate = true
		return m, nil
	}

	r := m.dest.result
	m.dest = destPicker{}

	src := engine.AddSource{Magnet: r.Magnet, TorrentURL: r.TorrentURL, SavePath: path}

	return m, addTorrentCmd(m.eng, src, !c.exists, r.Title, r.IndexerID, r.SourceURL)
}

// prepareDestination runs inside the add's tea.Cmd, before engine.Add: it
// creates the destination (with parents) when the user confirmed that, and
// admits it to the engine's known roots when the engine checks roots.
func prepareDestination(eng engine.Engine, dir string, create bool) error {
	if create {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}

	if ra, ok := eng.(engine.RootAdder); ok {
		if err := ra.AddRoot(dir); err != nil {
			return err
		}
	}

	return nil
}

// recordDestination makes dir a used destination after a successful add:
// most recent first in this session's list (and so a known root for
// open/reveal) and in the store for the next session.
func (m Model) recordDestination(dir string) (Model, error) {
	if strings.TrimSpace(dir) == "" {
		return m, nil
	}

	used := make([]string, 0, len(m.usedDestinations)+1)
	used = append(used, dir)

	for _, u := range m.usedDestinations {
		if u != dir {
			used = append(used, u)
		}
	}

	m.usedDestinations = used

	if m.destStore == nil {
		return m, nil
	}

	return m, m.destStore.TouchDestination(dir)
}

// renderDestinationPicker draws the open picker: the torrent, the rows, the
// highlighted candidate's validation, and the keys. Pure (AGENT.md §6.8).
func (m Model) renderDestinationPicker() string {
	th := m.theme
	// The box is inner columns wide inside its border, including one
	// column of padding each side (theme.Border), so text gets inner-2.
	inner := max(m.width-4, 12)
	textW := inner - 2

	var b strings.Builder

	r := m.dest.result
	b.WriteString(th.Accent.Render(theme.Truncate("Add "+r.Title, textW)))
	b.WriteString("\n")

	size := "size unknown"
	if r.SizeBytes > 0 {
		size = formatSize(r.SizeBytes)
	}

	b.WriteString(th.Muted.Render(size))
	b.WriteString("\n\n")

	start, end := m.destWindow()
	for i := start; i < end; i++ {
		b.WriteString(m.destRow(i, textW))
		b.WriteString("\n")
	}

	b.WriteString("\n")

	for _, line := range m.destStatusLines() {
		for _, w := range theme.Wrap(line.text, textW) {
			b.WriteString(line.style.Render(w))
			b.WriteString("\n")
		}
	}

	body := th.Border.Width(inner).Render(strings.TrimRight(b.String(), "\n"))

	return body + "\n" + th.Muted.Render(theme.Truncate(m.destHelp(), m.width))
}

// destWindow is the range of rows (entries plus the path field) shown, kept
// around the cursor so the picker fits the terminal however many saved
// destinations there are.
func (m Model) destWindow() (int, int) {
	total := len(m.dest.entries) + 1
	visible := total

	if m.height > 0 {
		visible = min(total, max(m.height-17, 3))
	}

	start := 0
	if m.dest.cursor >= visible {
		start = m.dest.cursor - visible + 1
	}

	return start, start + visible
}

// destRow renders row i: a fixed entry, or the path field.
func (m Model) destRow(i, width int) string {
	th := m.theme
	marker := "  "

	if i == m.dest.cursor {
		marker = "> "
	}

	var line string

	if i < len(m.dest.entries) {
		e := m.dest.entries[i]
		line = fmt.Sprintf("%s%-8s %s", marker, e.kind, e.path)
	} else {
		text := m.dest.input
		if text == "" && i != m.dest.cursor {
			text = "type a path (~, $VAR, relative to the default)"
		}

		if i == m.dest.cursor {
			text += "_"
		}

		line = fmt.Sprintf("%s%-8s %s", marker, "Path", text)
	}

	line = theme.Truncate(line, width)

	if i == m.dest.cursor {
		return th.Accent.Render(line)
	}

	return th.Foreground.Render(line)
}

// destStatusLine is one styled line of the validation panel.
type destStatusLine struct {
	text  string
	style lipgloss.Style
}

// destStatusLines renders the highlighted candidate's validation: where the
// data will go, and one line per check, each passed or failed with its
// reason — or the create prompt once the user asked to add into a folder
// that does not exist yet.
func (m Model) destStatusLines() []destStatusLine {
	th := m.theme
	ok := th.Glyphs.Check + " "
	bad := "! "

	path, err := m.destCandidate()
	if err != nil {
		return []destStatusLine{{text: bad + err.Error(), style: th.Error}}
	}

	lines := []destStatusLine{{text: "Into " + path, style: th.Foreground}}

	c := m.dest.check
	if c.path != path {
		return append(lines, destStatusLine{text: "checking…", style: th.Muted})
	}

	if c.problem != "" {
		return append(lines, destStatusLine{text: bad + c.problem, style: th.Error})
	}

	if c.exists {
		lines = append(lines, destStatusLine{text: ok + "exists", style: th.Foreground})
	} else {
		lines = append(lines, destStatusLine{text: ok + "will be created, with any missing parent folders", style: th.Foreground})
	}

	if c.writeErr != nil {
		lines = append(lines, destStatusLine{text: bad + fmt.Sprintf("not writable: %v", c.writeErr), style: th.Error})
	} else {
		lines = append(lines, destStatusLine{text: ok + "writable", style: th.Foreground})
	}

	switch {
	case c.freeErr != nil:
		lines = append(lines, destStatusLine{text: bad + fmt.Sprintf("couldn't read free space: %v", c.freeErr), style: th.Error})
	case c.need() > 0 && c.free < uint64(c.need()):
		lines = append(lines, destStatusLine{text: bad + c.blockReason(), style: th.Error})
	case c.need() > 0:
		lines = append(lines, destStatusLine{
			text:  ok + fmt.Sprintf("%s free, needs %s", formatSize(freeBytes(c.free)), formatSize(c.need())),
			style: th.Foreground,
		})
	default:
		lines = append(lines, destStatusLine{
			text:  ok + fmt.Sprintf("%s free (torrent size unknown)", formatSize(freeBytes(c.free))),
			style: th.Foreground,
		})
	}

	if m.dest.confirmCreate {
		lines = append(lines, destStatusLine{text: "Create " + path + " and any missing parent folders?", style: th.Accent})
	}

	return lines
}

// destHelp is the one-line key hint under the picker, generated from the
// ContextDestination bindings (keymap.go) so it cannot drift from them.
func (m Model) destHelp() string {
	if m.dest.confirmCreate {
		return "enter/y create folder and add · esc/n back"
	}

	lines := m.keys.HelpFor(ContextDestination)
	for i, l := range lines {
		lines[i] = strings.Join(strings.Fields(l), " ")
	}

	return strings.Join(lines, " · ")
}
