//go:build sqlite_fts5

package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/config"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/identity"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/server"
	"github.com/cgould/dtree/internal/storage"
)

// ---------------------------------------------------------------------------
// Test infrastructure
// ---------------------------------------------------------------------------

// treeTestEnv holds everything needed for a trees-endpoint test.
type treeTestEnv struct {
	srv      *http.Server
	repoRoot string
	db       *index.DB
}

// newTreeTestEnv creates a fresh repo with index, a "cam" actor, and a
// fully-configured server. The caller should defer env.close().
func newTreeTestEnv(t *testing.T) *treeTestEnv {
	t.Helper()

	repoRoot := t.TempDir()

	// Create .decisions/ layout.
	decisionsDir := filepath.Join(repoRoot, ".decisions")
	if err := os.MkdirAll(filepath.Join(decisionsDir, "audit"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write trees.yaml (empty registry).
	treesPath := filepath.Join(decisionsDir, storage.TreesFileName)
	if err := storage.WriteTrees(treesPath, &storage.TreesFile{}); err != nil {
		t.Fatalf("write trees.yaml: %v", err)
	}

	// Write actors.yaml with "cam".
	actorsPath := filepath.Join(decisionsDir, storage.ActorsFileName)
	af := &storage.ActorsFile{
		Actors: []core.Actor{
			{Handle: "cam", Name: "Cameron", Kind: core.ActorHuman, Active: true},
		},
	}
	if err := storage.WriteActors(actorsPath, af); err != nil {
		t.Fatalf("write actors.yaml: %v", err)
	}

	// Open index.
	dbPath := filepath.Join(decisionsDir, ".index.db")
	db, err := index.Open(dbPath)
	if err != nil {
		t.Fatalf("index.Open: %v", err)
	}

	// Build resolver.
	cfg := &config.Resolved{IdentitySrc: config.SourceDefault}
	resolver := identity.NewResolver(repoRoot, cfg)

	// Build server.
	srv := server.New(server.Config{
		Listen:   ":0",
		RepoRoot: repoRoot,
		DB:       db,
		Resolver: resolver,
		Trust:    server.TrustLocalhostOnly,
	})

	t.Cleanup(func() { _ = db.Close() })

	return &treeTestEnv{srv: srv, repoRoot: repoRoot, db: db}
}

// do executes a request against the test server and returns the response.
func (e *treeTestEnv) do(t *testing.T, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var bodyBytes []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		bodyBytes = b
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(bodyBytes))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	e.srv.Handler.ServeHTTP(w, req)
	return w
}

// withActor adds X-Dtree-As: cam to a header map.
func withActor(handle string) map[string]string {
	return map[string]string{"X-Dtree-As": handle}
}

// insertTestTree inserts a tree directly into the index and writes tree.yaml.
func (e *treeTestEnv) insertTestTree(t *testing.T, slug, title string, archived bool) {
	t.Helper()

	treeDir := filepath.Join(e.repoRoot, ".decisions", slug)
	for _, d := range []string{
		filepath.Join(treeDir, "decisions"),
		filepath.Join(treeDir, "audit"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	tree := &core.Tree{
		Slug:          slug,
		SchemaVersion: core.SchemaVersion,
		Title:         title,
		Archived:      archived,
		CreatedAt:     time.Now().UTC(),
	}
	tree.Layout.Direction = "TB"

	treeMetaPath := filepath.Join(treeDir, storage.TreeMetaFileName)
	if err := storage.WriteTree(treeMetaPath, tree); err != nil {
		t.Fatalf("write tree.yaml: %v", err)
	}

	// Insert into index.
	direction := "TB"
	archivedInt := 0
	if archived {
		archivedInt = 1
	}
	_, err := e.db.Conn().Exec(
		`INSERT OR REPLACE INTO trees(slug, title, description, archived, created_at, layout_direction, schema_version)
		 VALUES(?, ?, ?, ?, ?, ?, ?)`,
		slug, title, "", archivedInt, tree.CreatedAt.UTC().Format(time.RFC3339), direction, core.SchemaVersion,
	)
	if err != nil {
		t.Fatalf("insert tree %s: %v", slug, err)
	}

	// Update trees.yaml.
	treesPath := filepath.Join(e.repoRoot, ".decisions", storage.TreesFileName)
	tf, err := storage.ReadTrees(treesPath)
	if err != nil {
		tf = &storage.TreesFile{}
	}
	tf.Trees = append(tf.Trees, slug)
	if err := storage.WriteTrees(treesPath, tf); err != nil {
		t.Fatalf("write trees.yaml: %v", err)
	}
}

// insertTestDecision inserts a minimal decision into the index for tree.
func (e *treeTestEnv) insertTestDecision(t *testing.T, treeSlug, id string) {
	t.Helper()
	_, err := e.db.Conn().Exec(
		`INSERT INTO decisions(id, tree, slug, summary, status, priority, creator,
		  description, assignee, recommended_summary, recommended_full, recommended_by,
		  actual_choice, actual_choice_reason, out_of_scope_reason, schema_version, rev, content_sha256, deleted)
		 VALUES(?,?,?,?,?,?,?, ?,?,?,?,?, ?,?,?, ?,?,?,?)`,
		id, treeSlug, "test-decision", "Test decision", "proposed", "medium", "cam",
		"", "", "", "", "",
		"", "", "",
		core.SchemaVersion, "rev1", "", 0,
	)
	if err != nil {
		t.Fatalf("insert decision: %v", err)
	}
}

// ---------------------------------------------------------------------------
// GET /v1/trees — list
// ---------------------------------------------------------------------------

func TestListTreesEmpty(t *testing.T) {
	e := newTreeTestEnv(t)
	w := e.do(t, http.MethodGet, "/v1/trees", nil, nil)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	trees, ok := resp["trees"]
	if !ok {
		t.Fatal("response missing 'trees' key")
	}
	arr, ok := trees.([]any)
	if !ok {
		t.Fatalf("trees is not an array: %T", trees)
	}
	if len(arr) != 0 {
		t.Errorf("expected empty trees array, got %d entries", len(arr))
	}
}

func TestListTreesWithEntries(t *testing.T) {
	e := newTreeTestEnv(t)
	e.insertTestTree(t, "alpha", "Alpha Tree", false)
	e.insertTestTree(t, "beta", "Beta Tree", false)

	w := e.do(t, http.MethodGet, "/v1/trees", nil, nil)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", w.Code)
	}

	var resp struct {
		Trees []struct {
			Slug     string `json:"slug"`
			Title    string `json:"title"`
			Archived bool   `json:"archived"`
		} `json:"trees"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Trees) != 2 {
		t.Fatalf("expected 2 trees, got %d", len(resp.Trees))
	}
	// Ordered by slug ASC.
	if resp.Trees[0].Slug != "alpha" || resp.Trees[1].Slug != "beta" {
		t.Errorf("unexpected order: %v", resp.Trees)
	}
}

func TestListTreesIncludeArchived(t *testing.T) {
	e := newTreeTestEnv(t)
	e.insertTestTree(t, "active", "Active Tree", false)
	e.insertTestTree(t, "gone", "Archived Tree", true)

	// Default: archived hidden.
	w := e.do(t, http.MethodGet, "/v1/trees", nil, nil)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", w.Code)
	}
	var resp1 struct {
		Trees []struct{ Slug string } `json:"trees"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp1)
	if len(resp1.Trees) != 1 || resp1.Trees[0].Slug != "active" {
		t.Errorf("expected [active], got %v", resp1.Trees)
	}

	// With include_archived=true: both visible.
	w2 := e.do(t, http.MethodGet, "/v1/trees?include_archived=true", nil, nil)
	if w2.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", w2.Code)
	}
	var resp2 struct {
		Trees []struct{ Slug string } `json:"trees"`
	}
	_ = json.NewDecoder(w2.Body).Decode(&resp2)
	if len(resp2.Trees) != 2 {
		t.Errorf("expected 2 trees with include_archived, got %d", len(resp2.Trees))
	}
}

// ---------------------------------------------------------------------------
// GET /v1/trees/{tree}
// ---------------------------------------------------------------------------

func TestGetTreeNotFound(t *testing.T) {
	e := newTreeTestEnv(t)
	w := e.do(t, http.MethodGet, "/v1/trees/nonexistent", nil, nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d; want 404", w.Code)
	}
	var p server.Problem
	if err := json.NewDecoder(w.Body).Decode(&p); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if p.Status != http.StatusNotFound {
		t.Errorf("problem.status = %d; want 404", p.Status)
	}
}

func TestGetTreeFound(t *testing.T) {
	e := newTreeTestEnv(t)
	e.insertTestTree(t, "backend", "Backend Decisions", false)

	w := e.do(t, http.MethodGet, "/v1/trees/backend", nil, nil)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", w.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if slug, _ := body["slug"].(string); slug != "backend" {
		t.Errorf("slug = %q; want %q", slug, "backend")
	}
	if title, _ := body["title"].(string); title != "Backend Decisions" {
		t.Errorf("title = %q; want %q", title, "Backend Decisions")
	}
	if _, ok := body["decisions"]; !ok {
		t.Error("response missing 'decisions' count")
	}
}

