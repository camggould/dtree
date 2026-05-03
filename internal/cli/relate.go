package cli

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/concurrency"
	"github.com/cgould/dtree/internal/config"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/fsutil"
	"github.com/cgould/dtree/internal/identity"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/storage"
	"github.com/spf13/cobra"
)

// allowedRelateTypes are the relationship types that may be created via
// `dtree relate`. supersedes / superseded_by are managed by `dtree supersede`
// because they imply lifecycle changes on both endpoints.
var allowedRelateTypes = map[string]core.RelationshipType{
	string(core.RelBlocks):     core.RelBlocks,
	string(core.RelInfluences): core.RelInfluences,
	string(core.RelRelatesTo):  core.RelRelatesTo,
}

// supersedeReservedTypes are types that must not flow through relate/unrelate.
var supersedeReservedTypes = map[string]bool{
	"supersedes":     true,
	"superseded_by":  true,
}

// newRelateCommand returns the `dtree relate` command.
func newRelateCommand() *cobra.Command {
	var (
		note   string
		asFlag string
	)

	cmd := &cobra.Command{
		Use:   "relate <src-id> <type> <target-id>",
		Short: "Add a relationship between two decisions",
		Long: "Add a directed relationship of the given type from src to target. " +
			"Allowed types: blocks, influences, relates_to. " +
			"Refuses to create cycles in the blocks graph.",
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			rawSrc := args[0]
			relTypeStr := args[1]
			rawTarget := args[2]
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")

			if err := requireDecisionsDir(repoRoot); err != nil {
				return err
			}

			// Reject supersedes / superseded_by upfront.
			if supersedeReservedTypes[relTypeStr] {
				return fmt.Errorf(
					"relationships of type '%s' must be created via 'dtree supersede'",
					relTypeStr,
				)
			}

			relType, ok := allowedRelateTypes[relTypeStr]
			if !ok {
				return fmt.Errorf(
					"invalid relationship type %q: must be one of blocks, influences, relates_to",
					relTypeStr,
				)
			}

			db, err := openIndex(repoRoot)
			if err != nil {
				return fmt.Errorf("relate: open index: %w", err)
			}
			defer db.Close()

			srcID, err := resolveDecisionIDPrefix(db, rawSrc)
			if err != nil {
				return fmt.Errorf("relate: resolve src: %w", err)
			}
			targetID, err := resolveDecisionIDPrefix(db, rawTarget)
			if err != nil {
				return fmt.Errorf("relate: resolve target: %w", err)
			}

			if srcID == targetID {
				return fmt.Errorf("relate: cannot relate decision to itself")
			}

			// Cycle check for blocks.
			if relType == core.RelBlocks {
				chain, cycle, err := findBlocksPath(db, targetID, srcID)
				if err != nil {
					return fmt.Errorf("relate: cycle check: %w", err)
				}
				if cycle {
					// Build readable chain: src -> target -> ... -> src
					full := append([]string{srcID, targetID}, chain[1:]...)
					return fmt.Errorf("cycle detected: %s", strings.Join(shortenIDs(full), " -> "))
				}
			}

			// Load src decision from disk (file is source of truth).
			srcPath, err := decisionYAMLPath(repoRoot, db, srcID)
			if err != nil {
				return fmt.Errorf("relate: locate src file: %w", err)
			}
			d, err := storage.ReadDecision(srcPath)
			if err != nil {
				return fmt.Errorf("relate: read src decision: %w", err)
			}

			// Idempotency: check if (src, type, target) already exists.
			for _, r := range d.Relationships {
				if r.Type == relType && r.Target == targetID {
					fmt.Fprintln(cmd.OutOrStdout(), "already exists")
					return nil
				}
			}

			// Resolve identity for audit.
			cfg, err := config.Load(repoRoot)
			if err != nil {
				return fmt.Errorf("relate: load config: %w", err)
			}
			res, err := identity.NewResolver(repoRoot, cfg).MustResolve(asFlag)
			if err != nil {
				return fmt.Errorf("relate: resolve identity: %w", err)
			}

			// Get expected rev for optimistic concurrency.
			expectedRev, err := index.GetDecisionRev(db, srcID)
			if err != nil {
				return fmt.Errorf("relate: get rev: %w", err)
			}

			// Append relationship and write file.
			d.Relationships = append(d.Relationships, core.Relationship{
				Type:   relType,
				Target: targetID,
			})
			if err := storage.WriteDecision(srcPath, d); err != nil {
				return fmt.Errorf("relate: write src decision: %w", err)
			}

			// Recompute content sha and update index with new rev.
			contentSha, err := fsutil.Sha256File(srcPath)
			if err != nil {
				return fmt.Errorf("relate: sha256: %w", err)
			}
			newRev := concurrency.NewRev()
			if err := index.UpdateDecisionWithExpectedRev(db, d, contentSha, expectedRev, newRev); err != nil {
				return fmt.Errorf("relate: update index: %w", err)
			}

			// Append audit event.
			meta := map[string]any{
				"src":    srcID,
				"type":   string(relType),
				"target": targetID,
			}
			if note != "" {
				meta["note"] = note
			}
			ev := core.Event{
				Actor:  res.Handle,
				Action: core.ActionRelate,
				Kind:   core.KindRelationship,
				Tree:   d.Tree,
				ID:     srcID,
				Payload: core.EventPayload{
					Extra: meta,
				},
			}
			if err := audit.Append(repoRoot, ev); err != nil {
				return fmt.Errorf("relate: audit event: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Related %s -[%s]-> %s\n",
				shortenID(srcID), relType, shortenID(targetID))
			return nil
		},
	}

	cmd.Flags().StringVar(&note, "note", "", "Optional note attached to the relationship event")
	cmd.Flags().StringVar(&asFlag, "as", "", "Override identity handle for this invocation")
	return cmd
}

