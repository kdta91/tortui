package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/indexer"
	indexerfake "github.com/kdta91/tortui/internal/indexer/fake"
)

// fakeBuiltinManager adds the BuiltinManager half to fakeSourceManager.
type fakeBuiltinManager struct {
	*fakeSourceManager

	bmu       sync.Mutex
	builtins  []BuiltinSource
	setErr    error
	setCalls  []string // "id=on|off"
	tested    []string
	testBErr  error
	onChanged func([]BuiltinSource)
}

func (f *fakeBuiltinManager) BuiltinSources() []BuiltinSource {
	f.bmu.Lock()
	defer f.bmu.Unlock()

	return append([]BuiltinSource(nil), f.builtins...)
}

func (f *fakeBuiltinManager) SetBuiltinEnabled(id string, enabled bool) error {
	f.bmu.Lock()
	defer f.bmu.Unlock()

	state := "off"
	if enabled {
		state = "on"
	}

	f.setCalls = append(f.setCalls, id+"="+state)

	if f.setErr != nil {
		return f.setErr
	}

	for i := range f.builtins {
		if f.builtins[i].ID == id {
			f.builtins[i].Enabled = enabled
		}
	}

	if f.onChanged != nil {
		f.onChanged(append([]BuiltinSource(nil), f.builtins...))
	}

	return nil
}

func (f *fakeBuiltinManager) TestBuiltin(_ context.Context, id string) error {
	f.bmu.Lock()
	defer f.bmu.Unlock()

	f.tested = append(f.tested, id)

	return f.testBErr
}

func (f *fakeBuiltinManager) calls() []string {
	f.bmu.Lock()
	defer f.bmu.Unlock()

	return append([]string(nil), f.setCalls...)
}

func (f *fakeBuiltinManager) testedIDs() []string {
	f.bmu.Lock()
	defer f.bmu.Unlock()

	return append([]string(nil), f.tested...)
}

func newBuiltinFake(configured ...config.Indexer) *fakeBuiltinManager {
	return &fakeBuiltinManager{
		fakeSourceManager: &fakeSourceManager{sources: configured},
		builtins:          []BuiltinSource{{ID: "archive-src", Name: "Archive Source", Enabled: true}},
	}
}

// settle runs cmd (and the commands of a batch) and returns the messages
// that arrive within a short window, dropping slow timers such as the status
// bar's expiry tick.
func settle(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}

	out := make(chan tea.Msg, 1)

	go func() { out <- cmd() }()

	select {
	case msg := <-out:
		if batch, ok := msg.(tea.BatchMsg); ok {
			var all []tea.Msg
			for _, c := range batch {
				all = append(all, settle(c)...)
			}

			return all
		}

		return []tea.Msg{msg}
	case <-time.After(150 * time.Millisecond):
		return nil
	}
}

// press feeds msg to m and keeps feeding the resulting messages back until
// none are left.
func drive(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()

	queue := []tea.Msg{msg}

	for steps := 0; len(queue) > 0; steps++ {
		if steps > 50 {
			t.Fatal("message loop did not settle")
		}

		next, cmd := m.Update(queue[0])
		queue = append(queue[1:], settle(cmd)...)

		var ok bool
		if m, ok = next.(Model); !ok {
			t.Fatalf("Update returned %T", next)
		}
	}

	return m
}

