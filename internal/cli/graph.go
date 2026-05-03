// Package cli — `dtree graph` walks the relationships table to surface
// dependency closures, downstream impact, full closures, cycles, and
// Graphviz-rendered visualizations of decision relationships.
//
// Convention: `relationships(source, target, type='blocks')` means
// "source must be done before target". So:
//   - `graph deps <X>`       returns the closure of decisions that must be
//     done before X (sources of `blocks` edges where target=X, transitively
//     walking backward through `target -> source`).
//   - `graph downstream <X>` returns the closure of decisions waiting on X
//     (targets of `blocks` edges where source=X, transitively walking
//     forward through `source -> target`).
//
// `graph closure <X>` walks ALL relationship types in BOTH directions and
// returns every decision reachable. `graph cycles` (no id) detects any cycle
// in the global `blocks` graph. `graph viz <X>` emits the closure of X in
// Graphviz DOT format.
package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/cgould/dtree/internal/index"
	"github.com/spf13/cobra"
)

const (
	graphDefaultDepth = 0   // 0 = unlimited
	graphMaxDepth     = 100 // hard cap to prevent runaway BFS
)

// graphNode is a decision projected for graph output.
type graphNode struct {
	ID      string `json:"id" yaml:"id"`
	Summary string `json:"summary" yaml:"summary"`
	Status  string `json:"status" yaml:"status"`
}

// graphEdge is a directed relationship between two decisions.
type graphEdge struct {
	Source string `json:"source" yaml:"source"`
	Target string `json:"target" yaml:"target"`
	Type   string `json:"type" yaml:"type"`
}

// graphResult is the JSON-rendered output for graph subcommands.
type graphResult struct {
	Root  string      `json:"root" yaml:"root"`
	Nodes []graphNode `json:"nodes" yaml:"nodes"`
	Edges []graphEdge `json:"edges" yaml:"edges"`
}

// newGraphCommand returns the `dtree graph` parent command.
func newGraphCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Walk decision relationships (deps, downstream, closure, cycles, viz)",
		Long: "Walk the relationships graph. Subcommands:\n" +
			"  deps       decisions that must be done before X (transitive blockers)\n" +
			"  downstream decisions waiting on X (transitive dependents)\n" +
			"  closure    full transitive closure of all relationship types, both directions\n" +
			"  cycles     find any cycle in the global blocks graph\n" +
			"  viz        emit Graphviz DOT for the closure of X",
	}
	cmd.AddCommand(newGraphDepsCommand())
	cmd.AddCommand(newGraphDownstreamCommand())
	cmd.AddCommand(newGraphClosureCommand())
	cmd.AddCommand(newGraphCyclesCommand())
	cmd.AddCommand(newGraphVizCommand())
	return cmd
}

// ---------------------------------------------------------------------------
// graph deps
// ---------------------------------------------------------------------------

func newGraphDepsCommand() *cobra.Command {
	var depth int
	cmd := &cobra.Command{
		Use:   "deps <id>",
		Short: "Show transitive blockers (decisions that must be done before this one)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGraphWalk(cmd, args[0], graphWalkOptions{
				direction: walkBackwardBlocks,
				depth:     depth,
				format:    outputFormat(cmd),
			})
		},
	}
	cmd.Flags().IntVar(&depth, "depth", graphDefaultDepth, "Max BFS depth (0=unlimited; capped at 100)")
	cmd.Flags().StringP("output", "o", "", "Output format: human, json")
	return cmd
}

// ---------------------------------------------------------------------------
// graph downstream
// ---------------------------------------------------------------------------

func newGraphDownstreamCommand() *cobra.Command {
	var depth int
	cmd := &cobra.Command{
		Use:   "downstream <id>",
		Short: "Show transitive dependents (decisions waiting on this one)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGraphWalk(cmd, args[0], graphWalkOptions{
				direction: walkForwardBlocks,
				depth:     depth,
				format:    outputFormat(cmd),
			})
		},
	}
	cmd.Flags().IntVar(&depth, "depth", graphDefaultDepth, "Max BFS depth (0=unlimited; capped at 100)")
	cmd.Flags().StringP("output", "o", "", "Output format: human, json")
	return cmd
}

