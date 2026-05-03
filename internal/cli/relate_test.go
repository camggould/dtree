package cli_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/storage"
)

// makeDecision is a helper that creates a decision via `dtree new` and
// returns its full ULID.
func makeDecision(t *testing.T, repoRoot, treeSlug, summary string) string {
	t.Helper()
	args := []string{
		"--repo-root", repoRoot,
		"new", summary,
		"--no-edit",
		"--priority", "medium",
	}
	if treeSlug != "" {
		args = append(args, "--tree", treeSlug)
	}
	_, _, err := runCmdWithStdin(t, "", args...)
	if err != nil {
		t.Fatalf("create decision %q: %v", summary, err)
	}

	// Locate by scanning the decisions dir for the latest file with this slug.
	slug := storage.SlugFromSummary(summary)
	tree := treeSlug
	if tree == "" {
		// default tree from newTestRepo is whatever was passed first
		tree = "backend"
	}
	dir := filepath.Join(repoRoot, ".decisions", tree, "decisions")
	matches, err := filepath.Glob(filepath.Join(dir, "*-"+slug+".yaml"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("no decision file found for slug %q in %s", slug, dir)
	}
	d, err := storage.ReadDecision(matches[len(matches)-1])
	if err != nil {
		t.Fatalf("read decision %q: %v", matches[0], err)
	}
	return d.ID
}

// readDecisionByID locates and parses a decision file by id.
func readDecisionByID(t *testing.T, repoRoot, treeSlug, id string) *core.Decision {
	t.Helper()
	dir := filepath.Join(repoRoot, ".decisions", treeSlug, "decisions")
	matches, err := filepath.Glob(filepath.Join(dir, id+"-*.yaml"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("no decision file for id %s under %s", id, dir)
	}
	d, err := storage.ReadDecision(matches[0])
	if err != nil {
		t.Fatalf("read decision %s: %v", id, err)
	}
	return d
}

func TestRelateCreatesRelationship(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	src := makeDecision(t, repoRoot, "backend", "Pick a database")
	tgt := makeDecision(t, repoRoot, "backend", "Pick a cache")

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"relate", src, "blocks", tgt,
	)
	if err != nil {
		t.Fatalf("relate: %v", err)
	}
	if !strings.Contains(out, "Related") {
		t.Errorf("expected Related in output, got %q", out)
	}

	// Check file.
	d := readDecisionByID(t, repoRoot, "backend", src)
	if len(d.Relationships) != 1 {
		t.Fatalf("expected 1 relationship in src file, got %d", len(d.Relationships))
	}
	if d.Relationships[0].Type != core.RelBlocks || d.Relationships[0].Target != tgt {
		t.Errorf("relationship: got %+v, want blocks->%s", d.Relationships[0], tgt)
	}

	// Check index.
	db, err := index.Open(filepath.Join(repoRoot, ".decisions", ".index.db"))
	if err != nil {
		t.Fatalf("open index: %v", err)
	}
	defer db.Close()
	indexed, err := index.GetDecision(db, src)
	if err != nil {
		t.Fatalf("GetDecision: %v", err)
	}
	if len(indexed.Relationships) != 1 || indexed.Relationships[0].Target != tgt {
		t.Errorf("index relationships: got %+v", indexed.Relationships)
	}

	// Check audit.
	events, err := audit.Read(repoRoot, audit.Filter{Action: core.ActionRelate})
	if err != nil {
		t.Fatalf("audit.Read: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 relate event, got %d", len(events))
	}
	ev := events[0]
	if ev.ID != src {
		t.Errorf("event ID: got %q, want %q", ev.ID, src)
	}
	if ev.Payload.Extra == nil {
		t.Fatal("event Extra is nil")
	}
	if got, _ := ev.Payload.Extra["target"].(string); got != tgt {
		t.Errorf("event extra target: got %q, want %q", got, tgt)
	}
	if got, _ := ev.Payload.Extra["type"].(string); got != "blocks" {
		t.Errorf("event extra type: got %q, want blocks", got)
	}
}

func TestRelateIdempotent(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	src := makeDecision(t, repoRoot, "backend", "Decision A")
	tgt := makeDecision(t, repoRoot, "backend", "Decision B")

	if _, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"relate", src, "influences", tgt,
	); err != nil {
		t.Fatalf("first relate: %v", err)
	}

	out, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"relate", src, "influences", tgt,
	)
	if err != nil {
		t.Fatalf("second relate: %v", err)
	}
	if !strings.Contains(out, "already exists") {
		t.Errorf("expected 'already exists' on second call, got %q", out)
	}

	d := readDecisionByID(t, repoRoot, "backend", src)
	if len(d.Relationships) != 1 {
		t.Errorf("expected 1 relationship after duplicate relate, got %d", len(d.Relationships))
	}
}

