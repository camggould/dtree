import { useMemo, useState } from "react";
import { useParams } from "wouter";
import {
  Card,
  CardBody,
  CardHeader,
  Spinner,
  Chip,
  Button,
  Progress,
  Tooltip,
} from "@heroui/react";
import {
  PieChart,
  Pie,
  Cell,
  Tooltip as RTooltip,
  ResponsiveContainer,
} from "recharts";
import { ArrowLeft, Info } from "lucide-react";
import { useTrees, useAllDecisions, useActors } from "@/api/query";
import {
  computeUserStats,
  decisionsForUserBucket,
  type CreatorFacet,
  type RecommenderFacet,
  type DeciderFacet,
  type RateStat,
  type UserBucket,
} from "@/analytics/insights";
import { TreeFilter } from "@/components/TreeFilter";
import { DecisionListModal } from "@/components/DecisionListModal";
import { useAppStore } from "@/store/app";
import { humanStatus } from "@/util/labels";
import type { Decision } from "@/api/types.gen";

function pct(rate: number | null): string {
  return rate === null ? "—" : `${rate.toFixed(1)}%`;
}

// ---------- Drill-in plumbing ------------------------------------------------

interface DrillState {
  bucket: UserBucket;
  title: string;
  description?: string;
}

// ---------- Reusable display primitives -------------------------------------

function StackedBar({
  segments,
  onSegmentClick,
}: {
  segments: Array<{
    label: string;
    value: number;
    color: string;
    onClick?: () => void;
  }>;
  onSegmentClick?: (label: string) => void;
}) {
  const total = segments.reduce((a, s) => a + s.value, 0);
  if (total === 0) {
    return <div className="text-default-400 text-sm py-2">No data</div>;
  }
  return (
    <div>
      <div className="flex h-5 w-full rounded-md overflow-hidden">
        {segments.map((s) =>
          s.value === 0 ? null : (
            <Tooltip
              key={s.label}
              content={`${s.label}: ${s.value} (${((s.value / total) * 100).toFixed(0)}%) — click to view`}
            >
              <button
                type="button"
                style={{
                  width: `${(s.value / total) * 100}%`,
                  background: s.color,
                  border: "none",
                  cursor: s.onClick || onSegmentClick ? "pointer" : "default",
                }}
                onClick={() => {
                  if (s.onClick) s.onClick();
                  else if (onSegmentClick) onSegmentClick(s.label);
                }}
              />
            </Tooltip>
          ),
        )}
      </div>
      <div className="mt-2 flex flex-wrap gap-3 text-xs">
        {segments.map((s) => (
          <button
            key={s.label}
            type="button"
            className="flex items-center gap-1.5 cursor-pointer hover:bg-default-100 rounded px-1 -mx-1"
            onClick={() => {
              if (s.onClick) s.onClick();
              else if (onSegmentClick) onSegmentClick(s.label);
            }}
          >
            <span
              className="inline-block w-2.5 h-2.5 rounded-sm"
              style={{ background: s.color }}
            />
            <span className="text-default-600">{s.label}</span>
            <span className="font-semibold">{s.value}</span>
            <span className="text-default-400">
              ({total === 0 ? 0 : ((s.value / total) * 100).toFixed(0)}%)
            </span>
          </button>
        ))}
      </div>
    </div>
  );
}

