# Stremio setup

DebridNest Streams resolves movies and series from Stremio metadata through Jackett and your self-hosted DebridNest debrid API.

## Prerequisites

- Docker and Docker Compose
- DebridNest API token in `.env`
- Stremio desktop app (recommended for playback)

## 1. Start the stack

```bash
cp .env.example .env
# Set DEBRIDNEST_API_TOKEN and DEBRIDNEST_PUBLIC_URL
docker compose --profile stremio up -d --build
```

This starts **DebridNest**, **Jackett**, and the **Stremio addon**.

Verify DebridNest:

```bash
curl -H "Authorization: Bearer YOUR_TOKEN" http://localhost:8080/rest/1.0/user
```

Verify Jackett:

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:9117
```

## 2. Configure Jackett (first time)

On first `docker compose --profile stremio up`, a **jackett-setup** container automatically adds public indexers (`1337x`, `knaben`, `limetorrents`, `magnetz`, `nyaasi`, `rutracker-ru`, `thepiratebay`, `therarbg`, `yts`).

To customize:

```
JACKETT_INDEXERS=1337x,knaben,limetorrents,magnetz,nyaasi,rutracker-ru,thepiratebay,therarbg,yts
```

You can also add more indexers manually:

1. Open `http://localhost:9117`
2. **Indexers** → add additional indexers
3. Copy the API key (wrench icon) into `JACKETT_API_KEY` in `.env` only if auto-read fails

**Docker networking:** the Stremio addon talks to Jackett at `http://jackett:9117` inside the compose network. Do not use `localhost:9117` in addon config — that points at the addon container itself.

To use an external Prowlarr instance instead, set in `.env`:

```
JACKETT_URL=http://host.docker.internal:9696/1/api
JACKETT_API_KEY=your-prowlarr-key
```

## 2b. Prowlarr as Jackett-compatible indexer (optional)

Prowlarr exposes the same **Torznab** API Jackett uses, so the addon treats it as a drop-in replacement. No addon code changes are required — only the URL and API key differ.

### Docker Compose with external Prowlarr on the host

If Prowlarr runs on your Mac or NAS (default port **9696**):

```
JACKETT_URL=http://host.docker.internal:9696/1/api
JACKETT_API_KEY=your-prowlarr-api-key
```

Replace `1` with your Prowlarr **indexer ID** (Settings → Indexers → click an indexer → note the ID in the URL), or use the **All** indexers endpoint if your Prowlarr version supports it:

```
JACKETT_URL=http://host.docker.internal:9696/8/api
```

### Prowlarr-only (no Jackett container)

Skip the bundled Jackett service and point the addon at Prowlarr directly. On the configure page:

| Field | Value |
|-------|--------|
| Jackett/Prowlarr URL | `http://host.docker.internal:9696/1/api` (host) or your LAN IP |
| Jackett/Prowlarr API Key | Prowlarr → Settings → General → API Key |

**Networking:** from inside Docker, use `host.docker.internal` (Mac/Windows) or the host LAN IP — not `localhost:9696`, which refers to the addon container itself.

### Verify Prowlarr Torznab

```bash
curl -s "http://localhost:9696/1/api?t=caps&apikey=YOUR_KEY" | head -c 400
```

You should see Torznab capability XML. The addon diagnostics endpoint accepts the same URL:

```bash
curl -s 'http://127.0.0.1:7001/diagnostics?jackettUrl=http%3A%2F%2Fhost.docker.internal%3A9696%2F1%2Fapi&jackettApiKey=YOUR_KEY' | python3 -m json.tool
```

## 3. Install the Stremio addon

Configure page: `http://127.0.0.1:7001/configure`

### Stremio Desktop (recommended)

1. Fill in the fields (see below)
2. Click **INSTALL (Desktop)**

### Stremio Web

1. Fill in all fields on the configure page
2. Click **Copy manifest URL**
3. **Addons** → paste URL in the search bar → Install

**Note:** Stremio Web needs Stremio Desktop or Stremio Service running for playback (port 11470).

### Configuration fields (Docker defaults)

| Field | Value |
|-------|--------|
| DebridNest API URL | `http://debridnest:8080/rest/1.0` |
| DebridNest API Token | same as `DEBRIDNEST_API_TOKEN` in `.env` |
| Jackett URL | `http://jackett:9117` |
| Jackett API Key | from Jackett web UI |
| Max streams | `5` |
| Prefer SDR | enabled for Mac Stremio (optional) |
| Max resolution | `1080` or `Any` |
| Max file size (GB) | `15` or `0` for no limit |

