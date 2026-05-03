import { useCallback, useMemo, type CSSProperties } from "react";
import {
  ReactFlow,
  Background,
  Controls,
  MarkerType,
  Handle,
  Position,
  type Node,
  type Edge,
  type NodeProps,
} from "@xyflow/react";
import dagre from "@dagrejs/dagre";
import { useParams } from "wouter";
import { useDecisions } from "@/api/query";
import { useAppStore } from "@/store/app";
import { humanStatus } from "@/util/labels";
import {
  FilterPills,
  useFilterParams,
  type FilterDef,
} from "@/components/FilterPills";
import type { Decision, RelationshipType, Status } from "@/api/types.gen";
import "@xyflow/react/dist/style.css";

// ---- Visuals --------------------------------------------------------------

import type { Priority } from "@/api/types.gen";

// For PROPOSED decisions the ring colour communicates PRIORITY (so you can
// scan the graph and see what's critical). For decisions in any other status
// the ring colour communicates STATUS.
//
// Assumption is special: even though the API stores it as a Priority, we
// treat it as a fourth status. They aren't proposals or normal decisions —
// they're "I'll go with X for now, revisit later". Always grey + dashed.
const PRIORITY_PALETTE: Record<
  Exclude<Priority, "assumption">,
  { ring: string; label: string }
> = {
  critical: { ring: "#b91c1c", label: "Critical" }, // red-700
  high:     { ring: "#ea580c", label: "High" },     // orange-600
  medium:   { ring: "#1d4ed8", label: "Medium" },   // blue-700
  low:      { ring: "#475569", label: "Low" },      // slate-600
};
const ASSUMPTION_STYLE = {
  ring: "#94a3b8", // slate-400 — calmer than any priority
  label: "Assumption",
  dashed: true,
};
const STATUS_PALETTE_NON_PROPOSED: Record<
  Exclude<Status, "proposed">,
  { ring: string; label: string; dashed?: boolean }
> = {
  decided:      { ring: "#15803d", label: "Decided" },
  out_of_scope: { ring: "#94a3b8", label: "Out of scope", dashed: true },
  superseded:   { ring: "#a16207", label: "Superseded", dashed: true },
};

function nodeStyleFor(d: { status: Status; priority: Priority }): {
  ring: string;
  label: string;
  dashed: boolean;
} {
  if (d.priority === "assumption") return { ...ASSUMPTION_STYLE };
  if (d.status === "proposed") {
    const p = PRIORITY_PALETTE[d.priority as Exclude<Priority, "assumption">];
    return { ring: p.ring, label: p.label.toUpperCase(), dashed: false };
  }
  const s =
    STATUS_PALETTE_NON_PROPOSED[d.status as Exclude<Status, "proposed">];
  return { ring: s.ring, label: s.label.toUpperCase(), dashed: s.dashed ?? false };
}

const EDGE_COLORS: Record<RelationshipType, string> = {
  blocks: "#dc2626",
  influences: "#ca8a04",
  supersedes: "#ea580c",
  relates_to: "#2563eb",
};

// ---- Custom node ----------------------------------------------------------

// Each handle has an explicit id so edges can pick the side closest to the
// neighbour. Without ids xyflow assigns positional fallbacks that don't
// route as nicely.
const HANDLE_STYLE: CSSProperties = {
  width: 6,
  height: 6,
  background: "transparent",
  border: "none",
  opacity: 0,
};

