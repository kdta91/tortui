package config

import (
	"path/filepath"
	"testing"
)

// TestDefinitionsDirSitsInsideTheConfigDir pins the path the scraper's
// definition loader reads (T-023): it is always the "definitions" folder
// inside whichever config directory was resolved, so it follows
// $TORTUI_HOME and --config rather than being resolved a second, separate
// way.
func TestDefinitionsDirSitsInsideTheConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TORTUI_HOME", home)

	paths, err := ResolvePaths("")
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}

	if want := filepath.Join(home, "config", "definitions"); paths.DefinitionsDir != want {
		t.Fatalf("DefinitionsDir is %q, want %q", paths.DefinitionsDir, want)
	}

	if want := filepath.Join(paths.ConfigDir, "definitions"); paths.DefinitionsDir != want {
		t.Fatalf("DefinitionsDir is %q, want it inside ConfigDir at %q", paths.DefinitionsDir, want)
	}
}

// TestDefinitionsDirFollowsAnExplicitConfigPath covers the other branch:
// --config moves the config directory, and the definitions folder moves
// with it.
func TestDefinitionsDirFollowsAnExplicitConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TORTUI_HOME", home)

	elsewhere := filepath.Join(t.TempDir(), "sandbox", "config.toml")

	paths, err := ResolvePaths(elsewhere)
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}

	if want := filepath.Join(filepath.Dir(elsewhere), "definitions"); paths.DefinitionsDir != want {
		t.Fatalf("DefinitionsDir is %q, want %q", paths.DefinitionsDir, want)
	}
}
