package torznab

import (
	"encoding/xml"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/indexer"
)

// parseFixtureFeed decodes a feed fixture the way Search does.
func parseFixtureFeed(t *testing.T, name string) []indexer.Result {
	t.Helper()

	var doc feedDocument

	if err := decodeDocument([]byte(fixture(t, name)), rootFeed, &doc); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}

	results := make([]indexer.Result, 0, len(doc.Channel.Items))
	for _, it := range doc.Channel.Items {
		results = append(results, resultFrom(testID, it))
	}

	return results
}

func TestFeedMapsEveryAttributeItDocuments(t *testing.T) {
	t.Parallel()

	results := parseFixtureFeed(t, "search-full.xml")
	if len(results) != 2 {
		t.Fatalf("parsed %d results, want 2", len(results))
	}

	const hash = "0123456789abcdef0123456789abcdef01234567"

	first := results[0]

	want := indexer.Result{
		IndexerID: testID,
		ID:        hash,
		Title:     "Invented Reference Corpus 2026",
		InfoHash:  hash,
		Magnet:    "magnet:?xt=urn:btih:" + hash + "&dn=Invented+Reference+Corpus+2026",
		SizeBytes: 1073741824,
		Seeders:   42,
		// peers (55) is the total, so leechers is the difference.
		Leechers:  13,
		Category:  indexer.CategoryText,
		Published: time.Date(2026, time.September, 7, 12, 34, 56, 0, time.UTC),
		Uploader:  "fixture-uploader",
		Trust:     indexer.TrustVIP,
	}

	if first.IndexerID != want.IndexerID || first.ID != want.ID || first.Title != want.Title {
		t.Errorf("identity = %q/%q/%q, want %q/%q/%q",
			first.IndexerID, first.ID, first.Title, want.IndexerID, want.ID, want.Title)
	}

	if first.InfoHash != want.InfoHash {
		t.Errorf("InfoHash = %q, want %q (the uppercase attribute is normalised)", first.InfoHash, want.InfoHash)
	}

	if first.Magnet != want.Magnet {
		t.Errorf("Magnet = %q, want %q", first.Magnet, want.Magnet)
	}

	if first.SizeBytes != want.SizeBytes {
		t.Errorf("SizeBytes = %d, want %d", first.SizeBytes, want.SizeBytes)
	}

	if first.Seeders != want.Seeders || first.Leechers != want.Leechers {
		t.Errorf("swarm = %d/%d seeders/leechers, want %d/%d",
			first.Seeders, first.Leechers, want.Seeders, want.Leechers)
	}

	if first.Category != want.Category {
		t.Errorf("Category = %s, want %s", first.Category, want.Category)
	}

	if !first.Published.Equal(want.Published) {
		t.Errorf("Published = %s, want %s", first.Published, want.Published)
	}

	if first.Uploader != want.Uploader || first.Trust != want.Trust {
		t.Errorf("uploader/trust = %q/%s, want %q/%s",
			first.Uploader, first.Trust, want.Uploader, want.Trust)
	}

	if !strings.HasSuffix(first.TorrentURL, "/download/1001.torrent?apikey=redacted-in-fixture") {
		t.Errorf("TorrentURL = %q, want the enclosure address with its query intact", first.TorrentURL)
	}

	if !strings.HasSuffix(first.SourceURL, "/details/1001") {
		t.Errorf("SourceURL = %q, want the <comments> page", first.SourceURL)
	}

	if err := first.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}

	wantExtra := map[string]string{
		"torznab.grabs":                "17",
		"torznab.files":                "3",
		"torznab.downloadvolumefactor": "0",
		"torznab.uploadvolumefactor":   "1",
	}

	if len(first.Extra) != len(wantExtra) {
		t.Errorf("Extra = %v, want %v", first.Extra, wantExtra)
	}

	for key, value := range wantExtra {
		if first.Extra[key] != value {
			t.Errorf("Extra[%q] = %q, want %q", key, first.Extra[key], value)
		}
	}

	second := results[1]

	if second.ID != "fixture-guid-1002" {
		t.Errorf("second ID = %q, want the guid: there is no infohash to prefer", second.ID)
	}

	if second.Magnet != "" {
		t.Errorf("second Magnet = %q, want empty: the item published none", second.Magnet)
	}

	if second.SizeBytes != 524288000 {
		t.Errorf("second SizeBytes = %d, want the enclosure length 524288000", second.SizeBytes)
	}

	if second.Seeders != 7 || second.Leechers != 0 {
		t.Errorf("second swarm = %d/%d, want 7/0: peers equals seeders, so no leechers",
			second.Seeders, second.Leechers)
	}

	if second.Category != indexer.CategoryAudio {
		t.Errorf("second Category = %s, want audio", second.Category)
	}

	if !second.Published.Equal(time.Date(2026, time.September, 8, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("second Published = %s, want the RFC 1123 GMT date", second.Published)
	}

	if second.Uploader != "second-fixture-uploader" {
		t.Errorf("second Uploader = %q, want the poster attribute", second.Uploader)
	}

	if second.Trust != indexer.TrustVerified {
		t.Errorf("second Trust = %s, want verified", second.Trust)
	}

	if second.Extra != nil {
		t.Errorf("second Extra = %v, want nil: it published nothing worth carrying", second.Extra)
	}
}