function DecisionNode({ data, selected }: NodeProps) {
  const { summary, status, priority } = data as {
    summary: string;
    status: Status;
    priority: Priority;
  };
  const truncated =
    summary.length > 60 ? summary.slice(0, 57) + "..." : summary;
  const style = nodeStyleFor({ status, priority });

  return (
    <div
      className="bg-content1 text-foreground rounded-lg px-3 py-2 shadow-md"
      style={{
        borderWidth: 2,
        borderStyle: style.dashed ? "dashed" : "solid",
        borderColor: style.ring,
        minWidth: 180,
        maxWidth: 230,
        fontSize: 12,
        boxShadow: selected
          ? `0 0 0 2px ${style.ring}, 0 6px 14px rgba(0,0,0,0.25)`
          : undefined,
      }}
    >
      {/* One source + one target on every side; ids match chooseHandlePair. */}
      <Handle id="t-top" type="target" position={Position.Top} style={HANDLE_STYLE} />
      <Handle id="t-left" type="target" position={Position.Left} style={HANDLE_STYLE} />
      <Handle id="t-bottom" type="target" position={Position.Bottom} style={HANDLE_STYLE} />
      <Handle id="t-right" type="target" position={Position.Right} style={HANDLE_STYLE} />
      <Handle id="s-top" type="source" position={Position.Top} style={HANDLE_STYLE} />
      <Handle id="s-left" type="source" position={Position.Left} style={HANDLE_STYLE} />
      <Handle id="s-bottom" type="source" position={Position.Bottom} style={HANDLE_STYLE} />
      <Handle id="s-right" type="source" position={Position.Right} style={HANDLE_STYLE} />

      <div className="font-semibold leading-tight mb-1.5 text-foreground">
        {truncated}
      </div>
      <div className="flex items-center gap-1.5">
        {/* Status chip — colour follows priority for proposed, status otherwise */}
        <span
          className="inline-flex items-center px-2 py-0.5 rounded-md text-[10px] font-bold tracking-wide uppercase"
          style={{ background: style.ring, color: "white" }}
        >
          {style.label}
        </span>
        {/* Always show the status word so colour-only is never the only signal */}
        <span className="text-[10px] text-default-500 uppercase tracking-wide">
          {humanStatus(status)}
        </span>
      </div>
    </div>
  );
}

const nodeTypes = { decision: DecisionNode };

// ---- Layout ---------------------------------------------------------------

const NODE_W = 220;
const NODE_H = 72;

/** Top-down dagre layout. Sources (no incoming `blocks`) sit at the TOP
 *  of the canvas; dependencies cascade DOWN.
 *
 *  Edge weights drive how strongly dagre tries to keep the endpoints
 *  near each other:
 *    - blocks      : weight 10 — dominant, defines the tree skeleton
 *    - supersedes  : weight  6 — pulls a superseded pair adjacent so
 *                                their edge doesn't span the canvas
 *    - influences  : weight  2 — light pull
 *    - relates_to  : skipped — pure annotation, no layout pressure
 */
function applyDagreTB(nodes: Node[], edges: Edge[]): Node[] {
  const g = new dagre.graphlib.Graph();
  g.setGraph({
    rankdir: "TB",
    nodesep: 80,
    ranksep: 110,
    edgesep: 30,
    marginx: 30,
    marginy: 30,
    acyclicer: "greedy",
    ranker: "tight-tree",
  });
  g.setDefaultEdgeLabel(() => ({}));
  for (const n of nodes) g.setNode(n.id, { width: NODE_W, height: NODE_H });
  for (const e of edges) {
    const data = e.data as { type?: string } | undefined;
    if (!data?.type) continue;
    switch (data.type) {
      case "blocks":
        g.setEdge(e.source, e.target, { weight: 10 });
        break;
      case "supersedes":
        // minlen 0 is invalid; 1 is the closest. weight gives the pull.
        g.setEdge(e.source, e.target, { weight: 6, minlen: 1 });
        break;
      case "influences":
        g.setEdge(e.source, e.target, { weight: 2, minlen: 1 });
        break;
      // relates_to intentionally skipped
    }
  }
  dagre.layout(g);
  return nodes.map((n) => {
    const p = g.node(n.id);
    return { ...n, position: { x: p.x - NODE_W / 2, y: p.y - NODE_H / 2 } };
  });
}

type HandlePair = { sourceHandle: string; targetHandle: string };

