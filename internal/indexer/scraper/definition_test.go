package scraper

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// minimalDefinition is the smallest definition that validates. Every
// validation test below is this with one thing wrong.
const minimalDefinition = `
id: fixture-minimal
base_url: https://feed.example.org
rows: tr
fields:
  title:
    selector: a
  magnet:
    selector: a
    attr: href
search:
  path: /s
`

// TestParseAcceptsTheCheckedInDefinitions is the floor: the worked example
// in docs/indexer-definitions.md is this file, and it has to parse.
func TestParseAcceptsTheCheckedInDefinitions(t *testing.T) {
	t.Parallel()

	for _, name := range []string{htmlDefinitionFile, searchOnlyDefinitionFile, jsonDefinitionFile} {
		def := loadDefinition(t, name)

		if def.ID == "" || def.Search == nil {
			t.Errorf("%s parsed into an empty definition: %+v", name, def)
		}

		if err := def.Validate(); err != nil {
			t.Errorf("%s: Validate after Parse: %v", name, err)
		}
	}
}

// TestValidationNamesTheFailingFieldAndSelector is the acceptance
// criterion "definition validation produces actionable errors naming the
// failing field and selector", asserted on the message rather than on the
// fact that an error happened.
func TestValidationNamesTheFailingFieldAndSelector(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		source   string
		want     error
		location string
		selector string
	}{
		"no id": {
			source:   strings.Replace(minimalDefinition, "id: fixture-minimal", "name: x", 1),
			want:     ErrIDEmpty,
			location: "id",
		},
		"no base address": {
			source:   strings.Replace(minimalDefinition, "base_url: https://feed.example.org", "mode: html", 1),
			want:     ErrBaseAddressEmpty,
			location: "base_url",
		},
		"base address is not a URL": {
			// An unparseable port rather than an unbalanced bracket:
			// both fail url.Parse, and this one keeps the host readable
			// so scripts/check-indexer-hostnames.sh can see it is a
			// reserved example.org name (backlog T-926).
			source:   strings.Replace(minimalDefinition, "https://feed.example.org", "http://feed.example.org:not-a-port", 1),
			want:     ErrBaseAddressInvalid,
			location: "base_url",
		},
		"base address scheme": {
			source:   strings.Replace(minimalDefinition, "https://feed.example.org", "ftp://feed.example.org", 1),
			want:     ErrBaseAddressSchemeUnsupported,
			location: "base_url",
		},
		"base address has no host": {
			source:   strings.Replace(minimalDefinition, "https://feed.example.org", "https:///only/a/path", 1),
			want:     ErrBaseAddressHostMissing,
			location: "base_url",
		},
		"unknown mode": {
			source:   minimalDefinition + "mode: xml\n",
			want:     ErrModeUnknown,
			location: "mode",
		},
		"no search block": {
			source:   strings.Replace(minimalDefinition, "search:\n  path: /s\n", "", 1),
			want:     ErrSearchBlockMissing,
			location: "search",
		},
		"no rows selector": {
			source:   strings.Replace(minimalDefinition, "rows: tr\n", "", 1),
			want:     ErrRowsSelectorMissing,
			location: "search.rows",
		},
		"rows selector will not compile": {
			source:   strings.Replace(minimalDefinition, "rows: tr", `rows: "tr[class"`, 1),
			want:     ErrSelectorInvalid,
			location: "search.rows",
			selector: "tr[class",
		},
		"no title field": {
			source:   strings.Replace(minimalDefinition, "  title:\n    selector: a\n", "", 1),
			want:     ErrTitleFieldMissing,
			location: "search.fields.title",
		},
		"no link field": {
			source:   strings.Replace(minimalDefinition, "  magnet:\n    selector: a\n    attr: href\n", "", 1),
			want:     ErrLinkFieldMissing,
			location: "search.fields",
		},
		"unknown field": {
			source:   strings.Replace(minimalDefinition, "  title:", "  titel:\n    selector: a\n  title:", 1),
			want:     ErrFieldUnknown,
			location: "search.fields.titel",
		},
		"field selector will not compile": {
			source: strings.Replace(minimalDefinition, "  title:\n    selector: a", `  title:
    selector: "td.name a["`, 1),
			want:     ErrSelectorInvalid,
			location: "search.fields.title.selector",
			selector: "td.name a[",
		},
		"attr and text together": {
			source:   strings.Replace(minimalDefinition, "    attr: href", "    attr: href\n    text: true", 1),
			want:     ErrAttrAndText,
			location: "search.fields.magnet",
			selector: "a",
		},
		"layouts outside published": {
			source:   strings.Replace(minimalDefinition, "  title:\n    selector: a", "  title:\n    selector: a\n    layouts: [\"2006\"]", 1),
			want:     ErrLayoutsOutsidePublished,
			location: "search.fields.title.layouts",
		},
		"regex will not compile": {
			source: strings.Replace(minimalDefinition, "  title:\n    selector: a", `  title:
    selector: a
    regex: "([unclosed"`, 1),
			want:     ErrRegexInvalid,
			location: "search.fields.title.regex",
		},
		"unknown transform": {
			source:   strings.Replace(minimalDefinition, "  title:\n    selector: a", "  title:\n    selector: a\n    transform: [shout]", 1),
			want:     ErrTransformUnknown,
			location: "search.fields.title.transform",
		},
		"transform chain too long": {
			source: strings.Replace(minimalDefinition, "  title:\n    selector: a",
				"  title:\n    selector: a\n    transform: [trim, trim, trim, trim, trim, trim, trim, trim, trim]", 1),
			want:     ErrTransformChainTooLong,
			location: "search.fields.title.transform",
		},
		"trust with no values": {
			source:   minimalDefinition + "trust:\n  selector: .badge\n",
			want:     ErrTrustValuesMissing,
			location: "search.trust.values",
		},
		"unknown trust level": {
			source:   minimalDefinition + "trust:\n  selector: .badge\n  values:\n    gold: platinum\n",
			want:     ErrTrustLevelUnknown,
			location: "search.trust.values.gold",
		},
		"unknown placeholder": {
			source:   minimalDefinition + "  params:\n    q: \"{{apikey}}\"\n",
			want:     ErrPlaceholderUnknown,
			location: "search.params.q",
		},
		"unterminated placeholder": {
			source:   minimalDefinition + "  params:\n    q: \"{{query\"\n",
			want:     ErrPlaceholderUnterminated,
			location: "search.params.q",
		},
		"unknown placeholder in the path": {
			source:   strings.Replace(minimalDefinition, "  path: /s", "  path: /s/{{page}}", 1),
			want:     ErrPlaceholderUnknown,
			location: "search.path",
		},
		"a latest block reports itself as latest": {
			source:   minimalDefinition + "latest:\n  rows: \"li[\"\n",
			want:     ErrSelectorInvalid,
			location: "latest.rows",
			selector: "li[",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse([]byte(tc.source))
			if err == nil {
				t.Fatalf("the definition validated; it should not have")
			}

			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want it to wrap %v", err, tc.want)
			}

			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("error = %v (%T), want a *ValidationError", err, err)
			}

			if verr.Location != tc.location {
				t.Errorf("Location = %q, want %q", verr.Location, tc.location)
			}

			if verr.Selector != tc.selector {
				t.Errorf("Selector = %q, want %q", verr.Selector, tc.selector)
			}

			if !strings.Contains(err.Error(), tc.location) {
				t.Errorf("the message does not name the failing key: %q", err.Error())
			}

			if tc.selector != "" && !strings.Contains(err.Error(), tc.selector) {
				t.Errorf("the message does not name the selector: %q", err.Error())
			}
		})
	}
}

