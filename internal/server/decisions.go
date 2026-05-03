package server

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/concurrency"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/fsutil"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/storage"
	"github.com/cgould/dtree/internal/ulid"
	"github.com/cgould/dtree/internal/validate"
)

// ---------------------------------------------------------------------------
// Mount
// ---------------------------------------------------------------------------

// mountDecisions registers all /v1/trees/{tree}/decisions* routes.
//
// ID resolution: handlers accept either a full 26-char ULID or a prefix of
// at least 4 characters. Prefix lookups are scoped to the route's tree and
// must resolve to exactly one decision; ambiguous prefixes return 400.
func mountDecisions(r chi.Router, cfg Config) {
	r.Route("/trees/{tree}/decisions", func(r chi.Router) {
		r.Get("/", listDecisionsHandler(cfg))
		r.Post("/", createDecisionHandler(cfg))
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", getDecisionHandler(cfg))
			r.Patch("/", patchDecisionHandler(cfg))
			r.Delete("/", deleteDecisionHandler(cfg))
			r.Get("/history", historyDecisionHandler(cfg))
			r.Post("/decide", decideDecisionHandler(cfg))
			r.Post("/undecide", undecideDecisionHandler(cfg))
			r.Post("/scope-out", scopeOutDecisionHandler(cfg))
			r.Post("/restore", restoreDecisionHandler(cfg))
			r.Post("/supersede", supersedeDecisionHandler(cfg))
			r.Post("/relate", relateDecisionHandler(cfg))
			r.Post("/unrelate", unrelateDecisionHandler(cfg))
		})
	})
}

// ---------------------------------------------------------------------------
// Common helpers
// ---------------------------------------------------------------------------

// requireTree returns true and writes a Problem if the tree slug doesn't
// exist (404). Otherwise returns false meaning "tree exists, continue".
func requireTree(w http.ResponseWriter, r *http.Request, db *index.DB, slug string) bool {
	exists, err := treeExistsInIndex(db, slug)
	if err != nil {
		WriteProblem(w, r, Internal("failed to check tree"))
		return true
	}
	if !exists {
		WriteProblem(w, r, NotFound("tree not found: "+slug))
		return true
	}
	return false
}

// resolveDecisionID maps a URL {id} (full ULID or prefix ≥4 chars) to the
// canonical 26-char ULID within the given tree.
//
// Returns:
//   - id == "", problem == nil  → not found (caller writes 404)
//   - id == "", problem != nil  → already wrote a Problem (e.g. 400 ambiguous)
//   - id != ""                  → resolved
func resolveDecisionID(w http.ResponseWriter, r *http.Request, db *index.DB, treeSlug, raw string) (string, bool) {
	if len(raw) == 26 {
		// Treat as full ULID; verify it exists in this tree.
		var found string
		err := db.Conn().QueryRow(
			`SELECT id FROM decisions WHERE tree=? AND id=? AND deleted=0`,
			treeSlug, raw,
		).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			return "", false
		}
		if err != nil {
			WriteProblem(w, r, Internal("failed to look up decision"))
			return "", true
		}
		return found, true
	}
	if len(raw) < 4 {
		WriteProblem(w, r, BadRequest("decision id must be a full 26-char ULID or prefix of at least 4 chars"))
		return "", true
	}
	rows, err := db.Conn().Query(
		`SELECT id FROM decisions WHERE tree=? AND id LIKE ? AND deleted=0 LIMIT 2`,
		treeSlug, raw+"%",
	)
	if err != nil {
		WriteProblem(w, r, Internal("failed to look up decision"))
		return "", true
	}
	defer rows.Close()
	var matches []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			WriteProblem(w, r, Internal("failed to scan decision id"))
			return "", true
		}
		matches = append(matches, id)
	}
	if len(matches) == 0 {
		return "", false
	}
	if len(matches) > 1 {
		WriteProblem(w, r, BadRequest("ambiguous decision id prefix: "+raw))
		return "", true
	}
	return matches[0], true
}

// loadDecisionOr404 fetches a decision and writes 404 if missing.
// Returns (d, true) when caller may continue; (nil, false) means a Problem
// has already been written (or the row was missing).
func loadDecisionOr404(w http.ResponseWriter, r *http.Request, db *index.DB, id string) (*core.Decision, bool) {
	d, err := index.GetDecision(db, id)
	if err != nil {
		WriteProblem(w, r, Internal("failed to read decision"))
		return nil, false
	}
	if d == nil {
		WriteProblem(w, r, NotFound("decision not found: "+id))
		return nil, false
	}
	return d, true
}

// readOnlyGuard rejects mutating endpoints when the server is configured
// read-only. Returns true if a Problem was written.
func readOnlyGuard(w http.ResponseWriter, r *http.Request, cfg Config) bool {
	if cfg.ReadOnly {
		WriteProblem(w, r, Forbidden("server is in read-only mode"))
		return true
	}
	return false
}

// matchIfMatch validates an If-Match request header against the decision's
// current rev. Empty header is allowed (treated as no-op). Returns true if
// a Problem was written and the caller should abort.
func matchIfMatch(w http.ResponseWriter, r *http.Request, currentRev string) (expected string, abort bool) {
	expected = strings.Trim(r.Header.Get("If-Match"), `"`)
	if expected == "" {
		return "", false
	}
	if expected != currentRev {
		WriteProblem(w, r, PreconditionFailed(fmt.Sprintf(
			"rev mismatch: expected %q, current %q", expected, currentRev,
		)))
		return "", true
	}
	return expected, false
}

