// Package index — decision CRUD helpers.
//
// GetDecision, InsertDecision, UpdateDecision, and DeleteDecision are the
// canonical write-side helpers for the decisions table and its junction
// tables (decision_deciders, decision_tags, relationships). All mutations
// run in a single transaction so the index stays consistent.
//
// GetDecisionRev returns the current rev token without loading the full row.
package index

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/ulid"
)

// GetDecision loads a full Decision from the index by ID. It joins
// decisions, decision_deciders, decision_tags, and relationships.
//
// Returns (nil, nil) when the ID does not exist.
func GetDecision(db *DB, id string) (*core.Decision, error) {
	const q = `
		SELECT id, tree, slug, summary, description,
		       status, priority, creator, assignee,
		       recommended_summary, recommended_full, recommended_by,
		       actual_choice, actual_choice_reason, is_recommended,
		       out_of_scope_reason, schema_version, rev, content_sha256, deleted
		FROM decisions WHERE id = ?`

	var d core.Decision
	var isRec, deleted int
	err := db.conn.QueryRow(q, id).Scan(
		&d.ID, &d.Tree, &d.Slug, &d.Summary, &d.Description,
		(*string)(&d.Status), (*string)(&d.Priority), &d.Creator, &d.Assignee,
		&d.RecommendedSummary, &d.RecommendedFull, &d.RecommendedBy,
		&d.ActualChoice, &d.ActualChoiceReason, &isRec,
		&d.OutOfScopeReason, &d.SchemaVersion, &d.Rev, new(string), &deleted,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("index: get decision %s: %w", id, err)
	}
	d.IsRecommended = isRec == 1

	// decided_by
	deciders, err := getDeciders(db, id)
	if err != nil {
		return nil, err
	}
	d.DecidedBy = deciders

	// tags
	tags, err := getTags(db, id)
	if err != nil {
		return nil, err
	}
	d.Tags = tags

	// relationships (outgoing from this decision)
	rels, err := getRelationships(db, id)
	if err != nil {
		return nil, err
	}
	d.Relationships = rels

	return &d, nil
}

// GetDecisionRev returns the current rev string for id, or "" if missing.
func GetDecisionRev(db *DB, id string) (string, error) {
	var rev string
	err := db.conn.QueryRow(`SELECT rev FROM decisions WHERE id = ?`, id).Scan(&rev)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("index: get rev %s: %w", id, err)
	}
	return rev, nil
}

