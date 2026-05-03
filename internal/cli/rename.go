// Package cli — `dtree rename` changes the slug portion of a decision file.
//
// The ULID stays the canonical id; only the human-readable slug in the
// filename and the index `slug` column move. With no positional slug the
// new slug is derived from the current summary.
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/concurrency"
	"github.com/cgould/dtree/internal/config"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/fsutil"
	"github.com/cgould/dtree/internal/identity"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/storage"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// newRenameCommand returns the `dtree rename <id> [<slug>]` command.
func newRenameCommand() *cobra.Command {
	var asFlag string

	cmd := &cobra.Command{
		Use:   "rename <id> [<slug>]",
		Short: "Rename a decision's slug (the human-readable part of its filename)",
		Long: `Rename the slug portion of a decision's on-disk filename.

The decision's ULID is unchanged. With no slug argument the new slug is
derived from the decision's current summary via the standard slug rules.

Refuses to overwrite an existing target file.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			if err := requireDecisionsDir(repoRoot); err != nil {
				return fmt.Errorf("%w; run `dtree init`", err)
			}

			cfg, err := config.Load(repoRoot)
			if err != nil {
				return fmt.Errorf("rename: load config: %w", err)
			}
			res, err := identity.NewResolver(repoRoot, cfg).MustResolve(asFlag)
			if err != nil {
				return fmt.Errorf("rename: resolve identity: %w", err)
			}
			actor := res.Handle

			db, err := openIndex(repoRoot)
			if err != nil {
				return fmt.Errorf("rename: open index: %w", err)
			}
			defer db.Close()

			id, err := resolveDecisionID(db, args[0])
			if err != nil {
				return fmt.Errorf("rename: %w", err)
			}

			indexed, err := index.GetDecision(db, id)
			if err != nil {
				return fmt.Errorf("rename: get from index: %w", err)
			}
			if indexed == nil {
				return fmt.Errorf("rename: decision %s not found in index", id)
			}

			treeDir := filepath.Join(repoRoot, ".decisions", indexed.Tree)
			oldPath, err := findDecisionFile(treeDir, id)
			if err != nil {
				return fmt.Errorf("rename: %w", err)
			}

			// Determine target slug.
			var newSlug string
			if len(args) == 2 {
				newSlug = args[1]
			} else {
				newSlug = storage.SlugFromSummary(indexed.Summary)
			}
			if newSlug == "" {
				return fmt.Errorf("rename: new slug must not be empty")
			}

			newPath := storage.DecisionPath(treeDir, id, newSlug)
			if newPath == oldPath {
				fmt.Fprintln(cmd.OutOrStdout(), "no changes")
				return nil
			}

			// Refuse if target already exists.
			if _, err := os.Stat(newPath); err == nil {
				return fmt.Errorf("rename: target already exists: %s", newPath)
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("rename: stat target: %w", err)
			}

			oldRev, err := index.GetDecisionRev(db, id)
			if err != nil {
				return fmt.Errorf("rename: read rev: %w", err)
			}

			// Move file.
			if err := os.Rename(oldPath, newPath); err != nil {
				return fmt.Errorf("rename: %s -> %s: %w", oldPath, newPath, err)
			}

			// Reload, update slug field on disk so file content matches filename.
			d, err := storage.ReadDecision(newPath)
			if err != nil {
				// Best-effort rollback before surfacing the error.
				_ = os.Rename(newPath, oldPath)
				return fmt.Errorf("rename: reload decision: %w", err)
			}
			oldSlug := d.Slug
			d.ID = id
			d.Tree = indexed.Tree
			d.Slug = newSlug
			if err := storage.WriteDecision(newPath, d); err != nil {
				return fmt.Errorf("rename: rewrite decision: %w", err)
			}

			// Refresh index slug + content sha + rev under optimistic concurrency.
			contentSha, err := fsutil.Sha256File(newPath)
			if err != nil {
				return fmt.Errorf("rename: sha256: %w", err)
			}
			newRev := concurrency.NewRev()
			if err := index.UpdateDecisionWithExpectedRev(db, d, contentSha, oldRev, newRev); err != nil {
				if c, ok := concurrency.AsConflict(err); ok {
					return fmt.Errorf("rename: rev conflict on %s: expected %q but index now has %q",
						c.DecisionID, c.ExpectedRev, c.ActualRev)
				}
				return fmt.Errorf("rename: update index: %w", err)
			}

			// Audit event. Use ActionRename with meta carrying old/new paths and
			// slugs (relative to repoRoot for portability).
			meta := map[string]any{
				"old_slug": oldSlug,
				"new_slug": newSlug,
			}
			if rel, err := filepath.Rel(repoRoot, oldPath); err == nil {
				meta["old_path"] = rel
			} else {
				meta["old_path"] = oldPath
			}
			if rel, err := filepath.Rel(repoRoot, newPath); err == nil {
				meta["new_path"] = rel
			} else {
				meta["new_path"] = newPath
			}

			ev := core.Event{
				Actor:  actor,
				Action: core.ActionRename,
				Kind:   core.KindDecision,
				Tree:   indexed.Tree,
				ID:     id,
				Payload: core.EventPayload{
					Before: map[string]any{"slug": oldSlug},
					After:  map[string]any{"slug": newSlug},
					Extra:  map[string]any{"meta": meta},
				},
			}
			if err := audit.Append(repoRoot, ev); err != nil {
				return fmt.Errorf("rename: audit event: %w", err)
			}

			return printRenameResult(cmd, id, indexed.Tree, oldSlug, newSlug)
		},
	}

	cmd.Flags().StringVar(&asFlag, "as", "", "Override identity handle for this invocation")
	cmd.Flags().String("output", "", "Output format: human, json, yaml")
	return cmd
}

// printRenameResult emits a confirmation in the requested format.
func printRenameResult(cmd *cobra.Command, id, tree, oldSlug, newSlug string) error {
	type result struct {
		ID      string `json:"id" yaml:"id"`
		Tree    string `json:"tree" yaml:"tree"`
		OldSlug string `json:"old_slug" yaml:"old_slug"`
		NewSlug string `json:"new_slug" yaml:"new_slug"`
	}
	r := result{ID: id, Tree: tree, OldSlug: oldSlug, NewSlug: newSlug}

	switch outputFormat(cmd) {
	case "json":
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	case "yaml":
		enc := yaml.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent(2)
		if err := enc.Encode(r); err != nil {
			return err
		}
		return enc.Close()
	default:
		short := id
		if len(short) > 8 {
			short = short[:8]
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Renamed %s slug %s -> %s\n", short, oldSlug, newSlug)
		return nil
	}
}
