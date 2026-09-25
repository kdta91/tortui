package tui

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/engine/fake"
	"github.com/kdta91/tortui/internal/indexer"
)

// --- helpers -----------------------------------------------------------------

// acceptDestination drives an open destination picker (the model and cmd the
// add flow's finishAdd returned) through to the add: it runs the pending
// validation, types a temp dir when there is no fixed row to accept, presses
// enter, and confirms creating the folder if asked. It returns the model and
// the add's cmd.
func acceptDestination(t *testing.T, updated tea.Model, cmd tea.Cmd) (Model, tea.Cmd) {
	t.Helper()

	m := updated.(Model)
	if !m.dest.open {
		t.Fatalf("destination picker not open (cmd %v)", cmd)
	}

	if len(m.dest.entries) == 0 {
		m = typeDestination(t, m, t.TempDir())
	} else {
		m = runDestCheck(t, m, cmd)
	}

	next, addCmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if m.dest.confirmCreate {
		next, addCmd = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
		m = next.(Model)
	}

	if m.dest.open {
		t.Fatalf("picker still open after enter: %q", m.statusBar.Message())
	}

	return m, addCmd
}

// runDestCheck runs a validation cmd and feeds its message back in.
func runDestCheck(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()

	if cmd == nil {
		t.Fatal("expected a destination validation cmd")
	}

	msg, ok := cmd().(destCheckMsg)
	if !ok {
		t.Fatal("cmd() did not produce destCheckMsg")
	}

	next, _ := m.Update(msg)

	return next.(Model)
}

// typeDestination moves to the path field, types text a rune at a time, and
// runs the last validation, as the user would.
func typeDestination(t *testing.T, m Model, text string) Model {
	t.Helper()

	for !m.dest.onField() {
		next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(Model)
	}

	var cmd tea.Cmd

	for _, r := range text {
		var next tea.Model

		next, cmd = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
	}

	if cmd != nil {
		m = runDestCheck(t, m, cmd)
	}

	return m
}

// rootEngine is engine/fake plus an engine.RootAdder that records every
// root it is asked to admit.
type rootEngine struct {
	*fake.Engine

	mu    sync.Mutex
	roots []string
	err   error
}

func (e *rootEngine) AddRoot(dir string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.err != nil {
		return e.err
	}

	e.roots = append(e.roots, dir)

	return nil
}

func (e *rootEngine) addedRoots() []string {
	e.mu.Lock()
	defer e.mu.Unlock()

	return append([]string(nil), e.roots...)
}

// memDestStore is an in-memory DestinationStore.
type memDestStore struct {
	mu    sync.Mutex
	dests []string
	err   error
}

func (s *memDestStore) Destinations() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]string(nil), s.dests...)
}

func (s *memDestStore) TouchDestination(p string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.err != nil {
		return s.err
	}

	next := []string{p}
	for _, d := range s.dests {
		if d != p {
			next = append(next, d)
		}
	}

	s.dests = next

	return nil
}

func env(vars map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := vars[k]
		return v, ok
	}
}

func homeAt(dir string) func() (string, error) {
	return func() (string, error) { return dir, nil }
}

// --- expandDestination -------------------------------------------------------

