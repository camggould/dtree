// Package mcp — MCP tool definitions mirroring the HTTP API.
//
// Each tool definition is a `mcpgo.NewTool(...)` call paired with a wire
// adapter that decodes args from the CallToolRequest, calls the matching
// handler in handlers.go, and serialises the result as a JSON text payload.
//
// The single fixed actor used for all mutations is taken from Server.cfg.Actor.
// Mutating tools refuse early when Server.cfg.ReadOnly is true.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// ---------------------------------------------------------------------------
// Registration
// ---------------------------------------------------------------------------

// registerAPITools adds every tool that mirrors a /v1 HTTP endpoint. It is
// invoked from registerTools (server.go) after the smoke-test list_trees tool.
func (s *Server) registerAPITools() {
	// --- Tree CRUD ---
	s.mcp.AddTool(
		mcpgo.NewTool("get_tree",
			mcpgo.WithDescription("Get a single tree by slug."),
			mcpgo.WithString("tree", mcpgo.Description("Tree slug."), mcpgo.Required()),
		),
		s.wrapHandler("get_tree", s.toolGetTree),
	)
	s.mcp.AddTool(
		mcpgo.NewTool("create_tree",
			mcpgo.WithDescription("Create a new tree."),
			mcpgo.WithString("slug", mcpgo.Description("Slug, lowercase, ^[a-z][a-z0-9-]{0,63}$"), mcpgo.Required()),
			mcpgo.WithString("name", mcpgo.Description("Human-readable name (Title)."), mcpgo.Required()),
			mcpgo.WithString("description", mcpgo.Description("Optional description.")),
		),
		s.wrapHandler("create_tree", s.toolCreateTree),
	)
	s.mcp.AddTool(
		mcpgo.NewTool("update_tree",
			mcpgo.WithDescription("Update name and/or description of a tree."),
			mcpgo.WithString("tree", mcpgo.Description("Tree slug."), mcpgo.Required()),
			mcpgo.WithString("name", mcpgo.Description("New name (omit to leave unchanged).")),
			mcpgo.WithString("description", mcpgo.Description("New description (omit to leave unchanged).")),
		),
		s.wrapHandler("update_tree", s.toolUpdateTree),
	)
	s.mcp.AddTool(
		mcpgo.NewTool("archive_tree",
			mcpgo.WithDescription("Archive or unarchive a tree."),
			mcpgo.WithString("tree", mcpgo.Description("Tree slug."), mcpgo.Required()),
			mcpgo.WithBoolean("archive", mcpgo.Description("true to archive, false to unarchive."), mcpgo.Required()),
		),
		s.wrapHandler("archive_tree", s.toolArchiveTree),
	)

	// --- Decision CRUD ---
	s.mcp.AddTool(
		mcpgo.NewTool("list_decisions",
			mcpgo.WithDescription("List decisions in a tree with optional filters."),
			mcpgo.WithString("tree", mcpgo.Required()),
			mcpgo.WithString("status"),
			mcpgo.WithString("priority"),
			mcpgo.WithString("tag"),
			mcpgo.WithString("search", mcpgo.Description("FTS5 MATCH query.")),
			mcpgo.WithNumber("limit"),
			mcpgo.WithString("cursor"),
		),
		s.wrapHandler("list_decisions", s.toolListDecisions),
	)
	s.mcp.AddTool(
		mcpgo.NewTool("get_decision",
			mcpgo.WithDescription("Get a single decision by id (full ULID or ≥4-char prefix)."),
			mcpgo.WithString("tree", mcpgo.Required()),
			mcpgo.WithString("id", mcpgo.Required()),
		),
		s.wrapHandler("get_decision", s.toolGetDecision),
	)
	s.mcp.AddTool(
		mcpgo.NewTool("create_decision",
			mcpgo.WithDescription("Create a new decision."),
			mcpgo.WithString("tree", mcpgo.Required()),
			mcpgo.WithString("summary", mcpgo.Required()),
			mcpgo.WithString("description"),
			mcpgo.WithString("priority", mcpgo.Description("assumption|low|medium|high|critical (default medium)")),
			mcpgo.WithArray("tags", mcpgo.WithStringItems()),
			mcpgo.WithString("assignee"),
		),
		s.wrapHandler("create_decision", s.toolCreateDecision),
	)
	s.mcp.AddTool(
		mcpgo.NewTool("update_decision",
			mcpgo.WithDescription("Patch fields on a decision (RFC 7396-style)."),
			mcpgo.WithString("tree", mcpgo.Required()),
			mcpgo.WithString("id", mcpgo.Required()),
			mcpgo.WithObject("fields", mcpgo.Description("Field patch object."), mcpgo.Required()),
			mcpgo.WithString("expected_rev", mcpgo.Description("Optimistic-concurrency token (optional).")),
		),
		s.wrapHandler("update_decision", s.toolUpdateDecision),
	)
	s.mcp.AddTool(
		mcpgo.NewTool("delete_decision",
			mcpgo.WithDescription("Delete a decision. hard=true requires force=true if there are incoming refs."),
			mcpgo.WithString("tree", mcpgo.Required()),
			mcpgo.WithString("id", mcpgo.Required()),
			mcpgo.WithBoolean("hard"),
			mcpgo.WithBoolean("force"),
		),
		s.wrapHandler("delete_decision", s.toolDeleteDecision),
	)

	// --- Lifecycle ---
	s.mcp.AddTool(
		mcpgo.NewTool("decide_decision",
			mcpgo.WithDescription("Mark a decision as decided."),
			mcpgo.WithString("tree", mcpgo.Required()),
			mcpgo.WithString("id", mcpgo.Required()),
			mcpgo.WithString("choice", mcpgo.Required()),
			mcpgo.WithString("reason"),
			mcpgo.WithArray("by", mcpgo.WithStringItems()),
			mcpgo.WithBoolean("is_recommended"),
		),
		s.wrapHandler("decide_decision", s.toolDecideDecision),
	)
	s.mcp.AddTool(
		mcpgo.NewTool("undecide_decision",
			mcpgo.WithDescription("Revert a decided decision back to proposed."),
			mcpgo.WithString("tree", mcpgo.Required()),
			mcpgo.WithString("id", mcpgo.Required()),
		),
		s.wrapHandler("undecide_decision", s.toolUndecideDecision),
	)
	s.mcp.AddTool(
		mcpgo.NewTool("scope_out_decision",
			mcpgo.WithDescription("Mark a decision as out_of_scope with a reason."),
			mcpgo.WithString("tree", mcpgo.Required()),
			mcpgo.WithString("id", mcpgo.Required()),
			mcpgo.WithString("reason", mcpgo.Required()),
		),
		s.wrapHandler("scope_out_decision", s.toolScopeOutDecision),
	)
	s.mcp.AddTool(
		mcpgo.NewTool("supersede_decision",
			mcpgo.WithDescription("Mark a decision as superseded by another."),
			mcpgo.WithString("tree", mcpgo.Required()),
			mcpgo.WithString("id", mcpgo.Required()),
			mcpgo.WithString("by", mcpgo.Description("ID of the superseder decision."), mcpgo.Required()),
		),
		s.wrapHandler("supersede_decision", s.toolSupersedeDecision),
	)
	s.mcp.AddTool(
		mcpgo.NewTool("restore_decision",
			mcpgo.WithDescription("Restore an out_of_scope decision back to proposed."),
			mcpgo.WithString("tree", mcpgo.Required()),
			mcpgo.WithString("id", mcpgo.Required()),
		),
		s.wrapHandler("restore_decision", s.toolRestoreDecision),
	)

	// --- Relationships ---
	s.mcp.AddTool(
		mcpgo.NewTool("relate_decisions",
			mcpgo.WithDescription("Add a relationship edge from source to target."),
			mcpgo.WithString("tree", mcpgo.Required()),
			mcpgo.WithString("source", mcpgo.Required()),
			mcpgo.WithString("type", mcpgo.Description("blocks|influences|relates_to"), mcpgo.Required()),
			mcpgo.WithString("target", mcpgo.Required()),
			mcpgo.WithString("note"),
		),
		s.wrapHandler("relate_decisions", s.toolRelateDecisions),
	)
	s.mcp.AddTool(
		mcpgo.NewTool("unrelate_decisions",
			mcpgo.WithDescription("Remove a relationship edge from source to target."),
			mcpgo.WithString("tree", mcpgo.Required()),
			mcpgo.WithString("source", mcpgo.Required()),
			mcpgo.WithString("type", mcpgo.Required()),
			mcpgo.WithString("target", mcpgo.Required()),
		),
		s.wrapHandler("unrelate_decisions", s.toolUnrelateDecisions),
	)

	// --- History / Find ---
	s.mcp.AddTool(
		mcpgo.NewTool("decision_history",
			mcpgo.WithDescription("Return audit events for one decision."),
			mcpgo.WithString("tree", mcpgo.Required()),
			mcpgo.WithString("id", mcpgo.Required()),
			mcpgo.WithString("since", mcpgo.Description("Relative duration (7d, 24h, 30m, 10s) or RFC3339.")),
		),
		s.wrapHandler("decision_history", s.toolDecisionHistory),
	)
	s.mcp.AddTool(
		mcpgo.NewTool("find_decisions",
			mcpgo.WithDescription("Search decisions across all (or one) tree using FTS5."),
			mcpgo.WithString("query", mcpgo.Required()),
			mcpgo.WithString("tree"),
			mcpgo.WithNumber("limit"),
		),
		s.wrapHandler("find_decisions", s.toolFindDecisions),
	)
}

