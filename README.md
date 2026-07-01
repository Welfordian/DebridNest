# DebridNest

Self-hosted, open-source personal debrid server with a **Real-Debrid-compatible REST API** and a **Stremio addon** (Jackett/Prowlarr search). Run torrent downloads on your VPS or home server, then stream via signed HTTPS links.

> **Not affiliated** with Real-Debrid, Stremio, Jackett, or Prowlarr. See [DISCLAIMER.md](DISCLAIMER.md).

## Features

- Real-Debrid-compatible API subset for Stremio (`/user`, `/torrents/*`, `/unrestrict/link`, `/torrents/instantAvailability/*`)
- Jackett/Prowlarr search in the Stremio addon
- Progressive streaming before torrents finish downloading
- Web dashboard at `/dashboard/` for torrent and disk management
- Disk retention, quotas, and optional download rate limiting
- Docker Compose with optional Caddy TLS, Cloudflare Tunnel, VPN sidecar, and Stremio addon profiles
- **Phase 4:** WebDAV library access (Infuse/Kodi/rclone), Prometheus metrics, quality filters + IINA external player in Stremio addon
- **Phase 5:** Sonarr/Radarr qBit API, Torrentio RD proxy, HLS transcode, seeding controls, one-line installer, release workflow

## One-line install

```bash
curl -fsSL https://raw.githubusercontent.com/Welfordian/DebridNest/main/scripts/install.sh | bash
```

Or clone and run locally:

```bash
bash scripts/install.sh
```

Environment presets for common setups live in [`deploy/env/`](deploy/env/) (`home`, `vps`, `minimal`).

## Phase 5 features

| Feature | Docs |
|---------|------|
| qBittorrent Web API for Sonarr/Radarr | [docs/arr-setup.md](docs/arr-setup.md) |
| Plex/Jellyfin library via WebDAV | [docs/media-server.md](docs/media-server.md) |
| Torrentio + Real-Debrid API proxy | [docs/torrentio-setup.md](docs/torrentio-setup.md) |
| Optional HLS transcode (ffmpeg) | [docs/transcode.md](docs/transcode.md) |
| Seeding controls (`DEBRIDNEST_SEED_*`) | [docs/operations.md](docs/operations.md) |
| Env presets + Helm chart | [deploy/env/](deploy/env/), [deploy/helm/debridnest/](deploy/helm/debridnest/) |
| Release notes | [CHANGELOG.md](CHANGELOG.md) |

## Phase 4 features

| Feature | Docs |
|---------|------|
| WebDAV at `/webdav/` (read-only, Basic auth) | [docs/webdav.md](docs/webdav.md) |
| Prometheus metrics at `/metrics` (`DEBRIDNEST_METRICS=1`) | [docs/operations.md](docs/operations.md) |
| VPN sidecar (Gluetun) for torrent traffic | [docs/vpn.md](docs/vpn.md) |
| Stremio quality filters (resolution, SDR, file size) | [docs/stremio-setup.md](docs/stremio-setup.md) |
| IINA external player helper | [docs/stremio-setup.md](docs/stremio-setup.md) |

## Prerequisites

- Docker and Docker Compose
- At least one torrent indexer added in Jackett (web UI at `http://localhost:9117` after first boot)
- Stremio desktop app (recommended for playback)

## Quick start

1. Copy and configure environment:

```bash
cp .env.example .env
# Set DEBRIDNEST_API_TOKEN, DEBRIDNEST_PUBLIC_URL, JACKETT_URL, JACKETT_API_KEY
```

2. Start DebridNest + Jackett + Stremio addon:

```bash
docker compose --profile stremio up -d --build
```

3. Configure Jackett (first time only):

On first boot, **jackett-setup** automatically adds public indexers (`limetorrents`, `therarbg`, `eztv`, `knaben`, `magnetz`). Open `http://localhost:9117` to add more.

Verify:

```bash
curl -s 'http://127.0.0.1:7001/diagnostics?jackettUrl=http%3A%2F%2Fjackett%3A9117' | python3 -m json.tool
```

4. Install in Stremio: `http://127.0.0.1:7001/configure` — see [docs/stremio-setup.md](docs/stremio-setup.md)

5. Open the dashboard: `http://localhost:8080/dashboard/`

6. Open any movie/show in Discover → **Streams** tab → pick a DebridNest stream.

See [docs/operations.md](docs/operations.md) for retention, quotas, and admin API.

## Remote access

See [docs/remote-access.md](docs/remote-access.md) for Caddy TLS and Cloudflare Tunnel setup.

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `DEBRIDNEST_API_TOKEN` | *(required)* | Bearer token for API auth |
| `DEBRIDNEST_PUBLIC_URL` | `http://localhost:8080` | Public URL in download/host links |
| `DEBRIDNEST_DOMAIN` | `localhost` | Domain for Caddy `--profile tls` |
| `DEBRIDNEST_DATA_DIR` | `/data` | Persistent storage root |
| `JACKETT_URL` | `http://jackett:9117` (Docker) | Jackett base URL for the Stremio addon |
| `JACKETT_API_KEY` | — | Jackett/Prowlarr API key |
| `CLOUDFLARE_TUNNEL_TOKEN` | — | For `--profile tunnel` |
| `DEBRIDNEST_RETENTION_DAYS` | `30` | Auto-delete completed torrents after N days |
| `DEBRIDNEST_DISK_QUOTA_GB` | `0` | Disk quota in GB (0 = unlimited) |
| `DEBRIDNEST_DOWNLOAD_RATE_LIMIT_MBPS` | `0` | Download egress cap on `/dl/*` |
| `DEBRIDNEST_WEBDAV_ENABLED` | `1` | WebDAV library access at `/webdav/` |
| `DEBRIDNEST_WEBDAV_USER` / `DEBRIDNEST_WEBDAV_PASSWORD` | — | Custom WebDAV Basic auth (default: user `debridnest`, password = API token) |
| `DEBRIDNEST_METRICS` | `0` | Enable Prometheus metrics at `/metrics` |
| `PREFER_SDR` | `0` | Stremio addon: prefer SDR over HDR/DV |
| `MAX_RESOLUTION` | `0` | Stremio addon: max resolution (`720`, `1080`, `2160`; `0` = any) |
| `MAX_FILE_SIZE_GB` | `0` | Stremio addon: max torrent size in GB (`0` = unlimited) |

## Legal

See [DISCLAIMER.md](DISCLAIMER.md). You are responsible for lawful use of this software.

## License

MIT — see [LICENSE](LICENSE).
