package audit_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/ulid"
)

// ---------------------------------------------------------------------------
// Malformed JSONL lines are skipped, not fatal
// ---------------------------------------------------------------------------

func TestMalformedJSONLLinesSkipped(t *testing.T) {
	t.Parallel()
	root := newRoot(t)

	// Write a real event first.
	base := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	goodEv := makeEventAt(core.ActionCreate, core.KindDecision, "t", "ID1", "u", base)
	must(t, audit.Append(root, goodEv))

	// Directly inject a malformed line into the JSONL file.
	auditDir := filepath.Join(root, ".decisions", "t", "audit")
	files, err := os.ReadDir(auditDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("expected at least one JSONL file")
	}
	path := filepath.Join(auditDir, files[0].Name())
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("{this is not valid json}\n")
	_ = f.Close()

	// Read should succeed, returning only the valid event.
	evs, err := audit.Read(root, audit.Filter{})
	if err != nil {
		t.Fatalf("Read with malformed line: %v", err)
	}
	if len(evs) != 1 {
		t.Errorf("expected 1 good event, got %d", len(evs))
	}
}

// ---------------------------------------------------------------------------
// Empty line in JSONL is skipped
// ---------------------------------------------------------------------------

func TestEmptyLineInJSONL(t *testing.T) {
	t.Parallel()
	root := newRoot(t)

	base := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	must(t, audit.Append(root, makeEventAt(core.ActionCreate, core.KindDecision, "t", "ID1", "u", base)))

	// Inject an empty line.
	auditDir := filepath.Join(root, ".decisions", "t", "audit")
	files, _ := os.ReadDir(auditDir)
	path := filepath.Join(auditDir, files[0].Name())
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString("\n")
	_ = f.Close()

	evs, err := audit.Read(root, audit.Filter{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(evs) != 1 {
		t.Errorf("expected 1 event, got %d (empty line should be ignored)", len(evs))
	}
}

// ---------------------------------------------------------------------------
// Multiple filter predicates combined (AND semantics)
// ---------------------------------------------------------------------------

func TestFilterCombinedActorAndAction(t *testing.T) {
	t.Parallel()
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	must(t, audit.Append(root, makeEventAt(core.ActionCreate, core.KindDecision, "t", "A", "alice", base)))
	must(t, audit.Append(root, makeEventAt(core.ActionUpdate, core.KindDecision, "t", "A", "alice", base.Add(time.Second))))
	must(t, audit.Append(root, makeEventAt(core.ActionCreate, core.KindDecision, "t", "B", "bob", base.Add(2*time.Second))))

	got, err := audit.Read(root, audit.Filter{Actor: "alice", Action: core.ActionCreate})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 event matching actor=alice AND action=create, got %d", len(got))
	}
	if got[0].Actor != "alice" || got[0].Action != core.ActionCreate {
		t.Errorf("wrong event: %+v", got[0])
	}
}

func TestFilterCombinedTreeAndKind(t *testing.T) {
	t.Parallel()
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	must(t, audit.Append(root, makeEventAt(core.ActionCreate, core.KindDecision, "alpha", "D1", "u", base)))
	must(t, audit.Append(root, makeEventAt(core.ActionTreeCreate, core.KindTree, "alpha", "alpha", "u", base.Add(time.Second))))
	must(t, audit.Append(root, makeEventAt(core.ActionCreate, core.KindDecision, "beta", "D2", "u", base.Add(2*time.Second))))

	// Only tree=alpha, kind=decision.
	got, err := audit.Read(root, audit.Filter{Tree: "alpha", Kind: core.KindDecision})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].Tree != "alpha" || got[0].Kind != core.KindDecision {
		t.Errorf("unexpected event: %+v", got[0])
	}
}

