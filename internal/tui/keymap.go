// Package tui is the top-level bubbletea program: the root Model that owns
// screen routing, the global keymap, and the help overlay. It imports only
// the source-agnostic engine.Engine and indexer.Indexer interfaces (AGENT.md
// §4) — never a concrete implementation — so it can be developed and tested
// end-to-end against internal/engine/fake with no network and no swarm.
//
// Two of the five screens (downloads, settings) are still placeholders
// here: this package only routes to them. Their real content belongs to
// later tasks (T-071 downloads, T-080 settings), as does the shared
// modal/confirm component's use on those screens. Search (T-060), results
// (T-061), and details (T-063) are real: search.go owns the query input,
// mode selector, source multi-select, and category/min-seeders filters;
// results.go owns the sortable results table; details.go owns a single
// result's full information, the `u` open-source-in-browser action, and the
// basic add-to-engine path enter drives from there (T-070 builds resolve,
// dedup, and destination selection on top of it). The responsive table
// (T-053) is implemented in internal/tui/components. The status bar (T-052)
// is also implemented in internal/tui/components and wired in here as
// root.go's bottom line and ContextErrorDetail modal.
//
// details.go also owns the add flow (T-070): Resolve, duplicate-infohash
// detection, Origin persistence, and destination resolution, reachable from
// either the details screen's or the results screen's own enter key.
package tui

import (
	"fmt"
	"sort"
)

// Screen identifies one of the five top-level views AGENT.md §7 defines.
type Screen int

const (
	// ScreenSearch is the query input, mode selector, and source picker.
	ScreenSearch Screen = iota
	// ScreenResults is the sortable table of merged search results.
	ScreenResults
	// ScreenDetails is a single result's full information.
	ScreenDetails
	// ScreenDownloads is the active/completed torrent list.
	ScreenDownloads
	// ScreenSettings is indexer, download, and theme configuration.
	ScreenSettings
)

// screenOrder is the fixed tab/shift-tab and 1-5 cycle order (AGENT.md §7).
var screenOrder = []Screen{
	ScreenSearch,
	ScreenResults,
	ScreenDetails,
	ScreenDownloads,
	ScreenSettings,
}

// String names the screen for tab labels, placeholder bodies, and test
// failure messages. An out-of-range value renders as screen(N) rather than
// panicking or masquerading as a known screen.
func (s Screen) String() string {
	switch s {
	case ScreenSearch:
		return "search"
	case ScreenResults:
		return "results"
	case ScreenDetails:
		return "details"
	case ScreenDownloads:
		return "downloads"
	case ScreenSettings:
		return "settings"
	default:
		return fmt.Sprintf("screen(%d)", int(s))
	}
}

// placeholderTask names the tracker task that owns this screen's real
// content, shown in the placeholder body so a reader landing on a screen
// mid-build knows where its implementation lives.
func (s Screen) placeholderTask() string {
	switch s {
	case ScreenSearch:
		return "T-060"
	case ScreenResults:
		return "T-061"
	case ScreenDetails:
		return "T-063"
	case ScreenDownloads:
		return "T-071"
	case ScreenSettings:
		return "T-080"
	default:
		return ""
	}
}

// next returns the screen after s in screenOrder, wrapping around.
func (s Screen) next() Screen {
	for i, sc := range screenOrder {
		if sc == s {
			return screenOrder[(i+1)%len(screenOrder)]
		}
	}

	return screenOrder[0]
}

// prev returns the screen before s in screenOrder, wrapping around.
func (s Screen) prev() Screen {
	for i, sc := range screenOrder {
		if sc == s {
			return screenOrder[(i-1+len(screenOrder))%len(screenOrder)]
		}
	}

	return screenOrder[0]
}

// Action identifies what a key press means. It is opaque to the keymap
// itself — Model.handleKey is what interprets each one — which is what
// keeps the conflict-detection logic below independent of root's Update.
type Action string

