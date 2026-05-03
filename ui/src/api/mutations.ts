import { useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { apiFetch, ApiError } from "@/api/client";
import type { Decision, Relationship, RelationshipType } from "@/api/types.gen";
import { keys } from "@/api/query";

type AddToast = (opts: { title: string; description?: string; color?: string }) => void;

/** Invalidate every query that could change as a result of mutating one
 *  decision: the decision itself, all per-tree decision-list variants
 *  (filtered, paginated, anything starting with the same prefix), the
 *  per-decision history, the tree's metrics, and the audit log.
 */
function invalidateAllForDecision(
  qc: QueryClient,
  tree: string,
  id: string,
): void {
  qc.invalidateQueries({ queryKey: keys.decision(tree, id) });
  qc.invalidateQueries({ queryKey: keys.history(tree, id) });
  qc.invalidateQueries({ queryKey: ["trees", tree, "decisions"] });
  qc.invalidateQueries({ queryKey: keys.metrics(tree) });
  qc.invalidateQueries({ queryKey: ["audit"] });
}

/** Push the server's response straight into the cache so any active
 *  observer renders the new state on its very next render — no waiting
 *  for the refetch round-trip. Also bumps the `_rev` that subsequent
 *  If-Match calls will use, killing the stale-rev → 412 race when a
 *  user clicks twice in quick succession.
 */
function primeDecisionCache(
  qc: QueryClient,
  tree: string,
  id: string,
  fresh: Decision,
): void {
  qc.setQueryData(keys.decision(tree, id), fresh);
}

function handle412(err: unknown, refetch: () => void, addToast?: AddToast) {
  if (err instanceof ApiError && err.status === 412) {
    refetch();
    if (addToast) {
      addToast({
        title: "Conflict",
        description: "This decision was modified by someone else. Refreshed with latest version.",
        color: "warning",
      });
    } else {
      console.warn("412 Precondition Failed: decision was modified concurrently, refetching.");
    }
    return true;
  }
  return false;
}

export function useDecide(tree: string, id: string, addToast?: AddToast) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: {
      choice: string;
      reason: string;
      by: string[];
      is_recommended?: boolean;
      ifMatch?: string;
    }) => {
      const { ifMatch, ...payload } = body;
      return apiFetch<Decision>(`/v1/trees/${tree}/decisions/${id}/decide`, {
        method: "POST",
        body: JSON.stringify(payload),
        headers: ifMatch ? { "If-Match": ifMatch } : {},
      });
    },
    onSuccess: ({ data }) => {
      primeDecisionCache(qc, tree, id, data);
      invalidateAllForDecision(qc, tree, id);
    },
    onError: (err) => {
      handle412(err, () => qc.invalidateQueries({ queryKey: keys.decision(tree, id) }), addToast);
    },
  });
}

export function useUndecide(tree: string, id: string, addToast?: AddToast) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (ifMatch?: string) =>
      apiFetch<Decision>(`/v1/trees/${tree}/decisions/${id}/undecide`, {
        method: "POST",
        body: JSON.stringify({}),
        headers: ifMatch ? { "If-Match": ifMatch } : {},
      }),
    onSuccess: ({ data }) => {
      primeDecisionCache(qc, tree, id, data);
      invalidateAllForDecision(qc, tree, id);
    },
    onError: (err) => {
      handle412(err, () => qc.invalidateQueries({ queryKey: keys.decision(tree, id) }), addToast);
    },
  });
}

export function useScopeOut(tree: string, id: string, addToast?: AddToast) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { reason: string; ifMatch?: string }) => {
      const { ifMatch, ...payload } = body;
      return apiFetch<Decision>(`/v1/trees/${tree}/decisions/${id}/scope-out`, {
        method: "POST",
        body: JSON.stringify(payload),
        headers: ifMatch ? { "If-Match": ifMatch } : {},
      });
    },
    onSuccess: ({ data }) => {
      primeDecisionCache(qc, tree, id, data);
      invalidateAllForDecision(qc, tree, id);
    },
    onError: (err) => {
      handle412(err, () => qc.invalidateQueries({ queryKey: keys.decision(tree, id) }), addToast);
    },
  });
}

