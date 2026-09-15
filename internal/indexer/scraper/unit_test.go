package scraper

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// The tests in this file exercise the small pure helpers directly, where
// driving every branch through an HTTP fixture would take a page of markup
// to say what one line says here.

// TestScalarAndDescribeJSON covers every decoded JSON kind in both
// directions: what a field reads out of it, and how the one error that has
// to mention a kind names it.
func TestScalarAndDescribeJSON(t *testing.T) {
	t.Parallel()

	cases := []struct {
		value    any
		scalar   string
		describe string
	}{
		{"text", "text", "it is a JSON string"},
		{json.Number("42"), "42", "it is a JSON number"},
		{true, "true", "it is a JSON boolean"},
		{false, "false", "it is a JSON boolean"},
		{nil, "", "it is JSON null"},
		{map[string]any{"a": 1}, "", "it is a JSON object"},
		{[]any{1, 2}, "", "it is not a JSON array"},
		{struct{}{}, "", "it is not a JSON array"},
	}

	for _, tc := range cases {
		if got := scalar(tc.value); got != tc.scalar {
			t.Errorf("scalar(%#v) = %q, want %q", tc.value, got, tc.scalar)
		}

		if got := describeJSON(tc.value); got != tc.describe {
			t.Errorf("describeJSON(%#v) = %q, want %q", tc.value, got, tc.describe)
		}
	}
}

// TestJSONFailure covers the three shapes of decode failure and, above all,
// that none of them repeats the document back.
func TestJSONFailure(t *testing.T) {
	t.Parallel()

	syntaxErr := jsonFailure(&json.SyntaxError{Offset: 17})
	if !errors.Is(syntaxErr, ErrDocumentMalformed) || !strings.Contains(syntaxErr.Error(), "byte 17") {
		t.Errorf("a syntax error reported as %q", syntaxErr)
	}

	typeErr := jsonFailure(&json.UnmarshalTypeError{Offset: 23, Value: "string", Type: nil})
	if !errors.Is(typeErr, ErrDocumentMalformed) || !strings.Contains(typeErr.Error(), "byte 23") {
		t.Errorf("a type error reported as %q", typeErr)
	}

	other := jsonFailure(io.ErrUnexpectedEOF)
	if !errors.Is(other, ErrDocumentMalformed) {
		t.Errorf("an unexpected end of input reported as %q", other)
	}

	if strings.Contains(other.Error(), "unexpected EOF") {
		t.Errorf("the message of the underlying error reached the text: %q", other)
	}
}

// TestLookup covers walking a decoded document, including the ways a path
// can address nothing.
func TestLookup(t *testing.T) {
	t.Parallel()

	doc := map[string]any{
		"items": []any{
			map[string]any{"name": "first"},
			map[string]any{"name": "second"},
		},
		"count": json.Number("2"),
	}

	found, ok := lookup(doc, []string{"items", "1", "name"})
	if !ok || found != "second" {
		t.Errorf("lookup into an array element = %v, %v", found, ok)
	}

	for _, path := range [][]string{
		{"missing"},
		{"items", "9"},
		{"items", "-1"},
		{"items", "name"},
		{"count", "anything"},
	} {
		if _, ok := lookup(doc, path); ok {
			t.Errorf("lookup(%v) found something", path)
		}
	}
}

// TestInfoHashDerivation covers every branch of the two helpers that decide
// what may become Result.InfoHash — the one field this adapter derives out
// of text the page controls.
func TestInfoHashDerivation(t *testing.T) {
	t.Parallel()

	const (
		hex    = "0123456789abcdef0123456789abcdef01234567"
		base32 = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	)

	normalise := map[string]string{
		"":                      "",
		"  " + hex + "  ":       hex,
		strings.ToUpper(hex):    hex,
		base32:                  base32,
		strings.ToLower(base32): base32,
		"zzzz" + hex[4:]:        "",
		"1" + base32[1:] + "":   "",
		"too-short":             "",
	}

	for in, want := range normalise {
		if got := normaliseInfoHash(in); got != want {
			t.Errorf("normaliseInfoHash(%q) = %q, want %q", in, got, want)
		}
	}

	inMagnet := map[string]string{
		"":                           "",
		"https://feed.example.org/x": "",
		"magnet:?xt=urn:btih:" + hex: hex,
		"magnet:?xt=urn:sha1:" + hex: "",
		"magnet:?dn=no-xt":           "",
		"magnet:?xt=urn:sha1:x&xt=urn:btih:" + hex: hex,
		"magnet:%%": "",
		// A control character is the one thing url.Parse refuses
		// outright, which is the only way into infoHashInMagnet's parse
		// failure branch.
		"magnet:?xt=urn:btih:\x7f": "",
	}

	for in, want := range inMagnet {
		if got := infoHashInMagnet(in); got != want {
			t.Errorf("infoHashInMagnet(%q) = %q, want %q", in, got, want)
		}
	}

	if got := infoHashFrom("not-a-hash", "magnet:?xt=urn:btih:"+hex); got != hex {
		t.Errorf("infoHashFrom fell back wrongly: %q", got)
	}
}

