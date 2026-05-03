import {
  QueryClient,
  useQuery,
  useQueries,
  type UseQueryResult,
} from "@tanstack/react-query";
import { apiFetch } from "@/api/client";
import type {
  Tree,
  TreesResponse,
  Decision,
  PaginatedResponse,
  Metrics,
  Event,
  AuditResponse,
  QueueItem,
  Status,
  Priority,
  Actor,
} from "@/api/types.gen";
import { subDays, format, parseISO } from "date-fns";

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 1000 * 30, // 30s
      retry: 1,
    },
  },
});

/** Centralised query-key registry — invalidate by key slice */
export const keys = {
  trees: () => ["trees"] as const,
  tree: (slug: string) => ["trees", slug] as const,
  decisions: (tree: string, params?: Record<string, string>) =>
    ["trees", tree, "decisions", params ?? {}] as const,
  decision: (tree: string, id: string) =>
    ["trees", tree, "decisions", id] as const,
  metrics: (tree: string) => ["trees", tree, "metrics"] as const,
  audit: (tree?: string, filters?: Record<string, string>) =>
    ["audit", tree ?? "all", filters ?? {}] as const,
  auditWithFilters: (tree: string, filters: Record<string, string>) =>
    ["audit", tree, filters] as const,
  auditSince: (since: string) => ["audit", "since", since] as const,
  queue: (tree: string, kind: string) =>
    ["trees", tree, "queues", kind] as const,
  actors: () => ["actors"] as const,
  health: () => ["health"] as const,
  history: (tree: string, id: string) => ["history", tree, id] as const,
};

// ---- Hooks ----

export function useTrees(includeArchived = false): UseQueryResult<Tree[]> {
  return useQuery({
    queryKey: keys.trees(),
    queryFn: async () => {
      const qs = includeArchived ? "?include_archived=true" : "";
      const { data } = await apiFetch<TreesResponse>(`/v1/trees${qs}`);
      return data.trees;
    },
  });
}

export function useDecisions(
  tree: string,
  params: Record<string, string> = {},
): UseQueryResult<PaginatedResponse<Decision>> {
  return useQuery({
    queryKey: keys.decisions(tree, params),
    queryFn: async () => {
      const qs = new URLSearchParams(params).toString();
      const url = `/v1/trees/${tree}/decisions${qs ? `?${qs}` : ""}`;
      const { data } = await apiFetch<PaginatedResponse<Decision>>(url);
      return data;
    },
    enabled: Boolean(tree),
  });
}

export function useDecision(
  tree: string,
  id: string,
): UseQueryResult<Decision> {
  return useQuery({
    queryKey: keys.decision(tree, id),
    queryFn: async () => {
      const { data } = await apiFetch<Decision>(
        `/v1/trees/${tree}/decisions/${id}`,
      );
      return data;
    },
    enabled: Boolean(tree) && Boolean(id),
  });
}

export function useHistory(
  tree: string,
  id: string,
): UseQueryResult<Event[]> {
  return useQuery({
    queryKey: keys.history(tree, id),
    queryFn: async () => {
      const { data } = await apiFetch<{ events: Event[] }>(
        `/v1/trees/${tree}/decisions/${id}/history`,
      );
      return data.events;
    },
    enabled: Boolean(tree) && Boolean(id),
  });
}

export function useMetrics(tree: string): UseQueryResult<Metrics> {
  return useQuery({
    queryKey: keys.metrics(tree),
    queryFn: async () => {
      const { data } = await apiFetch<Metrics>(`/v1/trees/${tree}/metrics`);
      return data;
    },
    enabled: Boolean(tree),
  });
}

export interface AuditFilters {
  action?: string;
  actor?: string;
  since?: string;
  until?: string;
  cursor?: string;
  limit?: string;
}

