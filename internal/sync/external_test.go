package sync_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/fsutil"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/storage"
	dtreesync "github.com/cgould/dtree/internal/sync"
	"github.com/cgould/dtree/internal/ulid"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newTestEnv sets up a temp directory as repoRoot with a minimal .decisions/
// layout for the given tree slug. Returns (repoRoot, db, treeDir, decisionsDir).
func newTestEnv(t *testing.T, tree string) (repoRoot string, db *index.DB, decisionsDir string) {
	t.Helper()

	repoRoot = t.TempDir()
	decisionsDir = filepath.Join(repoRoot, ".decisions", tree, "decisions")
	if err := os.MkdirAll(decisionsDir, 0o755); err != nil {
		t.Fatalf("mkdir decisions: %v", err)
	}

	dbPath := filepath.Join(repoRoot, ".decisions", ".index.db")
	var err error
	db, err = index.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Insert the tree row into the index.
	insertTreeRow(t, db, tree)
	return repoRoot, db, decisionsDir
}

func insertTreeRow(t *testing.T, db *index.DB, slug string) {
	t.Helper()
	_, err := db.Conn().Exec(
		`INSERT OR IGNORE INTO trees(slug, created_at) VALUES(?, ?)`,
		slug, "2026-01-01T00:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert tree %q: %v", slug, err)
	}
}

// newTestDecision builds a minimal valid Decision with a unique ULID.
func newTestDecision(tree, slug, summary string) *core.Decision {
	return &core.Decision{
		ID:            ulid.New(),
		Tree:          tree,
		Slug:          slug,
		SchemaVersion: core.SchemaVersion,
		Summary:       summary,
		Status:        core.StatusProposed,
		Priority:      core.PriorityMedium,
		Creator:       "alice",
	}
}

// writeDecisionFile writes d to decisionsDir and returns (path, sha256).
func writeDecisionFile(t *testing.T, decisionsDir string, d *core.Decision) (string, string) {
	t.Helper()
	treeDir := filepath.Dir(decisionsDir)
	path := storage.DecisionPath(treeDir, d.ID, d.Slug)
	if err := storage.WriteDecision(path, d); err != nil {
		t.Fatalf("write decision: %v", err)
	}
	sha, err := fsutil.Sha256File(path)
	if err != nil {
		t.Fatalf("hash decision: %v", err)
	}
	return path, sha
}

// indexDecision inserts d into the index with the given sha.
func indexDecision(t *testing.T, db *index.DB, d *core.Decision, sha string) {
	t.Helper()
	if err := index.InsertDecision(db, d, sha); err != nil {
		t.Fatalf("InsertDecision: %v", err)
	}
}

// findMismatch returns the first mismatch whose DecisionID matches id.
func findMismatch(mismatches []dtreesync.Mismatch, id string) (dtreesync.Mismatch, bool) {
	for _, m := range mismatches {
		if m.DecisionID == id {
			return m, true
		}
	}
	return dtreesync.Mismatch{}, false
}

// ---------------------------------------------------------------------------
// Scan tests
// ---------------------------------------------------------------------------

func TestScanCleanReturnsEmpty(t *testing.T) {
	repoRoot, db, decisionsDir := newTestEnv(t, "arch")
	d := newTestDecision("arch", "clean-decision", "Nothing changed")

	path, sha := writeDecisionFile(t, decisionsDir, d)
	_ = path
	indexDecision(t, db, d, sha)

	mismatches, err := dtreesync.Scan(repoRoot, db)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(mismatches) != 0 {
		t.Errorf("expected 0 mismatches for clean state, got %d: %+v", len(mismatches), mismatches)
	}
}

func TestScanDetectsEdit(t *testing.T) {
	repoRoot, db, decisionsDir := newTestEnv(t, "arch")
	d := newTestDecision("arch", "edit-me", "Original summary")

	path, sha := writeDecisionFile(t, decisionsDir, d)
	indexDecision(t, db, d, sha)

	// Modify the file after indexing.
	d.Summary = "Modified summary"
	if err := storage.WriteDecision(path, d); err != nil {
		t.Fatalf("write modified decision: %v", err)
	}

	mismatches, err := dtreesync.Scan(repoRoot, db)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	m, ok := findMismatch(mismatches, d.ID)
	if !ok {
		t.Fatalf("no mismatch found for %s; all mismatches: %+v", d.ID, mismatches)
	}
	if m.Kind != dtreesync.MismatchEdit {
		t.Errorf("Kind = %d, want MismatchEdit", m.Kind)
	}
	if m.IndexSha != sha {
		t.Errorf("IndexSha = %q, want %q", m.IndexSha, sha)
	}
	if m.DiskSha == sha {
		t.Error("DiskSha should differ from IndexSha after modification")
	}
}