// ---------------------------------------------------------------------------
// POST /v1/trees
// ---------------------------------------------------------------------------

func TestCreateTreeSuccess(t *testing.T) {
	e := newTreeTestEnv(t)

	body := map[string]string{"slug": "myapp", "title": "My App"}
	w := e.do(t, http.MethodPost, "/v1/trees", body, withActor("cam"))

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d; want 201; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if slug, _ := resp["slug"].(string); slug != "myapp" {
		t.Errorf("slug = %q; want %q", slug, "myapp")
	}

	// Confirm tree appears in GET /v1/trees.
	w2 := e.do(t, http.MethodGet, "/v1/trees", nil, nil)
	var list struct {
		Trees []struct{ Slug string } `json:"trees"`
	}
	_ = json.NewDecoder(w2.Body).Decode(&list)
	found := false
	for _, tr := range list.Trees {
		if tr.Slug == "myapp" {
			found = true
		}
	}
	if !found {
		t.Error("created tree not found in GET /v1/trees")
	}
}

func TestCreateTreeMissingIdentity(t *testing.T) {
	e := newTreeTestEnv(t)

	body := map[string]string{"slug": "myapp", "title": "My App"}
	w := e.do(t, http.MethodPost, "/v1/trees", body, nil) // no X-Dtree-As

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q; want application/problem+json", ct)
	}
}

