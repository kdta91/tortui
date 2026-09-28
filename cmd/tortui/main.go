// Command tortui is the entrypoint for the tortui terminal UI torrent client.
//
// This file is wiring only: flag parsing and composition. Business logic
// lives in internal packages (see AGENT.md §4).
package main

import (
	"fmt"
	"os"

	"github.com/kdta91/tortui/internal/logging"
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
	// "doctor" and "completion" are subcommands, not flags: "doctor" must
	// work even against a dumb/non-TTY stdout (AGENT.md §15 — "safe to
	// pipe"), and "completion" is meant to be piped into a shell's own
	// completion directory (T-092), so both are dispatched before the flag
	// set below, which is unrelated to either.
	if len(args) > 0 && args[0] == "doctor" {
		return runDoctor(args[1:], out)
	}

	if len(args) > 0 && args[0] == "completion" {
		return runCompletion(args[1:], out)
	}

	// globalFlagSet (completion.go) is the single source of truth for these
	// flags: run() and `tortui completion <shell>` both build the FlagSet
	// from it, so a flag added, renamed, or removed here is automatically
	// reflected in every generated completion script (T-092 review finding
	// 1 — see TestRunAndDoctorAcceptEveryAdvertisedFlag).
	fs, g := globalFlagSet(out)

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *g.version {
		if _, err := fmt.Fprintf(out, "tortui %s (commit %s, built %s)\n", version, commit, date); err != nil {
			return 1
		}

		return 0
	}

	if *g.demo {
		return runDemo(out, *g.ascii)
	}

	// Reject a bad --log-level/TORTUI_LOG_LEVEL before anything is written.
	if _, err := logging.ParseLevel(logging.ResolveLevel("", *g.logLevel)); err != nil {
		if _, werr := fmt.Fprintln(out, err); werr != nil {
			return 1
		}

		return 1
	}

	return runApp(out, g)
}
