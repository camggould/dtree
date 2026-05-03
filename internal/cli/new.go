package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	"gopkg.in/yaml.v3"
)

// newNewCommand returns the `dtree new` command.
func newNewCommand() *cobra.Command {
	var (
		description        string
		priority           string
		tags               []string
		assignee           string
		treeFlag           string
		recommendedSummary string
		recommendedFull    string
		recommendedBy      string
		fromFile           string
		fromStdin          bool
		noEdit             bool
		editorFlag         string
	)

	cmd := &cobra.Command{
		Use:   "new [summary]",
		Short: "Create a new decision",
		Long:  "Create a new decision interactively or via flags.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")

			// Step 1: Check .decisions/ exists.
			if err := requireDecisionsDir(repoRoot); err != nil {
				return fmt.Errorf("%w; run `dtree init`", err)
			}

			// Load config and resolve identity.
			cfg, err := config.Load(repoRoot)
			if err != nil {
				return fmt.Errorf("new: load config: %w", err)
			}
			res, err := identity.NewResolver(repoRoot, cfg).MustResolve("")
			if err != nil {
				return fmt.Errorf("new: resolve identity: %w", err)
			}
			creator := res.Handle

			// Step 3: Resolve tree.
			treeSlug, err := resolveNewTree(repoRoot, treeFlag, cfg)
			if err != nil {
				return fmt.Errorf("new: %w", err)
			}

			treeDir := filepath.Join(repoRoot, ".decisions", treeSlug)

			// Get positional summary if provided.
			summary := ""
			if len(args) > 0 {
				summary = strings.TrimSpace(args[0])
			}

			// Determine which input mode to use.
			var d *core.Decision

			switch {
			case fromFile != "":
				// Step 4: Read from file.
				d, err = readDecisionFromPath(fromFile)
				if err != nil {
					return fmt.Errorf("new: read from file: %w", err)
				}

			case fromStdin:
				// Step 5: Read from stdin.
				d, err = readDecisionFromReader(cmd.InOrStdin())
				if err != nil {
					return fmt.Errorf("new: read from stdin: %w", err)
				}

			case hasBodyFlags(cmd):
				// Step 6: Build from flags.
				d = buildDecisionFromFlags(summary, description, priority, tags, assignee,
					recommendedSummary, recommendedFull, recommendedBy, creator)

			case noEdit:
				// Step 7: Interactive prompts.
				d, err = promptDecisionInteractive(cmd, summary, creator)
				if err != nil {
					return fmt.Errorf("new: interactive prompt: %w", err)
				}

			default:
				// Step 8: Open editor.
				editorPath := resolveEditor(editorFlag, cfg)
				d, err = editDecisionInEditor(cmd, editorPath, summary, creator)
				if err != nil {
					return fmt.Errorf("new: editor: %w", err)
				}
			}

			// Apply creator if not set by the input source.
			if d.Creator == "" {
				d.Creator = creator
			}
			// Apply tree.
			d.Tree = treeSlug

			// Step 9: Generate ULID and slug.
			d.ID = ulid.New()
			d.Slug = storage.SlugFromSummary(d.Summary)
			d.SchemaVersion = core.SchemaVersion

			// Set defaults for required fields.
			if d.Status == "" {
				d.Status = core.StatusProposed
			}
			if d.Priority == "" {
				d.Priority = core.PriorityMedium
			}

			// Step 10: Validate.
			if err := validate.Decision(d); err != nil {
				return fmt.Errorf("new: validation: %w", err)
			}

			// Step 11: Write to disk.
			decisionPath := storage.DecisionPath(treeDir, d.ID, d.Slug)
			if err := storage.WriteDecision(decisionPath, d); err != nil {
				return fmt.Errorf("new: write decision: %w", err)
			}

			// Step 12: Compute content SHA.
			contentSha, err := fsutil.Sha256File(decisionPath)
			if err != nil {
				return fmt.Errorf("new: sha256: %w", err)
			}

			// Step 13: Insert into index.
			db, err := openIndex(repoRoot)
			if err != nil {
				return fmt.Errorf("new: open index: %w", err)
			}
			defer db.Close()

			if err := index.InsertDecision(db, d, contentSha); err != nil {
				return fmt.Errorf("new: insert index: %w", err)
			}

			// Step 14: Append audit event.
			afterPayload := decisionToMap(d)
			ev := core.Event{
				Actor:  creator,
				Action: core.ActionCreate,
				Kind:   core.KindDecision,
				Tree:   treeSlug,
				ID:     d.ID,
				Payload: core.EventPayload{
					After: afterPayload,
				},
			}
			if err := audit.Append(repoRoot, ev); err != nil {
				return fmt.Errorf("new: audit event: %w", err)
			}

			// Step 16: Output.
			format := outputFormat(cmd)
			return printDecision(cmd, d, format)
		},
	}

	cmd.Flags().StringVar(&description, "description", "", "Decision description")
	cmd.Flags().StringVar(&priority, "priority", "", "Priority: assumption|low|medium|high|critical")
	cmd.Flags().StringArrayVar(&tags, "tag", nil, "Tag (repeatable)")
	cmd.Flags().StringVar(&assignee, "assignee", "", "Assignee handle")
	cmd.Flags().StringVar(&treeFlag, "tree", "", "Tree slug")
	cmd.Flags().StringVar(&recommendedSummary, "recommended-summary", "", "Recommended summary")
	cmd.Flags().StringVar(&recommendedFull, "recommended-full", "", "Recommended full text")
	cmd.Flags().StringVar(&recommendedBy, "recommended-by", "", "Recommended by handle")
	cmd.Flags().StringVar(&fromFile, "from-file", "", "Read decision from YAML file")
	cmd.Flags().BoolVar(&fromStdin, "from-stdin", false, "Read decision from stdin")
	cmd.Flags().BoolVar(&noEdit, "no-edit", false, "Prompt interactively instead of opening editor")
	cmd.Flags().StringVar(&editorFlag, "editor", "", "Editor binary to use (overrides $EDITOR)")
	cmd.Flags().String("output", "", "Output format: human, json, yaml")

	return cmd
}

