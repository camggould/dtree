import { useMemo, useState } from "react";
import { Link } from "wouter";
import {
  Card,
  CardBody,
  CardHeader,
  Spinner,
  Chip,
  Button,
  Dropdown,
  DropdownTrigger,
  DropdownMenu,
  DropdownItem,
  Table,
  TableHeader,
  TableColumn,
  TableBody,
  TableRow,
  TableCell,
  Progress,
} from "@heroui/react";
import { ChevronDown, Users } from "lucide-react";
import {
  PieChart,
  Pie,
  Cell,
  BarChart,
  Bar,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  LineChart,
  Line,
  ResponsiveContainer,
} from "recharts";
import {
  useTrees,
  useActivityCounts,
  useActors,
  useAllDecisions,
} from "@/api/query";
import {
  computeAgentHumanBreakdown,
  computeUserStats,
  isAccepted,
  hasRecommendation,
  actorKind,
  type RateStat,
} from "@/analytics/insights";
import { useAppStore } from "@/store/app";
import { DecisionListModal } from "@/components/DecisionListModal";
import { humanPriority } from "@/util/labels";
import type { Decision } from "@/api/types.gen";

const STATUS_COLORS: Record<string, string> = {
  proposed: "#3b82f6",
  decided: "#22c55e",
  out_of_scope: "#6b7280",
  superseded: "#f97316",
};
const PRIORITY_ORDER = ["assumption", "low", "medium", "high", "critical"] as const;
const PRIORITY_COLORS: Record<string, string> = {
  assumption: "#a78bfa",
  low: "#94a3b8",
  medium: "#3b82f6",
  high: "#f59e0b",
  critical: "#ef4444",
};

function pct(rate: number | null): string {
  return rate === null ? "—" : `${rate.toFixed(1)}%`;
}

// ---------------------------------------------------------------------------
// Tree filter
// ---------------------------------------------------------------------------

function TreeFilter({
  allSlugs,
  selected,
  setSelected,
}: {
  allSlugs: string[];
  selected: Set<string>;
  setSelected: (s: Set<string>) => void;
}) {
  const label =
    selected.size === 0 || selected.size === allSlugs.length
      ? "All trees"
      : `${selected.size} tree${selected.size === 1 ? "" : "s"}`;

  return (
    <Dropdown closeOnSelect={false}>
      <DropdownTrigger>
        <Button
          variant="flat"
          endContent={<ChevronDown size={14} />}
          size="sm"
        >
          Trees: {label}
        </Button>
      </DropdownTrigger>
      <DropdownMenu
        aria-label="Tree filter"
        selectionMode="multiple"
        selectedKeys={selected}
        onSelectionChange={(keys) => {
          if (keys === "all") setSelected(new Set(allSlugs));
          else setSelected(new Set(Array.from(keys, String)));
        }}
      >
        {allSlugs.map((slug) => (
          <DropdownItem key={slug}>{slug}</DropdownItem>
        ))}
      </DropdownMenu>
    </Dropdown>
  );
}

// ---------------------------------------------------------------------------
// Cards
// ---------------------------------------------------------------------------

type DrillFn = (
  title: string,
  decisions: Decision[],
  description?: string,
) => void;

