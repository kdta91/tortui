package torznab

import (
	"errors"
	"fmt"
)

// Errors this package reports. Callers match them with errors.Is.
//
// The URL-shaped sentinels are named prefix-first (ErrEndpointEmpty rather
// than ErrEmptyEndpoint) for the same reason httpx names its own that way:
// scripts/check-indexer-hostnames.sh reads any `...endpoint =` or `...url =`
// line inside internal/indexer as a hostname assignment and flags it. See
// backlog T-926.
var (
	// ErrIDEmpty reports Options with no id. The id is the registry key,
	// the config key, and Result.IndexerID, so there is no useful
	// behaviour without one.
	ErrIDEmpty = errors.New("indexer id is empty")

	// ErrEndpointEmpty reports Options with no api endpoint.
	ErrEndpointEmpty = errors.New("torznab api endpoint is empty")

	// ErrEndpointInvalid reports an api endpoint that will not parse.
	ErrEndpointInvalid = errors.New("torznab api endpoint is not usable")

	// ErrEndpointSchemeUnsupported reports an api endpoint that is not
	// http or https.
	ErrEndpointSchemeUnsupported = errors.New("torznab api endpoint scheme must be http or https")

	// ErrEndpointHostMissing reports an api endpoint with no host, which
	// is what a relative path or a bare path parses as.
	ErrEndpointHostMissing = errors.New("torznab api endpoint has no host")

	// ErrDocumentEmpty reports a response body with no XML element in it
	// at all: an empty body, whitespace, comments or an XML declaration
	// alone, or plain text that was never markup — a bare "503 Service
	// Unavailable" from a proxy in front of the source being the usual
	// way it happens.
	ErrDocumentEmpty = errors.New("the source returned no XML document")

	// ErrDocumentMalformed reports a response this package could not parse
	// as XML: truncated, syntactically invalid, or using an entity the
	// document never declared.
	ErrDocumentMalformed = errors.New("the source returned a document that is not well-formed XML")

	// ErrDocumentUnexpectedRoot reports a well-formed document whose root
	// element is not the one the request asked for — an HTML sign-in page
	// or error page in place of a feed being the usual case.
	ErrDocumentUnexpectedRoot = errors.New("the source returned a document that is not a torznab response")

	// ErrCapsUnavailable reports that a caps probe did not produce a usable
	// caps document. Discover returns it alongside a working adapter whose
	// Caps are the fail-closed baseline; it is not a fatal condition.
	ErrCapsUnavailable = errors.New("the source published no usable caps document")

	// ErrQueryTextEmpty reports a ModeSearch query with no keyword. An
	// adapter must not quietly turn one into a browse request
	// (indexer.Query's own contract), so it is refused instead.
	ErrQueryTextEmpty = errors.New("a keyword search needs a keyword")

	// ErrModeUnsupported reports a Query.Mode this adapter does not
	// implement, which today means a value that is neither ModeSearch nor
	// ModeLatest.
	ErrModeUnsupported = errors.New("query mode is not supported")

	// ErrLatestUnsupported reports a ModeLatest query sent to a source
	// whose caps probe did not show a working recent-additions feed. The
	// registry skips such a source rather than asking it (AGENT.md §6.3);
	// this is the adapter's own backstop for a caller that does not.
	ErrLatestUnsupported = errors.New("this source has no recent-additions feed")

	// ErrUnresolvable reports a Result that Resolve cannot complete: no
	// magnet, no usable infohash, and no torrent URL either, so there is
	// nothing to hand the engine and nothing to derive one from.
	ErrUnresolvable = errors.New("result has no magnet, no infohash, and no torrent URL")
)

// APIError is the Newznab/Torznab error document — a response whose root
// element is <error> rather than a feed — served, as those servers do, with
// HTTP 200.
//
// It carries the numeric code and nothing else. The document's own
// description attribute is deliberately discarded and never stored: it is
// free text under the source's control, several implementations echo the
// offending request back inside it, and every request this adapter makes
// carries the user's api_key in its query string. Keeping the description
// would put that key one log line away, which is precisely the leak
// internal/logging cannot catch (AGENT.md §2; DEC-061, DEC-066).
type APIError struct {
	// Code is the code attribute of the error document, or zero when the
	// document published none this package could read.
	Code int
}

// Error describes the failure by code and code family. No text from the
// document appears in it.
func (e *APIError) Error() string {
	return fmt.Sprintf("the source answered with torznab api error %d (%s)", e.Code, apiErrorFamily(e.Code))
}

// IsAuth reports whether the code is in the 1xx family, which the newznab
// API reserves for credential and account problems. It is what the caps
// probe reads to conclude that a source needs the user's own credentials
// (Caps.RequiresAuth).
//
// It is a display and capability signal only. Nothing in tortui works
// around a source's access controls (AGENT.md §2).
func (e *APIError) IsAuth() bool { return e.Code >= 100 && e.Code <= 199 }

// AuthFailed is IsAuth under the name internal/tui's settings screen
// matches via errors.As (T-081): the duck-typed shape that lets a
// connection-test probe classify this as "auth failed" without importing
// this package's concrete type directly (AGENT.md §4).
func (e *APIError) AuthFailed() bool { return e.IsAuth() }

// apiErrorFamily names the code's family. Only the family is reported,
// never a per-code message: the exact code-to-text table varies between
// implementations, and a family is what a user can act on anyway.
func apiErrorFamily(code int) string {
	switch {
	case code >= 100 && code <= 199:
		return "credentials or account"
	case code >= 200 && code <= 299:
		return "request"
	case code >= 300 && code <= 399:
		return "no such item"
	case code >= 500 && code <= 599:
		return "a limit on the account was reached"
	case code == 900:
		// The only 9xx code the newznab API specification defines
		// (docs/newznab_api_specification.txt §5, nZEDb/nZEDb, branch
		// dev). Jackett sends it for any unhandled server-side failure.
		return "the server reported an unknown error"
	default:
		return "unrecognised code"
	}
}
