package rd

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/debridnest/debridnest/internal/auth"
	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/links"
	"github.com/debridnest/debridnest/internal/metrics"
	"github.com/debridnest/debridnest/internal/storage"
	torrentmgr "github.com/debridnest/debridnest/internal/torrent"
	"github.com/go-chi/chi/v5"
)

type Handler struct {
	cfg     config.Config
	manager *torrentmgr.Manager
	signer  *links.Signer
	metrics *metrics.Collector
	auth    *auth.Service
}

func NewHandler(cfg config.Config, manager *torrentmgr.Manager, signer *links.Signer, m *metrics.Collector, authSvc *auth.Service) *Handler {
	return &Handler{
		cfg:     cfg,
		manager: manager,
		signer:  signer,
		metrics: m,
		auth:    authSvc,
	}
}

func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(AuthMiddleware(h.auth))
	r.Use(corsMiddleware())

	r.Get("/user", h.getUser)
	r.Route("/torrents", func(r chi.Router) {
		r.Get("/", h.listTorrents)
		r.Post("/addMagnet", h.addMagnet)
		r.Post("/addTorrent", h.addTorrent)
		r.Get("/info/{id}", h.getTorrentInfo)
		r.Get("/instantAvailability/*", h.instantAvailability)
		r.Post("/selectFiles/{id}", h.selectFiles)
		r.Delete("/delete/{id}", h.deleteTorrent)
	})
	r.Post("/unrestrict/link", h.unrestrictLink)
	r.Get("/downloads", h.listDownloads)

	r.Get("/d/{linkID}", h.hostLinkRedirect)
	r.Handle("/dl/*", http.HandlerFunc(h.serveDownloadWildcard))

	return r
}

func (h *Handler) getUser(w http.ResponseWriter, r *http.Request) {
	expiration := time.Now().UTC().Add(365 * 24 * time.Hour)
	writeJSON(w, http.StatusOK, map[string]any{
		"id":         1,
		"username":   "debridnest",
		"email":      "debridnest@localhost",
		"points":     0,
		"avatar":     "",
		"type":       "premium",
		"premium":    31536000,
		"expiration": expiration.Format("2006-01-02T15:04:05.000Z"),
	})
}

func (h *Handler) addMagnet(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	magnet := strings.TrimSpace(r.FormValue("magnet"))
	if magnet == "" {
		writeError(w, http.StatusBadRequest, "missing magnet")
		return
	}

	rec, err := h.manager.AddMagnet(r.Context(), magnet)
	if err != nil {
		if errors.Is(err, torrentmgr.ErrInvalidMagnet) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"id":  rec.ID,
		"uri": h.cfg.PublicURL + "/rest/1.0/torrents/info/" + rec.ID,
	})
}

func (h *Handler) addTorrent(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(12 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	file, _, err := r.FormFile("torrent")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing torrent file")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, 10<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read torrent")
		return
	}

	rec, err := h.manager.AddTorrentFile(r.Context(), data)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{
		"id":  rec.ID,
		"uri": h.cfg.PublicURL + "/rest/1.0/torrents/info/" + rec.ID,
	})
}

func (h *Handler) instantAvailability(w http.ResponseWriter, r *http.Request) {
	raw := chi.URLParam(r, "*")
	if raw == "" {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}

	var hashes []string
	for _, part := range strings.Split(raw, "/") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		for _, hash := range strings.Split(part, ",") {
			hash = strings.TrimSpace(hash)
			if hash != "" {
				hashes = append(hashes, hash)
			}
		}
	}

	result := h.manager.InstantAvailability(r.Context(), hashes)
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) getTorrentInfo(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rec, err := h.manager.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "unknown resource")
		return
	}
	writeJSON(w, http.StatusOK, torrentInfoResponse(h.cfg, rec))
}

func (h *Handler) selectFiles(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	files := strings.TrimSpace(r.FormValue("files"))
	if files == "" {
		writeError(w, http.StatusBadRequest, "missing files")
		return
	}
	if err := h.manager.SelectFiles(r.Context(), id, files); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) deleteTorrent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.manager.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "unknown resource")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listTorrents(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	items, err := h.manager.List(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(items))
	for i := range items {
		out = append(out, torrentSummaryResponse(h.cfg, &items[i]))
	}
	w.Header().Set("X-Total-Count", strconv.Itoa(len(out)))
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) unrestrictLink(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	link := strings.TrimSpace(r.FormValue("link"))
	if link == "" {
		writeError(w, http.StatusBadRequest, "missing link")
		return
	}

	dl, err := h.manager.Unrestrict(r.Context(), link)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":         dl.ID,
		"filename":   dl.Filename,
		"mimeType":   dl.MimeType,
		"filesize":   dl.Filesize,
		"link":       dl.HostLink,
		"host":       h.cfg.Host,
		"chunks":     16,
		"crc":        1,
		"download":   dl.DownloadURL,
		"streamable": 1,
	})
}

