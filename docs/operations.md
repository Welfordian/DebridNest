# DebridNest retention, quotas, and rate limits

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `DEBRIDNEST_RETENTION_DAYS` | `30` | Auto-delete completed torrents after N days (`0` = disabled) |
| `DEBRIDNEST_DISK_QUOTA_GB` | `0` | Max disk usage for `/data/files` in GB (`0` = unlimited) |
| `DEBRIDNEST_DOWNLOAD_RATE_LIMIT_MBPS` | `0` | Cap download egress on `/dl/*` URLs in MB/s (`0` = unlimited) |
| `DEBRIDNEST_METRICS` | `0` | Enable Prometheus metrics at `GET /metrics` (`1` = enabled) |

Retention runs every 15 minutes. When quota is exceeded, oldest completed torrents (by `ended_at`) are evicted first.

## Web dashboard

Open `http://localhost:8080/dashboard/` after starting DebridNest.

1. Paste your API token on first visit (stored in browser `localStorage`)
2. **Overview** — disk usage, active downloads, aggregate speed, quick links to other tabs
3. **Torrents** — list, add magnet, upload `.torrent`, delete, batch delete, retry failed jobs
4. **Library** — browse completed files and copy download links
5. **Settings** — retention, quota, rate limit, webhook URLs, notification toggles, maintenance cleanup, purge (admin)
6. **Users** *(admin)* — create/delete users, rotate tokens
7. **Activity** *(admin)* — audit log of admin and torrent actions
8. **Logs** *(admin)* — recent server log lines

## Multi-user authentication

By default (`DEBRIDNEST_MULTI_USER` unset or `1`), DebridNest stores users in SQLite and validates per-user API tokens. Set `DEBRIDNEST_MULTI_USER=0` for legacy single-token mode (only `DEBRIDNEST_API_TOKEN` is accepted).

| Variable | Default | Description |
|----------|---------|-------------|
| `DEBRIDNEST_MULTI_USER` | `1` | `0` = legacy single shared token; `1` = per-user tokens in DB |

On first start with multi-user enabled, an **owner** admin user is bootstrapped using the hash of `DEBRIDNEST_API_TOKEN`. Existing deployments can keep using that token; create additional users via the API.

All routes require `Authorization: Bearer <token>`.

### Current user

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/me
```

Returns `{"name":"owner","role":"admin","admin":true}`.

### User management (admin only)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/users` | List users (no token hashes) |
| POST | `/api/v1/users` | Create user `{"name","role"}` — returns token once |
| DELETE | `/api/v1/users/{id}` | Delete user |
| POST | `/api/v1/users/{id}/rotate-token` | Rotate token — returns new token once |

Admin-only routes also include purge, settings PATCH, activity log, and server logs.

### Runtime settings

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/settings` | Merged env defaults + runtime overrides |
| PATCH | `/api/v1/settings` | Update retention, quota, rate limit, webhooks (admin) |

### Activity and logs

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/activity?limit=50&offset=0` | Audit log (admin) |
| GET | `/api/v1/logs?limit=200` | Recent server log lines (admin) |

qBittorrent login (`/api/v2/auth/login`) accepts the legacy qBit username/password **or** any valid API token in the password field.

## Admin API

