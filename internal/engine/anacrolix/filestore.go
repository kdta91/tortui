package anacrolix

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/segments"
	"github.com/anacrolix/torrent/storage"
)

// File and directory modes for downloaded data: the torrent library's own
// file-storage defaults, so a download looks the same on disk as before T-097.
const (
	dataFilePerm os.FileMode = 0o644
	dataDirPerm  os.FileMode = 0o755
)

// maxOpenFilesPerTorrent caps the data files one torrent keeps open at once.
// A torrent can declare thousands of files; past the cap the least recently
// used handle is closed and reopened on its next use.
const maxOpenFilesPerTorrent = 16

// errStorageClosed is returned by piece I/O on a torrent whose storage has
// been closed.
var errStorageClosed = errors.New("anacrolix: torrent storage is closed")

// fileStore is tortui's own file-backed storage.ClientImpl (T-097).
//
// It replaces the torrent library's file storage, whose default I/O
// memory-maps every data file and never unmaps it: its per-torrent close is a
// no-op, so the mappings outlive the torrent and the whole engine. On Windows
// a mapped file cannot be rewritten or deleted, which broke remove-with-data
// and anything touching a finished download after quitting. Here every data
// file is a plain *os.File, and closing a torrent's storage — which the
// library does when the torrent is dropped, and for every torrent when the
// client closes — closes every handle it holds. Close closes whatever is
// still open, then the piece-completion record.
//
// The on-disk layout is the library's with part files off: dir joined with
// the torrent's name and each file's path, data written straight to its final
// name, and which pieces are good kept in completion.
type fileStore struct {
	dir        string
	completion storage.PieceCompletion
	logger     *slog.Logger

	// openFile opens a data file and closeFile closes one: os.OpenFile and
	// (*os.File).Close, unless a test holds one up to prove what waits on
	// it (T-9003).
	openFile  func(name string, flag int, perm os.FileMode) (*os.File, error)
	closeFile func(*os.File) error

	mu       sync.Mutex
	torrents map[*fileTorrent]struct{}

	// open counts the data files open right now across every torrent.
	open atomic.Int64
}

// newFileStore returns a file store rooted at dir recording piece state in
// completion, which it owns and closes.
func newFileStore(dir string, completion storage.PieceCompletion, logger *slog.Logger) *fileStore {
	return &fileStore{
		dir:        dir,
		completion: completion,
		logger:     logger,
		openFile:   os.OpenFile,
		closeFile:  (*os.File).Close,
		torrents:   make(map[*fileTorrent]struct{}),
	}
}

// storeFile is one file of a torrent: where it lives and where it sits in the
// torrent's byte stream.
type storeFile struct {
	path   string
	length int64
}

// OpenTorrent implements storage.ClientImpl. The caller (safeStorage) has
// already confirmed every declared path stays inside dir; the containment
// check here is the library's own, kept as a second line.
func (s *fileStore) OpenTorrent(
	_ context.Context,
	info *metainfo.Info,
	infoHash metainfo.Hash,
) (storage.TorrentImpl, error) {
	upverted := info.UpvertedFiles()
	files := make([]storeFile, len(upverted))

	for i := range upverted {
		var parts []string
		if name := info.BestName(); name != metainfo.NoName {
			parts = append(parts, name)
		}

		path := filepath.Join(s.dir, filepath.Join(append(parts, upverted[i].BestPath()...)...))
		if !insideDir(s.dir, path) {
			return storage.TorrentImpl{}, fmt.Errorf("file %d: path %q is not inside %q", i, path, s.dir)
		}

		files[i] = storeFile{path: path, length: upverted[i].Length}

		// A zero-length file is never written to, so it would never
		// appear on disk without this.
		if upverted[i].Length == 0 {
			if err := createEmptyFile(path); err != nil {
				return storage.TorrentImpl{}, fmt.Errorf("create zero-length file %s: %w", path, err)
			}
		}
	}

	t := &fileTorrent{
		store:    s,
		infoHash: infoHash,
		files:    files,
		index:    info.FileSegmentsIndex(),
		handles:  make(map[int]*storeHandle),
	}

	s.mu.Lock()
	s.torrents[t] = struct{}{}
	s.mu.Unlock()

	return storage.TorrentImpl{Piece: t.piece, Close: t.Close}, nil
}

