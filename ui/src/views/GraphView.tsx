import { useCallback, useEffect, useState } from "react";
import ReactFlow, {
  Background,
  Controls,
  MiniMap,
  MarkerType,
  addEdge,
  useEdgesState,
  useNodesState,
  type Node,
  type Edge,
  type NodeProps,
  type Connection,
} from "reactflow";
import dagre from "@dagrejs/dagre";
import { Button, ButtonGroup, Chip } from "@heroui/react";
import { useLocation, useParams } from "wouter";
import { useDecisions } from "@/api/query";
import type { Decision, RelationshipType, Status } from "@/api/types.gen";
import "reactflow/dist/style.css";

// ---- Status colors ----
const STATUS_COLORS: Record<Status, string> = {
  proposed: "#3b82f6",     // blue
  decided: "#22c55e",      // green
  out_of_scope: "#6b7280", // gray
  superseded: "#f97316",   // orange
};

const STATUS_BG: Record<Status, string> = {
  proposed: "#eff6ff",
  decided: "#f0fdf4",
  out_of_scope: "#f9fafb",
  superseded: "#fff7ed",
};

// ---- Edge colors ----
const EDGE_COLORS: Record<RelationshipType, string> = {
  blocks: "#ef4444",       // red
  influences: "#eab308",   // yellow
  supersedes: "#f97316",   // orange
  relates_to: "#3b82f6",   // blue
};

// ---- Custom decision node ----
function DecisionNode({ data }: NodeProps) {
  const { summary, status } = data as { summary: string; status: Status };
  const truncated =
    summary.length > 60 ? summary.slice(0, 57) + "..." : summary;

  return (
    <div
      style={{
        background: STATUS_BG[status] ?? "#fff",
        border: `2px solid ${STATUS_COLORS[status] ?? "#999"}`,
        borderRadius: 8,
        padding: "8px 12px",
        minWidth: 160,
        maxWidth: 220,
        fontSize: 12,
        cursor: "pointer",
      }}
    >
      <div
        style={{
          marginBottom: 4,
          fontWeight: 600,
          color: "#1a1a1a",
          lineHeight: 1.3,
        }}
      >
        {truncated}
      </div>
      <Chip
        size="sm"
        style={{
          background: STATUS_COLORS[status],
          color: "#fff",
          fontSize: 10,
          height: 18,
          minHeight: 18,
        }}
      >
        {status.replace("_", " ")}
      </Chip>
    </div>
  );
}

const nodeTypes = { decision: DecisionNode };

// ---- Dagre layout helper ----
type Direction = "TB" | "LR";

function computeLayout(
  nodes: Node[],
  edges: Edge[],
  direction: Direction,
): { nodes: Node[]; edges: Edge[] } {
  const g = new dagre.graphlib.Graph();
  g.setGraph({ rankdir: direction, nodesep: 60, ranksep: 80 });
  g.setDefaultEdgeLabel(() => ({}));

  const NODE_W = 220;
  const NODE_H = 72;

  nodes.forEach((n) => g.setNode(n.id, { width: NODE_W, height: NODE_H }));
  edges.forEach((e) => g.setEdge(e.source, e.target));

  dagre.layout(g);

  const laid = nodes.map((n) => {
    const pos = g.node(n.id);
    return {
      ...n,
      position: { x: pos.x - NODE_W / 2, y: pos.y - NODE_H / 2 },
    };
  });

  return { nodes: laid, edges };
}

// ---- Build RF nodes/edges from decisions ----
function buildGraph(
  decisions: Decision[],
): { nodes: Node[]; edges: Edge[] } {
  const nodes: Node[] = decisions.map((d) => ({
    id: d.id,
    type: "decision",
    position: { x: 0, y: 0 },
    data: { summary: d.summary, status: d.status, id: d.id },
  }));

  const edges: Edge[] = [];
  const seen = new Set<string>();

  decisions.forEach((d) => {
    (d.relationships ?? []).forEach((rel) => {
      const edgeId = `${d.id}-${rel.type}-${rel.target}`;
      if (seen.has(edgeId)) return;
      seen.add(edgeId);

      const color = EDGE_COLORS[rel.type] ?? "#999";
      edges.push({
        id: edgeId,
        source: d.id,
        target: rel.target,
        label: rel.type.replace("_", " "),
        style: {
          stroke: color,
          strokeDasharray: rel.type === "relates_to" ? "6 3" : undefined,
        },
        labelStyle: { fill: color, fontSize: 10, fontWeight: 600 },
        markerEnd: {
          type: MarkerType.ArrowClosed,
          color,
        },
      });
    });
  });

  return { nodes, edges };
}

// ---- GraphView ----
export default function GraphView() {
  const params = useParams<{ tree: string }>();
  const tree = params.tree ?? "";
  const [, setLocation] = useLocation();

  const [direction, setDirection] = useState<Direction | "free">("TB");
  const [nodes, setNodes, onNodesChange] = useNodesState([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState([]);

  const { data: decisionsPage } = useDecisions(tree);
  const decisions = decisionsPage?.items ?? [];

  // Rebuild graph whenever data or direction changes
  useEffect(() => {
    if (decisions.length === 0) {
      setNodes([]);
      setEdges([]);
      return;
    }
    const { nodes: raw, edges: rawEdges } = buildGraph(decisions);
    if (direction === "free") {
      // No dagre layout — just spread manually
      const spread = raw.map((n, i) => ({
        ...n,
        position: { x: (i % 5) * 260, y: Math.floor(i / 5) * 120 },
      }));
      setNodes(spread);
      setEdges(rawEdges);
    } else {
      const { nodes: laid, edges: laidEdges } = computeLayout(
        raw,
        rawEdges,
        direction,
      );
      setNodes(laid);
      setEdges(laidEdges);
    }
  }, [decisions, direction, setNodes, setEdges]);

  const onConnect = useCallback(
    (connection: Connection) => setEdges((eds) => addEdge(connection, eds)),
    [setEdges],
  );

  const onNodeClick = useCallback(
    (_: React.MouseEvent, node: Node) => {
      setLocation(`/trees/${tree}/decisions/${node.id}`);
    },
    [tree, setLocation],
  );

  // Filter pills placeholder — component may not exist yet
  const FilterPillsEl = <div className="text-xs text-gray-400">filters TBD</div>;

  return (
    <div style={{ width: "100%", height: "calc(100vh - 64px)", display: "flex", flexDirection: "column" }}>
      {/* Toolbar */}
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 12,
          padding: "8px 16px",
          borderBottom: "1px solid #e5e7eb",
          background: "#fff",
          flexShrink: 0,
        }}
      >
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
        {FilterPillsEl}
      </div>

      {/* Canvas */}
      <div style={{ flex: 1 }}>
        <ReactFlow
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onConnect={onConnect}
          onNodeClick={onNodeClick}
          nodeTypes={nodeTypes}
          fitView
          fitViewOptions={{ padding: 0.2 }}
        >
          <Background />
          <MiniMap />
          <Controls />
        </ReactFlow>
      </div>
    </div>
  );
}
