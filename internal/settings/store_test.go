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
		"retentionDays":            7,
		"diskQuotaGb":              float64(250),
		"downloadRateLimitMbps":    12.5,
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

func TestStoreRejectsInvalidS3Quota(t *testing.T) {
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

	for name, value := range map[string]any{
		"negative":   float64(-1),
		"fractional": float64(1.5),
		"string":     "100",
		"overflow":   float64(maxQuotaGB + 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := store.Patch(context.Background(), map[string]any{"s3QuotaGb": value}); err == nil {
				t.Fatal("expected invalid quota error")
			}
		})
	}
}

func TestStoreS3PatchGetAndRedaction(t *testing.T) {
	t.Setenv("DEBRIDNEST_S3_ENABLED", "")
	t.Setenv("DEBRIDNEST_S3_BUCKET", "")

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

	merged, err := store.Patch(context.Background(), map[string]any{
		"s3Enabled":        true,
		"s3Endpoint":       "https://abc.r2.cloudflarestorage.com",
		"s3Bucket":         "my-bucket",
		"s3Region":         "auto",
		"s3Prefix":         "debridnest",
		"s3AccessKey":      "access-id",
		"s3SecretKey":      "secret-value",
		"s3ForcePathStyle": true,
		"s3OffloadLocal":   true,
		"s3QuotaGb":        float64(750),
	})
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if !merged.S3Enabled || merged.S3Bucket != "my-bucket" {
		t.Fatalf("merged S3 = %+v", merged)
	}
	if merged.S3AccessKey != "access-id" || merged.S3SecretKey != "secret-value" {
		t.Fatal("expected admin GET to include secrets")
	}
	if !merged.S3ForcePathStyle || !merged.S3OffloadLocal {
		t.Fatal("expected S3 flags patched")
	}
	if merged.S3QuotaGb != 750 {
		t.Fatalf("s3 quota = %d, want 750", merged.S3QuotaGb)
	}

	cfg := store.S3Config()
	if !cfg.Enabled || cfg.Bucket != "my-bucket" || cfg.Endpoint == "" {
		t.Fatalf("S3Config = %+v", cfg)
	}
	if cfg.QuotaGB != 750 {
		t.Fatalf("S3Config quota = %d, want 750", cfg.QuotaGB)
	}
	if store.S3QuotaBytes() != 750*1024*1024*1024 {
		t.Fatalf("S3 quota bytes = %d", store.S3QuotaBytes())
	}

	redacted := store.RedactForNonAdmin()
	if redacted.S3SecretKey != "" {
		t.Fatal("expected secret key redacted for non-admin")
	}
	if redacted.S3AccessKey != "(configured)" {
		t.Fatalf("access key redaction = %q", redacted.S3AccessKey)
	}
	if redacted.S3Endpoint != "https://abc.r2.cloudflarestorage.com" {
		t.Fatalf("endpoint should remain visible, got %q", redacted.S3Endpoint)
	}
}
