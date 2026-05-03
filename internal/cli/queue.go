// Package cli — `dtree queue` surfaces decision queues: which decisions are
// blocking the most others (`spearhead`) and which are unblocked, high-priority,
// and ready to pick up (`quick-wins`).
package cli

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/cgould/dtree/internal/index"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const (
	queueDefaultLimit = 10
	queueMaxLimit     = 100
)

// queueRow is the row shape returned for both subcommands. BlockingCount is
// only populated for `spearhead`.
type queueRow struct {
	ID            string `json:"id" yaml:"id"`
	Tree          string `json:"tree" yaml:"tree"`
	Summary       string `json:"summary" yaml:"summary"`
	Status        string `json:"status" yaml:"status"`
	Priority      string `json:"priority" yaml:"priority"`
	BlockingCount int    `json:"blocking_count,omitempty" yaml:"blocking_count,omitempty"`
}

// queueResult wraps queueRow for JSON/YAML output.
type queueResult struct {
	Items []queueRow `json:"items" yaml:"items"`
}

// newQueueCommand returns the `dtree queue` parent command.
func newQueueCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "queue",
		Short: "Surface decision queues: spearhead and quick-wins",
		Long: "Subcommands:\n" +
			"  spearhead   decisions blocking the most other decisions (sorted desc)\n" +
			"  quick-wins  unblocked high/critical proposed decisions ready to pick up",
	}
	cmd.AddCommand(newQueueSpearheadCommand())
	cmd.AddCommand(newQueueQuickWinsCommand())
	return cmd
}

// ---------------------------------------------------------------------------
// queue spearhead
// ---------------------------------------------------------------------------

func newQueueSpearheadCommand() *cobra.Command {
	var (
		limit    int
		treeSlug string
	)
	cmd := &cobra.Command{
		Use:   "spearhead",
		Short: "Show decisions blocking the most other decisions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			if err := requireDecisionsDir(repoRoot); err != nil {
				return err
			}
			db, err := openIndex(repoRoot)
			if err != nil {
				return fmt.Errorf("queue spearhead: open index: %w", err)
			}
			defer db.Close()

			rows, err := querySpearhead(db, treeSlug, resolveQueueLimit(limit))
			if err != nil {
				return fmt.Errorf("queue spearhead: %w", err)
			}
			return emitQueue(cmd, rows, true)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", queueDefaultLimit, "Max rows to return (max 100)")
	cmd.Flags().StringVar(&treeSlug, "tree", "", "Filter by tree slug")
	cmd.Flags().StringP("output", "o", "", "Output format: human, json, yaml, ids")
	return cmd
}

