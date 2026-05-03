// Package mcp — handler implementations for MCP tools.
//
// Each function in this file is a pure operation against the underlying
// storage / index / audit subsystems; tools.go wires them into the MCP
// transport. Handlers are written so they can be exercised directly from
// tests without spinning up the wire layer.
//
// Identity for mutations is the single fixed actor passed in by tools.go
// (Server.cfg.Actor). All mutating handlers append a corresponding
// audit event on success. Errors are returned plain — the wire layer is
// responsible for converting them to MCP error results.
package mcp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/concurrency"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/fsutil"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/storage"
	"github.com/cgould/dtree/internal/ulid"
	"github.com/cgould/dtree/internal/validate"
)

// treeSlugRE mirrors the validation pattern used elsewhere in the codebase.
var treeSlugRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// ---------------------------------------------------------------------------
// Tree CRUD
// ---------------------------------------------------------------------------

// handleGetTree loads a single tree from the index by slug.
func handleGetTree(db *index.DB, slug string) (*core.Tree, error) {
	if db == nil {
		return nil, errors.New("get_tree: index not available")
	}
	if slug == "" {
		return nil, errors.New("get_tree: tree is required")
	}
	t, err := readTreeRow(db, slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("get_tree: tree not found: %s", slug)
		}
		return nil, fmt.Errorf("get_tree: %w", err)
	}
	return t, nil
}

// handleCreateTree creates a new tree and writes its metadata, registry
// entry, audit event, and index row. `name` maps to core.Tree.Title.
func handleCreateTree(repoRoot string, db *index.DB, actor, slug, name, description string) (*core.Tree, error) {
	if db == nil {
		return nil, errors.New("create_tree: index not available")
	}
	if !treeSlugRE.MatchString(slug) {
		return nil, fmt.Errorf("create_tree: invalid slug %q (must match %s)", slug, treeSlugRE.String())
	}

	exists, err := treeExistsRow(db, slug)
	if err != nil {
		return nil, fmt.Errorf("create_tree: check existence: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("create_tree: tree already exists: %s", slug)
	}

	treeDir := filepath.Join(repoRoot, ".decisions", slug)
	for _, dir := range []string{
		filepath.Join(treeDir, "decisions"),
		filepath.Join(treeDir, "audit"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create_tree: mkdir %s: %w", dir, err)
		}
	}

	t := &core.Tree{
		Slug:          slug,
		SchemaVersion: core.SchemaVersion,
		Title:         name,
		Description:   description,
		CreatedAt:     time.Now().UTC(),
	}
	t.Layout.Direction = "TB"

	if err := storage.WriteTree(filepath.Join(treeDir, storage.TreeMetaFileName), t); err != nil {
		return nil, fmt.Errorf("create_tree: write tree.yaml: %w", err)
	}

	treesPath := filepath.Join(repoRoot, ".decisions", storage.TreesFileName)
	tf, err := storage.ReadTrees(treesPath)
	if err != nil {
		tf = &storage.TreesFile{}
	}
	tf.Trees = append(tf.Trees, slug)
	sort.Strings(tf.Trees)
	if err := storage.WriteTrees(treesPath, tf); err != nil {
		return nil, fmt.Errorf("create_tree: write trees.yaml: %w", err)
	}

	if err := insertTreeRow(db, t); err != nil {
		return nil, fmt.Errorf("create_tree: insert index row: %w", err)
	}

	_ = audit.Append(repoRoot, core.Event{
		Actor:  actor,
		Action: core.ActionTreeCreate,
		Kind:   core.KindTree,
		ID:     slug,
		Payload: core.EventPayload{
			After: map[string]any{
				"slug":        t.Slug,
				"title":       t.Title,
				"description": t.Description,
				"archived":    t.Archived,
				"created_at":  t.CreatedAt.Format(time.RFC3339),
			},
		},
	})

	return t, nil
}

// handleUpdateTree applies optional name / description changes. nil-pointer
// arguments mean "do not modify".
func handleUpdateTree(repoRoot string, db *index.DB, actor, slug string, name, description *string) (*core.Tree, error) {
	if db == nil {
		return nil, errors.New("update_tree: index not available")
	}
	t, err := readTreeRow(db, slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("update_tree: tree not found: %s", slug)
		}
		return nil, fmt.Errorf("update_tree: %w", err)
	}

	before := map[string]any{
		"title":       t.Title,
		"description": t.Description,
	}

	if name != nil {
		t.Title = *name
	}
	if description != nil {
		t.Description = *description
	}

	if err := storage.WriteTree(
		filepath.Join(repoRoot, ".decisions", slug, storage.TreeMetaFileName), t,
	); err != nil {
		return nil, fmt.Errorf("update_tree: write tree.yaml: %w", err)
	}

	dir := t.Layout.Direction
	if dir == "" {
		dir = "TB"
	}
	if _, err := db.Conn().Exec(
		`UPDATE trees SET title=?, description=?, layout_direction=? WHERE slug=?`,
		t.Title, t.Description, dir, slug,
	); err != nil {
		return nil, fmt.Errorf("update_tree: update index: %w", err)
	}

	_ = audit.Append(repoRoot, core.Event{
		Actor:  actor,
		Action: core.ActionUpdate,
		Kind:   core.KindTree,
		ID:     slug,
		Payload: core.EventPayload{
			Before: before,
			After: map[string]any{
				"title":       t.Title,
				"description": t.Description,
			},
		},
	})

	return t, nil
}

