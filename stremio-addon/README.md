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

## Debug

- `GET /health` — verify DebridNest API auth
- `GET /resolve?magnet=...` — direct magnet debrid test
- `ENABLE_MAGNET_TEST=1` — enable Magnet Test catalog
