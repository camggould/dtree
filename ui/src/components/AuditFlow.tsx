import { useMemo } from "react";
import ReactFlow, {
  Background,
  MarkerType,
  type Node,
  type Edge,
} from "reactflow";
import { Chip, Spinner } from "@heroui/react";
import { formatDistanceToNow } from "date-fns";
import { useHistory } from "@/api/query";
import type { Event, Action } from "@/api/types.gen";
import "reactflow/dist/style.css";

// ---- Action chip color ----
const ACTION_COLORS: Partial<Record<Action, string>> = {
  create: "#22c55e",
  update: "#3b82f6",
  delete: "#ef4444",
  decide: "#8b5cf6",
  undecide: "#f97316",
  scope_out: "#6b7280",
  supersede: "#f97316",
  relate: "#0ea5e9",
  unrelate: "#64748b",
  restore: "#22c55e",
};

function actionColor(action: Action): string {
  return ACTION_COLORS[action] ?? "#94a3b8";
}

// ---- Custom event node ----
interface EventNodeData {
  action: Action;
  actor: string;
  ts: string;
}

function EventNode({ data }: { data: EventNodeData }) {
  const color = actionColor(data.action);
  const rel = formatDistanceToNow(new Date(data.ts), { addSuffix: true });

  return (
    <div
      style={{
        background: "#fff",
        border: `2px solid ${color}`,
        borderRadius: 8,
        padding: "6px 10px",
        minWidth: 140,
        fontSize: 11,
        display: "flex",
        flexDirection: "column",
        gap: 4,
      }}
    >
      <Chip
        size="sm"
        style={{
          background: color,
          color: "#fff",
          fontSize: 10,
          height: 18,
          minHeight: 18,
          alignSelf: "flex-start",
        }}
      >
        {data.action.replace("_", " ")}
      </Chip>
      <span style={{ color: "#374151", fontWeight: 600 }}>{data.actor}</span>
      <span style={{ color: "#9ca3af" }}>{rel}</span>
    </div>
  );
}

const nodeTypes = { event: EventNode };

// ---- Build graph from events ----
function buildAuditGraph(events: Event[]): { nodes: Node[]; edges: Edge[] } {
  const sorted = [...events].sort(
    (a, b) => new Date(a.ts).getTime() - new Date(b.ts).getTime(),
  );

  const NODE_W = 160;
  const GAP = 60;

  const nodes: Node[] = sorted.map((ev, i) => ({
    id: ev.event_id,
    type: "event",
    position: { x: i * (NODE_W + GAP), y: 0 },
    data: { action: ev.action, actor: ev.actor, ts: ev.ts },
  }));

  const edges: Edge[] = sorted.slice(1).map((ev, i) => ({
    id: `edge-${i}`,
    source: sorted[i].event_id,
    target: ev.event_id,
    markerEnd: { type: MarkerType.ArrowClosed },
    style: { stroke: "#94a3b8" },
  }));

  return { nodes, edges };
}

// ---- AuditFlow ----
export interface AuditFlowProps {
  tree: string;
  id: string;
}

export default function AuditFlow({ tree, id }: AuditFlowProps) {
  const { data, isLoading, isError } = useHistory(tree, id);

  const { nodes, edges } = useMemo(() => {
    if (!data?.events?.length) return { nodes: [], edges: [] };
    return buildAuditGraph(data.events);
  }, [data]);

  if (isLoading) {
    return (
      <div style={{ display: "flex", justifyContent: "center", padding: 32 }}>
        <Spinner />
      </div>
    );
  }

  if (isError) {
    return (
      <div style={{ padding: 16, color: "#ef4444", fontSize: 13 }}>
        Failed to load audit history.
      </div>
    );
  }

  if (!nodes.length) {
    return (
      <div style={{ padding: 16, color: "#9ca3af", fontSize: 13 }}>
        No audit events found.
      </div>
    );
  }

  return (
    <div style={{ width: "100%", height: 200 }}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        fitView
        fitViewOptions={{ padding: 0.3 }}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable={false}
        zoomOnScroll={false}
        panOnScroll
      >
        <Background />
      </ReactFlow>
    </div>
  );
}
