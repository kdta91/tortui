package anacrolix

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"

	"github.com/kdta91/tortui/internal/engine"
)

// Compile-time proof that Engine can save and restore sessions.
var _ engine.Resumer = (*Engine)(nil)

// provenance is what a new tracked entry is minted with beyond its
// destination and display name: the ID a restore would like to keep, and the
// origin and source that ResumeData later reports back.
type provenance struct {
	id         string
	origin     engine.Origin
	magnet     string
	torrentURL string
	metainfo   []byte
}

// newTracked builds a tracked entry carrying p. The caller sets its state and
// registers it.
func (p provenance) newTracked(id, dest, name string) *tracked {
	return &tracked{
		id:         id,
		savePath:   dest,
		name:       name,
		origin:     p.origin,
		magnet:     p.magnet,
		torrentURL: p.torrentURL,
		metainfo:   p.metainfo,
		done:       make(chan struct{}),
	}
}

// mintIDLocked returns want when it is set and free — a restored torrent
// keeps the ID it had last session — or the next unused "an-N" otherwise.
// Reusing an "an-N" moves the counter past it, so a torrent added later can
// never be handed a restored torrent's ID. Engine.mu must be held.
func (e *Engine) mintIDLocked(want string) string {
	if want != "" {
		if _, taken := e.torrents[want]; !taken {
			var n int
			if _, err := fmt.Sscanf(want, "an-%d", &n); err == nil && n > e.nextID {
				e.nextID = n
			}

			return want
		}
	}

	for {
		e.nextID++

		id := fmt.Sprintf("an-%d", e.nextID)
		if _, taken := e.torrents[id]; !taken {
			return id
		}
	}
}

// ResumeData implements engine.Resumer. Once the torrent's info dictionary is
// known the result carries it as Metainfo, so a restore needs no network to
// get back to where it was; before then it carries the magnet or URL the
// torrent was added from.
func (e *Engine) ResumeData(id string) (engine.ResumeData, error) {
	e.mu.Lock()

	if e.closed {
		e.mu.Unlock()
		return engine.ResumeData{}, ErrClosed
	}

	tr, err := e.lookupLocked(id)
	if err != nil {
		e.mu.Unlock()
		return engine.ResumeData{}, err
	}

	d := engine.ResumeData{
		ID:         tr.id,
		Name:       tr.name,
		Magnet:     tr.magnet,
		TorrentURL: tr.torrentURL,
		Metainfo:   bytes.Clone(tr.metainfo),
		SavePath:   tr.savePath,
		Origin:     tr.origin,
	}
	t, spec := tr.t, tr.spec
	e.mu.Unlock()

	if len(d.Metainfo) > 0 {
		return d, nil
	}

	switch {
	case t != nil && t.Info() != nil:
		d.Name = t.Name()
		d.Metainfo, err = encodeMetainfo(t.Metainfo())
	case spec != nil && len(spec.InfoBytes) > 0:
		d.Metainfo, err = encodeMetainfo(metainfo.MetaInfo{InfoBytes: spec.InfoBytes, AnnounceList: spec.Trackers})
	}

	if err != nil {
		return engine.ResumeData{}, fmt.Errorf("anacrolix: resume data for torrent %s: %w", id, err)
	}

	return d, nil
}

// encodeMetainfo bencodes mi into a complete .torrent.
func encodeMetainfo(mi metainfo.MetaInfo) ([]byte, error) {
	var buf bytes.Buffer
	if err := mi.Write(&buf); err != nil {
		return nil, fmt.Errorf("encode metainfo: %w", err)
	}

	return buf.Bytes(), nil
}

// Restore implements engine.Resumer.
//
// Nothing about d is trusted: the destination is re-checked against the
// current known roots (AGENT.md §6.12) and every path the metainfo declares
// is re-validated (AGENT.md §6.11), exactly as Add does. A torrent with
// metainfo resumes from the pieces the persistent piece-completion record
// says are on disk, so completed pieces are not downloaded again. One whose
// record says pieces were downloaded but whose data is gone from disk is
// tracked in StateErrored with engine.ErrDataMissing instead of being
// attached — the library would otherwise quietly start the whole download
// over.
func (e *Engine) Restore(ctx context.Context, d engine.ResumeData) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	prov := provenance{
		id:         d.ID,
		origin:     d.Origin,
		magnet:     d.Magnet,
		torrentURL: d.TorrentURL,
		metainfo:   bytes.Clone(d.Metainfo),
	}

	dest, err := resolveDestination(d.SavePath, e.downloadDir, e.roots)
	if err != nil {
		return e.trackFailed(prov, d.SavePath, d.Name, err)
	}

	switch {
	case len(d.Metainfo) > 0:
		return e.restoreMetainfo(prov, dest, d.Name)

	case trimmed(d.Magnet) != "":
		spec, err := specFromMagnet(d.Magnet)
		if err != nil {
			return e.trackFailed(prov, dest, d.Name, err)
		}

		return e.addOrFail(spec, dest, d.Name, prov)

	case trimmed(d.TorrentURL) != "":
		return e.addFromURL(ctx, d.TorrentURL, dest, prov)

	default:
		return e.trackFailed(prov, dest, d.Name, ErrNoSource)
	}
}

