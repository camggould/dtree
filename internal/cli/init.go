package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/config"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/storage"
	"github.com/spf13/cobra"
)

// gitattributesContent is written verbatim to .decisions/.gitattributes.
const gitattributesContent = "audit/**/*.jsonl merge=union\n"

// newInitCommand returns the `dtree init` command.
func newInitCommand() *cobra.Command {
	var (
		nonInteractive bool
		actorHandle    string
		actorName      string
		actorEmail     string
		actorKind      string
		firstTree      string
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a .decisions/ directory in the current repo",
		Long:  "Bootstrap the .decisions/ layout, register an actor, and optionally create a first decision tree.",
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			return runInit(cmd, repoRoot, nonInteractive, actorHandle, actorName, actorEmail, actorKind, firstTree)
		},
	}

	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Skip all prompts; require flags instead")
	cmd.Flags().StringVar(&actorHandle, "actor-handle", "", "Actor handle (e.g. cam)")
	cmd.Flags().StringVar(&actorName, "actor-name", "", "Actor display name")
	cmd.Flags().StringVar(&actorEmail, "actor-email", "", "Actor email address")
	cmd.Flags().StringVar(&actorKind, "actor-kind", "", "Actor kind: human or agent (default: human)")
	cmd.Flags().StringVar(&firstTree, "first-tree", "", "Name of the first decision tree to create")

	return cmd
}

