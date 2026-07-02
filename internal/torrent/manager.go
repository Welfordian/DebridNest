package torrent

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	tstorage "github.com/anacrolix/torrent/storage"
	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/diskusage"
	"github.com/debridnest/debridnest/internal/links"
	"github.com/debridnest/debridnest/internal/objectstore"
	"github.com/debridnest/debridnest/internal/settings"
	"github.com/debridnest/debridnest/internal/storage"
)

type Manager struct {
	cfg       config.Config
	db        *storage.DB
	signer    *links.Signer
	client    *torrent.Client
	mu        sync.RWMutex
	active    map[string]*runtimeTorrent
	filesDir  string
	hooks     *Hooks
	settings  *settings.Store
	lifecycle Lifecycle

	objectMu       sync.RWMutex
	objectStoreCfg objectstore.Config
	objectStore    *objectstore.Store
	objectQuotaMu  sync.Mutex

	diskMu     sync.RWMutex
	diskUsed   int64
	diskUsedAt time.Time
}

type Hooks struct {
	OnDownloadComplete func(name string)
}

var ErrInvalidMagnet = errors.New("invalid magnet")
var ErrObjectStorageQuotaExceeded = errors.New("S3 object storage quota exceeded")

func (m *Manager) SetHooks(h *Hooks) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hooks = h
}

func (m *Manager) fireDownloadComplete(name string) {
	m.mu.RLock()
	hooks := m.hooks
	m.mu.RUnlock()
	if hooks != nil && hooks.OnDownloadComplete != nil {
		hooks.OnDownloadComplete(name)
	}
}

type runtimeTorrent struct {
	id        string
	t         *torrent.Torrent
	done      chan struct{}
	startedAt time.Time
}

func NewManager(cfg config.Config, db *storage.DB, signer *links.Signer, settingsStore *settings.Store, s3Cfg objectstore.Config) (*Manager, error) {
	filesDir := cfg.FilesDir
	if filesDir == "" {
		filesDir = filepath.Join(cfg.DataDir, "files")
	}
	torrentDir := filepath.Join(cfg.DataDir, "torrent")
	if err := os.MkdirAll(filesDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(torrentDir, 0o755); err != nil {
		return nil, err
	}

	clientCfg := torrent.NewDefaultClientConfig()
	clientCfg.DataDir = torrentDir
	clientCfg.SetListenAddr(":" + cfg.TorrentPort)
	clientCfg.Seed = cfg.SeedAfterComplete
	clientCfg.NoUpload = false
	clientCfg.DefaultStorage = tstorage.NewFileByInfoHash(filesDir)

	client, err := torrent.NewClient(clientCfg)
	if err != nil {
		return nil, fmt.Errorf("create torrent client: %w", err)
	}

	objectStore, err := objectstore.New(s3Cfg)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("object store: %w", err)
	}

	m := &Manager{
		cfg:            cfg,
		db:             db,
		signer:         signer,
		client:         client,
		active:         make(map[string]*runtimeTorrent),
		filesDir:       filesDir,
		settings:       settingsStore,
		lifecycle:      NewLifecycle(cfg.MinStreamBytes()),
		objectStoreCfg: s3Cfg,
		objectStore:    objectStore,
	}

	if err := m.resumeIncomplete(context.Background()); err != nil {
		client.Close()
		return nil, err
	}

	m.reconcileOrphanFiles(context.Background())
	m.reconcileDiskUsage(true)
	go m.backgroundLoop()
	return m, nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	for _, rt := range m.active {
		rt.t.Drop()
		close(rt.done)
	}
	m.active = map[string]*runtimeTorrent{}
	m.mu.Unlock()
	errs := m.client.Close()
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func (m *Manager) AddMagnet(ctx context.Context, magnet string) (*storage.TorrentRecord, error) {
	magnet = strings.TrimSpace(magnet)
	ih, err := infoHashFromMagnet(magnet)
	if err != nil || ih == "" {
		return nil, ErrInvalidMagnet
	}

	if existing, err := m.db.GetTorrentByHash(ctx, ih); err == nil && existing != nil {
		m.refreshProgress(ctx, existing)
		if existing.Status == string(StatusDownloaded) {
			m.ensureHostLinks(ctx, existing)
			return existing, nil
		}
		m.mu.RLock()
		_, active := m.active[existing.ID]
		m.mu.RUnlock()
		if !active {
			if isFailedTorrentStatus(existing.Status) {
				if err := m.db.ResetTorrentForRetry(ctx, existing.ID); err == nil {
					if updated, getErr := m.db.GetTorrent(ctx, existing.ID); getErr == nil {
						existing = updated
					}
				}
			}
			go m.resumeOne(*existing)
		}
		return existing, nil
	}
	if err := m.ensureObjectStorageQuotaAvailable(ctx, 0); err != nil {
		return nil, err
	}

	id, err := newTorrentID()
	if err != nil {
		return nil, err
	}

	rec := storage.TorrentRecord{
		ID:       id,
		InfoHash: ih,
		Magnet:   magnet,
		Status:   string(StatusMagnetConversion),
		AddedAt:  time.Now().UTC(),
		Progress: 0,
	}

	if err := m.db.CreateTorrent(ctx, rec); err != nil {
		return nil, err
	}

	go m.processMagnet(id, magnet)
	return m.db.GetTorrent(ctx, id)
}

func (m *Manager) SelectFiles(ctx context.Context, torrentID, filesSpec string) error {
	rec, err := m.db.GetTorrent(ctx, torrentID)
	if err != nil {
		return err
	}

	if err := m.lifecycle.ApplySelection(rec, filesSpec); err != nil {
		return err
	}
	if m.currentS3Config().Enabled && m.objectStorageQuotaBytes() > 0 {
		m.objectQuotaMu.Lock()
		defer m.objectQuotaMu.Unlock()
		if err := m.ensureObjectStorageQuotaForSelection(ctx, rec); err != nil {
			return err
		}
		if err := m.db.UpdateTorrentFiles(ctx, torrentID, rec.Files); err != nil {
			return err
		}
		if err := m.db.UpdateTorrent(ctx, *rec); err != nil {
			return err
		}
	} else {
		if err := m.db.UpdateTorrentFiles(ctx, torrentID, rec.Files); err != nil {
			return err
		}
		if err := m.db.UpdateTorrent(ctx, *rec); err != nil {
			return err
		}
	}

	m.mu.RLock()
	rt := m.active[torrentID]
	m.mu.RUnlock()
	if rt != nil {
		m.applySelection(torrentID, rt.t, rec.Files)
	}
	return nil
}

func (m *Manager) Delete(ctx context.Context, torrentID string) error {
	rec, err := m.db.GetTorrent(ctx, torrentID)
	if err != nil {
		return err
	}

	m.stopTorrent(torrentID, rec.InfoHash, rec.Magnet)
	m.removeTorrentData(ctx, rec)
	m.invalidateDiskUsed()
	return m.db.DeleteTorrent(ctx, torrentID)
}

func (m *Manager) DeleteMany(ctx context.Context, ids []string) (deleted int, failed []string) {
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			continue
		}
		if err := m.Delete(ctx, id); err != nil {
			failed = append(failed, id)
			continue
		}
		deleted++
	}
	return deleted, failed
}

