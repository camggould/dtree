// Package cli — `dtree show` resolves a decision by ULID prefix or fuzzy
// summary substring and renders its full record.
package cli

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/index"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// showRelationship is a Relationship augmented with the resolved target summary.
type showRelationship struct {
	Type          core.RelationshipType `json:"type" yaml:"type"`
	Target        string                `json:"target" yaml:"target"`
	TargetSummary string                `json:"target_summary,omitempty" yaml:"target_summary,omitempty"`
}

// showResult is the shape rendered to JSON/YAML. Mirrors core.Decision but
// swaps Relationships for showRelationship to surface target_summary, and
// inlines created/updated timestamps derived from the events log.
type showResult struct {
	*core.Decision
	Relationships []showRelationship `json:"relationships,omitempty" yaml:"relationships,omitempty"`
	CreatedAt     *time.Time         `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	UpdatedAt     *time.Time         `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
}

// matchRow is a (id, summary) pair surfaced by resolveDecisionID for ambiguity.
type matchRow struct {
	ID      string
	Summary string
}

// newShowCommand returns the `dtree show <id>` subcommand.
func newShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show a decision by id-prefix or summary substring",
		Long: "Show resolves a decision by ULID prefix (case-insensitive, ≥4 chars) " +
			"or a fuzzy summary substring, then renders the full record.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.TrimSpace(args[0])
			if query == "" {
				return fmt.Errorf("show: id is required")
			}
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")

			if err := requireDecisionsDir(repoRoot); err != nil {
				return err
			}

			db, err := openIndex(repoRoot)
			if err != nil {
				return fmt.Errorf("show: open index: %w", err)
			}
			defer db.Close()

			id, err := resolveDecisionID(db, query)
			if err != nil {
				return err
			}

			d, err := index.GetDecision(db, id)
			if err != nil {
				return fmt.Errorf("show: get decision: %w", err)
			}
			if d == nil {
				return fmt.Errorf("show: decision %s vanished after resolution", id)
			}

			rels, err := enrichRelationships(db, d.Relationships)
			if err != nil {
				return fmt.Errorf("show: enrich relationships: %w", err)
			}

			created, updated, err := decisionTimestamps(db, d.ID)
			if err != nil {
				return fmt.Errorf("show: load timestamps: %w", err)
			}

			result := showResult{
				Decision:      d,
				Relationships: rels,
				CreatedAt:     created,
				UpdatedAt:     updated,
			}

			format := outputFormat(cmd)
			switch format {
			case "json":
				return showJSON(cmd, result)
			case "yaml":
				return showYAML(cmd, result)
			default:
				return showHuman(cmd, result)
			}
		},
	}

	cmd.Flags().String("output", "", "Output format: human, json, yaml")
	return cmd
}

// resolveDecisionID maps a free-form query to exactly one decision id.
// Resolution tiers (in order; first non-empty match wins):
//  1. exact id
//  2. id prefix (case-insensitive, ≥4 chars)
//  3. summary substring (case-insensitive)
//
// Multiple matches in a tier produce an "ambiguous" error listing them.
// Zero matches across all tiers produces a "no decision matching" error.
func resolveDecisionID(db *index.DB, query string) (string, error) {
	upper := strings.ToUpper(query)

	// Tier 1: exact id (only meaningful for full-length ULIDs).
	if len(upper) == 26 {
		var id string
		err := db.Conn().QueryRow(
			`SELECT id FROM decisions WHERE id = ? AND deleted = 0`, upper,
		).Scan(&id)
		if err == nil {
			return id, nil
		}
		if err != nil && err != sql.ErrNoRows {
			return "", fmt.Errorf("show: resolve exact id: %w", err)
		}
	}

	// Tier 2: id prefix (case-insensitive). Require ≥4 chars to avoid
	// matching everything; ULIDs are uppercase Crockford base32 in storage.
	if len(upper) >= 4 {
		matches, err := queryMatches(db,
			`SELECT id, summary FROM decisions
			 WHERE id LIKE ? AND deleted = 0
			 ORDER BY id`,
			upper+"%")
		if err != nil {
			return "", fmt.Errorf("show: resolve prefix: %w", err)
		}
		switch len(matches) {
		case 1:
			return matches[0].ID, nil
		default:
			if len(matches) > 1 {
				return "", ambiguousError(query, matches)
			}
		}
	}

	// Tier 3: summary substring (case-insensitive).
	matches, err := queryMatches(db,
		`SELECT id, summary FROM decisions
		 WHERE summary LIKE ? COLLATE NOCASE AND deleted = 0
		 ORDER BY id`,
		"%"+query+"%")
	if err != nil {
		return "", fmt.Errorf("show: resolve summary: %w", err)
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no decision matching %q", query)
	case 1:
		return matches[0].ID, nil
	default:
		return "", ambiguousError(query, matches)
	}
}

