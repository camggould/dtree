//go:build sqlite_fts5

package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cgould/dtree/internal/ulid"
)

// ---------------------------------------------------------------------------
// GET /v1/trees/{tree}/queues/spearhead — happy path
// ---------------------------------------------------------------------------

func TestSpearheadQueue_HappyPath(t *testing.T) {
	e := newTreeTestEnv(t)
	e.insertTestTree(t, "alpha", "Alpha", false)
	e.insertTestDecision(t, "alpha", ulid.New())

	w := e.do(t, http.MethodGet, "/v1/trees/alpha/queues/spearhead", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["items"]; !ok {
		t.Fatal("response missing 'items' key")
	}
}

// ---------------------------------------------------------------------------
// GET /v1/trees/{tree}/queues/spearhead — 404 on missing tree
// ---------------------------------------------------------------------------

func TestSpearheadQueue_TreeNotFound(t *testing.T) {
	e := newTreeTestEnv(t)

	w := e.do(t, http.MethodGet, "/v1/trees/nosuchTree/queues/spearhead", nil, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d; want 404; body: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// GET /v1/trees/{tree}/queues/quick-wins — happy path
// ---------------------------------------------------------------------------

func TestQuickWinsQueue_HappyPath(t *testing.T) {
	e := newTreeTestEnv(t)
	e.insertTestTree(t, "beta", "Beta", false)
	e.insertTestDecision(t, "beta", ulid.New())

	w := e.do(t, http.MethodGet, "/v1/trees/beta/queues/quick-wins", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := resp["items"]; !ok {
		t.Fatal("response missing 'items' key")
	}
}

// ---------------------------------------------------------------------------
// GET /v1/trees/{tree}/queues/unassigned — happy path + 404
// ---------------------------------------------------------------------------

func TestUnassignedQueue_HappyPath(t *testing.T) {
	e := newTreeTestEnv(t)
	e.insertTestTree(t, "gamma", "Gamma", false)
	e.insertTestDecision(t, "gamma", ulid.New())

	w := e.do(t, http.MethodGet, "/v1/trees/gamma/queues/unassigned", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	items, ok := resp["items"]
	if !ok {
		t.Fatal("response missing 'items' key")
	}
	arr, ok := items.([]any)
	if !ok {
		t.Fatalf("items is not an array: %T", items)
	}
	// Decision was inserted with empty assignee, so should appear.
	if len(arr) == 0 {
		t.Error("expected at least 1 unassigned decision")
	}
}

func TestUnassignedQueue_TreeNotFound(t *testing.T) {
	e := newTreeTestEnv(t)

	w := e.do(t, http.MethodGet, "/v1/trees/missing/queues/unassigned", nil, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d; want 404; body: %s", w.Code, w.Body.String())
	}
}