function PieBreakdown({
  segments,
  height = 200,
}: {
  segments: Array<{
    label: string;
    value: number;
    color: string;
    onClick?: () => void;
  }>;
  height?: number;
}) {
  const data = segments.filter((s) => s.value > 0);
  if (data.length === 0) {
    return <div className="text-default-400 text-sm py-2">No data</div>;
  }
  return (
    <ResponsiveContainer width="100%" height={height}>
      <PieChart>
        <Pie
          data={data}
          cx="50%"
          cy="50%"
          outerRadius={70}
          dataKey="value"
          label={({ payload, percent }) =>
            `${(payload as { label?: string })?.label ?? ""} ${(((percent as number | undefined) ?? 0) * 100).toFixed(0)}%`
          }
          onClick={(_, idx) => {
            const seg = data[idx ?? -1];
            if (seg?.onClick) seg.onClick();
          }}
          style={{ cursor: "pointer" }}
        >
          {data.map((s) => (
            <Cell key={s.label} fill={s.color} />
          ))}
        </Pie>
        <RTooltip
          formatter={(v: unknown, _n: unknown, p: unknown) => {
            const payload = (p as { payload?: { label?: string } })?.payload;
            return [String(v), payload?.label ?? ""] as [string, string];
          }}
        />
      </PieChart>
    </ResponsiveContainer>
  );
}

function MetricBlock({
  label,
  value,
  subtext,
  color,
  onClick,
}: {
  label: string;
  value: string | number;
  subtext?: string;
  color?: "success" | "default" | "primary";
  onClick?: () => void;
}) {
  const cls =
    color === "success"
      ? "text-success"
      : color === "primary"
        ? "text-primary"
        : "";
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={!onClick}
      className={`flex flex-col items-center text-center rounded-md p-2 transition-colors ${
        onClick ? "hover:bg-default-100 cursor-pointer" : ""
      }`}
    >
      <span className={`text-3xl font-bold ${cls}`}>{value}</span>
      <span className="text-xs text-default-500 mt-1">{label}</span>
      {subtext && <span className="text-xs text-default-400">{subtext}</span>}
    </button>
  );
}

function RateRow({
  label,
  stat,
  color,
  onClick,
}: {
  label: string;
  stat: RateStat;
  color: "primary" | "secondary" | "default";
  onClick?: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={!onClick}
      className={`flex items-center gap-3 w-full rounded-md p-1 transition-colors ${
        onClick ? "hover:bg-default-100 cursor-pointer" : ""
      }`}
    >
      <div className="min-w-32 text-sm text-left">{label}</div>
      <Progress
        aria-label={label}
        value={stat.rate ?? 0}
        color={color}
        size="md"
        className="flex-1"
      />
      <div className="min-w-32 text-sm text-right tabular-nums">
        <span className="font-semibold">{pct(stat.rate)}</span>
        <span className="text-default-400 ml-2">
          ({stat.accepted}/{stat.total})
        </span>
      </div>
    </button>
  );
}

// ---------- Facet sections --------------------------------------------------

function CreatorSection({
  f,
  handle,
  drill,
}: {
  f: CreatorFacet;
  handle: string;
  drill: (b: UserBucket, title: string, description?: string) => void;
}) {
  const statusKeys: Array<{ key: string; color: string }> = [
    { key: "proposed", color: "#3b82f6" },
    { key: "decided", color: "#22c55e" },
    { key: "out_of_scope", color: "#6b7280" },
    { key: "superseded", color: "#f97316" },
  ];

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-2">
          <h2 className="text-lg font-semibold">
            <span className="text-primary">Created</span> by {handle}
          </h2>
          <Tooltip content="Decisions this person opened. Creating a decision doesn't automatically attach a recommendation — that's a separate field.">
            <Info size={14} className="text-default-400 cursor-help" />
          </Tooltip>
        </div>
      </CardHeader>
      <CardBody className="gap-4">
        <div className="grid grid-cols-3 gap-2">
          <MetricBlock
            label="Total opened"
            value={f.totalCreated}
            onClick={() =>
              drill(
                { facet: "creator", key: "all" },
                `Decisions opened by ${handle}`,
              )
            }
          />
          <MetricBlock
            label="Outstanding"
            value={f.outstanding}
            subtext="still proposed"
            onClick={() =>
              drill(
                { facet: "creator", key: "byStatus", status: "proposed" },
                `Outstanding decisions opened by ${handle}`,
              )
            }
          />
          <MetricBlock
            label="Resolved"
            value={f.resolved}
            subtext="decided / scoped / superseded"
          />
        </div>

        <div>
          <div className="text-sm font-medium mb-2">By status</div>
          <StackedBar
            segments={statusKeys.map((s) => ({
              label: humanStatus(s.key),
              value: f.byStatus[s.key] ?? 0,
              color: s.color,
              onClick: () =>
                drill(
                  { facet: "creator", key: "byStatus", status: s.key },
                  `${humanStatus(s.key)} (opened by ${handle})`,
                ),
            }))}
          />
        </div>

        {f.resolved > 0 && (
          <div className="pt-2 border-t border-divider">
            <div className="text-sm font-medium mb-2">
              Resolved decisions, by who recommended
            </div>
            <StackedBar
              segments={[
                {
                  label: "Self",
                  value: f.resolvedByRecSource.self,
                  color: "#64748b",
                },
                {
                  label: "Another agent",
                  value: f.resolvedByRecSource.anotherAgent,
                  color: "#a855f7",
                },
                {
                  label: "Another human",
                  value: f.resolvedByRecSource.anotherHuman,
                  color: "#3b82f6",
                },
                {
                  label: "No recommendation",
                  value: f.resolvedByRecSource.none,
                  color: "#94a3b8",
                },
              ]}
            />
          </div>
        )}
      </CardBody>
    </Card>
  );
}

