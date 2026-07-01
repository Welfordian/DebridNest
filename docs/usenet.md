# Usenet (NZB) support

DebridNest can download from Usenet via an **NZBGet sidecar** and serve completed files through the same signed `/dl/` links, S3 offload, and RD-compatible API as torrents.

## Architecture (recommended)

**Option A — NZBGet sidecar + Newznab indexer (implemented MVP)**

| Component | Role |
|-----------|------|
| **NZBFinder** (or Prowlarr) | Newznab API — searches for NZB files |
| **NZBGet** | Downloads and unpacks NZBs using your **Usenet provider** |
| **DebridNest** | `POST /rest/1.0/torrents/addNzb` submits NZBs to NZBGet, tracks progress, streams results |
| **Stremio addon** | Merges Jackett torrent results with Newznab Usenet results |

This reuses the existing torrent record model (status, progress, `/torrents/info`, `/unrestrict/link`) without a native Go NZB decoder.

### Why not built-in Go (Option B)?

A native NZB downloader (nzbgo, etc.) would need Usenet provider integration, par2 repair, unrar/unpack, and queue management — duplicating NZBGet/SABnzbd. The sidecar approach ships faster and matches how most self-hosters already run Usenet.

### VPN note

Usenet uses SSL to your provider (port 563), not BitTorrent. The `vpn` profile routes **torrent** traffic through Gluetun; NZBGet can share the Gluetun network namespace (`nzbget-vpn` service) if your provider requires VPN egress.

## Requirements

1. **Usenet provider account** (Eweka, Newshosting, UsenetExpress, Frugal, etc.) — NZBFinder is only an **indexer**, not a download provider.
2. **NZBGet** configured with provider credentials in `deploy/nzbget/nzbget.conf` or the NZBGet web UI.
3. **Indexer API key** (e.g. NZBFinder) for Stremio search.

## Docker Compose

Enable with the `usenet` profile (included automatically in `stremio` profile):

```bash
docker compose --profile stremio --profile usenet up -d --build
```

With VPN:

```bash
docker compose --profile vpn --profile stremio --profile usenet up -d --build
```

Services:

- `nzbget` — port 6789 (web UI), writes to `/data/files/nzb` (shared volume)
- `nzbget-vpn` — same, behind Gluetun (use `DEBRIDNEST_NZBGET_URL=http://127.0.0.1:6789` on `debridnest-vpn`)

## Environment variables

```bash
# DebridNest → NZBGet RPC
DEBRIDNEST_NZBGET_URL=http://nzbget:6789
DEBRIDNEST_NZBGET_USER=nzbget
DEBRIDNEST_NZBGET_PASS=tegbzn6789   # match ControlPassword in nzbget.conf

# Stremio addon → NZBFinder (Newznab)
NEWZNAB_URL=https://nzbfinder.ws/api
NEWZNAB_API_KEY=your-nzbfinder-key
USENET_ENABLED=1
```

## API

### Add NZB (RD-compatible)

```http
POST /rest/1.0/torrents/addNzb
Authorization: Bearer $TOKEN
Content-Type: application/x-www-form-urlencoded

url=https://nzbfinder.ws/api?...&id=...
```

Returns the same `{ id, uri }` shape as `addMagnet`. Poll `/torrents/info/{id}` for progress; use `/unrestrict/link` when `links` appear.

### Admin API

```http
POST /api/v1/torrents/add-nzb
{"url":"https://...","name":"optional display name"}
```

## Stremio impact

When `USENET_ENABLED=1` and `NEWZNAB_API_KEY` are set, the addon searches both Jackett (torrents) and Newznab (Usenet). Usenet streams appear with a `[Usenet]` prefix. Playback uses the same DebridNest progress/buffer flow as torrents.

## Limitations vs torrents

| Feature | Torrents | Usenet (MVP) |
|---------|----------|--------------|
| Progressive streaming while downloading | Yes (anacrolix readahead) | No — wait for NZBGet to finish unpack |
| Instant availability / cache check | Yes (info hash) | No |
| qBittorrent / Sonarr add | Yes | No — use `addNzb` or Stremio |
| Seeding / ratio | Optional | N/A |
| Multi-file selection | Yes | Auto-selects largest video file |
| RD `instantAvailability` | Yes | Returns empty for NZB hashes |
| S3 offload after complete | Yes | Yes |
| Retention / quota | Yes | Yes (same torrent record) |

## Eweka provider setup

The repo ships with Eweka as the default news server in `deploy/nzbget/nzbget.conf`:

| Setting | Value |
|---------|-------|
| Host | `news.eweka.nl` |
| Port | `563` (SSL) or `119` (non-SSL) |
| Encryption | Yes (port 563) |
| Connections | 50 |

**Do not commit real credentials.** Add your Eweka login after deploy using one of:

### Option A — NZBGet web UI (recommended)

1. Open `http://<VPS-IP>:6789` (default control login: `nzbget` / `tegbzn6789`).
2. Go to **Settings → News-Servers → Eweka**.
3. Enter **Username** and **Password** from your [Eweka account dashboard](https://account.eweka.nl) (Account → Usenet server access).
4. Click **Test Connection** — expect success once credentials are correct.
5. Click **Save all changes**.

### Option B — Edit config on the VPS

1. SSH to the VPS and edit `deploy/nzbget/nzbget.conf` (or the merged file under the `nzbget-config` Docker volume if already booted).
2. Set `Server1.Username` and `Server1.Password`.
3. Restart NZBGet: `docker compose --profile vpn --profile usenet restart nzbget-vpn` (or `nzbget` without VPN).

### Test connection

After saving credentials:

1. In NZBGet UI: **Settings → News-Servers → Eweka → Test Connection**.
2. Or submit a small NZB via DebridNest `POST /rest/1.0/torrents/addNzb` and confirm progress in **History**.

## VPS setup checklist

1. SSH to VPS, edit `.env` with NZBGet and Newznab vars above.
2. Confirm `deploy/nzbget/nzbget.conf` has the correct **Usenet provider** host (Eweka: `news.eweka.nl`). Add username/password via web UI (see above).
3. `docker compose --profile vpn --profile stremio --profile usenet up -d --build`
4. Open NZBGet UI (`http://VPS:6789`), add Eweka credentials, verify **Test Connection** passes.
5. Reinstall/configure Stremio addon; Usenet streams should appear alongside torrents.
