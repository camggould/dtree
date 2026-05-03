// Package mcp implements the Model Context Protocol server for dtree.
//
// The server exposes decision-tree data to AI agents via MCP tools. It
// supports two transports:
//   - stdio (default): JSON-RPC over stdin/stdout, suitable for MCP clients
//     that spawn the server as a subprocess.
//   - HTTP+SSE: Server-Sent Events transport on a configurable listen address,
//     suitable for persistent server deployments.
//
// A single fixed actor (cfg.Actor) performs all mutations. The actor is
// validated against actors.yaml at startup.
package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/cgould/dtree/internal/identity"
	"github.com/cgould/dtree/internal/index"
)

// Transport selects the MCP transport mechanism.
type Transport int

const (
	// TransportStdio uses JSON-RPC over stdin/stdout (default).
	TransportStdio Transport = iota
	// TransportHTTP uses HTTP+SSE transport.
	TransportHTTP
)

// Config holds construction parameters for the MCP server.
type Config struct {
	// RepoRoot is the path to the repository root.
	RepoRoot string

	// DB is the open SQLite index. May be nil if no index is available.
	DB *index.DB

	// Resolver is the identity resolver for actor lookups.
	Resolver *identity.Resolver

	// Actor is the handle to act as. Validated against actors.yaml at startup.
	Actor string

	// Tree optionally scopes the server to a single tree slug.
	Tree string

	// ReadOnly prevents mutating tools from executing when true.
	ReadOnly bool

	// Transport selects the transport mechanism.
	Transport Transport

	// HTTPListen is the listen address for HTTP+SSE transport, e.g. ":7474".
	HTTPListen string

	// Logger receives tool-call log lines. Defaults to os.Stderr when nil.
	// Pass io.Discard to suppress all logging.
	Logger io.Writer
}

// Server is the dtree MCP server.
type Server struct {
	cfg Config
	mcp *mcpserver.MCPServer
	log *log.Logger
}

// New constructs and validates a Server. Returns an error when cfg.Actor is
// not registered in the project's actors.yaml.
func New(cfg Config) (*Server, error) {
	// Validate actor exists in actors.yaml.
	if cfg.Resolver != nil {
		actor, err := cfg.Resolver.FindActor(cfg.Actor)
		if err != nil {
			return nil, fmt.Errorf("mcp: resolve actor %q: %w", cfg.Actor, err)
		}
		if actor == nil {
			return nil, fmt.Errorf("mcp: actor %q is not registered in this project; run `dtree actor add %s`", cfg.Actor, cfg.Actor)
		}
	}

	// Set up logger.
	w := cfg.Logger
	if w == nil {
		w = os.Stderr
	}
	lg := log.New(w, "", 0)

	mcpSrv := mcpserver.NewMCPServer("dtree", "1.0.0")

	s := &Server{
		cfg: cfg,
		mcp: mcpSrv,
		log: lg,
	}

	s.registerTools()

	return s, nil
}

// registerTools adds all MCP tools to the server. Currently only list_trees
// is registered as a smoke-test proof of wiring. Future tools are added here.
func (s *Server) registerTools() {
	listTreesTool := mcpgo.NewTool("list_trees",
		mcpgo.WithDescription("List all decision tree slugs in this project."),
	)
	s.mcp.AddTool(listTreesTool, s.wrapHandler("list_trees", s.handleListTrees))
}

// Run starts the configured transport and blocks until ctx is cancelled or the
// transport stops.
func (s *Server) Run(ctx context.Context) error {
	switch s.cfg.Transport {
	case TransportStdio:
		stdio := mcpserver.NewStdioServer(s.mcp)
		return stdio.Listen(ctx, os.Stdin, os.Stdout)

	case TransportHTTP:
		addr := s.cfg.HTTPListen
		if addr == "" {
			addr = ":7474"
		}
		sse := mcpserver.NewSSEServer(s.mcp,
			mcpserver.WithBaseURL("http://"+addr),
		)
		// Run the server in a goroutine; shut down when ctx is cancelled.
		errCh := make(chan error, 1)
		go func() {
			errCh <- sse.Start(addr)
		}()
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = sse.Shutdown(shutdownCtx)
			return ctx.Err()
		case err := <-errCh:
			return err
		}

	default:
		return fmt.Errorf("mcp: unknown transport %d", s.cfg.Transport)
	}
}

