package admin

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/retention"
	torrentmgr "github.com/debridnest/debridnest/internal/torrent"
	"github.com/debridnest/debridnest/internal/storage"
)

const version = "5.0.0"

var serverStartedAt = time.Now().UTC()

type Handler struct {
	cfg       config.Config
	manager   *torrentmgr.Manager
	retention *retention.Runner
}

func NewHandler(cfg config.Config, manager *torrentmgr.Manager, retentionRunner *retention.Runner) *Handler {
	return &Handler{cfg: cfg, manager: manager, retention: retentionRunner}
}

func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(authMiddleware(h.cfg.APIToken))

	r.Get("/system", h.system)
	r.Get("/stats", h.stats)
	r.Get("/config", h.publicConfig)
	r.Get("/torrents", h.listTorrents)
	r.Get("/torrents/{id}", h.getTorrent)
	r.Post("/torrents/add", h.addMagnet)
	r.Post("/torrents/upload", h.uploadTorrent)
	r.Delete("/torrents/{id}", h.deleteTorrent)
	r.Post("/torrents/{id}/retry", h.retryTorrent)
	r.Post("/torrents/purge", h.purgeTorrents)
	r.Post("/maintenance/cleanup", h.maintenanceCleanup)

	return r
}

func (h *Handler) system(w http.ResponseWriter, r *http.Request) {
	uptime := int64(time.Since(serverStartedAt).Seconds())
	writeJSON(w, http.StatusOK, map[string]any{
		"version":           version,
		"startedAt":         serverStartedAt,
		"uptime":            uptime,
		"webdavEnabled":     h.cfg.WebDAVEnabled,
		"metricsEnabled":    h.cfg.MetricsEnabled,
		"transcodeEnabled":  h.cfg.TranscodeEnabled,
		"seedAfterComplete": h.cfg.SeedAfterComplete,
		"qbitEnabled":       true,
		"listen":            h.cfg.Listen,
		"torrentPort":       h.cfg.TorrentPort,
	})
}

func (h *Handler) stats(w http.ResponseWriter, r *http.Request) {
	s, err := h.manager.Stats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := map[string]any{
		"diskUsed":       s.DiskUsed,
		"diskQuota":      s.DiskQuota,
		"torrentCount":   s.TorrentCount,
		"activeCount":    s.ActiveCount,
		"downloadSpeed":  s.DownloadSpeed,
		"statusCounts":   s.StatusCounts,
		"retentionDays":  h.cfg.RetentionDays,
		"publicUrl":      h.cfg.PublicURL,
		"rateLimitMbps":  h.cfg.DownloadRateLimitMB,
		"diskQuotaGb":    h.cfg.DiskQuotaGB,
		"webdavEnabled":  h.cfg.WebDAVEnabled,
		"metricsEnabled": h.cfg.MetricsEnabled,
	}
	for k, v := range h.configExtras() {
		resp[k] = v
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) listTorrents(w http.ResponseWriter, r *http.Request) {
	limit := 500
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
		rec := &items[i]
		size := rec.OriginalBytes
		if size <= 0 {
			size = rec.Bytes
		}
		entry := map[string]any{
			"id":       rec.ID,
			"name":     rec.Name,
			"hash":     rec.InfoHash,
			"status":   rec.Status,
			"progress": rec.Progress,
			"bytes":    rec.Bytes,
			"size":     size,
			"speed":    rec.Speed,
			"seeders":  rec.Seeders,
			"added":    rec.AddedAt,
		}
		if rec.EndedAt != nil {
			entry["ended"] = rec.EndedAt
		}
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) getTorrent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	rec, err := h.manager.Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	size := rec.OriginalBytes
	if size <= 0 {
		size = rec.Bytes
	}

	files := make([]map[string]any, 0, len(rec.Files))
	for _, f := range rec.Files {
		files = append(files, map[string]any{
			"id":               f.ID,
			"path":             f.Path,
			"bytes":            f.Bytes,
			"selected":         f.Selected,
			"downloadedBytes":  f.DownloadedBytes,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":       rec.ID,
		"name":     rec.Name,
		"hash":     rec.InfoHash,
		"status":   rec.Status,
		"progress": rec.Progress,
		"bytes":    rec.Bytes,
		"size":     size,
		"files":    files,
		"links":    rec.Links,
	})
}

func (h *Handler) addMagnet(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Magnet string `json:"magnet"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Magnet == "" {
		writeError(w, http.StatusBadRequest, "magnet required")
		return
	}

	rec, err := h.manager.AddMagnet(r.Context(), body.Magnet)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, torrentSummary(rec))
}

func (h *Handler) uploadTorrent(w http.ResponseWriter, r *http.Request) {
	const maxUpload = 10 << 20
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	file, _, err := r.FormFile("torrent")
	if err != nil {
		writeError(w, http.StatusBadRequest, "torrent field required")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxUpload+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read torrent file")
		return
	}
	if len(data) > maxUpload {
		writeError(w, http.StatusBadRequest, "torrent file too large")
		return
	}

	rec, err := h.manager.AddTorrentFile(r.Context(), data)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, torrentSummary(rec))
}

func (h *Handler) purgeTorrents(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Filter string `json:"filter"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Filter == "" {
		writeError(w, http.StatusBadRequest, "filter required")
		return
	}

	deleted, err := h.manager.PurgeByStatus(r.Context(), body.Filter)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}

func (h *Handler) maintenanceCleanup(w http.ResponseWriter, r *http.Request) {
	if h.retention == nil {
		writeError(w, http.StatusServiceUnavailable, "retention not configured")
		return
	}
	result, err := h.retention.RunNow(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ageRemoved":   result.AgeRemoved,
		"quotaRemoved": result.QuotaRemoved,
		"diskUsed":     result.DiskUsed,
		"diskQuota":    result.DiskQuota,
	})
}

