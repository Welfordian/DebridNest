# DebridNest retention, quotas, and rate limits

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `DEBRIDNEST_RETENTION_DAYS` | `30` | Auto-delete completed torrents after N days (`0` = disabled) |
| `DEBRIDNEST_DISK_QUOTA_GB` | `0` | Max disk usage for `/data/files` in GB (`0` = unlimited) |
| `DEBRIDNEST_DOWNLOAD_RATE_LIMIT_MBPS` | `0` | Cap download egress on `/dl/*` URLs in MB/s (`0` = unlimited) |

Retention runs every 15 minutes. When quota is exceeded, oldest completed torrents (by `ended_at`) are evicted first.

## Web dashboard

Open `http://localhost:8080/dashboard/` after starting DebridNest.

1. Paste your `DEBRIDNEST_API_TOKEN` on first visit (stored in browser `localStorage`)
2. **Overview** — disk usage, active downloads, aggregate speed
3. **Torrents** — list, delete, retry failed jobs

## Admin API

All routes require `Authorization: Bearer <DEBRIDNEST_API_TOKEN>`:

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/stats` | Disk and torrent statistics |
| GET | `/api/v1/torrents` | Full torrent list |
| DELETE | `/api/v1/torrents/{id}` | Delete torrent and files |
| POST | `/api/v1/torrents/{id}/retry` | Re-queue failed torrent |
| GET | `/api/v1/config` | Read-only config summary |

## Torrent file upload

```bash
curl -H "Authorization: Bearer $TOKEN" \
  -F "torrent=@/path/to/file.torrent" \
  http://localhost:8080/rest/1.0/torrents/addTorrent
```

## Monitoring

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/stats
curl http://localhost:8080/healthz
```