func (m *Manager) Get(ctx context.Context, torrentID string) (*storage.TorrentRecord, error) {
	rec, err := m.db.GetTorrent(ctx, torrentID)
	if err != nil {
		return nil, err
	}
	m.refreshProgress(ctx, rec)
	return rec, nil
}

func (m *Manager) List(ctx context.Context, limit int) ([]storage.TorrentRecord, error) {
	items, err := m.db.ListTorrents(ctx, limit)
	if err != nil {
		return nil, err
	}
	for i := range items {
		m.refreshProgress(ctx, &items[i])
	}
	return items, nil
}

func (m *Manager) Unrestrict(ctx context.Context, hostLink string) (*storage.DownloadRecord, error) {
	linkID := extractLinkID(hostLink)
	if linkID == "" {
		return nil, fmt.Errorf("invalid host link")
	}

	torrentID, fileID, err := m.db.GetHostLink(ctx, linkID)
	if err != nil {
		return nil, fmt.Errorf("unknown host link")
	}

	rec, err := m.db.GetTorrent(ctx, torrentID)
	if err != nil {
		return nil, err
	}

	var file *storage.TorrentFileRecord
	for i := range rec.Files {
		if rec.Files[i].ID == fileID {
			file = &rec.Files[i]
			break
		}
	}
	if file == nil || !file.Selected {
		return nil, fmt.Errorf("file not found")
	}
	if !m.lifecycle.FileLinksVisible(rec, *file) {
		return nil, ErrStreamNotReady
	}

	relativePath, err := filepath.Rel(m.filesDir, file.DiskPath)
	if err != nil || strings.HasPrefix(relativePath, "..") {
		relativePath = filepath.Join(rec.InfoHash, filepath.Base(file.Path))
	}
	relativePath = filepath.ToSlash(relativePath)

	expires := time.Now().Add(m.cfg.LinkTTL)
	downloadURL := m.signer.SignDownload(relativePath, expires)
	filename := filepath.Base(file.Path)

	downloadID, err := newTorrentID()
	if err != nil {
		return nil, err
	}

	dl := storage.DownloadRecord{
		ID:          downloadID,
		TorrentID:   torrentID,
		FileID:      fileID,
		Filename:    filename,
		MimeType:    links.MimeType(filename),
		Filesize:    file.Bytes,
		HostLink:    hostLink,
		DownloadURL: downloadURL,
		GeneratedAt: time.Now().UTC(),
	}
	if err := m.db.SaveDownload(ctx, dl); err != nil {
		return nil, err
	}
	return &dl, nil
}

func (m *Manager) ListDownloads(ctx context.Context, limit int) ([]storage.DownloadRecord, error) {
	return m.db.ListDownloads(ctx, limit)
}

// InstantAvailability returns Real-Debrid-compatible cache map for hashes already downloaded locally.
// The "real-debrid.com" host key matches Real-Debrid API response shape for Stremio/Torrentio clients.
func (m *Manager) InstantAvailability(ctx context.Context, hashes []string) map[string]map[string][]string {
	out := make(map[string]map[string][]string)
	const hostKey = "real-debrid.com" // API compatibility only; not affiliated with Real-Debrid

	normalized := make([]string, 0, len(hashes))
	for _, raw := range hashes {
		hash := normalizeInfoHash(raw)
		if hash != "" {
			normalized = append(normalized, hash)
		}
	}
	if len(normalized) == 0 {
		return out
	}

	byHash, err := m.db.GetTorrentsByHashes(ctx, normalized)
	if err != nil {
		return out
	}

	for _, hash := range normalized {
		rec, ok := byHash[hash]
		if !ok || !m.lifecycle.LinksVisible(rec) {
			continue
		}
		m.ensureHostLinks(ctx, rec)
		variant := instantAvailabilityVariant(rec)
		out[hash] = map[string][]string{
			hostKey: {variant},
		}
	}
	return out
}

func instantAvailabilityVariant(rec *storage.TorrentRecord) string {
	for _, f := range rec.Files {
		if !f.Selected {
			continue
		}
		name := strings.ToLower(f.Path)
		switch {
		case strings.Contains(name, "2160p"), strings.Contains(name, "4k"):
			return "4K"
		case strings.Contains(name, "1080p"):
			return "1080p"
		case strings.Contains(name, "720p"):
			return "720p"
		default:
			return "1080p"
		}
	}
	return "1080p"
}

