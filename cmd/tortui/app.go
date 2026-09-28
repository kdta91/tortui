package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/app"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// runApp implements a plain `tortui`: refuse a non-interactive terminal
// before anything is written (AGENT.md §14), then hand the flags to
// internal/app's composition root and run it. Wiring only (AGENT.md §4).
func runApp(out *os.File, g *globalFlags) int {
	capability := theme.Detect(theme.DetectOptions{Out: out, ForceASCII: *g.ascii})
	if !capability.Interactive {
		return fail(out, theme.RefusalMessage())
	}

	a, err := app.New(app.Options{
		ConfigPath: *g.config,
		LogLevel:   *g.logLevel,
		LogFile:    *g.logFile,
		ASCII:      *g.ascii,
		Capability: capability,
	})
	if err != nil {
		return fail(out, fmt.Sprintf("tortui: %v", err))
	}

	if err := a.Run(tea.WithInput(os.Stdin), tea.WithOutput(out)); err != nil {
		return fail(out, fmt.Sprintf("tortui: %v", err))
	}

	return 0
}

// fail prints msg to out and returns exit code 1.
func fail(out *os.File, msg string) int {
	if _, err := fmt.Fprintln(out, msg); err != nil {
		return 1
	}

	return 1
}
