package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"

	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/objectstore"
	"github.com/debridnest/debridnest/internal/storage"
)

const maxQuotaGB = int64(8589934591)

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
	keyS3Enabled                = "s3Enabled"
	keyS3Endpoint               = "s3Endpoint"
	keyS3Bucket                 = "s3Bucket"
	keyS3Region                 = "s3Region"
	keyS3Prefix                 = "s3Prefix"
	keyS3AccessKey              = "s3AccessKey"
	keyS3SecretKey              = "s3SecretKey"
	keyS3ForcePathStyle         = "s3ForcePathStyle"
	keyS3OffloadLocal           = "s3OffloadLocal"
	keyS3QuotaGb                = "s3QuotaGb"
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
	keyS3Enabled:                true,
	keyS3Endpoint:               true,
	keyS3Bucket:                 true,
	keyS3Region:                 true,
	keyS3Prefix:                 true,
	keyS3AccessKey:              true,
	keyS3SecretKey:              true,
	keyS3ForcePathStyle:         true,
	keyS3OffloadLocal:           true,
	keyS3QuotaGb:                true,
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
	S3Enabled                bool    `json:"s3Enabled"`
	S3Endpoint               string  `json:"s3Endpoint"`
	S3Bucket                 string  `json:"s3Bucket"`
	S3Region                 string  `json:"s3Region"`
	S3Prefix                 string  `json:"s3Prefix"`
	S3AccessKey              string  `json:"s3AccessKey"`
	S3SecretKey              string  `json:"s3SecretKey"`
	S3ForcePathStyle         bool    `json:"s3ForcePathStyle"`
	S3OffloadLocal           bool    `json:"s3OffloadLocal"`
	S3QuotaGb                int64   `json:"s3QuotaGb"`
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
		S3Enabled:                s.GetS3Enabled(),
		S3Endpoint:               s.GetS3Endpoint(),
		S3Bucket:                 s.GetS3Bucket(),
		S3Region:                 s.GetS3Region(),
		S3Prefix:                 s.GetS3Prefix(),
		S3AccessKey:              s.GetS3AccessKey(),
		S3SecretKey:              s.GetS3SecretKey(),
		S3ForcePathStyle:         s.GetS3ForcePathStyle(),
		S3OffloadLocal:           s.GetS3OffloadLocal(),
		S3QuotaGb:                s.GetS3QuotaGB(),
	}
}

// RedactForNonAdmin returns merged settings with webhook secrets and URLs omitted.
func (s *Store) RedactForNonAdmin() Merged {
	m := s.GetMerged()
	m.WebhookDiscordUrl = redactWebhookURL(m.WebhookDiscordUrl)
	m.WebhookNtfyTopic = redactWebhookURL(m.WebhookNtfyTopic)
	m.WebhookGotifyUrl = redactWebhookURL(m.WebhookGotifyUrl)
	m.WebhookGotifyToken = ""
	m.S3AccessKey = redactSecretValue(m.S3AccessKey)
	m.S3SecretKey = ""
	return m
}

func redactSecretValue(v string) string {
	if v == "" {
		return ""
	}
	return "(configured)"
}

