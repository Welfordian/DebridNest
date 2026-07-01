# Media server library (Plex / Jellyfin)

After Sonarr or Radarr imports completed downloads, point Plex or Jellyfin at your DebridNest library via **WebDAV**.

DebridNest serves completed torrent files read-only at `/webdav/`. See [webdav.md](webdav.md) for full WebDAV setup, authentication, and client examples.

## Quick setup

1. Ensure WebDAV is enabled (default): `DEBRIDNEST_WEBDAV_ENABLED=1`
2. Note credentials from [webdav.md](webdav.md):
   - Username: `debridnest` (or `DEBRIDNEST_WEBDAV_USER`)
   - Password: `DEBRIDNEST_API_TOKEN` (or `DEBRIDNEST_WEBDAV_PASSWORD`)
3. WebDAV URL: `https://your-host/webdav/`

Completed torrents appear as folders with their selected files inside.

## Plex

### Direct WebDAV (Plex Pass)

Plex can add a WebDAV library source when you have Plex Pass:

1. **Settings → Manage → Libraries → Add Library**
2. Choose **Movies** or **TV Shows**
3. Add folder: `https://debridnest:TOKEN@your-host/webdav/Inception (2010)/`
4. Or mount with rclone (below) and point Plex at the local mount

### rclone mount (recommended)

Mount the WebDAV share locally and add the mount path as a Plex library:

```bash
rclone config create debridnest webdav \
  url https://your-host/webdav/ \
  vendor other \
  user debridnest \
  pass "$(rclone obscure "$DEBRIDNEST_API_TOKEN")"

rclone mount debridnest: ~/debridnest --read-only --vfs-cache-mode full &
```

In Plex, add `~/debridnest` as a library folder.

## Jellyfin

Jellyfin supports WebDAV via plugins or rclone mount:

### rclone mount

Same rclone setup as above. In Jellyfin:

1. **Dashboard → Libraries → Add Media Library**
2. Content type: Movies or Shows
3. Folder: `/path/to/debridnest/mount`
4. Scan library

### WebDAV plugin

Install a WebDAV plugin from the Jellyfin plugin catalog if available for your version, then configure:

- URL: `https://your-host/webdav/`
- Username / password: as in [webdav.md](webdav.md)

## Typical workflow

```
Sonarr/Radarr  →  DebridNest (qBit API)  →  download completes
       ↓
   import/copy to library path (optional if using same storage)
       ↓
Plex/Jellyfin  →  WebDAV or rclone mount  →  stream
```

When Sonarr/Radarr and DebridNest share the same `data/files` volume, you can point Plex/Jellyfin directly at that path instead of WebDAV. WebDAV is useful when the media server runs on a different host.

## Related docs

- [webdav.md](webdav.md) — WebDAV auth, layout, Infuse, Kodi, rclone
- [arr-setup.md](arr-setup.md) — Sonarr/Radarr download client setup
