package webdav

import (
	"context"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/links"
	"github.com/debridnest/debridnest/internal/objectstore"
	"github.com/debridnest/debridnest/internal/settings"
	"github.com/debridnest/debridnest/internal/storage"
	torrentmgr "github.com/debridnest/debridnest/internal/torrent"
)

func testManager(t *testing.T) (*torrentmgr.Manager, *storage.DB) {
	t.Helper()
	for _, key := range []string{
		"DEBRIDNEST_S3_ENABLED",
		"DEBRIDNEST_S3_ENDPOINT",
		"DEBRIDNEST_S3_BUCKET",
		"DEBRIDNEST_S3_REGION",
		"DEBRIDNEST_S3_ACCESS_KEY",
		"DEBRIDNEST_S3_SECRET_KEY",
		"DEBRIDNEST_S3_PREFIX",
		"DEBRIDNEST_S3_FORCE_PATH_STYLE",
		"DEBRIDNEST_S3_OFFLOAD_LOCAL",
		"DEBRIDNEST_S3_DIRECT",
		"DEBRIDNEST_S3_EARLY_OFFLOAD",
	} {
		t.Setenv(key, "")
	}

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
	manager, err := torrentmgr.NewManager(cfg, db, signer, settingsStore, objectstore.Config{})
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

func seedDownloadedTorrent(t *testing.T, manager *torrentmgr.Manager, db *storage.DB) storage.TorrentRecord {
	t.Helper()
	ctx := context.Background()
	hash := "23c343804f6b00d0f5e289b830fd827fec40870b"
	name := "Obsession (2025) [720p] [WEBRip] [YTS.GG - YTS.BZ]"
	filePath := "/" + name + "/Obsession.2025.720p.WEBRip.x264.AAC-[YTS.GG - YTS.BZ].mp4"
	diskPath := filepath.Join(manager.FilesDir(), hash, name, "Obsession.2025.720p.WEBRip.x264.AAC-[YTS.GG - YTS.BZ].mp4")
	if err := os.MkdirAll(filepath.Dir(diskPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(diskPath, []byte("video bytes"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	ended := time.Now().UTC()
	rec := storage.TorrentRecord{
		ID:       "A5AB52593C948462",
		InfoHash: hash,
		Name:     name,
		Status:   string(torrentmgr.StatusDownloaded),
		Progress: 100,
		Bytes:    int64(len("video bytes")),
		AddedAt:  ended.Add(-time.Minute),
		EndedAt:  &ended,
		Files: []storage.TorrentFileRecord{
			{
				ID:              1,
				TorrentID:       "A5AB52593C948462",
				Path:            filePath,
				Bytes:           int64(len("video bytes")),
				Selected:        true,
				DownloadedBytes: int64(len("video bytes")),
				StreamableBytes: int64(len("video bytes")),
				DiskPath:        diskPath,
			},
		},
	}
	if err := db.CreateTorrent(ctx, rec); err != nil {
		t.Fatalf("create torrent: %v", err)
	}
	return rec
}

func TestOpenFileSupportsInfoHashAndFullTorrentPath(t *testing.T) {
	manager, db := testManager(t)
	rec := seedDownloadedTorrent(t, manager, db)

	fs := newTorrentFS(manager)
	f, err := fs.OpenFile(context.Background(), "/"+rec.InfoHash+"/"+strings.TrimPrefix(rec.Files[0].Path, "/"), os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile by hash/full path: %v", err)
	}
	defer f.Close()

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "video bytes" {
		t.Fatalf("read = %q", got)
	}
}

func TestOpenFileSupportsUniqueBasenameFallback(t *testing.T) {
	manager, db := testManager(t)
	rec := seedDownloadedTorrent(t, manager, db)

	fs := newTorrentFS(manager)
	f, err := fs.OpenFile(context.Background(), "/"+rec.InfoHash+"/"+path.Base(rec.Files[0].Path), os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("OpenFile by hash/basename: %v", err)
	}
	defer f.Close()

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "video bytes" {
		t.Fatalf("read = %q", got)
	}
}
