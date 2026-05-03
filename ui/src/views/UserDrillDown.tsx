import { useMemo, useState } from "react";
import { useParams, Link } from "wouter";
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
            label="Decisions opened"
            value={f.totalCreated}
            onClick={() =>
              drill(
                { facet: "creator", key: "all" },
                `Decisions opened by ${handle}`,
              )
            }
          />
          <MetricBlock
            label="Where they also recommended"
            value={f.alsoRecommender}
            subtext={`of ${f.totalCreated}`}
            onClick={() =>
              drill(
                { facet: "creator", key: "alsoRecommender" },
                `${handle} opened and recommended`,
              )
            }
          />
          <MetricBlock
            label="Their pick was followed"
            value={pct(f.alsoRecommenderDecided.rate)}
            subtext={`${f.alsoRecommenderDecided.accepted}/${f.alsoRecommenderDecided.total} decided`}
            color="success"
            onClick={() =>
              drill(
                { facet: "creator", key: "alsoRecAccepted" },
                `${handle}'s pick was followed`,
                "Decisions where they were both opener and recommender, and the chosen outcome matched their recommendation.",
              )
            }
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
      </CardBody>
    </Card>
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
          <Tooltip content="When this person suggested a specific choice. The recommendation can be on a decision they opened or on someone else's.">
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
            label="Resolved"
            value={f.decidedCount}
            subtext={`${f.totalRecommended - f.decidedCount} still open`}
            onClick={() =>
              drill(
                { facet: "recommender", key: "decided" },
                `${handle}'s resolved suggestions`,
              )
            }
          />
          <MetricBlock
            label="Followed"
            value={pct(f.acceptance.rate)}
            subtext={`${f.acceptance.accepted}/${f.acceptance.total}`}
            color="success"
            onClick={() =>
              drill(
                { facet: "recommender", key: "accepted" },
                `${handle}'s suggestions that were followed`,
              )
            }
          />
        </div>

        {f.decidedCount > 0 && (
          <div className="space-y-2">
            <div className="text-sm font-medium">
              How {handle}'s suggestions fared, by who decided
            </div>
            {f.byKindOfDecider.human.total > 0 && (
              <RateRow
                label="When humans decided"
                stat={f.byKindOfDecider.human}
                color="primary"
                onClick={() =>
                  drill(
                    {
                      facet: "recommender",
                      key: "byDeciderKind",
                      kind: "human",
                    },
                    `${handle}'s suggestions decided by humans`,
                  )
                }
              />
            )}
            {f.byKindOfDecider.agent.total > 0 && (
              <RateRow
                label="When agents decided"
                stat={f.byKindOfDecider.agent}
                color="secondary"
                onClick={() =>
                  drill(
                    {
                      facet: "recommender",
                      key: "byDeciderKind",
                      kind: "agent",
                    },
                    `${handle}'s suggestions decided by agents`,
                  )
                }
              />
            )}
            {f.byKindOfDecider.unknown.total > 0 && (
              <RateRow
                label="Decider unknown"
                stat={f.byKindOfDecider.unknown}
                color="default"
                onClick={() =>
                  drill(
                    {
                      facet: "recommender",
                      key: "byDeciderKind",
                      kind: "unknown",
                    },
                    `${handle}'s suggestions decided by an unknown actor`,
                  )
                }
              />
            )}
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
                    ),
                },
              ]}
            />
          </div>

          {f.followedRec > 0 && (
            <div>
              <div className="text-sm font-medium mb-2">
                Who they listened to
              </div>
              <PieBreakdown
                segments={[
                  {
                    label: "Agent",
                    value: f.followedFromAgent,
                    color: "#a855f7",
                    onClick: () =>
                      drill(
                        {
                          facet: "decider",
                          key: "followedFromKind",
                          kind: "agent",
                        },
                        `${handle} followed an agent's recommendation`,
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
                          key: "followedFromKind",
                          kind: "human",
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
                          key: "followedFromKind",
                          kind: "unknown",
                        },
                        `Followed an unrecognized recommender`,
                      ),
                  },
                ]}
              />
            </div>
          )}
        </div>

        {(f.agentTrust.total > 0 || f.humanTrust.total > 0) && (
          <div className="space-y-2 pt-2 border-t border-default-200">
            <div className="text-sm font-medium flex items-center gap-2">
              Trust profile
              <Tooltip content="Of all decisions where a recommendation was on the table and this person decided, what fraction did they follow — split by whether the recommender was an agent or a human.">
                <Info size={12} className="text-default-400 cursor-help" />
              </Tooltip>
            </div>
            {f.agentTrust.total > 0 && (
              <RateRow
                label="Trusts agents"
                stat={f.agentTrust}
                color="secondary"
                onClick={() =>
                  drill(
                    { facet: "decider", key: "trustKind", kind: "agent" },
                    `${handle}'s decisions where an agent recommended`,
                    "Includes both followed and overridden — modal will mix outcomes.",
                  )
                }
              />
            )}
            {f.humanTrust.total > 0 && (
              <RateRow
                label="Trusts humans"
                stat={f.humanTrust}
                color="primary"
                onClick={() =>
                  drill(
                    { facet: "decider", key: "trustKind", kind: "human" },
                    `${handle}'s decisions where a human recommended`,
                    "Includes both followed and overridden — modal will mix outcomes.",
                  )
                }
              />
            )}
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

  return (
    <div className="p-6 max-w-7xl mx-auto">
      <Link href="/dashboard">
        <Button
          variant="light"
          size="sm"
          startContent={<ArrowLeft size={16} />}
        >
          Back to dashboard
        </Button>
      </Link>

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
      />
    </div>
  );
}

export default UserDrillDown;
