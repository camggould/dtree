import { useMemo, type CSSProperties } from "react";
import {
  ReactFlow,
  Background,
  MarkerType,
  Handle,
  Position,
  type Node,
  type Edge,
  type NodeProps,
} from "@xyflow/react";
import { Chip, Spinner } from "@heroui/react";
import { formatDistanceToNow } from "date-fns";
import { useHistory, useActors } from "@/api/query";
import { useAppStore } from "@/store/app";
import { humanAction, truncate } from "@/util/labels";
import type { Event, Action, Decision, Actor } from "@/api/types.gen";
import "@xyflow/react/dist/style.css";

const ACTION_COLORS: Partial<Record<Action, string>> = {
  create:    "#15803d", // green-700
  update:    "#1d4ed8", // blue-700
  delete:    "#b91c1c", // red-700
  decide:    "#7c3aed", // violet-600
  undecide:  "#c2410c", // orange-700
  scope_out: "#475569", // slate-600
  supersede: "#c2410c", // orange-700
  relate:    "#0369a1", // sky-700
  unrelate:  "#475569",
  restore:   "#15803d",
};
function actionColor(a: Action): string {
  return ACTION_COLORS[a] ?? "#64748b";
}

interface EventNodeData {
  action: Action;
  actor: string;
  actorKind?: "human" | "agent";
  ts: string;
  primary: string | null; // bold one-line summary
  secondary: string | null; // dim line below
}

function EventNode({ data }: NodeProps) {
  const d = data as unknown as EventNodeData;
  const color = actionColor(d.action);
  const rel = formatDistanceToNow(new Date(d.ts), { addSuffix: true });

  return (
    <div
      className="bg-content1 text-foreground border-2 rounded-lg shadow-md"
      style={{
        borderColor: color,
        padding: "8px 12px",
        minWidth: 220,
        maxWidth: 280,
        fontSize: 11,
        display: "flex",
        flexDirection: "column",
        gap: 6,
      }}
    >
      {/* Audit flow is laid out left-to-right; only horizontal handles needed. */}
      <Handle type="target" position={Position.Left} style={HANDLE_STYLE} />
      <Handle type="source" position={Position.Right} style={HANDLE_STYLE} />
      <div className="flex items-center gap-2 flex-wrap">
        <span
          className="inline-flex items-center px-2 py-0.5 rounded-md text-[10px] font-bold uppercase tracking-wide"
          style={{ background: color, color: "white" }}
        >
          {humanAction(d.action)}
        </span>
        <span className="text-foreground font-semibold">{d.actor}</span>
        {d.actorKind && (
          <Chip
            size="sm"
            variant="flat"
            color={d.actorKind === "agent" ? "secondary" : "primary"}
            className="h-4 text-[9px]"
          >
            {d.actorKind}
          </Chip>
        )}
      </div>
      {d.primary && (
        <div className="text-foreground text-[12px] leading-snug">
          {d.primary}
        </div>
      )}
      {d.secondary && (
        <div className="text-default-500 text-[10px] leading-snug">
          {d.secondary}
        </div>
      )}
      <div className="text-default-400 text-[10px]">{rel}</div>
    </div>
  );
}

const nodeTypes = { event: EventNode };

const HANDLE_STYLE: CSSProperties = {
  width: 6,
  height: 6,
  background: "transparent",
  border: "none",
  opacity: 0,
};

// Cap for inline reasoning quotes in audit-flow nodes.
const REASON_CAP = 200;

