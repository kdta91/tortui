package indexer

import (
	"fmt"
	"strings"
	"unicode"
)

// Category is tortui's internal classification bucket for a result.
//
// The taxonomy classifies the **kind of data** a torrent carries — audio,
// video, still images, text, software, datasets — and never its subject
// matter. That is not a stylistic choice: AGENT.md §2 bars content-specific
// logic outright and §16 explains why, so there is no bucket here for a genre,
// a medium, a scene tag, or anything else that would say what kind of material
// tortui is for. Sources that do classify by subject matter have their
// categories collapsed into these buckets on the way in, which is the whole
// point of having an internal enum instead of passing a source's own labels
// through.
//
// The set is deliberately small. It exists so the search screen can offer a
// coarse filter and the results table a one-word column, not so that anyone can
// reconstruct a source's own taxonomy from it. Adapters map onto it inside the
// adapter package, never in the TUI, and an unrecognised category becomes
// CategoryOther rather than being dropped (AGENT.md §13).
//
// CategoryOther is the zero value, so a Result that no adapter classified, and
// a result whose category nothing here recognised, are the same thing to every
// consumer: unclassified, still shown, never lost.
type Category int

const (
	// CategoryOther is the zero value: the bucket for anything
	// unclassified, unrecognised, or that fits nowhere else. It is never
	// an error and never a reason to drop a result.
	CategoryOther Category = iota

	// CategoryAudio is audio data — recordings, speech, music, anything
	// whose payload is sound.
	CategoryAudio

	// CategoryVideo is moving-image data, with or without sound.
	CategoryVideo

	// CategoryImage is still-image data. A disk image (an .iso and
	// friends) is CategorySoftware, not this — the shared English word is
	// a coincidence.
	CategoryImage

	// CategoryText is text and documents — anything whose payload is
	// written language rather than a rendering of it.
	CategoryText

	// CategorySoftware is executable software and the things distributed
	// as software: applications, games, operating system and disk images.
	CategorySoftware

	// CategoryData is structured data published as data — research
	// datasets, corpora, database dumps, archival exports. Distinct from
	// CategoryText because the bundled lawful sources (AGENT.md §2) are
	// largely dataset repositories and archives, for which "text" would be
	// wrong and "other" would be useless.
	CategoryData
)

// String returns a lowercase, stable token for the bucket: "other", "audio",
// "video", "image", "text", "software", or "data". These are the identifier
// forms — used in log lines, cache keys, test output, and anywhere a category
// is written down or parsed back with CategoryFromString — not a human-facing
// rendering. An out-of-range value renders as category(N) rather than
// panicking or masquerading as a known bucket, mirroring Trust.String.
func (c Category) String() string {
	switch c {
	case CategoryOther:
		return "other"
	case CategoryAudio:
		return "audio"
	case CategoryVideo:
		return "video"
	case CategoryImage:
		return "image"
	case CategoryText:
		return "text"
	case CategorySoftware:
		return "software"
	case CategoryData:
		return "data"
	default:
		return fmt.Sprintf("category(%d)", int(c))
	}
}

// torznabBlocks maps a Torznab/Newznab category block — the id divided by
// 1000 — onto a bucket.
//
// Torznab ids are allocated in blocks of 1000, with sub-categories inside each
// block, so the block alone determines the kind of data and the sub-id only
// refines a source's own subject-matter labelling, which tortui has no use for.
// The block numbers were read off two sources on 2026-09-13 rather than
// recalled:
//
//   - the newznab API specification, section 3 "Predefined Categories", at
//     https://raw.githubusercontent.com/nZEDb/nZEDb/dev/docs/newznab_api_specification.txt
//     which gives the ranges 0000-0999, 1000-1999, ..., 7000-7999, then
//     8000-99999 reserved and 100000+ site-specific custom;
//   - Jackett's TorznabCatType.cs, at
//     https://raw.githubusercontent.com/Jackett/Jackett/master/src/Jackett.Common/Models/TorznabCatType.cs
//     which is the numbering most Torznab servers actually emit. It agrees on
//     blocks 1-7 and additionally uses 8000 as its catch-all, which the
//     specification leaves reserved.
//
// Only the block numbers are reproduced here. The names those two sources give
// the blocks are subject-matter labels, and copying them into this repository
// would be exactly the content-specific categorisation AGENT.md §2 forbids —
// so each block is written down as the kind of data its items are, and nothing
// else. Blocks 0 and 8 both land on CategoryOther, which covers both
// conventions; every other block, including the reserved and custom ranges,
// falls through to CategoryOther as well.
var torznabBlocks = map[int]Category{
	0: CategoryOther,
	1: CategorySoftware,
	2: CategoryVideo,
	3: CategoryAudio,
	4: CategorySoftware,
	5: CategoryVideo,
	6: CategoryVideo,
	7: CategoryText,
	8: CategoryOther,
}