/** Pick the source/target handle pair for a TB layout.
 *
 *  - Forward (target below source, or directly below): exit BOTTOM, enter TOP.
 *  - Side (target at roughly the same row): exit/enter via horizontal sides.
 *  - Back-edge (target above source): route via sides so the line wraps
 *    around the chain instead of cutting back through nodes between them.
 */
function chooseHandlePair(source: Node, target: Node): HandlePair {
  const sx = source.position.x + NODE_W / 2;
  const sy = source.position.y + NODE_H / 2;
  const tx = target.position.x + NODE_W / 2;
  const ty = target.position.y + NODE_H / 2;
  const dx = tx - sx;
  const dy = ty - sy;

  if (dy < -NODE_H / 2) {
    if (Math.abs(dx) < NODE_W) {
      return { sourceHandle: "s-right", targetHandle: "t-right" };
    }
    return dx >= 0
      ? { sourceHandle: "s-right", targetHandle: "t-right" }
      : { sourceHandle: "s-left", targetHandle: "t-left" };
  }
  if (dy >= Math.abs(dx) * 0.6) {
    return { sourceHandle: "s-bottom", targetHandle: "t-top" };
  }
  return dx >= 0
    ? { sourceHandle: "s-right", targetHandle: "t-left" }
    : { sourceHandle: "s-left", targetHandle: "t-right" };
}

/** Hard rule: edges may not cross other nodes' bounding boxes.
 *  After the primary handle choice, scan all OTHER node rectangles and
 *  test if the straight segment from the chosen source handle to the
 *  chosen target handle clips any of them. If so, swap to a side-routed
 *  pair (left/left or right/right depending on dx) so the smoothstep
 *  curves around the cluster instead of crashing through it.
 */
function routeAroundObstacles(
  source: Node,
  target: Node,
  others: Node[],
  initial: HandlePair,
): HandlePair {
  const segment = handleSegment(source, target, initial);
  const conflict = others.some((n) =>
    n.id !== source.id &&
    n.id !== target.id &&
    segmentIntersectsRect(segment, nodeRect(n)),
  );
  if (!conflict) return initial;
  const sx = source.position.x + NODE_W / 2;
  const tx = target.position.x + NODE_W / 2;
  const dx = tx - sx;
  // Loop around the side opposite the cluster — pick the side closer to
  // canvas edge by dx sign.
  return dx >= 0
    ? { sourceHandle: "s-right", targetHandle: "t-right" }
    : { sourceHandle: "s-left", targetHandle: "t-left" };
}

function nodeRect(n: Node) {
  return {
    x1: n.position.x,
    y1: n.position.y,
    x2: n.position.x + NODE_W,
    y2: n.position.y + NODE_H,
  };
}

/** Approximate the smoothstep edge as the straight segment between the
 *  source and target handle anchor points. Good enough for clipping tests
 *  on a tidy dagre layout. */
function handleSegment(
  source: Node,
  target: Node,
  pair: HandlePair,
): { x1: number; y1: number; x2: number; y2: number } {
  return {
    x1: handlePoint(source, pair.sourceHandle).x,
    y1: handlePoint(source, pair.sourceHandle).y,
    x2: handlePoint(target, pair.targetHandle).x,
    y2: handlePoint(target, pair.targetHandle).y,
  };
}

function handlePoint(n: Node, handleId: string): { x: number; y: number } {
  const cx = n.position.x + NODE_W / 2;
  const cy = n.position.y + NODE_H / 2;
  if (handleId.endsWith("top")) return { x: cx, y: n.position.y };
  if (handleId.endsWith("bottom")) return { x: cx, y: n.position.y + NODE_H };
  if (handleId.endsWith("left")) return { x: n.position.x, y: cy };
  return { x: n.position.x + NODE_W, y: cy }; // right
}

