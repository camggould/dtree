import { Card, CardBody, CardHeader, Spinner } from "@heroui/react";
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
import { useQueries } from "@tanstack/react-query";
import {
  useAggregateMetrics,
  useActivityCounts,
  useTrees,
} from "@/api/query";
import { apiFetch } from "@/api/client";
import type { PaginatedResponse, Decision } from "@/api/types.gen";

// ---- Color maps ----
const STATUS_COLORS: Record<string, string> = {
  proposed: "#3b82f6",   // blue
  decided: "#22c55e",    // green
  out_of_scope: "#6b7280", // gray
  superseded: "#f97316", // orange
};

const PRIORITY_ORDER = [
  "assumption",
  "low",
  "medium",
  "high",
  "critical",
] as const;

// ---- Recommendation acceptance ----

function useAcceptanceRate(): {
  rate: number | null;
  numerator: number;
  denominator: number;
  isLoading: boolean;
} {
  const treesQuery = useTrees();
  const treeSlugs = treesQuery.data?.map((t) => t.slug) ?? [];

  // Fetch all decided decisions per tree in parallel
  const decidedResults = useQueries({
    queries: treeSlugs.map((slug) => ({
      queryKey: ["trees", slug, "decisions", { status: "decided" }],
      queryFn: async () => {
        const { data } = await apiFetch<PaginatedResponse<Decision>>(
          `/v1/trees/${slug}/decisions?status=decided&limit=1000`,
        );
        return data.items;
      },
      enabled: Boolean(slug),
    })),
  });

  const isLoading =
    treesQuery.isLoading || decidedResults.some((r) => r.isLoading);

  const allDecided = decidedResults
    .flatMap((r) => r.data ?? []);

  const denominator = allDecided.length;
  const numerator = allDecided.filter(
    (d) =>
      d.is_recommended === true ||
      (d.actual_choice != null &&
        d.recommended_summary != null &&
        d.actual_choice === d.recommended_summary),
  ).length;

  const rate = denominator > 0 ? (numerator / denominator) * 100 : null;

  return { rate, numerator, denominator, isLoading };
}

// ---- Sub-components ----

function HeadlineMetric() {
  const { rate, numerator, denominator, isLoading } = useAcceptanceRate();

  return (
    <Card className="col-span-2">
      <CardHeader>
        <h2 className="text-lg font-semibold">Recommendation Acceptance</h2>
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
              {numerator} of {denominator} decided decisions accepted the
              recommendation
            </p>
          </>
        )}
      </CardBody>
    </Card>
  );
}

function SummaryTiles() {
  const aggregate = useAggregateMetrics();

  const tiles = [
    {
      label: "Total Decisions",
      value: aggregate.total_decisions,
    },
    {
      label: "Total Trees",
      value: aggregate.total_trees,
    },
    {
      label: "Proposed",
      value: aggregate.by_status["proposed"] ?? 0,
    },
    {
      label: "Decided",
      value: aggregate.by_status["decided"] ?? 0,
    },
  ];

  return (
    <>
      {tiles.map((tile) => (
        <Card key={tile.label}>
          <CardBody className="flex flex-col items-center justify-center py-4">
            {aggregate.isLoading ? (
              <Spinner size="sm" />
            ) : (
              <>
                <span className="text-3xl font-bold">{tile.value}</span>
                <span className="text-sm text-default-500 mt-1">
                  {tile.label}
                </span>
              </>
            )}
          </CardBody>
        </Card>
      ))}
    </>
  );
}

function StatusPie() {
  const aggregate = useAggregateMetrics();

  const data = Object.entries(aggregate.by_status).map(([key, value]) => ({
    name: key,
    value,
  }));

  return (
    <Card>
      <CardHeader>
        <h3 className="text-base font-semibold">Status Breakdown</h3>
      </CardHeader>
      <CardBody>
        {aggregate.isLoading ? (
          <div className="flex justify-center py-8">
            <Spinner />
          </div>
        ) : data.length === 0 ? (
          <p className="text-default-400 text-sm text-center py-8">
            No data yet
          </p>
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
                labelLine={false}
              >
                {data.map((entry) => (
                  <Cell
                    key={entry.name}
                    fill={STATUS_COLORS[entry.name] ?? "#94a3b8"}
                  />
                ))}
              </Pie>
              <Tooltip />
              <Legend />
            </PieChart>
          </ResponsiveContainer>
        )}
      </CardBody>
    </Card>
  );
}

