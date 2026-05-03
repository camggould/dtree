package cli_test

import (
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/ulid"
	"gopkg.in/yaml.v3"
)

// lsTestRepo bootstraps a repo, opens the index, and returns both. Seeding is
// done directly through index.InsertDecision so we don't depend on the `new`
// command machinery.
func lsTestRepo(t *testing.T, treeSlug string) (string, *index.DB) {
	t.Helper()
	repoRoot := newTestRepo(t, treeSlug)
	db, err := index.Open(filepath.Join(repoRoot, ".decisions", ".index.db"))
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return repoRoot, db
}

// seedDecision inserts a decision row into the index with sensible defaults
// so each test only specifies the fields it cares about.
func seedDecision(t *testing.T, db *index.DB, d core.Decision) string {
	t.Helper()
	if d.ID == "" {
		d.ID = ulid.New()
	}
	if d.Slug == "" {
		d.Slug = strings.ToLower(strings.ReplaceAll(d.Summary, " ", "-"))
		if d.Slug == "" {
			d.Slug = "seeded"
		}
	}
	if d.Summary == "" {
		d.Summary = "Seeded decision " + d.ID[:6]
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
	d.SchemaVersion = core.SchemaVersion
	if err := index.InsertDecision(db, &d, "deadbeef"); err != nil {
		t.Fatalf("insert decision: %v", err)
	}
	return d.ID
}

// extractIDs returns the ULID list parsed from `dtree ls -o ids` output.
func extractIDs(out string) []string {
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "(") {
			continue
		}
		ids = append(ids, line)
	}
	return ids
}

// TestLsDefaultExcludesScopedOutAndSuperseded confirms the default filter only
// returns proposed/decided rows.
func TestLsDefaultExcludesScopedOutAndSuperseded(t *testing.T) {
	repoRoot, db := lsTestRepo(t, "backend")

	want := seedDecision(t, db, core.Decision{Tree: "backend", Status: core.StatusProposed})
	want2 := seedDecision(t, db, core.Decision{Tree: "backend", Status: core.StatusDecided})
	scoped := seedDecision(t, db, core.Decision{Tree: "backend", Status: core.StatusOutOfScope, OutOfScopeReason: "n/a"})
	sup := seedDecision(t, db, core.Decision{Tree: "backend", Status: core.StatusSuperseded})

	out, _, err := runCmdWithStdin(t, "", "--repo-root", repoRoot, "ls", "-o", "ids")
	if err != nil {
		t.Fatalf("ls failed: %v", err)
	}
	ids := extractIDs(out)
	got := map[string]bool{}
	for _, id := range ids {
		got[id] = true
	}
	if !got[want] || !got[want2] {
		t.Errorf("expected proposed+decided to appear; got %v", ids)
	}
	if got[scoped] || got[sup] {
		t.Errorf("expected scoped/superseded to be hidden; got %v", ids)
	}
}

