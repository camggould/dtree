// Package cli — `dtree scope-out` marks a decision as out-of-scope and
// records the reason. Mirrors the HTTP POST /scope-out handler.
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

// newScopeOutCommand returns the `dtree scope-out` command.
func newScopeOutCommand() *cobra.Command {
	var (
		reason string
		asFlag string
	)

	cmd := &cobra.Command{
		Use:   "scope-out <id>",
		Short: "Mark a decision as out-of-scope with a reason",
		Long: "Set status=out_of_scope on a proposed decision and record the " +
			"reason. The HTTP scope-out handler does not constrain the prior " +
			"status, so this command also accepts any non-out_of_scope state.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			if err := requireDecisionsDir(repoRoot); err != nil {
				return fmt.Errorf("%w; run `dtree init`", err)
			}

			if strings.TrimSpace(reason) == "" {
				return fmt.Errorf("--reason is required")
			}

			cfg, err := config.Load(repoRoot)
			if err != nil {
				return fmt.Errorf("scope-out: load config: %w", err)
			}
			res, err := identity.NewResolver(repoRoot, cfg).MustResolve(asFlag)
			if err != nil {
				return fmt.Errorf("scope-out: resolve identity: %w", err)
			}
			actor := res.Handle

			db, err := openIndex(repoRoot)
			if err != nil {
				return fmt.Errorf("scope-out: open index: %w", err)
			}
			defer db.Close()

			id, err := resolveDecisionID(db, args[0])
			if err != nil {
				return fmt.Errorf("scope-out: %w", err)
			}

			indexed, err := index.GetDecision(db, id)
			if err != nil {
				return fmt.Errorf("scope-out: get from index: %w", err)
			}
			if indexed == nil {
				return fmt.Errorf("scope-out: decision %s not found in index", id)
			}
			oldRev, err := index.GetDecisionRev(db, id)
			if err != nil {
				return fmt.Errorf("scope-out: read rev: %w", err)
			}

			treeDir := filepath.Join(repoRoot, ".decisions", indexed.Tree)
			path := storage.DecisionPath(treeDir, indexed.ID, indexed.Slug)
			d, err := storage.ReadDecision(path)
			if err != nil {
				return fmt.Errorf("scope-out: read decision file: %w", err)
			}
			d.ID = indexed.ID
			d.Tree = indexed.Tree

			if d.Status == core.StatusOutOfScope {
				return fmt.Errorf("scope-out: decision %s is already out_of_scope", id)
			}

			before := decisionToMap(d)

			d.Status = core.StatusOutOfScope
			d.OutOfScopeReason = reason

			if err := validate.Decision(d); err != nil {
				return fmt.Errorf("scope-out: validation: %w", err)
			}

			if err := storage.WriteDecision(path, d); err != nil {
				return fmt.Errorf("scope-out: write decision: %w", err)
			}

			contentSha, err := fsutil.Sha256File(path)
			if err != nil {
				return fmt.Errorf("scope-out: sha256: %w", err)
			}
			newRev := concurrency.NewRev()
			if err := index.UpdateDecisionWithExpectedRev(db, d, contentSha, oldRev, newRev); err != nil {
				if c, ok := concurrency.AsConflict(err); ok {
					return fmt.Errorf("scope-out: rev conflict on %s: expected %q but index now has %q",
						c.DecisionID, c.ExpectedRev, c.ActualRev)
				}
				return fmt.Errorf("scope-out: update index: %w", err)
			}

			ev := core.Event{
				Actor:  actor,
				Action: core.ActionScopeOut,
				Kind:   core.KindDecision,
				Tree:   d.Tree,
				ID:     d.ID,
				Payload: core.EventPayload{
					Before: before,
					After: map[string]any{
						"status":              string(d.Status),
						"out_of_scope_reason": d.OutOfScopeReason,
					},
					Extra: map[string]any{"reason": reason},
				},
			}
			if err := audit.Append(repoRoot, ev); err != nil {
				return fmt.Errorf("scope-out: audit: %w", err)
			}

			d.Rev = newRev
			return printDecision(cmd, d, outputFormat(cmd))
		},
	}

	cmd.Flags().StringVar(&reason, "reason", "", "Reason the decision is out-of-scope (required)")
	cmd.Flags().StringVar(&asFlag, "as", "", "Identity override (handle)")
	cmd.Flags().String("output", "", "Output format: human, json, yaml")
	return cmd
}
