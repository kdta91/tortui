package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kdta91/tortui/internal/app"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// runDemo implements `tortui --demo` (AGENT.md §15): detect terminal
// capability, refuse cleanly on a non-interactive stream exactly like a
// real TUI launch must (AGENT.md §14), then hand off to
// internal/app.Demo — the composition root — for everything else. This
// function is wiring only (AGENT.md §4): it does no seeding, no scripting,
// and imports no engine or indexer package directly.
func runDemo(out *os.File) int {
	capability := theme.Detect(theme.DetectOptions{Out: out})
	if !capability.Interactive {
		if _, err := fmt.Fprintln(out, theme.RefusalMessage()); err != nil {
			return 1
		}

		return 1
	}

	demo, err := app.NewDemo(app.DemoOptions{Capability: capability})
	if err != nil {
		if _, werr := fmt.Fprintf(out, "tortui --demo: %v\n", err); werr != nil {
			return 1
		}

		return 1
	}

	if err := demo.Run(tea.WithInput(os.Stdin), tea.WithOutput(out)); err != nil {
		if _, werr := fmt.Fprintf(out, "tortui --demo: %v\n", err); werr != nil {
			return 1
		}

		return 1
	}

	return 0
}
