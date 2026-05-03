package server

import (
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/core"
)

// ---------------------------------------------------------------------------
// Mount
// ---------------------------------------------------------------------------

// mountState registers GET /v1/trees/{tree}/state.
//
// The endpoint replays the per-tree audit log up to a point in time and
// returns the reconstructed snapshot. Two query parameters are honored:
//
//   - at=<RFC3339> — the cutoff timestamp. Defaults to time.Now().UTC().
func mountState(r chi.Router, cfg Config) {
	r.Get("/trees/{tree}/state", stateHandler(cfg))
}

// ---------------------------------------------------------------------------
// Response shape
// ---------------------------------------------------------------------------

type stateResponse struct {
	AsOf      time.Time        `json:"as_of"`
	Decisions []*core.Decision `json:"decisions"`
}

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

func stateHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tree := chi.URLParam(r, "tree")
		if requireTree(w, r, cfg.DB, tree) {
			return
		}

		at := time.Now().UTC()
		if v := r.URL.Query().Get("at"); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				WriteProblem(w, r, BadRequest("at: must be RFC3339 timestamp"))
				return
			}
			at = t.UTC()
		}

		snap, err := audit.ReplayState(cfg.RepoRoot, tree, at)
		if err != nil {
			WriteProblem(w, r, Internal("failed to replay state: "+err.Error()))
			return
		}

		ids := make([]string, 0, len(snap))
		for id := range snap {
			ids = append(ids, id)
		}
		sort.Strings(ids)

		decisions := make([]*core.Decision, 0, len(ids))
		for _, id := range ids {
			decisions = append(decisions, snap[id])
		}

		writeJSON(w, http.StatusOK, stateResponse{
			AsOf:      at,
			Decisions: decisions,
		})
	}
}
