package server

import (
	"database/sql"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
)

// ---------------------------------------------------------------------------
// Mount
// ---------------------------------------------------------------------------

// mountMetrics registers the /v1/trees/{tree}/metrics route.
func mountMetrics(r chi.Router, cfg Config) {
	r.Get("/trees/{tree}/metrics", metricsHandler(cfg))
}

// ---------------------------------------------------------------------------
// Response shape
// ---------------------------------------------------------------------------

type metricsResponse struct {
	TotalDecisions        int            `json:"total_decisions"`
	ByStatus              map[string]int `json:"by_status"`
	ByPriority            map[string]int `json:"by_priority"`
	ByCreator             map[string]int `json:"by_creator"`
	AssumptionsCount      int            `json:"assumptions_count"`
	UnblockedProposedCount int           `json:"unblocked_proposed_count"`
	OldestProposedID      string         `json:"oldest_proposed_id"`
}

// ---------------------------------------------------------------------------
// GET /v1/trees/{tree}/metrics
// ---------------------------------------------------------------------------

func metricsHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tree := chi.URLParam(r, "tree")
		if requireTree(w, r, cfg.DB, tree) {
			return
		}

		db := cfg.DB.Conn()

		// We fetch the metrics concurrently since sql.DB is safe for concurrent use.
		var (
			mu      sync.Mutex
			wg      sync.WaitGroup
			fetchErr error
		)

		resp := metricsResponse{
			ByStatus:   make(map[string]int),
			ByPriority: make(map[string]int),
			ByCreator:  make(map[string]int),
		}

		setErr := func(err error) {
			mu.Lock()
			if fetchErr == nil {
				fetchErr = err
			}
			mu.Unlock()
		}

		// 1. total_decisions + by_status + by_priority
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, err := db.QueryContext(r.Context(), `
				SELECT status, priority, COUNT(*) AS cnt
				FROM decisions
				WHERE tree = ? AND deleted = 0
				GROUP BY status, priority`, tree)
			if err != nil {
				setErr(err)
				return
			}
			defer rows.Close()
			mu.Lock()
			defer mu.Unlock()
			for rows.Next() {
				var status, priority string
				var cnt int
				if err := rows.Scan(&status, &priority, &cnt); err != nil {
					setErr(err)
					return
				}
				resp.TotalDecisions += cnt
				resp.ByStatus[status] += cnt
				resp.ByPriority[priority] += cnt
			}
			if err := rows.Err(); err != nil {
				setErr(err)
			}
		}()

		// 2. by_creator (top 10)
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, err := db.QueryContext(r.Context(), `
				SELECT creator, COUNT(*) AS cnt
				FROM decisions
				WHERE tree = ? AND deleted = 0
				GROUP BY creator
				ORDER BY cnt DESC
				LIMIT 10`, tree)
			if err != nil {
				setErr(err)
				return
			}
			defer rows.Close()
			mu.Lock()
			defer mu.Unlock()
			for rows.Next() {
				var creator string
				var cnt int
				if err := rows.Scan(&creator, &cnt); err != nil {
					setErr(err)
					return
				}
				resp.ByCreator[creator] = cnt
			}
			if err := rows.Err(); err != nil {
				setErr(err)
			}
		}()

		// 3. assumptions_count
		wg.Add(1)
		go func() {
			defer wg.Done()
			var cnt int
			err := db.QueryRowContext(r.Context(), `
				SELECT COUNT(*) FROM decisions
				WHERE tree = ? AND priority = 'assumption' AND deleted = 0`, tree).Scan(&cnt)
			if err != nil {
				setErr(err)
				return
			}
			mu.Lock()
			resp.AssumptionsCount = cnt
			mu.Unlock()
		}()

		// 4. unblocked_proposed_count
		wg.Add(1)
		go func() {
			defer wg.Done()
			var cnt int
			err := db.QueryRowContext(r.Context(), `
				SELECT COUNT(*) FROM decisions d
				WHERE d.tree = ? AND d.status = 'proposed' AND d.deleted = 0
				  AND NOT EXISTS (
				    SELECT 1 FROM relationships r2
				    JOIN decisions blocker ON blocker.id = r2.source
				    WHERE r2.target = d.id
				      AND r2.type = 'blocks'
				      AND blocker.status NOT IN ('decided', 'out_of_scope')
				      AND blocker.deleted = 0
				  )`, tree).Scan(&cnt)
			if err != nil {
				setErr(err)
				return
			}
			mu.Lock()
			resp.UnblockedProposedCount = cnt
			mu.Unlock()
		}()

		// 5. oldest_proposed_id
		wg.Add(1)
		go func() {
			defer wg.Done()
			var id sql.NullString
			err := db.QueryRowContext(r.Context(), `
				SELECT id FROM decisions
				WHERE tree = ? AND status = 'proposed' AND deleted = 0
				ORDER BY id ASC
				LIMIT 1`, tree).Scan(&id)
			if err != nil && err != sql.ErrNoRows {
				setErr(err)
				return
			}
			mu.Lock()
			if id.Valid {
				resp.OldestProposedID = id.String
			}
			mu.Unlock()
		}()

		wg.Wait()

		if fetchErr != nil {
			WriteProblem(w, r, Internal("failed to fetch metrics"))
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}
