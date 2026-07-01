package qbit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/go-chi/chi/v5"

	"github.com/debridnest/debridnest/internal/auth"
	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/storage"
	torrentmgr "github.com/debridnest/debridnest/internal/torrent"
)

var errTorrentNotFound = errors.New("torrent not found")

type Handler struct {
	cfg        config.Config
	manager    *torrentmgr.Manager
	sessions   *sessionStore
	auth       *auth.Service
	categories sync.Map
	syncRID    atomic.Uint64
}

func NewHandler(cfg config.Config, manager *torrentmgr.Manager, authSvc *auth.Service) *Handler {
	return &Handler{
		cfg:      cfg,
		manager:  manager,
		sessions: newSessionStore(),
		auth:     authSvc,
	}
}

func (h *Handler) Mount(r chi.Router) {
	r.Route("/api/v2", func(r chi.Router) {
		r.Post("/auth/login", h.login)

		r.Group(func(r chi.Router) {
			r.Use(h.authMiddleware)
			r.Get("/app/version", h.appVersion)
			r.Get("/app/webapiVersion", h.webAPIVersion)
			r.Post("/torrents/add", h.addTorrent)
			r.Get("/torrents/info", h.torrentsInfo)
			r.Post("/torrents/delete", h.deleteTorrents)
			r.Get("/sync/maindata", h.syncMaindata)
		})
	})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeText(w, http.StatusBadRequest, "Fails.")
		return
	}
	username := r.FormValue("username")
	password := r.FormValue("password")

	if h.authenticateLogin(r.Context(), username, password) {
		sid, err := h.sessions.create()
		if err != nil {
			writeText(w, http.StatusInternalServerError, "Fails.")
			return
		}
		setSessionCookie(w, sid)
		writeText(w, http.StatusOK, "Ok.")
		return
	}
	writeText(w, http.StatusOK, "Fails.")
}

func (h *Handler) authenticateLogin(ctx context.Context, username, password string) bool {
	qbitUser, qbitPass := h.cfg.QBitAuth()
	if username == qbitUser && password == qbitPass {
		return true
	}
	if h.auth != nil {
		if _, ok := h.auth.ValidateToken(ctx, "Bearer "+password); ok {
			return true
		}
	}
	return false
}

func (h *Handler) appVersion(w http.ResponseWriter, r *http.Request) {
	writeText(w, http.StatusOK, appVersion)
}

func (h *Handler) webAPIVersion(w http.ResponseWriter, r *http.Request) {
	writeText(w, http.StatusOK, webAPIVersion)
}

func (h *Handler) addTorrent(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(12 << 20); err != nil {
		if err := r.ParseForm(); err != nil {
			writeText(w, http.StatusBadRequest, "Fails.")
			return
		}
	}

	urls := strings.TrimSpace(r.FormValue("urls"))
	category := strings.TrimSpace(r.FormValue("category"))

	if urls == "" {
		writeText(w, http.StatusBadRequest, "Fails.")
		return
	}

	for _, line := range strings.Split(urls, "\n") {
		magnet := strings.TrimSpace(line)
		if magnet == "" {
			continue
		}
		rec, err := h.manager.AddMagnet(r.Context(), magnet)
		if err != nil {
			writeText(w, http.StatusInternalServerError, "Fails.")
			return
		}
		if category != "" {
			if hash := normalizeHash(rec.InfoHash); hash != "" {
				h.categories.Store(hash, category)
			} else {
				h.categories.Store(rec.ID, category)
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) torrentsInfo(w http.ResponseWriter, r *http.Request) {
	filter := r.URL.Query().Get("filter")
	items, err := h.listQBitTorrents(r, filter)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *Handler) deleteTorrents(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	hashes := strings.TrimSpace(r.FormValue("hashes"))
	if hashes == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	for _, raw := range strings.Split(hashes, "|") {
		hash := normalizeHash(raw)
		if hash == "" {
			continue
		}
		rec, err := h.findByHash(ctx, hash)
		if err != nil {
			continue
		}
		_ = h.manager.Delete(ctx, rec.ID)
		h.categories.Delete(hash)
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) syncMaindata(w http.ResponseWriter, r *http.Request) {
	nextRID := h.syncRID.Add(1)
	if nextRID == 0 {
		nextRID = h.syncRID.Add(1)
	}

	items, err := h.listQBitTorrents(r, "")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	torrents := make(map[string]map[string]any, len(items))
	for _, item := range items {
		hash, _ := item["hash"].(string)
		if hash == "" {
			continue
		}
		torrents[hash] = item
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"rid":          nextRID,
		"full_update":  true,
		"torrents":     torrents,
		"server_state": h.serverState(items),
	})
}

func (h *Handler) listQBitTorrents(r *http.Request, filter string) ([]map[string]any, error) {
	items, err := h.manager.List(r.Context(), 1000)
	if err != nil {
		return nil, err
	}

	out := make([]map[string]any, 0, len(items))
	for i := range items {
		rec := &items[i]
		hash := normalizeHash(rec.InfoHash)
		if hash == "" {
			continue
		}
		state := mapStatus(rec)
		if !matchesFilter(state, filter) {
			continue
		}
		category, _ := h.categoryFor(rec)
		out = append(out, toQBitTorrent(rec, category))
	}
	return out, nil
}

func (h *Handler) findByHash(ctx context.Context, hash string) (*storage.TorrentRecord, error) {
	items, err := h.manager.List(ctx, 1000)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if normalizeHash(items[i].InfoHash) == hash {
			return &items[i], nil
		}
	}
	return nil, errTorrentNotFound
}

func (h *Handler) categoryFor(rec *storage.TorrentRecord) (string, bool) {
	if hash := normalizeHash(rec.InfoHash); hash != "" {
		if v, ok := h.categories.Load(hash); ok {
			if cat, ok := v.(string); ok {
				return cat, true
			}
		}
	}
	if v, ok := h.categories.Load(rec.ID); ok {
		if cat, ok := v.(string); ok {
			if hash := normalizeHash(rec.InfoHash); hash != "" {
				h.categories.Store(hash, cat)
				h.categories.Delete(rec.ID)
			}
			return cat, true
		}
	}
	return "", false
}

func (h *Handler) serverState(torrents []map[string]any) map[string]any {
	var dlSpeed int64
	for _, t := range torrents {
		if v, ok := t["dlspeed"].(int64); ok {
			dlSpeed += v
		}
	}
	return map[string]any{
		"dl_info_speed": dlSpeed,
		"up_info_speed": int64(0),
		"dl_info_data":  int64(0),
		"up_info_data":  int64(0),
	}
}

func writeText(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
