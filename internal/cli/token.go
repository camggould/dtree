package cli

import (
	"fmt"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/cgould/dtree/internal/index"
)

// newTokenCommand returns the `dtree token` parent command.
func newTokenCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Manage bearer authentication tokens",
		Long:  "Create, list, and revoke bearer tokens used for dtree serve authentication.",
	}
	cmd.AddCommand(newTokenCreateCommand())
	cmd.AddCommand(newTokenListCommand())
	cmd.AddCommand(newTokenRevokeCommand())
	return cmd
}

func newTokenCreateCommand() *cobra.Command {
	var handle, label, ttlStr string

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new bearer token",
		Long:  "Create a new bearer token for the given actor handle. The plaintext is printed once and cannot be retrieved again.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if handle == "" {
				return fmt.Errorf("token create: --as is required")
			}

			var ttl time.Duration
			if ttlStr != "" {
				var err error
				ttl, err = time.ParseDuration(ttlStr)
				if err != nil {
					return fmt.Errorf("token create: invalid --ttl %q: %w", ttlStr, err)
				}
			}

			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			indexPath := filepath.Join(repoRoot, ".decisions", ".index.db")
			db, err := index.Open(indexPath)
			if err != nil {
				return fmt.Errorf("token create: open index: %w", err)
			}
			defer db.Close()

			plaintext, err := index.CreateToken(db, handle, label, ttl)
			if err != nil {
				return fmt.Errorf("token create: %w", err)
			}

			fmt.Fprintln(cmd.OutOrStdout(), plaintext)
			return nil
		},
	}

	cmd.Flags().StringVar(&handle, "as", "", "Actor handle to associate the token with (required)")
	cmd.Flags().StringVar(&label, "label", "", "Optional human-readable label for the token")
	cmd.Flags().StringVar(&ttlStr, "ttl", "", "Token lifetime, e.g. 24h, 30d (default: no expiry)")
	return cmd
}

func newTokenListCommand() *cobra.Command {
	var handle string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tokens",
		Long:  "List bearer tokens. Use --handle to filter by actor.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			indexPath := filepath.Join(repoRoot, ".decisions", ".index.db")
			db, err := index.Open(indexPath)
			if err != nil {
				return fmt.Errorf("token list: open index: %w", err)
			}
			defer db.Close()

			tokens, err := index.ListTokens(db, handle)
			if err != nil {
				return fmt.Errorf("token list: %w", err)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "HASH-PREFIX\tHANDLE\tCREATED_AT\tEXPIRES_AT\tREVOKED\tLABEL")
			for _, tok := range tokens {
				prefix := tok.Hash
				if len(prefix) > 12 {
					prefix = prefix[:12]
				}
				expiresStr := ""
				if tok.ExpiresAt != nil {
					expiresStr = tok.ExpiresAt.Format(time.RFC3339)
				}
				revokedStr := "no"
				if tok.Revoked {
					revokedStr = "yes"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					prefix,
					tok.Handle,
					tok.CreatedAt.Format(time.RFC3339),
					expiresStr,
					revokedStr,
					tok.Label,
				)
			}
			return w.Flush()
		},
	}

	cmd.Flags().StringVar(&handle, "handle", "", "Filter tokens by actor handle")
	return cmd
}

func newTokenRevokeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke <hash-prefix>",
		Short: "Revoke a token by hash prefix",
		Long:  "Revoke a token by its unambiguous hash prefix (at least a few characters).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			prefix := args[0]

			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			indexPath := filepath.Join(repoRoot, ".decisions", ".index.db")
			db, err := index.Open(indexPath)
			if err != nil {
				return fmt.Errorf("token revoke: open index: %w", err)
			}
			defer db.Close()

			full, err := index.RevokeTokenByHashPrefix(db, prefix)
			if err != nil {
				return fmt.Errorf("token revoke: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Revoked token %s\n", full[:12])
			return nil
		},
	}
	return cmd
}
