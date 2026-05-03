package cli

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/config"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/identity"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/storage"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// treeSlugRE is the allowed pattern for tree slugs.
var treeSlugRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

// newTreeCommand returns the `dtree tree` subcommand group.
func newTreeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tree",
		Short: "Manage decision trees",
		Long:  "Create, list, show, rename, archive, and delete decision trees.",
	}

	cmd.AddCommand(newTreeCreateCommand())
	cmd.AddCommand(newTreeListCommand())
	cmd.AddCommand(newTreeShowCommand())
	cmd.AddCommand(newTreeRenameCommand())
	cmd.AddCommand(newTreeArchiveCommand())
	cmd.AddCommand(newTreeDeleteCommand())

	return cmd
}

// requireDecisionsDir checks that .decisions/ exists in repoRoot.
func requireDecisionsDir(repoRoot string) error {
	decisionsDir := filepath.Join(repoRoot, ".decisions")
	if _, err := os.Stat(decisionsDir); os.IsNotExist(err) {
		return fmt.Errorf("no .decisions/ — run `dtree init`")
	}
	return nil
}

// openIndex opens the SQLite index at <repoRoot>/.decisions/.index.db.
func openIndex(repoRoot string) (*index.DB, error) {
	indexPath := filepath.Join(repoRoot, ".decisions", ".index.db")
	return index.Open(indexPath)
}

// outputFormat resolves the output format from the local flag or global flag.
func outputFormat(cmd *cobra.Command) string {
	local, _ := cmd.Flags().GetString("output")
	if local != "" {
		return local
	}
	global, _ := cmd.Root().PersistentFlags().GetString("output")
	if global != "" {
		return global
	}
	return "human"
}

// ---------------------------------------------------------------------------
// tree create
// ---------------------------------------------------------------------------

func newTreeCreateCommand() *cobra.Command {
	var (
		title       string
		description string
		asFlag      string
	)

	cmd := &cobra.Command{
		Use:   "create <slug>",
		Short: "Create a new decision tree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")

			if err := requireDecisionsDir(repoRoot); err != nil {
				return err
			}

			if !treeSlugRE.MatchString(slug) {
				return fmt.Errorf("invalid slug %q: must match ^[a-z][a-z0-9-]{0,63}$", slug)
			}

			treeDir := filepath.Join(repoRoot, ".decisions", slug)
			if _, err := os.Stat(treeDir); err == nil {
				return fmt.Errorf("tree %q already exists", slug)
			}

			// Create directory structure.
			for _, dir := range []string{
				filepath.Join(treeDir, "decisions"),
				filepath.Join(treeDir, "audit"),
			} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return fmt.Errorf("tree create: mkdir %s: %w", dir, err)
				}
			}

			tree := &core.Tree{
				Slug:          slug,
				SchemaVersion: core.SchemaVersion,
				Title:         title,
				Description:   description,
				CreatedAt:     time.Now().UTC(),
			}
			tree.Layout.Direction = "TB"

			treeMetaPath := filepath.Join(treeDir, storage.TreeMetaFileName)
			if err := storage.WriteTree(treeMetaPath, tree); err != nil {
				return fmt.Errorf("tree create: write tree.yaml: %w", err)
			}

			// Update trees.yaml.
			treesPath := filepath.Join(repoRoot, ".decisions", storage.TreesFileName)
			tf, err := storage.ReadTrees(treesPath)
			if err != nil {
				return fmt.Errorf("tree create: read trees.yaml: %w", err)
			}
			tf.Trees = append(tf.Trees, slug)
			sort.Strings(tf.Trees)
			if err := storage.WriteTrees(treesPath, tf); err != nil {
				return fmt.Errorf("tree create: write trees.yaml: %w", err)
			}

			// Insert into index.
			db, err := openIndex(repoRoot)
			if err != nil {
				return fmt.Errorf("tree create: open index: %w", err)
			}
			defer db.Close()

			if err := insertTreeRow(db, tree); err != nil {
				return fmt.Errorf("tree create: index tree: %w", err)
			}

			// Resolve identity for audit.
			cfg, err := config.Load(repoRoot)
			if err != nil {
				return fmt.Errorf("tree create: load config: %w", err)
			}
			res, err := identity.NewResolver(repoRoot, cfg).MustResolve(asFlag)
			if err != nil {
				return fmt.Errorf("tree create: resolve identity: %w", err)
			}

			// Emit tree_create audit event (repo-level).
			ev := core.Event{
				Actor:  res.Handle,
				Action: core.ActionTreeCreate,
				Kind:   core.KindTree,
				ID:     slug,
				Payload: core.EventPayload{
					After: map[string]any{
						"slug":        tree.Slug,
						"title":       tree.Title,
						"description": tree.Description,
						"archived":    tree.Archived,
						"created_at":  tree.CreatedAt.Format(time.RFC3339),
					},
				},
			}
			if err := audit.Append(repoRoot, ev); err != nil {
				return fmt.Errorf("tree create: audit event: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Created tree %q\n", slug)
			return nil
		},
	}

	cmd.Flags().StringVar(&title, "title", "", "Tree title")
	cmd.Flags().StringVar(&description, "description", "", "Tree description")
	cmd.Flags().StringVar(&asFlag, "as", "", "Override identity handle for this invocation")
	return cmd
}

