//go:build sqlite_fts5

package server_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
// Test environment
// ---------------------------------------------------------------------------

type decisionEnv struct {
	srv      *http.Server
	repoRoot string
	db       *index.DB
}

// newDecisionEnv spins up a server with a single tree "demo" and an actor
// "cam". readOnly toggles cfg.ReadOnly.
func newDecisionEnv(t *testing.T, readOnly bool) *decisionEnv {
	t.Helper()
	repoRoot := t.TempDir()

	decisionsDir := filepath.Join(repoRoot, ".decisions")
	if err := os.MkdirAll(filepath.Join(decisionsDir, "audit"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// trees.yaml registry.
	if err := storage.WriteTrees(filepath.Join(decisionsDir, storage.TreesFileName),
		&storage.TreesFile{Trees: []string{"demo"}}); err != nil {
		t.Fatalf("write trees.yaml: %v", err)
	}
	// actors.yaml.
	af := &storage.ActorsFile{Actors: []core.Actor{
		{Handle: "cam", Name: "Cameron", Kind: core.ActorHuman, Active: true},
	}}
	if err := storage.WriteActors(filepath.Join(decisionsDir, storage.ActorsFileName), af); err != nil {
		t.Fatalf("write actors.yaml: %v", err)
	}

	// Tree directory.
	for _, d := range []string{
		filepath.Join(decisionsDir, "demo", "decisions"),
		filepath.Join(decisionsDir, "demo", "audit"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	tree := &core.Tree{
		Slug:          "demo",
		SchemaVersion: core.SchemaVersion,
		Title:         "Demo",
		CreatedAt:     time.Now().UTC(),
	}
	tree.Layout.Direction = "TB"
	if err := storage.WriteTree(
		filepath.Join(decisionsDir, "demo", storage.TreeMetaFileName), tree,
	); err != nil {
		t.Fatalf("write tree.yaml: %v", err)
	}

	db, err := index.Open(filepath.Join(decisionsDir, ".index.db"))
	if err != nil {
		t.Fatalf("index.Open: %v", err)
	}
	if _, err := db.Conn().Exec(
		`INSERT INTO trees(slug, title, description, archived, created_at, layout_direction, schema_version)
		 VALUES('demo','Demo','',0,?, 'TB', ?)`,
		tree.CreatedAt.Format(time.RFC3339), core.SchemaVersion,
	); err != nil {
		t.Fatalf("insert tree row: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	resolver := identity.NewResolver(repoRoot, &config.Resolved{IdentitySrc: config.SourceDefault})
	srv := server.New(server.Config{
		Listen:   ":0",
		RepoRoot: repoRoot,
		DB:       db,
		Resolver: resolver,
		Trust:    server.TrustLocalhostOnly,
		ReadOnly: readOnly,
	})
	return &decisionEnv{srv: srv, repoRoot: repoRoot, db: db}
}

func (e *decisionEnv) do(t *testing.T, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var buf io.Reader
	if body != nil {
		switch v := body.(type) {
		case []byte:
			buf = bytes.NewReader(v)
		case string:
			buf = strings.NewReader(v)
		default:
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}
			buf = bytes.NewReader(b)
		}
	}
	req := httptest.NewRequest(method, path, buf)
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

func camHdr() map[string]string { return map[string]string{"X-Dtree-As": "cam"} }

// createDecision is a convenience helper that POSTs a minimal decision body
// and returns the server's parsed response.
func (e *decisionEnv) createDecision(t *testing.T, summary string) *core.Decision {
	t.Helper()
	body := map[string]any{
		"summary":  summary,
		"priority": "medium",
		"creator":  "cam",
	}
	w := e.do(t, http.MethodPost, "/v1/trees/demo/decisions", body, camHdr())
	if w.Code != http.StatusCreated {
		t.Fatalf("create %q: status %d; body %s", summary, w.Code, w.Body.String())
	}
	var d core.Decision
	if err := json.NewDecoder(w.Body).Decode(&d); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	return &d
}

// ---------------------------------------------------------------------------
// POST + GET round-trip
// ---------------------------------------------------------------------------

func TestDecisionsCreateAndGetRoundTrip(t *testing.T) {
	e := newDecisionEnv(t, false)

	body := map[string]any{
		"summary":  "Adopt PostgreSQL",
		"priority": "high",
		"creator":  "cam",
		"tags":     []string{"db", "infra"},
	}
	w := e.do(t, http.MethodPost, "/v1/trees/demo/decisions", body, camHdr())
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d; body %s", w.Code, w.Body.String())
	}
	if etag := w.Header().Get("ETag"); etag == "" {
		t.Error("ETag header not set on create")
	}
	var created core.Decision
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.ID == "" || len(created.ID) != 26 {
		t.Errorf("ID = %q; want 26-char ULID", created.ID)
	}
	if created.Tree != "demo" {
		t.Errorf("Tree = %q; want demo", created.Tree)
	}
	if created.Status != core.StatusProposed {
		t.Errorf("Status = %q; want proposed", created.Status)
	}

	w2 := e.do(t, http.MethodGet, "/v1/trees/demo/decisions/"+created.ID, nil, nil)
	if w2.Code != http.StatusOK {
		t.Fatalf("GET status = %d; body %s", w2.Code, w2.Body.String())
	}
	if etag := w2.Header().Get("ETag"); etag == "" {
		t.Error("ETag missing on GET")
	}
	var got core.Decision
	if err := json.NewDecoder(w2.Body).Decode(&got); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if got.Summary != "Adopt PostgreSQL" {
		t.Errorf("Summary = %q", got.Summary)
	}
	if len(got.Tags) != 2 {
		t.Errorf("Tags = %v; want 2", got.Tags)
	}
}

// ---------------------------------------------------------------------------
// list + pagination + filters
// ---------------------------------------------------------------------------

func TestDecisionsListPaginationAndFilters(t *testing.T) {
	e := newDecisionEnv(t, false)

	// Create 5 decisions with mixed priorities.
	for i := 0; i < 5; i++ {
		body := map[string]any{
			"summary":  "Decision " + string(rune('A'+i)),
			"priority": "medium",
			"creator":  "cam",
		}
		if i%2 == 0 {
			body["priority"] = "high"
		}
		w := e.do(t, http.MethodPost, "/v1/trees/demo/decisions", body, camHdr())
		if w.Code != http.StatusCreated {
			t.Fatalf("create %d: %d %s", i, w.Code, w.Body.String())
		}
		// Stagger to ensure distinct ULIDs.
		time.Sleep(2 * time.Millisecond)
	}

	// First page with limit=2.
	w := e.do(t, http.MethodGet, "/v1/trees/demo/decisions?limit=2", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list status %d; body %s", w.Code, w.Body.String())
	}
	var page1 struct {
		Items      []core.Decision `json:"items"`
		NextCursor string          `json:"next_cursor"`
	}
	if err := json.NewDecoder(w.Body).Decode(&page1); err != nil {
		t.Fatalf("decode page1: %v", err)
	}
	if len(page1.Items) != 2 {
		t.Fatalf("page1 items = %d; want 2", len(page1.Items))
	}
	if page1.NextCursor == "" {
		t.Fatal("expected next_cursor on page1")
	}

	// Verify cursor decodes to the last item's ID.
	decoded, err := base64.RawURLEncoding.DecodeString(page1.NextCursor)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	if string(decoded) != page1.Items[1].ID {
		t.Errorf("cursor %q; expected last id %q", decoded, page1.Items[1].ID)
	}

	// Page 2: should pick up after page1.
	w2 := e.do(t, http.MethodGet, "/v1/trees/demo/decisions?limit=2&cursor="+page1.NextCursor, nil, nil)
	var page2 struct {
		Items []core.Decision `json:"items"`
	}
	_ = json.NewDecoder(w2.Body).Decode(&page2)
	if len(page2.Items) != 2 {
		t.Errorf("page2 items = %d; want 2", len(page2.Items))
	}
	if page2.Items[0].ID == page1.Items[0].ID || page2.Items[0].ID == page1.Items[1].ID {
		t.Errorf("page2 should not contain page1 ids")
	}

	// Filter by priority=high should narrow.
	w3 := e.do(t, http.MethodGet, "/v1/trees/demo/decisions?priority=high", nil, nil)
	var hires struct {
		Items []core.Decision `json:"items"`
	}
	_ = json.NewDecoder(w3.Body).Decode(&hires)
	for _, d := range hires.Items {
		if d.Priority != core.PriorityHigh {
			t.Errorf("priority filter leaked %q", d.Priority)
		}
	}
	if len(hires.Items) != 3 { // i=0,2,4
		t.Errorf("priority=high: got %d items; want 3", len(hires.Items))
	}
}

// ---------------------------------------------------------------------------
// PATCH + If-Match
// ---------------------------------------------------------------------------

func TestPatchDecisionIfMatchStaleConflict(t *testing.T) {
	e := newDecisionEnv(t, false)
	d := e.createDecision(t, "Patch me")

	// Stale rev → 412.
	body := map[string]any{"summary": "New summary"}
	w := e.do(t, http.MethodPatch, "/v1/trees/demo/decisions/"+d.ID, body,
		map[string]string{"X-Dtree-As": "cam", "If-Match": `"definitely-not-the-rev"`})
	if w.Code != http.StatusPreconditionFailed {
		t.Fatalf("status = %d; want 412; body %s", w.Code, w.Body.String())
	}
}

func TestPatchDecisionIfMatchMatches(t *testing.T) {
	e := newDecisionEnv(t, false)
	d := e.createDecision(t, "Original")

	body := map[string]any{"summary": "Updated"}
	w := e.do(t, http.MethodPatch, "/v1/trees/demo/decisions/"+d.ID, body,
		map[string]string{"X-Dtree-As": "cam", "If-Match": `"` + d.Rev + `"`})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200; body %s", w.Code, w.Body.String())
	}
	var updated core.Decision
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.Summary != "Updated" {
		t.Errorf("Summary = %q; want Updated", updated.Summary)
	}
	if updated.Rev == "" || updated.Rev == d.Rev {
		t.Errorf("rev did not advance: before=%q after=%q", d.Rev, updated.Rev)
	}
}

// ---------------------------------------------------------------------------
// DELETE soft + hard with incoming refs
// ---------------------------------------------------------------------------

func TestDeleteDecisionSoft(t *testing.T) {
	e := newDecisionEnv(t, false)
	d := e.createDecision(t, "To be deleted")

	w := e.do(t, http.MethodDelete, "/v1/trees/demo/decisions/"+d.ID, nil, camHdr())
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d; want 204; body %s", w.Code, w.Body.String())
	}

	// File should be moved under .deleted/.
	deletedDir := filepath.Join(e.repoRoot, ".decisions", ".deleted", "demo")
	entries, _ := os.ReadDir(deletedDir)
	if len(entries) != 1 {
		t.Errorf(".deleted/demo entries = %d; want 1", len(entries))
	}

	// Index: deleted=1.
	var deleted int
	if err := e.db.Conn().QueryRow(
		`SELECT deleted FROM decisions WHERE id=?`, d.ID,
	).Scan(&deleted); err != nil {
		t.Fatalf("query deleted: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted flag = %d; want 1", deleted)
	}

	// GET should now 404 (resolver filters deleted=0).
	w2 := e.do(t, http.MethodGet, "/v1/trees/demo/decisions/"+d.ID, nil, nil)
	if w2.Code != http.StatusNotFound {
		t.Errorf("GET after soft delete: status %d; want 404", w2.Code)
	}
}

func TestDeleteDecisionHardWithIncomingRefs(t *testing.T) {
	e := newDecisionEnv(t, false)
	a := e.createDecision(t, "Target")
	b := e.createDecision(t, "Source")

	// b influences a.
	w := e.do(t, http.MethodPost, "/v1/trees/demo/decisions/"+b.ID+"/relate",
		map[string]any{"type": "influences", "target": a.ID}, camHdr())
	if w.Code != http.StatusOK {
		t.Fatalf("relate: %d %s", w.Code, w.Body.String())
	}

	// Hard deleting a should be refused (409).
	w2 := e.do(t, http.MethodDelete, "/v1/trees/demo/decisions/"+a.ID+"?hard=true", nil, camHdr())
	if w2.Code != http.StatusConflict {
		t.Errorf("hard delete with incoming refs: status %d; want 409; body %s",
			w2.Code, w2.Body.String())
	}
}

// ---------------------------------------------------------------------------
// decide + undecide
// ---------------------------------------------------------------------------

func TestDecideAndUndecideRoundTrip(t *testing.T) {
	e := newDecisionEnv(t, false)
	d := e.createDecision(t, "Make a call")

	// decide
	w := e.do(t, http.MethodPost, "/v1/trees/demo/decisions/"+d.ID+"/decide",
		map[string]any{
			"choice":         "Use Postgres",
			"reason":         "JSONB",
			"by":             []string{"cam"},
			"is_recommended": true,
		}, camHdr())
	if w.Code != http.StatusOK {
		t.Fatalf("decide: %d %s", w.Code, w.Body.String())
	}
	var dec core.Decision
	_ = json.NewDecoder(w.Body).Decode(&dec)
	if dec.Status != core.StatusDecided {
		t.Errorf("Status = %q; want decided", dec.Status)
	}
	if dec.ActualChoice != "Use Postgres" {
		t.Errorf("ActualChoice = %q", dec.ActualChoice)
	}
	if !dec.IsRecommended {
		t.Error("IsRecommended should be true")
	}

	// undecide
	w2 := e.do(t, http.MethodPost, "/v1/trees/demo/decisions/"+d.ID+"/undecide", nil, camHdr())
	if w2.Code != http.StatusOK {
		t.Fatalf("undecide: %d %s", w2.Code, w2.Body.String())
	}
	var undec core.Decision
	_ = json.NewDecoder(w2.Body).Decode(&undec)
	if undec.Status != core.StatusProposed {
		t.Errorf("Status after undecide = %q; want proposed", undec.Status)
	}
	if undec.ActualChoice != "" {
		t.Errorf("ActualChoice should be cleared, got %q", undec.ActualChoice)
	}
	if undec.IsRecommended {
		t.Error("IsRecommended should be cleared")
	}
}

// ---------------------------------------------------------------------------
// scope-out + restore
// ---------------------------------------------------------------------------

func TestScopeOutAndRestoreRoundTrip(t *testing.T) {
	e := newDecisionEnv(t, false)
	d := e.createDecision(t, "Maybe out of scope")

	w := e.do(t, http.MethodPost, "/v1/trees/demo/decisions/"+d.ID+"/scope-out",
		map[string]any{"reason": "Out of MVP scope"}, camHdr())
	if w.Code != http.StatusOK {
		t.Fatalf("scope-out: %d %s", w.Code, w.Body.String())
	}
	var so core.Decision
	_ = json.NewDecoder(w.Body).Decode(&so)
	if so.Status != core.StatusOutOfScope {
		t.Errorf("Status = %q; want out_of_scope", so.Status)
	}
	if so.OutOfScopeReason != "Out of MVP scope" {
		t.Errorf("Reason = %q", so.OutOfScopeReason)
	}

	w2 := e.do(t, http.MethodPost, "/v1/trees/demo/decisions/"+d.ID+"/restore", nil, camHdr())
	if w2.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", w2.Code, w2.Body.String())
	}
	var r core.Decision
	_ = json.NewDecoder(w2.Body).Decode(&r)
	if r.Status != core.StatusProposed {
		t.Errorf("Status after restore = %q; want proposed", r.Status)
	}
}

// ---------------------------------------------------------------------------
// supersede
// ---------------------------------------------------------------------------

func TestSupersedeCreatesBothEdges(t *testing.T) {
	e := newDecisionEnv(t, false)
	old := e.createDecision(t, "Old way")
	newD := e.createDecision(t, "New way")

	w := e.do(t, http.MethodPost, "/v1/trees/demo/decisions/"+old.ID+"/supersede",
		map[string]any{"by": newD.ID}, camHdr())
	if w.Code != http.StatusOK {
		t.Fatalf("supersede: %d %s", w.Code, w.Body.String())
	}

	// old should now be status=superseded with relationship → newD.
	w2 := e.do(t, http.MethodGet, "/v1/trees/demo/decisions/"+old.ID, nil, nil)
	var got core.Decision
	_ = json.NewDecoder(w2.Body).Decode(&got)
	if got.Status != core.StatusSuperseded {
		t.Errorf("old.Status = %q; want superseded", got.Status)
	}
	foundForward := false
	for _, rel := range got.Relationships {
		if rel.Type == core.RelSupersedes && rel.Target == newD.ID {
			foundForward = true
		}
	}
	if !foundForward {
		t.Errorf("expected supersedes edge on old; got %v", got.Relationships)
	}

	// newD should have a reverse supersedes edge.
	w3 := e.do(t, http.MethodGet, "/v1/trees/demo/decisions/"+newD.ID, nil, nil)
	var newGot core.Decision
	_ = json.NewDecoder(w3.Body).Decode(&newGot)
	foundReverse := false
	for _, rel := range newGot.Relationships {
		if rel.Type == core.RelSupersedes && rel.Target == old.ID {
			foundReverse = true
		}
	}
	if !foundReverse {
		t.Errorf("expected supersedes edge on newD; got %v", newGot.Relationships)
	}
}

// ---------------------------------------------------------------------------
// relate idempotent + cycle check
// ---------------------------------------------------------------------------

func TestRelateIdempotent(t *testing.T) {
	e := newDecisionEnv(t, false)
	a := e.createDecision(t, "A")
	b := e.createDecision(t, "B")

	body := map[string]any{"type": "relates_to", "target": b.ID}
	for i := 0; i < 2; i++ {
		w := e.do(t, http.MethodPost, "/v1/trees/demo/decisions/"+a.ID+"/relate", body, camHdr())
		if w.Code != http.StatusOK {
			t.Fatalf("relate %d: %d %s", i, w.Code, w.Body.String())
		}
	}

	// a should have exactly one relationship.
	w := e.do(t, http.MethodGet, "/v1/trees/demo/decisions/"+a.ID, nil, nil)
	var got core.Decision
	_ = json.NewDecoder(w.Body).Decode(&got)
	count := 0
	for _, rel := range got.Relationships {
		if rel.Type == core.RelRelatesTo && rel.Target == b.ID {
			count++
		}
	}
	if count != 1 {
		t.Errorf("relate edge count = %d; want 1", count)
	}
}

func TestRelateCycleRefused(t *testing.T) {
	e := newDecisionEnv(t, false)
	a := e.createDecision(t, "A")
	b := e.createDecision(t, "B")

	// a blocks b
	w := e.do(t, http.MethodPost, "/v1/trees/demo/decisions/"+a.ID+"/relate",
		map[string]any{"type": "blocks", "target": b.ID}, camHdr())
	if w.Code != http.StatusOK {
		t.Fatalf("first relate: %d %s", w.Code, w.Body.String())
	}

	// b blocks a → would create cycle
	w2 := e.do(t, http.MethodPost, "/v1/trees/demo/decisions/"+b.ID+"/relate",
		map[string]any{"type": "blocks", "target": a.ID}, camHdr())
	if w2.Code != http.StatusUnprocessableEntity {
		t.Errorf("cycle relate: status %d; want 422; body %s", w2.Code, w2.Body.String())
	}
}

// ---------------------------------------------------------------------------
// history
// ---------------------------------------------------------------------------

func TestHistoryReturnsAppendedEvents(t *testing.T) {
	e := newDecisionEnv(t, false)
	d := e.createDecision(t, "Will have history")

	// Patch to add an update event.
	w := e.do(t, http.MethodPatch, "/v1/trees/demo/decisions/"+d.ID,
		map[string]any{"summary": "Updated"},
		map[string]string{"X-Dtree-As": "cam", "If-Match": `"` + d.Rev + `"`})
	if w.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", w.Code, w.Body.String())
	}

	w2 := e.do(t, http.MethodGet, "/v1/trees/demo/decisions/"+d.ID+"/history", nil, nil)
	if w2.Code != http.StatusOK {
		t.Fatalf("history: %d %s", w2.Code, w2.Body.String())
	}
	var resp struct {
		Events []core.Event `json:"events"`
	}
	_ = json.NewDecoder(w2.Body).Decode(&resp)
	if len(resp.Events) < 2 {
		t.Errorf("events = %d; want at least 2 (create + update)", len(resp.Events))
	}
	// Sanity: verify via direct audit.Read too.
	evs, _ := audit.Read(e.repoRoot, audit.Filter{Tree: "demo", TargetID: d.ID})
	if len(evs) != len(resp.Events) {
		t.Errorf("history disagrees with audit.Read: %d vs %d", len(resp.Events), len(evs))
	}
}

// ---------------------------------------------------------------------------
// read-only mode
// ---------------------------------------------------------------------------

func TestReadOnlyRefusesAllMutations(t *testing.T) {
	e := newDecisionEnv(t, true)

	// list/GET still permitted.
	w := e.do(t, http.MethodGet, "/v1/trees/demo/decisions", nil, nil)
	if w.Code != http.StatusOK {
		t.Errorf("list status %d; want 200 in read-only", w.Code)
	}

	cases := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodPost, "/v1/trees/demo/decisions", map[string]any{"summary": "x", "creator": "cam"}},
		{http.MethodPatch, "/v1/trees/demo/decisions/01ABCDEFG0HJKMNPQRSTVWXYZ0", map[string]any{"summary": "x"}},
		{http.MethodDelete, "/v1/trees/demo/decisions/01ABCDEFG0HJKMNPQRSTVWXYZ0", nil},
		{http.MethodPost, "/v1/trees/demo/decisions/01ABCDEFG0HJKMNPQRSTVWXYZ0/decide", map[string]any{"choice": "x"}},
		{http.MethodPost, "/v1/trees/demo/decisions/01ABCDEFG0HJKMNPQRSTVWXYZ0/undecide", nil},
		{http.MethodPost, "/v1/trees/demo/decisions/01ABCDEFG0HJKMNPQRSTVWXYZ0/scope-out", map[string]any{"reason": "x"}},
		{http.MethodPost, "/v1/trees/demo/decisions/01ABCDEFG0HJKMNPQRSTVWXYZ0/restore", nil},
		{http.MethodPost, "/v1/trees/demo/decisions/01ABCDEFG0HJKMNPQRSTVWXYZ0/supersede", map[string]any{"by": "01ABCDEFG0HJKMNPQRSTVWXYZ1"}},
		{http.MethodPost, "/v1/trees/demo/decisions/01ABCDEFG0HJKMNPQRSTVWXYZ0/relate", map[string]any{"type": "relates_to", "target": "01ABCDEFG0HJKMNPQRSTVWXYZ1"}},
		{http.MethodPost, "/v1/trees/demo/decisions/01ABCDEFG0HJKMNPQRSTVWXYZ0/unrelate", map[string]any{"type": "relates_to", "target": "01ABCDEFG0HJKMNPQRSTVWXYZ1"}},
	}
	for _, c := range cases {
		w := e.do(t, c.method, c.path, c.body, camHdr())
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s: status %d; want 403; body %s",
				c.method, c.path, w.Code, w.Body.String())
		}
	}
}