func TestExpandDestination(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	home := t.TempDir()
	vars := env(map[string]string{"MEDIA": filepath.Join(home, "media"), "SUB": "sub"})

	cases := []struct {
		in   string
		want string
	}{
		{"~", home},
		{"~/dl", filepath.Join(home, "dl")},
		{`~\dl\x`, filepath.Join(home, "dl", "x")},
		{"$MEDIA/films", filepath.Join(home, "media", "films")},
		{"${MEDIA}/a", filepath.Join(home, "media", "a")},
		{"%MEDIA%/b", filepath.Join(home, "media", "b")},
		{"relative/$SUB", filepath.Join(base, "relative", "sub")},
		{`mixed\seps//here/`, filepath.Join(base, "mixed", "seps", "here")},
		{"  padded  ", filepath.Join(base, "padded")},
		{"../sibling", filepath.Join(filepath.Dir(base), "sibling")},
		{base + string(filepath.Separator) + "abs", filepath.Join(base, "abs")},
		{"50%off", filepath.Join(base, "50%off")},
		{"cost$", filepath.Join(base, "cost$")},
	}

	for _, tc := range cases {
		got, err := expandDestination(tc.in, base, vars, homeAt(home))
		if err != nil {
			t.Errorf("expandDestination(%q): %v", tc.in, err)
			continue
		}

		if got != tc.want {
			t.Errorf("expandDestination(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestExpandDestinationRefusals(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	vars := env(nil)
	volumeRoot := filepath.VolumeName(base) + string(filepath.Separator)

	cases := map[string]string{
		"":         "type a folder path",
		"~bob/dl":  "~user",
		"$NOPE/x":  "NOPE is not set",
		"%NOPE%":   "NOPE is not set",
		"a\x00b":   "NUL",
		volumeRoot: "root of a drive",
	}

	for in, want := range cases {
		_, err := expandDestination(in, base, vars, homeAt(base))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("expandDestination(%q) = %v, want an error mentioning %q", in, err, want)
		}
	}

	if _, err := expandDestination("rel", "", vars, homeAt(base)); err == nil || !strings.Contains(err.Error(), "no default download folder") {
		t.Errorf("relative path with no base = %v, want refused (never the working directory)", err)
	}

	if _, err := expandDestination("rel", "also-relative", vars, homeAt(base)); err == nil {
		t.Error("relative path against a relative base accepted, want refused")
	}

	if _, err := expandDestination("~", base, vars, func() (string, error) { return "", errors.New("no home") }); err == nil {
		t.Error("~ with no home folder accepted")
	}
}

// TestExpandDestinationWindowsPaths: drive-letter and UNC paths are accepted
// exactly where the OS gives them a volume name (Windows) and refused
// elsewhere, with separators normalised either way. The branch is chosen by
// filepath's own behaviour, never an OS-name switch (AGENT.md §14).
func TestExpandDestinationWindowsPaths(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	windows := filepath.VolumeName(`C:\x`) != ""

	for in, want := range map[string]string{
		`C:\Users\me\dl`:     `C:\Users\me\dl`,
		`C:/Users/me/dl/`:    `C:\Users\me\dl`,
		`\\nas\share\films`:  `\\nas\share\films`,
		`//nas/share/films/`: `\\nas\share\films`,
	} {
		got, err := expandDestination(in, base, env(nil), homeAt(base))

		switch {
		case windows && err != nil:
			t.Errorf("expandDestination(%q): %v", in, err)
		case windows && got != want:
			t.Errorf("expandDestination(%q) = %q, want %q", in, got, want)
		case !windows && strings.HasPrefix(in, "//"):
			// A POSIX path starting with // is an ordinary absolute path.
			if err != nil {
				t.Errorf("expandDestination(%q): %v", in, err)
			}
		case !windows && (err == nil || !strings.Contains(err.Error(), "not valid on this system")):
			t.Errorf("expandDestination(%q) = %q, %v; want refused on this OS", in, got, err)
		}
	}

	if windows {
		if _, err := expandDestination(`C:relative`, base, env(nil), homeAt(base)); err == nil {
			t.Error("drive-relative path accepted")
		}
	}
}

// --- checkDestination --------------------------------------------------------

func plentyProbe() destProbe {
	return destProbe{
		freeSpace: func(string) (uint64, error) { return 1 << 40, nil },
		writable:  probeWritable,
	}
}

func TestCheckDestinationExistingAndToBeCreated(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	c := checkDestination(dir, 1<<20, 0, plentyProbe())
	if !c.exists || c.blockReason() != "" {
		t.Fatalf("existing dir check = %+v, reason %q", c, c.blockReason())
	}

	fresh := filepath.Join(dir, "new", "nested")

	c = checkDestination(fresh, 1<<20, 0, plentyProbe())
	if c.exists || c.blockReason() != "" {
		t.Fatalf("to-be-created check = %+v, reason %q", c, c.blockReason())
	}

	if _, err := os.Stat(fresh); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("validation created %s before any confirm: %v", fresh, err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("the writable probe left files behind: %v %v", entries, err)
	}
}

func TestCheckDestinationBlocks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")

	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if c := checkDestination(file, 0, 0, plentyProbe()); !strings.Contains(c.blockReason(), "a file") {
		t.Errorf("file as destination: reason %q", c.blockReason())
	}

	if c := checkDestination(filepath.Join(file, "sub"), 0, 0, plentyProbe()); !strings.Contains(c.blockReason(), "is a file") {
		t.Errorf("file in the ancestor chain: reason %q", c.blockReason())
	}

	unwritable := plentyProbe()
	unwritable.writable = func(string) error { return os.ErrPermission }

	if c := checkDestination(dir, 0, 0, unwritable); !strings.Contains(c.blockReason(), "not writable") {
		t.Errorf("unwritable: reason %q", c.blockReason())
	}

	small := plentyProbe()
	small.freeSpace = func(string) (uint64, error) { return 3 << 30, nil }

	c := checkDestination(dir, 2<<30, 2<<30, small)
	if r := c.blockReason(); !strings.Contains(r, "not enough space") || !strings.Contains(r, "short by") {
		t.Errorf("size+margin over free: reason %q", r)
	}

	if c := checkDestination(dir, 2<<30, 0, small); c.blockReason() != "" {
		t.Errorf("fits without margin: reason %q", c.blockReason())
	}

	if c := checkDestination(dir, 0, 1<<50, small); c.blockReason() != "" {
		t.Errorf("unknown size must not block on space: reason %q", c.blockReason())
	}

	broken := plentyProbe()
	broken.freeSpace = func(string) (uint64, error) { return 0, errors.New("statfs failed") }

	if c := checkDestination(dir, 1, 0, broken); !strings.Contains(c.blockReason(), "free space") {
		t.Errorf("free-space error: reason %q", c.blockReason())
	}
}

// --- entries -----------------------------------------------------------------

func TestDestinationEntriesOrder(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	def := filepath.Join(root, "default")
	s1, s2, s3 := filepath.Join(root, "s1"), filepath.Join(root, "s2"), filepath.Join(root, "s3")
	r1, r2, r3, r4 := filepath.Join(root, "r1"), filepath.Join(root, "r2"), filepath.Join(root, "r3"), filepath.Join(root, "r4")

	store := &memDestStore{dests: []string{r1, s3, def, r2, s2, r3, r4}}

	m := New(fake.New(), testTheme(), WithDownloadDir(def),
		WithSavedDestinations([]string{s1, s2, s3, "relative"}), WithDestinationStore(store))

	want := []destEntry{
		{destDefault, def},
		{destSaved, s3},
		{destSaved, s2},
		{destSaved, s1},
		{destRecent, r1},
		{destRecent, r2},
		{destRecent, r3},
	}

	if got := m.destinationEntries(); !reflect.DeepEqual(got, want) {
		t.Fatalf("destinationEntries() =\n%v\nwant\n%v", got, want)
	}
}

// --- picker flow (unit) ------------------------------------------------------

func openPicker(t *testing.T, m Model, r indexer.Result) (Model, tea.Cmd) {
	t.Helper()

	next, cmd := m.startAdd(r)

	return next.(Model), cmd
}

func TestPickerDropsStaleValidationAndWaitsForIt(t *testing.T) {
	t.Parallel()

	def := t.TempDir()
	eng := fake.New()
	t.Cleanup(func() { _ = eng.Close() })

	m := New(eng, testTheme(), WithDownloadDir(def))
	m, cmd := openPicker(t, m, indexer.Result{Title: "stale.iso", Magnet: "magnet:?xt=urn:btih:aa"})

	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if !m.dest.open || !strings.Contains(m.statusBar.Message(), "still checking") {
		t.Fatalf("enter before validation: open=%v msg=%q", m.dest.open, m.statusBar.Message())
	}

	stale := destCheckMsg{check: destCheck{path: filepath.Join(def, "elsewhere"), exists: true}}

	next, _ = m.Update(stale)
	m = next.(Model)

	if m.dest.check.path != "" {
		t.Fatalf("stale validation for another path recorded: %+v", m.dest.check)
	}

	m = runDestCheck(t, m, cmd)
	if m.dest.check.path != def {
		t.Fatalf("validation for the highlighted path not recorded: %+v", m.dest.check)
	}
}

func TestPickerCreateConfirmAdmitsRootAndRecordsDestination(t *testing.T) {
	t.Parallel()

	def := t.TempDir()
	eng := &rootEngine{Engine: fake.New()}
	t.Cleanup(func() { _ = eng.Close() })

	store := &memDestStore{}
	ts := &stubTorrentStore{}
	m := New(eng, testTheme(), WithDownloadDir(def), WithDestinationStore(store), WithTorrentStore(ts))
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = sized.(Model)
	m, _ = openPicker(t, m, indexer.Result{Title: "create.iso", Magnet: "magnet:?xt=urn:btih:bb", SizeBytes: 10})

	target := filepath.Join(t.TempDir(), "new", "nested")
	m = typeDestination(t, m, target)

	next, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)

	if !m.dest.confirmCreate || cmd != nil {
		t.Fatalf("enter on a new folder: confirmCreate=%v cmd=%v, want the create prompt and no add", m.dest.confirmCreate, cmd)
	}

	if !strings.Contains(m.View(), "Create ") {
		t.Error("create prompt not rendered")
	}

	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)

	if m.dest.confirmCreate || !m.dest.open {
		t.Fatal("esc on the create prompt must return to the picker, not close it")
	}

	next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, cmd = m.handleKey(keyRune("y"))
	m = next.(Model)

	if m.dest.open || cmd == nil {
		t.Fatalf("confirmed create did not dispatch the add (open=%v)", m.dest.open)
	}

	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("folder created inside Update, before the add's cmd ran: %v", err)
	}

	msg, ok := cmd().(addResultMsg)
	if !ok || msg.err != nil {
		t.Fatalf("add cmd = %#v", msg)
	}

	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Fatalf("destination not created with its parents: %v", err)
	}

	if got := eng.addedRoots(); !reflect.DeepEqual(got, []string{target}) {
		t.Errorf("engine roots admitted = %v, want [%s]", got, target)
	}

	if got := eng.List(); len(got) != 1 || got[0].SavePath != target {
		t.Fatalf("engine List = %+v, want one torrent saved to %s", got, target)
	}

	next, _ = m.Update(msg)
	m = next.(Model)

	if got := store.Destinations(); !reflect.DeepEqual(got, []string{target}) {
		t.Errorf("store destinations = %v, want [%s]", got, target)
	}

	if rec, ok := ts.GetTorrent(msg.id); !ok || rec.SavePath != target {
		t.Errorf("torrent record = %+v, want SavePath %s", rec, target)
	}

	if roots := m.destinationRoots(); !contains(roots, target) {
		t.Errorf("destinationRoots() = %v, want it to include %s", roots, target)
	}

	// A later picker offers it as the most recent destination.
	m, _ = openPicker(t, m, indexer.Result{Title: "again.iso", Magnet: "magnet:?xt=urn:btih:cc"})
	if len(m.dest.entries) < 2 || m.dest.entries[1] != (destEntry{destRecent, target}) {
		t.Errorf("entries after use = %v, want %s offered as Recent", m.dest.entries, target)
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}

	return false
}