function AcceptanceRow({
  label,
  stat,
  color,
  onClick,
}: {
  label: string;
  stat: RateStat;
  color: "success" | "primary" | "secondary" | "default";
  onClick?: () => void;
}) {
  return (
    <button
      type="button"
      disabled={!onClick}
      onClick={onClick}
      className={`flex items-center gap-3 w-full rounded-md p-1 transition-colors ${
        onClick ? "hover:bg-default-100 cursor-pointer" : ""
      }`}
    >
      <div className="min-w-32 text-sm font-medium text-left">{label}</div>
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

function MiniStat({
  value,
  label,
  color,
  onClick,
}: {
  value: string | number;
  label: string;
  color?: "primary" | "success" | "default";
  onClick?: () => void;
}) {
  const cls =
    color === "success" ? "text-success" : color === "primary" ? "text-primary" : "";
  return (
    <button
      type="button"
      disabled={!onClick}
      onClick={onClick}
      className={`flex flex-col items-center text-center rounded-md p-2 transition-colors ${
        onClick ? "hover:bg-default-100 cursor-pointer" : ""
      }`}
    >
      <div className={`text-2xl font-bold ${cls}`}>{value}</div>
      <div className="text-xs text-default-500 mt-1">{label}</div>
    </button>
  );
}

function AgentVsHumanCard({
  decisions,
  actors,
  isLoading,
  openDrill,
}: {
  decisions: Decision[];
  actors: import("@/api/types.gen").Actor[] | undefined;
  isLoading: boolean;
  openDrill: DrillFn;
}) {
  const acts = actors ?? [];
  const breakdown = useMemo(
    () => computeAgentHumanBreakdown(decisions, acts),
    [decisions, acts],
  );

  // "Decided BY agent/human" — separate from "agent's REC accepted".
  const decided = decisions.filter((d) => d.status === "decided");
  const decidedByAgent = decided.filter(
    (d) => actorKind(acts, (d.decided_by ?? [])[0]) === "agent",
  );
  const decidedByHuman = decided.filter(
    (d) => actorKind(acts, (d.decided_by ?? [])[0]) === "human",
  );

  // For drill into "agent recs accepted":
  const agentRecAccepted = decisions.filter(
    (d) =>
      actorKind(acts, d.recommended_by) === "agent" && isAccepted(d),
  );
  const humanRecAccepted = decisions.filter(
    (d) =>
      actorKind(acts, d.recommended_by) === "human" && isAccepted(d),
  );

  return (
    <Card className="md:col-span-2">
      <CardHeader>
        <div>
          <h2 className="text-lg font-semibold">Delegation &amp; acceptance</h2>
          <p className="text-sm text-default-500">
            Two complementary lenses on agent/human roles in decision-making.
            Click any number to see the underlying decisions.
          </p>
        </div>
      </CardHeader>
      <CardBody className="gap-5">
        {isLoading ? (
          <Spinner />
        ) : (
          <>
            {/* Lens 1: Who DECIDED */}
            <div>
              <div className="text-sm font-medium mb-2 text-foreground">
                Who made the call
              </div>
              <div className="grid grid-cols-3 gap-2">
                <MiniStat
                  value={decided.length}
                  label="Total decided"
                  onClick={() => openDrill("Decided decisions", decided)}
                />
                <MiniStat
                  value={decidedByAgent.length}
                  label="Decided by agent"
                  color="primary"
                  onClick={() =>
                    openDrill(
                      "Decisions made by an agent",
                      decidedByAgent,
                      "An agent appeared in decided_by[]; the agent owns the outcome (regardless of who suggested it).",
                    )
                  }
                />
                <MiniStat
                  value={decidedByHuman.length}
                  label="Decided by human"
                  color="primary"
                  onClick={() =>
                    openDrill(
                      "Decisions made by a human",
                      decidedByHuman,
                      "A human appeared in decided_by[].",
                    )
                  }
                />
              </div>
            </div>

            {/* Lens 2: Whose RECOMMENDATIONS were accepted */}
            <div>
              <div className="text-sm font-medium mb-2 text-foreground">
                Whose recommendations were accepted
              </div>
              <div className="space-y-2">
                <AcceptanceRow
                  label="Agent recs accepted"
                  stat={breakdown.agent}
                  color="secondary"
                  onClick={() =>
                    openDrill(
                      "Agent recommendations that were accepted",
                      agentRecAccepted,
                      "Decided where the recommender was an agent and the chosen outcome matched the recommendation.",
                    )
                  }
                />
                <AcceptanceRow
                  label="Human recs accepted"
                  stat={breakdown.human}
                  color="primary"
                  onClick={() =>
                    openDrill(
                      "Human recommendations that were accepted",
                      humanRecAccepted,
                    )
                  }
                />
                {breakdown.unknown.total > 0 && (
                  <AcceptanceRow
                    label="Unknown recommender"
                    stat={breakdown.unknown}
                    color="default"
                  />
                )}
              </div>
              <div className="mt-3 pt-3 border-t border-divider text-xs text-default-500">
                <strong>Delegation rate</strong>: {pct(breakdown.delegationRate)} —{" "}
                {breakdown.withRecommendation} of {breakdown.totalDecided} decided
                decisions had a recommendation on the table.
              </div>
            </div>
          </>
        )}
      </CardBody>
    </Card>
  );
}

function HeadlineMetric({
  decisions,
  isLoading,
  openDrill,
}: {
  decisions: Decision[];
  isLoading: boolean;
  openDrill: DrillFn;
}) {
  const decided = decisions.filter((d) => d.status === "decided");
  const decidedWithRec = decided.filter(hasRecommendation);
  const accepted = decidedWithRec.filter(isAccepted);
  const rate =
    decidedWithRec.length === 0
      ? null
      : (accepted.length / decidedWithRec.length) * 100;

  return (
    <Card className="md:col-span-2">
      <CardHeader>
        <h2 className="text-lg font-semibold">Recommendation acceptance</h2>
      </CardHeader>
      <CardBody className="flex flex-col items-center justify-center py-6 gap-2">
        {isLoading ? (
          <Spinner size="lg" />
        ) : rate === null ? (
          <p className="text-default-400 text-sm">No decided decisions yet</p>
        ) : (
          <>
            <button
              type="button"
              className="rounded-md hover:bg-default-100 px-4 py-2 cursor-pointer text-center"
              onClick={() =>
                openDrill(
                  "Decisions that accepted the recommendation",
                  accepted,
                )
              }
            >
              <span className="text-6xl font-bold text-success">
                {rate.toFixed(1)}%
              </span>
              <p className="mt-1 text-sm text-default-500">
                {accepted.length} of {decidedWithRec.length} decisions with a
                recommendation followed it
              </p>
            </button>
          </>
        )}
      </CardBody>
    </Card>
  );
}

function SummaryTiles({
  decisions,
  treeCount,
  isLoading,
  openDrill,
}: {
  decisions: Decision[];
  treeCount: number;
  isLoading: boolean;
  openDrill: DrillFn;
}) {
  const proposed = decisions.filter((d) => d.status === "proposed");
  const decided = decisions.filter((d) => d.status === "decided");
  const tiles: Array<{
    label: string;
    value: number;
    onClick?: () => void;
  }> = [
    {
      label: "Total decisions",
      value: decisions.length,
      onClick: () => openDrill("All decisions in scope", decisions),
    },
    { label: "Trees in scope", value: treeCount },
    {
      label: "Proposed",
      value: proposed.length,
      onClick: () => openDrill("Proposed decisions", proposed),
    },
    {
      label: "Decided",
      value: decided.length,
      onClick: () => openDrill("Decided decisions", decided),
    },
  ];
  return (
    <>
      {tiles.map((t) => (
        <Card key={t.label}>
          <CardBody className="p-0">
            {isLoading ? (
              <div className="flex justify-center py-4">
                <Spinner size="sm" />
              </div>
            ) : (
              <button
                type="button"
                disabled={!t.onClick}
                onClick={t.onClick}
                className={`w-full flex flex-col items-center justify-center py-4 ${
                  t.onClick ? "hover:bg-default-100 cursor-pointer" : ""
                }`}
              >
                <span className="text-3xl font-bold">{t.value}</span>
                <span className="text-sm text-default-500 mt-1">
                  {t.label}
                </span>
              </button>
            )}
          </CardBody>
        </Card>
      ))}
    </>
  );
}

function StatusPie({
  decisions,
  isLoading,
  openDrill,
}: {
  decisions: Decision[];
  isLoading: boolean;
  openDrill: DrillFn;
}) {
  const grouped = decisions.reduce(
    (acc, d) => {
      (acc[d.status] ??= []).push(d);
      return acc;
    },
    {} as Record<string, Decision[]>,
  );
  const data = Object.entries(grouped).map(([name, ds]) => ({
    name,
    value: ds.length,
  }));

  return (
    <Card>
      <CardHeader>
        <h3 className="text-base font-semibold">Status breakdown</h3>
      </CardHeader>
      <CardBody>
        {isLoading ? (
          <div className="flex justify-center py-8">
            <Spinner />
          </div>
        ) : data.length === 0 ? (
          <p className="text-default-400 text-sm text-center py-8">No data</p>
        ) : (
          <ResponsiveContainer width="100%" height={260}>
            <PieChart>
              <Pie
                data={data}
                cx="50%"
                cy="50%"
                outerRadius={90}
                dataKey="value"
                style={{ cursor: "pointer" }}
                onClick={(_, idx) => {
                  const entry = data[idx ?? -1];
                  if (entry) {
                    openDrill(
                      `${entry.name} decisions`,
                      grouped[entry.name] ?? [],
                    );
                  }
                }}
                label={({ name, percent }) =>
                  `${String(name ?? "")} ${(((percent as number | undefined) ?? 0) * 100).toFixed(0)}%`
                }
              >
                {data.map((entry) => (
                  <Cell
                    key={entry.name}
                    fill={STATUS_COLORS[entry.name] ?? "#94a3b8"}
                  />
                ))}
              </Pie>
              <Tooltip />
            </PieChart>
          </ResponsiveContainer>
        )}
      </CardBody>
    </Card>
  );
}

function PriorityBar({
  decisions,
  isLoading,
  openDrill,
}: {
  decisions: Decision[];
  isLoading: boolean;
  openDrill: DrillFn;
}) {
  const grouped = decisions.reduce(
    (acc, d) => {
      (acc[d.priority] ??= []).push(d);
      return acc;
    },
    {} as Record<string, Decision[]>,
  );
  const data = PRIORITY_ORDER.map((p) => ({
    name: p,
    value: grouped[p]?.length ?? 0,
  }));

  return (
    <Card>
      <CardHeader>
        <h3 className="text-base font-semibold">Priority breakdown</h3>
      </CardHeader>
      <CardBody>
        {isLoading ? (
          <div className="flex justify-center py-8">
            <Spinner />
          </div>
        ) : (
          <ResponsiveContainer width="100%" height={260}>
            <BarChart data={data}>
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis dataKey="name" tick={{ fontSize: 12 }} />
              <YAxis allowDecimals={false} tick={{ fontSize: 12 }} />
              <Tooltip />
              <Bar
                dataKey="value"
                style={{ cursor: "pointer" }}
                onClick={(entry) => {
                  const name = (entry as { name?: string }).name;
                  if (name)
                    openDrill(
                      `${humanPriority(name)} priority decisions`,
                      grouped[name] ?? [],
                    );
                }}
              >
                {data.map((entry) => (
                  <Cell
                    key={entry.name}
                    fill={PRIORITY_COLORS[entry.name] ?? "#94a3b8"}
                  />
                ))}
              </Bar>
            </BarChart>
          </ResponsiveContainer>
        )}
      </CardBody>
    </Card>
  );
}

function ActivityLine() {
  const activityQuery = useActivityCounts(30);

  return (
    <Card className="md:col-span-2">
      <CardHeader>
        <h3 className="text-base font-semibold">Activity (last 30 days)</h3>
      </CardHeader>
      <CardBody>
        {activityQuery.isLoading ? (
          <div className="flex justify-center py-8">
            <Spinner />
          </div>
        ) : activityQuery.isError ? (
          <p className="text-danger text-sm">Failed to load audit data</p>
        ) : (
          <ResponsiveContainer width="100%" height={220}>
            <LineChart
              data={activityQuery.data ?? []}
              margin={{ top: 8, right: 16, left: 0, bottom: 8 }}
            >
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis
                dataKey="date"
                tick={{ fontSize: 11 }}
                tickFormatter={(v: string) => v.slice(5)}
                interval="preserveStartEnd"
              />
              <YAxis allowDecimals={false} tick={{ fontSize: 12 }} />
              <Tooltip />
              <Legend />
              <Line
                type="monotone"
                dataKey="count"
                name="events"
                stroke="#3b82f6"
                dot={false}
                strokeWidth={2}
              />
            </LineChart>
          </ResponsiveContainer>
        )}
      </CardBody>
    </Card>
  );
}

function ContributorsTable({
  decisions,
  actors,
  isLoading,
}: {
  decisions: ReturnType<typeof useAllDecisions>["decisions"];
  actors: ReturnType<typeof useActors>["data"];
  isLoading: boolean;
}) {
  const rows = useMemo(() => {
    if (!actors || actors.length === 0) return [];
    return actors
      .map((a) => {
        const s = computeUserStats(a.handle, decisions, actors);
        return {
          handle: a.handle,
          kind: a.kind,
          name: a.name,
          created: s.creator.totalCreated,
          recommended: s.recommender.totalRecommended,
          decided: s.decider.totalDecided,
          followedRec: s.decider.followedRec,
          rate: s.decider.acceptanceWhenRecExisted.rate,
        };
      })
      .filter(
        (r) => r.created > 0 || r.recommended > 0 || r.decided > 0,
      )
      .sort(
        (a, b) =>
          b.created + b.recommended + b.decided -
          (a.created + a.recommended + a.decided),
      );
  }, [decisions, actors]);

  return (
    <Card className="md:col-span-2">
      <CardHeader className="flex items-center gap-2">
        <Users size={18} />
        <h3 className="text-base font-semibold">Contributors</h3>
        <span className="text-default-400 text-sm">— click a row to drill in</span>
      </CardHeader>
      <CardBody>
        {isLoading ? (
          <Spinner />
        ) : rows.length === 0 ? (
          <p className="text-default-400 text-sm text-center py-8">
            No activity yet
          </p>
        ) : (
          <Table aria-label="Contributors" removeWrapper>
            <TableHeader>
              <TableColumn>Handle</TableColumn>
              <TableColumn>Kind</TableColumn>
              <TableColumn>Created</TableColumn>
              <TableColumn>Recommended</TableColumn>
              <TableColumn>Decided</TableColumn>
              <TableColumn>Followed rec.</TableColumn>
              <TableColumn>Acceptance rate</TableColumn>
            </TableHeader>
            <TableBody>
              {rows.map((r) => (
                <TableRow key={r.handle} className="cursor-pointer">
                  <TableCell>
                    <Link href={`/users/${r.handle}`}>
                      <span className="text-primary hover:underline">
                        {r.handle}
                      </span>
                    </Link>
                  </TableCell>
                  <TableCell>
                    <Chip
                      size="sm"
                      variant="flat"
                      color={r.kind === "agent" ? "secondary" : "primary"}
                    >
                      {r.kind}
                    </Chip>
                  </TableCell>
                  <TableCell>{r.created}</TableCell>
                  <TableCell>{r.recommended}</TableCell>
                  <TableCell>{r.decided}</TableCell>
                  <TableCell>{r.followedRec}</TableCell>
                  <TableCell className="font-semibold">{pct(r.rate)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardBody>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// Main
// ---------------------------------------------------------------------------

export function Dashboard() {
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

  // ---- Drill state ----
  const [drill, setDrill] = useState<{
    title: string;
    description?: string;
    decisions: Decision[];
  } | null>(null);
  const openDrill = (
    title: string,
    list: Decision[],
    description?: string,
  ) => setDrill({ title, decisions: list, description });
  const openDecision = useAppStore((s) => s.openDecision);

  return (
    <div className="p-6 max-w-7xl mx-auto">
      <div className="flex items-baseline justify-between mb-6 gap-4 flex-wrap">
        <h1 className="text-2xl font-bold">Dashboard</h1>
        <TreeFilter
          allSlugs={allSlugs}
          selected={selected}
          setSelected={setSelected}
        />
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <HeadlineMetric
          decisions={decisions}
          isLoading={isLoading}
          openDrill={openDrill}
        />
        <AgentVsHumanCard
          decisions={decisions}
          actors={actorsQuery.data}
          isLoading={isLoading}
          openDrill={openDrill}
        />

        <SummaryTiles
          decisions={decisions}
          treeCount={activeSlugs.length}
          isLoading={isLoading}
          openDrill={openDrill}
        />

        <StatusPie
          decisions={decisions}
          isLoading={isLoading}
          openDrill={openDrill}
        />
        <PriorityBar
          decisions={decisions}
          isLoading={isLoading}
          openDrill={openDrill}
        />

        <ActivityLine />

        <ContributorsTable
          decisions={decisions}
          actors={actorsQuery.data}
          isLoading={isLoading}
        />
      </div>

      <DecisionListModal
        isOpen={drill !== null}
        onClose={() => setDrill(null)}
        title={drill?.title ?? ""}
        description={drill?.description}
        decisions={drill?.decisions ?? []}
        onSelect={(t, id) => {
          setDrill(null);
          openDecision(t, id);
        }}
      />
    </div>
  );
}

export default Dashboard;
