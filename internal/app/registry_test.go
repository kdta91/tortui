package app

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/indexer/scraper/builtin"
)

// userDefinition is a minimal, valid scraper definition for an invented
// source on example.org.
const userDefinition = `id: example-user
name: Example User Source
base_url: https://example.org
mode: json
rows: items
fields:
  id:
    selector: id
  title:
    selector: title
  infohash:
    selector: hash
search:
  path: /search
  params:
    q: "{{query}}"
`

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func registryIDs(t *testing.T, cfg config.Config, defsDir string) map[string]bool {
	t.Helper()

	reg := buildRegistry(cfg, defsDir, discardLogger(), &recordingTransport{})

	ids := make(map[string]bool)
	for _, ix := range reg.Enabled() {
		ids[ix.ID()] = true
	}

	return ids
}

func bundledID(t *testing.T) string {
	t.Helper()

	defs, err := builtin.Definitions()
	if err != nil || len(defs) == 0 {
		t.Fatalf("builtin.Definitions: %v (%d)", err, len(defs))
	}

	return defs[0].ID
}

func TestRegistryAddsEnabledUserSources(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "example.yml"), []byte(userDefinition), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default(t.TempDir())
	cfg.Indexers = []config.Indexer{
		{ID: "my-feed", Name: "My Feed", Type: "torznab", URL: "https://example.org/api", APIKey: "k", Enabled: true},
		{ID: "example-user", Name: "Example", Type: "scraper", Definition: "example.yml", Enabled: true},
		{ID: "off-feed", Name: "Off", Type: "torznab", URL: "https://example.org/off", Enabled: false},
	}

	ids := registryIDs(t, cfg, dir)

	for _, want := range []string{"my-feed", "example-user", bundledID(t)} {
		if !ids[want] {
			t.Errorf("source %q not registered; got %v", want, ids)
		}
	}

	if ids["off-feed"] {
		t.Error("a disabled [[indexer]] entry was registered")
	}
}

// TestRegistryEntryWithABundledIDReplacesIt: enabled = false under a
// bundled source's id turns that default off.
func TestRegistryEntryWithABundledIDReplacesIt(t *testing.T) {
	id := bundledID(t)

	cfg := config.Default(t.TempDir())
	cfg.Indexers = []config.Indexer{{ID: id, Name: "off", Type: "scraper", Definition: "x.yml", Enabled: false}}

	if registryIDs(t, cfg, t.TempDir())[id] {
		t.Fatalf("bundled source %q still registered after an entry disabled it", id)
	}
}

// TestRegistrySkipsBrokenEntries: an entry that cannot be built is skipped,
// and the rest still register (AGENT.md §6.3).
func TestRegistrySkipsBrokenEntries(t *testing.T) {
	cfg := config.Default(t.TempDir())
	cfg.Indexers = []config.Indexer{
		{ID: "escape", Type: "scraper", Definition: "../outside.yml", Enabled: true},
		{ID: "missing", Type: "scraper", Definition: "nope.yml", Enabled: true},
		{ID: "weird", Type: "gopher", URL: "https://example.org", Enabled: true},
	}

	ids := registryIDs(t, cfg, t.TempDir())

	if len(ids) != 1 || !ids[bundledID(t)] {
		t.Fatalf("registered %v, want only the bundled source", ids)
	}
}

func TestReadDefinitionRefusesPathsOutsideTheDirectory(t *testing.T) {
	dir := t.TempDir()

	for _, name := range []string{"../x.yml", filepath.Join(dir, "x.yml"), ""} {
		if _, err := readDefinition(dir, name); !errors.Is(err, errDefinitionEscapes) {
			t.Errorf("readDefinition(%q) = %v, want errDefinitionEscapes", name, err)
		}
	}
}

func TestReadDefinitionRefusesOversizedFiles(t *testing.T) {
	dir := t.TempDir()

	big := make([]byte, maxUserDefinitionBytes+1)
	if err := os.WriteFile(filepath.Join(dir, "big.yml"), big, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := readDefinition(dir, "big.yml"); err == nil {
		t.Fatal("readDefinition accepted a file over the size cap")
	}
}
