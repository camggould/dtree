package cli

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// These are populated at build time via -ldflags. Defaults make the binary
// runnable from `go build` without ldflags during development.
var (
	buildVersion = "dev"
	buildCommit  = ""
	buildDate    = ""
)

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			version, commit, date := resolveVersion()
			fmt.Fprintf(cmd.OutOrStdout(), "dtree %s\n", version)
			if commit != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  commit: %s\n", commit)
			}
			if date != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "  built:  %s\n", date)
			}
			return nil
		},
	}
}

// resolveVersion returns (version, commit, date), preferring ldflag-injected
// values, then falling back to debug.ReadBuildInfo for `go install` usage.
func resolveVersion() (string, string, string) {
	version, commit, date := buildVersion, buildCommit, buildDate
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version, commit, date
	}
	if version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			if commit == "" {
				commit = s.Value
			}
		case "vcs.time":
			if date == "" {
				date = s.Value
			}
		}
	}
	return version, commit, date
}
