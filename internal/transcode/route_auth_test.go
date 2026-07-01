package transcode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/links"
	"github.com/debridnest/debridnest/internal/objectstore"
	"github.com/debridnest/debridnest/internal/settings"
	"github.com/debridnest/debridnest/internal/storage"
	"github.com/debridnest/debridnest/internal/torrent"
)

func TestHLSRoutesRequireSignedURLsAndRewritePlaylist(t *testing.T) {
	r, signer := newHLSRouteTestRouter(t)
	expires := time.Now().Add(time.Hour)

	unsigned := httptest.NewRequest(http.MethodGet, "/hls/TORRENT1/7/master.m3u8", nil)
	unsignedRec := httptest.NewRecorder()
	r.ServeHTTP(unsignedRec, unsigned)
	if unsignedRec.Code != http.StatusForbidden {
		t.Fatalf("unsigned master status = %d, want 403", unsignedRec.Code)
	}

	masterRec := serveHLSURL(t, r, signer.SignHLSAsset("TORRENT1", 7, "master.m3u8", expires))
	if masterRec.Code != http.StatusOK {
		t.Fatalf("signed master status = %d, body = %s", masterRec.Code, masterRec.Body.String())
	}
	masterBody := masterRec.Body.String()
	if !strings.Contains(masterBody, "/hls/TORRENT1/7/index.m3u8?expires=") {
		t.Fatalf("master playlist did not include signed media playlist URL: %s", masterBody)
	}
	indexURL := lastPlaylistURI(masterBody)

	indexRec := serveHLSURL(t, r, indexURL)
	if indexRec.Code != http.StatusOK {
		t.Fatalf("signed index status = %d, body = %s", indexRec.Code, indexRec.Body.String())
	}
	indexBody := indexRec.Body.String()
	if strings.Contains(indexBody, "\nseg000.ts\n") {
		t.Fatalf("media playlist left unsigned segment URI: %s", indexBody)
	}
	for _, want := range []string{
		`URI="http://media.example/hls/TORRENT1/7/init.mp4?expires=`,
		"http://media.example/hls/TORRENT1/7/seg000.ts?expires=",
		"http://media.example/hls/TORRENT1/7/seg001.ts?expires=",
	} {
		if !strings.Contains(indexBody, want) {
			t.Fatalf("media playlist missing %q in %s", want, indexBody)
		}
	}

	segmentRec := serveHLSURL(t, r, signer.SignHLSAsset("TORRENT1", 7, "seg000.ts", expires))
	if segmentRec.Code != http.StatusOK {
		t.Fatalf("signed segment status = %d, body = %s", segmentRec.Code, segmentRec.Body.String())
	}
	if got := segmentRec.Body.String(); got != "segment-0" {
		t.Fatalf("segment body = %q", got)
	}

	tampered := strings.Replace(indexURL, "index.m3u8", "seg000.ts", 1)
	tamperedRec := serveHLSURL(t, r, tampered)
	if tamperedRec.Code != http.StatusForbidden {
		t.Fatalf("asset-tampered segment status = %d, want 403", tamperedRec.Code)
	}
}

func TestMountPathReturnsSignedMasterURL(t *testing.T) {
	cfg := config.Config{
		PublicURL:        "http://media.example",
		LinkSecret:       "secret",
		LinkTTL:          time.Hour,
		TranscodeEnabled: true,
	}

	mountPath := MountPath(cfg, "TORRENT1", 7)
	u, err := url.Parse(mountPath)
	if err != nil {
		t.Fatalf("parse mount path: %v", err)
	}
	if u.Path != "/hls/TORRENT1/7/master.m3u8" {
		t.Fatalf("mount path = %q", u.Path)
	}
	expiresUnix, err := parseExpires(u)
	if err != nil {
		t.Fatalf("parse expires: %v", err)
	}
	signer := links.NewSigner(cfg.LinkSecret, cfg.PublicURL, cfg.Host, cfg.LinkTTL)
	if !signer.VerifyHLSAsset("TORRENT1", 7, "master.m3u8", expiresUnix, u.Query().Get("sig")) {
		t.Fatalf("mount path signature did not verify")
	}
}

