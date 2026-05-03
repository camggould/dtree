package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/cgould/dtree/internal/config"
	"github.com/cgould/dtree/internal/identity"
	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/server"
)

// newUICommand returns the `dtree ui` command which starts the HTTP server
// and (unless --no-open) opens the UI in the default browser.
func newUICommand() *cobra.Command {
	var addr string
	var noOpen bool

	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Start the dtree server and open the UI",
		Long:  "Start the dtree HTTP API server and open the web UI in a browser. Defaults to localhost trust on the loopback interface.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")

			indexPath := filepath.Join(repoRoot, ".decisions", ".index.db")
			db, err := index.Open(indexPath)
			if err != nil {
				return fmt.Errorf("ui: open index: %w", err)
			}
			defer db.Close()

			cfg, err := config.Load(repoRoot)
			if err != nil {
				return fmt.Errorf("ui: load config: %w", err)
			}

			resolver := identity.NewResolver(repoRoot, cfg)

			srvCfg := server.Config{
				Listen:   addr,
				RepoRoot: repoRoot,
				DB:       db,
				Resolver: resolver,
				Trust:    server.TrustLocalhostOnly,
			}

			srv := server.New(srvCfg)

			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return fmt.Errorf("ui: listen %s: %w", addr, err)
			}

			host := ln.Addr().String()
			uiURL := fmt.Sprintf("http://%s/ui/", host)

			fmt.Fprintf(cmd.OutOrStdout(), "dtree UI at %s\n", uiURL)

			if !noOpen {
				if openErr := openBrowser(uiURL); openErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warn: could not open browser: %v\n", openErr)
				}
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			errCh := make(chan error, 1)
			go func() {
				if serveErr := srv.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
					errCh <- serveErr
				} else {
					errCh <- nil
				}
			}()

			select {
			case <-ctx.Done():
				fmt.Fprintln(cmd.OutOrStdout(), "\nShutting down...")
				// 5s grace; long-poll handlers (SSE) get an explicit
				// shutdown signal from server.New so they exit promptly.
				// The Close() fallback covers anything wedged.
				shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := srv.Shutdown(shutCtx); err != nil {
					_ = srv.Close()
				}
				return <-errCh
			case err := <-errCh:
				return err
			}
		},
	}

	cmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8080", "Address to listen on")
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "Do not open the browser automatically")
	return cmd
}

// openBrowser opens the given URL in the system default browser.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
	return cmd.Start()
}