func TestPickerAddFailuresAreReportedNotRecorded(t *testing.T) {
	t.Parallel()

	def := t.TempDir()
	eng := &rootEngine{Engine: fake.New(), err: errors.New("refused root")}
	t.Cleanup(func() { _ = eng.Close() })

	store := &memDestStore{}
	m := New(eng, testTheme(), WithDownloadDir(def), WithDestinationStore(store))
	m, cmd := openPicker(t, m, indexer.Result{Title: "refused.iso", Magnet: "magnet:?xt=urn:btih:dd"})
	m, cmd = acceptDestination(t, m, cmd)

	msg := cmd().(addResultMsg)
	if msg.err == nil || len(eng.List()) != 0 {
		t.Fatalf("add with a refused root: err=%v torrents=%d, want an error and no add", msg.err, len(eng.List()))
	}

	next, _ := m.Update(msg)
	m = next.(Model)

	if len(store.Destinations()) != 0 || len(m.usedDestinations) != 0 {
		t.Errorf("failed add recorded a destination: %v %v", store.Destinations(), m.usedDestinations)
	}

	// A store failure after a good add is reported, the add stands.
	eng.err = nil
	store.err = errors.New("disk full")

	m = New(eng, testTheme(), WithDownloadDir(def), WithDestinationStore(store))
	m, cmd = openPicker(t, m, indexer.Result{Title: "ok.iso", Magnet: "magnet:?xt=urn:btih:ee"})
	m, cmd = acceptDestination(t, m, cmd)
	next, _ = m.Update(cmd())
	m = next.(Model)

	if len(eng.List()) != 1 || !contains(m.usedDestinations, def) || m.screen != ScreenDownloads {
		t.Fatalf("store failure undid the add or the session root (torrents=%d used=%v)", len(eng.List()), m.usedDestinations)
	}

	if !strings.Contains(m.statusBar.Message(), "couldn't save destination: disk full") {
		t.Errorf("statusBar.Message() = %q, want the store failure", m.statusBar.Message())
	}
}

