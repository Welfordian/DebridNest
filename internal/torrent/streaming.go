package torrent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/debridnest/debridnest/internal/storage"
)

var ErrStreamNotReady = errors.New("stream not ready")

type StreamOptions struct {
	StartOffset   int64
	RequestLength int64
}

func IsStreamable(rec *storage.TorrentRecord, minBytes int64) bool {
	return NewLifecycle(minBytes).Streamable(rec)
}

func (m *Manager) isStreamable(rec *storage.TorrentRecord) bool {
	return m.lifecycle.Streamable(rec)
}

func (m *Manager) refreshStreamLinks(ctx context.Context, rec *storage.TorrentRecord) {
	if m.lifecycle.LinksVisible(rec) {
		m.ensureHostLinks(ctx, rec)
	}
}

func (m *Manager) LookupByRelativePath(ctx context.Context, relativePath string) (*storage.TorrentRecord, *storage.TorrentFileRecord, error) {
	clean := filepath.Clean(relativePath)
	if filepath.IsAbs(clean) || clean == ".." || clean == "." {
		return nil, nil, fmt.Errorf("invalid path")
	}

	absPath := filepath.Join(m.filesDir, clean)
	file, err := m.db.GetTorrentFileByDiskPath(ctx, absPath)
	if err != nil {
		file, err = m.db.GetTorrentFileByRelativePath(ctx, clean)
		if err != nil {
			return nil, nil, fmt.Errorf("file not found")
		}
	}

	rec, err := m.Get(ctx, file.TorrentID)
	if err != nil {
		return nil, nil, err
	}

	for i := range rec.Files {
		if rec.Files[i].ID == file.ID {
			return rec, &rec.Files[i], nil
		}
	}
	return nil, nil, fmt.Errorf("file not found")
}

func (m *Manager) OpenServingReader(ctx context.Context, torrentID string, fileID int, opts StreamOptions) (io.ReadSeekCloser, time.Time, int64, error) {
	rec, err := m.Get(ctx, torrentID)
	if err != nil {
		return nil, time.Time{}, 0, err
	}

	var file *storage.TorrentFileRecord
	for i := range rec.Files {
		if rec.Files[i].ID == fileID {
			file = &rec.Files[i]
			break
		}
	}
	if file == nil || !file.Selected {
		return nil, time.Time{}, 0, fmt.Errorf("file not found")
	}
	if rec.Status != string(StatusDownloaded) && !m.lifecycle.FileLinksVisible(rec, *file) {
		return nil, time.Time{}, 0, ErrStreamNotReady
	}

	if file.RemoteStored {
		store, err := m.objectStoreForSettings()
		if err != nil {
			return nil, time.Time{}, 0, err
		}
		if store != nil && store.Enabled() {
			key := file.ObjectKey
			if key == "" {
				key = store.ObjectKey(rec.InfoHash, file.Path)
			}
			r, err := store.Open(ctx, key)
			if err != nil {
				return nil, time.Time{}, 0, err
			}
			start := opts.StartOffset
			if start < 0 {
				start = 0
			}
			if start > 0 {
				if _, err := r.Seek(start, io.SeekStart); err != nil {
					r.Close()
					return nil, time.Time{}, 0, err
				}
			}
			size, modTime, err := store.Head(ctx, key)
			if err != nil {
				modTime = time.Now()
				size = file.Bytes
			}
			return r, modTime, size, nil
		}
	}

	if rec.Status == string(StatusDownloaded) {
		f, err := os.Open(file.DiskPath)
		if err != nil {
			return nil, time.Time{}, 0, err
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, time.Time{}, 0, err
		}
		return f, st.ModTime(), file.Bytes, nil
	}

	readahead := m.cfg.StreamReadaheadBytes()
	preRoll := int64(0)
	start := opts.StartOffset
	if start < 0 {
		start = 0
	}
	if start >= file.Bytes {
		start = file.Bytes - 1
	}
	if start < 0 {
		start = 0
	}

	priorityStart := start
	prioritySpan := readahead + preRoll
	if start > 0 {
		readahead = m.cfg.SeekReadaheadBytes()
		preRoll = m.cfg.SeekPreRollBytes()
		priorityStart = start - preRoll
		if priorityStart < 0 {
			priorityStart = 0
		}
		prioritySpan = readahead + preRoll
	}
	if opts.RequestLength > 0 {
		prioritySpan = opts.RequestLength + readahead + preRoll
	}

	m.mu.RLock()
	rt := m.active[torrentID]
	m.mu.RUnlock()

	if rt != nil && rt.t.Info() != nil {
		tfiles := rt.t.Files()
		idx := fileID - 1
		if idx < 0 || idx >= len(tfiles) {
			return nil, time.Time{}, 0, fmt.Errorf("file not found")
		}
		tf := tfiles[idx]

		m.prioritizeFileRange(rt.t, tf, priorityStart, prioritySpan)

		r := tf.NewReader()
		r.SetReadahead(readahead)
		r.SetResponsive()
		if start > 0 {
			if _, err := r.Seek(start, io.SeekStart); err != nil {
				r.Close()
				return nil, time.Time{}, 0, err
			}
		}
		return r, time.Now(), tf.Length(), nil
	}

	if start >= file.StreamableBytes {
		return nil, time.Time{}, 0, ErrStreamNotReady
	}

	f, err := os.Open(file.DiskPath)
	if err != nil {
		return nil, time.Time{}, 0, ErrStreamNotReady
	}
	return f, time.Now(), file.Bytes, nil
}

func (m *Manager) prioritizeFileRange(t *torrent.Torrent, f *torrent.File, startByte int64, span int64) {
	info := t.Info()
	if info == nil || info.PieceLength == 0 {
		return
	}

	pieceLen := int64(info.PieceLength)
	if span < pieceLen {
		span = pieceLen
	}

	absStart := f.Offset() + startByte
	absEnd := absStart + span
	fileEnd := f.Offset() + f.Length()
	if absEnd > fileEnd {
		absEnd = fileEnd
	}

	beginPiece := int(absStart / pieceLen)
	endPieceExclusive := int((absEnd + pieceLen - 1) / pieceLen)

	fileBegin := f.BeginPieceIndex()
	fileEndPieceExclusive := f.EndPieceIndex()
	if beginPiece < fileBegin {
		beginPiece = fileBegin
	}
	if endPieceExclusive > fileEndPieceExclusive {
		endPieceExclusive = fileEndPieceExclusive
	}
	if beginPiece >= endPieceExclusive {
		return
	}

	t.DownloadPieces(beginPiece, endPieceExclusive)
	for i := beginPiece; i < endPieceExclusive; i++ {
		t.Piece(i).SetPriority(torrent.PiecePriorityHigh)
	}
}

func (m *Manager) prioritizeStreamStart(torrentID string, fileID int) {
	m.mu.RLock()
	rt := m.active[torrentID]
	m.mu.RUnlock()
	if rt == nil || rt.t.Info() == nil {
		return
	}

	tfiles := rt.t.Files()
	idx := fileID - 1
	if idx < 0 || idx >= len(tfiles) {
		return
	}

	window := m.cfg.StreamReadaheadBytes()
	m.prioritizeFileRange(rt.t, tfiles[idx], 0, window)
	tailStart := tfiles[idx].Length() - window
	if tailStart < 0 {
		tailStart = 0
	}
	if tailStart > 0 {
		m.prioritizeFileRange(rt.t, tfiles[idx], tailStart, window)
	}
}
