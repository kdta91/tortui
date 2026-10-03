package app

import (
	"fmt"
	"log/slog"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/lifecycle"
)

// startupNotices is what the first render tells the user about startup, in
// order: where a first run wrote its config, config problems, a store that
// had to be set aside, resumed torrents that came back errored, and
// duplicate records Resume dropped.
func startupNotices(loaded config.LoadResult, storeRecovered string, report lifecycle.ResumeReport) []string {
	var notices []string

	if loaded.FirstRun {
		notices = append(notices, "wrote a default config to "+loaded.Paths.ConfigFile)
	}

	if n := len(loaded.Problems) + len(loaded.Warnings); n > 0 {
		notices = append(notices, fmt.Sprintf("config.toml: %s — run `tortui doctor` for details", plural(n, "problem")))
	}

	if storeRecovered != "" {
		notices = append(notices, storeRecovered)
	}

	if n := len(report.Missing); n > 0 {
		notices = append(notices, fmt.Sprintf("%s with missing data on disk — see Downloads", plural(n, "resumed download")))
	}

	if n := len(report.Failed); n > 0 {
		notices = append(notices, fmt.Sprintf("%s could not be restored — see Downloads", plural(n, "resumed download")))
	}

	return append(notices, droppedNotices(report.Dropped)...)
}

// droppedNotices tells the user about duplicate records Resume dropped
// (T-9127): one line per record whose data sat elsewhere, naming where it is
// left unmanaged, and one count for the rest.
func droppedNotices(dropped []lifecycle.DroppedRecord) []string {
	var (
		notices []string
		same    int
	)

	for _, d := range dropped {
		if !d.Elsewhere {
			same++
			continue
		}

		name := d.Name
		if name == "" {
			name = d.ID
		}

		notices = append(notices, fmt.Sprintf(
			"dropped a duplicate record of %s; its data in %s is no longer managed — tortui deleted nothing", name, d.SavePath))
	}

	if same > 0 {
		notices = append(notices, fmt.Sprintf("dropped %s of downloads already resumed", plural(same, "duplicate record")))
	}

	return notices
}

// plural renders "1 thing" or "n things".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}

	return fmt.Sprintf("%d %ss", n, noun)
}

// absPaths returns each of dirs as a clean absolute path, dropping (and
// logging) any that cannot be one.
func absPaths(dirs []string, logger *slog.Logger) []string {
	out := make([]string, 0, len(dirs))

	for _, d := range dirs {
		abs, err := engine.CleanAbsPath(d)
		if err != nil {
			logger.Warn("app: ignoring saved destination", "path", d, "error", err)
			continue
		}

		out = append(out, abs)
	}

	return out
}

// minFreeSpace parses min_free_space, falling back to the default margin
// when the value is unusable (config validation has already reported it).
func minFreeSpace(s string, logger *slog.Logger) int64 {
	n, err := config.ParseByteSize(s)
	if err == nil {
		return n
	}

	logger.Warn("app: min_free_space unusable; using the default", "value", s, "error", err)

	n, err = config.ParseByteSize(config.Default("").MinFreeSpace)
	if err != nil {
		return 0
	}

	return n
}
