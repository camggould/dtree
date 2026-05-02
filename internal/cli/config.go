package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"

	"github.com/cgould/dtree/internal/config"
	"github.com/cgould/dtree/internal/core"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// newConfigCommand returns the `dtree config` command group.
func newConfigCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Read and write dtree configuration",
		Long:  "Manage dtree configuration across global (~/.config/dtree/config.yaml) and local (.decisions/config.yaml) scopes.",
	}

	cmd.AddCommand(newConfigGetCommand())
	cmd.AddCommand(newConfigSetCommand())
	cmd.AddCommand(newConfigUnsetCommand())
	cmd.AddCommand(newConfigListCommand())
	cmd.AddCommand(newConfigEditCommand())

	return cmd
}

// newConfigGetCommand returns the `dtree config get [key]` subcommand.
func newConfigGetCommand() *cobra.Command {
	var noSource bool

	cmd := &cobra.Command{
		Use:   "get [key]",
		Short: "Print resolved config value(s)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")

			r, err := config.Load(repoRoot)
			if err != nil {
				return err
			}

			if len(args) == 0 {
				// No key — behave like list.
				return runConfigList(cmd, r, repoRoot, noSource, "human")
			}

			key := args[0]
			value, source, ok := r.Get(key)
			if !ok {
				fmt.Fprintf(cmd.ErrOrStderr(), "error: unknown config key %q\n", key)
				return fmt.Errorf("unknown config key %q", key)
			}

			if noSource {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n", value)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "%s (from %s)\n", value, source)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&noSource, "no-source", false, "Suppress (from <source>) annotation")
	return cmd
}

// newConfigSetCommand returns the `dtree config set --local|--global <key> <value>` subcommand.
func newConfigSetCommand() *cobra.Command {
	var localScope bool
	var globalScope bool

	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value in the specified scope",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			key := args[0]
			value := args[1]

			scope, err := resolveScope(localScope, globalScope)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "error:", err)
				return err
			}

			// For --local, check that .decisions/ directory exists.
			if scope == config.SourceLocal {
				decisionsDir := repoRoot + "/.decisions"
				if _, statErr := os.Stat(decisionsDir); os.IsNotExist(statErr) {
					msg := "no .decisions/ found; run `dtree init`"
					fmt.Fprintln(cmd.ErrOrStderr(), "error:", msg)
					return fmt.Errorf("%s", msg)
				}
			}

			if err := config.Set(scope, repoRoot, key, value); err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "error:", err)
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Set %s = %s in %s\n", key, value, scope)
			return nil
		},
	}

	cmd.Flags().BoolVar(&localScope, "local", false, "Write to local config (.decisions/config.yaml)")
	cmd.Flags().BoolVar(&globalScope, "global", false, "Write to global config (~/.config/dtree/config.yaml)")
	return cmd
}

// newConfigUnsetCommand returns the `dtree config unset --local|--global <key>` subcommand.
func newConfigUnsetCommand() *cobra.Command {
	var localScope bool
	var globalScope bool

	cmd := &cobra.Command{
		Use:   "unset <key>",
		Short: "Remove a config key from the specified scope",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			key := args[0]

			scope, err := resolveScope(localScope, globalScope)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "error:", err)
				return err
			}

			if err := config.Unset(scope, repoRoot, key); err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "error:", err)
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Unset %s in %s\n", key, scope)
			return nil
		},
	}

	cmd.Flags().BoolVar(&localScope, "local", false, "Remove from local config (.decisions/config.yaml)")
	cmd.Flags().BoolVar(&globalScope, "global", false, "Remove from global config (~/.config/dtree/config.yaml)")
	return cmd
}

// newConfigListCommand returns the `dtree config list` subcommand.
func newConfigListCommand() *cobra.Command {
	var outputFormat string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all resolved config values",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")

			r, err := config.Load(repoRoot)
			if err != nil {
				return err
			}

			format := outputFormat
			if format == "" {
				if isTTY() {
					format = "human"
				} else {
					format = "json"
				}
			}

			return runConfigList(cmd, r, repoRoot, false, format)
		},
	}

	cmd.Flags().StringVar(&outputFormat, "output", "", "Output format: human, json, yaml (default: auto-detect)")
	return cmd
}

