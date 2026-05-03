import { Card, CardBody, Chip, Spinner, Button, Tabs, Tab } from "@heroui/react";
import { useParams, useLocation } from "wouter";
import { useQueue } from "@/api/query";
import { useAppStore } from "@/store/app";
import { humanPriority, humanStatus, statusColor } from "@/util/labels";
import type { Decision, QueueItem, Priority } from "@/api/types.gen";

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

function PriorityChip({ priority }: { priority?: Priority }) {
  if (!priority) return null;
  return (
    <Chip size="sm" color={PRIORITY_COLOR_MAP[priority]} variant="flat">
      {humanPriority(priority)}
    </Chip>
  );
}

interface RowProps {
  id: string;
  summary: string;
  priority?: Priority;
  // Decision-shaped queue items have status; spearhead items don't.
  status?: Decision["status"];
  blockingCount?: number;
  rank: number;
  tree: string;
}

function QueueRow({
  id,
  summary,
  priority,
  status,
  blockingCount,
  rank,
  tree,
}: RowProps) {
  const openDecision = useAppStore((s) => s.openDecision);
  return (
    <Card>
      <CardBody className="flex flex-row items-start gap-4">
        <div className="flex-shrink-0 w-8 h-8 rounded-full bg-default-100 flex items-center justify-center text-sm font-bold text-default-600">
          {rank}
        </div>
        <div className="flex-1 min-w-0">
          <p className="font-medium text-sm">{summary}</p>
          <div className="flex flex-wrap gap-2 mt-2">
            <PriorityChip priority={priority} />
            {status && (
              <Chip size="sm" variant="flat" color={statusColor(status)}>
                {humanStatus(status)}
              </Chip>
            )}
            {blockingCount !== undefined && blockingCount > 0 && (
              <Chip size="sm" color="danger" variant="flat">
                Blocking {blockingCount}
              </Chip>
            )}
          </div>
        </div>
        <Button
          size="sm"
          variant="light"
          color="primary"
          onPress={() => openDecision(tree, id)}
        >
          Open
        </Button>
      </CardBody>
    </Card>
  );
}

export function QueueView() {
  const params = useParams<{ tree: string; kind: string }>();
  const tree = params.tree ?? "";
  const kind = (params.kind ?? "quick-wins") as "quick-wins" | "spearhead";

  const [, navigate] = useLocation();
  const { data, isLoading, isError } = useQueue(tree, kind);

  return (
    <div className="p-6 space-y-4 max-w-5xl mx-auto">
      <div>
        <h1 className="text-2xl font-bold mb-1">Queues</h1>
        <p className="text-sm text-default-500">
          Two ways to find the next thing to work on.
        </p>
      </div>

      <Tabs
        aria-label="Queue mode"
        selectedKey={kind}
        onSelectionChange={(k) =>
          navigate(`/trees/${tree}/queue/${String(k)}`)
        }
      >
        <Tab key="quick-wins" title="Quick wins" />
        <Tab key="spearhead" title="Spearhead" />
      </Tabs>

      <p className="text-sm text-default-500">
        {kind === "spearhead"
          ? "Decisions blocking the most downstream work — unblock these to free up others."
          : "Proposed decisions whose blockers are all resolved — ready to close."}
      </p>

      {isLoading && (
        <div className="flex justify-center py-12">
          <Spinner size="lg" />
        </div>
      )}

      {isError && (
        <div className="py-8 text-center text-danger">
          Failed to load queue.
        </div>
      )}

      {!isLoading && !isError && (!data || data.length === 0) && (
        <div className="py-12 text-center text-default-400">
          No items in queue right now.
        </div>
      )}

      {!isLoading && !isError && data && data.length > 0 && (
        <div className="space-y-3">
          {kind === "quick-wins"
            ? (data as Decision[]).map((item, i) => (
                <QueueRow
                  key={item.id}
                  id={item.id}
                  summary={item.summary}
                  priority={item.priority}
                  status={item.status}
                  rank={i + 1}
                  tree={tree}
                />
              ))
            : (data as QueueItem[]).map((item, i) => (
                <QueueRow
                  key={item.id}
                  id={item.id}
                  summary={item.summary}
                  blockingCount={item.blocking_count}
                  rank={i + 1}
                  tree={tree}
                />
              ))}
        </div>
      )}
    </div>
  );
}