func TestScanDetectsCreate(t *testing.T) {
	repoRoot, db, decisionsDir := newTestEnv(t, "arch")
	d := newTestDecision("arch", "new-file", "Brand new decision")

	// Write file to disk but do NOT insert into index.
	_, sha := writeDecisionFile(t, decisionsDir, d)

	mismatches, err := dtreesync.Scan(repoRoot, db)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	m, ok := findMismatch(mismatches, d.ID)
	if !ok {
		t.Fatalf("no mismatch found for %s; all mismatches: %+v", d.ID, mismatches)
	}
	if m.Kind != dtreesync.MismatchCreate {
		t.Errorf("Kind = %d, want MismatchCreate", m.Kind)
	}
	if m.DiskSha != sha {
		t.Errorf("DiskSha = %q, want %q", m.DiskSha, sha)
	}
	if m.IndexSha != "" {
		t.Errorf("IndexSha = %q, want empty", m.IndexSha)
	}
}

func TestScanDetectsDelete(t *testing.T) {
	repoRoot, db, decisionsDir := newTestEnv(t, "arch")
	d := newTestDecision("arch", "to-delete", "Will be deleted from disk")

	path, sha := writeDecisionFile(t, decisionsDir, d)
	indexDecision(t, db, d, sha)

	// Remove the file from disk.
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove file: %v", err)
	}

	mismatches, err := dtreesync.Scan(repoRoot, db)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	m, ok := findMismatch(mismatches, d.ID)
	if !ok {
		t.Fatalf("no mismatch found for %s; all mismatches: %+v", d.ID, mismatches)
	}
	if m.Kind != dtreesync.MismatchDelete {
		t.Errorf("Kind = %d, want MismatchDelete", m.Kind)
	}
	if m.DiskSha != "" {
		t.Errorf("DiskSha = %q, want empty", m.DiskSha)
	}
	if m.IndexSha != sha {
		t.Errorf("IndexSha = %q, want %q", m.IndexSha, sha)
	}
}

func TestScanIgnoresDeletedDir(t *testing.T) {
	repoRoot, db, decisionsDir := newTestEnv(t, "arch")

	// Create a .deleted/ sub-directory with a YAML file.
	deletedDir := filepath.Join(decisionsDir, ".deleted")
	if err := os.MkdirAll(deletedDir, 0o755); err != nil {
		t.Fatalf("mkdir deleted: %v", err)
	}
	d := newTestDecision("arch", "deleted-decision", "Soft-deleted")
	// Write directly to .deleted/ (not using writeDecisionFile to avoid inserting).
	deletedPath := filepath.Join(deletedDir, d.ID+"-"+d.Slug+".yaml")
	if err := storage.WriteDecision(deletedPath, d); err != nil {
		t.Fatalf("write deleted: %v", err)
	}

	mismatches, err := dtreesync.Scan(repoRoot, db)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if _, ok := findMismatch(mismatches, d.ID); ok {
		t.Errorf("Scan should not report mismatch for file under .deleted/, got one")
	}
}

// ---------------------------------------------------------------------------
// Reconcile — ActionRecord tests
// ---------------------------------------------------------------------------

func TestReconcileRecordEdit(t *testing.T) {
	repoRoot, db, decisionsDir := newTestEnv(t, "arch")
	d := newTestDecision("arch", "record-edit", "Original")

	path, sha := writeDecisionFile(t, decisionsDir, d)
	indexDecision(t, db, d, sha)

	// Modify on disk.
	d.Summary = "Edited externally"
	if err := storage.WriteDecision(path, d); err != nil {
		t.Fatalf("write modified: %v", err)
	}
	newSha, _ := fsutil.Sha256File(path)

	m := dtreesync.Mismatch{
		DecisionID: d.ID,
		Tree:       "arch",
		Path:       path,
		Kind:       dtreesync.MismatchEdit,
		DiskSha:    newSha,
		IndexSha:   sha,
	}

	if err := dtreesync.Reconcile(repoRoot, db, m, dtreesync.ActionRecord, "alice"); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Index content_sha256 should now reflect the disk sha.
	var gotSha string
	if err := db.Conn().QueryRow(`SELECT content_sha256 FROM decisions WHERE id=?`, d.ID).Scan(&gotSha); err != nil {
		t.Fatalf("scan sha: %v", err)
	}
	if gotSha != newSha {
		t.Errorf("content_sha256 = %q, want %q", gotSha, newSha)
	}

	// Audit log should have an external_edit event.
	assertAuditAction(t, repoRoot, "arch", d.ID, string(core.ActionExternalEdit))
}

