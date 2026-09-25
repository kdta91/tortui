// Package httpx is the one HTTP client every indexer adapter talks to the
// network through (T-020). It exists so the rules that apply to talking to
// someone else's server — a deadline on every call, spacing between
// requests to the same host, a bounded number of retries, a bounded
// response size, and credentials that never reach a log file — are
// implemented once instead of once per adapter.
//
// Three things about it are load-bearing rather than incidental:
//
//   - Credentials are only ever the ones the user supplied from their own
//     account (AGENT.md §2). This package injects an api_key query
//     parameter and a Cookie request header and does nothing else with
//     authentication: it never discovers a credential, never harvests a
//     session, never solves a challenge, and never negotiates around an
//     access control.
//
//   - No error this package produces contains a request URL, a query
//     string, a request or response header, or a response body excerpt.
//     Only the request method, the host, and a status or cause. That is not
//     tidiness: internal/logging masks by key name and value shape, so an
//     opaque api_key sitting inside an error string under a key named
//     something like "err" would reach the user's log file in plaintext.
//     See redact.go, which also explains why *url.Error is stripped out of
//     every cause chain.
//
//   - Every wait goes through a Clock and every wait is interruptible by
//     context cancellation, so a cancelled search abandons a pending
//     backoff immediately (AGENT.md §6.2) and tests never sleep on the wall
//     clock.
package httpx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Defaults applied to any Config field left at its zero value. Each is a
// ceiling on how hard tortui may lean on someone else's server (AGENT.md
// §2, §6.13) rather than a performance knob.
const (
	// DefaultUserAgent identifies tortui without naming any address.
	// AGENT.md §2 keeps hostnames out of internal/indexer entirely, and a
	// user-agent string is not worth an exception.
	DefaultUserAgent = "tortui"

	// DefaultConnectTimeout bounds establishing the TCP connection and the
	// TLS handshake.
	DefaultConnectTimeout = 10 * time.Second

	// DefaultReadTimeout bounds waiting for the response after the request
	// has been sent.
	DefaultReadTimeout = 20 * time.Second

	// DefaultRequestTimeout bounds one Do call end to end, including every
	// retry, every backoff, and every rate-limiter wait. It is applied to
	// the caller's context, so a caller with a shorter deadline keeps it
	// and a caller with none still gets one (AGENT.md §6.2).
	DefaultRequestTimeout = 60 * time.Second

	// DefaultMinHostInterval is the minimum spacing between two requests to
	// the same host. It matches the registry's per-source refresh floor
	// (indexer.DefaultMinRefreshInterval) deliberately: the two guard
	// different layers of the same rule.
	DefaultMinHostInterval = 1 * time.Second

	// DefaultMaxAttempts is the total number of attempts, not the number of
	// retries: 4 means one request and up to three retries.
	DefaultMaxAttempts = 4

	// DefaultBaseBackoff is the delay after the first failed attempt; it
	// doubles from there.
	DefaultBaseBackoff = 500 * time.Millisecond

	// DefaultMaxBackoff caps the exponential growth.
	DefaultMaxBackoff = 8 * time.Second

	// DefaultMaxRetryAfter is the longest server-requested Retry-After
	// delay this client will honour by waiting. Anything longer ends the
	// attempt loop instead (see Config.MaxRetryAfter).
	DefaultMaxRetryAfter = 30 * time.Second

	// DefaultMaxBodyBytes is the response body size cap: 8 MB, per T-020.
	DefaultMaxBodyBytes int64 = 8 << 20

	// DefaultAPIKeyParam is the query parameter an api_key is injected as
	// when Config.APIKeyParam is empty. It matches the Torznab/Newznab
	// convention.
	DefaultAPIKeyParam = "apikey"
)

// headerCookie is the request header a user-supplied session value is sent
// in.
const headerCookie = "Cookie"

// maxRedirects is how many redirect hops one request may take.
const maxRedirects = 5

// drainLimit is how much of an unwanted response body is read before the
// connection is returned to the pool. Reading a little keeps the connection
// reusable; reading all of it would be an unbounded read through the very
// door the size cap exists to close.
const drainLimit int64 = 64 << 10

