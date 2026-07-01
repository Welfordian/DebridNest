# DebridNest — Agent & Contributor Guide

Onboarding reference for AI agents and human contributors working in this repo.

## Repo map

| Path | Purpose |
|------|---------|
| `cmd/debridnest/` | Main server entrypoint |
| `internal/api/rd/` | Real-Debrid-compatible REST API (`/rest/1.0/`) |
| `internal/api/admin/` | Admin/dashboard API (`/api/v1/`) |
| `internal/api/qbit/` | qBittorrent Web API subset (`/api/v2/`) |
| `internal/torrent/` | Torrent download pipeline and manager |
| `internal/objectstore/` | S3-compatible object storage (upload, range reads, offload) |
| `web/dashboard/` | React dashboard source (Vite + TypeScript) |
| `internal/web/dashboard/` | Built dashboard assets embedded via `internal/web/embed.go` |

After changing dashboard source, rebuild and commit the embed:

```bash
cd web/dashboard && npm ci && npm run build
```

## Common commands

```bash
make test                 # go test ./...
make test-integration     # integration tests (build tag)
go vet ./...              # static analysis
make build                # dashboard build + go binary → bin/debridnest
make dashboard            # npm ci && npm run build in web/dashboard
cd web/dashboard && npm run build   # dashboard only
```

## Docker Compose profiles

Optional services are gated by profiles (see `docker-compose.yml`):

| Profile | Services |
|---------|----------|
| *(default)* | `debridnest` only |
| `stremio` | Jackett, jackett-setup, Stremio addon |
| `tls` | Caddy reverse proxy with automatic HTTPS |
| `tls-nginx` | Nginx TLS on port 8443 |
| `tunnel` | Cloudflare Tunnel |
| `torrentio` | Real-Debrid API proxy for Torrentio |
| `transcode` | DebridNest with ffmpeg HLS transcode |
| `vpn` | Gluetun VPN sidecar + `debridnest-vpn` |

Example: `docker compose --profile stremio up -d --build`

## Do not commit

- `.env` — secrets and local config (use `.env.example` as reference)
- `bin/` — local Go build output
- `internal/api/admin/files/` — local test SQLite artifacts

## Documentation

- [README.md](README.md) — quick start and feature overview
- [docs/operations.md](docs/operations.md) — retention, quotas, admin API, webhooks
- [docs/object-storage.md](docs/object-storage.md) — S3/R2/B2 object storage setup
- [docs/api-compat.md](docs/api-compat.md) — Real-Debrid API compatibility
- [CHANGELOG.md](CHANGELOG.md) — release history by phase
