// Package mcp — read-only MCP resources for trees, decisions, and actors.
//
// Resources expose dtree state via MCP's resources/read protocol. They are
// strictly read-only; mutations go through tools. Each resource handler
// returns a single TextResourceContents entry with mime type
// application/json and a JSON-marshalled body.
//
// URI scheme:
//
//	dtree://trees                                — list of all (non-archived) trees
//	dtree://trees/{tree}                         — single tree by slug
//	dtree://trees/{tree}/decisions/{id}          — single decision by id
//	dtree://actors                               — list of all actors
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/storage"
)

const (
	resURITrees    = "dtree://trees"
	resURIActors   = "dtree://actors"
	resURITreeTpl  = "dtree://trees/{tree}"
	resURIDecTpl   = "dtree://trees/{tree}/decisions/{id}"
	mimeJSON       = "application/json"
)

// registerResources adds the four read-only resources to the MCP server.
// Called from New after registerTools.
func (s *Server) registerResources() {
	// dtree://trees — static URI listing all trees.
	s.mcp.AddResource(
		mcpgo.NewResource(resURITrees, "Trees",
			mcpgo.WithResourceDescription("List of all non-archived decision trees in this project."),
			mcpgo.WithMIMEType(mimeJSON),
		),
		s.adaptResource(resURITrees, s.handleListTreesResource),
	)

	// dtree://actors — static URI listing all actors.
	s.mcp.AddResource(
		mcpgo.NewResource(resURIActors, "Actors",
			mcpgo.WithResourceDescription("List of all actors (active + archived) registered in this project."),
			mcpgo.WithMIMEType(mimeJSON),
		),
		s.adaptResource(resURIActors, s.handleListActorsResource),
	)

	// dtree://trees/{tree} — parameterized: single tree by slug.
	s.mcp.AddResourceTemplate(
		mcpgo.NewResourceTemplate(resURITreeTpl, "Tree",
			mcpgo.WithTemplateDescription("Metadata for a single tree, by slug."),
			mcpgo.WithTemplateMIMEType(mimeJSON),
		),
		s.adaptTemplate(resURITreeTpl, s.handleGetTreeResource),
	)

	// dtree://trees/{tree}/decisions/{id} — parameterized: single decision.
	s.mcp.AddResourceTemplate(
		mcpgo.NewResourceTemplate(resURIDecTpl, "Decision",
			mcpgo.WithTemplateDescription("Full inflated decision (deciders, tags, relationships) by tree and id."),
			mcpgo.WithTemplateMIMEType(mimeJSON),
		),
		s.adaptTemplate(resURIDecTpl, s.handleGetDecisionResource),
	)
}

// adaptResource wraps a resourceFunc with the per-call logger so resource reads
// emit the same audit-style log line as tool calls.
func (s *Server) adaptResource(name string, h resourceFunc) mcpserver.ResourceHandlerFunc {
	return func(ctx context.Context, req mcpgo.ReadResourceRequest) ([]mcpgo.ResourceContents, error) {
		start := time.Now()
		out, err := h(ctx, req)
		elapsed := time.Since(start).Milliseconds()
		status := "ok"
		if err != nil {
			status = "err"
		}
		ts := time.Now().UTC().Format(time.RFC3339)
		s.log.Printf("%s resource=%s actor=%s status=%s elapsed=%dms",
			ts, name, s.cfg.Actor, status, elapsed)
		return out, err
	}
}

// resourceFunc is the internal signature for resource handlers; identical to
// ResourceHandlerFunc / ResourceTemplateHandlerFunc but typed as a method.
type resourceFunc func(ctx context.Context, req mcpgo.ReadResourceRequest) ([]mcpgo.ResourceContents, error)

// adaptTemplate is the same as adaptResource but typed as
// ResourceTemplateHandlerFunc so it satisfies AddResourceTemplate. The
// underlying function value is identical; Go's type system requires the
// distinction.
func (s *Server) adaptTemplate(name string, h resourceFunc) mcpserver.ResourceTemplateHandlerFunc {
	wrapped := s.adaptResource(name, h)
	return mcpserver.ResourceTemplateHandlerFunc(wrapped)
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// handleListTreesResource returns all non-archived trees from the index as
// {"trees": [...]} JSON.
func (s *Server) handleListTreesResource(ctx context.Context, req mcpgo.ReadResourceRequest) ([]mcpgo.ResourceContents, error) {
	trees, err := loadTrees(ctx, s.cfg.DB)
	if err != nil {
		return nil, fmt.Errorf("list trees: %w", err)
	}
	body, err := json.Marshal(map[string]any{"trees": trees})
	if err != nil {
		return nil, fmt.Errorf("marshal trees: %w", err)
	}
	return jsonContents(req.Params.URI, body), nil
}

// handleGetTreeResource returns a single Tree by slug, looked up via
// storage.ReadTree of .decisions/<slug>/tree.yaml. Returns an error when the
// tree slug is missing or the file does not exist.
func (s *Server) handleGetTreeResource(ctx context.Context, req mcpgo.ReadResourceRequest) ([]mcpgo.ResourceContents, error) {
	slug := stringArg(req, "tree")
	if slug == "" {
		return nil, fmt.Errorf("tree: missing 'tree' parameter")
	}
	tree, err := readTreeBySlug(s.cfg.RepoRoot, slug)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(tree)
	if err != nil {
		return nil, fmt.Errorf("marshal tree: %w", err)
	}
	return jsonContents(req.Params.URI, body), nil
}

// handleGetDecisionResource returns a fully inflated Decision by id, requiring
// that the stored decision belong to {tree}. Returns an error on mismatch or
// missing id.
func (s *Server) handleGetDecisionResource(ctx context.Context, req mcpgo.ReadResourceRequest) ([]mcpgo.ResourceContents, error) {
	tree := stringArg(req, "tree")
	id := stringArg(req, "id")
	if tree == "" || id == "" {
		return nil, fmt.Errorf("decision: 'tree' and 'id' are required")
	}
	dec, err := getDecisionInTree(s.cfg.DB, tree, id)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(dec)
	if err != nil {
		return nil, fmt.Errorf("marshal decision: %w", err)
	}
	return jsonContents(req.Params.URI, body), nil
}

// handleListActorsResource returns all actors (active + archived) read from
// .decisions/actors.yaml as {"actors": [...]} JSON. Returns an empty list
// when actors.yaml does not exist.
func (s *Server) handleListActorsResource(ctx context.Context, req mcpgo.ReadResourceRequest) ([]mcpgo.ResourceContents, error) {
	actors, err := loadActors(s.cfg.RepoRoot)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]any{"actors": actors})
	if err != nil {
		return nil, fmt.Errorf("marshal actors: %w", err)
	}
	return jsonContents(req.Params.URI, body), nil
}

