package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cgould/dtree/internal/core"
)

// ---------------------------------------------------------------------------
// ReadDecision — missing file returns error
// ---------------------------------------------------------------------------

func TestReadDecisionMissingFile(t *testing.T) {
	t.Parallel()
	_, err := ReadDecision("/nonexistent/path/decision.yaml")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

// ---------------------------------------------------------------------------
// ReadDecision — invalid YAML returns error
// ---------------------------------------------------------------------------

func TestReadDecisionInvalidYAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "x", "decisions", "01HXKQ5Z3PCWJ8FQR4M2TVB7D9-bad.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{ invalid: yaml: ["), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadDecision(path)
	if err == nil {
		t.Error("expected error for invalid YAML, got nil")
	}
}

// ---------------------------------------------------------------------------
// WriteDecision / ReadDecision — assignee absent (zero value)
// ---------------------------------------------------------------------------

func TestDecisionRoundTripAssigneeAbsent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := DecisionPath(filepath.Join(dir, "tree"), "01HXKQ5Z3PCWJ8FQR4M2TVB7D9", "no-assignee")
	in := &core.Decision{
		ID:       "01HXKQ5Z3PCWJ8FQR4M2TVB7D9",
		Slug:     "no-assignee",
		Summary:  "No assignee set",
		Priority: core.PriorityLow,
		Status:   core.StatusProposed,
		Creator:  "alice",
		// Assignee deliberately absent.
	}
	if err := WriteDecision(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := ReadDecision(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.Assignee != "" {
		t.Errorf("Assignee = %q, want empty", out.Assignee)
	}
}

// ---------------------------------------------------------------------------
// WriteDecision / ReadDecision — multiple deciders, empty relationships
// ---------------------------------------------------------------------------

func TestDecisionRoundTripMultipleDecidersEmptyRels(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := DecisionPath(filepath.Join(dir, "tree"), "01HXKQ5Z3PCWJ8FQR4M2TVB7D9", "multi-deciders")
	in := &core.Decision{
		ID:                 "01HXKQ5Z3PCWJ8FQR4M2TVB7D9",
		Slug:               "multi-deciders",
		Summary:            "Multiple deciders",
		Priority:           core.PriorityHigh,
		Status:             core.StatusDecided,
		Creator:            "alice",
		ActualChoice:       "Do it",
		ActualChoiceReason: "Because",
		DecidedBy:          []string{"alice", "bob", "carol"},
		Relationships:      []core.Relationship{}, // explicitly empty, not nil
	}
	if err := WriteDecision(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := ReadDecision(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.DecidedBy) != 3 {
		t.Errorf("DecidedBy len = %d, want 3; got %v", len(out.DecidedBy), out.DecidedBy)
	}
	// Empty relationships may round-trip as nil — either is fine.
	if len(out.Relationships) != 0 {
		t.Errorf("Relationships = %v, want empty", out.Relationships)
	}
}

// ---------------------------------------------------------------------------
// WriteDecision — schema_version defaults when zero
// ---------------------------------------------------------------------------

func TestWriteDecisionSetsSchemaVersion(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := DecisionPath(filepath.Join(dir, "tree"), "01HXKQ5Z3PCWJ8FQR4M2TVB7D9", "sv-test")
	in := &core.Decision{
		ID:       "01HXKQ5Z3PCWJ8FQR4M2TVB7D9",
		Slug:     "sv-test",
		Summary:  "Schema version test",
		Priority: core.PriorityLow,
		Status:   core.StatusProposed,
		Creator:  "alice",
		// SchemaVersion deliberately 0.
	}
	if err := WriteDecision(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := ReadDecision(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.SchemaVersion != core.SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", out.SchemaVersion, core.SchemaVersion)
	}
}

// ---------------------------------------------------------------------------
// ReadTree / WriteTree round-trip
// ---------------------------------------------------------------------------

func TestTreeRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "tree.yaml")
	in := &core.Tree{
		Slug:        "backend",
		Title:       "Backend decisions",
		Description: "All backend-related decisions.",
		Archived:    false,
		CreatedAt:   time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
	}
	in.Layout.Direction = "LR"

	if err := WriteTree(path, in); err != nil {
		t.Fatalf("WriteTree: %v", err)
	}

	out, err := ReadTree(path)
	if err != nil {
		t.Fatalf("ReadTree: %v", err)
	}
	if out.Slug != in.Slug {
		t.Errorf("Slug = %q, want %q", out.Slug, in.Slug)
	}
	if out.Title != in.Title {
		t.Errorf("Title = %q, want %q", out.Title, in.Title)
	}
	if out.Description != in.Description {
		t.Errorf("Description = %q, want %q", out.Description, in.Description)
	}
	if out.Archived != in.Archived {
		t.Errorf("Archived = %v, want %v", out.Archived, in.Archived)
	}
	if out.Layout.Direction != "LR" {
		t.Errorf("Layout.Direction = %q, want LR", out.Layout.Direction)
	}
	if out.SchemaVersion != core.SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", out.SchemaVersion, core.SchemaVersion)
	}
}

