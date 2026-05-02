package index

import (
	"errors"
	"testing"

	"github.com/cgould/dtree/internal/concurrency"
	"github.com/cgould/dtree/internal/ulid"
)

// ---------------------------------------------------------------------------
// UpdateDecisionWithExpectedRev
// ---------------------------------------------------------------------------

// TestUpdateWithCorrectExpectedRev succeeds when the expected rev matches the
// stored rev, and the stored rev advances to newRev.
func TestUpdateWithCorrectExpectedRev(t *testing.T) {
	db := openTestDB(t)
	insertTree(t, db, "arch")

	d := newDecision("01DECISION0000000000000010", "arch", "slug-10", "Summary 10")
	if err := InsertDecision(db, d, "sha-before"); err != nil {
		t.Fatalf("InsertDecision: %v", err)
	}

	currentRev, err := GetDecisionRev(db, d.ID)
	if err != nil {
		t.Fatalf("GetDecisionRev: %v", err)
	}
	if currentRev == "" {
		t.Fatal("expected non-empty rev after insert")
	}

	newRev := ulid.New()
	d.Summary = "Updated summary 10"
	if err := UpdateDecisionWithExpectedRev(db, d, "sha-after", currentRev, newRev); err != nil {
		t.Fatalf("UpdateDecisionWithExpectedRev: %v", err)
	}

	storedRev, err := GetDecisionRev(db, d.ID)
	if err != nil {
		t.Fatalf("GetDecisionRev after update: %v", err)
	}
	if storedRev != newRev {
		t.Errorf("rev = %q, want %q", storedRev, newRev)
	}
}

// TestUpdateWithStaleExpectedRev returns a *concurrency.Conflict when the
// expected rev does not match the stored rev.
func TestUpdateWithStaleExpectedRev(t *testing.T) {
	db := openTestDB(t)
	insertTree(t, db, "arch")

	d := newDecision("01DECISION0000000000000011", "arch", "slug-11", "Summary 11")
	if err := InsertDecision(db, d, "sha-orig"); err != nil {
		t.Fatalf("InsertDecision: %v", err)
	}

	currentRev, err := GetDecisionRev(db, d.ID)
	if err != nil {
		t.Fatalf("GetDecisionRev: %v", err)
	}

	staleRev := "01STALE000000000000000000001"
	newRev := ulid.New()
	err = UpdateDecisionWithExpectedRev(db, d, "sha-new", staleRev, newRev)
	if err == nil {
		t.Fatal("expected error on stale rev, got nil")
	}

	// Must be a *Conflict.
	c, ok := concurrency.AsConflict(err)
	if !ok {
		t.Fatalf("AsConflict = false; err = %v", err)
	}
	if c.DecisionID != d.ID {
		t.Errorf("Conflict.DecisionID = %q, want %q", c.DecisionID, d.ID)
	}
	if c.ExpectedRev != staleRev {
		t.Errorf("Conflict.ExpectedRev = %q, want %q", c.ExpectedRev, staleRev)
	}
	if c.ActualRev != currentRev {
		t.Errorf("Conflict.ActualRev = %q, want %q", c.ActualRev, currentRev)
	}

	// errors.Is chain must reach ErrConflict.
	if !errors.Is(err, concurrency.ErrConflict) {
		t.Errorf("errors.Is(err, ErrConflict) = false")
	}
}

// TestUpdateWithEmptyExpectedRev skips the rev check (legacy behavior) and
// succeeds unconditionally.
func TestUpdateWithEmptyExpectedRev(t *testing.T) {
	db := openTestDB(t)
	insertTree(t, db, "arch")

	d := newDecision("01DECISION0000000000000012", "arch", "slug-12", "Summary 12")
	if err := InsertDecision(db, d, "sha-init"); err != nil {
		t.Fatalf("InsertDecision: %v", err)
	}

	newRev := ulid.New()
	d.Summary = "Updated summary 12"
	if err := UpdateDecisionWithExpectedRev(db, d, "sha-upd", "", newRev); err != nil {
		t.Fatalf("UpdateDecisionWithExpectedRev (empty expectedRev): %v", err)
	}

	storedRev, err := GetDecisionRev(db, d.ID)
	if err != nil {
		t.Fatalf("GetDecisionRev: %v", err)
	}
	if storedRev != newRev {
		t.Errorf("rev = %q, want %q", storedRev, newRev)
	}
}

// ---------------------------------------------------------------------------
// DeleteDecisionWithExpectedRev
// ---------------------------------------------------------------------------

