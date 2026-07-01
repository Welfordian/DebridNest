package torrent

import (
	"context"
	"testing"
	"time"

	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/links"
	"github.com/debridnest/debridnest/internal/settings"
	"github.com/debridnest/debridnest/internal/storage"
)

func testManager(t *testing.T) (*Manager, *storage.DB) {
	t.Helper()
	cfg := config.Config{
		DataDir:         t.TempDir(),
		TorrentPort:     "0",
		PublicURL:       "http://127.0.0.1:8080",
		LinkSecret:      "secret",
		LinkTTL:         time.Hour,
		AutoSelectAfter: 0,
		MinStreamMB:     8,
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
	manager, err := NewManager(cfg, db, signer, settingsStore)
	if err != nil {
		_ = db.Close()
		t.Fatalf("manager: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close()
		_ = db.Close()
	})
	return manager, db
}

func TestAddMagnetDedup(t *testing.T) {
	manager, db := testManager(t)
	ctx := context.Background()

	magnet := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=test"
	hash := "0123456789abcdef0123456789abcdef01234567"
	existing := storage.TorrentRecord{
		ID:       "DEDUP001",
		InfoHash: hash,
		Magnet:   magnet,
		Name:     "test",
		Status:   "magnet_conversion",
		AddedAt:  time.Now().UTC(),
	}
	if err := db.CreateTorrent(ctx, existing); err != nil {
		t.Fatalf("seed: %v", err)
	}

	second, err := manager.AddMagnet(ctx, magnet)
	if err != nil {
		t.Fatalf("add magnet: %v", err)
	}
	if second.ID != existing.ID {
		t.Fatalf("dedup ids = %q vs %q", second.ID, existing.ID)
	}

	count, err := db.CountTorrents(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("torrent count = %d, want 1", count)
	}
}

func TestInstantAvailabilityShape(t *testing.T) {
	manager, db := testManager(t)
	ctx := context.Background()

	hash := "0123456789abcdef0123456789abcdef01234567"
	rec := storage.TorrentRecord{
		ID:       "AVAIL001",
		InfoHash: hash,
		Name:     "movie.mkv",
		Status:   "downloaded",
		AddedAt:  time.Now().UTC(),
		Files: []storage.TorrentFileRecord{
			{ID: 1, TorrentID: "AVAIL001", Path: "/movie.1080p.mkv", Bytes: 1000, Selected: true, DiskPath: "/tmp/x"},
		},
	}
	if err := db.CreateTorrent(ctx, rec); err != nil {
		t.Fatalf("create: %v", err)
	}

	result := manager.InstantAvailability(ctx, []string{hash, "bad"})
	if len(result) != 1 {
		t.Fatalf("availability keys = %d, want 1", len(result))
	}
	hosts, ok := result[hash]
	if !ok {
		t.Fatal("expected hash entry")
	}
	variants, ok := hosts["real-debrid.com"]
	if !ok || len(variants) != 1 || variants[0] != "1080p" {
		t.Fatalf("variants = %+v", hosts)
	}
}

func TestListDoesNotWriteOnStablePoll(t *testing.T) {
	manager, db := testManager(t)
	ctx := context.Background()

	rec := storage.TorrentRecord{
		ID:       "LIST0001",
		InfoHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Name:     "stable",
		Status:   "waiting_files_selection",
		Progress: 0,
		AddedAt:  time.Now().UTC(),
	}
	if err := db.CreateTorrent(ctx, rec); err != nil {
		t.Fatalf("create: %v", err)
	}

	before, err := db.GetTorrent(ctx, rec.ID)
	if err != nil {
		t.Fatalf("get before: %v", err)
	}

	if _, err := manager.List(ctx, 10); err != nil {
		t.Fatalf("list: %v", err)
	}

	after, err := db.GetTorrent(ctx, rec.ID)
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if before.Status != after.Status || before.Progress != after.Progress {
		t.Fatalf("list mutated db: before=%+v after=%+v", before.Status, after.Status)
	}
}

func TestCachedDiskUsed(t *testing.T) {
	manager, _ := testManager(t)
	stats, err := manager.Stats(context.Background())
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.DiskUsed < 0 {
		t.Fatalf("disk used = %d", stats.DiskUsed)
	}
}