// Close implements storage.ClientImplCloser: it releases every data file a
// torrent still holds, then closes the piece-completion record.
func (s *fileStore) Close() error {
	s.mu.Lock()
	open := make([]*fileTorrent, 0, len(s.torrents))
	for t := range s.torrents {
		open = append(open, t)
	}
	s.mu.Unlock()

	var errs []error
	for _, t := range open {
		errs = append(errs, t.Close())
	}

	errs = append(errs, s.completion.Close())

	return errors.Join(errs...)
}

// openHandles reports how many data files the store has open. It exists for
// tests: a leaked handle is invisible on an OS that lets an open file be
// deleted.
func (s *fileStore) openHandles() int64 { return s.open.Load() }

// fileTorrent is one torrent's data files.
//
// Every read or write holds mu shared for the duration of the I/O. Taking a
// handle out of the table — replacing, evicting, closing — holds it
// exclusively, so a handle is never taken away from an operation using it.
// Opening a file holds no lock (T-9003): a call that has to open one is
// counted in inflight, under mu shared, before it lets go, and Close waits
// for every such call before it closes the table. So a handle evicted after
// mu is released is closed before Close returns, and nothing is created once
// Close has begun: an open checks closed before it creates a file, and one
// that created a file as Close began closes it and removes what it created,
// unless another caller has since used it (see discard).
type fileTorrent struct {
	store    *fileStore
	infoHash metainfo.Hash
	files    []storeFile
	index    segments.Index

	mu       sync.RWMutex
	handles  map[int]*storeHandle
	closed   atomic.Bool // set under mu exclusive
	uses     atomic.Uint64
	inflight sync.WaitGroup
}

// storeHandle is an open data file.
type storeHandle struct {
	f        *os.File
	writable bool
	lastUse  atomic.Uint64
}

// Close closes every data file the torrent holds, once every call that was
// opening one has finished. It is idempotent.
func (t *fileTorrent) Close() error {
	t.mu.Lock()
	t.closed.Store(true)
	t.mu.Unlock()

	t.inflight.Wait()

	t.mu.Lock()
	handles := t.handles
	t.handles = map[int]*storeHandle{}
	t.mu.Unlock()

	var errs []error
	for i, h := range handles {
		if err := t.closeHandle(h); err != nil {
			errs = append(errs, fmt.Errorf("close %s: %w", t.files[i].path, err))
		}
	}

	t.store.mu.Lock()
	delete(t.store.torrents, t)
	t.store.mu.Unlock()

	return errors.Join(errs...)
}

