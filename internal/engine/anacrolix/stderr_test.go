package anacrolix

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"testing"
	"time"

	alog "github.com/anacrolix/log"

	"github.com/kdta91/tortui/internal/config"
	"github.com/kdta91/tortui/internal/engine"
)

// stderrSubprocessBaselineEnv and stderrSubprocessRealEnv select which half
// of TestStderrSubprocessHelper a re-executed test binary runs. See
// runStderrSubprocess.
const (
	stderrSubprocessBaselineEnv = "TORTUI_STDERR_SUBPROCESS_BASELINE"
	stderrSubprocessRealEnv     = "TORTUI_STDERR_SUBPROCESS_REAL"
)

// stderrSentinel is the exact message both subprocess halves try to emit
// through the same github.com/anacrolix/log façade every anacrolix package
// writes through. Its presence or absence on the child's real stderr (fd 2)
// is the entire assertion.
const stderrSentinel = "anacrolix-stderr-canary"

// TestRedirectLoggingKeepsStderrEmpty is the acceptance test for AGENT.md
// §13's most load-bearing logging rule: anacrolix/torrent and its sibling
// libraries write to os.Stderr by default, and the TUI owns the terminal, so
// once an Engine exists nothing may reach it (package doc, New's doc
// comment).
//
// This runs the assertion in a subprocess rather than swapping the
// os.Stderr package variable in this process, because
// github.com/anacrolix/log's own DefaultHandler captures the *os.File that
// os.Stderr pointed to at that package's init time (see its init.go) --
// long before any test could intervene. Reassigning the os.Stderr variable
// afterwards would not redirect writes made through that already-captured
// reference, so it would not observe the exact failure mode this test
// exists to catch. Re-executing this test binary gives the child a stderr
// pipe that the OS wires up before the child's Go runtime -- and therefore
// anacrolix/log's package init -- ever runs, so nothing written to real fd 2
// anywhere in the call chain can hide from it. This is the "run a subprocess
// via TestMain/os.Args re-exec" alternative named in the task brief.
//
// The test is two-sided so it cannot be satisfied by a handler that can
// never fail: the baseline half proves, with the library's real, unmodified
// default handler, that the exact same call genuinely lands on the process's
// real stderr when nothing has redirected it; the real half then makes that
// same call after constructing a genuine Engine (New calls RedirectLogging
// before it builds a client) and adding a genuine torrent, and requires
// stderr to be empty.
func TestRedirectLoggingKeepsStderrEmpty(t *testing.T) {
	if os.Getenv(stderrSubprocessBaselineEnv) == "1" || os.Getenv(stderrSubprocessRealEnv) == "1" {
		t.Skip("this test only runs as a dispatcher; see TestStderrSubprocessHelper")
	}

	t.Parallel()

	baseline := runStderrSubprocess(t, stderrSubprocessBaselineEnv)
	if !bytes.Contains(baseline, []byte(stderrSentinel)) {
		t.Fatalf("negative control failed: anacrolix/log's own unmodified default handler did not "+
			"write %q to the child's real stderr, so this test cannot prove the redirected case means "+
			"anything; got %q", stderrSentinel, baseline)
	}

	real := runStderrSubprocess(t, stderrSubprocessRealEnv)
	if len(real) != 0 {
		t.Fatalf("stderr is not empty after constructing a real Engine and adding a real torrent: %q", real)
	}
}

// runStderrSubprocess re-executes this test binary with only
// TestStderrSubprocessHelper selected and env set to "1", and returns
// everything the child wrote to its real stderr.
func runStderrSubprocess(t *testing.T, env string) []byte {
	t.Helper()

	cmd := exec.Command(os.Args[0], "-test.run=^TestStderrSubprocessHelper$")
	cmd.Env = append(os.Environ(), env+"=1")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = io.Discard

	if err := cmd.Run(); err != nil {
		t.Fatalf("subprocess (%s=1): %v\ncaptured stderr:\n%s", env, err, stderr.String())
	}

	return stderr.Bytes()
}

// TestStderrSubprocessHelper is not a test in its own right: `go test` only
// ever selects it when re-executed by runStderrSubprocess, dispatching on
// which of the two env vars is set. It exists as a single function, rather
// than two, so -test.run can target exactly one of them without also
// matching TestRedirectLoggingKeepsStderrEmpty.
func TestStderrSubprocessHelper(t *testing.T) {
	switch {
	case os.Getenv(stderrSubprocessBaselineEnv) == "1":
		stderrSubprocessBaseline(t)
	case os.Getenv(stderrSubprocessRealEnv) == "1":
		stderrSubprocessReal(t)
	default:
		t.Skip("only meant to run re-executed by TestRedirectLoggingKeepsStderrEmpty")
	}
}

// stderrSubprocessBaseline is the negative control: with nothing having
// redirected it, anacrolix/log's real, unmodified Default logger — the same
// façade every anacrolix package, including the torrent client, logs
// through — writes straight to this process's stderr.
func stderrSubprocessBaseline(t *testing.T) {
	t.Helper()

	alog.Default.LevelPrint(alog.Warning, stderrSentinel)
}

// stderrSubprocessReal constructs a real Engine — New redirects
// anacrolix/log into the slog sink before it builds a client, per New's own
// doc comment — adds a real torrent, and lets its metadata-timeout path run
// (exactly the kind of event a chatty library would narrate through a
// warning), then makes the identical call stderrSubprocessBaseline made,
// through the identical alog.Default façade. It also confirms that call
// landed in the engine's own slog sink instead of vanishing silently, so a
// future regression that merely stops calling RedirectLogging (rather than
// one that starts writing straight to os.Stderr some other way) is still
// caught here rather than only by the parent test's negative control.
func stderrSubprocessReal(t *testing.T) {
	t.Helper()

	var sink bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&sink, nil))

	e, err := New(Options{
		Config:          config.Config{DownloadDir: t.TempDir(), MaxPeers: 5},
		Logger:          logger,
		MetadataTimeout: 100 * time.Millisecond,
		Offline:         true,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := e.Add(context.Background(), engine.AddSource{Magnet: magnetURI("stderr-check")}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Give the metadata-timeout goroutine time to run its course.
	time.Sleep(200 * time.Millisecond)

	alog.Default.LevelPrint(alog.Warning, stderrSentinel)

	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if !bytes.Contains(sink.Bytes(), []byte(stderrSentinel)) {
		t.Fatalf("the redirected log line never reached the slog sink either: %q", sink.String())
	}
}