func TestFeedSurvivesEveryMalformedItem(t *testing.T) {
	t.Parallel()

	results := parseFixtureFeed(t, "search-messy.xml")
	if len(results) != 7 {
		t.Fatalf("parsed %d results, want 7: no item may be dropped", len(results))
	}

	byTitle := make(map[string]indexer.Result, len(results))
	for _, r := range results {
		byTitle[r.Title] = r
	}

	t.Run("nothing at all", func(t *testing.T) {
		r := byTitle["Invented Release With Nothing Else"]

		if r.ID != "Invented Release With Nothing Else" {
			t.Errorf("ID = %q, want the title as the last-resort identity", r.ID)
		}

		if r.Seeders != 0 || r.Leechers != 0 || r.SizeBytes != 0 {
			t.Errorf("counts = %d/%d/%d, want zeros", r.Seeders, r.Leechers, r.SizeBytes)
		}

		if !r.Published.IsZero() {
			t.Errorf("Published = %s, want the zero time", r.Published)
		}

		if r.Trust != indexer.TrustUnknown {
			t.Errorf("Trust = %s, want unknown: the source said nothing", r.Trust)
		}

		if r.Category != indexer.CategoryOther {
			t.Errorf("Category = %s, want other", r.Category)
		}
	})

	t.Run("junk attributes", func(t *testing.T) {
		r := byTitle["Invented Release With Junk Attributes"]

		if r.Seeders != 9 {
			t.Errorf("Seeders = %d, want 9: the unparseable duplicate is skipped, not fatal", r.Seeders)
		}

		if r.Leechers != 0 {
			t.Errorf("Leechers = %d, want 0: peers was unparseable", r.Leechers)
		}

		if r.SizeBytes != 0 {
			t.Errorf("SizeBytes = %d, want 0: a negative size is not a size", r.SizeBytes)
		}

		if r.InfoHash != "" {
			t.Errorf("InfoHash = %q, want empty: %q is not an infohash", r.InfoHash, "zzzz")
		}

		if r.Magnet != "" {
			t.Errorf("Magnet = %q, want empty: an http URL in magneturl is not a magnet", r.Magnet)
		}

		if !r.Published.IsZero() {
			t.Errorf("Published = %s, want the zero time for an unparseable date", r.Published)
		}

		if r.Extra != nil {
			t.Errorf("Extra = %v, want nil: a non-numeric grabs value is not carried", r.Extra)
		}

		if strings.Contains(r.ID, "apikey") {
			t.Errorf("ID = %q, want the guid with its query stripped", r.ID)
		}

		if !strings.Contains(r.SourceURL, "apikey") {
			t.Errorf("SourceURL = %q, want the guid permalink as published", r.SourceURL)
		}
	})

	t.Run("fewer peers than seeders", func(t *testing.T) {
		r := byTitle["Invented Release With Fewer Peers Than Seeders"]

		if r.Seeders != 10 || r.Leechers != 0 {
			t.Errorf("swarm = %d/%d, want 10/0: peers below seeders contradicts the attribute's meaning, so nothing is inferred",
				r.Seeders, r.Leechers)
		}

		want := time.Date(2026, time.September, 9, 1, 2, 3, 0, time.FixedZone("", -4*60*60))
		if !r.Published.Equal(want) {
			t.Errorf("Published = %s, want %s (single-digit day, numeric zone)", r.Published, want)
		}
	})

	t.Run("explicit leechers", func(t *testing.T) {
		r := byTitle["Invented Release With Explicit Leechers"]

		if r.Seeders != 30 || r.Leechers != 7 {
			t.Errorf("swarm = %d/%d, want 30/7: an explicit leechers attribute wins over the peers total",
				r.Seeders, r.Leechers)
		}
	})

	t.Run("badge stated as false", func(t *testing.T) {
		r := byTitle["Invented Release With No Badge"]

		if r.Trust != indexer.TrustNone {
			t.Errorf("Trust = %s, want none: present-and-false is a statement, unlike absent", r.Trust)
		}
	})

	t.Run("magnet in the link element", func(t *testing.T) {
		r := byTitle["Invented Release Linked By Magnet"]

		const hash = "89abcdef0123456789abcdef0123456789abcdef"

		if !strings.HasPrefix(r.Magnet, "magnet:") {
			t.Errorf("Magnet = %q, want the magnet from <link>", r.Magnet)
		}

		if r.InfoHash != hash {
			t.Errorf("InfoHash = %q, want %q derived from the magnet's xt parameter", r.InfoHash, hash)
		}

		if r.TorrentURL != "" {
			t.Errorf("TorrentURL = %q, want empty: a magnet is not a torrent URL", r.TorrentURL)
		}
	})

	t.Run("unknown numeric category", func(t *testing.T) {
		r := byTitle["Invented Release In An Unknown Bucket"]

		if r.Category != indexer.CategoryData {
			t.Errorf("Category = %s, want data: an unrecognised id falls through to the worded element", r.Category)
		}
	})
}

