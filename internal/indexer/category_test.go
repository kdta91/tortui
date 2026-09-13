package indexer

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

// allCategories is every bucket the taxonomy defines, read off the
// implementation rather than listed here a second time: the walk starts at the
// zero value and stops at the first value Category.String reports as unnamed
// (the category(N) sentinel). A bucket added to the enum therefore shows up in
// this slice on its own, which is what makes the closed-set tripwire below able
// to see it. Helpers must only ever return one of these, whatever they are fed.
var allCategories = walkCategories()

// walkCategories enumerates the buckets by asking Category.String where the
// named values stop. The bound is a sanity limit, not the expected size: an
// enum that somehow ran past it returns an obviously wrong slice, which
// TestCategoryBucketSetIsClosed then fails on, rather than looping forever.
func walkCategories() []Category {
	var cs []Category
	for i := range 1024 {
		c := Category(i)
		if c.String() == fmt.Sprintf("category(%d)", i) {
			break
		}
		cs = append(cs, c)
	}
	return cs
}

// categoryConstNames returns the identifiers of the Category enum's iota const
// block, in declaration order, parsed out of the package's own non-test source.
//
// This is the half of the tripwire that a String method cannot fool. Walking
// String only sees buckets someone remembered to add a case for; reading the
// declaration sees every constant of type Category the package declares, in any
// of its files, whether or not String knows about it.
//
// Anything about the declaration this cannot interpret unambiguously — a spec
// declaring several names at once, an explicit value part-way down the block, a
// Category const block that is not an iota run — fails the test rather than
// being skipped, because a shape this does not understand is a shape it cannot
// honestly claim to be guarding.
func categoryConstNames(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}

	var files []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files = append(files, name)
	}
	sort.Strings(files)

	if len(files) == 0 {
		t.Fatal("no non-test .go files found in the package directory")
	}

	fset := token.NewFileSet()

	var (
		names  []string
		blocks int
	)
	for _, name := range files {
		file, err := parser.ParseFile(fset, filepath.Clean(name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}

		for _, decl := range file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST || len(gd.Specs) == 0 {
				continue
			}

			first, ok := gd.Specs[0].(*ast.ValueSpec)
			if !ok {
				continue
			}
			if id, ok := first.Type.(*ast.Ident); !ok || id.Name != "Category" {
				continue
			}
			if len(first.Values) != 1 {
				t.Fatalf("%s: Category const block's first spec has %d values, want exactly iota", name, len(first.Values))
			}
			if id, ok := first.Values[0].(*ast.Ident); !ok || id.Name != "iota" {
				t.Fatalf("%s: Category const block is not an iota run; this test cannot derive bucket values from it", name)
			}

			blocks++
			if blocks > 1 {
				t.Fatalf("%s: a second Category iota const block was found; iota restarts per block, so bucket values can no longer be derived from declaration order", name)
			}

			for i, spec := range gd.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					t.Fatalf("%s: unexpected spec kind %T in the Category const block", name, spec)
				}
				if len(vs.Names) != 1 {
					t.Fatalf("%s: spec %d of the Category const block declares %d names, want 1", name, i, len(vs.Names))
				}
				if i > 0 && len(vs.Values) != 0 {
					t.Fatalf("%s: spec %d of the Category const block sets an explicit value, breaking the iota run", name, i)
				}
				names = append(names, vs.Names[0].Name)
			}
		}
	}

	return names
}

func isKnownCategory(c Category) bool {
	for _, known := range allCategories {
		if c == known {
			return true
		}
	}
	return false
}

// The zero value is the contract AGENT.md §13 relies on: anything an adapter
// never classified, and anything it could not classify, reads as "other".
func TestCategoryZeroValueIsOther(t *testing.T) {
	var c Category
	if c != CategoryOther {
		t.Errorf("zero Category = %d, want CategoryOther (%d)", int(c), int(CategoryOther))
	}

	var r Result
	if r.Category != CategoryOther {
		t.Errorf("zero Result.Category = %d, want CategoryOther (%d)", int(r.Category), int(CategoryOther))
	}
}

