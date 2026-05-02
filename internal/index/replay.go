// Package index — replay engine for rebuilding the SQLite index from disk.
//
// Reindex is the entry-point for `dtree reindex` and the automatic
// startup check (runs when a .dirty marker is present). It rebuilds every
// dtree-owned table from the canonical on-disk YAML files and appends all
// audit events into the events table.
//
// Dirty-marker helpers (MarkDirty / IsDirty / ClearDirty) are thin wrappers
// around a sentinel file at .decisions/.dirty; mutating code paths create
// this marker before any crash-prone work and rely on Reindex to clear it.
package index

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/fsutil"
	"github.com/cgould/dtree/internal/storage"
	"github.com/cgould/dtree/internal/ulid"
)

// decisionsDir is the top-level data directory inside a repo root.
const decisionsDir = ".decisions"

// dirtyMarker is the sentinel filename inside .decisions/.
const dirtyMarker = ".dirty"

// indexLockFile is the filename of the per-index exclusive lock inside .decisions/.
const indexLockFile = ".index.lock"

// ---------------------------------------------------------------------------
// Dirty-marker helpers
// ---------------------------------------------------------------------------

// MarkDirty creates the .decisions/.dirty sentinel. Callers should invoke
// this before any crash-prone mutation and rely on Reindex to clear it.
func MarkDirty(repoRoot string) error {
	path := dirtyPath(repoRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("index: mark dirty mkdir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("index: mark dirty: %w", err)
	}
	return f.Close()
}

// IsDirty reports whether the .dirty sentinel exists.
func IsDirty(repoRoot string) (bool, error) {
	_, err := os.Stat(dirtyPath(repoRoot))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("index: is dirty: %w", err)
}

// ClearDirty removes the .dirty sentinel. Safe to call when the marker
// doesn't exist.
func ClearDirty(repoRoot string) error {
	err := os.Remove(dirtyPath(repoRoot))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("index: clear dirty: %w", err)
	}
	return nil
}

func dirtyPath(repoRoot string) string {
	return filepath.Join(repoRoot, decisionsDir, dirtyMarker)
}

// ---------------------------------------------------------------------------
// ReindexReport
// ---------------------------------------------------------------------------

// ReindexReport summarises a completed Reindex run.
type ReindexReport struct {
	Trees         int
	Actors        int
	Decisions     int
	Events        int
	Relationships int
	Warnings      []string
	Started       time.Time
	Ended         time.Time
}

// ---------------------------------------------------------------------------
// Reindex
// ---------------------------------------------------------------------------