/** Standard segment-vs-rect intersection. */
function segmentIntersectsRect(
  s: { x1: number; y1: number; x2: number; y2: number },
  r: { x1: number; y1: number; x2: number; y2: number },
): boolean {
  // Quick reject: segment bbox vs rect.
  const sxMin = Math.min(s.x1, s.x2);
  const sxMax = Math.max(s.x1, s.x2);
  const syMin = Math.min(s.y1, s.y2);
  const syMax = Math.max(s.y1, s.y2);
  if (sxMax < r.x1 || sxMin > r.x2 || syMax < r.y1 || syMin > r.y2) {
    return false;
  }
  // Endpoint inside the rect?
  if (
    pointInRect(s.x1, s.y1, r) ||
    pointInRect(s.x2, s.y2, r)
  ) {
    return true;
  }
  // Segment vs each of the four edges.
  return (
    segIntersect(s.x1, s.y1, s.x2, s.y2, r.x1, r.y1, r.x2, r.y1) ||
    segIntersect(s.x1, s.y1, s.x2, s.y2, r.x2, r.y1, r.x2, r.y2) ||
    segIntersect(s.x1, s.y1, s.x2, s.y2, r.x2, r.y2, r.x1, r.y2) ||
    segIntersect(s.x1, s.y1, s.x2, s.y2, r.x1, r.y2, r.x1, r.y1)
  );
}

function pointInRect(
  x: number,
  y: number,
  r: { x1: number; y1: number; x2: number; y2: number },
): boolean {
  return x >= r.x1 && x <= r.x2 && y >= r.y1 && y <= r.y2;
}

/** Two-segment intersection (proper, no collinear edge cases needed). */
function segIntersect(
  ax: number, ay: number, bx: number, by: number,
  cx: number, cy: number, dx: number, dy: number,
): boolean {
  const d = (bx - ax) * (dy - cy) - (by - ay) * (dx - cx);
  if (d === 0) return false;
  const t = ((cx - ax) * (dy - cy) - (cy - ay) * (dx - cx)) / d;
  const u = ((cx - ax) * (by - ay) - (cy - ay) * (bx - ax)) / d;
  return t >= 0 && t <= 1 && u >= 0 && u <= 1;
}

// ---- Build raw nodes/edges ------------------------------------------------

function buildGraph(decisions: Decision[]) {
  const ids = new Set(decisions.map((d) => d.id));
  const nodes: Node[] = decisions.map((d) => ({
    id: d.id,
    type: "decision",
    position: { x: 0, y: 0 },
    data: { summary: d.summary, status: d.status, priority: d.priority },
    draggable: false,
    connectable: false,
  }));

  const edges: Edge[] = [];
  const seen = new Set<string>();
  for (const d of decisions) {
    for (const rel of d.relationships ?? []) {
      if (!ids.has(rel.target)) continue;
      const id = `${d.id}-${rel.type}-${rel.target}`;
      if (seen.has(id)) continue;
      seen.add(id);
      const color = EDGE_COLORS[rel.type] ?? "#999";
      edges.push({
        id,
        source: d.id,
        target: rel.target,
        type: "smoothstep",
        animated: rel.type === "blocks",
        label: rel.type.replace("_", " "),
        // Stash the rel type on the edge so the layout can distinguish
        // structural edges (blocks) from soft ones.
        data: { type: rel.type },
        style: {
          stroke: color,
          strokeWidth: 2.5,
          strokeDasharray: rel.type === "relates_to" ? "6 3" : undefined,
        },
        labelStyle: { fill: color, fontSize: 11, fontWeight: 700 },
        labelBgPadding: [4, 2],
        labelBgBorderRadius: 4,
        labelBgStyle: { fill: "white", fillOpacity: 0.85 },
        markerEnd: {
          type: MarkerType.ArrowClosed,
          color,
          width: 22,
          height: 22,
        },
      });
    }
  }
  return { nodes, edges };
}

// ---- Filters --------------------------------------------------------------

