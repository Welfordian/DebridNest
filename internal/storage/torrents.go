package storage

import (
	"context"
	"database/sql"
	"time"
)

type TorrentRecord struct {
	ID            string
	InfoHash      string
	Magnet        string
	Name          string
	OriginalName  string
	Status        string
	Progress      int
	Bytes         int64
	OriginalBytes int64
	InfoBytes     []byte
	AddedAt       time.Time
	EndedAt       *time.Time
	Speed         int64
	Seeders       int
	Files         []TorrentFileRecord
	Links         []string
}

type TorrentFileRecord struct {
	ID               int
	TorrentID        string
	Path             string
	Bytes            int64
	Selected         bool
	DownloadedBytes  int64
	DiskPath         string
}

type DownloadRecord struct {
	ID          string
	TorrentID   string
	FileID      int
	Filename    string
	MimeType    string
	Filesize    int64
	HostLink    string
	DownloadURL string
	GeneratedAt time.Time
}

func (db *DB) CreateTorrent(ctx context.Context, rec TorrentRecord) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO torrents (id, info_hash, magnet, name, original_name, status, progress, bytes, original_bytes, info_bytes, added_at, speed, seeders)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.InfoHash, rec.Magnet, rec.Name, rec.OriginalName, rec.Status, rec.Progress, rec.Bytes, rec.OriginalBytes, rec.InfoBytes, rec.AddedAt.UTC().Format(time.RFC3339Nano), rec.Speed, rec.Seeders,
	)
	if err != nil {
		return err
	}

	for _, f := range rec.Files {
		selected := 0
		if f.Selected {
			selected = 1
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO torrent_files (id, torrent_id, path, bytes, selected, downloaded_bytes, disk_path)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			f.ID, rec.ID, f.Path, f.Bytes, selected, f.DownloadedBytes, f.DiskPath,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (db *DB) UpdateTorrent(ctx context.Context, rec TorrentRecord) error {
	var ended sql.NullString
	if rec.EndedAt != nil {
		ended = sql.NullString{String: rec.EndedAt.UTC().Format(time.RFC3339Nano), Valid: true}
	}

	_, err := db.ExecContext(ctx, `
		UPDATE torrents SET
			info_hash = ?, name = ?, original_name = ?, status = ?, progress = ?, bytes = ?, original_bytes = ?,
			info_bytes = ?, ended_at = ?, speed = ?, seeders = ?
		WHERE id = ?`,
		rec.InfoHash, rec.Name, rec.OriginalName, rec.Status, rec.Progress, rec.Bytes, rec.OriginalBytes,
		rec.InfoBytes, ended, rec.Speed, rec.Seeders, rec.ID,
	)
	return err
}

func (db *DB) UpdateTorrentFiles(ctx context.Context, torrentID string, files []TorrentFileRecord) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, f := range files {
		selected := 0
		if f.Selected {
			selected = 1
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE torrent_files SET selected = ?, downloaded_bytes = ?, disk_path = ?
			WHERE torrent_id = ? AND id = ?`,
			selected, f.DownloadedBytes, f.DiskPath, torrentID, f.ID,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (db *DB) GetTorrent(ctx context.Context, id string) (*TorrentRecord, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, info_hash, magnet, name, original_name, status, progress, bytes, original_bytes, info_bytes, added_at, ended_at, speed, seeders
		FROM torrents WHERE id = ?`, id)

	rec, err := scanTorrent(row)
	if err != nil {
		return nil, err
	}

	files, err := db.listTorrentFiles(ctx, id)
	if err != nil {
		return nil, err
	}
	rec.Files = files

	links, err := db.listHostLinks(ctx, id)
	if err != nil {
		return nil, err
	}
	rec.Links = links
	return rec, nil
}

func (db *DB) GetTorrentByHash(ctx context.Context, infoHash string) (*TorrentRecord, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, info_hash, magnet, name, original_name, status, progress, bytes, original_bytes, info_bytes, added_at, ended_at, speed, seeders
		FROM torrents WHERE info_hash = ? ORDER BY added_at DESC LIMIT 1`, infoHash)

	rec, err := scanTorrent(row)
	if err != nil {
		return nil, err
	}

	files, err := db.listTorrentFiles(ctx, rec.ID)
	if err != nil {
		return nil, err
	}
	rec.Files = files

	links, err := db.listHostLinks(ctx, rec.ID)
	if err != nil {
		return nil, err
	}
	rec.Links = links
	return rec, nil
}

func (db *DB) ListTorrents(ctx context.Context, limit int) ([]TorrentRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, info_hash, magnet, name, original_name, status, progress, bytes, original_bytes, info_bytes, added_at, ended_at, speed, seeders
		FROM torrents ORDER BY added_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TorrentRecord
	for rows.Next() {
		rec, err := scanTorrentRows(rows)
		if err != nil {
			return nil, err
		}
		links, err := db.listHostLinks(ctx, rec.ID)
		if err != nil {
			return nil, err
		}
		rec.Links = links
		out = append(out, *rec)
	}
	return out, rows.Err()
}

func (db *DB) DeleteTorrent(ctx context.Context, id string) error {
	_, err := db.ExecContext(ctx, `DELETE FROM torrents WHERE id = ?`, id)
	return err
}

func (db *DB) UpsertHostLink(ctx context.Context, id, torrentID string, fileID int, createdAt time.Time) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO host_links (id, torrent_id, file_id, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(torrent_id, file_id) DO UPDATE SET id = excluded.id, created_at = excluded.created_at`,
		id, torrentID, fileID, createdAt.UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (db *DB) GetHostLink(ctx context.Context, linkID string) (torrentID string, fileID int, err error) {
	err = db.QueryRowContext(ctx, `SELECT torrent_id, file_id FROM host_links WHERE id = ?`, linkID).Scan(&torrentID, &fileID)
	return
}

func (db *DB) GetTorrentFileByDiskPath(ctx context.Context, diskPath string) (TorrentFileRecord, error) {
	var f TorrentFileRecord
	var selected int
	err := db.QueryRowContext(ctx, `
		SELECT id, torrent_id, path, bytes, selected, downloaded_bytes, disk_path
		FROM torrent_files WHERE disk_path = ?`, diskPath).Scan(
		&f.ID, &f.TorrentID, &f.Path, &f.Bytes, &selected, &f.DownloadedBytes, &f.DiskPath,
	)
	if err != nil {
		return TorrentFileRecord{}, err
	}
	f.Selected = selected == 1
	return f, nil
}

func (db *DB) GetTorrentFileByBasename(ctx context.Context, basename string) (TorrentFileRecord, error) {
	var f TorrentFileRecord
	var selected int
	err := db.QueryRowContext(ctx, `
		SELECT id, torrent_id, path, bytes, selected, downloaded_bytes, disk_path
		FROM torrent_files
		WHERE disk_path LIKE ? OR path LIKE ?
		ORDER BY downloaded_bytes DESC
		LIMIT 1`, "%/"+basename, "%/"+basename).Scan(
		&f.ID, &f.TorrentID, &f.Path, &f.Bytes, &selected, &f.DownloadedBytes, &f.DiskPath,
	)
	if err != nil {
		return TorrentFileRecord{}, err
	}
	f.Selected = selected == 1
	return f, nil
}

func (db *DB) GetHostLinkByTorrentFile(ctx context.Context, torrentID string, fileID int) (string, error) {
	var id string
	err := db.QueryRowContext(ctx, `SELECT id FROM host_links WHERE torrent_id = ? AND file_id = ?`, torrentID, fileID).Scan(&id)
	return id, err
}

func (db *DB) SaveDownload(ctx context.Context, rec DownloadRecord) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO downloads (id, torrent_id, file_id, filename, mime_type, filesize, host_link, download_url, generated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.TorrentID, rec.FileID, rec.Filename, rec.MimeType, rec.Filesize, rec.HostLink, rec.DownloadURL, rec.GeneratedAt.UTC().Format(time.RFC3339Nano),
	)
	return err
}

func (db *DB) ListDownloads(ctx context.Context, limit int) ([]DownloadRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, torrent_id, file_id, filename, mime_type, filesize, host_link, download_url, generated_at
		FROM downloads ORDER BY generated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DownloadRecord
	for rows.Next() {
		var rec DownloadRecord
		var generated string
		if err := rows.Scan(&rec.ID, &rec.TorrentID, &rec.FileID, &rec.Filename, &rec.MimeType, &rec.Filesize, &rec.HostLink, &rec.DownloadURL, &generated); err != nil {
			return nil, err
		}
		rec.GeneratedAt, _ = time.Parse(time.RFC3339Nano, generated)
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (db *DB) listTorrentFiles(ctx context.Context, torrentID string) ([]TorrentFileRecord, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, torrent_id, path, bytes, selected, downloaded_bytes, disk_path
		FROM torrent_files WHERE torrent_id = ? ORDER BY id ASC`, torrentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []TorrentFileRecord
	for rows.Next() {
		var f TorrentFileRecord
		var selected int
		if err := rows.Scan(&f.ID, &f.TorrentID, &f.Path, &f.Bytes, &selected, &f.DownloadedBytes, &f.DiskPath); err != nil {
			return nil, err
		}
		f.Selected = selected == 1
		files = append(files, f)
	}
	return files, rows.Err()
}

func (db *DB) listHostLinks(ctx context.Context, torrentID string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT hl.id FROM host_links hl
		JOIN torrent_files tf ON tf.torrent_id = hl.torrent_id AND tf.id = hl.file_id
		WHERE hl.torrent_id = ? AND tf.selected = 1
		ORDER BY tf.id ASC`, torrentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func scanTorrent(row *sql.Row) (*TorrentRecord, error) {
	var rec TorrentRecord
	var added, ended sql.NullString
	var infoBytes []byte
	if err := row.Scan(
		&rec.ID, &rec.InfoHash, &rec.Magnet, &rec.Name, &rec.OriginalName, &rec.Status, &rec.Progress,
		&rec.Bytes, &rec.OriginalBytes, &infoBytes, &added, &ended, &rec.Speed, &rec.Seeders,
	); err != nil {
		return nil, err
	}
	rec.InfoBytes = infoBytes
	rec.AddedAt, _ = time.Parse(time.RFC3339Nano, added.String)
	if ended.Valid {
		t, _ := time.Parse(time.RFC3339Nano, ended.String)
		rec.EndedAt = &t
	}
	return &rec, nil
}

func scanTorrentRows(rows *sql.Rows) (*TorrentRecord, error) {
	var rec TorrentRecord
	var added, ended sql.NullString
	var infoBytes []byte
	if err := rows.Scan(
		&rec.ID, &rec.InfoHash, &rec.Magnet, &rec.Name, &rec.OriginalName, &rec.Status, &rec.Progress,
		&rec.Bytes, &rec.OriginalBytes, &infoBytes, &added, &ended, &rec.Speed, &rec.Seeders,
	); err != nil {
		return nil, err
	}
	rec.InfoBytes = infoBytes
	rec.AddedAt, _ = time.Parse(time.RFC3339Nano, added.String)
	if ended.Valid {
		t, _ := time.Parse(time.RFC3339Nano, ended.String)
		rec.EndedAt = &t
	}
	return &rec, nil
}

func (db *DB) ListIncompleteTorrents(ctx context.Context) ([]TorrentRecord, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, info_hash, magnet, name, original_name, status, progress, bytes, original_bytes, info_bytes, added_at, ended_at, speed, seeders
		FROM torrents WHERE status NOT IN ('downloaded', 'error', 'dead')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TorrentRecord
	for rows.Next() {
		rec, err := scanTorrentRows(rows)
		if err != nil {
			return nil, err
		}
		files, err := db.listTorrentFiles(ctx, rec.ID)
		if err != nil {
			return nil, err
		}
		rec.Files = files
		out = append(out, *rec)
	}
	return out, rows.Err()
}

func (db *DB) ListCompletedBefore(ctx context.Context, before time.Time) ([]TorrentRecord, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, info_hash, magnet, name, original_name, status, progress, bytes, original_bytes, info_bytes, added_at, ended_at, speed, seeders
		FROM torrents
		WHERE status = 'downloaded' AND ended_at IS NOT NULL AND ended_at < ?
		ORDER BY ended_at ASC`, before.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TorrentRecord
	for rows.Next() {
		rec, err := scanTorrentRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rec)
	}
	return out, rows.Err()
}

func (db *DB) ListCompletedByEndedAt(ctx context.Context, limit int) ([]TorrentRecord, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, info_hash, magnet, name, original_name, status, progress, bytes, original_bytes, info_bytes, added_at, ended_at, speed, seeders
		FROM torrents
		WHERE status = 'downloaded' AND ended_at IS NOT NULL
		ORDER BY ended_at ASC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TorrentRecord
	for rows.Next() {
		rec, err := scanTorrentRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rec)
	}
	return out, rows.Err()
}

func (db *DB) CountTorrents(ctx context.Context) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM torrents`).Scan(&n)
	return n, err
}

func (db *DB) CountActiveTorrents(ctx context.Context) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM torrents WHERE status NOT IN ('downloaded', 'error', 'dead')`).Scan(&n)
	return n, err
}

func (db *DB) ResetTorrentForRetry(ctx context.Context, id string) error {
	_, err := db.ExecContext(ctx, `
		UPDATE torrents SET status = 'queued', progress = 0, ended_at = NULL, speed = 0
		WHERE id = ?`, id)
	return err
}
