package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/cgould/dtree/internal/config"
	"github.com/cgould/dtree/internal/identity"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/mcp"
)

func newMCPCommand() *cobra.Command {
	var (
		asFlag    string
		treeFlag  string
		readOnly  bool
		httpAddr  string
	)

	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run the dtree MCP server (stdio by default)",
		Long: "Run the dtree Model Context Protocol server. Default transport is " +
			"stdio (suitable for an MCP client subprocess). Pass --http to bind " +
			"an HTTP+SSE listener instead.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			if err := requireDecisionsDir(repoRoot); err != nil {
				return fmt.Errorf("%w; run `dtree init`", err)
			}

			cfg, err := config.Load(repoRoot)
			if err != nil {
				return fmt.Errorf("mcp: load config: %w", err)
			}

			resolver := identity.NewResolver(repoRoot, cfg)
			res, err := resolver.MustResolve(asFlag)
			if err != nil {
				return fmt.Errorf("mcp: resolve identity: %w", err)
			}

			db, err := index.Open(filepath.Join(repoRoot, ".decisions", ".index.db"))
			if err != nil {
				return fmt.Errorf("mcp: open index: %w", err)
			}
			defer db.Close()

			mcpCfg := mcp.Config{
				RepoRoot: repoRoot,
				DB:       db,
				Resolver: resolver,
				Actor:    res.Handle,
				Tree:     treeFlag,
				ReadOnly: readOnly,
			}
			if httpAddr != "" {
				mcpCfg.Transport = mcp.TransportHTTP
				mcpCfg.HTTPListen = httpAddr
			} else {
				mcpCfg.Transport = mcp.TransportStdio
			}

			srv, err := mcp.New(mcpCfg)
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			if httpAddr != "" {
				fmt.Fprintf(cmd.ErrOrStderr(), "dtree mcp listening on http://%s (actor=%s, read-only=%v)\n",
					httpAddr, res.Handle, readOnly)
			}
			return srv.Run(ctx)
		},
	}

	cmd.Flags().StringVar(&asFlag, "as", "", "Identity override (handle); falls back to config")
	cmd.Flags().StringVar(&treeFlag, "tree", "", "Scope the server to a single tree slug")
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "Disable mutation tools")
	cmd.Flags().StringVar(&httpAddr, "http", "", "Bind HTTP+SSE transport at the given addr (default: stdio)")
	return cmd
}
