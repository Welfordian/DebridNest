package admin

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/debridnest/debridnest/internal/config"
	torrentmgr "github.com/debridnest/debridnest/internal/torrent"
)

type Handler struct {
	cfg     config.Config
	manager *torrentmgr.Manager
}

func NewHandler(cfg config.Config, manager *torrentmgr.Manager) *Handler {
	return &Handler{cfg: cfg, manager: manager}
}

func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(authMiddleware(h.cfg.APIToken))

	r.Get("/stats", h.stats)
	r.Get("/torrents", h.listTorrents)
	r.Delete("/torrents/{id}", h.deleteTorrent)
	r.Post("/torrents/{id}/retry", h.retryTorrent)
	r.Get("/config", h.publicConfig)

	return r
}

func (h *Handler) stats(w http.ResponseWriter, r *http.Request) {
	s, err := h.manager.Stats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"diskUsed":       s.DiskUsed,
		"diskQuota":      s.DiskQuota,
		"torrentCount":   s.TorrentCount,
		"activeCount":    s.ActiveCount,
		"downloadSpeed":  s.DownloadSpeed,
		"retentionDays":  h.cfg.RetentionDays,
		"publicUrl":      h.cfg.PublicURL,
		"rateLimitMbps":  h.cfg.DownloadRateLimitMB,
	})
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
		entry := map[string]any{
			"id":       rec.ID,
			"name":     rec.Name,
			"hash":     rec.InfoHash,
			"status":   rec.Status,
			"progress": rec.Progress,
			"bytes":    rec.Bytes,
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
	writeJSON(w, http.StatusOK, map[string]any{
		"publicUrl":     h.cfg.PublicURL,
		"retentionDays": h.cfg.RetentionDays,
		"diskQuotaGb":   h.cfg.DiskQuotaGB,
		"rateLimitMbps": h.cfg.DownloadRateLimitMB,
	})
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
