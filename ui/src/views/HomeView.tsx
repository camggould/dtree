import { useMemo } from "react";
import { Link, useLocation } from "wouter";
import {
  Card,
  CardBody,
  CardHeader,
  Chip,
  Spinner,
  Button,
  Code,
} from "@heroui/react";
import {
  TreeDeciduous,
  ArrowRight,
  Activity,
  ListChecks,
  Zap,
  PlusCircle,
} from "lucide-react";
import { formatDistanceToNow } from "date-fns";
import { useTrees, useAuditList, useAllDecisions, useActors } from "@/api/query";
import { useAppStore } from "@/store/app";
import {
  humanStatus,
  statusColor,
  humanAction,
} from "@/util/labels";

export function HomeView() {
  const treesQuery = useTrees();
  const trees = treesQuery.data ?? [];
  const treeSlugs = useMemo(() => trees.map((t) => t.slug), [trees]);
  const { decisions } = useAllDecisions(treeSlugs);
  const actorsQuery = useActors();

  if (treesQuery.isLoading) {
    return (
      <div className="p-12 flex justify-center">
        <Spinner size="lg" />
      </div>
    );
  }

  // First-run state — no trees yet.
  if (trees.length === 0) {
    return <EmptyState />;
  }

  return (
    <div className="p-6 max-w-6xl mx-auto space-y-6">
      <header>
        <h1 className="text-3xl font-bold mb-1">Welcome back</h1>
        <p className="text-default-500">
          {trees.length} tree{trees.length === 1 ? "" : "s"} ·{" "}
          {decisions.length} decision{decisions.length === 1 ? "" : "s"} ·{" "}
          {actorsQuery.data?.length ?? 0} actor
          {actorsQuery.data?.length === 1 ? "" : "s"}
        </p>
      </header>

      <TreesGrid trees={trees} decisions={decisions} />

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <RecentActivity decisions={decisions} actors={actorsQuery.data} />
        <ProposedSummary decisions={decisions} />
      </div>
    </div>
  );
}

// ---------- First-run / empty state ---------------------------------------

function EmptyState() {
  return (
    <div className="p-12 max-w-3xl mx-auto">
      <Card>
        <CardHeader className="flex items-center gap-3">
          <TreeDeciduous size={28} className="text-primary" />
          <div>
            <h1 className="text-2xl font-bold">Welcome to dtree</h1>
            <p className="text-sm text-default-500">No trees yet — let's get one going.</p>
          </div>
        </CardHeader>
        <CardBody className="gap-4">
          <p className="text-sm">
            A <strong>tree</strong> is a named collection of decisions —
            usually one per workstream or system area. Create your first
            one from the CLI:
          </p>
          <Code className="block p-3 text-sm">
            dtree tree create backend --title "Backend Architecture"
          </Code>
          <p className="text-sm text-default-500">
            Then come back and refresh, or browse the <Link href="/settings"><span className="text-primary hover:underline cursor-pointer">Settings</span></Link>{" "}
            page to pick an identity.
          </p>
        </CardBody>
      </Card>
    </div>
  );
}

// ---------- Trees grid ----------------------------------------------------

