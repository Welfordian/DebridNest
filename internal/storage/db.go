package storage

import (
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

type DB struct {
	*sql.DB
}

func Open(dataDir string) (*DB, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	dbPath := filepath.Join(dataDir, "debridnest.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	s := &DB{DB: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (db *DB) migrate() error {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	if err := bootstrapLegacyMigrations(tx); err != nil {
		return err
	}

	applied, err := appliedMigrations(tx)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if applied[entry.Name()] {
			continue
		}
		body, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		if _, err := tx.Exec(string(body)); err != nil {
			return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
		}
		if err := recordMigration(tx, entry.Name()); err != nil {
			return fmt.Errorf("record migration %s: %w", entry.Name(), err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	committed = true
	return nil
}

func appliedMigrations(tx *sql.Tx) (map[string]bool, error) {
	rows, err := tx.Query(`SELECT filename FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var filename string
		if err := rows.Scan(&filename); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		applied[filename] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schema_migrations: %w", err)
	}
	return applied, nil
}

func bootstrapLegacyMigrations(tx *sql.Tx) error {
	initApplied, err := legacyInitMigrationApplied(tx)
	if err != nil {
		return err
	}
	if initApplied {
		if err := recordMigration(tx, "001_init.sql"); err != nil {
			return fmt.Errorf("record legacy migration 001_init.sql: %w", err)
		}
	}

	initRecorded, err := migrationRecorded(tx, "001_init.sql")
	if err != nil {
		return err
	}
	if initRecorded {
		if err := recordMigration(tx, "002_retention.sql"); err != nil {
			return fmt.Errorf("record legacy migration 002_retention.sql: %w", err)
		}
	}

	adminApplied, err := legacyAdminMigrationApplied(tx)
	if err != nil {
		return err
	}
	if adminApplied {
		if err := recordMigration(tx, "003_admin.sql"); err != nil {
			return fmt.Errorf("record legacy migration 003_admin.sql: %w", err)
		}
	}

	if err := bootstrapLegacyObjectStorageMigration(tx); err != nil {
		return err
	}
	return nil
}

func legacyInitMigrationApplied(tx *sql.Tx) (bool, error) {
	return schemaObjectsExist(tx,
		[]string{"torrents", "torrent_files", "host_links", "downloads"},
		[]string{"idx_torrents_info_hash", "idx_torrents_added_at"},
	)
}

func legacyAdminMigrationApplied(tx *sql.Tx) (bool, error) {
	return schemaObjectsExist(tx,
		[]string{"users", "runtime_settings", "activity_log"},
		[]string{"idx_activity_log_created_at"},
	)
}

func bootstrapLegacyObjectStorageMigration(tx *sql.Tx) error {
	filesTable, err := tableExists(tx, "torrent_files")
	if err != nil {
		return err
	}
	if !filesTable {
		return nil
	}

	hasObjectKey, err := columnExists(tx, "torrent_files", "object_key")
	if err != nil {
		return err
	}
	hasRemoteStored, err := columnExists(tx, "torrent_files", "remote_stored")
	if err != nil {
		return err
	}

	if !hasObjectKey && !hasRemoteStored {
		return nil
	}
	if !hasObjectKey {
		if _, err := tx.Exec(`ALTER TABLE torrent_files ADD COLUMN object_key TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("repair legacy migration 004_object_storage.sql object_key: %w", err)
		}
	}
	if !hasRemoteStored {
		if _, err := tx.Exec(`ALTER TABLE torrent_files ADD COLUMN remote_stored INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("repair legacy migration 004_object_storage.sql remote_stored: %w", err)
		}
	}
	if err := recordMigration(tx, "004_object_storage.sql"); err != nil {
		return fmt.Errorf("record legacy migration 004_object_storage.sql: %w", err)
	}
	return nil
}

func schemaObjectsExist(tx *sql.Tx, tables, indexes []string) (bool, error) {
	for _, table := range tables {
		exists, err := tableExists(tx, table)
		if err != nil {
			return false, err
		}
		if !exists {
			return false, nil
		}
	}
	for _, index := range indexes {
		exists, err := indexExists(tx, index)
		if err != nil {
			return false, err
		}
		if !exists {
			return false, nil
		}
	}
	return true, nil
}

func tableExists(tx *sql.Tx, name string) (bool, error) {
	return sqliteSchemaObjectExists(tx, "table", name)
}

func indexExists(tx *sql.Tx, name string) (bool, error) {
	return sqliteSchemaObjectExists(tx, "index", name)
}

func sqliteSchemaObjectExists(tx *sql.Tx, objectType, name string) (bool, error) {
	var count int
	if err := tx.QueryRow(`SELECT COUNT(1) FROM sqlite_master WHERE type = ? AND name = ?`, objectType, name).Scan(&count); err != nil {
		return false, fmt.Errorf("inspect sqlite schema object %s %s: %w", objectType, name, err)
	}
	return count > 0, nil
}

func columnExists(tx *sql.Tx, table, column string) (bool, error) {
	rows, err := tx.Query(`PRAGMA table_info(` + quoteSQLiteIdentifier(table) + `)`)
	if err != nil {
		return false, fmt.Errorf("inspect table %s columns: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			return false, fmt.Errorf("scan table %s columns: %w", table, err)
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate table %s columns: %w", table, err)
	}
	return false, nil
}

func quoteSQLiteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func migrationRecorded(tx *sql.Tx, filename string) (bool, error) {
	var count int
	if err := tx.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE filename = ?`, filename).Scan(&count); err != nil {
		return false, fmt.Errorf("check schema_migrations %s: %w", filename, err)
	}
	return count > 0, nil
}

func recordMigration(tx *sql.Tx, filename string) error {
	_, err := tx.Exec(`INSERT OR IGNORE INTO schema_migrations (filename, applied_at) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, filename)
	return err
}