// withFile runs op on file i, opened for writing when write is set and for
// reading otherwise. A read of a file that does not exist reports
// fs.ErrNotExist without creating it.
//
// op always runs under mu shared, so I/O on one file never waits for I/O on
// another. A file not yet open is opened with no lock held and put in the
// table, then used like any open one (T-9003). If an eviction takes it out
// again before op gets to it — more than the cap of other files opened in
// between — the file is opened and used under mu exclusive instead, so the
// call always finishes.
func (t *fileTorrent) withFile(i int, write bool, op func(*os.File) error) error {
	if done, err := t.withOpenFile(i, write, op, true); done {
		return err
	}

	defer t.inflight.Done()

	if err := t.install(i, write); err != nil {
		return err
	}

	if done, err := t.withOpenFile(i, write, op, false); done {
		return err
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	h, err := t.openLocked(i, write)
	if err != nil {
		return err
	}

	h.lastUse.Store(t.uses.Add(1))

	return op(h.f)
}

// withOpenFile runs op on file i under mu shared when its handle is open and
// fits write. done reports whether op ran, or the storage is closed. When op
// did not run and enter is set, the caller is counted in inflight before mu
// is released, and must call inflight.Done.
func (t *fileTorrent) withOpenFile(i int, write bool, op func(*os.File) error, enter bool) (done bool, err error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.closed.Load() {
		return true, errStorageClosed
	}

	h, ok := t.handles[i]
	if !ok || (write && !h.writable) {
		if enter {
			t.inflight.Add(1)
		}

		return false, nil
	}

	h.lastUse.Store(t.uses.Add(1))

	return true, op(h.f)
}

// install opens file i with no lock held, then puts the handle in the table
// under mu exclusive: replacing a read-only handle when write is wanted, and
// evicting the least recently used handle when the table is full. A handle
// another caller installed first is kept and this one closed. Every handle
// taken out of the table is closed after mu is released, before install
// returns. When Close began while the file was being opened, the handle is
// closed and what the open created is removed, as discard allows.
func (t *fileTorrent) install(i int, write bool) error {
	f, created, err := t.openData(i, write)
	if err != nil {
		return err
	}

	var (
		out  []int
		outH []*storeHandle
	)

	t.mu.Lock()

	switch h, ok := t.handles[i]; {
	case t.closed.Load():
		inUse := t.pathsInUseLocked()
		t.mu.Unlock()

		return errors.Join(errStorageClosed, t.discard(i, f, created, inUse))

	case ok && (h.writable || !write):
		// Another caller opened it first.
		t.mu.Unlock()

		if err := t.store.closeFile(f); err != nil {
			t.store.logger.Warn("anacrolix: close a duplicate data file handle", "path", t.files[i].path, "error", err)
		}

		return nil

	case ok:
		delete(t.handles, i)
		out, outH = append(out, i), append(outH, h)
	}

	if len(t.handles) >= maxOpenFilesPerTorrent {
		victim, h := t.evictLocked()
		out, outH = append(out, victim), append(outH, h)
	}

	h := &storeHandle{f: f, writable: write}
	h.lastUse.Store(t.uses.Add(1))
	t.handles[i] = h
	t.store.open.Add(1)
	t.mu.Unlock()

	return t.closeHandles(out, outH)
}

// discard closes f, opened for file i after Close began, and removes what
// opening it created — the file, then each directory, deepest first — so a
// write landing as the torrent's storage closes leaves nothing behind for a
// remove-with-data to miss.
//
// It never removes what another caller may have put data in (T-9127): the
// file is kept when a handle in the table is for it (inUse, read under mu as
// Close began, after which the table only shrinks), or when it is no longer
// empty, as when another caller opened it, wrote, and lost its handle to an
// eviction; a directory is kept when it holds a kept or in-use file. A
// directory something else has filled is left alone too, as os.Remove never
// removes a directory that is not empty.
func (t *fileTorrent) discard(i int, f *os.File, created, inUse []string) error {
	err := t.store.closeFile(f)

	keep := inUse

	for _, path := range created {
		if holdsAny(path, keep) || (path == t.files[i].path && written(path)) {
			keep = append(keep, path)
			continue
		}

		if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
			t.store.logger.Warn("anacrolix: remove a data path created as storage closed",
				"file", t.files[i].path, "path", path, "error", rmErr)
		}
	}

	if err != nil {
		return fmt.Errorf("close %s: %w", t.files[i].path, err)
	}

	return nil
}

// pathsInUseLocked lists the file of every handle in the table. mu must be
// held.
func (t *fileTorrent) pathsInUseLocked() []string {
	out := make([]string, 0, len(t.handles))
	for i := range t.handles {
		out = append(out, t.files[i].path)
	}

	return out
}

// holdsAny reports whether path is one of paths or a directory above one.
func holdsAny(path string, paths []string) bool {
	for _, p := range paths {
		if insideDir(path, p) {
			return true
		}
	}

	return false
}

// written reports whether the file at path may hold data: it is there and
// not an empty regular file, or it cannot be checked.
func written(path string) bool {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}

	return err != nil || !fi.Mode().IsRegular() || fi.Size() > 0
}

// closeHandles closes handles already taken out of the table; files[i] names
// the file each one is for.
func (t *fileTorrent) closeHandles(files []int, handles []*storeHandle) error {
	var errs []error

	for k, h := range handles {
		if err := t.closeHandle(h); err != nil {
			errs = append(errs, fmt.Errorf("close %s: %w", t.files[files[k]].path, err))
		}
	}

	return errors.Join(errs...)
}

// openLocked returns file i's handle, opening it if need be: replacing a
// read-only handle when write is wanted, and evicting the least recently
// used handle when the table is full. mu must be held exclusively. Only
// withFile's last resort opens a file under the lock.
func (t *fileTorrent) openLocked(i int, write bool) (*storeHandle, error) {
	if t.closed.Load() {
		return nil, errStorageClosed
	}

	if h, ok := t.handles[i]; ok {
		if h.writable || !write {
			return h, nil // Another caller opened it first.
		}

		delete(t.handles, i)

		if err := t.closeHandle(h); err != nil {
			return nil, fmt.Errorf("close %s for reopening: %w", t.files[i].path, err)
		}
	}

	if len(t.handles) >= maxOpenFilesPerTorrent {
		victim, h := t.evictLocked()
		if err := t.closeHandle(h); err != nil {
			return nil, fmt.Errorf("close %s: %w", t.files[victim].path, err)
		}
	}

	f, _, err := t.openData(i, write)
	if err != nil {
		return nil, err
	}

	h := &storeHandle{f: f, writable: write}
	t.handles[i] = h
	t.store.open.Add(1)

	return h, nil
}

