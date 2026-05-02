package cli

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/config"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/identity"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/storage"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// newActorCommand returns the `dtree actor` command and all subcommands.
func newActorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "actor",
		Short: "Manage registered actors (humans and agents)",
		Long:  "Add, list, show, rename, archive, and link actors in the project's actors.yaml registry.",
	}

	cmd.AddCommand(newActorAddCommand())
	cmd.AddCommand(newActorListCommand())
	cmd.AddCommand(newActorShowCommand())
	cmd.AddCommand(newActorRenameCommand())
	cmd.AddCommand(newActorArchiveCommand())
	cmd.AddCommand(newActorLinkCommand())

	return cmd
}

// actorRequireDecisionsDir returns an error if .decisions/ does not exist.
func actorRequireDecisionsDir(repoRoot string) error {
	decisionsDir := filepath.Join(repoRoot, ".decisions")
	info, err := os.Stat(decisionsDir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("no .decisions/ found at %s; run `dtree init` first", repoRoot)
	}
	return nil
}

// ── actor add ────────────────────────────────────────────────────────────────

func newActorAddCommand() *cobra.Command {
	var (
		nameFlag  string
		emailFlag string
		kindFlag  string
	)

	cmd := &cobra.Command{
		Use:   "add <handle>",
		Short: "Register a new actor in this project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			if err := actorRequireDecisionsDir(repoRoot); err != nil {
				return err
			}

			handle := args[0]
			kind := core.ActorKind(kindFlag)
			if kind == "" {
				kind = core.ActorHuman
			}

			cfg, err := config.Load(repoRoot)
			if err != nil {
				return err
			}
			resolver := identity.NewResolver(repoRoot, cfg)

			actor := core.Actor{
				Handle: handle,
				Name:   nameFlag,
				Email:  emailFlag,
				Kind:   kind,
				Active: true,
			}

			if err := resolver.AddActor(actor); err != nil {
				return err
			}

			// Update index actors table.
			db, err := actorOpenIndex(repoRoot)
			if err != nil {
				return err
			}
			defer db.Close()

			if err := actorUpsertRow(db, actor); err != nil {
				return err
			}

			// Emit actor_add audit event (repo-level).
			ev := core.Event{
				Actor:  handle,
				Action: core.ActionActorAdd,
				Kind:   core.KindActor,
				ID:     handle,
				Payload: core.EventPayload{
					After: map[string]any{
						"handle": handle,
						"name":   nameFlag,
						"email":  emailFlag,
						"kind":   string(kind),
						"active": true,
					},
				},
			}
			if err := audit.Append(repoRoot, ev); err != nil {
				return fmt.Errorf("actor add: audit: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Added actor %s\n", handle)
			return nil
		},
	}

	cmd.Flags().StringVar(&nameFlag, "name", "", "Display name")
	cmd.Flags().StringVar(&emailFlag, "email", "", "Email address")
	cmd.Flags().StringVar(&kindFlag, "kind", "human", "Actor kind: human or agent")

	return cmd
}

// ── actor list ───────────────────────────────────────────────────────────────

func newActorListCommand() *cobra.Command {
	var includeArchived bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List registered actors",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			outputFlag, _ := cmd.Root().PersistentFlags().GetString("output")

			if err := actorRequireDecisionsDir(repoRoot); err != nil {
				return err
			}

			cfg, err := config.Load(repoRoot)
			if err != nil {
				return err
			}
			resolver := identity.NewResolver(repoRoot, cfg)

			af, err := resolver.LoadActors()
			if err != nil {
				return err
			}

			actors := af.Actors
			if !includeArchived {
				var active []core.Actor
				for _, a := range actors {
					if a.Active {
						active = append(active, a)
					}
				}
				actors = active
			}

			format := actorResolveFormat(outputFlag)
			return actorPrintList(cmd, actors, format)
		},
	}

	cmd.Flags().BoolVar(&includeArchived, "include-archived", false, "Include archived (inactive) actors")
	return cmd
}

// ── actor show ───────────────────────────────────────────────────────────────

func newActorShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <handle>",
		Short: "Show a single actor's details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			outputFlag, _ := cmd.Root().PersistentFlags().GetString("output")

			if err := actorRequireDecisionsDir(repoRoot); err != nil {
				return err
			}

			handle := args[0]
			cfg, err := config.Load(repoRoot)
			if err != nil {
				return err
			}
			resolver := identity.NewResolver(repoRoot, cfg)

			actor, err := resolver.FindActor(handle)
			if err != nil {
				return err
			}
			if actor == nil {
				return fmt.Errorf("actor not found: %q", handle)
			}

			format := actorResolveFormat(outputFlag)
			return actorPrintList(cmd, []core.Actor{*actor}, format)
		},
	}

	return cmd
}

// ── actor rename ─────────────────────────────────────────────────────────────

func newActorRenameCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rename <old-handle> <new-handle>",
		Short: "Rename an actor and rewrite references across decisions",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")

			if err := actorRequireDecisionsDir(repoRoot); err != nil {
				return err
			}

			oldHandle := args[0]
			newHandle := args[1]

			cfg, err := config.Load(repoRoot)
			if err != nil {
				return err
			}
			resolver := identity.NewResolver(repoRoot, cfg)

			if err := resolver.RenameActor(oldHandle, newHandle); err != nil {
				return err
			}

			// Rewrite references in decisions (index + YAML).
			db, err := actorOpenIndex(repoRoot)
			if err != nil {
				return err
			}
			defer db.Close()

			refsRewritten, err := actorRewriteDecisionRefs(repoRoot, db, oldHandle, newHandle)
			if err != nil {
				return fmt.Errorf("actor rename: rewrite refs: %w", err)
			}

			// Update the actors row in the index.
			if err := actorRenameRow(db, oldHandle, newHandle); err != nil {
				return fmt.Errorf("actor rename: index actors: %w", err)
			}

			// Emit actor_rename audit event.
			ev := core.Event{
				Actor:  newHandle,
				Action: core.ActionActorRename,
				Kind:   core.KindActor,
				ID:     newHandle,
				Payload: core.EventPayload{
					After: map[string]any{
						"from":           oldHandle,
						"to":             newHandle,
						"refs_rewritten": refsRewritten,
					},
				},
			}
			if err := audit.Append(repoRoot, ev); err != nil {
				return fmt.Errorf("actor rename: audit: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Renamed %s → %s; updated %d decision references.\n",
				oldHandle, newHandle, refsRewritten)
			return nil
		},
	}

	return cmd
}

// ── actor archive ─────────────────────────────────────────────────────────────

func newActorArchiveCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "archive <handle>",
		Short: "Archive (deactivate) an actor",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")

			if err := actorRequireDecisionsDir(repoRoot); err != nil {
				return err
			}

			handle := args[0]
			cfg, err := config.Load(repoRoot)
			if err != nil {
				return err
			}
			resolver := identity.NewResolver(repoRoot, cfg)

			if err := resolver.ArchiveActor(handle); err != nil {
				return err
			}

			// Update index.
			db, err := actorOpenIndex(repoRoot)
			if err != nil {
				return err
			}
			defer db.Close()

			if err := actorArchiveRow(db, handle); err != nil {
				return fmt.Errorf("actor archive: index: %w", err)
			}

			// Emit actor_archive audit event.
			ev := core.Event{
				Actor:  handle,
				Action: core.ActionActorArchive,
				Kind:   core.KindActor,
				ID:     handle,
				Payload: core.EventPayload{
					After: map[string]any{
						"handle": handle,
						"active": false,
					},
				},
			}
			if err := audit.Append(repoRoot, ev); err != nil {
				return fmt.Errorf("actor archive: audit: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Archived actor %s\n", handle)
			return nil
		},
	}

	return cmd
}

// ── actor link ────────────────────────────────────────────────────────────────

func newActorLinkCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "link <handle>",
		Short: "Adopt an actor from the global identity catalog into this project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")

			if err := actorRequireDecisionsDir(repoRoot); err != nil {
				return err
			}

			handle := args[0]
			cfg, err := config.Load(repoRoot)
			if err != nil {
				return err
			}

			// Look up handle in global catalog.
			catalog := config.IdentityCatalog(cfg)
			var globalActor *core.Actor
			for i := range catalog {
				if catalog[i].Handle == handle {
					a := catalog[i]
					globalActor = &a
					break
				}
			}
			if globalActor == nil {
				return fmt.Errorf("no such global identity: %q", handle)
			}

			resolver := identity.NewResolver(repoRoot, cfg)

			// Check if already registered.
			existing, err := resolver.FindActor(handle)
			if err != nil {
				return err
			}
			if existing != nil {
				return fmt.Errorf("already registered: actor %q is already in this project's actors.yaml", handle)
			}

			// Copy the global actor into the project registry.
			actor := *globalActor
			actor.Active = true
			if err := resolver.AddActor(actor); err != nil {
				return err
			}

			// Update index.
			db, err := actorOpenIndex(repoRoot)
			if err != nil {
				return err
			}
			defer db.Close()

			if err := actorUpsertRow(db, actor); err != nil {
				return err
			}

			// Emit actor_add audit event.
			ev := core.Event{
				Actor:  handle,
				Action: core.ActionActorAdd,
				Kind:   core.KindActor,
				ID:     handle,
				Payload: core.EventPayload{
					After: map[string]any{
						"handle": actor.Handle,
						"name":   actor.Name,
						"email":  actor.Email,
						"kind":   string(actor.Kind),
						"active": true,
						"source": "global_catalog",
					},
				},
			}
			if err := audit.Append(repoRoot, ev); err != nil {
				return fmt.Errorf("actor link: audit: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Linked actor %s from global catalog\n", handle)
			return nil
		},
	}

	return cmd
}

// ── output helpers ────────────────────────────────────────────────────────────

// actorResolveFormat returns the effective output format, defaulting to "human".
func actorResolveFormat(flag string) string {
	if flag != "" {
		return flag
	}
	if isTTY() {
		return "human"
	}
	return "json"
}

// actorPrintList writes actors to cmd's stdout in the requested format.
func actorPrintList(cmd *cobra.Command, actors []core.Actor, format string) error {
	switch format {
	case "json":
		return actorPrintJSON(cmd, actors)
	case "yaml":
		return actorPrintYAML(cmd, actors)
	default:
		return actorPrintHuman(cmd, actors)
	}
}

func actorPrintHuman(cmd *cobra.Command, actors []core.Actor) error {
	if len(actors) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "(no actors)")
		return nil
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "HANDLE\tNAME\tEMAIL\tKIND\tACTIVE")
	for _, a := range actors {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%v\n",
			a.Handle, a.Name, a.Email, a.Kind, a.Active)
	}
	return w.Flush()
}

func actorPrintJSON(cmd *cobra.Command, actors []core.Actor) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(actors)
}

func actorPrintYAML(cmd *cobra.Command, actors []core.Actor) error {
	enc := yaml.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent(2)
	if err := enc.Encode(actors); err != nil {
		return err
	}
	return enc.Close()
}

// ── index helpers ─────────────────────────────────────────────────────────────

// actorOpenIndex opens the SQLite index for the given repo root.
func actorOpenIndex(repoRoot string) (*index.DB, error) {
	indexPath := filepath.Join(repoRoot, ".decisions", ".index.db")
	db, err := index.Open(indexPath)
	if err != nil {
		return nil, fmt.Errorf("actor: open index: %w", err)
	}
	return db, nil
}

// actorUpsertRow inserts or replaces an actor row in the index.
func actorUpsertRow(db *index.DB, a core.Actor) error {
	active := 0
	if a.Active {
		active = 1
	}
	_, err := db.Conn().Exec(
		`INSERT INTO actors(handle, name, email, kind, active)
		 VALUES(?, ?, ?, ?, ?)
		 ON CONFLICT(handle) DO UPDATE SET
		   name=excluded.name, email=excluded.email,
		   kind=excluded.kind, active=excluded.active`,
		a.Handle, a.Name, a.Email, string(a.Kind), active,
	)
	if err != nil {
		return fmt.Errorf("index: upsert actor %s: %w", a.Handle, err)
	}
	return nil
}

// actorRenameRow updates the handle column in the actors table.
func actorRenameRow(db *index.DB, oldHandle, newHandle string) error {
	_, err := db.Conn().Exec(
		`UPDATE actors SET handle = ? WHERE handle = ?`,
		newHandle, oldHandle,
	)
	if err != nil {
		return fmt.Errorf("index: rename actor %s→%s: %w", oldHandle, newHandle, err)
	}
	return nil
}

