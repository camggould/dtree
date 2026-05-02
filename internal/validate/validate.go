// Package validate enforces domain invariants on decisions and the
// relationship graph. It is consulted by every write path (CLI, HTTP,
// MCP) before mutation and by `dtree fsck` for ad-hoc auditing.
//
// Validation is deliberately data-only: it returns errors but never
// touches storage. Callers decide whether to surface, fix, or refuse.
package validate

import (
	"errors"
	"fmt"

	"github.com/cgould/dtree/internal/core"
	"github.com/cgould/dtree/internal/ulid"
)

// Error is a domain-validation failure. The Field, when non-empty,
// names the offending property. Code is a stable identifier for the
// HTTP/MCP layer's Problem-Details `type` URI.
type Error struct {
	Code    string
	Field   string
	Message string
}

func (e *Error) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: %s (field=%s)", e.Code, e.Message, e.Field)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// newErr constructs a domain validation error.
func newErr(code, field, msg string) *Error {
	return &Error{Code: code, Field: field, Message: msg}
}

// Decision validates the standalone invariants on d (no graph context).
// Returns nil if d is consistent. Errors are returned individually;
// callers can collect them by retrying after each fix, or use
// CollectDecision for batched output.
func Decision(d *core.Decision) error {
	if d == nil {
		return newErr("invalid", "", "decision is nil")
	}
	if err := ulid.Parse(d.ID); err != nil {
		return newErr("invalid_id", "id", err.Error())
	}
	if d.Slug == "" {
		return newErr("invalid_slug", "slug", "slug is required")
	}
	if d.Summary == "" {
		return newErr("invalid_summary", "summary", "summary is required")
	}
	if !validPriority(d.Priority) {
		return newErr("invalid_priority", "priority",
			fmt.Sprintf("priority %q not in {assumption,low,medium,high,critical}", d.Priority))
	}
	if !validStatus(d.Status) {
		return newErr("invalid_status", "status",
			fmt.Sprintf("status %q not in {proposed,decided,out_of_scope,superseded}", d.Status))
	}
	if d.Creator == "" {
		return newErr("invalid_creator", "creator", "creator handle is required")
	}

	// Status-specific invariants.
	switch d.Status {
	case core.StatusDecided:
		if d.ActualChoice == "" {
			return newErr("incomplete_decided", "actual_choice",
				"status=decided requires actual_choice")
		}
		if d.ActualChoiceReason == "" {
			return newErr("incomplete_decided", "actual_choice_reason",
				"status=decided requires actual_choice_reason")
		}
		if len(d.DecidedBy) == 0 {
			return newErr("incomplete_decided", "decided_by",
				"status=decided requires at least one decided_by handle")
		}
	case core.StatusOutOfScope:
		// out_of_scope_reason is optional. No invariant enforced.
	case core.StatusSuperseded:
		// supersedes edge is enforced at the graph level (Graph()),
		// not here — single-decision validation can't see other nodes.
	}

	// Recommendation invariant: a recommender requires a recommendation.
	if d.RecommendedBy != "" && d.RecommendedSummary == "" {
		return newErr("incomplete_recommendation", "recommended_summary",
			"recommended_by set but recommended_summary is empty")
	}

	// Per-edge structural checks (cycles need the full graph).
	for i, r := range d.Relationships {
		if !validRelType(r.Type) {
			return newErr("invalid_relationship_type",
				fmt.Sprintf("relationships[%d].type", i),
				fmt.Sprintf("type %q not in {blocks,influences,supersedes,relates_to}", r.Type))
		}
		if err := ulid.Parse(r.Target); err != nil {
			return newErr("invalid_relationship_target",
				fmt.Sprintf("relationships[%d].target", i),
				fmt.Sprintf("target ULID invalid: %s", err.Error()))
		}
		if r.Target == d.ID {
			return newErr("self_relationship",
				fmt.Sprintf("relationships[%d]", i),
				"a decision cannot have a relationship to itself")
		}
	}

	return nil
}

