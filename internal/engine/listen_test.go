package engine

import (
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
)

// errReserved stands in for a port the OS refuses on one protocol (on
// Windows, a port inside an excluded range).
var errReserved = errors.New("port reserved")

// flakySockets wraps real loopback sockets. A bind on a specific (non-zero)
// port fails whenever refuse says so for its protocol; binds on port 0
// — the OS picking a random port — always go through. It records which
// protocol picked each random port, in order.
type flakySockets struct {
	mu     sync.Mutex
	refuse func(proto string) bool
	picked []string
}

func (f *flakySockets) sockets() sockets {
	return sockets{
		listenTCP: func(addr string) (net.Listener, error) {
			if err := f.bind("tcp", addr); err != nil {
				return nil, err
			}

			return netSockets.listenTCP(addr)
		},
		listenUDP: func(addr string) (net.PacketConn, error) {
			if err := f.bind("udp", addr); err != nil {
				return nil, err
			}

			return netSockets.listenUDP(addr)
		},
	}
}

func (f *flakySockets) bind(proto, addr string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if strings.HasSuffix(addr, ":0") {
		f.picked = append(f.picked, proto)
		return nil
	}

	if f.refuse(proto) {
		return errReserved
	}

	return nil
}

func TestProbeListenPortRetriesManyRandomPortsBeforeGivingUp(t *testing.T) {
	t.Parallel()

	// The second-protocol bind fails for the first five random ports,
	// whichever protocol picked them; the sixth pair is free.
	// bind serialises refuse calls, so the counter needs no lock of its own.
	var specific int

	f := &flakySockets{refuse: func(string) bool {
		specific++
		return specific <= 5
	}}

	got, fellBack, err := f.sockets().probe(loopback, 0)
	if err != nil {
		t.Fatalf("probe after five refused ports: %v", err)
	}

	if got == 0 || fellBack {
		t.Fatalf("probe = %d, fellBack %v; want a random port and no fallback (none was configured)", got, fellBack)
	}

	if len(f.picked) != 6 {
		t.Fatalf("probe tried %d random ports, want 6 (five refused, then one free)", len(f.picked))
	}
}

func TestProbeListenPortLetsUDPPickWhenTCPPicksKeepLandingOnReservedUDPPorts(t *testing.T) {
	t.Parallel()

	// Every port TCP picks is reserved for UDP, as a Windows excluded
	// range can make it. Only a UDP-picked port can ever succeed.
	f := &flakySockets{refuse: func(proto string) bool { return proto == "udp" }}

	got, _, err := f.sockets().probe(loopback, 0)
	if err != nil {
		t.Fatalf("probe with UDP reserved on every TCP-picked port: %v", err)
	}

	if got == 0 || len(f.picked) != 2 || f.picked[0] != "tcp" || f.picked[1] != "udp" {
		t.Fatalf("probe = %d after random picks %v; want a TCP pick, then a UDP pick that succeeds", got, f.picked)
	}
}

func TestProbeListenPortGivesUpAfterItsAttempts(t *testing.T) {
	t.Parallel()

	f := &flakySockets{refuse: func(string) bool { return true }}

	_, fellBack, err := f.sockets().probe(loopback, 6881)
	if err == nil {
		t.Fatal("probe succeeded with every specific-port bind refused")
	}

	if !errors.Is(err, errReserved) || !strings.Contains(err.Error(), "after 32 attempts") {
		t.Fatalf("probe error %q; want the attempt count and the last bind error", err)
	}

	if !fellBack {
		t.Fatal("a configured port that could not be bound was not reported as a fallback")
	}

	if len(f.picked) != randomPortAttempts {
		t.Fatalf("probe tried %d random ports, want %d", len(f.picked), randomPortAttempts)
	}
}
