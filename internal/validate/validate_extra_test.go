package validate

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/ulid"
)

// ---------------------------------------------------------------------------
// All Code values exercised
// ---------------------------------------------------------------------------

func TestDecisionNilReturnsInvalidCode(t *testing.T) {
	t.Parallel()
	err := Decision(nil)
	if err == nil {
		t.Fatal("expected error for nil decision, got nil")
	}
	ve, ok := AsValidationError(err)
	if !ok {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if ve.Code != "invalid" {
		t.Errorf("Code = %q, want 'invalid'", ve.Code)
	}
}

func TestDecisionInvalidID(t *testing.T) {
	t.Parallel()
	d := validProposed()
	d.ID = "short"
	err := Decision(d)
	ve, ok := AsValidationError(err)
	if !ok {
		t.Fatalf("expected *Error: %v", err)
	}
	if ve.Code != "invalid_id" {
		t.Errorf("Code = %q, want 'invalid_id'", ve.Code)
	}
	if ve.Field != "id" {
		t.Errorf("Field = %q, want 'id'", ve.Field)
	}
}

func TestDecisionEmptySlug(t *testing.T) {
	t.Parallel()
	d := validProposed()
	d.Slug = ""
	err := Decision(d)
	ve, ok := AsValidationError(err)
	if !ok {
		t.Fatalf("expected *Error: %v", err)
	}
	if ve.Code != "invalid_slug" {
		t.Errorf("Code = %q, want 'invalid_slug'", ve.Code)
	}
}

func TestDecisionEmptySummary(t *testing.T) {
	t.Parallel()
	d := validProposed()
	d.Summary = ""
	err := Decision(d)
	ve, ok := AsValidationError(err)
	if !ok {
		t.Fatalf("expected *Error: %v", err)
	}
	if ve.Code != "invalid_summary" {
		t.Errorf("Code = %q, want 'invalid_summary'", ve.Code)
	}
}

func TestDecisionInvalidStatus(t *testing.T) {
	t.Parallel()
	d := validProposed()
	d.Status = "unknown"
	err := Decision(d)
	ve, ok := AsValidationError(err)
	if !ok {
		t.Fatalf("expected *Error: %v", err)
	}
	if ve.Code != "invalid_status" {
		t.Errorf("Code = %q, want 'invalid_status'", ve.Code)
	}
}

func TestDecisionEmptyCreator(t *testing.T) {
	t.Parallel()
	d := validProposed()
	d.Creator = ""
	err := Decision(d)
	ve, ok := AsValidationError(err)
	if !ok {
		t.Fatalf("expected *Error: %v", err)
	}
	if ve.Code != "invalid_creator" {
		t.Errorf("Code = %q, want 'invalid_creator'", ve.Code)
	}
}

func TestDecisionDecidedMissingActualChoiceReason(t *testing.T) {
	t.Parallel()
	d := validProposed()
	d.Status = core.StatusDecided
	d.ActualChoice = "Option A" // set this but not reason
	err := Decision(d)
	ve, ok := AsValidationError(err)
	if !ok {
		t.Fatalf("expected *Error: %v", err)
	}
	if ve.Code != "incomplete_decided" {
		t.Errorf("Code = %q, want 'incomplete_decided'", ve.Code)
	}
	if ve.Field != "actual_choice_reason" {
		t.Errorf("Field = %q, want 'actual_choice_reason'", ve.Field)
	}
}

func TestDecisionDecidedMissingDecidedBy(t *testing.T) {
	t.Parallel()
	d := validProposed()
	d.Status = core.StatusDecided
	d.ActualChoice = "X"
	d.ActualChoiceReason = "Y"
	// DecidedBy empty.
	err := Decision(d)
	ve, ok := AsValidationError(err)
	if !ok {
		t.Fatalf("expected *Error: %v", err)
	}
	if ve.Code != "incomplete_decided" {
		t.Errorf("Code = %q, want 'incomplete_decided'", ve.Code)
	}
	if ve.Field != "decided_by" {
		t.Errorf("Field = %q, want 'decided_by'", ve.Field)
	}
}

func TestDecisionOutOfScopeValid(t *testing.T) {
	t.Parallel()
	d := validProposed()
	d.Status = core.StatusOutOfScope
	// out_of_scope_reason is optional — should still pass.
	if err := Decision(d); err != nil {
		t.Errorf("out_of_scope without reason should be valid: %v", err)
	}
}

func TestDecisionOutOfScopeWithReason(t *testing.T) {
	t.Parallel()
	d := validProposed()
	d.Status = core.StatusOutOfScope
	d.OutOfScopeReason = "Not needed anymore"
	if err := Decision(d); err != nil {
		t.Errorf("out_of_scope with reason should be valid: %v", err)
	}
}

func TestDecisionSupersededValid(t *testing.T) {
	t.Parallel()
	d := validProposed()
	d.Status = core.StatusSuperseded
	if err := Decision(d); err != nil {
		t.Errorf("superseded should be valid at single-decision level: %v", err)
	}
}

func TestDecisionInvalidRelationshipTargetULID(t *testing.T) {
	t.Parallel()
	d := validProposed()
	d.Relationships = []core.Relationship{
		{Type: core.RelBlocks, Target: "not-a-ulid"},
	}
	err := Decision(d)
	ve, ok := AsValidationError(err)
	if !ok {
		t.Fatalf("expected *Error: %v", err)
	}
	if ve.Code != "invalid_relationship_target" {
		t.Errorf("Code = %q, want 'invalid_relationship_target'", ve.Code)
	}
}

func TestDecisionAllPriorityValues(t *testing.T) {
	t.Parallel()
	priorities := []core.Priority{
		core.PriorityAssumption,
		core.PriorityLow,
		core.PriorityMedium,
		core.PriorityHigh,
		core.PriorityCritical,
	}
	for _, p := range priorities {
		p := p
		t.Run(string(p), func(t *testing.T) {
			t.Parallel()
			d := validProposed()
			d.Priority = p
			if err := Decision(d); err != nil {
				t.Errorf("priority %q should be valid, got: %v", p, err)
			}
		})
	}
}

func TestDecisionAllStatusValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status  core.Status
		prepare func(*core.Decision)
	}{
		{core.StatusProposed, func(d *core.Decision) {}},
		{core.StatusOutOfScope, func(d *core.Decision) {}},
		{core.StatusSuperseded, func(d *core.Decision) {}},
		{core.StatusDecided, func(d *core.Decision) {
			d.ActualChoice = "X"
			d.ActualChoiceReason = "Y"
			d.DecidedBy = []string{"alice"}
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(string(tc.status), func(t *testing.T) {
			t.Parallel()
			d := validProposed()
			d.Status = tc.status
			tc.prepare(d)
			if err := Decision(d); err != nil {
				t.Errorf("status %q should be valid: %v", tc.status, err)
			}
		})
	}
}

func TestDecisionAllRelationshipTypes(t *testing.T) {
	t.Parallel()
	types := []core.RelationshipType{
		core.RelBlocks,
		core.RelInfluences,
		core.RelSupersedes,
		core.RelRelatesTo,
	}
	validTarget := ulid.New()
	for _, rt := range types {
		rt := rt
		t.Run(string(rt), func(t *testing.T) {
			t.Parallel()
			d := validProposed()
			d.Relationships = []core.Relationship{
				{Type: rt, Target: validTarget},
			}
			if err := Decision(d); err != nil {
				t.Errorf("relationship type %q with valid target should be valid: %v", rt, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// AsValidationError — wrapped errors
// ---------------------------------------------------------------------------

func TestAsValidationErrorWrapped(t *testing.T) {
	t.Parallel()
	inner := newErr("some_code", "field", "msg")
	wrapped := fmt.Errorf("outer: %w", inner)
	ve, ok := AsValidationError(wrapped)
	if !ok {
		t.Fatal("AsValidationError should unwrap to *Error")
	}
	if ve.Code != "some_code" {
		t.Errorf("Code = %q, want 'some_code'", ve.Code)
	}
}

func TestAsValidationErrorNonValidationError(t *testing.T) {
	t.Parallel()
	err := errors.New("generic error")
	_, ok := AsValidationError(err)
	if ok {
		t.Error("generic error should not be a validation error")
	}
}

func TestAsValidationErrorNil(t *testing.T) {
	t.Parallel()
	_, ok := AsValidationError(nil)
	if ok {
		t.Error("nil error should return ok=false")
	}
}

// ---------------------------------------------------------------------------
// Error.Error() — without field
// ---------------------------------------------------------------------------

func TestErrorMessageNoField(t *testing.T) {
	t.Parallel()
	e := newErr("my_code", "", "my message")
	s := e.Error()
	if !strings.Contains(s, "my_code") {
		t.Errorf("missing code in %q", s)
	}
	if !strings.Contains(s, "my message") {
		t.Errorf("missing message in %q", s)
	}
	if strings.Contains(s, "field=") {
		t.Errorf("field= should not appear when field is empty: %q", s)
	}
}

// ---------------------------------------------------------------------------
// CollectDecision — nil decision
// ---------------------------------------------------------------------------

func TestCollectDecisionNil(t *testing.T) {
	t.Parallel()
	errs := CollectDecision(nil)
	if len(errs) != 1 {
		t.Errorf("expected 1 error for nil, got %d", len(errs))
	}
}

// ---------------------------------------------------------------------------
// CollectDecision — valid decision returns no errors
// ---------------------------------------------------------------------------

func TestCollectDecisionValid(t *testing.T) {
	t.Parallel()
	d := validProposed()
	errs := CollectDecision(d)
	if len(errs) != 0 {
		t.Errorf("expected 0 errors for valid decision, got %d: %v", len(errs), errs)
	}
}

// ---------------------------------------------------------------------------
// CollectDecision — recommendation check
// ---------------------------------------------------------------------------

func TestCollectDecisionIncompleteRecommendation(t *testing.T) {
	t.Parallel()
	d := validProposed()
	d.RecommendedBy = "claude"
	// RecommendedSummary is empty — should produce an error.
	errs := CollectDecision(d)
	if len(errs) == 0 {
		t.Error("expected at least 1 error for incomplete recommendation")
	}
	found := false
	for _, e := range errs {
		ve, ok := AsValidationError(e)
		if ok && ve.Code == "incomplete_recommendation" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected incomplete_recommendation error, got: %v", errs)
	}
}

// ---------------------------------------------------------------------------
// CollectDecision — relationship errors collected
// ---------------------------------------------------------------------------

func TestCollectDecisionRelationshipErrors(t *testing.T) {
	t.Parallel()
	d := validProposed()
	d.Relationships = []core.Relationship{
		{Type: "bad-type", Target: "not-a-ulid"},
	}
	errs := CollectDecision(d)
	if len(errs) == 0 {
		t.Error("expected relationship errors in collect")
	}
}

// ---------------------------------------------------------------------------
// Graph — supersedes cycle detected
// ---------------------------------------------------------------------------

func TestGraphCycleInSupersedes(t *testing.T) {
	t.Parallel()
	edges := []Edge{
		{Source: "X", Target: "Y", Type: core.RelSupersedes},
		{Source: "Y", Target: "X", Type: core.RelSupersedes},
	}
	err := Graph(edges)
	if err == nil {
		t.Fatal("expected cycle in supersedes graph")
	}
	ve, ok := AsValidationError(err)
	if !ok {
		t.Fatalf("expected *Error: %v", err)
	}
	if ve.Code != "cycle_detected" {
		t.Errorf("Code = %q, want 'cycle_detected'", ve.Code)
	}
}

func TestGraphEmptyEdges(t *testing.T) {
	t.Parallel()
	if err := Graph(nil); err != nil {
		t.Errorf("empty graph should have no cycles: %v", err)
	}
}

// ---------------------------------------------------------------------------
// AddingEdgeWouldCycle — supersedes edges
// ---------------------------------------------------------------------------

func TestAddingEdgeWouldCycleSupersedes(t *testing.T) {
	t.Parallel()
	existing := []Edge{
		{Source: "A", Target: "B", Type: core.RelSupersedes},
	}
	if !AddingEdgeWouldCycle(existing, "B", "A", core.RelSupersedes) {
		t.Error("B→A supersedes should close A→B→A cycle")
	}
	if AddingEdgeWouldCycle(existing, "B", "C", core.RelSupersedes) {
		t.Error("B→C supersedes should not cycle")
	}
}

func TestAddingEdgeWouldCycleRelatesToNeverCycles(t *testing.T) {
	t.Parallel()
	// relates_to is not a DAG-restricted type.
	if AddingEdgeWouldCycle(nil, "A", "A", core.RelRelatesTo) {
		t.Error("relates_to self-edge should not be flagged as cycle")
	}
}

// ---------------------------------------------------------------------------
// formatCycle — empty slice
// ---------------------------------------------------------------------------

func TestFormatCycleEmpty(t *testing.T) {
	t.Parallel()
	s := formatCycle(nil)
	if s != "" {
		t.Errorf("formatCycle(nil) = %q, want empty", s)
	}
}