func TestRelateRefusesSelfRef(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	src := makeDecision(t, repoRoot, "backend", "Lone decision")

	_, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"relate", src, "blocks", src,
	)
	if err == nil {
		t.Fatal("expected error for self-ref, got nil")
	}
	if !strings.Contains(err.Error(), "itself") {
		t.Errorf("expected 'itself' in error, got %v", err)
	}
}

func TestRelateRefusesSupersedes(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	src := makeDecision(t, repoRoot, "backend", "Decision A")
	tgt := makeDecision(t, repoRoot, "backend", "Decision B")

	for _, typ := range []string{"supersedes", "superseded_by"} {
		_, _, err := runCmdWithStdin(t, "",
			"--repo-root", repoRoot,
			"relate", src, typ, tgt,
		)
		if err == nil {
			t.Errorf("expected error for type %q, got nil", typ)
			continue
		}
		want := "must be created via 'dtree supersede'"
		if !strings.Contains(err.Error(), want) {
			t.Errorf("type %q: expected %q in error, got %v", typ, want, err)
		}
	}
}

func TestRelateBlocksCycleDetected(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	a := makeDecision(t, repoRoot, "backend", "A")
	b := makeDecision(t, repoRoot, "backend", "B")
	c := makeDecision(t, repoRoot, "backend", "C")

	if _, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"relate", a, "blocks", b,
	); err != nil {
		t.Fatalf("a blocks b: %v", err)
	}
	if _, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"relate", b, "blocks", c,
	); err != nil {
		t.Fatalf("b blocks c: %v", err)
	}

	// c blocks a -> would form cycle a->b->c->a. Refused.
	_, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"relate", c, "blocks", a,
	)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
	if !strings.Contains(err.Error(), "cycle detected") {
		t.Errorf("expected 'cycle detected' in error, got %v", err)
	}
}

func TestRelateTargetMustExist(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	src := makeDecision(t, repoRoot, "backend", "Has source")

	_, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"relate", src, "blocks", "01HXKQ5Z3PCWJ8FQR4M2TVB7D9",
	)
	if err == nil {
		t.Fatal("expected error for bogus target, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found', got %v", err)
	}
}

func TestRelateHonorsAsFlag(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	src := makeDecision(t, repoRoot, "backend", "Decision A")
	tgt := makeDecision(t, repoRoot, "backend", "Decision B")

	// Add second actor.
	_, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"actor", "add", "alice", "--name", "Alice", "--email", "alice@example.com",
	)
	if err != nil {
		t.Fatalf("actor add: %v", err)
	}

	if _, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"relate", src, "relates_to", tgt,
		"--as", "alice",
	); err != nil {
		t.Fatalf("relate --as: %v", err)
	}

	events, err := audit.Read(repoRoot, audit.Filter{Action: core.ActionRelate, Actor: "alice"})
	if err != nil {
		t.Fatalf("audit.Read: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 relate event by alice, got %d", len(events))
	}
}

func TestRelateAcceptsPrefix(t *testing.T) {
	repoRoot := newTestRepo(t, "backend")
	src := makeDecision(t, repoRoot, "backend", "Decision A")
	tgt := makeDecision(t, repoRoot, "backend", "Decision B")

	// Use a long-enough prefix to disambiguate (ULIDs from the same ms share
	// leading time bits; the random tail is the last 10-or-so chars).
	srcPrefix := src[:20]
	tgtPrefix := tgt[:20]
	if srcPrefix == tgtPrefix {
		t.Fatalf("test setup: src/tgt prefixes collide, src=%s tgt=%s", src, tgt)
	}
	if _, _, err := runCmdWithStdin(t, "",
		"--repo-root", repoRoot,
		"relate", srcPrefix, "relates_to", tgtPrefix,
	); err != nil {
		t.Fatalf("relate by prefix: %v", err)
	}

	d := readDecisionByID(t, repoRoot, "backend", src)
	if len(d.Relationships) != 1 || d.Relationships[0].Target != tgt {
		t.Errorf("expected one relates_to->%s, got %+v", tgt, d.Relationships)
	}
}