func (h *Handler) deleteTorrent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.manager.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) retryTorrent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.manager.Retry(r.Context(), id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) publicConfig(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"publicUrl":      h.cfg.PublicURL,
		"retentionDays":  h.cfg.RetentionDays,
		"diskQuotaGb":    h.cfg.DiskQuotaGB,
		"rateLimitMbps":  h.cfg.DownloadRateLimitMB,
		"webdavEnabled":  h.cfg.WebDAVEnabled,
		"metricsEnabled": h.cfg.MetricsEnabled,
	}
	for k, v := range h.configExtras() {
		resp[k] = v
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) configExtras() map[string]any {
	return map[string]any{
		"seedAfterComplete": h.cfg.SeedAfterComplete,
		"seedRatio":         h.cfg.SeedRatio,
		"seedMinutes":       h.cfg.SeedMinutes,
		"transcodeEnabled":  h.cfg.TranscodeEnabled,
		"qbitUser":          h.cfg.QBitUser,
		"linkTtlHours":      int(h.cfg.LinkTTL.Hours()),
		"autoSelectSeconds": int(h.cfg.AutoSelectAfter.Seconds()),
		"minStreamMb":       h.cfg.MinStreamMB,
		"streamReadaheadMb": h.cfg.StreamReadaheadMB,
		"seekReadaheadMb":   h.cfg.SeekReadaheadMB,
		"seekPreRollMb":     h.cfg.SeekPreRollMB,
	}
}

func torrentSummary(rec *storage.TorrentRecord) map[string]any {
	size := rec.OriginalBytes
	if size <= 0 {
		size = rec.Bytes
	}
	return map[string]any{
		"id":       rec.ID,
		"name":     rec.Name,
		"hash":     rec.InfoHash,
		"status":   rec.Status,
		"progress": rec.Progress,
		"bytes":    rec.Bytes,
		"size":     size,
	}
}

func authMiddleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if len(auth) < 8 || auth[:7] != "Bearer " || auth[7:] != token {
				writeError(w, http.StatusUnauthorized, "bad_token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}
