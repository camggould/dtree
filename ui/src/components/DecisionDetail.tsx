import {
  Card,
  CardBody,
  CardHeader,
  Tabs,
  Tab,
  Chip,
  Button,
  Input,
  Select,
  SelectItem,
  Spinner,
  Divider,
} from "@heroui/react";
import { formatDistanceToNow } from "date-fns";
import { useDecision, useHistory } from "@/api/query";
import {
  useDecide,
  useUndecide,
  useScopeOut,
  useSupersede,
  useRestore,
  useRelate,
  useUnrelate,
} from "@/api/mutations";
import type { Decision, RelationshipType } from "@/api/types.gen";
import { useState } from "react";

// --- helpers ---

const statusColor: Record<string, "default" | "primary" | "secondary" | "success" | "warning" | "danger"> = {
  proposed: "warning",
  decided: "success",
  out_of_scope: "default",
  superseded: "secondary",
};

const priorityColor: Record<string, "default" | "primary" | "secondary" | "success" | "warning" | "danger"> = {
  assumption: "secondary",
  low: "default",
  medium: "primary",
  high: "warning",
  critical: "danger",
};

// --- Overview Tab ---

function OverviewTab({
  decision,
  tree,
  addToast,
}: {
  decision: Decision;
  tree: string;
  addToast?: (opts: { title: string; description?: string; color?: string }) => void;
}) {
  const id = decision.id;
  const rev = decision._rev ?? "";

  const decideMut = useDecide(tree, id, addToast);
  const undecideMut = useUndecide(tree, id, addToast);
  const scopeOutMut = useScopeOut(tree, id, addToast);
  const supersedeMut = useSupersede(tree, id, addToast);
  const restoreMut = useRestore(tree, id, addToast);

  const [decideModalOpen, setDecideModalOpen] = useState(false);
  const [decideChoice, setDecideChoice] = useState("");
  const [decideReason, setDecideReason] = useState("");
  const [scopeReason, setScopeReason] = useState("");
  const [supersedeId, setSupersedeId] = useState("");
  const [showScopeForm, setShowScopeForm] = useState(false);
  const [showSupersedeForm, setShowSupersedeForm] = useState(false);

  const { status } = decision;

  return (
    <div className="flex flex-col gap-4 p-2">
      {/* Summary */}
      <div>
        <p className="text-sm font-semibold text-default-500 mb-1">Summary</p>
        <p className="text-base">{decision.summary}</p>
      </div>

      {/* Description */}
      {decision.description && (
        <div>
          <p className="text-sm font-semibold text-default-500 mb-1">Description</p>
          <p className="text-sm text-default-700 whitespace-pre-wrap">{decision.description}</p>
        </div>
      )}

      {/* Metadata chips */}
      <div className="flex flex-wrap gap-2">
        <Chip color={statusColor[status] ?? "default"} variant="flat" size="sm">
          {status.replace(/_/g, " ")}
        </Chip>
        <Chip color={priorityColor[decision.priority] ?? "default"} variant="flat" size="sm">
          {decision.priority}
        </Chip>
        {decision.tags?.map((tag) => (
          <Chip key={tag} variant="bordered" size="sm">
            {tag}
          </Chip>
        ))}
      </div>

      {/* Creator / Assignee */}
      <div className="grid grid-cols-2 gap-2 text-sm">
        <div>
          <span className="text-default-500">Creator: </span>
          <span>{decision.creator}</span>
        </div>
        {decision.assignee && (
          <div>
            <span className="text-default-500">Assignee: </span>
            <span>{decision.assignee}</span>
          </div>
        )}
        {decision.decided_at && (
          <div>
            <span className="text-default-500">Decided: </span>
            <span>{new Date(decision.decided_at).toLocaleDateString()}</span>
          </div>
        )}
      </div>

      <Divider />

      {/* Lifecycle actions */}
      <div className="flex flex-col gap-3">
        <p className="text-sm font-semibold text-default-500">Actions</p>

        {status === "proposed" && (
          <div className="flex flex-col gap-2">
            {/* Decide form */}
            {decideModalOpen ? (
              <div className="flex flex-col gap-2 p-3 border border-default-200 rounded-lg">
                <Input
                  label="Choice"
                  size="sm"
                  value={decideChoice}
                  onChange={(e) => setDecideChoice(e.target.value)}
                  placeholder="What was decided?"
                />
                <Input
                  label="Reason"
                  size="sm"
                  value={decideReason}
                  onChange={(e) => setDecideReason(e.target.value)}
                  placeholder="Why this decision?"
                />
                <div className="flex gap-2">
                  <Button
                    size="sm"
                    color="success"
                    isLoading={decideMut.isPending}
                    onPress={() => {
                      decideMut.mutate(
                        { choice: decideChoice, reason: decideReason, by: [], ifMatch: rev },
                        { onSuccess: () => setDecideModalOpen(false) },
                      );
                    }}
                  >
                    Confirm
                  </Button>
                  <Button size="sm" variant="flat" onPress={() => setDecideModalOpen(false)}>
                    Cancel
                  </Button>
                </div>
              </div>
            ) : (
              <Button
                size="sm"
                color="success"
                variant="flat"
                onPress={() => setDecideModalOpen(true)}
              >
                Decide
              </Button>
            )}

            {/* Scope Out form */}
            {showScopeForm ? (
              <div className="flex flex-col gap-2 p-3 border border-default-200 rounded-lg">
                <Input
                  label="Reason"
                  size="sm"
                  value={scopeReason}
                  onChange={(e) => setScopeReason(e.target.value)}
                  placeholder="Why out of scope?"
                />
                <div className="flex gap-2">
                  <Button
                    size="sm"
                    color="warning"
                    isLoading={scopeOutMut.isPending}
                    onPress={() => {
                      scopeOutMut.mutate(
                        { reason: scopeReason, ifMatch: rev },
                        { onSuccess: () => setShowScopeForm(false) },
                      );
                    }}
                  >
                    Confirm
                  </Button>
                  <Button size="sm" variant="flat" onPress={() => setShowScopeForm(false)}>
                    Cancel
                  </Button>
                </div>
              </div>
            ) : (
              <Button size="sm" color="warning" variant="flat" onPress={() => setShowScopeForm(true)}>
                Scope Out
              </Button>
            )}
          </div>
        )}

        {status === "decided" && (
          <div className="flex flex-col gap-2">
            <Button
              size="sm"
              color="default"
              variant="flat"
              isLoading={undecideMut.isPending}
              onPress={() => undecideMut.mutate(rev)}
            >
              Undecide
            </Button>

            {/* Supersede form */}
            {showSupersedeForm ? (
              <div className="flex flex-col gap-2 p-3 border border-default-200 rounded-lg">
                <Input
                  label="Superseding decision ID"
                  size="sm"
                  value={supersedeId}
                  onChange={(e) => setSupersedeId(e.target.value)}
                  placeholder="ID of new decision"
                />
                <div className="flex gap-2">
                  <Button
                    size="sm"
                    color="secondary"
                    isLoading={supersedeMut.isPending}
                    onPress={() => {
                      supersedeMut.mutate(
                        { by: supersedeId, ifMatch: rev },
                        { onSuccess: () => setShowSupersedeForm(false) },
                      );
                    }}
                  >
                    Confirm
                  </Button>
                  <Button size="sm" variant="flat" onPress={() => setShowSupersedeForm(false)}>
                    Cancel
                  </Button>
                </div>
              </div>
            ) : (
              <Button
                size="sm"
                color="secondary"
                variant="flat"
                onPress={() => setShowSupersedeForm(true)}
              >
                Supersede
              </Button>
            )}
          </div>
        )}

        {status === "out_of_scope" && (
          <div className="flex flex-col gap-2">
            <Button
              size="sm"
              color="primary"
              variant="flat"
              isLoading={restoreMut.isPending}
              onPress={() => restoreMut.mutate(rev)}
            >
              Restore
            </Button>

            {/* Supersede form */}
            {showSupersedeForm ? (
              <div className="flex flex-col gap-2 p-3 border border-default-200 rounded-lg">
                <Input
                  label="Superseding decision ID"
                  size="sm"
                  value={supersedeId}
                  onChange={(e) => setSupersedeId(e.target.value)}
                  placeholder="ID of new decision"
                />
                <div className="flex gap-2">
                  <Button
                    size="sm"
                    color="secondary"
                    isLoading={supersedeMut.isPending}
                    onPress={() => {
                      supersedeMut.mutate(
                        { by: supersedeId, ifMatch: rev },
                        { onSuccess: () => setShowSupersedeForm(false) },
                      );
                    }}
                  >
                    Confirm
                  </Button>
                  <Button size="sm" variant="flat" onPress={() => setShowSupersedeForm(false)}>
                    Cancel
                  </Button>
                </div>
              </div>
            ) : (
              <Button
                size="sm"
                color="secondary"
                variant="flat"
                onPress={() => setShowSupersedeForm(true)}
              >
                Supersede
              </Button>
            )}
          </div>
        )}

        {status === "superseded" && (
          <p className="text-sm text-default-400 italic">No lifecycle actions available.</p>
        )}
      </div>
    </div>
  );
}

