package scraper

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/indexer"
	"github.com/kdta91/tortui/internal/indexer/httpx"
)

// A definition is user-supplied config and a response is written by the
// source, so both are hostile input. Everything in this file is a shape of
// hostile input that must cost bounded time and bounded memory, and must
// never panic.

// adapterFor builds an adapter from an inline definition pointed at src.
func adapterFor(t *testing.T, src *fakeSource, source string) *Adapter {
	t.Helper()

	def, err := Parse([]byte(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	return mustAdapter(t, src, def)
}

// nestedDivs builds a page that is nothing but nested <div> elements.
func nestedDivs(depth int) string {
	var b strings.Builder

	b.WriteString("<html><body>")
	b.WriteString(strings.Repeat("<div>", depth))
	b.WriteString(`<table class="results"><tr><td><a href="/i/1">Invented Deep Row</a></td></tr></table>`)
	b.WriteString(strings.Repeat("</div>", depth))
	b.WriteString("</body></html>")

	return b.String()
}

// TestADeeplyNestedPageIsRefusedBeforeTheParserSeesIt is the measured
// hazard in html.go's maxHTMLDepth comment, turned into a test.
//
// The bound asserted is deliberately loose. The point is the difference in
// order of magnitude: the same document took 39 seconds to parse when it
// was measured without the guard, and the guard's own scan of it is a
// single linear pass. Anything under ten seconds proves the parse did not
// happen; the measured time on this machine is a few tens of milliseconds.
func TestADeeplyNestedPageIsRefusedBeforeTheParserSeesIt(t *testing.T) {
	t.Parallel()

	src := newSource(t, map[string]reply{"/s": pageReply(nestedDivs(100000))})
	a := adapterFor(t, src, minimalDefinition)

	started := time.Now()

	_, err := a.Search(testContext(t), keywordQuery())

	elapsed := time.Since(started)

	if !errors.Is(err, ErrDocumentTooDeep) {
		t.Fatalf("error = %v, want ErrDocumentTooDeep", err)
	}

	if elapsed > 10*time.Second {
		t.Errorf("refusing the document took %s; the guard is supposed to run instead of the parse", elapsed)
	}
}

// TestAPageAtTheDepthLimitIsStillParsed keeps the guard from being a
// blanket refusal.
func TestAPageAtTheDepthLimitIsStillParsed(t *testing.T) {
	t.Parallel()

	// maxHTMLDepth counts the nesting the scanner sees; the wrapper
	// markup around the divs contributes a few levels of its own, so the
	// page is built comfortably under the limit.
	src := newSource(t, map[string]reply{"/s": pageReply(nestedDivs(maxHTMLDepth - 16))})
	a := adapterFor(t, src, minimalDefinition)

	results, err := a.Search(testContext(t), keywordQuery())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
}

// TestUnclosedOptionalEndTagsDoNotTripTheDepthGuard is why the guard
// excludes the elements HTML5 closes implicitly: a page with thousands of
// unclosed <li>, <tr> and <p> elements is ordinary, and the parser builds
// a flat tree from it.
func TestUnclosedOptionalEndTagsDoNotTripTheDepthGuard(t *testing.T) {
	t.Parallel()

	var b strings.Builder

	b.WriteString("<html><body><ul>")

	for i := 0; i < maxHTMLDepth*4; i++ {
		fmt.Fprintf(&b, "<li>item %d", i)
	}

	b.WriteString("</ul><table class=\"results\"><tr><td><a href=\"/i/1\">Invented Flat Row</a>")
	b.WriteString("</table>")

	for i := 0; i < maxHTMLDepth*4; i++ {
		fmt.Fprintf(&b, "<p>paragraph %d", i)
	}

	b.WriteString("</body></html>")

	src := newSource(t, map[string]reply{"/s": pageReply(b.String())})
	a := adapterFor(t, src, minimalDefinition)

	results, err := a.Search(testContext(t), keywordQuery())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
}

// TestTagShapedTextInsideRawTextElementsIsNotCounted covers the other
// direction of the same false positive: a page whose script or style block
// is full of tag-shaped strings is not nested at all.
func TestTagShapedTextInsideRawTextElementsIsNotCounted(t *testing.T) {
	t.Parallel()

	var b strings.Builder

	b.WriteString("<html><head><style>/* ")
	b.WriteString(strings.Repeat("<div>", maxHTMLDepth*2))
	b.WriteString(" */</style></head><body><!-- ")
	b.WriteString(strings.Repeat("<section>", maxHTMLDepth*2))
	b.WriteString(" --><script>var s = \"")
	b.WriteString(strings.Repeat("<div>", maxHTMLDepth*2))
	b.WriteString("\";</script>")
	b.WriteString(`<table class="results"><tr><td><a href="/i/1">Invented Quiet Row</a></td></tr></table>`)
	b.WriteString("</body></html>")

	src := newSource(t, map[string]reply{"/s": pageReply(b.String())})
	a := adapterFor(t, src, minimalDefinition)

	results, err := a.Search(testContext(t), keywordQuery())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1: %v", len(results), err)
	}
}

// TestASelectorThatMatchesThousandsOfRowsIsCapped covers the other half of
// a hostile page: not deep, but wide.
func TestASelectorThatMatchesThousandsOfRowsIsCapped(t *testing.T) {
	t.Parallel()

	var b strings.Builder

	b.WriteString("<html><body><table>")

	for i := 0; i < maxRows*3; i++ {
		fmt.Fprintf(&b, `<tr><td><a href="/i/%d">Invented Row %d</a></td></tr>`, i, i)
	}

	b.WriteString("</table></body></html>")

	src := newSource(t, map[string]reply{"/s": pageReply(b.String())})
	a := adapterFor(t, src, minimalDefinition)

	results, err := a.Search(testContext(t), keywordQuery())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != maxRows {
		t.Fatalf("got %d results from a page with %d rows, want the cap of %d", len(results), maxRows*3, maxRows)
	}
}

// TestARegexThatWouldBacktrackDoesNot checks the claim in Field.Regex's doc
// rather than assuming it: Go's RE2 has no backtracking, so the classic
// (a+)+$ pattern against a long non-matching string is linear.
func TestARegexThatWouldBacktrackDoesNot(t *testing.T) {
	t.Parallel()

	title := strings.Repeat("a", 40000) + "b"

	source := strings.Replace(minimalDefinition, "  title:\n    selector: a", `  title:
    selector: a
    regex: "(a+)+$"`, 1)

	src := newSource(t, map[string]reply{
		"/s": pageReply(`<html><body><table><tr><td><a href="/i/1">` + title + `</a></td></tr></table></body></html>`),
	})

	a := adapterFor(t, src, source)

	started := time.Now()

	results, err := a.Search(testContext(t), keywordQuery())

	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	// The regex does not match, so the title is empty and the row is
	// skipped — which is the documented behaviour of a regex that does
	// not match, and it is reached in milliseconds rather than never.
	if len(results) != 0 {
		t.Fatalf("got %d results, want none: the regex does not match", len(results))
	}

	if elapsed > 10*time.Second {
		t.Errorf("the regex took %s on a 40000-character value", elapsed)
	}
}

// TestAYAMLAliasBombIsRefused covers the classic billion-laughs document.
//
// Two things stop it, and both are asserted. Strict decoding refuses the
// top-level keys the anchors are attached to, so the document never
// decodes; and goccy's decoder shares the aliased value rather than
// expanding it, so even the bomb attached to a key the schema does define
// costs memory linear in the source. Measured: the nine-level, fan-nine
// bomb below decodes in well under a millisecond.
func TestAYAMLAliasBombIsRefused(t *testing.T) {
	t.Parallel()

	var bomb strings.Builder

	bomb.WriteString(`l0: &l0 ["x","x","x","x","x","x","x","x","x"]` + "\n")

	for level := 1; level <= 9; level++ {
		fmt.Fprintf(&bomb, "l%d: &l%d [", level, level)

		for i := 0; i < 9; i++ {
			if i > 0 {
				bomb.WriteString(",")
			}

			fmt.Fprintf(&bomb, "*l%d", level-1)
		}

		bomb.WriteString("]\n")
	}

	started := time.Now()

	_, err := Parse([]byte(minimalDefinition + bomb.String()))

	elapsed := time.Since(started)

	if !errors.Is(err, ErrDefinitionMalformed) {
		t.Fatalf("error = %v, want ErrDefinitionMalformed: strict decoding refuses the keys the anchors hang on", err)
	}

	if elapsed > 10*time.Second {
		t.Errorf("refusing the alias bomb took %s", elapsed)
	}

	// The same aliasing, attached to a key the schema does define. It
	// decodes, and what it decodes to is bounded by the schema's own
	// shape: transform is a list of strings, so an alias to a list of
	// lists cannot become one.
	aliased := strings.Replace(minimalDefinition, "  title:\n    selector: a",
		"  title:\n    selector: &s a\n    transform: [trim]\n  uploader:\n    selector: *s", 1)

	def, err := Parse([]byte(aliased))
	if err != nil {
		t.Fatalf("an ordinary anchor and alias should still work: %v", err)
	}

	if def.Fields[fieldUploader].Selector != "a" {
		t.Errorf("the alias did not resolve: %+v", def.Fields[fieldUploader])
	}
}

// TestAJSONResponseThatIsNotWhatTheDefinitionExpects covers every shape of
// body a json definition can be handed.
func TestAJSONResponseThatIsNotWhatTheDefinitionExpects(t *testing.T) {
	t.Parallel()

	jsonMinimal := strings.Replace(minimalDefinition, "rows: tr", "mode: json\nrows: items", 1)
	jsonMinimal = strings.Replace(jsonMinimal, "  title:\n    selector: a", "  title:\n    selector: name", 1)
	jsonMinimal = strings.Replace(jsonMinimal, "  magnet:\n    selector: a\n    attr: href", "  magnet:\n    selector: magnet", 1)

	deep := strings.Repeat("[", 100000) + "1" + strings.Repeat("]", 100000)

	cases := map[string]struct {
		body    string
		want    error
		results int
	}{
		"an object where the rows path leads nowhere": {body: `{"other":[]}`, results: 0},
		"a top-level array where an object was expected": {
			body: `[{"name":"Invented"}]`, want: nil, results: 0,
		},
		"a string body":                   {body: `"hello"`, results: 0},
		"a number body":                   {body: `42`, results: 0},
		"null":                            {body: `null`, results: 0},
		"the rows path is not a list":     {body: `{"items":{"a":1}}`, want: ErrRowsNotAList},
		"an HTML page":                    {body: `<html><body>Sign in</body></html>`, want: ErrDocumentMalformed},
		"a truncated document":            {body: `{"items":[{"name":"Inv`, want: ErrDocumentMalformed},
		"an empty body":                   {body: ``, want: ErrDocumentMalformed},
		"nested past the decoder's limit": {body: deep, want: ErrDocumentMalformed},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			src := newSource(t, map[string]reply{"/s": jsonReply(tc.body)})
			a := adapterFor(t, src, jsonMinimal)

			results, err := a.Search(testContext(t), keywordQuery())

			if tc.want != nil {
				if !errors.Is(err, tc.want) {
					t.Fatalf("error = %v, want %v", err, tc.want)
				}

				return
			}

			if err != nil {
				t.Fatalf("Search: %v", err)
			}

			if len(results) != tc.results {
				t.Fatalf("got %d results, want %d", len(results), tc.results)
			}
		})
	}
}

