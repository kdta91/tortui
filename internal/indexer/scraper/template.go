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
	return checkPlaceholders(where, tmpl, substitution{}.value, placeholderNames)
}

// checkPlaceholders is checkTemplate over any placeholder set: known
// reports whether a name is one this kind of template substitutes, and
// names lists them for the message. A field template (Field.Template) has a
// set of its own, which is why the loop is shared rather than duplicated.
func checkPlaceholders(where, tmpl string, known func(string) (string, bool), names []string) error {
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

		if _, ok := known(name); !ok {
			// The placeholder's own name is not echoed. It is text out
			// of a param value, and a param value is one of the three
			// places a user may have written their own credential
			// (DEC-073). The key is named instead, which is the one
			// line they have to look at.
			return invalid(where, fmt.Errorf(
				"a {{placeholder}} in this value is %w (they are %s)",
				ErrPlaceholderUnknown, strings.Join(names, ", "),
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
	return expandWith(tmpl, sub.value, escape)
}

// expandWith is expand over any placeholder set; lookup returns a name's
// value and whether the name is known.
func expandWith(tmpl string, lookup func(string) (string, bool), escape func(string) string) (string, bool) {
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

		value, known := lookup(name)
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

// placeholderValue is the one placeholder a field template may contain: the
// value the field's selector read, after its regex and transforms.
const placeholderValue = "value"

// fieldTemplateNames lists the field-template placeholders for a
// validation message.
var fieldTemplateNames = []string{placeholderValue}

// fieldValue returns the lookup a field template is expanded with.
func fieldValue(v string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		if name == placeholderValue {
			return v, true
		}

		return "", false
	}
}

// checkFieldTemplate validates a field template: every placeholder is
// closed, every one is {{value}}, and there is at least one. Like
// checkTemplate it names the key and never the template's text (DEC-073).
func checkFieldTemplate(where, tmpl string) error {
	if err := checkPlaceholders(where, tmpl, fieldValue(""), fieldTemplateNames); err != nil {
		return err
	}

	// Expansion changes the text exactly when it substituted something —
	// a placeholder is always longer than the one-byte value put in its
	// place — so an unchanged expansion means there was no {{value}}.
	// That asks the question without a second parser.
	if got, _ := expandWith(tmpl, fieldValue("v"), nil); got == tmpl {
		return invalid(where, ErrTemplateValueMissing)
	}

	return nil
}

// fillFieldTemplate places a read value into a validated field template.
// The value is escaped as one path segment (escapeSegment), so a value read
// off the response can never add or cancel a path segment, or add a query
// or a fragment, to the address the template builds; the template's own
// literal text is left alone. An empty value yields empty: a row that
// carried no id has no link to build, and the template's bare text would
// be a link to nothing.
func fillFieldTemplate(tmpl, v string) string {
	if v == "" {
		return ""
	}

	out, _ := expandWith(tmpl, fieldValue(v), escapeSegment)

	return out
}

// escapeSegment path-escapes a value so it stays exactly one path segment.
// url.PathEscape covers "/", "?" and "#", but leaves "." alone, so a value
// that is exactly "." or ".." would still be a dot segment that URL
// resolution removes — ".." cancelling the segment before it. Those two
// are percent-encoded dot by dot instead, which resolution leaves in place.
func escapeSegment(v string) string {
	if v == "." || v == ".." {
		return strings.Repeat("%2E", len(v))
	}

	return url.PathEscape(v)
}
