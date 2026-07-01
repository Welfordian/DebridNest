package links

import (
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
