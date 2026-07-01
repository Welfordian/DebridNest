CREATE TABLE IF NOT EXISTS torrents (
    id TEXT PRIMARY KEY,
    info_hash TEXT NOT NULL,
    magnet TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    original_name TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'magnet_conversion',
    progress INTEGER NOT NULL DEFAULT 0,
    bytes INTEGER NOT NULL DEFAULT 0,
    original_bytes INTEGER NOT NULL DEFAULT 0,
    info_bytes BLOB,
    added_at TEXT NOT NULL,
    ended_at TEXT,
    speed INTEGER NOT NULL DEFAULT 0,
    seeders INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_torrents_info_hash ON torrents(info_hash);
CREATE INDEX IF NOT EXISTS idx_torrents_added_at ON torrents(added_at DESC);

CREATE TABLE IF NOT EXISTS torrent_files (
    id INTEGER NOT NULL,
    torrent_id TEXT NOT NULL,
    path TEXT NOT NULL,
    bytes INTEGER NOT NULL DEFAULT 0,
    selected INTEGER NOT NULL DEFAULT 0,
    downloaded_bytes INTEGER NOT NULL DEFAULT 0,
    disk_path TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (torrent_id, id),
    FOREIGN KEY (torrent_id) REFERENCES torrents(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS host_links (
    id TEXT PRIMARY KEY,
    torrent_id TEXT NOT NULL,
    file_id INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (torrent_id) REFERENCES torrents(id) ON DELETE CASCADE,
    UNIQUE (torrent_id, file_id)
);

CREATE TABLE IF NOT EXISTS downloads (
    id TEXT PRIMARY KEY,
    torrent_id TEXT NOT NULL,
    file_id INTEGER NOT NULL,
    filename TEXT NOT NULL,
    mime_type TEXT NOT NULL DEFAULT '',
    filesize INTEGER NOT NULL DEFAULT 0,
    host_link TEXT NOT NULL,
    download_url TEXT NOT NULL,
    generated_at TEXT NOT NULL,
    FOREIGN KEY (torrent_id) REFERENCES torrents(id) ON DELETE CASCADE
);
