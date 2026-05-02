// Package sync detects and reconciles external edits to YAML decision files —
// changes made by editors, IDEs, or scripts outside the dtree CLI. It
// compares on-disk SHA-256 hashes against the value stored in the SQLite
// index, and offers three actions for each mismatch: record the change as an
// audit event, revert the disk to match the index, or abort without mutating
// anything.
//
// Pure reads (Scan) never mutate state. Reconcile is the only mutating entry
// point.
package sync

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/fsutil"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/storage"
	"github.com/cgould/dtree/internal/ulid"
)

// MismatchKind discriminates how a decision is out of sync.
type MismatchKind int

const (
	// MismatchEdit — file exists on disk but content_sha256 differs from index.
	MismatchEdit MismatchKind = iota
	// MismatchCreate — file exists on disk but the index has no row for it.
	MismatchCreate
	// MismatchDelete — the index has a row (not soft-deleted) but the file is
	// missing from disk.
	MismatchDelete
)

// Mismatch describes one out-of-sync decision file.
type Mismatch struct {
	DecisionID string // ULID; "" when MismatchKind == MismatchCreate and ID can't be read
	Tree       string
	Path       string // absolute path to the YAML file
	Kind       MismatchKind
	DiskSha    string // sha256 of disk content; "" if file missing
	IndexSha   string // sha256 recorded in index; "" if no index row
}

// Action is what to do with each Mismatch in Reconcile.
type Action int

const (
	// ActionRecord — emit an audit event reflecting the external change and
	// update the index to match disk.
	ActionRecord Action = iota
	// ActionRevert — restore disk to match the index's last known state.
	ActionRevert
	// ActionAbort — no-op; return nil without mutating anything.
	ActionAbort
)

// Scan walks .decisions/<tree>/decisions/*.yaml for every tree known to the
// index and returns all mismatches. It is a pure read — nothing is mutated.
//
// It also checks every non-soft-deleted index row to ensure its file is
// present, reporting MismatchDelete when a file is absent.
//
// Files under .deleted/ sub-directories are silently skipped.
func Scan(repoRoot string, db *index.DB) ([]Mismatch, error) {
	decisionsBase := filepath.Join(repoRoot, ".decisions")

	trees, err := listTrees(db)
	if err != nil {
		return nil, err
	}

	// Collect index rows so we can detect deletes.
	indexRows, err := allIndexRows(db)
	if err != nil {
		return nil, err
	}
	// seenOnDisk tracks which IDs we found a file for.
	seenOnDisk := make(map[string]bool, len(indexRows))

	var mismatches []Mismatch

	for _, tree := range trees {
		decDir := filepath.Join(decisionsBase, tree, "decisions")

		entries, err := os.ReadDir(decDir)
		if err != nil {
			if os.IsNotExist(err) {
				// Tree dir doesn't exist on disk yet — any index rows
				// for this tree will be caught as MismatchDelete below.
				continue
			}
			return nil, fmt.Errorf("sync: read dir %s: %w", decDir, err)
		}

		for _, e := range entries {
			if e.IsDir() {
				// Skip .deleted/ and any other sub-directories.
				continue
			}
			name := e.Name()
			if !strings.HasSuffix(name, ".yaml") {
				continue
			}

			path := filepath.Join(decDir, name)

			diskSha, err := fsutil.Sha256File(path)
			if err != nil {
				return nil, fmt.Errorf("sync: hash %s: %w", path, err)
			}

			// Extract the decision ID from the filename (<ULID>-<slug>.yaml).
			id := idFromFilename(name)

			row, inIndex := indexRows[id]
			if !inIndex {
				// File exists but no index row → MismatchCreate.
				mismatches = append(mismatches, Mismatch{
					DecisionID: id, // may be "" if filename doesn't parse
					Tree:       tree,
					Path:       path,
					Kind:       MismatchCreate,
					DiskSha:    diskSha,
					IndexSha:   "",
				})
				continue
			}

			seenOnDisk[id] = true

			if row.contentSha != diskSha {
				// Content differs → MismatchEdit.
				mismatches = append(mismatches, Mismatch{
					DecisionID: id,
					Tree:       tree,
					Path:       path,
					Kind:       MismatchEdit,
					DiskSha:    diskSha,
					IndexSha:   row.contentSha,
				})
			}
			// If sha matches: in sync — no mismatch.
		}
	}

	// Check for MismatchDelete: index row without a disk file.
	for id, row := range indexRows {
		if seenOnDisk[id] {
			continue
		}
		// The file is missing. Build expected path from index slug.
		treeDir := filepath.Join(decisionsBase, row.tree)
		expectedPath := storage.DecisionPath(treeDir, id, row.slug)

		mismatches = append(mismatches, Mismatch{
			DecisionID: id,
			Tree:       row.tree,
			Path:       expectedPath,
			Kind:       MismatchDelete,
			DiskSha:    "",
			IndexSha:   row.contentSha,
		})
	}

	return mismatches, nil
}