All routes require `Authorization: Bearer <token>` (any valid user token in multi-user mode).

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/v1/me` | user | Current user profile (`name`, `role`, `admin`) |
| GET | `/api/v1/system` | user | Server version, uptime, feature flags |
| GET | `/api/v1/stats` | user | Disk and torrent statistics |
| GET | `/api/v1/config` | user | Read-only config summary |
| GET | `/api/v1/torrents` | user | Full torrent list (`?limit=`) |
| GET | `/api/v1/torrents/{id}` | user | Torrent detail with files and links |
| POST | `/api/v1/torrents/add` | user | Add magnet `{"magnet"}` |
| POST | `/api/v1/torrents/upload` | user | Upload `.torrent` file (multipart field `torrent`) |
| POST | `/api/v1/torrents/batch-delete` | user | Delete many `{"ids":["…"]}` |
| DELETE | `/api/v1/torrents/{id}` | user | Delete torrent and files |
| POST | `/api/v1/torrents/{id}/retry` | user | Re-queue failed torrent |
| POST | `/api/v1/maintenance/cleanup` | user | Run retention/quota cleanup now |
| GET | `/api/v1/settings` | user | Merged env defaults + runtime overrides |
| POST | `/api/v1/torrents/purge` | admin | Purge by status filter `{"filter"}` |
| PATCH | `/api/v1/settings` | admin | Update retention, quota, rate limit, webhooks |
| GET | `/api/v1/activity` | admin | Audit log (`?limit=&offset=`) |
| GET | `/api/v1/logs` | admin | Recent server log lines (`?limit=`) |

User management routes (`/api/v1/users/*`) are documented in [Multi-user authentication](#multi-user-authentication) below.

## Notifications and webhooks

Configure via the dashboard **Settings** tab or `PATCH /api/v1/settings` (admin). Environment defaults can be set in `.env`:

| Setting key | Env variable | Description |
|-------------|--------------|-------------|
| `webhookDiscordUrl` | `DEBRIDNEST_WEBHOOK_DISCORD_URL` | Discord incoming webhook URL |
| `webhookNtfyTopic` | `DEBRIDNEST_WEBHOOK_NTFY_TOPIC` | ntfy.sh topic name |
| `webhookGotifyUrl` | `DEBRIDNEST_WEBHOOK_GOTIFY_URL` | Gotify server base URL |
| `webhookGotifyToken` | `DEBRIDNEST_WEBHOOK_GOTIFY_TOKEN` | Gotify app token |
| `notifyOnDownloadComplete` | `DEBRIDNEST_NOTIFY_ON_DOWNLOAD_COMPLETE` | Notify when a torrent finishes downloading (default `true`) |
| `notifyOnQuotaWarning` | `DEBRIDNEST_NOTIFY_ON_QUOTA_WARNING` | Notify when disk quota threshold is reached (default `true`) |

When enabled, DebridNest posts to all configured webhook targets. Events:

- **Download complete** — torrent reaches `downloaded` status
- **Quota warning** — disk usage exceeds configured quota during retention checks

## Torrent file upload

```bash
curl -H "Authorization: Bearer $TOKEN" \
  -F "torrent=@/path/to/file.torrent" \
  http://localhost:8080/rest/1.0/torrents/addTorrent
```

## Monitoring

### Health check

Unauthenticated liveness probe:

```bash
curl -f http://localhost:8080/healthz
```

Use this for Docker `HEALTHCHECK`, Kubernetes liveness probes, and uptime monitors. Expect HTTP 200 and body `ok`.

### Admin stats

Authenticated operational snapshot (disk, active torrents, speed):

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/stats
```

### Prometheus metrics

Set `DEBRIDNEST_METRICS=1` to expose `GET /metrics` (no auth). Scrape with Prometheus, Grafana Agent, or VictoriaMetrics.

| Metric | Type | Description |
|--------|------|-------------|
| `debridnest_http_requests_total` | counter | HTTP requests by method, normalized path, status code |
| `debridnest_active_torrents` | gauge | Torrents downloading or queued |
| `debridnest_disk_bytes_used` | gauge | Bytes under the files directory |
| `debridnest_download_bytes_served_total` | counter | Bytes served on signed `/dl/*` URLs |

Example scrape config:

```yaml
scrape_configs:
  - job_name: debridnest
    static_configs:
      - targets: ["localhost:8080"]
    metrics_path: /metrics
```

Restrict `/metrics` to internal networks (firewall or reverse proxy) in production.

## Production checklist

- [ ] Set a strong random `DEBRIDNEST_API_TOKEN` and optional separate `DEBRIDNEST_LINK_SECRET`
- [ ] Set `DEBRIDNEST_PUBLIC_URL` to the HTTPS URL clients use for streaming
- [ ] Enable TLS (`docker compose --profile tls`) or Cloudflare Tunnel (`--profile tunnel`)
- [ ] Configure `DEBRIDNEST_DISK_QUOTA_GB` and `DEBRIDNEST_RETENTION_DAYS` for your disk budget
- [ ] Enable `DEBRIDNEST_METRICS=1` and scrape `/metrics`; alert on disk usage and error rates
- [ ] Monitor `/healthz` with an external uptime checker
- [ ] Route torrent traffic through VPN if required — see [vpn.md](vpn.md)
- [ ] Back up the `debridnest-data` Docker volume (SQLite DB + cached files)
- [ ] Do not expose port `42069` publicly unless you intend to accept inbound peer connections

## VPN

See [vpn.md](vpn.md) for routing BitTorrent traffic through Gluetun.
