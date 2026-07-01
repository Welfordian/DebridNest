# WebDAV library access

DebridNest exposes completed torrent files over read-only WebDAV at `/webdav/`. Use it with Infuse, Kodi, rclone, or any WebDAV client to browse and stream your local debrid library.

## Enable and auth

WebDAV is **enabled by default**. Set `DEBRIDNEST_WEBDAV_ENABLED=0` to disable.

| Variable | Default | Description |
|----------|---------|-------------|
| `DEBRIDNEST_WEBDAV_ENABLED` | `1` | Set to `0` to disable WebDAV |
| `DEBRIDNEST_WEBDAV_USER` | *(unset)* | Basic auth username |
| `DEBRIDNEST_WEBDAV_PASSWORD` | *(unset)* | Basic auth password |

When `DEBRIDNEST_WEBDAV_USER` and `DEBRIDNEST_WEBDAV_PASSWORD` are both set, those credentials are used. Otherwise:

- **Username:** `debridnest`
- **Password:** `DEBRIDNEST_API_TOKEN`

## Layout

Completed torrents appear as top-level folders named after the torrent. Duplicate names get a numeric suffix (` (2)`, ` (3)`, …). Selected files keep their in-torrent path:

```
/webdav/
  Inception (2010)/
    Inception (2010).mkv
  Breaking Bad S01/
    Breaking Bad S01E01.mkv
    Breaking Bad S01E02.mkv
  Some Pack/
    Season 1/
      episode.mkv
```

Only torrents with `status: downloaded` and selected files are listed.

## Supported methods

Read-only: `GET`, `HEAD`, `OPTIONS`, `PROPFIND`. Upload, delete, and move return `405 Method Not Allowed`.

## Infuse (iOS / tvOS / macOS)

1. Open **Settings → Add Share… → WebDAV**
2. **Address:** `https://your-host/webdav/` (include trailing slash)
3. **Username / password:** as configured above
4. Browse torrent folders and play files directly

For local testing: `http://localhost:8080/webdav/` with user `debridnest` and your API token.

## rclone

Create a remote:

```bash
rclone config create debridnest webdav \
  url http://localhost:8080/webdav/ \
  vendor other \
  user debridnest \
  pass "$(rclone obscure "$DEBRIDNEST_API_TOKEN")"
```

List and copy:

```bash
rclone lsd debridnest:
rclone ls debridnest:"Inception (2010)"
rclone copy debridnest:"Inception (2010)/Inception (2010).mkv" /tmp/
```

Use `mount` for a local FUSE view (read-only on the DebridNest side):

```bash
rclone mount debridnest: ~/debridnest --read-only
```

Replace `http://localhost:8080` with your public `DEBRIDNEST_PUBLIC_URL` when accessing remotely.

## Kodi

1. **Settings → Services → UPnP/DLNA** is separate; for WebDAV use a file source or add-on that supports WebDAV shares.
2. Add network source: `https://user:pass@your-host/webdav/`
3. Set content type to Movies or TV shows and scan the library.

## curl sanity check

```bash
curl -u debridnest:"$DEBRIDNEST_API_TOKEN" -X PROPFIND \
  -H "Depth: 1" \
  http://localhost:8080/webdav/
```
