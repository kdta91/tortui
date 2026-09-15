package scraper

import (
	"errors"
	"fmt"
	"strings"
)

// Errors this package reports. Callers match them with errors.Is.
//
// The URL-shaped sentinels are named prefix-first (ErrBaseAddressEmpty
// rather than ErrEmptyBaseURL) for the same reason httpx and torznab name
// theirs that way: scripts/check-indexer-hostnames.sh reads any
// `...url =`, `...host =` or `...base_url =` line inside internal/indexer
// as a hostname assignment and flags it. See backlog T-926.
var (
	// ErrIDEmpty reports a definition with no id. The id is the registry
	// key, the config key, and Result.IndexerID, so there is no useful
	// behaviour without one.
	ErrIDEmpty = errors.New("definition id is empty")

	// ErrBaseAddressEmpty reports a definition with no base_url.
	ErrBaseAddressEmpty = errors.New("definition base_url is empty")

	// ErrBaseAddressInvalid reports a base_url that will not parse. The
	// value is never repeated back: a user is free to write a query
	// string into it and that query string is free to hold their own
	// api key (DEC-073).
	ErrBaseAddressInvalid = errors.New("definition base_url is not usable")

	// ErrBaseAddressSchemeUnsupported reports a base_url that is not http
	// or https.
	ErrBaseAddressSchemeUnsupported = errors.New("definition base_url scheme must be http or https")

	// ErrBaseAddressHostMissing reports a base_url with no host, which is
	// what a bare path parses as.
	ErrBaseAddressHostMissing = errors.New("definition base_url has no host")

	// ErrDefinitionEmpty reports a definition file with no YAML document
	// in it at all — empty, whitespace, or comments alone.
	ErrDefinitionEmpty = errors.New("the definition file contains no YAML document")

	// ErrDefinitionMalformed reports a definition this package could not
	// decode as YAML in the schema's shape: a syntax error, a value of
	// the wrong type, a duplicate key, or a key the schema does not
	// define. It is reported by line and column and never by content
	// (DEC-073).
	ErrDefinitionMalformed = errors.New("the definition is not valid YAML for this schema")

	// ErrSearchBlockMissing reports a definition with no search block. A
	// source that cannot be searched has nothing to offer, so this is a
	// validation failure rather than Caps.Search = false.
	ErrSearchBlockMissing = errors.New("the definition has no search block")

	// ErrRowsSelectorMissing reports a block with no rows selector, and no
	// top-level one to inherit.
	ErrRowsSelectorMissing = errors.New("no rows selector is defined for this block")

	// ErrTitleFieldMissing reports a block whose effective field set has
	// no title. Every other field is optional; a result with no title is
	// a blank row in the results table.
	ErrTitleFieldMissing = errors.New("no title field is defined for this block")

	// ErrLinkFieldMissing reports a block whose effective field set maps
	// none of magnet, torrent_url or infohash. Such a definition can only
	// ever produce results that fail indexer.Result.Validate, so it is
	// refused at validation rather than at download time.
	ErrLinkFieldMissing = errors.New("no magnet, torrent_url or infohash field is defined for this block")

	// ErrFieldUnknown reports a field name that is not one of the Result
	// fields this schema maps.
	ErrFieldUnknown = errors.New("not a field this schema maps")

	// ErrSelectorInvalid reports a CSS selector cascadia could not
	// compile.
	ErrSelectorInvalid = errors.New("not a valid CSS selector")

	// ErrPathInvalid reports a JSON path expression this package could
	// not read.
	ErrPathInvalid = errors.New("not a valid JSON path expression")

	// ErrRegexInvalid reports a regex that will not compile.
	ErrRegexInvalid = errors.New("not a valid regular expression")

	// ErrTransformUnknown reports a transform name that is not one of the
	// documented ones.
	ErrTransformUnknown = errors.New("not a transform this schema defines")

	// ErrTransformChainTooLong reports a transform chain longer than
	// maxTransforms.
	ErrTransformChainTooLong = errors.New("the transform chain is longer than the limit")

	// ErrAttrAndText reports a field that asks for both an attribute and
	// the element's text. Only one of them can be the value.
	ErrAttrAndText = errors.New("a field takes either attr or text, not both")

	// ErrAttrInJSONMode reports an attr on a field in a JSON definition.
	// A JSON value has no attributes, so an attr there would silently do
	// nothing.
	ErrAttrInJSONMode = errors.New("attr is meaningless in a json definition")

	// ErrLayoutsOutsidePublished reports layouts on a field other than
	// published, where they would silently do nothing.
	ErrLayoutsOutsidePublished = errors.New("layouts apply to the published field only")

	// ErrTrustLevelUnknown reports a trust mapping whose target is not one
	// of the indexer.Trust tokens.
	ErrTrustLevelUnknown = errors.New("not a trust level")

	// ErrTrustValuesMissing reports a trust block with no values mapping,
	// which could never resolve to a level.
	ErrTrustValuesMissing = errors.New("the trust block has no values mapping")

	// ErrModeUnknown reports a definition mode that is neither html nor
	// json.
	ErrModeUnknown = errors.New("not a response mode this schema defines")

	// ErrPlaceholderUnknown reports a {{...}} placeholder in a path or a
	// param that this package does not substitute. It is refused rather
	// than passed through so a typo cannot be sent to the source
	// verbatim — and so that a definition cannot name a credential
	// placeholder the schema deliberately does not have (DEC-074).
	ErrPlaceholderUnknown = errors.New("not a placeholder this schema substitutes")

	// ErrPlaceholderUnterminated reports a "{{" with no closing "}}".
	ErrPlaceholderUnterminated = errors.New("a {{placeholder}} is not closed")

	// ErrQueryTextEmpty reports a ModeSearch query with no keyword. An
	// adapter must not quietly turn one into a browse request
	// (indexer.Query's own contract), so it is refused instead.
	ErrQueryTextEmpty = errors.New("a keyword search needs a keyword")

	// ErrModeUnsupported reports a Query.Mode this adapter does not
	// implement, which today means a value that is neither ModeSearch nor
	// ModeLatest.
	ErrModeUnsupported = errors.New("query mode is not supported")

	// ErrLatestUnsupported reports a ModeLatest query sent to a
	// definition with no latest block. The registry skips such a source
	// rather than asking it (AGENT.md §6.3); this is the adapter's own
	// backstop for a caller that does not.
	ErrLatestUnsupported = errors.New("this source has no recent-additions feed")

	// ErrDocumentTooDeep reports an HTML response whose element nesting
	// is deeper than maxHTMLDepth. See guardHTMLDepth: parsing such a
	// document costs time quadratic in its depth, and the document is
	// written by the source.
	ErrDocumentTooDeep = errors.New("the source returned an HTML document nested deeper than the limit")

	// ErrDocumentMalformed reports a response body this package could not
	// parse in the definition's mode. It is reported by byte offset and
	// never by content (DEC-073).
	ErrDocumentMalformed = errors.New("the source returned a document this definition cannot parse")

	// ErrRowsNotAList reports a JSON response whose rows path does not
	// lead to an array.
	ErrRowsNotAList = errors.New("the rows path does not lead to a JSON array")

	// ErrUnresolvable reports a Result that Resolve cannot complete: no
	// magnet, no usable infohash, and no torrent URL either, so there is
	// nothing to hand the engine and nothing to derive one from.
	ErrUnresolvable = errors.New("result has no magnet, no infohash, and no torrent URL")
)

