package index

import (
	"path/filepath"
	"testing"
)

// openTestDB opens a fresh, fully-initialized DB in a temp directory and
// registers cleanup. Fatals the test on error.
func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), ".index.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// insertTree inserts a minimal tree row; fatals on error.
func insertTree(t *testing.T, db *DB, slug string) {
	t.Helper()
	_, err := db.Conn().Exec(
		`INSERT INTO trees(slug, created_at) VALUES(?, ?)`,
		slug, "2024-01-01T00:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert tree %q: %v", slug, err)
	}
}

// insertDecision inserts a minimal decision row and returns its rowid.
func insertDecision(t *testing.T, db *DB, id, tree, summary string) int64 {
	t.Helper()
	res, err := db.Conn().Exec(
		`INSERT INTO decisions(id, tree, slug, summary, status, priority, creator)
		 VALUES(?, ?, ?, ?, 'proposed', 'medium', 'alice')`,
		id, tree, id+"-slug", summary,
	)
	if err != nil {
		t.Fatalf("insert decision %q: %v", id, err)
	}
	rowid, _ := res.LastInsertId()
	return rowid
}

// TestCreateSchemaIdempotent verifies that calling CreateSchema twice on the
// same DB does not return an error (all statements use IF NOT EXISTS, and
// triggers are DROP-IF-EXISTS + CREATE).
func TestCreateSchemaIdempotent(t *testing.T) {
	db := openTestDB(t)
	// First call already happened inside Open; call a second time.
	if err := db.CreateSchema(); err != nil {
		t.Fatalf("second CreateSchema: %v", err)
	}
}