func normalizeInfoHash(hash string) string {
	hash = strings.TrimSpace(strings.ToLower(hash))
	hash = strings.TrimPrefix(hash, "urn:btih:")
	if len(hash) == 40 {
		return hash
	}
	return ""
}

func (m *Manager) FilePath(relativePath string) (string, error) {
	clean := filepath.Clean(relativePath)
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", fmt.Errorf("invalid path")
	}
	return filepath.Join(m.filesDir, clean), nil
}

const (
	magnetMetadataTimeout    = 3 * time.Minute
	magnetMetadataStaleGrace = 15 * time.Second
)

func (m *Manager) waitTorrentInfo(t *torrent.Torrent) bool {
	if t.Info() != nil {
		return true
	}
	select {
	case <-t.GotInfo():
		return t.Info() != nil
	case <-time.After(magnetMetadataTimeout):
		return false
	}
}

func (m *Manager) registerRuntimeTorrent(id string, t *torrent.Torrent) {
	rt := &runtimeTorrent{id: id, t: t, done: make(chan struct{}), startedAt: time.Now()}
	m.mu.Lock()
	m.active[id] = rt
	m.mu.Unlock()
}

func (m *Manager) dropRuntimeTorrent(id string, t *torrent.Torrent) {
	m.mu.Lock()
	delete(m.active, id)
	m.mu.Unlock()
	safeDropTorrent(t)
}

func safeDropTorrent(t *torrent.Torrent) {
	if t == nil {
		return
	}
	defer func() {
		if v := recover(); v != nil && fmt.Sprint(v) != "already closed" {
			panic(v)
		}
	}()
	t.Drop()
}

func (m *Manager) finalizeTorrentMetadata(ctx context.Context, id string, t *torrent.Torrent) {
	if m.abortIfTorrentRemoved(ctx, id, t) {
		return
	}

	rec, err := m.db.GetTorrent(ctx, id)
	if err != nil {
		return
	}

	mi := t.Metainfo()
	var infoBuf bytes.Buffer
	if err := mi.Write(&infoBuf); err != nil {
		m.setError(ctx, id, string(StatusMagnetError))
		return
	}
	infoBytes := infoBuf.Bytes()
	rec.InfoHash = t.InfoHash().HexString()
	rec.InfoBytes = infoBytes
	rec.OriginalName = t.Name()
	rec.Name = t.Name()
	rec.Status = string(StatusWaitingFileSelection)

	var originalBytes int64
	var files []storage.TorrentFileRecord
	for i, f := range t.Files() {
		path := "/" + filepath.ToSlash(f.DisplayPath())
		size := f.Length()
		originalBytes += size
		diskPath := filepath.Join(m.filesDir, rec.InfoHash, filepath.FromSlash(f.Path()))
		files = append(files, storage.TorrentFileRecord{
			ID:        i + 1,
			TorrentID: id,
			Path:      path,
			Bytes:     size,
			DiskPath:  diskPath,
		})
		f.SetPriority(torrent.PiecePriorityNone)
	}
	rec.OriginalBytes = originalBytes
	rec.Files = files

	if err := m.db.UpdateTorrent(ctx, *rec); err != nil {
		return
	}
	if err := m.replaceTorrentFiles(ctx, id, files); err != nil {
		return
	}

	if fileID := m.lifecycle.PickSingleObviousVideo(files); fileID != 0 {
		_ = m.SelectFiles(ctx, id, fmt.Sprintf("%d", fileID))
	} else {
		go m.autoSelectAfterDelay(id)
	}

	m.trackTorrent(id, t)
}

func (m *Manager) processMagnet(id, magnet string) {
	ctx := context.Background()
	t, err := m.client.AddMagnet(magnet)
	if err != nil {
		m.setError(ctx, id, string(StatusMagnetError))
		return
	}

	m.registerRuntimeTorrent(id, t)

	if !m.waitTorrentInfo(t) {
		m.setError(ctx, id, string(StatusMagnetError))
		m.dropRuntimeTorrent(id, t)
		return
	}

	m.finalizeTorrentMetadata(ctx, id, t)
}

func (m *Manager) processTorrentMetainfo(id string, mi *metainfo.MetaInfo) {
	ctx := context.Background()
	t, err := m.client.AddTorrent(mi)
	if err != nil {
		m.setError(ctx, id, string(StatusMagnetError))
		return
	}

	m.registerRuntimeTorrent(id, t)

	if !m.waitTorrentInfo(t) {
		m.setError(ctx, id, string(StatusMagnetError))
		m.dropRuntimeTorrent(id, t)
		return
	}

	m.finalizeTorrentMetadata(ctx, id, t)
}

func (m *Manager) autoSelectAfterDelay(id string) {
	if m.cfg.AutoSelectAfter <= 0 {
		return
	}
	time.Sleep(m.cfg.AutoSelectAfter)

	ctx := context.Background()
	rec, err := m.db.GetTorrent(ctx, id)
	if err != nil || rec.Status != string(StatusWaitingFileSelection) {
		return
	}

	best := m.lifecycle.PickLargestVideo(rec.Files)
	if best == 0 {
		return
	}
	_ = m.SelectFiles(ctx, id, fmt.Sprintf("%d", best))
}

