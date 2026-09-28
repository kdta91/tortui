package engine

import (
	"errors"
	"fmt"
	"net"
	"strconv"
)

// randomPortAttempts is how many random ports the probe tries before giving
// up. One is not enough: the OS picks a port free on one protocol only, and
// on Windows a large share of its dynamic range can be reserved for the other
// (T-097).
const randomPortAttempts = 32

// ProbeListenPort reports the BitTorrent listen port a client starting now
// would bind for the configured port: port itself when both TCP and UDP are
// free on it, otherwise a random free port (fellBack true). A configured
// port of 0 always means random. The probe binds and immediately releases
// its sockets; it makes no outbound connection.
//
// It exists for `tortui doctor`, which runs without starting an engine and
// so has no live client to ask (T-034).
func ProbeListenPort(port int) (bound int, fellBack bool, err error) {
	return probeListenPort("", port)
}

// probeListenPort is ProbeListenPort on one host; tests use loopback so they
// never open a socket on an external interface.
func probeListenPort(host string, port int) (bound int, fellBack bool, err error) {
	return netSockets.probe(host, port)
}

// sockets is the socket layer a probe binds through. Production uses
// netSockets; tests wrap it to make chosen binds fail.
type sockets struct {
	listenTCP func(addr string) (net.Listener, error)
	listenUDP func(addr string) (net.PacketConn, error)
}

// netSockets binds real sockets.
var netSockets = sockets{
	listenTCP: func(addr string) (net.Listener, error) { return net.Listen("tcp", addr) },
	listenUDP: func(addr string) (net.PacketConn, error) { return net.ListenPacket("udp", addr) },
}

// probe is probeListenPort over s.
func (s sockets) probe(host string, port int) (bound int, fellBack bool, err error) {
	if port < 0 || port > 65535 {
		return 0, false, fmt.Errorf("listen port %d: must be between 0 and 65535", port)
	}

	if port != 0 {
		if err := s.tryBind(host, port); err == nil {
			return port, false, nil
		}
	}

	var last error

	for attempt := range randomPortAttempts {
		// Alternate which protocol picks the port: a port the OS hands
		// out as free for TCP can sit in a range reserved for UDP, and
		// the other way round.
		candidate, err := s.randomPair(host, attempt%2 == 1)
		if err == nil {
			return candidate, port != 0, nil
		}

		last = err
	}

	return 0, port != 0, fmt.Errorf("no random port was free on both TCP and UDP after %d attempts: %w",
		randomPortAttempts, last)
}

// randomPair asks the OS for a free random port on one protocol and, while
// still holding it, binds the other protocol on the same port. Holding the
// first socket means nothing can take that port between the two binds, and
// neither protocol is ever rebound right after being released. Both sockets
// are released before it returns.
func (s sockets) randomPair(host string, udpFirst bool) (int, error) {
	anyPort := net.JoinHostPort(host, "0")

	if udpFirst {
		pc, err := s.listenUDP(anyPort)
		if err != nil {
			return 0, fmt.Errorf("bind a random UDP port: %w", err)
		}

		candidate := pc.LocalAddr().(*net.UDPAddr).Port

		ln, err := s.listenTCP(net.JoinHostPort(host, strconv.Itoa(candidate)))
		if err != nil {
			return 0, errors.Join(fmt.Errorf("bind TCP port %d: %w", candidate, err), pc.Close())
		}

		if err := errors.Join(ln.Close(), pc.Close()); err != nil {
			return 0, fmt.Errorf("release probe port %d: %w", candidate, err)
		}

		return candidate, nil
	}

	ln, err := s.listenTCP(anyPort)
	if err != nil {
		return 0, fmt.Errorf("bind a random TCP port: %w", err)
	}

	candidate := ln.Addr().(*net.TCPAddr).Port

	pc, err := s.listenUDP(net.JoinHostPort(host, strconv.Itoa(candidate)))
	if err != nil {
		return 0, errors.Join(fmt.Errorf("bind UDP port %d: %w", candidate, err), ln.Close())
	}

	if err := errors.Join(pc.Close(), ln.Close()); err != nil {
		return 0, fmt.Errorf("release probe port %d: %w", candidate, err)
	}

	return candidate, nil
}

// tryBind binds TCP and UDP on host (every interface when empty) at port,
// then releases both.
func (s sockets) tryBind(host string, port int) error {
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	ln, err := s.listenTCP(addr)
	if err != nil {
		return err
	}

	pc, err := s.listenUDP(addr)
	if err != nil {
		return errors.Join(err, ln.Close())
	}

	return errors.Join(pc.Close(), ln.Close())
}
