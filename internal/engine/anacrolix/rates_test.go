package anacrolix

import (
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/kdta91/tortui/internal/engine"
)

func TestRateMeterDifferencesConsecutiveSamples(t *testing.T) {
	t.Parallel()

	var m rateMeter
	base := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

	m.observe(base, 1000)
	if m.rate != 0 {
		t.Errorf("rate after one sample = %d, want 0", m.rate)
	}

	m.observe(base.Add(2*time.Second), 5000)
	if m.rate != 2000 {
		t.Errorf("rate = %d, want 2000 B/s (4000 bytes over 2s)", m.rate)
	}

	m.observe(base.Add(3*time.Second), 5000)
	if m.rate != 0 {
		t.Errorf("rate with no new bytes = %d, want 0", m.rate)
	}
}

func TestRateMeterHandlesACounterReset(t *testing.T) {
	t.Parallel()

	var m rateMeter
	base := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

	m.observe(base, 9000)
	m.observe(base.Add(time.Second), 10)

	if m.rate != 0 {
		t.Errorf("rate after a counter reset = %d, want 0 rather than a negative rate", m.rate)
	}

	m.observe(base.Add(2*time.Second), 1010)
	if m.rate != 1000 {
		t.Errorf("rate after resuming = %d, want 1000", m.rate)
	}
}

func TestRateMeterHandlesANonAdvancingClock(t *testing.T) {
	t.Parallel()

	var m rateMeter
	base := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

	m.observe(base, 100)
	m.observe(base, 500)

	if m.rate != 0 {
		t.Errorf("rate with no elapsed time = %d, want 0 rather than a division by zero", m.rate)
	}
}

func TestRateMeterReset(t *testing.T) {
	t.Parallel()

	var m rateMeter
	base := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

	m.observe(base, 0)
	m.observe(base.Add(time.Second), 1000)
	m.reset()

	if m.rate != 0 || m.bytes != 0 || !m.last.IsZero() {
		t.Errorf("reset left %+v", m)
	}
}

func TestProgress(t *testing.T) {
	t.Parallel()

	cases := []struct {
		done, total int64
		want        float64
	}{
		{0, 0, 0},
		{0, 100, 0},
		{25, 100, 0.25},
		{100, 100, 1},
		{150, 100, 1},
		{50, 0, 0},
		{-1, 100, 0},
	}

	for _, tc := range cases {
		if got := progress(tc.done, tc.total); got != tc.want {
			t.Errorf("progress(%d, %d) = %v, want %v", tc.done, tc.total, got, tc.want)
		}
	}
}

func TestETA(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		state       engine.State
		total, done int64
		rate        int64
		want        time.Duration
	}{
		"downloading":      {engine.StateDownloading, 1000, 200, 100, 8 * time.Second},
		"no rate":          {engine.StateDownloading, 1000, 200, 0, -1},
		"no metadata":      {engine.StateDownloading, 0, 0, 100, -1},
		"already complete": {engine.StateDownloading, 1000, 1000, 100, -1},
		"checking":         {engine.StateChecking, 1000, 0, 100, -1},
		"seeding":          {engine.StateSeeding, 1000, 1000, 100, -1},
		"errored":          {engine.StateErrored, 1000, 200, 100, -1},
	}

	for name, tc := range cases {
		if got := eta(tc.state, tc.total, tc.done, tc.rate); got != tc.want {
			t.Errorf("%s: eta = %s, want %s", name, got, tc.want)
		}
	}
}

func TestRateLimiterTreatsZeroAsUnlimited(t *testing.T) {
	t.Parallel()

	for _, bps := range []int64{0, -1} {
		if got := rateLimiter(bps).Limit(); got != rate.Inf {
			t.Errorf("rateLimiter(%d) limit = %v, want rate.Inf", bps, got)
		}
	}

	limited := rateLimiter(4096)
	if got := limited.Limit(); float64(got) != 4096 {
		t.Errorf("rateLimiter(4096) limit = %v, want 4096", got)
	}

	if limited.Burst() <= 0 {
		t.Errorf("rateLimiter(4096) burst = %d; a zero burst deadlocks a transfer", limited.Burst())
	}
}
