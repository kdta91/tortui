package engine

import (
	"net"
	"strings"
	"testing"
	"time"
)

// loopback keeps every probe in these tests off external interfaces.
const loopback = "127.0.0.1"

func TestParseSeedPolicy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		mode, duration string
		ratio          float64
		want           SeedPolicy
		text           string
	}{
		{"ratio", "", 1.5, SeedPolicy{Mode: SeedToRatio, Ratio: 1.5}, "seeds to ratio 1.5, then stops"},
		{"duration", "2h", 0, SeedPolicy{Mode: SeedForDuration, Duration: 2 * time.Hour}, "seeds for 2h0m0s after completing, then stops"},
		{"off", "", 0, SeedPolicy{Mode: SeedOff}, "stops uploading when complete"},
	}

	for _, c := range cases {
		got, err := ParseSeedPolicy(c.mode, c.ratio, c.duration)
		if err != nil {
			t.Fatalf("ParseSeedPolicy(%q): %v", c.mode, err)
		}

		if got != c.want {
			t.Errorf("ParseSeedPolicy(%q) = %+v, want %+v", c.mode, got, c.want)
		}

		if got.String() != c.text {
			t.Errorf("String() = %q, want %q", got.String(), c.text)
		}
	}

	for _, bad := range [][3]string{{"forever", "", ""}, {"duration", "", "soon"}, {"duration", "", "0s"}} {
		if _, err := ParseSeedPolicy(bad[0], 1, bad[2]); err == nil {
			t.Errorf("ParseSeedPolicy(%q, %q) = nil error, want refusal", bad[0], bad[2])
		}
	}

	if _, err := ParseSeedPolicy("ratio", -1, ""); err == nil {
		t.Error("negative ratio accepted")
	}

	if s := (SeedPolicy{Mode: 9}).String(); !strings.Contains(s, "9") {
		t.Errorf("unknown mode String() = %q", s)
	}
}

func TestProbeListenPortPrefersTheConfiguredPort(t *testing.T) {
	t.Parallel()

	free, _, err := probeListenPort(loopback, 0)
	if err != nil {
		t.Fatalf("probeListenPort(loopback, 0): %v", err)
	}

	got, fellBack, err := probeListenPort(loopback, free)
	if err != nil {
		t.Fatalf("ProbeListenPort(%d): %v", free, err)
	}

	// Another process could grab the port between the two probes; only
	// a same-port answer or an honest fallback is acceptable.
	if got != free && !fellBack {
		t.Fatalf("ProbeListenPort(%d) = %d without reporting a fallback", free, got)
	}
}

func TestProbeListenPortFallsBackWhenTaken(t *testing.T) {
	t.Parallel()

	ln, err := net.Listen("tcp", loopback+":0")
	if err != nil {
		t.Fatalf("occupy a port: %v", err)
	}
	defer func() { _ = ln.Close() }()

	taken := ln.Addr().(*net.TCPAddr).Port

	got, fellBack, err := probeListenPort(loopback, taken)
	if err != nil {
		t.Fatalf("ProbeListenPort(%d): %v", taken, err)
	}

	if !fellBack || got == taken || got == 0 {
		t.Fatalf("ProbeListenPort(taken %d) = %d, fellBack %v; want a different random port", taken, got, fellBack)
	}
}

func TestProbeListenPortRejectsOutOfRange(t *testing.T) {
	t.Parallel()

	if _, _, err := ProbeListenPort(70000); err == nil {
		t.Fatal("ProbeListenPort(70000) accepted an out-of-range port")
	}
}