// handleArchiveTree sets the tree's archived flag.
func handleArchiveTree(repoRoot string, db *index.DB, actor, slug string, archive bool) (*core.Tree, error) {
	if db == nil {
		return nil, errors.New("archive_tree: index not available")
	}
	t, err := readTreeRow(db, slug)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("archive_tree: tree not found: %s", slug)
		}
		return nil, fmt.Errorf("archive_tree: %w", err)
	}
	t.Archived = archive

	metaPath := filepath.Join(repoRoot, ".decisions", slug, storage.TreeMetaFileName)
	if err := storage.WriteTree(metaPath, t); err != nil {
		return nil, fmt.Errorf("archive_tree: write tree.yaml: %w", err)
	}
	flag := 0
	if archive {
		flag = 1
	}
	if _, err := db.Conn().Exec(
		`UPDATE trees SET archived=? WHERE slug=?`, flag, slug,
	); err != nil {
		return nil, fmt.Errorf("archive_tree: update index: %w", err)
	}

	_ = audit.Append(repoRoot, core.Event{
		Actor:  actor,
		Action: core.ActionTreeArchive,
		Kind:   core.KindTree,
		ID:     slug,
		Payload: core.EventPayload{
			After: map[string]any{"slug": slug, "archived": archive},
		},
	})

	return t, nil
}

// ---------------------------------------------------------------------------
// Decision CRUD / list / get
// ---------------------------------------------------------------------------

// listDecisionsArgs gathers the filter / pagination args for handleListDecisions.
type listDecisionsArgs struct {
	Tree     string
	Status   string
	Priority string
	Tag      string
	Search   string
	Limit    int
	Cursor   string
}

