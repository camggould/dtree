package index

import "fmt"

// createStatements holds every CREATE TABLE/INDEX/VIRTUAL TABLE and trigger
// statement for the dtree index schema. All statements are idempotent (use IF
// NOT EXISTS where available; triggers use DROP IF EXISTS + CREATE).
var createStatements = []string{
	// -----------------------------------------------------------------------
	// Core tables
	// -----------------------------------------------------------------------

	`CREATE TABLE IF NOT EXISTS trees (
		slug             TEXT PRIMARY KEY,
		title            TEXT NOT NULL DEFAULT '',
		description      TEXT NOT NULL DEFAULT '',
		archived         INTEGER NOT NULL DEFAULT 0,
		created_at       TEXT NOT NULL,
		layout_direction TEXT NOT NULL DEFAULT 'TB',
		schema_version   INTEGER NOT NULL DEFAULT 1
	)`,

	`CREATE TABLE IF NOT EXISTS actors (
		handle TEXT PRIMARY KEY,
		name   TEXT NOT NULL DEFAULT '',
		email  TEXT NOT NULL DEFAULT '',
		kind   TEXT NOT NULL CHECK(kind IN ('human','agent')),
		active INTEGER NOT NULL DEFAULT 1
	)`,

	`CREATE TABLE IF NOT EXISTS decisions (
		id                   TEXT PRIMARY KEY,
		tree                 TEXT NOT NULL REFERENCES trees(slug) ON DELETE CASCADE,
		slug                 TEXT NOT NULL,
		summary              TEXT NOT NULL,
		description          TEXT NOT NULL DEFAULT '',
		status               TEXT NOT NULL CHECK(status IN ('proposed','decided','out_of_scope','superseded')),
		priority             TEXT NOT NULL CHECK(priority IN ('assumption','low','medium','high','critical')),
		creator              TEXT NOT NULL,
		assignee             TEXT NOT NULL DEFAULT '',
		recommended_summary  TEXT NOT NULL DEFAULT '',
		recommended_full     TEXT NOT NULL DEFAULT '',
		recommended_by       TEXT NOT NULL DEFAULT '',
		actual_choice        TEXT NOT NULL DEFAULT '',
		actual_choice_reason TEXT NOT NULL DEFAULT '',
		is_recommended       INTEGER NOT NULL DEFAULT 0,
		out_of_scope_reason  TEXT NOT NULL DEFAULT '',
		schema_version       INTEGER NOT NULL DEFAULT 1,
		rev                  TEXT NOT NULL DEFAULT '',
		content_sha256       TEXT NOT NULL DEFAULT '',
		deleted              INTEGER NOT NULL DEFAULT 0
	)`,

	`CREATE TABLE IF NOT EXISTS decision_deciders (
		decision_id TEXT NOT NULL REFERENCES decisions(id) ON DELETE CASCADE,
		handle      TEXT NOT NULL,
		PRIMARY KEY (decision_id, handle)
	)`,

	`CREATE TABLE IF NOT EXISTS decision_tags (
		decision_id TEXT NOT NULL REFERENCES decisions(id) ON DELETE CASCADE,
		tag         TEXT NOT NULL,
		PRIMARY KEY (decision_id, tag)
	)`,

	`CREATE TABLE IF NOT EXISTS relationships (
		source           TEXT NOT NULL,
		target           TEXT NOT NULL,
		type             TEXT NOT NULL CHECK(type IN ('blocks','influences','supersedes','relates_to')),
		tree             TEXT NOT NULL,
		created_event_id TEXT NOT NULL,
		PRIMARY KEY (source, target, type)
	)`,

	`CREATE TABLE IF NOT EXISTS events (
		event_id     TEXT PRIMARY KEY,
		ts           TEXT NOT NULL,
		actor        TEXT NOT NULL,
		action       TEXT NOT NULL,
		kind         TEXT NOT NULL,
		tree         TEXT,
		target_id    TEXT NOT NULL,
		payload_json TEXT NOT NULL,
		source_file  TEXT NOT NULL DEFAULT ''
	)`,

	// -----------------------------------------------------------------------
	// Tokens table (bearer-token auth)
	// -----------------------------------------------------------------------

	`CREATE TABLE IF NOT EXISTS tokens (
		token_hash TEXT PRIMARY KEY,
		handle     TEXT NOT NULL,
		created_at TEXT NOT NULL,
		expires_at TEXT,
		revoked    INTEGER NOT NULL DEFAULT 0,
		label      TEXT
	)`,

	`CREATE INDEX IF NOT EXISTS idx_tokens_handle ON tokens(handle)`,

	// -----------------------------------------------------------------------
	// Indexes
	// -----------------------------------------------------------------------

	`CREATE INDEX IF NOT EXISTS decisions_tree_status_priority ON decisions(tree, status, priority)`,
	`CREATE INDEX IF NOT EXISTS decisions_creator              ON decisions(creator)`,
	`CREATE INDEX IF NOT EXISTS decisions_recommended_by       ON decisions(recommended_by)`,
	`CREATE INDEX IF NOT EXISTS decisions_assignee             ON decisions(assignee)`,
	`CREATE INDEX IF NOT EXISTS decision_deciders_handle       ON decision_deciders(handle)`,
	`CREATE INDEX IF NOT EXISTS decision_tags_tag              ON decision_tags(tag)`,
	`CREATE INDEX IF NOT EXISTS relationships_target_type      ON relationships(target, type)`,
	`CREATE INDEX IF NOT EXISTS relationships_tree_type        ON relationships(tree, type)`,
	`CREATE INDEX IF NOT EXISTS events_ts                      ON events(ts)`,
	`CREATE INDEX IF NOT EXISTS events_actor_ts                ON events(actor, ts)`,
	`CREATE INDEX IF NOT EXISTS events_target_ts               ON events(target_id, ts)`,
	`CREATE INDEX IF NOT EXISTS events_tree_action_ts          ON events(tree, action, ts)`,

	// -----------------------------------------------------------------------
	// FTS5 virtual tables
	// -----------------------------------------------------------------------

	`CREATE VIRTUAL TABLE IF NOT EXISTS decisions_fts USING fts5(
		summary, description, recommended_summary, recommended_full,
		actual_choice, actual_choice_reason,
		content='decisions', content_rowid='rowid', tokenize='porter unicode61'
	)`,

	`CREATE VIRTUAL TABLE IF NOT EXISTS events_fts USING fts5(
		payload_json, content='events', content_rowid='rowid', tokenize='unicode61'
	)`,

	// -----------------------------------------------------------------------
	// FTS5 sync triggers — decisions
	// -----------------------------------------------------------------------

	`DROP TRIGGER IF EXISTS decisions_fts_ai`,
	`CREATE TRIGGER decisions_fts_ai AFTER INSERT ON decisions BEGIN
		INSERT INTO decisions_fts(rowid, summary, description, recommended_summary, recommended_full, actual_choice, actual_choice_reason)
		VALUES (new.rowid, new.summary, new.description, new.recommended_summary, new.recommended_full, new.actual_choice, new.actual_choice_reason);
	END`,

	`DROP TRIGGER IF EXISTS decisions_fts_ad`,
	`CREATE TRIGGER decisions_fts_ad AFTER DELETE ON decisions BEGIN
		INSERT INTO decisions_fts(decisions_fts, rowid, summary, description, recommended_summary, recommended_full, actual_choice, actual_choice_reason)
		VALUES ('delete', old.rowid, old.summary, old.description, old.recommended_summary, old.recommended_full, old.actual_choice, old.actual_choice_reason);
	END`,

	`DROP TRIGGER IF EXISTS decisions_fts_au`,
	`CREATE TRIGGER decisions_fts_au AFTER UPDATE ON decisions BEGIN
		INSERT INTO decisions_fts(decisions_fts, rowid, summary, description, recommended_summary, recommended_full, actual_choice, actual_choice_reason)
		VALUES ('delete', old.rowid, old.summary, old.description, old.recommended_summary, old.recommended_full, old.actual_choice, old.actual_choice_reason);
		INSERT INTO decisions_fts(rowid, summary, description, recommended_summary, recommended_full, actual_choice, actual_choice_reason)
		VALUES (new.rowid, new.summary, new.description, new.recommended_summary, new.recommended_full, new.actual_choice, new.actual_choice_reason);
	END`,

	// -----------------------------------------------------------------------
	// FTS5 sync triggers — events
	// -----------------------------------------------------------------------

	`DROP TRIGGER IF EXISTS events_fts_ai`,
	`CREATE TRIGGER events_fts_ai AFTER INSERT ON events BEGIN
		INSERT INTO events_fts(rowid, payload_json)
		VALUES (new.rowid, new.payload_json);
	END`,

	`DROP TRIGGER IF EXISTS events_fts_ad`,
	`CREATE TRIGGER events_fts_ad AFTER DELETE ON events BEGIN
		INSERT INTO events_fts(events_fts, rowid, payload_json)
		VALUES ('delete', old.rowid, old.payload_json);
	END`,

	`DROP TRIGGER IF EXISTS events_fts_au`,
	`CREATE TRIGGER events_fts_au AFTER UPDATE ON events BEGIN
		INSERT INTO events_fts(events_fts, rowid, payload_json)
		VALUES ('delete', old.rowid, old.payload_json);
		INSERT INTO events_fts(rowid, payload_json)
		VALUES (new.rowid, new.payload_json);
	END`,
}