// ---------------------------------------------------------------------------
// tree list
// ---------------------------------------------------------------------------

// treeListRow is the data shape used for list output.
type treeListRow struct {
	Slug          string `json:"slug" yaml:"slug"`
	Title         string `json:"title" yaml:"title"`
	DecisionCount int    `json:"decision_count" yaml:"decision_count"`
	Archived      bool   `json:"archived" yaml:"archived"`
}

func newTreeListCommand() *cobra.Command {
	var includeArchived bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List decision trees",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")

			if err := requireDecisionsDir(repoRoot); err != nil {
				return err
			}

			rows, err := listTrees(repoRoot, includeArchived)
			if err != nil {
				return err
			}

			format := outputFormat(cmd)
			switch format {
			case "json":
				return treeListJSON(cmd, rows)
			case "yaml":
				return treeListYAML(cmd, rows)
			default:
				return treeListHuman(cmd, rows)
			}
		},
	}

	cmd.Flags().BoolVar(&includeArchived, "include-archived", false, "Include archived trees")
	cmd.Flags().String("output", "", "Output format: human, json, yaml")
	return cmd
}

// listTrees reads trees from the index (falling back to trees.yaml) and
// returns them sorted by slug.
func listTrees(repoRoot string, includeArchived bool) ([]treeListRow, error) {
	// Try index first.
	indexPath := filepath.Join(repoRoot, ".decisions", ".index.db")
	if _, err := os.Stat(indexPath); err == nil {
		db, err := index.Open(indexPath)
		if err == nil {
			defer db.Close()
			rows, err := listTreesFromIndex(db, includeArchived)
			if err == nil {
				return rows, nil
			}
		}
	}

	// Fallback: trees.yaml.
	return listTreesFromYAML(repoRoot, includeArchived)
}

func listTreesFromIndex(db *index.DB, includeArchived bool) ([]treeListRow, error) {
	query := `
		SELECT t.slug, t.title, t.archived,
		       COUNT(d.id) AS decision_count
		FROM trees t
		LEFT JOIN decisions d ON d.tree = t.slug AND d.deleted = 0
		%s
		GROUP BY t.slug
		ORDER BY t.slug`

	where := ""
	if !includeArchived {
		where = "WHERE t.archived = 0"
	}
	query = fmt.Sprintf(query, where)

	sqlRows, err := db.Conn().Query(query)
	if err != nil {
		return nil, fmt.Errorf("tree list: query index: %w", err)
	}
	defer sqlRows.Close()

	var rows []treeListRow
	for sqlRows.Next() {
		var r treeListRow
		var archived int
		if err := sqlRows.Scan(&r.Slug, &r.Title, &archived, &r.DecisionCount); err != nil {
			return nil, fmt.Errorf("tree list: scan row: %w", err)
		}
		r.Archived = archived == 1
		rows = append(rows, r)
	}
	return rows, sqlRows.Err()
}