func TestAttrElementParsesUnderAnyNamespacePrefix(t *testing.T) {
	t.Parallel()

	// The struct tag names no namespace, so encoding/xml matches the local
	// name whatever the prefix is — including one the document never
	// declared, which is what the fixtures rely on (see their headers).
	bodies := map[string]string{
		"torznab prefix, undeclared": `<rss><channel><item><title>t</title><torznab:attr name="seeders" value="5"/></item></channel></rss>`,
		"newznab prefix, undeclared": `<rss><channel><item><title>t</title><newznab:attr name="seeders" value="5"/></item></channel></rss>`,
		"declared prefix":            `<rss xmlns:tz="urn:example:torznab"><channel><item><title>t</title><tz:attr name="seeders" value="5"/></item></channel></rss>`,
		"no prefix":                  `<rss><channel><item><title>t</title><attr name="seeders" value="5"/></item></channel></rss>`,
	}

	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			var doc feedDocument

			if err := decodeDocument([]byte(body), rootFeed, &doc); err != nil {
				t.Fatalf("decode: %v", err)
			}

			if len(doc.Channel.Items) != 1 {
				t.Fatalf("parsed %d items, want 1", len(doc.Channel.Items))
			}

			if got := resultFrom(testID, doc.Channel.Items[0]).Seeders; got != 5 {
				t.Fatalf("Seeders = %d, want 5: the attr element was not matched", got)
			}
		})
	}
}

