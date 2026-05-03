package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"
	"time"

	"github.com/cgould/dtree/internal/audit"
	"github.com/cgould/dtree/internal/core"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// newAuditCommand returns the `dtree audit` subcommand group.
func newAuditCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Browse and replay the audit log",
		Long:  "Browse and replay the append-only audit log for decisions and other changes.",
	}

	cmd.AddCommand(newAuditLsCommand())
	cmd.AddCommand(newAuditShowCommand())
	cmd.AddCommand(newAuditReplayCommand())
	return cmd
}

// ---------------------------------------------------------------------------
// audit ls
// ---------------------------------------------------------------------------

func newAuditLsCommand() *cobra.Command {
	var (
		actor    string
		action   string
		kind     string
		decision string
		tree     string
		since    string
		until    string
		limit    int
		cursor   string
	)

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List audit events",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			outputFlag, _ := cmd.Root().PersistentFlags().GetString("output")

			format := resolveFormat(cmd, outputFlag)

			f := audit.Filter{
				Tree:     tree,
				Actor:    actor,
				Action:   core.Action(action),
				Kind:     core.Kind(kind),
				TargetID: decision,
			}

			if since != "" {
				t, err := ParseTimeFlag(since)
				if err != nil {
					return fmt.Errorf("--since: %w", err)
				}
				f.Since = t
			}
			if until != "" {
				t, err := ParseTimeFlag(until)
				if err != nil {
					return fmt.Errorf("--until: %w", err)
				}
				f.Until = t
			}

			// Default limit: 50 for human output; 0 (unlimited) for json/yaml.
			effectiveLimit := limit
			if effectiveLimit == 0 && format == "human" {
				effectiveLimit = 50
			}

			// Fetch one extra to detect whether more results exist.
			if effectiveLimit > 0 {
				f.Limit = effectiveLimit + 1
			}

			events, err := audit.Read(repoRoot, f)
			if err != nil {
				return err
			}

			// Apply cursor: skip all events where EventID <= cursor.
			if cursor != "" {
				filtered := events[:0]
				for _, ev := range events {
					if ev.EventID > cursor {
						filtered = append(filtered, ev)
					}
				}
				events = filtered
			}

			// Detect "has more" and trim to requested limit.
			var nextCursor string
			if effectiveLimit > 0 && len(events) > effectiveLimit {
				nextCursor = events[effectiveLimit-1].EventID
				events = events[:effectiveLimit]
			}

			switch format {
			case "json":
				return auditLsJSON(cmd, events)
			case "yaml":
				return auditLsYAML(cmd, events)
			default:
				if err := auditLsHuman(cmd, events); err != nil {
					return err
				}
				if nextCursor != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "\nnext cursor: %s\n", nextCursor)
				}
				return nil
			}
		},
	}

	cmd.Flags().StringVar(&actor, "actor", "", "Filter by actor handle")
	cmd.Flags().StringVar(&action, "action", "", "Filter by action (create, update, decide, …)")
	cmd.Flags().StringVar(&kind, "kind", "", "Filter by kind (decision, tree, actor, …)")
	cmd.Flags().StringVar(&decision, "decision", "", "Filter by target decision ID")
	cmd.Flags().StringVar(&tree, "tree", "", "Filter by tree slug")
	cmd.Flags().StringVar(&since, "since", "", "Show events at or after this time (7d, 24h, 2026-01-01, RFC3339)")
	cmd.Flags().StringVar(&until, "until", "", "Show events at or before this time (same formats as --since)")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of events (default 50 for human output, unlimited for json/yaml)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Skip events up to and including this event ULID")
	return cmd
}

// auditLsHuman prints a tabular view of events.
func auditLsHuman(cmd *cobra.Command, events []core.Event) error {
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TIME\tACTOR\tACTION\tTARGET\tKIND")
	for _, ev := range events {
		age := humanAge(ev.Ts)
		target := ev.ID
		if len(target) > 8 {
			target = target[:8]
		}
		if ev.Payload.After != nil {
			if sum, ok := ev.Payload.After["summary"].(string); ok && sum != "" {
				short := sum
				if len(short) > 20 {
					short = short[:17] + "…"
				}
				target = fmt.Sprintf("%s (%s)", target, short)
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			age, ev.Actor, ev.Action, target, ev.Kind)
	}
	return w.Flush()
}

// humanAge returns a short human-readable age string for t.
func humanAge(t time.Time) string {
	d := time.Since(t)
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hr ago"
		}
		return fmt.Sprintf("%d hrs ago", h)
	default:
		return t.UTC().Format("2006-01-02")
	}
}

func auditLsJSON(cmd *cobra.Command, events []core.Event) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	if events == nil {
		events = []core.Event{}
	}
	return enc.Encode(events)
}

func auditLsYAML(cmd *cobra.Command, events []core.Event) error {
	if events == nil {
		events = []core.Event{}
	}
	enc := yaml.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent(2)
	if err := enc.Encode(events); err != nil {
		return err
	}
	return enc.Close()
}