// newConfigEditCommand returns the `dtree config edit --local|--global` subcommand.
func newConfigEditCommand() *cobra.Command {
	var localScope bool
	var globalScope bool

	cmd := &cobra.Command{
		Use:   "edit",
		Short: "Open config file in $EDITOR",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")

			scope, err := resolveScope(localScope, globalScope)
			if err != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "error:", err)
				return err
			}

			var path string
			switch scope {
			case config.SourceGlobal:
				path, err = config.GlobalPath()
				if err != nil {
					return err
				}
			case config.SourceLocal:
				path = config.LocalPath(repoRoot)
			}

			// Create the file with empty schema_version=1 if it doesn't exist.
			if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
				f := &config.File{SchemaVersion: core.SchemaVersion}
				if writeErr := config.WriteFile(path, f); writeErr != nil {
					return fmt.Errorf("creating config file: %w", writeErr)
				}
			}

			// Resolve editor.
			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = "vi"
			}

			// Spawn the editor inheriting stdin/stdout/stderr.
			editorCmd := exec.Command(editor, path) //nolint:gosec
			editorCmd.Stdin = os.Stdin
			editorCmd.Stdout = os.Stdout
			editorCmd.Stderr = os.Stderr

			if runErr := editorCmd.Run(); runErr != nil {
				fmt.Fprintln(cmd.ErrOrStderr(), "Editor exited with non-zero status")
				return runErr
			}

			// Re-validate the file after the editor exits.
			if _, readErr := config.ReadFile(path); readErr != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "error: %v\n", readErr)
				fmt.Fprintln(cmd.ErrOrStderr(), "Hint: run `dtree config edit` again to fix")
				return readErr
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&localScope, "local", false, "Edit local config (.decisions/config.yaml)")
	cmd.Flags().BoolVar(&globalScope, "global", false, "Edit global config (~/.config/dtree/config.yaml)")
	return cmd
}

// resolveScope returns SourceLocal or SourceGlobal from the flag values,
// returning an error if neither or both are set.
func resolveScope(localScope, globalScope bool) (config.Source, error) {
	if localScope && globalScope {
		return "", fmt.Errorf("cannot set both --local and --global; choose one")
	}
	if !localScope && !globalScope {
		return "", fmt.Errorf("must specify --local or --global")
	}
	if localScope {
		return config.SourceLocal, nil
	}
	return config.SourceGlobal, nil
}

// configEntry holds a key, its resolved value, and source for display.
type configEntry struct {
	Key    string
	Value  string
	Source config.Source
}

// orderedConfigKeys returns the supported keys in stable order.
func orderedConfigKeys() []string {
	return []string{
		"color",
		"default_tree",
		"editor",
		"identity.default",
		"output",
	}
}

// loadConfigEntries builds the ordered list of config entries for display.
func loadConfigEntries(r *config.Resolved) []configEntry {
	keys := orderedConfigKeys()
	entries := make([]configEntry, 0, len(keys))
	for _, k := range keys {
		v, src, _ := r.Get(k)
		entries = append(entries, configEntry{Key: k, Value: v, Source: src})
	}
	return entries
}

// runConfigList formats and writes config to cmd's stdout.
func runConfigList(cmd *cobra.Command, r *config.Resolved, _ string, noSource bool, format string) error {
	entries := loadConfigEntries(r)

	switch format {
	case "json":
		return runConfigListJSON(cmd, entries)
	case "yaml":
		return runConfigListYAML(cmd, entries)
	default:
		return runConfigListHuman(cmd, entries, noSource)
	}
}

func runConfigListHuman(cmd *cobra.Command, entries []configEntry, noSource bool) error {
	// Sort by key for stable output (already sorted, but be defensive).
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Key < entries[j].Key
	})

	for _, e := range entries {
		if noSource {
			fmt.Fprintf(cmd.OutOrStdout(), "%s = %s\n", e.Key, e.Value)
		} else {
			// Suppress "(from default)" suffix when value is empty.
			if e.Source == config.SourceDefault && e.Value == "" {
				fmt.Fprintf(cmd.OutOrStdout(), "%s = %s\n", e.Key, e.Value)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "%s = %s (from %s)\n", e.Key, e.Value, e.Source)
			}
		}
	}
	return nil
}

// configJSONEntry matches the required JSON shape.
type configJSONEntry struct {
	Value  string `json:"value"`
	Source string `json:"source"`
}

func runConfigListJSON(cmd *cobra.Command, entries []configEntry) error {
	m := make(map[string]configJSONEntry, len(entries))
	for _, e := range entries {
		m[e.Key] = configJSONEntry{
			Value:  e.Value,
			Source: string(e.Source),
		}
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(m)
}

func runConfigListYAML(cmd *cobra.Command, entries []configEntry) error {
	// Build an ordered map using yaml.Node to preserve key order.
	// We need ordered output — use a yaml.Node mapping.
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}

	// Sort keys for stable output.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Key < entries[j].Key
	})

	for _, e := range entries {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: e.Key}
		valNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		valNode.Content = append(valNode.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "value"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: e.Value},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "source"},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: string(e.Source)},
		)
		root.Content = append(root.Content, keyNode, valNode)
	}

	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
	enc := yaml.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return err
	}
	return enc.Close()
}