func TestHLSRateLimiterUsesRuntimeSetting(t *testing.T) {
	cfg := config.Config{
		APIToken:            "token",
		DataDir:             t.TempDir(),
		PublicURL:           "http://media.example",
		LinkSecret:          "secret",
		LinkTTL:             time.Hour,
		TorrentPort:         "0",
		DownloadRateLimitMB: 5,
		SeedAfterComplete:   false,
		MinStreamMB:         8,
		TranscodeEnabled:    true,
	}

	db, err := storage.Open(cfg.DataDir)
	if err != nil {
		t.Fatalf("storage open: %v", err)
	}
	settingsStore, err := settings.NewStore(db, cfg)
	if err != nil {
		_ = db.Close()
		t.Fatalf("settings: %v", err)
	}
	signer := links.NewSigner(cfg.LinkSecret, cfg.PublicURL, cfg.Host, cfg.LinkTTL)
	manager, err := torrent.NewManager(cfg, db, signer, settingsStore, objectstore.Config{})
	if err != nil {
		_ = db.Close()
		t.Fatalf("manager: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close()
		_ = db.Close()
	})

	h := newHandler(cfg, manager, signer)
	if h.downloadRateLimiter() == nil {
		t.Fatalf("initial config rate limiter is nil")
	}
	if _, err := settingsStore.Patch(context.Background(), map[string]any{"downloadRateLimitMbps": 0}); err != nil {
		t.Fatalf("disable rate limit: %v", err)
	}
	if h.downloadRateLimiter() != nil {
		t.Fatalf("runtime-disabled rate limiter is not nil")
	}
	if _, err := settingsStore.Patch(context.Background(), map[string]any{"downloadRateLimitMbps": 1}); err != nil {
		t.Fatalf("enable rate limit: %v", err)
	}
	if h.downloadRateLimiter() == nil {
		t.Fatalf("runtime-enabled rate limiter is nil")
	}
}

func newHLSRouteTestRouter(t *testing.T) (chi.Router, *links.Signer) {
	t.Helper()

	cfg := config.Config{
		APIToken:          "token",
		DataDir:           t.TempDir(),
		PublicURL:         "http://media.example",
		LinkSecret:        "secret",
		LinkTTL:           time.Hour,
		TorrentPort:       "0",
		TranscodeEnabled:  true,
		SeedAfterComplete: false,
		MinStreamMB:       8,
	}

	db, err := storage.Open(cfg.DataDir)
	if err != nil {
		t.Fatalf("storage open: %v", err)
	}
	settingsStore, err := settings.NewStore(db, cfg)
	if err != nil {
		_ = db.Close()
		t.Fatalf("settings: %v", err)
	}
	signer := links.NewSigner(cfg.LinkSecret, cfg.PublicURL, cfg.Host, cfg.LinkTTL)
	manager, err := torrent.NewManager(cfg, db, signer, settingsStore, objectstore.Config{})
	if err != nil {
		_ = db.Close()
		t.Fatalf("manager: %v", err)
	}
	t.Cleanup(func() {
		_ = manager.Close()
		_ = db.Close()
	})

	sourcePath := filepath.Join(cfg.DataDir, "movie.mkv")
	if err := os.WriteFile(sourcePath, []byte("source"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	rec := storage.TorrentRecord{
		ID:           "TORRENT1",
		InfoHash:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Name:         "movie.mkv",
		Status:       "downloaded",
		Progress:     100,
		Bytes:        6,
		AddedAt:      time.Now().UTC(),
		OriginalName: "movie.mkv",
		Files: []storage.TorrentFileRecord{
			{
				ID:              7,
				TorrentID:       "TORRENT1",
				Path:            "movie.mkv",
				Bytes:           6,
				Selected:        true,
				DownloadedBytes: 6,
				DiskPath:        sourcePath,
			},
		},
	}
	if err := db.CreateTorrent(context.Background(), rec); err != nil {
		t.Fatalf("create torrent: %v", err)
	}

	outDir := filepath.Join(cfg.DataDir, "hls", rec.ID, "7")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("create hls dir: %v", err)
	}
	index := "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:6.000,\nseg000.ts\n#EXTINF:6.000,\nseg001.ts\n#EXT-X-ENDLIST\n"
	if err := os.WriteFile(filepath.Join(outDir, "index.m3u8"), []byte(index), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "init.mp4"), []byte("init"), 0o644); err != nil {
		t.Fatalf("write init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "seg000.ts"), []byte("segment-0"), 0o644); err != nil {
		t.Fatalf("write segment 0: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "seg001.ts"), []byte("segment-1"), 0o644); err != nil {
		t.Fatalf("write segment 1: %v", err)
	}

	r := chi.NewRouter()
	if err := Mount(r, cfg, manager, signer); err != nil {
		t.Fatalf("mount transcode: %v", err)
	}
	return r, signer
}

func serveHLSURL(t *testing.T, r http.Handler, rawURL string) *httptest.ResponseRecorder {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse url %q: %v", rawURL, err)
	}
	req := httptest.NewRequest(http.MethodGet, u.RequestURI(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func lastPlaylistURI(body string) string {
	lines := strings.Split(strings.TrimSpace(body), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func parseExpires(u *url.URL) (int64, error) {
	return strconv.ParseInt(u.Query().Get("expires"), 10, 64)
}
