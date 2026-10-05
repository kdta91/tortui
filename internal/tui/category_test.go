package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/indexer"
	"github.com/kdta91/tortui/internal/tui/theme"
)

var allCategories = []indexer.Category{
	indexer.CategoryOther, indexer.CategoryAudio, indexer.CategoryVideo, indexer.CategoryImage,
	indexer.CategoryText, indexer.CategorySoftware, indexer.CategoryData,
}

// TestCategoryGlyphSetCoversEveryBucketInEnumOrder pins the order the theme
// package's Category array relies on (it cannot import indexer): the array is
// as long as the bucket list, every glyph is one cell wide in both sets, and
// no two buckets share a glyph.
func TestCategoryGlyphSetCoversEveryBucketInEnumOrder(t *testing.T) {
	for i, c := range allCategories {
		if int(c) != i {
			t.Fatalf("bucket %v has value %d, want %d: the glyph array order is stale", c, int(c), i)
		}
	}

	if c := indexer.Category(len(allCategories)); !strings.HasPrefix(c.String(), "category(") {
		t.Fatalf("a bucket exists beyond the seven glyphs: %v", c)
	}

	for name, set := range map[string]theme.GlyphSet{"unicode": theme.UnicodeGlyphs, "ascii": theme.ASCIIGlyphs} {
		seen := map[string]indexer.Category{}

		for _, c := range allCategories {
			g := categoryGlyph(set, c)
			if w := theme.Width(g); w != 1 {
				t.Errorf("%s glyph for %v = %q has width %d, want 1", name, c, g, w)
			}

			if prev, dup := seen[g]; dup {
				t.Errorf("%s glyph %q is used for both %v and %v", name, g, prev, c)
			}

			seen[g] = c
		}
	}
}

func TestCategoryGlyphUnicodeAndASCII(t *testing.T) {
	if got := categoryGlyph(theme.UnicodeGlyphs, indexer.CategoryAudio); got != "♪" {
		t.Errorf("unicode audio = %q", got)
	}

	if got := categoryGlyph(theme.ASCIIGlyphs, indexer.CategoryAudio); got != "A" {
		t.Errorf("ascii audio = %q", got)
	}

	for _, set := range []theme.GlyphSet{theme.UnicodeGlyphs, theme.ASCIIGlyphs} {
		want := set.Category[0]
		for _, bad := range []indexer.Category{-1, 99} {
			if got := categoryGlyph(set, bad); got != want {
				t.Errorf("out-of-range category %d = %q, want the Other glyph %q", bad, got, want)
			}
		}
	}

	for _, g := range theme.ASCIIGlyphs.Category {
		for _, r := range g {
			if r > 0x7e {
				t.Errorf("ascii glyph %q is not plain ASCII", g)
			}
		}
	}
}

func TestResultRowCarriesTheCategoryGlyph(t *testing.T) {
	now := time.Now()

	for _, set := range []theme.GlyphSet{theme.UnicodeGlyphs, theme.ASCIIGlyphs} {
		for _, c := range allCategories {
			row := resultRow(indexer.Result{IndexerID: "a", ID: "1", Title: "t", Category: c}, now, set)
			if row.Cells[colCategory] != set.Category[c] {
				t.Errorf("category %v cell = %q, want %q", c, row.Cells[colCategory], set.Category[c])
			}
		}
	}
}

func TestCategoryColumnSortsByBucketOrder(t *testing.T) {
	now := time.Now()
	results := []indexer.Result{
		{IndexerID: "a", ID: "d", Title: "d", Category: indexer.CategoryData},
		{IndexerID: "a", ID: "o", Title: "o", Category: indexer.CategoryOther},
		{IndexerID: "a", ID: "v", Title: "v", Category: indexer.CategoryVideo},
	}

	m := newResultsModel().setResults(results, indexer.ModeSearch, now, theme.UnicodeGlyphs).sortAscBy(colCategory)

	var got []string
	for _, r := range m.table.Rows() {
		got = append(got, r.ID)
	}

	if strings.Join(got, ",") != "a|o,a|v,a|d" {
		t.Fatalf("order by category = %v, want other, video, data", got)
	}
}

// TestCategoryColumnIsTheFirstToBeDropped pins the drop order: the glyph goes
// before Source, Age and Trust, so narrowing never costs a documented column
// to keep it, and everything else lays out as it did before the column existed.
func TestCategoryColumnIsTheFirstToBeDropped(t *testing.T) {
	now := time.Now()
	results := []indexer.Result{{
		IndexerID: "alpha", ID: "1", Title: "some-long-title-for-the-table", Category: indexer.CategoryAudio,
		Trust: indexer.TrustVIP, Published: now.Add(-time.Hour), SizeBytes: 2048, Seeders: 4,
	}}
	m := newResultsModel().setResults(results, indexer.ModeSearch, now, theme.UnicodeGlyphs)
	th := testTheme()

	cases := []struct {
		width                    int
		glyph, source, age, trst bool
	}{
		{80, true, true, true, true},
		{72, true, true, true, true},   // the narrowest width that holds all seven
		{71, false, true, true, true},  // glyph goes first
		{69, false, false, true, true}, // then Source, as before
		{53, false, false, true, true},
		{52, false, false, false, true},
	}

	for _, c := range cases {
		out := m.table.View(c.width, 6, th)
		for _, chk := range []struct {
			name string
			text string
			want bool
		}{
			{"glyph", "♪", c.glyph}, {"Source", "Source", c.source}, {"Age", "Age", c.age}, {"Trust", "Trust", c.trst},
		} {
			if got := strings.Contains(out, chk.text); got != chk.want {
				t.Errorf("width %d: %s present = %v, want %v; got:\n%s", c.width, chk.name, got, chk.want, out)
			}
		}
	}
}

func TestDetailsShowsGlyphAndWord(t *testing.T) {
	for _, tc := range []struct {
		name string
		cap  theme.Capability
		want string
	}{
		{"unicode", theme.Capability{Color: theme.ColorNone, Unicode: true}, "⌘ software"},
		{"ascii", theme.Capability{Color: theme.ColorNone, Unicode: false}, "S software"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(newTestEngine(t), theme.New(theme.DefaultThemeName, tc.cap))
			m.width = 80
			m.details = m.details.withResult(indexer.Result{IndexerID: "a", ID: "1", Title: "x", Category: indexer.CategorySoftware})

			if got := m.renderDetailsScreen(); !strings.Contains(got, tc.want) {
				t.Errorf("details missing %q; got:\n%s", tc.want, got)
			}
		})
	}
}

func TestDetailsShowsTheOtherGlyphForUnclassified(t *testing.T) {
	m := New(newTestEngine(t), testTheme())
	m.width = 80
	m.details = m.details.withResult(indexer.Result{IndexerID: "a", ID: "1", Title: "x"})

	if got := m.renderDetailsScreen(); !strings.Contains(got, "· other") {
		t.Errorf("details missing the Other glyph and word; got:\n%s", got)
	}
}