// ---------------------------------------------------------------------------
// Argument helpers
// ---------------------------------------------------------------------------

// argString fetches a string argument; returns "" when missing or wrong type.
func argString(req mcpgo.CallToolRequest, key string) string {
	args := req.GetArguments()
	if args == nil {
		return ""
	}
	if v, ok := args[key].(string); ok {
		return v
	}
	return ""
}

// argStringPtr returns &v when the key is present (even if empty string),
// or nil when absent. Callers use the pointer to distinguish "no change"
// from "set to empty".
func argStringPtr(req mcpgo.CallToolRequest, key string) *string {
	args := req.GetArguments()
	if args == nil {
		return nil
	}
	raw, ok := args[key]
	if !ok {
		return nil
	}
	if s, ok := raw.(string); ok {
		return &s
	}
	return nil
}

// argBool fetches a bool argument; returns false when missing or wrong type.
func argBool(req mcpgo.CallToolRequest, key string) bool {
	args := req.GetArguments()
	if args == nil {
		return false
	}
	if v, ok := args[key].(bool); ok {
		return v
	}
	return false
}

// argInt fetches an integer argument from a number-typed input.
func argInt(req mcpgo.CallToolRequest, key string) int {
	args := req.GetArguments()
	if args == nil {
		return 0
	}
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return 0
}