func TestCreateTreeInvalidSlug(t *testing.T) {
	e := newTreeTestEnv(t)

	for _, bad := range []string{"Bad-Slug", "1startswithnumber", "has space", ""} {
		body := map[string]string{"slug": bad, "title": "Test"}
		w := e.do(t, http.MethodPost, "/v1/trees", body, withActor("cam"))
		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("slug %q: status = %d; want 422", bad, w.Code)
		}
	}
}

func TestCreateTreeAlreadyExists(t *testing.T) {
	e := newTreeTestEnv(t)
	e.insertTestTree(t, "existing", "Existing", false)

	body := map[string]string{"slug": "existing", "title": "Duplicate"}
	w := e.do(t, http.MethodPost, "/v1/trees", body, withActor("cam"))

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d; want 409", w.Code)
	}
}

func TestCreateTreeEmitsAuditEvent(t *testing.T) {
	e := newTreeTestEnv(t)

	body := map[string]string{"slug": "auditme", "title": "Audit Test"}
	w := e.do(t, http.MethodPost, "/v1/trees", body, withActor("cam"))
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}

	// Read audit log and look for tree_create event.
	events, err := audit.Read(e.repoRoot, audit.Filter{
		Action: core.ActionTreeCreate,
	})
	if err != nil {
		t.Fatalf("audit.Read: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected tree_create audit event; got none")
	}
	ev := events[0]
	if ev.Actor != "cam" {
		t.Errorf("event.actor = %q; want %q", ev.Actor, "cam")
	}
	if ev.ID != "auditme" {
		t.Errorf("event.id = %q; want %q", ev.ID, "auditme")
	}
}

// ---------------------------------------------------------------------------
// PATCH /v1/trees/{tree}
// ---------------------------------------------------------------------------

func TestPatchTreeSuccess(t *testing.T) {
	e := newTreeTestEnv(t)
	e.insertTestTree(t, "patchtree", "Original Title", false)

	body := map[string]any{"title": "Updated Title", "description": "New desc"}
	w := e.do(t, http.MethodPatch, "/v1/trees/patchtree", body, withActor("cam"))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d; want 200; body: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if title, _ := resp["title"].(string); title != "Updated Title" {
		t.Errorf("title = %q; want %q", title, "Updated Title")
	}

	// Confirm persists in GET.
	w2 := e.do(t, http.MethodGet, "/v1/trees/patchtree", nil, nil)
	var resp2 map[string]any
	_ = json.NewDecoder(w2.Body).Decode(&resp2)
	if title, _ := resp2["title"].(string); title != "Updated Title" {
		t.Errorf("persisted title = %q; want %q", title, "Updated Title")
	}
}

