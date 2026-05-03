package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/storage"
)

// treeSlugRE is the allowed pattern for tree slugs (mirrors cli/tree.go).
var treeSlugRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// ---------------------------------------------------------------------------
// Response shapes
// ---------------------------------------------------------------------------

// treeListItem is the compact shape returned by GET /v1/trees.
type treeListItem struct {
	Slug      string `json:"slug"`
	Title     string `json:"title,omitempty"`
	Decisions int    `json:"decisions"`
	Archived  bool   `json:"archived"`
}

// treeListResponse is the envelope for GET /v1/trees.
type treeListResponse struct {
	Trees []treeListItem `json:"trees"`
}

// treeDetailResponse is the full shape for GET /v1/trees/{tree}.
type treeDetailResponse struct {
	Slug          string    `json:"slug"`
	SchemaVersion int       `json:"schema_version"`
	Title         string    `json:"title,omitempty"`
	Description   string    `json:"description,omitempty"`
	Archived      bool      `json:"archived"`
	CreatedAt     time.Time `json:"created_at"`
	Layout        struct {
		Direction string `json:"direction,omitempty"`
	} `json:"layout,omitempty"`
	Decisions int `json:"decisions"`
}

// ---------------------------------------------------------------------------
// Mount helper called from server.go
// ---------------------------------------------------------------------------

// mountTrees registers the /v1/trees routes onto r.
func mountTrees(r chi.Router, cfg Config) {
	r.Route("/trees", func(r chi.Router) {
		r.Get("/", listTreesHandler(cfg))
		r.Post("/", createTreeHandler(cfg))
		r.Route("/{tree}", func(r chi.Router) {
			r.Get("/", getTreeHandler(cfg))
			r.Patch("/", patchTreeHandler(cfg))
			r.Delete("/", deleteTreeHandler(cfg))
		})
	})
}

// ---------------------------------------------------------------------------
// GET /v1/trees
// ---------------------------------------------------------------------------

func listTreesHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		includeArchived := r.URL.Query().Get("include_archived") == "true"

		rows, err := queryTreeList(cfg.DB, includeArchived)
		if err != nil {
			WriteProblem(w, r, Internal("failed to list trees"))
			return
		}

		writeJSON(w, http.StatusOK, treeListResponse{Trees: rows})
	}
}

// queryTreeList fetches tree list rows from the index.
func queryTreeList(db *index.DB, includeArchived bool) ([]treeListItem, error) {
	query := `
		SELECT t.slug, t.title, t.archived,
		       COUNT(d.id) AS decision_count
		FROM trees t
		LEFT JOIN decisions d ON d.tree = t.slug AND d.deleted = 0
		%s
		GROUP BY t.slug
		ORDER BY t.slug`

	where := ""
	if !includeArchived {
		where = "WHERE t.archived = 0"
	}
	query = queryf(query, where)

	sqlRows, err := db.Conn().Query(query)
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()

	var out []treeListItem
	for sqlRows.Next() {
		var item treeListItem
		var archived int
		if err := sqlRows.Scan(&item.Slug, &item.Title, &archived, &item.Decisions); err != nil {
			return nil, err
		}
		item.Archived = archived == 1
		out = append(out, item)
	}
	if out == nil {
		out = []treeListItem{}
	}
	return out, sqlRows.Err()
}

// queryf is a tiny helper so we can embed a format string without importing fmt in the hot path.
func queryf(template, where string) string {
	// We only ever substitute one %s placeholder (the WHERE clause).
	result := make([]byte, 0, len(template))
	for i := 0; i < len(template); i++ {
		if template[i] == '%' && i+1 < len(template) && template[i+1] == 's' {
			result = append(result, []byte(where)...)
			i++ // skip 's'
			continue
		}
		result = append(result, template[i])
	}
	return string(result)
}

// ---------------------------------------------------------------------------
// GET /v1/trees/{tree}
// ---------------------------------------------------------------------------

func getTreeHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := chi.URLParam(r, "tree")

		t, err := treeFromIndex(cfg.DB, slug)
		if err == sql.ErrNoRows {
			WriteProblem(w, r, NotFound("tree not found: "+slug))
			return
		}
		if err != nil {
			WriteProblem(w, r, Internal("failed to read tree"))
			return
		}

		count, err := countDecisionsInIndex(cfg.DB, slug)
		if err != nil {
			WriteProblem(w, r, Internal("failed to count decisions"))
			return
		}

		resp := treeDetailResponse{
			Slug:          t.Slug,
			SchemaVersion: t.SchemaVersion,
			Title:         t.Title,
			Description:   t.Description,
			Archived:      t.Archived,
			CreatedAt:     t.CreatedAt,
			Decisions:     count,
		}
		resp.Layout.Direction = t.Layout.Direction
		writeJSON(w, http.StatusOK, resp)
	}
}

// ---------------------------------------------------------------------------
// POST /v1/trees
// ---------------------------------------------------------------------------

type createTreeRequest struct {
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

func createTreeHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !MustHaveIdentity(w, r) {
			return
		}
		actor, _ := IdentityFromContext(r.Context())

		var req createTreeRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			WriteProblem(w, r, BadRequest("invalid request body: "+err.Error()))
			return
		}

		if !treeSlugRE.MatchString(req.Slug) {
			WriteProblem(w, r, Unprocessable("invalid slug: must match ^[a-z][a-z0-9-]{0,63}$"))
			return
		}

		// Check if tree already exists in index.
		exists, err := treeExistsInIndex(cfg.DB, req.Slug)
		if err != nil {
			WriteProblem(w, r, Internal("failed to check tree existence"))
			return
		}
		if exists {
			WriteProblem(w, r, Conflict("tree already exists: "+req.Slug))
			return
		}

		// Create directory structure.
		treeDir := filepath.Join(cfg.RepoRoot, ".decisions", req.Slug)
		for _, dir := range []string{
			filepath.Join(treeDir, "decisions"),
			filepath.Join(treeDir, "audit"),
		} {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				WriteProblem(w, r, Internal("failed to create tree directory"))
				return
			}
		}

		tree := &core.Tree{
			Slug:          req.Slug,
			SchemaVersion: core.SchemaVersion,
			Title:         req.Title,
			Description:   req.Description,
			CreatedAt:     time.Now().UTC(),
		}
		tree.Layout.Direction = "TB"

		// Write tree.yaml.
		treeMetaPath := filepath.Join(treeDir, storage.TreeMetaFileName)
		if err := storage.WriteTree(treeMetaPath, tree); err != nil {
			WriteProblem(w, r, Internal("failed to write tree metadata"))
			return
		}

		// Update trees.yaml.
		treesPath := filepath.Join(cfg.RepoRoot, ".decisions", storage.TreesFileName)
		tf, err := storage.ReadTrees(treesPath)
		if err != nil {
			tf = &storage.TreesFile{}
		}
		tf.Trees = append(tf.Trees, req.Slug)
		sort.Strings(tf.Trees)
		if err := storage.WriteTrees(treesPath, tf); err != nil {
			WriteProblem(w, r, Internal("failed to update trees registry"))
			return
		}

		// Insert into index.
		if err := insertTreeRowInDB(cfg.DB, tree); err != nil {
			WriteProblem(w, r, Internal("failed to index tree"))
			return
		}

		// Emit audit event.
		ev := core.Event{
			Actor:  actor,
			Action: core.ActionTreeCreate,
			Kind:   core.KindTree,
			ID:     req.Slug,
			Payload: core.EventPayload{
				After: map[string]any{
					"slug":        tree.Slug,
					"title":       tree.Title,
					"description": tree.Description,
					"archived":    tree.Archived,
					"created_at":  tree.CreatedAt.Format(time.RFC3339),
				},
			},
		}
		if err := audit.Append(cfg.RepoRoot, ev); err != nil {
			// Audit failure is non-fatal but should be logged; the tree was created.
			_ = err
		}

		resp := treeDetailResponse{
			Slug:          tree.Slug,
			SchemaVersion: tree.SchemaVersion,
			Title:         tree.Title,
			Description:   tree.Description,
			Archived:      tree.Archived,
			CreatedAt:     tree.CreatedAt,
			Decisions:     0,
		}
		resp.Layout.Direction = tree.Layout.Direction
		writeJSON(w, http.StatusCreated, resp)
	}
}

