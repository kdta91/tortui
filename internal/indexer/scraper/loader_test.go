package scraper

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kdta91/tortui/internal/logging"
)

// writeDefinitionFile puts one file into a definitions directory.
func writeDefinitionFile(t *testing.T, dir, name, body string) string {
	t.Helper()

	path := filepath.Join(dir, name)

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}

	return path
}

// definitionsDir makes a directory holding the named checked-in fixture
// definitions, under the file names given.
func definitionsDir(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()

	for name, fixtureName := range files {
		writeDefinitionFile(t, dir, name, fixture(t, fixtureName))
	}

	return dir
}

// aDefinition renders a minimal valid definition with the given id, so a
// test can have as many distinct ones as it needs without a fixture file
// per id. Every address in it is on an RFC 2606 reserved domain and the
// site is invented (AGENT.md §2, §16).
func aDefinition(id string) string {
	return strings.Join([]string{
		"id: " + id,
		"name: " + id,
		"base_url: https://" + id + ".example.org",
		"rows: table tr",
		"fields:",
		"  title:",
		"    selector: .name",
		"  magnet:",
		"    selector: a.magnet",
		"    attr: href",
		"search:",
		"  path: /search",
		"  params:",
		`    q: "{{query}}"`,
		"",
	}, "\n")
}

// capturingLogger returns a logger writing JSON into the buffer it also
// returns, so a test can assert on what was logged.
func capturingLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer

	return slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

// newTestLoader builds a loader over dir with a capturing logger.
func newTestLoader(t *testing.T, dir string) (*Loader, *bytes.Buffer) {
	t.Helper()

	logger, buf := capturingLogger()

	loader, err := NewLoader(LoaderOptions{Dir: dir, Logger: logger})
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	return loader, buf
}

// makeUnreadable takes read permission away from path for the rest of the
// test, restoring it afterwards, and skips the test when that does not
// actually make it unreadable — which is the case for root, and for a
// filesystem that does not honour the mode (Windows, where os.Chmod only
// moves the read-only bit). check is what the test needs to fail: reading
// the directory, or opening the file.
func makeUnreadable(t *testing.T, path string, check func() error) {
	t.Helper()

	if err := os.Chmod(path, 0o000); err != nil {
		t.Skipf("this filesystem will not drop the mode on %s: %v", filepath.Base(path), err)
	}

	t.Cleanup(func() {
		if err := os.Chmod(path, 0o700); err != nil {
			t.Errorf("restore the mode on %s: %v", filepath.Base(path), err)
		}
	})

	if err := check(); err == nil {
		t.Skipf("%s is still readable with mode 0000, so there is nothing to test here",
			filepath.Base(path))
	}
}

// ids lists the ids of a snapshot's definitions, in the order it returns
// them.
func ids(defs []*Definition) []string {
	out := make([]string, 0, len(defs))

	for _, def := range defs {
		out = append(out, def.ID)
	}

	return out
}

func TestReloadLoadsEveryYMLInTheDirectory(t *testing.T) {
	t.Parallel()

	dir := definitionsDir(t, map[string]string{
		"archive.yml": htmlDefinitionFile,
		"api.yml":     jsonDefinitionFile,
	})

	loader, _ := newTestLoader(t, dir)

	if err := loader.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	got := ids(loader.Definitions())
	want := []string{jsonID, htmlID} // "fixture-api" sorts before "fixture-archive"

	if len(got) != len(want) {
		t.Fatalf("loaded %v, want %v", got, want)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("loaded %v, want %v (sorted by id)", got, want)
		}
	}

	if skipped := loader.Skipped(); len(skipped) != 0 {
		t.Fatalf("skipped %v, want none", skipped)
	}

	def, ok := loader.Definition(htmlID)
	if !ok {
		t.Fatalf("Definition(%q) not found", htmlID)
	}

	if def.BaseURL == "" {
		t.Fatal("the definition came back without its base address")
	}

	if _, ok := loader.Definition("no-such-source"); ok {
		t.Fatal("Definition reported an id the directory does not define")
	}
}

func TestReloadOnAnEmptyDirectoryLoadsNothingAndDoesNotFail(t *testing.T) {
	t.Parallel()

	loader, buf := newTestLoader(t, t.TempDir())

	if err := loader.Reload(); err != nil {
		t.Fatalf("Reload on an empty directory: %v", err)
	}

	if defs := loader.Definitions(); len(defs) != 0 {
		t.Fatalf("loaded %v from an empty directory, want none", ids(defs))
	}

	if skipped := loader.Skipped(); len(skipped) != 0 {
		t.Fatalf("skipped %v, want none", skipped)
	}

	if strings.Contains(buf.String(), `"level":"ERROR"`) {
		t.Fatalf("an empty directory logged an error: %s", buf.String())
	}
}