// querySpearhead returns the top-N decisions ranked by outgoing blocks edges.
// Only decisions whose blocked targets are still actionable (not decided/
// out_of_scope) count toward the score; finished blockers don't really
// "spearhead" anything.
func querySpearhead(db *index.DB, treeSlug string, limit int) ([]queueRow, error) {
	args := []any{}
	cond := "blocker.deleted = 0 AND blocker.status NOT IN ('decided','out_of_scope','superseded')"
	if treeSlug != "" {
		cond += " AND blocker.tree = ?"
		args = append(args, treeSlug)
	}
	q := `
		SELECT blocker.id, blocker.tree, blocker.summary, blocker.status, blocker.priority,
		       COUNT(*) AS blocking_count
		FROM relationships r
		JOIN decisions blocker ON blocker.id = r.source
		JOIN decisions target  ON target.id  = r.target
		WHERE r.type = 'blocks'
		  AND target.deleted = 0
		  AND target.status NOT IN ('decided','out_of_scope','superseded')
		  AND ` + cond + `
		GROUP BY blocker.id
		ORDER BY blocking_count DESC, blocker.id ASC
		LIMIT ?`
	args = append(args, limit)

	rows, err := db.Conn().Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []queueRow
	for rows.Next() {
		var r queueRow
		if err := rows.Scan(&r.ID, &r.Tree, &r.Summary, &r.Status, &r.Priority, &r.BlockingCount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// queue quick-wins
// ---------------------------------------------------------------------------

func newQueueQuickWinsCommand() *cobra.Command {
	var (
		limit    int
		treeSlug string
	)
	cmd := &cobra.Command{
		Use:   "quick-wins",
		Short: "Show unblocked high/critical proposed decisions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			if err := requireDecisionsDir(repoRoot); err != nil {
				return err
			}
			db, err := openIndex(repoRoot)
			if err != nil {
				return fmt.Errorf("queue quick-wins: open index: %w", err)
			}
			defer db.Close()

			rows, err := queryQuickWins(db, treeSlug, resolveQueueLimit(limit))
			if err != nil {
				return fmt.Errorf("queue quick-wins: %w", err)
			}
			return emitQueue(cmd, rows, false)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", queueDefaultLimit, "Max rows to return (max 100)")
	cmd.Flags().StringVar(&treeSlug, "tree", "", "Filter by tree slug")
	cmd.Flags().StringP("output", "o", "", "Output format: human, json, yaml, ids")
	return cmd
}

// queryQuickWins returns proposed decisions that have NO incoming blocks edge
// from a still-actionable source AND priority is high or critical. Sort by
// priority desc (critical > high) then by id ASC (ULID id == created_at).
func queryQuickWins(db *index.DB, treeSlug string, limit int) ([]queueRow, error) {
	args := []any{}
	cond := ""
	if treeSlug != "" {
		cond = " AND d.tree = ?"
		args = append(args, treeSlug)
	}
	q := `
		SELECT d.id, d.tree, d.summary, d.status, d.priority
		FROM decisions d
		WHERE d.deleted = 0
		  AND d.status = 'proposed'
		  AND d.priority IN ('high','critical')
		  AND NOT EXISTS (
		    SELECT 1 FROM relationships r
		    JOIN decisions blocker ON blocker.id = r.source
		    WHERE r.target = d.id
		      AND r.type = 'blocks'
		      AND blocker.deleted = 0
		      AND blocker.status NOT IN ('decided','out_of_scope','superseded')
		  )
		  ` + cond + `
		ORDER BY CASE d.priority WHEN 'critical' THEN 0 WHEN 'high' THEN 1 ELSE 2 END,
		         d.id ASC
		LIMIT ?`
	args = append(args, limit)

	rows, err := db.Conn().Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []queueRow
	for rows.Next() {
		var r queueRow
		if err := rows.Scan(&r.ID, &r.Tree, &r.Summary, &r.Status, &r.Priority); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func resolveQueueLimit(l int) int {
	if l <= 0 {
		return queueDefaultLimit
	}
	if l > queueMaxLimit {
		return queueMaxLimit
	}
	return l
}

func emitQueue(cmd *cobra.Command, rows []queueRow, withCount bool) error {
	format := outputFormat(cmd)
	switch format {
	case "json":
		res := queueResult{Items: rows}
		if res.Items == nil {
			res.Items = []queueRow{}
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(res)
	case "yaml":
		res := queueResult{Items: rows}
		if res.Items == nil {
			res.Items = []queueRow{}
		}
		enc := yaml.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent(2)
		if err := enc.Encode(res); err != nil {
			_ = enc.Close()
			return err
		}
		return enc.Close()
	case "ids":
		for _, r := range rows {
			fmt.Fprintln(cmd.OutOrStdout(), r.ID)
		}
		return nil
	default:
		return emitQueueHuman(cmd, rows, withCount)
	}
}

func emitQueueHuman(cmd *cobra.Command, rows []queueRow, withCount bool) error {
	if len(rows) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "(no rows)")
		return nil
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	if withCount {
		fmt.Fprintln(w, "ID\tCOUNT\tPRIORITY\tSUMMARY\tTREE")
		for _, r := range rows {
			fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\n",
				shortenID(r.ID), r.BlockingCount, r.Priority,
				truncate(r.Summary, 80), r.Tree)
		}
	} else {
		fmt.Fprintln(w, "ID\tPRIORITY\tSUMMARY\tTREE")
		for _, r := range rows {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
				shortenID(r.ID), r.Priority,
				truncate(r.Summary, 80), r.Tree)
		}
	}
	return w.Flush()
}

