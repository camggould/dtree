// Package cli — `dtree edit` mutates an existing decision.
//
// Two modes:
//   - --field key=value (repeatable, non-interactive): apply the listed
//     mutations directly. Skips the editor.
//   - default: open the decision YAML in $EDITOR (or --editor, or config),
//     reload after exit, strip comment lines, and apply the result.
//
// In either mode the result is validated, written atomically, the index is
// updated under optimistic concurrency, and an `update` audit event is
// appended carrying before/after snapshots.
package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
	"gopkg.in/yaml.v3"
)

// newEditCommand returns the `dtree edit <id>` command.
func newEditCommand() *cobra.Command {
	var (
		editorFlag string
		asFlag     string
		fields     []string
	)

	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Edit an existing decision",
		Long: `Edit an existing decision by ID (ULID prefix or summary substring).

By default opens the decision YAML in $EDITOR. Pass one or more
--field key=value flags to apply edits non-interactively without
launching the editor.

Allowed --field keys: summary, description, priority, tags
(comma-separated), assignee, recommended_summary, recommended_full,
recommended_by.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			if err := requireDecisionsDir(repoRoot); err != nil {
				return fmt.Errorf("%w; run `dtree init`", err)
			}

			cfg, err := config.Load(repoRoot)
			if err != nil {
				return fmt.Errorf("edit: load config: %w", err)
			}
			res, err := identity.NewResolver(repoRoot, cfg).MustResolve(asFlag)
			if err != nil {
				return fmt.Errorf("edit: resolve identity: %w", err)
			}
			actor := res.Handle

			db, err := openIndex(repoRoot)
			if err != nil {
				return fmt.Errorf("edit: open index: %w", err)
			}
			defer db.Close()

			id, err := resolveDecisionID(db, args[0])
			if err != nil {
				return fmt.Errorf("edit: %w", err)
			}

			indexed, err := index.GetDecision(db, id)
			if err != nil {
				return fmt.Errorf("edit: get from index: %w", err)
			}
			if indexed == nil {
				return fmt.Errorf("edit: decision %s not found in index", id)
			}
			oldRev, err := index.GetDecisionRev(db, id)
			if err != nil {
				return fmt.Errorf("edit: read rev: %w", err)
			}

			treeDir := filepath.Join(repoRoot, ".decisions", indexed.Tree)
			path := storage.DecisionPath(treeDir, indexed.ID, indexed.Slug)
			d, err := storage.ReadDecision(path)
			if err != nil {
				return fmt.Errorf("edit: read decision file: %w", err)
			}
			d.ID = indexed.ID
			d.Tree = indexed.Tree

			before := decisionToMap(d)

			// Build the mutated decision either via --field or via the editor.
			var updated *core.Decision
			if len(fields) > 0 {
				updated, err = applyFieldEdits(d, fields)
				if err != nil {
					return fmt.Errorf("edit: %w", err)
				}
			} else {
				editorPath := resolveEditor(editorFlag, cfg)
				updated, err = editDecisionFile(cmd, editorPath, path, d)
				if err != nil {
					return fmt.Errorf("edit: %w", err)
				}
			}

			// Preserve identity-bearing/derived fields the editor must not change.
			updated.ID = d.ID
			updated.Tree = d.Tree
			updated.Status = d.Status
			updated.Creator = d.Creator
			updated.Slug = d.Slug
			updated.SchemaVersion = d.SchemaVersion
			if updated.SchemaVersion == 0 {
				updated.SchemaVersion = core.SchemaVersion
			}

			// Validate.
			if err := validate.Decision(updated); err != nil {
				return fmt.Errorf("edit: validation: %w", err)
			}

			// No-op detection: compare via the audit map representation so we
			// catch mutations on slice/scalar fields uniformly.
			after := decisionToMap(updated)
			if reflect.DeepEqual(before, after) {
				fmt.Fprintln(cmd.OutOrStdout(), "no changes")
				return nil
			}

			// Write file atomically.
			if err := storage.WriteDecision(path, updated); err != nil {
				return fmt.Errorf("edit: write decision: %w", err)
			}

			// Refresh index with optimistic-concurrency check.
			contentSha, err := fsutil.Sha256File(path)
			if err != nil {
				return fmt.Errorf("edit: sha256: %w", err)
			}
			newRev := concurrency.NewRev()
			if err := index.UpdateDecisionWithExpectedRev(db, updated, contentSha, oldRev, newRev); err != nil {
				if c, ok := concurrency.AsConflict(err); ok {
					return fmt.Errorf("edit: rev conflict on %s: expected %q but index now has %q (someone else updated it; re-run after `dtree show %s`)",
						c.DecisionID, c.ExpectedRev, c.ActualRev, id)
				}
				return fmt.Errorf("edit: update index: %w", err)
			}

			// Audit event.
			ev := core.Event{
				Actor:  actor,
				Action: core.ActionUpdate,
				Kind:   core.KindDecision,
				Tree:   updated.Tree,
				ID:     updated.ID,
				Payload: core.EventPayload{
					Before: before,
					After:  after,
				},
			}
			if err := audit.Append(repoRoot, ev); err != nil {
				return fmt.Errorf("edit: audit event: %w", err)
			}

			updated.Rev = newRev
			format := outputFormat(cmd)
			return printDecision(cmd, updated, format)
		},
	}

	cmd.Flags().StringVar(&editorFlag, "editor", "", "Editor binary to use (overrides $EDITOR)")
	cmd.Flags().StringVar(&asFlag, "as", "", "Override identity handle for this invocation")
	cmd.Flags().StringArrayVar(&fields, "field", nil, "Set field non-interactively as key=value (repeatable)")
	cmd.Flags().String("output", "", "Output format: human, json, yaml")

	return cmd
}

// applyFieldEdits returns a copy of d with each "key=value" pair from fields
// applied. Unknown keys produce an error.
func applyFieldEdits(d *core.Decision, fields []string) (*core.Decision, error) {
	out := *d // shallow copy; slices reassigned below as needed
	// Defensive copy of tags so callers' before snapshot is untouched.
	out.Tags = append([]string(nil), d.Tags...)

	for _, raw := range fields {
		k, v, ok := strings.Cut(raw, "=")
		if !ok {
			return nil, fmt.Errorf("--field %q must be key=value", raw)
		}
		k = strings.TrimSpace(k)
		switch k {
		case "summary":
			out.Summary = v
		case "description":
			out.Description = v
		case "priority":
			out.Priority = core.Priority(v)
		case "tags":
			out.Tags = parseTags(v)
		case "assignee":
			out.Assignee = v
		case "recommended_summary":
			out.RecommendedSummary = v
		case "recommended_full":
			out.RecommendedFull = v
		case "recommended_by":
			out.RecommendedBy = v
		default:
			return nil, fmt.Errorf("unknown --field key %q (allowed: summary, description, priority, tags, assignee, recommended_summary, recommended_full, recommended_by)", k)
		}
	}
	return &out, nil
}

// parseTags splits a comma-separated tags string, trimming whitespace and
// dropping empty entries. An empty input yields a nil slice (clear the tags).
func parseTags(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// editDecisionFile writes d to a temp file, opens it in editorPath, and
// reloads the result after the editor exits. Comment lines (leading '#') are
// stripped before unmarshal so users can leave guidance in the file.
func editDecisionFile(cmd *cobra.Command, editorPath, originalPath string, d *core.Decision) (*core.Decision, error) {
	tmpDir, err := os.MkdirTemp("", "dtree-edit-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpFile := filepath.Join(tmpDir, filepath.Base(originalPath))

	body, err := yaml.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("marshal decision: %w", err)
	}
	header := []byte("# Lines starting with '#' are ignored.\n# Save and exit to apply edits; exit without changes to abort.\n")
	if err := os.WriteFile(tmpFile, append(header, body...), 0o600); err != nil {
		return nil, fmt.Errorf("write temp file: %w", err)
	}

	editorCmd := exec.Command(editorPath, tmpFile) //nolint:gosec
	editorCmd.Stdin = cmd.InOrStdin()
	editorCmd.Stdout = cmd.OutOrStdout()
	editorCmd.Stderr = cmd.ErrOrStderr()
	if err := editorCmd.Run(); err != nil {
		return nil, fmt.Errorf("editor exited with error: %w", err)
	}

	raw, err := os.ReadFile(tmpFile)
	if err != nil {
		return nil, fmt.Errorf("read temp file: %w", err)
	}
	stripped := stripCommentLines(raw)

	var updated core.Decision
	if err := yaml.Unmarshal(stripped, &updated); err != nil {
		return nil, fmt.Errorf("parse decision yaml: %w", err)
	}
	return &updated, nil
}
