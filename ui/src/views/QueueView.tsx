import { useEffect, useMemo, useRef, useState } from "react";
import {
  Card,
  CardBody,
  CardHeader,
  Chip,
  Spinner,
  Button,
  ButtonGroup,
  Tabs,
  Tab,
  Input,
  Textarea,
} from "@heroui/react";
import {
  ChevronLeft,
  ChevronRight,
  SkipForward,
  Sparkles,
  Check,
  X,
  ExternalLink,
} from "lucide-react";
import { useParams, useLocation } from "wouter";
import { useQueue, useDecision } from "@/api/query";
import {
  useDecide,
  useScopeOut,
} from "@/api/mutations";
import { useAppStore } from "@/store/app";
import {
  humanPriority,
  humanStatus,
  statusColor,
  decisionDescription,
} from "@/util/labels";
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

interface QueueRow {
  id: string;
  summary: string;
  priority?: Priority;
  blockingCount?: number;
}

export function QueueView() {
  const params = useParams<{ tree: string; kind: string }>();
  const tree = params.tree ?? "";
  const kind = (params.kind ?? "quick-wins") as "quick-wins" | "spearhead";
  const [, navigate] = useLocation();

  const { data, isLoading, isError } = useQueue(tree, kind);

  // Normalise to a uniform row shape regardless of mode.
  const rows: QueueRow[] = useMemo(() => {
    if (!data) return [];
    if (kind === "spearhead") {
      return (data as QueueItem[]).map((q) => ({
        id: q.id,
        summary: q.summary,
        blockingCount: q.blocking_count,
      }));
    }
    return (data as Decision[]).map((d) => ({
      id: d.id,
      summary: d.summary,
      priority: d.priority,
    }));
  }, [data, kind]);

  // Cursor: which row are we on. Reset when the queue changes.
  const [cursor, setCursor] = useState(0);
  useEffect(() => setCursor(0), [tree, kind, rows.length]);

  const safeCursor = Math.min(Math.max(cursor, 0), Math.max(rows.length - 1, 0));
  const current = rows[safeCursor];

  const advance = () => setCursor((i) => Math.min(i + 1, rows.length - 1));
  const back = () => setCursor((i) => Math.max(i - 1, 0));

  return (
    <div className="p-6 space-y-4 max-w-5xl mx-auto">
      <div>
        <h1 className="text-2xl font-bold mb-1">Decision queue</h1>
        <p className="text-sm text-default-500">
          Walk one decision at a time. Take action and move on.
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
      {!isLoading && !isError && rows.length === 0 && (
        <div className="py-12 text-center text-default-400">
          No items in the queue right now. Nothing to do! 🎉
        </div>
      )}

      {current && (
        <>
          <QueueCard
            tree={tree}
            row={current}
            position={safeCursor + 1}
            total={rows.length}
            onAdvance={advance}
          />

          <div className="flex items-center justify-between gap-2 pt-1">
            <Button
              size="sm"
              variant="flat"
              startContent={<ChevronLeft size={14} />}
              isDisabled={safeCursor === 0}
              onPress={back}
            >
              Previous
            </Button>
            <span className="text-sm text-default-500">
              {safeCursor + 1} of {rows.length}
            </span>
            <ButtonGroup size="sm" variant="flat">
              <Button
                startContent={<SkipForward size={14} />}
                onPress={advance}
                isDisabled={safeCursor === rows.length - 1}
              >
                Skip
              </Button>
              <Button
                endContent={<ChevronRight size={14} />}
                color="primary"
                onPress={advance}
                isDisabled={safeCursor === rows.length - 1}
              >
                Next
              </Button>
            </ButtonGroup>
          </div>
        </>
      )}
    </div>
  );
}

// ---- Inline card with actions, mirroring the modal but without the chrome ----

