package scraper

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/andybalholm/cascadia"

	"github.com/kdta91/tortui/internal/indexer"
)

// plan is a validated, compiled definition: every selector compiled, every
// regex compiled, every transform resolved to a function, every inherited
// value already inherited. An Adapter holds one and never looks at the
// Definition again, so nothing a caller does to the Definition afterwards
// can change how requests are made or how a page is read.
type plan struct {
	base         *url.URL
	search       *blockPlan
	latest       *blockPlan
	id           string
	name         string
	mode         string
	requiresAuth bool
}

// blockPlan is one compiled request: where it goes and how its rows are
// read.
type blockPlan struct {
	params     map[string]string
	fields     map[string]*fieldPlan
	trust      *trustPlan
	path       string
	rows       matcher
	pagination bool
}

// fieldPlan is one compiled field selector.
type fieldPlan struct {
	regex      *regexp.Regexp
	attr       string
	sel        matcher
	transforms []transform
	layouts    []string
}

// trustPlan is a compiled trust mapping.
type trustPlan struct {
	field  *fieldPlan
	values map[string]indexer.Trust
}

// matcher is a compiled selector in whichever language the definition's
// mode speaks: a CSS selector for html, a path expression for json. Only
// the field matching that mode is ever populated.
type matcher struct {
	css  cascadia.Selector
	path []string
	raw  string
}

// empty reports whether the selector selects the row itself.
func (m matcher) empty() bool { return m.raw == "" }

// transform is one named, pure, single-pass string transform.
type transform func(string) string

// transforms are the transform names a definition may use.
//
// Every one is pure, single-pass, and takes no argument. That is what makes
// a transform chain a list rather than a language: there is nothing to
// recurse into, nothing that can re-enter the chain, and no way to express
// a cycle — the chain is a bounded slice walked once per field per row.
var transforms = map[string]transform{
	"trim":                strings.TrimSpace,
	"collapse_whitespace": collapseWhitespace,
	"lowercase":           strings.ToLower,
	"uppercase":           strings.ToUpper,
	"digits":              digitsOnly,
	"urldecode":           percentDecode,
}

// transformNames lists the transform names for a validation message, in a
// fixed order so the message is the same on every run.
var transformNames = []string{
	"collapse_whitespace",
	"digits",
	"lowercase",
	"trim",
	"uppercase",
	"urldecode",
}

// trustLevels are the tokens a trust mapping may target. They are
// indexer.Trust.String's own tokens, so a level written down anywhere in
// tortui reads the same.
var trustLevels = map[string]indexer.Trust{
	"unknown":  indexer.TrustUnknown,
	"none":     indexer.TrustNone,
	"verified": indexer.TrustVerified,
	"trusted":  indexer.TrustTrusted,
	"vip":      indexer.TrustVIP,
}

// Placeholders a path or a param template may contain. There is
// deliberately no credential placeholder: an api key or a cookie comes
// from the user's config through httpx, never from the definition
// (DEC-074).
const (
	placeholderQuery  = "query"
	placeholderLimit  = "limit"
	placeholderOffset = "offset"
)

// placeholderNames lists the placeholders for a validation message.
var placeholderNames = []string{placeholderLimit, placeholderOffset, placeholderQuery}

// plan compiles and validates the whole definition.
//
// Checks run in the order a user reads the file — identity, then the shared
// defaults, then each block — and the first failure is returned, so the
// message always points at the outermost thing that is wrong rather than at
// a symptom of it.
func (d *Definition) plan() (*plan, error) {
	id := strings.TrimSpace(d.ID)
	if id == "" {
		return nil, invalid("id", ErrIDEmpty)
	}

	base, err := parseBase(d.BaseURL)
	if err != nil {
		return nil, invalid("base_url", err)
	}

	mode := d.mode()
	if mode != ModeHTML && mode != ModeJSON {
		return nil, invalid("mode", fmt.Errorf("%w (it is %s or %s)", ErrModeUnknown, ModeHTML, ModeJSON))
	}

	if d.Search == nil {
		return nil, invalid("search", ErrSearchBlockMissing)
	}

	out := &plan{
		id:           id,
		name:         d.name(),
		mode:         mode,
		base:         base,
		requiresAuth: d.RequiresAuth,
	}

	if out.search, err = d.blockPlan("search", d.Search, mode); err != nil {
		return nil, err
	}

	if d.Latest != nil {
		if out.latest, err = d.blockPlan("latest", d.Latest, mode); err != nil {
			return nil, err
		}
	}

	return out, nil
}

