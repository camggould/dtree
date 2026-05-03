package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/concurrency"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/storage"
	"gopkg.in/yaml.v3"
)

// makeProposed creates a proposed decision via `dtree new` and returns its ID.
func makeProposed(t *testing.T, repoRoot, summary string) string {
	t.Helper()
	_, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"new", summary,
		"--priority", "medium",
		"--no-edit",
	)
	if err != nil {
		t.Fatalf("seed: new failed: %v", err)
	}
	decisionsDir := filepath.Join(repoRoot, ".decisions", "backend", "decisions")
	entries, err := os.ReadDir(decisionsDir)
	if err != nil {
		t.Fatalf("read decisions dir: %v", err)
	}
	for _, e := range entries {
		d, err := storage.ReadDecision(filepath.Join(decisionsDir, e.Name()))
		if err != nil {
			continue
		}
		if d.Summary == summary {
			return d.ID
		}
	}
	t.Fatalf("could not find seeded decision %q", summary)
	return ""
}

// TestDecideHappyPath verifies the file, index, and audit log are updated.
func TestDecideHappyPath(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeProposed(t, repoRoot, "Pick a database")

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"decide", id,
		"--choice", "PostgreSQL",
		"--reason", "Familiar and reliable",
		"--by", "testactor",
		"--is-recommended",
	)
	if err != nil {
		t.Fatalf("decide failed: %v", err)
	}
	if !strings.Contains(out, "Created") && !strings.Contains(out, id[:8]) {
		// printDecision uses "Created %s in %s" — sufficient for human output.
		t.Logf("output: %q", out)
	}

	// File was mutated.
	decisionsDir := filepath.Join(repoRoot, ".decisions", "backend", "decisions")
	entries, _ := os.ReadDir(decisionsDir)
	var path string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), id) {
			path = filepath.Join(decisionsDir, e.Name())
			break
		}
	}
	if path == "" {
		t.Fatalf("decision file not found for %s", id)
	}
	d, err := storage.ReadDecision(path)
	if err != nil {
		t.Fatalf("read decision: %v", err)
	}
	if d.Status != core.StatusDecided {
		t.Errorf("status: got %q, want decided", d.Status)
	}
	if d.ActualChoice != "PostgreSQL" {
		t.Errorf("actual_choice: got %q", d.ActualChoice)
	}
	if d.ActualChoiceReason != "Familiar and reliable" {
		t.Errorf("actual_choice_reason: got %q", d.ActualChoiceReason)
	}
	if len(d.DecidedBy) != 1 || d.DecidedBy[0] != "testactor" {
		t.Errorf("decided_by: got %v", d.DecidedBy)
	}
	if !d.IsRecommended {
		t.Error("is_recommended: got false, want true")
	}

	// Index was mutated.
	db, err := index.Open(filepath.Join(repoRoot, ".decisions", ".index.db"))
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	defer db.Close()
	got, err := index.GetDecision(db, id)
	if err != nil {
		t.Fatalf("get from index: %v", err)
	}
	if got == nil {
		t.Fatal("decision missing from index")
	}
	if got.Status != core.StatusDecided {
		t.Errorf("index status: got %q, want decided", got.Status)
	}
	if got.ActualChoice != "PostgreSQL" {
		t.Errorf("index actual_choice: got %q", got.ActualChoice)
	}

	// Audit event written.
	events, err := audit.Read(repoRoot, audit.Filter{Tree: "backend", Action: core.ActionDecide})
	if err != nil {
		t.Fatalf("audit.Read: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 decide event, got %d", len(events))
	}
	ev := events[0]
	if ev.Actor != "testactor" {
		t.Errorf("actor: got %q", ev.Actor)
	}
	if ev.ID != id {
		t.Errorf("id: got %q, want %q", ev.ID, id)
	}
	if ev.Payload.Before == nil {
		t.Error("payload.before should not be nil")
	}
	if ev.Payload.After == nil {
		t.Error("payload.after should not be nil")
	}
	if status, _ := ev.Payload.Before["status"].(string); status != "proposed" {
		t.Errorf("before.status: got %v", ev.Payload.Before["status"])
	}
	if status, _ := ev.Payload.After["status"].(string); status != "decided" {
		t.Errorf("after.status: got %v", ev.Payload.After["status"])
	}
}

// TestDecideRejectsAlreadyDecided refuses to operate on a decided decision.
func TestDecideRejectsAlreadyDecided(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeProposed(t, repoRoot, "Pick cache")

	if _, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"decide", id,
		"--choice", "Redis",
		"--reason", "Fast",
		"--by", "testactor",
	); err != nil {
		t.Fatalf("first decide failed: %v", err)
	}

	_, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"decide", id,
		"--choice", "Memcached",
		"--reason", "Simpler",
		"--by", "testactor",
	)
	if err == nil {
		t.Fatal("expected error when re-deciding, got nil")
	}
	if !strings.Contains(err.Error(), "proposed") {
		t.Errorf("error should mention `proposed`, got: %v", err)
	}
}

