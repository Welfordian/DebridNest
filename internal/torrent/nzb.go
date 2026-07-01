package torrent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/debridnest/debridnest/internal/nzbget"
	"github.com/debridnest/debridnest/internal/storage"
)

var ErrNZBDisabled = fmt.Errorf("nzb downloads are not configured")
var ErrInvalidNZB = fmt.Errorf("invalid nzb url")

type nzbMeta struct {
	Source string `json:"source"`
	NZBID  int    `json:"nzbid"`
	NZBURL string `json:"nzburl"`
}

func isNZBRecord(rec *storage.TorrentRecord) bool {
	if rec == nil {
		return false
	}
	var meta nzbMeta
	if err := json.Unmarshal(rec.InfoBytes, &meta); err != nil {
		return false
	}
	return meta.Source == "nzb"
}

func parseNZBMeta(raw []byte) nzbMeta {
	var meta nzbMeta
	_ = json.Unmarshal(raw, &meta)
	return meta
}

func encodeNZBMeta(meta nzbMeta) []byte {
	raw, _ := json.Marshal(meta)
	return raw
}

func nzbInfoHash(nzbURL string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(nzbURL)))
	return hex.EncodeToString(sum[:20])
}

func (m *Manager) NZBEnabled() bool {
	return m.nzbget != nil
}

func (m *Manager) AddNZB(ctx context.Context, nzbURL, name string) (*storage.TorrentRecord, error) {
	if m.nzbget == nil {
		return nil, ErrNZBDisabled
	}
	nzbURL = strings.TrimSpace(nzbURL)
	if nzbURL == "" || !strings.HasPrefix(strings.ToLower(nzbURL), "http") {
		return nil, ErrInvalidNZB
	}

	ih := nzbInfoHash(nzbURL)
	if existing, err := m.db.GetTorrentByHash(ctx, ih); err == nil && existing != nil {
		if isNZBRecord(existing) {
			m.refreshNZBProgress(ctx, existing)
			return m.db.GetTorrent(ctx, existing.ID)
		}
	}

	id, err := newTorrentID()
	if err != nil {
		return nil, err
	}

	displayName := strings.TrimSpace(name)
	if displayName == "" {
		displayName = filepath.Base(nzbURL)
	}

	rec := storage.TorrentRecord{
		ID:           id,
		InfoHash:     ih,
		Magnet:       nzbURL,
		Name:         displayName,
		OriginalName: displayName,
		Status:       "queued",
		AddedAt:      time.Now().UTC(),
		Progress:     0,
		InfoBytes:    encodeNZBMeta(nzbMeta{Source: "nzb", NZBURL: nzbURL}),
	}
	if err := m.db.CreateTorrent(ctx, rec); err != nil {
		return nil, err
	}

	go m.processNZB(id, nzbURL, displayName)
	return m.db.GetTorrent(ctx, id)
}

func (m *Manager) processNZB(id, nzbURL, name string) {
	ctx := context.Background()
	filename := sanitizeNZBFilename(name)

	nzbID, err := m.nzbget.AppendURL(ctx, filename, nzbURL, "debridnest")
	if err != nil {
		m.setError(ctx, id, "error")
		return
	}

	rec, err := m.db.GetTorrent(ctx, id)
	if err != nil {
		return
	}
	meta := parseNZBMeta(rec.InfoBytes)
	meta.Source = "nzb"
	meta.NZBID = nzbID
	meta.NZBURL = nzbURL
	rec.InfoBytes = encodeNZBMeta(meta)
	rec.Status = "downloading"
	_ = m.db.UpdateTorrent(ctx, *rec)

	go m.trackNZBJob(id, nzbID)
}

func (m *Manager) trackNZBJob(id string, nzbID int) {
	ctx := context.Background()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rec, err := m.db.GetTorrent(ctx, id)
			if err != nil || !isNZBRecord(rec) {
				return
			}

			group, ok, err := m.lookupNZBGroup(ctx, nzbID)
			if err != nil {
				continue
			}
			if !ok {
				continue
			}

			prevStatus := rec.Status
			rec.Progress = nzbProgress(group)
			switch {
			case group.Status == nzbget.StatusFailed:
				rec.Status = "error"
			case group.Status == nzbget.StatusFinished:
				if err := m.importNZBFiles(ctx, rec, nzbID, group.DestDir); err != nil {
					rec.Status = "error"
				} else {
					rec.Status = "downloaded"
					rec.Progress = 100
					now := time.Now().UTC()
					rec.EndedAt = &now
				}
			default:
				rec.Status = "downloading"
			}

			_ = m.db.UpdateTorrent(ctx, *rec)
			if rec.Status == "downloaded" {
				m.refreshStreamLinks(ctx, rec)
				if prevStatus != "downloaded" {
					m.fireDownloadComplete(rec.Name)
					go m.offloadTorrent(context.Background(), rec.ID)
				}
				return
			}
			if rec.Status == "error" {
				return
			}
		}
	}
}

func (m *Manager) lookupNZBGroup(ctx context.Context, nzbID int) (nzbget.Group, bool, error) {
	groups, err := m.nzbget.ListGroups(ctx)
	if err != nil {
		return nzbget.Group{}, false, err
	}
	for _, g := range groups {
		if g.NZBID == nzbID {
			return g, true, nil
		}
	}
	history, err := m.nzbget.History(ctx, 50)
	if err != nil {
		return nzbget.Group{}, false, nil
	}
	for _, g := range history {
		if g.NZBID == nzbID {
			return g, true, nil
		}
	}
	return nzbget.Group{}, false, nil
}

