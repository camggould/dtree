import { useState } from "react";
import {
  Modal,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
  Tabs,
  Tab,
  Chip,
  Button,
  ButtonGroup,
  Card,
  CardBody,
  Spinner,
  Input,
  Textarea,
  Divider,
  Table,
  TableHeader,
  TableColumn,
  TableBody,
  TableRow,
  TableCell,
} from "@heroui/react";
import { Check, X, Sparkles, ArrowLeft, ExternalLink } from "lucide-react";
import { useDecision, useHistory, useDecisions } from "@/api/query";
import {
  useDecide,
  useUndecide,
  useScopeOut,
  useRestore,
} from "@/api/mutations";
import { useAppStore } from "@/store/app";
import {
  humanStatus,
  statusColor,
  humanPriority,
  humanAction,
  decisionDescription,
  truncate,
} from "@/util/labels";
import { formatDistanceToNow } from "date-fns";
import AuditFlow from "@/components/AuditFlow";

interface Props {
  tree: string;
  decisionId: string | null;
  isOpen: boolean;
  onClose: () => void;
}

/** The canonical decision experience.
 *
 *  - Shows summary, description, status/priority chips
 *  - Recommendation block with one-click "Accept recommendation" when status === proposed
 *  - Lifecycle action buttons (side-by-side via ButtonGroup)
 *  - Tabs: Overview / History / Audit flow
 *  - Origin-aware: uses an inline modal close instead of routing back; the
 *    caller decides what's underneath (graph, queue, list-modal, etc.).
 */
export function DecisionModal({ tree, decisionId, isOpen, onClose }: Props) {
  return (
    <Modal
      isOpen={isOpen && Boolean(decisionId)}
      onClose={onClose}
      size="3xl"
      scrollBehavior="inside"
      backdrop="opaque"
    >
      <ModalContent>
        {decisionId && <DecisionModalBody tree={tree} id={decisionId} onClose={onClose} />}
      </ModalContent>
    </Modal>
  );
}

function DecisionModalBody({
  tree,
  id,
  onClose,
}: {
  tree: string;
  id: string;
  onClose: () => void;
}) {
  const { data: decision, isLoading } = useDecision(tree, id);
  const handle = useAppStore((s) => s.currentHandle);
  const stackDepth = useAppStore((s) => s.decisionStack.length);
  const popDecision = useAppStore((s) => s.popDecision);

  if (isLoading || !decision) {
    return (
      <ModalBody className="py-12 flex justify-center">
        <Spinner />
      </ModalBody>
    );
  }

  const recExists =
    Boolean(decision.recommended_summary) ||
    Boolean(decision.recommended_full);

  return (
    <>
      <ModalHeader className="flex flex-col gap-2 pb-2">
        {stackDepth > 0 && (
          <Button
            variant="light"
            size="sm"
            startContent={<ArrowLeft size={14} />}
            onPress={popDecision}
            className="self-start -ml-2 -mt-1"
          >
            Back to previous decision
          </Button>
        )}
        <div className="flex items-start justify-between gap-3">
          <h2 className="text-xl font-semibold leading-tight">
            {decision.summary}
          </h2>
          <div className="flex items-center gap-1 shrink-0">
            {/* Assumption is treated as a meta-status: it overrides the
                normal status chip because "assumption" reads more clearly
                than "Decided" with a tiny "Assumption" priority chip next to
                it. */}
            {decision.priority === "assumption" ? (
              <Chip size="sm" variant="flat" color="default">
                Assumption
              </Chip>
            ) : (
              <>
                <Chip
                  size="sm"
                  variant="flat"
                  color={statusColor(decision.status)}
                >
                  {humanStatus(decision.status)}
                </Chip>
                <Chip size="sm" variant="flat">
                  {humanPriority(decision.priority)}
                </Chip>
              </>
            )}
          </div>
        </div>
        <div className="text-xs text-default-500 font-normal">
          <span>by {decision.creator}</span>
          {decision.assignee && (
            <span> · assigned to {decision.assignee}</span>
          )}
          <span> · in {decision.tree}</span>
          <span className="ml-2 font-mono">{decision.id.slice(0, 8)}</span>
        </div>
      </ModalHeader>

      <ModalBody className="gap-3">
        <Tabs aria-label="Decision tabs" variant="underlined" size="sm">
          <Tab key="overview" title="Overview">
            <OverviewTab decision={decision} handle={handle} recExists={recExists} />
          </Tab>
          <Tab key="history" title="History">
            <HistoryTab tree={tree} id={id} decision={decision} />
          </Tab>
          <Tab key="audit" title="Audit flow">
            <AuditFlow tree={tree} id={id} decision={decision} />
          </Tab>
        </Tabs>
      </ModalBody>

      <ModalFooter className="border-t border-divider">
        <ActionBar decision={decision} handle={handle} onClose={onClose} />
      </ModalFooter>
    </>
  );
}