function BucketBar({
  label,
  buckets,
  total,
  drill,
  facet,
}: {
  label: string;
  buckets: { self: number; anotherAgent: number; anotherHuman: number; unknown: number };
  total: number;
  drill: (b: UserBucket, title: string, description?: string) => void;
  facet: "accepted" | "overridden";
}) {
  if (total === 0) return null;
  const segments = [
    {
      label: "Self",
      value: buckets.self,
      color: "#64748b",
      bucket: "self" as const,
    },
    {
      label: "Another agent",
      value: buckets.anotherAgent,
      color: "#a855f7",
      bucket: "anotherAgent" as const,
    },
    {
      label: "Another human",
      value: buckets.anotherHuman,
      color: "#3b82f6",
      bucket: "anotherHuman" as const,
    },
    {
      label: "Unknown",
      value: buckets.unknown,
      color: "#94a3b8",
      bucket: "unknown" as const,
    },
  ];
  const drillFor = (b: typeof segments[number]) => {
    // Map BucketBar's bucket keys to UserBucket types.
    const bucketMap = {
      self: "self",
      anotherAgent: "otherAgent",
      anotherHuman: "otherHuman",
      unknown: "unknownActor",
    } as const;
    return () =>
      drill(
        {
          facet: "recommender",
          key: facet === "accepted" ? "accepted" : "overridden",
        },
        `${facet === "accepted" ? "Accepted" : "Overridden"} by ${b.label.toLowerCase()}`,
        `${b.value} of ${total} ${facet} were ${facet === "accepted" ? "followed" : "overruled"} by ${b.label.toLowerCase()}.`,
      );
    void bucketMap;
  };
  return (
    <div>
      <div className="text-sm font-medium mb-2">{label}</div>
      <StackedBar
        segments={segments
          .filter((s) => s.value > 0)
          .map((s) => ({
            label: s.label,
            value: s.value,
            color: s.color,
            onClick: drillFor(s),
          }))}
      />
    </div>
  );
}

