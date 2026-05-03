package cli_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/ulid"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makeFsckRepo creates a minimal repo ready for fsck tests.
// It inserts one valid tree into the index.
func makeFsckRepo(t *testing.T) (repoRoot string, db *index.DB) {
	t.Helper()
	repoRoot = initMinimalRepo(t)

	dbPath := filepath.Join(repoRoot, ".decisions", ".index.db")
	var err error
	db, err = index.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Insert a test tree.
	if _, err := db.Conn().Exec(
		`INSERT OR IGNORE INTO trees(slug, created_at) VALUES(?, ?)`,
		"arch", "2026-01-01T00:00:00Z",
	); err != nil {
		t.Fatalf("insert tree: %v", err)
	}

	return repoRoot, db
}

// insertValidDecision inserts a fully valid decision into db.
func insertValidDecision(t *testing.T, db *index.DB, tree string) string {
	t.Helper()
	id := ulid.New()
	rev := ulid.New()
	_, err := db.Conn().Exec(`
		INSERT INTO decisions(id, tree, slug, summary, description,
			status, priority, creator, assignee,
			recommended_summary, recommended_full, recommended_by,
			actual_choice, actual_choice_reason, is_recommended,
			out_of_scope_reason, schema_version, rev, content_sha256, deleted)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, tree, "use-postgres", "Use PostgreSQL", "",
		"proposed", "high", "alice", "", "", "", "",
		"", "", 0, "", 1, rev, "sha256abc", 0,
	)
	if err != nil {
		t.Fatalf("insert valid decision: %v", err)
	}
	return id
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestFsckCleanRepo(t *testing.T) {
	repoRoot, db := makeFsckRepo(t)

	// Insert a valid decision.
	insertValidDecision(t, db, "arch")
	db.Close()

	out, _, err := runCmd(t, "--repo-root", repoRoot, "--output", "human", "fsck")
	if err != nil {
		t.Fatalf("expected exit 0 (clean), got: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "OK") {
		t.Errorf("expected 'OK' in clean output, got:\n%s", out)
	}
}

func TestFsckDetectsCycle(t *testing.T) {
	repoRoot, db := makeFsckRepo(t)

	// Insert two decisions and a blocks-cycle between them.
	id1 := insertValidDecision(t, db, "arch")

	// Second valid decision with different slug.
	id2 := ulid.New()
	rev2 := ulid.New()
	if _, err := db.Conn().Exec(`
		INSERT INTO decisions(id, tree, slug, summary, description,
			status, priority, creator, assignee,
			recommended_summary, recommended_full, recommended_by,
			actual_choice, actual_choice_reason, is_recommended,
			out_of_scope_reason, schema_version, rev, content_sha256, deleted)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id2, "arch", "use-redis", "Use Redis", "",
		"proposed", "medium", "alice", "", "", "", "",
		"", "", 0, "", 1, rev2, "sha256def", 0,
	); err != nil {
		t.Fatalf("insert second decision: %v", err)
	}

	// Insert a cycle: id1 blocks id2, id2 blocks id1.
	evID := ulid.New()
	if _, err := db.Conn().Exec(
		`INSERT INTO relationships(source, target, type, tree, created_event_id) VALUES(?,?,?,?,?)`,
		id1, id2, "blocks", "arch", evID,
	); err != nil {
		t.Fatalf("insert rel 1: %v", err)
	}
	if _, err := db.Conn().Exec(
		`INSERT INTO relationships(source, target, type, tree, created_event_id) VALUES(?,?,?,?,?)`,
		id2, id1, "blocks", "arch", evID,
	); err != nil {
		t.Fatalf("insert rel 2: %v", err)
	}
	db.Close()

	out, _, err := runCmd(t, "--repo-root", repoRoot, "--output", "human", "fsck")
	// Should exit 1 (violations found).
	if err == nil {
		t.Fatalf("expected exit 1 (cycle detected), but got exit 0\noutput:\n%s", out)
	}
	if !strings.Contains(out, "cycle") && !strings.Contains(out, "Graph") {
		t.Errorf("expected cycle info in output, got:\n%s", out)
	}
}

func TestFsckDetectsInvalidDecision(t *testing.T) {
	repoRoot, db := makeFsckRepo(t)

	// Insert a decided decision with no actual_choice → invalid.
	id := ulid.New()
	rev := ulid.New()
	if _, err := db.Conn().Exec(`
		INSERT INTO decisions(id, tree, slug, summary, description,
			status, priority, creator, assignee,
			recommended_summary, recommended_full, recommended_by,
			actual_choice, actual_choice_reason, is_recommended,
			out_of_scope_reason, schema_version, rev, content_sha256, deleted)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, "arch", "use-postgres", "Use PostgreSQL", "",
		"decided", "high", "alice", "", "", "", "",
		"", "", 0, "", 1, rev, "sha256abc", 0, // no actual_choice
	); err != nil {
		t.Fatalf("insert invalid decided decision: %v", err)
	}
	db.Close()

	out, _, err := runCmd(t, "--repo-root", repoRoot, "--output", "human", "fsck")
	if err == nil {
		t.Fatalf("expected exit 1 (violations), but got exit 0\noutput:\n%s", out)
	}
	if !strings.Contains(out, "incomplete_decided") {
		t.Errorf("expected 'incomplete_decided' in output, got:\n%s", out)
	}
}

func TestFsckJSON(t *testing.T) {
	repoRoot, db := makeFsckRepo(t)

	// Insert a valid decision.
	insertValidDecision(t, db, "arch")
	db.Close()

	out, _, err := runCmd(t, "--repo-root", repoRoot, "--output", "json", "fsck")
	if err != nil {
		t.Fatalf("expected exit 0 (clean), got: %v\noutput:\n%s", err, out)
	}

	var parsed struct {
		Clean           bool   `json:"clean"`
		TotalViolations int    `json:"total_violations"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &parsed); jsonErr != nil {
		t.Fatalf("invalid JSON: %v\noutput:\n%s", jsonErr, out)
	}
	if !parsed.Clean {
		t.Error("expected clean=true for valid repo")
	}
	if parsed.TotalViolations != 0 {
		t.Errorf("expected 0 violations, got %d", parsed.TotalViolations)
	}
}
