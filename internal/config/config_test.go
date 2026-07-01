package config

import (
	"strings"
	"testing"
	"time"
)

var configEnvKeys = []string{
	"DEBRIDNEST_API_TOKEN",
	"DEBRIDNEST_PUBLIC_URL",
	"DEBRIDNEST_DATA_DIR",
	"DEBRIDNEST_LISTEN",
	"DEBRIDNEST_TORRENT_PORT",
	"DEBRIDNEST_LINK_SECRET",
	"DEBRIDNEST_AUTO_SELECT_SECONDS",
	"DEBRIDNEST_LINK_TTL_HOURS",
	"DEBRIDNEST_RETENTION_DAYS",
	"DEBRIDNEST_DISK_QUOTA_GB",
	"DEBRIDNEST_DOWNLOAD_RATE_LIMIT_MBPS",
	"DEBRIDNEST_MIN_STREAM_MB",
	"DEBRIDNEST_STREAM_READAHEAD_MB",
	"DEBRIDNEST_SEEK_READAHEAD_MB",
	"DEBRIDNEST_SEEK_PREROLL_MB",
	"DEBRIDNEST_METRICS",
	"DEBRIDNEST_WEBDAV_ENABLED",
	"DEBRIDNEST_WEBDAV_USER",
	"DEBRIDNEST_WEBDAV_PASSWORD",
	"DEBRIDNEST_QBIT_USER",
	"DEBRIDNEST_QBIT_PASSWORD",
	"DEBRIDNEST_SEED_AFTER_COMPLETE",
	"DEBRIDNEST_SEED_RATIO",
	"DEBRIDNEST_SEED_MINUTES",
	"DEBRIDNEST_TRANSCODE",
	"DEBRIDNEST_MULTI_USER",
	"DEBRIDNEST_WEBHOOK_DISCORD_URL",
	"DEBRIDNEST_WEBHOOK_NTFY_TOPIC",
	"DEBRIDNEST_WEBHOOK_GOTIFY_URL",
	"DEBRIDNEST_WEBHOOK_GOTIFY_TOKEN",
	"DEBRIDNEST_NOTIFY_ON_DOWNLOAD_COMPLETE",
	"DEBRIDNEST_NOTIFY_ON_QUOTA_WARNING",
}

func cleanConfigEnv(t *testing.T) {
	t.Helper()
	for _, key := range configEnvKeys {
		t.Setenv(key, "")
	}
}

func setValidToken(t *testing.T) {
	t.Helper()
	t.Setenv("DEBRIDNEST_API_TOKEN", "test-token-0123456789abcdef")
}

func TestLoadDefaults(t *testing.T) {
	cleanConfigEnv(t)
	t.Setenv("DEBRIDNEST_API_TOKEN", "  test-token-0123456789abcdef  ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.APIToken != "test-token-0123456789abcdef" {
		t.Fatalf("APIToken = %q, want trimmed token", cfg.APIToken)
	}
	if cfg.PublicURL != "http://localhost:8080" {
		t.Fatalf("PublicURL = %q", cfg.PublicURL)
	}
	if cfg.Host != "localhost:8080" {
		t.Fatalf("Host = %q", cfg.Host)
	}
	if cfg.AutoSelectAfter != 5*time.Second {
		t.Fatalf("AutoSelectAfter = %v", cfg.AutoSelectAfter)
	}
	if cfg.LinkTTL != 12*time.Hour {
		t.Fatalf("LinkTTL = %v", cfg.LinkTTL)
	}
	if cfg.RetentionDays != 30 {
		t.Fatalf("RetentionDays = %d", cfg.RetentionDays)
	}
	if cfg.DiskQuotaGB != 0 {
		t.Fatalf("DiskQuotaGB = %d", cfg.DiskQuotaGB)
	}
	if cfg.DownloadRateLimitMB != 0 {
		t.Fatalf("DownloadRateLimitMB = %v", cfg.DownloadRateLimitMB)
	}
	if cfg.MinStreamMB != 8 || cfg.StreamReadaheadMB != 32 || cfg.SeekReadaheadMB != 64 || cfg.SeekPreRollMB != 8 {
		t.Fatalf("stream buffers = min:%d stream:%d seek:%d preroll:%d", cfg.MinStreamMB, cfg.StreamReadaheadMB, cfg.SeekReadaheadMB, cfg.SeekPreRollMB)
	}
	if cfg.LinkSecret != cfg.APIToken {
		t.Fatalf("LinkSecret default = %q, want API token", cfg.LinkSecret)
	}
	if cfg.QBitPassword != cfg.APIToken {
		t.Fatalf("QBitPassword default = %q, want API token", cfg.QBitPassword)
	}
}

func TestLoadRejectsMissingToken(t *testing.T) {
	cleanConfigEnv(t)

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "DEBRIDNEST_API_TOKEN is required") {
		t.Fatalf("Load() error = %v, want missing token error", err)
	}
}

func TestLoadRejectsPlaceholderTokens(t *testing.T) {
	for _, token := range []string{
		"change-me-to-a-long-random-string",
		"YOUR_TOKEN",
		"your-token",
	} {
		t.Run(token, func(t *testing.T) {
			cleanConfigEnv(t)
			t.Setenv("DEBRIDNEST_API_TOKEN", token)

			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), "must be replaced") {
				t.Fatalf("Load() error = %v, want placeholder rejection", err)
			}
		})
	}
}