// TestLsFilters runs a table of single-filter scenarios and asserts the
// returned ID set matches expectations.
func TestLsFilters(t *testing.T) {
	repoRoot, db := lsTestRepo(t, "backend")

	// Add an extra tree so --tree filter has something to discriminate on.
	if _, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"tree", "create", "frontend",
	); err != nil {
		t.Fatalf("tree create frontend: %v", err)
	}

	a := seedDecision(t, db, core.Decision{
		Tree: "backend", Status: core.StatusProposed, Priority: core.PriorityHigh,
		Creator: "alice", Assignee: "carol", RecommendedBy: "rec1",
		DecidedBy: []string{"alice"}, Tags: []string{"db", "infra"},
		Summary: "Pick database engine",
	})
	b := seedDecision(t, db, core.Decision{
		Tree: "backend", Status: core.StatusDecided, Priority: core.PriorityLow,
		Creator: "bob", Assignee: "dave", RecommendedBy: "rec2",
		DecidedBy: []string{"bob"}, Tags: []string{"db"},
		Summary: "Use migrations tool",
	})
	c := seedDecision(t, db, core.Decision{
		Tree: "frontend", Status: core.StatusProposed, Priority: core.PriorityMedium,
		Creator: "alice", Tags: []string{"ui"},
		Summary: "Choose CSS framework",
	})

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"status proposed", []string{"--status", "proposed"}, []string{a, c}},
		{"status decided", []string{"--status", "decided"}, []string{b}},
		{"priority high", []string{"--priority", "high"}, []string{a}},
		{"creator alice", []string{"--creator", "alice"}, []string{a, c}},
		{"assigned carol", []string{"--assigned", "carol"}, []string{a}},
		{"recommender rec2", []string{"--recommender", "rec2"}, []string{b}},
		{"decider alice", []string{"--decider", "alice"}, []string{a}},
		{"tree frontend", []string{"--tree", "frontend"}, []string{c}},
		{"tag db", []string{"--tag", "db"}, []string{a, b}},
		{"tag db AND infra", []string{"--tag", "db", "--tag", "infra"}, []string{a}},
		{"search migrations", []string{"--search", "migrations"}, []string{b}},
		{"status OR via comma", []string{"--status", "proposed,decided"}, []string{a, b, c}},
		{"creator alice + status proposed", []string{"--creator", "alice", "--status", "proposed"}, []string{a, c}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"--repo-root", repoRoot, "ls", "-o", "ids"}, tc.args...)
			out, _, err := runCmdWithStdin(t, "", args...)
			if err != nil {
				t.Fatalf("ls failed: %v\nstdout:%s", err, out)
			}
			gotIDs := extractIDs(out)
			gotSet := map[string]bool{}
			for _, id := range gotIDs {
				gotSet[id] = true
			}
			if len(gotIDs) != len(tc.want) {
				t.Errorf("count: got %d (%v), want %d (%v)", len(gotIDs), gotIDs, len(tc.want), tc.want)
			}
			for _, w := range tc.want {
				if !gotSet[w] {
					t.Errorf("missing expected id %s; got %v", w, gotIDs)
				}
			}
		})
	}
}

// TestLsUnblocked verifies that --unblocked excludes decisions whose blockers
// are still in non-decided status.
func TestLsUnblocked(t *testing.T) {
	repoRoot, db := lsTestRepo(t, "backend")

	// Two blockers, two targets.
	openBlocker := seedDecision(t, db, core.Decision{
		Tree: "backend", Status: core.StatusProposed, Summary: "open blocker",
	})
	doneBlocker := seedDecision(t, db, core.Decision{
		Tree: "backend", Status: core.StatusDecided, Summary: "done blocker",
	})

	// blocked target depends on openBlocker (still proposed) -> should NOT show.
	blocked := seedDecision(t, db, core.Decision{
		Tree: "backend", Status: core.StatusProposed, Summary: "blocked target",
	})
	// unblocked target depends only on doneBlocker (decided) -> should show.
	unblocked := seedDecision(t, db, core.Decision{
		Tree: "backend", Status: core.StatusProposed, Summary: "unblocked target",
	})

	// Insert relationships directly. Schema: relationships(source, target, type, tree, created_event_id).
	insertRel := func(src, tgt string) {
		if _, err := db.Conn().Exec(
			`INSERT INTO relationships(source, target, type, tree, created_event_id) VALUES(?,?,?,?,?)`,
			src, tgt, "blocks", "backend", ulid.New(),
		); err != nil {
			t.Fatalf("insert rel: %v", err)
		}
	}
	insertRel(openBlocker, blocked)
	insertRel(doneBlocker, unblocked)

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot, "ls", "--unblocked", "-o", "ids",
	)
	if err != nil {
		t.Fatalf("ls --unblocked failed: %v", err)
	}
	ids := extractIDs(out)
	gotSet := map[string]bool{}
	for _, id := range ids {
		gotSet[id] = true
	}

	if !gotSet[unblocked] {
		t.Errorf("expected unblocked target to appear; got %v", ids)
	}
	if gotSet[blocked] {
		t.Errorf("expected blocked target to be hidden; got %v", ids)
	}
	// The blockers themselves (no incoming blocks edge targeting them) qualify
	// trivially as unblocked, which matches the semantics. We only assert the
	// distinguishing pair.
}