func redactWebhookURL(u string) string {
	if u == "" {
		return ""
	}
	return "(configured)"
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
		value, err = normalizePatchValue(key, value)
		if err != nil {
			return Merged{}, err
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

func normalizePatchValue(key string, value any) (any, error) {
	if key != keyS3QuotaGb {
		return value, nil
	}
	v, err := nonNegativeIntegerSetting(key, value)
	if err != nil {
		return nil, err
	}
	return v, nil
}

func nonNegativeIntegerSetting(key string, value any) (int64, error) {
	var v int64
	switch n := value.(type) {
	case int:
		v = int64(n)
	case int8:
		v = int64(n)
	case int16:
		v = int64(n)
	case int32:
		v = int64(n)
	case int64:
		v = n
	case uint:
		if uint64(n) > uint64(maxQuotaGB) {
			return 0, fmt.Errorf("%s must be between 0 and %d", key, maxQuotaGB)
		}
		v = int64(n)
	case uint8:
		v = int64(n)
	case uint16:
		v = int64(n)
	case uint32:
		v = int64(n)
	case uint64:
		if n > uint64(maxQuotaGB) {
			return 0, fmt.Errorf("%s must be between 0 and %d", key, maxQuotaGB)
		}
		v = int64(n)
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) || math.Trunc(n) != n {
			return 0, fmt.Errorf("%s must be a whole number", key)
		}
		if n < 0 || n > float64(maxQuotaGB) {
			return 0, fmt.Errorf("%s must be between 0 and %d", key, maxQuotaGB)
		}
		v = int64(n)
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, fmt.Errorf("%s must be a whole number", key)
		}
		v = i
	default:
		return 0, fmt.Errorf("%s must be a whole number", key)
	}
	if v < 0 || v > maxQuotaGB {
		return 0, fmt.Errorf("%s must be between 0 and %d", key, maxQuotaGB)
	}
	return v, nil
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

func (s *Store) GetS3Enabled() bool {
	if v, ok := s.overrideBool(keyS3Enabled); ok {
		return v
	}
	return s.s3Defaults().Enabled
}

func (s *Store) GetS3Endpoint() string {
	if v, ok := s.overrideString(keyS3Endpoint); ok {
		return v
	}
	return s.s3Defaults().Endpoint
}

func (s *Store) GetS3Bucket() string {
	if v, ok := s.overrideString(keyS3Bucket); ok {
		return v
	}
	return s.s3Defaults().Bucket
}

func (s *Store) GetS3Region() string {
	if v, ok := s.overrideString(keyS3Region); ok {
		return v
	}
	return s.s3Defaults().Region
}

func (s *Store) GetS3Prefix() string {
	if v, ok := s.overrideString(keyS3Prefix); ok {
		return v
	}
	return s.s3Defaults().Prefix
}

func (s *Store) GetS3AccessKey() string {
	if v, ok := s.overrideString(keyS3AccessKey); ok {
		return v
	}
	return s.s3Defaults().AccessKey
}

func (s *Store) GetS3SecretKey() string {
	if v, ok := s.overrideString(keyS3SecretKey); ok {
		return v
	}
	return s.s3Defaults().SecretKey
}

func (s *Store) GetS3ForcePathStyle() bool {
	if v, ok := s.overrideBool(keyS3ForcePathStyle); ok {
		return v
	}
	return s.s3Defaults().ForcePathStyle
}

func (s *Store) GetS3OffloadLocal() bool {
	if v, ok := s.overrideBool(keyS3OffloadLocal); ok {
		return v
	}
	return s.s3Defaults().OffloadLocal
}

func (s *Store) GetS3QuotaGB() int64 {
	if v, ok := s.overrideInt64(keyS3QuotaGb); ok {
		return v
	}
	return s.s3Defaults().QuotaGB
}

func (s *Store) S3Config() objectstore.Config {
	return objectstore.Config{
		Enabled:        s.GetS3Enabled(),
		Endpoint:       s.GetS3Endpoint(),
		Bucket:         s.GetS3Bucket(),
		Region:         s.GetS3Region(),
		AccessKey:      s.GetS3AccessKey(),
		SecretKey:      s.GetS3SecretKey(),
		Prefix:         s.GetS3Prefix(),
		ForcePathStyle: s.GetS3ForcePathStyle(),
		OffloadLocal:   s.GetS3OffloadLocal(),
		QuotaGB:        s.GetS3QuotaGB(),
		EarlyOffload:   s.s3Defaults().EarlyOffload,
	}
}

func (s *Store) s3Defaults() objectstore.Config {
	return objectstore.LoadFromEnv()
}

func (s *Store) DiskQuotaBytes() int64 {
	gb := s.GetDiskQuotaGB()
	if gb <= 0 {
		return 0
	}
	return gb * 1024 * 1024 * 1024
}

func (s *Store) S3QuotaBytes() int64 {
	gb := s.GetS3QuotaGB()
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
