package transcode

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/storage"
	"github.com/debridnest/debridnest/internal/torrent"
)

type handler struct {
	cfg     config.Config
	manager *torrent.Manager
	jobs    sync.Map // key: jobKey -> *hlsJob
}

func newHandler(cfg config.Config, manager *torrent.Manager) *handler {
	return &handler{cfg: cfg, manager: manager}
}

func (h *handler) serveMaster(w http.ResponseWriter, r *http.Request) {
	torrentID := chi.URLParam(r, "torrentID")
	fileID, err := strconv.Atoi(chi.URLParam(r, "fileID"))
	if err != nil || torrentID == "" {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	rec, file, err := h.resolveFile(r.Context(), torrentID, fileID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if rec.Status != "downloaded" {
		http.Error(w, "file not ready", http.StatusServiceUnavailable)
		return
	}

	job, err := h.ensureJob(r.Context(), rec, file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := job.waitReady(r.Context(), 30*time.Second); err != nil {
		http.Error(w, "transcode not ready", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = fmt.Fprintf(w, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-STREAM-INF:BANDWIDTH=5000000\nindex.m3u8\n")
}

func (h *handler) serveAsset(w http.ResponseWriter, r *http.Request) {
	torrentID := chi.URLParam(r, "torrentID")
	fileID, err := strconv.Atoi(chi.URLParam(r, "fileID"))
	if err != nil || torrentID == "" {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	asset := strings.TrimPrefix(chi.URLParam(r, "*"), "/")
	if asset == "" || strings.Contains(asset, "..") {
		http.Error(w, "invalid asset", http.StatusBadRequest)
		return
	}

	rec, file, err := h.resolveFile(r.Context(), torrentID, fileID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if rec.Status != "downloaded" {
		http.Error(w, "file not ready", http.StatusServiceUnavailable)
		return
	}

	job, err := h.ensureJob(r.Context(), rec, file)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := job.waitReady(r.Context(), 30*time.Second); err != nil {
		http.Error(w, "transcode not ready", http.StatusServiceUnavailable)
		return
	}

	path := filepath.Join(job.outDir, filepath.FromSlash(asset))
	cleanOut := filepath.Clean(job.outDir)
	cleanPath := filepath.Clean(path)
	if cleanPath != cleanOut && !strings.HasPrefix(cleanPath, cleanOut+string(os.PathSeparator)) {
		http.Error(w, "invalid asset", http.StatusBadRequest)
		return
	}

	switch {
	case strings.HasSuffix(asset, ".m3u8"):
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	case strings.HasSuffix(asset, ".ts"):
		w.Header().Set("Content-Type", "video/mp2t")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	http.ServeFile(w, r, path)
}

func (h *handler) resolveFile(ctx context.Context, torrentID string, fileID int) (*storage.TorrentRecord, *storage.TorrentFileRecord, error) {
	rec, err := h.manager.Get(ctx, torrentID)
	if err != nil {
		return nil, nil, fmt.Errorf("torrent not found")
	}
	for i := range rec.Files {
		if rec.Files[i].ID == fileID {
			if !rec.Files[i].Selected {
				return nil, nil, fmt.Errorf("file not found")
			}
			return rec, &rec.Files[i], nil
		}
	}
	return nil, nil, fmt.Errorf("file not found")
}

func (h *handler) ensureJob(ctx context.Context, rec *storage.TorrentRecord, file *storage.TorrentFileRecord) (*hlsJob, error) {
	key := rec.ID + "/" + strconv.Itoa(file.ID)
	if existing, ok := h.jobs.Load(key); ok {
		return existing.(*hlsJob), nil
	}

	outDir := filepath.Join(h.cfg.DataDir, "hls", rec.ID, strconv.Itoa(file.ID))
	job := &hlsJob{
		outDir:  outDir,
		srcPath: file.DiskPath,
		done:    make(chan struct{}),
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}

	actual, loaded := h.jobs.LoadOrStore(key, job)
	if loaded {
		return actual.(*hlsJob), nil
	}

	go job.run(ctx)
	return job, nil
}