// writeDecisionResponse writes d as JSON with the ETag header.
func writeDecisionResponse(w http.ResponseWriter, status int, d *core.Decision) {
	if d.Rev != "" {
		w.Header().Set("ETag", `"`+d.Rev+`"`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(d)
}

// decisionToAuditMap renders d as a JSON-friendly map for audit payloads.
func decisionToAuditMap(d *core.Decision) map[string]any {
	b, _ := json.Marshal(d)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

// findDecisionFile returns the on-disk path of d's YAML file, or "" if it
// can't be located. The conventional name is <id>-<slug>.yaml under
// .decisions/<tree>/decisions/, but tolerate slug drift by globbing on id.
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

// writeDecisionAndIndex performs the standard "rewrite YAML + UpdateDecision"
// workflow shared by the lifecycle handlers. expectedRev is enforced inside
// the index update transaction; pass "" to skip the check.
//
// On concurrency conflict it writes a 412 Problem and returns (false, "").
// On success it returns (true, newRev).
func writeDecisionAndIndex(w http.ResponseWriter, r *http.Request, cfg Config, d *core.Decision, expectedRev string) (bool, string) {
	d.SchemaVersion = core.SchemaVersion
	if d.Slug == "" {
		d.Slug = storage.SlugFromSummary(d.Summary)
	}
	if err := validate.Decision(d); err != nil {
		WriteProblem(w, r, Unprocessable("validation: "+err.Error()))
		return false, ""
	}
	path := storage.DecisionPath(filepath.Join(cfg.RepoRoot, ".decisions", d.Tree), d.ID, d.Slug)
	if err := storage.WriteDecision(path, d); err != nil {
		WriteProblem(w, r, Internal("failed to write decision: "+err.Error()))
		return false, ""
	}
	contentSha, err := fsutil.Sha256File(path)
	if err != nil {
		WriteProblem(w, r, Internal("failed to hash decision file"))
		return false, ""
	}
	newRev := concurrency.NewRev()
	if err := index.UpdateDecisionWithExpectedRev(cfg.DB, d, contentSha, expectedRev, newRev); err != nil {
		if c, ok := concurrency.AsConflict(err); ok {
			WriteProblem(w, r, PreconditionFailed(fmt.Sprintf(
				"rev mismatch: expected %q, current %q", c.ExpectedRev, c.ActualRev,
			)))
			return false, ""
		}
		WriteProblem(w, r, Internal("failed to update index: "+err.Error()))
		return false, ""
	}
	d.Rev = newRev
	return true, newRev
}

// ---------------------------------------------------------------------------
// GET /v1/trees/{tree}/decisions  — list
// ---------------------------------------------------------------------------

type decisionListResponse struct {
	Items      []*core.Decision `json:"items"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

func listDecisionsHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tree := chi.URLParam(r, "tree")
		if requireTree(w, r, cfg.DB, tree) {
			return
		}

		q := r.URL.Query()
		limit := 50
		if l := q.Get("limit"); l != "" {
			n, err := strconv.Atoi(l)
			if err != nil || n < 1 {
				WriteProblem(w, r, BadRequest("limit must be a positive integer"))
				return
			}
			if n > 500 {
				n = 500
			}
			limit = n
		}

		cursor := q.Get("cursor")
		var afterID string
		if cursor != "" {
			b, err := base64.RawURLEncoding.DecodeString(cursor)
			if err != nil {
				WriteProblem(w, r, BadRequest("invalid cursor"))
				return
			}
			afterID = string(b)
		}

		// Build the query.
		var clauses []string
		var args []any
		clauses = append(clauses, "d.tree = ?")
		args = append(args, tree)
		clauses = append(clauses, "d.deleted = 0")

		if v := q.Get("status"); v != "" {
			clauses = append(clauses, "d.status = ?")
			args = append(args, v)
		}
		if v := q.Get("priority"); v != "" {
			clauses = append(clauses, "d.priority = ?")
			args = append(args, v)
		}
		if v := q.Get("creator"); v != "" {
			clauses = append(clauses, "d.creator = ?")
			args = append(args, v)
		}
		if v := q.Get("assigned"); v != "" {
			clauses = append(clauses, "d.assignee = ?")
			args = append(args, v)
		}
		if v := q.Get("decider"); v != "" {
			clauses = append(clauses, "EXISTS (SELECT 1 FROM decision_deciders dd WHERE dd.decision_id = d.id AND dd.handle = ?)")
			args = append(args, v)
		}
		if v := q.Get("tag"); v != "" {
			clauses = append(clauses, "EXISTS (SELECT 1 FROM decision_tags dt WHERE dt.decision_id = d.id AND dt.tag = ?)")
			args = append(args, v)
		}
		if v := q.Get("search"); v != "" {
			clauses = append(clauses, "d.rowid IN (SELECT rowid FROM decisions_fts WHERE decisions_fts MATCH ?)")
			args = append(args, v)
		}
		if v := q.Get("since"); v != "" {
			t, err := parseRelativeDuration(v)
			if err != nil {
				WriteProblem(w, r, BadRequest("since: "+err.Error()))
				return
			}
			clauses = append(clauses, "d.id >= ?")
			args = append(args, ulidFromTime(t))
		}
		if v := q.Get("until"); v != "" {
			t, err := parseRelativeDuration(v)
			if err != nil {
				WriteProblem(w, r, BadRequest("until: "+err.Error()))
				return
			}
			clauses = append(clauses, "d.id <= ?")
			args = append(args, ulidUpperFromTime(t))
		}
		if afterID != "" {
			clauses = append(clauses, "d.id > ?")
			args = append(args, afterID)
		}

		// Fetch limit+1 to detect "more".
		args = append(args, limit+1)
		query := fmt.Sprintf(
			`SELECT d.id FROM decisions d WHERE %s ORDER BY d.id ASC LIMIT ?`,
			strings.Join(clauses, " AND "),
		)
		rows, err := cfg.DB.Conn().Query(query, args...)
		if err != nil {
			WriteProblem(w, r, Internal("failed to list decisions: "+err.Error()))
			return
		}
		defer rows.Close()

		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				WriteProblem(w, r, Internal("scan decision id"))
				return
			}
			ids = append(ids, id)
		}

		var nextCursor string
		if len(ids) > limit {
			nextCursor = base64.RawURLEncoding.EncodeToString([]byte(ids[limit-1]))
			ids = ids[:limit]
		}

		items := make([]*core.Decision, 0, len(ids))
		for _, id := range ids {
			d, err := index.GetDecision(cfg.DB, id)
			if err != nil {
				WriteProblem(w, r, Internal("failed to load decision"))
				return
			}
			if d != nil {
				items = append(items, d)
			}
		}
		writeJSON(w, http.StatusOK, decisionListResponse{Items: items, NextCursor: nextCursor})
	}
}

// ulidFromTime converts t into a ULID-shaped lower-bound (timestamp + zeros)
// suitable for ordered-id range comparisons.
func ulidFromTime(t time.Time) string {
	if t.IsZero() {
		return strings.Repeat("0", 26)
	}
	ms := t.UTC().UnixMilli()
	if ms < 0 {
		ms = 0
	}
	// Crockford base32 alphabet for the 10-char timestamp prefix.
	const enc = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	var buf [10]byte
	for i := 9; i >= 0; i-- {
		buf[i] = enc[ms&31]
		ms >>= 5
	}
	return string(buf[:]) + strings.Repeat("0", 16)
}

// ulidUpperFromTime returns a ULID-shaped upper-bound.
func ulidUpperFromTime(t time.Time) string {
	if t.IsZero() {
		return strings.Repeat("Z", 26)
	}
	return ulidFromTime(t)[:10] + strings.Repeat("Z", 16)
}

// ---------------------------------------------------------------------------
// POST /v1/trees/{tree}/decisions  — create
// ---------------------------------------------------------------------------

func createDecisionHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if readOnlyGuard(w, r, cfg) {
			return
		}
		if !MustHaveIdentity(w, r) {
			return
		}
		actor, _ := IdentityFromContext(r.Context())
		tree := chi.URLParam(r, "tree")
		if requireTree(w, r, cfg.DB, tree) {
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			WriteProblem(w, r, BadRequest("read body: "+err.Error()))
			return
		}
		var d core.Decision
		if err := json.Unmarshal(body, &d); err != nil {
			WriteProblem(w, r, BadRequest("invalid JSON body: "+err.Error()))
			return
		}

		if d.ID == "" {
			d.ID = ulid.New()
		} else if len(d.ID) != 26 {
			WriteProblem(w, r, BadRequest("id must be a 26-char ULID"))
			return
		}
		d.Tree = tree
		if d.Status == "" {
			d.Status = core.StatusProposed
		}
		if d.Priority == "" {
			d.Priority = core.PriorityMedium
		}
		if d.Creator == "" {
			d.Creator = actor
		}
		if d.Slug == "" {
			d.Slug = storage.SlugFromSummary(d.Summary)
		}
		d.SchemaVersion = core.SchemaVersion

		if err := validate.Decision(&d); err != nil {
			WriteProblem(w, r, Unprocessable("validation: "+err.Error()))
			return
		}

		// Refuse duplicate ID.
		existing, err := index.GetDecision(cfg.DB, d.ID)
		if err != nil {
			WriteProblem(w, r, Internal("check existing decision"))
			return
		}
		if existing != nil {
			WriteProblem(w, r, Conflict("decision already exists: "+d.ID))
			return
		}

		path := storage.DecisionPath(filepath.Join(cfg.RepoRoot, ".decisions", tree), d.ID, d.Slug)
		if err := storage.WriteDecision(path, &d); err != nil {
			WriteProblem(w, r, Internal("write decision: "+err.Error()))
			return
		}
		contentSha, err := fsutil.Sha256File(path)
		if err != nil {
			WriteProblem(w, r, Internal("hash decision: "+err.Error()))
			return
		}
		if err := index.InsertDecision(cfg.DB, &d, contentSha); err != nil {
			WriteProblem(w, r, Internal("insert index: "+err.Error()))
			return
		}

		// Read back to capture the rev.
		stored, err := index.GetDecision(cfg.DB, d.ID)
		if err != nil || stored == nil {
			WriteProblem(w, r, Internal("read back created decision"))
			return
		}

		_ = audit.Append(cfg.RepoRoot, core.Event{
			Actor:  actor,
			Action: core.ActionCreate,
			Kind:   core.KindDecision,
			Tree:   tree,
			ID:     d.ID,
			Payload: core.EventPayload{
				After: decisionToAuditMap(stored),
			},
		})

		writeDecisionResponse(w, http.StatusCreated, stored)
	}
}

// ---------------------------------------------------------------------------
// GET /v1/trees/{tree}/decisions/{id}
// ---------------------------------------------------------------------------

func getDecisionHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tree := chi.URLParam(r, "tree")
		if requireTree(w, r, cfg.DB, tree) {
			return
		}
		raw := chi.URLParam(r, "id")
		id, ok := resolveDecisionID(w, r, cfg.DB, tree, raw)
		if !ok {
			if id == "" {
				WriteProblem(w, r, NotFound("decision not found: "+raw))
			}
			return
		}
		d, ok := loadDecisionOr404(w, r, cfg.DB, id)
		if !ok {
			return
		}
		writeDecisionResponse(w, http.StatusOK, d)
	}
}

// ---------------------------------------------------------------------------
// PATCH /v1/trees/{tree}/decisions/{id}  — JSON Merge Patch (RFC 7396)
// ---------------------------------------------------------------------------

// patchDecisionRequest holds the mutable subset of fields. Pointer types
// distinguish "not provided" from "set to zero". Slice types treat null as
// "clear" and absent as "no change" via a separate raw-payload pass.
type patchDecisionRequest struct {
	Summary            *string  `json:"summary"`
	Description        *string  `json:"decision_full_description"`
	Priority           *string  `json:"priority"`
	Tags               []string `json:"tags"`
	Assignee           *string  `json:"assignee"`
	RecommendedSummary *string  `json:"recommended_summary"`
	RecommendedFull    *string  `json:"recommended_full"`
	RecommendedBy      *string  `json:"recommended_by"`
}

func patchDecisionHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if readOnlyGuard(w, r, cfg) {
			return
		}
		if !MustHaveIdentity(w, r) {
			return
		}
		actor, _ := IdentityFromContext(r.Context())
		tree := chi.URLParam(r, "tree")
		if requireTree(w, r, cfg.DB, tree) {
			return
		}
		id, ok := resolveDecisionID(w, r, cfg.DB, tree, chi.URLParam(r, "id"))
		if !ok {
			if id == "" {
				WriteProblem(w, r, NotFound("decision not found"))
			}
			return
		}
		d, ok := loadDecisionOr404(w, r, cfg.DB, id)
		if !ok {
			return
		}

		// We need to peek at the raw JSON to honor "tags absent vs tags: []".
		rawBody, err := io.ReadAll(r.Body)
		if err != nil {
			WriteProblem(w, r, BadRequest("read body: "+err.Error()))
			return
		}
		var rawMap map[string]json.RawMessage
		if err := json.Unmarshal(rawBody, &rawMap); err != nil {
			WriteProblem(w, r, BadRequest("invalid JSON: "+err.Error()))
			return
		}
		var req patchDecisionRequest
		if err := json.Unmarshal(rawBody, &req); err != nil {
			WriteProblem(w, r, BadRequest("invalid JSON shape: "+err.Error()))
			return
		}

		expected, abort := matchIfMatch(w, r, d.Rev)
		if abort {
			return
		}

		before := decisionToAuditMap(d)

		if req.Summary != nil {
			d.Summary = *req.Summary
			d.Slug = storage.SlugFromSummary(d.Summary)
		}
		if req.Description != nil {
			d.Description = *req.Description
		}
		if req.Priority != nil {
			d.Priority = core.Priority(*req.Priority)
		}
		if _, present := rawMap["tags"]; present {
			if req.Tags == nil {
				d.Tags = nil
			} else {
				d.Tags = req.Tags
			}
		}
		if req.Assignee != nil {
			d.Assignee = *req.Assignee
		}
		if req.RecommendedSummary != nil {
			d.RecommendedSummary = *req.RecommendedSummary
		}
		if req.RecommendedFull != nil {
			d.RecommendedFull = *req.RecommendedFull
		}
		if req.RecommendedBy != nil {
			d.RecommendedBy = *req.RecommendedBy
		}

		ok, _ = writeDecisionAndIndex(w, r, cfg, d, expected)
		if !ok {
			return
		}

		_ = audit.Append(cfg.RepoRoot, core.Event{
			Actor:  actor,
			Action: core.ActionUpdate,
			Kind:   core.KindDecision,
			Tree:   tree,
			ID:     d.ID,
			Payload: core.EventPayload{
				Before: before,
				After:  decisionToAuditMap(d),
			},
		})

		writeDecisionResponse(w, http.StatusOK, d)
	}
}

// ---------------------------------------------------------------------------
// DELETE /v1/trees/{tree}/decisions/{id}
// ---------------------------------------------------------------------------

func deleteDecisionHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if readOnlyGuard(w, r, cfg) {
			return
		}
		if !MustHaveIdentity(w, r) {
			return
		}
		actor, _ := IdentityFromContext(r.Context())
		tree := chi.URLParam(r, "tree")
		if requireTree(w, r, cfg.DB, tree) {
			return
		}
		id, ok := resolveDecisionID(w, r, cfg.DB, tree, chi.URLParam(r, "id"))
		if !ok {
			if id == "" {
				WriteProblem(w, r, NotFound("decision not found"))
			}
			return
		}
		d, ok := loadDecisionOr404(w, r, cfg.DB, id)
		if !ok {
			return
		}

		expected, abort := matchIfMatch(w, r, d.Rev)
		if abort {
			return
		}
		_ = expected // honored below via DeleteDecisionWithExpectedRev

		hard := r.URL.Query().Get("hard") == "true"

		// Hard delete: refuse if any decision in any tree references this one.
		if hard {
			var n int
			if err := cfg.DB.Conn().QueryRow(
				`SELECT COUNT(*) FROM relationships WHERE target=?`, d.ID,
			).Scan(&n); err != nil {
				WriteProblem(w, r, Internal("count incoming refs"))
				return
			}
			if n > 0 {
				// Documented choice: return 409 Conflict — there is a
				// referential conflict that the caller must resolve first.
				WriteProblem(w, r, Conflict(fmt.Sprintf(
					"hard delete refused: %d incoming reference(s)", n,
				)))
				return
			}
		}

		// Locate the YAML file BEFORE we mutate state.
		filePath := findDecisionFile(cfg.RepoRoot, d)

		if hard {
			// Drop from index and unlink the file.
			if _, err := cfg.DB.Conn().Exec(`DELETE FROM decisions WHERE id=?`, d.ID); err != nil {
				WriteProblem(w, r, Internal("hard delete index: "+err.Error()))
				return
			}
			if filePath != "" {
				_ = os.Remove(filePath)
			}
		} else {
			// Soft delete: move file to .decisions/.deleted/<tree>/, mark deleted.
			if filePath != "" {
				deletedDir := filepath.Join(cfg.RepoRoot, ".decisions", ".deleted", tree)
				if err := os.MkdirAll(deletedDir, 0o755); err != nil {
					WriteProblem(w, r, Internal("mkdir deleted dir"))
					return
				}
				dst := filepath.Join(deletedDir, filepath.Base(filePath))
				if err := os.Rename(filePath, dst); err != nil {
					// Fall back to copy+remove for cross-device safety.
					data, rerr := os.ReadFile(filePath)
					if rerr == nil {
						_ = os.WriteFile(dst, data, 0o644)
						_ = os.Remove(filePath)
					}
				}
			}
			if err := index.DeleteDecisionWithExpectedRev(cfg.DB, d.ID, expected); err != nil {
				if c, isConflict := concurrency.AsConflict(err); isConflict {
					WriteProblem(w, r, PreconditionFailed(fmt.Sprintf(
						"rev mismatch: expected %q, current %q", c.ExpectedRev, c.ActualRev,
					)))
					return
				}
				WriteProblem(w, r, Internal("soft delete index: "+err.Error()))
				return
			}
		}

		_ = audit.Append(cfg.RepoRoot, core.Event{
			Actor:  actor,
			Action: core.ActionDelete,
			Kind:   core.KindDecision,
			Tree:   tree,
			ID:     d.ID,
			Payload: core.EventPayload{
				Before: decisionToAuditMap(d),
				After: map[string]any{
					"hard": hard,
				},
			},
		})

		w.WriteHeader(http.StatusNoContent)
	}
}

// ---------------------------------------------------------------------------
// POST /v1/trees/{tree}/decisions/{id}/decide
// ---------------------------------------------------------------------------

type decideRequest struct {
	Choice        string   `json:"choice"`
	Reason        string   `json:"reason"`
	By            []string `json:"by"`
	IsRecommended bool     `json:"is_recommended"`
}

func decideDecisionHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if readOnlyGuard(w, r, cfg) {
			return
		}
		if !MustHaveIdentity(w, r) {
			return
		}
		actor, _ := IdentityFromContext(r.Context())
		tree := chi.URLParam(r, "tree")
		if requireTree(w, r, cfg.DB, tree) {
			return
		}
		id, ok := resolveDecisionID(w, r, cfg.DB, tree, chi.URLParam(r, "id"))
		if !ok {
			if id == "" {
				WriteProblem(w, r, NotFound("decision not found"))
			}
			return
		}
		d, ok := loadDecisionOr404(w, r, cfg.DB, id)
		if !ok {
			return
		}

		var req decideRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteProblem(w, r, BadRequest("invalid JSON body: "+err.Error()))
			return
		}
		if req.Choice == "" {
			WriteProblem(w, r, Unprocessable("choice is required"))
			return
		}

		// Mirror the CLI guard: decide is only valid on proposed decisions.
		// Without this the same decision can accumulate multiple decide
		// events, producing a confusing audit trail.
		if d.Status != core.StatusProposed {
			WriteProblem(w, r, Conflict(fmt.Sprintf(
				"decide is only valid for proposed decisions; current status: %s",
				d.Status,
			)))
			return
		}

		expected, abort := matchIfMatch(w, r, d.Rev)
		if abort {
			return
		}

		before := decisionToAuditMap(d)
		d.Status = core.StatusDecided
		d.ActualChoice = req.Choice
		d.ActualChoiceReason = req.Reason
		if len(req.By) > 0 {
			d.DecidedBy = req.By
		} else {
			d.DecidedBy = []string{actor}
		}
		d.IsRecommended = req.IsRecommended

		ok, _ = writeDecisionAndIndex(w, r, cfg, d, expected)
		if !ok {
			return
		}

		_ = audit.Append(cfg.RepoRoot, core.Event{
			Actor:  actor,
			Action: core.ActionDecide,
			Kind:   core.KindDecision,
			Tree:   tree,
			ID:     d.ID,
			Payload: core.EventPayload{
				Before: before,
				After: map[string]any{
					"actual_choice":        d.ActualChoice,
					"actual_choice_reason": d.ActualChoiceReason,
					"decided_by":           d.DecidedBy,
					"is_recommended":       d.IsRecommended,
					"status":               string(d.Status),
				},
			},
		})

		writeDecisionResponse(w, http.StatusOK, d)
	}
}

// ---------------------------------------------------------------------------
// POST /v1/trees/{tree}/decisions/{id}/undecide
// ---------------------------------------------------------------------------

func undecideDecisionHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if readOnlyGuard(w, r, cfg) {
			return
		}
		if !MustHaveIdentity(w, r) {
			return
		}
		actor, _ := IdentityFromContext(r.Context())
		tree := chi.URLParam(r, "tree")
		if requireTree(w, r, cfg.DB, tree) {
			return
		}
		id, ok := resolveDecisionID(w, r, cfg.DB, tree, chi.URLParam(r, "id"))
		if !ok {
			if id == "" {
				WriteProblem(w, r, NotFound("decision not found"))
			}
			return
		}
		d, ok := loadDecisionOr404(w, r, cfg.DB, id)
		if !ok {
			return
		}

		// Mirror the CLI guard: undecide only valid on decided decisions.
		if d.Status != core.StatusDecided {
			WriteProblem(w, r, Conflict(fmt.Sprintf(
				"undecide is only valid for decided decisions; current status: %s",
				d.Status,
			)))
			return
		}

		expected, abort := matchIfMatch(w, r, d.Rev)
		if abort {
			return
		}

		before := decisionToAuditMap(d)
		d.Status = core.StatusProposed
		d.ActualChoice = ""
		d.ActualChoiceReason = ""
		d.DecidedBy = nil
		d.IsRecommended = false

		ok, _ = writeDecisionAndIndex(w, r, cfg, d, expected)
		if !ok {
			return
		}

		_ = audit.Append(cfg.RepoRoot, core.Event{
			Actor:  actor,
			Action: core.ActionUndecide,
			Kind:   core.KindDecision,
			Tree:   tree,
			ID:     d.ID,
			Payload: core.EventPayload{
				Before: before,
				After:  map[string]any{"status": string(d.Status)},
			},
		})

		writeDecisionResponse(w, http.StatusOK, d)
	}
}

// ---------------------------------------------------------------------------
// POST /v1/trees/{tree}/decisions/{id}/scope-out
// ---------------------------------------------------------------------------

type scopeOutRequest struct {
	Reason string `json:"reason"`
}

func scopeOutDecisionHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if readOnlyGuard(w, r, cfg) {
			return
		}
		if !MustHaveIdentity(w, r) {
			return
		}
		actor, _ := IdentityFromContext(r.Context())
		tree := chi.URLParam(r, "tree")
		if requireTree(w, r, cfg.DB, tree) {
			return
		}
		id, ok := resolveDecisionID(w, r, cfg.DB, tree, chi.URLParam(r, "id"))
		if !ok {
			if id == "" {
				WriteProblem(w, r, NotFound("decision not found"))
			}
			return
		}
		d, ok := loadDecisionOr404(w, r, cfg.DB, id)
		if !ok {
			return
		}

		var req scopeOutRequest
		if r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				WriteProblem(w, r, BadRequest("invalid JSON body: "+err.Error()))
				return
			}
		}

		// Mirror the CLI guard: scope-out is only valid on proposed decisions.
		if d.Status != core.StatusProposed {
			WriteProblem(w, r, Conflict(fmt.Sprintf(
				"scope-out is only valid for proposed decisions; current status: %s",
				d.Status,
			)))
			return
		}

		expected, abort := matchIfMatch(w, r, d.Rev)
		if abort {
			return
		}

		before := decisionToAuditMap(d)
		d.Status = core.StatusOutOfScope
		d.OutOfScopeReason = req.Reason

		ok, _ = writeDecisionAndIndex(w, r, cfg, d, expected)
		if !ok {
			return
		}

		_ = audit.Append(cfg.RepoRoot, core.Event{
			Actor:  actor,
			Action: core.ActionScopeOut,
			Kind:   core.KindDecision,
			Tree:   tree,
			ID:     d.ID,
			Payload: core.EventPayload{
				Before: before,
				After: map[string]any{
					"status":              string(d.Status),
					"out_of_scope_reason": d.OutOfScopeReason,
				},
			},
		})

		writeDecisionResponse(w, http.StatusOK, d)
	}
}

// ---------------------------------------------------------------------------
// POST /v1/trees/{tree}/decisions/{id}/restore
// ---------------------------------------------------------------------------

func restoreDecisionHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if readOnlyGuard(w, r, cfg) {
			return
		}
		if !MustHaveIdentity(w, r) {
			return
		}
		actor, _ := IdentityFromContext(r.Context())
		tree := chi.URLParam(r, "tree")
		if requireTree(w, r, cfg.DB, tree) {
			return
		}
		id, ok := resolveDecisionID(w, r, cfg.DB, tree, chi.URLParam(r, "id"))
		if !ok {
			if id == "" {
				WriteProblem(w, r, NotFound("decision not found"))
			}
			return
		}
		d, ok := loadDecisionOr404(w, r, cfg.DB, id)
		if !ok {
			return
		}
		if d.Status != core.StatusOutOfScope {
			WriteProblem(w, r, Conflict("restore is only valid for out_of_scope decisions; current status: "+string(d.Status)))
			return
		}

		expected, abort := matchIfMatch(w, r, d.Rev)
		if abort {
			return
		}

		before := decisionToAuditMap(d)
		d.Status = core.StatusProposed
		d.OutOfScopeReason = ""

		ok, _ = writeDecisionAndIndex(w, r, cfg, d, expected)
		if !ok {
			return
		}

		_ = audit.Append(cfg.RepoRoot, core.Event{
			Actor:  actor,
			Action: core.ActionRestore,
			Kind:   core.KindDecision,
			Tree:   tree,
			ID:     d.ID,
			Payload: core.EventPayload{
				Before: before,
				After:  map[string]any{"status": string(d.Status)},
			},
		})

		writeDecisionResponse(w, http.StatusOK, d)
	}
}

// ---------------------------------------------------------------------------
// POST /v1/trees/{tree}/decisions/{id}/supersede
// ---------------------------------------------------------------------------

type supersedeRequest struct {
	By string `json:"by"`
}

func supersedeDecisionHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if readOnlyGuard(w, r, cfg) {
			return
		}
		if !MustHaveIdentity(w, r) {
			return
		}
		actor, _ := IdentityFromContext(r.Context())
		tree := chi.URLParam(r, "tree")
		if requireTree(w, r, cfg.DB, tree) {
			return
		}
		id, ok := resolveDecisionID(w, r, cfg.DB, tree, chi.URLParam(r, "id"))
		if !ok {
			if id == "" {
				WriteProblem(w, r, NotFound("decision not found"))
			}
			return
		}
		d, ok := loadDecisionOr404(w, r, cfg.DB, id)
		if !ok {
			return
		}

		var req supersedeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteProblem(w, r, BadRequest("invalid JSON body: "+err.Error()))
			return
		}
		if req.By == "" {
			WriteProblem(w, r, Unprocessable("by is required"))
			return
		}
		if req.By == d.ID {
			WriteProblem(w, r, Unprocessable("a decision cannot supersede itself"))
			return
		}

		other, err := index.GetDecision(cfg.DB, req.By)
		if err != nil {
			WriteProblem(w, r, Internal("look up superseder"))
			return
		}
		if other == nil {
			WriteProblem(w, r, Unprocessable("by decision not found: "+req.By))
			return
		}

		expected, abort := matchIfMatch(w, r, d.Rev)
		if abort {
			return
		}

		before := decisionToAuditMap(d)
		d.Status = core.StatusSuperseded
		// Add superseded_by edge (encoded as a "supersedes" edge from the
		// other decision; we record that "other supersedes d" both ways).
		// On this decision we record a relates_to-like marker; the index
		// schema doesn't have a "superseded_by" type so we use supersedes
		// pointing at the new decision. The supersedes graph carries
		// directionality already (source supersedes target).
		appendUniqueRel(d, core.Relationship{Type: core.RelSupersedes, Target: req.By})

		ok, _ = writeDecisionAndIndex(w, r, cfg, d, expected)
		if !ok {
			return
		}

		// Add the reverse supersedes edge on the new decision.
		appendUniqueRel(other, core.Relationship{Type: core.RelSupersedes, Target: d.ID})
		if _, _ = writeDecisionAndIndex(w, r, cfg, other, ""); !ok {
			// best effort; primary mutation succeeded
			_ = ok
		}

		_ = audit.Append(cfg.RepoRoot, core.Event{
			Actor:  actor,
			Action: core.ActionSupersede,
			Kind:   core.KindDecision,
			Tree:   tree,
			ID:     d.ID,
			Payload: core.EventPayload{
				Before: before,
				After: map[string]any{
					"status": string(d.Status),
					"by":     req.By,
				},
			},
		})

		writeDecisionResponse(w, http.StatusOK, d)
	}
}

// appendUniqueRel adds rel to d.Relationships if not already present.
func appendUniqueRel(d *core.Decision, rel core.Relationship) {
	for _, r := range d.Relationships {
		if r.Type == rel.Type && r.Target == rel.Target {
			return
		}
	}
	d.Relationships = append(d.Relationships, rel)
}

// ---------------------------------------------------------------------------
// POST /v1/trees/{tree}/decisions/{id}/relate
// ---------------------------------------------------------------------------

type relateRequest struct {
	Type   string `json:"type"`
	Target string `json:"target"`
	Note   string `json:"note,omitempty"`
}

func relateDecisionHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if readOnlyGuard(w, r, cfg) {
			return
		}
		if !MustHaveIdentity(w, r) {
			return
		}
		actor, _ := IdentityFromContext(r.Context())
		tree := chi.URLParam(r, "tree")
		if requireTree(w, r, cfg.DB, tree) {
			return
		}
		id, ok := resolveDecisionID(w, r, cfg.DB, tree, chi.URLParam(r, "id"))
		if !ok {
			if id == "" {
				WriteProblem(w, r, NotFound("decision not found"))
			}
			return
		}
		d, ok := loadDecisionOr404(w, r, cfg.DB, id)
		if !ok {
			return
		}

		var req relateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteProblem(w, r, BadRequest("invalid JSON body: "+err.Error()))
			return
		}
		if req.Type == "" || req.Target == "" {
			WriteProblem(w, r, Unprocessable("type and target are required"))
			return
		}
		relType := core.RelationshipType(req.Type)
		if relType == core.RelSupersedes {
			WriteProblem(w, r, Unprocessable("supersedes relationships are managed via /supersede"))
			return
		}
		switch relType {
		case core.RelBlocks, core.RelInfluences, core.RelRelatesTo:
		default:
			WriteProblem(w, r, Unprocessable("invalid relationship type: "+req.Type))
			return
		}
		if req.Target == d.ID {
			WriteProblem(w, r, Unprocessable("a decision cannot relate to itself"))
			return
		}

		// Check target exists.
		other, err := index.GetDecision(cfg.DB, req.Target)
		if err != nil {
			WriteProblem(w, r, Internal("look up target"))
			return
		}
		if other == nil {
			WriteProblem(w, r, Unprocessable("target decision not found: "+req.Target))
			return
		}

		// Cycle check for blocks edges.
		if relType == core.RelBlocks {
			edges, err := loadAllEdges(cfg.DB)
			if err != nil {
				WriteProblem(w, r, Internal("load edges for cycle check"))
				return
			}
			if validate.AddingEdgeWouldCycle(edges, d.ID, req.Target, relType) {
				WriteProblem(w, r, Unprocessable("adding this edge would create a cycle"))
				return
			}
		}

		// Idempotent: if the edge already exists, return the current decision.
		for _, rel := range d.Relationships {
			if rel.Type == relType && rel.Target == req.Target {
				writeDecisionResponse(w, http.StatusOK, d)
				return
			}
		}

		expected, abort := matchIfMatch(w, r, d.Rev)
		if abort {
			return
		}

		before := decisionToAuditMap(d)
		d.Relationships = append(d.Relationships, core.Relationship{
			Type:   relType,
			Target: req.Target,
		})

		ok, _ = writeDecisionAndIndex(w, r, cfg, d, expected)
		if !ok {
			return
		}

		_ = audit.Append(cfg.RepoRoot, core.Event{
			Actor:  actor,
			Action: core.ActionRelate,
			Kind:   core.KindRelationship,
			Tree:   tree,
			ID:     d.ID,
			Payload: core.EventPayload{
				Before: before,
				After: map[string]any{
					"type":   string(relType),
					"target": req.Target,
					"note":   req.Note,
				},
			},
		})

		writeDecisionResponse(w, http.StatusOK, d)
	}
}

// loadAllEdges returns every relationship in the index, suitable for the
// cycle checker.
func loadAllEdges(db *index.DB) ([]validate.Edge, error) {
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

// ---------------------------------------------------------------------------
// POST /v1/trees/{tree}/decisions/{id}/unrelate
// ---------------------------------------------------------------------------

type unrelateRequest struct {
	Type   string `json:"type"`
	Target string `json:"target"`
}

func unrelateDecisionHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if readOnlyGuard(w, r, cfg) {
			return
		}
		if !MustHaveIdentity(w, r) {
			return
		}
		actor, _ := IdentityFromContext(r.Context())
		tree := chi.URLParam(r, "tree")
		if requireTree(w, r, cfg.DB, tree) {
			return
		}
		id, ok := resolveDecisionID(w, r, cfg.DB, tree, chi.URLParam(r, "id"))
		if !ok {
			if id == "" {
				WriteProblem(w, r, NotFound("decision not found"))
			}
			return
		}
		d, ok := loadDecisionOr404(w, r, cfg.DB, id)
		if !ok {
			return
		}

		var req unrelateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteProblem(w, r, BadRequest("invalid JSON body: "+err.Error()))
			return
		}
		if req.Type == "" || req.Target == "" {
			WriteProblem(w, r, Unprocessable("type and target are required"))
			return
		}

		expected, abort := matchIfMatch(w, r, d.Rev)
		if abort {
			return
		}

		before := decisionToAuditMap(d)
		filtered := d.Relationships[:0]
		removed := false
		for _, rel := range d.Relationships {
			if string(rel.Type) == req.Type && rel.Target == req.Target {
				removed = true
				continue
			}
			filtered = append(filtered, rel)
		}
		d.Relationships = filtered
		if !removed {
			// Idempotent: still return current state.
			writeDecisionResponse(w, http.StatusOK, d)
			return
		}

		ok, _ = writeDecisionAndIndex(w, r, cfg, d, expected)
		if !ok {
			return
		}

		_ = audit.Append(cfg.RepoRoot, core.Event{
			Actor:  actor,
			Action: core.ActionUnrelate,
			Kind:   core.KindRelationship,
			Tree:   tree,
			ID:     d.ID,
			Payload: core.EventPayload{
				Before: before,
				After: map[string]any{
					"type":   req.Type,
					"target": req.Target,
				},
			},
		})

		writeDecisionResponse(w, http.StatusOK, d)
	}
}

// ---------------------------------------------------------------------------
// GET /v1/trees/{tree}/decisions/{id}/history
// ---------------------------------------------------------------------------

type historyResponse struct {
	Events []core.Event `json:"events"`
}

func historyDecisionHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tree := chi.URLParam(r, "tree")
		if requireTree(w, r, cfg.DB, tree) {
			return
		}
		id, ok := resolveDecisionID(w, r, cfg.DB, tree, chi.URLParam(r, "id"))
		if !ok {
			if id == "" {
				WriteProblem(w, r, NotFound("decision not found"))
			}
			return
		}

		f := audit.Filter{Tree: tree, TargetID: id}
		if v := r.URL.Query().Get("since"); v != "" {
			t, err := parseRelativeDuration(v)
			if err != nil {
				WriteProblem(w, r, BadRequest("since: "+err.Error()))
				return
			}
			f.Since = t
		}

		events, err := audit.Read(cfg.RepoRoot, f)
		if err != nil {
			WriteProblem(w, r, Internal("read audit log"))
			return
		}
		if events == nil {
			events = []core.Event{}
		}
		writeJSON(w, http.StatusOK, historyResponse{Events: events})
	}
}