// Errors this package reports. Callers match them with errors.Is; the
// concrete error carrying one names the method and host but never a URL.
//
// The URL-shaped sentinels are named prefix-first (ErrURLEmpty rather than
// ErrEmptyURL) because scripts/check-indexer-hostnames.sh reads any
// `...url =` or `...host =` line inside internal/indexer as a hostname
// assignment and flags it. The names below say the same thing and keep that
// check meaningful; see backlog T-926.
var (
	// ErrBodyTooLarge reports a response body larger than the configured
	// cap. The body is discarded rather than truncated: a torznab feed cut
	// off mid-document parses as a short feed, which is silent data loss.
	ErrBodyTooLarge = errors.New("response body exceeds the size cap")

	// ErrURLEmpty reports a Request with no URL.
	ErrURLEmpty = errors.New("request URL is empty")

	// ErrURLInvalid reports a Request URL that will not parse.
	ErrURLInvalid = errors.New("request URL is not usable")

	// ErrURLSchemeUnsupported reports a Request URL that is not http or https.
	ErrURLSchemeUnsupported = errors.New("request URL scheme must be http or https")

	// ErrURLHostMissing reports a Request URL with no host.
	ErrURLHostMissing = errors.New("request URL has no host")

	// ErrCrossHostRedirect reports a redirect that would leave the host the
	// request was addressed to. It is refused rather than followed: the
	// user's api_key travels in the query string, so following a redirect
	// off-host would hand their credential to a server they never
	// configured.
	//
	// The comparison is on the URL host as written, so an explicit default
	// port counts as a different host (example.org and example.org:80).
	// That direction fails closed — a legitimate redirect is refused, no
	// credential moves — and normalising it is backlog T-928.
	ErrCrossHostRedirect = errors.New("refusing to follow a redirect to a different host")

	// ErrInsecureRedirect reports a same-host redirect that would move the
	// request from https to http. It is refused for the same reason as a
	// cross-host one: the api_key travels in the query string, and a
	// Location that preserves the query — the usual case for an
	// apex-to-www or path-rewrite redirect — would put the user's
	// credential on the wire in cleartext. The reverse direction, http to
	// https, is followed.
	ErrInsecureRedirect = errors.New("refusing to follow a redirect from https to http")

	// ErrTooManyRedirects reports a redirect chain longer than the limit.
	ErrTooManyRedirects = errors.New("too many redirects")
)

// Credentials are the values the user supplied from their own account for
// one source (AGENT.md §2). Nothing in tortui obtains them any other way.
//
// The field names are chosen for internal/logging, not for prose: that
// package masks by key name, and a struct walked through slog.Any has its
// field names checked with the same rule as a top-level attribute key. Both
// names below contain a substring on that list ("apikey", "cookie"), so a
// Credentials value that reaches a log call is redacted field by field even
// if the attribute it arrived under was named something innocuous. Renaming
// either field to something like Value, Auth, or Token-free wording would
// silently reopen that hole. LogValue below is the second layer.
type Credentials struct {
	// APIKey is injected as a query parameter (see Config.APIKeyParam).
	APIKey string

	// CookieHeader is the raw value of the Cookie request header, exactly
	// as the user copied it out of their own logged-in browser session.
	CookieHeader string
}

// LogValue implements slog.LogValuer so a Credentials value logged directly
// — as an attribute, or nested inside one — renders as a fixed string
// instead of its fields. internal/logging resolves LogValuer before it does
// anything else, so this holds regardless of the key it was logged under.
func (c Credentials) LogValue() slog.Value {
	return slog.StringValue("[credentials redacted]")
}