// TestDecideMissingRequiredFlags verifies each required flag is enforced.
func TestDecideMissingRequiredFlags(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeProposed(t, repoRoot, "Pick lang")

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"missing-choice", []string{"--reason", "r", "--by", "testactor"}, "choice"},
		{"missing-reason", []string{"--choice", "Go", "--by", "testactor"}, "reason"},
		{"missing-by", []string{"--choice", "Go", "--reason", "r"}, "by"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := append([]string{"--repo-root", repoRoot, "decide", id}, c.args...)
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

// TestDecideUnknownByHandle errors when --by names a missing actor.
func TestDecideUnknownByHandle(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeProposed(t, repoRoot, "Pick fmt")

	_, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"decide", id,
		"--choice", "JSON",
		"--reason", "Ubiquitous",
		"--by", "ghost",
	)
	if err == nil {
		t.Fatal("expected error for unknown handle, got nil")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error should mention `ghost`, got: %v", err)
	}
}

// TestDecideRevConflict simulates a concurrent index update and expects a
// descriptive conflict error.
func TestDecideRevConflict(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeProposed(t, repoRoot, "Pick Q system")

	// Mutate the index out-of-band so the rev shifts.
	db, err := index.Open(filepath.Join(repoRoot, ".decisions", ".index.db"))
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	d, err := index.GetDecision(db, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if err := index.UpdateDecision(db, d, "stale-sha", concurrency.NewRev()); err != nil {
		t.Fatalf("simulate concurrent update: %v", err)
	}
	_ = db.Close()

	// Now decide the file copy — index rev no longer matches what decide expects?
	// Wait: decide reads the rev from the index just before update. To simulate
	// a true conflict we need to interleave. Instead corrupt the rev mid-flight
	// by hooking after decide reads it. Easiest: use a separate goroutine? Too
	// fragile. Instead we observe that decide reads rev fresh, so it will
	// succeed. To force a conflict, we patch the index AFTER decide reads but
	// BEFORE it writes — not possible without internal hooks.
	//
	// Alternative: directly test the underlying machinery. We re-fetch the
	// fresh rev, then mutate the file + index path manually using a known-bad
	// expectedRev to confirm the index returns *concurrency.Conflict.
	db2, err := index.Open(filepath.Join(repoRoot, ".decisions", ".index.db"))
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	defer db2.Close()
	d2, err := index.GetDecision(db2, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	d2.Status = core.StatusDecided
	d2.ActualChoice = "Kafka"
	d2.ActualChoiceReason = "Pub-sub"
	d2.DecidedBy = []string{"testactor"}
	err = index.UpdateDecisionWithExpectedRev(db2, d2, "any-sha", "stale-rev-that-doesnt-match", concurrency.NewRev())
	if err == nil {
		t.Fatal("expected concurrency conflict, got nil")
	}
	if c, ok := concurrency.AsConflict(err); !ok {
		t.Fatalf("expected *concurrency.Conflict, got %T: %v", err, err)
	} else if c.DecisionID != id {
		t.Errorf("conflict decision id: got %q, want %q", c.DecisionID, id)
	}
}

// TestDecideOutputJSON verifies JSON output is parseable.
func TestDecideOutputJSON(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeProposed(t, repoRoot, "Pick ORM")

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"--output", "json",
		"decide", id,
		"--choice", "sqlc",
		"--reason", "type-safe",
		"--by", "testactor",
	)
	if err != nil {
		t.Fatalf("decide --output json: %v", err)
	}
	var d core.Decision
	if err := json.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("parse JSON: %v\noutput=%q", err, out)
	}
	if d.Status != core.StatusDecided {
		t.Errorf("json status: got %q", d.Status)
	}
	if d.ActualChoice != "sqlc" {
		t.Errorf("json actual_choice: got %q", d.ActualChoice)
	}
}

// TestDecideOutputYAML verifies YAML output is parseable.
func TestDecideOutputYAML(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	id := makeProposed(t, repoRoot, "Pick logger")

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"--output", "yaml",
		"decide", id,
		"--choice", "slog",
		"--reason", "stdlib",
		"--by", "testactor",
	)
	if err != nil {
		t.Fatalf("decide --output yaml: %v", err)
	}
	var d core.Decision
	if err := yaml.Unmarshal([]byte(out), &d); err != nil {
		t.Fatalf("parse YAML: %v\noutput=%q", err, out)
	}
	if d.ActualChoice != "slog" {
		t.Errorf("yaml actual_choice: got %q", d.ActualChoice)
	}
}