// TestDecisionsTableShape inserts a fully-populated decision row and reads
// back every column to confirm types and defaults.
func TestDecisionsTableShape(t *testing.T) {
	db := openTestDB(t)
	insertTree(t, db, "arch")

	_, err := db.Conn().Exec(`
		INSERT INTO decisions(
			id, tree, slug, summary, description,
			status, priority, creator, assignee,
			recommended_summary, recommended_full, recommended_by,
			actual_choice, actual_choice_reason, is_recommended,
			out_of_scope_reason, schema_version, rev, content_sha256, deleted
		) VALUES (
			'01ABCDEFGHJKMNPQRSTVWXYZ01', 'arch', 'use-postgres',
			'Use PostgreSQL for the main store',
			'Full description here',
			'proposed', 'high', 'alice', 'bob',
			'rec summary', 'rec full', 'agent-1',
			'chose postgres', 'it fits best', 1,
			'', 1, 'rev-ulid', 'sha256abc', 0
		)`,
	)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	var (
		id, tree, slug, summary, desc          string
		status, priority, creator, assignee    string
		recSummary, recFull, recBy             string
		actualChoice, actualChoiceReason       string
		isRecommended                          int
		oosReason, rev, sha256                 string
		schemaVer, deleted                     int
	)
	err = db.Conn().QueryRow(`
		SELECT id, tree, slug, summary, description,
		       status, priority, creator, assignee,
		       recommended_summary, recommended_full, recommended_by,
		       actual_choice, actual_choice_reason, is_recommended,
		       out_of_scope_reason, schema_version, rev, content_sha256, deleted
		FROM decisions WHERE id = '01ABCDEFGHJKMNPQRSTVWXYZ01'`,
	).Scan(
		&id, &tree, &slug, &summary, &desc,
		&status, &priority, &creator, &assignee,
		&recSummary, &recFull, &recBy,
		&actualChoice, &actualChoiceReason,
		&isRecommended,
		&oosReason, &schemaVer, &rev, &sha256, &deleted,
	)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}

	checks := []struct {
		name string
		got  string
		want string
	}{
		{"id", id, "01ABCDEFGHJKMNPQRSTVWXYZ01"},
		{"tree", tree, "arch"},
		{"slug", slug, "use-postgres"},
		{"summary", summary, "Use PostgreSQL for the main store"},
		{"description", desc, "Full description here"},
		{"status", status, "proposed"},
		{"priority", priority, "high"},
		{"creator", creator, "alice"},
		{"assignee", assignee, "bob"},
		{"recommended_summary", recSummary, "rec summary"},
		{"recommended_full", recFull, "rec full"},
		{"recommended_by", recBy, "agent-1"},
		{"actual_choice", actualChoice, "chose postgres"},
		{"actual_choice_reason", actualChoiceReason, "it fits best"},
		{"rev", rev, "rev-ulid"},
		{"content_sha256", sha256, "sha256abc"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
	if isRecommended != 1 {
		t.Errorf("is_recommended = %d, want 1", isRecommended)
	}
	if schemaVer != 1 {
		t.Errorf("schema_version = %d, want 1", schemaVer)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0", deleted)
	}
}

// TestForeignKeyCascadeOnTreeDelete verifies that deleting a tree cascades to
// its decisions and their tags.
func TestForeignKeyCascadeOnTreeDelete(t *testing.T) {
	db := openTestDB(t)
	insertTree(t, db, "treex")
	insertDecision(t, db, "01DEC0000000000000000000001", "treex", "some decision")

	_, err := db.Conn().Exec(
		`INSERT INTO decision_tags(decision_id, tag) VALUES(?,?)`,
		"01DEC0000000000000000000001", "backend",
	)
	if err != nil {
		t.Fatalf("insert tag: %v", err)
	}

	// Delete the tree — should cascade to decisions and tags.
	if _, err := db.Conn().Exec(`DELETE FROM trees WHERE slug='treex'`); err != nil {
		t.Fatalf("delete tree: %v", err)
	}

	var count int
	db.Conn().QueryRow(`SELECT COUNT(*) FROM decisions`).Scan(&count)
	if count != 0 {
		t.Errorf("decisions count = %d after tree delete, want 0", count)
	}
	db.Conn().QueryRow(`SELECT COUNT(*) FROM decision_tags`).Scan(&count)
	if count != 0 {
		t.Errorf("decision_tags count = %d after tree delete, want 0", count)
	}
}

// TestEnumChecks verifies that CHECK constraints reject invalid enum values
// for status, priority, actor kind, and relationship type.
func TestEnumChecks(t *testing.T) {
	db := openTestDB(t)
	insertTree(t, db, "t")

	tests := []struct {
		name string
		stmt string
		args []any
	}{
		{
			"bad status",
			`INSERT INTO decisions(id, tree, slug, summary, status, priority, creator)
			 VALUES(?,?,?,?,'wat','medium','alice')`,
			[]any{"id1", "t", "s1", "summary1"},
		},
		{
			"bad priority",
			`INSERT INTO decisions(id, tree, slug, summary, status, priority, creator)
			 VALUES(?,?,?,?,'proposed','wat','alice')`,
			[]any{"id2", "t", "s2", "summary2"},
		},
		{
			"bad actor kind",
			`INSERT INTO actors(handle, kind) VALUES(?,?)`,
			[]any{"actor1", "wat"},
		},
		{
			"bad relationship type",
			`INSERT INTO relationships(source, target, type, tree, created_event_id)
			 VALUES(?,?,?,?,?)`,
			[]any{"src", "tgt", "wat", "t", "evid"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.Conn().Exec(tc.stmt, tc.args...)
			if err == nil {
				t.Errorf("expected constraint error for %s, got nil", tc.name)
			}
		})
	}
}

// TestDecisionRevColumn inserts a decision with a specific rev value and reads
// it back.
func TestDecisionRevColumn(t *testing.T) {
	db := openTestDB(t)
	insertTree(t, db, "t")
	const wantRev = "01REV0000000000000000000001"

	_, err := db.Conn().Exec(
		`INSERT INTO decisions(id, tree, slug, summary, status, priority, creator, rev)
		 VALUES(?,?,?,?,?,?,?,?)`,
		"01DEC0000000000000000000001", "t", "d-slug", "A summary", "proposed", "low", "alice", wantRev,
	)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	var gotRev string
	if err := db.Conn().QueryRow(`SELECT rev FROM decisions WHERE id=?`, "01DEC0000000000000000000001").Scan(&gotRev); err != nil {
		t.Fatalf("scan rev: %v", err)
	}
	if gotRev != wantRev {
		t.Errorf("rev = %q, want %q", gotRev, wantRev)
	}
}

// TestFTSDecisionsInsertSearchable inserts a decision whose summary contains
// "database" and verifies the FTS index can locate it.
func TestFTSDecisionsInsertSearchable(t *testing.T) {
	db := openTestDB(t)
	insertTree(t, db, "t")
	rowid := insertDecision(t, db, "01DEC0000000000000000000001", "t", "Choose the right database engine")

	var gotRowid int64
	err := db.Conn().QueryRow(
		`SELECT rowid FROM decisions_fts WHERE decisions_fts MATCH 'database'`,
	).Scan(&gotRowid)
	if err != nil {
		t.Fatalf("FTS query: %v", err)
	}
	if gotRowid != rowid {
		t.Errorf("FTS rowid = %d, want %d", gotRowid, rowid)
	}
}

// TestFTSEventsInsertSearchable inserts an event with payload_json containing a
// unique word and verifies the FTS index returns it.
func TestFTSEventsInsertSearchable(t *testing.T) {
	db := openTestDB(t)

	_, err := db.Conn().Exec(
		`INSERT INTO events(event_id, ts, actor, action, kind, target_id, payload_json)
		 VALUES(?,?,?,?,?,?,?)`,
		"01EVT0000000000000000000001",
		"2024-01-01T00:00:00Z",
		"alice", "create", "decision",
		"01DEC0000000000000000000001",
		`{"summary":"xyzzyplugh something unique xyzzyplugh"}`,
	)
	if err != nil {
		t.Fatalf("insert event: %v", err)
	}

	var count int
	err = db.Conn().QueryRow(
		`SELECT COUNT(*) FROM events_fts WHERE events_fts MATCH 'xyzzyplugh'`,
	).Scan(&count)
	if err != nil {
		t.Fatalf("FTS events query: %v", err)
	}
	if count != 1 {
		t.Errorf("events_fts match count = %d, want 1", count)
	}
}

// TestFTSStaysSyncedOnUpdate updates a decision's summary and verifies the FTS
// index reflects the new value (old value no longer matches).
func TestFTSStaysSyncedOnUpdate(t *testing.T) {
	db := openTestDB(t)
	insertTree(t, db, "t")
	insertDecision(t, db, "01DEC0000000000000000000001", "t", "old unique quuxfoo term")

	_, err := db.Conn().Exec(
		`UPDATE decisions SET summary=? WHERE id=?`,
		"new unique barqux term", "01DEC0000000000000000000001",
	)
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	var count int
	db.Conn().QueryRow(
		`SELECT COUNT(*) FROM decisions_fts WHERE decisions_fts MATCH 'quuxfoo'`,
	).Scan(&count)
	if count != 0 {
		t.Errorf("old term 'quuxfoo' still found in FTS after update, want 0")
	}

	db.Conn().QueryRow(
		`SELECT COUNT(*) FROM decisions_fts WHERE decisions_fts MATCH 'barqux'`,
	).Scan(&count)
	if count != 1 {
		t.Errorf("new term 'barqux' not found in FTS after update, want 1")
	}
}

// TestFTSStaysSyncedOnDelete deletes a decision and verifies FTS no longer
// returns it.
func TestFTSStaysSyncedOnDelete(t *testing.T) {
	db := openTestDB(t)
	insertTree(t, db, "t")
	insertDecision(t, db, "01DEC0000000000000000000001", "t", "zorbflex unique keyword term")

	if _, err := db.Conn().Exec(`DELETE FROM decisions WHERE id=?`, "01DEC0000000000000000000001"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	var count int
	db.Conn().QueryRow(
		`SELECT COUNT(*) FROM decisions_fts WHERE decisions_fts MATCH 'zorbflex'`,
	).Scan(&count)
	if count != 0 {
		t.Errorf("deleted term 'zorbflex' still found in FTS, want 0")
	}
}

// TestIndexesExist queries sqlite_master to verify all expected indexes are
// present.
func TestIndexesExist(t *testing.T) {
	db := openTestDB(t)

	expected := []string{
		"decisions_tree_status_priority",
		"decisions_creator",
		"decisions_recommended_by",
		"decisions_assignee",
		"decision_deciders_handle",
		"decision_tags_tag",
		"relationships_target_type",
		"relationships_tree_type",
		"events_ts",
		"events_actor_ts",
		"events_target_ts",
		"events_tree_action_ts",
	}

	rows, err := db.Conn().Query(
		`SELECT name FROM sqlite_master WHERE type='index' AND name NOT LIKE 'sqlite_%'`,
	)
	if err != nil {
		t.Fatalf("query indexes: %v", err)
	}
	defer rows.Close()

	found := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan index name: %v", err)
		}
		found[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}

	for _, idx := range expected {
		if !found[idx] {
			t.Errorf("index %q not found in sqlite_master", idx)
		}
	}
}

// TestDropAll creates a schema with data, calls DropAll, verifies all dtree
// tables are gone from sqlite_master, then verifies CreateSchema succeeds again.
func TestDropAll(t *testing.T) {
	db := openTestDB(t)
	insertTree(t, db, "t")
	insertDecision(t, db, "01DEC0000000000000000000001", "t", "some summary")

	if err := db.DropAll(); err != nil {
		t.Fatalf("DropAll: %v", err)
	}

	dtreeTables := []string{
		"trees", "actors", "decisions", "decision_deciders", "decision_tags",
		"relationships", "events", "decisions_fts", "events_fts",
	}

	rows, err := db.Conn().Query(
		`SELECT name FROM sqlite_master WHERE type IN ('table','shadow') AND name NOT LIKE 'sqlite_%' AND name != '_meta'`,
	)
	if err != nil {
		t.Fatalf("query tables: %v", err)
	}
	defer rows.Close()

	remaining := map[string]bool{}
	for rows.Next() {
		var name string
		rows.Scan(&name)
		remaining[name] = true
	}

	for _, tbl := range dtreeTables {
		if remaining[tbl] {
			t.Errorf("table %q still exists after DropAll", tbl)
		}
	}

	// CreateSchema should work on the now-empty DB.
	if err := db.CreateSchema(); err != nil {
		t.Fatalf("CreateSchema after DropAll: %v", err)
	}
}

// TestSchemaVersionSetByCreate verifies that a fresh DB opened with Open
// (which calls CreateSchema) has schema_version = CurrentSchemaVersion.
func TestSchemaVersionSetByCreate(t *testing.T) {
	db := openTestDB(t)

	v, err := db.SchemaVersion()
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if v != CurrentSchemaVersion {
		t.Errorf("schema_version = %d, want %d (CurrentSchemaVersion)", v, CurrentSchemaVersion)
	}
}