// resolveNewTree determines which tree to use for a new decision.
func resolveNewTree(repoRoot, treeFlag string, cfg *config.Resolved) (string, error) {
	if treeFlag != "" {
		return treeFlag, nil
	}
	if cfg.DefaultTree != "" {
		return cfg.DefaultTree, nil
	}

	// Check trees.yaml.
	treesPath := filepath.Join(repoRoot, ".decisions", storage.TreesFileName)
	tf, err := storage.ReadTrees(treesPath)
	if err != nil {
		return "", fmt.Errorf("read trees.yaml: %w", err)
	}
	if len(tf.Trees) == 1 {
		return tf.Trees[0], nil
	}
	if len(tf.Trees) == 0 {
		return "", fmt.Errorf("no trees registered; run `dtree tree create <slug>`")
	}
	return "", fmt.Errorf("multiple trees available; specify --tree: %s", strings.Join(tf.Trees, ", "))
}

// hasBodyFlags reports whether any body-content flags were explicitly set.
func hasBodyFlags(cmd *cobra.Command) bool {
	bodyFlags := []string{
		"description", "priority", "tag", "assignee",
		"recommended-summary", "recommended-full", "recommended-by",
	}
	for _, name := range bodyFlags {
		if f := cmd.Flags().Lookup(name); f != nil && f.Changed {
			return true
		}
	}
	return false
}

// buildDecisionFromFlags assembles a Decision from the provided flag values.
func buildDecisionFromFlags(
	summary, description, priority string,
	tags []string,
	assignee, recommendedSummary, recommendedFull, recommendedBy string,
	creator string,
) *core.Decision {
	d := &core.Decision{
		Summary:            summary,
		Description:        description,
		Assignee:           assignee,
		Tags:               tags,
		RecommendedSummary: recommendedSummary,
		RecommendedFull:    recommendedFull,
		RecommendedBy:      recommendedBy,
		Creator:            creator,
		Status:             core.StatusProposed,
	}
	if priority != "" {
		d.Priority = core.Priority(priority)
	} else {
		d.Priority = core.PriorityMedium
	}
	return d
}

// readDecisionFromPath reads a YAML decision file at path.
func readDecisionFromPath(path string) (*core.Decision, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	return readDecisionFromReader(f)
}

// readDecisionFromReader deserializes a YAML decision from r.
func readDecisionFromReader(r io.Reader) (*core.Decision, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	var d core.Decision
	if err := yaml.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("unmarshal yaml: %w", err)
	}
	return &d, nil
}

