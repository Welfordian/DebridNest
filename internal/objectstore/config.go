package objectstore

import (
	"os"
	"strings"
)

type Config struct {
	Enabled        bool
	Endpoint       string
	Bucket         string
	Region         string
	AccessKey      string
	SecretKey      string
	Prefix         string
	ForcePathStyle bool
	OffloadLocal   bool
	// EarlyOffload uploads each selected file as soon as it finishes downloading,
	// instead of waiting for the entire torrent to reach downloaded status.
	EarlyOffload bool
}

func LoadFromEnv() Config {
	enabled := os.Getenv("DEBRIDNEST_S3_ENABLED") == "1"
	endpoint := strings.TrimSpace(os.Getenv("DEBRIDNEST_S3_ENDPOINT"))
	bucket := strings.TrimSpace(os.Getenv("DEBRIDNEST_S3_BUCKET"))
	region := getenv("DEBRIDNEST_S3_REGION", "auto")
	prefix := strings.Trim(os.Getenv("DEBRIDNEST_S3_PREFIX"), "/")
	forcePathStyle := os.Getenv("DEBRIDNEST_S3_FORCE_PATH_STYLE") == "1"
	direct := os.Getenv("DEBRIDNEST_S3_DIRECT") == "1"
	offloadLocal := os.Getenv("DEBRIDNEST_S3_OFFLOAD_LOCAL") == "1"
	if direct && os.Getenv("DEBRIDNEST_S3_OFFLOAD_LOCAL") == "" {
		offloadLocal = true
	}
	earlyOffload := os.Getenv("DEBRIDNEST_S3_EARLY_OFFLOAD") == "1" || direct

	return Config{
		Enabled:        enabled,
		Endpoint:       endpoint,
		Bucket:         bucket,
		Region:         region,
		AccessKey:      os.Getenv("DEBRIDNEST_S3_ACCESS_KEY"),
		SecretKey:      os.Getenv("DEBRIDNEST_S3_SECRET_KEY"),
		Prefix:         prefix,
		ForcePathStyle: forcePathStyle,
		OffloadLocal:   offloadLocal,
		EarlyOffload:   earlyOffload,
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