// Config configures a Client. Every field is optional: the zero Config
// produces a client with the documented defaults above, and any field left
// zero or negative is replaced by its default rather than rejected.
type Config struct {
	// UserAgent is sent on every request unless the caller sets its own
	// User-Agent header on a Request.
	UserAgent string

	// ConnectTimeout bounds connection establishment and the TLS
	// handshake.
	ConnectTimeout time.Duration

	// ReadTimeout bounds waiting for a response once the request is sent.
	// Together with ConnectTimeout it also bounds one attempt end to end,
	// body read included.
	ReadTimeout time.Duration

	// RequestTimeout bounds one Do call including every retry and wait. It
	// is applied on top of the caller's context, never instead of it.
	RequestTimeout time.Duration

	// MinHostInterval is the minimum spacing between requests to the same
	// host. Negative disables spacing; zero uses DefaultMinHostInterval.
	MinHostInterval time.Duration

	// MaxAttempts is the total number of attempts per Do call. 1 disables
	// retrying.
	MaxAttempts int

	// BaseBackoff and MaxBackoff bound the exponential backoff between
	// attempts.
	BaseBackoff time.Duration
	MaxBackoff  time.Duration

	// MaxRetryAfter is the longest server-requested Retry-After delay that
	// is honoured by waiting. A longer one ends the attempt loop and is
	// reported on the StatusError, because sleeping for an hour inside a
	// search is not a retry and ignoring the server's answer is exactly the
	// hammering AGENT.md §6.13 forbids.
	MaxRetryAfter time.Duration

	// MaxBodyBytes caps how much of a response body is read. Negative
	// disables the cap; zero uses DefaultMaxBodyBytes.
	MaxBodyBytes int64

	// APIKeyParam is the query parameter Credentials.APIKey is injected as.
	APIKeyParam string

	// Credentials are the user-supplied values injected into every request
	// this client makes. The zero value injects nothing.
	Credentials Credentials

	// Transport overrides the HTTP transport. When nil, one is built from
	// ConnectTimeout and ReadTimeout.
	Transport http.RoundTripper

	// Clock overrides the time source used for backoff and rate limiting.
	// When nil, real time is used.
	Clock Clock

	// Jitter, when set, is applied to each computed backoff delay before
	// waiting. It is injectable rather than built in so tests stay
	// deterministic; nothing in tortui sets it today, because a
	// single-user client contacting one source has no thundering herd to
	// spread out.
	Jitter func(time.Duration) time.Duration

	// Logger overrides the logger used for the client's debug lines. When
	// nil, slog's default (the masking file sink installed by
	// internal/logging) is resolved at call time.
	Logger *slog.Logger
}

// Client is a shared HTTP client for indexer adapters. It is safe for
// concurrent use.
type Client struct {
	http           *http.Client
	limiter        *hostLimiter
	clock          Clock
	redactor       *redactor
	logger         *slog.Logger
	jitter         func(time.Duration) time.Duration
	creds          Credentials
	userAgent      string
	apiKeyParam    string
	connectTimeout time.Duration
	readTimeout    time.Duration
	requestTimeout time.Duration
	baseBackoff    time.Duration
	maxBackoff     time.Duration
	maxRetryAfter  time.Duration
	maxBodyBytes   int64
	maxAttempts    int
}