// ---------------------------------------------------------------------------
// Helpers — small, pure functions that are easy to test in isolation.
// ---------------------------------------------------------------------------

// loadTrees queries the index for all non-archived trees and returns them as
// fully populated core.Tree values. Returns an empty slice when DB is nil or
// the trees table is empty.
func loadTrees(ctx context.Context, db *index.DB) ([]core.Tree, error) {
	if db == nil {
		return []core.Tree{}, nil
	}
	const q = `SELECT slug, title, description, archived, created_at, layout_direction, schema_version
	           FROM trees WHERE archived = 0 ORDER BY slug`
	rows, err := db.Conn().QueryContext(ctx, q)
	if err != nil {
		if isNoSuchTable(err) {
			return []core.Tree{}, nil
		}
		return nil, fmt.Errorf("query trees: %w", err)
	}
	defer rows.Close()

	out := []core.Tree{}
	for rows.Next() {
		var (
			t        core.Tree
			archived int
			createdS string
			dir      string
		)
		if err := rows.Scan(&t.Slug, &t.Title, &t.Description, &archived, &createdS, &dir, &t.SchemaVersion); err != nil {
			return nil, fmt.Errorf("scan tree row: %w", err)
		}
		t.Archived = archived == 1
		if createdS != "" {
			if ts, err := time.Parse(time.RFC3339, createdS); err == nil {
				t.CreatedAt = ts
			}
		}
		t.Layout.Direction = dir
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trees: %w", err)
	}
	return out, nil
}

// readTreeBySlug loads .decisions/<slug>/tree.yaml from repoRoot.
func readTreeBySlug(repoRoot, slug string) (*core.Tree, error) {
	if repoRoot == "" {
		return nil, fmt.Errorf("tree %q: repo root not configured", slug)
	}
	path := filepath.Join(repoRoot, ".decisions", slug, storage.TreeMetaFileName)
	t, err := storage.ReadTree(path)
	if err != nil {
		return nil, fmt.Errorf("tree %q: %w", slug, err)
	}
	if t.Slug == "" {
		t.Slug = slug
	}
	return t, nil
}

// getDecisionInTree fetches a decision via the index and asserts it belongs
// to tree. Returns an error when the id is missing or the decision's tree
// does not match.
func getDecisionInTree(db *index.DB, tree, id string) (*core.Decision, error) {
	if db == nil {
		return nil, fmt.Errorf("decision %q: index not available", id)
	}
	d, err := index.GetDecision(db, id)
	if err != nil {
		return nil, fmt.Errorf("decision %q: %w", id, err)
	}
	if d == nil {
		return nil, fmt.Errorf("decision %q: not found", id)
	}
	if d.Tree != tree {
		return nil, fmt.Errorf("decision %q: belongs to tree %q, not %q", id, d.Tree, tree)
	}
	return d, nil
}

// loadActors reads .decisions/actors.yaml and returns the actor list. When
// the file does not exist, returns an empty slice (not an error) so the
// resource is still queryable in fresh / test repos.
func loadActors(repoRoot string) ([]core.Actor, error) {
	if repoRoot == "" {
		return []core.Actor{}, nil
	}
	path := filepath.Join(repoRoot, ".decisions", storage.ActorsFileName)
	af, err := storage.ReadActors(path)
	if err != nil {
		// Treat missing file as empty.
		if isFileNotExist(err) {
			return []core.Actor{}, nil
		}
		return nil, fmt.Errorf("read actors: %w", err)
	}
	if af.Actors == nil {
		return []core.Actor{}, nil
	}
	return af.Actors, nil
}

// stringArg pulls a string parameter from a templated resource read request.
// Returns "" when the key is missing or the value is not a string.
func stringArg(req mcpgo.ReadResourceRequest, key string) string {
	if req.Params.Arguments == nil {
		return ""
	}
	v, ok := req.Params.Arguments[key]
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// jsonContents builds a single-entry contents slice with mime type
// application/json. uri is echoed as the contents URI per MCP spec.
func jsonContents(uri string, body []byte) []mcpgo.ResourceContents {
	return []mcpgo.ResourceContents{
		mcpgo.TextResourceContents{
			URI:      uri,
			MIMEType: mimeJSON,
			Text:     string(body),
		},
	}
}

// isFileNotExist reports whether err wraps an os "file not found" error.
// We avoid pulling in errors.Is(err, fs.ErrNotExist) machinery just to
// keep this dependency-free; substring match on the wrapped error is
// adequate for our two known callers.
func isFileNotExist(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return containsSubstring(msg, "no such file or directory") ||
		containsSubstring(msg, "cannot find the file") // windows; future-proof
}
