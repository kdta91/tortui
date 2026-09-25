package engine

import (
	"fmt"
	"strconv"
	"time"
)

// SeedMode selects what happens to a torrent once its download completes.
type SeedMode int

const (
	// SeedToRatio keeps uploading until the bytes uploaded reach
	// SeedPolicy.Ratio times the torrent's size, then stops.
	SeedToRatio SeedMode = iota
	// SeedForDuration keeps uploading for SeedPolicy.Duration after
	// completion, then stops.
	SeedForDuration
	// SeedOff stops uploading the moment the download completes.
	SeedOff
)

// SeedPolicy is the seeding policy an engine applies to every completed
// torrent. There is deliberately no "seed forever" mode: a client that keeps
// uploading after completion without saying so burns a metered connection
// (AGENT.md §13). String renders the policy as the sentence the UI shows, so
// continued upload is never a surprise.
type SeedPolicy struct {
	Mode     SeedMode
	Ratio    float64
	Duration time.Duration
}

// ParseSeedPolicy builds a SeedPolicy from the three config.toml keys
// (seed_policy, seed_ratio, seed_duration). mode is "ratio", "duration", or
// "off"; duration is a Go duration string and is only parsed for "duration".
func ParseSeedPolicy(mode string, ratio float64, duration string) (SeedPolicy, error) {
	switch mode {
	case "ratio":
		if ratio < 0 {
			return SeedPolicy{}, fmt.Errorf("seed_ratio %v: must not be negative", ratio)
		}

		return SeedPolicy{Mode: SeedToRatio, Ratio: ratio}, nil

	case "duration":
		d, err := time.ParseDuration(duration)
		if err != nil {
			return SeedPolicy{}, fmt.Errorf("seed_duration: %w", err)
		}

		if d <= 0 {
			return SeedPolicy{}, fmt.Errorf("seed_duration %s: must be positive", d)
		}

		return SeedPolicy{Mode: SeedForDuration, Duration: d}, nil

	case "off":
		return SeedPolicy{Mode: SeedOff}, nil

	default:
		return SeedPolicy{}, fmt.Errorf("seed_policy %q: want ratio, duration, or off", mode)
	}
}

// String describes the policy in the words the downloads screen uses, e.g.
// "seeds to ratio 1.0, then stops".
func (p SeedPolicy) String() string {
	switch p.Mode {
	case SeedToRatio:
		return "seeds to ratio " + strconv.FormatFloat(p.Ratio, 'f', 1, 64) + ", then stops"
	case SeedForDuration:
		return "seeds for " + p.Duration.String() + " after completing, then stops"
	case SeedOff:
		return "stops uploading when complete"
	default:
		return fmt.Sprintf("seed policy(%d)", int(p.Mode))
	}
}

// Queuer is implemented by an Engine that caps concurrent downloads and
// queues the rest (config max_active_downloads). It is a separate, optional
// interface rather than a change to the frozen Engine contract (AGENT.md §5):
// a caller discovers it with a type assertion, and an Engine without a queue
// simply does not implement it.
//
// A queued torrent reports StateQueued and starts automatically, in Queue
// order, as active downloads complete, pause, fail, or are removed.
type Queuer interface {
	// Queue returns the IDs of every queued torrent, next-to-start first.
	Queue() []string

	// MoveInQueue moves a queued torrent to position (0 = next to start),
	// clamping a position past either end to that end. It returns an error
	// for an unknown ID or one that is not currently queued.
	MoveInQueue(id string, position int) error
}