func listTreesFromYAML(repoRoot string, includeArchived bool) ([]treeListRow, error) {
	treesPath := filepath.Join(repoRoot, ".decisions", storage.TreesFileName)
	tf, err := storage.ReadTrees(treesPath)
	if err != nil {
		return nil, fmt.Errorf("tree list: read trees.yaml: %w", err)
	}

	var rows []treeListRow
	for _, slug := range tf.Trees {
		treeMetaPath := filepath.Join(repoRoot, ".decisions", slug, storage.TreeMetaFileName)
		t, err := storage.ReadTree(treeMetaPath)
		if err != nil {
			continue
		}
		if !includeArchived && t.Archived {
			continue
		}
		rows = append(rows, treeListRow{
			Slug:     t.Slug,
			Title:    t.Title,
			Archived: t.Archived,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Slug < rows[j].Slug })
	return rows, nil
}

func treeListHuman(cmd *cobra.Command, rows []treeListRow) error {
	if len(rows) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "(no trees)")
		return nil
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SLUG\tTITLE\tDECISIONS\tARCHIVED")
	for _, r := range rows {
		archived := ""
		if r.Archived {
			archived = "archived"
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", r.Slug, r.Title, r.DecisionCount, archived)
	}
	return w.Flush()
}

func treeListJSON(cmd *cobra.Command, rows []treeListRow) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

func treeListYAML(cmd *cobra.Command, rows []treeListRow) error {
	enc := yaml.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent(2)
	if err := enc.Encode(rows); err != nil {
		return err
	}
	return enc.Close()
}

// ---------------------------------------------------------------------------
// tree show
// ---------------------------------------------------------------------------

// treeShowResult is the data shape for show output.
type treeShowResult struct {
	Slug          string    `json:"slug" yaml:"slug"`
	Title         string    `json:"title,omitempty" yaml:"title,omitempty"`
	Description   string    `json:"description,omitempty" yaml:"description,omitempty"`
	Archived      bool      `json:"archived" yaml:"archived"`
	CreatedAt     time.Time `json:"created_at" yaml:"created_at"`
	DecisionCount int       `json:"decision_count" yaml:"decision_count"`
}

func newTreeShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <slug>",
		Short: "Show details of a decision tree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")

			if err := requireDecisionsDir(repoRoot); err != nil {
				return err
			}

			treeMetaPath := filepath.Join(repoRoot, ".decisions", slug, storage.TreeMetaFileName)
			t, err := storage.ReadTree(treeMetaPath)
			if err != nil {
				return fmt.Errorf("tree show: %w", err)
			}

			count, err := countDecisions(repoRoot, slug)
			if err != nil {
				return fmt.Errorf("tree show: count decisions: %w", err)
			}

			result := treeShowResult{
				Slug:          t.Slug,
				Title:         t.Title,
				Description:   t.Description,
				Archived:      t.Archived,
				CreatedAt:     t.CreatedAt,
				DecisionCount: count,
			}

			format := outputFormat(cmd)
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
			default:
				fmt.Fprintf(cmd.OutOrStdout(), "Slug:       %s\n", result.Slug)
				if result.Title != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "Title:      %s\n", result.Title)
				}
				if result.Description != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "Description: %s\n", result.Description)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Archived:   %v\n", result.Archived)
				fmt.Fprintf(cmd.OutOrStdout(), "Created:    %s\n", result.CreatedAt.Format(time.RFC3339))
				fmt.Fprintf(cmd.OutOrStdout(), "Decisions:  %d\n", result.DecisionCount)
			}
			return nil
		},
	}

	cmd.Flags().String("output", "", "Output format: human, json, yaml")
	return cmd
}