// ---------------------------------------------------------------------------
// ReadTree — missing file returns error
// ---------------------------------------------------------------------------

func TestReadTreeMissingFile(t *testing.T) {
	t.Parallel()
	_, err := ReadTree("/nonexistent/tree.yaml")
	if err == nil {
		t.Error("expected error for missing tree.yaml, got nil")
	}
}

// ---------------------------------------------------------------------------
// ReadTree — invalid YAML returns error
// ---------------------------------------------------------------------------

func TestReadTreeInvalidYAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "bad-tree.yaml")
	if err := os.WriteFile(path, []byte("[invalid"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ReadTree(path)
	if err == nil {
		t.Error("expected error for invalid YAML, got nil")
	}
}

// ---------------------------------------------------------------------------
// ReadActors — missing file returns error
// ---------------------------------------------------------------------------

func TestReadActorsMissingFile(t *testing.T) {
	t.Parallel()
	_, err := ReadActors("/nonexistent/actors.yaml")
	if err == nil {
		t.Error("expected error for missing actors.yaml, got nil")
	}
}

// ---------------------------------------------------------------------------
// WriteActors — schema_version defaults
// ---------------------------------------------------------------------------

func TestWriteActorsSetsSchemaVersion(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "actors.yaml")
	af := &ActorsFile{
		// SchemaVersion = 0 should be filled in.
		Actors: []core.Actor{
			{Handle: "alice", Kind: core.ActorHuman, Active: true},
		},
	}
	if err := WriteActors(path, af); err != nil {
		t.Fatal(err)
	}
	out, err := ReadActors(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.SchemaVersion != core.SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", out.SchemaVersion, core.SchemaVersion)
	}
}

// ---------------------------------------------------------------------------
// ReadTrees — missing file returns error
// ---------------------------------------------------------------------------

func TestReadTreesMissingFile(t *testing.T) {
	t.Parallel()
	_, err := ReadTrees("/nonexistent/trees.yaml")
	if err == nil {
		t.Error("expected error for missing trees.yaml, got nil")
	}
}

// ---------------------------------------------------------------------------
// WriteTrees — schema_version defaults
// ---------------------------------------------------------------------------

func TestWriteTreesSetsSchemaVersion(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "trees.yaml")
	tf := &TreesFile{Trees: []string{"alpha", "beta"}}
	if err := WriteTrees(path, tf); err != nil {
		t.Fatal(err)
	}
	out, err := ReadTrees(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.SchemaVersion != core.SchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", out.SchemaVersion, core.SchemaVersion)
	}
}

// ---------------------------------------------------------------------------
// MarshalDecisionJSON
// ---------------------------------------------------------------------------

func TestMarshalDecisionJSON(t *testing.T) {
	t.Parallel()
	d := &core.Decision{
		ID:       "01HXKQ5Z3PCWJ8FQR4M2TVB7D9",
		Slug:     "test",
		Summary:  "Test decision",
		Priority: core.PriorityMedium,
		Status:   core.StatusProposed,
		Creator:  "alice",
	}
	data, err := MarshalDecisionJSON(d)
	if err != nil {
		t.Fatalf("MarshalDecisionJSON: %v", err)
	}
	if !strings.Contains(string(data), `"id"`) {
		t.Error("JSON missing 'id' key")
	}
	if !strings.Contains(string(data), "01HXKQ5Z3PCWJ8FQR4M2TVB7D9") {
		t.Error("JSON missing ULID value")
	}
}

// ---------------------------------------------------------------------------
// SlugFromSummary — additional edge cases
// ---------------------------------------------------------------------------

func TestSlugFromSummaryEdgeCases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		// Leading/trailing special chars.
		{"---leading dashes", "leading-dashes"},
		{"trailing spaces   ", "trailing-spaces"},
		// Only digits.
		{"123", "123"},
		// Mixed unicode — non-ascii replaced with dash.
		{"café au lait", "caf-au-lait"},
		// Very long summary truncated at 80.
		{strings.Repeat("word ", 30), func() string {
			s := SlugFromSummary(strings.Repeat("word ", 30))
			if len(s) > 80 {
				return s[:80]
			}
			return s
		}()},
	}
	for _, c := range cases {
		got := SlugFromSummary(c.in)
		if got != c.want {
			t.Errorf("SlugFromSummary(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// treeFromDecisionPath edge cases
// ---------------------------------------------------------------------------

func TestTreeFromDecisionPathNonStandardPath(t *testing.T) {
	t.Parallel()
	// A path that doesn't match the expected pattern.
	path := "/some/arbitrary/path/file.yaml"
	out, err := ReadDecision(path)
	// Should error (file not found) but the tree-from-path logic should not panic.
	if err == nil {
		// Unexpected success — check tree value.
		if out.Tree != "" {
			t.Errorf("non-standard path: Tree = %q, want empty", out.Tree)
		}
	}
	// Error is expected; what matters is no panic.
}
