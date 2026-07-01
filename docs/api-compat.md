# Real-Debrid API compatibility

DebridNest implements a **Stremio-focused subset** of [Real-Debrid REST v1.0](https://api.real-debrid.com/rest/1.0/) under `/rest/1.0/`. This project is **not affiliated with Real-Debrid**.

## Authentication

```
Authorization: Bearer <DEBRIDNEST_API_TOKEN>
```

Public routes (no token): `/d/{linkID}`, `/dl/{expires}/{path}/{sig}`

## Implemented endpoints

| Method | Path | Status | Notes |
|--------|------|--------|-------|
| GET | `/user` | Implemented | Always returns `type: premium` |
| POST | `/torrents/addMagnet` | Implemented | Form field `magnet`; returns `201` + `{ id, uri }` |
| POST | `/torrents/addTorrent` | Implemented | Multipart field `torrent`; max 10MB |
| GET | `/torrents/instantAvailability/{hash}` | Implemented | Comma or `/`-separated hashes; returns local cache map |
| GET | `/torrents/info/{id}` | Implemented | Full torrent detail + `files[]` + `links[]` |
| POST | `/torrents/selectFiles/{id}` | Implemented | Form field `files=all` or `files=1,2,3`; returns `204` |
| GET | `/torrents` | Implemented | Summary list; supports `limit` |
| DELETE | `/torrents/delete/{id}` | Implemented | Returns `204`; removes torrent + files |
| POST | `/unrestrict/link` | Implemented | Form field `link`; returns direct `download` URL |
| GET | `/downloads` | Partial | Returns unrestricted download history |

## Not implemented (v1)

- Global shared cache network (local `instantAvailability` only)
- `/unrestrict/folder` — hoster folders
- Hoster link unrestrict (non-torrent)
- `/streaming/transcode/*` — pass-through only
- `/time`, `/user/me`, `/user/avatar`, etc.

## Response conventions

- **`files[].id`** — 1-based integers
- **`files[].selected`** — `0` or `1` (integer)
- **`links[]`** — host URLs (`{PUBLIC_URL}/d/{linkId}`); empty until `status` is `downloaded`
- **`links[]` order** — aligned with selected files in `files[]` order
- **Dates** — ISO8601 (`2006-01-02T15:04:05.000Z`)

## Torrent status flow

```
magnet_conversion → waiting_files_selection → queued → downloading → downloaded
```

Auto-select: if no `selectFiles` within `DEBRIDNEST_AUTO_SELECT_SECONDS` (default 5s), the largest video file is selected automatically.

### instantAvailability

```
GET /torrents/instantAvailability/{hash1}/{hash2}
```

Returns hashes already downloaded on this instance:

```json
{
  "abc123...": {
    "real-debrid.com": ["1080p"]
  }
}
```

Empty `{}` for uncached hashes. Used by the Stremio addon to prefer cached torrents. The `"real-debrid.com"` host key matches Real-Debrid response shape for compatibility only.

## Stremio / Torrentio flow

```
GET  /user
POST /torrents/addMagnet
GET  /torrents/info/{id}          (poll)
POST /torrents/selectFiles/{id}   (if waiting_files_selection)
GET  /torrents/info/{id}          (poll until downloaded)
POST /unrestrict/link             (host link → direct download URL)
```

## Error format

```json
{
  "error": "message",
  "error_code": 404
}
```

## Health

```
GET /healthz
```

Returns `200 ok` (no auth).
