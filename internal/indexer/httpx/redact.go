package httpx

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// redactedPlaceholder replaces a credential value anywhere it would
// otherwise be about to appear in an error message.
const redactedPlaceholder = "[REDACTED]"

// redactor removes the exact credential values this client was configured
// with from any text about to become an error message.
//
// It is a backstop, not the primary defence. The primary defence is
// structural: no error this package builds ever contains a request URL, a
// query string, a request or response header, or a response body excerpt —
// only the method, the host, and a status or cause. The backstop exists
// because the causes come from net/http, which does not share that
// discipline: a *url.Error stringifies the full URL it was given, query
// string and all, and net/http returns one from essentially every failed
// request.
type redactor struct {
	secrets []string
}

// newRedactor returns a redactor that scrubs every non-empty value in
// secrets. No minimum length is imposed: a short credential garbling an
// error message is a far better outcome than a leaked one, and the
// structural rule above means the situation should not arise at all.
func newRedactor(secrets ...string) *redactor {
	kept := make([]string, 0, len(secrets))

	for _, s := range secrets {
		if s != "" {
			kept = append(kept, s)
		}
	}

	return &redactor{secrets: kept}
}

// scrub replaces every configured credential value found in s.
func (r *redactor) scrub(s string) string {
	for _, secret := range r.secrets {
		s = strings.ReplaceAll(s, secret, redactedPlaceholder)
	}

	return s
}

// safeError is an error whose message has already been scrubbed and is
// known not to contain a URL, a header, or a body excerpt. The cause stays
// reachable through errors.Is and errors.As so callers can still test for
// context.DeadlineExceeded, net.Error, and the sentinels below.
//
// The cause is never a *url.Error: unwrapURLError strips that layer off
// before it is stored, because *url.Error.Error() prints the full URL — and
// with it any api_key the user's source expects in the query string. A
// caller that reaches the cause and formats it therefore cannot print one
// either.
type safeError struct {
	msg   string
	cause error
}

// Error returns the scrubbed message.
func (e *safeError) Error() string { return e.msg }

// Unwrap returns the underlying cause, with any *url.Error layer already
// removed.
func (e *safeError) Unwrap() error { return e.cause }

// safef builds a safeError from a format string, scrubbing the result and
// stripping any *url.Error from the cause chain first. The format string
// must never be handed a URL, a header value, or a body excerpt; pass the
// host instead (see hostOf).
func (r *redactor) safef(cause error, format string, args ...any) error {
	cause = unwrapURLError(cause)

	return &safeError{
		msg:   r.scrub(fmt.Sprintf(format, args...)),
		cause: cause,
	}
}

// unwrapURLError removes every *url.Error layer from err's chain. net/http
// wraps essentially every request failure in one, and its Error() method
// formats as `Op "the-full-url": cause` — so keeping it reachable would
// leave a credential-bearing query string one fmt.Errorf("%v", cause) away
// from a log file, however careful this package's own messages were.
//
// Everything below that layer is preserved, so errors.Is(err,
// context.DeadlineExceeded), errors.As(err, &netErr) and the like keep
// working; the transport-level causes underneath (*net.OpError and friends)
// name a host and port, never a query string.
func unwrapURLError(err error) error {
	for {
		var urlErr *url.Error
		if !errors.As(err, &urlErr) {
			return err
		}

		if urlErr.Err == nil {
			return errors.New(urlErr.Op + ": request failed")
		}

		err = urlErr.Err
	}
}

// hostOf returns the host:port of u, lowercased, which is the most detail
// any message from this package is allowed to carry about where a request
// went. It deliberately drops the scheme, any userinfo, the path, and the
// query: a path can embed a key (`/api/<key>/search`) and a query routinely
// does.
func hostOf(u *url.URL) string {
	if u == nil {
		return "unknown host"
	}

	if u.Host == "" {
		return "unknown host"
	}

	return strings.ToLower(u.Host)
}