// The full set of actions the global keymap and the two modal contexts
// (help overlay, quit confirmation) can bind a key to. Several — sort,
// open-file, pause/resume, and so on — belong to screens this task does not
// implement; root's Update treats them as a no-op today and later tasks
// (T-052-T-054, T-060-T-080) give them behaviour without touching the
// binding itself.
const (
	ActionQuit          Action = "quit"
	ActionHelp          Action = "help"
	ActionFocusSearch   Action = "focus-search"
	ActionLatest        Action = "latest"
	ActionRefresh       Action = "refresh"
	ActionNextScreen    Action = "next-screen"
	ActionPrevScreen    Action = "prev-screen"
	ActionGotoSearch    Action = "goto-search"
	ActionGotoResults   Action = "goto-results"
	ActionGotoDetails   Action = "goto-details"
	ActionGotoDownloads Action = "goto-downloads"
	ActionGotoSettings  Action = "goto-settings"
	ActionMoveUp        Action = "move-up"
	ActionMoveDown      Action = "move-down"
	ActionSelect        Action = "select"
	ActionDetails       Action = "details"
	ActionSortCycle     Action = "sort-cycle"
	ActionSortReverse   Action = "sort-reverse"
	// ActionToggleTrustFilter is the results screen's "t" key (T-062):
	// restricts the results table to TrustTrusted and above, toggling off
	// again on a second press.
	ActionToggleTrustFilter Action = "toggle-trust-filter"
	ActionOpenFile          Action = "open-file"
	ActionOpenFolder        Action = "open-folder"
	ActionOpenSource        Action = "open-source"
	ActionPauseResume       Action = "pause-resume"
	ActionRemove            Action = "remove"
	ActionConfirmYes        Action = "confirm-yes"
	ActionConfirmNo         Action = "confirm-no"
	ActionCancel            Action = "cancel"

	// ActionToggleErrorDetail opens the status bar's source-error detail
	// panel (T-052) when at least one source has failed, and — bound to a
	// different key in ContextErrorDetail — collapses it again. See
	// DEC-092 for why this is not literally bound to tab as the T-052
	// acceptance text's wording suggests.
	ActionToggleErrorDetail Action = "toggle-error-detail"
)

// Context is where a binding applies: one of the five screens (non-modal),
// or one of the two modal overlays this task owns. A later task's modal
// (T-054's generic confirm dialog) gets its own Context the same way.
type Context string

const (
	// contextGlobal is a pseudo-context: a binding declared with it (and no
	// more specific Contexts) is expanded to every screen context below,
	// never to a modal one. Modal contexts opt in explicitly, since a modal
	// deliberately narrows what's reachable.
	contextGlobal Context = "global"

	// ContextHelp is the help overlay.
	ContextHelp Context = "modal:help"
	// ContextQuitConfirm is the "quit with active downloads?" prompt.
	ContextQuitConfirm Context = "modal:quit-confirm"
	// ContextErrorDetail is the status bar's expanded source-error panel
	// (T-052). It owns tab for exactly one purpose — collapsing the panel
	// — which is safe precisely because tab's screen-cycling meaning
	// (ActionNextScreen) is bound only in the five screenContext values,
	// never in a modal context; see DEC-092.
	ContextErrorDetail Context = "modal:error-detail"
)

// screenContext names the Context a given Screen's keymap lookups use.
func screenContext(s Screen) Context {
	return Context("screen:" + s.String())
}

// allScreenContexts lists every non-modal Context, in screenOrder.
func allScreenContexts() []Context {
	ctxs := make([]Context, 0, len(screenOrder))
	for _, s := range screenOrder {
		ctxs = append(ctxs, screenContext(s))
	}

	return ctxs
}

// Binding is one key (or set of aliases for the same action) live in one or
// more Contexts.
type Binding struct {
	// Keys are the tea.KeyMsg.String() forms this binding matches, e.g.
	// "j", "down", "ctrl+c". Two entries in the same slice are aliases for
	// the same Action, not a conflict.
	Keys []string
	// Action is what pressing any of Keys does. Interpreted by
	// Model.handleKey.
	Action Action
	// Help is the short, human-readable description shown in the ?
	// overlay. It is the only place this text is written down — the
	// overlay is generated from it, never hand-duplicated (T-051
	// acceptance).
	Help string
	// Contexts lists where this binding is active. An empty slice, or one
	// containing contextGlobal, means "every screen, but no modal" —
	// AGENT.md §7's "global unless noted". A screen-scoped binding lists
	// exactly the screenContext(s) it applies to; a modal binding lists
	// ContextHelp and/or ContextQuitConfirm explicitly.
	Contexts []Context
}

// expand resolves a Binding's declared Contexts to the concrete, non-alias
// set of Context values it is actually live in.
func (b Binding) expand() []Context {
	if len(b.Contexts) == 0 {
		return allScreenContexts()
	}

	for _, c := range b.Contexts {
		if c == contextGlobal {
			return allScreenContexts()
		}
	}

	return b.Contexts
}