function PriorityBar() {
  const aggregate = useAggregateMetrics();

  const data = PRIORITY_ORDER.map((p) => ({
    name: p,
    count: aggregate.by_priority[p] ?? 0,
  }));

  return (
    <Card>
      <CardHeader>
        <h3 className="text-base font-semibold">Priority Breakdown</h3>
      </CardHeader>
      <CardBody>
        {aggregate.isLoading ? (
          <div className="flex justify-center py-8">
            <Spinner />
          </div>
        ) : (
          <ResponsiveContainer width="100%" height={260}>
            <BarChart data={data} margin={{ top: 8, right: 16, left: 0, bottom: 8 }}>
              <CartesianGrid strokeDasharray="3 3" />
              <XAxis dataKey="name" tick={{ fontSize: 12 }} />
              <YAxis allowDecimals={false} tick={{ fontSize: 12 }} />
              <Tooltip />
              <Bar dataKey="count" fill="#6366f1" radius={[4, 4, 0, 0]} />
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
    <Card className="col-span-2">
      <CardHeader>
        <h3 className="text-base font-semibold">Activity (Last 30 Days)</h3>
      </CardHeader>
      <CardBody>
        {activityQuery.isLoading ? (
          <div className="flex justify-center py-8">
            <Spinner />
          </div>
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
                tickFormatter={(v: string) => v.slice(5)} // MM-DD
                interval="preserveStartEnd"
              />
              <YAxis allowDecimals={false} tick={{ fontSize: 12 }} />
              <Tooltip />
              <Line
                type="monotone"
                dataKey="count"
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

function TopContributors() {
  const aggregate = useAggregateMetrics();
  const top10 = aggregate.by_creator.slice(0, 10);

  return (
    <Card>
      <CardHeader>
        <h3 className="text-base font-semibold">Top Contributors</h3>
      </CardHeader>
      <CardBody>
        {aggregate.isLoading ? (
          <div className="flex justify-center py-8">
            <Spinner />
          </div>
        ) : top10.length === 0 ? (
          <p className="text-default-400 text-sm text-center py-8">
            No contributors yet
          </p>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-default-200">
                <th className="text-left py-2 font-medium text-default-600">
                  Handle
                </th>
                <th className="text-right py-2 font-medium text-default-600">
                  Decisions
                </th>
              </tr>
            </thead>
            <tbody>
              {top10.map(({ handle, count }, i) => (
                <tr
                  key={handle}
                  className="border-b border-default-100 last:border-0"
                >
                  <td className="py-2 flex items-center gap-2">
                    <span className="text-default-400 w-5 text-right text-xs">
                      {i + 1}.
                    </span>
                    <span className="font-mono">{handle}</span>
                  </td>
                  <td className="py-2 text-right font-semibold">{count}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </CardBody>
    </Card>
  );
}

// ---- Main Dashboard ----

export function Dashboard() {
  return (
    <div className="p-6 max-w-7xl mx-auto">
      <h1 className="text-2xl font-bold mb-6">Dashboard</h1>

      {/* Grid layout: 2-col on desktop, 1-col mobile */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {/* Headline metric — full width */}
        <HeadlineMetric />

        {/* Summary tiles — 4 tiles, each half width on desktop */}
        <SummaryTiles />

        {/* Charts row */}
        <StatusPie />
        <PriorityBar />

        {/* Activity line — full width */}
        <ActivityLine />

        {/* Top contributors */}
        <TopContributors />
      </div>
    </div>
  );
}

export default Dashboard;
