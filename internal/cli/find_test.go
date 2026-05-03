package cli_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cgould/dtree/internal/core"
	"gopkg.in/yaml.v3"
)

// TestFindBasicMatch inserts three decisions with distinct text and verifies
// that a query for one term returns only the matching decision.
func TestFindBasicMatch(t *testing.T) {
	repoRoot, db := lsTestRepo(t, "backend")

	want := seedDecision(t, db, core.Decision{
		Tree: "backend", Summary: "Adopt Postgres for primary store",
	})
	seedDecision(t, db, core.Decision{
		Tree: "backend", Summary: "Use Redis for ephemeral cache",
	})
	seedDecision(t, db, core.Decision{
		Tree: "backend", Summary: "Switch logging to OpenTelemetry",
	})

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot, "find", "Postgres", "-o", "ids",
	)
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	ids := extractIDs(out)
	if len(ids) != 1 || ids[0] != want {
		t.Fatalf("got %v, want [%s]", ids, want)
	}
}

// TestFindFilterByTree narrows results to one tree.
func TestFindFilterByTree(t *testing.T) {
	repoRoot, db := lsTestRepo(t, "backend")

	if _, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot, "tree", "create", "frontend",
	); err != nil {
		t.Fatalf("tree create frontend: %v", err)
	}

	backendHit := seedDecision(t, db, core.Decision{
		Tree: "backend", Summary: "kafka pipeline retention",
	})
	frontendHit := seedDecision(t, db, core.Decision{
		Tree: "frontend", Summary: "kafka dashboard widgets",
	})

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot, "find", "kafka", "--tree", "backend", "-o", "ids",
	)
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	ids := extractIDs(out)
	if len(ids) != 1 || ids[0] != backendHit {
		t.Fatalf("got %v, want [%s] (frontend hit %s should be filtered out)",
			ids, backendHit, frontendHit)
	}
}

// TestFindEmptyResultsAreNotAnError verifies a no-match query returns an
// empty items list rather than failing.
func TestFindEmptyResultsAreNotAnError(t *testing.T) {
	repoRoot, db := lsTestRepo(t, "backend")
	seedDecision(t, db, core.Decision{Tree: "backend", Summary: "nothing relevant here"})

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot, "find", "kubernetes", "-o", "json",
	)
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	var res struct {
		Items []findHitJSON `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out)
	}
	if len(res.Items) != 0 {
		t.Fatalf("expected zero items, got %+v", res.Items)
	}
}

// TestFindSnippetContainsTerm asserts the FTS snippet column highlights the
// matched term using the configured `[`/`]` delimiters.
func TestFindSnippetContainsTerm(t *testing.T) {
	repoRoot, db := lsTestRepo(t, "backend")
	seedDecision(t, db, core.Decision{
		Tree: "backend", Summary: "Adopt sourdough starter for bakery line",
	})

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot, "find", "sourdough", "-o", "json",
	)
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	var res struct {
		Items []findHitJSON `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out)
	}
	if len(res.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(res.Items))
	}
	snip := strings.ToLower(res.Items[0].Snippet)
	if !strings.Contains(snip, "sourdough") {
		t.Errorf("snippet does not contain matched term: %q", res.Items[0].Snippet)
	}
	if !strings.Contains(res.Items[0].Snippet, "[") || !strings.Contains(res.Items[0].Snippet, "]") {
		t.Errorf("snippet missing FTS delimiters: %q", res.Items[0].Snippet)
	}
}

// TestFindJSONOutput parses the JSON envelope and checks core fields.
func TestFindJSONOutput(t *testing.T) {
	repoRoot, db := lsTestRepo(t, "backend")
	id := seedDecision(t, db, core.Decision{
		Tree:     "backend",
		Summary:  "json envelope test marker",
		Priority: core.PriorityHigh,
	})

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot, "find", "marker", "-o", "json",
	)
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	var res struct {
		Items []findHitJSON `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out)
	}
	if len(res.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(res.Items))
	}
	got := res.Items[0]
	if got.ID != id || got.Tree != "backend" || got.Priority != "high" {
		t.Errorf("unexpected hit: %+v", got)
	}
}

// TestFindYAMLOutput parses the YAML envelope.
func TestFindYAMLOutput(t *testing.T) {
	repoRoot, db := lsTestRepo(t, "backend")
	id := seedDecision(t, db, core.Decision{
		Tree: "backend", Summary: "yaml envelope marker test",
	})

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot, "find", "marker", "-o", "yaml",
	)
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	var res struct {
		Items []struct {
			ID      string `yaml:"id"`
			Summary string `yaml:"summary"`
			Snippet string `yaml:"snippet"`
		} `yaml:"items"`
	}
	if err := yaml.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out)
	}
	if len(res.Items) != 1 || res.Items[0].ID != id {
		t.Fatalf("unexpected items: %+v", res.Items)
	}
}

// TestFindLimitCapsResults seeds several matching decisions and asserts the
// limit flag enforces the requested ceiling.
func TestFindLimitCapsResults(t *testing.T) {
	repoRoot, db := lsTestRepo(t, "backend")
	for i := 0; i < 5; i++ {
		seedDecision(t, db, core.Decision{
			Tree: "backend", Summary: "uniqueterm result candidate",
		})
	}

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot, "find", "uniqueterm", "--limit", "2", "-o", "ids",
	)
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	ids := extractIDs(out)
	if len(ids) != 2 {
		t.Fatalf("limit cap: got %d ids, want 2 — %v", len(ids), ids)
	}
}

// findHitJSON is the shape used to parse find's JSON output in tests.
type findHitJSON struct {
	ID       string `json:"id"`
	Tree     string `json:"tree"`
	Status   string `json:"status"`
	Priority string `json:"priority"`
	Summary  string `json:"summary"`
	Snippet  string `json:"snippet"`
}