// TestIdentity covers the order Result.ID is decided in.
func TestIdentity(t *testing.T) {
	t.Parallel()

	const (
		hash = "0123456789abcdef0123456789abcdef01234567"
		page = "https://feed.example.org/item/1?k=v"
		file = "https://feed.example.org/dl/1?k=v"
	)

	cases := []struct {
		selected string
		hash     string
		page     string
		torrent  string
		title    string
		want     string
	}{
		{"1001", hash, page, file, "T", "1001"},
		{"", hash, page, file, "T", hash},
		{"", "", page, file, "T", "https://feed.example.org/item/1"},
		{"", "", "", file, "T", "https://feed.example.org/dl/1"},
		{"", "", "", "", "T", "T"},
		{"", "", "", "", "", ""},
	}

	for _, tc := range cases {
		if got := identity(tc.selected, tc.hash, tc.page, tc.torrent, tc.title); got != tc.want {
			t.Errorf("identity(%q, %q, %q, %q, %q) = %q, want %q",
				tc.selected, tc.hash, tc.page, tc.torrent, tc.title, got, tc.want)
		}
	}
}

// TestAbsolute covers resolving a link off the page against the base, and
// refusing every scheme that is not http or https.
func TestAbsolute(t *testing.T) {
	t.Parallel()

	base, err := url.Parse("https://feed.example.org/browse/")
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}

	p := &plan{base: base}

	cases := map[string]string{
		"":                            "",
		"   ":                         "",
		"/item/1":                     "https://feed.example.org/item/1",
		"item/1":                      "https://feed.example.org/browse/item/1",
		"https://other.example.org/x": "https://other.example.org/x",
		"//other.example.org/x":       "https://other.example.org/x",
		"javascript:alert(1)":         "",
		"data:text/html,<b>":          "",
		"file:///etc/passwd":          "",
		"magnet:?xt=urn:btih:abc":     "",
		"%zz":                         "",
	}

	for in, want := range cases {
		if got := p.absolute(in); got != want {
			t.Errorf("absolute(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestFirstMatch covers the regex narrowing: a group, no group, no match.
func TestFirstMatch(t *testing.T) {
	t.Parallel()

	cases := []struct {
		expr string
		in   string
		want string
	}{
		{`\d+`, "seeders: 1204", "1204"},
		{`^(\w+)`, "avery VIP", "avery"},
		{`^(\w+)`, "  ", ""},
		{`(a)(b)`, "zab", "a"},
	}

	for _, tc := range cases {
		re := regexp.MustCompile(tc.expr)

		if got := firstMatch(re, tc.in); got != tc.want {
			t.Errorf("firstMatch(%q, %q) = %q, want %q", tc.expr, tc.in, got, tc.want)
		}
	}
}

// TestExpand covers the template expander directly, including the
// unterminated placeholder validation refuses before a request is ever
// built.
func TestExpand(t *testing.T) {
	t.Parallel()

	sub := substitution{query: "a b", limit: "10"}

	cases := []struct {
		tmpl     string
		escape   func(string) string
		want     string
		complete bool
	}{
		{"/search", nil, "/search", true},
		{"{{query}}", nil, "a b", true},
		{"{{ QUERY }}", nil, "a b", true},
		{"{{query}}/{{limit}}", nil, "a b/10", true},
		{"{{offset}}", nil, "", false},
		{"page-{{offset}}", nil, "page-", false},
		{"{{nope}}", nil, "", false},
		{"/s/{{query}}", url.PathEscape, "/s/a%20b", true},
		{"{{unclosed", nil, "{{unclosed", true},
	}

	for _, tc := range cases {
		got, complete := expand(tc.tmpl, sub, tc.escape)
		if got != tc.want || complete != tc.complete {
			t.Errorf("expand(%q) = %q, %v; want %q, %v", tc.tmpl, got, complete, tc.want, tc.complete)
		}
	}
}

// TestMentionsOffset covers how Caps.Pagination is decided.
func TestMentionsOffset(t *testing.T) {
	t.Parallel()

	for tmpl, want := range map[string]bool{
		"":                false,
		"{{query}}":       false,
		"{{offset}}":      true,
		"{{ offset }}":    true,
		"{{OFFSET}}":      true,
		"p={{offset}}&q=": true,
	} {
		if got := mentionsOffset(tmpl); got != want {
			t.Errorf("mentionsOffset(%q) = %v, want %v", tmpl, got, want)
		}
	}
}

// TestSortedFieldNames pins the ordering validation depends on, so that a
// definition with more than one thing wrong fails the same way every run.
func TestSortedFieldNames(t *testing.T) {
	t.Parallel()

	fields := map[string]Field{
		fieldUploader: {},
		fieldTitle:    {},
		"zebra":       {},
		"apple":       {},
		"mango":       {},
	}

	got := strings.Join(sortedFieldNames(fields), ",")
	if want := "title,uploader,apple,mango,zebra"; got != want {
		t.Errorf("sortedFieldNames = %q, want %q", got, want)
	}
}

// TestCompileTrustReportsItsOwnSelector covers the trust block's selector
// going through the same compilation as any other field.
func TestCompileTrustReportsItsOwnSelector(t *testing.T) {
	t.Parallel()

	_, err := compileTrust("search.trust", &Trust{
		Spec:   Field{Selector: ".badge["},
		Values: map[string]string{"gold": "vip"},
	}, ModeHTML)

	if !errors.Is(err, ErrSelectorInvalid) {
		t.Fatalf("error = %v, want ErrSelectorInvalid", err)
	}

	var verr *ValidationError
	if !errors.As(err, &verr) || verr.Selector != ".badge[" {
		t.Errorf("the error does not name the trust selector: %v", err)
	}
}

// TestYAMLErrorPlaceWithoutAPosition covers the fallback for a decode error
// that carries no position.
func TestYAMLErrorPlaceWithoutAPosition(t *testing.T) {
	t.Parallel()

	got := yamlErrorPlace(errors.New("something with no position"))
	if !strings.Contains(got, "no position") {
		t.Errorf("yamlErrorPlace = %q", got)
	}

	if strings.Contains(got, "something") {
		t.Errorf("the underlying message reached the text: %q", got)
	}
}

// TestSkipToRunsOffTheEnd covers the depth scanner's unterminated cases.
func TestSkipToRunsOffTheEnd(t *testing.T) {
	t.Parallel()

	body := []byte("<!-- never closed")

	if got := skipTo(body, 0, "-->"); got != len(body) {
		t.Errorf("skipTo past an unterminated comment = %d, want %d", got, len(body))
	}

	if got := skipTo(body, 0, ""); got != 0 {
		t.Errorf("skipTo with an empty marker = %d, want 0", got)
	}

	if got := skipTo([]byte("<SCRIPT>x</SCRIPT>"), 8, "</script"); got != 17 {
		t.Errorf("skipTo is not case-insensitive: %d", got)
	}
}

// TestAPathThatCannotBeResolvedIsReportedWithoutItself covers the one error
// request can return, and that it does not echo the path back.
func TestAPathThatCannotBeResolvedIsReportedWithoutItself(t *testing.T) {
	t.Parallel()

	src := newSource(t, nil)

	def := src.pointAt(mustParse(t, strings.Replace(minimalDefinition, "  path: /s", "  path: /s%zz", 1)))

	a, err := New(Options{Definition: def})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = a.Search(testContext(t), keywordQuery())
	if !errors.Is(err, ErrBaseAddressInvalid) {
		t.Fatalf("error = %v, want ErrBaseAddressInvalid", err)
	}

	if strings.Contains(err.Error(), "%zz") {
		t.Errorf("the error echoed the path back: %q", err)
	}

	if len(src.requests()) != 0 {
		t.Errorf("a request was made anyway")
	}
}

// TestIsThousandsSeparator covers the comma rule directly, including the
// ends of a string where an off-by-one would live.
func TestIsThousandsSeparator(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"1,024":    true,
		"1,0":      false,
		"1,02":     false,
		"1,0245":   false,
		"1,024 MB": true,
		"1,4 GiB":  false,
		"1,":       false,
	}

	for in, want := range cases {
		at := strings.Index(in, ",")

		if got := isThousandsSeparator(in, at); got != want {
			t.Errorf("isThousandsSeparator(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestMatcherEmpty covers the one predicate that decides whether a field
// reads the row itself.
func TestMatcherEmpty(t *testing.T) {
	t.Parallel()

	blank, err := compileMatcher("   ", ModeHTML)
	if err != nil {
		t.Fatalf("compileMatcher: %v", err)
	}

	if !blank.empty() {
		t.Error("a blank selector is not reported as empty")
	}

	filled, err := compileMatcher("a.title", ModeHTML)
	if err != nil {
		t.Fatalf("compileMatcher: %v", err)
	}

	if filled.empty() {
		t.Error("a real selector is reported as empty")
	}

	if _, err := compileMatcher("a..b", ModeJSON); !errors.Is(err, ErrPathInvalid) {
		t.Errorf("a bad json path compiled: %v", err)
	}
}

// TestValidationErrorText covers the rendering of the error type itself.
func TestValidationErrorText(t *testing.T) {
	t.Parallel()

	withSelector := &ValidationError{Location: "search.fields.title.selector", Selector: "a[", Err: ErrSelectorInvalid}

	want := fmt.Sprintf("definition search.fields.title.selector: selector %q: %v", "a[", ErrSelectorInvalid)
	if got := withSelector.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}

	if !errors.Is(withSelector, ErrSelectorInvalid) {
		t.Error("Unwrap does not expose the sentinel")
	}

	without := &ValidationError{Location: "id", Err: ErrIDEmpty}
	if got := without.Error(); strings.Contains(got, "selector") {
		t.Errorf("Error() = %q, want no selector clause", got)
	}
}
