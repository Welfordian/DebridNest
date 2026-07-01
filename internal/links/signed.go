package links

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Signer struct {
	Secret    string
	PublicURL string
	TTL       time.Duration
	Host      string
}

func NewSigner(secret, publicURL, host string, ttl time.Duration) *Signer {
	return &Signer{
		Secret:    secret,
		PublicURL: strings.TrimRight(publicURL, "/"),
		TTL:       ttl,
		Host:      host,
	}
}

func (s *Signer) HostLink(linkID string) string {
	return fmt.Sprintf("%s/d/%s", s.PublicURL, linkID)
}

func (s *Signer) SignDownload(relativePath string, expires time.Time) string {
	payload := fmt.Sprintf("%s|%d", relativePath, expires.Unix())
	mac := hmac.New(sha256.New, []byte(s.Secret))
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%s/dl/%d/%s/%s", s.PublicURL, expires.Unix(), url.PathEscape(relativePath), sig)
}

func (s *Signer) VerifyDownload(relativePath string, expiresUnix int64, sig string) bool {
	if time.Now().Unix() > expiresUnix {
		return false
	}
	payload := fmt.Sprintf("%s|%d", relativePath, expiresUnix)
	mac := hmac.New(sha256.New, []byte(s.Secret))
	mac.Write([]byte(payload))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sig))
}

func (s *Signer) SignHLSAsset(torrentID string, fileID int, asset string, expires time.Time) string {
	expiresUnix := expires.Unix()
	sig := s.signHLSPayload(torrentID, fileID, asset, expiresUnix)
	return fmt.Sprintf("%s/hls/%s/%d/%s?expires=%d&sig=%s", s.PublicURL, url.PathEscape(torrentID), fileID, escapePath(asset), expiresUnix, sig)
}

func (s *Signer) VerifyHLSAsset(torrentID string, fileID int, asset string, expiresUnix int64, sig string) bool {
	if time.Now().Unix() > expiresUnix {
		return false
	}
	expected := s.signHLSPayload(torrentID, fileID, asset, expiresUnix)
	return hmac.Equal([]byte(expected), []byte(sig))
}

func (s *Signer) signHLSPayload(torrentID string, fileID int, asset string, expiresUnix int64) string {
	payload := fmt.Sprintf("hls|%s|%d|%s|%d", torrentID, fileID, asset, expiresUnix)
	mac := hmac.New(sha256.New, []byte(s.Secret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func NewLinkID() string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	return strings.ToUpper(hex.EncodeToString(sum[:6]))
}

func MimeType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".mkv":
		return "video/x-matroska"
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".avi":
		return "video/x-msvideo"
	case ".webm":
		return "video/webm"
	case ".srt":
		return "application/x-subrip"
	default:
		return "application/octet-stream"
	}
}

func ParseDownloadPath(path string) (relativePath string, expiresUnix int64, sig string, ok bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 4 || parts[0] != "dl" {
		return "", 0, "", false
	}
	expiresUnix, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return "", 0, "", false
	}
	relativePath, err = url.PathUnescape(strings.Join(parts[2:len(parts)-1], "/"))
	if err != nil {
		return "", 0, "", false
	}
	sig = parts[len(parts)-1]
	return relativePath, expiresUnix, sig, true
}