export function useSupersede(tree: string, id: string, addToast?: AddToast) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { by: string; ifMatch?: string }) => {
      const { ifMatch, ...payload } = body;
      return apiFetch<Decision>(`/v1/trees/${tree}/decisions/${id}/supersede`, {
        method: "POST",
        body: JSON.stringify(payload),
        headers: ifMatch ? { "If-Match": ifMatch } : {},
      });
    },
    onSuccess: ({ data }) => {
      primeDecisionCache(qc, tree, id, data);
      invalidateAllForDecision(qc, tree, id);
    },
    onError: (err) => {
      handle412(err, () => qc.invalidateQueries({ queryKey: keys.decision(tree, id) }), addToast);
    },
  });
}

export function useRestore(tree: string, id: string, addToast?: AddToast) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (ifMatch?: string) =>
      apiFetch<Decision>(`/v1/trees/${tree}/decisions/${id}/restore`, {
        method: "POST",
        body: JSON.stringify({}),
        headers: ifMatch ? { "If-Match": ifMatch } : {},
      }),
    onSuccess: ({ data }) => {
      primeDecisionCache(qc, tree, id, data);
      invalidateAllForDecision(qc, tree, id);
    },
    onError: (err) => {
      handle412(err, () => qc.invalidateQueries({ queryKey: keys.decision(tree, id) }), addToast);
    },
  });
}

export function useRelate(tree: string, id: string, addToast?: AddToast) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { type: RelationshipType; target: string; note?: string; ifMatch?: string }) => {
      const { ifMatch, ...payload } = body;
      return apiFetch<Relationship>(`/v1/trees/${tree}/decisions/${id}/relate`, {
        method: "POST",
        body: JSON.stringify(payload),
        headers: ifMatch ? { "If-Match": ifMatch } : {},
      });
    },
    onSuccess: () => {
      // Server returns the new Relationship (not the parent decision); just
      // invalidate so the decision refetches with updated relationships.
      invalidateAllForDecision(qc, tree, id);
    },
    onError: (err) => {
      handle412(err, () => qc.invalidateQueries({ queryKey: keys.decision(tree, id) }), addToast);
    },
  });
}

export function useUnrelate(tree: string, id: string, addToast?: AddToast) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: { type: RelationshipType; target: string; ifMatch?: string }) => {
      const { ifMatch, ...payload } = body;
      return apiFetch<void>(`/v1/trees/${tree}/decisions/${id}/unrelate`, {
        method: "POST",
        body: JSON.stringify(payload),
        headers: ifMatch ? { "If-Match": ifMatch } : {},
      });
    },
    onSuccess: () => {
      invalidateAllForDecision(qc, tree, id);
    },
    onError: (err) => {
      handle412(err, () => qc.invalidateQueries({ queryKey: keys.decision(tree, id) }), addToast);
    },
  });
}

export function useUpdateDecision(tree: string, id: string, addToast?: AddToast) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: Partial<Decision> & { ifMatch?: string }) => {
      const { ifMatch, ...payload } = body;
      return apiFetch<Decision>(`/v1/trees/${tree}/decisions/${id}`, {
        method: "PATCH",
        body: JSON.stringify(payload),
        headers: ifMatch ? { "If-Match": ifMatch } : {},
      });
    },
    onSuccess: ({ data }) => {
      primeDecisionCache(qc, tree, id, data);
      invalidateAllForDecision(qc, tree, id);
    },
    onError: (err) => {
      handle412(err, () => qc.invalidateQueries({ queryKey: keys.decision(tree, id) }), addToast);
    },
  });
}

export function useDeleteDecision(tree: string, id: string, addToast?: AddToast) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body?: { ifMatch?: string; hard?: boolean }) => {
      const qs = body?.hard ? "?hard=true" : "";
      return apiFetch<void>(`/v1/trees/${tree}/decisions/${id}${qs}`, {
        method: "DELETE",
        headers: body?.ifMatch ? { "If-Match": body.ifMatch } : {},
      });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: keys.decisions(tree) });
    },
    onError: (err) => {
      handle412(err, () => qc.invalidateQueries({ queryKey: keys.decision(tree, id) }), addToast);
    },
  });
}
