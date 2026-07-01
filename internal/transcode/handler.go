package transcode

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/links"
	"github.com/debridnest/debridnest/internal/storage"
	"github.com/debridnest/debridnest/internal/torrent"
)

type handler struct {
	cfg     config.Config
	manager *torrent.Manager
	signer  *links.Signer
	jobs    sync.Map // key: jobKey -> *hlsJob

	serverCtx  context.Context
	runHLS     hlsRunner
	jobTimeout time.Duration
}

func newHandler(cfg config.Config, manager *torrent.Manager, signer *links.Signer) *handler {
	return &handler{
		cfg:        cfg,
		manager:    manager,
		signer:     signer,
		serverCtx:  context.Background(),
		runHLS:     runHLSJob,
		jobTimeout: hlsJobTimeout,
	}
}

func (h *handler) serveMaster(w http.ResponseWriter, r *http.Request) {
	torrentID := chi.URLParam(r, "torrentID")
	fileID, err := strconv.Atoi(chi.URLParam(r, "fileID"))
	if err != nil || torrentID == "" {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	expiresUnix, ok := h.verifySignedAsset(w, r, torrentID, fileID, "master.m3u8")
	if !ok {
		return
	}

	rec, file, err := h.resolveFile(r.Context(), torrentID, fileID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !torrent.IsCompletedStatus(rec.Status) {
		http.Error(w, "file not ready", http.StatusServiceUnavailable)
		return
	}

	job, err := h.ensureJob(rec, file)
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
	indexURL := h.signer.SignHLSAsset(torrentID, fileID, "index.m3u8", time.Unix(expiresUnix, 0))
	_, _ = fmt.Fprintf(w, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-STREAM-INF:BANDWIDTH=5000000\n%s\n", indexURL)
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
	expiresUnix, ok := h.verifySignedAsset(w, r, torrentID, fileID, asset)
	if !ok {
		return
	}

	rec, file, err := h.resolveFile(r.Context(), torrentID, fileID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if !torrent.IsCompletedStatus(rec.Status) {
		http.Error(w, "file not ready", http.StatusServiceUnavailable)
		return
	}

	job, err := h.ensureJob(rec, file)
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
	if strings.HasSuffix(asset, ".m3u8") {
		h.serveMediaPlaylist(w, cleanPath, torrentID, fileID, expiresUnix)
		return
	}

	switch {
	case strings.HasSuffix(asset, ".ts"):
		w.Header().Set("Content-Type", "video/mp2t")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("Cache-Control", "public, max-age=3600")
	f, err := os.Open(cleanPath)
	if err != nil {
		http.Error(w, "asset not found", http.StatusNotFound)
		return
	}
	var limited interface {
		io.Reader
		io.Seeker
		io.Closer
	} = f
	if limiter := h.downloadRateLimiter(); limiter != nil {
		limited = limiter.ReadSeekCloser(f)
	}
	defer limited.Close()

	stat, err := f.Stat()
	if err != nil {
		http.Error(w, "asset not found", http.StatusNotFound)
		return
	}
	http.ServeContent(w, r, filepath.Base(asset), stat.ModTime(), limited)
}

func (h *handler) verifySignedAsset(w http.ResponseWriter, r *http.Request, torrentID string, fileID int, asset string) (int64, bool) {
	expiresRaw := r.URL.Query().Get("expires")
	sig := r.URL.Query().Get("sig")
	if expiresRaw == "" || sig == "" {
		http.Error(w, "missing signature", http.StatusForbidden)
		return 0, false
	}

	expiresUnix, err := strconv.ParseInt(expiresRaw, 10, 64)
	if err != nil || !h.signer.VerifyHLSAsset(torrentID, fileID, asset, expiresUnix, sig) {
		http.Error(w, "invalid or expired link", http.StatusForbidden)
		return 0, false
	}
	return expiresUnix, true
}

func (h *handler) serveMediaPlaylist(w http.ResponseWriter, path string, torrentID string, fileID int, expiresUnix int64) {
	body, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, "asset not found", http.StatusNotFound)
		return
	}

	rewritten := h.rewritePlaylist(string(body), torrentID, fileID, time.Unix(expiresUnix, 0))
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	reader := io.Reader(strings.NewReader(rewritten))
	if limiter := h.downloadRateLimiter(); limiter != nil {
		reader = limiter.Reader(reader)
	}
	_, _ = io.Copy(w, reader)
}

func (h *handler) downloadRateLimiter() *links.RateLimiter {
	limit := h.cfg.DownloadRateLimitMB
	if h.manager != nil {
		limit = h.manager.GetDownloadRateLimitMbps()
	}
	return links.NewRateLimiter(limit)
}

func (h *handler) rewritePlaylist(body string, torrentID string, fileID int, expires time.Time) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		lines[i] = h.rewritePlaylistLine(line, torrentID, fileID, expires)
	}
	return strings.Join(lines, "\n")
}

func (h *handler) rewritePlaylistLine(line string, torrentID string, fileID int, expires time.Time) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return line
	}
	if strings.HasPrefix(trimmed, "#") {
		return h.rewriteURIAttribute(line, torrentID, fileID, expires)
	}
	return h.signPlaylistURI(line, torrentID, fileID, expires)
}

func (h *handler) rewriteURIAttribute(line string, torrentID string, fileID int, expires time.Time) string {
	const marker = `URI="`
	start := strings.Index(line, marker)
	if start < 0 {
		return line
	}
	start += len(marker)
	end := strings.Index(line[start:], `"`)
	if end < 0 {
		return line
	}
	end += start
	return line[:start] + h.signPlaylistURI(line[start:end], torrentID, fileID, expires) + line[end:]
}

func (h *handler) signPlaylistURI(raw string, torrentID string, fileID int, expires time.Time) string {
	trimmed := strings.TrimSpace(raw)
	u, err := url.Parse(trimmed)
	if err != nil || u.IsAbs() || strings.HasPrefix(trimmed, "/") || u.Path == "" {
		return raw
	}
	return h.signer.SignHLSAsset(torrentID, fileID, u.Path, expires)
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

func (h *handler) ensureJob(rec *storage.TorrentRecord, file *storage.TorrentFileRecord) (*hlsJob, error) {
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

	go h.runJob(key, job)
	return job, nil
}

func (h *handler) runJob(key string, job *hlsJob) {
	defer close(job.done)

	ctx, cancel := context.WithTimeout(h.baseContext(), h.timeout())
	defer cancel()

	if err := h.runner()(ctx, job); err != nil {
		if cleanupErr := os.RemoveAll(job.outDir); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("cleanup HLS output: %w", cleanupErr))
		}
		job.setErr(err)
		h.jobs.CompareAndDelete(key, job)
	}
}

func (h *handler) baseContext() context.Context {
	if h.serverCtx != nil {
		return h.serverCtx
	}
	return context.Background()
}

func (h *handler) runner() hlsRunner {
	if h.runHLS != nil {
		return h.runHLS
	}
	return runHLSJob
}

func (h *handler) timeout() time.Duration {
	if h.jobTimeout > 0 {
		return h.jobTimeout
	}
	return hlsJobTimeout
}