// dropStatements lists DROP statements for all dtree-owned tables (and their
// dependents). Order matters: dependent tables/views first, then base tables.
// FTS5 virtual tables and their backing shadow tables are dropped as a unit.
var dropStatements = []string{
	// Triggers
	`DROP TRIGGER IF EXISTS decisions_fts_au`,
	`DROP TRIGGER IF EXISTS decisions_fts_ad`,
	`DROP TRIGGER IF EXISTS decisions_fts_ai`,
	`DROP TRIGGER IF EXISTS events_fts_au`,
	`DROP TRIGGER IF EXISTS events_fts_ad`,
	`DROP TRIGGER IF EXISTS events_fts_ai`,

	// FTS5 virtual tables (shadow tables dropped automatically)
	`DROP TABLE IF EXISTS decisions_fts`,
	`DROP TABLE IF EXISTS events_fts`,

	// Junction / child tables (FK references to parent rows)
	`DROP TABLE IF EXISTS decision_deciders`,
	`DROP TABLE IF EXISTS decision_tags`,
	`DROP TABLE IF EXISTS relationships`,
	`DROP TABLE IF EXISTS events`,
	`DROP TABLE IF EXISTS decisions`,

	// Root tables
	`DROP TABLE IF EXISTS actors`,
	`DROP TABLE IF EXISTS trees`,
}