// ---------------------------------------------------------------------------
// audit show
// ---------------------------------------------------------------------------

func newAuditShowCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <event-id>",
		Short: "Show a single audit event by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			outputFlag, _ := cmd.Root().PersistentFlags().GetString("output")
			format := resolveFormat(cmd, outputFlag)

			eventID := args[0]

			events, err := audit.Read(repoRoot, audit.Filter{})
			if err != nil {
				return err
			}

			var found *core.Event
			for i := range events {
				if events[i].EventID == eventID {
					found = &events[i]
					break
				}
			}
			if found == nil {
				return fmt.Errorf("event %q not found", eventID)
			}

			switch format {
			case "json":
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(found)
			case "yaml":
				enc := yaml.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent(2)
				if err := enc.Encode(found); err != nil {
					return err
				}
				return enc.Close()
			default:
				return auditShowHuman(cmd, found)
			}
		},
	}
	return cmd
}

func auditShowHuman(cmd *cobra.Command, ev *core.Event) error {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Event ID:  %s\n", ev.EventID)
	fmt.Fprintf(out, "Version:   %d\n", ev.V)
	fmt.Fprintf(out, "Time:      %s\n", ev.Ts.UTC().Format(time.RFC3339))
	fmt.Fprintf(out, "Actor:     %s\n", ev.Actor)
	fmt.Fprintf(out, "Action:    %s\n", ev.Action)
	fmt.Fprintf(out, "Kind:      %s\n", ev.Kind)
	if ev.Tree != "" {
		fmt.Fprintf(out, "Tree:      %s\n", ev.Tree)
	}
	fmt.Fprintf(out, "Target ID: %s\n", ev.ID)
	if ev.Payload.Before != nil || ev.Payload.After != nil || ev.Payload.Extra != nil {
		fmt.Fprintln(out, "Payload:")
		if ev.Payload.Before != nil {
			printMapSection(out, "  before", ev.Payload.Before)
		}
		if ev.Payload.After != nil {
			printMapSection(out, "  after", ev.Payload.After)
		}
		for k, v := range ev.Payload.Extra {
			fmt.Fprintf(out, "  %s: %v\n", k, v)
		}
	}
	return nil
}

func printMapSection(w io.Writer, label string, m map[string]any) {
	fmt.Fprintf(w, "%s:\n", label)
	for k, v := range m {
		fmt.Fprintf(w, "    %s: %v\n", k, v)
	}
}

// ---------------------------------------------------------------------------
// audit replay
// ---------------------------------------------------------------------------

func newAuditReplayCommand() *cobra.Command {
	var (
		at   string
		tree string
	)

	cmd := &cobra.Command{
		Use:   "replay",
		Short: "Reconstruct decision state at a point in time",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			outputFlag, _ := cmd.Root().PersistentFlags().GetString("output")
			format := resolveFormat(cmd, outputFlag)

			if at == "" {
				return fmt.Errorf("--at is required")
			}
			if tree == "" {
				return fmt.Errorf("--tree is required")
			}

			atTime, err := parseAbsoluteTime(at)
			if err != nil {
				return fmt.Errorf("--at: %w", err)
			}

			state, err := audit.ReplayState(repoRoot, tree, atTime)
			if err != nil {
				return err
			}

			switch format {
			case "json":
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(state)
			case "yaml":
				enc := yaml.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent(2)
				if err := enc.Encode(state); err != nil {
					return err
				}
				return enc.Close()
			default:
				return auditReplayHuman(cmd, state)
			}
		},
	}

	cmd.Flags().StringVar(&at, "at", "", "Point in time to replay to (RFC3339 or YYYY-MM-DD)")
	cmd.Flags().StringVar(&tree, "tree", "", "Tree slug to replay (required)")
	return cmd
}

func auditReplayHuman(cmd *cobra.Command, state map[string]*core.Decision) error {
	out := cmd.OutOrStdout()
	if len(state) == 0 {
		fmt.Fprintln(out, "(no decisions at this point in time)")
		return nil
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSTATUS\tPRIORITY\tSUMMARY")
	for id, d := range state {
		prefix := id
		if len(prefix) > 8 {
			prefix = prefix[:8]
		}
		summary := d.Summary
		if len(summary) > 40 {
			summary = summary[:37] + "…"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", prefix, d.Status, d.Priority, summary)
	}
	return w.Flush()
}

// parseAbsoluteTime parses only absolute time forms (RFC3339 or date-only).
// Relative durations like "7d" are rejected since they don't make sense for --at.
func parseAbsoluteTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("cannot parse %q: use RFC3339 (2026-04-22T14:32:11Z) or date-only (2026-04-22)", s)
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// resolveFormat determines the output format from the flag or auto-detection.
func resolveFormat(cmd *cobra.Command, flag string) string {
	if flag != "" {
		return flag
	}
	if isTTY() {
		return "human"
	}
	return "json"
}