// restoreMetainfo restores a torrent whose .torrent was saved: validate it,
// confirm its data has not gone missing, then add it like a local file.
func (e *Engine) restoreMetainfo(prov provenance, dest, name string) (string, error) {
	if len(prov.metainfo) > maxTorrentFileBytes {
		return e.trackFailed(prov, dest, name, fmt.Errorf("saved torrent is %d bytes, over the %d byte limit",
			len(prov.metainfo), maxTorrentFileBytes))
	}

	mi, err := metainfo.Load(bytes.NewReader(prov.metainfo))
	if err != nil {
		return e.trackFailed(prov, dest, name, fmt.Errorf("parse saved torrent: %w", err))
	}

	spec, err := torrent.TorrentSpecFromMetaInfoErr(mi)
	if err != nil {
		return e.trackFailed(prov, dest, name, fmt.Errorf("read saved torrent: %w", err))
	}

	info, err := specInfo(spec)
	if err != nil {
		return e.trackFailed(prov, dest, name, err)
	}

	if err := validateInfoPaths(info, dest); err != nil {
		return e.trackFailed(prov, dest, name, err)
	}

	if err := e.checkDataPresent(spec.InfoHash, info, dest); err != nil {
		return e.trackFailed(prov, dest, info.BestName(), err)
	}

	return e.addOrFail(spec, dest, name, prov)
}

// addOrFail adds spec as Add would, tracking it as errored rather than
// returning the error when the add is refused (e.g. not enough free space),
// so a restored torrent is never silently dropped.
func (e *Engine) addOrFail(spec *torrent.TorrentSpec, dest, name string, prov provenance) (string, error) {
	id, err := e.addSpec(spec, dest, prov)
	if err != nil {
		return e.trackFailed(prov, dest, name, err)
	}

	return id, nil
}

// checkDataPresent reports engine.ErrDataMissing when the piece-completion
// record for dest says some of this torrent's pieces were downloaded but
// nothing of its data is left on disk. A torrent that never downloaded
// anything has nothing to be missing. Partial loss — some files deleted — is
// not reported: the library notices a short or absent file itself and
// re-fetches just those pieces.
func (e *Engine) checkDataPresent(ih metainfo.Hash, info *metainfo.Info, dest string) error {
	s, err := e.storageFor(dest)
	if err != nil {
		return err
	}

	if !anyPieceComplete(s.completion, ih, info.NumPieces()) {
		return nil
	}

	// validateInfoPaths already confirmed this resolves inside dest. The
	// library writes an incomplete file as "<name>.part" and renames it
	// once complete, so either form counts as present.
	root := filepath.Join(dest, info.BestName())
	for _, p := range []string{root, root + ".part"} {
		_, err := os.Lstat(p)
		if err == nil {
			return nil
		}

		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("check data %s: %w", p, err)
		}
	}

	return fmt.Errorf("%w: nothing at %s — remove this entry, or put the files back and restart",
		engine.ErrDataMissing, root)
}

// anyPieceComplete reports whether pc records any of the torrent's pieces as
// complete.
func anyPieceComplete(pc storage.PieceCompletion, ih metainfo.Hash, pieces int) bool {
	for c := range storage.GetPieceCompletionRange(pc, ih, 0, pieces) {
		if c.Ok && c.Complete {
			return true
		}
	}

	return false
}

// trackFailed tracks a restored torrent that cannot run, in StateErrored with
// cause as its reason, keeping everything ResumeData needs so the entry
// survives another restart until the user removes it. It returns an error
// only when the engine is closed.
func (e *Engine) trackFailed(prov provenance, dest, name string, cause error) (string, error) {
	if errors.Is(cause, ErrClosed) {
		return "", cause
	}

	if name == "" {
		name = prov.id
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return "", ErrClosed
	}

	tr := prov.newTracked(e.mintIDLocked(prov.id), dest, name)
	tr.state = engine.StateErrored
	tr.err = fmt.Errorf("anacrolix: torrent %s: %w", tr.id, cause)

	e.torrents[tr.id] = tr
	e.order = append(e.order, tr.id)

	e.logger.Warn("anacrolix: restored torrent cannot resume",
		"id", tr.id, "destination", dest, "error", cause)

	return tr.id, nil
}
