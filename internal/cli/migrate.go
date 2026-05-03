package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/migrations"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// migrateStep is one step in the migration plan, used for structured output.
type migrateStep struct {
	From int    `json:"from" yaml:"from"`
	To   int    `json:"to"   yaml:"to"`
	Name string `json:"name" yaml:"name"`
}

// migrateResult is the full structured output for the migrate command.
type migrateResult struct {
	Current int           `json:"current"  yaml:"current"`
	Target  int           `json:"target"   yaml:"target"`
	Plan    []migrateStep `json:"plan"     yaml:"plan"`
	Applied []migrateStep `json:"applied"  yaml:"applied"`
}

// newMigrateCommand returns the `dtree migrate` command.
func newMigrateCommand() *cobra.Command {
	var (
		dryRun   bool
		targetN  int
		targetSet bool
	)

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply schema migrations to the index database",
		Long:  "Plan and apply schema migrations to bring the .decisions/.index.db up to the current schema version.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			outputFlag, _ := cmd.Root().PersistentFlags().GetString("output")

			// Honour local --output flag if set (not present on migrate, so use global only).
			format := outputFlag
			if format == "" {
				if isTTY() {
					format = "human"
				} else {
					format = "json"
				}
			}

			dbPath := filepath.Join(repoRoot, ".decisions", ".index.db")
			db, err := index.Open(dbPath)
			if err != nil {
				return fmt.Errorf("migrate: open index: %w", err)
			}
			defer db.Close()

			current, err := db.SchemaVersion()
			if err != nil {
				return fmt.Errorf("migrate: read schema version: %w", err)
			}

			target := index.CurrentSchemaVersion
			if targetSet {
				target = targetN
			}

			reg := migrations.Default()

			plan, err := reg.Plan(current, target)
			if err != nil {
				return fmt.Errorf("migrate: %w", err)
			}

			planSteps := migrationsToSteps(plan)

			if len(plan) == 0 {
				// Nothing to do.
				result := migrateResult{
					Current: current,
					Target:  target,
					Plan:    []migrateStep{},
					Applied: []migrateStep{},
				}
				return writeMigrateOutput(cmd, format, result, func() {
					fmt.Fprintf(cmd.OutOrStdout(), "Already at v%d; nothing to migrate.\n", current)
				})
			}

			// Print plan.
			if format == "human" {
				fmt.Fprintf(cmd.OutOrStdout(), "Will apply %d migration(s):\n", len(plan))
				for _, s := range planSteps {
					fmt.Fprintf(cmd.OutOrStdout(), "  v%d → v%d: %s\n", s.From, s.To, s.Name)
				}
			}

			if dryRun {
				result := migrateResult{
					Current: current,
					Target:  target,
					Plan:    planSteps,
					Applied: []migrateStep{},
				}
				return writeMigrateOutput(cmd, format, result, func() {
					// human output was already printed above
				})
			}

			// Apply migrations.
			applied, err := reg.Apply(db, target, false)
			if err != nil {
				return fmt.Errorf("migrate: %w", err)
			}

			appliedSteps := migrationsToSteps(applied)

			result := migrateResult{
				Current: current,
				Target:  target,
				Plan:    planSteps,
				Applied: appliedSteps,
			}

			return writeMigrateOutput(cmd, format, result, func() {
				fmt.Fprintf(cmd.OutOrStdout(), "Applied %d migration(s).\n", len(applied))
			})
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the migration plan without applying it")
	cmd.Flags().IntVar(&targetN, "target", 0, "Target schema version (default: current binary schema version)")
	// Track whether --target was explicitly set.
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		targetSet = cmd.Flags().Changed("target")
		return nil
	}

	return cmd
}

// migrationsToSteps converts a []migrations.Migration slice to []migrateStep.
func migrationsToSteps(ms []migrations.Migration) []migrateStep {
	if ms == nil {
		return []migrateStep{}
	}
	out := make([]migrateStep, len(ms))
	for i, m := range ms {
		out[i] = migrateStep{From: m.From, To: m.To, Name: m.Name}
	}
	return out
}

// writeMigrateOutput writes the result in the appropriate format.
// humanFn is called only for the "human" format (for the summary line).
func writeMigrateOutput(cmd *cobra.Command, format string, result migrateResult, humanFn func()) error {
	switch format {
	case "json":
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	case "yaml":
		enc := yaml.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent(2)
		if err := enc.Encode(result); err != nil {
			return err
		}
		return enc.Close()
	default: // human
		humanFn()
		return nil
	}
}
