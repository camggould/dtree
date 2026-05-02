package index

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/fsutil"
	"github.com/cgould/dtree/internal/storage"
	"github.com/cgould/dtree/internal/ulid"
)

// ---------------------------------------------------------------------------
// synthetic-repo helpers
// ---------------------------------------------------------------------------

// makeDecisionsDir creates .decisions/ and writes empty trees.yaml +
// actors.yaml. Returns the repo root and decisions dir path.
func makeDecisionsDir(t *testing.T) (repoRoot, decDir string) {
	t.Helper()
	repoRoot = t.TempDir()
	decDir = filepath.Join(repoRoot, ".decisions")
	if err := os.MkdirAll(decDir, 0o755); err != nil {
		t.Fatalf("mkdir .decisions: %v", err)
	}
	writeTreesYAML(t, repoRoot, nil)
	writeActorsYAML(t, repoRoot, nil)
	return repoRoot, decDir
}

func writeTreesYAML(t *testing.T, repoRoot string, slugs []string) {
	t.Helper()
	tf := &storage.TreesFile{SchemaVersion: 1, Trees: slugs}
	path := filepath.Join(repoRoot, ".decisions", storage.TreesFileName)
	if err := storage.WriteTrees(path, tf); err != nil {
		t.Fatalf("write trees.yaml: %v", err)
	}
}

func writeActorsYAML(t *testing.T, repoRoot string, actors []core.Actor) {
	t.Helper()
	af := &storage.ActorsFile{SchemaVersion: 1, Actors: actors}
	path := filepath.Join(repoRoot, ".decisions", storage.ActorsFileName)
	if err := storage.WriteActors(path, af); err != nil {
		t.Fatalf("write actors.yaml: %v", err)
	}
}

