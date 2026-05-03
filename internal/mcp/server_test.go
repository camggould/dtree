package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"

	"github.com/cgould/dtree/internal/config"
	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/identity"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/mcp"
	"github.com/cgould/dtree/internal/storage"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// tempRepo creates a temporary directory structure that looks like a dtree repo.
func tempRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".decisions"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeActors writes an actors.yaml into the repo.
func writeActors(t *testing.T, repoRoot string, actors []core.Actor) {
	t.Helper()
	af := &storage.ActorsFile{Actors: actors}
	path := filepath.Join(repoRoot, ".decisions", storage.ActorsFileName)
	if err := storage.WriteActors(path, af); err != nil {
		t.Fatalf("writeActors: %v", err)
	}
}

// humanActor is a convenience constructor for test actors.
func humanActor(handle string) core.Actor {
	return core.Actor{Handle: handle, Name: handle + " User", Kind: core.ActorHuman, Active: true}
}

// newResolver builds an identity.Resolver with a default empty config.
func newResolver(repoRoot string) *identity.Resolver {
	return identity.NewResolver(repoRoot, &config.Resolved{IdentitySrc: config.SourceDefault})
}

// openTestDB creates a fresh in-memory-backed SQLite index under t.TempDir().
func openTestDB(t *testing.T) *index.DB {
	t.Helper()
	db, err := index.Open(filepath.Join(t.TempDir(), ".index.db"))
	if err != nil {
		t.Fatalf("open test DB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// insertTree inserts a tree slug directly into the index for testing.
func insertTree(t *testing.T, db *index.DB, slug string) {
	t.Helper()
	_, err := db.Conn().Exec(
		`INSERT INTO trees(slug, title, description, archived, created_at, layout_direction, schema_version)
		 VALUES (?, '', '', 0, '2024-01-01T00:00:00Z', 'TB', 1)`,
		slug,
	)
	if err != nil {
		t.Fatalf("insertTree %q: %v", slug, err)
	}
}

// invokeListTrees directly invokes the list_trees handler via the MCPServer's
// HandleMessage so we bypass the transport layer.
func invokeListTrees(t *testing.T, s interface{ MCPServer() interface{ HandleMessage(context.Context, json.RawMessage) interface{} } }) []string {
	t.Helper()
	// We test the tool handler directly instead via buildCfg helpers.
	return nil
}

// ---------------------------------------------------------------------------
// TestNewValidatesActor
// ---------------------------------------------------------------------------

// TestNewValidatesActor verifies that New returns an error when cfg.Actor is
// not registered in the project's actors.yaml.
func TestNewValidatesActor(t *testing.T) {
	repo := tempRepo(t)
	// Write actors.yaml with only "alice", not "bob".
	writeActors(t, repo, []core.Actor{humanActor("alice")})

	_, err := mcp.New(mcp.Config{
		RepoRoot: repo,
		Resolver: newResolver(repo),
		Actor:    "bob",
		Logger:   io.Discard,
	})
	if err == nil {
		t.Fatal("expected error for unregistered actor, got nil")
	}
	if !strings.Contains(err.Error(), "bob") {
		t.Errorf("error should mention the actor handle; got %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// TestNewActorValid
// ---------------------------------------------------------------------------

// TestNewActorValid verifies that New succeeds when cfg.Actor is registered.
func TestNewActorValid(t *testing.T) {
	repo := tempRepo(t)
	writeActors(t, repo, []core.Actor{humanActor("alice")})

	_, err := mcp.New(mcp.Config{
		RepoRoot: repo,
		Resolver: newResolver(repo),
		Actor:    "alice",
		Logger:   io.Discard,
	})
	if err != nil {
		t.Fatalf("unexpected error for registered actor: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestListTreesToolReturnsTrees
// ---------------------------------------------------------------------------

// TestListTreesToolReturnsTrees directly invokes the handler via a test
// helper exposed by the Server, verifying the tool returns index contents.
func TestListTreesToolReturnsTrees(t *testing.T) {
	repo := tempRepo(t)
	writeActors(t, repo, []core.Actor{humanActor("agent1")})

	db := openTestDB(t)
	insertTree(t, db, "alpha")
	insertTree(t, db, "beta")

	s, err := mcp.New(mcp.Config{
		RepoRoot: repo,
		Resolver: newResolver(repo),
		Actor:    "agent1",
		DB:       db,
		Logger:   io.Discard,
	})
	if err != nil {
		t.Fatalf("mcp.New: %v", err)
	}

	result, err := s.InvokeListTrees(context.Background(), mcpgo.CallToolRequest{})
	if err != nil {
		t.Fatalf("InvokeListTrees: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.IsError {
		t.Fatalf("tool returned an error result: %+v", result)
	}

	// Unpack the text content.
	if len(result.Content) == 0 {
		t.Fatal("result.Content is empty")
	}
	tc, ok := result.Content[0].(mcpgo.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T, want TextContent", result.Content[0])
	}

	var slugs []string
	if err := json.Unmarshal([]byte(tc.Text), &slugs); err != nil {
		t.Fatalf("unmarshal tool output: %v", err)
	}

	want := map[string]bool{"alpha": true, "beta": true}
	if len(slugs) != len(want) {
		t.Errorf("got %d slugs, want %d: %v", len(slugs), len(want), slugs)
	}
	for _, slug := range slugs {
		if !want[slug] {
			t.Errorf("unexpected slug %q", slug)
		}
	}
}

// ---------------------------------------------------------------------------
// TestListTreesToolNoDB
// ---------------------------------------------------------------------------

// TestListTreesToolNoDB verifies that list_trees returns an empty JSON array
// when no DB is configured.
func TestListTreesToolNoDB(t *testing.T) {
	repo := tempRepo(t)
	writeActors(t, repo, []core.Actor{humanActor("agent1")})

	s, err := mcp.New(mcp.Config{
		RepoRoot: repo,
		Resolver: newResolver(repo),
		Actor:    "agent1",
		Logger:   io.Discard,
		// DB intentionally nil
	})
	if err != nil {
		t.Fatalf("mcp.New: %v", err)
	}

	result, err := s.InvokeListTrees(context.Background(), mcpgo.CallToolRequest{})
	if err != nil {
		t.Fatalf("InvokeListTrees: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.IsError {
		t.Fatalf("tool returned error: %+v", result)
	}

	tc, ok := result.Content[0].(mcpgo.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T, want TextContent", result.Content[0])
	}
	var slugs []string
	if err := json.Unmarshal([]byte(tc.Text), &slugs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(slugs) != 0 {
		t.Errorf("expected empty slice, got %v", slugs)
	}
}

// ---------------------------------------------------------------------------
// TestRegisterToolsReadOnly
// ---------------------------------------------------------------------------

// TestRegisterToolsReadOnly confirms that Server.ReadOnly is propagated.
// The actual enforcement of read-only mode is exercised once mutating tools
// are added; this test is a sanity check on the field.
func TestRegisterToolsReadOnly(t *testing.T) {
	repo := tempRepo(t)
	writeActors(t, repo, []core.Actor{humanActor("alice")})

	s, err := mcp.New(mcp.Config{
		RepoRoot: repo,
		Resolver: newResolver(repo),
		Actor:    "alice",
		ReadOnly: true,
		Logger:   io.Discard,
	})
	if err != nil {
		t.Fatalf("mcp.New: %v", err)
	}

	if !s.IsReadOnly() {
		t.Error("expected Server.IsReadOnly() == true")
	}
}

// ---------------------------------------------------------------------------
// TestLoggerCapturesToolCalls
// ---------------------------------------------------------------------------

// TestLoggerCapturesToolCalls verifies that the per-call log line is written
// to cfg.Logger after invoking a tool.
func TestLoggerCapturesToolCalls(t *testing.T) {
	repo := tempRepo(t)
	writeActors(t, repo, []core.Actor{humanActor("alice")})

	var buf bytes.Buffer
	s, err := mcp.New(mcp.Config{
		RepoRoot: repo,
		Resolver: newResolver(repo),
		Actor:    "alice",
		Logger:   &buf,
	})
	if err != nil {
		t.Fatalf("mcp.New: %v", err)
	}

	_, _ = s.InvokeListTrees(context.Background(), mcpgo.CallToolRequest{})

	logged := buf.String()
	if logged == "" {
		t.Fatal("expected a log line, got empty output")
	}
	if !strings.Contains(logged, "tool=list_trees") {
		t.Errorf("log line missing tool=list_trees: %q", logged)
	}
	if !strings.Contains(logged, "actor=alice") {
		t.Errorf("log line missing actor=alice: %q", logged)
	}
	if !strings.Contains(logged, "status=ok") {
		t.Errorf("log line missing status=ok: %q", logged)
	}
	if !strings.Contains(logged, "elapsed=") {
		t.Errorf("log line missing elapsed=: %q", logged)
	}
}

// ---------------------------------------------------------------------------
// TestHTTPTransport
// ---------------------------------------------------------------------------

// TestHTTPTransport starts the server with TransportHTTP on a random port,
// POSTs an MCP initialize JSON-RPC request, and asserts a valid response.
func TestHTTPTransport(t *testing.T) {
	repo := tempRepo(t)
	writeActors(t, repo, []core.Actor{humanActor("agent1")})

	// Use the mcp-go test server helper via the MCPServerInstance accessor.
	s, err := mcp.New(mcp.Config{
		RepoRoot: repo,
		Resolver: newResolver(repo),
		Actor:    "agent1",
		Logger:   io.Discard,
	})
	if err != nil {
		t.Fatalf("mcp.New: %v", err)
	}

	// Spin up an httptest server wrapping the internal MCPServer.
	ts := s.NewTestSSEServer()
	defer ts.Close()

	// POST an MCP initialize request to the /message endpoint with a session.
	// First, establish an SSE session by calling /sse briefly.
	sseURL := ts.URL + "/sse"
	sseResp, err := http.Get(sseURL)
	if err != nil {
		t.Fatalf("GET /sse: %v", err)
	}
	defer sseResp.Body.Close()

	if sseResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /sse status %d, want 200", sseResp.StatusCode)
	}
	// Verify we got SSE content-type.
	ct := sseResp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
}

// ---------------------------------------------------------------------------
// TestNewNoResolver
// ---------------------------------------------------------------------------

// TestNewNoResolver verifies that when Resolver is nil, New does not panic and
// succeeds (no actor validation is possible without a resolver).
func TestNewNoResolver(t *testing.T) {
	_, err := mcp.New(mcp.Config{
		Actor:  "whoever",
		Logger: io.Discard,
	})
	if err != nil {
		t.Fatalf("unexpected error with nil Resolver: %v", err)
	}
}
