//go:build integration

package integration

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/links"
	"github.com/debridnest/debridnest/internal/metrics"
	"github.com/debridnest/debridnest/internal/server"
	"github.com/debridnest/debridnest/internal/storage"
	"github.com/debridnest/debridnest/internal/torrent"
)

func TestSmoke(t *testing.T) {
	tmp := t.TempDir()
	token := "integration-test-token"

	t.Setenv("DEBRIDNEST_API_TOKEN", token)
	t.Setenv("DEBRIDNEST_DATA_DIR", tmp)
	t.Setenv("DEBRIDNEST_PUBLIC_URL", "http://127.0.0.1:8080")
	t.Setenv("DEBRIDNEST_LISTEN", ":0")
	t.Setenv("DEBRIDNEST_TORRENT_PORT", "0")
	t.Setenv("DEBRIDNEST_METRICS", "1")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	db, err := storage.Open(cfg.DataDir)
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	signer := links.NewSigner(cfg.LinkSecret, cfg.PublicURL, cfg.Host, cfg.LinkTTL)
	manager, err := torrent.NewManager(cfg, db, signer)
	if err != nil {
		t.Fatalf("torrent manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	collector := metrics.New()
	collector.StartStatsCollector(context.Background(), manager, time.Second)

	router, err := server.NewRouter(server.Options{
		Config:  cfg,
		Manager: manager,
		Signer:  signer,
		Metrics: collector,
	})
	if err != nil {
		t.Fatalf("router: %v", err)
	}

	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	client := srv.Client()

	t.Run("healthz", func(t *testing.T) {
		resp, err := client.Get(srv.URL + "/healthz")
		if err != nil {
			t.Fatalf("GET /healthz: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if string(body) != "ok" {
			t.Fatalf("body = %q, want ok", body)
		}
	})

	t.Run("user requires auth", func(t *testing.T) {
		resp, err := client.Get(srv.URL + "/rest/1.0/user")
		if err != nil {
			t.Fatalf("GET /rest/1.0/user: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
	})

	t.Run("user with auth", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, srv.URL+"/rest/1.0/user", nil)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("GET /rest/1.0/user: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, body)
		}
		if !strings.Contains(string(body), `"username":"debridnest"`) {
			t.Fatalf("unexpected body: %s", body)
		}
	})

	t.Run("metrics", func(t *testing.T) {
		resp, err := client.Get(srv.URL + "/metrics")
		if err != nil {
			t.Fatalf("GET /metrics: %v", err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		text := string(body)
		for _, want := range []string{
			"debridnest_http_requests_total",
			"debridnest_active_torrents",
			"debridnest_disk_bytes_used",
			"debridnest_download_bytes_served_total",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("metrics missing %q", want)
			}
		}
	})
}

func TestMetricsDisabled(t *testing.T) {
	tmp := t.TempDir()
	token := "integration-test-token"

	t.Setenv("DEBRIDNEST_API_TOKEN", token)
	t.Setenv("DEBRIDNEST_DATA_DIR", tmp)
	t.Setenv("DEBRIDNEST_PUBLIC_URL", "http://127.0.0.1:8080")
	t.Setenv("DEBRIDNEST_TORRENT_PORT", "0")
	t.Setenv("DEBRIDNEST_METRICS", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config: %v", err)
	}

	db, err := storage.Open(cfg.DataDir)
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	signer := links.NewSigner(cfg.LinkSecret, cfg.PublicURL, cfg.Host, cfg.LinkTTL)
	manager, err := torrent.NewManager(cfg, db, signer)
	if err != nil {
		t.Fatalf("torrent manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	router, err := server.NewRouter(server.Options{
		Config:  cfg,
		Manager: manager,
		Signer:  signer,
	})
	if err != nil {
		t.Fatalf("router: %v", err)
	}

	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when metrics disabled", resp.StatusCode)
	}
}
