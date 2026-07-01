package objectstore

import (
	"testing"
)

func TestObjectKey(t *testing.T) {
	s := &Store{cfg: Config{Prefix: "debridnest"}}

	tests := []struct {
		infoHash string
		filePath string
		want     string
	}{
		{
			infoHash: "ABCDEF0123456789ABCDEF0123456789ABCDEF01",
			filePath: "/Movie.mkv",
			want:     "debridnest/abcdef0123456789abcdef0123456789abcdef01/Movie.mkv",
		},
		{
			infoHash: "abc",
			filePath: "folder/file.mp4",
			want:     "debridnest/abc/folder/file.mp4",
		},
	}

	for _, tc := range tests {
		got := s.ObjectKey(tc.infoHash, tc.filePath)
		if got != tc.want {
			t.Fatalf("ObjectKey(%q, %q) = %q, want %q", tc.infoHash, tc.filePath, got, tc.want)
		}
	}
}

func TestObjectKeyNoPrefix(t *testing.T) {
	s := &Store{cfg: Config{}}
	got := s.ObjectKey("HASH", "/a/b.mkv")
	want := "hash/a/b.mkv"
	if got != want {
		t.Fatalf("ObjectKey without prefix = %q, want %q", got, want)
	}
}

func TestLoadFromEnvDefaults(t *testing.T) {
	t.Setenv("DEBRIDNEST_S3_ENABLED", "")
	t.Setenv("DEBRIDNEST_S3_BUCKET", "")

	cfg := LoadFromEnv()
	if cfg.Enabled {
		t.Fatal("expected S3 disabled by default")
	}
	if cfg.Region != "auto" {
		t.Fatalf("default region = %q, want auto", cfg.Region)
	}
	if cfg.OffloadLocal {
		t.Fatal("expected OffloadLocal false by default")
	}
}

func TestNewDisabled(t *testing.T) {
	store, err := New(Config{Enabled: false})
	if err != nil {
		t.Fatalf("New disabled: %v", err)
	}
	if store.Enabled() {
		t.Fatal("expected store disabled")
	}
}

func TestNewEnabledRequiresBucket(t *testing.T) {
	_, err := New(Config{Enabled: true})
	if err == nil {
		t.Fatal("expected error when bucket missing")
	}
}

func TestEnabled(t *testing.T) {
	store := &Store{cfg: Config{Enabled: true}}
	if store.Enabled() {
		t.Fatal("Enabled() should be false without client")
	}
}

func TestObjectKeySanitizesPaths(t *testing.T) {
	s := &Store{cfg: Config{Prefix: "nest"}}

	tests := []struct {
		filePath string
		want     string
	}{
		{filePath: `\Season 1\ep.mkv`, want: "nest/abc/Season 1/ep.mkv"},
		{filePath: "///nested//file.mkv", want: "nest/abc/nested/file.mkv"},
		{filePath: "..\\..\\etc/passwd", want: "nest/abc/../../etc/passwd"},
	}

	for _, tc := range tests {
		got := s.ObjectKey("ABC", tc.filePath)
		if got != tc.want {
			t.Fatalf("ObjectKey(%q) = %q, want %q", tc.filePath, got, tc.want)
		}
	}
}

func TestLoadFromEnvEnabled(t *testing.T) {
	t.Setenv("DEBRIDNEST_S3_ENABLED", "1")
	t.Setenv("DEBRIDNEST_S3_BUCKET", "my-bucket")
	t.Setenv("DEBRIDNEST_S3_ENDPOINT", "https://s3.example.com")
	t.Setenv("DEBRIDNEST_S3_FORCE_PATH_STYLE", "1")
	t.Setenv("DEBRIDNEST_S3_OFFLOAD_LOCAL", "1")

	cfg := LoadFromEnv()
	if !cfg.Enabled {
		t.Fatal("expected enabled")
	}
	if cfg.Bucket != "my-bucket" {
		t.Fatalf("bucket = %q", cfg.Bucket)
	}
	if !cfg.ForcePathStyle || !cfg.OffloadLocal {
		t.Fatalf("flags: pathStyle=%v offload=%v", cfg.ForcePathStyle, cfg.OffloadLocal)
	}
}