function TreesGrid({
  trees,
  decisions,
}: {
  trees: import("@/api/types.gen").Tree[];
  decisions: import("@/api/types.gen").Decision[];
}) {
  const byTree = useMemo(() => {
    const m = new Map<string, { proposed: number; decided: number; total: number }>();
    for (const t of trees) m.set(t.slug, { proposed: 0, decided: 0, total: 0 });
    for (const d of decisions) {
      const e = m.get(d.tree);
      if (!e) continue;
      e.total += 1;
      if (d.status === "proposed") e.proposed += 1;
      if (d.status === "decided") e.decided += 1;
    }
    return m;
  }, [trees, decisions]);

  return (
    <section>
      <SectionHeader icon={<TreeDeciduous size={18} />} label="Trees" />
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {trees.map((t) => {
          const stats = byTree.get(t.slug) ?? { proposed: 0, decided: 0, total: 0 };
          return (
            <Link key={t.slug} href={`/trees/${t.slug}`}>
              <Card isPressable className="w-full">
                <CardBody className="gap-2">
                  <div className="flex items-start justify-between gap-2">
                    <div>
                      <div className="font-semibold text-foreground">
                        {t.title ?? t.slug}
                      </div>
                      <div className="text-xs text-default-500 font-mono">
                        {t.slug}
                      </div>
                    </div>
                    <ArrowRight
                      size={16}
                      className="text-default-400 mt-1 shrink-0"
                    />
                  </div>
                  {t.description && (
                    <p className="text-sm text-default-600 line-clamp-2">
                      {t.description}
                    </p>
                  )}
                  <div className="flex flex-wrap gap-1.5 pt-1">
                    <Chip size="sm" variant="flat">
                      {stats.total} total
                    </Chip>
                    {stats.proposed > 0 && (
                      <Chip size="sm" variant="flat" color="primary">
                        {stats.proposed} proposed
                      </Chip>
                    )}
                    {stats.decided > 0 && (
                      <Chip size="sm" variant="flat" color="success">
                        {stats.decided} decided
                      </Chip>
                    )}
                  </div>
                </CardBody>
              </Card>
            </Link>
          );
        })}
      </div>
    </section>
  );
}

// ---------- Recent activity ----------------------------------------------

function RecentActivity({
  decisions,
  actors,
}: {
  decisions: import("@/api/types.gen").Decision[];
  actors: import("@/api/types.gen").Actor[] | undefined;
}) {
  const { data, isLoading } = useAuditList("", {
    limit: "8",
    order: "desc",
  });
  const openDecision = useAppStore((s) => s.openDecision);
  const [, navigate] = useLocation();
  const events = data?.items ?? [];

  // Lookups: by id for relate target/src, by handle for "from <name>".
  const decisionsById = useMemo(() => {
    const m = new Map<string, import("@/api/types.gen").Decision>();
    for (const d of decisions) m.set(d.id, d);
    return m;
  }, [decisions]);
  const actorsByHandle = useMemo(() => {
    const m = new Map<string, import("@/api/types.gen").Actor>();
    for (const a of actors ?? []) m.set(a.handle, a);
    return m;
  }, [actors]);

  const actorLabel = (handle: string): string => {
    const a = actorsByHandle.get(handle);
    if (!a) return handle;
    const display = a.name || a.handle;
    return a.kind === "agent" ? `${display} (agent)` : display;
  };

  return (
    <Card>
      <CardHeader className="flex items-center gap-2">
        <Activity size={18} />
        <h2 className="text-base font-semibold">Recent activity</h2>
      </CardHeader>
      <CardBody>
        {isLoading ? (
          <Spinner size="sm" />
        ) : events.length === 0 ? (
          <p className="text-sm text-default-400">Nothing recent.</p>
        ) : (
          <div className="flex flex-col gap-2">
            {events.slice(0, 8).map((e) => {
              const payload = (e.payload ?? {}) as Record<string, unknown>;
              const after = payload.after as
                | Record<string, unknown>
                | undefined;

              // Tree events: slug is in e.id (Kind=tree, ID=slug).
              const treeSlug =
                e.kind === "tree"
                  ? (e.id || (after?.slug as string | undefined) || "")
                  : "";
              const treeTitle =
                (after?.title as string | undefined) || treeSlug;

              const isDecisionClick =
                e.kind === "decision" && Boolean(e.tree) && Boolean(e.id);
              const isTreeClick =
                e.kind === "tree" && Boolean(treeSlug);
              const isRelateClick =
                e.action === "relate" && Boolean(e.tree) && Boolean(e.id);
              const clickable =
                isDecisionClick || isTreeClick || isRelateClick;

              const handleClick = () => {
                if (isDecisionClick || isRelateClick) {
                  openDecision(e.tree!, e.id);
                } else if (isTreeClick) {
                  navigate(`/trees/${treeSlug}`);
                }
              };

              // Headline: action-specific. Sub-line: extra context.
              const { primary, secondary } = describeEvent(e, {
                after,
                payload,
                treeTitle,
                decisionsById,
                actorLabel,
              });

              return (
                <button
                  key={e.event_id}
                  type="button"
                  disabled={!clickable}
                  onClick={handleClick}
                  className={`text-left text-sm p-2 -mx-2 rounded ${
                    clickable
                      ? "hover:bg-default-100 cursor-pointer"
                      : ""
                  }`}
                >
                  <div className="flex items-center gap-2 flex-wrap">
                    <Chip size="sm" variant="flat" color="primary">
                      {humanAction(e.action)}
                    </Chip>
                    <span className="font-medium text-foreground">
                      {e.actor}
                    </span>
                    {primary && (
                      <span className="text-default-600 truncate">
                        — {primary}
                      </span>
                    )}
                  </div>
                  {secondary && (
                    <div className="text-xs text-default-500 mt-0.5 italic">
                      {secondary}
                    </div>
                  )}
                  <div className="text-xs text-default-400 mt-0.5">
                    {formatDistanceToNow(new Date(e.ts))} ago
                    {e.tree && <span> · in {e.tree}</span>}
                    {!e.tree && e.kind === "tree" && treeSlug && (
                      <span> · {treeSlug}</span>
                    )}
                  </div>
                </button>
              );
            })}
          </div>
        )}
      </CardBody>
    </Card>
  );
}