// runInit is the main implementation, factored out for testability.
func runInit(
	cmd *cobra.Command,
	repoRoot string,
	nonInteractive bool,
	actorHandle, actorName, actorEmail, actorKind, firstTree string,
) error {
	decisionsDir := filepath.Join(repoRoot, ".decisions")

	// Step 2: Refuse if already initialized (idempotent exit-0).
	if _, err := os.Stat(decisionsDir); err == nil {
		fmt.Fprintf(cmd.OutOrStdout(),
			"already initialized at %s; nothing to do\n", decisionsDir)
		return nil
	}

	// Validate / slugify --first-tree before touching the filesystem.
	var treeSlug string
	if firstTree != "" {
		trimmed := strings.TrimSpace(firstTree)
		if trimmed == "" {
			return fmt.Errorf("--first-tree must not be empty or whitespace")
		}
		treeSlug = storage.SlugFromSummary(trimmed)
		if treeSlug == "" {
			return fmt.Errorf("--first-tree %q produces an empty slug", firstTree)
		}
	}

	// Step 3: Create directory structure.
	dirsToCreate := []string{
		decisionsDir,
		filepath.Join(decisionsDir, "audit"),
	}
	if treeSlug != "" {
		dirsToCreate = append(dirsToCreate,
			filepath.Join(decisionsDir, treeSlug, "decisions"),
			filepath.Join(decisionsDir, treeSlug, "audit"),
		)
	}
	for _, d := range dirsToCreate {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("init: mkdir %s: %w", d, err)
		}
	}

	// Step 4: Write .gitattributes.
	gaPath := filepath.Join(decisionsDir, ".gitattributes")
	if err := os.WriteFile(gaPath, []byte(gitattributesContent), 0o644); err != nil {
		return fmt.Errorf("init: write .gitattributes: %w", err)
	}

	// Step 5: Determine actor.
	actor, err := resolveInitActor(cmd, nonInteractive, actorHandle, actorName, actorEmail, actorKind)
	if err != nil {
		return err
	}

	// Step 6: Write actors.yaml.
	actorsPath := filepath.Join(decisionsDir, storage.ActorsFileName)
	af := &storage.ActorsFile{Actors: []core.Actor{actor}}
	if err := storage.WriteActors(actorsPath, af); err != nil {
		return fmt.Errorf("init: write actors.yaml: %w", err)
	}

	// Step 7: Write trees.yaml.
	treesPath := filepath.Join(decisionsDir, storage.TreesFileName)
	tf := &storage.TreesFile{}
	if treeSlug != "" {
		tf.Trees = []string{treeSlug}
	}
	if err := storage.WriteTrees(treesPath, tf); err != nil {
		return fmt.Errorf("init: write trees.yaml: %w", err)
	}

	// Step 8: Write config.yaml (empty local config).
	localCfgPath := config.LocalPath(repoRoot)
	if err := config.WriteFile(localCfgPath, &config.File{}); err != nil {
		return fmt.Errorf("init: write config.yaml: %w", err)
	}

	// Step 9: Write tree.yaml if --first-tree was given.
	var tree *core.Tree
	if treeSlug != "" {
		tree = &core.Tree{
			Slug:      treeSlug,
			CreatedAt: time.Now().UTC(),
		}
		treeMetaPath := filepath.Join(decisionsDir, treeSlug, storage.TreeMetaFileName)
		if err := storage.WriteTree(treeMetaPath, tree); err != nil {
			return fmt.Errorf("init: write tree.yaml: %w", err)
		}
	}

	// Step 10: Open SQLite index and insert rows.
	indexPath := filepath.Join(decisionsDir, ".index.db")
	db, err := index.Open(indexPath)
	if err != nil {
		return fmt.Errorf("init: open index: %w", err)
	}
	defer db.Close()

	if err := insertActorRow(db, actor); err != nil {
		return fmt.Errorf("init: index actor: %w", err)
	}
	if tree != nil {
		if err := insertTreeRow(db, tree); err != nil {
			return fmt.Errorf("init: index tree: %w", err)
		}
	}

	// Step 11: Append audit events.
	actorEv := core.Event{
		Actor:  actor.Handle,
		Action: core.ActionActorAdd,
		Kind:   core.KindActor,
		ID:     actor.Handle,
		Payload: core.EventPayload{
			After: map[string]any{
				"handle": actor.Handle,
				"name":   actor.Name,
				"email":  actor.Email,
				"kind":   string(actor.Kind),
				"active": actor.Active,
			},
		},
	}
	if err := audit.Append(repoRoot, actorEv); err != nil {
		return fmt.Errorf("init: audit actor_add: %w", err)
	}

	if tree != nil {
		treeEv := core.Event{
			Actor:  actor.Handle,
			Action: core.ActionTreeCreate,
			Kind:   core.KindTree,
			ID:     tree.Slug,
			Payload: core.EventPayload{
				After: map[string]any{
					"slug":       tree.Slug,
					"created_at": tree.CreatedAt.Format(time.RFC3339),
				},
			},
		}
		// Repo-level audit.
		if err := audit.Append(repoRoot, treeEv); err != nil {
			return fmt.Errorf("init: audit tree_create (repo): %w", err)
		}
		// Tree-level audit.
		treeEv2 := treeEv
		treeEv2.Tree = tree.Slug
		if err := audit.Append(repoRoot, treeEv2); err != nil {
			return fmt.Errorf("init: audit tree_create (tree): %w", err)
		}
	}

	// Step 12: Print summary.
	fmt.Fprintf(cmd.OutOrStdout(), "Initialized .decisions/ at %s\n", repoRoot)
	fmt.Fprintf(cmd.OutOrStdout(), "  Actor: %s (%s %s)\n", actor.Handle, actor.Name, actor.Email)
	if tree != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "  Tree:  %s\n", tree.Slug)
	}
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprintln(cmd.OutOrStdout(), "Tip: create your first decision with `dtree new \"...\"`")

	return nil
}