// New returns a Client built from cfg, applying the documented default for
// every field left at its zero value.
func New(cfg Config) *Client {
	if cfg.UserAgent == "" {
		cfg.UserAgent = DefaultUserAgent
	}

	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = DefaultConnectTimeout
	}

	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = DefaultReadTimeout
	}

	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = DefaultRequestTimeout
	}

	if cfg.MinHostInterval == 0 {
		cfg.MinHostInterval = DefaultMinHostInterval
	}

	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = DefaultMaxAttempts
	}

	if cfg.BaseBackoff <= 0 {
		cfg.BaseBackoff = DefaultBaseBackoff
	}

	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = DefaultMaxBackoff
	}

	if cfg.MaxBackoff < cfg.BaseBackoff {
		cfg.MaxBackoff = cfg.BaseBackoff
	}

	if cfg.MaxRetryAfter <= 0 {
		cfg.MaxRetryAfter = DefaultMaxRetryAfter
	}

	if cfg.MaxBodyBytes == 0 {
		cfg.MaxBodyBytes = DefaultMaxBodyBytes
	}

	if cfg.APIKeyParam == "" {
		cfg.APIKeyParam = DefaultAPIKeyParam
	}

	if cfg.Clock == nil {
		cfg.Clock = systemClock{}
	}

	transport := cfg.Transport
	if transport == nil {
		transport = newTransport(cfg.ConnectTimeout, cfg.ReadTimeout)
	}

	return &Client{
		http: &http.Client{
			Transport:     transport,
			CheckRedirect: checkRedirect,
		},
		limiter:        newHostLimiter(cfg.MinHostInterval, cfg.Clock),
		clock:          cfg.Clock,
		redactor:       newRedactor(cfg.Credentials.APIKey, cfg.Credentials.CookieHeader),
		logger:         cfg.Logger,
		jitter:         cfg.Jitter,
		creds:          cfg.Credentials,
		userAgent:      cfg.UserAgent,
		apiKeyParam:    cfg.APIKeyParam,
		connectTimeout: cfg.ConnectTimeout,
		readTimeout:    cfg.ReadTimeout,
		requestTimeout: cfg.RequestTimeout,
		baseBackoff:    cfg.BaseBackoff,
		maxBackoff:     cfg.MaxBackoff,
		maxRetryAfter:  cfg.MaxRetryAfter,
		maxBodyBytes:   cfg.MaxBodyBytes,
		maxAttempts:    cfg.MaxAttempts,
	}
}

// newTransport builds the default transport: connect and TLS handshake
// bounded by connect, waiting for response headers bounded by read.
func newTransport(connect, read time.Duration) *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           newDialer(connect).DialContext,
		TLSHandshakeTimeout:   connect,
		ResponseHeaderTimeout: read,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   2,
		ForceAttemptHTTP2:     true,
	}
}

// newDialer returns the dialer the default transport connects with.
// ConnectTimeout lands here, on net.Dialer.Timeout, which bounds
// establishing the TCP connection itself. It is a separate constructor so a
// test can assert the timeout actually reached the dialer: verifying it
// behaviourally would need a host that swallows SYN packets, and unit tests
// make zero network calls (AGENT.md §6.7).
func newDialer(connect time.Duration) *net.Dialer {
	return &net.Dialer{
		Timeout:   connect,
		KeepAlive: 30 * time.Second,
	}
}

// checkRedirect refuses to leave the host the request was addressed to,
// refuses to drop from https to http on the way, and bounds the chain
// length. No error names a URL — only hosts and schemes.
//
// The scheme matters as much as the host here. The api_key travels in the
// query string, and a Location that preserves the query is the common case,
// so following https -> http on the same host puts the credential on the
// wire in cleartext — the same harm the cross-host check exists to prevent.
// http -> https is the opposite: the destination is the host the user
// configured and the hop only adds TLS, so it is followed rather than
// broken (an apex http URL upgraded by the server is an ordinary,
// widespread redirect). See DEC-062.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("httpx: %w (%d hops)", ErrTooManyRedirects, len(via))
	}

	origin := via[0].URL
	if !strings.EqualFold(req.URL.Host, origin.Host) {
		return fmt.Errorf("httpx: %w (%s to %s)", ErrCrossHostRedirect, hostOf(origin), hostOf(req.URL))
	}

	if isSchemeDowngrade(origin.Scheme, req.URL.Scheme) {
		return fmt.Errorf("httpx: %w (at %s)", ErrInsecureRedirect, hostOf(req.URL))
	}

	return nil
}

// isSchemeDowngrade reports whether moving from one scheme to the other
// loses transport security. Only https -> http does; http -> https and any
// same-scheme hop do not.
func isSchemeDowngrade(from, to string) bool {
	return strings.EqualFold(from, "https") && !strings.EqualFold(to, "https")
}

// Request is one outbound HTTP request.
type Request struct {
	// Method defaults to GET when empty.
	Method string

	// URL is the absolute http or https URL to request. Any query it
	// already carries is preserved.
	URL string

	// Query holds extra query parameters appended to the URL's own.
	Query url.Values

	// Header holds extra request headers. A User-Agent set here wins over
	// the client's configured one; a Cookie set here is replaced by the
	// user's configured credential when one exists.
	Header http.Header
}

