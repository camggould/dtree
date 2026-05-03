package cli

import (
	"bufio"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cgould/dtree/internal/config"
	"github.com/cgould/dtree/internal/identity"
	"github.com/cgould/dtree/internal/index"
	dtreesync "github.com/cgould/dtree/internal/sync"
	"github.com/spf13/cobra"
)

// newSyncCommand returns the `dtree sync` command.
func newSyncCommand() *cobra.Command {
	var (
		yes        bool
		allRecord  bool
		allRevert  bool
	)

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Detect and reconcile external edits to decision files",
		Long:  "Scan for files changed outside the dtree CLI and prompt to record or revert each change.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")

			// Resolve actor handle for audit events.
			cfg, err := config.Load(repoRoot)
			if err != nil {
				return err
			}
			res, err := identity.NewResolver(repoRoot, cfg).Resolve("")
			if err != nil {
				return fmt.Errorf("sync: resolve identity: %w", err)
			}
			actorHandle := res.Handle
			if actorHandle == "" {
				actorHandle = "unknown"
			}

			indexPath := filepath.Join(repoRoot, ".decisions", ".index.db")
			db, err := index.Open(indexPath)
			if err != nil {
				return fmt.Errorf("sync: open index: %w", err)
			}
			defer db.Close()

			mismatches, err := dtreesync.Scan(repoRoot, db)
			if err != nil {
				return fmt.Errorf("sync: scan: %w", err)
			}

			if len(mismatches) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No mismatches found. Index is in sync.")
				return nil
			}

			var reconciled, aborted, errored int
			reader := bufio.NewReader(cmd.InOrStdin())

			for _, m := range mismatches {
				action, err := chooseSyncAction(cmd, reader, m, yes, allRecord, allRevert)
				if err != nil {
					return err
				}

				if action == dtreesync.ActionAbort {
					aborted++
					continue
				}

				if err := dtreesync.Reconcile(repoRoot, db, m, action, actorHandle); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "  error reconciling %s: %v\n", m.DecisionID, err)
					errored++
					continue
				}
				reconciled++
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "\nSync complete: %d reconciled, %d skipped, %d errored.\n",
				reconciled, aborted, errored)
			return nil
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "Automatically record all external edits without prompting")
	cmd.Flags().BoolVar(&allRecord, "all-record", false, "Record all mismatches as external edits (same as --yes)")
	cmd.Flags().BoolVar(&allRevert, "all-revert", false, "Revert all mismatches to index state without prompting")
	return cmd
}

// chooseSyncAction determines the action to take for a single mismatch.
func chooseSyncAction(
	cmd *cobra.Command,
	reader *bufio.Reader,
	m dtreesync.Mismatch,
	yes, allRecord, allRevert bool,
) (dtreesync.Action, error) {
	if yes || allRecord {
		return dtreesync.ActionRecord, nil
	}
	if allRevert {
		return dtreesync.ActionRevert, nil
	}

	// Print mismatch summary.
	kindStr := mismatchKindString(m.Kind)
	fmt.Fprintf(cmd.OutOrStdout(), "\n[%s] %s\n  Path: %s\n",
		kindStr, m.DecisionID, m.Path)
	fmt.Fprint(cmd.OutOrStdout(), "  Action? [r]ecord / [d] revert / [a]bort / [s]kip: ")

	line, err := reader.ReadString('\n')
	if err != nil {
		return dtreesync.ActionAbort, nil
	}
	line = strings.TrimSpace(strings.ToLower(line))

	switch line {
	case "r", "record":
		return dtreesync.ActionRecord, nil
	case "d", "revert":
		return dtreesync.ActionRevert, nil
	case "a", "abort":
		return dtreesync.ActionAbort, nil
	default:
		// Skip: treat as abort (no-op for this mismatch).
		return dtreesync.ActionAbort, nil
	}
}

// mismatchKindString returns a human-readable string for a MismatchKind.
func mismatchKindString(k dtreesync.MismatchKind) string {
	switch k {
	case dtreesync.MismatchEdit:
		return "external_edit"
	case dtreesync.MismatchCreate:
		return "external_create"
	case dtreesync.MismatchDelete:
		return "external_delete"
	default:
		return "unknown"
	}
}
