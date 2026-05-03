import {
  QueryClient,
  useQuery,
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
} from "@/api/types.gen";

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
