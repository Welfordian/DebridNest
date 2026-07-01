package settings

import (
	"context"
	"testing"

	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/storage"
)

func TestStorePatchAndMerge(t *testing.T) {
	cfg := config.Config{
		RetentionDays:       30,
		DiskQuotaGB:         100,
		DownloadRateLimitMB: 0,
		WebhookDiscordURL:   "https://discord.example/hook",
	}
	dir := t.TempDir()
	db, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	store, err := NewStore(db, cfg)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	merged := store.GetMerged()
	if merged.RetentionDays != 30 {
		t.Fatalf("retentionDays = %d, want 30", merged.RetentionDays)
	}
	if merged.DiskQuotaGb != 100 {
		t.Fatalf("diskQuotaGb = %d, want 100", merged.DiskQuotaGb)
	}

	updated, err := store.Patch(context.Background(), map[string]any{
		"retentionDays":         7,
		"diskQuotaGb":           float64(250),
		"downloadRateLimitMbps": 12.5,
		"notifyOnDownloadComplete": true,
	})
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if updated.RetentionDays != 7 {
		t.Fatalf("patched retentionDays = %d, want 7", updated.RetentionDays)
	}
	if updated.DiskQuotaGb != 250 {
		t.Fatalf("patched diskQuotaGb = %d, want 250", updated.DiskQuotaGb)
	}
	if updated.DownloadRateLimitMbps != 12.5 {
		t.Fatalf("patched rate = %v, want 12.5", updated.DownloadRateLimitMbps)
	}
	if !updated.NotifyOnDownloadComplete {
		t.Fatal("expected notifyOnDownloadComplete true")
	}
	if store.GetRetentionDays() != 7 {
		t.Fatalf("getter retentionDays = %d, want 7", store.GetRetentionDays())
	}
	if store.DiskQuotaBytes() != 250*1024*1024*1024 {
		t.Fatalf("disk quota bytes = %d", store.DiskQuotaBytes())
	}
}

func TestStorePatchUnknownField(t *testing.T) {
	dir := t.TempDir()
	db, err := storage.Open(dir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	store, err := NewStore(db, config.Config{})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	_, err = store.Patch(context.Background(), map[string]any{"unknown": true})
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
}