function RecommenderSection({
  f,
  handle,
  drill,
}: {
  f: RecommenderFacet;
  handle: string;
  drill: (b: UserBucket, title: string, description?: string) => void;
}) {
  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-2">
          <h2 className="text-lg font-semibold">
            <span className="text-secondary">Recommended</span> by {handle}
          </h2>
          <Tooltip content="Suggestions this person made. Each can later be accepted or overridden — and we split that by who took the action.">
            <Info size={14} className="text-default-400 cursor-help" />
          </Tooltip>
        </div>
      </CardHeader>
      <CardBody className="gap-4">
        <div className="grid grid-cols-3 gap-2">
          <MetricBlock
            label="Suggestions made"
            value={f.totalRecommended}
            onClick={() =>
              drill(
                { facet: "recommender", key: "all" },
                `${handle}'s suggestions`,
              )
            }
          />
          <MetricBlock
            label="Accepted"
            value={f.acceptedCount}
            subtext={`of ${f.decidedCount} resolved`}
            color="success"
            onClick={() =>
              drill(
                { facet: "recommender", key: "accepted" },
                `${handle}'s suggestions that were accepted`,
              )
            }
          />
          <MetricBlock
            label="Overridden"
            value={f.overriddenCount}
            subtext={`${pct(
              f.decidedCount === 0 ? null : (f.overriddenCount / f.decidedCount) * 100,
            )}`}
            onClick={() =>
              drill(
                { facet: "recommender", key: "overridden" },
                `${handle}'s suggestions that were overridden`,
              )
            }
          />
        </div>

        {f.acceptedCount > 0 && (
          <div className="pt-2 border-t border-divider">
            <BucketBar
              label="Who accepted"
              buckets={f.acceptedBy}
              total={f.acceptedCount}
              drill={drill}
              facet="accepted"
            />
          </div>
        )}

        {f.overriddenCount > 0 && (
          <div className="pt-2 border-t border-divider">
            <BucketBar
              label="Who overrode"
              buckets={f.overriddenBy}
              total={f.overriddenCount}
              drill={drill}
              facet="overridden"
            />
          </div>
        )}
      </CardBody>
    </Card>
  );
}