func TestOneMalformedFileAmongValidOnesIsSkippedAndLogged(t *testing.T) {
	t.Parallel()

	dir := definitionsDir(t, map[string]string{
		"archive.yml": htmlDefinitionFile,
		"api.yml":     jsonDefinitionFile,
	})

	writeDefinitionFile(t, dir, "broken.yml", "id: broken\n  this: is not\n valid yaml\n")

	loader, buf := newTestLoader(t, dir)

	if err := loader.Reload(); err != nil {
		t.Fatalf("Reload with one broken file returned an error, which would block startup: %v", err)
	}

	got := ids(loader.Definitions())
	if len(got) != 2 {
		t.Fatalf("loaded %v, want the two valid definitions", got)
	}

	skipped := loader.Skipped()
	if len(skipped) != 1 {
		t.Fatalf("skipped %v, want exactly the broken file", skipped)
	}

	if skipped[0].File != "broken.yml" {
		t.Fatalf("skipped %q, want broken.yml", skipped[0].File)
	}

	if !errors.Is(skipped[0].Err, ErrDefinitionMalformed) {
		t.Fatalf("skipped error is %v, want one wrapping ErrDefinitionMalformed", skipped[0].Err)
	}

	logged := buf.String()

	if !strings.Contains(logged, "broken.yml") {
		t.Fatalf("the skipped file was not named in the log: %s", logged)
	}

	if !strings.Contains(logged, `"level":"ERROR"`) {
		t.Fatalf("the skipped file was not logged at error level: %s", logged)
	}
}

func TestADefinitionThatFailsValidationIsSkippedRatherThanReturned(t *testing.T) {
	t.Parallel()

	dir := definitionsDir(t, map[string]string{"archive.yml": htmlDefinitionFile})

	// Well-formed YAML in the schema's shape, but no title field, which
	// Validate refuses.
	writeDefinitionFile(t, dir, "untitled.yml", strings.Join([]string{
		"id: untitled",
		"base_url: https://untitled.example.org",
		"rows: table tr",
		"fields:",
		"  magnet:",
		"    selector: a.magnet",
		"    attr: href",
		"search:",
		"  path: /search",
		"",
	}, "\n"))

	loader, _ := newTestLoader(t, dir)

	if err := loader.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if got := ids(loader.Definitions()); len(got) != 1 || got[0] != htmlID {
		t.Fatalf("loaded %v, want only the valid definition", got)
	}

	skipped := loader.Skipped()
	if len(skipped) != 1 || !errors.Is(skipped[0].Err, ErrTitleFieldMissing) {
		t.Fatalf("skipped %v, want the untitled definition on ErrTitleFieldMissing", skipped)
	}
}

