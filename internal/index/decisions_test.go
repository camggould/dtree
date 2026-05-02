package index

import (
	"testing"

	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/ulid"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newDecision(id, tree, slug, summary string) *core.Decision {
	return &core.Decision{
		ID:            id,
		Tree:          tree,
		Slug:          slug,
		Summary:       summary,
		SchemaVersion: core.SchemaVersion,
		Status:        core.StatusProposed,
		Priority:      core.PriorityMedium,
		Creator:       "alice",
	}
}

// ---------------------------------------------------------------------------
// TestInsertGetDecisionRoundTrip
// ---------------------------------------------------------------------------

func TestInsertGetDecisionRoundTrip(t *testing.T) {
	db := openTestDB(t)
	insertTree(t, db, "arch")

	d := newDecision("01DECISION0000000000000001", "arch", "use-postgres", "Use PostgreSQL")
	d.Tags = []string{"storage", "backend"}
	d.DecidedBy = []string{"alice", "bob"}
	d.Relationships = []core.Relationship{
		{Type: core.RelBlocks, Target: "01DECISION0000000000000002"},
	}
	d.Description = "Full description here."
	d.RecommendedBy = "alice"
	d.RecommendedSummary = "Use PostgreSQL"

	const sha = "deadbeef"
	if err := InsertDecision(db, d, sha); err != nil {
		t.Fatalf("InsertDecision: %v", err)
	}

	got, err := GetDecision(db, d.ID)
	if err != nil {
		t.Fatalf("GetDecision: %v", err)
	}
	if got == nil {
		t.Fatal("GetDecision returned nil for existing ID")
	}

	if got.ID != d.ID {
		t.Errorf("ID = %q, want %q", got.ID, d.ID)
	}
	if got.Tree != d.Tree {
		t.Errorf("Tree = %q, want %q", got.Tree, d.Tree)
	}
	if got.Summary != d.Summary {
		t.Errorf("Summary = %q, want %q", got.Summary, d.Summary)
	}
	if got.Description != d.Description {
		t.Errorf("Description = %q, want %q", got.Description, d.Description)
	}
	if got.RecommendedBy != d.RecommendedBy {
		t.Errorf("RecommendedBy = %q, want %q", got.RecommendedBy, d.RecommendedBy)
	}

	// tags — sorted
	if len(got.Tags) != 2 || got.Tags[0] != "backend" || got.Tags[1] != "storage" {
		t.Errorf("Tags = %v, want [backend storage]", got.Tags)
	}

	// deciders — sorted
	if len(got.DecidedBy) != 2 || got.DecidedBy[0] != "alice" || got.DecidedBy[1] != "bob" {
		t.Errorf("DecidedBy = %v, want [alice bob]", got.DecidedBy)
	}

	// relationships
	if len(got.Relationships) != 1 {
		t.Fatalf("Relationships len = %d, want 1", len(got.Relationships))
	}
	if got.Relationships[0].Type != core.RelBlocks || got.Relationships[0].Target != "01DECISION0000000000000002" {
		t.Errorf("Relationship = %+v, want blocks 01DECISION0000000000000002", got.Relationships[0])
	}
}

// ---------------------------------------------------------------------------
// TestGetDecisionMissing
// ---------------------------------------------------------------------------

func TestGetDecisionMissing(t *testing.T) {
	db := openTestDB(t)

	got, err := GetDecision(db, "01NOTEXIST000000000000001")
	if err != nil {
		t.Fatalf("GetDecision missing: unexpected error %v", err)
	}
	if got != nil {
		t.Errorf("GetDecision missing: expected nil, got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// TestUpdateDecisionChangesRev
// ---------------------------------------------------------------------------

func TestUpdateDecisionChangesRev(t *testing.T) {
	db := openTestDB(t)
	insertTree(t, db, "arch")

	d := newDecision("01DECISION0000000000000003", "arch", "initial-slug", "Initial summary")
	if err := InsertDecision(db, d, "sha-before"); err != nil {
		t.Fatalf("InsertDecision: %v", err)
	}

	rev1, err := GetDecisionRev(db, d.ID)
	if err != nil {
		t.Fatalf("GetDecisionRev: %v", err)
	}
	if rev1 == "" {
		t.Fatal("expected non-empty rev after insert")
	}

	newRev := ulid.New()
	d.Summary = "Updated summary"
	if err := UpdateDecision(db, d, "sha-after", newRev); err != nil {
		t.Fatalf("UpdateDecision: %v", err)
	}

	rev2, err := GetDecisionRev(db, d.ID)
	if err != nil {
		t.Fatalf("GetDecisionRev after update: %v", err)
	}
	if rev2 != newRev {
		t.Errorf("rev after update = %q, want %q", rev2, newRev)
	}
	if rev2 == rev1 {
		t.Error("rev did not change after update")
	}

	got, err := GetDecision(db, d.ID)
	if err != nil {
		t.Fatalf("GetDecision after update: %v", err)
	}
	if got.Summary != "Updated summary" {
		t.Errorf("Summary = %q, want %q", got.Summary, "Updated summary")
	}
}

// ---------------------------------------------------------------------------
// TestDeleteDecisionSoftDeletes
// ---------------------------------------------------------------------------

func TestDeleteDecisionSoftDeletes(t *testing.T) {
	db := openTestDB(t)
	insertTree(t, db, "arch")

	d := newDecision("01DECISION0000000000000004", "arch", "del-slug", "Decision to delete")
	if err := InsertDecision(db, d, "sha1"); err != nil {
		t.Fatalf("InsertDecision: %v", err)
	}

	if err := DeleteDecision(db, d.ID); err != nil {
		t.Fatalf("DeleteDecision: %v", err)
	}

	// GetDecision still returns it (callers check deleted flag via direct SQL).
	got, err := GetDecision(db, d.ID)
	if err != nil {
		t.Fatalf("GetDecision after soft-delete: %v", err)
	}
	if got == nil {
		t.Fatal("GetDecision returned nil for soft-deleted row; expected the row back")
	}

	// deleted flag should be 1 in the raw row.
	var deleted int
	if err := db.conn.QueryRow(`SELECT deleted FROM decisions WHERE id=?`, d.ID).Scan(&deleted); err != nil {
		t.Fatalf("scan deleted: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
}

// ---------------------------------------------------------------------------
// TestGetDecisionRevMissing
// ---------------------------------------------------------------------------

func TestGetDecisionRevMissing(t *testing.T) {
	db := openTestDB(t)

	rev, err := GetDecisionRev(db, "01NOTEXIST000000000000002")
	if err != nil {
		t.Fatalf("GetDecisionRev missing: %v", err)
	}
	if rev != "" {
		t.Errorf("GetDecisionRev missing: expected empty string, got %q", rev)
	}
}