// ---------------------------------------------------------------------------
// PATCH /v1/trees/{tree}
// ---------------------------------------------------------------------------

// patchTreeRequest contains mutable fields. All are pointers so we can
// distinguish "not provided" from "set to zero value".
type patchTreeRequest struct {
	Title       *string      `json:"title"`
	Description *string      `json:"description"`
	Layout      *patchLayout `json:"layout"`
}

type patchLayout struct {
	Direction *string `json:"direction"`
}

func patchTreeHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !MustHaveIdentity(w, r) {
			return
		}
		actor, _ := IdentityFromContext(r.Context())
		slug := chi.URLParam(r, "tree")

		t, err := treeFromIndex(cfg.DB, slug)
		if err == sql.ErrNoRows {
			WriteProblem(w, r, NotFound("tree not found: "+slug))
			return
		}
		if err != nil {
			WriteProblem(w, r, Internal("failed to read tree"))
			return
		}

		var req patchTreeRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			WriteProblem(w, r, BadRequest("invalid request body: "+err.Error()))
			return
		}

		// Apply mutations.
		before := map[string]any{
			"title":            t.Title,
			"description":      t.Description,
			"layout_direction": t.Layout.Direction,
		}

		if req.Title != nil {
			t.Title = *req.Title
		}
		if req.Description != nil {
			t.Description = *req.Description
		}
		if req.Layout != nil && req.Layout.Direction != nil {
			t.Layout.Direction = *req.Layout.Direction
		}

		// Write updated tree.yaml.
		treeMetaPath := filepath.Join(cfg.RepoRoot, ".decisions", slug, storage.TreeMetaFileName)
		if err := storage.WriteTree(treeMetaPath, t); err != nil {
			WriteProblem(w, r, Internal("failed to write tree metadata"))
			return
		}

		// Update index.
		direction := t.Layout.Direction
		if direction == "" {
			direction = "TB"
		}
		if _, err := cfg.DB.Conn().Exec(
			`UPDATE trees SET title=?, description=?, layout_direction=? WHERE slug=?`,
			t.Title, t.Description, direction, slug,
		); err != nil {
			WriteProblem(w, r, Internal("failed to update tree index"))
			return
		}

		// Emit audit event.
		ev := core.Event{
			Actor:  actor,
			Action: core.ActionUpdate,
			Kind:   core.KindTree,
			ID:     slug,
			Payload: core.EventPayload{
				Before: before,
				After: map[string]any{
					"title":            t.Title,
					"description":      t.Description,
					"layout_direction": t.Layout.Direction,
				},
			},
		}
		if err := audit.Append(cfg.RepoRoot, ev); err != nil {
			_ = err
		}

		count, _ := countDecisionsInIndex(cfg.DB, slug)

		resp := treeDetailResponse{
			Slug:          t.Slug,
			SchemaVersion: t.SchemaVersion,
			Title:         t.Title,
			Description:   t.Description,
			Archived:      t.Archived,
			CreatedAt:     t.CreatedAt,
			Decisions:     count,
		}
		resp.Layout.Direction = t.Layout.Direction
		writeJSON(w, http.StatusOK, resp)
	}
}

// ---------------------------------------------------------------------------
// DELETE /v1/trees/{tree}
// ---------------------------------------------------------------------------

func deleteTreeHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !MustHaveIdentity(w, r) {
			return
		}
		actor, _ := IdentityFromContext(r.Context())
		slug := chi.URLParam(r, "tree")

		// Confirmation: ?confirm=<slug>
		confirm := r.URL.Query().Get("confirm")
		if confirm == "" {
			WriteProblem(w, r, BadRequest("missing required query parameter ?confirm=<slug>"))
			return
		}
		if confirm != slug {
			WriteProblem(w, r, BadRequest("confirm value does not match tree slug"))
			return
		}

		cascade := r.URL.Query().Get("cascade") == "true"

		t, err := treeFromIndex(cfg.DB, slug)
		if err == sql.ErrNoRows {
			WriteProblem(w, r, NotFound("tree not found: "+slug))
			return
		}
		if err != nil {
			WriteProblem(w, r, Internal("failed to read tree"))
			return
		}

		// Check for decisions.
		count, err := countDecisionsInIndex(cfg.DB, slug)
		if err != nil {
			WriteProblem(w, r, Internal("failed to count decisions"))
			return
		}
		if count > 0 && !cascade {
			WriteProblem(w, r, BadRequest("tree has decisions; add ?cascade=true to delete them"))
			return
		}

		// Emit audit event before removal (need tree data).
		ev := core.Event{
			Actor:  actor,
			Action: core.ActionTreeDelete,
			Kind:   core.KindTree,
			ID:     slug,
			Payload: core.EventPayload{
				Before: map[string]any{
					"slug":        t.Slug,
					"title":       t.Title,
					"description": t.Description,
					"archived":    t.Archived,
					"created_at":  t.CreatedAt.Format(time.RFC3339),
				},
			},
		}
		if err := audit.Append(cfg.RepoRoot, ev); err != nil {
			_ = err
		}

		// Remove from index (FK cascade handles decisions, relationships, etc.).
		if _, err := cfg.DB.Conn().Exec(`DELETE FROM trees WHERE slug=?`, slug); err != nil {
			WriteProblem(w, r, Internal("failed to remove tree from index"))
			return
		}

		// Remove directory.
		treeDir := filepath.Join(cfg.RepoRoot, ".decisions", slug)
		if err := os.RemoveAll(treeDir); err != nil {
			WriteProblem(w, r, Internal("failed to remove tree directory"))
			return
		}

		// Update trees.yaml.
		treesPath := filepath.Join(cfg.RepoRoot, ".decisions", storage.TreesFileName)
		if tf, err := storage.ReadTrees(treesPath); err == nil {
			filtered := tf.Trees[:0]
			for _, s := range tf.Trees {
				if s != slug {
					filtered = append(filtered, s)
				}
			}
			tf.Trees = filtered
			_ = storage.WriteTrees(treesPath, tf)
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// ---------------------------------------------------------------------------
// Shared SQL helpers
// ---------------------------------------------------------------------------

// treeFromIndex reads a tree row from the index by slug.
// Returns sql.ErrNoRows if the slug is not found.
func treeFromIndex(db *index.DB, slug string) (*core.Tree, error) {
	var t core.Tree
	var archived int
	var createdAt, direction string
	err := db.Conn().QueryRow(
		`SELECT slug, title, description, archived, created_at, layout_direction, schema_version
		 FROM trees WHERE slug=?`, slug,
	).Scan(&t.Slug, &t.Title, &t.Description, &archived, &createdAt, &direction, &t.SchemaVersion)
	if err != nil {
		return nil, err
	}
	t.Archived = archived == 1
	t.Layout.Direction = direction
	if ts, err := time.Parse(time.RFC3339, createdAt); err == nil {
		t.CreatedAt = ts
	}
	return &t, nil
}

// treeExistsInIndex reports whether a tree slug is in the index.
func treeExistsInIndex(db *index.DB, slug string) (bool, error) {
	_, err := treeFromIndex(db, slug)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// countDecisionsInIndex returns the number of non-deleted decisions for slug.
func countDecisionsInIndex(db *index.DB, treeSlug string) (int, error) {
	var count int
	err := db.Conn().QueryRow(
		`SELECT COUNT(*) FROM decisions WHERE tree=? AND deleted=0`, treeSlug,
	).Scan(&count)
	return count, err
}

// insertTreeRowInDB inserts t into the SQLite trees table.
func insertTreeRowInDB(db *index.DB, t *core.Tree) error {
	direction := t.Layout.Direction
	if direction == "" {
		direction = "TB"
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
		t.CreatedAt.UTC().Format(time.RFC3339), direction, sv,
	)
	return err
}

// ---------------------------------------------------------------------------
// JSON response helper
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