func settingsModelFor(t *testing.T, sm SourceManager, opts ...Option) Model {
	t.Helper()

	opts = append([]Option{WithSourceManager(sm)}, opts...)
	m := New(newTestEngine(t), testTheme(), opts...)
	m = drive(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	return drive(t, m, keyRune("5"))
}

func view(m Model) string { return m.View() }

func TestSettingsListsBuiltinAfterConfiguredAndTagged(t *testing.T) {
	sm := newBuiltinFake(config.Indexer{ID: "my-tracker", Name: "My Tracker", Type: "torznab", URL: "https://example.org/a", Enabled: true})
	out := view(settingsModelFor(t, sm))

	if !strings.Contains(out, "Archive Source") || !strings.Contains(out, "built-in") {
		t.Fatalf("built-in row missing or untagged:\n%s", out)
	}

	if strings.Index(out, "My Tracker") > strings.Index(out, "Archive Source") {
		t.Fatalf("configured source should come before the built-in:\n%s", out)
	}
}

// TestFreshInstallShowsTheBuiltinNotAnEmptyList is the reported bug.
func TestFreshInstallShowsTheBuiltinNotAnEmptyList(t *testing.T) {
	out := view(settingsModelFor(t, newBuiltinFake()))

	if strings.Contains(out, "No sources configured") {
		t.Fatalf("empty-state shown though a built-in source exists:\n%s", out)
	}

	if !strings.Contains(out, "Archive Source") || !strings.Contains(out, "on") {
		t.Fatalf("built-in row missing:\n%s", out)
	}
}

func TestSpaceTogglesABuiltinAndSaves(t *testing.T) {
	sm := newBuiltinFake()
	m := settingsModelFor(t, sm)

	m = drive(t, m, tea.KeyMsg{Type: tea.KeySpace})

	if got := sm.calls(); len(got) != 1 || got[0] != "archive-src=off" {
		t.Fatalf("SetBuiltinEnabled calls = %v, want archive-src=off", got)
	}

	if !strings.Contains(view(m), "off") {
		t.Fatalf("row not shown off:\n%s", view(m))
	}

	_ = drive(t, m, tea.KeyMsg{Type: tea.KeySpace})

	if got := sm.calls(); len(got) != 2 || got[1] != "archive-src=on" {
		t.Fatalf("calls = %v, want a second archive-src=on", got)
	}

	if sm.saveCallCount() != 0 {
		t.Fatal("a built-in toggle went through SaveSources")
	}
}

func TestFailedBuiltinToggleIsRevertedAndReported(t *testing.T) {
	sm := newBuiltinFake()
	sm.setErr = errors.New("disk full")
	m := settingsModelFor(t, sm)

	m = drive(t, m, tea.KeyMsg{Type: tea.KeySpace})

	out := view(m)
	if !strings.Contains(out, "on") || strings.Contains(out, " off ") {
		t.Fatalf("row not restored to on after a failed save:\n%s", out)
	}

	if !strings.Contains(out, "disk full") {
		t.Fatalf("failure not reported:\n%s", out)
	}
}

func TestEditAndRemoveAreRefusedOnABuiltin(t *testing.T) {
	sm := newBuiltinFake()
	m := settingsModelFor(t, sm)

	m = drive(t, m, keyRune("e"))

	if m.settings.form != nil {
		t.Fatal("edit opened a form for a built-in source")
	}

	if out := view(m); !strings.Contains(out, "can't be edited") {
		t.Fatalf("edit refusal not shown:\n%s", out)
	}

	// The status bar queues messages behind the one showing, so the remove
	// refusal is checked on a fresh model.
	m = settingsModelFor(t, sm)
	m = drive(t, m, keyRune("x"))

	if m.settings.removeConfirm.IsOpen() || m.settings.removeID != "" {
		t.Fatal("remove opened its confirmation for a built-in source")
	}

	if out := view(m); !strings.Contains(out, "can't be removed") {
		t.Fatalf("remove refusal not shown:\n%s", out)
	}

	if sm.saveCallCount() != 0 || len(sm.calls()) != 0 {
		t.Fatal("a refused edit or remove still saved something")
	}

	if got := len(m.builtinSnapshot); got != 1 {
		t.Fatalf("built-in rows = %d after refused remove, want 1", got)
	}
}

func TestTestKeyProbesABuiltinAndDetailWorks(t *testing.T) {
	sm := newBuiltinFake()
	sm.testBErr = errors.New("connection refused")
	m := settingsModelFor(t, sm)

	m = drive(t, m, keyRune("t"))

	if got := sm.testedIDs(); len(got) != 1 || got[0] != "archive-src" {
		t.Fatalf("TestBuiltin calls = %v", got)
	}

	if sm.testCallCount() != 0 {
		t.Fatal("a built-in probe went through TestSource")
	}

	res, ok := m.settings.lastProbe["archive-src"]
	if !ok || res.outcome != probeUnreachable {
		t.Fatalf("lastProbe = %+v, want an unreachable result for the built-in", m.settings.lastProbe)
	}

	m = drive(t, m, keyRune("d"))

	if !m.settings.detailOpen {
		t.Fatal("d did not open the test detail for a built-in")
	}

	if out := view(m); !strings.Contains(out, "Archive Source") || !strings.Contains(out, "connection refused") {
		t.Fatalf("detail panel missing the built-in result:\n%s", out)
	}
}

func TestDetailBeforeAnyBuiltinTestShowsHint(t *testing.T) {
	m := drive(t, settingsModelFor(t, newBuiltinFake()), keyRune("d"))

	if m.settings.detailOpen {
		t.Fatal("detail opened with no result")
	}
}

func TestCursorReachesBuiltinRowsPastConfiguredOnes(t *testing.T) {
	sm := newBuiltinFake(config.Indexer{ID: "my-tracker", Name: "My Tracker", Type: "torznab", URL: "https://example.org/a", Enabled: true})
	m := settingsModelFor(t, sm)

	m = drive(t, m, keyRune("j"))
	m = drive(t, m, keyRune("j")) // clamped

	if b, ok := m.selectedBuiltin(); !ok || b.ID != "archive-src" {
		t.Fatalf("cursor %d does not select the built-in row", m.settings.cursor)
	}

	_ = drive(t, m, tea.KeyMsg{Type: tea.KeySpace})

	if got := sm.calls(); len(got) != 1 {
		t.Fatalf("calls = %v, want one built-in toggle", got)
	}

	if sm.saveCallCount() != 0 {
		t.Fatal("toggling the built-in re-saved the configured sources")
	}
}

// TestSearchScreenAgreesWithTheSettingsToggle: the Search source list is the
// registry's, refreshed once the toggle is saved.
func TestSearchScreenAgreesWithTheSettingsToggle(t *testing.T) {
	ix := indexerfake.New("archive-src", "Archive Source", indexer.Caps{Search: true, Latest: true}, nil)
	searcher := &dynamicSearcher{}
	searcher.setEnabled([]indexer.Indexer{ix})

	sm := newBuiltinFake()
	sm.onChanged = func(all []BuiltinSource) {
		var live []indexer.Indexer
		for _, b := range all {
			if b.ID == "archive-src" && b.Enabled {
				live = append(live, ix)
			}
		}

		searcher.setEnabled(live)
	}

	m := settingsModelFor(t, sm, WithSearcher(searcher))

	m = drive(t, m, keyRune("1"))
	if !strings.Contains(view(m), "[x] archive-src") {
		t.Fatalf("search screen lacks the enabled built-in:\n%s", view(m))
	}

	m = drive(t, m, keyRune("5"))
	m = drive(t, m, tea.KeyMsg{Type: tea.KeySpace})
	m = drive(t, m, keyRune("1"))

	if strings.Contains(view(m), "archive-src") {
		t.Fatalf("search screen still offers the disabled built-in:\n%s", view(m))
	}

	m = drive(t, m, keyRune("5"))
	m = drive(t, m, tea.KeyMsg{Type: tea.KeySpace})
	m = drive(t, m, keyRune("1"))

	if !strings.Contains(view(m), "archive-src") {
		t.Fatalf("search screen lost the re-enabled built-in:\n%s", view(m))
	}
}

// TestSettingsWithoutABuiltinManagerStillWorks covers the demo and plain
// fakes: no built-in rows, and every key on the empty list is harmless.
func TestSettingsWithoutABuiltinManagerStillWorks(t *testing.T) {
	m := New(newTestEngine(t), testTheme()) // no source manager at all (--demo)
	m = drive(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m = drive(t, m, keyRune("5"))

	for _, k := range []tea.Msg{tea.KeyMsg{Type: tea.KeySpace}, keyRune("t"), keyRune("e"), keyRune("x"), keyRune("d"), keyRune("j")} {
		m = drive(t, m, k)
	}

	if !strings.Contains(view(m), "No sources configured") {
		t.Fatalf("empty state missing:\n%s", view(m))
	}

	plain := settingsModelFor(t, &fakeSourceManager{})
	if len(plain.builtinSnapshot) != 0 || !strings.Contains(view(plain), "No sources configured") {
		t.Fatal("a manager without BuiltinManager grew built-in rows")
	}
}
