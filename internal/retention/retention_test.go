package retention

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/links"
	"github.com/debridnest/debridnest/internal/settings"
	"github.com/debridnest/debridnest/internal/storage"
	"github.com/debridnest/debridnest/internal/torrent"
)

func testRetention(t *testing.T, retentionDays int, diskQuotaGB int64) (*Runner, *torrent.Manager, *storage.DB, *settings.Store) {
	t.Helper()
	cfg := config.Config{
		DataDir:       t.TempDir(),
		TorrentPort:   "0",
		PublicURL:     "http://127.0.0.1:8080",
		LinkSecret:    "secret",
		LinkTTL:       time.Hour,
		RetentionDays: retentionDays,
		DiskQuotaGB:   diskQuotaGB,
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
	manager, err := torrent.NewManager(cfg, db, signer, settingsStore)
	if err != nil {
		_ = db.Close()
		t.Fatalf("manager: %v", err)
	}
	runner := NewRunner(cfg, manager, settingsStore)
	t.Cleanup(func() {
		_ = manager.Close()
		_ = db.Close()
	})
	return runner, manager, db, settingsStore
}

func seedCompletedTorrent(t *testing.T, db *storage.DB, id, hash string, ended time.Time, bytes int64) {
	t.Helper()
	ctx := context.Background()
	rec := storage.TorrentRecord{
		ID:       id,
		InfoHash: hash,
		Name:     id,
		Status:   "downloaded",
		Bytes:    bytes,
		AddedAt:  ended.Add(-time.Hour),
		EndedAt:  &ended,
	}
	if err := db.CreateTorrent(ctx, rec); err != nil {
		t.Fatalf("create torrent %s: %v", id, err)
	}
	if err := db.UpdateTorrent(ctx, rec); err != nil {
		t.Fatalf("update torrent %s: %v", id, err)
	}
}

func writeTorrentData(t *testing.T, manager *torrent.Manager, hash string, size int64) {
	t.Helper()
	dir := filepath.Join(manager.FilesDir(), hash)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "data.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	if _, err := f.Write(make([]byte, size)); err != nil {
		_ = f.Close()
		t.Fatalf("write: %v", err)
	}
	_ = f.Close()
}

func TestAgeRemoval(t *testing.T) {
	runner, _, db, _ := testRetention(t, 7, 0)
	ctx := context.Background()

	old := time.Now().UTC().Add(-10 * 24 * time.Hour)
	recent := time.Now().UTC().Add(-time.Hour)
	seedCompletedTorrent(t, db, "OLD00001", "1111111111111111111111111111111111111111", old, 100)
	seedCompletedTorrent(t, db, "NEW00001", "2222222222222222222222222222222222222222", recent, 100)

	result, err := runner.RunNow(ctx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.AgeRemoved != 1 {
		t.Fatalf("age removed = %d, want 1", result.AgeRemoved)
	}

	count, err := db.CountTorrents(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("remaining = %d, want 1", count)
	}
}

func TestQuotaEvictionPreservesActive(t *testing.T) {
	runner, manager, db, _ := testRetention(t, 0, 1)
	ctx := context.Background()

	ended := time.Now().UTC().Add(-time.Hour)
	seedCompletedTorrent(t, db, "COMP0001", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ended.Add(-2*time.Hour), 600<<20)
	seedCompletedTorrent(t, db, "COMP0002", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", ended, 600<<20)
	writeTorrentData(t, manager, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 600<<20)
	writeTorrentData(t, manager, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", 600<<20)

	active, err := db.GetTorrent(ctx, "COMP0002")
	if err != nil {
		t.Fatalf("get active: %v", err)
	}
	active.Status = "downloading"
	if err := db.UpdateTorrent(ctx, *active); err != nil {
		t.Fatalf("mark active: %v", err)
	}

	result, err := runner.RunNow(ctx)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if result.QuotaRemoved < 1 {
		t.Fatalf("quota removed = %d, want >= 1", result.QuotaRemoved)
	}

	rec, err := db.GetTorrent(ctx, "COMP0002")
	if err != nil {
		t.Fatalf("active torrent gone: %v", err)
	}
	if rec.Status != "downloading" {
		t.Fatalf("active status = %q", rec.Status)
	}
}