// listDecisionsResult is the response envelope for handleListDecisions.
type listDecisionsResult struct {
	Items      []*core.Decision `json:"items"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

// handleListDecisions queries the index with optional status / priority /
// tag / FTS search filters, returning a page of decisions plus a cursor
// for the next page when more results exist.
func handleListDecisions(db *index.DB, args listDecisionsArgs) (*listDecisionsResult, error) {
	if db == nil {
		return nil, errors.New("list_decisions: index not available")
	}
	if args.Tree == "" {
		return nil, errors.New("list_decisions: tree is required")
	}
	exists, err := treeExistsRow(db, args.Tree)
	if err != nil {
		return nil, fmt.Errorf("list_decisions: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("list_decisions: tree not found: %s", args.Tree)
	}

	limit := args.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}

	var (
		clauses = []string{"d.tree = ?", "d.deleted = 0"}
		sqlArgs = []any{args.Tree}
	)
	if args.Status != "" {
		clauses = append(clauses, "d.status = ?")
		sqlArgs = append(sqlArgs, args.Status)
	}
	if args.Priority != "" {
		clauses = append(clauses, "d.priority = ?")
		sqlArgs = append(sqlArgs, args.Priority)
	}
	if args.Tag != "" {
		clauses = append(clauses, "EXISTS (SELECT 1 FROM decision_tags dt WHERE dt.decision_id = d.id AND dt.tag = ?)")
		sqlArgs = append(sqlArgs, args.Tag)
	}
	if args.Search != "" {
		clauses = append(clauses, "d.rowid IN (SELECT rowid FROM decisions_fts WHERE decisions_fts MATCH ?)")
		sqlArgs = append(sqlArgs, args.Search)
	}
	if args.Cursor != "" {
		clauses = append(clauses, "d.id > ?")
		sqlArgs = append(sqlArgs, args.Cursor)
	}
	sqlArgs = append(sqlArgs, limit+1)

	q := fmt.Sprintf(
		`SELECT d.id FROM decisions d WHERE %s ORDER BY d.id ASC LIMIT ?`,
		strings.Join(clauses, " AND "),
	)
	rows, err := db.Conn().Query(q, sqlArgs...)
	if err != nil {
		return nil, fmt.Errorf("list_decisions: query: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("list_decisions: scan: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list_decisions: iterate: %w", err)
	}

	out := &listDecisionsResult{Items: []*core.Decision{}}
	if len(ids) > limit {
		out.NextCursor = ids[limit-1]
		ids = ids[:limit]
	}
	for _, id := range ids {
		d, err := index.GetDecision(db, id)
		if err != nil {
			return nil, fmt.Errorf("list_decisions: get %s: %w", id, err)
		}
		if d != nil {
			out.Items = append(out.Items, d)
		}
	}
	return out, nil
}

// handleGetDecision resolves id (full ULID or ≥4-char prefix) within tree
// and returns the decision.
func handleGetDecision(db *index.DB, tree, idOrPrefix string) (*core.Decision, error) {
	if db == nil {
		return nil, errors.New("get_decision: index not available")
	}
	id, err := resolveDecisionID(db, tree, idOrPrefix)
	if err != nil {
		return nil, fmt.Errorf("get_decision: %w", err)
	}
	d, err := index.GetDecision(db, id)
	if err != nil {
		return nil, fmt.Errorf("get_decision: %w", err)
	}
	if d == nil {
		return nil, fmt.Errorf("get_decision: not found: %s", idOrPrefix)
	}
	return d, nil
}

// createDecisionArgs holds the optional / required fields for creating a decision.
type createDecisionArgs struct {
	Summary     string
	Description string
	Priority    string
	Tags        []string
	Assignee    string
}

// handleCreateDecision creates a new decision with a fresh ULID, writes its
// YAML, indexes it, and emits a create event.
func handleCreateDecision(repoRoot string, db *index.DB, actor, tree string, in createDecisionArgs) (*core.Decision, error) {
	if db == nil {
		return nil, errors.New("create_decision: index not available")
	}
	if exists, err := treeExistsRow(db, tree); err != nil {
		return nil, fmt.Errorf("create_decision: %w", err)
	} else if !exists {
		return nil, fmt.Errorf("create_decision: tree not found: %s", tree)
	}
	if in.Summary == "" {
		return nil, errors.New("create_decision: summary is required")
	}

	d := core.Decision{
		ID:            ulid.New(),
		Tree:          tree,
		Summary:       in.Summary,
		Description:   in.Description,
		Status:        core.StatusProposed,
		Priority:      core.Priority(in.Priority),
		Tags:          in.Tags,
		Creator:       actor,
		Assignee:      in.Assignee,
		SchemaVersion: core.SchemaVersion,
	}
	if d.Priority == "" {
		d.Priority = core.PriorityMedium
	}
	d.Slug = storage.SlugFromSummary(d.Summary)

	if err := validate.Decision(&d); err != nil {
		return nil, fmt.Errorf("create_decision: validation: %w", err)
	}

	path := storage.DecisionPath(filepath.Join(repoRoot, ".decisions", tree), d.ID, d.Slug)
	if err := storage.WriteDecision(path, &d); err != nil {
		return nil, fmt.Errorf("create_decision: write yaml: %w", err)
	}
	sha, err := fsutil.Sha256File(path)
	if err != nil {
		return nil, fmt.Errorf("create_decision: hash: %w", err)
	}
	if err := index.InsertDecision(db, &d, sha); err != nil {
		return nil, fmt.Errorf("create_decision: index: %w", err)
	}

	stored, err := index.GetDecision(db, d.ID)
	if err != nil || stored == nil {
		return nil, fmt.Errorf("create_decision: re-read: %w", err)
	}

	_ = audit.Append(repoRoot, core.Event{
		Actor:  actor,
		Action: core.ActionCreate,
		Kind:   core.KindDecision,
		Tree:   tree,
		ID:     stored.ID,
		Payload: core.EventPayload{
			After: decisionAuditMap(stored),
		},
	})
	return stored, nil
}

// updateDecisionFields is the patch envelope. Pointer fields distinguish
// "not provided" from "set to zero". Tags is treated as "absent => no change,
// non-nil empty slice => clear", matching the HTTP PATCH semantics.
type updateDecisionFields struct {
	Summary            *string
	Description        *string
	Priority           *string
	TagsSet            bool
	Tags               []string
	Assignee           *string
	RecommendedSummary *string
	RecommendedFull    *string
	RecommendedBy      *string
}

// handleUpdateDecision applies the field patch to the named decision. When
// expectedRev is non-empty it is enforced via the index optimistic-lock path.
func handleUpdateDecision(repoRoot string, db *index.DB, actor, tree, idOrPrefix string, fields updateDecisionFields, expectedRev string) (*core.Decision, error) {
	if db == nil {
		return nil, errors.New("update_decision: index not available")
	}
	id, err := resolveDecisionID(db, tree, idOrPrefix)
	if err != nil {
		return nil, fmt.Errorf("update_decision: %w", err)
	}
	d, err := index.GetDecision(db, id)
	if err != nil || d == nil {
		return nil, fmt.Errorf("update_decision: load: %w", err)
	}
	before := decisionAuditMap(d)

	if fields.Summary != nil {
		d.Summary = *fields.Summary
		d.Slug = storage.SlugFromSummary(d.Summary)
	}
	if fields.Description != nil {
		d.Description = *fields.Description
	}
	if fields.Priority != nil {
		d.Priority = core.Priority(*fields.Priority)
	}
	if fields.TagsSet {
		d.Tags = fields.Tags
	}
	if fields.Assignee != nil {
		d.Assignee = *fields.Assignee
	}
	if fields.RecommendedSummary != nil {
		d.RecommendedSummary = *fields.RecommendedSummary
	}
	if fields.RecommendedFull != nil {
		d.RecommendedFull = *fields.RecommendedFull
	}
	if fields.RecommendedBy != nil {
		d.RecommendedBy = *fields.RecommendedBy
	}

	if err := writeAndIndex(repoRoot, db, d, expectedRev); err != nil {
		return nil, fmt.Errorf("update_decision: %w", err)
	}

	_ = audit.Append(repoRoot, core.Event{
		Actor:  actor,
		Action: core.ActionUpdate,
		Kind:   core.KindDecision,
		Tree:   tree,
		ID:     d.ID,
		Payload: core.EventPayload{
			Before: before,
			After:  decisionAuditMap(d),
		},
	})
	return d, nil
}

// handleDeleteDecision soft- or hard-deletes a decision. hard requires there
// to be no incoming references; pass force=true to override the soft-delete
// "rev" check (treated as expected_rev == "").
func handleDeleteDecision(repoRoot string, db *index.DB, actor, tree, idOrPrefix string, hard, force bool) error {
	if db == nil {
		return errors.New("delete_decision: index not available")
	}
	id, err := resolveDecisionID(db, tree, idOrPrefix)
	if err != nil {
		return fmt.Errorf("delete_decision: %w", err)
	}
	d, err := index.GetDecision(db, id)
	if err != nil || d == nil {
		return fmt.Errorf("delete_decision: load: %w", err)
	}

	if hard {
		var n int
		if err := db.Conn().QueryRow(
			`SELECT COUNT(*) FROM relationships WHERE target=?`, d.ID,
		).Scan(&n); err != nil {
			return fmt.Errorf("delete_decision: count refs: %w", err)
		}
		if n > 0 && !force {
			return fmt.Errorf("delete_decision: hard delete refused: %d incoming reference(s)", n)
		}
	}

	filePath := findDecisionFile(repoRoot, d)
	if hard {
		if _, err := db.Conn().Exec(`DELETE FROM decisions WHERE id=?`, d.ID); err != nil {
			return fmt.Errorf("delete_decision: index: %w", err)
		}
		if filePath != "" {
			_ = os.Remove(filePath)
		}
	} else {
		if filePath != "" {
			deletedDir := filepath.Join(repoRoot, ".decisions", ".deleted", tree)
			if err := os.MkdirAll(deletedDir, 0o755); err == nil {
				dst := filepath.Join(deletedDir, filepath.Base(filePath))
				if rerr := os.Rename(filePath, dst); rerr != nil {
					if data, rrerr := os.ReadFile(filePath); rrerr == nil {
						_ = os.WriteFile(dst, data, 0o644)
						_ = os.Remove(filePath)
					}
				}
			}
		}
		if err := index.DeleteDecisionWithExpectedRev(db, d.ID, ""); err != nil {
			if c, ok := concurrency.AsConflict(err); ok && !force {
				return fmt.Errorf("delete_decision: rev conflict: expected %q, current %q", c.ExpectedRev, c.ActualRev)
			}
			if !force {
				return fmt.Errorf("delete_decision: %w", err)
			}
		}
	}

	_ = audit.Append(repoRoot, core.Event{
		Actor:  actor,
		Action: core.ActionDelete,
		Kind:   core.KindDecision,
		Tree:   tree,
		ID:     d.ID,
		Payload: core.EventPayload{
			Before: decisionAuditMap(d),
			After:  map[string]any{"hard": hard},
		},
	})
	return nil
}

// ---------------------------------------------------------------------------
// Decision lifecycle: decide / undecide / scope-out / supersede / restore
// ---------------------------------------------------------------------------

// handleDecideDecision marks a decision as decided. by defaults to [actor]
// when empty.
func handleDecideDecision(repoRoot string, db *index.DB, actor, tree, idOrPrefix, choice, reason string, by []string, isRecommended bool) (*core.Decision, error) {
	if db == nil {
		return nil, errors.New("decide_decision: index not available")
	}
	if choice == "" {
		return nil, errors.New("decide_decision: choice is required")
	}
	id, err := resolveDecisionID(db, tree, idOrPrefix)
	if err != nil {
		return nil, fmt.Errorf("decide_decision: %w", err)
	}
	d, err := index.GetDecision(db, id)
	if err != nil || d == nil {
		return nil, fmt.Errorf("decide_decision: load: %w", err)
	}
	before := decisionAuditMap(d)

	d.Status = core.StatusDecided
	d.ActualChoice = choice
	d.ActualChoiceReason = reason
	if len(by) > 0 {
		d.DecidedBy = by
	} else {
		d.DecidedBy = []string{actor}
	}
	d.IsRecommended = isRecommended

	if err := writeAndIndex(repoRoot, db, d, ""); err != nil {
		return nil, fmt.Errorf("decide_decision: %w", err)
	}
	_ = audit.Append(repoRoot, core.Event{
		Actor: actor, Action: core.ActionDecide, Kind: core.KindDecision,
		Tree: tree, ID: d.ID,
		Payload: core.EventPayload{Before: before, After: map[string]any{
			"actual_choice":        d.ActualChoice,
			"actual_choice_reason": d.ActualChoiceReason,
			"decided_by":           d.DecidedBy,
			"is_recommended":       d.IsRecommended,
			"status":               string(d.Status),
		}},
	})
	return d, nil
}

// handleUndecideDecision reverts a decided decision back to proposed.
func handleUndecideDecision(repoRoot string, db *index.DB, actor, tree, idOrPrefix string) (*core.Decision, error) {
	if db == nil {
		return nil, errors.New("undecide_decision: index not available")
	}
	id, err := resolveDecisionID(db, tree, idOrPrefix)
	if err != nil {
		return nil, fmt.Errorf("undecide_decision: %w", err)
	}
	d, err := index.GetDecision(db, id)
	if err != nil || d == nil {
		return nil, fmt.Errorf("undecide_decision: load: %w", err)
	}
	before := decisionAuditMap(d)
	d.Status = core.StatusProposed
	d.ActualChoice = ""
	d.ActualChoiceReason = ""
	d.DecidedBy = nil
	d.IsRecommended = false
	if err := writeAndIndex(repoRoot, db, d, ""); err != nil {
		return nil, fmt.Errorf("undecide_decision: %w", err)
	}
	_ = audit.Append(repoRoot, core.Event{
		Actor: actor, Action: core.ActionUndecide, Kind: core.KindDecision,
		Tree: tree, ID: d.ID,
		Payload: core.EventPayload{Before: before, After: map[string]any{"status": string(d.Status)}},
	})
	return d, nil
}

// handleScopeOutDecision marks a decision as out_of_scope with a reason.
func handleScopeOutDecision(repoRoot string, db *index.DB, actor, tree, idOrPrefix, reason string) (*core.Decision, error) {
	if db == nil {
		return nil, errors.New("scope_out_decision: index not available")
	}
	id, err := resolveDecisionID(db, tree, idOrPrefix)
	if err != nil {
		return nil, fmt.Errorf("scope_out_decision: %w", err)
	}
	d, err := index.GetDecision(db, id)
	if err != nil || d == nil {
		return nil, fmt.Errorf("scope_out_decision: load: %w", err)
	}
	before := decisionAuditMap(d)
	d.Status = core.StatusOutOfScope
	d.OutOfScopeReason = reason
	if err := writeAndIndex(repoRoot, db, d, ""); err != nil {
		return nil, fmt.Errorf("scope_out_decision: %w", err)
	}
	_ = audit.Append(repoRoot, core.Event{
		Actor: actor, Action: core.ActionScopeOut, Kind: core.KindDecision,
		Tree: tree, ID: d.ID,
		Payload: core.EventPayload{Before: before, After: map[string]any{
			"status":              string(d.Status),
			"out_of_scope_reason": d.OutOfScopeReason,
		}},
	})
	return d, nil
}

// handleSupersedeDecision marks d as superseded by `by`, creating supersedes
// edges in both directions.
func handleSupersedeDecision(repoRoot string, db *index.DB, actor, tree, idOrPrefix, by string) (*core.Decision, error) {
	if db == nil {
		return nil, errors.New("supersede_decision: index not available")
	}
	if by == "" {
		return nil, errors.New("supersede_decision: by is required")
	}
	id, err := resolveDecisionID(db, tree, idOrPrefix)
	if err != nil {
		return nil, fmt.Errorf("supersede_decision: %w", err)
	}
	d, err := index.GetDecision(db, id)
	if err != nil || d == nil {
		return nil, fmt.Errorf("supersede_decision: load: %w", err)
	}
	if by == d.ID {
		return nil, errors.New("supersede_decision: a decision cannot supersede itself")
	}
	other, err := index.GetDecision(db, by)
	if err != nil {
		return nil, fmt.Errorf("supersede_decision: by lookup: %w", err)
	}
	if other == nil {
		return nil, fmt.Errorf("supersede_decision: by decision not found: %s", by)
	}

	before := decisionAuditMap(d)
	d.Status = core.StatusSuperseded
	addUniqueRel(d, core.Relationship{Type: core.RelSupersedes, Target: by})

	if err := writeAndIndex(repoRoot, db, d, ""); err != nil {
		return nil, fmt.Errorf("supersede_decision: %w", err)
	}
	addUniqueRel(other, core.Relationship{Type: core.RelSupersedes, Target: d.ID})
	_ = writeAndIndex(repoRoot, db, other, "")

	_ = audit.Append(repoRoot, core.Event{
		Actor: actor, Action: core.ActionSupersede, Kind: core.KindDecision,
		Tree: tree, ID: d.ID,
		Payload: core.EventPayload{Before: before, After: map[string]any{
			"status": string(d.Status),
			"by":     by,
		}},
	})
	return d, nil
}

// handleRestoreDecision moves an out_of_scope decision back to proposed.
func handleRestoreDecision(repoRoot string, db *index.DB, actor, tree, idOrPrefix string) (*core.Decision, error) {
	if db == nil {
		return nil, errors.New("restore_decision: index not available")
	}
	id, err := resolveDecisionID(db, tree, idOrPrefix)
	if err != nil {
		return nil, fmt.Errorf("restore_decision: %w", err)
	}
	d, err := index.GetDecision(db, id)
	if err != nil || d == nil {
		return nil, fmt.Errorf("restore_decision: load: %w", err)
	}
	if d.Status != core.StatusOutOfScope {
		return nil, fmt.Errorf("restore_decision: only valid for out_of_scope; current=%s", d.Status)
	}
	before := decisionAuditMap(d)
	d.Status = core.StatusProposed
	d.OutOfScopeReason = ""
	if err := writeAndIndex(repoRoot, db, d, ""); err != nil {
		return nil, fmt.Errorf("restore_decision: %w", err)
	}
	_ = audit.Append(repoRoot, core.Event{
		Actor: actor, Action: core.ActionRestore, Kind: core.KindDecision,
		Tree: tree, ID: d.ID,
		Payload: core.EventPayload{Before: before, After: map[string]any{"status": string(d.Status)}},
	})
	return d, nil
}

// ---------------------------------------------------------------------------
// Relate / unrelate
// ---------------------------------------------------------------------------

// handleRelateDecisions adds a relationship edge from source to target.
// supersedes edges must go through handleSupersedeDecision.
func handleRelateDecisions(repoRoot string, db *index.DB, actor, tree, source, relType, target, note string) (*core.Decision, error) {
	if db == nil {
		return nil, errors.New("relate_decisions: index not available")
	}
	if relType == "" || target == "" {
		return nil, errors.New("relate_decisions: type and target are required")
	}
	rt := core.RelationshipType(relType)
	if rt == core.RelSupersedes {
		return nil, errors.New("relate_decisions: supersedes is managed via supersede_decision")
	}
	switch rt {
	case core.RelBlocks, core.RelInfluences, core.RelRelatesTo:
	default:
		return nil, fmt.Errorf("relate_decisions: invalid type %q", relType)
	}
	srcID, err := resolveDecisionID(db, tree, source)
	if err != nil {
		return nil, fmt.Errorf("relate_decisions: source: %w", err)
	}
	d, err := index.GetDecision(db, srcID)
	if err != nil || d == nil {
		return nil, fmt.Errorf("relate_decisions: load source: %w", err)
	}
	if target == d.ID {
		return nil, errors.New("relate_decisions: cannot relate to self")
	}
	other, err := index.GetDecision(db, target)
	if err != nil {
		return nil, fmt.Errorf("relate_decisions: target lookup: %w", err)
	}
	if other == nil {
		return nil, fmt.Errorf("relate_decisions: target not found: %s", target)
	}

	if rt == core.RelBlocks {
		edges, err := loadEdges(db)
		if err != nil {
			return nil, fmt.Errorf("relate_decisions: load edges: %w", err)
		}
		if validate.AddingEdgeWouldCycle(edges, d.ID, target, rt) {
			return nil, errors.New("relate_decisions: would create cycle")
		}
	}
	for _, rel := range d.Relationships {
		if rel.Type == rt && rel.Target == target {
			// idempotent
			return d, nil
		}
	}

	before := decisionAuditMap(d)
	d.Relationships = append(d.Relationships, core.Relationship{Type: rt, Target: target})
	if err := writeAndIndex(repoRoot, db, d, ""); err != nil {
		return nil, fmt.Errorf("relate_decisions: %w", err)
	}
	_ = audit.Append(repoRoot, core.Event{
		Actor: actor, Action: core.ActionRelate, Kind: core.KindRelationship,
		Tree: tree, ID: d.ID,
		Payload: core.EventPayload{Before: before, After: map[string]any{
			"type":   string(rt),
			"target": target,
			"note":   note,
		}},
	})
	return d, nil
}

// handleUnrelateDecisions removes a relationship edge. Idempotent: returns
// the current decision unchanged when no matching edge is found.
func handleUnrelateDecisions(repoRoot string, db *index.DB, actor, tree, source, relType, target string) (*core.Decision, error) {
	if db == nil {
		return nil, errors.New("unrelate_decisions: index not available")
	}
	if relType == "" || target == "" {
		return nil, errors.New("unrelate_decisions: type and target are required")
	}
	srcID, err := resolveDecisionID(db, tree, source)
	if err != nil {
		return nil, fmt.Errorf("unrelate_decisions: %w", err)
	}
	d, err := index.GetDecision(db, srcID)
	if err != nil || d == nil {
		return nil, fmt.Errorf("unrelate_decisions: load: %w", err)
	}

	before := decisionAuditMap(d)
	filtered := d.Relationships[:0]
	removed := false
	for _, rel := range d.Relationships {
		if string(rel.Type) == relType && rel.Target == target {
			removed = true
			continue
		}
		filtered = append(filtered, rel)
	}
	d.Relationships = filtered
	if !removed {
		return d, nil
	}
	if err := writeAndIndex(repoRoot, db, d, ""); err != nil {
		return nil, fmt.Errorf("unrelate_decisions: %w", err)
	}
	_ = audit.Append(repoRoot, core.Event{
		Actor: actor, Action: core.ActionUnrelate, Kind: core.KindRelationship,
		Tree: tree, ID: d.ID,
		Payload: core.EventPayload{Before: before, After: map[string]any{
			"type": relType, "target": target,
		}},
	})
	return d, nil
}

// ---------------------------------------------------------------------------
// History / find
// ---------------------------------------------------------------------------

// handleDecisionHistory returns all events for the named decision. since is
// optional; supports relative durations like "7d" / "24h" or RFC3339.
func handleDecisionHistory(repoRoot string, db *index.DB, tree, idOrPrefix, since string) ([]core.Event, error) {
	if db == nil {
		return nil, errors.New("decision_history: index not available")
	}
	id, err := resolveDecisionID(db, tree, idOrPrefix)
	if err != nil {
		return nil, fmt.Errorf("decision_history: %w", err)
	}
	f := audit.Filter{Tree: tree, TargetID: id}
	if since != "" {
		t, err := parseSince(since)
		if err != nil {
			return nil, fmt.Errorf("decision_history: since: %w", err)
		}
		f.Since = t
	}
	events, err := audit.Read(repoRoot, f)
	if err != nil {
		return nil, fmt.Errorf("decision_history: %w", err)
	}
	if events == nil {
		events = []core.Event{}
	}
	return events, nil
}

// findHit is one row in handleFindDecisions's response.
type findHit struct {
	ID       string `json:"id"`
	Tree     string `json:"tree"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
	Summary  string `json:"summary"`
	Snippet  string `json:"snippet"`
}