// argStringSlice unpacks an array of strings; returns nil when missing.
func argStringSlice(req mcpgo.CallToolRequest, key string) []string {
	args := req.GetArguments()
	if args == nil {
		return nil
	}
	raw, ok := args[key]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// argMap returns the named object as a map, or nil if absent.
func argMap(req mcpgo.CallToolRequest, key string) map[string]any {
	args := req.GetArguments()
	if args == nil {
		return nil
	}
	if m, ok := args[key].(map[string]any); ok {
		return m
	}
	return nil
}

// jsonResult marshals v and wraps it in a TextContent CallToolResult.
func jsonResult(v any) (*mcpgo.CallToolResult, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return mcpgo.NewToolResultError("marshal: " + err.Error()), nil
	}
	return mcpgo.NewToolResultText(string(data)), nil
}

// errResult returns an MCP error result with the formatted message.
func errResult(format string, args ...any) (*mcpgo.CallToolResult, error) {
	return mcpgo.NewToolResultError(fmt.Sprintf(format, args...)), nil
}

// readOnly returns a ready-to-return error result when the server is in
// read-only mode and the caller is invoking a mutating tool.
func (s *Server) readOnly() (*mcpgo.CallToolResult, error) {
	return errResult("server is in read-only mode")
}

// ---------------------------------------------------------------------------
// Tree CRUD adapters
// ---------------------------------------------------------------------------

func (s *Server) toolGetTree(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	t, err := handleGetTree(s.cfg.DB, argString(req, "tree"))
	if err != nil {
		return errResult("%v", err)
	}
	return jsonResult(t)
}

func (s *Server) toolCreateTree(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	if s.cfg.ReadOnly {
		return s.readOnly()
	}
	t, err := handleCreateTree(s.cfg.RepoRoot, s.cfg.DB, s.cfg.Actor,
		argString(req, "slug"), argString(req, "name"), argString(req, "description"))
	if err != nil {
		return errResult("%v", err)
	}
	return jsonResult(t)
}

