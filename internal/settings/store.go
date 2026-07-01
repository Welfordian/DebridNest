package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/storage"
)

const (
	keyRetentionDays            = "retentionDays"
	keyDiskQuotaGb              = "diskQuotaGb"
	keyDownloadRateLimitMbps    = "downloadRateLimitMbps"
	keyWebhookDiscordURL        = "webhookDiscordUrl"
	keyWebhookNtfyTopic         = "webhookNtfyTopic"
	keyWebhookGotifyURL         = "webhookGotifyUrl"
	keyWebhookGotifyToken       = "webhookGotifyToken"
	keyNotifyOnDownloadComplete = "notifyOnDownloadComplete"
	keyNotifyOnQuotaWarning     = "notifyOnQuotaWarning"
)

var patchableKeys = map[string]bool{
	keyRetentionDays:            true,
	keyDiskQuotaGb:              true,
	keyDownloadRateLimitMbps:    true,
	keyWebhookDiscordURL:        true,
	keyWebhookNtfyTopic:         true,
	keyWebhookGotifyURL:         true,
	keyWebhookGotifyToken:       true,
	keyNotifyOnDownloadComplete: true,
	keyNotifyOnQuotaWarning:     true,
}

type Merged struct {
	RetentionDays            int     `json:"retentionDays"`
	DiskQuotaGb              int64   `json:"diskQuotaGb"`
	DownloadRateLimitMbps    float64 `json:"downloadRateLimitMbps"`
	WebhookDiscordUrl        string  `json:"webhookDiscordUrl"`
	WebhookNtfyTopic         string  `json:"webhookNtfyTopic"`
	WebhookGotifyUrl         string  `json:"webhookGotifyUrl"`
	WebhookGotifyToken       string  `json:"webhookGotifyToken"`
	NotifyOnDownloadComplete bool    `json:"notifyOnDownloadComplete"`
	NotifyOnQuotaWarning     bool    `json:"notifyOnQuotaWarning"`
}

type Store struct {
	mu        sync.RWMutex
	db        *storage.DB
	cfg       config.Config
	overrides map[string]json.RawMessage
}