// parseBase validates the base address.
//
// The value is never repeated back in the error. A base_url is a URL the
// user wrote, it is free to carry a query string, and a query string is
// free to carry their own api key — which is exactly the value
// internal/logging cannot mask once it is inside an error string
// (DEC-073).
func parseBase(raw string) (*url.URL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, ErrBaseAddressEmpty
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return nil, ErrBaseAddressInvalid
	}

	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return nil, ErrBaseAddressSchemeUnsupported
	}

	if parsed.Host == "" {
		return nil, ErrBaseAddressHostMissing
	}

	return parsed, nil
}

// blockPlan compiles one block, resolving everything it inherits first.
func (d *Definition) blockPlan(where string, b *Block, mode string) (*blockPlan, error) {
	rows := d.effectiveRows(b)
	if rows == "" {
		return nil, invalid(where+".rows", ErrRowsSelectorMissing)
	}

	rowMatcher, err := compileMatcher(rows, mode)
	if err != nil {
		return nil, invalidSelector(where+".rows", rows, err)
	}

	fields := d.effectiveFields(b)

	if _, ok := fields[fieldTitle]; !ok {
		return nil, invalid(where+".fields.title", ErrTitleFieldMissing)
	}

	if !hasLink(fields) {
		return nil, invalid(where+".fields", ErrLinkFieldMissing)
	}

	out := &blockPlan{
		path:   strings.TrimSpace(b.Path),
		params: b.Params,
		rows:   rowMatcher,
		fields: make(map[string]*fieldPlan, len(fields)),
	}

	if err := checkTemplate(where+".path", out.path); err != nil {
		return nil, err
	}

	for key, value := range out.params {
		if err := checkTemplate(where+".params."+key, value); err != nil {
			return nil, err
		}
	}

	out.pagination = mentionsOffset(out.path) || mentionsOffsetIn(out.params)

	for _, name := range sortedFieldNames(fields) {
		spec := fields[name]

		if !knownField(name) {
			return nil, invalid(
				where+".fields."+name,
				fmt.Errorf("%w (the fields are %s)", ErrFieldUnknown, strings.Join(fieldNames, ", ")),
			)
		}

		compiled, err := compileField(where+".fields."+name, name, spec, mode)
		if err != nil {
			return nil, err
		}

		out.fields[name] = compiled
	}

	if trust := d.effectiveTrust(b); trust != nil {
		if out.trust, err = compileTrust(where+".trust", trust, mode); err != nil {
			return nil, err
		}
	}

	return out, nil
}

// hasLink reports whether a field set can produce a result the engine could
// act on — directly, or after Resolve derives a magnet from an infohash.
func hasLink(fields map[string]Field) bool {
	for _, name := range []string{fieldMagnet, fieldTorrent, fieldInfoHash} {
		if _, ok := fields[name]; ok {
			return true
		}
	}

	return false
}

// knownField reports whether name is a field this schema maps.
func knownField(name string) bool {
	for _, known := range fieldNames {
		if name == known {
			return true
		}
	}

	return false
}

// sortedFieldNames returns the keys of a field set in the schema's own
// order, with any unknown key last, so validation reports the same failure
// for the same file on every run rather than whichever key Go's map
// iteration reached first.
func sortedFieldNames(fields map[string]Field) []string {
	out := make([]string, 0, len(fields))

	for _, name := range fieldNames {
		if _, ok := fields[name]; ok {
			out = append(out, name)
		}
	}

	unknown := make([]string, 0, len(fields))

	for name := range fields {
		if !knownField(name) {
			unknown = append(unknown, name)
		}
	}

	// A single unknown key is the overwhelmingly common case; sorting
	// keeps even the rare multi-key case deterministic.
	for i := 1; i < len(unknown); i++ {
		for j := i; j > 0 && unknown[j] < unknown[j-1]; j-- {
			unknown[j], unknown[j-1] = unknown[j-1], unknown[j]
		}
	}

	return append(out, unknown...)
}