// resolveDecisionIDPrefix resolves a (possibly partial) ULID against the
// decisions table. The full 26-char ID matches exactly; shorter strings are
// treated as a case-insensitive prefix and must match exactly one row.
func resolveDecisionIDPrefix(db *index.DB, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty decision id")
	}
	upper := strings.ToUpper(raw)

	if len(upper) == 26 {
		// Full ID — must exist (and not be soft-deleted).
		var id string
		err := db.Conn().QueryRow(
			`SELECT id FROM decisions WHERE id = ? AND deleted = 0`, upper,
		).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("decision %s not found", raw)
		}
		if err != nil {
			return "", fmt.Errorf("lookup decision %s: %w", raw, err)
		}
		return id, nil
	}

	rows, err := db.Conn().Query(
		`SELECT id FROM decisions WHERE id LIKE ? AND deleted = 0 LIMIT 2`,
		upper+"%",
	)
	if err != nil {
		return "", fmt.Errorf("lookup decision prefix %s: %w", raw, err)
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
	if err := rows.Err(); err != nil {
		return "", err
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("decision %s not found", raw)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("decision id prefix %q is ambiguous", raw)
	}
}

// findBlocksPath does a BFS over the blocks relationship graph starting at
// from, looking for to. Returns the chain (from -> ... -> to) when found.
// The chain always begins with `from`.
func findBlocksPath(db *index.DB, from, to string) (chain []string, found bool, err error) {
	if from == to {
		return []string{from}, true, nil
	}
	visited := map[string]bool{from: true}
	parent := map[string]string{}
	queue := []string{from}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		rows, err := db.Conn().Query(
			`SELECT target FROM relationships WHERE source = ? AND type = 'blocks'`,
			curr,
		)
		if err != nil {
			return nil, false, err
		}
		var nexts []string
		for rows.Next() {
			var t string
			if err := rows.Scan(&t); err != nil {
				rows.Close()
				return nil, false, err
			}
			nexts = append(nexts, t)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, false, err
		}

		for _, next := range nexts {
			if visited[next] {
				continue
			}
			visited[next] = true
			parent[next] = curr
			if next == to {
				// Reconstruct path from->...->to.
				path := []string{to}
				for n := to; n != from; {
					p := parent[n]
					path = append([]string{p}, path...)
					n = p
				}
				return path, true, nil
			}
			queue = append(queue, next)
		}
	}
	return nil, false, nil
}

// decisionYAMLPath looks up the on-disk YAML file for a decision id.
func decisionYAMLPath(repoRoot string, db *index.DB, id string) (string, error) {
	var tree, slug string
	err := db.Conn().QueryRow(
		`SELECT tree, slug FROM decisions WHERE id = ?`, id,
	).Scan(&tree, &slug)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("decision %s not found", id)
	}
	if err != nil {
		return "", err
	}
	return storage.DecisionPath(filepath.Join(repoRoot, ".decisions", tree), id, slug), nil
}

// shortenID returns the first 8 chars of an id (or the whole thing if shorter).
func shortenID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// shortenIDs maps shortenID over a slice.
func shortenIDs(ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = shortenID(id)
	}
	return out
}
