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

	// rate is the most recently computed bytes-per-second figure: the
	// difference between the last two samples only, so it tracks bursts.
	rate int64

	// window holds up to rateWindow+1 of the most recent samples, oldest
	// first. avg is computed across it.
	window []rateSample

	// avg is the rolling average rate across window: the bytes gained
	// between its oldest and newest sample over the time between them. It
	// is what ETA is computed from, so one bursty or idle sample does not
	// swing the estimate the way the instantaneous rate would.
	avg int64
}

// rateWindow is how many sample intervals the rolling average spans: 10
// intervals, or five seconds at DefaultRateSampleInterval.
const rateWindow = 10

// rateSample is one counter reading.
type rateSample struct {
	at    time.Time
	bytes int64
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
		// The history no longer describes the same counter (or the same
		// clock), so the rolling window restarts from this reading too.
		m.rate = 0
		m.avg = 0
		m.window = append(m.window[:0], rateSample{at: now, bytes: counter})

		return
	}

	elapsed := now.Sub(prev).Seconds()
	m.rate = int64(float64(counter-prevBytes) / elapsed)

	m.window = append(m.window, rateSample{at: now, bytes: counter})
	if n := len(m.window); n > rateWindow+1 {
		m.window = append(m.window[:0], m.window[n-rateWindow-1:]...)
	}

	oldest := m.window[0]
	m.avg = int64(float64(counter-oldest.bytes) / now.Sub(oldest.at).Seconds())
}

// reset clears the meter, so a torrent that stopped reports no rate rather
// than the last one it happened to have.
func (m *rateMeter) reset() {
	m.last = time.Time{}
	m.bytes = 0
	m.rate = 0
	m.avg = 0
	m.window = m.window[:0]
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
