package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/kdta91/tortui/internal/app"
	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/doctor"
	"github.com/kdta91/tortui/internal/platform"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// doctorTimeout bounds the whole `doctor` run, indexer probes included, so
// a hung network never turns a diagnostic command into a hang itself
// (AGENT.md §6.2).
const doctorTimeout = 30 * time.Second

// doctorBuiltins lists the bundled sources doctor probes. A variable so the
// cmd tests, which run the real wiring, can keep every probe off the network.
var doctorBuiltins = app.DoctorBuiltins

// runDoctor implements `tortui doctor`: build and print an environment
// report, then exit. It never starts the TUI (AGENT.md §15). All of the
// actual logic lives in internal/doctor; this function is wiring only
// (AGENT.md §4) — flag parsing, gathering the inputs internal/doctor needs,
// and translating its verdict into an exit code.
func runDoctor(args []string, out *os.File) int {
	// doctorFlagSet (completion.go) is the single source of truth for this
	// flag — see the matching comment in main.go's run().
	fs, d := doctorFlagSet(out)

	if err := fs.Parse(args); err != nil {
		return 2
	}

	loadResult, err := config.Load(*d.config)
	if err != nil {
		if _, werr := fmt.Fprintf(out, "tortui doctor: load config: %v\n", err); werr != nil {
			return 1
		}
		return 1
	}

	fdLimits, err := platform.RaiseFDLimit()
	if err != nil {
		if _, werr := fmt.Fprintf(out, "tortui doctor: raise file-descriptor limit: %v\n", err); werr != nil {
			return 1
		}
		return 1
	}

	capability := theme.Detect(theme.DetectOptions{Out: out})

	ctx, cancel := context.WithTimeout(context.Background(), doctorTimeout)
	defer cancel()

	report := doctor.Build(ctx, doctor.Options{
		Capability: capability,
		Term:       os.Getenv("TERM"),
		ColorTerm:  os.Getenv("COLORTERM"),
		Paths:      loadResult.Paths,
		Config:     loadResult.Config,
		FDLimits:   fdLimits,
		Builtins:   doctorBuiltins(),
	})

	if _, err := fmt.Fprint(out, doctor.Format(report)); err != nil {
		return 1
	}

	if doctor.HasHardProblem(report) {
		return 1
	}

	return 0
}