// Reconcile applies the chosen action to a single mismatch.
//
//   - ActionRecord: emit an audit event, update (or insert/soft-delete) the
//     index row to reflect the on-disk state.
//   - ActionRevert: restore disk from the index's last known view of the
//     decision; for MismatchCreate this means removing the file.
//   - ActionAbort: no-op, returns nil.
//
// actorHandle is the "actor" field on emitted audit events.
func Reconcile(repoRoot string, db *index.DB, m Mismatch, action Action, actorHandle string) error {
	switch action {
	case ActionAbort:
		return nil

	case ActionRecord:
		return reconcileRecord(repoRoot, db, m, actorHandle)

	case ActionRevert:
		return reconcileRevert(repoRoot, db, m)

	default:
		return fmt.Errorf("sync: unknown action %d", action)
	}
}

// ---------------------------------------------------------------------------
// reconcileRecord
// ---------------------------------------------------------------------------

func reconcileRecord(repoRoot string, db *index.DB, m Mismatch, actor string) error {
	switch m.Kind {
	case MismatchEdit:
		return recordEdit(repoRoot, db, m, actor)
	case MismatchCreate:
		return recordCreate(repoRoot, db, m, actor)
	case MismatchDelete:
		return recordDelete(repoRoot, db, m, actor)
	default:
		return fmt.Errorf("sync: unknown mismatch kind %d", m.Kind)
	}
}

func recordEdit(repoRoot string, db *index.DB, m Mismatch, actor string) error {
	// Load current disk state.
	diskDecision, err := storage.ReadDecision(m.Path)
	if err != nil {
		return fmt.Errorf("sync: read disk decision %s: %w", m.Path, err)
	}

	// Load previous index state for before diff.
	prevDecision, err := index.GetDecision(db, m.DecisionID)
	if err != nil {
		return fmt.Errorf("sync: load index decision %s: %w", m.DecisionID, err)
	}

	var before, after map[string]any
	if prevDecision != nil {
		before = decisionToMap(prevDecision)
	}
	after = decisionToMap(diskDecision)

	// Keep only changed fields in before/after (PRD: update payload has delta).
	beforeDelta, afterDelta := diffMaps(before, after)

	newRev := ulid.New()
	ev := core.Event{
		EventID: ulid.New(),
		V:       core.SchemaVersion,
		Ts:      time.Now().UTC(),
		Actor:   actor,
		Action:  core.ActionExternalEdit,
		Kind:    core.KindDecision,
		Tree:    m.Tree,
		ID:      m.DecisionID,
		Payload: core.EventPayload{Before: beforeDelta, After: afterDelta},
	}
	if err := audit.Append(repoRoot, ev); err != nil {
		return fmt.Errorf("sync: append audit event: %w", err)
	}

	// Update index row.
	diskDecision.Tree = m.Tree
	if err := index.UpdateDecision(db, diskDecision, m.DiskSha, newRev); err != nil {
		return fmt.Errorf("sync: update index for edit %s: %w", m.DecisionID, err)
	}
	return nil
}

func recordCreate(repoRoot string, db *index.DB, m Mismatch, actor string) error {
	diskDecision, err := storage.ReadDecision(m.Path)
	if err != nil {
		return fmt.Errorf("sync: read disk decision %s: %w", m.Path, err)
	}
	diskDecision.Tree = m.Tree

	after := decisionToMap(diskDecision)

	ev := core.Event{
		EventID: ulid.New(),
		V:       core.SchemaVersion,
		Ts:      time.Now().UTC(),
		Actor:   actor,
		Action:  core.ActionExternalCreate,
		Kind:    core.KindDecision,
		Tree:    m.Tree,
		ID:      diskDecision.ID,
		Payload: core.EventPayload{After: after},
	}
	if err := audit.Append(repoRoot, ev); err != nil {
		return fmt.Errorf("sync: append audit event: %w", err)
	}

	// Ensure the tree row exists in the index (best-effort; tree may already be there).
	ensureTreeRow(db, m.Tree)

	if err := index.InsertDecision(db, diskDecision, m.DiskSha); err != nil {
		return fmt.Errorf("sync: insert index for create %s: %w", diskDecision.ID, err)
	}
	return nil
}

