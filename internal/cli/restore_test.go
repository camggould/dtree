package cli_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/index"
	"gopkg.in/yaml.v3"
)

// TestRestoreHappyPath verifies restoring an out_of_scope decision to proposed.
func TestRestoreHappyPath(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeProposed(t, repoRoot, "Pick router")

	if _, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"scope-out", id,
		"--reason", "deferred",
	); err != nil {
		t.Fatalf("seed scope-out failed: %v", err)
	}

	_, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"restore", id,
	)
	if err != nil {
		t.Fatalf("restore failed: %v", err)
	}

	// File mutated.
	d := loadDecisionFile(t, repoRoot, "backend", id)
	if d.Status != core.StatusProposed {
		t.Errorf("status: got %q, want %q", d.Status, core.StatusProposed)
	}
	if d.OutOfScopeReason != "" {
		t.Errorf("out_of_scope_reason: got %q, want empty", d.OutOfScopeReason)
	}

	// Index mutated.
	db, err := index.Open(filepath.Join(repoRoot, ".decisions", ".index.db"))
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	defer db.Close()
	got, err := index.GetDecision(db, id)
	if err != nil {
		t.Fatalf("get from index: %v", err)
	}
	if got.Status != core.StatusProposed {
		t.Errorf("index status: got %q", got.Status)
	}

	// Audit event.
	events, err := audit.Read(repoRoot, audit.Filter{Tree: "backend", Action: core.ActionRestore})
	if err != nil {
		t.Fatalf("audit.Read: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 restore event, got %d", len(events))
	}
	ev := events[0]
	if ev.Actor != "testactor" {
		t.Errorf("actor: got %q", ev.Actor)
	}
	if ev.ID != id {
		t.Errorf("id: got %q", ev.ID)
	}
	if status, _ := ev.Payload.Before["status"].(string); status != "out_of_scope" {
		t.Errorf("before.status: got %v", ev.Payload.Before["status"])
	}
	if status, _ := ev.Payload.After["status"].(string); status != "proposed" {
		t.Errorf("after.status: got %v", ev.Payload.After["status"])
	}
}

// TestRestoreRefusesNonOutOfScope refuses to restore a proposed decision.
func TestRestoreRefusesNonOutOfScope(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeProposed(t, repoRoot, "Pick proto")

	_, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"restore", id,
	)
	if err == nil {
		t.Fatal("expected error restoring a proposed decision, got nil")
	}
	if !strings.Contains(err.Error(), "out_of_scope") {
		t.Errorf("error should mention `out_of_scope`, got: %v", err)
	}
}

// TestRestoreOutputJSON verifies JSON output is parseable.
func TestRestoreOutputJSON(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeProposed(t, repoRoot, "Pick gateway")
	if _, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"scope-out", id,
		"--reason", "deferred",
	); err != nil {
		t.Fatalf("seed scope-out: %v", err)
	}

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"--output", "json",
		"restore", id,
	)
	if err != nil {
		t.Fatalf("restore --output json: %v", err)
	}
	var d core.Decision
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("parse JSON: %v\noutput=%q", err, out)
	}
	if d.Status != core.StatusProposed {
		t.Errorf("json status: got %q", d.Status)
	}
}

// TestRestoreOutputYAML verifies YAML output is parseable.
func TestRestoreOutputYAML(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeProposed(t, repoRoot, "Pick framework")
	if _, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"scope-out", id,
		"--reason", "deferred",
	); err != nil {
		t.Fatalf("seed scope-out: %v", err)
	}

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"--output", "yaml",
		"restore", id,
	)
	if err != nil {
		t.Fatalf("restore --output yaml: %v", err)
	}
	var d core.Decision
	if err := yaml.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("parse YAML: %v\noutput=%q", err, out)
	}
	if d.Status != core.StatusProposed {
		t.Errorf("yaml status: got %q", d.Status)
	}
}