const STATUS_OPTIONS: Status[] = [
  "proposed",
  "decided",
  "out_of_scope",
  "superseded",
];
const PRIORITY_OPTIONS = [
  "assumption",
  "low",
  "medium",
  "high",
  "critical",
];
const FILTERS: FilterDef[] = [
  { key: "status", label: "Status", type: "enum", options: STATUS_OPTIONS },
  {
    key: "priority",
    label: "Priority",
    type: "enum",
    options: PRIORITY_OPTIONS,
  },
  { key: "tag", label: "Tag", type: "text" },
];

function arrEq(filter: string | string[], value: string): boolean {
  if (Array.isArray(filter)) return filter.includes(value);
  return filter === value;
}

const EMPTY_LIST: Decision[] = [];

// ---- View -----------------------------------------------------------------

export default function GraphView() {
  const params = useParams<{ tree: string }>();
  const tree = params.tree ?? "";
  const openDecision = useAppStore((s) => s.openDecision);

  const [filters, setFilter, clearFilter] = useFilterParams(FILTERS);
  const filterKey = JSON.stringify(filters);

  const { data: decisionsPage } = useDecisions(tree);
  const allDecisions: Decision[] = decisionsPage?.items ?? EMPTY_LIST;

  const decisions = useMemo(() => {
    return allDecisions.filter((d) => {
      const status = filters.status;
      if (status && !arrEq(status, d.status)) return false;
      const priority = filters.priority;
      if (priority && !arrEq(priority, d.priority)) return false;
      const tag = filters.tag;
      if (tag && typeof tag === "string") {
        const tags = d.tags ?? [];
        if (!tags.some((t) => t.toLowerCase().includes(tag.toLowerCase())))
          return false;
      }
      return true;
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [allDecisions, filterKey]);

  // Single LR layout. After dagre positions things, pick the best handle
  // pair per edge so anchors point at the side facing the neighbour.
  const { nodes, edges } = useMemo(() => {
    const { nodes: raw, edges: rawEdges } = buildGraph(decisions);
    if (raw.length === 0) return { nodes: raw, edges: rawEdges };
    const placed = applyDagreTB(raw, rawEdges);
    const byId = new Map(placed.map((n) => [n.id, n]));
    const positionedEdges = rawEdges.map((e) => {
      const s = byId.get(e.source);
      const t = byId.get(e.target);
      if (!s || !t) return e;
      const initial = chooseHandlePair(s, t);
      const safe = routeAroundObstacles(s, t, placed, initial);
      return { ...e, ...safe };
    });
    return { nodes: placed, edges: positionedEdges };
  }, [decisions]);

  const onNodeClick = useCallback(
    (_: React.MouseEvent, node: Node) => openDecision(tree, node.id),
    [tree, openDecision],
  );

  const theme = useAppStore((s) => s.theme);
  const resolved =
    theme === "system"
      ? typeof window !== "undefined" &&
        window.matchMedia("(prefers-color-scheme: dark)").matches
        ? "dark"
        : "light"
      : theme;

  return (
    <div className="h-[calc(100vh-65px)] flex flex-col">
      <div className="flex items-center gap-3 px-4 py-2 border-b border-divider bg-content1 flex-wrap">
        <FilterPills
          filters={FILTERS}
          values={filters}
          onChange={(k, v) =>
            v === undefined ? clearFilter(k) : setFilter(k, v)
          }
        />
        <span className="text-xs text-default-500 ml-auto">
          {decisions.length} decision{decisions.length === 1 ? "" : "s"}
        </span>
      </div>

      <div className="flex-1 min-h-0">
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          colorMode={resolved}
          nodesDraggable={false}
          nodesConnectable={false}
          elementsSelectable
          panOnDrag
          fitView
          fitViewOptions={{ padding: 0.2 }}
          proOptions={{ hideAttribution: true }}
          onNodeClick={onNodeClick}
        >
          <Background />
          <Controls showInteractive={false} />
        </ReactFlow>
      </div>
    </div>
  );
}