// TestJSONModeRefusesAnAttrAndABadPath covers the two validation rules that
// exist only in the json mode.
func TestJSONModeRefusesAnAttrAndABadPath(t *testing.T) {
	t.Parallel()

	jsonMinimal := strings.Replace(minimalDefinition, "rows: tr", "mode: json\nrows: items", 1)

	withAttr := strings.Replace(jsonMinimal, "  title:\n    selector: a", "  title:\n    selector: a\n    attr: href", 1)

	err := mustNotParse(t, withAttr)
	if !errors.Is(err, ErrAttrInJSONMode) {
		t.Errorf("error = %v, want ErrAttrInJSONMode", err)
	}

	for _, path := range []string{"a..b", "a.", `a\`, `a\x`} {
		// Single-quoted so YAML leaves a backslash alone: in a
		// double-quoted scalar it would be an escape and the file would
		// not decode at all, which is a different failure.
		bad := strings.Replace(jsonMinimal, "  title:\n    selector: a", "  title:\n    selector: '"+path+"'", 1)

		err := mustNotParse(t, bad)
		if !errors.Is(err, ErrPathInvalid) {
			t.Errorf("path %q: error = %v, want ErrPathInvalid", path, err)
		}

		var verr *ValidationError
		if !errors.As(err, &verr) || verr.Selector != path {
			t.Errorf("path %q: the error does not name the path it rejected: %v", path, err)
		}
	}
}

// mustNotParse parses a definition that is expected to fail and returns the
// error.
func mustNotParse(t *testing.T, source string) error {
	t.Helper()

	_, err := Parse([]byte(source))
	if err == nil {
		t.Fatal("the definition validated; it should not have")
	}

	return err
}

// TestParseIsStrictAboutTheShapeOfTheFile covers the decode-level refusals:
// an unknown key, a duplicate key, a value of the wrong type, and a file
// with nothing in it.
func TestParseIsStrictAboutTheShapeOfTheFile(t *testing.T) {
	t.Parallel()

	for name, source := range map[string]string{
		"unknown key":   minimalDefinition + "selectors: {}\n",
		"duplicate key": minimalDefinition + "id: again\n",
		"wrong type":    strings.Replace(minimalDefinition, "rows: tr", "rows: [a, b]", 1),
		"syntax error":  minimalDefinition + "\tnot: indented\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := mustNotParse(t, source)
			if !errors.Is(err, ErrDefinitionMalformed) {
				t.Fatalf("error = %v, want ErrDefinitionMalformed", err)
			}

			if !strings.Contains(err.Error(), "line") {
				t.Errorf("the message does not say where the problem is: %q", err.Error())
			}
		})
	}

	for name, source := range map[string]string{
		"empty":         "",
		"whitespace":    "   \n\n",
		"comments only": "# nothing here\n",
		"a bare scalar": "---\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if err := mustNotParse(t, source); !errors.Is(err, ErrDefinitionEmpty) {
				t.Fatalf("error = %v, want ErrDefinitionEmpty", err)
			}
		})
	}
}

// TestFieldsAreSharedByDefaultAndOverriddenPerBlock is the inheritance
// criterion at the unit level, in both directions and for all three
// inheritable things.
func TestFieldsAreSharedByDefaultAndOverriddenPerBlock(t *testing.T) {
	t.Parallel()

	shared := Field{Selector: ".shared"}
	own := Field{Selector: ".own"}

	def := &Definition{
		Rows:   "tr",
		Fields: map[string]Field{fieldTitle: shared, fieldSize: shared},
		Trust:  &Trust{Spec: Field{Selector: ".shared-badge"}, Values: map[string]string{"a": "vip"}},
	}

	inheriting := &Block{}
	overriding := &Block{
		Rows:   "li",
		Fields: map[string]Field{fieldTitle: own},
		Trust:  &Trust{Spec: Field{Selector: ".own-badge"}, Values: map[string]string{"b": "none"}},
	}

	if got := def.effectiveRows(inheriting); got != "tr" {
		t.Errorf("a block with no rows selector got %q, want the shared one", got)
	}

	if got := def.effectiveRows(overriding); got != "li" {
		t.Errorf("a block with its own rows selector got %q, want it", got)
	}

	if got := def.effectiveFields(inheriting); got[fieldTitle].Selector != shared.Selector ||
		got[fieldSize].Selector != shared.Selector {
		t.Errorf("a block with no fields got %+v, want both shared ones", got)
	}

	merged := def.effectiveFields(overriding)

	if merged[fieldTitle].Selector != own.Selector {
		t.Errorf("the overridden field is %+v, want the block's own", merged[fieldTitle])
	}

	if merged[fieldSize].Selector != shared.Selector {
		t.Errorf("the field the block did not override is %+v, want the shared one", merged[fieldSize])
	}

	if got := def.effectiveTrust(inheriting); got != def.Trust {
		t.Errorf("a block with no trust block got %+v, want the shared one", got)
	}

	if got := def.effectiveTrust(overriding); got != overriding.Trust {
		t.Errorf("a block with its own trust block got %+v, want it", got)
	}

	// Overriding must not write back into the shared map: the two blocks
	// are compiled one after the other and the second must see the
	// definition's own fields, not the first block's.
	if def.Fields[fieldTitle].Selector != shared.Selector {
		t.Errorf("effectiveFields mutated the definition's own map: %+v", def.Fields)
	}
}

// TestDefaultsAreApplied covers name-defaults-to-id and
// mode-defaults-to-html.
func TestDefaultsAreApplied(t *testing.T) {
	t.Parallel()

	def, err := Parse([]byte(minimalDefinition))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if def.name() != "fixture-minimal" {
		t.Errorf("name() = %q, want the id", def.name())
	}

	if def.mode() != ModeHTML {
		t.Errorf("mode() = %q, want %q", def.mode(), ModeHTML)
	}

	src := newSource(t, nil)

	a := mustAdapter(t, src, def)
	if a.Name() != "fixture-minimal" {
		t.Errorf("Name() = %q, want the id", a.Name())
	}
}

// TestCompilePath covers the json path expression syntax, including the
// escape.
func TestCompilePath(t *testing.T) {
	t.Parallel()

	ok := map[string][]string{
		"a":            {"a"},
		"a.b.c":        {"a", "b", "c"},
		"items.0.name": {"items", "0", "name"},
		`a\.b`:         {"a.b"},
		`a\\b`:         {`a\b`},
		`a\.b.c`:       {"a.b", "c"},
	}

	for expr, want := range ok {
		got, err := compilePath(expr)
		if err != nil {
			t.Errorf("compilePath(%q): %v", expr, err)

			continue
		}

		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("compilePath(%q) = %v, want %v", expr, got, want)
		}
	}

	for _, expr := range []string{".", "a..b", "a.", ".a", `a\`, `a\x`} {
		if _, err := compilePath(expr); !errors.Is(err, ErrPathInvalid) {
			t.Errorf("compilePath(%q) error = %v, want ErrPathInvalid", expr, err)
		}
	}
}

// TestTransforms covers every transform the schema defines.
func TestTransforms(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"trim", "  spaced  ", "spaced"},
		{"collapse_whitespace", " one\n\ttwo   three ", "one two three"},
		{"lowercase", "MiXeD", "mixed"},
		{"uppercase", "MiXeD", "MIXED"},
		{"digits", "1,204 seeders", "1204"},
		{"urldecode", "one%20two%2Fthree", "one two/three"},
		{"urldecode", "not%zz valid", "not%zz valid"},
	}

	for _, tc := range cases {
		fn, ok := transforms[tc.name]
		if !ok {
			t.Fatalf("no transform named %q", tc.name)
		}

		if got := fn(tc.in); got != tc.want {
			t.Errorf("%s(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}

	if len(transforms) != len(transformNames) {
		t.Errorf("transforms has %d entries and transformNames lists %d; the validation message would be wrong",
			len(transforms), len(transformNames))
	}

	for _, name := range transformNames {
		if _, ok := transforms[name]; !ok {
			t.Errorf("transformNames lists %q, which is not a transform", name)
		}
	}
}

// TestParseSize covers what listing pages actually print.
func TestParseSize(t *testing.T) {
	t.Parallel()

	cases := map[string]int64{
		"":                     0,
		"not stated":           0,
		"1503238553":           1503238553,
		"1.4 GiB":              1503238553,
		"1.4GB":                1503238553,
		"700 MB":               734003200,
		"1,024 KiB":            1048576,
		"1,4 GiB":              1503238553,
		"12 KiB":               12288,
		"0 B":                  0,
		"-5 MB":                0,
		"3 quatloos":           0,
		"1.2.3 GiB":            0,
		"9007199254740992 TiB": 9223372036854775807,
	}

	for in, want := range cases {
		if got := parseSize(in); got != want {
			t.Errorf("parseSize(%q) = %d, want %d", in, got, want)
		}
	}
}

// TestParseCount covers the swarm columns, including the ones that are not
// numbers.
func TestParseCount(t *testing.T) {
	t.Parallel()

	cases := map[string]int{
		"":                     0,
		"n/a":                  0,
		"42":                   42,
		"1204":                 1204,
		"  7  ":                7,
		"12 seeders":           12,
		"seeders: 12":          12,
		"99999999999999999999": maxInt,
	}

	for in, want := range cases {
		if got := parseCount(in); got != want {
			t.Errorf("parseCount(%q) = %d, want %d", in, got, want)
		}
	}
}

// TestParseTime covers the definition's own layouts winning over the
// built-in ones, and an unparseable date leaving the zero time.
func TestParseTime(t *testing.T) {
	t.Parallel()

	if got := parseTime("2026-03-04 11:20:00", nil); !got.Equal(time.Date(2026, time.March, 4, 11, 20, 0, 0, time.UTC)) {
		t.Errorf("a built-in layout did not parse: %v", got)
	}

	if got := parseTime("04/03/2026", []string{"02/01/2006"}); !got.Equal(time.Date(2026, time.March, 4, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("the definition's own layout did not parse: %v", got)
	}

	for _, in := range []string{"", "   ", "yesterday", "not a date"} {
		if got := parseTime(in, nil); !got.IsZero() {
			t.Errorf("parseTime(%q) = %v, want the zero time", in, got)
		}
	}
}

// TestWithoutQuery covers the id derivation's one reduction.
func TestWithoutQuery(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":                                   "",
		"   ":                                "",
		"https://feed.example.org/i/1?k=v#f": "https://feed.example.org/i/1",
		"an-opaque-token":                    "an-opaque-token",
		"1001":                               "1001",
	}

	for in, want := range cases {
		if got := withoutQuery(in); got != want {
			t.Errorf("withoutQuery(%q) = %q, want %q", in, got, want)
		}
	}
}
