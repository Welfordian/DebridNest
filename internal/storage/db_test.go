package storage

import (
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestOpenRunsMigrationsOnce(t *testing.T) {
	dir := t.TempDir()
	want := migrationNames(t)

	db, err := Open(dir)
	if err != nil {
		t.Fatalf("open fresh db: %v", err)
	}
	assertRecordedMigrations(t, db, want)
	assertColumnsExist(t, db.DB, "torrent_files", "object_key", "remote_stored", "streamable_bytes")
	if err := db.Close(); err != nil {
		t.Fatalf("close fresh db: %v", err)
	}

	db, err = Open(dir)
	if err != nil {
		t.Fatalf("reopen migrated db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	assertRecordedMigrations(t, db, want)
	assertColumnsExist(t, db.DB, "torrent_files", "object_key", "remote_stored", "streamable_bytes")
}

func TestOpenBootstrapsLegacyDatabaseWithObjectStorageColumns(t *testing.T) {
	dir := t.TempDir()
	seedLegacyDatabase(t, dir, "001_init.sql", "002_retention.sql", "003_admin.sql", "004_object_storage.sql")

	db, err := Open(dir)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	assertRecordedMigrations(t, db, migrationNames(t))
	assertColumnsExist(t, db.DB, "torrent_files", "object_key", "remote_stored", "streamable_bytes")
}

func TestOpenRepairsPartialLegacyObjectStorageMigration(t *testing.T) {
	dir := t.TempDir()
	seedLegacyDatabase(t, dir, "001_init.sql", "002_retention.sql", "003_admin.sql")

	raw := openRawDB(t, dir)
	if _, err := raw.Exec(`ALTER TABLE torrent_files ADD COLUMN object_key TEXT NOT NULL DEFAULT ''`); err != nil {
		t.Fatalf("seed partial object storage migration: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw db: %v", err)
	}

	db, err := Open(dir)
	if err != nil {
		t.Fatalf("open partial legacy db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	assertRecordedMigrations(t, db, migrationNames(t))
	assertColumnsExist(t, db.DB, "torrent_files", "object_key", "remote_stored", "streamable_bytes")
}

func TestFailedMagnetConversionsAreNotIncompleteOrActive(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := t.Context()
	now := time.Now().UTC()
	for _, rec := range []TorrentRecord{
		{ID: "ACTIVE01", InfoHash: "1111111111111111111111111111111111111111", Magnet: "magnet:?xt=urn:btih:1111111111111111111111111111111111111111", Status: "magnet_conversion", AddedAt: now},
		{ID: "FAILED01", InfoHash: "2222222222222222222222222222222222222222", Magnet: "magnet:?xt=urn:btih:2222222222222222222222222222222222222222", Status: "magnet_error", AddedAt: now},
	} {
		if err := db.CreateTorrent(ctx, rec); err != nil {
			t.Fatalf("create %s: %v", rec.ID, err)
		}
	}

	incomplete, err := db.ListIncompleteTorrents(ctx)
	if err != nil {
		t.Fatalf("list incomplete: %v", err)
	}
	if len(incomplete) != 1 || incomplete[0].ID != "ACTIVE01" {
		t.Fatalf("incomplete = %+v, want only ACTIVE01", incomplete)
	}

	active, err := db.CountActiveTorrents(ctx)
	if err != nil {
		t.Fatalf("count active: %v", err)
	}
	if active != 1 {
		t.Fatalf("active count = %d, want 1", active)
	}
}

func TestTorrentFileStreamableBytesRoundTrip(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx := t.Context()
	rec := TorrentRecord{
		ID:       "ROUNDTRIP01",
		InfoHash: "3333333333333333333333333333333333333333",
		Magnet:   "magnet:?xt=urn:btih:3333333333333333333333333333333333333333",
		Status:   "downloading",
		AddedAt:  time.Now().UTC(),
		Files: []TorrentFileRecord{
			{ID: 1, Path: "/movie.mkv", Bytes: 100, Selected: true, DownloadedBytes: 64, StreamableBytes: 32},
		},
	}
	if err := db.CreateTorrent(ctx, rec); err != nil {
		t.Fatalf("create torrent: %v", err)
	}

	got, err := db.GetTorrent(ctx, rec.ID)
	if err != nil {
		t.Fatalf("get torrent: %v", err)
	}
	if len(got.Files) != 1 || got.Files[0].StreamableBytes != 32 {
		t.Fatalf("streamable bytes after create = %+v, want 32", got.Files)
	}

	got.Files[0].StreamableBytes = 48
	if err := db.UpdateTorrentFiles(ctx, got.ID, got.Files); err != nil {
		t.Fatalf("update files: %v", err)
	}
	got, err = db.GetTorrent(ctx, rec.ID)
	if err != nil {
		t.Fatalf("get updated torrent: %v", err)
	}
	if got.Files[0].StreamableBytes != 48 {
		t.Fatalf("streamable bytes after update = %d, want 48", got.Files[0].StreamableBytes)
	}
}

func seedLegacyDatabase(t *testing.T, dir string, names ...string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	raw := openRawDB(t, dir)
	defer raw.Close()

	for _, name := range names {
		body, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		if _, err := raw.Exec(string(body)); err != nil {
			t.Fatalf("apply legacy migration %s: %v", name, err)
		}
	}
}

func openRawDB(t *testing.T, dir string) *sql.DB {
	t.Helper()

	raw, err := sql.Open("sqlite", filepath.Join(dir, "debridnest.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	return raw
}

func migrationNames(t *testing.T) []string {
	t.Helper()

	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	return names
}

func assertRecordedMigrations(t *testing.T, db *DB, want []string) {
	t.Helper()

	rows, err := db.Query(`SELECT filename FROM schema_migrations ORDER BY filename`)
	if err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	defer rows.Close()

	var got []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan schema_migrations: %v", err)
		}
		got = append(got, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate schema_migrations: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schema_migrations = %v, want %v", got, want)
	}
}

func assertColumnsExist(t *testing.T, db *sql.DB, table string, columns ...string) {
	t.Helper()

	rows, err := db.Query(`PRAGMA table_info(` + quoteSQLiteIdentifier(table) + `)`)
	if err != nil {
		t.Fatalf("read %s columns: %v", table, err)
	}
	defer rows.Close()

	got := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan %s columns: %v", table, err)
		}
		got[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate %s columns: %v", table, err)
	}
	for _, column := range columns {
		if !got[column] {
			t.Fatalf("%s.%s column missing; columns=%v", table, column, got)
		}
	}
}
