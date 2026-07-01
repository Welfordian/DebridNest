package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/debridnest/debridnest/internal/activity"
	"github.com/debridnest/debridnest/internal/auth"
	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/links"
	"github.com/debridnest/debridnest/internal/settings"
	"github.com/debridnest/debridnest/internal/storage"
	"github.com/debridnest/debridnest/internal/torrent"
)

func testConfig() config.Config {
	return config.Config{
		APIToken:          "test-token",
		PublicURL:         "http://127.0.0.1:8080",
		Listen:            ":8080",
		TorrentPort:       "0",
		LinkSecret:        "test-token",
		LinkTTL:           12 * time.Hour,
		AutoSelectAfter:   5 * time.Second,
		RetentionDays:     30,
		WebDAVEnabled:     true,
		MetricsEnabled:    true,
		TranscodeEnabled:  false,
		SeedAfterComplete: false,
		QBitUser:          "debridnest",
		MinStreamMB:       8,
		StreamReadaheadMB: 32,
		SeekReadaheadMB:   64,
		SeekPreRollMB:     8,
	}
}

func authHeader(token string) string {
	return "Bearer " + token
}

func newTestServices(t *testing.T, cfg config.Config) (*auth.Service, *settings.Store, *activity.Service, *storage.DB) {
	t.Helper()
	cfg.DataDir = t.TempDir()

	db, err := storage.Open(cfg.DataDir)
	if err != nil {
		t.Fatalf("storage open: %v", err)
	}

	authSvc, err := auth.New(db, cfg.MultiUserEnabled, cfg.APIToken)
	if err != nil {
		_ = db.Close()
		t.Fatalf("auth: %v", err)
	}

	settingsStore, err := settings.NewStore(db, cfg)
	if err != nil {
		_ = db.Close()
		t.Fatalf("settings: %v", err)
	}

	activitySvc := activity.New(db)
	t.Cleanup(func() { _ = db.Close() })
	return authSvc, settingsStore, activitySvc, db
}

func newTestHandler(t *testing.T) (*Handler, *torrent.Manager, *storage.DB) {
	t.Helper()
	cfg := testConfig()
	authSvc, settingsStore, activitySvc, db := newTestServices(t, cfg)

	signer := links.NewSigner(cfg.LinkSecret, cfg.PublicURL, cfg.Host, cfg.LinkTTL)
	manager, err := torrent.NewManager(cfg, db, signer, settingsStore)
	if err != nil {
		t.Fatalf("manager: %v", err)
	}

	t.Cleanup(func() { _ = manager.Close() })
	return NewHandler(cfg, manager, nil, activitySvc, settingsStore, authSvc), manager, db
}

func serve(t *testing.T, h *Handler, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Authorization", authHeader(h.cfg.APIToken))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)
	return rec
}