func queryMatches(db *index.DB, query string, arg string) ([]matchRow, error) {
	rows, err := db.Conn().Query(query, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []matchRow
	for rows.Next() {
		var m matchRow
		if err := rows.Scan(&m.ID, &m.Summary); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func ambiguousError(query string, matches []matchRow) error {
	var b strings.Builder
	fmt.Fprintf(&b, "ambiguous: matches %d decisions for %q:", len(matches), query)
	for _, m := range matches {
		fmt.Fprintf(&b, "\n  %s  %s", m.ID, m.Summary)
	}
	return fmt.Errorf("%s", b.String())
}

// enrichRelationships looks up each relationship's target summary and returns
// a parallel slice of showRelationship values.
func enrichRelationships(db *index.DB, rels []core.Relationship) ([]showRelationship, error) {
	if len(rels) == 0 {
		return nil, nil
	}
	out := make([]showRelationship, 0, len(rels))
	for _, r := range rels {
		var summary string
		err := db.Conn().QueryRow(
			`SELECT summary FROM decisions WHERE id = ?`, r.Target,
		).Scan(&summary)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		out = append(out, showRelationship{
			Type:          r.Type,
			Target:        r.Target,
			TargetSummary: summary,
		})
	}
	return out, nil
}

// decisionTimestamps derives created_at (earliest event) and updated_at
// (latest event) for a decision id from the events table. Returns nil
// pointers when there are no events recorded.
func decisionTimestamps(db *index.DB, id string) (*time.Time, *time.Time, error) {
	var minTs, maxTs sql.NullString
	err := db.Conn().QueryRow(
		`SELECT MIN(ts), MAX(ts) FROM events WHERE target_id = ? AND kind = 'decision'`,
		id,
	).Scan(&minTs, &maxTs)
	if err != nil && err != sql.ErrNoRows {
		return nil, nil, err
	}
	parse := func(s sql.NullString) *time.Time {
		if !s.Valid || s.String == "" {
			return nil
		}
		t, perr := time.Parse(time.RFC3339, s.String)
		if perr != nil {
			return nil
		}
		t = t.UTC()
		return &t
	}
	return parse(minTs), parse(maxTs), nil
}

// ---------------------------------------------------------------------------
// Renderers
// ---------------------------------------------------------------------------

func showJSON(cmd *cobra.Command, r showResult) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func showYAML(cmd *cobra.Command, r showResult) error {
	// Build a plain map so the embedded *core.Decision flattens cleanly under
	// YAML (gopkg.in/yaml.v3 does not honor json-style inline embedding).
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	enc := yaml.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent(2)
	if err := enc.Encode(m); err != nil {
		_ = enc.Close()
		return err
	}
	return enc.Close()
}

func showHuman(cmd *cobra.Command, r showResult) error {
	w := cmd.OutOrStdout()
	d := r.Decision

	fmt.Fprintf(w, "ID:        %s\n", d.ID)
	fmt.Fprintf(w, "Summary:   %s\n", d.Summary)
	fmt.Fprintf(w, "Tree:      %s\n", d.Tree)
	fmt.Fprintf(w, "Status:    %s\n", d.Status)
	fmt.Fprintf(w, "Priority:  %s\n", d.Priority)

	if d.Description != "" {
		fmt.Fprintf(w, "\nDescription:\n%s\n", indent(d.Description, "  "))
	}

	if d.RecommendedSummary != "" || d.RecommendedFull != "" || d.RecommendedBy != "" {
		fmt.Fprintln(w, "\nRecommended:")
		if d.RecommendedBy != "" {
			fmt.Fprintf(w, "  By:      %s\n", d.RecommendedBy)
		}
		if d.RecommendedSummary != "" {
			fmt.Fprintf(w, "  Summary: %s\n", d.RecommendedSummary)
		}
		if d.RecommendedFull != "" {
			fmt.Fprintf(w, "  Full:\n%s\n", indent(d.RecommendedFull, "    "))
		}
	}

	if d.ActualChoice != "" || d.ActualChoiceReason != "" {
		fmt.Fprintln(w, "\nActual choice:")
		if d.ActualChoice != "" {
			fmt.Fprintf(w, "  Choice: %s\n", d.ActualChoice)
		}
		if d.ActualChoiceReason != "" {
			fmt.Fprintf(w, "  Reason: %s\n", d.ActualChoiceReason)
		}
		fmt.Fprintf(w, "  Was recommended: %v\n", d.IsRecommended)
	}

	if d.OutOfScopeReason != "" {
		fmt.Fprintf(w, "\nOut-of-scope reason: %s\n", d.OutOfScopeReason)
	}

	if len(d.Tags) > 0 {
		fmt.Fprintf(w, "\nTags:      %s\n", strings.Join(d.Tags, ", "))
	}
	if len(d.DecidedBy) > 0 {
		fmt.Fprintf(w, "Deciders:  %s\n", strings.Join(d.DecidedBy, ", "))
	}
	if d.Assignee != "" {
		fmt.Fprintf(w, "Assignee:  %s\n", d.Assignee)
	}
	if d.Creator != "" {
		fmt.Fprintf(w, "Creator:   %s\n", d.Creator)
	}

	if len(r.Relationships) > 0 {
		fmt.Fprintln(w, "\nRelationships:")
		for _, rel := range r.Relationships {
			summary := rel.TargetSummary
			if summary == "" {
				summary = "(unknown)"
			}
			fmt.Fprintf(w, "  %-12s %s  %s\n", rel.Type, rel.Target, summary)
		}
	}

	if r.CreatedAt != nil {
		fmt.Fprintf(w, "\nCreated:   %s\n", r.CreatedAt.Format(time.RFC3339))
	}
	if r.UpdatedAt != nil {
		fmt.Fprintf(w, "Updated:   %s\n", r.UpdatedAt.Format(time.RFC3339))
	}
	return nil
}

// indent prefixes every line of s with prefix.
func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}
