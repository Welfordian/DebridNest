package admin

import (
	"encoding/json"
	"net/http"

	"github.com/debridnest/debridnest/internal/auth"
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

func (h *Handler) effectiveRateLimitMbps() float64 {
	if h.settings != nil {
		return h.settings.GetDownloadRateLimitMbps()
	}
	return h.cfg.DownloadRateLimitMB
}