// TestDeleteWithCorrectExpectedRev succeeds when the expected rev matches.
func TestDeleteWithCorrectExpectedRev(t *testing.T) {
	db := openTestDB(t)
	insertTree(t, db, "arch")

	d := newDecision("01DECISION0000000000000013", "arch", "slug-13", "Summary 13")
	if err := InsertDecision(db, d, "sha-d13"); err != nil {
		t.Fatalf("InsertDecision: %v", err)
	}

	currentRev, err := GetDecisionRev(db, d.ID)
	if err != nil {
		t.Fatalf("GetDecisionRev: %v", err)
	}

	if err := DeleteDecisionWithExpectedRev(db, d.ID, currentRev); err != nil {
		t.Fatalf("DeleteDecisionWithExpectedRev: %v", err)
	}

	var deleted int
	if err := db.conn.QueryRow(`SELECT deleted FROM decisions WHERE id=?`, d.ID).Scan(&deleted); err != nil {
		t.Fatalf("scan deleted: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}
}

// TestDeleteWithStaleExpectedRev returns a *concurrency.Conflict when the
// expected rev does not match.
func TestDeleteWithStaleExpectedRev(t *testing.T) {
	db := openTestDB(t)
	insertTree(t, db, "arch")

	d := newDecision("01DECISION0000000000000014", "arch", "slug-14", "Summary 14")
	if err := InsertDecision(db, d, "sha-d14"); err != nil {
		t.Fatalf("InsertDecision: %v", err)
	}

	staleRev := "01STALE000000000000000000002"
	err := DeleteDecisionWithExpectedRev(db, d.ID, staleRev)
	if err == nil {
		t.Fatal("expected error on stale rev, got nil")
	}

	c, ok := concurrency.AsConflict(err)
	if !ok {
		t.Fatalf("AsConflict = false; err = %v", err)
	}
	if c.DecisionID != d.ID {
		t.Errorf("Conflict.DecisionID = %q, want %q", c.DecisionID, d.ID)
	}
	if c.ExpectedRev != staleRev {
		t.Errorf("Conflict.ExpectedRev = %q, want %q", c.ExpectedRev, staleRev)
	}

	// Row must remain undeleted.
	var deleted int
	if err := db.conn.QueryRow(`SELECT deleted FROM decisions WHERE id=?`, d.ID).Scan(&deleted); err != nil {
		t.Fatalf("scan deleted: %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d after conflict, want 0 (no mutation)", deleted)
	}
}

// TestUpdateConflictDoesNotMutateIndex verifies that on a rev mismatch the
// rev and content_sha256 columns are unchanged (no partial write).
func TestUpdateConflictDoesNotMutateIndex(t *testing.T) {
	db := openTestDB(t)
	insertTree(t, db, "arch")

	d := newDecision("01DECISION0000000000000015", "arch", "slug-15", "Summary 15")
	const origSha = "sha-original"
	if err := InsertDecision(db, d, origSha); err != nil {
		t.Fatalf("InsertDecision: %v", err)
	}

	origRev, err := GetDecisionRev(db, d.ID)
	if err != nil {
		t.Fatalf("GetDecisionRev: %v", err)
	}

	// Attempt an update with a wrong expected rev.
	staleRev := "01STALE000000000000000000003"
	newRev := ulid.New()
	d.Summary = "Should not be stored"
	updateErr := UpdateDecisionWithExpectedRev(db, d, "sha-should-not-be-stored", staleRev, newRev)
	if updateErr == nil {
		t.Fatal("expected conflict error, got nil")
	}
	if _, ok := concurrency.AsConflict(updateErr); !ok {
		t.Fatalf("expected *Conflict, got: %v", updateErr)
	}

	// Rev must be unchanged.
	storedRev, err := GetDecisionRev(db, d.ID)
	if err != nil {
		t.Fatalf("GetDecisionRev after conflict: %v", err)
	}
	if storedRev != origRev {
		t.Errorf("rev = %q after conflict, want original %q", storedRev, origRev)
	}

	// content_sha256 must be unchanged.
	var storedSha string
	if err := db.conn.QueryRow(`SELECT content_sha256 FROM decisions WHERE id=?`, d.ID).Scan(&storedSha); err != nil {
		t.Fatalf("scan content_sha256: %v", err)
	}
	if storedSha != origSha {
		t.Errorf("content_sha256 = %q after conflict, want %q", storedSha, origSha)
	}
}
