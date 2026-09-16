// Package doctor builds the environment report `tortui doctor` prints
// (T-055, AGENT.md §15): OS/arch, terminal capability, resolved paths,
// file-descriptor limits, and a reachability verdict for every configured
// indexer. It never starts the TUI and never touches the terminal beyond
// what its caller (cmd/tortui/doctor.go) already resolved.
//
// Everything here is plain data plus formatting: no bubbletea, no
// business logic beyond "ask the environment a question and describe the
// answer," which is what keeps cmd/tortui/doctor.go a thin wiring shim
// (AGENT.md §4).
package doctor

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/indexer/httpx"
	"github.com/kdta91/tortui/internal/logging"
	"github.com/kdta91/tortui/internal/platform"
	"github.com/kdta91/tortui/internal/tui/theme"
)

// DefaultIndexerTimeout bounds one indexer's reachability probe
// (AGENT.md §6.2: "every network call takes a context.Context with a
// deadline"). It is deliberately short: doctor is a diagnostic that must
// return promptly even when a configured source is completely dead, not a
// search.
const DefaultIndexerTimeout = 5 * time.Second

// IndexerVerdict is one configured indexer's reachability outcome.
type IndexerVerdict struct {
	// ID and Name identify the indexer. Never its URL, API key, or
	// cookie — see Report's doc comment.
	ID   string
	Name string

	// Checked is false when the indexer was skipped (disabled, or has no
	// URL configured) rather than actually probed.
	Checked bool

	// Reachable is true when the probe got any HTTP response at all,
	// success or not — a 401 from a private indexer still means the host
	// answered. It is meaningless when Checked is false.
	Reachable bool

	// Detail is a short, human-readable outcome, e.g. "reachable (HTTP
	// 200, 143ms)", "unreachable: dial tcp: connect: connection refused",
	// or "skipped: disabled". It has already been passed through
	// logging.Redact, so even a raw net/http error naming the request URL
	// (and therefore any api_key query parameter) cannot leak a
	// credential through this field.
	Detail string
}

// Report is everything `tortui doctor` prints.
//
// Nothing on this type or reachable from it may carry a raw indexer URL,
// API key, or session credential — IndexerVerdict deliberately has no URL
// field, and every string built from a network error is redacted before
// it is stored. This is a hard requirement (AGENT.md §6.6, §9) verified by
// TestBuildNeverLeaksCredentials.
type Report struct {
	OS   string
	Arch string

	// Term and ColorTerm are the raw TERM/COLORTERM environment values.
	Term      string
	ColorTerm string

	// ColorProfile, Unicode, Multiplexer, Width, Height, and Interactive
	// mirror theme.Capability — see internal/tui/theme/capability.go
	// (T-050, extended in T-055 with Width/Height/Multiplexer).
	ColorProfile string
	Unicode      bool
	Multiplexer  string
	Width        int
	Height       int
	Interactive  bool

	ConfigFile     string
	StateDir       string
	DownloadDir    string
	DefinitionsDir string

	FDLimits platform.FDLimits

	// DownloadDirWritable reports whether DownloadDir (creating it first,
	// if missing) actually accepted a test file. DownloadDirProblem
	// explains a false value.
	DownloadDirWritable bool
	DownloadDirProblem  string

	Indexers []IndexerVerdict
}

// Options configures Build. Every field is required except Timeout and
// NewClient, which default when zero/nil.
type Options struct {
	Capability theme.Capability
	Term       string
	ColorTerm  string
	Paths      config.Paths
	Config     config.Config

	// FDLimits is the outcome of platform.RaiseFDLimit, run by the caller
	// before Build so Build itself stays free of process-wide side
	// effects (AGENT.md §8: rendering/reporting should be easy to test in
	// isolation).
	FDLimits platform.FDLimits

	// Timeout bounds one indexer's reachability probe. Zero uses
	// DefaultIndexerTimeout.
	Timeout time.Duration

	// NewClient builds the HTTP client used to probe one indexer. Zero
	// uses a real httpx.Client per indexer, carrying that indexer's own
	// credentials. Tests override this to point at an httptest.Server
	// instead of the network (AGENT.md §6.7).
	NewClient func(ix config.Indexer, timeout time.Duration) *httpx.Client
}