// The buckets are distinct values, so a filter on one never matches another.
func TestCategoryValuesAreDistinct(t *testing.T) {
	seen := map[Category]bool{}
	for _, c := range allCategories {
		if seen[c] {
			t.Errorf("category value %d is used by more than one bucket", int(c))
		}
		seen[c] = true
	}
}

func TestCategoryString(t *testing.T) {
	tests := []struct {
		c    Category
		want string
	}{
		{CategoryOther, "other"},
		{CategoryAudio, "audio"},
		{CategoryVideo, "video"},
		{CategoryImage, "image"},
		{CategoryText, "text"},
		{CategorySoftware, "software"},
		{CategoryData, "data"},
		// Out of range renders diagnosably rather than masquerading as a
		// known bucket, mirroring Trust.String.
		{Category(99), "category(99)"},
		{Category(-1), "category(-1)"},
	}
	for _, tt := range tests {
		if got := tt.c.String(); got != tt.want {
			t.Errorf("Category(%d).String() = %q, want %q", int(tt.c), got, tt.want)
		}
	}
}

// Every bucket's String token parses back to that bucket, so a category
// written to a log line, a cache key, or a config file survives a round trip.
func TestCategoryStringRoundTrips(t *testing.T) {
	for _, c := range allCategories {
		if got := CategoryFromString(c.String()); got != c {
			t.Errorf("CategoryFromString(%q) = %v, want %v", c.String(), got, c)
		}
	}
}

func TestCategoryFromTorznab(t *testing.T) {
	tests := []struct {
		name string
		id   int
		want Category
	}{
		// Block boundaries. Block numbers are from the newznab predefined
		// category ranges; see the citation on torznabBlocks.
		{"block 0 lower", 0, CategoryOther},
		{"block 0 upper", 999, CategoryOther},
		{"block 1 lower", 1000, CategorySoftware},
		{"block 1 sub", 1180, CategorySoftware},
		{"block 1 upper", 1999, CategorySoftware},
		{"block 2 lower", 2000, CategoryVideo},
		{"block 2 sub", 2040, CategoryVideo},
		{"block 2 upper", 2999, CategoryVideo},
		{"block 3 lower", 3000, CategoryAudio},
		{"block 3 sub", 3020, CategoryAudio},
		{"block 3 upper", 3999, CategoryAudio},
		{"block 4 lower", 4000, CategorySoftware},
		{"block 4 sub", 4020, CategorySoftware},
		{"block 4 upper", 4999, CategorySoftware},
		{"block 5 lower", 5000, CategoryVideo},
		{"block 5 sub", 5040, CategoryVideo},
		{"block 5 upper", 5999, CategoryVideo},
		{"block 6 lower", 6000, CategoryVideo},
		{"block 6 upper", 6999, CategoryVideo},
		{"block 7 lower", 7000, CategoryText},
		{"block 7 sub", 7020, CategoryText},
		{"block 7 upper", 7999, CategoryText},
		// 8000 is "Other" in Jackett's numbering and "reserved" in the
		// nZEDb spec; either way it is nothing this taxonomy names.
		{"block 8 lower", 8000, CategoryOther},
		{"block 8 sub", 8010, CategoryOther},
		{"block 8 upper", 8999, CategoryOther},
		// Unmapped, reserved, and site-specific ranges.
		{"block 9", 9000, CategoryOther},
		{"reserved upper", 99999, CategoryOther},
		{"custom range lower", 100000, CategoryOther},
		{"custom range", 138604, CategoryOther},
		// Hostile input: negatives, huge values.
		{"negative small", -1, CategoryOther},
		{"negative block", -2000, CategoryOther},
		{"max int", math.MaxInt, CategoryOther},
		{"min int", math.MinInt, CategoryOther},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CategoryFromTorznab(tt.id)
			if got != tt.want {
				t.Errorf("CategoryFromTorznab(%d) = %v, want %v", tt.id, got, tt.want)
			}
			if !isKnownCategory(got) {
				t.Errorf("CategoryFromTorznab(%d) = %d, which is not a defined bucket", tt.id, int(got))
			}
		})
	}
}

