package tui

import (
	"strings"
)

// GlobalKeymapReadmeTable renders the exact markdown table README.md embeds
// between the "<!-- keymap:start -->"/"<!-- keymap:end -->" markers, one row
// per GlobalBindings() entry, in the same order, so README's Keys section
// (T-090) can be generated rather than hand-typed and can never say
// anything GlobalBindings itself doesn't say.
// TestReadmeKeysTableMatchesGlobalBindings (keymap_docs_test.go) fails the
// build the moment the two drift.
func GlobalKeymapReadmeTable() string {
	var b strings.Builder

	b.WriteString("| Key | Action | Screen |\n")
	b.WriteString("|---|---|---|\n")

	for _, bind := range GlobalBindings() {
		b.WriteString("| ")
		b.WriteString(formatKeys(bind.Keys))
		b.WriteString(" | ")
		b.WriteString(bind.Help)
		b.WriteString(" | ")
		b.WriteString(formatScope(bind.Contexts))
		b.WriteString(" |\n")
	}

	return strings.TrimRight(b.String(), "\n")
}

// formatKeys renders a Binding's aliases as backtick-quoted, "/"-joined
// tokens, e.g. []string{"j", "down"} -> "`j` / `down`".
func formatKeys(keys []string) string {
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = "`" + k + "`"
	}

	return strings.Join(parts, " / ")
}

// formatScope renders where a Binding applies: "global" for an empty
// Contexts slice or one naming contextGlobal (Binding.expand's own rule —
// see keymap.go), otherwise the screen names a screen-scoped Contexts
// slice names, comma-joined in screenOrder.
func formatScope(contexts []Context) string {
	for _, c := range contexts {
		if c == contextGlobal {
			return "global"
		}
	}

	if len(contexts) == 0 {
		return "global"
	}

	want := make(map[Context]bool, len(contexts))
	for _, c := range contexts {
		want[c] = true
	}

	names := make([]string, 0, len(contexts))

	for _, s := range screenOrder {
		if ctx := screenContext(s); want[ctx] {
			names = append(names, s.String())
		}
	}

	return strings.Join(names, ", ")
}