### Quality filters (env vars or configure page)

Filter and rank torrents before resolving. Set server defaults in `.env` or override per install on the configure page.

| Variable | Configure field | Default | Description |
|----------|-----------------|---------|-------------|
| `PREFER_SDR` | Prefer SDR over HDR/DV | off | Rank SDR releases above HDR/Dolby Vision (helps Mac Stremio) |
| `MAX_RESOLUTION` | Max resolution | `0` (Any) | Cap resolution: `720`, `1080`, `2160`, or `0`/`Any` |
| `MAX_FILE_SIZE_GB` | Max file size (GB) | `0` | Drop torrents larger than this size (Torznab `size` in bytes) |

Example `.env` for 1080p SDR-friendly Mac playback:

```
PREFER_SDR=1
MAX_RESOLUTION=1080
MAX_FILE_SIZE_GB=15
```

Docker Compose passes these through the `stremio-addon` service `environment` block when set in `.env`.

## 4. Watch content

1. Use Stremio **Discover** (Cinemeta metadata is built-in)
2. Open a movie or series
3. Go to the **Streams** tab
4. Select a stream labeled `DebridNest 1080p BluRay (...)` etc.

Cached streams show a ⚡ label; uncached show ⏳ while DebridNest downloads. Stream descriptions include an **IINA:** link for macOS external playback — open it in a browser to launch [IINA](https://iina.io/) or copy the `iina://weblink?url=...` URL.

First play may take minutes while DebridNest downloads the torrent.

Stream listing is non-blocking by default: opening Stremio's Streams tab searches Jackett and returns placeholder streams without adding magnets to DebridNest. The selected stream starts the DebridNest download. Set `LIST_RESOLVE_COUNT` above `0` only if you intentionally want the addon to pre-resolve already-cached DebridNest items into direct URLs while listing.

Set `ADDON_BASE_URL` to a URL reachable by Stremio when using downloading placeholders remotely (e.g. `https://addon.example.com`).

## Local Node.js addon (without Docker Jackett)

```bash
cd stremio-addon
npm install
export DEBRIDNEST_API_URL=http://localhost:8080/rest/1.0
export DEBRIDNEST_API_TOKEN=YOUR_TOKEN
export JACKETT_URL=http://localhost:9117
export JACKETT_API_KEY=YOUR_JACKETT_KEY
export PREFER_SDR=1
export MAX_RESOLUTION=1080
export MAX_FILE_SIZE_GB=15
PORT=7001 npm start
```

Install: `http://127.0.0.1:7001/configure`

## Debug endpoints

```bash
# Full addon diagnostics (DebridNest + Jackett indexers)
curl -s 'http://127.0.0.1:7001/diagnostics?jackettUrl=http%3A%2F%2Fjackett%3A9117' | python3 -m json.tool

# DebridNest health
curl http://localhost:8080/healthz

# Jackett UI
open http://localhost:9117

# Addon health (DebridNest auth only)
curl "http://127.0.0.1:7001/health?apiUrl=http://localhost:8080/rest/1.0&apiToken=YOUR_TOKEN"

# Direct stream test (replace ENCODED_CONFIG from configure page)
curl -s "http://127.0.0.1:7001/ENCODED_CONFIG/stream/movie/tt0111161.json" | head -c 500

# IINA open helper (use streamId from stream response openInExternal URL)
open "http://127.0.0.1:7001/open/STREAM_ID?format=iina"
```

## Magnet test catalog (optional)

```bash
ENABLE_MAGNET_TEST=1 npm start
```

Adds a **Magnet Test** catalog for manual magnet URI testing.

## Remote access

If Stremio runs on another device, see [remote-access.md](remote-access.md) for HTTPS and tunnel setup. Both the addon URL and `DEBRIDNEST_PUBLIC_URL` must be reachable from the Stremio client.

## Troubleshooting

| Symptom | Check |
|---------|--------|
| No streams listed | Jackett has indexers; `JACKETT_API_KEY` in `.env`; addon uses `http://jackett:9117` not `localhost` |
| `bad_token` | Token matches `DEBRIDNEST_API_TOKEN` |
| Stream times out | Torrent has seeders; port 42069 open |
| Playback URL fails | `DEBRIDNEST_PUBLIC_URL` matches playback origin |
| Jackett empty results | Indexers configured and returning results in Jackett UI search |
