package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cgould/dtree/internal/index"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Helpers shared by status tests
// ---------------------------------------------------------------------------

// initIndexedRepo sets up a full minimal repo with a SQLite index and returns
// the repoRoot. The index is opened and closed to ensure schema is created.
func initIndexedRepo(t *testing.T) string {
	t.Helper()
	repoRoot := initMinimalRepo(t)
	// initMinimalRepo already creates and closes the index DB.
	return repoRoot
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestStatusEmptyRepo(t *testing.T) {
	repoRoot := initIndexedRepo(t)

	out, _, err := runCmd(t, "--repo-root", repoRoot, "--output", "human", "status")
	// Empty repo should be clean → exit 0.
	if err != nil {
		t.Fatalf("expected exit 0, got: %v\noutput:\n%s", err, out)
	}
	if !strings.Contains(out, "All clean.") {
		t.Errorf("expected 'All clean.' in output, got:\n%s", out)
	}
}

func TestStatusWithDecisions(t *testing.T) {
	repoRoot := initIndexedRepo(t)

	// Insert an archived tree (sync skips archived trees, so no mismatch) and a decision.
	dbPath := filepath.Join(repoRoot, ".decisions", ".index.db")
	db, err := index.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}

	_, err = db.Conn().Exec(
		`INSERT OR IGNORE INTO trees(slug, archived, created_at) VALUES(?, ?, ?)`,
		"arch", 1, "2026-01-01T00:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert tree: %v", err)
	}

	id := "01JNXYZ1234567890ABCDEFGH"
	_, err = db.Conn().Exec(`
		INSERT INTO decisions(id, tree, slug, summary, description,
			status, priority, creator, assignee,
			recommended_summary, recommended_full, recommended_by,
			actual_choice, actual_choice_reason, is_recommended,
			out_of_scope_reason, schema_version, rev, content_sha256, deleted)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, "arch", "use-postgres", "Use PostgreSQL", "",
		"proposed", "high", "testactor", "", "", "", "",
		"", "", 0, "", 1, "rev1", "sha256abc", 0,
	)
	if err != nil {
		t.Fatalf("insert decision: %v", err)
	}
	db.Close()

	// The output should list "proposed:" with count 1.
	// We use --output json to reliably check counts regardless of mismatch state.
	out, _, _ := runCmd(t, "--repo-root", repoRoot, "--output", "json", "status")
	if !strings.Contains(out, `"proposed"`) {
		t.Errorf("expected 'proposed' in JSON output, got:\n%s", out)
	}
	var parsed struct {
		DecisionsByStatus map[string]int `json:"decisions_by_status"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &parsed); jsonErr != nil {
		t.Fatalf("invalid JSON: %v\noutput:\n%s", jsonErr, out)
	}
	if parsed.DecisionsByStatus["proposed"] != 1 {
		t.Errorf("expected decisions_by_status.proposed=1, got %d", parsed.DecisionsByStatus["proposed"])
	}
}

func TestStatusJSON(t *testing.T) {
	repoRoot := initIndexedRepo(t)

	out, _, err := runCmd(t, "--repo-root", repoRoot, "--output", "json", "status")
	if err != nil {
		t.Fatalf("expected exit 0, got: %v\noutput:\n%s", err, out)
	}

	var parsed struct {
		IndexDirty      bool `json:"index_dirty"`
		MigrationNeeded bool `json:"migration_needed"`
	}
	if jsonErr := json.Unmarshal([]byte(out), &parsed); jsonErr != nil {
		t.Fatalf("invalid JSON: %v\noutput:\n%s", jsonErr, out)
	}
	if parsed.IndexDirty {
		t.Error("expected index_dirty=false for fresh repo")
	}
	if parsed.MigrationNeeded {
		t.Error("expected migration_needed=false for fresh repo")
	}
}

func TestStatusYAML(t *testing.T) {
	repoRoot := initIndexedRepo(t)

	out, _, err := runCmd(t, "--repo-root", repoRoot, "--output", "yaml", "status")
	if err != nil {
		t.Fatalf("expected exit 0, got: %v\noutput:\n%s", err, out)
	}

	var parsed struct {
		IndexDirty      bool `yaml:"index_dirty"`
		MigrationNeeded bool `yaml:"migration_needed"`
	}
	if yamlErr := yaml.Unmarshal([]byte(out), &parsed); yamlErr != nil {
		t.Fatalf("invalid YAML: %v\noutput:\n%s", yamlErr, out)
	}
	if parsed.IndexDirty {
		t.Error("expected index_dirty=false for fresh repo")
	}
}

func TestStatusDetectsDirty(t *testing.T) {
	repoRoot := initIndexedRepo(t)

	// Create the .dirty marker.
	decisionsDir := filepath.Join(repoRoot, ".decisions")
	if err := os.WriteFile(filepath.Join(decisionsDir, ".dirty"), []byte(""), 0o644); err != nil {
		t.Fatalf("create .dirty: %v", err)
	}

	out, _, err := runCmd(t, "--repo-root", repoRoot, "--output", "human", "status")
	// Should exit 1 because dirty.
	if err == nil {
		t.Fatalf("expected exit 1 (dirty), but got exit 0; output:\n%s", out)
	}
	if !strings.Contains(out, "true") {
		t.Errorf("expected 'true' (dirty) in output, got:\n%s", out)
	}
}