func (s *Server) toolUpdateTree(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	if s.cfg.ReadOnly {
		return s.readOnly()
	}
	t, err := handleUpdateTree(s.cfg.RepoRoot, s.cfg.DB, s.cfg.Actor,
		argString(req, "tree"),
		argStringPtr(req, "name"),
		argStringPtr(req, "description"),
	)
	if err != nil {
		return errResult("%v", err)
	}
	return jsonResult(t)
}

func (s *Server) toolArchiveTree(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	if s.cfg.ReadOnly {
		return s.readOnly()
	}
	t, err := handleArchiveTree(s.cfg.RepoRoot, s.cfg.DB, s.cfg.Actor,
		argString(req, "tree"), argBool(req, "archive"))
	if err != nil {
		return errResult("%v", err)
	}
	return jsonResult(t)
}

// ---------------------------------------------------------------------------
// Decision CRUD adapters
// ---------------------------------------------------------------------------

func (s *Server) toolListDecisions(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	res, err := handleListDecisions(s.cfg.DB, listDecisionsArgs{
		Tree:     argString(req, "tree"),
		Status:   argString(req, "status"),
		Priority: argString(req, "priority"),
		Tag:      argString(req, "tag"),
		Search:   argString(req, "search"),
		Limit:    argInt(req, "limit"),
		Cursor:   argString(req, "cursor"),
	})
	if err != nil {
		return errResult("%v", err)
	}
	return jsonResult(res)
}

func (s *Server) toolGetDecision(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	d, err := handleGetDecision(s.cfg.DB, argString(req, "tree"), argString(req, "id"))
	if err != nil {
		return errResult("%v", err)
	}
	return jsonResult(d)
}

func (s *Server) toolCreateDecision(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	if s.cfg.ReadOnly {
		return s.readOnly()
	}
	d, err := handleCreateDecision(s.cfg.RepoRoot, s.cfg.DB, s.cfg.Actor,
		argString(req, "tree"),
		createDecisionArgs{
			Summary:     argString(req, "summary"),
			Description: argString(req, "description"),
			Priority:    argString(req, "priority"),
			Tags:        argStringSlice(req, "tags"),
			Assignee:    argString(req, "assignee"),
		},
	)
	if err != nil {
		return errResult("%v", err)
	}
	return jsonResult(d)
}

func (s *Server) toolUpdateDecision(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	if s.cfg.ReadOnly {
		return s.readOnly()
	}
	fields := decodeUpdateFields(argMap(req, "fields"))
	d, err := handleUpdateDecision(s.cfg.RepoRoot, s.cfg.DB, s.cfg.Actor,
		argString(req, "tree"), argString(req, "id"),
		fields, argString(req, "expected_rev"),
	)
	if err != nil {
		return errResult("%v", err)
	}
	return jsonResult(d)
}

// decodeUpdateFields converts the loosely-typed fields map (per the MCP
// schema) into the typed envelope used by handleUpdateDecision.
func decodeUpdateFields(m map[string]any) updateDecisionFields {
	var f updateDecisionFields
	if m == nil {
		return f
	}
	pickStr := func(key string) *string {
		raw, ok := m[key]
		if !ok {
			return nil
		}
		if s, ok := raw.(string); ok {
			return &s
		}
		return nil
	}
	f.Summary = pickStr("summary")
	f.Description = pickStr("description")
	f.Priority = pickStr("priority")
	f.Assignee = pickStr("assignee")
	f.RecommendedSummary = pickStr("recommended_summary")
	f.RecommendedFull = pickStr("recommended_full")
	f.RecommendedBy = pickStr("recommended_by")
	if raw, ok := m["tags"]; ok {
		f.TagsSet = true
		switch v := raw.(type) {
		case []string:
			f.Tags = v
		case []any:
			out := make([]string, 0, len(v))
			for _, it := range v {
				if s, ok := it.(string); ok {
					out = append(out, s)
				}
			}
			f.Tags = out
		}
	}
	return f
}