// compileField compiles one field spec.
func compileField(where, name string, spec Field, mode string) (*fieldPlan, error) {
	selector := strings.TrimSpace(spec.Selector)
	attr := strings.TrimSpace(spec.Attr)

	if attr != "" && spec.Text {
		return nil, invalidSelector(where, selector, ErrAttrAndText)
	}

	if attr != "" && mode == ModeJSON {
		return nil, invalidSelector(where, selector, ErrAttrInJSONMode)
	}

	if len(spec.Layouts) > 0 && name != fieldPublished {
		return nil, invalid(where+".layouts", ErrLayoutsOutsidePublished)
	}

	sel, err := compileMatcher(selector, mode)
	if err != nil {
		return nil, invalidSelector(where+".selector", selector, err)
	}

	out := &fieldPlan{sel: sel, attr: attr, layouts: spec.Layouts}

	if expr := strings.TrimSpace(spec.Regex); expr != "" {
		// RE2: no backreferences, no lookaround, linear time in the
		// length of the input. A definition cannot express a pattern
		// that backtracks catastrophically because the syntax has
		// nothing to backtrack with — regexp.Compile rejects the
		// constructs that would.
		re, err := regexp.Compile(expr)
		if err != nil {
			return nil, invalid(where+".regex", fmt.Errorf("%w: %s", ErrRegexInvalid, expr))
		}

		out.regex = re
	}

	if len(spec.Transform) > maxTransforms {
		return nil, invalid(
			where+".transform",
			fmt.Errorf("%w (%d, limit %d)", ErrTransformChainTooLong, len(spec.Transform), maxTransforms),
		)
	}

	for _, raw := range spec.Transform {
		key := strings.ToLower(strings.TrimSpace(raw))

		fn, ok := transforms[key]
		if !ok {
			return nil, invalid(
				where+".transform",
				fmt.Errorf("%q is %w (they are %s)", raw, ErrTransformUnknown, strings.Join(transformNames, ", ")),
			)
		}

		out.transforms = append(out.transforms, fn)
	}

	return out, nil
}

// compileTrust compiles a trust mapping.
func compileTrust(where string, t *Trust, mode string) (*trustPlan, error) {
	if len(t.Values) == 0 {
		return nil, invalid(where+".values", ErrTrustValuesMissing)
	}

	field, err := compileField(where, fieldTitle, t.Spec, mode)
	if err != nil {
		return nil, err
	}

	out := &trustPlan{field: field, values: make(map[string]indexer.Trust, len(t.Values))}

	for value, level := range t.Values {
		token := strings.ToLower(strings.TrimSpace(level))

		trust, ok := trustLevels[token]
		if !ok {
			return nil, invalid(
				where+".values."+value,
				fmt.Errorf("%q is %w (they are unknown, none, verified, trusted, vip)", level, ErrTrustLevelUnknown),
			)
		}

		out.values[strings.ToLower(strings.TrimSpace(value))] = trust
	}

	return out, nil
}

// compileMatcher compiles a selector in the definition's mode.
func compileMatcher(raw, mode string) (matcher, error) {
	selector := strings.TrimSpace(raw)
	if selector == "" {
		return matcher{}, nil
	}

	if mode == ModeJSON {
		segments, err := compilePath(selector)
		if err != nil {
			return matcher{}, err
		}

		return matcher{raw: selector, path: segments}, nil
	}

	css, err := cascadia.Compile(selector)
	if err != nil {
		// cascadia's message says what is wrong with the syntax and
		// does not quote the selector; the selector is named by the
		// ValidationError around this, which is what the acceptance
		// criterion asks for.
		// The cause is rendered as text rather than wrapped: the caller
		// matches ErrSelectorInvalid, and cascadia's error type is an
		// implementation detail of the CSS parser rather than part of
		// this package's contract.
		return matcher{}, fmt.Errorf("%w (%s)", ErrSelectorInvalid, err.Error())
	}

	return matcher{raw: selector, css: css}, nil
}

// collapseWhitespace replaces every run of whitespace with a single space
// and trims the ends. It is what turns a multi-line table cell into one
// line.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// digitsOnly keeps the ASCII digits and drops everything else, which is
// what "1,234" and "42 seeders" need.
func digitsOnly(s string) string {
	var b strings.Builder

	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}

	return b.String()
}

// percentDecode undoes percent-encoding. A value that is not validly
// encoded is returned unchanged rather than emptied: a transform's job is
// to tidy a value, and a field that a transform could not tidy is still
// better than no field at all.
func percentDecode(s string) string {
	decoded, err := url.QueryUnescape(s)
	if err != nil {
		return s
	}

	return decoded
}
