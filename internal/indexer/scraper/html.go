package scraper

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// maxHTMLDepth is the deepest element nesting this package will parse.
//
// It is a cost bound, not a correctness one. golang.org/x/net/html at
// v0.39.0 — the version this module pinned while it was written — parses a
// deeply nested document in time quadratic in its depth, measured on
// darwin/arm64 under Go 1.27.1 with a document that is nothing but nested
// <div> elements: depth 1000 took 8.5ms, 5000 took 119ms, 10000 took 400ms,
// 20000 took 1.6s, 40000 took 6.4s, and 100000 took 39s, for only 440KB and
// 1.1MB of input at the last two. Both sit comfortably inside httpx's 8MB
// body cap, and the document is written by the source, so a source that
// wants to burn a minute of tortui's CPU on one search needs no more than a
// small page to do it. A context deadline does not help: the parse is one
// uninterruptible call.
//
// That measured behaviour is a published vulnerability in its own right —
// GO-2026-4440, "Quadratic parsing complexity in golang.org/x/net/html",
// fixed upstream in x/net v0.45.0. The module now pins v0.58.0 (T-941
// raised the language floor to Go 1.25, which is what the fixed releases
// require; see the T-022 tracker notes and DEC-077), so the upstream fix is
// in place and govulncheck reports nothing against this call. The guard
// stays as defence in depth and because the upstream fix caps the parser's
// own open-element stack at 512: this limit is deliberately the same
// number, so the guard behaves identically before and after the upgrade.
// The whole test suite passes at v0.58.0 with no change to any source file.
//
// 512 costs about 2ms in the same measurement and is far past any real
// page. Depth is counted by guardHTMLDepth before the parser is handed
// anything.
const maxHTMLDepth = 512

// maxRows bounds how many rows one response may contribute.
//
// A rows selector is a definition value and the page is the source's, so
// "tr" against a page with a million rows is reachable without the user
// doing anything wrong. Every row costs a field extraction per mapped
// field, and the results table has no use for more than this anyway.
const maxRows = 1000

// voidElements never have children, so an unclosed one does not nest.
var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true,
	"hr": true, "img": true, "input": true, "link": true, "meta": true,
	"param": true, "source": true, "track": true, "wbr": true,
}

// optionalEndTagElements are the elements HTML5 closes implicitly when the
// next one opens, so a page full of unclosed ones produces a *flat* tree
// rather than a deep one.
//
// They are excluded from the depth count because counting them would refuse
// perfectly ordinary pages: unclosed <li>, <td>, <tr> and <p> are
// everywhere, and the parser does not nest them. Excluding them costs
// nothing against the case the count exists for, which needs an element
// that really does nest — a <div>, a <span>, a formatting element — to
// build a deep tree.
var optionalEndTagElements = map[string]bool{
	"body": true, "colgroup": true, "dd": true, "dt": true, "head": true,
	"html": true, "li": true, "optgroup": true, "option": true, "p": true,
	"rp": true, "rt": true, "tbody": true, "td": true, "tfoot": true,
	"th": true, "thead": true, "tr": true,
}

// rawTextElements hold text rather than markup, so a "<div>" inside one is
// a string and not a tag.
var rawTextElements = map[string]bool{
	"script": true, "style": true, "textarea": true, "title": true,
}

// htmlRows parses an HTML response and returns the rows the block's
// selector matches, capped at maxRows.
//
// A selector that matches nothing returns no rows and no error. That is the
// honest answer — it is indistinguishable from a page with no results, and
// a source is allowed to have none — and it is why the `t`est-a-source
// command exists rather than an error here.
func htmlRows(body []byte, rows matcher) ([]row, error) {
	if err := guardHTMLDepth(body); err != nil {
		return nil, err
	}

	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		// x/net/html recovers from anything a byte slice can contain,
		// so this is unreachable in practice. It is reported without
		// the cause's text all the same: the cause is built from the
		// document, and the document is the source's (DEC-073).
		return nil, ErrDocumentMalformed
	}

	selection := goquery.NewDocumentFromNode(doc).Selection

	if !rows.empty() {
		selection = selection.FindMatcher(rows.css)
	}

	out := make([]row, 0, min(selection.Length(), maxRows))

	selection.EachWithBreak(func(_ int, s *goquery.Selection) bool {
		out = append(out, htmlRow{sel: s})

		return len(out) < maxRows
	})

	return out, nil
}

// htmlRow is one matched element and everything under it.
type htmlRow struct {
	sel *goquery.Selection
}

