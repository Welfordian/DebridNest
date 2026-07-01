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

On first `docker compose --profile stremio up`, a **jackett-setup** container automatically adds public indexers (`limetorrents`, `therarbg`, `eztv`, `knaben`, `magnetz`).

To customize:

```
JACKETT_INDEXERS=limetorrents,therarbg,eztv
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

## 4. Watch content

1. Use Stremio **Discover** (Cinemeta metadata is built-in)
2. Open a movie or series
3. Go to the **Streams** tab
4. Select a stream labeled `DebridNest 1080p BluRay (...)` etc.

First play may take minutes while DebridNest downloads the torrent. Cached torrents show with a ⚡ label and start immediately. Uncached streams show ⏳ (downloading…) — select them and Stremio will poll until ready.

Set `ADDON_BASE_URL` to a URL reachable by Stremio when using downloading placeholders remotely (e.g. `https://addon.example.com`).

## Local Node.js addon (without Docker Jackett)

```bash
cd stremio-addon
npm install
export DEBRIDNEST_API_URL=http://localhost:8080/rest/1.0
export DEBRIDNEST_API_TOKEN=YOUR_TOKEN
export JACKETT_URL=http://localhost:9117
export JACKETT_API_KEY=YOUR_JACKETT_KEY
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
