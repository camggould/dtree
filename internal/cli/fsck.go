package cli

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/cgould/dtree/internal/index"
	"github.com/cgould/dtree/internal/validate"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// FsckReport holds fsck results.
type FsckReport struct {
	Clean            bool             `json:"clean" yaml:"clean"`
	DecisionIssues   []DecisionIssue  `json:"decision_issues" yaml:"decision_issues"`
	GraphIssues      []string         `json:"graph_issues" yaml:"graph_issues"`
	TotalViolations  int              `json:"total_violations" yaml:"total_violations"`
}

// DecisionIssue holds one decision's validation failures.
type DecisionIssue struct {
	ID     string   `json:"id" yaml:"id"`
	Errors []string `json:"errors" yaml:"errors"`
}

// newFsckCommand returns the `dtree fsck` command.
func newFsckCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fsck",
		Short: "Check integrity of all decisions and the relationship graph",
		Long:  "Walk all decisions in the index and validate each one. Also check the relationship graph for cycles.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			outputFlag, _ := cmd.Root().PersistentFlags().GetString("output")
			format := resolveFormat(cmd, outputFlag)

			report, err := runFsck(repoRoot)
			if err != nil {
				return err
			}

			switch format {
			case "json":
				if err := fsckJSON(cmd, report); err != nil {
					return err
				}
			case "yaml":
				if err := fsckYAML(cmd, report); err != nil {
					return err
				}
			default:
				fsckHuman(cmd, report)
			}

			if !report.Clean {
				return fmt.Errorf("fsck: %d violation(s) found", report.TotalViolations)
			}
			return nil
		},
	}
	return cmd
}

// runFsck performs the fsck check and returns a report.
func runFsck(repoRoot string) (*FsckReport, error) {
	report := &FsckReport{
		DecisionIssues: []DecisionIssue{},
		GraphIssues:    []string{},
	}

	indexPath := filepath.Join(repoRoot, ".decisions", ".index.db")
	db, err := index.Open(indexPath)
	if err != nil {
		return nil, fmt.Errorf("fsck: open index: %w", err)
	}
	defer db.Close()

	// Validate each decision.
	decisions, err := loadAllDecisions(db)
	if err != nil {
		return nil, fmt.Errorf("fsck: load decisions: %w", err)
	}

	for _, d := range decisions {
		errs := validate.CollectDecision(d)
		if len(errs) > 0 {
			msgs := make([]string, len(errs))
			for i, e := range errs {
				msgs[i] = e.Error()
			}
			report.DecisionIssues = append(report.DecisionIssues, DecisionIssue{
				ID:     d.ID,
				Errors: msgs,
			})
			report.TotalViolations += len(errs)
		}
	}

	// Validate graph.
	edges, err := loadEdges(db)
	if err != nil {
		return nil, fmt.Errorf("fsck: load edges: %w", err)
	}
	if graphErr := validate.Graph(edges); graphErr != nil {
		report.GraphIssues = append(report.GraphIssues, graphErr.Error())
		report.TotalViolations++
	}

	report.Clean = report.TotalViolations == 0
	return report, nil
}

func fsckHuman(cmd *cobra.Command, r *FsckReport) {
	out := cmd.OutOrStdout()
	if r.Clean {
		fmt.Fprintln(out, "fsck: OK — no violations found.")
		return
	}
	fmt.Fprintf(out, "fsck: %d violation(s) found.\n\n", r.TotalViolations)
	if len(r.DecisionIssues) > 0 {
		fmt.Fprintln(out, "Decision violations:")
		for _, di := range r.DecisionIssues {
			fmt.Fprintf(out, "  %s:\n", di.ID)
			for _, e := range di.Errors {
				fmt.Fprintf(out, "    - %s\n", e)
			}
		}
	}
	if len(r.GraphIssues) > 0 {
		fmt.Fprintln(out, "Graph violations:")
		for _, g := range r.GraphIssues {
			fmt.Fprintf(out, "  - %s\n", g)
		}
	}
}

func fsckJSON(cmd *cobra.Command, r *FsckReport) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func fsckYAML(cmd *cobra.Command, r *FsckReport) error {
	enc := yaml.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent(2)
	if err := enc.Encode(r); err != nil {
		return err
	}
	return enc.Close()
}
