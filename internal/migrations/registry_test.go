package migrations_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/migrations"
)

// openTestDB opens a fresh index DB in a temp dir and registers cleanup.
func openTestDB(t *testing.T) *index.DB {
	t.Helper()
	db, err := index.Open(filepath.Join(t.TempDir(), ".index.db"))
	if err != nil {
		t.Fatalf("index.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newRegistry returns an empty, isolated Registry for test use (does not share
// state with the package-level Default() registry).
func newRegistry() *migrations.Registry {
	return &migrations.Registry{}
}

// noop is a migration Apply func that succeeds without doing anything.
func noop(_ *sql.Tx) error { return nil }

// TestRegisterAndAll verifies that registering two migrations and calling All()
// returns them sorted by From ascending.
func TestRegisterAndAll(t *testing.T) {
	r := &migrations.Registry{}
	m1 := migrations.Migration{From: 1, To: 2, Name: "one_to_two", Apply: noop}
	m0 := migrations.Migration{From: 0, To: 1, Name: "zero_to_one", Apply: noop}
	r.Register(m1)
	r.Register(m0)

	all := r.All()
	if len(all) != 2 {
		t.Fatalf("All() returned %d migrations, want 2", len(all))
	}
	if all[0].From != 0 || all[1].From != 1 {
		t.Errorf("All() not sorted by From: got [%d, %d]", all[0].From, all[1].From)
	}
}

// TestRegisterDuplicatePanics ensures registering the same (From, To) pair
// panics.
func TestRegisterDuplicatePanics(t *testing.T) {
	r := &migrations.Registry{}
	r.Register(migrations.Migration{From: 0, To: 1, Name: "a", Apply: noop})
	defer func() {
		if rec := recover(); rec == nil {
			t.Error("expected panic on duplicate (From, To), but did not panic")
		}
	}()
	r.Register(migrations.Migration{From: 0, To: 1, Name: "b", Apply: noop})
}

// TestPlanNoOp verifies that Plan returns nil when current == target.
func TestPlanNoOp(t *testing.T) {
	r := &migrations.Registry{}
	plan, err := r.Plan(2, 2)
	if err != nil {
		t.Fatalf("Plan(2,2): unexpected error: %v", err)
	}
	if plan != nil {
		t.Errorf("Plan(2,2) = %v, want nil", plan)
	}
}

// TestPlanSequential verifies that Plan returns both steps in order when going
// from 0 to 2 with migrations 0→1 and 1→2 registered.
func TestPlanSequential(t *testing.T) {
	r := &migrations.Registry{}
	r.Register(migrations.Migration{From: 0, To: 1, Name: "zero_to_one", Apply: noop})
	r.Register(migrations.Migration{From: 1, To: 2, Name: "one_to_two", Apply: noop})

	plan, err := r.Plan(0, 2)
	if err != nil {
		t.Fatalf("Plan(0,2): %v", err)
	}
	if len(plan) != 2 {
		t.Fatalf("Plan(0,2) = %d steps, want 2", len(plan))
	}
	if plan[0].From != 0 || plan[1].From != 1 {
		t.Errorf("unexpected plan order: [%d→%d, %d→%d]",
			plan[0].From, plan[0].To, plan[1].From, plan[1].To)
	}
}

// TestPlanGapDetection verifies that Plan returns an error mentioning the
// missing step when the migration chain has a gap.
func TestPlanGapDetection(t *testing.T) {
	r := &migrations.Registry{}
	r.Register(migrations.Migration{From: 0, To: 1, Name: "zero_to_one", Apply: noop})
	// No 1→2 step registered.

	_, err := r.Plan(0, 2)
	if err == nil {
		t.Fatal("Plan(0,2) with gap: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "1") {
		t.Errorf("error should mention missing version 1, got: %v", err)
	}
}

// TestPlanDowngradeRejected verifies that Plan returns an error when current >
// target.
func TestPlanDowngradeRejected(t *testing.T) {
	r := &migrations.Registry{}
	_, err := r.Plan(2, 1)
	if err == nil {
		t.Fatal("Plan(2,1): expected error for downgrade, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "downgrade") {
		t.Errorf("error should mention downgrade, got: %v", err)
	}
}

// TestApplyDryRunNoChange verifies that dryRun=true returns the plan but does
// not modify schema_version.
func TestApplyDryRunNoChange(t *testing.T) {
	db := openTestDB(t)

	// Manually set schema_version to 0 so there's a migration to plan.
	if err := db.SetSchemaVersion(0); err != nil {
		t.Fatalf("SetSchemaVersion: %v", err)
	}

	r := &migrations.Registry{}
	r.Register(migrations.Migration{From: 0, To: 1, Name: "zero_to_one", Apply: noop})

	applied, err := r.Apply(db, 1, true /* dryRun */)
	if err != nil {
		t.Fatalf("Apply(dryRun): %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("Apply(dryRun) returned %d steps, want 1", len(applied))
	}

	// schema_version must still be 0.
	v, err := db.SchemaVersion()
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if v != 0 {
		t.Errorf("schema_version after dry-run = %d, want 0", v)
	}
}

// TestApplyAdvancesVersion verifies that Apply updates schema_version after each
// step.
func TestApplyAdvancesVersion(t *testing.T) {
	db := openTestDB(t)
	if err := db.SetSchemaVersion(0); err != nil {
		t.Fatalf("SetSchemaVersion: %v", err)
	}

	r := &migrations.Registry{}
	r.Register(migrations.Migration{From: 0, To: 1, Name: "zero_to_one", Apply: noop})
	r.Register(migrations.Migration{From: 1, To: 2, Name: "one_to_two", Apply: noop})

	applied, err := r.Apply(db, 2, false)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(applied) != 2 {
		t.Fatalf("Apply returned %d steps, want 2", len(applied))
	}

	v, err := db.SchemaVersion()
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if v != 2 {
		t.Errorf("schema_version = %d, want 2", v)
	}
}

// TestApplyTransactional verifies that a migration whose Apply func returns an
// error leaves schema_version unchanged.
func TestApplyTransactional(t *testing.T) {
	db := openTestDB(t)
	if err := db.SetSchemaVersion(0); err != nil {
		t.Fatalf("SetSchemaVersion: %v", err)
	}

	errBoom := errors.New("boom")
	r := &migrations.Registry{}
	r.Register(migrations.Migration{
		From:  0,
		To:    1,
		Name:  "zero_to_one_failing",
		Apply: func(_ *sql.Tx) error { return errBoom },
	})

	_, err := r.Apply(db, 1, false)
	if err == nil {
		t.Fatal("Apply with failing migration: expected error, got nil")
	}

	v, err := db.SchemaVersion()
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if v != 0 {
		t.Errorf("schema_version after failed migration = %d, want 0", v)
	}
}

// TestApplyRunsInOrder registers migrations out of order and verifies they are
// applied in From-order (0→1 before 1→2).
func TestApplyRunsInOrder(t *testing.T) {
	db := openTestDB(t)
	if err := db.SetSchemaVersion(0); err != nil {
		t.Fatalf("SetSchemaVersion: %v", err)
	}

	var order []int
	r := &migrations.Registry{}
	// Register in reverse order.
	r.Register(migrations.Migration{
		From: 1,
		To:   2,
		Name: "one_to_two",
		Apply: func(_ *sql.Tx) error {
			order = append(order, 1)
			return nil
		},
	})
	r.Register(migrations.Migration{
		From: 0,
		To:   1,
		Name: "zero_to_one",
		Apply: func(_ *sql.Tx) error {
			order = append(order, 0)
			return nil
		},
	})

	if _, err := r.Apply(db, 2, false); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if len(order) != 2 || order[0] != 0 || order[1] != 1 {
		t.Errorf("migrations applied in wrong order: %v, want [0 1]", order)
	}
}

// TestV0ToV1Idempotent verifies that running the v0→v1 migration on a fresh DB
// (already at schema_version=1) is a no-op and returns no error.
func TestV0ToV1Idempotent(t *testing.T) {
	db := openTestDB(t)

	// Fresh DB opened with Open() is already at version 1.
	v, err := db.SchemaVersion()
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if v != 1 {
		t.Fatalf("precondition: schema_version = %d, want 1", v)
	}

	// Manually set to 0 to simulate a legacy DB, then apply the default
	// registry (which includes v0→v1).
	if err := db.SetSchemaVersion(0); err != nil {
		t.Fatalf("SetSchemaVersion(0): %v", err)
	}

	applied, err := migrations.Default().Apply(db, 1, false)
	if err != nil {
		t.Fatalf("Apply v0→v1: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("Apply returned %d steps, want 1", len(applied))
	}

	v, err = db.SchemaVersion()
	if err != nil {
		t.Fatalf("SchemaVersion after apply: %v", err)
	}
	if v != 1 {
		t.Errorf("schema_version = %d, want 1", v)
	}
}

// TestNeedsMigrationFreshDb verifies that a freshly-opened DB reports no
// migration needed.
func TestNeedsMigrationFreshDb(t *testing.T) {
	db := openTestDB(t)
	current, target, needed := db.NeedsMigration()
	if needed {
		t.Errorf("NeedsMigration() = (%d, %d, true), want false for fresh DB", current, target)
	}
	if current != 1 {
		t.Errorf("NeedsMigration current = %d, want 1", current)
	}
	if target != index.CurrentSchemaVersion {
		t.Errorf("NeedsMigration target = %d, want %d", target, index.CurrentSchemaVersion)
	}
}

// TestNeedsMigrationLegacy simulates a legacy DB at schema_version=0 and
// verifies NeedsMigration returns (0, 1, true).
func TestNeedsMigrationLegacy(t *testing.T) {
	db := openTestDB(t)
	if err := db.SetSchemaVersion(0); err != nil {
		t.Fatalf("SetSchemaVersion(0): %v", err)
	}

	current, target, needed := db.NeedsMigration()
	if !needed {
		t.Errorf("NeedsMigration() = (%d, %d, false), want true for legacy DB", current, target)
	}
	if current != 0 {
		t.Errorf("NeedsMigration current = %d, want 0", current)
	}
	if target != index.CurrentSchemaVersion {
		t.Errorf("NeedsMigration target = %d, want %d", target, index.CurrentSchemaVersion)
	}
}

// TestSyntheticV1ToV2 demonstrates the framework with a synthetic v1→v2
// migration registered on a local, isolated Registry (does not pollute
// Default()).
func TestSyntheticV1ToV2(t *testing.T) {
	db := openTestDB(t)

	// Fresh DB is at v1; build an isolated registry for this test.
	r := &migrations.Registry{}
	r.Register(migrations.Migration{
		From: 0,
		To:   1,
		Name: "baseline",
		Apply: func(tx *sql.Tx) error {
			_, err := tx.Exec(
				`INSERT INTO _meta(key, value) VALUES('schema_version', 1) ON CONFLICT(key) DO NOTHING`,
			)
			return err
		},
	})
	r.Register(migrations.Migration{
		From: 1,
		To:   2,
		Name: "add_synthetic_table",
		Apply: func(tx *sql.Tx) error {
			_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS _synthetic_test (id INTEGER PRIMARY KEY)`)
			return err
		},
	})

	applied, err := r.Apply(db, 2, false)
	if err != nil {
		t.Fatalf("Apply 1→2: %v", err)
	}
	if len(applied) != 1 {
		// Only one step should run because current is already 1.
		t.Fatalf("Apply returned %d steps, want 1", len(applied))
	}
	if applied[0].From != 1 {
		t.Errorf("expected step from 1→2, got %d→%d", applied[0].From, applied[0].To)
	}

	v, err := db.SchemaVersion()
	if err != nil {
		t.Fatalf("SchemaVersion: %v", err)
	}
	if v != 2 {
		t.Errorf("schema_version = %d, want 2", v)
	}

	// Verify the table was actually created.
	var name string
	err = db.Conn().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='_synthetic_test'`,
	).Scan(&name)
	if err != nil {
		t.Errorf("_synthetic_test table not found after migration: %v", err)
	}
}
