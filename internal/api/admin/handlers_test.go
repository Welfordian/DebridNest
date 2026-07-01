package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/links"
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

func newTestHandler(t *testing.T) (*Handler, *torrent.Manager, *storage.DB) {
	t.Helper()
	cfg := testConfig()
	cfg.DataDir = t.TempDir()

	db, err := storage.Open(cfg.DataDir)
	if err != nil {
		t.Fatalf("storage open: %v", err)
	}

	signer := links.NewSigner(cfg.LinkSecret, cfg.PublicURL, cfg.Host, cfg.LinkTTL)
	manager, err := torrent.NewManager(cfg, db, signer)
	if err != nil {
		_ = db.Close()
		t.Fatalf("manager: %v", err)
	}

	t.Cleanup(func() {
		_ = manager.Close()
		_ = db.Close()
	})
	return NewHandler(cfg, manager, nil), manager, db
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
	h := NewHandler(testConfig(), nil, nil)

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
	h := NewHandler(testConfig(), nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/system", nil)
	rec := httptest.NewRecorder()
	h.Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
