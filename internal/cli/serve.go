package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/cgould/dtree/internal/config"
	"github.com/cgould/dtree/internal/identity"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/server"
)

// newServeCommand returns the `dtree serve` command.
func newServeCommand() *cobra.Command {
	var addr string
	var readOnly bool
	var trustStr string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the dtree HTTP API server",
		Long:  "Start the dtree HTTP API server. Defaults to loopback (localhost trust). Use --trust token for remote access.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Parse trust setting.
			var trust server.Trust
			switch trustStr {
			case "localhost":
				trust = server.TrustLocalhostOnly
			case "token":
				trust = server.TrustToken
			case "":
				// Auto-detect based on addr.
				if isLoopbackAddr(addr) {
					trust = server.TrustLocalhostOnly
				} else {
					trust = server.TrustToken
				}
			default:
				return fmt.Errorf("serve: unknown --trust value %q; must be 'localhost' or 'token'", trustStr)
			}

			if err := validateTrustAddr(trust, addr); err != nil {
				return fmt.Errorf("serve: %w", err)
			}

			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")

			indexPath := filepath.Join(repoRoot, ".decisions", ".index.db")
			db, err := index.Open(indexPath)
			if err != nil {
				return fmt.Errorf("serve: open index: %w", err)
			}
			defer db.Close()

			cfg, err := config.Load(repoRoot)
			if err != nil {
				return fmt.Errorf("serve: load config: %w", err)
			}

			resolver := identity.NewResolver(repoRoot, cfg)

			srvCfg := server.Config{
				Listen:   addr,
				RepoRoot: repoRoot,
				DB:       db,
				Resolver: resolver,
				ReadOnly: readOnly,
				Trust:    trust,
			}

			srv := server.New(srvCfg)

			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return fmt.Errorf("serve: listen %s: %w", addr, err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "dtree serve listening on %s (trust=%s, read-only=%v)\n",
				ln.Addr(), trustStr, readOnly)

			// Trap SIGINT / SIGTERM for graceful shutdown.
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			errCh := make(chan error, 1)
			go func() {
				if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
					errCh <- err
				} else {
					errCh <- nil
				}
			}()

			select {
			case <-ctx.Done():
				fmt.Fprintln(cmd.OutOrStdout(), "\nShutting down...")
				_ = srv.Shutdown(context.Background())
				return <-errCh
			case err := <-errCh:
				return err
			}
		},
	}

	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8080", "Address to listen on")
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "Refuse mutation endpoints")
	cmd.Flags().StringVar(&trustStr, "trust", "", "Identity trust strategy: localhost or token (default: auto)")
	return cmd
}

// validateTrustAddr returns an error if localhost trust is requested on a
// non-loopback address, as that would allow unauthenticated identity spoofing.
func validateTrustAddr(trust server.Trust, addr string) error {
	if trust != server.TrustLocalhostOnly {
		return nil
	}
	if isLoopbackAddr(addr) {
		return nil
	}
	return fmt.Errorf("localhost trust is not allowed on non-loopback address %q; use --trust token or bind to 127.0.0.1", addr)
}

// isLoopbackAddr reports whether addr (host:port) resolves to a loopback
// interface. Returns false on any parse error.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "" || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