func TestOnlyYMLFilesAreRead(t *testing.T) {
	t.Parallel()

	dir := definitionsDir(t, map[string]string{"archive.yml": htmlDefinitionFile})

	// Neither of these is a *.yml file, so neither is read at all — not
	// read and rejected, which is why the notes file is deliberately not
	// valid YAML and still does not appear in Skipped.
	writeDefinitionFile(t, dir, "notes.txt", "{{{ not yaml")
	writeDefinitionFile(t, dir, "api.yaml", fixture(t, jsonDefinitionFile))

	if err := os.Mkdir(filepath.Join(dir, "nested.yml"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	loader, _ := newTestLoader(t, dir)

	if err := loader.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if got := ids(loader.Definitions()); len(got) != 1 || got[0] != htmlID {
		t.Fatalf("loaded %v, want only the one .yml file", got)
	}

	if skipped := loader.Skipped(); len(skipped) != 0 {
		t.Fatalf("skipped %v, want none: a non-.yml entry is not read at all", skipped)
	}
}

func TestAnUppercaseExtensionIsReadOnEveryPlatform(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeDefinitionFile(t, dir, "ARCHIVE.YML", fixture(t, htmlDefinitionFile))

	loader, _ := newTestLoader(t, dir)

	if err := loader.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if got := ids(loader.Definitions()); len(got) != 1 {
		t.Fatalf("loaded %v, want the one .YML file: the extension is matched case-insensitively "+
			"on every platform so a file that loads on a case-insensitive filesystem loads on a "+
			"case-sensitive one too (AGENT.md §13)", got)
	}
}

func TestTwoFilesDeclaringTheSameIDKeepTheFirstByFileName(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeDefinitionFile(t, dir, "a-first.yml", aDefinition("twice"))
	writeDefinitionFile(t, dir, "b-second.yml", aDefinition("twice"))

	loader, _ := newTestLoader(t, dir)

	if err := loader.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if got := ids(loader.Definitions()); len(got) != 1 || got[0] != "twice" {
		t.Fatalf("loaded %v, want the id once", got)
	}

	skipped := loader.Skipped()
	if len(skipped) != 1 {
		t.Fatalf("skipped %v, want the second file", skipped)
	}

	if skipped[0].File != "b-second.yml" || !errors.Is(skipped[0].Err, ErrDefinitionIDDuplicated) {
		t.Fatalf("skipped %v, want b-second.yml on ErrDefinitionIDDuplicated", skipped)
	}
}

func TestAFileLargerThanTheLimitIsSkipped(t *testing.T) {
	t.Parallel()

	dir := definitionsDir(t, map[string]string{"archive.yml": htmlDefinitionFile})

	padding := strings.Repeat("# padding\n", (maxDefinitionBytes/10)+1)
	writeDefinitionFile(t, dir, "huge.yml", aDefinition("huge")+padding)

	loader, _ := newTestLoader(t, dir)

	if err := loader.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if got := ids(loader.Definitions()); len(got) != 1 || got[0] != htmlID {
		t.Fatalf("loaded %v, want only the definition inside the limit", got)
	}

	skipped := loader.Skipped()
	if len(skipped) != 1 || !errors.Is(skipped[0].Err, ErrDefinitionTooLarge) {
		t.Fatalf("skipped %v, want huge.yml on ErrDefinitionTooLarge", skipped)
	}
}

func TestAMissingDirectoryIsNotAFailure(t *testing.T) {
	t.Parallel()

	loader, buf := newTestLoader(t, filepath.Join(t.TempDir(), "definitions"))

	if err := loader.Reload(); err != nil {
		t.Fatalf("Reload with no definitions directory: %v, want no error — a fresh install has none", err)
	}

	if defs := loader.Definitions(); len(defs) != 0 {
		t.Fatalf("loaded %v with no directory, want none", ids(defs))
	}

	if strings.Contains(buf.String(), `"level":"ERROR"`) {
		t.Fatalf("a missing directory logged an error: %s", buf.String())
	}
}

func TestADirectoryThatCannotBeReadIsReportedAndLeavesTheLastSetInPlace(t *testing.T) {
	t.Parallel()

	dir := definitionsDir(t, map[string]string{"archive.yml": htmlDefinitionFile})

	loader, _ := newTestLoader(t, dir)

	if err := loader.Reload(); err != nil {
		t.Fatalf("first Reload: %v", err)
	}

	makeUnreadable(t, dir, func() error {
		_, err := os.ReadDir(dir)

		return err
	})

	if err := loader.Reload(); err == nil {
		t.Fatal("Reload on an unreadable directory returned no error")
	}

	if got := ids(loader.Definitions()); len(got) != 1 || got[0] != htmlID {
		t.Fatalf("after a failed Reload the set is %v, want the previously loaded one", got)
	}
}

func TestThereIsAnEmptySetBeforeTheFirstReload(t *testing.T) {
	t.Parallel()

	loader, _ := newTestLoader(t, definitionsDir(t, map[string]string{"archive.yml": htmlDefinitionFile}))

	if defs := loader.Definitions(); len(defs) != 0 {
		t.Fatalf("a loader that has not reloaded holds %v, want nothing: NewLoader touches no disk", ids(defs))
	}

	if skipped := loader.Skipped(); len(skipped) != 0 {
		t.Fatalf("a loader that has not reloaded holds skipped %v, want none", skipped)
	}

	if _, ok := loader.Definition(htmlID); ok {
		t.Fatal("a loader that has not reloaded answered Definition")
	}
}

func TestReloadPicksUpAFileAddedAfterTheFirstLoad(t *testing.T) {
	t.Parallel()

	dir := definitionsDir(t, map[string]string{"archive.yml": htmlDefinitionFile})

	loader, _ := newTestLoader(t, dir)

	if err := loader.Reload(); err != nil {
		t.Fatalf("first Reload: %v", err)
	}

	writeDefinitionFile(t, dir, "added.yml", aDefinition("added"))

	if err := os.Remove(filepath.Join(dir, "archive.yml")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	if err := loader.Reload(); err != nil {
		t.Fatalf("second Reload: %v", err)
	}

	got := ids(loader.Definitions())
	if len(got) != 1 || got[0] != "added" {
		t.Fatalf("after the second Reload the set is %v, want exactly the file now on disk", got)
	}
}

// TestReloadSwapsAtomicallyUnderConcurrentReaders is the -race test for the
// "swaps definitions atomically" criterion. Readers run while reloaders
// run, and every read must see a whole set — never a partial one, and never
// a set that disagrees with its own id index.
func TestReloadSwapsAtomicallyUnderConcurrentReaders(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	want := []string{"one", "three", "two"} // sorted by id
	for _, id := range want {
		writeDefinitionFile(t, dir, id+".yml", aDefinition(id))
	}

	loader, _ := newTestLoader(t, dir)

	if err := loader.Reload(); err != nil {
		t.Fatalf("seed Reload: %v", err)
	}

	var wg sync.WaitGroup

	const (
		readers   = 8
		reloaders = 4
		rounds    = 200
	)

	for range readers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for range rounds {
				snap := loader.Snapshot()

				got := ids(snap.Definitions())
				if len(got) != len(want) {
					t.Errorf("a reader saw %v, want the whole set %v", got, want)

					return
				}

				for i := range want {
					if got[i] != want[i] {
						t.Errorf("a reader saw %v, want %v", got, want)

						return
					}

					if _, ok := snap.Definition(want[i]); !ok {
						t.Errorf("a reader saw a set whose index is missing %q", want[i])

						return
					}
				}

				if skipped := snap.Skipped(); len(skipped) != 0 {
					t.Errorf("a reader saw skipped %v, want none", skipped)

					return
				}
			}
		}()
	}

	for range reloaders {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for range rounds {
				if err := loader.Reload(); err != nil {
					t.Errorf("concurrent Reload: %v", err)

					return
				}
			}
		}()
	}

	wg.Wait()
}

