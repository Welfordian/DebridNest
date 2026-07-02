package torrent

import (
	"context"
	"os"

	"github.com/debridnest/debridnest/internal/objectstore"
	"github.com/debridnest/debridnest/internal/storage"
)

func (m *Manager) maybeOffloadCompletedFiles(ctx context.Context, rec *storage.TorrentRecord) {
	store, err := m.objectStoreForSettings()
	if err != nil || store == nil || !store.Enabled() || !store.EarlyOffload() {
		return
	}
	m.offloadFiles(ctx, store, rec, false)
}

func (m *Manager) offloadTorrent(ctx context.Context, torrentID string) {
	store, err := m.objectStoreForSettings()
	if err != nil || store == nil || !store.Enabled() {
		return
	}

	rec, err := m.db.GetTorrent(ctx, torrentID)
	if err != nil || !IsCompletedStatus(rec.Status) {
		return
	}

	m.offloadFiles(ctx, store, rec, store.OffloadLocal())
}

func (m *Manager) offloadFiles(ctx context.Context, store *objectstore.Store, rec *storage.TorrentRecord, removeLocal bool) {
	var updated []storage.TorrentFileRecord
	for i := range rec.Files {
		f := rec.Files[i]
		if f.RemoteStored {
			if removeLocal {
				removeLocalOffloadedFile(f)
			}
			continue
		}
		if !offloadCandidate(f) {
			continue
		}
		if changed, out := m.offloadOneFile(ctx, store, rec, f, removeLocal); changed {
			updated = append(updated, out)
		}
	}
	if len(updated) == 0 {
		return
	}

	for _, f := range updated {
		for i := range rec.Files {
			if rec.Files[i].ID == f.ID {
				rec.Files[i] = f
				break
			}
		}
	}
	_ = m.db.UpdateTorrentFiles(ctx, rec.ID, rec.Files)
	m.invalidateDiskUsed()
}

func offloadCandidate(f storage.TorrentFileRecord) bool {
	return f.Selected && !f.RemoteStored && f.Bytes > 0 && f.DownloadedBytes >= f.Bytes
}

func (m *Manager) offloadOneFile(ctx context.Context, store *objectstore.Store, rec *storage.TorrentRecord, f storage.TorrentFileRecord, removeLocal bool) (bool, storage.TorrentFileRecord) {
	if !offloadCandidate(f) {
		return false, f
	}
	if f.DiskPath == "" {
		return false, f
	}
	if _, err := os.Stat(f.DiskPath); err != nil {
		return false, f
	}

	key := store.ObjectKey(rec.InfoHash, f.Path)
	if err := store.Upload(ctx, f.DiskPath, key); err != nil {
		return false, f
	}

	f.ObjectKey = key
	f.RemoteStored = true
	if removeLocal {
		removeLocalOffloadedFile(f)
	}
	return true, f
}

func removeLocalOffloadedFile(f storage.TorrentFileRecord) {
	if f.DiskPath == "" {
		return
	}
	_ = os.Remove(f.DiskPath)
}