// CollectDecision returns all validation errors for d, not just the
// first. Useful for fsck output.
func CollectDecision(d *core.Decision) []error {
	var errs []error
	// We can't easily run Decision() repeatedly to collect; instead
	// re-implement the same checks as a series. Keeping logic in
	// sync with Decision() above is mandatory; helper functions below.
	for _, check := range decisionChecks(d) {
		if err := check(); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

func decisionChecks(d *core.Decision) []func() error {
	if d == nil {
		return []func() error{func() error { return newErr("invalid", "", "decision is nil") }}
	}
	checks := []func() error{
		func() error {
			if err := ulid.Parse(d.ID); err != nil {
				return newErr("invalid_id", "id", err.Error())
			}
			return nil
		},
		func() error {
			if d.Slug == "" {
				return newErr("invalid_slug", "slug", "slug is required")
			}
			return nil
		},
		func() error {
			if d.Summary == "" {
				return newErr("invalid_summary", "summary", "summary is required")
			}
			return nil
		},
		func() error {
			if !validPriority(d.Priority) {
				return newErr("invalid_priority", "priority",
					fmt.Sprintf("priority %q invalid", d.Priority))
			}
			return nil
		},
		func() error {
			if !validStatus(d.Status) {
				return newErr("invalid_status", "status",
					fmt.Sprintf("status %q invalid", d.Status))
			}
			return nil
		},
	}
	if d.Status == core.StatusDecided {
		checks = append(checks,
			func() error {
				if d.ActualChoice == "" {
					return newErr("incomplete_decided", "actual_choice",
						"status=decided requires actual_choice")
				}
				return nil
			},
			func() error {
				if d.ActualChoiceReason == "" {
					return newErr("incomplete_decided", "actual_choice_reason",
						"status=decided requires actual_choice_reason")
				}
				return nil
			},
			func() error {
				if len(d.DecidedBy) == 0 {
					return newErr("incomplete_decided", "decided_by",
						"status=decided requires at least one decided_by handle")
				}
				return nil
			},
		)
	}
	if d.RecommendedBy != "" && d.RecommendedSummary == "" {
		checks = append(checks, func() error {
			return newErr("incomplete_recommendation", "recommended_summary",
				"recommended_by set but recommended_summary is empty")
		})
	}
	for i, r := range d.Relationships {
		i, r := i, r
		checks = append(checks, func() error {
			if !validRelType(r.Type) {
				return newErr("invalid_relationship_type",
					fmt.Sprintf("relationships[%d].type", i),
					fmt.Sprintf("type %q invalid", r.Type))
			}
			if err := ulid.Parse(r.Target); err != nil {
				return newErr("invalid_relationship_target",
					fmt.Sprintf("relationships[%d].target", i), err.Error())
			}
			if r.Target == d.ID {
				return newErr("self_relationship",
					fmt.Sprintf("relationships[%d]", i),
					"a decision cannot have a relationship to itself")
			}
			return nil
		})
	}
	return checks
}

// Edge represents one directed relationship for graph-level checks.
// Source and Target are ULIDs.
type Edge struct {
	Source string
	Target string
	Type   core.RelationshipType
}

// Graph runs whole-graph invariants: cycle detection on `blocks` and
// `supersedes` edges. influences/relates_to may cycle (informational).
//
// Returns nil if no cycles. On cycle, returns an Error with Code
// "cycle_detected" and Message containing the cycle path.
func Graph(edges []Edge) error {
	for _, t := range []core.RelationshipType{core.RelBlocks, core.RelSupersedes} {
		if cycle := findCycle(edges, t); cycle != nil {
			return newErr("cycle_detected",
				string(t),
				fmt.Sprintf("cycle in %s graph: %s", t, formatCycle(cycle)))
		}
	}
	return nil
}

// AddingEdgeWouldCycle reports whether adding (source, target, type)
// to the existing edge set would introduce a cycle. Cheap: a DFS from
// target looking for source. Used by `dtree relate` to refuse before
// writing.
func AddingEdgeWouldCycle(edges []Edge, source, target string, t core.RelationshipType) bool {
	if t != core.RelBlocks && t != core.RelSupersedes {
		return false
	}
	if source == target {
		return true // self-edge is trivially a cycle
	}
	// DFS from target following only `t`-edges; if we reach source,
	// the new edge source→target closes a cycle.
	adj := buildAdjacency(edges, t)
	visited := make(map[string]bool)
	var dfs func(node string) bool
	dfs = func(node string) bool {
		if node == source {
			return true
		}
		if visited[node] {
			return false
		}
		visited[node] = true
		for _, next := range adj[node] {
			if dfs(next) {
				return true
			}
		}
		return false
	}
	return dfs(target)
}

// findCycle returns the first cycle (as a node-slice) in edges of the
// given type, or nil if acyclic. Standard iterative-DFS three-color
// (WHITE/GRAY/BLACK) algorithm.
func findCycle(edges []Edge, t core.RelationshipType) []string {
	adj := buildAdjacency(edges, t)
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int)
	parent := make(map[string]string)
	for node := range adj {
		if color[node] == white {
			if cycle := dfsCycle(node, adj, color, parent, gray, black, white); cycle != nil {
				return cycle
			}
		}
	}
	return nil
}

func dfsCycle(start string, adj map[string][]string, color map[string]int, parent map[string]string, gray, black, white int) []string {
	stack := []string{start}
	color[start] = gray
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		// Find first white neighbor.
		var nextWhite string
		for _, n := range adj[node] {
			switch color[n] {
			case gray:
				// back edge → cycle. Reconstruct path n → ... → node → n.
				return reconstructCycle(parent, node, n)
			case white:
				nextWhite = n
			}
			if nextWhite != "" {
				break
			}
		}
		if nextWhite != "" {
			color[nextWhite] = gray
			parent[nextWhite] = node
			stack = append(stack, nextWhite)
		} else {
			color[node] = black
			stack = stack[:len(stack)-1]
		}
	}
	return nil
}