// describeEvent returns the action-specific headline (primary) and a
// reasoning line (secondary) for an audit event. Kept outside the
// component so it can be exercised by future tests.
function describeEvent(
  e: import("@/api/types.gen").Event,
  ctx: {
    after: Record<string, unknown> | undefined;
    payload: Record<string, unknown>;
    treeTitle: string;
    decisionsById: Map<string, import("@/api/types.gen").Decision>;
    actorLabel: (handle: string) => string;
  },
): { primary: string | null; secondary: string | null } {
  const { after, payload, treeTitle, decisionsById, actorLabel } = ctx;
  const summary = (after?.summary as string | undefined) ?? null;

  switch (e.action) {
    case "decide": {
      const choice = (after?.actual_choice as string | undefined) ?? null;
      const reason =
        (after?.actual_choice_reason as string | undefined) ?? null;
      const isRec = Boolean(after?.is_recommended);
      const recBy = (after?.recommended_by as string | undefined) ?? null;

      const parts: string[] = [];
      if (choice) parts.push(`chose “${choice}”`);
      if (isRec && recBy) {
        parts.push(`accepted recommendation from ${actorLabel(recBy)}`);
      } else if (reason) {
        parts.push(reason);
      }
      return { primary: summary, secondary: parts.join(" — ") || null };
    }

    case "relate":
    case "unrelate": {
      const srcId = (payload.src as string | undefined) ?? e.id;
      const targetId = (payload.target as string | undefined) ?? "";
      const type = (payload.type as string | undefined) ?? "relates_to";
      const srcSummary =
        decisionsById.get(srcId)?.summary ?? shortId(srcId);
      const targetSummary =
        decisionsById.get(targetId)?.summary ?? shortId(targetId);
      const verb = e.action === "unrelate" ? `un-${type}` : type;
      return {
        primary: `${srcSummary} ${verb.replace(/_/g, " ")} ${targetSummary}`,
        secondary: null,
      };
    }

    case "supersede": {
      const oldId = (payload.old as string | undefined) ?? e.id;
      const newId = (payload.new as string | undefined) ?? "";
      const oldSummary =
        decisionsById.get(oldId)?.summary ?? summary ?? shortId(oldId);
      const newSummary =
        decisionsById.get(newId)?.summary ?? shortId(newId);
      return {
        primary: `${oldSummary} superseded by ${newSummary}`,
        secondary: null,
      };
    }

    case "scope_out": {
      const reason = (payload.reason as string | undefined) ?? null;
      return { primary: summary, secondary: reason };
    }

    case "tree_create":
    case "tree_rename":
    case "tree_archive":
    case "tree_delete":
      return { primary: treeTitle || null, secondary: null };

    default:
      return { primary: summary, secondary: null };
  }
}

