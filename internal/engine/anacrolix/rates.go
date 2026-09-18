package anacrolix

import (
	"bytes"
	"io"
	"strings"
	"time"

	"github.com/anacrolix/torrent"
)

// rateMeter turns a monotonically increasing byte counter into a bytes-per-
// second rate by differencing consecutive samples.
//
// It is deliberately a plain struct with no clock of its own: the caller
// supplies the timestamp, which is what lets the rate arithmetic be tested
// exactly, with no sleeping and no swarm (AGENT.md §6.7).
type rateMeter struct {
	// last is when the previous sample was taken; the zero value means
	// "no sample yet", which produces a rate of 0 rather than a division
	// by zero.
	last time.Time

	// bytes is the counter value at the previous sample.
	bytes int64

	// rate is the most recently computed bytes-per-second figure.
	rate int64
}

// observe records a counter reading taken at now and updates the meter's rate.
//
// A counter that goes backwards (a torrent dropped and re-added resets the
// client's counters) restarts the measurement rather than reporting a negative
// rate. So does a non-advancing clock, which would otherwise divide by zero.
func (m *rateMeter) observe(now time.Time, counter int64) {
	prev, prevBytes := m.last, m.bytes

	m.last = now
	m.bytes = counter

	if prev.IsZero() || !now.After(prev) || counter < prevBytes {
		m.rate = 0
		return
	}

	elapsed := now.Sub(prev).Seconds()
	m.rate = int64(float64(counter-prevBytes) / elapsed)
}

// reset clears the meter, so a torrent that stopped reports no rate rather
// than the last one it happened to have.
func (m *rateMeter) reset() {
	m.last = time.Time{}
	m.bytes = 0
	m.rate = 0
}

// trimmed is strings.TrimSpace, named for how it reads at the call sites that
// decide whether an AddSource field was set.
func trimmed(s string) string { return strings.TrimSpace(s) }

// displayName is the best name available for a torrent before its info
// dictionary arrives: the magnet's display name, or its infohash.
func displayName(spec *torrent.TorrentSpec) string {
	if spec.DisplayName != "" {
		return spec.DisplayName
	}

	return spec.InfoHash.HexString()
}

// bytesReader adapts a fetched response body for metainfo.Load.
func bytesReader(b []byte) io.Reader { return bytes.NewReader(b) }