// --- History Tab ---

const actionColor: Record<string, "default" | "primary" | "secondary" | "success" | "warning" | "danger"> = {
  create: "primary",
  update: "default",
  decide: "success",
  undecide: "warning",
  scope_out: "warning",
  supersede: "secondary",
  restore: "primary",
  relate: "default",
  unrelate: "default",
  delete: "danger",
};

function HistoryTab({ tree, id }: { tree: string; id: string }) {
  const { data: events, isLoading, isError } = useHistory(tree, id);

  if (isLoading) return <div className="flex justify-center p-8"><Spinner /></div>;
  if (isError) return <p className="text-danger p-4">Failed to load history.</p>;
  if (!events?.length) return <p className="text-default-400 p-4">No history yet.</p>;

  return (
    <div className="flex flex-col gap-3 p-2">
      {events.map((ev) => (
        <Card key={ev.event_id} shadow="none" className="border border-default-100">
          <CardBody className="flex flex-row items-center gap-3 py-2 px-3">
            <Chip
              size="sm"
              color={actionColor[ev.action] ?? "default"}
              variant="flat"
            >
              {ev.action.replace(/_/g, " ")}
            </Chip>
            <span className="text-sm text-default-600">{ev.actor}</span>
            <span className="text-xs text-default-400 ml-auto">
              {formatDistanceToNow(new Date(ev.ts), { addSuffix: true })}
            </span>
          </CardBody>
        </Card>
      ))}
    </div>
  );
}