export function useAuditList(
  tree: string,
  filters: AuditFilters = {},
): UseQueryResult<AuditResponse> {
  const filterRecord: Record<string, string> = {};
  if (tree) filterRecord.tree = tree;
  if (filters.action) filterRecord.action = filters.action;
  if (filters.actor) filterRecord.actor = filters.actor;
  if (filters.since) filterRecord.since = filters.since;
  if (filters.until) filterRecord.until = filters.until;
  if (filters.cursor) filterRecord.cursor = filters.cursor;
  if (filters.limit) filterRecord.limit = filters.limit;

  return useQuery({
    queryKey: keys.audit(tree, filterRecord),
    queryFn: async () => {
      const qs = new URLSearchParams(filterRecord).toString();
      const url = `/v1/audit${qs ? `?${qs}` : ""}`;
      const { data } = await apiFetch<AuditResponse>(url);
      return data;
    },
    enabled: Boolean(tree),
  });
}

export interface QueueResponseSpearhead {
  items: QueueItem[];
}

export interface QueueResponseQuickWins {
  items: Decision[];
}

export function useQueue(
  tree: string,
  kind: "quick-wins" | "spearhead",
  limit?: number,
): UseQueryResult<QueueItem[] | Decision[]> {
  return useQuery({
    queryKey: keys.queue(tree, kind),
    queryFn: async () => {
      const qs = limit ? `?limit=${limit}` : "";
      const url = `/v1/trees/${tree}/queues/${kind}${qs}`;
      if (kind === "spearhead") {
        const { data } = await apiFetch<QueueResponseSpearhead>(url);
        return data.items;
      } else {
        const { data } = await apiFetch<QueueResponseQuickWins>(url);
        return data.items;
      }
    },
    enabled: Boolean(tree),
  });
}

export interface AggregateMetrics {
  total_decisions: number;
  total_trees: number;
  by_status: Record<Status, number>;
  by_priority: Record<Priority, number>;
  by_creator: Array<{ handle: string; count: number }>;
  assumptions_count: number;
  // Recommendation acceptance: % of decided where is_recommended or actual_choice === recommended_summary
  acceptance_rate: number | null;
  acceptance_numerator: number;
  acceptance_denominator: number;
  isLoading: boolean;
  isError: boolean;
}

// ---------------------------------------------------------------------------
// Cross-tree analytics primitives
// ---------------------------------------------------------------------------

/** All actors. Returns [] until loaded. */
export function useActors(): UseQueryResult<Actor[]> {
  return useQuery({
    queryKey: keys.actors(),
    queryFn: async () => {
      const { data } = await apiFetch<{ items: Actor[] }>("/v1/actors");
      return data.items;
    },
  });
}

/** All decisions across the given tree slugs. Empty slugs[] = no fetches. */
export function useAllDecisions(treeSlugs: string[]): {
  decisions: Decision[];
  isLoading: boolean;
  isError: boolean;
} {
  const results = useQueries({
    queries: treeSlugs.map((slug) => ({
      queryKey: keys.decisions(slug, { _scope: "all" }),
      queryFn: async () => {
        const { data } = await apiFetch<PaginatedResponse<Decision>>(
          `/v1/trees/${slug}/decisions?limit=1000`,
        );
        return data.items.map((d) => ({ ...d, tree: slug }));
      },
      enabled: Boolean(slug),
    })),
  });
  return {
    decisions: results.flatMap((r) => r.data ?? []),
    isLoading: results.some((r) => r.isLoading),
    isError: results.some((r) => r.isError),
  };
}

