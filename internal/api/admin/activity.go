package admin

import (
	"context"
	"net/http"
	"strconv"

	"github.com/debridnest/debridnest/internal/activity"
	"github.com/debridnest/debridnest/internal/applog"
	"github.com/debridnest/debridnest/internal/auth"
)

func (h *Handler) LogActivity(ctx context.Context, action string, details map[string]any) {
	if h.activity == nil {
		return
	}
	userID, userName := "system", "system"
	if u, ok := auth.UserFromContext(ctx); ok {
		userID, userName = u.ID, u.Name
	}
	_ = h.activity.Log(ctx, userID, userName, action, details)
}

func (h *Handler) listActivity(w http.ResponseWriter, r *http.Request) {
	if h.activity == nil {
		writeError(w, http.StatusServiceUnavailable, "activity not configured")
		return
	}

	limit := 50
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			offset = n
		}
	}

	items, err := h.activity.List(r.Context(), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if items == nil {
		items = []activity.Entry{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) listLogs(w http.ResponseWriter, r *http.Request) {
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	writeJSON(w, http.StatusOK, applog.Recent(limit))
}
