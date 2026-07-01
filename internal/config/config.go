package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	APIToken                   string
	PublicURL                  string
	DataDir                    string
	Listen                     string
	TorrentPort                string
	LinkSecret                 string
	AutoSelectAfter            time.Duration
	LinkTTL                    time.Duration
	Host                       string
	SplitGB                    int
	RetentionDays              int
	DiskQuotaGB                int64
	DownloadRateLimitMB        float64
	MinStreamMB                int64
	StreamReadaheadMB          int64
	SeekReadaheadMB            int64
	SeekPreRollMB              int64
	MetricsEnabled             bool
	WebDAVUser                 string
	WebDAVPassword             string
	WebDAVEnabled              bool
	QBitUser                   string
	QBitPassword               string
	SeedAfterComplete          bool
	SeedRatio                  float64
	SeedMinutes                int
	TranscodeEnabled           bool
	MultiUserEnabled           bool
	WebhookDiscordURL          string
	WebhookNtfyTopic           string
	WebhookGotifyURL           string
	WebhookGotifyToken         string
	NotifyOnDownloadComplete   bool
	NotifyOnQuotaWarning       bool
}

func (c Config) MinStreamBytes() int64 {
	if c.MinStreamMB <= 0 {
		return 8 * 1024 * 1024
	}
	return c.MinStreamMB * 1024 * 1024
}

func (c Config) StreamReadaheadBytes() int64 {
	if c.StreamReadaheadMB <= 0 {
		return 32 * 1024 * 1024
	}
	return c.StreamReadaheadMB * 1024 * 1024
}

func (c Config) SeekReadaheadBytes() int64 {
	if c.SeekReadaheadMB <= 0 {
		return 64 * 1024 * 1024
	}
	return c.SeekReadaheadMB * 1024 * 1024
}

func (c Config) SeekPreRollBytes() int64 {
	if c.SeekPreRollMB <= 0 {
		return 8 * 1024 * 1024
	}
	return c.SeekPreRollMB * 1024 * 1024
}