func TestSystemEndpoint(t *testing.T) {
	cfg := testConfig()
	authSvc, settingsStore, activitySvc, _ := newTestServices(t, cfg)
	h := NewHandler(cfg, nil, nil, activitySvc, settingsStore, authSvc)

	rec := serve(t, h, http.MethodGet, "/system", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp["version"] != version {
		t.Fatalf("version = %v, want %q", resp["version"], version)
	}
	if resp["qbitEnabled"] != true {
		t.Fatalf("qbitEnabled = %v, want true", resp["qbitEnabled"])
	}
	if resp["listen"] != ":8080" {
		t.Fatalf("listen = %v, want :8080", resp["listen"])
	}
	if _, ok := resp["uptime"]; !ok {
		t.Fatal("expected uptime field")
	}
	if _, ok := resp["startedAt"]; !ok {
		t.Fatal("expected startedAt field")
	}
}

func TestMeEndpoint(t *testing.T) {
	h, _, _ := newTestHandler(t)

	rec := serve(t, h, http.MethodGet, "/me", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["name"] != "owner" || resp["admin"] != true {
		t.Fatalf("me = %+v", resp)
	}
}

func TestAddMagnet(t *testing.T) {
	h, _, _ := newTestHandler(t)

	body := []byte(`{"magnet":"magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=test"}`)
	rec := serve(t, h, http.MethodPost, "/torrents/add", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["id"] == "" {
		t.Fatal("expected torrent id")
	}
	if resp["status"] != "magnet_conversion" {
		t.Fatalf("status = %v, want magnet_conversion", resp["status"])
	}
}

func TestBatchDeleteTorrents(t *testing.T) {
	h, manager, db := newTestHandler(t)
	ctx := context.Background()

	ids := []string{"BATCH001", "BATCH002", "BATCH003"}
	for _, id := range ids {
		rec := storage.TorrentRecord{
			ID:       id,
			Name:     id,
			InfoHash: id,
			Status:   "magnet_conversion",
			AddedAt:  time.Now().UTC(),
		}
		if err := db.CreateTorrent(ctx, rec); err != nil {
			t.Fatalf("create torrent %s: %v", id, err)
		}
	}

	payload, err := json.Marshal(map[string]any{"ids": ids})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rec := serve(t, h, http.MethodPost, "/torrents/batch-delete", payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if int(resp["deleted"].(float64)) != 3 {
		t.Fatalf("deleted = %v, want 3", resp["deleted"])
	}

	items, err := manager.List(ctx, 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("remaining torrents = %d, want 0", len(items))
	}
}

func TestPurgeCompleted(t *testing.T) {
	h, manager, db := newTestHandler(t)
	ctx := context.Background()

	seed := []struct {
		id     string
		status string
	}{
		{"DOWN0001", "downloaded"},
		{"ERR00001", "error"},
		{"DOWN0002", "downloading"},
	}
	for _, item := range seed {
		rec := storage.TorrentRecord{
			ID:      item.id,
			Name:    item.status,
			Status:  item.status,
			AddedAt: time.Now().UTC(),
		}
		if err := db.CreateTorrent(ctx, rec); err != nil {
			t.Fatalf("create torrent %s: %v", item.id, err)
		}
	}

	rec := serve(t, h, http.MethodPost, "/torrents/purge", []byte(`{"filter":"completed"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if int(resp["deleted"].(float64)) != 1 {
		t.Fatalf("deleted = %v, want 1", resp["deleted"])
	}

	items, err := manager.List(ctx, 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("remaining torrents = %d, want 2", len(items))
	}
}

func TestPurgeUnknownFilter(t *testing.T) {
	h, _, _ := newTestHandler(t)

	rec := serve(t, h, http.MethodPost, "/torrents/purge", []byte(`{"filter":"unknown"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestUnauthorized(t *testing.T) {
	cfg := testConfig()
	authSvc, settingsStore, activitySvc, _ := newTestServices(t, cfg)
	h := NewHandler(cfg, nil, nil, activitySvc, settingsStore, authSvc)

	req := httptest.NewRequest(http.MethodGet, "/system", nil)
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestMultiUserCRUD(t *testing.T) {
	cfg := testConfig()
	cfg.MultiUserEnabled = true
	authSvc, settingsStore, activitySvc, _ := newTestServices(t, cfg)
	h := NewHandler(cfg, nil, nil, activitySvc, settingsStore, authSvc)

	createReq := httptest.NewRequest(http.MethodPost, "/users/", bytes.NewReader([]byte(`{"name":"alice","role":"user"}`)))
	createReq.Header.Set("Authorization", authHeader(cfg.APIToken))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	h.Routes().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createRec.Code, createRec.Body.String())
	}

	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	token, _ := created["token"].(string)
	userID, _ := created["id"].(string)
	if token == "" || userID == "" {
		t.Fatalf("create response = %+v", created)
	}

	listRec := httptest.NewRecorder()
	listReq := httptest.NewRequest(http.MethodGet, "/users/", nil)
	listReq.Header.Set("Authorization", authHeader(cfg.APIToken))
	h.Routes().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d", listRec.Code)
	}

	meReq := httptest.NewRequest(http.MethodGet, "/me", nil)
	meReq.Header.Set("Authorization", authHeader(token))
	meRec := httptest.NewRecorder()
	h.Routes().ServeHTTP(meRec, meReq)
	if meRec.Code != http.StatusOK {
		t.Fatalf("alice me status = %d", meRec.Code)
	}

	purgeReq := httptest.NewRequest(http.MethodPost, "/torrents/purge", bytes.NewReader([]byte(`{"filter":"completed"}`)))
	purgeReq.Header.Set("Authorization", authHeader(token))
	purgeReq.Header.Set("Content-Type", "application/json")
	purgeRec := httptest.NewRecorder()
	h.Routes().ServeHTTP(purgeRec, purgeReq)
	if purgeRec.Code != http.StatusForbidden {
		t.Fatalf("non-admin purge status = %d, want 403", purgeRec.Code)
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/users/"+userID, nil)
	delReq.Header.Set("Authorization", authHeader(cfg.APIToken))
	delRec := httptest.NewRecorder()
	h.Routes().ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", delRec.Code, delRec.Body.String())
	}
}
