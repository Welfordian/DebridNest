package notify

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"sync"

	"github.com/debridnest/debridnest/internal/storage"
)

const (
	keyDiscordURL     = "webhookDiscordUrl"
	keyNtfyTopic      = "webhookNtfyTopic"
	keyGotifyURL      = "webhookGotifyUrl"
	keyGotifyToken    = "webhookGotifyToken"
	keyNotifyDownload = "notifyOnDownloadComplete"
	keyNotifyQuota    = "notifyOnQuotaWarning"
)

type Settings struct {
	DiscordWebhookURL        string
	NtfyTopic                string
	GotifyURL                string
	GotifyToken              string
	NotifyOnDownloadComplete bool
	NotifyOnQuotaWarning     bool
}

type SettingsReader interface {
	NotifySettings() Settings
}

type DBSettingsReader struct {
	db       *storage.DB
	defaults Settings
	mu       sync.RWMutex
	cache    map[string]string
}

func NewDBSettingsReader(db *storage.DB, defaults Settings) *DBSettingsReader {
	return &DBSettingsReader{db: db, defaults: defaults, cache: map[string]string{}}
}

func DefaultsFromEnv() Settings {
	return Settings{
		DiscordWebhookURL:        os.Getenv("DEBRIDNEST_WEBHOOK_DISCORD_URL"),
		NtfyTopic:                os.Getenv("DEBRIDNEST_WEBHOOK_NTFY_TOPIC"),
		GotifyURL:                os.Getenv("DEBRIDNEST_WEBHOOK_GOTIFY_URL"),
		GotifyToken:              os.Getenv("DEBRIDNEST_WEBHOOK_GOTIFY_TOKEN"),
		NotifyOnDownloadComplete: envBool("DEBRIDNEST_NOTIFY_ON_DOWNLOAD_COMPLETE", true),
		NotifyOnQuotaWarning:     envBool("DEBRIDNEST_NOTIFY_ON_QUOTA_WARNING", true),
	}
}

func (r *DBSettingsReader) NotifySettings() Settings {
	r.mu.RLock()
	cache := r.cache
	r.mu.RUnlock()
	if cache == nil {
		cache = map[string]string{}
	}

	s := r.defaults
	if v := r.value(keyDiscordURL, cache); v != "" {
		s.DiscordWebhookURL = v
	}
	if v := r.value(keyNtfyTopic, cache); v != "" {
		s.NtfyTopic = v
	}
	if v := r.value(keyGotifyURL, cache); v != "" {
		s.GotifyURL = v
	}
	if v := r.value(keyGotifyToken, cache); v != "" {
		s.GotifyToken = v
	}
	if v, ok := r.boolValue(keyNotifyDownload, cache); ok {
		s.NotifyOnDownloadComplete = v
	}
	if v, ok := r.boolValue(keyNotifyQuota, cache); ok {
		s.NotifyOnQuotaWarning = v
	}
	return s
}

func (r *DBSettingsReader) Refresh(ctx context.Context) {
	rows, err := r.db.QueryContext(ctx, `SELECT key, value FROM runtime_settings`)
	if err != nil {
		return
	}
	defer rows.Close()

	next := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		next[key] = value
	}
	r.mu.Lock()
	r.cache = next
	r.mu.Unlock()
}

func (r *DBSettingsReader) value(key string, cache map[string]string) string {
	if v, ok := cache[key]; ok {
		return unquoteJSONString(v)
	}
	return ""
}

func (r *DBSettingsReader) boolValue(key string, cache map[string]string) (bool, bool) {
	v, ok := cache[key]
	if !ok {
		return false, false
	}
	v = unquoteJSONString(v)
	switch v {
	case "true", "1":
		return true, true
	case "false", "0":
		return false, true
	default:
		return false, false
	}
}

func unquoteJSONString(v string) string {
	if v == "" {
		return ""
	}
	var s string
	if err := json.Unmarshal([]byte(v), &s); err == nil {
		return s
	}
	return v
}

func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}
