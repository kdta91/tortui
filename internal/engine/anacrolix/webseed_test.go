package anacrolix

import (
	"bytes"
	"context"
	"crypto/sha1"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"

	"github.com/kdta91/tortui/internal/engine"
)

// webseedName is the fixture torrent's name, which a BEP 19 web seed whose
// address ends in "/" puts in front of every file's path.
const webseedName = "ws-fixture"

// webseedFile is one file of the fixture torrent and its content.
type webseedFile struct {
	path []string
	data []byte
}

// webseedFixture builds a real two-file torrent — piece hashes computed
// over the content, unlike buildInfo's zeroed ones, so the engine can
// verify what a web seed sends — plus the directory tree a web seed serves
// it from.
func webseedFixture(t *testing.T) (metainfo.Info, []webseedFile, string) {
	t.Helper()

	files := []webseedFile{
		{path: []string{"a.bin"}, data: patterned(40_000, 3)},
		{path: []string{"sub", "b.bin"}, data: patterned(30_001, 7)},
	}

	var all []byte

	info := metainfo.Info{Name: webseedName, PieceLength: testPieceLength}

	root := t.TempDir()

	for _, f := range files {
		all = append(all, f.data...)
		info.Files = append(info.Files, metainfo.FileInfo{Length: int64(len(f.data)), Path: f.path})

		dst := filepath.Join(append([]string{root, webseedName}, f.path...)...)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatalf("mkdir web seed tree: %v", err)
		}

		if err := os.WriteFile(dst, f.data, 0o600); err != nil {
			t.Fatalf("write web seed file: %v", err)
		}
	}

	for off := 0; off < len(all); off += testPieceLength {
		sum := sha1.Sum(all[off:min(off+testPieceLength, len(all))])
		info.Pieces = append(info.Pieces, sum[:]...)
	}

	return info, files, root
}

// patterned returns n deterministic, non-repeating-per-piece bytes.
func patterned(n int, step byte) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = byte(i/251) ^ byte(i)*step
	}

	return out
}

// storageRedirect is the .torrent fetch's transport: the source's download
// address answers with a redirect to a storage host under its own domain,
// which serves the file — the hand-off a source that stores items on
// separate hosts makes. Nothing leaves the process.
type storageRedirect struct {
	torrent []byte
}

func (s storageRedirect) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host == "files.example.org" {
		return &http.Response{
			StatusCode: http.StatusFound,
			Status:     "302 Found",
			Header:     http.Header{"Location": []string{"https://node1.us.files.example.org/items" + r.URL.Path}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    r,
		}, nil
	}

	if r.URL.Host == "node1.us.files.example.org" {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"application/x-bittorrent"}},
			Body:       io.NopCloser(bytes.NewReader(s.torrent)),
			Request:    r,
		}, nil
	}

	return &http.Response{
		StatusCode: http.StatusNotFound,
		Status:     "404 Not Found",
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    r,
	}, nil
}

// TestTorrentURLDownloadCompletesFromItsWebSeedWithNoPeers is T-9010's
// engine half. A source's .torrent, fetched by URL through a redirect to
// its storage host, names a web seed (url-list) and nothing else; with
// DHT, trackers, PEX and every peer connection off, the download must
// still complete from that web seed alone, byte for byte. It fails if the
// fetch path drops the file's web seeds, or refuses the storage redirect.
func TestTorrentURLDownloadCompletesFromItsWebSeedWithNoPeers(t *testing.T) {
	t.Parallel()

	info, files, root := webseedFixture(t)

	seed := httptest.NewServer(http.FileServer(http.Dir(root)))
	t.Cleanup(seed.Close)

	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("bencode info: %v", err)
	}

	var torrent bytes.Buffer

	mi := metainfo.MetaInfo{InfoBytes: infoBytes, UrlList: []string{seed.URL + "/"}}
	if err := mi.Write(&torrent); err != nil {
		t.Fatalf("write metainfo: %v", err)
	}

	e := newTestEngine(t, func(o *Options) {
		o.webseeds = true
		o.torrentTransport = storageRedirect{torrent: torrent.Bytes()}
		o.MetadataTimeout = 10 * time.Second
	})

	const address = "https://files.example.org/download/" + webseedName + "/" + webseedName + "_archive.torrent"

	id, err := e.Add(context.Background(), engine.AddSource{TorrentURL: address})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	st := waitForComplete(t, e, id)

	for _, f := range files {
		got, err := os.ReadFile(filepath.Join(append([]string{st.SavePath, webseedName}, f.path...)...))
		if err != nil {
			t.Fatalf("read downloaded %v: %v", f.path, err)
		}

		if !bytes.Equal(got, f.data) {
			t.Errorf("downloaded %v differs from what the web seed served (%d bytes, want %d)",
				f.path, len(got), len(f.data))
		}
	}
}

// waitForComplete waits for a torrent to have every byte, which is the
// terminal fact this test is about — whatever the seeding policy moves the
// state to afterwards (seeding, or paused) — and fails at once on an error.
// The library schedules its first web seed request on its own internal
// five-second timer, which no option shortens, so the budget is generous.
func waitForComplete(t *testing.T, e *Engine, id string) engine.TorrentStatus {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)

	var last engine.TorrentStatus

	for time.Now().Before(deadline) {
		last = statusOf(t, e, id)

		if last.State == engine.StateErrored {
			t.Fatalf("torrent %s errored: %v", id, last.Err)
		}

		if last.TotalBytes > 0 && last.DownloadedBytes == last.TotalBytes {
			return last
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("torrent %s did not complete: state %s, %d/%d bytes (err %v)",
		id, last.State, last.DownloadedBytes, last.TotalBytes, last.Err)

	return last
}
