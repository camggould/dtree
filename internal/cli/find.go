// Package cli — `dtree find` runs an FTS5 MATCH search across all decisions.
//
// The query is executed against the decisions_fts virtual table (configured
// in internal/index/schema.go with content='decisions' and
// content_rowid='rowid') and joined back to the parent `decisions` table by
// the integer rowid. Results include a snippet of the matched text.
package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/cgould/dtree/internal/index"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const (
	findDefaultLimit = 25
	findMaxLimit     = 200
)

// findRow is the projection rendered for each search hit.
type findRow struct {
	ID       string `json:"id" yaml:"id"`
	Tree     string `json:"tree" yaml:"tree"`
	Status   string `json:"status" yaml:"status"`
	Priority string `json:"priority" yaml:"priority"`
	Summary  string `json:"summary" yaml:"summary"`
	Snippet  string `json:"snippet" yaml:"snippet"`
}

// findResult wraps results for json/yaml output.
type findResult struct {
	Items []findRow `json:"items" yaml:"items"`
}

// newFindCommand returns the `dtree find` cobra command.
func newFindCommand() *cobra.Command {
	var (
		treeSlug string
		limit    int
	)

	cmd := &cobra.Command{
		Use:   "find <query>",
		Short: "Search decisions with SQLite FTS5",
		Long: `Search decisions across all trees using SQLite FTS5 MATCH.

The query is passed verbatim to FTS5, so the standard FTS5 query syntax
applies (e.g. "exact phrase", term1 OR term2, prefix*, NEAR(...)).

Results are ranked by FTS5 relevance and include a short snippet of the
match. The snippet column comes from the summary field; descriptions and
recommendation/outcome text are also indexed and contribute to ranking.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			if err := requireDecisionsDir(repoRoot); err != nil {
				return err
			}

			db, err := openIndex(repoRoot)
			if err != nil {
				return fmt.Errorf("find: open index: %w", err)
			}
			defer db.Close()

			if limit <= 0 {
				limit = findDefaultLimit
			}
			if limit > findMaxLimit {
				limit = findMaxLimit
			}

			rows, err := findQuery(db, query, treeSlug, limit)
			if err != nil {
				return fmt.Errorf("find: query: %w", err)
			}

			format := outputFormat(cmd)
			switch format {
			case "json":
				return findPrintJSON(cmd, rows)
			case "yaml":
				return findPrintYAML(cmd, rows)
			case "ids":
				return findPrintIDs(cmd, rows)
			default:
				return findPrintHuman(cmd, rows)
			}
		},
	}

	cmd.Flags().StringVar(&treeSlug, "tree", "", "Restrict search to a single tree slug")
	cmd.Flags().IntVar(&limit, "limit", findDefaultLimit, "Max results (default 25, max 200)")
	cmd.Flags().StringP("output", "o", "", "Output format: human, json, yaml, ids")

	return cmd
}

// findQuery runs the FTS5 MATCH SELECT and returns the projected rows.
//
// decisions_fts is created with content='decisions' and content_rowid='rowid',
// so each FTS row's rowid equals the integer rowid of its decisions row. We
// join on that to recover id/tree/status/priority/summary, then ask FTS for
// a snippet over column 0 (summary).
func findQuery(db *index.DB, match, treeSlug string, limit int) ([]findRow, error) {
	q := `SELECT d.id, d.tree, d.status, d.priority, d.summary,
	             snippet(decisions_fts, 0, '[', ']', '...', 32) AS snip
	      FROM decisions_fts
	      JOIN decisions d ON d.rowid = decisions_fts.rowid
	      WHERE decisions_fts MATCH ?
	        AND d.deleted = 0`
	args := []any{match}
	if treeSlug != "" {
		q += ` AND d.tree = ?`
		args = append(args, treeSlug)
	}
	q += ` ORDER BY rank LIMIT ?`
	args = append(args, limit)

	rows, err := db.Conn().Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []findRow
	for rows.Next() {
		var r findRow
		if err := rows.Scan(&r.ID, &r.Tree, &r.Status, &r.Priority, &r.Summary, &r.Snippet); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Output renderers
// ---------------------------------------------------------------------------

func findPrintHuman(cmd *cobra.Command, rows []findRow) error {
	if len(rows) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "(no matches)")
		return nil
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTREE\tSTATUS\tSUMMARY")
	for _, r := range rows {
		id := r.ID
		if len(id) > 8 {
			id = id[:8]
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			id, r.Tree, r.Status, truncate(r.Summary, 80))
		// Snippet is rendered after the row so tabwriter alignment is
		// preserved; leading whitespace flushes the previous record.
		if snip := strings.TrimSpace(r.Snippet); snip != "" {
			fmt.Fprintf(w, "\t\t\t  %s\n", snip)
		}
	}
	return w.Flush()
}

func findPrintJSON(cmd *cobra.Command, rows []findRow) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	res := findResult{Items: rows}
	if res.Items == nil {
		res.Items = []findRow{}
	}
	return enc.Encode(res)
}

func findPrintYAML(cmd *cobra.Command, rows []findRow) error {
	enc := yaml.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent(2)
	res := findResult{Items: rows}
	if res.Items == nil {
		res.Items = []findRow{}
	}
	if err := enc.Encode(res); err != nil {
		return err
	}
	return enc.Close()
}

func findPrintIDs(cmd *cobra.Command, rows []findRow) error {
	for _, r := range rows {
		fmt.Fprintln(cmd.OutOrStdout(), r.ID)
	}
	return nil
}
