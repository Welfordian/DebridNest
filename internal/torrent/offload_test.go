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

func TestEarlyOffloadDoesNotRemoveAlreadyRemoteLocalFile(t *testing.T) {
	manager, _ := testManager(t)
	ctx := context.Background()
	localPath := filepath.Join(t.TempDir(), "movie.mkv")
	if err := os.WriteFile(localPath, []byte("data"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	rec := storage.TorrentRecord{
		ID:       "OFFLOAD_EARLY",
		InfoHash: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		Files: []storage.TorrentFileRecord{
			{
				ID:           1,
				TorrentID:    "OFFLOAD_EARLY",
				Path:         "/movie.mkv",
				Bytes:        4,
				Selected:     true,
				DiskPath:     localPath,
				ObjectKey:    "remote/movie.mkv",
				RemoteStored: true,
			},
		},
	}

	manager.offloadFiles(ctx, nil, &rec, false)

	if _, err := os.Stat(localPath); err != nil {
		t.Fatalf("early offload removed local file: %v", err)
	}
}

func TestCompletedOffloadRemovesAlreadyRemoteLocalFile(t *testing.T) {
	manager, _ := testManager(t)
	ctx := context.Background()
	localPath := filepath.Join(t.TempDir(), "movie.mkv")
	if err := os.WriteFile(localPath, []byte("data"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	rec := storage.TorrentRecord{
		ID:       "OFFLOAD_DONE",
		InfoHash: "ffffffffffffffffffffffffffffffffffffffff",
		Files: []storage.TorrentFileRecord{
			{
				ID:           1,
				TorrentID:    "OFFLOAD_DONE",
				Path:         "/movie.mkv",
				Bytes:        4,
				Selected:     true,
				DiskPath:     localPath,
				ObjectKey:    "remote/movie.mkv",
				RemoteStored: true,
			},
		},
	}

	manager.offloadFiles(ctx, nil, &rec, true)

	if _, err := os.Stat(localPath); !os.IsNotExist(err) {
		t.Fatalf("completed offload local file exists or unexpected error: %v", err)
	}
}

func TestOffloadSkipsUploadWhenObjectStorageQuotaExceeded(t *testing.T) {
	manager, db := testManager(t)
	ctx := context.Background()
	if _, err := manager.settings.Patch(ctx, map[string]any{
		"s3Enabled": true,
		"s3QuotaGb": float64(1),
	}); err != nil {
		t.Fatalf("patch settings: %v", err)
	}

	existing := storage.TorrentRecord{
		ID:       "REMOTE_FULL",
		InfoHash: "7777777777777777777777777777777777777777",
		Status:   string(StatusDownloaded),
		AddedAt:  time.Now().UTC(),
		Files: []storage.TorrentFileRecord{
			{
				ID:           1,
				TorrentID:    "REMOTE_FULL",
				Path:         "/existing.mkv",
				Bytes:        1 << 30,
				Selected:     true,
				ObjectKey:    "remote/existing.mkv",
				RemoteStored: true,
			},
		},
	}
	if err := db.CreateTorrent(ctx, existing); err != nil {
		t.Fatalf("create existing: %v", err)
	}

	localPath := filepath.Join(t.TempDir(), "new.mkv")
	if err := os.WriteFile(localPath, []byte("data"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	rec := storage.TorrentRecord{
		ID:       "REMOTE_SKIP",
		InfoHash: "8888888888888888888888888888888888888888",
		Status:   string(StatusDownloaded),
		AddedAt:  time.Now().UTC(),
		Files: []storage.TorrentFileRecord{
			{
				ID:              1,
				TorrentID:       "REMOTE_SKIP",
				Path:            "/new.mkv",
				Bytes:           4,
				DownloadedBytes: 4,
				Selected:        true,
				DiskPath:        localPath,
			},
		},
	}

	manager.offloadFiles(ctx, nil, &rec, false)

	if rec.Files[0].RemoteStored {
		t.Fatal("file was marked remote despite object storage quota")
	}
	if _, err := os.Stat(localPath); err != nil {
		t.Fatalf("local file should remain after skipped offload: %v", err)
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
