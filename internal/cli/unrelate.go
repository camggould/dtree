package cli

import (
	"fmt"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/concurrency"
	"github.com/cgould/dtree/internal/config"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/fsutil"
	"github.com/cgould/dtree/internal/identity"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/storage"
	"github.com/spf13/cobra"
)

// newUnrelateCommand returns the `dtree unrelate` command.
func newUnrelateCommand() *cobra.Command {
	var asFlag string

	cmd := &cobra.Command{
		Use:   "unrelate <src-id> <type> <target-id>",
		Short: "Remove a relationship between two decisions",
		Long: "Remove a directed relationship of the given type from src to target. " +
			"Errors when the relationship does not exist. " +
			"Use 'dtree supersede' to manage supersedes/superseded_by edges.",
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			rawSrc := args[0]
			relTypeStr := args[1]
			rawTarget := args[2]
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")

			if err := requireDecisionsDir(repoRoot); err != nil {
				return err
			}

			if supersedeReservedTypes[relTypeStr] {
				return fmt.Errorf(
					"relationships of type '%s' must be removed via 'dtree supersede'",
					relTypeStr,
				)
			}

			relType, ok := allowedRelateTypes[relTypeStr]
			if !ok {
				return fmt.Errorf(
					"invalid relationship type %q: must be one of blocks, influences, relates_to",
					relTypeStr,
				)
			}

			db, err := openIndex(repoRoot)
			if err != nil {
				return fmt.Errorf("unrelate: open index: %w", err)
			}
			defer db.Close()

			srcID, err := resolveDecisionIDPrefix(db, rawSrc)
			if err != nil {
				return fmt.Errorf("unrelate: resolve src: %w", err)
			}
			targetID, err := resolveDecisionIDPrefix(db, rawTarget)
			if err != nil {
				return fmt.Errorf("unrelate: resolve target: %w", err)
			}

			srcPath, err := decisionYAMLPath(repoRoot, db, srcID)
			if err != nil {
				return fmt.Errorf("unrelate: locate src file: %w", err)
			}
			d, err := storage.ReadDecision(srcPath)
			if err != nil {
				return fmt.Errorf("unrelate: read src decision: %w", err)
			}

			// Find and remove the matching relationship.
			found := false
			out := d.Relationships[:0]
			for _, r := range d.Relationships {
				if !found && r.Type == relType && r.Target == targetID {
					found = true
					continue
				}
				out = append(out, r)
			}
			if !found {
				return fmt.Errorf("relationship not found")
			}
			d.Relationships = out

			cfg, err := config.Load(repoRoot)
			if err != nil {
				return fmt.Errorf("unrelate: load config: %w", err)
			}
			res, err := identity.NewResolver(repoRoot, cfg).MustResolve(asFlag)
			if err != nil {
				return fmt.Errorf("unrelate: resolve identity: %w", err)
			}

			expectedRev, err := index.GetDecisionRev(db, srcID)
			if err != nil {
				return fmt.Errorf("unrelate: get rev: %w", err)
			}

			if err := storage.WriteDecision(srcPath, d); err != nil {
				return fmt.Errorf("unrelate: write src decision: %w", err)
			}

			contentSha, err := fsutil.Sha256File(srcPath)
			if err != nil {
				return fmt.Errorf("unrelate: sha256: %w", err)
			}
			newRev := concurrency.NewRev()
			if err := index.UpdateDecisionWithExpectedRev(db, d, contentSha, expectedRev, newRev); err != nil {
				return fmt.Errorf("unrelate: update index: %w", err)
			}

			meta := map[string]any{
				"src":    srcID,
				"type":   string(relType),
				"target": targetID,
			}
			ev := core.Event{
				Actor:  res.Handle,
				Action: core.ActionUnrelate,
				Kind:   core.KindRelationship,
				Tree:   d.Tree,
				ID:     srcID,
				Payload: core.EventPayload{
					Extra: meta,
				},
			}
			if err := audit.Append(repoRoot, ev); err != nil {
				return fmt.Errorf("unrelate: audit event: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Removed %s -[%s]-> %s\n",
				shortenID(srcID), relType, shortenID(targetID))
			return nil
		},
	}

	cmd.Flags().StringVar(&asFlag, "as", "", "Override identity handle for this invocation")
	return cmd
}