// Every id in a wide sweep produces a defined bucket and never panics: the
// mapping is total, which is what "never dropped or errored" means for a
// helper that returns no error.
func TestCategoryFromTorznabIsTotal(t *testing.T) {
	for id := -20000; id <= 120000; id++ {
		if got := CategoryFromTorznab(id); !isKnownCategory(got) {
			t.Fatalf("CategoryFromTorznab(%d) = %d, which is not a defined bucket", id, int(got))
		}
	}
}

func TestCategoryFromString(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Category
	}{
		// Canonical tokens.
		{"canonical other", "other", CategoryOther},
		{"canonical audio", "audio", CategoryAudio},
		{"canonical video", "video", CategoryVideo},
		{"canonical image", "image", CategoryImage},
		{"canonical text", "text", CategoryText},
		{"canonical software", "software", CategorySoftware},
		{"canonical data", "data", CategoryData},
		// Case is irrelevant.
		{"upper", "AUDIO", CategoryAudio},
		{"mixed", "SoFtWaRe", CategorySoftware},
		{"title case", "Video", CategoryVideo},
		// Surrounding whitespace and separators are irrelevant.
		{"padded", "   audio\t\n", CategoryAudio},
		{"path form", "Movies/HD", CategoryVideo},
		{"path form spaced", "PC / Games", CategorySoftware},
		{"arrow form", "TV > Documentary", CategoryVideo},
		{"underscored", "audio_books", CategoryAudio},
		{"bracketed", "[Books]", CategoryText},
		// First recognised token wins, left to right.
		{"first token wins", "Audio/Video", CategoryAudio},
		{"leading noise", "zzz qqq music", CategoryAudio},
		// A sample of the words real source dialects use.
		{"movies", "Movies", CategoryVideo},
		{"film", "Film", CategoryVideo},
		{"music", "Music", CategoryAudio},
		{"sound", "Sound", CategoryAudio},
		{"photos", "Photos", CategoryImage},
		{"pictures", "Pictures", CategoryImage},
		{"ebooks", "eBooks", CategoryText},
		{"documents", "Documents", CategoryText},
		{"apps", "Apps", CategorySoftware},
		{"games", "Games", CategorySoftware},
		{"iso", "ISO", CategorySoftware},
		{"console", "Console", CategorySoftware},
		{"dataset", "Dataset", CategoryData},
		{"database", "Database dump", CategoryData},
		{"misc", "Misc", CategoryOther},
		{"unknown word", "Unknown", CategoryOther},
		// Unrecognised input of every shape maps to Other, not an error.
		{"empty", "", CategoryOther},
		{"whitespace only", " \t \n ", CategoryOther},
		{"punctuation only", "!!! ---- ///", CategoryOther},
		{"digits", "5030", CategoryOther},
		{"nonsense", "qwertyuiop", CategoryOther},
		{"non-ascii", "Ελληνικά", CategoryOther},
		{"cjk", "音楽", CategoryOther},
		{"emoji", "🎧🎬", CategoryOther},
		{"combining marks", "áú", CategoryOther},
		{"nul byte", "audio\x00", CategoryAudio},
		{"only nul", "\x00", CategoryOther},
		{"substring is not a token", "audiophile", CategoryOther},
		{"token inside word", "myvideos", CategoryOther},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CategoryFromString(tt.in)
			if got != tt.want {
				t.Errorf("CategoryFromString(%q) = %v, want %v", tt.in, got, tt.want)
			}
			if !isKnownCategory(got) {
				t.Errorf("CategoryFromString(%q) = %d, which is not a defined bucket", tt.in, int(got))
			}
		})
	}
}