// openData opens file i, for reading and writing when write is set and for
// reading otherwise. A write to a missing file creates it and its
// directories — never once Close has begun — and created lists what it made,
// the file first, then each directory deepest first, up to but not including
// the store's own directory.
func (t *fileTorrent) openData(i int, write bool) (f *os.File, created []string, err error) {
	path := t.files[i].path

	if !write {
		f, err = t.store.openFile(path, os.O_RDONLY, 0)
		return f, nil, err
	}

	f, err = t.store.openFile(path, os.O_RDWR, 0)
	if !errors.Is(err, fs.ErrNotExist) {
		return f, nil, err
	}

	if t.closed.Load() {
		return nil, nil, errStorageClosed
	}

	dirs := t.missingDirs(filepath.Dir(path))
	if err := os.MkdirAll(filepath.Dir(path), dataDirPerm); err != nil {
		return nil, nil, fmt.Errorf("create directory for %s: %w", path, err)
	}

	f, err = t.store.openFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, dataFilePerm)
	if errors.Is(err, fs.ErrExist) {
		// Another caller created it in between.
		f, err = t.store.openFile(path, os.O_RDWR, 0)

		return f, dirs, err
	}

	if err != nil {
		return nil, dirs, err
	}

	return f, append([]string{path}, dirs...), nil
}

// missingDirs lists dir and each parent of it that does not exist yet,
// deepest first, stopping at the store's own directory.
func (t *fileTorrent) missingDirs(dir string) []string {
	var out []string

	for dir != t.store.dir && insideDir(t.store.dir, dir) {
		if _, err := os.Lstat(dir); !errors.Is(err, fs.ErrNotExist) {
			break
		}

		out = append(out, dir)
		dir = filepath.Dir(dir)
	}

	return out
}

// evictLocked takes the least recently used handle out of the table and
// returns it, with the file it is for, for the caller to close. mu must be
// held exclusively.
func (t *fileTorrent) evictLocked() (int, *storeHandle) {
	victim, oldest := -1, uint64(0)
	for i, h := range t.handles {
		if use := h.lastUse.Load(); victim < 0 || use < oldest {
			victim, oldest = i, use
		}
	}

	h := t.handles[victim]
	delete(t.handles, victim)

	return victim, h
}

// closeHandle closes h, which is no longer in the handle table, so nothing
// else can reach it.
func (t *fileTorrent) closeHandle(h *storeHandle) error {
	t.store.open.Add(-1)

	return t.store.closeFile(h.f)
}

// readAt reads the torrent's byte stream at off. A missing or short file
// reads as the end of the data: it returns what it could and io.EOF, which
// is how the library learns a piece is not really there.
func (t *fileTorrent) readAt(b []byte, off int64) (int, error) {
	n := 0

	for i, e := range t.index.LocateIter(segments.Extent{Start: off, Length: int64(len(b))}) {
		n1, err := t.readFileAt(i, b[:e.Length], e.Start)
		n += n1
		b = b[n1:]

		if int64(n1) < e.Length {
			if err == nil {
				err = io.ErrUnexpectedEOF
			}

			return n, err
		}

		if err != nil && !errors.Is(err, io.EOF) {
			return n, err
		}
	}

	if len(b) != 0 {
		return n, io.EOF
	}

	return n, nil
}

// readFileAt fills b from file i at off, reporting io.EOF when the file is
// missing or ends first.
func (t *fileTorrent) readFileAt(i int, b []byte, off int64) (int, error) {
	n := 0

	err := t.withFile(i, false, func(f *os.File) error {
		var err error
		n, err = f.ReadAt(b, off)

		return err
	})
	if errors.Is(err, fs.ErrNotExist) {
		return 0, io.EOF
	}

	return n, err
}