// TestPickerFitsAt80x24 renders the picker with more saved destinations
// than fit and checks it stays inside the 80×24 floor (AGENT.md §7).
func TestPickerFitsAt80x24(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	var saved []string
	for i := range 12 {
		saved = append(saved, filepath.Join(root, "saved", string(rune('a'+i))))
	}

	m := New(fake.New(), testTheme(), WithDownloadDir(root), WithSavedDestinations(saved))
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = next.(Model)
	m, _ = openPicker(t, m, indexer.Result{Title: strings.Repeat("long title ", 20), Magnet: "magnet:?xt=urn:btih:ff", SizeBytes: 1 << 30})

	for range len(saved) + 1 {
		next, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyDown})
		m = next.(Model)
	}

	out := m.View()
	if lines := strings.Count(out, "\n") + 1; lines > 24 {
		t.Fatalf("picker renders %d lines at 80x24, want ≤ 24:\n%s", lines, out)
	}

	if !strings.Contains(out, "> Path") {
		t.Errorf("the path field under the cursor scrolled out of view:\n%s", out)
	}
}

// --- teatest -----------------------------------------------------------------

// pickerModel is a Model on the results screen holding r as its one row.
func pickerModel(eng engine.Engine, r indexer.Result, opts ...Option) Model {
	m := New(eng, testTheme(), opts...)
	m.screen = ScreenResults
	m.lastResults = []indexer.Result{r}
	m.results = m.results.setResults(m.lastResults, indexer.ModeSearch, time.Now())

	return m
}

