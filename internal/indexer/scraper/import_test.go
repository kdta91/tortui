package scraper

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kdta91/tortui/internal/indexer/httpx"
)

// newTestImporter builds an importer over dir. httpx's own test client
// disables retries and per-host spacing so a URL test never waits.
func newTestImporter(t *testing.T, dir string) *Importer {
	t.Helper()

	im, err := NewImporter(ImporterOptions{Dir: dir, HTTPClient: testClient(httpx.Config{})})
	if err != nil {
		t.Fatalf("NewImporter: %v", err)
	}

	return im
}

func TestImporterDefaultDirectoryIsTheConfigDirectorysDefinitionsFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TORTUI_HOME", home)

	im, err := NewImporter(ImporterOptions{})
	if err != nil {
		t.Fatalf("NewImporter: %v", err)
	}

	want := filepath.Join(home, "config", "definitions")

	if im.Dir() != want {
		t.Fatalf("default directory is %q, want %q", im.Dir(), want)
	}
}

func TestImporterWithNoHTTPClientStillFetchesAURL(t *testing.T) {
	t.Parallel()

	body := aDefinition("fixture-default-client")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)

		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	im, err := NewImporter(ImporterOptions{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewImporter: %v", err)
	}

	def, _, err := im.Import(testContext(t), server.URL)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if def.ID != "fixture-default-client" {
		t.Fatalf("ID = %q, want fixture-default-client", def.ID)
	}
}

func TestImportFromALocalFileInstallsIt(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	dir := t.TempDir()

	srcFile := filepath.Join(src, "whatever-the-user-called-it.yml")
	if err := os.WriteFile(srcFile, []byte(aDefinition("fixture-local")), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	im := newTestImporter(t, dir)

	def, path, err := im.Import(testContext(t), srcFile)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if def.ID != "fixture-local" {
		t.Fatalf("ID = %q, want fixture-local", def.ID)
	}

	wantPath := filepath.Join(dir, "fixture-local.yml")
	if path != wantPath {
		t.Fatalf("path = %q, want %q", path, wantPath)
	}

	installed, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("read installed file: %v", err)
	}

	if string(installed) != aDefinition("fixture-local") {
		t.Fatalf("installed content does not match the source file byte for byte")
	}

	// A definition that Loader.Reload can pick straight back up is the
	// point of writing it under dir at all.
	loader, _ := newTestLoader(t, dir)

	if err := loader.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if _, ok := loader.Definition("fixture-local"); !ok {
		t.Fatal("the loader did not pick up the imported definition")
	}
}

func TestImportFromAURLInstallsIt(t *testing.T) {
	t.Parallel()

	body := aDefinition("fixture-remote")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/one/exact/path.yml" {
			w.WriteHeader(http.StatusNotFound)

			return
		}

		w.WriteHeader(http.StatusOK)

		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	dir := t.TempDir()
	im := newTestImporter(t, dir)

	def, path, err := im.Import(testContext(t), server.URL+"/one/exact/path.yml")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if def.ID != "fixture-remote" {
		t.Fatalf("ID = %q, want fixture-remote", def.ID)
	}

	if filepath.Base(path) != "fixture-remote.yml" {
		t.Fatalf("path = %q, want a file named fixture-remote.yml", path)
	}
}

func TestImportFetchesOnlyTheExactAddressGiven(t *testing.T) {
	t.Parallel()

	body := aDefinition("fixture-exact")

	var requests []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.URL.Path)

		if r.URL.Path != "/exact.yml" {
			w.WriteHeader(http.StatusNotFound)

			return
		}

		w.WriteHeader(http.StatusOK)

		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	im := newTestImporter(t, t.TempDir())

	if _, _, err := im.Import(testContext(t), server.URL+"/exact.yml"); err != nil {
		t.Fatalf("Import: %v", err)
	}

	if len(requests) != 1 || requests[0] != "/exact.yml" {
		t.Fatalf("requests = %v, want exactly one request for /exact.yml — Import must never discover or follow anything beyond the address it was given", requests)
	}
}

func TestImportRejectsAnEmptySource(t *testing.T) {
	t.Parallel()

	im := newTestImporter(t, t.TempDir())

	if _, _, err := im.Import(testContext(t), "   "); !errors.Is(err, ErrImportSourceEmpty) {
		t.Fatalf("error = %v, want ErrImportSourceEmpty", err)
	}
}

func TestImportRejectsAnUnsupportedURLScheme(t *testing.T) {
	t.Parallel()

	im := newTestImporter(t, t.TempDir())

	_, _, err := im.Import(testContext(t), "ftp://definitions.example.org/one.yml")
	if !errors.Is(err, ErrImportSchemeUnsupported) {
		t.Fatalf("error = %v, want ErrImportSchemeUnsupported", err)
	}
}

func TestImportOfAMissingLocalFileFailsAndWritesNothing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	im := newTestImporter(t, dir)

	_, _, err := im.Import(testContext(t), filepath.Join(dir, "does-not-exist.yml"))
	if err == nil {
		t.Fatal("Import: want an error for a file that does not exist")
	}

	assertDirEmpty(t, dir)
}

