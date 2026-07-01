# Remote access

DebridNest and the Stremio addon must be reachable from the devices running Stremio. Download URLs embedded in streams use `DEBRIDNEST_PUBLIC_URL` — set this to the origin clients use to fetch `/dl/...` links.

## Environment

| Variable | Purpose |
|----------|---------|
| `DEBRIDNEST_PUBLIC_URL` | Public URL for host/download links (e.g. `https://debridnest.example.com`) |
| `DEBRIDNEST_DOMAIN` | Domain for Caddy TLS profile |
| `CLOUDFLARE_TUNNEL_TOKEN` | Token from Cloudflare Zero Trust dashboard |

## Option 1 — Direct port (LAN / VPS)

```bash
docker compose up -d
```

- API: `http://your-server:8080`
- Open TCP **42069** for BitTorrent peers
- Set `DEBRIDNEST_PUBLIC_URL=http://your-server:8080` (or HTTPS if fronted)

## Option 2 — Caddy with automatic HTTPS

For a VPS with a public domain:

```bash
# .env
DEBRIDNEST_DOMAIN=debridnest.example.com
DEBRIDNEST_PUBLIC_URL=https://debridnest.example.com

docker compose --profile tls up -d
```

Caddy obtains Let's Encrypt certificates and reverse-proxies to DebridNest on port 443.

**Note:** BitTorrent peer port 42069 must still be reachable on the host firewall for downloads to complete.

## Option 3 — Cloudflare Tunnel (no open ports)

For home servers without port forwarding:

1. Create a tunnel in [Cloudflare Zero Trust](https://one.dash.cloudflare.com/)
2. Add a public hostname pointing to `http://debridnest:8080`
3. Set `CLOUDFLARE_TUNNEL_TOKEN` in `.env`
4. Set `DEBRIDNEST_PUBLIC_URL` to your tunnel URL (e.g. `https://debridnest.example.com`)

```bash
docker compose --profile tunnel up -d
```

**Limitations:**

- Tunnel handles HTTP API and download traffic
- BitTorrent peers still need outbound connectivity; inbound peer port may be limited on restrictive networks
- Stremio addon must also be reachable (host locally, tunnel separately, or use `--profile stremio` on a machine Stremio can reach)

## Stremio remote checklist

1. `DEBRIDNEST_PUBLIC_URL` matches the URL Stremio uses for playback
2. DebridNest API token configured in the Stremio addon
3. Jackett/Prowlarr reachable from the addon server
4. If Stremio runs on another device, install the addon from a URL that device can reach (not `127.0.0.1` unless Stremio is on the same machine)

## All-in-one compose profiles

```bash
# DebridNest + Stremio addon
docker compose --profile stremio up -d

# DebridNest + Caddy TLS
docker compose --profile tls up -d

# DebridNest + Cloudflare Tunnel
docker compose --profile tunnel up -d
```
