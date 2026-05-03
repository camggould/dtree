package cli

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/index"
	dtreesync "github.com/cgould/dtree/internal/sync"
	"github.com/cgould/dtree/internal/validate"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// StatusReport holds all gathered status information.
type StatusReport struct {
	IndexDirty           bool           `json:"index_dirty" yaml:"index_dirty"`
	MigrationNeeded      bool           `json:"migration_needed" yaml:"migration_needed"`
	SchemaVersionCurrent int            `json:"schema_version_current" yaml:"schema_version_current"`
	SchemaVersionTarget  int            `json:"schema_version_target" yaml:"schema_version_target"`
	ExternalEdits        int            `json:"external_edits" yaml:"external_edits"`
	ValidationViolations int            `json:"validation_violations" yaml:"validation_violations"`
	Trees                int            `json:"trees" yaml:"trees"`
	TreesArchived        int            `json:"trees_archived" yaml:"trees_archived"`
	DecisionsByStatus    map[string]int `json:"decisions_by_status" yaml:"decisions_by_status"`
	AuditEventsTotal     int            `json:"audit_events_total" yaml:"audit_events_total"`
	AuditEventsByMonth   map[string]int `json:"audit_events_by_month" yaml:"audit_events_by_month"`
}

// IsClean returns true when no issues are detected.
func (r *StatusReport) IsClean() bool {
	return !r.IndexDirty && !r.MigrationNeeded && r.ExternalEdits == 0 && r.ValidationViolations == 0
}

// newStatusCommand returns the `dtree status` command.
func newStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show repository health and summary statistics",
		Long:  "Display index health, decision counts, and audit event statistics.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			outputFlag, _ := cmd.Root().PersistentFlags().GetString("output")
			format := resolveFormat(cmd, outputFlag)

			report, err := gatherStatus(repoRoot)
			if err != nil {
				return err
			}

			switch format {
			case "json":
				if err := statusJSON(cmd, report); err != nil {
					return err
				}
			case "yaml":
				if err := statusYAML(cmd, report); err != nil {
					return err
				}
			default:
				statusHuman(cmd, report)
			}

			if !report.IsClean() {
				return fmt.Errorf("status: issues detected")
			}
			return nil
		},
	}
	return cmd
}

