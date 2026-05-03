package server

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// ---------------------------------------------------------------------------
// Mount
// ---------------------------------------------------------------------------

// mountQueues registers all /v1/trees/{tree}/queues/* routes.
func mountQueues(r chi.Router, cfg Config) {
	r.Route("/trees/{tree}/queues", func(r chi.Router) {
		r.Get("/spearhead", spearheadQueueHandler(cfg))
		r.Get("/quick-wins", quickWinsQueueHandler(cfg))
		r.Get("/unassigned", unassignedQueueHandler(cfg))
	})
}

// ---------------------------------------------------------------------------
// Response shapes
// ---------------------------------------------------------------------------

type spearheadItem struct {
	ID            string `json:"id"`
	Summary       string `json:"summary"`
	BlockingCount int    `json:"blocking_count"`
}

type spearheadResponse struct {
	Items []spearheadItem `json:"items"`
}

type quickWinItem struct {
	ID       string `json:"id"`
	Summary  string `json:"summary"`
	Priority string `json:"priority"`
}

type quickWinsResponse struct {
	Items []quickWinItem `json:"items"`
}

type unassignedItem struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
}

type unassignedResponse struct {
	Items []unassignedItem `json:"items"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func parseLimit(r *http.Request, defaultVal int) int {
	s := r.URL.Query().Get("limit")
	if s == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return defaultVal
	}
	return n
}

// ---------------------------------------------------------------------------
// GET /v1/trees/{tree}/queues/spearhead
// ---------------------------------------------------------------------------

func spearheadQueueHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tree := chi.URLParam(r, "tree")
		if requireTree(w, r, cfg.DB, tree) {
			return
		}

		limit := parseLimit(r, 20)

		rows, err := cfg.DB.Conn().QueryContext(r.Context(), `
			SELECT d.id, d.summary, COUNT(r.target) AS bc
			FROM decisions d
			LEFT JOIN relationships r ON r.source = d.id AND r.type = 'blocks'
			WHERE d.tree = ? AND d.status = 'proposed' AND d.deleted = 0
			GROUP BY d.id
			ORDER BY bc DESC
			LIMIT ?`, tree, limit)
		if err != nil {
			WriteProblem(w, r, Internal("failed to query spearhead queue"))
			return
		}
		defer rows.Close()

		items := []spearheadItem{}
		for rows.Next() {
			var it spearheadItem
			if err := rows.Scan(&it.ID, &it.Summary, &it.BlockingCount); err != nil {
				WriteProblem(w, r, Internal("failed to scan spearhead row"))
				return
			}
			items = append(items, it)
		}
		if err := rows.Err(); err != nil {
			WriteProblem(w, r, Internal("row iteration error"))
			return
		}

		writeJSON(w, http.StatusOK, spearheadResponse{Items: items})
	}
}

// ---------------------------------------------------------------------------
// GET /v1/trees/{tree}/queues/quick-wins
// ---------------------------------------------------------------------------

func quickWinsQueueHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tree := chi.URLParam(r, "tree")
		if requireTree(w, r, cfg.DB, tree) {
			return
		}

		limit := parseLimit(r, 20)

		// Quick wins: proposed decisions whose blockers are all decided or out_of_scope.
		// A decision with no blockers at all is also a quick win.
		rows, err := cfg.DB.Conn().QueryContext(r.Context(), `
			SELECT d.id, d.summary, d.priority
			FROM decisions d
			WHERE d.tree = ? AND d.status = 'proposed' AND d.deleted = 0
			  AND NOT EXISTS (
			    SELECT 1 FROM relationships r2
			    JOIN decisions blocker ON blocker.id = r2.source
			    WHERE r2.target = d.id
			      AND r2.type = 'blocks'
			      AND blocker.status NOT IN ('decided', 'out_of_scope')
			      AND blocker.deleted = 0
			  )
			ORDER BY CASE d.priority
			  WHEN 'critical'   THEN 1
			  WHEN 'high'       THEN 2
			  WHEN 'medium'     THEN 3
			  WHEN 'low'        THEN 4
			  WHEN 'assumption' THEN 5
			  ELSE 6
			END
			LIMIT ?`, tree, limit)
		if err != nil {
			WriteProblem(w, r, Internal("failed to query quick-wins queue"))
			return
		}
		defer rows.Close()

		items := []quickWinItem{}
		for rows.Next() {
			var it quickWinItem
			if err := rows.Scan(&it.ID, &it.Summary, &it.Priority); err != nil {
				WriteProblem(w, r, Internal("failed to scan quick-wins row"))
				return
			}
			items = append(items, it)
		}
		if err := rows.Err(); err != nil {
			WriteProblem(w, r, Internal("row iteration error"))
			return
		}

		writeJSON(w, http.StatusOK, quickWinsResponse{Items: items})
	}
}

// ---------------------------------------------------------------------------
// GET /v1/trees/{tree}/queues/unassigned
// ---------------------------------------------------------------------------

func unassignedQueueHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tree := chi.URLParam(r, "tree")
		if requireTree(w, r, cfg.DB, tree) {
			return
		}

		limit := parseLimit(r, 20)

		rows, err := cfg.DB.Conn().QueryContext(r.Context(), `
			SELECT id, summary
			FROM decisions
			WHERE tree = ? AND status = 'proposed' AND deleted = 0
			  AND (assignee IS NULL OR assignee = '')
			ORDER BY id ASC
			LIMIT ?`, tree, limit)
		if err != nil {
			WriteProblem(w, r, Internal("failed to query unassigned queue"))
			return
		}
		defer rows.Close()

		items := []unassignedItem{}
		for rows.Next() {
			var it unassignedItem
			if err := rows.Scan(&it.ID, &it.Summary); err != nil {
				WriteProblem(w, r, Internal("failed to scan unassigned row"))
				return
			}
			items = append(items, it)
		}
		if err := rows.Err(); err != nil {
			WriteProblem(w, r, Internal("row iteration error"))
			return
		}

		writeJSON(w, http.StatusOK, unassignedResponse{Items: items})
	}
}
