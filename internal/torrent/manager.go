package torrent

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	tstorage "github.com/anacrolix/torrent/storage"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/diskusage"
	"github.com/debridnest/debridnest/internal/links"
	"github.com/debridnest/debridnest/internal/storage"
)

type Manager struct {
	cfg     config.Config
	db      *storage.DB
	signer  *links.Signer
	client  *torrent.Client
	mu      sync.RWMutex
	active  map[string]*runtimeTorrent
	filesDir string
}

type runtimeTorrent struct {
	id   string
	t    *torrent.Torrent
	done chan struct{}
}

func NewManager(cfg config.Config, db *storage.DB, signer *links.Signer) (*Manager, error) {
	filesDir := filepath.Join(cfg.DataDir, "files")
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
	if cfg.SeedAfterComplete {
		clientCfg.Seed = true
		clientCfg.NoUpload = false
	} else {
		clientCfg.Seed = false
		clientCfg.NoUpload = true
	}
	clientCfg.DefaultStorage = tstorage.NewFileByInfoHash(filesDir)

	client, err := torrent.NewClient(clientCfg)
	if err != nil {
		return nil, fmt.Errorf("create torrent client: %w", err)
	}

	m := &Manager{
		cfg:      cfg,
		db:       db,
		signer:   signer,
		client:   client,
		active:   make(map[string]*runtimeTorrent),
		filesDir: filesDir,
	}

	if err := m.resumeIncomplete(context.Background()); err != nil {
		client.Close()
		return nil, err
	}

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
	if ih, err := infoHashFromMagnet(magnet); err == nil {
		if existing, err := m.db.GetTorrentByHash(ctx, ih); err == nil && existing != nil {
			m.refreshProgress(ctx, existing)
			if existing.Status == "downloaded" {
				m.ensureHostLinks(ctx, existing)
				return existing, nil
			}
			m.mu.RLock()
			_, active := m.active[existing.ID]
			m.mu.RUnlock()
			if !active {
				go m.resumeOne(*existing)
			}
			return existing, nil
		}
	}

	id, err := newTorrentID()
	if err != nil {
		return nil, err
	}

	rec := storage.TorrentRecord{
		ID:       id,
		Magnet:   magnet,
		Status:   "magnet_conversion",
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

	selectedIDs, err := parseFilesSpec(filesSpec, len(rec.Files))
	if err != nil {
		return err
	}

	selectedSet := map[int]bool{}
	for _, id := range selectedIDs {
		selectedSet[id] = true
	}

	var selectedBytes int64
	for i := range rec.Files {
		rec.Files[i].Selected = selectedSet[rec.Files[i].ID]
		if rec.Files[i].Selected {
			selectedBytes += rec.Files[i].Bytes
		}
	}
	rec.Bytes = selectedBytes
	rec.Status = "queued"
	if err := m.db.UpdateTorrentFiles(ctx, torrentID, rec.Files); err != nil {
		return err
	}
	if err := m.db.UpdateTorrent(ctx, *rec); err != nil {
		return err
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
	m.mu.Lock()
	if rt, ok := m.active[torrentID]; ok {
		rt.t.Drop()
		delete(m.active, torrentID)
	}
	m.mu.Unlock()

	rec, err := m.db.GetTorrent(ctx, torrentID)
	if err != nil {
		return err
	}

	dir := filepath.Join(m.filesDir, rec.InfoHash)
	_ = os.RemoveAll(dir)
	return m.db.DeleteTorrent(ctx, torrentID)
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
	for _, raw := range hashes {
		hash := normalizeInfoHash(raw)
		if hash == "" {
			continue
		}
		rec, err := m.db.GetTorrentByHash(ctx, hash)
		if err != nil || rec.Status != "downloaded" {
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

func (m *Manager) processMagnet(id, magnet string) {
	ctx := context.Background()
	t, err := m.client.AddMagnet(magnet)
	if err != nil {
		m.setError(ctx, id, "magnet_error")
		return
	}

	rt := &runtimeTorrent{id: id, t: t, done: make(chan struct{})}
	m.mu.Lock()
	m.active[id] = rt
	m.mu.Unlock()

	<-t.GotInfo()

	rec, err := m.db.GetTorrent(ctx, id)
	if err != nil {
		return
	}

	mi := t.Metainfo()
	var infoBuf bytes.Buffer
	if err := mi.Write(&infoBuf); err != nil {
		m.setError(ctx, id, "magnet_error")
		return
	}
	infoBytes := infoBuf.Bytes()
	rec.InfoHash = t.InfoHash().HexString()
	rec.InfoBytes = infoBytes
	rec.OriginalName = t.Name()
	rec.Name = t.Name()
	rec.Status = "waiting_files_selection"

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

	go m.autoSelectAfterDelay(id)

	m.trackTorrent(id, t)
}

func (m *Manager) autoSelectAfterDelay(id string) {
	if m.cfg.AutoSelectAfter <= 0 {
		return
	}
	time.Sleep(m.cfg.AutoSelectAfter)

	ctx := context.Background()
	rec, err := m.db.GetTorrent(ctx, id)
	if err != nil || rec.Status != "waiting_files_selection" {
		return
	}

	best := pickLargestVideo(rec.Files)
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

	for {
		select {
		case <-ticker.C:
			rec, err := m.db.GetTorrent(ctx, id)
			if err != nil {
				return
			}

			m.syncRuntimeFiles(t, rec)
			completed := m.selectedCompleted(rec)
			total := m.selectedTotal(rec)
			if total > 0 {
				rec.Progress = int(completed * 100 / total)
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
			rec.Seeders = stats.ActivePeers

			if completed >= total && total > 0 {
				rec.Status = "downloaded"
				rec.Progress = 100
				now := time.Now().UTC()
				rec.EndedAt = &now
				m.refreshStreamLinks(ctx, rec)
			} else if rec.Status == "queued" || rec.Status == "downloading" {
				rec.Status = "downloading"
				m.refreshStreamLinks(ctx, rec)
			}

			_ = m.db.UpdateTorrentFiles(ctx, id, rec.Files)
			_ = m.db.UpdateTorrent(ctx, *rec)

			if rec.Status == "downloaded" {
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
		rec.Files[i].DownloadedBytes = f.BytesCompleted()
		rec.Files[i].DiskPath = filepath.Join(m.filesDir, rec.InfoHash, filepath.FromSlash(f.Path()))
	}
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
	m.mu.RLock()
	rt := m.active[rec.ID]
	m.mu.RUnlock()
	if rt == nil {
		if rec.Status == "downloaded" || m.isStreamable(rec) {
			m.refreshStreamLinks(ctx, rec)
		}
		return
	}

	if rt.t.Info() == nil {
		rec.Status = "magnet_conversion"
		_ = m.db.UpdateTorrent(ctx, *rec)
		return
	}

	if rec.InfoHash == "" {
		rec.InfoHash = rt.t.InfoHash().HexString()
	}

	m.syncRuntimeFiles(rt.t, rec)
	total := m.selectedTotal(rec)
	completed := m.selectedCompleted(rec)
	if total > 0 {
		rec.Progress = int(completed * 100 / total)
	}
	if completed >= total && total > 0 {
		rec.Status = "downloaded"
		rec.Progress = 100
		if rec.EndedAt == nil {
			now := time.Now().UTC()
			rec.EndedAt = &now
		}
		m.refreshStreamLinks(ctx, rec)
	} else if rec.Status != "waiting_files_selection" && rec.Status != "magnet_conversion" {
		rec.Status = "downloading"
		m.refreshStreamLinks(ctx, rec)
	}
	stats := rt.t.Stats()
	rec.Seeders = stats.ActivePeers
	_ = m.db.UpdateTorrentFiles(ctx, rec.ID, rec.Files)
	_ = m.db.UpdateTorrent(ctx, *rec)
}

func (m *Manager) ensureHostLinks(ctx context.Context, rec *storage.TorrentRecord) {
	var linkIDs []string
	for _, f := range rec.Files {
		if !f.Selected {
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
	for _, rec := range items {
		go m.resumeOne(rec)
	}
	return nil
}

func (m *Manager) resumeOne(rec storage.TorrentRecord) {
	ctx := context.Background()
	t, err := m.client.AddMagnet(rec.Magnet)
	if err != nil {
		return
	}
	<-t.GotInfo()

	rt := &runtimeTorrent{id: rec.ID, t: t, done: make(chan struct{})}
	m.mu.Lock()
	m.active[rec.ID] = rt
	m.mu.Unlock()

	m.applySelection(rec.ID, t, rec.Files)
	go m.trackTorrent(rec.ID, t)

	updated, err := m.db.GetTorrent(ctx, rec.ID)
	if err == nil && updated.Status == "waiting_files_selection" {
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
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO torrent_files (id, torrent_id, path, bytes, selected, downloaded_bytes, disk_path)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			f.ID, torrentID, f.Path, f.Bytes, selected, f.DownloadedBytes, f.DiskPath,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (m *Manager) backgroundLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		ctx := context.Background()
		items, err := m.db.ListIncompleteTorrents(ctx)
		if err != nil {
			continue
		}
		for _, rec := range items {
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

func (m *Manager) enforceSeedingLimits(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, rt := range m.active {
		rec, err := m.db.GetTorrent(ctx, id)
		if err != nil || rec.Status != "downloaded" {
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
			done += f.DownloadedBytes
		}
	}
	return done
}

type Stats struct {
	DiskUsed      int64
	DiskQuota     int64
	TorrentCount  int
	ActiveCount   int
	DownloadSpeed int64
	StatusCounts  map[string]int
}

func (m *Manager) FilesDir() string {
	return m.filesDir
}

func (m *Manager) Stats(ctx context.Context) (Stats, error) {
	used, err := diskusage.DirSize(m.filesDir)
	if err != nil {
		return Stats{}, err
	}
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

	return Stats{
		DiskUsed:      used,
		DiskQuota:     m.cfg.DiskQuotaBytes(),
		TorrentCount:  total,
		ActiveCount:   active,
		DownloadSpeed: totalSpeed,
		StatusCounts:  statusCounts,
	}, nil
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
	return removed, nil
}

func (m *Manager) PurgeByStatus(ctx context.Context, filter string) (int, error) {
	var statuses []string
	switch filter {
	case "completed":
		statuses = []string{"downloaded"}
	case "failed":
		statuses = []string{"error", "dead", "magnet_error"}
	case "active":
		statuses = []string{"downloading", "queued", "waiting_files_selection", "magnet_conversion"}
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
	if rec.Status != "error" && rec.Status != "magnet_error" && rec.Status != "dead" {
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
		if existing.Status == "downloaded" {
			m.ensureHostLinks(ctx, existing)
		}
		return existing, nil
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
		Status:       "magnet_conversion",
		InfoBytes:    infoBuf.Bytes(),
		AddedAt:      time.Now().UTC(),
	}

	if err := m.db.CreateTorrent(ctx, rec); err != nil {
		return nil, err
	}

	go m.processMagnet(id, magnet)
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

func parseFilesSpec(spec string, fileCount int) ([]int, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("empty files spec")
	}
	if strings.EqualFold(spec, "all") {
		ids := make([]int, fileCount)
		for i := range ids {
			ids[i] = i + 1
		}
		return ids, nil
	}
	parts := strings.Split(spec, ",")
	var ids []int
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var id int
		if _, err := fmt.Sscanf(part, "%d", &id); err != nil || id < 1 || id > fileCount {
			return nil, fmt.Errorf("invalid file id: %s", part)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func pickLargestVideo(files []storage.TorrentFileRecord) int {
	exts := map[string]bool{".mkv": true, ".mp4": true, ".avi": true, ".m4v": true, ".webm": true}
	var bestID int
	var bestSize int64
	for _, f := range files {
		ext := strings.ToLower(filepath.Ext(f.Path))
		if !exts[ext] {
			continue
		}
		if f.Bytes > bestSize {
			bestSize = f.Bytes
			bestID = f.ID
		}
	}
	return bestID
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