// ---------------------------------------------------------------------------
// graph closure
// ---------------------------------------------------------------------------

func newGraphClosureCommand() *cobra.Command {
	var depth int
	cmd := &cobra.Command{
		Use:   "closure <id>",
		Short: "Show full transitive closure (all rel types, both directions)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGraphWalk(cmd, args[0], graphWalkOptions{
				direction: walkBoth,
				depth:     depth,
				format:    outputFormat(cmd),
			})
		},
	}
	cmd.Flags().IntVar(&depth, "depth", graphDefaultDepth, "Max BFS depth (0=unlimited; capped at 100)")
	cmd.Flags().StringP("output", "o", "", "Output format: human, json")
	return cmd
}

// ---------------------------------------------------------------------------
// graph cycles
// ---------------------------------------------------------------------------

func newGraphCyclesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cycles",
		Short: "Find any cycle in the global blocks graph",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
			if err := requireDecisionsDir(repoRoot); err != nil {
				return err
			}
			db, err := openIndex(repoRoot)
			if err != nil {
				return fmt.Errorf("graph cycles: open index: %w", err)
			}
			defer db.Close()

			cycles, err := findAllBlocksCycles(db)
			if err != nil {
				return fmt.Errorf("graph cycles: %w", err)
			}

			format := outputFormat(cmd)
			if format == "json" {
				out := struct {
					Cycles [][]string `json:"cycles"`
				}{Cycles: cycles}
				if out.Cycles == nil {
					out.Cycles = [][]string{}
				}
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}
			if len(cycles) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(no cycles)")
				return nil
			}
			for _, c := range cycles {
				fmt.Fprintln(cmd.OutOrStdout(), strings.Join(shortenIDs(c), " -> "))
			}
			return nil
		},
	}
	cmd.Flags().StringP("output", "o", "", "Output format: human, json")
	return cmd
}

// ---------------------------------------------------------------------------
// graph viz
// ---------------------------------------------------------------------------

func newGraphVizCommand() *cobra.Command {
	var depth int
	cmd := &cobra.Command{
		Use:   "viz <id>",
		Short: "Emit Graphviz DOT for the closure of this decision",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format := outputFormat(cmd)
			// viz defaults to dot when output is not explicitly set.
			localFormat, _ := cmd.Flags().GetString("output")
			if localFormat == "" {
				rootFormat, _ := cmd.Root().PersistentFlags().GetString("output")
				if rootFormat == "" {
					format = "dot"
				}
			}
			return runGraphWalk(cmd, args[0], graphWalkOptions{
				direction: walkBoth,
				depth:     depth,
				format:    format,
			})
		},
	}
	cmd.Flags().IntVar(&depth, "depth", graphDefaultDepth, "Max BFS depth (0=unlimited; capped at 100)")
	cmd.Flags().StringP("output", "o", "", "Output format: human, json, dot (default: dot)")
	return cmd
}

// ---------------------------------------------------------------------------
// Walk core
// ---------------------------------------------------------------------------

type walkDirection int

const (
	// walkForwardBlocks follows source -> target on type='blocks' only.
	walkForwardBlocks walkDirection = iota
	// walkBackwardBlocks follows target -> source on type='blocks' only.
	walkBackwardBlocks
	// walkBoth follows all types in both directions.
	walkBoth
)

type graphWalkOptions struct {
	direction walkDirection
	depth     int
	format    string
}