func recordDelete(repoRoot string, db *index.DB, m Mismatch, actor string) error {
	prevDecision, err := index.GetDecision(db, m.DecisionID)
	if err != nil {
		return fmt.Errorf("sync: load index decision %s: %w", m.DecisionID, err)
	}

	var before map[string]any
	if prevDecision != nil {
		before = decisionToMap(prevDecision)
	}

	ev := core.Event{
		EventID: ulid.New(),
		V:       core.SchemaVersion,
		Ts:      time.Now().UTC(),
		Actor:   actor,
		Action:  core.ActionExternalDelete,
		Kind:    core.KindDecision,
		Tree:    m.Tree,
		ID:      m.DecisionID,
		Payload: core.EventPayload{Before: before},
	}
	if err := audit.Append(repoRoot, ev); err != nil {
		return fmt.Errorf("sync: append audit event: %w", err)
	}

	if err := index.DeleteDecision(db, m.DecisionID); err != nil {
		return fmt.Errorf("sync: soft-delete index row %s: %w", m.DecisionID, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// reconcileRevert
// ---------------------------------------------------------------------------

func reconcileRevert(_ string, db *index.DB, m Mismatch) error {
	switch m.Kind {
	case MismatchEdit:
		// Restore disk file from index's known state.
		d, err := index.GetDecision(db, m.DecisionID)
		if err != nil {
			return fmt.Errorf("sync: load index decision for revert %s: %w", m.DecisionID, err)
		}
		if d == nil {
			return fmt.Errorf("sync: no index row to revert to for %s", m.DecisionID)
		}
		return storage.WriteDecision(m.Path, d)

	case MismatchCreate:
		// File was created outside the CLI — discard it.
		if err := os.Remove(m.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("sync: remove external file %s: %w", m.Path, err)
		}
		return nil

	case MismatchDelete:
		// File was deleted outside the CLI — restore from index.
		d, err := index.GetDecision(db, m.DecisionID)
		if err != nil {
			return fmt.Errorf("sync: load index decision for restore %s: %w", m.DecisionID, err)
		}
		if d == nil {
			return fmt.Errorf("sync: no index row to restore for %s", m.DecisionID)
		}
		return storage.WriteDecision(m.Path, d)

	default:
		return fmt.Errorf("sync: unknown mismatch kind %d", m.Kind)
	}
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// indexRow is a lightweight representation of a decisions table row used
// during scan.
type indexRow struct {
	id         string
	tree       string
	slug       string
	contentSha string
	deleted    bool
}

// listTrees returns all non-archived tree slugs from the index.
func listTrees(db *index.DB) ([]string, error) {
	rows, err := db.Conn().Query(`SELECT slug FROM trees WHERE archived=0 ORDER BY slug`)
	if err != nil {
		return nil, fmt.Errorf("sync: list trees: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// allIndexRows loads all non-soft-deleted decision rows from the index
// (only id, tree, slug, content_sha256). Used to detect MismatchDelete.
func allIndexRows(db *index.DB) (map[string]indexRow, error) {
	rows, err := db.Conn().Query(
		`SELECT id, tree, slug, content_sha256 FROM decisions WHERE deleted=0`,
	)
	if err != nil {
		return nil, fmt.Errorf("sync: load index rows: %w", err)
	}
	defer rows.Close()
	out := make(map[string]indexRow)
	for rows.Next() {
		var r indexRow
		if err := rows.Scan(&r.id, &r.tree, &r.slug, &r.contentSha); err != nil {
			return nil, err
		}
		out[r.id] = r
	}
	return out, rows.Err()
}

// idFromFilename extracts the ULID prefix from a decision filename
// (<ULID>-<slug>.yaml). Returns "" when the name doesn't match.
func idFromFilename(name string) string {
	name = strings.TrimSuffix(name, ".yaml")
	if len(name) < ulid.Length {
		return ""
	}
	candidate := name[:ulid.Length]
	if ulid.Parse(candidate) != nil {
		return ""
	}
	return candidate
}

// decisionToMap converts a Decision to map[string]any using the JSON
// serialisation path so field names match the audit event convention.
func decisionToMap(d *core.Decision) map[string]any {
	b, err := json.Marshal(d)
	if err != nil {
		// Should never fail for a well-formed Decision.
		return map[string]any{"id": d.ID}
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

// diffMaps returns (before delta, after delta) containing only keys where
// the values differ between a and b. Keys present in only one map are included.
func diffMaps(a, b map[string]any) (map[string]any, map[string]any) {
	before := make(map[string]any)
	after := make(map[string]any)

	keys := make(map[string]bool)
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}

	for k := range keys {
		av, aok := a[k]
		bv, bok := b[k]
		if !aok {
			after[k] = bv
			continue
		}
		if !bok {
			before[k] = av
			continue
		}
		// Both present: compare via JSON re-serialisation for value equality.
		aj, _ := json.Marshal(av)
		bj, _ := json.Marshal(bv)
		if string(aj) != string(bj) {
			before[k] = av
			after[k] = bv
		}
	}

	if len(before) == 0 {
		before = nil
	}
	if len(after) == 0 {
		after = nil
	}
	return before, after
}

// ensureTreeRow inserts a minimal tree row if it doesn't already exist.
// Used when recording an external create where the tree may not be indexed.
func ensureTreeRow(db *index.DB, slug string) {
	_, _ = db.Conn().Exec(
		`INSERT OR IGNORE INTO trees(slug, created_at) VALUES(?, ?)`,
		slug, time.Now().UTC().Format(time.RFC3339),
	)
}
