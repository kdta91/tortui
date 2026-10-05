package torznab

import (
	"strings"
	"testing"

	"github.com/kdta91/tortui/internal/indexer"
)

const sourceCategoryCaps = `<?xml version="1.0"?>
<caps>
  <limits max="100" default="50"/>
  <searching><search available="yes" supportedParams="q"/></searching>
  <categories>
    <category id="4000" name="Apps">
      <subcat id="4010" name="PC/Mac"/>
      <subcat id="4020" name="Phone/Android"/>
      <subcat id="4030" name="  "/>
    </category>
    <category id="3000" name="Audio"/>
    <category id="8000" name="Misc"/>
  </categories>
</caps>`

func sourceCategoryItem(cats ...string) string {
	var b strings.Builder

	b.WriteString(`<item><title>Invented release</title><guid>https://feed.example.org/d/1</guid>`)
	b.WriteString(`<torznab:attr name="infohash" value="0123456789abcdef0123456789abcdef01234567"/>`)

	for _, c := range cats {
		b.WriteString(`<torznab:attr name="category" value="` + c + `"/>`)
	}

	b.WriteString(`</item>`)

	return b.String()
}

func searchSourceCategory(t *testing.T, caps string, items ...string) []indexer.Result {
	t.Helper()

	feed := `<rss version="2.0"><channel><title>x</title>` + strings.Join(items, "") + `</channel></rss>`

	src := newSource(t, map[string]reply{
		functionCaps:   xmlReply(caps),
		functionSearch: xmlReply(feed),
	})

	a := mustDiscover(t, src)

	got, err := a.Search(testContext(t), indexer.Query{Text: "x"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	return got
}

func TestSourceCategoryFromCapsNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cats []string
		want string
	}{
		{"nested subcategory beats its parent, parent listed first", []string{"4000", "4010"}, "PC/Mac"},
		{"nested subcategory beats its parent, parent listed last", []string{"4020", "4000"}, "Phone/Android"},
		{"top-level id alone is named", []string{"3000"}, "Audio"},
		{"first subcategory wins", []string{"4020", "4010"}, "Phone/Android"},
		{"id the caps do not name is empty", []string{"9999"}, ""},
		{"unnamed subcategory falls back to the named parent", []string{"4000", "4030"}, "Apps"},
		{"unknown id is skipped for a named one", []string{"9999", "3000"}, "Audio"},
		{"no category at all", nil, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := searchSourceCategory(t, sourceCategoryCaps, sourceCategoryItem(tc.cats...))
			if len(got) != 1 {
				t.Fatalf("got %d results, want 1", len(got))
			}

			if got[0].SourceCategory != tc.want {
				t.Errorf("SourceCategory = %q, want %q", got[0].SourceCategory, tc.want)
			}
		})
	}
}

func TestSourceCategoryEmptyWithoutCapsNames(t *testing.T) {
	t.Parallel()

	caps := `<caps><searching><search available="yes"/></searching><categories><category id="4000"/></categories></caps>`

	got := searchSourceCategory(t, caps, sourceCategoryItem("4000"))
	if got[0].SourceCategory != "" {
		t.Errorf("SourceCategory = %q, want empty when caps name nothing", got[0].SourceCategory)
	}
}

func TestSourceCategoryNameIsSanitised(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("W", 500)
	caps := `<caps><searching><search available="yes"/></searching><categories>` +
		`<category id="1000" name="PC&#x202e;/&#x200b;Mac&#xfeff;&#x9; x"/>` +
		`<category id="2000" name="` + long + `"/>` +
		`<category id="3000" name="&#x202e;&#x200b;&#xfeff;"/>` +
		`</categories></caps>`

	tests := []struct {
		id   string
		want string
	}{
		{"1000", "PC/Mac x"},
		{"2000", strings.Repeat("W", indexer.MaxSourceCategoryRunes)},
		{"3000", ""},
	}

	for _, tc := range tests {
		got := searchSourceCategory(t, caps, sourceCategoryItem(tc.id))
		if got[0].SourceCategory != tc.want {
			t.Errorf("id %s: SourceCategory = %q, want %q", tc.id, got[0].SourceCategory, tc.want)
		}
	}
}