// GlobalBindings is the single source of truth for tortui's keymap
// (AGENT.md §7). Both key routing (Model.handleKey, via KeyMap built from
// this) and the ? help overlay are generated from it, so the two can never
// drift apart, and TestKeymapNoConflicts checks it directly rather than
// against a hand-maintained expectation list.
func GlobalBindings() []Binding {
	return []Binding{
		{Keys: []string{"/"}, Action: ActionFocusSearch, Help: "focus search input"},
		{Keys: []string{"L"}, Action: ActionLatest, Help: "latest — recent additions, no keyword"},
		{Keys: []string{"R"}, Action: ActionRefresh, Help: "refresh current results"},
		{Keys: []string{"tab"}, Action: ActionNextScreen, Help: "next screen"},
		{Keys: []string{"shift+tab"}, Action: ActionPrevScreen, Help: "previous screen"},
		{Keys: []string{"1"}, Action: ActionGotoSearch, Help: "jump to search"},
		{Keys: []string{"2"}, Action: ActionGotoResults, Help: "jump to results"},
		{Keys: []string{"3"}, Action: ActionGotoDetails, Help: "jump to details"},
		{Keys: []string{"4"}, Action: ActionGotoDownloads, Help: "jump to downloads"},
		{Keys: []string{"5"}, Action: ActionGotoSettings, Help: "jump to settings"},
		{Keys: []string{"j", "down"}, Action: ActionMoveDown, Help: "move selection down"},
		{Keys: []string{"k", "up"}, Action: ActionMoveUp, Help: "move selection up"},
		{Keys: []string{"enter"}, Action: ActionSelect, Help: "add torrent (results) / open details (downloads)"},
		{Keys: []string{"d"}, Action: ActionDetails, Help: "details"},
		{
			Keys: []string{"s"}, Action: ActionSortCycle, Help: "cycle sort column",
			Contexts: []Context{screenContext(ScreenResults)},
		},
		{
			Keys: []string{"S"}, Action: ActionSortReverse, Help: "reverse sort",
			Contexts: []Context{screenContext(ScreenResults)},
		},
		{
			Keys: []string{"t"}, Action: ActionToggleTrustFilter, Help: "toggle trust filter (Trusted and above)",
			Contexts: []Context{screenContext(ScreenResults)},
		},
		{
			Keys: []string{"o"}, Action: ActionOpenFile, Help: "open downloaded file",
			Contexts: []Context{screenContext(ScreenDownloads)},
		},
		{
			Keys: []string{"f"}, Action: ActionOpenFolder, Help: "open containing folder",
			Contexts: []Context{screenContext(ScreenDownloads)},
		},
		{Keys: []string{"u"}, Action: ActionOpenSource, Help: "open source page in browser"},
		{
			Keys: []string{"p"}, Action: ActionPauseResume, Help: "pause/resume",
			Contexts: []Context{screenContext(ScreenDownloads)},
		},
		{
			Keys: []string{"x"}, Action: ActionRemove, Help: "remove (opens keep/delete data confirm)",
			Contexts: []Context{screenContext(ScreenDownloads)},
		},
		{Keys: []string{"?"}, Action: ActionHelp, Help: "toggle this help overlay"},
		{Keys: []string{"q", "ctrl+c"}, Action: ActionQuit, Help: "quit (prompts if downloads active)"},
		{
			Keys: []string{"e"}, Action: ActionToggleErrorDetail,
			Help: "view source errors, if any (T-052; DEC-092)",
		},
	}
}

// Two search-screen-only key behaviours — esc cancels an in-flight query,
// space toggles/cycles whichever field the cursor is on — are deliberately
// *not* Bindings here. Registering them would add two lines to the "?"
// overlay for every context that includes them, and screenContext(
// ScreenSearch)'s help text already renders at exactly 24 lines: the
// budget every other screen's help text fits inside at the 80×24 floor
// AGENT.md §7 requires. Model.handleKey (root.go) handles both directly,
// ahead of the declarative Lookup this file drives, the same way it
// already has to for raw text entry (search.go's handleSearchTyping) — see
// the T-060 tracker notes for the measurement.

// errorDetailBindings are the bindings live while the status bar's
// source-error detail panel (ContextErrorDetail) is open. tab collapses it
// here — a different action than tab's screen-cycling meaning everywhere
// else, but never the same context, so TestKeymapNoConflicts stays clean
// (DEC-092).
func errorDetailBindings() []Binding {
	return []Binding{
		{
			Keys: []string{"tab"}, Action: ActionToggleErrorDetail, Help: "collapse",
			Contexts: []Context{ContextErrorDetail},
		},
		{Keys: []string{"esc"}, Action: ActionCancel, Help: "close", Contexts: []Context{ContextErrorDetail}},
	}
}