// --- Relationships Tab ---

function RelationshipsTab({
  decision,
  tree,
  addToast,
}: {
  decision: Decision;
  tree: string;
  addToast?: (opts: { title: string; description?: string; color?: string }) => void;
}) {
  const id = decision.id;
  const rev = decision._rev ?? "";
  const relateMut = useRelate(tree, id, addToast);
  const unrelateMut = useUnrelate(tree, id, addToast);

  const [newTarget, setNewTarget] = useState("");
  const [newType, setNewType] = useState<RelationshipType>("relates_to");
  const [newNote, setNewNote] = useState("");

  const relationships = decision.relationships ?? [];

  return (
    <div className="flex flex-col gap-4 p-2">
      {/* Existing relationships */}
      {relationships.length === 0 ? (
        <p className="text-default-400 text-sm">No relationships yet.</p>
      ) : (
        <div className="flex flex-col gap-2">
          {relationships.map((rel) => (
            <Card
              key={`${rel.type}:${rel.target}`}
              shadow="none"
              className="border border-default-100"
            >
              <CardBody className="flex flex-row items-center gap-3 py-2 px-3">
                <Chip size="sm" variant="bordered">
                  {rel.type.replace(/_/g, " ")}
                </Chip>
                <span className="text-sm font-mono">{rel.target}</span>
                {rel.note && <span className="text-xs text-default-400">{rel.note}</span>}
                <Button
                  size="sm"
                  color="danger"
                  variant="light"
                  className="ml-auto"
                  isLoading={unrelateMut.isPending}
                  onPress={() =>
                    unrelateMut.mutate({ type: rel.type, target: rel.target, ifMatch: rev })
                  }
                >
                  Remove
                </Button>
              </CardBody>
            </Card>
          ))}
        </div>
      )}

      {/* Add relationship form */}
      <div className="flex flex-col gap-2 p-3 border border-default-200 rounded-lg">
        <p className="text-sm font-semibold text-default-500">Add Relationship</p>
        <Input
          size="sm"
          label="Target decision ID"
          value={newTarget}
          onChange={(e) => setNewTarget(e.target.value)}
          placeholder="e.g. abc123"
        />
        <Select
          size="sm"
          label="Type"
          selectedKeys={[newType]}
          onSelectionChange={(keys) => {
            const val = Array.from(keys)[0] as RelationshipType;
            if (val) setNewType(val);
          }}
        >
          {(["blocks", "influences", "supersedes", "relates_to"] as RelationshipType[]).map(
            (t) => (
              <SelectItem key={t}>{t.replace(/_/g, " ")}</SelectItem>
            ),
          )}
        </Select>
        <Input
          size="sm"
          label="Note (optional)"
          value={newNote}
          onChange={(e) => setNewNote(e.target.value)}
        />
        <Button
          size="sm"
          color="primary"
          isLoading={relateMut.isPending}
          isDisabled={!newTarget}
          onPress={() => {
            relateMut.mutate(
              { type: newType, target: newTarget, note: newNote || undefined, ifMatch: rev },
              {
                onSuccess: () => {
                  setNewTarget("");
                  setNewNote("");
                  setNewType("relates_to");
                },
              },
            );
          }}
        >
          Add
        </Button>
      </div>
    </div>
  );
}