// startPicker runs m in a teatest program and presses enter on its one
// result, opening the destination picker.
func startPicker(t *testing.T, m Model) *teatest.TestModel {
	t.Helper()

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	t.Cleanup(func() { _ = tm.Quit() })

	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	return tm
}

// pastePath sends text as one bracketed paste, so the picker only ever
// validates the complete path: typing it rune by rune renders (and
// validates) every prefix, and a wait on the check text could match a
// prefix's frame before the full path's check arrived. Rune-by-rune typing
// is covered by TestPickerPathFieldEditing.
func pastePath(tm *teatest.TestModel, text string) {
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true})
}

func savePaths(eng engine.Engine) []string {
	var out []string
	for _, s := range eng.List() {
		out = append(out, s.SavePath)
	}

	return out
}

func TestDestinationPickerTeatest(t *testing.T) {
	result := indexer.Result{Title: "picker.iso", Magnet: "magnet:?xt=urn:btih:0a0a", SizeBytes: 5 << 30}

	t.Run("accept default", func(t *testing.T) {
		def := t.TempDir()
		eng := fake.New()
		t.Cleanup(func() { _ = eng.Close() })

		m := pickerModel(eng, result, WithDownloadDir(def))
		tm := startPicker(t, m)

		waitForAllOutput(t, tm, "Default", "exists", "writable", "free, needs")
		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
		waitForOutput(t, tm, "added picker.iso")
		waitForPredicate(t, func() bool { return reflect.DeepEqual(savePaths(eng), []string{def}) })
	})

	t.Run("pick a saved destination", func(t *testing.T) {
		// The saved destination does not exist yet, so its validation
		// renders differently from the default's and can be waited for.
		def, saved := t.TempDir(), filepath.Join(t.TempDir(), "saved")
		eng := fake.New()
		t.Cleanup(func() { _ = eng.Close() })

		m := pickerModel(eng, result, WithDownloadDir(def), WithSavedDestinations([]string{saved}))
		tm := startPicker(t, m)

		waitForAllOutput(t, tm, "Saved", "exists", "writable")
		tm.Send(tea.KeyMsg{Type: tea.KeyDown})
		waitForOutput(t, tm, "will be created")
		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
		waitForOutput(t, tm, "create folder and add")
		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
		waitForPredicate(t, func() bool { return reflect.DeepEqual(savePaths(eng), []string{saved}) })
	})

	t.Run("type a new path", func(t *testing.T) {
		def := t.TempDir()
		target := filepath.Join(t.TempDir(), "typed", "new")
		eng := fake.New()
		t.Cleanup(func() { _ = eng.Close() })

		m := pickerModel(eng, result, WithDownloadDir(def))
		tm := startPicker(t, m)

		waitForOutput(t, tm, "writable")
		tm.Send(tea.KeyMsg{Type: tea.KeyDown})
		pastePath(tm, target)
		waitForOutput(t, tm, "will be created")
		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
		waitForOutput(t, tm, "create folder and add")
		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
		waitForOutput(t, tm, "added picker.iso")
		waitForPredicate(t, func() bool { return reflect.DeepEqual(savePaths(eng), []string{target}) })

		if info, err := os.Stat(target); err != nil || !info.IsDir() {
			t.Fatalf("typed destination not created: %v", err)
		}
	})

	t.Run("type an invalid path", func(t *testing.T) {
		def := t.TempDir()
		file := filepath.Join(def, "not-a-folder")

		if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}

		eng := fake.New()
		t.Cleanup(func() { _ = eng.Close() })

		m := pickerModel(eng, result, WithDownloadDir(def))
		tm := startPicker(t, m)

		waitForOutput(t, tm, "writable")
		tm.Send(tea.KeyMsg{Type: tea.KeyDown})
		pastePath(tm, "not-a-folder/sub")
		waitForOutput(t, tm, "is a file")
		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
		waitForOutput(t, tm, "can't add here")

		if n := len(eng.List()); n != 0 {
			t.Fatalf("invalid destination added %d torrents", n)
		}
	})

	t.Run("type a path with no space", func(t *testing.T) {
		def := t.TempDir()
		roomy := t.TempDir()
		eng := fake.New()
		t.Cleanup(func() { _ = eng.Close() })

		m := pickerModel(eng, result, WithDownloadDir(def))
		m.destProbe.freeSpace = func(p string) (uint64, error) {
			if p == roomy {
				return 1 << 40, nil
			}

			return 1 << 30, nil
		}

		tm := startPicker(t, m)

		waitForOutput(t, tm, "not enough space")
		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
		waitForOutput(t, tm, "can't add here: not enough space")

		tm.Send(tea.KeyMsg{Type: tea.KeyDown})
		pastePath(tm, roomy)
		waitForOutput(t, tm, "free, needs")
		tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
		// The status bar still shows the refusal, so wait on the engine.
		waitForPredicate(t, func() bool { return reflect.DeepEqual(savePaths(eng), []string{roomy}) })
	})

	t.Run("cancel out of the picker", func(t *testing.T) {
		def := t.TempDir()
		eng := fake.New()
		t.Cleanup(func() { _ = eng.Close() })

		m := pickerModel(eng, result, WithDownloadDir(def))
		tm := startPicker(t, m)

		waitForOutput(t, tm, "writable")
		tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
		waitForOutput(t, tm, "add cancelled: picker.iso")

		if err := tm.Quit(); err != nil {
			t.Fatal(err)
		}

		final := tm.FinalModel(t, teatest.WithFinalTimeout(3*time.Second)).(Model)
		if final.dest.open || final.screen != ScreenResults || len(eng.List()) != 0 {
			t.Fatalf("after cancel: open=%v screen=%v torrents=%d", final.dest.open, final.screen, len(eng.List()))
		}
	})
}