function DeciderSection({
  f,
  handle,
  drill,
}: {
  f: DeciderFacet;
  handle: string;
  drill: (b: UserBucket, title: string, description?: string) => void;
}) {
  return (
    <Card className="md:col-span-2">
      <CardHeader>
        <div className="flex items-center gap-2">
          <h2 className="text-lg font-semibold">
            <span className="text-success">Decided</span> by {handle}
          </h2>
          <Tooltip content="Decisions where this person made the final call. Each one either follows the standing recommendation, overrides it with a different choice, or had no recommendation to begin with.">
            <Info size={14} className="text-default-400 cursor-help" />
          </Tooltip>
        </div>
      </CardHeader>
      <CardBody className="gap-5">
        <div className="grid grid-cols-3 gap-2">
          <MetricBlock
            label="Decisions made"
            value={f.totalDecided}
            onClick={() =>
              drill(
                { facet: "decider", key: "all" },
                `Decisions made by ${handle}`,
              )
            }
          />
          <MetricBlock
            label="Followed a recommendation"
            value={f.followedRec}
            subtext={`of ${f.followedRec + f.overrodeRec} that had one`}
            color="success"
            onClick={() =>
              drill(
                { facet: "decider", key: "followedRec" },
                `${handle} followed the recommendation`,
              )
            }
          />
          <MetricBlock
            label="Acceptance rate"
            value={pct(f.acceptanceWhenRecExisted.rate)}
            subtext="when a recommendation was on the table"
            color="success"
            onClick={() =>
              drill(
                { facet: "decider", key: "followedRec" },
                `${handle} followed the recommendation`,
              )
            }
          />
        </div>

        {/* Outcome breakdown — pie since there are 3 mutually-exclusive buckets */}
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          <div>
            <div className="text-sm font-medium mb-2">Outcome breakdown</div>
            <PieBreakdown
              segments={[
                {
                  label: "Followed",
                  value: f.followedRec,
                  color: "#22c55e",
                  onClick: () =>
                    drill(
                      { facet: "decider", key: "followedRec" },
                      `${handle} followed the recommendation`,
                    ),
                },
                {
                  label: "Overrode",
                  value: f.overrodeRec,
                  color: "#f59e0b",
                  onClick: () =>
                    drill(
                      { facet: "decider", key: "overrodeRec" },
                      `${handle} overrode the recommendation`,
                      "A recommendation existed; they chose a different outcome.",
                    ),
                },
                {
                  label: "No suggestion",
                  value: f.noRecExisted,
                  color: "#94a3b8",
                  onClick: () =>
                    drill(
                      { facet: "decider", key: "noRec" },
                      `${handle} decided without a recommendation`,
                      "Truly autonomous calls — nobody had suggested anything.",
                    ),
                },
              ]}
            />
          </div>

          {f.followedRec > 0 && (
            <div>
              <div className="text-sm font-medium mb-2">
                Who they listened to (when they followed)
              </div>
              <PieBreakdown
                segments={[
                  {
                    label: "Self",
                    value: f.followedFromSelf,
                    color: "#64748b",
                    onClick: () =>
                      drill(
                        {
                          facet: "decider",
                          key: "followedFromSource",
                          source: "self",
                        },
                        `${handle} followed their own recommendation`,
                        "Self-acceptance — they recommended and they decided.",
                      ),
                  },
                  {
                    label: "Other agent",
                    value: f.followedFromOtherAgent,
                    color: "#a855f7",
                    onClick: () =>
                      drill(
                        {
                          facet: "decider",
                          key: "followedFromSource",
                          source: "otherAgent",
                        },
                        `${handle} followed another agent's recommendation`,
                      ),
                  },
                  {
                    label: "Human",
                    value: f.followedFromHuman,
                    color: "#3b82f6",
                    onClick: () =>
                      drill(
                        {
                          facet: "decider",
                          key: "followedFromSource",
                          source: "human",
                        },
                        `${handle} followed a human's recommendation`,
                      ),
                  },
                  {
                    label: "Unknown",
                    value: f.followedFromUnknown,
                    color: "#94a3b8",
                    onClick: () =>
                      drill(
                        {
                          facet: "decider",
                          key: "followedFromSource",
                          source: "unknown",
                        },
                        `Followed an unrecognised recommender`,
                      ),
                  },
                ]}
              />
            </div>
          )}
        </div>

        {(f.bySource.self.total > 0 ||
          f.bySource.anotherAgent.total > 0 ||
          f.bySource.anotherHuman.total > 0) && (
          <div className="space-y-2 pt-2 border-t border-default-200">
            <div className="text-sm font-medium flex items-center gap-2">
              Acceptance rate, by recommender
              <Tooltip content="Of all decisions where a recommendation was on the table and this person decided, what fraction did they follow — split by whether the recommender was themselves, another agent, or a human.">
                <Info size={12} className="text-default-400 cursor-help" />
              </Tooltip>
            </div>
            {f.bySource.self.total > 0 && (
              <RateRow
                label="Their own"
                stat={f.bySource.self}
                color="default"
                onClick={() =>
                  drill(
                    { facet: "decider", key: "trustSource", source: "self" },
                    `${handle}'s decisions where they recommended themselves`,
                  )
                }
              />
            )}
            {f.bySource.anotherAgent.total > 0 && (
              <RateRow
                label="Another agent's"
                stat={f.bySource.anotherAgent}
                color="secondary"
                onClick={() =>
                  drill(
                    { facet: "decider", key: "trustSource", source: "otherAgent" },
                    `${handle}'s decisions where another agent recommended`,
                  )
                }
              />
            )}
            {f.bySource.anotherHuman.total > 0 && (
              <RateRow
                label="A human's"
                stat={f.bySource.anotherHuman}
                color="primary"
                onClick={() =>
                  drill(
                    { facet: "decider", key: "trustSource", source: "human" },
                    `${handle}'s decisions where a human recommended`,
                  )
                }
              />
            )}
          </div>
        )}

        {f.agenticAutonomy.total > 0 && (
          <div className="pt-3 border-t border-divider flex items-center justify-between gap-4">
            <div>
              <div className="text-sm font-medium flex items-center gap-2">
                Agentic autonomy
                <Tooltip content="Of all decisions where an agent recommended a choice and this person decided, what fraction did they follow? High = comfortable delegating to AI.">
                  <Info size={12} className="text-default-400 cursor-help" />
                </Tooltip>
              </div>
              <div className="text-xs text-default-500 mt-0.5">
                {f.agenticAutonomy.accepted} of {f.agenticAutonomy.total} agent recommendations followed
              </div>
            </div>
            <div className="text-3xl font-bold text-success">
              {pct(f.agenticAutonomy.rate)}
            </div>
          </div>
        )}
      </CardBody>
    </Card>
  );
}