// gatherStatus collects all status information for the given repo root.
func gatherStatus(repoRoot string) (*StatusReport, error) {
	r := &StatusReport{
		DecisionsByStatus:  make(map[string]int),
		AuditEventsByMonth: make(map[string]int),
	}

	// Check dirty marker.
	dirty, err := index.IsDirty(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("status: check dirty: %w", err)
	}
	r.IndexDirty = dirty

	// Open index.
	indexPath := filepath.Join(repoRoot, ".decisions", ".index.db")
	db, err := index.Open(indexPath)
	if err != nil {
		return nil, fmt.Errorf("status: open index: %w", err)
	}
	defer db.Close()

	// Check migration.
	current, target, needed := db.NeedsMigration()
	r.MigrationNeeded = needed
	r.SchemaVersionCurrent = current
	r.SchemaVersionTarget = target

	// Tree counts.
	if err := db.Conn().QueryRow(`SELECT COUNT(*) FROM trees WHERE archived=0`).Scan(&r.Trees); err != nil {
		return nil, fmt.Errorf("status: count trees: %w", err)
	}
	if err := db.Conn().QueryRow(`SELECT COUNT(*) FROM trees WHERE archived=1`).Scan(&r.TreesArchived); err != nil {
		return nil, fmt.Errorf("status: count archived trees: %w", err)
	}

	// Decision counts by status.
	rows, err := db.Conn().Query(`SELECT status, COUNT(*) FROM decisions WHERE deleted=0 GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("status: count decisions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		r.DecisionsByStatus[status] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Audit events.
	events, err := audit.Read(repoRoot, audit.Filter{})
	if err != nil {
		return nil, fmt.Errorf("status: read audit: %w", err)
	}
	r.AuditEventsTotal = len(events)
	for _, ev := range events {
		bucket := ev.Ts.UTC().Format("2006-01")
		r.AuditEventsByMonth[bucket]++
	}

	// External edits (sync scan).
	mismatches, err := dtreesync.Scan(repoRoot, db)
	if err != nil {
		// Non-fatal: report 0.
		mismatches = nil
	}
	r.ExternalEdits = len(mismatches)

	// Validation violations (fsck check).
	violations, err := countViolations(repoRoot, db)
	if err != nil {
		return nil, fmt.Errorf("status: violations: %w", err)
	}
	r.ValidationViolations = violations

	return r, nil
}

// countViolations returns the total number of validation violations across all decisions.
func countViolations(_ string, db *index.DB) (int, error) {
	decisions, err := loadAllDecisions(db)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, d := range decisions {
		errs := validate.CollectDecision(d)
		total += len(errs)
	}

	// Graph check.
	edges, err := loadEdges(db)
	if err != nil {
		return 0, err
	}
	if graphErr := validate.Graph(edges); graphErr != nil {
		total++
	}
	return total, nil
}

func statusHuman(cmd *cobra.Command, r *StatusReport) {
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "=== dtree status ===")
	fmt.Fprintf(out, "Index dirty:           %v\n", r.IndexDirty)
	fmt.Fprintf(out, "Migration needed:      %v", r.MigrationNeeded)
	if r.MigrationNeeded {
		fmt.Fprintf(out, " (current=%d, target=%d)", r.SchemaVersionCurrent, r.SchemaVersionTarget)
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "External edits:        %d\n", r.ExternalEdits)
	fmt.Fprintf(out, "Validation violations: %d\n", r.ValidationViolations)
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Trees (active):   %d\n", r.Trees)
	fmt.Fprintf(out, "Trees (archived): %d\n", r.TreesArchived)
	fmt.Fprintln(out, "Decisions by status:")
	for _, s := range []string{"proposed", "decided", "out_of_scope", "superseded"} {
		fmt.Fprintf(out, "  %-14s %d\n", s+":", r.DecisionsByStatus[s])
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Audit events total: %d\n", r.AuditEventsTotal)
	if len(r.AuditEventsByMonth) > 0 {
		fmt.Fprintln(out, "Audit events by month:")
		months := mapKeys(r.AuditEventsByMonth)
		sortStrings(months)
		for _, m := range months {
			fmt.Fprintf(out, "  %s: %d\n", m, r.AuditEventsByMonth[m])
		}
	}
	if r.IsClean() {
		fmt.Fprintln(out, "\nAll clean.")
	} else {
		fmt.Fprintln(out, "\nIssues detected (see above).")
	}
}

func statusJSON(cmd *cobra.Command, r *StatusReport) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func statusYAML(cmd *cobra.Command, r *StatusReport) error {
	enc := yaml.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent(2)
	if err := enc.Encode(r); err != nil {
		return err
	}
	return enc.Close()
}

// ---------------------------------------------------------------------------
// Shared helpers used by status and fsck
// ---------------------------------------------------------------------------

// loadAllDecisions loads all non-deleted decisions from the index.
// It performs junction table lookups for each decision.
func loadAllDecisions(db *index.DB) ([]*core.Decision, error) {
	rows, err := db.Conn().Query(`
		SELECT id, tree, slug, summary, description,
		       status, priority, creator, assignee,
		       recommended_summary, recommended_full, recommended_by,
		       actual_choice, actual_choice_reason, is_recommended,
		       out_of_scope_reason, schema_version, rev
		FROM decisions WHERE deleted=0`)
	if err != nil {
		return nil, fmt.Errorf("load decisions: %w", err)
	}
	defer rows.Close()

	var out []*core.Decision
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Load junction tables for each decision.
	for _, d := range out {
		if err := loadDecisionJunctions(db, d); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// scanDecision scans one row from the decisions SELECT into a core.Decision.
func scanDecision(rows *sql.Rows) (*core.Decision, error) {
	d := &core.Decision{}
	var isRec int
	err := rows.Scan(
		&d.ID, &d.Tree, &d.Slug, &d.Summary, &d.Description,
		(*string)(&d.Status), (*string)(&d.Priority), &d.Creator, &d.Assignee,
		&d.RecommendedSummary, &d.RecommendedFull, &d.RecommendedBy,
		&d.ActualChoice, &d.ActualChoiceReason, &isRec,
		&d.OutOfScopeReason, &d.SchemaVersion, &d.Rev,
	)
	if err != nil {
		return nil, fmt.Errorf("scan decision: %w", err)
	}
	d.IsRecommended = isRec == 1
	return d, nil
}

// loadDecisionJunctions fills d.DecidedBy, d.Tags, d.Relationships from the
// index junction tables.
func loadDecisionJunctions(db *index.DB, d *core.Decision) error {
	// decided_by
	deciders, err := queryStrings(db, `SELECT handle FROM decision_deciders WHERE decision_id=? ORDER BY handle`, d.ID)
	if err != nil {
		return fmt.Errorf("load deciders %s: %w", d.ID, err)
	}
	d.DecidedBy = deciders

	// tags
	tags, err := queryStrings(db, `SELECT tag FROM decision_tags WHERE decision_id=? ORDER BY tag`, d.ID)
	if err != nil {
		return fmt.Errorf("load tags %s: %w", d.ID, err)
	}
	d.Tags = tags

	// relationships
	rrows, err := db.Conn().Query(`SELECT target, type FROM relationships WHERE source=? ORDER BY target`, d.ID)
	if err != nil {
		return fmt.Errorf("load relationships %s: %w", d.ID, err)
	}
	defer rrows.Close()
	for rrows.Next() {
		var r core.Relationship
		if err := rrows.Scan(&r.Target, (*string)(&r.Type)); err != nil {
			return err
		}
		d.Relationships = append(d.Relationships, r)
	}
	return rrows.Err()
}

// queryStrings executes a query returning a single TEXT column and returns the results.
func queryStrings(db *index.DB, query, id string) ([]string, error) {
	rows, err := db.Conn().Query(query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// loadEdges returns all directed edges from the index relationships table.
func loadEdges(db *index.DB) ([]validate.Edge, error) {
	rows, err := db.Conn().Query(`SELECT source, target, type FROM relationships`)
	if err != nil {
		return nil, fmt.Errorf("load edges: %w", err)
	}
	defer rows.Close()
	var edges []validate.Edge
	for rows.Next() {
		var e validate.Edge
		if err := rows.Scan(&e.Source, &e.Target, (*string)(&e.Type)); err != nil {
			return nil, err
		}
		edges = append(edges, e)
	}
	return edges, rows.Err()
}

// mapKeys returns the keys of a map[string]int.
func mapKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// sortStrings sorts a slice of strings in place (insertion sort for small slices).
func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		key := ss[i]
		j := i - 1
		for j >= 0 && ss[j] > key {
			ss[j+1] = ss[j]
			j--
		}
		ss[j+1] = key
	}
}
