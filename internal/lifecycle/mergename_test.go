package lifecycle

import (
	"strings"
	"testing"
	"time"

	"github.com/kdta91/tortui/internal/engine"
	"github.com/kdta91/tortui/internal/store"
)

// TestMergeRecordKeepsTheTitleUntilMetadata pins T-9057's record rule: the
// title the add flow recorded survives every save until the engine has the
// torrent's metainfo, and then the torrent's own name replaces it. A recorded
// web address — a session saved before T-9057 — is never kept, and no name
// that reaches the record is one.
func TestMergeRecordKeepsTheTitleUntilMetadata(t *testing.T) {
	t.Parallel()

	const (
		sentinel = "SENTINEL-9057"
		address  = "https://feed.example.org/api?t=get&apikey=" + sentinel
		host     = "torrent file from feed.example.org"
		title    = "Synthetic Corpus 9057"
	)

	meta := []byte("d4:infod4:name4:x.isoee")

	cases := []struct {
		name   string
		rec    string
		d      engine.ResumeData
		wanted string
	}{
		{"title kept while fetching", title, engine.ResumeData{Name: host, TorrentURL: address}, title},
		{"title kept for a magnet without metadata", title, engine.ResumeData{Name: "dn-name"}, title},
		{"torrent name once metadata is known", title, engine.ResumeData{Name: "corpus-9057.iso", Metainfo: meta}, "corpus-9057.iso"},
		{"no recorded name takes the engine's", "", engine.ResumeData{Name: host, TorrentURL: address}, host},
		{"blank recorded name takes the engine's", "  ", engine.ResumeData{Name: host, TorrentURL: address}, host},
		{"recorded address replaced", address, engine.ResumeData{Name: host, TorrentURL: address}, host},
		{"an engine name that is an address is reduced", "", engine.ResumeData{Name: address, TorrentURL: address}, host},
	}

	for _, tc := range cases {
		got := mergeRecord(store.TorrentRecord{ID: "an-1", Name: tc.rec}, tc.d, time.Unix(0, 0))
		if got.Name != tc.wanted {
			t.Errorf("%s: Name = %q, want %q", tc.name, got.Name, tc.wanted)
		}

		if strings.Contains(got.Name, sentinel) {
			t.Errorf("%s: the record's name carries the api key: %q", tc.name, got.Name)
		}
	}
}

// TestSeedProgressRoundTripsThroughTheRecord is T-9135: the seed policy's
// stop, the bytes uploaded and the completion time go from the engine's
// resume data into the record and back, so a restart keeps the policy's
// count.
func TestSeedProgressRoundTripsThroughTheRecord(t *testing.T) {
	t.Parallel()

	done := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	d := engine.ResumeData{ID: "an-1", SavePath: "/downloads", SeedDone: true, Uploaded: 4096, CompletedAt: done}

	rec := mergeRecord(store.TorrentRecord{}, d, time.Now())
	if !rec.SeedDone || rec.Uploaded != 4096 || !rec.CompletedAt.Equal(done) {
		t.Fatalf("record = %+v, want the seed progress of %+v", rec, d)
	}

	back := resumeDataFrom(rec)
	if !back.SeedDone || back.Uploaded != 4096 || !back.CompletedAt.Equal(done) {
		t.Fatalf("resume data = %+v, want the seed progress of %+v", back, d)
	}
}
