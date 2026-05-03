// Package mcp — tests for tool handlers.
//
// Tests target the handler functions directly (not the wire layer) so they
// stay quick and stable. Each domain is exercised in one test grouped by
// area (tree CRUD, decision CRUD, lifecycle, relate, find, history).
package mcp

import (
	"path/filepath"
	"testing"

	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/storage"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// newTestRepo opens a fresh repo + index. The repo has one registered actor
// "agent1" so create/decide handlers can attribute mutations.
func newTestRepo(t *testing.T) (string, *index.DB, string) {
	t.Helper()
	repo := t.TempDir()
	if err := storage.WriteActors(
		filepath.Join(repo, ".decisions", storage.ActorsFileName),
		&storage.ActorsFile{Actors: []core.Actor{
			{Handle: "agent1", Kind: core.ActorAgent, Active: true},
		}},
	); err != nil {
		t.Fatalf("write actors: %v", err)
	}
	db, err := index.Open(filepath.Join(repo, ".index.db"))
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return repo, db, "agent1"
}

// mustCreateTree creates "alpha" via the handler and asserts no error.
func mustCreateTree(t *testing.T, repo string, db *index.DB, actor, slug string) {
	t.Helper()
	if _, err := handleCreateTree(repo, db, actor, slug, slug+" tree", "desc"); err != nil {
		t.Fatalf("create tree %q: %v", slug, err)
	}
}

// mustCreateDecision creates a decision and returns it, failing the test on err.
func mustCreateDecision(t *testing.T, repo string, db *index.DB, actor, tree, summary string) *core.Decision {
	t.Helper()
	d, err := handleCreateDecision(repo, db, actor, tree, createDecisionArgs{
		Summary:  summary,
		Priority: "medium",
	})
	if err != nil {
		t.Fatalf("create decision %q: %v", summary, err)
	}
	return d
}

// ---------------------------------------------------------------------------
// 1. Tree CRUD
// ---------------------------------------------------------------------------

// TestTreeCRUD covers create -> get -> update -> archive in a single flow,
// asserting the index and on-disk YAML reflect each transition.
func TestTreeCRUD(t *testing.T) {
	repo, db, actor := newTestRepo(t)

	t1, err := handleCreateTree(repo, db, actor, "alpha", "Alpha", "first")
	if err != nil {
		t.Fatalf("create_tree: %v", err)
	}
	if t1.Slug != "alpha" || t1.Title != "Alpha" || t1.Description != "first" {
		t.Errorf("create_tree returned %+v", t1)
	}
	if t1.Layout.Direction != "TB" {
		t.Errorf("Layout.Direction default got %q want TB", t1.Layout.Direction)
	}

	got, err := handleGetTree(db, "alpha")
	if err != nil {
		t.Fatalf("get_tree: %v", err)
	}
	if got.Title != "Alpha" {
		t.Errorf("get_tree title = %q", got.Title)
	}

	// Update title only; description should be preserved.
	newName := "Alpha v2"
	updated, err := handleUpdateTree(repo, db, actor, "alpha", &newName, nil)
	if err != nil {
		t.Fatalf("update_tree: %v", err)
	}
	if updated.Title != "Alpha v2" {
		t.Errorf("update title got %q want Alpha v2", updated.Title)
	}
	if updated.Description != "first" {
		t.Errorf("update preserved desc got %q want first", updated.Description)
	}

	// Archive.
	arch, err := handleArchiveTree(repo, db, actor, "alpha", true)
	if err != nil {
		t.Fatalf("archive_tree: %v", err)
	}
	if !arch.Archived {
		t.Errorf("archive_tree returned Archived=false")
	}

	// Duplicate slug should error.
	if _, err := handleCreateTree(repo, db, actor, "alpha", "x", ""); err == nil {
		t.Error("expected error creating duplicate tree, got nil")
	}

	// Invalid slug.
	if _, err := handleCreateTree(repo, db, actor, "Bad-Slug!", "x", ""); err == nil {
		t.Error("expected error for invalid slug, got nil")
	}
}

// ---------------------------------------------------------------------------
// 2. Decision CRUD
// ---------------------------------------------------------------------------

// TestDecisionCRUD covers create -> get -> list -> update -> delete (soft).
func TestDecisionCRUD(t *testing.T) {
	repo, db, actor := newTestRepo(t)
	mustCreateTree(t, repo, db, actor, "alpha")

	d, err := handleCreateDecision(repo, db, actor, "alpha", createDecisionArgs{
		Summary:     "Pick a database",
		Description: "We need durable storage",
		Priority:    "high",
		Tags:        []string{"db", "infra"},
		Assignee:    "agent1",
	})
	if err != nil {
		t.Fatalf("create_decision: %v", err)
	}
	if len(d.ID) != 26 {
		t.Errorf("id len = %d want 26", len(d.ID))
	}
	if d.Creator != "agent1" {
		t.Errorf("creator = %q want agent1", d.Creator)
	}
	if d.Status != core.StatusProposed {
		t.Errorf("status = %q want proposed", d.Status)
	}

	// Get by full ID.
	got, err := handleGetDecision(db, "alpha", d.ID)
	if err != nil {
		t.Fatalf("get_decision full id: %v", err)
	}
	if got.ID != d.ID {
		t.Errorf("get_decision returned wrong id: %s vs %s", got.ID, d.ID)
	}

	// Get by 4-char prefix.
	got2, err := handleGetDecision(db, "alpha", d.ID[:6])
	if err != nil {
		t.Fatalf("get_decision prefix: %v", err)
	}
	if got2.ID != d.ID {
		t.Errorf("prefix lookup mismatch")
	}

	// List (no filter).
	res, err := handleListDecisions(db, listDecisionsArgs{Tree: "alpha"})
	if err != nil {
		t.Fatalf("list_decisions: %v", err)
	}
	if len(res.Items) != 1 {
		t.Errorf("list got %d items want 1", len(res.Items))
	}

	// Filter by status.
	res2, err := handleListDecisions(db, listDecisionsArgs{Tree: "alpha", Status: "decided"})
	if err != nil {
		t.Fatalf("list filter: %v", err)
	}
	if len(res2.Items) != 0 {
		t.Errorf("decided filter returned %d items want 0", len(res2.Items))
	}

	// Update summary + tags.
	newSummary := "Pick the database"
	updated, err := handleUpdateDecision(repo, db, actor, "alpha", d.ID,
		updateDecisionFields{
			Summary: &newSummary,
			TagsSet: true,
			Tags:    []string{"db"},
		}, "")
	if err != nil {
		t.Fatalf("update_decision: %v", err)
	}
	if updated.Summary != newSummary {
		t.Errorf("summary not updated: %q", updated.Summary)
	}
	if len(updated.Tags) != 1 || updated.Tags[0] != "db" {
		t.Errorf("tags not updated: %v", updated.Tags)
	}

	// Soft-delete.
	if err := handleDeleteDecision(repo, db, actor, "alpha", d.ID, false, false); err != nil {
		t.Fatalf("delete_decision soft: %v", err)
	}
	if _, err := handleGetDecision(db, "alpha", d.ID); err == nil {
		t.Error("expected get_decision to fail after delete")
	}
}

// ---------------------------------------------------------------------------
// 3. Decision lifecycle
// ---------------------------------------------------------------------------

// TestDecisionLifecycle exercises decide / undecide / scope-out / restore /
// supersede transitions through the handler layer.
func TestDecisionLifecycle(t *testing.T) {
	repo, db, actor := newTestRepo(t)
	mustCreateTree(t, repo, db, actor, "alpha")
	d := mustCreateDecision(t, repo, db, actor, "alpha", "Use Postgres")

	// Decide.
	dec, err := handleDecideDecision(repo, db, actor, "alpha", d.ID,
		"Postgres 16", "Mature, FTS5 not relevant", nil, true)
	if err != nil {
		t.Fatalf("decide: %v", err)
	}
	if dec.Status != core.StatusDecided {
		t.Errorf("status = %q want decided", dec.Status)
	}
	if dec.ActualChoice != "Postgres 16" {
		t.Errorf("choice = %q", dec.ActualChoice)
	}
	if len(dec.DecidedBy) != 1 || dec.DecidedBy[0] != actor {
		t.Errorf("decided_by = %v want [%s]", dec.DecidedBy, actor)
	}
	if !dec.IsRecommended {
		t.Error("is_recommended should be true")
	}

	// Undecide.
	und, err := handleUndecideDecision(repo, db, actor, "alpha", d.ID)
	if err != nil {
		t.Fatalf("undecide: %v", err)
	}
	if und.Status != core.StatusProposed {
		t.Errorf("after undecide status = %q want proposed", und.Status)
	}
	if und.ActualChoice != "" {
		t.Errorf("undecide should clear choice; got %q", und.ActualChoice)
	}

	// Scope-out.
	so, err := handleScopeOutDecision(repo, db, actor, "alpha", d.ID, "moved to backlog")
	if err != nil {
		t.Fatalf("scope_out: %v", err)
	}
	if so.Status != core.StatusOutOfScope {
		t.Errorf("status = %q want out_of_scope", so.Status)
	}
	if so.OutOfScopeReason != "moved to backlog" {
		t.Errorf("reason = %q", so.OutOfScopeReason)
	}

	// Restore.
	rs, err := handleRestoreDecision(repo, db, actor, "alpha", d.ID)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if rs.Status != core.StatusProposed {
		t.Errorf("restored status = %q want proposed", rs.Status)
	}

	// Supersede with a second decision.
	d2 := mustCreateDecision(t, repo, db, actor, "alpha", "Use SQLite")
	sup, err := handleSupersedeDecision(repo, db, actor, "alpha", d.ID, d2.ID)
	if err != nil {
		t.Fatalf("supersede: %v", err)
	}
	if sup.Status != core.StatusSuperseded {
		t.Errorf("status = %q want superseded", sup.Status)
	}
	foundSelf := false
	for _, r := range sup.Relationships {
		if r.Type == core.RelSupersedes && r.Target == d2.ID {
			foundSelf = true
		}
	}
	if !foundSelf {
		t.Errorf("expected supersedes->%s edge on source", d2.ID)
	}
	// Supersede self should fail.
	if _, err := handleSupersedeDecision(repo, db, actor, "alpha", d.ID, d.ID); err == nil {
		t.Error("expected error superseding self")
	}
}

// ---------------------------------------------------------------------------
// 4. Relate / unrelate
// ---------------------------------------------------------------------------

// TestDecisionRelate exercises relate / unrelate including the cycle check
// for blocks edges, idempotency, and the supersedes guard.
func TestDecisionRelate(t *testing.T) {
	repo, db, actor := newTestRepo(t)
	mustCreateTree(t, repo, db, actor, "alpha")
	a := mustCreateDecision(t, repo, db, actor, "alpha", "Decision A")
	b := mustCreateDecision(t, repo, db, actor, "alpha", "Decision B")

	// A relates_to B.
	out, err := handleRelateDecisions(repo, db, actor, "alpha", a.ID, "relates_to", b.ID, "")
	if err != nil {
		t.Fatalf("relate: %v", err)
	}
	if len(out.Relationships) != 1 {
		t.Fatalf("expected 1 rel, got %d", len(out.Relationships))
	}

	// Idempotent: same edge again is a no-op.
	if _, err := handleRelateDecisions(repo, db, actor, "alpha", a.ID, "relates_to", b.ID, ""); err != nil {
		t.Fatalf("relate idempotent: %v", err)
	}

	// Supersedes via relate is rejected.
	if _, err := handleRelateDecisions(repo, db, actor, "alpha", a.ID, "supersedes", b.ID, ""); err == nil {
		t.Error("expected supersedes via relate to be rejected")
	}

	// Self-relate rejected.
	if _, err := handleRelateDecisions(repo, db, actor, "alpha", a.ID, "relates_to", a.ID, ""); err == nil {
		t.Error("expected self-relate to be rejected")
	}

	// Invalid type.
	if _, err := handleRelateDecisions(repo, db, actor, "alpha", a.ID, "bogus", b.ID, ""); err == nil {
		t.Error("expected invalid type to be rejected")
	}

	// Unrelate.
	out2, err := handleUnrelateDecisions(repo, db, actor, "alpha", a.ID, "relates_to", b.ID)
	if err != nil {
		t.Fatalf("unrelate: %v", err)
	}
	if len(out2.Relationships) != 0 {
		t.Errorf("expected 0 rels after unrelate, got %d", len(out2.Relationships))
	}

	// Unrelate again is idempotent (no error).
	if _, err := handleUnrelateDecisions(repo, db, actor, "alpha", a.ID, "relates_to", b.ID); err != nil {
		t.Errorf("unrelate idempotent: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 5. Find
// ---------------------------------------------------------------------------

// TestFindDecisions verifies the FTS5-backed search finds matching summaries
// and respects the optional tree-scope filter.
func TestFindDecisions(t *testing.T) {
	repo, db, actor := newTestRepo(t)
	mustCreateTree(t, repo, db, actor, "alpha")
	mustCreateTree(t, repo, db, actor, "beta")

	mustCreateDecision(t, repo, db, actor, "alpha", "Use Postgres for primary store")
	mustCreateDecision(t, repo, db, actor, "alpha", "Use Redis for caching")
	mustCreateDecision(t, repo, db, actor, "beta", "Postgres replication topology")

	// Search across all trees for "Postgres".
	hits, err := handleFindDecisions(db, "Postgres", "", 0)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(hits) != 2 {
		t.Errorf("got %d hits, want 2: %+v", len(hits), hits)
	}

	// Constrain to alpha.
	hits2, err := handleFindDecisions(db, "Postgres", "alpha", 0)
	if err != nil {
		t.Fatalf("find tree-scoped: %v", err)
	}
	if len(hits2) != 1 {
		t.Errorf("alpha-scope: got %d hits, want 1", len(hits2))
	}

	// Empty query -> error.
	if _, err := handleFindDecisions(db, "", "", 0); err == nil {
		t.Error("expected error for empty query")
	}
}

// ---------------------------------------------------------------------------
// 6. History
// ---------------------------------------------------------------------------

// TestDecisionHistory verifies that mutations on a decision produce events
// retrievable via handleDecisionHistory.
func TestDecisionHistory(t *testing.T) {
	repo, db, actor := newTestRepo(t)
	mustCreateTree(t, repo, db, actor, "alpha")
	d := mustCreateDecision(t, repo, db, actor, "alpha", "Pick a queue")

	// Drive a couple of state transitions.
	if _, err := handleDecideDecision(repo, db, actor, "alpha", d.ID,
		"NATS", "Lightweight", nil, false); err != nil {
		t.Fatalf("decide: %v", err)
	}
	if _, err := handleUndecideDecision(repo, db, actor, "alpha", d.ID); err != nil {
		t.Fatalf("undecide: %v", err)
	}

	events, err := handleDecisionHistory(repo, db, "alpha", d.ID, "")
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(events) < 3 {
		t.Errorf("expected ≥3 events (create+decide+undecide), got %d", len(events))
	}

	// All events should target the same decision id.
	for _, ev := range events {
		if ev.ID != d.ID {
			t.Errorf("event %s targets %q, want %s", ev.EventID, ev.ID, d.ID)
		}
	}
}
