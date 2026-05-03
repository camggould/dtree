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
  type RateStat,
} from "@/analytics/insights";

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

function AcceptanceRow({
  label,
  stat,
  color,
}: {
  label: string;
  stat: RateStat;
  color: "success" | "primary" | "secondary" | "default";
}) {
  return (
    <div className="flex items-center gap-3">
      <div className="min-w-24 text-sm font-medium">{label}</div>
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
    </div>
  );
}

function AgentVsHumanCard({
  decisions,
  actors,
  isLoading,
}: {
  decisions: ReturnType<typeof useAllDecisions>["decisions"];
  actors: ReturnType<typeof useActors>["data"];
  isLoading: boolean;
}) {
  const breakdown = useMemo(
    () => computeAgentHumanBreakdown(decisions, actors ?? []),
    [decisions, actors],
  );

  return (
    <Card className="md:col-span-2">
      <CardHeader>
        <div>
          <h2 className="text-lg font-semibold">Delegation & acceptance</h2>
          <p className="text-sm text-default-500">
            How often decisions follow a recommendation, broken down by who
            recommended
          </p>
        </div>
      </CardHeader>
      <CardBody className="gap-4">
        {isLoading ? (
          <Spinner />
        ) : (
          <>
            <div className="flex gap-6 text-center justify-around pb-3 border-b border-default-200">
              <div>
                <div className="text-2xl font-bold">{breakdown.totalDecided}</div>
                <div className="text-xs text-default-500">Decided</div>
              </div>
              <div>
                <div className="text-2xl font-bold">
                  {breakdown.withRecommendation}
                </div>
                <div className="text-xs text-default-500">With recommendation</div>
              </div>
              <div>
                <div className="text-2xl font-bold text-primary">
                  {pct(breakdown.delegationRate)}
                </div>
                <div className="text-xs text-default-500">Delegation rate</div>
              </div>
            </div>

            <AcceptanceRow
              label="Agent rec."
              stat={breakdown.agent}
              color="secondary"
            />
            <AcceptanceRow
              label="Human rec."
              stat={breakdown.human}
              color="primary"
            />
            {breakdown.unknown.total > 0 && (
              <AcceptanceRow
                label="Unknown"
                stat={breakdown.unknown}
                color="default"
              />
            )}
          </>
        )}
      </CardBody>
    </Card>
  );
}

function HeadlineMetric({
  decisions,
  isLoading,
}: {
  decisions: ReturnType<typeof useAllDecisions>["decisions"];
  isLoading: boolean;
}) {
  const decided = decisions.filter((d) => d.status === "decided");
  const accepted = decided.filter(isAccepted);
  const rate = decided.length === 0 ? null : (accepted.length / decided.length) * 100;

  return (
    <Card className="md:col-span-2">
      <CardHeader>
        <h2 className="text-lg font-semibold">Recommendation acceptance</h2>
      </CardHeader>
      <CardBody className="flex flex-col items-center justify-center py-6">
        {isLoading ? (
          <Spinner size="lg" />
        ) : rate === null ? (
          <p className="text-default-400 text-sm">No decided decisions yet</p>
        ) : (
          <>
            <span className="text-6xl font-bold text-success">
              {rate.toFixed(1)}%
            </span>
            <p className="mt-2 text-sm text-default-500 text-center">
              {accepted.length} of {decided.length} decided decisions accepted
              the recommendation
            </p>
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
}: {
  decisions: ReturnType<typeof useAllDecisions>["decisions"];
  treeCount: number;
  isLoading: boolean;
}) {
  const byStatus = decisions.reduce(
    (acc, d) => {
      acc[d.status] = (acc[d.status] ?? 0) + 1;
      return acc;
    },
    {} as Record<string, number>,
  );
  const tiles = [
    { label: "Total decisions", value: decisions.length },
    { label: "Trees in scope", value: treeCount },
    { label: "Proposed", value: byStatus["proposed"] ?? 0 },
    { label: "Decided", value: byStatus["decided"] ?? 0 },
  ];
  return (
    <>
      {tiles.map((t) => (
        <Card key={t.label}>
          <CardBody className="flex flex-col items-center justify-center py-4">
            {isLoading ? (
              <Spinner size="sm" />
            ) : (
              <>
                <span className="text-3xl font-bold">{t.value}</span>
                <span className="text-sm text-default-500 mt-1">{t.label}</span>
              </>
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
}: {
  decisions: ReturnType<typeof useAllDecisions>["decisions"];
  isLoading: boolean;
}) {
  const data = Object.entries(
    decisions.reduce(
      (acc, d) => {
        acc[d.status] = (acc[d.status] ?? 0) + 1;
        return acc;
      },
      {} as Record<string, number>,
    ),
  ).map(([name, value]) => ({ name, value }));

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
}: {
  decisions: ReturnType<typeof useAllDecisions>["decisions"];
  isLoading: boolean;
}) {
  const counts = decisions.reduce(
    (acc, d) => {
      acc[d.priority] = (acc[d.priority] ?? 0) + 1;
      return acc;
    },
    {} as Record<string, number>,
  );
  const data = PRIORITY_ORDER.map((p) => ({ name: p, value: counts[p] ?? 0 }));

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
              <Bar dataKey="value">
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

  // Selected trees state — empty == all trees.
  const [selected, setSelected] = useState<Set<string>>(new Set());

  // The slugs we actually fetch from. Empty selection -> all trees.
  const activeSlugs = useMemo(
    () => (selected.size === 0 ? allSlugs : allSlugs.filter((s) => selected.has(s))),
    [allSlugs, selected],
  );

  const { decisions, isLoading: decisionsLoading } = useAllDecisions(activeSlugs);
  const actorsQuery = useActors();

  const isLoading =
    treesQuery.isLoading || decisionsLoading || actorsQuery.isLoading;

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
        <HeadlineMetric decisions={decisions} isLoading={isLoading} />
        <AgentVsHumanCard
          decisions={decisions}
          actors={actorsQuery.data}
          isLoading={isLoading}
        />

        <SummaryTiles
          decisions={decisions}
          treeCount={activeSlugs.length}
          isLoading={isLoading}
        />

        <StatusPie decisions={decisions} isLoading={isLoading} />
        <PriorityBar decisions={decisions} isLoading={isLoading} />

        <ActivityLine />

        <ContributorsTable
          decisions={decisions}
          actors={actorsQuery.data}
          isLoading={isLoading}
        />
      </div>
    </div>
  );
}

export default Dashboard;
