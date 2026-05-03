import { useCallback, useMemo, useState, type CSSProperties } from "react";
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
import { Button, ButtonGroup } from "@heroui/react";
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

// Higher-contrast ring/chip colors. Each status has a strong border + a
// chip with white bold text on the same color — readable on light + dark
// without further tweaking.
const STATUS_PALETTE: Record<Status, { ring: string; chipBg: string }> = {
  proposed:     { ring: "#1d4ed8", chipBg: "#1d4ed8" }, // blue-700
  decided:      { ring: "#15803d", chipBg: "#15803d" }, // green-700
  out_of_scope: { ring: "#475569", chipBg: "#475569" }, // slate-600
  superseded:   { ring: "#c2410c", chipBg: "#c2410c" }, // orange-700
};

const EDGE_COLORS: Record<RelationshipType, string> = {
  blocks:     "#dc2626", // red-600
  influences: "#ca8a04", // yellow-600
  supersedes: "#ea580c", // orange-600
  relates_to: "#2563eb", // blue-600
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
      {/* Handles are REQUIRED for custom nodes — edges anchor to them.
          We expose source AND target on every side so any layout direction
          (TB/LR/free) routes edges sensibly without per-direction wiring. */}
      <Handle type="target" position={Position.Top} style={HANDLE_STYLE} />
      <Handle type="target" position={Position.Left} style={HANDLE_STYLE} />
      <Handle type="source" position={Position.Bottom} style={HANDLE_STYLE} />
      <Handle type="source" position={Position.Right} style={HANDLE_STYLE} />

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

// Tiny invisible handle — doesn't show, but anchors edges so they can route.
const HANDLE_STYLE: CSSProperties = {
  width: 6,
  height: 6,
  background: "transparent",
  border: "none",
  opacity: 0,
};

const nodeTypes = { decision: DecisionNode };

type Direction = "TB" | "LR" | "free";

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
        labelStyle: {
          fill: color,
          fontSize: 11,
          fontWeight: 700,
        },
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

function applyDagre(
  nodes: Node[],
  edges: Edge[],
  direction: "TB" | "LR",
): Node[] {
  const g = new dagre.graphlib.Graph();
  g.setGraph({ rankdir: direction, nodesep: 60, ranksep: 80 });
  g.setDefaultEdgeLabel(() => ({}));
  const W = 220,
    H = 72;
  for (const n of nodes) g.setNode(n.id, { width: W, height: H });
  for (const e of edges) g.setEdge(e.source, e.target);
  dagre.layout(g);
  return nodes.map((n) => {
    const p = g.node(n.id);
    return { ...n, position: { x: p.x - W / 2, y: p.y - H / 2 } };
  });
}

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

export default function GraphView() {
  const params = useParams<{ tree: string }>();
  const tree = params.tree ?? "";
  const openDecision = useAppStore((s) => s.openDecision);

  const [direction, setDirection] = useState<Direction>("TB");
  const [filters, setFilter, clearFilter] = useFilterParams(FILTERS);

  // Stable filter snapshot (string-key) so memo deps don't fire spuriously.
  const filterKey = JSON.stringify(filters);

  const { data: decisionsPage } = useDecisions(tree);

  // Stabilise the items reference: React Query returns the same array between
  // renders when data is unchanged, but `?? []` was creating a new [] each
  // time when data was undefined.
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

  // ----- DERIVED nodes/edges (no setState — kills the React #185 loop) -----
  const { nodes, edges } = useMemo(() => {
    const { nodes: raw, edges } = buildGraph(decisions);
    if (raw.length === 0) return { nodes: raw, edges };
    if (direction === "free") {
      return {
        nodes: raw.map((n, i) => ({
          ...n,
          position: { x: (i % 5) * 260, y: Math.floor(i / 5) * 130 },
        })),
        edges,
      };
    }
    return { nodes: applyDagre(raw, edges, direction), edges };
  }, [decisions, direction]);

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
        <ButtonGroup size="sm" variant="bordered">
          <Button
            color={direction === "TB" ? "primary" : "default"}
            onPress={() => setDirection("TB")}
          >
            Top→Bottom
          </Button>
          <Button
            color={direction === "LR" ? "primary" : "default"}
            onPress={() => setDirection("LR")}
          >
            Left→Right
          </Button>
          <Button
            color={direction === "free" ? "primary" : "default"}
            onPress={() => setDirection("free")}
          >
            Free
          </Button>
        </ButtonGroup>
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

const EMPTY_LIST: Decision[] = [];