function describePayload(
  ev: Event,
  decision?: Decision | null,
): { primary: string | null; secondary: string | null } {
  const after = (ev.payload?.after ?? {}) as Record<string, unknown>;
  if (ev.action === "decide") {
    const choice =
      (after.actual_choice as string | undefined) ?? decision?.actual_choice;
    if (!choice) return { primary: null, secondary: null };
    const isRec = after.is_recommended as boolean | undefined;
    const recAfter = after.recommended_summary as string | undefined;
    const recommended = recAfter ?? decision?.recommended_summary;
    const reasonRaw =
      (after.actual_choice_reason as string | undefined) ??
      decision?.actual_choice_reason ??
      null;
    let head: string;
    if (isRec === true || (recommended && choice === recommended)) {
      head = `Followed recommendation: “${choice}”`;
    } else if (recommended) {
      head = `Overrode “${recommended}” → “${choice}”`;
    } else {
      head = `Decided: “${choice}”`;
    }
    return {
      primary: head,
      secondary: reasonRaw ? `“${truncate(reasonRaw, REASON_CAP)}”` : null,
    };
  }
  if (ev.action === "scope_out") {
    const reason =
      (after.scope_out_reason as string | undefined) ??
      (after.reason as string | undefined) ??
      decision?.out_of_scope_reason ??
      null;
    return {
      primary: "Marked out of scope",
      secondary: reason ? `“${truncate(reason, REASON_CAP)}”` : null,
    };
  }
  if (ev.action === "supersede") {
    const by = after.superseded_by as string | undefined;
    return {
      primary: by ? `Superseded by ${by.slice(0, 8)}` : "Superseded",
      secondary: null,
    };
  }
  if (ev.action === "undecide") {
    return { primary: "Cleared previous outcome", secondary: null };
  }
  if (ev.action === "create") {
    const summary = after.summary as string | undefined;
    return { primary: summary ? `“${summary}”` : "Created", secondary: null };
  }
  if (ev.action === "relate") {
    const extra = ev.payload as unknown as {
      type?: string;
      target?: string;
    };
    if (extra.type && extra.target)
      return {
        primary: `Linked: ${extra.type.replace("_", " ")} → ${extra.target.slice(0, 8)}`,
        secondary: null,
      };
    return { primary: "Linked relationship", secondary: null };
  }
  if (ev.action === "unrelate") {
    return { primary: "Removed a relationship", secondary: null };
  }
  return { primary: null, secondary: null };
}

function buildAuditGraph(
  events: Event[],
  actors: Actor[],
  decision: Decision | null,
): { nodes: Node[]; edges: Edge[] } {
  const sorted = [...events].sort(
    (a, b) => new Date(a.ts).getTime() - new Date(b.ts).getTime(),
  );

  const NODE_W = 260;
  const GAP = 60;

  const nodes: Node[] = sorted.map((ev, i) => {
    const a = actors.find((x) => x.handle === ev.actor);
    const desc = describePayload(ev, decision);
    return {
      id: ev.event_id,
      type: "event",
      position: { x: i * (NODE_W + GAP), y: 0 },
      data: {
        action: ev.action,
        actor: ev.actor,
        actorKind: a?.kind,
        ts: ev.ts,
        primary: desc.primary,
        secondary: desc.secondary,
      },
      draggable: false,
    };
  });

  const edges: Edge[] = sorted.slice(1).map((ev, i) => ({
    id: `edge-${i}`,
    source: sorted[i].event_id,
    target: ev.event_id,
    type: "smoothstep",
    animated: false,
    markerEnd: {
      type: MarkerType.ArrowClosed,
      color: "#94a3b8",
      width: 22,
      height: 22,
    },
    style: { stroke: "#94a3b8", strokeWidth: 2 },
  }));

  return { nodes, edges };
}

export interface AuditFlowProps {
  tree: string;
  id: string;
  decision?: Decision | null;
}

export default function AuditFlow({ tree, id, decision }: AuditFlowProps) {
  const { data, isLoading, isError } = useHistory(tree, id);
  const actorsQuery = useActors();
  const theme = useAppStore((s) => s.theme);
  const resolved =
    theme === "system"
      ? typeof window !== "undefined" &&
        window.matchMedia("(prefers-color-scheme: dark)").matches
        ? "dark"
        : "light"
      : theme;

  const { nodes, edges } = useMemo(() => {
    if (!data?.length) return { nodes: [], edges: [] };
    return buildAuditGraph(data, actorsQuery.data ?? [], decision ?? null);
  }, [data, actorsQuery.data, decision]);

  if (isLoading) {
    return (
      <div className="flex justify-center py-8">
        <Spinner />
      </div>
    );
  }
  if (isError) {
    return (
      <div className="p-4 text-danger text-sm">
        Failed to load audit history.
      </div>
    );
  }
  if (!nodes.length) {
    return (
      <div className="p-4 text-default-500 text-sm">No audit events found.</div>
    );
  }

  return (
    <div style={{ width: "100%", height: 280 }}>
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        colorMode={resolved}
        fitView
        fitViewOptions={{ padding: 0.3 }}
        nodesDraggable={false}
        nodesConnectable={false}
        elementsSelectable={false}
        zoomOnScroll={false}
        panOnScroll
        proOptions={{ hideAttribution: true }}
      >
        <Background />
      </ReactFlow>
    </div>
  );
}