// handleFindDecisions runs an FTS5 MATCH across all (or a single) tree.
func handleFindDecisions(db *index.DB, query, treeSlug string, limit int) ([]findHit, error) {
	if db == nil {
		return nil, errors.New("find_decisions: index not available")
	}
	if query == "" {
		return nil, errors.New("find_decisions: query is required")
	}
	if limit <= 0 {
		limit = 25
	}
	if limit > 200 {
		limit = 200
	}

	q := `SELECT d.id, d.tree, d.status, d.priority, d.summary,
	             snippet(decisions_fts, 0, '[', ']', '...', 32) AS snip
	      FROM decisions_fts
	      JOIN decisions d ON d.rowid = decisions_fts.rowid
	      WHERE decisions_fts MATCH ?
	        AND d.deleted = 0`
	args := []any{query}
	if treeSlug != "" {
		q += ` AND d.tree = ?`
		args = append(args, treeSlug)
	}
	q += ` ORDER BY rank LIMIT ?`
	args = append(args, limit)

	rows, err := db.Conn().Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("find_decisions: query: %w", err)
	}
	defer rows.Close()

	out := []findHit{}
	for rows.Next() {
		var h findHit
		if err := rows.Scan(&h.ID, &h.Tree, &h.Status, &h.Priority, &h.Summary, &h.Snippet); err != nil {
			return nil, fmt.Errorf("find_decisions: scan: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

// resolveDecisionID maps a {id} (full ULID or ≥4-char prefix) to the
// canonical 26-char ULID within the given tree.
func resolveDecisionID(db *index.DB, tree, raw string) (string, error) {
	if raw == "" {
		return "", errors.New("id is required")
	}
	if tree == "" {
		return "", errors.New("tree is required")
	}
	if len(raw) == 26 {
		var found string
		err := db.Conn().QueryRow(
			`SELECT id FROM decisions WHERE tree=? AND id=? AND deleted=0`,
			tree, raw,
		).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("decision not found: %s", raw)
		}
		if err != nil {
			return "", err
		}
		return found, nil
	}
	if len(raw) < 4 {
		return "", fmt.Errorf("decision id must be 26-char ULID or prefix ≥4 chars: %q", raw)
	}
	rows, err := db.Conn().Query(
		`SELECT id FROM decisions WHERE tree=? AND id LIKE ? AND deleted=0 LIMIT 2`,
		tree, raw+"%",
	)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var matches []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		matches = append(matches, id)
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("decision not found: %s", raw)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("ambiguous decision id prefix: %s", raw)
	}
	return matches[0], nil
}

// writeAndIndex performs the standard write-yaml + update-index sequence used
// by every mutating handler. expectedRev "" disables optimistic-lock checks.
func writeAndIndex(repoRoot string, db *index.DB, d *core.Decision, expectedRev string) error {
	d.SchemaVersion = core.SchemaVersion
	if d.Slug == "" {
		d.Slug = storage.SlugFromSummary(d.Summary)
	}
	if err := validate.Decision(d); err != nil {
		return fmt.Errorf("validation: %w", err)
	}
	path := storage.DecisionPath(filepath.Join(repoRoot, ".decisions", d.Tree), d.ID, d.Slug)
	if err := storage.WriteDecision(path, d); err != nil {
		return fmt.Errorf("write yaml: %w", err)
	}
	sha, err := fsutil.Sha256File(path)
	if err != nil {
		return fmt.Errorf("hash: %w", err)
	}
	newRev := concurrency.NewRev()
	if err := index.UpdateDecisionWithExpectedRev(db, d, sha, expectedRev, newRev); err != nil {
		if c, ok := concurrency.AsConflict(err); ok {
			return fmt.Errorf("rev conflict: expected %q, current %q", c.ExpectedRev, c.ActualRev)
		}
		return fmt.Errorf("index: %w", err)
	}
	d.Rev = newRev
	return nil
}

// addUniqueRel mirrors server.appendUniqueRel: dedupe by (type, target).
func addUniqueRel(d *core.Decision, rel core.Relationship) {
	for _, r := range d.Relationships {
		if r.Type == rel.Type && r.Target == rel.Target {
			return
		}
	}
	d.Relationships = append(d.Relationships, rel)
}

// loadEdges returns all relationships from the index for cycle checks.
func loadEdges(db *index.DB) ([]validate.Edge, error) {
	rows, err := db.Conn().Query(`SELECT source, target, type FROM relationships`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []validate.Edge
	for rows.Next() {
		var e validate.Edge
		var t string
		if err := rows.Scan(&e.Source, &e.Target, &t); err != nil {
			return nil, err
		}
		e.Type = core.RelationshipType(t)
		out = append(out, e)
	}
	return out, rows.Err()
}

// readTreeRow loads a tree row by slug. Returns sql.ErrNoRows on miss.
func readTreeRow(db *index.DB, slug string) (*core.Tree, error) {
	var (
		t        core.Tree
		archived int
		createdS string
		dir      string
	)
	err := db.Conn().QueryRow(
		`SELECT slug, title, description, archived, created_at, layout_direction, schema_version
		 FROM trees WHERE slug=?`, slug,
	).Scan(&t.Slug, &t.Title, &t.Description, &archived, &createdS, &dir, &t.SchemaVersion)
	if err != nil {
		return nil, err
	}
	t.Archived = archived == 1
	t.Layout.Direction = dir
	if ts, err := time.Parse(time.RFC3339, createdS); err == nil {
		t.CreatedAt = ts
	}
	return &t, nil
}

// treeExistsRow is a yes/no check that swallows ErrNoRows.
func treeExistsRow(db *index.DB, slug string) (bool, error) {
	_, err := readTreeRow(db, slug)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// insertTreeRow indexes a freshly-created tree.
func insertTreeRow(db *index.DB, t *core.Tree) error {
	dir := t.Layout.Direction
	if dir == "" {
		dir = "TB"
	}
	archived := 0
	if t.Archived {
		archived = 1
	}
	sv := t.SchemaVersion
	if sv == 0 {
		sv = core.SchemaVersion
	}
	_, err := db.Conn().Exec(
		`INSERT OR IGNORE INTO trees(slug, title, description, archived, created_at, layout_direction, schema_version)
		 VALUES(?, ?, ?, ?, ?, ?, ?)`,
		t.Slug, t.Title, t.Description, archived,
		t.CreatedAt.UTC().Format(time.RFC3339), dir, sv,
	)
	return err
}

// findDecisionFile returns the on-disk YAML path for d, or "" if it can't
// be located (slug drift tolerated by globbing on id).
func findDecisionFile(repoRoot string, d *core.Decision) string {
	dir := filepath.Join(repoRoot, ".decisions", d.Tree, "decisions")
	candidate := storage.DecisionPath(filepath.Join(repoRoot, ".decisions", d.Tree), d.ID, d.Slug)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	matches, err := filepath.Glob(filepath.Join(dir, d.ID+"-*.yaml"))
	if err == nil && len(matches) > 0 {
		return matches[0]
	}
	return ""
}

// decisionAuditMap renders d as a JSON-friendly map for audit payloads.
func decisionAuditMap(d *core.Decision) map[string]any {
	return map[string]any{
		"id":       d.ID,
		"tree":     d.Tree,
		"slug":     d.Slug,
		"summary":  d.Summary,
		"priority": string(d.Priority),
		"status":   string(d.Status),
	}
}

// parseSince parses "7d" / "24h" / "30m" / "10s" or RFC3339.
func parseSince(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if len(s) < 2 {
		return time.Time{}, fmt.Errorf("unparseable time %q", s)
	}
	unit := s[len(s)-1]
	num := s[:len(s)-1]
	var mult time.Duration
	switch unit {
	case 'd':
		mult = 24 * time.Hour
	case 'h':
		mult = time.Hour
	case 'm':
		mult = time.Minute
	case 's':
		mult = time.Second
	default:
		return time.Time{}, fmt.Errorf("unparseable time %q", s)
	}
	var n int
	if _, err := fmt.Sscanf(num, "%d", &n); err != nil {
		return time.Time{}, fmt.Errorf("unparseable time %q", s)
	}
	return time.Now().UTC().Add(-time.Duration(n) * mult), nil
}

// ctxBackground is a tiny convenience to avoid context.Background() lint
// warnings when a handler doesn't take ctx (kept here for future use).
var _ = context.Background