func TestPublishedParsesTheLayoutsServersSend(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		raw  string
		want time.Time
	}{
		"rfc 1123 with a numeric zone": {
			raw:  "Sat, 14 Mar 2015 17:10:42 -0400",
			want: time.Date(2015, time.March, 14, 17, 10, 42, 0, time.FixedZone("", -4*60*60)),
		},
		"rfc 1123 with a named zone": {
			raw:  "Sun, 06 Jun 2010 17:29:23 UTC",
			want: time.Date(2010, time.June, 6, 17, 29, 23, 0, time.UTC),
		},
		"rfc 822 with a numeric zone": {
			raw:  "06 Jun 10 17:29 +0100",
			want: time.Date(2010, time.June, 6, 17, 29, 0, 0, time.FixedZone("", 60*60)),
		},
		"rfc 822 with a named zone": {
			raw:  "06 Jun 10 17:29 UTC",
			want: time.Date(2010, time.June, 6, 17, 29, 0, 0, time.UTC),
		},
		"single-digit day": {
			raw:  "Sun, 6 Jun 2010 17:29:23 +0100",
			want: time.Date(2010, time.June, 6, 17, 29, 23, 0, time.FixedZone("", 60*60)),
		},
		"single-digit day, named zone": {
			raw:  "Sun, 6 Jun 2010 17:29:23 UTC",
			want: time.Date(2010, time.June, 6, 17, 29, 23, 0, time.UTC),
		},
		"rfc 3339": {
			raw:  "2010-06-06T17:29:23Z",
			want: time.Date(2010, time.June, 6, 17, 29, 23, 0, time.UTC),
		},
		"bare timestamp": {
			raw:  "2010-06-06 17:29:23",
			want: time.Date(2010, time.June, 6, 17, 29, 23, 0, time.UTC),
		},
		"absent":         {raw: "", want: time.Time{}},
		"whitespace":     {raw: "   ", want: time.Time{}},
		"prose":          {raw: "last tuesday", want: time.Time{}},
		"partial":        {raw: "Sat, 14 Mar", want: time.Time{}},
		"unix timestamp": {raw: "1426360242", want: time.Time{}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := publishedFrom(attrIndex{}, feedItem{PubDate: tc.raw})

			if !got.Equal(tc.want) {
				t.Fatalf("publishedFrom(%q) = %s, want %s", tc.raw, got, tc.want)
			}
		})
	}

	t.Run("falls back to the pubdate attribute", func(t *testing.T) {
		idx := attrIndex{attrPubDate: {"Sun, 06 Jun 2010 17:29:23 +0000"}}

		got := publishedFrom(idx, feedItem{PubDate: "nonsense"})
		if got.IsZero() {
			t.Fatal("want the attribute to be tried when the element is unusable")
		}
	})
}

func TestAttributesWithNoNameOrNoValueAreDropped(t *testing.T) {
	t.Parallel()

	it := feedItem{Attrs: []feedAttr{
		{Name: "  ", Value: "9"},
		{Name: attrSeeders, Value: "   "},
		{Name: "  " + strings.ToUpper(attrSeeders) + " ", Value: " 4 "},
	}}

	idx := it.index()

	if len(idx) != 1 {
		t.Fatalf("index = %v, want only the one attribute that had both a name and a value", idx)
	}

	if got := idx.first(attrSeeders); got != "4" {
		t.Fatalf("seeders = %q, want %q: the name is lowercased and both are trimmed", got, "4")
	}

	if got := idx.first("absent"); got != "" {
		t.Fatalf("first(absent) = %q, want empty", got)
	}
}