// countDecisions returns the number of non-deleted decisions in a tree.
func countDecisions(repoRoot, treeSlug string) (int, error) {
	indexPath := filepath.Join(repoRoot, ".decisions", ".index.db")
	if _, err := os.Stat(indexPath); err != nil {
		return 0, nil
	}
	db, err := index.Open(indexPath)
	if err != nil {
		return 0, nil
	}
	defer db.Close()

	var count int
	err = db.Conn().QueryRow(
		`SELECT COUNT(*) FROM decisions WHERE tree=? AND deleted=0`, treeSlug,
	).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// ---------------------------------------------------------------------------
// tree rename
// ---------------------------------------------------------------------------

func newTreeRenameCommand() *cobra.Command {
	var asFlag string

	cmd := &cobra.Command{
		Use:   "rename <old-slug> <new-slug>",
		Short: "Rename a decision tree",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			oldSlug := args[0]
			newSlug := args[1]
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")

			if err := requireDecisionsDir(repoRoot); err != nil {
				return err
			}

			if !treeSlugRE.MatchString(newSlug) {
				return fmt.Errorf("invalid slug %q: must match ^[a-z][a-z0-9-]{0,63}$", newSlug)
			}

			decisionsDir := filepath.Join(repoRoot, ".decisions")
			oldDir := filepath.Join(decisionsDir, oldSlug)
			newDir := filepath.Join(decisionsDir, newSlug)

			// Verify old tree exists.
			treeMetaPath := filepath.Join(oldDir, storage.TreeMetaFileName)
			t, err := storage.ReadTree(treeMetaPath)
			if err != nil {
				return fmt.Errorf("tree rename: %w", err)
			}

			if t.Archived {
				return fmt.Errorf("tree %q is archived; cannot rename an archived tree", oldSlug)
			}

			// Verify new slug is not taken.
			if _, err := os.Stat(newDir); err == nil {
				return fmt.Errorf("tree %q already exists", newSlug)
			}

			// Rename directory.
			if err := os.Rename(oldDir, newDir); err != nil {
				return fmt.Errorf("tree rename: rename directory: %w", err)
			}

			// Update tree.yaml with new slug.
			t.Slug = newSlug
			if err := storage.WriteTree(filepath.Join(newDir, storage.TreeMetaFileName), t); err != nil {
				return fmt.Errorf("tree rename: write tree.yaml: %w", err)
			}

			// Update trees.yaml.
			treesPath := filepath.Join(decisionsDir, storage.TreesFileName)
			tf, err := storage.ReadTrees(treesPath)
			if err != nil {
				return fmt.Errorf("tree rename: read trees.yaml: %w", err)
			}
			for i, s := range tf.Trees {
				if s == oldSlug {
					tf.Trees[i] = newSlug
					break
				}
			}
			sort.Strings(tf.Trees)
			if err := storage.WriteTrees(treesPath, tf); err != nil {
				return fmt.Errorf("tree rename: write trees.yaml: %w", err)
			}

			// Update index tables.
			db, err := openIndex(repoRoot)
			if err != nil {
				return fmt.Errorf("tree rename: open index: %w", err)
			}
			defer db.Close()

			var nDecisions, nRelationships, nEvents int64
			tx, err := db.Conn().Begin()
			if err != nil {
				return fmt.Errorf("tree rename: begin tx: %w", err)
			}
			defer func() { _ = tx.Rollback() }()

			res, err := tx.Exec(`UPDATE decisions SET tree=? WHERE tree=?`, newSlug, oldSlug)
			if err != nil {
				return fmt.Errorf("tree rename: update decisions: %w", err)
			}
			nDecisions, _ = res.RowsAffected()

			res, err = tx.Exec(`UPDATE relationships SET tree=? WHERE tree=?`, newSlug, oldSlug)
			if err != nil {
				return fmt.Errorf("tree rename: update relationships: %w", err)
			}
			nRelationships, _ = res.RowsAffected()

			res, err = tx.Exec(`UPDATE events SET tree=? WHERE tree=?`, newSlug, oldSlug)
			if err != nil {
				return fmt.Errorf("tree rename: update events: %w", err)
			}
			nEvents, _ = res.RowsAffected()

			_, err = tx.Exec(`UPDATE trees SET slug=? WHERE slug=?`, newSlug, oldSlug)
			if err != nil {
				return fmt.Errorf("tree rename: update trees: %w", err)
			}

			if err := tx.Commit(); err != nil {
				return fmt.Errorf("tree rename: commit: %w", err)
			}

			// Emit audit event.
			cfg, err := config.Load(repoRoot)
			if err != nil {
				return fmt.Errorf("tree rename: load config: %w", err)
			}
			actor, err := identity.NewResolver(repoRoot, cfg).MustResolve(asFlag)
			if err != nil {
				return fmt.Errorf("tree rename: resolve identity: %w", err)
			}

			ev := core.Event{
				Actor:  actor.Handle,
				Action: core.ActionTreeRename,
				Kind:   core.KindTree,
				ID:     newSlug,
				Payload: core.EventPayload{
					Before: map[string]any{"slug": oldSlug},
					After:  map[string]any{"slug": newSlug},
				},
			}
			if err := audit.Append(repoRoot, ev); err != nil {
				return fmt.Errorf("tree rename: audit event: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(),
				"Renamed %s → %s; updated %d decisions, %d relationships, %d events.\n",
				oldSlug, newSlug, nDecisions, nRelationships, nEvents)
			return nil
		},
	}

	cmd.Flags().StringVar(&asFlag, "as", "", "Override identity handle for this invocation")
	return cmd
}

// ---------------------------------------------------------------------------
// tree archive
// ---------------------------------------------------------------------------

func newTreeArchiveCommand() *cobra.Command {
	var asFlag string

	cmd := &cobra.Command{
		Use:   "archive <slug>",
		Short: "Archive a decision tree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")

			if err := requireDecisionsDir(repoRoot); err != nil {
				return err
			}

			treeMetaPath := filepath.Join(repoRoot, ".decisions", slug, storage.TreeMetaFileName)
			t, err := storage.ReadTree(treeMetaPath)
			if err != nil {
				return fmt.Errorf("tree archive: %w", err)
			}

			if t.Archived {
				return fmt.Errorf("tree %q is already archived", slug)
			}

			t.Archived = true
			if err := storage.WriteTree(treeMetaPath, t); err != nil {
				return fmt.Errorf("tree archive: write tree.yaml: %w", err)
			}

			// Update index.
			db, err := openIndex(repoRoot)
			if err != nil {
				return fmt.Errorf("tree archive: open index: %w", err)
			}
			defer db.Close()

			if _, err := db.Conn().Exec(
				`UPDATE trees SET archived=1 WHERE slug=?`, slug,
			); err != nil {
				return fmt.Errorf("tree archive: update index: %w", err)
			}

			// Emit audit event.
			cfg, err := config.Load(repoRoot)
			if err != nil {
				return fmt.Errorf("tree archive: load config: %w", err)
			}
			actor, err := identity.NewResolver(repoRoot, cfg).MustResolve(asFlag)
			if err != nil {
				return fmt.Errorf("tree archive: resolve identity: %w", err)
			}

			ev := core.Event{
				Actor:  actor.Handle,
				Action: core.ActionTreeArchive,
				Kind:   core.KindTree,
				ID:     slug,
				Payload: core.EventPayload{
					After: map[string]any{"slug": slug, "archived": true},
				},
			}
			if err := audit.Append(repoRoot, ev); err != nil {
				return fmt.Errorf("tree archive: audit event: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Archived tree %q\n", slug)
			return nil
		},
	}

	cmd.Flags().StringVar(&asFlag, "as", "", "Override identity handle for this invocation")
	return cmd
}