function QueueCard({
  tree,
  row,
  position,
  total,
  onAdvance,
}: {
  tree: string;
  row: QueueRow;
  position: number;
  total: number;
  onAdvance: () => void;
}) {
  const { data: decision, isLoading } = useDecision(tree, row.id);
  const handle = useAppStore((s) => s.currentHandle);
  const openDecision = useAppStore((s) => s.openDecision);

  const decide = useDecide(tree, row.id);
  const scopeOut = useScopeOut(tree, row.id);

  const [showOverride, setShowOverride] = useState(false);
  const [showScope, setShowScope] = useState(false);

  // Auto-advance after a successful mutation.
  // CAREFUL: the mutation object's identity changes on every render, so it
  // CANNOT be in the dep array — that creates an effect → reset() → render
  // → effect loop ("too much recursion"). Only the boolean flag is a dep.
  const decideRef = useRef(decide);
  decideRef.current = decide;
  const scopeRef = useRef(scopeOut);
  scopeRef.current = scopeOut;
  const advanceRef = useRef(onAdvance);
  advanceRef.current = onAdvance;

  useEffect(() => {
    if (decide.isSuccess) {
      decideRef.current.reset();
      setShowOverride(false);
      advanceRef.current();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [decide.isSuccess]);
  useEffect(() => {
    if (scopeOut.isSuccess) {
      scopeRef.current.reset();
      setShowScope(false);
      advanceRef.current();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [scopeOut.isSuccess]);

  if (isLoading || !decision) {
    return (
      <Card>
        <CardBody className="py-12 flex justify-center">
          <Spinner />
        </CardBody>
      </Card>
    );
  }

  const recExists = Boolean(decision.recommended_summary);
  const canAct = decision.status === "proposed" && handle !== null;

  const onAccept = () => {
    if (!handle || !decision.recommended_summary) return;
    decide.mutate({
      choice: decision.recommended_summary,
      reason:
        decision.recommended_full ??
        `Accepted recommendation from ${decision.recommended_by ?? "previous note"}.`,
      by: [handle],
      is_recommended: true,
      ifMatch: decision._rev,
    });
  };

  return (
    <Card className="border border-divider">
      <CardHeader className="flex flex-col gap-3 px-6 pt-5 pb-3">
        {/* Eyebrow: position + chip row */}
        <div className="flex items-center justify-between gap-3 flex-wrap">
          <span className="text-xs uppercase tracking-wider text-default-500 font-semibold">
            Decision {position} of {total}
          </span>
          <div className="flex items-center gap-2 flex-wrap">
            <Chip
              size="sm"
              variant="flat"
              color={PRIORITY_COLOR_MAP[decision.priority]}
            >
              {humanPriority(decision.priority)}
            </Chip>
            <Chip
              size="sm"
              variant="flat"
              color={statusColor(decision.status)}
            >
              {humanStatus(decision.status)}
            </Chip>
            {row.blockingCount !== undefined && row.blockingCount > 0 && (
              <Chip size="sm" color="danger" variant="flat">
                Blocks {row.blockingCount}
              </Chip>
            )}
          </div>
        </div>

        {/* Headline: full-width title with proper line-height */}
        <h2 className="text-2xl font-semibold leading-snug text-foreground">
          {decision.summary}
        </h2>

        {/* Attribution row */}
        <div className="text-sm text-default-500 flex flex-wrap gap-x-3 gap-y-1">
          <span>
            <span className="text-default-400">Opened by</span>{" "}
            <span className="font-medium text-foreground">
              {decision.creator}
            </span>
          </span>
          {decision.assignee && (
            <span>
              <span className="text-default-400">Assigned to</span>{" "}
              <span className="font-medium text-foreground">
                {decision.assignee}
              </span>
            </span>
          )}
          {decision.recommended_by && (
            <span>
              <span className="text-default-400">Recommended by</span>{" "}
              <span className="font-medium text-foreground">
                {decision.recommended_by}
              </span>
            </span>
          )}
        </div>
      </CardHeader>

      <CardBody className="gap-5 px-6 pb-5">
        {(() => {
          const body = decisionDescription(decision);
          return body ? (
            <div>
              <div className="text-[11px] font-semibold uppercase tracking-wider text-default-500 mb-1.5">
                Context
              </div>
              <p className="text-sm leading-relaxed text-foreground/90 whitespace-pre-wrap">
                {body}
              </p>
            </div>
          ) : (
            <p className="text-xs italic text-default-400">
              No description on this decision yet.
            </p>
          );
        })()}

        {recExists && (
          <Card className="bg-primary-50 dark:bg-primary-950 border border-primary-300 dark:border-primary-700">
            <CardBody className="gap-2">
              {decision.recommended_summary && (
                <div className="font-semibold text-foreground">
                  {decision.recommended_summary}
                </div>
              )}
              {decision.recommended_full && (
                <div className="text-sm text-foreground/85 whitespace-pre-wrap">
                  {decision.recommended_full}
                </div>
              )}
              <div className="text-xs text-foreground/60">
                Recommendation
                {decision.recommended_by && <> from {decision.recommended_by}</>}
              </div>
            </CardBody>
          </Card>
        )}

        {/* Resolution cards: outcome / out-of-scope / superseded. Same shape
            as the modal so a decided item read in the queue is informative. */}
        {decision.status === "decided" && decision.actual_choice && (
          <Card className="bg-success-50 dark:bg-success-950 border border-success-300 dark:border-success-700">
            <CardBody className="gap-1">
              <div className="text-xs uppercase tracking-wider text-foreground/60 font-semibold">
                Outcome
              </div>
              <div className="font-semibold text-foreground">
                {decision.actual_choice}
              </div>
              {decision.actual_choice_reason && (
                <p className="text-sm text-foreground/80 whitespace-pre-wrap">
                  {decision.actual_choice_reason}
                </p>
              )}
              <div className="text-xs text-foreground/60 mt-1 flex flex-wrap gap-2">
                {decision.decided_by && decision.decided_by.length > 0 && (
                  <span>Decided by {decision.decided_by.join(", ")}</span>
                )}
                {recExists && (
                  <span>
                    ·{" "}
                    {decision.is_recommended ||
                    decision.actual_choice === decision.recommended_summary
                      ? "Followed the recommendation"
                      : "Overrode the recommendation"}
                  </span>
                )}
              </div>
            </CardBody>
          </Card>
        )}

        {decision.status === "out_of_scope" && (
          <Card className="bg-default-100 dark:bg-default-50/50 border border-default-300 dark:border-default-200">
            <CardBody className="gap-1">
              <div className="text-xs uppercase tracking-wider text-foreground/60 font-semibold">
                Marked out of scope
              </div>
              {decision.out_of_scope_reason ? (
                <p className="text-sm text-foreground whitespace-pre-wrap">
                  {decision.out_of_scope_reason}
                </p>
              ) : (
                <p className="text-xs italic text-default-400">
                  No reason recorded.
                </p>
              )}
            </CardBody>
          </Card>
        )}

        {decision.status === "superseded" && (
          <Card className="bg-warning-50 dark:bg-warning-950 border border-warning-300 dark:border-warning-700">
            <CardBody className="gap-1">
              <div className="text-xs uppercase tracking-wider text-foreground/60 font-semibold">
                Superseded
              </div>
              <p className="text-sm text-foreground/80">
                Replaced by a newer decision. Open in modal to navigate to it.
              </p>
            </CardBody>
          </Card>
        )}

        {(decision.tags ?? []).length > 0 && (
          <div className="flex flex-wrap gap-1.5">
            {(decision.tags ?? []).map((t) => (
              <Chip key={t} size="sm" variant="flat">
                {t}
              </Chip>
            ))}
          </div>
        )}

        {/* Inline action forms */}
        {showOverride && (
          <OverrideForm
            decision={decision}
            handle={handle ?? ""}
            loading={decide.isPending}
            onSubmit={(payload) =>
              decide.mutate({ ...payload, ifMatch: decision._rev })
            }
            onCancel={() => setShowOverride(false)}
          />
        )}
        {showScope && (
          <ScopeForm
            loading={scopeOut.isPending}
            onSubmit={(reason) =>
              scopeOut.mutate({ reason, ifMatch: decision._rev })
            }
            onCancel={() => setShowScope(false)}
          />
        )}
      </CardBody>

      <div className="px-4 pb-4 pt-1 flex flex-wrap items-center gap-2 border-t border-divider">
        {canAct && recExists && (
          <Button
            size="sm"
            color="success"
            startContent={<Sparkles size={14} />}
            isLoading={decide.isPending}
            onPress={onAccept}
          >
            Accept recommendation
          </Button>
        )}
        {canAct && (
          <Button
            size="sm"
            color={recExists ? "default" : "success"}
            variant="flat"
            onPress={() => setShowOverride((v) => !v)}
          >
            {recExists ? "Override recommendation" : "Decide"}
          </Button>
        )}
        {canAct && (
          <Button
            size="sm"
            color="warning"
            variant="flat"
            onPress={() => setShowScope((v) => !v)}
          >
            Scope out
          </Button>
        )}
        <div className="ml-auto flex gap-2">
          <Button
            size="sm"
            variant="light"
            startContent={<ExternalLink size={14} />}
            onPress={() => openDecision(tree, row.id)}
          >
            Open in modal
          </Button>
        </div>
      </div>
    </Card>
  );
}

function OverrideForm({
  decision,
  handle,
  loading,
  onSubmit,
  onCancel,
}: {
  decision: Decision;
  handle: string;
  loading: boolean;
  onSubmit: (p: {
    choice: string;
    reason: string;
    by: string[];
    is_recommended?: boolean;
  }) => void;
  onCancel: () => void;
}) {
  const [choice, setChoice] = useState("");
  const [reason, setReason] = useState("");
  const recExists = Boolean(decision.recommended_summary);
  const matchesRec =
    recExists && choice.trim() === (decision.recommended_summary ?? "").trim();
  return (
    <div className="border border-divider rounded-md p-3 bg-content2 flex flex-col gap-2">
      <Input
        label="Your choice"
        placeholder="What did you decide?"
        size="sm"
        value={choice}
        onValueChange={setChoice}
      />
      <Textarea
        label="Reason"
        placeholder="Why?"
        size="sm"
        minRows={2}
        value={reason}
        onValueChange={setReason}
      />
      {matchesRec && (
        <div className="text-xs text-warning">
          Matches the recommendation — use the green “Accept recommendation” button instead.
        </div>
      )}
      <div className="flex gap-2 justify-end">
        <Button size="sm" variant="light" onPress={onCancel}>
          Cancel
        </Button>
        <Button
          size="sm"
          color="success"
          isLoading={loading}
          startContent={<Check size={14} />}
          isDisabled={!choice.trim() || !reason.trim() || !handle}
          onPress={() =>
            onSubmit({
              choice,
              reason,
              by: [handle],
              is_recommended: matchesRec,
            })
          }
        >
          Submit
        </Button>
      </div>
    </div>
  );
}

function ScopeForm({
  loading,
  onSubmit,
  onCancel,
}: {
  loading: boolean;
  onSubmit: (reason: string) => void;
  onCancel: () => void;
}) {
  const [reason, setReason] = useState("");
  return (
    <div className="border border-divider rounded-md p-3 bg-content2 flex flex-col gap-2">
      <Textarea
        label="Reason"
        size="sm"
        minRows={2}
        value={reason}
        onValueChange={setReason}
      />
      <div className="flex gap-2 justify-end">
        <Button size="sm" variant="light" onPress={onCancel}>
          Cancel
        </Button>
        <Button
          size="sm"
          color="warning"
          isLoading={loading}
          startContent={<X size={14} />}
          isDisabled={!reason.trim()}
          onPress={() => onSubmit(reason)}
        >
          Scope out
        </Button>
      </div>
    </div>
  );
}