// runGraphWalk runs a BFS from a resolved root id and emits the requested format.
func runGraphWalk(cmd *cobra.Command, rawID string, opts graphWalkOptions) error {
	repoRoot, _ := cmd.Root().PersistentFlags().GetString("repo-root")
	if err := requireDecisionsDir(repoRoot); err != nil {
		return err
	}
	db, err := openIndex(repoRoot)
	if err != nil {
		return fmt.Errorf("graph: open index: %w", err)
	}
	defer db.Close()

	rootID, err := resolveDecisionID(db, rawID)
	if err != nil {
		return err
	}

	depthCap := opts.depth
	if depthCap <= 0 || depthCap > graphMaxDepth {
		depthCap = graphMaxDepth
	}

	nodes, edges, depths, err := bfsGraph(db, rootID, opts.direction, depthCap)
	if err != nil {
		return fmt.Errorf("graph: bfs: %w", err)
	}

	switch opts.format {
	case "json":
		return emitGraphJSON(cmd, rootID, nodes, edges)
	case "dot":
		return emitGraphDOT(cmd, rootID, nodes, edges)
	default:
		return emitGraphHuman(cmd, rootID, nodes, edges, depths, opts.direction)
	}
}

// bfsGraph performs a BFS from root following the given direction. Returns
// (nodes, edges, perNodeDepth) where edges include every relationship between
// any two visited nodes (so the closure is complete).
func bfsGraph(db *index.DB, root string, dir walkDirection, depthCap int) (
	[]graphNode, []graphEdge, map[string]int, error,
) {
	visited := map[string]int{root: 0}
	queue := []string{root}

	type rel struct {
		source, target, typ string
	}
	edgeSet := map[rel]bool{}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		currDepth := visited[curr]
		if currDepth >= depthCap {
			continue
		}

		neighbors, err := graphNeighbors(db, curr, dir)
		if err != nil {
			return nil, nil, nil, err
		}
		for _, n := range neighbors {
			edgeSet[rel{n.source, n.target, n.typ}] = true
			next := n.other
			if _, seen := visited[next]; seen {
				continue
			}
			visited[next] = currDepth + 1
			queue = append(queue, next)
		}
	}

	// Hydrate visited ids into graphNodes.
	ids := make([]string, 0, len(visited))
	for id := range visited {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	nodes, err := loadGraphNodes(db, ids)
	if err != nil {
		return nil, nil, nil, err
	}

	// Materialize edges: only those where both endpoints are visited.
	edges := make([]graphEdge, 0, len(edgeSet))
	for e := range edgeSet {
		if _, okS := visited[e.source]; !okS {
			continue
		}
		if _, okT := visited[e.target]; !okT {
			continue
		}
		edges = append(edges, graphEdge{Source: e.source, Target: e.target, Type: e.typ})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].Source != edges[j].Source {
			return edges[i].Source < edges[j].Source
		}
		if edges[i].Target != edges[j].Target {
			return edges[i].Target < edges[j].Target
		}
		return edges[i].Type < edges[j].Type
	})

	return nodes, edges, visited, nil
}

// neighbor describes one edge incident on the current node, plus the id of
// the "other" endpoint relative to the BFS.
type neighbor struct {
	source, target, typ, other string
}