func TestFilterCombinedTargetIDAndSince(t *testing.T) {
	t.Parallel()
	root := newRoot(t)

	t1 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	t3 := t2.Add(time.Hour)

	must(t, audit.Append(root, makeEventAt(core.ActionCreate, core.KindDecision, "t", "ID_A", "u", t1)))
	must(t, audit.Append(root, makeEventAt(core.ActionUpdate, core.KindDecision, "t", "ID_A", "u", t2)))
	must(t, audit.Append(root, makeEventAt(core.ActionUpdate, core.KindDecision, "t", "ID_A", "u", t3)))

	// Since=t2, TargetID=ID_A → should return 2 events.
	got, err := audit.Read(root, audit.Filter{TargetID: "ID_A", Since: t2})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 events (since t2, targetID=ID_A), got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// Replay edge cases
// ---------------------------------------------------------------------------

func TestReplayUpdateWithoutPriorCreate(t *testing.T) {
	t.Parallel()
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	id := ulid.New()

	// Update without prior create — should materialise a shell decision.
	update := makeEventAt(core.ActionUpdate, core.KindDecision, "t", id, "u", base)
	update.Payload = core.EventPayload{
		After: map[string]any{"summary": "Orphan update"},
	}
	must(t, audit.Append(root, update))

	state, err := audit.ReplayState(root, "t", base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	d, ok := state[id]
	if !ok {
		t.Fatal("orphan update should materialise a shell decision")
	}
	if d.Summary != "Orphan update" {
		t.Errorf("Summary = %q", d.Summary)
	}
}

func TestReplayUndecide(t *testing.T) {
	t.Parallel()
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	id := ulid.New()

	create := makeEventAt(core.ActionCreate, core.KindDecision, "t", id, "u", base)
	create.Payload = core.EventPayload{
		After: map[string]any{"id": id, "slug": "d", "summary": "D", "status": "proposed", "priority": "low", "creator": "u"},
	}
	decide := makeEventAt(core.ActionDecide, core.KindDecision, "t", id, "u", base.Add(time.Minute))
	decide.Payload = core.EventPayload{
		Extra: map[string]any{
			"actual_choice":        "Choice A",
			"actual_choice_reason": "Reason",
			"decided_by":           []any{"u"},
			"is_recommended":       true,
		},
	}
	undecide := makeEventAt(core.ActionUndecide, core.KindDecision, "t", id, "u", base.Add(2*time.Minute))

	must(t, audit.Append(root, create))
	must(t, audit.Append(root, decide))
	must(t, audit.Append(root, undecide))

	state, err := audit.ReplayState(root, "t", base.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	d := state[id]
	if d == nil {
		t.Fatal("decision should exist after undecide")
	}
	if d.Status != core.StatusProposed {
		t.Errorf("status = %q, want proposed", d.Status)
	}
	if d.ActualChoice != "" {
		t.Errorf("actual_choice should be cleared, got %q", d.ActualChoice)
	}
	if d.IsRecommended {
		t.Error("is_recommended should be false after undecide")
	}
}

func TestReplayScopeOut(t *testing.T) {
	t.Parallel()
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	id := ulid.New()

	create := makeEventAt(core.ActionCreate, core.KindDecision, "t", id, "u", base)
	create.Payload = core.EventPayload{
		After: map[string]any{"id": id, "slug": "d", "summary": "D", "status": "proposed", "priority": "low", "creator": "u"},
	}
	scopeOut := makeEventAt(core.ActionScopeOut, core.KindDecision, "t", id, "u", base.Add(time.Minute))
	scopeOut.Payload = core.EventPayload{
		Extra: map[string]any{"out_of_scope_reason": "Not relevant anymore"},
	}

	must(t, audit.Append(root, create))
	must(t, audit.Append(root, scopeOut))

	state, err := audit.ReplayState(root, "t", base.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	d := state[id]
	if d == nil {
		t.Fatal("decision should exist after scope_out")
	}
	if d.Status != core.StatusOutOfScope {
		t.Errorf("status = %q, want out_of_scope", d.Status)
	}
	if d.OutOfScopeReason != "Not relevant anymore" {
		t.Errorf("out_of_scope_reason = %q", d.OutOfScopeReason)
	}
}

func TestReplaySupersede(t *testing.T) {
	t.Parallel()
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	id := ulid.New()

	create := makeEventAt(core.ActionCreate, core.KindDecision, "t", id, "u", base)
	create.Payload = core.EventPayload{
		After: map[string]any{"id": id, "slug": "d", "summary": "D", "status": "proposed", "priority": "low", "creator": "u"},
	}
	supersede := makeEventAt(core.ActionSupersede, core.KindDecision, "t", id, "u", base.Add(time.Minute))

	must(t, audit.Append(root, create))
	must(t, audit.Append(root, supersede))

	state, err := audit.ReplayState(root, "t", base.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	d := state[id]
	if d == nil {
		t.Fatal("decision should exist after supersede")
	}
	if d.Status != core.StatusSuperseded {
		t.Errorf("status = %q, want superseded", d.Status)
	}
}

func TestReplayRename(t *testing.T) {
	t.Parallel()
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	id := ulid.New()

	create := makeEventAt(core.ActionCreate, core.KindDecision, "t", id, "u", base)
	create.Payload = core.EventPayload{
		After: map[string]any{"id": id, "slug": "old-slug", "summary": "Old summary", "status": "proposed", "priority": "low", "creator": "u"},
	}
	rename := makeEventAt(core.ActionRename, core.KindDecision, "t", id, "u", base.Add(time.Minute))
	rename.Payload = core.EventPayload{
		Extra: map[string]any{"slug": "new-slug", "summary": "New summary"},
	}

	must(t, audit.Append(root, create))
	must(t, audit.Append(root, rename))

	state, err := audit.ReplayState(root, "t", base.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	d := state[id]
	if d == nil {
		t.Fatal("decision should exist after rename")
	}
	if d.Slug != "new-slug" {
		t.Errorf("slug = %q, want new-slug", d.Slug)
	}
	if d.Summary != "New summary" {
		t.Errorf("summary = %q, want 'New summary'", d.Summary)
	}
}

func TestReplayRestore(t *testing.T) {
	t.Parallel()
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	id := ulid.New()

	create := makeEventAt(core.ActionCreate, core.KindDecision, "t", id, "u", base)
	create.Payload = core.EventPayload{
		After: map[string]any{"id": id, "slug": "d", "summary": "D", "status": "proposed", "priority": "low", "creator": "u"},
	}
	del := makeEventAt(core.ActionDelete, core.KindDecision, "t", id, "u", base.Add(time.Minute))
	restore := makeEventAt(core.ActionRestore, core.KindDecision, "t", id, "u", base.Add(2*time.Minute))
	restore.Payload = core.EventPayload{
		After: map[string]any{"id": id, "slug": "d", "summary": "Restored", "status": "proposed", "priority": "low", "creator": "u"},
	}

	must(t, audit.Append(root, create))
	must(t, audit.Append(root, del))
	must(t, audit.Append(root, restore))

	state, err := audit.ReplayState(root, "t", base.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	d, ok := state[id]
	if !ok {
		t.Fatal("decision should be in state after restore")
	}
	if d.Summary != "Restored" {
		t.Errorf("summary = %q, want 'Restored'", d.Summary)
	}
}

func TestReplayCreateWithNilAfter(t *testing.T) {
	t.Parallel()
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	id := ulid.New()

	// Create event with no payload.After — should materialise an empty shell.
	create := makeEventAt(core.ActionCreate, core.KindDecision, "t", id, "u", base)
	must(t, audit.Append(root, create))

	state, err := audit.ReplayState(root, "t", base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	d, ok := state[id]
	if !ok {
		t.Fatal("create with nil after should still add to state")
	}
	if d.ID != id {
		t.Errorf("ID = %q, want %q", d.ID, id)
	}
}

func TestReplayDecideWithAfterFallback(t *testing.T) {
	t.Parallel()
	// Decide payload uses After instead of Extra (older format).
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	id := ulid.New()

	create := makeEventAt(core.ActionCreate, core.KindDecision, "t", id, "u", base)
	create.Payload = core.EventPayload{
		After: map[string]any{"id": id, "slug": "d", "summary": "D", "status": "proposed", "priority": "low", "creator": "u"},
	}
	// Use After (not Extra) for decide payload.
	decide := makeEventAt(core.ActionDecide, core.KindDecision, "t", id, "u", base.Add(time.Minute))
	decide.Payload = core.EventPayload{
		After: map[string]any{
			"actual_choice":        "Option B",
			"actual_choice_reason": "Reason B",
			"decided_by":           []any{"u"},
		},
	}

	must(t, audit.Append(root, create))
	must(t, audit.Append(root, decide))

	state, err := audit.ReplayState(root, "t", base.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	d := state[id]
	if d == nil {
		t.Fatal("decision should exist")
	}
	if d.ActualChoice != "Option B" {
		t.Errorf("actual_choice = %q, want 'Option B'", d.ActualChoice)
	}
}

// ---------------------------------------------------------------------------
// toRelationships coverage via audit replay
// ---------------------------------------------------------------------------

func TestReplayWithRelationshipsInAfter(t *testing.T) {
	t.Parallel()
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	id := ulid.New()
	tgt := ulid.New()

	// Create event with relationships in After.
	create := makeEventAt(core.ActionCreate, core.KindDecision, "t", id, "u", base)
	create.Payload = core.EventPayload{
		After: map[string]any{
			"id": id, "slug": "d", "summary": "D", "status": "proposed",
			"priority": "low", "creator": "u",
			"relationships": []any{
				map[string]any{"type": "blocks", "target": tgt},
			},
		},
	}
	must(t, audit.Append(root, create))

	state, err := audit.ReplayState(root, "t", base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	d := state[id]
	if d == nil {
		t.Fatal("decision should exist")
	}
	if len(d.Relationships) != 1 {
		t.Errorf("expected 1 relationship, got %d", len(d.Relationships))
	}
	if d.Relationships[0].Target != tgt {
		t.Errorf("relationship target = %q, want %q", d.Relationships[0].Target, tgt)
	}
}

func TestReplayUnrelateOnMissingDecision(t *testing.T) {
	t.Parallel()
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	id := ulid.New()

	// Unrelate without prior create — should be a no-op.
	unrelate := makeEventAt(core.ActionUnrelate, core.KindRelationship, "t", id, "u", base)
	unrelate.Payload = core.EventPayload{
		Extra: map[string]any{"type": "blocks", "target": ulid.New()},
	}
	must(t, audit.Append(root, unrelate))

	state, err := audit.ReplayState(root, "t", base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := state[id]; ok {
		t.Error("unrelate on missing decision should not create it")
	}
}

func TestReplayRelateWithNilExtra(t *testing.T) {
	t.Parallel()
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	id := ulid.New()

	create := makeEventAt(core.ActionCreate, core.KindDecision, "t", id, "u", base)
	create.Payload = core.EventPayload{
		After: map[string]any{"id": id, "slug": "d", "summary": "D", "status": "proposed", "priority": "low", "creator": "u"},
	}
	// Relate with no Extra (no type/target) — should not add a relationship.
	relate := makeEventAt(core.ActionRelate, core.KindRelationship, "t", id, "u", base.Add(time.Minute))

	must(t, audit.Append(root, create))
	must(t, audit.Append(root, relate))

	state, err := audit.ReplayState(root, "t", base.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	d := state[id]
	if d == nil {
		t.Fatal("decision should exist")
	}
	if len(d.Relationships) != 0 {
		t.Errorf("expected 0 relationships (nil Extra relate), got %d", len(d.Relationships))
	}
}

// ---------------------------------------------------------------------------
// discoverTreeAuditDirs when .decisions is missing
// ---------------------------------------------------------------------------

func TestReadNonExistentDecisionsDir(t *testing.T) {
	t.Parallel()
	// A root with no .decisions dir — should return empty, no error.
	root := t.TempDir()
	evs, err := audit.Read(root, audit.Filter{})
	if err != nil {
		t.Fatalf("Read with no .decisions dir: %v", err)
	}
	if len(evs) != 0 {
		t.Errorf("expected empty, got %d events", len(evs))
	}
}

// ---------------------------------------------------------------------------
// applyAfterFields schema_version branches
// ---------------------------------------------------------------------------

func TestReplaySchemaVersionFloat64(t *testing.T) {
	t.Parallel()
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	id := ulid.New()

	// schema_version as float64 (standard JSON number decoding).
	create := makeEventAt(core.ActionCreate, core.KindDecision, "t", id, "u", base)
	create.Payload = core.EventPayload{
		After: map[string]any{
			"id": id, "slug": "d", "summary": "D", "status": "proposed",
			"priority": "low", "creator": "u",
			"schema_version": float64(2),
		},
	}
	must(t, audit.Append(root, create))

	state, err := audit.ReplayState(root, "t", base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	d := state[id]
	if d == nil {
		t.Fatal("decision should exist")
	}
	if d.SchemaVersion != 2 {
		t.Errorf("schema_version = %d, want 2", d.SchemaVersion)
	}
}