// text reads one field out of the row: the selected element's named
// attribute, or its text. A selector that matches nothing, and an
// attribute the element does not carry, both yield the empty string —
// never an error (the acceptance criterion's "a missing optional selector
// yields a zero value").
func (r htmlRow) text(f *fieldPlan) string {
	sel := r.sel

	if !f.sel.empty() {
		sel = sel.FindMatcher(f.sel.css).First()
		if sel.Length() == 0 {
			return ""
		}
	}

	if f.attr != "" {
		return sel.AttrOr(f.attr, "")
	}

	return sel.Text()
}

// guardHTMLDepth walks the raw bytes and refuses a document whose element
// nesting exceeds maxHTMLDepth, before the parser sees it.
//
// It is a scanner, not a parser: it is linear in the length of the body, it
// allocates nothing, and it is deliberately approximate. Approximate is
// safe here because both directions of error are bounded — over-counting
// refuses a page (and the elements that would cause it are excluded, see
// optionalEndTagElements), under-counting parses a page the real parser
// will also find shallow. Comments, doctypes, processing instructions and
// the raw-text elements are skipped so their contents cannot be mistaken
// for markup.
func guardHTMLDepth(body []byte) error {
	depth := 0

	for i := 0; i < len(body); {
		if body[i] != '<' {
			i++

			continue
		}

		next := i + 1
		if next >= len(body) {
			return nil
		}

		switch {
		case body[next] == '!':
			i = skipBogus(body, i)
		case body[next] == '?':
			i = skipTo(body, i, ">")
		case body[next] == '/':
			name, after := tagName(body, next+1)
			i = skipTagBody(body, after)

			if !voidElements[name] && !optionalEndTagElements[name] && depth > 0 {
				depth--
			}
		case isTagStart(body[next]):
			name, after := tagName(body, next)
			end := skipTagBody(body, after)

			selfClosing := end >= 2 && body[end-2] == '/'

			if !selfClosing && !voidElements[name] && !optionalEndTagElements[name] {
				depth++

				if depth > maxHTMLDepth {
					return fmt.Errorf("%w (%d, limit %d)", ErrDocumentTooDeep, depth, maxHTMLDepth)
				}
			}

			i = end

			if rawTextElements[name] && !selfClosing {
				i = skipTo(body, i, "</"+name)
			}
		default:
			i++
		}
	}

	return nil
}

// isTagStart reports whether a byte can begin a tag name.
func isTagStart(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// tagName reads the lowercased tag name starting at i, and returns the
// index just past it.
func tagName(body []byte, i int) (string, int) {
	start := i

	for i < len(body) {
		b := body[i]
		if b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '>' || b == '/' {
			break
		}

		i++
	}

	return strings.ToLower(string(body[start:i])), i
}

// skipTagBody advances past the rest of a tag, honouring quoted attribute
// values so a ">" inside one does not end it early. It returns the index
// just past the closing ">".
func skipTagBody(body []byte, i int) int {
	var quote byte

	for ; i < len(body); i++ {
		switch {
		case quote != 0:
			if body[i] == quote {
				quote = 0
			}
		case body[i] == '"' || body[i] == '\'':
			quote = body[i]
		case body[i] == '>':
			return i + 1
		}
	}

	return len(body)
}

// skipBogus advances past a comment, a doctype, or a CDATA-like construct
// beginning at i.
func skipBogus(body []byte, i int) int {
	if bytes.HasPrefix(body[i:], []byte("<!--")) {
		return skipTo(body, i+4, "-->")
	}

	return skipTo(body, i, ">")
}

// skipTo returns the index just past the next case-insensitive occurrence
// of marker at or after i, or the end of the body when there is none.
//
// It compares in place rather than lowercasing a slice of the body: this is
// called once per comment, doctype and raw-text element, and allocating a
// copy of the remaining body each time would make a script-heavy page cost
// time quadratic in its length — the very shape of failure this file's
// depth guard exists to prevent.
func skipTo(body []byte, i int, marker string) int {
	n := len(marker)
	if n == 0 {
		return i
	}

	for ; i+n <= len(body); i++ {
		if foldedAt(body, i, marker) {
			return i + n
		}
	}

	return len(body)
}

// foldedAt reports whether marker occurs at i, comparing ASCII letters
// case-insensitively.
func foldedAt(body []byte, i int, marker string) bool {
	for j := 0; j < len(marker); j++ {
		if lowerASCII(body[i+j]) != lowerASCII(marker[j]) {
			return false
		}
	}

	return true
}

// lowerASCII lowercases one ASCII letter and leaves every other byte alone.
func lowerASCII(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}

	return b
}