// graphNeighbors returns the relationships incident on curr in the requested
// walk direction.
func graphNeighbors(db *index.DB, curr string, dir walkDirection) ([]neighbor, error) {
	var (
		out []neighbor
	)

	switch dir {
	case walkForwardBlocks:
		rows, err := db.Conn().Query(
			`SELECT source, target, type FROM relationships WHERE source = ? AND type = 'blocks'`,
			curr,
		)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var s, t, ty string
			if err := rows.Scan(&s, &t, &ty); err != nil {
				return nil, err
			}
			out = append(out, neighbor{source: s, target: t, typ: ty, other: t})
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	case walkBackwardBlocks:
		rows, err := db.Conn().Query(
			`SELECT source, target, type FROM relationships WHERE target = ? AND type = 'blocks'`,
			curr,
		)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var s, t, ty string
			if err := rows.Scan(&s, &t, &ty); err != nil {
				return nil, err
			}
			out = append(out, neighbor{source: s, target: t, typ: ty, other: s})
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	case walkBoth:
		// Outgoing edges (curr is source).
		rows, err := db.Conn().Query(
			`SELECT source, target, type FROM relationships WHERE source = ?`,
			curr,
		)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var s, t, ty string
			if err := rows.Scan(&s, &t, &ty); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, neighbor{source: s, target: t, typ: ty, other: t})
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()

		// Incoming edges (curr is target).
		rows, err = db.Conn().Query(
			`SELECT source, target, type FROM relationships WHERE target = ?`,
			curr,
		)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var s, t, ty string
			if err := rows.Scan(&s, &t, &ty); err != nil {
				return nil, err
			}
			out = append(out, neighbor{source: s, target: t, typ: ty, other: s})
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// loadGraphNodes materializes (id, summary, status) for the given ids.
func loadGraphNodes(db *index.DB, ids []string) ([]graphNode, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	q := `SELECT id, summary, status FROM decisions WHERE id IN (` +
		placeholders(len(ids)) + `)`
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := db.Conn().Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byID := map[string]graphNode{}
	for rows.Next() {
		var n graphNode
		if err := rows.Scan(&n.ID, &n.Summary, &n.Status); err != nil {
			return nil, err
		}
		byID[n.ID] = n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]graphNode, 0, len(ids))
	for _, id := range ids {
		if n, ok := byID[id]; ok {
			out = append(out, n)
		} else {
			// Decision missing from index; surface bare id.
			out = append(out, graphNode{ID: id})
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Cycle detection
// ---------------------------------------------------------------------------

// findAllBlocksCycles returns at most one representative cycle per strongly
// connected component in the blocks graph. Each cycle is presented as a list
// of ids ending with the same id it started with.
func findAllBlocksCycles(db *index.DB) ([][]string, error) {
	// Load full edge list once.
	rows, err := db.Conn().Query(
		`SELECT source, target FROM relationships WHERE type = 'blocks'`,
	)
	if err != nil {
		return nil, err
	}
	adj := map[string][]string{}
	nodeSet := map[string]bool{}
	for rows.Next() {
		var s, t string
		if err := rows.Scan(&s, &t); err != nil {
			rows.Close()
			return nil, err
		}
		adj[s] = append(adj[s], t)
		nodeSet[s] = true
		nodeSet[t] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// DFS with explicit stack to detect cycles. Track per-search-start ids
	// already-fully-explored to avoid revisiting them as cycle starts.
	finished := map[string]bool{}
	nodes := make([]string, 0, len(nodeSet))
	for id := range nodeSet {
		nodes = append(nodes, id)
	}
	sort.Strings(nodes)

	var cycles [][]string
	seenCycleSig := map[string]bool{}

	for _, start := range nodes {
		if finished[start] {
			continue
		}
		// Iterative DFS with parent tracking.
		stack := []string{start}
		parent := map[string]string{start: ""}
		onPath := map[string]bool{start: true}
		order := []string{start}

		for len(stack) > 0 {
			curr := stack[len(stack)-1]
			advanced := false
			for _, next := range adj[curr] {
				if onPath[next] {
					// Found cycle: walk parents from curr until we hit next.
					path := []string{curr}
					for p := parent[curr]; p != "" && p != next; p = parent[p] {
						path = append([]string{p}, path...)
					}
					path = append([]string{next}, path...)
					path = append(path, next)
					sig := cycleSignature(path)
					if !seenCycleSig[sig] {
						seenCycleSig[sig] = true
						cycles = append(cycles, path)
					}
					continue
				}
				if finished[next] {
					continue
				}
				if _, seen := parent[next]; seen {
					continue
				}
				parent[next] = curr
				onPath[next] = true
				order = append(order, next)
				stack = append(stack, next)
				advanced = true
				break
			}
			if !advanced {
				stack = stack[:len(stack)-1]
				onPath[curr] = false
				finished[curr] = true
			}
		}
		_ = order
	}
	return cycles, nil
}

// cycleSignature returns a canonical string representing the cycle's set of
// edges, so duplicate detections of the same cycle from different starting
// nodes collapse into one.
func cycleSignature(cyc []string) string {
	// Drop the trailing repeat, find the lex-min rotation.
	if len(cyc) < 2 {
		return strings.Join(cyc, "->")
	}
	body := cyc[:len(cyc)-1]
	minIdx := 0
	for i := 1; i < len(body); i++ {
		if body[i] < body[minIdx] {
			minIdx = i
		}
	}
	rot := append(append([]string{}, body[minIdx:]...), body[:minIdx]...)
	rot = append(rot, rot[0])
	return strings.Join(rot, "->")
}

// ---------------------------------------------------------------------------
// Renderers
// ---------------------------------------------------------------------------

func emitGraphJSON(cmd *cobra.Command, root string, nodes []graphNode, edges []graphEdge) error {
	res := graphResult{Root: root, Nodes: nodes, Edges: edges}
	if res.Nodes == nil {
		res.Nodes = []graphNode{}
	}
	if res.Edges == nil {
		res.Edges = []graphEdge{}
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(res)
}

func emitGraphDOT(cmd *cobra.Command, root string, nodes []graphNode, edges []graphEdge) error {
	w := cmd.OutOrStdout()
	fmt.Fprintln(w, "digraph G {")
	fmt.Fprintln(w, "  rankdir=LR;")
	for _, n := range nodes {
		label := strings.ReplaceAll(n.Summary, `"`, `\"`)
		marker := ""
		if n.ID == root {
			marker = ", style=bold"
		}
		fmt.Fprintf(w, "  %q [label=%q%s];\n", n.ID, fmt.Sprintf("%s\\n%s", shortenID(n.ID), label), marker)
	}
	for _, e := range edges {
		fmt.Fprintf(w, "  %q -> %q [label=%q];\n", e.Source, e.Target, e.Type)
	}
	fmt.Fprintln(w, "}")
	return nil
}

func emitGraphHuman(cmd *cobra.Command, root string, nodes []graphNode, edges []graphEdge, depths map[string]int, dir walkDirection) error {
	w := cmd.OutOrStdout()

	// Build adjacency for tree rendering. For walkForwardBlocks we render
	// children = outgoing blocks. For walkBackwardBlocks, children = sources
	// blocking the parent. For walkBoth we render an undirected adjacency.
	children := map[string][]string{}
	for _, e := range edges {
		switch dir {
		case walkForwardBlocks:
			if e.Type == "blocks" {
				children[e.Source] = append(children[e.Source], e.Target)
			}
		case walkBackwardBlocks:
			if e.Type == "blocks" {
				children[e.Target] = append(children[e.Target], e.Source)
			}
		case walkBoth:
			children[e.Source] = append(children[e.Source], e.Target)
			children[e.Target] = append(children[e.Target], e.Source)
		}
	}

	// Build a summary index for printing.
	byID := map[string]graphNode{}
	for _, n := range nodes {
		byID[n.ID] = n
	}

	if len(nodes) == 0 {
		fmt.Fprintln(w, "(no nodes)")
		return nil
	}

	// DFS print rooted at root.
	visited := map[string]bool{}
	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		if visited[id] {
			return
		}
		visited[id] = true
		n, ok := byID[id]
		summary := ""
		status := ""
		if ok {
			summary = n.Summary
			status = n.Status
		}
		indent := strings.Repeat("  ", depth)
		fmt.Fprintf(w, "%s%s  %s  [%s]\n", indent, shortenID(id), summary, status)
		kids := append([]string{}, children[id]...)
		sort.Strings(kids)
		for _, k := range kids {
			walk(k, depth+1)
		}
	}
	walk(root, 0)

	// Print any orphans (nodes visited via BFS but not reachable in our tree
	// rendering — should be rare; can occur with walkBoth and isolated edges).
	for _, n := range nodes {
		if !visited[n.ID] {
			fmt.Fprintf(w, "%s  %s  [%s]\n", shortenID(n.ID), n.Summary, n.Status)
		}
	}
	return nil
}
