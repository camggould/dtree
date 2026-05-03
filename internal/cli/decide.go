// Package cli — `dtree decide` records the outcome on a proposed decision.
//
// Workflow: load decision from disk, mutate to status=decided + outcome
// fields, write file atomically, update the index with optimistic
// concurrency, append a `decide` audit event.
package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/concurrency"
	"github.com/cgould/dtree/internal/config"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/fsutil"
	"github.com/cgould/dtree/internal/identity"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/storage"
	"github.com/cgould/dtree/internal/validate"
	"github.com/spf13/cobra"
)

// newDecideCommand returns the `dtree decide` command.
func newDecideCommand() *cobra.Command {
	var (
		choice         string
		reason         string
		by             []string
		isRecommended  bool
		asFlag         string
	)

	cmd := &cobra.Command{
		Use:   "decide <id>",
		Short: "Record the outcome of a proposed decision",
		Long:  "Mark a proposed decision as decided, recording the actual choice, reason, and deciders.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			if err := requireDecisionsDir(repoRoot); err != nil {
				return fmt.Errorf("%w; run `dtree init`", err)
			}

			// Required flags.
			if strings.TrimSpace(choice) == "" {
				return fmt.Errorf("--choice is required")
			}
			if strings.TrimSpace(reason) == "" {
				return fmt.Errorf("--reason is required")
			}
			if len(by) == 0 {
				return fmt.Errorf("--by is required (at least one handle)")
			}

			cfg, err := config.Load(repoRoot)
			if err != nil {
				return fmt.Errorf("decide: load config: %w", err)
			}
			resolver := identity.NewResolver(repoRoot, cfg)
			res, err := resolver.MustResolve(asFlag)
			if err != nil {
				return fmt.Errorf("decide: resolve identity: %w", err)
			}
			actor := res.Handle

			// Validate every --by handle exists.
			for _, h := range by {
				h = strings.TrimSpace(h)
				if h == "" {
					return fmt.Errorf("--by handle must not be empty")
				}
				a, err := resolver.FindActor(h)
				if err != nil {
					return fmt.Errorf("decide: lookup actor %q: %w", h, err)
				}
				if a == nil {
					return fmt.Errorf("decide: unknown actor %q (run `dtree actor add %s`)", h, h)
				}
			}

			// Open index and resolve ID prefix.
			db, err := openIndex(repoRoot)
			if err != nil {
				return fmt.Errorf("decide: open index: %w", err)
			}
			defer db.Close()

			id, err := resolveDecisionID(db, args[0])
			if err != nil {
				return fmt.Errorf("decide: %w", err)
			}

			// Load current state from index for tree slug + rev.
			indexed, err := index.GetDecision(db, id)
			if err != nil {
				return fmt.Errorf("decide: get from index: %w", err)
			}
			if indexed == nil {
				return fmt.Errorf("decide: decision %s not found in index", id)
			}
			oldRev, err := index.GetDecisionRev(db, id)
			if err != nil {
				return fmt.Errorf("decide: read rev: %w", err)
			}

			// Build canonical file path and read disk copy.
			treeDir := filepath.Join(repoRoot, ".decisions", indexed.Tree)
			path := storage.DecisionPath(treeDir, indexed.ID, indexed.Slug)
			d, err := storage.ReadDecision(path)
			if err != nil {
				return fmt.Errorf("decide: read decision file: %w", err)
			}
			d.ID = indexed.ID
			d.Tree = indexed.Tree

			// Refuse if not proposed.
			if d.Status != core.StatusProposed {
				return fmt.Errorf("decide: decision %s has status %q; only `proposed` decisions can be decided", id, d.Status)
			}

			// Snapshot before for audit.
			before := decisionToMap(d)

			// Apply mutations.
			d.Status = core.StatusDecided
			d.ActualChoice = choice
			d.ActualChoiceReason = reason
			d.DecidedBy = append([]string(nil), by...)
			d.IsRecommended = isRecommended

			// Validate.
			if err := validate.Decision(d); err != nil {
				return fmt.Errorf("decide: validation: %w", err)
			}

			// Write file atomically.
			if err := storage.WriteDecision(path, d); err != nil {
				return fmt.Errorf("decide: write decision: %w", err)
			}

			// Compute new content sha and rev, update index with expected rev.
			contentSha, err := fsutil.Sha256File(path)
			if err != nil {
				return fmt.Errorf("decide: sha256: %w", err)
			}
			newRev := concurrency.NewRev()
			if err := index.UpdateDecisionWithExpectedRev(db, d, contentSha, oldRev, newRev); err != nil {
				if c, ok := concurrency.AsConflict(err); ok {
					return fmt.Errorf("decide: rev conflict on %s: expected %q but index now has %q (someone else updated it; re-run after `dtree show %s`)",
						c.DecisionID, c.ExpectedRev, c.ActualRev, id)
				}
				return fmt.Errorf("decide: update index: %w", err)
			}

			// Append audit event.
			after := decisionToMap(d)
			ev := core.Event{
				Actor:  actor,
				Action: core.ActionDecide,
				Kind:   core.KindDecision,
				Tree:   d.Tree,
				ID:     d.ID,
				Payload: core.EventPayload{
					Before: before,
					After:  after,
				},
			}
			if err := audit.Append(repoRoot, ev); err != nil {
				return fmt.Errorf("decide: audit: %w", err)
			}

			// Output.
			d.Rev = newRev
			format := outputFormat(cmd)
			return printDecision(cmd, d, format)
		},
	}

	cmd.Flags().StringVar(&choice, "choice", "", "The actual choice that was made (required)")
	cmd.Flags().StringVar(&reason, "reason", "", "Reason for the choice (required)")
	cmd.Flags().StringArrayVar(&by, "by", nil, "Decider handle (repeatable, at least one required)")
	cmd.Flags().BoolVar(&isRecommended, "is-recommended", false, "Whether the chosen option matches the recommendation")
	cmd.Flags().StringVar(&asFlag, "as", "", "Identity override (handle)")
	cmd.Flags().String("output", "", "Output format: human, json, yaml")

	return cmd
}