func (m *Manager) trackTorrent(id string, t *torrent.Torrent) {
	ctx := context.Background()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var lastBytes int64
	var lastAt time.Time
	var lastPersistAt time.Time
	var lastPersistProgress int

	for {
		select {
		case <-ticker.C:
			rec, err := m.db.GetTorrent(ctx, id)
			if err != nil {
				return
			}
			if m.reconcileRemoteStoredComplete(ctx, rec) {
				return
			}

			prevStatus := rec.Status
			prevProgress := rec.Progress
			prevFiles := cloneFileDownloaded(rec.Files)

			m.syncRuntimeState(rec, t)
			if m.lifecycle.LinksVisible(rec) {
				m.refreshStreamLinks(ctx, rec)
			}

			stats := t.Stats()
			now := time.Now()
			currentBytes := stats.BytesReadUsefulData.Int64()
			if !lastAt.IsZero() {
				delta := currentBytes - lastBytes
				secs := now.Sub(lastAt).Seconds()
				if secs > 0 {
					rec.Speed = int64(float64(delta) / secs)
				}
			}
			lastBytes = currentBytes
			lastAt = now

			statusChanged := rec.Status != prevStatus
			progressDelta := absInt(rec.Progress - lastPersistProgress)
			if lastPersistAt.IsZero() {
				progressDelta = absInt(rec.Progress - prevProgress)
			}
			filesChanged := filesDownloadedChanged(prevFiles, rec.Files)
			shouldPersist := statusChanged ||
				progressDelta >= 1 ||
				filesChanged ||
				lastPersistAt.IsZero() ||
				now.Sub(lastPersistAt) >= 10*time.Second

			if shouldPersist {
				_ = m.db.UpdateTorrentFiles(ctx, id, rec.Files)
				_ = m.db.UpdateTorrent(ctx, *rec)
				lastPersistAt = now
				lastPersistProgress = rec.Progress
			}

			if filesChanged || shouldPersist {
				m.maybeOffloadCompletedFiles(ctx, rec)
			}

			if rec.Status == string(StatusDownloaded) {
				if prevStatus != string(StatusDownloaded) {
					m.fireDownloadComplete(rec.Name)
					go m.offloadTorrent(context.Background(), rec.ID)
				}
				return
			}
		}
	}
}

func (m *Manager) syncRuntimeFiles(t *torrent.Torrent, rec *storage.TorrentRecord) {
	if t.Info() == nil {
		return
	}
	tfiles := t.Files()
	for i := range rec.Files {
		idx := rec.Files[i].ID - 1
		if idx < 0 || idx >= len(tfiles) {
			continue
		}
		f := tfiles[idx]
		if rec.Files[i].RemoteStored {
			rec.Files[i].DownloadedBytes = rec.Files[i].Bytes
			rec.Files[i].StreamableBytes = rec.Files[i].Bytes
			rec.Files[i].DiskPath = filepath.Join(m.filesDir, rec.InfoHash, filepath.FromSlash(f.Path()))
			continue
		}
		rec.Files[i].DownloadedBytes = f.BytesCompleted()
		rec.Files[i].StreamableBytes = streamablePrefixBytes(f)
		rec.Files[i].DiskPath = filepath.Join(m.filesDir, rec.InfoHash, filepath.FromSlash(f.Path()))
	}
}

func streamablePrefixBytes(f *torrent.File) int64 {
	if f == nil {
		return 0
	}
	var ready int64
	for _, state := range f.State() {
		if !state.Complete {
			break
		}
		ready += state.Bytes
		if ready >= f.Length() {
			return f.Length()
		}
	}
	return ready
}

func (m *Manager) applySelection(torrentID string, t *torrent.Torrent, files []storage.TorrentFileRecord) {
	if t.Info() == nil {
		return
	}
	tfiles := t.Files()
	selected := map[int]bool{}
	for _, f := range files {
		if f.Selected {
			selected[f.ID] = true
		}
	}
	for i, f := range tfiles {
		fileID := i + 1
		if selected[fileID] {
			f.Download()
			go m.prioritizeStreamStart(torrentID, fileID)
		} else {
			f.SetPriority(torrent.PiecePriorityNone)
		}
	}
}

func (m *Manager) refreshProgress(ctx context.Context, rec *storage.TorrentRecord) {
	if m.reconcileRemoteStoredComplete(ctx, rec) {
		return
	}

	m.mu.RLock()
	rt := m.active[rec.ID]
	m.mu.RUnlock()
	if rt == nil {
		if m.lifecycle.LinksVisible(rec) {
			m.refreshStreamLinks(ctx, rec)
		}
		return
	}

	if rt.t.Info() == nil {
		if isFailedTorrentStatus(rec.Status) || rec.Status == string(StatusDownloaded) {
			return
		}
		if rec.Status != string(StatusMagnetConversion) {
			rec.Status = string(StatusMagnetConversion)
			_ = m.db.UpdateTorrent(ctx, *rec)
		}
		return
	}

	prevStatus := rec.Status
	prevProgress := rec.Progress
	prevFiles := cloneFileDownloaded(rec.Files)

	if rec.InfoHash == "" {
		rec.InfoHash = rt.t.InfoHash().HexString()
	}

	m.syncRuntimeState(rec, rt.t)

	statusChanged := rec.Status != prevStatus
	progressChanged := rec.Progress != prevProgress
	filesChanged := filesDownloadedChanged(prevFiles, rec.Files)

	if statusChanged || progressChanged || filesChanged {
		if m.lifecycle.LinksVisible(rec) {
			m.refreshStreamLinks(ctx, rec)
		}
		_ = m.db.UpdateTorrentFiles(ctx, rec.ID, rec.Files)
		_ = m.db.UpdateTorrent(ctx, *rec)
		if filesChanged {
			m.maybeOffloadCompletedFiles(ctx, rec)
		}
		if prevStatus != string(StatusDownloaded) && rec.Status == string(StatusDownloaded) {
			m.fireDownloadComplete(rec.Name)
			go m.offloadTorrent(context.Background(), rec.ID)
		}
	} else if m.lifecycle.LinksVisible(rec) {
		m.refreshStreamLinks(ctx, rec)
	}
}