// writeTreeDir creates the tree directory, tree.yaml, and decisions/ subdir.
func writeTreeDir(t *testing.T, repoRoot, slug string) {
	t.Helper()
	treeDir := filepath.Join(repoRoot, ".decisions", slug)
	if err := os.MkdirAll(filepath.Join(treeDir, "decisions"), 0o755); err != nil {
		t.Fatalf("mkdir tree dir: %v", err)
	}
	tree := &core.Tree{
		Slug:      slug,
		Title:     slug + " title",
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	tree.Layout.Direction = "TB"
	if err := storage.WriteTree(filepath.Join(treeDir, storage.TreeMetaFileName), tree); err != nil {
		t.Fatalf("write tree.yaml: %v", err)
	}
}

// writeDecisionFile writes a decision YAML under <tree>/decisions/<id>-<slug>.yaml.
func writeDecisionFile(t *testing.T, repoRoot string, d *core.Decision) string {
	t.Helper()
	path := filepath.Join(
		repoRoot, ".decisions", d.Tree,
		"decisions", d.ID+"-"+d.Slug+".yaml",
	)
	if err := storage.WriteDecision(path, d); err != nil {
		t.Fatalf("write decision %s: %v", d.ID, err)
	}
	return path
}

// makeDecision returns a minimal valid Decision using a generated ULID.
func makeDecision(tree, slug, summary string) *core.Decision {
	return &core.Decision{
		ID:            ulid.New(),
		Tree:          tree,
		Slug:          slug,
		Summary:       summary,
		Status:        core.StatusProposed,
		Priority:      core.PriorityMedium,
		Creator:       "alice",
		SchemaVersion: core.SchemaVersion,
	}
}

// openRepoIndex opens a fresh SQLite index in the repo's .decisions/ dir.
func openRepoIndex(t *testing.T, repoRoot string) *DB {
	t.Helper()
	decDir := filepath.Join(repoRoot, ".decisions")
	if err := os.MkdirAll(decDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	db, err := Open(filepath.Join(decDir, ".index.db"))
	if err != nil {
		t.Fatalf("Open index: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// writeAuditEvent appends a minimal event to the audit log.
func writeAuditEvent(t *testing.T, repoRoot, tree, id string, action core.Action) {
	t.Helper()
	ev := core.Event{
		EventID: ulid.New(),
		V:       core.SchemaVersion,
		Ts:      time.Now().UTC(),
		Actor:   "alice",
		Action:  action,
		Kind:    core.KindDecision,
		Tree:    tree,
		ID:      id,
		Payload: core.EventPayload{
			After: map[string]any{"summary": "test"},
		},
	}
	if err := audit.Append(repoRoot, ev); err != nil {
		t.Fatalf("append audit event: %v", err)
	}
}

// countRows returns the number of rows in the given table.
func countRows(t *testing.T, db *DB, table string) int {
	t.Helper()
	var n int
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// ---------------------------------------------------------------------------
// TestReindexEmptyRepo
// ---------------------------------------------------------------------------

func TestReindexEmptyRepo(t *testing.T) {
	repoRoot, _ := makeDecisionsDir(t)
	db := openRepoIndex(t, repoRoot)

	report, err := Reindex(repoRoot, db, nil)
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	if report.Trees != 0 {
		t.Errorf("Trees = %d, want 0", report.Trees)
	}
	if report.Actors != 0 {
		t.Errorf("Actors = %d, want 0", report.Actors)
	}
	if report.Decisions != 0 {
		t.Errorf("Decisions = %d, want 0", report.Decisions)
	}
	if report.Events != 0 {
		t.Errorf("Events = %d, want 0", report.Events)
	}
}

// ---------------------------------------------------------------------------
// TestReindexBasicSnapshot
// ---------------------------------------------------------------------------

func TestReindexBasicSnapshot(t *testing.T) {
	repoRoot, _ := makeDecisionsDir(t)

	// Two trees.
	writeTreesYAML(t, repoRoot, []string{"alpha", "beta"})
	writeTreeDir(t, repoRoot, "alpha")
	writeTreeDir(t, repoRoot, "beta")

	// Two actors.
	writeActorsYAML(t, repoRoot, []core.Actor{
		{Handle: "alice", Name: "Alice", Kind: core.ActorHuman, Active: true},
		{Handle: "bob", Name: "Bob", Kind: core.ActorHuman, Active: true},
	})

	// 4 decisions: 2 in alpha, 2 in beta.
	d1 := makeDecision("alpha", "slug-1", "Alpha decision one")
	d1.Tags = []string{"infra"}

	d2 := makeDecision("alpha", "slug-2", "Alpha decision two")
	d1.Relationships = []core.Relationship{
		{Type: core.RelBlocks, Target: d2.ID},
	}

	d3 := makeDecision("beta", "slug-3", "Beta decision one")
	d4 := makeDecision("beta", "slug-4", "Beta decision two")

	writeDecisionFile(t, repoRoot, d1)
	writeDecisionFile(t, repoRoot, d2)
	writeDecisionFile(t, repoRoot, d3)
	writeDecisionFile(t, repoRoot, d4)

	db := openRepoIndex(t, repoRoot)
	report, err := Reindex(repoRoot, db, nil)
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	if report.Trees != 2 {
		t.Errorf("Trees = %d, want 2", report.Trees)
	}
	if report.Actors != 2 {
		t.Errorf("Actors = %d, want 2", report.Actors)
	}
	if report.Decisions != 4 {
		t.Errorf("Decisions = %d, want 4", report.Decisions)
	}
	if report.Relationships != 1 {
		t.Errorf("Relationships = %d, want 1", report.Relationships)
	}

	// GetDecision should return correct data.
	got, err := GetDecision(db, d1.ID)
	if err != nil {
		t.Fatalf("GetDecision: %v", err)
	}
	if got == nil {
		t.Fatal("GetDecision returned nil for d1")
	}
	if got.Summary != "Alpha decision one" {
		t.Errorf("Summary = %q, want %q", got.Summary, "Alpha decision one")
	}
	if got.Tree != "alpha" {
		t.Errorf("Tree = %q, want alpha", got.Tree)
	}
	if len(got.Tags) != 1 || got.Tags[0] != "infra" {
		t.Errorf("Tags = %v, want [infra]", got.Tags)
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("Relationships len = %d, want 1", len(got.Relationships))
	}
	if got.Relationships[0].Target != d2.ID || got.Relationships[0].Type != core.RelBlocks {
		t.Errorf("unexpected relationship: %+v", got.Relationships[0])
	}

	// All four decisions should be in the DB.
	if c := countRows(t, db, "decisions"); c != 4 {
		t.Errorf("decisions count = %d, want 4", c)
	}
}

// ---------------------------------------------------------------------------
// TestReindexDetectsDuplicateIDs
// ---------------------------------------------------------------------------

func TestReindexDetectsDuplicateIDs(t *testing.T) {
	repoRoot, _ := makeDecisionsDir(t)

	writeTreesYAML(t, repoRoot, []string{"tree1", "tree2"})
	writeTreeDir(t, repoRoot, "tree1")
	writeTreeDir(t, repoRoot, "tree2")

	sharedID := ulid.New()

	// Write the same ID in two different trees.
	d1 := &core.Decision{
		ID: sharedID, Tree: "tree1", Slug: "slug-dup-1", Summary: "First copy",
		Status: core.StatusProposed, Priority: core.PriorityMedium,
		Creator: "alice", SchemaVersion: core.SchemaVersion,
	}
	d2 := &core.Decision{
		ID: sharedID, Tree: "tree2", Slug: "slug-dup-2", Summary: "Second copy",
		Status: core.StatusProposed, Priority: core.PriorityMedium,
		Creator: "alice", SchemaVersion: core.SchemaVersion,
	}

	writeDecisionFile(t, repoRoot, d1)
	writeDecisionFile(t, repoRoot, d2)

	db := openRepoIndex(t, repoRoot)

	// Seed some existing data to verify it is NOT mutated on error
	// (error is detected before the transaction opens).
	_, _ = db.conn.Exec(`INSERT INTO trees(slug, created_at) VALUES('pre-existing','2024-01-01T00:00:00Z')`)

	_, err := Reindex(repoRoot, db, nil)
	if err == nil {
		t.Fatal("Reindex with duplicate IDs: expected error, got nil")
	}
	if !strings.Contains(err.Error(), sharedID) {
		t.Errorf("error does not mention the duplicate ID %q: %v", sharedID, err)
	}

	// Both file paths should appear in the error message.
	if !strings.Contains(err.Error(), "tree1") {
		t.Errorf("error missing tree1 path: %v", err)
	}
	if !strings.Contains(err.Error(), "tree2") {
		t.Errorf("error missing tree2 path: %v", err)
	}

	// The DB should be unchanged (error detected in pre-flight scan,
	// transaction was never opened).
	var count int
	db.conn.QueryRow(`SELECT COUNT(*) FROM trees WHERE slug='pre-existing'`).Scan(&count)
	if count != 1 {
		t.Errorf("pre-existing tree row was removed despite pre-flight error; count=%d", count)
	}
}

// ---------------------------------------------------------------------------
// TestReindexClearsExistingData
// ---------------------------------------------------------------------------

func TestReindexClearsExistingData(t *testing.T) {
	repoRoot, _ := makeDecisionsDir(t)

	db := openRepoIndex(t, repoRoot)

	// Pre-populate with stale data.
	_, err := db.conn.Exec(`INSERT INTO trees(slug, created_at) VALUES('stale-tree','2020-01-01T00:00:00Z')`)
	if err != nil {
		t.Fatalf("pre-insert stale tree: %v", err)
	}

	// Reindex with no YAML trees → stale data should be gone.
	_, err = Reindex(repoRoot, db, nil)
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	var count int
	db.conn.QueryRow(`SELECT COUNT(*) FROM trees`).Scan(&count)
	if count != 0 {
		t.Errorf("trees count after reindex = %d, want 0 (stale data should be cleared)", count)
	}
}

// ---------------------------------------------------------------------------
// TestReindexLoadsAuditEvents
// ---------------------------------------------------------------------------

func TestReindexLoadsAuditEvents(t *testing.T) {
	repoRoot, _ := makeDecisionsDir(t)

	writeTreesYAML(t, repoRoot, []string{"tree1"})
	writeTreeDir(t, repoRoot, "tree1")

	d := makeDecision("tree1", "evt-slug", "Decision for event test")
	writeDecisionFile(t, repoRoot, d)

	// Write 3 audit events.
	writeAuditEvent(t, repoRoot, "tree1", d.ID, core.ActionCreate)
	writeAuditEvent(t, repoRoot, "tree1", d.ID, core.ActionUpdate)
	writeAuditEvent(t, repoRoot, "tree1", d.ID, core.ActionDecide)

	db := openRepoIndex(t, repoRoot)
	report, err := Reindex(repoRoot, db, nil)
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	if report.Events != 3 {
		t.Errorf("Events = %d, want 3", report.Events)
	}
	if c := countRows(t, db, "events"); c != 3 {
		t.Errorf("events table count = %d, want 3", c)
	}
}

// ---------------------------------------------------------------------------
// TestReindexIdempotent
// ---------------------------------------------------------------------------

func TestReindexIdempotent(t *testing.T) {
	repoRoot, _ := makeDecisionsDir(t)

	writeTreesYAML(t, repoRoot, []string{"alpha"})
	writeTreeDir(t, repoRoot, "alpha")
	writeActorsYAML(t, repoRoot, []core.Actor{
		{Handle: "alice", Kind: core.ActorHuman, Active: true},
	})
	writeDecisionFile(t, repoRoot, makeDecision("alpha", "slug-idem", "Idempotent test"))

	db := openRepoIndex(t, repoRoot)

	r1, err := Reindex(repoRoot, db, nil)
	if err != nil {
		t.Fatalf("first Reindex: %v", err)
	}
	r2, err := Reindex(repoRoot, db, nil)
	if err != nil {
		t.Fatalf("second Reindex: %v", err)
	}

	if r1.Trees != r2.Trees {
		t.Errorf("Trees: %d vs %d", r1.Trees, r2.Trees)
	}
	if r1.Actors != r2.Actors {
		t.Errorf("Actors: %d vs %d", r1.Actors, r2.Actors)
	}
	if r1.Decisions != r2.Decisions {
		t.Errorf("Decisions: %d vs %d", r1.Decisions, r2.Decisions)
	}

	// DB state should match the second run.
	if c := countRows(t, db, "decisions"); c != 1 {
		t.Errorf("decisions count after two runs = %d, want 1", c)
	}
}

// ---------------------------------------------------------------------------
// TestReindexHonorsLock
// ---------------------------------------------------------------------------

// TestReindexHonorsLock verifies that when a goroutine holds .index.lock,
// a concurrent Reindex call blocks (not fails) and eventually succeeds once
// the lock is released. We use AcquireLock (non-blocking) for the competing
// goroutine so the test is deterministic.
//
// Behavior: Reindex uses AcquireLockBlocking, so the second call will wait
// until the first goroutine releases the lock, then succeed.
func TestReindexHonorsLock(t *testing.T) {
	repoRoot, _ := makeDecisionsDir(t)
	db := openRepoIndex(t, repoRoot)

	lockPath := filepath.Join(repoRoot, ".decisions", indexLockFile)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Acquire lock from main goroutine.
	lock, err := fsutil.AcquireLock(lockPath)
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}

	var (
		reindexDone = make(chan error, 1)
		started     = make(chan struct{})
	)
	go func() {
		close(started)
		_, err := Reindex(repoRoot, db, nil)
		reindexDone <- err
	}()

	<-started
	// Give the goroutine a moment to reach the lock attempt.
	time.Sleep(50 * time.Millisecond)

	// Release the lock — Reindex should now proceed.
	if err := lock.Release(); err != nil {
		t.Fatalf("release lock: %v", err)
	}

	select {
	case err := <-reindexDone:
		if err != nil {
			t.Errorf("Reindex after lock release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("Reindex did not complete within 5s after lock was released")
	}
}

// ---------------------------------------------------------------------------
// TestReindexClearsDirty
// ---------------------------------------------------------------------------

func TestReindexClearsDirty(t *testing.T) {
	repoRoot, _ := makeDecisionsDir(t)
	db := openRepoIndex(t, repoRoot)

	if err := MarkDirty(repoRoot); err != nil {
		t.Fatalf("MarkDirty: %v", err)
	}
	dirty, err := IsDirty(repoRoot)
	if err != nil || !dirty {
		t.Fatalf("expected dirty=true before reindex; dirty=%v err=%v", dirty, err)
	}

	if _, err := Reindex(repoRoot, db, nil); err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	dirty, err = IsDirty(repoRoot)
	if err != nil {
		t.Fatalf("IsDirty after reindex: %v", err)
	}
	if dirty {
		t.Error(".dirty marker still present after successful Reindex")
	}
}

// ---------------------------------------------------------------------------
// TestReindexLeavesDirtyOnError
// ---------------------------------------------------------------------------

func TestReindexLeavesDirtyOnError(t *testing.T) {
	repoRoot, _ := makeDecisionsDir(t)

	// Create a duplicate ID situation to cause a pre-flight error.
	writeTreesYAML(t, repoRoot, []string{"t1", "t2"})
	writeTreeDir(t, repoRoot, "t1")
	writeTreeDir(t, repoRoot, "t2")

	sharedID := ulid.New()
	d1 := &core.Decision{
		ID: sharedID, Tree: "t1", Slug: "s1", Summary: "First",
		Status: core.StatusProposed, Priority: core.PriorityMedium,
		Creator: "alice", SchemaVersion: core.SchemaVersion,
	}
	d2 := &core.Decision{
		ID: sharedID, Tree: "t2", Slug: "s2", Summary: "Second",
		Status: core.StatusProposed, Priority: core.PriorityMedium,
		Creator: "alice", SchemaVersion: core.SchemaVersion,
	}
	writeDecisionFile(t, repoRoot, d1)
	writeDecisionFile(t, repoRoot, d2)

	if err := MarkDirty(repoRoot); err != nil {
		t.Fatalf("MarkDirty: %v", err)
	}

	db := openRepoIndex(t, repoRoot)
	_, err := Reindex(repoRoot, db, nil)
	if err == nil {
		t.Fatal("expected error from duplicate IDs, got nil")
	}

	// .dirty must still be present.
	dirty, dirtyErr := IsDirty(repoRoot)
	if dirtyErr != nil {
		t.Fatalf("IsDirty: %v", dirtyErr)
	}
	if !dirty {
		t.Error(".dirty marker was cleared despite Reindex error")
	}
}

// ---------------------------------------------------------------------------
// TestMarkAndClearDirty
// ---------------------------------------------------------------------------

func TestMarkAndClearDirty(t *testing.T) {
	repoRoot, _ := makeDecisionsDir(t)

	dirty, err := IsDirty(repoRoot)
	if err != nil {
		t.Fatalf("IsDirty initial: %v", err)
	}
	if dirty {
		t.Error("expected not dirty initially")
	}

	if err := MarkDirty(repoRoot); err != nil {
		t.Fatalf("MarkDirty: %v", err)
	}
	dirty, err = IsDirty(repoRoot)
	if err != nil {
		t.Fatalf("IsDirty after mark: %v", err)
	}
	if !dirty {
		t.Error("expected dirty after MarkDirty")
	}

	if err := ClearDirty(repoRoot); err != nil {
		t.Fatalf("ClearDirty: %v", err)
	}
	dirty, err = IsDirty(repoRoot)
	if err != nil {
		t.Fatalf("IsDirty after clear: %v", err)
	}
	if dirty {
		t.Error("expected not dirty after ClearDirty")
	}

	// ClearDirty when no marker is a no-op.
	if err := ClearDirty(repoRoot); err != nil {
		t.Errorf("ClearDirty with no marker: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestReindexProgressCallback
// ---------------------------------------------------------------------------

func TestReindexProgressCallback(t *testing.T) {
	repoRoot, _ := makeDecisionsDir(t)
	db := openRepoIndex(t, repoRoot)

	var mu sync.Mutex
	var phases []string
	progress := func(msg string) {
		mu.Lock()
		phases = append(phases, msg)
		mu.Unlock()
	}

	if _, err := Reindex(repoRoot, db, progress); err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	expected := []string{
		"acquiring lock",
		"scanning trees",
		"scanning actors",
		"loading decisions",
		"loading audit events",
		"clearing tables",
		"inserting trees",
		"inserting actors",
		"inserting decisions",
		"inserting events",
		"committing",
		"done",
	}

	mu.Lock()
	got := phases
	mu.Unlock()

	for _, want := range expected {
		found := false
		for _, p := range got {
			if p == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("progress callback missing phase %q; got %v", want, got)
		}
	}
}

// ---------------------------------------------------------------------------
// Additional: missing actors.yaml / trees.yaml
// ---------------------------------------------------------------------------

// TestReindexMissingActorsYAML verifies that a missing actors.yaml is treated
// as "no actors" rather than an error.
func TestReindexMissingActorsYAML(t *testing.T) {
	repoRoot := t.TempDir()
	decDir := filepath.Join(repoRoot, ".decisions")
	if err := os.MkdirAll(decDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Only trees.yaml, no actors.yaml.
	writeTreesYAML(t, repoRoot, nil)

	db := openRepoIndex(t, repoRoot)
	report, err := Reindex(repoRoot, db, nil)
	if err != nil {
		t.Fatalf("Reindex with missing actors.yaml: %v", err)
	}
	if report.Actors != 0 {
		t.Errorf("Actors = %d, want 0", report.Actors)
	}
}

// TestReindexMissingTreeDir verifies that a tree slug in trees.yaml whose
// directory doesn't exist on disk emits a warning and is skipped gracefully.
func TestReindexMissingTreeDir(t *testing.T) {
	repoRoot, _ := makeDecisionsDir(t)
	writeTreesYAML(t, repoRoot, []string{"ghost"}) // no ghost/ directory

	db := openRepoIndex(t, repoRoot)
	report, err := Reindex(repoRoot, db, nil)
	if err != nil {
		t.Fatalf("Reindex with missing tree dir: %v", err)
	}
	if len(report.Warnings) == 0 {
		t.Error("expected at least one warning for missing tree dir, got none")
	}
	found := false
	for _, w := range report.Warnings {
		if strings.Contains(w, "ghost") {
			found = true
		}
	}
	if !found {
		t.Errorf("warning does not mention missing tree %q: %v", "ghost", report.Warnings)
	}
}

// ---------------------------------------------------------------------------
// Additional: report timestamps
// ---------------------------------------------------------------------------

func TestReindexReportTimestamps(t *testing.T) {
	repoRoot, _ := makeDecisionsDir(t)
	db := openRepoIndex(t, repoRoot)

	before := time.Now()
	report, err := Reindex(repoRoot, db, nil)
	after := time.Now()
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	if report.Started.Before(before) || report.Started.After(after) {
		t.Errorf("Started %v not in [%v, %v]", report.Started, before, after)
	}
	if report.Ended.Before(report.Started) {
		t.Errorf("Ended %v before Started %v", report.Ended, report.Started)
	}
}

// ---------------------------------------------------------------------------
// verify event payload JSON is round-trippable
// ---------------------------------------------------------------------------

func TestReindexEventPayloadJSON(t *testing.T) {
	repoRoot, _ := makeDecisionsDir(t)
	writeTreesYAML(t, repoRoot, []string{"tree1"})
	writeTreeDir(t, repoRoot, "tree1")

	d := makeDecision("tree1", "pay-slug", "Payload test")
	writeDecisionFile(t, repoRoot, d)

	// Write an event with a non-trivial payload.
	ev := core.Event{
		EventID: ulid.New(),
		V:       core.SchemaVersion,
		Ts:      time.Now().UTC(),
		Actor:   "alice",
		Action:  core.ActionCreate,
		Kind:    core.KindDecision,
		Tree:    "tree1",
		ID:      d.ID,
		Payload: core.EventPayload{
			After: map[string]any{"summary": "Payload test", "status": "proposed"},
			Extra: map[string]any{"custom_key": "custom_value"},
		},
	}
	if err := audit.Append(repoRoot, ev); err != nil {
		t.Fatalf("append event: %v", err)
	}

	db := openRepoIndex(t, repoRoot)
	if _, err := Reindex(repoRoot, db, nil); err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	var payloadJSON string
	err := db.conn.QueryRow(`SELECT payload_json FROM events WHERE event_id=?`, ev.EventID).Scan(&payloadJSON)
	if err != nil {
		t.Fatalf("query event payload: %v", err)
	}

	// Verify the JSON can be parsed back and contains expected keys.
	var m map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &m); err != nil {
		t.Fatalf("unmarshal payload_json: %v", err)
	}
	if _, ok := m["after"]; !ok {
		t.Errorf("payload_json missing 'after' key: %s", payloadJSON)
	}
	if _, ok := m["custom_key"]; !ok {
		t.Errorf("payload_json missing 'custom_key': %s", payloadJSON)
	}
}

// ---------------------------------------------------------------------------
// Ensure nil progress callback doesn't panic
// ---------------------------------------------------------------------------

func TestReindexProgressNil(t *testing.T) {
	repoRoot, _ := makeDecisionsDir(t)
	db := openRepoIndex(t, repoRoot)
	// Should not panic with nil progress.
	if _, err := Reindex(repoRoot, db, nil); err != nil {
		t.Fatalf("Reindex(nil progress): %v", err)
	}
}

// ---------------------------------------------------------------------------
// Multi-tree relationship count
// ---------------------------------------------------------------------------

func TestReindexRelationshipCount(t *testing.T) {
	repoRoot, _ := makeDecisionsDir(t)
	writeTreesYAML(t, repoRoot, []string{"rel-tree"})
	writeTreeDir(t, repoRoot, "rel-tree")

	dA := makeDecision("rel-tree", "slug-a", "A")
	dB := makeDecision("rel-tree", "slug-b", "B")
	dC := makeDecision("rel-tree", "slug-c", "C")

	dA.Relationships = []core.Relationship{
		{Type: core.RelBlocks, Target: dB.ID},
		{Type: core.RelInfluences, Target: dC.ID},
	}
	writeDecisionFile(t, repoRoot, dA)
	writeDecisionFile(t, repoRoot, dB)
	writeDecisionFile(t, repoRoot, dC)

	db := openRepoIndex(t, repoRoot)
	report, err := Reindex(repoRoot, db, nil)
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	if report.Relationships != 2 {
		t.Errorf("Relationships = %d, want 2", report.Relationships)
	}
	if c := countRows(t, db, "relationships"); c != 2 {
		t.Errorf("relationships table count = %d, want 2", c)
	}
}
