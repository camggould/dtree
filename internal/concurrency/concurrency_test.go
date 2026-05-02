package concurrency_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cgould/dtree/internal/concurrency"
	"github.com/cgould/dtree/internal/ulid"
)

// TestConflictError verifies that the Error string contains the decision ID,
// expected rev, and actual rev.
func TestConflictError(t *testing.T) {
	c := &concurrency.Conflict{
		DecisionID:  "01DECISION0000000000000001",
		ExpectedRev: "01REV0000000000000000000001",
		ActualRev:   "01REV0000000000000000000002",
	}
	msg := c.Error()
	if !strings.Contains(msg, c.DecisionID) {
		t.Errorf("Error() = %q; want it to contain DecisionID %q", msg, c.DecisionID)
	}
	if !strings.Contains(msg, c.ExpectedRev) {
		t.Errorf("Error() = %q; want it to contain ExpectedRev %q", msg, c.ExpectedRev)
	}
	if !strings.Contains(msg, c.ActualRev) {
		t.Errorf("Error() = %q; want it to contain ActualRev %q", msg, c.ActualRev)
	}
}

// TestConflictUnwrap verifies that errors.Is(c, concurrency.ErrConflict) is true.
func TestConflictUnwrap(t *testing.T) {
	c := &concurrency.Conflict{
		DecisionID:  "01DECISION0000000000000001",
		ExpectedRev: "01REV0000000000000000000001",
		ActualRev:   "01REV0000000000000000000002",
	}
	if !errors.Is(c, concurrency.ErrConflict) {
		t.Errorf("errors.Is(conflict, ErrConflict) = false; want true")
	}
}

// TestAsConflict verifies that AsConflict extracts the *Conflict correctly,
// including from a wrapped error.
func TestAsConflict(t *testing.T) {
	c := &concurrency.Conflict{
		DecisionID:  "01DECISION0000000000000001",
		ExpectedRev: "01REV0000000000000000000001",
		ActualRev:   "01REV0000000000000000000002",
	}

	// Direct error.
	got, ok := concurrency.AsConflict(c)
	if !ok {
		t.Fatal("AsConflict(conflict) = _, false; want true")
	}
	if got.DecisionID != c.DecisionID {
		t.Errorf("DecisionID = %q, want %q", got.DecisionID, c.DecisionID)
	}
	if got.ExpectedRev != c.ExpectedRev {
		t.Errorf("ExpectedRev = %q, want %q", got.ExpectedRev, c.ExpectedRev)
	}
	if got.ActualRev != c.ActualRev {
		t.Errorf("ActualRev = %q, want %q", got.ActualRev, c.ActualRev)
	}

	// Non-conflict error.
	_, ok = concurrency.AsConflict(errors.New("some other error"))
	if ok {
		t.Error("AsConflict(non-conflict error) = _, true; want false")
	}

	// Nil.
	_, ok = concurrency.AsConflict(nil)
	if ok {
		t.Error("AsConflict(nil) = _, true; want false")
	}
}

// TestNewRevValidULID verifies that NewRev returns a string that parses as a
// valid ULID.
func TestNewRevValidULID(t *testing.T) {
	rev := concurrency.NewRev()
	if err := ulid.Parse(rev); err != nil {
		t.Errorf("NewRev() = %q; ulid.Parse error: %v", rev, err)
	}
}