func (s *Server) toolDeleteDecision(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	if s.cfg.ReadOnly {
		return s.readOnly()
	}
	if err := handleDeleteDecision(s.cfg.RepoRoot, s.cfg.DB, s.cfg.Actor,
		argString(req, "tree"), argString(req, "id"),
		argBool(req, "hard"), argBool(req, "force"),
	); err != nil {
		return errResult("%v", err)
	}
	return jsonResult(map[string]any{"deleted": true})
}

// ---------------------------------------------------------------------------
// Lifecycle adapters
// ---------------------------------------------------------------------------

func (s *Server) toolDecideDecision(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	if s.cfg.ReadOnly {
		return s.readOnly()
	}
	d, err := handleDecideDecision(s.cfg.RepoRoot, s.cfg.DB, s.cfg.Actor,
		argString(req, "tree"), argString(req, "id"),
		argString(req, "choice"), argString(req, "reason"),
		argStringSlice(req, "by"), argBool(req, "is_recommended"),
	)
	if err != nil {
		return errResult("%v", err)
	}
	return jsonResult(d)
}

func (s *Server) toolUndecideDecision(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	if s.cfg.ReadOnly {
		return s.readOnly()
	}
	d, err := handleUndecideDecision(s.cfg.RepoRoot, s.cfg.DB, s.cfg.Actor,
		argString(req, "tree"), argString(req, "id"),
	)
	if err != nil {
		return errResult("%v", err)
	}
	return jsonResult(d)
}

func (s *Server) toolScopeOutDecision(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	if s.cfg.ReadOnly {
		return s.readOnly()
	}
	d, err := handleScopeOutDecision(s.cfg.RepoRoot, s.cfg.DB, s.cfg.Actor,
		argString(req, "tree"), argString(req, "id"), argString(req, "reason"),
	)
	if err != nil {
		return errResult("%v", err)
	}
	return jsonResult(d)
}

func (s *Server) toolSupersedeDecision(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	if s.cfg.ReadOnly {
		return s.readOnly()
	}
	d, err := handleSupersedeDecision(s.cfg.RepoRoot, s.cfg.DB, s.cfg.Actor,
		argString(req, "tree"), argString(req, "id"), argString(req, "by"),
	)
	if err != nil {
		return errResult("%v", err)
	}
	return jsonResult(d)
}

func (s *Server) toolRestoreDecision(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	if s.cfg.ReadOnly {
		return s.readOnly()
	}
	d, err := handleRestoreDecision(s.cfg.RepoRoot, s.cfg.DB, s.cfg.Actor,
		argString(req, "tree"), argString(req, "id"),
	)
	if err != nil {
		return errResult("%v", err)
	}
	return jsonResult(d)
}

// ---------------------------------------------------------------------------
// Relationship adapters
// ---------------------------------------------------------------------------

func (s *Server) toolRelateDecisions(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	if s.cfg.ReadOnly {
		return s.readOnly()
	}
	d, err := handleRelateDecisions(s.cfg.RepoRoot, s.cfg.DB, s.cfg.Actor,
		argString(req, "tree"),
		argString(req, "source"),
		argString(req, "type"),
		argString(req, "target"),
		argString(req, "note"),
	)
	if err != nil {
		return errResult("%v", err)
	}
	return jsonResult(d)
}

func (s *Server) toolUnrelateDecisions(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	if s.cfg.ReadOnly {
		return s.readOnly()
	}
	d, err := handleUnrelateDecisions(s.cfg.RepoRoot, s.cfg.DB, s.cfg.Actor,
		argString(req, "tree"),
		argString(req, "source"),
		argString(req, "type"),
		argString(req, "target"),
	)
	if err != nil {
		return errResult("%v", err)
	}
	return jsonResult(d)
}

// ---------------------------------------------------------------------------
// History / Find
// ---------------------------------------------------------------------------

func (s *Server) toolDecisionHistory(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	events, err := handleDecisionHistory(s.cfg.RepoRoot, s.cfg.DB,
		argString(req, "tree"), argString(req, "id"), argString(req, "since"),
	)
	if err != nil {
		return errResult("%v", err)
	}
	return jsonResult(map[string]any{"events": events})
}

func (s *Server) toolFindDecisions(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	hits, err := handleFindDecisions(s.cfg.DB,
		argString(req, "query"), argString(req, "tree"), argInt(req, "limit"),
	)
	if err != nil {
		return errResult("%v", err)
	}
	return jsonResult(map[string]any{"items": hits})
}