// TestNoResponseShapeEverPanics is the blunt version of everything above:
// a body drawn from a spread of hostile shapes, through both modes, must
// come back as a result set or an error and never as a panic.
func TestNoResponseShapeEverPanics(t *testing.T) {
	t.Parallel()

	bodies := []string{
		"",
		"\x00\x00\x00",
		"<html",
		"<table><tr><td><a href=",
		strings.Repeat("<", 10000),
		strings.Repeat("</div>", 10000),
		`<table><tr><td><a href="/i/1">` + strings.Repeat("ü", 10000) + `</a>`,
		`{"items":`,
		`{"items":[null,1,"two",[],{}]}`,
		"\xff\xfe invalid utf-8",
	}

	jsonMinimal := strings.Replace(minimalDefinition, "rows: tr", "mode: json\nrows: items", 1)
	jsonMinimal = strings.Replace(jsonMinimal, "  title:\n    selector: a", "  title:\n    selector: name", 1)
	jsonMinimal = strings.Replace(jsonMinimal, "  magnet:\n    selector: a\n    attr: href", "  magnet:\n    selector: magnet", 1)

	for i, body := range bodies {
		for _, definition := range []string{minimalDefinition, jsonMinimal} {
			src := newSource(t, map[string]reply{"/s": pageReply(body)})
			a := adapterFor(t, src, definition)

			if _, err := a.Search(testContext(t), keywordQuery()); err != nil {
				t.Logf("body %d: %v", i, err)
			}
		}
	}
}

