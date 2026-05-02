// Package migrations provides the schema migration framework for the dtree
// index database. Each migration advances the schema from one version to the
// next. Migrations are registered at package init time and executed by the
// dtree migrate command.
//
// Usage:
//
//	applied, err := migrations.Default().Apply(db, index.CurrentSchemaVersion, false)
package migrations

import (
	"database/sql"
	"fmt"
	"sort"

	"github.com/cgould/dtree/internal/index"
)

// Migration is one schema version transition.
type Migration struct {
	From  int    // source schema version
	To    int    // target schema version (typically From+1)
	Name  string // human-friendly description, e.g. "add_recommended_by_column"
	Apply func(*sql.Tx) error // SQL DDL/DML to run inside a transaction
}

// Registry holds the ordered list of migrations available in this build.
// Populated by package init; access via All().
type Registry struct {
	migrations []Migration
}

// packageRegistry is the default package-level registry populated by Register.
var packageRegistry Registry

// Default returns the package-level registry (all migrations registered
// via Register() at init).
func Default() *Registry {
	return &packageRegistry
}

// Register adds a migration to the default package-level registry. Panics if
// (From, To) duplicates an existing entry. Called from migration files' init().
func Register(m Migration) {
	packageRegistry.Register(m)
}

// Register adds a migration to this registry. Panics if (From, To) duplicates
// an existing entry. Can be used on local registry instances in tests to avoid
// polluting the package-level Default() registry.
func (r *Registry) Register(m Migration) {
	for _, existing := range r.migrations {
		if existing.From == m.From && existing.To == m.To {
			panic(fmt.Sprintf("migrations: duplicate registration for %d→%d", m.From, m.To))
		}
	}
	r.migrations = append(r.migrations, m)
}

// All returns migrations sorted by From ascending.
func (r *Registry) All() []Migration {
	out := make([]Migration, len(r.migrations))
	copy(out, r.migrations)
	sort.Slice(out, func(i, j int) bool {
		return out[i].From < out[j].From
	})
	return out
}

// Plan returns the migrations that need to apply to bring current up to
// target. Returns an error if:
//   - current > target (downgrades not supported)
//   - there is a gap in the migration chain (e.g. current=1, no 1→2 step when target=3)
//
// Returns a nil slice if already at target.
func (r *Registry) Plan(current, target int) ([]Migration, error) {
	if current > target {
		return nil, fmt.Errorf("migrations: downgrade not supported (current=%d > target=%d)", current, target)
	}
	if current == target {
		return nil, nil
	}

	// Build a lookup table: From → Migration.
	byFrom := make(map[int]Migration, len(r.migrations))
	for _, m := range r.migrations {
		byFrom[m.From] = m
	}

	var plan []Migration
	v := current
	for v < target {
		m, ok := byFrom[v]
		if !ok {
			return nil, fmt.Errorf("migrations: no migration from version %d (needed to reach %d)", v, target)
		}
		plan = append(plan, m)
		v = m.To
	}
	if v != target {
		return nil, fmt.Errorf("migrations: migration chain ended at %d, not at target %d", v, target)
	}
	return plan, nil
}

// Apply runs the planned migrations on db, advancing schema_version after each
// successful step. Each step runs in its own transaction; failure of step N
// leaves schema_version at N-1 (as the SetSchemaVersion call for that step is
// inside the same transaction as the migration Apply func).
//
// dryRun=true returns the planned migrations without executing them.
//
// Returns the list of migrations that were applied (or would be, in dry-run).
func (r *Registry) Apply(db *index.DB, target int, dryRun bool) ([]Migration, error) {
	current, err := db.SchemaVersion()
	if err != nil {
		return nil, fmt.Errorf("migrations: read current schema_version: %w", err)
	}

	plan, err := r.Plan(current, target)
	if err != nil {
		return nil, err
	}
	if dryRun || len(plan) == 0 {
		return plan, nil
	}

	for _, m := range plan {
		if err := applyOne(db, m); err != nil {
			return nil, fmt.Errorf("migrations: applying %q (%d→%d): %w", m.Name, m.From, m.To, err)
		}
	}
	return plan, nil
}

// applyOne runs a single migration in its own transaction and updates
// schema_version on success.
func applyOne(db *index.DB, m Migration) error {
	tx, err := db.Conn().Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		// Only reached if we haven't committed; rollback is a no-op after commit.
		_ = tx.Rollback()
	}()

	if err := m.Apply(tx); err != nil {
		return fmt.Errorf("apply func: %w", err)
	}

	// Update schema_version inside the same transaction so the version and the
	// schema change are atomic.
	_, err = tx.Exec(
		`INSERT INTO _meta(key, value) VALUES('schema_version', ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		m.To,
	)
	if err != nil {
		return fmt.Errorf("set schema_version=%d: %w", m.To, err)
	}

	return tx.Commit()
}
