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

const STATUS_PALETTE: Record<Status, { ring: string; chipBg: string }> = {
  proposed: { ring: "#1d4ed8", chipBg: "#1d4ed8" },
  decided: { ring: "#15803d", chipBg: "#15803d" },
  out_of_scope: { ring: "#475569", chipBg: "#475569" },
  superseded: { ring: "#c2410c", chipBg: "#c2410c" },
};

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
  const { summary, status } = data as { summary: string; status: Status };
  const truncated =
    summary.length > 60 ? summary.slice(0, 57) + "..." : summary;
  const palette = STATUS_PALETTE[status] ?? STATUS_PALETTE.proposed;

  return (
    <div
      className="bg-content1 text-foreground border-2 rounded-lg px-3 py-2 shadow-md"
      style={{
        borderColor: palette.ring,
        minWidth: 180,
        maxWidth: 230,
        fontSize: 12,
        boxShadow: selected
          ? `0 0 0 2px ${palette.ring}, 0 6px 14px rgba(0,0,0,0.25)`
          : undefined,
      }}
    >
      {/* One source + one target on every side; ids match the names used by
          chooseHandlePair below to pick the closest sides per edge. */}
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
      <span
        className="inline-flex items-center px-2 py-0.5 rounded-md text-[10px] font-bold tracking-wide uppercase"
        style={{ background: palette.chipBg, color: "white" }}
      >
        {humanStatus(status)}
      </span>
    </div>
  );
}

const nodeTypes = { decision: DecisionNode };

// ---- Layout ---------------------------------------------------------------

const NODE_W = 220;
const NODE_H = 72;

/** Run dagre LR with sources (no incoming `blocks`) on the left.
 *  Returns nodes with computed centre positions; edges untouched.
 */
function applyDagreLR(nodes: Node[], edges: Edge[]): Node[] {
  const g = new dagre.graphlib.Graph();
  g.setGraph({ rankdir: "LR", nodesep: 50, ranksep: 90, edgesep: 20 });
  g.setDefaultEdgeLabel(() => ({}));
  for (const n of nodes) g.setNode(n.id, { width: NODE_W, height: NODE_H });
  for (const e of edges) g.setEdge(e.source, e.target);
  dagre.layout(g);
  return nodes.map((n) => {
    const p = g.node(n.id);
    return { ...n, position: { x: p.x - NODE_W / 2, y: p.y - NODE_H / 2 } };
  });
}

/** Pick the source/target handle pair that minimises the path. We compare
 *  the centre-to-centre vector and choose the dominant axis: if Δx
 *  dominates, route through left/right sides; if Δy dominates, route
 *  through top/bottom. Result: the edge always exits and enters from the
 *  side closest to the other node, which reads as a tidy tree.
 */
function chooseHandlePair(
  source: Node,
  target: Node,
): { sourceHandle: string; targetHandle: string } {
  const sx = source.position.x + NODE_W / 2;
  const sy = source.position.y + NODE_H / 2;
  const tx = target.position.x + NODE_W / 2;
  const ty = target.position.y + NODE_H / 2;
  const dx = tx - sx;
  const dy = ty - sy;
  if (Math.abs(dx) >= Math.abs(dy)) {
    return dx >= 0
      ? { sourceHandle: "s-right", targetHandle: "t-left" }
      : { sourceHandle: "s-left", targetHandle: "t-right" };
  }
  return dy >= 0
    ? { sourceHandle: "s-bottom", targetHandle: "t-top" }
    : { sourceHandle: "s-top", targetHandle: "t-bottom" };
}

// ---- Build raw nodes/edges ------------------------------------------------

function buildGraph(decisions: Decision[]) {
  const ids = new Set(decisions.map((d) => d.id));
  const nodes: Node[] = decisions.map((d) => ({
    id: d.id,
    type: "decision",
    position: { x: 0, y: 0 },
    data: { summary: d.summary, status: d.status },
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
    const placed = applyDagreLR(raw, rawEdges);
    const byId = new Map(placed.map((n) => [n.id, n]));
    const positionedEdges = rawEdges.map((e) => {
      const s = byId.get(e.source);
      const t = byId.get(e.target);
      if (!s || !t) return e;
      return { ...e, ...chooseHandlePair(s, t) };
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