// Response is a successful (2xx) HTTP response with its body already read
// and size-capped.
type Response struct {
	// StatusCode is the 2xx status the server returned.
	StatusCode int

	// Header is the response header map.
	Header http.Header

	// Body is the complete response body. It is never truncated: a body
	// over the cap is an error, not a short read.
	Body []byte
}

// StatusError reports an HTTP response tortui cannot use — a non-2xx status
// that either was not retryable or survived every retry.
//
// It names the method and the host and nothing else. There is no URL, no
// header, and no body excerpt on purpose: a URL carries the user's api_key
// in its query string and a body can echo a credential straight back.
type StatusError struct {
	// Method is the HTTP method that was sent.
	Method string

	// Host is the host:port the request went to.
	Host string

	// StatusCode is the status the server returned.
	StatusCode int

	// RetryAfter is the delay the server asked for via Retry-After.
	// Meaningful only when RetryAfterSet is true.
	RetryAfter time.Duration

	// RetryAfterSet reports whether the server sent a parseable
	// Retry-After. It distinguishes "the server said come back now" (zero
	// duration, set) from "the server said nothing" (zero duration, not
	// set).
	RetryAfterSet bool

	// Attempts is how many attempts were made before giving up.
	Attempts int
}

// Error describes the failure without naming a URL.
func (e *StatusError) Error() string {
	var b strings.Builder

	fmt.Fprintf(&b, "httpx: %s %s: HTTP %d", e.Method, e.Host, e.StatusCode)

	if text := http.StatusText(e.StatusCode); text != "" {
		fmt.Fprintf(&b, " %s", text)
	}

	if e.Attempts == 1 {
		b.WriteString(" (1 attempt")
	} else {
		fmt.Fprintf(&b, " (%d attempts", e.Attempts)
	}

	if e.RetryAfterSet {
		fmt.Fprintf(&b, ", server asked to retry after %s", e.RetryAfter)
	}

	b.WriteString(")")

	return b.String()
}

// Retryable reports whether this status is one the client retries: 429 or
// any 5xx. Every other 4xx is permanent, 408 included (DEC-058).
func (e *StatusError) Retryable() bool { return isRetryableStatus(e.StatusCode) }

// AuthFailed reports whether the status means the request was rejected over
// credentials — 401 Unauthorized or 403 Forbidden — rather than being
// unreachable or erroring for some other reason. It implements the
// duck-typed shape internal/tui's settings screen matches via errors.As to
// classify a connection-test outcome (T-081) without importing this
// package's concrete type directly (AGENT.md §4).
func (e *StatusError) AuthFailed() bool {
	return e.StatusCode == http.StatusUnauthorized || e.StatusCode == http.StatusForbidden
}

// Get issues a GET request for rawURL with the given extra query
// parameters, which may be nil.
func (c *Client) Get(ctx context.Context, rawURL string, query url.Values) (*Response, error) {
	return c.Do(ctx, Request{Method: http.MethodGet, URL: rawURL, Query: query})
}