// helpOverlayBindings are the bindings live while the ? overlay itself is
// open: only escape (and ? again) close it. They are declared separately
// from GlobalBindings, which never itself lists ContextHelp, so the overlay
// is a real closed context rather than one that happens to inherit every
// screen action underneath it.
func helpOverlayBindings() []Binding {
	return []Binding{
		{Keys: []string{"?", "esc"}, Action: ActionCancel, Help: "close help", Contexts: []Context{ContextHelp}},
	}
}

// quitConfirmBindings are the bindings live while the "quit with active
// downloads?" prompt is open.
func quitConfirmBindings() []Binding {
	return []Binding{
		{Keys: []string{"y", "enter"}, Action: ActionConfirmYes, Help: "confirm quit", Contexts: []Context{ContextQuitConfirm}},
		{Keys: []string{"n", "esc"}, Action: ActionConfirmNo, Help: "cancel quit", Contexts: []Context{ContextQuitConfirm}},
	}
}

// AllBindings returns every binding tortui defines, across every context:
// the global/screen keymap plus both modal overlays. This is what
// TestKeymapNoConflicts checks and what the help overlay for a modal
// context is generated from.
func AllBindings() []Binding {
	all := GlobalBindings()
	all = append(all, helpOverlayBindings()...)
	all = append(all, quitConfirmBindings()...)
	all = append(all, errorDetailBindings()...)

	return all
}

// KeyMap is GlobalBindings (plus the modal bindings) indexed for fast
// per-context, per-key lookup by Model.handleKey.
type KeyMap struct {
	byContext map[Context]map[string]Action
	bindings  []Binding
}

// NewKeyMap builds a KeyMap from AllBindings.
func NewKeyMap() KeyMap {
	km := KeyMap{
		byContext: make(map[Context]map[string]Action),
		bindings:  AllBindings(),
	}

	for _, b := range km.bindings {
		for _, ctx := range b.expand() {
			if km.byContext[ctx] == nil {
				km.byContext[ctx] = make(map[string]Action)
			}

			for _, k := range b.Keys {
				km.byContext[ctx][k] = b.Action
			}
		}
	}

	return km
}

// Lookup returns the Action bound to key in ctx, if any.
func (km KeyMap) Lookup(ctx Context, key string) (Action, bool) {
	a, ok := km.byContext[ctx][key]
	return a, ok
}

// helpEntry is one rendered line of the ? overlay.
type helpEntry struct {
	keys string
	help string
}

// HelpFor renders the help overlay's lines for ctx: every binding live in
// that context, one line per distinct Action (so aliased keys like "j"/"down"
// share a line), sorted for a stable, deterministic render. Building this
// from the same Binding data Lookup uses is what AGENT.md's "no hand-written
// help text that can drift" requires.
func (km KeyMap) HelpFor(ctx Context) []string {
	byAction := make(map[Action]*helpEntry)
	var order []Action

	for _, b := range km.bindings {
		live := false

		for _, c := range b.expand() {
			if c == ctx {
				live = true
				break
			}
		}

		if !live {
			continue
		}

		if _, ok := byAction[b.Action]; !ok {
			byAction[b.Action] = &helpEntry{keys: joinKeys(b.Keys), help: b.Help}
			order = append(order, b.Action)
		}
	}

	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })

	lines := make([]string, 0, len(order))
	for _, a := range order {
		e := byAction[a]
		lines = append(lines, fmt.Sprintf("%-14s %s", e.keys, e.help))
	}

	return lines
}

func joinKeys(keys []string) string {
	out := keys[0]
	for _, k := range keys[1:] {
		out += "/" + k
	}

	return out
}

// Conflicts reports every case where the same key is bound to two different
// actions within the same context — including the two modal contexts. Each
// entry is a human-readable description; a nil/empty result means the
// keymap is conflict-free. This walks bindings directly rather than
// comparing against any hand-maintained expectation, so a future edit to
// GlobalBindings that introduces a real collision is caught by
// TestKeymapNoConflicts without that test itself needing to change.
func Conflicts(bindings []Binding) []string {
	seen := make(map[Context]map[string]Action)

	var conflicts []string

	for _, b := range bindings {
		for _, ctx := range b.expand() {
			if seen[ctx] == nil {
				seen[ctx] = make(map[string]Action)
			}

			for _, k := range b.Keys {
				if existing, ok := seen[ctx][k]; ok && existing != b.Action {
					conflicts = append(conflicts, fmt.Sprintf(
						"context %q: key %q is bound to both %q and %q", ctx, k, existing, b.Action,
					))

					continue
				}

				seen[ctx][k] = b.Action
			}
		}
	}

	sort.Strings(conflicts)

	return conflicts
}
