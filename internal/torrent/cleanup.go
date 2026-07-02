package torrent

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/anacrolix/torrent"
	"github.com/debridnest/debridnest/internal/storage"
)

func (m *Manager) stopTorrent(id, infoHash, magnet string) {
	hash := normalizeInfoHash(infoHash)
	if hash == "" && magnet != "" {
		if h, err := infoHashFromMagnet(magnet); err == nil {
			hash = h
		}
	}

	dropped := make(map[*torrent.Torrent]struct{})
	drop := func(t *torrent.Torrent) {
		if t == nil {
			return
		}
		if _, ok := dropped[t]; ok {
			return
		}
		dropped[t] = struct{}{}
		t.Drop()
	}

	m.mu.Lock()
	if rt, ok := m.active[id]; ok {
		drop(rt.t)
		delete(m.active, id)
	}
	if hash != "" {
		for tid, rt := range m.active {
			if tid == id {
				continue
			}
			if normalizeInfoHash(rt.t.InfoHash().HexString()) == hash {
				drop(rt.t)
				delete(m.active, tid)
			}
		}
	}
	m.mu.Unlock()

	if hash == "" {
		return
	}
	for _, t := range m.client.Torrents() {
		if normalizeInfoHash(t.InfoHash().HexString()) == hash {
			drop(t)
		}
	}
}

func (m *Manager) torrentDataDirs(rec *storage.TorrentRecord) []string {
	if rec == nil {
		return nil
	}

	seen := make(map[string]struct{})
	var dirs []string
	add := func(hash string) {
		hash = normalizeInfoHash(hash)
		if hash == "" {
			return
		}
		dir := filepath.Join(m.filesDir, hash)
		if _, ok := seen[dir]; ok {
			return
		}
		seen[dir] = struct{}{}
		dirs = append(dirs, dir)
	}

	add(rec.InfoHash)
	if rec.Magnet != "" {
		if h, err := infoHashFromMagnet(rec.Magnet); err == nil {
			add(h)
		}
	}
	for _, f := range rec.Files {
		if f.DiskPath == "" {
			continue
		}
		rel, err := filepath.Rel(m.filesDir, f.DiskPath)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) > 0 {
			add(parts[0])
		}
	}
	return dirs
}

func (m *Manager) removeTorrentData(ctx context.Context, rec *storage.TorrentRecord) {
	_ = m.deleteRemoteObjects(ctx, rec)
	for _, dir := range m.torrentDataDirs(rec) {
		_ = os.RemoveAll(dir)
	}
}

func (m *Manager) remoteObjectKey(store interface {
	ObjectKey(string, string) string
}, rec *storage.TorrentRecord, f storage.TorrentFileRecord) string {
	if f.ObjectKey != "" {
		return f.ObjectKey
	}
	if store == nil || rec == nil || rec.InfoHash == "" || f.Path == "" {
		return ""
	}
	return store.ObjectKey(rec.InfoHash, f.Path)
}

func (m *Manager) deleteRemoteObjects(ctx context.Context, rec *storage.TorrentRecord) error {
	store, err := m.objectStoreForSettings()
	if err != nil {
		return err
	}
	if store == nil || !store.Enabled() || rec == nil {
		return nil
	}
	for _, f := range rec.Files {
		if !f.RemoteStored {
			continue
		}
		key := m.remoteObjectKey(store, rec, f)
		if key == "" {
			continue
		}
		if err := store.Delete(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) reconcileOrphanFiles(ctx context.Context) {
	items, err := m.db.ListTorrents(ctx, 100000)
	if err != nil {
		return
	}

	known := make(map[string]struct{})
	for i := range items {
		for _, dir := range m.torrentDataDirs(&items[i]) {
			known[filepath.Base(dir)] = struct{}{}
		}
	}

	entries, err := os.ReadDir(m.filesDir)
	if err != nil {
		return
	}

	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		name := ent.Name()
		if normalizeInfoHash(name) == "" {
			continue
		}
		if _, ok := known[name]; ok {
			continue
		}
		m.stopTorrent("", name, "")
		_ = os.RemoveAll(filepath.Join(m.filesDir, name))
	}
	m.invalidateDiskUsed()
}

func (m *Manager) abortIfTorrentRemoved(ctx context.Context, id string, t *torrent.Torrent) bool {
	if _, err := m.db.GetTorrent(ctx, id); err == nil {
		return false
	}
	m.mu.Lock()
	delete(m.active, id)
	m.mu.Unlock()
	safeDropTorrent(t)
	if hash := normalizeInfoHash(t.InfoHash().HexString()); hash != "" {
		_ = os.RemoveAll(filepath.Join(m.filesDir, hash))
		m.invalidateDiskUsed()
	}
	return true
}
