# DebridNest Stremio Addon

Stremio addon that searches Jackett/Prowlarr and resolves streams through your self-hosted DebridNest RealDebrid-compatible API.

## Requirements

- Node.js 18+
- Running DebridNest API
- Jackett or Prowlarr with indexers

## Setup

```bash
npm install
export DEBRIDNEST_API_URL=http://localhost:8080/rest/1.0
export DEBRIDNEST_API_TOKEN=your-token
export JACKETT_URL=http://localhost:9117
export JACKETT_API_KEY=your-jackett-key
npm start
```

Install in Stremio: `http://127.0.0.1:7001/manifest.json`

See [../docs/stremio-setup.md](../docs/stremio-setup.md) for full instructions.

## Docker

```bash
docker compose --profile stremio up -d
```

By default, stream listing returns placeholders and starts the top placeholder in DebridNest in the background. Set `PREWARM_COUNT=0` to disable listing-time startup, or raise it carefully to prewarm more returned placeholders. Set `LIST_RESOLVE_COUNT` above `0` only to pre-resolve already-cached DebridNest items into direct URLs while listing. `PROGRESS_POLL_MS` and `PLAY_POLL_MS` default to `1000`; `PLAY_WAIT_MS` defaults to `30000`.

## Debug

- `GET /health` — verify DebridNest API auth
- `GET /resolve?magnet=...` — direct magnet debrid test
- `ENABLE_MAGNET_TEST=1` — enable Magnet Test catalog
