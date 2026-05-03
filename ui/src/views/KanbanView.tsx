import { Card, CardBody, Chip, Spinner } from "@heroui/react";
import { useParams } from "wouter";
import { useDecisions } from "@/api/query";
import { useAppStore } from "@/store/app";
import { humanPriority, humanStatus } from "@/util/labels";
import type { Decision, Status, Priority } from "@/api/types.gen";

// Order: out_of_scope → proposed → superseded → decided
// (per UX: archive/parking on the left, active middle, history on the right)
const COLUMNS: { status: Status; label: string; color: string }[] = [
  {
    status: "out_of_scope",
    label: "Out of scope",
    color:
      "bg-default-50 dark:bg-default-100/10 border-default-200 dark:border-default-300/20",
  },
  {
    status: "proposed",
    label: "Proposed",
    color:
      "bg-primary-50 dark:bg-primary-900/20 border-primary-200 dark:border-primary-700/30",
  },
  {
    status: "superseded",
    label: "Superseded",
    color:
      "bg-warning-50 dark:bg-warning-900/20 border-warning-200 dark:border-warning-700/30",
  },
  {
    status: "decided",
    label: "Decided",
    color:
      "bg-success-50 dark:bg-success-900/20 border-success-200 dark:border-success-700/30",
  },
];

const PRIORITY_COLOR_MAP: Record<
  Priority,
  "danger" | "warning" | "primary" | "default" | "success"
> = {
  critical: "danger",
  high: "warning",
  medium: "primary",
  low: "default",
  assumption: "success",
};

function DecisionCard({ decision, tree }: { decision: Decision; tree: string }) {
  const openDecision = useAppStore((s) => s.openDecision);
  return (
    <Card
      isPressable
      onPress={() => openDecision(tree, decision.id)}
      className="w-full"
      shadow="sm"
    >
      <CardBody className="p-3 space-y-2">
        <p className="text-sm font-medium line-clamp-2 text-foreground">
          {decision.summary}
        </p>
        <div className="flex flex-wrap gap-1">
          <Chip
            size="sm"
            color={PRIORITY_COLOR_MAP[decision.priority]}
            variant="flat"
          >
            {humanPriority(decision.priority)}
          </Chip>
          {decision.assignee && (
            <Chip size="sm" variant="flat" color="default">
              @{decision.assignee}
            </Chip>
          )}
        </div>
        <p className="text-xs text-default-400">
          by {decision.creator}
          {decision.recommended_by && ` · rec ${decision.recommended_by}`}
        </p>
      </CardBody>
    </Card>
  );
}

function KanbanColumn({
  label,
  color,
  decisions,
  tree,
}: {
  status: Status;
  label: string;
  color: string;
  decisions: Decision[];
  tree: string;
}) {
  return (
    <div
      className={`flex-1 min-w-[240px] rounded-xl border p-4 space-y-3 ${color}`}
    >
      <div className="flex items-center justify-between">
        <h3 className="font-semibold text-sm text-foreground">{label}</h3>
        <Chip size="sm" variant="flat">
          {decisions.length}
        </Chip>
      </div>
      {decisions.length === 0 ? (
        <p className="text-xs text-default-400 text-center py-4">
          {humanStatus(label)} decisions will appear here
        </p>
      ) : (
        decisions.map((d) => (
          <DecisionCard key={d.id} decision={d} tree={tree} />
        ))
      )}
    </div>
  );
}

export function KanbanView() {
  const params = useParams<{ tree: string }>();
  const tree = params.tree ?? "";

  const { data, isLoading, isError } = useDecisions(tree);
  const decisions = data?.items ?? [];

  const byStatus = (status: Status) =>
    decisions.filter((d) => d.status === status);

  return (
    <div className="p-6 space-y-4">
      <h1 className="text-2xl font-bold">Kanban</h1>

      {isLoading && (
        <div className="flex justify-center py-12">
          <Spinner size="lg" />
        </div>
      )}

      {isError && (
        <div className="py-8 text-center text-danger">
          Failed to load decisions.
        </div>
      )}

      {!isLoading && !isError && (
        <div className="flex gap-4 overflow-x-auto pb-4">
          {COLUMNS.map(({ status, label, color }) => (
            <KanbanColumn
              key={status}
              status={status}
              label={label}
              color={color}
              decisions={byStatus(status)}
              tree={tree}
            />
          ))}
        </div>
      )}
    </div>
  );
}
