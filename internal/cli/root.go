// Package cli wires the cobra command tree for dtree.
package cli

import (
	"os"

	"github.com/spf13/cobra"
)

// NewRootCommand constructs the top-level dtree command. All subcommands
// hang off this root. Returns a fresh tree on each call so tests can
// instantiate the CLI in isolation.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "dtree",
		Short:         "Build, record, and audit decisions",
		Long:          "dtree is a directory-based persistence layer for building, recording, and auditing decisions.",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	// --repo-root defaults to the current working directory. Tests and headless
	// contexts can override this to point at a synthetic repo tree.
	cwd, _ := os.Getwd()
	root.PersistentFlags().String("repo-root", cwd, "Path to the repo root (default: current directory)")

	// --output sets the global output format. Empty string means auto-detect.
	root.PersistentFlags().String("output", "", "Output format: human, json, yaml (default: auto-detect)")

	root.AddCommand(newVersionCommand())
	root.AddCommand(newConfigCommand())
	root.AddCommand(newInitCommand())
	root.AddCommand(newWhoamiCommand())
	root.AddCommand(newAsCommand())
	root.AddCommand(newActorCommand())
	root.AddCommand(newAuditCommand())

	return root
}
