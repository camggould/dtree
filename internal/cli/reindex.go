package cli

import (
	"fmt"
	"path/filepath"

	"github.com/cgould/dtree/internal/index"
	"github.com/spf13/cobra"
)

// newReindexCommand returns the `dtree reindex` command.
func newReindexCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reindex",
		Short: "Rebuild the SQLite index from on-disk YAML files",
		Long:  "Scan all decision YAML files and audit events and rebuild the SQLite index from scratch.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")

			indexPath := filepath.Join(repoRoot, ".decisions", ".index.db")
			db, err := index.Open(indexPath)
			if err != nil {
				return fmt.Errorf("reindex: open index: %w", err)
			}
			defer db.Close()

			progress := func(msg string) {
				fmt.Fprintf(cmd.ErrOrStderr(), "  %s\n", msg)
			}

			report, err := index.Reindex(repoRoot, db, progress)
			if err != nil {
				return fmt.Errorf("reindex: %w", err)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintln(out, "Reindex complete.")
			fmt.Fprintf(out, "  Trees:         %d\n", report.Trees)
			fmt.Fprintf(out, "  Actors:        %d\n", report.Actors)
			fmt.Fprintf(out, "  Decisions:     %d\n", report.Decisions)
			fmt.Fprintf(out, "  Events:        %d\n", report.Events)
			fmt.Fprintf(out, "  Relationships: %d\n", report.Relationships)
			if len(report.Warnings) > 0 {
				fmt.Fprintf(out, "  Warnings (%d):\n", len(report.Warnings))
				for _, w := range report.Warnings {
					fmt.Fprintf(out, "    - %s\n", w)
				}
			}
			fmt.Fprintf(out, "  Duration: %s\n", report.Ended.Sub(report.Started).Round(1000000))
			return nil
		},
	}
	return cmd
}