// Build assembles a Report. It performs I/O — a download-directory write
// probe and one HTTP request per enabled, URL-bearing indexer — but every
// one of those calls carries a bounded deadline (via ctx and Options.Timeout)
// so Build always returns promptly even when every configured source is
// unreachable.
func Build(ctx context.Context, opts Options) Report {
	r := Report{
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,

		Term:      opts.Term,
		ColorTerm: opts.ColorTerm,

		ColorProfile: opts.Capability.Color.String(),
		Unicode:      opts.Capability.Unicode,
		Multiplexer:  opts.Capability.Multiplexer,
		Width:        opts.Capability.Width,
		Height:       opts.Capability.Height,
		Interactive:  opts.Capability.Interactive,

		ConfigFile:     opts.Paths.ConfigFile,
		StateDir:       opts.Paths.StateDir,
		DownloadDir:    opts.Paths.DownloadDir,
		DefinitionsDir: opts.Paths.DefinitionsDir,

		FDLimits: opts.FDLimits,
	}

	r.DownloadDirWritable, r.DownloadDirProblem = checkWritable(opts.Paths.DownloadDir)
	r.Indexers = checkIndexers(ctx, opts)

	return r
}

// checkWritable reports whether dir exists (creating it if missing) and
// accepts a file write, without leaving anything behind. AGENT.md §11's
// "unwritable download dir" hard problem is exactly this check.
func checkWritable(dir string) (writable bool, problem string) {
	if strings.TrimSpace(dir) == "" {
		return false, "download_dir is empty"
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, logging.Redact(fmt.Sprintf("cannot create: %v", err))
	}

	probe, err := os.CreateTemp(dir, ".tortui-doctor-probe-*")
	if err != nil {
		return false, logging.Redact(fmt.Sprintf("cannot write: %v", err))
	}

	name := probe.Name()

	if err := probe.Close(); err != nil {
		_ = os.Remove(name)
		return false, logging.Redact(fmt.Sprintf("cannot close probe file: %v", err))
	}

	if err := os.Remove(name); err != nil {
		return false, logging.Redact(fmt.Sprintf("cannot remove probe file: %v", err))
	}

	return true, ""
}

// checkIndexers probes every configured indexer concurrently, each under
// its own deadline, and returns verdicts in the same order the indexers
// were configured in.
func checkIndexers(ctx context.Context, opts Options) []IndexerVerdict {
	indexers := opts.Config.Indexers
	verdicts := make([]IndexerVerdict, len(indexers))

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultIndexerTimeout
	}

	newClient := opts.NewClient
	if newClient == nil {
		newClient = defaultClient
	}

	var wg sync.WaitGroup
	for i, ix := range indexers {
		verdicts[i] = IndexerVerdict{ID: ix.ID, Name: ix.Name}

		if !ix.Enabled {
			verdicts[i].Detail = "skipped: disabled"
			continue
		}

		if strings.TrimSpace(ix.URL) == "" {
			verdicts[i].Detail = "skipped: no URL configured"
			continue
		}

		wg.Add(1)
		go func(i int, ix config.Indexer) {
			defer wg.Done()
			verdicts[i] = probeOne(ctx, newClient(ix, timeout), ix, timeout)
		}(i, ix)
	}
	wg.Wait()

	return verdicts
}

// defaultClient builds a real httpx.Client for probing one indexer,
// carrying that indexer's own user-supplied credentials (AGENT.md §2) so
// the reachability check reflects what a real search would see rather
// than an anonymous request a private indexer would answer differently.
func defaultClient(ix config.Indexer, timeout time.Duration) *httpx.Client {
	return httpx.New(httpx.Config{
		RequestTimeout: timeout,
		MaxAttempts:    1, // a diagnostic probe never retries (AGENT.md §6.13)
		Credentials: httpx.Credentials{
			APIKey:       ix.APIKey,
			CookieHeader: ix.Cookie,
		},
	})
}