// actorArchiveRow sets active=0 for the given handle in the index.
func actorArchiveRow(db *index.DB, handle string) error {
	_, err := db.Conn().Exec(
		`UPDATE actors SET active = 0 WHERE handle = ?`, handle,
	)
	if err != nil {
		return fmt.Errorf("index: archive actor %s: %w", handle, err)
	}
	return nil
}

// actorRewriteDecisionRefs updates creator, assignee, recommended_by columns
// and the decision_deciders junction table in the index, then rewrites the
// affected YAML files on disk. Returns the number of decisions touched.
func actorRewriteDecisionRefs(repoRoot string, db *index.DB, oldHandle, newHandle string) (int, error) {
	// Collect decision IDs that reference oldHandle.
	affectedIDs, err := actorFindDecisionsWithHandle(db, oldHandle)
	if err != nil {
		return 0, err
	}

	if len(affectedIDs) == 0 {
		return 0, nil
	}

	// Update decisions table columns.
	for _, stmt := range []string{
		`UPDATE decisions SET creator        = ? WHERE creator        = ?`,
		`UPDATE decisions SET assignee       = ? WHERE assignee       = ?`,
		`UPDATE decisions SET recommended_by = ? WHERE recommended_by = ?`,
	} {
		if _, err := db.Conn().Exec(stmt, newHandle, oldHandle); err != nil {
			return 0, fmt.Errorf("index: update decisions handle %s→%s: %w", oldHandle, newHandle, err)
		}
	}

	// Update decision_deciders junction table.
	if _, err := db.Conn().Exec(
		`UPDATE decision_deciders SET handle = ? WHERE handle = ?`,
		newHandle, oldHandle,
	); err != nil {
		return 0, fmt.Errorf("index: update decision_deciders %s→%s: %w", oldHandle, newHandle, err)
	}

	// Rewrite YAML files on disk.
	rewritten := 0
	for _, id := range affectedIDs {
		path, err := actorDecisionYAMLPath(repoRoot, db, id)
		if err != nil || path == "" {
			continue
		}

		d, err := storage.ReadDecision(path)
		if err != nil {
			return rewritten, fmt.Errorf("rewrite decision YAML %s: %w", id, err)
		}

		changed := false
		if d.Creator == oldHandle {
			d.Creator = newHandle
			changed = true
		}
		if d.Assignee == oldHandle {
			d.Assignee = newHandle
			changed = true
		}
		if d.RecommendedBy == oldHandle {
			d.RecommendedBy = newHandle
			changed = true
		}
		for i, v := range d.DecidedBy {
			if v == oldHandle {
				d.DecidedBy[i] = newHandle
				changed = true
			}
		}

		if changed {
			if err := storage.WriteDecision(path, d); err != nil {
				return rewritten, fmt.Errorf("write decision YAML %s: %w", id, err)
			}
			rewritten++
		}
	}
	return rewritten, nil
}

// actorFindDecisionsWithHandle returns IDs of decisions that reference handle
// in creator, assignee, recommended_by, or decision_deciders.
func actorFindDecisionsWithHandle(db *index.DB, handle string) ([]string, error) {
	const q = `
		SELECT DISTINCT d.id
		FROM decisions d
		LEFT JOIN decision_deciders dd ON dd.decision_id = d.id
		WHERE d.creator = ? OR d.assignee = ? OR d.recommended_by = ? OR dd.handle = ?
	`
	rows, err := db.Conn().Query(q, handle, handle, handle, handle)
	if err != nil {
		return nil, fmt.Errorf("index: find decisions with handle %s: %w", handle, err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// actorDecisionYAMLPath reconstructs the on-disk path for a decision by
// querying the index for its tree and slug.
func actorDecisionYAMLPath(repoRoot string, db *index.DB, id string) (string, error) {
	var tree, slug string
	err := db.Conn().QueryRow(
		`SELECT tree, slug FROM decisions WHERE id = ?`, id,
	).Scan(&tree, &slug)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("index: lookup decision path for %s: %w", id, err)
	}
	path := storage.DecisionPath(
		filepath.Join(repoRoot, ".decisions", tree),
		id, slug,
	)
	return path, nil
}