func TestASnapshotIsNotAffectedByALaterReload(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeDefinitionFile(t, dir, "before.yml", aDefinition("before"))

	loader, _ := newTestLoader(t, dir)

	if err := loader.Reload(); err != nil {
		t.Fatalf("first Reload: %v", err)
	}

	held := loader.Snapshot()

	if err := os.Remove(filepath.Join(dir, "before.yml")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	writeDefinitionFile(t, dir, "after.yml", aDefinition("after"))

	if err := loader.Reload(); err != nil {
		t.Fatalf("second Reload: %v", err)
	}

	if got := ids(held.Definitions()); len(got) != 1 || got[0] != "before" {
		t.Fatalf("the held snapshot changed to %v, want the set it was taken from", got)
	}

	if got := ids(loader.Snapshot().Definitions()); len(got) != 1 || got[0] != "after" {
		t.Fatalf("the current snapshot is %v, want the set now on disk", got)
	}
}

func TestMutatingTheReturnedSliceDoesNotChangeTheLoadersSet(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeDefinitionFile(t, dir, "one.yml", aDefinition("one"))
	writeDefinitionFile(t, dir, "two.yml", aDefinition("two"))

	loader, _ := newTestLoader(t, dir)

	if err := loader.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	defs := loader.Definitions()
	defs[0] = nil

	if got := ids(loader.Definitions()); len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("the loader's set is %v after a caller edited the slice it was handed", got)
	}
}

func TestTheDefaultDirectoryIsTheConfigDirectorysDefinitionsFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TORTUI_HOME", home)

	loader, err := NewLoader(LoaderOptions{})
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	want := filepath.Join(home, "config", "definitions")

	if loader.Dir() != want {
		t.Fatalf("default directory is %q, want %q", loader.Dir(), want)
	}
}

func TestANilLoggerFallsBackToTheProcessDefault(t *testing.T) {
	// Not parallel: it swaps slog's process-wide default.
	dir := definitionsDir(t, map[string]string{"archive.yml": htmlDefinitionFile})
	writeDefinitionFile(t, dir, "broken.yml", "id: broken\n  this: is not\n valid yaml\n")

	installed, buf := capturingLogger()

	previous := slog.Default()
	slog.SetDefault(installed)

	t.Cleanup(func() { slog.SetDefault(previous) })

	loader, err := NewLoader(LoaderOptions{Dir: dir})
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	if err := loader.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if got := ids(loader.Definitions()); len(got) != 1 {
		t.Fatalf("loaded %v, want one definition", got)
	}

	if !strings.Contains(buf.String(), "broken.yml") {
		t.Fatalf("a loader with no logger of its own did not reach slog's default: %s", buf.String())
	}
}

