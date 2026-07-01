# Sonarr and Radarr setup

DebridNest exposes a [qBittorrent Web API v2](https://github.com/qbittorrent/qBittorrent/wiki/WebUI-API-(qBittorrent-5.0)) subset at `/api/v2/`. Sonarr and Radarr can use it as a download client instead of running qBittorrent locally.

## Prerequisites

- DebridNest running and reachable from Sonarr/Radarr
- `DEBRIDNEST_API_TOKEN` set (used as the default qBit password)

## Credentials

| Variable | Default | Description |
|----------|---------|-------------|
| `DEBRIDNEST_QBIT_USER` | `debridnest` | qBittorrent Web UI username |
| `DEBRIDNEST_QBIT_PASSWORD` | *(API token)* | qBittorrent Web UI password |

When `DEBRIDNEST_QBIT_PASSWORD` is unset, the password is your `DEBRIDNEST_API_TOKEN`.

## Sonarr

1. Open **Settings → Download Clients → + → qBittorrent**
2. Configure:

   | Field | Value |
   |-------|-------|
   | Name | `DebridNest` |
   | Host | DebridNest hostname (e.g. `debridnest` in Docker, or `localhost`) |
   | Port | `8080` (or your `DEBRIDNEST_LISTEN` port) |
   | Username | `debridnest` |
   | Password | your `DEBRIDNEST_API_TOKEN` |
   | Category | `tv-sonarr` (recommended) |
   | Use SSL | enable if using HTTPS |

3. Click **Test** — should report success
4. Set **Completed Download Handling** as usual; DebridNest reports completed torrents as `pausedUP` with `progress: 1`

### Remote path mapping

DebridNest reports `save_path` as `/downloads` and `content_path` as `/downloads/<torrent name>`. If Sonarr runs in a separate container, add a **Remote Path Mapping**:

| Host | Remote Path | Local Path |
|------|-------------|------------|
| `debridnest` | `/downloads` | `/path/to/debridnest/data/files` |

Adjust the local path to match your `DEBRIDNEST_DATA_DIR/files` mount inside the Sonarr container.

## Radarr

Same steps as Sonarr, but use category `movies-radarr` (or your preferred category name).

1. **Settings → Download Clients → + → qBittorrent**
2. Same host, port, username, and password as above
3. Category: `movies-radarr`
4. Add the same remote path mapping if Radarr is in a separate container

## Supported API endpoints

| Method | Path | Purpose |
|--------|------|---------|
| POST | `/api/v2/auth/login` | Cookie-based login |
| GET | `/api/v2/app/version` | Client compatibility check |
| GET | `/api/v2/app/webapiVersion` | API version (`2.11.0`) |
| POST | `/api/v2/torrents/add` | Add magnet (`urls` + optional `category`) |
| GET | `/api/v2/torrents/info` | List torrents |
| POST | `/api/v2/torrents/delete` | Remove by info hash |
| GET | `/api/v2/sync/maindata` | Polling sync (full state stub) |

## curl sanity check

```bash
# Login (sets SID cookie)
curl -c /tmp/qbit-cookie -X POST \
  -d 'username=debridnest&password='"$DEBRIDNEST_API_TOKEN" \
  http://localhost:8080/api/v2/auth/login

# Version
curl -b /tmp/qbit-cookie http://localhost:8080/api/v2/app/version

# Add magnet
curl -b /tmp/qbit-cookie -X POST \
  -d 'urls=magnet:?xt=urn:btih:YOUR_HASH&category=tv-sonarr' \
  http://localhost:8080/api/v2/torrents/add

# List torrents
curl -b /tmp/qbit-cookie http://localhost:8080/api/v2/torrents/info
```

## Status mapping

DebridNest maps internal statuses to qBittorrent states:

| DebridNest status | qBit state |
|-------------------|------------|
| `magnet_conversion`, `waiting_files_selection` | `metaDL` |
| `queued` | `queuedDL` |
| `downloading` (with speed) | `downloading` |
| `downloading` (no speed) | `stalledDL` |
| `downloaded` | `pausedUP` |
| `error`, `magnet_error`, `dead` | `error` |

## Notes

- Categories are tracked in memory for the running process; set a distinct category per app (`tv-sonarr`, `movies-radarr`) so each only sees its own downloads.
- DebridNest auto-selects the largest video file after a short delay (`DEBRIDNEST_AUTO_SELECT_SECONDS`, default 5s).
- For library playback after import, see [media-server.md](media-server.md).
