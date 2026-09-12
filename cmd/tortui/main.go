// Command tortui is the entrypoint for the tortui terminal UI torrent client.
//
// This file is wiring only: flag parsing and composition. Business logic
// lives in internal packages (see AGENT.md §4).
package main

import (
	"flag"
	"fmt"
	"os"
)

// version, commit, and date are overridden at build time via -ldflags
// (see T-092). Defaults are used for local `go build`/`go run`.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

// run implements the CLI entrypoint against injectable args and output, so
// it can be exercised by tests without spawning a subprocess.
func run(args []string, out *os.File) int {
	fs := flag.NewFlagSet("tortui", flag.ContinueOnError)
	fs.SetOutput(out)

	showVersion := fs.Bool("version", false, "print version information and exit")
	// config is accepted but not yet consumed; config loading lands in T-002.
	fs.String("config", "", "path to config.toml (overrides the default XDG location)")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *showVersion {
		if _, err := fmt.Fprintf(out, "tortui %s (commit %s, built %s)\n", version, commit, date); err != nil {
			return 1
		}

		return 0
	}

	// The TUI, engine, and indexer registry are wired here in later tasks
	// (see internal/app). Nothing to compose yet.
	if _, err := fmt.Fprintln(out, "tortui: not yet implemented — see TASK_TRACKER.md"); err != nil {
		return 1
	}

	return 0
}
