package cli_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/storage"
	"github.com/cgould/dtree/internal/ulid"
	"gopkg.in/yaml.v3"
)

// newTestRepo sets up a minimal .decisions/ repo with one tree and one actor.
// Returns repoRoot. Sets XDG_CONFIG_HOME and DTREE_AS, DTREE_TREE in env.
func newTestRepo(t *testing.T, treeSlug string) string {
	t.Helper()
	repoRoot, xdgDir := isolatedEnv(t)

	// Use the xdgDir but we don't need to do anything with it; isolatedEnv
	// already sets XDG_CONFIG_HOME.
	_ = xdgDir

	// Run dtree init to create the full structure.
	_, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"init",
		"--non-interactive",
		"--actor-handle", "testactor",
		"--actor-name", "Test Actor",
		"--actor-email", "test@example.com",
		"--first-tree", treeSlug,
	)
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	// Set identity in global config so `dtree new` can resolve it.
	t.Setenv("DTREE_AS", "testactor")

	return repoRoot
}

// runNewCmd runs `dtree new` with the given args, returning stdout, stderr, and error.
func runNewCmd(t *testing.T, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	return runCmdWithStdin(t, stdin, args...)
}

// TestNewWithFlags verifies that --description and --priority create a decision on disk and in the index.
func TestNewWithFlags(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")

	out, _, err := runNewCmd(t, "",
		"--repo-root", repoRoot,
		"new", "Pick a database",
		"--description", "We need to pick a database engine.",
		"--priority", "high",
		"--no-edit",
	)
	if err != nil {
		t.Fatalf("new failed: %v", err)
	}

	if !strings.Contains(out, "Created") {
		t.Errorf("expected 'Created' in output, got: %q", out)
	}

	// Check file on disk.
	decisionsDir := filepath.Join(repoRoot, ".decisions", "backend", "decisions")
	entries, err := os.ReadDir(decisionsDir)
	if err != nil {
		t.Fatalf("read decisions dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 decision file, got %d", len(entries))
	}

	decisionPath := filepath.Join(decisionsDir, entries[0].Name())
	d, err := storage.ReadDecision(decisionPath)
	if err != nil {
		t.Fatalf("ReadDecision: %v", err)
	}
	if d.Summary != "Pick a database" {
		t.Errorf("summary: got %q, want %q", d.Summary, "Pick a database")
	}
	if d.Priority != core.PriorityHigh {
		t.Errorf("priority: got %q, want %q", d.Priority, core.PriorityHigh)
	}
	if d.Description != "We need to pick a database engine." {
		t.Errorf("description: got %q, want %q", d.Description, "We need to pick a database engine.")
	}
	if d.Creator != "testactor" {
		t.Errorf("creator: got %q, want %q", d.Creator, "testactor")
	}

	// Check index.
	db, err := index.Open(filepath.Join(repoRoot, ".decisions", ".index.db"))
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	defer db.Close()

	got, err := index.GetDecision(db, d.ID)
	if err != nil {
		t.Fatalf("GetDecision: %v", err)
	}
	if got == nil {
		t.Fatal("decision not found in index")
	}
	if got.Summary != "Pick a database" {
		t.Errorf("index summary: got %q, want %q", got.Summary, "Pick a database")
	}
}

