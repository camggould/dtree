// Package cli — `dtree ls` lists decisions with rich filters and pagination.
//
// The query is built dynamically from flags but always uses parameterized
// SQL. Ordering is by id DESC (ULIDs are time-sortable), so pagination
// cursors are simply the last seen id of the previous page.
package cli

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/index"
	uliddep "github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const (
	lsDefaultLimit = 50
	lsMaxLimit     = 1000
)

// lsRow is the lightweight projection used for human / json / yaml output.
type lsRow struct {
	ID       string `json:"id" yaml:"id"`
	Tree     string `json:"tree" yaml:"tree"`
	Slug     string `json:"slug" yaml:"slug"`
	Summary  string `json:"summary" yaml:"summary"`
	Status   string `json:"status" yaml:"status"`
	Priority string `json:"priority" yaml:"priority"`
	Creator  string `json:"creator" yaml:"creator"`
	Assignee string `json:"assignee,omitempty" yaml:"assignee,omitempty"`
	Rev      string `json:"_rev,omitempty" yaml:"_rev,omitempty"`
}

// lsResult wraps results for json/yaml output with optional pagination cursor.
type lsResult struct {
	Items      []lsRow `json:"items" yaml:"items"`
	NextCursor string  `json:"next_cursor,omitempty" yaml:"next_cursor,omitempty"`
}

