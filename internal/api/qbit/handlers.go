package qbit

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
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
	cfg           config.Config
	manager       *torrentmgr.Manager
	sessions      *sessionStore
	auth          *auth.Service
	categories    sync.Map
	categoryPaths sync.Map
	syncRID       atomic.Uint64
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
		r.Get("/", h.apiIndex)
		r.Post("/auth/login", h.login)

		r.Group(func(r chi.Router) {
			r.Use(h.authMiddleware)
			r.Post("/auth/logout", h.logout)
			r.Get("/app/version", h.appVersion)
			r.Get("/app/webapiVersion", h.webAPIVersion)
			r.Get("/app/buildInfo", h.buildInfo)
			r.Get("/app/preferences", h.preferences)
			r.Post("/app/setPreferences", h.ok)
			r.Get("/app/defaultSavePath", h.defaultSavePath)
			r.Get("/transfer/info", h.transferInfo)
			r.Get("/transfer/speedLimitsMode", h.zeroText)
			r.Get("/transfer/downloadLimit", h.zeroText)
			r.Post("/transfer/setDownloadLimit", h.ok)
			r.Get("/transfer/uploadLimit", h.zeroText)
			r.Post("/transfer/setUploadLimit", h.ok)
			r.Post("/torrents/add", h.addTorrent)
			r.Get("/torrents/info", h.torrentsInfo)
			r.Get("/torrents/properties", h.torrentProperties)
			r.Get("/torrents/files", h.torrentFiles)
			r.Get("/torrents/trackers", h.emptyList)
			r.Get("/torrents/webseeds", h.emptyList)
			r.Get("/torrents/pieceStates", h.emptyList)
			r.Get("/torrents/pieceHashes", h.emptyList)
			r.Post("/torrents/pause", h.ok)
			r.Post("/torrents/resume", h.ok)
			r.Post("/torrents/stop", h.ok)
			r.Post("/torrents/start", h.ok)
			r.Post("/torrents/delete", h.deleteTorrents)
			r.Post("/torrents/recheck", h.ok)
			r.Post("/torrents/reannounce", h.ok)
			r.Post("/torrents/addTrackers", h.ok)
			r.Post("/torrents/addPeers", h.ok)
			r.Get("/torrents/downloadLimit", h.torrentLimits)
			r.Post("/torrents/setDownloadLimit", h.ok)
			r.Get("/torrents/uploadLimit", h.torrentLimits)
			r.Post("/torrents/setUploadLimit", h.ok)
			r.Post("/torrents/setShareLimits", h.ok)
			r.Post("/torrents/setLocation", h.ok)
			r.Post("/torrents/setName", h.ok)
			r.Post("/torrents/setCategory", h.setCategory)
			r.Get("/torrents/categories", h.categoriesList)
			r.Post("/torrents/createCategory", h.createCategory)
			r.Post("/torrents/editCategory", h.createCategory)
			r.Post("/torrents/removeCategories", h.removeCategories)
			r.Get("/torrents/tags", h.emptyList)
			r.Post("/torrents/addTags", h.ok)
			r.Post("/torrents/removeTags", h.ok)
			r.Post("/torrents/createTags", h.ok)
			r.Post("/torrents/deleteTags", h.ok)
			r.Post("/torrents/setAutoManagement", h.ok)
			r.Post("/torrents/toggleSequentialDownload", h.ok)
			r.Post("/torrents/toggleFirstLastPiecePrio", h.ok)
			r.Post("/torrents/setForceStart", h.ok)
			r.Post("/torrents/setSuperSeeding", h.ok)
			r.Post("/torrents/filePrio", h.ok)
			r.Get("/sync/maindata", h.syncMaindata)
			r.Get("/sync/torrentPeers", h.torrentPeers)
			r.Get("/log/main", h.emptyList)
			r.Get("/log/peers", h.emptyList)
		})
	})
}

func (h *Handler) apiIndex(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":          "DebridNest qBittorrent Web API",
		"version":       appVersion,
		"webapiVersion": webAPIVersion,
		"login":         "/api/v2/auth/login",
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

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		h.sessions.delete(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	w.WriteHeader(http.StatusOK)
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

func (h *Handler) buildInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"qt":         "6.7.0",
		"libtorrent": "2.0.0",
		"boost":      "1.84.0",
		"openssl":    "3.0.0",
		"bitness":    64,
	})
}

