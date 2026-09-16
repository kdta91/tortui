package tui

import (
	"testing"
)

// TestKeymapNoConflicts is the data-driven conflict check T-051 requires: it
// walks the real keymap data (AllBindings, which is exactly what
// Model.handleKey and the ? overlay are built from) rather than a
// hand-maintained list of expected bindings, so an edit to GlobalBindings
// that introduces a real same-key-two-actions collision in any context —
// including the two modal contexts — fails this test without anyone having
// to remember to update a parallel expectation list.
func TestKeymapNoConflicts(t *testing.T) {
	if conflicts := Conflicts(AllBindings()); len(conflicts) != 0 {
		t.Fatalf("keymap has %d conflict(s):\n%s", len(conflicts), joinLines(conflicts))
	}
}

func joinLines(lines []string) string {
	out := ""
	for _, l := range lines {
		out += "  " + l + "\n"
	}

	return out
}

// TestConflictsDetectsRealCollision proves Conflicts actually detects a
// same-key/different-action collision instead of vacuously passing —
// without this, TestKeymapNoConflicts finding zero conflicts would be
// equally consistent with a broken detector.
func TestConflictsDetectsRealCollision(t *testing.T) {
	bindings := []Binding{
		{Keys: []string{"g"}, Action: "action-one", Contexts: []Context{screenContext(ScreenSearch)}},
		{Keys: []string{"g"}, Action: "action-two", Contexts: []Context{screenContext(ScreenSearch)}},
	}

	got := Conflicts(bindings)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 conflict, got %d: %v", len(got), got)
	}
}

// TestConflictsAllowsAliasesAndDifferentContexts confirms two legitimate
// patterns are never flagged: multiple keys for the same action (aliases),
// and the same key bound to different actions in different, non-overlapping
// contexts (e.g. "o" for open-file on the downloads screen and unbound
// elsewhere is fine; this test uses two distinct screens explicitly).
func TestConflictsAllowsAliasesAndDifferentContexts(t *testing.T) {
	bindings := []Binding{
		{Keys: []string{"j", "down"}, Action: ActionMoveDown, Contexts: []Context{screenContext(ScreenSearch)}},
		{Keys: []string{"g"}, Action: "action-one", Contexts: []Context{screenContext(ScreenSearch)}},
		{Keys: []string{"g"}, Action: "action-two", Contexts: []Context{screenContext(ScreenResults)}},
	}

	if got := Conflicts(bindings); len(got) != 0 {
		t.Fatalf("expected no conflicts, got %v", got)
	}
}

// TestConflictsCoversModalContexts confirms a collision inside a modal
// context (help, quit-confirm) is caught the same way a screen collision
// is — the acceptance criterion explicitly calls out "including modal
// contexts".
func TestConflictsCoversModalContexts(t *testing.T) {
	bindings := []Binding{
		{Keys: []string{"y"}, Action: ActionConfirmYes, Contexts: []Context{ContextQuitConfirm}},
		{Keys: []string{"y"}, Action: ActionConfirmNo, Contexts: []Context{ContextQuitConfirm}},
	}

	got := Conflicts(bindings)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 conflict in modal context, got %d: %v", len(got), got)
	}
}

// TestGlobalBindingExpandsToEveryScreenNotModals confirms the contextGlobal
// convention: a binding with no explicit Contexts is live on every screen
// but never inside a modal, so it cannot silently collide with a modal-only
// binding.
func TestGlobalBindingExpandsToEveryScreenNotModals(t *testing.T) {
	b := Binding{Keys: []string{"q"}, Action: ActionQuit}
	ctxs := b.expand()

	if len(ctxs) != len(screenOrder) {
		t.Fatalf("expected %d contexts, got %d: %v", len(screenOrder), len(ctxs), ctxs)
	}

	for _, c := range ctxs {
		if c == ContextHelp || c == ContextQuitConfirm {
			t.Fatalf("global binding must not expand into a modal context, got %v", c)
		}
	}
}

// TestKeyMapLookup exercises the built KeyMap end to end for a screen-scoped
// binding, a global one, and a modal one, confirming Lookup only reports a
// hit where the binding is actually live.
func TestKeyMapLookup(t *testing.T) {
	km := NewKeyMap()

	if a, ok := km.Lookup(screenContext(ScreenDownloads), "p"); !ok || a != ActionPauseResume {
		t.Fatalf("downloads/p = %v,%v, want %v,true", a, ok, ActionPauseResume)
	}

	if _, ok := km.Lookup(screenContext(ScreenSearch), "p"); ok {
		t.Fatalf("search/p should not be bound (pause/resume is downloads-only)")
	}

	if a, ok := km.Lookup(screenContext(ScreenSearch), "q"); !ok || a != ActionQuit {
		t.Fatalf("search/q = %v,%v, want %v,true", a, ok, ActionQuit)
	}

	if a, ok := km.Lookup(ContextQuitConfirm, "y"); !ok || a != ActionConfirmYes {
		t.Fatalf("quit-confirm/y = %v,%v, want %v,true", a, ok, ActionConfirmYes)
	}

	if _, ok := km.Lookup(ContextQuitConfirm, "q"); ok {
		t.Fatalf("quit-confirm should not inherit the global quit binding")
	}
}

// TestHelpForIsGeneratedNotHandWritten confirms HelpFor produces one line
// per distinct action bound in a context, and that aliased keys collapse to
// a single line rather than duplicating it — the mechanism that keeps the
// overlay from drifting away from the real bindings.
func TestHelpForIsGeneratedNotHandWritten(t *testing.T) {
	km := NewKeyMap()

	lines := km.HelpFor(screenContext(ScreenSearch))
	if len(lines) == 0 {
		t.Fatal("expected at least one help line for the search screen")
	}

	moveDownLines := 0

	for _, l := range lines {
		if len(l) >= 5 && l[:5] == "j/dow" {
			moveDownLines++
		}
	}

	if moveDownLines != 1 {
		t.Fatalf("expected exactly one collapsed j/down line, found %d in %v", moveDownLines, lines)
	}
}

// TestScreenStringAndCycle covers Screen.String, next, and prev, including
// wraparound in both directions.
func TestScreenStringAndCycle(t *testing.T) {
	if got := Screen(99).String(); got != "screen(99)" {
		t.Fatalf("out-of-range Screen.String() = %q", got)
	}

	if got := ScreenSettings.next(); got != ScreenSearch {
		t.Fatalf("ScreenSettings.next() = %v, want wraparound to %v", got, ScreenSearch)
	}

	if got := ScreenSearch.prev(); got != ScreenSettings {
		t.Fatalf("ScreenSearch.prev() = %v, want wraparound to %v", got, ScreenSettings)
	}

	for _, s := range screenOrder {
		if s.next().prev() != s {
			t.Fatalf("next().prev() is not identity for %v", s)
		}
	}
}
