package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/index"
	"gopkg.in/yaml.v3"
)

// createDecisionForDelete creates a single decision via `dtree new` and
// returns its ID by reading the index. Uses --no-edit + flags to avoid the
// editor path.
func createDecisionForDelete(t *testing.T, repoRoot, tree, summary string) string {
	t.Helper()
	args := []string{
		"--repo-root", repoRoot,
		"new", summary,
		"--no-edit",
		"--priority", "low",
	}
	if tree != "" {
		args = append(args, "--tree", tree)
	}
	if _, _, err := runCmdWithStdin(t, "", args...); err != nil {
		t.Fatalf("create decision %q: %v", summary, err)
	}

	// Find the decision ID by scanning the tree's decisions dir for the
	// matching summary.
	indexPath := filepath.Join(repoRoot, ".decisions", ".index.db")
	db, err := index.Open(indexPath)
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	defer db.Close()

	var id string
	err = db.Conn().QueryRow(
		`SELECT id FROM decisions WHERE summary = ? AND tree = ?`, summary, tree,
	).Scan(&id)
	if err != nil {
		t.Fatalf("locate decision %q in tree %q: %v", summary, tree, err)
	}
	return id
}

// findDeleteEvent returns the first delete event for id, or fails.
func findDeleteEvent(t *testing.T, repoRoot, tree, id string) *core.Event {
	t.Helper()
	events, err := audit.Read(repoRoot, audit.Filter{Tree: tree})
	if err != nil {
		t.Fatalf("audit.Read: %v", err)
	}
	for i := range events {
		if events[i].Action == core.ActionDelete && events[i].ID == id {
			return &events[i]
		}
	}
	t.Fatalf("no delete event found for %s", id)
	return nil
}

// metaFromEvent returns the payload meta map (set under Extra["meta"]).
func metaFromEvent(t *testing.T, ev *core.Event) map[string]any {
	t.Helper()
	if ev.Payload.Extra == nil {
		t.Fatalf("event %s has no Extra payload", ev.ID)
	}
	m, ok := ev.Payload.Extra["meta"].(map[string]any)
	if !ok {
		t.Fatalf("event %s payload.meta is not a map: %T", ev.ID, ev.Payload.Extra["meta"])
	}
	return m
}

// TestDeleteSoftMovesFile verifies the default soft delete path: file is
// moved to .deleted/<tree>/, index row is removed, audit event has mode=soft.
func TestDeleteSoftMovesFile(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := createDecisionForDelete(t, repoRoot, "backend", "Soft target")

	// Capture the original filename before deletion.
	originDir := filepath.Join(repoRoot, ".decisions", "backend", "decisions")
	entries, err := os.ReadDir(originDir)
	if err != nil {
		t.Fatalf("read decisions dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 decision before delete, got %d", len(entries))
	}
	origName := entries[0].Name()

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"delete", id,
	)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !strings.Contains(out, "Soft-deleted") {
		t.Errorf("expected 'Soft-deleted' in output, got %q", out)
	}

	// File should be gone from origin.
	leftover, _ := os.ReadDir(originDir)
	if len(leftover) != 0 {
		t.Errorf("expected origin dir empty after soft delete, got %d files", len(leftover))
	}

	// File should now be in .deleted/backend/.
	deletedDir := filepath.Join(repoRoot, ".decisions", ".deleted", "backend")
	if _, err := os.Stat(filepath.Join(deletedDir, origName)); err != nil {
		t.Errorf("expected moved file at %s/%s: %v", deletedDir, origName, err)
	}

	// Index row should be soft-deleted (deleted=1) — GetDecision still
	// returns it but with deleted set; we verify it no longer appears in the
	// active query by checking the deleted column directly.
	db, err := index.Open(filepath.Join(repoRoot, ".decisions", ".index.db"))
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	defer db.Close()
	var deleted int
	if err := db.Conn().QueryRow(`SELECT deleted FROM decisions WHERE id=?`, id).Scan(&deleted); err != nil {
		t.Fatalf("query deleted col: %v", err)
	}
	if deleted != 1 {
		t.Errorf("expected deleted=1 in index, got %d", deleted)
	}

	// Audit event with mode=soft.
	ev := findDeleteEvent(t, repoRoot, "backend", id)
	if ev.Tree != "backend" {
		t.Errorf("audit tree: got %q want %q", ev.Tree, "backend")
	}
	meta := metaFromEvent(t, ev)
	if mode, _ := meta["mode"].(string); mode != "soft" {
		t.Errorf("audit meta.mode: got %v, want soft", meta["mode"])
	}
	if ev.Payload.Before == nil {
		t.Error("audit before should be populated")
	}
	if summary, _ := ev.Payload.Before["summary"].(string); summary != "Soft target" {
		t.Errorf("audit before.summary: got %v", ev.Payload.Before["summary"])
	}
}

