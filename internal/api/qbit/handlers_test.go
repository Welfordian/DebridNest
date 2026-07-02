package qbit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/debridnest/debridnest/internal/auth"
	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/links"
	"github.com/debridnest/debridnest/internal/objectstore"
	"github.com/debridnest/debridnest/internal/settings"
	"github.com/debridnest/debridnest/internal/storage"
	torrentmgr "github.com/debridnest/debridnest/internal/torrent"
)

func testConfig() config.Config {
	return config.Config{
		APIToken:     "secret-token",
		QBitUser:     "debridnest",
		QBitPassword: "secret-token",
	}
}

func newTestRouter(h *Handler) chi.Router {
	r := chi.NewRouter()
	h.Mount(r)
	return r
}

func newTestHandler(t *testing.T, cfg config.Config) *Handler {
	t.Helper()
	db, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	authSvc, err := auth.New(db, false, cfg.APIToken)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	return NewHandler(cfg, nil, authSvc)
}

func newTestHandlerWithManager(t *testing.T, cfg config.Config) (*Handler, *storage.DB) {
	t.Helper()
	cfg.DataDir = t.TempDir()
	cfg.TorrentPort = "0"
	cfg.PublicURL = "http://127.0.0.1:8080"
	cfg.LinkSecret = "secret"
	cfg.LinkTTL = time.Hour

	db, err := storage.Open(cfg.DataDir)
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	settingsStore, err := settings.NewStore(db, cfg)
	if err != nil {
		_ = db.Close()
		t.Fatalf("settings: %v", err)
	}
	authSvc, err := auth.New(db, false, cfg.APIToken)
	if err != nil {
		_ = db.Close()
		t.Fatalf("auth: %v", err)
	}
	signer := links.NewSigner(cfg.LinkSecret, cfg.PublicURL, cfg.Host, cfg.LinkTTL)
	manager, err := torrentmgr.NewManager(cfg, db, signer, settingsStore, objectstore.Config{})
	if err != nil {
		_ = db.Close()
		t.Fatalf("manager: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close()
		_ = db.Close()
	})
	return NewHandler(cfg, manager, authSvc), db
}

