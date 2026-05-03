package cli_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/ulid"
	"gopkg.in/yaml.v3"
)

// showTestRepo builds a repo with a single tree and the given decisions
// inserted into the index. Returns repoRoot and the open *index.DB the caller
// can keep using (closed via t.Cleanup).
func showTestRepo(t *testing.T, treeSlug string, decisions []*core.Decision) (string, *index.DB) {
	t.Helper()
	repoRoot := newTestRepo(t, treeSlug)

	db, err := index.Open(filepath.Join(repoRoot, ".decisions", ".index.db"))
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	for _, d := range decisions {
		if d.Tree == "" {
			d.Tree = treeSlug
		}
		if d.Slug == "" {
			d.Slug = "slug-" + d.ID[:6]
		}
		if d.Status == "" {
			d.Status = core.StatusProposed
		}
		if d.Priority == "" {
			d.Priority = core.PriorityMedium
		}
		if d.Creator == "" {
			d.Creator = "testactor"
		}
		if d.SchemaVersion == 0 {
			d.SchemaVersion = core.SchemaVersion
		}
		if err := index.InsertDecision(db, d, "sha-"+d.ID[:6]); err != nil {
			t.Fatalf("insert decision %s: %v", d.ID, err)
		}
	}
	return repoRoot, db
}

// runShow runs `dtree show` with the given args.
func runShow(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	return runCmdWithStdin(t, "", args...)
}

func TestShow_ExactID(t *testing.T) {
	id := ulid.New()
	repoRoot, _ := showTestRepo(t, "backend", []*core.Decision{
		{ID: id, Summary: "Pick a database"},
	})

	out, _, err := runShow(t, "--repo-root", repoRoot, "show", id)
	if err != nil {
		t.Fatalf("show exact id: %v", err)
	}
	if !strings.Contains(out, id) || !strings.Contains(out, "Pick a database") {
		t.Errorf("expected id and summary in output, got:\n%s", out)
	}
}

func TestShow_PrefixCaseInsensitive(t *testing.T) {
	id := ulid.New()
	repoRoot, _ := showTestRepo(t, "backend", []*core.Decision{
		{ID: id, Summary: "Cache layer choice"},
	})

	prefix := strings.ToLower(id[:8])
	out, _, err := runShow(t, "--repo-root", repoRoot, "show", prefix)
	if err != nil {
		t.Fatalf("show prefix: %v", err)
	}
	if !strings.Contains(out, id) || !strings.Contains(out, "Cache layer choice") {
		t.Errorf("expected prefix match output, got:\n%s", out)
	}
}

func TestShow_SummarySubstring(t *testing.T) {
	id1, id2 := ulid.New(), ulid.New()
	// id2 has unique substring "Redis"; id1 mentions Postgres so they don't collide.
	repoRoot, _ := showTestRepo(t, "backend", []*core.Decision{
		{ID: id1, Summary: "Use Postgres for primary store"},
		{ID: id2, Summary: "Use Redis for caching"},
	})

	out, _, err := runShow(t, "--repo-root", repoRoot, "show", "redis")
	if err != nil {
		t.Fatalf("show summary: %v", err)
	}
	if !strings.Contains(out, id2) {
		t.Errorf("expected id2 in output, got:\n%s", out)
	}
	if strings.Contains(out, id1) {
		t.Errorf("did not expect id1 in output, got:\n%s", out)
	}
}

