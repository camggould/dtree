package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/config"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/identity"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/storage"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// newDeleteCommand returns the `dtree delete <id>` command.
//
// Soft delete (default) moves the decision file to
// .decisions/.deleted/<tree>/<original-filename> and removes the row from the
// index. Hard delete (--hard) unlinks the file outright. In both cases an
// audit event with action=delete is appended.
//
// When the decision is the target of incoming relationships:
//   - Soft delete warns but proceeds; refs survive on the moved file.
//   - Hard delete refuses unless --force; with --force the refs become dangling
//     and the audit event records them under payload.meta.dangling_refs.
func newDeleteCommand() *cobra.Command {
	var (
		hard   bool
		force  bool
		asFlag string
	)

	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a decision",
		Long: `Delete a decision by ID (ULID prefix accepted).

Soft delete (default) moves the file to .decisions/.deleted/<tree>/
preserving the original filename. Use --hard to unlink the file.

Hard delete refuses when other decisions reference this one; pass
--force to proceed (the references become dangling).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			idArg := args[0]
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")

			if err := requireDecisionsDir(repoRoot); err != nil {
				return err
			}

			cfg, err := config.Load(repoRoot)
			if err != nil {
				return fmt.Errorf("delete: load config: %w", err)
			}
			res, err := identity.NewResolver(repoRoot, cfg).MustResolve(asFlag)
			if err != nil {
				return fmt.Errorf("delete: resolve identity: %w", err)
			}
			actor := res.Handle

			db, err := openIndex(repoRoot)
			if err != nil {
				return fmt.Errorf("delete: open index: %w", err)
			}
			defer db.Close()

			// Resolve ULID (allow prefix).
			id, err := resolveDecisionID(db, idArg)
			if err != nil {
				return fmt.Errorf("delete: %w", err)
			}

			d, err := index.GetDecision(db, id)
			if err != nil {
				return fmt.Errorf("delete: get decision %s: %w", id, err)
			}
			if d == nil {
				return fmt.Errorf("delete: decision %s not found", id)
			}

			// Locate the on-disk file.
			treeDir := filepath.Join(repoRoot, ".decisions", d.Tree)
			filePath, err := findDecisionFile(treeDir, id)
			if err != nil {
				return fmt.Errorf("delete: %w", err)
			}

			// Capture full file contents for audit.before.
			diskDecision, err := storage.ReadDecision(filePath)
			if err != nil {
				return fmt.Errorf("delete: read decision file: %w", err)
			}
			beforePayload := decisionToMap(diskDecision)

			// Find incoming relationships.
			incoming, err := incomingRelationships(db, id)
			if err != nil {
				return fmt.Errorf("delete: query incoming refs: %w", err)
			}

			if hard && len(incoming) > 0 && !force {
				var lines []string
				for _, r := range incoming {
					lines = append(lines, fmt.Sprintf("  %s --[%s]--> %s", r.source, r.relType, id))
				}
				return fmt.Errorf(
					"delete: %d incoming reference(s) point at %s; pass --force to break them:\n%s",
					len(incoming), id, strings.Join(lines, "\n"),
				)
			}

			mode := "soft"
			if hard {
				mode = "hard"
			}

			// Perform the action.
			var movedTo string
			if hard {
				if err := index.DeleteDecision(db, id); err != nil {
					return fmt.Errorf("delete: index delete: %w", err)
				}
				if err := os.Remove(filePath); err != nil {
					return fmt.Errorf("delete: unlink %s: %w", filePath, err)
				}
			} else {
				movedTo, err = softDeleteMove(repoRoot, d.Tree, filePath)
				if err != nil {
					return fmt.Errorf("delete: %w", err)
				}
				if err := index.DeleteDecision(db, id); err != nil {
					return fmt.Errorf("delete: index delete: %w", err)
				}
			}

			// Build meta map for the audit payload.
			meta := map[string]any{"mode": mode}
			if movedTo != "" {
				// Record relative path so audit lines don't bake in absolute
				// paths from the host running the command.
				if rel, err := filepath.Rel(repoRoot, movedTo); err == nil {
					meta["moved_to"] = rel
				} else {
					meta["moved_to"] = movedTo
				}
			}
			if len(incoming) > 0 {
				dangling := make([]map[string]string, 0, len(incoming))
				for _, r := range incoming {
					dangling = append(dangling, map[string]string{
						"source": r.source,
						"type":   r.relType,
					})
				}
				meta["dangling_refs"] = dangling
			}

			ev := core.Event{
				Actor:  actor,
				Action: core.ActionDelete,
				Kind:   core.KindDecision,
				Tree:   d.Tree,
				ID:     id,
				Payload: core.EventPayload{
					Before: beforePayload,
					After:  nil,
					Extra:  map[string]any{"meta": meta},
				},
			}
			if err := audit.Append(repoRoot, ev); err != nil {
				return fmt.Errorf("delete: audit event: %w", err)
			}

			return printDeleteResult(cmd, id, d.Tree, mode, meta["moved_to"])
		},
	}

	cmd.Flags().BoolVar(&hard, "hard", false, "Permanently unlink the decision file")
	cmd.Flags().BoolVar(&force, "force", false, "Skip safety prompts / break incoming references")
	cmd.Flags().StringVar(&asFlag, "as", "", "Override identity handle for this invocation")
	cmd.Flags().String("output", "", "Output format: human, json, yaml")
	return cmd
}

// incomingRef describes one incoming relationship row.
type incomingRef struct {
	source  string
	relType string
}

// incomingRelationships returns all relationships pointing at targetID.
func incomingRelationships(db *index.DB, targetID string) ([]incomingRef, error) {
	rows, err := db.Conn().Query(
		`SELECT source, type FROM relationships WHERE target = ? ORDER BY source, type`,
		targetID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []incomingRef
	for rows.Next() {
		var r incomingRef
		if err := rows.Scan(&r.source, &r.relType); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// resolveDecisionID accepts a full ULID or an unambiguous prefix and returns
// the canonical 26-char ID. Returns an error if the prefix matches zero or
// more than one decision.
func resolveDecisionID(db *index.DB, idOrPrefix string) (string, error) {
	idOrPrefix = strings.TrimSpace(idOrPrefix)
	if idOrPrefix == "" {
		return "", fmt.Errorf("decision id is required")
	}
	// Exact match first.
	if len(idOrPrefix) == 26 {
		var id string
		err := db.Conn().QueryRow(
			`SELECT id FROM decisions WHERE id = ?`, idOrPrefix,
		).Scan(&id)
		if err == nil {
			return id, nil
		}
		// Fall through to prefix search if not found.
	}

	// Prefix search (ULIDs are uppercase Crockford).
	pattern := strings.ToUpper(idOrPrefix) + "%"
	rows, err := db.Conn().Query(
		`SELECT id FROM decisions WHERE id LIKE ? LIMIT 2`, pattern,
	)
	if err != nil {
		return "", fmt.Errorf("resolve id: %w", err)
	}
	defer rows.Close()
	var matches []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		matches = append(matches, id)
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("decision %q not found", idOrPrefix)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("decision id %q is ambiguous (matches multiple)", idOrPrefix)
	}
}

// findDecisionFile returns the absolute path to the YAML file for id under
// treeDir. Walks <treeDir>/decisions/ looking for a file whose name begins
// with id. Returns an error if no file is found.
func findDecisionFile(treeDir, id string) (string, error) {
	dir := filepath.Join(treeDir, "decisions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read decisions dir %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, id+"-") || name == id+".yaml" {
			return filepath.Join(dir, name), nil
		}
	}
	return "", fmt.Errorf("no decision file for %s under %s", id, dir)
}

// softDeleteMove moves filePath to .decisions/.deleted/<tree>/, returning the
// destination path. If a file with the same basename exists, appends -N before
// the .yaml extension.
func softDeleteMove(repoRoot, tree, filePath string) (string, error) {
	destDir := filepath.Join(repoRoot, ".decisions", ".deleted", tree)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", destDir, err)
	}
	base := filepath.Base(filePath)
	dest := uniquePath(filepath.Join(destDir, base))
	if err := os.Rename(filePath, dest); err != nil {
		return "", fmt.Errorf("rename %s -> %s: %w", filePath, dest, err)
	}
	return dest, nil
}

// uniquePath returns path unchanged if no file exists at it; otherwise it
// inserts -1, -2, ... before the extension until it finds an open slot.
func uniquePath(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 1; ; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s-%d%s", stem, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

// printDeleteResult prints confirmation in the requested format.
func printDeleteResult(cmd *cobra.Command, id, tree, mode string, movedToAny any) error {
	movedTo, _ := movedToAny.(string)

	type result struct {
		ID      string `json:"id" yaml:"id"`
		Tree    string `json:"tree" yaml:"tree"`
		Mode    string `json:"mode" yaml:"mode"`
		MovedTo string `json:"moved_to,omitempty" yaml:"moved_to,omitempty"`
	}
	r := result{ID: id, Tree: tree, Mode: mode, MovedTo: movedTo}

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
		if mode == "soft" {
			fmt.Fprintf(cmd.OutOrStdout(), "Soft-deleted %s (moved to %s)\n", short, movedTo)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "Hard-deleted %s\n", short)
		}
		return nil
	}
}
