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
} from "@/api/types.gen";

export interface HistoryResponse {
  events: Event[];
}

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
  audit: (tree?: string) => ["audit", tree ?? "all"] as const,
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

export function useHistory(
  tree: string,
  id: string,
): UseQueryResult<HistoryResponse> {
  return useQuery({
    queryKey: keys.history(tree, id),
    queryFn: async () => {
      const { data } = await apiFetch<HistoryResponse>(
        `/v1/trees/${tree}/decisions/${id}/history`,
      );
      return data;
    },
    enabled: !!tree && !!id,
  });
}
