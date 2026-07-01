package transcode

import (
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/torrent"
)

// Mount registers HLS transcode routes when enabled in config.
func Mount(r chi.Router, cfg config.Config, manager *torrent.Manager) error {
	if !cfg.TranscodeEnabled {
		return nil
	}

	h := newHandler(cfg, manager)
	r.Get("/hls/{torrentID}/{fileID}/master.m3u8", h.serveMaster)
	r.Get("/hls/{torrentID}/{fileID}/*", h.serveAsset)
	return nil
}

// Enabled reports whether HLS transcode routes are configured for mounting.
func Enabled(cfg config.Config) bool {
	return cfg.TranscodeEnabled
}

// MountPath returns the HLS master playlist URL prefix for a torrent file.
func MountPath(cfg config.Config, torrentID string, fileID int) string {
	if !Enabled(cfg) {
		return ""
	}
	return cfg.PublicURL + "/hls/" + torrentID + "/" + strconv.Itoa(fileID) + "/master.m3u8"
}