// CreateSchema creates all tables, indexes, FTS5 virtual tables, and sync
// triggers needed by the dtree index. It is idempotent: calling it on an
// already-initialized database is safe and a no-op for existing objects.
//
// If schema_version is not yet set in _meta, CreateSchema stamps it with
// CurrentSchemaVersion. (Open already called ensureMetaTable, so _meta exists.)
func (db *DB) CreateSchema() error {
	for _, stmt := range createStatements {
		if _, err := db.conn.Exec(stmt); err != nil {
			return fmt.Errorf("index: create schema: %w\nstatement: %s", err, stmt)
		}
	}

	// Stamp schema_version if not already set.
	v, err := db.SchemaVersion()
	if err != nil {
		return fmt.Errorf("index: create schema: read version: %w", err)
	}
	if v == 0 {
		if err := db.SetSchemaVersion(CurrentSchemaVersion); err != nil {
			return fmt.Errorf("index: create schema: set version: %w", err)
		}
	}
	return nil
}

// DropAll removes every dtree-owned table (and associated triggers/virtual
// tables) from the database. It is used by reindex to start fresh, and by
// tests to reset state. The _meta table is intentionally preserved.
func (db *DB) DropAll() error {
	for _, stmt := range dropStatements {
		if _, err := db.conn.Exec(stmt); err != nil {
			return fmt.Errorf("index: drop all: %w\nstatement: %s", err, stmt)
		}
	}
	return nil
}