// ---------- Main view -------------------------------------------------------

export function UserDrillDown() {
  const params = useParams<{ handle: string }>();
  const handle = params.handle;

  // Tree filter (shared with Dashboard)
  const treesQuery = useTrees();
  const allSlugs = useMemo(
    () => treesQuery.data?.map((t) => t.slug) ?? [],
    [treesQuery.data],
  );
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const activeSlugs = useMemo(
    () =>
      selected.size === 0 ? allSlugs : allSlugs.filter((s) => selected.has(s)),
    [allSlugs, selected],
  );

  const { decisions, isLoading: decisionsLoading } = useAllDecisions(activeSlugs);
  const actorsQuery = useActors();

  const isLoading =
    treesQuery.isLoading || decisionsLoading || actorsQuery.isLoading;

  const stats = useMemo(
    () => computeUserStats(handle, decisions, actorsQuery.data ?? []),
    [handle, decisions, actorsQuery.data],
  );

  const actor = actorsQuery.data?.find((a) => a.handle === handle);

  // Drill-in modal state
  const [drillState, setDrillState] = useState<DrillState | null>(null);
  const openDecision = useAppStore((s) => s.openDecision);
  const drill = (
    bucket: UserBucket,
    title: string,
    description?: string,
  ) => setDrillState({ bucket, title, description });

  const drillDecisions: Decision[] = useMemo(
    () =>
      drillState
        ? decisionsForUserBucket(
            handle,
            decisions,
            actorsQuery.data ?? [],
            drillState.bucket,
          )
        : [],
    [drillState, handle, decisions, actorsQuery.data],
  );

  // Origin-aware back: prefer browser history when there's somewhere to go,
  // otherwise fall back to the actors list (most likely entry point).
  const back = () => {
    if (window.history.length > 1) window.history.back();
    else window.location.assign("/ui/actors");
  };

  return (
    <div className="p-6 max-w-7xl mx-auto">
      <Button
        variant="light"
        size="sm"
        startContent={<ArrowLeft size={16} />}
        onPress={back}
      >
        Back
      </Button>

      <div className="mt-4 mb-6 flex items-center justify-between gap-4 flex-wrap">
        <div className="flex items-baseline gap-3 flex-wrap">
          <h1 className="text-3xl font-bold">{handle}</h1>
          {actor && (
            <>
              <span className="text-default-500">{actor.name}</span>
              <Chip
                size="sm"
                color={actor.kind === "agent" ? "secondary" : "primary"}
                variant="flat"
              >
                {actor.kind}
              </Chip>
            </>
          )}
        </div>
        <TreeFilter
          allSlugs={allSlugs}
          selected={selected}
          setSelected={setSelected}
        />
      </div>

      {isLoading ? (
        <div className="flex justify-center py-12">
          <Spinner size="lg" />
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <CreatorSection f={stats.creator} handle={handle} drill={drill} />
          <RecommenderSection
            f={stats.recommender}
            handle={handle}
            drill={drill}
          />
          <DeciderSection f={stats.decider} handle={handle} drill={drill} />
        </div>
      )}

      <DecisionListModal
        isOpen={drillState !== null}
        onClose={() => setDrillState(null)}
        title={drillState?.title ?? ""}
        description={drillState?.description}
        decisions={drillDecisions}
        onSelect={(tree, id) => {
          setDrillState(null);
          openDecision(tree, id);
        }}
      />
    </div>
  );
}

export default UserDrillDown;