func TestReconcileRecordCreate(t *testing.T) {
	repoRoot, db, decisionsDir := newTestEnv(t, "arch")
	d := newTestDecision("arch", "record-create", "Externally created")

	path, sha := writeDecisionFile(t, decisionsDir, d)
	// NOT indexed.

	m := dtreesync.Mismatch{
		DecisionID: d.ID,
		Tree:       "arch",
		Path:       path,
		Kind:       dtreesync.MismatchCreate,
		DiskSha:    sha,
		IndexSha:   "",
	}

	if err := dtreesync.Reconcile(repoRoot, db, m, dtreesync.ActionRecord, "alice"); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Decision should now be in the index.
	got, err := index.GetDecision(db, d.ID)
	if err != nil {
		t.Fatalf("GetDecision: %v", err)
	}
	if got == nil {
		t.Fatal("decision not found in index after RecordCreate")
	}

	// Audit log should have an external_create event.
	assertAuditAction(t, repoRoot, "arch", d.ID, string(core.ActionExternalCreate))
}

func TestReconcileRecordDelete(t *testing.T) {
	repoRoot, db, decisionsDir := newTestEnv(t, "arch")
	d := newTestDecision("arch", "record-delete", "Will be deleted")

	path, sha := writeDecisionFile(t, decisionsDir, d)
	indexDecision(t, db, d, sha)

	// Remove file from disk.
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}

	treeDir := filepath.Join(repoRoot, ".decisions", "arch")
	expectedPath := storage.DecisionPath(treeDir, d.ID, d.Slug)

	m := dtreesync.Mismatch{
		DecisionID: d.ID,
		Tree:       "arch",
		Path:       expectedPath,
		Kind:       dtreesync.MismatchDelete,
		DiskSha:    "",
		IndexSha:   sha,
	}

	if err := dtreesync.Reconcile(repoRoot, db, m, dtreesync.ActionRecord, "alice"); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	// Decision should be soft-deleted.
	var deleted int
	if err := db.Conn().QueryRow(`SELECT deleted FROM decisions WHERE id=?`, d.ID).Scan(&deleted); err != nil {
		t.Fatalf("scan deleted: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}

	// Audit log should have an external_delete event.
	assertAuditAction(t, repoRoot, "arch", d.ID, string(core.ActionExternalDelete))
}

// ---------------------------------------------------------------------------
// Reconcile — ActionRevert tests
// ---------------------------------------------------------------------------

func TestReconcileRevertEdit(t *testing.T) {
	repoRoot, db, decisionsDir := newTestEnv(t, "arch")
	d := newTestDecision("arch", "revert-edit", "Original content")

	path, sha := writeDecisionFile(t, decisionsDir, d)
	indexDecision(t, db, d, sha)

	// Modify on disk.
	dModified := *d
	dModified.Summary = "Modified by external editor"
	if err := storage.WriteDecision(path, &dModified); err != nil {
		t.Fatalf("write modified: %v", err)
	}
	newSha, _ := fsutil.Sha256File(path)
	if newSha == sha {
		t.Fatal("sha should differ after modification")
	}

	m := dtreesync.Mismatch{
		DecisionID: d.ID,
		Tree:       "arch",
		Path:       path,
		Kind:       dtreesync.MismatchEdit,
		DiskSha:    newSha,
		IndexSha:   sha,
	}

	if err := dtreesync.Reconcile(repoRoot, db, m, dtreesync.ActionRevert, "alice"); err != nil {
		t.Fatalf("Reconcile revert: %v", err)
	}

	// Disk file should match index's view (original sha).
	afterSha, err := fsutil.Sha256File(path)
	if err != nil {
		t.Fatalf("hash after revert: %v", err)
	}
	if afterSha != sha {
		t.Errorf("disk sha after revert = %q, want index sha %q", afterSha, sha)
	}
}

func TestReconcileRevertCreate(t *testing.T) {
	repoRoot, db, decisionsDir := newTestEnv(t, "arch")
	d := newTestDecision("arch", "revert-create", "Externally created file")

	path, sha := writeDecisionFile(t, decisionsDir, d)
	// NOT indexed.

	m := dtreesync.Mismatch{
		DecisionID: d.ID,
		Tree:       "arch",
		Path:       path,
		Kind:       dtreesync.MismatchCreate,
		DiskSha:    sha,
		IndexSha:   "",
	}

	if err := dtreesync.Reconcile(repoRoot, db, m, dtreesync.ActionRevert, "alice"); err != nil {
		t.Fatalf("Reconcile revert create: %v", err)
	}

	// File should be removed from disk.
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file still exists after revert-create, want it removed")
	}
}

