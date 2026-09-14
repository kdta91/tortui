package httpx

import (
	"context"
	"strings"
	"sync"
	"time"
)

// hostLimiter spaces outbound requests per host: at most one request to a
// given host per interval, with every other host unaffected. It is per host
// rather than global because two of the user's sources have no reason to
// slow each other down, and per host rather than per request because the
// thing AGENT.md §6.13 forbids is hammering one server.
//
// A slot is claimed under the lock *before* the wait happens, exactly like
// the registry's own refresh floor (DEC-054): two goroutines that both look
// at the same "next allowed" instant would otherwise both decide they are
// first and fire together. Claiming first makes the spacing hold under
// concurrency and makes the order requests are admitted in the order they
// arrived.
//
// If the wait is cut short by context cancellation the claimed slot is not
// handed back. That is deliberate — releasing it would need the reservation
// tracked and unwound, and erring towards *more* spacing after a cancelled
// request is the safe direction for a politeness limit.
type hostLimiter struct {
	mu       sync.Mutex
	interval time.Duration
	clock    Clock
	next     map[string]time.Time
}

// newHostLimiter returns a limiter admitting one request per host per
// interval. A non-positive interval disables spacing entirely.
func newHostLimiter(interval time.Duration, clock Clock) *hostLimiter {
	return &hostLimiter{
		interval: interval,
		clock:    clock,
		next:     make(map[string]time.Time),
	}
}

// wait blocks until this host's next slot is due, or until ctx is done. It
// returns ctx.Err() if the context ended first, so a cancelled context
// aborts the caller's retry loop promptly rather than sleeping it out.
func (l *hostLimiter) wait(ctx context.Context, host string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if l.interval <= 0 {
		return nil
	}

	delay := l.claim(strings.ToLower(host))
	if delay <= 0 {
		return nil
	}

	return l.clock.Sleep(ctx, delay)
}

// claim reserves this host's next slot and returns how long the caller must
// wait before using it.
func (l *hostLimiter) claim(host string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.clock.Now()

	at := l.next[host]
	if at.Before(now) {
		at = now
	}

	l.next[host] = at.Add(l.interval)

	return at.Sub(now)
}
