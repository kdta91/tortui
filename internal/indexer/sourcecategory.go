package indexer

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxSourceCategoryRunes is the longest SourceCategory CleanSourceCategory
// returns, in runes. A real category name is a few words; the cap bounds what
// a hostile source can put in memory and on the details screen.
const MaxSourceCategoryRunes = 40

// CleanSourceCategory makes a source's own category name safe to keep and to
// show (DEC-174). It drops control runes (so an ANSI escape loses its ESC
// byte), Unicode format runes (bidi controls, zero-width characters, byte
// order marks), variation selectors and the other default-ignorable runes,
// and invalid UTF-8; turns every other run of white space, including line
// separators, into one space; trims; and cuts the result to
// MaxSourceCategoryRunes runes. A name with nothing left is "". It never
// interprets the text.
func CleanSourceCategory(s string) string {
	s = strings.ToValidUTF8(s, "")

	var b strings.Builder

	space := false

	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			space = true

			continue
		case unicode.IsControl(r),
			unicode.In(r, unicode.Cf, unicode.Variation_Selector, unicode.Other_Default_Ignorable_Code_Point):
			continue
		}

		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}

		space = false

		b.WriteRune(r)

		if utf8.RuneCountInString(b.String()) >= MaxSourceCategoryRunes {
			break
		}
	}

	return strings.TrimSpace(truncateRunes(b.String(), MaxSourceCategoryRunes))
}

// truncateRunes returns s cut to at most n runes.
func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}

	return string([]rune(s)[:n])
}