func TestReconcileRevertDelete(t *testing.T) {
	repoRoot, db, decisionsDir := newTestEnv(t, "arch")
	d := newTestDecision("arch", "revert-delete", "Restore this from index")

	path, sha := writeDecisionFile(t, decisionsDir, d)
	indexDecision(t, db, d, sha)

	// Remove file.
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}

	m := dtreesync.Mismatch{
		DecisionID: d.ID,
		Tree:       "arch",
		Path:       path,
		Kind:       dtreesync.MismatchDelete,
		DiskSha:    "",
		IndexSha:   sha,
	}

	if err := dtreesync.Reconcile(repoRoot, db, m, dtreesync.ActionRevert, "alice"); err != nil {
		t.Fatalf("Reconcile revert delete: %v", err)
	}

	// File should now exist on disk.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("file not restored after revert-delete")
	}

	// Content should round-trip (summary preserved).
	restored, err := storage.ReadDecision(path)
	if err != nil {
		t.Fatalf("read restored: %v", err)
	}
	if restored.Summary != d.Summary {
		t.Errorf("restored summary = %q, want %q", restored.Summary, d.Summary)
	}
}

// ---------------------------------------------------------------------------
// Reconcile — ActionAbort
// ---------------------------------------------------------------------------

func TestReconcileAbortNoOp(t *testing.T) {
	repoRoot, db, decisionsDir := newTestEnv(t, "arch")
	d := newTestDecision("arch", "abort-me", "Should not change")

	path, sha := writeDecisionFile(t, decisionsDir, d)
	indexDecision(t, db, d, sha)

	// Modify on disk.
	dModified := *d
	dModified.Summary = "Unauthorized change"
	if err := storage.WriteDecision(path, &dModified); err != nil {
		t.Fatalf("write modified: %v", err)
	}
	modSha, _ := fsutil.Sha256File(path)

	m := dtreesync.Mismatch{
		DecisionID: d.ID,
		Tree:       "arch",
		Path:       path,
		Kind:       dtreesync.MismatchEdit,
		DiskSha:    modSha,
		IndexSha:   sha,
	}

	if err := dtreesync.Reconcile(repoRoot, db, m, dtreesync.ActionAbort, "alice"); err != nil {
		t.Fatalf("Reconcile abort: %v", err)
	}

	// Index sha must remain unchanged.
	var gotSha string
	if err := db.Conn().QueryRow(`SELECT content_sha256 FROM decisions WHERE id=?`, d.ID).Scan(&gotSha); err != nil {
		t.Fatalf("scan sha: %v", err)
	}
	if gotSha != sha {
		t.Errorf("index sha changed after abort: got %q, want %q", gotSha, sha)
	}

	// Disk file must remain unchanged (still has modified content).
	diskSha, _ := fsutil.Sha256File(path)
	if diskSha != modSha {
		t.Errorf("disk sha changed after abort: got %q, want %q", diskSha, modSha)
	}
}

// ---------------------------------------------------------------------------
// audit helper
// ---------------------------------------------------------------------------

// assertAuditAction scans the audit log for tree and verifies that at least
// one event matching action and targetID exists.
func assertAuditAction(t *testing.T, repoRoot, tree, targetID, action string) {
	t.Helper()

	auditDir := filepath.Join(repoRoot, ".decisions", tree, "audit")
	entries, err := os.ReadDir(auditDir)
	if err != nil {
		t.Fatalf("read audit dir %s: %v", auditDir, err)
	}

	found := false
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(auditDir, e.Name()))
		if err != nil {
			continue
		}
		if strings.Contains(string(data), action) && strings.Contains(string(data), targetID) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no %q audit event for %s found in %s", action, targetID, auditDir)
	}
}
