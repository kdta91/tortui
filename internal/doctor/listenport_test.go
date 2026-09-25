package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kdta91/tortui/internal/config"
)

// init keeps every Build in this package's tests from binding a port on a
// real interface: the default probe is swapped for one that reports the
// configured port as free (or a fixed random one for 0). Tests of the real
// probe live in internal/engine.
func init() {
	defaultProbeListenPort = func(port int) (int, bool, error) {
		if port == 0 {
			return 40000, false, nil
		}

		return port, false, nil
	}
}

func TestCheckListenPortDescribesEveryOutcome(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		configured int
		probe      func(int) (int, bool, error)
		wantPort   int
		wantDetail string
	}{
		{"free", 6881, func(p int) (int, bool, error) { return p, false, nil }, 6881, "6881 (configured, free)"},
		{"taken", 6881, func(int) (int, bool, error) { return 51234, true, nil }, 51234, "configured 6881 is in use"},
		{"random", 0, func(int) (int, bool, error) { return 40001, false, nil }, 40001, "random free port each start"},
		{"error", 6881, func(int) (int, bool, error) { return 0, true, errors.New("no sockets") }, 0, "cannot bind any port: no sockets"},
	}

	for _, c := range cases {
		port, detail := checkListenPort(Options{Config: config.Config{ListenPort: c.configured}, ProbeListenPort: c.probe})
		if port != c.wantPort || !strings.Contains(detail, c.wantDetail) {
			t.Errorf("%s: checkListenPort = %d, %q; want %d and %q", c.name, port, detail, c.wantPort, c.wantDetail)
		}
	}
}

func TestReportPrintsTheListenPortTheProbeFound(t *testing.T) {
	t.Parallel()

	const taken = 6881

	report := Build(context.Background(), Options{
		Capability: testCapability(),
		Paths:      testPaths(t, t.TempDir()),
		Config:     config.Config{DownloadDir: t.TempDir(), ListenPort: taken},
		ProbeListenPort: func(p int) (int, bool, error) {
			if p == taken {
				return 50999, true, nil
			}

			return p, false, nil
		},
	})

	if report.ListenPort != 50999 {
		t.Fatalf("ListenPort = %d, want the fallback 50999", report.ListenPort)
	}

	if out := Format(report); !strings.Contains(out, "Listen port:") || !strings.Contains(out, "50999") {
		t.Fatalf("Format output does not report the listen port:\n%s", out)
	}
}
