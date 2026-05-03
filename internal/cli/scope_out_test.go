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
	"github.com/cgould/dtree/internal/storage"
	"gopkg.in/yaml.v3"
)

// TestScopeOutHappyPath verifies file, index, and audit are updated.
func TestScopeOutHappyPath(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeProposed(t, repoRoot, "Pick a queue")

	_, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"scope-out", id,
		"--reason", "deferred to next quarter",
	)
	if err != nil {
		t.Fatalf("scope-out failed: %v", err)
	}

	// File mutated.
	d := loadDecisionFile(t, repoRoot, "backend", id)
	if d.Status != core.StatusOutOfScope {
		t.Errorf("status: got %q, want %q", d.Status, core.StatusOutOfScope)
	}
	if d.OutOfScopeReason != "deferred to next quarter" {
		t.Errorf("out_of_scope_reason: got %q", d.OutOfScopeReason)
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
	if got.Status != core.StatusOutOfScope {
		t.Errorf("index status: got %q, want %q", got.Status, core.StatusOutOfScope)
	}

	// Audit event.
	events, err := audit.Read(repoRoot, audit.Filter{Tree: "backend", Action: core.ActionScopeOut})
	if err != nil {
		t.Fatalf("audit.Read: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 scope_out event, got %d", len(events))
	}
	ev := events[0]
	if ev.Actor != "testactor" {
		t.Errorf("actor: got %q", ev.Actor)
	}
	if ev.ID != id {
		t.Errorf("id: got %q, want %q", ev.ID, id)
	}
	if status, _ := ev.Payload.After["status"].(string); status != "out_of_scope" {
		t.Errorf("after.status: got %v", ev.Payload.After["status"])
	}
	if reason, _ := ev.Payload.After["out_of_scope_reason"].(string); reason != "deferred to next quarter" {
		t.Errorf("after.reason: got %v", ev.Payload.After["out_of_scope_reason"])
	}
}

// TestScopeOutRequiresReason rejects an empty --reason.
func TestScopeOutRequiresReason(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeProposed(t, repoRoot, "Pick storage")

	_, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"scope-out", id,
	)
	if err == nil {
		t.Fatal("expected error for missing --reason, got nil")
	}
	if !strings.Contains(err.Error(), "reason") {
		t.Errorf("error should mention `reason`, got: %v", err)
	}
}

// TestScopeOutRefusesAlreadyOutOfScope refuses to re-scope-out.
func TestScopeOutRefusesAlreadyOutOfScope(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeProposed(t, repoRoot, "Pick metrics")

	if _, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"scope-out", id,
		"--reason", "first reason",
	); err != nil {
		t.Fatalf("first scope-out failed: %v", err)
	}
	_, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"scope-out", id,
		"--reason", "second reason",
	)
	if err == nil {
		t.Fatal("expected error when re-scoping-out, got nil")
	}
	if !strings.Contains(err.Error(), "out_of_scope") {
		t.Errorf("error should mention `out_of_scope`, got: %v", err)
	}
}

// TestScopeOutOutputJSON verifies JSON output is parseable.
func TestScopeOutOutputJSON(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeProposed(t, repoRoot, "Pick LB")

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"--output", "json",
		"scope-out", id,
		"--reason", "blocked",
	)
	if err != nil {
		t.Fatalf("scope-out --output json: %v", err)
	}
	var d core.Decision
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("parse JSON: %v\noutput=%q", err, out)
	}
	if d.Status != core.StatusOutOfScope {
		t.Errorf("json status: got %q", d.Status)
	}
}

// TestScopeOutOutputYAML verifies YAML output is parseable.
func TestScopeOutOutputYAML(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeProposed(t, repoRoot, "Pick CDN")

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"--output", "yaml",
		"scope-out", id,
		"--reason", "premature",
	)
	if err != nil {
		t.Fatalf("scope-out --output yaml: %v", err)
	}
	var d core.Decision
	if err := yaml.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("parse YAML: %v\noutput=%q", err, out)
	}
	if d.Status != core.StatusOutOfScope {
		t.Errorf("yaml status: got %q", d.Status)
	}
}

// loadDecisionFile reads the on-disk YAML for an id.
func loadDecisionFile(t *testing.T, repoRoot, tree, id string) *core.Decision {
	t.Helper()
	dir := filepath.Join(repoRoot, ".decisions", tree, "decisions")
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), id) {
			d, err := storage.ReadDecision(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatalf("read decision: %v", err)
			}
			return d
		}
	}
	t.Fatalf("decision file not found for %s", id)
	return nil
}
