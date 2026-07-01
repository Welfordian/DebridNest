package torrent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/links"
	"github.com/debridnest/debridnest/internal/objectstore"
	"github.com/debridnest/debridnest/internal/settings"
	"github.com/debridnest/debridnest/internal/storage"
)

func testManager(t *testing.T) (*Manager, *storage.DB) {
	t.Helper()
	clearObjectStoreEnv(t)
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
	manager, err := NewManager(cfg, db, signer, settingsStore, objectstore.Config{})
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

func clearObjectStoreEnv(t *testing.T) {
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
}

func TestFilesDownloadedChangedIncludesStreamableBytes(t *testing.T) {
	prev := cloneFileDownloaded([]storage.TorrentFileRecord{
		{ID: 1, DownloadedBytes: 64, StreamableBytes: 0},
	})

	if !filesDownloadedChanged(prev, []storage.TorrentFileRecord{
		{ID: 1, DownloadedBytes: 64, StreamableBytes: 32},
	}) {
		t.Fatal("filesDownloadedChanged returned false when streamable bytes changed")
	}
}

func TestEnsureHostLinksOnlyIncludesReadyIncompleteFiles(t *testing.T) {
	manager, db := testManager(t)
	ctx := context.Background()
	now := time.Now().UTC()
	minStreamBytes := manager.cfg.MinStreamBytes()
	rec := storage.TorrentRecord{
		ID:       "LINKS001",
		InfoHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Magnet:   "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Status:   string(StatusDownloading),
		AddedAt:  now,
		Files: []storage.TorrentFileRecord{
			{ID: 1, Path: "/ready.mkv", Bytes: minStreamBytes + 100, Selected: true, StreamableBytes: minStreamBytes},
			{ID: 2, Path: "/later.mkv", Bytes: minStreamBytes + 100, Selected: true, StreamableBytes: minStreamBytes - 1},
		},
	}
	if err := db.CreateTorrent(ctx, rec); err != nil {
		t.Fatalf("create torrent: %v", err)
	}
	got, err := db.GetTorrent(ctx, rec.ID)
	if err != nil {
		t.Fatalf("get torrent: %v", err)
	}

	manager.ensureHostLinks(ctx, got)
	if len(got.Links) != 1 {
		t.Fatalf("links = %+v, want only ready file link", got.Links)
	}
	readyLinkID, err := db.GetHostLinkByTorrentFile(ctx, rec.ID, 1)
	if err != nil {
		t.Fatalf("ready host link: %v", err)
	}
	if got.Links[0] != manager.signer.HostLink(readyLinkID) {
		t.Fatalf("link = %q, want ready file link", got.Links[0])
	}
	if _, err := db.GetHostLinkByTorrentFile(ctx, rec.ID, 2); err == nil {
		t.Fatal("unready file unexpectedly received a host link")
	}
}

func TestUnrestrictRejectsUnreadyHostLink(t *testing.T) {
	manager, db := testManager(t)
	ctx := context.Background()
	now := time.Now().UTC()
	minStreamBytes := manager.cfg.MinStreamBytes()
	rec := storage.TorrentRecord{
		ID:       "LINKS002",
		InfoHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Magnet:   "magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Status:   string(StatusDownloading),
		AddedAt:  now,
		Files: []storage.TorrentFileRecord{
			{ID: 1, Path: "/later.mkv", Bytes: minStreamBytes + 100, Selected: true, StreamableBytes: minStreamBytes - 1},
		},
	}
	if err := db.CreateTorrent(ctx, rec); err != nil {
		t.Fatalf("create torrent: %v", err)
	}
	if err := db.UpsertHostLink(ctx, "STALELINK", rec.ID, 1, now); err != nil {
		t.Fatalf("seed stale host link: %v", err)
	}

	_, err := manager.Unrestrict(ctx, manager.signer.HostLink("STALELINK"))
	if !errors.Is(err, ErrStreamNotReady) {
		t.Fatalf("Unrestrict error = %v, want ErrStreamNotReady", err)
	}
}

func TestOpenServingReaderRejectsRangePastStreamablePrefixWithoutRuntime(t *testing.T) {
	manager, db := testManager(t)
	ctx := context.Background()
	hash := "cccccccccccccccccccccccccccccccccccccccc"
	diskPath := filepath.Join(manager.filesDir, hash, "movie.mkv")
	minStreamBytes := manager.cfg.MinStreamBytes()
	fileSize := minStreamBytes + 100
	if err := os.MkdirAll(filepath.Dir(diskPath), 0o755); err != nil {
		t.Fatalf("create torrent dir: %v", err)
	}
	f, err := os.Create(diskPath)
	if err != nil {
		t.Fatalf("create test file: %v", err)
	}
	if err := f.Truncate(fileSize); err != nil {
		_ = f.Close()
		t.Fatalf("truncate test file: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close test file: %v", err)
	}

	rec := storage.TorrentRecord{
		ID:       "RANGE001",
		InfoHash: hash,
		Magnet:   "magnet:?xt=urn:btih:" + hash,
		Status:   string(StatusDownloading),
		AddedAt:  time.Now().UTC(),
		Files: []storage.TorrentFileRecord{
			{ID: 1, Path: "/movie.mkv", Bytes: fileSize, Selected: true, DownloadedBytes: fileSize, StreamableBytes: minStreamBytes, DiskPath: diskPath},
		},
	}
	if err := db.CreateTorrent(ctx, rec); err != nil {
		t.Fatalf("create torrent: %v", err)
	}

	reader, _, _, err := manager.OpenServingReader(ctx, rec.ID, 1, StreamOptions{StartOffset: 0})
	if err != nil {
		t.Fatalf("OpenServingReader start=0: %v", err)
	}
	_ = reader.Close()

	_, _, _, err = manager.OpenServingReader(ctx, rec.ID, 1, StreamOptions{StartOffset: minStreamBytes + 1})
	if !errors.Is(err, ErrStreamNotReady) {
		t.Fatalf("OpenServingReader past prefix error = %v, want ErrStreamNotReady", err)
	}
}

func TestObjectStoreRefreshesFromRuntimeS3Settings(t *testing.T) {
	manager, _ := testManager(t)
	ctx := context.Background()
	hash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	store, err := manager.objectStoreForSettings()
	if err != nil {
		t.Fatalf("initial store: %v", err)
	}
	if got, want := store.ObjectKey(hash, "/movie.mkv"), hash+"/movie.mkv"; got != want {
		t.Fatalf("initial object key = %q, want %q", got, want)
	}

	if _, err := manager.settings.Patch(ctx, map[string]any{
		"s3Prefix": "runtime-prefix",
	}); err != nil {
		t.Fatalf("patch settings: %v", err)
	}

	refreshed, err := manager.objectStoreForSettings()
	if err != nil {
		t.Fatalf("refreshed store: %v", err)
	}
	if refreshed == store {
		t.Fatal("expected object store to refresh after S3 settings changed")
	}
	if got, want := refreshed.ObjectKey(hash, "/movie.mkv"), "runtime-prefix/"+hash+"/movie.mkv"; got != want {
		t.Fatalf("refreshed object key = %q, want %q", got, want)
	}
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

func TestAddMagnetStoresInfoHashBeforeMetadata(t *testing.T) {
	manager, db := testManager(t)
	ctx := context.Background()

	magnet := "magnet:?xt=urn:btih:abcdefabcdefabcdefabcdefabcdefabcdefabcd&dn=pending"
	hash := "abcdefabcdefabcdefabcdefabcdefabcdefabcd"

	first, err := manager.AddMagnet(ctx, magnet)
	if err != nil {
		t.Fatalf("add magnet: %v", err)
	}
	if first.InfoHash != hash {
		t.Fatalf("first info hash = %q, want %q", first.InfoHash, hash)
	}

	second, err := manager.AddMagnet(ctx, magnet)
	if err != nil {
		t.Fatalf("dedup add magnet: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("dedup created id %q, want %q", second.ID, first.ID)
	}

	count, err := db.CountTorrents(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("torrent count = %d, want 1", count)
	}
}

func TestAddMagnetRejectsInvalidMagnetWithoutCreatingRow(t *testing.T) {
	manager, db := testManager(t)
	ctx := context.Background()

	if _, err := manager.AddMagnet(ctx, "not-a-magnet"); err == nil {
		t.Fatalf("AddMagnet succeeded for invalid magnet")
	}
	count, err := db.CountTorrents(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("torrent count = %d, want 0", count)
	}
}

func TestReconcileStaleMagnetConversionMarksOldInactiveRowsError(t *testing.T) {
	manager, db := testManager(t)
	ctx := context.Background()
	now := time.Now().UTC()

	rec := storage.TorrentRecord{
		ID:       "STALE001",
		InfoHash: "1111111111111111111111111111111111111111",
		Magnet:   "magnet:?xt=urn:btih:1111111111111111111111111111111111111111",
		Status:   "magnet_conversion",
		AddedAt:  now.Add(-(magnetMetadataTimeout + magnetMetadataStaleGrace + time.Minute)),
	}
	if err := db.CreateTorrent(ctx, rec); err != nil {
		t.Fatalf("create stale torrent: %v", err)
	}

	if !manager.reconcileStaleMagnetConversion(ctx, rec, now) {
		t.Fatal("stale magnet conversion was not handled")
	}
	updated, err := db.GetTorrent(ctx, rec.ID)
	if err != nil {
		t.Fatalf("get stale torrent: %v", err)
	}
	if updated.Status != "magnet_error" {
		t.Fatalf("status = %q, want magnet_error", updated.Status)
	}
}

func TestReconcileStaleMagnetConversionOnlyFailsPlaceholderDuplicate(t *testing.T) {
	manager, db := testManager(t)
	ctx := context.Background()
	now := time.Now().UTC()
	hash := "4444444444444444444444444444444444444444"

	downloaded := storage.TorrentRecord{
		ID:            "DONE0001",
		InfoHash:      hash,
		Magnet:        "magnet:?xt=urn:btih:" + hash,
		Name:          "movie.mkv",
		OriginalName:  "movie",
		Status:        "downloaded",
		Progress:      100,
		Bytes:         1024,
		OriginalBytes: 1024,
		InfoBytes:     []byte("info"),
		AddedAt:       now.Add(-time.Hour),
		Files: []storage.TorrentFileRecord{
			{ID: 1, TorrentID: "DONE0001", Path: "/movie.mkv", Bytes: 1024, Selected: true},
		},
	}
	if err := db.CreateTorrent(ctx, downloaded); err != nil {
		t.Fatalf("create downloaded torrent: %v", err)
	}

	stale := storage.TorrentRecord{
		ID:       "STALEDUP",
		InfoHash: hash,
		Magnet:   "magnet:?xt=urn:btih:" + hash,
		Status:   "magnet_conversion",
		AddedAt:  now.Add(-(magnetMetadataTimeout + magnetMetadataStaleGrace + time.Minute)),
	}
	if err := db.CreateTorrent(ctx, stale); err != nil {
		t.Fatalf("create stale duplicate: %v", err)
	}

	if !manager.reconcileStaleMagnetConversion(ctx, stale, now) {
		t.Fatal("stale duplicate magnet conversion was not handled")
	}
	updatedStale, err := db.GetTorrent(ctx, stale.ID)
	if err != nil {
		t.Fatalf("get stale duplicate: %v", err)
	}
	if updatedStale.Status != "magnet_error" {
		t.Fatalf("stale status = %q, want magnet_error", updatedStale.Status)
	}
	updatedDownloaded, err := db.GetTorrent(ctx, downloaded.ID)
	if err != nil {
		t.Fatalf("get downloaded torrent: %v", err)
	}
	if updatedDownloaded.Status != "downloaded" || len(updatedDownloaded.Files) != 1 {
		t.Fatalf("downloaded torrent changed: status=%q files=%d", updatedDownloaded.Status, len(updatedDownloaded.Files))
	}
}

func TestReconcileStaleMagnetConversionKeepsFreshInactiveRowsPending(t *testing.T) {
	manager, db := testManager(t)
	ctx := context.Background()
	now := time.Now().UTC()

	rec := storage.TorrentRecord{
		ID:       "FRESH001",
		InfoHash: "2222222222222222222222222222222222222222",
		Magnet:   "magnet:?xt=urn:btih:2222222222222222222222222222222222222222",
		Status:   "magnet_conversion",
		AddedAt:  now.Add(-time.Minute),
	}
	if err := db.CreateTorrent(ctx, rec); err != nil {
		t.Fatalf("create fresh torrent: %v", err)
	}

	if manager.reconcileStaleMagnetConversion(ctx, rec, now) {
		t.Fatal("fresh magnet conversion was handled as stale")
	}
	updated, err := db.GetTorrent(ctx, rec.ID)
	if err != nil {
		t.Fatalf("get fresh torrent: %v", err)
	}
	if updated.Status != "magnet_conversion" {
		t.Fatalf("status = %q, want magnet_conversion", updated.Status)
	}
}

func TestReconcileStaleMagnetConversionDropsOldActiveRows(t *testing.T) {
	manager, db := testManager(t)
	ctx := context.Background()
	now := time.Now().UTC()

	rec := storage.TorrentRecord{
		ID:       "ACTIVEOLD",
		InfoHash: "3333333333333333333333333333333333333333",
		Magnet:   "magnet:?xt=urn:btih:3333333333333333333333333333333333333333",
		Status:   "magnet_conversion",
		AddedAt:  now,
	}
	if err := db.CreateTorrent(ctx, rec); err != nil {
		t.Fatalf("create active torrent: %v", err)
	}

	manager.mu.Lock()
	manager.active[rec.ID] = &runtimeTorrent{
		id:        rec.ID,
		done:      make(chan struct{}),
		startedAt: now.Add(-(magnetMetadataTimeout + magnetMetadataStaleGrace + time.Minute)),
	}
	manager.mu.Unlock()

	if !manager.reconcileStaleMagnetConversion(ctx, rec, now) {
		t.Fatal("stale active magnet conversion was not handled")
	}
	updated, err := db.GetTorrent(ctx, rec.ID)
	if err != nil {
		t.Fatalf("get active torrent: %v", err)
	}
	if updated.Status != "magnet_error" {
		t.Fatalf("status = %q, want magnet_error", updated.Status)
	}
	manager.mu.RLock()
	_, active := manager.active[rec.ID]
	manager.mu.RUnlock()
	if active {
		t.Fatal("stale active torrent was not removed from active map")
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