func TestShow_AmbiguousPrefix(t *testing.T) {
	// Construct two ids that share the same 4-char prefix using
	// the deterministic generator.
	prev := ulid.Default
	t.Cleanup(func() { ulid.Default = prev })
	ulid.Default = ulid.NewDeterministic(42)

	id1 := ulid.New()
	id2 := ulid.New()

	prefix := id1[:6]
	if !strings.HasPrefix(id2, prefix) {
		// Find a longer common prefix shared by the two ULIDs.
		n := 4
		for n < len(id1) && id1[n] == id2[n] {
			n++
		}
		if n < 4 {
			t.Skipf("deterministic ulids did not share a 4-char prefix (id1=%s id2=%s)", id1, id2)
		}
		prefix = id1[:n]
	}

	repoRoot, _ := showTestRepo(t, "backend", []*core.Decision{
		{ID: id1, Summary: "First decision"},
		{ID: id2, Summary: "Second decision"},
	})

	_, _, err := runShow(t, "--repo-root", repoRoot, "show", prefix)
	if err == nil {
		t.Fatalf("expected ambiguous error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "ambiguous") || !strings.Contains(msg, "matches 2") {
		t.Errorf("expected ambiguous + matches 2 in error, got: %v", err)
	}
}

func TestShow_AmbiguousSummary(t *testing.T) {
	id1, id2 := ulid.New(), ulid.New()
	repoRoot, _ := showTestRepo(t, "backend", []*core.Decision{
		{ID: id1, Summary: "Pick a database engine"},
		{ID: id2, Summary: "Database backup strategy"},
	})

	_, _, err := runShow(t, "--repo-root", repoRoot, "show", "database")
	if err == nil {
		t.Fatalf("expected ambiguous error")
	}
	if !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("expected ambiguous in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "matches 2") {
		t.Errorf("expected count in error, got: %v", err)
	}
}

func TestShow_NoMatches(t *testing.T) {
	repoRoot, _ := showTestRepo(t, "backend", []*core.Decision{
		{ID: ulid.New(), Summary: "Some decision"},
	})

	_, _, err := runShow(t, "--repo-root", repoRoot, "show", "absolutely-no-such-thing-xyz")
	if err == nil {
		t.Fatalf("expected no-matches error")
	}
	if !strings.Contains(err.Error(), "no decision matching") {
		t.Errorf("expected 'no decision matching' in error, got: %v", err)
	}
}

func TestShow_JSONOutput(t *testing.T) {
	id := ulid.New()
	repoRoot, _ := showTestRepo(t, "backend", []*core.Decision{
		{ID: id, Summary: "JSON renderable", Tags: []string{"a", "b"}},
	})

	out, _, err := runShow(t, "--repo-root", repoRoot, "show", id, "--output", "json")
	if err != nil {
		t.Fatalf("show json: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out)
	}
	if got["id"] != id {
		t.Errorf("json id: got %v, want %s", got["id"], id)
	}
	if got["summary"] != "JSON renderable" {
		t.Errorf("json summary: got %v", got["summary"])
	}
}

func TestShow_YAMLOutput(t *testing.T) {
	id := ulid.New()
	repoRoot, _ := showTestRepo(t, "backend", []*core.Decision{
		{ID: id, Summary: "YAML renderable"},
	})

	out, _, err := runShow(t, "--repo-root", repoRoot, "show", id, "--output", "yaml")
	if err != nil {
		t.Fatalf("show yaml: %v", err)
	}
	var got map[string]any
	if err := yaml.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid yaml: %v\n%s", err, out)
	}
	if got["id"] != id {
		t.Errorf("yaml id: got %v, want %s", got["id"], id)
	}
}

func TestShow_RelationshipsTargetSummary(t *testing.T) {
	id1 := ulid.New()
	time.Sleep(time.Millisecond) // ensure ULID monotonicity for distinct ids
	id2 := ulid.New()

	repoRoot, _ := showTestRepo(t, "backend", []*core.Decision{
		{ID: id2, Summary: "Target decision"},
		{
			ID:      id1,
			Summary: "Source decision",
			Relationships: []core.Relationship{
				{Type: core.RelInfluences, Target: id2},
			},
		},
	})

	// JSON: target_summary appears.
	out, _, err := runShow(t, "--repo-root", repoRoot, "show", id1, "--output", "json")
	if err != nil {
		t.Fatalf("show json: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid json: %v\n%s", err, out)
	}
	rels, ok := got["relationships"].([]any)
	if !ok || len(rels) != 1 {
		t.Fatalf("expected 1 relationship in json, got: %v", got["relationships"])
	}
	rel := rels[0].(map[string]any)
	if rel["target"] != id2 {
		t.Errorf("rel target: got %v, want %s", rel["target"], id2)
	}
	if rel["target_summary"] != "Target decision" {
		t.Errorf("rel target_summary: got %v", rel["target_summary"])
	}

	// Human: target summary is rendered too.
	out, _, err = runShow(t, "--repo-root", repoRoot, "show", id1)
	if err != nil {
		t.Fatalf("show human: %v", err)
	}
	if !strings.Contains(out, "Target decision") {
		t.Errorf("expected target summary in human output, got:\n%s", out)
	}
	if !strings.Contains(out, "influences") {
		t.Errorf("expected relationship type in output, got:\n%s", out)
	}
}
