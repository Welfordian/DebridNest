# Object storage (S3-compatible)

DebridNest can offload torrent files to S3-compatible object storage (AWS S3, Cloudflare R2, Backblaze B2, MinIO, etc.). This is **opt-in and disabled by default**.

## How it works

BitTorrent downloads use the anacrolix engine, which writes piece data through normal filesystem calls (`WriteAt` on files under a configurable **files directory**). Object storage is not a POSIX filesystem, so DebridNest uses a **staging + upload** model rather than writing pieces directly to S3 APIs.

### Default staging model

1. Pieces are written locally under `{DATA_DIR}/files/{infohash}/…` while a torrent is active.
2. When a file finishes (or the whole torrent reaches `downloaded`), it is uploaded to your bucket.
3. The database records `object_key` and `remote_stored=1` for each file.
4. If **offload local** is enabled, the local copy is deleted after a successful upload.
5. Streaming and signed `/dl/*` links serve from object storage via HTTP Range when the local file is gone.

### Minimal-local mode (`DEBRIDNEST_S3_DIRECT=1`)

For deployments where VPS disk is scarce, enable **direct mode**. This turns on **early offload**: each selected file is uploaded (and optionally deleted locally) as soon as that file finishes downloading, instead of waiting for the entire torrent to complete. Peak local usage is roughly **one in-progress file** plus small metadata, not the full torrent size.

Recommended `.env` block for a small VPS with Backblaze B2:

```bash
DEBRIDNEST_S3_ENABLED=1
DEBRIDNEST_S3_ENDPOINT=https://s3.us-west-004.backblazeb2.com
DEBRIDNEST_S3_BUCKET=your-bucket
DEBRIDNEST_S3_REGION=us-west-004
DEBRIDNEST_S3_ACCESS_KEY=your-key-id
DEBRIDNEST_S3_SECRET_KEY=your-app-key
DEBRIDNEST_S3_PREFIX=debridnest
DEBRIDNEST_S3_FORCE_PATH_STYLE=1
DEBRIDNEST_S3_DIRECT=1
```

`DEBRIDNEST_S3_DIRECT=1` implies:

- `DEBRIDNEST_S3_EARLY_OFFLOAD=1` — upload per file as each completes
- `DEBRIDNEST_S3_OFFLOAD_LOCAL=1` — delete local copy after upload (unless you explicitly set `DEBRIDNEST_S3_OFFLOAD_LOCAL=0`)

You can enable early offload without direct mode by setting `DEBRIDNEST_S3_EARLY_OFFLOAD=1` alone.

## Why not mount S3 as a drive?

Tools like **rclone mount**, **s3fs**, and **mountpoint-s3** expose S3 buckets as POSIX paths. In theory DebridNest could point `filesDir` at such a mount. In practice this is **not recommended** for BitTorrent:

| Approach | Problem |
|----------|---------|
| **FUSE mount (rclone, s3fs)** | BitTorrent uses random writes across large sparse files. S3 is object storage; FUSE layers buffer or cache locally anyway, or perform poorly / unreliably on random `WriteAt`. |
| **mountpoint-s3** | Optimized for sequential read/write workloads, not random piece assembly. |
| **Custom anacrolix storage backend** | Would require implementing `storage.ClientImpl` with S3 multipart + piece completion tracking — large effort, still needs local piece-index state. |

**True zero-local torrent download is not practical** with anacrolix today. The supported path is: short local staging → upload → delete local copy.

### Advanced: custom files directory

If you still want to experiment with a FUSE mount, set:

```bash
DEBRIDNEST_FILES_DIR=/mnt/s3/debridnest-files
```

Mount your bucket at that path **before** starting DebridNest. Expect slower downloads, possible corruption or stalls, and hidden local cache usage depending on the mount tool. The default `{DATA_DIR}/files` on local disk (with `S3_DIRECT=1`) is the supported configuration.

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
| `DEBRIDNEST_S3_FORCE_PATH_STYLE` | `0` | Set to `1` for path-style URLs (R2, B2, MinIO) |
| `DEBRIDNEST_S3_OFFLOAD_LOCAL` | `0` | Set to `1` to delete local files after upload |
| `DEBRIDNEST_S3_EARLY_OFFLOAD` | `0` | Set to `1` to upload each file as soon as it completes |
| `DEBRIDNEST_S3_DIRECT` | `0` | Minimal-local preset (early offload + offload local) |
| `DEBRIDNEST_FILES_DIR` | `{DATA_DIR}/files` | Override torrent files directory |

Example `.env` block (standard post-download offload):

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

Runtime overrides are available in the dashboard **Settings → Object storage (S3)** section (admin only). Patched values take precedence over environment defaults for bucket credentials and offload-local. Early/direct mode is controlled by environment variables only.

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

With early offload enabled, streaming continues to work for offloaded files: `/dl/*` and in-progress playback read from object storage once `remote_stored=1`.

## Limitations

- **Active piece assembly** always uses the files directory; object storage receives uploads after file bytes are complete (or at torrent completion without early offload).
- **Not zero-local** — expect temporary disk use for the currently downloading file(s) and torrent metadata under `{DATA_DIR}/torrent`.
- **WebDAV** library browsing serves from local disk only; remotely stored files without a local copy are not listed via WebDAV today.
- **Seeding** requires local files; do not enable offload local if you rely on post-download seeding (`DEBRIDNEST_SEED_AFTER_COMPLETE=1`).
- Deleting a torrent removes corresponding objects from the bucket when `remote_stored=1`.

## Admin API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/settings` | Includes S3 fields (secrets redacted for non-admin) |
| PATCH | `/api/v1/settings` | Patch S3 fields (`s3Enabled`, `s3Bucket`, …) |
| POST | `/api/v1/settings/s3-test` | Test bucket connectivity (admin, requires S3 enabled) |

Returns `503` when S3 is disabled, `400` when bucket is missing, `502` when the provider rejects the connection.
