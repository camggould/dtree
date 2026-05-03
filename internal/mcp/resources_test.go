package mcp_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/mcp"
	"github.com/cgould/dtree/internal/storage"
)

// ---------------------------------------------------------------------------
// Helpers specific to resource tests.
// ---------------------------------------------------------------------------

// writeTreeMeta writes .decisions/<slug>/tree.yaml with a minimal Tree.
func writeTreeMeta(t *testing.T, repoRoot, slug, title string) {
	t.Helper()
	dir := filepath.Join(repoRoot, ".decisions", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir tree dir: %v", err)
	}
	tree := &core.Tree{
		Slug:      slug,
		Title:     title,
		CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := storage.WriteTree(filepath.Join(dir, storage.TreeMetaFileName), tree); err != nil {
		t.Fatalf("write tree.yaml: %v", err)
	}
}

// insertSampleDecision inserts a decision into the index (and its tree row).
func insertSampleDecision(t *testing.T, db *index.DB, tree, id string) *core.Decision {
	t.Helper()
	d := &core.Decision{
		ID:            id,
		Tree:          tree,
		Slug:          "sample",
		SchemaVersion: core.SchemaVersion,
		Summary:       "sample decision",
		Priority:      core.PriorityMedium,
		Status:        core.StatusProposed,
		Creator:       "alice",
		Tags:          []string{"x", "y"},
		DecidedBy:     []string{"alice", "bob"},
		Relationships: []core.Relationship{
			{Type: core.RelInfluences, Target: "01HX0000000000000000000002"},
		},
	}
	if err := index.InsertDecision(db, d, "deadbeef"); err != nil {
		t.Fatalf("insert decision: %v", err)
	}
	return d
}

// readTextJSON unwraps a single TextResourceContents and unmarshals its body.
func readTextJSON(t *testing.T, contents []mcpgo.ResourceContents, into any) {
	t.Helper()
	if len(contents) == 0 {
		t.Fatal("empty contents")
	}
	tc, ok := contents[0].(mcpgo.TextResourceContents)
	if !ok {
		t.Fatalf("contents[0] type = %T, want TextResourceContents", contents[0])
	}
	if tc.MIMEType != "application/json" {
		t.Errorf("mime = %q, want application/json", tc.MIMEType)
	}
	if err := json.Unmarshal([]byte(tc.Text), into); err != nil {
		t.Fatalf("unmarshal body: %v\nbody: %s", err, tc.Text)
	}
}

// ---------------------------------------------------------------------------
// dtree://trees
// ---------------------------------------------------------------------------

func TestResourceListTrees(t *testing.T) {
	repo := tempRepo(t)
	writeActors(t, repo, []core.Actor{humanActor("alice")})
	db := openTestDB(t)
	insertTree(t, db, "alpha")
	insertTree(t, db, "beta")

	s, err := mcp.New(mcp.Config{
		RepoRoot: repo,
		Resolver: newResolver(repo),
		Actor:    "alice",
		DB:       db,
		Logger:   io.Discard,
	})
	if err != nil {
		t.Fatalf("mcp.New: %v", err)
	}

	contents, err := s.InvokeResource(context.Background(), "dtree://trees", nil)
	if err != nil {
		t.Fatalf("InvokeResource trees: %v", err)
	}

	var got struct {
		Trees []core.Tree `json:"trees"`
	}
	readTextJSON(t, contents, &got)

	if len(got.Trees) != 2 {
		t.Fatalf("got %d trees, want 2: %+v", len(got.Trees), got.Trees)
	}
	want := map[string]bool{"alpha": true, "beta": true}
	for _, tr := range got.Trees {
		if !want[tr.Slug] {
			t.Errorf("unexpected slug %q", tr.Slug)
		}
	}
}

func TestResourceListTreesNoDB(t *testing.T) {
	repo := tempRepo(t)
	writeActors(t, repo, []core.Actor{humanActor("alice")})

	s, err := mcp.New(mcp.Config{
		RepoRoot: repo,
		Resolver: newResolver(repo),
		Actor:    "alice",
		Logger:   io.Discard,
	})
	if err != nil {
		t.Fatalf("mcp.New: %v", err)
	}

	contents, err := s.InvokeResource(context.Background(), "dtree://trees", nil)
	if err != nil {
		t.Fatalf("InvokeResource: %v", err)
	}
	var got struct {
		Trees []core.Tree `json:"trees"`
	}
	readTextJSON(t, contents, &got)
	if len(got.Trees) != 0 {
		t.Errorf("want empty list, got %v", got.Trees)
	}
}

// ---------------------------------------------------------------------------
// dtree://trees/{tree}
// ---------------------------------------------------------------------------

func TestResourceGetTree(t *testing.T) {
	repo := tempRepo(t)
	writeActors(t, repo, []core.Actor{humanActor("alice")})
	writeTreeMeta(t, repo, "alpha", "Alpha Tree")

	s, err := mcp.New(mcp.Config{
		RepoRoot: repo,
		Resolver: newResolver(repo),
		Actor:    "alice",
		Logger:   io.Discard,
	})
	if err != nil {
		t.Fatalf("mcp.New: %v", err)
	}

	contents, err := s.InvokeResource(context.Background(),
		"dtree://trees/alpha",
		map[string]any{"tree": "alpha"})
	if err != nil {
		t.Fatalf("InvokeResource tree: %v", err)
	}

	var got core.Tree
	readTextJSON(t, contents, &got)

	if got.Slug != "alpha" {
		t.Errorf("slug = %q, want alpha", got.Slug)
	}
	if got.Title != "Alpha Tree" {
		t.Errorf("title = %q, want Alpha Tree", got.Title)
	}
}

func TestResourceGetTreeMissing(t *testing.T) {
	repo := tempRepo(t)
	writeActors(t, repo, []core.Actor{humanActor("alice")})

	s, err := mcp.New(mcp.Config{
		RepoRoot: repo,
		Resolver: newResolver(repo),
		Actor:    "alice",
		Logger:   io.Discard,
	})
	if err != nil {
		t.Fatalf("mcp.New: %v", err)
	}

	_, err = s.InvokeResource(context.Background(),
		"dtree://trees/ghost",
		map[string]any{"tree": "ghost"})
	if err == nil {
		t.Fatal("expected error for missing tree, got nil")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error should mention slug; got %q", err.Error())
	}
}

func TestResourceGetTreeMissingArg(t *testing.T) {
	repo := tempRepo(t)
	writeActors(t, repo, []core.Actor{humanActor("alice")})

	s, err := mcp.New(mcp.Config{
		RepoRoot: repo,
		Resolver: newResolver(repo),
		Actor:    "alice",
		Logger:   io.Discard,
	})
	if err != nil {
		t.Fatalf("mcp.New: %v", err)
	}

	// Pass tree arg as empty string so dispatcher routes to the tree handler
	// but the handler still rejects it.
	_, err = s.InvokeResource(context.Background(),
		"dtree://trees/",
		map[string]any{"tree": ""})
	if err == nil {
		t.Fatal("expected error for missing tree param, got nil")
	}
}

// ---------------------------------------------------------------------------
// dtree://trees/{tree}/decisions/{id}
// ---------------------------------------------------------------------------

func TestResourceGetDecision(t *testing.T) {
	repo := tempRepo(t)
	writeActors(t, repo, []core.Actor{humanActor("alice")})

	db := openTestDB(t)
	insertTree(t, db, "alpha")
	insertSampleDecision(t, db, "alpha", "01HX0000000000000000000001")

	s, err := mcp.New(mcp.Config{
		RepoRoot: repo,
		Resolver: newResolver(repo),
		Actor:    "alice",
		DB:       db,
		Logger:   io.Discard,
	})
	if err != nil {
		t.Fatalf("mcp.New: %v", err)
	}

	contents, err := s.InvokeResource(context.Background(),
		"dtree://trees/alpha/decisions/01HX0000000000000000000001",
		map[string]any{"tree": "alpha", "id": "01HX0000000000000000000001"})
	if err != nil {
		t.Fatalf("InvokeResource decision: %v", err)
	}

	var got core.Decision
	readTextJSON(t, contents, &got)

	if got.ID != "01HX0000000000000000000001" {
		t.Errorf("id = %q, want 01HX0000000000000000000001", got.ID)
	}
	if got.Tree != "alpha" {
		t.Errorf("tree = %q, want alpha", got.Tree)
	}
	if len(got.Tags) != 2 {
		t.Errorf("expected 2 tags, got %v", got.Tags)
	}
	if len(got.DecidedBy) != 2 {
		t.Errorf("expected 2 deciders, got %v", got.DecidedBy)
	}
	if len(got.Relationships) != 1 {
		t.Errorf("expected 1 relationship, got %v", got.Relationships)
	}
}

func TestResourceGetDecisionTreeMismatch(t *testing.T) {
	repo := tempRepo(t)
	writeActors(t, repo, []core.Actor{humanActor("alice")})

	db := openTestDB(t)
	insertTree(t, db, "alpha")
	insertTree(t, db, "beta")
	insertSampleDecision(t, db, "alpha", "01HX0000000000000000000003")

	s, err := mcp.New(mcp.Config{
		RepoRoot: repo,
		Resolver: newResolver(repo),
		Actor:    "alice",
		DB:       db,
		Logger:   io.Discard,
	})
	if err != nil {
		t.Fatalf("mcp.New: %v", err)
	}

	_, err = s.InvokeResource(context.Background(),
		"dtree://trees/beta/decisions/01HX0000000000000000000003",
		map[string]any{"tree": "beta", "id": "01HX0000000000000000000003"})
	if err == nil {
		t.Fatal("expected mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "alpha") {
		t.Errorf("error should mention actual tree alpha; got %q", err.Error())
	}
}

func TestResourceGetDecisionMissing(t *testing.T) {
	repo := tempRepo(t)
	writeActors(t, repo, []core.Actor{humanActor("alice")})

	db := openTestDB(t)
	insertTree(t, db, "alpha")

	s, err := mcp.New(mcp.Config{
		RepoRoot: repo,
		Resolver: newResolver(repo),
		Actor:    "alice",
		DB:       db,
		Logger:   io.Discard,
	})
	if err != nil {
		t.Fatalf("mcp.New: %v", err)
	}

	_, err = s.InvokeResource(context.Background(),
		"dtree://trees/alpha/decisions/01HX9999999999999999999999",
		map[string]any{"tree": "alpha", "id": "01HX9999999999999999999999"})
	if err == nil {
		t.Fatal("expected not-found error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention not found; got %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// dtree://actors
// ---------------------------------------------------------------------------

func TestResourceListActors(t *testing.T) {
	repo := tempRepo(t)
	actors := []core.Actor{
		humanActor("alice"),
		{Handle: "bot", Kind: core.ActorAgent, Active: true},
		{Handle: "old", Kind: core.ActorHuman, Active: false},
	}
	writeActors(t, repo, actors)

	s, err := mcp.New(mcp.Config{
		RepoRoot: repo,
		Resolver: newResolver(repo),
		Actor:    "alice",
		Logger:   io.Discard,
	})
	if err != nil {
		t.Fatalf("mcp.New: %v", err)
	}

	contents, err := s.InvokeResource(context.Background(), "dtree://actors", nil)
	if err != nil {
		t.Fatalf("InvokeResource actors: %v", err)
	}

	var got struct {
		Actors []core.Actor `json:"actors"`
	}
	readTextJSON(t, contents, &got)

	if len(got.Actors) != 3 {
		t.Fatalf("want 3 actors, got %d: %+v", len(got.Actors), got.Actors)
	}

	want := map[string]bool{"alice": true, "bot": true, "old": true}
	for _, a := range got.Actors {
		if !want[a.Handle] {
			t.Errorf("unexpected actor %q", a.Handle)
		}
	}
}

func TestResourceListActorsMissingFile(t *testing.T) {
	repo := tempRepo(t)
	// No actors.yaml on disk; New requires Resolver=nil to skip validation.

	s, err := mcp.New(mcp.Config{
		RepoRoot: repo,
		Actor:    "anyone",
		Logger:   io.Discard,
	})
	if err != nil {
		t.Fatalf("mcp.New: %v", err)
	}

	contents, err := s.InvokeResource(context.Background(), "dtree://actors", nil)
	if err != nil {
		t.Fatalf("InvokeResource: %v", err)
	}
	var got struct {
		Actors []core.Actor `json:"actors"`
	}
	readTextJSON(t, contents, &got)
	if len(got.Actors) != 0 {
		t.Errorf("want empty actors list, got %v", got.Actors)
	}
}