func TestLoadRejectsInvalidNumericEnv(t *testing.T) {
	for _, tc := range []struct {
		key   string
		value string
	}{
		{key: "DEBRIDNEST_TORRENT_PORT", value: "not-a-port"},
		{key: "DEBRIDNEST_AUTO_SELECT_SECONDS", value: "soon"},
		{key: "DEBRIDNEST_LINK_TTL_HOURS", value: "later"},
		{key: "DEBRIDNEST_RETENTION_DAYS", value: "thirty"},
		{key: "DEBRIDNEST_DISK_QUOTA_GB", value: "lots"},
		{key: "DEBRIDNEST_DOWNLOAD_RATE_LIMIT_MBPS", value: "fast"},
		{key: "DEBRIDNEST_MIN_STREAM_MB", value: "eight"},
		{key: "DEBRIDNEST_STREAM_READAHEAD_MB", value: "thirty-two"},
		{key: "DEBRIDNEST_SEEK_READAHEAD_MB", value: "sixty-four"},
		{key: "DEBRIDNEST_SEEK_PREROLL_MB", value: "eight"},
		{key: "DEBRIDNEST_SEED_RATIO", value: "one"},
		{key: "DEBRIDNEST_SEED_MINUTES", value: "ten"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			cleanConfigEnv(t)
			setValidToken(t)
			t.Setenv(tc.key, tc.value)

			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("Load() error = %v, want error mentioning %s", err, tc.key)
			}
		})
	}
}

func TestLoadRejectsOutOfRangeNumericEnv(t *testing.T) {
	for _, tc := range []struct {
		key   string
		value string
	}{
		{key: "DEBRIDNEST_TORRENT_PORT", value: "65536"},
		{key: "DEBRIDNEST_AUTO_SELECT_SECONDS", value: "-1"},
		{key: "DEBRIDNEST_LINK_TTL_HOURS", value: "0"},
		{key: "DEBRIDNEST_RETENTION_DAYS", value: "-1"},
		{key: "DEBRIDNEST_DISK_QUOTA_GB", value: "-1"},
		{key: "DEBRIDNEST_DOWNLOAD_RATE_LIMIT_MBPS", value: "-0.1"},
		{key: "DEBRIDNEST_DOWNLOAD_RATE_LIMIT_MBPS", value: "NaN"},
		{key: "DEBRIDNEST_MIN_STREAM_MB", value: "-1"},
		{key: "DEBRIDNEST_STREAM_READAHEAD_MB", value: "-1"},
		{key: "DEBRIDNEST_SEEK_READAHEAD_MB", value: "-1"},
		{key: "DEBRIDNEST_SEEK_PREROLL_MB", value: "-1"},
		{key: "DEBRIDNEST_SEED_RATIO", value: "-1"},
		{key: "DEBRIDNEST_SEED_RATIO", value: "+Inf"},
		{key: "DEBRIDNEST_SEED_MINUTES", value: "-1"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			cleanConfigEnv(t)
			setValidToken(t)
			t.Setenv(tc.key, tc.value)

			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("Load() error = %v, want range error mentioning %s", err, tc.key)
			}
		})
	}
}

func TestLoadAllowsZeroDisableValues(t *testing.T) {
	cleanConfigEnv(t)
	setValidToken(t)
	t.Setenv("DEBRIDNEST_TORRENT_PORT", "0")
	t.Setenv("DEBRIDNEST_AUTO_SELECT_SECONDS", "0")
	t.Setenv("DEBRIDNEST_LINK_TTL_HOURS", "1")
	t.Setenv("DEBRIDNEST_RETENTION_DAYS", "0")
	t.Setenv("DEBRIDNEST_DISK_QUOTA_GB", "0")
	t.Setenv("DEBRIDNEST_DOWNLOAD_RATE_LIMIT_MBPS", "0")
	t.Setenv("DEBRIDNEST_MIN_STREAM_MB", "0")
	t.Setenv("DEBRIDNEST_STREAM_READAHEAD_MB", "0")
	t.Setenv("DEBRIDNEST_SEEK_READAHEAD_MB", "0")
	t.Setenv("DEBRIDNEST_SEEK_PREROLL_MB", "0")
	t.Setenv("DEBRIDNEST_SEED_RATIO", "0")
	t.Setenv("DEBRIDNEST_SEED_MINUTES", "0")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.TorrentPort != "0" || cfg.AutoSelectAfter != 0 || cfg.RetentionDays != 0 || cfg.DiskQuotaGB != 0 || cfg.DownloadRateLimitMB != 0 {
		t.Fatalf("zero values were not preserved: %+v", cfg)
	}
	if cfg.MinStreamMB != 0 || cfg.StreamReadaheadMB != 0 || cfg.SeekReadaheadMB != 0 || cfg.SeekPreRollMB != 0 {
		t.Fatalf("zero buffer values were not preserved: min:%d stream:%d seek:%d preroll:%d", cfg.MinStreamMB, cfg.StreamReadaheadMB, cfg.SeekReadaheadMB, cfg.SeekPreRollMB)
	}
	if cfg.SeedRatio != 0 || cfg.SeedMinutes != 0 {
		t.Fatalf("zero seed settings were not preserved: ratio:%v minutes:%d", cfg.SeedRatio, cfg.SeedMinutes)
	}
}
