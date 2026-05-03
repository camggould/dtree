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

// makeDecided seeds a decided decision and returns its id.
func makeDecided(t *testing.T, repoRoot, summary string) string {
	t.Helper()
	id := makeProposed(t, repoRoot, summary)
	if _, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"decide", id,
		"--choice", "X",
		"--reason", "y",
		"--by", "testactor",
	); err != nil {
		t.Fatalf("seed decide: %v", err)
	}
	return id
}

// TestUndecideHappyPath verifies file/index/audit reset to proposed.
func TestUndecideHappyPath(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeDecided(t, repoRoot, "Pick parser")

	_, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"undecide", id,
	)
	if err != nil {
		t.Fatalf("undecide failed: %v", err)
	}

	// File mutated.
	d := loadDecisionFile(t, repoRoot, "backend", id)
	if d.Status != core.StatusProposed {
		t.Errorf("status: got %q, want %q", d.Status, core.StatusProposed)
	}
	if d.ActualChoice != "" {
		t.Errorf("actual_choice: got %q, want empty", d.ActualChoice)
	}
	if d.ActualChoiceReason != "" {
		t.Errorf("actual_choice_reason: got %q, want empty", d.ActualChoiceReason)
	}
	if len(d.DecidedBy) != 0 {
		t.Errorf("decided_by: got %v, want empty", d.DecidedBy)
	}
	if d.IsRecommended {
		t.Error("is_recommended: got true, want false")
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
	if got.ActualChoice != "" {
		t.Errorf("index actual_choice: got %q", got.ActualChoice)
	}

	// Audit event.
	events, err := audit.Read(repoRoot, audit.Filter{Tree: "backend", Action: core.ActionUndecide})
	if err != nil {
		t.Fatalf("audit.Read: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 undecide event, got %d", len(events))
	}
	ev := events[0]
	if ev.Actor != "testactor" {
		t.Errorf("actor: got %q", ev.Actor)
	}
	if ev.ID != id {
		t.Errorf("id: got %q", ev.ID)
	}
	if status, _ := ev.Payload.Before["status"].(string); status != "decided" {
		t.Errorf("before.status: got %v", ev.Payload.Before["status"])
	}
	if status, _ := ev.Payload.After["status"].(string); status != "proposed" {
		t.Errorf("after.status: got %v", ev.Payload.After["status"])
	}
}

// TestUndecideRefusesProposed refuses to undecide a proposed decision.
func TestUndecideRefusesProposed(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeProposed(t, repoRoot, "Pick build system")

	_, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"undecide", id,
	)
	if err == nil {
		t.Fatal("expected error undeciding a proposed decision, got nil")
	}
	if !strings.Contains(err.Error(), "decided") {
		t.Errorf("error should mention `decided`, got: %v", err)
	}
}

// TestUndecideOutputJSON verifies JSON output is parseable.
func TestUndecideOutputJSON(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeDecided(t, repoRoot, "Pick lint")

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"--output", "json",
		"undecide", id,
	)
	if err != nil {
		t.Fatalf("undecide --output json: %v", err)
	}
	var d core.Decision
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("parse JSON: %v\noutput=%q", err, out)
	}
	if d.Status != core.StatusProposed {
		t.Errorf("json status: got %q", d.Status)
	}
}

// TestUndecideOutputYAML verifies YAML output is parseable.
func TestUndecideOutputYAML(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeDecided(t, repoRoot, "Pick test runner")

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"--output", "yaml",
		"undecide", id,
	)
	if err != nil {
		t.Fatalf("undecide --output yaml: %v", err)
	}
	var d core.Decision
	if err := yaml.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("parse YAML: %v\noutput=%q", err, out)
	}
	if d.Status != core.StatusProposed {
		t.Errorf("yaml status: got %q", d.Status)
	}
}
