package transcode

import (
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/links"
	"github.com/debridnest/debridnest/internal/torrent"
)

// Mount registers HLS transcode routes when enabled in config.
func Mount(r chi.Router, cfg config.Config, manager *torrent.Manager, signer *links.Signer) error {
	if !cfg.TranscodeEnabled {
		return nil
	}
	if signer == nil {
		signer = links.NewSigner(cfg.LinkSecret, cfg.PublicURL, cfg.Host, cfg.LinkTTL)
	}

	h := newHandler(cfg, manager, signer)
	r.Get("/hls/{torrentID}/{fileID}/master.m3u8", h.serveMaster)
	r.Get("/hls/{torrentID}/{fileID}/*", h.serveAsset)
	return nil
}

// Enabled reports whether HLS transcode routes are configured for mounting.
func Enabled(cfg config.Config) bool {
	return cfg.TranscodeEnabled
}

// MountPath returns a signed HLS master playlist URL for a torrent file.
func MountPath(cfg config.Config, torrentID string, fileID int) string {
	if !Enabled(cfg) {
		return ""
	}
	signer := links.NewSigner(cfg.LinkSecret, cfg.PublicURL, cfg.Host, cfg.LinkTTL)
	return signer.SignHLSAsset(torrentID, fileID, "master.m3u8", time.Now().Add(cfg.LinkTTL))
}
