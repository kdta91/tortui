package scraper

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/kdta91/tortui/internal/indexer"
)

// Template delimiters.
const (
	openDelim  = "{{"
	closeDelim = "}}"
)

// substitution is what one query supplies to a template.
type substitution struct {
	query  string
	limit  string
	offset string
}

// substitutionFor builds the substitution for one query. A numeric value
// the caller left at zero is supplied as empty rather than as "0", so a
// param that exists only to carry it is dropped instead of being sent as
// limit=0 — which several sites read as "no results" rather than as "your
// default".
func substitutionFor(q indexer.Query) substitution {
	out := substitution{query: strings.TrimSpace(q.Text)}

	if q.Limit > 0 {
		out.limit = strconv.Itoa(q.Limit)
	}

	if q.Offset > 0 {
		out.offset = strconv.Itoa(q.Offset)
	}

	return out
}

// value returns the substitution for one placeholder name, and whether the
// name is one this schema defines.
func (s substitution) value(name string) (string, bool) {
	switch name {
	case placeholderQuery:
		return s.query, true
	case placeholderLimit:
		return s.limit, true
	case placeholderOffset:
		return s.offset, true
	default:
		return "", false
	}
}

// checkTemplate validates a path or param template: every placeholder is
// closed and every name is one this schema substitutes.
//
// An unknown placeholder is refused rather than sent through verbatim for
// two reasons. A typo would otherwise be sent to the source as literal
// "{{quary}}" text and look like a site-side failure; and a definition
// must not be able to name a placeholder the schema deliberately does not
// have, which is how "{{apikey}}" stays a validation error instead of a
// credential path through a definition file (DEC-074).
//
// The error names the key. It never names any of the template's text, the
// placeholder's own name included: a param value is one of the three places
// a user may have written their own credential (DEC-073).
func checkTemplate(where, tmpl string) error {
	rest := tmpl

	for {
		start := strings.Index(rest, openDelim)
		if start < 0 {
			return nil
		}

		rest = rest[start+len(openDelim):]

		end := strings.Index(rest, closeDelim)
		if end < 0 {
			return invalid(where, ErrPlaceholderUnterminated)
		}

		name := strings.ToLower(strings.TrimSpace(rest[:end]))

		if _, ok := (substitution{}).value(name); !ok {
			// The placeholder's own name is not echoed. It is text out
			// of a param value, and a param value is one of the three
			// places a user may have written their own credential
			// (DEC-073). The key is named instead, which is the one
			// line they have to look at.
			return invalid(where, fmt.Errorf(
				"a {{placeholder}} in this value is %w (they are %s)",
				ErrPlaceholderUnknown, strings.Join(placeholderNames, ", "),
			))
		}

		rest = rest[end+len(closeDelim):]
	}
}

// expand substitutes a validated template.
//
// The second return reports whether every placeholder in the template had
// something to put there. A template with an empty substitution in it is
// "incomplete", which is how a param that exists only to carry an offset is
// dropped entirely rather than sent empty.
//
// escape is applied to each substituted value and not to the template's own
// literal text, so a path template keeps its slashes while a keyword that
// contains one does not grow a path segment.
func expand(tmpl string, sub substitution, escape func(string) string) (string, bool) {
	var (
		out      strings.Builder
		complete = true
		rest     = tmpl
	)

	for {
		start := strings.Index(rest, openDelim)
		if start < 0 {
			out.WriteString(rest)

			return out.String(), complete
		}

		out.WriteString(rest[:start])
		rest = rest[start+len(openDelim):]

		end := strings.Index(rest, closeDelim)
		if end < 0 {
			// checkTemplate refuses this at validation, so it cannot
			// reach a live request. Writing the text back rather than
			// dropping it keeps the function total.
			out.WriteString(openDelim)
			out.WriteString(rest)

			return out.String(), complete
		}

		name := strings.ToLower(strings.TrimSpace(rest[:end]))

		value, known := sub.value(name)
		if !known || value == "" {
			complete = false
		}

		if escape != nil {
			value = escape(value)
		}

		out.WriteString(value)

		rest = rest[end+len(closeDelim):]
	}
}

// mentionsOffset reports whether a template carries the offset
// placeholder, which is what makes a block paginated: Query.Offset is
// meaningful to a source exactly when the definition has somewhere to put
// it.
func mentionsOffset(tmpl string) bool {
	return strings.Contains(strings.ToLower(tmpl), openDelim+placeholderOffset+closeDelim) ||
		strings.Contains(strings.ToLower(tmpl), openDelim+" "+placeholderOffset+" "+closeDelim)
}

// mentionsOffsetIn reports whether any param template carries the offset
// placeholder.
func mentionsOffsetIn(params map[string]string) bool {
	for _, tmpl := range params {
		if mentionsOffset(tmpl) {
			return true
		}
	}

	return false
}

// request builds the address and query parameters for one query against a
// block.
//
// The path is resolved against the definition's base as a reference, so a
// leading-slash path replaces the base's path and a relative one extends
// it — the ordinary URL rule, and the one a user writing a definition
// expects. Substituted path values are path-escaped; param values are left
// alone for url.Values to encode.
func (b *blockPlan) request(base *url.URL, q indexer.Query) (string, url.Values, error) {
	sub := substitutionFor(q)

	path, _ := expand(b.path, sub, url.PathEscape)

	target := base

	if path != "" {
		ref, err := url.Parse(path)
		if err != nil {
			// The path is a definition value and is never repeated
			// back; see DEC-073.
			return "", nil, ErrBaseAddressInvalid
		}

		target = base.ResolveReference(ref)
	}

	params := url.Values{}

	for key, tmpl := range b.params {
		value, complete := expand(tmpl, sub, nil)
		if !complete || value == "" {
			continue
		}

		params.Set(key, value)
	}

	return target.String(), params, nil
}