// ValidationError is one problem with a definition, named precisely enough
// for the user to go and fix it: the dotted location of the failing key in
// the definition, and the selector at that location when the failure is
// about one.
//
// What it carries, exactly: a *key* the user wrote — a field name, a param
// key, a trust mapping key — in Location, plus a selector, a regex, a
// transform name or a mode. What it never carries is a param *value*, a
// path, or the base_url. Those three are where a user plausibly writes
// their own api key (a hardcoded token in a param, a query string on the
// base address), internal/logging masks by key name and by value shape,
// and an error string is neither a masked key nor a recognisable shape
// (DEC-073). A selector, a regex and a transform name are echoed because
// the acceptance criterion for this task asks for exactly that and because
// none of them is a place a credential goes.
type ValidationError struct {
	// Location is the dotted path of the failing key inside the
	// definition, as the user wrote it: "search.fields.title.selector",
	// "trust.values.gold", "latest.params.page".
	Location string

	// Selector is the CSS selector or JSON path the failure is about, or
	// empty when the failure is not about one.
	Selector string

	// Err is the underlying reason, one of the sentinels above.
	Err error
}

// Error names the location, the selector when there is one, and the reason.
func (e *ValidationError) Error() string {
	var b strings.Builder

	fmt.Fprintf(&b, "definition %s", e.Location)

	if e.Selector != "" {
		fmt.Fprintf(&b, ": selector %q", e.Selector)
	}

	fmt.Fprintf(&b, ": %v", e.Err)

	return b.String()
}

// Unwrap exposes the sentinel so callers can match it with errors.Is.
func (e *ValidationError) Unwrap() error { return e.Err }

// invalid builds a ValidationError for a location with no selector.
func invalid(location string, err error) error {
	return &ValidationError{Location: location, Err: err}
}

// invalidSelector builds a ValidationError naming the selector that failed.
func invalidSelector(location, selector string, err error) error {
	return &ValidationError{Location: location, Selector: selector, Err: err}
}
