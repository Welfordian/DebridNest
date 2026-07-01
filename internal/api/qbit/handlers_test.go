package qbit

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/storage"
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
	h := NewHandler(cfg, nil)

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

func TestAppVersionRequiresAuth(t *testing.T) {
	cfg := testConfig()
	h := NewHandler(cfg, nil)

	rec := httptestRequest(t, h, http.MethodGet, "/api/v2/app/version", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want forbidden", rec.Code)
	}
}