// A very long, hostile label must still return a bucket rather than blowing
// up: source-supplied category strings are attacker-influenced text.
func TestCategoryFromStringLongInput(t *testing.T) {
	long := strings.Repeat("x/", 100000) + "audio"
	if got := CategoryFromString(long); got != CategoryAudio {
		t.Errorf("CategoryFromString(long) = %v, want CategoryAudio", got)
	}

	noMatch := strings.Repeat("\xff\xfe", 50000)
	if got := CategoryFromString(noMatch); got != CategoryOther {
		t.Errorf("CategoryFromString(invalid utf-8) = %v, want CategoryOther", got)
	}
	if utf8.ValidString(noMatch) {
		t.Fatal("test bug: noMatch was supposed to be invalid UTF-8")
	}
}

// FuzzCategoryFromString pins the two properties the acceptance criteria name
// for arbitrary adapter strings: the helper never panics, and it always
// returns a defined bucket.
func FuzzCategoryFromString(f *testing.F) {
	seeds := []string{
		"", " ", "audio", "AUDIO", "Movies/HD", "5030", "🎬", "\x00",
		"Ελληνικά", "audio\xff", strings.Repeat("a", 4096), "-1", "other",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if got := CategoryFromString(s); !isKnownCategory(got) {
			t.Errorf("CategoryFromString(%q) = %d, which is not a defined bucket", s, int(got))
		}
	})
}

// FuzzCategoryFromTorznab is the same property for numeric ids.
func FuzzCategoryFromTorznab(f *testing.F) {
	for _, id := range []int{0, 1000, 5030, 8000, 100000, -1, math.MaxInt, math.MinInt} {
		f.Add(id)
	}
	f.Fuzz(func(t *testing.T, id int) {
		if got := CategoryFromTorznab(id); !isKnownCategory(got) {
			t.Errorf("CategoryFromTorznab(%d) = %d, which is not a defined bucket", id, int(got))
		}
	})
}

// The bucket set is closed, and this test is the tripwire on it: adding,
// removing, or renaming a bucket fails here until someone updates the two lists
// below on purpose.
//
// That is the point. Every bucket must name a kind of DATA — audio, video,
// still images, text, software, datasets. A bucket that names subject matter
// instead (a genre, a medium, a scene tag, a kind of material) is the
// content-specific categorisation AGENT.md §2 forbids and §16 explains, and it
// must not reach the enum by accident.
//
// Both sides of every comparison here come off the implementation, never off a
// second copy of the bucket list kept in this file: wantNames and wantTokens
// are the only hand-maintained lists, and a change to category.go alone moves
// the other side. There are three checks because no single one of them sees
// every way a bucket can arrive:
//
//   - the declared constants (parsed out of the package source) catch a bucket
//     added to the enum whether or not String has a case for it;
//   - the tokens those constants render as catch a bucket whose name is fine
//     but whose String token is not;
//   - the String walk that produces allCategories, compared against the
//     declaration, catches a String case written for a value the enum never
//     declared.
func TestCategoryBucketSetIsClosed(t *testing.T) {
	wantNames := []string{
		"CategoryOther",
		"CategoryAudio",
		"CategoryVideo",
		"CategoryImage",
		"CategoryText",
		"CategorySoftware",
		"CategoryData",
	}
	wantTokens := []string{"other", "audio", "video", "image", "text", "software", "data"}

	const rule = "\nif this is a deliberate change: a bucket must name a kind of data, " +
		"never subject matter (AGENT.md §2, §16)"

	gotNames := categoryConstNames(t)
	if strings.Join(gotNames, ",") != strings.Join(wantNames, ",") {
		t.Errorf("Category constants declared in the package source = [%s], want [%s]%s",
			strings.Join(gotNames, ", "), strings.Join(wantNames, ", "), rule)
	}

	gotTokens := make([]string, len(gotNames))
	for i := range gotNames {
		gotTokens[i] = Category(i).String()
	}
	if strings.Join(gotTokens, ",") != strings.Join(wantTokens, ",") {
		t.Errorf("bucket set = [%s], want [%s]%s",
			strings.Join(gotTokens, ", "), strings.Join(wantTokens, ", "), rule)
	}

	if len(allCategories) != len(gotNames) {
		t.Errorf("Category.String names %d consecutive values but the enum declares %d constants; "+
			"the String switch and the const block disagree about what the buckets are%s",
			len(allCategories), len(gotNames), rule)
	}
}