// TestNewWithFromFile verifies that --from-file reads a YAML file and creates the decision.
func TestNewWithFromFile(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")

	// Write a decision YAML to a temp file.
	decisionYAML := `summary: Use PostgreSQL
priority: medium
status: proposed
creator: testactor
decision_full_description: We should use PostgreSQL.
`
	tmpFile := filepath.Join(t.TempDir(), "decision.yaml")
	if err := os.WriteFile(tmpFile, []byte(decisionYAML), 0o644); err != nil {
		t.Fatalf("write tmp file: %v", err)
	}

	_, _, err := runNewCmd(t, "",
		"--repo-root", repoRoot,
		"new",
		"--from-file", tmpFile,
	)
	if err != nil {
		t.Fatalf("new --from-file failed: %v", err)
	}

	decisionsDir := filepath.Join(repoRoot, ".decisions", "backend", "decisions")
	entries, err := os.ReadDir(decisionsDir)
	if err != nil {
		t.Fatalf("read decisions dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(entries))
	}

	d, err := storage.ReadDecision(filepath.Join(decisionsDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadDecision: %v", err)
	}
	if d.Summary != "Use PostgreSQL" {
		t.Errorf("summary: got %q, want %q", d.Summary, "Use PostgreSQL")
	}
}

// TestNewFromStdin verifies that --from-stdin reads YAML from stdin and creates the decision.
func TestNewFromStdin(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")

	decisionYAML := `summary: Use Redis for caching
priority: low
status: proposed
creator: testactor
`
	_, _, err := runNewCmd(t, decisionYAML,
		"--repo-root", repoRoot,
		"new",
		"--from-stdin",
	)
	if err != nil {
		t.Fatalf("new --from-stdin failed: %v", err)
	}

	decisionsDir := filepath.Join(repoRoot, ".decisions", "backend", "decisions")
	entries, err := os.ReadDir(decisionsDir)
	if err != nil {
		t.Fatalf("read decisions dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(entries))
	}

	d, err := storage.ReadDecision(filepath.Join(decisionsDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadDecision: %v", err)
	}
	if d.Summary != "Use Redis for caching" {
		t.Errorf("summary: got %q, want %q", d.Summary, "Use Redis for caching")
	}
}

// TestNewInteractiveNoEdit provides stdin answers and creates a decision.
func TestNewInteractiveNoEdit(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")

	// Provide: summary (already given as arg), priority (default), description (empty)
	// The prompt reads priority then description.
	stdin := "medium\nThis is the description.\n"

	_, _, err := runNewCmd(t, stdin,
		"--repo-root", repoRoot,
		"new", "Pick a framework",
		"--no-edit",
	)
	if err != nil {
		t.Fatalf("new --no-edit failed: %v", err)
	}

	decisionsDir := filepath.Join(repoRoot, ".decisions", "backend", "decisions")
	entries, err := os.ReadDir(decisionsDir)
	if err != nil {
		t.Fatalf("read decisions dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(entries))
	}

	d, err := storage.ReadDecision(filepath.Join(decisionsDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadDecision: %v", err)
	}
	if d.Summary != "Pick a framework" {
		t.Errorf("summary: got %q, want %q", d.Summary, "Pick a framework")
	}
}

// TestNewInvalidPriority verifies that an invalid priority returns an error.
func TestNewInvalidPriority(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")

	_, _, err := runNewCmd(t, "",
		"--repo-root", repoRoot,
		"new", "Pick a database",
		"--priority", "urgent",
	)
	if err == nil {
		t.Fatal("expected error for invalid priority, got nil")
	}
	if !strings.Contains(err.Error(), "priority") && !strings.Contains(err.Error(), "validation") {
		t.Errorf("error should mention priority or validation, got: %v", err)
	}
}

// TestNewMissingSummary verifies that a missing summary returns an error.
func TestNewMissingSummary(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")

	// No summary arg, no body flags, --no-edit but stdin is empty.
	_, _, err := runNewCmd(t, "\n",
		"--repo-root", repoRoot,
		"new",
		"--no-edit",
	)
	if err == nil {
		t.Fatal("expected error for missing summary, got nil")
	}
}

// TestNewRespectsTreeFlag verifies that --tree puts the decision in the specified tree.
func TestNewRespectsTreeFlag(t *testing.T) {
	repoRoot := newTestRepo(t, "frontend")

	// Create a second tree.
	_, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"tree", "create", "backend",
	)
	if err != nil {
		t.Fatalf("tree create backend: %v", err)
	}

	_, _, err = runNewCmd(t, "",
		"--repo-root", repoRoot,
		"new", "Backend decision",
		"--tree", "backend",
		"--no-edit",
		"--priority", "low",
	)
	if err != nil {
		t.Fatalf("new --tree backend failed: %v", err)
	}

	// Check decision exists in backend tree.
	decisionsDir := filepath.Join(repoRoot, ".decisions", "backend", "decisions")
	entries, err := os.ReadDir(decisionsDir)
	if err != nil {
		t.Fatalf("read decisions dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 decision in backend, got %d", len(entries))
	}

	// Check frontend tree is empty.
	frontendDecisions := filepath.Join(repoRoot, ".decisions", "frontend", "decisions")
	frontendEntries, _ := os.ReadDir(frontendDecisions)
	if len(frontendEntries) != 0 {
		t.Errorf("expected 0 decisions in frontend, got %d", len(frontendEntries))
	}
}

// TestNewRespectsDefaultTree verifies that config default_tree is used when no --tree flag is given.
func TestNewRespectsDefaultTree(t *testing.T) {
	repoRoot := newTestRepo(t, "frontend")

	// Create a second tree.
	_, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"tree", "create", "backend",
	)
	if err != nil {
		t.Fatalf("tree create backend: %v", err)
	}

	// Set default_tree in local config.
	_, _, err = runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"config", "set", "--local", "default_tree", "backend",
	)
	if err != nil {
		t.Fatalf("config set default_tree: %v", err)
	}

	// Set DTREE_TREE env to override (higher priority than local config).
	// We want to use config, not env, so clear DTREE_TREE.
	t.Setenv("DTREE_TREE", "")

	_, _, err = runNewCmd(t, "",
		"--repo-root", repoRoot,
		"new", "Backend decision via config",
		"--no-edit",
		"--priority", "low",
	)
	if err != nil {
		t.Fatalf("new failed: %v", err)
	}

	// Decision should be in backend.
	decisionsDir := filepath.Join(repoRoot, ".decisions", "backend", "decisions")
	entries, err := os.ReadDir(decisionsDir)
	if err != nil {
		t.Fatalf("read decisions dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 decision in backend, got %d", len(entries))
	}
}

// TestNewMultipleTreesNoFlag verifies that when multiple trees exist and no --tree flag is given, an error is returned.
func TestNewMultipleTreesNoFlag(t *testing.T) {
	repoRoot := newTestRepo(t, "frontend")

	// Create a second tree.
	_, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"tree", "create", "backend",
	)
	if err != nil {
		t.Fatalf("tree create backend: %v", err)
	}

	// Ensure DTREE_TREE is not set.
	t.Setenv("DTREE_TREE", "")

	_, _, err = runNewCmd(t, "",
		"--repo-root", repoRoot,
		"new", "Ambiguous decision",
		"--no-edit",
		"--priority", "low",
	)
	if err == nil {
		t.Fatal("expected error when multiple trees and no --tree flag, got nil")
	}
	// Error should list the trees.
	if !strings.Contains(err.Error(), "backend") || !strings.Contains(err.Error(), "frontend") {
		t.Errorf("error should list available trees, got: %v", err)
	}
}

// TestNewSlugDerivedFromSummary verifies that "Pick DB" produces a slug "pick-db" in the filename.
func TestNewSlugDerivedFromSummary(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")

	_, _, err := runNewCmd(t, "",
		"--repo-root", repoRoot,
		"new", "Pick DB",
		"--priority", "low",
	)
	if err != nil {
		t.Fatalf("new failed: %v", err)
	}

	decisionsDir := filepath.Join(repoRoot, ".decisions", "backend", "decisions")
	entries, err := os.ReadDir(decisionsDir)
	if err != nil {
		t.Fatalf("read decisions dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(entries))
	}

	fileName := entries[0].Name()
	if !strings.Contains(fileName, "pick-db") {
		t.Errorf("expected filename to contain 'pick-db', got %q", fileName)
	}
}

// TestNewIDIsValidULID verifies that the generated ID is a valid ULID.
func TestNewIDIsValidULID(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")

	_, _, err := runNewCmd(t, "",
		"--repo-root", repoRoot,
		"new", "ULID test decision",
		"--priority", "low",
	)
	if err != nil {
		t.Fatalf("new failed: %v", err)
	}

	decisionsDir := filepath.Join(repoRoot, ".decisions", "backend", "decisions")
	entries, err := os.ReadDir(decisionsDir)
	if err != nil {
		t.Fatalf("read decisions dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(entries))
	}

	d, err := storage.ReadDecision(filepath.Join(decisionsDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadDecision: %v", err)
	}

	if err := ulid.Parse(d.ID); err != nil {
		t.Errorf("invalid ULID %q: %v", d.ID, err)
	}
}

// TestNewWritesAuditEvent verifies that a create audit event is appended.
func TestNewWritesAuditEvent(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")

	_, _, err := runNewCmd(t, "",
		"--repo-root", repoRoot,
		"new", "Audit test decision",
		"--priority", "medium",
	)
	if err != nil {
		t.Fatalf("new failed: %v", err)
	}

	events, err := audit.Read(repoRoot, audit.Filter{Tree: "backend"})
	if err != nil {
		t.Fatalf("audit.Read: %v", err)
	}

	var found *core.Event
	for i := range events {
		if events[i].Action == core.ActionCreate && events[i].Kind == core.KindDecision {
			found = &events[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected create audit event, none found")
	}
	if found.Actor != "testactor" {
		t.Errorf("audit actor: got %q, want %q", found.Actor, "testactor")
	}
	if found.Tree != "backend" {
		t.Errorf("audit tree: got %q, want %q", found.Tree, "backend")
	}
	if found.Payload.After == nil {
		t.Error("expected non-nil payload.after")
	}
	if summary, ok := found.Payload.After["summary"].(string); !ok || summary != "Audit test decision" {
		t.Errorf("audit payload.after.summary: got %v", found.Payload.After["summary"])
	}
}

// TestNewOutputJSON verifies that --output json produces parseable JSON.
func TestNewOutputJSON(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")

	out, _, err := runNewCmd(t, "",
		"--repo-root", repoRoot,
		"--output", "json",
		"new", "JSON output test",
		"--priority", "low",
	)
	if err != nil {
		t.Fatalf("new failed: %v", err)
	}

	var d core.Decision
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("parse JSON output: %v\noutput: %s", err, out)
	}
	if d.Summary != "JSON output test" {
		t.Errorf("json summary: got %q, want %q", d.Summary, "JSON output test")
	}
	if d.ID == "" {
		t.Error("json id should not be empty")
	}
}

// TestNewOutputYAML verifies that --output yaml produces parseable YAML.
func TestNewOutputYAML(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")

	out, _, err := runNewCmd(t, "",
		"--repo-root", repoRoot,
		"--output", "yaml",
		"new", "YAML output test",
		"--priority", "low",
	)
	if err != nil {
		t.Fatalf("new failed: %v", err)
	}

	var d core.Decision
	if err := yaml.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("parse YAML output: %v\noutput: %s", err, out)
	}
	if d.Summary != "YAML output test" {
		t.Errorf("yaml summary: got %q, want %q", d.Summary, "YAML output test")
	}
}

// TestNewOutputHuman verifies that the human output contains the expected fields.
func TestNewOutputHuman(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")

	out, _, err := runNewCmd(t, "",
		"--repo-root", repoRoot,
		"new", "Human output test",
		"--priority", "low",
	)
	if err != nil {
		t.Fatalf("new failed: %v", err)
	}

	if !strings.Contains(out, "Created") {
		t.Errorf("human output should contain 'Created', got: %q", out)
	}
	if !strings.Contains(out, "backend") {
		t.Errorf("human output should contain tree name 'backend', got: %q", out)
	}
	if !strings.Contains(out, "Human output test") {
		t.Errorf("human output should contain summary, got: %q", out)
	}
}

// TestNewRejectsWhenNoDecisionsDir verifies the error message when .decisions/ is absent.
func TestNewRejectsWhenNoDecisionsDir(t *testing.T) {
	repoRoot, _ := isolatedEnv(t)

	_, _, err := runNewCmd(t, "",
		"--repo-root", repoRoot,
		"new", "Test decision",
		"--priority", "low",
	)
	if err == nil {
		t.Fatal("expected error when .decisions/ is missing, got nil")
	}
	if !strings.Contains(err.Error(), "dtree init") {
		t.Errorf("error should mention 'dtree init', got: %v", err)
	}
}

// TestNewEditorStub verifies the editor flow using a stub editor script.
func TestNewEditorStub(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")

	// Write a stub editor script that appends to the file (simulating a save).
	stubDir := t.TempDir()
	stubScript := filepath.Join(stubDir, "fake-editor.sh")
	// The script replaces the file contents with a valid decision.
	scriptContent := `#!/bin/sh
cat > "$1" << 'EOF'
summary: Editor stub decision
priority: high
status: proposed
creator: testactor
EOF
`
	if err := os.WriteFile(stubScript, []byte(scriptContent), 0o755); err != nil {
		t.Fatalf("write stub editor: %v", err)
	}

	_, _, err := runNewCmd(t, "",
		"--repo-root", repoRoot,
		"new", "Editor stub decision",
		"--editor", stubScript,
	)
	if err != nil {
		t.Fatalf("new with editor stub failed: %v", err)
	}

	decisionsDir := filepath.Join(repoRoot, ".decisions", "backend", "decisions")
	entries, err := os.ReadDir(decisionsDir)
	if err != nil {
		t.Fatalf("read decisions dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(entries))
	}

	d, err := storage.ReadDecision(filepath.Join(decisionsDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadDecision: %v", err)
	}
	if d.Summary != "Editor stub decision" {
		t.Errorf("summary: got %q, want %q", d.Summary, "Editor stub decision")
	}
}

// TestNewTagsAndAssignee verifies that tags and assignee are persisted.
func TestNewTagsAndAssignee(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")

	_, _, err := runNewCmd(t, "",
		"--repo-root", repoRoot,
		"new", "Tagged decision",
		"--priority", "low",
		"--tag", "db",
		"--tag", "infra",
		"--assignee", "testactor",
	)
	if err != nil {
		t.Fatalf("new failed: %v", err)
	}

	decisionsDir := filepath.Join(repoRoot, ".decisions", "backend", "decisions")
	entries, err := os.ReadDir(decisionsDir)
	if err != nil {
		t.Fatalf("read decisions dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(entries))
	}

	d, err := storage.ReadDecision(filepath.Join(decisionsDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadDecision: %v", err)
	}

	if len(d.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d: %v", len(d.Tags), d.Tags)
	}
	if d.Assignee != "testactor" {
		t.Errorf("assignee: got %q, want %q", d.Assignee, "testactor")
	}
}

// TestNewRecommendedFields verifies that recommendation flags are persisted.
func TestNewRecommendedFields(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")

	_, _, err := runNewCmd(t, "",
		"--repo-root", repoRoot,
		"new", "Recommended decision",
		"--priority", "medium",
		"--recommended-summary", "Use option A",
		"--recommended-full", "Option A is best because...",
		"--recommended-by", "testactor",
	)
	if err != nil {
		t.Fatalf("new failed: %v", err)
	}

	decisionsDir := filepath.Join(repoRoot, ".decisions", "backend", "decisions")
	entries, err := os.ReadDir(decisionsDir)
	if err != nil {
		t.Fatalf("read decisions dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(entries))
	}

	d, err := storage.ReadDecision(filepath.Join(decisionsDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadDecision: %v", err)
	}
	if d.RecommendedSummary != "Use option A" {
		t.Errorf("recommended_summary: got %q, want %q", d.RecommendedSummary, "Use option A")
	}
	if d.RecommendedBy != "testactor" {
		t.Errorf("recommended_by: got %q, want %q", d.RecommendedBy, "testactor")
	}
}

// helperReadAuditLine reads and parses audit JSONL files.
// This is used to directly check raw JSONL for format correctness.
func helperReadAuditLine(t *testing.T, repoRoot, tree string) []core.Event {
	t.Helper()
	events, err := audit.Read(repoRoot, audit.Filter{Tree: tree})
	if err != nil {
		t.Fatalf("audit.Read: %v", err)
	}
	return events
}

// TestNewAuditEventHasCorrectFields is a more targeted check of the audit event structure.
func TestNewAuditEventHasCorrectFields(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")

	_, _, err := runNewCmd(t, "",
		"--repo-root", repoRoot,
		"new", "Detailed audit test",
		"--priority", "critical",
		"--description", "Critical decision.",
	)
	if err != nil {
		t.Fatalf("new failed: %v", err)
	}

	events := helperReadAuditLine(t, repoRoot, "backend")

	var createEvent *core.Event
	for i := range events {
		if events[i].Action == core.ActionCreate && events[i].Kind == core.KindDecision {
			createEvent = &events[i]
			break
		}
	}
	if createEvent == nil {
		t.Fatal("expected create event, none found")
	}
	if createEvent.EventID == "" {
		t.Error("event_id should not be empty")
	}
	if createEvent.Ts.IsZero() {
		t.Error("ts should not be zero")
	}
	if createEvent.Actor != "testactor" {
		t.Errorf("actor: got %q, want %q", createEvent.Actor, "testactor")
	}
	after := createEvent.Payload.After
	if after == nil {
		t.Fatal("payload.after should not be nil")
	}
	// Verify priority is in the payload.
	if priority, ok := after["priority"].(string); !ok || priority != "critical" {
		t.Errorf("payload.after.priority: got %v", after["priority"])
	}
}

// TestNewInteractivePromptForSummary verifies that the interactive prompt asks for summary when missing.
func TestNewInteractivePromptForSummary(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")

	// Provide summary via stdin, then priority, then description.
	stdin := "Interactive summary\nlow\n\n"

	_, _, err := runNewCmd(t, stdin,
		"--repo-root", repoRoot,
		"new",
		"--no-edit",
	)
	if err != nil {
		t.Fatalf("new --no-edit with stdin summary failed: %v", err)
	}

	decisionsDir := filepath.Join(repoRoot, ".decisions", "backend", "decisions")
	entries, err := os.ReadDir(decisionsDir)
	if err != nil {
		t.Fatalf("read decisions dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(entries))
	}

	d, err := storage.ReadDecision(filepath.Join(decisionsDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadDecision: %v", err)
	}
	if d.Summary != "Interactive summary" {
		t.Errorf("summary: got %q, want %q", d.Summary, "Interactive summary")
	}
}

// Ensure bytesBuffer and related helpers are available — defined in init_test.go.
// This file uses them without redefinition.

// scanLinesFromBytes is a small helper used in some tests.
func scanLinesFromBytes(data []byte) []string {
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}

// --- helper to write actor inline for tests that need multiple actors ---

// addActor adds an actor to the repo's actors.yaml directly.
func addActor(t *testing.T, repoRoot, handle string) {
	t.Helper()
	actorsPath := filepath.Join(repoRoot, ".decisions", storage.ActorsFileName)
	af, err := storage.ReadActors(actorsPath)
	if err != nil {
		t.Fatalf("ReadActors: %v", err)
	}
	af.Actors = append(af.Actors, core.Actor{
		Handle: handle,
		Kind:   core.ActorHuman,
		Active: true,
	})
	if err := storage.WriteActors(actorsPath, af); err != nil {
		t.Fatalf("WriteActors: %v", err)
	}
}

// Ensure scanLinesFromBytes is used so the compiler doesn't complain.
var _ = fmt.Sprintf
var _ = scanLinesFromBytes
var _ = addActor