// Do issues req, retrying a 429 or 5xx response with exponential backoff
// (honouring Retry-After) up to the configured attempt limit, and returns
// the 2xx response with its body read.
//
// ctx bounds the whole call, retries and waits included: the client applies
// its own RequestTimeout on top of whatever deadline ctx already carries, so
// a call with no deadline still has one (AGENT.md §6.2). Cancelling ctx
// abandons a pending backoff or rate-limiter wait immediately.
//
// A non-2xx response returns a *StatusError and no Response. Any error
// returned is safe to log and to show: it names the method, the host, and a
// cause, and never a URL, a header, or a body.
func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}

	method = strings.ToUpper(method)

	target, err := c.resolveURL(req.URL, req.Query)
	if err != nil {
		return nil, err
	}

	host := hostOf(target)

	ctx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()

	for attempt := 1; ; attempt++ {
		if err := c.limiter.wait(ctx, host); err != nil {
			return nil, c.redactor.safef(err, "httpx: %s %s: %v waiting for this host's rate-limit slot", method, host, err)
		}

		res, statusErr, err := c.attempt(ctx, method, target, req.Header)
		if err != nil {
			return nil, err
		}

		if statusErr == nil {
			return res, nil
		}

		statusErr.Attempts = attempt

		if attempt >= c.maxAttempts || !statusErr.Retryable() {
			return nil, statusErr
		}

		delay, ok := c.retryDelay(attempt, statusErr)
		if !ok {
			return nil, fmt.Errorf(
				"httpx: not retrying, the server asked to wait %s which is longer than the %s limit: %w",
				statusErr.RetryAfter, c.maxRetryAfter, statusErr,
			)
		}

		if !c.fitsDeadline(ctx, delay) {
			return nil, fmt.Errorf(
				"httpx: not retrying, a %s wait would outlast the request deadline: %w",
				delay, statusErr,
			)
		}

		c.log().Debug("httpx: retrying request",
			"method", method,
			"host", host,
			"status", statusErr.StatusCode,
			"attempt", attempt,
			"delay", delay,
		)

		if err := c.clock.Sleep(ctx, delay); err != nil {
			return nil, c.redactor.safef(err, "httpx: %s %s: %v while waiting to retry after HTTP %d", method, host, err, statusErr.StatusCode)
		}
	}
}

// attempt performs exactly one request. It returns a Response on 2xx, a
// *StatusError on any other status, or an error for a transport or body
// failure. Exactly one of the three is ever non-nil.
func (c *Client) attempt(ctx context.Context, method string, target *url.URL, header http.Header) (*Response, *StatusError, error) {
	host := hostOf(target)

	attemptCtx, cancel := context.WithTimeout(ctx, c.connectTimeout+c.readTimeout)
	defer cancel()

	hreq, err := http.NewRequestWithContext(attemptCtx, method, target.String(), nil)
	if err != nil {
		return nil, nil, c.redactor.safef(unwrapURLError(err), "httpx: %s %s: cannot build request: %v", method, host, unwrapURLError(err))
	}

	c.applyHeaders(hreq, header)

	resp, err := c.http.Do(hreq)
	if resp != nil {
		defer c.closeBody(resp)
	}

	if err != nil {
		cause := unwrapURLError(err)

		return nil, nil, c.redactor.safef(cause, "httpx: %s %s: %v", method, host, cause)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		retryAfter, ok := parseRetryAfter(resp.Header.Get("Retry-After"), c.clock.Now())
		c.drain(resp)

		return nil, &StatusError{
			Method:        method,
			Host:          host,
			StatusCode:    resp.StatusCode,
			RetryAfter:    retryAfter,
			RetryAfterSet: ok,
		}, nil
	}

	body, err := c.readBody(resp, method, host)
	if err != nil {
		return nil, nil, err
	}

	return &Response{StatusCode: resp.StatusCode, Header: resp.Header, Body: body}, nil, nil
}

// applyHeaders sets the caller's headers, then the user-agent (unless the
// caller set one), then the user-supplied session credential.
func (c *Client) applyHeaders(hreq *http.Request, header http.Header) {
	for key, values := range header {
		for _, value := range values {
			hreq.Header.Add(key, value)
		}
	}

	if hreq.Header.Get("User-Agent") == "" {
		hreq.Header.Set("User-Agent", c.userAgent)
	}

	if c.creds.CookieHeader != "" {
		hreq.Header.Set(headerCookie, c.creds.CookieHeader)
	}
}

// readBody reads the response body under the configured cap. A body over
// the cap is an error and the partial read is discarded — truncating an XML
// feed silently would look like a short result set rather than a failure.
func (c *Client) readBody(resp *http.Response, method, host string) ([]byte, error) {
	if c.maxBodyBytes < 0 {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, c.redactor.safef(err, "httpx: %s %s: reading response body: %v", method, host, err)
		}

		return body, nil
	}

	// A declared Content-Length over the cap is refused before anything is
	// read. The limited read below is what actually enforces the cap, since
	// a chunked response declares nothing and a server is free to lie.
	if resp.ContentLength > c.maxBodyBytes {
		return nil, fmt.Errorf("httpx: %s %s: %w (declared %d bytes, cap %d)",
			method, host, ErrBodyTooLarge, resp.ContentLength, c.maxBodyBytes)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBodyBytes+1))
	if err != nil {
		return nil, c.redactor.safef(err, "httpx: %s %s: reading response body: %v", method, host, err)
	}

	if int64(len(body)) > c.maxBodyBytes {
		return nil, fmt.Errorf("httpx: %s %s: %w (cap %d bytes)", method, host, ErrBodyTooLarge, c.maxBodyBytes)
	}

	return body, nil
}