export function useAggregateMetrics(): AggregateMetrics {
  const treesQuery = useTrees();
  const treeSlugs = treesQuery.data?.map((t) => t.slug) ?? [];

  const metricsResults = useQueries({
    queries: treeSlugs.map((slug) => ({
      queryKey: keys.metrics(slug),
      queryFn: async () => {
        const { data } = await apiFetch<Metrics>(`/v1/trees/${slug}/metrics`);
        return data;
      },
      enabled: Boolean(slug),
    })),
  });

  const isLoading =
    treesQuery.isLoading || metricsResults.some((r) => r.isLoading);
  const isError =
    treesQuery.isError || metricsResults.some((r) => r.isError);

  const allMetrics = metricsResults
    .map((r) => r.data)
    .filter((d): d is Metrics => d != null);

  const total_decisions = allMetrics.reduce(
    (sum, m) => sum + m.total_decisions,
    0,
  );
  const assumptions_count = allMetrics.reduce(
    (sum, m) => sum + m.assumptions_count,
    0,
  );

  const by_status = allMetrics.reduce(
    (acc, m) => {
      for (const [k, v] of Object.entries(m.by_status)) {
        acc[k as Status] = (acc[k as Status] ?? 0) + v;
      }
      return acc;
    },
    {} as Record<Status, number>,
  );

  const by_priority = allMetrics.reduce(
    (acc, m) => {
      for (const [k, v] of Object.entries(m.by_priority)) {
        acc[k as Priority] = (acc[k as Priority] ?? 0) + v;
      }
      return acc;
    },
    {} as Record<Priority, number>,
  );

  // Merge by_creator: server returns {handle: count}; sum across trees, then
  // emit a sorted array for the dashboard to render.
  const creatorMap = new Map<string, number>();
  for (const m of allMetrics) {
    const entries: Array<[string, number]> = Array.isArray(m.by_creator)
      ? (m.by_creator as Array<{ handle: string; count: number }>).map((e) => [
          e.handle,
          e.count,
        ])
      : Object.entries(m.by_creator as unknown as Record<string, number>);
    for (const [handle, count] of entries) {
      creatorMap.set(handle, (creatorMap.get(handle) ?? 0) + count);
    }
  }
  const by_creator = Array.from(creatorMap.entries())
    .map(([handle, count]) => ({ handle, count }))
    .sort((a, b) => b.count - a.count);

  // Acceptance rate: by_status.decided count as denominator,
  // For numerator we rely on the decisions queries but metrics doesn't have
  // is_recommended breakdown, so we use a conservative 0/denominator when unknown
  // The Dashboard will compute this separately via decided decisions fetch.
  const acceptance_denominator = by_status["decided"] ?? 0;
  const acceptance_numerator = 0; // computed in Dashboard via decision fetches
  const acceptance_rate =
    acceptance_denominator > 0
      ? (acceptance_numerator / acceptance_denominator) * 100
      : null;

  return {
    total_decisions,
    total_trees: treeSlugs.length,
    by_status,
    by_priority,
    by_creator,
    assumptions_count,
    acceptance_rate,
    acceptance_numerator,
    acceptance_denominator,
    isLoading,
    isError,
  };
}

export interface DailyCount {
  date: string; // "YYYY-MM-DD"
  count: number;
}

export function useActivityCounts(days = 30): UseQueryResult<DailyCount[]> {
  // Stable bucket: midnight UTC `days` days ago. Without this, `new Date()`
  // would re-run on every render and the queryKey would change every paint,
  // causing perpetual loading.
  const today = new Date();
  today.setUTCHours(0, 0, 0, 0);
  const since = subDays(today, days).toISOString();

  return useQuery({
    queryKey: keys.auditSince(since),
    queryFn: async () => {
      const params = new URLSearchParams({ since, limit: "1000" });
      const { data } = await apiFetch<AuditResponse & { events?: Event[] }>(
        `/v1/audit?${params.toString()}`,
      );
      // Server returns {events: [...]} on this endpoint; keep `items` fallback
      // in case a future change normalises the shape.
      const events = data.events ?? data.items ?? [];

      const countMap = new Map<string, number>();
      for (let i = days - 1; i >= 0; i--) {
        const day = format(subDays(today, i), "yyyy-MM-dd");
        countMap.set(day, 0);
      }
      for (const event of events) {
        const day = format(parseISO(event.ts), "yyyy-MM-dd");
        if (countMap.has(day)) {
          countMap.set(day, (countMap.get(day) ?? 0) + 1);
        }
      }
      return Array.from(countMap.entries()).map(([date, count]) => ({
        date,
        count,
      }));
    },
  });
}
