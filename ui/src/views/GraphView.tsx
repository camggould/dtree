import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type CSSProperties,
} from "react";
import {
  ReactFlow,
  Background,
  Controls,
  MarkerType,
  Handle,
  Position,
  BaseEdge,
  type Node,
  type Edge,
  type NodeProps,
  type EdgeProps,
} from "@xyflow/react";
import ELK, { type ElkNode, type ElkExtendedEdge } from "elkjs/lib/elk.bundled.js";
import { Spinner } from "@heroui/react";
import { useParams } from "wouter";
import { useDecisions } from "@/api/query";
import { useAppStore } from "@/store/app";
import { humanStatus } from "@/util/labels";
import {
  FilterPills,
  useFilterParams,
  type FilterDef,
} from "@/components/FilterPills";
import type {
  Decision,
  RelationshipType,
  Status,
  Priority,
} from "@/api/types.gen";
import "@xyflow/react/dist/style.css";

// ---------------------------------------------------------------------------
// Visuals (palette + node)
// ---------------------------------------------------------------------------

const PRIORITY_PALETTE: Record<
  Exclude<Priority, "assumption">,
  { ring: string; label: string }
> = {
  critical: { ring: "#b91c1c", label: "Critical" },
  high: { ring: "#ea580c", label: "High" },
  medium: { ring: "#1d4ed8", label: "Medium" },
  low: { ring: "#475569", label: "Low" },
};
const ASSUMPTION_STYLE = {
  ring: "#94a3b8",
  label: "Assumption",
  dashed: true,
};
const STATUS_PALETTE_NON_PROPOSED: Record<
  Exclude<Status, "proposed">,
  { ring: string; label: string; dashed?: boolean }
> = {
  decided: { ring: "#15803d", label: "Decided" },
  out_of_scope: { ring: "#94a3b8", label: "Out of scope", dashed: true },
  superseded: { ring: "#a16207", label: "Superseded", dashed: true },
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
  return {
    ring: s.ring,
    label: s.label.toUpperCase(),
    dashed: s.dashed ?? false,
  };
}

const EDGE_COLORS: Record<RelationshipType, string> = {
  blocks: "#dc2626",
  influences: "#ca8a04",
  supersedes: "#ea580c",
  relates_to: "#2563eb",
};

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
      {/* All four sides expose source + target so ELK can pick whichever
          side it routed each edge through. */}
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
        <span
          className="inline-flex items-center px-2 py-0.5 rounded-md text-[10px] font-bold tracking-wide uppercase"
          style={{ background: style.ring, color: "white" }}
        >
          {style.label}
        </span>
        <span className="text-[10px] text-default-500 uppercase tracking-wide">
          {humanStatus(status)}
        </span>
      </div>
    </div>
  );
}

const nodeTypes = { decision: DecisionNode };

// ---------------------------------------------------------------------------
// Custom edge: render the polyline that ELK computed.
// ---------------------------------------------------------------------------

interface ELKWaypointEdgeData extends Record<string, unknown> {
  points: Array<{ x: number; y: number }>;
  color: string;
  label?: string;
  dashed?: boolean;
}

/** Build an SVG path from ELK's waypoints. ELK's edge sections look like:
 *    { startPoint: {x,y}, bendPoints: [{x,y}...], endPoint: {x,y} }
 *  We pre-flatten that to a single `points` array on the edge data, then
 *  emit a polyline with rounded corners (cheap orthogonal smoothing) and
 *  attach the marker arrow at the end. */
function pathFromPoints(
  points: Array<{ x: number; y: number }>,
  cornerRadius = 8,
): string {
  if (points.length === 0) return "";
  if (points.length === 1) return `M ${points[0].x} ${points[0].y}`;
  if (points.length === 2) {
    return `M ${points[0].x} ${points[0].y} L ${points[1].x} ${points[1].y}`;
  }
  // Rounded-corner polyline: line to (corner-radius) before each waypoint,
  // quadratic-curve through the waypoint to the same offset on the next leg.
  let d = `M ${points[0].x} ${points[0].y}`;
  for (let i = 1; i < points.length - 1; i++) {
    const prev = points[i - 1];
    const curr = points[i];
    const next = points[i + 1];
    const inDir = unit(sub(curr, prev));
    const outDir = unit(sub(next, curr));
    const r = Math.min(
      cornerRadius,
      dist(prev, curr) / 2,
      dist(curr, next) / 2,
    );
    const before = { x: curr.x - inDir.x * r, y: curr.y - inDir.y * r };
    const after = { x: curr.x + outDir.x * r, y: curr.y + outDir.y * r };
    d += ` L ${before.x} ${before.y}`;
    d += ` Q ${curr.x} ${curr.y} ${after.x} ${after.y}`;
  }
  const last = points[points.length - 1];
  d += ` L ${last.x} ${last.y}`;
  return d;
}