func (m *Manager) syncRuntimeState(rec *storage.TorrentRecord, t *torrent.Torrent) {
	if t.Info() == nil {
		return
	}

	m.syncRuntimeFiles(t, rec)
	total := m.selectedTotal(rec)
	completed := m.selectedCompleted(rec)
	if total > 0 {
		rec.Progress = int(completed * 100 / total)
	}
	stats := t.Stats()
	m.lifecycle.ApplyRuntimeSnapshot(rec, RuntimeSnapshot{
		TotalBytes:     total,
		CompletedBytes: completed,
		Seeders:        stats.ActivePeers,
		Now:            time.Now().UTC(),
	})
}

type fileProgressSnapshot struct {
	downloaded int64
	streamable int64
}

func cloneFileDownloaded(files []storage.TorrentFileRecord) []fileProgressSnapshot {
	out := make([]fileProgressSnapshot, len(files))
	for i, f := range files {
		out[i] = fileProgressSnapshot{
			downloaded: f.DownloadedBytes,
			streamable: f.StreamableBytes,
		}
	}
	return out
}

func filesDownloadedChanged(prev []fileProgressSnapshot, files []storage.TorrentFileRecord) bool {
	if len(prev) != len(files) {
		return true
	}
	for i, f := range files {
		if prev[i].downloaded != f.DownloadedBytes || prev[i].streamable != f.StreamableBytes {
			return true
		}
	}
	return false
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func (m *Manager) ensureHostLinks(ctx context.Context, rec *storage.TorrentRecord) {
	var linkIDs []string
	for _, f := range rec.Files {
		if !m.lifecycle.FileLinksVisible(rec, f) {
			continue
		}
		linkID, err := m.db.GetHostLinkByTorrentFile(ctx, rec.ID, f.ID)
		if err != nil {
			linkID, _ = newTorrentID()
			_ = m.db.UpsertHostLink(ctx, linkID, rec.ID, f.ID, time.Now().UTC())
		}
		linkIDs = append(linkIDs, m.signer.HostLink(linkID))
	}
	rec.Links = linkIDs
}

func (m *Manager) resumeIncomplete(ctx context.Context) error {
	items, err := m.db.ListIncompleteTorrents(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, rec := range items {
		if m.reconcileRemoteStoredComplete(ctx, &rec) {
			continue
		}
		if m.reconcileStaleMagnetConversion(ctx, rec, now) {
			continue
		}
		go m.resumeOne(rec)
	}
	return nil
}

func (m *Manager) resumeOne(rec storage.TorrentRecord) {
	ctx := context.Background()
	if rec.InfoHash == "" {
		if ih, err := infoHashFromMagnet(rec.Magnet); err == nil && ih != "" {
			rec.InfoHash = ih
			_ = m.db.UpdateTorrent(ctx, rec)
		}
	}

	t, err := m.client.AddMagnet(rec.Magnet)
	if err != nil {
		m.setError(ctx, rec.ID, string(StatusMagnetError))
		return
	}

	m.registerRuntimeTorrent(rec.ID, t)

	if !m.waitTorrentInfo(t) {
		m.setError(ctx, rec.ID, string(StatusMagnetError))
		m.dropRuntimeTorrent(rec.ID, t)
		return
	}

	if m.abortIfTorrentRemoved(ctx, rec.ID, t) {
		return
	}

	m.applySelection(rec.ID, t, rec.Files)
	go m.trackTorrent(rec.ID, t)

	updated, err := m.db.GetTorrent(ctx, rec.ID)
	if err == nil && updated.Status == string(StatusWaitingFileSelection) {
		go m.autoSelectAfterDelay(updated.ID)
	}
}

func (m *Manager) replaceTorrentFiles(ctx context.Context, torrentID string, files []storage.TorrentFileRecord) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM torrent_files WHERE torrent_id = ?`, torrentID); err != nil {
		return err
	}
	for _, f := range files {
		selected := 0
		if f.Selected {
			selected = 1
		}
		remoteStored := 0
		if f.RemoteStored {
			remoteStored = 1
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO torrent_files (id, torrent_id, path, bytes, selected, downloaded_bytes, streamable_bytes, disk_path, object_key, remote_stored)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			f.ID, torrentID, f.Path, f.Bytes, selected, f.DownloadedBytes, f.StreamableBytes, f.DiskPath, f.ObjectKey, remoteStored,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (m *Manager) backgroundLoop() {
	diskTicker := time.NewTicker(60 * time.Second)
	defer diskTicker.Stop()
	loopTicker := time.NewTicker(10 * time.Second)
	defer loopTicker.Stop()

	for {
		select {
		case <-diskTicker.C:
			m.reconcileDiskUsage(false)
		case <-loopTicker.C:
			ctx := context.Background()
			items, err := m.db.ListIncompleteTorrents(ctx)
			if err != nil {
				continue
			}
			now := time.Now()
			for _, rec := range items {
				if m.reconcileRemoteStoredComplete(ctx, &rec) {
					continue
				}
				if m.reconcileStaleMagnetConversion(ctx, rec, now) {
					continue
				}
				m.mu.RLock()
				_, ok := m.active[rec.ID]
				m.mu.RUnlock()
				if !ok {
					go m.resumeOne(rec)
				}
			}
			if m.cfg.SeedAfterComplete {
				m.enforceSeedingLimits(ctx)
			}
		}
	}
}

func (m *Manager) reconcileStaleMagnetConversion(ctx context.Context, rec storage.TorrentRecord, now time.Time) bool {
	if rec.Status != string(StatusMagnetConversion) {
		return false
	}

	m.mu.RLock()
	rt := m.active[rec.ID]
	m.mu.RUnlock()
	if rt != nil {
		if rt.t != nil && rt.t.Info() != nil {
			return false
		}
		if now.Sub(rt.startedAt) <= magnetMetadataTimeout+magnetMetadataStaleGrace {
			return true
		}
		m.setError(ctx, rec.ID, string(StatusMagnetError))
		m.dropRuntimeTorrent(rec.ID, rt.t)
		return true
	}

	if isPlaceholderMagnetConversion(rec) && !rec.AddedAt.IsZero() && now.Sub(rec.AddedAt) > magnetMetadataTimeout+magnetMetadataStaleGrace {
		m.setError(ctx, rec.ID, string(StatusMagnetError))
		return true
	}

	return false
}

func isPlaceholderMagnetConversion(rec storage.TorrentRecord) bool {
	return rec.Name == "" &&
		rec.OriginalName == "" &&
		rec.Bytes == 0 &&
		rec.OriginalBytes == 0 &&
		len(rec.InfoBytes) == 0 &&
		len(rec.Files) == 0
}

func (m *Manager) enforceSeedingLimits(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, rt := range m.active {
		rec, err := m.db.GetTorrent(ctx, id)
		if err != nil || rec.Status != string(StatusDownloaded) {
			continue
		}
		if !m.shouldStopSeeding(rt.t, rec) {
			continue
		}
		rt.t.Drop()
		delete(m.active, id)
	}
}

func (m *Manager) shouldStopSeeding(t *torrent.Torrent, rec *storage.TorrentRecord) bool {
	if m.cfg.SeedRatio > 0 {
		stats := t.Stats()
		downloaded := stats.BytesReadUsefulData.Int64()
		if downloaded > 0 {
			ratio := float64(stats.BytesWrittenData.Int64()) / float64(downloaded)
			if ratio >= m.cfg.SeedRatio {
				return true
			}
		}
	} else if m.cfg.SeedMinutes > 0 && rec.EndedAt != nil {
		deadline := rec.EndedAt.Add(time.Duration(m.cfg.SeedMinutes) * time.Minute)
		if !time.Now().Before(deadline) {
			return true
		}
	}
	return false
}

func (m *Manager) setError(ctx context.Context, id, status string) {
	rec, err := m.db.GetTorrent(ctx, id)
	if err != nil {
		return
	}
	rec.Status = status
	_ = m.db.UpdateTorrent(ctx, *rec)
}

func (m *Manager) selectedTotal(rec *storage.TorrentRecord) int64 {
	var total int64
	for _, f := range rec.Files {
		if f.Selected {
			total += f.Bytes
		}
	}
	return total
}

func (m *Manager) selectedCompleted(rec *storage.TorrentRecord) int64 {
	var done int64
	for _, f := range rec.Files {
		if f.Selected {
			if remoteStoredComplete(f) {
				done += f.Bytes
				continue
			}
			done += f.DownloadedBytes
		}
	}
	return done
}

func (m *Manager) reconcileRemoteStoredComplete(ctx context.Context, rec *storage.TorrentRecord) bool {
	if rec == nil || IsCompletedStatus(rec.Status) || !selectedFilesRemoteStoredComplete(rec) {
		return false
	}

	for i := range rec.Files {
		if rec.Files[i].Selected && remoteStoredComplete(rec.Files[i]) {
			rec.Files[i].DownloadedBytes = rec.Files[i].Bytes
			rec.Files[i].StreamableBytes = rec.Files[i].Bytes
		}
	}

	m.lifecycle.MarkDownloaded(rec, time.Now().UTC())
	m.ensureHostLinks(ctx, rec)
	_ = m.db.UpdateTorrentFiles(ctx, rec.ID, rec.Files)
	_ = m.db.UpdateTorrent(ctx, *rec)

	m.mu.RLock()
	rt := m.active[rec.ID]
	m.mu.RUnlock()
	if rt != nil {
		m.dropRuntimeTorrent(rec.ID, rt.t)
	}

	m.fireDownloadComplete(rec.Name)
	go m.offloadTorrent(context.Background(), rec.ID)
	return true
}

func selectedFilesRemoteStoredComplete(rec *storage.TorrentRecord) bool {
	if rec == nil {
		return false
	}
	var selected bool
	for _, f := range rec.Files {
		if !f.Selected {
			continue
		}
		selected = true
		if !remoteStoredComplete(f) {
			return false
		}
	}
	return selected
}

func remoteStoredComplete(f storage.TorrentFileRecord) bool {
	return f.Bytes > 0 && f.RemoteStored
}

type Stats struct {
	DiskUsed        int64
	DiskQuota       int64
	S3Used          int64
	S3Quota         int64
	S3ObjectCount   int
	S3Enabled       bool
	TorrentCount    int
	ActiveCount     int
	DownloadSpeed   int64
	StatusCounts    map[string]int
	LifecycleCounts map[string]int
}

func (m *Manager) FilesDir() string {
	return m.filesDir
}

func (m *Manager) GetDownloadRateLimitMbps() float64 {
	if m.settings != nil {
		return m.settings.GetDownloadRateLimitMbps()
	}
	return m.cfg.DownloadRateLimitMB
}

func (m *Manager) currentS3Config() objectstore.Config {
	if m.settings != nil {
		return m.settings.S3Config()
	}
	return m.objectStoreCfg
}

func (m *Manager) objectStoreForSettings() (*objectstore.Store, error) {
	cfg := m.currentS3Config()

	m.objectMu.RLock()
	if m.objectStore != nil && m.objectStoreCfg == cfg {
		store := m.objectStore
		m.objectMu.RUnlock()
		return store, nil
	}
	m.objectMu.RUnlock()

	m.objectMu.Lock()
	defer m.objectMu.Unlock()
	if m.objectStore != nil && m.objectStoreCfg == cfg {
		return m.objectStore, nil
	}

	store, err := objectstore.New(cfg)
	if err != nil {
		return nil, err
	}
	m.objectStoreCfg = cfg
	m.objectStore = store
	return store, nil
}

func (m *Manager) diskQuotaBytes() int64 {
	if m.settings != nil {
		return m.settings.DiskQuotaBytes()
	}
	return m.cfg.DiskQuotaBytes()
}

func (m *Manager) objectStorageQuotaBytes() int64 {
	var gb int64
	if m.settings != nil {
		gb = m.settings.GetS3QuotaGB()
	} else {
		gb = m.objectStoreCfg.QuotaGB
	}
	if gb <= 0 {
		return 0
	}
	return gb * 1024 * 1024 * 1024
}

func (m *Manager) ensureObjectStorageQuotaAvailable(ctx context.Context, requiredBytes int64) error {
	if !m.currentS3Config().Enabled {
		return nil
	}
	quota := m.objectStorageQuotaBytes()
	if quota <= 0 {
		return nil
	}
	usage, err := m.db.ObjectStorageReservedUsage(ctx, "")
	if err != nil {
		return err
	}
	if requiredBytes <= 0 {
		if usage.Bytes >= quota {
			return ErrObjectStorageQuotaExceeded
		}
		return nil
	}
	if usage.Bytes+requiredBytes > quota {
		return ErrObjectStorageQuotaExceeded
	}
	return nil
}

func (m *Manager) ensureObjectStorageQuotaForSelection(ctx context.Context, rec *storage.TorrentRecord) error {
	if rec == nil {
		return nil
	}
	quota := m.objectStorageQuotaBytes()
	if quota <= 0 {
		return nil
	}
	var required int64
	for _, f := range rec.Files {
		if f.Selected && !remoteStoredComplete(f) {
			required += f.Bytes
		}
	}
	usage, err := m.db.ObjectStorageReservedUsage(ctx, rec.ID)
	if err != nil {
		return err
	}
	if usage.Bytes+required > quota {
		return ErrObjectStorageQuotaExceeded
	}
	return nil
}

const diskUsageMaxStale = 60 * time.Second

func (m *Manager) reconcileDiskUsage(force bool) {
	m.diskMu.RLock()
	stale := force || m.diskUsedAt.IsZero() || time.Since(m.diskUsedAt) > diskUsageMaxStale
	m.diskMu.RUnlock()
	if !stale {
		return
	}

	used, err := diskusage.DirSize(m.filesDir)
	if err != nil {
		return
	}
	m.diskMu.Lock()
	m.diskUsed = used
	m.diskUsedAt = time.Now()
	m.diskMu.Unlock()
}

func (m *Manager) invalidateDiskUsed() {
	m.diskMu.Lock()
	m.diskUsedAt = time.Time{}
	m.diskMu.Unlock()
}

func (m *Manager) cachedDiskUsed() int64 {
	m.reconcileDiskUsage(false)
	m.diskMu.RLock()
	defer m.diskMu.RUnlock()
	return m.diskUsed
}

func (m *Manager) Stats(ctx context.Context) (Stats, error) {
	used := m.cachedDiskUsed()
	total, err := m.db.CountTorrents(ctx)
	if err != nil {
		return Stats{}, err
	}
	active, err := m.db.CountActiveTorrents(ctx)
	if err != nil {
		return Stats{}, err
	}

	var totalSpeed int64
	items, _ := m.db.ListIncompleteTorrents(ctx)
	for _, rec := range items {
		totalSpeed += rec.Speed
	}

	statusCounts, err := m.db.CountTorrentsByStatus(ctx)
	if err != nil {
		return Stats{}, err
	}
	objectUsage, err := m.db.ObjectStorageUsage(ctx)
	if err != nil {
		return Stats{}, err
	}
	s3Cfg := m.currentS3Config()
	s3Quota := int64(0)
	if s3Cfg.Enabled {
		s3Quota = m.objectStorageQuotaBytes()
	}

	return Stats{
		DiskUsed:        used,
		DiskQuota:       m.diskQuotaBytes(),
		S3Used:          objectUsage.Bytes,
		S3Quota:         s3Quota,
		S3ObjectCount:   objectUsage.Count,
		S3Enabled:       s3Cfg.Enabled,
		TorrentCount:    total,
		ActiveCount:     active,
		DownloadSpeed:   totalSpeed,
		StatusCounts:    statusCounts,
		LifecycleCounts: m.lifecycle.CountsByGroup(statusCounts),
	}, nil
}

func (m *Manager) ObjectStorageUsage(ctx context.Context) (storage.ObjectStorageUsage, error) {
	return m.db.ObjectStorageUsage(ctx)
}

func (m *Manager) ObjectStorageQuotaBytes() int64 {
	if !m.currentS3Config().Enabled {
		return 0
	}
	return m.objectStorageQuotaBytes()
}

func (m *Manager) DeleteCompletedBefore(ctx context.Context, before time.Time) (int, error) {
	items, err := m.db.ListCompletedBefore(ctx, before)
	if err != nil {
		return 0, err
	}
	var removed int
	for _, rec := range items {
		if err := m.Delete(ctx, rec.ID); err == nil {
			removed++
		}
	}
	m.invalidateDiskUsed()
	return removed, nil
}

func (m *Manager) EvictOldestCompleted(ctx context.Context, needBytes int64) (int, error) {
	if needBytes <= 0 {
		return 0, nil
	}
	items, err := m.db.ListCompletedByEndedAt(ctx, 1000)
	if err != nil {
		return 0, err
	}
	var removed int
	var freed int64
	for _, rec := range items {
		if freed >= needBytes {
			break
		}
		size := rec.Bytes
		if size <= 0 {
			size = rec.OriginalBytes
		}
		if err := m.Delete(ctx, rec.ID); err == nil {
			removed++
			freed += size
		}
	}
	m.invalidateDiskUsed()
	return removed, nil
}

func (m *Manager) EvictOldestRemoteStored(ctx context.Context, needBytes int64) (int, error) {
	if needBytes <= 0 {
		return 0, nil
	}
	items, err := m.db.ListCompletedByEndedAt(ctx, 1000)
	if err != nil {
		return 0, err
	}
	var removed int
	var freed int64
	var firstErr error
	for _, item := range items {
		if freed >= needBytes {
			break
		}
		rec, err := m.db.GetTorrent(ctx, item.ID)
		if err != nil {
			continue
		}
		var remoteBytes int64
		for _, f := range rec.Files {
			if remoteStoredComplete(f) {
				remoteBytes += f.Bytes
			}
		}
		if remoteBytes <= 0 {
			continue
		}
		if err := m.deleteRemoteObjects(ctx, rec); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := m.Delete(ctx, rec.ID); err == nil {
			removed++
			freed += remoteBytes
		} else if firstErr == nil {
			firstErr = err
		}
	}
	m.invalidateDiskUsed()
	return removed, firstErr
}

func (m *Manager) PurgeByStatus(ctx context.Context, filter string) (int, error) {
	var statuses []string
	switch filter {
	case "completed":
		statuses = []string{string(StatusDownloaded)}
	case "failed":
		statuses = []string{string(StatusError), string(StatusDead), string(StatusMagnetError)}
	case "active":
		statuses = []string{
			string(StatusDownloading),
			string(StatusQueued),
			string(StatusWaitingFileSelection),
			string(StatusMagnetConversion),
		}
	default:
		return 0, fmt.Errorf("unknown filter: %s", filter)
	}

	statusSet := make(map[string]bool, len(statuses))
	for _, s := range statuses {
		statusSet[s] = true
	}

	items, err := m.List(ctx, 10000)
	if err != nil {
		return 0, err
	}

	var deleted int
	for i := range items {
		if !statusSet[items[i].Status] {
			continue
		}
		if err := m.Delete(ctx, items[i].ID); err == nil {
			deleted++
		}
	}
	return deleted, nil
}

func (m *Manager) Retry(ctx context.Context, torrentID string) error {
	rec, err := m.db.GetTorrent(ctx, torrentID)
	if err != nil {
		return fmt.Errorf("unknown torrent")
	}
	if !isFailedTorrentStatus(rec.Status) {
		return fmt.Errorf("torrent is not in a failed state")
	}
	if err := m.db.ResetTorrentForRetry(ctx, torrentID); err != nil {
		return err
	}
	updated, err := m.db.GetTorrent(ctx, torrentID)
	if err != nil {
		return err
	}
	go m.resumeOne(*updated)
	return nil
}

func isFailedTorrentStatus(status string) bool {
	return IsFailedStatus(status)
}

func (m *Manager) AddTorrentFile(ctx context.Context, data []byte) (*storage.TorrentRecord, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty torrent file")
	}
	if len(data) > 10*1024*1024 {
		return nil, fmt.Errorf("torrent file too large")
	}

	mi, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("invalid torrent file")
	}
	info, err := mi.UnmarshalInfo()
	if err != nil {
		return nil, fmt.Errorf("invalid torrent info")
	}

	infoHash := mi.HashInfoBytes().HexString()
	magnet := mi.Magnet(nil, &info).String()

	if existing, err := m.db.GetTorrentByHash(ctx, infoHash); err == nil && existing != nil {
		m.refreshProgress(ctx, existing)
		if existing.Status == string(StatusDownloaded) {
			m.ensureHostLinks(ctx, existing)
		}
		return existing, nil
	}
	requiredBytes := int64(0)
	if len(info.UpvertedFiles()) <= 1 {
		requiredBytes = info.TotalLength()
	}
	if m.currentS3Config().Enabled && m.objectStorageQuotaBytes() > 0 {
		m.objectQuotaMu.Lock()
		defer m.objectQuotaMu.Unlock()
		if err := m.ensureObjectStorageQuotaAvailable(ctx, requiredBytes); err != nil {
			return nil, err
		}
	}

	id, err := newTorrentID()
	if err != nil {
		return nil, err
	}

	var infoBuf bytes.Buffer
	if err := mi.Write(&infoBuf); err != nil {
		return nil, err
	}

	rec := storage.TorrentRecord{
		ID:           id,
		InfoHash:     infoHash,
		Magnet:       magnet,
		Name:         info.Name,
		OriginalName: info.Name,
		Status:       string(StatusMagnetConversion),
		InfoBytes:    infoBuf.Bytes(),
		AddedAt:      time.Now().UTC(),
	}

	if err := m.db.CreateTorrent(ctx, rec); err != nil {
		return nil, err
	}

	go m.processTorrentMetainfo(id, mi)
	return m.db.GetTorrent(ctx, id)
}

func newTorrentID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(b)), nil
}

func infoHashFromMagnet(magnet string) (string, error) {
	mi, err := metainfo.ParseMagnetUri(magnet)
	if err != nil {
		return "", err
	}
	return mi.InfoHash.HexString(), nil
}

func extractLinkID(hostLink string) string {
	hostLink = strings.TrimSpace(hostLink)
	if i := strings.LastIndex(hostLink, "/d/"); i >= 0 {
		return strings.TrimSpace(hostLink[i+3:])
	}
	if i := strings.LastIndex(hostLink, "/"); i >= 0 {
		return strings.TrimSpace(hostLink[i+1:])
	}
	return ""
}