// --- Audit Tab (stub) ---

function AuditTab({ tree, id }: { tree: string; id: string }) {
  return (
    <div className="p-4 text-default-400 text-sm">
      Per-decision audit flowchart coming in lth.13.
      <br />
      Tree: {tree} / Decision: {id}
    </div>
  );
}

// --- Main DecisionDetail component ---

export interface DecisionDetailProps {
  tree: string;
  id: string;
  addToast?: (opts: { title: string; description?: string; color?: string }) => void;
}

export function DecisionDetail({ tree, id, addToast }: DecisionDetailProps) {
  const { data: decision, isLoading, isError } = useDecision(tree, id);

  if (isLoading) {
    return (
      <div className="flex justify-center items-center p-16">
        <Spinner size="lg" />
      </div>
    );
  }

  if (isError || !decision) {
    return (
      <Card>
        <CardBody>
          <p className="text-danger">Failed to load decision.</p>
        </CardBody>
      </Card>
    );
  }

  return (
    <Card className="w-full" data-testid="decision-detail">
      <CardHeader className="flex flex-col items-start gap-1 pb-0">
        <p className="text-xs font-mono text-default-400">{decision.id}</p>
        <h2 className="text-lg font-semibold">{decision.summary}</h2>
      </CardHeader>
      <CardBody className="pt-2">
        <Tabs aria-label="Decision detail tabs" variant="underlined">
          <Tab key="overview" title="Overview">
            <OverviewTab decision={decision} tree={tree} addToast={addToast} />
          </Tab>
          <Tab key="history" title="History">
            <HistoryTab tree={tree} id={id} />
          </Tab>
          <Tab key="relationships" title="Relationships">
            <RelationshipsTab decision={decision} tree={tree} addToast={addToast} />
          </Tab>
          <Tab key="audit" title="Audit">
            <AuditTab tree={tree} id={id} />
          </Tab>
        </Tabs>
      </CardBody>
    </Card>
  );
}