function sub(a: { x: number; y: number }, b: { x: number; y: number }) {
  return { x: a.x - b.x, y: a.y - b.y };
}
function dist(a: { x: number; y: number }, b: { x: number; y: number }) {
  return Math.hypot(a.x - b.x, a.y - b.y);
}
function unit(v: { x: number; y: number }) {
  const m = Math.hypot(v.x, v.y) || 1;
  return { x: v.x / m, y: v.y / m };
}

function ELKWaypointEdge({ id, data, markerEnd }: EdgeProps) {
  const d = data as unknown as ELKWaypointEdgeData;
  if (!d?.points?.length) return null;
  const path = pathFromPoints(d.points);
  return (
    <>
      <BaseEdge
        id={id}
        path={path}
        markerEnd={markerEnd}
        style={{
          stroke: d.color,
          strokeWidth: 2.5,
          strokeDasharray: d.dashed ? "6 3" : undefined,
          fill: "none",
        }}
      />
      {d.label && d.points.length >= 2 && (
        <EdgeLabel points={d.points} color={d.color} text={d.label} />
      )}
    </>
  );
}

function EdgeLabel({
  points,
  color,
  text,
}: {
  points: Array<{ x: number; y: number }>;
  color: string;
  text: string;
}) {
  // Place label at the midpoint of the polyline (by arc length).
  const mid = midpointAlongPath(points);
  return (
    <g transform={`translate(${mid.x}, ${mid.y})`} pointerEvents="none">
      <rect
        x={-text.length * 3.3 - 4}
        y={-8}
        width={text.length * 6.6 + 8}
        height={16}
        rx={3}
        ry={3}
        fill="white"
        opacity={0.85}
      />
      <text
        textAnchor="middle"
        dy="0.32em"
        style={{ fill: color, fontSize: 11, fontWeight: 700 }}
      >
        {text}
      </text>
    </g>
  );
}

function midpointAlongPath(points: Array<{ x: number; y: number }>): {
  x: number;
  y: number;
} {
  let total = 0;
  for (let i = 0; i < points.length - 1; i++) {
    total += dist(points[i], points[i + 1]);
  }
  let target = total / 2;
  for (let i = 0; i < points.length - 1; i++) {
    const seg = dist(points[i], points[i + 1]);
    if (target <= seg) {
      const t = target / seg;
      return {
        x: points[i].x + (points[i + 1].x - points[i].x) * t,
        y: points[i].y + (points[i + 1].y - points[i].y) * t,
      };
    }
    target -= seg;
  }
  return points[points.length - 1];
}

const edgeTypes = { elkRoute: ELKWaypointEdge };

// ---------------------------------------------------------------------------
// ELK layout
// ---------------------------------------------------------------------------

const NODE_W = 220;
const NODE_H = 72;
const elk = new ELK();

interface LayoutResult {
  nodes: Node[];
  edges: Edge[];
}

const EMPTY_LAYOUT: LayoutResult = { nodes: [], edges: [] };

/** Run ELK with the layered algorithm + orthogonal edge routing. ELK
 *  produces collision-free routes wherever the graph is planar enough;
 *  for non-planar bits it still routes through computed channels rather
 *  than cutting through node boxes.
 *
 *  Edge weights (via `priority`) tune how aggressively ELK pulls related
 *  nodes together. blocks dominates so the rank skeleton stays clean.
 */