// TestDeleteSoftCollisionAppendsSuffix verifies that when a file with the
// same basename already exists in .deleted/<tree>/, the moved file gets
// a -1 suffix.
func TestDeleteSoftCollisionAppendsSuffix(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := createDecisionForDelete(t, repoRoot, "backend", "Collision target")

	// Find original filename.
	originDir := filepath.Join(repoRoot, ".decisions", "backend", "decisions")
	entries, err := os.ReadDir(originDir)
	if err != nil {
		t.Fatalf("read decisions dir: %v", err)
	}
	origName := entries[0].Name()

	// Pre-create a file with the same name in .deleted/backend/.
	deletedDir := filepath.Join(repoRoot, ".decisions", ".deleted", "backend")
	if err := os.MkdirAll(deletedDir, 0o755); err != nil {
		t.Fatalf("mkdir .deleted: %v", err)
	}
	collidingPath := filepath.Join(deletedDir, origName)
	if err := os.WriteFile(collidingPath, []byte("preexisting"), 0o644); err != nil {
		t.Fatalf("write colliding file: %v", err)
	}

	if _, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"delete", id,
	); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// The new file should be at <stem>-1.yaml.
	ext := filepath.Ext(origName)
	stem := strings.TrimSuffix(origName, ext)
	expected := filepath.Join(deletedDir, stem+"-1"+ext)
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("expected suffixed file at %s: %v", expected, err)
	}
	// Original collision file should still be intact.
	data, err := os.ReadFile(collidingPath)
	if err != nil {
		t.Errorf("collision file disappeared: %v", err)
	}
	if string(data) != "preexisting" {
		t.Errorf("collision file overwritten")
	}
}

// TestDeleteHardRemovesFile verifies that --hard unlinks the file and the
// audit event records mode=hard.
func TestDeleteHardRemovesFile(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := createDecisionForDelete(t, repoRoot, "backend", "Hard target")

	originDir := filepath.Join(repoRoot, ".decisions", "backend", "decisions")

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"delete", id,
		"--hard",
	)
	if err != nil {
		t.Fatalf("delete --hard: %v", err)
	}
	if !strings.Contains(out, "Hard-deleted") {
		t.Errorf("expected 'Hard-deleted' in output, got %q", out)
	}

	leftover, _ := os.ReadDir(originDir)
	if len(leftover) != 0 {
		t.Errorf("expected origin dir empty after hard delete, got %d files", len(leftover))
	}

	// .deleted should not contain the file.
	deletedDir := filepath.Join(repoRoot, ".decisions", ".deleted", "backend")
	if entries, err := os.ReadDir(deletedDir); err == nil && len(entries) > 0 {
		t.Errorf("expected .deleted/backend to be empty after hard delete, got %d", len(entries))
	}

	ev := findDeleteEvent(t, repoRoot, "backend", id)
	meta := metaFromEvent(t, ev)
	if mode, _ := meta["mode"].(string); mode != "hard" {
		t.Errorf("audit meta.mode: got %v, want hard", meta["mode"])
	}
}

// TestDeleteHardRefusesWithIncomingRefs verifies hard delete refuses without
// --force when other decisions reference the target.
func TestDeleteHardRefusesWithIncomingRefs(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	targetID := createDecisionForDelete(t, repoRoot, "backend", "Target with refs")
	sourceID := createDecisionForDelete(t, repoRoot, "backend", "Source with edge")

	// Insert a relationship source -> target directly via the index.
	db, err := index.Open(filepath.Join(repoRoot, ".decisions", ".index.db"))
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	if _, err := db.Conn().Exec(
		`INSERT INTO relationships(source, target, type, tree, created_event_id) VALUES(?,?,?,?,?)`,
		sourceID, targetID, "blocks", "backend", "fakeevent01234567890123456",
	); err != nil {
		t.Fatalf("insert relationship: %v", err)
	}
	db.Close()

	_, _, err = runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"delete", targetID,
		"--hard",
	)
	if err == nil {
		t.Fatal("expected error refusing hard delete with incoming refs, got nil")
	}
	if !strings.Contains(err.Error(), "incoming reference") && !strings.Contains(err.Error(), "--force") {
		t.Errorf("error should mention incoming references or --force, got: %v", err)
	}

	// File should still exist.
	originDir := filepath.Join(repoRoot, ".decisions", "backend", "decisions")
	entries, _ := os.ReadDir(originDir)
	// 2 originals (target + source).
	if len(entries) != 2 {
		t.Errorf("expected 2 files after refusal, got %d", len(entries))
	}
}

