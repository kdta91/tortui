package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/engine/fake"
	"github.com/kdta91/tortui/internal/store"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// --- pure helpers (no I/O, no teatest) ------------------------------------

func TestIsDownloadCompleteSeedingIsComplete(t *testing.T) {
	s := engine.TorrentStatus{State: engine.StateSeeding, Progress: 1}
	if !isDownloadComplete(s) {
		t.Error("expected a seeding torrent to be complete")
	}
}

func TestIsDownloadCompletePausedAfterFinishingIsComplete(t *testing.T) {
	// applyPolicyLocked (internal/engine/anacrolix) reports a torrent whose
	// seed policy is satisfied as StatePaused with Progress still 1 — there
	// is no distinct TorrentStatus state for "done seeding".
	s := engine.TorrentStatus{State: engine.StatePaused, Progress: 1}
	if !isDownloadComplete(s) {
		t.Error("expected a paused-after-complete torrent to be complete")
	}
}

func TestIsDownloadCompletePausedMidDownloadIsNotComplete(t *testing.T) {
	s := engine.TorrentStatus{State: engine.StatePaused, Progress: 0.4}
	if isDownloadComplete(s) {
		t.Error("expected a paused, not-yet-complete torrent to stay in the active section")
	}
}

func TestIsDownloadCompleteErroredIsNeverComplete(t *testing.T) {
	s := engine.TorrentStatus{State: engine.StateErrored, Progress: 1}
	if isDownloadComplete(s) {
		t.Error("expected an errored torrent to never be in the completed section, even at Progress 1")
	}
}

func TestPartitionDownloadsPreservesOrderWithinEachSection(t *testing.T) {
	statuses := []engine.TorrentStatus{
		{ID: "a", State: engine.StateDownloading},
		{ID: "b", State: engine.StateSeeding, Progress: 1},
		{ID: "c", State: engine.StateQueued},
		{ID: "d", State: engine.StateSeeding, Progress: 1},
	}

	active, completed := partitionDownloads(statuses)

	if len(active) != 2 || active[0].ID != "a" || active[1].ID != "c" {
		t.Fatalf("active = %v, want [a c] in order", active)
	}
	if len(completed) != 2 || completed[0].ID != "b" || completed[1].ID != "d" {
		t.Fatalf("completed = %v, want [b d] in order", completed)
	}
}