async function layoutWithELK(decisions: Decision[]): Promise<LayoutResult> {
  if (decisions.length === 0) return EMPTY_LAYOUT;

  const ids = new Set(decisions.map((d) => d.id));
  const elkNodes: ElkNode[] = decisions.map((d) => ({
    id: d.id,
    width: NODE_W,
    height: NODE_H,
  }));

  const elkEdges: ElkExtendedEdge[] = [];
  const seen = new Set<string>();
  for (const d of decisions) {
    for (const rel of d.relationships ?? []) {
      if (!ids.has(rel.target)) continue;
      const id = `${d.id}-${rel.type}-${rel.target}`;
      if (seen.has(id)) continue;
      seen.add(id);
      const priority =
        rel.type === "blocks"
          ? 10
          : rel.type === "supersedes"
            ? 6
            : rel.type === "influences"
              ? 2
              : 1;
      elkEdges.push({
        id,
        sources: [d.id],
        targets: [rel.target],
        layoutOptions: {
          "elk.layered.priority.direction": String(priority),
        },
      });
    }
  }

  const graph: ElkNode = {
    id: "root",
    layoutOptions: {
      "elk.algorithm": "layered",
      "elk.direction": "DOWN",
      "elk.edgeRouting": "ORTHOGONAL",
      "elk.spacing.nodeNode": "60",
      "elk.layered.spacing.nodeNodeBetweenLayers": "100",
      "elk.layered.spacing.edgeNodeBetweenLayers": "30",
      "elk.layered.spacing.edgeEdgeBetweenLayers": "20",
      "elk.spacing.edgeNode": "20",
      "elk.spacing.edgeEdge": "15",
      "elk.layered.crossingMinimization.strategy": "LAYER_SWEEP",
      "elk.layered.nodePlacement.strategy": "NETWORK_SIMPLEX",
      "elk.layered.cycleBreaking.strategy": "GREEDY",
      "elk.layered.considerModelOrder.strategy": "PREFER_EDGES",
    },
    children: elkNodes,
    edges: elkEdges,
  };

  const out = await elk.layout(graph);
  const positions = new Map<string, { x: number; y: number }>();
  for (const c of out.children ?? []) {
    positions.set(c.id, { x: c.x ?? 0, y: c.y ?? 0 });
  }

  const flowNodes: Node[] = decisions.map((d) => {
    const pos = positions.get(d.id) ?? { x: 0, y: 0 };
    return {
      id: d.id,
      type: "decision",
      position: pos,
      data: { summary: d.summary, status: d.status, priority: d.priority },
      draggable: false,
      connectable: false,
    };
  });

  // Recover decision -> rel-type lookup for styling.
  const relByEdgeId = new Map<string, RelationshipType>();
  for (const d of decisions) {
    for (const rel of d.relationships ?? []) {
      relByEdgeId.set(`${d.id}-${rel.type}-${rel.target}`, rel.type);
    }
  }

  const flowEdges: Edge[] = (out.edges ?? []).map((e) => {
    const relType = relByEdgeId.get(e.id) ?? "relates_to";
    const color = EDGE_COLORS[relType] ?? "#999";
    const points = elkEdgeToPoints(e);
    return {
      id: e.id,
      source: e.sources?.[0] ?? "",
      target: e.targets?.[0] ?? "",
      type: "elkRoute",
      animated: relType === "blocks",
      data: {
        points,
        color,
        label: relType.replace("_", " "),
        dashed: relType === "relates_to",
      },
      markerEnd: {
        type: MarkerType.ArrowClosed,
        color,
        width: 22,
        height: 22,
      },
    };
  });

  return { nodes: flowNodes, edges: flowEdges };
}

/** Flatten ELK's edge sections into a single waypoints list that includes
 *  the start, all bends, and the end. */
function elkEdgeToPoints(
  e: ElkExtendedEdge,
): Array<{ x: number; y: number }> {
  const points: Array<{ x: number; y: number }> = [];
  for (const sec of e.sections ?? []) {
    points.push({ x: sec.startPoint.x, y: sec.startPoint.y });
    for (const b of sec.bendPoints ?? []) {
      points.push({ x: b.x, y: b.y });
    }
    points.push({ x: sec.endPoint.x, y: sec.endPoint.y });
  }
  return points;
}

// ---------------------------------------------------------------------------
// Filters
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

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

  const [layout, setLayout] = useState<LayoutResult>(EMPTY_LAYOUT);
  const [layingOut, setLayingOut] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setLayingOut(true);
    layoutWithELK(decisions)
      .then((result) => {
        if (!cancelled) {
          setLayout(result);
          setLayingOut(false);
        }
      })
      .catch((err) => {
        // eslint-disable-next-line no-console
        console.error("ELK layout failed:", err);
        if (!cancelled) {
          setLayout({ nodes: [], edges: [] });
          setLayingOut(false);
        }
      });
    return () => {
      cancelled = true;
    };
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
          {layingOut && " · laying out…"}
        </span>
      </div>

      <div className="flex-1 min-h-0 relative">
        {layingOut && layout.nodes.length === 0 && (
          <div className="absolute inset-0 flex items-center justify-center">
            <Spinner />
          </div>
        )}
        <ReactFlow
          nodes={layout.nodes}
          edges={layout.edges}
          nodeTypes={nodeTypes}
          edgeTypes={edgeTypes}
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