func (h *Handler) preferences(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"locale":                         "en",
		"create_subfolder_enabled":       true,
		"start_paused_enabled":           false,
		"auto_delete_mode":               0,
		"preallocate_all":                false,
		"incomplete_files_ext":           false,
		"auto_tmm_enabled":               false,
		"torrent_changed_tmm_enabled":    false,
		"save_path_changed_tmm_enabled":  false,
		"category_changed_tmm_enabled":   false,
		"save_path":                      defaultSave,
		"temp_path_enabled":              false,
		"temp_path":                      "",
		"scan_dirs":                      map[string]any{},
		"export_dir":                     "",
		"export_dir_fin":                 "",
		"mail_notification_enabled":      false,
		"autorun_enabled":                false,
		"autorun_program":                "",
		"queueing_enabled":               false,
		"max_active_downloads":           -1,
		"max_active_torrents":            -1,
		"max_active_uploads":             -1,
		"dont_count_slow_torrents":       true,
		"slow_torrent_dl_rate_threshold": 2,
		"slow_torrent_ul_rate_threshold": 2,
		"slow_torrent_inactive_timer":    60,
		"max_ratio_enabled":              false,
		"max_ratio":                      -1,
		"max_ratio_act":                  0,
		"listen_port":                    0,
		"upnp":                           false,
		"random_port":                    false,
		"dl_limit":                       -1,
		"up_limit":                       -1,
		"max_connec":                     -1,
		"max_connec_per_torrent":         -1,
		"max_uploads":                    -1,
		"max_uploads_per_torrent":        -1,
		"dht":                            true,
		"pex":                            true,
		"lsd":                            true,
		"encryption":                     0,
		"anonymous_mode":                 false,
		"web_ui_username":                h.cfg.QBitUser,
	})
}

func (h *Handler) defaultSavePath(w http.ResponseWriter, r *http.Request) {
	writeText(w, http.StatusOK, defaultSave)
}

func (h *Handler) transferInfo(w http.ResponseWriter, r *http.Request) {
	items, err := h.listQBitTorrents(r, "")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	state := h.serverState(items)
	writeJSON(w, http.StatusOK, map[string]any{
		"dl_info_speed":     state["dl_info_speed"],
		"dl_info_data":      state["dl_info_data"],
		"up_info_speed":     state["up_info_speed"],
		"up_info_data":      state["up_info_data"],
		"connection_status": "connected",
	})
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

func (h *Handler) torrentProperties(w http.ResponseWriter, r *http.Request) {
	rec, err := h.recordFromHash(r.Context(), r.URL.Query().Get("hash"))
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, torrentProperties(rec))
}

func (h *Handler) torrentFiles(w http.ResponseWriter, r *http.Request) {
	rec, err := h.recordFromHash(r.Context(), r.URL.Query().Get("hash"))
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	files := make([]map[string]any, 0, len(rec.Files))
	for i, f := range rec.Files {
		progress := float64(0)
		if f.Bytes > 0 {
			progress = float64(f.DownloadedBytes) / float64(f.Bytes)
			if f.RemoteStored || f.DownloadedBytes >= f.Bytes {
				progress = 1
			}
		}
		name := strings.TrimPrefix(filepath.ToSlash(f.Path), "/")
		files = append(files, map[string]any{
			"index":        i,
			"name":         name,
			"size":         f.Bytes,
			"progress":     progress,
			"priority":     qbitFilePriority(f),
			"is_seed":      f.RemoteStored || (f.Bytes > 0 && f.DownloadedBytes >= f.Bytes),
			"piece_range":  []int{0, 0},
			"availability": 1,
		})
	}
	writeJSON(w, http.StatusOK, files)
}