// CategoryFromTorznab maps a Torznab/Newznab numeric category id onto a
// bucket, by the 1000-block the id sits in (see torznabBlocks for where those
// blocks were verified).
//
// It is total and cannot fail: any id it does not recognise — a reserved
// block, a site-specific custom id, zero, a negative, or a value from a server
// that invented its own numbering — returns CategoryOther. Nothing is dropped
// and nothing is reported as an error, per AGENT.md §13, because a result with
// a category tortui cannot place is still a result the user asked for.
//
// Call it in the adapter, never in the TUI.
func CategoryFromTorznab(id int) Category {
	if c, ok := torznabBlocks[id/1000]; ok {
		return c
	}
	return CategoryOther
}

// categoryWords maps a single lowercase word to a bucket, for
// CategoryFromString.
//
// Two kinds of word are in here: the canonical tokens Category.String returns,
// so a category survives a write-and-parse round trip, and the everyday words
// sources actually label things with. Some of the second kind name subject
// matter, because that is what the sources write — but every one of them
// collapses into a data-kind bucket, which is the opposite of adopting a
// source's taxonomy: it is how a subject-matter label gets discarded at the
// boundary. The list stays short and generic on purpose. It is not a place to
// accumulate genre words, format names, or scene tags, and a word that only
// makes sense for one kind of material does not belong in it (AGENT.md §2).
var categoryWords = map[string]Category{
	"other":         CategoryOther,
	"misc":          CategoryOther,
	"miscellaneous": CategoryOther,
	"unknown":       CategoryOther,
	"uncategorized": CategoryOther,
	"uncategorised": CategoryOther,

	"audio": CategoryAudio,
	"sound": CategoryAudio,
	"music": CategoryAudio,

	"video":  CategoryVideo,
	"videos": CategoryVideo,
	"movie":  CategoryVideo,
	"movies": CategoryVideo,
	"film":   CategoryVideo,
	"films":  CategoryVideo,
	"tv":     CategoryVideo,

	"image":    CategoryImage,
	"images":   CategoryImage,
	"picture":  CategoryImage,
	"pictures": CategoryImage,
	"photo":    CategoryImage,
	"photos":   CategoryImage,

	"text":      CategoryText,
	"texts":     CategoryText,
	"doc":       CategoryText,
	"docs":      CategoryText,
	"document":  CategoryText,
	"documents": CategoryText,
	"book":      CategoryText,
	"books":     CategoryText,
	"ebook":     CategoryText,
	"ebooks":    CategoryText,

	"software":     CategorySoftware,
	"app":          CategorySoftware,
	"apps":         CategorySoftware,
	"application":  CategorySoftware,
	"applications": CategorySoftware,
	"program":      CategorySoftware,
	"programs":     CategorySoftware,
	"game":         CategorySoftware,
	"games":        CategorySoftware,
	"console":      CategorySoftware,
	"pc":           CategorySoftware,
	"iso":          CategorySoftware,

	"data":     CategoryData,
	"dataset":  CategoryData,
	"datasets": CategoryData,
	"database": CategoryData,
}

// CategoryFromString maps an arbitrary category label supplied by a source
// onto a bucket. It is the string counterpart of CategoryFromTorznab, for
// adapters — the YAML-driven scraper above all — whose sources publish words
// rather than numbers.
//
// The label is lowercased and split on everything that is not a letter or a
// digit, so "Movies/HD", "PC > Games", "  audio  ", and "[Books]" all work,
// and the first token the taxonomy recognises wins. Nothing else is inferred:
// a word that merely contains a known token ("audiophile") does not match, and
// a numeric label is not routed to CategoryFromTorznab — an adapter that has
// numeric ids should call that helper directly rather than stringifying them.
//
// It is total and cannot fail. An empty, whitespace-only, non-ASCII, invalid
// UTF-8, or simply unrecognised label returns CategoryOther. As with the
// numeric helper, an unplaceable category is never a dropped result and never
// an error (AGENT.md §13).
//
// Call it in the adapter, never in the TUI.
func CategoryFromString(s string) Category {
	tokens := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, tok := range tokens {
		if c, ok := categoryWords[tok]; ok {
			return c
		}
	}
	return CategoryOther
}
