package audit_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/ulid"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func newRoot(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// makeEvent builds a minimal valid core.Event. Callers override fields as
// needed. EventID, V, and Ts are deliberately left zero to test default-fill.
func makeEvent(action core.Action, kind core.Kind, tree, id, actor string) core.Event {
	return core.Event{
		Actor:  actor,
		Action: action,
		Kind:   kind,
		Tree:   tree,
		ID:     id,
	}
}

// makeEventAt is like makeEvent but with an explicit timestamp (no fill).
func makeEventAt(action core.Action, kind core.Kind, tree, id, actor string, ts time.Time) core.Event {
	return core.Event{
		EventID: ulid.New(),
		V:       core.SchemaVersion,
		Ts:      ts,
		Actor:   actor,
		Action:  action,
		Kind:    kind,
		Tree:    tree,
		ID:      id,
	}
}

// ---------------------------------------------------------------------------
// AppendThenReadRoundTrip
// ---------------------------------------------------------------------------

func TestAppendThenReadRoundTrip(t *testing.T) {
	root := newRoot(t)

	evs := []core.Event{
		makeEventAt(core.ActionCreate, core.KindDecision, "alpha", "ID1", "alice", time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)),
		makeEventAt(core.ActionUpdate, core.KindDecision, "alpha", "ID1", "bob", time.Date(2026, 5, 1, 11, 0, 0, 0, time.UTC)),
		makeEventAt(core.ActionCreate, core.KindDecision, "beta", "ID2", "carol", time.Date(2026, 5, 2, 9, 0, 0, 0, time.UTC)),
		makeEventAt(core.ActionTreeCreate, core.KindTree, "", "alpha", "alice", time.Date(2026, 4, 15, 8, 0, 0, 0, time.UTC)),
	}

	for _, ev := range evs {
		if err := audit.Append(root, ev); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	got, err := audit.Read(root, audit.Filter{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != len(evs) {
		t.Fatalf("expected %d events, got %d", len(evs), len(got))
	}

	// Must be sorted by (Ts ASC, EventID ASC).
	for i := 1; i < len(got); i++ {
		ti, tj := got[i-1].Ts, got[i].Ts
		if ti.After(tj) {
			t.Errorf("event[%d] Ts %v > event[%d] Ts %v", i-1, ti, i, tj)
		}
		if ti.Equal(tj) && got[i-1].EventID > got[i].EventID {
			t.Errorf("event[%d] EventID %q > event[%d] EventID %q (same Ts, out of order)", i-1, got[i-1].EventID, i, got[i].EventID)
		}
	}
}

// ---------------------------------------------------------------------------
// FilterByActor
// ---------------------------------------------------------------------------

func TestFilterByActor(t *testing.T) {
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	must(t, audit.Append(root, makeEventAt(core.ActionCreate, core.KindDecision, "t", "A", "alice", base)))
	must(t, audit.Append(root, makeEventAt(core.ActionCreate, core.KindDecision, "t", "B", "bob", base.Add(time.Second))))
	must(t, audit.Append(root, makeEventAt(core.ActionUpdate, core.KindDecision, "t", "A", "alice", base.Add(2*time.Second))))

	got, err := audit.Read(root, audit.Filter{Actor: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 alice events, got %d", len(got))
	}
	for _, ev := range got {
		if ev.Actor != "alice" {
			t.Errorf("unexpected actor %q", ev.Actor)
		}
	}
}

// ---------------------------------------------------------------------------
// FilterByAction
// ---------------------------------------------------------------------------

func TestFilterByAction(t *testing.T) {
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	must(t, audit.Append(root, makeEventAt(core.ActionCreate, core.KindDecision, "t", "A", "u", base)))
	must(t, audit.Append(root, makeEventAt(core.ActionUpdate, core.KindDecision, "t", "A", "u", base.Add(time.Second))))
	must(t, audit.Append(root, makeEventAt(core.ActionDelete, core.KindDecision, "t", "A", "u", base.Add(2*time.Second))))

	got, err := audit.Read(root, audit.Filter{Action: core.ActionUpdate})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Action != core.ActionUpdate {
		t.Fatalf("expected 1 update event, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// FilterByTree
// ---------------------------------------------------------------------------

func TestFilterByTree(t *testing.T) {
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	must(t, audit.Append(root, makeEventAt(core.ActionCreate, core.KindDecision, "alpha", "A", "u", base)))
	must(t, audit.Append(root, makeEventAt(core.ActionCreate, core.KindDecision, "beta", "B", "u", base.Add(time.Second))))

	got, err := audit.Read(root, audit.Filter{Tree: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Tree != "alpha" {
		t.Fatalf("expected 1 alpha event, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// FilterByTimeRange
// ---------------------------------------------------------------------------

func TestFilterByTimeRange(t *testing.T) {
	root := newRoot(t)

	t1 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	t3 := t2.Add(time.Hour)
	t4 := t3.Add(time.Hour)

	must(t, audit.Append(root, makeEventAt(core.ActionCreate, core.KindDecision, "t", "A", "u", t1)))
	must(t, audit.Append(root, makeEventAt(core.ActionCreate, core.KindDecision, "t", "B", "u", t2)))
	must(t, audit.Append(root, makeEventAt(core.ActionCreate, core.KindDecision, "t", "C", "u", t3)))
	must(t, audit.Append(root, makeEventAt(core.ActionCreate, core.KindDecision, "t", "D", "u", t4)))

	got, err := audit.Read(root, audit.Filter{Since: t2, Until: t3})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 events in [t2,t3], got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// AppendDefaultsFields
// ---------------------------------------------------------------------------

func TestAppendDefaultsFields(t *testing.T) {
	root := newRoot(t)

	ev := makeEvent(core.ActionCreate, core.KindDecision, "t", "ID1", "u")
	// All of EventID, Ts, V are zero.

	before := time.Now().UTC()
	if err := audit.Append(root, ev); err != nil {
		t.Fatal(err)
	}
	after := time.Now().UTC()

	got, err := audit.Read(root, audit.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	g := got[0]
	if g.EventID == "" {
		t.Error("EventID should have been filled")
	}
	if g.V == 0 {
		t.Error("V should have been set to SchemaVersion")
	}
	if g.Ts.IsZero() {
		t.Error("Ts should have been filled")
	}
	if g.Ts.Before(before) || g.Ts.After(after) {
		t.Errorf("Ts %v not in [%v, %v]", g.Ts, before, after)
	}
}

// ---------------------------------------------------------------------------
// AppendPreservesProvidedFields
// ---------------------------------------------------------------------------

func TestAppendPreservesProvidedFields(t *testing.T) {
	root := newRoot(t)

	fixedID := ulid.New()
	fixedTs := time.Date(2025, 1, 15, 12, 0, 0, 0, time.UTC)
	const fixedV = 99

	ev := core.Event{
		EventID: fixedID,
		V:       fixedV,
		Ts:      fixedTs,
		Actor:   "u",
		Action:  core.ActionCreate,
		Kind:    core.KindDecision,
		Tree:    "t",
		ID:      "ID1",
	}
	if err := audit.Append(root, ev); err != nil {
		t.Fatal(err)
	}

	got, err := audit.Read(root, audit.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	g := got[0]
	if g.EventID != fixedID {
		t.Errorf("EventID: got %q, want %q", g.EventID, fixedID)
	}
	if g.V != fixedV {
		t.Errorf("V: got %d, want %d", g.V, fixedV)
	}
	if !g.Ts.Equal(fixedTs) {
		t.Errorf("Ts: got %v, want %v", g.Ts, fixedTs)
	}
}

// ---------------------------------------------------------------------------
// MonthPartitioning
// ---------------------------------------------------------------------------

func TestMonthPartitioning(t *testing.T) {
	root := newRoot(t)

	may := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	jun := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	must(t, audit.Append(root, makeEventAt(core.ActionCreate, core.KindDecision, "t", "A", "u", may)))
	must(t, audit.Append(root, makeEventAt(core.ActionCreate, core.KindDecision, "t", "B", "u", jun)))

	auditDir := filepath.Join(root, ".decisions", "t", "audit")
	entries, err := os.ReadDir(auditDir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	wantFiles := map[string]bool{"2026-05.jsonl": false, "2026-06.jsonl": false}
	for _, n := range names {
		if _, ok := wantFiles[n]; ok {
			wantFiles[n] = true
		}
	}
	for name, found := range wantFiles {
		if !found {
			t.Errorf("expected file %q in %v", name, names)
		}
	}
}

// ---------------------------------------------------------------------------
// ReplayCreateOnly
// ---------------------------------------------------------------------------

func TestReplayCreateOnly(t *testing.T) {
	root := newRoot(t)
	ts := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	id := ulid.New()

	ev := makeEventAt(core.ActionCreate, core.KindDecision, "myTree", id, "alice", ts)
	ev.Payload = core.EventPayload{
		After: map[string]any{
			"id":       id,
			"slug":     "first-decision",
			"summary":  "First decision",
			"status":   "proposed",
			"priority": "medium",
			"creator":  "alice",
		},
	}
	must(t, audit.Append(root, ev))

	state, err := audit.ReplayState(root, "myTree", ts.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	d, ok := state[id]
	if !ok {
		t.Fatalf("decision %q not in replayed state", id)
	}
	if d.Summary != "First decision" {
		t.Errorf("summary: got %q", d.Summary)
	}
	if d.Status != core.StatusProposed {
		t.Errorf("status: got %q", d.Status)
	}
}

// ---------------------------------------------------------------------------
// ReplayUpdate
// ---------------------------------------------------------------------------

func TestReplayUpdate(t *testing.T) {
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	id := ulid.New()

	create := makeEventAt(core.ActionCreate, core.KindDecision, "t", id, "alice", base)
	create.Payload = core.EventPayload{
		After: map[string]any{
			"id":       id,
			"slug":     "orig",
			"summary":  "Original",
			"status":   "proposed",
			"priority": "low",
			"creator":  "alice",
		},
	}
	update := makeEventAt(core.ActionUpdate, core.KindDecision, "t", id, "alice", base.Add(time.Minute))
	update.Payload = core.EventPayload{
		Before: map[string]any{"summary": "Original"},
		After:  map[string]any{"summary": "Updated summary"},
	}

	must(t, audit.Append(root, create))
	must(t, audit.Append(root, update))

	state, err := audit.ReplayState(root, "t", base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	d, ok := state[id]
	if !ok {
		t.Fatal("decision not in state")
	}
	if d.Summary != "Updated summary" {
		t.Errorf("summary: got %q", d.Summary)
	}
}

// ---------------------------------------------------------------------------
// ReplayDelete
// ---------------------------------------------------------------------------

func TestReplayDelete(t *testing.T) {
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	id := ulid.New()

	create := makeEventAt(core.ActionCreate, core.KindDecision, "t", id, "alice", base)
	create.Payload = core.EventPayload{
		After: map[string]any{"id": id, "slug": "d", "summary": "D", "status": "proposed", "priority": "low", "creator": "alice"},
	}
	del := makeEventAt(core.ActionDelete, core.KindDecision, "t", id, "alice", base.Add(time.Minute))

	must(t, audit.Append(root, create))
	must(t, audit.Append(root, del))

	state, err := audit.ReplayState(root, "t", base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := state[id]; ok {
		t.Error("deleted decision should not appear in state")
	}
}

// ---------------------------------------------------------------------------
// ReplayDecide
// ---------------------------------------------------------------------------

func TestReplayDecide(t *testing.T) {
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	id := ulid.New()

	create := makeEventAt(core.ActionCreate, core.KindDecision, "t", id, "alice", base)
	create.Payload = core.EventPayload{
		After: map[string]any{"id": id, "slug": "d", "summary": "D", "status": "proposed", "priority": "low", "creator": "alice"},
	}
	decide := makeEventAt(core.ActionDecide, core.KindDecision, "t", id, "alice", base.Add(time.Minute))
	decide.Payload = core.EventPayload{
		Extra: map[string]any{
			"actual_choice":        "Go with option A",
			"actual_choice_reason": "It's cheaper",
			"decided_by":           []any{"alice", "bob"},
			"is_recommended":       true,
		},
	}

	must(t, audit.Append(root, create))
	must(t, audit.Append(root, decide))

	state, err := audit.ReplayState(root, "t", base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	d, ok := state[id]
	if !ok {
		t.Fatal("decision not in state")
	}
	if d.Status != core.StatusDecided {
		t.Errorf("status: got %q want decided", d.Status)
	}
	if d.ActualChoice != "Go with option A" {
		t.Errorf("actual_choice: %q", d.ActualChoice)
	}
	if !d.IsRecommended {
		t.Error("is_recommended should be true")
	}
}

// ---------------------------------------------------------------------------
// ReplayAt — cutoff is honored
// ---------------------------------------------------------------------------

func TestReplayAt(t *testing.T) {
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	id := ulid.New()

	create := makeEventAt(core.ActionCreate, core.KindDecision, "t", id, "alice", base)
	create.Payload = core.EventPayload{
		After: map[string]any{"id": id, "slug": "d", "summary": "Original", "status": "proposed", "priority": "low", "creator": "alice"},
	}
	update := makeEventAt(core.ActionUpdate, core.KindDecision, "t", id, "alice", base.Add(2*time.Hour))
	update.Payload = core.EventPayload{
		After: map[string]any{"summary": "After cutoff"},
	}

	must(t, audit.Append(root, create))
	must(t, audit.Append(root, update))

	// Replay at base+1h: update at base+2h should NOT be applied.
	state, err := audit.ReplayState(root, "t", base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	d, ok := state[id]
	if !ok {
		t.Fatal("decision not in state")
	}
	if d.Summary != "Original" {
		t.Errorf("summary: got %q, want Original", d.Summary)
	}
}

// ---------------------------------------------------------------------------
// EmptyDirReturnsEmpty
// ---------------------------------------------------------------------------

func TestEmptyDirReturnsEmpty(t *testing.T) {
	root := newRoot(t)

	got, err := audit.Read(root, audit.Filter{})
	if err != nil {
		t.Fatalf("Read on missing audit dir: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}

	state, err := audit.ReplayState(root, "nonexistent", time.Now())
	if err != nil {
		t.Fatalf("ReplayState on missing dir: %v", err)
	}
	if len(state) != 0 {
		t.Errorf("expected empty map, got %v", state)
	}
}

// ---------------------------------------------------------------------------
// PayloadJSONFlattening
// ---------------------------------------------------------------------------

func TestPayloadJSONFlattening(t *testing.T) {
	original := core.EventPayload{
		Before: map[string]any{"status": "proposed"},
		After:  map[string]any{"status": "decided"},
		Extra:  map[string]any{"type": "blocks", "target": "TARGETID"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Verify the JSON is flat (extra keys at top level alongside before/after).
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	for _, key := range []string{"before", "after", "type", "target"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("key %q missing from flattened JSON", key)
		}
	}

	// Unmarshal back and check round-trip.
	var got core.EventPayload
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Before["status"] != "proposed" {
		t.Errorf("before.status: got %v", got.Before["status"])
	}
	if got.After["status"] != "decided" {
		t.Errorf("after.status: got %v", got.After["status"])
	}
	if got.Extra["type"] != "blocks" {
		t.Errorf("extra.type: got %v", got.Extra["type"])
	}
	if got.Extra["target"] != "TARGETID" {
		t.Errorf("extra.target: got %v", got.Extra["target"])
	}
}

// ---------------------------------------------------------------------------
// Limit filter
// ---------------------------------------------------------------------------

func TestFilterLimit(t *testing.T) {
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	for i := 0; i < 5; i++ {
		ev := makeEventAt(core.ActionCreate, core.KindDecision, "t", ulid.New(), "u", base.Add(time.Duration(i)*time.Second))
		must(t, audit.Append(root, ev))
	}

	got, err := audit.Read(root, audit.Filter{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 events with Limit=3, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// FilterByKind
// ---------------------------------------------------------------------------

func TestFilterByKind(t *testing.T) {
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	must(t, audit.Append(root, makeEventAt(core.ActionCreate, core.KindDecision, "t", "D1", "u", base)))
	must(t, audit.Append(root, makeEventAt(core.ActionTreeCreate, core.KindTree, "", "t", "u", base.Add(time.Second))))

	got, err := audit.Read(root, audit.Filter{Kind: core.KindTree})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Kind != core.KindTree {
		t.Fatalf("expected 1 tree event, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// FilterByTargetID
// ---------------------------------------------------------------------------

func TestFilterByTargetID(t *testing.T) {
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	must(t, audit.Append(root, makeEventAt(core.ActionCreate, core.KindDecision, "t", "ID_A", "u", base)))
	must(t, audit.Append(root, makeEventAt(core.ActionCreate, core.KindDecision, "t", "ID_B", "u", base.Add(time.Second))))

	got, err := audit.Read(root, audit.Filter{TargetID: "ID_A"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "ID_A" {
		t.Fatalf("expected 1 event for ID_A, got %v", got)
	}
}

// ---------------------------------------------------------------------------
// ReplayRelate / ReplayUnrelate
// ---------------------------------------------------------------------------

func TestReplayRelateUnrelate(t *testing.T) {
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	src := ulid.New()
	tgt := ulid.New()

	create := makeEventAt(core.ActionCreate, core.KindDecision, "t", src, "u", base)
	create.Payload = core.EventPayload{After: map[string]any{"id": src, "slug": "s", "summary": "S", "status": "proposed", "priority": "low", "creator": "u"}}

	relate := makeEventAt(core.ActionRelate, core.KindRelationship, "t", src, "u", base.Add(time.Minute))
	relate.Payload = core.EventPayload{Extra: map[string]any{"type": "blocks", "target": tgt}}

	unrelate := makeEventAt(core.ActionUnrelate, core.KindRelationship, "t", src, "u", base.Add(2*time.Minute))
	unrelate.Payload = core.EventPayload{Extra: map[string]any{"type": "blocks", "target": tgt}}

	must(t, audit.Append(root, create))
	must(t, audit.Append(root, relate))

	stateAfterRelate, err := audit.ReplayState(root, "t", base.Add(90*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	d := stateAfterRelate[src]
	if d == nil || len(d.Relationships) != 1 {
		t.Fatalf("expected 1 relationship after relate, got %v", d)
	}

	must(t, audit.Append(root, unrelate))
	stateAfterUnrelate, err := audit.ReplayState(root, "t", base.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	d = stateAfterUnrelate[src]
	if d == nil || len(d.Relationships) != 0 {
		t.Fatalf("expected 0 relationships after unrelate, got %v", d)
	}
}

// ---------------------------------------------------------------------------
// ExternalCreate/Edit/Delete treated like create/update/delete
// ---------------------------------------------------------------------------

func TestReplayExternalActions(t *testing.T) {
	root := newRoot(t)
	base := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	id := ulid.New()

	extCreate := makeEventAt(core.ActionExternalCreate, core.KindDecision, "t", id, "u", base)
	extCreate.Payload = core.EventPayload{After: map[string]any{"id": id, "slug": "ec", "summary": "ECreated", "status": "proposed", "priority": "low", "creator": "u"}}

	extEdit := makeEventAt(core.ActionExternalEdit, core.KindDecision, "t", id, "u", base.Add(time.Minute))
	extEdit.Payload = core.EventPayload{After: map[string]any{"summary": "EEdited"}}

	extDelete := makeEventAt(core.ActionExternalDelete, core.KindDecision, "t", id, "u", base.Add(2*time.Minute))

	must(t, audit.Append(root, extCreate))

	s, err := audit.ReplayState(root, "t", base.Add(30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s[id]; !ok {
		t.Fatal("extCreate should create decision")
	}

	must(t, audit.Append(root, extEdit))
	s, err = audit.ReplayState(root, "t", base.Add(90*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if s[id] == nil || s[id].Summary != "EEdited" {
		t.Fatal("extEdit should update summary")
	}

	must(t, audit.Append(root, extDelete))
	s, err = audit.ReplayState(root, "t", base.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s[id]; ok {
		t.Fatal("extDelete should remove decision")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
