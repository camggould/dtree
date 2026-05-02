// Package concurrency provides optimistic-concurrency helpers for dtree.
//
// The central type is Conflict, a structured error returned when a mutating
// operation detects that the caller's expected rev does not match the current
// rev stored in the index. Callers can test for a conflict with AsConflict and
// inspect DecisionID, ExpectedRev, and ActualRev to build user-facing messages
// or HTTP Problem Detail responses.
//
// NewRev delegates to ulid.New() and exists to make rev-creation greppable and
// testable in isolation.
package concurrency

import (
	"errors"
	"fmt"

	"github.com/cgould/dtree/internal/ulid"
)

// ErrConflict is the sentinel error for optimistic-concurrency violations.
// Use errors.Is to test for it; use AsConflict to obtain structured detail.
var ErrConflict = errors.New("optimistic concurrency conflict")

// Conflict is the structured error returned when an expected rev does not
// match the current rev in the index. It wraps ErrConflict so callers can
// use errors.Is(err, concurrency.ErrConflict) as well as AsConflict for the
// field values.
type Conflict struct {
	DecisionID  string
	ExpectedRev string
	ActualRev   string
}

// Error implements the error interface.
func (c *Conflict) Error() string {
	return fmt.Sprintf(
		"concurrency conflict: decision %s: expected rev %q, actual rev %q",
		c.DecisionID, c.ExpectedRev, c.ActualRev,
	)
}

// Unwrap returns ErrConflict so errors.Is works on the chain.
func (c *Conflict) Unwrap() error { return ErrConflict }

// AsConflict tests whether err (or any error in its chain) is a *Conflict.
// It mirrors the errors.As pattern so callers do not need to import errors.As
// directly with the concrete type.
func AsConflict(err error) (*Conflict, bool) {
	var c *Conflict
	if errors.As(err, &c) {
		return c, true
	}
	return nil, false
}

// NewRev returns a new ULID string to be stored as the next rev token.
// Delegating through this function makes rev-creation greppable and allows
// tests to substitute a deterministic ULID generator at the ulid package level.
func NewRev() string {
	return ulid.New()
}