function shortId(id: string): string {
  return id ? id.slice(0, 8) : "(unknown)";
}

// ---------- Proposed-summary panel ---------------------------------------

function ProposedSummary({
  decisions,
}: {
  decisions: import("@/api/types.gen").Decision[];
}) {
  const openDecision = useAppStore((s) => s.openDecision);
  const proposed = decisions.filter((d) => d.status === "proposed");
  const top = proposed
    .sort((a, b) => priorityRank(b.priority) - priorityRank(a.priority))
    .slice(0, 6);

  return (
    <Card>
      <CardHeader className="flex items-center gap-2">
        <ListChecks size={18} />
        <h2 className="text-base font-semibold">Open decisions</h2>
        <Chip size="sm" variant="flat" className="ml-auto">
          {proposed.length}
        </Chip>
      </CardHeader>
      <CardBody>
        {proposed.length === 0 ? (
          <div className="flex items-center gap-2 text-sm text-default-500">
            <Zap size={16} className="text-success" />
            Nothing waiting on you. Inbox zero.
          </div>
        ) : (
          <div className="flex flex-col gap-2">
            {top.map((d) => (
              <button
                key={d.id}
                type="button"
                onClick={() => openDecision(d.tree, d.id)}
                className="text-left p-2 -mx-2 rounded hover:bg-default-100 cursor-pointer"
              >
                <div className="flex items-start gap-2">
                  <Chip
                    size="sm"
                    variant="flat"
                    color={priorityChipColor(d.priority)}
                    className="shrink-0 mt-0.5"
                  >
                    {d.priority}
                  </Chip>
                  <div className="flex-1 min-w-0">
                    <div className="text-sm font-medium text-foreground line-clamp-2">
                      {d.summary}
                    </div>
                    <div className="text-xs text-default-400 mt-0.5">
                      {d.tree} · by {d.creator}
                      {d.recommended_by && <> · rec {d.recommended_by}</>}
                    </div>
                  </div>
                </div>
              </button>
            ))}
            {proposed.length > top.length && (
              <Link href={`/trees/${top[0].tree}/queue/quick-wins`}>
                <Button
                  size="sm"
                  variant="light"
                  startContent={<PlusCircle size={14} />}
                  className="self-start mt-1"
                >
                  See all in queue
                </Button>
              </Link>
            )}
          </div>
        )}
      </CardBody>
    </Card>
  );
}

// ---------- helpers -------------------------------------------------------

function SectionHeader({
  icon,
  label,
}: {
  icon: React.ReactNode;
  label: string;
}) {
  return (
    <div className="flex items-center gap-2 mb-3">
      <span className="text-default-500">{icon}</span>
      <h2 className="text-sm font-semibold uppercase tracking-wider text-default-500">
        {label}
      </h2>
    </div>
  );
}

const PRIORITY_RANK: Record<string, number> = {
  critical: 5,
  high: 4,
  medium: 3,
  low: 2,
  assumption: 1,
};
function priorityRank(p: string): number {
  return PRIORITY_RANK[p] ?? 0;
}
function priorityChipColor(
  p: string,
): "danger" | "warning" | "primary" | "default" | "success" {
  switch (p) {
    case "critical":
      return "danger";
    case "high":
      return "warning";
    case "medium":
      return "primary";
    case "low":
      return "default";
    case "assumption":
      return "success";
    default:
      return "default";
  }
}

// keep imports tidy
void statusColor;
void humanStatus;

export default HomeView;
