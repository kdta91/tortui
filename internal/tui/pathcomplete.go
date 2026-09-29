package tui

// Shell-style path completion for the add-source form's "Import from"
// field (T-9019). The directory read is a tea.Cmd, never Update
// (AGENT.md §6.1); a result whose text no longer matches the field is
// dropped. Home expansion uses os.UserHomeDir, the same call
// internal/platform's path resolvers use; nothing here branches on the OS.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/platform"
)

// userHomeDir is a seam so tests can pin the home directory.
var userHomeDir = os.UserHomeDir

// WithDefinitionsDir tells the settings form where the user's scraper
// definitions live, so completion on an empty import field starts there.
func WithDefinitionsDir(dir string) Option {
	return func(m *Model) { m.definitionsDir = dir }
}

// pathCompletion is the cycling state left behind by an applied completion.
// It is only valid while the import field still holds exactly applied.
type pathCompletion struct {
	matches []string
	idx     int // index of the match last applied, -1 after a prefix-only completion
	applied string
}

// isURLSource reports whether s is an http(s) URL, which is never completed.
func isURLSource(s string) bool {
	l := strings.ToLower(strings.TrimSpace(s))

	return strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://")
}

// expandHome expands a leading "~" or "~/" (or "~\" on Windows) to the
// user's home directory. Anything else is returned unchanged.
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p
	}

	home, err := userHomeDir()
	if err != nil || home == "" {
		return p
	}

	return filepath.Join(home, p[1:])
}

func isSep(r byte) bool { return r == '/' || r == '\\' && platform.IsWindows() }

// completionMatches lists every full replacement text for input: directories
// (with a trailing separator) and .yml/.yaml files. It reads the disk, so it
// only runs inside a tea.Cmd. The input's own spelling (a "~/" prefix, its
// separator) is preserved in every match.
func completionMatches(input, defsDir string) []string {
	if isURLSource(input) {
		return nil
	}

	dirText, base := "", input

	if i := strings.LastIndexFunc(input, func(r rune) bool { return r < 128 && isSep(byte(r)) }); i >= 0 {
		dirText, base = input[:i+1], input[i+1:]
	}

	var realDir string

	switch {
	case input == "":
		if defsDir == "" {
			return nil
		}

		realDir = defsDir
		dirText = withTrailingSep(defsDir, "")
	case dirText == "":
		if input == "~" {
			return nil
		}

		realDir = "."
	default:
		realDir = expandHome(dirText)
	}

	entries, err := os.ReadDir(realDir)
	if err != nil {
		return nil
	}

	sep := string(filepath.Separator)
	if strings.Contains(dirText, "/") && !strings.Contains(dirText, `\`) {
		sep = "/"
	}

	var out []string

	for _, e := range entries {
		name := e.Name()

		if !hasNamePrefix(name, base) || (strings.HasPrefix(name, ".") && !strings.HasPrefix(base, ".")) {
			continue
		}

		isDir := e.IsDir()
		if e.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(filepath.Join(realDir, name)); err == nil {
				isDir = st.IsDir()
			}
		}

		switch {
		case isDir:
			out = append(out, dirText+name+sep)
		case hasYAMLExt(name):
			out = append(out, dirText+name)
		}
	}

	sort.Strings(out)

	return out
}

func withTrailingSep(dir, _ string) string {
	if dir == "" || os.IsPathSeparator(dir[len(dir)-1]) {
		return dir
	}

	return dir + string(filepath.Separator)
}

func hasNamePrefix(name, prefix string) bool {
	if platform.IsWindows() {
		return strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix))
	}

	return strings.HasPrefix(name, prefix)
}

func hasYAMLExt(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".yml", ".yaml":
		return true
	}

	return false
}

// longestCommonPrefix returns the longest common prefix of ss.
func longestCommonPrefix(ss []string) string {
	if len(ss) == 0 {
		return ""
	}

	p := ss[0]
	for _, s := range ss[1:] {
		for !strings.HasPrefix(s, p) {
			p = p[:len(p)-1]
		}
	}

	return p
}

// formCompleteMsg carries a completion read's result back to Update.
type formCompleteMsg struct {
	gen     int
	text    string // the field text the read started from
	matches []string
}

func completeCmd(gen int, text, defsDir string) tea.Cmd {
	return func() tea.Msg {
		return formCompleteMsg{gen: gen, text: text, matches: completionMatches(text, defsDir)}
	}
}

// advanceField moves the form cursor to the next field, as plain tab does.
func (f sourceForm) advanceField() sourceForm {
	f = f.clampCursor()
	f.cursor = (f.cursor + 1) % len(f.fields())

	return f
}

// handleImportTab handles tab on the import field. It cycles an existing
// completion synchronously, otherwise starts a directory read.
func (m Model) handleImportTab(f sourceForm) (tea.Model, tea.Cmd) {
	if c := f.completion; c != nil && len(c.matches) > 1 && f.importText == c.applied {
		c.idx = (c.idx + 1) % len(c.matches)
		c.applied = c.matches[c.idx]
		f.importText = c.applied
		f.dirty = true
		m.settings.form = &f

		return m, nil
	}

	f.completion = nil
	f.completeGen++
	m.settings.form = &f

	return m, completeCmd(f.completeGen, f.importText, m.definitionsDir)
}

// handleFormComplete applies a completion read, or falls through to the next
// field when there is nothing to complete. A stale result is dropped.
func (m Model) handleFormComplete(msg formCompleteMsg) (tea.Model, tea.Cmd) {
	if m.settings.form == nil {
		return m, nil
	}

	f := *m.settings.form
	if msg.gen != f.completeGen || f.current() != fieldImport || f.importText != msg.text {
		return m, nil
	}

	next, c := applyCompletion(msg.text, msg.matches)
	if next == "" || next == msg.text {
		f = f.advanceField()
		m.settings.form = &f

		return m, nil
	}

	f.importText = next
	f.completion = c
	f.dirty = true
	m.settings.form = &f

	return m, nil
}

// applyCompletion picks the text to show for matches: the single match, else
// the common prefix when it extends the input, else the first match.
func applyCompletion(input string, matches []string) (string, *pathCompletion) {
	switch len(matches) {
	case 0:
		return "", nil
	case 1:
		return matches[0], nil
	}

	if lcp := longestCommonPrefix(matches); len(lcp) > len(input) {
		return lcp, &pathCompletion{matches: matches, idx: -1, applied: lcp}
	}

	return matches[0], &pathCompletion{matches: matches, idx: 0, applied: matches[0]}
}
