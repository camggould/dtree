package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/config"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/fsutil"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/storage"
	"github.com/cgould/dtree/internal/ulid"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makeSyncRepo builds a repo with one tree and one indexed decision.
// Returns (repoRoot, decisionPath, decision).
func makeSyncRepo(t *testing.T) (repoRoot string, decPath string, d *core.Decision) {
	t.Helper()
	repoRoot, _ = isolatedEnv(t)
	decisionsDir := filepath.Join(repoRoot, ".decisions")
	treeSlug := "arch"
	treeDir := filepath.Join(decisionsDir, treeSlug)
	decDir := filepath.Join(treeDir, "decisions")
	auditDir := filepath.Join(decisionsDir, "audit")

	for _, dir := range []string{decDir, auditDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
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
			{Handle: "testactor", Name: "Test", Email: "t@x.com", Kind: core.ActorHuman, Active: true},
		},
	}
	if err := storage.WriteActors(filepath.Join(decisionsDir, storage.ActorsFileName), af); err != nil {
		t.Fatalf("write actors.yaml: %v", err)
	}

	// Write tree.yaml.
	tree := &core.Tree{Slug: treeSlug, CreatedAt: time.Now().UTC()}
	if err := storage.WriteTree(filepath.Join(treeDir, storage.TreeMetaFileName), tree); err != nil {
		t.Fatalf("write tree.yaml: %v", err)
	}

	// Write config pointing to testactor.
	localCfg := &config.File{Identity: config.IdentityConfig{Default: "testactor"}}
	if err := config.WriteFile(config.LocalPath(repoRoot), localCfg); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Create decision.
	d = &core.Decision{
		ID:            ulid.New(),
		Tree:          treeSlug,
		Slug:          "use-postgres",
		SchemaVersion: core.SchemaVersion,
		Summary:       "Use PostgreSQL as primary database",
		Status:        core.StatusProposed,
		Priority:      core.PriorityHigh,
		Creator:       "testactor",
	}

	decPath = storage.DecisionPath(treeDir, d.ID, d.Slug)
	if err := storage.WriteDecision(decPath, d); err != nil {
		t.Fatalf("write decision: %v", err)
	}

	// Hash and index the decision.
	sha, err := fsutil.Sha256File(decPath)
	if err != nil {
		t.Fatalf("hash decision: %v", err)
	}

	dbPath := filepath.Join(decisionsDir, ".index.db")
	db, err := index.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// Insert tree and actor rows.
	if _, err := db.Conn().Exec(
		`INSERT OR IGNORE INTO trees(slug, created_at) VALUES(?, ?)`,
		treeSlug, "2026-01-01T00:00:00Z",
	); err != nil {
		t.Fatalf("insert tree: %v", err)
	}
	if _, err := db.Conn().Exec(
		`INSERT OR IGNORE INTO actors(handle,name,email,kind,active) VALUES(?,?,?,?,?)`,
		"testactor", "Test", "t@x.com", "human", 1,
	); err != nil {
		t.Fatalf("insert actor: %v", err)
	}

	if err := index.InsertDecision(db, d, sha); err != nil {
		t.Fatalf("index decision: %v", err)
	}

	return repoRoot, decPath, d
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestSyncCleanRepoNoOp(t *testing.T) {
	repoRoot, _, _ := makeSyncRepo(t)

	out, _, err := runCmd(t, "--repo-root", repoRoot, "sync", "--yes")
	if err != nil {
		t.Fatalf("expected exit 0, got: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "No mismatches found.") {
		t.Errorf("expected 'No mismatches found.' in output, got:\n%s", out)
	}
}

func TestSyncRecordExternalEdit(t *testing.T) {
	repoRoot, decPath, d := makeSyncRepo(t)

	// Modify the YAML file externally.
	updated := *d
	updated.Summary = "Use PostgreSQL — EXTERNALLY EDITED"
	if err := storage.WriteDecision(decPath, &updated); err != nil {
		t.Fatalf("write updated decision: %v", err)
	}

	// Run sync --yes (auto-record).
	out, _, err := runCmd(t, "--repo-root", repoRoot, "sync", "--yes")
	if err != nil {
		t.Fatalf("expected exit 0, got: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "reconciled") {
		t.Errorf("expected 'reconciled' in output, got:\n%s", out)
	}

	// Verify audit event was written for external_edit.
	events, err := audit.Read(repoRoot, audit.Filter{Action: core.ActionExternalEdit})
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if len(events) == 0 {
		t.Error("expected at least one external_edit audit event, got none")
	}
}

func TestSyncRevertExternalEdit(t *testing.T) {
	repoRoot, decPath, d := makeSyncRepo(t)

	// Remember the original summary.
	originalSummary := d.Summary

	// Modify the YAML file externally.
	updated := *d
	updated.Summary = "Use PostgreSQL — EXTERNALLY EDITED"
	if err := storage.WriteDecision(decPath, &updated); err != nil {
		t.Fatalf("write updated decision: %v", err)
	}

	// Run sync --all-revert.
	out, _, err := runCmd(t, "--repo-root", repoRoot, "sync", "--all-revert")
	if err != nil {
		t.Fatalf("expected exit 0, got: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "reconciled") {
		t.Errorf("expected 'reconciled' in output, got:\n%s", out)
	}

	// Verify file was restored to original content.
	restored, err := storage.ReadDecision(decPath)
	if err != nil {
		t.Fatalf("read restored decision: %v", err)
	}
	if restored.Summary != originalSummary {
		t.Errorf("expected summary %q after revert, got %q", originalSummary, restored.Summary)
	}
}
