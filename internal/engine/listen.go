package engine

import (
	"errors"
	"fmt"
	"net"
	"strconv"
)

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
	if port < 0 || port > 65535 {
		return 0, false, fmt.Errorf("listen port %d: must be between 0 and 65535", port)
	}

	if port != 0 {
		if err := tryBind(host, port); err == nil {
			return port, false, nil
		}
	}

	for range 8 {
		ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
		if err != nil {
			return 0, port != 0, fmt.Errorf("bind a random listen port: %w", err)
		}

		candidate := ln.Addr().(*net.TCPAddr).Port

		if err := ln.Close(); err != nil {
			return 0, port != 0, fmt.Errorf("release probe listener: %w", err)
		}

		if tryBind(host, candidate) == nil {
			return candidate, port != 0, nil
		}
	}

	return 0, port != 0, errors.New("no random port was free on both TCP and UDP")
}

// tryBind binds TCP and UDP on host (every interface when empty) at port,
// then releases both.
func tryBind(host string, port int) error {
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		return errors.Join(err, ln.Close())
	}

	return errors.Join(pc.Close(), ln.Close())
}