// probeOne issues one bounded GET against ix.URL and classifies the
// outcome. Any HTTP response at all — including a non-2xx one, reported by
// httpx as a *httpx.StatusError — counts as reachable, since the point is
// "is the host there," not "did this unauthenticated probe succeed."
func probeOne(ctx context.Context, client *httpx.Client, ix config.Indexer, timeout time.Duration) IndexerVerdict {
	v := IndexerVerdict{ID: ix.ID, Name: ix.Name, Checked: true}

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()

	resp, err := client.Get(reqCtx, ix.URL, nil)

	var statusErr *httpx.StatusError
	switch {
	case err == nil:
		v.Reachable = true
		v.Detail = fmt.Sprintf("reachable (HTTP %d, %s)", resp.StatusCode, time.Since(start).Round(time.Millisecond))
	case errors.As(err, &statusErr):
		v.Reachable = true
		v.Detail = fmt.Sprintf("reachable, HTTP %d %s (%s)", statusErr.StatusCode, http.StatusText(statusErr.StatusCode), time.Since(start).Round(time.Millisecond))
	default:
		v.Reachable = false
		// httpx's own errors never carry a URL (see internal/indexer/httpx
		// doc), but logging.Redact runs anyway as a second, independent
		// layer — the same defense-in-depth internal/logging already
		// applies to every log line.
		v.Detail = "unreachable: " + logging.Redact(err.Error())
	}

	return v
}

// Format renders r as plain text: no ANSI escapes, no box-drawing glyphs,
// safe to pipe or paste verbatim into a bug report (AGENT.md §15).
func Format(r Report) string {
	var b strings.Builder

	fmt.Fprintf(&b, "tortui doctor\n")
	fmt.Fprintf(&b, "OS/Arch:        %s/%s\n", r.OS, r.Arch)
	fmt.Fprintf(&b, "TERM:           %s\n", displayOrUnset(r.Term))
	fmt.Fprintf(&b, "COLORTERM:      %s\n", displayOrUnset(r.ColorTerm))
	fmt.Fprintf(&b, "Color profile:  %s\n", r.ColorProfile)
	fmt.Fprintf(&b, "Unicode:        %s\n", yesNo(r.Unicode))
	fmt.Fprintf(&b, "Multiplexer:    %s\n", displayOrNone(r.Multiplexer))
	fmt.Fprintf(&b, "Terminal size:  %s\n", sizeString(r.Width, r.Height))
	fmt.Fprintf(&b, "Interactive:    %s\n", yesNo(r.Interactive))
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "Config file:    %s\n", r.ConfigFile)
	fmt.Fprintf(&b, "State dir:      %s\n", r.StateDir)
	fmt.Fprintf(&b, "Download dir:   %s\n", r.DownloadDir)
	fmt.Fprintf(&b, "Definitions:    %s\n", r.DefinitionsDir)
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "Download dir writable: %s\n", yesNoProblem(r.DownloadDirWritable, r.DownloadDirProblem))
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "File descriptors:\n")

	if r.FDLimits.Supported {
		fmt.Fprintf(&b, "  soft (original): %d\n", r.FDLimits.Soft)
		fmt.Fprintf(&b, "  soft (raised):   %d\n", r.FDLimits.Raised)
		fmt.Fprintf(&b, "  hard:            %d\n", r.FDLimits.Hard)
	} else {
		fmt.Fprintf(&b, "  not applicable on %s (no per-process descriptor limit)\n", r.OS)
	}

	fmt.Fprintf(&b, "\n")

	if len(r.Indexers) == 0 {
		fmt.Fprintf(&b, "Indexers: none configured\n")
	} else {
		fmt.Fprintf(&b, "Indexers:\n")

		// Printed in configured order, not sorted — that order is the
		// user's own config.toml and is the most useful one for matching
		// a doctor report back to it.
		for _, v := range r.Indexers {
			fmt.Fprintf(&b, "  %s (%s): %s\n", displayOrUnset(v.Name), displayOrUnset(v.ID), v.Detail)
		}
	}

	return b.String()
}

// HasHardProblem reports whether r describes a condition serious enough to
// make `tortui doctor` exit non-zero (AGENT.md §15's own text, and the
// T-055 acceptance criteria): TERM=dumb, or a download directory that
// cannot be written to. An unreachable indexer is informational, not a
// hard failure — that is exactly what the verdict column is for.
func HasHardProblem(r Report) bool {
	return r.Term == "dumb" || !r.DownloadDirWritable
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func yesNoProblem(ok bool, problem string) string {
	if ok {
		return "yes"
	}
	if problem == "" {
		return "no"
	}
	return "no (" + problem + ")"
}

func displayOrUnset(s string) string {
	if s == "" {
		return "(unset)"
	}
	return s
}

func displayOrNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func sizeString(w, h int) string {
	if w <= 0 || h <= 0 {
		return "unknown"
	}
	return fmt.Sprintf("%dx%d", w, h)
}