// wrapHandler wraps a tool handler to emit a log line for every call:
//
//	<timestamp> tool=<name> actor=<handle> status=<ok|err> elapsed=<ms>ms
func (s *Server) wrapHandler(name string, h mcpserver.ToolHandlerFunc) mcpserver.ToolHandlerFunc {
	return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
		start := time.Now()
		result, err := h(ctx, req)
		elapsed := time.Since(start).Milliseconds()

		status := "ok"
		if err != nil {
			status = "err"
		} else if result != nil && result.IsError {
			status = "err"
		}

		ts := time.Now().UTC().Format(time.RFC3339)
		s.log.Printf("%s tool=%s actor=%s status=%s elapsed=%dms",
			ts, name, s.cfg.Actor, status, elapsed)

		return result, err
	}
}

// handleListTrees returns the list of tree slugs from the index as a JSON
// array. When the index is unavailable it returns an empty list.
func (s *Server) handleListTrees(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	slugs, err := s.listTreeSlugs()
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("list_trees: %v", err)), nil
	}

	data, err := json.Marshal(slugs)
	if err != nil {
		return mcpgo.NewToolResultError(fmt.Sprintf("list_trees: marshal: %v", err)), nil
	}

	return mcpgo.NewToolResultText(string(data)), nil
}

// listTreeSlugs queries the index for all (non-archived) tree slugs. Returns
// an empty slice when the DB is nil or the trees table is empty.
func (s *Server) listTreeSlugs() ([]string, error) {
	if s.cfg.DB == nil {
		return []string{}, nil
	}

	query := `SELECT slug FROM trees WHERE archived = 0 ORDER BY slug`
	rows, err := s.cfg.DB.Conn().QueryContext(context.Background(), query)
	if err != nil {
		// trees table might not be populated yet — return empty gracefully.
		if isNoSuchTable(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("query trees: %w", err)
	}
	defer rows.Close()

	var slugs []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, fmt.Errorf("scan tree slug: %w", err)
		}
		slugs = append(slugs, slug)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate trees: %w", err)
	}

	if slugs == nil {
		slugs = []string{}
	}
	return slugs, nil
}

// isNoSuchTable reports whether err is a SQLite "no such table" error.
func isNoSuchTable(err error) bool {
	if err == nil {
		return false
	}
	// mattn/go-sqlite3 wraps errors; checking the message is reliable.
	return sql.ErrNoRows == err || containsNoSuchTable(err.Error())
}

func containsNoSuchTable(msg string) bool {
	const needle = "no such table"
	return len(msg) >= len(needle) && containsSubstring(msg, needle)
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Exported accessors used by tests (and future callers).
// ---------------------------------------------------------------------------

// IsReadOnly reports whether the server was constructed with ReadOnly=true.
func (s *Server) IsReadOnly() bool { return s.cfg.ReadOnly }

// InvokeListTrees calls the list_trees tool handler through the logging
// wrapper, bypassing transport. Useful for unit-testing tool behaviour without
// stdio/HTTP setup.
func (s *Server) InvokeListTrees(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
	return s.wrapHandler("list_trees", s.handleListTrees)(ctx, req)
}

// NewTestSSEServer spins up an httptest.Server wrapping the internal MCPServer
// using the mcp-go SSE transport. The caller is responsible for closing it.
func (s *Server) NewTestSSEServer() *httptest.Server {
	return mcpserver.NewTestServer(s.mcp)
}
