import { Card, CardBody, Chip, Spinner } from "@heroui/react";
import { useParams, Link } from "wouter";
import { useQueue } from "@/api/query";
import type { Decision, QueueItem, Priority, Status } from "@/api/types.gen";

const PRIORITY_COLOR_MAP: Record<Priority, "danger" | "warning" | "primary" | "default" | "success"> = {
  critical: "danger",
  high: "warning",
  medium: "primary",
  low: "default",
  assumption: "success",
};

const STATUS_COLOR_MAP: Record<Status, "success" | "warning" | "danger" | "default"> = {
  proposed: "warning",
  decided: "success",
  out_of_scope: "danger",
  superseded: "default",
};

function PriorityChip({ priority }: { priority: Priority }) {
  return (
    <Chip size="sm" color={PRIORITY_COLOR_MAP[priority]} variant="flat">
      {priority}
    </Chip>
  );
}

function StatusChip({ status }: { status: Status }) {
  return (
    <Chip size="sm" color={STATUS_COLOR_MAP[status]} variant="flat">
      {status.replace(/_/g, " ")}
    </Chip>
  );
}

function QuickWinsCard({ item, tree, rank }: { item: Decision; tree: string; rank: number }) {
  return (
    <Card className="w-full">
      <CardBody className="flex flex-row items-start gap-4">
        <div className="flex-shrink-0 w-8 h-8 rounded-full bg-default-100 flex items-center justify-center text-sm font-bold text-default-600">
          {rank}
        </div>
        <div className="flex-1 min-w-0">
          <p className="font-medium text-sm truncate">{item.summary}</p>
          <div className="flex flex-wrap gap-2 mt-2">
            <PriorityChip priority={item.priority} />
            <StatusChip status={item.status} />
          </div>
        </div>
        <Link
          href={`/trees/${tree}/decisions/${item.id}`}
          className="flex-shrink-0 text-sm text-primary hover:underline whitespace-nowrap"
        >
          Open detail
        </Link>
      </CardBody>
    </Card>
  );
}

function SpearheadCard({ item, tree, rank }: { item: QueueItem; tree: string; rank: number }) {
  return (
    <Card className="w-full">
      <CardBody className="flex flex-row items-start gap-4">
        <div className="flex-shrink-0 w-8 h-8 rounded-full bg-primary-100 flex items-center justify-center text-sm font-bold text-primary-600">
          {rank}
        </div>
        <div className="flex-1 min-w-0">
          <p className="font-medium text-sm truncate">{item.summary}</p>
          {item.blocking_count !== undefined && item.blocking_count > 0 && (
            <div className="mt-2">
              <Chip size="sm" color="danger" variant="flat">
                Blocking {item.blocking_count}
              </Chip>
            </div>
          )}
        </div>
        <Link
          href={`/trees/${tree}/decisions/${item.id}`}
          className="flex-shrink-0 text-sm text-primary hover:underline whitespace-nowrap"
        >
          Open detail
        </Link>
      </CardBody>
    </Card>
  );
}

export function QueueView() {
  const params = useParams<{ tree: string; kind: string }>();
  const tree = params.tree ?? "";
  const kind = (params.kind ?? "quick-wins") as "quick-wins" | "spearhead";

  const { data, isLoading, isError } = useQueue(tree, kind);

  const title = kind === "spearhead" ? "Spearhead Queue" : "Quick Wins Queue";

  return (
    <div className="p-6 space-y-4">
      <h1 className="text-2xl font-bold">{title}</h1>
      <p className="text-sm text-default-500">
        {kind === "spearhead"
          ? "Decisions blocking the most downstream work"
          : "Low-effort, high-impact decisions ready to close"}
      </p>

      {isLoading && (
        <div className="flex justify-center py-12">
          <Spinner size="lg" />
        </div>
      )}

      {isError && (
        <div className="py-8 text-center text-danger">Failed to load queue.</div>
      )}

      {!isLoading && !isError && (
        <>
          {(!data || data.length === 0) ? (
            <div className="py-12 text-center text-default-400">
              No items in queue right now.
            </div>
          ) : (
            <div className="space-y-3">
              {kind === "quick-wins"
                ? (data as Decision[]).map((item, i) => (
                    <QuickWinsCard key={item.id} item={item} tree={tree} rank={i + 1} />
                  ))
                : (data as QueueItem[]).map((item, i) => (
                    <SpearheadCard key={item.id} item={item} tree={tree} rank={i + 1} />
                  ))}
            </div>
          )}
        </>
      )}
    </div>
  );
}