func (h *Handler) listDownloads(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	items, err := h.manager.ListDownloads(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, dl := range items {
		out = append(out, map[string]any{
			"id":        dl.ID,
			"filename":  dl.Filename,
			"mimeType":  dl.MimeType,
			"filesize":  dl.Filesize,
			"link":      dl.HostLink,
			"host":      h.cfg.Host,
			"chunks":    16,
			"download":  dl.DownloadURL,
			"generated": dl.GeneratedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) hostLinkRedirect(w http.ResponseWriter, r *http.Request) {
	linkID := chi.URLParam(r, "linkID")
	hostLink := h.signer.HostLink(linkID)
	dl, err := h.manager.Unrestrict(r.Context(), hostLink)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	http.Redirect(w, r, dl.DownloadURL, http.StatusFound)
}

func (h *Handler) serveDownloadWildcard(w http.ResponseWriter, r *http.Request) {
	h.serveSigned(w, r, strings.TrimPrefix(r.URL.Path, "/rest/1.0/"))
}

func (h *Handler) ServeDownloadPublic(w http.ResponseWriter, r *http.Request) {
	h.serveSigned(w, r, strings.TrimPrefix(r.URL.Path, "/"))
}

func (h *Handler) serveSigned(w http.ResponseWriter, r *http.Request, rawPath string) {
	relativePath, expiresUnix, sig, ok := links.ParseDownloadPath(rawPath)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid download path")
		return
	}
	if !h.signer.VerifyDownload(relativePath, expiresUnix, sig) {
		writeError(w, http.StatusForbidden, "invalid or expired link")
		return
	}

	ctx := r.Context()
	rec, file, err := h.manager.LookupByRelativePath(ctx, relativePath)
	if err != nil {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}

	filename := filepath.Base(relativePath)
	startOffset := torrentmgr.ParseRangeStart(r.Header.Get("Range"), file.Bytes)
	reader, modTime, _, err := h.manager.OpenServingReader(ctx, rec.ID, file.ID, torrentmgr.StreamOptions{
		StartOffset: startOffset,
	})
	if err != nil {
		if errors.Is(err, torrentmgr.ErrStreamNotReady) {
			retryAfter := "2"
			if startOffset > 0 {
				retryAfter = "1"
			}
			w.Header().Set("Retry-After", retryAfter)
			writeError(w, http.StatusServiceUnavailable, "stream not ready yet")
			return
		}
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", links.MimeType(filename))
	w.Header().Set("Content-Disposition", "inline; filename=\""+filename+"\"")
	w.Header().Set("Accept-Ranges", "bytes")

	out := w
	if h.metrics != nil {
		var bcw *metrics.ByteCountWriter
		bcw, out = metrics.WrapDownloadWriter(w)
		defer func() {
			h.metrics.RecordDownloadBytes(bcw.Bytes())
		}()
	}

	limited := reader
	if limiter := h.downloadRateLimiter(); limiter != nil {
		limited = limiter.ReadSeekCloser(reader)
	}
	http.ServeContent(out, r, filename, modTime, limited)
}

func (h *Handler) downloadRateLimiter() *links.RateLimiter {
	limit := h.cfg.DownloadRateLimitMB
	if h.manager != nil {
		limit = h.manager.GetDownloadRateLimitMbps()
	}
	return links.NewRateLimiter(limit)
}

func torrentInfoResponse(cfg config.Config, rec *storage.TorrentRecord) map[string]any {
	resp := torrentSummaryResponse(cfg, rec)
	resp["original_filename"] = rec.OriginalName
	resp["original_bytes"] = rec.OriginalBytes

	files := make([]map[string]any, 0, len(rec.Files))
	for _, f := range rec.Files {
		selected := 0
		if f.Selected {
			selected = 1
		}
		files = append(files, map[string]any{
			"id":               f.ID,
			"path":             f.Path,
			"bytes":            f.Bytes,
			"selected":         selected,
			"downloaded_bytes": f.DownloadedBytes,
		})
	}
	resp["files"] = files
	return resp
}

func torrentSummaryResponse(cfg config.Config, rec *storage.TorrentRecord) map[string]any {
	resp := map[string]any{
		"id":       rec.ID,
		"filename": rec.Name,
		"hash":     rec.InfoHash,
		"bytes":    rec.Bytes,
		"host":     cfg.Host,
		"split":    cfg.SplitGB,
		"progress": rec.Progress,
		"status":   rec.Status,
		"added":    rec.AddedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		"links":    buildLinks(cfg, rec),
	}
	if rec.EndedAt != nil {
		resp["ended"] = rec.EndedAt.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	if rec.Status == "downloading" || rec.Status == "queued" {
		resp["speed"] = rec.Speed
	}
	if rec.Status == "downloading" || rec.Status == "magnet_conversion" {
		resp["seeders"] = rec.Seeders
	}
	return resp
}

func buildLinks(cfg config.Config, rec *storage.TorrentRecord) []string {
	if rec.Status == "dead" {
		return []string{}
	}
	if rec.Status != "downloaded" && !torrentmgr.IsStreamable(rec, cfg.MinStreamBytes()) {
		return []string{}
	}
	if len(rec.Links) > 0 {
		out := make([]string, len(rec.Links))
		for i, id := range rec.Links {
			if strings.HasPrefix(id, "http") {
				out[i] = id
			} else {
				out[i] = cfg.PublicURL + "/d/" + id
			}
		}
		return out
	}
	return []string{}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg, "error_code": status})
}