// ---- Overview tab --------------------------------------------------------

function OverviewTab({
  decision,
  handle,
  recExists,
}: {
  decision: import("@/api/types.gen").Decision;
  handle: string | null;
  recExists: boolean;
}) {
  const body = decisionDescription(decision);
  return (
    <div className="flex flex-col gap-4 py-2">
      <section>
        <SectionLabel>Description</SectionLabel>
        {body ? (
          <p className="text-sm leading-relaxed whitespace-pre-wrap">{body}</p>
        ) : (
          <p className="text-xs italic text-default-400">
            No description provided. The decision needs context — what's the
            question, what are the options, what are the constraints?
          </p>
        )}
      </section>

      {recExists && (
        <RecommendationBlock decision={decision} handle={handle} />
      )}

      {decision.status === "out_of_scope" && (
        <section>
          <SectionLabel>Out-of-scope reason</SectionLabel>
          <Card className="bg-default-100 dark:bg-default-50/50 border border-default-300 dark:border-default-200">
            <CardBody>
              {decision.out_of_scope_reason ? (
                <p className="text-sm whitespace-pre-wrap text-foreground">
                  {decision.out_of_scope_reason}
                </p>
              ) : (
                <p className="text-xs italic text-default-400">
                  Marked out of scope without a stated reason.
                </p>
              )}
            </CardBody>
          </Card>
        </section>
      )}

      {decision.status === "superseded" && (
        <SupersededBlock decision={decision} />
      )}

      {decision.status === "decided" && decision.actual_choice && (
        <section>
          <SectionLabel>Outcome</SectionLabel>
          <Card className="bg-success-50 dark:bg-success-950 border border-success-300 dark:border-success-700">
            <CardBody className="gap-1 text-foreground">
              <div className="font-semibold">{decision.actual_choice}</div>
              {decision.actual_choice_reason && (
                <div className="text-sm text-foreground/80">
                  {decision.actual_choice_reason}
                </div>
              )}
              <div className="text-xs text-foreground/60 mt-1 flex flex-wrap gap-2">
                {decision.decided_by && decision.decided_by.length > 0 && (
                  <span>Decided by {decision.decided_by.join(", ")}</span>
                )}
                {decision.recommended_summary && (
                  <span>
                    ·{" "}
                    {decision.is_recommended ||
                    decision.actual_choice ===
                      decision.recommended_summary
                      ? "Followed the recommendation"
                      : "Overrode the recommendation"}
                  </span>
                )}
              </div>
            </CardBody>
          </Card>
        </section>
      )}

      {(decision.tags ?? []).length > 0 && (
        <section>
          <SectionLabel>Tags</SectionLabel>
          <div className="flex flex-wrap gap-1.5">
            {(decision.tags ?? []).map((t) => (
              <Chip key={t} size="sm" variant="flat">
                {t}
              </Chip>
            ))}
          </div>
        </section>
      )}

      {(decision.relationships ?? []).length > 0 && (
        <RelationshipsSection
          tree={decision.tree}
          relationships={decision.relationships ?? []}
        />
      )}
    </div>
  );
}

function SupersededBlock({
  decision,
}: {
  decision: import("@/api/types.gen").Decision;
}) {
  const pushDecision = useAppStore((s) => s.pushDecision);
  // Find the supersedes target (the new decision that replaces this one).
  const target = (decision.relationships ?? []).find(
    (r) => r.type === "supersedes",
  );
  return (
    <section>
      <SectionLabel>Superseded by</SectionLabel>
      <Card className="bg-warning-50 dark:bg-warning-950 border border-warning-300 dark:border-warning-700">
        <CardBody className="gap-2">
          {target ? (
            <button
              type="button"
              onClick={() => pushDecision(decision.tree, target.target)}
              className="text-left text-foreground hover:text-warning"
            >
              <span className="font-mono text-xs text-default-500">
                {target.target.slice(0, 8)}
              </span>
              <span className="ml-2 text-sm">→ click to open replacement</span>
            </button>
          ) : (
            <p className="text-xs italic text-default-400">
              Marked superseded but no replacement is linked.
            </p>
          )}
        </CardBody>
      </Card>
    </section>
  );
}