// promptDecisionInteractive prompts the user interactively for required fields.
func promptDecisionInteractive(cmd *cobra.Command, summary, creator string) (*core.Decision, error) {
	r := bufio.NewReader(cmd.InOrStdin())
	w := cmd.OutOrStdout()

	if summary == "" {
		s, err := prompt(r, w, "Summary", "")
		if err != nil {
			return nil, err
		}
		summary = strings.TrimSpace(s)
	}
	if summary == "" {
		return nil, fmt.Errorf("summary is required")
	}

	priorityStr, err := prompt(r, w, "Priority (assumption|low|medium|high|critical)", "medium")
	if err != nil {
		return nil, err
	}
	if priorityStr == "" {
		priorityStr = "medium"
	}

	desc, err := prompt(r, w, "Description", "")
	if err != nil {
		return nil, err
	}

	return &core.Decision{
		Summary:     summary,
		Priority:    core.Priority(priorityStr),
		Description: desc,
		Creator:     creator,
		Status:      core.StatusProposed,
	}, nil
}

// editorTemplate is the YAML buffer shown to the user in the editor.
const editorTemplate = `# Lines starting with '#' are ignored.
# Save and exit to create the decision; exit without saving to abort.
summary: %s
priority: medium
tags: []
assignee:
decision_full_description: |

recommended_summary:
recommended_full: |

recommended_by:
`

// editDecisionInEditor opens the editor with a pre-populated template and
// reads the result back. Returns an error if the user aborted (no change).
func editDecisionInEditor(cmd *cobra.Command, editorPath, summary, creator string) (*core.Decision, error) {
	// Write template to a temp file.
	tmpDir, err := os.MkdirTemp("", "dtree-new-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpFile := filepath.Join(tmpDir, "decision.yaml")
	content := fmt.Sprintf(editorTemplate, summary)
	if err := os.WriteFile(tmpFile, []byte(content), 0o600); err != nil {
		return nil, fmt.Errorf("write temp file: %w", err)
	}

	// Record content hash before editor for abort detection.
	hashBefore, err := fsutil.Sha256File(tmpFile)
	if err != nil {
		return nil, fmt.Errorf("hash temp file: %w", err)
	}

	// Spawn editor.
	editorCmd := exec.Command(editorPath, tmpFile) //nolint:gosec
	editorCmd.Stdin = cmd.InOrStdin()
	editorCmd.Stdout = cmd.OutOrStdout()
	editorCmd.Stderr = cmd.ErrOrStderr()
	if err := editorCmd.Run(); err != nil {
		return nil, fmt.Errorf("editor exited with error: %w", err)
	}

	// Detect abort: if content is unchanged the user did not save.
	hashAfter, err := fsutil.Sha256File(tmpFile)
	if err != nil {
		return nil, fmt.Errorf("hash temp file after edit: %w", err)
	}
	if hashAfter == hashBefore {
		return nil, fmt.Errorf("aborted: file was not modified")
	}

	// Read and strip comment lines.
	raw, err := os.ReadFile(tmpFile)
	if err != nil {
		return nil, fmt.Errorf("read temp file: %w", err)
	}
	stripped := stripCommentLines(raw)

	var d core.Decision
	if err := yaml.Unmarshal(stripped, &d); err != nil {
		return nil, fmt.Errorf("parse decision yaml: %w", err)
	}
	d.Creator = creator
	return &d, nil
}

// stripCommentLines removes lines beginning with '#' from YAML content.
func stripCommentLines(data []byte) []byte {
	var buf bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	return buf.Bytes()
}

// resolveEditor returns the editor binary to use.
func resolveEditor(editorFlag string, cfg *config.Resolved) string {
	if editorFlag != "" {
		return editorFlag
	}
	if cfg.Editor != "" && cfg.Editor != "vi" {
		return cfg.Editor
	}
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	return "vi"
}

// decisionToMap serializes a Decision to map[string]any for audit payloads.
func decisionToMap(d *core.Decision) map[string]any {
	b, _ := json.Marshal(d)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

// printDecision writes d in the requested format.
func printDecision(cmd *cobra.Command, d *core.Decision, format string) error {
	switch format {
	case "json":
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(d)

	case "yaml":
		enc := yaml.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent(2)
		if err := enc.Encode(d); err != nil {
			return err
		}
		return enc.Close()

	default:
		// Human: print short prefix of ID + tree + summary.
		prefix := d.ID
		if len(prefix) > 8 {
			prefix = prefix[:8]
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Created %s in %s (%s)\n", prefix, d.Tree, d.Summary)
		return nil
	}
}