// drain reads a bounded amount of an unwanted body so the connection can be
// reused. A failure here costs a pooled connection and nothing else, so it
// is logged rather than returned — but it is not discarded into `_`
// (AGENT.md §6.9).
func (c *Client) drain(resp *http.Response) {
	if _, err := io.Copy(io.Discard, io.LimitReader(resp.Body, drainLimit)); err != nil {
		c.log().Debug("httpx: discarding an unused response body failed", "error", err.Error())
	}
}

// closeBody closes a response body, logging rather than returning a close
// failure for the same reason as drain.
func (c *Client) closeBody(resp *http.Response) {
	if err := resp.Body.Close(); err != nil {
		c.log().Debug("httpx: closing a response body failed", "error", err.Error())
	}
}

// resolveURL parses rawURL, merges extra query parameters into it, and
// injects the user's api_key when one is configured.
func (c *Client) resolveURL(rawURL string, extra url.Values) (*url.URL, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return nil, fmt.Errorf("httpx: %w", ErrURLEmpty)
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		// url.Parse returns a *url.Error whose text repeats the whole URL;
		// only the reason underneath it is safe to show.
		cause := unwrapURLError(err)

		return nil, c.redactor.safef(ErrURLInvalid, "httpx: %v: %v", ErrURLInvalid, cause)
	}

	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return nil, fmt.Errorf("httpx: %w (got %q)", ErrURLSchemeUnsupported, parsed.Scheme)
	}

	if parsed.Host == "" {
		return nil, fmt.Errorf("httpx: %w", ErrURLHostMissing)
	}

	query := parsed.Query()

	for key, values := range extra {
		for _, value := range values {
			query.Add(key, value)
		}
	}

	if c.creds.APIKey != "" {
		query.Set(c.apiKeyParam, c.creds.APIKey)
	}

	target := *parsed
	target.RawQuery = query.Encode()

	return &target, nil
}

// retryDelay returns how long to wait before attempt n+1, and whether
// waiting is acceptable at all. A parseable Retry-After always wins over the
// backoff schedule — including a zero one, which means "now". A Retry-After
// longer than MaxRetryAfter returns false: the caller stops rather than
// either sleeping for it or ignoring it.
//
// A negative RetryAfter is clamped to zero rather than handed to Sleep.
// parseRetryAfter cannot produce one — it saturates instead of wrapping —
// but StatusError is exported and a negative delay must never silently
// become "no wait at all", which is what Clock.Sleep does with one.
func (c *Client) retryDelay(n int, statusErr *StatusError) (time.Duration, bool) {
	if statusErr.RetryAfterSet {
		if statusErr.RetryAfter > c.maxRetryAfter {
			return 0, false
		}

		return max(statusErr.RetryAfter, 0), true
	}

	delay := backoffFor(n, c.baseBackoff, c.maxBackoff)
	if c.jitter != nil {
		delay = c.jitter(delay)
	}

	return delay, true
}

// fitsDeadline reports whether waiting delay would still leave the context
// alive. Sleeping past the deadline only to be cancelled wastes the time the
// caller had left and reports the timeout instead of the status that caused
// it.
func (c *Client) fitsDeadline(ctx context.Context, delay time.Duration) bool {
	deadline, ok := ctx.Deadline()
	if !ok {
		return true
	}

	return !c.clock.Now().Add(delay).After(deadline)
}

// log resolves the logger at call time so a logger installed after this
// client was built (internal/logging.New sets slog's default) is still the
// one used.
func (c *Client) log() *slog.Logger {
	if c.logger != nil {
		return c.logger
	}

	return slog.Default()
}
