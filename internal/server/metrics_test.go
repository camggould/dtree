//go:build sqlite_fts5

package server_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/cgould/dtree/internal/ulid"
)

// ---------------------------------------------------------------------------
// GET /v1/trees/{tree}/metrics — happy path
// ---------------------------------------------------------------------------

func TestMetrics_HappyPath(t *testing.T) {
	e := newTreeTestEnv(t)
	e.insertTestTree(t, "metrics-tree", "Metrics Tree", false)
	e.insertTestDecision(t, "metrics-tree", ulid.New())

	w := e.do(t, http.MethodGet, "/v1/trees/metrics-tree/metrics", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	for _, key := range []string{
		"total_decisions",
		"by_status",
		"by_priority",
		"by_creator",
		"assumptions_count",
		"unblocked_proposed_count",
		"oldest_proposed_id",
	} {
		if _, ok := resp[key]; !ok {
			t.Errorf("response missing key %q", key)
		}
	}

	total, _ := resp["total_decisions"].(float64)
	if total < 1 {
		t.Errorf("total_decisions = %v; want >= 1", total)
	}
}

// ---------------------------------------------------------------------------
// GET /v1/trees/{tree}/metrics — 404 on missing tree
// ---------------------------------------------------------------------------

func TestMetrics_TreeNotFound(t *testing.T) {
	e := newTreeTestEnv(t)

	w := e.do(t, http.MethodGet, "/v1/trees/no-such-tree/metrics", nil, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d; want 404; body: %s", w.Code, w.Body.String())
	}
}