// TestLsPaginationCursor verifies that paging with --limit + --cursor returns
// a stable, non-overlapping sequence of ids.
func TestLsPaginationCursor(t *testing.T) {
	repoRoot, db := lsTestRepo(t, "backend")

	const total = 7
	var ids []string
	for i := 0; i < total; i++ {
		ids = append(ids, seedDecision(t, db, core.Decision{Tree: "backend"}))
	}

	// Page 1.
	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot, "ls", "--limit", "3", "-o", "ids",
	)
	if err != nil {
		t.Fatalf("ls page1 failed: %v", err)
	}
	page1 := extractIDs(out)
	if len(page1) != 3 {
		t.Fatalf("page1 length: got %d, want 3", len(page1))
	}

	// Cursor = base64 of last id on page1.
	cursor := base64.RawURLEncoding.EncodeToString([]byte(page1[len(page1)-1]))
	out2, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot, "ls", "--limit", "3", "--cursor", cursor, "-o", "ids",
	)
	if err != nil {
		t.Fatalf("ls page2 failed: %v", err)
	}
	page2 := extractIDs(out2)
	if len(page2) != 3 {
		t.Fatalf("page2 length: got %d, want 3", len(page2))
	}

	// Pages must not overlap.
	seen := map[string]bool{}
	for _, id := range page1 {
		seen[id] = true
	}
	for _, id := range page2 {
		if seen[id] {
			t.Errorf("page2 overlaps page1 at %s", id)
		}
	}
	// Order should be DESC so the first id on page1 is the newest.
	if page1[0] != ids[total-1] {
		t.Errorf("page1[0]: got %s, want %s (newest)", page1[0], ids[total-1])
	}
}

// TestLsIDsOutput verifies that -o ids prints ULIDs (and only ULIDs).
func TestLsIDsOutput(t *testing.T) {
	repoRoot, db := lsTestRepo(t, "backend")
	id := seedDecision(t, db, core.Decision{Tree: "backend"})

	out, _, err := runCmdWithStdin(t, "", "--repo-root", repoRoot, "ls", "-o", "ids")
	if err != nil {
		t.Fatalf("ls failed: %v", err)
	}
	got := extractIDs(out)
	if len(got) != 1 || got[0] != id {
		t.Errorf("got %v, want [%s]", got, id)
	}
	// Each line should be a valid ULID.
	for _, line := range got {
		if err := ulid.Parse(line); err != nil {
			t.Errorf("invalid ulid %q: %v", line, err)
		}
	}
}

// TestLsJSONOutput verifies that -o json produces a parseable wrapper object.
func TestLsJSONOutput(t *testing.T) {
	repoRoot, db := lsTestRepo(t, "backend")
	id := seedDecision(t, db, core.Decision{
		Tree: "backend", Summary: "hello",
		Priority: core.PriorityHigh,
	})

	out, _, err := runCmdWithStdin(t, "", "--repo-root", repoRoot, "ls", "-o", "json")
	if err != nil {
		t.Fatalf("ls failed: %v", err)
	}
	var res struct {
		Items []struct {
			ID       string `json:"id"`
			Summary  string `json:"summary"`
			Priority string `json:"priority"`
		} `json:"items"`
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out)
	}
	if len(res.Items) != 1 || res.Items[0].ID != id || res.Items[0].Summary != "hello" {
		t.Errorf("unexpected json items: %+v", res.Items)
	}
}

// TestLsYAMLOutput verifies that -o yaml produces a parseable wrapper.
func TestLsYAMLOutput(t *testing.T) {
	repoRoot, db := lsTestRepo(t, "backend")
	id := seedDecision(t, db, core.Decision{Tree: "backend", Summary: "yamltest"})

	out, _, err := runCmdWithStdin(t, "", "--repo-root", repoRoot, "ls", "-o", "yaml")
	if err != nil {
		t.Fatalf("ls failed: %v", err)
	}
	var res struct {
		Items []struct {
			ID      string `yaml:"id"`
			Summary string `yaml:"summary"`
		} `yaml:"items"`
		NextCursor string `yaml:"next_cursor"`
	}
	if err := yaml.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v\noutput: %s", err, out)
	}
	if len(res.Items) != 1 || res.Items[0].ID != id || res.Items[0].Summary != "yamltest" {
		t.Errorf("unexpected yaml items: %+v", res.Items)
	}
}

// TestLsHumanOutputCursorHint asserts the human page shows a `(more: ...)`
// hint when results may continue.
func TestLsHumanOutputCursorHint(t *testing.T) {
	repoRoot, db := lsTestRepo(t, "backend")
	for i := 0; i < 4; i++ {
		seedDecision(t, db, core.Decision{Tree: "backend"})
	}
	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot, "ls", "--limit", "2",
	)
	if err != nil {
		t.Fatalf("ls failed: %v", err)
	}
	if !strings.Contains(out, "(more: --cursor=") {
		t.Errorf("expected cursor hint in human output, got: %s", out)
	}
}