// TestPickerPathFieldEditing covers the path field's own editing: letters
// that are hotkeys elsewhere type, space and backspace edit, a pasted path
// loses its trailing newline, and up leaves the field.
func TestPickerPathFieldEditing(t *testing.T) {
	t.Parallel()

	def := t.TempDir()
	m := New(fake.New(), testTheme(), WithDownloadDir(def))
	m, _ = openPicker(t, m, indexer.Result{Title: "edit.iso", Magnet: "magnet:?xt=urn:btih:ab"})
	m = typeDestination(t, m, "jkq")

	keys := []tea.KeyMsg{
		{Type: tea.KeySpace, Runes: []rune{' '}},
		{Type: tea.KeyRunes, Runes: []rune("my dir\r\n"), Paste: true},
		{Type: tea.KeyBackspace},
	}

	for _, k := range keys {
		next, _ := m.handleKey(k)
		m = next.(Model)
	}

	if m.dest.input != "jkq my di" {
		t.Fatalf("input = %q, want %q", m.dest.input, "jkq my di")
	}

	if got, err := m.destCandidate(); err != nil || got != filepath.Join(def, "jkq my di") {
		t.Fatalf("candidate = %q, %v", got, err)
	}

	next, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyUp})
	m = next.(Model)

	if m.dest.onField() || cmd == nil {
		t.Fatalf("up from the path field: onField=%v cmd=%v, want back on a row with a revalidation", m.dest.onField(), cmd)
	}

	if !m.dest.open || m.screen != ScreenSearch {
		t.Fatal("typing hotkey letters left the picker or switched screens")
	}
}
