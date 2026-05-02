package cli

import (
	"encoding/json"
	"fmt"

	"github.com/cgould/dtree/internal/config"
	"github.com/cgould/dtree/internal/identity"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// newWhoamiCommand returns the `dtree whoami` command.
func newWhoamiCommand() *cobra.Command {
	var asFlag string

	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Print the resolved identity and where it came from",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			outputFlag, _ := cmd.Root().PersistentFlags().GetString("output")

			cfg, err := config.Load(repoRoot)
			if err != nil {
				return err
			}

			resolver := identity.NewResolver(repoRoot, cfg)
			res, err := resolver.Resolve(asFlag)
			if err != nil {
				return err
			}

			if res.Handle == "" {
				fmt.Fprintln(cmd.ErrOrStderr(), "No identity configured. Run `dtree config set --global identity.default <handle>`")
				return fmt.Errorf("no identity configured")
			}

			format := outputFlag
			if format == "" {
				if isTTY() {
					format = "human"
				} else {
					format = "json"
				}
			}

			switch format {
			case "json":
				return whoamiJSON(cmd, res)
			case "yaml":
				return whoamiYAML(cmd, res)
			default:
				return whoamiHuman(cmd, res)
			}
		},
	}

	cmd.Flags().StringVar(&asFlag, "as", "", "Override identity handle for this invocation")
	return cmd
}

// whoamiHuman writes the human-readable identity line.
func whoamiHuman(cmd *cobra.Command, res *identity.Resolution) error {
	fmt.Fprintf(cmd.OutOrStdout(), "%s (from %s)\n", res.Handle, res.Source)
	if !res.InProject {
		fmt.Fprintf(cmd.OutOrStdout(), "  [not registered in this project — run `dtree actor add %s`]\n", res.Handle)
	}
	return nil
}

// whoamiJSONShape is the JSON wire shape for whoami.
type whoamiJSONShape struct {
	Handle    string          `json:"handle"`
	Source    string          `json:"source"`
	InProject bool            `json:"in_project"`
	Actor     *whoamiActor    `json:"actor,omitempty"`
}

type whoamiActor struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
	Kind  string `json:"kind,omitempty"`
}

func buildWhoamiShape(res *identity.Resolution) whoamiJSONShape {
	s := whoamiJSONShape{
		Handle:    res.Handle,
		Source:    string(res.Source),
		InProject: res.InProject,
	}
	if res.Actor != nil {
		s.Actor = &whoamiActor{
			Name:  res.Actor.Name,
			Email: res.Actor.Email,
			Kind:  string(res.Actor.Kind),
		}
	}
	return s
}

func whoamiJSON(cmd *cobra.Command, res *identity.Resolution) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(buildWhoamiShape(res))
}

func whoamiYAML(cmd *cobra.Command, res *identity.Resolution) error {
	s := buildWhoamiShape(res)
	enc := yaml.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent(2)
	if err := enc.Encode(s); err != nil {
		return err
	}
	return enc.Close()
}
