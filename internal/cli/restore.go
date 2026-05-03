// Package cli — `dtree restore` reverts an out_of_scope decision back to
// proposed. Mirrors the HTTP POST /restore handler.
package cli

import (
	"fmt"
	"path/filepath"

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

// newRestoreCommand returns the `dtree restore` command.
func newRestoreCommand() *cobra.Command {
	var asFlag string

	cmd := &cobra.Command{
		Use:   "restore <id>",
		Short: "Restore an out-of-scope decision back to proposed",
		Long:  "Sets status=proposed and clears the out-of-scope reason. Refused unless the decision is currently out_of_scope.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			if err := requireDecisionsDir(repoRoot); err != nil {
				return fmt.Errorf("%w; run `dtree init`", err)
			}

			cfg, err := config.Load(repoRoot)
			if err != nil {
				return fmt.Errorf("restore: load config: %w", err)
			}
			res, err := identity.NewResolver(repoRoot, cfg).MustResolve(asFlag)
			if err != nil {
				return fmt.Errorf("restore: resolve identity: %w", err)
			}
			actor := res.Handle

			db, err := openIndex(repoRoot)
			if err != nil {
				return fmt.Errorf("restore: open index: %w", err)
			}
			defer db.Close()

			id, err := resolveDecisionID(db, args[0])
			if err != nil {
				return fmt.Errorf("restore: %w", err)
			}

			indexed, err := index.GetDecision(db, id)
			if err != nil {
				return fmt.Errorf("restore: get from index: %w", err)
			}
			if indexed == nil {
				return fmt.Errorf("restore: decision %s not found in index", id)
			}
			oldRev, err := index.GetDecisionRev(db, id)
			if err != nil {
				return fmt.Errorf("restore: read rev: %w", err)
			}

			treeDir := filepath.Join(repoRoot, ".decisions", indexed.Tree)
			path := storage.DecisionPath(treeDir, indexed.ID, indexed.Slug)
			d, err := storage.ReadDecision(path)
			if err != nil {
				return fmt.Errorf("restore: read decision file: %w", err)
			}
			d.ID = indexed.ID
			d.Tree = indexed.Tree

			if d.Status != core.StatusOutOfScope {
				return fmt.Errorf("restore: decision %s has status %q; only `out_of_scope` decisions can be restored", id, d.Status)
			}

			before := decisionToMap(d)

			d.Status = core.StatusProposed
			d.OutOfScopeReason = ""

			if err := validate.Decision(d); err != nil {
				return fmt.Errorf("restore: validation: %w", err)
			}

			if err := storage.WriteDecision(path, d); err != nil {
				return fmt.Errorf("restore: write decision: %w", err)
			}

			contentSha, err := fsutil.Sha256File(path)
			if err != nil {
				return fmt.Errorf("restore: sha256: %w", err)
			}
			newRev := concurrency.NewRev()
			if err := index.UpdateDecisionWithExpectedRev(db, d, contentSha, oldRev, newRev); err != nil {
				if c, ok := concurrency.AsConflict(err); ok {
					return fmt.Errorf("restore: rev conflict on %s: expected %q but index now has %q",
						c.DecisionID, c.ExpectedRev, c.ActualRev)
				}
				return fmt.Errorf("restore: update index: %w", err)
			}

			after := decisionToMap(d)
			ev := core.Event{
				Actor:  actor,
				Action: core.ActionRestore,
				Kind:   core.KindDecision,
				Tree:   d.Tree,
				ID:     d.ID,
				Payload: core.EventPayload{
					Before: before,
					After:  after,
				},
			}
			if err := audit.Append(repoRoot, ev); err != nil {
				return fmt.Errorf("restore: audit: %w", err)
			}

			d.Rev = newRev
			return printDecision(cmd, d, outputFormat(cmd))
		},
	}

	cmd.Flags().StringVar(&asFlag, "as", "", "Identity override (handle)")
	cmd.Flags().String("output", "", "Output format: human, json, yaml")
	return cmd
}