// resolveInitActor determines which actor to register based on flags and
// (in interactive mode) prompts, using git config as defaults.
func resolveInitActor(
	cmd *cobra.Command,
	nonInteractive bool,
	actorHandle, actorName, actorEmail, actorKind string,
) (core.Actor, error) {
	// Read git config defaults (best-effort; failure is silently ignored).
	gitName := gitConfigValue("user.name")
	gitEmail := gitConfigValue("user.email")

	// Derive suggested handle from the local part of the git email.
	suggestedHandle := ""
	if gitEmail != "" {
		if idx := strings.Index(gitEmail, "@"); idx > 0 {
			suggestedHandle = gitEmail[:idx]
		}
	}

	if actorHandle != "" {
		// At least --actor-handle was supplied; use remaining flags or git defaults.
		name := actorName
		if name == "" {
			name = gitName
		}
		email := actorEmail
		if email == "" {
			email = gitEmail
		}
		kind := core.ActorKind(actorKind)
		if kind == "" {
			kind = core.ActorHuman
		}
		return core.Actor{
			Handle: actorHandle,
			Name:   name,
			Email:  email,
			Kind:   kind,
			Active: true,
		}, nil
	}

	if nonInteractive {
		return core.Actor{}, fmt.Errorf(
			"no --actor-handle provided; specify --actor-handle or omit --non-interactive for prompts",
		)
	}

	// Interactive mode: prompt the user for each field.
	reader := bufio.NewReader(cmd.InOrStdin())

	handle, err := prompt(reader, cmd.OutOrStdout(), "Handle", suggestedHandle)
	if err != nil {
		return core.Actor{}, fmt.Errorf("init: reading handle: %w", err)
	}
	if handle == "" {
		return core.Actor{}, fmt.Errorf("actor handle must not be empty")
	}

	name, err := prompt(reader, cmd.OutOrStdout(), "Name", gitName)
	if err != nil {
		return core.Actor{}, fmt.Errorf("init: reading name: %w", err)
	}

	email, err := prompt(reader, cmd.OutOrStdout(), "Email", gitEmail)
	if err != nil {
		return core.Actor{}, fmt.Errorf("init: reading email: %w", err)
	}

	kindStr, err := prompt(reader, cmd.OutOrStdout(), "Kind (human/agent)", "human")
	if err != nil {
		return core.Actor{}, fmt.Errorf("init: reading kind: %w", err)
	}
	if kindStr == "" {
		kindStr = "human"
	}

	return core.Actor{
		Handle: handle,
		Name:   name,
		Email:  email,
		Kind:   core.ActorKind(kindStr),
		Active: true,
	}, nil
}

// prompt writes "Label [default]: " and reads a trimmed line from r.
// Returns defaultVal when the user enters nothing.
func prompt(r *bufio.Reader, w io.Writer, label, defaultVal string) (string, error) {
	if defaultVal != "" {
		fmt.Fprintf(w, "%s [%s]: ", label, defaultVal)
	} else {
		fmt.Fprintf(w, "%s: ", label)
	}
	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return defaultVal, nil
	}
	return line, nil
}

// gitConfigValue reads a single git config key via os/exec.
// Returns "" on any failure (missing binary, key not set, etc.).
func gitConfigValue(key string) string {
	out, err := exec.Command("git", "config", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// insertActorRow inserts a into the SQLite actors table (INSERT OR IGNORE).
func insertActorRow(db *index.DB, a core.Actor) error {
	active := 0
	if a.Active {
		active = 1
	}
	_, err := db.Conn().Exec(
		`INSERT OR IGNORE INTO actors(handle, name, email, kind, active)
		 VALUES(?, ?, ?, ?, ?)`,
		a.Handle, a.Name, a.Email, string(a.Kind), active,
	)
	if err != nil {
		return fmt.Errorf("index: insert actor %s: %w", a.Handle, err)
	}
	return nil
}

// insertTreeRow inserts t into the SQLite trees table (INSERT OR IGNORE).
func insertTreeRow(db *index.DB, t *core.Tree) error {
	direction := t.Layout.Direction
	if direction == "" {
		direction = "TB"
	}
	archived := 0
	if t.Archived {
		archived = 1
	}
	sv := t.SchemaVersion
	if sv == 0 {
		sv = core.SchemaVersion
	}
	_, err := db.Conn().Exec(
		`INSERT OR IGNORE INTO trees(slug, title, description, archived, created_at, layout_direction, schema_version)
		 VALUES(?, ?, ?, ?, ?, ?, ?)`,
		t.Slug, t.Title, t.Description, archived,
		t.CreatedAt.UTC().Format(time.RFC3339), direction, sv,
	)
	if err != nil {
		return fmt.Errorf("index: insert tree %s: %w", t.Slug, err)
	}
	return nil
}
