package links

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSignAndVerifyDownload(t *testing.T) {
	s := NewSigner("secret", "http://localhost:8080", "localhost", time.Hour)
	expires := time.Now().Add(time.Hour)
	relative := "deadbeef/Movie/file.mkv"

	signed := s.SignDownload(relative, expires)
	path := strings.TrimPrefix(signed, "http://localhost:8080")

	relativePath, expiresUnix, signature, ok := ParseDownloadPath(path)
	if !ok {
		t.Fatalf("parse failed for %q", path)
	}
	if relativePath != relative {
		t.Fatalf("got path %q want %q", relativePath, relative)
	}
	if expiresUnix != expires.Unix() {
		t.Fatalf("expires mismatch")
	}
	if !s.VerifyDownload(relativePath, expiresUnix, signature) {
		t.Fatal("verify failed")
	}
}

func TestSignAndVerifyHLSAsset(t *testing.T) {
	s := NewSigner("secret", "http://localhost:8080", "localhost", time.Hour)
	expires := time.Now().Add(time.Hour)

	signed := s.SignHLSAsset("TORRENT 1", 2, "variant/index.m3u8", expires)
	u, err := url.Parse(signed)
	if err != nil {
		t.Fatalf("parse signed url: %v", err)
	}
	if u.Path != "/hls/TORRENT 1/2/variant/index.m3u8" {
		t.Fatalf("path = %q", u.Path)
	}

	expiresUnix := expires.Unix()
	if got := u.Query().Get("expires"); got == "" {
		t.Fatalf("missing expires query")
	}
	if !s.VerifyHLSAsset("TORRENT 1", 2, "variant/index.m3u8", expiresUnix, u.Query().Get("sig")) {
		t.Fatal("verify failed")
	}
	if s.VerifyHLSAsset("TORRENT 1", 2, "seg000.ts", expiresUnix, u.Query().Get("sig")) {
		t.Fatal("signature verified for a different asset")
	}
	if s.VerifyHLSAsset("TORRENT 1", 2, "variant/index.m3u8", time.Now().Add(-time.Hour).Unix(), u.Query().Get("sig")) {
		t.Fatal("expired signature verified")
	}
}
