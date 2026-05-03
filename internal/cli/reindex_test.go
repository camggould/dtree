package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/storage"
	"github.com/cgould/dtree/internal/ulid"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makeReindexRepo creates a synthetic .decisions/ layout with one tree and
// one decision YAML file ready for reindex.
func makeReindexRepo(t *testing.T) string {
	t.Helper()
	repoRoot, _ := isolatedEnv(t)
	decisionsDir := filepath.Join(repoRoot, ".decisions")

	// Create directory structure.
	treeSlug := "arch"
	treeDir := filepath.Join(decisionsDir, treeSlug)
	decDir := filepath.Join(treeDir, "decisions")
	auditDir := filepath.Join(decisionsDir, "audit")
	for _, d := range []string{decDir, auditDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	// Write trees.yaml.
	tf := &storage.TreesFile{Trees: []string{treeSlug}}
	if err := storage.WriteTrees(filepath.Join(decisionsDir, storage.TreesFileName), tf); err != nil {
		t.Fatalf("write trees.yaml: %v", err)
	}

	// Write actors.yaml.
	af := &storage.ActorsFile{
		Actors: []core.Actor{
			{Handle: "alice", Name: "Alice", Email: "alice@example.com", Kind: core.ActorHuman, Active: true},
		},
	}
	if err := storage.WriteActors(filepath.Join(decisionsDir, storage.ActorsFileName), af); err != nil {
		t.Fatalf("write actors.yaml: %v", err)
	}

	// Write tree.yaml.
	tree := &core.Tree{
		Slug:      treeSlug,
		CreatedAt: time.Now().UTC(),
	}
	if err := storage.WriteTree(filepath.Join(treeDir, storage.TreeMetaFileName), tree); err != nil {
		t.Fatalf("write tree.yaml: %v", err)
	}

	// Write a decision YAML.
	d := &core.Decision{
		ID:            ulid.New(),
		Tree:          treeSlug,
		Slug:          "use-postgres",
		SchemaVersion: core.SchemaVersion,
		Summary:       "Use PostgreSQL as the primary database",
		Status:        core.StatusProposed,
		Priority:      core.PriorityHigh,
		Creator:       "alice",
	}
	decPath := storage.DecisionPath(treeDir, d.ID, d.Slug)
	if err := storage.WriteDecision(decPath, d); err != nil {
		t.Fatalf("write decision: %v", err)
	}

	// Create empty index DB (schema only).
	dbPath := filepath.Join(decisionsDir, ".index.db")
	db, err := index.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.Close()

	return repoRoot
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestReindexFreshRepo(t *testing.T) {
	repoRoot := makeReindexRepo(t)

	out, errOut, err := runCmd(t, "--repo-root", repoRoot, "reindex")
	if err != nil {
		t.Fatalf("reindex failed: %v\nstdout:\n%s\nstderr:\n%s", err, out, errOut)
	}

	if !strings.Contains(out, "Reindex complete.") {
		t.Errorf("expected 'Reindex complete.' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Trees:") {
		t.Errorf("expected 'Trees:' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Decisions:") {
		t.Errorf("expected 'Decisions:' in output, got:\n%s", out)
	}

	// Verify the index was populated.
	dbPath := filepath.Join(repoRoot, ".decisions", ".index.db")
	db, err := index.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	var treeCount int
	if err := db.Conn().QueryRow(`SELECT COUNT(*) FROM trees`).Scan(&treeCount); err != nil {
		t.Fatalf("count trees: %v", err)
	}
	if treeCount != 1 {
		t.Errorf("expected 1 tree, got %d", treeCount)
	}

	var decCount int
	if err := db.Conn().QueryRow(`SELECT COUNT(*) FROM decisions`).Scan(&decCount); err != nil {
		t.Fatalf("count decisions: %v", err)
	}
	if decCount != 1 {
		t.Errorf("expected 1 decision, got %d", decCount)
	}

	var actorCount int
	if err := db.Conn().QueryRow(`SELECT COUNT(*) FROM actors`).Scan(&actorCount); err != nil {
		t.Fatalf("count actors: %v", err)
	}
	if actorCount != 1 {
		t.Errorf("expected 1 actor, got %d", actorCount)
	}
}

func TestReindexClearsExisting(t *testing.T) {
	repoRoot := makeReindexRepo(t)

	// Run reindex once to populate.
	if _, _, err := runCmd(t, "--repo-root", repoRoot, "reindex"); err != nil {
		t.Fatalf("first reindex: %v", err)
	}

	// Run reindex again — should produce same counts (idempotent).
	out, _, err := runCmd(t, "--repo-root", repoRoot, "reindex")
	if err != nil {
		t.Fatalf("second reindex: %v", err)
	}
	if !strings.Contains(out, "Reindex complete.") {
		t.Errorf("expected 'Reindex complete.' on second run, got:\n%s", out)
	}

	// Verify decision count is still 1 (not doubled).
	dbPath := filepath.Join(repoRoot, ".decisions", ".index.db")
	db, err := index.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	var decCount int
	if err := db.Conn().QueryRow(`SELECT COUNT(*) FROM decisions`).Scan(&decCount); err != nil {
		t.Fatalf("count decisions: %v", err)
	}
	if decCount != 1 {
		t.Errorf("expected 1 decision after second reindex, got %d", decCount)
	}
}
