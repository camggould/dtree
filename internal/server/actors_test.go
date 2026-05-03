//go:build sqlite_fts5

package server_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// ---------------------------------------------------------------------------
// GET /v1/actors — happy path
// ---------------------------------------------------------------------------

func TestListActors_HappyPath(t *testing.T) {
	e := newTreeTestEnv(t)

	w := e.do(t, http.MethodGet, "/v1/actors", nil, nil)
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
	// "cam" is inserted by newTreeTestEnv and is active.
	if len(arr) == 0 {
		t.Error("expected at least one actor (cam), got 0")
	}
}

// ---------------------------------------------------------------------------
// POST /v1/actors — error: no identity
// ---------------------------------------------------------------------------

func TestCreateActor_NoIdentity(t *testing.T) {
	e := newTreeTestEnv(t)

	body := map[string]any{
		"handle":       "newactor",
		"kind":         "human",
		"display_name": "New Actor",
	}
	w := e.do(t, http.MethodPost, "/v1/actors", body, nil) // no X-Dtree-As header
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d; want 401; body: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// POST /v1/actors — happy path: create then GET
// ---------------------------------------------------------------------------

func TestCreateAndGetActor_HappyPath(t *testing.T) {
	e := newTreeTestEnv(t)

	body := map[string]any{
		"handle":       "alice",
		"kind":         "human",
		"display_name": "Alice",
	}
	w := e.do(t, http.MethodPost, "/v1/actors", body, withActor("cam"))
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d; want 201; body: %s", w.Code, w.Body.String())
	}

	// Now GET the actor
	w2 := e.do(t, http.MethodGet, "/v1/actors/alice", nil, nil)
	if w2.Code != http.StatusOK {
		t.Fatalf("get status = %d; want 200; body: %s", w2.Code, w2.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w2.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["handle"] != "alice" {
		t.Errorf("handle = %v; want alice", resp["handle"])
	}
}

// ---------------------------------------------------------------------------
// GET /v1/actors/{handle} — 404
// ---------------------------------------------------------------------------

func TestGetActor_NotFound(t *testing.T) {
	e := newTreeTestEnv(t)

	w := e.do(t, http.MethodGet, "/v1/actors/nonexistent", nil, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d; want 404; body: %s", w.Code, w.Body.String())
	}
}