func NewStore(db *storage.DB, cfg config.Config) (*Store, error) {
	s := &Store{
		db:        db,
		cfg:       cfg,
		overrides: make(map[string]json.RawMessage),
	}
	if err := s.reload(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) reload(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM runtime_settings`)
	if err != nil {
		return fmt.Errorf("load runtime settings: %w", err)
	}
	defer rows.Close()

	overrides := make(map[string]json.RawMessage)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return fmt.Errorf("scan runtime setting: %w", err)
		}
		overrides[key] = json.RawMessage(value)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	s.overrides = overrides
	s.mu.Unlock()
	return nil
}

func (s *Store) GetMerged() Merged {
	return Merged{
		RetentionDays:            s.GetRetentionDays(),
		DiskQuotaGb:              s.GetDiskQuotaGB(),
		DownloadRateLimitMbps:    s.GetDownloadRateLimitMbps(),
		WebhookDiscordUrl:        s.GetWebhookDiscordURL(),
		WebhookNtfyTopic:         s.GetWebhookNtfyTopic(),
		WebhookGotifyUrl:         s.GetWebhookGotifyURL(),
		WebhookGotifyToken:       s.GetWebhookGotifyToken(),
		NotifyOnDownloadComplete: s.GetNotifyOnDownloadComplete(),
		NotifyOnQuotaWarning:     s.GetNotifyOnQuotaWarning(),
	}
}

func (s *Store) Patch(ctx context.Context, fields map[string]any) (Merged, error) {
	if len(fields) == 0 {
		return s.GetMerged(), nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Merged{}, err
	}
	defer tx.Rollback()

	for key, value := range fields {
		if !patchableKeys[key] {
			return Merged{}, fmt.Errorf("unknown setting: %s", key)
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return Merged{}, fmt.Errorf("encode %s: %w", key, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO runtime_settings (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			key, string(raw),
		); err != nil {
			return Merged{}, fmt.Errorf("save %s: %w", key, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return Merged{}, err
	}
	if err := s.reload(ctx); err != nil {
		return Merged{}, err
	}
	return s.GetMerged(), nil
}

func (s *Store) GetRetentionDays() int {
	if v, ok := s.overrideInt(keyRetentionDays); ok {
		return v
	}
	return s.cfg.RetentionDays
}

func (s *Store) GetDiskQuotaGB() int64 {
	if v, ok := s.overrideInt64(keyDiskQuotaGb); ok {
		return v
	}
	return s.cfg.DiskQuotaGB
}

func (s *Store) GetDownloadRateLimitMbps() float64 {
	if v, ok := s.overrideFloat(keyDownloadRateLimitMbps); ok {
		return v
	}
	return s.cfg.DownloadRateLimitMB
}

func (s *Store) GetWebhookDiscordURL() string {
	if v, ok := s.overrideString(keyWebhookDiscordURL); ok {
		return v
	}
	return s.cfg.WebhookDiscordURL
}

func (s *Store) GetWebhookNtfyTopic() string {
	if v, ok := s.overrideString(keyWebhookNtfyTopic); ok {
		return v
	}
	return s.cfg.WebhookNtfyTopic
}

func (s *Store) GetWebhookGotifyURL() string {
	if v, ok := s.overrideString(keyWebhookGotifyURL); ok {
		return v
	}
	return s.cfg.WebhookGotifyURL
}

func (s *Store) GetWebhookGotifyToken() string {
	if v, ok := s.overrideString(keyWebhookGotifyToken); ok {
		return v
	}
	return s.cfg.WebhookGotifyToken
}

func (s *Store) GetNotifyOnDownloadComplete() bool {
	if v, ok := s.overrideBool(keyNotifyOnDownloadComplete); ok {
		return v
	}
	return s.cfg.NotifyOnDownloadComplete
}

func (s *Store) GetNotifyOnQuotaWarning() bool {
	if v, ok := s.overrideBool(keyNotifyOnQuotaWarning); ok {
		return v
	}
	return s.cfg.NotifyOnQuotaWarning
}

func (s *Store) DiskQuotaBytes() int64 {
	gb := s.GetDiskQuotaGB()
	if gb <= 0 {
		return 0
	}
	return gb * 1024 * 1024 * 1024
}

func (s *Store) overrideInt(key string) (int, bool) {
	raw, ok := s.rawOverride(key)
	if !ok {
		return 0, false
	}
	var v int
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, false
	}
	return v, true
}

func (s *Store) overrideInt64(key string) (int64, bool) {
	raw, ok := s.rawOverride(key)
	if !ok {
		return 0, false
	}
	var v int64
	if err := json.Unmarshal(raw, &v); err != nil {
		// JSON numbers decode as float64 when using any; try float fallback.
		var f float64
		if err := json.Unmarshal(raw, &f); err != nil {
			return 0, false
		}
		return int64(f), true
	}
	return v, true
}

func (s *Store) overrideFloat(key string) (float64, bool) {
	raw, ok := s.rawOverride(key)
	if !ok {
		return 0, false
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		return 0, false
	}
	return v, true
}

func (s *Store) overrideString(key string) (string, bool) {
	raw, ok := s.rawOverride(key)
	if !ok {
		return "", false
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", false
	}
	return v, true
}

func (s *Store) overrideBool(key string) (bool, bool) {
	raw, ok := s.rawOverride(key)
	if !ok {
		return false, false
	}
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		return false, false
	}
	return v, true
}

func (s *Store) rawOverride(key string) (json.RawMessage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	raw, ok := s.overrides[key]
	return raw, ok
}

// Ensure Store satisfies optional reload after external writes (used in tests).
func (s *Store) Reload(ctx context.Context) error {
	return s.reload(ctx)
}