func TestImportRejectsAMalformedDefinitionAndWritesNothing(t *testing.T) {
	t.Parallel()

	src := filepath.Join(t.TempDir(), "broken.yml")
	if err := os.WriteFile(src, []byte("id: [this is not a definition"), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	dir := t.TempDir()
	im := newTestImporter(t, dir)

	_, _, err := im.Import(testContext(t), src)
	if !errors.Is(err, ErrDefinitionMalformed) {
		t.Fatalf("error = %v, want ErrDefinitionMalformed", err)
	}

	assertDirEmpty(t, dir)
}

func TestImportNamesTheFailingFieldOfAnInvalidDefinition(t *testing.T) {
	t.Parallel()

	// Valid YAML, but no rows selector: a structural validation failure
	// that ValidationError names precisely, per T-025's own acceptance
	// ("actionable errors... naming the failing field and selector").
	body := strings.Join([]string{
		"id: fixture-invalid",
		"base_url: https://fixture-invalid.example.org",
		"fields:",
		"  title:",
		"    selector: .name",
		"search:",
		"  path: /search",
		"",
	}, "\n")

	src := filepath.Join(t.TempDir(), "invalid.yml")
	if err := os.WriteFile(src, []byte(body), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	dir := t.TempDir()
	im := newTestImporter(t, dir)

	_, _, err := im.Import(testContext(t), src)

	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("error = %v, want a *ValidationError", err)
	}

	if verr.Location != "search.rows" {
		t.Fatalf("Location = %q, want search.rows", verr.Location)
	}

	if !errors.Is(err, ErrRowsSelectorMissing) {
		t.Fatalf("error = %v, want ErrRowsSelectorMissing", err)
	}

	assertDirEmpty(t, dir)
}

func TestImportRejectsAnIDThatIsUnsafeAsAFileName(t *testing.T) {
	t.Parallel()

	cases := []string{
		"../escape",
		"../../etc/passwd",
		"a/b",
		`a\b`,
		"",
		"  ",
	}

	for _, id := range cases {
		t.Run(id, func(t *testing.T) {
			t.Parallel()

			body := strings.Join([]string{
				"id: " + id,
				"base_url: https://fixture.example.org",
				"rows: table tr",
				"fields:",
				"  title:",
				"    selector: .name",
				"  magnet:",
				"    selector: a.magnet",
				"    attr: href",
				"search:",
				"  path: /search",
				"",
			}, "\n")

			src := filepath.Join(t.TempDir(), "def.yml")
			if err := os.WriteFile(src, []byte(body), 0o600); err != nil {
				t.Fatalf("write source file: %v", err)
			}

			dir := t.TempDir()
			im := newTestImporter(t, dir)

			_, _, err := im.Import(testContext(t), src)

			// An id of "" or all-whitespace fails Definition.Validate
			// itself (ErrIDEmpty) before Import ever looks at the file
			// name; every other case here reaches ErrImportIDUnsafe.
			if strings.TrimSpace(id) == "" {
				if !errors.Is(err, ErrIDEmpty) {
					t.Fatalf("error = %v, want ErrIDEmpty", err)
				}
			} else if !errors.Is(err, ErrImportIDUnsafe) {
				t.Fatalf("error = %v, want ErrImportIDUnsafe", err)
			}

			assertDirEmpty(t, dir)
		})
	}
}

func TestImportRefusesToOverwriteAnInstalledDefinition(t *testing.T) {
	t.Parallel()

	dir := definitionsDir(t, map[string]string{"fixture-archive.yml": htmlDefinitionFile})
	im := newTestImporter(t, dir)

	src := filepath.Join(t.TempDir(), "another-copy.yml")
	if err := os.WriteFile(src, []byte(fixture(t, htmlDefinitionFile)), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	_, _, err := im.Import(testContext(t), src)
	if !errors.Is(err, ErrImportDestinationExists) {
		t.Fatalf("error = %v, want ErrImportDestinationExists", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("dir has %d entries after a refused import, want exactly the original one", len(entries))
	}
}

func TestImportRefusesToOverwriteCaseInsensitively(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeDefinitionFile(t, dir, "Fixture-Case.yml", aDefinition("fixture-case"))

	im := newTestImporter(t, dir)

	src := filepath.Join(t.TempDir(), "copy.yml")
	if err := os.WriteFile(src, []byte(aDefinition("fixture-case")), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	_, _, err := im.Import(testContext(t), src)
	if !errors.Is(err, ErrImportDestinationExists) {
		t.Fatalf("error = %v, want ErrImportDestinationExists (macOS's default filesystem cannot tell fixture-case.yml from Fixture-Case.yml apart)", err)
	}
}

func TestImportCreatesTheDefinitionsDirectoryWhenAbsent(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "definitions")

	src := filepath.Join(t.TempDir(), "def.yml")
	if err := os.WriteFile(src, []byte(aDefinition("fixture-fresh")), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	im := newTestImporter(t, dir)

	if _, _, err := im.Import(testContext(t), src); err != nil {
		t.Fatalf("Import: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "fixture-fresh.yml")); err != nil {
		t.Fatalf("stat installed file: %v", err)
	}
}

func TestImportLeavesNoTemporaryFileBehind(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := filepath.Join(t.TempDir(), "def.yml")

	if err := os.WriteFile(src, []byte(aDefinition("fixture-clean")), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	im := newTestImporter(t, dir)

	if _, _, err := im.Import(testContext(t), src); err != nil {
		t.Fatalf("Import: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}

	if len(entries) != 1 || entries[0].Name() != "fixture-clean.yml" {
		t.Fatalf("dir entries = %v, want exactly fixture-clean.yml", entries)
	}
}

// assertDirEmpty fails the test unless dir has no entries — the "nothing is
// written" half of T-025's acceptance criteria.
func assertDirEmpty(t *testing.T, dir string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}

		t.Fatalf("read dir: %v", err)
	}

	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}

	if len(names) != 0 {
		t.Fatalf("dir has entries %v, want none — a rejected import must write nothing", names)
	}
}