// TestDeleteHardForceRecordsDanglingRefs verifies --force breaks the refs and
// the audit event lists them under meta.dangling_refs.
func TestDeleteHardForceRecordsDanglingRefs(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	targetID := createDecisionForDelete(t, repoRoot, "backend", "Forced target")
	sourceID := createDecisionForDelete(t, repoRoot, "backend", "Source two")

	db, err := index.Open(filepath.Join(repoRoot, ".decisions", ".index.db"))
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	if _, err := db.Conn().Exec(
		`INSERT INTO relationships(source, target, type, tree, created_event_id) VALUES(?,?,?,?,?)`,
		sourceID, targetID, "influences", "backend", "fakeevent01234567890123456",
	); err != nil {
		t.Fatalf("insert relationship: %v", err)
	}
	db.Close()

	if _, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"delete", targetID,
		"--hard",
		"--force",
	); err != nil {
		t.Fatalf("delete --hard --force: %v", err)
	}

	ev := findDeleteEvent(t, repoRoot, "backend", targetID)
	meta := metaFromEvent(t, ev)
	if mode, _ := meta["mode"].(string); mode != "hard" {
		t.Errorf("meta.mode: got %v want hard", meta["mode"])
	}
	dangling, ok := meta["dangling_refs"].([]any)
	if !ok {
		t.Fatalf("meta.dangling_refs missing or wrong type: %T %v", meta["dangling_refs"], meta["dangling_refs"])
	}
	if len(dangling) != 1 {
		t.Fatalf("expected 1 dangling ref, got %d", len(dangling))
	}
	row, _ := dangling[0].(map[string]any)
	if got, _ := row["source"].(string); got != sourceID {
		t.Errorf("dangling source: got %q want %q", got, sourceID)
	}
	if got, _ := row["type"].(string); got != "influences" {
		t.Errorf("dangling type: got %q want %q", got, "influences")
	}
}

// TestDeleteOutputJSON verifies JSON output is parseable.
func TestDeleteOutputJSON(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := createDecisionForDelete(t, repoRoot, "backend", "JSON delete")

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"--output", "json",
		"delete", id,
	)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	var got struct {
		ID      string `json:"id"`
		Tree    string `json:"tree"`
		Mode    string `json:"mode"`
		MovedTo string `json:"moved_to"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parse JSON: %v\noutput: %q", err, out)
	}
	if got.ID != id {
		t.Errorf("json id: got %q want %q", got.ID, id)
	}
	if got.Mode != "soft" {
		t.Errorf("json mode: got %q want soft", got.Mode)
	}
	if got.Tree != "backend" {
		t.Errorf("json tree: got %q want backend", got.Tree)
	}
	if got.MovedTo == "" {
		t.Error("json moved_to should be set for soft delete")
	}
}

// TestDeleteOutputYAML verifies YAML output is parseable.
func TestDeleteOutputYAML(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := createDecisionForDelete(t, repoRoot, "backend", "YAML delete")

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"--output", "yaml",
		"delete", id,
		"--hard",
	)
	if err != nil {
		t.Fatalf("delete --hard: %v", err)
	}

	var got struct {
		ID   string `yaml:"id"`
		Tree string `yaml:"tree"`
		Mode string `yaml:"mode"`
	}
	if err := yaml.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parse YAML: %v\noutput: %q", err, out)
	}
	if got.ID != id {
		t.Errorf("yaml id: got %q want %q", got.ID, id)
	}
	if got.Mode != "hard" {
		t.Errorf("yaml mode: got %q want hard", got.Mode)
	}
}

// TestDeleteAcceptsPrefix verifies that an unambiguous ULID prefix resolves.
func TestDeleteAcceptsPrefix(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := createDecisionForDelete(t, repoRoot, "backend", "Prefix target")

	prefix := id[:8]
	if _, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"delete", prefix,
	); err != nil {
		t.Fatalf("delete by prefix: %v", err)
	}
}
