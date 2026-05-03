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

// TestAssumeHappyPath creates a fully-decided assumption.
func TestAssumeHappyPath(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"assume", "Latency budget is 100ms",
		"--choice", "100ms p95",
		"--reason", "Inherited from product spec",
	)
	if err != nil {
		t.Fatalf("assume failed: %v", err)
	}
	if !strings.Contains(out, "Created") {
		t.Errorf("expected 'Created' in human output, got: %q", out)
	}

	decisionsDir := filepath.Join(repoRoot, ".decisions", "backend", "decisions")
	entries, err := os.ReadDir(decisionsDir)
	if err != nil {
		t.Fatalf("read decisions dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 decision file, got %d", len(entries))
	}
	d, err := storage.ReadDecision(filepath.Join(decisionsDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadDecision: %v", err)
	}
	if d.Status != core.StatusDecided {
		t.Errorf("status: got %q, want decided", d.Status)
	}
	if d.Priority != core.PriorityAssumption {
		t.Errorf("priority: got %q, want assumption", d.Priority)
	}
	if d.ActualChoice != "100ms p95" {
		t.Errorf("actual_choice: got %q", d.ActualChoice)
	}
	if d.ActualChoiceReason != "Inherited from product spec" {
		t.Errorf("actual_choice_reason: got %q", d.ActualChoiceReason)
	}
	if len(d.DecidedBy) != 1 || d.DecidedBy[0] != "testactor" {
		t.Errorf("decided_by: got %v (should default to current actor)", d.DecidedBy)
	}
	if d.Creator != "testactor" {
		t.Errorf("creator: got %q", d.Creator)
	}

	// Index has the decision.
	db, err := index.Open(filepath.Join(repoRoot, ".decisions", ".index.db"))
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	defer db.Close()
	got, err := index.GetDecision(db, d.ID)
	if err != nil {
		t.Fatalf("get from index: %v", err)
	}
	if got == nil {
		t.Fatal("decision missing from index")
	}
	if got.Status != core.StatusDecided || got.Priority != core.PriorityAssumption {
		t.Errorf("index row: status=%q priority=%q", got.Status, got.Priority)
	}

	// Single create event with after.status=decided.
	events, err := audit.Read(repoRoot, audit.Filter{Tree: "backend"})
	if err != nil {
		t.Fatalf("audit.Read: %v", err)
	}
	var creates []core.Event
	for _, ev := range events {
		if ev.Action == core.ActionCreate && ev.ID == d.ID {
			creates = append(creates, ev)
		}
	}
	if len(creates) != 1 {
		t.Fatalf("expected 1 create event for assumption, got %d", len(creates))
	}
	ev := creates[0]
	if ev.Payload.After == nil {
		t.Fatal("payload.after should not be nil")
	}
	if status, _ := ev.Payload.After["status"].(string); status != "decided" {
		t.Errorf("after.status: got %v, want decided", ev.Payload.After["status"])
	}
	if priority, _ := ev.Payload.After["priority"].(string); priority != "assumption" {
		t.Errorf("after.priority: got %v, want assumption", ev.Payload.After["priority"])
	}
}

// TestAssumeWithExplicitBy verifies a custom --by overrides the default.
func TestAssumeWithExplicitBy(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	addActor(t, repoRoot, "alice")

	_, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"assume", "Use UTC everywhere",
		"--choice", "UTC",
		"--reason", "Avoids DST",
		"--by", "alice",
		"--by", "testactor",
	)
	if err != nil {
		t.Fatalf("assume failed: %v", err)
	}
	decisionsDir := filepath.Join(repoRoot, ".decisions", "backend", "decisions")
	entries, _ := os.ReadDir(decisionsDir)
	d, err := storage.ReadDecision(filepath.Join(decisionsDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadDecision: %v", err)
	}
	if len(d.DecidedBy) != 2 {
		t.Errorf("decided_by: got %v, want 2 entries", d.DecidedBy)
	}
}

// TestAssumeRejectsMissingFlags exercises required-flag validation.
func TestAssumeRejectsMissingFlags(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"missing-choice", []string{"assume", "x", "--reason", "r"}, "choice"},
		{"missing-reason", []string{"assume", "x", "--choice", "c"}, "reason"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := append([]string{"--repo-root", repoRoot}, c.args...)
			_, _, err := runCmdWithStdin(t, "", args...)
			if err == nil {
				t.Fatalf("expected error mentioning %q, got nil", c.want)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error should mention %q, got: %v", c.want, err)
			}
		})
	}
}

// TestAssumeUnknownByHandle errors when --by names a missing actor.
func TestAssumeUnknownByHandle(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")

	_, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"assume", "ghosts exist",
		"--choice", "yes",
		"--reason", "spooky",
		"--by", "nobody",
	)
	if err == nil {
		t.Fatal("expected error for unknown handle, got nil")
	}
	if !strings.Contains(err.Error(), "nobody") {
		t.Errorf("error should mention `nobody`, got: %v", err)
	}
}

// TestAssumeOutputJSON verifies JSON output is parseable.
func TestAssumeOutputJSON(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"--output", "json",
		"assume", "JSON assumption",
		"--choice", "yes",
		"--reason", "because",
	)
	if err != nil {
		t.Fatalf("assume --output json: %v", err)
	}
	var d core.Decision
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("parse JSON: %v\noutput=%q", err, out)
	}
	if d.Status != core.StatusDecided || d.Priority != core.PriorityAssumption {
		t.Errorf("json: status=%q priority=%q", d.Status, d.Priority)
	}
}

// TestAssumeOutputYAML verifies YAML output is parseable.
func TestAssumeOutputYAML(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"--output", "yaml",
		"assume", "YAML assumption",
		"--choice", "yes",
		"--reason", "because",
	)
	if err != nil {
		t.Fatalf("assume --output yaml: %v", err)
	}
	var d core.Decision
	if err := yaml.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("parse YAML: %v\noutput=%q", err, out)
	}
	if d.Priority != core.PriorityAssumption {
		t.Errorf("yaml priority: got %q", d.Priority)
	}
}
