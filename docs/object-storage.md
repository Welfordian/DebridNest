# Object storage (S3-compatible)

DebridNest can offload completed torrent files to S3-compatible object storage (AWS S3, Cloudflare R2, Backblaze B2, MinIO, etc.). This is **opt-in and disabled by default**.

## How it works (staging model)

BitTorrent downloads always use **local disk** under `DEBRIDNEST_DATA_DIR/files`. The anacrolix engine requires local staging while a torrent is active.

When a torrent reaches `downloaded`:

1. Selected files are uploaded to your bucket.
2. The database records `object_key` and `remote_stored=1` for each file.
3. If **offload local** is enabled, the local copy is deleted after a successful upload.
4. Streaming and signed `/dl/*` links serve from object storage via HTTP Range when the local file is gone.

Active downloads and in-progress pieces always remain on local staging disk.

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `DEBRIDNEST_S3_ENABLED` | `0` | Set to `1` to enable object storage |
| `DEBRIDNEST_S3_ENDPOINT` | *(empty)* | Custom endpoint URL (required for R2, B2, MinIO) |
| `DEBRIDNEST_S3_BUCKET` | *(empty)* | Bucket name (required when enabled) |
| `DEBRIDNEST_S3_REGION` | `auto` | AWS region or `auto` for R2 |
| `DEBRIDNEST_S3_ACCESS_KEY` | *(empty)* | Access key ID |
| `DEBRIDNEST_S3_SECRET_KEY` | *(empty)* | Secret access key |
| `DEBRIDNEST_S3_PREFIX` | *(empty)* | Optional key prefix (e.g. `debridnest`) |
| `DEBRIDNEST_S3_FORCE_PATH_STYLE` | `0` | Set to `1` for path-style URLs (R2, MinIO) |
| `DEBRIDNEST_S3_OFFLOAD_LOCAL` | `0` | Set to `1` to delete local files after upload |

Example `.env` block:

```bash
DEBRIDNEST_S3_ENABLED=1
DEBRIDNEST_S3_ENDPOINT=https://<account-id>.r2.cloudflarestorage.com
DEBRIDNEST_S3_BUCKET=debridnest-media
DEBRIDNEST_S3_REGION=auto
DEBRIDNEST_S3_ACCESS_KEY=...
DEBRIDNEST_S3_SECRET_KEY=...
DEBRIDNEST_S3_PREFIX=debridnest
DEBRIDNEST_S3_FORCE_PATH_STYLE=1
DEBRIDNEST_S3_OFFLOAD_LOCAL=1
```

Runtime overrides are available in the dashboard **Settings → Object storage (S3)** section (admin only). Patched values take precedence over environment defaults.

## Provider setup

### Cloudflare R2

1. Create an R2 bucket in the Cloudflare dashboard.
2. Create an API token with Object Read & Write on that bucket.
3. Set `DEBRIDNEST_S3_ENDPOINT` to `https://<account-id>.r2.cloudflarestorage.com`.
4. Set `DEBRIDNEST_S3_FORCE_PATH_STYLE=1` and `DEBRIDNEST_S3_REGION=auto`.

### Backblaze B2

1. Create a bucket and application key with read/write access.
2. Set `DEBRIDNEST_S3_ENDPOINT` to the S3-compatible endpoint shown in B2 (e.g. `https://s3.<region>.backblazeb2.com`).
3. Use the key ID and application key as access/secret.
4. Set `DEBRIDNEST_S3_FORCE_PATH_STYLE=1`.

### AWS S3

1. Create a bucket and IAM user with `s3:PutObject`, `s3:GetObject`, `s3:DeleteObject`, `s3:ListBucket`.
2. Leave `DEBRIDNEST_S3_ENDPOINT` empty to use the default AWS endpoint.
3. Set `DEBRIDNEST_S3_REGION` to your bucket region.
4. Path-style is usually not required (`DEBRIDNEST_S3_FORCE_PATH_STYLE=0`).

## Dashboard

Admins can configure S3 from **Settings → Object storage (S3)**:

- Enable/disable, endpoint, bucket, region, prefix
- Access key and secret key (secret is redacted on GET for non-admins)
- Force path style and offload local toggles
- **Test connection** — calls `POST /api/v1/settings/s3-test` (HeadBucket)

## Force path style

Some providers (R2, B2, MinIO) require path-style URLs (`https://endpoint/bucket/key`) instead of virtual-hosted style (`https://bucket.endpoint/key`). Enable `DEBRIDNEST_S3_FORCE_PATH_STYLE=1` when connecting to these services.

## Offload local

When `DEBRIDNEST_S3_OFFLOAD_LOCAL=1` (or the dashboard toggle is on), DebridNest removes each local file after it is successfully uploaded. Disk quota pressure is reduced, but the object store becomes the sole copy for that file.

If upload fails, the local file is kept and `remote_stored` stays unset.

## Limitations

- **Active downloads** always use local staging disk; object storage is only used after completion (and optional local deletion).
- **WebDAV** library browsing serves from local disk only; remotely stored files without a local copy are not listed via WebDAV today.
- Deleting a torrent removes corresponding objects from the bucket when `remote_stored=1`.

## Admin API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/settings` | Includes S3 fields (secrets redacted for non-admin) |
| PATCH | `/api/v1/settings` | Patch S3 fields (`s3Enabled`, `s3Bucket`, …) |
| POST | `/api/v1/settings/s3-test` | Test bucket connectivity (admin, requires S3 enabled) |

Returns `503` when S3 is disabled, `400` when bucket is missing, `502` when the provider rejects the connection.