func TestTrustDistinguishesAbsentFromStatedFalse(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		idx  attrIndex
		want indexer.Trust
	}{
		"nothing":                {idx: attrIndex{}, want: indexer.TrustUnknown},
		"vip wins":               {idx: attrIndex{attrVIP: {"1"}, attrTrusted: {"1"}}, want: indexer.TrustVIP},
		"trusted over verified":  {idx: attrIndex{attrTrusted: {"yes"}, attrVerified: {"yes"}}, want: indexer.TrustTrusted},
		"verified alone":         {idx: attrIndex{attrVerified: {"true"}}, want: indexer.TrustVerified},
		"all stated false":       {idx: attrIndex{attrVIP: {"0"}, attrVerified: {"no"}}, want: indexer.TrustNone},
		"highest false, low set": {idx: attrIndex{attrVIP: {"0"}, attrVerified: {"y"}}, want: indexer.TrustVerified},
		"unreadable value":       {idx: attrIndex{attrVerified: {"maybe"}}, want: indexer.TrustUnknown},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := trustFrom(tc.idx); got != tc.want {
				t.Fatalf("trustFrom = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestSwarmCounts(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		idx                       attrIndex
		wantSeeders, wantLeechers int
	}{
		"peers is the total":     {idx: attrIndex{attrSeeders: {"10"}, attrPeers: {"25"}}, wantSeeders: 10, wantLeechers: 15},
		"peers equals seeders":   {idx: attrIndex{attrSeeders: {"10"}, attrPeers: {"10"}}, wantSeeders: 10, wantLeechers: 0},
		"peers below seeders":    {idx: attrIndex{attrSeeders: {"10"}, attrPeers: {"4"}}, wantSeeders: 10, wantLeechers: 0},
		"explicit leechers wins": {idx: attrIndex{attrSeeders: {"10"}, attrPeers: {"25"}, attrLeechers: {"3"}}, wantSeeders: 10, wantLeechers: 3},
		"negative leechers":      {idx: attrIndex{attrSeeders: {"10"}, attrLeechers: {"-3"}}, wantSeeders: 10, wantLeechers: 0},
		"negative seeders":       {idx: attrIndex{attrSeeders: {"-1"}, attrPeers: {"5"}}, wantSeeders: 0, wantLeechers: 5},
		"no swarm at all":        {idx: attrIndex{}, wantSeeders: 0, wantLeechers: 0},
		"peers only":             {idx: attrIndex{attrPeers: {"8"}}, wantSeeders: 0, wantLeechers: 8},
		"saturating value": {
			idx:         attrIndex{attrSeeders: {"99999999999999999999999"}, attrPeers: {"1"}},
			wantSeeders: 0, wantLeechers: 1,
		},
		"int64 that overflows int on a 32-bit build": {
			idx:         attrIndex{attrSeeders: {"9223372036854775807"}},
			wantSeeders: maxInt, wantLeechers: 0,
		},
		"the most negative int64 saturates rather than wrapping": {
			idx:         attrIndex{attrLeechers: {"-9223372036854775808"}},
			wantSeeders: 0, wantLeechers: 0,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			seeders, leechers := swarmFrom(tc.idx)

			if seeders != tc.wantSeeders || leechers != tc.wantLeechers {
				t.Fatalf("swarmFrom = %d/%d, want %d/%d", seeders, leechers, tc.wantSeeders, tc.wantLeechers)
			}
		})
	}
}

