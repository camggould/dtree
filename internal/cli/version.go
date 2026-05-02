package cli

import (
	"encoding/json"
	"fmt"
	"runtime/debug"

	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/index"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// These are populated at build time via -ldflags. Defaults make the binary
// runnable from `go build` without ldflags during development.
var (
	buildVersion = "dev"
	buildCommit  = ""
	buildDate    = ""
)

// versionInfo holds all version-related fields for output marshalling.
type versionInfo struct {
	Version string         `json:"version" yaml:"version"`
	Commit  string         `json:"commit"  yaml:"commit"`
	Built   string         `json:"built"   yaml:"built"`
	Schema  versionSchemas `json:"schema"  yaml:"schema"`
}

type versionSchemas struct {
	Core  int `json:"core"  yaml:"core"`
	Index int `json:"index" yaml:"index"`
}

func newVersionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
	}

	var outputFlag string
	cmd.Flags().StringVarP(&outputFlag, "output", "o", "human", "Output format: human, json, yaml")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		version, commit, date := resolveVersion()
		info := versionInfo{
			Version: version,
			Commit:  commit,
			Built:   date,
			Schema: versionSchemas{
				Core:  core.SchemaVersion,
				Index: index.CurrentSchemaVersion,
			},
		}

		// Honour global --output flag if the local flag was not explicitly set.
		if !cmd.Flags().Changed("output") {
			if global, err := cmd.Root().PersistentFlags().GetString("output"); err == nil && global != "" {
				outputFlag = global
			}
		}

		switch outputFlag {
		case "json":
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(info)
		case "yaml":
			return yaml.NewEncoder(cmd.OutOrStdout()).Encode(info)
		default: // human
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "dtree %s\n", info.Version)
			fmt.Fprintf(w, "  commit:           %s\n", info.Commit)
			fmt.Fprintf(w, "  built:            %s\n", info.Built)
			fmt.Fprintf(w, "  core schema:      v%d\n", info.Schema.Core)
			fmt.Fprintf(w, "  index schema:     v%d\n", info.Schema.Index)
		}
		return nil
	}

	return cmd
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
