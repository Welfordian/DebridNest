package rd

import (
	"context"
	"testing"
	"time"

	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/links"
	"github.com/debridnest/debridnest/internal/objectstore"
	"github.com/debridnest/debridnest/internal/settings"
	"github.com/debridnest/debridnest/internal/storage"
	torrentmgr "github.com/debridnest/debridnest/internal/torrent"
)

func TestDownloadRateLimiterUsesRuntimeSetting(t *testing.T) {
	cfg := config.Config{
		DataDir:             t.TempDir(),
		TorrentPort:         "0",
		PublicURL:           "http://127.0.0.1:8080",
		LinkSecret:          "secret",
		LinkTTL:             time.Hour,
		DownloadRateLimitMB: 0,
	}
	db, err := storage.Open(cfg.DataDir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	settingsStore, err := settings.NewStore(db, cfg)
	if err != nil {
		_ = db.Close()
		t.Fatalf("settings: %v", err)
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

	handler := NewHandler(cfg, manager, signer, nil, nil)
	if handler.downloadRateLimiter() != nil {
		t.Fatal("expected no limiter from initial zero setting")
	}

	if _, err := settingsStore.Patch(context.Background(), map[string]any{
		"downloadRateLimitMbps": 2.5,
	}); err != nil {
		t.Fatalf("patch rate limit: %v", err)
	}
	if handler.downloadRateLimiter() == nil {
		t.Fatal("expected limiter after runtime rate limit was enabled")
	}

	if _, err := settingsStore.Patch(context.Background(), map[string]any{
		"downloadRateLimitMbps": 0,
	}); err != nil {
		t.Fatalf("clear rate limit: %v", err)
	}
	if handler.downloadRateLimiter() != nil {
		t.Fatal("expected no limiter after runtime rate limit was disabled")
	}
}