function RelationshipsSection({
  tree,
  relationships,
}: {
  tree: string;
  relationships: import("@/api/types.gen").Relationship[];
}) {
  const pushDecision = useAppStore((s) => s.pushDecision);
  // Pull all decisions in this tree to look up target summaries.
  const { data: page } = useDecisions(tree);
  const byId = new Map((page?.items ?? []).map((d) => [d.id, d]));

  const rows = relationships.map((r) => ({
    rel: r,
    target: byId.get(r.target),
  }));

  return (
    <section>
      <SectionLabel>Relationships</SectionLabel>
      <Table aria-label="Relationships" removeWrapper isStriped>
        <TableHeader>
          <TableColumn>Type</TableColumn>
          <TableColumn>Target</TableColumn>
          <TableColumn>Status</TableColumn>
          <TableColumn> </TableColumn>
        </TableHeader>
        <TableBody>
          {rows.map(({ rel, target }) => (
            <TableRow key={`${rel.type}-${rel.target}`}>
              <TableCell>
                <Chip size="sm" variant="flat" color="warning">
                  {rel.type.replace("_", " ")}
                </Chip>
              </TableCell>
              <TableCell>
                <button
                  type="button"
                  onClick={() => pushDecision(tree, rel.target)}
                  className="text-left hover:text-primary"
                >
                  {target?.summary ?? (
                    <span className="font-mono text-default-400">
                      {rel.target.slice(0, 8)}
                    </span>
                  )}
                </button>
              </TableCell>
              <TableCell>
                {target ? (
                  <Chip size="sm" variant="flat" color={statusColor(target.status)}>
                    {humanStatus(target.status)}
                  </Chip>
                ) : (
                  <span className="text-xs text-default-400">unresolved</span>
                )}
              </TableCell>
              <TableCell>
                <Button
                  size="sm"
                  variant="light"
                  isIconOnly
                  onPress={() => pushDecision(tree, rel.target)}
                  aria-label="Open"
                >
                  <ExternalLink size={14} />
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </section>
  );
}

// ---- Recommendation block with Accept button ----

function RecommendationBlock({
  decision,
  handle,
}: {
  decision: import("@/api/types.gen").Decision;
  handle: string | null;
}) {
  const decide = useDecide(decision.tree, decision.id);
  const canAccept = decision.status === "proposed" && handle !== null;

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
    <section>
      <SectionLabel>Recommendation</SectionLabel>
      <Card className="bg-primary-50 dark:bg-primary-950 border border-primary-300 dark:border-primary-700">
        <CardBody className="gap-2 text-foreground">
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
          <div className="flex items-center justify-between mt-1 gap-2 flex-wrap">
            <div className="text-xs text-foreground/60">
              {decision.recommended_by
                ? `Recommended by ${decision.recommended_by}`
                : "Source not attributed"}
            </div>
            {canAccept && (
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
          </div>
          {decide.isError && (
            <div className="text-xs text-danger">
              {(decide.error as Error)?.message ?? "Failed"}
            </div>
          )}
        </CardBody>
      </Card>
    </section>
  );
}

// ---- History tab --------------------------------------------------------

// Cap for inline reasoning quotes in history/audit-flow nodes. Longer text
// is truncated with an ellipsis; the full text is on the decision itself.
const REASON_CAP = 240;

function HistoryTab({
  tree,
  id,
  decision,
}: {
  tree: string;
  id: string;
  decision: import("@/api/types.gen").Decision;
}) {
  const { data: events, isLoading } = useHistory(tree, id);
  if (isLoading) return <Spinner size="sm" />;
  const list = events ?? [];
  if (list.length === 0)
    return <p className="text-default-500 text-sm py-4">No events</p>;
  return (
    <div className="flex flex-col gap-3 py-2">
      {list.map((e) => {
        const after = (e.payload?.after ?? {}) as Record<string, unknown>;
        const before = (e.payload?.before ?? {}) as Record<string, unknown>;
        const summary = describeEvent(e.action, after, before, decision);
        const reason = describeReason(e.action, after, decision);
        return (
          <div
            key={e.event_id}
            className="flex items-start gap-2 text-sm border-l-2 border-divider pl-3 py-1"
          >
            <Chip size="sm" variant="flat" color="primary">
              {humanAction(e.action)}
            </Chip>
            <div className="flex-1 min-w-0">
              <div className="text-foreground">
                <span className="font-medium">{e.actor}</span>
                {summary && (
                  <span className="text-foreground/80"> — {summary}</span>
                )}
              </div>
              {reason && (
                <div className="mt-1 text-xs text-foreground/65 italic border-l border-default-200 pl-2">
                  “{truncate(reason, REASON_CAP)}”
                </div>
              )}
              <div className="text-xs text-default-500 mt-0.5">
                {formatDistanceToNow(new Date(e.ts))} ago
              </div>
            </div>
          </div>
        );
      })}
    </div>
  );
}

/** Pull the user-supplied reason text out of an event payload, falling back
 *  to whatever's on the parent decision when the event only carries a diff. */
function describeReason(
  action: string,
  after: Record<string, unknown>,
  decision: import("@/api/types.gen").Decision,
): string | null {
  if (action === "decide") {
    return (
      (after.actual_choice_reason as string | undefined) ??
      decision.actual_choice_reason ??
      null
    );
  }
  if (action === "scope_out") {
    return (
      (after.scope_out_reason as string | undefined) ??
      (after.reason as string | undefined) ??
      decision.out_of_scope_reason ??
      null
    );
  }
  return null;
}

/** Build a one-line plain-language summary for an audit event's payload.
 *  Uses the parent decision as a fallback context when the event payload
 *  doesn't carry recommendation fields directly (e.g. some server paths
 *  emit a partial diff in `after` rather than the full record).
 */
function describeEvent(
  action: string,
  after: Record<string, unknown>,
  _before: Record<string, unknown>,
  decision: import("@/api/types.gen").Decision,
): string | null {
  if (action === "decide") {
    const choice =
      (after.actual_choice as string | undefined) ?? decision.actual_choice;
    if (!choice) return null;

    // Prefer the explicit is_recommended flag set by the API; fall back to
    // matching the choice against whatever recommendation is on the
    // decision now (the after payload may not include it on partial diffs).
    const isRecAfter = after.is_recommended as boolean | undefined;
    const recAfter = after.recommended_summary as string | undefined;
    const recCurrent = decision.recommended_summary;
    const recommended = recAfter ?? recCurrent;

    const recExisted = Boolean(recommended);
    const followed =
      isRecAfter === true ||
      (recommended !== undefined && choice === recommended);

    if (followed) return `chose “${choice}” (followed recommendation)`;
    if (recExisted)
      return `chose “${choice}” (overrode recommendation “${recommended}”)`;
    return `chose “${choice}” (no recommendation existed)`;
  }
  if (action === "scope_out") {
    const reason =
      (after.scope_out_reason as string | undefined) ??
      (after.reason as string | undefined);
    return reason ? `reason: ${reason}` : null;
  }
  if (action === "supersede") {
    const by = after.superseded_by as string | undefined;
    return by ? `replaced by ${by.slice(0, 8)}` : null;
  }
  if (action === "undecide") {
    return "cleared the previous outcome";
  }
  if (action === "create") {
    const summary = after.summary as string | undefined;
    return summary ? `“${summary}”` : null;
  }
  return null;
}

// ---- Action bar (side-by-side, status-gated) ---------------------------

function ActionBar({
  decision,
  handle,
  onClose,
}: {
  decision: import("@/api/types.gen").Decision;
  handle: string | null;
  onClose: () => void;
}) {
  const [showDecide, setShowDecide] = useState(false);
  const [showScope, setShowScope] = useState(false);

  const decide = useDecide(decision.tree, decision.id);
  const undecide = useUndecide(decision.tree, decision.id);
  const scopeOut = useScopeOut(decision.tree, decision.id);
  const restore = useRestore(decision.tree, decision.id);

  if (!handle) {
    return (
      <div className="flex items-center justify-between w-full">
        <span className="text-sm text-default-500">
          Pick an identity to take actions
        </span>
        <Button size="sm" variant="light" onPress={onClose}>
          Close
        </Button>
      </div>
    );
  }

  const isAssumption = decision.priority === "assumption";

  const buttonsForStatus = (() => {
    // Assumptions get their own treatment regardless of status: they're a
    // working assumption, not a decision. The user wants to either OVERRIDE
    // (replace with a real choice) or CLEAR (back to nothing).
    if (isAssumption) {
      // If still proposed, "Override assumption" opens the decide form so
      // they can record an actual choice + reason. If already decided,
      // "Undecide" clears the assumption back to a blank slate.
      const showActions =
        decision.status === "proposed" || decision.status === "decided";
      if (!showActions) return null;
      return (
        <ButtonGroup size="sm" variant="flat">
          <Button color="primary" onPress={() => setShowDecide(true)}>
            Override assumption
          </Button>
          {decision.status === "decided" && (
            <Button
              color="warning"
              isLoading={undecide.isPending}
              onPress={() => undecide.mutate(decision._rev)}
            >
              Clear assumption
            </Button>
          )}
        </ButtonGroup>
      );
    }

    if (decision.status === "proposed") {
      const recExists = Boolean(decision.recommended_summary);
      return (
        <ButtonGroup size="sm" variant="flat">
          <Button color="success" onPress={() => setShowDecide(true)}>
            {recExists ? "Override recommendation" : "Decide"}
          </Button>
          <Button color="warning" onPress={() => setShowScope(true)}>
            Scope out
          </Button>
        </ButtonGroup>
      );
    }
    if (decision.status === "decided") {
      return (
        <Button
          size="sm"
          variant="flat"
          color="warning"
          isLoading={undecide.isPending}
          onPress={() => undecide.mutate(decision._rev)}
        >
          Undecide
        </Button>
      );
    }
    if (decision.status === "out_of_scope") {
      return (
        <Button
          size="sm"
          variant="flat"
          color="primary"
          isLoading={restore.isPending}
          onPress={() => restore.mutate(decision._rev)}
        >
          Restore
        </Button>
      );
    }
    return null;
  })();

  return (
    <div className="w-full flex flex-col gap-3">
      {showDecide && (
        <DecideForm
          decision={decision}
          handle={handle}
          onSubmit={(payload) => {
            decide.mutate({ ...payload, ifMatch: decision._rev });
            setShowDecide(false);
          }}
          onCancel={() => setShowDecide(false)}
          loading={decide.isPending}
        />
      )}
      {showScope && (
        <ScopeOutForm
          loading={scopeOut.isPending}
          onSubmit={(reason) => {
            scopeOut.mutate({ reason, ifMatch: decision._rev });
            setShowScope(false);
          }}
          onCancel={() => setShowScope(false)}
        />
      )}
      <div className="flex items-center justify-between gap-2">
        <div>{buttonsForStatus}</div>
        <Button size="sm" variant="light" onPress={onClose}>
          Close
        </Button>
      </div>
    </div>
  );
}

function DecideForm({
  decision,
  handle,
  onSubmit,
  onCancel,
  loading,
}: {
  decision: import("@/api/types.gen").Decision;
  handle: string;
  onSubmit: (p: { choice: string; reason: string; by: string[]; is_recommended?: boolean }) => void;
  onCancel: () => void;
  loading: boolean;
}) {
  // Blank by default: this form is for proposing your OWN answer, distinct
  // from the one-click "Accept recommendation" button above.
  const [choice, setChoice] = useState("");
  const [reason, setReason] = useState("");
  const recExists = Boolean(decision.recommended_summary);
  const matchesRec =
    recExists && choice.trim() === (decision.recommended_summary ?? "").trim();

  return (
    <div className="border border-divider rounded-md p-3 bg-content2 flex flex-col gap-2">
      <div className="text-xs text-foreground/70 font-medium">
        {recExists
          ? `Overriding the standing recommendation from ${decision.recommended_by ?? "unknown"}.`
          : "Recording your decision."}
      </div>
      <Input
        label="Your choice"
        placeholder="What did you decide?"
        value={choice}
        onValueChange={setChoice}
        size="sm"
      />
      <Textarea
        label="Reason"
        placeholder="Why?"
        value={reason}
        onValueChange={setReason}
        size="sm"
        minRows={2}
      />
      {matchesRec && (
        <div className="text-xs text-warning">
          Heads-up: this matches the recommendation. Consider using the
          “Accept recommendation” button instead.
        </div>
      )}
      <Divider />
      <div className="flex gap-2 justify-end">
        <Button size="sm" variant="light" onPress={onCancel}>
          Cancel
        </Button>
        <Button
          size="sm"
          color="success"
          isLoading={loading}
          startContent={<Check size={14} />}
          isDisabled={!choice.trim() || !reason.trim()}
          onPress={() =>
            onSubmit({
              choice,
              reason,
              by: [handle],
              is_recommended: matchesRec,
            })
          }
        >
          Submit decision
        </Button>
      </div>
    </div>
  );
}

function ScopeOutForm({
  onSubmit,
  onCancel,
  loading,
}: {
  onSubmit: (reason: string) => void;
  onCancel: () => void;
  loading: boolean;
}) {
  const [reason, setReason] = useState("");
  return (
    <div className="border border-divider rounded-md p-3 bg-content2 flex flex-col gap-2">
      <Textarea
        label="Reason"
        value={reason}
        onValueChange={setReason}
        size="sm"
        minRows={2}
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

// ----

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="text-[11px] font-semibold uppercase tracking-wider text-default-500 mb-1.5">
      {children}
    </div>
  );
}

export default DecisionModal;
