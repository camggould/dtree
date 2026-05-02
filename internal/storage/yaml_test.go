package storage

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cgould/dtree/internal/core"
)

func TestDecisionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	treeDir := filepath.Join(dir, "backend")
	path := DecisionPath(treeDir, "01HXKQ5Z3PCWJ8FQR4M2TVB7D9", "pick-database")

	in := &core.Decision{
		ID:                 "01HXKQ5Z3PCWJ8FQR4M2TVB7D9",
		Slug:               "pick-database",
		Summary:            "Pick database engine",
		Priority:           core.PriorityHigh,
		Status:             core.StatusDecided,
		Creator:            "cam",
		DecidedBy:          []string{"cam", "alice"},
		IsRecommended:      true,
		ActualChoice:       "SQLite + FTS5",
		ActualChoiceReason: "Single-binary requirement.\nFTS5 fits search.",
		RecommendedSummary: "SQLite + FTS5",
		RecommendedBy:      "cam-claude",
		Tags:               []string{"storage", "infra"},
		Description:        "We need an embedded database.\nMulti-line body.",
		Relationships: []core.Relationship{
			{Type: core.RelBlocks, Target: "01HXKQ7N9MR4VXBPDTYFW2K8H1"},
		},
	}

	if err := WriteDecision(path, in); err != nil {
		t.Fatal(err)
	}

	out, err := ReadDecision(path)
	if err != nil {
		t.Fatal(err)
	}

	if out.ID != in.ID || out.Summary != in.Summary {
		t.Errorf("scalar mismatch: %+v vs %+v", out, in)
	}
	if out.Status != in.Status || out.Priority != in.Priority {
		t.Errorf("enum mismatch")
	}
	if out.Tree != "backend" {
		t.Errorf("tree slug derived = %q, want backend", out.Tree)
	}
	if out.SchemaVersion != core.SchemaVersion {
		t.Errorf("schema_version = %d, want %d", out.SchemaVersion, core.SchemaVersion)
	}
	if len(out.DecidedBy) != 2 {
		t.Errorf("decided_by lost: %v", out.DecidedBy)
	}
	if len(out.Relationships) != 1 || out.Relationships[0].Target != in.Relationships[0].Target {
		t.Errorf("relationships lost: %v", out.Relationships)
	}
	if !strings.Contains(out.ActualChoiceReason, "Multi") && !strings.Contains(out.Description, "Multi-line") {
		t.Errorf("multiline body lost")
	}
}

func TestDecisionMultilinePreserved(t *testing.T) {
	dir := t.TempDir()
	path := DecisionPath(filepath.Join(dir, "x"), "01HXKQ5Z3PCWJ8FQR4M2TVB7D9", "x")
	in := &core.Decision{
		ID:          "01HXKQ5Z3PCWJ8FQR4M2TVB7D9",
		Slug:        "x",
		Summary:     "X",
		Priority:    core.PriorityLow,
		Status:      core.StatusProposed,
		Creator:     "cam",
		Description: "Line 1\nLine 2\nLine 3\n",
	}
	if err := WriteDecision(path, in); err != nil {
		t.Fatal(err)
	}
	out, _ := ReadDecision(path)
	if !strings.Contains(out.Description, "Line 1") || !strings.Contains(out.Description, "Line 3") {
		t.Errorf("multiline body lost: %q", out.Description)
	}
}

func TestActorsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "actors.yaml")
	in := &ActorsFile{
		Actors: []core.Actor{
			{Handle: "cam", Name: "Cameron Gould", Email: "cam@example.com", Kind: core.ActorHuman, Active: true},
			{Handle: "cam-claude", Kind: core.ActorAgent, Active: true},
		},
	}
	if err := WriteActors(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := ReadActors(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Actors) != 2 || out.Actors[1].Kind != core.ActorAgent {
		t.Errorf("actors lost: %+v", out.Actors)
	}
}

func TestTreesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trees.yaml")
	in := &TreesFile{Trees: []string{"backend", "frontend"}}
	if err := WriteTrees(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := ReadTrees(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Trees) != 2 || out.Trees[0] != "backend" {
		t.Errorf("trees lost: %v", out.Trees)
	}
}

func TestSlugFromSummary(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Pick database engine", "pick-database-engine"},
		{"  Multiple   spaces!! ", "multiple-spaces"},
		{"Punctuation: a/b/c?", "punctuation-a-b-c"},
		{"", "decision"},
		{"!!!", "decision"},
		{strings.Repeat("a", 200), strings.Repeat("a", 80)},
	}
	for _, c := range cases {
		got := SlugFromSummary(c.in)
		if got != c.want {
			t.Errorf("SlugFromSummary(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDecisionPathFormat(t *testing.T) {
	got := DecisionPath("/repo/.decisions/backend", "01HXKQ5Z3PCWJ8FQR4M2TVB7D9", "pick-db")
	want := "/repo/.decisions/backend/decisions/01HXKQ5Z3PCWJ8FQR4M2TVB7D9-pick-db.yaml"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