// Reindex rebuilds the SQLite index from the on-disk YAML files and
// audit JSONL events. Acquires .decisions/.index.lock before mutating.
//
// Idempotent: rebuilds from scratch by clearing dtree-owned tables and
// re-importing. Safe to call any time.
//
// Detects fatal anomalies and aborts before mutation:
//   - Duplicate decision IDs across YAML files → returns error listing files.
//   - Tree directory present in trees.yaml but missing on disk → warning
//     logged and decision-loading is skipped for that slug.
//
// Reports progress via the optional progress callback (nil = silent).
func Reindex(repoRoot string, db *DB, progress func(string)) (*ReindexReport, error) {
	report := &ReindexReport{Started: time.Now()}
	emit := func(msg string) {
		if progress != nil {
			progress(msg)
		}
	}

	// ------------------------------------------------------------------
	// Step 1: acquire exclusive lock so no two rebuilds run at once.
	// ------------------------------------------------------------------
	lockPath := filepath.Join(repoRoot, decisionsDir, indexLockFile)
	emit("acquiring lock")
	lock, err := fsutil.AcquireLockBlocking(lockPath)
	if err != nil {
		return nil, fmt.Errorf("index: reindex: acquire lock: %w", err)
	}
	defer func() { _ = lock.Release() }()

	// ------------------------------------------------------------------
	// Step 2: pre-flight scan — load all YAML, detect duplicates, before
	// touching the database.
	// ------------------------------------------------------------------
	emit("scanning trees")
	treesPath := filepath.Join(repoRoot, decisionsDir, storage.TreesFileName)
	tf, err := readTreesFile(treesPath)
	if err != nil {
		return nil, fmt.Errorf("index: reindex: read trees: %w", err)
	}

	emit("scanning actors")
	actorsPath := filepath.Join(repoRoot, decisionsDir, storage.ActorsFileName)
	actors, err := readActorsFile(actorsPath)
	if err != nil {
		return nil, fmt.Errorf("index: reindex: read actors: %w", err)
	}

	emit("loading decisions")
	type treeDecisions struct {
		slug  string
		tree  *core.Tree
		items []decisionEntry
		warn  string // non-empty if tree dir was missing
	}

	// globalIDs maps ULID → file path for duplicate detection.
	globalIDs := make(map[string]string)

	var allTrees []treeDecisions
	for _, slug := range tf {
		treeDir := filepath.Join(repoRoot, decisionsDir, slug)
		info, statErr := os.Stat(treeDir)
		if statErr != nil || !info.IsDir() {
			msg := fmt.Sprintf("tree %q directory missing on disk — skipping", slug)
			log.Printf("index: reindex warning: %s", msg)
			allTrees = append(allTrees, treeDecisions{slug: slug, warn: msg})
			report.Warnings = append(report.Warnings, msg)
			continue
		}

		// Load tree metadata (missing tree.yaml is non-fatal; use defaults).
		treeMeta, treeMetaErr := storage.ReadTree(filepath.Join(treeDir, storage.TreeMetaFileName))
		if treeMetaErr != nil {
			treeMeta = &core.Tree{
				Slug:      slug,
				CreatedAt: time.Time{},
			}
		}
		treeMeta.Slug = slug // always canonical

		decisionsGlob := filepath.Join(treeDir, "decisions", "*.yaml")
		files, globErr := filepath.Glob(decisionsGlob)
		if globErr != nil {
			return nil, fmt.Errorf("index: reindex: glob decisions for tree %q: %w", slug, globErr)
		}

		var entries []decisionEntry
		for _, fpath := range files {
			// Skip soft-deleted directory.
			if isInsideDeletedDir(fpath) {
				continue
			}
			d, readErr := storage.ReadDecision(fpath)
			if readErr != nil {
				msg := fmt.Sprintf("tree %q: skip unreadable file %s: %v", slug, fpath, readErr)
				log.Printf("index: reindex warning: %s", msg)
				report.Warnings = append(report.Warnings, msg)
				continue
			}
			// Validate ULID.
			if parseErr := ulid.Parse(d.ID); parseErr != nil {
				msg := fmt.Sprintf("tree %q: skip invalid ID %q in %s: %v", slug, d.ID, fpath, parseErr)
				log.Printf("index: reindex warning: %s", msg)
				report.Warnings = append(report.Warnings, msg)
				continue
			}
			// Duplicate detection.
			if prev, dup := globalIDs[d.ID]; dup {
				return nil, fmt.Errorf(
					"index: reindex: duplicate decision ID %q found in:\n  %s\n  %s",
					d.ID, prev, fpath,
				)
			}
			globalIDs[d.ID] = fpath
			entries = append(entries, decisionEntry{decision: d, path: fpath})
		}
		allTrees = append(allTrees, treeDecisions{slug: slug, tree: treeMeta, items: entries})
	}

	// ------------------------------------------------------------------
	// Step 3: load audit events (before opening the transaction — reads
	// only, failures don't corrupt DB).
	// ------------------------------------------------------------------
	emit("loading audit events")
	events, err := audit.Read(repoRoot, audit.Filter{})
	if err != nil {
		return nil, fmt.Errorf("index: reindex: read audit events: %w", err)
	}

	// ------------------------------------------------------------------
	// Step 4: open transaction and truncate all dtree tables.
	// ------------------------------------------------------------------
	emit("clearing tables")
	tx, err := db.conn.Begin()
	if err != nil {
		return nil, fmt.Errorf("index: reindex: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, tbl := range []string{
		"events", "relationships", "decision_deciders", "decision_tags",
		"decisions", "actors", "trees",
	} {
		if _, execErr := tx.Exec("DELETE FROM " + tbl); execErr != nil {
			return nil, fmt.Errorf("index: reindex: truncate %s: %w", tbl, execErr)
		}
	}

	// ------------------------------------------------------------------
	// Step 5: insert trees.
	// ------------------------------------------------------------------
	emit("inserting trees")
	for _, td := range allTrees {
		t := td.tree
		if t == nil {
			// Missing on disk — still record slug with defaults.
			t = &core.Tree{Slug: td.slug}
		}
		createdAt := t.CreatedAt.UTC().Format(time.RFC3339)
		if t.CreatedAt.IsZero() {
			createdAt = "0001-01-01T00:00:00Z"
		}
		direction := t.Layout.Direction
		if direction == "" {
			direction = "TB"
		}
		_, err = tx.Exec(`
			INSERT INTO trees(slug, title, description, archived, created_at, layout_direction, schema_version)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			t.Slug, t.Title, t.Description, boolToInt(t.Archived),
			createdAt, direction, t.SchemaVersion,
		)
		if err != nil {
			return nil, fmt.Errorf("index: reindex: insert tree %q: %w", t.Slug, err)
		}
		report.Trees++
	}

	// ------------------------------------------------------------------
	// Step 6: insert actors.
	// ------------------------------------------------------------------
	emit("inserting actors")
	for _, a := range actors {
		_, err = tx.Exec(`
			INSERT INTO actors(handle, name, email, kind, active)
			VALUES (?, ?, ?, ?, ?)`,
			a.Handle, a.Name, a.Email, string(a.Kind), boolToInt(a.Active),
		)
		if err != nil {
			return nil, fmt.Errorf("index: reindex: insert actor %q: %w", a.Handle, err)
		}
		report.Actors++
	}

	// ------------------------------------------------------------------
	// Step 7: insert decisions (and their junction rows).
	// ------------------------------------------------------------------
	emit("inserting decisions")
	for _, td := range allTrees {
		for _, entry := range td.items {
			sha, shaErr := fsutil.Sha256File(entry.path)
			if shaErr != nil {
				return nil, fmt.Errorf("index: reindex: sha256 %s: %w", entry.path, shaErr)
			}
			d := entry.decision
			rev := ulid.New()
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
				d.OutOfScopeReason, d.SchemaVersion, rev, sha, 0,
			)
			if err != nil {
				return nil, fmt.Errorf("index: reindex: insert decision %s: %w", d.ID, err)
			}
			if err := insertDeciders(tx, d.ID, d.DecidedBy); err != nil {
				return nil, err
			}
			if err := insertTags(tx, d.ID, d.Tags); err != nil {
				return nil, err
			}
			if err := insertRelationships(tx, d.ID, d.Tree, d.Relationships); err != nil {
				return nil, err
			}
			report.Decisions++
			report.Relationships += len(d.Relationships)
		}
	}

	// ------------------------------------------------------------------
	// Step 8: insert audit events.
	// ------------------------------------------------------------------
	emit("inserting events")
	evStmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO events(event_id, ts, actor, action, kind, tree, target_id, payload_json, source_file)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
	)
	if err != nil {
		return nil, fmt.Errorf("index: reindex: prepare event stmt: %w", err)
	}
	defer evStmt.Close()

	for _, ev := range events {
		payloadBytes, marshalErr := json.Marshal(ev.Payload)
		if marshalErr != nil {
			return nil, fmt.Errorf("index: reindex: marshal event payload %s: %w", ev.EventID, marshalErr)
		}
		_, err = evStmt.Exec(
			ev.EventID,
			ev.Ts.UTC().Format(time.RFC3339Nano),
			ev.Actor,
			string(ev.Action),
			string(ev.Kind),
			ev.Tree,
			ev.ID,
			string(payloadBytes),
			"", // source_file not tracked at this layer
		)
		if err != nil {
			return nil, fmt.Errorf("index: reindex: insert event %s: %w", ev.EventID, err)
		}
		report.Events++
	}

	// ------------------------------------------------------------------
	// Step 9: commit.
	// ------------------------------------------------------------------
	emit("committing")
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("index: reindex: commit: %w", err)
	}

	// ------------------------------------------------------------------
	// Step 10: clear dirty marker on success.
	// ------------------------------------------------------------------
	if err := ClearDirty(repoRoot); err != nil {
		// Non-fatal: log and continue; the index is valid.
		log.Printf("index: reindex warning: clear dirty: %v", err)
	}

	report.Ended = time.Now()
	emit("done")
	return report, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// decisionEntry pairs a parsed Decision with its file path (for sha256).
type decisionEntry struct {
	decision *core.Decision
	path     string
}

// readTreesFile reads trees.yaml and returns the slug list. Returns nil
// (not an error) when the file doesn't exist.
func readTreesFile(path string) ([]string, error) {
	tf, err := storage.ReadTrees(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return tf.Trees, nil
}

// readActorsFile reads actors.yaml and returns the actor list. Returns nil
// (not an error) when the file doesn't exist.
func readActorsFile(path string) ([]core.Actor, error) {
	af, err := storage.ReadActors(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return af.Actors, nil
}

// isInsideDeletedDir reports whether path contains a ".deleted" path component.
func isInsideDeletedDir(path string) bool {
	for _, seg := range filepath.SplitList(filepath.ToSlash(path)) {
		if seg == ".deleted" {
			return true
		}
	}
	// Fallback: check raw string (covers Unix paths).
	base := filepath.Dir(path)
	return filepath.Base(base) == ".deleted"
}

