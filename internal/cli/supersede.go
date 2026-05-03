// Package cli — `dtree supersede` marks an old decision as superseded by a
// new one. Mirrors the HTTP POST /supersede handler: both endpoints get a
// `supersedes` relationship edge (the schema doesn't carry directionality
// in the relationship type, so we record reciprocal edges).
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

// newSupersedeCommand returns the `dtree supersede` command.
func newSupersedeCommand() *cobra.Command {
	var (
		by     string
		asFlag string
	)

	cmd := &cobra.Command{
		Use:   "supersede <old-id>",
		Short: "Mark a decision as superseded by another",
		Long: "Set status=superseded on the old decision and add reciprocal " +
			"`supersedes` relationship edges between the old and new decisions.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			if err := requireDecisionsDir(repoRoot); err != nil {
				return fmt.Errorf("%w; run `dtree init`", err)
			}

			if by == "" {
				return fmt.Errorf("--by is required (id of the new decision)")
			}

			cfg, err := config.Load(repoRoot)
			if err != nil {
				return fmt.Errorf("supersede: load config: %w", err)
			}
			res, err := identity.NewResolver(repoRoot, cfg).MustResolve(asFlag)
			if err != nil {
				return fmt.Errorf("supersede: resolve identity: %w", err)
			}
			actor := res.Handle

			db, err := openIndex(repoRoot)
			if err != nil {
				return fmt.Errorf("supersede: open index: %w", err)
			}
			defer db.Close()

			oldID, err := resolveDecisionID(db, args[0])
			if err != nil {
				return fmt.Errorf("supersede: resolve old: %w", err)
			}
			newID, err := resolveDecisionID(db, by)
			if err != nil {
				return fmt.Errorf("supersede: resolve new: %w", err)
			}

			if oldID == newID {
				return fmt.Errorf("supersede: a decision cannot supersede itself")
			}

			oldIdx, err := index.GetDecision(db, oldID)
			if err != nil {
				return fmt.Errorf("supersede: get old from index: %w", err)
			}
			if oldIdx == nil {
				return fmt.Errorf("supersede: old decision %s not found in index", oldID)
			}
			newIdx, err := index.GetDecision(db, newID)
			if err != nil {
				return fmt.Errorf("supersede: get new from index: %w", err)
			}
			if newIdx == nil {
				return fmt.Errorf("supersede: new decision %s not found in index", newID)
			}

			oldRev, err := index.GetDecisionRev(db, oldID)
			if err != nil {
				return fmt.Errorf("supersede: read old rev: %w", err)
			}
			newRev, err := index.GetDecisionRev(db, newID)
			if err != nil {
				return fmt.Errorf("supersede: read new rev: %w", err)
			}

			oldTreeDir := filepath.Join(repoRoot, ".decisions", oldIdx.Tree)
			oldPath := storage.DecisionPath(oldTreeDir, oldIdx.ID, oldIdx.Slug)
			oldD, err := storage.ReadDecision(oldPath)
			if err != nil {
				return fmt.Errorf("supersede: read old decision file: %w", err)
			}
			oldD.ID = oldIdx.ID
			oldD.Tree = oldIdx.Tree

			newTreeDir := filepath.Join(repoRoot, ".decisions", newIdx.Tree)
			newPath := storage.DecisionPath(newTreeDir, newIdx.ID, newIdx.Slug)
			newD, err := storage.ReadDecision(newPath)
			if err != nil {
				return fmt.Errorf("supersede: read new decision file: %w", err)
			}
			newD.ID = newIdx.ID
			newD.Tree = newIdx.Tree

			if oldD.Status == core.StatusSuperseded {
				return fmt.Errorf("supersede: decision %s is already superseded", oldID)
			}

			beforeOld := decisionToMap(oldD)

			// Mutate old: status + supersedes edge to new.
			oldD.Status = core.StatusSuperseded
			appendUniqueSupersede(oldD, newID)

			if err := validate.Decision(oldD); err != nil {
				return fmt.Errorf("supersede: validate old: %w", err)
			}
			if err := storage.WriteDecision(oldPath, oldD); err != nil {
				return fmt.Errorf("supersede: write old: %w", err)
			}
			oldSha, err := fsutil.Sha256File(oldPath)
			if err != nil {
				return fmt.Errorf("supersede: sha256 old: %w", err)
			}
			oldNewRev := concurrency.NewRev()
			if err := index.UpdateDecisionWithExpectedRev(db, oldD, oldSha, oldRev, oldNewRev); err != nil {
				if c, ok := concurrency.AsConflict(err); ok {
					return fmt.Errorf("supersede: rev conflict on old %s: expected %q but index now has %q",
						c.DecisionID, c.ExpectedRev, c.ActualRev)
				}
				return fmt.Errorf("supersede: update old index: %w", err)
			}

			// Mutate new: reciprocal supersedes edge.
			appendUniqueSupersede(newD, oldID)
			if err := validate.Decision(newD); err != nil {
				return fmt.Errorf("supersede: validate new: %w", err)
			}
			if err := storage.WriteDecision(newPath, newD); err != nil {
				return fmt.Errorf("supersede: write new: %w", err)
			}
			newSha, err := fsutil.Sha256File(newPath)
			if err != nil {
				return fmt.Errorf("supersede: sha256 new: %w", err)
			}
			newNewRev := concurrency.NewRev()
			if err := index.UpdateDecisionWithExpectedRev(db, newD, newSha, newRev, newNewRev); err != nil {
				if c, ok := concurrency.AsConflict(err); ok {
					return fmt.Errorf("supersede: rev conflict on new %s: expected %q but index now has %q",
						c.DecisionID, c.ExpectedRev, c.ActualRev)
				}
				return fmt.Errorf("supersede: update new index: %w", err)
			}

			// Single combined audit event keyed off the old decision.
			afterOld := decisionToMap(oldD)
			ev := core.Event{
				Actor:  actor,
				Action: core.ActionSupersede,
				Kind:   core.KindDecision,
				Tree:   oldD.Tree,
				ID:     oldD.ID,
				Payload: core.EventPayload{
					Before: beforeOld,
					After:  afterOld,
					Extra: map[string]any{
						"old": oldID,
						"new": newID,
					},
				},
			}
			if err := audit.Append(repoRoot, ev); err != nil {
				return fmt.Errorf("supersede: audit: %w", err)
			}

			oldD.Rev = oldNewRev
			return printDecision(cmd, oldD, outputFormat(cmd))
		},
	}

	cmd.Flags().StringVar(&by, "by", "", "ID (or prefix) of the new decision that supersedes the old one (required)")
	cmd.Flags().StringVar(&asFlag, "as", "", "Identity override (handle)")
	cmd.Flags().String("output", "", "Output format: human, json, yaml")
	return cmd
}

// appendUniqueSupersede appends a supersedes relationship pointing at target
// to d if not already present.
func appendUniqueSupersede(d *core.Decision, target string) {
	for _, r := range d.Relationships {
		if r.Type == core.RelSupersedes && r.Target == target {
			return
		}
	}
	d.Relationships = append(d.Relationships, core.Relationship{
		Type:   core.RelSupersedes,
		Target: target,
	})
}