// ---------------------------------------------------------------------------
// tree delete
// ---------------------------------------------------------------------------

func newTreeDeleteCommand() *cobra.Command {
	var (
		force            bool
		confirmName      string
		cascadeDecisions bool
		asFlag           string
	)

	cmd := &cobra.Command{
		Use:   "delete <slug>",
		Short: "Delete a decision tree",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")

			if err := requireDecisionsDir(repoRoot); err != nil {
				return err
			}

			// Safety checks.
			if !force {
				return fmt.Errorf("tree delete requires --force and --confirm-name %s", slug)
			}
			if confirmName != slug {
				return fmt.Errorf("--confirm-name %q does not match tree slug %q", confirmName, slug)
			}

			treeMetaPath := filepath.Join(repoRoot, ".decisions", slug, storage.TreeMetaFileName)
			t, err := storage.ReadTree(treeMetaPath)
			if err != nil {
				return fmt.Errorf("tree delete: %w", err)
			}

			// Check for decisions.
			count, err := countDecisions(repoRoot, slug)
			if err != nil {
				return fmt.Errorf("tree delete: count decisions: %w", err)
			}
			if count > 0 && !cascadeDecisions {
				return fmt.Errorf(
					"tree %q has %d decision(s); use --cascade-decisions to delete them",
					slug, count)
			}

			// Open index before removing directory.
			db, err := openIndex(repoRoot)
			if err != nil {
				return fmt.Errorf("tree delete: open index: %w", err)
			}
			defer db.Close()

			// Resolve identity before any mutations.
			cfg, err := config.Load(repoRoot)
			if err != nil {
				return fmt.Errorf("tree delete: load config: %w", err)
			}
			actor, err := identity.NewResolver(repoRoot, cfg).MustResolve(asFlag)
			if err != nil {
				return fmt.Errorf("tree delete: resolve identity: %w", err)
			}

			// Remove from index (FK cascade handles decisions, relationships, etc.).
			if _, err := db.Conn().Exec(`DELETE FROM trees WHERE slug=?`, slug); err != nil {
				return fmt.Errorf("tree delete: delete from index: %w", err)
			}

			// Remove directory.
			treeDir := filepath.Join(repoRoot, ".decisions", slug)
			if err := os.RemoveAll(treeDir); err != nil {
				return fmt.Errorf("tree delete: remove directory: %w", err)
			}

			// Update trees.yaml.
			treesPath := filepath.Join(repoRoot, ".decisions", storage.TreesFileName)
			tf, err := storage.ReadTrees(treesPath)
			if err != nil {
				return fmt.Errorf("tree delete: read trees.yaml: %w", err)
			}
			filtered := tf.Trees[:0]
			for _, s := range tf.Trees {
				if s != slug {
					filtered = append(filtered, s)
				}
			}
			tf.Trees = filtered
			if err := storage.WriteTrees(treesPath, tf); err != nil {
				return fmt.Errorf("tree delete: write trees.yaml: %w", err)
			}

			// Emit audit event.
			ev := core.Event{
				Actor:  actor.Handle,
				Action: core.ActionTreeDelete,
				Kind:   core.KindTree,
				ID:     slug,
				Payload: core.EventPayload{
					Before: map[string]any{
						"slug":        t.Slug,
						"title":       t.Title,
						"description": t.Description,
						"archived":    t.Archived,
						"created_at":  t.CreatedAt.Format(time.RFC3339),
					},
				},
			}
			if err := audit.Append(repoRoot, ev); err != nil {
				return fmt.Errorf("tree delete: audit event: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Deleted tree %q\n", slug)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Required: confirm destructive delete")
	cmd.Flags().StringVar(&confirmName, "confirm-name", "", "Required: type the tree slug to confirm")
	cmd.Flags().BoolVar(&cascadeDecisions, "cascade-decisions", false, "Delete decisions in this tree")
	cmd.Flags().StringVar(&asFlag, "as", "", "Override identity handle for this invocation")
	return cmd
}

// ---------------------------------------------------------------------------
// SQL helpers (reused from init.go pattern but for trees table updates)
// ---------------------------------------------------------------------------

// treeFromIndex reads a tree row from the index by slug.
// Returns sql.ErrNoRows if the slug is not found.
func treeFromIndex(db *index.DB, slug string) (*core.Tree, error) {
	var t core.Tree
	var archived int
	var createdAt, direction string
	err := db.Conn().QueryRow(
		`SELECT slug, title, description, archived, created_at, layout_direction, schema_version
		 FROM trees WHERE slug=?`, slug,
	).Scan(&t.Slug, &t.Title, &t.Description, &archived, &createdAt, &direction, &t.SchemaVersion)
	if err != nil {
		return nil, err
	}
	t.Archived = archived == 1
	t.Layout.Direction = direction
	if ts, err := time.Parse(time.RFC3339, createdAt); err == nil {
		t.CreatedAt = ts
	}
	return &t, nil
}

// treeExistsInIndex reports whether a tree slug exists in the index.
func treeExistsInIndex(db *index.DB, slug string) (bool, error) {
	_, err := treeFromIndex(db, slug)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