func TestPatchTreeNotFound(t *testing.T) {
	e := newTreeTestEnv(t)

	body := map[string]any{"title": "X"}
	w := e.do(t, http.MethodPatch, "/v1/trees/ghost", body, withActor("cam"))

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d; want 404", w.Code)
	}
}

func TestPatchTreeMissingIdentity(t *testing.T) {
	e := newTreeTestEnv(t)
	e.insertTestTree(t, "patchtree2", "Title", false)

	body := map[string]any{"title": "X"}
	w := e.do(t, http.MethodPatch, "/v1/trees/patchtree2", body, nil)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", w.Code)
	}
}

// ---------------------------------------------------------------------------
// DELETE /v1/trees/{tree}
// ---------------------------------------------------------------------------

func TestDeleteTreeRequiresConfirm(t *testing.T) {
	e := newTreeTestEnv(t)
	e.insertTestTree(t, "deltree", "To Delete", false)

	// No confirm param → 400.
	w := e.do(t, http.MethodDelete, "/v1/trees/deltree", nil, withActor("cam"))
	if w.Code != http.StatusBadRequest {
		t.Errorf("no confirm: status = %d; want 400", w.Code)
	}

	// Wrong confirm value → 400.
	w2 := e.do(t, http.MethodDelete, "/v1/trees/deltree?confirm=wrongslug", nil, withActor("cam"))
	if w2.Code != http.StatusBadRequest {
		t.Errorf("wrong confirm: status = %d; want 400", w2.Code)
	}
}

func TestDeleteTreeWithDecisions(t *testing.T) {
	e := newTreeTestEnv(t)
	e.insertTestTree(t, "hastree", "Has Decisions", false)
	e.insertTestDecision(t, "hastree", "01JDECISION000000000000001")

	// Without cascade → refuses.
	w := e.do(t, http.MethodDelete, "/v1/trees/hastree?confirm=hastree", nil, withActor("cam"))
	if w.Code != http.StatusBadRequest {
		t.Errorf("no cascade: status = %d; want 400", w.Code)
	}

	// With cascade=true → 204.
	w2 := e.do(t, http.MethodDelete, "/v1/trees/hastree?confirm=hastree&cascade=true", nil, withActor("cam"))
	if w2.Code != http.StatusNoContent {
		t.Errorf("with cascade: status = %d; want 204; body: %s", w2.Code, w2.Body.String())
	}

	// Confirm gone from GET.
	w3 := e.do(t, http.MethodGet, "/v1/trees/hastree", nil, nil)
	if w3.Code != http.StatusNotFound {
		t.Errorf("after delete: status = %d; want 404", w3.Code)
	}
}

func TestDeleteTreeEmitsEvent(t *testing.T) {
	e := newTreeTestEnv(t)
	e.insertTestTree(t, "evtree", "Event Tree", false)

	w := e.do(t, http.MethodDelete, "/v1/trees/evtree?confirm=evtree", nil, withActor("cam"))
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}

	events, err := audit.Read(e.repoRoot, audit.Filter{
		Action: core.ActionTreeDelete,
	})
	if err != nil {
		t.Fatalf("audit.Read: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected tree_delete audit event; got none")
	}
	ev := events[0]
	if ev.Actor != "cam" {
		t.Errorf("actor = %q; want %q", ev.Actor, "cam")
	}
	if ev.ID != "evtree" {
		t.Errorf("id = %q; want %q", ev.ID, "evtree")
	}
}

func TestDeleteTreeNotFound(t *testing.T) {
	e := newTreeTestEnv(t)

	w := e.do(t, http.MethodDelete, "/v1/trees/ghost?confirm=ghost", nil, withActor("cam"))
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d; want 404", w.Code)
	}
}

func TestDeleteTreeMissingIdentity(t *testing.T) {
	e := newTreeTestEnv(t)
	e.insertTestTree(t, "authd", "Needs Auth", false)

	w := e.do(t, http.MethodDelete, "/v1/trees/authd?confirm=authd", nil, nil)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", w.Code)
	}
}

func TestDeleteTreeSuccessNoDecisions(t *testing.T) {
	e := newTreeTestEnv(t)
	e.insertTestTree(t, "empty", "Empty Tree", false)

	w := e.do(t, http.MethodDelete, "/v1/trees/empty?confirm=empty", nil, withActor("cam"))
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d; want 204; body: %s", w.Code, w.Body.String())
	}
}