func (h *Handler) setCategory(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	category := strings.TrimSpace(r.FormValue("category"))
	for _, hash := range splitHashes(r.FormValue("hashes")) {
		if hash == "all" {
			if h.manager == nil {
				continue
			}
			items, err := h.manager.List(r.Context(), 1000)
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			for i := range items {
				if normalized := normalizeHash(items[i].InfoHash); normalized != "" {
					h.storeCategory(normalized, category)
				}
			}
			continue
		}
		h.storeCategory(hash, category)
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) createCategory(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("category"))
	if name == "" {
		name = strings.TrimSpace(r.FormValue("name"))
	}
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	savePath := strings.TrimSpace(r.FormValue("savePath"))
	if savePath == "" {
		savePath = strings.TrimSpace(r.FormValue("save_path"))
	}
	if savePath == "" {
		savePath = defaultSave
	}
	h.categoryPaths.Store(name, savePath)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) removeCategories(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	for _, category := range strings.Split(r.FormValue("categories"), "\n") {
		category = strings.TrimSpace(category)
		if category != "" {
			h.categoryPaths.Delete(category)
		}
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) categoriesList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.qbitCategories())
}

func (h *Handler) torrentLimits(w http.ResponseWriter, r *http.Request) {
	out := map[string]int64{}
	for _, hash := range splitHashes(r.URL.Query().Get("hashes")) {
		if hash != "all" {
			out[hash] = 0
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) torrentPeers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"rid":         h.syncRID.Add(1),
		"full_update": true,
		"peers":       map[string]any{},
		"show_flags":  true,
	})
}

func (h *Handler) emptyList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []any{})
}

func (h *Handler) zeroText(w http.ResponseWriter, r *http.Request) {
	writeText(w, http.StatusOK, "0")
}

func (h *Handler) ok(w http.ResponseWriter, r *http.Request) {
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
		"categories":   h.qbitCategories(),
		"tags":         []string{},
		"server_state": h.serverState(items),
	})
}

func (h *Handler) listQBitTorrents(r *http.Request, filter string) ([]map[string]any, error) {
	if h.manager == nil {
		return []map[string]any{}, nil
	}
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
	if h.manager == nil {
		return nil, errTorrentNotFound
	}
	items, err := h.manager.List(ctx, 1000)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if normalizeHash(items[i].InfoHash) == hash {
			return h.manager.Get(ctx, items[i].ID)
		}
	}
	return nil, errTorrentNotFound
}

func (h *Handler) recordFromHash(ctx context.Context, hash string) (*storage.TorrentRecord, error) {
	hash = normalizeHash(hash)
	if hash == "" {
		return nil, errTorrentNotFound
	}
	return h.findByHash(ctx, hash)
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

func (h *Handler) storeCategory(hash, category string) {
	hash = normalizeHash(hash)
	if hash == "" {
		return
	}
	if category == "" {
		h.categories.Delete(hash)
		return
	}
	h.categories.Store(hash, category)
	if _, ok := h.categoryPaths.Load(category); !ok {
		h.categoryPaths.Store(category, defaultSave)
	}
}

func (h *Handler) qbitCategories() map[string]map[string]string {
	out := map[string]map[string]string{}
	h.categoryPaths.Range(func(key, value any) bool {
		name, ok := key.(string)
		if !ok || name == "" {
			return true
		}
		savePath, _ := value.(string)
		if savePath == "" {
			savePath = defaultSave
		}
		out[name] = map[string]string{"name": name, "savePath": savePath}
		return true
	})
	h.categories.Range(func(_, value any) bool {
		name, ok := value.(string)
		if !ok || name == "" {
			return true
		}
		if _, ok := out[name]; !ok {
			out[name] = map[string]string{"name": name, "savePath": defaultSave}
		}
		return true
	})
	return out
}

func (h *Handler) serverState(torrents []map[string]any) map[string]any {
	var dlSpeed int64
	for _, t := range torrents {
		if v, ok := t["dlspeed"].(int64); ok {
			dlSpeed += v
		}
	}
	return map[string]any{
		"dl_info_speed":     dlSpeed,
		"up_info_speed":     int64(0),
		"dl_info_data":      int64(0),
		"up_info_data":      int64(0),
		"connection_status": "connected",
		"dht_nodes":         0,
	}
}

func splitHashes(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.EqualFold(raw, "all") {
		return []string{"all"}
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '|' || r == '\n' || r == ','
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		hash := normalizeHash(part)
		if hash != "" {
			out = append(out, hash)
		}
	}
	return out
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