func TestFormatETA(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{-1, "unknown"},
		{0, "done"},
		{30 * time.Second, "30s"},
		{2 * time.Minute, "2m"},
		{90 * time.Minute, "1h30m"},
	}

	for _, c := range cases {
		if got := formatETA(c.d); got != c.want {
			t.Errorf("formatETA(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestRenderProgressBarFillsProportionally(t *testing.T) {
	g := theme.UnicodeGlyphs

	full := renderProgressBar(g, 1)
	if full != strings.Repeat(g.Full, downloadBarWidth) {
		t.Errorf("renderProgressBar(1) = %q, want all-full", full)
	}

	empty := renderProgressBar(g, 0)
	if empty != strings.Repeat(g.Empty, downloadBarWidth) {
		t.Errorf("renderProgressBar(0) = %q, want all-empty", empty)
	}

	half := renderProgressBar(g, 0.5)
	if got := strings.Count(half, g.Full); got != downloadBarWidth/2 {
		t.Errorf("renderProgressBar(0.5) filled cells = %d, want %d", got, downloadBarWidth/2)
	}
}

func TestRenderProgressBarClampsOutOfRangeProgress(t *testing.T) {
	g := theme.UnicodeGlyphs

	if got := renderProgressBar(g, -1); got != strings.Repeat(g.Empty, downloadBarWidth) {
		t.Errorf("renderProgressBar(-1) = %q, want all-empty", got)
	}
	if got := renderProgressBar(g, 2); got != strings.Repeat(g.Full, downloadBarWidth) {
		t.Errorf("renderProgressBar(2) = %q, want all-full", got)
	}
}

func TestFormatRate(t *testing.T) {
	cases := []struct {
		bps  int64
		want string
	}{
		{0, "0 B/s"},
		{2048, "2 KB/s"},
		{5 * 1024 * 1024, "5.0 MB/s"},
		{3 * 1024 * 1024 * 1024, "3.0 GB/s"},
	}

	for _, c := range cases {
		if got := formatRate(c.bps); got != c.want {
			t.Errorf("formatRate(%d) = %q, want %q", c.bps, got, c.want)
		}
	}
}

func TestDownloadQueueReasonFallsBackToSnapshotPosition(t *testing.T) {
	statuses := []engine.TorrentStatus{
		{ID: "a", State: engine.StateDownloading},
		{ID: "q1", State: engine.StateQueued},
		{ID: "q2", State: engine.StateQueued},
	}

	// fake.Engine does not implement queueProvider, so this exercises the
	// fallback path.
	got := downloadQueueReason(fake.New(), statuses, "q2")
	if !strings.Contains(got, "position 2 of 2") {
		t.Errorf("downloadQueueReason = %q, want it to name position 2 of 2", got)
	}
}

// stubQueueSeedEngine wraps *fake.Engine to additionally implement
// queueProvider and seedPolicyProvider, so downloadQueueReason and
// downloadSeedPolicyText's "real engine" branches (over the fake's
// fallback) are exercised too.
type stubQueueSeedEngine struct {
	*fake.Engine
	queue  []string
	policy engine.SeedPolicy
}

func (e *stubQueueSeedEngine) Queue() []string               { return e.queue }
func (e *stubQueueSeedEngine) SeedPolicy() engine.SeedPolicy { return e.policy }

func TestDownloadQueueReasonPrefersRealQueuer(t *testing.T) {
	eng := &stubQueueSeedEngine{Engine: fake.New(), queue: []string{"x", "y", "z"}}
	t.Cleanup(func() { _ = eng.Close() })

	got := downloadQueueReason(eng, nil, "y")
	if !strings.Contains(got, "position 2 of 3") {
		t.Errorf("downloadQueueReason = %q, want it to name the real queue's position 2 of 3", got)
	}
}

func TestDownloadSeedPolicyTextFallsBackWithoutProvider(t *testing.T) {
	eng := fake.New()
	t.Cleanup(func() { _ = eng.Close() })

	if got := downloadSeedPolicyText(eng); !strings.Contains(got, "not reported") {
		t.Errorf("downloadSeedPolicyText(fake) = %q, want it to say the policy is not reported", got)
	}
}

func TestDownloadSeedPolicyTextUsesRealProvider(t *testing.T) {
	eng := &stubQueueSeedEngine{Engine: fake.New(), policy: engine.SeedPolicy{Mode: engine.SeedOff}}
	t.Cleanup(func() { _ = eng.Close() })

	if got := downloadSeedPolicyText(eng); got != eng.policy.String() {
		t.Errorf("downloadSeedPolicyText = %q, want %q", got, eng.policy.String())
	}
}

func TestDownloadOriginPrefersLiveOriginOverStore(t *testing.T) {
	ts := &stubTorrentStore{}
	_ = ts.SetTorrent(store.TorrentRecord{ID: "t1", IndexerID: "store-id", AddedAt: time.Now()})

	m := New(fake.New(), testTheme(), WithTorrentStore(ts))

	s := engine.TorrentStatus{ID: "t1", Origin: engine.Origin{IndexerID: "live-id"}}
	gotID, _ := m.downloadOrigin(s)
	if gotID != "live-id" {
		t.Errorf("downloadOrigin indexerID = %q, want the live Origin to win", gotID)
	}
}

// TestDownloadOriginFallsBackToStoreRecord is finding (d) from the T-070
// review (PR #43): a torrent added this session has a zero live
// engine.TorrentStatus.Origin — Engine.Add never populates it, since
// AddSource (frozen §5) carries no Origin field — so the Source column has
// nowhere else to look but the store record the add flow persisted
// (details.go's handleAddResult).
func TestDownloadOriginFallsBackToStoreRecord(t *testing.T) {
	addedAt := time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC)
	ts := &stubTorrentStore{}
	_ = ts.SetTorrent(store.TorrentRecord{ID: "t1", IndexerID: "archive-src", AddedAt: addedAt})

	m := New(fake.New(), testTheme(), WithTorrentStore(ts))

	s := engine.TorrentStatus{ID: "t1"} // zero Origin, as Add leaves it
	gotID, gotAt := m.downloadOrigin(s)

	if gotID != "archive-src" {
		t.Errorf("downloadOrigin indexerID = %q, want the store record's indexer id", gotID)
	}
	if !gotAt.Equal(addedAt) {
		t.Errorf("downloadOrigin addedAt = %v, want %v", gotAt, addedAt)
	}
}

func TestDownloadOriginNilStoreIsSafe(t *testing.T) {
	m := New(fake.New(), testTheme())

	gotID, gotAt := m.downloadOrigin(engine.TorrentStatus{ID: "t1"})
	if gotID != "" || !gotAt.IsZero() {
		t.Errorf("downloadOrigin with no store = (%q, %v), want (\"\", zero)", gotID, gotAt)
	}
}

func TestDownloadSourceAddedDestText(t *testing.T) {
	if got := downloadSourceText(""); got != "unknown source" {
		t.Errorf("downloadSourceText(\"\") = %q, want %q", got, "unknown source")
	}
	if got := downloadSourceText("alpha"); got != "alpha" {
		t.Errorf("downloadSourceText(alpha) = %q, want unchanged", got)
	}

	if got := downloadAddedText(time.Time{}); got != "-" {
		t.Errorf("downloadAddedText(zero) = %q, want %q", got, "-")
	}

	if got := downloadDestText(""); got != "(default)" {
		t.Errorf("downloadDestText(\"\") = %q, want %q", got, "(default)")
	}
	if got := downloadDestText("/data/x"); got != "/data/x" {
		t.Errorf("downloadDestText(/data/x) = %q, want unchanged", got)
	}
}

func TestDownloadErrorLineTruncatesUnlessExpanded(t *testing.T) {
	longMsg := strings.Repeat("insufficient free space at /very/long/destination/path ", 3)
	s := engine.TorrentStatus{State: engine.StateErrored, Err: errors.New(longMsg)}

	collapsed := downloadErrorLine(s, false)
	if !strings.Contains(collapsed, "enter to expand") {
		t.Errorf("collapsed error line = %q, want it to hint at expanding", collapsed)
	}
	if len(collapsed) >= len(longMsg) {
		t.Errorf("collapsed error line should be shorter than the full message")
	}

	expanded := downloadErrorLine(s, true)
	if !strings.Contains(expanded, longMsg) {
		t.Errorf("expanded error line = %q, want the full message present", expanded)
	}
}

func TestDownloadErrorLineNilErrIsSafe(t *testing.T) {
	s := engine.TorrentStatus{State: engine.StateErrored}
	if got := downloadErrorLine(s, true); !strings.Contains(got, "unknown error") {
		t.Errorf("downloadErrorLine(nil Err) = %q, want it to say unknown error", got)
	}
}

func TestDownloadActionHintOmitsOpenAndFolderBeforeDataExists(t *testing.T) {
	for _, state := range []engine.State{engine.StateQueued, engine.StateChecking} {
		hint := downloadActionHint(engine.TorrentStatus{State: state})
		if strings.Contains(hint, "[o]pen") || strings.Contains(hint, "[f]older") {
			t.Errorf("downloadActionHint(%v) = %q, want no open/folder hints", state, hint)
		}
	}
}

func TestDownloadActionHintPausedOffersResume(t *testing.T) {
	hint := downloadActionHint(engine.TorrentStatus{State: engine.StatePaused})
	if !strings.Contains(hint, "resume") {
		t.Errorf("downloadActionHint(paused) = %q, want a resume hint", hint)
	}
}

func TestDownloadActionHintDownloadingOffersPause(t *testing.T) {
	hint := downloadActionHint(engine.TorrentStatus{State: engine.StateDownloading})
	if !strings.Contains(hint, "[p]ause") {
		t.Errorf("downloadActionHint(downloading) = %q, want a pause hint", hint)
	}
}

func TestDownloadsModelMoveCursorClampsToRowCount(t *testing.T) {
	m := newDownloadsModel()

	m = m.moveCursor(1, 3)
	m = m.moveCursor(1, 3)
	m = m.moveCursor(1, 3) // one past the end
	if m.cursor != 2 {
		t.Fatalf("cursor = %d, want clamped to 2 (last of 3 rows)", m.cursor)
	}

	m = m.moveCursor(-100, 3)
	if m.cursor != 0 {
		t.Fatalf("cursor = %d, want clamped to 0", m.cursor)
	}
}

func TestDownloadsModelMoveCursorNoRowsResetsToZero(t *testing.T) {
	m := downloadsModel{cursor: 5}
	m = m.moveCursor(1, 0)
	if m.cursor != 0 {
		t.Fatalf("cursor with zero rows = %d, want 0", m.cursor)
	}
}

func TestDownloadsModelToggleExpanded(t *testing.T) {
	m := newDownloadsModel()

	m = m.toggleExpanded("t1")
	if m.expandedErr != "t1" {
		t.Fatalf("expandedErr = %q, want t1", m.expandedErr)
	}

	m = m.toggleExpanded("t1")
	if m.expandedErr != "" {
		t.Fatalf("expandedErr after second toggle = %q, want collapsed", m.expandedErr)
	}

	m = m.toggleExpanded("t2")
	m = m.toggleExpanded("t3")
	if m.expandedErr != "t3" {
		t.Fatalf("expandedErr = %q, want only the most recently toggled id expanded", m.expandedErr)
	}

	m = m.toggleExpanded("")
	if m.expandedErr != "t3" {
		t.Fatalf("toggling a blank id must be a no-op, got %q", m.expandedErr)
	}
}

// --- Update/handleKey wiring (no teatest) ---------------------------------

func addFakeTorrent(t *testing.T, eng *fake.Engine, magnet string, script fake.Script) string {
	t.Helper()

	eng.ScriptFor = func(engine.AddSource) fake.Script { return script }

	id, err := eng.Add(context.Background(), engine.AddSource{Magnet: magnet})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	return id
}

func TestDownloadsScreenCursorNavigationAndClamping(t *testing.T) {
	eng := fake.New()
	t.Cleanup(func() { _ = eng.Close() })

	addFakeTorrent(t, eng, "magnet:?xt=urn:btih:aaaa", nil) // stays StateQueued
	addFakeTorrent(t, eng, "magnet:?xt=urn:btih:bbbb", nil)

	m := New(eng, testTheme())
	m.screen = ScreenDownloads

	updated, _ := m.Update(engineUpdateMsg{statuses: eng.List()})
	m = updated.(Model)

	if got := len(m.downloadRows()); got != 2 {
		t.Fatalf("downloadRows() = %d, want 2", got)
	}

	updated, _ = m.handleKey(keyRune("j"))
	m = updated.(Model)
	if m.downloads.cursor != 1 {
		t.Fatalf("cursor after one j = %d, want 1", m.downloads.cursor)
	}

	// A further move-down must clamp, not run past the last row.
	updated, _ = m.handleKey(keyRune("j"))
	m = updated.(Model)
	if m.downloads.cursor != 1 {
		t.Fatalf("cursor after clamping j = %d, want still 1", m.downloads.cursor)
	}

	updated, _ = m.handleKey(keyRune("k"))
	m = updated.(Model)
	if m.downloads.cursor != 0 {
		t.Fatalf("cursor after k = %d, want 0", m.downloads.cursor)
	}
}

func TestEnterTogglesErrorDetailOnDownloadsScreen(t *testing.T) {
	eng := fake.New()
	t.Cleanup(func() { _ = eng.Close() })

	wantErr := errors.New("not enough free space at /data: 1.0 GB short")
	addFakeTorrent(t, eng, "magnet:?xt=urn:btih:cccc", fake.Errored(0, wantErr))

	m := New(eng, testTheme())
	m.screen = ScreenDownloads

	updated, _ := m.Update(engineUpdateMsg{statuses: eng.List()})
	m = updated.(Model)

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	rows := m.downloadRows()
	if len(rows) != 1 {
		t.Fatalf("downloadRows() = %d, want 1", len(rows))
	}
	if m.downloads.expandedErr != rows[0].ID {
		t.Fatalf("expandedErr = %q, want %q", m.downloads.expandedErr, rows[0].ID)
	}

	updated, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.downloads.expandedErr != "" {
		t.Fatal("expected a second enter to collapse the error detail again")
	}
}

func TestEngineUpdateClampsDownloadsCursorWhenRowsShrink(t *testing.T) {
	eng := fake.New()
	t.Cleanup(func() { _ = eng.Close() })

	addFakeTorrent(t, eng, "magnet:?xt=urn:btih:1111", nil)
	addFakeTorrent(t, eng, "magnet:?xt=urn:btih:2222", nil)

	m := New(eng, testTheme())
	m.screen = ScreenDownloads

	updated, _ := m.Update(engineUpdateMsg{statuses: eng.List()})
	m = updated.(Model)

	updated, _ = m.handleKey(keyRune("j"))
	m = updated.(Model)
	if m.downloads.cursor != 1 {
		t.Fatalf("setup: cursor = %d, want 1", m.downloads.cursor)
	}

	// Down to one row: the cursor must be re-clamped, not left pointing past
	// the end.
	updated, _ = m.Update(engineUpdateMsg{statuses: []engine.TorrentStatus{eng.List()[0]}})
	m = updated.(Model)

	if m.downloads.cursor != 0 {
		t.Fatalf("cursor after rows shrank to 1 = %d, want 0", m.downloads.cursor)
	}
}

func TestRenderDownloadsScreenEmptyState(t *testing.T) {
	m := New(fake.New(), testTheme())
	m.width, m.height = 80, 24

	view := m.renderDownloadsScreen()
	if !strings.Contains(view, "No downloads yet") {
		t.Fatalf("renderDownloadsScreen() with no torrents = %q, want the empty state", view)
	}
}

func TestRenderDownloadsScreenShowsQueuedActiveAndCompletedSections(t *testing.T) {
	eng := fake.New()
	t.Cleanup(func() { _ = eng.Close() })

	addFakeTorrent(t, eng, "magnet:?xt=urn:btih:queued&dn=queued.iso", nil)
	addFakeTorrent(t, eng, "magnet:?xt=urn:btih:seeding&dn=seeded.iso", fake.Completed())

	m := New(eng, testTheme())
	m.width, m.height = 100, 40

	updated, _ := m.Update(engineUpdateMsg{statuses: eng.List()})
	m = updated.(Model)

	view := m.renderDownloadsScreen()

	for _, want := range []string{
		"Active (1)", "Completed (1)",
		"queued.iso", "seeded.iso",
		"queued", "waiting to start",
		"not reported by this engine", // fake does not implement seedPolicyProvider
	} {
		if !strings.Contains(view, want) {
			t.Errorf("renderDownloadsScreen() missing %q; got:\n%s", want, view)
		}
	}
}

// --- end-to-end: download -> complete -> error (teatest) ------------------

// TestDownloadsScreenEndToEndDownloadCompleteError drives the whole screen
// through a real teatest program against internal/engine/fake, exactly as
// T-071's acceptance requires: a torrent progresses from downloading to
// complete (Downloading's own script ends in StateSeeding), and a second,
// independent torrent surfaces a readable error — proving the engine
// Updates() subscription wired in root.go actually reaches this screen's
// render, not just the pure helpers above.
func TestDownloadsScreenEndToEndDownloadCompleteError(t *testing.T) {
	eng := fake.New()
	t.Cleanup(func() { _ = eng.Close() })

	m := New(eng, testTheme())
	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(100, 40))
	t.Cleanup(func() { _ = tm.Quit() })

	waitForOutput(t, tm, "Query:")

	// Downloading() checks, then progresses to 100% and StateSeeding over
	// its total.
	const total = 4 * time.Second
	eng.ScriptFor = func(engine.AddSource) fake.Script { return fake.Downloading(total) }

	downloadID, err := eng.Add(context.Background(), engine.AddSource{Magnet: "magnet:?xt=urn:btih:dl&dn=growing.iso"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	tm.Send(keyRune("4")) // jump to downloads
	waitForOutput(t, tm, "growing.iso")

	// bubbletea's non-altscreen renderer diffs frames and only rewrites the
	// lines that actually changed, so each wait below drains the stream up
	// to its own marker rather than asserting several markers arrive in the
	// same frame (waitForAllOutput would be wrong here for exactly that
	// reason — a later marker's frame need not still carry an earlier,
	// unchanged line's bytes).
	eng.Advance(total / 2) // lands on the script's 40% step
	waitForOutput(t, tm, "40%")

	eng.Advance(total) // reach and pass the final event -> StateSeeding, 100%
	waitForOutput(t, tm, "Completed (1)")

	// A second, independent torrent that fails outright.
	wantErr := errors.New("not enough free space")
	eng.ScriptFor = func(engine.AddSource) fake.Script { return fake.Errored(1*time.Second, wantErr) }

	errID, err := eng.Add(context.Background(), engine.AddSource{Magnet: "magnet:?xt=urn:btih:err&dn=broken.iso"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	eng.Advance(2 * time.Second)
	// Both markers land in the same redraw (the newly errored row's name and
	// its own error line), so they must be asserted together in one
	// waitForAllOutput — a second, separate waitForOutput here would only
	// ever see bytes written *after* the first one already drained this
	// exact frame (teatest.WaitFor consumes what it reads).
	waitForAllOutput(t, tm, "broken.iso", "enter to expand")

	if downloadID == errID {
		t.Fatal("setup: expected two distinct torrent IDs")
	}
}