// TestAnEmptyResponseBodyIsNotAnError covers the html side of the same
// ground: an empty page is a page with no rows.
func TestAnEmptyResponseBodyIsNotAnError(t *testing.T) {
	t.Parallel()

	src := newSource(t, map[string]reply{"/s": pageReply("")})
	a := adapterFor(t, src, minimalDefinition)

	results, err := a.Search(testContext(t), keywordQuery())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != 0 {
		t.Fatalf("got %d results from an empty page", len(results))
	}
}

// TestGuardHTMLDepthOnItsOwn exercises the scanner directly on the shapes
// a page can end in the middle of, where a parser loop is easiest to get
// wrong.
func TestGuardHTMLDepthOnItsOwn(t *testing.T) {
	t.Parallel()

	fine := []string{
		"",
		"<",
		"<div",
		"<!--",
		"<!doctype html>",
		"<?xml version=\"1.0\"?>",
		"<script>",
		"<div attr=\"a>b\">x</div>",
		"<br><br><br>",
		"<div/><div/>",
		"<3 not a tag",
		"</div></div>",
		strings.Repeat("<div></div>", 10000),
	}

	for _, body := range fine {
		if err := guardHTMLDepth([]byte(body)); err != nil {
			t.Errorf("guardHTMLDepth(%.30q) = %v, want no error", body, err)
		}
	}

	if err := guardHTMLDepth([]byte(strings.Repeat("<span>", maxHTMLDepth+1))); !errors.Is(err, ErrDocumentTooDeep) {
		t.Errorf("a document one level past the limit was not refused: %v", err)
	}
}