// InsertDecision inserts d and its junction rows in a single transaction.
// contentSha is stored in content_sha256. The rev is set to a new ULID.
func InsertDecision(db *DB, d *core.Decision, contentSha string) error {
	rev := ulid.New()
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("index: insert decision begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.Exec(`
		INSERT INTO decisions(
			id, tree, slug, summary, description,
			status, priority, creator, assignee,
			recommended_summary, recommended_full, recommended_by,
			actual_choice, actual_choice_reason, is_recommended,
			out_of_scope_reason, schema_version, rev, content_sha256, deleted
		) VALUES (?,?,?,?,?, ?,?,?,?, ?,?,?, ?,?,?, ?,?,?,?,?)`,
		d.ID, d.Tree, d.Slug, d.Summary, d.Description,
		string(d.Status), string(d.Priority), d.Creator, d.Assignee,
		d.RecommendedSummary, d.RecommendedFull, d.RecommendedBy,
		d.ActualChoice, d.ActualChoiceReason, boolToInt(d.IsRecommended),
		d.OutOfScopeReason, d.SchemaVersion, rev, contentSha, 0,
	)
	if err != nil {
		return fmt.Errorf("index: insert decision %s: %w", d.ID, err)
	}

	if err := insertDeciders(tx, d.ID, d.DecidedBy); err != nil {
		return err
	}
	if err := insertTags(tx, d.ID, d.Tags); err != nil {
		return err
	}
	if err := insertRelationships(tx, d.ID, d.Tree, d.Relationships); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("index: insert decision commit: %w", err)
	}
	return nil
}

// UpdateDecision replaces all mutable columns for d and refreshes junction
// tables in a single transaction. newRev is stored in rev.
func UpdateDecision(db *DB, d *core.Decision, contentSha, newRev string) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("index: update decision begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.Exec(`
		UPDATE decisions SET
			slug=?, summary=?, description=?,
			status=?, priority=?, creator=?, assignee=?,
			recommended_summary=?, recommended_full=?, recommended_by=?,
			actual_choice=?, actual_choice_reason=?, is_recommended=?,
			out_of_scope_reason=?, schema_version=?, rev=?, content_sha256=?
		WHERE id=?`,
		d.Slug, d.Summary, d.Description,
		string(d.Status), string(d.Priority), d.Creator, d.Assignee,
		d.RecommendedSummary, d.RecommendedFull, d.RecommendedBy,
		d.ActualChoice, d.ActualChoiceReason, boolToInt(d.IsRecommended),
		d.OutOfScopeReason, d.SchemaVersion, newRev, contentSha,
		d.ID,
	)
	if err != nil {
		return fmt.Errorf("index: update decision %s: %w", d.ID, err)
	}

	// Refresh junction tables: delete-then-insert for simplicity.
	if _, err := tx.Exec(`DELETE FROM decision_deciders WHERE decision_id=?`, d.ID); err != nil {
		return fmt.Errorf("index: clear deciders %s: %w", d.ID, err)
	}
	if _, err := tx.Exec(`DELETE FROM decision_tags WHERE decision_id=?`, d.ID); err != nil {
		return fmt.Errorf("index: clear tags %s: %w", d.ID, err)
	}
	if _, err := tx.Exec(`DELETE FROM relationships WHERE source=?`, d.ID); err != nil {
		return fmt.Errorf("index: clear relationships %s: %w", d.ID, err)
	}

	if err := insertDeciders(tx, d.ID, d.DecidedBy); err != nil {
		return err
	}
	if err := insertTags(tx, d.ID, d.Tags); err != nil {
		return err
	}
	if err := insertRelationships(tx, d.ID, d.Tree, d.Relationships); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("index: update decision commit: %w", err)
	}
	return nil
}

// DeleteDecision sets deleted=1 for id (soft delete). The row stays
// queryable so replay and audit can still see it.
func DeleteDecision(db *DB, id string) error {
	_, err := db.conn.Exec(`UPDATE decisions SET deleted=1 WHERE id=?`, id)
	if err != nil {
		return fmt.Errorf("index: soft-delete decision %s: %w", id, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

func getDeciders(db *DB, id string) ([]string, error) {
	rows, err := db.conn.Query(
		`SELECT handle FROM decision_deciders WHERE decision_id=? ORDER BY handle`, id,
	)
	if err != nil {
		return nil, fmt.Errorf("index: get deciders %s: %w", id, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func getTags(db *DB, id string) ([]string, error) {
	rows, err := db.conn.Query(
		`SELECT tag FROM decision_tags WHERE decision_id=? ORDER BY tag`, id,
	)
	if err != nil {
		return nil, fmt.Errorf("index: get tags %s: %w", id, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func getRelationships(db *DB, id string) ([]core.Relationship, error) {
	rows, err := db.conn.Query(
		`SELECT target, type FROM relationships WHERE source=? ORDER BY target`, id,
	)
	if err != nil {
		return nil, fmt.Errorf("index: get relationships %s: %w", id, err)
	}
	defer rows.Close()
	var out []core.Relationship
	for rows.Next() {
		var r core.Relationship
		if err := rows.Scan(&r.Target, (*string)(&r.Type)); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func insertDeciders(tx *sql.Tx, id string, handles []string) error {
	for _, h := range handles {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO decision_deciders(decision_id, handle) VALUES(?,?)`, id, h,
		); err != nil {
			return fmt.Errorf("index: insert decider %s/%s: %w", id, h, err)
		}
	}
	return nil
}

func insertTags(tx *sql.Tx, id string, tags []string) error {
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO decision_tags(decision_id, tag) VALUES(?,?)`, id, t,
		); err != nil {
			return fmt.Errorf("index: insert tag %s/%s: %w", id, t, err)
		}
	}
	return nil
}

func insertRelationships(tx *sql.Tx, id, tree string, rels []core.Relationship) error {
	evID := ulid.New()
	for _, r := range rels {
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO relationships(source, target, type, tree, created_event_id)
			 VALUES(?,?,?,?,?)`,
			id, r.Target, string(r.Type), tree, evID,
		); err != nil {
			return fmt.Errorf("index: insert relationship %s->%s: %w", id, r.Target, err)
		}
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
