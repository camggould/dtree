// Package cli wires the cobra command tree for dtree.
package cli

import (
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

	root.AddCommand(newVersionCommand())

	return root
}