// TestAHostileDefinitionCannotReachTheNetworkTwice is a small sanity check
// that a definition cannot turn one query into a storm of requests: one
// Search is one request, whatever the definition says.
func TestAHostileDefinitionCannotReachTheNetworkTwice(t *testing.T) {
	t.Parallel()

	src := newSource(t, map[string]reply{"/s": pageReply("<html><body></body></html>")})

	a, err := New(Options{
		Definition: src.pointAt(mustParse(t, minimalDefinition)),
		Client:     testClient(httpx.Config{}),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := a.Search(testContext(t), indexer.Query{Text: "one"}); err != nil {
		t.Fatalf("Search: %v", err)
	}

	if got := len(src.requests()); got != 1 {
		t.Fatalf("one Search made %d requests", got)
	}
}

// mustParse parses a definition that is expected to be valid.
func mustParse(t *testing.T, source string) *Definition {
	t.Helper()

	def, err := Parse([]byte(source))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	return def
}

// TestAJSONEndpointWithThousandsOfItemsIsCapped is the json counterpart of
// the wide-page case: maxRows bounds both modes.
func TestAJSONEndpointWithThousandsOfItemsIsCapped(t *testing.T) {
	t.Parallel()

	var b strings.Builder

	b.WriteString(`{"items":[`)

	for i := 0; i < maxRows*2; i++ {
		if i > 0 {
			b.WriteString(",")
		}

		fmt.Fprintf(&b, `{"name":"Invented Item %d","magnet":"magnet:?xt=urn:btih:%040d"}`, i, i)
	}

	b.WriteString("]}")

	source := strings.Replace(minimalDefinition, "rows: tr", "mode: json\nrows: items", 1)
	source = strings.Replace(source, "  title:\n    selector: a", "  title:\n    selector: name", 1)
	source = strings.Replace(source, "  magnet:\n    selector: a\n    attr: href", "  magnet:\n    selector: magnet", 1)

	src := newSource(t, map[string]reply{"/s": jsonReply(b.String())})
	a := adapterFor(t, src, source)

	results, err := a.Search(testContext(t), keywordQuery())
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(results) != maxRows {
		t.Fatalf("got %d results from %d items, want the cap of %d", len(results), maxRows*2, maxRows)
	}
}