func Load() (Config, error) {
	token := os.Getenv("DEBRIDNEST_API_TOKEN")
	if token == "" {
		return Config{}, fmt.Errorf("DEBRIDNEST_API_TOKEN is required")
	}

	publicURL := getenv("DEBRIDNEST_PUBLIC_URL", "http://localhost:8080")
	dataDir := getenv("DEBRIDNEST_DATA_DIR", "./data")
	listen := getenv("DEBRIDNEST_LISTEN", ":8080")
	torrentPort := getenv("DEBRIDNEST_TORRENT_PORT", "42069")

	linkSecret := os.Getenv("DEBRIDNEST_LINK_SECRET")
	if linkSecret == "" {
		linkSecret = token
	}

	autoSelectSec, _ := strconv.Atoi(getenv("DEBRIDNEST_AUTO_SELECT_SECONDS", "5"))
	linkTTLHours, _ := strconv.Atoi(getenv("DEBRIDNEST_LINK_TTL_HOURS", "12"))
	retentionDays, _ := strconv.Atoi(getenv("DEBRIDNEST_RETENTION_DAYS", "30"))
	diskQuotaGB, _ := strconv.ParseInt(getenv("DEBRIDNEST_DISK_QUOTA_GB", "0"), 10, 64)
	rateLimitMB, _ := strconv.ParseFloat(getenv("DEBRIDNEST_DOWNLOAD_RATE_LIMIT_MBPS", "0"), 64)
	minStreamMB, _ := strconv.ParseInt(getenv("DEBRIDNEST_MIN_STREAM_MB", "8"), 10, 64)
	streamReadaheadMB, _ := strconv.ParseInt(getenv("DEBRIDNEST_STREAM_READAHEAD_MB", "32"), 10, 64)
	seekReadaheadMB, _ := strconv.ParseInt(getenv("DEBRIDNEST_SEEK_READAHEAD_MB", "64"), 10, 64)
	seekPreRollMB, _ := strconv.ParseInt(getenv("DEBRIDNEST_SEEK_PREROLL_MB", "8"), 10, 64)
	metricsEnabled := os.Getenv("DEBRIDNEST_METRICS") == "1"
	webdavEnabled := getenv("DEBRIDNEST_WEBDAV_ENABLED", "1") != "0"
	seedAfterComplete := os.Getenv("DEBRIDNEST_SEED_AFTER_COMPLETE") == "1"
	seedRatio, _ := strconv.ParseFloat(getenv("DEBRIDNEST_SEED_RATIO", "0"), 64)
	seedMinutes, _ := strconv.Atoi(getenv("DEBRIDNEST_SEED_MINUTES", "0"))
	transcodeEnabled := os.Getenv("DEBRIDNEST_TRANSCODE") == "1"
	multiUserEnabled := os.Getenv("DEBRIDNEST_MULTI_USER") != "0"
	notifyOnDownloadComplete := os.Getenv("DEBRIDNEST_NOTIFY_ON_DOWNLOAD_COMPLETE") == "1"
	notifyOnQuotaWarning := os.Getenv("DEBRIDNEST_NOTIFY_ON_QUOTA_WARNING") == "1"

	return Config{
		APIToken:            token,
		PublicURL:           trimTrailingSlash(publicURL),
		DataDir:             dataDir,
		Listen:              listen,
		TorrentPort:         torrentPort,
		LinkSecret:          linkSecret,
		AutoSelectAfter:     time.Duration(autoSelectSec) * time.Second,
		LinkTTL:             time.Duration(linkTTLHours) * time.Hour,
		Host:                hostFromURL(publicURL),
		SplitGB:             50,
		RetentionDays:       retentionDays,
		DiskQuotaGB:         diskQuotaGB,
		DownloadRateLimitMB: rateLimitMB,
		MinStreamMB:         minStreamMB,
		StreamReadaheadMB:   streamReadaheadMB,
		SeekReadaheadMB:     seekReadaheadMB,
		SeekPreRollMB:       seekPreRollMB,
		MetricsEnabled:      metricsEnabled,
		WebDAVUser:          os.Getenv("DEBRIDNEST_WEBDAV_USER"),
		WebDAVPassword:      os.Getenv("DEBRIDNEST_WEBDAV_PASSWORD"),
		WebDAVEnabled:       webdavEnabled,
		QBitUser:            getenv("DEBRIDNEST_QBIT_USER", "debridnest"),
		QBitPassword:        qbitPassword(token),
		SeedAfterComplete:   seedAfterComplete,
		SeedRatio:           seedRatio,
		SeedMinutes:         seedMinutes,
		TranscodeEnabled:         transcodeEnabled,
		MultiUserEnabled:         multiUserEnabled,
		WebhookDiscordURL:        os.Getenv("DEBRIDNEST_WEBHOOK_DISCORD_URL"),
		WebhookNtfyTopic:         os.Getenv("DEBRIDNEST_WEBHOOK_NTFY_TOPIC"),
		WebhookGotifyURL:         os.Getenv("DEBRIDNEST_WEBHOOK_GOTIFY_URL"),
		WebhookGotifyToken:       os.Getenv("DEBRIDNEST_WEBHOOK_GOTIFY_TOKEN"),
		NotifyOnDownloadComplete: notifyOnDownloadComplete,
		NotifyOnQuotaWarning:     notifyOnQuotaWarning,
	}, nil
}

func qbitPassword(token string) string {
	if v := os.Getenv("DEBRIDNEST_QBIT_PASSWORD"); v != "" {
		return v
	}
	return token
}

// WebDAVAuth returns Basic auth credentials. Disabled when WebDAVEnabled is false.
// Custom DEBRIDNEST_WEBDAV_USER/PASSWORD are used when both are set; otherwise user
// "debridnest" with DEBRIDNEST_API_TOKEN as password.
func (c Config) WebDAVAuth() (user, password string, ok bool) {
	if !c.WebDAVEnabled {
		return "", "", false
	}
	if c.WebDAVUser != "" && c.WebDAVPassword != "" {
		return c.WebDAVUser, c.WebDAVPassword, true
	}
	return "debridnest", c.APIToken, true
}

// QBitAuth returns qBittorrent Web UI credentials for Sonarr/Radarr.
// Default user is "debridnest" with DEBRIDNEST_API_TOKEN as password.
func (c Config) QBitAuth() (user, password string) {
	return c.QBitUser, c.QBitPassword
}

func (c Config) DiskQuotaBytes() int64 {
	if c.DiskQuotaGB <= 0 {
		return 0
	}
	return c.DiskQuotaGB * 1024 * 1024 * 1024
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func trimTrailingSlash(s string) string {
	for len(s) > 1 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func hostFromURL(publicURL string) string {
	u := trimTrailingSlash(publicURL)
	if i := len("https://"); len(u) > i && u[:i] == "https://" {
		u = u[i:]
	} else if i := len("http://"); len(u) > i && u[:i] == "http://" {
		u = u[i:]
	}
	if j := indexByte(u, '/'); j >= 0 {
		u = u[:j]
	}
	if u == "" {
		return "localhost"
	}
	return u
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
