package admin

import (
	"encoding/json"
	"net/http"

	"github.com/debridnest/debridnest/internal/auth"
	"github.com/debridnest/debridnest/internal/objectstore"
)

func (h *Handler) getSettings(w http.ResponseWriter, r *http.Request) {
	if h.settings == nil {
		writeError(w, http.StatusServiceUnavailable, "settings not configured")
		return
	}
	user, ok := auth.UserFromContext(r.Context())
	if ok && user.Admin {
		writeJSON(w, http.StatusOK, h.settings.GetMerged())
		return
	}
	writeJSON(w, http.StatusOK, h.settings.RedactForNonAdmin())
}

func (h *Handler) patchSettings(w http.ResponseWriter, r *http.Request) {
	if h.settings == nil {
		writeError(w, http.StatusServiceUnavailable, "settings not configured")
		return
	}

	var fields map[string]any
	if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	merged, err := h.settings.Patch(r.Context(), fields)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.LogActivity(r.Context(), ActionSettingsPatch, map[string]any{"fields": fields})
	writeJSON(w, http.StatusOK, merged)
}

func (h *Handler) effectiveRetentionDays() int {
	if h.settings != nil {
		return h.settings.GetRetentionDays()
	}
	return h.cfg.RetentionDays
}

func (h *Handler) effectiveDiskQuotaGB() int64 {
	if h.settings != nil {
		return h.settings.GetDiskQuotaGB()
	}
	return h.cfg.DiskQuotaGB
}

func (h *Handler) effectiveS3QuotaGB() int64 {
	if h.settings != nil {
		return h.settings.GetS3QuotaGB()
	}
	return objectstore.LoadFromEnv().QuotaGB
}

func (h *Handler) effectiveS3Enabled() bool {
	if h.settings != nil {
		return h.settings.GetS3Enabled()
	}
	return objectstore.LoadFromEnv().Enabled
}

func (h *Handler) effectiveRateLimitMbps() float64 {
	if h.settings != nil {
		return h.settings.GetDownloadRateLimitMbps()
	}
	return h.cfg.DownloadRateLimitMB
}

func (h *Handler) testS3Settings(w http.ResponseWriter, r *http.Request) {
	if h.settings == nil {
		writeError(w, http.StatusServiceUnavailable, "settings not configured")
		return
	}

	cfg := h.settings.S3Config()
	if !cfg.Enabled {
		writeError(w, http.StatusServiceUnavailable, "S3 object storage is disabled")
		return
	}
	if cfg.Bucket == "" {
		writeError(w, http.StatusBadRequest, "S3 bucket is required")
		return
	}

	store, err := objectstore.New(cfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := store.TestConnection(r.Context()); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
