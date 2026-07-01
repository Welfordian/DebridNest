package admin

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/debridnest/debridnest/internal/auth"
)

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "bad_token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":  user.Name,
		"role":  user.Role,
		"admin": user.Admin,
	})
}

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil || !h.auth.MultiUserEnabled() {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	users, err := h.auth.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if users == nil {
		users = []auth.UserRecord{}
	}
	writeJSON(w, http.StatusOK, users)
}

func (h *Handler) createUser(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil || !h.auth.MultiUserEnabled() {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	var body struct {
		Name string `json:"name"`
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Role == "" {
		body.Role = "user"
	}

	rec, token, err := h.auth.CreateUser(r.Context(), body.Name, body.Role)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, auth.ErrUserExists) {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}

	h.LogActivity(r.Context(), ActionUserCreate, map[string]any{
		"userId": rec.ID,
		"name":   rec.Name,
		"role":   rec.Role,
	})

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":    rec.ID,
		"name":  rec.Name,
		"role":  rec.Role,
		"token": token,
	})
}

func (h *Handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil || !h.auth.MultiUserEnabled() {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	id := chi.URLParam(r, "id")
	if err := h.auth.DeleteUser(r.Context(), id); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, auth.ErrUserNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}

	h.LogActivity(r.Context(), ActionUserDelete, map[string]any{"userId": id})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) rotateUserToken(w http.ResponseWriter, r *http.Request) {
	if h.auth == nil || !h.auth.MultiUserEnabled() {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	id := chi.URLParam(r, "id")
	token, err := h.auth.RotateToken(r.Context(), id)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, auth.ErrUserNotFound) {
			status = http.StatusNotFound
		}
		writeError(w, status, err.Error())
		return
	}

	h.LogActivity(r.Context(), ActionUserRotateToken, map[string]any{"userId": id})

	writeJSON(w, http.StatusOK, map[string]any{"token": token})
}
