package torrent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/debridnest/debridnest/internal/objectstore"
	"github.com/debridnest/debridnest/internal/storage"
)

func TestOffloadCandidate(t *testing.T) {
	tests := []struct {
		name string
		f    storage.TorrentFileRecord
		want bool
	}{
		{
			name: "complete selected file",
			f:    storage.TorrentFileRecord{Selected: true, Bytes: 100, DownloadedBytes: 100},
			want: true,
		},
		{
			name: "incomplete",
			f:    storage.TorrentFileRecord{Selected: true, Bytes: 100, DownloadedBytes: 50},
			want: false,
		},
		{
			name: "already remote",
			f:    storage.TorrentFileRecord{Selected: true, Bytes: 100, DownloadedBytes: 100, RemoteStored: true},
			want: false,
		},
		{
			name: "unselected",
			f:    storage.TorrentFileRecord{Selected: false, Bytes: 100, DownloadedBytes: 100},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := offloadCandidate(tc.f); got != tc.want {
				t.Fatalf("offloadCandidate() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMaybeOffloadCompletedFilesNoOpWhenEarlyOffloadDisabled(t *testing.T) {
	manager, db := testManager(t)
	ctx := context.Background()

	hash := "cccccccccccccccccccccccccccccccccccccccc"
	localPath := filepath.Join(t.TempDir(), "local.mkv")
	if err := os.WriteFile(localPath, []byte("data"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	rec := storage.TorrentRecord{
		ID:       "OFFLOAD03",
		InfoHash: hash,
		Name:     "local",
		Status:   "downloading",
		AddedAt:  time.Now().UTC(),
		Files: []storage.TorrentFileRecord{
			{
				ID:              1,
				TorrentID:       "OFFLOAD03",
				Path:            "/local.mkv",
				Bytes:           4,
				DownloadedBytes: 4,
				Selected:        true,
				DiskPath:        localPath,
			},
		},
	}
	if err := db.CreateTorrent(ctx, rec); err != nil {
		t.Fatalf("create: %v", err)
	}

	manager.maybeOffloadCompletedFiles(ctx, &rec)

	after, err := db.GetTorrent(ctx, rec.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if after.Files[0].RemoteStored {
		t.Fatal("expected remote_stored unchanged when early offload disabled")
	}
}

func TestOffloadTorrentNoOpWhenNotDownloaded(t *testing.T) {
	manager, db := testManager(t)
	ctx := context.Background()

	t.Setenv("DEBRIDNEST_S3_ENABLED", "1")
	t.Setenv("DEBRIDNEST_S3_BUCKET", "test-bucket")
	t.Setenv("DEBRIDNEST_S3_DIRECT", "1")

	hash := "dddddddddddddddddddddddddddddddddddddddd"
	localPath := filepath.Join(t.TempDir(), "local.mkv")
	if err := os.WriteFile(localPath, []byte("data"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	rec := storage.TorrentRecord{
		ID:       "OFFLOAD04",
		InfoHash: hash,
		Name:     "local",
		Status:   "downloading",
		AddedAt:  time.Now().UTC(),
		Files: []storage.TorrentFileRecord{
			{
				ID:              1,
				TorrentID:       "OFFLOAD04",
				Path:            "/local.mkv",
				Bytes:           4,
				DownloadedBytes: 4,
				Selected:        true,
				DiskPath:        localPath,
			},
		},
	}
	if err := db.CreateTorrent(ctx, rec); err != nil {
		t.Fatalf("create: %v", err)
	}

	manager.offloadTorrent(ctx, rec.ID)

	after, err := db.GetTorrent(ctx, rec.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if after.Files[0].RemoteStored {
		t.Fatal("offloadTorrent should not run for non-downloaded torrents")
	}
}

func TestStoreEarlyOffloadFlag(t *testing.T) {
	store, err := objectstore.New(objectstore.Config{EarlyOffload: true})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if !store.EarlyOffload() {
		t.Fatal("expected EarlyOffload true")
	}
}


func TestGetTorrentFileByRelativePath(t *testing.T) {
	manager, db := testManager(t)
	ctx := context.Background()

	hash := "0123456789abcdef0123456789abcdef01234567"
	rec := storage.TorrentRecord{
		ID:       "OFFLOAD01",
		InfoHash: hash,
		Name:     "movie",
		Status:   "downloaded",
		AddedAt:  time.Now().UTC(),
		Files: []storage.TorrentFileRecord{
			{
				ID:           1,
				TorrentID:    "OFFLOAD01",
				Path:         "/movie.mkv",
				Bytes:        4096,
				Selected:     true,
				DiskPath:     "",
				ObjectKey:    hash + "/movie.mkv",
				RemoteStored: true,
			},
		},
	}
	if err := db.CreateTorrent(ctx, rec); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := db.GetTorrentFileByRelativePath(ctx, hash+"/movie.mkv")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if !got.RemoteStored {
		t.Fatal("expected remote_stored")
	}
	if got.ObjectKey == "" {
		t.Fatal("expected object_key")
	}
	if got.Path != "/movie.mkv" {
		t.Fatalf("path = %q", got.Path)
	}

	_, err = db.GetTorrentFileByRelativePath(ctx, "unknown/file.mkv")
	if err == nil {
		t.Fatal("expected error for unknown path")
	}

	_ = manager // manager wired with disabled object store via testManager
}

func TestOffloadTorrentNoOpWhenDisabled(t *testing.T) {
	manager, db := testManager(t)
	ctx := context.Background()

	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	localPath := filepath.Join(t.TempDir(), "local.mkv")
	if err := os.WriteFile(localPath, []byte("data"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	rec := storage.TorrentRecord{
		ID:       "OFFLOAD02",
		InfoHash: hash,
		Name:     "local",
		Status:   "downloaded",
		AddedAt:  time.Now().UTC(),
		Files: []storage.TorrentFileRecord{
			{
				ID:        1,
				TorrentID: "OFFLOAD02",
				Path:      "/local.mkv",
				Bytes:     4,
				Selected:  true,
				DiskPath:  localPath,
			},
		},
	}
	if err := db.CreateTorrent(ctx, rec); err != nil {
		t.Fatalf("create: %v", err)
	}

	manager.offloadTorrent(ctx, rec.ID)

	after, err := db.GetTorrent(ctx, rec.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if after.Files[0].RemoteStored {
		t.Fatal("expected remote_stored unchanged when S3 disabled")
	}
	if _, err := os.Stat(localPath); err != nil {
		t.Fatalf("local file removed unexpectedly: %v", err)
	}
}
