package validate

import (
	"strings"
	"testing"

	"github.com/cgould/dtree/internal/core"
)

func validProposed() *core.Decision {
	return &core.Decision{
		ID:       "01HXKQ5Z3PCWJ8FQR4M2TVB7D9",
		Slug:     "x",
		Summary:  "x",
		Priority: core.PriorityMedium,
		Status:   core.StatusProposed,
		Creator:  "cam",
	}
}

func TestValidProposed(t *testing.T) {
	if err := Decision(validProposed()); err != nil {
		t.Errorf("expected valid, got %v", err)
	}
}

func TestDecidedRequiresOutcomeFields(t *testing.T) {
	d := validProposed()
	d.Status = core.StatusDecided
	err := Decision(d)
	if err == nil {
		t.Fatal("expected error for decided without actual_choice")
	}
	if ve, ok := AsValidationError(err); !ok || ve.Code != "incomplete_decided" {
		t.Errorf("got %v, want incomplete_decided", err)
	}
}

func TestDecidedComplete(t *testing.T) {
	d := validProposed()
	d.Status = core.StatusDecided
	d.ActualChoice = "X"
	d.ActualChoiceReason = "Y"
	d.DecidedBy = []string{"cam"}
	if err := Decision(d); err != nil {
		t.Errorf("expected valid, got %v", err)
	}
}

func TestRecommenderWithoutRecommendation(t *testing.T) {
	d := validProposed()
	d.RecommendedBy = "cam-claude"
	err := Decision(d)
	if ve, ok := AsValidationError(err); !ok || ve.Code != "incomplete_recommendation" {
		t.Errorf("got %v, want incomplete_recommendation", err)
	}
}

func TestSelfRelationshipRejected(t *testing.T) {
	d := validProposed()
	d.Relationships = []core.Relationship{
		{Type: core.RelBlocks, Target: d.ID},
	}
	err := Decision(d)
	if ve, ok := AsValidationError(err); !ok || ve.Code != "self_relationship" {
		t.Errorf("got %v, want self_relationship", err)
	}
}

func TestInvalidRelationshipType(t *testing.T) {
	d := validProposed()
	d.Relationships = []core.Relationship{
		{Type: "wat", Target: "01HXKQ7N9MR4VXBPDTYFW2K8H1"},
	}
	if err := Decision(d); err == nil {
		t.Error("expected error for invalid relationship type")
	}
}

func TestInvalidPriorityRejected(t *testing.T) {
	d := validProposed()
	d.Priority = "wat"
	err := Decision(d)
	if ve, ok := AsValidationError(err); !ok || ve.Code != "invalid_priority" {
		t.Errorf("got %v, want invalid_priority", err)
	}
}

func TestCollectDecisionMultipleErrors(t *testing.T) {
	// Decided with 0 outcome fields → 3 incomplete_decided errors.
	d := validProposed()
	d.Status = core.StatusDecided
	errs := CollectDecision(d)
	if len(errs) != 3 {
		t.Errorf("got %d errs, want 3: %v", len(errs), errs)
	}
}

func TestGraphCycleInBlocks(t *testing.T) {
	edges := []Edge{
		{Source: "A", Target: "B", Type: core.RelBlocks},
		{Source: "B", Target: "C", Type: core.RelBlocks},
		{Source: "C", Target: "A", Type: core.RelBlocks},
	}
	err := Graph(edges)
	if err == nil {
		t.Fatal("expected cycle")
	}
	if ve, ok := AsValidationError(err); !ok || ve.Code != "cycle_detected" {
		t.Errorf("got %v, want cycle_detected", err)
	}
}

func TestGraphAcyclicBlocks(t *testing.T) {
	edges := []Edge{
		{Source: "A", Target: "B", Type: core.RelBlocks},
		{Source: "B", Target: "C", Type: core.RelBlocks},
	}
	if err := Graph(edges); err != nil {
		t.Errorf("expected acyclic, got %v", err)
	}
}

func TestGraphInfluencesCycleAllowed(t *testing.T) {
	edges := []Edge{
		{Source: "A", Target: "B", Type: core.RelInfluences},
		{Source: "B", Target: "A", Type: core.RelInfluences},
	}
	if err := Graph(edges); err != nil {
		t.Errorf("influences cycles should be allowed, got %v", err)
	}
}

func TestAddingEdgeWouldCycle(t *testing.T) {
	existing := []Edge{
		{Source: "A", Target: "B", Type: core.RelBlocks},
		{Source: "B", Target: "C", Type: core.RelBlocks},
	}
	if !AddingEdgeWouldCycle(existing, "C", "A", core.RelBlocks) {
		t.Error("C→A blocks should close A→B→C→A cycle")
	}
	if AddingEdgeWouldCycle(existing, "C", "D", core.RelBlocks) {
		t.Error("C→D blocks should not cycle")
	}
}

func TestAddingEdgeWouldCycleSelf(t *testing.T) {
	if !AddingEdgeWouldCycle(nil, "A", "A", core.RelBlocks) {
		t.Error("self-edge blocks should cycle")
	}
}

func TestAddingEdgeInfluencesNeverCycles(t *testing.T) {
	if AddingEdgeWouldCycle(nil, "A", "A", core.RelInfluences) {
		t.Error("influences self-edge should not be flagged as cycle")
	}
}

func TestErrorMessageFormat(t *testing.T) {
	e := newErr("bad", "field", "msg")
	s := e.Error()
	if !strings.Contains(s, "bad") || !strings.Contains(s, "field") || !strings.Contains(s, "msg") {
		t.Errorf("error message lost info: %s", s)
	}
}