func (m *Manager) importNZBFiles(ctx context.Context, rec *storage.TorrentRecord, nzbID int, destDir string) error {
	files, err := m.nzbget.ListFiles(ctx, nzbID)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return m.importNZBFromDisk(ctx, rec, destDir)
	}

	var out []storage.TorrentFileRecord
	var totalBytes int64
	for i, f := range files {
		if !isVideoOrArchive(f.FileName) {
			continue
		}
		diskPath := filepath.Join(f.DestDir, f.FileName)
		if f.DestDir == "" {
			diskPath = filepath.Join(destDir, f.FileName)
		}
		size := f.FileSize
		if st, err := os.Stat(diskPath); err == nil {
			size = st.Size()
		}
		out = append(out, storage.TorrentFileRecord{
			ID:              i + 1,
			TorrentID:       rec.ID,
			Path:            "/" + filepath.ToSlash(f.FileName),
			Bytes:           size,
			Selected:        true,
			DownloadedBytes: size,
			DiskPath:        diskPath,
		})
		totalBytes += size
	}
	if len(out) == 0 {
		return m.importNZBFromDisk(ctx, rec, destDir)
	}

	rec.Files = out
	rec.Bytes = totalBytes
	rec.OriginalBytes = totalBytes
	if err := m.replaceTorrentFiles(ctx, rec.ID, out); err != nil {
		return err
	}
	return m.db.UpdateTorrent(ctx, *rec)
}

func (m *Manager) importNZBFromDisk(ctx context.Context, rec *storage.TorrentRecord, destDir string) error {
	if destDir == "" {
		destDir = filepath.Join(m.filesDir, "nzb", "debridnest")
	}
	entries, err := os.ReadDir(destDir)
	if err != nil {
		return fmt.Errorf("no nzb files found in %s: %w", destDir, err)
	}

	var out []storage.TorrentFileRecord
	var totalBytes int64
	id := 1
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if !isVideoOrArchive(name) {
			continue
		}
		diskPath := filepath.Join(destDir, name)
		st, err := ent.Info()
		if err != nil {
			continue
		}
		size := st.Size()
		out = append(out, storage.TorrentFileRecord{
			ID:              id,
			TorrentID:       rec.ID,
			Path:            "/" + filepath.ToSlash(name),
			Bytes:           size,
			Selected:        true,
			DownloadedBytes: size,
			DiskPath:        diskPath,
		})
		totalBytes += size
		id++
	}
	if len(out) == 0 {
		return fmt.Errorf("no streamable files found after nzb download")
	}
	rec.Files = out
	rec.Bytes = totalBytes
	rec.OriginalBytes = totalBytes
	if err := m.replaceTorrentFiles(ctx, rec.ID, out); err != nil {
		return err
	}
	return m.db.UpdateTorrent(ctx, *rec)
}

func (m *Manager) refreshNZBProgress(ctx context.Context, rec *storage.TorrentRecord) {
	if !isNZBRecord(rec) || m.nzbget == nil {
		return
	}
	meta := parseNZBMeta(rec.InfoBytes)
	if meta.NZBID <= 0 {
		return
	}
	group, ok, err := m.lookupNZBGroup(ctx, meta.NZBID)
	if err != nil || !ok {
		if rec.Status != "downloaded" {
			go m.trackNZBJob(rec.ID, meta.NZBID)
		}
		return
	}
	rec.Progress = nzbProgress(group)
	if group.Status == nzbget.StatusFinished {
		_ = m.importNZBFiles(ctx, rec, meta.NZBID, group.DestDir)
		rec.Status = "downloaded"
		rec.Progress = 100
		now := time.Now().UTC()
		rec.EndedAt = &now
	} else if group.Status == nzbget.StatusFailed {
		rec.Status = "error"
	} else {
		rec.Status = "downloading"
	}
	_ = m.db.UpdateTorrent(ctx, *rec)
	if rec.Status == "downloaded" {
		m.refreshStreamLinks(ctx, rec)
	}
}

func (m *Manager) resumeNZBJobs(ctx context.Context) {
	items, err := m.db.ListIncompleteTorrents(ctx)
	if err != nil {
		return
	}
	for _, rec := range items {
		if !isNZBRecord(&rec) {
			continue
		}
		meta := parseNZBMeta(rec.InfoBytes)
		if meta.NZBID <= 0 {
			if meta.NZBURL != "" {
				go m.processNZB(rec.ID, meta.NZBURL, rec.Name)
			}
			continue
		}
		go m.trackNZBJob(rec.ID, meta.NZBID)
	}
}

func (m *Manager) cancelNZBJob(ctx context.Context, rec *storage.TorrentRecord) {
	if !isNZBRecord(rec) || m.nzbget == nil {
		return
	}
	meta := parseNZBMeta(rec.InfoBytes)
	if meta.NZBID > 0 {
		_ = m.nzbget.GroupDelete(ctx, meta.NZBID)
	}
}

func nzbProgress(group nzbget.Group) int {
	if group.Progress > 0 {
		return group.Progress / 10
	}
	if group.FileSizeMB <= 0 {
		return 0
	}
	done := group.FileSizeMB - group.RemainingSizeMB
	if done < 0 {
		done = 0
	}
	return int(float64(done) / float64(group.FileSizeMB) * 100)
}

func sanitizeNZBFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "download.nzb"
	}
	replacer := strings.NewReplacer("/", "-", "\\", "-", ":", "-")
	name = replacer.Replace(name)
	if len(name) > 120 {
		name = name[:120]
	}
	return name + ".nzb"
}

func isVideoOrArchive(name string) bool {
	lower := strings.ToLower(name)
	switch filepath.Ext(lower) {
	case ".mkv", ".mp4", ".avi", ".webm", ".mov", ".m4v", ".wmv", ".flv", ".ts", ".m2ts":
		return true
	default:
		return false
	}
}
