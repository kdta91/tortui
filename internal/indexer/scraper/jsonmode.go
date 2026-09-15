package scraper

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// pathSeparator separates the segments of a json path expression, and
// pathEscape escapes one inside a key.
const (
	pathSeparator = '.'
	pathEscape    = '\\'
)

// compilePath parses a gjson-style path expression into its segments.
//
// The syntax is deliberately the small, obvious subset rather than the
// whole of gjson: dot-separated keys, a segment of digits addressing an
// array index, and a backslash escaping a literal dot or backslash inside
// a key. There are no wildcards, no queries, no modifiers and no
// multipaths, so the expression is read by a loop over a slice of strings
// and there is nothing in it that can recurse, backtrack, or evaluate.
// See DEC-075 for why this is hand-written rather than a dependency.
func compilePath(raw string) ([]string, error) {
	var (
		segments []string
		current  strings.Builder
	)

	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case pathEscape:
			if i+1 >= len(raw) {
				return nil, fmt.Errorf("%w (it ends with a %q)", ErrPathInvalid, string(pathEscape))
			}

			i++

			if raw[i] != pathSeparator && raw[i] != pathEscape {
				return nil, fmt.Errorf(
					"%w (a %q escapes only %q or %q)",
					ErrPathInvalid, string(pathEscape), string(pathSeparator), string(pathEscape),
				)
			}

			current.WriteByte(raw[i])
		case pathSeparator:
			if current.Len() == 0 {
				return nil, fmt.Errorf("%w (it has an empty segment)", ErrPathInvalid)
			}

			segments = append(segments, current.String())
			current.Reset()
		default:
			current.WriteByte(raw[i])
		}
	}

	if current.Len() == 0 {
		return nil, fmt.Errorf("%w (it has an empty segment)", ErrPathInvalid)
	}

	return append(segments, current.String()), nil
}

// jsonRows decodes a JSON response and returns the rows the block's path
// addresses, capped at maxRows.
//
// A path that addresses nothing returns no rows and no error, exactly as an
// HTML selector that matches nothing does: the two modes answer "this page
// has no results" the same way. A path that addresses something which is
// not an array is different — the definition is describing the response
// wrongly rather than the response being empty — and returns
// ErrRowsNotAList. That is also what a body which is not a JSON object or
// array at all produces, when the definition asked for the document
// itself.
func jsonRows(body []byte, rows matcher) ([]row, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	// Numbers are kept as their source text. A seeder count that went
	// through float64 and back would come out as "1.234e+03", and an id
	// long enough to lose precision would come out wrong.
	dec.UseNumber()

	var doc any

	if err := dec.Decode(&doc); err != nil {
		return nil, jsonFailure(err)
	}

	value := doc

	if !rows.empty() {
		found, ok := lookup(doc, rows.path)
		if !ok {
			return nil, nil
		}

		value = found
	}

	list, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%w (%s)", ErrRowsNotAList, describeJSON(value))
	}

	out := make([]row, 0, min(len(list), maxRows))

	for _, item := range list {
		if len(out) >= maxRows {
			break
		}

		out = append(out, jsonRow{value: item})
	}

	return out, nil
}

// jsonRow is one element of the rows array.
type jsonRow struct {
	value any
}

// text reads one field out of the row. A path that addresses nothing, and
// a value that is not a scalar, both yield the empty string — never an
// error.
func (r jsonRow) text(f *fieldPlan) string {
	value := r.value

	if !f.sel.empty() {
		found, ok := lookup(r.value, f.sel.path)
		if !ok {
			return ""
		}

		value = found
	}

	return scalar(value)
}

// lookup walks a decoded JSON value along a path.
//
// A segment addresses a key of an object, or — when it is all digits — an
// index of an array. Anything else (a missing key, an out-of-range index, a
// segment applied to a scalar) reports not-found rather than an error: a
// field the response did not carry is a zero value, not a failure.
func lookup(value any, path []string) (any, bool) {
	for _, segment := range path {
		switch container := value.(type) {
		case map[string]any:
			next, ok := container[segment]
			if !ok {
				return nil, false
			}

			value = next
		case []any:
			index, err := strconv.Atoi(segment)
			if err != nil || index < 0 || index >= len(container) {
				return nil, false
			}

			value = container[index]
		default:
			return nil, false
		}
	}

	return value, true
}

// scalar renders a decoded JSON value as the text a field carries. A
// container is not a scalar and renders as empty, which is the same zero
// value a missing key produces.
func scalar(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	case bool:
		return strconv.FormatBool(typed)
	case nil:
		return ""
	default:
		return ""
	}
}

// describeJSON names the kind of a decoded value, for the one error that
// needs to say what was there instead of an array. It names the kind and
// never the content: the content is the source's (DEC-072).
func describeJSON(value any) string {
	switch value.(type) {
	case map[string]any:
		return "it is a JSON object"
	case string:
		return "it is a JSON string"
	case json.Number:
		return "it is a JSON number"
	case bool:
		return "it is a JSON boolean"
	case nil:
		return "it is JSON null"
	default:
		return "it is not a JSON array"
	}
}

// jsonFailure turns an encoding/json failure into an error that says where
// the response stopped making sense without quoting any of it.
//
// encoding/json's own messages embed the offending byte — and, for a type
// error, the struct field and Go type — and the response is written by the
// source. So a syntax error and a type error contribute their byte offset
// and not their message, for the same reason torznab reports an XML line
// number and not the syntax error's text (DEC-072). A failure that is
// neither — a body that simply stops, which arrives as io.ErrUnexpectedEOF
// and carries no offset — contributes only its Go type, which is built
// from nothing the source wrote.
//
// Nesting depth needs no guard of its own here: encoding/json refuses a
// document nested past its own limit with an ordinary syntax error rather
// than recursing, which was verified against this Go version with a
// 100000-deep array (it reports "exceeded max depth" in constant time).
func jsonFailure(err error) error {
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return fmt.Errorf("%w (the JSON stops making sense at byte %d)", ErrDocumentMalformed, syntaxErr.Offset)
	}

	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return fmt.Errorf("%w (unexpected JSON value at byte %d)", ErrDocumentMalformed, typeErr.Offset)
	}

	return fmt.Errorf("%w (%T from encoding/json)", ErrDocumentMalformed, err)
}