// writeAt writes b into the torrent's byte stream at off.
func (t *fileTorrent) writeAt(b []byte, off int64) (int, error) {
	n := 0

	for i, e := range t.index.LocateIter(segments.Extent{Start: off, Length: int64(len(b))}) {
		chunk := b[:e.Length]

		err := t.withFile(i, true, func(f *os.File) error {
			n1, err := f.WriteAt(chunk, e.Start)
			n += n1

			return err
		})
		if err != nil {
			return n, fmt.Errorf("write %s: %w", t.files[i].path, err)
		}

		b = b[e.Length:]
	}

	return n, nil
}

// sync flushes the files under extent that are open for writing, so a piece
// recorded complete is on disk before a crash could lose it.
func (t *fileTorrent) sync(extent segments.Extent) error {
	var errs []error

	for i := range t.index.LocateIter(extent) {
		t.mu.RLock()
		if h, ok := t.handles[i]; ok && h.writable {
			errs = append(errs, h.f.Sync())
		}
		t.mu.RUnlock()
	}

	return errors.Join(errs...)
}

// piece implements storage.TorrentImpl.Piece.
func (t *fileTorrent) piece(p metainfo.Piece) storage.PieceImpl {
	return &filePiece{t: t, p: p}
}

// filePiece is one piece of a fileTorrent.
type filePiece struct {
	t *fileTorrent
	p metainfo.Piece
}

func (p *filePiece) key() metainfo.PieceKey {
	return metainfo.PieceKey{InfoHash: p.t.infoHash, Index: p.p.Index()}
}

func (p *filePiece) extent() segments.Extent {
	return segments.Extent{Start: p.p.Offset(), Length: p.p.Length()}
}

// ReadAt implements io.ReaderAt within the piece.
func (p *filePiece) ReadAt(b []byte, off int64) (int, error) {
	return io.NewSectionReader(readerAt(p.t.readAt), p.p.Offset(), p.p.Length()).ReadAt(b, off)
}

// WriteAt implements io.WriterAt within the piece.
func (p *filePiece) WriteAt(b []byte, off int64) (int, error) {
	if off < 0 || off+int64(len(b)) > p.p.Length() {
		return 0, fmt.Errorf("write of %d bytes at %d overflows piece %d", len(b), off, p.p.Index())
	}

	return p.t.writeAt(b, p.p.Offset()+off)
}

// MarkComplete implements storage.PieceImpl: it records the piece good, then
// flushes its data. A failed flush is logged, not returned, as the library's
// own file storage does.
func (p *filePiece) MarkComplete() error {
	if err := p.t.store.completion.Set(p.key(), true); err != nil {
		return err
	}

	if err := p.t.sync(p.extent()); err != nil {
		p.t.store.logger.Warn("anacrolix: flush completed piece", "piece", p.p.Index(), "error", err)
	}

	return nil
}

// MarkNotComplete implements storage.PieceImpl.
func (p *filePiece) MarkNotComplete() error {
	return p.t.store.completion.Set(p.key(), false)
}

// Completion implements storage.PieceImpl. A piece the record calls complete
// is only reported complete while every file under it is still on disk and
// long enough to hold it; otherwise it is re-marked incomplete, so data
// deleted or truncated since is fetched again rather than served as good.
func (p *filePiece) Completion() storage.Completion {
	c, err := p.t.store.completion.Get(p.key())
	c.Err = errors.Join(c.Err, err)

	if !c.Ok || c.Err != nil || !c.Complete {
		return c
	}

	for i, e := range p.t.index.LocateIter(p.extent()) {
		fi, err := os.Stat(p.t.files[i].path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return storage.Completion{Err: fmt.Errorf("check %s: %w", p.t.files[i].path, err)}
		}

		if err != nil || fi.Size() < e.End() {
			if err := p.MarkNotComplete(); err != nil {
				return storage.Completion{Err: err}
			}

			return storage.Completion{Ok: true, Complete: false}
		}
	}

	return c
}

// readerAt adapts a function to io.ReaderAt.
type readerAt func(b []byte, off int64) (int, error)

func (r readerAt) ReadAt(b []byte, off int64) (int, error) { return r(b, off) }

// insideDir reports whether path is dir or lies beneath it.
func insideDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}

	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// createEmptyFile creates a zero-length file at path, with its parent
// directories, leaving an existing empty file alone.
func createEmptyFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), dataDirPerm); err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, dataFilePerm)
	if err != nil {
		if fi, statErr := os.Stat(path); statErr == nil && fi.Mode().IsRegular() && fi.Size() == 0 {
			return nil
		}

		return err
	}

	return f.Close()
}