func TestAFileThatCannotBeReadIsSkippedWithoutNamingItsPath(t *testing.T) {
	t.Parallel()

	dir := definitionsDir(t, map[string]string{"archive.yml": htmlDefinitionFile})
	unreadable := writeDefinitionFile(t, dir, "locked.yml", aDefinition("locked"))

	makeUnreadable(t, unreadable, func() error {
		f, err := os.Open(unreadable)
		if err != nil {
			return err
		}

		return f.Close()
	})

	loader, _ := newTestLoader(t, dir)

	if err := loader.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if got := ids(loader.Definitions()); len(got) != 1 || got[0] != htmlID {
		t.Fatalf("loaded %v, want the readable definition", got)
	}

	skipped := loader.Skipped()
	if len(skipped) != 1 || skipped[0].File != "locked.yml" {
		t.Fatalf("skipped %v, want locked.yml", skipped)
	}

	message := skipped[0].Err.Error()

	if !strings.Contains(message, "open") {
		t.Fatalf("the skipped error is %q, want the operation that failed", message)
	}

	if strings.Contains(message, dir) || strings.Contains(message, unreadable) {
		t.Fatalf("the skipped error repeats the path, which runs through the user's home directory: %q", message)
	}
}

func TestALoadedDefinitionCanBuildAnAdapter(t *testing.T) {
	t.Parallel()

	dir := definitionsDir(t, map[string]string{"archive.yml": htmlDefinitionFile})

	loader, _ := newTestLoader(t, dir)

	if err := loader.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	def, ok := loader.Definition(htmlID)
	if !ok {
		t.Fatalf("Definition(%q) not found", htmlID)
	}

	adapter, err := New(Options{Definition: def})
	if err != nil {
		t.Fatalf("New from a loaded definition: %v", err)
	}

	if adapter.ID() != htmlID {
		t.Fatalf("adapter id is %q, want %q", adapter.ID(), htmlID)
	}
}

// TestNothingTheLoaderLogsOrSkipsCarriesTheCredential is the loader's half
// of DEC-073. The loader is the first thing in this package that logs at
// all, so the property the rest of the package gets by never logging has to
// be asserted here instead: a user is free to hardcode their own api key
// into a param value or into the base address of a definition, and a file
// that then fails to parse or to validate must not carry either into the
// log file or into Skipped.
//
// The failure modes it drives, enumerated: a YAML syntax error on the line
// holding the key, a schema-shape failure (an unknown key) in a file whose
// params hold the key, a validation failure (an uncompilable selector) in a
// file whose base address holds the key, a file over the size limit whose
// padding holds the key, and a duplicate id in a file whose params hold the
// key.
func TestNothingTheLoaderLogsOrSkipsCarriesTheCredential(t *testing.T) {
	// Not parallel: logging.New installs the process-wide slog default.
	dir := t.TempDir()

	withKey := strings.Replace(aDefinition("keyed"),
		`    q: "{{query}}"`,
		"    q: \"{{query}}\"\n    token: \""+testKey+"\"", 1)

	writeDefinitionFile(t, dir, "syntax.yml", "id: broken\n  token: "+testKey+"\n bad: yaml\n")
	writeDefinitionFile(t, dir, "shape.yml", withKey+"not_a_key: "+testCookie+"\n")
	writeDefinitionFile(t, dir, "selector.yml", strings.Replace(
		strings.Replace(aDefinition("bad-selector"),
			"base_url: https://bad-selector.example.org",
			"base_url: https://bad-selector.example.org/?token="+testKey, 1),
		"    selector: .name", "    selector: ((", 1))
	writeDefinitionFile(t, dir, "huge.yml", withKey+strings.Repeat("# "+testCookie+"\n", maxDefinitionBytes/10))
	writeDefinitionFile(t, dir, "a-keyed.yml", withKey)
	writeDefinitionFile(t, dir, "b-keyed.yml", withKey)

	path := filepath.Join(t.TempDir(), "tortui.log")

	logger, closer, err := logging.New(logging.Options{File: path, Level: "debug"})
	if err != nil {
		t.Fatalf("build logger: %v", err)
	}

	loader, err := NewLoader(LoaderOptions{Dir: dir, Logger: logger})
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	if err := loader.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	skipped := loader.Skipped()
	if len(skipped) != 5 {
		t.Fatalf("skipped %v, want every file but a-keyed.yml", skipped)
	}

	for _, s := range skipped {
		assertNoCredentialLeak(t, "the skipped entry for "+s.File, s.Err.Error())
	}

	if err := closer.Close(); err != nil {
		t.Fatalf("close log: %v", err)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	if !strings.Contains(string(contents), "skipping a definition") {
		t.Fatalf("the log file does not contain the lines under test:\n%s", contents)
	}

	assertNoCredentialLeak(t, "the log file", string(contents))
}