func postForm(t *testing.T, h *Handler, path string, values map[string]string) (string, *httptest.ResponseRecorder) {
	t.Helper()
	form := url.Values{}
	for k, v := range values {
		form.Set(k, v)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	newTestRouter(h).ServeHTTP(rec, req)
	return rec.Body.String(), rec
}

func httptestRequest(t *testing.T, h *Handler, method, path string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	newTestRouter(h).ServeHTTP(rec, req)
	return rec
}

func httptestAuthRequest(t *testing.T, h *Handler, method, path string, configure func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if configure != nil {
		configure(req)
	}
	rec := httptest.NewRecorder()
	newTestRouter(h).ServeHTTP(rec, req)
	return rec
}

func TestMapStatus(t *testing.T) {
	tests := []struct {
		status string
		speed  int64
		prog   int
		want   string
	}{
		{"magnet_conversion", 0, 0, "metaDL"},
		{"waiting_files_selection", 0, 0, "metaDL"},
		{"queued", 0, 0, "queuedDL"},
		{"downloading", 1000, 50, "downloading"},
		{"downloading", 0, 50, "stalledDL"},
		{"downloaded", 0, 100, "pausedUP"},
		{"error", 0, 0, "error"},
		{"magnet_error", 0, 0, "error"},
		{"dead", 0, 0, "error"},
	}

	for _, tc := range tests {
		rec := &storage.TorrentRecord{
			Status:   tc.status,
			Speed:    tc.speed,
			Progress: tc.prog,
		}
		if got := mapStatus(rec); got != tc.want {
			t.Errorf("mapStatus(%q) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

func TestToQBitTorrent(t *testing.T) {
	ended := time.Unix(1700000000, 0)
	rec := &storage.TorrentRecord{
		ID:            "ABC123",
		InfoHash:      "AABBCCDDEEFF00112233445566778899AABBCCDD",
		Magnet:        "magnet:?xt=urn:btih:aabbccddeeff00112233445566778899aabbccdd",
		Name:          "Test Release",
		Status:        "downloaded",
		Progress:      100,
		Bytes:         1_000_000,
		OriginalBytes: 1_000_000,
		Speed:         0,
		Seeders:       3,
		AddedAt:       time.Unix(1699990000, 0),
		EndedAt:       &ended,
	}

	out := toQBitTorrent(rec, "tv-sonarr")
	if out["hash"] != "aabbccddeeff00112233445566778899aabbccdd" {
		t.Fatalf("hash = %v", out["hash"])
	}
	if out["state"] != "pausedUP" {
		t.Fatalf("state = %v", out["state"])
	}
	if out["progress"] != 1.0 {
		t.Fatalf("progress = %v", out["progress"])
	}
	if out["category"] != "tv-sonarr" {
		t.Fatalf("category = %v", out["category"])
	}
	if out["size"] != int64(1_000_000) {
		t.Fatalf("size = %v", out["size"])
	}
}

func TestMatchesFilter(t *testing.T) {
	if !matchesFilter("downloading", "downloading") {
		t.Fatal("expected downloading to match downloading filter")
	}
	if matchesFilter("pausedUP", "downloading") {
		t.Fatal("did not expect pausedUP to match downloading filter")
	}
	if !matchesFilter("pausedUP", "completed") {
		t.Fatal("expected pausedUP to match completed filter")
	}
}

func TestLogin(t *testing.T) {
	cfg := testConfig()
	h := newTestHandler(t, cfg)

	t.Run("success", func(t *testing.T) {
		body, rec := postForm(t, h, "/api/v2/auth/login", map[string]string{
			"username": "debridnest",
			"password": "secret-token",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		if body != "Ok." {
			t.Fatalf("body = %q", body)
		}
		cookies := rec.Result().Cookies()
		if len(cookies) == 0 || cookies[0].Name != sessionCookie {
			t.Fatal("expected SID cookie")
		}
	})

	t.Run("failure", func(t *testing.T) {
		body, rec := postForm(t, h, "/api/v2/auth/login", map[string]string{
			"username": "debridnest",
			"password": "wrong",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		if body != "Fails." {
			t.Fatalf("body = %q", body)
		}
	})
}

func TestAPIIndex(t *testing.T) {
	h := newTestHandler(t, testConfig())

	rec := httptestRequest(t, h, http.MethodGet, "/api/v2", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["webapiVersion"] != webAPIVersion {
		t.Fatalf("webapiVersion = %v", body["webapiVersion"])
	}
}

func TestAppVersionRequiresAuth(t *testing.T) {
	cfg := testConfig()
	h := newTestHandler(t, cfg)

	rec := httptestRequest(t, h, http.MethodGet, "/api/v2/app/version", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want forbidden", rec.Code)
	}
}

func TestProtectedRoutesAcceptBasicAndBearerAuth(t *testing.T) {
	h := newTestHandler(t, testConfig())

	basic := httptestAuthRequest(t, h, http.MethodGet, "/api/v2/app/version", func(req *http.Request) {
		req.SetBasicAuth("debridnest", "secret-token")
	})
	if basic.Code != http.StatusOK || basic.Body.String() != appVersion {
		t.Fatalf("basic auth response = %d %q", basic.Code, basic.Body.String())
	}

	bearer := httptestAuthRequest(t, h, http.MethodGet, "/api/v2/app/version", func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer secret-token")
	})
	if bearer.Code != http.StatusOK || bearer.Body.String() != appVersion {
		t.Fatalf("bearer auth response = %d %q", bearer.Code, bearer.Body.String())
	}
}

func TestCompatibilityEndpoints(t *testing.T) {
	h := newTestHandler(t, testConfig())
	_, login := postForm(t, h, "/api/v2/auth/login", map[string]string{
		"username": "debridnest",
		"password": "secret-token",
	})
	cookies := login.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login did not set cookie")
	}
	cookie := cookies[0]

	for _, path := range []string{
		"/api/v2/app/buildInfo",
		"/api/v2/app/preferences",
		"/api/v2/transfer/info",
		"/api/v2/torrents/categories",
		"/api/v2/torrents/tags",
		"/api/v2/log/main",
		"/api/v2/sync/maindata",
	} {
		rec := httptestRequest(t, h, http.MethodGet, path, cookie)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200", path, rec.Code)
		}
	}
}

func TestTorrentPropertiesAndFilesEndpoints(t *testing.T) {
	h, db := newTestHandlerWithManager(t, testConfig())
	ended := time.Unix(1700001000, 0)
	rec := storage.TorrentRecord{
		ID:            "QBITFILES",
		InfoHash:      "00112233445566778899AABBCCDDEEFF00112233",
		Magnet:        "magnet:?xt=urn:btih:00112233445566778899aabbccddeeff00112233",
		Name:          "Release",
		Status:        "downloaded",
		Progress:      100,
		Bytes:         1000,
		OriginalBytes: 1000,
		AddedAt:       time.Unix(1700000000, 0),
		EndedAt:       &ended,
		Files: []storage.TorrentFileRecord{
			{ID: 1, TorrentID: "QBITFILES", Path: "/Release/movie.mkv", Bytes: 1000, DownloadedBytes: 1000, Selected: true},
		},
	}
	if err := db.CreateTorrent(context.Background(), rec); err != nil {
		t.Fatalf("create: %v", err)
	}

	hash := "00112233445566778899aabbccddeeff00112233"
	props := httptestAuthRequest(t, h, http.MethodGet, "/api/v2/torrents/properties?hash="+hash, func(req *http.Request) {
		req.SetBasicAuth("debridnest", "secret-token")
	})
	if props.Code != http.StatusOK {
		t.Fatalf("properties status = %d", props.Code)
	}

	files := httptestAuthRequest(t, h, http.MethodGet, "/api/v2/torrents/files?hash="+hash, func(req *http.Request) {
		req.SetBasicAuth("debridnest", "secret-token")
	})
	if files.Code != http.StatusOK {
		t.Fatalf("files status = %d", files.Code)
	}
	var body []map[string]any
	if err := json.Unmarshal(files.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode files: %v", err)
	}
	if len(body) != 1 || body[0]["name"] != "Release/movie.mkv" {
		t.Fatalf("files body = %#v", body)
	}
}