func TestCategoryUsesTheSharedTaxonomy(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		idx  attrIndex
		item feedItem
		want indexer.Category
	}{
		"numeric attribute":     {idx: attrIndex{attrCategory: {"3010"}}, want: indexer.CategoryAudio},
		"first usable of many":  {idx: attrIndex{attrCategory: {"8000", "5000"}}, want: indexer.CategoryVideo},
		"non-numeric attribute": {idx: attrIndex{attrCategory: {"music"}}, want: indexer.CategoryOther},
		"numeric element":       {item: feedItem{Categories: []string{"7000"}}, want: indexer.CategoryText},
		"worded element":        {item: feedItem{Categories: []string{"Datasets"}}, want: indexer.CategoryData},
		"blank element":         {item: feedItem{Categories: []string{"  ", "audio"}}, want: indexer.CategoryAudio},
		"unknown id then word": {
			idx:  attrIndex{attrCategory: {"999999"}},
			item: feedItem{Categories: []string{"999999", "books"}},
			want: indexer.CategoryText,
		},
		"nothing anywhere": {want: indexer.CategoryOther},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := categoryFrom(tc.idx, tc.item); got != tc.want {
				t.Fatalf("categoryFrom = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestInfoHashIsNormalisedOrRejected(t *testing.T) {
	t.Parallel()

	const hex = "0123456789abcdef0123456789abcdef01234567"

	cases := map[string]struct {
		raw  string
		want string
	}{
		"lowercase hex":    {raw: hex, want: hex},
		"uppercase hex":    {raw: strings.ToUpper(hex), want: hex},
		"padded hex":       {raw: "  " + hex + "  ", want: hex},
		"base32":           {raw: "abcdefghijklmnopqrstuvwxyz234567", want: "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"},
		"too short":        {raw: "abcdef", want: ""},
		"hex with a typo":  {raw: strings.Repeat("z", 40), want: ""},
		"base32 with an 8": {raw: strings.Repeat("8", 32), want: ""},
		"empty":            {raw: "", want: ""},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := normaliseInfoHash(tc.raw); got != tc.want {
				t.Fatalf("normaliseInfoHash(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}

	t.Run("derived from a magnet", func(t *testing.T) {
		cases := map[string]string{
			"magnet:?xt=urn:btih:" + hex:                      hex,
			"magnet:?dn=x&xt=urn:sha1:abc&xt=urn:btih:" + hex: hex,
			"magnet:?dn=x":               "",
			"magnet:?xt=urn:btih:nothex": "",
			"magnet:?\x7f":               "",
			"https://feed.example.org/x": "",
			"magnet:%zz":                 "",
		}

		for magnet, want := range cases {
			if got := infoHashFrom(attrIndex{}, magnet); got != want {
				t.Errorf("infoHashFrom(%q) = %q, want %q", magnet, got, want)
			}
		}
	})
}

func TestSizeFallsBackThroughEverySource(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		idx  attrIndex
		item feedItem
		want int64
	}{
		"attribute wins":     {idx: attrIndex{attrSize: {"10"}}, item: feedItem{Size: "20"}, want: 10},
		"element next":       {item: feedItem{Size: "20"}, want: 20},
		"enclosure last":     {item: feedItem{Enclosure: feedLink{Length: "30"}}, want: 30},
		"negative attribute": {idx: attrIndex{attrSize: {"-1"}}, item: feedItem{Size: "20"}, want: 20},
		"negative element":   {item: feedItem{Size: "-1", Enclosure: feedLink{Length: "30"}}, want: 30},
		"all unusable":       {idx: attrIndex{attrSize: {"big"}}, item: feedItem{Size: "bigger"}, want: 0},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := sizeFrom(tc.idx, tc.item); got != tc.want {
				t.Fatalf("sizeFrom = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestUploaderRefusesALinkShapedValue(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		idx  attrIndex
		want string
	}{
		"uploader":                  {idx: attrIndex{attrUploader: {"someone"}}, want: "someone"},
		"poster is the spec'd name": {idx: attrIndex{attrPoster: {"someone else"}}, want: "someone else"},
		"uploader wins":             {idx: attrIndex{attrUploader: {"a"}, attrPoster: {"b"}}, want: "a"},
		"a link is refused":         {idx: attrIndex{attrUploader: {"https://feed.example.org/u/1"}}, want: ""},
		"falls past the link":       {idx: attrIndex{attrUploader: {"https://feed.example.org/u/1"}, attrPoster: {"b"}}, want: "b"},
		"absent":                    {idx: attrIndex{}, want: ""},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := uploaderFrom(tc.idx); got != tc.want {
				t.Fatalf("uploaderFrom = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExtraOnlyCarriesWhitelistedNumbers(t *testing.T) {
	t.Parallel()

	idx := attrIndex{
		attrGrabs:       {"12"},
		attrMinRatio:    {"1.5"},
		attrMinSeedTime: {"not a number"},
		attrUploader:    {"someone"},
		attrMagnet:      {"magnet:?xt=urn:btih:x"},
	}

	got := extraFrom(idx, feedItem{Files: "4", Grabs: "999"})

	want := map[string]string{
		"torznab.grabs":        "12",
		"torznab.minimumratio": "1.5",
		"torznab.files":        "4",
	}

	if len(got) != len(want) {
		t.Fatalf("extraFrom = %v, want %v", got, want)
	}

	for key, value := range want {
		if got[key] != value {
			t.Errorf("extra[%q] = %q, want %q", key, got[key], value)
		}
	}

	if _, present := got["torznab."+attrUploader]; present {
		t.Error("a non-whitelisted attribute reached Extra")
	}

	if extraFrom(attrIndex{}, feedItem{}) != nil {
		t.Error("an item with nothing to carry must produce a nil Extra, not an empty map")
	}
}

func TestSourceAddressIsThePageAndNeverTheDownload(t *testing.T) {
	t.Parallel()

	download := "https://feed.example.org/download/1.torrent?apikey=x"

	cases := map[string]struct {
		item feedItem
		want string
	}{
		"comments wins": {
			item: feedItem{Comments: "https://feed.example.org/details/1", Link: download},
			want: "https://feed.example.org/details/1",
		},
		"permalink guid next": {
			item: feedItem{GUID: feedGUID{IsPermaLink: "true", Value: "https://feed.example.org/details/2"}},
			want: "https://feed.example.org/details/2",
		},
		"a guid that is not a permalink": {
			item: feedItem{GUID: feedGUID{IsPermaLink: "false", Value: "https://feed.example.org/details/3"}},
			want: "",
		},
		"a guid that is not a URL": {
			item: feedItem{GUID: feedGUID{IsPermaLink: "true", Value: "opaque-id"}},
			want: "",
		},
		"never the enclosure": {
			item: feedItem{Enclosure: feedLink{Address: download}, Link: download},
			want: "",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := sourceAddress(tc.item); got != tc.want {
				t.Fatalf("sourceAddress = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTorrentAddressPrefersTheEnclosure(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		item feedItem
		want string
	}{
		"enclosure": {
			item: feedItem{Enclosure: feedLink{Address: "https://feed.example.org/a.torrent"}, Link: "https://feed.example.org/b.torrent"},
			want: "https://feed.example.org/a.torrent",
		},
		"link when there is no enclosure": {
			item: feedItem{Link: "http://feed.example.org/b.torrent"},
			want: "http://feed.example.org/b.torrent",
		},
		"a magnet link is not a torrent URL": {
			item: feedItem{Link: "magnet:?xt=urn:btih:x"},
			want: "",
		},
		"nothing": {want: ""},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := torrentAddress(tc.item); got != tc.want {
				t.Fatalf("torrentAddress = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResultIDStripsACredentialBearingQuery(t *testing.T) {
	t.Parallel()

	const key = "opaque-value-from-the-users-own-account-0192837465"

	cases := map[string]struct {
		hash string
		item feedItem
		want string
	}{
		"infohash first": {
			hash: "0123456789abcdef0123456789abcdef01234567",
			item: feedItem{GUID: feedGUID{Value: "https://feed.example.org/dl?apikey=" + key}},
			want: "0123456789abcdef0123456789abcdef01234567",
		},
		"guid loses its query": {
			item: feedItem{GUID: feedGUID{Value: "https://feed.example.org/dl/7?apikey=" + key}},
			want: "https://feed.example.org/dl/7",
		},
		"guid loses its fragment": {
			item: feedItem{GUID: feedGUID{Value: "https://feed.example.org/dl/7#" + key}},
			want: "https://feed.example.org/dl/7",
		},
		"an opaque guid is kept": {
			item: feedItem{GUID: feedGUID{Value: "  item-7  "}},
			want: "item-7",
		},
		"comments next": {
			item: feedItem{Comments: "https://feed.example.org/details/7?apikey=" + key},
			want: "https://feed.example.org/details/7",
		},
		"link last": {
			item: feedItem{Link: "https://feed.example.org/dl/7?apikey=" + key},
			want: "https://feed.example.org/dl/7",
		},
		"title as the last resort": {
			item: feedItem{Title: " Only A Title "},
			want: "Only A Title",
		},
		"an unparseable candidate is used as it stands": {
			// A control character makes url.Parse fail, so there is no
			// query to strip and the value is kept verbatim.
			item: feedItem{GUID: feedGUID{Value: "https://feed.example.org/dl/7\x7f"}},
			want: "https://feed.example.org/dl/7\x7f",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := resultID(tc.hash, tc.item)

			if got != tc.want {
				t.Fatalf("resultID = %q, want %q", got, tc.want)
			}

			if strings.Contains(got, key) {
				t.Fatalf("resultID leaked the credential: %q", got)
			}
		})
	}
}

func TestDecodeRejectsEveryUnusableDocument(t *testing.T) {
	t.Parallel()

	deep := strings.Repeat("<a>", 60000) + strings.Repeat("</a>", 60000)

	cases := map[string]struct {
		body    string
		wantErr error
	}{
		"empty body":          {body: "", wantErr: ErrDocumentEmpty},
		"whitespace only":     {body: "   \n\t ", wantErr: ErrDocumentEmpty},
		"declaration only":    {body: `<?xml version="1.0"?>`, wantErr: ErrDocumentEmpty},
		"comment only":        {body: "<!-- nothing here -->", wantErr: ErrDocumentEmpty},
		"truncated":           {body: fixture(t, "search-truncated.xml"), wantErr: ErrDocumentMalformed},
		"plain text, not xml": {body: "503 Service Unavailable", wantErr: ErrDocumentEmpty},
		"mismatched tags":     {body: "<rss><channel></rss>", wantErr: ErrDocumentMalformed},
		"undeclared entity":   {body: fixture(t, "search-entity-bomb.xml"), wantErr: ErrDocumentMalformed},
		// 60,000 levels of nesting, through the two paths a document can
		// take: inside an element this package maps, and inside one it
		// ignores and hands to Decoder.Skip. Neither overflows the stack
		// nor errors — it is asserted rather than assumed, because the
		// alternative to knowing is a source that can crash the app.
		"deeply nested inside a mapped element":   {body: `<rss><channel><item><title>` + deep + `</title></item></channel></rss>`, wantErr: nil},
		"deeply nested inside an ignored element": {body: `<rss><channel><ignored>` + deep + `</ignored></channel></rss>`, wantErr: nil},
		"an html page":    {body: fixture(t, "search-html.xml"), wantErr: ErrDocumentUnexpectedRoot},
		"a caps document": {body: fixture(t, "caps-minimal.xml"), wantErr: ErrDocumentUnexpectedRoot},
		"an unknown root": {body: "<feed><entry/></feed>", wantErr: ErrDocumentUnexpectedRoot},
		"a nested rss":    {body: "<wrapper><rss/></wrapper>", wantErr: ErrDocumentUnexpectedRoot},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var doc feedDocument

			err := decodeDocument([]byte(tc.body), rootFeed, &doc)

			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("decodeDocument = %v, want no error", err)
				}

				return
			}

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("decodeDocument = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestDecodeNeverRepeatsTheDocumentBack(t *testing.T) {
	t.Parallel()

	// A hostile source controls every name in the document it sends, and it
	// knows the api_key because every request carries one. So neither an
	// element name nor an entity name may reach the error text.
	const secret = "opaque-value-from-the-users-own-account-0192837465"

	bodies := map[string]string{
		"root element named after the key": "<" + secret + "></" + secret + ">",
		"entity named after the key":       "<rss><channel><item><title>&" + secret + ";</title></item></channel></rss>",
		"element named after the key":      "<rss><channel><" + secret + "></channel></rss>",
	}

	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			var doc feedDocument

			err := decodeDocument([]byte(body), rootFeed, &doc)
			if err == nil {
				t.Fatal("want an error")
			}

			if strings.Contains(err.Error(), secret) {
				t.Fatalf("the error repeated the document back: %q", err)
			}
		})
	}
}

func TestXMLFailureOnANonSyntaxError(t *testing.T) {
	t.Parallel()

	// Every failure encoding/xml can produce here is a *xml.SyntaxError, so
	// the other branch is exercised directly rather than through a document
	// that cannot exist.
	err := xmlFailure(&xml.UnsupportedTypeError{})

	if !errors.Is(err, ErrDocumentMalformed) {
		t.Fatalf("xmlFailure = %v, want it to wrap ErrDocumentMalformed", err)
	}

	if strings.Contains(err.Error(), "line") {
		t.Fatalf("xmlFailure = %q, want no line number for a non-syntax error", err)
	}
}

func TestDescribeRoot(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		rootFeed:    "<rss>",
		rootCaps:    "<caps>",
		rootError:   "<error>",
		"html":      "<html>",
		"something": "an unrecognised element",
	}

	for name, want := range cases {
		if got := describeRoot(name); got != want {
			t.Errorf("describeRoot(%q) = %q, want %q", name, got, want)
		}
	}
}
