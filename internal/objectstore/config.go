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
}

func LoadFromEnv() Config {
	enabled := os.Getenv("DEBRIDNEST_S3_ENABLED") == "1"
	endpoint := strings.TrimSpace(os.Getenv("DEBRIDNEST_S3_ENDPOINT"))
	bucket := strings.TrimSpace(os.Getenv("DEBRIDNEST_S3_BUCKET"))
	region := getenv("DEBRIDNEST_S3_REGION", "auto")
	prefix := strings.Trim(os.Getenv("DEBRIDNEST_S3_PREFIX"), "/")
	forcePathStyle := os.Getenv("DEBRIDNEST_S3_FORCE_PATH_STYLE") == "1"
	offloadLocal := os.Getenv("DEBRIDNEST_S3_OFFLOAD_LOCAL") == "1"

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
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
