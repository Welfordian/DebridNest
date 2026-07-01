package torrent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/debridnest/debridnest/internal/storage"
)

const testInfoHash = "0123456789abcdef0123456789abcdef01234567"

func testMagnet(hash string) string {
	return "magnet:?xt=urn:btih:" + hash + "&dn=test"
}

func seedTorrentFiles(t *testing.T, filesDir, hash string) {
	t.Helper()
	dir := filepath.Join(filesDir, hash)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "movie.mkv"), []byte("partial-data"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func TestDeleteRemovesFiles(t *testing.T) {
	manager, db := testManager(t)
	ctx := context.Background()

	seedTorrentFiles(t, manager.FilesDir(), testInfoHash)
	rec := storage.TorrentRecord{
		ID:       "DELFILES",
		InfoHash: testInfoHash,
		Magnet:   testMagnet(testInfoHash),
		Name:     "movie",
		Status:   "downloading",
		AddedAt:  time.Now().UTC(),
		Files: []storage.TorrentFileRecord{
			{
				ID:        1,
				TorrentID: "DELFILES",
				Path:      "/movie.mkv",
				Bytes:     1000,
				Selected:  true,
				DiskPath:  filepath.Join(manager.FilesDir(), testInfoHash, "movie.mkv"),
			},
		},
	}
	if err := db.CreateTorrent(ctx, rec); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := manager.Delete(ctx, rec.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(manager.FilesDir(), testInfoHash)); !os.IsNotExist(err) {
		t.Fatalf("torrent data dir still exists: %v", err)
	}
	count, err := db.CountTorrents(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("torrent count = %d, want 0", count)
	}
}

func TestDeleteRemovesFilesWithoutInfoHash(t *testing.T) {
	manager, db := testManager(t)
	ctx := context.Background()

	seedTorrentFiles(t, manager.FilesDir(), testInfoHash)
	rec := storage.TorrentRecord{
		ID:      "DELMAGNT",
		Magnet:  testMagnet(testInfoHash),
		Name:    "movie",
		Status:  "magnet_conversion",
		AddedAt: time.Now().UTC(),
	}
	if err := db.CreateTorrent(ctx, rec); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := manager.Delete(ctx, rec.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(manager.FilesDir(), testInfoHash)); !os.IsNotExist(err) {
		t.Fatalf("torrent data dir still exists: %v", err)
	}
}

func TestPurgeByStatusRemovesFiles(t *testing.T) {
	manager, db := testManager(t)
	ctx := context.Background()

	seedTorrentFiles(t, manager.FilesDir(), testInfoHash)
	rec := storage.TorrentRecord{
		ID:       "PURG0001",
		InfoHash: testInfoHash,
		Magnet:   testMagnet(testInfoHash),
		Name:     "movie",
		Status:   "downloaded",
		AddedAt:  time.Now().UTC(),
	}
	if err := db.CreateTorrent(ctx, rec); err != nil {
		t.Fatalf("create: %v", err)
	}

	deleted, err := manager.PurgeByStatus(ctx, "completed")
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	if _, err := os.Stat(filepath.Join(manager.FilesDir(), testInfoHash)); !os.IsNotExist(err) {
		t.Fatalf("torrent data dir still exists: %v", err)
	}
}

func TestReconcileOrphanFiles(t *testing.T) {
	manager, db := testManager(t)
	ctx := context.Background()

	orphanHash := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	seedTorrentFiles(t, manager.FilesDir(), orphanHash)

	knownHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	rec := storage.TorrentRecord{
		ID:       "KEEP0001",
		InfoHash: knownHash,
		Magnet:   testMagnet(knownHash),
		Name:     "keep",
		Status:   "downloaded",
		AddedAt:  time.Now().UTC(),
	}
	if err := db.CreateTorrent(ctx, rec); err != nil {
		t.Fatalf("create: %v", err)
	}
	seedTorrentFiles(t, manager.FilesDir(), knownHash)

	manager.reconcileOrphanFiles(ctx)

	if _, err := os.Stat(filepath.Join(manager.FilesDir(), orphanHash)); !os.IsNotExist(err) {
		t.Fatalf("orphan dir still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(manager.FilesDir(), knownHash)); err != nil {
		t.Fatalf("known dir removed: %v", err)
	}
}

func TestDeleteManyRemovesFiles(t *testing.T) {
	manager, db := testManager(t)
	ctx := context.Background()

	hashA := "1111111111111111111111111111111111111111"
	hashB := "2222222222222222222222222222222222222222"
	seedTorrentFiles(t, manager.FilesDir(), hashA)
	seedTorrentFiles(t, manager.FilesDir(), hashB)

	for _, item := range []struct {
		id   string
		hash string
	}{
		{"MANY0001", hashA},
		{"MANY0002", hashB},
	} {
		rec := storage.TorrentRecord{
			ID:       item.id,
			InfoHash: item.hash,
			Magnet:   testMagnet(item.hash),
			Name:     item.id,
			Status:   "downloading",
			AddedAt:  time.Now().UTC(),
		}
		if err := db.CreateTorrent(ctx, rec); err != nil {
			t.Fatalf("create %s: %v", item.id, err)
		}
	}

	deleted, failed := manager.DeleteMany(ctx, []string{"MANY0001", "MANY0002"})
	if deleted != 2 || len(failed) != 0 {
		t.Fatalf("deleted = %d failed = %v, want 2 deleted", deleted, failed)
	}
	for _, hash := range []string{hashA, hashB} {
		if _, err := os.Stat(filepath.Join(manager.FilesDir(), hash)); !os.IsNotExist(err) {
			t.Fatalf("data dir %s still exists", hash)
		}
	}
}
