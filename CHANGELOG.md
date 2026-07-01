# Changelog

All notable changes to DebridNest are summarized here by development phase.

## Phase 5 — *arr, Torrentio, transcode, release polish

- **qBittorrent Web API v2 subset** at `/api/v2/` for Sonarr/Radarr download clients ([docs/arr-setup.md](docs/arr-setup.md))
- **Plex/Jellyfin library** guidance via WebDAV ([docs/media-server.md](docs/media-server.md))
- **Real-Debrid API reverse proxy** compose profile for Torrentio ([docs/torrentio-setup.md](docs/torrentio-setup.md))
- **Stremio addon search** — dedupe streams, season-pack fallback, env toggles
- **Seeding controls** — optional post-complete seeding with ratio/time limits
- **Optional HLS transcode** for incompatible audio via ffmpeg ([docs/transcode.md](docs/transcode.md))
- **Release tooling** — curl installer, env presets, GitHub release workflow, Helm chart

## Phase 4 — WebDAV, metrics, VPN, Stremio quality

- WebDAV library access at `/webdav/` ([docs/webdav.md](docs/webdav.md))
- Prometheus metrics at `/metrics` when `DEBRIDNEST_METRICS=1`
- VPN sidecar profile with Gluetun ([docs/vpn.md](docs/vpn.md))
- Stremio quality filters (resolution, SDR, file size) and IINA external player
- Refreshed React management dashboard

## Phase 3 — Operations dashboard and Stremio UX

- Disk retention and quota enforcement with background cleanup
- Admin API at `/api/v1/` (stats, torrent list, delete, retry)
- `.torrent` file upload via `POST /torrents/addTorrent`
- Download rate limiting on signed `/dl/*` URLs
- React dashboard at `/dashboard/` (overview + torrent management)
- Stremio cache-first stream ordering and downloading placeholders
- Optional Zilean scraper for additional indexers

## Phase 2 — Real Stremio playback and remote deploy

- Jackett/Prowlarr-powered Stremio addon for movie/series metadata IDs
- `GET /torrents/instantAvailability/{hash}` local cache
- `addMagnet` dedup and resume for existing hashes
- Caddy TLS profile (`docker compose --profile tls`)
- Cloudflare Tunnel profile (`docker compose --profile tunnel`)
- Stremio compose profile with Jackett auto-setup

## Phase 1 — Core debrid server

- Real-Debrid-compatible REST API subset (`/user`, `/torrents/*`, `/unrestrict/link`)
- Torrent download pipeline with progressive streaming before completion
- Signed HTTPS download links
- Docker Compose deployment
- Minimal Stremio magnet-test addon