func reconstructCycle(parent map[string]string, from, to string) []string {
	// Walk parent chain from `from` back until we hit `to`, then
	// close the loop with `to` itself.
	out := []string{from}
	cur := from
	for cur != to {
		next, ok := parent[cur]
		if !ok {
			break
		}
		cur = next
		out = append(out, cur)
	}
	// Reverse to get from → ... → to order, then append from again
	// to make the cycle visible.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	out = append(out, from)
	return out
}

func buildAdjacency(edges []Edge, t core.RelationshipType) map[string][]string {
	adj := map[string][]string{}
	for _, e := range edges {
		if e.Type != t {
			continue
		}
		adj[e.Source] = append(adj[e.Source], e.Target)
		// Make sure target appears as a node even with no outgoing edges.
		if _, ok := adj[e.Target]; !ok {
			adj[e.Target] = nil
		}
	}
	return adj
}

func formatCycle(cycle []string) string {
	if len(cycle) == 0 {
		return ""
	}
	out := cycle[0]
	for _, n := range cycle[1:] {
		out += " → " + n
	}
	return out
}

// AsValidationError tests whether err is a domain validation Error (vs
// a generic transport/IO error). Useful for the HTTP layer to map to
// 422 vs 500.
func AsValidationError(err error) (*Error, bool) {
	var ve *Error
	if errors.As(err, &ve) {
		return ve, true
	}
	return nil, false
}

func validPriority(p core.Priority) bool {
	switch p {
	case core.PriorityAssumption, core.PriorityLow, core.PriorityMedium, core.PriorityHigh, core.PriorityCritical:
		return true
	}
	return false
}

func validStatus(s core.Status) bool {
	switch s {
	case core.StatusProposed, core.StatusDecided, core.StatusOutOfScope, core.StatusSuperseded:
		return true
	}
	return false
}

func validRelType(t core.RelationshipType) bool {
	switch t {
	case core.RelBlocks, core.RelInfluences, core.RelSupersedes, core.RelRelatesTo:
		return true
	}
	return false
}
