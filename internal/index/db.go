// Package index manages the SQLite-backed query layer that sits on top
// of the canonical YAML + JSONL storage. The database file is fully
// rebuildable via dtree reindex; nothing here is the source of truth.
//
// On open we set:
//   - WAL mode (concurrent readers + one writer; readers see committed writes)
//   - busy_timeout=5000ms (briefly contend rather than fail-fast)
//   - foreign_keys=on (so relationships referential integrity holds)
//
// _meta(schema_version) tracks the index schema generation. Migrations
// (a separate package) auto-run on startup if the on-disk version trails
// the embedded one.
package index

import (
	"database/sql"
	"errors"
	"fmt"

	_ "github.com/mattn/go-sqlite3" // sqlite3 driver
)

// CurrentSchemaVersion is the index schema version this build supports.
// Bump when schema-defining migrations are added; the migration runner
// uses this to decide what to apply.
const CurrentSchemaVersion = 2

// DB wraps *sql.DB with dtree-specific helpers.
type DB struct {
	conn *sql.DB
	path string
}

// Open initializes (or opens) the SQLite index at path. The file is
// created if missing. WAL mode survives across reopen, but we set the
// pragmas every time anyway (cheap and idempotent).
func Open(path string) (*DB, error) {
	// _journal_mode and _busy_timeout are honored by the mattn driver
	// via DSN params; explicit PRAGMAs below cover anything the DSN can't.
	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on", path)
	conn, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("index: open %s: %w", path, err)
	}
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("index: ping: %w", err)
	}

	db := &DB{conn: conn, path: path}

	if err := db.applyPragmas(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := db.ensureMetaTable(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := db.CreateSchema(); err != nil {
		_ = conn.Close()
		return nil, err
	}

	// Check schema version after CreateSchema stamps a fresh DB.
	current, err := db.SchemaVersion()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if current > CurrentSchemaVersion {
		_ = conn.Close()
		return nil, fmt.Errorf(
			"index: binary too old; upgrade dtree (db schema_version=%d, binary supports up to %d)",
			current, CurrentSchemaVersion,
		)
	}

	return db, nil
}

// Close shuts down the underlying database. Safe to call multiple times.
func (db *DB) Close() error {
	if db == nil || db.conn == nil {
		return nil
	}
	err := db.conn.Close()
	db.conn = nil
	return err
}

// Conn returns the underlying *sql.DB. Other packages in this module
// use it for their own table creation/queries.
func (db *DB) Conn() *sql.DB { return db.conn }

// Path returns the on-disk database path.
func (db *DB) Path() string { return db.path }

// SchemaVersion reports the version recorded in _meta. Returns 0 if
// _meta has no row yet (fresh DB).
func (db *DB) SchemaVersion() (int, error) {
	var v int
	err := db.conn.QueryRow("SELECT value FROM _meta WHERE key = 'schema_version'").Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("index: read schema_version: %w", err)
	}
	return v, nil
}

// NeedsMigration reports whether the on-disk schema version is behind the
// version this binary supports. Returns (current, target, true) when a
// migration is needed, or (current, target, false) when already up to date.
//
// The caller (typically the dtree migrate command) uses this to decide
// whether to invoke migrations.Default().Apply(...). The index package itself
// does not import migrations to avoid a circular dependency.
func (db *DB) NeedsMigration() (current, target int, needed bool) {
	v, err := db.SchemaVersion()
	if err != nil {
		// Treat errors conservatively — don't claim migration is needed.
		return 0, CurrentSchemaVersion, false
	}
	return v, CurrentSchemaVersion, v < CurrentSchemaVersion
}

// SetSchemaVersion writes the schema version to _meta. Migration code
// calls this after applying a step.
func (db *DB) SetSchemaVersion(v int) error {
	_, err := db.conn.Exec(
		`INSERT INTO _meta(key, value) VALUES('schema_version', ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		v,
	)
	if err != nil {
		return fmt.Errorf("index: set schema_version: %w", err)
	}
	return nil
}

func (db *DB) applyPragmas() error {
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
		"PRAGMA synchronous = NORMAL", // WAL+NORMAL is the standard durable+fast pairing
	}
	for _, p := range pragmas {
		if _, err := db.conn.Exec(p); err != nil {
			return fmt.Errorf("index: pragma %q: %w", p, err)
		}
	}
	return nil
}

func (db *DB) ensureMetaTable() error {
	_, err := db.conn.Exec(`
		CREATE TABLE IF NOT EXISTS _meta (
			key   TEXT PRIMARY KEY,
			value INTEGER NOT NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("index: create _meta: %w", err)
	}
	return nil
}
