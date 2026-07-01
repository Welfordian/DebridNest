package qbit

import (
	"github.com/go-chi/chi/v5"

	"github.com/debridnest/debridnest/internal/config"
	"github.com/debridnest/debridnest/internal/torrent"
)

// Mount registers qBittorrent Web API v2 routes at /api/v2/.
func Mount(r chi.Router, cfg config.Config, manager *torrent.Manager) {
	NewHandler(cfg, manager).Mount(r)
}