// newLsCommand returns the `dtree ls` cobra command.
func newLsCommand() *cobra.Command {
	var (
		statuses     []string
		priorities   []string
		tags         []string
		creator      string
		decider      string
		assigned     string
		recommender  string
		since        string
		until        string
		treeSlug     string
		unblocked    bool
		search       string
		limit        int
		cursor       string
	)

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List decisions with filters and pagination",
		Long: `List decisions across all trees with rich filters.

By default ls excludes scoped-out and superseded decisions. Pass --status
explicitly (e.g. --status out_of_scope) or --search to override the default.

Pagination: results are ordered by id DESC (newest first). When the result
page may continue, a cursor is printed; pass it back with --cursor to
fetch the next page.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			if err := requireDecisionsDir(repoRoot); err != nil {
				return err
			}

			db, err := openIndex(repoRoot)
			if err != nil {
				return fmt.Errorf("ls: open index: %w", err)
			}
			defer db.Close()

			// Normalize repeatable / comma-separated values.
			statuses = splitCSV(statuses)
			priorities = splitCSV(priorities)
			tags = splitCSV(tags)

			// Validate status / priority values.
			for _, s := range statuses {
				if !validStatus(s) {
					return fmt.Errorf("ls: invalid status %q", s)
				}
			}
			for _, p := range priorities {
				if !validPriority(p) {
					return fmt.Errorf("ls: invalid priority %q", p)
				}
			}

			// Resolve limit.
			if limit <= 0 {
				limit = lsDefaultLimit
			}
			if limit > lsMaxLimit {
				limit = lsMaxLimit
			}

			// Resolve cursor (base64 of last seen id).
			cursorID := ""
			if cursor != "" {
				raw, err := base64.RawURLEncoding.DecodeString(cursor)
				if err != nil {
					return fmt.Errorf("ls: decode cursor: %w", err)
				}
				cursorID = string(raw)
			}

			// Resolve --since / --until.
			var sinceTime, untilTime time.Time
			if since != "" {
				t, err := ParseTimeFlag(since)
				if err != nil {
					return fmt.Errorf("ls: --since: %w", err)
				}
				sinceTime = t
			}
			if until != "" {
				t, err := ParseTimeFlag(until)
				if err != nil {
					return fmt.Errorf("ls: --until: %w", err)
				}
				untilTime = t
			}

			rows, err := lsQuery(db, lsFilter{
				Statuses:    statuses,
				Priorities:  priorities,
				Tags:        tags,
				Creator:     creator,
				Decider:     decider,
				Assigned:    assigned,
				Recommender: recommender,
				Since:       sinceTime,
				Until:       untilTime,
				Tree:        treeSlug,
				Unblocked:   unblocked,
				Search:      search,
				Limit:       limit,
				CursorID:    cursorID,
			})
			if err != nil {
				return fmt.Errorf("ls: query: %w", err)
			}

			// Compute next cursor when we filled the page.
			nextCursor := ""
			if len(rows) == limit && len(rows) > 0 {
				last := rows[len(rows)-1].ID
				nextCursor = base64.RawURLEncoding.EncodeToString([]byte(last))
			}

			format := outputFormat(cmd)
			switch format {
			case "json":
				return lsPrintJSON(cmd, rows, nextCursor)
			case "yaml":
				return lsPrintYAML(cmd, rows, nextCursor)
			case "ids":
				return lsPrintIDs(cmd, rows)
			default:
				return lsPrintHuman(cmd, rows, nextCursor)
			}
		},
	}

	cmd.Flags().StringSliceVar(&statuses, "status", nil, "Filter by status (repeatable, comma-separable)")
	cmd.Flags().StringSliceVar(&priorities, "priority", nil, "Filter by priority (repeatable, comma-separable)")
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "Filter by tag (AND across multiple --tag)")
	cmd.Flags().StringVar(&creator, "creator", "", "Filter by creator handle")
	cmd.Flags().StringVar(&decider, "decider", "", "Filter by decider handle (matches decision_deciders)")
	cmd.Flags().StringVar(&assigned, "assigned", "", "Filter by assignee handle")
	cmd.Flags().StringVar(&recommender, "recommender", "", "Filter by recommended_by handle")
	cmd.Flags().StringVar(&since, "since", "", "Only decisions whose id timestamp is >= since (RFC3339, YYYY-MM-DD, or relative like 7d)")
	cmd.Flags().StringVar(&until, "until", "", "Only decisions whose id timestamp is <= until")
	cmd.Flags().StringVar(&treeSlug, "tree", "", "Filter by tree slug")
	cmd.Flags().BoolVar(&unblocked, "unblocked", false, "Only decisions whose blocking deps are decided or scoped out")
	cmd.Flags().StringVar(&search, "search", "", "Substring match on summary or description (cheap LIKE)")
	cmd.Flags().IntVar(&limit, "limit", lsDefaultLimit, "Page size (max 1000)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor from a previous ls invocation")
	cmd.Flags().StringP("output", "o", "", "Output format: human, json, yaml, ids")

	return cmd
}

// ---------------------------------------------------------------------------
// SQL builder
// ---------------------------------------------------------------------------

// lsFilter holds normalized filter inputs for the SELECT.
type lsFilter struct {
	Statuses    []string
	Priorities  []string
	Tags        []string
	Creator     string
	Decider     string
	Assigned    string
	Recommender string
	Since       time.Time
	Until       time.Time
	Tree        string
	Unblocked   bool
	Search      string
	Limit       int
	CursorID    string
}

// lsQuery builds and executes the SELECT, returning the projected rows.
func lsQuery(db *index.DB, f lsFilter) ([]lsRow, error) {
	var (
		conds []string
		args  []any
	)

	conds = append(conds, "d.deleted = 0")

	// Default filter: when neither --status nor --search supplied, restrict to
	// proposed / decided.
	if len(f.Statuses) == 0 && f.Search == "" {
		conds = append(conds, "d.status IN ('proposed','decided')")
	}

	if len(f.Statuses) > 0 {
		conds = append(conds, "d.status IN ("+placeholders(len(f.Statuses))+")")
		for _, s := range f.Statuses {
			args = append(args, s)
		}
	}
	if len(f.Priorities) > 0 {
		conds = append(conds, "d.priority IN ("+placeholders(len(f.Priorities))+")")
		for _, p := range f.Priorities {
			args = append(args, p)
		}
	}
	if f.Creator != "" {
		conds = append(conds, "d.creator = ?")
		args = append(args, f.Creator)
	}
	if f.Assigned != "" {
		conds = append(conds, "d.assignee = ?")
		args = append(args, f.Assigned)
	}
	if f.Recommender != "" {
		conds = append(conds, "d.recommended_by = ?")
		args = append(args, f.Recommender)
	}
	if f.Tree != "" {
		conds = append(conds, "d.tree = ?")
		args = append(args, f.Tree)
	}
	if f.Decider != "" {
		conds = append(conds,
			"EXISTS (SELECT 1 FROM decision_deciders dd WHERE dd.decision_id = d.id AND dd.handle = ?)")
		args = append(args, f.Decider)
	}

	// AND across multiple --tag: each tag adds its own EXISTS.
	for _, tag := range f.Tags {
		conds = append(conds,
			"EXISTS (SELECT 1 FROM decision_tags dt WHERE dt.decision_id = d.id AND dt.tag = ?)")
		args = append(args, tag)
	}

	// --search: cheap LIKE on summary or description. Case-insensitive via
	// COLLATE NOCASE on the LIKE comparison.
	if f.Search != "" {
		pattern := "%" + escapeLike(f.Search) + "%"
		conds = append(conds,
			"(d.summary LIKE ? ESCAPE '\\' OR d.description LIKE ? ESCAPE '\\')")
		args = append(args, pattern, pattern)
	}

	// --since / --until on the ULID-encoded timestamp. We use ulid.Time on
	// each row in Go since SQLite cannot decode ULIDs; instead we filter by
	// a derived id range using the synthetic ULID for the time bound.
	if !f.Since.IsZero() {
		bound, err := ulidLowerBoundFor(f.Since)
		if err != nil {
			return nil, fmt.Errorf("ls: since bound: %w", err)
		}
		conds = append(conds, "d.id >= ?")
		args = append(args, bound)
	}
	if !f.Until.IsZero() {
		bound, err := ulidUpperBoundFor(f.Until)
		if err != nil {
			return nil, fmt.Errorf("ls: until bound: %w", err)
		}
		conds = append(conds, "d.id <= ?")
		args = append(args, bound)
	}

	// --unblocked: no remaining blocking edge whose source is still proposed.
	// Schema convention here: relationships(source, target, type='blocks') means
	// "source blocks target", so target is the decision under consideration and
	// it is unblocked once every such source is decided or out_of_scope.
	if f.Unblocked {
		conds = append(conds, `NOT EXISTS (
			SELECT 1 FROM relationships r
			JOIN decisions blocker ON blocker.id = r.source
			WHERE r.target = d.id
			  AND r.type = 'blocks'
			  AND blocker.deleted = 0
			  AND blocker.status NOT IN ('decided','out_of_scope')
		)`)
	}

	// Cursor: ORDER BY id DESC, so cursor means "rows with id < cursorID".
	if f.CursorID != "" {
		conds = append(conds, "d.id < ?")
		args = append(args, f.CursorID)
	}

	q := `SELECT d.id, d.tree, d.slug, d.summary, d.status, d.priority,
	             d.creator, d.assignee, d.rev
	      FROM decisions d
	      WHERE ` + strings.Join(conds, " AND ") + `
	      ORDER BY d.id DESC
	      LIMIT ?`
	args = append(args, f.Limit)

	rows, err := db.Conn().Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []lsRow
	for rows.Next() {
		var r lsRow
		if err := rows.Scan(&r.ID, &r.Tree, &r.Slug, &r.Summary, &r.Status,
			&r.Priority, &r.Creator, &r.Assignee, &r.Rev); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// placeholders returns "?,?,?" for the given count.
func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("?,", n-1) + "?"
}

// splitCSV expands repeated --flag a,b values from cobra's StringSlice handling.
// cobra's StringSlice already splits on commas, but we also trim whitespace and
// drop empties for safety.
func splitCSV(in []string) []string {
	var out []string
	for _, v := range in {
		for _, p := range strings.Split(v, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// escapeLike escapes %, _ and \ in a LIKE pattern using \ as the ESCAPE char.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// validStatus reports whether s is one of the four allowed status values.
func validStatus(s string) bool {
	switch core.Status(s) {
	case core.StatusProposed, core.StatusDecided, core.StatusOutOfScope, core.StatusSuperseded:
		return true
	}
	return false
}

// validPriority reports whether s is one of the five allowed priority values.
func validPriority(s string) bool {
	switch core.Priority(s) {
	case core.PriorityAssumption, core.PriorityLow, core.PriorityMedium,
		core.PriorityHigh, core.PriorityCritical:
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// ULID time bounds for --since / --until
// ---------------------------------------------------------------------------

// ulidLowerBoundFor returns a ULID whose timestamp equals t and whose
// random part is all zeros. Any decision created at or after t will sort
// >= this bound.
func ulidLowerBoundFor(t time.Time) (string, error) {
	return ulidBound(t, false)
}

// ulidUpperBoundFor returns a ULID whose timestamp equals t and whose
// random part is all 0xFF. Decisions created at or before t will sort
// <= this bound.
func ulidUpperBoundFor(t time.Time) (string, error) {
	return ulidBound(t, true)
}

// ulidBound builds a ULID at timestamp t with the entropy bytes set to
// either all-zeros (lower bound) or all-0xFF (upper bound). The result is
// the canonical 26-char Crockford base32 encoding, suitable for direct
// lexicographic comparison against stored decision IDs.
func ulidBound(t time.Time, upper bool) (string, error) {
	var entropy [10]byte
	if upper {
		for i := range entropy {
			entropy[i] = 0xFF
		}
	}
	var id uliddep.ULID
	if err := id.SetTime(uliddep.Timestamp(t)); err != nil {
		return "", err
	}
	if err := id.SetEntropy(entropy[:]); err != nil {
		return "", err
	}
	return id.String(), nil
}

// ---------------------------------------------------------------------------
// Output renderers
// ---------------------------------------------------------------------------

func lsPrintHuman(cmd *cobra.Command, rows []lsRow, nextCursor string) error {
	if len(rows) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "(no decisions)")
		return nil
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSTATUS\tPRIORITY\tSUMMARY\tTREE")
	for _, r := range rows {
		id := r.ID
		if len(id) > 8 {
			id = id[:8]
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			id, r.Status, r.Priority, truncate(r.Summary, 80), r.Tree)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if nextCursor != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "(more: --cursor=%s)\n", nextCursor)
	}
	return nil
}

func lsPrintJSON(cmd *cobra.Command, rows []lsRow, nextCursor string) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	res := lsResult{Items: rows, NextCursor: nextCursor}
	if res.Items == nil {
		res.Items = []lsRow{}
	}
	return enc.Encode(res)
}

func lsPrintYAML(cmd *cobra.Command, rows []lsRow, nextCursor string) error {
	enc := yaml.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent(2)
	res := lsResult{Items: rows, NextCursor: nextCursor}
	if res.Items == nil {
		res.Items = []lsRow{}
	}
	if err := enc.Encode(res); err != nil {
		return err
	}
	return enc.Close()
}

func lsPrintIDs(cmd *cobra.Command, rows []lsRow) error {
	for _, r := range rows {
		fmt.Fprintln(cmd.OutOrStdout(), r.ID)
	}
	return nil
}

// truncate returns s shortened to n runes with "..." suffix when needed.
func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}
