import { useCallback, useMemo, useState } from "react";
import {
  ReactFlow,
  Background,
  Controls,
  MarkerType,
  type Node,
  type Edge,
  type NodeProps,
} from "@xyflow/react";
import dagre from "@dagrejs/dagre";
import { Button, ButtonGroup, Chip } from "@heroui/react";
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

const STATUS_RING: Record<Status, string> = {
  proposed: "rgb(59 130 246)",
  decided: "rgb(34 197 94)",
  out_of_scope: "rgb(107 114 128)",
  superseded: "rgb(249 115 22)",
};

const EDGE_COLORS: Record<RelationshipType, string> = {
  blocks: "#ef4444",
  influences: "#eab308",
  supersedes: "#f97316",
  relates_to: "#3b82f6",
};

function DecisionNode({ data, selected }: NodeProps) {
  const { summary, status } = data as { summary: string; status: Status };
  const truncated =
    summary.length > 60 ? summary.slice(0, 57) + "..." : summary;
  const ring = STATUS_RING[status] ?? "#999";

  return (
    <div
      className="bg-content1 text-foreground border-2 rounded-lg px-3 py-2 shadow-sm"
      style={{
        borderColor: ring,
        minWidth: 170,
        maxWidth: 220,
        fontSize: 12,
        boxShadow: selected
          ? `0 0 0 2px ${ring}, 0 4px 12px rgba(0,0,0,0.15)`
          : undefined,
      }}
    >
      <div className="font-semibold leading-tight mb-1">{truncated}</div>
      <Chip
        size="sm"
        variant="flat"
        style={{
          background: ring,
          color: "white",
          fontSize: 10,
          height: 18,
          minHeight: 18,
        }}
      >
        {humanStatus(status)}
      </Chip>
    </div>
  );
}

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
        type: "default",
        animated: rel.type === "blocks",
        label: rel.type.replace("_", " "),
        style: {
          stroke: color,
          strokeWidth: 2,
          strokeDasharray: rel.type === "relates_to" ? "6 3" : undefined,
        },
        labelStyle: { fill: color, fontSize: 10, fontWeight: 600 },
        labelBgStyle: { fill: "var(--heroui-content1)", opacity: 0.9 },
        markerEnd: {
          type: MarkerType.ArrowClosed,
          color,
          width: 18,
          height: 18,
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
