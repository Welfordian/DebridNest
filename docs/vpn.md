# VPN sidecar (optional)

Route DebridNest torrent traffic through a VPN using [Gluetun](https://github.com/qdm12/gluetun). HTTP API and download URLs stay reachable on the host ports published by Gluetun; BitTorrent peer traffic exits via the VPN tunnel.

## Quick start

1. Add VPN credentials to `.env` (see below).
2. Place an OpenVPN config at `deploy/vpn/custom.conf`, or set WireGuard variables.
3. Stop the standalone DebridNest container if it is already running:

```bash
docker compose stop debridnest
```

4. Start the VPN stack:

```bash
docker compose --profile vpn up -d gluetun debridnest-vpn
```

5. Verify:

```bash
curl http://localhost:8080/healthz
docker compose --profile vpn logs gluetun | tail
```

## Environment variables

| Variable | Description |
|----------|-------------|
| `VPN_SERVICE_PROVIDER` | Gluetun provider name (`custom`, `mullvad`, `nordvpn`, etc.) |
| `VPN_TYPE` | `openvpn` or `wireguard` |
| `OPENVPN_USER` / `OPENVPN_PASSWORD` | Provider credentials (OpenVPN) |
| `OPENVPN_CONFIG_DIR` | Host directory mounted at `/gluetun` (default `./deploy/vpn`) |
| `WIREGUARD_PRIVATE_KEY` | WireGuard private key |
| `WIREGUARD_ADDRESSES` | WireGuard interface address (e.g. `10.64.222.21/32`) |
| `WIREGUARD_PUBLIC_KEY` | Peer public key |
| `WIREGUARD_ENDPOINT_IP` | Peer endpoint IP or hostname |
| `FIREWALL_OUTBOUND_SUBNETS` | Comma-separated LAN CIDRs allowed outside the tunnel (e.g. `192.168.1.0/24`) |

See [Gluetun wiki](https://github.com/qdm12/gluetun-wiki) for provider-specific settings.

## OpenVPN (custom)

```bash
mkdir -p deploy/vpn
cp /path/to/provider.ovpn deploy/vpn/custom.conf
```

Set in `.env`:

```env
VPN_SERVICE_PROVIDER=custom
VPN_TYPE=openvpn
OPENVPN_USER=your-user
OPENVPN_PASSWORD=your-password
```

## WireGuard

```env
VPN_SERVICE_PROVIDER=custom
VPN_TYPE=wireguard
WIREGUARD_PRIVATE_KEY=...
WIREGUARD_ADDRESSES=10.x.x.x/32
WIREGUARD_PUBLIC_KEY=...
WIREGUARD_ENDPOINT_IP=...
```

## Stremio + VPN

When using the Stremio profile with VPN, point the addon at the VPN-backed API:

```bash
docker compose stop debridnest
docker compose --profile vpn --profile stremio up -d gluetun debridnest-vpn jackett jackett-setup stremio-addon
```

Update `stremio-addon` to use `http://debridnest-vpn:8080/rest/1.0` only works when both share Gluetun's network namespace. For the default compose layout, keep `DEBRIDNEST_API_URL=http://debridnest:8080/rest/1.0` and run Stremio without VPN, or run a custom override so the addon shares the VPN network.

## Notes

- `debridnest-vpn` uses `network_mode: service:gluetun`; do not run `debridnest` and `debridnest-vpn` on the same host port at once.
- Gluetun publishes ports `8080` and `42069` (TCP/UDP) for the API and torrent client.
- Torrent traffic is forced through the VPN; allow LAN subnets via `FIREWALL_OUTBOUND_SUBNETS` if Jackett runs on the host network.
