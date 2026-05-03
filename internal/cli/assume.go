// Package cli — `dtree assume` is a one-shot create+decide for assumptions.
//
// An "assumption" is a recorded premise: a decision whose status is decided
// at creation time and whose priority is `assumption`. The command emits a
// single `create` audit event with status=decided in `after` (rather than
// emitting create + decide separately) to keep replay simple — replaying a
// single event is enough to reconstruct the final state.
package cli

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/config"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/fsutil"
	"github.com/cgould/dtree/internal/identity"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/storage"
	"github.com/cgould/dtree/internal/ulid"
	"github.com/cgould/dtree/internal/validate"
	"github.com/spf13/cobra"
)

// newAssumeCommand returns the `dtree assume` command.
func newAssumeCommand() *cobra.Command {
	var (
		choice      string
		reason      string
		by          []string
		treeFlag    string
		description string
		tags        []string
		asFlag      string
	)

	cmd := &cobra.Command{
		Use:   "assume <summary>",
		Short: "Record an assumption (decided-on-creation, priority=assumption)",
		Long:  "Create a decision in the `decided` state with priority=assumption. Useful for recording premises.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			if err := requireDecisionsDir(repoRoot); err != nil {
				return fmt.Errorf("%w; run `dtree init`", err)
			}

			summary := strings.TrimSpace(args[0])
			if summary == "" {
				return fmt.Errorf("summary is required")
			}
			if strings.TrimSpace(choice) == "" {
				return fmt.Errorf("--choice is required")
			}
			if strings.TrimSpace(reason) == "" {
				return fmt.Errorf("--reason is required")
			}

			cfg, err := config.Load(repoRoot)
			if err != nil {
				return fmt.Errorf("assume: load config: %w", err)
			}
			resolver := identity.NewResolver(repoRoot, cfg)
			res, err := resolver.MustResolve(asFlag)
			if err != nil {
				return fmt.Errorf("assume: resolve identity: %w", err)
			}
			actor := res.Handle

			// Default --by to the current actor when unset.
			if len(by) == 0 {
				by = []string{actor}
			}
			for _, h := range by {
				h = strings.TrimSpace(h)
				if h == "" {
					return fmt.Errorf("--by handle must not be empty")
				}
				a, err := resolver.FindActor(h)
				if err != nil {
					return fmt.Errorf("assume: lookup actor %q: %w", h, err)
				}
				if a == nil {
					return fmt.Errorf("assume: unknown actor %q (run `dtree actor add %s`)", h, h)
				}
			}

			// Resolve tree.
			treeSlug, err := resolveNewTree(repoRoot, treeFlag, cfg)
			if err != nil {
				return fmt.Errorf("assume: %w", err)
			}
			treeDir := filepath.Join(repoRoot, ".decisions", treeSlug)

			// Build the decision in its decided state.
			d := &core.Decision{
				ID:                 ulid.New(),
				Tree:               treeSlug,
				Summary:            summary,
				Description:        description,
				Priority:           core.PriorityAssumption,
				Status:             core.StatusDecided,
				Creator:            actor,
				Tags:               tags,
				ActualChoice:       choice,
				ActualChoiceReason: reason,
				DecidedBy:          append([]string(nil), by...),
				SchemaVersion:      core.SchemaVersion,
			}
			d.Slug = storage.SlugFromSummary(d.Summary)

			if err := validate.Decision(d); err != nil {
				return fmt.Errorf("assume: validation: %w", err)
			}

			// Write file atomically.
			path := storage.DecisionPath(treeDir, d.ID, d.Slug)
			if err := storage.WriteDecision(path, d); err != nil {
				return fmt.Errorf("assume: write decision: %w", err)
			}

			contentSha, err := fsutil.Sha256File(path)
			if err != nil {
				return fmt.Errorf("assume: sha256: %w", err)
			}

			db, err := openIndex(repoRoot)
			if err != nil {
				return fmt.Errorf("assume: open index: %w", err)
			}
			defer db.Close()

			if err := index.InsertDecision(db, d, contentSha); err != nil {
				return fmt.Errorf("assume: insert index: %w", err)
			}

			// Single create event with the fully-decided after-state.
			ev := core.Event{
				Actor:  actor,
				Action: core.ActionCreate,
				Kind:   core.KindDecision,
				Tree:   d.Tree,
				ID:     d.ID,
				Payload: core.EventPayload{
					After: decisionToMap(d),
				},
			}
			if err := audit.Append(repoRoot, ev); err != nil {
				return fmt.Errorf("assume: audit: %w", err)
			}

			format := outputFormat(cmd)
			return printDecision(cmd, d, format)
		},
	}

	cmd.Flags().StringVar(&choice, "choice", "", "The assumed choice (required)")
	cmd.Flags().StringVar(&reason, "reason", "", "Reason for the assumption (required)")
	cmd.Flags().StringArrayVar(&by, "by", nil, "Decider handle (repeatable, defaults to current actor)")
	cmd.Flags().StringVar(&treeFlag, "tree", "", "Tree slug (defaults to default tree)")
	cmd.Flags().StringVar(&description, "description", "", "Long-form description")
	cmd.Flags().StringSliceVar(&tags, "tags", nil, "Tags (comma-separated)")
	cmd.Flags().StringVar(&asFlag, "as", "", "Identity override (handle)")
	cmd.Flags().String("output", "", "Output format: human, json, yaml")

	return cmd
}
