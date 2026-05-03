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
} from "@heroui/react";
import { Check, X, Sparkles } from "lucide-react";
import { useDecision, useHistory } from "@/api/query";
import {
  useDecide,
  useUndecide,
  useScopeOut,
  useRestore,
} from "@/api/mutations";
import { useAppStore } from "@/store/app";
import { humanStatus, statusColor, humanPriority, humanAction } from "@/util/labels";
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
        <div className="flex items-start justify-between gap-3">
          <h2 className="text-xl font-semibold leading-tight">
            {decision.summary}
          </h2>
          <div className="flex items-center gap-1 shrink-0">
            <Chip size="sm" variant="flat" color={statusColor(decision.status)}>
              {humanStatus(decision.status)}
            </Chip>
            <Chip size="sm" variant="flat">
              {humanPriority(decision.priority)}
            </Chip>
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
            <HistoryTab tree={tree} id={id} />
          </Tab>
          <Tab key="audit" title="Audit flow">
            <AuditFlow tree={tree} id={id} />
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
  return (
    <div className="flex flex-col gap-4 py-2">
      {decision.description && (
        <section>
          <SectionLabel>Description</SectionLabel>
          <p className="text-sm whitespace-pre-wrap">{decision.description}</p>
        </section>
      )}

      {recExists && (
        <RecommendationBlock decision={decision} handle={handle} />
      )}

      {decision.status === "decided" && decision.actual_choice && (
        <section>
          <SectionLabel>Outcome</SectionLabel>
          <Card className="bg-success-50 dark:bg-success-900/40 border border-success-300 dark:border-success-600">
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
        <section>
          <SectionLabel>Relationships</SectionLabel>
          <div className="flex flex-col gap-1">
            {(decision.relationships ?? []).map((r) => (
              <div
                key={`${r.type}-${r.target}`}
                className="text-sm flex gap-2 items-center"
              >
                <Chip size="sm" variant="flat" color="warning">
                  {r.type.replace("_", " ")}
                </Chip>
                <span className="font-mono text-xs text-default-500">
                  {r.target.slice(0, 8)}
                </span>
              </div>
            ))}
          </div>
        </section>
      )}
    </div>
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
      <Card className="bg-primary-50 dark:bg-primary-900/40 border border-primary-300 dark:border-primary-600">
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

function HistoryTab({ tree, id }: { tree: string; id: string }) {
  const { data: events, isLoading } = useHistory(tree, id);
  if (isLoading) return <Spinner size="sm" />;
  const list = events ?? [];
  if (list.length === 0)
    return <p className="text-default-500 text-sm py-4">No events</p>;
  return (
    <div className="flex flex-col gap-2 py-2">
      {list.map((e) => {
        const after = (e.payload?.after ?? {}) as Record<string, unknown>;
        const before = (e.payload?.before ?? {}) as Record<string, unknown>;
        const summary = describeEvent(e.action, after, before);
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
                  <span className="text-foreground/70"> — {summary}</span>
                )}
              </div>
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

/** Build a one-line plain-language summary for an audit event's payload. */
function describeEvent(
  action: string,
  after: Record<string, unknown>,
  _before: Record<string, unknown>,
): string | null {
  if (action === "decide") {
    const choice = after.actual_choice as string | undefined;
    const recommended = after.recommended_summary as string | undefined;
    const isRec = after.is_recommended as boolean | undefined;
    if (!choice) return null;
    if (recommended) {
      const accepted = isRec === true || choice === recommended;
      return accepted
        ? `chose “${choice}” (followed recommendation)`
        : `chose “${choice}” (overrode recommendation “${recommended}”)`;
    }
    return `chose “${choice}” (no recommendation existed)`;
  }
  if (action === "scope_out") {
    const reason = after.scope_out_reason as string | undefined;
    return reason ? `reason: ${reason}` : null;
  }
  if (action === "supersede") {
    const by = after.superseded_by as string | undefined;
    return by ? `replaced by ${by.slice(0, 8)}` : null;
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

  const buttonsForStatus = (() => {
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
